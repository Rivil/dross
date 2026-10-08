# mvp draft — cmd-exec-baseline-drain

Lens: the smallest task set that satisfies every criterion. Each task compiles and goes green on its own; a file leaves the ratchet baseline in whichever task drops its last forbidden import.

```
Phase cmd-exec-baseline-drain — 8 tasks across 3 waves

Wave 1
  t-1  Add leaf gitrun runner; route outside sites
       desc:     New leaf package internal/gitrun (stdlib imports only) with the verb split: Trim (pinned ref verbs, taint-cleared),
                 Read (content, unmarked), Raw (untrimmed bytes, optional stdin), Run (effect; failure output to gitrun.Stderr,
                 error carries exit status only), NoOut (exit status), ShortSHA, ExitCode, ArgvRecorder. Route the 7 non-cmd git
                 spawns through it: the three ShortSHA copies, codex recentLog, consent.RefuseTrackedLocal, remote ignoreRule and
                 isGitWorkTree. Add the exec-exempt marker ceiling test.
       files:    internal/gitrun/gitrun.go (NEW), internal/gitrun/gitrun_test.go (NEW),
                 internal/techdebt/run.go, internal/quality/run.go, internal/security/run.go,
                 internal/codex/git.go, internal/consent/store.go, internal/remote/remote.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/subprocargs_audit_test.go,
                 internal/cmd/taint_scanners_test.go
       covers:   c-3, c-4
       contract: - gitrun_test: with a stub `git` on PATH that prints CANARY to both streams and exits 1, gitrun.Run returns an
                   error with no CANARY in its text and CANARY lands in gitrun.Stderr. gitrun.Raw keeps the leading space of
                   " M a.go", which Trim strips. ShortSHA outside a repo returns "nogit".
                 - NEW TestExecExemptMarkersDoNotGrow: the total of repoExecGraph's marker map is <= 26, the phase-start count
                   under the directive grammar. The 28 raw grep hits include trust.go's prose mention and diag/trust.go's
                   string literal. The same count over a fixture with one extra marker fails.
                 - TestExecConsentScansTheSpawningPackages and TestAuditScansCodexPackage are re-pinned from
                   internal/codex/git.go to internal/gitrun/gitrun.go. Left on codex/git.go they fail with "holds no spawn
                   site". TestScannerRevParseMarkerIsLoadBearing is re-pointed at gitrun.ShortSHA and still reports a finding
                   naming the spawn when its taint-cleared marker is stripped.
                 - TestEverySpawnSiteGatedOrExempt stays green. Every gitrun spawn is marked and reached as mixed, ungated or
                   none, never gated-only, so no marker is itself a finding.
       depends:  —

  t-2  Move testlane spawns out of cmd
       desc:     Move runLocalCommandCtx + shArgvFor, runRemoteCommand, runSlotCommand, the install spawn inside
                 runInstallLocally, and exec.LookPath into internal/testlane. cmd keeps its seam vars (spawnLocal, spawnLocalCtx,
                 spawnRemote, laneLookPath, localInstallFn), the installStderr printing and every
                 requireExecConsent/RunConsented call. redproof_replay reads exit codes via testlane.ExitCode.
       files:    internal/testlane/spawn.go (NEW), internal/cmd/test.go, internal/cmd/run.go,
                 internal/cmd/lane_install.go, internal/cmd/redproof_replay.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go,
                 internal/cmd/subprocargs_audit_test.go, internal/cmd/boundary_test.go
       covers:   c-1, c-4, c-7
       contract: - TestToolchainSpawnsResolveAsGated, with execConsentGatedFiles naming internal/testlane/spawn.go instead of
                   cmd's test.go, run.go and lane_install.go, finds sites there, every one `gated` and unmarked.
                 - TestReachProofIsLoadBearing: ungating run.go's RunConsented call still yields an `ungated` finding, now
                   matched by execFindingIsIn(f, "internal/testlane/spawn.go").
                 - The subprocargs accepted key "spawn.go:argv[…]" replaces "test.go:argv[…]". Leaving the old key makes the
                   audit flag argv[0] as an unaccepted non-literal binary.
                 - These pass with no assertion edits: TestLaneInstallOutputGoesToStderr (CANARY stays off the error),
                   TestRunSlotStillRefusesAtRuntime (unconsented `dross run lint` refused) and TestStreamSitesCarryNoMarker
                   (streamSiteFiles re-pointed).
                 - TestCmdBoundaryByImportDirection: test.go, run.go, lane_install.go and redproof_replay.go are removed from the
                   os/exec baseline. A stale entry fails asking for its removal.
       depends:  —

  t-3  Move survivor and verify spawns to domains
       desc:     Move goListDirs' `go list` spawn, runCoverageProfile and the gremlins report decode (readRawReport,
                 notCoveredPositions, plus their json import) into internal/survivor. Move runDetachArgv into internal/verify,
                 and classifyFetch's *exec.ExitError read into internal/remote beside remote.Classify. cmd keeps the seam vars
                 and requireExecConsent.
       files:    internal/survivor/drain.go (NEW), internal/survivor/drain_test.go (NEW, moved tests),
                 internal/verify/detach.go (NEW), internal/remote/remote.go,
                 internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go, internal/cmd/verify.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/boundary_test.go
       covers:   c-1, c-4, c-7, c-8
       contract: - TestToolchainSpawnsResolveAsGated, with internal/survivor/drain.go and internal/verify/detach.go in
                   execConsentGatedFiles: every site is `gated` and none is marked. The execLiveSurgeries verify ungate (its
                   anchor in cmd/verify.go is unchanged) still turns the moved detach spawn into an ungated finding.
                 - survivor_drain_consent_test passes unedited: an unconsented `survivor drain` refuses before goListDirs
                   spawns.
                 - The moved notCoveredPositions test, now in internal/survivor, still returns "read mutant statuses" on a
                   malformed payload. TestNoTestLost stays green (moved, not copied).
                 - The existing verify fetch-classification tests pass unedited: rsync exit 23 still maps to remote.ErrPartial.
                 - survivor_drain.go and verify.go are removed from the os/exec baseline.
       depends:  —

  t-4  Move update HTTP client and self-exec
       desc:     Add update.HTTPDoer and a client constructor taking apiBase + doer. Move the self-exec into
                 update.SelfInstall(newBinary, out) in internal/update/update.go, carrying its minisign marker unchanged.
                 cmd/update.go keeps only flags and output, and its httpClient field is typed update.HTTPDoer.
       files:    internal/update/update.go, internal/cmd/update.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/boundary_test.go
       covers:   c-2, c-1, c-4
       contract: - cmd update_test.go passes with zero edits: `httpClient: srv.Client()` still compiles against
                   update.HTTPDoer. Retyping the field as *update.Client would force setup edits.
                 - TestUpdateSelfExecIsTheOnlyMarkerHere, re-pinned to internal/update/update.go, finds exactly one site. That
                   site is marked and its reason contains "verif".
                 - The subprocargs accepted key "update.go:newBinary" still matches (basename update.go, var newBinary). Renaming
                   the var flags an unaccepted non-literal binary.
                 - The net/http baseline is empty and update.go is gone from the os/exec baseline.
                   TestCmdBoundaryByImportDirection is green.
       depends:  —

  t-5  Extract local.go persistence into localstore
       desc:     Create internal/localstore holding: localStore load/save (toml), the local.toml key table, detached-run records,
                 readRemoteGrants/effectiveRemote/readAllowHosts/resolveRemoteEnv, mutation tuning, readLocalKey and the
                 consent grant store (GrantStore). local.go keeps only Local()/localGet/localSet. resolveRemoteHost's probe half
                 moves to cmd/remote_pool.go next to probeRemotePool. Callers switch to localstore.X.
       files:    internal/localstore/localstore.go (NEW), internal/localstore/localstore_test.go (NEW, moved tests),
                 internal/cmd/local.go, internal/cmd/remote_pool.go,
                 internal/cmd/local_test.go, internal/cmd/local_detached_test.go,
                 internal/cmd/local_remote_alias_test.go, internal/cmd/local_resolution_test.go,
                 internal/cmd/execconsent_audit_test.go,
                 callers: internal/cmd/{basebranch,doctor,gitignore,issue,lane_install,lane_locality,lane_preview,
                 lane_preview_locality,redproof_replay,remote_bootstrap,remote_bootstrap_cmd,remote_grant,run,ship,test,
                 test_lane,test_lane_install,trust,verify}.go
       covers:   c-5, c-7, c-8
       contract: - The moved store tests pass in internal/localstore: TestDetachedRunRoundTripsEveryField,
                   TestSecondRunForOnePhaseIsRefused, TestExistingMutationGrantStillResolves and TestNewKeyWinsOverAlias.
                   TestNoTestLost stays green.
                 - The command-level tests stay in cmd and pass with no assertion edits:
                   TestRemoteGrantKeysAreNotGenericallySettable, the tracked-local.toml refusal and
                   TestRemoteStatusReportsTheHostTheRunWouldUse.
                 - The execLiveSurgeries and TestReachProofIsLoadBearing anchor becomes
                   `consent.RunConsented(localstore.GrantStore(root), line)`. The old anchor fails with "anchor never matched".
                 - local.go imports neither toml nor consent's store types and declares no struct type.
       depends:  —

  t-6  Route cmd JSON/TOML through render and domains
       desc:     New internal/render: JSON(w,v) with emitJSON's encoder + 2-space indent, JSONCompact (json.Marshal),
                 JSONIndent (MarshalIndent) and TOML(w,v). The 15 output-only files swap to it, and jsonout.go's emitJSON
                 becomes a one-line call. Decoders move to their domains: changes.Parse for phase.go's git-show blobs,
                 statusline.ExistingCommand for statusline.go, hooks.ReadSettings/MutateSettings for env.go. stack.go and
                 init.go resolve binaries through a new stack.HostLookPath.
       files:    internal/render/render.go (NEW), internal/render/render_test.go (NEW),
                 internal/changes/changes.go, internal/statusline/settings.go, internal/hooks/settings.go,
                 internal/stack/runtime.go,
                 internal/cmd/{changes,deferred,defaults,dotget,env,jsonout,milestone,phase,project,profile,reentry,
                 ship,state,stack,statusline,task,watch,verifyscope,init}.go,
                 internal/cmd/boundary_test.go
       covers:   c-8, c-1, c-7
       contract: - These pass unedited, which proves the bytes are unchanged: json_show_test.go
                   (TestProjectShowJSONMatchesTheDocument, TestMilestoneAndDefaultsShowJSONDropTheHeader,
                   TestStackShowJSONEmitsTheProfile), json_show_phase_test.go and json_show_task_test.go.
                 - render_test: JSON output ends in "\n" with 2-space indent; JSONCompact equals json.Marshal byte for byte; TOML
                   equals toml.NewEncoder's output for a fixture with a nested table.
                 - TestPhaseGitShowOutputStaysOffTheError passes: a changes.Parse failure on a garbage blob still yields the
                   no-recorded-base refusal. `dross env set` still writes settings.json 0600 with a trailing newline through
                   hooks.MutateSettings.
                 - After this task the only encoding/json or BurntSushi/toml importer left in cmd is local.go (if t-5 is still
                   pending). stack.go leaves the os/exec baseline.
       depends:  —

Wave 2 (depends t-1)
  t-7  Route every cmd git spawn through gitrun
       desc:     Delete gitTrim/gitRead/gitRun/gitNoOut/gitVerb/gitArgvTap/gitStderr (ship_recover.go, phase.go),
                 gitBranchTrim, gitStatusRaw and gitRemoteOriginURL. The ~170 helper call sites become gitrun.Trim/Read/Run/NoOut,
                 and the 16 direct spawns move to the matching verb: git show → Read, status --porcelain → Raw,
                 diff | patch-id → Raw with stdin, isAncestor → gitrun.ExitCode. The argv builders stay in cmd/gitargs.go. The
                 git-keyed audits are re-pointed at the runner, spawn floors are recalibrated, and the c-3 proof test is added.
       files:    internal/cmd/{ship_recover,statusline,phase,cleantree,milestone_stale,pause,worktree_files,init,
                 techdebt,milestone_merged}.go,
                 helper callers: internal/cmd/{redproof,redproof_set,redproof_repoint,redproof_lifecycle,repair,
                 repair_state,repair_files,repair_phasedirs,phase_lifecycle,phase_backfill,phase_reconcile,phase_checkout,
                 originpush,topology,verifyscope,status,doctor,forkpoint,milestone,ship,secretscan,basebranch,
                 switchbranch}.go,
                 internal/gitrun/gitrun.go,
                 internal/cmd/taint_gitplumbing_test.go, internal/cmd/taint_gitdirect_test.go,
                 internal/cmd/subprocargs_audit_test.go, internal/cmd/execconsent_audit_test.go,
                 internal/cmd/taint_audit_test.go, internal/cmd/boundary_test.go,
                 test setup: internal/cmd/{ship,switchbranch,basebranch,gitseparator,verifyscope}_test.go
       covers:   c-1, c-3, c-4
       contract: - NEW TestEveryGitSpawnIsInTheRunner: every exec.Command/CommandContext with the literal "git" in non-test source
                   under internal/ sits in internal/gitrun. None of gitTrim, gitRead, gitRun, gitNoOut, gitBranchTrim,
                   gitStatusRaw, gitRemoteOriginURL or ShortSHA is declared outside it. A fixture that adds
                   exec.Command("git", "status") in internal/foo/x.go fails naming that file.
                 - TestGitTrimRunsRefVerbsOnly walks `gitrun.Trim` selector calls repo-wide. It examines at least 30 sites
                   (gitTrimSiteFloor), and `gitrun.Trim(dir, "log", "--oneline")` still trips "runs `log`". The subprocargs
                   gitCallFuncs recognise the gitrun selectors: if one is dropped, that helper's floor count goes to zero and
                   the test fails.
                 - TestShipRecoverFetchFailureOutputGoesToStderr and TestGitRunGateIsScopedByOrigin pass after their setup swaps
                   to gitrun.Stderr and gitrun.ArgvRecorder: git's output reaches stderr and never the error.
                   TestInitRawRemoteURLCarriesNoMarker is re-pointed at init's gitrun.Read call and still requires no
                   taint-cleared marker.
                 - execConsentMinSites/Files and execSourceFloor/FileFloor are re-measured and reset ~25% under the new live
                   counts, with a comment. TestExecConsentFloorCatchesANarrowedWalk and TestExecSourceFloor still pass at the
                   minimum and fail one under. TestDroppingAScanRootFailsTheFloor still fails cmd/ alone.
                 - ship_recover, statusline, phase, cleantree, milestone_stale, pause, worktree_files, techdebt and init
                   leave the os/exec baseline, which is now empty.
       depends:  t-1

Wave 3 (depends t-1..t-7)
  t-8  Replace ratchet with flat import ban
       desc:     Delete cmdForbiddenBaseline. forbiddenInCmd becomes os/exec, net/http, go/ast, encoding/json and
                 github.com/BurntSushi/toml, and checkBoundary loses its baseline parameter. Add internal/localstore to the
                 wiring set, plus a rule that no non-test internal/cmd struct field carries a `toml:` tag, which rules out a
                 local.toml store type in cmd. Rewrite the ratchet self-tests as ban self-tests.
       files:    internal/cmd/boundary_test.go
       covers:   c-6, c-5, c-8, c-1, c-2
       contract: - TestCmdForbiddenImportRatchet → ban self-test: for each of the 5 forbidden imports, adding it to a copy of
                   issue.go yields exactly one finding naming issue.go and that import. checkBoundary(pkgs) has no allowlist
                   argument to widen.
                 - If cmd drops its import of internal/localstore, the check fails with "wiring: … does not import …/localstore".
                   A synthetic cmd file declaring `type store struct{ AllowHosts []string \`toml:"allow_hosts"\` }` fails the
                   no-store-type rule.
                 - TestCmdBoundaryByImportDirection is green over the live tree, so c-1, c-2 and c-8 hold as bans.
                   TestNoTestLost stays green.
       depends:  t-1, t-2, t-3, t-4, t-5, t-6, t-7
```

## Coverage

| Criterion | Tasks | How |
|---|---|---|
| c-1 os/exec baseline empty (incl. phase/survivor_drain/test/verify) | t-2, t-3, t-4, t-6, t-7, t-8 | t-2: test.go, run.go, lane_install.go, redproof_replay.go. t-3: survivor_drain.go, verify.go. t-4: update.go. t-6: stack.go and init.go's LookPath. t-7: the git-only files, including phase.go. t-8: turns it into a ban. |
| c-2 net/http baseline empty | t-4, t-8 | The HTTP client moves into internal/update. t-8 turns the empty baseline into a ban. |
| c-3 one git runner, verb split kept | t-1, t-7 | t-1 builds the runner and moves the 7 sites outside cmd. t-7 moves the 16 cmd sites and adds TestEveryGitSpawnIsInTheRunner. |
| c-4 verdicts kept, markers don't grow | t-1, t-2, t-3, t-4, t-7 | t-1 adds the marker ceiling. Each move re-pins its gated or exempt assertions to the new file. t-7 recalibrates the floors. |
| c-5 local.go persistence in own package | t-5, t-8 | t-5 moves it. t-8 adds the localstore wiring rule and the no-`toml:`-tag rule, and the codec ban comes with it. |
| c-6 flat ban with biting self-tests | t-8 | Ban self-tests for each of the 5 forbidden imports. |
| c-7 no CLI change, tests move, TestNoTestLost | t-2, t-3, t-5, t-6 (and every task's gate) | Each contract names the tests that must pass unedited, plus TestNoTestLost where tests move. |
| c-8 no json/toml in cmd; render pkg; ban | t-3, t-5, t-6, t-8 | t-3: survivor_drain decode. t-5: local.go toml. t-6: render plus the remaining 18 files. t-8: ban. |

## Judgment calls

- **The git runner is split in two: t-1 builds the runner and moves the 7 sites outside cmd, t-7 moves cmd.** One task was rejected at ~50 files. Doing cmd first was rejected because the leaf package plus consent/remote importing it is what t-1 proves. Only t-7 can land the c-3 proof test, because only t-7 empties every site.
- **Call sites become package-qualified `gitrun.Trim(...)` calls.** The alternative was keeping `var gitTrim = gitrun.Trim` aliases. That is cheaper and would leave the git-keyed audits unedited. It was rejected because c-3 names those helpers as replaced, and 170 calls through a function var would lean on VTA for taint precision.
- **The argv builders (gitRefArgs, gitPathArgs, gitRefPathArgs) stay in cmd/gitargs.go.** They import nothing forbidden, and the verb-pin audit resolves them by identifier. Moving them to gitrun would add churn with nothing gained.
- **run.go's slot spawn goes to testlane, next to runLocalCommandCtx.** It is the same `sh -c` shape. A new package was rejected because the locked decision forbids a catch-all spawn package. stack was rejected as a home because it is not where spawns live.
- **update gets its own task (t-4) instead of joining t-3.** The locked decision groups update with the other spawn homes, but merged the task would be ~13 files across 4 packages, and t-4 is the only task that delivers c-2.
- **update's httpClient field is typed update.HTTPDoer.** The alternative was *update.Client plus editing 5 setup lines in update_test.go. The interface lets the test compile untouched. The self-exec var stays named `newBinary` in update.go, so subprocargs' accepted key survives.
- **classifyFetch's exit-status read goes to internal/remote, next to remote.Classify.** internal/verify was the alternative. Only the spawn has to go to verify under the locked decision, and exit classification is rsync-transport logic that remote already owns.
- **Only resolveRemoteHost's local.toml half moves to localstore.** That half is readRemoteGrants and effectiveRemote. The probe half moves to cmd/remote_pool.go. Moving probing was rejected: it is not persistence, and it drags remote_pool.go's preflight machinery along.
- **env.go's settings.json read/mutate goes to internal/hooks, which already owns settings.json edits.** A new claudesettings package was rejected as speculative structure.
- **emitJSON stays as a one-line cmd wrapper around render.JSON.** Rewriting its ~9 call sites was rejected: the wrapper imports nothing forbidden, and the call sites stay as they are.
- **The survivor drain's gremlins JSON decode moves with its spawn in t-3, not with render in t-6.** That way survivor_drain.go is touched once.
- **stack.go's and init.go's LookPath are handled in t-6 through stack.HostLookPath.** t-6 edits stack.go for toml anyway. A file leaves the baseline in whichever task drops its last forbidden import, so init.go leaves in t-7 and t-7 does not depend on t-6.
- **The spawn floors are recalibrated in the task that trips them rather than kept.** That is at least t-7: about 42 sites drop to about 25, and 28 files drop to about 14. Keeping 30/20 and 37/24 is impossible once 23 git sites collapse into one file. The floor self-tests and the cmd-alone check keep the floors honest.
- **The marker ceiling lands in t-1, first in id order, not in t-8, so it guards every move.** It is set at 26 (the directive grammar) rather than the orchestrator's 28 (raw grep hits that include two prose mentions).
- **There is no separate c-7 task and no new CLI goldens.** The existing e2e tests, the json_show tests and TestNoTestLost are the gate, and each contract names the unedited tests it relies on. A render golden per command was rejected as speculative.
- **There is no leaf-package test for gitrun.** consent and remote both import gitrun, so gitrun importing either would be a compile-time import cycle, and that is the property the locked decision needs.
- **The wave-1 tasks share edits to boundary_test.go and execconsent_audit_test.go.** They edit different lines and none needs another's output, so they stay in one wave; run them sequentially in id order.
