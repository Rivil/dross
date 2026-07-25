# Plan Review — context-hygiene

Reviewed: 2026-07-23
Plan: 7 tasks across 2 waves

## BLOCKING
(none)

Coverage is complete — every criterion appears in at least one `covers` field
(c-1: t-3/t-6, c-2: t-5, c-3: t-1/t-2/t-7, c-4: t-4, c-5: t-1/t-4/t-7). No task
contradicts a locked decision; all four locked decisions are explicitly threaded
to their implementing task. No rules.toml violation (r-01 is an executor process
rule; runtime is native Go, no docker/pnpm surface).

## FLAG
- [spec-fidelity] c-2 requires the checkpoint option sit "alongside continue/stop"
  at the gate *after each task commit*. execute.md has no such post-commit gate
  today — §1f ends with "Loop back to 1a"; the only per-task pair-mode gate is the
  §1c approach approval (proceed/steer/show/skip). t-5's description ("extend the
  per-task pair-mode gate to continue/stop/checkpoint") and its contract only
  assert a *checkpoint* option exists — nothing pins that a continue/stop
  post-commit gate is introduced, so an executor could graft checkpoint onto the
  §1c approach gate and still pass the test while missing c-2's intent.
  Suggestion: t-5 should state it introduces the post-commit continue/stop/checkpoint
  gate (not "extend" an existing one), and the contract should assert continue +
  stop are present at that gate, not just checkpoint.

- [wave-order] t-5 declares `depends_on = ["t-4"]`, but t-5 is a prompt-only change
  (execute.md + execute_prompt_test.go) with no code or output dependency on t-4's
  Go work (reentry.go / status footer) — its checkpoint emits the literal
  `/clear → /dross-execute --from <next-task>`, not `dross reentry`'s output. The
  real ordering constraint is t-3, which edits the *same* execute.md in wave 1.
  Suggestion: point t-5 at t-3 (same-file sequencing), not t-4. As written the
  graph both claims a spurious dependency and hides the real one.

- [granularity] t-7 (6 files) bundles two things across three surfaces: the
  hooks-ensure helper (hooks.go/_test) and the init+onboard wiring (init.go,
  onboard.go, +2 tests). It's a split candidate — ensure-helper vs command wiring.
  Suggestion: split, or confirm the init/onboard wiring is thin enough (a single
  ensure call each) to justify keeping it one task.

- [wave-order] t-7 `depends_on = ["t-1","t-2","t-4"]`, but only t-1 (the merge
  helper it calls) is a hard code dependency. t-2/t-4 are referenced only as fixed
  command strings ("dross pause --auto", "dross reentry"); t-7's tests exercise the
  settings.json merge, not the verbs, so nothing needs those verbs to compile. The
  author acknowledges this ("hook command strings are plan-fixed constants"), so
  it's a deliberate integration-safety choice — but it over-constrains parallelism.
  Suggestion: keep only t-1 as a true dependency, or note that t-2/t-4 are ordering
  hints for integration coherence rather than build deps.

## NOTE
- [granularity] t-3 (7 files) and t-4 (5 files) trip the 5+-file heuristic but are
  not real split candidates: t-3 is one uniform footer edit repeated across the
  seven boundary prompts; t-4's count is inflated by two test files + a one-line
  root.go registration, with the real work in reentry.go + status.go sharing
  `reentryLine()`. Called out to preempt a mechanical "split these" reading.
- [test-coverage] t-3's `TestBoundaryPromptsCarryFooter` (checks the 7 prompts carry
  the sentinel) partially overlaps t-6's fail-closed `TestFooterCoverageFailClosed`.
  Harmless, but t-6 is the convention-owning gate; t-3 could own content and let t-6
  own enforcement to avoid two tests asserting the same footers.
- [coverage] t-1 is listed as covering c-3/c-5 but is pure infrastructure (a generic
  settings.json merge helper); c-3/c-5 are actually satisfied by t-2/t-4/t-7.
  Coverage still holds because those tasks also list c-3/c-5 — noting only that t-1's
  `covers` is aspirational, not load-bearing.
- [strength] Test contracts are exemplary — every one names the failing test function
  and the exact behaviour that breaks (byte-identical settings.json, foreign-entry
  survival, section-scoped handoff rewrite, suggestNext parity). Well above the
  "tests pass" bar; nothing vague to flag.
- [strength] Clean wave-1 parallelism: four genuinely independent foundation tasks,
  with all real dependencies concentrated in wave 2.
- [strength] Reuses existing conventions instead of inventing parallel machinery —
  interaction-audit mirror for the footer gate (footer_coverage decision), suggestNext
  reuse for the re-entry line, and each locked decision cited by its implementing task.

## Summary
Solid, well-specified plan with no blockers — tighten t-5's dependency (t-3, not t-4)
and its handling of c-2's missing continue/stop gate, and reconsider t-7's split before executing.
