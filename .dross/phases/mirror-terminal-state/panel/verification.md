# Plan draft — verification lens

Designed backward from the test contracts: for each criterion, the ideal failing
test was written first, then the smallest change that makes it satisfiable.

```
Phase mirror-terminal-state — 7 tasks across 4 waves

Wave 1
  t-1  Exclude dross-authored issues from pull
       files:    internal/board/board.go, internal/cmd/issue.go,
                 internal/cmd/issue_pull_filter_test.go
       covers:   c-1
       contract: an id linked only under board.json's backlog / tasks /
                 milestones is absent from `issue pull --json`; reverting
                 IsLinked to phases+quicks puts it back in .issues
       contract: an unlinked issue carrying the `dross` marker label is absent
                 from the feed; the identical issue without the label is present
       contract: loading THIS repo's .dross/board.json and feeding every id it
                 links back as a ListIssues response leaves the feed empty —
                 the 116→1 claim pinned to the file, not to a count
       contract: a dismissed id and a plain human issue keep today's verdicts —
                 deleting either existing filter clause fails this test

  t-2  Add task-complete lifecycle status
       files:    internal/configenum/configenum.go, internal/forge/jira.go,
                 internal/forge/youtrack.go, internal/cmd/task_lifecycle.go,
                 assets/prompts/ship.md, internal/cmd/task_lifecycle_test.go
       covers:   c-2
       description:
                 New `task-complete` member of LifecycleStatuses, keyed to
                 YouTrack `Verified` and Jira `Done`; ship.md's finalize step
                 gains the `--status task-complete --close` emission; the
                 board→plan inverse gains the one-way terminal entry.
       contract: dropping "task-complete" from either default state map fails
                 TestStateMapsKeyExactlyTheEmittedStatuses; dropping ship.md's
                 `--status task-complete` emission fails
                 TestEmittedStatusesAreTheLifecycleSet — the existing
                 bidirectional guard, now load-bearing for the task lane
       contract: resolveYouTrackState("task-complete", nil) == "Verified" and
                 resolveJiraState("task-complete", nil) == "Done"
       contract: planStatusForLifecycle("task-complete") returns done — a card
                 dragged to the terminal column reads back as a done task
                 instead of as an unmirrored column (taskUnchanged)
       contract: lifecycleForPlanStatus("done") still returns task-in-review —
                 the outbound execute edge is untouched by the new key

Wave 2 (depends t-2)
  t-3  Verify every close against the tracker
       files:    internal/forge/jira.go, internal/cmd/issue.go,
                 internal/cmd/issue_close_truth_test.go
       covers:   c-4
       description:
                 Add JiraClient.CloseIssueAs (state-map write + read-back
                 verify, unmapped status is an error) and route closeBoardIssue
                 through it; `issue quick --close` calls closeBoardIssue instead
                 of the raw client.CloseIssue.
       contract: `issue quick <ref> --close` against a YouTrack fake answering
                 resolved:false exits non-zero and prints no "(closed)" line —
                 today it prints closed unconditionally
       contract: the quick close writes the state map's terminal value
                 (Verified by default, a [board].state_map override when set);
                 the fake's recorded state writes name it
       contract: a Jira board offering no transition to the mapped Done status
                 fails the close; a transition landing on a non-done category
                 fails the read-back rather than reporting success
       contract: a forgejo board still takes the plain CloseIssue path — no
                 state write is attempted and no unmapped-status error is raised

Wave 3 (depends t-3)
  t-4  Close task issues at ship finalize
       files:    internal/cmd/issue_task.go,
                 internal/cmd/issue_task_close_test.go,
                 internal/cmd/ship_prompt_test.go
       covers:   c-2
       description:
                 `task-sync --close`; `--status` validated against
                 configenum.LifecycleStatuses before openBoard; flat providers
                 plainly close.
       contract: `task-sync <phase> --status task-complete --close` writes
                 Verified to every mirrored task issue and reports each closed
                 only after the read-back says resolved; with resolved:false the
                 command exits non-zero and no task prints closed
       contract: after that run, no mirrored task issue carries
                 dross/status:task-in-review — the label set the patch writes
                 carries the terminal status instead
       contract: `task-sync --status bogus` errors naming the LifecycleStatuses
                 list and makes no HTTP call (the fake records zero requests) —
                 the validation happens ahead of openBoard, as phase-sync's does
       contract: on a forgejo board `--close` issues the plain close and writes
                 no state field; it does NOT refuse by provider name the way
                 task-pull does
       contract: ship_prompt guard — `task-sync <phase-id> --status
                 task-complete --close` appears in ship.md AFTER `dross phase
                 complete <phase-id>`; swapping the two lines fails
       depends_on: t-2, t-3

  t-5  Close the milestone epic
       files:    internal/cmd/issue.go, assets/prompts/milestone.md,
                 internal/cmd/issue_milestone_close_test.go
       covers:   c-3
       description:
                 `milestone-sync <version> --close` resolves the milestone
                 entity and closes it through closeBoardIssue when the entity is
                 an issue (epic mode); milestone.md's finalize step emits it.
       contract: on milestone_mode=epic, `milestone-sync v1.5 --close` writes
                 the terminal state to the epic's idReadable and fails when the
                 read-back reads unresolved
       contract: on milestone_mode=version the same call names the mode and
                 exits non-zero — it never prints closed over a version bundle
                 that is not an issue and that no verb can resolve
       contract: `milestone-sync v1.5` without --close still only ensures the
                 link — the fake records no state write, so the ensure path
                 cannot start closing epics by accident
       contract: milestone.md's completion arm carries `dross issue
                 milestone-sync <version> --close` after the `--finalize` call;
                 deleting that line fails the guard naming the epic lane
       depends_on: t-3

  t-6  Close resolved backlog mirrors
       files:    internal/cmd/issue.go, assets/prompts/ship.md,
                 internal/cmd/issue_backlog_close_test.go
       covers:   c-6
       description:
                 syncBacklog computes the live item-key set, then closes and
                 unlinks every board.json backlog key outside it; a routed item
                 whose target phase issue reads resolved leaves the live set.
                 ship.md's finalize step gains the backlog-sync call.
       contract: a board.json holding `slug:x` for a phase that now has a
                 directory → backlog-sync closes that issue and drops the key;
                 the summary line reports closed as its own count
       contract: a `[routed]` item whose target phase issue reads Resolved is
                 closed; the same item whose target issue reads unresolved is
                 updated and left open — the two cases differ only in the
                 target's read-back
       contract: a close the tracker refuses (resolved:false) fails the sync and
                 LEAVES the board.json key in place; an unlinked-but-open mirror
                 would be unreachable by every later run
       contract: a dismissed deferred item is closed and unlinked rather than
                 silently skipped — it leaves the live set the same way a
                 scaffolded slug does
       contract: ship.md's finalize block calls `dross issue backlog-sync
                 <version>`; removing it fails the guard in this file naming the
                 backlog lane as having no prompt edge
       depends_on: t-3

Wave 4 (depends t-1, t-4, t-5, t-6)
  t-7  Guard mirror namespaces and terminal paths
       files:    internal/board/link_namespace_test.go,
                 internal/cmd/mirror_terminal_path_test.go
       covers:   c-5
       description:
                 Two guards: one over board.Board's link namespaces vs
                 IsLinked, one over every lane's terminal path (prompt call
                 site, mapped terminal state, verified close).
       contract: the namespace table is checked by reflection against
                 board.Board's link map fields — adding a sixth link map without
                 a table entry fails by field name, before IsLinked is even
                 called
       contract: each covered namespace populated with a sentinel id makes
                 IsLinked(sentinel) true — reverting IsLinked to phases+quicks
                 fails on backlog, tasks and milestones by name
       contract: every namespace has a close call site in assets/prompts
                 (phase-sync --close, task-sync --close, milestone-sync --close,
                 quick --close, backlog-sync); deleting any one line fails
                 naming the lane left with no terminal path
       contract: every `--close` emission in the prompt corpus that carries a
                 --status passes a LifecycleStatuses member that both default
                 state maps resolve to that provider's terminal value —
                 emitting `--status uat --close` fails
       contract: each close verb run against a fake whose read-back says
                 unresolved exits non-zero; a lane wired to a close path that
                 skips the read-back fails here even when its own tests pass
       depends_on: t-1, t-4, t-5, t-6
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 inbound filter (feed is DRO-133 alone) | t-1 |
| c-2 no task left in task-in-review after ship | t-2, t-4 |
| c-3 milestone epic reaches a resolved state | t-5 |
| c-4 `quick --close` writes the terminal state and verifies | t-3 |
| c-5 guard test for escaped namespace / missing terminal path | t-7 |
| c-6 resolved backlog mirrors close at source | t-6 |

Every criterion has at least one task; no task exists without a criterion.

## Judgment calls

- **t-2 spans six files and cannot be split.** `board_lifecycle_divergence_test.go`
  fails in BOTH directions: a Set member nothing emits, and a map key nothing
  emits. So the `configenum` member, both default state maps, and ship.md's
  emission site must land in one commit or the suite is red between them. Chose
  a single wide task over three narrow red ones.
- **The board→plan inverse for `task-complete` is in scope.** No criterion asks
  for it, but a terminal lifecycle status with no inverse makes `task-pull` read
  a card in the terminal column as `taskUnchanged` — a silent inbound blind spot
  of exactly the class this repo keeps closing. One map entry, one contract.
- **Jira gets its own `CloseIssueAs` (t-3).** Rejected: leaving `closeBoardIssue`
  YouTrack-only. c-4 says the close "writes the state map's terminal state and
  verifies the read-back"; Jira's `CloseIssue` transitions to any done-category
  status, consults no state map, and never reads back — the criterion is simply
  false on Jira boards without it.
- **`quick --close` routed through `closeBoardIssue` rather than given its own
  close logic.** One close seam for five lanes is what makes t-7's "every verb
  fails on an unresolved read-back" contract checkable as a table instead of as
  five bespoke tests.
- **Routed-item resolution is read from the target phase issue's `Resolved`,
  not from local state.** Rejected: `state.json` (machine-local and gitignored,
  so a second machine reaches a different verdict) and plan-task counts (a plan
  can be all-done and unshipped). The tracker's own verdict is the same signal
  c-2 and c-4 already trust.
- **`milestone-sync --close` refuses on version/agile mode instead of
  no-op'ing.** A silent success over a version bundle nothing can resolve is the
  exact false-close shape this phase exists to remove; naming the mode tells the
  user why nothing happened.
- **t-6's ship.md prompt guard lives in `issue_backlog_close_test.go`, not
  `ship_prompt_test.go`.** t-4 owns that file in the same wave; splitting the
  guard by lane keeps two wave-3 tasks off one file.
- **Waves encode dependencies only, not file contention.** t-3, t-5 and t-6 all
  edit `internal/cmd/issue.go`; t-5 and t-6 are same-wave siblings that must be
  executed one after another. That is a serialization detail of a 1300-line
  file, not a dependency, so it does not become an extra wave.
- **The c-1 "116 → 1" number is not asserted as a number.** A test pinning 116
  breaks the next time anything is mirrored. The test instead loads this repo's
  own `board.json` and asserts every id it links is filtered — the mechanism
  behind the number, which stays true as the file grows.
