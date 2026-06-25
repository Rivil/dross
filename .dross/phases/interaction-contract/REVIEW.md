# Plan Review — 10-interaction-contract

Reviewed: 2026-06-21
Plan: 8 tasks across 4 waves (t-1..t-5 done; t-6 reworked, t-7 + t-8 new, all pending)

## BLOCKING
(none)

## FLAG
- [test-contract] t-6's second contract still reads "... record the pilot as
  resolved-via-emitter (nested @-include FAILED) **with a date** ...". The date
  clause remains effectively unfalsifiable — any string (or a wrong/placeholder
  date) passes a grep for "a date". The prior review flagged this and the
  amendment did not change the wording.
  Suggestion: drop the date from the machine assertion (assert the two phrases
  only), or pin a format the test actually validates (e.g. a YYYY-MM-DD regex).

## NOTE
- [prior-finding-1/registration] RESOLVED. t-7 now lists `cmd/dross/main.go` in
  files (line 60), its description says "Register cmd.Interaction() in
  cmd/dross/main.go's root.AddCommand list", and its first test_contract asserts
  the command is "absent from root.AddCommand in cmd/dross/main.go". Grounded:
  cmd/dross/main.go lines 23–48 are the real registration site (root.AddCommand);
  internal/cmd/root.go has no AddCommand. The wrong path is gone.
- [prior-finding-2/stale-@-include] RESOLVED. New t-8 (covers c-1, wave 3)
  targets the two stale spots confirmed in source: rules.go:139 still says
  "snippet that interactive prompts @-include", and _interaction.md:6 still says
  "Interactive prompts @-include it". t-8's test_contract asserts the rendered
  rule no longer contains "@-include" and points at `dross interaction show`, and
  that the snippet header no longer instructs @-include — directly falsifiable
  against both stale strings.
- [wave-order] t-7 and t-8 both sit in wave 3 and run in parallel; files are
  disjoint (t-7: assets/embed.go, internal/cmd/interaction.go, cmd/dross/main.go,
  interaction_test.go; t-8: rules.go, rules_test.go, _interaction.md), so no
  write collision. t-6 (wave 4) correctly depends on t-7. Both wave-3 tasks
  depend only on done tasks (t-1, t-2). Order is sound. (As the prior review
  noted, the wave numbers overstate the remaining 2-step tail now that t-1..t-5
  are done — cosmetic only.)
- [coverage] All four criteria still covered after the amendment: c-1 (t-1, t-3,
  t-8), c-2 (t-2, t-3, t-5), c-3 (t-7, t-6), c-4 (t-4). No orphan criteria or
  tasks. t-8 correctly adds a second c-1 owner rather than overloading t-6.
- [test-contract/specificity] t-6's pilot test targets the `@`-prefixed include
  line (spec.md line 7), not the legitimate prose mention of `_interaction.md` on
  spec.md line 5. The contract wording ("the dead
  @~/.claude/dross/prompts/_interaction.md line") is specific enough not to catch
  the prose pointer, which must survive. The distinction is load-bearing for
  execution — do not over-strip line 5.
- [locked-decision] t-6/t-7/t-8 all align with the locked snippet_delivery
  decision (emitter via `dross interaction show`, @-expansion disproven). No
  conflict; t-8 in particular closes the last contradiction with that decision.
- [granularity/strength] t-8 is a tight single-concern, three-file unit that does
  exactly one thing (repoint two stale strings) with falsifiable assertions on
  each. t-7's single-source byte-compare contract (emitter output vs
  _interaction.md on disk) remains the strongest in the plan.

## Summary
Both prior findings are genuinely resolved and grounded in source, and the
amendment introduces no new issues; the one residual flag (t-6's unfalsifiable
"with a date" clause) is a carry-over the rework left untouched and is non-blocking.
