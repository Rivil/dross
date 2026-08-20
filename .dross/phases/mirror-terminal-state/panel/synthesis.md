# Panel synthesis — mirror-terminal-state

Judged cold: none of the three drafts is mine. Every referenced path was checked
against the tree — `internal/board/board.go` (`IsLinked` walks Phases+Quicks only),
`internal/cmd/issue.go` (`closeBoardIssue` verifies on YouTrack only; `quick --close`
calls `ctx.client.CloseIssue` raw at issue.go:1022), `internal/cmd/issue_task.go`
(no `--close`, no `--status` validation), `internal/cmd/task_lifecycle.go`,
`internal/configenum/configenum.go` (no `task-complete`),
`internal/cmd/board_lifecycle_divergence_test.go` (the named guards exist),
`assets/prompts/{ship,milestone}.md`. One nit: verification cites
`internal/cmd/task_lifecycle_test.go`, which does not exist yet — it is a new file,
not a wrong path.

## Scores

| Draft | Dimension | Score | One line |
|---|---|---|---|
| risk | criteria coverage | 5/5 | All six covered; c-2 split across the flag (t-3) and the vocabulary+emission (t-6), which is where the real work sits. |
| risk | test-contract specificity | 5/5 | Contracts name the test, the fake's recorded call, and the mutation that reddens it — "reverting IsLinked to phases+quicks fails X". |
| risk | granularity | 4/5 | Seven right-sized tasks; t-5 (backlog reconcile with three attribution rules) is the one task carrying more than one idea. |
| risk | wave correctness | 5/5 | Shared close seam (t-2) lands in wave 1 before every lane that routes through it; guards last so they distinguish "not built" from "regressed". |
| mvp | criteria coverage | 4/5 | All six covered, but c-2's inbound half (a card in the terminal column) is unaddressed and c-3's non-epic path is a no-op. |
| mvp | test-contract specificity | 3/5 | Real assertions, but run-on prose per task; no contract for a partial close, none for an empty-id match, none for the milestone entity type. |
| mvp | granularity | 3/5 | Five tasks; t-2 spans enum + two forge maps + cmd + prompt + two test files, and t-3 merges two criteria into one function edit. |
| mvp | wave correctness | 2/5 | t-2 (`task-sync --close`) and t-3 (the verified `closeBoardIssue` seam) are wave-1 siblings, yet t-2's close must route through t-3's seam — an undeclared dependency. |
| verification | criteria coverage | 5/5 | All six, plus the board→plan inverse for `task-complete` that neither other draft noticed. |
| verification | test-contract specificity | 5/5 | Most test-shaped of the three: 3–5 discrete contracts per task, each naming the mutation it catches; pins c-1 to the repo's real board.json rather than to "116". |
| verification | granularity | 5/5 | Seven tasks, each one edit surface; the only wide task (t-2) is wide because the divergence guard forces it. |
| verification | wave correctness | 3/5 | Correct dependency edges, but ship.md's `--status task-complete --close` emission lands in wave 1 (t-2) while the `--close` flag it names arrives in wave 3 (t-4) — two waves where the prompt emits a flag the CLI rejects. |

**Skeleton: risk.** It is the only draft whose wave order is defensible end to end —
the one verified-close seam lands before the four lanes that consume it, and the
`task-complete` emission lands after the flag it names exists. It is also the only
draft that plans against the two failure modes that would ship a *green-looking*
regression: an empty id matching in `IsLinked` (which blanks the entire feed and
reads exactly like a working filter) and `CloseIssue("7")` on a numeric forge
milestone id (which closes an unrelated issue #7). verification scores equal or
higher on coverage and contract specificity, and its contracts are grafted in
wholesale below; mvp contributes two contracts and loses on wave correctness.

## Merged plan

```
Phase mirror-terminal-state — 7 tasks across 4 waves

Wave 1
  t-1  Exclude every mirror namespace from pull                    [risk skeleton]
       files:    internal/board/board.go, internal/cmd/issue.go,
                 internal/board/board_test.go, internal/cmd/issue_test.go
       covers:   c-1
       desc:     IsLinked walks Milestones, Phases, Quicks, Backlog and Tasks
                 (Tasks by .Issue, not the struct), never matching an empty id;
                 collectInbound additionally drops any issue carrying labelMarker.
       contract: - a board.json whose ONLY link to PROJ-9 is a `tasks` entry makes
                   `issue pull` omit PROJ-9; reverting IsLinked to phases+quicks
                   fails TestPullExcludesEveryNamespace (one subtest per namespace,
                   so a namespace dropped from the walk names itself)          [risk]
                 - an issue carrying the `dross` label and linked in no namespace is
                   omitted; the identical issue without the label is present —
                   deleting the marker clause in collectInbound fails
                   TestPullExcludesMarkerLabelledIssue        [risk+verification]
                 - a board.json holding an empty task issue id (the migrated
                   TaskLink shape) plus a forge issue whose Key is "" leaves that
                   issue IN the feed: TestEmptyIdNeverMatches fails if IsLinked("")
                   returns true, which would blank the whole feed              [risk]
                 - loading THIS repo's .dross/board.json and feeding every id it
                   links back as a ListIssues response leaves the feed empty — the
                   116→1 claim pinned to the file, not to a count      [verification]
                 - a dismissed id and a plain human issue keep today's verdicts —
                   deleting either existing filter clause fails this test
                                                                       [verification]

  t-2  Make closeBoardIssue the one verified close                  [risk skeleton]
       files:    internal/cmd/issue.go, internal/forge/jira.go,
                 internal/cmd/issue_close_truth_test.go,
                 internal/cmd/issue_quick_close_test.go
       covers:   c-4
       desc:     closeBoardIssue writes the [board].state_map terminal state and
                 re-reads the issue, failing unless the tracker reports it resolved
                 (YouTrack via CloseIssueAs; Jira via its state-map write plus a
                 GetIssue read-back; REST forges via plain CloseIssue + read-back).
                 `issue quick --close` routes through it instead of calling
                 client.CloseIssue directly.
       contract: - `issue quick --close` against a YouTrack fake whose read-back
                   reports resolved:false exits non-zero and prints no "(closed)"
                   line — TestQuickCloseFailsWhenTheIssueStaysUnresolved
                                                             [risk+mvp+verification]
                 - a [board].state_map override of `complete` reaches the quick
                   close: the fake records the OVERRIDDEN State value, not the
                   built-in "Verified" — TestQuickCloseHonoursStateMap fails while
                   the nil-override CloseIssue path is used    [risk+mvp+verification]
                 - a Jira board offering no transition to the mapped Done status
                   fails the close; a transition landing on a non-done category
                   fails the read-back rather than reporting success   [verification]
                 - a forgejo board still takes the plain CloseIssue path — no state
                   write is attempted and no unmapped-status error is raised
                                                                       [verification]
                 - the phase path is unchanged: the existing TestPhaseSyncClose*
                   suite in issue_close_truth_test.go stays green, including
                   TestPhaseSyncWithoutCloseKeepsTheLenientPath                [risk]

Wave 2 (depends t-2)
  t-3  Add task-sync --status validation and --close               [risk skeleton]
       files:    internal/cmd/issue_task.go, internal/cmd/issue.go,
                 internal/cmd/issue_task_close_test.go
       covers:   c-2
       depends:  t-2
       desc:     task-sync validates --status against configenum.LifecycleStatuses
                 before openBoard and normalizes it (phase-sync's shape); a new
                 --close flag drives each synced task through closeBoardIssue, and
                 on forgejo/gitea/gitlab plainly closes rather than refusing by
                 name. A failed close names the task and fails the command.
       contract: - `task-sync <phase> --status bogus` exits non-zero naming
                   configenum.LifecycleStatuses.List() and makes no tracker call at
                   all (the fake records zero requests) —
                   TestTaskSyncValidatesStatusBeforeTouchingTheBoard
                                                             [risk+mvp+verification]
                 - `task-sync <phase> --status " Task-In-Review"` reaches the state
                   map: the fake sees label "dross/status:task-in-review", not the
                   padded form — TestTaskSyncNormalizesStatus                  [risk]
                 - after a `--status task-complete --close` run, no mirrored task
                   issue carries dross/status:task-in-review — the label set the
                   patch writes carries the terminal status instead    [verification]
                 - on provider=forgejo, `task-sync <phase> --close` issues one close
                   per task, writes no state field, and prints no provider-refusal
                   message — TestFlatBoardTaskCloseClosesEveryCard fails if
                   task-pull's refuse-by-name shape is copied here
                                                             [risk+mvp+verification]
                 - with three tasks where the SECOND close read-back reports
                   unresolved: the command exits non-zero, the message names that
                   task id, the third task is still attempted, and board.json
                   records no agreement point for the failed one —
                   TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing        [risk]

  t-4  Close the milestone epic, entity-gated                      [risk skeleton]
       files:    internal/cmd/issue.go, assets/prompts/milestone.md,
                 internal/cmd/issue_milestone_close_test.go
       covers:   c-3
       depends:  t-2
       desc:     milestone-sync gains --close: it resolves the milestone entity and
                 closes it through closeBoardIssue only when that entity is an ISSUE
                 (YouTrack epic mode). A version bundle, an agile board or a forge
                 milestone id is refused by name, closing nothing. milestone.md's
                 finalize step emits the command after `dross milestone complete
                 --finalize`.
       contract: - in epic mode, `milestone-sync v1.5 --close` writes the mapped
                   resolved State to the epic's idReadable and verifies the
                   read-back — TestEpicCloseResolvesForReal   [risk+mvp+verification]
                 - in version mode, and on a forgejo board whose milestone id is the
                   numeric milestone 7, --close makes NO close request (the fake
                   fails the test if any close/transition endpoint is hit) and exits
                   non-zero naming the mode — TestMilestoneCloseRefusesANonIssue
                   Entity; without the gate this test closes forge issue #7
                                                                 [risk+verification]
                 - an epic whose read-back still reads unresolved fails the command
                   and prints no closed line — TestEpicCloseFailsWhenUnresolved
                                                                 [risk+verification]
                 - `milestone-sync v1.5` WITHOUT --close still only ensures the link
                   — the fake records no state write, so the ensure path cannot
                   start closing epics by accident                      [verification]
                 - milestone.md §8 contains `dross issue milestone-sync <version>
                   --close` positioned after the `--finalize` step; deleting or
                   reordering it fails TestMilestonePromptClosesTheEpicAtFinalize
                                                             [risk+mvp+verification]

  t-5  Reconcile and close resolved backlog mirrors                [risk skeleton]
       files:    internal/cmd/issue.go, assets/prompts/ship.md,
                 internal/cmd/issue_backlog_close_test.go
       covers:   c-6
       depends:  t-2
       desc:     syncBacklog, after pushing the live set, walks board.BacklogKeys()
                 and closes each recorded mirror that has left the live set AND is
                 provably resolved — a `slug:` whose phase directory now exists, a
                 deferred item that is dismissed, a `[routed]` item whose target
                 phase issue reads Resolved on the tracker — dropping the key only
                 after a verified close. A key it cannot attribute is warned about
                 and left open. ship.md's finalize step runs `dross issue
                 backlog-sync <version>`.
       contract: - a `slug:x` mirror whose phase dir now exists is closed and its
                   board.json key dropped, while the issue for a still-unscaffolded
                   slug stays open — TestBacklogClosesScaffoldedSlugOnly
                                                             [risk+mvp+verification]
                 - a second run closes nothing: the fake counts close calls, so a
                   non-idempotent reconcile fails                              [mvp]
                 - a `[routed]` mirror is closed when lookupPhaseIssue's target
                   issue reads Resolved and left open when it reads unresolved —
                   the two cases differ only in the target's read-back —
                   TestRoutedBacklogClosesOnlyWhenTargetResolved
                                                             [risk+mvp+verification]
                 - a dismissed deferred item is closed and unlinked rather than
                   silently skipped — it leaves the live set the same way a
                   scaffolded slug does                                [verification]
                 - a recorded key for ANOTHER milestone's slug (absent from this
                   version's live set, phase not scaffolded) is left open and warned
                   about — TestUnattributableBacklogKeyIsNeverClosed fails if the
                   reconcile closes by set-difference alone                    [risk]
                 - a close whose read-back fails leaves the board.json key in place
                   so the next run retries — an unlinked-but-open mirror would be
                   unreachable by every later run —
                   TestFailedBacklogCloseKeepsTheLink                [risk+verification]
                 - ship.md's finalize section calls `dross issue backlog-sync
                   <version>`; removing the line fails
                   TestShipPromptReconcilesTheBacklog          [risk+mvp+verification]

Wave 3 (depends t-3)
  t-6  Introduce task-complete and emit it at finalize             [risk skeleton]
       files:    internal/configenum/configenum.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/cmd/task_lifecycle.go,
                 assets/prompts/ship.md, internal/cmd/ship_prompt_test.go,
                 internal/cmd/task_lifecycle_test.go
       covers:   c-2
       depends:  t-3
       desc:     Adds `task-complete` to LifecycleStatuses, maps it to YouTrack
                 `Verified` and Jira `Done`, gives the board→plan inverse its
                 one-way terminal entry, and emits `dross issue task-sync
                 <phase-id> --status task-complete --close` from ship.md's §6
                 finalize, after `dross phase complete`. All of it lands together:
                 the existing divergence guard is red with any one part missing.
       contract: - dropping the `task-complete` key from defaultYouTrackStateMap or
                   defaultJiraStateMap fails the existing
                   TestStateMapsKeyExactlyTheEmittedStatuses for that provider alone
                   (the two maps are checked independently)   [risk+mvp+verification]
                 - adding the Set member without the ship.md line fails the existing
                   TestEmittedStatusesAreTheLifecycleSet with "in the Set but
                   nothing emits it"                          [risk+mvp+verification]
                 - resolveYouTrackState("task-complete", nil) == "Verified" and
                   resolveJiraState("task-complete", nil) == "Done"    [verification]
                 - planStatusForLifecycle("task-complete") returns done — a card
                   dragged to the terminal column reads back as a done task instead
                   of as an unmirrored column (taskUnchanged)          [verification]
                 - lifecycleForPlanStatus("done") still returns task-in-review, and
                   the existing TestTaskEdgesPairTheBoardStatusWithThePlanStatus
                   stays green — `task-complete` never appears at a per-task execute
                   edge                                            [risk+verification]
                 - ship.md §6 emits the task close AFTER `dross phase complete
                   <phase-id>` and before §7 wrap; swapping the two lines fails
                   TestShipPromptClosesTaskCardsAfterPhaseComplete
                                                             [risk+mvp+verification]

Wave 4 (depends t-1, t-4, t-5, t-6)
  t-7  Guard namespace and terminal-path escapes                   [risk skeleton]
       files:    internal/board/namespace_guard_test.go,
                 internal/cmd/board_lifecycle_divergence_test.go
       covers:   c-5
       depends:  t-1, t-4, t-5, t-6
       desc:     Two guards. One reflects over board.Board's json-tagged link map
                 fields, plants a sentinel id in each, and asserts IsLinked reports
                 it — a namespace added to the struct and not to the filter fails by
                 field name. The other extends the existing lifecycle divergence
                 file: every mirror lane has a --close emission in assets/prompts,
                 and every terminal lifecycle status maps, in BOTH provider maps, to
                 a state the tracker reports resolved.
       contract: - adding a field `Epics map[string]string` to board.Board and not
                   wiring it into IsLinked fails
                   TestEveryBoardNamespaceIsFiltered with "Epics" in the message —
                   the guard enumerates by reflection, never by a hand-written list
                   (a hand-written list is the same manual step that produced this
                   phase)                                     [risk+mvp+verification]
                 - each covered namespace populated with a sentinel makes
                   IsLinked(sentinel) true — reverting IsLinked to phases+quicks
                   fails on backlog, tasks and milestones by name      [verification]
                 - a LifecycleStatuses member whose state-map value is not one the
                   provider treats as resolved (e.g. `task-complete` → "In Review",
                   or defaultYouTrackStateMap "task-complete": "Open") fails
                   TestEveryTerminalStatusResolves, naming the provider and value
                                                             [risk+mvp+verification]
                 - deleting the `--close` emission from ship.md (phase, task or
                   backlog) or from milestone.md fails
                   TestEveryMirrorLaneHasACloseEmission, naming the lane whose cards
                   would now strand                          [risk+mvp+verification]
                 - every `--close` emission in the prompt corpus that carries a
                   --status passes a LifecycleStatuses member both default state
                   maps resolve to that provider's terminal value — emitting
                   `--status uat --close` fails                       [verification]
                 - the guard scans a non-empty prompt corpus and a non-empty
                   namespace set: an empty scan t.Fatal's rather than passing
                   vacuously (the `scanned == 0` shape the existing divergence guard
                   already uses)                                              [risk]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3, t-6 |
| c-3 | t-4 |
| c-4 | t-2 |
| c-5 | t-7 |
| c-6 | t-5 |

## Disagreements

### D-1 — `milestone-sync --close` over a non-issue entity: refuse, or report nothing to close?

- **risk**: refuse by name, exit non-zero. In version mode `ensureMilestoneLink`
  returns a version-bundle id or a numeric forge milestone id, and `CloseIssue("7")`
  on a REST forge closes **issue #7** — an unrelated card.
- **verification**: same refusal, same non-zero exit; adds that naming the mode tells
  the user why nothing happened.
- **mvp**: report "nothing to close" and exit **0** — a fixVersion is not an issue, so
  erroring makes milestone.md's unconditional call fail for every version-mode repo.
- **Provisional default taken: refuse, exit non-zero (risk + verification).**
- **Why it matters**: this is the one place in the phase where getting it wrong writes
  to *someone else's* card rather than failing to write to ours. Both halves of the
  disagreement are real, though: the gate is non-negotiable (a silent
  `CloseIssue(<milestone-id>)` is a data-corrupting bug), but the exit code is a
  policy choice, and a non-zero exit inside milestone.md's finalize arm will halt
  finalize on every non-epic board until someone guards the call site. If the phase
  wants both, the fix is mvp's exit code with risk's gate — the entity check stays,
  only the severity drops. Left non-zero here because c-3 asks for the epic to *reach*
  a resolved state, and a zero exit over a bundle nothing can resolve is exactly the
  false-close shape this phase exists to remove.

### D-2 — Backlog closure basis: attributed, or set-difference?

- **risk**: attributed. Each closure names its reason (slug scaffolded / item
  dismissed / routed target resolved); a key that cannot be attributed is warned about
  and left open. `board.Backlog` is global while `backlog-sync` takes one version and
  `slug:` keys carry no version, so pure absence would close a sibling milestone's
  mirrors.
- **mvp** and **verification**: set-difference — close and unlink every recorded key
  outside the live item set just built.
- **Provisional default taken: set-difference as the entry gate, attribution as the
  close gate (risk), i.e. a key must both have left the live set and be provably
  resolved.**
- **Why it matters**: 94 of this repo's 120 links are backlog keys. The 2-of-3 majority
  is on the cheaper rule, but neither mvp nor verification addresses the global-vs-
  per-version scope of `board.Backlog`, and a set-difference reconcile run under `v1.5`
  would see every `v1.4` slug key as absent. If the executing agent finds that
  `syncBacklog` already scopes its recorded keys per version, the attribution layer is
  redundant and t-5 should collapse to the simpler rule.

### D-3 — When the `task-complete` ship.md emission lands

- **risk**: wave 3 (t-6), after `task-sync --close` exists (t-3), bundling enum + both
  state maps + prompt line in one commit because `TestEmittedStatusesAreTheLifecycleSet`
  fails the build in both directions.
- **verification**: wave 1 (t-2) — same bundle, but two waves *before* the `--close`
  flag it names (t-4, wave 3).
- **mvp**: wave 1, with the flag itself in the same task, so nothing is ever emitted
  that the CLI cannot parse.
- **Provisional default taken: risk's ordering (emission in wave 3, after the flag).**
- **Why it matters**: verification's ordering leaves two waves in which `ship.md`
  instructs the agent to run `task-sync --status task-complete --close` against a
  binary that rejects `--close`. That is a live prompt regression on `main` if the
  phase is shipped mid-way, and rule r-01 means the installed binary may lag the source
  anyway. mvp's single-task version has no red window at all but produces one task
  spanning six files across four layers, which is the granularity risk deliberately
  avoids.

### D-4 — How Jira gets a verified close

- **verification**: add `JiraClient.CloseIssueAs` (state-map write + read-back, unmapped
  status is an error), mirroring YouTrack, and route `closeBoardIssue` through it.
- **risk** and **mvp**: generalise inside `closeBoardIssue` — after the close, `GetIssue`
  and require `Resolved || State == "closed"`; explicitly reject adding a
  `CloseAndVerify` method every backend must implement for one call site.
- **Provisional default taken: the generic read-back in `closeBoardIssue` (risk + mvp),
  with Jira's existing `SetState(key, status, override)` supplying the state-map write.**
- **Why it matters**: c-4 says the close "writes the state map's terminal state" — a
  generic `GetIssue` read-back verifies the *verdict* but does not by itself write the
  mapped state on Jira, so the generic path is only honest if it calls Jira's existing
  `SetState` before closing. `JiraClient.SetState` and `Issue.Resolved` both already
  exist (`internal/forge/jira.go:448`, `internal/forge/forge.go:244`), so this is a
  wiring choice, not a capability gap. Pick verification's dedicated method instead if
  the executing agent finds Jira's transition machinery can't be driven from
  `closeBoardIssue` without leaking Jira specifics into `internal/cmd`.

### D-5 — Do c-3 and c-4 share a task?

- **mvp**: one task (its t-3). Both are "route this close through `closeBoardIssue` and
  verify the read-back" in the same function; split, the second is a two-line edit to a
  function the first just rewrote.
- **risk** and **verification**: separate tasks in separate waves — the shared seam
  first, the epic lane after it, because the epic lane also needs the entity gate and a
  prompt edit that have nothing to do with the quick lane.
- **Provisional default taken: separate (risk + verification), giving 7 tasks / 4 waves
  rather than 5 / 2.**
- **Why it matters**: this is the whole shape difference between the drafts. mvp's
  merge is defensible on edit-size grounds, but it puts the entity gate — the one
  destructive-write risk in the phase — inside a task whose headline is "quick close",
  where a reviewer is not looking for it.

### D-6 — Where the namespace guard lives

- **risk**: `internal/cmd/board_mirror_guard_test.go` — the filter is two pieces
  (`IsLinked` plus `collectInbound`'s marker clause) and only `internal/cmd` sees both.
- **mvp**: `internal/board/namespace_guard_test.go`. **verification**:
  `internal/board/link_namespace_test.go` plus `internal/cmd/mirror_terminal_path_test.go`.
- **Provisional default taken: `internal/board/namespace_guard_test.go` for the
  reflection guard (mvp + verification), terminal paths in the existing
  `internal/cmd/board_lifecycle_divergence_test.go` (risk).**
- **Why it matters**: the reflected surface is `board.Board`'s own fields, so the guard
  belongs in the package that owns the struct and will be seen by whoever adds the sixth
  namespace. Note `Dismissed` is a `[]string` and `LastPull` a `time.Time`, so the
  reflection must select map-typed fields or the guard fails on day one.

### D-7 — What evidence backs c-1's "116 → 1"

- **risk**: a fixture board.json built at this repo's shape (3 milestone / 22 phase /
  3 quick / 94 backlog / 20 task links, 6 dismissed) over a 120-issue fake, asserting
  `len(feed) == 1` by key.
- **verification**: load THIS repo's real `.dross/board.json`, feed every id it links
  back as the `ListIssues` response, assert the feed is empty — explicitly refusing to
  pin the number 116, which breaks the next time anything is mirrored.
- **mvp**: neither; asserts the mechanism per namespace only.
- **Provisional default taken: both (verification's real-file test plus risk's
  per-namespace subtests), and no assertion on 116.**
- **Why it matters**: the real-file test is the only one that can fail when the repo's
  own board drifts, and the fixture test is the only one that names *which* namespace
  broke. They cost one file between them and catch different regressions; the count
  itself is asserted by neither, which is the right call.
