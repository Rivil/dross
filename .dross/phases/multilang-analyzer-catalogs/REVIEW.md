# Plan Review — multilang-analyzer-catalogs

Reviewed: 2026-06-27
Plan: 8 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [spec-fidelity] t-7 under-proves c-3 as worded. c-3 requires "a dross-quality or
  dross-secure run with the dedicated tools installed surfaces a real finding that
  the agnostic fallback alone does not." t-7's description records "the knip-vs-agnostic
  commands and their outputs" — i.e. raw `knip` vs raw `scc/jscpd/semgrep`. That
  demonstrates the *tool* delta but not that a `dross quality` run surfaces it, which
  is what the criterion names.
  Suggestion: have RUN.md record an actual `dross quality` run on the fixture showing
  knip's finding present, plus an agnostic-only run showing it absent. Note this makes
  t-7's `depends_on = ["t-1"]` genuinely load-bearing (the profile must wire knip) and
  forces a `make install` first per rule r-01 — neither of which the raw-tool framing
  needs, which is why the dependency currently looks loose.

- [test-contract] t-2's third contract conflates `optional` with graceful-skip/no-abort:
  "if osv-scanner is not marked optional, t-5's missing-tool-skip-no-abort subtest for
  dart can't prove graceful skip." That causal claim is wrong — BuildManifest never
  aborts on a missing tool regardless of Optional (Detect just sets Installed=false;
  BuildManifest returns nil). Optional only governs how loudly the CLI warns, not abort
  behavior, so the skip-no-abort property holds whether or not osv-scanner is optional.
  Suggestion: assert graceful skip the way t-5 does (tool in m.Skipped() + nil error),
  and test the Optional flag's effect (warning prominence) separately if it matters.

- [granularity] t-8 is a one-file, sub-10-minute test that largely duplicates t-4.
  t-4 already exercises AnalyzersFor("typescript") and counts knip's dead-code dimension
  toward len>=3; you cannot reach that dimension for typescript without knip being
  present and non-agnostic. t-8's "knip present and distinct from {scc,jscpd}" therefore
  adds little independent coverage.
  Suggestion: fold the knip-distinct-from-agnostic assertion into t-4, or keep it but
  acknowledge it is a thin parallelism-driven file split (the stated "keep wave-2 files
  disjoint" rationale), not separate coverage of c-3.

- [coverage / spec-fidelity] c-2 reading is ambiguous and the plan resolves it in the
  only way that lets dart pass. c-2 says "at least three substantive dimensions (beyond
  today's single complexity tool)." t-4 asserts len>=3 *including* complexity, so dart
  (complexity + dead-code + error-handling) passes at exactly 3 while providing only 2
  dimensions *beyond* complexity. The locked dart_loadout "why" endorses the >=3-total
  reading, so the plan is internally consistent — but if the criterion actually meant
  three *in addition to* complexity, dart's locked loadout under-delivers.
  Suggestion: confirm the >=3-total reading with the spec author before executing; if
  >=3-beyond-complexity was intended, dart needs a 4th dimension and the locked decision
  must change.

## NOTE
- [coverage] c-1's "dross security detect lists it for a typescript repo" clause is never
  exercised at the recon level — t-5 builds manifests for svelte and dart only. typescript
  has no extension unique to it (.ts/.tsx are shared with svelte, which outranks it on the
  winner-take-all Detect path), but DetectLanguages uses the union map (extLangFor), so a
  .ts tree yields BOTH svelte and typescript and osv-scanner still surfaces. t-3 also covers
  ScannersFor("typescript") at the catalog layer. Coverage is adequate, just proven only
  transitively for typescript.
- [rule r-01 / CLI surface] c-1/c-3's "dross security detect" / "dross quality run" clauses
  are proven at the BuildManifest + catalog library layer, never through the actual cobra
  command — consistent with existing repo test style. Any manual `dross` invocation in t-7
  must follow `make install` after t-1 (r-01: Go/profile edits aren't live in the installed
  binary until then).
- [granularity] t-7 touches 5 files, tripping the 5+-file heuristic, but they are one
  cohesive fixture directory of trivially small files (package.json/tsconfig.json/index.ts
  + two record files). A split is not warranted here.
- [locked-decision] This phase deliberately reverses the prior v0.2-era "security_agnostic_only"
  stance. t-3 correctly rewrites TestScannersForLanguageProfilesAgnosticOnly. No conflict
  within *this* spec, but the stale "security_agnostic_only" references in catalog.go's
  comment and the old test docstring will become misleading once dedicated scanners land —
  worth updating in the same pass.
- [spec-fidelity] The dart-analyze -> "error-handling" dimension mapping (t-2) is loose:
  `dart analyze` is a general analyzer, not specifically an error-handling tool. It is the
  locked dart_loadout choice and reaching the 3rd dimension depends on it, so it is in scope,
  but the mapping is the weakest link in dart's c-2 claim.

## Strengths
- [test-contract] NOTE: Test contracts are unusually specific — each names the exact test
  and the precise failure surface (dedup-by-Name collapsing a dimension, losing a Languages
  tag, a non-substantive dimension tripping the cosmetic guard). This is the strongest part
  of the plan and makes each task falsifiable.
- [antipattern] NOTE: The plan correctly identifies and scopes the single green-blocker —
  TestScannersForLanguageProfilesAgnosticOnly asserts svelte/dart/typescript carry ONLY the
  agnostic set, which the new loadouts violate — and narrows its strict-equality loop to
  {kotlin, sql} rather than deleting the guard. Verified accurate: it is the only existing
  test the scanner additions break.
- [granularity] NOTE: c-3 is split per the locked findings_proof decision into a committed
  manual fixture (t-7) plus a wiring-only go-test (t-8), avoiding JS/Dart toolchains in
  `go test`. The plan also anticipates the .ts -> {svelte,typescript} union ambiguity and
  uses unambiguous .svelte/.dart fixtures for the recon tasks.

## Summary
Structurally sound and well-covered with exceptionally specific test contracts; no blockers,
but resolve the c-3 "dross run vs raw tools" framing (t-7), the Optional/no-abort conflation
(t-2), and confirm the >=3-total reading of c-2 before executing.
