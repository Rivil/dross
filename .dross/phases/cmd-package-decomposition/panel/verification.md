# Lens: verification

Design principle applied: every criterion got its ideal test contract written
first; each task is the smallest move that makes that contract satisfiable.
The two c-6 contracts (CLI-surface pin, doctor golden) are captured BEFORE any
code moves, so every later task is diffed against a recorded truth rather than
against a reviewer's memory of the output.

Package homes (four extracted + one enabling):

| domain | package | why this name |
|---|---|---|
| c-1 consent | `internal/consent` | the gate, the store contract, the framing |
| c-2 board reconciliation | `internal/boardsync` | `internal/board` already holds board.json |
| c-3 diagnostics | `internal/diag` | structured checks; `Doctor()` stays in cmd |
| c-4 adapter construction | `internal/mutationcfg` | locked `adapter_home` |
| enabling for c-2 | `internal/deferred` | boardsync needs `[]deferredEntry` + `ensureDeferredIDs`, and its moved tests need a real collector — cmd's cannot be imported |

Seams chosen where a moved function touched cmd-only persistence (local.go is
deferred, so it stays):

- consent: `type Store interface { Load() (*Grants, error); Save(*Grants) error }`.
  `consent.Grants` owns the five `trusted_*` toml fields; cmd's `localStore`
  EMBEDS it (BurntSushi flattens anonymous structs) so there is still ONE
  writer of local.toml and `l.TrustedTestCommand` keeps resolving in cmd.
  `consent.RefuseTrackedLocal(repoDir)` moves too (fixed-argv `git ls-files`,
  exec-exempt marker); `readAllowHosts`/`readRemoteGrants` call it.
- mutationcfg: `Local{Targets []*remote.Target; Workers, TestCPU int}` is READ
  by cmd from local.toml and passed in; the remote pool walk stays in cmd
  (`remote_pool.go`) and is passed as `Probe func([]*remote.Target) (*remote.Target, cores int, fallbackWhy string, error)`.
- diag: checks take structured input (pins already classified, allow-host
  list already read) and return `[]Line`/`[]Section`; cmd gathers and prints.

Phase cmd-package-decomposition — 11 tasks across 3 waves

Wave 1

  t-1  Pin CLI surface and doctor output
       files:    internal/cmd/cli_surface_test.go,
                 internal/cmd/testdata/cli_surface/trust.txt,
                 internal/cmd/testdata/cli_surface/issue.txt,
                 internal/cmd/testdata/cli_surface/doctor.txt,
                 internal/cmd/testdata/cli_surface/verify.txt,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-6
       description:
                 Walk Trust()/Issue()/Doctor()/Verify() cobra trees (Use, Aliases,
                 Short, every flag: name, shorthand, default, usage, declared
                 order) into a text rendering and check it in as goldens. Run
                 `dross doctor` over consentFixture (git-version seam pinned,
                 no [remote], no [board], auth_env empty) and check the full
                 stdout + exit code in as doctor_run.txt. Also record the
                 pre-phase `func Test*` name set of internal/cmd into
                 testdata/cli_surface/tests_before.txt for t-11's no-test-lost
                 check.
       test_contract:
                 - Renaming `--lane-install`, dropping `issue reap undo`, or
                   changing `verify --detach`'s default fails
                   TestCLISurfacePinned naming the command and the diff line.
                 - A changed glyph, section order, blank line or exit code in
                   `dross doctor` over the fixture fails TestDoctorRunGolden
                   with a unified diff.
                 - The golden files are written by the test under
                   DROSS_UPDATE_GOLDEN=1 only; a run without it that finds no
                   golden fails rather than passing vacuously.
       depends_on: []
       status:   pending

  t-2  Extract consent gate into internal/consent
       files:    internal/consent/consent.go (State, Err*, Fingerprint,
                   CheckConsent, GrantConsent, Replay*/Run* consent,
                   fingerprintInSet, addFingerprint, RefuseTrackedLocal),
                 internal/consent/lane.go (laneFrame/laneTemplateFrame/
                   laneInstallFrame, LaneLine, LaneInstallLine, LaneConsented,
                   GrantLaneConsent, RevokeLaneConsent, LaneInstallConsented,
                   GrantLaneInstallConsent, RevokeLaneInstallConsent),
                 internal/consent/gate.go (GatedCommands roster, RequireExec,
                   Refusal, LaneRefusal, LaneInstallRefusal),
                 internal/consent/store.go (Grants, Store),
                 internal/consent/consent_test.go, internal/consent/lane_test.go,
                 internal/consent/store_test.go (file-backed test Store),
                 internal/cmd/trust.go (keeps Trust(), trustLane, trustRun,
                   trustReplay, trustLaneInstall, recordedReplayLine, findLane,
                   `requireExecConsent()` = FindRoot + project.Load +
                   consent.RequireExec, `var execGatedCommands = consent.GatedCommands`),
                 internal/cmd/local.go (localStore embeds consent.Grants;
                   `grantStore(root) consent.Store`; refuseTrackedLocal
                   callers → consent.RefuseTrackedLocal),
                 internal/cmd/{doctor,lane_install,lane_preview,lane_preview_locality,
                   remote_bootstrap,remote_grant,redproof_replay,run,test,test_lane,
                   test_lane_install}.go (call-site re-pointing only)
       covers:   c-1
       description:
                 Move trust.go lines 1-806 minus findLane/recordedReplayLine to
                 internal/consent with `root` replaced by a `Store` parameter;
                 cmd gains `grantStore(root)`. `type ConsentState = consent.State`
                 and the five `Consent*` constants stay as aliases in cmd so
                 no cobra-level test edits its assertions. Unit tests move
                 (TestFingerprint, TestGrantStoresHashNotCommand,
                 TestConsentStates, TestConsentRefusesTrackedLocalToml,
                 TestConsentNotApplicable, the lane framing/isolation/revoke
                 tests, the install-frame tests); tests that run `dross trust`,
                 `dross doctor` or `dross local set` stay in cmd.
       test_contract:
                 - consent.Fingerprint("go test ./... ") != Fingerprint("go test ./...")
                   (moved TestFingerprint) — a normalizer added later fails it.
                 - A Store whose Load returns TrustedTestCommand ==
                   Fingerprint("a") answers ConsentStale for "b" and
                   ConsentGranted for "a"; an empty testCmd answers
                   ConsentNotApplicable with ErrNoTestCommand (moved
                   TestConsentStates / TestConsentNotApplicable).
                 - With .dross/local.toml `git add -f`'d, CheckConsent returns
                   ConsentRefused and the error names "git rm --cached"; after
                   `git rm --cached` the same store grants (moved
                   TestConsentRefusesTrackedLocalToml, over the file-backed test
                   Store + a real git repo).
                 - NEW TestLocalStoreRoundTripsGrants (cmd): write every
                   trusted_* key through cmd's localStore.save, read back through
                   grantStore(root).Load() — if BurntSushi does not flatten the
                   embedded Grants, or a field tag drifts, the read is empty.
                 - cmd's TestExecGatedSetIsExplicit and TestGatedCommandsRefuse
                   run unchanged: the roster is `consent.GatedCommands` and
                   `requireExecConsent` keeps its name, so the exec-audit's
                   name rule (`requireExecConsent` / `*Consented`) still marks
                   every gated RunE; TestEverySpawnSiteGatedOrExempt stays
                   green because the moved `git ls-files` spawn carries its
                   exec-exempt marker (mixed reach: gated commands + doctor).
                 - TestLocalSetCannotGrantConsent (cmd, unchanged): `dross
                   local set trusted_test_command x` still refuses.
       depends_on: []
       status:   pending

  t-3  Extract deferred data layer into internal/deferred
       files:    internal/deferred/deferred.go (Entry, Collect, Flatten,
                   ProjectStoreSlug, StorePath, Store, LoadStore, EnsureIDs,
                   NewID, Filter),
                 internal/deferred/deferred_test.go,
                 internal/cmd/deferred.go (`type deferredEntry = deferred.Entry`,
                   cobra verbs + route/dismiss/unroute/repoint logic stay),
                 internal/cmd/deferred_store.go (thin aliases or deleted),
                 internal/cmd/issue.go (ensureDeferredIDs/newDeferredID removed;
                   syncBacklog calls deferred.EnsureIDs),
                 internal/cmd/deferred_store_test.go (store-addressing unit
                   tests move; verb tests stay)
       covers:   c-2 (enabling)
       description:
                 Move the [[deferred]] flatten/collect/store-path code and the
                 id backfill out of cmd. cmd keeps the `dross deferred` verbs
                 and verify.go's collectDeferred call re-points to
                 deferred.Collect.
       test_contract:
                 - deferred.Store(root, "_project") returns .dross/deferred.toml
                   and Store(root, "no-such-phase") errors naming "_project" as
                   the only non-phase source (moved TestDeferredRouteAddressesStore / TestDeferredUnrouteDismissAddressStore).
                 - EnsureIDs over a spec with two id-less items writes two
                   distinct 16-hex ids and a second call rewrites nothing
                   (file mtime and bytes unchanged) — the idempotence clause of
                   the ensureDeferredIDs doc comment, now a test.
                 - Collect skips a `phases/_project` directory (existing
                   validate-collision behaviour) — moved test.
                 - cmd's deferred_board_test.go and deferred_routed_sync_test.go
                   run unchanged (they drive `dross issue backlog sync`).
       depends_on: []
       status:   pending

  t-4  Extract adapter construction into internal/mutationcfg
       files:    internal/mutationcfg/mutationcfg.go (Tuning, Local, Probe,
                   ResolveTuning, Adapters, Selected, RemoteTools, ToolFor,
                   DockerPrefix, ProfileCacheVars, Tuning.Gremlins, MeasuredOn),
                 internal/mutationcfg/mutationcfg_test.go,
                 internal/cmd/verify.go (`type mutationTuning = mutationcfg.Tuning`;
                   configuredAdapters(p, root, skip) becomes the thin reader of
                   readRemoteGrants+readMutationTuning → mutationcfg.Adapters
                   with the selectRemoteTarget closure; resolveMutationTuning
                   same; measuredOnOf → mutationcfg.MeasuredOn),
                 internal/cmd/survivor_drain.go (mt.Gremlins),
                 internal/cmd/doctor.go (remoteMutationTools body →
                   mutationcfg.RemoteTools; remoteAdapterTools/Order deleted),
                 internal/cmd/mutation_remote_wiring_test.go,
                 internal/cmd/verify_detach_test.go (type alias keeps literals)
       covers:   c-4
       description:
                 Move mutationTuning, resolveMutationTuning, configuredAdapters,
                 dockerPrefix, profileCacheVars, the gremlins constructor,
                 measuredOnOf and doctor's adapter→tool table into one package;
                 `Selected(p) []string` is the single allowlist rule both
                 Adapters and RemoteTools derive from. Package imports project,
                 mutation, remote, stack, verify (stack for profile cache vars —
                 see judgment calls).
       test_contract:
                 - TestToolsFollowSelection: a project with adapters=["stryker-net"]
                   yields Adapters names ["stryker-net"] and RemoteTools
                   ["dotnet"] from the SAME Selected(); re-adding a private
                   allowlist loop to RemoteTools that treats an empty list
                   differently fails it (empty list → all three, in
                   stryker/gremlins/stryker-net order).
                 - TestResolveTuningFallsBackWhenUnreached: Probe returning
                   (nil, 0, "ssh exit 255", nil) with one Target yields
                   Prefix==DockerPrefix(p), FellBackFrom==host, Target==nil;
                   Probe returning an error yields the "remote mutation host %s
                   is not usable" error and no adapters; Probe returning a
                   target with cores 8 yields Target.Cores==8 and Prefix=="".
                 - TestDockerPrefixRefusesLookalikeBinary: "dockerevil compose
                   exec app go test" yields the default "docker compose exec
                   app" (moved from mutation_remote_wiring_test's dockerPrefix
                   assertions).
                 - cmd's mutation_remote_wiring_test.go stays and passes
                   unchanged: configuredAdapters(p, root, false) with a
                   local.toml grant still sets every adapter's Remote — the cmd
                   reader is the only place local.toml is decoded.
                 - trust_test's refuseAdapters seam (configuredAdaptersFn) keeps
                   its signature; TestVerifyRefusesWithoutConsent unchanged.
       depends_on: []
       status:   pending

  t-5  Create internal/diag with the pure checks
       files:    internal/diag/diag.go (Level, Line, Section, Issues, Warnings),
                 internal/diag/redproof.go (RedProofPin, Reach, PinLines,
                   DiscoveryFailed, SameCommitSHA),
                 internal/diag/roadmap.go (DuplicateRoadmapSlug, DuplicateRoadmapSlugs),
                 internal/diag/combinations.go (RemoteCombinationWarnings,
                   BoardCombinationWarnings, SortedStateMapKeys, LooksLikeBoardURL),
                 internal/diag/git.go (GitVersion seam var, GitVersionAtLeast,
                   EndOfOptionsMinGit, PhaseCommitsOnMain, LeakedPhaseCommit,
                   ExtractCommitSHAs — the `git rev-list` spawn with its
                   exec-exempt marker),
                 internal/diag/diag_test.go, internal/diag/redproof_test.go,
                 internal/diag/roadmap_test.go, internal/diag/combinations_test.go,
                 internal/cmd/doctor.go (`type doctorLine = diag.Line`,
                   doctorOK/Warn/Issue = diag constants; redProofChecks and
                   redProofPinLines become the cmd composers: discover pins,
                   classifyReachability, redProofDocSHA, redProofRepointHint →
                   diag.PinLines; duplicateRoadmapSlugs/remote+board
                   CombinationWarnings/phaseCommitsOnMain/gitVersionOutput
                   re-pointed; `printLines(lines)` renderer added),
                 internal/cmd/doctor_test.go (`.level`/`.text` → `.Level`/`.Text`,
                   `gitVersionOutput =` → `diag.GitVersion =`; no assertion text
                   changes)
       covers:   c-3 (part 1)
       description:
                 Land the structured-result vocabulary and move every doctor
                 check that is already a pure function over data. Red-proof
                 verdict text becomes a matrix over a pre-classified pin so it
                 is testable without git. Rendering rule: OK → "  ✓ ", Issue →
                 "  ✗ ", Warn → "  ⚠ ", Note → verbatim (continuation lines like
                 "    Fix: …").
       test_contract:
                 - TestPinLinesMatrix (diag): Reach=Unreachable → one Issue line
                   containing "unreachable" and the RepointHint; Indeterminate →
                   one Warn; DocErr set → Issue containing "cannot be read";
                   DocSHA "" → Issue containing "carries no `base commit:`";
                   DocSHA abbreviated prefix of SHA → no doc line; all clear →
                   exactly one OK line. Swapping Issue/Warn on any arm fails.
                 - TestSameCommitSHA: ("abc123","ABC") true, ("","x") false.
                 - TestDuplicateRoadmapSlugs: two milestone.toml files listing
                   "x" report one entry {x, [v1, v2]}; a slug repeated inside ONE
                   array reports nothing (moved from doctor_test).
                 - TestRemoteCombinationWarnings: ("bitbucket","", "") warns on
                   auth_user; ("github","basic","u") warns "no effect";
                   ("none","","") is empty (moved).
                 - TestGitVersionAtLeast("git version 2.23.0", "2.24") false.
                 - cmd: redproof_*_test.go and doctor_test.go red-proof tests
                   still call redProofChecks(root, repoDir) and see the same
                   levels/texts; TestDoctorRunGolden (t-1) is byte-identical
                   after the renderer swap.
       depends_on: []
       status:   pending

Wave 2

  t-6  Extract board sync core into internal/boardsync   (depends t-3)
       files:    internal/boardsync/ctx.go (Ctx{Client, Board, Proj, Root,
                   BoardPath, Out io.Writer}, Open(proj, root, extraHosts),
                   Config, WrapErr, label vocabulary + Status* consts),
                 internal/boardsync/phase.go (SyncPhase, ResolvePhaseIssue,
                   LookupPhaseIssue, HasMarker, DerivePhaseStatus,
                   RenderPhaseBody, CloseIssue, VerifyClosed),
                 internal/boardsync/milestone.go (CheckMilestoneClosable,
                   EnsureMilestoneLink, MilestoneBody, SyncMilestone),
                 internal/boardsync/backlog.go (BacklogItem, ResolveBacklogVersion,
                   SyncBacklog, ReconcileBacklog, BacklogVerdictFor,
                   BoardIssueIsDone, AdoptLegacyBacklogKey, PushBacklogItems,
                   DeferredBacklogKey/Label, TargetLabel, LegacyDeferredBacklogKey),
                 internal/boardsync/inbound.go (CollectInbound, PullEnvelope,
                   EmitPullEnvelope(w, …), ReportBoardFailure, QuickOpen, QuickClose),
                 internal/boardsync/*_test.go (moved unit tests: backlog id/
                   close truth/phase resolve/milestone close/quick close/
                   lifecycle divergence tests that call the functions directly),
                 internal/cmd/issue.go (keeps Issue() tree, openBoard() =
                   loadProject + FindRoot + readAllowHosts → boardsync.Open,
                   every RunE re-pointed; `type boardCtx = boardsync.Ctx`)
       covers:   c-2 (part 1)
       description:
                 Move issue.go's non-cobra code into boardsync with output on
                 ctx.Out (cmd passes os.Stdout at open time, so runCmdCapturing
                 still sees it). Field names of Ctx are exported; cmd test
                 fixtures that build a boardCtx literal use the exported names
                 (mechanical edit, no assertion change).
       test_contract:
                 - TestSyncPhaseCreatesThenUpdates (moved): first SyncPhase
                   against a fake client creates one issue with labels
                   [dross, dross/status:planned, dross/phase:<id>]; the second
                   call updates the same key and creates nothing — a lost
                   board.json link creates a duplicate and fails it.
                 - TestPhaseSyncCloseFailsWhenTheIssueStaysUnresolved +
                   TestFlatBoardCloseFailsWhenTheIssueStaysOpen (moved
                   close-truth tests): CloseIssue re-reads the issue after the
                   transition and errors when the tracker did not actually
                   resolve it; TestUnverifiedCloseIsNotCountedClosed keeps the
                   count honest.
                 - TestOpenRefusesUnlistedBoardHost: Open with base_url on a
                   host outside hostallow.Derive(remoteURL, extra) errors
                   before any request (the allowlist rides Config.Hosts).
                 - cmd: issue_test.go's 48 tests (all drive `dross issue …`
                   through cobra with httptest boards) pass with no assertion
                   edits; TestCLISurfacePinned's issue.txt golden unchanged.
                 - boardsync imports no cobra (locked in by t-11).
       depends_on: [t-3]
       status:   pending

  t-7  Move config-trust, lane-consent, toolchain checks into diag; Doctor composes   (depends t-2, t-4, t-5)
       files:    internal/diag/trust.go (TrustInput{RepoDir, Project, AllowHosts,
                   AllowHostsErr, Grants consent.Store, GatedCommands,
                   ValidateRef func, IgnoresPath func, LocalIgnorePath},
                   ConfigTrust(in) []Section — Branch names / API host /
                   Machine-local store / git version / Exec consent (+ gated
                   surface note + one row per lane)),
                 internal/diag/toolchain.go (LookPath seam var,
                   MutationToolchain(p) []Section via mutationcfg.RemoteTools,
                   ToolInstall, ToolLanguage),
                 internal/diag/trust_test.go, internal/diag/toolchain_test.go,
                 internal/cmd/doctor.go (checkConfigTrust/reportExecGatedSurface/
                   reportLaneConsent/printLanePrepare/checkMutationToolchain/
                   execLookPath/mutationToolInstall/mutationToolLanguage
                   deleted; RunE composes: printSections(diag.ConfigTrust(in));
                   issues += checkRemoteMutation(...); printSections(
                   diag.MutationToolchain(p)); os/exec import gone),
                 internal/cmd/doctor_test.go, internal/cmd/doctor_lane_toolchain_test.go,
                 internal/cmd/consent_surface_test.go (`execLookPath =` →
                   `diag.LookPath =`; nothing else)
       covers:   c-3 (part 2)
       description:
                 Convert the printing checks to section-returning ones. Issue
                 count = diag.Issues(sections); the same ✗ lines that
                 incremented before are the Issue-level lines now, and ⚠ lines
                 stay Warn (never counted), preserving exit codes. Doctor's
                 RunE for these blocks becomes gather → call → print.
       test_contract:
                 - TestConfigTrustExecConsentLadder (diag): a Store returning a
                   stale fingerprint yields the "Exec consent:" section with
                   one Issue line containing "CHANGED" and Issues()==1; an
                   absent store yields a Warn ("has not trusted") and
                   Issues()==0; with lanes declared and testCmd "" the Warn says
                   "`dross test --files` still runs the lanes" — the lane-aware
                   wording arm.
                 - TestConfigTrustLaneRows: two lanes, one granted one stale,
                   yield "lane \"a\": trusted" OK and a stale Issue naming
                   `dross trust --lane b`; a prepared lane's row includes the
                   prepare line (printLanePrepare behaviour).
                 - TestMutationToolchainNamesTheAdapter: LookPath failing for
                   "gremlins" with adapters=["gremlins"] yields one Warn naming
                   gremlins and "go" and the install hint; LookPath succeeding
                   yields no section at all (today's early return).
                 - TestConfigTrustHostOutsideAllowlist: api_base on
                   evil.example yields an Issue plus a Note line "Fix (only if
                   you trust this host): `dross local set allow_hosts
                   evil.example`".
                 - cmd (unchanged assertions): TestDoctorReportsStaleConsent,
                   TestDoctorReportsAbsentConsent, TestDoctorStaleLaneMovesTheExitCode,
                   TestDoctorCountsEachRefusedLane, TestDoctorNamesTheGatedSurface,
                   TestDoctorShowsAPreparedLanesBootstrap and the toolchain tests
                   pass; TestDoctorRunGolden (t-1) byte-identical.
                 - internal/cmd/doctor.go no longer imports os/exec — t-11's
                   baseline omits it, so re-adding the import fails the ratchet.
       depends_on: [t-2, t-4, t-5]
       status:   pending

Wave 3

  t-8  Move reap into boardsync   (depends t-6)
       files:    internal/boardsync/reap.go (Verdict, Card, Plan, Lane, lanes
                   table, Classify, BuildPlan, ResolveLanes, phase/task/
                   milestone/backlog/quick mirror classifiers, RoadmapSlugs),
                 internal/boardsync/reap_discover.go (orphan identity, Discover,
                   OrphanVerdict),
                 internal/boardsync/reap_apply.go (Inventory, Apply, Failure,
                   RelabelReapedCard, DropBacklogLink, AppendRun, LogPath),
                 internal/boardsync/reap_undo.go (Undo, RestoreDroppedLink),
                 internal/boardsync/reap_*_test.go (moved: classify, discover,
                   apply, undo unit tests incl. the readOnlyYT fixture),
                 internal/cmd/issue_reap_cmd.go (cobra verb, printReapPlan,
                   validateReapNamespaces, boardNamespaceNames stay),
                 internal/cmd/issue_reap.go, issue_reap_apply.go,
                   issue_reap_discover.go, issue_reap_undo.go (deleted)
       covers:   c-2 (part 2)
       description:
                 Move the classify/discover/apply/undo cluster. phaseDirExists
                 becomes os.Stat(phase.Dir(root, slug)) inside boardsync;
                 deferred entries come from deferred.Collect.
       test_contract:
                 - readOnlyYT fixture (moved with issue_reap_classify_test.go): any
                   non-GET during Classify fails the test — a classifier that
                   decides by patching a card reddens here.
                 - TestPhaseCardIsClassifiedFromItsCompletionRecord (moved):
                   changes.json status complete → verdict reap; shipped → not
                   yet; no phase dir → unattributable.
                 - TestUndoRestoresTheRecordedPriorState + TestUndoRestoresDroppedBoardLinks
                   (moved): after Apply then Undo, every card is back to its
                   recorded prior state and a dropped Backlog link is re-set in
                   board.json; TestSecondApplyIsANoOp (moved) writes nothing.
                 - cmd: issue_reap_cmd_test.go (drives `dross issue reap`)
                   passes unchanged; `--dry-run` output identical.
       depends_on: [t-6]
       status:   pending

  t-9  Move task sync and task pull into boardsync   (depends t-6)
       files:    internal/boardsync/task.go (TaskLabel, SyncTasks, SyncOneTask,
                   ResolveTaskIssue, RenderTaskBody, SetBoardState,
                   RunWarnings, TaskCloseError),
                 internal/boardsync/task_pull.go (TaskMoveKind, TaskMoveVerdict,
                   TaskPull(ctx, phaseID, plan, planPath, apply), ProviderHasWorkflowState,
                   CollectTaskMoves, ClassifyTaskMove, LifecycleFromLabels,
                   ReportTaskMoves),
                 internal/boardsync/lifecycle.go (taskLifecycle, boardStatusToPlan,
                   LifecycleForPlanStatus, PlanStatusForLifecycle,
                   StatusTaskComplete),
                 internal/boardsync/task_test.go, task_pull_test.go, lifecycle_test.go (moved),
                 internal/cmd/issue_task.go (issueTaskSync verb only),
                 internal/cmd/issue_task_pull.go (issueTaskPull verb:
                   resolveTaskPhaseID + loadPhasePlanAndSpec → boardsync.TaskPull),
                 internal/cmd/task_lifecycle.go (deleted; aliases if
                   task_lifecycle_test.go stays)
       covers:   c-2 (part 3)
       description:
                 Move task mirroring and the inbound task-state pull. The cobra
                 verb resolves the phase and loads the plan (cmd helpers), the
                 package does the reconciliation.
       test_contract:
                 - TestClassifyBoardMoved / TestClassifyConflict / TestClassifyUnchanged
                   (moved): an issue whose tracker state is "In Progress" but
                   whose dross/status label is task-in-review classifies as
                   move-to-done, not in_progress — inverting the provider map
                   instead of reading the label fails them.
                 - TestTaskCloseRequiresAStatus + TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing
                   (moved): doClose without a status refuses before touching
                   the board; a mid-run failure returns a TaskCloseError naming
                   the task and the remaining tasks are still closed.
                 - TestLifecycleMappingRoundTrips (moved): every taskLifecycle value
                   inverts back, and task-complete → done.
                 - cmd: TestTaskPullCommandNoOpsWhenBoardIsOff / NeedsAPhase /
                   SurfacesASetupFailure stay in cmd and pass unchanged;
                   TestReportDryRunWritesNothing moves.
       depends_on: [t-6]
       status:   pending

  t-10 Architecture boundary test with shrink-only ratchet   (depends t-2, t-4, t-6, t-7)
       files:    internal/cmd/boundary_test.go,
                 internal/cmd/testdata/cli_surface/tests_before.txt (read)
       covers:   c-5, c-6
       description:
                 go/parser ImportsOnly over every non-test .go file under
                 internal/ (skipping testdata), grouped by package dir.
                 Asserts: (a) cobra imported only by internal/cmd; (b) no
                 package under internal/ other than cmd imports
                 internal/cmd; (c) internal/cmd imports consent, boardsync,
                 diag, mutationcfg; (d) ratchet: non-test cmd files importing
                 os/exec must be in execBaseline (17 names: cleantree, init,
                 lane_install, milestone_stale, pause, phase, redproof_replay,
                 run, ship_recover, stack, statusline, survivor_drain,
                 techdebt, test, update, verify, worktree_files), net/http in
                 httpBaseline {update.go}, go/ast in astBaseline {} — an
                 unlisted importer fails AND a listed file that no longer
                 imports fails ("remove it from the baseline"); (e) a package
                 floor (≥ 40 packages seen) so a narrowed walk cannot pass;
                 (f) no-test-lost: every name in tests_before.txt exists as a
                 `func Test` somewhere under internal/.
       test_contract:
                 - Adding `"os/exec"` to internal/cmd/issue.go fails
                   TestCmdForbiddenImportRatchet naming issue.go and the import.
                 - Adding a name to execBaseline for a file that does not import
                   os/exec fails the same test as a stale entry — the list can
                   only shrink truthfully.
                 - Adding `github.com/spf13/cobra` to internal/boardsync fails
                   TestCobraOnlyInCmd naming boardsync.
                 - Adding `internal/cmd` to internal/diag's imports fails
                   TestNoReverseImportOfCmd.
                 - Deleting cmd's use of internal/consent fails
                   TestCmdImportsExtractedPackages.
                 - Pointing the walk at internal/cmd only fails
                   TestBoundaryWalkFloor (mirrors execConsentFloor).
                 - Deleting a moved test instead of moving it fails
                   TestNoTestLost naming it.
       depends_on: [t-2, t-4, t-6, t-7]
       status:   pending

  t-11 Prove c-6 end to end and drop the pre-move scaffolding   (depends t-8, t-9, t-10)
       files:    internal/cmd/cli_surface_test.go,
                 internal/cmd/testdata/cli_surface/*.txt,
                 .dross/phases/cmd-package-decomposition/verify.toml (by dross verify, not by hand)
       covers:   c-6
       description:
                 With every move landed: re-run TestCLISurfacePinned and
                 TestDoctorRunGolden WITHOUT DROSS_UPDATE_GOLDEN (they must pass
                 against the pre-move goldens), run `go test -count=1 ./...`
                 remotely via `dross test`, and check `git diff <phase base>
                 -- 'internal/cmd/*_test.go'` shows only deletions (moved
                 files), identifier re-pointing (`.level`→`.Level`,
                 `execLookPath`→`diag.LookPath`, `gitVersionOutput`→
                 `diag.GitVersion`, boardCtx field case) and no changed string
                 literal inside a t.Error/t.Fatal/want. The goldens stay as a
                 permanent guard; nothing is deleted.
       test_contract:
                 - TestCLISurfacePinned and TestDoctorRunGolden pass against
                   goldens recorded before any code moved (t-1); a flag renamed
                   during any move shows here even if every other test passed.
                 - `grep -c 'func Test' internal/cmd/*_test.go` +
                   the four packages' counts ≥ the pre-phase cmd count
                   (TestNoTestLost, t-10).
                 - The observed `dross test` run is green in this session
                   before verify.toml is written (rule: never claim an unseen
                   result).
       depends_on: [t-8, t-9, t-10]
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-3 (enabling), t-6, t-8, t-9 |
| c-3 | t-5, t-7 |
| c-4 | t-4 |
| c-5 | t-10 |
| c-6 | t-1 (contract recorded first), t-10 (no-test-lost), t-11 (final proof) |

## Judgment calls

- **Goldens before moves (t-1).** Chose to capture the CLI tree and a full
  doctor transcript as checked-in goldens in wave 1 rather than rely on
  "existing tests pass": existing tests assert substrings, so a dropped blank
  line or reordered section would not fail them. Rejected recording goldens at
  the end (that proves nothing about drift).
- **consent Store interface + embedded Grants** rather than moving local.go
  (deferred by the spec) or giving consent its own local.toml codec (two
  writers of one file; one would drop the other's keys). Embedding keeps one
  writer and one schema; the cost is one 20-line file-backed Store in the
  consent tests.
- **internal/deferred is a fifth extraction (t-3), not an injected loader.**
  Rejected `Ctx.Deferred func() ([]Entry, error)` because the moved backlog
  and reap unit tests need a real collector and cannot import cmd. ~150 lines
  of pure data code; the `dross deferred` verbs stay in cmd.
- **Remote pool walk stays in cmd; mutationcfg takes a Probe func.** Rejected
  moving remote_pool.go into internal/remote (a sixth move, five callers).
  The func seam is also what makes the fallback-vs-abort matrix testable
  without ssh (t-4 contract).
- **mutationcfg imports stack** for profileCacheVars, beyond the locked
  "project, mutation and remote". The lock's why is that internal/mutation
  stays project-agnostic, which holds; the alternative (caller passes
  cacheVars) leaves construction logic in cmd. Flagged for the judge.
- **Ratchet baseline is 17, not ~13.** doctor.go drains (its three spawns are
  diagnostics and move to diag). verify.go (detach self-exec), phase.go (git
  plumbing helpers), survivor_drain.go (`go list`/`go test`) and test.go (lane
  runner) keep os/exec for reasons outside the four domains and are listed;
  draining them is the deferred cmd-exec-baseline-drain target.
- **Ratchet scope is non-test files.** 21 cmd test files import os/exec and
  the exec-audit test imports go/ast; the rule targets domain logic landing
  in cmd, which tests are not.
- **Stale baseline entries fail** (not just unlisted importers). "Shrink-only"
  is enforced by making the baseline exact, so a drained file must be removed
  from the list in the same commit.
- **c-3 scope is the six named checks plus what shares their functions**
  (checkConfigTrust's branch/host/gitignore/git-version sub-blocks, and
  phaseCommitsOnMain because it is doctor.go's other spawn). The other
  inline RunE sections (remote/board field checks, .gitattributes, version
  parity, stale branches, backfill residue, stranded mirrors) stay inline —
  candidate for a follow-up "doctor-sections-to-diag" item, not this phase.
- **diag.ConfigTrust takes injected ValidateRef/IgnoresPath** rather than
  moving refguard.go/gitignore.go (both are cmd-wide helpers with their own
  callers). Two func fields on the input struct; the checks stay pure.
- **`doctorLine` becomes an alias of diag.Line with exported fields**, so 20
  `l.level`/`l.text` reads in cmd tests are re-pointed. Rejected a
  cmd-side conversion loop that would keep unexported fields purely to avoid
  a mechanical rename; c-6's "no assertion edits" is read as "no changed
  expectation", which the t-11 diff check enforces.
- **cmd keeps same-named composers where it genuinely composes**
  (redProofChecks, configuredAdapters, resolveMutationTuning, openBoard,
  requireExecConsent, remoteProbeTools) and gets no other pass-through shims.
- **Board reconciliation split into three tasks** (core, reap, task) rather
  than one: each has its own moved-test set and its own cobra-level guard,
  and issue.go alone is 1.6k lines.
- **Boundary test lives in internal/cmd/boundary_test.go**, next to the
  exec-audit sweep that already walks the repo from cmd, rather than a
  test-only package under internal/ (which `go build ./...` skips silently).
