# board-state-map-truth — verification lens

Every task below was derived by writing the criterion's failing test first, then
asking what the smallest change is that makes that test green.

```
Phase board-state-map-truth — 11 tasks across 4 waves

Wave 1
  t-1  Define lifecycle set, rename planning to planned
       files:    internal/configenum/configenum.go, internal/cmd/issue.go,
                 internal/forge/jira.go, internal/forge/youtrack.go,
                 internal/cmd/issue_test.go, internal/forge/jira_test.go
       covers:   c-1
       desc:     Add configenum.BoardStatuses (planned, in-progress, verifying,
                 shipped, complete). statusPlanning's value becomes "planned";
                 issue.go's constants and both forge default maps key off the Set.
       contract: - phase-sync on a phase whose plan.toml has every task
                   status=pending records a POST to /issue/<key>/transitions on
                   the mock Jira server; if derivePhaseStatus emits anything but
                   "planned", zero transition requests are recorded and the test
                   fails.
                 - the same run's captured stderr contains no
                   "has no Jira status mapping" warning.
                 - resolveJiraState("planned", nil) returns ("To Do", true) and
                   resolveJiraState("planning", nil) returns ok=false;
                   resolveYouTrackState("planned", nil) returns ("Open", true).
                 - every lifecycle literal declared in issue.go is a
                   configenum.BoardStatuses member — retyping statusVerifying as
                   "verify" fails the membership assertion.

  t-2  Mirror toml tags as json tags across schemas
       files:    internal/project/project.go, internal/milestone/milestone.go,
                 internal/defaults/defaults.go, internal/profile/profile.go,
                 internal/stack/profile.go, internal/phase/phase.go,
                 internal/cmd/json_tag_parity_test.go
       covers:   c-5
       desc:     Every struct field carrying `toml:"x"` gains `json:"x"` with the
                 same name and omitempty. Without this, --json emits Go field
                 names (TestContract) where the document says test_contract.
       contract: - a reflection walk over project.Project, milestone.Milestone,
                   defaults.Defaults, profile.Profile, stack.Profile, phase.Spec,
                   phase.Plan and phase.Task asserts each toml-tagged field has an
                   identical json tag; adding phase.Task.Retries with only a toml
                   tag fails it, naming the struct and field.
                 - json.Marshal(phase.Task{TestContract: []string{"x"}}) contains
                   the key "test_contract" and not "TestContract".

Wave 2 (depends t-1 / t-2)
  t-3  Reject out-of-set --status on phase-sync
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       covers:   c-3
       desc:     issuePhaseSync validates --status against configenum.BoardStatuses
                 in RunE, before openBoard, and the flag help names the Set.
       depends:  t-1
       contract: - `dross issue phase-sync 01-auth --status planning` returns an
                   error whose message contains
                   "planned | in-progress | verifying | shipped | complete", and
                   the mock board server records zero HTTP requests.
                 - the same rejection fires with [board].enabled = false: the
                   check runs ahead of the enabled short-circuit, so a bad
                   --status can never exit 0 as a silent no-op.
                 - `--status shipped` and `--status complete` return nil — the
                   two statuses t-5 starts emitting must not be rejected.

  t-4  Gate state_map keys in project set and doctor
       files:    internal/cmd/project.go, internal/cmd/doctor.go,
                 internal/cmd/project_test.go, internal/cmd/doctor_test.go
       covers:   c-4
       desc:     writeDotted's stateMapKey branch rejects a key outside
                 BoardStatuses; doctor's [board] block reports an on-disk bad key
                 as an issue (counts toward the non-zero exit, per
                 state_map_key_severity).
       depends:  t-1
       contract: - `dross project set board.state_map.planning "To Do"` errors
                   naming the five valid keys, and project.toml is byte-identical
                   to its pre-call contents.
                 - `dross project set board.state_map.verifying "In Review"` still
                   writes the entry.
                 - doctor over a project.toml carrying
                   `[board.state_map] planning = "To Do"` prints a ✗ line naming
                   board.state_map.planning and returns a non-nil error; the same
                   fixture with only valid keys returns nil.
                 - `dross project set --unset board.state_map.planning` still
                   deletes an existing bad key — the write gate must not trap a
                   repo that already has one on disk.

  t-5  Emit terminal board statuses from ship
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-6
       desc:     ship.md §6 emits `--status shipped` on merge and
                 `--status complete --close` after finalize, replacing the bare
                 `--close`. `dross phase complete` gains no board call.
       depends:  t-1
       contract: - ship.md contains "phase-sync <phase-id> --status shipped" at an
                   index after the squash-merge bullet and before
                   "dross phase complete"; "--status complete --close" appears
                   after "dross phase complete". Swapping the two fails the index
                   comparison, not just a substring check.
                 - ship.md contains no `phase-sync <phase-id> --close` occurrence
                   lacking a --status.
                 - no line in ship.md pairs `dross phase complete` with a
                   `phase-sync` call (locked terminal_emit_sites: phase complete
                   stays board-unaware).

  t-6  Add --json helper and config-document shows
       files:    internal/cmd/jsonout.go, internal/cmd/project.go,
                 internal/cmd/milestone.go, internal/cmd/defaults.go,
                 internal/cmd/profile.go, internal/cmd/stack.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       desc:     New emitJSON(v any) helper marshals the bare document. project,
                 milestone, defaults, profile and stack `show` gain --json, which
                 suppresses the `# <path>` header line (locked json_shape).
       depends:  t-2
       contract: - `dross project show --json` output's first non-space byte is
                   `{`, contains no line starting with `#`, and unmarshals to an
                   object whose project.name equals what the toml encoder printed.
                 - `dross milestone show v1.1 --json` and `dross defaults show
                   --json` likewise emit no `#` header line.
                 - `dross profile show --scope global --json` emits the global
                   profile, not the merged one — --scope and --json compose.
                 - `dross stack show nope --json` still fails with
                   `stack profile "nope" not found` instead of printing "null".

Wave 3 (depends wave 2)
  t-7  Add --json to phase and stats shows
       files:    internal/cmd/phase.go, internal/cmd/stats.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       desc:     phase show --json emits {"spec":…,"plan":…} via phase.LoadSpec /
                 LoadPlan, null for a missing file. stats show's aggregation moves
                 into a statsSummary struct the renderers read, so --json emits
                 the same numbers the table prints.
       depends:  t-6
       contract: - `dross phase show 01-auth --json` unmarshals to an object with
                   exactly the keys "spec" and "plan"; with plan.toml deleted,
                   "plan" is JSON null and "spec" is still populated (today the
                   command prints a "— (missing)" comment line instead).
                 - `dross stats show --json` carries the same top-command counts
                   and error buckets the table renders, compared bucket by bucket
                   against the parsed table output — dropping a bucket from the
                   payload while it still renders fails.
                 - `dross stats show --since 7d --json` omits an event stamped 30
                   days ago that `--since` filters out of the table.

  t-8  Add --json to task and changes shows
       files:    internal/cmd/task.go, internal/cmd/changes.go,
                 internal/cmd/json_show_test.go
       covers:   c-5
       desc:     task show --json marshals the phase.Task record; changes show
                 accepts --json for symmetry (it already emits JSON), matching
                 `state show --json`.
       depends:  t-6
       contract: - `dross task show 01-auth t-1 --json` unmarshals to an object
                   carrying every field the aligned rendering prints — id, title,
                   wave, status, files, covers, depends_on, test_contract,
                   description — each compared against the plan.toml task; a field
                   present in the text rendering and absent from the payload fails.
                 - task show --json on a task with empty status emits "pending",
                   matching orPending in the text path rather than "".
                 - `dross changes show 01-auth --json` is byte-identical to
                   `dross changes show 01-auth`.

  t-9  Pin lifecycle/state-map bidirectional divergence
       files:    internal/cmd/board_lifecycle_divergence_test.go,
                 internal/forge/jira.go, internal/forge/youtrack.go
       covers:   c-2
       desc:     Export the two provider default maps (or a DefaultStateMap
                 accessor) and add the two-direction gate: emitted statuses vs
                 state-map keys.
       depends:  t-1, t-5
       contract: - the test collects the statuses dross emits — every
                   `--status <literal>` in assets/prompts/*.md plus issue.go's
                   lifecycle constants — and asserts the set equals
                   configenum.BoardStatuses; adding `--status blocked` to
                   execute.md fails it, naming the prompt file.
                 - the test asserts key-set equality in both directions against
                   defaultJiraStateMap and defaultYouTrackStateMap: deleting the
                   "shipped" key from the Jira map fails ("status dross emits has
                   no state-map entry"), and adding a "paused" key fails ("state
                   map keys on a status nothing emits").
                 - reverting t-5's ship.md edit fails this test, because shipped
                   and complete stop being emitted while both maps still key them.

  t-10 Pin the five telemetry friction cases
       files:    internal/cmd/friction_window_test.go
       covers:   c-7
       desc:     One test per friction case recorded 2026-07-15..2026-07-28, each
                 asserting the command now succeeds or names the working
                 alternative.
       depends:  t-1, t-3, t-4
       contract: - incomplete root: with .dross/state.json removed, `dross task
                   list` fails with a message containing RepairHint, not a bare
                   stat/parse error.
                 - `dross task list` on a phase with a plan exits nil and prints
                   the task table — it must never reach the unknown-subcommand
                   path that produced the telemetry.
                 - `dross task done 01-auth t-1` errors with a message containing
                   "dross task status <phase-id> <task-id> done".
                 - `dross milestone set v1.1 milestone.status active` succeeds and
                   writes status = "active"; `milestone.status shipped` errors
                   naming "planning | active | complete".
                 - doctor with [board].provider = jira, auth_env set and exported,
                   and a valid base_url prints "✓ [board] is well-formed" and adds
                   zero board issues.
                 - unmapped board state: phase-sync on a locked-but-unstarted plan
                   against the mock Jira server records a transition request and
                   prints no "no Jira status mapping" warning.

Wave 4 (depends wave 3)
  t-11 Gate every structured show for --json
       files:    internal/cmd/json_show_test.go, README.md
       covers:   c-5
       desc:     Walk the built command tree for every `show` subcommand and
                 require a registered `json` bool flag, with a documented exempt
                 list (rule show, interaction show). README's command-table rows
                 for the nine commands mention --json.
       depends:  t-6, t-7, t-8
       contract: - the tree walk fails when a `show` subcommand has no `json`
                   flag and is not in the exempt list — deleting the flag
                   registration from stack show fails it by name, and a future
                   `dross findings show` landing without --json fails it too.
                 - for each of the nine commands the walk finds, invoking it with
                   --json against the fixture repo produces output that
                   json.Valid accepts (catches a flag registered but not wired).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-9 |
| c-3 | t-3 |
| c-4 | t-4 |
| c-5 | t-2, t-6, t-7, t-8, t-11 |
| c-6 | t-5 |
| c-7 | t-10 |

7/7 criteria covered.

## Judgment calls

- **json tags get their own wave-1 task (t-2).** Chosen over folding tag edits
  into each --json task: none of project/milestone/defaults/profile/stack/phase
  carry a `json:` tag today (verified by grep), so without it `--json` emits
  `TestContract` where the document says `test_contract` and c-5's "the same data
  its default rendering shows" is false. Rejected doing it per-command because
  the parity gate only works as one reflection walk over all the schemas.
- **c-5 split into four impl tasks by payload shape, not by file count.**
  Document-marshal shows (t-6) share one helper and one contract; phase and stats
  (t-7) assemble new payloads — stats needs its aggregation lifted out of the
  renderers, which is the only non-mechanical work in c-5; task and changes (t-8)
  are the small ones. Rejected one big nine-command task (five layers of
  different payload logic in one commit) and rejected nine tiny tasks.
- **t-6 lists six source files, above the granularity heuristic.** Accepted
  deliberately: the edit at each of the five command sites is the same four
  lines against one new helper, and splitting it would serialize a wave to share
  `jsonout.go` for no isolation benefit.
- **The divergence gate (t-9) waits for the ship prompt (t-5).** Chosen over
  running it in wave 2: `shipped` and `complete` only become emitted statuses
  once ship.md names them, so the "map key nothing emits" direction would fail on
  a correct tree. This dependency is exactly what makes the `dead_map_keys`
  decision satisfiable, so t-9 also acts as the regression guard on t-5.
- **The emitted-status set is scraped from the prompts, not just from Go.**
  Rejected asserting only over `configenum.BoardStatuses` and the forge maps: that
  version passes while `assets/prompts/execute.md` says `--status in-flight`,
  which is the real shape of the bug — the emitter is a markdown file.
- **`--status` validation runs before `openBoard` (t-3).** Rejected validating
  inside `syncPhase`: with board sync disabled, phase-sync returns nil early, so
  a typo'd status would keep exiting 0 in exactly the repos that later enable the
  board.
- **doctor gets the state_map check as an issue, not a warning.** Not a judgment
  call — locked `state_map_key_severity` — but it drives t-4's contract to assert
  the non-zero exit rather than just the printed line.
- **t-11 derives the show list from the command tree.** Rejected a hand-written
  table of nine names: a hand-written table cannot fail when a tenth structured
  show lands, which is the only thing the gate exists to catch.
