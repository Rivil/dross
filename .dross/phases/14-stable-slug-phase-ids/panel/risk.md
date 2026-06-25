# Phase 14-stable-slug-phase-ids — risk lens

Bias: failure modes drive the graph. The graph is anchored on the three things
that break a slug rename: (a) a reference resolving to the wrong phase or none,
(b) the order silently losing/duplicating a phase when it lives only in the
array, (c) a migration that dies half-done and can't be re-run. Each of those
gets exactly one owning task with a test that trips when that failure recurs.

Phase 14-stable-slug-phase-ids — 6 tasks across 2 waves

Wave 1
  t-1  Add legacy-id resolver + collision-safe slug allocator
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-5, pc-2
       contract: Resolver strips the numeric prefix and finds the dir whether
                 on-disk is still "03-foo" or bare "foo": passing "03-foo",
                 "foo", or "3-foo" all resolve to phase foo; an arg matching
                 no dir returns a not-found error (never a silent wrong hit).
                 AllocateSlug("foo", existing={foo}) returns "foo-2" and
                 ({foo,foo-2}) returns "foo-3"; an empty namespace returns
                 "foo" unchanged. If the prefix-strip regex or the suffix
                 loop breaks, these resolver/allocator unit tests fail.

  t-2  Add per-milestone displayNumber helper
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-3
       contract: DisplayNumber(phases, id) returns the 1-based index in the
                 array: index 0 → 1, index 2 → 3; after the slice is reordered
                 the same id returns its new position; an id absent from the
                 array returns 0 (sentinel, not a panic and not a guessed
                 number). If the indexing is off-by-one or reads dir order
                 instead of the array, the reorder/absent cases fail.

  t-3  Resolve phase order from the milestone array, not dir sort
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   pc-1
       contract: Ordered(root, version) returns ids in milestone-array order;
                 reordering the array (e.g. swap phases[0] and phases[1])
                 swaps the returned order with no dir rename. A phase listed
                 in the array but missing on disk is reported as a problem
                 (stale reference), and a dir on disk absent from the array is
                 reported as an orphan — neither is silently dropped or
                 duplicated. If ordering reverts to sort.Strings(dirnames),
                 the reorder test fails; if reconciliation is removed, the
                 orphan/stale tests fail.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Create phases as bare slugs appended to the array
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   pc-2
       contract: `phase create "Foo Bar"` makes dir foo-bar with [phase]
                 id="foo-bar" and branch phase/foo-bar — no NN- prefix in dir,
                 id, or branch — and appends "foo-bar" to the current
                 milestone's phases array. A second `create "Foo Bar"` routes
                 through AllocateSlug and yields foo-bar-2 (dir, id, branch,
                 array entry) without clobbering the first. If nextPhaseNumber/
                 "%02d-%s" formatting survives, the no-prefix assertion fails;
                 if the array append is dropped, the array-membership assertion
                 fails.
       depends_on: t-1

  t-5  Add idempotent `dross phase migrate` command
       files:    internal/cmd/phase.go, internal/cmd/migrate.go, internal/cmd/phase_test.go
       covers:   pc-4
       contract: migrate renames every NN-slug dir to its bare slug, rewrites
                 [phase] id in spec.toml/plan.toml, replaces NN-slug entries in
                 every milestone phases array, and updates state.json
                 current_phase — all via the t-1 resolver. Re-running migrate
                 on an already-migrated tree performs zero renames/writes (a
                 no-op run touches no files). A partial state — some dirs bare,
                 some still prefixed — converges to fully-bare in one run
                 rather than erroring. If a target slug already exists on disk
                 (collision during rename) migrate refuses that dir with a
                 clear error instead of overwriting. `dross validate` passes on
                 the migrated tree. If idempotency breaks, the re-run no-op
                 test fails; if state.current_phase isn't rewritten, the
                 stale-reference test fails.
       depends_on: t-1, t-3

  t-6  Wire resolver/order/displayNumber into consumers + validate
       files:    internal/cmd/status.go, internal/cmd/phase.go, internal/cmd/validate.go, internal/cmd/status_test.go, internal/cmd/validate_test.go
       covers:   pc-1, pc-3, pc-5
       contract: `phase list` prints array order (t-3) not dir order; status's
                 phase line shows "phase N of <milestone>" from DisplayNumber
                 (t-2); a new `dross phase number [id]` subcommand prints the
                 1-based ordinal so the version patch digit reads it from one
                 place. `phase show 03-foo`/`task ... 03-foo` resolve through
                 the t-1 shim to phase foo. validate accepts a bare-slug id
                 (matches plan.phase.id == dir) AND flags a phase on disk that
                 is missing from its milestone array. If a consumer still
                 sorts dir names or recomputes the ordinal locally, the
                 list-order or status-ordinal test fails; if validate is left
                 prefix-only, the bare-slug acceptance test fails.
       depends_on: t-1, t-2, t-3

## Coverage
- pc-1 (order from array) → t-3 (engine), t-6 (phase list + validate consume it)
- pc-2 (bare-slug create, append, collision suffix) → t-1 (allocator), t-4 (create)
- pc-3 (single displayNumber; status + version patch) → t-2 (helper), t-6 (status + `phase number`)
- pc-4 (migrate all existing phases, validate passes) → t-5
- pc-5 (legacy NN-slug arg resolves) → t-1 (resolver), t-6 (show/task/validate consume it)

## Judgment calls
- Split the resolver (t-1) from its consumers (t-4 create, t-5 migrate, t-6
  status/show) rather than inlining strip-prefix at each call site — chose one
  owned, unit-tested shim so the "wrong-phase resolution" risk lives in exactly
  one place; rejected per-command prefix handling that would scatter the bug.
- Gave the collision allocator to t-1, not t-4, even though only create calls
  it today — chose to test the -2/-3 suffix logic as a pure function (no git/fs
  fixture needed); rejected burying it inside create where the suffix edge is
  hard to exercise.
- Made array-vs-disk reconciliation (orphan + stale) part of t-3's ordering
  engine, not a separate validate-only check — chose to surface the half-
  migrated/desynced state at the single ordering chokepoint; rejected a
  detached audit task that could drift from the order logic it guards.
- Kept migrate (t-5) depending on t-3, not just t-1 — migration's correctness
  is judged by "validate passes / order intact afterward", so it needs the
  array-reconciliation surface to assert against; rejected wiring it to the raw
  resolver alone, which wouldn't catch a phase stranded out of the array.
- Folded "version patch digit uses displayNumber" into t-6 via a concrete
  `dross phase number` subcommand rather than editing prompt markdown — chose a
  Go-testable single source for the ordinal; rejected a prompt-only change that
  no Go test could gate.
- Did NOT add insert/move/rename verbs — locked as phase-15 scope; touching
  them here would widen the blast radius the slug model is meant to contain.
