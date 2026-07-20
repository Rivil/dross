# Plan Review — task-lifecycle-commands

Reviewed: 2026-07-20
Plan: 7 tasks across 2 waves

## BLOCKING
(none)

All five criteria are covered: c-1 (t-1,t-4), c-2 (t-1,t-3,t-4), c-3 (t-3,t-5),
c-4 (t-2,t-4,t-5,t-6,t-7), c-5 (t-3,t-6). No task contradicts a locked decision —
in particular t-1 honors the amended `id_scheme` (persisted high-water `Plan.TaskSeq`,
backfill-from-max, "removed HIGHEST id never reused"), not plain max+1. No rules.toml
violation (r-01 make-install is an execute-time concern, not a plan defect).

## FLAG
- [check-6 / seam mismatch] t-4 says it places records by "reusing resolveAnchor
  (which already enforces exactly-one-of --after/--before)", but resolveAnchor
  (`internal/cmd/phase_lifecycle.go:21-32`) returns an error when BOTH flags are
  empty — and empty-both is exactly the default-append path that c-1 and c-2
  ("appended by default") require. t-4's own first contract line (`task add <phase>
  --title T --covers c-1`, no placement flag) would hit that error if resolveAnchor
  is called unconditionally.
  Suggestion: call resolveAnchor only when a placement flag is present and tail-append
  otherwise; make t-4's contract state the no-flag default path explicitly so the
  reused seam's precondition is respected.

- [check-5 / wave order] t-5 (wave 2, depends_on=[t-3,t-4]) and t-6 (wave 2,
  depends_on=[t-2,t-3,t-4]) both depend on t-4, which is itself wave 2. That is an
  intra-wave dependency: they cannot run alongside the rest of wave 2, and under the
  locked `wave_placement` rule (wave = deepest depends_on wave + 1) a task depending
  on a wave-2 task derives to wave 3. Execution stays correct because NextRunnable
  gates on depends_on regardless of wave, but the wave labels contradict the decision's
  stated intent ("wave label semantically tied to dependencies").
  Suggestion: move t-5 and t-6 to wave 3, or lift the shared saveIfValid/
  assertPlanUnchanged harness out of t-4 into its own wave-1 task so t-5/t-6 depend
  only on wave-1 outputs and legitimately stay in wave 2.

- [check-4 / granularity] t-1 and t-7 both edit `internal/phase/phase.go` in wave 1.
  The regions are disjoint (t-1 adds the `TaskSeq` field to the Plan struct ~line 262
  plus new NextTaskID/deriveWave helpers; t-7 rewrites saveTOML ~line 385), so a
  merge conflict is unlikely, but it is nonzero if wave 1 is run by parallel agents.
  Suggestion: if wave 1 executes in parallel, sequence these two (they commit to the
  same file); otherwise note the disjoint-region expectation in t-7.

## NOTE
- [check-6] Anchor-not-found is untested. t-3's AddTask contract only exercises a
  valid anchor and the no-anchor tail append; nothing (no criterion, no test_contract)
  says what `--after t-99` for a non-existent task id does. Not required by the spec,
  but it is the obvious adversarial input to the placement path — worth a decided
  behavior (error vs silent tail-append).
- [check-8 / strength] t-1's contract directly encodes the amended locked id_scheme:
  "after removing the HIGHEST task the next id is still t-4, never the freed t-3" is
  exactly the regression the high-water amendment exists to prevent.
- [check-8 / strength] Test contracts are mutation-style and name the surface that
  breaks ("Deleting any one branch flips exactly that case", "Reverting to os.Create
  truncate-in-place fails the survive-on-failed-write assertion") — specific, not
  "tests pass".
- [check-8 / strength] t-7 surfaces and fixes a real pre-existing bug (saveTOML's
  truncate-in-place os.Create at phase.go:389) that materially strengthens c-4's
  "leaves plan.toml unchanged" guarantee, and t-6 explicitly asserts no --status flag,
  encoding the edit_semantics locked decision.

## Summary
A well-structured, well-covered plan whose only real risks are the resolveAnchor
default-append precondition (t-4) and the wave-2 intra-wave dependencies of t-5/t-6 —
both fixable without rescoping.
