# Planner panel — verification lens

Bias: design backward from the test contracts. Each criterion's ideal contract came first; each task is the smallest change that makes one of those contracts satisfiable. The mutant-level kill targets come from the survivors recorded in `.dross/phases/cmd-package-decomposition/verify.toml`: 118 routed here, as `file:line (OP)`. internal/boardsync and internal/diag have not changed since that run (73f0f1f), so those line numbers still hold. internal/secretscan has changed since then, so the drain in t-10 decides what is true for it.

Measure used in every "≥80%" or "≥50%" contract below. It reproduces the 2026-09-26 baseline exactly, weighting each file by statements:

```
go test -count=1 -coverprofile=$T/c.out ./internal/<pkg>/ && \
awk 'NR>1{split($1,p,":");f=p[1];s[f]+=$2;if($3>0)c[f]+=$2}END{for(f in s)printf "%5.1f%% %s\n",100*c[f]/s[f],f}' $T/c.out | sort -n
```

Rules for naming new tests (these protect c-3):
- No new test name may appear in `internal/cmd/testdata/cli_surface/tests_before.txt`. A name copied from the cmd e2e tests (for example TestUndoRestoresTheRecordedPriorState or TestWriterRegistryValidate) would make TestNoTestLost report "copied and left behind".
- No test that captures os.Stderr may call t.Parallel.

```
Phase extracted-package-test-parity — 11 tasks across 4 waves

Wave 1
  t-1  Build coverfloor per-file floor checker
       files:    cmd/coverfloor/main.go, cmd/coverfloor/floor.go, cmd/coverfloor/floor_test.go, cmd/coverfloor/fixture_test.go
       covers:   c-4
       desc:     A stdlib-only CLI, `go run ./cmd/coverfloor <profile>`. It reads the module path from go.mod,
                 totals statements per file (a duplicated block counts once), and exits 1 naming every file
                 under internal/ (except internal/cmd/) where 2*covered < total. It exits 2 on bad usage, a
                 missing profile, or a profile with no in-scope file. On a pass it prints
                 "N files measured, lowest <file> <pct>". The 50% floor is a const. There are no flags and
                 no exemption input.
       contract: TestFloorBoundary: 2/4 statements passes, 1/3 and 0/5 fail. Changing `<` to `<=`, or the
                 const away from 50, fails it.
       contract: TestScopeExcludesOnlyInternalCmd: internal/cmd/x.go at 0% is not reported,
                 internal/cmdline/x.go at 0% IS reported (catches a bad prefix match), and
                 cmd/testsummary/main.go at 0% is not reported.
       contract: TestDuplicateBlocksCountOnce: one block listed twice (count 0 and count 3) adds its
                 statements to the file total once and counts them as covered.
       contract: TestVacuousInputIsExitTwo: a missing profile, a profile with only the mode line, a profile
                 holding only internal/cmd files, and a stray flag or extra argument each exit 2. None of
                 them exits 0.
       contract: TestFixtureBelowFloorFailsTheCheck (the c-4 self-test): writes an inline fixture module
                 to TempDir (same pattern as cmd/testsummary/fixture_test.go), runs the real
                 `go test <fixtureCoverFlags> ./...` over it, then run() on the profile. Expected result:
                 exit 1, with exactly these offenders:
                   - internal/low/low.go (25.0%)
                   - internal/untested/u.go (0%, a package with no test files)
                   - internal/lib/lib.go (0%, even though internal/user's tests call it; this proves
                     own-package attribution, i.e. no -coverpkg)
                   - internal/cmdline/x.go
                 Must not be reported: internal/edge/edge.go (exactly 50%) and internal/cmd/c.go.
       contract: Calibration (evidence captured in the commit, not a test): the checker, run over a
                 non-cmd profile of the base tree, exits 1 and lists exactly the independently measured
                 baseline: backlog.go, reap_apply.go, reap_undo.go, inbound.go, milestone.go, writers.go,
                 verify/detach.go. It lists 6 files if t-3's detach test has already landed.

  t-2  Add fault-injecting boardsync test doubles
       files:    internal/boardsync/doubles_test.go
       covers:   c-1, c-2
       desc:     Test doubles for the rest of the phase:
                 - faultBoard wraps sync_test.go's fakeBoard. Per-key failGet, nilGet (GetIssue returns
                   nil, nil), failUpdate, failClose and failCreate; listErr; refuseClose (the close call succeeds but the issue still reads open);
                   a scripted EnsureMilestone id/error that records its title and body.
                 - stateBoard adds forge.StateWriter. linkBoard adds forge.IssueLinker.
                 - captureStderr(t, fn) swaps os.Stderr for a pipe.
                 - ytRecorder is an httptest YouTrack stand-in that records method, path and body, can fail
                   a chosen path with 500, and builds its Ctx through ytRepoMode(t, h, boardJSON, mode).
       contract: TestFaultBoardInjectsPerKey: failUpdate["K"] makes UpdateIssue("K") return that error and
                 leaves K's labels untouched, while UpdateIssue("L") still succeeds. Under refuseClose,
                 CloseIssue returns nil but GetIssue still reads State "open".
       contract: TestDoubleCapabilitiesAreExact: faultBoard does not satisfy forge.StateWriter or
                 forge.IssueLinker; stateBoard and linkBoard do. If the base double ever gains
                 SetStateRaw, t-7's refusal test would pass without testing anything, and this test fails
                 first.
       contract: TestCaptureStderrRestores: the returned text is exactly what fn wrote, and os.Stderr is the
                 original *os.File afterwards.
       contract: TestYTRecorderRecordsWrites: a forge.YouTrackClient built over ytRecorder records the body
                 of POST /api/issues, and a configured path returns 500 as a client error.

  t-3  Own-package tests for writers.go and detach.go; retire the 18 acceptances
       files:    internal/secretscan/writers_test.go, internal/verify/detach_test.go, .dross/survivors.toml
       covers:   c-1, c-2, c-4
       desc:     Add in-package tests for ArtifactNames, ValidateWriters and Writers, and for
                 verify.RunDetachArgv. Then run `make install` (rule r-01) and
                 `dross survivor retire <18 keys>` for the category cross-package-only-coverage.
                 The store prunes the category once no entry uses it.
       contract: TestValidateWritersNamesEachMalformedEntry: a table with one malformed entry per error
                 branch, each producing exactly one error containing its needle:
                   empty File | no disposition | "2 dispositions set" | no Artifacts |
                   ".dross/x" and "/abs" (both "must be .dross-relative") |
                   machine-local with no Path / no IgnoreSeed / no Why | outside-dross with no Why |
                   agent-authored but not under-dross | "declared twice"
                 A well-formed registry with one entry per disposition yields zero errors, so flipping any
                 `== ""`, `!= nil` or `set++` in ValidateWriters (lines 106–147) fails a row.
       contract: TestLiveWriterRegistryValidatesInPackage: ValidateWriters(Writers()) is empty, and mutating
                 the slice Writers() returns leaves the next Writers() call unchanged.
       contract: TestArtifactNamesSpansEveryDisposition:
                 [UnderDross{a,b}, MachineLocal{p}, OutsideDross{q,r}, {}] returns exactly [a b p q r],
                 in that order.
       contract: TestRunDetachArgvRoutesOutputToStderr:
                 - `sh -c 'echo out; echo err >&2'` returns nil, both lines land on the captured os.Stderr,
                   and nothing reaches os.Stdout.
                 - `sh -c 'exit 23'` returns an *exec.ExitError with code 23.
                 - A nonexistent argv[0] returns an error.
       contract: writers.go ≥80% and verify/detach.go 100% on the awk measure.
                 `grep -c cross-package-only-coverage .dross/survivors.toml` is 0.
                 `go test -run TestRepoAcceptanceReasonsCiteRealTests ./internal/survivor/` passes.

Wave 2 (depends t-2)
  t-4  Test the backlog push half
       files:    internal/boardsync/backlog_push_test.go
       covers:   c-1, c-2
       depends:  t-2
       desc:     Covers PushBacklogItems, resolveBacklogIssue, adoptLegacyBacklogKey, lookupPhaseIssue and
                 DeferredBacklogItem, on faultBoard and linkBoard. The YouTrack create path runs on
                 ytRecorder.
       contract: TestPushBacklogCreatesThenUpdatesByIDKey:
                 - First push of [plain, routed→x] creates 2 issues (created=2, updated=0) carrying
                   dross/deferred:<id>, and dross/target:x on the routed one.
                 - Second push gives (0, 2) and creates nothing.
                 - Re-routing the item to y leaves dross/target:y as its ONLY target label.
                 - An item with no labels is created with [dross].
                 Kills lines 399, 402, 405 ×2, 412, 419, 423, 447, 452.
       contract: TestResolveBacklogAdoptsMarkerBearingIdentityMatch: with board.json empty, the identity
                 query adopts the lowest-keyed issue that carries the dross marker, and skips a same-label
                 issue without it. A ListIssues failure returns a "board:" error and creates nothing.
                 Kills 317, 319, 330 ×2.
       contract: TestLegacyBacklogKeyAdoptedOnlyOnTitleMatch: when the stored issue's title matches, the id
                 key is set, the legacy key is deleted, and nothing is created. On a title mismatch or a
                 GetIssue failure, the legacy link is untouched and a fresh issue is created.
                 Kills 49, 57 ×3.
       contract: TestPushBacklogYouTrackVersionAndEpicModes (ytRecorder):
                 - Modes "version" and "" send the cached milestone as fix version and no subtask command.
                 - Mode "epic" sends no fix version and links the item as a subtask of the epic.
                 - Tags are patched after create.
                 - A failed create returns an error and records no link.
                 Kills 362 ×2, 429 ×3, 433.
       contract: TestLinkRoutedWarnsOncePerRunWithoutLinker:
                 - On faultBoard, two routed items produce exactly one "cannot link issues" line on stderr.
                 - On linkBoard, a target with no phase issue warns, naming the phase, and makes no
                   LinkIssues call.
                 - A target with a phase issue records LinkIssues(item, target).
                 - A LinkIssues error only warns; the push still succeeds.
                 Kills 375, 388, 392.
       contract: TestLookupPhaseIssueNeverCreates: a cache hit returns the key; the label query filters on
                 the marker and takes the lowest key; a list error or no match returns "" with zero
                 creates. Kills 288, 297.
       contract: DeferredBacklogItem: an id-less entry gets no Identity label; a routed entry is titled
                 "[routed] " and its body names the target. Kills 259, 263, 267, 270.
       contract: Failure paths:
                 - A scripted EnsureMilestone error (empty cache, milestone toml present) aborts before any
                   create (350).
                 - An unwritable BoardPath returns the Save error (454).
                 Each of the five functions reaches ≥85% in `go tool cover -func`.

  t-5  Test the backlog sync and reconcile half
       files:    internal/boardsync/backlog_reconcile_test.go
       covers:   c-1, c-2
       depends:  t-2
       desc:     Covers SyncBacklog, ReconcileBacklog, BacklogVerdictFor and IssueIsDone over a TempDir
                 .dross root: milestone toml via writeMilestoneToml, phase dirs, and [[deferred]] entries
                 via writeSpec.
       contract: TestSyncBacklogMirrorsUnscaffoldedSlugsAndLiveDeferred: milestone v1 has phases
                 [shipped (has a phase dir), todo] and deferred [plain, routed, dismissed].
                 - Exactly "[backlog] todo", "[someday] …" and "[routed] …" are created. Neither
                   `shipped` nor the dismissed item is created.
                 - Ctx.Out reads "backlog v1 -> 3 created, 0 updated, 0 closed".
                 - board.json on disk holds the 3 keys.
                 Kills 78, 82, 83, 92, 106, 110, 113.
       contract: TestSyncBacklogMissingMilestoneNamesIt: an absent v9.toml returns an error containing
                 `load milestone "v9"`, with zero board calls. Kills 71.
       contract: TestReconcileBacklogClosesOnlyProvablyResolvedMirrors:
                 - Scaffolded slug: closed, key deleted.
                 - Slug with no phase dir: left open, with a stderr warning naming the issue and key.
                 - Dismissed deferred item (by id key and by legacy key): closed.
                 - Live routed item whose target issue is done: closed, key KEPT.
                 - Live routed item whose target is open: untouched.
                 - Already-resolved mirror: not closed a second time.
                 - The returned count is exact.
                 Kills 150, 159, 187, 204, 208, 211, 221.
       contract: TestReconcileBacklogKeepsLinkWhenCloseFails: failClose makes stderr say the link is kept,
                 leaves the key in board.json, and excludes the mirror from the count. failGet produces a
                 "could not read" warning and leaves the mirror open. Kills 177, 183.
       contract: TestBacklogVerdictTable: every branch of BacklogVerdictFor returns its expected
                 StillOpen / Resolved / Unattributable. IssueIsDone: Resolved=true is done;
                 State "closed" is done; a GetIssue error returns a "board:" error; a nil issue is
                 not done. Each of the four functions reaches ≥85%. Together with t-4, backlog.go ≥80%.

  t-6  Test reap apply
       files:    internal/boardsync/reap_apply_test.go
       covers:   c-1, c-2
       depends:  t-2
       desc:     Covers Apply, relabelReapedCard, dropBacklogLink, appendReapRun, BoardNamespaceNames,
                 ValidateReapNamespaces, Inventory and reapFailure. Apply runs on faultBoard. Inventory
                 reuses discoverYT/discoverRepo from reap_discover_test.go.
       contract: TestApplyJournalsPriorStateBeforeClosing: a plan of 3 cards (Phases, Tasks and Backlog
                 lanes), each prior State "open", labels [dross, dross/phase:p, dross/status:uat].
                 - All three close.
                 - The last reap-log.json run records PriorState "open" (not "closed"), the prior labels,
                   and Outcome closed.
                 - Each card ends with its identity labels plus dross/status:<terminal>.
                 - The Backlog card's board.json link is dropped and journalled as DroppedLink; the other
                   links are kept.
                 - Ctx.Out reads "reaped 3 card(s)".
                 Kills 144, 164, 171, 174, 177, 182, 241.
       contract: TestApplyIsolatesPerCardFailures: failGet on A, nilGet on B (GetIssue returns nil, nil),
                 refuseClose on C, D healthy.
                 - D closes; A, B and C are journalled Failed.
                 - B's failure reads "read prior state: issue not found".
                 - The error reads "3 of 4 card(s) could not be closed", and stderr names A, B and C.
                 - A failUpdate relabel on D only warns, and D still counts as closed.
                 Kills 125 ×2, 126.
       contract: TestRelabelKeepsIdentityLabelsAndSkipsNoOps:
                 - No prior labels: no panic, result [dross/status:T] (kills 200: cap len-1 panics).
                 - The only status label already equals T: zero UpdateIssue calls (211 ×3).
                 - Right label plus a stale one: rewritten to exactly one status label (204).
       contract: TestApplyWithNothingClosedWritesNoJournal: an empty plan creates no reap-log.json (253).
                 A corrupt existing reap-log.json makes Apply return the load error (258).
       contract: TestBoardNamespaceNamesAreTheMapFields: the result equals
                 [Backlog Milestones Phases Quicks Tasks]. Dismissed (a slice field) never appears.
                 Kills 279 ×3, 280.
       contract: TestValidateReapNamespacesNamesUnknowns: nil or empty input returns nil; " BACKLOG "
                 returns nil; ["phases","bogus","nope"] errors naming "bogus, nope" and listing all 5.
                 Kills 291, 304 ×2.
       contract: TestInventoryDedupesLinkedAndOrphanCards: a card that is both linked and discovered is
                 planned once. An unknown namespace is refused before any HTTP request (40). A 500 on the
                 discovery list returns an error, not a partial plan (52).
                 Also: reapLogPathFor(root) == reaplog.FilePath(root); reapFailure's Error() is "K: msg"
                 and errors.Unwrap returns the inner error. reap_apply.go ≥80%.

  t-7  Test reap undo and the apply→undo round trip
       files:    internal/boardsync/reap_undo_test.go
       covers:   c-1, c-2
       depends:  t-2
       desc:     Extends the existing file with Undo and restoreDroppedLink tests on stateBoard and
                 faultBoard, plus an end-to-end Apply-then-Undo test.
       contract: TestUndoRefusesClientWithoutStateWriter: on faultBoard, returns an error naming the
                 provider and "nothing was written". Zero writes, even when reap-log.json is corrupt.
       contract: TestUndoWithEmptyLedgerSaysNothingToUndo: with no ledger, Ctx.Out gets "nothing to undo"
                 and the return is nil (43). A corrupt ledger returns an error (38).
       contract: TestUndoWritesPriorStateThroughStateWriter: the last run holds closed A (with labels),
                 closed B (no labels, DroppedLink), failed C, and closed D (class Tasks, DroppedLink).
                 - SetStateRaw runs for A, B and D only.
                 - A's labels are restored. B gets no label patch at all (kills 61 `>` → `>=`).
                 - B's backlog key is back in board.json on disk. D's key is NOT put into Backlog (93).
                 - Ctx.Out reads "restored 3 card(s)".
                 Kills 51, 61 ×2, 63, 67, 70, 73, 78.
       contract: TestUndoContinuesPastSetStateFailure: when SetStateRaw fails on A, the others are still
                 restored, the error reads "1 of 3 card(s) could not be restored", and stderr names A. A
                 failed label restore only warns and leaves the count unchanged.
       contract: TestUndoOnlyReversesTheLastRun: with two runs journalled, only the last run's cards are
                 written.
       contract: TestApplyThenUndoRoundTrip: Apply then Undo on stateBoard leaves every card's State,
                 label set and board.json backlog map reflect.DeepEqual to the snapshot taken before
                 Apply. reap_undo.go ≥80%.

  t-8  Test inbound feed and milestone linking
       files:    internal/boardsync/inbound_test.go, internal/boardsync/milestone_test.go
       covers:   c-1, c-2
       depends:  t-2
       desc:     Covers CollectInbound, EmitPullEnvelope and ReportBoardFailure; EnsureMilestoneLink on
                 faultBoard and on ytRecorder (epic mode); CheckMilestoneClosable; MilestoneBody.
       contract: TestCollectInboundDropsDrossAuthoredIssues: from [human, linked in any board.json
                 namespace, dismissed, carries the dross marker], only human is returned. A ListIssues error
                 returns a "board:" error and a nil feed. Kills 20.
       contract: TestPullEnvelopeShape: nil issues gives {"issues":[],"error":null}; an error sets
                 "error":"<msg>"; one issue appears as issues[0]; the line ends with a newline.
                 Kills 53, 56, 61.
       contract: TestReportBoardFailureByMode:
                 - JSON: an envelope with the error, and a nil return.
                 - Human, fatal: returns the error and prints nothing.
                 - Human, non-fatal: prints "board unreachable: <err>" and returns nil.
       contract: TestEnsureMilestoneLinkRecordsBoardID:
                 - Cached: returns the id with zero EnsureMilestone calls.
                 - Missing toml: ("", nil), zero calls (46).
                 - Titled: EnsureMilestone(title, "title\n\nSuccess criteria:\nc1\nc2"), and the id is
                   recorded in board.json.
                 - Untitled: the title is the version (50).
                 - Board error: a "board:" error, nothing recorded (68).
                 - Board returns "": ("", nil), nothing recorded (71).
                 - YouTrack epic mode (ytRecorder) records the epic's idReadable.
       contract: TestCheckMilestoneClosableOnlyForYouTrackEpic: faultBoard errors, naming the provider.
                 YouTrack in mode "version" or "" errors, naming the mode. " Epic " returns nil.
                 MilestoneBody("t","") == "t". inbound.go and milestone.go are each ≥80%.

  t-9  Kill the routed survivors in phase/task/pull/reap
       files:    internal/boardsync/sync_paths_test.go
       covers:   c-1
       depends:  t-2
       desc:     The 16 keys routed here from outside c-2's files: phase.go 65/94/138/147/198,
                 task.go 78/217×2/257/261, task_pull.go 49/53/66-68, reap.go 472. The YouTrack paths use
                 ytRecorder; this file holds its own minimal Jira stand-in for the Jira paths.
       contract: TestPhaseSyncAdoptsLegacyTitleOnlyWithMarker: when no phase label matches, the
                 same-title issue with the marker is adopted and the one without it is not. Kills 65.
       contract: TestPhaseSyncAssignsDeclaredMilestone: a spec declaring v1 creates the issue with
                 Milestone 7 and records v1→7 in board.json. A spec with no milestone gives Milestone 0 and
                 makes no EnsureMilestone call. Kills 94.
       contract: TestPhaseSyncSurfacesTrackerStateWriteFailure: a YouTrack state POST returning 500, or a
                 Jira transition returning 500, makes SyncPhase, CloseIssue and SyncTasks each return a
                 "board:" error. On a successful write, board.json is saved with the phase link.
                 Kills phase.go 138, 147, 198 and task.go 257, 261.
       contract: TestTaskSyncLinkFailureWarnsOnce: with a failing LinkIssues on linkBoard, a two-task phase
                 writes exactly one stderr warning and both issues are still created. Kills task.go 78.
       contract: TestTaskIssueResolutionWarnsOnDuplicates: two marker issues on one task label mean the
                 lowest key is updated and stderr names "2 issues". A single match produces no warning.
                 Kills task.go 217 ×2.
       contract: TestTaskPullRefusesFlatBoards: provider github returns an error naming "github" and
                 "youtrack or jira", with no board call (49; covers 66-68 so the string-concat mutants stop
                 compiling). TestTaskPullSurfacesListFailure: on youtrack with listErr, the error is returned
                 and plan.toml is byte-identical (53).
       contract: TestQuickMirrorWithEmptyIssueIsSkipped: for Quicks {q1:"", q2:"P-9"},
                 classifyQuickMirrors returns P-9 only. Kills reap.go 472.

Wave 3 (depends t-3, t-4, t-5, t-6, t-7, t-8, t-9)
  t-10 Drain the three packages to zero outstanding
       files:    .dross/survivors.toml
       covers:   c-1
       depends:  t-3, t-4, t-5, t-6, t-7, t-8, t-9
       desc:     Run `make install` (r-01). Then run
                   dross survivor drain --packages ./internal/boardsync,./internal/diag,./internal/secretscan
                                        --phase extracted-package-test-parity
                 on the mutation host, detached. Act on each outstanding survivor's evidence line:
                 - "attribution ceiling": dross survivor accept … --category gremlins-attribution-ceiling
                 - "no coverage block": --category const-initializer-arithmetic
                 - "killable": add the test to the owning wave-2 file (backlog.go → t-4/t-5 file, reap_* →
                   t-6/t-7 file, etc.) and re-drain.
                 Predicted ceiling set: diag/redproof.go 91 and 94, and writers.go's switch arms 88, 90, 92,
                 116, 118 ×2, 120, 129, 139.
       contract: The final drain prints "0 outstanding" and exits 0. survivor_drain.go exits non-zero for
                 any survivor with no disposition, so a single leftover fails this task.
       contract: No acceptance is in category cross-package-only-coverage. Every acceptance on
                 internal/{boardsync,diag,secretscan} is in gremlins-attribution-ceiling or
                 const-initializer-arithmetic (checked with `dross survivor list --json`). Each accepted
                 key's drain evidence line said "ceiling" or "no coverage block"; paste that transcript
                 into the commit. A survivor marked killable or not covered is never accepted.
       contract: TestRepoAcceptanceReasonsCiteRealTests passes after the accepts: every effective reason
                 still cites an existing test.

Wave 4 (depends t-1, t-10)
  t-11 Wire the floor into CI and gate the phase
       files:    .github/workflows/ci.yml, cmd/coverfloor/ci_test.go
       covers:   c-2, c-3, c-4
       depends:  t-1, t-10
       desc:     Add -coverprofile="$RUNNER_TEMP/cover.out" to the test job's ONE existing go test line. No
                 new go test invocation, no -coverpkg, and no ${{ }} inside run. Add a `coverage floor` step
                 after it: `go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"`. Audit ci.yml against the
                 supply-chain checklist (universal 1-3, Go G1-G3, npm N1-N4), report findings, and offer
                 fixes in the same edit.
       contract: TestCoverFloorIsWiredIntoCI (reads .github/workflows/ci.yml) fails if:
                 - the test job's go test lacks -coverprofile="$RUNNER_TEMP/cover.out";
                 - that line carries -coverpkg;
                 - its flags differ from t-1's fixtureCoverFlags (so the self-test measures the flags that
                   are actually deployed);
                 - no later step in job `test` runs `go run ./cmd/coverfloor "$RUNNER_TEMP/cover.out"`;
                 - that step carries continue-on-error or `|| true`.
       contract: These existing pins pass unedited: TestCIGoTestStepsLive (still exactly one go test in
                 job test, with -race, -count=1, ./... and -json into testsummary), TestGoTestStepScanner,
                 TestWorkflowsHaveNoExpressionsInRun, TestWorkflowActionsArePinned, TestCIConcurrencyLive,
                 TestToolchainSingleSource.
       contract: c-4 live check:
                 - Local preflight mimicking CI: gremlins (at minimum) stripped from PATH, then
                     go test -count=1 -coverprofile=$T/c.out $(go list ./... | grep -v '/internal/cmd$')
                     go run ./cmd/coverfloor $T/c.out
                   must exit 0.
                 - The PR's CI run shows the floor step green (observed). Also record internal/cmd's
                   -race+cover time from the testsummary table.
                 - If any file outside this phase drops under 50% under CI conditions, STOP and ask
                   (defer or add). Never add an exemption.
       contract: c-2: the awk measure shows backlog.go, reap_apply.go, reap_undo.go, inbound.go,
                 milestone.go and writers.go each ≥80.0%.
       contract: c-3:
                 - `git diff --name-only 018d325...HEAD -- internal/cmd` is empty.
                 - Every changed .go file under internal/ is a _test.go.
                 - Remote `dross test` is green, including TestNoTestLost, TestNoTestLostDetectsDropsAndCopies,
                   internal/cmd issue_*_test.go and doctor*_test.go, and the cli_surface pins.
```

## Coverage

- **c-1** (the drain reports zero survivors with no disposition; 18 cross-package acceptances retired):
  - t-3 retires the 18 and adds the writers.go killers.
  - t-4, t-5, t-6, t-7, t-8, t-9 kill the 118 routed survivors, each contract naming its lines.
  - t-2 enables the kills.
  - t-10 is the locked whole-package drain evidence, plus the ceiling accepts.
- **c-2** (six files at ≥80% own-package coverage):
  - t-3: writers.go
  - t-4 + t-5: backlog.go
  - t-6: reap_apply.go
  - t-7: reap_undo.go
  - t-8: inbound.go and milestone.go
  - t-2: enabler
  - t-11: final measurement of all six
- **c-3** (no behaviour change, cmd e2e tests and CLI pins pass unedited):
  - t-11: no internal/cmd diff, only _test.go changes under internal/, remote suite green.
  - Every test task's name-uniqueness rule keeps TestNoTestLost green.
- **c-4** (CI floor plus a self-test):
  - t-1: checker, fixture self-test, calibration against the baseline.
  - t-3: lifts verify/detach.go, the one below-floor file outside the phase.
  - t-11: CI wiring, the pin tying self-test flags to ci.yml, and the observed live pass.

All 4 criteria are covered.

## Judgment calls

1. **Where the c-4 floor runs.** Chosen: a CI step that reads a profile from the test job's existing single `go test`, with `-coverprofile` added. Rejected alternatives:
   - A Go test inside `go test ./...` that spawns the coverage run. Gremlins and the drain would re-run the whole internal/ tree for every mutant in its package, it would recurse into itself, and it would double the local suite.
   - A separate `go test -coverprofile` step or job. A second invocation breaks TestCIGoTestStepsLive's "exactly one per job" pin, forcing assertion edits, and re-runs about 40 packages, against build-once.

   Accepted cost: internal/cmd, around 200s under -race today, is coverage-instrumented too. Watch the 300s ::warning.
2. **The checker never spawns `go` itself.** Chosen: a pure checker that reads a profile file. Rejected: a checker that runs its own `go test`. That adds a production spawn site the exec-consent and taint audits (in internal/cmd, which cannot run on this laptop) would have to clear, and it re-runs every package.
3. **Where the checker lives.** Chosen: cmd/coverfloor as package main, following the precedent of the stdlib-only CI tool cmd/testsummary. Rejected: an internal/ package. This is a CI-only tool, not part of the dross binary, and an internal/ package would put CI tooling into the dross source tree.
4. **Kind of self-test.** Chosen: a real-toolchain fixture module plus a pin that ties the fixture's flags to ci.yml. Rejected: a synthetic-profile-only test. It cannot catch Go dropping test-less packages from profiles, or a `-coverpkg` creeping in and attributing cmd's coverage (the blind spot test_home forbids).
5. **Floor arithmetic.** Chosen: integer comparison `2*covered < total`. Rejected: float percentages. With integers, exactly 50% passes with no rounding ambiguity, and the boundary mutant has a clean kill.
6. **CI wiring goes last (wave 4).** Chosen: wire CI last so CI never goes red mid-phase. The red case is already proven by the fixture self-test and by the wave-1 calibration, which must list exactly the 7 baseline files, cross-checking the checker against the independent 2026-09-26 measurement. Rejected: wiring first to watch CI go red on the real tree.
7. **Where the 18 retirements happen.** Chosen: in the same task as writers.go's in-package tests (t-3), because the justification for retiring them is those tests existing. Re-accepts use the existing gremlins-attribution-ceiling category, whose reason cites TestAttributionCeilingIsReal. Rejected: a new writers-specific category, which would be new prose for the reason audit. Also rejected: gremlins-switch-case-ceiling, whose prose cites unrelated Classify tests.
8. **Kill targets come from the recorded list.** Chosen: take the 118 routed survivors from cmd-package-decomposition's verify.toml; boardsync and diag are unchanged since that run. Rejected: a baseline drain in wave 1, which is a long mutation run to rediscover a list already recorded against identical code. secretscan has changed since then, so t-10's drain is authoritative for it.
9. **The phase.go/task.go/task_pull.go/reap.go survivors get their own task (t-9).** c-2 does not name these files, but t-10's drain counts every key routed here as outstanding. Rejected: finding them in the drain loop, the slowest feedback loop in the phase.
10. **Shared doubles are a wave-1 task with their own self-test.** Chosen: one set of doubles. TestDoubleCapabilitiesAreExact makes sure faultBoard is not a StateWriter, so t-7's refusal test cannot pass without testing anything. Rejected: each task defining its own doubles, which risks name collisions in one package and duplicated fault behaviour.
11. **No category-name ban for c-1.** Rejected: a test that forbids the name "cross-package-only-coverage". guard_shape already notes that renaming a category defeats a check on its name. The rename-proof guard is c-4's floor: a file at 0% own coverage now fails CI whatever its acceptances say.
12. **The live floor is judged under CI conditions, not the laptop's.** The laptop has gremlins, npx, node and dotnet on PATH, so tests gated on those tools may run here and raise coverage above what CI's test job sees. t-11 strips them for the preflight and treats the PR run as the verdict.
13. **Dead reapLogPathFor is tested, not deleted.** Chosen: pin reapLogPathFor(root) to reaplog.FilePath(root) in a test. Rejected: deleting it, which would be a production edit in a phase whose c-3 is "no behaviour change". Deferring its removal is a candidate for later.
14. **The 118 routed deferred entries are left in place.** Once their mutants are killed, they never surface in a drain again, and ReconcileBacklog closes routed board mirrors when this phase's issue resolves. Cleaning them up is not a criterion.
