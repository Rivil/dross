# Synthesis — cmd-package-decomposition

Judge read all three drafts cold and settled contested facts against the tree
before scoring (see "Facts settled" at the end).

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk (10 tasks / 4 waves) | All six; c-6 gets its own task (t-10) but c-1 is reached by moving the deferred local.go persistence (t-1), which the spec explicitly defers. | Sharpest failure-mode framing (clobber guard, fail-closed, notice ordering), but three contracts rest on wrong facts: subprocargs audit does not name `validateGitRef`; "≥300 files in internal/cmd" fails on a non-test walk (114); mutationcfg importing localstore breaks the locked import set. | Best split (phase/ref substrate, pool, consent, mutationcfg, board, reap, doctor, boundary, c-6) but t-3 (remote pool → internal/remote) is outside every criterion and re-points 6 test stubs. | Dependency depth is right; t-7 and t-8 both edit doctor.go in wave 3 (reap inventory vs check extraction) with no ordering. |
| mvp (7 tasks / 3 waves) | All six; c-6 has no dedicated proof beyond "cmd tests unchanged". Baseline list (17 files) matches the tree exactly. | Precise and named (TestToolsAndAdaptersShareTheRoster, TestSaveGrantsPreservesForeignKeys, composerRoots fix in toolfence_composer_test), every cmd seam var kept so zero stub edits. Map-overlay consent writer is the one weak contract (two encoders on one file). | t-1 (consent, 13 call-site files) and t-4 (issue.go 1.6k + task + lifecycle) are oversized; t-3 correctly carves internal/deferred. | Cleanest graph; wave-1 t-1 and t-2 both touch doctor.go (disjoint regions). Floor ≥30 is the only floor that is true today (39). |
| verification (11 tasks / 3 waves) | All six; strongest c-6 (goldens before moves, tests_before.txt, no-test-lost, final proof task). Baseline 17 correct. Only draft that notices checkConfigTrust's `ignoresPath` input. | Most thorough named contracts (PinLines matrix, ConfigTrust ladder, close-truth, readOnlyYT). Costs: re-points `execLookPath`→`diag.LookPath`, `gitVersionOutput`→`diag.GitVersion`, `.level`→`.Level` in cmd tests — more c-6 churn than needed. Floor ≥40 is false on today's tree (39). | Good: board in three tasks, diag in two. | Weakest: t-2, t-4, t-5 all edit doctor.go in wave 1; t-10 depends on t-7 but t-11 depends on t-10 in the same wave. Moving phaseCommitsOnMain's spawn into diag makes the "pure checks" package an os/exec importer. |

**Skeleton: mvp.** Fewest factual errors, seams that keep every cmd seam var
(`remoteProbeFn`, `execLookPath`, `gitVersionOutput`, `configuredAdaptersFn`)
in place so no test stub moves, the correct 17-file baseline, and the only
draft whose vacuity floor is true on the current tree. Its gaps — no c-6 proof
task, an oversized board task, a fragile consent writer — are exactly what the
other two supply.

## Merged plan

Phase cmd-package-decomposition — 10 tasks across 4 waves

Package homes: `internal/consent` (c-1), `internal/boardsync` (c-2),
`internal/diag` (c-3), `internal/mutationcfg` (c-4, locked), plus the enabling
`internal/deferred` and `phase.Done`/`argfence.ValidateGitRef` relocations.

Wave 1 — independent; t-3 and t-4 both edit doctor.go in disjoint regions
(remoteMutationTools/execLookPath vs the CheckConsent/LaneConsented qualifiers);
if the wave is executed serially, run t-4 before t-3.

  t-1  Pin CLI surface, doctor transcript and the pre-move test-name set   [verification]
       wave:     1
       files:    internal/cmd/cli_surface_test.go (new),
                 internal/cmd/testdata/cli_surface/{trust,issue,doctor,verify}.txt (new),
                 internal/cmd/testdata/cli_surface/doctor_run.txt (new),
                 internal/cmd/testdata/cli_surface/tests_before.txt (new)
       description:
                 Walk Trust()/Issue()/Doctor()/Verify() cobra trees (Use, Aliases, Short,
                 every flag: name, shorthand, default, usage, declared order) into a text
                 rendering checked in as goldens. Run `dross doctor` over consentFixture
                 (gitVersionOutput stubbed, no [remote], no [board]) and check in the full
                 stdout + exit code. Record every `func Test*` name in internal/cmd
                 (2544 today) into tests_before.txt for t-9's no-test-lost check. Goldens
                 are written only under DROSS_UPDATE_GOLDEN=1; a run that finds no golden
                 fails rather than passing vacuously.
       covers:   c-6
       test_contract:
         - Renaming `--lane-install`, dropping `issue reap undo`, or changing
           `verify --detach`'s default fails TestCLISurfacePinned naming the command and
           the diff line.
         - A changed glyph, section order, blank line or exit code in `dross doctor` over
           the fixture fails TestDoctorRunGolden with a unified diff.
         - Missing golden + no DROSS_UPDATE_GOLDEN → fail, never a silent pass.
       depends_on: []

  t-2  Extract deferred ledger to internal/deferred; phase doneness to internal/phase   [mvp+verification+risk]
       wave:     1
       files:    internal/deferred/deferred.go (new), internal/deferred/deferred_test.go (new),
                 internal/phase/done.go (new), internal/phase/done_test.go (new),
                 internal/cmd/deferred.go, internal/cmd/deferred_store.go,
                 internal/cmd/phasedone.go, internal/cmd/issue.go,
                 internal/cmd/deferred_store_test.go
       description:
                 internal/deferred (imports milestone, phase): deferredEntry → Entry,
                 collectDeferred → Collect, flattenDeferred → flatten, deferredStorePath →
                 StorePath, deferredStore → Store, loadDeferredStore → LoadStore,
                 projectStoreSlug, resolveDeferredSource → ResolveSource, deferredIndex →
                 Index, filterDeferred → Filter, repointDeferredTarget → RepointTarget; from
                 issue.go ensureDeferredIDs → EnsureIDs, newDeferredID → newID,
                 legacyDeferredBacklogKey → LegacyBacklogKey. cmd/deferred.go keeps
                 Deferred() and its verbs calling the package; `type deferredEntry =
                 deferred.Entry` alias keeps cmd literals compiling. phasedone.go moves whole
                 to internal/phase/done.go as phase.Done / phase.IsDone / phase.DirExists
                 (phase gains an import of changes; changes imports nothing internal, no
                 cycle). Callers (doctor.go, issue_reap*.go, milestone_progress.go, phase.go,
                 status.go, deferred_add.go, verify.go) take the qualified names — see
                 disagreement D9 for the wrapper alternative.
                 Tests: deferred_store_test.go's store-addressing tests move to
                 internal/deferred; new internal/phase/done_test.go.
       covers:   c-2 (enabling), c-3 (enabling), c-6
       test_contract:
         - deferred.Store(root, "_project") returns .dross/deferred.toml and
           Store(root, "no-such-phase") errors naming "_project" as the only non-phase
           source (moved TestDeferredRouteAddressesStore /
           TestDeferredUnrouteDismissAddressStore).   [verification]
         - EnsureIDs over a spec with two id-less items writes two distinct 16-hex ids and
           a second call rewrites nothing (bytes unchanged); both ids readable on a second
           Collect.   [mvp+verification]
         - Collect skips a `phases/_project` directory (moved test).   [verification]
         - phase.Done: a phase dir whose changes.json status is complete/shipped is Done;
           an unscaffolded slug is never Done; a verify.toml "pass" with no completion
           record is NOT Done (TestDoneRequiresTerminalStatus, TestDoneNeedsTheDir).   [risk+mvp]
         - cmd unchanged: deferred_board_test.go, deferred_routed_sync_test.go, the
           `dross deferred route` e2e test, milestone_progress_test / status_test /
           phase_reconcile_test pass with no assertion edits.   [all]
         - `go list -deps ./internal/phase ./internal/deferred` contains no
           internal/cmd (asserted by the build).   [risk]
       depends_on: []

  t-3  Extract adapter construction to internal/mutationcfg   [mvp+verification+risk]
       wave:     1
       files:    internal/mutationcfg/mutationcfg.go (new), internal/mutationcfg/toolchain.go (new),
                 internal/mutationcfg/mutationcfg_test.go (new), internal/mutationcfg/toolchain_test.go (new),
                 internal/cmd/verify.go, internal/cmd/doctor.go, internal/cmd/survivor_drain.go,
                 internal/cmd/verify_test.go, internal/cmd/verify_scratch_test.go,
                 internal/cmd/doctor_lane_toolchain_test.go
       description:
                 New package (locked adapter_home) importing project, mutation, remote,
                 stack, verify — never cmd. One roster is the single construction path:
                 `[]entry{{name:"stryker", tool:"npx", install, language}, {gremlins}, {stryker-net}}`
                 in the order remoteAdapterOrder pins today; `Selected(p) []string` is the
                 one allowlist rule (empty = all) that Configured, Tools and Missing all
                 derive from. From verify.go: mutationTuning → Tuning (+ Tuning.Gremlins),
                 resolveMutationTuning → ResolveTuning(p, root, src Source),
                 configuredAdapters → Configured(p, root, skip, src Source), dockerPrefix →
                 DockerPrefix, profileCacheVars → CacheVars, measuredOnOf → MeasuredOn.
                 `Source` is the already-read seam cmd fills: {Grants func() ([]*remote.Target,
                 error); Tuning func() (workers, testCPU int, err error); Select
                 func([]*remote.Target) (*remote.Target, Selection, error)} with
                 Selection{Cores int; Fallback bool; Why string}; cmd's localSource(root)
                 wraps readRemoteGrants, readMutationTuning and selectRemoteTarget unchanged
                 (the pool walk and its notices stay in cmd — D2). From doctor.go:
                 remoteAdapterTools, remoteAdapterOrder, mutationToolInstall,
                 mutationToolLanguage, remoteMutationTools → Tools(p); the LookPath loop of
                 checkMutationToolchain → Missing(p, lookPath) []Gap{Tool, Adapter,
                 Language, Install}; `var LookPath = exec.LookPath`. cmd keeps
                 `var execLookPath = mutationcfg.LookPath` (doctor_multilang_test's stub is
                 untouched) and `configuredAdaptersFn = func(p, root, skip) {return
                 mutationcfg.Configured(p, root, skip, localSource(root))}` (five verify
                 test stubs untouched); `type mutationTuning = mutationcfg.Tuning` keeps
                 verify_detach_test literals compiling. survivor_drain.go calls
                 ResolveTuning(...).Gremlins.
                 Tests: dockerPrefix/profileCacheVars/measuredOnOf/configuredAdapters
                 direct-call tests in verify_test.go and verify_scratch_test.go, and
                 remoteMutationTools/mutationToolLanguage direct-call tests in
                 doctor_lane_toolchain_test.go move; wiring tests
                 (mutation_remote_wiring_test.go, remote_bootstrap_test.go,
                 remote_preflight_wiring_test.go, doctor_remote_test.go) stay in cmd.
       covers:   c-4, c-6
       test_contract:
         - TestToolsAndAdaptersShareTheRoster: for allowlists {}, {gremlins},
           {stryker, stryker-net}, {stryker-net, gremlins} the tool sequence from Tools(p)
           equals the roster tool of each adapter Configured returns, in order; a second
           allowlist loop that treats empty differently fails it naming the side.   [mvp+verification+risk]
         - TestResolveTuningFallsBackWhenUnreached: Select reporting Fallback with one
           Target yields Prefix==DockerPrefix(p), FellBackFrom==host, Target==nil; Select
           returning an error yields the "remote mutation host %s is not usable" error and
           no adapters; a target with cores 8 yields Target.Cores==8 and Prefix=="".   [verification]
         - Drain and verify build the same Gremlins: constructing via Configured and via
           Tuning.Gremlins with identical inputs yields deep-equal *mutation.Gremlins
           (Workers, TestCPU, Prefix, Remote, CacheVars).   [risk]
         - TestDockerPrefixRefusesLookalikeBinary: "dockerevil compose exec app go test" →
           default "docker compose exec app"; "docker compose exec node node test.js" →
           "docker compose exec node" (moved, field-based).   [risk+verification]
         - cmd unchanged: mutation_remote_wiring_test (Gremlins.Remote threaded),
           doctor_multilang_test "npx is not installed — the stryker adapter needs it …"
           byte-identical through the untouched execLookPath stub, trust_test's
           refuseAdapters seam, verify_*_test configuredAdaptersFn stubs.   [all]
       depends_on: []

  t-4  Extract consent to internal/consent behind a Store seam   [mvp+verification+risk; seam per verification]
       wave:     1
       files:    internal/consent/consent.go (new), internal/consent/lane.go (new),
                 internal/consent/store.go (new), internal/consent/consent_test.go (new),
                 internal/consent/lane_test.go (new), internal/consent/store_test.go (new),
                 internal/cmd/trust.go, internal/cmd/local.go, internal/cmd/remote_grant.go,
                 internal/cmd/{doctor,lane_install,lane_preview,lane_preview_locality,
                   remote_bootstrap,redproof_replay,run,test,test_lane_install}.go (qualifier edits),
                 internal/cmd/trust_test.go, internal/cmd/trust_lane_test.go,
                 internal/cmd/trust_lane_install_test.go, internal/cmd/local_test.go
       description:
                 Move from trust.go: ConsentState (+String) → State with the five constants,
                 the Err* sentinels, Fingerprint, CheckConsent, GrantConsent,
                 ReplayConsented/GrantReplayConsent, RunConsented/GrantRunConsent,
                 fingerprintInSet/addFingerprint, the laneFrame/laneTemplateFrame/
                 laneInstallFrame consts, laneConsentLine → LaneLine, laneInstallConsentLine
                 → LaneInstallLine, LaneConsented/GrantLaneConsent/RevokeLaneConsent,
                 LaneInstallConsented/GrantLaneInstallConsent/RevokeLaneInstallConsent,
                 consentRefusal → Refusal, laneConsentRefusal → LaneRefusal,
                 laneInstallRefusal → LaneInstallRefusal. Every exported check keeps its
                 `Consented` suffix (the exec audit's isExecConsentCall matches by that
                 shape). Persistence seam (D1): `type Grants struct` holds the five
                 trusted_* toml fields; `type Store interface { Load() (*Grants, error);
                 Save(*Grants) error }`; cmd's localStore EMBEDS consent.Grants (BurntSushi
                 flattens anonymous structs) and cmd's `grantStore(root)` adapter loads the
                 full localStore, swaps the embedded Grants, saves — ONE writer of
                 local.toml, foreign keys untouched. `File`, `RelPath` and refuseTrackedLocal
                 → RefuseTrackedLocal(repoDir) move to consent (fixed argv `git -C repoDir
                 ls-files --error-unmatch -- .dross/local.toml`, //dross:exec-exempt marker
                 in the grammar the audit accepts); local.go's readAllowHosts/
                 readRemoteGrants and remote_grant.go call it. `type ConsentState =
                 consent.State` + the five Consent* consts stay as aliases in cmd.
                 trust.go keeps: Trust() and its subcommand bodies (trustLane, trustRun,
                 trustReplay, trustLaneInstall, recordedReplayLine), findLane,
                 execGatedCommands + its doc comment (execconsent_docs_test pins it by
                 path), reportExecGatedSurface's roster, and requireExecConsent — same
                 name, now FindRoot → project.Load → consent.CheckConsent(grantStore(root),
                 …) → consent.Refusal.
                 Tests: direct-call halves of trust_test.go (TestFingerprint,
                 TestGrantStoresHashNotCommand, TestConsentStates,
                 TestConsentRefusesTrackedLocalToml, TestConsentNotApplicable's store half),
                 trust_lane_test.go (TestOneLaneGoesStaleAlone, TestRenamedLaneInheritsNothing,
                 TestLaneWithNoCommandIsNotApplicable, TestRevokeLaneConsentDropsOnlyThatLane,
                 framing/NUL tests) and trust_lane_install_test.go (frame/isolation tests)
                 move over a 20-line file-backed test Store; tests that run Trust()/Doctor()/
                 `dross local set` through runCmdCapturing stay in cmd.
       covers:   c-1, c-6
       test_contract:
         - consent.Fingerprint("go test ./... ") != Fingerprint("go test ./...") — a
           normalizer added later fails the moved TestFingerprint.   [all]
         - Stale beats absent: a Store holding Fingerprint("a") answers Stale +
           ErrStaleConsent for "b" (never Absent/ErrNoConsent) and Granted for "a"; empty
           testCmd answers NotApplicable + ErrNoTestCommand.   [risk+verification]
         - Framing is unforgeable: {prepare:"a",command:"bc"} and {prepare:"ab",command:"c"}
           fingerprint differently; a no-prepare lane's line equals its bare command
           byte-for-byte (pre-phase grants stay valid); a command containing NUL is
           refused; revoking the last lane leaves no empty `trusted_lane_installs` table
           (moved TestRemoveDropsAnInstallOnlyGrant).   [risk+mvp]
         - Clobber guard — NEW cmd TestLocalStoreRoundTripsGrants: a local.toml seeded with
           quick_base, mutation_remote_host, one [[detached_run]] and every trusted_* key,
           after GrantConsent + GrantLaneConsent through grantStore(root), still decodes all
           foreign keys byte-identical and reads every grant back; a partial-struct writer
           or a drifted field tag fails on the dropped key.   [risk+mvp+verification]
         - Fail-closed: with .dross/local.toml `git add -f`'d, CheckConsent returns Refused
           and the error names "git rm --cached"; after `git rm --cached` the same store
           grants (moved TestConsentRefusesTrackedLocalToml over a real git repo);
           readRemoteGrants/readAllowHosts return the error with a nil list, never an empty
           grant.   [risk+verification]
         - Gate reach survives the move: TestEverySpawnSiteGatedOrExempt still resolves
           run.go, test.go, verify.go, lane_install.go, survivor_drain.go as GATED and
           reports one more exempt site (consent's `git ls-files`) with zero findings; the
           subprocargs audit reports no finding for it. execconsent_docs_test passes
           unchanged.   [risk+mvp+verification]
         - cmd unchanged: TestGatedCommandsRefuse, TestRefusalWritesNothing,
           TestExecGatingIsANameRuleNotARoster, TestExecGatedSetIsExplicit,
           TestTrustStaleMessage, TestTrustCheckExitCodes, TestLocalSetCannotGrantConsent,
           TestLaneGrantRefusesATrackedStore, lane_preview_json "granted"/"stale" strings —
           no assertion edits.   [all]
       depends_on: []

Wave 2

  t-5  Extract board sync core (backlog / phase / milestone) to internal/boardsync   [mvp+verification+risk]
       wave:     2
       files:    internal/boardsync/ctx.go (new), internal/boardsync/backlog.go (new),
                 internal/boardsync/phase.go (new), internal/boardsync/milestone.go (new),
                 internal/boardsync/*_test.go (moved), internal/cmd/issue.go,
                 internal/cmd/board_lifecycle_divergence_test.go, internal/cmd/toolfence_composer_test.go,
                 internal/cmd/hostpolicy_test.go, internal/cmd/hostile_config_test.go
       description:
                 ctx.go: boardCtx → Ctx{Client forge.BoardClient; Board *board.Board; Proj
                 *project.Project; Root, BoardPath string; Out io.Writer} with `ctx.out()`
                 returning Out or os.Stdout resolved AT CALL TIME (nil-safe; never a
                 package-level `= os.Stdout` initializer — captureStdout swaps os.Stdout
                 after init) — D11; boardConfig → Config, wrapBoard → Wrap, the label
                 helpers (statusLabel, phaseLabel, taskLabel, deferredLabel, targetLabel,
                 deferredBacklogKey, hasMarker) and status/label constants, exported.
                 backlog.go: backlogItem, resolveBacklogVersion, adoptLegacyBacklogKey,
                 syncBacklog → SyncBacklog, reconcileBacklog, backlogVerdictFor,
                 boardIssueIsDone, deferredBacklogItem, lookupPhaseIssue, resolveBacklogIssue,
                 pushBacklogItems (EnsureIDs from internal/deferred). phase.go:
                 resolvePhaseIssue, syncPhase → SyncPhase, closeBoardIssue, verifyClosed,
                 derivePhaseStatus, renderPhaseBody. milestone.go: checkMilestoneClosable,
                 ensureMilestoneLink, milestoneBody. boardsync imports board, forge, phase,
                 milestone, changes, deferred, project, hostallow — never cmd.
                 cmd/issue.go keeps Issue() and every issue* cobra constructor, openBoard
                 (loadProject + FindRoot + readAllowHosts → boardsync.Ctx),
                 collectInbound/pullEnvelope/emitPullEnvelope/reportBoardFailure (until t-8),
                 dismiss/link/list/quick; `type boardCtx = boardsync.Ctx`.
                 Tests: board_lifecycle_divergence_test.go moves with its derivePhaseStatus
                 path re-pointed to internal/boardsync/phase.go; toolfence_composer_test.go
                 moves with composerRoots = {".", "../ship", "../cmd"} (it CALLS
                 renderPhaseBody/milestoneBody/renderTaskBody); boardConfig direct-call tests
                 in hostpolicy_test.go / hostile_config_test.go move; every httptest-fake
                 e2e test (issue_test.go, issue_backlog_*_test.go, issue_close_truth_test.go,
                 issue_milestone_close_test.go, …) stays in cmd.
       covers:   c-2 (part 1), c-6
       test_contract:
         - TestSyncPhaseCreatesThenUpdates (moved): first SyncPhase against a fake client
           creates one issue with labels [dross, dross/status:planned, dross/phase:<id>];
           the second call updates the same key and creates nothing.   [verification]
         - Close truth: verifyClosed's re-read still fails with "wrote the close for status …
           but the issue still reads unresolved" when the fake accepts the close but reports
           the issue open (issue_close_truth_test.go, unchanged in cmd).   [risk+verification]
         - If derivePhaseStatus grows a return value no prompt emits, moved
           TestEmittedStatusesAreTheLifecycleSet fails; if a body composer renders a
           Recorded toolfence field, moved TestNoBodySinkReadsARecordedField fails; fewer
           than 8 sinks across boardsync+ship+cmd trips the vacuity floor (proves the
           composers moved intact).   [mvp]
         - Config derives the host allowlist from [remote].url, not the board URL (moved
           TestBoardConfigDerivesFromRemoteURL); a tracked local.toml blocks Config (moved
           TestTrackedLocalTomlBlocksBoardConfig).   [mvp+verification]
         - Output capture survives the writer seam: a cmd e2e test capturing `dross issue
           phase sync` via captureStdout still sees "phase <id> -> board <key> (<state>)".   [risk]
         - cmd unchanged: issue_test.go's 48 cobra-driven tests, issue_backlog_close_test
           (dismissed deferred entry closes its board item), issue_verb_shape_test.go, and
           t-1's issue.txt golden — no assertion edits.   [all]
       depends_on: [t-2]

  t-6  Extract diagnostic checks to internal/diag; Doctor() composes and prints   [mvp+risk+verification]
       wave:     2
       files:    internal/diag/diag.go (new), internal/diag/redproof.go (new),
                 internal/diag/roadmap.go (new), internal/diag/combinations.go (new),
                 internal/diag/trust.go (new), internal/diag/toolchain.go (new),
                 internal/diag/*_test.go (new/moved), internal/argfence/refguard.go (new),
                 internal/argfence/refguard_test.go (moved), internal/cmd/doctor.go,
                 internal/cmd/refguard.go, internal/cmd/doctor_test.go,
                 internal/cmd/redproof_test.go, internal/cmd/redproof_repoint_test.go,
                 internal/cmd/redproof_set_test.go, internal/cmd/redproof_repoint_cmd_test.go
       description:
                 diag.go: doctorLine → Line{Level, Text} with OK/Warn/Issue (+ Note for
                 verbatim continuation lines), Section{Heading; Lines}, Issues(sections).
                 redproof.go: redProofChecks + redProofPinLines + sameCommitSHA →
                 RedProof(pins []RedProofPin, readErr error) ([]Line, bool) where cmd
                 pre-classifies each pin (RedProofPin{Phase, SHA, Doc; Reach; Why; ReachErr;
                 DocSHA; DocErr; RepointHint}) using its git-backed classifyReachability,
                 redProofDocSHA and redProofRepointHint, which stay in cmd (D5).
                 roadmap.go: duplicateRoadmapSlugs → RoadmapDuplicates. combinations.go:
                 remoteCombinationWarnings → RemoteCombination, boardCombinationWarnings →
                 BoardCombination, sortedStateMapKeys, gitVersionAtLeast. trust.go:
                 checkConfigTrust → ConfigTrust(root, repoDir, p, in TrustInputs)
                 ([]Section, int) with TrustInputs{AllowHosts []string; AllowHostsErr error;
                 GitVersion func() (string, error); Grants consent.Store; IgnoresPath
                 func(body, target string) bool; LocalIgnorePath string} — the
                 `ignoresPath` input is real (gitignore.go:127) and only verification saw
                 it; reportLaneConsent → LaneConsent(root, repoDir, p, grants) ([]Line, int)
                 (consent.LaneConsented). toolchain.go: the printing half of
                 checkMutationToolchain → MutationToolchain(p, lookPath) []Line over
                 mutationcfg.Missing. Branch-name validation: validateGitRef moves to
                 argfence.ValidateGitRef and cmd/refguard.go shrinks to a one-line
                 same-named wrapper so its 12 callers stay (D9).
                 doctor.go: Doctor() gathers inputs (readAllowHosts, gitVersionOutput — the
                 seam var STAYS in cmd, now `func() (string, error) { return gitTrim(".",
                 "--version") }` so doctor_test's stub is untouched — discoverRedProofPins +
                 classification, grantStore(root)), calls the checks, prints through one
                 printLines/printSections helper with the exact current glyphs ("  ✓ ",
                 "  ✗ ", "  ⚠ ", Note verbatim), tallies Issue lines into `issues` exactly as
                 the inline code did. phaseCommitsOnMain's exec.Command moves onto
                 gitTrim/gitOut and execLookPath is already mutationcfg.LookPath (t-3), so
                 doctor.go drops its os/exec import (D6). reportExecGatedSurface stays in cmd
                 (prints execGatedCommands). Sections c-3 does not name (remote/board field
                 checks, .gitattributes, version parity, stale branches, backfill residue,
                 stranded mirrors) stay inline.
                 Tests: doctor_test.go's direct-call tests (remoteCombinationWarnings,
                 boardCombinationWarnings, sameCommitSHA, duplicateRoadmapSlugs,
                 gitVersionAtLeast) move to internal/diag; refguard_test.go moves to
                 argfence. cmd tests that read `.level`/`.text` on redProofChecks' result
                 (doctor_test, redproof_*_test) get the mechanical `.Level`/`.Text` rename
                 via `type doctorLine = diag.Line` — no expectation changes. Every
                 runDoctorEnum/captureStdout e2e test stays in cmd.
       covers:   c-3, c-4 (doctor consumes mutationcfg.Tools/Missing), c-6
       test_contract:
         - TestPinLinesMatrix (diag): Reach=Unreachable → one Issue line containing
           "unreachable" and the RepointHint; Indeterminate → one Warn; DocErr set → Issue
           containing "cannot be read"; DocSHA "" → Issue containing "carries no `base
           commit:`"; DocSHA an abbreviated prefix of SHA → no doc line; all clear → exactly
           one OK line. Swapping Issue/Warn on any arm fails.   [verification+mvp]
         - Escaping doc suppresses every pin: discoverRedProofPins with one changes.json
           whose red_proof.doc is `../x` returns the pathfence error and zero pins, and
           RedProof returns exactly one Issue line whose text does not contain "could not be
           read" (moved wording distinction).   [risk]
         - TestConfigTrustExecConsentLadder: a stale Store yields the "Exec consent:" section
           with one Issue containing "CHANGED" and Issues()==1; an absent store yields a
           Warn ("has not trusted") and Issues()==0; lanes declared + testCmd "" yields the
           "`dross test --files` still runs the lanes" wording.   [verification]
         - TestConfigTrustHostOutsideAllowlist: api_base on evil.example yields an Issue plus
           the Note "Fix (only if you trust this host): `dross local set allow_hosts
           evil.example`".   [verification]
         - Lane consent: a lane whose stored fingerprint mismatches yields Level=Issue and a
           lane with no grant yields Level=Warn; two lanes one granted one stale yield
           "lane \"a\": trusted" OK and a stale Issue naming `dross trust --lane b`; a
           prepared lane's row includes the prepare line.   [risk+verification]
         - TestMutationToolchainNamesTheAdapter: LookPath failing for "gremlins" with
           adapters=["gremlins"] yields one Warn naming gremlins, "go" and the install hint;
           LookPath succeeding yields no lines (today's early return).   [verification]
         - Printing is only in cmd: diag tests wrap os.Stdout in a pipe around every check
           call and fail if a single byte is written.   [risk]
         - TestDuplicateRoadmapSlugs, TestRemoteCombinationWarnings
           (("bitbucket","","") warns on auth_user; ("github","basic","u") warns "no
           effect"), TestSameCommitSHA(("abc123","ABC") true), TestGitVersionAtLeast("git
           version 2.23.0","2.24") false — moved.   [verification]
         - argfence.ValidateGitRef rejects a leading-dash ref, `--`, and empty (moved
           table); cmd's doctor branch-name test ("git reads a leading dash as an option")
           passes unchanged through the wrapper.   [mvp+risk]
         - cmd unchanged: TestDoctorWarnsJiraEpicCombination, TestDoctorReportsStaleConsent,
           TestDoctorReportsAbsentConsent, TestDoctorReportsEachLaneGrant,
           TestDoctorCountsEachRefusedLane, TestDoctorStaleLaneMovesTheExitCode,
           TestDoctorNamesTheGatedSurface, TestDoctorShowsAPreparedLanesBootstrap,
           TestDoctorReportsOldGit (stub untouched), exit-code tests, and t-1's
           TestDoctorRunGolden byte-identical.   [all]
         - internal/cmd/doctor.go no longer imports os/exec — t-9's baseline omits it.   [mvp+verification]
       depends_on: [t-3, t-4]

Wave 3

  t-7  Extract reap classify / discover / apply / undo to boardsync   [mvp+verification+risk]
       wave:     3
       files:    internal/boardsync/reap.go (new), internal/boardsync/reap_discover.go (new),
                 internal/boardsync/reap_apply.go (new), internal/boardsync/reap_undo.go (new),
                 internal/boardsync/reap_*_test.go (moved), internal/cmd/issue_reap.go (deleted),
                 internal/cmd/issue_reap_discover.go (deleted), internal/cmd/issue_reap_apply.go (deleted),
                 internal/cmd/issue_reap_undo.go (deleted), internal/cmd/issue_reap_cmd.go,
                 internal/cmd/doctor.go, internal/cmd/issue_reap_classify_test.go,
                 internal/cmd/issue_reap_discover_test.go, internal/cmd/issue_reap_apply_test.go,
                 internal/cmd/issue_reap_undo_test.go
       description:
                 Move reapVerdict/reapCard/reapPlan/reapLane, reapLanes, reapLaneFor,
                 reapLaneNames, classifyReap → Classify, buildReapPlan, resolveReapLanes,
                 phaseRecordVerdict, the classify*Mirrors set, roadmapSlugs,
                 reapBacklogVerdict, slugVerdict, orphanKind, identityLabels, orphanIdentity,
                 discoverReap → Discover, orphanVerdict, hasLabel, reapInventory → Inventory,
                 reapFailure, lanesDroppingTheirLink, applyReap → Apply, relabelReapedCard,
                 priorStateOf, dropBacklogLink, appendReapRun, reapLogPathFor, undoReap →
                 Undo, restoreDroppedLink. Doneness goes through phase.Done (t-2), never a
                 local re-read of changes.json; deferred entries via deferred.Collect.
                 cmd keeps issue_reap_cmd.go (issueReap, printReapPlan, boardNamespaceNames,
                 validateReapNamespaces — the last two read boardsync.ReapLaneNames);
                 doctor.go's stranded-mirrors section (line ~1774) calls boardsync.Inventory.
                 Tests: issue_reap_classify_test.go (readOnlyYT fake), the direct-call tests
                 of issue_reap_discover_test.go / issue_reap_apply_test.go /
                 issue_reap_undo_test.go, and TestEveryBoardNamespaceHasAReapPath /
                 TestReapTerminalMatchesTheForwardTerminal / TestEveryReapTerminalIsMapped
                 move; issue_reap_cmd_test.go and the cobra halves stay.
       covers:   c-2 (part 2), c-6
       test_contract:
         - readOnlyYT fixture (moved): any non-GET during Classify fails — a classifier that
           decides by patching a card reddens here.   [verification]
         - TestPhaseCardIsClassifiedFromItsCompletionRecord (moved): changes.json complete →
           reap; shipped → not yet; no phase dir → unattributable.   [verification+mvp]
         - Partial-failure accounting: Apply over 3 cards where the fake fails the 2nd
           returns a failure naming that key, the 1st relabel is in the reap log (card count
           == 1), the 3rd was not attempted.   [risk]
         - Undo restores only what it dropped: after Apply drops a Backlog link, Undo
           re-links it; a Phase-lane card is relabelled back without touching board.json;
           TestSecondApplyIsANoOp writes nothing.   [risk+verification]
         - Orphan identity is label-derived: `dross/phase:x` with no board.json link →
           orphan kind=phase; marker-only → unclassifiable second return.   [risk]
         - A board namespace gaining a label family with no reap lane fails moved
           TestEveryBoardNamespaceHasAReapPath.   [mvp]
         - cmd unchanged: issue_reap_cmd_test.go (dry-run plan rendering, `--undo`+`--apply`
           conflict text, namespace flag validation, `--apply` writes the reaplog run and
           relabels) — no assertion edits.   [all]
       depends_on: [t-5]

  t-8  Extract task sync and task pull to boardsync   [verification+risk; mvp folded these into t-4/t-6]
       wave:     3
       files:    internal/boardsync/task.go (new), internal/boardsync/task_pull.go (new),
                 internal/boardsync/lifecycle.go (new), internal/boardsync/inbound.go (new),
                 internal/boardsync/task_test.go, task_pull_test.go, lifecycle_test.go (moved),
                 internal/cmd/issue_task.go, internal/cmd/issue_task_pull.go,
                 internal/cmd/task_lifecycle.go (deleted), internal/cmd/issue.go,
                 internal/cmd/issue_task_pull_test.go, internal/cmd/issue_task_state_test.go,
                 internal/cmd/task_lifecycle_test.go
       description:
                 task.go: from issue_task.go taskCloseError, syncTasks → SyncTasks,
                 runWarnings, syncOneTask, resolveTaskIssue, renderTaskBody, setBoardState.
                 lifecycle.go: task_lifecycle.go whole (taskLifecycle, statusTaskComplete,
                 boardStatusToPlan, lifecycleForPlanStatus, planStatusForLifecycle);
                 milestoneStatusComplete stays in milestone_finalize_state.go.
                 task_pull.go: taskMoveKind, taskMoveVerdict, taskPull → TaskPull(ctx,
                 phaseID, plan *phase.Plan, planPath string, apply bool),
                 providerHasWorkflowState, collectTaskMoves, classifyTaskMove,
                 lifecycleFromLabels, reportTaskMoves. inbound.go: from issue.go
                 collectInbound → CollectInbound, pullEnvelope, emitPullEnvelope →
                 EmitPullEnvelope(w, …), reportBoardFailure → ReportBoardFailure.
                 cmd keeps issueTaskSync and the issueTaskPull constructor, which resolves
                 the phase (resolveTaskPhaseID, loadPhasePlanAndSpec) and hands plan + path
                 to boardsync.TaskPull.
                 Tests: issue_task_pull_test.go's Ctx-constructing tests,
                 issue_task_state_test.go's classify tests, task_lifecycle_test.go move;
                 TestTaskPullCommandNoOpsWhenBoardIsOff / NeedsAPhase / SurfacesASetupFailure
                 stay in cmd.
       covers:   c-2 (part 3), c-6
       test_contract:
         - TestClassifyBoardMoved / TestClassifyConflict / TestClassifyUnchanged (moved): an
           issue whose tracker state is "In Progress" but whose dross/status label is
           task-in-review classifies as move-to-done; inverting the provider map instead of
           reading the label fails.   [verification]
         - Idempotent on lifecycle drift: classifyTaskMove with a board state mapping to the
           plan's current status returns noop; an unknown label returns unmapped and
           reportTaskMoves writes no plan.toml (TestReportDryRunWritesNothing).   [risk+verification]
         - TestTaskCloseRequiresAStatus + TestTaskClosePartialFailureNamesTheTaskAndKeepsGoing
           (moved): doClose without a status refuses before touching the board; a mid-run
           failure returns a TaskCloseError naming the task and the remaining tasks still
           close.   [verification]
         - TestLifecycleMappingRoundTrips (moved): every taskLifecycle value inverts back;
           task-complete → done.   [verification]
         - cmd unchanged: the three TestTaskPullCommand* tests, issue_task_test.go and
           issue_task_close_test.go cobra tests — no assertion edits.   [all]
       depends_on: [t-5]

  t-9  Import-direction boundary test with shrink-only exec ratchet and no-test-lost   [mvp+verification+risk]
       wave:     3
       files:    internal/cmd/boundary_test.go (new),
                 internal/cmd/testdata/cli_surface/tests_before.txt (read)
       description:
                 A go/parser ImportsOnly walk over every directory under internal/ holding a
                 non-test .go file (skipping testdata), building {package → imports}. A
                 `checkBoundary(pkgs, baseline)` function returns findings so the test runs
                 it over the live tree AND over synthetic package maps / a t.TempDir tree to
                 prove each rule fires. Rules (locked proof_shape): (1)
                 github.com/spf13/cobra appears only in internal/cmd; (2) no package under
                 internal/ imports internal/cmd; (3) internal/cmd imports each of
                 internal/consent, internal/boardsync, internal/diag, internal/mutationcfg
                 (fails naming the missing one); (4) ratchet: a non-test internal/cmd file
                 importing os/exec, net/http or go/ast must be in the baseline literal, and
                 every baseline entry must still import the package it is listed for — a
                 stale entry fails with "remove it", which is what makes the list
                 shrink-only. Baseline is derived at task time by grep; measured today:
                 os/exec → cleantree.go, init.go, lane_install.go, milestone_stale.go,
                 pause.go, phase.go, redproof_replay.go, run.go, ship_recover.go, stack.go,
                 statusline.go, survivor_drain.go, techdebt.go, test.go, update.go,
                 verify.go (detach self-spawn + exec.ExitError), worktree_files.go (17);
                 net/http → update.go; go/ast → none; doctor.go must NOT appear. (5)
                 Vacuity floor mirroring execConsentFloor: ≥ 30 packages seen and
                 internal/cmd among them (39 today, ~44 after this phase; NOT 40, which is
                 false pre-phase). (6) TestNoTestLost: every name in tests_before.txt exists
                 as a `func Test` somewhere under internal/. Lives in internal/cmd beside the
                 other repo-wide audits (a test-only package under internal/ is skipped
                 silently by `go build ./...`; internal/architecture is the ARCHITECTURE.md
                 generator and would mislead).
       covers:   c-5, c-6
       test_contract:
         - Adding `"os/exec"` to internal/cmd/issue.go fails TestCmdForbiddenImportRatchet
           naming issue.go and the import; a baseline entry whose file no longer imports
           any forbidden package fails asking for its removal; feeding the checker the real
           baseline plus one extra existing cmd file fails.   [all]
         - Synthetic tree: a fake internal/consent/x.go importing internal/cmd fails naming
           both packages; a fake internal/foo importing cobra fails the exclusivity rule;
           dropping cmd's import of internal/consent fails TestCmdImportsExtractedPackages.   [risk+mvp+verification]
         - The floor bites: the checker over an empty temp root, or pointed at internal/cmd
           only, returns the vacuity error rather than passing.   [risk+verification]
         - Deleting a moved test instead of moving it fails TestNoTestLost naming it.   [verification]
         - Live tree: green at commit, baseline listing exactly the observed importers.   [risk]
       depends_on: [t-3, t-4, t-5, t-6]

Wave 4

  t-10 Prove c-6 end to end against the pre-move goldens   [verification+risk]
       wave:     4
       files:    internal/cmd/cli_surface_test.go, internal/cmd/testdata/cli_surface/*.txt,
                 .dross/phases/cmd-package-decomposition/verify.toml (by dross verify, not by hand)
       description:
                 With every move landed: run TestCLISurfacePinned and TestDoctorRunGolden
                 WITHOUT DROSS_UPDATE_GOLDEN (they must pass against the goldens t-1
                 recorded before any code moved); run the whole suite remotely via
                 `dross test` (never --local — internal/cmd hangs on the laptop) and read the
                 green before anything is committed; inspect `git diff <phase-base> --
                 'internal/cmd/*_test.go'`: permitted hunks are file deletions (moved
                 tests), import lines, package-qualifier renames, `.level`/`.text` →
                 `.Level`/`.Text`, and boardCtx field case; any hunk touching a line
                 containing t.Error, t.Fatal, want, or a quoted expected string is a c-6
                 violation to fix, not merge. Test-set conservation: for every test moved in
                 t-2..t-8, `go test -list '^Name$' ./internal/<newpkg>` lists it and `go test
                 -list '^Name$' ./internal/cmd` does not (present in both = copied not moved;
                 neither = dropped). Goldens stay as a permanent guard; nothing is deleted.
       covers:   c-6
       test_contract:
         - TestCLISurfacePinned and TestDoctorRunGolden pass against the t-1 goldens; a flag
           renamed during any move shows here even if every other test passed.   [verification]
         - `grep -c 'func Test'` over internal/cmd plus the extracted packages ≥ the
           pre-phase cmd count (2544), and TestNoTestLost (t-9) is green.   [verification]
         - The `go test -list` conservation check passes for every moved name.   [risk]
         - The observed `dross test` run is green in this session before verify.toml is
           written; the commit body states the run it observed (never claim an unseen
           result).   [risk+verification]
       depends_on: [t-1, t-7, t-8, t-9]

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-4 |
| c-2 | t-2 (enabling), t-5, t-7, t-8 |
| c-3 | t-2 (enabling), t-6 |
| c-4 | t-3, t-6 (doctor consumes mutationcfg.Tools/Missing) |
| c-5 | t-9 |
| c-6 | t-1 (recorded first), t-9 (no-test-lost), t-10 (final proof), plus every extraction's "cmd unchanged" contract |

## Disagreements

D1. **Consent persistence seam.** risk: move the whole typed `localStore` +
readers to a new `internal/localstore` (t-1) because a partial-struct BurntSushi
Save deletes every unknown key. mvp: `consent.Grants` embedded in localStore,
consent writes via a map[string]any overlay (two encoders on one file).
verification: `consent.Store` interface {Load; Save(*Grants)} with cmd's
localStore embedding Grants and a cmd adapter doing the full load/swap/save.
**Default: verification's Store interface**, with risk's clobber-guard test
grafted as the cmd round-trip contract. Why it matters: risk's move IS the
spec's second deferred item ("move local.go persistence … into its own
package"); mvp's overlay creates a second writer whose key order and table
encoding differ from the typed one; the interface keeps one writer and needs
no new package. Risk's objection ("tests stop exercising the real file") is
answered by the file-backed test Store plus the cmd round-trip test.

D2. **Remote pool location.** risk t-3 moves remote_pool.go/remote_preflight.go
to `internal/remote` and re-points 6 test files' `remoteProbeFn` stubs to
`remote.ProbeFn`. mvp and verification keep the pool in cmd and hand
mutationcfg a Select/Probe closure. **Default: stays in cmd.** No criterion
names the pool, it is shared with `dross test` lanes and bootstrap, it prints
through cmd, and the closure seam is what makes the fallback-vs-abort matrix
testable without ssh. Cost: mutationcfg's "single construction path" is
single only above the pool walk.

D3. **Home of the deferred ledger.** risk puts collectDeferred/deferredStore
into `internal/phase`; mvp and verification create `internal/deferred`.
**Default: internal/deferred.** deferred.go imports `milestone` (checked);
folding it into `phase` would make a phase package milestone-aware. phase.Done
goes to `internal/phase` in all three.

D4. **Golden pin before moves.** Only verification records CLI-tree and
doctor-transcript goldens before any code moves (t-1); mvp and risk rely on
existing cmd tests plus (risk) a final diff inspection. **Default: include
t-1.** Existing tests assert substrings — a dropped blank line, reordered
section or changed flag default would not fail them, and c-6 says "output …
unchanged". Cost: one task and a fixture-bound doctor golden that any future
doctor change must regenerate under DROSS_UPDATE_GOLDEN=1.

D5. **Red-proof git access.** risk moves classifyReachability/discoverRedProofPins
into the diagnostics package behind a 2-method `Git` interface satisfied by
cmd's gitTrim/gitNoOut, buying a deterministic test of the shallow-clone arm.
mvp and verification keep classification in cmd and pass pre-classified pins.
**Default: pre-classified pins.** It keeps the diagnostics package free of
git plumbing and matches c-3's "Doctor() composes"; the shallow-arm coverage
risk wanted is real and is not delivered by this plan.

D6. **How doctor.go leaves the os/exec baseline.** verification moves
phaseCommitsOnMain's `git rev-list` spawn (and gitVersionOutput) into diag,
making diag an os/exec importer. mvp keeps the spawns in cmd on
gitTrim/gitOut and keeps the gitVersionOutput seam var in cmd. risk leaves the
leaked-commit and git-version sections "as-is", which would keep doctor.go in
the baseline (its measured count then = 18). **Default: mvp.** doctor.go is in
a named domain (c-3) so it must leave the baseline; keeping spawns in cmd
keeps diag pure and leaves doctor_test's `gitVersionOutput` stub untouched.
Baseline is 17 files, not the spec's "~13".

D7. **Diagnostics extraction: one task or two.** verification splits it into
pure checks (wave 1) and trust/lane-consent/toolchain (wave 2); mvp and risk do
it in one task. **Default: one task (t-6).** Verification's wave-1 half would
collide on doctor.go with t-3 and t-4 in the same wave; sequencing the halves
across waves adds a wave for no parallelism gain. Cost: t-6 is the largest
task in the plan (~8 check families, ~600 lines of doctor.go).

D8. **Seam vars stay in cmd vs. move.** mvp keeps `execLookPath =
mutationcfg.LookPath`, `gitVersionOutput`, `remoteProbeFn`,
`configuredAdaptersFn` in cmd so no test stub line changes. verification
re-points `execLookPath`→`diag.LookPath` and `gitVersionOutput`→`diag.GitVersion`
in cmd tests; risk re-points `remoteProbeFn`→`remote.ProbeFn` and
`execLookPath`→`mutationcfg.LookPath`. **Default: mvp — zero stub edits.**
A stub bound to the wrong seam is a silent test hollowing (risk's own
judgment call names this failure from remote_bootstrap); keeping the vars in
cmd removes the opportunity.

D9. **Old-name wrappers vs. qualified renames at call sites.** risk keeps
one-line cmd wrappers (`phaseDone`, `collectDeferred`, `validateGitRef`,
`selectRemoteTarget`) so ~40 call sites are untouched; mvp and verification
rename call sites to the qualified names (verification injects `ValidateRef`
instead of moving refguard). **Default: wrapper for `validateGitRef` only
(12 callers; mvp and risk agree), qualified renames for phase.Done and
deferred.Collect.** Note risk's factual premise for the refguard wrapper
(the subprocargs audit matches `validateGitRef` by name) is false — it names
only gitRefArgs/gitPathArgs — so the wrapper is a churn decision, not an
audit requirement.

D10. **Moved-test conservation guard.** risk adds a permanent checked-in
manifest of (test name, new package) rows (≥40, t-10) that fails on copied-
not-moved and on dropped. verification records tests_before.txt once and
TestNoTestLost checks each name exists somewhere under internal/ (catches
drops, not copies). **Default: verification's TestNoTestLost (t-9) plus a
one-off `go test -list` copy/drop check in t-10.** A hand-maintained
(name, package) manifest is a maintenance tax on every future move; the
copy check is needed once, at the end of this phase.

D11. **Board output seam.** mvp leaves boardsync writing through fmt.Printf
(os.Stdout resolved at call time, so captureStdout works); risk and
verification add `Ctx.Out io.Writer`. **Default: Ctx.Out, nil → os.Stdout
resolved at call time** (risk's phrasing), never a package-level initializer.
It costs nothing for cmd and lets the moved unit tests capture output without
a pipe. The warning that matters is risk's: a `var out = os.Stdout` at package
init bypasses captureStdout's swap and every cmd sync-output assertion reads "".

D12. **Boundary-test wave.** verification runs it in wave 3 alongside reap/task
(its dependencies — the four imports and doctor.go's drain — are satisfied
after wave 2); mvp and risk put it last. **Default: wave 3 (t-9).** t-7/t-8
touch no os/exec importer and move tests only within internal/, so neither
rule nor floor changes after wave 2; t-10 then closes the phase in wave 4.

D13. **Diagnostics package name.** mvp `internal/diagnose`, verification
`internal/diag`, risk `internal/doctor`. **Default: `internal/diag`.** A
package named `doctor` beside cmd's `Doctor()` invites the same confusion risk
flagged for internal/architecture.

## Facts settled against the tree

- Non-test cmd files importing os/exec: 18 (doctor.go included); net/http:
  update.go; go/ast: none. Post-t-6 baseline = 17.
- Packages under internal/ with non-test .go files: 39. cmd: 114 non-test,
  281 test files, 2544 `func Test`.
- execconsent_audit_test.go: isExecConsentCall = `requireExecConsent` or
  suffix `Consented`; scan roots are all of `internal` and `cmd`; marker
  `//dross:exec-exempt`. subprocargs_audit_test.go names only
  gitRefArgs/gitPathArgs.
- Seam vars: `remoteProbeFn` (doctor.go:1422, 6 test stubs), `execLookPath`
  (doctor.go:1378, doctor_multilang_test), `gitVersionOutput`
  (doctor.go:1084, doctor_test), `configuredAdaptersFn` (verify.go:1014, 5
  test files). captureStdout swaps os.Stdout at call time; cmd's Print/Printf
  are fmt wrappers.
- checkConfigTrust calls readAllowHosts, validateGitRef, ignoresPath
  (gitignore.go:127), gitVersionOutput, hostallow.Derive, CheckConsent,
  reportLaneConsent, reportExecGatedSurface.
- deferred.go imports milestone + phase; phase imports pathfence; changes,
  milestone, board, argfence import nothing internal; mutation imports
  argfence, remote, telemetry.
- doctor.go:1774 calls reapInventory (so t-7 touches doctor.go, after t-6).
- toolfence_composer_test.go composerRoots = {".", "../ship"} today.
- Every file named by any draft exists (spot-checked 27 names).
