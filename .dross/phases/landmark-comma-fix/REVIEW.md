# Plan Review — landmark-comma-fix

Reviewed: 2026-07-24
Plan: 2 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [test-contract-specificity] t-1's contracts never pin the boundary rule's defining discriminator: a comma segment that *contains* `=` but whose key is unrecognized (e.g. `what=uses x=1, y=2` — segment `y=2` must JOIN the value, not error). This is also a deliberate behavior change: today `what=a, y=2` errors with "unknown landmark key", after the fix it silently joins — so it falls through both c-2 (it wasn't currently-valid) and c-3 (which only pins `color=blue` as a *first* segment). None of the four t-1 contract lines would fail if an implementer split on every `<word>=` instead of only recognized keys.
  Suggestion: Add one contract line to t-1: "if unrecognized key= segments stop joining, the test asserting ParseLandmark(\"what=a, y=2\") yields What == \"a, y=2\" (no error) fails".

## NOTE
- [forbidden-actions] No rules violation: both tasks verify via `go test` against source (runtime.mode = native), so r-01 is not contradicted. But r-01 is severity=hard and the 2026-06-20 stale-binary incident is on record — after t-1/t-2's Go edits, `make install` must run before any manual end-to-end check of c-1 or before verify/ship exercises the installed binary. The plan doesn't need a task for this, but the executor should know.
- [strengths] Test contracts are genuinely specific: each names an exact input and the exact assertion that breaks (`ParseLandmark("feature=x, feature=y")` returns error; `"a, b"` round-trips through record+show). The malformed-input contract reuses the exact cases already in TestParseLandmark (changes_test.go:238-247), so regression coverage is anchored to real existing tests, not imagined ones.
- [strengths] Locked decisions translated faithfully: t-1's description restates dup_keys (parse error, not last-writer-wins) and value_join (preserve original text, trim only value ends) verbatim; t-2 documents exactly the no_escape_syntax contract in help text and introduces no quoting mechanism.
- [strengths] Right-sized plan: 2 tasks, 2 files each, one layer each, wave order justified (t-2's e2e test genuinely requires t-1's parser fix — the old parser errors on `what=a, b`). No granularity inflation, no vapor files — all four referenced files exist.

## Summary
Sound, well-anchored plan with no blocking issues; the single flag is a missing contract for the unrecognized-`key=`-joins case, the one input class where a wrong implementation would pass every listed test.
