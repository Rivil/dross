# MVP lens draft

Phase root-robustness — 5 tasks across 2 waves

Wave 1
  t-1  Make FindRoot completeness-aware
       files:    internal/cmd/root.go, internal/cmd/root_test.go, internal/cmd/ship_recover.go
       covers:   c-1, c-3
       desc:     Split the up-walk into FindRootDir (locate the first `.dross/`, stop there)
                 and FindRoot (locate + require project.toml and state.json to exist).
                 An incomplete root returns an error value that satisfies
                 `errors.Is(err, ErrNoRoot)` and whose text names the missing file plus
                 `dross onboard`. Add the shared `hookRoot()` helper the wave-2 hook targets
                 use (returns ok=false for absent *and* incomplete, error only for real
                 stat failures). `ship recover` switches to FindRootDir so the escape hatch
                 still works on a half-wiped `.dross/`.
       contract: - `.dross/` holding only project.toml: FindRoot returns err with
                   errors.Is(err, ErrNoRoot)==true and err.Error() containing "state.json"
                   and "dross onboard" — dropping either substring fails the assertion.
                 - walk_stop: cwd in `parent/child` where child/.dross is incomplete and
                   parent/.dross is complete — FindRoot returns the error and never
                   parent/.dross; a resumed up-walk fails this test.
                 - non-hook command: `dross state show` in that repo exits non-zero with the
                   same missing-file + repair message; a silent exit 0 fails.
                 - `dross ship recover` still resolves the root when only
                   `.dross/project.toml` survives (regression guard on the recovery path).

  t-2  Refuse /dross-pause on an uninitialised root
       files:    assets/prompts/pause.md, internal/cmd/pause_prompt_test.go
       covers:   c-4
       desc:     Add a pre-flight gate to pause.md: if `.dross/` is absent, or project.toml
                 or state.json is missing, print one line naming which file is missing and
                 `dross onboard`, then stop — no handoff.md, no `.dross/`, no
                 `dross state touch`, and it never runs onboard itself. Mirror it as a hard
                 rule at the bottom. (Prompt edits need `make install` to go live — rule r-01.)
       contract: - pause_prompt_test asserts the normalised prompt contains, as separate
                   sub-assertions: "project.toml", "state.json", "dross onboard", a
                   writes-nothing phrase, and a does-not-run-onboard phrase — removing any
                   one fails exactly that sub-test.
                 - ordering assertion: the refusal text's byte index is lower than the
                   index of the "write the confirmed content to `.dross/handoff.md`" step,
                   so a gate placed after the write fails.

Wave 2 (depends t-1)
  t-3  Silence reentry and pause --auto on no-root
       files:    internal/cmd/reentry.go, internal/cmd/pause.go, internal/cmd/hookroot_test.go
       covers:   c-2
       desc:     Both hook targets route through hookRoot(): absent or incomplete `.dross/`
                 → return nil with no output and no writes. Parse failures stay loud —
                 autoSnapshot's swallowed state.Load / project.Load errors are propagated out
                 of pauseAuto instead of being silently skipped (completeness_check).
       depends:  t-1
       contract: - `dross reentry` in a `.dross/` with project.toml but no state.json writes
                   zero bytes to stdout and returns nil; emitting the SessionStart JSON
                   envelope or an error fails.
                 - pauseAuto in that repo returns nil and `.dross/handoff.md` does not exist
                   afterwards.
                 - corrupt-not-missing: state.json containing `{"version":` (truncated) with
                   project.toml present → pauseAuto returns a non-nil error naming
                   state.json, and `dross reentry` returns non-nil; silence on either fails.

  t-4  Silence status and state touch on no-root
       files:    internal/cmd/status.go, internal/cmd/state.go, internal/cmd/status_test.go
       covers:   c-2
       desc:     `dross status` and `dross state touch` route through hookRoot() and exit 0
                 with no output when the root is absent or incomplete. Other `state`
                 subcommands (show/set/bump) keep the loud FindRoot path. Existing
                 TestStatusFreshDirSuggestsInit is inverted: a bare dir is now silence, not
                 ErrNoRoot.
       depends:  t-1
       contract: - `dross status` in a `.dross/` missing state.json writes zero bytes and
                   returns nil; the current ErrNoRoot behaviour fails this.
                 - `dross state touch "x"` in that repo returns nil, prints no "touched:"
                   line, and does not create `.dross/state.json`.
                 - over-silencing guard: `dross state show` in the same repo still returns a
                   non-nil error naming state.json.
                 - corrupt-not-missing: garbled state.json → `dross status` returns non-nil.

  t-5  Diagnose an incomplete root in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-5
       desc:     Doctor resolves its root with FindRootDir so it reaches the existing
                 Foundational-files block instead of dying on the not-a-dross-repo error;
                 the per-file miss line also names `dross onboard` as the repair.
       depends:  t-1
       contract: - `dross doctor` in a `.dross/` with project.toml + rules.toml but no
                   state.json prints a line containing ".dross/state.json" and returns the
                   "1 project-level issue(s) found" error; a regression to FindRoot returns
                   the not-a-dross-repo error and prints no "Foundational files:" header,
                   failing both assertions.
                 - no `.dross/` anywhere: doctor still returns an error satisfying
                   errors.Is(err, ErrNoRoot) and prints nothing.

## Coverage

| criterion | tasks |
|---|---|
| c-1 completeness → not-a-dross-repo, indistinguishable from absent | t-1 |
| c-2 four hook targets exit 0 silently | t-3, t-4 |
| c-3 non-hook commands fail loudly, naming file + repair | t-1 |
| c-4 /dross-pause refuses and writes nothing | t-2 |
| c-5 doctor names the missing file | t-5 |

5/5 criteria covered.

## Judgment calls

- One error value, not a second exported sentinel: the incomplete-root error wraps ErrNoRoot
  (`errors.Is` true) while carrying the missing-file text. Rejected a distinct
  `ErrIncompleteRoot` that callers must also check — c-1 demands the two cases be
  indistinguishable, and 56 existing `return err` call sites then deliver c-3 for free with
  zero edits.
- No task for the ~56 FindRoot call sites. Rejected an audit/sweep task: they already
  propagate the error to cobra's non-zero exit, so a sweep would be structure without a
  criterion behind it.
- status and `state touch` go silent on an *absent* root too, not just an incomplete one —
  which inverts an existing test. Rejected keeping ErrNoRoot for absent: c-1 says the two
  cases must be indistinguishable to callers, so a command cannot error on one and exit 0
  on the other.
- The pause prompt gates on its own existence check of project.toml + state.json (files it
  already reads in pre-flight). Rejected adding a `dross root` command for the prompt to
  shell out to — new CLI surface no criterion asks for — and rejected keying on "`dross
  status` printed nothing", which would make the refusal depend on t-4 and read as an
  accident rather than a rule.
- Hook silencing is two tasks, not one. They are independent and parallel; merged, the task
  would touch six files across the reentry/pause/status/state handlers plus a fixture
  rewrite, over the split-at-5-files line.
- Parse-error loudness in pauseAuto rides in t-3 rather than getting its own task: it is the
  same code path deciding which errors are swallowed, and it is a locked decision
  (completeness_check) rather than a separate criterion.
- Doctor keeps its existing project.toml + rules.toml + state.json trio in the foundational
  block even though completeness is only the first and third. Rejected narrowing it —
  c-5 asks for a diagnosis of which required file is missing, and rules.toml is required
  by loadProject regardless.
