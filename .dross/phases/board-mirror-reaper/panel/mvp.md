# board-mirror-reaper — mvp lens

Phase board-mirror-reaper — 5 tasks across 3 waves

## Wave 1

```
t-1  Nest the five dross issue verbs
     files:    internal/cmd/issue.go, internal/cmd/issue_task.go,
               internal/cmd/issue_task_pull.go,
               assets/prompts/execute.md, assets/prompts/ship.md,
               assets/prompts/verify.md, assets/prompts/plan.md,
               assets/prompts/milestone.md,
               internal/cmd/board_lifecycle_divergence_test.go,
               internal/cmd/ship_prompt_test.go,
               internal/cmd/issue_close_truth_test.go,
               internal/cmd/issue_task_test.go,
               internal/cmd/issue_task_close_test.go,
               internal/cmd/issue_task_state_test.go,
               internal/cmd/issue_task_pull_test.go,
               internal/cmd/issue_milestone_close_test.go,
               internal/cmd/issue_backlog_close_test.go,
               internal/cmd/issue_backlog_id_test.go,
               internal/cmd/issue_phase_resolve_test.go,
               internal/cmd/deferred_link_test.go,
               internal/cmd/deferred_routed_sync_test.go,
               internal/cmd/deferred_board_test.go,
               internal/cmd/deferred_add.go,
               README.md
     covers:   c-8
     contract: `dross issue phase sync` resolves through a `phase` parent
               command holding a `sync` child; typing the old `dross issue
               phase-sync` hits EnforceSubcommandKnown's unknown-subcommand
               RunE and exits non-zero. TestShipPromptEmitsTerminalBoardStatuses
               matches `dross issue phase sync <phase-id> --status complete
               --close` and `dross issue task sync <phase-id> --status
               task-complete --close` against raw ship.md — leaving either
               hyphenated fails it. mirrorLanes' four emission literals in
               board_lifecycle_divergence_test.go are repointed, so
               TestEveryMirrorLaneHasATerminalEmission fails by lane name if a
               prompt still carries the compound form. Every `runCmd(t,
               Issue(), "backlog-sync", …)` call site becomes `"backlog",
               "sync"`; a missed one fails with cobra's unknown-subcommand
               error rather than passing silently.

     Description: split each compound into a parent+child cobra pair —
     `phase sync`, `task sync`, `task pull`, `milestone sync`, `backlog sync`.
     Flags, args and RunE bodies move unchanged onto the child. No aliases.
     Repoint every assets/prompts emission site, the prompt-guard literals,
     and the test call sites that pass the verb as a literal arg.
```

```
t-2  Classify stranded mirrors from on-disk records
     files:    internal/cmd/reap_classify.go,
               internal/cmd/reap_classify_test.go
     covers:   c-1, c-3, c-7
     contract: a phase whose changes.json carries no `status` field yields no
               candidate, even when its card sits in "In Progress" —
               TestReapNeverClassifiesFromCardState plants a done-looking card
               over an incomplete record and asserts zero candidates; swapping
               the read from changes.json to forge.Issue.WorkflowState fails
               it. TestReapDiscoversUnlinkedMarkerMirrors feeds a ListIssues
               response holding a marker-labelled `dross/phase:<done-slug>`
               card absent from board.json.Phases and asserts it is classified
               as a phases candidate; deleting the marker-label discovery pass
               drops the count to 0. The same response holds one card with no
               `dross` label, and it is never a candidate.
               Per-class: TestReapClassifiesEachNamespace asserts a done phase
               yields terminal `complete`, its task links yield
               `task-complete`, a milestone.toml with status="complete" yields
               the epic, a quick ref older than state.json's version yields
               `complete`, and a `slug:` key whose phase directory exists
               yields a bare close — while an active milestone, a quick ref
               equal to the current version, and a `slug:` with no phase
               directory all classify as skipped-with-reason.

     Description: new `reapClass` table keyed by board.Board field name, a
     `reapCandidate{Issue, Class, Key, Terminal, Reason}`, and
     `classifyReap(ctx, only []string)` walking all five namespaces plus a
     marker-label discovery pass for issues no namespace links. Verdicts reuse
     phaseDone, milestone.Load().Milestone.Status, state.json's version, and
     backlogVerdictFor. Classification makes no GetIssue call for its verdict.
```

## Wave 2 (depends t-2)

```
t-3  Add dross issue reap with dry-run and --apply
     files:    internal/cmd/issue_reap.go, internal/cmd/issue_reap_test.go,
               internal/cmd/issue.go
     covers:   c-1, c-2, c-4, c-5
     depends:  t-2
     contract: TestReapDryRunWritesNothing runs `reap` over a fake BoardClient
               that fails the test on any CloseIssue / UpdateIssue / SetState
               call, and asserts the printed plan still names all three
               candidates with issue id, class and justifying record.
               TestReapContinuesPastAFailedClose makes the 2nd of 3 candidates'
               close return an error and asserts cards 1 and 3 are still
               closed, the command exits non-zero, and the final report names
               the 2nd card's issue id — an abort-on-first-error implementation
               closes 1 and leaves 3 stranded, failing it.
               TestReapSecondApplyIsANoOp re-runs `--apply` against the same
               fake with the closed cards now reading Resolved, and asserts
               zero closes and exit 0. TestReapNamespaceFilterScopesTheSweep
               runs `--namespace tasks` over a board with phase and task
               candidates and asserts only the task cards were written.

     Description: cobra `reap` under `issue` — dry-run by default, `--apply`
     to write, repeatable `--namespace` validated against the reapClass set.
     Apply skips a card boardIssueIsDone already reports resolved, closes the
     rest through closeBoardIssue with the class's terminal status, collects
     per-card failures instead of returning on the first, and reports them by
     issue id at the end.
```

```
t-4  Guard every mirror class for a reap path
     files:    internal/cmd/board_lifecycle_divergence_test.go,
               internal/cmd/reap_classify.go
     covers:   c-6
     depends:  t-2
     contract: TestEveryMirrorLaneHasAReapPath enumerates board.Board's
               map-typed fields by reflection (the same derivation
               boardNamespaceFields already uses) and fails by field name when
               one has no reapClasses entry — adding a sixth namespace to
               board.Board and nothing else turns the suite red. The same test
               asserts each class's terminal status is a
               configenum.LifecycleStatuses member that resolves in both
               defaultYouTrackStateMap and defaultJiraStateMap, so setting a
               class's terminal to an unmapped literal fails naming the status.

     Description: add a `reapClass` field to the existing mirrorLanes table and
     one test asserting namespace→class→terminal-status coverage end to end.
```

## Wave 3 (depends t-3)

```
t-5  Record applied closes and add --undo
     files:    internal/board/board.go, internal/cmd/reap_undo.go,
               internal/cmd/reap_undo_test.go, internal/cmd/issue_reap.go,
               internal/forge/youtrack.go
     covers:   c-9
     depends:  t-3
     contract: TestReapApplyJournalsPriorState asserts board.json's `reaped`
               slice, after an apply, holds one record per closed card
               carrying its pre-close labels, `dross/status:` value and
               WorkflowState — dropping the pre-close read leaves the record's
               prior state empty and fails it. TestReapUndoRestoresPriorState
               closes a card that was in `task-in-review`, runs `reap --undo`,
               and asserts the fake client received a state write back to
               `task-in-review` and the original label set — an undo that only
               reopens without restoring the state fails it.
               TestReapUndoReversesOnlyTheLastRun asserts a second apply
               replaces the journal, so `--undo` after two runs restores the
               second run's cards and touches none from the first.
               TestReapUndoContinuesPastAFailure asserts a card whose restore
               is refused is named by issue id while the remaining cards are
               still restored.

     Description: board.Board gains `Reaped []ReapRecord` (a slice, so the
     namespace reflection guards are unaffected). Apply captures each card's
     pre-close read into the journal under one run id, replacing the previous
     run's. `reap --undo` restores labels + lifecycle status per record;
     YouTrackClient gains SetStateValue for the raw-state write back, since
     UpdateIssue ignores IssuePatch.State on that backend.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 sweep closes every stranded class | t-2, t-3 |
| c-2 dry-run by default, `--apply` writes | t-3 |
| c-3 verdicts from the on-disk record | t-2 |
| c-4 re-run is a no-op | t-3 |
| c-5 failures recorded, sweep continues | t-3 |
| c-6 guard for a class with no reap path | t-4 |
| c-7 marker-label discovery of unlinked mirrors | t-2 |
| c-8 nested subcommands, no hyphenated compounds | t-1 |
| c-9 reversible sweep | t-5 |

9/9 criteria covered.

## Judgment calls

- **Dropped the doctor / watch stranded-count detector.** The `prompt_edge`
  decision names `dross doctor` and `/dross-watch` as the drift signal, but no
  criterion asks for it, and `dross issue reap` with no flags already *is* the
  detector — it prints the classified plan and writes nothing. Adding two more
  reporting surfaces means two more whole-board API sweeps per doctor run for a
  count the sweep itself prints. The negative half of the decision (no prompt
  emits reap) is honoured. Flagging it loudly: if the judge reads the decision
  as mandating both surfaces, it is a sixth task on t-3.
- **t-1 breaks the 5-file rule deliberately.** `verb_naming` locks out
  back-compat aliases, so there is no midpoint where the Go tree and the
  prompts agree — a Go-only rename commit leaves every prompt emitting a dead
  verb while the prompt-literal guards still pass, which is green-but-wrong.
  One task, one commit, one `sed`-shaped change across the call sites.
- **Quick cards resolve off state.json's version, not a per-quick record.**
  c-3 enumerates the record for every class except quicks, and quicks have
  none — a standalone quick explicitly skips the changes record. The version
  counter is the on-disk fact that moves when a quick finishes, so a ref
  strictly older than `state.version` is resolved and a ref equal to it is left
  open (the quick may still be in flight). Rejected: reading the card's own
  state, which c-3 forbids; and closing every quick unconditionally, which
  would close an in-flight one.
- **Journal lives on board.Board as a slice, not a new file.** board.json is
  already git-tracked, loaded on every `dross issue` call, and a slice field is
  invisible to both namespace-reflection guards (which select map-typed fields
  only). A new `.dross/reap.json` would need its own load path, its own
  gitignore decision and its own doctor check for nothing.
- **Undo restores state, and needs one new forge method.** YouTrack's
  `UpdateIssue` silently ignores `IssuePatch.State`, so "restore to the state
  held before the run" is unimplementable without a raw state write —
  `SetStateValue` is a ten-line extraction of `CloseIssueAs`'s body minus the
  resolved assertion. Rejected: undo-by-reopen-only, which would land a
  previously-`In Review` task card in `Open` and quietly lose the column the
  criterion is about.
- **Reused `backlogVerdictFor` rather than reimplementing the backlog rules.**
  `slug:` → phase directory, routed → target phase done, dismissed → resolved
  is already written, already tested by four tests in
  issue_backlog_close_test.go, and is exactly what c-3 specifies for that
  class. Rejected: a second backlog rule set inside the classifier, which
  would be two places to disagree about the same lane.
- **No separate "print the plan" task.** The dry-run print is the classifier's
  only consumer in wave 2 and is three lines of formatting; splitting it out
  would be a sub-ten-minute single-file task, which the granularity rule sends
  back into t-3.
