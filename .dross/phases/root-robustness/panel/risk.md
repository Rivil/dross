# risk lens — root-robustness

Failure-mode inventory driving this graph (each is owned by exactly one task):

| # | Failure mode | Owner |
|---|---|---|
| F1 | A stray/incomplete `.dross/` in a nested dir lets the walk climb and bind writes to an ancestor project's real root | t-1 |
| F2 | `.dross` exists as a regular file / symlink / unreadable dir — walk behaviour undefined | t-1 |
| F3 | A corrupt (parses-badly) `project.toml` / `state.json` gets swallowed as "uninitialised", so dross looks uninstalled while real work is lost | t-4 |
| F4 | A hook target (SessionStart / PreCompact) returns non-zero and breaks session start or a compaction | t-3 |
| F5 | The silent path still *writes* — `handoff.md`, `state.json`, a fresh `.dross/` — in a repo that is not a dross repo | t-2, t-7 |
| F6 | The error names a repair command that itself refuses to run against an incomplete root — dead-end advice | t-6 |
| F7 | `doctor` fails to load before it can diagnose the very condition it exists to name | t-5 |
| F8 | 52 `FindRoot()` call sites drift: only the touched commands get the new semantics, and a future command silently opts out of the loud path | t-7 |

Phase root-robustness — 7 tasks across 3 waves

Wave 1
  t-1  Gate FindRoot on root completeness
       files:    internal/cmd/root.go, internal/cmd/root_test.go
       covers:   c-1, c-3
       desc:     Stop the up-walk at the first `.dross/` directory (complete or not). Add an
                 existence check on project.toml + state.json; an incomplete root returns an
                 `IncompleteRootError{Root, Missing}` that unwraps to `ErrNoRoot` (so all 52
                 existing call sites classify it identically) but whose message names each
                 missing file and `dross onboard`. Split the bare directory walk out as an
                 unexported `findRootDir()` for the three callers that must see a root the
                 completeness gate rejects (doctor, onboard, ship recover).
       contract: - a repo with `.dross/` containing only project.toml resolves to
                   `errors.Is(err, ErrNoRoot) == true` and never returns the ancestor
                   `.dross/` two levels up (F1) — the ancestor-binding test fails if the walk
                   is allowed to continue
                 - `err.Error()` for a root missing state.json contains "state.json" and
                   "dross onboard"; contains neither "project.toml" nor a second filename
                 - a root missing BOTH files names both in one message
                 - `.dross` present as a regular file does not stop the walk and does not
                   panic (F2); `.dross` present but chmod 000 returns a non-ErrNoRoot error
                   rather than being misread as "not a dross repo"
                 - a `.dross/` with both files present but zero-byte still resolves
                   successfully — completeness is existence, never parse (locked
                   completeness_check); the test fails if a parse is smuggled into FindRoot
                 - `findRootDir()` returns the incomplete `.dross/` path with a nil error

  t-2  Gate /dross-pause on an initialised root
       files:    assets/prompts/pause.md, internal/cmd/pause_prompt_test.go
       covers:   c-4
       desc:     Add a pre-flight refusal step to pause.md: resolve the root first, and on an
                 absent OR incomplete `.dross/` print one line naming why, point at
                 `dross onboard`, and stop — no `.dross/`, no handoff.md, no gitignore edit,
                 no `dross state touch`, and never run onboard. Also flag that in the refusal
                 case the wrap-up block is not printed (it would claim a save that didn't
                 happen).
       contract: - new content-gate test over assets/prompts/pause.md asserts the refusal
                   step names both the absent and the incomplete case, "dross onboard", and
                   an explicit "do not create" for `.dross/handoff.md` — dropping any one
                   needle fails exactly that sub-assertion
                 - the test asserts the refusal precedes the "Write the confirmed content"
                   step by byte offset, so a refusal bolted on after the write step fails
                 - the test asserts pause.md never instructs running `dross init` or
                   `dross onboard` itself (locked pause_refusal: name it, don't run it)

Wave 2 (depends t-1)
  t-3  Silence status and state touch on no-root
       files:    internal/cmd/status.go, internal/cmd/state.go,
                 internal/cmd/status_test.go, internal/cmd/state_test.go
       covers:   c-2
       desc:     `dross status` and `dross state touch` join reentry and `pause --auto` in
                 treating `ErrNoRoot` (now including the incomplete case) as exit 0 with no
                 output. `state set` / `state show` / `state bump` keep the loud path — only
                 `touch`, which the prompts fire unconditionally, goes silent.
       depends:  t-1
       contract: - `dross status` in a dir whose `.dross/` holds only state.json exits 0 and
                   writes zero bytes to stdout (F4); today it exits 1 with a load error
                 - `dross status` outside any `.dross/` exits 0 silently — the
                   indistinguishability half of c-1
                 - `dross state touch "x"` against an incomplete root exits 0, prints
                   nothing, and creates no `.dross/state.json` (F5)
                 - `dross state set current_phase p` against the same incomplete root exits
                   NON-zero and names state.json — the test fails if the silence is applied
                   to the whole `state` verb instead of `touch`

  t-4  Keep corrupt files loud in hook targets
       files:    internal/cmd/pause.go, internal/cmd/pause_test.go,
                 internal/cmd/reentry_test.go
       covers:   c-2
       desc:     Per locked completeness_check, a file that exists but fails to parse is an
                 error everywhere. Remove `pause --auto`'s degrade-on-unparseable-state
                 path (autoSnapshot's `stErr == nil` skip) so a corrupt state.json returns an
                 error instead of writing a snapshot with the phase line quietly missing.
                 Git failures keep degrading — that is an environment fact, not broken state.
       depends:  t-1
       contract: - `pause --auto` with a truncated `state.json` (`{"version":`) returns a
                   non-nil error naming state.json, and handoff.md is NOT written — the
                   pre-existing bytes of handoff.md are byte-identical after the run (F3)
                 - `pause --auto` with a valid state.json but no git repo still succeeds and
                   writes "branch: (no git)" — proves the loudness is scoped to parse errors
                 - `dross reentry` with a `project.toml` containing `name = ` (bad TOML)
                   exits non-zero and names project.toml, rather than the ErrNoRoot silence
                 - a `.dross/` whose state.json is a directory (EISDIR on read) also errors
                   loudly rather than being classified as incomplete

  t-5  Name the incomplete root as a doctor diagnosis
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-5
       desc:     Switch doctor's root lookup to `findRootDir()` so it can still run inside a
                 root the completeness gate rejects. Promote the foundational-files section
                 to distinguish two verdicts: missing project.toml and/or state.json is
                 "not a dross repo (incomplete `.dross/`)" naming each missing file plus
                 `dross onboard`; a missing rules.toml alone stays the existing repairable
                 issue. Both keep the short-circuit before loadProject.
       depends:  t-1
       contract: - `dross doctor` in a `.dross/` holding only rules.toml prints a line
                   containing "incomplete", "project.toml", "state.json" and "dross onboard",
                   and exits non-zero WITHOUT a load/decode error in the output (F7)
                 - `dross doctor` in a `.dross/` with project.toml + state.json but no
                   rules.toml prints the rules.toml issue and does NOT print "incomplete" —
                   the test fails if the two verdicts are collapsed into one
                 - the missing-file list doctor prints for an incomplete root is equal to the
                   `Missing` slice on t-1's error for the same directory (single source of
                   truth; the test fails if doctor keeps its own hardcoded trio)

  t-6  Let onboard adopt an incomplete .dross
       files:    internal/cmd/onboard.go, internal/cmd/onboard_test.go
       covers:   c-3
       desc:     Onboard currently refuses whenever `.dross/` exists, so the repair command
                 every new error message names is a dead end on exactly the roots it is meant
                 to repair. Narrow the refusal to a COMPLETE root; an incomplete one is
                 adopted in place, preserving files already present (never a blind rewrite of
                 a project.toml that exists).
       depends:  t-1
       contract: - `dross onboard` in a dir whose `.dross/` holds only project.toml exits 0,
                   creates state.json, and leaves the existing project.toml byte-identical
                   (F6) — today this run fails with ".dross already exists"
                 - `dross onboard` against a complete root still refuses with the existing
                   message unless `--force`
                 - after that adopting run, `dross status` in the same dir produces output
                   (the round trip the error message promises actually terminates)

Wave 3 (depends t-3, t-4, t-6)
  t-7  Enforce loud path and no-write across commands
       files:    internal/cmd/root_incomplete_test.go
       covers:   c-2, c-3, c-4
       desc:     One new test file holding the cross-cutting guarantees: a table run of
                 non-hook commands against an incomplete root asserting the loud path, a
                 no-write assertion for the silent set, and a source-level guard that pins
                 which files are allowed to swallow `ErrNoRoot` or call `findRootDir()`.
       depends:  t-3, t-4, t-6
       contract: - table over `project show`, `rule list`, `phase list`, `verify`,
                   `task show`, `milestone show`: each exits non-zero against an incomplete
                   root and its error text contains the missing filename AND "dross onboard"
                   — adding a command that swallows the error fails its row (c-3)
                 - after running `reentry`, `status`, `state touch x` and `pause --auto` in a
                   dir with an incomplete `.dross/`, a recursive dir snapshot taken before
                   and after is byte-identical: no handoff.md, no state.json, no new
                   `.dross/` (F5, c-2/c-4 write-side)
                 - the same four commands run in a dir with NO `.dross/` at all also create
                   nothing and print nothing — the c-1 indistinguishability assertion
                 - source guard: scanning internal/cmd/*.go, the set of files containing
                   `ErrNoRoot` in a swallow position is exactly {reentry.go, pause.go,
                   status.go, state.go, telemetry.go}, and the set calling `findRootDir()` is
                   exactly {root.go, doctor.go, onboard.go, ship_recover.go}. A new command
                   that copies the silent pattern fails the guard by name (F8)

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-3, t-4, t-7 |
| c-3 | t-1, t-6, t-7 |
| c-4 | t-2, t-7 |
| c-5 | t-5 |

All 5 criteria covered.

## Judgment calls

- Changed `FindRoot()`'s semantics in place rather than adding a new `RequireRoot()` helper that 52 call sites must adopt. A parallel helper leaves the old function as a live footgun and makes correctness opt-in per command; changing the choke point makes every non-hook command loud for free and reduces the phase's blast radius to one function plus the four commands that need silence.
- Made the incomplete-root error unwrap to `ErrNoRoot` instead of introducing a second sentinel callers must also check. c-1 demands the two be indistinguishable to callers; a second sentinel guarantees some caller eventually handles one and not the other, which is precisely the bug class this phase exists to close.
- Kept `.dross` present as a regular file as a non-stopping condition (walk continues) rather than treating any `.dross` entry as a stop. The locked walk_stop decision says "directory", and stopping on a file would make an unrelated stray file able to sever a real project — but the behaviour is ambiguous enough that t-1 pins it with an explicit test rather than leaving it to `IsDir()` by accident.
- Removed `pause --auto`'s degrade-on-unparseable-state path even though its code comment argues a PreCompact hook must never fail a compaction. The locked completeness_check decision overrides that comment; the risk it names (a truncated state.json hidden behind a clean-looking snapshot) is worse than a failed compaction. Git failures keep degrading, because a missing git repo is an environment fact rather than broken dross state.
- Added t-6 (onboard adopts an incomplete root) even though no criterion names onboard. c-3 requires the message to name "the repair command" and locked pause_refusal names `dross onboard`; onboard currently refuses on any existing `.dross/`, so without this task every new error message in the phase points at a command that fails. Shipping actionable-looking dead-end advice is a worse failure than the current confusion.
- Scoped the silent set to `state touch` only, not the whole `state` verb. `touch` is fired unconditionally by prompts and is the one c-2 names; `state set` silently no-op'ing would let a workflow believe it recorded a phase transition that never landed.
- Put the source-level guard test in wave 3 rather than wave 1. Its allowlist encodes the phase's end state, so authoring it first guarantees one rewrite; authoring it last makes it a genuine regression gate for command 57.
- Left telemetry alone. `telemetryPath()` resolves to `~/.claude/dross/`, so an incomplete root costs only a missing `repo_hash` on the event — no `.dross/` write, nothing that can violate c-4.
