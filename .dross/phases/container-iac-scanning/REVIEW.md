# Plan Review — container-iac-scanning

Reviewed: 2026-06-27
Plan: 6 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [test-contract / locked-decision] t-1's test_contract claims "if the file_patterns set
  drops *.tf.json or *.hcl, t-2's per-pattern match rows fail" — but the planned t-2 does
  not actually enforce that. t-2(a) "mirrors TestFilePatternMatch", which in the repo
  (profile_test.go:160-191) drives the matcher from a hardcoded `dockerFilePatterns` var
  *decoupled from the TOML* ("Pinning it here keeps the matcher's contract honest
  independent of the TOML file"). t-2(b) "mirrors TestEmbeddedDocker", which only asserts
  `FilePatterns` non-empty + `Exts` empty (embed_test.go:33-38) — it never checks the
  specific patterns. Result: the locked `marker_patterns` set (the exact 5 globs) is NOT
  pinned against the shipped terraform.toml. Dropping or renaming a pattern in the file
  would pass every test in the plan. The contract overstates the protection it provides.
  Suggestion: have TestEmbeddedTerraform assert the loaded profile's FilePatterns equals
  the exact locked set, OR drive TestTerraformFilePatternMatch from the loaded profile
  rather than a hardcoded list — then the t-1 contract becomes true.

- [coverage / regression-guard] No negative "tflint does not leak into a marker-less repo"
  test. The docker phase shipped no-leak guards (TestBuildManifestNoMarkerRegression /
  TestQualityNoMarkerRegression for c-6); this plan proves only positive surface (t-3a/t-4a)
  and skip-when-missing (t-3c/t-4b). The scanner side is incidentally covered — the existing
  security regression test already asserts "trivy config" absent in a Go repo
  (recon_test.go:137), and terraform reuses that exact name — but tflint has no such guard
  anywhere (TestQualityNoMarkerRegression only checks hadolint). This phase's spec has no
  no-leak criterion, so it is not a coverage failure, but a new analyzer reaching every repo
  on a signals misconfig would go uncaught.
  Suggestion: add a one-line assertion that a Go-only repo's quality manifest does not
  contain "tflint" (extend t-4 or the existing regression test).

## NOTE
- [strengths] Clean wave structure: a single wave-1 data task (the profile) with all five
  verification tasks fanning out in parallel in wave-2, each genuinely depending on t-1.
  Wave ordering is correct and parallelism is maximal — no over-serialization.
- [strengths] Faithful mirroring of the docker precedent (test names, fixture layout, the
  locked-decision rationale carried into the header comment), and explicit awareness of the
  manifest dedup pitfall — t-3(b) pins that distinct "trivy config" survives alongside the
  agnostic "trivy" each exactly once.
- [antipattern] t-1's header comment names dockle as out-of-scope, inherited from docker.toml
  and from spec c-4 ("why dockle/checkov are out of scope"). dockle is a Docker-image linter,
  irrelevant to Terraform; checkov is the substantive Terraform tool being declined. Harmless
  and spec-mandated (not actionable by the plan), but the dockle mention is incongruous for an
  IaC profile.
- [granularity] t-5 introduces a new file internal/stack/profiles_doc_test.go for the
  doc-presence guard, whereas the analogous docker guard (TestReadmeDocumentsZeroCodeDropIn)
  lives in embed_test.go. Minor organizational choice; both compile fine in package stack.

## Summary
Solid, precedent-faithful plan with complete criterion coverage, no locked-decision conflicts,
and no rule violations — the one finding worth acting on is that the locked marker-pattern set
is not actually pinned against the shipped terraform.toml despite the t-1 contract claiming it
is.
