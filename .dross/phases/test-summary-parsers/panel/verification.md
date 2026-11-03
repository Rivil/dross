# Planner draft: VERIFICATION lens

Method: for each criterion I wrote the test that would prove it first, then sized the task to the smallest change that test needs. Fixtures belong to the parser task that is tested against them. A capture with no test reading it is not a task. Every fixture header records the command, the runner version and the source repo plus commit. Every red fixture header also names the uncommitted break it came from. The provenance tests read those headers, so the fixture_provenance decision is enforced by a test instead of by review.

```
Phase test-summary-parsers — 14 tasks across 3 waves

Wave 1
  t-1  Parse gradle test output from equerry
       files:    internal/runsummary/gradle.go, internal/runsummary/gradle_test.go,
                 internal/runsummary/testdata/gradle/green.txt, internal/runsummary/testdata/gradle/red.txt,
                 internal/runsummary/testdata/gradle/compile_failed.txt
       desc:     Capture `./gradlew testDebugUnitTest` in equerry through a pipe (so plain console), with stdout and stderr
                 on one fd. Capture three runs: green, red from 2 locally broken unit tests (one with a multi-line
                 assertion), and red from a local compile break. Restore the tree after each capture. The parser
                 claims only on `BUILD SUCCESSFUL`/`BUILD FAILED`. Green counts are `BUILD SUCCESSFUL` plus
                 gradle's own `N actionable tasks: …` line. Red adds gradle's `N tests completed, M failed`
                 when gradle prints it. Each `Class > method FAILED` starts a failure named class.method, holding
                 its indented exception lines.
       covers:   c-1
       contract: if green counts drift from gradle_green_counts, TestGradleGreenCounts fails: green.txt → Counts ==
                   ["BUILD SUCCESSFUL", "<N> actionable tasks: …"] exactly, and no phrase contains "test"
                 if a name loses its class or method, TestGradleRedNames fails: red.txt → names == the two broken
                   tests as fully.qualified.Class.method, nothing else (the `* What went wrong:` /
                   `There were failing tests` epilogue opens no failure)
                 if failure output bleeds between tests, TestGradleRedOwnLines fails: each block holds its own
                   assertion line and none of the other test's lines
                 if gradle's red count is dropped, TestGradleRedCounts fails: Counts contains gradle's own
                   "<N> tests completed, 2 failed" verbatim
                 if a compile break reads green, TestGradleCompileFailedNeverGreen fails: compile_failed.txt
                   rendered at exit 1 prints no "PASS" line (a failure named after the task, or the fallback)
                 if a cut-off run is summarised, TestGradleTruncatedNotClaimed fails: red.txt without its
                   BUILD FAILED line and everything after it is not claimed
                 if someone adds JUnit XML reads, TestGradleReadsNoFiles fails: gradle.go imports none of os,
                   io/fs, path/filepath, encoding/xml (locked gradle_green_counts)
                 if a fixture is synthesized, TestGradleFixturesCaptured fails: every testdata/gradle file starts
                   "# captured: gradle <ver> ./gradlew testDebugUnitTest (equerry@<sha>)", and the red files
                   also name their break
       depends:  —

  t-2  Parse flutter/dart test, both reporters
       files:    internal/runsummary/dart.go, internal/runsummary/dart_test.go,
                 internal/runsummary/testdata/dart/compact_green.txt, internal/runsummary/testdata/dart/compact_red.txt,
                 internal/runsummary/testdata/dart/expanded_green.txt, internal/runsummary/testdata/dart/expanded_red.txt
       desc:     In feastahead/phone, capture `flutter test --reporter compact` and `--reporter expanded` through a
                 pipe, green and red, with the same 2 local test breaks for both red runs. Restore the tree. The
                 parser claims only on the terminal `+N …: All tests passed!` / `Some tests failed.` line. Counts
                 are the runner's own +passed / ~skipped / -failed. Each failing test's `[E]` header starts a block
                 with its own error output. A carriage-return redraw is already reduced by Segment.clean.
       covers:   c-2
       contract: if one reporter is mis-parsed, TestDartReportersAgree fails: compact_red and expanded_red give
                   identical failure names and identical counts; the green pair likewise
                 if counts are not the runner's own, TestDartGreenCounts fails: the counts carry the fixture's
                   final "+N" (and "~K" when present) verbatim
                 if a name is cut or prefixed with timer noise, TestDartRedNames fails: names == the two broken
                   tests' full names as the reporter printed them, with no "00:0x +n -m:" prefix
                 if error output bleeds, TestDartRedOwnLines fails: each block holds its own "Expected:/Actual:"
                   lines and not the other's
                 if a running counter forges a claim, TestDartTruncatedNotClaimed fails: either red fixture cut
                   before its terminal line is not claimed
                 if a fixture is synthesized, TestDartFixturesCaptured fails: header
                   "# captured: flutter <ver> flutter test --reporter <compact|expanded> (feastahead@<sha>)"
       depends:  —

  t-3  Parse pytest output, default and -q
       files:    internal/runsummary/pytest.go, internal/runsummary/pytest_test.go,
                 internal/runsummary/testdata/pytest/default_green.txt, internal/runsummary/testdata/pytest/default_red.txt,
                 internal/runsummary/testdata/pytest/quiet_green.txt, internal/runsummary/testdata/pytest/quiet_red.txt
       desc:     In sunday-rider, capture the repo's own test_command (selftest.sh → `pytest -q`, so ruff/mypy
                 output comes before it), plus the same compose service running `pytest` without -q. Capture each
                 green, and red with the same 2 local breaks. Restore the tree. The parser claims only on pytest's
                 terminal "N failed, M passed in Xs" line (with or without `===` padding). Counts are that phrase.
                 Failures are named by the node id from the short summary's `FAILED <nodeid>` lines, and each
                 carries its own section from the `FAILURES` part.
       covers:   c-3
       contract: if -q or default is mis-parsed, TestPytestFormatsAgree fails: default_red and quiet_red give
                   identical node ids and count phrases; the green pair likewise
                 if the node id is lost, TestPytestRedNodeIDs fails: names == the two broken tests as
                   "path/test_x.py::[Class::]test_y"
                 if sections are mismatched to ids, TestPytestRedOwnLines fails: each block holds its own "E   "
                   assertion line and not the other's
                 if counts are rewritten, TestPytestGreenCounts fails: Counts == ["<N> passed in <t>s"] as pytest
                   printed it, with `=` padding stripped
                 if wrapper noise forges a claim, TestPytestClaimsOnlyItsSummary fails: quiet_green with its final
                   pytest line removed (the ruff/mypy/`selftest passed` lines remain) is not claimed
                 if a fixture is synthesized, TestPytestFixturesCaptured fails: header
                   "# captured: pytest <ver> <command> (sunday-rider@<sha>)"
       depends:  —

  t-4  Count name and marker lines in the block bound
       files:    internal/runsummary/render.go, internal/runsummary/summary.go, internal/runsummary/summary_test.go
       desc:     Render prints a block's output lines only while "✗ name", those lines and the "… N more lines —
                 see <log>" marker together fit BlockBytes. The marker's length is known only at Render, because
                 it holds logPath. Lines that do not fit move into the marker's count. Update the BlockBytes doc
                 and TestBlockByteShare's fit arithmetic.
       covers:   c-4
       contract: if name/marker escape the bound, TestWorstCaseBlockBytes fails: 14 failures with names past
                   LineCut, 40 output lines of LineCut bytes each and a 200-byte logPath → every printed block
                   (from its ✗ line through its marker) is ≤ BlockBytes, len(out) ≤ 30,000, exactly 10 ✗ blocks
                 if the name is not counted, TestBlockNameCounts fails: the same failure prints strictly fewer output
                   lines under a 240-byte name than under a 10-byte name
                 if the marker is not counted, TestBlockMarkerCounts fails: output that overflows the block prints
                   fewer lines under a 200-byte logPath than under a 10-byte one
                 if the shared-error path escapes, TestSharedErrorBounded (jsrunner_test.go, existing) still
                   holds and the new per-block assertion covers vitest's shared-name copies through Render
       depends:  —

  t-5  Stop go blocks absorbing and phantom headings
       files:    internal/runsummary/gotest.go, internal/runsummary/gotest_test.go,
                 internal/runsummary/testdata/gotest/build_then_testmain.txt, internal/runsummary/testdata/gotest/hash_heading.txt,
                 internal/runsummary/testdata/gotest/build_failed_oneword.txt
       desc:     Capture from a scratch module with GOTOOLCHAIN=go1.27.1, so the existing header check holds. Three
                 runs: (a) a dep compile error followed by another package whose TestMain exits 1 after printing,
                 (b) a failing test that fmt.Println's "# Results", (c) a build error in a one-word module path.
                 An open `# pkg` block closes at the next package's first unindented non-error line. A one-word
                 `# X` opens a block only where a build block can appear. Raise TestGoFixturesCaptured's minimum
                 to 9.
       covers:   c-5
       contract: if a block absorbs orphans, TestGoBlockDoesNotAbsorbNextPackage fails: build_then_testmain.txt →
                   the "# <dep>" failure holds only its compile-error lines, and a failure named after the TestMain
                   package holds its TestMain output line
                 if a heading forges a build failure, TestGoHashHeadingNotABuildBlock fails: hash_heading.txt →
                   names == ["TestX"] and its block holds "# Results"
                 if the fix drops real one-word build blocks, TestGoOneWordBuildStillReported fails:
                   build_failed_oneword.txt → one failure carrying the "undefined:" line, and the package counted
                   as FAIL
                 if earlier behaviour regresses, TestGoBuildFailed / TestGoSetupAndVet (existing) fail
       depends:  —

  t-6  Confine vitest FAIL to banners; keep it.each names
       files:    internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go,
                 internal/runsummary/testdata/js/vitest_console_fail.txt, internal/runsummary/testdata/js/vitest_each_bracket.txt
       desc:     In feastahead/web, capture two runs from local uncommitted tests: one test console.logs a
                 "FAIL  phantom" line, and one it.each title contains " [ ". Restore the tree. A FAIL header
                 counts only after a "Failed Tests"/"Failed Suites" banner. Only a trailing " [ <file> ]" suite
                 suffix is stripped. TestJSFixturesCaptured accepts any recorded "vitest@<semver>" header, because
                 feastahead pins ^4.1.12 and the existing pin is 4.1.11.
       covers:   c-6
       contract: if console output forges a failure, TestVitestConsoleFailNotAFailure fails:
                   vitest_console_fail.txt → Failures.Total equals the fixture's own "Tests N failed" N, and no
                   failure is named "phantom"
                 if that forging reaches the summary, the same test's green half fails: the console-FAIL run at
                   exit 0 renders a PASS line
                 if the strip clips names, TestVitestEachBracketNameWhole fails: the failure name ends with the
                   it.each title's " [ … ]" text intact
                 if the suite strip is lost, TestVitestLoadError (existing) fails: "x.test.ts [ x.test.ts ]" is
                   still named "x.test.ts"
                 if the header relaxation admits hand-written files, TestJSFixturesCaptured fails on any vitest
                   fixture whose header lacks "vitest@<digits>.<digits>.<digits> "
       depends:  —

  t-7  Test stryker-net head reaches stderr with stream
       files:    internal/mutation/stryker_net_failure_test.go
       desc:     Add a test that sets SetStream(streamLog(t)) and runs noisyStrykerNet with the canary followed by
                 200 filler lines, exit 5. Confirm it by hand: delete `head` from stryker_net.go's MultiWriter,
                 watch the test go red, then restore.
       covers:   c-7
       contract: if head is dropped from the MultiWriter when a stream is set, TestStrykerNetHeadReachesStderrWithStream
                   fails: stderr no longer contains CANARY-N-2e5c
                 if the head stops being bounded, the same test fails: stderr carries more than strykerHeadLines
                   tool lines
                 if the stream loses output, the same test fails: the run log holds fewer than all 200 filler lines
       depends:  —

  t-8  Test production host-log fetch; empty log says so
       files:    internal/cmd/verify.go, internal/cmd/verify_log_test.go
       desc:     Tests swap runDetachArgv and leave detachFetchLog at its production value. collectHostLog prints
                 "log unavailable: the host log was empty" when the copy creates no log file (Path() == "").
       covers:   c-8
       contract: if the fetch source drifts, TestDetachFetchLogProduction fails: the argv handed to runDetachArgv is
                   rsync and its remote operand ends "<workdir>/.dross-runs/r-1/log"
                 if classifyFetch is bypassed, the same test fails: a stub exit status of 23 comes back as
                   errors.Is(err, remote.ErrPartial)
                 if an empty host log is silent, TestCollectEmptyHostLogSaysSo fails: a stub that writes a 0-byte
                   file leaves stdout containing neither "verify log (" nor "log unavailable"
                 if the success path regresses, TestCollectFetchesHostLog (existing) fails
       depends:  —

  t-9  Copy pending lines to the run log once
       files:    internal/runlog/runlog.go, internal/runlog/runlog_test.go, internal/cmd/test_output.go,
                 internal/cmd/test_output_test.go
       desc:     Add Log.Record(p): it writes to the file only, never echoes, and never falls back to the terminal.
                 testRun.spawn copies pending lines with Record on a terminal and off one. Those lines were already
                 printed once, by say.
       covers:   c-10
       contract: if Record echoes, TestRecordNeverReachesTerminal fails: on an Echo log, Record's bytes land in the
                   file and the terminal stays empty; on a failed log, the terminal gets only the one warning
                 if a terminal run drops its preamble, TestPendingLinesReachLogOnTerminal fails: tty=true, say
                   "PRE-MARK" then spawn → the log holds PRE-MARK before its first "$ " header, and stdout holds it
                   exactly once
                 if an unavailable log double-prints, TestPendingLinesPrintOnceWithoutLog fails: tty=false, the
                   logs dir is a file → stdout+stderr contain PRE-MARK exactly once
                 if a refusal now creates a log, TestRefusalWritesNoLog (existing) fails
       depends:  —

Wave 2 (depends t-1…t-9)
  t-10 Show the failing step's tail on 3/4/7
       files:    internal/cmd/test_output.go, internal/cmd/test.go, internal/cmd/test_output_test.go
       desc:     testRun records the spawn whose call returned an error (tr.failed(err) at each
                 spawnLocal/spawnRemote site). On 3/4/7, summarise shows that segment's tail. When no spawn failed
                 (a lock or argv step), it prints no other spawn's tail.
       covers:   c-9
       contract: if the tail still comes from the last spawn, TestFallbackTailFromFailingStep fails: two lanes on two
                   hosts, testHold returns ErrTransport for host-2 after host-1's rsync printed RSYNC-MARK → exit 3
                   and stdout has no RSYNC-MARK
                 if a failing spawn's own tail is lost, TestFallbackTailFromFailingSpawn fails: a prepare that prints
                   PREP-MARK and exits 1 → exit 7, and stdout's fallback holds PREP-MARK
                 if suite parsing is disturbed, TestTTYStreamsNonTTYSummarises (existing) fails
       depends:  t-9 (same files: test_output.go, test_output_test.go)

  t-11 Merge several runners' claims on one segment
       files:    internal/runsummary/summary.go, internal/runsummary/render.go, internal/runsummary/merge_test.go,
                 internal/runsummary/testdata/mixed/feastahead_green.txt, internal/runsummary/testdata/mixed/feastahead_web_red.txt
       desc:     Capture feastahead's bare `bash scripts/dross-test.sh` locally (flutter runs here only). Two runs:
                 green, and flutter green with vitest red. A flutter failure ends the script under `set -e`, so
                 no live bare run has both red. Segment.result returns every claiming parser's Result in registry
                 order. Render prints every runner's counts on one line and the union of failures under the one
                 Budget. The ambiguous-claim fallback goes away; TestAmbiguousClaimFallsBack becomes
                 TestMergedClaims.
       covers:   c-12
       contract: if the merge falls back, TestMixedGreen fails: feastahead_green.txt at exit 0 → one "PASS ·" line
                   carrying both "vitest: Test Files …" and "dart test: +N…"
                 if one runner's failures vanish, TestMixedRedBothRunners fails: one segment fed t-2's
                   compact_red.txt followed by t-6's vitest red capture → the ✗ names are the union of both
                   parsers' names, and the FAIL line carries both runners' counts
                 if the budget is per-runner, TestMixedSharesMaxBlocks fails: two fake parsers with 8 failures each
                   in one segment → exactly 10 ✗ blocks and "+6 more failing"
                 if the live mixed red mis-renders, TestMixedWebRed fails: feastahead_web_red.txt → dart counts and
                   the vitest ✗ names
                 if strict claims are broken now that nothing falls back, TestNoCrossClaims fails: every
                   single-runner fixture under testdata/{gotest,js,gradle,dart,pytest} is claimed by exactly one
                   registered parser, its own
       depends:  t-1, t-2, t-3, t-4, t-5, t-6

  t-12 Record before-numbers on the base binary
       files:    .dross/phases/test-summary-parsers/flow-cost-before.toml
       desc:     Build dross from base_commit ea72a2cb into a temp dir, put it first on PATH, and record `dross
                 version`. Replay the breaks named in t-1/t-2/t-3's red headers, and record `dross test >f 2>&1;
                 wc -c <f` as gradle_red_bytes (equerry), dart_red_bytes (feastahead, `--local phone/…`) and
                 pytest_red_bytes (sunday-rider). In equerry, the human runs one fresh session with the prompt
                 "dross test is red: name the failing tests and their assertions". Count agent_turns, turn_tools,
                 wall_clock_s, dross_calls and log_reads with jq over the transcript. Write [scenario] (including
                 jq_method) and [before]. Restore every repo's tree.
       covers:   c-11
       contract: if the half record is globbed, TestFlowCostRecords fails on every later commit: the
                   phases/*/flow-cost.toml glob does not match flow-cost-before.toml
                 if before ran on the phase binary, t-14's checkLegPair fails: before.binary_version is the
                   ea72a2cb build and must differ from after's
                 if a field is missing, t-14's merge fails TestFlowCostRecords: [before] needs agent_turns > 0,
                   wall_clock_s > 0, session_id, len(turn_tools) == agent_turns, non-empty dross_calls, and every
                   *_red_bytes > 0
       depends:  t-1, t-2, t-3

Wave 3 (depends t-11, t-12)
  t-13 Name the new runners in the README
       files:    README.md, internal/cmd/readme_doc_test.go
       desc:     Change the `dross test` row from "plain `go test`, vitest and jest are" to list gradle,
                 flutter/dart test and pytest. Also say that a segment with several runners prints each runner's
                 counts and shares the ten-block budget.
       covers:   c-1, c-2, c-3, c-12
       contract: if the README lags the registry, TestReadmeNamesSummaryRunners fails: the `dross test` row does not
                   name each of go test, vitest, jest, gradle, dart test and pytest
                 if the merge goes undocumented, the same test fails: the row has no mention of several runners in
                   one run
       depends:  t-11

  t-14 Record after-numbers and gate them
       files:    .dross/phases/test-summary-parsers/flow-cost.toml, .dross/phases/test-summary-parsers/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       desc:     Run `make install` (r-01) and confirm `dross version` matches HEAD. Replay t-12's scenario
                 exactly. Move [before] next to [after] in flow-cost.toml and delete flow-cost-before.toml. Add
                 gradle/dart/pytest_red_bytes to flowLeg, add checkTestSummaryParsersFlow, and add
                 "test-summary-parsers" to the found list. Record only observed numbers.
       covers:   c-11
       contract: if a round-trip was added, checkLegPair fails: after.dross_calls ⊄ before.dross_calls
                 if the red summary still needs the log, checkTestSummaryParsersFlow fails: after.log_reads == 0,
                   and it must be ≤ before.log_reads
                 if a parser's bytes went unmeasured, checkTestSummaryParsersFlow fails: any of
                   gradle/dart/pytest_red_bytes is ≤ 0 on either leg
                 if the after leg used the old binary, checkLegPair's binary_version check fails
                 if the record is missing, TestFlowCostRecords' found check fails for "test-summary-parsers"
                 if the half file lingers, checkTestSummaryParsersFlow fails: flow-cost-before.toml still exists
       depends:  t-1…t-12
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 gradle | t-1 (parser and fixtures), t-13 (README) |
| c-2 flutter/dart | t-2 (parser and both reporters), t-13 |
| c-3 pytest | t-3 (parser, default and -q), t-13 |
| c-4 30 KB worst case | t-4 |
| c-5 go block fixes | t-5 |
| c-6 vitest FAIL and it.each | t-6 |
| c-7 stryker-net SetStream head | t-7 |
| c-8 verify collect fetch and empty log | t-8 |
| c-9 3/4/7 tail from failing step | t-10 |
| c-10 run-log pending lines | t-9 |
| c-11 before/after numbers | t-12 (before), t-14 (after and gate) |
| c-12 mixed runner merge | t-11, t-13 |

All 12/12 criteria are covered. All four locked decisions are guarded by tests: gradle_green_counts by TestGradleGreenCounts and TestGradleReadsNoFiles; mixed_runner_segments by TestMixed* and TestNoCrossClaims; reporter_formats by TestDartReportersAgree and TestPytestFormatsAgree; fixture_provenance by the four *FixturesCaptured header tests.

## Judgment calls

- **Fixture capture lives in the parser task, not in a separate wave.** I rejected a capture-only wave 1. A capture with no test reading it has no contract of its own, and a separate wave only delays the parsers.
- **c-4's bound is enforced in Render, not in Failure.Line.** I rejected parse-time accounting. The marker holds logPath, which is known only at Render, and vitest's shared-name copies change the name after the lines are counted. Render is the only place where the exact bytes exist.
- **Parsers do not depend on t-4.** With Render-time accounting the Failure API is unchanged, so t-1…t-6 run in parallel in wave 1. The judge should serialise them only if another draft moves the bound into Failure.
- **c-12's both-runners-red contract feeds two live captures into one segment.** I rejected synthesizing a fixture, and I rejected waiting for a live bare run with both red. feastahead's dross-test.sh runs flutter first under `set -e`, so a red flutter lane stops the script before vitest. That makes a live both-red bare run impossible without editing feastahead, which is out of bounds. The live mixed fixtures cover green, and flutter green with vitest red.
- **TestAmbiguousClaimFallsBack is replaced by TestNoCrossClaims.** I rejected keeping any ambiguity fallback. The locked decision says to merge. With no fallback left, strict claims are the only guard against console noise forging a runner, so a matrix over every fixture becomes the gate.
- **The merged runner order is registry order.** I rejected "order of each runner's terminal line in the output", which needs line positions from every parser. For feastahead, registry order gives vitest before dart test, which is also the order in the output.
- **The flutter/dart runner label is "dart test".** I rejected picking between "flutter test" and "dart test" per run. Both run package:test's reporter, and the output has no reliable marker for which tool printed it.
- **The vitest header check is relaxed to any recorded vitest@semver.** I rejected keeping the 4.1.11 pin. feastahead pins ^4.1.12, and fixture_provenance requires its live output.
- **The new gotest fixtures are captured with GOTOOLCHAIN=go1.27.1.** I rejected relaxing TestGoFixturesCaptured. Keeping one toolchain across all gotest fixtures keeps the existing exact-header guard meaningful. c-5's fixtures come from a scratch module, because fixture_provenance names repos only for gradle, flutter, vitest and pytest.
- **t-5 adds a one-word-module build fixture that no criterion names.** The obvious fix for phantom `# Heading` blocks ("require a / or .") would silently drop a real build failure in a one-word module. The fixture is what makes that regression visible.
- **The gradle compile-failure fixture is in t-1's scope.** c-1 only speaks of failing tests. But a compile break is the most common gradle red, and the contract only asks that it never prints PASS. That costs one capture and no extra parser branch.
- **c-9 and c-10 are split into t-10 (wave 2) and t-9 (wave 1).** I rejected one combined task. Their contracts are disjoint, and c-10 needs a runlog API change while c-9 needs test.go call sites. The shared test_output files force the ordering.
- **c-7 stays its own small task (one test file).** It falls under the "too small" rule, but merging it into t-8 would join two packages that have nothing in common. The hand check (delete `head`, watch the test go red) is the real work.
- **c-11's after gate is log_reads == 0 plus the dross_calls subset, not agent_turns ≤ before.** Turn counts from a live session vary from run to run. log_reads is the direct measure of whether the red summary was enough, and the dross_calls subset is the gate the earlier phases already use for "zero added round-trips".
- **c-11's before leg builds from base_commit in a temp dir.** I rejected "run it first, before any make install". The build makes the leg independent of wave order, so it can wait in wave 2 for the breaks t-1/t-2/t-3 define. The red bytes before and after then measure the same failing output as the fixtures.
