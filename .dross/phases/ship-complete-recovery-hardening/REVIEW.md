# Plan Review — ship-complete-recovery-hardening

Reviewed: 2026-06-27
Plan: 5 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [coverage / test-contract] c-3 has two limbs — "dirty working tree OR the wrong
  branch it aborts cleanly". t-5 is the only task crediting c-3, but its contract
  tests only the dirty limb (TestPhaseCompleteRecoverRefusesDirty). For
  `complete --recover` the wrong-branch case is auto-resolved by the checkout-main
  step (phase.go:286-290), not aborted, so it never exercises a wrong-branch guard.
  The only wrong-branch *abort* test, TestShipRecoverRefusesWrongBranch, lives in
  t-1 — which is credited to c-6/c-2, not c-3. So no c-3-credited task exercises the
  wrong-branch limb.
  Suggestion: add a wrong-branch assertion to t-5 (or an explicit "complete resolves
  wrong-branch via checkout, abort lives in ship recover" note), or credit t-1's
  RefusesWrongBranch toward c-3 so the limb is visibly owned.

## NOTE
- [test-contract] The delta gate's placement ("check `git status --porcelain` after
  `git add`, before `state.Touch`") is the correct order and the plan got it right:
  the current ship_recover.go does Touch→Save→add→commit, but Touch mutates
  state.json *inside* `.dross/`, which would itself manufacture a delta and defeat
  the c-6 no-op. t-1 therefore must reorder to restore→add→check-delta→(Touch+Save+
  re-add)→commit. Worth keeping front-of-mind during implementation since it's a
  reorder of existing code, not a pure extraction.
- [test-contract] t-3/t-4 use doc-presence tests rather than behavioural tests —
  the right level for markdown prompts. resume.md already runs `dross status` in its
  pre-flight (line 18), so the c-4 resume limb is genuinely delivered by t-2's status
  emission plus the t-3 doc, a clean single-detection-path design consistent with the
  stale_state_surface decision (no second Go detection path).
- [strengths] Test contracts are unusually rigorous: each names the test, the
  fixture, and the exact regression it catches via a negative control
  (e.g. "drop the delta gate → phantom empty commit → count becomes 1 → fails";
  "a current-phase-only restore drops 02-y → fails"). This is the strongest part of
  the plan.
- [strengths] Wave structure is minimal and correct — only t-5's real code
  dependency on t-1's runDrossRecovery is serialized into wave 2; the independent
  prompt/status surfaces (t-2, t-3, t-4) all run in wave 1.
- [strengths] Source citations verified accurate (phase.go:314 ff-only merge,
  phase.go:277-279 dirty guard, repoRootFromTest, and the cited existing ship-recover
  tests all exist), and the plan honours recover_command_fate precisely — one shared
  routine, two entry points, no duplication, nothing deprecated.

## Summary
A tight, well-sequenced plan that fully covers the spec and respects every locked
decision; the only open item is a coverage-bookkeeping seam on c-3's wrong-branch
limb, which is a FLAG, not a blocker.
