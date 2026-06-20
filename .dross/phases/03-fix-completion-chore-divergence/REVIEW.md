# Plan Review — 03-fix-completion-chore-divergence

Reviewed: 2026-06-19
Plan: 3 tasks across 2 waves

## BLOCKING
(none)

## FLAG

- [granularity / squashed task] t-2 bundles two distinct behaviours under one task:
  (a) removing the post-merge `chore(dross): complete` commit so `complete` writes
  no commit to main (covers c-1, c-2), and (b) keeping the unmerged-upstream guard
  intact (covers c-5). These are listed as one test_contract pair, but the guard
  (phase.go ~217-236) is *existing* code being preserved, while the commit removal
  (~262-280) is the actual change. Bundling a "preserve existing X" with a "remove Y"
  in one task makes the diff harder to review and the c-5 coverage easy to assert
  vacuously (the guard already passes today).
  Suggestion: acceptable to keep as one task if t-2's c-5 test is genuinely re-run
  against the post-edit code; just confirm the guard test isn't already green for
  reasons unrelated to this change.

- [coverage gap vs locked decision] The `unmerged_pr_guard` decision (locked) and c-5
  require that completing an *unmerged* PR "changes nothing — no branch deletion, no
  state mutation." The existing guard (phase.go:223) only fires when the **local**
  phase branch ref still exists (`rev-parse --verify refs/heads/<branch>`). After
  ship + provider `--delete-branch`, or on a fresh clone, the local ref may be gone —
  in which case the merge-base guard is skipped entirely and `complete` proceeds to
  ff + delete. t-2 says "Keep the upstream-not-advanced guard" but does not address
  this branch-absent escape hatch. Under the new model where ship writes the completed
  record pre-merge, an abandoned-but-unmerged phase with no local branch could pass
  c-5's intent while the guard never runs.
  Suggestion: have t-2 (or its test_contract) explicitly cover the
  "local phase branch absent, PR unmerged" path, not only the "branch present" path
  that TestPhaseCompleteRefusesUnmergedUpstream likely exercises today.

- [test contract specificity] t-1's second contract ("if re-running ship on an
  already-shipped branch errors instead of no-op'ing, the idempotency test fails")
  names the idempotency surface but is ambiguous about what "no-op" means at the git
  layer: re-ship after review edits will produce a *different* tree (the review edits),
  so the completed-state write must be idempotent while the surrounding commit is not.
  The contract should name whether idempotency is asserted on the state.json content
  (current_phase cleared, single `completed <id>` history entry — not duplicated) vs.
  on the commit/push succeeding.
  Suggestion: tighten to "re-shipping does not append a second `completed <id>` history
  entry and leaves current_phase cleared" so the assertion targets the audit record,
  not exit code alone.

## NOTE

- [wave order] Wave structure is correct: t-3 is an integration test that genuinely
  needs the behaviour from both t-1 (pre-push completed-state write) and t-2 (no-commit
  complete) to exist, so wave 2 is justified — not a parallelism miss. t-1 and t-2 are
  correctly independent in wave 1 (ship.go vs phase.go, no shared edit surface).

- [strength] The plan correctly identifies the real root cause confirmed in the source:
  ship.go currently commits its state write AFTER the push (ship.go:189-203), making it
  local-only and discarded on branch delete. t-1's "commit BEFORE the push" directly
  inverts this, which is exactly what the `completion_record_placement` locked decision
  requires. The plan is faithful to the locked decisions, not fighting them.

- [strength] t-3's test_contract is unusually good: it names the precise failure mode
  ("local main carries a standalone commit origin lacks → second-phase fast-forward
  fails with a diverging-branches error"), which is the actual divergence symptom from
  the memory note, rather than a vague "divergence is gone" assertion. c-3's
  "across consecutive phases" intent is met by an explicit N then N+1 loop.

- [forbidden actions] No rule violations. runtime.mode is "native" (not docker), so the
  go test commands implied by the test_contracts are permitted. r-01 (make install
  before relying on prompt/Go edits) is a runtime concern for execution, not a plan
  defect.

## Summary
Coverage and locked-decision fidelity are solid and the plan targets the verified root
cause, but the unmerged-guard's branch-absent escape hatch (c-5) and t-1's
idempotency-surface ambiguity should be tightened before execution.
