# Plan Review — 14-stable-slug-phase-ids

Reviewed: 2026-06-25
Plan: 6 tasks across 3 waves

## BLOCKING

(none)

- The prior blocker (r-01 stale binary) is **resolved**. t-6 now opens with
  `make install` before invoking `dross phase migrate`, so the migrate subcommand
  built in t-5 (source-only) is actually present in the binary the repo migration
  runs against. Scoping is right: t-2..t-5 are in-process unit tests
  (`phase_test.go`, `phase_migrate_test.go`) calling functions directly, so they
  need no install; only t-6 drives the real installed binary, which is exactly
  where the step was added.
- Coverage is mechanically complete: pc-1 (t-1,t-2), pc-2 (t-1,t-3),
  pc-3 (t-1,t-4), pc-4 (t-5,t-6), pc-5 (t-1). No locked decision is contradicted
  (identity_model, slug_collisions, migration_command, legacy_shim, display_scope
  are each honored). The only rule, r-01, is satisfied by t-6.

## FLAG

- **pc-5 is "covered" only by an uncalled helper — no task wires the shim into
  the resolution path.** t-1 adds `ResolveDir`/`StripLegacyPrefix` and its
  contract tests them in isolation, but every user-facing resolver still calls
  `phase.Dir(root, id)` directly: `phaseShow` (internal/cmd/phase.go:366),
  `verify.go:43`, `ship.go:65`, `task.go:129`, `issue.go:252`. No wave-2/3 task
  swaps `phase.Dir`→`ResolveDir` on the phase-id argument path. As planned, after
  migration `dross phase show 03-fix-completion-chore-divergence` looks for
  `phases/03-…` (now renamed away) and errors — so pc-5's promise ("old
  references, scripts, and muscle memory keep working after migration") is not
  delivered, while verify could still go green on t-1's isolated helper test.
  This is the strongest finding and borders on blocking; the merge of the old
  t-1/t-2 helper tasks did not add a task to actually call what the helper
  enables.

- **pc-4's residual is unowned.** Skipping phase 14 (the in-flight phase) is the
  correct fix and does not fail this phase — validate stays green because 14's
  dir still matches its `plan.phase.id` (validate.go:80), and t-5 fully delivers
  pc-4's command capability. But "14 migrates on a later run after it ships" is
  owned by no task, no `[[deferred]]` entry, and no reminder — it lives only in
  t-6's prose. If that re-run is forgotten the repo stays permanently part-NN-slug.
  Recommend recording it as an explicit follow-up rather than narrative.

## NOTE

- The prior self-migration FLAG is **resolved**. t-5 now never touches
  `state.current_phase`: its dir, id and array entry are left as-is so its
  `phase/<id>` branch keeps resolving for ship. Its contract asserts the in-flight
  dir is byte-untouched and that omitting the plan.id rewrite makes validate fail.
- Renumbering did not break wiring: every `depends_on` (t-2..t-5 → t-1, t-6 → t-5)
  targets a live task id; no dangling refs. Wave order is correct — helpers (1) →
  command wiring (2) → real-repo migration (3), with t-6 gated on t-5.
- Wave 2 has four tasks (t-2..t-5) all editing `internal/cmd/phase.go`, but
  `Plan.NextRunnable` (internal/phase/phase.go:135) returns one task at a time,
  ordered by wave then id, so execution is sequential — no parallel merge
  conflict. The "wave" label overstates parallelism here; fine as-is.
- t-3 (create) appends to "the current milestone's phases array" with no stated
  handling for an empty `state.current_milestone`. Worth a guard so create on a
  milestone-less repo fails clearly instead of writing a stray array.
- Test contracts are mutation-grade throughout — each names the exact regression
  it kills ("reverting to sort.Strings fails the gamma-before-alpha case", "a loop
  stopping at the first probe fails the foo-3 case", "skipping the plan.toml id
  rewrite makes validate report the dir/plan.id mismatch"). The orphan 01/02 case
  (real — v0.1's array starts at 03) is correctly anticipated in t-6.

## Summary

The prior BLOCKING (r-01) is genuinely fixed by t-6's `make install` step, and the
amendment introduced no new blocker: depends_on survived renumbering, the skip-
current redesign resolves the earlier self-migration foot-gun while keeping ship
and validate working, and the pc-4 residual is a defensible inherent limitation
rather than a phase-failing gap. The most important open issue — not new to the
amendment but real — is that pc-5 is covered on paper by an uncalled helper: no
task wires `ResolveDir` into the phase-id resolution path, so legacy NN-slug
arguments will not actually resolve post-migration. Recommend adding that wiring
(or widening t-2/t-3's scope) and recording phase 14's post-ship migration as an
explicit follow-up before execution.
