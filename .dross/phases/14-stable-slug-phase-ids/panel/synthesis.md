# Synthesis — 14-stable-slug-phase-ids

Cold judge over three independent decompositions (risk / mvp / verification). I
authored none of them. Goal: merge the strongest plan and surface the real
disagreements rather than smoothing them.

Path check: every file the drafts name exists or is a legitimate new
file/test. `internal/phase/phase.go` (`Dir`, `List` via `ReadDir`+`sort.Strings`,
`Slugify`), `internal/cmd/phase.go` (`nextPhaseNumber`, `%02d-%s`, `Phase()`
AddCommand registration), `internal/cmd/status.go`, `internal/cmd/validate.go`,
`assets/prompts/plan.md`, `.dross/milestones/v0.{1,2,3,4}.toml`, `.dross/state.json`
all present. New files (`migrate.go` / `phase_migrate_test.go` /
`validate_test.go`) are creatable, not phantom references. No task references a
non-existent file — none rejected on that ground.

Confirmed factual anchor: `v0.1.toml` phases array starts at `"03-..."` — phases
01 and 02 exist on disk but are in **no** milestone array (orphans). This drives
Disagreement A below.

## Scores

Scale: weak / ok / strong. One verdict per draft per dimension.

| Dimension                  | risk (6t/2w)                                                      | mvp (3t/2w)                                                       | verification (7t/3w)                                                       |
| -------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Criteria coverage          | strong — all 5, engine+consumer double-cover; but stops at fixture-validated command, never migrates *this* repo | ok — all 5 claimed, but coarse; migrate only proven on a fixture, repo not migrated | strong — all 5, and t-7 is the only task that actually migrates this repo so pc-4's "all existing phases migrated / validate passes" is true on the real tree |
| Test-contract specificity  | strong — concrete in/out + an explicit "if X regresses, this test fails" per task | ok — concrete, but t-2 folds 4 contracts (create/list/number/collision) into one oracle | strong — ideal-test-first; regexp `^\d\d-` matches nothing, `"2 of 3"` line, byte-identical idempotency, `01-foo`+`03-foo`→`foo`/`foo-2` |
| Granularity                | ok — 3 crisp wave-1 helpers, but t-6 wires 5 files in one commit | weak — 3 tasks; t-2 spans 3 files + 4 behaviours, heavy for one test-gated commit | strong — each task = one wire, one named test; t-3 (list) is tiny but honest |
| Wave correctness           | ok — 2 waves, helpers→consumers, DAG sound; repo-migration not isolated | ok — 2 waves, sound, but no isolation of repo migration         | strong — 3 waves; wave 3 (repo migration) correctly isolated behind the built binary it needs |

**Skeleton: verification.** It has the best test oracles, the finest honest
granularity, and is the only draft whose wave structure isolates the actual
repo migration (t-7) — the part pc-4 literally demands and the part carrying the
self-rename hazard. risk and mvp ship a correct *command* but leave this repo on
NN- dirs, so pc-4 would not actually pass on the dross repo itself.

## Merged plan

Display format — `t-N [origin] title`, then `files / covers / depends / contract`.
7 tasks across 3 waves.

### Wave 1 — pure identity helpers (fast unit oracle, no fs/git fixture)

**t-1 [verification+risk] Legacy-shim + array-ordering helpers**
- files: `internal/phase/phase.go`, `internal/phase/phase_test.go`
- covers: pc-1, pc-5
- contract: `StripLegacyPrefix("03-fix-foo")=="fix-foo"` and
  `StripLegacyPrefix("fix-foo")=="fix-foo"` (no over-strip of a non-numeric
  leading segment); with only `phases/onboarding/` present,
  `ResolveDir(root,"12-onboarding")` and `ResolveDir(root,"onboarding")` both
  return the onboarding dir, and an id matching no dir returns a not-found error
  (never a silent wrong hit — risk's anti-mis-resolution assertion).
  `Ordered(["gamma","alpha"], {alpha,gamma,orphan})` returns
  `["gamma","alpha","orphan"]` — array order first, falling back to
  `sort.Strings` fails the gamma-before-alpha case. **Grafted from risk:** an
  array entry with no dir on disk is reported as a *stale reference* problem; a
  dir on disk in no array is an *orphan* and is tolerated (appended, never
  dropped, never silently duplicated). See Disagreement A for why orphan is
  tolerated, not an error.

**t-2 [verification+mvp] DisplayNumber + UniqueSlug helpers**
- files: `internal/phase/phase.go`, `internal/phase/phase_test.go`
- covers: pc-2, pc-3
- contract: `DisplayNumber(["alpha","beta","gamma"],"beta")==2`,
  `DisplayNumber(["gamma","beta","alpha"],"alpha")==3` (recomputed from array
  index, so a reorder changes it), `DisplayNumber(arr,"missing")==0` (sentinel,
  not a panic, not a guessed number). With `phases/foo` and `phases/foo-2` on
  disk, `UniqueSlug(root,"foo")=="foo-3"`; with none present, `=="foo"`
  unchanged — a collision loop that stops at the first probe fails the `foo-3`
  case.

### Wave 2 — wire each helper into exactly one command (depends t-1, t-2)

**t-3 [verification] Order `dross phase list` by milestone array**
- files: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- covers: pc-1 / depends: t-1
- contract: set milestone v0.4 `phases=["gamma","alpha"]`; `dross phase list`
  stdout `== "gamma\nalpha\n"`; reordering to `["alpha","gamma"]` flips the
  output. Reverting to `phase.List`'s `ReadDir`+`sort.Strings` prints
  alphabetical regardless of the array → test fails.

**t-4 [verification+risk] Rewrite `dross phase create` to slug identity**
- files: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- covers: pc-2 / depends: t-1, t-2
- contract: `create "My Feature"` with current milestone v0.4 makes
  `phases/my-feature/` (regexp `^\d\d-` matches nothing under `phases/`), writes
  `[phase] id="my-feature"`, checks out branch `phase/my-feature`, and appends
  `"my-feature"` as the last element of v0.4's phases array. A second
  `create "My Feature"` yields `phases/my-feature-2` (UniqueSlug end-to-end,
  no clobber of the first). `nextPhaseNumber` and `%02d-%s` are deleted; their
  removal is proven by the no-prefix assertion. Dropping the milestone append
  fails the array-tail assertion (risk's explicit append guard).

**t-5 [verification+mvp] Add `dross phase number` + wire status display + patch-digit prompt**
- files: `internal/cmd/phase.go`, `internal/cmd/status.go`,
  `internal/cmd/phase_test.go`, `assets/prompts/plan.md`
- covers: pc-3 / depends: t-2
- contract: `dross phase number beta` prints `"2"` for milestone
  `[alpha,beta,gamma]` and prints its new index after the array is reordered —
  counting directories instead of array position fails the post-reorder value.
  `status` `renderPhase` for `current_phase=beta` emits a line containing
  `"2 of 3"`; dropping the `DisplayNumber` call removes the number. `plan.md`'s
  version patch-digit step is changed to read `dross phase number` (single
  source for the ordinal), asserted by grepping the prompt for the command
  string. NOTE r-01: this prompt edit is not live until `make install`. See
  Disagreement C — risk rejects this prompt edit.

**t-6 [verification+risk] Add idempotent `dross phase migrate` command**
- files: `internal/cmd/phase.go` (register in `Phase()`),
  `internal/cmd/migrate.go`, `internal/cmd/phase_migrate_test.go`
- covers: pc-4 / depends: t-1, t-2
- contract: fixture with `phases/01-foo`, `phases/02-bar`, milestone
  `[01-foo,02-bar]`, `state.current_phase=01-foo`. After one run: dirs `foo` and
  `bar` exist (`01-`/`02-` gone), each `spec.toml` **and** `plan.toml`
  `[phase].id` == its slug, the milestone array == `["foo","bar"]`,
  `state.current_phase=="foo"`, and `dross validate` exits 0 (skipping the
  `plan.toml` id rewrite makes validate report the dir/plan.id mismatch and this
  fails). A second run leaves the dir listing and every file's bytes identical
  (no `foo-2` spawned, exit 0) — proves idempotency. A fixture with both
  `01-foo` and `03-foo` maps them to `foo` and `foo-2`, the two milestone
  entries remapped distinctly. **Grafted from risk:** if a target slug dir
  already exists on disk at rename time (collision), migrate refuses that dir
  with a clear error rather than overwriting; a partial tree (some bare, some
  prefixed) converges to fully-bare in one run rather than erroring. git
  branches are never referenced by the command.

### Wave 3 — migrate this repo (depends t-6)

**t-7 [verification] Migrate this repo's phases to slug identity**
- files: `.dross/phases/` (renames), `.dross/milestones/v0.1.toml`,
  `.dross/milestones/v0.2.toml`, `.dross/milestones/v0.3.toml`,
  `.dross/milestones/v0.4.toml`, `.dross/state.json`
- covers: pc-4 / depends: t-6
- contract: after running `dross phase migrate` on this repo, `ls .dross/phases`
  matches no `^\d\d-` entry, `dross validate` exits 0, every milestone phases
  array holds bare slugs, and `git branch --list 'phase/*'` still lists the old
  NN- branch names (history untouched — touching branches fails the branch-name
  assertion). Self-rename hazard: this also renames phase 14's own dir
  (`14-stable-slug-phase-ids` → `stable-slug-phase-ids`, including `panel/` and
  `spec.toml`), so run it **last and on-branch**. Phases 01/02 (orphans, in no
  array) are renamed to bare slugs uniformly but no array entry is rewritten for
  them (mvp's uniform-rename judgment) — and validate must stay green on them
  (Disagreement A).

### Coverage map (all five covered)
- pc-1 (order from milestone array; list reads it; reorder reorders) → t-1 (`Ordered`), t-3 (list wiring)
- pc-2 (bare `<slug>/` dir, branch `phase/<slug>`, appended to array, collision suffix) → t-2 (`UniqueSlug`), t-4 (create)
- pc-3 (single `DisplayNumber`; status + version patch digit; survives reorder) → t-2 (helper), t-5 (number cmd + status + prompt)
- pc-4 (all phases migrated; ids/arrays/state updated; branches historical; validate green; idempotent command) → t-6 (command), t-7 (this repo)
- pc-5 (legacy NN-slug arg resolves via prefix strip) → t-1 (`StripLegacyPrefix` + `ResolveDir`)

## Disagreements

### A. How are orphan phases (01/02, in NO milestone array) ordered and validated?
This is the load-bearing one — phases 01 and 02 are factually in no array.
- **risk**: `Ordered` flags a dir-not-in-array as an orphan *problem*, and its
  t-6 makes `validate` flag "a phase on disk missing from its milestone array."
  Taken literally this makes `validate` **fail** on phases 01/02.
- **mvp**: migrate renames 01/02 uniformly to slug dirs; `phase list` only ever
  prints the current milestone's array, so orphans are simply never listed; no
  flagging anywhere.
- **verification**: `Ordered` *appends* orphans after the array entries (visible,
  ordered last); adds no validate-orphan rule.
- **Provisional default (taken):** orphans are **tolerated, never an error**.
  Migrate renames them to bare slugs (mvp); `DisplayNumber` returns 0 for any id
  not in the queried milestone (all three agree on the 0 sentinel); `Ordered`
  treats array-entry-without-dir as a *stale* problem but dir-without-array as a
  tolerated orphan (risk's stale half, not its orphan-as-error half); `validate`
  does **not** fail on an array-less dir.
- **Why it matters:** pc-4 requires `dross validate` to pass after migration on
  *this* repo. Adopting risk's "validate flags any dir missing from an array"
  would make validate fail on the legitimately-orphaned 01/02 and break pc-4.
  That is the decisive reason risk's orphan-as-error graft is **rejected** while
  its stale-reference graft is kept.

### B. Does this phase actually migrate *this* repo, or only ship the command?
- **risk** / **mvp**: stop at a migrate command proven green on a fixture; no
  task renames the dross repo's own `.dross/phases/`. Result: 2 waves.
- **verification**: adds t-7 to run migrate on this repo (renames, milestone
  arrays, state.json, validate green). Result: 3 waves.
- **Provisional default (taken):** include t-7 (verification). pc-4 says "*All
  existing phases are migrated … and dross validate passes afterward*" — that is
  a statement about the real tree, not a fixture.
- **Why it matters:** without t-7 the dross repo keeps NN- dirs and pc-4 is
  unmet on the very repo being changed; t-7 also owns the self-rename hazard
  (phase 14 renaming its own dir/panel/spec mid-run) that must be sequenced last
  and on-branch — a real risk that simply doesn't exist if you stop at the
  fixture.

### C. Edit `assets/prompts/plan.md` to wire the version patch digit?
- **risk**: explicitly *rejects* a prompt edit ("no Go test could gate it") and
  makes `dross phase number` the only, Go-testable surface for the ordinal.
- **mvp**: adds `phase number`, says workflows read it, does not touch plan.md.
- **verification**: adds `phase number` *and* edits plan.md so the patch-digit
  step calls it, gating the edit by grepping the prompt for the command string.
- **Provisional default (taken):** keep both (verification) — `phase number` as
  the Go-tested core *and* the plan.md edit so the patch digit literally "uses
  it" per pc-3, gated by the prompt-grep. Flagged with r-01: the prompt edit is
  not live until `make install`.
- **Why it matters:** pc-3 says the version patch digit *uses* the single
  helper. Shipping only the command (risk/mvp) leaves the prompt still telling
  Claude to eyeball the array, so the criterion's "uses it" clause is only
  half-met. risk is right that the grep test is weak — hence the Go-tested
  command stays the primary oracle and the prompt edit is the secondary wire.

---
synthesis: 7 tasks across 3 waves, 3 disagreements
