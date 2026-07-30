# risk lens — board-state-map-truth

Phase board-state-map-truth — 12 tasks across 3 waves

Wave 1

  t-1  Rename planning to planned, add lifecycle set
       files:    internal/configenum/configenum.go, internal/configenum/configenum_test.go,
                 internal/cmd/issue.go, internal/cmd/issue_test.go
       covers:   c-1
       desc:     Add `configenum.LifecycleStatuses` (planned | in-progress | verifying |
                 shipped | complete, no empty default). Rename the `statusPlanning`
                 constant's value to "planned" and re-derive the `--status` flag help
                 from the Set.
       contract: - phase-sync against the stub Jira board, on a phase whose plan.toml
                   exists with every task pending, POSTs a transition and writes nothing
                   matching "has no Jira status mapping" to stderr; reverting the constant
                   to "planning" fails it.
                 - the label PUT carries `dross/status:planned`, not `dross/status:planning`.
                 - derivePhaseStatus(nil) and derivePhaseStatus(all-pending plan) both
                   return a value LifecycleStatuses.Has accepts.
                 - LifecycleStatuses.Has("planning") is false and Has("") is false; a
                   Set with an accidental default would fail the empty case.

  t-2  Emit shipped then complete from ship
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-6
       desc:     In §6, insert `dross issue phase-sync <phase-id> --status shipped` between
                 the squash-merge and `dross phase complete`, and replace the bare
                 `--close` step with `--status complete --close` after finalize.
       contract: - the test asserts both literals are present AND that the byte offset of
                   `--status shipped` is less than that of `dross phase complete`, which
                   is less than that of `--status complete --close`; swapping the two sync
                   calls fails it.
                 - no `phase-sync <phase-id> --close` without a `--status` survives in
                   ship.md — the pattern that produced the dead terminal map entries.

  t-3  Add toml-tag-keyed JSON marshaller
       files:    internal/tomljson/tomljson.go, internal/tomljson/tomljson_test.go
       covers:   c-5
       desc:     New leaf package: `Marshal(v any) ([]byte, error)` renders a struct to
                 JSON using its `toml` tag names and omitempty semantics, so `--json` needs
                 no second tag set on the 190 toml-tagged fields in project/phase/milestone/
                 stack/profile/defaults.
       contract: - for a fully-populated project.Project, the key set Marshal emits at every
                   level equals the key set BurntSushi's toml encoder writes — `state_map`,
                   `git_main_branch`, `auth_env`, never `StateMap`/`GitMainBranch`.
                 - a toml:"x,omitempty" field left zero is absent from the JSON, and a
                   non-omitempty zero field is present; a nil []string emits [] not null.
                 - an unexported or toml:"-" field never appears.

  t-4  Add --json to stats show
       files:    internal/cmd/stats.go, internal/cmd/stats_test.go
       covers:   c-5
       desc:     Extract the five render* sections into one aggregate struct with json tags;
                 the renderers read it, `--json` marshals it. No tomljson dependency — this
                 shape is computed, not on disk.
       contract: - on a fixture telemetry file, the JSON's top_commands[0] name+count equal
                   the first row the default rendering prints, and error_buckets carries the
                   same `other` detail tail strings the text section lists.
                 - `stats show --json --since 7d` filters identically to the text path: an
                   event older than the cutoff appears in neither.
                 - the empty-telemetry case emits an empty-but-valid JSON document, not the
                   "(no telemetry events at …)" sentence.

Wave 2 (depends on wave 1)

  t-5  Validate --status against the lifecycle set
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       depends:  t-1
       covers:   c-3
       desc:     syncPhase's caller rejects a non-member `--status` before openBoard runs,
                 with a message interpolating LifecycleStatuses.List().
       contract: - `issue phase-sync 01-x --status done` exits non-zero, the message contains
                   "planned | in-progress | verifying | shipped | complete", and the stub
                   board server records zero requests — rejection happens before any network
                   or board.json write.
                 - `--status " Verifying"` is accepted and syncs as `verifying` (validation
                   goes through configenum.Normalize, so it is exactly as forgiving as the
                   map lookup).
                 - omitting `--status` still derives from the plan and does not error.

  t-6  Refuse and report dead state_map keys
       files:    internal/cmd/project.go, internal/cmd/project_test.go,
                 internal/cmd/doctor.go, internal/cmd/doctor_test.go
       depends:  t-1
       covers:   c-4
       desc:     writeDotted rejects a `board.state_map.<key>` whose key is not a lifecycle
                 status (storing the normalized form when it is); doctor's Board section
                 walks the on-disk map and reports each bad key as an issue.
       contract: - `project set board.state_map.done X` exits non-zero naming the five valid
                   keys, and project.toml is byte-identical afterwards (reject-before-write).
                 - `project set board.state_map.shipped Fixed` still writes, and
                   `board.state_map.Planned` stores under the key `planned` so the map lookup
                   at sync time can find it.
                 - `project set --unset board.state_map.done` still deletes an
                   already-on-disk bad key and `project get board.state_map.done` still reads
                   it — otherwise doctor reports a fault the CLI cannot repair.
                 - doctor on a project.toml carrying `[board.state_map] done = "X"` prints a
                   ✗ line naming `done`, the run exits non-zero, and the warning tally is
                   unchanged (locked state_map_key_severity).

  t-7  Guard lifecycle vocabulary against the maps
       files:    internal/cmd/lifecycle_divergence_test.go
       depends:  t-1, t-2
       covers:   c-2
       desc:     Test-only. Parses defaultJiraStateMap and defaultYouTrackStateMap out of
                 internal/forge source (the enum_divergence_test.go AST pattern) and scans
                 assets/prompts/*.md for `--status <literal>`, comparing both against
                 configenum.LifecycleStatuses.
       contract: - deleting "complete" from defaultJiraStateMap fails the test naming the
                   emitted-but-unmapped status.
                 - adding "planning" back to defaultYouTrackStateMap fails it naming the
                   mapped-but-never-emitted key.
                 - changing verify.md to `--status verify` fails it naming the prompt file
                   and the literal.
                 - the two provider maps are checked independently, so a key present in one
                   and missing from the other is caught rather than averaged away.

  t-8  Add --json to project and milestone show
       files:    internal/cmd/project.go, internal/cmd/milestone.go
       depends:  t-3
       covers:   c-5
       desc:     Both grow a `--json` bool; under it the `# <path>` header is suppressed and
                 the doc is emitted via tomljson.Marshal.
       contract: - `project show --json` stdout parses as JSON, contains no line starting
                   `# `, and its repo.git_main_branch equals the value the default TOML
                   rendering prints for the same fixture.
                 - `milestone show --json` with no argument and no state.current_milestone
                   still fails with the existing "no version given …" message, not an empty
                   JSON doc.
                 - `milestone show --json 1.1` and `milestone show 1.1` name the same file.

  t-9  Add --json to stack, defaults, profile show
       files:    internal/cmd/stack.go, internal/cmd/defaults.go, internal/cmd/profile.go
       depends:  t-3
       covers:   c-5
       desc:     Same treatment for the three remaining TOML-rendering shows; `profile show
                 --json` honours --scope.
       contract: - `stack show go --json` parses as JSON and carries the same profile id the
                   TOML rendering shows; `stack show nope --json` still errors with
                   "stack profile \"nope\" not found" and prints no JSON.
                 - `defaults show --json` emits no `# <path>` header line.
                 - `profile show --json --scope global` and `--scope project` emit different
                   documents, and `--scope bogus --json` still errors before emitting.

  t-10 Add --json to phase, task, changes show
       files:    internal/cmd/phase.go, internal/cmd/task.go, internal/cmd/changes.go
       depends:  t-3
       covers:   c-5
       desc:     `phase show --json` emits `{"spec":…,"plan":…}` (locked json_shape), null
                 for a missing file, and errors when the phase directory itself is absent;
                 `task show --json` emits the task record; `changes show` accepts `--json`
                 with byte-identical output (it is already JSON).
       contract: - `phase show <id> --json` on a phase with spec.toml but no plan.toml emits
                   `"plan": null` and a non-null spec — not an omitted key and not `{}`.
                 - `phase show does-not-exist` errors naming the id in BOTH modes; today it
                   prints two "(missing)" lines and exits 0, which under --json would be an
                   indistinguishable `{"spec":null,"plan":null}`.
                 - `task show <phase> t-99 --json` still errors "task not found: t-99" and
                   prints no JSON.
                 - `changes show <id> --json` output is byte-identical to `changes show <id>`.

Wave 3 (depends on wave 2)

  t-11 Guard every show command carries --json
       files:    internal/cmd/json_surface_test.go
       depends:  t-4, t-8, t-9, t-10
       covers:   c-5
       desc:     Test-only. Walks the built command tree for every command named `show`,
                 asserts each declares a `json` flag, and for the fixture-backed ones asserts
                 the captured stdout parses as JSON and carries no `# ` header line.
       contract: - adding a tenth `show` subcommand without a `--json` flag fails this test
                   naming that command's path.
                 - a command that emits JSON but keeps the `# <path>` header fails the
                   header assertion (locked json_shape: bare document, no envelope).
                 - `state show --json` and `changes show --json` are included, so the
                   already-JSON pair cannot regress to rejecting the flag.

  t-12 Pin the five friction cases with tests
       files:    internal/cmd/friction_pins_test.go
       depends:  t-1, t-5, t-6
       covers:   c-7
       desc:     Test-only. One subtest per friction case in the 2026-07-15..2026-07-28
                 telemetry window, each asserting the message a user now gets.
       contract: - incomplete root: `project show` in a .dross missing state.json fails with
                   a message containing ".dross/state.json" and cmd.RepairHint.
                 - `task list <phase>` succeeds and lists the phase's tasks (the subcommand
                   the unknown_subcommand events reached for now exists).
                 - `task done` fails with a message containing
                   "dross task status <phase-id> <task-id> done" from the curated hint table.
                 - `milestone set milestone.status active` writes, and an invalid value
                   fails naming "planning | active | complete" — no unknown_field error.
                 - doctor accepts every member of configenum.BoardProviders as
                   [board].provider without a ✗ line for the provider field.
                 - the unmapped board state: phase-sync on a locked-but-unstarted plan
                   transitions the stub board and emits no mapping warning.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-7 (guard), enabled by t-1 + t-2 |
| c-3 | t-5 |
| c-4 | t-6 |
| c-5 | t-3, t-4, t-8, t-9, t-10, t-11 |
| c-6 | t-2 |
| c-7 | t-12 |

Every criterion has an owning task, and every task has exactly one primary failure
mode: t-1 unmapped-emit, t-2 dead-terminal-entries, t-5 unvalidated CLI input,
t-6 unvalidated on-disk config, t-7 vocabulary drift, t-3/t-4/t-8/t-9/t-10 shape
drift per surface group, t-11 future-surface drift, t-12 regression of the fixed
friction set.

## Judgment calls

- **A toml-tag-keyed marshaller (t-3) over hand-adding json tags.** The alternative is
  190 json tags across six schema files; every future toml field would silently emit a
  Go field name until someone noticed. One encoder plus a key-set-equality test removes
  that drift surface by construction. Cost: a hand-rolled reflection encoder has its own
  edge cases (nil slices, nested maps, omitempty), which is why t-3's contract pins them
  explicitly rather than testing "it marshals".
- **`stats show --json` (t-4) is wave 1, not wave 2.** Its data is computed, not read off
  disk, so it never touches tomljson — putting it in wave 2 would be false serialisation.
- **The c-2 guard reads the forge maps by AST (t-7), not by exporting them.** Exporting
  `defaultJiraStateMap` to make it testable widens the forge API for a test's benefit; the
  repo already parses source for exactly this class of guard in
  internal/cmd/enum_divergence_test.go, so the precedent costs nothing new.
- **The guard also scans `assets/prompts/*.md`.** The `--status` literals in execute.md and
  verify.md are emit sites that no Go test can see, and rule r-01 means a prompt can drift
  for a whole release before anyone runs `make install`. Leaving them out would make c-2's
  "a status dross emits" a half-truth.
- **t-6 keeps `--unset` and `get` permissive on a rejected key.** Symmetric rejection is
  tidier but strands anyone whose project.toml already carries a bad key: doctor would
  report a fault with no CLI repair, forcing a hand-edit — the exact friction c-7 is about.
- **t-6 normalises the key on write rather than rejecting mixed case.** `board.state_map.Planned`
  is a near-miss that would store a key the sync-time lookup never finds; storing `planned`
  fixes the intent instead of failing it, and matches how configenum.Normalize already
  reconciles validator and dispatch elsewhere.
- **t-10 makes `phase show <unknown-id>` an error in both modes.** Keeping the current exit-0
  "(missing)" rendering would make `--json` emit `{"spec":null,"plan":null}` for a typo, which
  is indistinguishable from a real phase with no artefacts — and c-5 requires the two modes to
  show the same data, so a --json-only error would break that too. It is a small behaviour
  change to a non-locked surface, flagged here because it is not literally in a criterion.
- **--json split three ways (t-8/t-9/t-10) rather than one task over nine files.** They are
  three different risk shapes: t-8/t-9 are header-drop plus shape, t-10 is the missing-file
  and not-found edges. Merging them would hide t-10's edges inside a mechanical sweep.
- **t-5 and t-1 stay separate despite both editing issue.go.** The vocabulary fix and the
  input-validation gate are different failure modes, and sequencing them across waves avoids
  two tasks editing issue.go concurrently. Same-file overlap does remain between t-6 and t-8
  (internal/cmd/project.go) inside wave 2 — acceptable because dross executes tasks
  sequentially with one commit each, and splitting the wave further would misrepresent the
  dependency graph.
