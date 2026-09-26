# cmd-exec-baseline-drain — verification-lens draft

Lens: each criterion's ideal test contract was written first (see ## Coverage). Every
task below is the smallest change that makes one of those contracts satisfiable while
leaving the tree green on its own.

Phase cmd-exec-baseline-drain — 22 tasks across 5 waves

Standing contracts (hold at the end of every task from the one that arms them;
each task below adds its own specific lines):
  S1 (c-4)  TestEverySpawnSiteGatedOrExempt and TestNoSpawnOutputEscapes are green after
            the task. A moved marker that clears nothing is itself a finding in the second.
  S2 (c-4)  TestExecExemptBudget (armed in t-2) holds: parsed //dross:exec-exempt directives
            under internal/ and cmd/ are <= the phase-start count.
  S3 (c-1/2/8) TestCmdBoundaryByImportDirection is green. A task that drains a file removes
            that file's baseline entry in the same commit (the stale-entry rule fails otherwise),
            and a new importer fails.
  S4 (c-7)  TestCLITreeGolden (armed in t-1) is byte-identical and TestNoTestLost is green
            against the phase-start inventory. cmd end-to-end tests change setup identifiers
            at most, never an assertion. Where a lot of setup would change, a _test.go shim
            is used instead, as consent_shim_test.go already does.
  S5 (floors) If a task's census drops under execConsentMinSites/MinFiles, execSourceFloor/
            execSourceFileFloor, gitTrimSiteFloor or gitHelperSiteFloor, that task resets the
            floor to floor(0.75 x the count the test logs) and records before→after in the
            comment. The floor self-tests (the minimum passes, one under fails),
            TestDroppingAScanRootFailsTheFloor and the "internal/cmd alone must not meet the
            source floor" check stay green. The floors are never lowered ahead of the census.

Wave 1
  t-1  Pin full CLI tree and test inventory
       desc:     Mint a whole-tree `dross` surface golden from newRoot() before anything moves.
                 Re-mint tests_before.txt at phase start, running only TestTestNamesRecorded under
                 DROSS_UPDATE_GOLDEN=1.
       files:    cmd/dross/surface_test.go (NEW), cmd/dross/testdata/cli_tree.txt (NEW),
                 internal/cmd/testdata/cli_surface/tests_before.txt
       covers:   c-7
       contract: renaming any flag, changing a default/usage/hidden bit, or dropping any subcommand
                 anywhere under `dross` (e.g. `local get`, `update --check`, `env unset`) makes
                 TestCLITreeGolden fail with a diff naming the command. A missing golden fails
                 and does not pass vacuously.
       contract: the re-minted inventory is a superset of the old one (`comm -23 old new` is
                 empty). Deleting a test that exists today but not in the old inventory (any
                 exec-taint-enumeration test) now fails TestNoTestLost naming it.

  t-2  Arm codec ratchet and exec-exempt budget
       desc:     Add encoding/json and github.com/BurntSushi/toml to forbiddenInCmd, with baselines
                 listing today's 14 json and 6 toml importers. Make the stale-entry self-test pick
                 a live baseline entry instead of the hard-coded cleantree.go. Add the
                 exec-exempt budget test.
       files:    internal/cmd/boundary_test.go, internal/cmd/exempt_budget_test.go (NEW)
       covers:   c-4, c-8, c-6
       contract: a synthetic issue.go importing encoding/json gives exactly one finding naming
                 issue.go and encoding/json. The same holds for toml. A baselined codec file with
                 its import removed gives one "remove it" finding.
       contract: the "dropped import fails as stale" subtest names the entry it picked. Draining
                 cleantree.go (t-16) no longer breaks this self-test.
       contract: TestExecExemptBudget counts through directiveMarkers, not grep. On a synthetic
                 tree with 2 directives and 1 prose `// //dross:exec-exempt` it counts 2, and a
                 budget of 1 fails with "2 > 1". The live budget is the phase-start parse. That
                 should be 27: the 28 grep hits include trust.go:124's prose, which the grammar
                 rejects.

Wave 2 (each depends t-1, t-2)
  t-3  Move update flow into internal/update
       desc:     runUpdate (fetch, minisign verify, checksum, extract, swap, self-exec resync),
                 extractBinary* and the *http.Client seam become update.Apply(ctx, Options).
                 cmd/update.go keeps only flags. The update tests move with the code.
       files:    internal/update/apply.go (NEW), internal/update/apply_test.go (NEW, moved from
                 internal/cmd/update_test.go), internal/cmd/update.go, internal/cmd/update_test.go,
                 internal/cmd/boundary_test.go, internal/cmd/execconsent_audit_test.go,
                 internal/cmd/subprocargs_audit_test.go
       covers:   c-1, c-2
       contract: the 8 moved tests pass in internal/update with identical assertions:
                 TestUpdateRefusesOnMissingSignature, TestUpdateRefusesOnWrongKeySignature,
                 TestUpdateRefusesOnBadChecksum and TestUpdateAppliesAndResyncs, among others.
                 Resyncing before the signature gate, or swapping on a bad checksum, fails one
                 of them.
       contract: TestUpdateSelfExecIsTheOnlyMarkerHere, retargeted to internal/update/apply.go,
                 finds exactly 1 marked site whose reason names verification. The
                 acceptedNonLiteralBinaries key becomes "apply.go:newBinary". S2 is unchanged
                 because the marker moved.
       contract: update.go leaves both the os/exec and net/http baselines. Re-adding either
                 import fails S3.

  t-4  Move suite and slot spawns to testlane
       desc:     runLocalCommandCtx's `sh -c` spawn, runRemoteCommand's streaming ssh/rsync spawn,
                 runSlotCommand's spawn and laneLookPath's exec.LookPath move to
                 internal/testlane/spawn.go as a 1:1 move of three sites. cmd keeps the seam vars,
                 shArgvFor, and every consent call.
       files:    internal/testlane/spawn.go (NEW), internal/testlane/spawn_test.go (NEW),
                 internal/cmd/test.go, internal/cmd/run.go, internal/cmd/boundary_test.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/subprocargs_audit_test.go,
                 internal/cmd/taint_usercmds_test.go
       covers:   c-1
       contract: execConsentGatedFiles lists internal/testlane/spawn.go in place of test.go and
                 run.go. TestToolchainSpawnsResolveAsGated sees its 3 sites as execReachGated
                 and unmarked. Adding a marker to ease the move is refused as "needs no
                 exemption".
       contract: TestReachProofIsLoadBearing, retargeted: ungating run.go's
                 `consent.RunConsented(` call makes the slot spawn in testlane/spawn.go a finding.
                 This proves the consent call stayed in cmd and the spawn sits behind it
                 (locked gated_spawn_homes).
       contract: TestStreamSitesCarryNoMarker scans testlane/spawn.go, because streamSiteFiles
                 moves with the streams. A taint-cleared marker placed there is reported, so the
                 guard does not go vacuous.
       contract: in testlane, RunSlot given a nil *os.File leaves the child's Stdin unset: a
                 `cat` child exits 0 on EOF instead of failing on a typed-nil reader.
                 TestSpawnLocalCtxCancels and TestRunSlotStillRefusesAtRuntime stay green
                 unchanged.
       contract: "spawn.go:argv[…]" replaces "test.go:argv[…]" in acceptedNonLiteralBinaries.
                 test.go and run.go leave the os/exec baseline.

  t-5  Move lane-install spawn; drop ExitError types
       desc:     runInstallLocally's CombinedOutput spawn moves to internal/testlane/install.go,
                 which returns the output. The cmd wrapper still prints to installStderr.
                 redproof_replay's *exec.ExitError match becomes the existing exitCoder interface.
       files:    internal/testlane/install.go (NEW), internal/cmd/lane_install.go,
                 internal/cmd/redproof_replay.go, internal/cmd/boundary_test.go,
                 internal/cmd/execconsent_audit_test.go
       covers:   c-1
       contract: TestLaneInstallOutputGoesToStderr (a named guard) passes unchanged: CANARY-LANE
                 is in stderr and absent from the error. TestLaneInstallReusesTheRemoteSeam
                 still finds remoteExecFn in lane_install.go.
       contract: TestReplayRedVsGreen: `exit 3` still yields Red with ExitCode 3. If the
                 exitCoder match missed real exit errors, the replay would report "could not be
                 run" and this test fails.
       contract: internal/testlane/install.go replaces lane_install.go in
                 execConsentGatedFiles, and its site is gated and unmarked. lane_install.go and
                 redproof_replay.go leave the os/exec baseline.

  t-6  Move drain spawns and payload decode to survivor
       desc:     goListDirs' `go list` spawn, together with its taint-cleared marker, and
                 runCoverageProfile's `go test -coverprofile` spawn move to
                 internal/survivor/drain.go. So do rawGremlinsPayload and notCoveredPositions.
                 cmd keeps the seam vars, requireExecConsent and the verify.IsTestdataPath call.
       files:    internal/survivor/drain.go (NEW), internal/survivor/drain_test.go (NEW),
                 internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go,
                 internal/cmd/boundary_test.go, internal/cmd/execconsent_audit_test.go
       covers:   c-1, c-8
       contract: the new TestDeletingDrainsGateFlagsItsSpawns ungates survivor_drain.go's
                 `requireExecConsent()`, and both sites in internal/survivor/drain.go become
                 findings. Without that surgery, TestToolchainSpawnsResolveAsGated sees both
                 sites as gated and unmarked.
       contract: the moved notCoveredPositions tests (names preserved) pass in survivor. A
                 "NOT COVERED" mutant yields its position key, and malformed JSON returns
                 "read mutant statuses".
       contract: TestDrainHasNoLocalTestdataRule still finds `verify.IsTestdataPath(` in
                 survivor_drain.go. survivor_drain.go leaves the os/exec and encoding/json
                 baselines.

  t-7  Move verify detach spawn to internal/verify
       desc:     runDetachArgv's rsync spawn moves to internal/verify/detach.go, and the seam var
                 stays in cmd. classifyFetch's *exec.ExitError match becomes the exitCoder
                 interface.
       files:    internal/verify/detach.go (NEW), internal/cmd/verify.go,
                 internal/cmd/verify_results_test.go, internal/cmd/boundary_test.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go
       covers:   c-1
       contract: the new TestClassifyFetchReadsARealExitStatus: a real exit error from
                 `sh -c 'exit 23'` classifies as exactly `remote.Classify("rsync", host, 23)`.
                 Today that arm of classifyFetch has no direct test. If the interface stopped
                 matching real exit errors, classifyFetch would return the raw error and this
                 test fails.
       contract: the new companion to TestDeletingVerifysGateFlagsEveryMutationSpawn: ungating
                 verify.go's requireExecConsent anchor flags the detach site in
                 internal/verify/detach.go. The detach site is also gated and unmarked in
                 TestToolchainSpawnsResolveAsGated.
       contract: TestUserCommandFixKeepsTheSurgeryAnchors: the verify.go anchor still lands.
                 TestStreamSitesCarryNoMarker covers detach.go. verify.go leaves the os/exec
                 baseline.

  t-8  Introduce leaf gitrun behind git helpers
       desc:     New leaf package internal/gitrun with exactly four spawns, one per verb:
                 - Trim: pinned ref verbs. Carries exec-exempt and the moved taint-cleared marker.
                 - Raw: untrimmed content, no taint marker. Read is TrimSpace(Raw).
                 - Run: CombinedOutput. On failure, git's output goes to gitrun.Stderr.
                 - Quiet: exit status only.
                 It also exports ArgvRecorder. gitTrim, gitRead, gitRun and gitNoOut become
                 one-line delegates. gitStderr, gitArgvRecorder and gitVerb move into gitrun.
       files:    internal/gitrun/gitrun.go (NEW), internal/gitrun/gitrun_test.go (NEW),
                 internal/cmd/ship_recover.go, internal/cmd/phase.go,
                 internal/cmd/taint_gitplumbing_test.go, internal/cmd/boundary_test.go.
                 Setup-only edits: internal/cmd/ship_test.go, internal/cmd/switchbranch_test.go,
                 internal/cmd/basebranch_test.go, internal/cmd/gitseparator_test.go,
                 internal/cmd/verifyscope_test.go
       covers:   c-3, c-4
       contract: gitrun unit tests run against a stub git on PATH:
                 - a failing Run's error lacks CANARY, and Stderr holds "git <verb>:" plus CANARY
                 - a failing Quiet puts CANARY in neither the error nor Stderr
                 - Raw keeps a leading " M" status column, and Read trims it
                 - every verb's argv starts with `-C <dir>` and reaches ArgvRecorder
       contract: new leaf rule in checkBoundary: an internal/gitrun that imports any
                 github.com/Rivil/dross package is a finding. A synthetic gitrun importing
                 internal/consent gives exactly one finding. extractedPackages gains gitrun.
       contract: the new TestGitrunContentVerbsCarryNoMarker: no taint-cleared marker binds a
                 line inside Raw or Read. TestGitRunGateIsScopedByOrigin, retargeted, finds Run's
                 and Trim's spawn lines in internal/gitrun on distinct lines, and keeps only the
                 Run-origin finding.
       contract: S2 is unchanged: 4 markers leave cmd and 4 land in gitrun.
                 TestShipRecoverFetchFailureOutputGoesToStderr (a named guard) passes with its
                 setup swapping gitrun.Stderr. gitseparator_test's argv-order check passes through
                 gitrun.ArgvRecorder.

  t-9  Teach argv and pin audits gitrun calls
       desc:     subprocargs spawnArgvOf gains the `gitrun.<Verb>(dir, argv...)` selector form
                 next to the ident form, and gitHelperSiteFloor counts per verb across both forms.
                 gitTrimProblems also matches `gitrun.Trim(` and walks every package instead of
                 internal/cmd only.
       files:    internal/cmd/subprocargs_audit_test.go, internal/cmd/taint_gitplumbing_test.go
       covers:   c-3, c-4
       contract: new snippet rows: `gitrun.Quiet(repoDir, "rev-parse", branch)` is flagged
                 (bare positional with no separator). `gitrun.Quiet(repoDir, "rev-parse",
                 "refs/heads/"+branch)` is not flagged. If the selector case were missing, the
                 first row would stop firing.
       contract: new TestGitTrimRunsRefVerbsOnly fixture rows: `gitrun.Trim(dir, "log",
                 "--oneline")` trips naming `log`. `gitrun.Trim(dir, "remote", "get-url",
                 "origin")` trips, so a URL cannot be laundered through Trim's marker. Each row
                 examines exactly 1 site. Live counts stay at or above today's floors.

  t-10 Add render package; route TOML output
       desc:     New internal/render provides JSON(w, v) (indented encoder plus newline),
                 MarshalJSON, MarshalJSONIndent and TOML(w, v). The five TOML `show` emitters use
                 render.TOML. stack.go's explicit Indent collapses into the default.
       files:    internal/render/render.go (NEW), internal/render/render_test.go (NEW),
                 internal/cmd/defaults.go, internal/cmd/milestone.go, internal/cmd/profile.go,
                 internal/cmd/project.go, internal/cmd/stack.go, internal/cmd/boundary_test.go
       covers:   c-8
       contract: differential tests use a value with nested tables, omitempty and "<&>" strings.
                 Each render function must produce the same bytes as the call it replaces:
                 - render.TOML equals toml.NewEncoder (default), and also equals the encoder with
                   Indent "  ", which proves stack.go's collapse is byte-safe
                 - JSON equals json.Encoder with SetIndent("", "  ")
                 - MarshalJSON equals json.Marshal, including HTML escaping
                 - MarshalJSONIndent equals json.MarshalIndent
       contract: `project/milestone/profile/defaults/stack show` e2e tests pass unchanged. The 5
                 files leave the toml baseline, and extractedPackages gains render.

  t-11 Extract local.toml store to localstore
       desc:     New internal/localstore takes over from local.go:
                 - the Store type and its toml codec
                 - the keys table and detached-run records
                 - allow-hosts, remote-grant and remote-env reading
                 - mutation tuning
                 - the consent grant-store adapter
                 local.go keeps Local/localGet/localSet plus thin forwarders, which t-15 deletes.
                 resolveRemoteHost (probe orchestration) moves beside probeRemotePool.
       files:    internal/localstore/store.go (NEW), internal/localstore/store_test.go (NEW),
                 internal/cmd/local.go, internal/cmd/local_test.go,
                 internal/cmd/local_detached_test.go, internal/cmd/local_remote_alias_test.go,
                 internal/cmd/remote_pool.go, internal/cmd/boundary_test.go
       covers:   c-5, c-8
       contract: the store-level tests move to localstore with identical assertions, for example
                 TestDetachedRunRoundTripsEveryField, TestSecondRunForOnePhaseIsRefused,
                 TestNewKeyWinsOverAlias, TestRemoteGrantPairResolvesTogether,
                 TestUnreadableStoreIsNotASilentLocalRun, TestLocalStoreRoundTripsGrants and
                 TestReadRemoteGrantRefusesTrackedLocal. TestNoTestLost counts them as moved,
                 not copied.
       contract: the new boundary rule fails on any non-test internal/cmd struct with a `toml:`
                 field tag. Its self-test uses a synthetic cmd file: `type x struct{ A string
                 \`toml:"a"\` }` gives one finding naming the file and x, and a json-only struct
                 gives none. extractedPackages gains localstore, so deleting the feature cannot
                 satisfy c-5.
       contract: TestLocalSetGetRoundTrips, TestLocalRejectsUnknownKey and
                 TestRemoteGrantKeysAreNotSettable pass unchanged, so grant keys are still not
                 settable through `local set`. local.go leaves the toml baseline.

  t-12 Move settings.json codec out of cmd
       desc:     env.go's readSettings/mutateSettings (0600, atomic, trailing newline) move to the
                 new internal/claudesettings. statusline.go's existingStatusLineCommand moves to
                 statusline.ExistingCommand.
       files:    internal/claudesettings/settings.go (NEW), internal/claudesettings/settings_test.go
                 (NEW, receives TestMutateSettingsCreatesFile and
                 TestMutateSettingsPreservesUnrelatedKeys), internal/cmd/env.go,
                 internal/cmd/env_test.go, internal/cmd/statusline.go,
                 internal/statusline/settings.go, internal/cmd/boundary_test.go
       covers:   c-8
       contract: the moved tests pass, and assert mode 0600 plus the trailing newline (add the
                 assertion if it is missing). TestEnvListMasksValues and
                 TestEnvCover_UnsetPropagatesMutateError pass unchanged. A malformed
                 settings.json still makes `env unset` fail.
       contract: statusline.ExistingCommand returns "x" for {"statusLine":{"command":"x"}} and ""
                 for malformed JSON, matching today's swallowed error. The clobber refusal still
                 names the existing command. env.go and statusline.go leave the encoding/json
                 baseline.

Wave 3
  t-13 Replace git helper calls with gitrun (depends t-8, t-9)
       desc:     About 164 call sites of gitTrim, gitRead, gitRun and gitNoOut in 29 cmd files
                 become gitrun.Trim, gitrun.Read, gitrun.Run and gitrun.Quiet (grep finds 168
                 `gitX(` occurrences, which include the 4 definitions), and the delegates are
                 deleted. A test shim keeps the 12 cmd test files that call the helpers for setup
                 unchanged. The audits drop the ident form.
       files:    internal/cmd/{basebranch,cleantree,doctor,forkpoint,milestone,milestone_merged,
                 milestone_stale,phase_backfill,originpush,phase_lifecycle,phase,phase_reconcile,
                 phase_checkout,redproof_replay,redproof_lifecycle,redproof,redproof_repoint,
                 redproof_set,repair_files,repair_state,repair_phasedirs,repair,ship,secretscan,
                 ship_recover,status,switchbranch,topology,verifyscope}.go,
                 internal/cmd/git_shim_test.go (NEW), internal/cmd/subprocargs_audit_test.go,
                 internal/cmd/taint_gitplumbing_test.go
       covers:   c-3
       contract: TestGitHelperCallSiteFloor, keyed by gitrun verb, stays at or above today's
                 floors (Run 29, Trim 30, Read 8, Quiet 30). If any call site were left out of
                 the audit, its verb count would drop and the test fails.
       contract: TestGitTrimRunsRefVerbsOnly examines at least 30 gitrun.Trim sites repo-wide
                 with zero problems. No non-test FuncDecl named gitTrim, gitRead, gitRun or
                 gitNoOut remains under internal/. The final census in t-22 enforces this.
       contract: the 12 setup-calling test files are byte-identical. Of the 14 files that grep
                 matches, the other two are the audits themselves. `git diff --stat` shows only the
                 29 production files, the shim and the 2 audits.

  t-14 Route JSON output through render (depends t-10)
       desc:     The json.Marshal, MarshalIndent and Encoder output sites switch to render:
                 changes, deferred, dotget, jsonout (emitJSON becomes a render.JSON delegate),
                 reentry, ship, state, task, verifyscope and watch.
       files:    internal/cmd/changes.go, internal/cmd/deferred.go, internal/cmd/dotget.go,
                 internal/cmd/jsonout.go, internal/cmd/reentry.go, internal/cmd/ship.go,
                 internal/cmd/state.go, internal/cmd/task.go, internal/cmd/verifyscope.go,
                 internal/cmd/watch.go, internal/cmd/boundary_test.go
       covers:   c-8
       contract: the json_show_test, json_show_phase_test, json_show_task_test and
                 lane_preview_json_test suites pass unchanged, as do the ship --json, dotget,
                 task list --json and reentry tests. The byte identity of each swapped call is
                 already proven by t-10's differential tests.
       contract: all 10 files leave the encoding/json baseline. Re-adding `"encoding/json"` to
                 any of them fails S3.

  t-15 Rewire cmd onto localstore; shrink local.go (depends t-11)
       desc:     The 20 non-test cmd callers call localstore directly and local.go's forwarders
                 are deleted. The cmd test identifiers (grantStore, detachedRun,
                 recordDetachedRun, localKeys and others) move into a test-only shim, following
                 the consent_shim_test.go precedent. The two run.go surgery anchors are updated.
       files:    internal/cmd/local.go, internal/cmd/local_shim_test.go (NEW),
                 internal/cmd/{trust,verify,remote_grant,test,remote_pool,test_lane_install,doctor,
                 remote_bootstrap_cmd,issue,gitignore,ship,run,remote_bootstrap,redproof_replay,
                 lane_preview_locality,lane_preview,lane_locality,lane_install,basebranch,
                 test_lane}.go, internal/cmd/execconsent_audit_test.go,
                 internal/cmd/boundary_test.go
       covers:   c-5, c-7
       contract: the new TestLocalGoIsOnlyTheCommandTree parses internal/cmd/local.go and fails on
                 any type, var or const declaration, or on any func that does not return
                 *cobra.Command. A forwarder left behind fails it by name.
       contract: execLiveSurgeries and TestReachProofIsLoadBearing use a rename-stable anchor,
                 `consent.RunConsented(`. TestSurgeryLeavesTheSharedGraphAlone and
                 TestUserCommandFixKeepsTheSurgeryAnchors still land every surgery.
       contract: cmd e2e test files other than the new shim are byte-identical, and S4 holds.

  t-16 Route porcelain and ls-files reads via gitrun (depends t-8, t-9)
       desc:     Four files move off direct git spawns:
                 - cleantree: gitStatusRaw is replaced by gitrun.Raw plus TrimRight
                 - pause: symbolic-ref goes through gitrun.Trim, and status through Raw
                 - worktree_files: status --untracked-files=all goes through Raw
                 - techdebt: ls-files -z goes through Raw
                 Taint-cleared markers on Raw consumers stay. pause.go:89's marker becomes
                 redundant under Trim's own marker and is deleted.
       files:    internal/cmd/cleantree.go, internal/cmd/pause.go, internal/cmd/worktree_files.go,
                 internal/cmd/techdebt.go, internal/cmd/boundary_test.go
       covers:   c-1, c-3
       contract: TestNoSpawnOutputEscapes stays green (S1). Keeping pause.go:89's now-redundant
                 marker would be reported as "clears nothing". Deleting a Raw-consumer marker, such
                 as cleantree's porcelain line, would report the escape.
       contract: the existing cleantree, pause and worktree_files tests pass unchanged, including
                 a rename line and a leading " M" first line, which only an untrimmed Raw
                 preserves. S2 drops by 5. The 4 files leave the os/exec baseline, and S5 applies
                 if the file floors cross.

  t-17 Route statusline branch read via gitrun (depends t-8, t-9)
       desc:     gitBranchTrim becomes a gitrun Trim variant with a 2s timeout and
                 --no-optional-locks, sharing Trim's single spawn, which switches to
                 CommandContext. The rev-parse pin gains --short. The taint spawn-line finders
                 accept CommandContext.
       files:    internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go,
                 internal/cmd/statusline.go, internal/cmd/statusline_test.go (setup line 132),
                 internal/cmd/taint_gitplumbing_test.go, internal/cmd/taint_scanners_test.go,
                 internal/cmd/boundary_test.go
       covers:   c-1, c-3
       contract: new gitrun tests: a stub git that sleeps 5s makes the variant return an error
                 within 3s, and the recorded argv starts with `--no-optional-locks`. Existing
                 statusline tests still give "main" on a branch, the short SHA when detached, and
                 "" outside a repo.
       contract: gitHelperSpawnLines and spawnLineIn still find Trim's spawn after it becomes
                 CommandContext. Without the finder fix, TestGitRunGateIsScopedByOrigin fails
                 fatally with "found no exec.Command".
       contract: TestGitTrimRunsRefVerbsOnly accepts `rev-parse --short` and still rejects
                 `--pretty`. The count of Trim spawns in internal/gitrun stays 1. statusline.go
                 leaves the os/exec baseline, and S2 drops by 1.

  t-18 Route init remote URL; drop LookPath (depends t-8, t-9)
       desc:     seedRemote reads origin through gitrun.Read, a content verb that is never Trim,
                 so the raw URL stays tainted. exec.LookPath in init.go and stack.go is replaced
                 by stack.SystemLookPath.
       files:    internal/cmd/init.go, internal/cmd/stack.go, internal/stack/runtime.go,
                 internal/cmd/taint_gitdirect_test.go, internal/cmd/boundary_test.go
       covers:   c-1, c-3
       contract: TestRemoteURLUserinfoNeverPersists (a named guard) passes: CANARY-TOK never
                 reaches the detected remote. TestInitRawRemoteURLCarriesNoMarker, retargeted to
                 seedRemote, finds the gitrun.Read call and no taint-cleared marker binding
                 inside it.
       contract: the existing seedRuntimeFromProfile and stack loadout tests pass through
                 stack.SystemLookPath. init.go and stack.go leave the os/exec baseline, and S2
                 drops by 1 (init.go's marker).

  t-19 Collapse three ShortSHA copies into gitrun (depends t-8, t-9)
       desc:     techdebt, quality and security ShortSHA/normalizeSHA become one
                 gitrun.ShortSHA("nogit" on failure) over Trim, and the rev-parse pin gains
                 --short. The cmd callers switch over. The two scanner taint guards are retargeted
                 to Trim's marker.
       files:    internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go (receives techdebt's
                 TestShortSHADegradesToNogit and TestNormalizeSHAFallsBackOnEmpty),
                 internal/techdebt/run.go, internal/techdebt/run_test.go, internal/quality/run.go,
                 internal/quality/run_test.go, internal/security/run.go,
                 internal/security/run_test.go, internal/cmd/security.go, internal/cmd/quality.go,
                 internal/cmd/techdebt.go, internal/cmd/taint_scanners_test.go
       covers:   c-3
       contract: TestNoTestLost is green without editing the inventory:
                 - techdebt's differently named pair moves to gitrun
                 - quality's and security's same-named TestShortSHAFallback/TestNormalizeSHA pairs
                   stay in place and target gitrun.ShortSHA
                 - security's normalize case uses a stub git that prints blanks and exits 0, and
                   gets "nogit"
       contract: TestScannerRevParseMarkerIsLoadBearing and TestEveryScannerMarkerIsLoadBearing
                 keep their names and files (TestScopedGuardsSurvive). Deleting Trim's
                 taint-cleared marker yields findings whose origin is gitrun's Trim spawn and
                 which escape in each of security, quality and techdebt. This proves the run-dir
                 SHA is still cleared, and cleared by the runner.
       contract: S2 drops by 3 and no ShortSHA FuncDecl remains outside gitrun.

  t-20 Route codex, consent, remote git via gitrun (depends t-8, t-9)
       desc:     Four remaining git spawns outside cmd move onto gitrun verbs:
                 - codex recentLog goes through gitrun.Read
                 - consent RefuseTrackedLocal goes through gitrun.Quiet
                 - remote ignoreRule goes through gitrun.Raw, keeping its consumer marker
                 - remote isGitWorkTree goes through gitrun.Trim, with --is-inside-work-tree
                   pinned and Trim's marker prose extended to booleans
                 The pinned file lists are updated.
       files:    internal/codex/git.go, internal/consent/store.go, internal/remote/remote.go,
                 internal/cmd/taint_gitplumbing_test.go, internal/cmd/execconsent_audit_test.go
       covers:   c-3
       contract: TestExecConsentScansTheSpawningPackages pins internal/gitrun/gitrun.go instead
                 of codex/git.go, which no longer spawns. execConsentMarkedFiles lists gitrun.
                 TestHelperPackageSpawnsAreMarked stays green. The remote.go buildCommand marker
                 and TestRemoteMarkerNamesTheCallerCheck are untouched.
       contract: the existing consent tests still refuse a tracked local.toml unread, and the
                 existing remote tests still anchor ignore-derived excludes and fall back outside
                 a work tree. S2 drops by 2. S5 applies, and this or t-21 is the likely task to
                 cross the site and source floors.

Wave 4
  t-21 Route phase, recover, stale git via gitrun (depends t-13)
       desc:     The rest of cmd's direct git spawns move onto gitrun:
                 - phase.go: the two `git show` reads go through gitrun.Read, and changes.json
                   decodes through the new changes.Decode
                 - ship_recover.go: the rev-parse --verify probe goes through gitrun.Quiet
                 - milestone_stale.go: the diff goes through Raw, and patch-id through a stdin
                   variant sharing Raw's spawn
                 isAncestor's *exec.ExitError match becomes exitCoder.
       files:    internal/cmd/phase.go, internal/cmd/ship_recover.go, internal/cmd/milestone_stale.go,
                 internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go,
                 internal/cmd/boundary_test.go
       covers:   c-1, c-3, c-8
       contract: TestPhaseGitShowOutputStaysOffTheError (a named guard) is unchanged: CANARY-SHOW
                 never reaches the refusal, and the stub saw `show`. phase.go's taint-cleared
                 marker on `return ch.Base` still clears something (S1).
       contract: the new TestIsAncestorOverARealRepo runs on a temp repo. An ancestor gives
                 (true, nil). A non-ancestor gives (false, nil), because exit 1 is matched through
                 exitCoder. A missing ref gives an error. TestStale* passes unchanged. The new
                 changes.Decode test round-trips Base and PR, and garbage returns an error.
       contract: a gitrun test for the stdin variant: patch-id over a two-file diff equals
                 `git diff | git patch-id --stable` run by hand. The count of Raw spawns stays 1.
                 phase.go, ship_recover.go and milestone_stale.go leave os/exec, and phase.go
                 also leaves encoding/json. S2 drops by 5.

Wave 5
  t-22 Flat ban and git-runner census (depends t-3, t-4, t-5, t-6, t-7, t-12, t-14, t-15,
       t-16, t-17, t-18, t-19, t-20, t-21)
       desc:     Delete cmdForbiddenBaseline. checkBoundary(pkgs) bans os/exec, net/http, go/ast,
                 encoding/json and BurntSushi/toml outright in non-test internal/cmd.
                 TestCmdForbiddenImportRatchet is replaced by a table-driven ban self-test. Add
                 the git census.
       files:    internal/cmd/boundary_test.go
       covers:   c-6, c-1, c-2, c-3, c-5, c-8
       contract: each of the 5 imports injected into a synthetic cmd file gives exactly one
                 finding naming the file and the import. The same import in a cmd _test.go, or in
                 a synthetic internal/foo, gives none. The live tree is green.
       contract: TestFlatBanHasNoAllowlist requires `reflect.TypeOf(checkBoundary).NumIn() == 1`.
                 It also requires that boundary_test.go declares no package-level var whose name
                 matches (?i)baseline|allow. Re-introducing an allowlist fails one of the two.
       contract: TestEveryGitSpawnIsInTheRunner finds exactly zero exec.Command or
                 exec.CommandContext calls with a literal "git" binary outside internal/gitrun,
                 and at least 1 inside it, so the check cannot pass vacuously. Its synthetic
                 self-tests cover three cases:
                 - a git spawn in internal/foo gives a finding naming the file
                 - the same spawn in gitrun gives none
                 - a FuncDecl named gitTrim, gitRead, gitRun, gitNoOut, gitBranchTrim,
                   gitStatusRaw, gitRemoteOriginURL or ShortSHA outside gitrun gives a finding
                 subprocargs' fail-closed rule for non-literal binaries covers a git binary held
                 in a variable.

## Coverage

Each criterion's ideal contract, written before the tasks, and the tasks that make it
satisfiable:

- **c-1** (os/exec baseline empty; four gated sites included). Ideal contract: the flat ban
  (t-22) is green over the live tree with no allowlist. Each moved gated site still resolves
  execReachGated and unmarked. Deleting its cmd-side consent call makes it a finding, so
  requireExecConsent stays in cmd.
  Tasks: t-3, t-4, t-5, t-6, t-7, t-16, t-17, t-18, t-21, sealed by t-22. Per file, S3 enforces
  it from t-2 on. The load-bearing surgeries are in t-4 (run), t-6 (survivor drain) and t-7
  (verify).
- **c-2** (net/http empty; the client and fetch live in internal/update). Ideal contract: the
  flat ban covers net/http, and the signature-gated update flow's tests pass in
  internal/update.
  Tasks: t-3, sealed by t-22.
- **c-3** (one git runner repo-wide with the verb split kept). Ideal contract has four parts:
  - a census puts every literal-"git" spawn in internal/gitrun, and none of the named helpers
    exist outside it
  - Trim's verb pin covers every gitrun.Trim call repo-wide
  - Raw and Read carry no taint-cleared marker
  - Run's failure output goes to stderr and never into the error
  The runner is also a leaf package.
  Tasks: t-8 (runner, verb tests, leaf rule), t-9 (the audits learn the gitrun form), t-13
  (cmd helpers), t-16, t-17, t-18, t-21 (cmd direct sites), t-19 and t-20 (the 7 sites outside
  cmd), sealed by t-22 (census).
- **c-4** (verdicts kept, audit and taint gate green after every move, marker count not grown).
  Ideal contract: S1 and S2 hold at every commit, and floors are reset only by the S5 rule.
  Tasks: t-2 arms the budget. t-8 and t-9 carry the runner and audit retargeting. Every moving
  task names the pinned guard it retargets: t-3, t-4, t-5, t-6, t-7, t-16 to t-21. Net
  exec-exempt markers go from 27 to 10.
- **c-5** (local.toml persistence in its own package; local.go is just the command tree).
  Ideal contract, per the locked local_store_proof decision: the codec ban (t-22) plus
  internal/cmd importing localstore plus no toml-tagged struct in cmd. On top of that, local.go
  declares only cobra constructors.
  Tasks: t-11 (move, import-wiring rule, toml-tag rule), t-15 (forwarders gone, local.go
  structure rule), sealed by t-22.
- **c-6** (flat ban with bite-proving self-tests). Ideal contract: a table-driven injection of
  each of the 5 imports fires. _test.go files and other packages are exempt. There is no
  allowlist parameter or var.
  Tasks: t-2 (codec imports armed early, self-test decoupled), t-22.
- **c-7** (no CLI change, e2e unedited, moved tests move, TestNoTestLost green). Ideal
  contract: a whole-tree surface golden minted before any move, a phase-start inventory, and
  e2e test files byte-identical apart from shims and named setup lines.
  Tasks: t-1 arms it. t-13 and t-15 keep it through their test shims. Every task is held to S4.
- **c-8** (no encoding/json or toml in cmd; decoding in domain packages; output through one
  render package; both imports banned). Ideal contract: the flat ban covers both imports.
  render's differential tests prove byte identity, and the moved decoders' tests pass in
  their packages.
  Tasks: t-2 (ratchet armed), t-6 (survivor payload), t-10 (render plus TOML), t-11
  (localstore), t-12 (settings.json), t-14 (JSON output), t-21 (changes.Decode), sealed by
  t-22.

Coverage: 8/8.

## Judgment calls

- The codec imports are armed in the ratchet at t-2 instead of banned only at the end. That
  gives every c-8 task a mechanical per-file contract. The rejected alternative, ban-at-end,
  would leave 7 tasks without enforcement until t-22.
- The exec-exempt budget is "<= phase-start count", set to 27 by parsing directives. A
  shrink-only ratchet was rejected: c-4 says "does not grow", and a ratchet would force a
  shared constant edit in about 10 tasks. The grep count of 28 includes trust.go's prose
  mention.
- A whole-tree CLI golden is added from cmd/dross newRoot(). Extending internal/cmd's four-tree
  golden was rejected because those four trees miss update, local, env, statusline, test and run,
  which this phase touches.
- tests_before.txt is re-minted at phase start as a superset. The old inventory predates
  exec-taint-enumeration, so those guards could otherwise be deleted with TestNoTestLost staying
  green.
- gitrun has exactly four spawns: Trim, Raw, Run and Quiet. Read, the timeout variant, the
  stdin variant and ShortSHA reuse them.
  - One shared constructor was rejected: it would merge the taint-origin lines that
    TestGitRunGateIsScopedByOrigin keys on.
  - One spawn per helper was rejected: every extra spawn costs a marker against the budget.
- The runner lands behind delegates (t-8), the audits learn `gitrun.X(...)` (t-9), and only then
  do the call sites move (t-13). The rejected big-bang task would reveal a broken selector case
  only as a floor drop.
- Test-only shims (git_shim_test.go, local_shim_test.go) follow the consent_shim_test.go
  precedent, so c-7's "no assertion edits" holds as a zero diff rather than a claim needing
  review. Rewriting setup in about 45 test files was rejected.
- The gated moves are 1:1, keeping run's and test's `sh -c` spawns separate, so "keeps its
  verdict" can be checked site for site. Merging them was rejected: it adds a behaviour change
  (stdin wiring) to a move.
- `dross run`'s slot spawn moves to testlane next to the suite spawn it mirrors. The locked
  decision forbids a catch-all spawn package, and testlane is the closest named home.
- runUpdate moves wholesale into update.Apply, and its tests move with it. Keeping it in cmd
  behind an `*http.Client` type alias was rejected because it defeats c-2's intent.
- resolveRemoteHost stays in cmd/remote_pool.go. It is probe orchestration over the grants that
  localstore reads, and moving it would drag the ssh preflight into the store. The locked proof
  holds either way.
- The final flat-ban task (t-22) also takes the git census, since both are end-state proofs in
  boundary_test.go. Putting the census in t-20 was rejected: it would stay red until t-16, t-17,
  t-18 and t-21 all land.
- Floors are reset only in the task whose census crosses them (S5). Lowering them early in wave 1
  was rejected: the "internal/cmd alone must not meet the source floor" check would go red while
  cmd still holds about half the sources.
- For the scanner ShortSHA tests, techdebt's pair moves and quality's and security's same-named
  pairs stay, retargeted at gitrun.ShortSHA. Moving both would drop a name that exists in two
  packages to one, which TestNoTestLost correctly reads as a deletion. Editing the inventory was
  rejected.
- remote.go's two git reads change from gated-by-reach to marker-exempt shared runner spawns.
  The locked git_runner_scope decision sanctions this. The user-code spawns keep
  "gated, unmarked", enforced in t-4 to t-7.
- gitargs.go (gitRefArgs, gitPathArgs) stays in cmd. They build argv and do not spawn, and
  moving them would churn every ident-keyed audit for no gain on c-3.
- t-21 is sequenced after t-13 without a semantic dependency, because phase.go, ship_recover.go
  and milestone_stale.go are all rename targets in t-13.
- env's settings.json codec goes to a new internal/claudesettings rather than
  internal/statusline. `dross env` is not a status-line concern, and statusline keeps only
  ExistingCommand.
- emitJSON is kept as a cmd delegate to render.JSON, which leaves its 9 callers untouched. It
  holds no codec, so the ban is satisfied.
- The JSON output swap (t-14, 10 files) and the git rename (t-13, 29 files) are each kept as one
  task. The edits are uniform one-line swaps under a single contract, and splitting would add
  10-minute gates without adding a contract.
