# cli-surface-sweep — RISK lens draft

Failure modes drive the graph. Each task owns exactly one class of breakage and
proves it. The risks in this phase are not "does the feature exist" — every
criterion here is a small surface — they are **silent wrong behaviour**: a plan
task that vanishes from NextRunnable, a `get` that partially prints before
erroring, a hint table that points at a command that was renamed, a `--unset`
that clobbers a whole map, an ambiguous first argument read as the wrong thing.

Phase cli-surface-sweep — 8 tasks across 2 waves

## Wave 1

```
  t-1  Add task list with empty-plan safety
       files:    internal/cmd/task.go, internal/cmd/task_test.go
       covers:   c-1
       description:
                 Add `dross task list [phase-id]` printing an aligned ID/WAVE/STATUS/TITLE
                 table, with --json emitting the task array. Phase-id optional, defaulting
                 to state.current_phase; blank task status renders as pending.
       contract: - a plan whose tasks all have status="" prints STATUS=pending for every row,
                   not an empty column
                 - `task list --json` on a plan with zero tasks emits `[]` and exits 0, never
                   `null` and never the "(no tasks)" human line
                 - omitting the phase-id with state.current_phase empty fails naming
                   current_phase, instead of loading `.dross/phases//plan.toml`
                 - omitting the phase-id with state.current_phase set lists that phase's tasks
                 - a phase-id with no plan.toml errors with the missing path, and prints no
                   table header first
```

```
  t-2  Build atomic multi-path get core
       files:    internal/cmd/dotted_get.go, internal/cmd/dotted_get_test.go
       covers:   c-4
       description:
                 New shared resolver: given a lookup func and N dotted paths, returns either
                 the single bare value (unchanged) or one keyed JSON object. Resolves every
                 path before emitting anything.
       contract: - one path returns the lookup's raw string with no JSON quoting, no braces
                   and no trailing key — byte-identical to today's single-path output
                 - two paths where the SECOND is unknown returns an error naming that path and
                   writes nothing to stdout (no partial object)
                 - a path whose lookup yields a list serialises as a JSON array in multi mode,
                   not as a CSV string
                 - the same path passed twice appears once in the object rather than emitting
                   duplicate JSON keys
                 - key order in the emitted object follows argument order, so a script reading
                   it does not depend on map iteration
```

```
  t-3  Add curated hint table with drift guard
       files:    internal/cmd/hints.go, internal/cmd/hints_test.go
       covers:   c-2, c-8
       description:
                 Curated map from known mis-reaches (`task done`, `phase create --title`,
                 `task edit --files`, `security run --new`) to their working invocation,
                 consulted first; string-distance over the command/flag tree as fallback.
       contract: - `task done` resolves to a hint naming `dross task status <phase-id>
                   <task-id> done`, a remap no edit distance can produce
                 - every curated hint's replacement invocation resolves against the real
                   command tree built by newRoot(), so a renamed command fails this test
                   rather than shipping a hint that points nowhere
                 - a mis-reach absent from the table (`dross phse`) still returns the nearest
                   existing name from the distance fallback
                 - lookup is normalised, so `dross Task Done` hits the same table entry
                 - a typo further than the distance threshold returns no fabricated
                   suggestion rather than an arbitrary nearest command
```

```
  t-4  Report unrecognised task status in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-7
       description:
                 New doctor section walking every .dross/phases/*/plan.toml and reporting any
                 task whose status is outside pending|in_progress|done|failed, naming phase
                 and task id. Counts toward doctor's issue total.
       contract: - a plan.toml with status="Done" (capitalised) is reported naming both the
                   phase slug and the task id — this is the exact value NextRunnable skips
                   silently today
                 - status="" is NOT reported, because empty means pending everywhere else in
                   the code
                 - a phase directory with no plan.toml is skipped, and doctor still reaches
                   its final verdict rather than erroring out of the section
                 - an unparseable plan.toml is reported as its own issue naming the phase,
                   not swallowed into a clean "✓" line
                 - one bad status increments doctor's exit-code issue count (doctor exits
                   non-zero), rather than landing in the advisory warnings block
```

```
  t-5  Complete board writes and add project set --unset
       files:    internal/cmd/project.go, internal/cmd/project_test.go
       covers:   c-6, c-9
       description:
                 Add board.github_project and dynamic board.state_map.<status> to readDotted/
                 writeDotted, plus a --unset <path> mode on `project set` that clears a scalar
                 or removes one state_map entry.
       contract: - `project set board.state_map.done Closed` on a project.toml with no
                   [board.state_map] table creates the map instead of panicking on a nil map
                 - setting board.state_map.done leaves an existing board.state_map.failed
                   entry present — a per-key write, never a whole-map replace
                 - `project set --unset board.state_map.done` removes only that key; the
                   remaining entries and every other [board] field are unchanged
                 - unsetting the last state_map entry writes project.toml with no
                   [board.state_map] table at all, not an empty one
                 - `project set --unset` on an unknown path errors and leaves project.toml
                   byte-unchanged (no truncate-then-fail)
                 - `--unset repo.squash_merge` yields false and `--unset` on a list field
                   yields an absent list, not the literal string "<nil>"
                 - `project get board.github_project` round-trips the value written by
                   `project set board.github_project`
```

## Wave 2

```
  t-6  Wire multi-get into project and state
       files:    internal/cmd/project.go, internal/cmd/state.go, internal/cmd/state_test.go,
                 internal/cmd/project_test.go
       covers:   c-3, c-4
       depends:  t-2, t-5
       description:
                 Switch `project get` to variadic paths through the t-2 core, add
                 `dross state get <path>...` over state.json fields, and accept --json on
                 `state show`.
       contract: - `project get project.name` prints exactly the bare name with no JSON —
                   the byte-identical guard that keeps existing prompts working
                 - `project get project.name runtime.mode` prints one object carrying both
                   keys
                 - `project get project.name board.nonsense` errors and prints nothing, so a
                   caller cannot parse a half-written object
                 - `state get current_phase` prints the bare phase id; `state get version
                   current_phase` prints both keyed
                 - `state show --json` exits 0 and its stdout parses as the same JSON object
                   bare `state show` emits — the flag is accepted, not a second format
                 - `state get` on a path that exists in project.toml but not state.json (e.g.
                   project.name) errors naming the unknown field
```

```
  t-7  Resolve milestone arg ambiguity and status enum
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go,
                 internal/configenum/configenum.go, internal/configenum/configenum_test.go
       covers:   c-4, c-5
       depends:  t-2
       description:
                 Teach milestone get/set to tell an optional version argument from dotted
                 paths, resolve an unambiguous bare field name to its dotted path, and reject
                 a milestone status outside a new configenum set before writing.
       contract: - `milestone get v1.1 milestone.title milestone.status` treats v1.1 as the
                   version and the other two as paths; `milestone get milestone.title
                   milestone.status` treats none of them as a version and still reads
                   state.current_milestone
                 - `milestone set status active` writes milestone.status on the current
                   milestone, resolving the bare name
                 - a bare name that matches more than one settable path is rejected as
                   ambiguous, listing the candidates, rather than silently picking the first
                 - `milestone set milestone.status shipping` (not in the set) errors and the
                   milestone toml is byte-unchanged — validation runs before Save, not after
                 - a valid status differing only in case/whitespace is accepted, matching how
                   configenum.Normalize behaves for every other enum
                 - `milestone get scope.success_criteria` alone still prints one entry per
                   line, while multi-get returns it as a JSON array
```

```
  t-8  Wire hints into subcommand and flag errors
       files:    internal/cmd/subcommand_guard.go, internal/cmd/subcommand_guard_test.go,
                 cmd/dross/main.go, cmd/dross/main_test.go
       covers:   c-2, c-8
       depends:  t-3
       description:
                 Consult the t-3 hint resolver from EnforceSubcommandKnown, and install a
                 root FlagErrorFunc so unknown-flag errors — which never reach RunE — get the
                 same curated-then-distance treatment.
       contract: - `dross security run --new` fails with a message naming `dross security
                   run`; the unknown-flag path is proven separately from the subcommand path
                   because cobra rejects flags before RunE ever fires
                 - `dross phase create --title x` names the positional form `dross phase
                   create <title>`
                 - the FlagErrorFunc set on root applies to a depth-3 command (`dross task
                   edit --files a.go`), proving cobra's parent inheritance rather than
                   assuming it
                 - `dross task done` still exits non-zero, so the pre-existing exit-0-on-help
                   regression does not return
                 - an unknown subcommand WITH no curated entry still prints the "Available
                   subcommands" list, so the new hint path does not replace the existing
                   fallback
                 - a hinted flag error is still classified as unknown_flag by the telemetry
                   taxonomy — the reworded message must not knock the event into `other`
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 `task list` | t-1 |
| c-2 mis-reach names working invocation | t-3, t-8 |
| c-3 `state show --json` | t-6 |
| c-4 multi-path get (project/milestone/state) | t-2, t-6, t-7 |
| c-5 bare field name + milestone status enum | t-7 |
| c-6 every `[board]` field settable | t-5 |
| c-7 doctor reports unrecognised task status | t-4 |
| c-8 distance fallback for plain typos | t-3, t-8 |
| c-9 `project set --unset` | t-5 |

9/9 criteria covered.

## Judgment calls

- **Split the hint work into engine (t-3) and wiring (t-8)** rather than one task.
  Rejected the single-task version because the two failure modes are structurally
  different: table rot (a hint pointing at a renamed command) versus cobra's flag
  errors never reaching RunE. One owner each means a regression in either is
  attributable.
- **The hint drift guard lives in `cmd/dross/main_test.go`, not `internal/cmd`.**
  Rejected asserting against a hand-built tree in the cmd package — that tree can
  drift from `newRoot()`, which is precisely the drift the guard exists to catch.
  Only package main can see the real assembled tree.
- **Multi-get core extracted to its own file (t-2) before any wiring.** Rejected
  editing project/milestone/state get in place in parallel: the byte-identical
  single-path guarantee is the highest-regression surface in the phase (every
  existing prompt calls it), and it should be proven once in isolation rather than
  three times against three different lookup shapes.
- **Grouped c-6 and c-9 into one task (t-5).** Both are `board.state_map.<status>`
  path handling — set and unset are the same dynamic-path parser and the same
  nil-map/last-entry edges. Splitting them would put the same code under two
  owners.
- **Grouped c-4's milestone half with c-5 (t-7) instead of with c-4's other half.**
  Rejected the criterion-shaped split: the real risk in both is "which argument is
  this?" — version-vs-path and bare-name-vs-dotted-path are one ambiguity problem,
  and one resolver should own it.
- **t-4 counts a bad status as an issue (exit-code affecting), not a warning.**
  Rejected the advisory framing used for architecture links: a task silently
  dropped from NextRunnable stalls execution, which is a correctness failure, not
  a suspicion. Accepting that this can newly fail an existing repo's doctor is the
  point — doctor is the enum-enforcing validator per the locked `status_check_home`
  decision, and `validate` deliberately stays untouched.
- **t-6 depends on t-5 as well as t-2.** Not a wave-padding choice: `project get`
  must read `board.state_map.<status>` back symmetrically per the locked
  `state_map_write` decision, so it needs t-5's extended readDotted.
- **No task adds `--files` to `task edit`.** c-2 lists it as a mis-reach to be
  hinted, and `task edit`'s doc comment records the deliberate omission of status
  editing there. Treating it as a missing feature would exceed the criterion; t-3
  curates it as a hint instead.
