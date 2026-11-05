# Panel draft — verification lens (test contracts first)

Method: for each criterion I wrote the test that would prove it, then cut the
smallest task that makes that test satisfiable. Tasks are shaped by where a
contract can be asserted cheaply (a pure package with fixtures before the
1.2k-line `internal/cmd/test.go`), and every task names the test that goes red
if its surface regresses.

```
Phase test-output-summaries — 13 tasks across 5 waves

Wave 1
  t-1  Record before-numbers on the pre-change binary
       files:    .dross/phases/test-output-summaries/flow-cost-before.toml
       covers:   c-8
       desc:     Build dross from changes.json base_commit (54ad906) into a temp dir, then in a scratch
                 Go repo (dross init, ~6 packages, one trivial task, no remote grant) record stdout+stderr
                 bytes (`2>&1 | wc -c`) of a green `dross test`, a red one (one broken assertion) and
                 `dross verify <scratch-phase>` (local gremlins v0.6.0, one ~20-mutant package). The human
                 then drives one pair-mode /dross-execute in that repo (old binary on PATH, prompts not yet
                 re-installed) and the §1e `dross test` → §1f `git commit` leg is counted with the
                 context-boundaries jq method: agent_turns, turn_tools, wall_clock_s, dross_calls, log_reads.
                 Written to a draft file that TestFlowCostRecords' `flow-cost.toml` glob does not match.
                 Runs before any task in this phase runs `make install`.
       contract: t-13's TestFlowCostRecords/test-output-summaries fails if [before] is absent, if any of
                 bytes.green_test / red_test / verify_leg is 0, if agent_turns != len(turn_tools), or if
                 before.binary_version == after.binary_version. That last check catches a before leg taken
                 after this phase's code was installed.

  t-2  Add runlog package and stdout-TTY seam
       files:    internal/runlog/runlog.go, internal/runlog/runlog_test.go,
                 internal/cmd/output_mode.go, internal/cmd/output_mode_test.go
       covers:   c-1
       desc:     runlog.New(repoDir, name) returns a lazy writer. Its first Write creates .dross/logs/, writes
                 a `*` .gitignore there if none exists (refusing one that is not a regular file, like
                 debugsession.ensureIgnored), O_EXCL-creates <name>-<ts>.log and prunes the dir to the newest
                 20 logs. Path() returns the repo-relative path, or "" if nothing was written. Name helpers
                 build test-<ts> and verify-<phase>-<leg>-<ts>. output_mode.go holds the
                 `stdoutIsTerminal` seam (golang.org/x/term), which t-9 and t-11 both read.
       contract: TestLogSelfIgnores: in a `git init` temp repo, after one Write `git check-ignore -q` on the
                 log exits 0 and `git status --porcelain -uall` prints nothing.
                 TestUnwrittenLogCreatesNothing: New + Close with no Write leaves no .dross/logs/ dir.
                 TestPruneKeepsNewest20: 21 logs with test- and verify- prefixes mixed and the clock
                 ascending leave exactly 20 .log files. The oldest by timestamp is gone, whatever its
                 prefix, and .gitignore survives.
                 TestSameInstantNoClobber: two logs on one injected instant get two paths, each holding only
                 its own bytes.
                 TestGitignoreDirRefused: a .gitignore that is a directory makes the first Write return an
                 error.
                 TestPipeIsNotTerminal: with an os.Pipe swapped into os.Stdout, stdoutIsTerminal() is false.

  t-3  Add testsummary core: render, bounds, fallback
       files:    internal/testsummary/summary.go, internal/testsummary/render.go,
                 internal/testsummary/render_test.go
       covers:   c-2, c-3, c-4
       desc:     Types: Result{Runner, Unit, Passed, Failed, Skipped, NoTests, Failures[]{Name, Lines}} and
                 Segment{Label, Result, Parsed, Tail}. Detect(text) strips ANSI escapes, then asks each
                 registered parser to claim the output. Render(segments, exitCode, wall, logPath) applies
                 the locked failure_bound (30 lines per failure, the first 10 failures in full, then
                 `+N more failing — see <log>`) and the c-4 fallback (exit status, last 40 lines, log path).
                 The first line is PASS only when exitCode == 0.
       contract: TestGreenRendersCountsOnly: a 41-package/3-no-test Result with 72s prints one line holding
                 "41", "packages", "1m12s" and the log path, and nothing else.
                 TestFailureBoundedAt30: a 50-line failure prints its first 30 lines plus a truncation
                 marker, and line 31 is absent.
                 TestTenFailuresThenMore: 12 failures print blocks for names 1–10. Names 11 and 12 are
                 absent and "+2 more failing — see <path>" is present.
                 TestUnparsedTail40: 100 unclaimed lines with exit 1 print "exit 1", lines 61–100 and the
                 path. Line 60 is absent.
                 TestNonZeroNeverPass: a parsed Result with Failed=0 and exit 1 prints the tail, and the PASS
                 token appears nowhere.
                 TestRunDidNotHappenNeverCounts: exit 3 and exit 4 with output that parses green print the
                 exit status and the tail, never counts.
                 TestDetectStripsANSI: a stub parser registered by the test receives no ESC byte when fed
                 colored input.

  t-4  Route gremlins tool stream through a sink
       files:    internal/mutation/adapter.go, internal/mutation/gremlins.go,
                 internal/mutation/gremlins_stream_test.go
       covers:   c-5
       desc:     Add `Streamer interface{ SetStream(io.Writer) }` to adapter.go. In Gremlins.Run, the tool's
                 stdout/stderr and the per-package invocation echo go to the stream (default os.Stderr). The
                 bounded printHead failure diagnosis and the "skipped" lines stay on os.Stderr.
       contract: TestGremlinsStreamToSink: the gremlinsBuildCmd seam runs `sh -c` that prints
                 "KILLED …"/"LIVED …" lines and writes a valid report. With SetStream(buf), captureStderr
                 holds neither token and buf holds both, and the Report counts are unchanged.
                 TestGremlinsRemoteArmStreamToSink: the same, via scriptRemote with g.Remote set.
                 TestGremlinsNilStreamKeepsStderr: with no SetStream, stderr carries the tool lines (TTY path
                 unchanged).
                 TestGremlinsFailureHeadStaysOnStderr: a reportless non-zero exit still prints the bounded
                 head on stderr.

  t-5  Add shown-marker store; reentry bumps epoch
       files:    internal/gatestate/shown.go, internal/gatestate/shown_test.go,
                 internal/cmd/reentry.go, internal/cmd/reentry_test.go
       covers:   c-6, c-7
       desc:     .dross/gate/shown/ holds an epoch file and one digest per (hashed session id, kind).
                 Same(root, sess, kind, digest) is true only when an epoch exists, the session is non-empty
                 and the recorded digest is for this epoch. NewEpoch(root) replaces the epoch. `dross
                 reentry` (the SessionStart hook, which fires on startup, resume, /clear and compaction)
                 calls NewEpoch as soon as FindRoot succeeds, before it loads the project.
       contract: TestShownNeedsEpoch: Record then Same with no epoch file is false, so a repo whose hooks
                 were never wired never gets a marker.
                 TestShownMatchesOnlySameKey: after NewEpoch + Record(s1, rules, d), Same is true for (s1,
                 rules, d) and false for (s1, rules, d2), (s2, rules, d) and (s1, playbook, d).
                 TestNewEpochForgetsAll: Record, then NewEpoch, then Same is false.
                 TestEmptySessionNeverRecords: Record("") creates no file under .dross/gate/shown.
                 TestSessionIDCannotEscape: Record("../../x") creates files only inside .dross/gate/shown.
                 TestReentryBumpsEpoch: Record, then run Reentry(), then Same is false. The reentry.txt
                 output golden still matches byte-for-byte.

Wave 2 (t-6, t-7 depend t-3 · t-8 depends t-4 · t-9 depends t-2, t-4 · t-10 depends t-5)
  t-6  Parse plain go test output
       files:    internal/testsummary/gotest.go, internal/testsummary/gotest_test.go,
                 internal/testsummary/testdata/gomod/ (fixture module: pass, fail, table, panic, broken),
                 internal/testsummary/testdata/go_green.txt, go_red.txt, go_build_failed.txt, go_timeout.txt
       covers:   c-2, c-3
       desc:     A parser that claims `ok`/`FAIL`/`?` package lines. Each top-level `--- FAIL:` block is one
                 failure, with its subtests inside it. A FAIL package with no `--- FAIL` block (build failed,
                 setup failed, timeout panic, TestMain exit) is one failure named by its package. Fixtures
                 are real `go test -count=1 ./...` captures over testdata/gomod. The capture command is
                 noted in the test file.
       contract: TestGoGreenCounts: go_green.txt gives Unit=packages, Passed = its `ok` line count,
                 NoTests = its `?` count, Failed = 0.
                 TestGoRedNamesOwnLines: go_red.txt yields exactly the failure names {…/fail.TestAdd,
                 …/table.TestTable}. TestAdd's lines include its `fail_test.go:` assertion line and none of
                 TestTable's.
                 TestGoTableKeepsSubtests: TestTable's block contains "--- FAIL: TestTable/".
                 TestGoBuildFailedIsFailure: go_build_failed.txt gives a failure for the broken package
                 holding the "undefined:" compile line, and Failed counts it.
                 TestGoPanicNamedByTest: the panicking test's block has "panic:" within its first 30 lines.
                 TestGoTimeoutNamedByPackage: go_timeout.txt gives a package-named failure that starts at
                 "panic: test timed out".
                 TestGoDoesNotClaimForeign: the parser does not claim 40 lines of `make` output.

  t-7  Parse vitest and jest output
       files:    internal/testsummary/jsrunner.go, internal/testsummary/jsrunner_test.go,
                 internal/testsummary/testdata/vitest_green.txt, vitest_red.txt, vitest_suite_error.txt,
                 vitest_red_color.txt, jest_green.txt, jest_red.txt
       covers:   c-2, c-3
       desc:     vitest (`Test Files`/`Tests` summary lines and `FAIL file > suite > name` blocks) and jest
                 (`Tests: N failed, M passed, T total` and `● suite › name` blocks), with Unit=tests. vitest
                 captures come from internal/mutation/testdata/ts-project (`vitest run`, plus one scratch
                 copy with a broken assertion). jest captures come from a scratch project pinned to an exact
                 version and installed with --ignore-scripts. The version is recorded in each fixture's
                 header comment.
       contract: TestVitestGreenCounts: Passed equals the N in vitest_green.txt's "Tests  N passed".
                 TestVitestRedFailure: one failure named "src/tally.test.ts > …" whose lines hold the
                 AssertionError and the expected/received lines. Failed matches "Tests  1 failed".
                 TestVitestSuiteErrorIsFailure: a file that failed to import counts as a failure with its
                 error lines (the JS equivalent of build failed).
                 TestVitestColorSameResult: vitest_red_color.txt (FORCE_COLOR=1) gives a Result equal to
                 vitest_red.txt's.
                 TestJestCounts and TestJestRedFailure: counts match the "Tests:" line, and the "● suite ›
                 name" block is one failure.
                 TestJSDoesNotClaimGo: the js parser does not claim go_green.txt.

  t-8  Route stryker and stryker-net streams to sink
       files:    internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/stryker_stream_test.go
       covers:   c-5
       desc:     Implement Streamer on Stryker and StrykerNet the way t-4 did for Gremlins. Tool output goes
                 to the stream. The printHead diagnosis stays on os.Stderr.
       contract: TestStrykerStreamToSink and TestStrykerNetStreamToSink: with the strykerBuildCmd /
                 strykerNetBuildCmd seam printing "[Survived] …" lines and writing a report, captureStderr
                 has no "[Survived]", the sink has every line, and the Report is unchanged.
                 TestStrykerNilStreamKeepsStderr: stderr still carries the tool lines when no stream is set.

  t-9  Verify legs: log file, per-package summary
       files:    internal/cmd/verify.go, internal/cmd/verify_output.go, internal/cmd/verify_output_test.go
       covers:   c-5
       desc:     Before RunScoped, give every Streamer adapter a runlog writer for
                 verify-<phase>-<adapter.Name()>-<ts>.log. On a TTY it is teed to stderr. Because it is lazy,
                 a leg that never dispatches opens no log. printVerifySummary gains per-package rows (package
                 = the mutated file's directory, from Mutation.Files) with
                 killed/survived/not_covered/timed_out, keeps the score, lists each kept survivor's
                 file:line, and prints each leg's log path. `verify results` fetches the host run dir's `log`
                 into a verify-<phase>-gremlins-<ts>.log through a new detachFetchLog seam. The seam is built
                 on remote.FetchArgs and runDetachArgv, so no new spawn site appears. A failed log fetch
                 prints one line and does not change the collect's exit code.
       contract: TestVerifyPerPackageRows: a Tests value with Files over internal/a/x.go, internal/a/y.go and
                 internal/b/z.go prints one row each for internal/a (x+y summed) and internal/b. Row sums
                 equal the leg totals, and no per-file row appears.
                 TestVerifyListsSurvivorLocations: survivors internal/a/x.go:12 and internal/b/z.go:40 each
                 print as exactly one "file:line" line.
                 TestVerifyLegStreamToLog: configuredAdaptersFn returns a fake Streamer that writes
                 "LIVED foo". `dross verify p1` (non-TTY) puts no "LIVED" in stdout or stderr,
                 .dross/logs/verify-p1-fake-*.log contains it, and stdout names that path.
                 TestVerifyIdleLegOpensNoLog: with two adapters and only one dispatched, exactly one verify
                 log exists.
                 TestVerifyTTYTeesStream: with stdoutIsTerminal stubbed true, stderr carries "LIVED foo" and
                 the log has it too.
                 TestCollectFetchesHostLog: detachFetchLog records <runDir>/log →
                 .dross/logs/verify-<phase>-gremlins-*.log, and the output names it. If the fetch fails,
                 collect still exits 0.

  t-10 Mark repeat rule/interaction show
       files:    internal/cmd/rule.go, internal/cmd/interaction.go, internal/cmd/show_marker_test.go,
                 assets/prompts/rule.md
       covers:   c-6, c-7
       desc:     `rule show` and `interaction show` digest the bytes they render. If gatestate.Same holds
                 for CLAUDE_CODE_SESSION_ID, they print one marker line ("rules unchanged" / "playbook
                 unchanged"). Otherwise they print in full and Record the digest. A non-empty
                 CLAUDE_CODE_CHILD_SESSION counts as no session id. `rule list --merged` never marks.
                 rule.md's "show what claude sees" row moves to `dross rule list --merged`. Tests pin both env
                 vars with t.Setenv so a suite run from inside Claude Code cannot inherit a live id.
       contract: TestRuleShowRepeatMarks: with s1 and an epoch, the first call prints the full block. The
                 second prints exactly one line containing "rules unchanged", under 120 bytes.
                 TestRuleShowFullAfter{RuleAdd,GlobalEdit,Reentry,OtherSession}: each of `rule add`, a
                 changed global rules.toml under ruleCovFakeHome, a Reentry() run (stands in for /clear and
                 compaction) or session s2 between the calls makes the second call print the full block.
                 TestRuleShowNoSessionAlwaysFull: with the id unset, both calls print in full and nothing is
                 created under .dross/gate/shown.
                 TestRuleShowChildSessionAlwaysFull: with CLAUDE_CODE_CHILD_SESSION=1, both calls print in
                 full.
                 TestRuleListMergedNeverMarks: `rule list --merged` after a show prints the full block.
                 TestInteractionShowRepeatMarks plus the same After/NoSession matrix give "playbook
                 unchanged", and TestInteractionShowEmitsPlaybook still passes.
                 TestRulePromptShowUsesListMerged: rule.md's show row names `dross rule list --merged`.

Wave 3 (depends t-2, t-3, t-6, t-7)
  t-11 Wire dross test through log + summary
       files:    internal/cmd/test.go, internal/cmd/test_output.go, internal/cmd/test_output_test.go
       covers:   c-1, c-2, c-3, c-4
       desc:     Thread a per-run sink through every spawn in test.go: the rsync in syncTreeTo,
                 runRemoteLine, runOneLane, runLanePrepare and runTest. The sink writes a `$ <line>` header
                 and each spawn's combined stdout/stderr into one runlog test-<ts>.log, buffers each spawn
                 as a Segment, and tees to os.Stdout when stdoutIsTerminal. Off a TTY, it renders
                 testsummary at the end using ExitCode(err) and the wall clock from a `testNow` seam. On a
                 TTY it prints only the log path. A run refused before its first spawn writes no log. If
                 the log cannot be opened, the run streams as today and prints one warning, with the exit
                 status unchanged. The Long help drops "Output streams as it arrives". green.finish(err)
                 gets the same err.
       contract: TestEveryRunShapeWritesOneLog: a table over {bare, selector, --files lane, remote}. A writing
                 spawn recorder emits OUT-MARK to stdout and ERR-MARK to stderr. Each shape leaves exactly
                 one new test-*.log holding both marks, and stdout contains its repo-relative path. In the
                 remote shape the log also holds the rsync segment.
                 TestGreenSummaryNoPackageLines: spawning go_green.txt with exit nil prints PASS and the
                 counts. No `ok  \t` line from the fixture reaches stdout, and stdout is ≤ 300 bytes.
                 TestRedSummaryNamesFailures: go_red.txt with fakeExit{1} gives ExitCode 1 and prints each
                 failure name with its assertion line. No passing package's line appears.
                 TestSummaryKeepsExitCode: a table where fakeExit{1}→1, remote 255→3, rsync 23→4, a
                 prepare failure→7 and a missing toolchain→8 each keep the same ExitCode as before this
                 task, and none prints PASS.
                 TestUnparsedRunnerTail: 100 numbered `make:` lines with exit 1 print "exit 1", lines 61–100
                 and the log path. Line 60 is absent.
                 TestTTYStreamsLive: with stdoutIsTerminal stubbed true, the fixture lines appear verbatim on
                 stdout and in the log, no PASS/summary block appears, and the log path is printed.
                 TestNoStreamingFlag: Test().Flags() defines exactly local, files and wait. Any added flag
                 fails (locked tty_behaviour: no opt-out).
                 TestMixedLaneSummary: lanes go (go_green) and web (vitest_red) via --files share one log.
                 Stdout counts both lanes, names the vitest failure, and exit is 1.
                 TestSummaryGreenStillRecordsGate: a green full run in summary mode leaves
                 gatestate.LoadGreen non-nil.
                 TestRefusalWritesNoLog: an untrusted run leaves .dross/logs/ absent, so a refused run
                 never prunes a red log.
                 TestWallClockFromSeam: testNow advancing 75s prints "1m15s".

Wave 4 (depends t-11)
  t-12 Prompts describe summary, no log read
       files:    assets/prompts/execute.md, assets/prompts/quick.md,
                 internal/cmd/prompt_test_command_test.go
       covers:   c-3, c-8
       desc:     §1e of execute.md and quick.md: replace "streaming output as it arrives" and "surface the
                 failure tail (last 30-40 lines)" with "off a terminal dross test prints counts, failing tests
                 (bounded) and the .dross/logs/ path. Act on the printed failures, and open the log only when
                 a block says it was cut". Net bytes may not grow. Then run `make install` (r-01).
       contract: TestExecutePromptDescribesSummary: execute.md §1e contains ".dross/logs/" and the exact
                 testsummary more-failing prefix constant, and contains neither "streaming output as it
                 arrives" nor "last 30-40 lines".
                 TestQuickPromptDescribesSummary: the same holds for quick.md's red branch.
                 TestExecutePromptDidNotGrow: len(execute.md) ≤ 25854 (its size at base 54ad906), so the
                 v1.8 ≤15 KB budget is not made harder.
                 TestGreenPathNeverReadsLog: the §1e "**Green**" branch of both prompts has no
                 cat/Read/open instruction naming .dross/logs.

Wave 5 (depends t-1, t-8, t-9, t-10, t-11, t-12)
  t-13 Record after-numbers, gate flow-cost
       files:    .dross/phases/test-output-summaries/flow-cost.toml,
                 .dross/phases/test-output-summaries/flow-cost-before.toml (merged, then deleted),
                 internal/cmd/flowcost_record_test.go
       covers:   c-8
       desc:     On the installed phase-final binary, replay t-1's scratch scenario: the same three byte
                 measurements, and the same human-driven execute test→commit leg counted with the same jq.
                 Merge t-1's draft into flow-cost.toml ([scenario], [before], [after], [bytes.before],
                 [bytes.after]) and delete the draft. Extend TestFlowCostRecords with a
                 test-output-summaries block.
       contract: TestFlowCostRecords/test-output-summaries fails unless all of the following hold: [bytes.before]
                 and [bytes.after] each have green_test, red_test and verify_leg > 0; after < before for all
                 three; after.log_reads == 0 (opening the log would mean c-3's summary was not enough);
                 after.dross_calls ⊆ before.dross_calls (zero added round-trips, the generic gate); and the
                 two binary_versions differ.
                 TestFlowCostRecords' `found` check is extended so a missing test-output-summaries
                 flow-cost.toml fails the suite, and a leftover flow-cost-before.toml beside it fails too.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 log file + path, every run shape | t-2, t-11 |
| c-2 green summary: counts, time, path, no per-test lines | t-3, t-6, t-7, t-11 |
| c-3 red summary: names + own output, bounded; build errors are failures | t-3, t-6, t-7, t-11, t-12 |
| c-4 exit status untouched; 40-line fallback; non-zero never green | t-3, t-11 |
| c-5 mutation legs: per-package counts, score, survivor file:line, stream to log (local/remote, attached/collected) | t-4, t-8, t-9 (log via t-2) |
| c-6 rule show marker; change, /clear, compaction or missing session id prints in full | t-5, t-10 |
| c-7 interaction show marker, same rules | t-5, t-10 |
| c-8 before/after bytes, turns, wall-clock; zero added round-trips | t-1, t-13 (prompt side t-12) |

All 8 criteria are covered. The locked decisions map to these contracts: parser_coverage → t-6, t-7 plus t-3's
fallback. failure_bound → TestFailureBoundedAt30 and TestTenFailuresThenMore. log_location →
TestLogSelfIgnores. log_retention → TestPruneKeepsNewest20 and TestSameInstantNoClobber. tty_behaviour →
TestTTYStreamsLive, TestNoStreamingFlag and TestVerifyTTYTeesStream.

## Judgment calls

- **Runner detection:** chosen: sniff the output. Rejected: matching the command text. Why: `make test` and `npm test` wrap go test and vitest, and only the output says which runner ran.
- **Segments:** chosen: one Segment per spawn (one per lane), all in one log per run. Rejected: one count summed across lanes. Why: packages and tests are different units, and a summed count would mislead.
- **Go failure granularity:** chosen: one failure per top-level `--- FAIL` block, with its subtests inside. Rejected: one entry per leaf subtest. Why: a 40-case table test would use up the 10-failure cap by itself.
- **Package-level failures:** chosen: build failed, setup failed, a timeout panic or a TestMain exit is a failure named by its package. Rejected: sending these to the tail fallback. Why: c-3 makes build errors failures.
- **When the log opens:** chosen: on the first write, with dross test writing a `$ <line>` header before each spawn so a silent command still logs. Rejected: opening it eagerly. Why: under the 20-file cap, refused runs would prune real red logs.
- **Log failures:** chosen: a log that can't be opened falls back to today's live stream with one warning. Rejected: refusing the run. Why: c-4 says summarising never changes exit status.
- **TTY tee:** chosen: on a TTY the child writes to a pipe (no vitest colors in a human shell). Rejected: handing the child os.Stdout. Why: that skips the log c-1 requires on every run.
- **Log path form:** chosen: repo-relative `.dross/logs/…`, like every other path dross prints. Rejected: absolute paths.
- **Fixtures:** chosen: committed captures of real runs. Rejected: a live `go test` inside the suite (slow, timing-flaky) and hand-written fixtures (they would test my guess at the format).
- **Session key:** chosen: CLAUDE_CODE_SESSION_ID (confirmed present in Bash env, 36-char UUID). A set CLAUDE_CODE_CHILD_SESSION counts as no session, because a subagent shares the parent's id but not its context. Rejected: transcript_path. Why: only hooks receive it, Bash calls don't.
- **/clear and compaction:** chosen: an epoch that `dross reentry` (already the SessionStart hook) bumps; no epoch, no marker. Rejected: a PreCompact-only bump (misses /clear) and parsing the hook's `source` (bumping on every SessionStart is conservative, and the only cost is one extra full print for concurrent sessions).
- **Marker store location:** chosen: .dross/gate/shown/, which is already self-ignoring, with session ids hashed the way nudge claims are. Rejected: a new directory.
- **Mutation "package":** chosen: the mutated file's directory, for all three tools. survived keeps including not_covered, as the leg line and tests.json already do, so rows sum to the leg totals. Rejected: disjoint columns.
- **Detached collect:** chosen: copy the host's run-dir `log` into .dross/logs; a failed fetch is one line. Rejected: leaving the stream on the host. Why: c-5 names "collected" runs.
- **Failure heads:** chosen: the bounded heads stay on stderr. Rejected: moving them to the log. Why: they are the diagnosis the conversation needs when a leg dies.
- **When "before" is measured:** chosen: first (t-1), on the old binary and old prompts, into a draft file. Rejected: writing flow-cost.toml in t-1 (TestFlowCostRecords goes red without [after] and blocks every later commit) and rebuilding the old binary at the end (the prompts would already be reinstalled).
- **c-8 gates:** chosen: bytes after < before for all three, `log_reads == 0`, and the dross_calls subset. Rejected: gating turns and wall-clock. Why: context-boundaries already showed they vary from batching alone.
- **/dross-rule show row:** chosen: switch it to `rule list --merged`. Why: a user asking to see the rules must never get the marker instead.
- **Stryker split:** chosen: gremlins (t-4) split from stryker/stryker-net (t-8) under the 5-file rule. gremlins goes first because it is this repo's leg and the leg t-9's contracts exercise.
- **Execute.md byte cap:** chosen: execute.md may not grow (TestExecutePromptDidNotGrow). Why: prompt-partials will trim it to 15 KB later in v1.8, and this phase must not add to that work.
- **Seen, not planned:** `verify.adapterLanguage` maps "stryker.net", but `StrykerNet.Name()` returns "stryker-net", so a C# leg is labelled "stryker-net" rather than "csharp". This predates the phase and no criterion covers it. It should be filed, not fixed here.
