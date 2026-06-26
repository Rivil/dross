# Plan Review — 12-retrofit-setup-commands

Reviewed: 2026-06-21
Plan: 10 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [check-6 antipattern / factual-error] t-1's description states "rule.md and inbox.md
  have no pre-flight section yet — add the emitter under a new pre-flight step." This is
  wrong for inbox.md: it already has a `## 0. Pre-flight` (inbox.md:7) that runs
  `dross rule show`. Only rule.md actually lacks one (its first heading is `## Parse
  intent`, no pre-flight). Acting on the description literally would add a duplicate
  pre-flight to inbox.md.
  Suggestion: amend the t-1 description to "rule.md has no pre-flight section yet" and
  for inbox.md add `dross interaction show` to the existing pre-flight step.

- [check-3 test-contract] t-9's second test_contract line — "if a rewritten prompt's
  decision points don't match its audit row, the reconciliation is incomplete (caught in
  review)" — names no failing surface; "caught in review" is a manual proxy, not a guard.
  The row-marker conformance is covered by t-10's TestSetupAuditSectionsConform, but the
  decision-point *content* reconciliation has no automated check.
  Suggestion: accept this as a known manual check, or drop the pseudo-contract line so the
  task doesn't imply an automated guard it doesn't have.

- [check-4 granularity / merge-candidate] t-4 (options) edits one file; its substantive
  new work is adding the locked section-pick gate. options.md already walks settings
  section-by-section (§1–§12, Keep/Change/Skip per section, save-per-option), so the
  per-setting half of the task is largely already conforming. The real delta is the single
  new FIRST gate turn — borderline a sub-10-min edit dressed as a full rewrite.
  Suggestion: keep as-is (it carries a distinct locked decision and its own anchor test),
  but scope the edit to adding the gate rather than rewriting conforming sections — the
  retrofit_depth lock permits touching them, it does not require churning them.

- [check-6 / check-2 mirror-task] t-6's quick arm largely re-confirms an already-aligned
  flow: quick.md already uses proceed/steer/show/abort (quick.md:56-60) matching
  execute.md. Under the quick_inbox_mirror lock this is correct (reuse execute's shape
  verbatim), so the work is real but small; pairing it with the inbox per-issue walk in one
  task is reasonable. Noting only that the quick half may land as a near no-op.
  Suggestion: none required; verify quick.md's tokens are byte-identical to execute's
  rather than assuming, since the anchor test keys on token equality.

## NOTE
- [check-1 coverage] All five criteria are covered: c-1 → t-1,t-2; c-2 → t-1,t-2;
  c-3 → t-3,t-4,t-5,t-6,t-7,t-8; c-4 → t-3,t-4,t-5,t-6,t-7,t-8; c-5 → t-9,t-10. No gaps.

- [check-2 locked-decisions] No task contradicts a locked decision. options_walk
  (section-pick gate then per-setting walk) is implemented by t-4; milestone_walk
  (per-criterion, non-goals, phase order) by t-5; quick_inbox_mirror (reuse execute's
  shape verbatim) by t-6; retrofit_depth (uniform restructure of all 7) by t-1 + t-3..t-7.
  All consistent with spec.toml.

- [check-7 forbidden-actions] r-01 (make install after prompt/Go edits) is explicitly
  honored: t-10 runs `make install` then `go test -count=1 ./...` and requires observing
  green before marking done. The plan also correctly defers that observe-and-commit to the
  final wave-3 task rather than batching it — consistent with the global commit-safety
  rules. No violations.

- [check-5 wave-order] Wave structure is sound. Wave-1 (t-1 emitter wiring, t-2 grep
  guards) → wave-2 rewrites (t-3..t-7, each depends_on t-1) → wave-3 (t-8 anchor tests
  depends on all rewrites; t-9 audit flip depends on rewrites; t-10 audit guard + make
  install depends on t-9). Each N+1 task strictly needs N's output. t-2 sits in wave 1
  alongside t-1 but reads source independently, so it does not gate on t-1 — correctly
  marked same-wave, not wave-2.

- [check-3 strength] Test contracts are mostly specific and name the failing surface:
  per-prompt anchors (t-3..t-7 each name the t-8 subtest that breaks), the generic
  TestSetupNoBundledTurns / TestSetupNoArtifactDump nets, and the section-sliced counting
  (AskUserQuestion *within* a promptSection, never whole-file) directly reuse the proven
  phase-11 coreLoop pattern. This is a strong, regression-resistant test design.

- [check-6 strength] The split of t-2 (grep guards, wave 1) from t-8 (anchor + no-bundle
  tests, wave 3) is well-judged: the cheap presence guards can land and fail-red before any
  rewrite, while the structural anchors wait until the rewritten prompts give them stable
  strings to key on. This is the correct ordering, not an artificial two-task split.

## Summary
A well-structured, coverage-complete plan that faithfully implements all four locked
decisions and honors r-01; the only real defect is t-1's factually wrong claim that
inbox.md lacks a pre-flight, which would cause a duplicate-pre-flight edit if followed
literally — fix the description and proceed.
