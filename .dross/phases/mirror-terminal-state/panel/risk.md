# risk lens — mirror-terminal-state

Every task owns one failure mode. The failure modes, named first, because they drive the graph:

1. The inbound filter reads two of five namespaces (`board.IsLinked` walks `Phases` and `Quicks` only) — and the three it misses hold 117 of this repo's 120 links.
2. Widening it naively blanks the feed: `Tasks` values are `TaskLink` structs whose `Issue` is `""` on every pre-ledger migrated entry, and forge REST issues carry an empty `Key`, so an empty id that matches is a total-exclusion bug.
3. `CloseIssue` is not a verified close. YouTrack's `CloseIssue` hardcodes `CloseIssueAs(key,"complete",nil)` — `nil` override, so `[board].state_map` is ignored; Jira's transitions to a done category and never reads back.
4. `milestone-sync --close` over a non-issue entity is destructive: in version mode `ensureMilestoneLink` returns a version-bundle id / a numeric forge milestone id, and `CloseIssue(id)` on a REST forge closes **issue #id** — an unrelated card.
5. `task-sync --close` refused by provider name (the shape `task-pull` uses) strands every task card on Forgejo/Gitea/GitLab forever — the exact defect this phase exists to fix. Locked: it must plainly close there.
6. Backlog reconcile by set-difference closes other milestones' mirrors: `board.Backlog` is global, `backlog-sync` takes one version, and `slug:` keys carry no version.
7. A partial close (task 2 of 3 fails) that still writes board.json / prints success is a false green on the board.
8. A new mirror namespace, or a new lifecycle status with no terminal path, escapes silently — the class this phase is closing, one lane at a time, months late.

```
Phase mirror-terminal-state — 7 tasks across 4 waves

Wave 1
  t-1  Exclude every mirror namespace from pull
       files:    internal/board/board.go, internal/cmd/issue.go,
                 internal/board/board_test.go, internal/cmd/issue_test.go
       covers:   c-1
       desc:     IsLinked walks Milestones, Phases, Quicks, Backlog and Tasks
                 (Tasks by .Issue, not the struct), never matching an empty id;
                 collectInbound additionally drops any issue carrying labelMarker.
       contract: - a board.json whose ONLY link to PROJ-9 is a `tasks` entry makes
                   `issue pull` omit PROJ-9; reverting IsLinked to phases+quicks
                   fails TestPullExcludesEveryNamespace (one subtest per namespace,
                   so a namespace dropped from the walk names itself)
                 - an issue carrying the `dross` label and linked in no namespace
                   is omitted — deleting the marker post-filter in collectInbound
                   fails TestPullExcludesMarkerLabelledIssue
                 - a board.json holding an empty task issue id (the migrated
                   TaskLink shape) plus a forge issue whose Key is "" leaves that
                   issue IN the feed: TestEmptyIdNeverMatches fails if IsLinked("")
                   returns true, which would blank the whole feed
                 - a fixture board.json at this repo's shape (3 milestone, 22 phase,
                   3 quick, 94 backlog, 20 task links, 6 dismissed) over a 120-issue
                   fake leaves exactly the one unlinked, unlabelled issue:
                   TestPullFeedIsOnlyForeignIssues asserts len(feed)==1 by key

  t-2  Make closeBoardIssue the one verified close
       files:    internal/cmd/issue.go, internal/cmd/issue_close_truth_test.go,
                 internal/cmd/issue_quick_close_test.go
       covers:   c-4
       desc:     closeBoardIssue writes the [board].state_map terminal state and
                 re-reads the issue, failing unless the tracker reports it
                 resolved (YouTrack via CloseIssueAs, Jira and the REST forges via
                 GetIssue read-back). `issue quick --close` routes through it
                 instead of calling client.CloseIssue directly.
       contract: - `issue quick --close` against a YouTrack fake whose read-back
                   reports resolved:false exits non-zero and prints no "(closed)"
                   line — TestQuickCloseFailsWhenTheIssueStaysUnresolved
                 - a [board].state_map override of `complete` reaches the quick
                   close: the fake records the OVERRIDDEN State value, not the
                   built-in "Verified" — TestQuickCloseHonoursStateMap fails while
                   the nil-override CloseIssue path is used
                 - a Jira quick close whose transition list has no done-category
                   entry exits non-zero — TestQuickCloseJiraWithoutDoneTransition
                 - the phase path is unchanged: the existing
                   TestPhaseSyncClose* suite in issue_close_truth_test.go stays
                   green, including TestPhaseSyncWithoutCloseKeepsTheLenientPath

Wave 2 (depends t-2)
  t-3  Add task-sync --status validation and --close
       files:    internal/cmd/issue_task.go, internal/cmd/issue.go,
                 internal/cmd/issue_task_close_test.go
       covers:   c-2
       desc:     task-sync validates --status against configenum.LifecycleStatuses
                 before openBoard and normalizes it (phase-sync's shape); a new
                 --close flag drives each synced task through closeBoardIssue, and
                 on forgejo/gitea/gitlab plainly closes rather than refusing by
                 name. A failed close names the task and fails the command.
       depends:  t-2
       contract: - `task-sync <phase> --status bogus` exits non-zero naming
                   configenum.LifecycleStatuses.List() and makes no tracker call
                   at all (the fake records zero requests) —
                   TestTaskSyncValidatesStatusBeforeTouchingTheBoard
                 - `task-sync <phase> --status " Task-In-Review"` reaches the state
                   map: the fake sees label "dross/status:task-in-review", not the
                   padded form — TestTaskSyncNormalizesStatus
                 - on provider=forgejo, `task-sync <phase> --close` issues one
                   close per task and prints no provider-refusal message —
                   TestFlatBoardTaskCloseClosesEveryCard fails if task-pull's
                   refuse-by-name shape is copied here
                 - with three tasks where the SECOND close read-back reports
                   unresolved: the command exits non-zero, the message names that
                   task id, the third task is still attempted, and board.json
                   records no agreement point for the failed one —
                   TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing

  t-4  Close the milestone epic, entity-gated
       files:    internal/cmd/issue.go, assets/prompts/milestone.md,
                 internal/cmd/issue_milestone_close_test.go
       covers:   c-3
       desc:     milestone-sync gains --close: it resolves the milestone entity,
                 and closes only when that entity is an ISSUE (YouTrack epic mode).
                 A version bundle, an agile board or a forge milestone id is
                 refused by name, closing nothing. milestone.md's §8 finalize step
                 emits the command after `dross milestone complete --finalize`.
       depends:  t-2
       contract: - in epic mode, `milestone-sync v0.1 --close` writes the mapped
                   resolved State to the epic's idReadable and verifies the
                   read-back — TestEpicCloseResolvesForReal
                 - in version mode, and on a forgejo board whose milestone id is
                   the numeric milestone 7, --close makes NO close request (the
                   fake fails the test if any close/transition endpoint is hit)
                   and exits non-zero naming the mode —
                   TestMilestoneCloseRefusesANonIssueEntity; without the gate this
                   test closes forge issue #7
                 - an epic whose read-back still reads unresolved fails the command
                   and prints no closed line — TestEpicCloseFailsWhenUnresolved
                 - milestone.md §8 contains `dross issue milestone-sync <version>
                   --close` positioned after the `--finalize` step; deleting or
                   reordering it fails TestMilestonePromptClosesTheEpicAtFinalize

  t-5  Reconcile and close resolved backlog mirrors
       files:    internal/cmd/issue.go, assets/prompts/ship.md,
                 internal/cmd/issue_backlog_close_test.go
       covers:   c-6
       desc:     syncBacklog, after pushing the live set, walks board.BacklogKeys()
                 and closes each recorded mirror whose artefact is provably
                 resolved — a `slug:` whose phase directory now exists, a deferred
                 item that is dismissed, a `[routed]` item whose target phase issue
                 reads Resolved on the tracker — dropping the key only after a
                 verified close. A key it cannot attribute is warned about and left
                 open. ship.md's finalize step runs `dross issue backlog-sync`.
       depends:  t-2
       contract: - a `slug:x` mirror whose phase dir now exists is closed and its
                   board.json key dropped, while the issue for a still-unscaffolded
                   slug stays open — TestBacklogClosesScaffoldedSlugOnly
                 - a `[routed]` mirror is closed when lookupPhaseIssue's target
                   issue reads Resolved and left open when it reads unresolved —
                   TestRoutedBacklogClosesOnlyWhenTargetResolved
                 - a recorded key for ANOTHER milestone's slug (absent from this
                   version's live set, phase not scaffolded) is left open and
                   warned about — TestUnattributableBacklogKeyIsNeverClosed fails
                   if the reconcile closes by set-difference alone
                 - a close whose read-back fails leaves the board.json key in place
                   so the next run retries — TestFailedBacklogCloseKeepsTheLink
                 - ship.md's finalize section calls `dross issue backlog-sync`;
                   removing the line fails TestShipPromptReconcilesTheBacklog

Wave 3 (depends t-3)
  t-6  Introduce task-complete and emit it at finalize
       files:    internal/configenum/configenum.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, assets/prompts/ship.md,
                 internal/cmd/ship_prompt_test.go
       covers:   c-2
       desc:     Adds `task-complete` to LifecycleStatuses, maps it to YouTrack
                 `Verified` and Jira `Done`, and emits `dross issue task-sync
                 <phase-id> --status task-complete --close` from ship.md's §6
                 finalize, after `dross phase complete`. All three land together:
                 the existing divergence guard is red with any one of them missing.
       depends:  t-3
       contract: - dropping the `task-complete` key from defaultYouTrackStateMap or
                   defaultJiraStateMap fails the existing
                   TestStateMapsKeyExactlyTheEmittedStatuses for that provider
                   alone (the two maps are checked independently)
                 - adding the Set member without the ship.md line fails the existing
                   TestEmittedStatusesAreTheLifecycleSet with "in the Set but
                   nothing emits it"
                 - ship.md §6 emits the task close AFTER `dross phase complete`
                   and before §7 wrap; moving it above the phase close fails
                   TestShipPromptClosesTaskCardsAfterPhaseComplete
                 - the execute-edge pairing is untouched: the existing
                   TestTaskEdgesPairTheBoardStatusWithThePlanStatus stays green,
                   i.e. `task-complete` never appears at a per-task execute edge

Wave 4 (depends t-1, t-4, t-5, t-6)
  t-7  Guard namespace and terminal-path escapes
       files:    internal/cmd/board_mirror_guard_test.go,
                 internal/cmd/board_lifecycle_divergence_test.go
       covers:   c-5
       desc:     Two guards. One reflects over board.Board's json-tagged link maps,
                 plants a sentinel id in each, and asserts the inbound filter omits
                 it — a namespace added to the struct and not to the filter fails
                 by field name. The other asserts every mirror lane has a --close
                 emission in assets/prompts and every terminal lifecycle status
                 maps, in BOTH provider maps, to a state that tracker reports
                 resolved.
       depends:  t-1, t-4, t-5, t-6
       contract: - adding a field `Epics map[string]string` to board.Board and not
                   wiring it into IsLinked fails TestEveryBoardNamespaceIsFiltered
                   with "Epics" in the message (the guard enumerates by reflection,
                   never by a hand-written list — a hand-written list is the same
                   manual step that produced this phase)
                 - a LifecycleStatuses member whose state-map value is not one the
                   provider treats as resolved (e.g. mapping `task-complete` to
                   "In Review") fails TestEveryTerminalStatusResolves, per provider
                 - deleting the `--close` emission from ship.md (phase, task or
                   backlog) or from milestone.md fails
                   TestEveryMirrorLaneHasACloseEmission, naming the lane whose
                   cards would now strand
                 - the guard scans a non-empty prompt corpus and a non-empty
                   namespace set: an empty scan t.Fatal's rather than passing
                   vacuously (the failure mode the existing divergence guard
                   already defends with its `scanned == 0` check)
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3, t-6 |
| c-3 | t-4 |
| c-4 | t-2 |
| c-5 | t-7 |
| c-6 | t-5 |

## Judgment calls

- **One shared verified-close seam (t-2) in wave 1, not three per-lane closes.** Rejected letting task, milestone and backlog each call `client.CloseIssue`: that is precisely how the phase lane ended up the only one that verifies, and it re-opens the `nil` state-map override on every new lane.
- **Empty ids never match in IsLinked.** Rejected the literal widening: `Tasks` holds `TaskLink` structs whose `Issue` is `""` for every pre-ledger migrated entry, and forge REST issues carry an empty `Key` — the naive version excludes the entire feed, which reads exactly like a working filter.
- **`milestone-sync --close` gated on the entity being issue-shaped.** Rejected an unconditional close mirroring the phase path: in version mode the id is a version bundle / numeric forge milestone id, and `CloseIssue("7")` on Forgejo closes issue #7. A refusal by name is the only safe non-epic behaviour, and it does not contradict `flat_board_close` — that decision is about task CARDS, which are issues everywhere.
- **Backlog closure is attributed, not set-difference.** Rejected "close every recorded key absent from this version's live set": `board.Backlog` is global while `backlog-sync` takes one version and `slug:` keys carry no version, so absence would close a sibling milestone's mirrors. Each closure names its reason — scaffolded / dismissed / target resolved — and an unattributable key is warned and left.
- **Routed closure reads the target phase ISSUE's Resolved flag, not `.dross/state.json`.** Rejected the completion record: it is gitignored and machine-local, so on any other machine a routed mirror would never close and the bug would look fixed on the author's laptop only.
- **`task-complete` (enum + both maps + ship.md line) is one task, not three.** Rejected splitting: `TestEmittedStatusesAreTheLifecycleSet` fails the build for a Set member nothing emits, so any split leaves a red commit between them — and this repo's rules forbid committing behind an unobserved gate.
- **Guards last (wave 4), not test-first.** Rejected writing t-7 first: both guards assert across every lane, so they would be red for the whole phase and stop distinguishing "not built yet" from "regressed".
- **`--close` on task-sync errors rather than warns, per task, and keeps going.** Rejected aborting on the first failure (hides the other cards' state) and rejected warn-and-continue (a printed close over an unresolved card is the c-4 lie wearing a task-shaped hat).
