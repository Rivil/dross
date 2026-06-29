# gitlab-ship-provider — VERIFICATION lens

Designed backward from test contracts. Each task exists because a specific test
should be able to fail when its slice of behaviour breaks. The two proven tools
here are the **httptest mock** (Forgejo `open_test`/`comment_test`/`forge_test`)
and the **ghCommand factory override**; GitLab is REST-only so every network
contract below is an `httptest.NewServer` asserting method + path + header +
body, exactly like `TestOpenForgejoPRHappyPath`. Prompt-only work (c-4) is locked
behind a `shipPromptContent` grep test, never a Go unit test (rule r-01).

```
Phase gitlab-ship-provider — 9 tasks across 3 waves

Wave 1
  t-1  Add GitLab remote config fields
       files:    internal/project/project.go, internal/cmd/project.go
       covers:   c-1
       contract: extend TestRemoteRoundTrip with auth_scheme="bearer" +
                 project_id=42 — if the `auth_scheme`/`project_id` toml tags are
                 dropped they vanish across Save→Load and the assert fails; a
                 project set/get test (`remote.auth_scheme`→read back) fails if
                 the readDotted/writeDotted cases are missing.

  t-2  Recognize gitlab host + accept in doctor
       files:    internal/project/remote.go, internal/cmd/doctor_test.go
       covers:   c-1
       contract: add a TestDetectRemote row for "https://gitlab.com/o/r" — fails
                 unless KnownHostProviders maps gitlab.com→gitlab AND the APIBase
                 switch derives "https://gitlab.com/api/v4". New
                 TestDoctorAcceptsGitLabRemote: project.toml provider=gitlab,
                 origin==remote.url, $GITLAB_TOKEN set → doctor reports 0 remote
                 issues (fails if a gitlab remote is ever rejected/mis-validated).

  t-3  Add GitLab CI + merge steps to ship.md
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-4
       contract: TestShipPromptGitLabSections asserts shipPromptContent contains
                 `api/v4/projects`, `pipelines?sha`, the terminal words
                 `success`/`failed`/`canceled`, the ambiguous words
                 `manual`/`skipped`, plus `squash=true` and
                 `should_remove_source_branch` — i.e. the locked
                 pipeline_status_mapping + the §6 squash-merge PUT. Missing any
                 token fails the grep (the only check that catches a prompt-only
                 regression).

  t-4  Extract forge Backend interface; enable gitlab
       files:    internal/forge/forge.go, internal/cmd/issue.go
       covers:   c-6
       contract: rename the concrete struct to forgejoBackend behind a Backend
                 interface (the six methods issue.go calls). TestBackend
                 Conformance: var _ Backend = (*forgejoBackend)(nil) — drop a
                 method and issue.go stops compiling. TestIssueEnableAcceptsGitLab:
                 `dross issue enable` with provider=gitlab must NOT print
                 "no board backend yet" (it currently does via the default arm).

  t-5  Shared GitLab REST primitives (ship pkg)
       files:    internal/ship/gitlab.go, internal/ship/gitlab_test.go
       covers:   c-2, c-3, c-5
       contract: TestGitLabProjectRef: ("me","p",0)→"me%2Fp"; ("me","p",123)→"123"
                 (numeric project_id override wins). TestGitLabAuthHeader: scheme
                 ""/"private-token"→header PRIVATE-TOKEN:tok with NO Authorization;
                 "bearer"→Authorization: Bearer tok with NO PRIVATE-TOKEN.
                 TestGitLabRequestStatus: a 422 from the stub surfaces "HTTP 422"
                 + body. These pure-function tests are why the helper is its own
                 task — the encoding and header choice are the bug-prone bits.

Wave 2 (depends t-5, t-4)
  t-6  openGitLabPR: MR create + reviewer-id resolution
       files:    internal/ship/open.go, internal/ship/open_test.go,
                 internal/telemetry/telemetry.go, internal/telemetry/telemetry_test.go
       covers:   c-2, c-5
       depends:  t-5
       contract: TestOpenGitLabPRHappyPath (httptest): POST
                 /api/v4/projects/me%2Fp/merge_requests, header PRIVATE-TOKEN:secret,
                 body source_branch="phase/x"/target_branch="main"/title/description;
                 stub returns {"iid":7,"web_url":...} → OpenResult{Number:7,URL:web}.
                 TestOpenGitLabPRDraftPrefix: Draft=true → title gets "Draft: ".
                 TestOpenGitLabPRBearerScheme: AuthScheme="bearer" → Authorization:
                 Bearer (no PRIVATE-TOKEN). TestOpenGitLabPRResolvesReviewerIDs:
                 GET /api/v4/users?username=alice→[{"id":11}], bob→[{"id":22}],
                 then PUT .../merge_requests/7 carries reviewer_ids [11,22].
                 TestOpenGitLabPRReviewerLookupNonFatal: /users 404 → res non-nil
                 (MR opened) AND non-nil error mentioning reviewer (mirrors
                 TestOpenForgejoPRReviewerFailureNonFatal). TestClassifyError gains
                 "gitlab backend needs APIBase" → "provider" bucket.

  t-7  postGitLabComment: MR notes
       files:    internal/ship/comment.go, internal/ship/comment_test.go
       covers:   c-3
       depends:  t-5
       contract: TestPostGitLabCommentHappyPath: POST
                 /api/v4/projects/me%2Fproj/merge_requests/7/notes, body {"body":...},
                 header PRIVATE-TOKEN:tok123 (mirrors TestPostForgejoCommentHappyPath
                 with the notes path + token header). TestPostGitLabCommentBearer:
                 AuthScheme="bearer" → Authorization: Bearer. Dispatch: provider
                 "gitlab" routes to postGitLabComment while
                 TestPostCommentRejectsUnknownProvider still rejects "weird".

  t-8  gitlabBackend impl + New() routing
       files:    internal/forge/gitlab.go, internal/forge/gitlab_test.go,
                 internal/forge/forge.go, internal/cmd/issue.go
       covers:   c-6
       depends:  t-4
       contract: gitlab_test.go uses the newTestClient httptest shape against a
                 gitlabBackend. TestGitLabEnsureMilestone: GET+POST
                 /api/v4/projects/me%2Fproj/milestones. TestGitLabCreateIssue: POST
                 /api/v4/projects/me%2Fproj/issues, `labels` sent as a comma-joined
                 STRING (not id array — GitLab's model), response {"iid":12} →
                 Issue.Number==12. TestGitLabUpdateKeyedOnIID: PUT .../issues/12.
                 TestGitLabCloseIssue: body carries state_event="close".
                 TestGitLabListIssues: GET .../issues?state=open. TestNewValidation
                 gains a gitlab row → New(provider=gitlab) returns a backend, NOT
                 ErrNotImplemented. forge.Config gains AuthScheme/ProjectID and
                 openBoard threads Remote.AuthScheme/ProjectID through.

Wave 3 (depends t-1, t-6, t-7)
  t-9  Thread auth_scheme/project_id from project into ship
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-2, c-3, c-5
       depends:  t-1, t-6, t-7
       contract: TestShipThreadsGitLabAuthFields: a temp-root project.toml with
                 provider=gitlab, auth_scheme=bearer, project_id=42 driven through
                 the Ship/shipComment commands against an httptest server must hit
                 /api/v4/projects/42/... with Authorization: Bearer — if cmd/ship.go
                 omits AuthScheme/ProjectID from the OpenOpts/CommentOpts it builds,
                 the request falls back to PRIVATE-TOKEN + the encoded path and the
                 assert fails. (The logic is in t-6/t-7; this proves the CLI wires it.)
```

## Coverage

| criterion | tasks |
| --------- | ----- |
| c-1 (recognize gitlab provider: autodetect, manual config, doctor) | t-1, t-2 |
| c-2 (ship opens MR via REST POST /merge_requests, draft prefix, URL→state) | t-5, t-6, t-9 |
| c-3 (ship comment posts MR note via /notes) | t-5, t-7, t-9 |
| c-4 (ship.md §5 pipeline poll + §6 squash merge, prompt-presence test) | t-3 |
| c-5 (username→id lookup, reviewer_ids, non-fatal failure) | t-5, t-6, t-9 |
| c-6 (forge.New gitlab client, issue ops on iid, issue enable accepts gitlab) | t-4, t-8 |

Every criterion c-1..c-6 has at least one task whose named test fails if that
criterion breaks.

## Judgment calls

- **Pulled the GitLab auth-header + project-ref encoding into a standalone pure
  helper task (t-5)** rather than inlining it in open/comment like Forgejo does.
  Rejected: duplicating the `%2F` encoding and PRIVATE-TOKEN/bearer switch inside
  both openGitLabPR and postGitLabComment. Why: those two lines are the most
  bug-prone surface (decisions auth_scheme + project_identifier both live here)
  and a pure function gets a deterministic unit test with no httptest scaffolding
  — the cheapest possible failing test for the riskiest logic. It also lets t-6/t-7
  run in parallel.
- **Folded reviewer-id resolution (c-5) into the openGitLabPR task (t-6), not a
  separate task.** Rejected: a standalone reviewer task. Why: GitLab sets reviewers
  on the same MR object the create path owns, exactly like openForgejoPR handles
  requested_reviewers inline; the non-fatal-failure contract is only meaningful as
  "MR opened AND warn", which requires the create path in the same test.
- **t-4 extracts a `Backend` interface instead of branching provider inside each
  forge method.** Rejected: an `if provider==gitlab` ladder in EnsureMilestone/
  CreateIssue/etc. Why: GitLab's issue identity (iid), label model (CSV strings,
  not id arrays), and state_event close differ enough that branching would tangle
  every method; a separate gitlabBackend file gets its own clean httptest suite
  (t-8) and the interface boundary itself becomes a compile-enforced contract.
- **Doctor (t-2) gets a regression test, not new validation code.** Rejected:
  inventing an auth_scheme-enum check just to have a code change. Why: doctor
  already validates origin-match + auth_env generically with no provider allowlist,
  so a gitlab remote passes today; the honest deliverable is a test that locks that
  in (c-1 explicitly only asks for origin/auth_env validation + non-rejection).
- **Separate wave-3 wiring task (t-9) instead of folding cmd/ship.go edits into
  t-6/t-7.** Rejected: putting the cmd glue in the ship-package tasks. Why: the
  ship-package tasks stay pure and independent (wave 1/2, unit-testable via OpenPR/
  PostComment directly), and the CLI wiring genuinely needs both the new OpenOpts/
  CommentOpts fields (t-6/t-7) and the new Remote fields (t-1) to exist first.
- **GitLab errors are phrased "gitlab backend …"** so the existing telemetry
  provider-bucket classifier extends with one substring (added in t-6), matching
  the "forgejo backend"/"github backend" precedent rather than leaking to "other".
