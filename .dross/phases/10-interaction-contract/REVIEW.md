# Plan Review — 10-interaction-contract

Reviewed: 2026-06-21
Plan: 6 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [granularity] t-5 ("Wire snippet into install; verify symlink") may be a near no-op
  disguised as work. `make install` links the whole prompts directory with a single
  `ln -sfn $(CURDIR)/assets/prompts $(PROMPTS_DIR)` (Makefile line 44) — it does not
  enumerate individual files. A newly-created `assets/prompts/_interaction.md` is
  therefore already resolvable through the existing dir symlink with zero Makefile
  change. `make doctor` likewise loops `assets/prompts/*.md` (lines 123-134), so it
  already counts the new file automatically. The task's description ("Ensure make
  install links the new snippet... alongside the other prompts") implies a Makefile
  edit that is almost certainly unnecessary.
  Suggestion: keep the verification test (valuable regression guard) but expect t-5 to
  touch the Makefile only if doctor needs a new explicit assertion; otherwise it's a
  test-only task and should say so. Confirm before allocating a Makefile edit.

- [wave-order] t-5 depends only on t-2 and is in wave 2, but t-2 is in wave 1 and t-5
  touches only the Makefile / install path — it shares no files with t-3 (the other
  wave-2 task) and doesn't consume t-3's output. Per the install mechanism above, t-5's
  real dependency (a file existing under assets/prompts/) is satisfied the moment t-2
  lands. It could run in wave 1 alongside t-2... except it must run *after* t-2, not
  concurrently. So wave 2 is defensible, but t-5 and t-3 are independent of each other —
  worth confirming the wave split is for ordering vs. t-2, not an implied t-3 coupling.
  Suggestion: leave as-is; just note t-5 ⊥ t-3 so they can run in parallel within wave 2.

- [granularity] t-6 spans 3 concerns in one task: (a) mechanical — add the @-include
  line to spec.md; (b) empirical — manually load /dross-spec and observe whether
  two-level @-expansion delivers the text; (c) contingent — apply the CLI-emitter
  fallback if it doesn't. (b) is a human-in-the-loop observation that cannot be
  unit-tested, and (c) is an entire alternate implementation path gated on (b)'s outcome.
  This is the riskiest task in the phase (it's the c-3 de-risking pilot) yet it's sized
  like the others.
  Suggestion: don't split mechanically, but flag t-6 as the task most likely to expand —
  if nested @-expansion fails, the fallback is real net-new work (a CLI emitter wired
  into the prompt's pre-flight) that arguably deserves its own task. Be ready to spawn it.

## NOTE
- [coverage] All four criteria are covered: c-1 → t-1, t-3; c-2 → t-2, t-3, t-5;
  c-3 → t-6; c-4 → t-4. No gaps.

- [locked-decisions] No conflicts found. The plan faithfully honors all four locked
  decisions: builtin rule in rules.go not .dross/rules.toml (rule_tier → t-1); @-include
  delivery with CLI fallback and explicit nested-expansion pilot (snippet_delivery → t-6);
  rule-terse / snippet-heavy split enforced by t-3's ~600-char length-bound assertion
  (rule_snippet_split); checklist at docs/interaction-audit.md (checklist_home → t-4).

- [forbidden-actions] No rule violations. Project rule r-01 (prompt/Go edits aren't live
  until `make install`) is actively respected — t-5 and t-6 both run against the installed
  symlink, and t-6's pilot explicitly loads /dross-spec "for real". No global rules file
  exists at ~/.claude/dross/rules.toml.

- [test-contract] Test contracts are a genuine strength — they are specific and
  failure-named, not vague. Examples: t-1 names the exact rendered token
  `[builtin/hard/dross-interaction-contract]` plus required phrases; t-3 names
  `TestInteractionRuleNamesSnippet` and a concrete ~600-char length bound; t-5 names a
  temp-PROMPTS_DIR readlink check. None use "tests pass" or "covered by integration".
  This is well above the usual LLM-plan baseline.

- [realism] t-3's premise is grounded: spec.md genuinely carries a hand-rolled
  interaction paragraph today (lines 5, 143, 172-173 — "one point per turn", "no walls
  of text", "Don't paste the TOML back"). The dedup-into-snippet work is real, and the
  drift/duplicate-grep assertion has a true target. The two-level @-expansion uncertainty
  t-6 pilots is also real: command wrappers already `@`-include their prompt (verified
  across all 20 wrappers), so the snippet would be a second nesting level.

- [granularity] t-6 also writes to docs/interaction-audit.md (recording the pilot
  result), which is created by t-4 (wave 1). The dependency is satisfied by wave order
  but is not declared in t-6's depends_on (only t-3, t-5 are). Harmless given t-4 is
  wave 1 and t-6 is wave 3, but the implicit edge is worth noting.

## Summary
A genuinely strong plan — coverage complete, locked decisions honored, and test
contracts unusually specific; the only real soft spot is that t-5 likely overstates its
install work (the prompts dir is symlinked wholesale) and t-6 carries hidden
fallback-path risk worth watching.
