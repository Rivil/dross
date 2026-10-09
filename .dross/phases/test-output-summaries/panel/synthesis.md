# test-output-summaries — panel synthesis

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 5/5. All 8 criteria, including every run shape, refusals, remote, collected and a live marker check. It is the only draft that sees the taint gate, the lane-ordering tests and the hermetic-env hole. | 5/5. Every contract names a test, an input and an observable. Two contracts point at cli_surface goldens that don't exist, and the prompt-phrase absence check is vacuous because the phrase wraps across lines. | 4/5. t-9 is 6 files over 2 layers. The split is forced, but the task still misses a 7th file it must edit (test_lane_selector_test.go). t-10 and t-14 sit at 5 files. | 4/5. t-13 waits on t-9 for a TTY seam that could land in wave 1. t-14 waits on t-11 and t-13, and uses neither. |
| mvp | 3/5. 8/8 on paper. With no prompt change and no log_reads measure, c-3's "act without the log" stays unproven. There is no handling for the taint gate, the ordering tests or the hermetic env, so t-4 would go red. | 3/5. Inputs and outputs are concrete, but the bullets are unnamed and one hedges "(or the implementation's equivalent row)". | 2/5. t-1 bundles the log store, the summariser and three parsers (6 files, 2 concerns). t-3 is 5 files over 2 layers. Both break the split rule. | 4/5. Three waves with correct deps. Measuring before and after in one end task avoids the glob trap, but that only works because the draft drops the prompt edit. |
| verification | 4/5. 8/8, with every locked decision mapped to a named test. It misses the taint gate and the lane-ordering tests (t-11 would go red), and only its new tests pin the session env. | 5/5. Inputs and observables are exact (41 packages/72s → "1m12s", lines 61–100). TestNoStreamingFlag is the right mechanism. It shares the vacuous phrase check. | 4/5. The gremlins/stryker split follows the 5-file rule. t-11 is 3 files but carries 11 contracts and omits the ordering-test files it has to edit. | 4/5. Putting the TTY seam in t-2 decouples verify wiring from test wiring. t-8 waits on t-4 for an interface that its method doesn't need in order to compile. |

**Skeleton: risk.** It is the only draft that stays green commit by commit: it handles the taint gate, the ordering tests, the hermetic env and the flow-cost glob. Its contracts are also the most specific of the three.

## Merged plan

Granularity exceptions, each forced, are flagged per task:

- **Parser fixtures (t-8, t-9).** Fixture captures count as one testdata artifact.
- **t-10.** Its tests and the taint marker must ride the wiring commit, or the suite goes red.
- **t-11 and t-15.** Each has a 5th file that is a one-row edit. On its own that file would be a task under 10 minutes, which the merge rule forbids.

```
Phase test-output-summaries — 16 tasks across 5 waves

Wave 1
  t-1  Record before-numbers on the base-commit binary   [risk+verification]
       files:    .dross/phases/test-output-summaries/flow-cost-before.toml
       covers:   c-8
       depends:  —
       desc:     Build dross from changes.json base_commit 54ad9064 into a temp dir, put it first
                 on PATH and record its `dross version` [verification]. Run this before t-15's
                 `make install` re-links the edited prompts (r-01). Use fixed scratch repos [risk]:
                 a Go module with 6 packages and one seeded failing test, and a gremlins phase on
                 a 2-package module (local gremlins v0.6.0). Record green_test_bytes,
                 red_test_bytes, verify_leg_bytes and rule_show_repeat_bytes as
                 `dross … >f 2>&1; wc -c <f`. The human drives one pair-mode /dross-execute task
                 over §1e red → fix → green → §1f full `dross test` → commit [risk: the red→fix
                 step is what makes log_reads meaningful]. Count agent_turns, turn_tools,
                 wall_clock_s, dross_calls and log_reads with jq over the transcript, using the
                 context-boundaries method. Write [scenario] and [before] to
                 flow-cost-before.toml.
       contract: if the half record is globbed, TestFlowCostRecords fails on every later commit
                   gate: after t-1 no phases/test-output-summaries/flow-cost.toml exists, and the
                   `phases/*/flow-cost.toml` glob (flowcost_record_test.go:45) does not match
                   flow-cost-before.toml
                 if before ran on a rebuilt or stale binary, t-16's TestFlowCostRecords fails:
                   before.binary_version is the base-commit build's and must differ from
                   after.binary_version
                 if a field is missing, t-16's merge fails TestFlowCostRecords: [scenario] carries
                   repo_shape, wave_position, flow_start, flow_end and jq_method; [before] has
                   agent_turns > 0, len(turn_tools) == agent_turns, non-empty dross_calls and
                   every *_bytes > 0

  t-2  Add self-ignoring run-log sink and stdout-TTY seam   [risk+verification]
       files:    internal/runlog/runlog.go, internal/runlog/runlog_test.go,
                 internal/cmd/output_mode.go, internal/cmd/output_mode_test.go
       covers:   c-1, c-5
       depends:  —
       desc:     Open(root, Name{Kind test|verify, Phase, Leg}) returns a *Log created lazily on
                 the first Write [verification; see D8]. Before the file exists, write or rewrite
                 .dross/logs/.gitignore to `*`, and refuse a .gitignore that is not a regular file.
                 Name files test-<ts>.log or verify-<phase>-<leg>-<ts>.log, with ts in UTC to the
                 nanosecond. Create with O_EXCL plus a -N suffix on collision. Phase and leg pass
                 pathfence.Segment. Prune `^(test|verify)-.*\.log$` to the newest 20 by mtime,
                 never touching the file just created and ignoring ENOENT. Write is
                 mutex-guarded and always returns (len(p), nil); Err() reports the first failure,
                 because io.MultiWriter would otherwise stop the child's pipe copy. Path() is
                 absolute and "" until the first write. output_mode.go holds stdoutIsTerminal over
                 golang.org/x/term (already in go.mod) for t-10 and t-12 [verification].
       contract: if logs dirty the tree, TestLogDirIgnored fails: in a git temp repo with .dross/
                   tracked, Open+Write leaves `git status --porcelain -uall` empty and
                   `git check-ignore -q` matches the log
                 if an edited ignore file is trusted, the same test fails: a pre-existing
                   .dross/logs/.gitignore holding `foo` is rewritten to `*` before the log exists
                 if a non-file ignore is accepted, TestGitignoreDirRefused fails: a .gitignore
                   that is a directory makes the first Write's Err() non-nil
                 if the log precedes its ignore file, TestIgnoreBeforeLog fails: the create-order
                   seam records .gitignore → log
                 if an unwritten log leaves debris, TestUnwrittenLogCreatesNothing fails: Open +
                   Close with no Write leaves no .dross/logs/
                 if same-instant runs clobber, TestSameInstantDistinctFiles fails: two Opens under
                   a frozen clock seam yield two distinct paths, each holding only its own bytes
                 if pruning is wrong, TestPruneNewest20 fails: 25 mixed test-/verify- logs with
                   distinct mtimes plus .gitignore and notes.txt → after one Open, the 20 newest
                   by mtime survive (including the new one, and an old-named log whose mtime is
                   newest), .gitignore and notes.txt remain, and a log deleted mid-prune (ENOENT)
                   is not an error
                 if a log error can kill the child, TestWriteNeverFails fails: with the file seam
                   failing after 1 KiB, every Write returns (len(p), nil) and Err() names the
                   first failure
                 if a name escapes, TestNameStaysInDir fails: Phase "../../etc" or Leg "go/x" is
                   either refused or yields a separator-free base name inside .dross/logs/
                 if logs cross the wire, TestLogsOffTheWire fails: after a log is written in a git
                   temp repo, the exclude file remote.SyncArgs builds (from `ls-files --others
                   --ignored`, remote.go:734) excludes /.dross/logs/
                 if writes race, `go test -race` TestConcurrentWrites fails: 8 goroutines ×
                   1,000 writes → no race, and file length == bytes written
                 if TTY detection inverts, TestPipeIsNotTerminal fails: with an os.Pipe swapped
                   into os.Stdout, stdoutIsTerminal() is false

  t-3  Add bounded summary core and renderer   [risk+verification]
       files:    internal/runsummary/summary.go, internal/runsummary/render.go,
                 internal/runsummary/summary_test.go
       covers:   c-2, c-3, c-4
       depends:  —
       desc:     A streaming line splitter that cuts lines at 64 KiB (so no ErrTooLong stop) and
                 strips \r and ANSI SGR. A Parser interface (Claims/Feed/Result) with an
                 init()-registered list, a 40-line tail ring and a per-run Budget.
                 Render(outcome{exit, ran}, results, budget, logPath, wall) prints green only when
                 exit == 0, a parser claimed the output and failures == 0. Otherwise it prints red
                 (counts and failures) or the fallback (exit status, last 40 lines, log path).
                 Printed lines are cut at 240 chars. The "+N more failing — see" prefix is an
                 exported constant, which t-15's prompt test quotes [verification].
       contract: if a non-zero exit can read green, TestNonZeroNeverGreen fails: a fake parser
                   reporting 12 ok / 0 failures with exit 1 renders the fallback (exit 1, last 40
                   lines, log path) and no green line
                 if a run that didn't happen shows counts, TestRunDidNotHappenNeverCounts fails:
                   exit 3 and exit 4 over output that parses green print the exit status and the
                   tail, never counts [verification]
                 if unparseable output is mishandled, TestUnparseableFallback fails: 100 unclaimed
                   lines at exit 2 → an `exit 2` line, lines 61-100 verbatim (line 60 absent) and
                   the log path; at exit 0 the same shape and no counts
                 if the bound slips, TestFailureBound fails: 14 failures × 50 lines → 10 blocks of
                   30 lines, each ending `… 20 more lines — see <log>`, then exactly
                   `+4 more failing — see <log>`
                 if a mass break overflows the Bash window, TestMassBreakSize fails: 500 failures
                   × 100 lines of 2,000 chars render ≤ 320 lines and ≤ 30,000 bytes
                 if a long line stops parsing, TestLongLineKeepsParsing fails: a 10 MiB line with
                   no newline, then `SENTINEL` → the fake parser still receives SENTINEL
                 if memory is unbounded, TestBoundedRetention fails: 2,000,000 fed lines retain
                   ≤ 1 MiB (Retained()), and the tail ring holds exactly the last 40
                 if colour or CRLF breaks matching, TestStripsANSIAndCR fails:
                   "\x1b[31mFAIL\x1b[0m\r\n" reaches Feed as "FAIL"
                 if dross guesses between runners, TestAmbiguousClaimFallsBack fails: two fake
                   parsers both claiming one segment → fallback render
                 if the budget isn't shared, TestBudgetSpansSegments fails: two segments × 8
                   failures through one Budget → 10 full blocks in total, then `+6 more failing`
                 if green is chatty, TestGreenIsShort fails: a fake 60-package green over 72s →
                   ≤ 3 lines (counts, "1m12s", log path) and no package name [risk+verification]

  t-4  Add show-marker store; make cmd tests session-hermetic   [risk]
       files:    internal/gatestate/showmark.go, internal/gatestate/showmark_test.go,
                 internal/cmd/hermetic_env_test.go
       covers:   c-6, c-7
       depends:  —
       desc:     Under .dross/gate/show/ (already self-ignored by gatestate's ensureDir),
                 BumpContext(root, sid) replaces a repo-wide epoch nonce and marks sid as seen.
                 ShownCurrent(root, sid, kind, sha) is read-only. It is true only when sid is
                 non-empty, sid is seen, and sid's kind record holds {current epoch, sha}.
                 RecordShown writes that record by atomic rename. Session keys are hashed the way
                 nudgeKey hashes them. Records and seen-marks older than 7 days are pruned on
                 write, inside show/ only. The hermetic TestMain also unsets
                 CLAUDE_CODE_SESSION_ID and CLAUDE_CODE_CHILD_SESSION. Today it clears only HOME
                 and CLAUDE_CONFIG_DIR, and TestInteractionShowEmitsPlaybook resolves the real
                 repo root. This edit moved here from risk's t-10 to keep t-11 under 5 files.
       contract: if an unhooked session gets the marker, TestUnseenSessionNeverCurrent fails:
                   RecordShown(s, rules, h) with no BumpContext(s) → ShownCurrent false
                 if one session's start doesn't reset the others, TestAnyBumpInvalidates fails:
                   Bump(s1); Record(s1, rules, h); Bump(s2) → ShownCurrent(s1, rules, h) false
                 if content isn't compared, TestShaMismatch fails: Record(s, rules, h1) →
                   ShownCurrent(s, rules, h2) false
                 if kinds clobber, TestKindsIndependent fails: Record(s, rules, h) leaves
                   ShownCurrent(s, playbook, h) false; 32 goroutines recording both kinds leave
                   both readable
                 if an id escapes, TestShowKeyContained fails: sids "../../x" and "a/b" land in
                   .dross/gate/show/; an empty sid → ShownCurrent false and RecordShown errors
                 if records dirty the tree, TestShowRecordsIgnored fails: `git status --porcelain`
                   is empty after Bump+Record in a git temp repo
                 if prune overreaches, TestShowPrune fails: an 8-day-old record is pruned on the
                   next write, while nudge claims and green.json are untouched
                 if the read path writes, TestShownCurrentReadOnly fails: a fresh root gets no
                   .dross/gate/show/
                 if tests read the developer's store, TestHermeticSessionEnv fails:
                   CLAUDE_CODE_SESSION_ID and CLAUDE_CODE_CHILD_SESSION are empty inside the cmd
                   package

  t-5  Route gremlins tool stream through a Streamer sink   [verification+risk+mvp]
       files:    internal/mutation/adapter.go, internal/mutation/gremlins.go,
                 internal/mutation/gremlins_stream_test.go
       covers:   c-5
       depends:  —
       desc:     Add `Streamer interface{ SetStream(io.Writer) }` to adapter.go, plus
                 Gremlins.SetStream (nil → os.Stderr, today's behaviour). The gremlins.go:222 tee
                 becomes MultiWriter(stream, head), and the per-package invocation echo goes to
                 the stream. printHead and the "skipped" lines stay on os.Stderr (see D10). No
                 taint-cleared marker goes in gremlins.go.
       contract: if the stream still leaks, TestGremlinsStreamToSink fails: the
                   gremlinsBuildCmd seam prints 300 KILLED/LIVED lines and writes a valid report;
                   with SetStream(buf), captured os.Stderr holds none of them, buf holds all 300,
                   the Report counts are unchanged, and headBuffer still holds them, so the
                   failure re-print is not starved [mvp]
                 if the remote arm leaks, TestGremlinsRemoteArmStreamToSink fails: the same
                   split via scriptRemote with g.Remote set
                 if diagnosis is lost, TestGremlinsFailureHeadStaysOnStderr fails: a reportless
                   exit 2 → os.Stderr holds the bounded head (≤ strykerHeadLines lines), and the
                   error keeps its RecordToolFailure kind
                 if the default moves, TestGremlinsNilStreamKeepsStderr fails, and
                   gremlins_failure_test.go and remote_failure_test.go go red unmodified [mvp]
                 if a marker sneaks in, TestToolRunnersCarryNoMarker (existing) fails

  t-6  Route stryker and stryker-net streams to the sink   [verification+risk+mvp]
       files:    internal/mutation/stryker.go, internal/mutation/stryker_net.go,
                 internal/mutation/stryker_stream_test.go
       covers:   c-5
       depends:  —   (verification had t-4. SetStream is a plain method and compiles without
                      the interface; conformance is asserted in t-12)
       desc:     SetStream on Stryker and StrykerNet. The stryker.go:155 and stryker_net.go:155
                 tees write to the stream. printHead stays on os.Stderr.
       contract: if the stream still leaks, TestStrykerStreamToSink and TestStrykerNetStreamToSink
                   fail: with the strykerBuildCmd / strykerNetBuildCmd seam printing
                   "[Survived] …" lines and writing a report, captured stderr has no
                   "[Survived]", the sink has every line, the Report is unchanged, and headBuffer
                   still holds the lines
                 if diagnosis is lost, TestStrykerFailureHeadStaysOnStderr fails: a reportless
                   non-zero exit prints the bounded head on stderr [risk]
                 if the default moves, TestStrykerNilStreamKeepsStderr fails, and
                   stryker_failure_test.go and stryker_net_failure_test.go go red unmodified
                 if a marker sneaks in, TestToolRunnersCarryNoMarker fails

  t-7  Print per-package counts and survivor lines   [risk+verification+mvp]
       files:    internal/verify/legsummary.go, internal/verify/legsummary_test.go,
                 internal/cmd/verify.go
       covers:   c-5
       depends:  —
       desc:     LegLines(LanguageRun) groups Mutation.Files by directory for all three tools.
                 Columns are killed / survived / not covered / timed out, where survived includes
                 not covered, as the leg line and tests.json already count it (verify.go:514;
                 see D11). Each in-scope survivor follows as file:line, grouped per file, with
                 NOT COVERED marked. printVerifySummary prints the rows under each leg's existing
                 counts line, so attached and collected runs share them through finishVerify.
       contract: if rows don't sum, TestPackageRowsSumToLeg fails: a report over
                   internal/a/x.go, internal/a/y.go and internal/b/z.go gives exactly one row each
                   for internal/a (x+y summed) and internal/b, with no per-file row, and each
                   column sums to the leg's Killed, Survived, NotCovered and Timeout
                   [risk+verification]
                 if survivors are wrong, TestSurvivorLines fails: 5 in-scope survivors over 2
                   files (one NOT COVERED) → each file:line printed exactly once, NC marked, and
                   no out-of-scope survivor listed
                 if stryker rows mis-group, TestStrykerRowsByDir fails: src/a/x.ts and
                   src/a/y.ts → one src/a row
                 if a failed leg grows rows, TestErrorLegUnchanged fails: an Error or a nil
                   Mutation → only today's line
                 if the wiring is missing, TestVerifySummaryPrintsPackagesAndSurvivors
                   (internal/cmd) fails: finishVerify on seeded Tests prints the rows and every
                   survivor's file:line

Wave 2
  t-8  Parse plain go test output (depends t-3)   [risk+verification]
       files:    internal/runsummary/gotest.go, internal/runsummary/gotest_test.go,
                 internal/runsummary/testdata/gotest/ (green, red_subtests, panic, timeout,
                 build_failed, setup_and_vet captures)
       covers:   c-2, c-3
       depends:  t-3
       desc:     Fixtures are captured from a scratch module with a real `go test -count=1 ./...`
                 (go1.26.1), stdout and stderr on one fd. Each starts with a `# captured:` header
                 naming the go version and the command. Counts are ok / FAIL / no test files /
                 no tests to run packages. A failure is a leaf `--- FAIL` with its indented
                 output (see D7). A FAIL package with no test failure (build failed, setup
                 failed, timeout panic, TestMain exit) becomes a package-named failure. Each
                 `# pkg` block is one build failure.
       contract: if counts drift, TestGoGreenCounts fails: green.txt → 4 ok, 2 no test files,
                   1 no tests to run, 0 failures
                 if subtests double-count, TestGoSubtestsAreLeaves fails: red_subtests.txt →
                   TestTable/a, /b, /c and not TestTable
                 if failure output bleeds, TestGoRedNamesOwnLines fails: TestAdd's block holds its
                   `_test.go:` assertion line and none of TestTable's lines [verification]
                 if panic text is lost, TestGoPanicAttributed fails: panic.txt → TestBoom's block
                   holds "panic: boom" and its first stack frame
                 if a timeout reads as unparseable, TestGoTimeout fails: timeout.txt
                   (-timeout 2s) → a package failure that starts "panic: test timed out after 2s"
                   and names the running test
                 if build errors vanish, TestGoBuildFailed fails: build_failed.txt → the `# dep`
                   block's "undefined: foo" printed once, and every `[build failed]` package
                   counted as failed
                 if setup/vet/test-file compile errors are missed, TestGoSetupAndVet fails: a
                   `[setup failed]` package and a `# pkg [pkg.test]` block each become a failure
                   carrying their error line
                 if go claims foreign text, TestGoClaimsOnlyGo fails: a make transcript with
                   "ok  done" and t-9's vitest fixtures are not claimed
                 if a fixture is invented, TestGoFixturesCaptured fails: any testdata/gotest file
                   lacks the `# captured: go1.` header

  t-9  Parse vitest and jest output (depends t-3)   [risk+verification]
       files:    internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go,
                 internal/runsummary/testdata/js/ (vitest_green, vitest_red, vitest_red_color,
                 vitest_load_error, jest_red, jest_suite_failed captures)
       covers:   c-2, c-3
       depends:  t-3
       desc:     vitest fixtures are captured with the vitest 4.1.11 pinned in
                 internal/mutation/testdata/ts-project (confirmed), from a scratch copy, run plain
                 and with FORCE_COLOR=1. jest fixtures come from an exact-pinned jest in a scratch
                 dir outside the repo, installed with --ignore-scripts [verification]. Each
                 fixture carries a `# captured:` header. Counts are the runner's own summary
                 lines; failures are FAIL and ● blocks. Load errors, "Test suite failed to run"
                 and "Unhandled Errors" count as failures.
       contract: if counts aren't the runner's, TestVitestCounts fails: vitest_red.txt →
                   "Tests 1 failed | 4 passed (5)" and "Test Files 1 failed | 1 passed (2)", plus
                   a failure named "src/math.test.ts > add > negatives" carrying its
                   AssertionError and diff; vitest_green.txt → Passed equals its "Tests N passed"
                   [verification]
                 if a load error is lost, TestVitestLoadError fails: vitest_load_error.txt → a
                   failure carrying the transform/syntax error line
                 if jest is wrong, TestJestCounts and TestJestSuiteFailed fail: jest_red.txt →
                   "Tests: 1 failed, 3 passed, 4 total" and the ● block as one failure;
                   jest_suite_failed.txt → a failure carrying "Test suite failed to run"
                 if colour breaks parsing, TestJSForcedColour fails: the FORCE_COLOR capture of
                   vitest_red parses to a Result equal to the plain one's
                 if truncated output is claimed, TestJSTruncatedFallsBack fails: vitest_red cut
                   before its "Tests" line is not claimed
                 if runners cross, TestJSDoesNotClaimGo fails: t-8's go fixtures are not claimed
                 if a fixture is invented, TestJSFixturesCaptured fails: a fixture lacks the
                   `# captured: vitest@|jest@` header

  t-10 Route dross test output through the run log (depends t-2, t-3)   [risk+verification]
       files:    internal/cmd/test.go, internal/cmd/test_output.go,
                 internal/cmd/test_output_test.go, internal/cmd/test_lane_consent_test.go,
                 internal/cmd/test_lane_prepare_test.go, internal/cmd/test_lane_selector_test.go,
                 internal/runlog/runlog.go
       covers:   c-1, c-4
       depends:  t-2, t-3
       desc:     test_output.go opens a per-invocation sink after FindRoot. dross writes a
                 `$ <line>` header before each spawn [verification], so even a silent command
                 logs. That first write creates the file, and `log: <abs path>` then prints
                 before the first spawn and again at the end [risk; see D9]. TTY: one writer =
                 log + os.Stdout. Non-TTY: one writer = log + segment feed. The same writer value
                 goes to every spawn's stdout and stderr (runTest, runOneLane, runLanePrepare,
                 syncTreeTo, runRemoteLine). dross's own lines still reach stdout and are copied
                 to the log. Until t-14 lands, every non-TTY run prints t-3's fallback. If the
                 log can't be opened, the run streams live with one warning. The Long help
                 (test.go:242) drops "streams". No new flag. Exactly one //dross:taint-cleared
                 marker goes in internal/runlog (see D1). Granularity exception: the three
                 ordering tests read lane output from captured (non-TTY) stdout and go red the
                 moment non-TTY runs stop streaming. A runlog marker committed before the first
                 flow is an orphan finding. Both therefore ride this commit.
       contract: if a killed run leaves no path, TestLogPathBeforeSpawn fails: the fake spawn sees
                   stdout already holding "log: /…/.dross/logs/test-"
                 if a run skips the log, TestEveryRunWritesLog fails: bare, selector, --files,
                   lanes-only bare, local and remote (recorders) each leave exactly one new
                   test-*.log holding OUT-MARK (written to stdout) and ERR-MARK (written to
                   stderr), and the remote shape's log also holds the rsync segment
                   [risk+verification]
                 if a refused run churns the log window, TestRefusalWritesNoLog fails: an
                   untrusted run leaves .dross/logs/ absent [verification; see D8]
                 if streams split, TestOneWriterBothStreams fails: every spawn seam receives
                   stdout == stderr (the same writer value)
                 if TTY handling inverts, TestTTYStreamsNonTTYSummarises fails: non-TTY → the
                   sentinel is in the log and not on stdout; TTY seam on → the sentinel is on
                   stdout and in the log, and no summary block is printed [verification]
                 if ordering tests rot, TestLaneHeaderPrecedesLaneOutput,
                   TestPrepareLinePrecedesTheLaneHeaderAndItsOutput and
                   TestSelectorHeaderStillPrecedesLaneOutput fail unless prepare < header <
                   sentinel by byte offset in the log; prepare and header lines still appear on
                   stdout; names are kept for TestNoTestLost (code-check: risk named only the
                   first two; the selector test asserts the same order at
                   test_lane_selector_test.go:111)
                 if a broken log dir breaks the run, TestLogUnavailableStreams fails: with
                   .dross/logs a regular file, the sentinel is on stdout, one warning is printed,
                   and the exit code equals the fake's suite outcome
                 if the taint gate breaks, TestNoSpawnOutputEscapes fails; the only new marker is
                   in internal/runlog, so TestToolRunnersCarryNoMarker stays green
                 if a streaming flag appears, TestNoStreamingFlag fails: Test().Flags() defines
                   exactly local, files and wait [verification] (code-check: TestCLISurfacePinned
                   pins only trust/issue/doctor/verify, so risk's "test cli_surface golden" does
                   not exist)

  t-11 Print unchanged marker on repeat rule/interaction show (depends t-4)   [risk+verification+mvp]
       files:    internal/cmd/showmark.go, internal/cmd/rule.go, internal/cmd/interaction.go,
                 internal/cmd/showmark_test.go, assets/prompts/rule.md
       covers:   c-6, c-7
       depends:  t-4
       desc:     showOnce(kind, rendered) digests the bytes it would print [verification]. The
                 identity is CLAUDE_CODE_SESSION_ID, treated as empty when
                 CLAUDE_CODE_CHILD_SESSION is set. Env check during this synthesis: a subagent's
                 CLAUDE_CODE_SESSION_ID names the parent's transcript, which holds subagents/,
                 and CHILD_SESSION is set inside the subagent. The command prints one line when
                 all of these hold: the root resolves, the identity is set, --full is unset and
                 ShownCurrent is true. The line reads `rules unchanged — identical to the full
                 block printed earlier in this session; still MUST-FOLLOW (dross rule show --full
                 reprints)`, and the same for the playbook. Otherwise it prints in full, then
                 calls RecordShown, and only after the write returned nil. Store errors never
                 fail the command. `rule list --merged` never marks. rule.md's "show what claude
                 sees" row moves to `dross rule list --merged` [verification; see D3].
                 Granularity exception: interaction.go is a 3-line mirror and rule.md a one-row
                 edit.
       contract: if repeats don't shrink, TestRuleShowRepeatPrintsMarker fails: with
                   BumpContext(root, "s") and env "s", the 1st call prints the full block and the
                   2nd exactly one line starting "rules unchanged", under 120 bytes
                   [risk+verification]
                 if a change hides, TestRuleShowChangeReprints fails: `rule add` between the
                   calls, or a changed global rules.toml under ruleCovFakeHome, makes the 2nd
                   call print the full block containing the change [risk+verification]
                 if the id is missing, TestRuleShowNoSessionFull fails: env unset → full both
                   times, and nothing is created under .dross/gate/show/
                 if a subagent is short-changed, TestShowChildSessionFull fails:
                   CLAUDE_CODE_CHILD_SESSION=1 with a current record for "s" → the full block,
                   for both kinds
                 if a record precedes the print, TestShowRecordsOnlyAfterPrint fails: a print seam
                   returning EPIPE → no record, and the next call prints full
                 if the store can fail the command, TestShowStoreFailureStillPrints fails: with
                   .dross/gate a regular file → full block, exit 0
                 if the kinds tangle, TestInteractionShowMarker fails: a repeat prints "playbook
                   unchanged", and a rules-only change leaves the playbook marker in place
                 if there is no way back, TestShowFullFlag fails: --full after a current record
                   prints the full block, and the marker line names --full and MUST-FOLLOW
                 if the inspection path marks, TestRuleListMergedNeverMarks fails: `rule list
                   --merged` after a show prints the full block [verification]
                 if /dross-rule shows a marker to a human, TestRulePromptShowUsesListMerged
                   fails: rule.md's "show what claude sees" row names `dross rule list --merged`
                   [verification]
                 if the flag drifts, the show-flags check in showmark_test.go fails: rule show and
                   interaction show each define exactly `full` (replaces risk's rule/interaction
                   cli_surface goldens, which don't exist)
                 if hermeticity regresses, TestInteractionShowEmitsPlaybook (existing) fails when
                   the suite runs from inside a Claude Code session

Wave 3
  t-12 Write verify leg streams to run logs (depends t-2, t-5, t-6, t-10)   [risk+verification+mvp]
       files:    internal/cmd/verify.go, internal/cmd/verify_log_test.go
       covers:   c-5
       depends:  t-2, t-5, t-6, t-10   (t-10 only for the runlog taint marker: whichever flow
                                        lands first must carry it, see D1)
       desc:     Attached runs: before RunScoped, every adapter implementing mutation.Streamer
                 gets a lazy runlog writer for verify-<phase>-<adapter.Name()>-<ts>.log (TTY:
                 log + os.Stderr; non-TTY: log only), and each leg prints its log path. Collected
                 runs: a detachFetchLog seam built on remote.FetchArgs + runDetachArgv pulls
                 <runDir>/log (written on the host, remote.go:416) into a fresh verify log. A
                 fetch failure prints "log unavailable: <why>" and the collect carries on. If a
                 log can't be opened, that leg streams to stderr with one warning.
       contract: if mutant lines reach the conversation, TestAttachedLegLogsStream fails: verify
                   with a fake Streamer adapter (configuredAdaptersFn) printing 300 mutant lines,
                   non-TTY → stdout and stderr hold none of them, verify-<phase>-<name>-*.log
                   holds all, and its absolute path is printed
                 if an adapter drops the seam, TestAdaptersAreStreamers fails: *Gremlins,
                   *Stryker and *StrykerNet each satisfy mutation.Streamer
                 if the human loses live output, TestTTYLegStillStreams fails: with the TTY seam
                   on, mutant lines go to stderr and to the log
                 if an idle leg churns the window, TestVerifyIdleLegOpensNoLog fails: with two
                   adapters and one dispatched, exactly one verify log exists [verification]
                 if the collected stream is lost, TestCollectFetchesHostLog fails: the fake fetch
                   copies a host log → a new verify log holds it, and its path is printed
                 if the log can fail a collect, TestCollectLogFetchFailureIsANote fails: the fetch
                   returns a transport error → tests.json and verify.toml are still written, the
                   exit is as before, and "log unavailable" is printed
                 if the fetch skips the audited seams, TestEverySpawnSiteGatedOrExempt fails (the
                   argv is remote.FetchArgs(target, <runDir>/log, local))
                 if a second marker is needed, TestNoSpawnOutputEscapes fails: no new marker
                   outside internal/runlog
                 if codes move, TestVerifyExitCodesWithLogs fails: --no-wait on a held host still
                   exits 15, and an unwritable .dross/logs still yields the same verdict

  t-13 Bump context epoch from the SessionStart hook (depends t-11)   [risk]
       files:    internal/cmd/reentry.go, internal/cmd/reentry_test.go
       covers:   c-6, c-7
       depends:  t-11
       desc:     `dross reentry` is already the wired SessionStart target (hooks.go:60), so this
                 goes live on binary upgrade. Today it does not read stdin. It now reads stdin
                 only when stdin isn't a terminal, capped at 1 MiB, behind a 300 ms deadline
                 goroutine. Any payload with a session_id calls BumpContext, and every source
                 (startup, resume, clear, compact) bumps. A bump failure is silent; the envelope
                 bytes and the exit code are unchanged.
       contract: if a stale env id survives /clear, TestClearThenShowPrintsFull fails: rule show
                   ×2 (marker) → reentry with source "clear" and a NEW session_id while
                   CLAUDE_CODE_SESSION_ID keeps the old one → the next rule show prints the full
                   block
                 if compaction is invisible, TestCompactThenShowPrintsFull fails: the same
                   session id with source "compact" → the full block, for both kinds
                 if a source is skipped, TestReentryBumpsEverySource fails: startup, resume, clear
                   and compact each change the epoch and mark the id seen
                 if the hook can hang, TestReentryOpenPipeReturns fails: with stdin an os.Pipe
                   never written or closed, reentry returns in < 500 ms with golden output and no
                   bump; with the terminal seam on, stdin is not read
                 if hook output moves, the output/reentry.txt golden fails with a payload on stdin
                 if a bad payload breaks the hook, TestReentryBadPayload fails: non-JSON, a 10 MiB
                   payload or an unwritable .dross/gate → golden output, exit 0, no bump

  t-14 Print per-lane summaries and outcome tails (depends t-8, t-9, t-10)   [risk+mvp+verification]
       files:    internal/cmd/test.go, internal/cmd/test_output.go,
                 internal/cmd/test_summary_wiring_test.go
       covers:   c-2, c-3, c-4
       depends:  t-8, t-9, t-10
       desc:     Each spawn is its own segment, parsed by whichever parser claims it, through one
                 Budget per run. The summary renders after each lane, from the classified outcome
                 (the exit code from ExitCodeError and the exitCoder). Exits 3/4/7 print the
                 failing spawn's last 40 lines. A selector miss prints only its miss line.
                 Summaries go to stdout and never into a returned error. Wall-clock comes from a
                 testNow seam and covers the whole run [verification].
       contract: if green leaks lines, TestGreenRunPrintsSummaryOnly fails: a fake writing
                   gotest/green.txt → stdout holds the log line, one counts line and the
                   wall-clock, no fixture package path, and ≤ 300 bytes [risk+verification]
                 if red is incomplete, TestRedRunPrintsFailures fails: red_subtests.txt + exit 1 →
                   the three leaf names and their lines on stdout, exit 1
                 if a failed spawn reads green, TestNonZeroAllOkFallsBack fails: green.txt written
                   but exit 1 → the fallback (exit 1, last 40 lines), no green line, exit code 1
                 if transport loses its why, TestTransportTailShown fails: a remote fake writing
                   "ssh: connect to host h port 22: Connection refused" and exiting 255 → exit 3
                   with that line and the log path on stdout; the same for exit 4, and for exit 7
                   with "make: *** [build] Error 2"
                 if lanes blur, TestTwoLanesEachParsed fails: a go lane (red_subtests.txt) and a
                   vitest lane (vitest_green.txt) → two lane summaries, overall red, exit 1
                 if a parsed lane masks an unparsed one, TestParsedLaneCannotMaskUnparsed fails:
                   a go-green lane and an unrecognised-output lane, both exit 0 → go package
                   counts and the other lane's exit status + tail [mvp]
                 if the budget resets per lane, TestBudgetAcrossLanes fails: two lanes × 8
                   failures → 10 blocks, then "+6 more failing"
                 if a miss dumps output, TestSelectorMissNoTail fails: an EmptyExit miss prints
                   only its miss line
                 if summarising leaks into exit classification, TestOutputIsNeverScraped
                   (existing, test_lane_miss_test.go) fails
                 if failures escape into errors, TestSummaryNotInError fails: the returned error's
                   text holds no fixture line
                 if the clock is wrong, TestWallClockFromSeam fails: testNow advancing 75s prints
                   "1m15s" [verification]
                 if exit codes move, TestExitCodesUnchanged fails: fakes for 0, 1, 3, 4, 5, 6, 7
                   and 8 exit as before, and a green full run in summary mode still writes
                   .dross/gate/green.json [risk+mvp+verification]

Wave 4
  t-15 Rewrite execute/quick test-step guidance (depends t-11, t-14)   [risk+verification]
       files:    assets/prompts/execute.md, assets/prompts/quick.md, README.md,
                 internal/cmd/execute_prompt_test.go, internal/cmd/quick_prompt_test.go
       covers:   c-3, c-8
       depends:  t-11, t-14
       desc:     §1e tells the agent that `dross test` prints a summary (counts, failing tests
                 with their output, the log path). On red, act on the summary's failures and open
                 the log only when a block says it was cut. "streaming output as it arrives"
                 (execute.md:198-199) and "surface the failure tail (last 30-40 lines)"
                 (execute.md:239, quick.md:120) are replaced with no net growth. README's
                 dross test row gains the log and summary; its rule show row gains the marker.
                 Then `make install` (r-01). Granularity exception: README is a two-row edit.
       contract: if the agent is still told to tail or stream, TestExecuteTestStepDescribesSummary
                   and TestQuickTestStepDescribesSummary fail: after whitespace-normalising the
                   prompt, neither "streaming output as it arrives" nor "last 30-40 lines"
                   appears, and §1e names .dross/logs/ and quotes t-3's more-failing prefix
                   constant (code-check: execute.md wraps the phrase across lines 198-199, so the
                   drafts' raw-substring absence check passes today, vacuously)
                 if the green path invites a log read, TestGreenPathNeverReadsLog fails: neither
                   prompt's §1e "**Green**" branch has a cat/Read/open instruction naming
                   .dross/logs [verification]
                 if prompts grow, TestTestStepPromptsNotGrown fails: execute.md ≤ 25,854 bytes
                   (its size at 54ad9064 and today) and quick.md ≤ 18,102 bytes

Wave 5
  t-16 Record after-numbers and gate them (depends t-1, t-7, t-12, t-13, t-15)   [risk+verification+mvp]
       files:    .dross/phases/test-output-summaries/flow-cost.toml,
                 .dross/phases/test-output-summaries/flow-cost-before.toml (merged, then deleted),
                 internal/cmd/flowcost_record_test.go
       covers:   c-8
       depends:  t-1, t-7, t-12, t-13, t-15
       desc:     Run `make install` and `dross hooks ensure`. Confirm `dross doctor` shows
                 SessionStart → dross reentry and that `dross version` matches HEAD. Replay t-1's
                 scenario exactly. Live-check the marker in a real session: a repeat, a /clear, a
                 /compact and a subagent's call [risk]. The live check also settles mvp's open
                 question: CLAUDE_CODE_CHILD_SESSION is set inside a subagent, but whether it is
                 unset in a main session is unverified, and if it is set there too, c-6 never
                 fires. Move [before] into flow-cost.toml beside [after] and [marker_live], then
                 delete flow-cost-before.toml. Extend flowLeg with the *_bytes fields and
                 log_reads, and add a test-output-summaries block to TestFlowCostRecords.
                 Observed numbers only.
       contract: if output didn't shrink, TestFlowCostRecords/test-output-summaries fails:
                   green_test_bytes and verify_leg_bytes are after < before, and
                   after.green_test_bytes ≤ 1024; red_test_bytes is > 0 on both legs but not
                   gated (see D6)
                 if the red step needed the log, it fails: after.log_reads == 0
                 if a round-trip was added, the existing gate fails: after.dross_calls ⊆
                   before.dross_calls
                 if the marker is inert in production, it fails: after.rule_show_repeat_bytes <
                   300, and [marker_live] clear_full, compact_full and subagent_full are all true
                 if the after leg used the old binary, the existing binary_version check fails
                 if the record is missing, the extended `found` check fails: no
                   test-output-summaries flow-cost.toml fails the suite [verification]
                 if the before file lingers, it fails: flow-cost-before.toml no longer exists
                   [risk+verification]
```

Coverage: c-1 → t-2, t-10. c-2 → t-3, t-8, t-9, t-14. c-3 → t-3, t-8, t-9, t-14, t-15. c-4 → t-3, t-10, t-14. c-5 → t-2, t-5, t-6, t-7, t-12. c-6 and c-7 → t-4, t-11, t-13. c-8 → t-1, t-15, t-16. All five locked decisions keep the owners risk gave them (failure_bound in t-3; log_location and log_retention in t-2; tty_behaviour in t-10 and t-12, enforced by TestNoStreamingFlag).

## Disagreements

**D1. How the run log passes the exec-taint gate.** Risk says writing a spawn stream to a file is an escape under TestNoSpawnOutputEscapes. It puts one `//dross:taint-cleared` marker in internal/runlog, landing with the first real flow, and rejects a taint-policy edit. mvp and verification are silent, so their plans go red at the first wiring commit.

The code backs risk on the escape: os.File writes are escapes (taint_engine_test.go:61, :1192), and an early marker is an "orphan" finding. It does not back the fix as written. A marker clears only values *defined* on its bound line and cannot sit on a source (taint_marker_test.go:14-27). A marker on `f.Write(p)` therefore clears nothing, and passing would mean marking a copy line, which bends the marker's documented meaning.

**Default:** risk's single marker. If that only passes vacuously, the policy edit risk rejected (a runlog terminal sink) needs a human call. Every log-writing task depends on this.

**D2. What proves the SessionStart hook ran before a marker may print.**
- **Risk:** a repo-wide epoch bumped by every SessionStart, plus a per-session seen-mark taken from the hook's stdin payload; reentry gains a capped, deadlined stdin read.
- **mvp:** reentry wipes the records, and the marker requires the hook entry in user settings.json.
- **Verification:** the marker requires only that an epoch file exists.

**Default:** risk. Verification's check stays true forever once any session in the repo ran the hook. mvp's check reads configuration, not whether the hook fired for this session. Either can print "unchanged" after an undetected compaction, which drops MUST-FOLLOW rules from context.

**D3. Escape hatch from the marker.**
- **Risk:** `--full` on both show commands, named in the marker line.
- **Verification:** no flag; rule.md's "show what claude sees" row is rerouted to `rule list --merged` (exists, rule.go:96), which has no playbook equivalent.
- **mvp:** no escape hatch.

**Default:** both risk's flag and verification's row. Without `--full`, a context loss the hook misses (`| head`, an unknown compaction path) leaves the playbook unrecoverable. The cost is one flag of new CLI surface.

**D4. Whether the phase edits execute.md and quick.md.** Risk and verification rewrite §1e: no "streaming", no "last 30-40 lines", no net growth. mvp makes no prompt edit. Its reasons: no criterion needs one, prompt-partials rewrites execute.md next, and unedited prompts make the c-8 legs differ only by binary. **Default:** include (t-15). The old text tells a red-path agent to surface a "failure tail", which invites exactly the log Read that c-3 and the log_reads == 0 gate rule out. The cost is a rebase for prompt-partials.

**D5. When the before-numbers are measured.** Risk and verification measure first, on the old binary and old prompts, into a glob-safe draft file that is merged at the end. Verification builds that binary from base_commit (taken). mvp measures both legs at the end, building the before binary from base in a temp worktree, all in one commit. The code backs all three on the trap: TestFlowCostRecords fails a glob-matched file that has no `[after]` (flowcost_record_test.go:45, :63-66). **Default:** a wave-1 before leg. mvp's end-measure is valid only without prompt edits (D4); with t-15 in, its before leg would run on the new prompts.

**D6. Whether red-run bytes are gated.** Risk and verification gate after < before for the red run too. mvp records red bytes and doesn't gate them. **Default:** mvp. In risk's own 6-package scratch repo, a plain red `go test ./...` is about five `ok` lines plus one failure block. The after-output (the log path printed twice, counts, the same failure block, the wall-clock) is about the same size, so the gate would test the scenario rather than the change. c-8 asks for the numbers to be recorded, and c-3's real claim is gated by log_reads == 0.

**D7. Unit of a go failure.** Risk and mvp count each leaf `--- FAIL` subtest. Verification counts the top-level test, with its subtests inside. **Default:** leaf. Under failure_bound's limits (10 in full, 30 lines each), leaf counting names 10 broken cases and counts the rest. Top-level counting spends one 30-line block on a table test and keeps the cap for other tests. The choice decides what a mass table break shows the agent.

**D8. When the log opens, and whether refusals log.** Risk opens eagerly for every invocation, so refusals (exit 2/5/6) still write a log and print its path, which is c-1 read literally. Verification opens lazily on the first write and writes a `$ <line>` header per spawn. Refused runs and idle verify legs then write nothing, so they can't push a red log out of the 20-file window. mvp opens for "every run" without saying when. **Default:** lazy, combined with risk's path-before-first-spawn: the header write creates the file. This trades c-1's literal "every run" for keeping red logs.

**D9. Log path form and timing.** Risk prints an absolute path, before the first spawn and again at the end. Verification prints a repo-relative path at the end, like every other path dross prints. mvp prints a relative path once. **Default:** risk. The agent's Read tool takes absolute paths. And if the Bash tool kills a run (a 10-minute internal/cmd run is a known case), a path printed only at the end is never printed.

**D10. Shape of the mutation sink.** Risk adds a `Stream io.Writer` field per adapter in one 4-file task, and routes "skipped" lines to the stream. Verification adds a `Streamer` interface in adapter.go, splits gremlins from stryker under the 5-file rule, and keeps "skipped" lines on stderr. mvp adds a per-tool package seam next to launcherLog. **Default:** verification's interface, with both halves in wave 1. verify.go receives `[]mutation.Adapter` from configuredAdaptersFn (verify.go:1268), so it needs a generic setter. A package-level seam is shared state across runs.

**D11. What the "survived" column means.** Risk prints "lived" = Survived − NotCovered, so the columns are disjoint. Verification keeps survived inclusive of not covered, as the leg line and tests.json do. The code: NotCovered is a subset of Survived (verify.go:514). **Default:** verification, which also matches the spec's word "survived". Both sum correctly. If the conversation's column means something different from tests.json and the score line, a reader double-counts or under-counts.

**D12. Decomposition grain.** mvp uses 6 tasks: log store, summariser and all parsers in one; c-6/c-7 plus reentry in one; mutation in one. Risk uses 15 tasks and verification 13. Verification wires `dross test` in one task, where risk splits log routing (t-9) from summaries (t-12). **Default:** risk's grain. mvp's t-1 and t-3 break the split rule. Fewer tasks would mean fewer pair-mode gates, which matters under the milestone's time criterion.

## Out-of-scope observations

- [verification] `verify.adapterLanguage` maps "stryker.net" → "csharp", but `StrykerNet.Name()` returns "stryker-net" (verify.go:782 vs stryker_net.go:70), so a C# leg is labelled "stryker-net". Confirmed in code; it predates the phase and should be filed.
- [risk] `dross survivor drain` keeps a nil Stream, i.e. today's stderr stream; c-5 names only `dross verify` legs.
- [risk] Pruning by mtime can still delete a long-running verify log that has gone quiet, once 20 newer logs land. Risk accepts this as residual.
