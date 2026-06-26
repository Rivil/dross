# Verification-lens decomposition — 14-stable-slug-phase-ids

Lens: design backward from the test contract. For each criterion I wrote the ideal
test first, then carved the smallest task that makes that test satisfiable. The
foundation wave is the pure, fast-to-assert helper layer (one Go test file proves
the whole identity model); wave 2 wires each helper into exactly one command so a
broken wire fails a named command test; wave 3 proves the migration on the real repo.

Phase 14-stable-slug-phase-ids — 7 tasks across 3 waves

Wave 1
  t-1  Add legacy-shim + array-ordering helpers
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-1, pc-5
       contract: phase_test asserts StripLegacyPrefix("03-fix-foo")=="fix-foo" and
                 StripLegacyPrefix("fix-foo")=="fix-foo" (no over-strip of a non-numeric
                 leading segment); with only phases/onboarding present,
                 ResolveDir(root,"12-onboarding") and ResolveDir(root,"onboarding") both
                 return the onboarding dir — if the shim stops stripping the prefix, the
                 "12-onboarding" lookup errors; Ordered(["gamma","alpha"], {alpha,gamma,orphan})
                 == ["gamma","alpha","orphan"] — if it falls back to sort.Strings, the
                 gamma-before-alpha assertion fails.

  t-2  Add DisplayNumber + UniqueSlug helpers
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-2, pc-3
       contract: phase_test asserts DisplayNumber(["alpha","beta","gamma"],"beta")==2,
                 DisplayNumber(["gamma","beta","alpha"],"alpha")==3 (recomputed from array
                 index, so reorder changes it), DisplayNumber(arr,"missing")==0; with
                 phases/foo and phases/foo-2 on disk UniqueSlug(root,"foo")=="foo-3" and
                 with none present =="foo" — if the collision loop stops at the first probe,
                 the foo-3 assertion fails.

Wave 2 (depends t-1, t-2)
  t-3  Order `dross phase list` by milestone array
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   pc-1
       depends:  t-1
       contract: phaseList test sets milestone v0.4 phases=["gamma","alpha"] and asserts
                 `dross phase list` stdout == "gamma\nalpha\n"; reordering the array to
                 ["alpha","gamma"] flips the output. If list reverts to phase.List's
                 ReadDir sort it prints alphabetical order regardless of the array.

  t-4  Rewrite `dross phase create` to slug identity
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   pc-2
       depends:  t-1, t-2
       contract: phaseCreate test runs `create "My Feature"` with current milestone v0.4
                 and asserts the new dir is phases/my-feature (regexp ^\d\d- matches nothing
                 under phases/), the checked-out branch is phase/my-feature, and v0.4.toml's
                 phases array's last element == "my-feature". If %02d formatting survives the
                 no-prefix assertion fails; if the milestone append is dropped the array-tail
                 assertion fails. A second `create "My Feature"` yields phases/my-feature-2
                 (UniqueSlug exercised end-to-end). nextPhaseNumber is deleted; its removal
                 is proven by the no-prefix assertion.

  t-5  Add `dross phase number` and wire status display
       files:    internal/cmd/phase.go, internal/cmd/status.go, internal/cmd/phase_test.go, assets/prompts/plan.md
       covers:   pc-3
       depends:  t-2
       contract: phase-number test asserts `dross phase number beta` prints "2" for milestone
                 [alpha,beta,gamma] and prints its new index after the array is reordered — if
                 it counts directories instead of array position the post-reorder value is wrong.
                 status test asserts renderPhase for current_phase=beta emits a line containing
                 "2 of 3"; dropping the DisplayNumber call removes the number. plan.md's version
                 patch-digit step is changed to read `dross phase number` (single source for the
                 patch ordinal) — asserted by grepping the prompt for the command string.

  t-6  Add idempotent `dross phase migrate` command
       files:    internal/cmd/phase.go, internal/cmd/phase_migrate_test.go
       covers:   pc-4
       depends:  t-1, t-2
       contract: migrate test builds a fixture with phases/01-foo, 02-bar, milestone
                 [01-foo,02-bar], state.current_phase=01-foo, then asserts after one run:
                 dirs foo and bar exist (01-/02- gone), each spec.toml AND plan.toml [phase].id
                 == its slug, the milestone array == ["foo","bar"], state.current_phase=="foo",
                 and `dross validate` exits 0 — if plan.id rewrite is skipped, validate reports
                 the dir/plan.id mismatch. A second run leaves the dir listing and every file's
                 bytes identical (no foo-2 spawned, exit 0) — proving idempotency. A fixture with
                 both 01-foo and 03-foo maps them to foo and foo-2 with the two milestone entries
                 remapped distinctly. git branches are never referenced by the command.

Wave 3 (depends t-6)
  t-7  Migrate this repo's phases to slug identity
       files:    .dross/phases/ (renames), .dross/milestones/v0.1.toml, .dross/milestones/v0.2.toml, .dross/milestones/v0.3.toml, .dross/milestones/v0.4.toml, .dross/state.json
       covers:   pc-4
       depends:  t-6
       contract: after running `dross phase migrate` on this repo, `ls .dross/phases` matches
                 no ^\d\d- entry, `dross validate` exits 0, every milestone phases array holds
                 bare slugs, and `git branch --list 'phase/*'` still lists the old NN- branch
                 names (history untouched). If migrate touched branches the branch-name
                 assertion fails. (Self-note: this also renames 14's own dir to
                 stable-slug-phase-ids, including panel/ and spec.toml — run last, on-branch.)

## Coverage
- pc-1 (order from milestone array; list reads it; reorder reorders) → t-1 (Ordered), t-3 (list wiring)
- pc-2 (bare <slug>/ dir, branch phase/<slug>, appended to array, collision suffix) → t-2 (UniqueSlug), t-4 (create)
- pc-3 (single displayNumber helper; status + version patch use it; survives reorder) → t-2 (DisplayNumber), t-5 (number cmd + status + prompt)
- pc-4 (all phases migrated; ids/arrays/state updated; branches historical; validate passes; idempotent command) → t-6 (migrate command), t-7 (repo migration + validate green)
- pc-5 (legacy NN-slug arg resolves via prefix strip) → t-1 (StripLegacyPrefix + ResolveDir)

## Judgment calls
- Kept phase.List(root) unchanged (still ReadDir-sorted, used by validate/status set-ops) and added a separate Ordered() consumed only by phase list — chose additive over changing List's signature, because validate/doctor/pendingVerdicts iterate as a set where order is irrelevant; reworking List would force edits in 4 callers for no behavioural gain.
- Made migrate rewrite plan.toml [phase].id as well as spec.toml's, not just dir names — chose this over relaxing validate.go's id/dir check, because validate's existing `plan.Phase.ID == dir` guard is the cheapest test oracle for "migration was complete" (pc-4 explicitly requires validate green); loosening the check would delete that oracle.
- Exposed displayNumber as a `dross phase number` subcommand rather than only an internal helper — chose a CLI surface so the version patch-digit rule (a prompt step) has a deterministic, unit-testable source instead of Claude eyeballing the array; status and the command then share one helper, satisfying pc-3's "single helper" literally.
- Split the migration into the reusable command (t-6) and running it on this repo (t-7) across waves — rejected folding the repo migration into t-6, because pc-4 demands both a reusable idempotent command (unit-testable on fixtures) AND this repo's artifacts actually migrated with validate green; they have different oracles and t-7 strictly needs t-6's binary.
- Put DisplayNumber and UniqueSlug together (t-2) and the resolver/ordering pair together (t-1) even though all four live in phase.go — chose two wave-1 tasks over one bundle so each carries a crisp unit contract, and over four micro-tasks since same-file helpers under ~10 lines each shouldn't each be their own commit; the two tasks are logically independent (neither calls the other) so the wave is honest.
