# Plan Review — verify-auto-finalize

Reviewed: 2026-07-25 (blocking re-check after plan amendment: 2026-07-25)
Plan: 4 tasks across 2 waves

## BLOCKING
- [coverage] **RESOLVED** (plan amended 2026-07-25). All three remediation
  points landed:
  1. t-1 now owns phase-tagging: its description explicitly stamps
     `Event.Phase` on the verify pending + outcome events ("RecordOutcomeEvent
     currently drops phase identity"), and a new test-contract clause pins it
     ("if phase stamping breaks, the test asserting verify pending/outcome
     events carry the phase id fails"). `internal/cmd/telemetry.go` is not in
     t-1's files, but a phase-aware emission variant can live in
     `internal/cmd/verify.go` (same package, listed) — the work is explicit
     and contract-backed either way.
  2. t-4 moved to wave 2 with `depends_on = ["t-1"]`, eliminating the
     wave-order collision with the event-schema change.
  3. Legacy events are addressed: t-4's description states "Legacy events
     without a phase id are excluded from the pending nag", so unpaired
     historical pendings can no longer produce a permanent nag.
  Original finding (for the record): t-4's per-phase pending→resolved matching
  required a phase id on verify telemetry events, but no emitter populated
  `Event.Phase` and no task owned adding it; t-4 also sat in wave 1 parallel
  to t-1 despite depending on t-1's emission-path change, and phase-less
  pre-existing events could never be paired.

## FLAG
- [antipattern] t-2's description says "Note the self-heal in the verify.md
  prompt wrap section" but its `files` list is only internal/cmd/ship.go and
  ship_test.go. The prompt lives at assets/prompts/verify.md and is not listed
  anywhere in the plan — work referenced with no file backing it tends to get
  dropped at execution time.
  Suggestion: add assets/prompts/verify.md to t-2's files. Note rule r-01
  applies: the prompt edit is not live until `make install` re-links it.
- [test-contract] c-2's idempotency guarantee silently excludes history: the
  already-finalized short-circuit keys on the new `finalized` marker, but every
  phase finalized before this change has no marker in its verify.toml.
  Re-running `dross verify finalize` on such a phase emits a duplicate outcome
  event — exactly what c-2 says must not happen — and no test contract in t-1
  covers the pre-marker case.
  Suggestion: decide whether pre-marker phases are grandfathered (record it in
  the spec/plan) or backfilled, and add a contract clause either way.
- [coverage] t-2 heals "before continuing", which reads as heal-only-on-the-
  continue-path. But ship's gate (internal/cmd/ship.go:113-134) refuses on
  partial/fail — both *resolved* verdicts. If the heal fires only when ship
  proceeds, a resolved-but-unfinalized partial/fail phase hit by ship keeps its
  pending telemetry, against the spec's heal-before-gate direction (the
  finalize_mode decision heals resolved verdicts, not just passes). Neither
  t-2 test contract pins down which behavior is intended.
  Suggestion: state explicitly whether finalizeVerify runs before the
  partial/fail refusal, and add a contract clause for that path.

## NOTE
- [strengths] Test contracts are exemplary throughout — every clause names the
  specific observable that breaks ("second run must emit no outcome event, exit
  0, print 'already recorded'"; "no 'have not been finalized' line"). Nothing
  resembling "tests pass" anywhere.
- [strengths] Both locked decisions are faithfully mapped: `dross verify
  finalize` stays the primary path (t-1 rewires rather than replaces it), and
  t-4 retains the nag for genuinely unresolved verdicts instead of suppressing
  it wholesale. The file-based `finalized` marker is a good design call — gate
  idempotency becomes a cheap verify.toml read instead of a telemetry scan.
- [coverage] The status half of c-4 already holds today: `pendingVerdicts`
  (internal/cmd/status.go:519-536) keys on verdict empty/"pending", so a
  resolved-but-unfinalized phase never appears in status's nag as-is. t-4's
  status_test.go addition is a regression guard over existing behavior —
  correctly scoped as test-only, no status.go change needed.

## Summary
Solid structure with unusually sharp test contracts, but blocked on one real
gap: the stats redesign in t-4 assumes phase-tagged verify events that nothing
in the codebase emits and no task creates.
