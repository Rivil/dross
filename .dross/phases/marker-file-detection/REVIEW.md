# Plan Review — 09-marker-file-detection

Reviewed: 2026-06-21 (re-review)
Plan: 7 tasks across 3 waves

## BLOCKING
- (none)

## FLAG
- [granularity] t-6 and t-7 remain test-only single-file tasks (`detect_test.go`, `stack_test.go`). t-6's wave-3 placement is justified (it source-greps the post-edit recon files from t-4/t-5), but t-7 is now a standalone wave-2 task gated only by t-3. Both are well under 10 minutes and are merge candidates with their producing tasks. Suggestion: optional — fold t-7 into t-3-adjacent CLI coverage; not a correctness issue, leave as-is if atomic-commit traceability is preferred.

## NOTE
- [blocking-fix verified] The prior BLOCKING dimension issue is genuinely resolved. t-3 now assigns the hadolint analyzer `dimension="error-handling"` (plan.toml:57), explicitly requires it be in the substantive allowlist, and adds a test-contract row (plan.toml:64) asserting both that the dimension is `error-handling` AND that `internal/quality/catalog_test.go:TestCatalogExcludesCosmetic` stays green after docker.toml embeds. Confirmed in source: `substantiveDimensions` (internal/quality/catalog.go:36-39) contains `ErrorHandling = "error-handling"` (catalog.go:30), and `Catalog()` (catalog.go:140-151) does pull every embedded profile's analyzer, so the guard would indeed have tripped on a missing/non-allowlisted dimension — the fix addresses the real failure path.

- [distinct-scanner-name verified] The dedup-collision invariant is now contract-tested. t-3 (plan.toml:57-58) requires the docker trivy scanner's Name be distinct from the agnostic `"trivy"`, and t-4's test-contract (plan.toml:85) asserts the docker scanner surfaces under its own distinct name `"trivy config"`, NOT collapsed into agnostic `trivy` by the `seen` dedup, explicitly stating that reusing the bare name would fail the c-2 assertion. Confirmed the agnostic scanner is literally `"trivy"` (internal/security/catalog.go:57), so `"trivy config"` is distinct.

- [wave-order fixed] t-7 is corrected to `wave = 2` with `depends_on = ["t-3"]` (plan.toml:125-136); it no longer sits in wave 3 ahead of its only dependency.

- [coverage] All seven criteria remain covered after the amendment: c-1(t-2,t-3), c-2(t-4), c-3(t-5), c-4(t-1), c-5(t-1,t-2,t-6), c-6(t-2,t-4,t-5), c-7(t-3,t-7). No gap introduced.

- [locked-decisions] No new locked-decision conflict. t-3's loadout is hadolint scanner+analyzer + trivy-config scanner with explicit "no dockle/checkov" (docker_tool_loadout); the `error-handling` dimension on the analyzer is consistent with the loadout's "both security and quality dimensions" rationale. The additive seam (t-2 touches neither Detect nor DetectLanguages; t-4/t-5 union via the existing seen dedup) still honors marker_detection_additive, and t-1's new `file_patterns` field leaving exact-match `files` untouched still honors marker_pattern_syntax.

- [strength] The amendments are surgical: they added a dimension assignment and two test-contract rows without restructuring tasks, waves (other than the t-7 fix), or dependencies. No new blocking risk was introduced.

## Summary
The previously-blocking dimension issue is genuinely resolved — t-3 assigns and contract-tests the `error-handling` dimension (confirmed in the allowlist) keeping `TestCatalogExcludesCosmetic` green, the distinct-scanner-name invariant is now name-tested, and t-7's wave is corrected; no new blocking issues, with only an optional granularity flag remaining.
