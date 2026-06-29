# gitlab-ship-provider — RISK lens

Failure-mode-first decomposition. Every GitLab-vs-Forgejo divergence (auth header,
project-identifier shape, `iid` vs `number`, MR vs PR endpoints, pipeline-state
ambiguity, reviewer-lookup failure) is owned and tested by exactly one task.

```
Phase gitlab-ship-provider — 8 tasks across 3 waves

Wave 1
  t-1  Add GitLab config surface + detection
       files:    internal/project/project.go, internal/project/remote.go,
                 internal/cmd/project.go, internal/defaults/defaults.go
       covers:   c-1
       contract: if the gitlab.com→/api/v4 mapping regresses, a remote_test.go case
                 DetectRemote("https://gitlab.com/o/r") asserting provider=="gitlab" &&
                 api_base=="https://gitlab.com/api/v4" fails; if the new dotted keys
                 aren't wired, a project_test.go round-trip writing remote.auth_scheme=
                 "bearer" and remote.project_id="42" then reading them back fails; an
                 arbitrary self-hosted host must leave Provider="" so manual config stays
                 the path (asserted in remote_test.go).

  t-2  Open GitLab MR via REST
       files:    internal/ship/open.go, internal/ship/open_test.go
       covers:   c-2
       contract: OpenOpts gains AuthScheme+ProjectID; OpenPR adds a "gitlab" case →
                 openGitLabPR + shared gitlabAuthHeader / gitlabProjectRef helpers.
                 If the auth scheme breaks, an httptest case asserts the PRIVATE-TOKEN
                 header carries the token by default and Authorization: Bearer when
                 AuthScheme="bearer"; if project-ref encoding breaks, a case asserts the
                 request path is /projects/o%2Fr/merge_requests by default and
                 /projects/42/merge_requests when ProjectID set; if param names regress,
                 the body must carry source_branch/target_branch (not head/base); the
                 Draft case asserts a "Draft: " title prefix and web_url→URL, iid→Number.

  t-3  Add GitLab steps to ship.md (CI gate + merge)
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-4
       contract: §5 gains GitLab pipeline polling (GET /api/v4/projects/<id>/pipelines?sha=)
                 with the LOCKED status mapping (success=pass; failed/canceled=fail→fix
                 loop; running/pending/created/preparing=keep polling; manual/skipped/
                 no-pipeline=ask the user); §6 gains the squash-merge (PUT .../merge_requests/
                 <iid>/merge with squash=true + should_remove_source_branch=true). If either
                 section is dropped, ship_prompt_test.go assertions that ship.md contains the
                 "pipelines?sha=" CI-watch string, the "merge_requests/<iid>/merge" squash
                 string, AND the manual/skipped/no-pipeline "ask" mapping fail.

Wave 2 (depends t-1, t-2)
  t-4  Diagnose GitLab remotes (doctor + telemetry)   [depends t-1]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
                 internal/telemetry/telemetry.go, internal/telemetry/telemetry_test.go
       covers:   c-1
       contract: doctor accepts provider=gitlab (origin matches remote.url + auth_env set)
                 and flags an invalid remote.auth_scheme (anything but private-token|bearer);
                 telemetry classifies "gitlab backend" / "private-token" error text as the
                 "provider" bucket. If doctor skips auth_scheme validation, a doctor_test.go
                 case with a gitlab remote + bogus auth_scheme that reports 0 issues fails;
                 if telemetry mis-buckets, telemetry_test.go asserting
                 classify("gitlab backend needs APIBase")=="provider" (not "other") fails.

  t-5  Implement GitLab forge backend (issues/milestones)   [depends t-1]
       files:    internal/forge/forge.go, internal/forge/forge_test.go,
                 internal/cmd/issue.go
       covers:   c-6
       contract: forge.Config gains AuthScheme+ProjectID; New() returns a GitLab Client for
                 provider=gitlab instead of ErrNotImplemented; requests use PRIVATE-TOKEN/
                 Bearer auth and the /projects/<ref> path; issues keyed on project-relative
                 iid for get/update/close/list; labels sent as a comma-joined name string
                 (NOT resolved to label ids like Forgejo); issue enable accepts gitlab.
                 If New() still errors for gitlab, the TestNewValidation gitlab row fails;
                 if iid keying breaks, a GetIssue/CloseIssue httptest case asserting the path
                 /projects/<ref>/issues/<iid> + PRIVATE-TOKEN header fails; if label handling
                 regresses to id-resolution, a CreateIssue case asserting labels go as a
                 comma-joined string fails; an issueEnable test asserts gitlab is not reported
                 as an unsupported board backend.

  t-6  Post GitLab MR note   [depends t-2]
       files:    internal/ship/comment.go, internal/ship/comment_test.go
       covers:   c-3
       contract: CommentOpts gains AuthScheme+ProjectID; PostComment adds a "gitlab" case
                 reusing gitlabAuthHeader/gitlabProjectRef; PRNumber is treated as the MR iid.
                 If the notes endpoint is keyed wrong, an httptest case for PRNumber=7 asserting
                 the POST path is /projects/<ref>/merge_requests/7/notes with body in "body"
                 and a PRIVATE-TOKEN header fails; unknown-provider rejection still holds.

  t-7  Resolve reviewers to user IDs (non-fatal)   [depends t-2]
       files:    internal/ship/open.go, internal/ship/open_test.go
       covers:   c-5
       contract: after the MR opens, each reviewer username resolves via
                 GET /users?username=<u> to a numeric id set as reviewer_ids on the MR; a
                 lookup miss or assignment error WARNS and returns the open MR instead of
                 aborting. If reviewer failure becomes fatal, an httptest case where /users
                 returns 500 (or an empty array) asserting OpenPR still returns the MR URL
                 with a non-nil warning error (mirroring TestOpenForgejoPRReviewerFailureNonFatal)
                 fails; a happy case asserts the lookup hits GET /users?username=alice and the
                 returned id lands in reviewer_ids.

Wave 3 (depends t-2, t-6)
  t-8  Wire GitLab provider config through ship cmd   [depends t-2, t-6]
       files:    internal/cmd/ship.go
       covers:   c-2, c-3
       contract: extract testable buildOpenOpts(p)/buildCommentOpts(p) that copy
                 remote.auth_scheme→AuthScheme and remote.project_id→ProjectID (plus the
                 existing url/api_base/auth_env/reviewers) into the opts, and confirm the
                 returned MR web_url is recorded in state on the gitlab path. If a field is
                 dropped, a ship_test.go unit asserting buildOpenOpts maps remote.project_id
                 and remote.auth_scheme onto OpenOpts (and buildCommentOpts likewise) fails —
                 catching the silent "GitLab uses default auth / derived id even when the user
                 overrode them" drift.
```

## Coverage
- c-1 (recognize gitlab: detect + manual config + doctor): **t-1, t-4**
- c-2 (open MR via REST, draft, url in state, no CLI): **t-2, t-8**
- c-3 (post MR note via notes endpoint): **t-6, t-8**
- c-4 (ship.md §5 pipeline poll + §6 squash-merge + presence test): **t-3**
- c-5 (reviewer username→id, non-fatal failure): **t-7**
- c-6 (forge GitLab client: milestones/issues by iid + enable gitlab): **t-5**

All criteria c-1..c-6 owned.

## Judgment calls
- **Reviewer resolution (t-7) split from MR-open (t-2) despite sharing open.go.** Chose a dedicated owner for the non-fatal-failure risk; rejected folding reviewer_ids into the create-MR POST because a bad/unresolvable id would fail the whole MR creation, directly violating the "lookup or assignment failure is non-fatal" criterion. t-7 opens first, then assigns.
- **project-identifier (t-2 gitlabProjectRef helper): URL-encode owner/repo by default, numeric project_id override.** Honors the locked decision (zero-config common case); rejected always-require a configured numeric id.
- **Auth header: single gitlabAuthHeader helper (PRIVATE-TOKEN default, Bearer via auth_scheme) in the ship pkg, reused by t-6; forge pkg (t-5) duplicates it.** Rejected a new shared auth package — the codebase already duplicates splitOwnerRepo across ship/forge to keep package coupling flat, so I mirror that rather than introduce new cross-package wiring this phase.
- **Doctor + telemetry merged into one diagnostics task (t-4).** Both exist to make GitLab failures visible (validate the remote / bucket the error). Rejected a standalone one-line telemetry task as too-small; the auth_scheme-validation regression test is the substantive half.
- **ship.md (t-3) sits in wave 1, not after the REST tasks.** The prompt + its presence test have no Go dependency, so sequencing it later would only cost parallelism.
- **cmd wiring (t-8) split out as the integration point with testable buildOpenOpts/buildCommentOpts helpers.** It depends on both opts shapes (t-2, t-6) and owns the silent-config-drop risk; leaving the wiring inline in t-2/t-6 would leave that risk untested because cmd/ship.go's push path isn't unit-testable as-is.
