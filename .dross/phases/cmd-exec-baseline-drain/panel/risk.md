# cmd-exec-baseline-drain — risk lens

Bias: failure modes drive the graph. The phase moves spawn sites past six audit
suites that pin files by path, count sites against absolute floors, and key on
helper *names*. Most of the ways this phase can go wrong are silent: an audit
still passes because a moved site left its pinned list, not because the site is
safe. So wave 1 turns the silent failures into loud ones. After that, each move
task owns the failure modes of the code it moves.

```
Phase cmd-exec-baseline-drain — 25 tasks across 7 waves

Wave 1
  t-1  Pin CLI surface and encoder output bytes
       files:    cmd/dross/surface_test.go (new), cmd/dross/testdata/root_surface.txt (new),
                 internal/cmd/output_golden_test.go (new), internal/cmd/testdata/cli_surface/output/ (new goldens)
       desc:     Render newRoot()'s whole tree (use/short/aliases/hidden, every flag's type/default/usage) to one golden;
                 pin exact stdout bytes of every command whose output crosses encoding/json or toml (project/milestone/
                 defaults/profile/stack show ± --json, state show, `state get b a`, changes + verify-scope --json,
                 task/deferred/watch --json, reentry, ship --json via ship_test's runCmd fixture). Minted from the pre-phase tree.
       covers:   c-7, c-8
       contract: dropping `update --check` or renaming `local get` fails TestRootCommandSurfacePinned with a diff naming it;
                 swapping state show's MarshalIndent for Marshal, dropping reentry's trailing newline, or emitting
                 `state get b a` in sorted key order each fails its output golden byte-for-byte; two runs produce
                 identical goldens (temp paths normalised)

  t-2  Make spawn-audit pins fail when stale
       files:    internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go, internal/cmd/subprocargs_audit_test.go
       desc:     Add an exec-exempt ceiling: real directives counted with directiveMarkers over non-test files, ≤ the
                 phase-start count. Every execConsentGatedFiles / execConsentMarkedFiles / streamSiteFiles entry must hold ≥1
                 live spawn site (streamSiteFiles reformatted one per line). acceptedNonLiteralBinaries is re-keyed by
                 repo-relative path, and a key that matches no live site fails as stale.
       covers:   c-4
       contract: one synthetic extra //dross:exec-exempt directive fails TestExecExemptCeiling, while the prose mentions
                 in cmd/trust.go and diag/trust.go are not counted; pointing execConsentGatedFiles at internal/cmd/issue.go
                 (no spawn) fails naming the entry; `exec.Command(argv[0])` in a synthetic internal/testlane/remote.go is NOT
                 accepted by internal/remote/remote.go's key; a key for a vanished site fails as stale

  t-3  Extend forbidden-import ratchet to codecs
       files:    internal/cmd/boundary_test.go
       desc:     Add encoding/json + github.com/BurntSushi/toml to forbiddenInCmd, with a baseline of today's 20 importers.
                 Put every baseline entry on its own line so parallel drains never collide. Rewrite the ratchet self-tests on
                 synthetic files so they never depend on which live file still imports.
       covers:   c-8, c-6
       contract: a synthetic cmd file importing encoding/json that is not in the baseline fails naming file and import;
                 baselining ship.go for toml, which it does not import, fails as stale; on a clone of the live map with every
                 forbidden import stripped and an empty baseline, every TestCmdForbiddenImportRatchet subtest still passes
                 (today's cleantree.go-dependent subtest would go red when t-19 drains cleantree.go)

Wave 2 (depends t-1, t-2, t-3 as noted)
  t-4  Create gitrun runner from core git helpers                               depends: t-2, t-3
       files:    internal/gitrun/gitrun.go (new), internal/gitrun/gitrun_test.go (new), internal/cmd/ship_recover.go,
                 internal/cmd/phase.go, internal/cmd/taint_gitplumbing_test.go, internal/cmd/execconsent_audit_test.go,
                 internal/cmd/boundary_test.go; setup-line re-points in internal/cmd/{gitseparator,verifyscope,ship,
                 basebranch,switchbranch}_test.go
       desc:     New leaf package. Trim/Read/Run/Quiet each get one spawn line and one exec-exempt marker; Trim keeps its one
                 taint-cleared marker; gitVerb, the argv Tap and the Stderr writer move in. cmd's gitTrim/gitRead/gitRun/gitNoOut
                 become one-line delegators. ship_recover's direct `rev-parse --verify` now runs through Quiet.
       covers:   c-3, c-1, c-4
       contract: Run on a stub git that prints CANARY and exits 1 returns an error without CANARY and writes CANARY to
                 gitrun.Stderr; Quiet on a non-ancestor `merge-base --is-ancestor` yields ExitCode()==1 via errors.As on
                 an interface, so isAncestor still returns (false,nil); a tap recorder sees every verb's argv;
                 TestGitRunGateIsScopedByOrigin, now keyed on gitrun's Run/Trim spawn lines, keeps only the Run-origin
                 finding; a boundary leaf rule fails if gitrun imports any internal/ package;
                 TestLiveTreeHasNoUnresolvedDispatch green (no func-valued seam around a spawn); exempt ceiling holds (net −1)

  t-5  Move sh-line spawns into testlane                                        depends: t-2, t-3
       files:    internal/testlane/spawn.go (new), internal/testlane/spawn_test.go (new), internal/cmd/test.go, internal/cmd/run.go,
                 internal/cmd/redproof_replay.go, internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go,
                 internal/cmd/boundary_test.go
       desc:     runLocalCommandCtx, runSlotCommand and shArgv/shArgvFor move to testlane as two separate spawns (not
                 merged). The spawnLocal/spawnLocalCtx/spawnRunSlot seam vars stay in cmd and point at testlane.
                 redproof's *exec.ExitError becomes an ExitCode() interface. Gated/stream lists and the reach-proof
                 expectation re-point to testlane/spawn.go.
       covers:   c-1, c-4, c-7
       contract: TestToolchainSpawnsResolveAsGated sees both testlane/spawn.go sites gated and unmarked; in TestReachProofIsLoadBearing,
                 ungating run.go's unchanged RunConsented anchor yields an "ungated" finding in internal/testlane/spawn.go;
                 replaying a red proof of `exit 3` records Red:true ExitCode:3, and a timed-out replay still says "replay could
                 not be run: timed out"; a synthetic taint-cleared marker in testlane/spawn.go fails TestStreamSitesCarryNoMarker

  t-6  Move drain spawns and report parse to survivor                           depends: t-2, t-3
       files:    internal/survivor/drain.go (new), internal/survivor/drain_test.go (new), internal/cmd/survivor_drain.go,
                 internal/cmd/survivor_drain_test.go, internal/cmd/execconsent_audit_test.go, internal/cmd/boundary_test.go
       desc:     `go list` package discovery, the `go test -coverprofile` run and readRawReport's JSON decode move to
                 survivor. The goListDirs/coverageProfileFn/drainRunner seams stay in cmd. The go-list taint-cleared marker
                 moves with its conversion. Unit tests of moved functions move with them.
       covers:   c-1, c-8, c-4
       contract: both survivor sites resolve as gated, since requireExecConsent stays in survivor_drain.go's RunE;
                 a coverage run with no `go` on PATH gives a nil profile that the drain reports as "unknown", never "not
                 covered"; a malformed gremlins report yields the same error text as before; the ratchet stays red until
                 survivor_drain.go leaves both the os/exec and encoding/json baselines

  t-7  Move detached rsync spawn into verify                                    depends: t-2, t-3
       files:    internal/verify/detach.go (new), internal/verify/detach_test.go (new), internal/cmd/verify.go,
                 internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go, internal/cmd/boundary_test.go
       desc:     runDetachArgv's body becomes verify.RunDetachArgv; the cmd var keeps its name. classifyFetch reads rsync's
                 exit status through an ExitCode() interface. Both verify.go requireExecConsent surgery anchors stay
                 byte-identical.
       covers:   c-1, c-4
       contract: for a real *exec.ExitError from `sh -c 'exit N'`, classifyFetch returns exactly what remote.Classify("rsync", …)
                 returns, across its partial/transport/remote-command codes; detachSync still runs before detachSpawn;
                 TestUserCommandFixKeepsTheSurgeryAnchors stays green, and TestSurgeryAnchorMustMatch still reports
                 "matched 2 times"; the verify/detach.go site is gated

  t-8  Move update client and self-exec into update                             depends: t-2, t-3
       files:    internal/update/apply.go (new), internal/update/apply_test.go (moved from internal/cmd/update_test.go),
                 internal/cmd/update.go, internal/cmd/execconsent_audit_test.go, internal/cmd/subprocargs_audit_test.go,
                 internal/cmd/boundary_test.go
       desc:     runUpdate, extractBinary/Zip and the resync self-exec become update.Apply(ctx, Options{Out, APIBase, HTTP,
                 Version, Commit, GOOS, GOARCH, TargetPath, Resync}). cmd's Update() only builds Options. The TestUpdate*
                 tests move with the code, and the update pins and accepted-binary key re-point to internal/update/apply.go.
       covers:   c-2, c-1, c-4, c-7
       contract: a bad minisign signature or checksum refuses before AtomicReplace (target bytes unchanged — moved
                 TestUpdateRefusesOnBadChecksum*); TestUpdateSelfExecIsTheOnlyMarkerHere finds exactly one site in
                 internal/update/apply.go whose reason names verification; TestNoTestLost: each TestUpdate* is defined once, in
                 update; t-1's surface golden still shows update's --check/--force/hidden --api-base

  t-9  Move local.toml store core to localstore                                 depends: t-3
       files:    internal/localstore/store.go (new), internal/cmd/local.go, internal/cmd/localstore_shim_test.go (new),
                 internal/pathfence/fields.go, internal/pathfence/fields_test.go, internal/cmd/boundary_test.go
       desc:     These move to localstore: the Store type (embedded consent.Grants, DetachedRun type, remote fields),
                 Load/Save, the settable-key table, ReadKey, GrantStore, AllowHosts and MutationTuning. cmd keeps
                 `grantStore(root)` as a one-line wrapper so run.go's surgery anchor text does not change. A test-only shim
                 keeps loadLocal/localPath for existing cmd tests. The pathfence declarations re-point from
                 cmd.localStore/cmd.detachedRun to localstore.
       covers:   c-5, c-8
       contract: `dross local set trusted_test_command x` and `local set remote_host h` still refuse; a grant Save keeps
                 quick_base, remote_host and detached_run entries written before it; pathfence's stale-declaration check
                 stays red until cmd.localStore.RemoteWorkdir is re-declared on localstore, and pathtaint reports no
                 unresolved declaration; TestSurgeryAnchorMustMatch is green; local.toml bytes (two-space indent) are unchanged

  t-10 Add render package; migrate indented JSON                                depends: t-1, t-3
       files:    internal/render/render.go (new), internal/render/render_test.go (new), internal/cmd/jsonout.go,
                 internal/cmd/state.go, internal/cmd/changes.go, internal/cmd/verifyscope.go, internal/cmd/boundary_test.go
       desc:     New package exposing exactly today's shapes, each taking an io.Writer: an indented document (encoder,
                 two-space indent, trailing newline), compact value bytes (for dotget's hand-ordered object), a compact
                 document and a TOML document. emitJSON and the MarshalIndent sites go through it, each keeping its
                 original writer.
       covers:   c-8
       contract: t-1's goldens for every `show --json`, state show, changes and verify-scope JSON stay byte-identical;
                 render_test pins `<a & b>` → `<a & b>` in both indented and compact shapes (HTML escaping
                 not disabled) and exactly one trailing "\n" on the indented shape

  t-11 Move settings.json codec into hooks                                      depends: t-3
       files:    internal/hooks/settings.go, internal/hooks/settings_test.go, internal/cmd/env.go, internal/cmd/boundary_test.go
       desc:     readSettings/mutateSettings (decode, marshal, 0700 dir, 0600 tmp+rename) move to hooks; env.go keeps
                 only the command tree.
       covers:   c-8
       contract: `dross env set K V` on a settings.json holding a foreign key keeps that key, leaves mode 0600 and no
                 `<path>.tmp` behind; a malformed file still fails with the "parse <path>:" prefix; an empty file reads as {}

Wave 3
  t-12 Teach git audits the runner's selector form                              depends: t-4
       files:    internal/cmd/subprocargs_audit_test.go, internal/cmd/taint_gitplumbing_test.go,
                 internal/cmd/testdata/subprocargs_audit/snippets.txt
       desc:     spawnArgvOf also recognises gitrun.<Verb>(dir, argv...) selector calls, and bare verbs inside package gitrun.
                 gitHelperSiteFloor counts delegator plus selector calls per verb. The Trim pin walks gitrun.Trim calls in
                 every package. rev-parse gains --short and --is-inside-work-tree, with the callers named (statusline,
                 ShortSHA, remote). An aliased import of gitrun is a finding.
       covers:   c-3, c-4
       contract: a synthetic `gitrun.Trim(dir, "log", "--oneline")` in an internal/security file trips
                 TestGitTrimRunsRefVerbsOnly, naming file:line; an unfenced `gitrun.Run(dir, "checkout", branch)` is flagged by
                 TestNoUnseparatedGitPositional; `import g ".../internal/gitrun"` in a non-test file is a finding;
                 `rev-parse --git-path x` through Trim still trips while `rev-parse --short HEAD` passes

  t-13 Move remote-suite and lane-install spawns to testlane                    depends: t-5
       files:    internal/testlane/spawn.go, internal/testlane/spawn_test.go, internal/cmd/test.go, internal/cmd/lane_install.go,
                 internal/cmd/subprocargs_audit_test.go, internal/cmd/execconsent_audit_test.go, internal/cmd/boundary_test.go
       desc:     runRemoteCommand (keeping the Command(argv[0]) + Args form, not a spread) and
                 runInstallLocally/installStderr move to testlane. laneLookPath defaults to a new testlane.LookPath. The
                 spawnRemote seam stays in cmd and remoteFailure keeps classifying through exitCoder. The accepted-binary
                 key is re-keyed to internal/testlane/spawn.go.
       covers:   c-1, c-4
       contract: remoteFailure over a real ExitError from the moved spawn maps exit 1 → exitSuiteFailed, and a missing
                 binary → exitTransport; TestLaneInstallOutputGoesToStderr, still in taint_usercmds_test.go per scopedGuards,
                 sees CANARY-LANE on stderr and not in the error; the stale internal/cmd/test.go:argv[…] key fails t-2's check
                 until it is re-keyed

  t-14 Move detached-run and remote-grant persistence                           depends: t-9
       files:    internal/localstore/detached.go (new), internal/localstore/grants.go (new), internal/cmd/local.go,
                 internal/cmd/remote_pool.go, internal/cmd/boundary_test.go, internal/pathfence/fields.go
       desc:     The detached-run read/record/find/clear, remote grants, candidate/alias resolution and the remote-env
                 allowlist move to localstore. resolveRemoteHost probes over ssh, so it moves next to probeRemotePool
                 in remote_pool.go instead. local.go ends with only Local/localGet/localSet. The boundary test gains the
                 locked proof: cmd imports internal/localstore, and no non-test cmd struct carries a `toml:` tag.
       covers:   c-5
       contract: a synthetic cmd file declaring a struct with a `toml:"x"` tag, or a cmd import map without
                 internal/localstore, each fail TestCmdBoundaryByImportDirection; a local.go top-level func outside
                 {Local, localGet, localSet} fails; a detached run round-trips record→find→clear and clear leaves other
                 local.toml keys intact; an allowlisted but unset mutation_remote_env name is still an error

  t-15 Route compact JSON emitters through render                               depends: t-10
       files:    internal/cmd/deferred.go, internal/cmd/dotget.go, internal/cmd/reentry.go, internal/cmd/ship.go,
                 internal/cmd/task.go, internal/cmd/watch.go, internal/cmd/boundary_test.go
       desc:     The six json.Marshal sites use render's compact shapes. dotget still builds its object in argument order
                 from per-value bytes. Each site keeps its writer (Print vs os.Stdout).
       covers:   c-8
       contract: t-1's goldens for `state get b a` (argument order, not sorted), task/deferred/watch --json, reentry's
                 SessionStart document and ship --json stay byte-identical

  t-16 Route TOML emitters through render; drain stack.go                       depends: t-10
       files:    internal/cmd/defaults.go, internal/cmd/milestone.go, internal/cmd/profile.go, internal/cmd/project.go,
                 internal/cmd/stack.go, internal/stack/runtime.go, internal/cmd/boundary_test.go
       desc:     The toml.NewEncoder sites use render's TOML shape; the `# <path>` header lines stay in cmd. stack.go's
                 exec.LookPath becomes a new stack.LookPath, following the security/quality/mutationcfg precedent.
       covers:   c-8, c-1
       contract: t-1's goldens for project/milestone/defaults/profile/stack show (header line first, TOML indent and key order)
                 stay byte-identical; the stack loadout renders the same on a PATH with none of the tools; stack.go leaves
                 both the os/exec and toml baselines

Wave 4
  t-17 Move local.toml unit tests to localstore                                 depends: t-14
       files:    internal/cmd/local_detached_test.go → internal/localstore/detached_test.go, internal/cmd/local_resolution_test.go,
                 internal/cmd/local_remote_alias_test.go, internal/localstore/grants_test.go (new), internal/cmd/localstore_shim_test.go
       desc:     Tests that call store functions directly move next to them with their names unchanged. Tests driven by
                 runCmd stay in cmd. Shim entries that no cmd test still uses are deleted.
       covers:   c-7, c-5
       contract: TestNoTestLost: every moved Test* name is defined in exactly one package (localstore), none dropped or
                 duplicated; the moved detached round-trip test calls localstore directly, not the shim, so it fails if
                 Clear stops rewriting the file

  t-18 Re-derive spawn and taint discovery floors                               depends: t-4, t-6, t-7, t-8, t-13
       files:    internal/cmd/execconsent_audit_test.go, internal/cmd/taint_audit_test.go
       desc:     Set execConsentMinSites/Files and execSourceFloor/FileFloor about 25% under the projected end state:
                 live counts, minus the 16 git sites t-19..t-23 collapse, plus the Raw and stdin verbs. Record the
                 arithmetic in the comment.
       covers:   c-4
       contract: each floor passes at its minimum and fails one under (TestExecConsentFloorCatchesANarrowedWalk,
                 TestExecSourceFloor); the cmd/-restricted program still falls under the exec-consent floor, and internal/cmd
                 alone still falls under the taint floor, which is why this can't be lowered in wave 1

Wave 5
  t-19 Route untrimmed and NUL git reads via runner                             depends: t-12, t-18
       files:    internal/gitrun/raw.go (new), internal/cmd/cleantree.go, internal/cmd/worktree_files.go, internal/cmd/pause.go,
                 internal/cmd/techdebt.go, internal/cmd/boundary_test.go
       desc:     Add gitrun.Raw: untrimmed bytes, one spawn, one exempt marker, no taint marker. status --porcelain,
                 the untracked listing, pause's status/symbolic-ref and techdebt's `ls-files -z` go through Raw/Trim.
                 Taint-cleared markers stay at the callers' conversions.
       covers:   c-3, c-1, c-4
       contract: a repo whose first porcelain line is " M a.go" still yields path "a.go", not a truncated path (the
                 leading status column survives); techdebt's list keeps "with space.go" and a name containing "\n" intact
                 (NUL split, no trim); TestNoSpawnOutputEscapes reports no marker that "clears nothing"; exempt count
                 drops by 4 net

  t-20 Route content git reads; drain phase.go, init.go                         depends: t-12, t-16, t-18
       files:    internal/cmd/phase.go, internal/cmd/init.go, internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/boundary_test.go
       desc:     phase.go's two `git show` blob reads go through gitrun.Read and decode with a new changes.Parse.
                 gitRemoteOriginURL stays in cmd but reads through gitrun.Read, never Trim, because the URL can carry a
                 token. init.go's LookPath uses stack.LookPath.
       covers:   c-1, c-3, c-8
       contract: TestPhaseGitShowOutputStaysOffTheError (no CANARY-SHOW in the refusal) and TestRemoteURLUserinfoNeverPersists
                 (no CANARY-TOK) stay green; TestInitRawRemoteURLCarriesNoMarker still finds gitRemoteOriginURL unmarked,
                 and routing `remote get-url` through gitrun.Trim trips the pin; a malformed blob makes
                 phaseRefRecordedBase return "" as before

  t-21 Route timeout and stdin git via runner                                   depends: t-12, t-18
       files:    internal/gitrun/opts.go (new), internal/gitrun/stdin.go (new), internal/cmd/statusline.go,
                 internal/cmd/milestone_stale.go, internal/statusline/settings.go, internal/cmd/taint_gitplumbing_test.go,
                 internal/cmd/boundary_test.go
       desc:     Trim gets an options form (timeout, --no-optional-locks before -C) that shares Trim's one spawn line and
                 taint marker. A stdin-fed verb carries diff→`patch-id --stable`. statusline's settings.json decode moves
                 to internal/statusline. isAncestor reads merge-base's exit status through the ExitCode() interface.
       covers:   c-3, c-1, c-8
       contract: a stub git that sleeps 5s makes statuslineGitBranch return "" within ~2s; the tap records
                 --no-optional-locks before -C; identical diffs on different parents give the same patch-id, and an empty
                 diff gives ""; isAncestor returns (false,nil) on exit 1 and an error on 128; a `--format=%(contents)` call
                 through Trim's options form trips the pin

  t-22 Collapse the three ShortSHA copies into gitrun                           depends: t-12, t-18
       files:    internal/gitrun/sha.go (new), internal/techdebt/run.go, internal/quality/run.go, internal/security/run.go,
                 internal/cmd/taint_scanners_test.go, internal/cmd/execconsent_audit_test.go
       desc:     One gitrun.ShortSHA over Trim replaces the three copies. Their taint-cleared markers now clear nothing, so
                 they go. The two scanner guards keep their names and file (scopedGuards) and re-target to Trim's marker.
                 execConsentMarkedFiles drops the three run.go files.
       covers:   c-3, c-4
       contract: run dirs carry the same short SHA as before (techdebt/quality/security run_test); in
                 TestScannerRevParseMarkerIsLoadBearing, removing gitrun.Trim's taint marker yields a finding escaping in
                 internal/security; TestEveryScannerMarkerIsLoadBearing asserts the scanner packages hold no marker and still
                 see scanner-escape findings without Trim's; TestScopedGuardsSurvive is green; exempt count −3

Wave 6
  t-23 Route consent, remote and codex git via runner                           depends: t-19
       files:    internal/consent/store.go, internal/remote/remote.go, internal/codex/git.go, internal/cmd/execconsent_audit_test.go
       desc:     consent's tracked-store probe uses Quiet; remote's ignore listing uses Raw and its work-tree probe Trim;
                 codex's log uses Read. The marked/spawning-package pins drop internal/codex/git.go; remote.go stays for
                 its ssh seam.
       covers:   c-3, c-4
       contract: a tracked local.toml still makes consent's store refuse, naming the file, and an untracked one does
                 not; remote's rsync exclude file keeps a path with spaces, and a non-git dir falls back to the merge rule;
                 TestRemoteMarkerNamesTheCallerCheck still finds the marked ssh seam; t-18's floors pass at the final counts

  t-24 Rewrite cmd git call sites; delete helpers                               depends: t-19, t-20, t-21
       files:    ~131 non-test call sites of gitTrim/gitRead/gitRun/gitNoOut across internal/cmd (mechanical),
                 internal/cmd/ship_recover.go, internal/cmd/phase.go, internal/cmd/gitshim_test.go (new, test-only helpers)
       desc:     Every delegator call becomes gitrun.<Verb> and the four delegators are deleted. cmd tests that use
                 gitTrim/gitRun as fixture helpers get a test-only shim, so their bodies are unchanged.
       covers:   c-3, c-7
       contract: TestGitHelperCallSiteFloor's per-verb totals equal their pre-rewrite totals (no site fell out of the argv
                 audit); TestGitTrimRunsRefVerbsOnly examines the same number of Trim sites; cmd tests pass with the new shim
                 file as the only test-side change

Wave 7
  t-25 Flatten the ratchet into a ban; pin git-only-runner                      depends: t-11, t-15, t-17, t-22, t-23, t-24
       files:    internal/cmd/boundary_test.go, internal/cmd/gitrunonly_test.go (new), internal/cmd/execconsent_audit_test.go
       desc:     Delete cmdForbiddenBaseline and checkBoundary's baseline parameter, so os/exec, net/http, go/ast,
                 encoding/json and BurntSushi/toml become a flat ban. Add a guard that no non-test file outside
                 internal/gitrun spawns a literal "git" or declares ShortSHA/the old helper names. Set the exempt ceiling
                 to the final measured count.
       covers:   c-6, c-3, c-1, c-2, c-8, c-4
       contract: injecting any forbidden import into any non-test internal/cmd file (an exhaustive file × import loop on a
                 clone) yields exactly one finding naming both, and the same import in a synthetic x_test.go yields none;
                 a synthetic `exec.Command("git", "status")` in internal/codex, or a `func ShortSHA` outside gitrun, fails
                 the git-only guard; one added marker now fails the ceiling
```

## Coverage

- **c-1** (os/exec baseline empty, incl. phase/survivor_drain/test/verify): t-4 (ship_recover), t-5 (run, redproof_replay), t-6 (survivor_drain), t-7 (verify), t-8 (update), t-13 (test, lane_install), t-16 (stack), t-19 (cleantree, worktree_files, pause, techdebt), t-20 (phase, init), t-21 (statusline, milestone_stale), t-25 (ban proves it)
- **c-2** (net/http out of cmd): t-8, t-25
- **c-3** (one git runner, verb split kept, all 23 sites): t-4, t-12, t-19, t-20, t-21, t-22, t-23, t-24, t-25
- **c-4** (verdicts kept, audits green every move, markers do not grow): t-2 (ceiling + stale pins), t-18 (floors), t-4/t-5/t-6/t-7/t-8/t-13/t-19/t-22/t-23 (per-move verdict contracts), t-25 (ceiling locked at final)
- **c-5** (local.go persistence in its own package; cmd keeps get/set): t-9, t-14, t-17
- **c-6** (flat ban with bite-proving self-tests): t-3, t-25
- **c-7** (no CLI change; e2e unedited; tests move; TestNoTestLost): t-1, t-5, t-8, t-17, t-24 (plus TestNoTestLost gating every task)
- **c-8** (no json/toml in cmd; domain decode; one render package): t-3, t-6, t-9, t-10, t-11, t-14, t-15, t-16, t-20, t-21, t-25

## Judgment calls

- **Guards land before any move (wave 1).** I rejected folding each guard into its move task, because a task that writes its own guard can make both agree on the wrong thing.
- **Runner lands together with its first callers (t-4).** I rejected shipping an empty runner first. An uncalled marked verb adds to the exempt count and breaks c-4, and an uncalled unmarked verb is an "unreachable" exec-consent finding.
- **One spawn line per gitrun verb.** I rejected a single shared spawn. Taint origin scoping keys on the spawn line (TestGitRunGateIsScopedByOrigin), and Trim's taint-cleared marker must not also clear Read/Raw content.
- **Two-step rewrite, delegators kept from t-4 to t-24.** I rejected one mass rewrite at runner creation. It would touch ~131 call sites across ~40 cmd files and collide with every wave-2–5 task, and the name-keyed audits (subprocargs gitCallFuncs, the Trim pin) would go blind partway through. t-12 teaches the audits the selector form before anything writes it.
- **The mass rewrite goes last (t-24, wave 6).** It is the only task touching most of cmd, so it runs after every other non-test cmd edit rather than conflicting with them.
- **Floors are re-derived once, in t-18, to a projected end state.** It sits after the moves that keep counts the same and before the moves that collapse them. I rejected lowering floors in wave 1, because the taint gate's "internal/cmd alone must not meet the floor" check would fail while cmd still holds its git sources. I also rejected letting whichever task crosses a floor re-derive it: that gives multiple owners, and the crossing point can't be known without running the audit.
- **Exempt ceiling during the phase, exact ratchet at the end.** The ceiling is ≤ the phase-start count in t-2 and is tightened to the exact final count in t-25. I rejected an exact shrink-only ratchet during the phase, because 6+ marker-removing tasks would edit the same constant concurrently.
- **remote.go's two git calls change from gated-by-reach to covered by the runner's marker.** The locked git_runner_scope decision forces this. I read c-4 as "no moved site becomes a finding". Each runner verb's reach is the union of its callers, and every verb has at least one caller reachable without the gate, so "marked" is its true verdict.
- **Gated spawns move 1:1, not merged.** The local-suite and run-slot `sh -c` spawns stay separate, so each command's reach proof stays attributable and site counts match t-18's projection.
- **Seam vars stay in cmd and point at domain functions** (spawnLocal, spawnRemote, spawnRunSlot, runDetachArgv, goListDirs, coverageProfileFn, detachSpawn). Existing seam-swapping tests stay unedited (c-7), and VTA already follows var seams.
- **gitRemoteOriginURL reads through Read, never Trim.** The URL can carry a token, and Trim's taint-cleared marker would clear it silently. t-12's pin makes the wrong routing loud.
- **resolveRemoteHost moves to cmd/remote_pool.go, not localstore.** It probes over ssh. A persistence package that spawns transitively is the wrong shape, and c-5 only requires local.go to end as the get/set tree.
- **Test-only shims instead of editing cmd test bodies** (localstore_shim_test.go, gitshim_test.go). c-7 forbids assertion edits, and every boundary/ban check skips _test.go, so a shim can't hide a forbidden import.
- **render exposes today's exact shapes instead of one unified call.** Unifying would change bytes: compact vs indented, the trailing newline, dotget's argument order, and Print vs os.Stdout writers. t-1's goldens make any drift loud.
- **acceptedNonLiteralBinaries is re-keyed by repo-relative path in t-2.** Base-name keys ("remote.go:argv[…]") would silently accept a moved spawn in any file sharing that base name. That is exactly the risk for testlane and update.
- **Pinned lists and the baseline go one entry per line (t-2, t-3).** Every drain task edits them, and wave-2 siblings would otherwise conflict textually.
- **No automated guard for "no assertion edits".** It is a verify-time review of internal/cmd/*_test.go against the phase base. An AST snapshot of every assertion was rejected as heavier than the risk.
- **stack.LookPath and testlane.LookPath are exported per domain,** following security/quality/mutationcfg.LookPath. I rejected a shared exec-helper package, since the locked gated_spawn_homes decision rejects a catch-all.
- **Size exceptions.** t-4 and t-24 go past five files. t-4's extra files are compile-forced one-line seam re-points (Tap/Stderr); t-24 is one mechanical rewrite. Splitting either would leave a red intermediate tree.
