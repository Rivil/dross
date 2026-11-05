# Draft plan — MVP lens

```
Phase test-summary-parsers — 8 tasks across 3 waves

Wave 1
  t-1  Bound blocks by bytes, merge multi-claim segments
       files:    internal/runsummary/summary.go, internal/runsummary/render.go,
                 internal/runsummary/summary_test.go
       covers:   c-4, c-12
       desc:     Count the "✗ name" line and the "… N more lines" marker toward one block's
                 byte bound so 10 worst-case blocks fit ~30 KB. Replace the ambiguous-claim
                 fallback with a merge: every claiming parser's Result becomes one entry on
                 the counts line, failures unioned under the one Budget/MaxBlocks.
                 TestAmbiguousClaimFallsBack is rewritten into the merge test.
       contract: - if name/marker bytes escape the bound, TestMassBreakSize fails: 500 failures
                   with 240-byte names and 100 full 240-byte lines each render > 30,000 bytes
                 - if multi-claim still falls back, TestMultiClaimMerges fails: two fake
                   parsers claiming one segment print "FAIL … · a: …, b: …" with both
                   runners' counts, not "output claimed by more than one runner"
                 - if the merge bypasses the budget, TestMultiClaimMerges fails: two claimants
                   with 8 failures each print exactly 10 "✗ " blocks and "+6 more failing"

  t-2  Add gradle parser from equerry fixtures
       files:    internal/runsummary/gradle.go, internal/runsummary/gradle_test.go,
                 internal/runsummary/testdata/gradle/green.txt,
                 internal/runsummary/testdata/gradle/red.txt
       covers:   c-1
       desc:     Capture `./gradlew testDebugUnitTest` green + red (local uncommitted test
                 break, restored after) in /Users/rivil/Development/equerry, header
                 "# captured: gradle@<ver> <cmd>". Parser claims only on BUILD SUCCESSFUL /
                 BUILD FAILED; green counts = gradle's own outcome + "N actionable tasks" line
                 (no XML, no init script); red = each "Class > method FAILED" + its own lines;
                 gradle's "N tests completed, M failed" when printed.
       contract: - if green invents a test count, TestGradleGreenCounts fails: green.txt's Counts
                   are exactly BUILD SUCCESSFUL + "N actionable tasks" as printed
                 - if a failure loses its name or lines, TestGradleRed fails: red.txt yields the
                   broken test's Class.method with its assertion line, and the
                   "tests completed, failed" count
                 - if the parser claims early or foreign output, TestGradleClaims fails: red.txt
                   cut before BUILD FAILED is unclaimed, and no gotest/js fixture is claimed
                 - if a fixture is synthesized, TestGradleFixturesCaptured fails on a missing
                   "# captured: gradle@" header

  t-3  Add pytest parser from sunday-rider fixtures
       files:    internal/runsummary/pytest.go, internal/runsummary/pytest_test.go,
                 internal/runsummary/testdata/pytest/ (default_green.txt, default_red.txt,
                 q_green.txt, q_red.txt)
       covers:   c-3
       desc:     Capture default and -q runs, green + red, in /Users/rivil/Development/sunday-rider
                 (red from a local uncommitted break, restored after). Parser claims only on the
                 final "N passed/failed … in Xs" line (with or without the ==== rule); red maps
                 each FAILURES block to its node id.
       contract: - if one reporter format is dropped, TestPytestFormats fails: default_* and q_*
                   both claim, and green Counts equal pytest's own "N passed" phrase
                 - if node ids are lost, TestPytestRed fails: both red fixtures yield
                   "path/test_x.py::test_y" with the block's own assertion lines
                 - if the parser forges a claim, TestPytestClaims fails: red cut before the
                   summary line is unclaimed, and no gotest/js/gradle fixture is claimed
                 - TestPytestFixturesCaptured fails on a missing "# captured: pytest " header

  t-4  Fix go build-block and vitest FAIL phantoms
       files:    internal/runsummary/gotest.go, internal/runsummary/gotest_test.go,
                 internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go
       covers:   c-5, c-6
       desc:     goTest: close an open "# pkg" block when a following package's lines start,
                 and stop a one-word "# Heading" from opening a build block. vitest: open a
                 FAIL block only inside the Failed Tests/Suites banner; strip the " [ " suffix
                 only when it is the suite echo "x [ x ]", keeping it.each names whole.
       contract: - if the orphan is absorbed, TestGoBuildBlockClosesOnNextPackage fails: a
                   build-failed pkg followed by another pkg's TestMain exit line keeps that line
                   out of the build block and attributes it to the second package
                 - if headings open blocks, TestGoHeadingNotABuildBlock fails: a test printing
                   "# Results" in a green run yields 0 failures
                 - if console FAIL lines count, TestVitestConsoleFailNotAFailure fails: a
                   "FAIL something" stdout line before the banner adds no failure
                 - if it.each names are clipped, TestVitestEachNameWhole fails:
                   "FAIL src/a.test.ts > adds [ 1, 2 ]" keeps "[ 1, 2 ]"

  t-5  Close verify-collect and stryker-net test gaps
       files:    internal/cmd/verify.go, internal/cmd/verify_log_test.go,
                 internal/mutation/stryker_stream_test.go
       covers:   c-7, c-8
       desc:     Test production detachFetchLog (real remote.FetchArgs + classifyFetch, a fake
                 rsync on PATH) and make collectHostLog print the log path or "log unavailable"
                 for an empty host log. Add the stryker-net SetStream-set failure-head test.
       contract: - if detachFetchLog stops fetching <runDir>/log, TestDetachFetchLogProduction
                   fails: the fake rsync's argv ends in "host:<runDir>/log <local>", and its
                   exit 23 surfaces as remote.ErrPartial
                 - if an empty host log prints nothing, TestCollectEmptyHostLogSaysSo fails:
                   output contains a verify log path or "log unavailable"
                 - if head leaves stryker_net.go's MultiWriter with a stream set,
                   TestStrykerNetFailureHeadStaysOnStderr fails: stderr holds 1..strykerHeadLines
                   [Survived] lines

  t-6  Fix dross test fallback tail and pending lines
       files:    internal/cmd/test_output.go, internal/cmd/test.go,
                 internal/cmd/test_output_test.go
       covers:   c-9, c-10
       desc:     Mark non-spawn steps (host lock) so a 3/4/7 shows the failing step's output,
                 not the last spawn's tail. On a terminal copy pending lines into the log
                 without re-echo; off a terminal with the log unavailable print them once.
       contract: - if the tail comes from the last spawn, TestFallbackTailFromFailingStep fails:
                   host-1 rsync output then a host-2 lock exit 3 does not show host-1's rsync lines
                 - if pre-spawn lines miss the log on a TTY, TestTTYLogHasPendingLines fails: the
                   log file contains the "remote: …" line said before the first spawn
                 - if pending lines double, TestLogUnavailablePendingOnce fails: off a terminal
                   with the log failed, the pre-spawn line appears exactly once on stdout

Wave 2 (depends t-1)
  t-7  Add dart/flutter parser and feastahead mixed fixture
       files:    internal/runsummary/dart.go, internal/runsummary/dart_test.go,
                 internal/runsummary/testdata/dart/ (compact_green.txt, compact_red.txt,
                 expanded_green.txt, expanded_red.txt, feastahead_bare_red.txt)
       covers:   c-2, c-12
       desc:     Capture compact + expanded `flutter test` green + red and feastahead's bare
                 run red (vitest + flutter in one spawn, one break in each) in
                 /Users/rivil/Development/feastahead, restored after. Parser claims only on
                 "All tests passed!" / "Some tests failed."; red = each failing test's name +
                 its own output.
       contract: - if a reporter is dropped, TestDartFormats fails: all four fixtures claim and
                   green Counts are the runner's own "+N" count
                 - if failure attribution breaks, TestDartRed fails: both red fixtures yield the
                   broken test's name with its "Expected/Actual" lines
                 - if mixed runs fall back, TestFeastaheadBareRunMerged fails: Render on
                   feastahead_bare_red.txt shows "vitest:" and "flutter test:" counts and one
                   ✗ block from each runner
                 - if the parser forges a claim, TestDartClaims fails: no gotest/js/gradle/pytest
                   fixture is claimed by dart, and dart fixtures are not claimed by vitest
                 - TestDartFixturesCaptured fails on a missing "# captured: flutter@" header

Wave 3 (depends t-1..t-7)
  t-8  Record before/after flow cost
       files:    .dross/phases/test-summary-parsers/flow-cost.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-11
       desc:     Build dross at base 54a96123 (before) and phase head (after); per new parser
                 (gradle/pytest/flutter) record red_test_bytes from `dross test >f 2>&1; wc -c`
                 on a local uncommitted break, restored after; human drives one red→fix→green
                 flow in one repo per leg for agent_turns, wall_clock_s, dross_calls. Add a
                 test-summary-parsers block to TestFlowCostRecords.
       contract: - if the record is missing, TestFlowCostRecords' found check fails with no
                   test-summary-parsers flow-cost.toml
                 - if a parser is unmeasured, checkTestSummaryParsersFlow fails: before and after
                   each carry gradle/pytest/flutter red bytes > 0
                 - if a round-trip was added, checkTestSummaryParsersFlow fails:
                   after.agent_turns <= before.agent_turns and after.dross_calls ⊆ before.dross_calls
                 - if the after leg ran on the base binary, the existing binary_version
                   inequality check fails
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 gradle parser | t-2 |
| c-2 dart/flutter parser | t-7 |
| c-3 pytest parser | t-3 |
| c-4 30 KB block bound | t-1 |
| c-5 go test phantoms | t-4 |
| c-6 vitest phantoms | t-4 |
| c-7 stryker-net SetStream head | t-5 |
| c-8 verify collect fetch + empty log | t-5 |
| c-9 3/4/7 tail from failing step | t-6 |
| c-10 run-log pending lines | t-6 |
| c-11 before/after record | t-8 |
| c-12 mixed-runner merge | t-1 (engine, fake parsers), t-7 (live feastahead fixture) |

12/12 covered.

## Judgment calls

- Fixture capture folded into each parser task; rejected a separate wave-1 capture task, because the fixture is the parser's spec and splitting adds a hand-off and a wave with no criterion of its own.
- c-4 and c-12 merged into one core task (both change summary.go/render.go/summary_test.go); rejected separate tasks, which would collide on the same three files in wave 1.
- c-5 and c-6 merged into one task (two small parser fixes, single layer, 4 files); rejected one task each because each alone is a ~20-minute, single-file change.
- c-7 (test-only, ~10 min) rides with c-8; rejected a standalone stryker task as too small.
- c-9 and c-10 merged; both are testRun state in test_output.go and share one test file.
- The c-12 live proof sits with the dart parser (t-7, wave 2), not a separate mixed-fixture task; it is the only parser that needs t-1's merge to be tested at all, since gradle and pytest each claim alone.
- t-7 depends on t-1 only, not t-4: the flutter output in the bare run has no "FAIL " console lines, and jsrunner.go is not in t-7's files, so there is no conflict.
- No separate "before" capture task at wave 1: no prompts change in this phase, so r-01's install hazard does not apply and the base-commit binary can be rebuilt at any time; t-8 measures both legs.
- Red bytes per parser are recorded, not gated: a red summary with full blocks can exceed the 40-line fallback, and c-11 asks for numbers, not a shrink. The zero-round-trip claim is the only gate.
- Gradle's compile-error BUILD FAILED (no failing test) is left to the existing "no failing test reported" fallback; rejected adding a build-failure block, since no criterion asks for it.
