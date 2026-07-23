# Planner draft — verification lens (design backward from test contracts)

Method: each criterion's ideal failing test was written first; tasks are the
smallest changes that make those tests satisfiable. Contracts reference the
repo's existing test idioms (plan_edit_test.go table style, task_test.go CLI
round-trips, spec_prompt_test.go needle assertions, commands_parity_test.go).

```
Phase task-reordering — 5 tasks across 2 waves

Wave 1
  t-1  Add Plan.MoveTask engine with guards
       files:    internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:   c-2, c-3, c-4
       depends:  —
       description: Add MoveTask(id, anchor, before) to the plan-edit layer:
                    reposition the task slice entry relative to the anchor,
                    adopt the anchor's wave on a legal cross-wave move (locked
                    move_wave_semantics), reflow dependents' waves to stay
                    > the mover's, and reject — mutating nothing — any move
                    that (a) places the mover positionally before one of its
                    depends_on or after a task that depends on it, (b) would
                    give the mover a wave <= its deepest dependency's wave,
                    (c) moves a non-pending task, or (d) lands a pending task
                    before a done/in_progress task (locked move_execution_guard).
                    IDs and TaskSeq are never touched.
       test_contract:
         - TestMoveTaskRejectsBeforeDependency: t-2 (depends_on t-1) moved
           before t-1 returns an error naming both t-2 and t-1, and the plan
           struct is deep-equal to its pre-call snapshot — if the guard is
           dropped or the reject path mutates first, this fails.
         - TestMoveTaskRejectsAfterDependent: moving t-1 after t-2 (which
           depends on t-1) errors; plan unchanged.
         - TestMoveTaskExecutionGuard: moving a done task errors, and moving
           a pending task to before an in_progress task errors — delete the
           locked move_execution_guard check and this fails.
         - TestMoveTaskAdoptsAnchorWaveAndReflows: legal move of a wave-1
           task after a wave-2 anchor sets the mover's wave to 2 and bumps
           its dependent from wave 2 to wave 3; ValidatePlan(plan, spec)
           returns nil afterwards — skip the reflow and ValidatePlan's
           depends_on/wave state exposes the stale dependent wave.
         - TestMoveTaskRejectsWaveIllegalAdoption: moving a task whose
           dependency sits in wave 2 before a wave-1 anchor errors (the
           "when dependencies allow; otherwise reject" half of the locked
           decision); plan unchanged.
         - TestMoveTaskKeepsIDsStable: after a legal move the set of task
           ids, the mover's id, TaskSeq, and NextTaskID() output are all
           identical to before — renumber on move and this fails (c-4).

  t-2  Order-aware NextRunnable tie-break
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-5
       depends:  —
       description: Change NextRunnable's same-wave tie-break from
                    lexicographic id (t.ID < best.ID) to document position in
                    plan.toml. Wave still dominates; deps-done filter
                    unchanged.
       test_contract:
         - TestNextRunnableFollowsDocumentOrder: a plan whose task array
           lists t-2 before t-1 (both wave 1, pending, no deps) returns t-2;
           under today's id tie-break it returns t-1, so this test fails on
           the current code and pins the new behavior.
         - TestNextRunnableWaveStillDominates: a wave-2 task positioned
           before a wave-1 task is NOT returned first — revert to pure
           document order and this fails (and existing
           TestTaskNextRespectsWaveAndDeps in internal/cmd/task_test.go must
           stay green).

  t-3  Replace spec §2 free-recall with candidate slate
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-6
       depends:  —
       description: Rewrite §2 of the spec prompt: open with a proposed
                    candidate-criteria slate derived from milestone scope +
                    CLI/context gap analysis, each item gated
                    accept/reword/drop one per turn; delete the freeform
                    "List 3-7 outcomes" ask. Keep the §1 deferred-idea
                    resurfacing feeding the same slate.
       test_contract:
         - TestSpecPromptProposesCandidateSlate: normalized spec.md contains
           the needles "candidate" slate framing, "milestone" scope +
           "gap" analysis as derivation sources, and the per-item
           "accept / reword / drop" gate — drop the slate and a needle
           disappears.
         - TestSpecPromptFreeRecallGone: normalized spec.md does NOT contain
           "list 3-7" — restore the free-recall ask and this test fails.

  t-4  Ship /dross-techdebt skill, repoint status action
       files:    assets/commands/dross-techdebt.md, assets/prompts/techdebt.md,
                 internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-7
       depends:  —
       description: Add the thin skill pair — dross-techdebt.md shim
                    (@-includes ~/.claude/dross/prompts/techdebt.md, mirrors
                    dross-watch.md's shape) and prompts/techdebt.md (run
                    `dross techdebt`, read the newest .dross/techdebt run
                    report, summarize top findings). Change actionCatalog's
                    tech-debt entry command from "dross techdebt" to
                    "/dross-techdebt".
       test_contract:
         - TestCommandsPromptsParity (existing) fails if either new file is
           added without its twin; add an explicit pin asserting
           cmds["techdebt"] && prompts["techdebt"] so deleting BOTH also
           fails (the parity loops alone miss that case).
         - TestStatusActionsAllSlashCommands: every actionCatalog entry's
           command begins with "/" — revert tech-debt to the bare CLI hint
           "dross techdebt" and this fails; the rendered actions line for
           the tech-debt area contains "/dross-techdebt"
           (extends TestRenderActionAreasAvailableEmitsCommand).

Wave 2 (depends t-1, t-2)
  t-5  Wire dross task move subcommand
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1, c-2, c-4, c-5
       depends:  t-1, t-2
       description: Register taskMove() in Task(): `dross task move
                    <phase-id> <task-id> --before/--after <other-id>`, flag
                    validation via the existing resolveAnchor, mutation via
                    Plan.MoveTask, persistence via saveIfValid so a rejected
                    move never reaches Save.
       test_contract:
         - TestTaskMoveRepositions: seed plan t-1,t-2,t-3; run
           `task move ph t-3 --before t-1`; reloading plan.toml yields task
           order t-3,t-1,t-2 (c-1 — no hand-editing).
         - TestTaskMoveFlagValidation: neither flag, and both flags, produce
           resolveAnchor's exact error strings (parity with phase move/add).
         - TestTaskMoveRejectLeavesFileUntouched: an illegal move (mover
           after its dependent) exits non-zero and plan.toml's bytes read
           before and after are identical — route the write before the
           guard and this fails (c-2's "left untouched" half).
         - TestTaskMoveThenNextRespectsOrder: both wave 1 pending; after
           `task move ph t-2 --before t-1`, `task next ph` prints t-2 —
           the end-to-end c-5 proof through the real CLI.
         - TestTaskMoveKeepsID: after the move `task show ph t-3` still
           resolves, and the saved file contains id "t-3" exactly once
           (c-4 — history references stay valid).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-5 |
| c-2 | t-1 (engine guards), t-5 (file-untouched at CLI) |
| c-3 | t-1 |
| c-4 | t-1 (ids/TaskSeq stable), t-5 (end-to-end) |
| c-5 | t-2 (tie-break), t-5 (CLI proof) |
| c-6 | t-3 |
| c-7 | t-4 |

## Judgment calls

- **Split move into engine (t-1) + CLI (t-5)** rather than one task: the two layers carry different contracts (struct-level mutate-nothing vs plan.toml bytes-untouched) and it mirrors the existing AddTask/RemoveTask ↔ taskAdd/taskRemove layering; rejected a single 4-file task spanning both layers.
- **NextRunnable tie-break = document position within a wave, wave still dominant**: rejected pure document order (breaks interleaved-wave plans and existing TestTaskNextRespectsWaveAndDeps) and rejected keeping the id tie-break (makes c-5 unsatisfiable — a same-wave move changes nothing NextRunnable reads today).
- **Wave-illegal adoption folded into the c-2 guard** (t-1) rather than silently deriving a different wave: the locked move_wave_semantics decision says "otherwise the c-2 guard rejects", so deriveWave-style fallback would violate a locked decision.
- **Dependent wave reflow on a legal cross-wave move** (c-3 "waves are re-derived"): bump dependents transitively to > the mover's new wave; rejected refusing such moves outright — that would outlaw legal moves c-2 doesn't forbid.
- **Status repoint bundled into t-4** with the skill it points at: a one-line actionCatalog edit fails the too-small test as its own task, and its test contract is meaningless until /dross-techdebt exists.
- **prompts/techdebt.md is genuinely thin** (run CLI, read newest run report, summarize): the CLI already owns the scan and last_run stamping (internal/cmd/techdebt.go), so the prompt adds orchestration only — matching the "thin skill over `dross techdebt`" wording, not a re-implementation.
- **No README/docs task**: no criterion demands docs; the repo's ship-time README-sync convention covers it, and padding the phase would dilute wave-1 parallelism.
- **`task move` owns position+wave jointly; `task edit --wave` untouched**: edit changes labels without position, move changes position with locked wave adoption — merging them would let --wave contradict the locked position-driven semantics.
