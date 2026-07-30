# mvp lens

Phase board-state-map-truth — 6 tasks across 2 waves

Wave 1

  t-1  Define lifecycle status set, rename planning
       files:    internal/configenum/configenum.go, internal/cmd/issue.go,
                 internal/cmd/issue_test.go
       covers:   c-1, c-3
       depends:  —
       desc:     Add `configenum.LifecycleStatuses` (planned | in-progress |
                 verifying | shipped | complete, empty rejected). In issue.go
                 rename `statusPlanning`'s value to "planned" and add
                 `statusShipped` / `statusComplete` consts; validate the
                 `--status` flag against the Set in phase-sync's RunE before
                 any board call, and update the flag's usage string.
       contract: - phase-sync against a fake YouTrack whose phase has a
                   task-less plan.toml POSTs a State customField write of
                   "Open" to /api/issues/<key>; today that request is absent
                   and stderr carries `no YouTrack State mapping`.
                 - `dross issue phase-sync p --status planing` exits non-zero
                   before any HTTP request is made, with a message containing
                   `planned | in-progress | verifying | shipped | complete`.
                 - dropping "shipped" from LifecycleStatuses makes the
                   `--status shipped` accept case fail.

  t-2  Add --json to whole-document show commands
       files:    internal/cmd/jsonshow.go, internal/cmd/project.go,
                 internal/cmd/milestone.go, internal/cmd/defaults.go,
                 internal/cmd/profile.go, internal/cmd/stack.go,
                 internal/cmd/jsonshow_test.go
       covers:   c-5 (project, milestone, defaults, profile, stack)
       depends:  —
       desc:     New jsonshow.go helper marshals the same struct the TOML
                 encoder receives and suppresses the `# <path>` header
                 (locked json_shape). Wire a `--json` bool onto the five
                 shows that already print a whole document.
       contract: - `project show --json` output parses as JSON, its first
                   byte is `{`, and `.runtime.test_command` equals what
                   `project get runtime.test_command` prints.
                 - `project show --json` contains no line starting `# ` —
                   re-adding the header line fails the test.
                 - `profile show --scope project --json` and
                   `--scope merged --json` differ, proving --json reads the
                   scope-selected document, not always the merged one.
                 - `stack show no-such-id --json` still exits non-zero with
                   `stack profile "no-such-id" not found` — --json does not
                   swallow the not-found path.

Wave 2

  t-3  Add --json to computed show commands
       files:    internal/cmd/phase.go, internal/cmd/task.go,
                 internal/cmd/changes.go, internal/cmd/stats.go,
                 internal/cmd/jsonshow_test.go
       covers:   c-5 (phase, task, changes, stats)
       depends:  t-2
       desc:     Reuse the t-2 helper. `phase show --json` emits a two-key
                 `{"spec":…,"plan":…}` document with null for a missing file
                 (locked json_shape); task show marshals the plan Task
                 struct; changes show gains the flag over its existing JSON;
                 stats show marshals the aggregate the renderers compute.
       contract: - `phase show <id> --json` on a phase directory holding
                   spec.toml but no plan.toml parses to an object whose
                   `plan` key is present and null, and whose
                   `spec.phase.title` matches spec.toml.
                 - `task show <phase> <task> --json` emits every field the
                   labelled rendering prints — dropping `test_contract` from
                   the marshalled shape fails the field-set comparison.
                 - `stats show --json --since 7d` returns a different
                   command-count total than `--json` unfiltered on a fixture
                   with events either side of the cutoff, proving --json
                   honours --since instead of dumping all events.
                 - `changes show <id> --json` on a phase with no changes.json
                   still emits the empty record, not an error.

  t-4  Reject non-lifecycle state_map keys on both surfaces
       files:    internal/cmd/project.go, internal/cmd/doctor.go,
                 internal/cmd/project_test.go, internal/cmd/doctor_test.go
       covers:   c-4
       depends:  t-1
       desc:     `writeDotted`'s state_map branch checks the key against
                 LifecycleStatuses and errors before mutating; doctor's
                 [board] block iterates StateMap keys and counts an
                 unrecognised one as an issue (locked
                 state_map_key_severity), not a warning.
       contract: - `dross project set board.state_map.planing To Do` exits
                   non-zero naming `planned | in-progress | verifying |
                   shipped | complete`, and project.toml is byte-identical
                   to before the call.
                 - `dross project set board.state_map.verifying "In Review"`
                   still writes, so the guard rejects only the bad key.
                 - `dross project set --unset board.state_map.planing`
                   deletes an already-on-disk bad key rather than erroring —
                   the escape hatch doctor's message points at.
                 - a project.toml with `[board.state_map] planing = "X"` makes
                   `dross doctor` print a `✗` line naming `planing` and exit
                   non-zero; demoting it to the warning list fails the
                   exit-code assertion.

  t-5  Emit terminal statuses on ship, pin divergence
       files:    assets/prompts/ship.md,
                 internal/cmd/lifecycle_divergence_test.go
       covers:   c-2, c-6
       depends:  t-1
       desc:     ship.md §6 gains `dross issue phase-sync <id> --status
                 shipped` right after the provider squash-merge, and its
                 close step becomes `--status complete --close` after
                 `dross phase complete` (locked terminal_emit_sites). New
                 test reads the emitted vocabulary — derivePhaseStatus's
                 returns plus every `--status <value>` literal in the
                 embedded assets/prompts/*.md — and compares it both ways
                 against LifecycleStatuses and both default state maps.
       contract: - deleting the `--status shipped` line from ship.md fails
                   the divergence test with a message naming `shipped` as a
                   map key nothing emits.
                 - adding `"blocked": "Blocked"` to defaultJiraStateMap fails
                   the test naming `blocked`; adding "blocked" to
                   LifecycleStatuses without a map entry fails it naming the
                   missing YouTrack and Jira entries.
                 - defaultJiraStateMap and defaultYouTrackStateMap key sets
                   are asserted equal to each other — dropping `complete`
                   from only one of them fails.

  t-6  Pin the five telemetry friction cases
       files:    internal/cmd/friction_regression_test.go
       covers:   c-7
       depends:  t-1
       desc:     One test file, one subtest per case from the 2026-07-15 to
                 2026-07-28 window, each asserting the current outcome so a
                 regression is caught by name.
       contract: - a dross command run in a directory holding `.dross/` with
                   no project.toml fails with an error naming `dross init`
                   / `dross onboard`, not a bare "not a dross project".
                 - `dross task list <phase>` exits 0 and prints the phase's
                   task ids — removing the `list` subcommand fails it.
                 - `dross task done p t-1` exits non-zero with a message
                   containing `dross task status <phase-id> <task-id> done`.
                 - `dross milestone set v1.1 milestone.status active` exits 0
                   and the toml reads back `status = "active"`; the same call
                   with `frozen` fails naming `planning | active | complete`.
                 - `dross doctor` on a project with `[board] provider =
                   "jira"` prints no `[board].provider … is invalid` line.
                 - `dross issue phase-sync` on a locked, unstarted plan emits
                   no `has no ... State mapping` warning on stderr.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-1 (single definition), t-5 (bidirectional test) |
| c-3 | t-1 |
| c-4 | t-4 |
| c-5 | t-2 (project, milestone, defaults, profile, stack), t-3 (phase, task, changes, stats) |
| c-6 | t-5 |
| c-7 | t-6 |

7/7 criteria covered.

## Judgment calls

- **c-1 and c-3 merged into t-1.** Both are the same edit surface — the
  lifecycle vocabulary and the one function that consumes it. Rejected a
  separate "add the Set" task: it would be ~6 lines in one leaf file, under
  the merge threshold, and would force every other task into wave 3.
- **LifecycleStatuses lives in configenum, not internal/board or a new
  package.** configenum already imports nothing from internal/ and already
  owns MilestoneStatuses; forge, cmd and doctor can all depend on it without
  a cycle. Rejected defining it in internal/forge (cmd would import forge for
  a validation constant) and in internal/board (board.json holds links, not
  vocabulary).
- **The emitted-status set is derived from source, not declared twice.**
  t-5's test reads derivePhaseStatus's returns plus `--status` literals in
  the embedded prompts. Rejected a hand-maintained `emittedStatuses` slice in
  the test — that is a third copy, and c-2 asks for a single definition.
- **--json split into t-2/t-3 rather than one task.** Nine commands is well
  past the 5-file ceiling. The seam is shape risk, not file count: t-2's five
  commands marshal the identical struct the TOML encoder gets and need no
  design, while t-3's four each need a shape decision (the locked spec/plan
  envelope, the task field set, the stats aggregate). Rejected splitting by
  file-count into three arbitrary groups.
- **t-2 still touches 6 files, over the stated ceiling.** Accepted
  deliberately: the ceiling exists for coordination risk, and five identical
  three-line flag additions through one shared helper have none. Splitting it
  would produce two tasks that must be reviewed as one anyway.
- **c-6's emit sites are prompt edits, not Go.** ship.md §6 is where the
  merge and finalize moments live; `dross ship` (the Go command) returns
  before the merge and `dross phase complete` is locked out of board
  coupling. Rejected adding a board hook to phase complete — the locked
  decision forbids it. Note for execute: rule r-01 means `make install`
  before relying on the edited prompt.
- **c-7 is one test-only task, not five.** Four of its five cases are already
  fixed by validator-truth / root-robustness / cli-surface-sweep; the fifth
  is t-1's output. What is missing is the pin, and one file with five
  subtests is the smallest thing that supplies it. Rejected distributing the
  subtests into the four commands' existing test files — the criterion is
  about the telemetry window as a set, and scattering it loses that.
- **t-4 is wave 2 only because it reads LifecycleStatuses.** Its two edits
  (project.go writeDotted, doctor.go board block) are independent of each
  other but both need the Set to exist, so they ride together rather than
  splitting into two wave-2 tasks touching one file each.
