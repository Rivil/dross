# cmd-exec-baseline-drain — panel synthesis

## Scores

| Dimension | risk (25 tasks / 7 waves) | mvp (8 / 3) | verification (22 / 5) |
|---|---|---|---|
| Criteria coverage | 4: covers all 8, and its c-4 coverage is the strongest (stale-pin checks, a verdict contract per move). It keeps `gitRemoteOriginURL` as a cmd wrapper, although c-3 names that helper as replaced. | 3: covers all 8 on paper, but c-7 rests on json_show tests that decode with `json.Unmarshal`, so they can't catch changed bytes. It has no whole-tree CLI golden, and nothing stops a cmd file adding a json/toml import until the final ban. | 5: writes the ideal contract for each criterion first. Standing contracts S1–S5 keep c-4 and c-7 true at every commit. It is the only draft that noticed tests_before.txt is out of date. |
| Test-contract specificity | 4: byte goldens, stub-git behaviour and synthetic proofs that each guard fails. It misses the same-name TestNoTestLost trap in the ShortSHA tests. | 3: concrete CANARY and stub checks, and the correct marker baseline of 26. But "passes unedited, so the bytes are unchanged" is false, and its call-site counts are guesses (~170). | 4: every existing test it cites exists (checked), new tests are named, and it catches the multiplicity trap. Its errors: marker baseline 27 (should be 26), rename size 164 (131 are in production code), and it misses pathfence/fields.go. |
| Granularity | 2: 25 tasks. Several are 1–2 test-file edits (t-17 test moves, t-18 floors), and each still costs a ~10-min gate plus a pair-mode turn. | 3: cheapest overhead. But t-7 (~50 files: rename, 16 direct sites, audits and floors) and t-6 (~25 files) make a failing gate hard to trace. | 3: clean single-purpose tasks, but sibling moves share a contract and could share a gate (t-4/t-5, t-6/t-7, t-10/t-14, t-19/t-20). |
| Wave correctness | 4: dependencies are explicit and real, and running the rename last avoids file overlap. t-18's floors rest on a projected end state. | 2: six wave-1 tasks share two audit files. t-7 depends only on t-1 yet rewrites files that t-5 and t-6 also touch, and t-6's contract is unsure whether t-5 has landed. The non-cmd git sites move before the Trim pin looks outside internal/cmd. | 4: right in the main but inconsistent. t-21 waits on t-13 because of shared files, yet t-16 (cleantree.go, which t-13 renames) does not. t-15 and t-13 share four production files in wave 3. |

**Skeleton: verification.** It has the best criterion coverage and contracts, its standing-contract model maps directly onto per-task gates, and it had the fewest factual errors. I kept its task boundaries and regrouped them to cut per-task overhead.

Checked in the source to settle conflicts:
- **Exec-exempt baseline is 26.** `directiveMarkers` (internal/cmd/directive_test.go:45) walks only `f.Comments` and requires `strings.CutPrefix(c.Text, "//dross:exec-exempt")`. `internal/cmd/trust.go:124` is the comment `// //dross:exec-exempt …`, so the prefix check fails. `internal/diag/trust.go:239` is a string literal inside `note(…)`, so it is never in `f.Comments`. 28 grep hits − 2 = 26, which is mvp's number. Verification's 27 discounted only trust.go.
- **Rename size is 131 call sites in 29 non-test cmd files** (plus 4 definitions), as risk said. The 168 `gitX(` hits include 33 in test files: 15 in 12 setup files and 18 in the two audits.
- **tests_before.txt predates exec-taint-enumeration** (last written by #130). TestGitRunGateIsScopedByOrigin, TestNoSpawnOutputEscapes, TestInitRawRemoteURLCarriesNoMarker, TestScannerRevParseMarkerIsLoadBearing and TestStreamSitesCarryNoMarker are not in it. TestNoTestLost counts packages per name, so moving both quality's and security's `TestShortSHAFallback` into one package reads as a drop.
- **Pinned gate lists and the binary allowlist can go stale silently.** TestToolchainSpawnsResolveAsGated fails only when the whole list has no sites. `acceptedNonLiteralBinaries` is keyed by `filepath.Base`. TestStreamSitesCarryNoMarker scans only `taintMarkersIn(t, "internal/cmd")`.
- **pathfence must be updated with the store move.** `internal/pathfence/fields.go:291-296` declares `cmd.localStore`, `cmd.detachedRun` and `cmd.remoteCandidate`, and pathtaint_audit_test.go fails on an "unresolved declaration". Only risk listed this file.
- **The rename-stable surgery anchor is unique.** `consent.RunConsented(` occurs once in run.go (line 174), so verification's anchor is safe.

## Merged plan

What was grafted onto the verification skeleton:
- **From risk:**
  - per-command output byte goldens (t-1)
  - stale-pin checks and allowlist keys by repo-relative path (t-2)
  - the pathfence re-point (t-6)
  - the ship_recover probe and the rev-parse pin additions in the runner task (t-9)
  - its grouping of the direct cmd git sites (t-13, t-14)
  - the exhaustive file × import ban loop and locking the ceiling at the end (t-15)
- **From mvp:**
  - merged sibling moves (t-4, t-5, t-8, t-12)
  - the marker baseline of 26
  - settings.json in internal/hooks (also risk's choice)

**Standing contracts.** Every task must leave these green, on top of its own lines:
- **S1 (c-4):** TestEverySpawnSiteGatedOrExempt and TestNoSpawnOutputEscapes are green. A moved marker that clears nothing is itself a finding.
- **S2 (c-4):** TestExecExemptBudget (armed in t-2) holds: parsed directives ≤ 26. Expected path: 26 → 25 (t-9) → 20 (t-12) → 12 (t-13) → 9 (t-14), then locked at 9 (t-15).
- **S3 (c-1/c-2/c-8):** TestCmdBoundaryByImportDirection is green. A task that drains a file removes that file's baseline entry in the same commit, because a stale entry fails.
- **S4 (c-7):** TestCLITreeGolden and t-1's output goldens stay byte-identical, and TestNoTestLost is green against the re-minted inventory. cmd end-to-end tests change setup identifiers at most, never an assertion. Where setup churn would be wide, use a `_test.go` shim, following the consent_shim_test.go precedent.
- **S5 (floors):** if a task's own run drops a census under execConsentMinSites/MinFiles, execSourceFloor/FileFloor, gitTrimSiteFloor or gitHelperSiteFloor, that task resets the floor to floor(0.75 × logged count) and records before→after in the comment. The floor self-tests, TestDroppingAScanRootFailsTheFloor and the "internal/cmd alone must not meet the source floor" check stay green. Floors never drop ahead of the census.

```
Phase cmd-exec-baseline-drain — 15 tasks across 5 waves

Wave 1
  t-1  Pin CLI tree, output bytes, test inventory                    [verification+risk]
       files:      cmd/dross/surface_test.go (NEW), cmd/dross/testdata/cli_tree.txt (NEW),
                   internal/cmd/output_golden_test.go (NEW), internal/cmd/testdata/cli_surface/output/*.golden (NEW),
                   internal/cmd/testdata/cli_surface/tests_before.txt
       desc:       Mint, from the pre-phase tree, one golden of newRoot()'s whole tree (use/short/aliases/hidden, every
                   flag's type/default/usage). Also mint exact stdout goldens for every command whose output crosses
                   encoding/json or toml, reusing cli_surface_test.go's goldenCheck. Re-mint tests_before.txt by running
                   only TestTestNamesRecorded under DROSS_UPDATE_GOLDEN=1.
       covers:     c-7, c-8
       depends_on: —
       contract:   dropping `update --check`, renaming `local get`, or flipping any flag's default/hidden bit fails
                   TestCLITreeGolden with a diff naming the command; a missing golden fails, not passes
       contract:   swapping state show's MarshalIndent for Marshal, dropping reentry's trailing newline, or emitting
                   `state get b a` in sorted key order each fails its output golden byte-for-byte (goldens cover
                   project/milestone/defaults/profile/stack show ± --json, state show, state get, changes/verify-scope/
                   task/deferred/watch --json, reentry, ship --json); two runs mint identical bytes (temp paths normalised)
       contract:   the re-minted inventory is a superset (`comm -23 old new` empty); deleting TestGitRunGateIsScopedByOrigin
                   (absent from today's inventory) now fails TestNoTestLost naming it

  t-2  Arm codec ratchet, exempt ceiling, stale-pin checks           [verification+risk+mvp]
       files:      internal/cmd/boundary_test.go, internal/cmd/exempt_budget_test.go (NEW),
                   internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go,
                   internal/cmd/subprocargs_audit_test.go
       desc:       Add encoding/json + BurntSushi/toml to forbiddenInCmd with today's 14 json + 6 toml importers, one entry
                   per line, and rebuild the ratchet self-tests on synthetic entries. Add TestExecExemptBudget (ceiling 26).
                   Every gated/marked/stream pin must hold ≥1 live site; acceptedNonLiteralBinaries is keyed by repo-relative path.
       covers:     c-4, c-6, c-8
       depends_on: —
       contract:   a synthetic issue.go importing encoding/json (or toml) yields exactly one finding naming issue.go and the
                   import; baselining ship.go for toml, which it does not import, fails as stale
       contract:   on a clone of the live map with every forbidden import stripped and an empty baseline, every
                   TestCmdForbiddenImportRatchet subtest still passes (today's cleantree.go-keyed subtest goes red at t-13)
       contract:   TestExecExemptBudget counts via directiveMarkers over non-test internal/ and cmd/ files: a synthetic tree with
                   2 directives, one `// //dross:exec-exempt` prose comment and one string-literal mention counts 2, and a
                   ceiling of 1 fails "2 > 1"; the live count is 26
       contract:   pointing execConsentGatedFiles at internal/cmd/issue.go (no spawn) fails naming the entry; an
                   `exec.Command(argv[0])` in a synthetic internal/testlane/remote.go is NOT accepted by the
                   internal/remote/remote.go key; a key whose site vanished fails as stale

Wave 2 (each depends t-1, t-2; run in id order — shared audit files are edited on disjoint, one-per-line entries)
  t-3  Move update flow into internal/update                         [risk+verification]  (see D3)
       files:      internal/update/apply.go (NEW), internal/update/apply_test.go (NEW, moved from internal/cmd/update_test.go),
                   internal/cmd/update.go, internal/cmd/update_test.go (deleted), internal/cmd/boundary_test.go,
                   internal/cmd/execconsent_audit_test.go, internal/cmd/subprocargs_audit_test.go
       desc:       runUpdate (fetch, minisign verify, checksum, extract, swap, self-exec resync) and extractBinary/Zip become
                   update.Apply(ctx, Options{Out, APIBase, HTTP, Version, Commit, GOOS, GOARCH, TargetPath, Resync}); cmd's
                   Update() only builds Options. The 8 TestUpdate* tests and their fixture helpers move with the code.
       covers:     c-2, c-1, c-4, c-7
       depends_on: t-1, t-2
       contract:   the 8 moved tests pass in internal/update with identical assertions; resyncing before the signature gate or
                   swapping on a bad checksum fails TestUpdateRefusesOnMissingSignature / TestUpdateRefusesOnBadChecksum
                   (target bytes unchanged)
       contract:   TestUpdateSelfExecIsTheOnlyMarkerHere, re-pinned to internal/update/apply.go, finds exactly one marked site
                   whose reason contains "verif"; the accepted key becomes "internal/update/apply.go:newBinary"; S2 unchanged
       contract:   TestNoTestLost sees each TestUpdate* once, in update; update.go leaves the os/exec and net/http baselines
                   (net/http now empty); TestCLITreeGolden still shows --check, --force and hidden --api-base

  t-4  Move suite, slot, install spawns to testlane                  [mvp; verification t-4+t-5; risk t-5+t-13]
       files:      internal/testlane/spawn.go (NEW), internal/testlane/spawn_test.go (NEW), internal/cmd/test.go,
                   internal/cmd/run.go, internal/cmd/lane_install.go, internal/cmd/lane_plan.go, internal/cmd/redproof_replay.go,
                   internal/cmd/boundary_test.go, internal/cmd/execconsent_audit_test.go,
                   internal/cmd/subprocargs_audit_test.go, internal/cmd/taint_usercmds_test.go
       desc:       runLocalCommandCtx, runSlotCommand, runRemoteCommand (keeping the Command(argv[0]) + Args form), the install
                   spawn, shArgv/shArgvFor and exec.LookPath move 1:1 to testlane. cmd keeps its seam vars, installStderr and every
                   consent call; redproof_replay reads exit codes via an ExitCode() interface.
       covers:     c-1, c-4, c-7
       depends_on: t-1, t-2
       contract:   execConsentGatedFiles names internal/testlane/spawn.go in place of test.go, run.go and lane_install.go, and
                   TestToolchainSpawnsResolveAsGated sees every testlane site gated and unmarked; in TestReachProofIsLoadBearing,
                   ungating run.go's unchanged RunConsented anchor yields an "ungated" finding in internal/testlane/spawn.go
       contract:   TestStreamSitesCarryNoMarker's scan widens beyond taintMarkersIn(t, "internal/cmd"), so a synthetic
                   taint-cleared marker in testlane/spawn.go fails it
       contract:   TestReplayRedVsGreen: `exit 3` still gives Red with ExitCode 3; TestLaneInstallOutputGoesToStderr sees
                   CANARY-LANE on stderr and not in the error; RunSlot with a nil *os.File leaves Stdin unset (`cat` exits 0)
       contract:   the key "internal/cmd/test.go:argv[…]" becomes "internal/testlane/spawn.go:argv[…]" (a leftover old key fails
                   as stale); test.go, run.go, lane_install.go and redproof_replay.go leave the os/exec baseline

  t-5  Move drain and detach spawns to domains                       [mvp; verification t-6+t-7; risk t-6+t-7]
       files:      internal/survivor/drain.go (NEW), internal/survivor/drain_test.go (NEW), internal/verify/detach.go (NEW),
                   internal/cmd/survivor_drain.go, internal/cmd/survivor_drain_test.go, internal/cmd/verify.go,
                   internal/cmd/verify_results_test.go, internal/cmd/boundary_test.go,
                   internal/cmd/execconsent_audit_test.go, internal/cmd/taint_usercmds_test.go
       desc:       The `go list` spawn (with its taint-cleared marker), runCoverageProfile and readRawReport/notCoveredPositions
                   with their JSON decode move to survivor; runDetachArgv's body becomes verify.RunDetachArgv. cmd keeps the
                   seam vars, requireExecConsent and verify.IsTestdataPath; classifyFetch reads rsync's exit via ExitCode().
       covers:     c-1, c-4, c-7, c-8
       depends_on: t-1, t-2
       contract:   new TestDeletingDrainsGateFlagsItsSpawns: ungating survivor_drain.go's requireExecConsent() turns both
                   internal/survivor/drain.go sites into findings; without the surgery, both are gated and unmarked
       contract:   new companion to TestDeletingVerifysGateFlagsEveryMutationSpawn: ungating verify.go's byte-identical anchor
                   flags internal/verify/detach.go; TestSurgeryAnchorMustMatch still reports "matched 2 times"
       contract:   new TestClassifyFetchReadsARealExitStatus: a real exit error from `sh -c 'exit 23'` classifies exactly as
                   remote.Classify("rsync", host, 23)
       contract:   the moved notCoveredPositions tests (names kept) still return "read mutant statuses" on malformed JSON; a coverage
                   run with no `go` on PATH reports "unknown", never "not covered"; TestDrainHasNoLocalTestdataRule green;
                   survivor_drain.go leaves os/exec + encoding/json, verify.go leaves os/exec

  t-6  Extract local.toml store into localstore                      [verification t-11; risk t-9+t-14; mvp t-5]
       files:      internal/localstore/store.go (NEW), internal/localstore/store_test.go (NEW, moved tests),
                   internal/cmd/local.go, internal/cmd/remote_pool.go, internal/cmd/local_test.go,
                   internal/cmd/local_detached_test.go, internal/cmd/local_remote_alias_test.go,
                   internal/pathfence/fields.go, internal/pathfence/fields_test.go, internal/cmd/boundary_test.go
       desc:       The store and its toml codec, the key table, detached runs, allow-hosts/grants/remote-env, mutation tuning,
                   readLocalKey and the grant-store adapter move to localstore; local.go keeps Local/localGet/localSet plus thin
                   forwarders, which t-10 deletes. resolveRemoteHost moves beside probeRemotePool; pathfence declarations re-point.
       covers:     c-5, c-8
       depends_on: t-1, t-2
       contract:   the store-level tests move with identical assertions (TestDetachedRunRoundTripsEveryField,
                   TestSecondRunForOnePhaseIsRefused, TestNewKeyWinsOverAlias, TestRemoteGrantPairResolvesTogether,
                   TestUnreadableStoreIsNotASilentLocalRun, TestLocalStoreRoundTripsGrants,
                   TestReadRemoteGrantRefusesTrackedLocal); TestNoTestLost counts them as moved, not copied
       contract:   extractedPackages gains internal/localstore (dropping the import fails "wiring: … does not import
                   …/localstore"); a synthetic cmd `type x struct{ A string `toml:"a"` }` gives one finding naming file and x,
                   and a json-only struct gives none
       contract:   leaving pathfence's declarations on cmd.localStore / cmd.detachedRun / cmd.remoteCandidate fails pathtaint's
                   resolvePathSources with "unresolved declaration … no such struct field"; re-declared on localstore it is
                   green and pathfence_carrier_test still resolves every carrier
       contract:   TestLocalSetGetRoundTrips, TestLocalRejectsUnknownKey and TestRemoteGrantKeysAreNotSettable pass unchanged; a
                   grant Save keeps quick_base/remote_host/detached_run entries; local.toml bytes unchanged; local.go leaves toml

  t-7  Move settings.json codecs out of cmd                          [verification t-12; risk t-11+t-21; mvp t-6]
       files:      internal/hooks/settings.go, internal/hooks/settings_test.go, internal/cmd/env.go, internal/cmd/env_test.go,
                   internal/statusline/settings.go, internal/statusline/settings_test.go, internal/cmd/statusline.go,
                   internal/cmd/boundary_test.go
       desc:       env.go's readSettings/mutateSettings (decode, indent, 0700 dir, 0600 tmp+rename, trailing newline) move to
                   hooks.ReadSettings/MutateSettings, taking their two TestMutateSettings* tests. statusline.go's
                   existingStatusLineCommand becomes statusline.ExistingCommand.
       covers:     c-8, c-7
       depends_on: t-1, t-2
       contract:   `dross env set K V` on a settings.json holding a foreign key keeps that key, leaves mode 0600 with a trailing
                   newline and no `<path>.tmp`; a malformed file still fails with the "parse <path>:" prefix; an empty file reads as {}
       contract:   statusline.ExistingCommand returns "x" for {"statusLine":{"command":"x"}} and "" for malformed JSON; the
                   clobber refusal still names the existing command
       contract:   TestEnvListMasksValues and TestEnvCover_UnsetPropagatesMutateError pass unchanged; env.go and statusline.go
                   leave the encoding/json baseline

  t-8  Add render package; route JSON/TOML output                    [verification t-10+t-14; risk t-10+t-15+t-16; mvp t-6]
       files:      internal/render/render.go (NEW), internal/render/render_test.go (NEW), internal/stack/runtime.go,
                   internal/cmd/{changes,deferred,dotget,jsonout,reentry,ship,state,task,verifyscope,watch}.go,
                   internal/cmd/{defaults,milestone,profile,project,stack}.go, internal/cmd/boundary_test.go
       desc:       render exposes today's exact shapes: JSON (indented encoder + newline), MarshalJSON, MarshalJSONIndent, TOML.
                   emitJSON stays a one-line delegate; the 10 JSON and 5 TOML emitters switch, keeping their writers, dotget's
                   argument order and cmd's `# <path>` headers. stack.go's exec.LookPath becomes stack.LookPath.
       covers:     c-8, c-1, c-7
       depends_on: t-1, t-2
       contract:   render_test differentials on a value with nested tables, omitempty and "<&>": JSON == Encoder+SetIndent("","  "),
                   MarshalJSON == json.Marshal (HTML escaping kept), MarshalJSONIndent == json.MarshalIndent, TOML ==
                   toml.NewEncoder by default and with Indent "  " (so collapsing stack.go's explicit Indent changes no bytes)
       contract:   every t-1 output golden stays byte-identical, which catches a call site that picked the wrong shape or writer
                   (e.g. `state get b a` still in argument order)
       contract:   the stack loadout renders the same on a PATH with none of the tools; the 15 files leave their json/toml
                   baselines and stack.go also leaves os/exec; extractedPackages gains internal/render

  t-9  Add leaf gitrun runner; teach audits selectors                [verification t-8+t-9; risk t-4+t-12]
       files:      internal/gitrun/gitrun.go (NEW), internal/gitrun/gitrun_test.go (NEW), internal/cmd/ship_recover.go,
                   internal/cmd/phase.go, internal/cmd/boundary_test.go, internal/cmd/execconsent_audit_test.go,
                   internal/cmd/taint_gitplumbing_test.go, internal/cmd/subprocargs_audit_test.go,
                   internal/cmd/testdata/subprocargs_audit/snippets.txt;
                   setup-only: internal/cmd/{ship,switchbranch,basebranch,gitseparator,verifyscope}_test.go
       desc:       A stdlib-only package with exactly four spawns: Trim (ref verbs; exempt + moved taint marker), Raw (untrimmed;
                   Read = TrimSpace(Raw)), Run (failure output to gitrun.Stderr) and Quiet, plus ArgvRecorder and ExitCode. The
                   four cmd helpers become delegates; ship_recover's probe uses Quiet; audits learn `gitrun.<Verb>(…)` repo-wide.
       covers:     c-3, c-4, c-1
       depends_on: t-1, t-2
       contract:   against a stub git: a failing Run's error lacks CANARY while Stderr holds "git <verb>:" + CANARY; a failing Quiet
                   puts CANARY in neither; Raw keeps a leading " M" that Read trims; non-ancestor merge-base gives ExitCode()==1;
                   every argv starts `-C <dir>` and reaches ArgvRecorder
       contract:   leaf rule: a synthetic gitrun importing internal/consent gives exactly one finding; TestGitRunGateIsScopedByOrigin,
                   retargeted to gitrun's Run and Trim spawn lines, keeps only the Run-origin finding; new
                   TestGitrunContentVerbsCarryNoMarker fails if a taint-cleared marker binds inside Raw or Read
       contract:   `gitrun.Trim(dir, "log", "--oneline")` in an internal/security file trips TestGitTrimRunsRefVerbsOnly, naming
                   file:line; `gitrun.Trim(dir, "remote", "get-url", "origin")` and `rev-parse --git-path x` trip while
                   `rev-parse --short HEAD` / `--is-inside-work-tree` pass; `gitrun.Quiet(dir, "rev-parse", branch)` is flagged by
                   TestNoUnseparatedGitPositional and the "refs/heads/"+branch form is not; an aliased gitrun import is a finding
       contract:   S2 goes 26→25; TestShipRecoverFetchFailureOutputGoesToStderr passes with only its setup swapped to gitrun.Stderr;
                   TestLiveTreeHasNoUnresolvedDispatch green; ship_recover.go leaves the os/exec baseline

Wave 3
  t-10 Rewire cmd onto localstore; shrink local.go                   [verification t-15; risk t-14+t-17]
       files:      internal/cmd/local.go, internal/cmd/local_shim_test.go (NEW),
                   internal/cmd/{trust,verify,remote_grant,test,remote_pool,test_lane_install,doctor,remote_bootstrap_cmd,issue,
                   gitignore,ship,run,remote_bootstrap,redproof_replay,lane_preview_locality,lane_preview,lane_locality,
                   lane_install,basebranch,test_lane}.go, internal/cmd/execconsent_audit_test.go, internal/cmd/boundary_test.go
       desc:       The 20 cmd callers call localstore directly and local.go's forwarders are deleted. The cmd test identifiers
                   (grantStore, detachedRun, recordDetachedRun, loadLocal, localPath …) move into a test-only shim, and the run.go
                   surgery anchors become the rename-stable `consent.RunConsented(`.
       covers:     c-5, c-7
       depends_on: t-6
       contract:   new TestLocalGoIsOnlyTheCommandTree fails on any type/var/const in internal/cmd/local.go or any func not
                   returning *cobra.Command, naming a leftover forwarder
       contract:   `consent.RunConsented(` matches exactly once in run.go; TestSurgeryLeavesTheSharedGraphAlone and
                   TestUserCommandFixKeepsTheSurgeryAnchors still land every surgery; an anchor still spelling grantStore(root)
                   fails "anchor never matched"
       contract:   `git diff --stat` shows no cmd *_test.go change besides the shim and the two audit files; `dross local set
                   trusted_test_command x` and `local set remote_host h` still refuse

  t-11 Replace git helper calls with gitrun                          [verification t-13; risk t-24]
       files:      internal/cmd/{basebranch,cleantree,doctor,forkpoint,milestone,milestone_merged,milestone_stale,originpush,
                   phase_backfill,phase_checkout,phase,phase_reconcile,phase_lifecycle,redproof,redproof_lifecycle,
                   redproof_repoint,redproof_set,redproof_replay,repair_files,repair_state,repair,repair_phasedirs,ship,
                   secretscan,ship_recover,status,switchbranch,topology,verifyscope}.go, internal/cmd/git_shim_test.go (NEW),
                   internal/cmd/subprocargs_audit_test.go, internal/cmd/taint_gitplumbing_test.go
       desc:       The 131 production calls to gitTrim/gitRead/gitRun/gitNoOut become gitrun.Trim/Read/Run/Quiet and the four
                   delegates are deleted. A test-only shim keeps the 12 setup-calling test files unchanged; gitCallFuncs and
                   gitHelperSiteFloor are keyed per gitrun verb, and the ident form is dropped.
       covers:     c-3, c-7
       depends_on: t-9
       contract:   TestGitHelperCallSiteFloor's per-verb totals equal the pre-rewrite counts (Run 39, Trim 40, Read 11, Quiet 41)
                   and stay ≥ floors 29/30/8/30; a call site that fell out of the audit lowers its verb's count and fails
       contract:   TestGitTrimRunsRefVerbsOnly examines ≥ gitTrimSiteFloor (30) gitrun.Trim sites with zero problems; no non-test
                   FuncDecl named gitTrim/gitRead/gitRun/gitNoOut remains
       contract:   the 12 setup-calling test files are byte-identical; `git diff --stat` shows only the 29 production files, the
                   shim and the 2 audits

  t-12 Route non-cmd git spawns through gitrun                       [verification t-19+t-20; risk t-22+t-23; mvp t-1]
       files:      internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go, internal/techdebt/run.go,
                   internal/techdebt/run_test.go, internal/quality/run.go, internal/quality/run_test.go, internal/security/run.go,
                   internal/security/run_test.go, internal/cmd/techdebt.go, internal/cmd/quality.go, internal/cmd/security.go,
                   internal/codex/git.go, internal/consent/store.go, internal/remote/remote.go,
                   internal/cmd/taint_scanners_test.go, internal/cmd/taint_gitplumbing_test.go,
                   internal/cmd/execconsent_audit_test.go, internal/cmd/subprocargs_audit_test.go
       desc:       One gitrun.ShortSHA ("nogit" on failure) over Trim replaces the three copies and their markers. codex's log goes
                   through Read, consent's tracked probe through Quiet, remote's ignore listing through Raw (consumer marker kept)
                   and its work-tree probe through Trim. codex/git.go leaves the spawn pins and gitrun joins execConsentMarkedFiles.
       covers:     c-3, c-4
       depends_on: t-9
       contract:   TestNoTestLost is green without editing the inventory: techdebt's TestShortSHADegradesToNogit and
                   TestNormalizeSHAFallsBackOnEmpty move to gitrun; quality's and security's same-named TestShortSHAFallback and
                   TestNormalizeSHA stay in place and target gitrun.ShortSHA; run dirs carry the same short SHA
       contract:   TestScannerRevParseMarkerIsLoadBearing and TestEveryScannerMarkerIsLoadBearing keep their names and files
                   (TestScopedGuardsSurvive); deleting Trim's taint-cleared marker yields findings that start at gitrun's Trim
                   spawn and escape in each of security, quality and techdebt
       contract:   TestExecConsentScansTheSpawningPackages and TestAuditScansCodexPackage re-pin internal/codex/git.go →
                   internal/gitrun/gitrun.go (left on codex they fail "holds no spawn site"); TestHelperPackageSpawnsAreMarked
                   and TestRemoteMarkerNamesTheCallerCheck green
       contract:   a tracked local.toml still makes consent refuse, naming the file, and an untracked one does not; remote's rsync
                   exclude keeps a path with spaces, and a non-git dir falls back; S2 goes 25→20; S5 applies

Wave 4
  t-13 Route direct cmd git reads through gitrun                     [risk t-19+t-20; verification t-16+t-18+t-21]
       files:      internal/cmd/cleantree.go, internal/cmd/worktree_files.go, internal/cmd/pause.go, internal/cmd/techdebt.go,
                   internal/cmd/init.go, internal/cmd/phase.go, internal/changes/changes.go, internal/changes/changes_test.go,
                   internal/cmd/taint_gitdirect_test.go, internal/cmd/boundary_test.go
       desc:       Porcelain/untracked status and `ls-files -z` go through Raw; pause's symbolic-ref goes through Trim and its
                   redundant marker is deleted. phase.go's two `git show` reads go through Read and a new changes.Decode. init
                   reads origin in seedRemote through Read (gitRemoteOriginURL deleted) and resolves binaries via stack.LookPath.
       covers:     c-1, c-3, c-4, c-8
       depends_on: t-8, t-11
       contract:   a repo whose first porcelain line is " M a.go" still yields "a.go" (status column survives Raw); techdebt keeps
                   "with space.go" and a name containing "\n" intact (NUL split, no trim)
       contract:   TestNoSpawnOutputEscapes: a pause marker kept after the move is reported "clears nothing", and deleting a
                   Raw-consumer marker (cleantree's porcelain line) reports the escape
       contract:   TestPhaseGitShowOutputStaysOffTheError (no CANARY-SHOW) and TestRemoteURLUserinfoNeverPersists (no CANARY-TOK)
                   pass unchanged; TestInitRawRemoteURLCarriesNoMarker, retargeted to seedRemote, finds its gitrun.Read call
                   unmarked, and routing `remote get-url` through Trim trips the verb pin
       contract:   changes.Decode round-trips Base and PR, and garbage returns an error (phaseRefRecordedBase still returns "");
                   the 6 cmd files leave os/exec and phase.go also leaves encoding/json; S2 goes 20→12; S5 applies

  t-14 Route timeout and stdin git through gitrun                    [risk t-21; verification t-17+t-21]
       files:      internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go, internal/cmd/statusline.go,
                   internal/cmd/statusline_test.go (setup line), internal/cmd/milestone_stale.go,
                   internal/cmd/taint_gitplumbing_test.go, internal/cmd/taint_scanners_test.go, internal/cmd/boundary_test.go
       desc:       gitBranchTrim becomes a Trim options form (2s timeout, --no-optional-locks) on Trim's one spawn, now
                   CommandContext; gitHelperSpawnLines and spawnLineIn learn CommandContext. milestone_stale's diff→patch-id uses
                   Raw plus Raw's new stdin form, and isAncestor reads merge-base's exit via gitrun.ExitCode.
       covers:     c-1, c-3, c-4
       depends_on: t-7, t-11
       contract:   a stub git that sleeps 5s makes the timeout form return within 3s, with recorded argv starting
                   --no-optional-locks; statusline tests still give "main" on a branch, the short SHA detached and "" outside a repo
       contract:   without the finder fix, TestGitRunGateIsScopedByOrigin fails fatally "found no exec.Command"; internal/gitrun
                   still holds exactly one Trim spawn and one Raw spawn
       contract:   new TestIsAncestorOverARealRepo: ancestor → (true,nil), non-ancestor → (false,nil) via exit 1, missing ref → error;
                   patch-id over a two-file diff equals `git diff | git patch-id --stable` by hand; empty diff → ""; TestStale* unchanged
       contract:   a `--format=%(contents)` call through Trim's options form trips the pin; statusline.go and milestone_stale.go
                   leave os/exec, so the os/exec baseline is empty; S2 goes 12→9

Wave 5
  t-15 Flatten ratchet into ban; census git spawns                   [verification t-22; risk t-25; mvp t-8]
       files:      internal/cmd/boundary_test.go, internal/cmd/exempt_budget_test.go
       desc:       Delete cmdForbiddenBaseline and checkBoundary's baseline parameter. os/exec, net/http, go/ast, encoding/json and
                   BurntSushi/toml become a flat ban on non-test internal/cmd, with a table-driven ban self-test. Add
                   TestEveryGitSpawnIsInTheRunner and lock TestExecExemptBudget at the final count (9).
       covers:     c-6, c-1, c-2, c-3, c-4, c-5, c-8
       depends_on: t-3, t-4, t-5, t-7, t-8, t-10, t-12, t-13, t-14
       contract:   an exhaustive loop on a clone injects each of the 5 imports into each non-test internal/cmd file, and each
                   injection yields exactly one finding naming both; the same import in a synthetic x_test.go or internal/foo
                   yields none; the live tree is green
       contract:   TestFlatBanHasNoAllowlist requires reflect.TypeOf(checkBoundary).NumIn()==1 and no package-level var in
                   boundary_test.go matching (?i)baseline|allow — re-introducing an allowlist fails one of them
       contract:   TestEveryGitSpawnIsInTheRunner finds zero exec.Command/CommandContext with a literal "git" outside
                   internal/gitrun and ≥1 inside (non-vacuous); a synthetic `exec.Command("git","status")` in internal/codex fails,
                   and so does a FuncDecl named gitTrim/gitRead/gitRun/gitNoOut/gitBranchTrim/gitStatusRaw/gitRemoteOriginURL/
                   ShortSHA outside gitrun
       contract:   one added //dross:exec-exempt directive now fails TestExecExemptBudget; TestNoTestLost and TestCLITreeGolden green
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 os/exec baseline empty (incl. phase/survivor_drain/test/verify) | t-3, t-4, t-5, t-8, t-9, t-13, t-14, t-15 |
| c-2 net/http baseline empty | t-3, t-15 |
| c-3 one git runner repo-wide, verb split kept | t-9, t-11, t-12, t-13, t-14, t-15 |
| c-4 verdicts kept, audits green every move, markers do not grow | t-2, t-3, t-4, t-5, t-9, t-12, t-13, t-14, t-15 (plus S1/S2/S5 on every task) |
| c-5 local.toml persistence in its own package | t-6, t-10, t-15 |
| c-6 flat ban with bite-proving self-tests | t-2, t-15 |
| c-7 no CLI change; e2e unedited; tests move; TestNoTestLost | t-1, t-3, t-4, t-5, t-7, t-8, t-10, t-11 (plus S4 on every task) |
| c-8 no json/toml in cmd; domain decode; one render package; banned | t-1, t-2, t-5, t-6, t-7, t-8, t-13, t-15 |

Each of the 17 os/exec baseline files has a draining task:
- t-3: update
- t-4: test, run, lane_install, redproof_replay
- t-5: survivor_drain, verify
- t-8: stack
- t-9: ship_recover
- t-13: cleantree, worktree_files, pause, techdebt, init, phase
- t-14: statusline, milestone_stale

Each of the 20 codec files has one too:
- t-5: survivor_drain
- t-6: local
- t-7: env, statusline
- t-8: the 10 JSON emitters and the 5 TOML emitters
- t-13: phase

## Disagreements

**D1 — How many tasks (risk 25, verification 22, mvp 8).**
- risk gives every guard, move, floor re-derivation and test move its own task.
- verification splits by single purpose.
- mvp packs the work into 8 tasks, several of them 25–50 files.
- **Default: 15 tasks.**
  - Verification's boundaries are kept.
  - Sibling moves merge where mvp already groups them: the two testlane tasks become t-4, survivor + verify become t-5, render + JSON become t-8, and ShortSHA + codex/consent/remote become t-12.
  - The direct cmd sites follow risk's grouping (t-13, t-14).
  - The runner and the audit teaching share one task (t-9). No draft merges those two, but both risk and verification run them back to back, and the audits only need to know the selector form before any call site writes it, which t-9 still guarantees.
- **Why it matters:** each task costs a ~10-minute gate plus a pair-mode turn, but a failing gate in a 10–30-file task is harder to trace. If you want easier tracing, split back along verification's lines in this order: t-9 (runner / audits), t-12 (ShortSHA / others), t-5 (survivor / verify).

**D2 — How the cmd git helpers get replaced, and when the rename runs.**
- mvp does it in one task (t-7): delete the helpers, rewrite every call, move the 16 direct sites, re-point the audits and reset floors, ~50 files.
- risk and verification stage it: the runner sits behind one-line delegates, the audits learn the `gitrun.X(…)` form, then a mechanical rename follows. They then split on timing:
  - risk runs the rename last (t-24), so the delegates live all phase and the audits read both forms throughout.
  - verification runs it in wave 3, before the phase/recover/stale sites.
- **Default:** staged, with the rename (t-11) before the direct-site tasks that edit the same files (t-13, t-14).
- **Why it matters:** in mvp's version a broken selector case shows up only as an unexplained floor drop somewhere in 50 files. Renaming first retires the ident form early, so t-13 and t-14 are written against one form.

**D3 — How much of `dross update` moves.**
- mvp keeps runUpdate in cmd and adds `update.HTTPDoer` and `update.SelfInstall`, so cmd's update_test.go is untouched.
- risk and verification move the whole flow into `update.Apply(ctx, Options)`, and the 8 TestUpdate* tests move with it.
- **Default:** move the whole flow.
- **Why it matters:** c-2 says the "HTTP client and release fetch move into internal/update". mvp leaves the fetch, verify and swap sequencing in cmd, and still has to change `update.Client.HTTP` (a `*http.Client` today) to an interface. mvp's zero-test-edit advantage is real, but c-7 explicitly allows tests to move with their code.

**D4 — How output bytes are pinned while the codecs move.**
- risk mints per-command stdout goldens before anything moves.
- verification relies on render differential tests (render.X produces the same bytes as the call it replaces).
- mvp relies on the existing json_show tests passing unedited. They decode with `json.Unmarshal`, so indent, trailing-newline and key-order drift would pass.
- **Default:** both risk's goldens (t-1) and verification's differential tests (t-8).
- **Why it matters:** differential tests prove each render function is correct, but only per-command goldens prove each call site picked the right function and writer (dotget's argument order, Print vs os.Stdout).

**D5 — Who re-derives the spawn and source floors.**
- risk gives it a dedicated task (t-18) that sets floors ~25% under a projected end state, before the moves that shrink the counts.
- verification uses standing rule S5: whichever task drops a census under a floor resets it from the logged count.
- mvp resets them inside its big git task.
- **Default:** S5.
- **Why it matters:** S5 saves a task and needs no projection. The cost is that a task can go red on a floor and need a second ~10-minute run. risk's objection, that nobody knows in advance which task crosses, is recorded; switch to risk's t-18 (placed after t-9, before t-12) if one extra task is cheaper for you than a surprise re-run.

**D6 — When the 7 git sites outside cmd move.**
- mvp moves them in t-1, together with creating the runner, to prove early that gitrun is a leaf with no import cycle.
- risk and verification move them only after the audits learn the selector form.
- **Default:** after (t-12 depends on t-9).
- **Why it matters:** TestGitTrimRunsRefVerbsOnly walks only internal/cmd today. Routing the scanners' ShortSHA through `gitrun.Trim` before the pin walks every package leaves those Trim calls outside the verb pin.

**D7 — How the local.toml extraction is split.**
- mvp does it in one task: store plus ~20 callers, ~30 files.
- verification does it in two: extract with forwarders, then rewire the callers, add the test shim and the local.go structure rule.
- risk uses three tasks and keeps a `grantStore(root)` wrapper so run.go's surgery anchor never changes.
- **Default:** verification's two (t-6, t-10), with the anchor `consent.RunConsented(` (checked: it occurs once in run.go).
- **Why it matters:** forwarders keep each step green. risk's wrapper contradicts the "local.go is only the command tree" rule that all three drafts want, unless it moves to another file.

**D8 — How many spawns gitrun has.**
- verification: exactly four (Trim, Raw, Run, Quiet). Read is TrimSpace(Raw), and the timeout and stdin forms reuse existing spawns.
- risk: one spawn line per verb, adding separate Read and stdin spawns.
- mvp does not say.
- **Default:** four.
- **Why it matters:** every spawn costs an exec-exempt marker, so the final count is 9 here versus ~11. risk's underlying concern, that Trim's taint-cleared marker must never clear Read/Raw content, still holds because Trim keeps its own spawn line.

**Minor, settled by judge:**
- The exempt baseline is 26 (mvp); verification's 27 counts diag/trust.go's string literal.
- The rename covers 131 production calls in 29 files (risk); 164 and ~170 include test-file hits.
- `Quiet`, not `NoOut`.
- classifyFetch uses an exit-code interface in cmd (risk + verification) rather than moving into internal/remote (mvp).
- The settings.json codec goes to internal/hooks (risk + mvp), not a new claudesettings package (verification).
- shArgv/shArgvFor move to testlane with the spawns (risk + mvp); lane_plan.go and lane_install.go's validation call `testlane.ShArgvFor`.
- gitRemoteOriginURL is deleted (mvp + verification), not kept as a wrapper (risk).
- The gitrun leaf rule is kept (risk + verification) even though mvp called it redundant.
- The codec ratchet is armed in wave 1 (risk + verification), not only at the end (mvp).
- The ceiling test goes in a new exempt_budget_test.go (verification) and is locked at the final count in t-15 (risk).
- The git census lives in boundary_test.go (verification), not a new gitrunonly_test.go (risk).
- The install spawn goes into testlane/spawn.go (risk + mvp), not a separate install.go (verification).
- The helper is named stack.LookPath (risk), not HostLookPath or SystemLookPath.
- ship_recover's rev-parse probe moves in the runner task (risk), not later (verification).
