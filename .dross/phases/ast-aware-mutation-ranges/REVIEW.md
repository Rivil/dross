# Plan Review — ast-aware-mutation-ranges

Reviewed: 2026-09-19
Plan: 9 tasks across 5 waves

## BLOCKING
(none)

## FLAG
- [antipattern/files] t-1 adds `Construct string` to `EffectiveRange`, but `EffectiveRange` lives in `internal/verify/range_provenance.go:14-20`, which is not in t-1's `files` (only construct.go, range_expand.go and two test files are). The task cannot land as described without editing a file it does not list.
  Suggestion: add `internal/verify/range_provenance.go` to t-1's files.

- [antipattern/files] t-6 changes the `PlanRanges` signature to `(a, files, scope, asts ASTIndex)` and says "collectDetachedFrom passes nil", but `internal/cmd/verify.go` (the call at `internal/cmd/verify.go:704`) is not in t-6's `files`. Without that edit `internal/cmd` does not compile and the commit is red. t-3 and t-5 both edit the same file earlier, so the omission is a listing gap, not a design choice.
  Suggestion: add `internal/cmd/verify.go` to t-6's files.

- [wave-order/contract] t-5 (wave 2) contract "TestVerifyOutputNamesTheConstructPerRange — a run through Verify() with the stub prints … a per-range line naming the construct" cannot be green at wave 2. The run goes through the real `PlanRanges`, which until t-6 still stamps `Pad` and never sets `Construct`, so the only label t-5's own readout can print is `(construct unrecorded)`. Either the test asserts the placeholder at t-5 and is re-pointed in t-6 (the pattern t-6 already uses for the e2e test), or the assertion belongs to t-6.
  Suggestion: reword t-5's third contract to assert `(construct unrecorded)` on the per-range line and no "pad", and move the "names the construct" arm into t-6's contract list.

- [antipattern/justification] t-4's stated reason ("t-7's commit cannot go red on the subprocargs audit, which fails closed on any unlisted literal binary") does not hold. The audit (`internal/cmd/subprocargs_audit_test.go:247-270`) only inspects `exec.Command`/`CommandContext`/`ghCommand` calls with a literal binary; t-7 spawns through `strykerBuildCmd → (*Stryker).buildCmd`, whose `exec.Command(args[0], args[1:]...)` is a spread and is explicitly skipped. Nothing in t-7 calls `argfence.Fence("node", …)` either — argv is two literals. The task is harmless (documents node in the policy, and `TestAuditKnowsEveryPolicyBinary` keeps it consistent) but it is a 3-file, <10-minute task justified by a gate that will not fire.
  Suggestion: fold t-4 into t-7, or keep it with an honest rationale ("node joins the policy table so a future derived operand is refused") and drop the audit-goes-red claim.

- [test-contract] t-9's headline test compares N1 (RunRanges with the changed line alone) against N2 (the construct span) and asserts N1 < N2. That proves the construct span is wider than one line; it does not prove the criterion's claim that "the mutant the pad heuristic lost is generated". A construct expansion that happened to equal the old ±25 window would pass this test. The distinguishing comparison is the pad-era window (`changed line − 25 … changed line + 25`, clamped) vs the construct span — the lost mutant is the function's own BlockStatement/declaration whose span starts above the window.
  Suggestion: compute the old window in the test (the constant is gone from source after t-6; hard-code 25 in the fixture test with a comment naming it as the retired heuristic) and assert N(pad window) < N(construct span), keeping the N(line) < N(construct) arm as a sanity check.

- [granularity] t-6 touches 8 files across two packages. The plan already names it as the one oversized task and the reason (deleting `Pad` from `EffectiveRange`/`LegSummary` turns every pad-pinning test red in the same commit) is sound — there is no green intermediate. Recorded, not contested.
  Suggestion: none beyond the file-list fix above; executor should expect the longest single task here.

## NOTE
- [coverage] Every criterion is covered: c-1 (t-1, t-4, t-6, t-7, t-9), c-2 (t-6, t-8), c-3 (t-1, t-5, t-6), c-4 (t-6, t-7, t-9), c-5 (t-3), c-6 (t-2).

- [locked-decisions] No conflicts. `ast_source` (Babel out of Stryker's tree, node in Workdir, stdin-fed embedded script, consent-gated) is t-7 verbatim; `expansion_unit` (outermost Program.body node incl. Export wrapper, uncovered lines stay bare hunk) is t-1 + t-7's dumper; `provenance_shape` and `ast_fallback_severity` are t-6; `planning_locus` (resolve before PlanRanges, PlanRanges stays pure, never via launcherCommand) is t-6 + t-7's "NEVER via launcherCommand/newLauncher" arm. t-7's up-front `.svelte` refusal matches the deferred item's premise exactly.

- [forbidden-actions] rules.toml has one rule (r-01, `make install` after prompt/Go edits); t-1 and t-8 name it explicitly. No global rules file exists. `runtime.mode = native`, so native `go test` is permitted; t-2 and t-5 correctly route `internal/cmd` through `dross test` (remote), matching the recorded laptop-hang memory.

- [files] Every referenced pre-existing file and symbol resolves: `strykerBuildCmd` (`internal/mutation/stryker.go:595`), `configuredAdaptersFn` (`internal/cmd/verify.go:1012`), `applyReuseReport` (`:187`), `collectDetachedFrom` (`:577`, PlanRanges call at `:704`), `printRangeProvenance` (`:1517`), `stubRangeAdapter` (`verify_scoping_test.go:597`), `e2eSkipReason`, `TestFixtureTestCoversTheSource`, `dross architecture check`, `dross deferred unroute|dismiss`, and all 20+ named existing tests. Files that do not yet exist are all created by the task that lists them.

- [t-2] `internal/cmd/verify_reuse_report_test.go` already exists with two unit tests of `applyReuseReport`; t-2 reads as though it creates the file. It is an append. The survivor line is now `verify.go:116` (spec says 115); the Identifier is symbol-keyed so the id a22f0ab51a69e9e8 survives the drift.

- [t-3] `TestRunScopedLegNeverListsUnsupportedFiles` pins `verify.RunScoped` behaviour but is placed in `internal/cmd/verify_results_test.go`. It works (cmd tests already drive RunScoped through stubs) but a `internal/verify` home would be the natural one.

- [t-8] The residue needles (`hunkContextLines`, `padAndMerge`, `json:"pad"`, `toml:"pad`, `(pad `, `post-pad`, `padded hunk`) currently match only the intended 14 lines in range_provenance.go, verify/verify.go, cmd/verify.go, cmd/verifyscope.go and the three docs — no false positives elsewhere under internal/ or cmd/.

- [t-7] `buildCmd` returns a context-less `exec.Cmd`; the 30s deadline will need a timer-and-kill (or a parallel `CommandContext` seam), not a `ctx` passed through `strykerBuildCmd`. Implementation detail, not a plan gap.

- [strengths] (1) Every contract is falsification-shaped — "if X regresses, TestY fails on exactly Z" — with concrete fixtures (hunk {18,26} over {1,20} → [{1,20},{21,26,"hunk"}]; exit 2 + `{"error":{"line":12,"column":4}}` → "parse error at 12:4"). (2) The pad retirement is staged so no wave leaves a red tree: additive `Construct` first (t-1), cmd readers off `.Pad` (t-5), then one atomic delete (t-6), then a residue scanner with its own tripwire fixture (t-8). (3) The resolver seam is designed to keep `PlanRanges` pure and to prove ordering by counting (`TestResolverIsAskedOnlyForRangeableFilesAndBeforeDispatch`), which is the right shape for the `planning_locus` lock.

## Summary
No blockers; two file-list omissions (t-1, t-6) would produce a non-compiling commit as written, one t-5 contract is unsatisfiable before t-6, t-4's rationale is wrong, and t-9's headline test should compare against the retired pad window rather than a single line to actually prove c-1.
