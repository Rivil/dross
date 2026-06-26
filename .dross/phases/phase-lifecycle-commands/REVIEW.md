# Plan Review — phase-lifecycle-commands

Reviewed: 2026-06-26
Plan: 5 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [antipattern/reuse] t-4 claims it will scaffold the new phase "reusing phaseCreate's
  machinery", but phaseCreate (internal/cmd/phase.go:112) has no extractable helper — the
  dir-make, preflight, `git checkout -b`, state-write, and milestone-append logic all live
  inline inside the cobra RunE closure. t-4 will have to extract a shared scaffold function
  or duplicate it, which is more work than "reuse" implies. Worse, phaseCreate's slug step is
  `phase.UniqueSlug` (phase.go:112), which AUTO-SUFFIXES on collision — the exact behavior the
  locked `input_validation` decision forbids ("refuse ... rather than auto-suffixing"). And
  phaseCreate appends to the milestone array at the tail, whereas insert must splice at the
  anchor. So the reusable subset is narrower than the claim suggests.
  Suggestion: have t-4 reuse only `preflightPhaseBranch` + mkdir + `checkout -b` + state-write,
  and explicitly use `phase.Slugify` + a strict collision check (NOT `UniqueSlug`) for the slug.
  Consider whether extracting a `scaffoldPhase` helper belongs in t-3's shared-plumbing scope.

- [granularity] t-5 (rename) is the heaviest task: 4 files and ~6 distinct operations spanning
  fs (dir move), git (branch -m), toml (spec/plan id rewrite via rewritePhaseID), deferred
  re-point (new write logic in deferred.go), state (current_phase), plus no-op and
  target-exists ordering. That is 4+ layers in one task.
  Suggestion: consider splitting the deferred-re-point (collectDeferred scan + write-back) into
  its own task; it is the most separable piece and has its own two test contracts.

- [wave-order/parallelism] t-4 and t-5 are both wave 3 and both edit the SAME three files —
  internal/cmd/phase.go, internal/cmd/phase_lifecycle.go, and internal/cmd/phase_lifecycle_test.go.
  If the wave is executed concurrently they will collide on every one of those files.
  Suggestion: either mark them for sequential execution within the wave, or split the shared
  files so insert and rename touch disjoint files. The depends_on graph is correct; the file
  overlap is the risk.

## NOTE
- [reuse] t-5's "re-point deferred via collectDeferred" is sound, but collectDeferred
  (deferred.go:40) is READ-only — it returns an inventory. The actual rewrite (load each other
  spec, set Target old→new, Save) is new code. The scan claim is fine; just flagging that the
  write path isn't free.
- [decision-fidelity] refuseIfShipped's "live origin phase branch ⇒ open-PR window" heuristic
  (t-3) is a proxy for the locked inflight_guard's "shipped/awaiting-merge (open PR)" state. It
  matches phaseComplete's existing `git ls-remote --heads origin <branch>` model (phase.go:332)
  and the decision's rationale ("unstarted/planning phases have no remote branch"), so it's
  consistent — but note it will over-refuse a phase that was pushed without a PR, or whose PR
  merged but whose remote branch wasn't deleted. Acceptable given the decision, worth a comment.
- [strengths] Test-first ordering is excellent: t-2 builds the byte-for-byte snapshotPhases /
  assertUntouched harness — with explicit anti-vacuous self-tests — in wave 1, BEFORE any
  mutating verb exists, directly enforcing the "byte-for-byte unchanged" guarantee in c-1/c-2/c-3.
- [strengths] t-1 cleanly separates pure slice logic (InsertRelative/MoveRelative/RenameInArray +
  ErrAnchorNotFound) from all git/fs side effects, sitting beside the real Ordered/DisplayNumber
  helpers it extends — easy to unit-test the splice in isolation.
- [strengths] Test contracts are specific and falsifiable throughout: they name the exact surface
  (InsertRelative, assertUntouched, `phase number`, `dross validate` exit code, `git branch -m`)
  and the exact expected arrays, rather than vague "works correctly" assertions.

## Summary
A genuinely strong, test-first plan with full criterion coverage and no locked-decision
contradictions; the only real risks are the overstated phaseCreate reuse (which hides a
UniqueSlug auto-suffix trap), t-5's breadth, and the t-4/t-5 same-file overlap in wave 3.
