# Plan Review — 04-status-action-surfaces

Reviewed: 2026-06-20
Plan: 2 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [locked-decision / antipattern] t-2's spine-idle predicate is described as keying off a
  status enum — "the phase is verified/pass/complete/shipped" — but `state.CurrentPhaseStatus`
  is free-text and is NOT reliably set to those values. In the codebase it is set to "created"
  (internal/cmd/phase.go:123), cleared to "" on ship (internal/cmd/ship.go:147), and otherwise
  only changed by a manual `dross state set` (internal/cmd/state.go:60). The real idle signal
  already lives in `suggestNext` (internal/cmd/status.go:127-175): idle == no NextRunnable task,
  no failed tasks, and verify verdict is "pass" or absent → it returns "phase looks complete /
  verified — start a new phase or move on". If t-2 implements its predicate by string-matching
  CurrentPhaseStatus, a verified-but-status-unchanged phase ("created" or "executing") will
  never show the actions block, silently failing c-1.
  Suggestion: derive the idle predicate from the same file/verdict logic suggestNext uses
  (NextRunnable == nil && failed == 0 && verdict in {"", "pass"}), or factor that decision out
  of suggestNext and reuse it — do not trust CurrentPhaseStatus as a four-value enum.

- [granularity / wave-order] t-1 and t-2 both touch exactly the same two files
  (internal/cmd/status.go, internal/cmd/status_test.go), and t-2 is a thin wrapper (one
  predicate + one call site that prints the t-1 renderer's output). This is close to an
  artificial split — t-2 adds little beyond wiring t-1's pure renderer into the idle path. The
  wave-2/depends-on ordering is technically defensible (t-2 calls the t-1 renderer), but the
  total work is one cohesive change to a single command.
  Suggestion: consider merging into one task, or keep the split but be aware it buys no
  parallelism (same files, serial). Either is acceptable; flagging the inflation risk.

- [test-contract] t-2's third contract — "if a verified/pass phase stops showing the actions
  block, the verified-phase test fails" — names "verified/pass phase" but the value that makes
  a phase count as verified is the open question above (verdict file vs status string). The
  contract should pin which signal the test asserts on, so it actually guards the idle predicate
  rather than the status string.
  Suggestion: state the contract in terms of the observable state the test will construct, e.g.
  "a phase whose verify.toml verdict is pass and whose plan has no runnable task shows the
  actions block".

## NOTE
- [coverage] All three criteria are covered: c-2 → t-1; c-1 and c-3 → t-2. Coverage is complete.
- [strengths] t-1's test contracts are genuinely specific and behaviour-named — they pin the
  exact regressions (unavailable area emitting a runnable command; available area dropping its
  `/dross-…` line), which is exactly what the dead_command_handling lock (c-2) requires. Good
  contract hygiene.
- [strengths] The plan honours the locked decisions: fixed in-code catalog (no config schema),
  "(planned)" marker instead of a runnable pointer to an unbuilt command, and the actions block
  placed near `next:` while leaving the start-new-phase pointer intact. No locked-decision
  conflicts found.
- [strengths] Files referenced all exist (internal/cmd/status.go, status_test.go) and match the
  existing block-renderer style (pending:/handoff:) the placement_format decision cites. No
  phantom files.
- [forbidden-actions] No rule violations. runtime.mode is "native" (test `go test -count=1
  ./...`); the only project rule (r-01: `make install` before relying on prompt/Go edits) is an
  execution-time concern, not a plan-structure concern. Nothing in the plan runs a forbidden
  tool.

## Summary
Structurally sound and faithful to the locked decisions, but t-2's idle predicate is described
against an unreliable status enum when the real idle signal lives in suggestNext's file/verdict
logic — resolve that before executing or c-1 risks silently failing.
