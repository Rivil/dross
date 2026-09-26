# Plan Review — cmd-exec-baseline-drain

Reviewed: 2026-09-25
Plan: 15 tasks across 5 waves

## BLOCKING
(none)

## FLAG
- [files] t-13 deletes `gitRemoteOriginURL` ("gitRemoteOriginURL deleted"). But `internal/cmd/doctor.go:101` also calls it (`gitURL := gitRemoteOriginURL(repoDir)`, used for doctor's Remote: section). doctor.go is not in t-13's files, so t-13 as scoped does not compile. That call site is also a new direct git read that has to be routed through gitrun.Read, and the taint audits need to see it.
  Suggestion: add internal/cmd/doctor.go to t-13's files, and name doctor's origin read in the contract alongside seedRemote's (TestInitRawRemoteURLCarriesNoMarker / verb pin on `remote get-url`).

- [files] The standing clause says any census floor a task's run drops is "reset here". But the files that hold those floors are missing from the tasks that will cross them. Today's tree has 42 non-test spawn call sites across 28 files, 23 of them git. Following the plan's own moves, that falls to about 26 sites / 15 files after t-13 and 23 / 13 after t-14, below execConsentMinSites=30 / execConsentMinFiles=20. Those two constants live in internal/cmd/execconsent_audit_test.go, which is not in t-13's or t-14's files. execSourceFloor=37 / execSourceFileFloor=24 live in internal/cmd/taint_audit_test.go, which is in no task's files at all. The 23→4 git spawn consolidation will take the exec-source census under that floor too.
  Suggestion: add execconsent_audit_test.go to t-13 and t-14. Add taint_audit_test.go to each task whose moves cross execSourceFloor/FileFloor (t-12 and t-13 at least, by the counts above).

- [test_contract] t-3's `update.Apply` Options are {Out, APIBase, HTTP, Version, Commit, GOOS, GOARCH, TargetPath, Resync}, with no Check or Force. Two of the 8 tests the contract says move "with identical assertions" depend on exactly those fields. TestUpdateCheckNoApply calls runUpdate with `check: true` and asserts the target is untouched and resync is never called. TestUpdateForce sets `force: true`. So either Options gains Check/Force, or --check/--force stay in cmd and those two tests cannot move to internal/update unchanged. Separately, the fixture helper `sha256hex` (internal/cmd/update_test.go:68) collides with the existing `sha256hex` in internal/update/update_test.go:70 (same package `update`). Moving the helpers to apply_test.go as described fails to compile without a dedup.
  Suggestion: add Check and Force to the Options list, or say which layer owns --check/--force and which of the 8 tests stay in cmd. Note the sha256hex dedup in the description.

- [test_contract] t-15's TestEveryGitSpawnIsInTheRunner does not state what it scans. As written, it fails on "zero exec.Command/CommandContext with a literal "git" outside internal/gitrun" and on "a FuncDecl named gitTrim/gitRead/gitRun/gitNoOut/… outside gitrun". That contradicts t-11: its NEW internal/cmd/git_shim_test.go declares exactly those FuncDecls. It also contradicts 22 existing *_test.go files that spawn `exec.Command("git", …)` for fixtures. t-15's files are only boundary_test.go and exempt_budget_test.go, so it could not reconcile either.
  Suggestion: state the scope in the contract (non-test .go under internal/, testdata skipped). Add a case where a git spawn and a gitTrim FuncDecl in a synthetic x_test.go yield no finding, mirroring the flat ban's own x_test.go case.

- [test_contract] t-15 bans a FuncDecl named `gitStatusRaw` (cleantree.go) outside gitrun. But t-13, which owns cleantree.go, only says porcelain status "goes through Raw". It never says gitStatusRaw is deleted, and a thin wrapper over gitrun.Raw would pass every t-13 contract. The first failure would then land in t-15, which cannot edit cleantree.go. gitBranchTrim in t-14 is only implied deleted, by statusline_test.go's setup-line swap.
  Suggestion: have t-13's contract assert that no non-test FuncDecl named gitStatusRaw or gitRemoteOriginURL remains, as t-11 does for its four helpers. Do the same for gitBranchTrim in t-14.

- [wave order] t-13's and t-14's absolute counts depend on tasks outside their depends_on.
  - t-13's "exec-exempt count goes 20 -> 12" assumes t-12's 25→20 has landed, but t-13 depends only on t-8 and t-11.
  - t-14's "12 -> 9" and "so the os/exec baseline is empty" assume t-13 is done. t-13 is in the same wave (4) and is not a dependency.
  - Plan.NextRunnable happens to run t-12 < t-13 < t-14 (lowest wave, then document order). If t-12 is marked failed, or `dross task move` reorders wave 4, t-13/t-14 become runnable and their contracts are false against a correct tree.
  - t-14 also edits internal/gitrun/gitrun.go and gitrun_test.go, which t-12 edits, without depending on it.
  Suggestion: add t-12 to t-13's depends_on, and t-12 and t-13 to t-14's (t-14 then belongs in a later wave than t-13). Or restate the counts as deltas (t-13: −8 markers, 6 files leave os/exec; t-14: −3, 2 files) so they hold in either order.

- [test_contract] t-12 keeps quality's and security's TestNormalizeSHA "in place targeting gitrun.ShortSHA". That test calls the package-local `normalizeSHA("   \n")`, and its own comment says it "Covers the empty-output branch ShortSHA can't force". Once the ShortSHA copy leaves those packages, normalizeSHA leaves with it. The test then either keeps a dead normalizeSHA alive just to test it, or gets rewritten into something that can't reach the blank-output branch. TestNoTestLost checks names only, so the name survives while the assertion is gone.
  Suggestion: state in the contract what each kept TestNormalizeSHA asserts after the move. Or accept an explicit tests_before.txt edit for those two rows instead of keeping hollow names.

- [granularity] t-12 (18 files: 6 production packages, 3 cmd callers, 4 audit files) bundles two separable changes, each with its own contracts. (a) consolidates the three ShortSHA copies: techdebt/quality/security, their cmd callers and tests, and the scanner marker load-bearing tests. (b) routes codex/consent/remote through gitrun: the codex pins, consent's tracked probe, and remote's ls-files and rev-parse. A failure in (b) holds back (a).
  Suggestion: split into a ShortSHA task and a codex/consent/remote task, both wave 3 and depending on t-9.

- [test_contract] t-8's third contract ("the stack loadout renders differently on a PATH with none of the tools") has nothing to compare against. `stack loadout` is not among t-1's output goldens, which list `stack show` only.
  Suggestion: add `stack loadout` on an empty PATH to t-1's goldens, or name the expected output lines in t-8's contract.

## NOTE
- [coverage] All 8 criteria are covered. c-4 and c-7 are carried by the standing clause on every move task. c-6 is carried by t-2's ratchet self-tests plus t-15's flat ban.
- [locked decisions] No conflicts found:
  - git_runner_scope: all 7 non-cmd sites land in t-12, and all 16 cmd sites are split across t-9, t-13 and t-14. gitrun is stdlib-only, and a synthetic consent import tests that it stays a leaf.
  - gated_spawn_homes: the spawns go to the existing update, testlane, survivor and verify packages, and requireExecConsent stays in cmd.
  - local_store_proof: t-6 adds the positive localstore import and a ban on toml-tagged structs in cmd. The tag check is a heuristic: BurntSushi/toml decodes untagged structs by field name, so an untagged store type would slip past it. t-15's codec ban still keeps the decode itself out of cmd.
- [rules] No task implies a violation of r-01 or runtime.mode=native, and no global rules file exists. t-7 and t-10 phrase some contracts as `dross env set …` and `dross local set …`. Run these in-process, as the existing TestLocal*/TestEnv* tests do. Run against the installed binary, r-01 requires `make install` first or they check stale code.
- [granularity] Most tasks exceed the 5-file threshold. t-10 (24 files) and t-11 (32 files) are single-commit call-site sweeps: deleting the forwarders or helpers requires every caller switched at once, so a split would leave a broken or dual-path intermediate state. Not flagged.
- [wave order] Same-wave file overlap is heavy: boundary_test.go is in all seven wave-2 tasks, and t-10 and t-11 share basebranch.go, doctor.go, ship.go and redproof_replay.go. This is harmless because dross runs one task at a time via NextRunnable, but it means waves 2 to 4 give no real parallelism.
- [coverage] t-1's description says its goldens cover "every command whose output crosses encoding/json or toml", but its list leaves out the commands that only reach JSON through emitJSON: stats, milestone progress, survivor, test --preview, task show, and phase.go's --json. Because emitJSON becomes a one-line delegate, those commands are protected only by render_test's `JSON == Encoder+SetIndent` differential. That is adequate, but narrower than the description claims.
- [strength] The numbers match the live tree:
  - exec-exempt count: 26 today, then 26→25 (t-9: −5 in ship_recover.go and phase.go, +4 in gitrun), →20 (t-12), →12 (t-13), →9 (t-14). The final 9 are update/apply 1, gitrun 4, codex/ast_grep, compilefence, remote's buildCommand and ship/open.
  - Baselines: os/exec 17, json 14, toml 6, net/http 1. Every file maps to exactly one draining task.
  - Also checked: 29 files / 131 helper calls with floors 29/30/8/30 (floor of 0.75×), 8 TestUpdate* tests, 12 setup-calling test files, 20 local.go callers.
- [strength] t-1 and t-2 pin the current state before anything moves, and they anticipate their own breakage. The inventory re-mint is gated by a `comm -23` subset check and proven with TestGitRunGateIsScopedByOrigin, which is confirmed absent from today's tests_before.txt. t-2 foresees the cleantree.go-keyed ratchet subtest going red at t-13. It also catches that filepath.Base keys would collide between internal/testlane/remote.go and internal/remote/remote.go.
- [strength] The contracts name the concrete change that must fail them: MarshalIndent→Marshal, `state get b a` in sorted order, resync before the signature gate, CANARY routing per git verb. The floor-reset rule ("never ahead of the census", with before->after in the comment) stops floors being lowered in advance.

## Summary
The plan is sound and closely checked against the code: full coverage and no locked-decision or rule conflicts. Before execution it needs these fixes:
- t-13 is missing doctor.go.
- Floor files are missing from the tasks whose moves will cross those floors.
- t-3's Options cannot carry the --check/--force tests it moves.
- t-15's git-spawn census needs a stated scope.
- The t-12→t-13→t-14 count chain depends on document order, not depends_on.
