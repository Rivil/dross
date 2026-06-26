# Phase 10-interaction-contract — MVP draft

Phase 10-interaction-contract — 4 tasks across 2 waves

Wave 1
  t-1  Add interaction-contract builtin rule
       files:    internal/rules/rules.go, internal/rules/rules_test.go
       covers:   c-1
       contract: a new test (e.g. TestRenderEmitsInteractionContractBuiltin) asserts
                 `Render(nil)` contains `[builtin/hard/dross-interaction-contract]` and the
                 do/don't clauses ("one decision per turn", "never paste"); if the builtin is
                 dropped or downgraded from hard, that test fails.

  t-2  Author reusable interaction snippet
       files:    assets/prompts/_interaction.md
       covers:   c-2 (content half)
       contract: grep over assets/prompts/_interaction.md finds all four pattern phrases
                 ("propose", "one decision per turn", "wall", "never paste"/build-artifact) plus
                 an AskUserQuestion accept/reword/drop example; missing any → grep check fails.

  t-4  Seed interaction-audit checklist
       files:    docs/interaction-audit.md
       covers:   c-4
       contract: one row per dross-*.md command flagged interactive (those whose wrapper lists
                 AskUserQuestion in allowed-tools) appears in the table with a decision-point
                 column; a grep-driven check (count interactive wrappers vs rows) fails if a
                 command is missing or a row lacks a decision-point entry.

Wave 2 (depends t-2)
  t-3  Wire snippet into spec.md pilot + prove nested @-include
       files:    assets/prompts/spec.md
       covers:   c-2 (install-link half), c-3
       description: Add `@~/.claude/dross/prompts/_interaction.md` to spec.md (itself @-included
                 by assets/commands/dross-spec.md). Run `make install`, then resolve the chain to
                 prove the snippet text reaches the model two levels deep.
       contract: after `make install`, following the symlinks
                 ~/.claude/skills/dross-spec/SKILL.md → assets/commands/dross-spec.md →
                 @prompts/spec.md → @prompts/_interaction.md resolves to a real file, and grep for
                 a unique snippet sentinel string succeeds at the leaf; if nested @-expansion or the
                 `make install` prompts symlink breaks, the resolution/grep fails. (If nesting can't
                 resolve, the locked fallback — a `dross rule show`-style emitter line in spec.md's
                 pre-flight — is grepped for instead.)
       depends_on: t-2

## Coverage
- c-1 → t-1
- c-2 → t-2 (snippet content), t-3 (`make install` links it so commands can reference it)
- c-3 → t-3
- c-4 → t-4

## Judgment calls
- Merged "author snippet" and "wire it into the pilot" candidates into t-2 + t-3 rather than three tasks: snippet authoring (content, c-2) is independent wave-1 work, but the install-link proof of c-2 is inseparable from the c-3 nested-@-include demonstration (same `make install` + same symlink walk), so they collapse into one wave-2 task. Rejected a standalone "verify make install links snippet" task — that check is the c-3 contract itself.
- Picked spec.md as the pilot (not a new throwaway prompt): the locked decision names it, it already carries the interaction pattern informally, and reusing it avoids a speculative scaffold. Rejected creating a fresh pilot command.
- t-1, t-2, t-4 are all pure wave-1 (no cross-dependency): the rule, the snippet file, and the checklist touch disjoint files and need none of each other's output, so they run in parallel. Only t-3 waits — it needs the snippet file to exist before it can @-include and install it. Rejected forcing the checklist (t-4) into a later wave; it depends on the command wrappers (already in repo), not on this phase's artifacts.
- Kept the rule (t-1) and snippet (t-2) as separate tasks despite both being "the contract": the locked `rule_snippet_split` decision mandates terse-rule-vs-full-playbook with different homes (Go builtin vs markdown) and different test surfaces (Go test vs grep), so merging would span two layers and two contracts — a forced split, not gold-plating.
- No dedicated test-only task: each task carries its own concrete contract (Go test, grep, symlink-walk), so a separate verification task would be empty structure. Rejected per MVP bias.
