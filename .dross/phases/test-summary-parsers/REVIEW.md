# Plan Review — test-summary-parsers

Reviewed: 2026-11-02
Plan: 13 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [granularity / file collision] t-4's last contract ("TestSharedErrorBounded fails" when a re-stamped 240-byte shared name exceeds BlockBytes) targets a test that lives in `internal/runsummary/jsrunner_test.go:146`. That file is not in t-4's `files`. It is in t-6's `files`, and t-6 also runs in wave 1. t-4 can do one of two things, and both are bad. If it edits jsrunner_test.go outside its scope, it races t-6. If it declares a new `TestSharedErrorBounded` in summary_test.go, the duplicate symbol stops package `runsummary` from compiling.
  Suggestion: either add jsrunner_test.go to t-4's files and make t-6 `depends_on` t-4 (moving t-6 to wave 2), or rename the contract to a new test in summary_test.go that drives Render over a shared-header vitest block.

- [antipattern: one task that should be two] t-7 covers c-7 and c-8. These are two unrelated surfaces in different packages. c-7 is the stryker-net SetStream/MultiWriter head in `internal/mutation`, with a manual red-proof against stryker_net.go. c-8 is verify collect's `detachFetchLog`/`classifyFetch`/`collectHostLog` in `internal/cmd`. They share no code, seam or fixture. The only reason to bundle them is that both came from deferred backlog items.
  Suggestion: split t-7 along the c-7 / c-8 line so each red-proof and commit stands alone. Both halves stay in wave 1.

- [test contract specificity] Two contracts depend on fixtures that no `files` list includes. t-6's TestVitestConsoleFailNotAFailure asserts "Failures.Total == the fixture's own Tests N failed N" (a red run) and also that "the exit-0 variant renders PASS" (a green run), but only `vitest_console_fail.txt` is listed. t-5's TestGoHashHeadingNotABuildBlock says "(green variant renders PASS)" with only `hash_heading.txt` listed, and t-5 pins TestGoFixturesCaptured's minimum at exactly 9 (6 existing + 3). If the variants are made by editing a captured file inside the test, they are synthesized fixtures, which fixture_provenance's rationale rules out. If they are captured separately, the files are missing from the plan.
  Suggestion: either list the green/exit-0 variant files in t-5 and t-6 (and adjust the TestGoFixturesCaptured minimum), or state that hash_heading.txt is itself the green run and drop "variant".

- [test contract specificity] The shape of t-13's `red_summarised` is ambiguous. The description adds one `red_summarised` field to flowLeg, but the contract says "per-parser red_summarised = true on [after]". A single bool on a leg cannot show which of gradle/dart/pytest fell back, so the contract as written can't be implemented against the field the description adds.
  Suggestion: pin the shape in t-13: either three fields (`gradle_red_summarised`, …) alongside the three `*_red_bytes`, or one bool that the agent sets only after observing all three summarised.

- [test contract specificity] t-10's TestMixedRedBothRunners feeds "t-6's vitest red capture". t-6 produces two fixtures, `vitest_console_fail.txt` and `vitest_each_bracket.txt`, and the contract names neither. If console_fail is used, the merge test also depends on t-6's banner-confinement fix, so a regression there shows up as a merge failure.
  Suggestion: name the exact file in the contract. `vitest_each_bracket.txt` is the less coupled choice.

- [coverage / criterion fidelity] c-2 says "A `flutter test` / `dart test` run", and t-12 will make the README name "dart test". Every t-2 fixture is a `flutter test` capture from feastahead/phone. Plain `dart test` support rests on the two sharing package:test's reporters, not on any captured run. Under fixture_provenance (feastahead only for this runner) that is a defensible position, but the plan doesn't say it anywhere.
  Suggestion: state in t-2's description that `dart test` is claimed through reporter equivalence with no live capture, so verify can map c-2 honestly instead of treating the `dart test` half as proven.

- [wave order / replay] t-11 checks its patches with `git apply --check` at "its repo's HEAD" when t-11 runs. t-13 runs in wave 3, after all the code tasks, and replays them "verbatim". By then equerry, feastahead or sunday-rider may have moved and the patches may no longer apply. Separately, every t-11 contract except the glob one names a test that only t-13 creates (`checkTestSummaryParsersFlow`, the TestFlowCostRecords fields), so nothing automated gates t-11's commit.
  Suggestion: have t-11 record each repo's sha in flow-cost-before.toml (the fixture headers already carry `<repo>@<sha>`) and have t-13 replay at that sha. Also accept explicitly that t-11 is gated by hand (`git apply --check`, porcelain-clean) until t-13 lands.

- [test contract specificity] t-8's description covers two cases where the failing step spawned nothing: "host lock, argv validation". The contracts cover only host lock (TestFallbackTailFromFailingStep with `testHold` → ErrTransport) and a failing prepare spawn. If the argv-validation path still shows an earlier spawn's tail, nothing fails.
  Suggestion: either add the argv-validation case to TestFallbackTailFromFailingStep's contract or drop it from the description.

## NOTE
- [granularity] t-1 (5), t-2 (7), t-3 (8), t-5 (5) and t-10 (6) go over the 5-file mark only because of captured fixtures. Each has one or two Go files in a single layer, so none of them is a real candidate for splitting.
- [locked-decision check] t-5 captures its go fixtures from a scratch module, not one of the three named repos. This doesn't conflict with fixture_provenance, which scopes to gradle/flutter/vitest/pytest. It keeps the existing go1.27.1 header pin (panel D7).
- [forbidden actions] Checked against r-01 (project) and the global rules (none at ~/.claude/dross/rules.toml). No violations. t-13 runs `make install` and checks `dross version` before measuring. t-11 builds the base binary from `git archive`, not a worktree. The capture header rules out `dross test` from a worktree.
- [strength] Shared files are serialized on purpose: t-4→t-10 (summary.go/render.go/summary_test.go) and t-8→t-9 (test_output.go, with the reason stated). New parsers register via `init()`/`Register`, so wave-1 tasks t-1/t-2/t-3 never touch summary.go and don't collide with t-4.
- [strength] Each locked decision has a test that would fail if it were broken: TestGradleReadsNoFiles (no XML/init script), TestNoCrossClaims plus TestPytestClaimsOnlyItsSummary (strict claims), TestDartReportersAgree/TestPytestFormatsAgree (both reporter formats), and the `*FixturesCaptured` headers (provenance). TestAmbiguousClaimFallsBack is rewritten, not renamed, so TestNoTestLost stays satisfied.
- [strength] The plan matches the real wrappers. feastahead's `scripts/dross-test.sh` runs `set -eo pipefail` with flutter first, so a live both-red bare run can't happen, and D4's composed both-red case is correct. sunday-rider's `selftest.sh` collects failures and keeps going, so pytest does run after a mypy break, and TestPytestGreenUnderRedWrapper's fixture can be captured as described.

## Summary
The plan is sound and well grounded. There are no blockers, but fix the t-4/t-6 TestSharedErrorBounded collision before wave 1 starts, and tighten the fixture and field ambiguities (variant fixtures, red_summarised shape, t-10's vitest input) before execution.
