# mvp lens — test-lane-config

Phase test-lane-config — 5 tasks across 3 waves

Wave 1
  t-1  Add test_lane schema, validation, resolution
       files:    internal/project/project.go, internal/project/testlane.go,
                 internal/project/testlane_test.go, internal/cmd/validate.go
       covers:   c-1, c-2
       desc:     Add `TestLane []TestLane` to `Runtime` (name / match []string / command,
                 toml+json tags per json_tag_parity_test.go). testlane.go holds
                 `LaneProblems(p) []string` (a lane missing name, match or command yields a
                 problem naming that lane) and `ResolveLanes(lanes, paths) (matched []TestLane,
                 unmatched []string)` with a `**`-capable glob matcher. validate.go appends
                 LaneProblems to its problems slice.
       contract: - a lane with an empty command (or empty name, or empty match list) makes
                   `dross validate` emit a problem naming that lane; a lane with all three
                   emits none
                 - ResolveLanes over lanes {go: "internal/**/*.go", docs: "docs/**"} with
                   paths ["docs/x.md"] returns only the docs lane and an empty unmatched list
                 - ResolveLanes with ["Makefile"] against those lanes returns zero lanes and
                   ["Makefile"] as unmatched — the path is reported, never dropped
                 - a path matching two lanes returns both, once each, in declaration order
                 - "internal/**/*.go" matches "internal/cmd/test.go" (the `**` case
                   path.Match alone cannot do)

Wave 2 (depends t-1)
  t-2  Run resolved lanes for dross test --files
       files:    internal/cmd/test.go, internal/cmd/test_lanes_test.go
       covers:   c-3, c-4, c-5, c-8
       desc:     Add `--files` (repeatable/comma-split) to `dross test`. With lanes declared
                 and --files given, resolve, print "lane <name>: <exact command line>" then
                 spawn that lane's command through the existing spawnLocal seam, in
                 declaration order; any red lane fails the run. Zero matched lanes returns an
                 ExitCodeError naming the unmatched paths. No lanes declared, or no --files,
                 falls through to today's testCommandLine path untouched.
       contract: - with a go lane and a docs lane declared, `dross test --files
                   internal/cmd/test.go` leaves spawnRecorder holding exactly one line — the
                   go lane's — and the docs lane's command appears in zero recorded spawns
                 - the line printed before each lane's output contains the lane name and a
                   command string byte-identical to the argv the recorder captured for it
                 - with no [[runtime.test_lane]] present, both `dross test` and `dross test
                   --files internal/cmd/test.go` spawn exactly runtime.test_command,
                   byte-identical to the consented string, and no unmatched refusal fires
                 - `dross test --files README.md` where no lane globs it spawns nothing,
                   returns a non-zero ExitCode, and the message contains "README.md"
                 - --files with one matched and one unmatched path still spawns the matched
                   lane once and prints the unmatched path

  t-3  Add dross test lane add|list|remove
       files:    internal/cmd/testlane.go, internal/cmd/testlane_test.go,
                 internal/cmd/test.go
       covers:   c-9
       desc:     `lane` subcommand under `dross test`, attached in Test(). add takes
                 <name> --match (repeatable) --command; list prints each lane's name, globs
                 and command; remove deletes by name. All three go through project.Load /
                 (*Project).Save.
       contract: - `test lane add go --match 'internal/**/*.go' --command 'go test ./...'`
                   produces a project.toml that project.Load reads back with that lane's
                   three fields intact; a second add of name "go" errors instead of writing a
                   duplicate entry
                 - `test lane remove go` removes only that entry — a second declared lane and
                   the [runtime] scalars (test_command, mode) survive the Load→Save round trip
                 - `test lane add go --match '*.go'` with no --command exits non-zero and
                   leaves project.toml byte-for-byte unchanged
                 - `test lane remove nope` for a lane that does not exist exits non-zero and
                   names the known lanes

Wave 3
  t-4  Gate each lane on its own consent  (depends t-2)
       files:    internal/cmd/local.go, internal/cmd/trust.go, internal/cmd/test.go,
                 internal/cmd/testlane_consent_test.go
       covers:   c-6
       desc:     Add `TrustedLaneCommands map[string]string` (lane name → sha256 of its
                 command) to localStore, absent from localKeys. `dross trust --lane <name>`
                 prints the command then records that one entry; `--lane <name> --check`
                 reports granted/absent/stale. The lane dispatch in t-2 checks each matched
                 lane before spawning it: an ungranted lane is skipped with a named refusal
                 and makes the whole run exit non-zero, granted lanes still run. A --files
                 lane run gates per lane instead of on runtime.test_command.
       contract: - two matched lanes, one granted and one not: the granted lane's command is
                   spawned exactly once, the ungranted lane's command zero times, the error
                   names the ungranted lane, and ExitCode is non-zero
                 - changing a granted lane's command by one byte makes that lane refuse as
                   stale on the next run while the second lane's grant still spawns
                 - `dross trust --lane docs` prints the docs command before writing and leaves
                   trusted_test_command and every other lane's fingerprint unchanged
                 - `dross local set trusted_lane_commands x` fails as an unknown key — the
                   grant is reachable only through `dross trust --lane`

  t-5  Pass the task's files to the execute gate  (depends t-2)
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       covers:   c-7
       desc:     Rewrite execute.md's §1e test-gate block to run
                 `dross test --files <task.files from plan.toml>`, and add one line saying an
                 unmatched-path refusal means the gate measured nothing and is never a reason
                 to commit.
       contract: - execute.md's test-gate block contains `dross test --files` fed from the
                   current task's plan.toml files; a revert to a bare `dross test` there fails
                   the new assertion in execute_prompt_test.go
                 - TestPromptsRunDrossTest still passes — execute.md names `dross test` and
                   carries no bare `<runtime.test_command>` line

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-1 |
| c-3 | t-2 |
| c-4 | t-2 |
| c-5 | t-2 |
| c-6 | t-4 |
| c-7 | t-5 |
| c-8 | t-2 |
| c-9 | t-3 |

9/9 criteria covered.

## Judgment calls

- Merged schema + validator + resolver into one wave-1 task instead of three. Resolution is a
  pure function over a struct that cannot exist without it, and validate.go's hook is four
  added lines; three tasks would be three commits of the same idea.
- Hand-rolled `**` matcher in internal/project/testlane.go, rejecting both a doublestar
  dependency (go.mod has five direct deps and ships a static binary) and a new internal/lane
  package (one consumer). path.Match, already used in gitignore.go, cannot cross directory
  separators, so `internal/**/*.go` needs its own segment walk.
- `dross test lane …` as a subcommand of `test`, rejecting a top-level `dross lane` and
  `dross project lane`. Verified in cobra 1.8.1 (command.go:1141) that a nil `Args` defaults
  to ArbitraryArgs, so attaching a subcommand keeps `dross test ./internal/cmd/...` selectors
  working — the collision that would have forced a different home does not exist.
- Consent stored as a name→fingerprint map, not a reuse of the flat comma-separated
  trusted_run_commands set. The locked lane_consent decision says map; the map also lets the
  refusal distinguish "never trusted" from "changed since trusted", which the flat set cannot.
- A `--files` lane run gates per lane and does NOT also require the global
  runtime.test_command grant. Requiring both would brick a lane repo that sets no
  test_command, and the bare path keeps requireExecConsent unchanged so c-5 stays trivially
  true.
- No README task. cmd/dross/main_test.go's guard runs README→cobra only, so new subcommands
  force no doc edit, and no criterion asks for one. If the phase wants README parity it is a
  quick task, not a wave.
- c-7 kept as its own wave-3 task rather than folded into t-2. The prompt has to quote the
  final flag and exit-status surface; writing it alongside the code that defines it invites a
  prompt that documents an intention.
