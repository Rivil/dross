# test-summary-parsers — panel synthesis

Facts checked against the repo before judging (read-only; nothing run in equerry / feastahead / sunday-rider):

- **Base commit.** `changes.json` records `base_commit = ea72a2cb`. 54a96123 (HEAD) adds only `.dross/phases/test-summary-parsers/{spec.toml,changes.json}`, so both commits build identical Go source. The two prior phases with a base build pinned it to their own `changes.json` base_commit: test-output-summaries used 54ad9064 and prompt-partials used `promptPartialsBase = "commit 3ee0fe71"`. So **ea72a2cb is the convention**. Risk's 54a96123 builds the same binary under a different `binary_version` string.
- **Half-record file.** `checkTestOutputSummariesFlow` and `checkPromptPartialsFlow` both assert that `flow-cost-before.toml` has been deleted. That file name is the established pattern, and the `phases/*/flow-cost.toml` glob does not match it. Risk's `flow/before.toml` works too, but it is a new convention.
- **Turn gating.** The `TestFlowCostRecords` doc comment says turns and wall-clock are "recorded but ungated" (rivil, 2026-10-08). That contradicts mvp's `after.agent_turns <= before.agent_turns` gate.
- **Pins.** `capturedHeader = "# captured: go1.27.1 go test -count=1 "` is an exact string. dross's own go.mod has `toolchain go1.27.3` and the system go is 1.26.5, so a scratch module can only produce 1.27.1 with `GOTOOLCHAIN=go1.27.1`. `vitestHeader = "# captured: vitest@4.1.11 "` is also exact, but feastahead's pnpm-lock resolves vitest **4.1.12**. Any new feastahead vitest fixture fails `TestJSFixturesCaptured` unless the pin changes. mvp never addresses this.
- **feastahead wrapper.** `scripts/dross-test.sh` runs `set -eo pipefail`, and the **phone (flutter) lane runs before the web (vitest) lane**. A red flutter lane exits before vitest starts, so a live both-red bare run is impossible. mvp's `feastahead_bare_red.txt` ("one break in each") cannot be captured. The phone lane is skipped on a host without flutter, with a `!! PHONE LANE SKIPPED` warning.
- **Parser registration.** Each parser file calls `Register` in `init()`, so there is no shared registry edit. Wave-1 parser tasks are file-disjoint. Registry order is file-name order, so a new `dart.go` registers before `jsrunner.go`. Verification's claim that registry order gives "vitest before dart test, which is also the order in the output" is wrong on both counts. Registry order is still a fine rule; the contracts just must not hardcode vitest-first.
- **Existing fallback.** `Render` with zero failures and exit ≠ 0 already prints the `no failing test reported` fallback. Risk's gradle compile-fail and pytest-green-under-mypy-red contracts pin that path through the new parsers; neither needs a new branch.
- **Test locations.** `TestTransportTailShown` and `TestExitCodesUnchanged` live in `test_summary_wiring_test.go`, which is risk's choice for c-9. Verification's choice, `test_output_test.go`, is wrong for c-9. The JS mirror test for c-7 (`TestStrykerFailureHeadStaysOnStderr`) and `TestStrykerNetStreamToSink` live in `stryker_stream_test.go`.
- **README.** README.md L232 says the summary handles "plain `go test`, vitest and jest". It goes stale the moment a new parser lands.

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | 12/12, every criterion has an owning task. No README, so the doc row goes stale. | 12/12 on paper, but the c-12 live fixture can't be captured (`set -e`, flutter first), and the new feastahead vitest fixtures break the 4.1.11 pin it never mentions. | 12/12 plus a README task, and each locked decision has a named guard test (`TestGradleReadsNoFiles`, the `*FixturesCaptured` headers). |
| Test-contract specificity | High, and grounded in real failure modes: `> Task … FAILED`, compile-fail, mypy-red wrapper, parametrized pytest, Result() idempotence. A few contracts lack a test name. | Medium. Every line is "if X, TestY fails", but it leaves out own-lines bleed, compile-fail and the pins. Its c-11 turn gate contradicts the recorded decision. | Highest: marker strings (RSYNC-MARK, PRE-MARK, CANARY), exact equalities, import guard, a one-word-module regression fixture. One factual slip on output order. |
| Granularity | 13 tasks, mostly right-sized. t-7 is one file (justified). t-12 is about 6 files in one layer. | Over-merged. t-4 is two parsers (c-5+c-6). t-6 is c-9+c-10, about 5 files across 2 layers, which the split rule covers. t-7 is a parser plus the c-12 live proof. t-5 (c-7+c-8) is a legitimate merge. | 14 tasks. t-7 is one file and t-13 is one doc plus a test, both small. The merge is consciously rejected for t-7. Otherwise well cut. |
| Wave correctness | Correct. t-11 is serialised behind t-9 only for the shared test_output.go. t-12 waits for all fixtures (the claim matrix needs them). The before-leg runs in wave 1 with its own patches (valid; it uses a non-convention base commit). | t-1 merges c-4 with c-12 in wave 1, which puts the merge engine ahead of the claim matrix that guards it. Both c-11 legs are in wave 3 (valid). | Correct. t-10 is serialised behind t-9 for the shared file. The before-leg waits on t-1..t-3's breaks (a real need). The after-leg depends on everything. |

**Skeleton: verification.** It has the sharpest contracts and the only README coverage. It follows the repo's own c-11 conventions (ea72a2cb base, `flow-cost-before.toml`, `log_reads == 0`, no turn gate). Its waves are right. Risk supplies most grafts: failure-mode contracts, saved break patches, capture hygiene, c-9's test file and the vitest version set. mvp supplies one graft: the c-7 + c-8 merge.

## Merged plan

**Capture rules for every task that captures fixtures (t-1, t-2, t-3, t-5, t-6, t-10).** Grafted from [risk] and [verification].

- Run the runner directly in the source repo, through a pipe, with stdout and stderr on one fd. Never run `dross test` from a git worktree.
- Make red runs from a local uncommitted break, then revert it. `git -C <repo> status --porcelain` must be empty before the task ends.
- The flutter lane is local only, because helicon has no flutter.
- Header format: `# captured: <runner>@<ver> <command> (<repo>@<sha>)`. Red files also name their break.
- Run `git add -N` on the new fixtures and the secret self-scan pre-gate (`TestDrossTreeHasNoUnmarkedHits`) before gating. The self-scan reads tracked files only, and live captures can carry token-shaped strings.

```
Phase test-summary-parsers — 13 tasks across 3 waves

Wave 1
  t-1  Parse gradle test output from equerry                        [verification+risk; mvp]
       files:    internal/runsummary/gradle.go, internal/runsummary/gradle_test.go,
                 internal/runsummary/testdata/gradle/green.txt, …/testdata/gradle/red.txt,
                 …/testdata/gradle/compile_failed.txt
       covers:   c-1
       desc:     Capture `./gradlew testDebugUnitTest` in equerry: green, red with 2 broken tests (one multi-line
                 assertion; a Kotlin backtick name with spaces if equerry has one), and red from a compile break.
                 Claims only on `BUILD SUCCESSFUL in` / `BUILD FAILED in`. Counts = outcome + `N actionable tasks`
                 (+ `N tests completed, M failed` when printed). Each `Class > method FAILED` starts a failure.
       contract: - if green counts drift from gradle_green_counts, TestGradleGreenCounts fails: green.txt → Counts ==
                   ["BUILD SUCCESSFUL", "<N> actionable tasks: …"] exactly, no phrase contains "test"
                 - if a name loses class or method, TestGradleRedNames fails: red.txt → names == the two broken tests
                   as Class.method, nothing else; `> Task :app:testDebugUnitTest FAILED` and the `* What went wrong:`
                   epilogue open no failure                                                       [+risk]
                 - if a method name with spaces is cut at the first word, TestGradleRedNames fails on the whole name [risk]
                 - if output bleeds between tests, TestGradleRedOwnLines fails: each block holds its own assertion line
                   and none of the other test's
                 - if gradle's red count is dropped, TestGradleRedCounts fails: Counts contain "<N> tests completed,
                   2 failed" verbatim
                 - if a compile break reads green or as `FAIL · 0 failing`, TestGradleCompileFailedNeverGreen fails:
                   compile_failed.txt at exit 1 prints the fallback tail containing `What went wrong`, no PASS line
                 - if a cut-off run is summarised, TestGradleTruncatedNotClaimed fails: red.txt cut before BUILD FAILED
                   is not claimed
                 - if JUnit XML reads are added, TestGradleReadsNoFiles fails: gradle.go imports none of os, io/fs,
                   path/filepath, encoding/xml
                 - if a fixture is synthesized, TestGradleFixturesCaptured fails: every file starts
                   "# captured: gradle@<ver> ./gradlew testDebugUnitTest (equerry@<sha>)", red files name their break
       depends:  —

  t-2  Parse flutter/dart test, both reporters                       [verification+risk; mvp]
       files:    internal/runsummary/dart.go, internal/runsummary/dart_test.go,
                 …/testdata/dart/compact_green.txt, compact_red.txt, expanded_green.txt, expanded_red.txt,
                 load_error.txt
       covers:   c-2
       desc:     In feastahead/phone, LOCALLY, capture `flutter test --reporter compact|expanded`, green and red on the
                 same 2 breaks, plus a load error (syntax error in a _test.dart). Claims only on `All tests passed!` /
                 `Some tests failed.` / `No tests ran.`. Counts = the runner's own +N ~S -F. Each `[E]` header starts
                 a block. One runner label for package:test output (see D3).
       contract: - if one reporter is mis-parsed, TestDartReportersAgree fails: compact_red and expanded_red give
                   identical failure names and counts; the green pair likewise (catches a CR-redraw mis-split)
                 - if counts are not the runner's own, TestDartGreenCounts fails: final "+N" (and "~K") verbatim
                 - if a name keeps timer noise, TestDartRedNames fails: names == the two broken tests, no
                   "00:0x +n -m:" prefix and no "[E]"
                 - if error output bleeds or carries the next progress line, TestDartRedOwnLines fails: each block
                   holds its own Expected:/Actual: lines, and the next progress line closes it          [+risk TestDartBlockEnds]
                 - load_error.txt: `loading test/x_test.dart [E]` is one failure named after the file  [risk]
                 - a `══╡ EXCEPTION CAUGHT` block longer than BlockLines is cut with More > 0, not dropped [risk]
                 - if a running counter forges a claim, TestDartTruncatedNotClaimed fails: either red fixture cut
                   before its terminal line is not claimed
                 - TestDartFixturesCaptured: "# captured: flutter@<ver> flutter test --reporter <r> (feastahead@<sha>)"
       depends:  —

  t-3  Parse pytest output, default and -q                            [verification+risk; mvp]
       files:    internal/runsummary/pytest.go, internal/runsummary/pytest_test.go,
                 …/testdata/pytest/default_green.txt, default_red.txt, quiet_green.txt, quiet_red.txt,
                 selftest_mypy_red.txt, collect_error.txt
       covers:   c-3
       desc:     In sunday-rider's test container, capture the repo's test_command (selftest.sh: ruff → mypy →
                 `pytest -q`) and the same service running bare `pytest`. Each is captured green and red on the same
                 2 breaks, one of them parametrized. Also capture a collection error, and selftest with pytest green
                 and a local mypy break. Claims only on pytest's terminal count line, with or without `===` padding.
                 Node IDs come from `FAILED <nodeid>`, and output comes from the matching `____ name ____` section.
       contract: - if -q or default is mis-parsed, TestPytestFormatsAgree fails: default_red and quiet_red give identical
                   node ids and count phrases; green pair likewise
                 - if the node id is lost, TestPytestRedNodeIDs fails: names == "path/test_x.py::[Class::]test_y"
                 - if sections are mismatched, TestPytestRedOwnLines fails: each block holds its own "E   " line; a
                   parametrized test_x[a-b] carries ITS section, not sibling test_x[a-c]'s          [+risk]
                 - if counts are rewritten, TestPytestGreenCounts fails: Counts == ["<N> passed in <t>s"], `=` stripped
                 - if wrapper noise forges a claim, TestPytestClaimsOnlyItsSummary fails: quiet_green minus its final
                   pytest line is not claimed; mypy's `Found 3 errors in 2 files` and ruff's `Found 2 errors.` fed
                   alone are not claimed                                                          [+risk]
                 - selftest_mypy_red.txt (pytest green, exit 1) renders the fallback, never PASS —
                   TestPytestGreenUnderRedWrapper                                                 [risk]
                 - collect_error.txt: `ERROR collecting tests/x.py` is one failure named by its path, and the counts
                   keep pytest's `error` phrase                                                   [risk]
                 - TestPytestFixturesCaptured: "# captured: pytest@<ver> <command> (sunday-rider@<sha>)"
       depends:  —

  t-4  Count name and marker lines in the block bound                 [verification+risk; mvp]
       files:    internal/runsummary/render.go, internal/runsummary/summary.go, internal/runsummary/summary_test.go
       covers:   c-4
       desc:     At Render, a block's printed bytes = "✗ name" + kept lines + the "… N more lines — see <log>" marker.
                 When that exceeds BlockBytes, trailing lines move into the marker's count. Applied per printed copy,
                 so vitest's re-stamped shared names (blockLines.addTo) are covered. Update the BlockBytes doc and
                 TestBlockByteShare's arithmetic.
       contract: - if name/marker escape the bound, TestWorstCaseBlockBytes fails: 14 failures with names past LineCut,
                   40 LineCut-byte lines each, a 200-byte logPath → every printed block (✗ through marker)
                   ≤ BlockBytes, len(out) ≤ 30,000, exactly 10 ✗ blocks (TestMassBreakSize extended the same way)
                 - if the name is not counted, TestBlockNameCounts fails: a 240-byte name prints strictly fewer lines
                   than a 10-byte name
                 - if the marker is not counted, TestBlockMarkerCounts fails: a 200-byte logPath prints fewer lines than
                   a 10-byte one
                 - if dropped lines vanish from the count, TestBlockBoundCountsDropped fails: marker N = original More +
                   lines dropped                                                                  [risk]
                 - if the bound trims what fits, a name + 3 lines block prints unchanged           [risk]
                 - a shared-header copy re-stamped with a 240-byte name stays ≤ BlockBytes in printed output;
                   TestSharedErrorBounded (existing) still holds
       depends:  —

  t-5  Stop go blocks absorbing orphans and phantom headings           [verification+risk; mvp]
       files:    internal/runsummary/gotest.go, internal/runsummary/gotest_test.go,
                 …/testdata/gotest/build_then_testmain.txt, hash_heading.txt, build_failed_oneword.txt
       covers:   c-5
       desc:     Capture from a scratch module with GOTOOLCHAIN=go1.27.1, keeping the exact header pin (see D7).
                 Capture three runs: (a) a build-failed pkg followed by a pkg whose TestMain prints and exits 1; (b) a
                 test that prints "# Results"; (c) a build error in a one-word module path. An open `# pkg` block
                 closes on the next package's first non-compiler line, and a one-word `# X` opens a block only where
                 a build block can appear. Raise TestGoFixturesCaptured's minimum to 9.
       contract: - if a block absorbs orphans, TestGoBlockDoesNotAbsorbNextPackage fails: the "# <dep>" failure holds only
                   its compile lines; the TestMain package's failure holds its TestMain output
                 - if a heading forges a build failure, TestGoHashHeadingNotABuildBlock fails: hash_heading.txt has no
                   `Results` failure (green variant renders PASS)                                 [+risk]
                 - if the fix drops real one-word build blocks, TestGoOneWordBuildStillReported fails: one failure with
                   the "undefined:" line, package counted FAIL
                 - TestGoBuildFailed / TestGoSetupAndVet (existing) keep exact names and counts; vet's second
                   `# [pkg]` header stays inside its block                                         [+risk]
                 - TestGoFixturesCaptured still rejects any header that isn't "# captured: go1.27.1 go test -count=1 "
       depends:  —

  t-6  Confine vitest FAIL to banners; keep it.each names              [verification+risk; mvp]
       files:    internal/runsummary/jsrunner.go, internal/runsummary/jsrunner_test.go,
                 …/testdata/js/vitest_console_fail.txt, …/testdata/js/vitest_each_bracket.txt
       covers:   c-6
       desc:     In feastahead/web, capture runs from local uncommitted tests: one test console.logs "FAIL  phantom",
                 and one it.each title contains " [ ". FAIL opens a failure only after a Failed Tests/Suites banner.
                 Strip only a trailing " [ <same path> ]" suite suffix. Change vitestHeader to a recorded version set
                 {4.1.11, 4.1.12} (see D7).
       contract: - if console output forges a failure, TestVitestConsoleFailNotAFailure fails: Failures.Total == the
                   fixture's own "Tests N failed" N, no failure named "phantom"; the exit-0 variant renders PASS
                 - if it.each names are clipped, TestVitestEachBracketNameWhole fails: "FAIL src/a.test.ts > adds
                   [ 1, 2 ]" keeps "[ 1, 2 ]"
                 - if the suite strip is lost, TestVitestLoadError (existing) fails: "x.test.ts [ x.test.ts ]" still
                   named "x.test.ts"; every existing vitest_* fixture keeps its names and counts        [+risk]
                 - if the pin is loosened to anything, TestJSFixturesCaptured fails on a vitest version outside the
                   recorded set                                                                   [risk]
       depends:  —

  t-7  Close verify-collect and stryker-net test gaps                  [mvp merge; contracts verification+risk]
       files:    internal/cmd/verify.go, internal/cmd/verify_log_test.go, internal/mutation/stryker_stream_test.go
       covers:   c-7, c-8
       desc:     Tests swap runDetachArgv and leave detachFetchLog at its production value. When the copy creates no
                 log file (Path()=="" or runlog Err), collectHostLog prints "log unavailable: the host log was empty".
                 Add the stryker-net SetStream test next to its JS mirror and red-prove it by hand: drop `head` from
                 stryker_net.go's MultiWriter, watch it fail, restore.
       contract: - if the fetch source drifts, TestDetachFetchLogProduction fails: the argv handed to runDetachArgv is
                   rsync with remote operand ending "<workdir>/.dross-runs/r-1/log"
                 - if classifyFetch is bypassed, the same test fails: stub exit 23 comes back errors.Is remote.ErrPartial
                   and reaches stdout as `log unavailable` naming a partial transfer             [+risk]
                 - if an empty host log is silent, TestCollectEmptyHostLogSaysSo fails: a 0-byte stub leaves stdout
                   with exactly one of {"verify log (", "log unavailable"}
                 - if the success path regresses, TestCollectFetchesHostLog (existing) fails; collect still writes the
                   tests/verify toml when the log is empty                                       [+risk]
                 - if head leaves the MultiWriter with a stream set, TestStrykerNetHeadReachesStderrWithStream fails:
                   stderr lacks CANARY-N-2e5c (observed red before green)
                 - same test: stderr carries > strykerHeadLines tool lines → fails; stream log holds fewer than all
                   filler lines → fails
       depends:  —

  t-8  Show the failing step's tail on 3/4/7                          [risk wave+file; verification contracts]
       files:    internal/cmd/test_output.go, internal/cmd/test.go, internal/cmd/test_summary_wiring_test.go
       covers:   c-9
       desc:     testRun records the segment whose spawn returned the error (tr.failed(err) at each
                 spawnLocal/spawnRemote site). On 3/4/7, summarise shows that segment. When the failing step spawned
                 nothing (host lock, argv validation), it shows no other spawn's tail and gives the reason instead.
       contract: - if the tail still comes from the last spawn, TestFallbackTailFromFailingStep fails: two hosts, host-1
                   rsync prints RSYNC-MARK, testHold returns ErrTransport for host-2 → exit 3, stdout has no RSYNC-MARK
                 - if a failing spawn's own tail is lost, TestFallbackTailFromFailingSpawn fails: lane 1 ran, lane 2's
                   prepare prints PREP-MARK and exits 1 → exit 7 fallback holds PREP-MARK, not lane 1's suite output
                 - TestTransportTailShown (existing) 3/4/7 cases still show the failing spawn's line and the log path
                 - TestExitCodesUnchanged and TestTTYStreamsNonTTYSummarises (existing) unchanged
       depends:  —

Wave 2
  t-9  Copy pending lines to the run log once                         [verification+risk]  (depends t-8)
       files:    internal/runlog/runlog.go, internal/runlog/runlog_test.go, internal/cmd/test_output.go,
                 internal/cmd/test_output_test.go
       covers:   c-10
       desc:     Add Log.Record(p): file-only, lazy-open like Write, never echoes, never falls back to the terminal.
                 testRun.spawn copies pending lines through Record on a terminal and off one. say already printed them.
       contract: - if Record echoes, TestRecordNeverReachesTerminal fails: on an Echo log Record's bytes land only in the
                   file; on a failed log (wrapFile seam) the terminal gets only the one warning
                 - if a terminal run drops its preamble, TestPendingLinesReachLogOnTerminal fails: tty=true, say
                   "PRE-MARK" then spawn → log holds PRE-MARK before its first "$ " header, stdout holds it exactly once
                 - if an unavailable log double-prints, TestPendingLinesPrintOnceWithoutLog fails: tty=false, logs dir
                   is a file → stdout+stderr contain PRE-MARK exactly once (today twice)
                 - TestRefusalWritesNoLog / TestLogPathBeforeSpawn (existing): a refused run still creates no log
       depends:  t-8 (same file: test_output.go — conflict serialisation, not data dependency; see D2)

  t-10 Merge several runners' claims on one segment                   [verification+risk; mvp contract]
       files:    internal/runsummary/summary.go, internal/runsummary/render.go, internal/runsummary/summary_test.go,
                 internal/runsummary/merge_test.go, …/testdata/mixed/feastahead_green.txt,
                 …/testdata/mixed/feastahead_web_red.txt
       covers:   c-12
       desc:     Capture feastahead's bare `bash scripts/dross-test.sh` locally: once green, and once with flutter
                 green and vitest red. Segment.result returns every claiming parser's Result in registry order. Render
                 puts each runner's counts on one line and spends one Budget on the union of failures. The ambiguous
                 fallback goes away. TestAmbiguousClaimFallsBack is rewritten. The both-red case comes from t-2's and
                 t-6's live captures fed into one segment (see D4).
       contract: - if the merge falls back, TestMixedGreen fails: feastahead_green.txt at exit 0 → one "PASS ·" line with
                   both the vitest and the dart counts (registry order, not hardcoded)
                 - if one runner's failures vanish, TestMixedRedBothRunners fails: one segment fed t-2's compact_red.txt
                   then t-6's vitest red capture → ✗ names are the union, FAIL line carries both runners' counts
                 - if the budget is per-runner, TestMixedSharesMaxBlocks fails: two fake parsers × 8 failures → exactly
                   10 ✗ blocks and "+6 more failing"                                              [+mvp]
                 - if the live mixed red mis-renders, TestMixedWebRed fails: feastahead_web_red.txt → dart counts and
                   the vitest ✗ names
                 - if strict claims break now that nothing falls back, TestNoCrossClaims fails: every fixture under
                   testdata/{gotest,js,gradle,dart,pytest} is claimed by exactly its own parser; only testdata/mixed
                   by two
                 - if merging double-closes a parser, the matrix fails: Result() called twice on every claiming parser
                   for every fixture returns equal Results                                        [risk]
                 - two claims + zero failures + exit 1 still prints the fallback; two claims + exit 0 → PASS with both
                   counts (rewritten TestAmbiguousClaimFallsBack)                                 [risk]
                 - the mixed header stays under LineCut with both runner names intact            [risk]
       depends:  t-1, t-2, t-3, t-4, t-5, t-6 (t-4 shares summary.go/render.go/summary_test.go; the matrix reads
                 every parser's fixtures)

  t-11 Record before-numbers on the base binary; save the breaks      [verification + risk patches]
       files:    .dross/phases/test-summary-parsers/flow-cost-before.toml,
                 .dross/phases/test-summary-parsers/flow/equerry-red.patch, …/flow/feastahead-red.patch,
                 …/flow/sunday-rider-red.patch
       covers:   c-11
       desc:     Build dross from base_commit ea72a2cb (git archive into scratch, not a worktree) and record
                 `dross version`. Re-create the breaks named in t-1/t-2/t-3's red headers and save each as a patch.
                 Record `dross test >f 2>&1; wc -c <f` as gradle/dart/pytest_red_bytes (dart via `--local phone/…`).
                 The human drives one fresh red-flow session on sunday-rider (see D5). Count agent_turns, turn_tools,
                 wall_clock_s, dross_calls and log_reads with jq. Write [scenario] and [before]. Revert every repo.
       contract: - if the half record is globbed, TestFlowCostRecords goes red on every later commit: the
                   phases/*/flow-cost.toml glob must not match flow-cost-before.toml
                 - if a patch can't replay, t-13 cannot reproduce the break: each patch passes `git apply --check` at its
                   repo's HEAD, and `git status --porcelain` is empty afterwards                   [risk]
                 - if before ran on the phase binary, t-13's check fails: before.binary_version contains
                   "commit ea72a2cb"
                 - if a field is missing, t-13's TestFlowCostRecords fails: agent_turns > 0, wall_clock_s > 0,
                   session_id, len(turn_tools) == agent_turns, non-empty dross_calls, every *_red_bytes > 0
       depends:  t-1, t-2, t-3 (the breaks their red fixtures name)

Wave 3
  t-12 Name the new runners in the README                             [verification]
       files:    README.md, internal/cmd/readme_doc_test.go
       covers:   c-1, c-2, c-3, c-12
       desc:     Rewrite the `dross test` row's "plain `go test`, vitest and jest are" to list gradle, flutter/dart test
                 and pytest, and say a segment with several runners prints each runner's counts under the shared
                 ten-block budget.
       contract: - if the README lags the registry, TestReadmeNamesSummaryRunners fails: the row does not name each of
                   go test, vitest, jest, gradle, dart test and pytest
                 - if the merge goes undocumented, the same test fails: no mention of several runners in one run
       depends:  t-10

  t-13 Record after-numbers and gate them                             [verification+risk]
       files:    .dross/phases/test-summary-parsers/flow-cost.toml,
                 .dross/phases/test-summary-parsers/flow-cost-before.toml (deleted),
                 internal/cmd/flowcost_record_test.go
       covers:   c-11
       desc:     `make install` (r-01), confirm `dross version` names the phase build. Re-apply t-11's patches verbatim,
                 replay its scenario, fold [before] next to [after] in flow-cost.toml, delete flow-cost-before.toml.
                 Add gradle/dart/pytest_red_bytes to flowLeg, a testSummaryParsersBase const, checkTestSummaryParsersFlow,
                 and "test-summary-parsers" to the required list. Record only observed numbers.
       contract: - if a round-trip was added, checkLegPair fails: after.dross_calls ⊄ before.dross_calls
                 - if the red summary still needs the log, checkTestSummaryParsersFlow fails: after.log_reads != 0
                 - if a parser went unmeasured, it fails: any gradle/dart/pytest_red_bytes ≤ 0 on either leg
                 - if an after-run was really a fallback, it fails: per-parser red_summarised = true on [after] [risk]
                 - if before drifted off base, it fails: before.binary_version lacks "commit ea72a2cb"; if after used
                   the old binary, checkLegPair's binary_version inequality fails
                 - if the half file lingers, it fails: flow-cost-before.toml still exists
                 - if the record is deleted, TestFlowCostRecords' found check fails for "test-summary-parsers"
       depends:  t-1 … t-11
```

Coverage: c-1 t-1, t-12. c-2 t-2, t-12. c-3 t-3, t-12. c-4 t-4. c-5 t-5. c-6 t-6. c-7 t-7. c-8 t-7. c-9 t-8. c-10 t-9. c-11 t-11, t-13. c-12 t-10, t-12. That is 12/12.

Same-file notes:
- No two tasks in the same wave edit the same file.
- t-4 and t-10 share summary.go, render.go and summary_test.go, and are serialised across waves 1 and 2.
- t-8 and t-9 share test_output.go, and are serialised across waves 1 and 2.
- t-12 and t-13 are both in wave 3 and both in `internal/cmd`, but they edit different files.

## Disagreements

**D1 — c-7 as its own task, or merged with c-8.**
- What each lens said:
  - mvp folds c-7 (one stryker test file) into the c-8 task.
  - Risk and verification both keep c-7 standalone. They admit it falls under the size threshold, but argue that merging joins two unrelated packages, and that the hand red-proof is the real work.
- Provisional default: merged (t-7). The one-file, under-10-minute rule is explicit. The red-proof is one edit-and-revert, and the merged task is only 3 files.
- Why it matters: a merged task means one commit and one task review cover two unrelated criteria. If the c-7 red-proof is skipped or fails, it blocks the c-8 fix from landing. Split it back out if per-criterion commit isolation matters more than task count.

**D2 — c-9 and c-10: one task or two, and in which order.**
- What each lens said:
  - mvp merges them. That task is about 5–6 files across runlog and cmd, which the split rule covers.
  - Risk puts c-9 in wave 1 and c-10 in wave 2.
  - Verification puts c-10 in wave 1 and c-9 in wave 2.
  - Neither of the split drafts needs the other's output. Both serialise only because both edit test_output.go.
- Provisional default: split, with c-9 first (risk's order). c-9's tests also belong in test_summary_wiring_test.go, as risk says, so the two tasks share only one file.
- Why it matters: the wave-2 slot is a conflict-avoidance serialisation, not a dependency. If an executor runs same-wave tasks sequentially anyway, both can sit in wave 1. Whichever goes second must rebase onto the first's testRun changes (spawn vs summarise).

**D3 — Where the c-12 merge and its claim-isolation guard live.**
- What each lens said:
  - mvp builds the merge engine in wave 1 together with c-4, tests it on fake parsers, and puts the live proof inside the dart parser task. Each parser task carries its own "doesn't claim others" check.
  - Risk and verification put the merge in wave 2, after every parser, with one claim matrix over all fixtures. Risk explicitly rejects per-parser checks: the danger only exists once the ambiguous fallback is removed, and one owner keeps the matrix complete.
  - The runner label also diverges. mvp's contract asserts a "flutter test:" label. Verification picks "dart test". Risk says "one name" without choosing one.
- Provisional default: wave 2, with the matrix in t-10. Registry order (file order: dart before vitest) sets the order of the counts, and the label is "dart test".
- Why it matters: if the merge ships before the matrix, there is a window where a parser that claims on foreign noise silently adds a phantom runner to a green summary instead of falling back. The label is user-visible, and t-2, t-10 and t-12 must all assert the same string.

**D4 — Where the both-runners-red evidence for c-12 comes from.**
- What each lens said:
  - mvp captures feastahead's bare run red with a break in each runner. That is impossible: the wrapper runs flutter first under `set -eo pipefail`.
  - Risk captures a live both-red run through a non-wrapper `sh -c 'cd phone && flutter test; cd ../web && pnpm test'`, and calls composition "synthesized".
  - Verification feeds t-2's and t-6's live red captures into one segment in the test.
- Provisional default: verification's composition. Each half is a live capture. Sequential single-spawn output is essentially that concatenation. Risk's command also skips `web_lane_prepare` (.env, test-db), so its vitest half may fail for environment reasons rather than the break.
- Why it matters: this decides how strictly fixture_provenance is read. If composing two live captures counts as "synthesized", the executor must use risk's command and also reproduce the web-lane preamble. Confirm before t-10 starts.

**D5 — The c-11 before leg: base commit, file, timing and flow repo.**
- What each lens said:
  - Base commit: risk uses 54a96123 (HEAD), verification uses ea72a2cb, mvp uses 54a96123.
  - File: risk writes `flow/before.toml`, verification writes `flow-cost-before.toml`, and mvp writes both legs at the end.
  - Timing: risk runs the before leg in wave 1 with its own saved patches. Verification runs it in wave 2, replaying the breaks named in the parser fixtures. mvp runs it in wave 3.
  - Flow repo: risk uses sunday-rider, verification uses equerry, and mvp says "one repo".
- Provisional default:
  - Base commit ea72a2cb. It is `changes.json`'s base_commit and matches the precedent of both prior pinned bases. 54a96123 differs only in `.dross/`, so this is about the recorded identity, not the code.
  - File `flow-cost-before.toml`, which two existing checks already expect to be deleted.
  - Wave 2, plus risk's saved patches.
  - sunday-rider, on risk's rationale that it needs only docker. Verification gave no reason for equerry.
- Why it matters: t-13's `contains "commit ea72a2cb"` check must match whatever t-11 recorded. Before and after legs measured on different breaks make the byte comparison meaningless, which is why the patches matter.

**D6 — The c-11 after-leg gate.**
- What each lens said:
  - mvp gates `after.agent_turns <= before.agent_turns`.
  - Verification gates `log_reads == 0` plus the dross_calls subset, and explicitly rejects a turn gate.
  - Risk adds `red_summarised = true` per parser.
- Provisional default: verification's gate plus risk's red_summarised. mvp's turn gate is dropped.
- Why it matters: the TestFlowCostRecords doc comment records the user's 2026-10-08 decision that turns and wall-clock are ungated, because batching moves them run to run. mvp's gate would make the record flaky, and contradicts that decision.

**D7 — Fixture version pins (go1.27.1, vitest@4.1.11).**
- What each lens said:
  - Go: risk widens `capturedHeader` to a recorded set {1.27.1, 1.27.3}. Verification keeps the exact pin and captures with `GOTOOLCHAIN=go1.27.1`.
  - vitest: risk uses a recorded set {4.1.11, feastahead's}. Verification accepts any `vitest@X.Y.Z`.
  - mvp addresses neither. Its feastahead fixtures would fail TestJSFixturesCaptured as written.
- Provisional default:
  - Go: `GOTOOLCHAIN=go1.27.1`. The scratch module has no toolchain directive, and the exact guard stays unchanged.
  - vitest: the recorded set {4.1.11, 4.1.12}. It is stricter than any-semver, and a new version has to be added on purpose.
- Why it matters: `GOTOOLCHAIN=go1.27.1` needs a toolchain download on the capture machine. If that is unavailable, fall back to risk's widened set. Any-semver would let a hand-written `vitest@0.0.0` header pass the provenance test.

**D8 — Red runs that fail without a failing test (gradle compile, pytest under red mypy).**
- What each lens said:
  - mvp explicitly rejects handling gradle's compile-fail BUILD FAILED, leaving it to the existing fallback.
  - Risk and verification capture a compile_failed fixture.
  - Risk alone adds sunday-rider's selftest with pytest green and mypy red, plus mypy/ruff "Found N errors" non-claim checks.
- Provisional default: include them all, as contracts only. Render already falls back on exit ≠ 0 with zero failures, so no new parser branch is needed. The fixtures pin that the new parsers reach that path, never PASS and never `FAIL · 0 failing`.
- Why it matters: these are the most common real reds in equerry and sunday-rider. Without the fixtures, a parser change that claims and reports zero failures could print a misleading summary unnoticed.
