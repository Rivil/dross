Phase 12-retrofit-setup-commands — 3 tasks across 2 waves

Wave 1
  t-1  Retrofit all 7 setup prompts
       files:    assets/prompts/init.md, assets/prompts/onboard.md,
                 assets/prompts/options.md, assets/prompts/rule.md,
                 assets/prompts/inbox.md, assets/prompts/quick.md,
                 assets/prompts/milestone.md
       covers:   c-1, c-2, c-3, c-4
       contract: For each of the 7 prompts: a grep for `dross interaction show`
                 in its pre-flight returns a hit (if any prompt loses the emitter
                 wire, t-3's TestSetupPromptsWireEmitter fails for that name); a
                 grep for "interaction playbook" returns a hit (drop it and
                 TestSetupPromptsReferenceContract fails); the dead nested
                 @-include line "@~/.claude/dross/prompts/_interaction.md" is
                 absent (re-add it and the emitter test fails). Per-prompt anchor
                 checks: options.md carries a section-pick gate phrase
                 ("project / defaults / rules / profile / env") before any
                 per-setting turn; milestone.md keeps §3 success-criteria, §4
                 non-goals, §5 phase-order as three distinct propose-and-react
                 segments; quick.md's §2 approval + §4 red-path each hold exactly
                 one AskUserQuestion and reuse execute.md's proceed/steer/abort
                 wording. If a rewrite collapses two decisions into one turn or
                 pastes a composed project.toml/milestone.toml/rules block back
                 wholesale, the matching t-3 anchor assertion fails.

Wave 2 (depends t-1)
  t-2  Flip audit rows for the 7 commands
       files:    docs/interaction-audit.md
       covers:   c-5
       contract: In docs/interaction-audit.md the "## Setup & config (phase 12)"
                 sections for dross-{init,onboard,options,rule,inbox,quick,
                 milestone} each carry ✅ and zero ⬜/🟡/❌ markers, and each
                 row's decision points match t-1's rewritten turns (e.g. options
                 lists the section-pick gate, milestone lists criteria/non-goals/
                 phase-order as separate rows). If any of the 7 sections still
                 shows a pending/partial/violates marker, t-3's
                 TestSetupAuditSectionsConform fails for that command.

  t-3  Add setup-command interaction test
       files:    internal/cmd/interaction_setup_test.go
       covers:   c-1, c-2, c-5
       contract: New test in package cmd mirroring interaction_coreloop_test.go
                 over setupPrompts = {init,onboard,options,rule,inbox,quick,
                 milestone}: TestSetupPromptsWireEmitter fails if any prompt
                 omits `dross interaction show` or carries deadIncludeLine;
                 TestSetupPromptsReferenceContract fails if any prompt omits
                 interactionRefPhrase ("interaction playbook");
                 TestSetupAuditSectionsConform fails if any of the 7 audit
                 sections lacks ✅ or retains ⬜/🟡/❌; plus c-3/c-4 anchor checks
                 via promptSection — options.md section-pick gate present,
                 milestone.md three scoping segments each with one
                 AskUserQuestion, quick.md §2/§4 each one AskUserQuestion. Reuses
                 repoRootFromTest, deadIncludeLine, interactionRefPhrase,
                 promptSection. Does not duplicate
                 interaction_audit_test.go's section-existence guard.

## Coverage

- c-1 (pre-flight invokes `dross interaction show`, all 7) → t-1 (edits), t-3 (TestSetupPromptsWireEmitter)
- c-2 ("interaction playbook" reference, all 7) → t-1 (edits), t-3 (TestSetupPromptsReferenceContract)
- c-3 (no bundled decisions; one turn per setting) → t-1 (rewrite into one-AskUserQuestion turns), t-3 (per-prompt anchor checks on options/milestone/quick)
- c-4 (no wholesale artifact paste; one-line summary) → t-1 (rewrite confirmations to summaries), t-3 (no-dump anchor in the per-prompt checks)
- c-5 (audit checklist entry per command, propose-and-react confirmed) → t-2 (flip rows), t-3 (TestSetupAuditSectionsConform)

## Judgment calls

- Grouped all 7 prompt edits into one task (t-1) rather than 7 tasks: the retrofit is one near-identical mechanical pattern (wire emitter, add playbook phrase, split turns, summarize confirmations) per the spec's uniform retrofit_depth; 7 tasks would be ceremony with no reviewability gain, since t-3's per-name loop already pins each prompt individually.
- One test file (t-3) instead of per-criterion test tasks: c-1/c-2/c-5 are three table-driven asserts over the same prompt set plus the audit doc — splitting them buys nothing and the phase-11 template (interaction_coreloop_test.go) is a single file covering the same shape.
- t-2 (audit) and t-3 (test) are both wave 2 and parallel, not chained: each strictly needs t-1's rewritten prompts (rows/asserts must match the new turns) but neither needs the other — the test reads the audit file but doesn't need it authored in a prior wave within the run's eventual `make install`/check step.
- Folded `make install` (RULE r-01) into execution discipline, not a task: it's a build step the executor runs before relying on a prompt/Go change, not a deliverable traceable to a criterion — a standalone "run make install" task would be untestable and violate the no-task-without-contract rule.
- Did not add a quick/inbox-mirror-verification task: quick_inbox_mirror is satisfied inside t-1 by reusing execute.md's verbatim proceed/steer/abort gate, and pinned by t-3's quick.md anchor check — a separate task would duplicate that coverage.
