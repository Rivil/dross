# Plan draft — lens: VERIFICATION

Designed backward from test contracts. The load-bearing contract is c-4's integrity
guard: a rejected add/remove/edit must leave `plan.toml` **byte-for-byte unchanged**.
The whole architecture is chosen to make that provable by a single assertion:

> **load → mutate in memory → validate → Save-only-if-valid.**
> `(*Plan).Save` is the sole writer and is gated behind the guard, so an invalid
> result never touches disk. Byte-unchanged is then structural, not incidental —
> and the CLI test proves it with `assertPlanUnchanged` (a `mustRead` byte-compare,
> mirroring `TestPhaseMoveNoOp` in phase_lifecycle_test.go).

Pure logic (id/wave/validator/mutators) lives in `internal/phase/plan_edit.go` so each
contract is a dense, mutation-friendly unit test with no fixture ceremony; the cobra
verbs are thin wrappers whose tests assert the end-to-end + byte-unchanged properties.

```
Phase task-lifecycle-commands — 6 tasks across 2 waves

Wave 1  (pure plan-mutation core — internal/phase)
  t-1  Add NextTaskID + deriveWave helpers
       files:    internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:   c-1, c-2
       contract: If NextTaskID reuses a freed id or is off-by-one, TestNextTaskID
                 fails — over {t-1,t-3} it must return t-4 (max+1, never refilling
                 the removed t-2); over {} → t-1. If deriveWave ignores the deepest
                 --depends-on or drops the explicit override, TestDeriveWave fails:
                 deps [t-a@w1,t-b@w3] & no --wave → 4; --wave 2 given → 2; no deps → 1.

  t-2  Add ValidatePlan integrity guard
       files:    internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:   c-4
       contract: TestValidatePlan pins one case per branch — two tasks sharing id
                 t-2 → error naming the duplicate; depends_on=[t-99] absent from the
                 plan → error naming the unknown dep; covers=[c-99] absent from the
                 spec's criteria → error; a clean plan+spec → nil. Deleting any one
                 branch flips exactly one case from error to a false pass.

  t-3  Add AddTask / RemoveTask / EditTask mutators
       files:    internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:   c-2, c-3, c-5
       depends:  t-1
       contract: TestMutators asserts, all in memory: AddTask(anchor=t-1) into
                 [t-1,t-2,t-3] yields order [t-1,new,t-2,t-3] with every existing
                 id/field equal (placement never renumbers); no anchor → tail append.
                 RemoveTask(t-1,force=false) when t-2.depends_on=[t-1] returns a
                 refusal and mutates nothing; force=true drops t-1 AND strips it from
                 t-2.depends_on while t-2.wave is unchanged (no reflow) — omit the
                 strip and the "no dangling dep" assert fails. EditTask(t-2,{Title:X})
                 changes only Title; Covers/Wave/DependsOn/Files/Status compare equal
                 to the pre-image — clobber any unpassed field and its assert fails.

Wave 2  (cobra wiring — internal/cmd; needs the wave-1 core)
  t-4  Wire `dross task add` + saveIfValid + byte-unchanged harness
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1, c-2, c-4
       depends:  t-2, t-3
       contract: TestTaskAdd — `task add <phase> --title T --covers c-1` grows
                 plan.toml by one task whose id is max+1, and `dross validate` exits
                 0. `--after t-1` places the record after t-1 with existing task
                 bytes intact. `--covers c-99` (absent criterion) exits non-zero and
                 assertPlanUnchanged holds (plan.toml identical to the pre-read
                 bytes). Introduces saveIfValid(plan,spec,path) — if it Saves before
                 validating, the c-99 byte-unchanged case fails.

  t-5  Wire `dross task remove`
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-3, c-4
       depends:  t-3, t-4
       contract: TestTaskRemove — `task remove <phase> t-1` with t-2.depends_on=[t-1]
                 exits non-zero, the message names t-2, and assertPlanUnchanged holds.
                 `--force` deletes t-1, strips it from t-2.depends_on, and `dross
                 validate` passes. Skip the depended-on refusal and the byte-unchanged
                 assert fails because t-1 was deleted.

  t-6  Wire `dross task edit`
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-4, c-5
       depends:  t-2, t-3, t-4
       contract: TestTaskEdit — `task edit <phase> t-2 --title New` rewrites only the
                 title; reload shows Covers/Wave/DependsOn preserved. `task edit
                 <phase> t-2 --covers c-99` exits non-zero and assertPlanUnchanged
                 holds (shares saveIfValid). The command exposes no --status flag:
                 `--status done` errors as an unknown flag, proving `task status`
                 stays the sole status owner. Drop the guard and the c-99 case
                 becomes a silent write the byte-unchanged assert catches.
```

## Coverage
- c-1  → t-1, t-4
- c-2  → t-1, t-3, t-4
- c-3  → t-3, t-5
- c-4  → t-2, t-4, t-5, t-6
- c-5  → t-3, t-6

All of c-1..c-5 accounted for (5/5).

## Judgment calls
- Chose **validate-before-Save architecture** (guard gates the sole writer); rejected mutate-then-repair, because byte-unchanged is then a structural guarantee provable by one `mustRead` compare rather than a property to be re-checked per verb.
- Chose **pure core in a new `internal/phase/plan_edit.go`** (id/wave/validator/mutators) split from the cobra verbs; rejected inlining logic in `task.go`, because pure functions give dense per-branch mutation tests without CLI fixtures — the verification lens's highest-value surface.
- Chose to **model c-4's "duplicate id" via a hand-seeded two-`t-2` fixture** in TestValidatePlan; rejected asserting it only through auto-assign, because auto-assign (t-1) can't produce a dup, so the guard's dup branch would go untested — the fixture is the only way to reach it.
- Chose to **make the never-reused contract a MIDDLE-remove** ({t-1,t-3}→t-4); rejected a max-remove case, because the locked mechanism is literally `max+1`, so a middle-gap is the honest, non-vacuous test of "never reused."
- Chose **ValidatePlan as a superset of `dross validate`** (adds depends_on + dup-id checks it lacks) and reused the criteria-set covers-check; rejected calling `Validate()` as the guard, because validate never inspects depends_on or duplicate ids.
- Chose **wave-2 verbs to depend on t-4** for the shared `saveIfValid` + `assertPlanUnchanged` harness; rejected three parallel same-file verbs, because they co-edit `task.go`'s registration line — the deps are real output deps, not artificial locks.
- Chose to **assert "status not editable" by the absence of `--status`** (unknown-flag error); rejected a silent no-op, because an explicit flag rejection is the only observable proof the ownership boundary holds.
```
verification: 6 tasks across 2 waves, criteria covered 5/5
```
