# Panel synthesis — merged plan (judge over risk / mvp / verification drafts)

## Scores

Scale 1-5 per dimension.

| draft | dimension | score | note |
|---|---|---|---|
| risk | criteria coverage | 5 | all 7 covered; c-1/c-2/c-5 dual-proved (engine + CLI e2e) |
| risk | test-contract specificity | 5 | concrete inputs→outputs everywhere; unique edge cases (out-of-order done history, no-op/self-anchor, byte-capture reject proof) |
| risk | granularity | 3 | splice/guards/reflow split into three same-file tasks (t-1/t-5/t-6) — forces a serial chain; t-6 is borderline too small alone |
| risk | wave correctness | 4 | deps internally correct, but the same-file split costs a third wave; t-7 runs beside t-6 so the CLI e2e wave can ship before re-derivation lands (legal since t-7 doesn't claim c-3, but fragile) |
| mvp | criteria coverage | 4 | all 7 hit, but mostly single-point: no CLI-level proof for c-4/c-5, no wave-illegal-adoption case |
| mvp | test-contract specificity | 3 | solid core contracts, fewest edge cases — misses out-of-order done history, wave-illegal adoption reject, parity both-deleted gap |
| mvp | granularity | 2 | t-1 spans 4 files / 2 packages / 4 criteria — exactly the "one task owns five failure modes" mega-task risk warns ships one untested |
| mvp | wave correctness | 5 | 2 waves, deps minimal and correct |
| verification | criteria coverage | 5 | all 7; deliberate engine+CLI dual coverage on c-2/c-4/c-5 |
| verification | test-contract specificity | 5 | named tests, fails-on-current-code pins (NextRunnable), needle lists, catches the parity both-deleted blind spot |
| verification | granularity | 4 | engine t-1 carries 3 criteria / 6 tests — heavier than ideal, but one file, one layer, one coherent contract |
| verification | wave correctness | 5 | 2 waves; t-5 depends exactly on t-1 + t-2, mirroring the shipped AddTask/taskAdd layering |

**Skeleton: verification.** It has the same full coverage and contract sharpness as risk but achieves it in 2 waves with a layering (engine task → CLI task) that mirrors the repo's existing plan-edit structure, and it's the only draft that designed each contract to fail on current code. Risk's extra value is grafted below as contract lines, not structure.

## Merged plan

```
Phase task-reordering — 5 tasks across 2 waves

Wave 1
  t-1  Add Plan.MoveTask engine with guards and reflow   [verification, grafts: risk]
       files:      internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:     c-2, c-3, c-4
       depends_on: (none)
       description: Add MoveTask(id, anchor, before): splice the task relative to
                    the anchor in p.Task; on a legal cross-wave move adopt the
                    anchor's wave (locked move_wave_semantics) and reflow PENDING
                    dependents' waves so every dependent stays > the mover's wave
                    (see D3); done/in_progress waves stay frozen. Reject — mutating
                    nothing — any move that (a) places the mover positionally
                    before one of its depends_on or after a task that depends on
                    it, (b) would give the mover a wave <= its deepest dep's wave,
                    (c) moves a non-pending task, or (d) lands a pending task
                    before a done/in_progress task (locked move_execution_guard).
                    IDs and TaskSeq are never touched. Own the bad-input paths:
                    unknown task/anchor, anchor==self, no-op move.
       contract:   - TestMoveTaskRejectsBeforeDependency: t-2 (depends_on t-1)
                     moved before t-1 errors naming both ids; plan deep-equals
                     its pre-call snapshot                        [verification]
                   - TestMoveTaskRejectsAfterDependent: moving t-1 after its
                     dependent t-2 errors; plan unchanged         [verification]
                   - TestMoveTaskExecutionGuard: moving a done task errors;
                     moving a pending task before an in_progress task errors
                                                                  [verification]
                   - Out-of-order history: array [t-1 done, t-2 pending, t-3
                     done] — moving t-2 anywhere before t-3 is rejected; after
                     t-3 succeeds (execute's wave-then-id order produces this
                     shape, so the guard must handle mid-array done)     [risk]
                   - TestMoveTaskAdoptsAnchorWaveAndReflows: legal move of a
                     wave-1 task after a wave-2 anchor sets mover wave 2, bumps
                     its dependent wave 2 → 3; ValidatePlan returns nil
                                                            [verification+mvp]
                   - TestMoveTaskRejectsWaveIllegalAdoption: mover whose dep
                     sits in wave 2 moved before a wave-1 anchor errors (the
                     "otherwise reject" half of move_wave_semantics)
                                                                  [verification]
                   - A done task's wave field is asserted unchanged by the
                     reflow pass                                         [risk]
                   - TestMoveTaskKeepsIDsStable: after a legal move all ids,
                     TaskSeq, and NextTaskID() are identical to before
                                                        [verification, risk]
                   - Unknown anchor errors "anchor task not found: t-9" with
                     plan unchanged; anchor==self errors; moving a task to the
                     position it already holds returns nil, plan unchanged
                                                                         [risk]

  t-2  Order-aware NextRunnable tie-break                 [risk+verification]
       files:      internal/phase/phase.go, internal/phase/phase_test.go
       covers:     c-5
       depends_on: (none)
       description: Change NextRunnable's same-wave tie-break from lexicographic
                    id to array position in plan.toml. Wave still dominates;
                    deps-done filter unchanged. Without this, no move can ever
                    satisfy c-5 — today `task next` ignores plan order entirely.
       contract:   - TestNextRunnableFollowsDocumentOrder: array lists t-2
                     before t-1 (both wave 1, pending, no deps) → returns t-2;
                     fails on current code, pinning the change    [verification]
                   - Wave still dominates: a pending wave-1 task listed AFTER a
                     pending wave-2 task is still returned first
                                                        [risk+verification]
                   - Existing TestTaskNextRespectsWaveAndDeps in
                     internal/cmd/task_test.go stays green        [verification]

  t-3  Replace spec §2 free-recall with candidate slate   [risk+mvp+verification]
       files:      assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:     c-6
       depends_on: (none)
       description: Rewrite §2: open with a proposed candidate-criteria slate
                    derived from milestone scope (milestone.toml success criteria
                    + phase position) and CLI/context gap analysis, each item
                    gated accept/reword/drop one per turn; delete the freeform
                    "List 3-7 outcomes" ask. Keep the quality bar and the
                    trailing "anything missing?" catch-all [mvp]; keep §1
                    deferred-idea resurfacing feeding the slate [verification].
       contract:   - Test fails if normalized spec.md §2 still (or again)
                     contains the "list 3-7" free-recall phrase          [all]
                   - Test fails if §2 lacks the candidate-slate needles:
                     propose/candidate framing, milestone-scope + gap-analysis
                     derivation, per-item accept/reword/drop gate        [all]
                   - Existing §2 quality-bar pins (testable/measurable
                     pushback) still pass — the bar applies to slate items
                                                                         [risk]

  t-4  Ship /dross-techdebt skill, repoint status action  [risk+mvp+verification]
       files:      assets/commands/dross-techdebt.md, assets/prompts/techdebt.md,
                   internal/cmd/status.go, internal/cmd/status_test.go
       covers:     c-7
       depends_on: (none)
       description: Add the thin skill pair — dross-techdebt.md shim
                    (frontmatter + @~/.claude/dross/prompts/techdebt.md,
                    mirroring an existing shim's shape) and prompts/techdebt.md:
                    run `dross techdebt`, read the newest .dross/techdebt run
                    report, summarize top findings — no agent panel. Flip
                    actionCatalog's tech-debt entry from "dross techdebt" to
                    "/dross-techdebt". Makefile install already globs
                    dross-*.md, so linking is free [risk].
       contract:   - TestCommandsPromptsParity fails if either new file ships
                     without its twin                                    [all]
                   - Explicit pin asserting cmds["techdebt"] &&
                     prompts["techdebt"] so deleting BOTH also fails (the
                     parity loops alone miss that case)          [verification]
                   - status_test asserts every actionCatalog command starts
                     with "/" — regressing tech-debt to the bare CLI string
                     fails it                              [risk+verification]
                   - Rendered actions block for a stamped techdebt store
                     contains "/dross-techdebt · last run"               [risk]

Wave 2 (depends t-1, t-2)
  t-5  Wire dross task move subcommand                    [verification, grafts: risk+mvp]
       files:      internal/cmd/task.go, internal/cmd/task_test.go
       covers:     c-1, c-2, c-3, c-4, c-5
       depends_on: t-1, t-2
       description: Register taskMove() in Task(): `dross task move <phase-id>
                    <task-id> --before/--after <other-id>`; flag validation via
                    the existing resolveAnchor (exactly one required), mutation
                    via Plan.MoveTask, persistence via saveIfValid so a rejected
                    move never reaches Save and plan.toml stays byte-unchanged.
                    E2E-proves the move→next chain and post-move validity.
       contract:   - TestTaskMoveRepositions: seed t-1,t-2,t-3; `task move ph
                     t-3 --before t-1`; reloaded plan.toml orders t-3,t-1,t-2
                     (c-1, no hand-editing)                 [verification+mvp]
                   - Both --before and --after, or neither → resolveAnchor's
                     exact error strings; no file write in either case   [all]
                   - Illegal move (mover after its dependent) exits non-zero
                     and plan.toml bytes captured before/after are identical
                     (c-2's "left untouched" half)                       [all]
                   - E2E: both wave 1 pending; after `task move ph t-2
                     --before t-1`, `task next ph` prints t-2 — the c-5 proof
                     through the real CLI                  [risk+verification]
                   - TestTaskMoveKeepsID: after the move `task show ph t-3`
                     still resolves and the saved file contains id "t-3"
                     exactly once (c-4 end-to-end)               [verification]
                   - After a legal cross-wave move, `dross validate` over the
                     fixture phase reports no problems (c-3's literal clause,
                     proved at CLI level, not just ValidatePlan)         [risk]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-5 |
| c-2 | t-1 (engine guards), t-5 (file-untouched at CLI) |
| c-3 | t-1 (reflow + ValidatePlan), t-5 (`dross validate` e2e) |
| c-4 | t-1 (ids/TaskSeq stable), t-5 (end-to-end) |
| c-5 | t-2 (tie-break), t-5 (CLI proof) |
| c-6 | t-3 |
| c-7 | t-4 |

## Disagreements

### D1 — Engine granularity: one MoveTask task or three sequenced tasks
- **risk**: three same-file tasks (splice → guards → re-derivation) across 3 waves, each owning distinct failure modes — "a single task owning five failure modes is exactly how one of them ships untested."
- **verification**: one engine task (one file, one layer, six named tests), CLI separate — 2 waves.
- **mvp**: goes further the other way — engine + guards + reflow + NextRunnable in one 4-file mega-task.
- **Default taken**: verification's single engine task. Same file means risk's split buys no parallelism, only a longer serial chain; the untested-failure-mode concern is mitigated by grafting every one of risk's per-failure-mode contract lines into t-1's contract, so each mode is still individually pinned.
- **Why it matters**: this decides phase shape (2 vs 3 waves) and commit granularity; if t-1 turns out too big in execution, the fallback is risk's exact split with the same contracts.

### D2 — NextRunnable tie-break: standalone task or folded into the engine task
- **risk + verification**: standalone wave-1 task — different file (phase.go), different behavior, independently testable, and the only draft-agreed change that fails on current code.
- **mvp**: folded into t-1 as "one-function change, alone it is <10 min and would only add graph noise."
- **Default taken**: standalone (2 of 3 lenses). It keeps t-1's already-heavy contract focused on plan_edit.go, preserves wave-1 parallelism, and gives the c-5 pin its own atomic commit.
- **Why it matters**: this is the change that makes c-5 satisfiable at all; buried inside a mega-task its regression pin is easiest to lose.

### D3 — Dependent lands at wave <= the mover after a cross-wave move: reject or reflow
- **risk**: guard rejects the move when "any dependent at wave <= it" (dependents only ever get pulled *down* by its later re-derivation task, never pushed up).
- **verification + mvp**: reflow — bump pending dependents transitively to > the mover's new wave; rejecting "would outlaw legal moves c-2 doesn't forbid."
- **Default taken**: reflow (2 of 3), encoded in t-1's TestMoveTaskAdoptsAnchorWaveAndReflows. It matches c-3's literal "waves are re-derived" and the locked move_wave_semantics, whose "when dependencies allow" clause conditions rejection on *dependencies*, not dependents. Risk's frozen-history concern is honored by scoping reflow to pending tasks only, with an explicit done-wave-unchanged assertion.
- **Why it matters**: this is a real behavioral fork — under risk's rule a legal-per-spec move errors; under the default it succeeds and rewrites dependents' waves. Whichever ships defines the user-facing contract of `task move`.

### D4 — How c-3's "dross validate passes" is proven
- **risk**: dedicated re-derivation task whose contract runs `dross validate` over a fixture phase.
- **mvp + verification**: no dedicated task; c-3 is a contract on the engine task via phase.ValidatePlan (mvp: "a documented superset of `dross validate`'s plan checks").
- **Default taken**: no dedicated task (2 of 3), but risk's CLI-level `dross validate` assertion is grafted into t-5's contract and c-3 added to t-5's covers — so the criterion's literal clause is proved against the real command, not only its in-process superset.
- **Why it matters**: if ValidatePlan and `dross validate` ever drift, an in-process-only proof would let c-3 silently rot; the grafted e2e line closes that gap without adding a task.
