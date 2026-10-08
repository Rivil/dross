# Planner draft: risk lens

Phase extracted-package-test-parity: 13 tasks across 4 waves

Premise: c-2 (>=80% per file) is the easy half. c-1 is stricter. The drain reports every mutant on an
uncovered line as NOT COVERED, and every mutant on a covered but unasserted line as LIVED. That means
the remaining 20% of each file, plus the arms that only print to os.Stderr, and the routed survivors in
files c-2 doesn't name (phase.go, task.go, task_pull.go, reap.go, diag/redproof.go) all have to be
killed or honestly ceiling-accepted. The worklist is the 118 survivors routed to this phase in
cmd-package-decomposition/spec.toml [[deferred]]. boardsync and diag are unchanged since that run, so
its line numbers still hold.
Each task below carries an `owns:` line naming the failure mode it alone is responsible for.

```
Wave 1
  t-1  Build strict boardsync tracker test fixtures
       files:    internal/boardsync/fixture_test.go
       desc:     Add a scripted httptest tracker. It fails on an unscripted request and on a scripted route
                 that is never hit. Add ctx builders for real YouTrack (per milestone_mode) and Jira clients,
                 built through Config() the way ytRepo is. Add fakeBoard wrappers: stateWriterBoard
                 (SetStateRaw + refuse set), linkingBoard (LinkIssues record/err), faultyBoard (per-method,
                 per-key errors). Add captureStderr, which restores the stream in t.Cleanup.
       covers:   c-1
       owns:     vacuous tests. A fake that answers 200 {} to everything makes every "error arm" test pass
                 without reaching the tracker. A leaked os.Stderr swap poisons later tests.
       contract: - TestScriptedTrackerReportsUnscriptedAndUnusedRoutes: a GET to an unscripted path and a
                   scripted route never called are both reported. If the unused-route check is removed,
                   the test fails.
                 - TestCaptureStderrRestoresTheStream: after capture returns, and after a recovered panic
                   inside fn, os.Stderr is the original *os.File again.
                 - Every helper roots Ctx.Root in t.TempDir(). `git status --porcelain .dross` is
                   byte-identical before and after `go test -count=1 ./internal/boardsync/` (reap-log.json,
                   deferred.toml and board.json are never written into the real repo).
                 - No new name is taken from tests_before.txt (grep -c "^Name\t" == 0 for each).
       status:   pending

  t-2  Test writers.go registry in-package
       files:    internal/secretscan/writers_test.go
       desc:     Port the intent of cmd's TestWriterRegistryValidate under NEW names. Add the arms cmd never
                 fed: empty or whitespace File, machine-local with no Path, machine-local with no Why, a
                 "/"-absolute artifact, and three dispositions. Add a live-registry-validates-clean check and
                 ArtifactNames ordering. Leave the internal/cmd tests untouched.
       covers:   c-1, c-2
       owns:     the 9 non-case writers.go mutants (lines 106, 110, 112, 121, 130, 133, 136, 140, 144). They
                 must die in-package, not be re-accepted.
       contract: - TestWriterValidationNamesEveryMalformedShape: each malformed entry yields exactly one error
                   containing its arm's phrase ("empty File", "no disposition", "2 dispositions",
                   "3 dispositions", "no Artifacts", "must be .dross-relative" for both ".dross/a" and "/a",
                   "machine-local with no Path", "no IgnoreSeed", "machine-local with no Why",
                   "outside-dross with no Why", "under .dross by definition", "declared twice").
                   Mutating set++ to set-- drops the "2 dispositions" error, so the test fails.
                 - TestLiveWriterRegistryValidatesInPackage: ValidateWriters(Writers()) returns zero errors.
                   Negating any guard at 121/130/133/136/140/144 makes a good entry error, so the test fails.
                   A set>1 to set>=1 boundary mutant makes every entry error, so the test fails.
                 - TestArtifactNamesSpansEveryDisposition: [UnderDross{a,b}, MachineLocal{p}, OutsideDross{x,y},
                   {no disposition}] maps to exactly [a b p x y].
                 - TestWritersReturnsACopy: overwriting Writers()[0] leaves a second Writers()[0] unchanged.
                 - `go test -coverprofile ./internal/secretscan/` puts writers.go at >=80% (expect ~100%).
                 - No name collides with tests_before.txt (TestWriterRegistryValidate exists there; reusing
                   it makes TestNoTestLost fail with "copied and left behind").
       status:   pending

  t-3  Add coverfloor checker and detach test
       files:    cmd/coverfloor/main.go, cmd/coverfloor/floor.go, cmd/coverfloor/floor_test.go,
                 internal/verify/detach_test.go
       desc:     Stdlib-only checker, following cmd/testsummary's run() pattern. It reads a coverprofile, keys
                 blocks by file+span (deduped, max count), and sums statements per file for paths under
                 <module>/internal/ excluding internal/cmd/. It fails any file with covered*2 < total. It has
                 no flags, no env and no allowlist. Also add a helper-process test for verify.RunDetachArgv.
       covers:   c-4
       owns:     a floor that passes vacuously or at the wrong boundary, and the one non-phase file below the
                 floor (verify/detach.go at 0%).
       contract: - TestFloorFailsABelowFloorFile (the c-4 self-test): a profile where internal/x/low.go has 1
                   of 3 statements covered makes run() exit non-zero and the output names internal/x/low.go.
                 - TestFloorPassesExactlyFiftyPercent: 1 of 2 passes and 99 of 200 fails. Either a `<` to
                   `<=` mutant or float/int rounding makes one of the two cases fail.
                 - Scope: internal/cmd/a.go at 0% passes, internal/cmdx/a.go at 0% fails (prefix needs the
                   trailing slash), and cmd/dross/main.go at 0% is ignored.
                 - Vacuity: an empty profile, or one whose paths lack the module prefix, exits non-zero with
                   "0 files checked", never green.
                 - A malformed line (non-numeric count, missing span) exits non-zero; it is not skipped the
                   way survivor.ParseProfile skips. A missing profile file exits non-zero.
                 - The same block listed twice (one entry count 0, one count 1) counts its statements once.
                 - TestDetachArgvRoutesChildStdoutToStderr: RunDetachArgv([os.Args[0],
                   -test.run=^TestDetachHelperProcess$]) returns nil, and the helper's stdout sentinel shows up
                   in the captured os.Stderr, not os.Stdout. An exit-3 helper returns *exec.ExitError with
                   ExitCode()==3. A nonexistent argv[0] returns an error. detach.go reaches 100%.
       status:   pending

Wave 2 (t-4..t-9 depend on t-1; t-10 depends on t-2)
  t-4  Test inbound feed and milestone link
       files:    internal/boardsync/inbound_test.go, internal/boardsync/milestone_test.go
       desc:     CollectInbound exclusion basis, the PullEnvelope shape, the three ReportBoardFailure modes,
                 the CheckMilestoneClosable gates, and EnsureMilestoneLink dispatch (cached, missing toml,
                 default, YouTrack epic, Jira version, error, empty id).
       covers:   c-1, c-2
       depends:  t-1
       owns:     routed inbound.go:20/53/56/61 and milestone.go:46/50/68/71. That includes the
                 closing-a-human's-issue gate: a forge milestone id resolving to issue #N.
       contract: - From 5 issues (board-linked, dismissed, marker-labelled, 2 human-filed), CollectInbound
                   returns exactly the 2 human ones. A ListIssues error returns an error prefixed "board:".
                 - EmitPullEnvelope(nil, nil) prints exactly {"issues":[],"error":null}. With an error, it
                   carries the message. Negating the post-Marshal err check suppresses the print, so the test
                   fails.
                 - ReportBoardFailure: asJSON gives the envelope and a nil return. humanFatal returns the error
                   and writes nothing. Otherwise it prints "board unreachable:" and returns nil.
                 - CheckMilestoneClosable: fakeBoard is refused naming the provider. YouTrack with mode "" is
                   refused quoting "version" (kills the :46 negation). YouTrack with mode "agile" is refused.
                   YouTrack with " Epic " is nil.
                 - EnsureMilestoneLink: a cached id makes zero tracker calls. A missing toml returns ("", nil)
                   with no link. A blank title makes EnsureMilestone receive the version. The YouTrack epic and
                   Jira version paths hit their scripted routes. On EnsureMilestone error, the error is
                   prefixed "board:" and nothing is linked. An empty id from the backend stores no link.
                 - `go test -coverprofile` puts inbound.go and milestone.go at >=80% each.
       status:   pending

  t-5  Test backlog push, resolve, legacy adopt
       files:    internal/boardsync/backlog_push_test.go
       desc:     Outbound half of backlog.go: PushBacklogItems, resolveBacklogIssue, adoptLegacyBacklogKey,
                 lookupPhaseIssue and DeferredBacklogItem, over fakeBoard, linkingBoard and the scripted
                 YouTrack.
       covers:   c-1, c-2
       depends:  t-1
       owns:     duplicate minting after board.json dies with a branch, adopting a drifted legacy key, stale
                 dross/target labels, and linker-warning spam.
       contract: - With an empty cache and one marker+identity issue on the tracker, push updates it
                   (created=0, updated=1) and mints nothing. With two matches, the lowest-sorted key wins. An
                   identity match without the marker is not adopted, so a new issue is created. A ListIssues
                   error returns an error prefixed "board:" and creates nothing.
                 - With the legacy key's issue title equal to the item's, the link moves to the id key and the
                   legacy key is deleted. On title mismatch, the legacy key is untouched and a fresh issue is
                   created. On a GetIssue error, nothing is adopted.
                 - The update path replaces labels wholesale: a re-routed item loses the old dross/target. An
                   update with empty Labels leaves the issue's labels intact (kills the len>0 to >=0 mutant).
                   A create with empty Labels carries [dross]. With a non-YouTrack milestone "7",
                   IssueInput.Milestone==7.
                 - YouTrack epic mode issues CreateBacklogItem, then LinkSubtask(epic, key), then the tag patch,
                   in that order. Version mode sends fixVersion and no LinkSubtask. A LinkSubtask 500 returns
                   an error and leaves no board.json link.
                 - No linker with 2 routed items gives exactly one "cannot link issues" line on stderr (kills
                   the !warnedNoLinker negation). A target with no phase issue warns "no board issue yet" and
                   makes no LinkIssues call. A LinkIssues error warns and the push still counts the item.
                 - Returned (created, updated) are asserted exactly (kills ++ mutants). board.json is on disk
                   after the call.
                 - `go tool cover -func` shows 100% for the five functions above.
       status:   pending

  t-6  Test backlog reconcile verdicts and closes
       files:    internal/boardsync/backlog_reconcile_test.go
       desc:     Inbound half of backlog.go: SyncBacklog, ReconcileBacklog, BacklogVerdictFor and IssueIsDone,
                 on temp milestone, phase-dir and deferred fixtures.
       covers:   c-1, c-2
       depends:  t-1
       owns:     wrongful close of unattributable work, double close, and stranding an issue by dropping its
                 link on a failed close.
       contract: - SyncBacklog over 3 slugs (1 scaffolded) and deferred items (1 dismissed, 1 routed, 1 plain)
                   prints exactly "backlog v1 -> 4 created, 0 updated, 0 closed". The second run prints
                   "0 created, 4 updated". A missing milestone toml errors `load milestone "v1"`.
                 - BacklogVerdictFor table, one row per branch: live unrouted, live routed with no target
                   issue, target done, target open, target GetIssue error, slug with dir, slug without dir,
                   deferred dismissed, deferred live, legacy positional key, unknown key. Each row asserts its
                   exact verdict.
                 - ReconcileBacklog: a resolved open mirror is closed and its link dropped (1 closed). An
                   already-done mirror gets zero CloseIssue calls. A read-back error warns and keeps the link.
                   A refused close warns "link is kept" and keeps the link (0 closed). A live routed mirror
                   whose target is done is closed with its link KEPT. An unattributable key warns and is
                   untouched. An empty issue id is skipped.
                 - IssueIsDone: Resolved gives true, State "closed" gives true, open gives false, a nil issue
                   gives (false, nil), and an error is prefixed "board:".
                 - `go tool cover -func` shows 100% for these four functions. backlog.go is >=80% once t-5 and
                   t-6 have both landed.
       status:   pending

  t-7  Test reap apply isolation and journal
       files:    internal/boardsync/reap_apply_test.go
       desc:     Apply over faultyBoard/fakeBoard, plus relabelReapedCard, dropBacklogLink, appendReapRun,
                 Inventory (reusing readOnlyYT/discoverYT), ValidateReapNamespaces and BoardNamespaceNames.
       covers:   c-1, c-2
       depends:  t-1
       owns:     card 40 of 90 stranding the rest, a journal built from post-close state (so undo restores
                 nothing), and an empty run shadowing the real undo target.
       contract: - 3 cards with card 2's close refused: cards 1 and 3 are closed. The error is "1 of 3 card(s)
                   could not be closed". Stdout shows "reaped 2 card(s), 1 failed:". Stderr names card 2.
                 - The ledger records PriorState/PriorLabels equal to the state read BEFORE the close. A card
                   GetIssue can't find is journalled failed, with "issue not found" on stderr. An unverified
                   close (nil error, read-back open) is failed and not counted.
                 - Relabel: [dross, dross/status:task-in-review] becomes [dross, dross/status:<terminal>]. A
                   single already-correct status label makes zero UpdateIssue calls. Two status labels, one
                   correct, collapse to one. A card with ZERO prior labels relabels without panicking (kills
                   the len(prior)+1 to -1 capacity mutant). A relabel failure warns and the card still counts
                   closed.
                 - A Backlog-lane card drops its board.json key and journals DroppedLink. A "Phases"-lane card
                   drops nothing.
                 - An empty plan leaves reap-log.json absent. A no-op apply after a real one leaves Last()
                   unchanged.
                 - ValidateReapNamespaces: nil gives nil, " Phases " gives nil, and "bogus" errors listing every
                   BoardNamespaceNames entry. BoardNamespaceNames equals the sorted map fields of board.Board.
                 - Inventory lists an orphan and a linked card once each and returns the unclassifiable ones.
                 - `go test -coverprofile` puts reap_apply.go at >=80%.
       status:   pending

  t-8  Test reap undo restore and round trip
       files:    internal/boardsync/reap_undo_test.go
       desc:     Extend the existing file with Undo tests over stateWriterBoard and a hand-built reaplog
                 ledger, plus one Apply-then-Undo round trip.
       covers:   c-1, c-2
       depends:  t-1
       owns:     the routed reap_undo.go:38-93 set, a partial undo reporting success, and a capability-less
                 backend reopening everything.
       contract: - fakeBoard (no StateWriter) returns an error naming the provider and "nothing was written".
                   Zero SetStateRaw/UpdateIssue calls are made, and the board.json bytes are unchanged.
                 - A missing ledger prints "nothing to undo" and returns nil.
                 - With a last run of [closed A (prior "In Review", labels L), failed B], only
                   SetStateRaw(A, "In Review") is called, A's labels become L, and stdout shows
                   "restored 1 card(s)". With two runs, only the last is reversed.
                 - One of two SetStateRaw calls refused: the other is still restored. The error is "1 of 2
                   card(s) could not be restored". Stderr names the refused key.
                 - A label-restore failure warns "could not put its labels back" and the card still counts.
                   Empty PriorLabels gives zero UpdateIssue calls (kills the len>0 to >=0 mutant).
                 - DroppedLink with Class "Backlog" restores the board.json backlog key and saves it. With
                   Class "Phases", nothing is restored (reap_undo.go:93).
                 - Round trip: after Apply then Undo, every card's state and labels equal the pre-Apply
                   snapshot.
                 - `go test -coverprofile` puts reap_undo.go at >=80% (expect 100%).
       status:   pending

  t-9  Kill residual phase/task/pull/reap survivors
       files:    internal/boardsync/state_paths_test.go
       desc:     Target the routed survivors outside c-2's six files: the phase.go/task.go YouTrack and Jira
                 SetState error arms, legacy title adoption, milestone assignment, the task link-error and
                 duplicate-label paths, the TaskPull refusal, and the quick mirror with an empty link.
       covers:   c-1
       depends:  t-1
       owns:     c-1 failing on files c-2 never mentions (16 routed survivors: phase.go:65/94/138/147/198,
                 task.go:78/217x2/257/261, task_pull.go:49/53/66-68, reap.go:472).
       contract: - phase.go:65: a marker issue with the phase title is adopted (0 creates). The same title
                   without the marker creates a new issue.
                 - phase.go:94: a spec with milestone "v1" gives CreateIssue Milestone==7. Without a
                   milestone, EnsureMilestone is never called.
                 - phase.go:138/147 and task.go:257/261: a scripted YouTrack/Jira state write returning 500
                   makes the sync return an error prefixed "board:". status "" sends no state request (an
                   unused scripted route would fail the test).
                 - phase.go:198: a Jira SetState 500 during CloseIssue errors with no read-back GET.
                 - task.go:78: a LinkIssues error across 2 tasks gives exactly one warning line and a
                   successful sync.
                 - task.go:217: two marker issues carrying one task label give a warning naming both keys and
                   return the lowest-sorted. With exactly one, no warning (kills the >1 to >=1 boundary).
                 - task_pull.go:49/53: provider "github" is refused with "has no workflow state" and plan.toml
                   bytes unchanged. A ListIssues error propagates with plan.toml unchanged (the string-concat
                   ARITHMETIC_BASE at 66-68 then becomes not-viable instead of NOT COVERED).
                 - reap.go:472: a board.json quick with an empty issue yields no candidate. With an id, it
                   yields one candidate whose Why names the ref.
       status:   pending

  t-10 Retire cross-package acceptances, accept ceilings
       files:    .dross/survivors.toml
       desc:     `dross survivor retire` the 18 writers.go keys in category cross-package-only-coverage (the
                 category is pruned automatically). Re-accept ONLY the bare switch case-condition mutants
                 (writers.go 88, 90, 92, 116, 118 NEGATION+BOUNDARY, 120, 129, 139; diag/redproof.go 91, 94)
                 under one new category. Its reason cites t-2's test names and TestPinLinesMatrix.
       covers:   c-1
       depends:  t-2
       owns:     laundering: the cross-package category renamed to a ceiling, or a real gap accepted as a
                 ceiling.
       contract: - `grep -c cross-package-only-coverage .dross/survivors.toml` == 0, and `dross survivor list`
                   shows none of the 18 retired keys under that category.
                 - Each re-accepted line is a `case <cond>:` line, and shows count >=1 in
                   `go test -coverprofile ./internal/<pkg>/`. That proves the arm executes in-package, so
                   the ceiling is go-cover attribution, not a missing test.
                 - No acceptance exists for writers.go 106/110/112/121/130/133/136/140/144.
                 - `go test -count=1 -run TestRepoAcceptanceReasonsCiteRealTests ./internal/survivor/`
                   passes. If the category cites a test name that doesn't exist, it goes red.
       status:   pending

Wave 3
  t-11 Wire coverage floor into CI test job
       files:    .github/workflows/ci.yml, cmd/coverfloor/ci_wiring_test.go
       desc:     Add `-coverprofile=cover.out` to the EXISTING test-job line `go test -race -count=1 -json ./...
                 | go run ./cmd/testsummary`. Add a following step, `go run ./cmd/coverfloor cover.out`. Add no
                 new job, no second go test, no upload-artifact and no `${{ }}` in run. Read both
                 ~/.claude/memory CI references first, and surface the supply-chain audit findings.
       covers:   c-4
       depends:  t-2, t-3, t-4, t-5, t-6, t-7, t-8
       owns:     the floor silently unwired, breaking the unedited workflow pins, a day-one CI red from
                 tool-gated tests that skip on the runner, and coverage overhead pushing internal/cmd toward
                 go test's 10m wall.
       contract: - TestCoverFloorIsWiredIntoCI: ci.yml's test-job go test carries -coverprofile=cover.out, and
                   a later step in the same job runs `go run ./cmd/coverfloor cover.out`. Deleting either
                   fails the test.
                 - Unedited pins stay green, run TARGETED (not the full cmd suite):
                   `go test -count=1 -run 'TestCIGoTestStepsLive|TestGoTestStepScanner|TestCIConcurrencyLive|
                   TestToolchainSingleSource|TestWorkflowActionsArePinned|TestWorkflowsHaveNoExpressionsInRun|
                   TestCIShellcheckCoversScripts|TestGovulncheckPinAgrees' ./internal/cmd/`. A second go test
                   invocation would break TestCIGoTestStepsLive's "exactly one in test and one in mutation-ts".
                 - Pre-flight: an own-package profile over `go list ./internal/... | grep -v '/internal/cmd$'`,
                   run with a runner-like PATH (gitleaks/ast-grep/gremlins stripped) and fed to coverfloor,
                   exits 0. A file failing only there gets a test (ask first); an allowlist is never an option.
                 - The first PR CI run shows the floor step green, with its file count, and testsummary shows
                   no new >300s warning for internal/cmd versus the last main run.
                 - Supply-chain audit recorded: actions SHA-pinned, permissions contents: read,
                   GOFLAGS/GOPROXY/GOSUMDB set, govulncheck pinned. The new step adds no dependency.
       status:   pending

  t-12 Drain three packages to zero outstanding
       files:    .dross/phases/extracted-package-test-parity/notes.md, .dross/survivors.toml
       desc:     Run `dross survivor drain --packages ./internal/boardsync,./internal/diag,./internal/secretscan`
                 detached (remote, host-locked, long), then record the summary in notes.md. Re-classify
                 residual ceiling survivors from the raw reports via `--report`, not a second gremlins run.
       covers:   c-1
       depends:  t-2, t-4, t-5, t-6, t-7, t-8, t-9, t-10
       owns:     c-1's evidence (locked c1_evidence), and equivalent-mutant surprises.
       contract: - The drain summary line reads "... 0 outstanding", and no survivor is classified under
                   cross-package-only-coverage.
                 - Each residual is either (a) a case/const-initializer line whose go-cover count is >=1,
                   accepted under t-10's category and re-classified with --report, or (b) a gap: STOP and
                   route it back to the owning t-4..t-9 task (a pair decision). A new category is never
                   invented for an equivalent mutant.
                 - Any t-10 ceiling acceptance the drain shows KILLED is retired, so no false survival claim
                   remains.
       status:   pending

Wave 4 (depends t-11, t-12)
  t-13 Prove no behaviour change across cmd
       files:    .dross/phases/extracted-package-test-parity/notes.md
       desc:     A behaviour-preservation gate over the full internal/cmd suite, which nobody can run locally.
                 It runs after the drain so the two remote jobs don't contend for the helicon host lock.
       covers:   c-3
       depends:  t-11, t-12
       owns:     cross-package regression, and a "fix" slipped into production code while writing tests.
       contract: - `git diff --name-only <fork-point>..HEAD -- internal/cmd` is empty (so no cmd assertion
                   edits are possible).
                 - `git diff --name-only <fork-point>..HEAD -- '*.go' ':!*_test.go'` lists only
                   cmd/coverfloor/*.go. A defect found while testing becomes a `dross deferred add`, not an
                   edit.
                 - Every TestXxx added this phase is absent from
                   internal/cmd/testdata/cli_surface/tests_before.txt. This is the TestNoTestLost
                   "copied and left behind" failure, caught before the remote run.
                 - `dross test` (remote, full suite, never --local) is green, including TestNoTestLost,
                   issue*_test.go, doctor*_test.go and cli_surface_test.go.
       status:   pending
```

## Coverage

| Criterion | Tasks | Note |
|---|---|---|
| c-1 | t-1, t-2, t-4, t-5, t-6, t-7, t-8, t-9, t-10, t-12 | t-12 holds the evidence; t-10 does the retire and honest re-accept |
| c-2 | t-2 (writers.go), t-4 (inbound.go, milestone.go), t-5 + t-6 (backlog.go), t-7 (reap_apply.go), t-8 (reap_undo.go) | |
| c-3 | t-13 | Per-task name-collision checks in t-1, t-2 and t-13 feed it |
| c-4 | t-3 (checker, self-test, detach.go), t-11 (CI wiring, wiring pin, pre-flight) | |

All 4 of 4 criteria are covered.

## Judgment calls

- **Where the c-4 floor runs.** Chose a CI step that piggybacks `-coverprofile` on the existing test-job
  `go test`, followed by `go run ./cmd/coverfloor`. Rejected a Go test that shells out to `go test -cover`,
  because whichever package holds it would re-run the whole internal suite once per gremlins mutant. That
  is the exact blow-up the locked test_home decision exists to avoid, plus a recursion hole and double CI
  runtime. Also rejected a separate CI coverage step or job: it adds a third `go test` invocation, which
  breaks TestCIGoTestStepsLive, and editing that pin is a c-3 assertion edit.
- **Where the checker lives.** Chose cmd/coverfloor (stdlib-only, the testsummary precedent). Rejected
  internal/coverfloor: under internal/, any write, such as a step-summary file, would have to be declared in
  secretscan's writer registry (a production edit to writers.go), and the checker would police itself.
  Cost: this puts production code in the diff, so verify's diff-scoped mutation will mutate
  cmd/coverfloor. t-3's boundary and vacuity tests are sized for that.
- **How the comparison is done.** Chose integer comparison `covered*2 < total` with a fail-on-zero-files
  guard. Rejected float percentages (rounding at exactly 50%) and survivor.ParseProfile reuse (it drops
  statement counts and silently skips malformed lines).
- **Scope of c-1.** Chose to plan tests for all 118 routed survivors, including the 16 in phase.go, task.go,
  task_pull.go and reap.go (t-9) that c-2 never names. Rejected reading c-2's six files as the whole job: a
  file at 80% still leaves its other 20% as NOT COVERED survivors in the drain.
- **Test fixture style.** Chose a scripted, strict httptest tracker that fails on unscripted AND on unused
  routes. Rejected porting cmd's stateful YouTrack emulators (applyYT, undoYT, backlogCloseFake): an
  emulator is itself untested logic, and an answer-everything fake makes error-arm tests pass without ever
  reaching the tracker. Stateful paths (reconcile, apply, undo) use Go-level fakeBoard wrappers instead of
  HTTP.
- **Splitting backlog.go.** Chose to split it into outbound (t-5) and inbound (t-6) along failure domains:
  duplicate-minting vs wrongful-close. Rejected one task for 194 statements and 51 routed mutants: it is
  too large, and one test file would have two owners.
- **The writers.go switch-case lines.** Chose to re-accept them under a NEW category whose reason cites
  t-2's tests and TestPinLinesMatrix, and only after proving go-cover count >=1 on each line. Rejected
  reusing gremlins-switch-case (its prose cites another file's test) and gremlins-attribution-ceiling
  (generic, names no test that drives these arms).
- **Order of acceptances and drain.** Chose to accept predicted ceilings before a single drain, then
  re-classify residuals with `--report`. Rejected drain, then accept, then a second full drain: it doubles
  a 1h+ remote run for a well-proven ceiling. Any accepted line the drain shows killed is retired.
- **Equivalent mutants.** Chose STOP-and-ask. Rejected inventing an "equivalent-mutant" category: c-1
  permits only killed or gremlins-ceiling, and a production refactor to kill one would breach c-3.
- **Test names.** Chose new names throughout, checked against tests_before.txt. Rejected porting cmd e2e
  names such as TestUndoRefusesOnABackendWithoutStateWriter into boardsync: TestNoTestLost counts
  multiplicity and fails on the copy.
- **detach.go test.** Chose the os.Args[0] helper-process pattern. Rejected `sh -c` or `true`, which are
  shell/OS-dependent and can't prove stdout lands on stderr. It is folded into t-3 because it exists only
  to let the c-4 floor pass on day one.
- **How c-3 is proven.** Chose a structural proof: an empty `git diff` on internal/cmd plus the remote
  `dross test`. Rejected a local cmd run: it hangs about 600s on this laptop.
