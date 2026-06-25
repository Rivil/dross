Phase 14-stable-slug-phase-ids — 3 tasks across 2 waves

Lens: MVP — smallest task set that satisfies every criterion. Phase identity
becomes the slug; order is read from the milestone phases array; one
resolving `Dir` is the permanent legacy shim; one `DisplayNumber` helper is
the single ordinal source. Insert/move/rename stay deferred to phase 15.

Wave 1
  t-1  Add slug-identity primitives to internal/phase
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-5
       desc:     Add StripOrdinal(id) (drops a leading `NN-`). Make Dir()
                 resolve a legacy id: if phases/<id> is absent but
                 phases/<StripOrdinal(id)> exists, return the stripped path —
                 this is the permanent back-compat shim, and because every
                 command already routes phase ids through phase.Dir it lights
                 up repo-wide for free. Add DisplayNumber(phases, slug) int
                 (1-based position in the array, 0 if absent) and
                 UniqueSlug(root, slug) (returns slug, else slug-2/-3… when a
                 dir already exists).
       contract: - Dir(root,"03-foo") returns the phases/foo path when only
                   phases/foo/ exists and phases/03-foo/ does not — if the
                   legacy-id resolver breaks, this returns the wrong path.
                 - DisplayNumber(["a","b","c"],"b")==2, and ==3 after the
                   slice is reordered to ["a","c","b"] — guards the ordinal
                   source against directory-sort assumptions.
                 - UniqueSlug(root,"foo") returns "foo-2" when phases/foo/
                   already exists — guards the collision auto-suffix.

Wave 2 (depends t-1)
  t-2  Make the phase command surface slug-native
       files:    internal/cmd/phase.go, internal/cmd/status.go,
                 internal/cmd/phase_test.go
       covers:   pc-1, pc-2, pc-3
       depends:  t-1
       desc:     `phase create`: id = Slugify(title) run through UniqueSlug,
                 branch phase/<slug>, append the slug to the current
                 milestone's phases array; delete nextPhaseNumber and the
                 "%02d-" formatting. `phase list`: print the current
                 milestone's phases array in array order (not dir sort). New
                 `phase number [slug]` subcommand prints DisplayNumber over
                 the current milestone array — the value workflows read for
                 the version patch digit. status renderPhase prints the
                 current phase's DisplayNumber ("phase N of M").
       contract: - Creating a phase titled "Foo Bar" makes phases/foo-bar
                   (no numeric prefix), writes [phase] id="foo-bar", checks
                   out phase/foo-bar, and appends "foo-bar" to the current
                   milestone phases array — if any of those regress, the
                   create test fails (pc-2).
                 - `phase list` emits ids in the milestone array's order;
                   swapping two entries in the array swaps two lines of
                   output — if list falls back to directory sorting this
                   fails (pc-1).
                 - `phase number <slug>` prints the slug's 1-based array
                   index, and prints a different number after the array is
                   reordered — if DisplayNumber is wired to dir order this
                   fails (pc-3).
                 - Creating "Foo Bar" twice yields phases/foo-bar-2 for the
                   second (collision auto-suffix, pc-2).

  t-3  Add idempotent `dross phase migrate`
       files:    internal/cmd/migrate.go, internal/cmd/phase.go,
                 internal/cmd/migrate_test.go
       covers:   pc-4
       depends:  t-1
       desc:     New `phase migrate` subcommand (registered in Phase()):
                 for every phases/NN-slug/ dir, rename to phases/<slug>/,
                 rewrite spec.toml and plan.toml [phase] id to the slug,
                 rewrite each milestone phases array entry NN-slug→slug
                 (via StripOrdinal), and rewrite state.json current_phase if
                 it carries a prefix. Re-running is a no-op.
       contract: - After migrate over a fixture with phases/03-foo/, no
                   phases/ dir name retains a `NN-` prefix, spec.toml/plan.toml
                   id read "foo", and the milestone phases array holds "foo" —
                   if any rewrite is skipped the migrate test fails (pc-4).
                 - `dross validate` exits 0 on the migrated fixture — if
                   plan.phase.id no longer matches the renamed dir, validate
                   reports a problem and this fails (pc-4).
                 - A second `phase migrate` run reports zero renames/rewrites
                   and leaves file contents byte-identical — if migrate isn't
                   idempotent this fails.

## Coverage
- pc-1 (order from milestone array; list reflects it) → t-2
- pc-2 (bare <slug>/ dir, id+branch, appended to array, collision suffix) → t-2 (+ UniqueSlug from t-1)
- pc-3 (single displayNumber: status + version patch digit, reorder-stable) → t-2 (+ DisplayNumber from t-1)
- pc-4 (migrate dirs/ids/arrays/state; validate green; idempotent) → t-3
- pc-5 (legacy NN-slug arg still resolves via prefix-strip shim) → t-1

## Judgment calls
- Put the legacy shim inside phase.Dir() rather than a separate Resolve() wired into ~6 command call sites. Chose the one-function approach because every phase-id consumer already goes through Dir, so pc-5 lights up repo-wide with one change and one test; rejected per-site wiring as speculative surface area phase 15 doesn't need.
- Merged "phase create/list", "phase number", and "status display" into one wave-2 task (t-2). Chose to bundle because they share internal/cmd/phase.go and the single DisplayNumber helper, and splitting per-criterion would manufacture three near-empty tasks; rejected a separate display task as over-structuring under the MVP lens.
- Made `phase number` a thin CLI surface over DisplayNumber instead of computing the version patch digit in Go. Chose this because the patch digit is set by workflow prompts (the global versioning rule), so the testable Go contract is "the command prints the right ordinal"; rejected adding patch-digit math into Go as scope the workflow already owns.
- Kept migrate as a command registered in Phase() (no main.go edit) and gave it its own file/test rather than folding it into phase.go. Chose separation because pc-4's idempotency + validate-green contract is distinct from the create/list behavior and deserves an isolated test; rejected merging into t-2 as it would push that task past two concerns and four files.
- Left phases 01/02 (in no milestone array) handled by the same dir-rename + id-rewrite pass; they get slug dirs even though no array references them. Chose uniform renaming so `validate` and the shim see a consistent namespace; rejected special-casing unreferenced phases as needless branching.

mvp: 3 tasks across 2 waves, criteria covered 5/5
