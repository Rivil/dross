# Plan Review — ci-timing-trend

Reviewed: 2026-11-26
Plan: 3 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [locked-decision / test-contract] The trend rule as t-1 describes it ("warns when the fit reaches 540s within the next 10 runs") has no positive-slope condition, so it contradicts c-3's "a flat or falling history draws none" for any history that already sits near the cap. A flat history at ~545s (this run 530s, so under 90% and trend-eligible) fits above 540 at n+1 and warns; so does a history falling from 600s to 545s. t-1's contract "with 5+ flat or falling points returns none" only holds if its fixtures use low values, and nothing pins the high-value case.
  Suggestion: t-1's description should require slope > 0 as well as the reach test, and its contract should add a flat-near-cap and a falling-near-cap series, each asserting no warning.

- [locked-decision / test-contract] trend_rule (locked) says "the last 10 main entries", but t-1 says "the last 10 entries carrying the package". For a package missing from some recent entries, t-1 reaches further back than the locked text allows and leaves the x-axis unclear (entry index or point index). The boundary contract "fit reaches 540.0 at run n+10 warns; 539.x does not" also never defines n: is it the last history entry, or the current run, which the history excludes? That is a one-run offset, and an off-by-one there is exactly what this boundary test is meant to catch.
  Suggestion: t-1 should pick one reading and say it. Either use the last 10 window entries, skipping those without the package and indexing x by entry position, or amend the decision text. Also define n in the contract as the x of the newest history entry, so "within the next 10 runs" has one meaning.

- [coverage / c-6] Plan and spec both miss that actions/cache keys are immutable. On a re-run of the same main commit, restore hits the exact key `test-timing-<sha>` (the first attempt's saved history), upsert replaces that commit's entry in memory, and then the save under the same key is refused with a "cache already exists" warning. So the persisted store keeps the first attempt's numbers. "Never adds a second" holds; "replaces that commit's entry" holds only inside the run. On that re-run, the loaded history also already contains this commit's own entry, so the "computed from history as loaded (this run excluded)" in t-2 doesn't hold: baseline and trend include the same commit's earlier timings. Every c-6 test in the plan is unit-level or run()-level, and none touches the cache layer.
  Suggestion: t-2 should drop the GITHUB_SHA entry from the loaded history before computing baseline and trend. The plan (or a spec note) should record that a re-run's save is a no-op, so verify doesn't map c-6 to a persisted-replacement claim the cache can't deliver. A failed save on re-run also adds an annotation to the run, and the t-3 description should mention it.

- [coverage / c-1] c-1 says "Every CI `go test` run on main persists its per-package totals", but t-3 deliberately leaves the mutation-ts `go test` (ci.yml:202) without TESTSUMMARY_HISTORY. The exclusion is correct: a `-run`-filtered single-package stream would pollute the internal/mutation baseline. But it contradicts the criterion's literal wording, and verify will have to explain the gap.
  Suggestion: either have t-3's guard contract assert that mutation-ts does *not* carry TESTSUMMARY_HISTORY, so the scope is pinned rather than incidental, or narrow c-1's wording to the test job's run before verify.

- [antipattern / companion files] t-3's guard has to read a step's `with:` (key, restore-keys, path), `env:` (TESTSUMMARY_HISTORY) and `if:`. But the scanner it is told to mirror, `workflowSteps` in internal/cmd/ci_go_test_step_test.go, records only job/uses/shell/run (the wfStep struct at line 18). The guard can only be built by extending wfStep, in a file t-3 doesn't list, or by writing a second line parser in the same package. This is the companion-file gap that cost five asks in milestone-closeout-check.
  Suggestion: list internal/cmd/ci_go_test_step_test.go in t-3's files if the scanner is to be extended, or state in the description that the guard brings its own parser.

- [prose gate] t-3 opens with "Before editing ci.yml, audit it against the CI supply-chain hardening checklist", which is a before-X-do-Y gate with no `preconditions` entry. I'm not marking it BLOCKING because it can't be encoded as `{run, max}`: it's a judgement audit against ~/.claude/memory/reference_ci_supply_chain_hardening.md, not a countable command. The mechanical parts (SHA pins, `${{` in run:) are already enforced by TestWorkflowActionsArePinned and TestWorkflowsHaveNoExpressionsInRun. Still, as written, an executor can skip it without a trace.
  Suggestion: make the audit leave a visible output, e.g. "findings listed in the task's commit body or surfaced to the user before the ci.yml edit". That way skipping it shows.

## NOTE
- [wave order] t-3 sits in wave 3 only because its ARCHITECTURE.md file:line refs need t-2's code. The ci.yml wiring and guard depend only on the env name TESTSUMMARY_HISTORY, which the plan already fixes, so they could run alongside t-2. Under sequential solo execution this costs nothing.
- [CI wiring] TESTSUMMARY_HISTORY must be an `env:` value, because TestWorkflowsHaveNoExpressionsInRun bans `${{` in run:. A step `env:` value is not shell-expanded, so `$RUNNER_TEMP/...` there is a literal string. Used consistently in env and both cache `path:` inputs, the literal still passes t-3's equality contract while quietly caching a workspace-relative `$RUNNER_TEMP` directory. Use `${{ runner.temp }}/...` or a plain workspace path.
- [existing refs] ARCHITECTURE.md's ci.yml refs are already stale (go test step listed at ci.yml:78, actually at :81; concurrency at :18, actually at :22). t-3's architecture-check contract will catch this, so expect to fix more lines than the new entries.
- [strength] The boundary contracts are exact and two-sided: 540.0 warns and 539.9 doesn't for the cap, the reach boundary is tested the same way, and a 4-point vs 5-point series tests the minimum-history gate. t-3's "checker run against synthetic workflows reports each break separately … cannot go green by matching nothing" follows the repo's established anti-vacuous guard pattern.
- [strength] Exit-code invariance is tested in both directions: a failing stream with warning history still exits 1, and a passing stream with cap and trend warnings still exits 0. That protects the one property c-5 and the existing run() doc comment both rely on.
- [strength] Gating history on TESTSUMMARY_HISTORY (unset means no read, no write, no note) keeps local runs and the mutation-ts leg free of side effects, and gives t-2 a clean run()-level seam to test c-5 and c-6 end to end without touching CI.

## Summary
The plan is sound and well specified. Before execution, fix the trend rule's missing positive-slope condition (it currently contradicts c-3 near the cap) and acknowledge the cache-immutability limit on c-6; nothing blocks it.
