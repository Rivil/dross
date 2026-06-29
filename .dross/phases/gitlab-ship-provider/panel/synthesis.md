# gitlab-ship-provider — SYNTHESIS (cold judge)

Three drafts judged against four dimensions, then merged. Skeleton = **risk**;
grafts pulled from mvp (reviewer fold) and verification (contract tokens, pure-
helper tests). Source files were read to adjudicate file lists and waves.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| **risk** (8t/3w) | 5 — all c-1..c-6 owned; doctor sub-clause owned (t-4); cmd-wiring silent-drop risk owned (t-8) | 4 — concrete httptest assertions + named mirror tests, but fewer exact test-fn names than verification | 4 — clean per-criterion ownership, but splitting reviewers (t-7) out of the MR-create function is needless | 5 — t-1/t-2 correctly parallel; forge client+New() together in t-5; deps all sound |
| **mvp** (4t/2w) | 4 — all six covered, but doctor is test-only inside t-1 and the cmd-wiring path is untested | 3 — describes assertions but coarse; t-3 bundles three criteria's contracts in one block | 2 — t-3 merges c-2+c-3+c-5 **plus** cmd/ship.go glue (4 code files / 2 layers → too-large); wiring risk untested | 5 — 2 waves, minimal correct deps, simplest safe ordering |
| **verification** (9t/3w) | 5 — all six; c-1 and c-6 split with explicit per-slice tests | 5 — exact test-fn names + exact grep tokens; designed test-first | 3 — strong helper-as-task, but Backend-interface extraction (t-4) + config split (t-1/t-2) are speculative; 9 tasks | 4 — correct, but most cross-wave same-file edits (forge.go/issue.go in t-4 & t-8) → more coordination |

**Skeleton: risk.** Best balance of coverage, granularity and wave correctness,
and it uniquely owns the cmd-wiring silent-config-drop risk (buildOpenOpts/
buildCommentOpts must copy `auth_scheme`/`project_id`) that mvp leaves untested.
It loses two points: it over-splits reviewer resolution, and its contracts are a
notch less specific than verification's — both fixed by graft below.

## Merged plan

**7 tasks across 3 waves.** (risk's 8 minus the reviewer task, which folds into
the open task per mvp+verification + source evidence.)

```
Wave 1
  t-1  Add GitLab config surface + detection                              [risk+mvp]
       files:    internal/project/project.go, internal/project/remote.go,
                 internal/cmd/project.go, internal/project/remote_test.go,
                 internal/cmd/project_test.go
                 (+ internal/defaults/defaults.go — risk-only, optional; see D6)
       covers:   c-1 (detect + config fields)
       contract: DetectRemote("https://gitlab.com/o/r") asserts provider=="gitlab"
                 && api_base=="https://gitlab.com/api/v4" (KnownHostProviders +
                 APIBase switch in remote.go); an arbitrary self-hosted host leaves
                 Provider=="" so manual config stays the path; a project set-then-get
                 round-trip on remote.auth_scheme="bearer" and remote.project_id="42"
                 fails if the readDotted/writeDotted "remote.*" arms aren't added and
                 the Remote struct doesn't gain AuthScheme/ProjectID toml tags.
       depends:  —

  t-2  Open GitLab MR via REST (+ reviewer-id resolution, non-fatal)   [risk+mvp+verification]
       files:    internal/ship/open.go, internal/ship/open_test.go
       covers:   c-2, c-5
       contract: OpenOpts gains AuthScheme+ProjectID; OpenPR adds a "gitlab" case →
                 openGitLabPR with a gitlabAuthHeader / gitlabProjectRef helper.
                 Pure-fn units (grafted from verification t-5): TestGitLabProjectRef
                 ("me","p",0)->"me%2Fp", ("me","p",123)->"123" (numeric project_id
                 wins); TestGitLabAuthHeader ""/"private-token"->PRIVATE-TOKEN header
                 with NO Authorization, "bearer"->Authorization: Bearer with NO
                 PRIVATE-TOKEN. httptest: POST /api/v4/projects/me%2Fp/merge_requests
                 with source_branch/target_branch (NOT head/base) + title/description;
                 Draft=true -> "Draft: " title prefix; web_url->URL, iid->Number.
                 Reviewers resolve via GET /api/v4/users?username= -> numeric id set
                 as reviewer_ids; a 404/empty/500 on lookup or assignment WARNS and
                 returns the open MR (mirrors TestOpenForgejoPRReviewerFailureNonFatal,
                 which already returns &OpenResult+error inline).
       depends:  —  (open.go only references OpenOpts fields it adds itself, not
                     p.Remote; so it is genuinely parallel with t-1)

  t-3  Add GitLab CI + merge steps to ship.md                  [verification+risk+mvp]
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-4
       contract: PROMPT-PRESENCE grep test only (rule r-01; no Go unit). §5 gains
                 GitLab pipeline polling (GET /api/v4/projects/<id>/pipelines?sha=)
                 with the LOCKED status mapping; §6 gains the squash-merge (PUT
                 .../merge_requests/<iid>/merge, squash=true +
                 should_remove_source_branch=true). TestShipPromptGitLabSections
                 asserts shipPromptContent contains the exact tokens (verification's
                 sharper list): `api/v4/projects`, `pipelines?sha`, `success`,
                 `failed`, `canceled`, the ambiguous `manual`/`skipped`, `squash=true`,
                 `should_remove_source_branch`. Missing any token fails the grep.
       depends:  —

Wave 2 (depends on Wave 1)
  t-4  Diagnose GitLab remotes (doctor + telemetry)                          [risk]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
                 internal/telemetry/telemetry.go, internal/telemetry/telemetry_test.go
       covers:   c-1
       contract: MANDATORY (c-1-faithful): TestDoctorAcceptsGitLabRemote — project.toml
                 provider=gitlab, origin==remote.url, auth_env set -> doctor reports 0
                 remote issues (locks in non-rejection; doctor has no provider allowlist
                 so this is largely a regression test, per verification t-2).
                 OPTIONAL (risk addition, beyond c-1): flag an invalid remote.auth_scheme
                 (anything but private-token|bearer). Telemetry: classify("gitlab backend
                 needs APIBase")=="provider" not "other" — one substring added next to
                 the existing "github backend"/"forgejo backend" arm (telemetry.go ~269).
       depends:  t-1

  t-5  Implement GitLab forge backend (issues/milestones) + New() + enable    [risk+mvp]
       files:    internal/forge/forge.go, internal/forge/forge_test.go,
                 internal/cmd/issue.go
       covers:   c-6
       contract: forge.Config gains AuthScheme+ProjectID; New() returns a GitLab client
                 for provider=gitlab INSTEAD of ErrNotImplemented (client impl + dispatch
                 land in ONE task — see D3). TestNewValidation gains a gitlab row
                 returning a backend. Requests use PRIVATE-TOKEN/Bearer auth and the
                 /api/v4/projects/<enc-or-id> path. EnsureMilestone -> /milestones;
                 issues keyed on project-relative iid for get/update/close/list:
                 GetIssue/CloseIssue assert /projects/<ref>/issues/<iid>; CloseIssue body
                 carries state_event="close"; CreateIssue sends `labels` as a comma-joined
                 STRING (GitLab's model — NOT id-resolved like Forgejo), response iid->
                 Issue.Number; ListIssues GET .../issues?state=open. issue enable accepts
                 gitlab (replaces the cmd/issue.go:126-132 default "no board backend" arm).
                 openBoard threads Remote.AuthScheme/ProjectID into forge.Config.
       depends:  t-1

  t-6  Post GitLab MR note                                                   [risk]
       files:    internal/ship/comment.go, internal/ship/comment_test.go
       covers:   c-3
       contract: CommentOpts gains AuthScheme+ProjectID; PostComment adds a "gitlab"
                 case reusing gitlabAuthHeader/gitlabProjectRef; PRNumber == MR iid.
                 httptest for PRNumber=7 asserts POST /api/v4/projects/<ref>/
                 merge_requests/7/notes, body {"body":...}, PRIVATE-TOKEN header (and
                 Authorization: Bearer when scheme=bearer); unknown-provider rejection
                 still holds.
       depends:  t-2

Wave 3 (depends on Wave 2)
  t-7  Wire GitLab provider config through ship cmd                  [risk+verification]
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-2, c-3
       contract: extract testable buildOpenOpts(p)/buildCommentOpts(p) that copy
                 remote.auth_scheme->AuthScheme and remote.project_id->ProjectID (plus
                 existing url/api_base/auth_env/reviewers) onto the opts, and confirm the
                 returned MR web_url is recorded in state on the gitlab path. ship_test.go
                 asserts buildOpenOpts maps project_id+auth_scheme (buildCommentOpts
                 likewise) — catching the silent "GitLab uses default auth / derived id
                 even when the user overrode them" drift (the inline-built opts at
                 cmd/ship.go:184-193 & 279-283 are not unit-testable as-is).
       depends:  t-2, t-6
```

### Coverage check
- c-1 → t-1, t-4
- c-2 → t-2, t-7
- c-3 → t-6, t-7
- c-4 → t-3
- c-5 → t-2 (folded)
- c-6 → t-5

All c-1..c-6 owned; every criterion has a task whose named test fails if it breaks.

## Disagreements

**D1 — Granularity of the ship-package REST surface (c-2/c-3/c-5).**
- mvp: ONE wave-2 task (t-3) merging open + comment + reviewers + cmd/ship.go glue.
- risk: FOUR tasks — open (t-2), comment (t-6), reviewers (t-7), cmd-wiring (t-8).
- verification: shared-helper (t-5) + open+reviewers (t-6) + comment (t-7) + wiring (t-9).
- **Provisional default:** risk's split, minus the reviewer split → open(t-2, reviewers folded), comment(t-6), cmd-wiring(t-7).
- **Why it matters:** mvp's single task bundles three criteria + cmd glue (4 code files / 2 layers = too-large) and leaves the silent-config-drop risk untested; the cmd-wiring task must be separate because the inline-built opts in cmd/ship.go aren't unit-testable today.

**D2 — Reviewer resolution: own task vs folded into openGitLabPR.**
- risk: own task (t-7). mvp + verification: folded into the open task.
- **Provisional default:** FOLDED (2-1, and source-confirmed). `openForgejoPR` (open.go:128-137) sets reviewers inline in the create flow and already returns `&OpenResult + error` for the non-fatal case.
- **Why it matters:** a separate task means two tasks editing one MR-create function; the "MR opened AND warn" contract is only testable with the create path present in the same test.

**D3 — forge c-6: single task vs Backend-interface extraction split.**
- risk + mvp: single task (client impl + New() routing + enable together).
- verification: t-4 (extract Backend interface + enable, wave 1) + t-8 (gitlabBackend + New() routing, wave 2).
- **Provisional default:** single task (t-5).
- **Why it matters:** this is the flagged sequencing risk — the GitLab client and the `forge.New()` dispatch that returns it must land together (today New() returns ErrNotImplemented and Client is a concrete struct). All three are technically safe (verification keeps gitlabBackend+New() together in t-8), but the interface extraction is speculative structure c-6 doesn't require; a single task is lower-risk and matches the existing concrete-Client shape. Note verification's interface idea as a future refactor if the file proves heavy.

**D4 — Shared GitLab REST helper (auth header + %2F project-ref): folded vs standalone task.**
- risk + mvp: fold the helper into the open task and duplicate it in forge (mirrors the existing splitOwnerRepo duplication across ship/forge).
- verification: standalone internal/ship/gitlab.go task (t-5) with pure-function unit tests.
- **Provisional default:** FOLD (2-1 + matches the in-repo duplicate-not-share precedent), but GRAFT verification's pure-function units (TestGitLabProjectRef, TestGitLabAuthHeader) into t-2's contract.
- **Why it matters:** the %2F encoding + PRIVATE-TOKEN/bearer switch are the bug-prone bits (both locked decisions live here); the cheap deterministic unit tests are worth keeping even without a standalone task. No new shared `gitlab` package is introduced (all three drafts reject that).

**D5 — Doctor: new validation code vs regression test only.**
- risk: t-4 adds auth_scheme-enum validation. verification: doctor already passes a gitlab remote generically — deliverable is a regression test only; c-1 asks only for origin/auth_env validation + non-rejection.
- **Provisional default:** the c-1-faithful regression test (TestDoctorAcceptsGitLabRemote) is MANDATORY; the auth_scheme-enum check is kept as risk's OPTIONAL addition, not gated by c-1.
- **Why it matters:** inventing validation purely to have a code change risks building past the criterion; the honest deliverable is locking in non-rejection.

**D6 — Telemetry classification + defaults.go: in scope at all?**
- risk: telemetry in t-4, defaults.go in t-1. verification: telemetry in t-6. mvp: drops both (no criterion names them).
- **Provisional default:** keep the 1-line telemetry substring ("gitlab backend"→provider bucket) riding on the diagnostics task t-4 (source-confirmed it would otherwise fall to "other"); treat defaults.go as OPTIONAL (auth_scheme defaults to private-token in code, not config).
- **Why it matters:** the telemetry line is cheap and improves observability, but mvp's purity argument is legitimate — so it rides an existing task rather than spawning its own, and the defaults change is only made if a config-level default proves necessary.

**D7 — Config: one task vs two.**
- verification splits into t-1 (struct fields + dotted paths) and t-2 (host detection + doctor). risk + mvp keep detection + config fields in one task.
- **Provisional default:** single config task (t-1).
- **Why it matters:** both halves touch the same project/remote config surface; splitting adds a task without removing any same-file conflict. (Doctor stays in wave-2 t-4 with telemetry, per the skeleton.)
