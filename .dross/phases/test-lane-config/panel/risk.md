# risk lens — test-lane-config

Failure modes first. Every task below owns exactly one way this feature can lie
to its caller: a lane that silently doesn't match, a run that measured nothing
and exits green, a consent grant that covers a command nobody read, a gate that
short-circuits before the second lane ran.

Phase test-lane-config — 8 tasks across 5 waves

Wave 1
  t-1  Declare test lanes in the project schema
       files:    internal/project/project.go, internal/cmd/validate.go,
                 internal/cmd/validate_test.go, internal/project/project_test.go
       covers:   c-1
       desc:     Add `project.TestLane{Name, Match []string, Command string}` (toml+json
                 tags) and `Runtime.TestLane []TestLane` under `[[runtime.test_lane]]`.
                 validate reports a lane missing name, match or command, and a duplicate
                 lane name, each naming the offending lane (by ordinal when the name is
                 what's missing).
       contract: - a lane with match+command but no `name` makes `dross validate` report a
                   problem naming its ordinal position; a lane with a name and command but
                   an empty `match` array is reported BY NAME rather than accepted as a
                   lane that matches nothing
                 - two lanes sharing one name are reported as a duplicate — if this check
                   is deleted, t-4's name-keyed consent map silently gives one grant
                   authority over two commands, and validate_test.go's duplicate case fails
                 - json_tag_parity_test.go fails if TestLane's toml fields carry no
                   matching json tags
                 - a repo with zero `[[runtime.test_lane]]` entries produces zero new
                   validate problems (lanes are opt-in; the validator must not invent work
                   for every existing repo)

  t-2  Glob matcher for lane match patterns
       files:    internal/lane/match.go, internal/lane/match_test.go
       covers:   c-2
       desc:     Pure-string matcher `Match(globs []string, path string) bool` plus path
                 normalization (strip `./`, force forward slashes, refuse absolute and
                 `..`-escaping paths). Supports `**` crossing separators and `*` not
                 crossing one; a trailing `/` means "everything beneath".
       contract: - `internal/**/*.go` matches both `internal/cmd/test.go` and
                   `internal/a/b/c.go`, while `internal/*.go` matches only
                   `internal/x.go` — if `**` degrades to filepath.Match's `*`, the nested
                   case fails and every nested source file becomes a silent miss
                 - `docs/*.md` does NOT match `docs/sub/x.md`
                 - `./internal/cmd/test.go` and `internal/cmd/test.go` give the same
                   answer, so an agent's `./`-prefixed argv is not a silent miss
                 - `docs/` matches `docs/x.md` and `docs/sub/y.md` (trailing slash ==
                   `docs/**`)
                 - `/etc/passwd` and `../outside/x.go` match nothing rather than being
                   normalized into a lane
                 - a pattern with no metacharacter matches only that exact path

Wave 2 (depends t-1, t-2)
  t-3  Resolve a file set to lanes and unmatched paths
       files:    internal/lane/resolve.go, internal/lane/resolve_test.go
       covers:   c-2
       depends:  t-1, t-2
       desc:     `Resolve(lanes []project.TestLane, paths []string) Resolution{Matched
                 []project.TestLane, Unmatched []string}`. Declaration order preserved,
                 each lane appears at most once, a path may land in several lanes,
                 unmatched paths kept in input order, deduped.
       contract: - a path matching lanes A and C returns both in DECLARATION order — a
                   resolver that stopped at the first hit, or ordered by match count,
                   fails resolve_test.go's multi-lane case (locked multi_lane)
                 - a path matching two globs inside the SAME lane yields that lane once,
                   not twice — a duplicated entry would spawn one lane's command twice
                 - {`internal/x.go`, `docs/y.md`} against a go-only lane returns the go
                   lane matched AND `docs/y.md` in Unmatched; dropping the md path fails
                   the "named rather than silently dropped" case
                 - zero input paths returns zero matched and zero unmatched: the resolver
                   states facts, the caller (t-7) decides that a nothing-matched run is a
                   refusal

  t-4  Per-lane consent grants + `dross trust --lane`
       files:    internal/cmd/local.go, internal/cmd/trust.go,
                 internal/cmd/trust_lane_test.go
       covers:   c-6
       depends:  t-1
       desc:     Add `TrustedLaneCommands map[string]string` to localStore
                 (`trusted_lane_commands`, lane name → sha256 of that lane's command),
                 deliberately ABSENT from localKeys. `LaneConsented(root, name, line)`,
                 `GrantLaneConsent(root, name, line)`, and `dross trust --lane <name>`
                 which prints the command line before it writes.
       contract: - editing ONE lane's command makes LaneConsented false for that lane and
                   leaves every other lane's grant true — an aggregate hash over all lanes
                   fails this, which is the locked lane_consent rationale
                 - `dross local set trusted_lane_commands ...` is rejected as an unknown
                   key (TrustedTestCommand precedent: only the grant verb, which shows the
                   line, may write a grant)
                 - `dross trust --lane <name>` prints the exact command BEFORE the store
                   is written, and refuses an unknown lane naming the declared ones
                 - a git-tracked `.dross/local.toml` makes the lane grant refuse UNREAD via
                   refuseTrackedLocal, exactly like every other grant in that file
                 - renaming a lane leaves its old grant orphaned and the new name
                   ungranted — the lookup misses and refuses, it never inherits

  t-5  `dross lane add|list|rm`
       files:    internal/cmd/lane.go, internal/cmd/lane_test.go,
                 cmd/dross/main.go, README.md
       covers:   c-9
       depends:  t-1
       desc:     New top-level `dross lane` group. `add <name> --match <glob> [--match ...]
                 --command <line>`, `list`, `rm <name>`. Writes through project.Save; a
                 README command-table row keeps the README truth-pass honest.
       contract: - `dross lane add go --match 'internal/**/*.go' --command 'go test ./...'`
                   followed by a reload yields exactly one lane; adding a second lane
                   leaves the first's name, globs and command untouched
                 - `dross lane add` with an already-declared name REFUSES rather than
                   appending — a duplicate would break t-4's name-keyed consent map
                 - `dross lane add` missing `--match` or `--command` refuses at the CLI, so
                   `dross validate` is never the first thing to notice an unusable lane
                 - `dross lane rm nope` exits non-zero naming the declared lanes; a silent
                   success here is the "tool said OK and nothing happened" failure
                 - `dross lane list` on a lane-less repo prints that none are configured and
                   exits 0 — a lane-less repo is legal (c-5), not an error

Wave 3 (depends t-3, t-4)
  t-6  Run resolved lanes for `dross test --files`
       files:    internal/cmd/test.go, internal/cmd/test_lane_test.go
       covers:   c-3, c-4
       depends:  t-3, t-4
       desc:     Add `--files` (StringArray — repeated flag, never comma-split, because a
                 comma is legal in a path). Resolve, then for each matched lane in
                 declaration order print `lane <name>: <command>` and spawn that lane's
                 line through the existing local/remote path; aggregate results with the
                 worst outcome winning and at most one remote sync per run.
       contract: - lanes `go` and `docs` declared, `--files internal/cmd/test.go`: the
                   recorded spawn argv contains the go lane's command exactly once and the
                   docs lane's command ZERO times
                 - each lane's banner, naming the lane and the exact spawned line, appears
                   BEFORE that lane's output in the captured stream — moving it after the
                   run breaks the ordering assertion, and an unlabelled transcript is a
                   result whose runner is unknown
                 - two matched lanes both red: BOTH commands are spawned (no
                   short-circuit on the first failure) and the call exits 1
                 - the line spawned for a lane is byte-identical to its configured
                   command — no `--files` paths appended, so the fingerprint t-4 checked
                   covers the line that actually ran
                 - with a remote grant present and two lanes matched, the tree is synced
                   ONCE, not once per lane; a transport failure on any lane exits 3, never
                   1 — "the host was down" must never be reported as "your code is broken"

Wave 4 (depends t-6)
  t-7  Refuse runs that measured nothing; keep lane-less repos identical
       files:    internal/cmd/test.go, internal/cmd/test_lane_refusal_test.go
       covers:   c-5, c-6, c-8
       depends:  t-6
       desc:     Three refusal paths on top of t-6's dispatch: a zero-match file set exits
                 with a new `exitNothingMeasured = 5` naming the unmatched paths; a lane
                 whose grant is absent or stale is skipped, named, and forces a non-zero
                 exit while every granted lane still runs; a repo declaring no lanes takes
                 the pre-existing requireExecConsent path and runs runtime.test_command
                 unchanged, `--files` or not.
       contract: - `--files docs/x.md` with only a go lane declared exits non-zero naming
                   `docs/x.md` and spawns NOTHING — a green exit here is precisely the
                   false-green this phase exists to stop
                 - that refusal uses its own exit code, distinct from 1 (red suite) and
                   3/4 (transport), so a caller cannot read "nothing ran" as "code is
                   broken"; a test asserts the four codes are pairwise distinct
                 - two lanes match and one has a STALE grant: the granted lane's command IS
                   spawned, the stale lane's is NOT, the message names the stale lane, and
                   the call exits non-zero — aborting at the first refusal would hide a
                   genuine red on the other lane behind a consent problem
                 - on a repo with no `[[runtime.test_lane]]`, `dross test --files a.go
                   b.go` spawns exactly `proj.Runtime.TestCommand`, compared byte for byte,
                   with no path arguments appended, and `dross test` with no flags spawns
                   the same string
                 - a lane-less repo NEVER reaches the c-8 refusal whatever `--files`
                   holds — the assertion passes a file set that would match no lane and
                   requires exit 0 on a green suite

Wave 5 (depends t-6, t-7)
  t-8  Scope the execute pre-commit gate to the task's files
       files:    assets/prompts/execute.md, internal/cmd/prompt_test_command_test.go
       covers:   c-7
       depends:  t-6, t-7
       desc:     Rewrite execute.md step 1e to run `dross test --files <task.files>`,
                 document the per-lane trust check and the nothing-measured exit code, and
                 say what to do when a run refuses (add a lane, or run the full suite
                 deliberately — never commit on a refusal). Add the grep-based drift guard
                 beside the existing prompt guards.
       contract: - execute.md's test-gate block names `dross test --files` and interpolates
                   task.files; if the instruction reverts to a bare `dross test`,
                   TestExecutePromptScopesGateToTaskFiles fails
                 - the prompt lists the nothing-measured code alongside 1/3/4 with
                   "did not run" semantics, so an agent cannot read the c-8 refusal as a
                   passed gate and commit on it
                 - the existing TestPromptsRunDrossTest still passes: the rewrite must not
                   reintroduce a raw `<runtime.test_command>` fence
                 - the guard reads assets/prompts/execute.md, so it fails in CI regardless
                   of whether `make install` re-linked the prompt locally (r-01)

## Coverage

| criterion | tasks |
|---|---|
| c-1 lane schema + validator refusal      | t-1 |
| c-2 resolve lanes, name unmatched paths  | t-2, t-3 |
| c-3 only resolved lanes' commands run    | t-6 |
| c-4 lane name + exact line printed first | t-6 |
| c-5 lane-less repo byte-identical        | t-7 |
| c-6 per-lane consent                     | t-4 (store + grant verb), t-7 (enforcement) |
| c-7 execute gate passes task files       | t-8 |
| c-8 zero-match set exits non-zero        | t-7 |
| c-9 lanes managed through the CLI        | t-5 |

9/9 criteria covered.

## Judgment calls

- **Hand-rolled `**` matcher in a new `internal/lane` rather than adding
  github.com/bmatcuk/doublestar.** Chose no new dependency (locked single-static-binary
  stack, and a glob library is supply-chain surface for ~80 lines); pinned the semantics
  with a table test instead. Rejected `filepath.Match` alone — it cannot express
  `internal/**/*.go`, so every nested source file would be a silent miss and every task
  would land in the c-8 refusal.
- **Lane commands run verbatim; matched paths are NOT appended as a selector.** Rejected
  appending them the way `dross test <selector>` does: the consent fingerprint covers the
  configured line, and a path like `-x` or `--` arriving from an agent's argv would alter
  a command the user consented to. c-4 asks for the exact line; verbatim is the only way
  the printed line and the fingerprinted line are the same string.
- **Top-level `dross lane` group, not `dross test lane …`.** `dross test` takes arbitrary
  positional selectors, so a subcommand named `lane` would shadow a legitimate
  `dross test lane/...` path selector and silently run the wrong thing.
- **New exit code 5 for "nothing measured", not a reuse of 1.** The same reasoning test.go
  already applies to 3 and 4: collapsing "the run never happened" into "the suite is red"
  is how a false green is read. Rejected exiting 1, which a caller retries or attributes
  to the code.
- **A stale or untrusted lane is skipped and named while other lanes still run.** Rejected
  aborting the whole run at the first refusal (simpler, and what a naive `return err`
  gives): it would hide a real red on a trusted lane behind a consent problem, and locked
  lane_consent says one lane going stale refuses only that lane.
- **The lane path replaces `requireExecConsent`'s test_command gate with per-lane gates.**
  Rejected keeping the global gate in front of lane runs: `CheckConsent` treats an empty
  `runtime.test_command` as a refusal (ConsentNotApplicable), so a lanes-only repo would
  be refused on every lane run — unusable exactly where lanes are most wanted. Bare
  `dross test` keeps the existing gate untouched, which is what makes c-5 trivially true.
- **`--files` is a StringArray (repeated flag), not a comma-separated StringSlice.** A
  comma is legal in a filename, and comma-splitting would silently shatter one path into
  two unmatched ones — a c-8 refusal with a nonsense cause.
- **`docs/` (trailing slash) is treated as `docs/**`.** Rejected literal matching: a
  plausible pattern returning zero lanes drops the user into the c-8 refusal with no clue
  why the lane they declared never fired.
- **Duplicate lane names refused at both the CLI (t-5) and the validator (t-1).** The
  consent map is keyed by lane name; two lanes sharing a name would give one grant
  authority over two commands. Two gates because project.toml can also be hand-edited or
  arrive in a pull.
- **Rejected: a lane-consent section in `dross doctor`.** No criterion asks for it and it
  would add a file and a layer to the phase; t-4's refusal already names the lane and the
  `dross trust --lane <name>` fix at the moment of use.
