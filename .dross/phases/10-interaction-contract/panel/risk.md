# Phase 10-interaction-contract — RISK lens

Failure modes drive the graph. Each named risk in the spec is owned and tested by exactly
one task: the builtin-rule render, the install symlink picking up a *new* file, the *nested*
@-include expansion, drift between rule/snippet/pilot copies, and partial/omitted checklist rows.

Phase 10-interaction-contract — 6 tasks across 3 waves

Wave 1
  t-1  Add dross-interaction builtin rule
       files:    internal/rules/rules.go, internal/rules/rules_test.go
       covers:   c-1
       contract: rules_test asserts `dross rule show` Render output contains
                 `[builtin/hard/dross-interaction-contract]` AND the do/don't phrases
                 "one decision per turn" and "never paste the build artifact back";
                 if the rule is dropped from Builtins or its severity is not Hard, the
                 new Render test fails. Also assert the rule names the snippet path
                 (`_interaction.md`) so the rule→snippet pointer can't silently vanish.

  t-2  Author reusable interaction snippet
       files:    assets/prompts/_interaction.md
       covers:   c-2 (content half)
       contract: a file-existence + grep check (in t-4's install test, and a doc note here)
                 confirms assets/prompts/_interaction.md exists and contains all four
                 playbook beats: propose-default-and-react, one-decision-per-turn,
                 no-walls-of-text, never-paste-the-build-artifact-back. If any beat is
                 missing the grep assertion in t-4 fails. Snippet is the *full playbook*
                 (AskUserQuestion patterns, accept/reword/drop examples) per rule_snippet_split.

  t-5  Seed interaction-audit checklist
       files:    docs/interaction-audit.md
       covers:   c-4
       contract: a checklist-completeness check (committed as a shell snippet in the doc's
                 own header, or a note enumerating the source-of-truth) asserts every
                 interactive command under assets/commands/ whose prompt contains
                 `AskUserQuestion` has a section in interaction-audit.md, and each command's
                 section has ≥1 per-decision-point row (not one row per command). If a new
                 interactive command is added without a checklist section, the enumeration
                 in the doc visibly omits it (diff against `grep -l AskUserQuestion`).

Wave 2 (depends t-1, t-2)
  t-3  Reconcile rule / snippet / pilot text for drift
       files:    internal/rules/rules.go, assets/prompts/_interaction.md, assets/prompts/spec.md
       covers:   c-1, c-2 (consistency)
       depends:  t-1, t-2
       contract: a drift check — rules_test (or a committed grep) asserts the canonical
                 do/don't phrases ("one decision per turn", "never paste the build artifact
                 back") appear verbatim-or-equivalent in BOTH the builtin rule text (rules.go)
                 AND the snippet, AND that spec.md no longer carries its OWN divergent copy of
                 the wall-of-text paragraph (lines ~5/143/172) — it must point at the snippet
                 instead. If spec.md keeps a hand-rolled paragraph alongside the @-include,
                 the grep for the duplicated phrase outside _interaction.md fails.

  t-4  Verify install links snippet; symlink-resolution test
       files:    Makefile, internal/install (or a Makefile `doctor`/test target)
       covers:   c-2 (delivery half)
       depends:  t-2
       contract: after `make install`, a check resolves
                 `~/.claude/dross/prompts/_interaction.md` (via the whole-dir symlink at
                 Makefile:44) and greps it for a snippet beat — proving a NEW file added to
                 assets/prompts/ is picked up without a per-file link edit. If the install
                 stopped symlinking the directory (or someone switched to per-file links and
                 forgot the new file), the readlink/grep check fails. `make doctor`'s prompt
                 loop already counts `_interaction.md`; assert its `p_ok` count rose by one.

Wave 3 (depends t-3, t-4)
  t-6  Prove nested @-include reaches the model on pilot
       files:    assets/prompts/spec.md, .dross/phases/10-interaction-contract/panel/risk.md (notes only)
       covers:   c-3
       depends:  t-3, t-4
       contract: spec.md gains `@~/.claude/dross/prompts/_interaction.md`; the pilot proof
                 is that the SECOND-LEVEL include resolves — the command wrapper
                 assets/commands/dross-spec.md already does `@~/.claude/dross/prompts/spec.md`,
                 so spec.md's own @-include is nested. The contract: a documented
                 end-to-end check (load /dross-spec, confirm the snippet's verbatim text —
                 e.g. an accept/reword/drop example string unique to _interaction.md —
                 appears in the assembled prompt). If nested @-expansion does NOT resolve,
                 record that the fallback fires: a `dross rule show`-style emitter line in
                 spec.md's pre-flight that prints the snippet, and the check then greps the
                 emitter output instead. Either way the unique snippet string must reach the
                 model; a pilot where neither path delivers the text is a fail.

## Coverage
- c-1 (builtin rule at hard severity in `dross rule show`, tests assert presence): t-1, t-3
- c-2 (reusable snippet spelling out full pattern + `make install` links it): t-2 (content), t-4 (delivery), t-3 (consistency)
- c-3 (reference mechanism decided + demonstrated end-to-end on one pilot, nested expansion proven or fallback): t-6
- c-4 (docs/interaction-audit.md enumerates every interactive command at per-decision-point granularity): t-5

## Judgment calls
- Split c-2 into t-2 (content) + t-4 (delivery/symlink) rather than one task: the two failure modes are independent — the snippet can be perfect yet the symlink not pick it up, or the install be fine yet the snippet incomplete. One owner each makes each testable in isolation.
- Added t-3 (drift reconciliation) as a dedicated task even though no criterion names "drift": three copies of the contract (rule, snippet, spec.md's existing embedded paragraph) is the highest-probability silent regression. Rejected folding it into t-1/t-2 — drift is a *relationship* between files, not a property of one, so it needs its own cross-file owner and grep contract.
- Put t-6 (nested @-include pilot) alone in wave 3 depending on t-3 and t-4: the pilot can only prove "the contract text reaches the model" once the snippet text is final (t-3) and actually installed (t-4). Demoting it to wave 2 would test against pre-reconciliation/un-installed text — a false green. This is the spec's one explicit uncertainty (snippet_delivery decision), so it gets an isolated wave and an explicit fallback branch in its contract rather than assuming nesting works.
- t-1 and t-5 both start in wave 1 (no dependency between the rule and the checklist); kept parallel rather than artificially serialized.
- t-4 verifies via `make doctor`'s existing prompt-count loop + a direct readlink, rather than adding a bespoke Go test for a Makefile behaviour: matches rule r-01's existing tooling and avoids a parallel install-verification path that could itself drift from `make install`.
