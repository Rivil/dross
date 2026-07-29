# cli-surface-sweep — verification-lens draft

Lens: each criterion's ideal test contract was written first; the task is the
smallest change that makes that contract satisfiable.

```
Phase cli-surface-sweep — 10 tasks across 3 waves

Wave 1
  t-1  Add `dross task list` with --json
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1
       contract: a 3-task/2-wave plan prints exactly 3 rows, each row containing the
                 task id, its wave number, its status and its title
       contract: a task whose plan.toml `status` field is absent prints `pending`, not
                 an empty column (the same orPending rule `task show` uses)
       contract: `task list --json` unmarshals into a []struct with id/wave/status/title;
                 after `task add --title X` the array length goes 3 -> 4
       contract: with the phase-id argument omitted and state.current_phase = "p", the
                 rows are p's tasks; with current_phase empty the command errors naming
                 the missing phase-id argument instead of reading an arbitrary phase

  t-2  Complete + reversible project dotted paths
       files:    internal/cmd/project.go, internal/cmd/project_test.go
       covers:   c-6, c-9
       contract: `project set board.github_project PVT_x` then `project get
                 board.github_project` round-trips PVT_x, and the value reloads from
                 project.toml as Board.GitHubProject (not a stray top-level key)
       contract: `project set board.state_map.shipped Fixed` on a project whose
                 state_map already has in_progress leaves in_progress intact — the
                 whole-map clobber the state_map_write decision forbids fails here
       contract: a reflection test over `project.Board`'s toml tags fails naming the
                 field if any Board field has no readDotted AND writeDotted case, so a
                 [board] field added later cannot ship unsettable
       contract: `project set --unset remote.auth_user` clears the scalar and
                 `project get remote.auth_user` then prints an empty line (exit 0), while
                 the key disappears from the re-read project.toml
       contract: `project set --unset board.state_map.shipped` deletes only that entry —
                 board.state_map.in_progress still reads back — and `--unset` on an
                 unknown path errors with the same unknown-field message `set` uses

  t-3  Validate + resolve milestone status writes
       files:    internal/configenum/configenum.go, internal/configenum/configenum_test.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-5
       contract: `milestone set status shipped` writes Milestone.Status = "shipped" —
                 the bare name resolves to milestone.status without the caller spelling
                 the prefix
       contract: `milestone set status bogus` exits non-zero, the message names the
                 accepted set (planning | active | shipped | archived), and the milestone
                 toml is byte-identical afterwards — rejection happens before Save
       contract: `milestone set milestone.status shipped` (fully dotted) still works
                 unchanged, so the resolver is additive
       contract: a bare name that matches no known milestone path errors as unknown;
                 configenum.MilestoneStatuses.Has("") is false, so `milestone set status ""`
                 is rejected rather than blanking the field

  t-4  Multi-path get renderer + `state get`
       files:    internal/cmd/dotget.go, internal/cmd/dotget_test.go,
                 internal/cmd/state.go, internal/cmd/state_test.go
       covers:   c-3, c-4
       contract: one path in -> the bare value and nothing else: `state get current_phase`
                 prints exactly "<phase>\n", no braces, no quotes, no key — a
                 golden-string test, because multi_get_shape locks this byte-identical
       contract: two or more paths in -> stdout parses as a single JSON object whose keys
                 are the requested paths in the order given, and whose values are the
                 same strings the one-path form prints
       contract: an unknown path among several aborts the whole call with an
                 unknown-field error and prints no partial object
       contract: `dross state get` exists at all — `dross state --help` lists it, and
                 `state get version` returns the 4-part version from state.json
       contract: `state show --json` exits 0 and its stdout is byte-identical to bare
                 `state show`; the pre-change `unknown flag: --json` failure is what this
                 test would have caught

  t-5  Doctor flags unrecognised task statuses
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-7
       contract: a plan.toml carrying `status = "in-progress"` (hyphen) makes doctor print
                 one line containing BOTH the phase id and the task id, and doctor exits
                 non-zero — the exact silent-drop-from-NextRunnable case
       contract: each of pending / in_progress / done / failed, plus an omitted status
                 field, produces no such line and leaves doctor's exit code unchanged
       contract: a phase directory with no plan.toml is skipped silently rather than
                 becoming a doctor issue
       contract: `dross validate` on the same repo with the hyphenated status still exits
                 0 — pins the status_check_home decision (doctor owns the enum, validate
                 stays structural)

  t-6  Curated mis-reach table + nearest-match
       files:    internal/cmd/hints.go, internal/cmd/hints_test.go,
                 assets/prompts/secure.md
       covers:   c-2, c-8
       contract: CuratedHint("dross task", "done") returns a string containing
                 `dross task status`; the semantic remap string distance can never find
       contract: CuratedHint returns the documented working invocation for each of
                 ("dross phase create", "--title"), ("dross task edit", "--files") and
                 ("dross security run", "--new"); a lookup with no entry returns
                 found=false so the fallback path is reachable
       contract: Nearest("stauts", ["status","show","set"]) == ["status"];
                 Nearest("zzzzzz", same) is empty — a far-off guess gets no bogus suggestion
       contract: no assets/prompts/*.md contains any invocation the table classifies as
                 broken — a grep test over the prompt corpus, which fails today on
                 assets/prompts/secure.md:42 (`dross security run --new`); dross's own
                 prompts may not teach an invocation its own CLI calls wrong
       note:     editing assets/prompts/secure.md needs `make install` before the change
                 is live (rules.toml r-01)

Wave 2 (depends t-2, t-3, t-4, t-6)
  t-7  Adopt multi-path get in project + milestone
       files:    internal/cmd/project.go, internal/cmd/project_test.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-4
       depends:  t-2, t-3, t-4
       contract: `project get project.name` prints the bare name, byte-identical to the
                 pre-change output (asserted against a literal, not a re-derived value)
       contract: `project get project.name runtime.mode board.state_map.shipped` emits one
                 JSON object with those three keys — including the state_map path t-2 added
       contract: `milestone get milestone.title milestone.status` emits one keyed object,
                 while `milestone get scope.success_criteria` alone still prints one entry
                 per line exactly as today (the list shape is not swallowed by JSON)
       contract: in the multi-path form a list-valued milestone path serialises as a JSON
                 array, not as a newline-joined string
       contract: `milestone get v1.1 milestone.title milestone.status` — the optional
                 leading version argument still resolves, and is not mistaken for a path

  t-8  Route unknown subcommands through the table
       files:    internal/cmd/subcommand_guard.go, internal/cmd/subcommand_guard_test.go
       covers:   c-2, c-8
       depends:  t-6
       contract: `dross task done t-1` errors with a message containing
                 `dross task status` — the curated entry, and it wins over cobra's own
                 suggestion list when both would fire
       contract: `dross task shwo p t-1` (no curated entry) errors naming `show` via the
                 distance fallback
       contract: `dross task wibble` (no entry, no near match) still lists the available
                 subcommands and exits non-zero — the existing behaviour is preserved,
                 not replaced by the hint path
       contract: a valid subcommand is untouched: `dross task next <phase>` still runs its
                 RunE and returns nil

  t-9  Hint on unknown flags via FlagErrorFunc
       files:    internal/cmd/flag_hint.go, internal/cmd/flag_hint_test.go,
                 cmd/dross/main.go
       covers:   c-2, c-8
       depends:  t-6
       contract: on a test tree, an unknown flag with a curated entry produces the
                 entry's working invocation in the error text instead of cobra's bare
                 `unknown flag: --title`
       contract: an unknown flag with NO curated entry names the nearest flag actually
                 declared on that command (`--titel` -> `--title`), and names none when
                 nothing is within distance
       contract: the hook reaches nested commands, not just root: an unknown flag on a
                 2-deep command (parent -> child) is intercepted, proving cobra's
                 FlagErrorFunc parent-walk is relied on rather than per-command wiring
       contract: a KNOWN flag still parses — installing the hook does not change
                 `--image x` on a command that declares it

Wave 3 (depends t-8, t-9)
  t-10 Pin the four mis-reaches against the real tree
       files:    cmd/dross/main_test.go
       covers:   c-2
       depends:  t-8, t-9
       contract: a table test over the assembled newRoot() tree asserts all four of
                 `task done`, `phase create --title`, `task edit --files` and
                 `security run --new` exit non-zero AND print their working invocation —
                 c-2's acceptance sentence, executed
       contract: every replacement invocation in the curated table resolves against
                 newRoot(): a hint naming a command path or flag that does not exist
                 fails here, so the table cannot rot into pointing at nothing
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 `task list` | t-1 |
| c-2 curated mis-reach hints | t-6, t-8, t-9, t-10 |
| c-3 `state show --json` | t-4 |
| c-4 multi-path get | t-4, t-7 |
| c-5 milestone status resolve + enum | t-3 |
| c-6 full [board] settable | t-2 |
| c-7 doctor task-status check | t-5 |
| c-8 distance fallback | t-6, t-8, t-9 |
| c-9 `project set --unset` | t-2 |

9/9 criteria covered.

## Judgment calls

- **Merged c-6 and c-9 into t-2 rather than sequencing them.** `--unset` on a
  `board.state_map.<status>` entry is the exact inverse of the set that c-6 adds; splitting
  them would have made a false wave-2 dependency and two tasks editing the same two files.
- **Split c-4 across t-4 (renderer + `state get`) and t-7 (project/milestone adoption)
  rather than one task.** `dross state get` does not exist at all today — it needs a whole
  new dotted reader over state.json — so it is the hardest call site; building the renderer
  against it first means project/milestone adoption is pure wiring. One combined task would
  have spanned 8 files.
- **t-7 depends on t-2 and t-3, not only on t-4.** Not a file-collision hedge: t-2 changes
  `readDotted`'s path set and t-3 changes how a milestone path is resolved. Wrapping those
  functions in the multi-get renderer before their signatures settle means rework.
- **Put the c-2 end-to-end assertion in its own wave-3 task (t-10) instead of appending it
  to t-9.** c-2 names four invocations, two of which travel the subcommand path (t-8) and
  three the flag path (t-9); the only place all four are true at once is after both. It is
  also the only test that can run against `newRoot()` (package main), which is where the
  "no hint points at a nonexistent invocation" check has to live.
- **Chose a reflection test over `project.Board`'s toml tags for c-6, not four hand-written
  round-trips.** c-6 says "every `[board]` field the code reads" — a reflection test is the
  only shape that stays true when a field is added later; hand-written cases would pass
  while a new field ships unsettable, which is exactly the bug c-6 describes.
- **Chose a golden-string assertion for the one-path get, not a round-trip.** The
  multi_get_shape decision promises byte-identical output for existing callers; a test that
  re-derives the expected value from the same code cannot detect a stray quote or newline.
- **Included `assets/prompts/secure.md` in t-6 and made "no prompt teaches a broken
  invocation" a test contract.** `security run --new` is a curated mis-reach *because*
  dross's own prompt documents a flag `securityRun` never declared. Hinting about it while
  still shipping the prompt that causes it leaves the mis-reach in place.
- **Rejected putting the task-status check in `dross validate` as a second home.** The
  status_check_home decision is locked; t-5 spends a contract asserting `validate` stays
  green on the same repo so the boundary is pinned, not merely respected.
- **Rejected a `--status` flag on `task edit` as the fix for `task edit --files`.** t-6
  routes it to a hint instead: `task edit` deliberately owns no status/files surface
  (`dross task status` and `task add --files` do), and widening it would contradict the
  existing comment in task.go rather than close c-2.
