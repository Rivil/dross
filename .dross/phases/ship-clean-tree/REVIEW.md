# Plan Review — ship-clean-tree

Reviewed: 2026-07-23
Plan: 4 tasks across 2 waves

## BLOCKING
(none)

Coverage is complete: c-1→t-1, c-2→t-2+t-4, c-3→t-3, c-4→t-4, c-5→t-4. No task
contradicts a locked decision (`chore_push`, `push_failure`, `autocommit_coverage`
each map cleanly onto t-1/t-2/t-4). No forbidden action: runtime.mode is native/Go,
no assets/ prompt edits, no pnpm/docker. rules.toml (r-01) has no bearing on these
tasks.

## FLAG
- [wave / file-contention] Three wave-1 tasks all mutate the same files with no
  ordering between them: t-1, t-2, and t-3 all edit `internal/cmd/phase.go`, and
  t-1 + t-2 both edit `internal/cmd/ship.go`. t-1 and t-2 both insert a new
  pre-flight into the SAME functions (phaseComplete RunE, ship RunE). Sequential
  pair-mode execution is safe (each agent rebases on the prior commit), but true
  wave-1 parallelism would collide on these files, and the auto-commit (t-1) vs
  base-ahead-push (t-2) inserts have an implicit order at the gate (commit local
  .dross dirt before evaluating "base ahead of origin").
  Suggestion: confirm these run sequentially, or add an explicit `depends_on`
  chain (t-2→t-1) so the shared-file inserts land in a defined order.

- [granularity / squash] t-4 carries three distinct concerns across three criteria:
  the --recover reorder (c-4), parameterizing runDrossRecovery by branch to drop the
  milestone abort (c-5), and pushing the restored tree at the network-bearing end
  (c-2 tail). The c-2-tail push is mechanically separable from the milestone
  reorder work.
  Suggestion: consider splitting the "push restored .dross/ tree" into its own task;
  it also benefits `dross ship recover` (shared routine) independently of the
  milestone/reorder change. Not required — the three are cohesive around
  runDrossRecovery — but flag it as a split candidate.

- [granularity] t-1 touches 6 files (over the 5-file threshold). It is cohesive
  (one shared helper + wiring into three gates, all in internal/cmd, single layer),
  so a split would separate the helper from its call sites.
  Suggestion: acceptable as-is; noting for the threshold. If split, split by gate
  (helper+ship vs helper+phase) is the only natural seam and probably not worth it.

## NOTE
- [test-contract] Contracts are uniformly specific across all four tasks — each
  names the exact surface that breaks (the .dross partition, the prefix guard,
  origin-side PR resolution via ship.PRMergedFunc vs the ancestry fallback,
  verify-before-reset SHA-unchanged). No vague "tests pass" contracts. Strength.

- [wave-order] The single genuine dependency (t-4 needs t-3's origin-sourced PR for
  its mergeGate call) is correctly modeled with wave separation and depends_on=["t-3"];
  the other three tasks are correctly left dependency-free. Strength.

- [coverage] c-4's "reorder" is largely already satisfied by the existing code
  (phase.go:344 runs mergeGate before the ff-merge/reset) plus t-3's PR-resolution
  fix — the current refusal is caused by mergeGate reading the stale-tree PR, not by
  reset preceding verification. t-4's verify-before-reset test still meaningfully
  guards the ordering, so coverage is fine; just noting c-4's behavioral delta is
  smaller than the "reorder" wording implies.

- [autocommit_coverage scope] The decision names exactly three gates (ship, phase
  complete, phase create/start). `internal/cmd/milestone.go:129` has a fourth
  dirtyTreeError gate (milestone finalize) that t-1 leaves untouched. Consistent
  with the locked decision's stated scope — recording the asymmetry, not objecting.

- [verify-hygiene] Per the repo's own memory (stale-binary-bites-ship, r-01's second
  clause), the installed `dross` binary can be stale vs source. Neither the plan nor
  its tasks mention `make install` before verify — a verify-phase concern, not a plan
  defect, but worth carrying forward so the stale-binary dogfood bug doesn't recur.

## Summary
Solid, well-covered plan with specific contracts and one correctly-modeled
dependency; the only real thing to settle before execution is the wave-1 shared-file
ordering between t-1/t-2/t-3.
