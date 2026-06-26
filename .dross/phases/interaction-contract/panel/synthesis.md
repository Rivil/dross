# Phase 10-interaction-contract — SYNTHESIS

Judged from three independently-drafted decompositions (risk / mvp / verification).
I authored none of them. Below: scores, the merged plan grafted from the strongest
skeleton plus concrete improvements, and the genuine disagreements left explicit
rather than papered over.

## Scores

| Draft        | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
| ------------ | ----------------- | ------------------------- | ----------- | ---------------- |
| risk         | all c-1..c-4, plus an un-mandated drift relationship (t-3) — fullest coverage | strong, cross-file grep contracts; the drift contract is the sharpest single contract in any draft | 6 tasks, one failure-mode per owner; arguably over-splits with the extra drift task | correct — t-6 isolated in wave 3 depending on both t-3 (final text) and t-4 (installed); the only draft that gates the pilot on reconciled text |
| mvp          | all c-1..c-4, but c-2's three surfaces compressed into two tasks | adequate; the merged t-3 contract is a single symlink-walk that blends install-link + nested-include into one assertion | 4 tasks — leanest; collapses install verification into the pilot, losing an independent owner for the regression-prone install surface | mostly correct but coarse: only 2 waves; t-3 carries both delivery and pilot, so a pre-reconciliation/un-isolated install bug surfaces as a pilot failure |
| verification | all c-1..c-4; cleanest criterion→task table | **strongest** — overridable make-var temp-dir install test, terse-rule length cap, explicit mechanical/manual split on the one un-Go-testable step | 6 tasks, each with a *distinct testable surface* (file grep / rule-names-snippet / resolved symlink); principled, not split-for-tidiness | correct — t-1/t-2/t-4 parallel in wave 1, t-3 & t-5 in wave 2, t-6 wave 3 on t-2+t-5 |

**Skeleton: `verification.md`.** It has the strongest and most mechanically-honest
contracts (the overridable-make-var install test and the mechanical-vs-manual c-3
split are the best test thinking in the panel), the cleanest criterion→task mapping,
and principled granularity where every task owns a distinct testable surface. risk's
6/3 structure is nearly identical; verification edges it on contract quality and on
*why* each split exists. mvp is the cleanest skeleton to read but loses two
independently-testable surfaces (install verification, drift) that the milestone's
anti-batching/anti-drift intent specifically wants pinned.

## Merged plan

Skeleton = verification's 6-task / 3-wave shape. Grafts: risk's drift task folded
into t-3, risk's verbatim-phrase drift contract, risk's explicit fallback branch in
the pilot contract, and verification's overridable-make-var install test + length cap.

Format per task: `t-N  title  [origin]` then files / covers / depends / contract.

### Wave 1 (no cross-dependencies — all touch disjoint files)

```
t-1  Add interaction-contract builtin rule                    [verification+mvp+risk]
     files:    internal/rules/rules.go, internal/rules/rules_test.go
     covers:   c-1
     contract: TestRenderEmitsInteractionContractBuiltin asserts Render(nil)
               contains "[builtin/hard/dross-interaction-contract]" AND
               "one decision per turn" AND "propose" AND
               "never paste the build artifact back"; fails if the rule is
               dropped from Builtins, renamed, or demoted from hard. Sibling
               assert (extend TestRenderEmitsBuiltinsBeforeUserRules) confirms
               it renders inside <rules> before user rules. [graft from risk:]
               also assert the rule text names the snippet path "_interaction.md"
               so the rule→snippet pointer can't silently vanish.

t-2  Author reusable interaction snippet                       [all three]
     files:    assets/prompts/_interaction.md
     covers:   c-2 (content half)
     contract: a Go test (internal/rules or new internal/prompts) reads the file
               via a repo-relative path and fails if missing or if any of the
               four playbook markers is absent: "propose"/propose-default-and-react,
               "one decision per turn", "wall" (no-walls-of-text),
               "never paste"/"artifact back". Must also contain an AskUserQuestion
               accept/reword/drop example. Asserts content, not just existence.
               Snippet is the FULL playbook per rule_snippet_split.

t-4  Seed interaction-audit checklist                          [all three]
     files:    docs/interaction-audit.md
     covers:   c-4
     contract: a check (table-driven Go test over the rendered markdown, or a
               scripted grep) asserts the file exists; every interactive command
               in assets/commands/ — those whose wrapper lists AskUserQuestion —
               appears as a row prefix; the table header has a "decision point"
               column AND a "conformance"/status column; and [graft from risk:]
               each command's section has >=1 PER-DECISION-POINT row, not one row
               per command. If a new interactive command is added without a
               section (diff against `grep -l AskUserQuestion assets/commands/`),
               the enumeration assertion fails.
```

### Wave 2 (depends t-1, t-2)

```
t-3  Reconcile rule / snippet / spec.md text; cross-reference  [risk⊕verification]
     files:    internal/rules/rules.go, internal/rules/rules_test.go,
               assets/prompts/_interaction.md, assets/prompts/spec.md
     covers:   c-1, c-2 (consistency / the "rule may name the snippet" link)
     depends:  t-1, t-2
     contract: [verification:] TestInteractionRuleNamesSnippet asserts the
               rendered rule text contains "_interaction.md"; a terse-cap assert
               keeps rule text under a length bound (e.g. < 600 chars) so the
               heavy playbook can't leak into the every-command rule block.
               [graft from risk — drift:] a drift check asserts the canonical
               do/don't phrases ("one decision per turn", "never paste the build
               artifact back") appear in BOTH the builtin rule text AND the
               snippet, AND that spec.md no longer carries its OWN divergent
               wall-of-text paragraph (risk flags lines ~5/143/172) — it must
               point at the snippet instead. If spec.md keeps a hand-rolled
               duplicate alongside the @-include, the grep for that phrase
               outside _interaction.md fails.

t-5  Wire snippet into install + verify symlink                [verification+risk]
     files:    Makefile (+ internal/install or a doctor/test target)
     covers:   c-2 (install/delivery half)
     depends:  t-2
     contract: [verification:] a shell/Go test runs `make install` against a temp
               PROMPTS_DIR/SKILLS_DIR/BIN_DIR (overridable make vars), then asserts
               readlink of $PROMPTS_DIR resolves to assets/prompts AND
               $PROMPTS_DIR/_interaction.md is readable with content matching
               assets/prompts/_interaction.md. Fails if install stops linking the
               prompts dir or excludes the new file. [graft from risk:] also assert
               `make doctor`'s prompt loop counts _interaction.md (its *.md glob
               already includes it) — i.e. p_total/p_ok reflects the new file.
```

### Wave 3 (depends t-3, t-5)

```
t-6  Pilot @-include in spec.md; prove nested expansion        [all three]
     files:    assets/prompts/spec.md, docs/interaction-audit.md
     covers:   c-3
     depends:  t-3, t-5   [provisional default — see Disagreements]
     contract: (a) MECHANICAL — grep asserts assets/prompts/spec.md contains the
               literal line `@~/.claude/dross/prompts/_interaction.md`, and a test
               resolves that path against the installed symlink (from t-5) to a
               real readable file; fails if the line is missing or non-resolving.
               (b) MANUAL (irreducible) — documented in interaction-audit.md: load
               /dross-spec in a real Claude Code session and confirm a snippet
               sentinel string (e.g. a unique accept/reword/drop example from
               _interaction.md) reaches the model through the TWO-LEVEL include
               (wrapper dross-spec.md → spec.md → _interaction.md).
               [graft from risk — explicit fallback branch:] if Claude Code does
               NOT expand the nested line, the snippet_delivery fallback fires —
               a `dross rule show`-style emitter line in spec.md's pre-flight that
               prints the snippet — and the mechanical contract is re-run against
               the emitter output instead. Record result (resolved / fell-back)
               and date in docs/interaction-audit.md. A pilot where neither path
               delivers the sentinel text is a FAIL.
```

Coverage: c-1 → t-1, t-3 · c-2 → t-2 (content), t-3 (consistency), t-5 (delivery)
· c-3 → t-6 (with t-5 as substrate) · c-4 → t-4. No criterion uncovered.

## Disagreements

**D-1 — Is install verification its own task, or folded into the pilot?**
- *verification & risk:* a dedicated install/symlink task (t-5/t-4) — the install
  target is the surface most likely to silently regress (a future glob/exclude
  change) and is fully shell/Go-testable; it deserves an independent owner.
- *mvp:* fold it into the pilot t-3 — "verify make install links the snippet" *is*
  the c-3 contract, so a standalone task is empty structure; one symlink-walk
  proves both.
- **Provisional default: keep it separate (t-5).** Taken from the skeleton + risk.
- *Why it matters:* separation buys an independent red when the install regresses
  vs. when nested-expansion fails — mvp's merge would surface a glob/exclude bug as
  a *pilot* failure, misdirecting the fix and (per the repo's r-01 / stale-binary
  history) hiding an install regression behind the one genuinely-manual check.

**D-2 — Does a drift-reconciliation task exist at all?**
- *risk:* yes, a dedicated cross-file task — three copies of the contract (rule,
  snippet, spec.md's existing embedded paragraph) is the highest-probability silent
  regression, and drift is a *relationship* between files, so it needs its own owner.
- *verification:* a cross-reference task (t-3) exists but scoped to rule↔snippet
  (names-snippet + length cap); it does NOT police spec.md's pre-existing duplicate.
- *mvp:* no drift task at all — no criterion names drift; rejected as gold-plating.
- **Provisional default: include it, merged into t-3** (verification's cross-ref task
  absorbs risk's drift contract — one wave-2 cross-file owner, not two tasks).
- *Why it matters:* the spec's own decisions (rule_snippet_split, "minimal content
  overlap") presuppose the three copies stay in sync; without the spec.md-duplicate
  grep, a stale hand-rolled paragraph survives next to the @-include and the model
  sees two divergent contracts. Folding rather than adding a 7th task keeps mvp's
  leanness objection partly honoured. (Note: this is the one place I extend an
  *existing* task's contract with another draft's contract — no new task invented.)

**D-3 — What does the pilot depend on: reconciled text, or just the snippet file?**
- *risk:* t-6 depends on t-3 AND t-4 (install) — the pilot must test FINAL,
  reconciled, installed text; gating only on the raw snippet risks a false green
  against pre-reconciliation copy.
- *verification:* t-6 depends on t-2 (snippet file) AND t-5 (install) — not on the
  cross-ref/drift task.
- *mvp:* t-3 (its combined pilot) depends only on t-2.
- **Provisional default: depend on t-3 AND t-5** (risk's gating logic, mapped onto
  the merged task names).
- *Why it matters:* this is the project's documented false-green failure mode — if
  the pilot runs before t-3 settles the canonical phrasing, it validates text that
  the very next task rewrites, and the "contract reaches the model" proof is stale.
  Per the repo's commit-safety rules, the gate (final text) must be observed before
  the gated work (pilot) is even queued. Costs nothing but a wave-3 placement that
  all three drafts already accept for the pilot.

**D-4 — 2 waves or 3?**
- *mvp:* 2 waves (4 tasks) — fewest moving parts.
- *risk & verification:* 3 waves (6 tasks) — the pilot must sit alone after both
  the text is final and the install is verified.
- **Provisional default: 3 waves.** Follows from D-1/D-2/D-3: once install and drift
  are their own tasks and the pilot gates on both, a third wave is forced, not
  cosmetic. mvp's 2-wave shape is only reachable by accepting its merges, which D-1
  and D-3 decline.
