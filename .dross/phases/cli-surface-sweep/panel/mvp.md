# MVP lens — cli-surface-sweep

Phase cli-surface-sweep — 7 tasks across 2 waves

Wave 1

  t-1  Add `dross task list` with --json
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1
       desc:     New `task list [phase-id]` subcommand under Task(). Aligned
                 ID/WAVE/STATUS/TITLE table by default, `--json` emits an array
                 of task objects. Omitted phase-id falls back to
                 state.current_phase (mirrors `milestone show`'s loadMilestone
                 fallback).
       contract: - `dross task list` with no arg and state.current_phase set prints that
                   phase's rows; if the state fallback is dropped the no-arg test fails
                   with an args/`no phase given` error instead of a table
                 - a task whose status field is empty renders `pending` in the STATUS
                   column; dropping orPending from the row renderer leaves it blank and
                   the test fails
                 - `dross task list --json` stdout unmarshals into a slice whose entries
                   carry id, wave, status and title; a table-only regression fails the
                   json.Unmarshal assertion

  t-2  Add curated + distance invocation hints
       files:    internal/cmd/hint.go, internal/cmd/hint_test.go,
                 internal/cmd/subcommand_guard.go, internal/cmd/subcommand_guard_test.go
       covers:   c-2, c-8
       desc:     New hint table keyed by (command path, typed token) → working
                 invocation. EnforceSubcommandKnown consults it in its
                 unknown-subcommand RunE and also installs a root
                 FlagErrorFunc (cobra inherits it down the tree) that consults
                 it for unknown flags. Fallback when the table misses:
                 string-distance over sibling subcommand names and over the
                 command's own flag set.
       contract: - `dross task done t-1` errors with a message containing
                   `dross task status <phase-id> <task-id> done`; deleting that table row
                   drops the string and the test fails
                 - `dross phase create --title X` errors naming `dross phase create <title>`
                   and `dross security run --new` names `dross security run`; if the
                   FlagErrorFunc is set on the leaf instead of the root both return bare
                   `unknown flag` and fail
                 - `dross task edit --files a.go` names a working invocation rather than
                   printing only `unknown flag: --files`
                 - with no table entry, `dross phse list` names `phase` and
                   `dross deferred list --targt x` names `--target`; removing the
                   distance fallback fails both

  t-3  Add multi-get renderer, `state get`, `state show --json`
       files:    internal/cmd/dotted.go, internal/cmd/dotted_test.go,
                 internal/cmd/state.go, internal/cmd/state_test.go
       covers:   c-3, c-4
       desc:     New renderMultiGet(paths, lookup) in dotted.go: one path prints
                 the bare value unchanged, two or more emit a single keyed JSON
                 object (locked multi_get_shape). New `dross state get <path>...`
                 over state.json's fields. `state show` gains an accepted
                 `--json` flag that prints state.json as it already does.
       contract: - `dross state get current_phase` prints the bare id with no braces or
                   quotes; losing the single-path shortcut fails the exact-stdout assert
                 - `dross state get version current_phase` unmarshals into a two-key JSON
                   object whose keys are the requested paths verbatim
                 - `dross state get nope` errors naming the unknown field instead of
                   emitting `{}` or an empty line
                 - `dross state show --json` exits 0 and its stdout unmarshals as
                   state.json; before the flag it fails with `unknown flag: --json`

  t-5  Resolve bare milestone field, validate status
       files:    internal/configenum/configenum.go, internal/configenum/configenum_test.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-5
       desc:     Add configenum.MilestoneStatuses (planning | active | complete,
                 no empty default). `milestone set` expands an unambiguous bare
                 field name to its dotted path before writeMilestoneDotted, and
                 rejects an out-of-set value for milestone.status before Save.
       contract: - `dross milestone set status active` writes milestone.status and
                   `dross milestone get milestone.status` reads back `active`; without bare-name
                   expansion it fails with `unknown or unsettable milestone field: status`
                 - `dross milestone set status shipped` errors naming
                   `planning | active | complete` and the milestone toml is byte-identical
                   on disk afterwards
                 - `dross milestone set foo x` still errors — expansion resolves known bare
                   names only, it does not fall through to a nearest guess
                 - configenum.MilestoneStatuses.Has("") is false — an unset milestone
                   status has no code default and must not pass

  t-6  Complete [board] set paths and add --unset
       files:    internal/cmd/project.go, internal/cmd/project_test.go
       covers:   c-6, c-9
       desc:     readDotted/writeDotted gain `board.github_project` and a
                 `board.state_map.<status>` prefix arm addressing one entry at a
                 time (locked state_map_write). `project set` gains
                 `--unset <path>`, which zeroes a scalar or deletes a single
                 state_map key.
       contract: - `dross project set board.github_project PVT_x` then
                   `dross project get board.github_project` round-trips; before the arm
                   exists set returns `unknown or unsettable field`
                 - `dross project set board.state_map.in_progress 'In Progress'` on a
                   project.toml that already has board.state_map.done leaves the done
                   entry intact — a whole-map assignment fails this
                 - `dross project set --unset board.state_map.done` removes only that key
                   and in_progress survives; `--unset board.github_project` empties the
                   scalar so the key is absent from the re-encoded project.toml
                 - a reflect-over-toml-tags test asserts every field of project.Board
                   resolves through both readDotted and writeDotted; adding a Board field
                   without a set arm fails it

  t-7  Doctor flags unrecognised task status
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-7
       desc:     Doctor walks .dross/phases/*/plan.toml and reports every task
                 whose status is outside pending|in_progress|done|failed, naming
                 the phase id and task id, counting each as an issue (non-zero
                 exit). Lives in doctor, not validate (locked status_check_home).
       contract: - a plan.toml carrying `status = "blocked"` makes `dross doctor` print both
                   the phase id and the task id and exit non-zero; a task with an empty
                   status stays silent because empty means pending
                 - a repo whose phases dir has no plan.toml emits no task-status section
                   and doctor's exit code is unchanged
                 - `dross validate` on that same blocked-status repo still passes — the
                   check must not be duplicated into the structural validator

Wave 2 (depends t-3)

  t-4  Wire project/milestone get to multi-get
       files:    internal/cmd/project.go, internal/cmd/project_test.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-4
       desc:     `project get` and `milestone get` take 1+ dotted paths and
                 render through renderMultiGet. `milestone get` keeps its
                 optional leading version by treating args[0] as a version only
                 when it is not itself a known milestone field.
       depends:  t-3
       contract: - `dross project get project.name` prints the bare name byte-identically to
                   today (no quotes, no object); `dross project get project.name
                   repo.git_main_branch` emits a two-key JSON object
                 - `dross milestone get v1.1 milestone.status phases` resolves v1.1 as the
                   version and returns two keys, while `dross milestone get milestone.status
                   phases` falls back to state.current_milestone — treating args[0] as a
                   version unconditionally fails the second case
                 - a list-valued path (scope.success_criteria) prints one entry per line
                   in single-path mode and appears as a JSON array in multi-path mode
                 - an unknown path among several still errors naming that path rather
                   than emitting an object with the good keys only

## Coverage

| criterion | tasks |
|---|---|
| c-1 `task list` | t-1 |
| c-2 curated mis-reach hints | t-2 |
| c-3 `state show --json` | t-3 |
| c-4 multi-path get (project/milestone/state) | t-3, t-4 |
| c-5 bare milestone field + status enum | t-5 |
| c-6 every [board] field settable | t-6 |
| c-7 doctor unrecognised task status | t-7 |
| c-8 distance fallback for typos | t-2 |
| c-9 `project set --unset` | t-6 |

9/9 criteria covered.

## Judgment calls

- Split multi-get into t-3 (renderer + state) and t-4 (project + milestone) rather than one task: the single version was 7 files across three command surfaces. The split falls exactly on the one real dependency edge — the shared renderer — so nothing artificial was invented.
- Installed the FlagErrorFunc inside `EnforceSubcommandKnown` rather than editing `cmd/dross/main.go`: main.go already calls it on the root, cobra inherits FlagErrorFunc down the tree, and the whole hint layer then stays testable inside internal/cmd with no new wiring seam.
- Kept c-2 and c-8 in one task: the locked `hint_mechanism` defines table-then-distance as a single chain. Splitting them ships a half-wired error path where the fallback exists but nothing routes to it.
- Handled `task edit --files` as a curated hint rather than adding a real `--files` flag to `task edit`: c-2 asks for an error naming the working invocation, not for the flag to work. Adding the flag is a separate feature no criterion requests.
- Put MilestoneStatuses in configenum rather than a literal switch in milestone.go: configenum is the repo's declared single home for enumerated config values and `enum_divergence_test.go` guards drift. Five lines there beats a fifth private list.
- Disambiguated `milestone get [version] <path>...` by "is args[0] a known milestone field": rejected a `--version` flag (breaks the existing `milestone get v1.1 x` form callers already use) and rejected a positional-count rule (undecidable once paths are variadic).
- No README / prompt-doc task: no criterion mentions docs, and nothing gates it — `readme_doc_test.go` only checks the install and update strings, and `commands_parity_test.go` covers slash commands, not CLI subcommands. Any doc sync rides along in the task that adds the surface.
- No shared "dotted path registry" refactor: c-4 and c-6 could motivate replacing the three hand-written switch tables with one reflective registry. That is speculative structure — every criterion is satisfied by adding arms to the switches that already exist.
- Scheduled t-4 in wave 2 so no two same-wave tasks share a file: t-6 and t-4 both edit project.go, t-5 and t-4 both edit milestone.go, and the wave boundary already separates them for free.
