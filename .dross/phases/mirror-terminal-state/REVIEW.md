# Plan Review — mirror-terminal-state

Reviewed: 2026-08-20 (pass 2)
Plan: 7 tasks across 4 waves

## BLOCKING

- [antipatterns] t-7's terminal-distinctness guard is red before `task-complete` even exists.
  The contract says: "every LifecycleStatuses member the prompts emit with `--close` must map,
  in BOTH provider maps, to a state that no non-terminal member maps to". `complete` is emitted
  with `--close` (ship.md:155); `shipped` is emitted without one (ship.md:153), so it is
  non-terminal. `internal/forge/jira.go:419` maps BOTH to `"Done"`. The guard therefore fails on
  day one, for a collision this phase neither introduced nor is scoped to fix — and fixing it
  would mean re-mapping `shipped` in jira.go, a file t-7 does not list and no criterion covers.
  (YouTrack is clean: `Fixed` vs `Verified`.)
  Suggestion: narrow the assertion to what c-5 actually asks for and what the lock cares about —
  the terminal status of a lane must differ from the non-terminal statuses OF THAT LANE
  (`task-complete` != `task-in-review`/`task-in-progress`) — or carry an explicit, commented
  exemption for the pre-existing (shipped, complete) Jira pair. Do not silently widen t-7's file
  list to include jira.go.

## FLAG

- [antipatterns] t-7's mirror-lane enumeration omits the quicks lane. The contract names phase,
  task, milestone and backlog; `assets/prompts/quick.md:199` already emits
  `dross issue quick $NEW_VERSION --close`, and quicks is one of the five namespaces c-1 and the
  t-7 reflection guard both treat as a mirror namespace. As contracted, deleting that line strands
  every quick card and no guard notices. Worse, the lane list is hand-written — the exact weakness
  the sibling namespace guard deliberately avoids by reflecting over `board.Board`.
  Suggestion: derive the lane set from the same namespace enumeration the first guard uses, so the
  two guards cannot disagree about how many lanes exist; add quicks to the expected set.

- [antipatterns] GitHub is a fourth flat board and appears nowhere in the plan.
  `configenum.BoardProviders` (internal/configenum/configenum.go:75) includes `"github"`, and
  `githubIssue.toIssue` (internal/forge/github.go:329) populates `State` and never `Resolved` —
  identical to the REST forges. t-2 and t-3 describe the flat path as "forgejo/gitea/gitlab"
  throughout, and the `flat_board_close` lock names only those three. If the implementation
  branches on a three-name allowlist rather than on "not YouTrack, not Jira", a GitHub board either
  demands `Resolved` (and strands every card — the exact defect) or is refused by name.
  Suggestion: state that the flat path is by exclusion, and add a github subtest to
  TestFlatBoardCloseVerifiesOnStateNotResolved. If GitHub is deliberately out of scope, say so.

- [test-contract] t-3 never pins `task-sync --close` with no `--status`. `closeBoardIssue`
  (internal/cmd/issue.go:941) defaults `status == ""` to `"complete"`, so on YouTrack/Jira a
  bare `--close` writes the PHASE lane's terminal state onto task cards — precisely the collision
  the `task_terminal_status` lock exists to prevent ("reusing `complete` would collide with the
  phase issue's own label"). t-3's only `--close`-without-`--status` case is forgejo, which writes
  no state, so the contract cannot catch it.
  Suggestion: either make `task-sync --close` require `--status` (fails cleanly until t-6 lands
  `task-complete`), or pin the default explicitly in a test. Do not leave it to the
  `closeBoardIssue` default.

- [test-contract] t-5's last contract clause is not assertable. "a phase spec carrying no milestone
  leaves the step unemitted rather than passing an empty argument" describes what an agent does at
  runtime while reading ship.md; a prompt-corpus test can only assert the markdown's text.
  TestShipPromptReconcilesTheBacklog can prove the line exists and that it derives `<version>` from
  `[phase].milestone`; it cannot prove the no-milestone branch.
  Suggestion: drop that half, or move the resolution into Go (`backlog-sync --phase <id>`, which
  self-resolves the version and no-ops on a milestone-less phase) — which would also make the
  behaviour greppable and testable rather than prose in a prompt.

- [antipatterns] t-6's contract edits a file t-6 does not list. "state_map_test.go's distinctness
  pairs gain `task-complete` != `task-in-review`" refers to `internal/forge/state_map_test.go`
  (its `distinctPairs` table, line 27); t-6's `files` list has configenum.go, youtrack.go, jira.go,
  task_lifecycle.go, ship.md, ship_prompt_test.go, task_lifecycle_test.go.
  Suggestion: add `internal/forge/state_map_test.go` to t-6's files.

- [test-contract] t-1's id-space gate is described inaccurately and its predicate is never named.
  `YouTrackClient.ensureVersion` returns the version NAME (e.g. "v1.5"), and agile mode returns the
  board name — so "under version/agile mode … the value is a numeric milestone id" is wrong; only
  the REST forges and GitHub store a numeric milestone id, which is the real `"7"` collision (and
  it is real: `issueResponse.toIssue` sets `Key: strconv.Itoa(r.Number)`, so forge issue #7 and
  milestone 7 are the same string). Separately, `IsLinked` lives in package `board`, which has no
  access to `[board].milestone_mode`, so "mode-aware" can only mean shape inference — and the plan
  never says which shape ("contains a dash" would be a different, worse rule than
  `^[A-Za-z]+-\d+$`).
  Suggestion: name the predicate in the description, and fix the version/agile claim. Add the
  YouTrack version-name case ("v1.5") to the contract alongside "7"; both must be unlinked, and
  they fail differently.

- [wave-order] Wave 2 is nominally parallel and actually serial. t-3, t-4 and t-5 all edit
  `internal/cmd/issue.go`, so no two can run concurrently — and t-4's own description concedes its
  t-2 edge is "the shared closeBoardIssue seam plus contention on internal/cmd/issue.go rather than
  a capability need … this task could run in wave 1 if the file were split". By the strict reading,
  t-4 does not belong in wave 2. The disclosure is honest but the wave numbers now carry no
  parallelism information.
  Suggestion: either accept that waves here mean "commit order, not concurrency" and say so once in
  the plan, or split the milestone-close path out of issue.go so t-4 can genuinely join wave 1.

- [granularity] t-6 touches 7 files across 4 layers (configenum, forge, cmd, assets) — a split
  candidate by the rule.
  Suggestion: leave it. The atomicity argument in the description is correct: the existing
  TestEmittedStatusesAreTheLifecycleSet / TestStateMapsKeyExactlyTheEmittedStatuses pair is red
  with any one part missing, so a split produces a knowingly-broken intermediate commit. Recording
  the flag, not endorsing it.

## NOTE

- [granularity] t-6 adds a one-way `task-complete -> done` entry to `boardStatusToPlan`, which
  `internal/cmd/task_lifecycle.go:41` currently builds by inverting `taskLifecycle` precisely so
  "the two cannot disagree". A hand-added entry retires that invariant; the comment should be
  updated to say why the terminal status is asymmetric rather than left claiming a pure inversion.

- [test-contract] `internal/forge/forge.go:218` claims "forge backends leave this empty until the
  string-id migration", but both `issueResponse.toIssue` and `gitlabIssueResponse.toIssue` populate
  `Key`. t-1's TestEmptyIdNeverMatches is still worth having as a guard, but no live backend
  produces an empty Key today — the test's premise is defensive, not observed.

- [forbidden-actions] No global `~/.claude/dross/rules.toml` exists; the only project rule is r-01
  (`make install` before relying on a prompt or binary change). No task implies a violation, and
  t-7 explicitly reasons about it for its two guards.

- [strengths] Test contracts are inversion-shaped throughout — "deleting the marker clause in
  collectInbound fails TestPullExcludesMarkerLabelledIssue", "reverting IsLinked to phases+quicks
  fails on backlog, tasks and milestones by name". They name the test AND the failure mode, which
  is what makes them checkable rather than aspirational.

- [strengths] t-1 refuses to pin c-1's headline number: it asserts the reduction against this
  repo's real board.json instead of the literal "116". That is the right call — 116 is a fact about
  a live tracker at one moment, and pinning it would make the suite fail on the next board change.

- [strengths] t-2's split verification verdict is derived from what each client actually populates,
  not assumed: YouTrack (`youtrack.go:804/832`) and Jira (`jira.go:561`) set `Resolved`; the REST
  client sets only `State`. Verifying flat boards on `State == "closed"` is the only reading that
  does not strand every card, and it matches the `flat_board_close` lock.

## Summary
One blocking defect — t-7's global terminal-distinctness guard is unsatisfiable against the
existing Jira map's shipped/complete collision — plus a missing quicks lane, an unhandled GitHub
board, and an unpinned `task-sync --close` default; otherwise the strongest-contracted plan in this
repo's recent history.
