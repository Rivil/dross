# Plan Review — 01-architecture-comprehension-layer

Reviewed: 2026-06-19
Plan: 4 tasks across 2 waves

## BLOCKING
(none)

All seven criteria are covered: c-1/c-2/c-3 by t-1, c-5 by t-2, c-4/c-7 by t-3, c-6 by t-4.
No task contradicts a locked decision — t-3 routes both /dross-architecture and onboard
through one engine (backfill_trigger), t-2 reuses --notes instead of a typed field
(matches the deferred-field decision), and t-1's template matches entry_template +
provenance_format + feature_granularity. No rule violation: runtime is native, no
docker/pnpm concerns; r-01 (make install before relying on prompt edits) is an execution
concern, not a plan-structure one.

## FLAG
- [granularity] t-3 touches 4 files and spans 3 surfaces (a new prompt engine, a new
  slash command, an edit to onboard.md, and a new Go parity test). The Go test
  (commands_parity_test.go) is a different kind of work from authoring the LLM backfill
  prompt, and it gates a structural invariant (commands<->prompts 1:1) that is independent
  of whether the backfill engine actually works.
  Suggestion: consider splitting the parity-test addition out, or at minimum sequence it
  first within the task so a green parity test doesn't get conflated with "engine works."

- [test-contract] t-3's first contract ("if .../dross-architecture.md or .../architecture.md
  is missing, the parity test fails") describes a test that does not yet exist — there is no
  commands_parity_test.go in the repo today, and no existing test asserts the
  commands<->prompts mapping. The contract is really "this task must author that parity test
  AND make it pass," which is heavier than the wording implies.
  Suggestion: make explicit that t-3 creates the parity invariant test, not just satisfies
  one; otherwise "parity test fails" reads as if the harness already exists.

- [test-contract] t-3's second and t-4's first/second contracts assert end-to-end LLM
  behaviour ("running /dross-architecture ... produces one with at least one feature entry
  carrying desc + symbol links + provenance"; "after ship the landmarks are absent ...").
  These name the right surface (the provenance breadcrumb, the by-feature in-place merge vs
  a duplicate per-phase heading) so they are specific, but they are prompt-behaviour
  assertions with no Go test harness behind them — there is no automated way to fail them in
  `go test`. They are verifiable only by manual/agent execution.
  Suggestion: acknowledge these as manual/verify-time contracts, or note which are
  machine-checkable (file-existence, parity) vs prose-quality (must be eyeballed at verify).

## NOTE
- [strengths] Wave order is tight and correct: t-1 (template) is the genuine prerequisite
  for both t-3 (backfill must emit that template) and t-4 (merge must match it); t-2 is
  correctly independent and sits in wave 1; t-4's dep on t-2 is real (it merges the
  landmarks t-2 produces). Nothing could be pulled forward for more parallelism, and nothing
  is artificially serialized.

- [strengths] The locked decisions are honored precisely — one engine with multiple entry
  points, --notes reuse over a new typed field, and the fixed micro-template are each
  reflected in the matching task rather than drifted. The plan reads as authored against the
  spec, not around it.

- [strengths] The by-feature-in-place merge contract (t-4) explicitly names the failure mode
  it guards against — "a duplicate per-phase heading appearing instead of an updated
  existing entry" — which is exactly the c-1 "never per phase" risk. Good adversarial framing.

- [coverage] c-2 ("symbol-level location links") is satisfied at the template/slot level by
  t-1; the actual link population happens in t-3 (backfill) and t-4 (merge). This is correct
  decomposition, not a gap — recording it so it isn't mistaken for missing coverage later.

## Summary
Solid, spec-faithful plan with correct coverage and wave order; the only soft spots are
that t-3 bundles a structural Go parity test with prose-engine work and several contracts
are prompt-behaviour assertions with no automated harness, so verify will lean on manual
checking.
