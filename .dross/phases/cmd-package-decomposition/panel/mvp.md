# Lens: mvp — smallest task set that satisfies every criterion

Phase cmd-package-decomposition — 7 tasks across 3 waves

Seam rule applied everywhere: an extracted package takes *already-read inputs*
from cmd (local.toml values, git results, the remote pool pick) rather than
dragging cmd's git plumbing or the deferred local.go persistence with it. The
only new package not named by the spec is `internal/deferred`, which exists
because both `dross deferred` (cmd) and board reconciliation need the same
ledger reader and neither may import the other.

Wave 1

  t-1  Extract consent package from trust.go
       files:    internal/consent/consent.go, internal/consent/lane.go,
                 internal/consent/store.go, internal/cmd/trust.go,
                 internal/cmd/local.go
       covers:   c-1, c-6
       description:
         Move from trust.go into internal/consent: ConsentState (+ the five
         states, exported as consent.Granted/Stale/Absent/Refused/
         NotApplicable), the Err* sentinels, Fingerprint, CheckConsent → Check,
         GrantConsent → Grant, ReplayConsented/GrantReplayConsent,
         RunConsented/GrantRunConsent, fingerprintInSet/addFingerprint, the
         laneFrame/laneTemplateFrame/laneInstallFrame constants,
         laneConsentLine → LaneLine, laneInstallConsentLine → LaneInstallLine,
         LaneConsented/GrantLaneConsent/RevokeLaneConsent,
         LaneInstallConsented/GrantLaneInstallConsent/RevokeLaneInstallConsent,
         consentRefusal → Refusal, laneConsentRefusal → LaneRefusal,
         laneInstallRefusal → LaneInstallRefusal. Every exported check keeps
         its `Consented` suffix — execconsent_audit_test.go's
         isExecConsentCall recognises gates by that name shape.
         Persistence seam (store.go): `type Grants struct` holds the five
         trusted_* fields (moved out of localStore, which now EMBEDS
         consent.Grants so `dross local set` round-trips them untouched);
         `loadGrants(path)` decodes local.toml into Grants; `saveGrants(path,
         Grants)` decodes the file into map[string]any, overlays the five keys
         (deleting a key whose value is empty), re-encodes — so quick_base,
         remote_*, detached_run survive a grant. `File = "local.toml"`,
         `RelPath = ".dross/local.toml"`, and refuseTrackedLocal →
         `RefuseTrackedStore(repoDir)` (its own `git -C repoDir ls-files
         --error-unmatch -- .dross/local.toml` with a //dross:exec-exempt
         marker) move here; local.go's readAllowHosts calls it.
         trust.go keeps: Trust() and its subcommand bodies (trustLane,
         trustRun, trustReplay, trustLaneInstall, recordedReplayLine),
         findLane, execGatedCommands + its doc comment (execconsent_docs_test
         pins it by path), and requireExecConsent — same name, now
         FindRoot → project.Load → consent.Check → consent.Refusal.
         Callers switch to the qualified names: doctor.go, lane_install.go,
         lane_preview.go, lane_preview_locality.go, redproof_replay.go,
         run.go, remote_bootstrap.go, test.go, test_lane.go,
         test_lane_install.go (mechanical qualifier edits, no logic).
         Tests: the direct-call halves of trust_test.go (TestFingerprint,
         TestGrantStoresHashNotCommand, TestConsentStates,
         TestConsentNotApplicable's store half), trust_lane_test.go
         (TestOneLaneGoesStaleAlone, TestRenamedLaneInheritsNothing,
         TestLaneWithNoCommandIsNotApplicable, TestRevokeLaneConsentDropsOnlyThatLane,
         the framing tests) and trust_lane_install_test.go (frame/NUL/
         isolation tests) move to internal/consent/*_test.go with unexported
         access. Tests that run Trust()/Doctor()/Verify() through
         runCmdCapturing stay in cmd untouched.
       test_contract:
         - if Fingerprint starts trimming or collapsing whitespace, the moved
           consent.TestFingerprint fails ("a  b" and "a b" must not collide)
         - if saveGrants clobbers a non-grant key, new
           consent.TestSaveGrantsPreservesForeignKeys fails: a local.toml
           seeded with quick_base, remote_host and one [[detached_run]] table
           must still decode all three after Grant + GrantLaneConsent
         - if revoking the last lane leaves an empty `trusted_lane_installs`
           table behind, moved TestRemoveDropsAnInstallOnlyGrant's
           "key absent from the file" assertion fails
         - if requireExecConsent is renamed, moved out of cmd, or stops
           returning before I/O, cmd's unchanged TestGatedCommandsRefuse,
           TestRefusalWritesNothing and TestExecGatingIsANameRuleNotARoster fail
         - if RefuseTrackedStore stops probing git, cmd's unchanged
           TestConsentRefusesTrackedLocalToml and TestLaneGrantRefusesATrackedStore
           fail
       depends_on: []
       status: pending

  t-2  Extract mutationcfg adapter construction
       files:    internal/mutationcfg/mutationcfg.go,
                 internal/mutationcfg/toolchain.go, internal/cmd/verify.go,
                 internal/cmd/doctor.go, internal/cmd/survivor_drain.go
       covers:   c-4, c-6
       description:
         New package (locked adapter_home) importing project, mutation,
         remote, stack, verify. One roster is the single construction path:
         `var roster = []entry{{name:"stryker", tool:"npx", install:…,
         language:…, build:…}, {gremlins…}, {stryker-net…}}` in the order
         remoteAdapterOrder pins today. From verify.go move: mutationTuning →
         `Tuning` (Prefix, Target, Workers, TestCPU, FellBackFrom,
         FallbackWhy), mutationTuning.gremlins, resolveMutationTuning →
         `ResolveTuning(p, root, src Source)`, configuredAdapters →
         `Configured(p, root, skip, src Source) ([]mutation.Adapter, Tuning,
         error)` (roster filtered by p.Mutation.Adapters), dockerPrefix →
         `DockerPrefix`, profileCacheVars → `CacheVars`, measuredOnOf →
         `MeasuredOn`. `Source` is the read seam cmd fills from local.toml
         and the remote pool: `{Grants func() ([]*remote.Target, error);
         Tuning func() (workers, testCPU int, err error); Select
         func([]*remote.Target) (*remote.Target, Selection, error)}` with
         `Selection{Cores int; Fallback bool; Why string}`; cmd's
         `localSource(root)` wraps readRemoteGrants, readMutationTuning and
         selectRemoteTarget unchanged. From doctor.go move remoteAdapterTools,
         remoteAdapterOrder, mutationToolInstall, mutationToolLanguage and
         remoteMutationTools → `Tools(p) ([]string, map[string]string)`
         (derived from the same roster + allowlist filter); the probe half of
         checkMutationToolchain → `Missing(p, lookPath func(string) (string,
         error)) []Gap{Tool, Adapter, Language, Install}`; `var LookPath =
         exec.LookPath`. doctor.go keeps `var execLookPath =
         mutationcfg.LookPath` (tests keep overriding the cmd var) and
         `checkMutationToolchain` prints from Missing(p, execLookPath) until
         t-5 moves the rendering; remoteProbeTools calls mutationcfg.Tools.
         verify.go: `configuredAdaptersFn = func(p, root, skip) {return
         mutationcfg.Configured(p, root, skip, localSource(root))}`;
         survivor_drain.go calls mutationcfg.ResolveTuning(p, root,
         localSource(root)).
         Tests: dockerPrefix/profileCacheVars/measuredOnOf/configuredAdapters
         direct-call tests in verify_test.go and verify_scratch_test.go, and
         remoteMutationTools/mutationToolLanguage direct-call tests in
         doctor_lane_toolchain_test.go move to internal/mutationcfg; the
         wiring tests (mutation_remote_wiring_test.go, remote_bootstrap_test.go,
         remote_preflight_wiring_test.go, doctor_remote_test.go) stay in cmd.
       test_contract:
         - if Tools() and Configured() ever filter from different tables, new
           mutationcfg.TestToolsAndAdaptersShareTheRoster fails: for
           allowlists {}, {gremlins}, {stryker, stryker-net}, {stryker-net,
           gremlins} the tool sequence from Tools(p) must equal the roster tool
           of each adapter Configured(...) returns, in order
         - if DockerPrefix stops cutting the compose prefix at the first runner
           token, the moved TestDockerPrefix cases ("docker compose exec app
           pnpm test" → "docker compose exec app") fail
         - if Configured stops threading the selected remote target into
           Gremlins.Remote, cmd's unchanged mutation_remote_wiring_test fails
         - if doctor stops probing through cmd's execLookPath seam, cmd's
           unchanged doctor_lane_toolchain_test "gremlins not installed"
           case fails
       depends_on: []
       status: pending

  t-3  Extract deferred ledger + phase.Done helpers
       files:    internal/deferred/deferred.go, internal/phase/done.go,
                 internal/cmd/deferred.go, internal/cmd/phasedone.go,
                 internal/cmd/issue.go
       covers:   c-2, c-6
       description:
         internal/deferred (imports milestone, phase): deferredEntry →
         `Entry`, collectDeferred → `Collect`, flattenDeferred → `flatten`,
         deferredStore → `Store`, resolveDeferredSource → `ResolveSource`,
         deferredIndex → `Index`, filterDeferred → `Filter`,
         repointDeferredTarget → `RepointTarget`, plus from issue.go
         ensureDeferredIDs → `EnsureIDs`, newDeferredID → `newID`,
         legacyDeferredBacklogKey → `LegacyBacklogKey`. cmd/deferred.go keeps
         Deferred() and its five subcommands calling the package.
         phasedone.go moves whole to internal/phase/done.go as
         `phase.Done(root, slug)`, `phase.IsDone`, `phase.DirExists`
         (phase gains an import of changes; changes imports only pathfence,
         no cycle). Callers doctor.go, issue_reap.go, issue_reap_discover.go,
         milestone_progress.go, phase.go, status.go take the qualified names.
         Tests: deferred_store_test.go's direct-call tests move to
         internal/deferred; a new internal/phase/done_test.go covers
         Done/DirExists.
       test_contract:
         - if EnsureIDs mints a colliding id or forgets to persist a newly
           minted one, the moved deferred.TestEnsureIDs… cases fail (two
           specs with id-less entries → distinct ids, both readable from
           spec.toml on a second Collect)
         - if phase.Done returns true for a scaffolded phase whose changes.json
           is still active, new phase.TestDoneRequiresTerminalStatus fails;
           if it returns true for a missing dir, TestDoneNeedsTheDir fails
         - if `dross deferred route` stops rewriting the target, cmd's
           unchanged deferred route e2e test fails
       depends_on: []
       status: pending

Wave 2

  t-4  Extract boardsync core: backlog/phase/milestone/task sync
       files:    internal/boardsync/boardsync.go, internal/boardsync/backlog.go,
                 internal/boardsync/phase.go, internal/boardsync/task.go,
                 internal/cmd/issue.go, internal/cmd/issue_task.go,
                 internal/cmd/task_lifecycle.go
       covers:   c-2, c-6
       description:
         boardsync.go: boardCtx → `Ctx{Client forge.BoardClient; Board
         *board.Board; Proj *project.Project; Root, BoardPath string}`,
         boardConfig → `Config`, wrapBoard → `Wrap`, the label helpers
         (statusLabel, phaseLabel, taskLabel, deferredLabel, targetLabel,
         deferredBacklogKey) and the status/label constants, exported.
         backlog.go: backlogItem, resolveBacklogVersion, adoptLegacyBacklogKey,
         syncBacklog → `SyncBacklog`, reconcileBacklog, backlogVerdictFor,
         boardIssueIsDone, deferredBacklogItem, lookupPhaseIssue,
         resolveBacklogIssue, pushBacklogItems. phase.go: checkMilestoneClosable,
         ensureMilestoneLink, milestoneBody, hasMarker, resolvePhaseIssue,
         syncPhase → `SyncPhase`, closeBoardIssue, verifyClosed,
         derivePhaseStatus, renderPhaseBody. task.go: task_lifecycle.go whole
         (taskLifecycle, statusTaskComplete, boardStatusToPlan,
         lifecycleForPlanStatus, planStatusForLifecycle) + from issue_task.go
         taskCloseError, syncTasks → `SyncTasks`, runWarnings, syncOneTask,
         resolveTaskIssue, renderTaskBody, setBoardState. Output stays on
         fmt.Printf to stdout (cmd's Print/Printf are fmt wrappers, and cmd
         tests capture os.Stdout), so bytes are unchanged.
         cmd keeps in issue.go: Issue() and every issue* cobra constructor,
         openBoard (loadProject + FindRoot + readAllowHosts → boardsync.Ctx),
         collectInbound/pullEnvelope/emitPullEnvelope/reportBoardFailure
         until t-6. issue_task.go keeps issueTaskSync. task_lifecycle.go is
         deleted (milestoneStatusComplete stays in milestone_finalize_state.go).
         Tests: board_lifecycle_divergence_test.go moves to internal/boardsync
         with its derivePhaseStatus path re-pointed to
         internal/boardsync/phase.go; toolfence_composer_test.go moves to
         internal/boardsync with composerRoots = {".", "../ship", "../cmd"}
         (it CALLS renderPhaseBody/milestoneBody/renderTaskBody); direct-call
         tests in hostpolicy_test.go / hostile_config_test.go (boardConfig)
         move; every httptest-fake e2e test (issue_test.go,
         issue_backlog_*_test.go, issue_close_truth_test.go, …) stays in cmd.
       test_contract:
         - if derivePhaseStatus grows a return value no prompt emits, the moved
           TestEmittedStatusesAreTheLifecycleSet fails
         - if a body composer starts rendering a Recorded toolfence field, the
           moved TestNoBodySinkReadsARecordedField fails; if fewer than 8
           sinks resolve across boardsync+ship+cmd the vacuity floor fails
           (proves the composers moved intact)
         - if Config derives the host allowlist from the synthetic board URL
           instead of [remote].url, moved TestBoardConfig… in hostpolicy_test
           fails
         - if SyncBacklog stops closing a board item whose deferred entry was
           dismissed, cmd's unchanged issue_backlog_close_test fails
       depends_on: [t-3]
       status: pending

  t-5  Extract diagnose checks; Doctor composes and prints
       files:    internal/diagnose/diagnose.go, internal/diagnose/trust.go,
                 internal/diagnose/toolchain.go, internal/argfence/refguard.go,
                 internal/cmd/doctor.go, internal/cmd/refguard.go
       covers:   c-3, c-4, c-6
       description:
         diagnose.go: doctorLine → `Line{Level, Text}` with `OK/Warn/Issue`;
         redProofChecks + redProofPinLines + sameCommitSHA →
         `RedProof(pins []RedProofPin, readErr error) ([]Line, bool)` where
         cmd pre-classifies each pin (`RedProofPin{Phase, SHA, Doc string;
         Reach Reach; Why string; ReachErr error; DocSHA string; DocErr error;
         RepointHint string}`) using its git-backed classifyReachability,
         redProofDocSHA and redProofRepointHint, which stay in cmd;
         duplicateRoadmapSlugs → `RoadmapDuplicates(root) []DuplicateSlug`;
         remoteCombinationWarnings → `RemoteCombination`,
         boardCombinationWarnings → `BoardCombination`. trust.go:
         checkConfigTrust → `ConfigTrust(root, repoDir string, p
         *project.Project, in TrustInputs) ([]Section, int)` with
         `TrustInputs{AllowHosts []string; AllowHostsErr error; GitVersion
         func() (string, error)}` and `Section{Heading string; Lines []Line}`
         for Branch names / API host / Machine-local store / git version /
         Exec consent (via consent.Check); reportLaneConsent → `LaneConsent(root,
         repoDir, p) ([]Line, int)`. toolchain.go: the printing half of
         checkMutationToolchain → `MutationToolchain(p, lookPath) []Line`
         over mutationcfg.Missing. validateGitRef moves to
         internal/argfence/refguard.go as `argfence.ValidateGitRef`; cmd's
         refguard.go shrinks to the one-line wrapper so its 12 callers stay.
         doctor.go: Doctor() gathers inputs (readAllowHosts, gitVersionOutput
         now `func() (string, error) { return gitTrim(".", "--version") }`,
         discoverRedProofPins + classification), calls the checks, and prints
         through one `printLines(prefix-by-level)` helper with the exact
         current text; phaseCommitsOnMain's exec.Command moves onto
         gitTrim/gitOut so doctor.go drops its os/exec import (execLookPath
         is already mutationcfg.LookPath after t-2). reportExecGatedSurface
         stays in cmd (it prints execGatedCommands).
         Tests: doctor_test.go's direct-call tests (remoteCombinationWarnings,
         boardCombinationWarnings, sameCommitSHA, duplicateRoadmapSlugs,
         gitVersionAtLeast) move to internal/diagnose; refguard_test.go moves
         to argfence; every runDoctorEnum/captureStdout e2e test stays in cmd.
       test_contract:
         - if RedProof stops treating a doc SHA that disagrees with the record
           as an Issue, new diagnose.TestRedProofDisagreeingDocIsAnIssue
           fails; an unreachable pin must yield an Issue line carrying the
           RepointHint text, a reachable+matching pin exactly one OK line
         - if any printed doctor line changes by a byte, cmd's unchanged
           doctor_test golden assertions (TestDoctorWarnsJiraEpicCombination,
           TestDoctorReportsStaleConsent, TestDoctorReportsEachLaneGrant,
           TestDoctorCountsEachRefusedLane) fail
         - if LaneConsent's stale count stops feeding the exit code, cmd's
           unchanged TestDoctorStaleLaneMovesTheExitCode fails
         - if argfence.ValidateGitRef accepts a leading-dash ref, the moved
           refguard tests fail and cmd's unchanged doctor branch-name test
           ("git reads a leading dash as an option") fails
         - if doctor.go re-imports os/exec, t-7's ratchet fails (doctor.go is
           not in the baseline)
       depends_on: [t-1, t-2]
       status: pending

Wave 3

  t-6  Extract boardsync reap and task pull
       files:    internal/boardsync/reap.go, internal/boardsync/reap_apply.go,
                 internal/boardsync/pull.go, internal/cmd/issue_reap.go,
                 internal/cmd/issue_reap_apply.go,
                 internal/cmd/issue_reap_discover.go,
                 internal/cmd/issue_reap_undo.go, internal/cmd/issue_task_pull.go
       covers:   c-2, c-6
       description:
         reap.go: issue_reap.go + issue_reap_discover.go whole (reapVerdict,
         reapCard, reapPlan, reapLane, reapLaneFor, reapLaneNames, candidate,
         classifyReap → `ClassifyReap`, buildReapPlan, resolveReapLanes,
         phaseRecordVerdict, the classify*Mirrors set, roadmapSlugs,
         reapBacklogVerdict, slugVerdict, orphanKind, orphanIdentity,
         discoverReap, orphanVerdict, hasLabel). reap_apply.go:
         issue_reap_apply.go + issue_reap_undo.go whole (reapInventory →
         `Inventory`, reapFailure, applyReap → `Apply`, relabelReapedCard,
         priorStateOf, dropBacklogLink, appendReapRun, reapLogPathFor,
         undoReap → `Undo`, restoreDroppedLink). pull.go: from
         issue_task_pull.go taskMoveKind, taskMoveVerdict, taskPull →
         `TaskPull(ctx, phaseID string, plan *phase.Plan, planPath string,
         apply bool)`, providerHasWorkflowState, collectTaskMoves,
         classifyTaskMove, lifecycleFromLabels, reportTaskMoves; from issue.go
         collectInbound → `CollectInbound`, pullEnvelope, emitPullEnvelope →
         `EmitPullEnvelope`, reportBoardFailure → `ReportBoardFailure`.
         cmd keeps issue_reap_cmd.go (issueReap, printReapPlan,
         boardNamespaceNames, validateReapNamespaces — the last two now read
         boardsync.ReapLaneNames) and the issueTaskPull constructor, which
         resolves the phase (resolveTaskPhaseID, loadPhasePlanAndSpec) and
         hands plan + path to boardsync.TaskPull. The four moved cmd files are
         deleted.
         Tests: issue_reap_classify_test.go (readOnlyYT fake, direct calls),
         issue_reap_discover_test.go, issue_reap_apply_test.go and
         issue_reap_undo_test.go's direct-call tests, issue_task_pull_test.go's
         Ctx-constructing tests, and TestEveryBoardNamespaceHasAReapPath /
         TestReapTerminalMatchesTheForwardTerminal / TestEveryReapTerminalIsMapped
         (already in boardsync after t-4) move; issue_reap_cmd_test.go and
         the cobra halves stay in cmd.
       test_contract:
         - if a phase mirror whose phases/<slug> dir is gone stops classifying
           as reapable, the moved TestClassifyReap… phase-lane case fails
         - if classifyTaskMove maps a board issue closed under a non-terminal
           label to task-complete, the moved issue_task_pull unit case fails
         - if a board namespace gains a label family with no reap lane, the
           moved TestEveryBoardNamespaceHasAReapPath fails
         - if `dross issue reap --apply` stops writing the reaplog run or
           relabelling cards, cmd's unchanged issue_reap_cmd_test fails
       depends_on: [t-4]
       status: pending

  t-7  Add import-direction boundary test with exec ratchet
       files:    internal/cmd/boundary_test.go
       covers:   c-5
       description:
         A go/parser walk (imports only) over every directory under internal/
         that holds a non-test .go file, building {package → imports}. A
         `checkBoundary(pkgs, baseline)` function returns findings, and the
         test runs it over the live tree AND over synthetic package maps to
         prove each rule fires. Rules (locked proof_shape): (1)
         github.com/spf13/cobra appears only in internal/cmd; (2) none of
         internal/consent, internal/boardsync, internal/diagnose,
         internal/mutationcfg imports internal/cmd (nor does any other
         package); (3) internal/cmd imports all four; (4) a non-test file in
         internal/cmd importing os/exec, net/http or go/ast must be in the
         baseline literal, and every baseline entry must still import the
         package it is listed for — a stale entry is a failure that says
         "remove it", which is what makes the list shrink-only. Baseline is
         derived at task time by grep; expected: os/exec → cleantree.go,
         init.go, lane_install.go, milestone_stale.go, pause.go, phase.go,
         redproof_replay.go, run.go, ship_recover.go, stack.go, statusline.go,
         survivor_drain.go, techdebt.go, test.go, update.go, verify.go
         (detach self-spawn + exec.ExitError), worktree_files.go; net/http →
         update.go; go/ast → none. doctor.go must not appear. Vacuity floor:
         the walk must find ≥ 30 packages and internal/cmd must be among them.
       test_contract:
         - if any non-cmd package imports cobra, the live-tree run fails and
           the synthetic case "cobra in internal/x" proves the rule fires
         - if an extracted package imports internal/cmd, or cmd drops one of
           the four imports, the live run fails; synthetic cases cover both
         - if a cmd file outside the baseline imports os/exec (or net/http,
           go/ast), the ratchet fails naming the file; if a baseline entry no
           longer imports its package, the test fails asking for its removal
         - if the walk returns fewer than 30 packages or misses internal/cmd,
           the floor fails (the test cannot pass by scanning nothing)
       depends_on: [t-1, t-2, t-4, t-5]
       status: pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3, t-4, t-6 |
| c-3 | t-5 |
| c-4 | t-2, t-5 (doctor consumes mutationcfg.Tools/Missing) |
| c-5 | t-7 |
| c-6 | t-1, t-2, t-3, t-4, t-5, t-6 (every extraction keeps the cmd e2e tests as its unchanged guard; unit tests move) |

## Judgment calls

- **Consent persistence = grant-key overlay on local.toml, not a store interface and not moving localStore.** Chose: consent decodes the five trusted_* keys typed, writes them by map overlay; localStore embeds consent.Grants. Rejected: a Store interface (every call site and every moved test gains an injected store; tests stop exercising the real file), and moving localStore wholesale (that is the deferred fifth domain, and it would drag detachedRun/remoteCandidate into a package named consent). Cost: two encoders touch the file with different key order; the deferred local.go move unifies them.
- **requireExecConsent stays in cmd under its own name.** Rejected exporting it as consent.RequireExec: execconsent_audit_test resolves gates by the literal name `requireExecConsent` or a `Consented` suffix, and c-1 says cmd keeps the gate call. Every exported consent check keeps its `Consented` suffix for the same reason.
- **mutationcfg reads nothing itself; cmd hands it a Source.** Chose: Grants/Tuning/Select closures over readRemoteGrants, readMutationTuning, selectRemoteTarget. Rejected: moving remote_pool.go/remote_preflight.go into mutationcfg — the pool is shared with `dross test` lanes and bootstrap, and it prints through cmd. This keeps the locked import set (project, mutation, remote) plus stack for cache vars.
- **Toolchain probe lives in mutationcfg (c-4), its rendering in diagnose (c-3).** One roster feeds Configured, Tools and Missing, which is the "single construction path"; the seam var `execLookPath` stays in cmd initialised from mutationcfg.LookPath so doctor tests override nothing new.
- **Red-proof check takes pre-classified pins.** Rejected moving redproof.go: classifyReachability is git plumbing on cmd's gitTrim/gitNoOut/gitRefArgs and discoverRedProofPins sits on the phase red-proof command set. The verdict logic (which combination is issue/warn/ok, the exact text) moves; the gathering stays as "composition" in Doctor().
- **Only the six named check families move out of Doctor().** The remaining inline remote/board/state/milestone blocks in the 500-line RunE are not named by c-3 and stay. Not moving them is the whole point of this lens.
- **`internal/deferred` is the one unnamed package.** Chose it over exporting Collect/EnsureIDs from boardsync (cmd's `dross deferred` would then import a board package for a roadmap ledger) and over stuffing it into `phase` (it spans milestones). phasedone.go goes to `phase` because it is a phase predicate with no board meaning.
- **verify.go stays in the exec baseline.** Its residual os/exec is the detach self-spawn and exec.ExitError classification, neither of which is adapter construction. Listing it honestly beats moving a process runner into a config package to make a number smaller. doctor.go leaves the baseline (git via gitTrim/gitOut, LookPath via mutationcfg).
- **Ratchet scans non-test files.** execconsent_audit_test.go and the enum tests import go/ast by design; the milestone's target is domain logic landing in cmd, which lives in non-test files.
- **Boundary test lives in internal/cmd.** Rejected a test-only `internal/boundary` directory (a package with no non-test files trips `go build` when targeted directly). It grows the cmd test binary by one file.
- **Tests split per function, not per file.** Rule for the executor: a test moves iff it calls a moved identifier directly and does not build a cobra command; httptest-fake board tests and runCmdCapturing/captureStdout tests all stay. Qualifier edits at surviving cmd call sites are not assertion edits.
