# Plan draft — VERIFICATION lens

Design backward from the test contracts. For each criterion the ideal contract is
written first; each task is the smallest change that makes that contract satisfiable.
Every task ships its own failing guard — no task without a named breaking test.

## Test contracts (written first)

- **c-1** — *diverged-complete fixture* (local main carries an extra commit origin's
  squash rebuilt differently, so `merge --ff-only origin/main` aborts; origin/main
  carries the `completed <id>` record): `dross phase complete` **without** `--recover`
  exits non-zero with a message naming `--recover` / `dross ship recover`, and
  `git rev-parse main` is byte-identical before/after (non-destructive). The same
  fixture with `dross phase complete --recover` exits 0, HEAD carries the full
  `.dross/` tree, local `phase/<id>` is deleted, working tree clean.
- **c-2** — *two-phase fixture* (pre-merge HEAD holds `.dross/phases/01-x/**` AND
  `.dross/phases/02-y/**`; origin/main has neither): after recovery, `git ls-tree -r HEAD`
  contains BOTH phases' artefacts — restoring only the current phase fails the 01-x
  assertion.
- **c-3** — *diverged main + dirty tree*: `dross phase complete --recover` aborts with a
  dirty-tree message naming the file and leaves `git rev-parse main` unchanged; the
  reset never runs over the uncommitted file. (Ship-recover's own dirty/wrong-branch
  refusals stay green under t-1's refactor.)
- **c-4** — *on phase/x with branch-local state reading completed*: `dross status`
  prints a `stale:` line with a reconcile pointer and does NOT render the phase as
  done; on main (or with no `completed x` record) the line is absent. `resume.md`
  documents the same drift case and its reconcile command.
- **c-5** — `ship.md` recovery section names all three failure states (ff-abort,
  diverged main, dirty post-push tree) and the exact commands (`complete --recover`,
  `ship recover`), with zero manual `.dross/` surgery — enforced by a prompt guard test.
- **c-6** — *in-sync fixture* (main already at origin/main, `.dross/` intact): recovery
  exits 0, prints a no-op message, and `git rev-list --count origin/main..HEAD == 0`
  — no phantom empty commit, no error.

---

Phase ship-complete-recovery-hardening — 6 tasks across 2 waves

Wave 1
  t-1  Extract shared recovery routine, delta-gate the commit
       files:    internal/cmd/ship_recover.go, internal/cmd/ship_recover_test.go
       covers:   c-6
       contract: Lift shipRecover's RunE body into an internal
                 runDrossRecovery(repoDir, root, p, s, phaseID, preMergeSHA) that
                 ship recover now delegates to; gate the commit on a real staged
                 `.dross/` delta vs origin (skip state.Touch+commit when none).
                 New TestShipRecoverIdempotentNoOp: in-sync fixture →
                 `git rev-list --count origin/main..HEAD` is 0 and a "nothing to
                 restore / already in sync" message prints; drop the delta gate and
                 a phantom empty commit makes the count 1. Existing
                 TestShipRecoverHappyPath (real delta) still commits exactly 1, and
                 TestShipRecoverRefusesDirtyTree / RefusesWrongBranch stay green —
                 proving the extraction preserved the c-3 ship-path guards.

  t-2  Detect stale completed-on-phase-branch in dross status
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-4
       contract: Add staleCompletedState(root, repoDir) → (phaseID, bool): true when
                 HEAD is `phase/<id>` AND branch-local state.json records
                 `completed <id>` (the phase says done but you're still on its
                 unmerged branch). Status prints
                 `stale:  on phase/<id> but state reads completed — reconcile: …`.
                 New TestStatusSurfacesStaleCompletedState (git fixture, HEAD on
                 phase/x, history has "completed x") asserts the `stale:` line +
                 reconcile pointer appear; control TestStatusNoStaleOnMain (on main,
                 or no completed record) asserts the line is absent. Delete the
                 detection → the stale fixture renders as a normal/done phase and the
                 assertion fails. Read-only: no state write in the path.

  t-3  Document stale-state reconcile in resume.md
       files:    assets/prompts/resume.md, internal/cmd/resume_prompt_test.go
       covers:   c-4
       contract: Add a §1 drift case: "on phase/<id> but state reads completed →
                 stale, not done" with its reconcile command, and a no-auto-mutate
                 caveat. New TestResumePromptStaleStateSection (mirrors
                 secure_prompt_test's normalise+needle pattern via repoRootFromTest)
                 asserts resume.md contains the stale-completed phrase, the reconcile
                 pointer, and "never auto-mutate"; removing the section fails exactly
                 those needles.

  t-4  Add recovery cookbook to ship.md + guard test
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-5
       contract: New "## Recovery" section covering ff-abort, diverged main, and
                 dirty post-push tree, each naming the exact command
                 (`dross phase complete --recover` and/or `dross ship recover`), with
                 no manual `.dross/` surgery. New TestShipPromptRecoverySection
                 asserts all three failure-state phrases + both commands are present
                 AND that no `checkout … -- .dross/` manual-surgery instruction
                 appears; dropping any state's recipe or reintroducing manual `.dross/`
                 surgery fails the matching sub-assertion.

Wave 2
  t-5  Wire --recover into phase complete (heal-or-pointer)
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-1, c-3
       depends:  t-1
       contract: Add `--recover` bool to phaseComplete; at the `merge --ff-only`
                 hook, on a non-ff (diverged) result delegate to t-1's
                 runDrossRecovery when `--recover` is set, else return a one-line
                 pointer error naming `--recover` / `dross ship recover` (touching
                 nothing). New divergedCompleteFixture (local main one commit ahead
                 of origin's rebuilt squash). TestPhaseCompleteDivergedNoFlagStops:
                 no flag → error mentions `--recover` and `git rev-parse main`
                 unchanged. TestPhaseCompleteRecoverHeals: `--recover` → exit 0, full
                 `.dross/` tree on HEAD, phase/<id> deleted, tree clean.
                 TestPhaseCompleteRecoverRefusesDirty (c-3): diverged + dirty →
                 aborts with the file named, `git rev-parse main` byte-identical —
                 drop complete's pre-recovery dirty guard and the file is destroyed
                 by reset --hard.

  t-6  Guard cumulative .dross/ restore (no partial)
       files:    internal/cmd/ship_recover_test.go
       covers:   c-2
       depends:  t-1
       contract: New TestRecoverRestoresAllPhases: fixture whose pre-merge HEAD holds
                 `.dross/phases/01-x/spec.toml` AND `.dross/phases/02-y/spec.toml`
                 while origin/main has neither; after `dross ship recover`,
                 `git ls-tree -r --name-only HEAD` contains BOTH paths. A
                 current-phase-only restore (the partial-restore regression) leaves
                 02-y out and fails the second assertion.

## Coverage

- c-1 → t-5
- c-2 → t-6
- c-3 → t-5  (complete --recover dirty abort; t-1 keeps the ship-recover dirty/branch guards green)
- c-4 → t-2 (status surface), t-3 (resume.md surface)
- c-5 → t-4
- c-6 → t-1

All of c-1..c-6 accounted for.

## Judgment calls

- Chose to extract `runDrossRecovery` as the wave-1 spine (t-1) and fold the c-6
  delta-gate into it, so both `ship recover` and `complete --recover` inherit
  idempotency from one place — rejected duplicating the heal logic into complete
  (locked decision: shared routine, no drift).
- Made the c-6 commit gate fire on the `.dross/` staged delta *before* `state.Touch`,
  rejecting a touch-then-check ordering — touching state.json first always creates a
  delta, which would make the c-6 no-op impossible to reach.
- Put the diverged-vs-heal decision at the `merge --ff-only` failure hook (t-5)
  rather than pre-detecting divergence with extra git plumbing — the ff abort *is*
  the divergence signal, so the smallest change is to branch on its error.
- Folded c-3's complete-path dirty guard into t-5 instead of a standalone one-file
  test task (would be too-small), and leaned on t-1's regression check to keep
  c-3's existing ship-recover path green — rejected a separate c-3 task.
- Surfaced c-4 stale-state via one Go detector in `status` (t-2) and a prompt edit in
  `resume.md` (t-3, transitively fed by status output) — rejected adding a `dross
  resume` Go command, which does not exist (resume is a prompt that already runs
  `dross status`). Kept both warn-only, no auto-mutate (locked decision).
- Split c-4 into two tasks (status Go vs resume prompt) so each surface carries its
  own guard test — rejected one combined task that would blur the two contracts the
  judge needs to see fail independently.
