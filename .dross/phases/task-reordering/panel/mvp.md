# Draft: MVP lens (smallest task set)

Phase task-reordering — 4 tasks across 2 waves

Wave 1
  t-1  Add MoveTask with guards and order-aware next
       files:    internal/phase/plan_edit.go, internal/phase/plan_edit_test.go,
                 internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-2, c-3, c-4, c-5
       description:
                 Implement `(p *Plan) MoveTask(id, anchor string, before bool) error` in
                 plan_edit.go: reposition the task in the ordered p.Task slice; on a
                 cross-wave move adopt the anchor's wave (locked move_wave_semantics),
                 then re-derive dependents' waves via the existing deriveWave logic so
                 wave labels stay consistent. Reject — mutating nothing — any move that
                 lands the task before one of its depends_on or after a task that depends
                 on it (c-2), any move of a non-pending task, and any move to a position
                 before a done/in_progress task (locked move_execution_guard). The task
                 keeps its ID; no renumbering (c-4). In phase.go, change NextRunnable's
                 within-wave tie-break from lexicographic ID to array position so a
                 within-wave reorder changes what runs next (c-5).
       test_contract:
                 - moving a task before one of its depends_on (or after a dependent)
                   returns an error and the plan's Task slice deep-equals its pre-call
                   value — the reject-mutates-nothing guard
                 - moving a done task, or moving a pending task to a position before an
                   in_progress task, is rejected with an error naming the blocker
                 - after a legal cross-wave move the moved task carries the anchor's
                   wave, a dependent downstream gets its wave re-derived, and
                   phase.ValidatePlan returns nil
                 - after any legal move every task ID is byte-identical to before —
                   only slice positions differ
                 - on two pending same-wave tasks with no deps, NextRunnable returns
                   the one earlier in array order even when its ID sorts higher
       depends_on: []
       status:   pending

  t-3  Replace spec §2 free-recall with candidate slate
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-6
       description:
                 Rewrite §2 "Acceptance criteria" in spec.md: open by *proposing* a
                 candidate-criteria slate derived from milestone scope (milestone.toml
                 success criteria + phase position) and a CLI/context gap analysis,
                 then gate each candidate accept/reword/drop via AskUserQuestion, one
                 per turn. Delete the freeform "List 3-7 user-observable, testable
                 outcomes" ask. Keep the quality bar and the trailing "anything
                 missing?" catch-all. Add needle assertions to spec_prompt_test.go.
       test_contract:
                 - spec_prompt_test fails if the "list 3-7" free-recall phrasing
                   reappears anywhere in spec.md §2
                 - spec_prompt_test fails if §2 loses the candidate-slate needles
                   (propose/candidate wording, milestone-scope derivation, and the
                   accept/reword/drop gate)
       depends_on: []
       status:   pending

  t-4  Add /dross-techdebt skill, repoint status actions
       files:    assets/commands/dross-techdebt.md, assets/prompts/techdebt.md,
                 internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-7
       description:
                 Create the thin shim assets/commands/dross-techdebt.md (frontmatter +
                 @~/.claude/dross/prompts/techdebt.md, matching dross-status.md's shape)
                 and assets/prompts/techdebt.md — a thin orchestration over `dross
                 techdebt` (run it, relay output, suggest follow-up). Flip the
                 tech-debt entry in status.go's actionCatalog from command "dross
                 techdebt" to "/dross-techdebt" so all three actions lines are slash
                 commands. Update status_test.go expectations.
       test_contract:
                 - TestCommandsPromptsParity fails if dross-techdebt.md ships without
                   prompts/techdebt.md (or the prompt without the command)
                 - the status idle-actions test fails if the tech-debt line renders
                   "dross techdebt" instead of "/dross-techdebt"
       depends_on: []
       status:   pending

Wave 2 (depends t-1)
  t-2  Wire dross task move subcommand
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1, c-2
       description:
                 Register taskMove() in Task()'s AddCommand list: `dross task move
                 <phase-id> <task-id>` with --after/--before resolved through the
                 existing resolveAnchor (exactly one required). Load via
                 loadPhasePlanAndSpec, call plan.MoveTask, persist via saveIfValid so
                 a rejected move never reaches Save and plan.toml stays byte-unchanged.
       test_contract:
                 - `dross task move <p> t-1 --after t-2` on a fixture plan rewrites
                   plan.toml with t-1 positioned after t-2 and every task ID unchanged
                 - an illegal move (target before its dependency) returns a non-nil
                   error and plan.toml on disk is byte-identical to before the call
                 - passing both --before and --after, or neither, errors via
                   resolveAnchor without touching the plan
       depends_on: [t-1]
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-1, t-2 |
| c-3 | t-1 |
| c-4 | t-1 |
| c-5 | t-1 |
| c-6 | t-3 |
| c-7 | t-4 |

## Judgment calls

- Merged the NextRunnable tie-break fix (c-5) into t-1 instead of a standalone task — one-function change in the same package; alone it is <10 min and would only add graph noise.
- No separate "dross validate integration" task for c-3 — move routes through the existing saveIfValid → phase.ValidatePlan path (a documented superset of `dross validate`'s plan checks), so c-3's validate clause is a contract on t-1/t-2, not new plumbing.
- c-2's mutate-nothing guard lives in the domain MoveTask (error before any slice mutation), not a CLI pre-check — reuses the established "rejected mutation leaves plan.toml byte-unchanged" pattern that add/remove/edit already follow via saveIfValid; rejected duplicating guard logic in cmd.
- t-4 keeps /dross-techdebt a genuinely thin shim over the existing `dross techdebt` CLI (criterion wording says "thin skill"); rejected authoring a new multi-step orchestration prompt — speculative structure with no criterion behind it.
- No README/docs task and no `--to-wave`/index-based move flags — no criterion requires them; the spec's move contract is anchor-relative only (--before/--after), mirroring the shipped phase-move UX.
- Wave re-derivation after a legal move is scoped to the moved task + its downstream dependents (deriveWave cascade), not a whole-plan reflow — smallest change that makes c-3's consistency clause hold; a full reflow would renumber waves on untouched tasks for no criterion.
