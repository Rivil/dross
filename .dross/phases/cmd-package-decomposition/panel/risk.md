# Lens: risk — failure modes drive the graph

Every task below owns a named way the move can break, and its contract is the
test that catches exactly that break. The seam through the deferred local.go
domain is chosen for the failure it prevents (a partial-struct write that
clobbers unrelated keys), not for elegance. Function names that an existing
audit matches BY NAME (`requireExecConsent`, `*Consented`, `gitRefArgs`,
`gitPathArgs`, `validateGitRef`) stay spelled exactly as today at every call
site, either because the function stays in cmd or because cmd keeps a
one-line wrapper of the same name.

Phase cmd-package-decomposition — 10 tasks across 4 waves

Wave 1

  t-1  Extract local.toml store seam to internal/localstore
       files:    internal/localstore/store.go (new), internal/localstore/store_test.go (new),
                 internal/localstore/grants.go (new), internal/localstore/grants_test.go (new),
                 internal/cmd/local.go, internal/cmd/local_test.go, internal/cmd/local_resolution_test.go
       covers:   c-1, c-4 (substrate; no criterion closes here)
       description:
                 Move the TYPED store — `localStore` struct with EVERY field, `LocalFile`,
                 `localPath`, `loadLocal`, `save`, `refuseTrackedLocal` — to
                 localstore.{Store, File, Path, Load, (*Store).Save, RefuseTracked}. Move the
                 store readers the two consumers in wave 2 need and that nothing else may
                 re-derive: `readRemoteGrants`, `remoteCandidate`, `resolveRemoteEnv`,
                 `readMutationTuning`, `parseTuning`, `readAllowHosts` → localstore.
                 {ReadRemoteGrants, ReadAllowHosts, ReadMutationTuning, ResolveRemoteEnv}.
                 RefuseTracked spawns git itself with `cmd.Dir = repoDir` and a LITERAL
                 pathspec behind `--` (no `-C repoDir` positional before the separator), and
                 carries a `//dross:exec-exempt` marker in the grammar
                 TestExecConsentMarkerGrammar accepts. cmd/local.go keeps `Local()`,
                 `localKeys`, get/set, detached runs, `effectiveRemote`, `resolveRemoteHost`
                 (deferred domain) and calls the package. Unit tests of the moved readers
                 move (tracked-store refusal, unparseable-store-fails-closed, unset-env-name
                 refusal, tuning parse errors); cobra `dross local get/set` tests stay.
       depends_on: []
       test_contract:
         - Clobber guard (the reason the whole struct moves, not a consent-only slice): a
           localstore_test writes a store carrying quick_base, mutation_remote_host,
           trusted_lane_commands and detached_runs, then Load→set one field→Save; every
           other key round-trips byte-identical. If a later task narrows Store to a partial
           struct, this test fails on the dropped keys.
         - Fail-closed: `git add .dross/local.toml` in a fixture repo makes
           localstore.RefuseTracked return the "git reports it tracked" error and
           ReadRemoteGrants/ReadAllowHosts return that error with a nil list — never an
           empty grant. A syntactically broken local.toml makes ReadRemoteGrants error
           (not empty) and Load error (not zero store).
         - Both repo-wide audits stay green with the new site COUNTED: TestEverySpawnSiteGatedOrExempt
           reports one more spawn site (in internal/localstore) and zero findings; the
           subprocess-argv audit (subprocargs_audit_test.go) reports no finding for the
           new `git ls-files` site. If the marker is missing or `repoDir` lands as a
           positional before `--`, one of the two fails by name.
         - cmd e2e: TestLocalSetCannotGrantConsent (trust_test.go) and every `dross local set/get` test
           in local_test.go pass with no assertion edits.

  t-2  Relocate pure phase/ref helpers shared by wave-2 packages
       files:    internal/phase/done.go (new), internal/phase/done_test.go (new),
                 internal/phase/deferred.go (new), internal/phase/deferred_test.go (new),
                 internal/argfence/gitref.go (new), internal/argfence/gitref_test.go (new),
                 internal/cmd/phasedone.go, internal/cmd/deferred.go, internal/cmd/deferred_store.go,
                 internal/cmd/refguard.go, internal/cmd/refguard_test.go, internal/cmd/deferred_store_test.go
       covers:   c-2, c-3 (substrate)
       description:
                 `phaseDone/phaseIsDone/phaseDirExists` → phase.{Done, IsDone, DirExists}.
                 `deferredEntry`, `collectDeferred`, `flattenDeferred`, `deferredStorePath`,
                 `deferredStore`, `loadDeferredStore`, `projectStoreSlug` → phase.
                 {DeferredEntry, CollectDeferred, FlattenDeferred, DeferredStorePath,
                 DeferredStore, LoadDeferredStore}. `validateGitRef` → argfence.ValidateGitRef.
                 cmd keeps ONE-LINE wrappers with the old unexported names (`phaseDone`,
                 `collectDeferred`, `validateGitRef`) so the ~30 cmd call sites and the
                 subprocargs audit's by-name recognition are untouched; the wrappers are the
                 only body left in phasedone.go / refguard.go. phase must not import cmd;
                 phase gains an import of internal/changes (leaf, no cycle).
       depends_on: []
       test_contract:
         - Doneness has one answer: phase/done_test proves a slug with a phase dir whose
           changes.json status is "complete" or "shipped" is Done, an unscaffolded slug is
           never Done, and a verify.toml "pass" with no completion record is NOT Done. If a
           cmd surface re-implements doneness locally, milestone_progress_test / status_test
           / phase_reconcile_test (unchanged) still pass only because they route through
           the wrapper — the wrapper body is the single call.
         - refguard_test.go stays in cmd byte-identical and green through the wrapper
           (proves the by-name call surface survived); argfence/gitref_test carries the
           same option-shaped-ref table (leading dash, `--`, empty) moved verbatim.
         - `go list -deps ./internal/phase` does not contain internal/cmd (import-cycle guard,
           asserted by the build itself; a cycle here fails every package's compile).

  t-3  Move remote pool probing + preflight to internal/remote
       files:    internal/remote/pool.go (new), internal/remote/pool_test.go (new),
                 internal/remote/preflight.go (new), internal/cmd/remote_pool.go,
                 internal/cmd/remote_preflight.go, internal/cmd/remote_pool_test.go,
                 internal/cmd/doctor.go, internal/cmd/remote_bootstrap_cmd.go,
                 internal/cmd/lane_preview_locality.go, internal/cmd/remote_preflight_wiring_test.go
       covers:   c-4 (substrate)
       description:
                 `poolCandidate`, `remotePool`, `poolMiss`, `probeRemotePool`,
                 `probeEveryCandidate`, `walkRemotePool`, `containsTool`, `pickRemoteTarget`,
                 `preflight`, `preflightRemote` → remote.{PoolCandidate, Pool, PoolMiss,
                 ProbePool, ProbeEveryCandidate, PickTarget, Preflight, PreflightRemote}.
                 The `remoteProbeFn` seam becomes `remote.ProbeFn` (package-level var
                 defaulting to Probe, in the same package). `selectRemoteTarget` — the one
                 function that PRINTS notices — stays in cmd/remote_pool.go as a thin wrapper
                 over remote.PickTarget that Printf's the returned notices in order. The six
                 cmd test files that stub `remoteProbeFn` re-point the stub to
                 `remote.ProbeFn` (setup line only). Pool unit tests (walk order, stop-when-
                 covered, unreached attribution) move into remote/pool_test.go.
       depends_on: []
       test_contract:
         - Seam re-point is complete: with `remote.ProbeFn` stubbed to fail for host A and
           succeed for host B, `dross doctor` (doctor_remote_test.go, unchanged assertions)
           still reports B as the usable host — if any path still calls remote.Probe
           directly, the stub is bypassed and the assertion on "using B" fails.
         - Notices are data, not stdout: remote/pool_test asserts PickTarget returns the
           "skipping <host>: <why>" notices in probe order and writes nothing to os.Stdout
           (captured via pipe); the cmd wrapper test in remote_pool_test.go asserts the same
           notices appear on stdout in that order and the swap line "remote: using B"
           follows them.
         - TestExecConsentScansTheSpawningPackages still covers internal/remote/remote.go
           (the sweep's per-file coverage floor is unchanged by adding pool.go to the package).

Wave 2 (depends t-1, t-2, t-3)

  t-4  Extract consent store to internal/consent
       files:    internal/consent/consent.go (new), internal/consent/lane.go (new),
                 internal/consent/refusal.go (new), internal/consent/consent_test.go (new),
                 internal/consent/lane_test.go (new), internal/cmd/trust.go,
                 internal/cmd/trust_test.go, internal/cmd/trust_lane_test.go,
                 internal/cmd/trust_lane_install_test.go, internal/cmd/lane_preview.go,
                 internal/cmd/lane_preview_json.go, internal/cmd/test.go, internal/cmd/run.go,
                 internal/cmd/redproof_replay.go, internal/cmd/lane_install.go
       covers:   c-1
       description:
                 Move `ConsentState` (+String), the Err* sentinels, `Fingerprint`,
                 `CheckConsent`, `GrantConsent`, Replay/Run/Lane/LaneInstall
                 Consented+Grant+Revoke, `fingerprintInSet`, `addFingerprint`, the three
                 frame consts, `laneConsentLine`, `laneInstallConsentLine`, and the pure
                 refusal builders `consentRefusal`, `laneConsentRefusal`,
                 `laneInstallRefusal` → package consent (State, Granted/Stale/…,
                 LaneConsentLine, LaneInstallConsentLine, Refusal, LaneRefusal,
                 LaneInstallRefusal), reading/writing through localstore. Exported names
                 keep the `Consented` suffix verbatim. cmd/trust.go KEEPS: the package
                 comment sentence naming TestEverySpawnSiteGatedOrExempt, `execGatedCommands`
                 and its comment, `requireExecConsent` (now `consent.CheckConsent` +
                 `consent.Refusal`), `Trust()`, trustLane/trustRun/trustReplay/
                 trustLaneInstall, `findLane`, `recordedReplayLine`. Unit tests move:
                 TestFingerprint, TestGrantStoresHashNotCommand, TestConsentStates,
                 TestConsentRefusesTrackedLocalToml, TestConsentNotApplicable, the lane
                 frame/length-framing/NUL tests in trust_lane_test.go, the install-line
                 tests in trust_lane_install_test.go. E2e tests stay: TestGatedCommandsRefuse,
                 TestExecGatedSetIsExplicit, TestGateDoesNotBrickReadOnly, TestTrust*,
                 TestRefusalWritesNothing, TestEmptyTestCommandDoesNotBypassGate.
       depends_on: [t-1]
       test_contract:
         - Gate reach survives the move: TestEverySpawnSiteGatedOrExempt still resolves every
           site in run.go, test.go, verify.go, lane_install.go, survivor_drain.go as GATED
           (execConsentGatedFiles) — the walk matches `requireExecConsent` and `*Consented`
           BY NAME across packages, so a renamed `consent.Check` (no suffix) at a lane call
           site turns a gated site into a finding. The "deleting `dross run`'s consent check
           changes nothing" negative test (execconsent_audit_test.go ~L2180) still fires.
         - execconsent_docs_test.go passes unchanged: cmd/trust.go still declares
           `execGatedCommands` and its comment still names TestEverySpawnSiteGatedOrExempt.
         - Framing is unforgeable across the package boundary: consent/lane_test proves
           {prepare:"a",command:"bc"} and {prepare:"ab",command:"c"} fingerprint
           differently, a no-prepare lane's line equals its bare command byte-for-byte
           (grants written before this phase stay valid), and a command containing NUL is
           refused. If the frame consts drift during the move, an old-machine grant
           silently stales — this is the test that says so.
         - Stale beats absent: consent_test proves a store holding sha256("old") checked
           against "new" returns (Stale, ErrStaleConsent), never (Absent, ErrNoConsent);
           and empty testCmd returns (NotApplicable, ErrNoTestCommand).
         - cmd e2e: TestTrustStaleMessage, TestTrustCheckExitCodes, TestGatedCommandsRefuse
           pass with no assertion edits (refusal text and exit codes byte-identical).
         - `dross test lane preview --json` (lane_preview_json tests) still emits
           "granted"/"stale"/… — State.String moved intact.

  t-5  Extract adapter construction to internal/mutationcfg
       files:    internal/mutationcfg/adapters.go (new), internal/mutationcfg/tuning.go (new),
                 internal/mutationcfg/toolchain.go (new), internal/mutationcfg/adapters_test.go (new),
                 internal/mutationcfg/tuning_test.go (new), internal/mutationcfg/toolchain_test.go (new),
                 internal/cmd/verify.go, internal/cmd/doctor.go, internal/cmd/survivor_drain.go,
                 internal/cmd/remote_bootstrap.go, internal/cmd/verify_test.go,
                 internal/cmd/mutation_remote_wiring_test.go, internal/cmd/doctor_multilang_test.go
       covers:   c-4
       description:
                 Move `mutationTuning` (+gremlins), `resolveMutationTuning`, `measuredOnOf`,
                 `profileCacheVars`, `configuredAdapters`, `dockerPrefix` → mutationcfg.
                 {Tuning, Resolve, MeasuredOn, ProfileCacheVars, Adapters, DockerPrefix}.
                 Move `remoteAdapterTools`, `remoteAdapterOrder`, `mutationToolInstall`,
                 `mutationToolLanguage`, `remoteMutationTools`, `execLookPath`, and the
                 LookPath loop of `checkMutationToolchain` → mutationcfg.{Tools, LookPath,
                 MissingLocalTools, InstallHint, Language}. Tools derives its allowlist
                 filter from the SAME `allowed` set Adapters uses (one helper `selected(p)`),
                 which is the c-4 unification — today two loops restate the empty-means-all
                 rule. Adapters takes `out io.Writer` and writes the pool notices there at
                 probe time (never a package-level `= os.Stdout` initializer — captureStdout
                 swaps os.Stdout after init). cmd keeps `var configuredAdaptersFn =
                 mutationcfg.Adapters`-shaped seam so verify tests' stubs are untouched;
                 survivor_drain's runGremlinsOverPackages calls Tuning.Gremlins; doctor's
                 remoteProbeTools composes mutationcfg.Tools + laneToolUnion. mutationcfg
                 imports project, mutation, remote, localstore, stack — never cmd.
       depends_on: [t-1, t-3]
       test_contract:
         - Single construction path: mutationcfg/adapters_test proves that for
           `[mutation] adapters = ["stryker"]`, Adapters returns exactly {Stryker} AND Tools
           returns exactly {"npx"→"stryker"}; for an empty allowlist both return all three.
           If a second allowlist loop reappears, the two can disagree and this test names
           which side.
         - Drain and verify build the same Gremlins: a test constructs via Adapters and via
           Tuning.Gremlins with identical inputs and asserts the two *mutation.Gremlins are
           deep-equal (Workers, TestCPU, Prefix, Remote, TimeoutCoefficient, CacheVars).
         - Notice ordering under partial failure: with remote.ProbeFn failing host A then
           answering on B, Adapters writes "remote: skipping A: …" then "remote: using B"
           to `out` BEFORE returning, and when every host is unreachable it returns a
           Tuning with FellBackFrom=last host and Prefix=DockerPrefix (local fallback), not
           an error. When a host ANSWERS with a failure, the error is returned and the
           notices were still written.
         - Docker prefix cannot be spoofed: "dockerevil compose exec app go test" yields
           "docker compose exec app", and "docker compose exec node node test.js" yields
           "docker compose exec node" (field-based, moved test).
         - doctor_multilang_test.go passes with only its LookPath stub re-pointed to
           mutationcfg.LookPath: the "⚠ npx is not installed — the stryker adapter needs
           it to measure TypeScript/JavaScript/Svelte files here" line is byte-identical.
         - verify_test.go / verify_detach_test.go / verify_results_test.go /
           verify_reuse_report_test.go / trust_test.go pass with `configuredAdaptersFn`
           stubs untouched.

  t-6  Extract board sync + task pull to internal/boardsync
       files:    internal/boardsync/ctx.go (new), internal/boardsync/labels.go (new),
                 internal/boardsync/backlog.go (new), internal/boardsync/phase.go (new),
                 internal/boardsync/milestone.go (new), internal/boardsync/task.go (new),
                 internal/boardsync/taskpull.go (new), internal/boardsync/lifecycle.go (new),
                 internal/boardsync/*_test.go (moved unit tests), internal/cmd/issue.go,
                 internal/cmd/issue_task.go, internal/cmd/issue_task_pull.go,
                 internal/cmd/task_lifecycle.go, internal/cmd/issue_test.go,
                 internal/cmd/issue_task_test.go, internal/cmd/issue_task_pull_test.go,
                 internal/cmd/issue_backlog_close_test.go, internal/cmd/issue_backlog_id_test.go,
                 internal/cmd/issue_close_truth_test.go, internal/cmd/issue_milestone_close_test.go,
                 internal/cmd/issue_phase_resolve_test.go, internal/cmd/issue_task_close_test.go,
                 internal/cmd/issue_task_state_test.go, internal/cmd/task_lifecycle_test.go
       covers:   c-2 (with t-7)
       description:
                 `boardCtx` → boardsync.Ctx with exported fields {Client, Board, Proj, Root,
                 BoardPath, Out io.Writer}; `ctx.out()` returns Out or os.Stdout resolved AT
                 CALL TIME (nil-safe, so unit tests that build a Ctx literal need no writer).
                 Move label vocab (`statusLabel`, `phaseLabel`, `deferredLabel`, `targetLabel`,
                 `taskLabel`, `hasMarker`, backlog keys), backlog sync (`backlogItem`,
                 `ensureDeferredIDs`, `newDeferredID`, `syncBacklog`, `reconcileBacklog`,
                 `backlogVerdictFor`, `boardIssueIsDone`, `pushBacklogItems`,
                 `resolveBacklogIssue`, `lookupPhaseIssue`, `adoptLegacyBacklogKey`), phase
                 sync (`resolvePhaseIssue`, `syncPhase`, `closeBoardIssue`, `verifyClosed`,
                 `derivePhaseStatus`, `renderPhaseBody`), milestone sync
                 (`ensureMilestoneLink`, `checkMilestoneClosable`, `milestoneBody`), task sync
                 (`taskCloseError`, `syncTasks`, `runWarnings`, `syncOneTask`,
                 `resolveTaskIssue`, `renderTaskBody`, `setBoardState`), task pull
                 (`taskMoveKind`, `taskMoveVerdict`, `taskPull`, `providerHasWorkflowState`,
                 `collectTaskMoves`, `classifyTaskMove`, `lifecycleFromLabels`,
                 `reportTaskMoves`), and task_lifecycle.go whole. `openBoard`, `boardConfig`,
                 `wrapBoard`, every `issue*()` cobra builder, `resolveBacklogVersion`,
                 `collectInbound`/pull envelope, dismiss/link/list/quick stay in cmd.
                 boardsync imports board, forge, phase, milestone, changes, reaplog, project
                 — never cmd; `RootDirName`-style constants it needs are re-declared locally.
                 Unit tests that build a `boardCtx{}` literal with a fake client move; tests
                 that go through `Issue()`/captureStdout stay.
       depends_on: [t-2]
       test_contract:
         - Output capture is not broken by the writer seam: a cmd e2e test in issue_test.go
           that captures `dross issue phase sync` output via captureStdout still sees the
           "phase <id> -> board <key> (<state>)" line. If Out is bound to os.Stdout at package init
           instead of call time, the pipe swap is bypassed and this assertion sees "".
         - Verdict tables move with their tests: boardsync/backlog_test carries the
           reconcile matrix (live+linked→update, deferred-dismissed→close, unknown key→
           orphan) from issue_backlog_close_test.go and issue_close_truth_test.go; the
           moved tests are found by `go test -list . ./internal/boardsync` under their
           original names, and `go test -list . ./internal/cmd` no longer lists them (a test
           present in both means it was copied, not moved; present in neither means
           coverage was dropped).
         - Close truth: verifyClosed's re-read still fails with "wrote the close for status
           … but the issue still reads unresolved" when the fake client accepts the close
           but reports the issue open (issue_close_truth_test.go, unchanged assertions).
         - Task pull is idempotent on lifecycle drift: classifyTaskMove with a board state
           mapping to the plan's current status returns kind=noop; with an unknown label
           returns kind=unmapped and reportTaskMoves writes no plan.toml (moved from
           issue_task_pull_test.go / issue_task_state_test.go).
         - cmd e2e: issue_verb_shape_test.go (command tree, flags, Use strings) passes with
           no edits — the cobra surface is unchanged by construction.

Wave 3

  t-7  Extract reap classify/discover/apply/undo to boardsync
       files:    internal/boardsync/reap.go (new), internal/boardsync/reap_discover.go (new),
                 internal/boardsync/reap_apply.go (new), internal/boardsync/reap_undo.go (new),
                 internal/boardsync/reap_*_test.go (moved), internal/cmd/issue_reap.go,
                 internal/cmd/issue_reap_discover.go, internal/cmd/issue_reap_apply.go,
                 internal/cmd/issue_reap_undo.go, internal/cmd/issue_reap_cmd.go,
                 internal/cmd/issue_reap_classify_test.go, internal/cmd/issue_reap_discover_test.go,
                 internal/cmd/issue_reap_apply_test.go, internal/cmd/issue_reap_undo_test.go,
                 internal/cmd/doctor.go
       covers:   c-2
       description:
                 Move reap verdict/card/plan/lane types, `reapLanes`, `classifyReap`,
                 `buildReapPlan`, the per-namespace classifiers, `roadmapSlugs`,
                 `slugVerdict`, orphan discovery (`orphanKind`, `identityLabels`,
                 `orphanIdentity`, `discoverReap`, `orphanVerdict`), `reapInventory`,
                 `reapFailure`, `lanesDroppingTheirLink`, `applyReap`, `relabelReapedCard`,
                 `priorStateOf`, `dropBacklogLink`, `appendReapRun`, `reapLogPathFor`,
                 `undoReap`, `restoreDroppedLink` → boardsync (exported: Lanes, LaneFor,
                 Classify, Discover, Inventory, Apply, Undo, Plan, Card, Verdict). issue_reap_cmd.go
                 keeps `issueReap()`, `printReapPlan`, `boardNamespaceNames`,
                 `validateReapNamespaces` (printing + flag parsing). doctor.go's stranded-
                 mirrors section calls boardsync.Inventory. Doneness goes through phase.Done
                 (t-2), never a local re-read of changes.json.
       depends_on: [t-6]
       test_contract:
         - Partial-failure accounting: Apply over a plan of 3 cards where the fake client
           fails the 2nd returns a *reapFailure naming that key, the 1st card's relabel is
           recorded in the reap log, and the 3rd was NOT attempted (moved from
           issue_reap_apply_test.go; the assertion is on the log's card count == 1).
         - Undo restores only what it dropped: after Apply drops a Backlog link, Undo
           re-links it; a Phase-lane card (lanesDroppingTheirLink false) is relabelled back
           without touching board.json (issue_reap_undo_test.go, moved).
         - Orphan identity is label-derived, not title-derived: an issue carrying
           `dross/phase:x` with no board.json link classifies as orphan kind=phase; one
           carrying only the marker label is unclassifiable and lands in the second return
           (issue_reap_discover_test.go, moved).
         - cmd e2e: issue_reap_cmd_test.go (dry-run-by-default plan rendering, `--undo`+`--apply` conflict text, namespace flag
           validation error text) passes with no assertion edits.

  t-8  Extract structured diagnostics to internal/doctor
       files:    internal/doctor/line.go (new), internal/doctor/redproof.go (new),
                 internal/doctor/roadmap.go (new), internal/doctor/combinations.go (new),
                 internal/doctor/configtrust.go (new), internal/doctor/laneconsent.go (new),
                 internal/doctor/toolchain.go (new), internal/doctor/*_test.go (moved),
                 internal/cmd/doctor.go, internal/cmd/redproof.go, internal/cmd/doctor_test.go,
                 internal/cmd/doctor_lane_toolchain_test.go, internal/cmd/redproof_test.go,
                 internal/cmd/redproof_lifecycle.go, internal/cmd/redproof_repoint.go
       covers:   c-3
       description:
                 `doctorLine{level,text}` becomes doctor.Line{Level, Text} with the three
                 level consts; EVERY check returns []Line (or a typed slice) and PRINTS
                 NOTHING. Move: `redProofChecks`, `redProofPinLines`, `redProofRepointHint`,
                 `sameCommitSHA` + redproof.go's `reachability`, `classifyReachability`,
                 `redProofPin`, `discoverRedProofPins`, `redProofDocSHA`, `redProofSHA`;
                 `duplicateRoadmapSlug(s)`; `remoteCombinationWarnings`,
                 `boardCombinationWarnings`, `sortedStateMapKeys`; `checkConfigTrust` (its
                 Printf's become Lines; the exec-consent arm reads consent.CheckConsent, the
                 allowlist arm reads localstore.ReadAllowHosts, refs via
                 argfence.ValidateGitRef); `reportLaneConsent` (→ Lines over
                 consent.LaneConsented); `checkMutationToolchain` (→ Lines over
                 mutationcfg.MissingLocalTools). Git access for red-proof reachability and
                 the repoint hint's fork point is an interface `doctor.Git{Trim(dir string,
                 args ...string) (string, error); NoOut(dir string, args ...string) error}`
                 that cmd satisfies with its existing gitTrim/gitNoOut (the spawn sites and
                 their exempt markers do not move; internal/doctor imports no os/exec).
                 `phaseForkPoint` stays in cmd and is passed as a `func(phase string)
                 (string, error)` field on the red-proof check input. Doctor() in cmd becomes:
                 gather inputs → call each check → print Lines with the existing glyph per
                 level → tally issues/warnings exactly as today. Leaked-commit, git-version,
                 backfill-residue, stranded-mirror sections are not in c-3 and stay as-is.
                 Unit tests move: duplicate-slug ordering, combination-warning tables,
                 red-proof pin verdict arms, config-trust ref validation; e2e doctor tests
                 (captureStdout over `dross doctor`, exit codes) stay.
       depends_on: [t-2, t-4, t-5]
       test_contract:
         - Printing is only in cmd: doctor/*_test wraps os.Stdout in a pipe around every
           check call and fails if a single byte is written — a Printf left behind in a
           moved check fails here, not in a snapshot diff.
         - Every reachability arm is deterministic with a fake Git: shallow-repo "true" →
           Indeterminate; no origin refs → Indeterminate; rev-parse ^{commit} fails →
           UNREACHABLE (never Indeterminate — the post-gc case); for-each-ref --contains
           empty → Unreachable; otherwise Reachable naming the ref. The shallow arm has no
           real-git test today; this is new coverage the interface buys.
         - Escaping doc suppresses every pin: discoverRedProofPins with one changes.json
           whose red_proof.doc is `../x` returns the pathfence error and ZERO pins, and
           redProofChecks returns exactly one Issue line whose text does not contain
           "could not be read" (the pinned wording distinction, moved from redproof_test.go).
         - Level tally is preserved end-to-end: doctor_test.go's exit-code tests (one issue
           → exit 1; warnings only → exit 0) and the "Combinations:" block text pass with no
           assertion edits — the composition in cmd counts Issue lines into `issues` and
           Warn lines into `warnings` exactly as the inline code did.
         - Lane consent stale is an issue, absent is a warning: doctor/laneconsent_test
           proves a lane whose stored fingerprint mismatches yields Level=issue and a lane
           with no grant yields Level=warn (moved from trust_lane_test.go's doctor section).

Wave 4 (depends t-4, t-5, t-6, t-7, t-8)

  t-9  Add import-direction architecture test with ratchet
       files:    internal/cmd/import_boundary_test.go (new),
                 internal/cmd/testdata/import_boundary/exec_baseline.txt (new)
       covers:   c-5
       description:
                 A go/parser walk (test file, so not itself subject to the ratchet) over
                 every directory under internal/ containing non-test .go files, building
                 pkg → imports. Asserts: (1) `github.com/spf13/cobra` is imported by
                 internal/cmd and by no other internal/ package; (2) internal/cmd imports
                 each of internal/consent, internal/boardsync, internal/doctor,
                 internal/mutationcfg (fails naming the missing one); (3) no internal/
                 package other than cmd imports internal/cmd; (4) ratchet: every non-test
                 internal/cmd file importing os/exec, net/http or go/ast is in the baseline
                 file, every baseline entry still imports one of them (stale entry → fail
                 with "remove it"), and len(baseline) <= a `execBaselineCeiling` const set
                 to the observed count at commit time (the count is MEASURED from the tree
                 in this task, not copied from the spec's "~13"). (5) vacuity floor: the walk
                 found >= 25 packages and >= 300 files in internal/cmd, and found at least
                 one cobra importer.
       depends_on: [t-4, t-5, t-6, t-7, t-8]
       test_contract:
         - Adding a file to the baseline fails: a sub-test feeds the checker the real
           baseline plus one extra existing cmd file and asserts the ceiling error; feeding
           it the baseline minus one entry that still imports os/exec asserts the
           "not in baseline" error names that file; feeding it an entry whose file no
           longer imports any forbidden package asserts the stale error.
         - Reverse-import is caught, not assumed: a sub-test runs the checker over a
           synthetic tree (t.TempDir) where a fake `internal/consent/x.go` imports
           internal/cmd and asserts the failure names both packages; the same harness with
           a fake `internal/foo` importing cobra asserts the exclusivity failure.
         - The floor bites: the checker over an empty temp root returns the vacuity error
           rather than passing with zero packages.
         - Live tree: the test is green on this repo at commit, with the baseline file
           listing exactly the observed os/exec/net/http importers and nothing else.

  t-10 Prove c-6: test-set conservation and unchanged cmd assertions
       files:    internal/cmd/import_boundary_test.go, internal/cmd/testdata/import_boundary/moved_tests.txt (new)
       covers:   c-6
       description:
                 Adds one test to the boundary file: a manifest of every `func Test*` name
                 that t-4..t-8 moved out of internal/cmd, each paired with its new package;
                 the test parses both packages and asserts each name exists in the new
                 package and NOT in cmd (copied-not-moved or dropped both fail). The task
                 also runs the whole suite via `dross test` (remote lane, never --local per
                 project memory) and records the observed green in the commit body. Before
                 committing, `git diff <phase-base> -- 'internal/cmd/*_test.go'` is
                 inspected: permitted hunks are import lines, package-qualifier renames on
                 identifiers, and seam-stub re-points (`remote.ProbeFn =`,
                 `mutationcfg.LookPath =`); any hunk touching a line containing t.Error,
                 t.Fatal, want, or a quoted expected string is a c-6 violation to fix, not
                 merge.
       depends_on: [t-9]
       test_contract:
         - Moved-test manifest: for every (name, pkg) row, `go test -list '^name$'
           ./internal/<pkg>` lists it and `go test -list '^name$' ./internal/cmd` does not;
           a row whose test was silently dropped during a move fails by name.
         - Manifest cannot be vacuous: it must contain >= 40 rows (the moves in t-4..t-8
           account for well over that), else the test fails as "manifest too small to prove
           anything".
         - Whole-suite observation is the gate for the commit: `dross test` green output is
           read before the commit is queued (execution rule 2), and the commit message
           states the run it observed.

## Coverage

- c-1 → t-1 (store seam), t-4 (consent package; cmd keeps trust tree + requireExecConsent)
- c-2 → t-2 (phase readers), t-6 (backlog/phase/milestone/task sync + task pull), t-7 (reap)
- c-3 → t-2 (ValidateGitRef, Done), t-8 (structured checks; Doctor() composes/prints)
- c-4 → t-1 (grants/tuning readers), t-3 (pool/preflight), t-5 (mutationcfg; verify + doctor + drain consume one path)
- c-5 → t-9
- c-6 → t-10 (plus every task's "no assertion edits" contract)

## Judgment calls

- Seam through the deferred local.go domain: moved the WHOLE typed `localStore` struct
  plus the trust-bearing readers (grants, tuning, allow-hosts) into internal/localstore;
  rejected a consent-only partial struct because BurntSushi encodes only the fields it
  knows — a Save from a partial struct silently deletes every other key (detached runs,
  remote host, quick_base). The clobber-guard test in t-1 is the proof. Rejected
  injecting a store interface into consent/mutationcfg because two injection sites is a
  second construction path, the drift c-4 exists to remove. What stays deferred is
  exactly what the spec lists: get/set cobra, localKeys, detached runs, effectiveRemote.
- mutationcfg imports localstore and stack in addition to the three the locked decision
  names. The decision's load-bearing half is "internal/mutation stays project-agnostic",
  which holds; naming the extra two here so the judge can reject rather than discover.
- Red-proof git access goes through a 2-method interface satisfied by cmd's gitTrim/
  gitNoOut rather than moving git plumbing out of cmd. Rejected moving gitTrim/gitNoOut/
  gitRefArgs (≈30 cmd files, the deferred baseline-drain) and rejected duplicating the
  fence in internal/doctor (a copy of an argv fence drifts). Doctor has ONE consumer, so
  injection cannot fork a path here; and it makes every reachability arm — including the
  shallow-clone arm no real-git test covers today — deterministic.
- Wrappers of the old name kept in cmd for `validateGitRef`, `phaseDone`,
  `collectDeferred`, `selectRemoteTarget`, `configuredAdaptersFn`: chosen so ~40 call
  sites and the by-name audits (subprocargs, exec-consent) are untouched. Rejected
  bulk-renaming call sites because every rename is a chance to bind a test stub to the
  wrong seam, which the remote_bootstrap tests showed once already.
- Seam vars: `remoteProbeFn` → `remote.ProbeFn`, `execLookPath` → `mutationcfg.LookPath`.
  Seven cmd test files change a stub line. I read c-6's "no assertion edits" as
  permitting stub-setup and qualifier edits; the t-10 diff inspection enforces that
  reading. Rejected keeping cmd-level vars that the packages call back into — that is
  an extracted package importing cmd, which c-5 forbids.
- Ratchet excludes _test.go files. The audit tests themselves import go/ast and several
  tests import os/exec; the boundary protects domain logic, and the spec's "18 files"
  count is the non-test count. Stale baseline entries FAIL (not merely pass) so the list
  tracks reality and the ceiling is the actual ratchet; the swap loophole (remove A, add
  B, same length) is inherent to any hand-edited allowlist and is stated, not hidden.
- The boundary test lives in internal/cmd/import_boundary_test.go beside the other
  repo-wide audits (same repoRootForDocs walk), not in internal/architecture, which is
  the ARCHITECTURE.md generator and would be a misleading home.
- Board output goes through Ctx.Out resolved at call time with os.Stdout fallback.
  Rejected a package-level writer initialized to os.Stdout: captureStdout swaps the
  variable after init, so every cmd e2e assertion on sync output would read "".
- issueQuick / pull / dismiss / link / list stay in cmd. The criterion names backlog/
  phase/milestone sync, reap and task pull; quick-mirror sync is thin enough to leave,
  and pulling it would widen t-6 past the 5-file rule.
