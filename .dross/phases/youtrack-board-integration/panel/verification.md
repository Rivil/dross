# YouTrack board integration — verification-lens decomposition

Bias: every task is derived from the one test that would fail if the criterion regressed.
For each task the contract names the exact test fn + the asserted surface (endpoint / body field /
round-tripped value), modelled on `forge_test.go`'s httptest mocks, `project_test.go`'s
read/write round-trips, `doctor_test.go`'s gitInit+Init+captureStdout harness, and
`inbox_prompt_test.go`'s prompt-presence greps.

```
Phase youtrack-board-integration — 12 tasks across 4 waves

Wave 1
  t-1  Add [board] config struct + round-trip
       files:    internal/project/project.go, internal/project/board_test.go
       covers:   c-1
       contract: TestProjectBoardRoundTrip — build a Project with Board{Provider:"youtrack",
                 BaseURL, AuthEnv, Project, Enabled:true, MilestoneMode:"version",
                 StateMap:{"shipped":"Fixed"}}, Save then Load to a tmp file; every field
                 incl. the StateMap map must survive. Fails if a toml tag is wrong or
                 milestone_mode/state_map is dropped on encode.

  t-2  Add BoardClient interface + youtrack factory arm
       files:    internal/forge/forge.go, internal/forge/forge_test.go
       covers:   c-2
       contract: TestNewAcceptsYouTrack — New(Config{Provider:"youtrack",BaseURL/APIBase,
                 AuthEnv set}) returns a non-nil client and NOT ErrNotImplemented (mirrors
                 TestNewAcceptsGitLab). Plus a compile-time `var _ BoardClient = (*Client)(nil)`
                 and `var _ BoardClient = (*YouTrackClient)(nil)`, and TestForgeIssueCarriesKey:
                 issueResponse.toIssue() sets Issue.Key = strconv(Number) so the link layer is
                 provider-agnostic. Fails if youtrack still hits the github ErrNotImplemented arm
                 or the concrete Client stops satisfying the interface.

  t-12 Move inbox board gate to [board].enabled
       files:    assets/prompts/inbox.md, internal/cmd/inbox_prompt_test.go
       covers:   c-3
       contract: TestInboxPromptReadsBoardEnabled — normalised assets/prompts/inbox.md contains
                 `dross project get board.enabled` (not `remote.board_sync`). Prompt-only edit, so
                 a grep test; r-01 — `make install` before relying on it live.

Wave 2 (depends t-1, t-2)
  t-3  Add board.* dotted-path read/write arms
       files:    internal/cmd/project.go, internal/cmd/project_test.go
       depends:  t-1
       covers:   c-1
       contract: TestBoardDottedPathsRoundTrip (extends TestExpandedDottedPathsRoundTrip pattern)
                 — writeDotted then readDotted for board.provider, board.base_url, board.auth_env,
                 board.project, board.enabled (bool), board.milestone_mode each round-trip their
                 string form. Fails if an arm is missing so /dross-options can't set the field.

  t-4  Validate [board] in dross doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       depends:  t-1
       covers:   c-1
       contract: TestDoctorValidatesBoardBlock (gitInit+Init+captureStdout) — with board.enabled
                 true: unset $auth_env → "✗ ... auth_env"; provider "bogus" → "✗ ... provider";
                 provider "youtrack" + base_url + set token → a "✓" board line. Fails if doctor
                 ignores [board] or rejects the youtrack provider.

  t-5  Implement YouTrack REST client CRUD
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       depends:  t-2
       covers:   c-2
       contract: httptest mock à la newTestClient: TestYouTrackCreateIssue asserts POST /api/issues
                 body {project:{...PROJ...}, summary, description} and maps resp
                 {"idReadable":"PROJ-7"} → Issue.Key=="PROJ-7"; TestYouTrackListIssues asserts
                 GET /api/issues?query=...&fields=... query scopes the project shortname + open
                 state + passed tag; TestYouTrackGetIssue asserts GET /api/issues/PROJ-7?fields=;
                 TestYouTrackUpdateIssue asserts POST /api/issues/PROJ-7; TestYouTrackBearerAuth
                 asserts Authorization: Bearer <token>. Fails on wrong path/verb/body/header.

  t-6  Store readable issue id; route board to [board]
       files:    internal/board/board.go, internal/board/board_test.go, internal/cmd/issue.go
       depends:  t-1, t-2
       covers:   c-1, c-4
       contract: TestBoardReadableIDRoundTrip — SetPhase("youtrack-x","PROJ-123"); Save+Load;
                 PhaseIssue→"PROJ-123"; IsLinked("PROJ-123") true; IsDismissed round-trips a
                 string id (board.json link values become the readable string identity).
                 TestOpenBoardResolvesFromBoardBlock (cmd) — repo with [remote].provider=github
                 (no board backend) but [board].provider=forgejo + enabled pointing at an httptest
                 server; `dross issue pull` hits the BOARD server, proving no [remote] fallback.
                 Fails if openBoard still reads proj.Remote or links key on int Number.

Wave 3 (depends wave 2)
  t-7  Surface YouTrack issues in issue pull
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       depends:  t-5, t-6
       covers:   c-3
       contract: TestIssuePullYouTrackFiltersLinkedAndDismissed — youtrack httptest returns three
                 open issues (PROJ-12 linked to a phase, PROJ-20 dismissed, PROJ-21 new); board.json
                 seeded with those links; `dross issue pull --labels bug --json` emits only PROJ-21
                 and the upstream GET query carries the bug tag. Fails if linked/dismissed filtering
                 or label passthrough breaks for string ids.

  t-8  Set YouTrack State via state_map, warn-skip
       files:    internal/forge/youtrack.go, internal/cmd/issue.go, internal/forge/youtrack_test.go
       depends:  t-5, t-1
       covers:   c-5
       contract: TestYouTrackSetStateMapsAndUpdates — phase-sync --status shipped on a board whose
                 state_map maps shipped→Fixed sends POST /api/issues/PROJ-7 with a State custom-field
                 value "Fixed" (built-in default map used when state_map empty).
                 TestYouTrackSetStateUnmappedWarnsSkips — target state absent from the project's
                 available States (mock returns a bundle without it): no State write is sent, the
                 issue body/labels sync still succeeds (err==nil), and a warning is printed. Fails if
                 a missing/unmapped state aborts the whole sync.

  t-9  Ensure milestone entity per milestone_mode
       files:    internal/forge/youtrack.go, internal/forge/youtrack_test.go
       depends:  t-5
       covers:   c-4
       contract: three mode tests off one dispatch — TestYouTrackMilestoneVersionMode asserts the
                 project Version bundle value is ensured (GET then POST under
                 /api/admin/projects/<id>); TestYouTrackMilestoneAgileMode asserts a sprint/column
                 ensured via /api/agiles; TestYouTrackMilestoneEpicMode asserts an Epic issue is
                 created (POST /api/issues) and its idReadable returned. Default mode = version.
                 Fails if any mode errors or hits the wrong endpoint.

Wave 4 (depends wave 3)
  t-10 Wire phase-sync + milestone-sync to YouTrack
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       depends:  t-5, t-6, t-9
       covers:   c-4
       contract: TestIssuePhaseSyncYouTrackCreatesThenUpdates — first phase-sync POSTs /api/issues
                 with a body containing the acceptance criteria + a `- [ ]`/`- [x]` task checklist
                 (renderPhaseBody reuse) and stores board.json phases[<phase>]=="PROJ-7"; second
                 sync POSTs /api/issues/PROJ-7 (update) with NO new create. TestIssueMilestoneSync-
                 YouTrack asserts the milestone entity is ensured per milestone_mode and linked in
                 board.json. Fails if a second sync duplicates the issue or skips the milestone link.

  t-11 Sync milestone backlog to YouTrack
       files:    internal/cmd/issue.go, internal/board/board.go, internal/cmd/issue_test.go
       depends:  t-5, t-6, t-9
       covers:   c-6
       contract: TestIssueBacklogSyncYouTrackIdempotent — milestone.phases=["01-done","future-x"]
                 with only 01-done scaffolded (phase.Dir exists) plus one unrouted someday deferred
                 (collectDeferred, Target==""); first run POSTs exactly two backlog items (future-x +
                 the deferred) in Open state attached to the milestone entity and records them in a
                 board.json backlog map; second run POSTs zero new items (idempotent). Fails if a
                 scaffolded slug leaks in, the deferred item is missed, or re-running duplicates.
```

## Coverage
- c-1 (dedicated [board] block, board-only resolution, doctor validation): t-1, t-3, t-4, t-6
- c-2 (YouTrack backend + factory returns a working client): t-2, t-5
- c-3 (issue pull surfaces unlinked/undismissed YouTrack issues for inbox): t-7, t-12
- c-4 (phase-sync issue + board.json readable-id link + milestone entity per mode): t-6, t-9, t-10
- c-5 (phase-sync --status → State custom field via map, warn-skip): t-8
- c-6 (backlog sync of unscaffolded slugs + unrouted someday, idempotent): t-11

## Judgment calls
- forge extend-vs-new-client: chose a new sibling `YouTrackClient` in `internal/forge/youtrack.go`
  behind a `BoardClient` interface the existing concrete `Client` also satisfies; rejected adding
  more `provider==` branches inside `Client` (its int-`Number`/`/repos/`-path/label-id model is
  REST-issue-tracker shaped and fights YouTrack's readable-id + custom-field + `/api/issues` model)
  and rejected a full interface-per-method rewrite (churns every forge method for no test win). The
  interface is the smallest seam that lets `New` return a youtrack client testable with the exact
  httptest pattern c-2 asks for.
- three milestone_modes → tasks: chose ONE task (t-9) with one dispatch method and THREE separate
  test contracts (version/agile/epic), each pinning a distinct endpoint; rejected three tasks (the
  modes share construction + dispatch, so splitting would duplicate scaffolding and the per-mode
  delta is only the endpoint) and rejected folding modes into t-10 (would bury three asserted
  surfaces behind one cmd-level test and blur which mode regressed).
- issue identity: board.json link values become the readable STRING id (locked issue_identity), and
  `Issue.Key` is added so forge backends stringify their int number — done early in t-2/t-6 so every
  downstream contract (pull, phase-sync, backlog) asserts against one identity type instead of
  branching int-vs-string per provider.
- board-only resolution (no [remote] fallback): made the load-bearing contract a github-`[remote]` +
  forgejo-`[board]` repo whose pull hits the board server (t-6) — the single test that fails loudest
  if any fallback to `[remote]` survives.
- prompt vs code for c-3: split the inbox gate (prompt, t-12, grep test) from the pull behaviour
  (code, t-7, httptest) so each half has its own honest failure mode; r-01 flagged on t-12.
```
