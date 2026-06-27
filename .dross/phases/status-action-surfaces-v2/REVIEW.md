# Plan Review — status-action-surfaces-v2

Reviewed: 2026-06-27
Plan: 6 tasks across 4 waves (re-review of amended plan)

## BLOCKING
(none)

## FLAG
- [granularity] t-4 still touches 5 files (internal/cmd/techdebt.go,
  internal/cmd/techdebt_test.go, internal/techdebt/gitignore_test.go, .gitignore,
  cmd/dross/main.go), sitting at the 5-file split threshold. As before this is cohesive
  single-command wiring (build cmd, register on root, ignore artifacts) and registration in
  cmd/dross/main.go is correct, so it is acceptable as-is. If a split is wanted, the natural
  seam is moving the `.gitignore` entry + internal/techdebt/gitignore_test.go into t-3's
  scaffolding task. Unchanged from the prior review; not a blocker.

## NOTE
- [resolution / prior-blocking] The prior BLOCKING (t-3 consumed t-2's output but shared
  wave 1 with no depends_on edge) is RESOLVED. t-3 is now `wave = 2` with
  `depends_on = ["t-2"]`. t-3's run.go/state.go and tests still consume t-2's scan output
  ("a scanned finding set renders into the run dir's report file"), and the new edge correctly
  asserts that dependency and removes the false same-wave parallelizability. The atomic-commit
  test gate can no longer break on t-3-before-t-2 ordering.
- [dependency-graph] The amendment cascaded the downstream waves consistently: t-4 moved to
  wave 3 (it depends on t-3@wave 2) and t-6 to wave 4 (it depends on t-4@wave 3). Every
  depends_on edge points at a strictly lower wave, every wave-N+1 task carries at least one
  wave-N dependency, and waves 1-4 are contiguous. No new graph defect introduced.
- [resolution / prior-note] The prior cosmetic note (t-3 testing run.go's NewRun/WriteReport
  inside state_test.go) is RESOLVED: t-3's file list now includes a dedicated
  internal/techdebt/run_test.go alongside state_test.go, matching the security-package
  convention.
- [resolution / prior-note] The prior TOML field-ordering hazard is now addressed in the plan
  text: t-1's description explicitly requires declaring the scalar last_run field "before the
  existing Records slice so the TOML encoder emits it before the [[finding]] tables." The
  round-trip test contract still guards it.
- [strength / coverage] All four criteria remain covered: c-1 (t-1/t-5/t-6), c-2 (t-1/t-5/t-6),
  c-3 (t-6), c-4 (t-2/t-3/t-4). c-3's "no dead command" clause is directly guarded by t-6's
  "every available area resolves to a real surface … no '(planned)'" test.
- [strength / test-contract] Test contracts remain exemplary — every entry is a falsifiable
  "if X breaks, the test fails: <concrete behavior>" naming the exact surface at risk
  (word-boundary marker regex, NUL/zero-byte handling, no-trailing-newline last-line counting,
  same-second NewRun `-N` clobber suffix, prune-proof "last run" rendering with run dirs
  deleted, future-timestamp rendering). No vague contracts.
- [strength / locked-decisions] Still faithful to all five locked decisions: fixed in-code
  catalog (no [[action_areas]] config), dependency-free marker+size scan, a no-prompt
  deterministic `dross techdebt` command writing a durable run record, prune-proof store-level
  last_run in .dross/<area>/state.toml, and pure relative ranking with no threshold/config.

## Summary
The amended plan resolves the sole prior BLOCKING (t-3 now wave 2 with depends_on=["t-2"]),
and the wave cascade onto t-4/t-6 is internally consistent. Two prior cosmetic notes
(run_test.go split, TOML field ordering) are also addressed. No new defects. The only standing
item is the unchanged borderline t-4 5-file count, which is acceptable. Plan is clear to
execute.
