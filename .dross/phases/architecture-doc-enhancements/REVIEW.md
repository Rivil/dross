# Plan Review — architecture-doc-enhancements

Reviewed: 2026-06-30
Plan: 6 tasks across 2 waves

## BLOCKING
(none)

All four criteria are covered (c-1: t-1+t-5; c-2: t-6; c-3: t-2+t-3; c-4: t-2+t-4).
No task contradicts a locked decision — landmark_field_shape, refresh_merge_strategy,
link_check, and legacy_notes each map cleanly onto a task. No forbidden action: runtime
is native Go, no docker/pnpm, and r-01 (make install) is an execution-time concern, not a
plan-structure defect.

## FLAG
- [antipatterns/scope] t-2 hard-restricts the symbol resolver to "Go-only; non-Go links
  classify Unresolved/Skipped", but internal/codex already dispatches across multiple
  languages (allIndexers/dispatch: ts, tsx, svelte, csharp, gdscript via ast-grep, plus the
  Go stdlib indexer). On a non-Go repo this resolver would classify every legitimate symbol
  link as Unresolved, so t-3's doctor section would flood ⚠ warnings for healthy links. dross
  ships generically and codex is deliberately multi-language; a Go-only resolver contradicts
  that.
  Suggestion: route through codex's existing language dispatch instead of hard-coding Go, or
  state in the task why only the Go stdlib indexer gives the line precision the resolver needs
  (and confirm t-3 suppresses warnings for languages the resolver can't see, rather than
  emitting Unresolved for them).

## NOTE
- [test-contract] Test contracts are unusually specific and break-surface-named throughout:
  t-1 pins SplitN-on-first-`=` with `·`/`=` surviving the round-trip; t-3 asserts
  finalizeDoctor's issue count (not stdout) so advisory links provably never block; t-4 asserts
  byte-identical rewrite of everything except the `:line` suffix. This is exactly the
  specificity the check wants — no vague "tests pass" contracts anywhere.
- [wave-order] Wave 2 (t-3, t-4) genuinely depends on t-2's ParseDoc + resolver; the split is
  real, not granularity inflation. The shared resolver in t-2 correctly prevents two divergent
  parsers in doctor vs the check subcommand.
- [wave-order] t-5 (prompt emit/read) is kept in wave 1 alongside the t-1 binary and explicitly
  justified: field names are pinned by the landmark_field_shape locked decision, and `dross
  changes show` already JSON-marshals the whole TaskRecord, so adding `Landmarks` auto-surfaces
  it in the JSON ship reads. The decoupling holds — no hidden t-1→t-5 dependency.
- [coverage] t-6 and the c-2 merge logic are prompt-only (architecture.md + a grep test), with
  no Go merge code. This is consistent with the codebase: architecture.go only provides
  Skeleton(); generation and merge are LLM-driven by the prompt. Correct, not a gap.
- [scope] t-5 bundles two prompt surfaces (execute.md + ship.md) in one task. Acceptable —
  they share a single format contract and changing them together keeps that contract
  consistent; not a split candidate.

## Summary
A tight, well-sequenced plan with standout test-contract specificity and clean
decision-to-task mapping; the only substantive concern is t-2's Go-only resolver narrowing,
which is a FLAG, not a blocker.
