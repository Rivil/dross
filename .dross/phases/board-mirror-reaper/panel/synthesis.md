# Panel synthesis — board-mirror-reaper

Cold judge over three independent drafts (risk / mvp / verification). Nothing
below is authored from scratch: every task appears in at least one draft.

## Scores

| Draft | Dimension | Verdict |
|---|---|---|
| risk | criteria coverage | 9/9 plus the only draft that owns the `prompt_edge` locked decision with named files on both surfaces; but its classifier's evidence list omits the quick lane that c-1 counts (2 quicks). |
| risk | test-contract specificity | Strong and falsification-shaped throughout; uniquely catches the vacuous-guard failure (`TestTaskSyncEdgeRegexIsNotVacuous`, `TestClassTableIsNotEmpty`). |
| risk | granularity | 10 tasks, one failure mode each, no task spanning two subsystems — the cleanest atomic-commit shape of the three. |
| risk | wave correctness | 4 waves, deps honest; one factual slip — it hangs the coverage guard off `namespace_guard_test.go`, which does not exist (the reflection helper is in `board_lifecycle_divergence_test.go`). |
| mvp | criteria coverage | 9/9 on paper, but deliberately drops the doctor / watch detector that the locked `prompt_edge` decision names — a locked decision left unimplemented. |
| mvp | test-contract specificity | Good, and the best-grounded in code that exists (`backlogVerdictFor`, `boardIssueIsDone`, `EnforceSubcommandKnown`); alone in spotting that no forge method can write a raw prior state back. |
| mvp | granularity | Weakest: t-1 is a ~25-file monolith and t-3 carries c-1+c-2+c-4+c-5, so the dry-run gate and the write path land in one commit and can only fail together. |
| mvp | wave correctness | 3 waves, deps correct as far as they go, but the coarseness means the waves encode almost no ordering information. |
| verification | criteria coverage | 9/9, and the only draft that states c-1's untouched-cards half as a durable negative rather than a fixture pinned to today's 90-card board. |
| verification | test-contract specificity | Sharpest of the three: two-fixture (complete / not-complete) tests holding the card's board state identical, the pre-close-vs-read-back capture assertion, prompt invocations walked against the live cobra tree instead of a regexp allowlist. |
| verification | granularity | 10 tasks, one file cluster each; only t-10 (docs) is thin enough to question. |
| verification | wave correctness | 5 waves, correct classifier-before-command ordering; over-serialized at the tail — t-10 depends on t-9 for a doc edit that only needs t-3 and the reap surface. |

**Skeleton: verification.** It is the only draft whose ordering is derived from
what each criterion can be falsified by, and its contracts survive the two
failure modes the others leave open (a classifier testable only through an
HTTP-writing command; a ledger written from the post-close read-back). Grafts
onto it: risk's journal-first ordering, its vacuous-guard tests, its
doctor/watch file set and its `--undo` refusal rule; mvp's quick-lane evidence,
its raw-state forge gap, and its single-lane-registry guard placement.

## Merged plan

11 tasks across 5 waves.

```
Wave 1

t-1  Nest the five dross issue verbs as subcommands (Go tree + call sites)
     [verification skeleton + risk file sweep + mvp contract]
     files:    internal/cmd/issue.go, internal/cmd/issue_task.go,
               internal/cmd/issue_task_pull.go, internal/cmd/deferred_add.go,
               internal/cmd/issue_verb_shape_test.go (new),
               + the runCmd-arg sweep the rename reddens:
                 internal/cmd/{issue_test,issue_task_test,issue_task_close_test,
                 issue_task_state_test,issue_task_pull_test,issue_close_truth_test,
                 issue_backlog_close_test,issue_backlog_id_test,
                 issue_milestone_close_test,issue_phase_resolve_test,
                 deferred_link_test,deferred_routed_sync_test,deferred_board_test,
                 task_lifecycle_test,token_leak_test}.go
               + the doc-comment mentions in internal/forge/forge.go,
                 internal/forge/youtrack.go, internal/configenum/configenum_test.go
               [token_leak_test.go, task_lifecycle_test.go, forge/youtrack.go and
                configenum_test.go carry the compound spelling and are named in no
                single draft — grafted from a repo grep, all three drafts' sweep
                instruction implies them]
     covers:   c-8
     depends:  —
     desc:     Split each compound into a parent+child cobra pair — `phase sync`,
               `task sync`, `task pull`, `milestone sync`, `backlog sync`. Flags,
               args and RunE bodies move unchanged onto the child. No aliases.
               Prompts are NOT touched here (t-5).
     contract: - `runCmd(t, Issue(), "phase-sync", "01-auth")` returns
                 EnforceSubcommandKnown's unknown-subcommand error and exits
                 non-zero; `runCmd(t, Issue(), "phase", "sync", "01-auth")` runs
                 issue_test.go's existing body assertions unchanged. [mvp]
               - TestIssueVerbsAreNested walks Issue()'s whole tree and fails on any
                 `Use` whose first word contains a hyphen — restoring "task-sync"
                 reddens it. [risk]
               - TestNestedIssueVerbsResolve fails when Find(["task","sync"]) or
                 Find(["backlog","sync"]) returns cobra's unknown-command error. [risk]
               - TestIssueTaskPullReportsPlanMoved fails while the printed remedy
                 still reads `dross issue task-sync`. [verification]

t-2  Record-derived reap classifier (pure, GET-only)
     [verification skeleton + risk taxonomy + mvp quick rule]
     files:    internal/cmd/issue_reap.go, internal/cmd/issue_reap_classify_test.go
     covers:   c-3, c-4
     depends:  —
     desc:     `classifyReap(ctx, namespaces []string) ([]reapCard, error)` — a
               verdict over records, no forge write and no card-state read for the
               verdict. Evidence per class: phaseDone(changes.json) for phase AND
               task cards; milestone.toml [milestone].status for epics; phase.Dir
               existence for `slug:` backlog keys; the target phase's changes.json
               for `[routed]`; deferred dismissal for `someday:`; and for quicks,
               state.json's version — a quick ref strictly older than the current
               version is finished, a ref equal to it may still be in flight and
               stays open [mvp+verification; risk's evidence list omits quicks
               entirely, which would strand c-1's 2 quick cards]. Verdicts:
               reap / open / unattributable, deny by default. Reuses
               backlogVerdictFor rather than restating the backlog rules [mvp].
               A card boardIssueIsDone already reports resolved is not stranded —
               idempotence is a classifier rule, so the post-sweep dry run prints an
               honestly empty plan [risk+verification].
               reapCard carries {Key, Lane, Terminal, Why}; Why is the record path
               that justified it, for t-6's print.
     contract: - Two fixtures with the card's board state held IDENTICAL: a phase
                 whose changes.json reads complete yields its phase card and its task
                 cards; the same board state over a record with status removed yields
                 zero. A classifier reading iss.State or the dross/status label fails
                 here. (TestPhaseCardIsClassifiedFromItsCompletionRecord) [verification+risk]
               - An epic whose milestone.toml status is "active" classifies open;
                 status "complete" yields it. (TestEpicFollowsMilestoneStatusNotTheCard) [risk]
               - A `slug:` key whose phase directory is absent classifies
                 unattributable, not reap. (TestSlugBacklogNeedsItsPhaseDir) [risk]
               - A `[routed]` entry yields a card only when the target phase's
                 changes.json reads complete. [verification]
               - A quick at the current state.json version is not classified; an
                 older ref is. [mvp+verification]
               - A card already sitting in its class's terminal state classifies
                 not-stranded (TestAlreadyTerminalIsNotStranded) — c-4's first half. [risk]
               - The fake forge fails the test on any non-GET request during
                 classify. [verification]

t-3  Reap journal: write, load, prior state
     [risk — mvp and verification both fold this into the undo task; see D4]
     files:    internal/reaplog/reaplog.go, internal/reaplog/reaplog_test.go
     covers:   c-9
     depends:  —
     desc:     .dross/reap-log.json: runs, each holding started-at plus per-card
               {issue, class, prior_state, prior_resolved, prior_labels,
               dropped_link, outcome}. Append-on-apply, Last() for the undo target.
               A missing file is an empty log, not an error. Standalone package, not
               a board.Board field — board.go's package doc commits to holding
               "nothing but the cross-references", and a map on Board would register
               as a mirror namespace with t-7's guard and demand a nonsense reap path.
     contract: - An entry that drops prior_state fails TestJournalRoundTripsPriorState
                 (marshal → load → compare). [risk]
               - Load on an absent .dross/reap-log.json returns an empty log and a nil
                 error, so a first `--undo` says "nothing to undo" instead of
                 erroring. (TestMissingJournalIsNotAnError) [risk]
               - A card recorded with outcome=failed is excluded from Last().Closed().
                 (TestFailedCardIsNotUndoable) [risk]
               - TestReapLogIsNotABoardNamespace: reflection over board.Board finds no
                 new map field, so t-7's guard does not demand a reap path for the
                 ledger. [verification]

Wave 2 (depends wave 1)

t-4  Discover unlinked mirrors by the dross marker label
     [verification skeleton + risk attribution detail]
     files:    internal/cmd/issue_reap_discover.go,
               internal/cmd/issue_reap_discover_test.go
     covers:   c-7
     depends:  t-2
     desc:     Second classify source: ListIssues{Labels:[dross marker]} minus
               everything board.IsLinked already covers, then recover each orphan's
               lane from its own identity labels (dross/phase:<id>,
               dross/task:<phase>/<task>, dross/deferred:<id>, dross/target:<slug>,
               the quick label) and run it through the SAME record-derived verdict
               t-2 owns — discovery does not get its own verdict path. An orphan with
               no recognisable identity label is reported unclassifiable and never
               closed.
     contract: - A marker-labelled card in no board.json namespace, carrying
                 dross/phase:<complete-slug>, appears in the plan with lane Phases —
                 the DRO-33/36/37/38/95/96 shape. Deleting the marker sweep drops it.
                 (TestUnlinkedMirrorIsDiscovered) [risk+verification]
               - Deleting the dross/phase label from that same fixture moves the card
                 to the unclassifiable list rather than dropping it silently.
                 (TestUnattributableMarkerCardIsNamedNotGuessed) [risk+verification]
               - An orphan whose recovered phase is NOT complete is absent from the
                 plan. [verification]
               - A card carrying no marker label (a human-filed bug) never reaches the
                 plan or the unclassifiable list.
                 (TestHumanFiledCardIsNeverInInventory) [risk+mvp+verification]
               - A card present BOTH in board.json and in the marker sweep appears
                 exactly once (dedupe by issue key). [verification]

t-5  Repoint the prompt corpus to the nested verbs
     [verification skeleton + risk's missed files and non-vacuity guard]
     files:    assets/prompts/execute.md, ship.md, verify.md, plan.md, milestone.md,
               assets/prompts/inbox.md, assets/prompts/pause.md
               [inbox.md and pause.md each carry a compound invocation and appear
                only in risk's list — grep-confirmed],
               internal/cmd/board_lifecycle_divergence_test.go,
               internal/cmd/ship_prompt_test.go,
               internal/cmd/issue_verb_shape_test.go
     covers:   c-8
     depends:  t-1
     desc:     Rewrite every `dross issue <a>-<b>` invocation in the corpus; update
               taskSyncEdgeRE, mirrorLanes' emission literals, and ship_prompt_test's
               literal needles to the nested spelling.
     contract: - TestNoHyphenatedIssueVerbInThePromptCorpus: a regexp scan of
                 assets/prompts/*.md for `dross issue [a-z]+-[a-z]+` reports zero
                 hits. [verification]
               - TestEveryPromptIssueInvocationResolves: each `dross issue …` line is
                 walked against the real cobra tree via Issue().Find(args), failing by
                 prompt and line — a prompt repointed to a verb t-1 never registered
                 fails here, not on a live board. Not a regexp allowlist of the five
                 new spellings. [verification]
               - TestTaskSyncEdgeRegexIsNotVacuous: taskSyncEdgeRE matching zero
                 prompt lines is a failure — without it the rename silently empties
                 board_lifecycle_divergence_test's emit-set and the lifecycle guard
                 passes on nothing. [risk; named in no other draft and it is the exact
                 silent-guard class c-6 exists to prevent]
               - TestEveryMirrorLaneHasATerminalEmission and
                 TestShipPromptEmitsTerminalBoardStatuses still pass against the
                 rewritten prompts, so a half-done rewrite fails. [mvp+verification]

t-6  `dross issue reap` — dry-run plan and --namespace
     [verification skeleton + risk plan-line contract]
     files:    internal/cmd/issue_reap_cmd.go, internal/cmd/issue_reap_cmd_test.go,
               internal/cmd/issue.go
     covers:   c-1, c-2
     depends:  t-2, t-4
     desc:     Register `reap` under `issue`. Inventory = every board.json link ∪
               every marker-labelled card, deduped by issue id. No flag prints the
               whole-board plan: one line per card — issue id, lane, justifying record
               — grouped by lane with per-lane and total counts. `--namespace` is
               repeatable and validated against board.Board's map-field set, not a
               literal list, so a new namespace can never be unreachable by the filter
               while the guard still passes. `--apply` is declared here, wired in t-8.
     contract: - TestReapWithoutApplyNeverWrites: the fake forge fails the test on any
                 POST/PUT/PATCH/DELETE and records zero CreateIssue/UpdateIssue/
                 CloseIssue/SetState calls; the run exits 0 and prints a plan. [all three]
               - TestReapPlanNamesTheJustifyingRecord: each line carries issue key,
                 lane, and the record (phase id / milestone / backlog key) that
                 justified it — a line printing only the key fails. [risk+verification]
               - TestReapNamespaceFilterScopesThePlan: `--namespace tasks` over a
                 fixture stranded in all five lanes prints only task cards with a
                 matching footer; two `--namespace` flags print both lanes; an unknown
                 `--namespace bogus` errors naming the valid set. [all three]
               - TestReapWholeBoardCoversFiveLanes: with no flag the plan's lane set
                 equals board.Board's map-field set. [verification]

t-7  Guard that every mirror class has a reap path
     [mvp+verification placement, risk's anti-vacuity assertions]
     files:    internal/cmd/board_lifecycle_divergence_test.go,
               internal/cmd/issue_reap.go
     covers:   c-6
     depends:  t-2
     desc:     Add a `reapClass`/terminal field to the EXISTING mirrorLanes registry
               rather than standing up a second lane table, and assert coverage end to
               end through the existing boardNamespaceFields + stateMapPairs helpers.
               (risk put this in a new issue_reap_coverage_test.go and attributed the
               reflection helper to a `namespace_guard_test.go` that does not exist —
               the helper is in board_lifecycle_divergence_test.go.)
     contract: - TestEveryBoardNamespaceHasAReapPath: adding a map field to board.Board
                 with no reap lane entry fails BY FIELD NAME in the same run that adds
                 it; the reverse direction fails for a lane entry naming a namespace
                 that no longer exists. [all three]
               - TestEveryReapTerminalIsMapped: each lane's terminal is a
                 configenum.LifecycleStatuses member with an entry in BOTH
                 defaultJiraStateMap and defaultYouTrackStateMap, failing by provider
                 and status. [mvp+verification]
               - The guard cannot pass vacuously: an empty lane registry or an empty
                 reflection result is a t.Fatal, matching the existing helper's
                 behaviour. [risk+verification]

Wave 3 (depends wave 2)

t-8  Apply the sweep: write, journal the prior state, survive failures
     [verification skeleton + risk failure contracts + verification's capture assertion]
     files:    internal/cmd/issue_reap_apply.go, internal/cmd/issue_reap_apply_test.go,
               internal/cmd/issue_reap_cmd.go
     covers:   c-1, c-4, c-5, c-9 (capture half)
     depends:  t-2, t-3, t-6
     desc:     `--apply` walks the classified plan, captures each card's pre-write
               state into the t-3 journal, then writes the lane terminal through
               closeBoardIssue (mapped write + verified read-back), collecting a
               per-card failure instead of returning on the first — the taskCloseError
               shape issue_task.go already uses. Drops the board.json link only for
               lanes whose forward path drops it, recording the drop. Ends by printing
               every failure by issue id and exiting non-zero if any occurred.
     contract: - TestReapApplyClosesEveryLane: one stranded card per lane ends Resolved
                 on the fake forge, each written with its OWN lane terminal (task cards
                 task-complete, phase cards complete) — one shared terminal fails. [verification]
               - TestApplyContinuesPastAWriteFailure: the forge 500s on card 2 of 4;
                 cards 3 and 4 are still attempted and Resolved, the run exits non-zero,
                 and the closing report names the failing issue id exactly once. [all three]
               - TestUnverifiedCloseIsNotCountedClosed: a close whose read-back still
                 reads unresolved is reported failed, not counted closed. [risk]
               - TestReapLogCapturesStateBeforeTheWrite: the entry records the state
                 read BEFORE the close — a log written from the post-close read-back
                 records the terminal state and makes undo a no-op; that fixture fails. [verification]
               - TestJournalRecordsOnlyRealCloses: the journal for a partially failed
                 run holds only the cards that actually closed. [risk]
               - TestSecondApplyIsANoOp: a second `--apply` over the same fixture
                 classifies zero cards, issues zero write requests and exits 0 — c-4
                 end to end. [all three]
               - TestReapNeverTouchesCorrectlyOpenCards: cards whose record is not
                 complete, and cards with no marker, are still open after apply — c-1's
                 untouched half as a durable negative rather than a "90 cards closed"
                 fixture pinned to one day's board. [verification]

t-9  Report the stranded count in doctor and watch
     [risk+verification; mvp drops it — see D1]
     files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
               internal/cmd/watch.go, internal/cmd/watch_test.go,
               assets/prompts/watch.md
               [risk named watch.go/watch_test.go, verification named watch.md; the
                digest is produced by `dross watch` in Go and RENDERED by the prompt,
                so both halves are required — neither draft had the full set]
     covers:   — (locked decision prompt_edge; verification maps it to c-2, which is
               a stretch, so it is recorded here as decision-driven)
     depends:  t-6
     desc:     doctor gains an advisory "stranded board mirrors" section from the same
               read-only classifier, printing the count and the
               `dross issue reap --namespace <lane>` remedy; the watch digest gains a
               `stranded` count and watch.md renders it as a drift signal. Read-only.
               Skipped when [board].enabled is false; degrades to omitting the line
               when the board is unreachable.
     contract: - TestDoctorReportsStrandedMirrors: 3 stranded cards make doctor print
                 the advisory naming 3 and the remedy, with doctor's exit status
                 unchanged; 0 stranded prints the clean line. [risk+verification]
               - TestDoctorNeverWritesToTheBoard: the fake forge fails the test on any
                 non-GET during `dross doctor` — the detector may not become a silent
                 sweep. [verification]
               - TestDoctorSkipsStrandedCheckWhenBoardDisabled: no section and no HTTP
                 request at all with [board].enabled = false. [verification]
               - TestWatchDegradesWhenBoardUnreachable: an unreachable board still
                 produces a digest, with stranded omitted rather than the tick failing. [risk]

Wave 4 (depends wave 3)

t-10 Reverse the last applied run with --undo
     [risk skeleton + verification link-restore + mvp's raw-state forge gap]
     files:    internal/cmd/issue_reap_undo.go, internal/cmd/issue_reap_undo_test.go,
               internal/cmd/issue_reap_cmd.go, internal/forge/youtrack.go
     covers:   c-9
     depends:  t-3, t-8
     desc:     `dross issue reap --undo` reads the last applied run from the journal
               and writes each card back to its recorded prior state, verifying the
               read-back the way the close does, and re-adds any board.json link the
               sweep dropped. Refuses `--undo` alongside `--apply`/`--namespace`.
               YouTrackClient gains a raw state writer: CloseIssueAs and SetState both
               take a dross lifecycle status and map it, and UpdateIssue ignores
               IssuePatch.State — so restoring an arbitrary prior column ("In Review")
               is unimplementable today. [mvp; the gap is real and is named in no other
               draft — undo-by-reopen lands a previously In-Review task card in Open
               and loses exactly the column c-9 is about]
     contract: - TestUndoRestoresTheRecordedPriorState: after an apply of 3–4 cards,
                 --undo returns each to the exact prior state string the journal
                 recorded, per card, not one blanket state — hardcoding "Open" fails. [all three]
               - TestUndoRestoresDroppedBoardLinks: a backlog key deleted at apply is
                 present again in board.json after --undo. [verification]
               - TestUndoRefusesACardMovedSinceTheSweep: a card whose current state no
                 longer matches what the sweep wrote is skipped and named, not
                 overwritten. [risk — see D7]
               - TestUndoContinuesPastAFailure: a card whose restore is refused is named
                 by issue id while the remaining cards are still restored. [mvp]
               - TestUndoReversesOnlyTheLastRun: after two applies, --undo restores the
                 second run's cards and touches none from the first. [mvp]
               - TestUndoWithNoJournalIsClean: `--undo` with no reap-log.json writes
                 nothing and says there is nothing to undo. [risk]

Wave 5 (depends wave 4)

t-11 Document reap and the nested verb tree
     [verification]
     files:    README.md, ARCHITECTURE.md, internal/cmd/issue_verb_shape_test.go
     covers:   c-8
     depends:  t-5, t-10
     desc:     Update README's `dross issue {…}` capability row to the nested verb set
               plus `reap`, add the reap paragraph to ARCHITECTURE's board-sync section
               (classify-from-record, marker discovery, dry-run default, per-card
               failure isolation, undo ledger), and extend t-5's corpus guard to scan
               README.md and ARCHITECTURE.md. Both files carry compound spellings today
               (README 3 hits, ARCHITECTURE 5).
     contract: - TestNoHyphenatedIssueVerbInTheDocs: the t-5 scan extended to README.md
                 and ARCHITECTURE.md reports zero `dross issue [a-z]+-[a-z]+` hits — the
                 stale `milestone-sync, phase-sync` list in README fails until rewritten.
               - TestEveryDocumentedIssueVerbResolves: every `dross issue …` invocation
                 quoted in README/ARCHITECTURE resolves against the real cobra tree, so a
                 documented flag combination that does not exist fails in CI.
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 one sweep closes every stranded class, correct cards untouched | t-2, t-4, t-6, t-8 |
| c-2 dry-run by default, classified plan | t-6 |
| c-3 every decision derived from the on-disk record | t-2, t-4 |
| c-4 re-run after a full apply is a no-op | t-2 (rule), t-8 (e2e) |
| c-5 record-and-continue, report failures by issue id | t-8 |
| c-6 guard fails when a class gains no reap path | t-7 (+ t-5's non-vacuity guard) |
| c-7 marker-label discovery of unlinked mirrors | t-4, t-6 |
| c-8 nested verbs, every emission site, guard tests | t-1, t-5, t-11 |
| c-9 reversible sweep | t-3, t-8 (capture), t-10 (restore) |
| locked `prompt_edge` (doctor / watch carry the count) | t-9 |

## Disagreements

**D1 — Does the doctor / watch detector get built?**
risk (t-9) and verification (t-8) both build it; mvp deliberately drops it,
arguing no criterion asks for it and `dross issue reap` with no flags already IS
the detector, at the cost of two more whole-board API sweeps per doctor run.
**Default taken: build it (t-9).** `prompt_edge` is a *locked* decision naming
`dross doctor` and `/dross-watch` explicitly; a locked decision left
unimplemented is a spec deviation, not a scope call. Matters because the whole
phase's premise is that strandedness accumulates invisibly — mvp's version ships
a sweep nobody is ever told to run. mvp's cost objection is real and unresolved:
if the doctor sweep proves too expensive, the honest fix is caching or a
`--no-board` skip, not dropping the surface.

**D2 — Is the verb rename one atomic task or split Go/prompts?**
mvp: one task, ~25 files, because `verb_naming` bans aliases so there is no
midpoint where the Go tree and the prompts agree. risk and verification: split
(Go tree + test call sites first, prompt corpus second).
**Default taken: split (t-1 / t-5).** Two-of-three, and the failure modes differ
— a missed call site fails at build, a missed prompt line fails silently at
runtime months later, and an 18-file commit mixing both is unreadable when it
goes red. Matters because the window between t-1 and t-5 leaves prompts emitting
verbs that no longer resolve; nothing executes them in the suite, but the branch
is briefly wrong if execution stops mid-phase.

**D3 — Where does the undo journal live?**
mvp: a `Reaped []ReapRecord` slice on board.Board, arguing board.json is already
git-tracked and loaded on every `dross issue` call, and a slice is invisible to
the map-typed namespace reflection. risk: a new `internal/reaplog` package.
verification: `internal/board/reaplog.go`, both writing `.dross/reap-log.json`.
**Default taken: standalone `.dross/reap-log.json` in `internal/reaplog`
(risk).** Two-of-three for a standalone file, and board.go's package doc commits
to holding "nothing but the cross-references" — which argues against
verification's placement inside `internal/board` too. mvp's technical claim
checks out (Board's five namespaces are all maps; `Dismissed []string` already
sits there unguarded), so the objection to a slice field is editorial, not
mechanical. Matters for whether an audit trail with prior states is git-tracked
and merge-conflicting on every branch.

**D4 — Is the journal its own task, or part of undo?**
risk: separate wave-1 task, so apply writes into an existing ledger. mvp and
verification: folded into the undo task, with verification arguing the
pre-close-capture assertion needs to live with the task that can fail it.
**Default taken: risk's split (t-3 wave 1), with verification's capture contract
grafted onto t-8 instead of t-10.** Ordering journal → apply → undo means the
apply task never ships a write path that loses prior state, and the capture
assertion is still owned by the task that performs the write. Matters because
under the folded version, t-8 lands an apply that can close 90 cards
irreversibly and t-9/t-10 retro-fits the capture.

**D5 — Is marker discovery independent of the classifier?**
risk: wave 1, no deps — attribution by label needs no taxonomy, and keeping it
independent lets the dry run consume a complete inventory immediately. mvp:
folded into the classifier as a second pass inside `classifyReap`. verification:
separate task depending on t-2, running orphans through the SAME verdict.
**Default taken: verification (t-4, depends t-2).** One verdict path is the
point of c-3; risk's independent version risks a second attribution rule that
can disagree with the first. Matters because a discovery path with its own
verdict logic is exactly where an over-close hides — the 6 unlinked cards have no
board.json entry to sanity-check against.

**D6 — Where do README/ARCHITECTURE go?**
risk folds them into the prompt-repoint task; mvp folds them into the rename;
verification gives them a final task that also documents reap.
**Default taken: verification's separate final task (t-11).** It is the only
placement where the docs can describe reap's finished behaviour including undo,
and it carries a real guard (docs walked against the cobra tree) rather than a
prose edit. Matters mildly: if the phase is cut short, t-11 is the safest task to
drop, and the other two placements would have buried a droppable doc edit inside
a load-bearing one.

**D7 — What does `--undo` do with a card a human moved after the sweep?**
risk: skip it and name it — restoring blindly overwrites a deliberate post-sweep
move. mvp and verification: restore unconditionally to the recorded prior state.
**Default taken: risk's refusal rule.** c-9 says "restores those cards to the
state they held before the run", which is silent on conflict, and the
conservative reading loses nothing a human can't redo by hand. Matters because
the reverse error is unrecoverable: an undo that clobbers a human's triage has no
undo of its own.

**D8 — Where does the coverage guard live?**
risk: a new `internal/cmd/issue_reap_coverage_test.go` with its own class table.
mvp and verification: extend the existing `mirrorLanes` registry in
`board_lifecycle_divergence_test.go` with a reap class/terminal column.
**Default taken: extend mirrorLanes (t-7).** Two-of-three, and one lane
vocabulary cannot disagree with itself. Note that risk's stated basis for the new
file — "the shape namespace_guard_test.go already uses" — is a phantom: no such
file exists; `boardNamespaceFields`, `mirrorLanes` and `stateMapPairs` are all in
`board_lifecycle_divergence_test.go`.

**D9 — Is idempotence a classifier rule or an apply-time skip?**
risk and verification: the classifier itself drops any card the tracker already
reports resolved, so the post-sweep dry run prints an empty plan. mvp: the
classifier still lists them and `--apply` skips what `boardIssueIsDone` reports
resolved.
**Default taken: classifier rule (t-2).** c-4 says the re-run "classifies zero
cards as stranded", which is a statement about classification, not about writes.
Matters because mvp's version makes the second dry run print 90 cards and write
none — the human reading the plan cannot tell a swept board from a stranded one.
