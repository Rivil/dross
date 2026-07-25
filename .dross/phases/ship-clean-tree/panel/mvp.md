Phase ship-clean-tree — 3 tasks across 1 wave

All three tasks are independent behaviors with no runtime data dependency on
each other, so they are all wave 1 (they co-edit phase.go / ship.go /
ship_recover.go, but file overlap is an execution-ordering concern, not a wave
dependency — see Judgment calls).

Wave 1
  t-1  Auto-commit .dross-only dirt in all three gates
       files:    internal/cmd/phase.go, internal/cmd/ship.go,
                 internal/cmd/phase_test.go, internal/cmd/ship_test.go
       covers:   c-1
       desc:     Add one shared helper next to dirtyTreeError: partition
                 `git status --porcelain` into .dross/ vs non-.dross paths.
                 All-.dross → `git add .dross` + a `chore(dross):` commit and
                 proceed; any non-.dross path → the existing dirtyTreeError.
                 Wire it into phaseComplete (replaces the :299-305 gate),
                 forkPhaseBranch (replaces the :478-484 gate), and a NEW
                 pre-push gate in ship RunE (ship has no dirty gate today).
       contract: - phase complete on a tree dirty with ONLY .dross/handoff.md
                   creates a `chore(dross):` commit and proceeds; the same tree
                   with README.md also dirty still returns dirtyTreeError.
                 - ship with an uncommitted .dross/ file commits it as a chore
                   then pushes; ship with an uncommitted source file refuses
                   via dirtyTreeError BEFORE any push happens.
                 - phase create/start on a .dross-only dirty tree auto-commits
                   instead of hitting dirtyTreeError; a non-.dross dirty file
                   still aborts forkPhaseBranch.

  t-2  Safety-net push of base-ahead .dross chores
       files:    internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/ship_recover.go, internal/cmd/ship_recover_test.go
       covers:   c-2
       desc:     Add a `pushBaseIfAheadDrossOnly` helper: when `rev-list
                 origin/<base>..<base>` is non-empty and every ahead commit is
                 .dross-only, `git push origin <base>`. Call it at the end of
                 ship RunE, at the end of phase complete RunE (after teardown),
                 and at the end of runDrossRecovery (covers `dross ship recover`
                 + `phase complete --recover`). Push failure is a returned hard
                 error, never warn-and-continue. pause.go is left unchanged
                 (writers stay local-only).
       contract: - after phase complete when local main is one .dross-only
                   chore ahead of origin/main, origin/main advances to match
                   (rev-list origin/main..main is empty post-run).
                 - after runDrossRecovery restores .dross/ as a commit, that
                   commit is pushed so local base == origin/base post-recover.
                 - when the safety-net `git push` is rejected, ship/complete
                   return a non-nil error and print no success line.
                 - after `dross pause`, local main may be ahead of origin
                   (pause performs no push).

  t-3  Make --recover work in stale / diverged / milestone completion
       files:    internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/phase_test.go, internal/cmd/ship_recover_test.go
       covers:   c-3, c-4, c-5
       desc:     (c-3) In phaseComplete, resolve recordedPR for mergeGate from
                 `git show origin/<base>:.dross/phases/<id>/changes.json` (post
                 fetch) when the working-tree changes.json lacks it, instead of
                 only the stale local read at :315.
                 (c-4) Restructure the --recover path so merge verification
                 (mergeGate with the c-3-resolved PR) runs, THEN the destructive
                 reset/heal (runDrossRecovery), THEN completion — so --recover
                 heals the very stale/diverged state its error recommends it
                 for, without ever resetting an unmerged branch.
                 (c-5) Parameterize runDrossRecovery by branch (currently
                 hardcoded mainBranch) and replace the milestone abort at
                 :352-361 with a recover path that resets/restores against
                 milestone/<version>.
       contract: - (c-3) with a stale post-squash-merge tree whose working-copy
                   changes.json has no PR, mergeGate still resolves PR #N from
                   origin/<base>'s changes.json and confirms merge — completion
                   no longer falls through to the ancestry refusal in that state.
                 - (c-4) `phase complete --recover` in the diverged/stale state
                   heals and completes, while the same state without --recover
                   still refuses; an UNMERGED PR aborts --recover before any
                   `git reset --hard` runs (verify-before-reset).
                 - (c-5) `phase complete --recover` (and `dross ship recover`)
                   on a milestone/<version> reconcile branch resets/restores
                   against milestone/<version> and completes, instead of the
                   "does not yet support milestone branches" abort.

## Coverage
- c-1 → t-1
- c-2 → t-2
- c-3 → t-3
- c-4 → t-3
- c-5 → t-3

All 5 criteria covered.

## Judgment calls
- Merged c-3 + c-4 + c-5 into one task (t-3). Rejected three separate tasks:
  they are one cohesive change to the completion/recover machinery in the same
  two files (phase.go + ship_recover.go, 2 files / 1 layer — under the split
  threshold), and c-4's reorder is untestable without c-3's PR resolution
  (the reorder exists so the resolved verification can gate the reset). c-5 is
  the milestone variant of the same reset. Splitting would add structure the
  criteria don't require.
- Kept t-1 (local auto-commit gate) and t-2 (network safety-net push) separate
  despite both editing ship.go + phase.go. Rejected merging them: distinct
  mechanisms at distinct execution points (pre-push local commit vs post-work
  network push), distinct failure surfaces (non-.dross refusal vs push-failure
  refusal), and dross's atomic-commit-per-task convention wants them as
  separate commits. Merging would bundle two criteria into one non-atomic diff.
- All three tasks are wave 1. Rejected sequencing t-2/t-3 into later waves:
  neither needs the other's runtime *output*. c-2's "after recover" acceptance
  is satisfied by ordering within the function (t-3's heal is inside the
  --recover block; t-2's complete-end push runs after teardown, naturally
  later), not by a build-time dependency. The file overlap (phase.go in all
  three, ship_recover.go in t-2+t-3) is an executor merge concern, and forcing
  serial waves purely to avoid co-editing would be the speculative structure
  this lens rejects.
- Put the recovery-routine push in t-2 (not t-3), so all of c-2 lives in one
  task and t-3 stays scoped to the recover *logic*. t-2 adds a push call at the
  end of runDrossRecovery; t-3 changes runDrossRecovery's branch parameter —
  different edits, same function, no dependency.
