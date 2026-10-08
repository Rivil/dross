# Planner panel synthesis: extracted-package-test-parity

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 5/5. All four criteria. c-1 includes the 16 routed survivors outside c-2's files and has an explicit ceiling policy. c-3 gets its own gate after the drain. | 4/5. Mutant-level contracts with exact outputs, and an `owns:` line per task. Some line labels are wrong: the `""`→"version" CheckMilestoneClosable case is credited to milestone.go:46, which is EnsureMilestoneLink's missing-toml arm. | 5/5. 13 tasks, one failure domain each. backlog.go is split along duplicate-minting vs wrongful-close. | 4/5. The DAG is clean: CI wiring runs alongside the drain, and c-3 is serialised after both. The weak spot is t-10, which pre-accepts predicted ceilings before the drain has produced evidence. |
| mvp | 3/5. All four criteria in name, but the c-3 remote run lands before or during the drain's test additions. The 16 known routed survivors are left to the drain loop. | 2.5/5. Contracts are behaviour-level ("each arm has a row") with no line targets. The drain contract only checks the outcome. The `true`/`false` detach test cannot show that stdout is routed to stderr. | 2/5. Only 5 tasks. t-1 spans 3 files and about 59 routed mutants. t-4 bundles the checker, CI wiring, the detach test and the c-3 gate. t-5 is an open-ended loop. | 2.5/5. Running t-4 alongside t-5 puts two remote jobs on the helicon host lock, and proves c-3 against a tree from before the drain. There is no enabler wave. |
| verification | 5/5. All four criteria, plus an explicit final c-2 measurement and a calibration of the checker against the measured baseline. | 5/5. Every contract names the routed lines it kills. Cross-checked against cmd-package-decomposition's 118 routed survivors, the union matches exactly except reap_apply.go 44/48/56. The self-test runs the real toolchain and has a ci.yml flag-parity pin. | 3.5/5. 11 tasks, but t-3 bundles the writers tests, the detach test and the retire. t-11 bundles CI wiring, the c-2 measurement and the c-3 gate. | 3.5/5. The DAG is correct, but CI wiring waits behind a ~2h drain for a reason that does not hold (see D1). |

**Skeleton: risk.** It is the only draft with no structural defect: CI wiring runs alongside the drain, c-3 is gated after everything, and each task owns one failure domain. Grafted onto it from verification: the line-anchored kill lists, the test doubles with their own self-tests, the real-toolchain self-test and the baseline calibration. Grafted from mvp: the drain task's fallback file list and the go/ast runtime estimate. Risk's separate retire/pre-accept task (its t-10) is dissolved into t-2 and t-11 (see D5).

## Merged plan

Rules for every test task: no new Test name may appear in `internal/cmd/testdata/cli_surface/tests_before.txt`, because TestNoTestLost fails a copy with "copied and left behind" [risk+verification]. No test that captures os.Stderr may call t.Parallel [verification]. Every change under internal/ is a `_test.go` file [all].

```
Phase extracted-package-test-parity — 12 tasks across 4 waves

Wave 1
  t-1  Build boardsync test doubles and strict tracker                      [verification+risk]
       files:    internal/boardsync/doubles_test.go
       desc:     faultBoard wraps sync_test.go's fakeBoard. Per key it can inject failGet, nilGet
                 (GetIssue returns nil, nil), failUpdate, failClose and failCreate. It also supports
                 listErr, refuseClose (the close returns nil but the issue still reads open), and a
                 scripted EnsureMilestone id/error that records title and body.
                 stateBoard adds forge.StateWriter (SetStateRaw, plus a refuse set). linkBoard adds
                 forge.IssueLinker (records calls, can return an error).
                 captureStderr(t, fn) swaps os.Stderr for a pipe and restores it in t.Cleanup.
                 A scripted httptest tracker records method, path and body, and fails chosen routes
                 with 500. It is STRICT (D4): an unscripted request, or a scripted route that is never
                 hit, fails the test.
                 Ctx builders: ytRepoMode(t, h, boardJSON, mode) for YouTrack per milestone_mode, and
                 a Jira builder through Config() the way ytRepo is. Every helper roots Ctx.Root in
                 t.TempDir().
       covers:   c-1, c-2 (enabler)
       contract: - TestFaultBoardInjectsPerKey: failUpdate["K"] makes UpdateIssue("K") return that
                   error and leaves K's labels untouched, while UpdateIssue("L") succeeds. Under
                   refuseClose, CloseIssue returns nil and GetIssue still reads State "open".
                   [verification]
                 - TestDoubleCapabilitiesAreExact: faultBoard satisfies neither forge.StateWriter nor
                   forge.IssueLinker; stateBoard and linkBoard do. This keeps t-8's refusal test from
                   passing vacuously. [verification]
                 - TestCaptureStderrRestoresTheStream: the returned text is exactly what fn wrote.
                   Afterwards os.Stderr is the original *os.File, including after a recovered panic
                   inside fn. [risk+verification]
                 - TestScriptedTrackerReportsUnscriptedAndUnusedRoutes: an unscripted GET and a
                   scripted route that is never called are both reported. Removing the unused-route
                   check fails the test. The tracker records the body of POST /api/issues, and a
                   configured path's 500 surfaces as a client error. [risk+verification]
                 - `git status --porcelain .dross` is byte-identical before and after
                   `go test -count=1 ./internal/boardsync/`: reap-log.json, deferred.toml and
                   board.json never land in the real repo. [risk]
       depends:  —

  t-2  Test writers.go in-package; retire the 18 acceptances                  [risk+mvp+verification]
       files:    internal/secretscan/writers_test.go, .dross/survivors.toml
       desc:     Port the intent of cmd's TestWriterRegistryValidate under NEW names. Include the arms
                 cmd never fed: blank File, 3 dispositions, a "/"-absolute artifact, machine-local
                 with no Path, and machine-local with no Why. Also check that the live registry
                 validates clean, ArtifactNames ordering, and that Writers() returns a copy.
                 internal/cmd is not touched.
                 Then run `make install` (r-01), and `dross survivor retire` the 18 writers.go keys in
                 cross-package-only-coverage. Retire prunes the orphaned category. Nothing is
                 re-accepted here (D5).
       covers:   c-1, c-2
       contract: - TestWriterValidationNamesEveryMalformedShape: each malformed entry yields exactly one
                   error containing its needle:
                     "empty File", "no disposition", "2 dispositions", "3 dispositions",
                     "no Artifacts", "must be .dross-relative" (for both ".dross/a" and "/a"),
                     "machine-local with no Path", "no IgnoreSeed", "machine-local with no Why",
                     "outside-dross with no Why", "under .dross by definition", "declared twice".
                   Mutating set++ to set-- drops the "2 dispositions" error. [risk+verification]
                 - TestLiveWriterRegistryValidatesInPackage: ValidateWriters(Writers()) is empty.
                   Negating any guard at 106/110/121/130/133/136/140/144 makes a good entry
                   error. [risk+verification]
                 - TestArtifactNamesSpansEveryDisposition: [UnderDross{a,b}, MachineLocal{p},
                   OutsideDross{x,y}, {}] returns exactly [a b p x y]. [risk+verification]
                 - TestWritersReturnsACopy: overwriting Writers()[0] leaves a second call's [0]
                   unchanged. [risk+mvp+verification]
                 - writers.go is >=80% own-package under `go test -coverprofile ./internal/secretscan/`
                   (expect ~100%). [all]
                 - `grep -c cross-package-only-coverage .dross/survivors.toml` == 0.
                   `go test -count=1 -run TestRepoAcceptanceReasonsCiteRealTests ./internal/survivor/`
                   passes. [all]
       depends:  —

  t-3  Build coverfloor checker, self-test and detach test                    [risk+mvp+verification]
       files:    cmd/coverfloor/main.go, cmd/coverfloor/floor.go, cmd/coverfloor/floor_test.go,
                 cmd/coverfloor/fixture_test.go, internal/verify/detach_test.go
       desc:     Usage: `go run ./cmd/coverfloor <profile>`. The profile parser is stdlib-only (D8) and
                 follows cmd/testsummary's run() pattern. The checker reads the module path from
                 go.mod. It keys blocks by file+span, so a duplicated block counts once at its max
                 count. It sums statements per file under <module>/internal/, excluding
                 internal/cmd/, and fails any file where 2*covered < total.
                 Exit codes: 1 names the offenders. 2 means bad usage, a missing or malformed profile,
                 or zero in-scope files. On a pass it prints
                 "N files measured, lowest <file> <pct>".
                 The 50% floor is a const. There are no flags, no env and no allowlist.
                 Also add a helper-process test for verify.RunDetachArgv (D9).
       covers:   c-4
       contract: - TestFloorBoundary: 1/2 and 2/4 pass; 99/200, 1/3 and 0/5 fail. A `<` to `<=` mutant,
                   float rounding, or a moved const each fail a row. [risk+verification]
                 - TestScopeExcludesOnlyInternalCmd: internal/cmd/x.go at 0% passes.
                   internal/cmdx/a.go and internal/cmdline/x.go at 0% fail (the prefix needs its
                   trailing slash). cmd/dross/main.go and cmd/testsummary/main.go at 0% are
                   ignored. [risk+mvp+verification]
                 - TestDuplicateBlocksCountOnce: one block listed twice (count 0, then count 3) counts its
                   statements once, as covered. [risk+verification]
                 - TestVacuousInputIsExitTwo: each of these exits 2, never 0:
                     - a missing profile
                     - a profile with only the mode line
                     - a profile with only internal/cmd files, or only unprefixed paths
                     - a malformed line (non-numeric count, missing span); these are not skipped the
                       way survivor.ParseProfile skips
                     - a stray flag or an extra argument
                   [risk+mvp+verification]
                 - TestFixtureBelowFloorFailsTheCheck is the c-4 self-test (D6). It writes an inline
                   fixture module to TempDir (the cmd/testsummary/fixture_test.go pattern), runs the
                   real `go test <fixtureCoverFlags> ./...`, then calls run(). Expected: exit 1, with
                   exactly these offenders:
                     - internal/low/low.go (25%)
                     - internal/untested/u.go (0%, no test files)
                     - internal/lib/lib.go (0% even though internal/user's tests call it)
                     - internal/cmdline/x.go
                   Not reported: internal/edge/edge.go (exactly 50%) and internal/cmd/c.go.
                   [verification]
                 - Calibration (evidence in the commit, not a test): run the checker over an
                   own-package, non-cmd profile of the base tree. Before detach_test.go lands it
                   lists exactly backlog.go, reap_apply.go, reap_undo.go, inbound.go, milestone.go,
                   writers.go and verify/detach.go. After, it lists the same six without
                   detach.go. [verification]
                 - TestDetachArgvRoutesChildStdoutToStderr:
                     - RunDetachArgv([os.Args[0], -test.run=^TestDetachHelperProcess$]) returns nil,
                       and the helper's stdout sentinel appears on the captured os.Stderr, not on
                       os.Stdout.
                     - A helper that exits 3 gives *exec.ExitError with ExitCode()==3.
                     - A nonexistent argv[0] returns an error.
                   detach.go reaches 100%. [risk]
       depends:  —

Wave 2 (t-4..t-9 depend t-1)
  t-4  Test the backlog push half                                            [risk+verification]
       files:    internal/boardsync/backlog_push_test.go
       desc:     Covers PushBacklogItems, resolveBacklogIssue, adoptLegacyBacklogKey, lookupPhaseIssue
                 and DeferredBacklogItem, on faultBoard and linkBoard. The YouTrack paths run on the
                 strict tracker. (D7)
       covers:   c-1, c-2
       contract: - TestPushBacklogCreatesThenUpdatesByIDKey:
                     - The first push of [plain, routed→x] gives (2,0), carrying
                       dross/deferred:<id>, and dross/target:x on the routed item.
                     - The second push gives (0,2) and creates nothing.
                     - Re-routing to y leaves dross/target:y as the ONLY target label.
                     - An update with empty Labels keeps the issue's labels; a create with no labels
                       carries [dross].
                     - A non-YouTrack milestone "7" gives IssueInput.Milestone==7.
                   Kills 399, 402, 405×2, 412, 419, 423, 447, 452. [verification+risk]
                 - TestResolveBacklogAdoptsMarkerBearingIdentityMatch: with board.json empty, the
                   lowest-keyed marker-bearing identity match is adopted (0 created, 1 updated). A
                   same-label issue without the marker is skipped. A ListIssues error returns a
                   "board:" error and creates nothing. Kills 317, 319, 330×2. [verification+risk]
                 - TestLegacyBacklogKeyAdoptedOnlyOnTitleMatch: on a title match, the id key is set,
                   the legacy key is deleted, and nothing is created. On a title mismatch or a
                   GetIssue failure, the legacy link is untouched and a fresh issue is created.
                   Kills 49, 57×3. [all]
                 - TestPushBacklogYouTrackVersionAndEpicModes:
                     - Modes "version" and "" send the cached fixVersion and no subtask command.
                     - Mode "epic" sends CreateBacklogItem, then LinkSubtask(epic,key), then the tag
                       patch, in that order.
                     - A failed create or a LinkSubtask 500 returns an error and records no link.
                   Kills 362×2, 429×3, 433. [verification+risk]
                 - TestLinkRoutedWarnsOncePerRunWithoutLinker:
                     - On faultBoard, two routed items give exactly one "cannot link issues" line.
                     - On linkBoard, a target with no phase issue warns, naming the phase, and makes
                       no LinkIssues call.
                     - A target with a phase issue records LinkIssues(item, target).
                     - A LinkIssues error only warns; the item still counts.
                   Kills 375, 388, 392. [verification+risk]
                 - TestLookupPhaseIssueNeverCreates: a cache hit returns the key. The label query
                   filters on the marker and takes the lowest key. A list error or no match returns
                   "" with zero creates. Kills 288, 297. [verification]
                 - DeferredBacklogItem: an id-less entry gets no Identity label. A routed entry is
                   titled "[routed] " and its body names the target. Kills 259, 263, 267, 270.
                   [verification]
                 - Failure paths: a scripted EnsureMilestone error aborts before any create (350). An
                   unwritable BoardPath returns the Save error (454). (created, updated) is asserted
                   exactly everywhere. Each of the five functions is >=85% in
                   `go tool cover -func`. [verification+risk]
       depends:  t-1

  t-5  Test the backlog sync and reconcile half                              [risk+verification]
       files:    internal/boardsync/backlog_reconcile_test.go
       desc:     Covers SyncBacklog, ReconcileBacklog, BacklogVerdictFor and IssueIsDone over a
                 TempDir .dross root, built with writeMilestoneToml, writeSpec and phase dirs. (D7)
       covers:   c-1, c-2
       contract: - TestSyncBacklogMirrorsUnscaffoldedSlugsAndLiveDeferred: milestone v1 has phases
                   [shipped (has a dir), todo] and deferred items [plain, routed, dismissed].
                     - Exactly "[backlog] todo", "[someday] …" and "[routed] …" are created.
                     - Ctx.Out reads "backlog v1 -> 3 created, 0 updated, 0 closed".
                     - board.json on disk holds the 3 keys.
                     - A second run prints "0 created, 3 updated".
                   Kills 78, 82, 83, 92, 106, 110, 113. [verification+risk]
                 - TestSyncBacklogMissingMilestoneNamesIt: an absent v9.toml returns an error
                   containing `load milestone "v9"`, with zero board calls. Kills 71.
                   [verification+risk]
                 - TestReconcileBacklogClosesOnlyProvablyResolvedMirrors:
                     - A scaffolded slug is closed and its key deleted.
                     - A slug with no phase dir is left open, with a stderr warning naming the issue
                       and key.
                     - A dismissed deferred item (by id key and by legacy key) is closed.
                     - A live routed item whose target is done is closed, with its key KEPT.
                     - A live routed item whose target is open is untouched.
                     - An already-resolved mirror gets zero CloseIssue calls.
                     - An empty issue id is skipped.
                     - The returned count is exact.
                   Kills 150, 159, 187, 204, 208, 211, 221. [all]
                 - TestReconcileBacklogKeepsLinkWhenCloseFails: with failClose, stderr says "link is
                   kept", the key stays, and the mirror is not counted. With failGet, a "could not
                   read" warning is printed and the mirror stays open. Kills 177, 183.
                   [verification+risk]
                 - TestBacklogVerdictTable: one row per BacklogVerdictFor branch, each asserting its
                   exact verdict. The rows: live unrouted; live routed with no target issue; target
                   done; target open; target GetIssue error; slug with a dir; slug without a dir;
                   deferred dismissed; deferred live; legacy positional key; unknown key.
                   IssueIsDone: Resolved gives true, State "closed" gives true, open gives false, a
                   nil issue gives (false, nil), and an error is prefixed "board:". [all]
                 - Each of the four functions is >=85%. backlog.go is >=80% once t-4 and t-5 have
                   both landed. [verification+risk]
       depends:  t-1

  t-6  Test inbound feed and milestone linking                               [risk+verification]
       files:    internal/boardsync/inbound_test.go, internal/boardsync/milestone_test.go
       desc:     Covers CollectInbound, EmitPullEnvelope, ReportBoardFailure, EnsureMilestoneLink
                 (on faultBoard and on the strict tracker in epic and Jira modes),
                 CheckMilestoneClosable and MilestoneBody.
       covers:   c-1, c-2
       contract: - TestCollectInboundDropsDrossAuthoredIssues: the input is 2 human issues, one linked
                   in any board.json namespace, one dismissed, and one marker-labelled. Exactly the 2
                   human issues are returned, and each exclusion row fails if its check is removed. A
                   ListIssues error returns a "board:" error and a nil feed. Kills 20. [all]
                 - TestPullEnvelopeShape: EmitPullEnvelope(nil,nil) prints exactly
                   {"issues":[],"error":null} plus a newline. With an error it carries
                   "error":"<msg>". One issue appears as issues[0]. Kills 53, 56, 61.
                   [verification+risk]
                 - TestReportBoardFailureByMode:
                     - JSON: the envelope, and a nil return.
                     - Human, fatal: returns the error and prints nothing.
                     - Human, non-fatal: prints "board unreachable: <err>" and returns nil.
                   [all]
                 - TestEnsureMilestoneLinkRecordsBoardID:
                     - Cached: returns the id with zero EnsureMilestone calls.
                     - Missing toml: ("", nil) with zero calls (46).
                     - Titled: calls EnsureMilestone(title, "title\n\nSuccess criteria:\nc1\nc2")
                       and records the id.
                     - Untitled: the title is the version (50).
                     - Board error: a "board:" error, nothing recorded (68).
                     - Backend returns "": ("", nil), nothing recorded (71).
                     - YouTrack epic mode records the epic's idReadable.
                     - The Jira version path hits its scripted route.
                   [verification+risk]
                 - TestCheckMilestoneClosableOnlyForYouTrackEpic: faultBoard is refused, naming the
                   provider. YouTrack with mode "" is refused quoting "version". Modes "agile" and
                   "version" are refused, naming the mode. " Epic " returns nil.
                   MilestoneBody("t","") == "t". [all]
                 - inbound.go and milestone.go are each >=80%. [all]
       depends:  t-1

  t-7  Test reap apply isolation and journal                                 [risk+verification]
       files:    internal/boardsync/reap_apply_test.go
       desc:     Covers Apply, relabelReapedCard, dropBacklogLink, appendReapRun, BoardNamespaceNames,
                 ValidateReapNamespaces, Inventory and reapFailure. Apply runs on faultBoard.
                 Inventory reuses discoverYT/discoverRepo and readOnlyYT.
       covers:   c-1, c-2
       contract: - TestApplyJournalsPriorStateBeforeClosing: 3 cards (Phases, Tasks and Backlog lanes),
                   each prior "open" with labels [dross, dross/phase:p, dross/status:uat].
                     - All three close.
                     - The last run records PriorState "open" (not "closed") and the prior labels.
                     - Each card ends with its identity labels plus dross/status:<terminal>.
                     - Only the Backlog card's key is dropped, and it is journalled as DroppedLink.
                     - Ctx.Out reads "reaped 3 card(s)".
                   Kills 144, 164, 171, 174, 177, 182, 241. [verification+risk]
                 - TestApplyIsolatesPerCardFailures: failGet on A, nilGet on B, refuseClose on C, and
                   D healthy.
                     - D closes; A, B and C are journalled Failed.
                     - B's failure reads "read prior state: issue not found".
                     - The error reads "3 of 4 card(s) could not be closed", and stderr names A, B
                       and C.
                     - A failUpdate relabel on D only warns, and D still counts (215).
                   Kills 125×2, 126. [all]
                 - TestRelabelKeepsIdentityLabelsAndSkipsNoOps:
                     - No prior labels: no panic, and the result is [dross/status:T] (200).
                     - The only status label already equals T: zero UpdateIssue calls (211×3).
                     - Right label plus a stale one: exactly one status label (204).
                   [verification+risk]
                 - TestApplyWithNothingClosedWritesNoJournal: an empty plan leaves no reap-log.json
                   (253). A no-op apply after a real one leaves Last() unchanged. A corrupt existing
                   log returns the load error (258). [verification+risk]
                 - TestBoardNamespaceNamesAreTheMapFields: the result is
                   [Backlog Milestones Phases Quicks Tasks]. Kills 279×3, 280. [verification]
                 - TestValidateReapNamespacesNamesUnknowns: nil or empty input returns nil.
                   " BACKLOG " and " Phases " return nil. ["phases","bogus","nope"] errors naming
                   "bogus, nope" and listing all 5. Kills 291, 304×2. [verification+risk]
                 - TestInventoryDedupesLinkedAndOrphanCards: a card that is both linked and discovered
                   is planned once, and unclassifiable cards are returned. An unknown namespace is
                   refused before any HTTP request (40). A discovery 500 returns an error, not a
                   partial plan (52). Also: reapLogPathFor(root)==reaplog.FilePath(root), and
                   reapFailure's Error() is "K: msg" with errors.Unwrap returning the inner error.
                   [verification+risk]
                 - reap_apply.go is >=80%. [all]
       gap:      Routed lines 44, 48 and 56 (Inventory's Classify, resolveReapLanes and buildReapPlan
                 error returns) carry no contract in ANY draft. t-11's drain is their only backstop.
       depends:  t-1

  t-8  Test reap undo and the apply-to-undo round trip                       [risk+verification]
       files:    internal/boardsync/reap_undo_test.go
       desc:     Extends the existing file with Undo and restoreDroppedLink tests on stateBoard and
                 faultBoard, plus one Apply-then-Undo round trip.
       covers:   c-1, c-2
       contract: - TestUndoRefusesClientWithoutStateWriter: on faultBoard, returns an error naming the
                   provider and "nothing was written". Zero SetStateRaw/UpdateIssue calls even with a
                   corrupt ledger, and board.json bytes are unchanged. [all]
                 - TestUndoWithEmptyLedgerSaysNothingToUndo: with no ledger, prints "nothing to undo"
                   and returns nil (43). A corrupt ledger returns an error (38).
                   [verification+risk]
                 - TestUndoWritesPriorStateThroughStateWriter: the last run holds closed A (with
                   labels), closed B (no labels, DroppedLink Backlog), failed C, and closed D (class
                   Tasks, DroppedLink).
                     - SetStateRaw runs for A, B and D only.
                     - A's labels are restored. B gets no label patch (61).
                     - B's backlog key is back in board.json. D's key is NOT put into Backlog (93).
                     - Ctx.Out reads "restored 3 card(s)".
                   Kills 51, 61×2, 63, 67, 70, 73, 78. [verification+risk]
                 - TestUndoContinuesPastSetStateFailure: when SetStateRaw fails on A, the others are
                   still restored. The error reads "1 of 3 card(s) could not be restored" and stderr
                   names A. A failed label restore warns "could not put its labels back" and the
                   count is unchanged. [verification+risk]
                 - TestUndoOnlyReversesTheLastRun: with two runs journalled, only the last run's
                   cards are written. [verification+risk]
                 - TestApplyThenUndoRoundTrip: Apply then Undo on stateBoard leaves every card's State,
                   its label set, and the board.json backlog map reflect.DeepEqual to the pre-Apply
                   snapshot. [verification+risk]
                 - reap_undo.go is >=80% (expect 100%). [all]
       depends:  t-1

  t-9  Kill the routed survivors in phase/task/pull/reap                     [risk+verification]
       files:    internal/boardsync/sync_paths_test.go
       desc:     Targets the 16 routed keys outside c-2's six files: phase.go 65/94/138/147/198,
                 task.go 78/217×2/257/261, task_pull.go 49/53/66-68, and reap.go 472. The YouTrack
                 and Jira paths use t-1's strict tracker. mvp rejects this task (D3).
       covers:   c-1
       contract: - TestPhaseSyncAdoptsLegacyTitleOnlyWithMarker: a same-title issue with the marker is
                   adopted (0 creates). Without the marker, a new issue is created. Kills phase.go:65.
                 - TestPhaseSyncAssignsDeclaredMilestone: a spec declaring v1 creates the issue with
                   Milestone 7 and records v1→7. With no milestone, Milestone is 0 and EnsureMilestone
                   is never called. Kills phase.go:94.
                 - TestPhaseSyncSurfacesTrackerStateWriteFailure:
                     - A YouTrack state POST 500 or a Jira transition 500 makes SyncPhase, CloseIssue
                       and SyncTasks each return a "board:" error.
                     - A Jira SetState 500 during CloseIssue errors with no read-back GET.
                     - Status "" sends no state request; the strict tracker's unused-route check
                       fails the test otherwise.
                     - A successful write saves the phase link.
                   Kills phase.go:138/147/198 and task.go:257/261.
                 - TestTaskSyncLinkFailureWarnsOnce: with LinkIssues failing, a two-task phase writes
                   exactly one stderr warning and both issues are created. Kills task.go:78.
                 - TestTaskIssueResolutionWarnsOnDuplicates: with two marker issues on one task label,
                   the lowest key is updated and stderr names both keys. A single match gives no
                   warning. Kills task.go:217×2.
                 - TestTaskPullRefusesFlatBoards: provider github returns an error naming "github",
                   "has no workflow state" and "youtrack or jira". No board call is made and
                   plan.toml bytes are unchanged (49). Covering 66-68 makes those string-concat
                   mutants not viable.
                   TestTaskPullSurfacesListFailure: on youtrack with listErr, the error is returned
                   and plan.toml is byte-identical (53).
                 - TestQuickMirrorWithEmptyIssueIsSkipped: for Quicks {q1:"", q2:"P-9"},
                   classifyQuickMirrors yields only P-9, and its Why names the ref. Kills reap.go:472.
       depends:  t-1

Wave 3
  t-10 Wire the coverage floor into the CI test job                          [risk+verification+mvp]
       files:    .github/workflows/ci.yml, cmd/coverfloor/ci_wiring_test.go
       desc:     Add -coverprofile="$RUNNER_TEMP/cover.out" to the test job's ONE existing go test
                 line. Add a following step: `go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"`.
                 Forbidden: a new job, a second go test, -coverpkg, upload-artifact, and ${{ }}
                 inside run.
                 Read both ~/.claude/memory CI references first. Audit ci.yml against the supply-chain
                 checklist, surface the findings, and offer fixes in the same edit. Runs alongside
                 t-11 (D1).
       covers:   c-4
       contract: - TestCoverFloorIsWiredIntoCI reads ci.yml and fails if any of these hold:
                     - the test job's go test lacks -coverprofile=<p>;
                     - it carries -coverpkg;
                     - its flags differ from t-3's fixtureCoverFlags;
                     - no later step in job `test` runs `go run ./cmd/coverfloor <p>` with the
                       same <p>;
                     - that step carries continue-on-error or `|| true`.
                   [verification+risk+mvp]
                 - The existing pins pass unedited, run TARGETED:
                   `go test -count=1 -run 'TestCIGoTestStepsLive|TestGoTestStepScanner|
                   TestCIConcurrencyLive|TestToolchainSingleSource|TestWorkflowActionsArePinned|
                   TestWorkflowsHaveNoExpressionsInRun|TestCIShellcheckCoversScripts|
                   TestGovulncheckPinAgrees' ./internal/cmd/`. TestCIGoTestStepsLive must still find
                   exactly one go test in `test` and one in `mutation-ts`. [risk+verification]
                 - Pre-flight under a runner-like PATH (gitleaks, ast-grep, gremlins, npx, node and
                   dotnet stripped):
                     go test -count=1 -coverprofile=$T/c.out $(go list ./... | grep -v '/internal/cmd$')
                     go run ./cmd/coverfloor $T/c.out
                   must exit 0. A file that fails only here means STOP and ask (defer or add a test).
                   An allowlist is never an option. [risk+verification]
                 - Ship-time evidence: the PR run shows the floor step green with its file count, and
                   testsummary shows no new >300s warning for internal/cmd. This can only be observed
                   at ship, because ci.yml runs only on pull_request and push to main.
                   [risk+verification]
                 - The supply-chain audit is recorded: actions SHA-pinned, permissions contents: read,
                   GOFLAGS/GOPROXY/GOSUMDB set, govulncheck pinned. The new step adds no
                   dependency. [all]
       depends:  t-2, t-3, t-4, t-5, t-6, t-7, t-8

  t-11 Drain the three packages to zero outstanding                          [risk+mvp+verification]
       files:    .dross/survivors.toml, .dross/phases/extracted-package-test-parity/notes.md;
                 residual kills go into the owning wave-2 test file. For files no task owns, they go
                 into internal/diag/{trust,diag}_test.go, internal/secretscan/secretscan_test.go, or
                 internal/boardsync/{sync,reap_classify,reap_discover}_test.go [mvp]
       desc:     Run `make install` (r-01). Estimate the runtime from a go/ast mutant-site count, then
                 launch this detached via nohup on the mutation host, with a 2h budget:
                   dross survivor drain --packages ./internal/boardsync,./internal/diag,./internal/secretscan
                                        --phase extracted-package-test-parity
                 For each outstanding survivor, act on its evidence line:
                   - "attribution ceiling" (ceiling-eligible=yes): accept with
                     --category gremlins-attribution-ceiling (D2).
                   - "no coverage block": accept with --category const-initializer-arithmetic.
                   - "killable" or "line never executes": STOP and ask (pair mode), then add the
                     killing test in the owning test file.
                 If the only residuals are acceptances, re-classify the recorded reports with
                 `--report`. Re-run gremlins only when a test was added (D5). Record the final summary
                 in notes.md.
       covers:   c-1
       contract: - The final drain summary reads "… 0 outstanding" and the command exits 0.
                   survivor_drain.go errors on any survivor with no disposition. [all]
                 - Checked with `dross survivor list --json`: no acceptance is in
                   cross-package-only-coverage. Every acceptance on internal/{boardsync,diag,
                   secretscan} is in gremlins-attribution-ceiling or const-initializer-arithmetic;
                   all 14 pre-existing secretscan entries already are. [verification+mvp]
                 - Each accepted key's evidence line said "attribution ceiling" or "no coverage block".
                   Paste that transcript into the commit. A survivor marked killable or not covered is
                   never accepted. [all]
                 - No acceptance exists for writers.go 106/110/112/121/130/133/136/140/144. None of the
                   18 retired keys comes back as outstanding. [risk+mvp]
                 - Predicted ceilings, to be accepted only if the drain confirms them: writers.go 88,
                   90, 92, 116, 118×2, 120, 129, 139, and diag/redproof.go 91 and 94.
                   [risk+verification]
                 - No category is invented for an equivalent mutant: STOP and ask instead. [risk]
                 - TestRepoAcceptanceReasonsCiteRealTests passes after the accepts.
                   [risk+verification]
       depends:  t-2, t-4, t-5, t-6, t-7, t-8, t-9

Wave 4 (depends t-10, t-11)
  t-12 Prove no behaviour change; take the final c-2 measurement             [risk+verification]
       files:    .dross/phases/extracted-package-test-parity/notes.md
       desc:     The behaviour-preservation gate over the full internal/cmd suite. It runs remote only,
                 and after the drain, so the two remote jobs never contend for the helicon host lock
                 (D1).
       covers:   c-2, c-3
       contract: - `git diff --name-only 018d325...HEAD -- internal/cmd` is empty. 018d325 is the
                   verified merge-base with milestone/v1.7. [risk+verification]
                 - `git diff --name-only 018d325...HEAD -- '*.go' ':!*_test.go'` lists only
                   cmd/coverfloor/*.go. A defect found while testing becomes a `dross deferred add`,
                   not an edit. [all]
                 - Before the remote run, confirm every Test name added this phase is absent from
                   tests_before.txt. [risk]
                 - Remote `dross test` (full suite, never --local) is green. That includes
                   TestNoTestLost, TestNoTestLostDetectsDropsAndCopies, issue*_test.go,
                   doctor*_test.go and TestCLISurfacePinned. [all]
                 - c-2: the per-file statement-weighted measure over
                   `go test -count=1 -coverprofile ./internal/boardsync/ ./internal/secretscan/` shows
                   backlog.go, reap_apply.go, reap_undo.go, inbound.go, milestone.go and writers.go
                   each at >=80.0%. [verification]
       depends:  t-10, t-11
```

## Disagreements

**D1: Where CI wiring and the c-3 gate sit relative to the drain.**
- **mvp:** CI wiring and the c-3 remote `dross test` share one wave-2 task, which runs alongside the drain. mvp concedes the c-3 run won't see tests the drain adds.
- **verification:** CI wiring, the final c-2 measurement and the c-3 gate share one wave-4 task after the drain, "so CI never goes red mid-phase".
- **risk:** CI wiring is in wave 3, alongside the drain; it only needs the six files at >=50%. The c-3 gate is its own wave-4 task after both.
- **Default:** risk's split (t-10 runs alongside t-11, then t-12).
- **Why it matters:**
  - Under mvp's order, the c-3 evidence predates the drain's test additions, so the final tree is never proven. Both remote jobs would also queue on the helicon host lock.
  - Verification's premise is false. ci.yml triggers only on `pull_request` and `push: branches: [main]`, so no CI run exists on the phase branch before ship, whenever the step is wired. Waiting ~2h for the drain buys nothing.
  - The same fact means the "first PR CI run shows the floor green" contract, shared by risk and verification, is only observable at /dross-ship. The merged t-10 labels it ship-time evidence and gates on the local preflight.

**D2: Which category the switch-case ceiling survivors are accepted under.**
- **risk:** a NEW category whose reason cites t-2's tests and TestPinLinesMatrix. Risk rejects gremlins-attribution-ceiling as generic.
- **verification:** the existing gremlins-attribution-ceiling (with const-initializer-arithmetic for "no coverage block"). Verification rejects both a new writers-specific category and gremlins-switch-case-ceiling.
- **mvp:** any of four existing categories, including gremlins-switch-case-ceiling and gremlins-var-initializer.
- **Default:** gremlins-attribution-ceiling only, plus const-initializer-arithmetic.
- **Evidence for the default:**
  - The drain's own evidence line says "accept it into the ceiling category" (internal/survivor/evidence.go).
  - In the same package, sinks.go 102/104/106/110/114/116 have the identical `case set == 0` / `case set > 1` validator shape, and payload.go 85 is also a case line. All of them already sit in gremlins-attribution-ceiling.
  - TestAttributionCeilingIsReal proves the mechanism.
- **Why it matters:**
  - A brand-new category created right after retiring cross-package-only-coverage is the rename-shaped move guard_shape warns about.
  - mvp's list admits gremlins-switch-case-ceiling, whose prose cites Classify tests. TestRepoAcceptanceReasonsCiteRealTests only checks that cited names exist, so it would pass a reason that is false for these lines.
  - Risk's objection stands, though: the default's reason names no test that drives the writers.go or redproof.go arms. That link lives only in t-2's commit and TestPinLinesMatrix.

**D3: Whether to pre-kill the 16 routed survivors outside c-2's files (t-9).**
- **mvp:** no task. "The drain names them exactly", and c-1 needs kills, not covered blocks.
- **risk and verification:** a dedicated task with line-anchored contracts.
- **Default:** include t-9.
- **Why it matters:** these 16 keys are routed to this phase (cmd-package-decomposition/spec.toml). survivor_drain.go counts a survivor routed to the phase being drained as outstanding, so all 16 are guaranteed to fail the first drain. boardsync is unchanged since the recording run, so the list is exact. Leaving them to the drain loop costs at least one extra gremlins cycle of about 1h40m against a harness reap at about 1h48m.

**D4: Whether to add a shared test-double task, and whether its HTTP fake is strict.**
- **mvp:** no enabler task. Reuse sync_test.go's fakeBoard and the existing httptest YouTrack fakes.
- **verification:** faultBoard, stateBoard, linkBoard, captureStderr and ytRecorder, with self-tests (including capability exactness). ytRecorder records calls and fails chosen paths, but otherwise answers.
- **risk:** a strict scripted tracker that fails on unscripted AND unused routes. Risk rejects answer-everything fakes and porting cmd's stateful emulators.
- **Default:** verification's doubles and self-tests, with risk's strict routing.
- **Why it matters:**
  - The existing fakeBoard hard-codes EnsureMilestone to "7" and has no per-key faults, no StateWriter and no IssueLinker. The error-arm and refusal contracts in t-4..t-9 cannot be written without new doubles, and defining them per task in one package invites name collisions.
  - Strictness: a non-strict recorder lets "status '' sends no state request" and the 500-arm tests pass without ever reaching the tracker. The cost is that every HTTP test in t-4, t-6 and t-9 must script its full route set.

**D5: When ceilings are accepted, and who owns residual kills.**
- **risk:** a separate wave-2 task retires the 18 keys and pre-accepts the predicted switch-case set before any drain, after proving go-cover count >=1 per line. Then one drain, with residuals re-classified via `--report`. A killable residual means STOP and route it back to the owning earlier task.
- **mvp and verification:** retire alongside the writers tests. Accept only on the drain's evidence line. Add killing tests inside the drain task, and re-drain until clean.
- **Default:**
  - Retire in t-2.
  - Accept only after the drain's evidence, in t-11.
  - Graft risk's `--report` re-classification (the flag exists in survivor_drain.go), so accept-only residuals don't cost a gremlins re-run.
  - For a killable residual, STOP and ask, then fix it inside t-11.
- **Why it matters:**
  - Pre-accepting puts acceptances ahead of evidence the drain derives anyway. The drain's ceiling-eligible test is exactly risk's count>=1 proof.
  - Pre-accepting also needs a retire-if-killed cleanup that post-accepting avoids.
  - Where fixes land (a reopened earlier task, or the drain task) changes commit attribution and t-11's file scope.

**D6: The shape of the c-4 self-test.**
- **risk and mvp:** a synthetic profile. For example, internal/x/low.go at 1/3 gives a non-zero exit.
- **verification:** a real `go test` over an inline fixture module (the cmd/testsummary/fixture_test.go precedent). It proves that test-less packages appear at 0% and that attribution is own-package only (no -coverpkg). t-10 adds a ci.yml flag-parity pin.
- **Default:** both. Synthetic unit tests cover the boundary, scope, dedupe and vacuity cases; the fixture is the c-4 self-test.
- **Why it matters:**
  - Only the fixture catches a toolchain or flag change that drops test-less packages from the profile, or that attributes cross-package coverage. That is the blind spot the locked test_home decision forbids.
  - Cost: cmd/coverfloor is production code in the diff, so verify's diff-scoped mutation re-runs a nested `go test` per mutant.
  - The flag-parity pin also couples t-10 to t-3.

**D7: Task granularity for the c-2 test work.**
- **mvp:** backlog, inbound and milestone in one task (3 files, about 59 routed mutants), and reap apply and undo in another.
- **risk and verification:** backlog split into push and reconcile halves; inbound and milestone together; apply and undo separate.
- **Default:** the split (t-4..t-8).
- **Why it matters:** risk explicitly rejects one task for 194 statements and 51 routed mutants, with one test file having two owners. mvp's shape halves the pair-mode checkpoints and parallelism. It changes review granularity, not the end state.

**D8: How the checker parses the profile.**
- **mvp:** golang.org/x/tools/cover. Checked: x/tools v0.50.0 is already a direct require in go.mod, so this adds no new module.
- **risk and verification:** a stdlib-only parser, following cmd/testsummary. Risk also rejects survivor.ParseProfile, which drops statement counts and skips malformed lines.
- **Default:** stdlib-only.
- **Why it matters:** x/tools/cover already merges duplicate blocks and rejects malformed lines, so it would put less production code in the diff for verify's diff-scoped mutation. The stdlib choice follows precedent and keeps the CI tool off x/tools' API. The cost is the hand-rolled dedupe and malformed-line code that t-3's contracts must then pin.

**D9: How detach.go's test works.**
- **risk:** an os.Args[0] helper process. Risk rejects `sh -c` and `true` as shell-dependent.
- **verification:** `sh -c 'echo out; echo err >&2'` and `exit 23`.
- **mvp:** `true` and `false` only.
- **Default:** risk's helper process.
- **Why it matters:** only a little. All three reach 100% of detach.go, which only has to clear the c-4 floor; it has no drain-scope mutants. mvp's form cannot show that stdout is routed to stderr. CI is ubuntu-only (`runs-on: ubuntu-latest`), so `sh -c` would work there. The helper form is the only one that doesn't depend on the host shell.
