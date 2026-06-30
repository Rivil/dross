# Plan Review — deferred-unroute-command

Reviewed: 2026-06-30
Plan: 1 task across 1 wave

## BLOCKING
(none)

## FLAG
- [test-contract] Decision `already_someday` is locked on a specific behaviour:
  idempotent success *with an "already someday" message, no state change*. The
  matching test_contract entry only asserts "returns nil and leaves it someday"
  — it verifies the no-state-change half but not the message half. A regression
  that drops or garbles the "already someday" output (e.g. printing the normal
  "unrouted ..." line, or nothing) would pass the contract as written, even
  though it violates the locked decision.
  Suggestion: have the idempotent-case test also assert the distinct "already
  someday" message string, so the locked behaviour is fully pinned.

## NOTE
- [strength] The plan gets the one subtle correctness point right: it orders the
  Dismissed-refusal *before* the idempotent already-someday clear. This matters
  because a routed item can't be dismissed (deferred.go:229), so every dismissed
  item has `Target == ""` — a naive "Target empty → success" check would silently
  succeed on a dismissed item instead of refusing it. The description's ordering
  ("refuse when Dismissed ... otherwise clear Target") avoids that trap.
- [strength] Every test_contract entry maps to a specific criterion or locked
  decision and names a concrete failure surface (route→unroute round-trip,
  out-of-range index naming the valid range, missing-spec via LoadSpec, dismissed
  refusal pointing to `dismiss --undo`). No vague "tests pass" contracts.
- [granularity] Single task / single wave is the right grain here: 2 files
  (deferred.go + deferred_test.go), one CLI layer, mirroring the existing
  route/dismiss structure. No artificial split, no squashed multi-concern task.
- [coverage] The non-integer `idx` path (`strconv.Atoi` → "idx must be an
  integer", present in both route and dismiss) is not exercised by any contract
  entry. This is *consistent* with the existing route/dismiss tests, which also
  omit it, and c-2 only enumerates out-of-range + missing-spec — so it is not a
  gap against the spec. Recorded only so the omission is a known, deliberate one.
- [scope] The new `unroute` verb gets no prompt-side surface: assets/prompts/
  inbox.md and spec.md document `route`/`dismiss` for the triage funnel but no
  inverse "un-route a mis-routed item" affordance. The spec scopes only CLI
  behaviour (c-1, c-2), so this is correctly out of plan scope — flagged only to
  confirm the CLI-only reach is intended, not an oversight.

## Summary
A clean, well-scoped single-task plan that honours all three locked decisions
and covers both criteria; the only actionable nit is pinning the locked
"already someday" message in the idempotent test.
