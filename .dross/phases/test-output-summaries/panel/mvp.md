# mvp lens: test-output-summaries

Phase test-output-summaries: 6 tasks across 3 waves

```
Wave 1
  t-1  Add run-log store and test summariser
       files:    internal/runlog/runlog.go (new), internal/runlog/summary.go (new),
                 internal/runlog/runlog_test.go (new),
                 internal/runlog/testdata/gotest_red.txt (new),
                 internal/runlog/testdata/vitest_red.txt (new),
                 internal/runlog/testdata/jest_red.txt (new)
       covers:   c-1, c-2, c-3, c-4
       desc:     runlog.go: Open(root, name) creates .dross/logs/, writes .dross/logs/.gitignore
                 containing `*` on first use, creates test-<ts>.log or verify-<phase>-<leg>-<ts>.log
                 with O_EXCL (retrying on a name collision), and prunes the dir to the newest 20.
                 It also holds the IsTerminal(os.Stdout) seam test.go and verify.go share.
                 summary.go: Summarise(segments, wall, logPath) over one segment per spawned line
                 (output + exit code). The parser is picked by output signature: plain `go test`
                 counts packages, vitest/jest count tests. Failures follow the failure_bound
                 decision: up to 30 lines each, the first 10 in full, then "+N more failing — see
                 <log>". A segment no parser recognises gets c-4's fallback.
       contract: - green go output (3 `ok`, 1 `? [no test files]`) → one summary with package
                   counts, wall-clock and log path, and no line starting `ok ` or `?` (c-2)
                 - gotest_red.txt → names the leaf subtest `TestX/sub` with its own output, prints
                   the `[build failed]` package's compile errors, and counts a `FAIL pkg` that has
                   no `--- FAIL` block (timeout panic) as a failure with its own tail (c-3)
                 - 12 failures of 50 lines each → 10 blocks of ≤30 lines each, then
                   "+2 more failing — see <log>"
                 - exit 1 over output where every package is `ok` → no green line; prints
                   "exit 1", the last 40 lines and the log path (c-4 never-green)
                 - unrecognised output (pytest dots) at exit 0 → exit status, last 40 lines,
                   log path
                 - vitest_red.txt → test counts ("1 failed | 4 passed"), not file counts, plus
                   the `suite > name` failure; jest_red.txt → "1 failed, 4 passed" plus
                   `suite › name`
                 - first Open writes .dross/logs/.gitignore == "*\n" and leaves the repo's
                   .gitignore byte-identical; the 21st Open leaves the 20 newest; two Opens in the
                   same instant give two distinct files
       depends:  —

  t-2  Route mutation tool streams through a sink
       files:    internal/mutation/launcher.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/stream_test.go (new)
       covers:   c-5
       desc:     Add a per-tool stream seam next to launcherLog, defaulting to os.Stderr. The
                 three `io.MultiWriter(os.Stderr, head)` tees write to it instead. headBuffer and
                 the failure-point printHead are unchanged.
       contract: - table test over gremlins, stryker and stryker-net: with the build-cmd seam
                   emitting `MUTANT-LINE` and the stream seam set to a buffer, the buffer holds
                   MUTANT-LINE, captured os.Stderr does not, and headBuffer still holds it (the
                   failure re-print is not starved)
                 - seam left unset → MUTANT-LINE reaches os.Stderr (the gremlins/stryker
                   *_failure_test.go suites stay green unmodified)
       depends:  —

  t-3  Print unchanged markers for rule/interaction show
       files:    internal/gatestate/shown.go (new), internal/cmd/rule.go,
                 internal/cmd/interaction.go, internal/cmd/reentry.go,
                 internal/cmd/show_marker_test.go (new)
       covers:   c-6, c-7
       desc:     The session key is CLAUDE_CODE_SESSION_ID. The record is
                 .dross/gate/shown-<sha(session)>.json, holding the sha256 of the last full print
                 per kind (rules, playbook). A repeat with an equal digest prints one line,
                 "rules unchanged" or "playbook unchanged". `dross reentry` (the SessionStart hook,
                 which fires on /clear and on compaction) removes every shown record. These cases
                 always print the full block: an empty session id, CLAUDE_CODE_CHILD_SESSION set
                 (a subagent context), or the SessionStart → `dross reentry` hook not wired
                 (checked with hooks.MergeHook on userSettingsPath being a no-op).
       contract: - same session id, second `rule show` → output is exactly one line containing
                   "rules unchanged"; the same for `interaction show` and "playbook unchanged"
                 - rules.toml edited between the calls → second call prints the full <rules> block
                 - Reentry() run between the calls (the /clear and compaction path) → full block
                 - CLAUDE_CODE_SESSION_ID unset, or a different id → full block both times
                 - CLAUDE_CODE_CHILD_SESSION=1 → full block on the repeat
                 - HOME settings.json without the SessionStart reentry hook → full block on the
                   repeat
                 - reentry's golden (testdata/cli_surface/output/reentry.txt) is unchanged
       depends:  —

Wave 2 (depends t-1; t-5 also depends t-2)
  t-4  Summarise dross test runs into the log
       files:    internal/cmd/test.go, internal/cmd/test_summary_test.go (new)
       covers:   c-1, c-2, c-3, c-4
       desc:     RunE opens a runlog test log for every run: bare, selector, --files, a lanes-only
                 bare run, local and remote. The spawn sites (runTest, runOneLane,
                 runLanePrepare, syncTreeTo, runRemoteLine) write through per-run writers that
                 read os.Stdout at call time. When stdout is not a TTY the writers go to the log
                 only, and one segment per spawned line feeds runlog.Summarise. On a TTY they tee
                 terminal+log as today. The log path is always printed. The returned error and
                 its exit code are untouched. The Long help's "Output streams as it arrives" is
                 corrected.
       contract: - bare run, spawnLocal fake printing go-green output, under captureStdout →
                   stdout holds the package-count summary and "log: .dross/logs/test-", and lacks
                   the fake's `ok  \t` lines; the log file holds them (c-1, c-2)
                 - selector run, --files lane run and remote run (spawnRemote fake) each create a
                   test-*.log and print its path (c-1's every-run clause)
                 - red fake (`--- FAIL: TestX` + output, exit 1) → ExitCode 1 and stdout names
                   TestX and its output line (c-3)
                 - spawnRemote returning a transport-class exit → ExitCode 3 with no green line
                   and the log path printed; partial-transfer exit → ExitCode 4 (c-4)
                 - two-lane run (go-green lane + unrecognised-output lane, both exit 0) → go
                   package counts AND the other lane's exit status + tail; a parsed lane cannot
                   mask an unparsed one
                 - IsTerminal seam true → the fake's output appears on stdout live and in the
                   log; the path is printed
                 - a summary-mode full green still saves .dross/gate/green.json (log writes under
                   .dross/ do not move treefp.WorkingTree)
       depends:  t-1

  t-5  Summarise verify mutation legs per package
       files:    internal/cmd/verify.go, internal/cmd/verify_summary_test.go (new)
       covers:   c-5
       desc:     Each attached leg gets a runlog verify-<phase>-<tool>-<ts>.log through t-2's
                 seam: log only when stdout is not a TTY, stderr+log when it is. The path is
                 printed per leg. printVerifySummary adds per-package rows (lr.Mutation.Files
                 grouped by dir: killed/survived/not_covered/timeout) and every survivor as
                 file:line. These print from finishVerify, so attached and collected runs share
                 them. collectDetachedFrom fetches <rec.RunDir>/log into a verify log through the
                 detach fetch seam; a failed log fetch prints a note and never fails the collect.
       contract: - attached verify, fake adapter writing "LIVED internal/x/a.go:12" lines to
                   the sink, stdout not a TTY → neither stdout nor stderr holds any LIVED/KILLED
                   line; .dross/logs/verify-<phase>-<tool>-*.log holds them; stdout has
                   "internal/x: killed=2 survived=1 not_covered=0 timeout=0" (or the
                   implementation's equivalent row) and "internal/x/a.go:12"
                 - `dross verify results` (collect) → a verify-<phase>-gremlins-*.log is written
                   from the fetched host log and its path printed; per-package rows and
                   survivor file:line printed
                 - fetch seam failing for the log only → collect still writes tests.json and
                   prints the note, with the exit status unchanged
                 - IsTerminal seam true → the sink's lines also reach stderr
       depends:  t-1, t-2

Wave 3 (depends t-4, t-5)
  t-6  Record before/after output and flow cost
       files:    .dross/phases/test-output-summaries/flow-cost.toml (new),
                 internal/cmd/flowcost_record_test.go
       covers:   c-8
       desc:     Build the before binary from the base commit 54ad9064 in a temp worktree; the
                 after binary is `make install` (rule r-01). Use one scratch Go repo with several
                 packages and gremlins. Byte counts (wc -c) for a green `dross test`, a red one
                 (one broken assertion) and one attached verify mutation leg, on each binary. The
                 human drives a one-task pair /dross-execute on each binary. Turns, turn_tools,
                 wall-clock and dross_calls for its test→commit step (§1e `dross test` through
                 the commit) are extracted with jq, structurally, as in context-boundaries'
                 record. [scenario], [before] and [after] are written in one commit. Extend the
                 test with this phase's byte fields.
       contract: - TestFlowCostRecords (existing glob) fails if this record lacks scenario keys,
                   a complete before/after leg, len(turn_tools) == agent_turns, or if after's
                   dross_calls hold a call before's do not (zero added round-trips), or if
                   both legs share a binary_version
                 - new phase branch: fails unless green_bytes, red_bytes and verify_bytes are
                   > 0 on both legs, after.green_bytes < before.green_bytes and
                   after.verify_bytes < before.verify_bytes
       depends:  t-4, t-5
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (log store, .gitignore, retention), t-4 (every run path writes it and prints the path) |
| c-2 | t-1 (green summary, counts per runner), t-4 (wired into the non-TTY path) |
| c-3 | t-1 (failure blocks, bound, build errors), t-4 (wired, remote and lanes included) |
| c-4 | t-1 (fallback, never-green on non-zero), t-4 (exit 3/4 preserved under summary) |
| c-5 | t-2 (tool stream reaches a sink, not stderr), t-5 (per-leg log, per-package rows, survivors, collect path) |
| c-6 | t-3 |
| c-7 | t-3 |
| c-8 | t-6 |

8/8 criteria covered. Every task maps to at least one criterion.

## Judgment calls

- **One `internal/runlog` package for the log store, the TTY seam and the summariser.** I rejected a separate store task. It is one layer, both wirings need all three parts, and keeping them together lets t-4 and t-5 run in parallel in wave 2 rather than chaining.
- **The parser is picked per spawned segment, by output signature.** I rejected two alternatives. Picking by command line would miss `make test` wrapping `go test`. Parsing the whole combined log would let a parsed lane's green hide an unparsed lane.
- **On a TTY, `dross test` streams and prints only the log path, with no closing summary.** Locked tty_behaviour says to stream as today, and c-1 still requires the path.
- **Mutation streams go through a per-tool seam in `internal/mutation`.** I rejected swapping os.Stderr from verify.go. The locked log_retention decision wants one file per leg, so routing has to happen per adapter.
- **Collect fetches the host's `<runDir>/log` into the local log.** I rejected leaving collected runs without a log, because c-5 names collected runs explicitly. A missing host log only prints a note, because the reports are the evidence and the log is a convenience.
- **The c-6 session key is CLAUDE_CODE_SESSION_ID.** It is present in the agent's Bash env (observed, value not printed). Reset comes from `dross reentry` wiping all records, since SessionStart fires on /clear and on compaction. I rejected two alternatives. A PreCompact-only reset misses /clear. Scanning the transcript for compact_boundary is more machinery. Wiping all records also stops /clear depending on whether the Bash env's session id updates.
- **The marker is only emitted when the SessionStart reentry hook is wired.** Without the hook, "a compaction prints the full block" would silently be false. The check reuses hooks.MergeHook, so no new parser is needed.
- **CLAUDE_CODE_CHILD_SESSION set means a full print.** A subagent's context never saw the block. Caveat: I observed the variable =1 only inside a subagent. The executor must confirm it is unset in a main session. If it is set there too, use agent-id keying instead, or c-6 never fires.
- **c-6 and c-7 are one task.** I rejected a split because c-7 is a 3-line mirror of the same helper and would be a <10-minute task on its own. t-3 touches 5 files, but interaction.go and reentry.go are tiny edits.
- **c-8 is one final task that measures before (base-commit build) and after together.** I rejected the usual wave-1 "measure before" task because TestFlowCostRecords globs every `phases/*/flow-cost.toml`. A before-only record would fail it and turn every intermediate task's gate red.
- **No edits to execute.md or verify.md.** I rejected rewording execute.md's "streaming output as it arrives". No criterion needs it, prompt-partials (next phase) rewrites execute.md, and a prompt edit would force the before leg to run against old prompts too. With no prompt edits, the two legs differ only by binary. test.go's own Long help is fixed inside t-4.
- **No gitignore.go change.** Locked log_location makes the dir ignore itself, and treefp.WorkingTree already excludes `.dross/`. Log writes therefore can never stale the green record or verify freshness. t-4 asserts this.
- **c-8 gate: green and verify bytes must drop, red bytes are recorded but not gated.** A bounded red can come out at about the size of a tiny suite's full stream, and that would make the gate fail on the choice of scenario rather than on a regression. Turns and wall-clock stay recorded but not gated, per the 2026-10-08 rule TestFlowCostRecords already encodes.
