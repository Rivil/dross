# MVP draft — task-lifecycle-commands

Lens: smallest task set that satisfies every criterion. The single lever is that
add/remove/edit are thin cobra wrappers over one shared, pure plan-mutation +
integrity core (wave 1). Placement (`--after`/`--before`) reuses the existing
`resolveAnchor` in package `cmd`; the post-write check reuses the existing
`dross validate`. Nothing else is built.

```
Phase task-lifecycle-commands — 4 tasks across 2 waves

Wave 1
  t-1  Add pure plan-mutation + integrity core
       files:    internal/phase/plan_mutate.go
                 internal/phase/plan_mutate_test.go
       covers:   c-4
       contract: Pure helpers on *Plan, all in plan_mutate_test.go table tests:
                 NextID() = max trailing-int of existing ids + 1 and is NEVER
                 reused (remove t-3 while t-4 exists → next add is t-5, not t-3);
                 DeriveWave(deps, explicit) = explicit if >0 else deepest-dep
                 wave +1 else 1; CheckIntegrity(criterionIDs) errors on a
                 duplicate id, a depends_on naming an absent task, and a covers
                 naming an absent criterion; RemoveTask(id, force) refuses when
                 depended-on and, with force, strips that id from every
                 dependent's depends_on leaving no dangling ref. Flip any rule
                 and its case fails.

Wave 2
  t-2  Wire `dross task add` subcommand
       files:    internal/cmd/task.go
                 internal/cmd/task_test.go
       covers:   c-1, c-2, c-4
       contract: TestTaskAdd in task_test.go: add appends a task with the fresh
                 NextID and a wave derived per the locked rule, existing ids
                 unrenumbered, `dross validate` passes after; `--after <id>`
                 lands it at the right list index (reusing resolveAnchor);
                 `--covers c-nope` / `--depends-on t-nope` is rejected AND a
                 byte-compare of plan.toml before/after is identical (unchanged
                 on reject). Break placement or the guard-before-save order and
                 the test fails.

  t-3  Wire `dross task remove` subcommand
       files:    internal/cmd/task.go
                 internal/cmd/task_test.go
       covers:   c-3
       contract: TestTaskRemove in task_test.go: removing a task another task
                 depends on errors with a clear message and writes nothing;
                 `--force` deletes it and strips the id from dependents'
                 depends_on so `dross validate` still passes; the freed id is
                 not reused by a subsequent add. Drop the depended-on refusal or
                 the force-strip and the test fails.

  t-4  Wire `dross task edit` subcommand
       files:    internal/cmd/task.go
                 internal/cmd/task_test.go
       covers:   c-4, c-5
       contract: TestTaskEdit in task_test.go: `edit <id> --wave N` changes only
                 wave, leaving title/covers/depends_on byte-identical
                 (partial update); status is not an editable flag; an edit whose
                 result would fail CheckIntegrity (bad --covers/--depends-on) is
                 rejected and plan.toml is unchanged. Regress partial-update or
                 the guard and the test fails.
```

## Coverage

- c-1 (add appends fresh unique id, validate passes) → t-2
- c-2 (append default / `--after` / `--before` placement, no renumber) → t-2
- c-3 (remove is dependency-safe, `--force` override) → t-3 (logic in t-1 RemoveTask)
- c-4 (integrity guard: bad covers/depends_on/dup id rejected, plan unchanged) → t-1 (CheckIntegrity) invoked by t-2, t-3, t-4
- c-5 (edit partial-update, status not editable, same guard) → t-4

All of c-1..c-5 accounted for.

## Judgment calls

- Chose ONE pure core task (t-1) holding NextID/DeriveWave/CheckIntegrity/RemoveTask — the locked id/wave/force/guard rules live in one testable, cobra-free place; rejected a helper-per-verb split as speculative structure.
- Chose three separate wrapper tasks (t-2/t-3/t-4) rather than one "wire all verbs" task — each maps to a distinct criterion and a distinct failure mode (append+place vs dependency-safe delete vs partial update), so each earns a crisp, non-compound test contract; merging them would force a sprawling contract.
- Reused existing `resolveAnchor` (package cmd) for `--after`/`--before` and existing `dross validate` for the c-1 post-write check — no new placement or validation command built.
- Rejected a standalone c-4 task: the guard is not a user-facing command, it is a pre-save gate; built once in t-1 and invoked by every mutating verb, so c-4 rides on t-2/t-3/t-4.
- Put all wrappers in wave 2 depending only on t-1 (they need its helpers, not each other); shared file task.go is a merge-coordination detail, not a logical dependency.
