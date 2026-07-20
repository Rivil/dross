# Plan Review — dross-watch

Reviewed: 2026-07-20
Plan: 5 tasks across 3 waves

## BLOCKING
(none)

Criteria coverage is complete: c-1 (t-2,t-3), c-2 (t-1,t-3), c-3 (t-3,t-5),
c-4 (t-1,t-4,t-3), c-5 (t-2,t-3,t-5). Every locked decision is honoured —
notably read_only_boundary is enforced structurally by t-4's mark-free
collectInbound extraction, not by convention. All referenced files either
exist (issue.go, main.go, docs/interaction-audit.md) or are created by an
earlier task (internal/watch/*, watch.go, the watch assets). Every "existing"
test t-5 leans on was confirmed present (TestInteractionCoverageFailClosed,
TestCommandsPromptsParity, the `## Exempt` section, the inbox board-off
pattern it mirrors).

## FLAG
- [seam] The issue-state scope collectInbound requests is unpinned. t-4 exposes
  `collectInbound(ctx, filter)` with `filter` passed by the caller, and t-3 says
  only "read-only inbound pull (via collectInbound)" — it never states which
  state it queries. The locked delta_identity decision ("a reopened issue
  re-surfaces as new") only holds one way: with an open-only feed, a reopen is
  detected via feed-absence (closed → drops out of the open list → reappears as
  absent-from-prior → New). If watch instead passes state=all, the id+open/closed
  key becomes the mechanism and closed items linger in `current`. t-1's
  "flipped open->closed -> New" diff case only occurs in production under the
  latter. These two designs give different digests; the plan doesn't say which.
  Suggestion: pin t-3 to pass state="open" (the inbound-triage meaning) and mark
  t-1's open→closed diff case as defensive, or state the chosen scope explicitly
  in t-4/t-3.
- [granularity] t-2's drift.go reimplements status.go's phase-state
  classification in-package (justified: dodges an internal/cmd import cycle), but
  no test ties watch's buckets to what `dross status` actually emits for the same
  .dross tree. The locked drift_signals decision says "reuse the phase states
  dross status already computes"; a second independent implementation can
  silently drift from status.go (e.g. status.go changes how it reads a pending
  verdict, watch doesn't). Not blocking — intent (same states, no new
  thresholds) is preserved and the buckets are fixture-pinned — but the "reuse"
  is really "re-derive". Suggestion: add one cross-check test asserting drift.go
  and status.go agree on a shared fixture, or extract the signal logic into a
  lower package both import.

## NOTE
- [test-contract] c-4 requires git to be byte-identical before/after, but
  TestWatchReadOnlyBoundary asserts only "the sole .dross write is
  watch.state.json" plus an HTTP GET-only handler — git untouched is left
  implicit. Acceptable, since watch has no git code path to trip it, but the
  git half of the criterion is verified by absence-of-code rather than by an
  assertion.
- [strength] Test contracts name the exact surface that breaks
  (byte-identical board.json incl. last_pull, an httptest handler that fails on
  any non-GET method, "state written ONLY when the board was reached") rather
  than "tests pass" — these are genuinely falsifiable.
- [strength] The read-only guarantee is designed in at the seam: t-4 removes the
  ability to re-introduce --mark by giving watch a filter path that never
  stamps last_pull, instead of trusting the command to avoid it.
- [strength] Wave/dependency graph is minimal and correct — t-3 is a real
  integration point that strictly needs t-1/t-2/t-4, and t-5 strictly needs
  t-3's suggested_command shape. No task could drop a wave.

## Summary
A tight, well-covered plan with strong test contracts and a structurally
enforced read-only boundary; the only substantive gaps are an unpinned
issue-state scope that the reopen semantics silently depend on, and a
re-derived drift classifier with no cross-check against status.go.
