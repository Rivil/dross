# Plan Review — complete-base-truth

Reviewed: 2026-07-31
Plan: 9 tasks across 4 waves

## BLOCKING
(none)

Coverage is complete (c-1→t-2, c-2→t-1/t-4/t-5, c-3→t-5, c-4→t-6/t-8, c-5→t-9, c-6→t-6,
c-7→t-3/t-7), no task contradicts a locked decision, and nothing implies a rules.toml
violation (r-01 is the only project rule and is an execute-time step).

## FLAG

- [wave-order] Two waves put tasks that edit the *same* functions in parallel. Wave 2:
  t-4 and t-5 both list `internal/cmd/phase.go` + `internal/cmd/phase_test.go`, and t-5
  rewrites `phaseComplete`'s base resolution while t-4 rewrites `forkPhaseBranch` in the
  same file. Wave 3: t-6 and t-7 both edit `phase.go` + `phase_test.go`, and both rewrite
  parts of the same `phaseComplete` RunE (the `--recover` arm and the safety-net push
  block, ~20 lines apart at phase.go:364 and phase.go:406-428). The dependency graph is
  correct; the file graph is not.
  Suggestion: either declare the waves sequential-within-wave, or add `depends_on = ["t-4"]`
  to t-5 and `["t-6"]` to t-7 so the ordering is explicit rather than incidental.

- [antipattern: design] `quick_base` lives in `state.json` — the cumulative file — and t-7
  says it "is not cleared on success". That is exactly the drag-forward failure the locked
  `base_storage` decision rejected for the phase base ("a phase-scoped record cannot be
  dragged forward by cumulative history"). Concretely: a standalone quick on `main` writes
  `quick_base=main`; that state.json commit squash-merges onto the base and rides forward
  into every later phase's tree, so from then on *every* ship and complete runs an extra
  `pushBaseIfAheadDrossOnly(main)` — and per t-7's own contract bullet 4, an unrelated
  non-.dross commit on that stale branch becomes a permanent refusal with no expiry. No
  task in the plan defines quick_base's lifetime.
  Suggestion: pin the lifetime somewhere — clear it at the start of the next quick, or make
  t-7's trigger require that quick_base still names a branch with unpushed .dross-only
  commits *and* is not simply inherited history. At minimum add a contract bullet asserting
  what happens to a quick_base left over from a merged quick.

- [test-contract: c-1] t-2's refusal assertions cover local refs only ("HEAD on phase/auth",
  "rev-parse main unchanged", "branch --list phase/auth non-empty"). But with the checkout
  relocated to just before `merge --ff-only`, `pushBaseIfAheadDrossOnly` still runs *before*
  `mergeGate` (phase.go:364 vs :393), and on its success path it does
  `git push origin <base>` (basebranch.go:103). So a mergeGate refusal has already moved
  `origin/<base>`. c-1's "no ref moved" is therefore true of local refs only, and nothing in
  the task states that reading.
  Suggestion: say so in t-2's description and add a contract bullet that pins the intended
  remote behaviour ("the mergeGate-refusal test asserts origin/main is unchanged when the
  base had nothing to push"), so a later reader doesn't treat the push as a c-1 violation.

- [t-6 unaddressed interaction] t-6 changes `runDrossRecovery`'s tree source to the phase
  tip, but says nothing about phase.go:412-422, whose comment explicitly justifies the
  opposite: "reload state from the (now checked-out) base working tree so the recovery
  commit carries the base's .dross/ state, not the phase branch's stale copy". After t-6 the
  ordering is: reset to origin → `checkout <phase-tip> -- .dross/` (state.json now the phase
  branch's) → `s.Save(rs)` at ship_recover.go:196 overwrites state.json with the base's copy.
  Tree from the phase tip, state.json from the base. That is probably what you want, but it
  is unstated, the comment now reads as contradicting the code, and no contract bullet pins
  which state.json wins.
  Suggestion: state the intended split in t-6's description, rewrite the stale comment as
  part of the task, and add a bullet asserting the recovered state.json is the base's.

- [t-8 scope] t-8 modifies `dross ship recover`, a command c-4 does not name — c-4 is about
  `--recover`, which t-6 already covers end to end. It is also only half of c-4 for that
  command: t-8 fixes base *resolution*, but `shipRecover` still sources the restored tree
  from HEAD (ship_recover.go:143-149), and its own guard (ship_recover.go:98) forces HEAD to
  be the base — so under a milestone it restores .dross/ from the milestone branch, which is
  the pattern c-4's second clause forbids.
  Suggestion: either drop t-8 to a deferred item (its `--pre-merge-sha` escape hatch already
  exists), or say explicitly in the task that only the base half is in scope and why the
  tree-source half is left alone for `ship recover`.

- [granularity] Two tasks hit the 5-file threshold. t-3 spans `internal/state`,
  `internal/cmd` (two files), an `assets/prompts` asset and a new test — three layers for
  one field. t-4 is two independent writes in two different commands (fork-time in
  `phase.go`/`phase_lifecycle.go`, ship-time in `ship.go`) that share only the `SetBase`
  helper; they have disjoint test contracts (bullets 1-4 vs 5-7) and would make two clean
  atomic commits.
  Suggestion: split t-4 at the fork/ship seam. t-3 is defensible as one field's full
  vertical, but expect it to be the longest commit in wave 1.

- [t-3 completeness] Adding a settable state field leaves `stateSet`'s help text wrong:
  `internal/cmd/state.go:93` reads `Short: "Set version | current_milestone | current_phase |
  current_phase_status"`. t-3's files include state.go so the edit is reachable, but neither
  the description nor any contract bullet mentions it.
  Suggestion: name the Short string in t-3's description, or add a bullet asserting
  `state set --help` lists quick_base.

- [docs] No task touches README.md or docs/dross.1, yet the phase adds a user-visible flag
  (`dross phase complete --base`) and changes what `--recover` does under a milestone. t-6
  fixes the cobra `Long` text (c-6), which is the criterion, but the repo's README-sync
  convention is not represented anywhere in the plan.
  Suggestion: confirm this is deliberate (README syncs at ship) or attach the doc edit to
  t-6, which already owns the help-text rewrite.

## NOTE

- [antipattern] Every existing symbol and test name the contracts lean on was verified to
  exist: `stubPRMerged` (phase_test.go:23), `completeFixture` (:323),
  `TestPhaseCompleteHappyPath` (:384), `TestPhaseCreateRootsOnMilestoneBranch` (:1151),
  `shipMockFlow` (ship_test.go:505), `activateMilestone` (:915),
  `TestShipPushesPRRecordToPhaseBranch` (:370), and `runDrossRecovery`'s existing
  `(preMergeSHA, baseBranch)` signature. The two new files (`quick_prompt_test.go`,
  `phase_base_truth_test.go`) are declared as new. No phantom references.

- [antipattern] t-4's rollback claim is accurate: `phase create` does `MkdirAll(dir)` at
  phase.go:136 and rolls back with `os.Remove(dir)` at :149, which fails on a non-empty dir —
  so writing the base before `checkout -b` really would leak the phase slug. Same shape at
  phase_lifecycle.go:188-190.

- [granularity] t-4 implies a `forkPhaseBranch` signature change — it is
  `(repoDir, root, branchName)` today with no phase id — but only says "phaseInsert's call
  site passes its id". Worth stating outright so the executor doesn't derive the id by
  string-trimming `phase/` off the branch name.

- [coverage] A `--no-branch` (or non-git) phase never calls `forkPhaseBranch`, so it gets no
  base and permanently requires `--base` at completion. That is consistent with the locked
  `legacy_escape` decision, and t-5 does ask for wording that reads sensibly there — flagged
  only so the executor treats it as permanent behaviour, not a migration gap.

- [t-6] `git checkout <sha> -- .dross/` is additive: paths present in the working tree but
  absent from `<sha>` are not deleted. So sourcing the restore from the phase tip cannot drop
  .dross/ artefacts that landed on the base after the fork. This makes t-6 safer than it
  reads; no action.

- [forbidden-actions] `/Users/rivil/.claude/dross/rules.toml` does not exist — the only rule
  in force is project r-01 (`make install` after prompt/Go edits). t-3 edits
  `assets/prompts/quick.md`, but its test reads the asset from source, so no task *relies* on
  the installed copy. Not a plan defect; execute-time hygiene.

- [strength] The test contracts are written as falsification statements naming concrete
  surfaces ("if the checkout precedes the fetch, rewriting origin to a nonexistent path
  leaves HEAD off phase/auth"), and they include negative cases most plans omit: the
  omitempty legacy round-trip (t-1), the `--no-push`/`--print-body` early return (t-4), the
  missing-ref silent skip and the no-double-push guard (t-7). Nothing here is a "tests pass"
  contract.

- [strength] Putting t-2 (the checkout reorder) in wave 1, ahead of t-5, is the right call:
  both rewrite the same RunE, and doing the mechanical move first keeps the semantic change
  reviewable on its own.

- [strength] t-9 asserting an explicit two-outcome switch (clean success or clean refusal,
  with a failing default arm) is a stronger regression shape than asserting one expected
  outcome — it catches the half-completed states that caused the original incident.

## Summary
No blockers — coverage, locked decisions and rules all hold, and the contracts are unusually
concrete; the real risks are the same-file parallelism inside waves 2 and 3, and `quick_base`
living in cumulative state with no defined lifetime, which reintroduces the drag-forward
problem the phase base was deliberately kept out of.
