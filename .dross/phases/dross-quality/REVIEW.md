# Plan Review — 06-dross-quality

Reviewed: 2026-06-20
Plan: 8 tasks across 5 waves

## BLOCKING
(none)

All five spec criteria (c-1..c-5) appear in at least one task's `covers`. No task
description, files, or test_contract contradicts a locked decision — checked against
context_model (t-7/t-8 carry calibrate-only + code-only-sweep; t-2/t-4 sweep is
code-only), quality_scope (t-2 has an explicit TestCatalogExcludesCosmetic guard),
ranking_model (t-3/t-5/t-8 say maintainability-risk / blast-radius, never Severity),
report_artifact (t-9 gitignores .dross/quality/), and testable_surface (CLI in
internal/quality + internal/cmd, prompt in assets/, split cleanly). No task implies
a forbidden action under rules.toml (r-01 is about `make install` timing, an
execute-time concern, not a planning violation).

## FLAG
- [granularity] t-6 (`dross quality command + containment`) touches 3 files across
  2 layers (cmd wiring + main registration) and bundles three subcommands
  (detect/run/scaffold) plus read-only containment. This mirrors security.go's
  as-built shape so it is defensible, but it is the heaviest task in the plan and a
  plausible split candidate (e.g. command-wiring vs. containment enforcement).
  Suggestion: leave as-is if security.go was a single comparable unit; otherwise
  consider splitting containment (the c-5 enforcement) into its own task so the
  read-only guarantee gets isolated test pressure.

- [wave-order] t-5 (`scaffold writer`) is in wave 2 and depends only on t-3, which
  is in wave 1. t-3's outputs (Ledger, Survivors, Risk) are genuinely needed, so the
  dependency is real — but nothing forces t-5 into wave 2 rather than running
  alongside t-4 (which depends on t-2+t-3). The waves are consistent; this is just an
  observation that t-5 and t-4 are independent of each other and both gate only on
  wave-1 output. No reordering needed; flagging only that t-5 could equally be
  labelled "wave 1.5" — the current grouping is fine.
  Suggestion: no action; the depends_on graph already expresses the true ordering and
  the executor parallelises within reachable deps regardless of the wave label.

- [test-contract] t-7's contract is the weakest in the plan: items 1-3 are
  "Prompt must contain <phrase>" assertions but t-7 itself ships no test (its files
  are the two .md assets). The real gate is t-8. The contract leans on "verified by
  t-8's content assertions" (item 4) without t-7 owning any executable check.
  This is structurally identical to how 05-dross-secure split prompt-authoring (t-7)
  from prompt-assertion (t-8), so it matches precedent — but a reader could mistake
  t-7's contract for self-verifying.
  Suggestion: keep, but treat t-7's "contract" as authoring acceptance notes; the
  binding gate is t-8. Ensure t-8 lands in the same phase (it does, wave 5).

## NOTE
- [strengths] Test contracts are unusually specific and negation-framed — most name
  the exact test and the mutation that should break it (e.g. "drop the suffix loop and
  TestNewRunNoClobber sees one dir overwritten and fails", "a high-complexity finding
  on a cold path sorts BELOW a moderate one on a core/hot path"). This is the right
  shape for mutation-tested verify and a clear strength.

- [strengths] Locked-decision fidelity is high and deliberate: t-3/t-5/t-8 consistently
  say "maintainability-risk, NOT nominal category" and "blast-radius", directly
  encoding the ranking_model lock; t-2 carries an affirmative
  TestCatalogExcludesCosmetic guard for quality_scope; t-7/t-8 spell out
  calibrate-only/downrank/never-suppress AND the code-only-sweep note for the two-part
  context_model lock. The plan defends the locks rather than merely not violating them.

- [strengths] The plan mirrors the shipped 05-dross-secure file-for-file
  (run/catalog/findings/recon/scaffold + cmd + standalone gitignore_test +
  prompt-content test), so the file paths and task shapes match a known-good as-built
  precedent. The one cosmetic divergence — t-9 gitignores `.dross/quality/` (trailing
  slash) vs. the existing `.dross/security/` (no slash) — is harmless; both satisfy
  `git check-ignore`.

- [coverage] c-5 (read-only) is defended in depth: enforced at the writer (t-1
  TestNewRunWriteBoundary), gitignored at t-9, and re-checked at the CLI (t-6
  containedPath / prefix walk). Triple coverage of the safety-critical criterion is
  appropriate, not redundant.

## Summary
A clean, precedent-faithful plan with specific contracts and strong locked-decision
fidelity; no blocking issues and only minor granularity/wave observations worth a
glance before execution.
