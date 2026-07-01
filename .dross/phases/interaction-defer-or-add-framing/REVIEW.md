# Plan Review — interaction-defer-or-add-framing

Reviewed: 2026-07-01
Plan: 3 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [antipattern/squashed-split] t-3 bundles two genuinely independent plan.md edits
  into one task: (1) c-2's §3 borderline-task defer-first framing, and (2) c-4's §4
  coverage-gap either/or reframe. They target different sections, deliver different
  criteria, and were deliberately kept separate by the `plan_reach` locked decision
  ("handled separately by c-4 as its own explicit either/or"). Bundling them under
  one task means one commit conflates two criteria — the opposite of that decision's
  intent.
  Suggestion: split into two tasks (both wave 2, both depends_on t-1), or if kept as
  one, state in the description that both criteria land in a single commit and why.

- [design-coherence] t-2 layers a "defer it / add to current phase" entry gate on
  top of an *intact* §4a (per `defer_routing_layering`), but §4a's first routing
  option is already "Pull into the current phase". After the user picks "defer it"
  at the entry gate, the intact §4a re-offers "pull into the current phase" — a
  contradictory double-offer (they just declined to add it). The task description
  says §4a "runs as a two-step follow-up" without addressing this overlap.
  Suggestion: have t-2 drop or gate §4a's "Pull into the current phase" option once
  the entry gate owns the add-vs-defer split, so the follow-up only routes an
  already-deferred item (park / attach / someday). Note this tension exists inside
  the locked decision itself, so surface it rather than silently overriding.

- [wave-order] t-2 and t-3 sit in wave 2 depending on t-1, but the dependency is
  documentation-coherence ("per the playbook"), not build output. Each edits a
  different file (spec.md / plan.md) with an independent test file; nothing t-2 or
  t-3 asserts consumes t-1's produced artifact. They could run in wave 1 for
  parallelism.
  Suggestion: keep the ordering only if you want the canonical _interaction.md
  statement to exist before the prompts textually reference it; otherwise flatten to
  one wave.

## NOTE
- [strength] Clean 1:1 criterion coverage: c-1→t-2, c-2→t-3, c-3→t-1, c-4→t-3. No
  criterion is orphaned and no task lacks a `covers`.
- [strength] Locked decisions are named and honored explicitly in task descriptions
  (defer_trigger in t-2, defer_routing_layering in t-2, plan_reach in t-3) rather
  than left implicit — easy to audit against spec.toml.
- [strength] Test contracts follow the repo's established prompt-content harness
  (spec_prompt_test.go reads assets/ source directly per r-01) and each names the
  specific wording whose removal breaks the assertion — not "tests pass".
- [test-contract] All referenced files resolve: _interaction.md, spec.md, plan.md,
  interaction_snippet_test.go, and spec_prompt_test.go exist; plan_prompt_test.go is
  created by t-3; repoRootFromTest helper exists. No dangling references.
- [test-contract] t-1's file list includes the pre-existing
  interaction_snippet_test.go — it modifies (adds an assertion) rather than creates,
  which is fine, but note the existing markers there belong to a prior phase's c-2,
  so the new defer-first assertion is additive.

## Summary
Coverage and locked-decision fidelity are solid; the substantive issues are the
§4a double-offer overlap t-2 doesn't reconcile and t-3 conflating two criteria the
spec deliberately kept separate — both worth resolving before executing.
