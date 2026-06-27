# Risk-lens decomposition

Bias: **failure modes drive the graph.** The destructive surface here is a
`git reset --hard` over local main. Every task is shaped around one way that
reset (or its absence) can corrupt state: resetting the wrong tree, restoring a
partial `.dross/`, committing nothing/empty, healing without a human gate, or
silently treating an unmerged phase as done. Each risk is owned and tested by
exactly one task.

Phase ship-complete-recovery-hardening — 4 tasks across 3 waves

Wave 1
  t-1  Extract delta-guarded shared recovery routine
       files:    internal/cmd/ship_recover.go, internal/cmd/ship_recover_test.go
       covers:   c-2, c-3, c-6
       contract:
         - RISK empty/clean-main: TestRecoverNoOpWhenNotDiverged — run recovery
           when local main is already at origin/main; if the delta-guard is
           dropped (routine commits unconditionally as it does today) the test
           fails because HEAD gains a commit / errors instead of printing a
           "nothing to recover" no-op (rev-list origin/main..HEAD != 0).
         - RISK partial-restore: TestRecoverKeepsPriorPhases — fixture seeds
           two prior phases' `.dross/phases/*/spec.toml` plus the current one;
           if the restore is narrowed to the current phase's subdir, the prior
           phase's spec.toml is absent from HEAD's tree after recovery and the
           test fails (guards the cumulative-tree regression in c-2).
         - RISK reset-over-work: TestShipRecoverRefusesDirtyTree /
           TestShipRecoverRefusesWrongBranch (existing) must still pass through
           the extracted routine — if the dirty/wrong-branch guard is lost in
           the extraction, reset destroys uncommitted work and they fail.

  t-2  Detect stale completed-on-unmerged-branch in status
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-4
       contract:
         - RISK phantom-done: TestStatusFlagsStaleCompletedBranch — fixture puts
           HEAD on phase/<id>, branch-local state.json carries a `completed <id>`
           record, and origin/main's state.json lacks it; status must print a
           reconcile pointer and must NOT render the phase as done. Removing the
           detector drops the pointer (and the phase reads as complete) → fails.
         - RISK false-positive: TestStatusQuietWhenMergedOrOnMain — when
           origin/main already carries `completed <id>` (genuinely merged) or
           HEAD is on main, the warning must be absent; a detector that keys only
           off local state without checking origin would warn here and fail.

Wave 2 (depends t-1)
  t-3  Gate complete auto-recovery behind --recover
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-1, c-3
       depends:  t-1
       contract:
         - RISK silent-destruction: TestCompleteRefusesDivergedWithoutRecover —
           on a diverged main with no flag, complete must abort with a one-line
           pointer naming `complete --recover` and leave local main's SHA
           unchanged; if the no-flag path resets anyway the SHA moves → fails.
         - RISK heal-works: TestCompleteRecoverHealsDivergedMain — on the same
           diverged main, `complete --recover` ends with main at origin/main +
           the cumulative `.dross/` restored and a clean tree, zero manual git;
           dropping the delegation to t-1's routine leaves main diverged → fails.
         - RISK reset-over-dirty-via-complete: TestCompleteRecoverDirtyAborts —
           `--recover` on a dirty tree never reaches the reset (complete's
           up-front dirty guard fires); if that guard is bypassed for the
           recover path the dirty file is destroyed and the test fails.

Wave 3 (depends t-3, t-2)
  t-4  Document recovery cookbook; add resume pointer
       files:    assets/prompts/ship.md, assets/prompts/resume.md,
                 internal/cmd/prompt_docs_test.go
       covers:   c-4, c-5
       depends:  t-3, t-2
       contract:
         - RISK manual-surgery-doc: TestShipDocRecoveryRecipe — asserts ship.md
           contains a recovery section naming all three failure states (ff-abort
           / diverged main / dirty post-push tree), each pointing at an exact
           dross command (`dross phase complete --recover`, `dross ship recover`),
           AND contains no manual `.dross/` surgery in that section (no
           `git reset` / `git checkout ... -- .dross/` presented as a user step).
           Re-introducing a hand-surgery instruction or dropping a state fails it.
         - RISK orphaned-resume: TestResumeDocStalePointer — asserts resume.md
           tells the reader to heed status's stale-completed warning with a
           reconcile pointer; without it, re-entry via resume misses the c-4
           signal and the test fails.

## Coverage
- c-1 (complete --recover heals; no-flag stops non-destructively) -> t-3
- c-2 (cumulative `.dross/` survives, no partial restore) -> t-1
- c-3 (dirty / wrong-branch abort, changes nothing) -> t-1 (guard owner),
  t-3 (verifies the complete entry point honors it)
- c-4 (stale completed-on-unmerged-branch surfaced, never auto-mutated) ->
  t-2 (`dross status` detector), t-4 (`dross resume` doc pointer)
- c-5 (ship.md recovery recipe, no manual `.dross/` surgery, doc-guard test) -> t-4
- c-6 (idempotent no-op on a clean / non-diverged main) -> t-1

## Judgment calls
- Folded the c-6 delta-guard INTO t-1's routine extraction rather than a
  separate wave-2 task: a follow-up task editing the same function t-1 just
  wrote serializes churn on one routine for no isolation gain. Kept as two
  distinct test contracts on one task instead.
- Made c-2's cumulative-tree check a named regression test inside t-1, not its
  own task: it tests a property of the routine directly and would be a sub-10-min
  single-test task on its own — but it gets an explicit, separate contract so the
  partial-restore risk stays individually owned.
- Assigned c-3's guard ownership to t-1 (where the dirty/wrong-branch refusal
  lives) and gave t-3 only a verification contract that complete's path can't
  reach a destructive reset — rejected duplicating the guard logic in phase.go,
  which would create two drifting copies of the exact safety check this phase
  exists to consolidate.
- Put the c-4 detector in the CLI (t-2, status.go) and let `dross resume` inherit
  it via the `dross status` call its prompt already makes, adding only a doc
  pointer (t-4) — rejected a second Go detection path for resume (resume is a
  prompt/skill, not a cobra command; there is no resume.go to host logic).
- Sequenced t-4 (docs) behind t-3 so the documented flag name matches what
  actually shipped — rejected drafting the cookbook in wave 1, which risks
  documenting a `--recover` spelling that the implementation then changes.
- Kept the no-flag refusal as an assertion that local main's SHA is byte-for-byte
  unchanged (not just "an error returned") — a refusal that errors AFTER a partial
  reset would still pass a weaker contract while having already corrupted main.
