# test-output-summaries — risk-lens draft

Lens: failure modes drive the graph. Every risk in the register has exactly one owning task,
and that task's test_contract is the test that goes red when the risk materialises. Tasks follow
risk clusters, not features: bytes-to-disk (run log), bytes-to-meaning (parser core, each runner's
format), meaning-to-conversation (wiring, outcome tails), mutation streams, the session-context
marker (store / consumer / producer), prompt guidance and measurement.

Logic goes in leaf packages (`internal/runlog`, `internal/runsummary`, `internal/gatestate`,
`internal/verify`) so its tests are fast. `internal/cmd` is the package that hangs on the laptop,
so its tasks stay thin wiring, gated through remote `dross test`.

Facts checked in the tree that shape the graph:
- `TestFlowCostRecords` globs every `phases/*/flow-cost.toml` and fails on a file with no
  `[after]`. A half record committed at t-1 would turn every later full `dross test` red, and
  that would block every commit in the phase. So the before-numbers go to a file the glob
  doesn't match.
- `TestNoSpawnOutputEscapes` treats any non-terminal write of spawn output as an ESCAPE, so
  writing the stream to a log file fails it. A `//dross:taint-cleared` marker that clears nothing
  also fails it, so the marker can only land in the same task as the first real flow (t-9).
  Markers are banned in gremlins.go, stryker.go and stryker_net.go (`TestToolRunnersCarryNoMarker`).
- Subagents inherit the parent's `CLAUDE_CODE_SESSION_ID`. Seen in this session:
  `CLAUDE_CODE_CHILD_SESSION=1`, and the parent's transcript dir holds `subagents/`. Keyed on
  that id alone, a subagent would get "rules unchanged" for rules it never saw.
- `TestLaneHeaderPrecedesLaneOutput` and `TestPrepareLinePrecedesTheLaneHeaderAndItsOutput` check
  that lane output reaches stdout, and tests run non-TTY. Both must be re-pointed at the log. Their
  names are pinned by `tests_before.txt` / TestNoTestLost.
- The hermetic `TestMain` (internal/cmd/hermetic_env_test.go) doesn't clear `CLAUDE_CODE_*`.
  Run from an agent's Bash, `TestInteractionShowEmitsPlaybook` (cwd inside the real repo)
  would read the developer's real marker store and could see "playbook unchanged".
- `io.MultiWriter` stops on the first writer error. If a log write fails with ENOSPC, the child's
  pipe copy stops and the suite dies with SIGPIPE. That changes the exit status, which c-4 forbids.

## Risk register (risk → owner)

| # | What breaks | Owner |
|---|---|---|
| R1 | Before-numbers taken after a `make install` measure the new binary | t-1 |
| R2 | A half flow-cost.toml committed at t-1 reddens TestFlowCostRecords, so every commit gate in the phase is red | t-1 |
| R3 | Logs not ignored (missing or edited `.dross/logs/.gitignore`). Ship's clean-tree auto-commit then puts test output into a public PR | t-2 |
| R4 | Log created before its ignore file (a window where `git status` sees it) | t-2 |
| R5 | Concurrent runs in one instant clobber one log | t-2 |
| R6 | Prune deletes non-logs, the run's own log, or a live long verify log; ENOENT race between two pruners | t-2 |
| R7 | A log write error propagates through a fan-out and kills the child, changing the exit status | t-2 |
| R8 | A phase id or leg name containing separators writes outside `.dross/logs/` | t-2 |
| R9 | Logs rsync'd to the remote on every run, which costs wall-clock (milestone c-6) | t-2 |
| R10 | Non-zero exit renders a green summary (parse all-ok, exit 1) | t-3 |
| R11 | Unparseable output not rendered as exit + last 40 + log path, or counts invented at exit 0 | t-3 |
| R12 | failure_bound slips (30 lines per failure, first 10 in full, `+N more failing — see <log>`) | t-3 |
| R13 | Mass break or 2,000-char lines exceed the Bash tool's output window, which truncates the middle | t-3 |
| R14 | bufio.Scanner ErrTooLong silently stops parsing at a long line | t-3 |
| R15 | Unbounded memory on a 200 MB stream | t-3 |
| R16 | ANSI colour (FORCE_COLOR) / CRLF break line matching | t-3 |
| R17 | Two parsers claim one segment and dross guesses | t-3 |
| R18 | go: subtests double-counted; panic or timeout text lost; TestMain/init failure invisible | t-7 |
| R19 | go: `# pkg` build blocks (stderr-first), `[pkg.test]`, vet, `[setup failed]` not counted as failures | t-7 |
| R20 | go: package counts wrong (`[no test files]`, `[no tests to run]`) | t-7 |
| R21 | Fixtures written from memory rather than captured from a real runner | t-7 (go), t-8 (js) |
| R22 | vitest/jest: load/syntax-error suites, "Unhandled Errors", runner-reported counts | t-8 |
| R23 | A JS parser claims go output or vice versa; truncated output before the summary line is claimed | t-8 |
| R24 | Log path printed only at the end, so a run the Bash tool kills leaves no path | t-9 |
| R25 | Different writers for child stdout/stderr: two fds, lost interleave, write race | t-9 |
| R26 | TTY detection inverted (human loses live stream / agent gets the stream) | t-9 |
| R27 | Header→output ordering tests break, or get deleted (TestNoTestLost) | t-9 |
| R28 | Log can't be opened (read-only `.dross`) and the run fails or goes silent | t-9 |
| R29 | Taint gate red: a stream-to-file write with no marker, or a marker in a tool-runner file | t-9 |
| R30 | A "no flag to force streaming" lock breach: a new flag on `dross test` | t-9 |
| R31 | Wiring passes a zero outcome for a failed spawn, so the summary claims green | t-12 |
| R32 | Exit 3/4/7 lose the ssh/rsync/prepare diagnosis that streamed before | t-12 |
| R33 | Multi-lane: one parse over mixed lanes; budget per lane instead of per run; selector miss dumps a tail | t-12 |
| R34 | Failure text folded into the returned error (taint escape, duplicate print) | t-12 |
| R35 | Tool stream (gremlins/stryker/stryker-net, local and remote) still reaches stderr when summarising | t-5 |
| R36 | Failure diagnosis (bounded head) lost when the stream moves to the log | t-5 |
| R37 | Per-package rows double-count NOT COVERED inside survived, or don't sum to the leg's totals | t-6 |
| R38 | Out-of-scope survivors listed as the phase's; a NOT COVERED survivor unmarked | t-6 |
| R39 | Collected detached run: log fetch failure fails the collect, or reads a stale local copy | t-13 |
| R40 | Log fetch uses a spawn outside the audited seams (TestEverySpawnSiteGatedOrExempt) | t-13 |
| R41 | Verify exit codes / TTY streaming change with the log wiring | t-13 |
| R42 | Marker store: session id path traversal, kinds clobber each other, records dirty the tree or never prune | t-4 |
| R43 | Marker fires for a session no SessionStart hook ever recorded (hook unwired, or session launched outside the repo), where compaction can't be detected | t-4 |
| R44 | Subagent (shared session id) gets the marker | t-10 |
| R45 | Record written before the full print succeeded / on an error path | t-10 |
| R46 | Store failure fails `rule show` | t-10 |
| R47 | Tests inherit the developer's `CLAUDE_CODE_SESSION_ID` and touch the real repo's store | t-10 |
| R48 | No way back to the full block after an undetected context loss (`\| head`, unknown compaction path) | t-10 |
| R49 | /clear with a stale env session id, or a compaction (same id), yields a false "unchanged" | t-11 |
| R50 | `dross reentry` reads stdin and hangs on an inherited open pipe or TTY; hook output bytes change; hook exits non-zero | t-11 |
| R51 | Prompts still say "streaming" / "surface the last 30-40 lines", so the agent re-reads the log (an added round-trip) | t-14 |
| R52 | execute.md / quick.md grow against prompt-partials' ≤15 KB budget | t-14 |
| R53 | After-numbers on a stale binary or with the hook unwired; real-session marker never observed | t-15 |
| R54 | After-flow adds a dross call or a log Read the gate can't see | t-15 |

```
Phase test-output-summaries — 15 tasks across 5 waves

Wave 1
  t-1  Record before-numbers on today's binary
       files:    .dross/phases/test-output-summaries/flow-cost-before.toml
       covers:   c-8
       depends:  —
       desc:     Run FIRST, before any `make install` in this phase; record `dross version`. In fixed
                 scratch repos (a Go module with 6 packages and one seeded failing test; a gremlins
                 phase on a 2-package module) record green_test_bytes, red_test_bytes and
                 verify_leg_bytes as `dross … >f 2>&1; wc -c <f`. Also record rule_show_repeat_bytes
                 (2nd `dross rule show` in one session). The human then drives the flow in a scratch
                 dross repo: /dross-execute pair, one task, §1e red → `fix here` → green → §1f full
                 `dross test` → commit. Count agent_turns, turn_tools, wall_clock_s, dross_calls and
                 log_reads with jq over the transcript, structurally (context-boundaries' method).
                 Write [scenario] + [before] to flow-cost-before.toml.
       contract: if the half record is globbed, TestFlowCostRecords fails on every later commit gate:
                   after t-1 no .dross/phases/test-output-summaries/flow-cost.toml exists, and the
                   phases/*/flow-cost.toml glob does not match flow-cost-before.toml
                 if before ran on a rebuilt binary, t-15's TestFlowCostRecords fails:
                   before.binary_version is recorded here and must differ from after.binary_version
                 if a field is missing, t-15's merge fails TestFlowCostRecords: [scenario] carries
                   repo_shape, wave_position, flow_start, flow_end and jq_method, and [before] carries
                   agent_turns > 0, len(turn_tools) == agent_turns, non-empty dross_calls, and every
                   *_bytes > 0

  t-2  Add self-ignoring run-log sink package
       files:    internal/runlog/runlog.go, internal/runlog/runlog_test.go
       covers:   c-1, c-5
       depends:  —
       desc:     Open(root, Name{Kind test|verify, Phase, Leg}) → *Log in .dross/logs/. Before the log
                 is created, write (or rewrite to the canonical body) .dross/logs/.gitignore holding
                 `*`. Name test-<ts>.log / verify-<phase>-<leg>-<ts>.log, with ts in UTC to the
                 nanosecond. Create with O_EXCL plus a -N suffix on collision. Phase and leg pass
                 pathfence.Segment. After each create, prune `^(test|verify)-.*\.log$` to the newest
                 20 by mtime, never the file just created, with ENOENT ignored. Log.Write is
                 mutex-guarded and always returns len(p), nil; Err() reports the first failure.
                 Path() is absolute.
       contract: if logs dirty the tree, TestLogDirIgnored fails: in a git temp repo with .dross/
                   tracked, Open+Write leaves `git status --porcelain` empty, and `git check-ignore`
                   matches the log
                 if an edited ignore file is trusted, the same test fails: a pre-existing
                   .dross/logs/.gitignore holding `foo` is rewritten to `*` before the log exists
                 if the log precedes its ignore file, TestIgnoreBeforeLog fails: the create-order seam
                   records .gitignore → log
                 if same-instant runs clobber, TestSameInstantDistinctFiles fails: two Opens under a
                   frozen clock seam yield two distinct paths, each holding only its own bytes
                 if pruning is wrong, TestPruneNewest20 fails: 25 logs with distinct mtimes plus
                   .gitignore and notes.txt → after one Open, the 20 newest logs by mtime survive
                   (including the new one, and an old-named log whose mtime is newest), .gitignore and
                   notes.txt remain, and a log deleted mid-prune (ENOENT) is not an error
                 if a log error can kill the child, TestWriteNeverFails fails: with the file seam
                   failing after 1 KiB, every Write returns (len(p), nil) and Err() names the first
                   failure
                 if a name escapes, TestNameStaysInDir fails: Phase "../../etc" or Leg "go/x" →
                   refused, or a base name with no separator, inside .dross/logs/
                 if logs cross the wire, TestLogsOffTheWire fails: after a log is written in a git
                   temp repo, the exclude file remote.SyncArgs builds lists /.dross/logs/
                 if writes race, `go test -race` TestConcurrentWrites fails: 8 goroutines × 1,000
                   writes → no race, file length == bytes written

  t-3  Add bounded summary core and renderer
       files:    internal/runsummary/summary.go, internal/runsummary/render.go,
                 internal/runsummary/summary_test.go
       covers:   c-2, c-3, c-4
       depends:  —
       desc:     A streaming line splitter (lines cut at 64 KiB, no ErrTooLong stop; strip \r and ANSI
                 SGR), a Parser interface (Claims/Feed/Result) with an init()-registered list so the
                 runner parsers land in their own files, a 40-line tail ring, and a per-run Budget.
                 Render(outcome{exit, ran}, results, budget, logPath, wall) prints green only when
                 exit == 0, a parser claimed and failures == 0. Otherwise it prints red with counts
                 and failures, or the fallback: exit status, last 40 lines, log path. Each printed
                 line is cut at 240 chars.
       contract: if a non-zero exit can read green, TestNonZeroNeverGreen fails: a fake parser
                   reporting 12 ok / 0 failures with exit 1 renders the fallback (exit 1, the last 40
                   lines, the log path) and no green line
                 if unparseable output is mishandled, TestUnparseableFallback fails: 100 unclaimed
                   lines at exit 2 → an `exit 2` line, lines 61-100 verbatim, the log path; at exit 0
                   the same shape and no counts
                 if the bound slips, TestFailureBound fails: 14 failures × 50 lines → 10 blocks of 30
                   lines, each ending `… 20 more lines — see <log>`, then exactly `+4 more failing —
                   see <log>`
                 if a mass break overflows the Bash window, TestMassBreakSize fails: 500 failures ×
                   100 lines of 2,000 chars render ≤ 320 lines and ≤ 30,000 bytes
                 if a long line stops parsing, TestLongLineKeepsParsing fails: a 10 MiB line with no
                   newline, then `SENTINEL` → the fake parser still receives SENTINEL
                 if memory is unbounded, TestBoundedRetention fails: 2,000,000 fed lines retain
                   ≤ 1 MiB (Retained()), and the tail ring holds exactly the last 40
                 if colour or CRLF break matching, TestStripsANSIAndCR fails:
                   "\x1b[31mFAIL\x1b[0m\r\n" reaches Feed as "FAIL"
                 if dross guesses between runners, TestAmbiguousClaimFallsBack fails: two fake
                   parsers both claiming one segment → fallback render
                 if the budget isn't shared, TestBudgetSpansSegments fails: two segments × 8 failures
                   through one Budget → 10 full blocks in total, then `+6 more failing`
                 if green is chatty, TestGreenIsShort fails: a fake 60-package green → ≤ 3 lines
                   (counts, wall-clock, log path) and no package name

  t-4  Add show-marker store to gatestate
       files:    internal/gatestate/showmark.go, internal/gatestate/showmark_test.go
       covers:   c-6, c-7
       depends:  —
       desc:     Under .dross/gate/show/ (already self-ignored), BumpContext(root, sid) replaces a
                 repo-wide epoch nonce and marks sid seen. ShownCurrent(root, sid, kind, sha) is
                 read-only and true only when sid is non-empty, sid is seen, and its kind record holds
                 {current epoch, sha}. RecordShown writes that record by atomic rename. Session keys
                 are hashed (nudgeKey). Records and seen-marks older than 7 days are pruned on write,
                 inside show/ only.
       contract: if an unhooked session gets the marker, TestUnseenSessionNeverCurrent fails:
                   RecordShown(s, rules, h) with no BumpContext(s) → ShownCurrent false
                 if one session's start doesn't reset others, TestAnyBumpInvalidates fails:
                   Bump(s1); Record(s1, rules, h); Bump(s2) → ShownCurrent(s1, rules, h) false
                 if content isn't compared, TestShaMismatch fails: Record(s, rules, h1) →
                   ShownCurrent(s, rules, h2) false
                 if kinds clobber, TestKindsIndependent fails: Record(s, rules, h) leaves
                   ShownCurrent(s, playbook, h) false; 32 goroutines recording both kinds leave both
                   readable
                 if an id escapes, TestShowKeyContained fails: sid "../../x" and "a/b" land in
                   .dross/gate/show/; an empty sid → ShownCurrent false and RecordShown error
                 if records dirty the tree, TestShowRecordsIgnored fails: `git status --porcelain` is
                   empty after Bump+Record in a git temp repo
                 if prune overreaches, TestShowPrune fails: an 8-day-old record is pruned on the next
                   write, while nudge claims and green.json are untouched
                 if the read path writes, TestShownCurrentReadOnly fails: on a fresh root no
                   .dross/gate/show/ is created

  t-5  Route mutation tool streams to an injectable sink
       files:    internal/mutation/gremlins.go, internal/mutation/stryker.go,
                 internal/mutation/stryker_net.go, internal/mutation/stream_test.go
       covers:   c-5
       depends:  —
       desc:     Each adapter gains `Stream io.Writer` (nil → os.Stderr, today's behaviour). The tee
                 becomes MultiWriter(Stream, head), and the per-package invocation echo and skip lines
                 go to Stream. The bounded failure head (printHead) stays on os.Stderr. No
                 taint-cleared marker in these three files.
       contract: if the stream still leaks, TestToolStreamGoesToSink fails: per adapter, with Stream =
                   buf and a buildCmd fake printing 300 per-mutant lines, captured os.Stderr holds
                   none of them and buf holds all 300
                 if a remote leg leaks, TestRemoteToolStreamGoesToSink fails: a remote Target with a
                   faked launcherCommand writing mutant lines → the same split
                 if diagnosis is lost, TestFailureHeadStillOnStderr fails: a fake exiting 2 with no
                   report → os.Stderr holds the head (≤ strykerHeadLines lines), and the error still
                   carries its RecordToolFailure kind
                 if the default moves, TestNilStreamIsStderr fails: Stream nil → tool lines on
                   os.Stderr
                 if a marker sneaks in, TestToolRunnersCarryNoMarker (existing) fails

  t-6  Print per-package counts and survivor lines
       files:    internal/verify/legsummary.go, internal/verify/legsummary_test.go,
                 internal/cmd/verify.go
       covers:   c-5
       depends:  —
       desc:     LegLines(LanguageRun) groups Mutation.Files by package dir (gremlins) or directory
                 (stryker, stryker-net). Columns: killed / lived (Survived − NotCovered) /
                 not covered / timed out. Then each in-scope survivor as file:line grouped per file,
                 with NOT COVERED marked. printVerifySummary prints them under each leg's existing
                 counts line, so attached and collected runs share it through finishVerify.
       contract: if rows double-count, TestPackageRowsSumToLeg fails: a 3-package report → column
                   sums equal the leg's Killed, Survived−NotCovered, NotCovered and Timeout
                 if survivors are wrong, TestSurvivorLines fails: 5 in-scope survivors over 2 files
                   (one NOT COVERED) → each file:line once, NC marked, and no out-of-scope survivor
                   listed
                 if stryker rows mis-group, TestStrykerRowsByDir fails: src/a/x.ts and src/a/y.ts →
                   one src/a row
                 if a failed leg grows rows, TestErrorLegUnchanged fails: Error or nil Mutation →
                   only today's line
                 if the wiring is missing, TestVerifySummaryPrintsPackagesAndSurvivors (internal/cmd)
                   fails: finishVerify on seeded Tests prints the rows and every survivor's file:line

Wave 2
  t-7  Parse plain go test output (depends t-3)
       files:    internal/runsummary/gotest.go, internal/runsummary/gotest_test.go,
                 internal/runsummary/testdata/gotest/green.txt,
                 internal/runsummary/testdata/gotest/red_subtests.txt,
                 internal/runsummary/testdata/gotest/panic.txt,
                 internal/runsummary/testdata/gotest/timeout.txt,
                 internal/runsummary/testdata/gotest/build_failed.txt,
                 internal/runsummary/testdata/gotest/setup_and_vet.txt
       covers:   c-2, c-3
       depends:  t-3
       desc:     Fixtures are CAPTURED from a scratch module with real `go test -count=1 ./...`
                 (go1.26.1), stdout and stderr on one fd. Each fixture starts with a `# captured:`
                 header naming the go version and the command. Counts: ok / FAIL / no test files /
                 no tests to run packages. A failure is a leaf `--- FAIL` with its indented output;
                 non-indented lines up to the package's FAIL line attach to the last failure. A FAIL
                 package with no test failure becomes a package failure. Each `# pkg` block is one
                 build failure.
       contract: if counts drift, TestGoGreenCounts fails: green.txt → 4 ok, 2 no test files,
                   1 no tests to run, 0 failures
                 if subtests double-count, TestGoSubtestsAreLeaves fails: red_subtests.txt →
                   TestTable/a, /b, /c and not TestTable
                 if panic text is lost, TestGoPanicAttributed fails: panic.txt → TestBoom's block
                   holds "panic: boom" and its first stack frame
                 if a timeout reads unparseable, TestGoTimeout fails: timeout.txt (-timeout 2s) → a
                   package failure starting "panic: test timed out after 2s" and naming the running
                   test
                 if build errors vanish, TestGoBuildFailed fails: build_failed.txt → the `# dep` block's
                   "undefined: foo" printed once, and every `[build failed]` package counted failed
                 if setup/vet/test-file compile are missed, TestGoSetupAndVet fails: a `[setup
                   failed]` package and a `# pkg [pkg.test]` vet/compile block each become a failure
                   with their error line
                 if go claims foreign text, TestGoClaimsOnlyGo fails: a make transcript with "ok  done"
                   and t-8's vitest fixtures are not claimed
                 if a fixture is invented, TestGoFixturesCaptured fails: any testdata/gotest file
                   without the `# captured: go1.` header

  t-8  Parse vitest and jest output (depends t-3)
       files:    internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go,
                 internal/runsummary/testdata/js/vitest_green.txt,
                 internal/runsummary/testdata/js/vitest_red.txt,
                 internal/runsummary/testdata/js/vitest_load_error.txt,
                 internal/runsummary/testdata/js/jest_red.txt,
                 internal/runsummary/testdata/js/jest_suite_failed.txt
       covers:   c-2, c-3
       depends:  t-3
       desc:     vitest fixtures are captured with the vitest 4.1.11 already in
                 internal/mutation/testdata/ts-project (a scratch copy, plain and FORCE_COLOR=1). jest
                 fixtures come from an exact-pinned jest installed in a scratch dir outside the repo.
                 Each carries a `# captured:` header. Counts are the runner's own summary lines;
                 failures are FAIL / ● blocks. Load errors, "Test suite failed to run" and
                 "Unhandled Errors" count as failures.
       contract: if counts aren't the runner's, TestVitestCounts fails: vitest_red.txt → "Tests 1
                   failed | 4 passed (5)" and "Test Files 1 failed | 1 passed (2)", plus a failure
                   named "src/math.test.ts > add > negatives" carrying its AssertionError and diff
                 if a load error is lost, TestVitestLoadError fails: vitest_load_error.txt → a failure
                   carrying the transform/syntax error line
                 if jest is wrong, TestJestCounts/TestJestSuiteFailed fail: jest_red.txt → "Tests: 1
                   failed, 3 passed, 4 total" and the ● block; jest_suite_failed.txt → a failure
                   carrying the "Test suite failed to run" error
                 if colour breaks parsing, TestJSForcedColour fails: the FORCE_COLOR capture of
                   vitest_red parses identically to the plain one
                 if truncated output is claimed, TestJSTruncatedFallsBack fails: vitest_red cut before
                   its "Tests" line → not claimed
                 if runners cross, TestJSDoesNotClaimGo fails: t-7's go fixtures are not claimed
                 if a fixture is invented, TestJSFixturesCaptured fails: a fixture without the
                   `# captured: vitest@|jest@` header

  t-9  Route dross test output through the run log (depends t-2, t-3)
       files:    internal/cmd/test.go, internal/cmd/test_output.go, internal/cmd/test_output_test.go,
                 internal/cmd/test_lane_consent_test.go, internal/cmd/test_lane_prepare_test.go,
                 internal/runlog/runlog.go
       covers:   c-1, c-4
       depends:  t-2, t-3
       desc:     test_output.go: a per-invocation sink opened after FindRoot for every run. It prints
                 `log: <abs path>` before the first spawn and again at the end. stdoutIsTerminal is a
                 seam over term.IsTerminal. TTY: one writer = log + os.Stdout. Non-TTY: one writer =
                 log + segment feed. That same writer value goes to every spawn's stdout and stderr
                 (local, remote, rsync, prepare). dross's own lines still reach stdout and are
                 copied to the log. Until t-12 every non-TTY run prints t-3's fallback. If the log
                 can't be opened, the run streams live with one warning. Long help drops "streams";
                 no new flag. A //dross:taint-cleared marker goes on runlog's file write, with its
                 reason.
       contract: if a killed run leaves no path, TestLogPathBeforeSpawn fails: the fake spawn sees
                   stdout already holding "log: /…/.dross/logs/test-"
                 if a run skips the log, TestEveryRunWritesLog fails: bare, selector, --files,
                   lanes-only bare, local and remote (recorders) each leave exactly one new test-*.log
                   holding the fake's sentinel; refusals (exit 2/5/6) still print a path
                 if streams split, TestOneWriterBothStreams fails: every spawn seam receives
                   stdout == stderr (the same writer value)
                 if TTY handling inverts, TestTTYStreamsNonTTYSummarises fails: non-TTY → the sentinel
                   is in the log and not stdout; TTY seam on → the sentinel is in stdout AND the log
                 if ordering tests rot, TestLaneHeaderPrecedesLaneOutput and
                   TestPrepareLinePrecedesTheLaneHeaderAndItsOutput (names kept, TestNoTestLost) fail
                   unless prepare < header < sentinel by byte offset in the log; prepare and header
                   lines still appear on stdout
                 if a broken log dir breaks the run, TestLogUnavailableStreams fails: .dross/logs is a
                   regular file → the sentinel is on stdout, one warning, and the exit code equals the
                   fake's suite outcome
                 if the taint gate breaks, TestNoSpawnOutputEscapes fails; the only new marker is on
                   runlog's write, so TestToolRunnersCarryNoMarker stays green
                 if a streaming flag appears, the `test` command's cli_surface golden fails

  t-10 Wire unchanged-marker into rule and interaction show (depends t-4)
       files:    internal/cmd/showmark.go, internal/cmd/rule.go, internal/cmd/interaction.go,
                 internal/cmd/showmark_test.go, internal/cmd/hermetic_env_test.go
       covers:   c-6, c-7
       depends:  t-4
       desc:     showOnce(kind, text): the identity is CLAUDE_CODE_SESSION_ID, treated as empty when
                 CLAUDE_CODE_CHILD_SESSION is set. If the root resolves, the identity is set, --full
                 is unset and ShownCurrent holds, print one line: `rules unchanged — identical to the
                 full block printed earlier in this session; still MUST-FOLLOW (dross rule show
                 --full reprints)`, or the same for the playbook. Otherwise print in full, and
                 RecordShown only after the write returned nil. Store errors never fail the command.
                 TestMain unsets both env vars.
       contract: if repeats don't shrink, TestRuleShowRepeatPrintsMarker fails: BumpContext(root, "s")
                   and env "s" → 1st call the full block, 2nd call exactly one line starting "rules
                   unchanged"
                 if a change hides, TestRuleShowChangeReprints fails: a rule added between calls → the
                   2nd call prints the full block containing it
                 if the id is missing, TestRuleShowNoSessionFull fails: env unset → full both times
                 if a subagent is short-changed, TestShowChildSessionFull fails:
                   CLAUDE_CODE_CHILD_SESSION=1 with a current record for "s" → the full block, both
                   kinds
                 if a record precedes the print, TestShowRecordsOnlyAfterPrint fails: a print seam
                   returning EPIPE → no record, and the next call prints full
                 if the store can fail the command, TestShowStoreFailureStillPrints fails: .dross/gate
                   a regular file → full block, exit 0
                 if the kinds tangle, TestInteractionShowMarker fails: a repeat prints "playbook
                   unchanged", and a rules-only change leaves the playbook marker in place
                 if there is no way back, TestShowFullFlag fails: --full after a current record prints
                   the full block; the marker line names --full and MUST-FOLLOW
                 if tests read the developer's store, TestHermeticSessionEnv fails:
                   CLAUDE_CODE_SESSION_ID and CLAUDE_CODE_CHILD_SESSION are empty inside the package
                   (TestInteractionShowEmitsPlaybook is then env-independent)
                 if the flag drifts, the cli_surface goldens for rule show / interaction show differ
                   only by --full

Wave 3
  t-11 Bump context epoch from the SessionStart hook (depends t-10)
       files:    internal/cmd/reentry.go, internal/cmd/reentry_test.go
       covers:   c-6, c-7
       depends:  t-10
       desc:     `dross reentry` (already the wired SessionStart target, so it is live on binary
                 upgrade) reads stdin only when it isn't a terminal. The read is capped at 1 MiB and
                 has a 300 ms deadline goroutine. Any payload with a session_id calls BumpContext.
                 Every source (startup, resume, clear, compact) bumps. A bump failure is silent, and
                 the envelope bytes and exit code are unchanged.
       contract: if a stale env id survives /clear, TestClearThenShowPrintsFull fails: rule show ×2
                   (marker) → reentry with source "clear" and a NEW session_id while
                   CLAUDE_CODE_SESSION_ID keeps the old one → the next rule show prints the full block
                 if compaction is invisible, TestCompactThenShowPrintsFull fails: the same session id
                   with source "compact" → the full block, for both kinds
                 if a source is skipped, TestReentryBumpsEverySource fails: startup, resume, clear and
                   compact each change the epoch and mark the id seen
                 if the hook can hang, TestReentryOpenPipeReturns fails: stdin is an os.Pipe never
                   written or closed → returns in < 500 ms with golden output and no bump; with the
                   terminal seam on, stdin is not read
                 if hook output moves, the output/reentry.txt golden fails with a payload on stdin
                 if a bad payload breaks the hook, TestReentryBadPayload fails: non-JSON, a 10 MiB
                   payload, or an unwritable .dross/gate → golden output, exit 0, no bump

  t-12 Print per-lane summaries and outcome tails (depends t-7, t-8, t-9)
       files:    internal/cmd/test.go, internal/cmd/test_output.go,
                 internal/cmd/test_summary_wiring_test.go
       covers:   c-2, c-3, c-4
       depends:  t-7, t-8, t-9
       desc:     Each spawn is its own segment and is parsed by whichever parser claims it, through
                 one Budget per run. The summary is rendered after each lane, from the classified
                 outcome (the exit code from ExitCodeError and the exitCoder). Exits 3/4/7 print the
                 failing spawn's last 40 lines. A selector miss prints only its miss line. Summaries
                 go to stdout and never into a returned error. Wall-clock covers the whole run.
       contract: if green leaks lines, TestGreenRunPrintsSummaryOnly fails: a fake writing
                   gotest/green.txt → stdout holds the log line, one counts line and the wall-clock,
                   and no fixture package path
                 if red is incomplete, TestRedRunPrintsFailures fails: red_subtests.txt + exit 1 → the
                   three leaf names and their lines on stdout, exit 1
                 if a failed spawn reads green, TestNonZeroAllOkFallsBack fails: green.txt written but
                   exit 1 → the fallback (exit 1, last 40 lines), no green line, exit code 1
                 if transport loses its why, TestTransportTailShown fails: a remote fake writing "ssh:
                   connect to host h port 22: Connection refused" and exiting 255 → exit 3, with that
                   line and the log path on stdout; the same for exit 4, and for exit 7 with "make:
                   *** [build] Error 2"
                 if lanes blur, TestTwoLanesEachParsed fails: a go lane (red_subtests.txt) and a
                   vitest lane (vitest_green.txt) → two lane summaries, overall red, exit 1
                 if the budget resets per lane, TestBudgetAcrossLanes fails: two lanes × 8 failures →
                   10 blocks, then "+6 more failing"
                 if a miss dumps output, TestSelectorMissNoTail fails: an EmptyExit miss prints only
                   its miss line
                 if failures escape into errors, TestSummaryNotInError fails: the returned error's text
                   holds no fixture line
                 if exit codes move, TestExitCodesUnchanged fails: fakes for 0, 1, 3, 4, 5, 6, 7 and 8
                   exit as before; a green full run still writes green.json

  t-13 Write verify leg streams to run logs (depends t-2, t-5, t-9)
       files:    internal/cmd/verify.go, internal/cmd/verify_log_test.go
       covers:   c-5
       depends:  t-2, t-5, t-9
       desc:     Attached: before RunScoped, open verify-<phase>-<tool>-<ts>.log per adapter and set
                 its Stream (TTY: log + os.Stderr; non-TTY: log only), and print each leg's log path.
                 Collected: a detachFetchLog seam pulls <runDir>/log via remote.FetchArgs +
                 runDetachArgv into a fresh verify log. A fetch failure prints "log unavailable:
                 <why>" and the collect carries on. If a log can't be opened, that leg streams to
                 stderr with one warning.
       contract: if mutant lines reach the conversation, TestAttachedLegLogsStream fails: verify with
                   a fake gremlins (configuredAdaptersFn) printing 300 mutant lines, non-TTY →
                   stdout and stderr hold none, the verify-<phase>-gremlins-*.log holds all, and its
                   absolute path is printed
                 if the human loses live output, TestTTYLegStillStreams fails: TTY seam on → mutant
                   lines on stderr and in the log
                 if the collected stream is lost, TestCollectFetchesHostLog fails: the fake fetch
                   copies a host log → a new verify log holds it and its path is printed
                 if the log can fail a collect, TestCollectLogFetchFailureIsANote fails: the fetch
                   returns a transport error → tests.json and verify.toml are written, the exit is as
                   before, and "log unavailable" is printed
                 if the fetch skips the audited seams, TestEverySpawnSiteGatedOrExempt fails (the
                   argv is remote.FetchArgs(target, <runDir>/log, local))
                 if codes move, TestVerifyExitCodesWithLogs fails: --no-wait on a held host still
                   exits 15; an unwritable .dross/logs still yields the same verdict

Wave 4
  t-14 Rewrite execute/quick test-step guidance (depends t-11, t-12, t-13)
       files:    assets/prompts/execute.md, assets/prompts/quick.md, README.md,
                 internal/cmd/execute_prompt_test.go, internal/cmd/quick_prompt_test.go
       covers:   c-3, c-8
       depends:  t-11, t-12, t-13
       desc:     §1e: `dross test` prints a summary (counts, failing tests with their output, the log
                 path). On red, act on the summary's failures; open the log only when a block says
                 it is cut. "streaming output as it arrives" and "surface the failure tail (last
                 30-40 lines)" are replaced with no net growth. README's dross test row gets the log
                 + summary, and the rule show row gets the marker. Then `make install` (r-01).
       contract: if the agent is told to tail or stream, TestExecuteTestStepDescribesSummary fails:
                   execute.md has neither "streaming output as it arrives" nor "last 30-40 lines",
                   and names the log path and acting on the summary; the same for quick.md in
                   TestQuickTestStepDescribesSummary
                 if prompts grow, TestTestStepPromptsNotGrown fails: execute.md ≤ 25,854 bytes and
                   quick.md ≤ its pre-phase size

Wave 5
  t-15 Record after-numbers and gate them (depends t-1, t-6, t-14)
       files:    .dross/phases/test-output-summaries/flow-cost.toml,
                 .dross/phases/test-output-summaries/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-8
       depends:  t-1, t-6, t-14
       desc:     `make install`, then `dross hooks ensure`. Confirm that `dross doctor` shows
                 SessionStart → dross reentry and that `dross version` matches HEAD. Replay t-1's
                 scenario exactly. Live-check the marker in a real session: a repeat, a /clear,
                 /compact, a subagent's call. Move [before] into flow-cost.toml beside
                 [after] + [marker_live], and delete flow-cost-before.toml. Add a
                 test-output-summaries block to TestFlowCostRecords. Observed numbers only.
       contract: if output didn't shrink, TestFlowCostRecords (test-output-summaries) fails: each of
                   green_test_bytes, red_test_bytes and verify_leg_bytes is after < before, and
                   after.green_test_bytes ≤ 1024
                 if the red step needed the log, it fails: after.log_reads == 0
                 if a round-trip was added, the existing gate fails: after.dross_calls ⊆
                   before.dross_calls
                 if the marker is inert in production, it fails: after.rule_show_repeat_bytes < 300
                   and [marker_live] clear_full, compact_full and subagent_full are all true
                 if the after leg used the old binary, the existing binary_version check fails
                 if the before file lingers, it fails: flow-cost-before.toml no longer exists
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 log + path for every run | t-2, t-9 |
| c-2 green summary | t-3, t-7, t-8, t-12 |
| c-3 red: names + own output, build errors, act without the log | t-3, t-7, t-8, t-12, t-14 |
| c-4 exit status untouched, fallback, never green on non-zero | t-3, t-9, t-12 |
| c-5 mutation legs: per-package, score, survivors, stream to log (local, remote, collected) | t-2, t-5, t-6, t-13 |
| c-6 rules unchanged marker (change, /clear, compaction, missing id) | t-4, t-10, t-11 |
| c-7 playbook unchanged marker | t-4, t-10, t-11 |
| c-8 before/after bytes, turns, wall-clock, zero round-trips | t-1, t-14, t-15 |

8/8 criteria covered. All five locked decisions are honoured: parser_coverage (t-7/t-8 only,
everything else takes t-3's fallback), failure_bound (t-3), log_location (t-2), log_retention (t-2)
and tty_behaviour (t-9/t-13, no flag).

## Judgment calls

- **Context epoch.** I chose a repo-wide epoch that every SessionStart bumps, plus a per-session
  "seen" mark. I rejected per-session epochs because it isn't known whether CLAUDE_CODE_SESSION_ID
  in Bash updates after /clear, and a stale id with a per-session epoch reads "unchanged" in a
  cleared context. The cost is one extra full print in other panes of the same repo.
- **Unseen sessions.** A session the hook never recorded is never shown the marker. A missing hook
  must fail to "full", and the alternative was trusting an env id with no compaction signal behind it.
- **Subagents.** CLAUDE_CODE_CHILD_SESSION forces a full print. I rejected keying on the session id
  alone because subagents share it (verified in this session).
- **Hook placement.** The epoch bump lives in the existing `dross reentry` hook, reading stdin with
  a cap and a deadline. I rejected a new hook verb because it stays inert until `dross hooks ensure`
  re-runs.
- **Escape flag.** I added `--full` to both show commands, and the marker names it. Rejected: no
  escape, because a `| head` truncation or an unforeseen compaction path would leave the rules
  unrecoverable. tty_behaviour's no-flag lock is about test streaming, not this.
- **Store order.** store (t-4) → consumer (t-10) → producer hook (t-11). t-10 alone is inert-safe
  (nothing is "seen" until t-11), so every intermediate commit fails to "full".
- **Failure unit.** A failure is a leaf `--- FAIL` subtest, not the top-level test. A 50-case table
  break is 50 failures under failure_bound rather than one 30-line block that hides 49 of them.
- **`+N more failing`.** This line is a count only, per the locked text. I rejected listing the
  remaining names because 500 names breaks the ~300-line mass-break bound.
- **Line width.** Each printed line is cut at 240 chars, and a run's output stays at or under
  30,000 bytes. Rejected: uncapped lines, because a minified-JS diff would push the summary past
  the Bash tool's window and lose the middle failures.
- **Log path timing.** The path prints before the first spawn and again at the end. Printing it
  only at the end leaves nothing when the Bash tool reaps a 10-minute run (seen on internal/cmd).
- **Prune order.** Logs are pruned by mtime, never the file just created. Pruning by name
  timestamp would delete a live 1h40m verify log after 20 task test runs. A silent live log can
  still be lost; that's accepted as residual.
- **Runner detection.** The parser is picked by sniffing the output, and an ambiguous claim falls
  back. Matching the command line misses `make test` / `pnpm test` wrappers.
- **One writer.** The same writer value goes to child stdout and stderr, so os/exec shares one fd
  and the log keeps real interleave. The cost: under a TTY, child stderr shows on stdout.
- **Every invocation logs.** Refusals included, every invocation writes a log and prints its path,
  per the literal reading of c-1. Rejected: lazy creation at the first spawn, which leaves "every
  run" ambiguous.
- **Mid-phase state.** t-9 prints the t-3 fallback for every non-TTY run until t-12 lands. Rejected:
  log-path-only output, because a `make install` mid-phase would leave the agent gating blind.
- **Log open failure.** If the log can't be opened, the run streams live with a warning. Failing
  the run would turn a disk problem into a red suite (c-4).
- **Taint.** One `//dross:taint-cleared` on runlog's file write, landed with the first real flow
  (t-9). Rejected: a taint-policy edit, which is a larger blast radius, and an early marker, which
  fails as "clears nothing".
- **Before file.** Before-numbers go in flow-cost-before.toml and merge at t-15. Rejected: writing
  flow-cost.toml at t-1, which turns every commit gate red for the rest of the phase.
- **Measured flow.** The measured flow includes a red → fix step. A green-only flow can't observe
  the milestone hot spot this phase exists for, a summary that forces a log re-read (log_reads).
- **Live marker check.** t-15 checks the marker live. Rejected: trusting unit tests alone, because
  the hook payload and env behaviour are external to the repo and only a real session proves them.
- **Gremlins invocation echo.** It moves to the log. The bounded failure head stays on stderr, a
  failure-only diagnostic that is not per-mutant.
- **Stryker "package".** For stryker and stryker-net, a "package" is the directory. Neither tool has
  a package concept, and a directory groups the same way gremlins' Go packages do.
- **Lived vs survived.** The column is lived = Survived − NotCovered, so the rows sum to the leg's
  aggregates and NOT COVERED isn't double-counted.
- **survivor drain.** `dross survivor drain` keeps a nil Stream (today's behaviour). c-5 names
  `dross verify` legs only.
