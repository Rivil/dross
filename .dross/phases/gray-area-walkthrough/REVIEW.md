# Plan Review — gray-area-walkthrough

Reviewed: 2026-06-30
Plan: 1 task across 1 wave

## BLOCKING
(none)

## FLAG
- [test-contract] Contract 3 (TestSpecPromptUncertaintyDiscriminator) bundles a
  concrete assertion with a fuzzy one. The "decide internal architecture yourself"
  boundary half maps to a literal phrase that a grep can pin. The
  "uncertainty-discriminator framing" half does not — there is no concrete token
  named, so the test author has latitude to assert almost anything and a weak
  sentinel could pass against unintended prose. As written this half is not
  falsifiable in a meaningful way.
  Suggestion: Name the exact sentinel string the rewrite must contain to encode
  the uncertainty discriminator (e.g. "genuinely uncertain" or "cannot confidently
  resolve") so the test pins a real token, not a concept. Same latitude exists in
  contract 2 ("walk every uncertain area, one decision per turn") — pin "one
  decision per turn" or an equivalent fixed phrase rather than a paraphrase.

- [coverage-of-decisions] The count_ordering locked decision (soft ~3-4 areas,
  impact-/uncertainty-ordered) is encoded into the prompt per the task description
  but is pinned by no test contract. A future edit that drops impact-first ordering
  or the soft-count guidance would fail nothing, even though this phase exists
  precisely to pin §3 behavior with sentinels. (Not a criteria-coverage gap —
  count_ordering is a decision, not a criterion — hence FLAG not BLOCKING.)
  Suggestion: Either add a sentinel pinning the ordering/soft-count language, or
  consciously accept that this locked decision rides unguarded.

## NOTE
- [coverage] c-1, c-2, c-3 all appear in t-1's `covers`. Coverage is complete.
- [locked-decisions] No contradiction with walk_termination, count_ordering, or
  uncertainty_threshold. The task description explicitly restates all three
  (walk-all + off-ramp, soft ~3-4 impact-ordered, intersection threshold).
- [antipattern/files] Both referenced files exist. The existing TestSpecPrompt*
  functions cover §1 (resurface seed) and §4 (routing) only — none assert §3
  content — so removing §3b's multiSelect does not break any existing test. The
  new test function names don't collide with the existing ones.
- [forbidden/r-01] Tests read assets/prompts/spec.md source directly via
  specPromptContent (helper even cites r-01); nothing in the plan relies on
  installed/`make install` behavior, so there's no stale-behavior risk. The c-1
  contract targets the exact current phrase ("which of these should we pin down"),
  which is concrete and grep-falsifiable.
- [granularity] One task touching the prompt plus its sentinel test is the correct
  grain — splitting would leave a meaningless intermediate state (prompt rewritten,
  nothing pinning it, or tests asserting against un-rewritten text). Single wave is
  trivially correct.
- [strengths] Contracts are written in falsification form ("if X, test Y fails"),
  which is good discipline; the prompt+test pairing is properly atomic; all three
  locked decisions are carried into the task description rather than left implicit.

## Summary
Sound, well-scoped 1-task plan with complete criteria coverage and no
locked-decision conflicts; the only real soft spots are an under-specified
sentinel in contract 3 (and 2) and the count_ordering decision riding unguarded by
any test — both worth tightening, neither blocking.
