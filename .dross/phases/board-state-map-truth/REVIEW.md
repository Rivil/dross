# Plan Review — board-state-map-truth

Reviewed: 2026-07-30
Plan: 11 tasks across 4 waves

## BLOCKING

- [antipatterns / test-contract-coherence] t-9's emit-set definition makes its own fourth
  assertion unsatisfiable. Line 1 defines the "statuses dross emits" as `--status <literal>`
  in `assets/prompts/*.md` **plus issue.go's lifecycle constants**. t-1 adds
  `statusShipped` / `statusComplete` constants. With those constants in the collection, the
  emit-set contains all five members regardless of ship.md's contents — so line 4
  ("reverting t-5's ship.md edit fails this test") cannot hold. The divergence test becomes a
  tautology over `configenum.LifecycleStatuses` and t-9 loses all teeth against c-2's first
  direction ("a status dross emits has no state-map entry").
  Compounding this: nothing in the plan gives `statusShipped` / `statusComplete` a Go
  reference. t-5's consumer is `assets/prompts/ship.md` (markdown), and t-3 validates against
  the `Set`, not the constants — so t-1 introduces two dead package-level consts whose only
  effect is to poison t-9's collection.
  Suggestion: decide what "emits" means and state it in t-9's description — the defensible
  reading is *call sites only*: the `--status <literal>` occurrences in `assets/prompts/*.md`
  plus the values `derivePhaseStatus` can return. Under that definition line 4 holds and the
  test has real teeth. Then either drop the two new consts from t-1 or name the Go site that
  references them (e.g. `issuePhaseSync`'s terminal handling), because as specified they are
  unreferenced.

## FLAG

- [test contract] t-4's negative control is unsatisfiable with the fixture it implies. The
  contract says "the same fixture with only valid keys returns nil", but `doctor.go:133`
  enters the Board block on `len(b.StateMap) > 0` alone, and then `provider` (empty → invalid,
  `BoardProviders` has no default) and `auth_env` (empty → ✗) each add an issue. A fixture
  carrying only `[board.state_map]` returns non-nil whether the keys are valid or not, so the
  control proves nothing and the positive case cannot attribute the non-nil error to the
  state_map check.
  Suggestion: pin the fixture explicitly in the contract — a complete, otherwise-clean
  `[board]` block (valid provider, `auth_env` exported in the test env, valid `base_url`) so
  the valid-keys run reaches `✓ [board] is well-formed` and returns nil, and the bad-key run
  differs by exactly one ✗ line.

- [test contract] t-4 normalizes on write but nothing covers read/unset symmetry, which is
  the exact failure mode t-4's own fourth line worries about. `readDotted`
  (`internal/cmd/project.go:118-122`) and `unsetDotted` (`:452-461`) both look up
  `p.Board.StateMap[key]` with the **raw** path suffix via `stateMapKey`. Once
  `board.state_map.Planned` stores under `planned`, `dross project get board.state_map.Planned`
  reads back empty and `set --unset board.state_map.Planned` deletes nothing — an entry the
  CLI wrote and cannot address. The contract pins get/unset only for `planning`, which is
  already lowercase, so the gap is invisible to the test.
  Suggestion: add a contract line requiring `stateMapKey` to normalize (so write, read and
  unset agree), asserting `get board.state_map.Planned` returns the value written under
  `planned` and `--unset board.state_map.Planned` removes it.

- [test contract] t-3 has no assertion on the `--status` flag help, which is the one place
  the stale vocabulary survives the rename. `internal/cmd/issue.go:408` currently reads
  `"lifecycle status label (planning|in-progress|verifying); derived from the plan if unset"`.
  t-3's description says the help "is derived from the Set", but no contract line checks it —
  and t-9 scans prompts and Go constants, not flag usage strings, so a hand-written help that
  still says `planning` (and still omits shipped/complete) passes every test in the plan while
  advertising a value t-3 now rejects.
  Suggestion: add "the `--status` flag's usage string contains
  `configenum.LifecycleStatuses.List()` verbatim and the literal `planning` appears nowhere
  in it".

- [test contract] t-7's stats assertions cover 2 of the 5 sections `statsShow` renders.
  `internal/cmd/stats.go:40-44` calls `renderHeader`, `renderTopCommands`,
  `renderErrorBuckets`, `renderForceFlags`, `renderOutcomes`. The contract compares top-command
  counts and error buckets bucket-by-bucket, and says nothing about the header, force-flag
  tallies or outcomes. c-5 requires `--json` to emit "the same data its default rendering
  shows", so `statsSummary` could omit force flags and outcomes entirely and still pass.
  Suggestion: extend the bucket-by-bucket comparison to name all five sections, or state
  explicitly which rendered sections are intentionally excluded from the payload and why.

- [test contract] t-7's `phase show --json` can silently drop data the default rendering
  shows, with no assertion against it. Today `phaseShow` (`internal/cmd/phase.go:591-613`)
  `os.ReadFile`s the raw TOML — it displays *everything on disk*. Routing `--json` through
  `phase.LoadSpec` / `LoadPlan` narrows the output to what `phase.Spec` / `phase.Plan` model,
  so any key not in the structs (a hand-added table, a field from a newer schema) is visible
  in the text rendering and absent from the JSON. That is a direct c-5 violation ("the same
  data its default rendering shows"), and it is the only one of the nine shows where the two
  renderings read different sources.
  Suggestion: add a line asserting round-trip completeness — e.g. every top-level table and
  key present in the fixture's `spec.toml` appears in the `spec` payload — or state in the
  description that lossy narrowing to the struct is accepted, and why.

- [test contract] t-8's "empty status emits `pending`" collides with t-2's mechanical tag
  mirroring. `phase.Task.Status` is `toml:"status,omitempty"`
  (`internal/phase/phase.go:288`), so t-2 gives it `json:"status,omitempty"` and t-2's own
  contract endorses omission ("a `toml:"x,omitempty"` field left zero is absent from the
  JSON"). A bare `json.Marshal` of a task with empty status therefore omits the key, failing
  t-8's second line *and* its first ("a field present in the text rendering and absent from
  the payload fails"). Satisfiable only by populating `orPending(t.Status)` on a copy before
  marshalling — which the plan never says.
  Suggestion: say it in t-8's description: marshal a copy with `Status` set through
  `orPending`, so the text and JSON paths normalize identically.

- [wave order / antipatterns] t-7 and t-8 are the same wave and both edit
  `internal/cmd/json_show_test.go`. Four tasks write that file (t-6, t-7, t-8, t-11); t-6 and
  t-11 are serialized by waves, but t-7 and t-8 are peers in wave 3 and will collide if run in
  parallel. They are also the same shape of work at the same size (2 impl files + the shared
  test, "add `--json` to two `show` commands"), which reads as an artificial split.
  Suggestion: merge t-7 and t-8 into one wave-3 task, or give each its own test file
  (`json_show_phase_test.go` / `json_show_task_test.go`) so the wave is genuinely parallel.

- [wave order] t-5 does not need t-1's output. t-5 edits `assets/prompts/ship.md` and asserts
  only ship.md's text content; nothing in its contract touches `configenum` or Go code. The
  dependency is semantic (don't emit `shipped` before the set accepts it) rather than
  mechanical, and t-9 already gates the pairing.
  Suggestion: drop t-5 to wave 1 for parallelism, or keep the dependency and say in the
  description that it is a correctness ordering, not a build dependency.

- [granularity] Three tasks exceed the 5-file threshold: t-1 (6 files spanning
  `internal/configenum`, `internal/cmd`, `internal/forge`), t-2 (7 files across six schema
  packages), t-6 (7 files). t-2 and t-6 are mechanically uniform and a split would mostly add
  bookkeeping, but t-1 mixes two concerns that fail differently — introducing
  `LifecycleStatuses` (new leaf-package API, no behaviour change) and re-pointing three
  consumer sites at it (behaviour change, the actual c-1 fix).
  Suggestion: consider splitting t-1 into "add `configenum.LifecycleStatuses`" and "rename
  the producer and key both forge maps off the Set"; leave t-2 and t-6 whole.

- [test contract] t-11 lists `README.md` among its files with no contract line covering it.
  The doc half of the task is unverified, while t-5 shows the plan already knows how to assert
  markdown content by grep and index.
  Suggestion: add a line asserting each of the nine command-table rows (`dross project`,
  `dross milestone`, … `dross defaults`) mentions `--json`, so the README claim rots loudly.

## NOTE

- [test contract] t-3's description says only that `--status` is "validated against
  `configenum.LifecycleStatuses`", but its third line ("`--status " Verifying"` … syncs as
  `verifying`") requires the normalized value to be **assigned back** — `syncPhase` passes
  `status` raw to `statusLabel` and to both `SetState` lookups, so validate-without-reassign
  reproduces the unmapped-state warning this phase exists to kill. The contract pins it; the
  description should say it.

- [coverage] Five of c-7's six friction cases are already fixed on disk — `task list`
  (`internal/cmd/task.go:42`), the curated `task done` hint (`internal/cmd/hints.go:32`),
  `milestone set milestone.status` with `MilestoneStatuses` validation
  (`internal/cmd/milestone.go:515-519`), `RepairHint` on `IncompleteRootError`
  (`internal/cmd/root.go:39-42`), and doctor's `BoardProviders` accept-set
  (`internal/cmd/doctor.go:141`). Only the unmapped board state is new work (via t-1). t-10 is
  therefore honest in calling itself test-only, and c-7 is satisfied by regression pinning
  rather than fixes — worth knowing when reading the verify report.

- [strengths] The contracts are written as mutation assertions rather than existence checks:
  "retyping `statusVerifying` as `verify` fails the membership assertion", "deleting the
  `shipped` key fails with …", "adding `phase.Task.Retries` with only a toml tag fails it,
  naming the struct and field", "swapping the two fails the index comparison, not just a
  substring check". That is the style that survives a mutation run instead of decorating it.

- [strengths] The plan's factual claims about the repo hold up under checking: the 189 toml
  tags are exactly 96+10+9+7+37+30 across the six named files; twelve `show` subcommands do
  exist against c-5's nine, with `state` already flagged and `rule`/`interaction` the
  residue; `changes.go:93` really is the existing `json.MarshalIndent`; and
  `internal/cmd/enum_divergence_test.go` really is the go/parser precedent t-9 cites. Nothing
  was authored from memory.

- [strengths] t-4's fourth contract line ("`--unset` still deletes an existing bad key … and
  `get` still reads it — otherwise doctor reports a fault the CLI cannot repair") anticipates
  the exact trap a naive reject-bad-keys change sets, and it doubles as the repair path for
  the `planning` → `planned` rename's fallout in existing project.toml files. That reasoning
  is what the FLAG above asks you to extend to case normalization.

## Summary

Structurally sound — full criterion coverage, no locked-decision conflicts, no rule
violations, and unusually well-grounded in the actual code — but t-9's emit-set definition
contradicts its own key assertion and would ship c-2's central test as a tautology, so fix
that before executing.
