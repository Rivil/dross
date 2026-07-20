# Synthesis — task-lifecycle-commands

Cold judge over three independently-drafted plans (risk / mvp / verification).
I authored none of them. Seams sanity-checked against source before merging.

## Scores

| Dimension | risk (7t) | mvp (4t) | verification (6t) |
| --- | --- | --- | --- |
| Criteria coverage | 5/5; every failure mode owned by exactly one task | 5/5; c-3 logic in t-1, tested in t-3 | 5/5; richest cross-map, each criterion touched in both waves |
| Test-contract specificity | Excellent — each contract names the mutation that flips one case | Good but t-1 is compound (NextID+DeriveWave+CheckIntegrity+RemoveTask in one contract) | Best — every contract bound to a named test + `assertPlanUnchanged` byte-compare mirroring the real `TestPhaseMoveNoOp` |
| Granularity | Fine (4 wave-1 primitives); t-4 atomic-Save is extra, no criterion backs it | Coarsest; one giant pure core + 3 thin wrappers | Balanced — 3 pure-core tasks (id/wave, validator, mutators) + 3 verbs |
| Wave correctness | Verbs→specific helpers, but treats `task.go` co-edit as non-blocking (parallel same-file verbs) | Same blind spot: "shared file is a merge-coordination detail, not a dependency" | Correct — wave-2 verbs depend on t-4 for the shared `saveIfValid`/`assertPlanUnchanged` harness; recognises the `task.go` registration co-edit as a real output dep |

**Skeleton = verification (6 tasks).** It wins test-contract specificity and wave
correctness outright: the load→mutate→validate→save-only-if-valid architecture makes
c-4's "byte-for-byte unchanged on reject" a *structural* guarantee provable by one
`mustRead` compare, and it is the only draft that correctly serialises the wave-2
verbs behind the shared `task.go` harness (t-4) instead of pretending three same-file
verbs are independent. Grafts pulled from the runners-up below.

## Merged plan

Phase task-lifecycle-commands — 6 tasks across 2 waves.

### Wave 1 — pure plan-mutation core (`internal/phase/plan_edit.go`)

- **t-1 — NextTaskID + deriveWave helpers**  `[verification + risk + mvp]`
  - files: `internal/phase/plan_edit.go`, `internal/phase/plan_edit_test.go`
  - covers: c-1, c-2
  - contract: `NextTaskID` over `{t-1,t-3}` → `t-4` (max+1, never refilling removed
    t-2); over `{}` → `t-1`. `deriveWave`: deps `[w1,w3]` & no `--wave` → 4;
    `--wave 2` given → 2; no deps → 1. Off-by-one, id-reuse, or ignored-deepest-dep
    each flips one table case. (Graft from risk's t-2: pin the never-reuse case as a
    **middle** remove, not a max remove — see Disagreement 5.)

- **t-2 — ValidatePlan integrity guard (superset of `dross validate`)**  `[verification + risk]`
  - files: `internal/phase/plan_edit.go`, `internal/phase/plan_edit_test.go`
  - covers: c-4
  - contract: one case per branch — two tasks sharing `id=t-2` → error naming the
    dup; `depends_on=[t-99]` absent → error naming the unknown dep;
    `covers=[c-99]` absent from spec → error; clean plan+spec → nil. Deleting any
    branch flips exactly one case error→false-pass. Note: duplicate-id must be
    reached via a hand-seeded two-`t-2` fixture (auto-assign can never produce a
    dup) — graft from verification's judgment call. Confirmed: `dross validate`
    checks covers only (validate.go:110), never depends_on or dup ids, so this is a
    genuine superset, not a duplicate.

- **t-3 — AddTask / RemoveTask / EditTask mutators**  `[verification + risk]`
  - files: `internal/phase/plan_edit.go`, `internal/phase/plan_edit_test.go`
  - covers: c-2, c-3, c-5
  - depends_on: t-1
  - contract: `AddTask(anchor=t-1)` into `[t-1,t-2,t-3]` → `[t-1,new,t-2,t-3]`,
    every existing id/field equal (placement never renumbers); no anchor → tail
    append. `RemoveTask(t-1,force=false)` when `t-2.depends_on=[t-1]` → refusal,
    mutates nothing; `force=true` drops t-1 AND strips it from `t-2.depends_on`,
    `t-2.wave` unchanged (no reflow) — omit the strip and "no dangling dep" fails.
    `EditTask(t-2,{Title:X})` changes only Title; Covers/Wave/DependsOn/Files/Status
    compare equal to the pre-image. (Folds risk's separate Dependents/StripDependency
    primitives (risk t-3) into RemoveTask — one owner, one test surface.)

### Wave 2 — cobra wiring (`internal/cmd/task.go`)

- **t-4 — Wire `dross task add` + `saveIfValid` + byte-unchanged harness**  `[verification + mvp]`
  - files: `internal/cmd/task.go`, `internal/cmd/task_test.go`
  - covers: c-1, c-2, c-4
  - depends_on: t-2, t-3
  - contract: `task add <phase> --title T --covers c-1` grows plan.toml by one task
    whose id is max+1, and `dross validate` exits 0. `--after t-1` places the record
    after t-1 with existing task bytes intact — **reuse the existing
    `resolveAnchor(after,before)` in package cmd** (graft from mvp; confirmed at
    phase_lifecycle.go:21, already enforces exactly-one-of `--after`/`--before`).
    `--covers c-99` exits non-zero and `assertPlanUnchanged` holds (a `mustRead`
    byte-compare, mirroring `TestPhaseMoveNoOp` at phase_lifecycle_test.go:213).
    Introduces `saveIfValid(plan,spec,path)`; if it Saves before validating, the
    c-99 byte-unchanged case fails.

- **t-5 — Wire `dross task remove`**  `[verification + risk + mvp]`
  - files: `internal/cmd/task.go`, `internal/cmd/task_test.go`
  - covers: c-3, c-4
  - depends_on: t-3, t-4
  - contract: `task remove <phase> t-1` with `t-2.depends_on=[t-1]` exits non-zero,
    message names t-2, `assertPlanUnchanged` holds. `--force` deletes t-1, strips it
    from `t-2.depends_on`, `dross validate` still passes; the freed id is not reused
    by a subsequent add (graft from mvp/risk). Removing an unknown id errors (graft
    from risk's t-6). Skip the refusal → byte-unchanged assert fails.
  - (depends_on t-4 is a real output dep: t-5 co-edits `task.go`'s `AddCommand`
    registration and reuses `saveIfValid`/`assertPlanUnchanged`, not a lock.)

- **t-6 — Wire `dross task edit` (no `--status` flag)**  `[verification + mvp]`
  - files: `internal/cmd/task.go`, `internal/cmd/task_test.go`
  - covers: c-4, c-5
  - depends_on: t-2, t-3, t-4
  - contract: `task edit <phase> t-2 --title New` rewrites only the title; reload
    shows Covers/Wave/DependsOn preserved. `--covers c-99` exits non-zero and
    `assertPlanUnchanged` holds (shares `saveIfValid`). Command exposes no `--status`
    flag: `--status done` errors as an unknown flag, proving `task status` stays the
    sole status owner (verification's observable proof of the ownership boundary,
    sharper than mvp's silent no-op).

Coverage: c-1 → t-1,t-4 · c-2 → t-1,t-3,t-4 · c-3 → t-3,t-5 · c-4 → t-2,t-4,t-5,t-6
· c-5 → t-3,t-6. All 5/5 accounted for.

## Disagreements

1. **Where the pure logic lives.** verification → new `internal/phase/plan_edit.go`
   (id/wave/validator/mutators, cobra-free). mvp → inline in `internal/cmd/task.go`.
   risk → many small primitives added into the existing `internal/phase/phase.go`.
   *Provisional default:* new `plan_edit.go` (verification). *Why it matters:* a
   cobra-free file gives dense per-branch unit tests with no CLI fixtures and keeps
   the already-large `phase.go` from absorbing four more concerns; inlining in
   task.go forfeits the pure-unit test surface that is c-4's cheapest proof.

2. **Task count / granularity.** risk 7 · verification 6 · mvp 4. *Provisional
   default:* 6 (verification). *Why it matters:* mvp's single pure-core task compounds
   NextID+DeriveWave+CheckIntegrity+RemoveTask into one contract (four behaviours,
   one test) — hard to attribute a regression; risk's 7 over-splits (separate
   Dependents/StripDependency, plus the atomic-Save task in #3). 6 gives one pure
   task per concern cluster without a sprawling contract.

3. **Standalone atomic/crash-safe `Plan.Save` task.** risk adds one (its t-4:
   temp-file + `os.Rename`, tested by pre-creating a directory at `plan.toml.tmp` to
   force the write to fail and asserting the live file is byte-identical).
   verification and mvp have none. **No criterion names crash-safety.** *Provisional
   default:* DROP as a standalone task — out of the criteria's scope. *Why it
   matters, and a real finding:* I confirmed `saveTOML` uses `os.Create`
   (phase.go:389), which truncates the live file before writing, so a crash mid-Save
   *can* corrupt plan.toml today — but that is orthogonal to c-4. The
   load→validate→save-only-if-valid architecture already delivers c-4's actual
   requirement (byte-unchanged on *rejection*, because an invalid plan never reaches
   Save). Recommend surfacing risk's atomic-Save as an **optional hardening task /
   follow-up quick task**, not smuggling crash-safety into this phase under c-4.

4. **Shared guard vs the existing `dross validate`.** verification → a new
   `ValidatePlan` **superset** (adds dup-id + unknown-depends_on that validate lacks)
   as the pre-write gate, while still calling `dross validate` for c-1's "validate
   passes afterward". mvp → reuses `dross validate` for the post-write c-1 check but
   builds a separate `CheckIntegrity`. risk → a `CheckIntegrity` primitive that
   reuses validate's covers rule as its semantic reference. *Provisional default:*
   new `ValidatePlan` superset for the pre-write guard, `dross validate` retained for
   the post-write c-1 assertion (verification). *Why it matters:* confirmed
   `validate.go` inspects covers only (line 110) and never depends_on or duplicate
   ids, so `dross validate` **cannot** serve as the c-4 guard as-is; the two must
   coexist, and keeping the covers rule in parity avoids c-1's "validate passes"
   disagreeing with the pre-write guard.

5. **[NOTE for the user — locked-decision wrinkle, not overridden] id_scheme edge
   case.** The locked decision (`max existing +1, never reused`) is self-consistent
   only for *middle* removals. Its two clauses collide when the **highest** id is
   removed: remove t-3 from `{t-1,t-2,t-3}`, and the next add computes `max+1 = t-3`
   again — a reuse of a freed id, exactly what the decision's rationale ("reusing a
   freed id would silently repoint stale references") warns against. All three drafts
   spotted this; all correctly implement the locked mechanism *literally* (risk and
   verification deliberately pin the never-reuse test as a **middle** remove so it is
   non-vacuous). No draft overrides the lock, and neither do I. *Surfaced for a
   spec-owner decision:* if "never reused" must hold for top-id removals too, the
   scheme needs a persisted high-water counter — a spec change, out of scope here.

synthesis: 6 tasks across 2 waves, 5 disagreements
