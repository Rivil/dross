# Panel draft — risk lens

Phase board-mirror-reaper — 10 tasks across 4 waves

Lens: every task owns exactly one failure mode. The nine risks this phase can
fail on, and their owners:

| # | Failure mode | Owner |
|---|---|---|
| R1 | A card is closed whose artefact is not done (or a human's card is swept) | t-2 |
| R2 | A stranded card is invisible because its board.json link was lost | t-4 |
| R3 | A new namespace / lifecycle status silently has no reap path | t-7 |
| R4 | A "dry run" writes to the live board | t-6 |
| R5 | One bad write aborts the sweep and leaves the board half-swept | t-8 |
| R6 | A second run re-closes cards it already closed | t-2 (rule) + t-8 (proof) |
| R7 | A wrong sweep cannot be reversed | t-3 + t-10 |
| R8 | Strandedness silently accumulates again | t-9 |
| R9 | The verb rename breaks a caller that only fails months later, at runtime | t-1 + t-5 |

## Wave 1

```
t-1  Nest issue verbs as subcommands
     files:    internal/cmd/issue.go, internal/cmd/issue_task.go,
               internal/cmd/issue_task_pull.go, internal/cmd/issue_verb_shape_test.go
               (+ the mechanical runCmd-arg sweep the rename reddens:
                internal/cmd/deferred_link_test.go, deferred_routed_sync_test.go,
                deferred_board_test.go, issue_task_test.go, issue_task_pull_test.go,
                issue_test.go, issue_backlog_close_test.go, issue_milestone_close_test.go,
                issue_close_truth_test.go, issue_task_close_test.go,
                issue_task_state_test.go, issue_backlog_id_test.go,
                issue_phase_resolve_test.go, task_lifecycle_test.go)
     covers:   c-8
     desc:     Split phase-sync/task-sync/task-pull/milestone-sync/backlog-sync into
               `phase sync`, `task sync`, `task pull`, `milestone sync`, `backlog sync`
               parent+child cobra commands. No aliases. Flags stay on the leaf.
     contract: restoring a hyphenated Use ("task-sync") fails TestIssueVerbsAreNested,
               which walks Issue()'s whole tree and errors on any Use whose first word
               contains a hyphen; a tree where Find(["task","sync"]) or
               Find(["backlog","sync"]) returns cobra's unknown-command error fails
               TestNestedIssueVerbsResolve.

t-2  Classify a mirror from its on-disk record
     files:    internal/cmd/issue_reap.go, internal/cmd/issue_reap_test.go
     covers:   c-3, c-4, c-1
     desc:     reapClass taxonomy (phase/task/milestone/quick/backlog-slug/backlog-routed/
               backlog-someday) + a class→terminal-lifecycle-status table + classify(),
               a pure verdict over records: phaseDone(changes.json) for phase and task
               cards, milestone.toml [milestone].status for epics, phase.Dir existence for
               `slug:` keys, phaseDone(target) for `[routed]` keys, deferred.toml dismissal
               for someday keys. Verdicts: reap / open / unattributable. Deny by default.
     contract: a phase card sitting in "In Review" whose phase dir carries no changes.json
               classifies open — reading iss.State or the dross/status label instead of the
               record fails TestPhaseCardIsClassifiedFromItsCompletionRecord; a `slug:` key
               whose phase directory is absent classifies unattributable, not reap
               (TestSlugBacklogNeedsItsPhaseDir); an epic whose milestone.toml status is
               "active" classifies open (TestEpicFollowsMilestoneStatusNotTheCard);
               a card already sitting in its class's terminal state classifies not-stranded,
               which is what makes the re-run a no-op (TestAlreadyTerminalIsNotStranded, c-4).

t-3  Add the reap journal (write, load, prior state)
     files:    internal/reaplog/reaplog.go, internal/reaplog/reaplog_test.go
     covers:   c-9
     desc:     .dross/reap-log.json: runs, each holding started-at plus per-card
               {issue, class, prior_state, prior_resolved, prior_labels, outcome}.
               Append-on-apply, Last() for the undo target. Missing file = empty log.
     contract: an entry that drops prior_state fails TestJournalRoundTripsPriorState
               (marshal→load→compare); Load on an absent .dross/reap-log.json returns an
               empty log and a nil error, so a first `--undo` says "nothing to undo"
               instead of erroring (TestMissingJournalIsNotAnError); a card recorded with
               outcome=failed is excluded from Last().Closed() (TestFailedCardIsNotUndoable).

t-4  Discover mirrors by the dross marker label
     files:    internal/cmd/issue_reap_discover.go, internal/cmd/issue_reap_discover_test.go
     covers:   c-7, c-1
     desc:     ListIssues{State:"all", Labels:["dross"]} plus attribution: parse
               dross/phase:<slug>, dross/task:<slug>/<id>, dross/deferred:<id>,
               dross/target:<slug>, dross/quick into an artefact ref, so a card with no
               board.json link still resolves to the record that governs it.
     contract: a marker-labelled card in no board.json namespace still enters the inventory,
               attributed by its dross/phase: label — deleting the marker sweep fails
               TestUnlinkedMirrorIsDiscovered (seeded with DRO-33/36/37/38/95/96);
               a card carrying neither the dross marker nor any link never enters the
               inventory (TestHumanFiledCardIsNeverInInventory); a marker-labelled card
               with no attributable label yields an unattributable ref rather than a bare
               guess (TestUnattributableMarkerCardIsNamedNotGuessed).
```

## Wave 2 (depends on wave 1)

```
t-5  Repoint every emission site to the nested verbs
     files:    assets/prompts/execute.md, assets/prompts/ship.md, assets/prompts/milestone.md,
               assets/prompts/verify.md, assets/prompts/plan.md, assets/prompts/inbox.md,
               assets/prompts/pause.md, internal/cmd/deferred_add.go,
               internal/cmd/prompt_issue_verbs_test.go,
               internal/cmd/board_lifecycle_divergence_test.go
     covers:   c-8
     depends:  t-1
     desc:     Rewrite every `dross issue <hyphenated>` occurrence in the prompts and in
               deferred_add.go's user-facing hint; update taskSyncEdgeRE to the nested
               spelling; refresh README.md/ARCHITECTURE.md mentions.
     contract: a prompt still invoking `dross issue task-sync` fails
               TestPromptIssueVerbsResolve, which extracts every `dross issue …` invocation
               from assets/prompts/*.md and resolves it against the live Issue() tree;
               and TestTaskSyncEdgeRegexIsNotVacuous fails when taskSyncEdgeRE matches zero
               prompt lines — without it the rename would silently empty
               board_lifecycle_divergence_test's emit-set and pass on nothing.

t-6  Add `dross issue reap` — dry-run plan, --namespace
     files:    internal/cmd/issue_reap_cmd.go, internal/cmd/issue_reap_cmd_test.go,
               internal/cmd/issue.go
     covers:   c-2, c-1, c-7
     depends:  t-1, t-2, t-4
     desc:     The inventory = every board.json link ∪ every marker-labelled card, deduped
               by issue id; classify each; render one line per stranded card (issue id,
               class, justifying record path) plus per-class counts. `--namespace` is
               repeatable and its accepted values are the class table's namespaces.
     contract: with no --apply the fake forge records zero CreateIssue/UpdateIssue/
               CloseIssue/SetState calls — TestReapDryRunWritesNothing fails the moment a
               write escapes the dry path; `--namespace quicks` over a fixture holding a
               stranded task card and a stranded quick prints only the quick
               (TestNamespaceFilterScopesThePlan); an unknown `--namespace bogus` is refused
               naming the valid set (TestUnknownNamespaceIsRefused); each plan line names
               the record that justifies the close, not just the card
               (TestPlanLineNamesItsJustifyingRecord).

t-7  Guard that every mirror class has a reap path
     files:    internal/cmd/issue_reap_coverage_test.go
     covers:   c-6
     depends:  t-2, t-4
     desc:     Reflection over board.Board's map-typed fields (the shape
               namespace_guard_test.go already uses) asserting each has a class-table entry;
               and every configenum.LifecycleStatuses member has a terminal mapping.
     contract: adding a sixth map field to board.Board with no class-table entry fails
               TestEveryBoardNamespaceHasAReapPath, by field name, the day the field is
               added; a LifecycleStatuses member with no entry in the class table's terminal
               column fails TestEveryLifecycleStatusHasATerminalState; and
               TestClassTableIsNotEmpty fails if reflection finds no namespaces, so neither
               assertion can pass vacuously.
```

## Wave 3 (depends on wave 2)

```
t-8  Apply the sweep: write, survive failures, report
     files:    internal/cmd/issue_reap_apply.go, internal/cmd/issue_reap_apply_test.go,
               internal/cmd/issue_reap_cmd.go
     covers:   c-1, c-5, c-4, c-9
     depends:  t-2, t-3, t-6
     desc:     --apply walks the classified plan, writes each card's class terminal state
               through closeBoardIssue (mapped write + verified read-back), records
               {prior state, outcome} into the journal per card, collects failures instead
               of returning on the first, and exits non-zero listing each failed issue id.
     contract: a fake forge erroring on card 3 of 5 still attempts 4 and 5 and the closing
               report names DRO-x among failures — TestApplyContinuesPastAWriteFailure;
               a close whose read-back still reads unresolved is reported failed and not
               counted closed (TestUnverifiedCloseIsNotCountedClosed); a second --apply over
               the same fixture issues zero writes and reports zero stranded
               (TestSecondApplyIsANoOp, c-4); the journal written for a partially failed run
               holds only the cards that actually closed (TestJournalRecordsOnlyRealCloses).

t-9  Report the stranded count in doctor and watch
     files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
               internal/cmd/watch.go, internal/cmd/watch_test.go
     covers:   — (locked decision prompt_edge)
     depends:  t-6
     desc:     doctor gains an advisory "Stranded board mirrors" section from the same
               classifier; the watch digest gains a `stranded` count. Read-only, no writes,
               and a board that is off or unreachable degrades to omitting the line.
     contract: a fixture board holding 2 stranded cards makes doctor print the advisory with
               the count and leave the exit code unchanged — TestDoctorReportsStrandedMirrors;
               a clean board prints no section at all (TestDoctorSilentWhenNothingStranded);
               an unreachable board still produces a digest, with stranded omitted rather
               than the tick failing (TestWatchDegradesWhenBoardUnreachable).
```

## Wave 4 (depends on wave 3)

```
t-10 Reverse the last applied run with --undo
     files:    internal/cmd/issue_reap_undo.go, internal/cmd/issue_reap_undo_test.go,
               internal/cmd/issue_reap_cmd.go
     covers:   c-9
     depends:  t-3, t-8
     desc:     `dross issue reap --undo` reads the last applied run from the journal and
               writes each card back to its recorded prior state, verifying the read-back
               the same way the close does. Refuses --undo alongside --namespace/--apply.
     contract: after an apply of 3 cards, --undo restores each to the state the journal
               recorded — hardcoding "Open" instead of reading prior_state fails
               TestUndoRestoresTheRecordedPriorState; a card whose current state no longer
               matches what the sweep wrote is skipped and named, not overwritten
               (TestUndoRefusesACardMovedSinceTheSweep); --undo with no journal exits 0
               saying nothing to undo (TestUndoWithNoJournalIsClean).
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (one sweep closes every class, correct cards untouched) | t-2, t-4, t-6, t-8 |
| c-2 (dry-run by default, classified plan) | t-6 |
| c-3 (decisions derived from on-disk records) | t-2 |
| c-4 (re-run is a no-op) | t-2 (rule), t-8 (e2e proof) |
| c-5 (record-and-continue, report failures by id) | t-8 |
| c-6 (guard fails when a class gains no reap path) | t-7 |
| c-7 (marker-label discovery of unlinked mirrors) | t-4, t-6 |
| c-8 (nested verbs, every emission site, guard tests) | t-1, t-5 |
| c-9 (reversible sweep) | t-3, t-8, t-10 |
| locked: prompt_edge (doctor / watch report the count) | t-9 |

## Judgment calls

- **Classifier split from command (t-2 vs t-6), not built inside the RunE.** A verdict
  function over records is testable without a forge; a verdict computed inline in the
  command can only be tested through a fake client, which is where over-closing hides.
- **Idempotence is a classifier rule, not an apply-time skip.** Putting "already terminal →
  not stranded" in t-2 makes c-4 true of the dry run too, so the plan a human reads after a
  sweep is honestly empty. An apply-time skip would print 90 cards and write none.
- **Marker discovery (t-4) is wave 1, attribution by label only.** Rejected making it depend
  on the classifier: parsing dross/phase:/dross/task:/dross/deferred: into an artefact ref
  needs no taxonomy, and keeping it independent lets t-6 consume a complete inventory in
  wave 2 instead of shipping a dry run that cannot see the six unlinked cards.
- **The rename (t-1) carries its own test-file sweep.** Renaming the cobra `Use` strings
  reddens ~14 test files' `runCmd(t, Issue(), "backlog-sync", …)` args in the same commit —
  they cannot be deferred to t-5 without leaving the suite red between tasks. t-5 keeps only
  the prompt/doc surface, which is a different failure mode (silent at build time).
- **t-5 must update `taskSyncEdgeRE`.** Chose to name this explicitly rather than let the
  rename land: that regex greps prompts for `dross issue task-sync … --status`, and a
  rename without it leaves the lifecycle divergence guard matching nothing and passing
  vacuously — the exact class of silent-guard failure c-6 exists to prevent.
- **Journal in a new `internal/reaplog` package, not in board.json.** board.go's package doc
  commits to holding "nothing but the cross-references"; a per-run audit trail with prior
  states is a different artefact, and folding it in would also drag it into t-7's
  reflection-over-map-fields guard as a fake namespace.
- **`--undo` refuses a card moved since the sweep.** c-9 only asks for restore; restoring
  blindly would overwrite a human's deliberate post-sweep move, so a mismatch is skipped and
  named. Rejected the silent-overwrite reading of "restores those cards".
- **t-9 covers no criterion.** It exists solely because the `prompt_edge` locked decision
  requires doctor and /dross-watch to carry the stranded count; dropping it would ship a
  sweep nobody is told to run.
