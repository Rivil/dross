# MVP decomposition — gitlab-ship-provider

Bias: smallest task set that satisfies every criterion. Every task traces to a
criterion; the ship-package REST surface (open + comment + reviewers) is merged
into one task because all three live behind the same provider dispatch and all
touch `cmd/ship.go`; splitting them would create a same-file wave-2 conflict for
no gain. No telemetry/defaults work — neither is named by any criterion.

```
Phase gitlab-ship-provider — 4 tasks across 2 waves

Wave 1
  t-1  Recognize gitlab: detect, config, doctor
       files:    internal/project/project.go, internal/project/remote.go,
                 internal/cmd/project.go, internal/project/remote_test.go,
                 internal/cmd/project_test.go, internal/cmd/doctor_test.go
       covers:   c-1
       contract: if remote.go drops the gitlab case, DetectRemote("https://gitlab.com/o/r")
                 no longer yields Provider="gitlab" + APIBase="https://gitlab.com/api/v4"
                 and the remote_test detection case fails; if remote.auth_scheme /
                 remote.project_id aren't added to readDotted/writeDotted, the
                 project_test set-then-get round-trip for those two keys fails; if doctor
                 ever rejects an unsupported provider, a doctor_test with provider="gitlab"
                 + matching origin + set auth_env reports issues>0 instead of passing.

  t-2  Add GitLab steps to ship.md
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-4
       contract: if ship.md §5 loses the GitLab pipeline poll
                 (GET /api/v4/projects/<id>/pipelines?sha=) or §6 loses the GitLab
                 squash-merge (PUT .../merge_requests/<iid>/merge with squash=true +
                 should_remove_source_branch=true), TestShipPromptGitLabSections fails;
                 it also asserts the ambiguous-state branch (manual / skipped /
                 no-pipeline-for-SHA → ask the user, don't guess).

Wave 2 (depends t-1)
  t-3  GitLab ship REST surface (MR, reviewers, comment)
       files:    internal/ship/open.go, internal/ship/comment.go,
                 internal/cmd/ship.go, internal/ship/open_test.go,
                 internal/ship/comment_test.go
       covers:   c-2, c-3, c-5
       depends:  t-1
       contract: c-2 — if openGitLabPR posts the wrong endpoint or omits the body, the
                 httptest mock asserting POST /api/v4/projects/<urlencoded owner/repo>/merge_requests
                 with source_branch/target_branch/title fails; if Draft doesn't prepend
                 "Draft:" to the title the draft test fails; if the PRIVATE-TOKEN header is
                 absent (or auth_scheme="bearer" doesn't switch to Authorization: Bearer),
                 the auth-header assertion fails; if the returned MR web_url isn't surfaced,
                 the result-URL assertion fails.
                 c-5 — if the username→id lookup (GET /api/v4/users?username=) or the
                 reviewer_ids field on the MR is wrong, the reviewer test fails; if a 404 on
                 lookup/assignment aborts the ship instead of warning, the non-fatal test
                 fails (MR still returned, error reported).
                 c-3 — if postGitLabComment hits anything but
                 POST /api/v4/projects/<id>/merge_requests/<iid>/notes with body {"body":...},
                 the comment httptest assertion fails.

  t-4  GitLab forge client for board sync
       files:    internal/forge/forge.go, internal/cmd/issue.go,
                 internal/forge/forge_test.go, internal/cmd/issue_test.go
       covers:   c-6
       depends:  t-1
       contract: if forge.New(Config{Provider:"gitlab"}) still returns ErrNotImplemented,
                 the gitlab TestNewValidation case (now expecting a live client) fails; if
                 CreateIssue/UpdateIssue/CloseIssue/GetIssue key on a global number instead
                 of the project-relative iid, the httptest asserting
                 /api/v4/projects/<enc>/issues/<iid> fails; if EnsureMilestone uses the wrong
                 path the /api/v4/projects/<enc>/milestones test fails; if the PRIVATE-TOKEN
                 header (default) isn't sent, the auth assertion fails; if issue enable
                 rejects gitlab, the issueEnable gitlab test (expects no "no board backend"
                 note) fails.
```

## Coverage

| criterion | tasks |
|-----------|-------|
| c-1 | t-1 |
| c-2 | t-3 |
| c-3 | t-3 |
| c-4 | t-2 |
| c-5 | t-3 |
| c-6 | t-4 |

All of c-1..c-6 accounted for.

## Judgment calls

- Merged c-2 + c-3 + c-5 into one task (t-3): all live in the `internal/ship`
  package behind the same provider dispatch and all require wiring new
  OpenOpts/CommentOpts fields through `cmd/ship.go`. Rejected three separate
  wave-2 tasks because two of them would edit `cmd/ship.go` concurrently — a
  same-file conflict that buys nothing.
- c-5 stays inside openGitLabPR rather than its own task: reviewer-id lookup
  mutates the same MR create flow; a separate task would mean two tasks editing
  one function. Rejected splitting.
- t-2 (prompt) placed in wave 1, not wave 2: ship.md is pure markdown about REST
  endpoints and its presence test reads strings — it has no dependency on the Go
  struct fields t-1 adds. Rejected dropping it to wave 2 "to be safe"; that would
  serialize work that is genuinely independent.
- New `auth_scheme` + `project_id` fields land in t-1 (the config task), not in
  t-3/t-4: they are `[remote]` config leaves, so they belong with detection and
  dotted-path plumbing. This is why t-3 and t-4 depend on t-1 — `cmd/ship.go` and
  `cmd/issue.go` won't compile referencing `p.Remote.AuthScheme` until the field
  exists.
- Dropped telemetry classification (telemetry.go ~271-274) and defaults
  (RemoteDefaults auth_scheme): no criterion names either. A "gitlab backend"
  error falling into the `other` bucket is cosmetic; adding it is speculative
  structure the MVP lens cuts.
- Kept c-6 as a single task despite forge.go being the heaviest change: it is one
  coherent layer (the forge client backend) plus a one-line enable switch — 2
  files, within the too-large threshold (5+ files / >2 layers). Rejected
  pre-emptively splitting the forge backend; if it proves heavy in execution,
  the natural seam is milestones-vs-issues, but that split isn't justified up
  front.
- Followed the existing duplicate-not-share pattern: the GitLab auth-header +
  project-path-encoding helpers are written independently in `internal/ship` and
  `internal/forge` (which already duplicate splitOwnerRepo) rather than inventing
  a shared `gitlab` package.
