# Plan Review — telemetry-taxonomy-overhaul

Reviewed: 2026-07-25
Plan: 4 tasks across 2 waves

## BLOCKING

(none)

- Coverage is exact: c-1→t-1, c-2→t-2, c-3→t-3, c-4→t-4. Every criterion appears in exactly one `covers`.
- Locked decisions honored: t-1 implements `rule_table` directly; t-3 explicitly restricts fixes to reorder/token-tightening "bucket names untouched per bucket_stability".
- No conflict with rules.toml r-01 (make-install staleness is an execution concern, not a plan defect).
- All referenced files and tests exist or are created by the plan: `testdata/error_corpus.jsonl` (27 rows), `classifyCases`/`TestCorpusShapesClassifyAsExpected`/`TestCorpusOtherShareUnderCeiling`/`TestReadmeDocumentsDetailAllowlist` are all real; `taxonomy_test.go` is new and declared.

## FLAG

1. **t-2's `files` list understates its writes.** The description commits to "Fix (reorder or tighten tokens in t-1's table) anything real it surfaces", which is an edit to `internal/telemetry/telemetry.go` (and likely `telemetry_test.go` if a classify case moves) — but `files` declares only `taxonomy_test.go`. Undeclared writes break the atomic-commit/file-scope expectation. Either add the files or move any table fix into t-3 (which already owns reorder/tightening).

2. **Wave-2 write collision on the same two files.** t-3 and t-4 both declare `internal/telemetry/telemetry.go` + `internal/telemetry/telemetry_test.go`, and per finding 1 t-2 may touch them too. If waves imply parallel execution, three tasks writing the classifier table and its test file concurrently will conflict; if wave 2 is executed sequentially anyway, fine — but then the plan should say which order (t-2 before t-3 is the sensible one: get the guard in place before t-3 starts reordering, so any t-3 reorder that introduces shadowing fails immediately).

3. **t-4's second contract overstates the existing test.** "if a new safety-net bucket is added without README mention, the existing bucket-list doc test plus the tier test catch the omission" — the existing `TestReadmeDocumentsDetailAllowlist` checks a *fixed five-name list* (`unknown_flag`, `arg_count`, `landmark_parse`, `config_io`, `env_token`), not completeness against the code's bucket set. A new bucket omitted from the README passes it today. For that contract to be true, t-4 must add a table-driven completeness check (iterate t-1's rule table, assert each bucket name appears in the README list) — which the rule table makes cheap, but the task description doesn't ask for it.

## NOTE

1. **The guard's rule-pair scoping is load-bearing.** I scanned the current switch's full token order: there is no earlier-token ⊂ later-token pair *across* rules (the guard should pass on the ported table without forcing changes — t-2's "fix anything real" is precautionary). But there IS a same-rule substring pair: `"marshal state"` ⊂ `"unmarshal state"` inside state_io (harmless — same bucket). t-2's "rule pairs (i<j)" wording correctly excludes it; the implementer must keep the comparison cross-rule only, or this pair false-positives on day one. Excluding multi-token matchers as shadow *sources* is also semantically correct (an all-tokens-required matcher can't guarantee unreachability).

2. **t-4's dependency on t-1 is soft.** The tier prose could be written against today's switch; the real reason to sequence it after t-1 is avoiding conflicting edits to telemetry.go's package doc during the rewrite. Legitimate, just not the "strictly needs wave-1 output" kind.

3. **Strengths.** (a) Every test contract is falsifiable and names the exact test that fails, including the seeded self-check in t-2 that guards the guard itself. (b) t-1's "corpus + classifyCases stay green untouched" makes the behavior-identical claim mechanically checkable rather than asserted — the 27-row corpus plus ~60 classify cases pin both buckets and precedence (e.g. "board: … is not set" pins board-before-env_token). (c) t-3's ambiguous-shape list matches c-3's enumeration one-for-one.

## Summary

No blockers — coverage, locked decisions, and file references are all sound; the three flags are file-scope honesty on t-2, a wave-2 write collision to sequence deliberately, and one t-4 contract that needs a completeness check added to be true.
