# Plan draft — RISK lens

Failure modes drive the graph. Every way an add/remove/edit can corrupt a
plan.toml — bad flags, a `--covers` to a non-existent criterion, a
`--depends-on` to an unknown task, a duplicate id, removing a depended-on task,
a partial write, editing a nonexistent task — is assigned to exactly one owner
task that also tests it. The four wave-1 tasks are pure package-level primitives
(each a directly unit-testable guard), so the risk is proven in isolation before
any cobra wiring can hide it. The three wave-2 verbs wire those guards and each
re-proves "a rejected mutation leaves plan.toml byte-for-byte unchanged."

```
Phase task-lifecycle-commands — 7 tasks across 2 waves

Wave 1
  t-1  Add plan integrity guard
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-4
       contract: CheckIntegrity(plan, criterionIDs) is the single detector for
                 all three invalid-plan shapes. If the duplicate-id branch is
                 dropped, a plan with two tasks both id="t-2" passes instead of
                 erroring; if the unknown-covers branch is dropped, covers=["c-9"]
                 against a spec holding only c-1 passes; if the unknown-depends
                 branch is dropped, depends_on=["t-99"] passes. Each has its own
                 failing table case naming which id/criterion/dep is dangling.

  t-2  Add id + wave derivation helpers
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-1
       contract: NextTaskID(plan) returns "t-<max+1>" and DeriveWave(plan, deps)
                 returns deepest-dep-wave+1 (else 1). If NextTaskID gap-fills
                 instead of taking max+1, removing t-2 from {t-1,t-2,t-3} then
                 allocating returns "t-2" (a reuse) and the never-reuse case
                 fails. If DeriveWave ignores deps, a task depending on a wave-2
                 task derives wave 1 instead of 3 and the derivation case fails.

  t-3  Add dependent lookup + strip helpers
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-3
       contract: Dependents(plan, id) lists every task whose depends_on contains
                 id; StripDependency(plan, id) removes id from every dependent.
                 If Dependents misses a dependent, remove of t-1 while t-3
                 depends_on t-1 reports no blocker and the safety case fails. If
                 StripDependency leaves the id, after a --force remove t-3 still
                 lists t-1 in depends_on and the strip case fails.

  t-4  Make Plan.Save atomic (temp+rename)
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-4
       contract: saveTOML writes to "<path>.tmp" then os.Rename onto the target,
                 so a failed write never truncates the live file. Test seeds a
                 valid plan.toml, pre-creates a DIRECTORY at plan.toml.tmp to
                 force the temp-create to fail, calls Save expecting an error,
                 and asserts plan.toml is byte-identical to the seed. A second
                 case asserts a normal Save leaves no plan.toml.tmp sibling. The
                 old direct-os.Create path fails the first assertion (it opens
                 the live file for truncation before failing).

Wave 2 (depends wave 1)
  t-5  Add `dross task add` subcommand
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1, c-2, c-4
       depends:  t-1, t-2
       contract: add loads spec+plan, allocates NextTaskID, derives the wave,
                 splices at --after/--before (append when neither), then runs
                 CheckIntegrity BEFORE Save. Cases: (a) --after and --before
                 together error, neither appends at the tail; (b) `add --after t-2`
                 places the new task at slice index after t-2 with every existing
                 id byte-unchanged (no renumber); (c) `--covers c-9` (absent from
                 spec) exits non-zero AND leaves plan.toml byte-identical — move
                 the guard after Save and case (c) finds the bad task written.
                 `dross validate` passes on the resulting plan.

  t-6  Add `dross task remove` subcommand
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-3, c-4
       depends:  t-1, t-3
       contract: remove deletes the task; if Dependents is non-empty it refuses
                 with an error naming the blocking task id and leaves plan.toml
                 byte-unchanged; with --force it StripDependency's the id from
                 dependents, re-runs CheckIntegrity, then Saves. Cases: removing
                 t-1 while t-3 depends_on t-1 errors without --force (file
                 unchanged); the same with --force succeeds and t-3's depends_on
                 no longer lists t-1; removing an unknown id errors. Drop the
                 refusal and the depended-on case leaves a dangling depends_on.

  t-7  Add `dross task edit` subcommand
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-5, c-4
       depends:  t-1
       contract: edit mutates only the flags passed (--title/--covers/
                 --depends-on/--wave), runs CheckIntegrity before Save. Cases:
                 editing only --title leaves wave/covers/depends_on/files/status
                 byte-identical (partial update); editing an unknown id errors;
                 `--covers c-9` exits non-zero and leaves plan.toml unchanged;
                 there is no --status flag (status stays owned by `task status`).
                 Drop the preserve-untouched behaviour and the partial-update
                 case sees wave reset to 0.
```

## Coverage
- c-1 (add appends fresh unique id, validate passes): t-2, t-5
- c-2 (append default / --after / --before, no renumber): t-5
- c-3 (remove is dependency-safe, --force override): t-3, t-6
- c-4 (invalid plan rejected, plan.toml unchanged): t-1, t-4, t-5, t-6, t-7
- c-5 (edit is partial in-place update, same guard): t-7

All of c-1..c-5 accounted for.

## Judgment calls
- Split the c-4 guard into a pure `CheckIntegrity` primitive (t-1) that the three
  verbs call: chose one detector owning duplicate-id/unknown-covers/unknown-
  depends; rejected re-implementing checks inside each verb — three copies drift
  and only one gets the edge cases tested.
- "max+1, never reused" vs the top-id-removal edge: chose the locked "max existing
  +1" mechanism literally, so removing the HIGHEST id (t-3 of {t-1,t-2,t-3}) does
  let the next add reuse t-3 — the one case where the two clauses of the locked
  decision collide; rejected persisting a high-water counter (not in the locked
  scheme). t-2's contract pins the guaranteed property: removing a MIDDLE id
  never reuses it. Flagged for the merge judge.
- Kept an atomic-Save task (t-4) though no criterion names crash-safety: chose to
  own the risk-lens "partial/corrupt write" failure mode with a deterministic
  temp+rename test; rejected folding it into the verbs — it is a package-level
  write primitive all three share, and the "unchanged on rejection" the verbs
  test (validate-before-write) does not cover a crash mid-Save. Droppable if the
  judge rules it out of scope.
- Reused the existing `covers→criterion` check that already lives in
  `dross validate` as the semantic reference for t-1, rather than inventing a
  divergent rule: chose parity with validate so c-1's "validate passes afterward"
  can't disagree with the pre-write guard; added the depends_on + duplicate-id
  checks validate currently lacks.
- One task per verb rather than splitting add's append (c-1) from its relative
  placement (c-2): chose to merge — both mutate the same add function and slice,
  so splitting would put two tasks in conflict on one file; the no-renumber risk
  is pinned inside t-5's contract.
- Verbs depend only on the wave-1 helpers they compile against (t-5→t-1,t-2;
  t-6→t-1,t-3; t-7→t-1): chose not to add a t-4 edge — Save's signature is
  unchanged, so the atomic upgrade is picked up without a code dependency, and a
  false edge would serialise the graph needlessly.
