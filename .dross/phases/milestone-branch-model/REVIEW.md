# Plan Review — milestone-branch-model

Reviewed: 2026-07-01
Plan: 7 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [granularity/parallel-safety] t-3 and t-6 both modify `internal/cmd/phase.go` and
  `internal/cmd/phase_test.go`, and both live in wave 2. They are independent features
  (phase-create rooting vs phase-complete ff) with no dependency between them, so if
  wave-2 tasks run in parallel they will collide on the same two files.
  Suggestion: serialize them — add `depends_on = ["t-3"]` to t-6 (or assign both to a
  single executor), or move one to a later wave, so the shared-file edits don't race.

- [locked-decision] The `milestone_main_merge` decision (non-squash merge-commit for
  milestone→main) is honored only by a documentation note in ship.md (t-7). Nothing in
  the plan enforces or tests the merge method, and the repo default is `squash_merge = true`
  (project.toml) — so the actual merge could silently squash and re-introduce the very
  divergence the decision exists to avoid. There is no test contract covering it.
  Suggestion: acknowledge the merge method is a merge-time provider/human action and make
  the milestone-complete PR body / prompt explicitly instruct a non-squash merge; if no
  automated enforcement is possible, state that plainly so verify doesn't expect a test.

- [locked-decision] `no_milestone_fallback` states "the commands nudge the user to scope a
  milestone." Only t-3 (phase create) emits a nudge (TestPhaseCreateNudgesNoMilestone).
  t-4 (quick) and t-5 (ship fallback) fall back to main silently with no nudge.
  Suggestion: either extend the nudge to quick and ship, or narrow the decision text to
  phase-create only so the plan matches the locked wording.

- [coverage-accuracy] t-2 is tagged `covers = ["c-1"]`, but its own description says it
  "delivers no criterion on its own," and its test contract verifies the base resolver
  (milestone/main selection), not c-1 (a milestone branch is cut+pushed at scope time).
  c-1 is genuinely covered by t-1, so this is not a coverage gap — but the mis-tag will
  point verify at resolver tests that never prove c-1.
  Suggestion: drop the `covers` tag from t-2 (or mark it enabler-only) so c-1 maps solely
  to t-1's branch-creation tests.

## NOTE
- [wave-order] t-3, t-4, t-5, t-6 all declare `depends_on = ["t-1", "t-2"]`, but their
  production code only calls `resolveNewWorkBase` (t-2); none call t-1's branch-creation
  code (their tests set up milestone branches directly). The t-1 edge is over-declared.
  Harmless — both t-1 and t-2 are wave 1, so wave placement is unchanged — but the graph
  overstates the real dependency.

- [strength] Test contracts are exemplary: each names the exact test function and the
  precise failure condition (ancestor probe vs tip equality in t-3, `ls-remote` against a
  bare origin in t-1, reading `origin/milestone/<v>` vs `origin/main` in t-6). These are
  the kind of specific, falsifiable contracts the check asks for.

- [strength] `rollout_cutover` (v0.7 finishes under the old model) is enforced cleanly via
  ref-existence inside `resolveNewWorkBase` and directly tested
  (TestResolveBase_CutoverNoBranch: current_milestone=v0.7 + no ref → main,false),
  correctly sidestepping the bootstrap paradox where this phase itself ships under v0.7.

- [antipattern-clear] File references verified against source: `basebranch.go` does not
  exist and is created by t-2 (wave 1) before t-3/t-4/t-5/t-6 consume it; `ship.go:222`
  hardcode confirmed; `phase_lifecycle.go` insert checkout-b at :190; `milestoneComplete`
  absent and added by t-7. No dangling file references.

- [rules] Project rule r-01 (prompt/Go edits aren't live until `make install`) applies:
  t-1, t-4, t-7 edit assets/prompts (milestone.md, quick.md, ship.md). Not a plan
  violation, but the executor must run `make install` before verifying prompt-consuming
  behavior. No global rules file (~/.claude/dross/rules.toml) exists.

## Summary
Solid, well-decomposed plan with exemplary test contracts and correct criterion coverage;
address the same-wave phase.go collision (t-3/t-6) and tighten how the non-squash merge and
the fallback nudge honor their locked decisions before executing.
