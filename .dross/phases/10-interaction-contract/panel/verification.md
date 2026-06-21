# Phase 10-interaction-contract — VERIFICATION lens

Designed backward from test contracts. For each criterion I wrote the ideal
contract first, then derived the smallest task that makes it satisfiable. The
governing question throughout: *what is the most that can be Go-tested / grep-
verified, and what is irreducibly manual?*

The c-3 nested-@-include resolution is the one thing that genuinely cannot be
proven by a Go unit test (it depends on Claude Code's loader expanding a nested
`@`-line at prompt-load time). So I isolate it: everything *around* it is
mechanically asserted (the snippet file exists, the include line is present and
points at the installed path, the install symlink resolves to it), and the
single residual manual check is narrowed to "does the loader expand the nested
line" — nothing else.

---

Phase 10-interaction-contract — 6 tasks across 3 waves

Wave 1
  t-1  Add interaction-contract builtin rule
       files:    internal/rules/rules.go, internal/rules/rules_test.go
       covers:   c-1
       contract: TestRenderEmitsInteractionContractBuiltin asserts Render(nil)
                 contains "[builtin/hard/dross-interaction-contract]" AND the
                 phrase "one decision per turn" AND "propose"; if the rule is
                 dropped, renamed, demoted from hard, or loses the do/don't core
                 the test fails. A sibling assert (extend
                 TestRenderEmitsBuiltinsBeforeUserRules) confirms it renders
                 inside <rules> before user rules.

  t-2  Author reusable interaction snippet
       files:    assets/prompts/_interaction.md
       covers:   c-2 (content half)
       description: Full playbook — propose-default-and-react, one decision per
                 turn, no walls of text, never paste the build artifact back,
                 with AskUserQuestion + accept/reword/drop examples.
       contract: grep over assets/prompts/_interaction.md must hit all four
                 invariant markers ("one decision per turn", "propose",
                 "AskUserQuestion", "never paste"/"artifact back"); a Go test in
                 internal/rules (or a new internal/prompts test) reads the file
                 via a repo-relative path and fails if any marker string is
                 absent or the file is missing. Asserts content, not just
                 existence.

  t-4  Seed interaction-audit checklist
       files:    docs/interaction-audit.md
       covers:   c-4
       description: Per-decision-point table: one row per (interactive command,
                 decision point) with a pattern-conformance column, seeded for
                 phases 11-13 to tick. Enumerate every interactive command in
                 assets/commands/ that takes user input.
       contract: a test (table-driven Go test over the rendered markdown, or a
                 scripted check) asserts docs/interaction-audit.md exists, every
                 command name from `dross-{spec,plan,quick,execute,milestone,
                 options,init,onboard,inbox}` appears as a row prefix, and the
                 table header contains a "decision point" column and a
                 "conformance"/status column. If a new interactive command is
                 added without a row, the enumeration assertion fails.

Wave 2 (depends t-1, t-2)
  t-3  Cross-reference rule and snippet
       files:    internal/rules/rules.go, internal/rules/rules_test.go,
                 assets/prompts/_interaction.md
       covers:   c-1, c-2 (the "rule may name the snippet" link in
                 rule_snippet_split)
       description: The builtin rule text names the snippet path
                 (_interaction.md) so interactive commands know where the full
                 playbook lives; keep rule terse, snippet heavy, minimal
                 overlap.
       depends_on: t-1, t-2
       contract: TestInteractionRuleNamesSnippet asserts the rendered rule text
                 contains "_interaction.md"; a divergence test asserts the rule
                 text length stays under a terse cap (e.g. < 600 chars) so the
                 heavy playbook does not leak into the every-command rule block.
                 If someone inlines the playbook into the rule, the length cap
                 fails.

  t-5  Wire snippet into install + verify symlink
       files:    Makefile
       covers:   c-2 (install half), c-3 (delivery substrate)
       description: Confirm `make install` links assets/prompts (incl.
                 _interaction.md) into ~/.claude/dross/prompts/. The dir is
                 already symlinked wholesale (Makefile:44), so verify the new
                 file is picked up; add it to the `doctor` prompt check only if
                 doctor enumerates files individually (it globs *.md, so
                 _interaction.md is auto-covered — assert that).
       depends_on: t-2
       contract: a shell/Go test runs `make install` against a temp
                 PROMPTS_DIR/SKILLS_DIR/BIN_DIR (overridable make vars), then
                 asserts readlink of $PROMPTS_DIR resolves to assets/prompts AND
                 $PROMPTS_DIR/_interaction.md is a readable path whose content
                 matches assets/prompts/_interaction.md. If the install stops
                 linking the prompts dir, or _interaction.md is excluded, the
                 resolved-path assertion fails. Also assert `make doctor`'s
                 prompt loop counts _interaction.md in p_total (its glob already
                 includes it).

Wave 3 (depends t-2, t-5)
  t-6  Pilot @-include in spec prompt + prove nested expansion
       files:    assets/prompts/spec.md, docs/interaction-audit.md
       covers:   c-3
       description: Add an `@~/.claude/dross/prompts/_interaction.md` include
                 line to spec.md (the pilot). Since dross-spec.md (wrapper)
                 already @-includes spec.md, this makes the chain wrapper →
                 prompt → snippet a NESTED include — the exact uncertainty
                 decision snippet_delivery flags. Record the pilot's result
                 (resolved / fell-back-to-emitter) and date in
                 docs/interaction-audit.md.
       depends_on: t-2, t-5
       contract: (a) MECHANICAL — grep asserts assets/prompts/spec.md contains
                 the literal line `@~/.claude/dross/prompts/_interaction.md`,
                 and a test resolves that include path against the installed
                 symlink (from t-5) to a real readable file. If the include line
                 is missing or points at a non-resolving path the test fails.
                 (b) MANUAL — the irreducible check, documented in
                 interaction-audit.md: load /dross-spec in a real Claude Code
                 session and confirm the _interaction.md text reaches the model
                 through the two-level include; if Claude Code does NOT expand
                 the nested line, fall back to a `dross rule show`-style emitter
                 in spec.md pre-flight (snippet_delivery fallback) and re-run the
                 mechanical contract against the emitter output instead.

---

## Coverage

| Criterion | Tasks            |
| --------- | ---------------- |
| c-1       | t-1, t-3         |
| c-2       | t-2, t-3, t-5    |
| c-3       | t-6, (t-5 substrate) |
| c-4       | t-4              |

All of c-1..c-4 accounted for.

## Judgment calls

- Split c-2 into content (t-2), cross-link (t-3), and install-wiring (t-5)
  rather than one task: each has a *distinct* testable surface (file-content
  grep, rule-names-snippet assertion, resolved-symlink check). One blob task
  would have a vague "snippet works" contract — rejected for un-testability.
- Made t-5 (install/symlink verification) its own task instead of folding it
  into t-2. The install target is the surface most likely to silently regress
  (a future glob/exclude change), and it is fully Go/shell-testable via the
  overridable make vars — exactly the kind of thing the verification lens wants
  pinned down rather than assumed. Rejected: trusting Makefile:44's wholesale
  dir symlink "obviously" covers the new file.
- Isolated the un-Go-testable nested-@-expansion into t-6 alone, and split its
  contract into a mechanical half (always assertable) and a narrowly-scoped
  manual half. Rejected: marking all of c-3 "manual only" — that would surrender
  the grep + symlink-resolution checks that *can* be automated around the one
  truly manual step.
- Chose spec.md as the pilot (t-6) because its wrapper already @-includes the
  prompt, so adding the snippet include exercises the *nested* case the locked
  decision singles out as the real risk. Rejected: a non-included prompt (e.g.
  status.md) as pilot — it would prove only single-level expansion and leave the
  actual uncertainty untested.
- Kept t-1/t-2/t-4 all in wave 1 (parallel): the rule, the snippet file, and the
  checklist share no inputs. Only t-3 (needs both rule and snippet text), t-5
  (needs the snippet file), and t-6 (needs snippet + install) carry real
  dependencies. Rejected: serializing them for tidiness — wave correctness
  demands they drop to wave 1.
- Did not add a project rule to .dross/rules.toml for the contract: locked
  decision rule_tier mandates a Go builtin. Contract for t-1 therefore targets
  Render output, not rules.toml parsing.
