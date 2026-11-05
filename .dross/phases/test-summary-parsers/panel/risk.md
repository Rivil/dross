# test-summary-parsers — RISK lens draft

Lens: every task owns one family of failure modes and the test that pins it.
The risks below were found by reading the code and the fixture repos, not
assumed:

- **Merge turns a forged claim from safe into silent.** Today two claims →
  `ambiguous` → fallback (safe). After c-12's merge, a parser that claims on
  another runner's noise adds a phantom runner to a green summary. The
  claim-isolation matrix therefore ships *with* the merge (t-12), not before or
  after it.
- **feastahead's wrapper cannot produce "both red" live.**
  `scripts/dross-test.sh` runs `set -eo pipefail`, phone (flutter) lane first:
  a red flutter exits before vitest runs. The two-red fixture must come from a
  live un-`-e` shell line in feastahead, not the wrapper (t-12).
- **Phone lane is local-only.** helicon has no flutter; the wrapper *skips* it
  there and prints a `!! PHONE LANE SKIPPED` warning. Any flutter or mixed
  capture must run locally (`--local` / direct), or it silently captures
  vitest-only output (t-2, t-12).
- **Fixture header pins already disagree with the live repos.**
  `TestJSFixturesCaptured` pins `vitest@4.1.11`; feastahead's web has
  `vitest ^4.1.12`. `TestGoFixturesCaptured` pins `go1.27.1`; the local
  toolchain is `go1.27.3`. A new live fixture fails the pin unless the pin is
  widened to a recorded version set — never deleted (t-5, t-6).
- **sunday-rider's gate is not just pytest.** `selftest.sh` runs ruff → mypy →
  pytest and collects failures. Green pytest + red mypy exits non-zero; the
  pytest parser must not let that read PASS, and mypy's `Found N errors` /
  ruff's `Found N errors.` must not forge a pytest claim (t-3).
- **Gradle compile failure is BUILD FAILED with zero test failures.** It must
  reach the fallback (tail with `* What went wrong`), never `FAIL · 0 failing`
  (t-1).
- **`> Task :app:testDebugUnitTest FAILED` looks like a test FAILED line** (t-1).
- **The 30 KB bound is broken by what sits outside BlockBytes**: name (≤243
  bytes), the `… N more lines — see <log>` marker (≤241), and vitest's
  shared-header copies, which re-stamp `Name` *after* the lines were budgeted.
  Enforcing at render time is the only place every parser's output passes (t-4).
- **A `flow-cost.toml` with only `[before]` turns the suite red.**
  `TestFlowCostRecords` globs `phases/*/flow-cost.toml` and `checkLegPair`
  demands both legs. The before-leg must land somewhere the glob doesn't see
  until the after-leg folds it in (t-10 → t-13).
- **Before/after measured on different red breaks is not a comparison.** The
  breaks are saved as patches in the phase dir by t-10 and re-applied verbatim
  by t-13.
- **New fixtures are tracked files → secret self-scan reads them.** Captures
  from real repos may carry token-shaped strings or env dumps
  (`TestDrossTreeHasNoUnmarkedHits`). Each capture task `git add -N`s its
  fixtures and runs the self-scan before gating.
- **An empty host log is invisible today**: `runlog.Open` is lazy, so
  `io.Copy` of zero bytes never creates the file, `Path()` is `""`, and
  `collectHostLog` prints nothing at all (t-8).
- **A 3/4/7 from a non-spawn step blames the previous spawn**: host-2's lock
  failure in the multi-host loop (`test.go` ~L515) returns after host-1's rsync
  segment, and `summarise` shows `segs[len(segs)-1]` (t-9).
- **Pending lines double-print off a terminal when the log fails**: `say`
  already printed them, then `spawn` writes them through `r.log.Write`, whose
  fallback prints them again; on a terminal they are never copied in at all
  (t-11).

Fixture capture rules (apply to t-1, t-2, t-3, t-5, t-6, t-12): run the runner
directly in the source repo (never `dross test` from a git worktree), red via a
local uncommitted break, revert, then `git -C <repo> status --porcelain` must be
empty before the task ends. Header line `# captured: <runner>@<version>
<command>`. Never commit a fixture before the self-scan has read it.

```
Phase test-summary-parsers — 13 tasks across 3 waves

Wave 1
  t-1  Add gradle parser with live equerry fixtures
       files:    internal/runsummary/gradle.go,
                 internal/runsummary/gradle_test.go,
                 internal/runsummary/testdata/gradle/green.txt,
                 internal/runsummary/testdata/gradle/red.txt,
                 internal/runsummary/testdata/gradle/compile_failed.txt
       covers:   c-1
       desc:     Capture `./gradlew testDebugUnitTest` green, red (local test
                 break, incl. a Kotlin backtick name with spaces if equerry has
                 one) and compile-red (local `e:` break) from equerry. Parser
                 claims only on `BUILD SUCCESSFUL in` / `BUILD FAILED in`;
                 counts = gradle's own outcome + `N actionable tasks` (+ `N tests
                 completed, M failed` only when printed). Each `<Class> >
                 <method> FAILED` → Failure `Class.method` with its indented
                 lines.
       contract: - green.txt yields Counts with `BUILD SUCCESSFUL` and the
                   actionable-tasks line and NO test-count phrase — inventing a
                   count (locked gradle_green_counts) fails TestGradleGreenCounts
                 - red.txt: every `X > y FAILED` line in the fixture becomes
                   exactly one failure named `X.y`, carrying its indented
                   exception line(s); `> Task :app:testDebugUnitTest FAILED`
                   opens none (TestGradleRedNames counts them against a grep of
                   the fixture)
                 - a FAILED line whose method contains spaces yields the whole
                   name, not the first word
                 - compile_failed.txt is claimed with zero failures, and
                   Render at exit 1 prints the fallback tail containing `What
                   went wrong` — a `FAIL · 0 failing` header fails the test
                 - red.txt truncated before its `BUILD FAILED in` line is not
                   claimed
                 - TestGradleFixturesCaptured: every testdata/gradle file starts
                   `# captured: gradle@<ver> ./gradlew testDebugUnitTest`
       depends:  —

  t-2  Add dart/flutter parser, both reporters, live feastahead
       files:    internal/runsummary/darttest.go,
                 internal/runsummary/darttest_test.go,
                 internal/runsummary/testdata/dart/green_expanded.txt,
                 internal/runsummary/testdata/dart/red_expanded.txt,
                 internal/runsummary/testdata/dart/red_compact.txt
                 (+ testdata/dart/load_error.txt)
       covers:   c-2
       desc:     Capture `flutter test` in feastahead/phone LOCALLY (helicon has
                 no flutter) with `--reporter expanded` and `--reporter compact`
                 for the SAME local break, plus a load-error break (syntax error
                 in a _test.dart). Claims only on `All tests passed!` / `Some
                 tests failed.` / `No tests ran.`; counts = the final progress
                 token run (`+N ~S -F`) verbatim; each `... [E]` line opens a
                 failure named without timestamp/counters/`[E]`.
       contract: - red_expanded.txt and red_compact.txt (same break) yield the
                   identical set of failure names — a CR-redraw mis-split in the
                   compact reporter fails TestDartReportersAgree
                 - each failure's first kept line is its own error, and the next
                   progress line closes the block (a failure carrying the NEXT
                   test's progress line fails TestDartBlockEnds)
                 - load_error.txt: `loading test/x_test.dart [E]` is one failure
                   named after the file
                 - a FLUTTER `══╡ EXCEPTION CAUGHT` block longer than BlockLines
                   is cut with More > 0, not dropped
                 - red_expanded.txt truncated before `Some tests failed.` is not
                   claimed (a killed run is not summarised)
                 - TestDartFixturesCaptured: every testdata/dart file starts
                   `# captured: flutter@<ver> flutter test --reporter <r>`
       depends:  —

  t-3  Add pytest parser, default and -q, live sunday-rider
       files:    internal/runsummary/pytest.go,
                 internal/runsummary/pytest_test.go,
                 internal/runsummary/testdata/pytest/red_default.txt,
                 internal/runsummary/testdata/pytest/red_quiet.txt,
                 internal/runsummary/testdata/pytest/selftest_mypy_red.txt
                 (+ green_default.txt, green_quiet.txt, collect_error.txt)
       covers:   c-3
       desc:     Capture via sunday-rider's test container: `pytest` and
                 `pytest -q` on one local break (incl. a parametrized test),
                 a collection error, and the full `selftest.sh` with pytest
                 green + a local mypy break. Claims only on pytest's terminal
                 count line (`== N failed, M passed in Xs ==` or the bare -q
                 form, strict regex); node ids from `FAILED <nodeid> - `, output
                 from the matching `____ name ____` section.
       contract: - red_default.txt and red_quiet.txt yield the same node-id set
                   — a -q regression fails TestPytestFormatsAgree
                 - a parametrized `test_x[a-b]` failure carries ITS section's
                   output, not its sibling `test_x[a-c]`'s
                 - selftest_mypy_red.txt (pytest green, exit 1) renders the
                   fallback, never `PASS` — TestPytestGreenUnderRedWrapper
                 - mypy's `Found 3 errors in 2 files` and ruff's `Found 2
                   errors.` lines fed alone do not make the parser claim
                 - collect_error.txt: `ERROR collecting tests/x.py` counts as
                   one failure named by its path; counts keep pytest's `error`
                   phrase
                 - TestPytestFixturesCaptured: every file starts `# captured:
                   pytest@<ver> <command>`
       depends:  —

  t-4  Count name and marker inside the block bound
       files:    internal/runsummary/render.go,
                 internal/runsummary/summary.go,
                 internal/runsummary/summary_test.go
       covers:   c-4
       desc:     At render, a block's printed bytes = `✗ name` + kept lines +
                 the `… N more lines — see <log>` marker; when that exceeds
                 BlockBytes, trailing kept lines move into More. Applied per
                 printed copy, so vitest's re-stamped shared names are covered.
       contract: - TestMassBreakSize extended: 10 failures with 240-byte names,
                   240-byte lines, More > 0 and a 240-byte log path render under
                   30 KiB — reverting to lines-only accounting pushes it over
                 - a block cut by the new accounting reports `… N more lines`
                   where N = original More + lines dropped (an off-by-dropped
                   count fails TestBlockBoundCountsDropped)
                 - a shared-header failure (blockLines.addTo copy) renamed to a
                   240-byte name stays within BlockBytes in the printed output
                 - a short block (name + 3 lines) is printed unchanged — the
                   bound must not trim what fits
       depends:  —

  t-5  Scope go test # blocks to real build output
       files:    internal/runsummary/gotest.go,
                 internal/runsummary/gotest_test.go,
                 internal/runsummary/testdata/gotest/block_then_testmain.txt,
                 internal/runsummary/testdata/gotest/heading_print.txt
       covers:   c-5
       desc:     Capture live `go test -count=1 ./...` from a scratch module:
                 (a) a build-failed pkg followed by a pkg whose TestMain prints
                 and os.Exit(1)s; (b) a passing test that fmt.Println("#
                 Heading"). A `# pkg` block closes on the first line that is not
                 compiler/vet output; a `# word` block is only a failure once a
                 result line names that package as build/setup failed or FAIL —
                 otherwise its lines return to the orphan stream. Widen
                 capturedHeader to a recorded go-version set (1.27.1, 1.27.3).
       contract: - block_then_testmain.txt: the TestMain package's failure
                   carries TestMain's lines, and the `# pkg` block carries only
                   its compiler line — absorption fails TestBlockReleasesOrphans
                 - heading_print.txt: claimed, zero failures, Render prints PASS
                   — a phantom `Heading` failure fails TestHeadingIsNotABlock
                 - build_failed.txt and setup_and_vet.txt keep their exact
                   failure names and counts (vet's `# [pkg]` second header still
                   inside the block)
                 - TestGoFixturesCaptured accepts only listed go versions — a
                   fixture with an unlisted version still fails
       depends:  —

  t-6  Scope vitest FAIL to banners; keep it.each names
       files:    internal/runsummary/jsrunner.go,
                 internal/runsummary/jsrunner_test.go,
                 internal/runsummary/testdata/js/vitest_console_fail.txt,
                 internal/runsummary/testdata/js/vitest_each_bracket.txt
       covers:   c-6
       desc:     Capture from feastahead/web (local break): a test that
                 console.logs `FAIL something` and an it.each whose name
                 contains ` [ `. FAIL opens a failure only inside the Failed
                 Tests / Failed Suites section; the ` [ ` strip applies only to
                 a suite header whose suffix is ` [ <same path> ]`. Widen
                 vitestHeader to a recorded version set (4.1.11 + feastahead's).
       contract: - vitest_console_fail.txt: failure count equals the number of
                   FAIL headers after the banner — the console `FAIL` line
                   opening one fails TestVitestConsoleFailIgnored
                 - `FAIL  src/a.test.ts > adds [ 1, 2 ]` keeps the whole name;
                   `FAIL  src/x.test.ts [ src/x.test.ts ]` still prints
                   `src/x.test.ts`
                 - every existing vitest_* fixture yields the same names and
                   counts as before (load_error, unhandled, merged shared
                   headers)
                 - TestJSFixturesCaptured rejects a vitest version not in the
                   recorded set
       depends:  —

  t-7  Pin stryker-net head on stderr with stream
       files:    internal/mutation/stryker_stream_test.go
       covers:   c-7
       desc:     Add TestStrykerNetFailureHeadStaysOnStderr mirroring the JS
                 one: noisyStrykerNet* with a reportless exit 3, SetStream(log).
                 Red-prove by dropping `head` from stryker_net.go's
                 MultiWriter locally and observing the failure, then restore.
       contract: - with SetStream set and a reportless non-zero exit, stderr
                   holds 1..strykerHeadLines `[Survived]` lines; removing head
                   from `io.MultiWriter(out, head)` makes it 0 and fails the
                   test (observed red before green)
                 - the stream log holds all 300 lines while stderr holds only
                   the head — a head that leaks the full stream to stderr fails
       depends:  —

  t-8  Test production host-log fetch; never silent
       files:    internal/cmd/verify.go,
                 internal/cmd/verify_log_test.go
       covers:   c-8
       desc:     Test the real detachFetchLog through the runDetachArgv seam
                 (no host). In collectHostLog, a log that ends with no path
                 (zero bytes copied, or runlog Err) prints `log unavailable:
                 the host log was empty` (or the runlog error).
       contract: - production detachFetchLog asks the seam for an rsync argv
                   whose source is `<host>:<runDir>/log` — a FetchArgs path
                   change fails TestDetachFetchLogArgv
                 - a seam exit 23 reaches stdout as `log unavailable` naming a
                   partial transfer (classifyFetch wired) — bypassing
                   classifyFetch fails
                 - a fetch that writes an empty file prints exactly one of
                   {log path, `log unavailable`} — printing neither fails
                   TestCollectEmptyHostLogIsNamed
                 - the existing collect still writes tests/verify toml when the
                   log is empty
       depends:  —

  t-9  Blame the failing step for 3/4/7 tails
       files:    internal/cmd/test_output.go,
                 internal/cmd/test.go,
                 internal/cmd/test_summary_wiring_test.go
       covers:   c-9
       desc:     testRun records the segment whose spawn returned the error
                 (a `tr.failed(err)` after spawnLocal/spawnRemote); summarise on
                 3/4/7 shows that segment, or — when the failing step spawned
                 nothing (host lock, argv validation) — no tail and the reason.
       contract: - two-host run: host-1 lock + rsync ok, host-2 lock returns
                   ErrTransport → exit 3, and the output contains NO line from
                   host-1's rsync spawn — TestNonSpawnFailureNoStaleTail
                 - lane 1 suite ran, lane 2's prepare fails → exit 7 tail is the
                   prepare's line, not lane 1's suite output
                 - TestTransportTailShown's exit 3/4/7 cases still show the
                   failing spawn's line and the log path
                 - exit codes unchanged (TestExitCodesUnchanged)
       depends:  —

  t-10 Record before-leg on base binary; save red patches
       files:    .dross/phases/test-summary-parsers/flow/before.toml,
                 .dross/phases/test-summary-parsers/flow/equerry-red.patch,
                 .dross/phases/test-summary-parsers/flow/feastahead-red.patch,
                 .dross/phases/test-summary-parsers/flow/sunday-rider-red.patch
       covers:   c-11
       desc:     Build dross from base commit 54a96123 (git archive → scratch,
                 not a worktree) to a scratch path. In each repo apply a local
                 break, save it as a patch, run the base binary's `dross test`
                 off a terminal (`>f 2>&1; wc -c`), revert. Run the touched flow
                 (execute test→commit on sunday-rider's red) and count turns
                 with prior phases' jq_flow. Written to flow/before.toml, NOT
                 flow-cost.toml.
       contract: - TestFlowCostRecords stays green after this task: no
                   phases/test-summary-parsers/flow-cost.toml exists yet (a
                   before-only flow-cost.toml fails checkLegPair)
                 - each patch applies cleanly with `git apply --check` in its
                   repo at HEAD and the repo is clean afterwards — t-13 cannot
                   reproduce the break otherwise
                 - before.toml decodes into flowLeg with red bytes for gradle,
                   flutter and pytest all > 0 and binary_version naming
                   54a96123 (asserted by t-13's TestFlowCostRecords case)
       depends:  —

Wave 2
  t-11 Copy pre-spawn lines into log, print once      (depends t-9)
       files:    internal/runlog/runlog.go,
                 internal/runlog/runlog_test.go,
                 internal/cmd/test_output.go,
                 internal/cmd/test_output_test.go
       covers:   c-10
       desc:     Add runlog `Record(p)`: file-only write (lazy open like Write;
                 never echoes, never falls back to the terminal). spawn copies
                 pending through Record on and off a terminal.
       contract: - runlog: Record with Echo on writes nothing to the terminal;
                   Record with the file failing writes nothing to the terminal
                   (wrapFile seam) — TestRecordIsFileOnly
                 - tty run: a `say` before the first spawn appears in the log
                   file exactly once and on stdout exactly once —
                   TestTTYPendingLinesLogged
                 - non-tty run with the log failing: a pending line appears on
                   stdout exactly once (today twice) — TestPendingPrintsOnce
                 - TestLogPathBeforeSpawn / TestRefusalWritesNoLog unchanged: a
                   refused run still creates no log (Record is only reached via
                   spawn)
       depends:  t-9 (same file: test_output.go)

  t-12 Merge multi-runner claims; pin claim isolation
       files:    internal/runsummary/summary.go,
                 internal/runsummary/render.go,
                 internal/runsummary/summary_test.go,
                 internal/runsummary/claims_test.go,
                 internal/runsummary/testdata/mixed/feastahead_bare_green.txt,
                 internal/runsummary/testdata/mixed/both_red.txt
       covers:   c-12
       desc:     Segment.result returns every claiming parser's Result in
                 registry order; Render puts each runner's counts on the line
                 and spends one Budget across all their failures. Capture
                 locally in feastahead: the wrapper's bare run green, flutter
                 green + vitest red, and both red via `sh -c 'cd phone &&
                 flutter test; cd ../web && pnpm test'` (the wrapper's set -e
                 makes both-red impossible through it). Add a claim matrix over
                 every testdata/** fixture.
       contract: - both_red.txt: header names `dart`/`flutter` and `vitest`
                   counts, failures from BOTH runners appear, at most 10 `✗`
                   lines total and `+N more failing` = total − 10 —
                   TestMixedRunnersShareBudget
                 - TestClaimMatrix: every fixture under testdata/{gotest,js,
                   gradle,dart,pytest} is claimed by exactly its own parser;
                   only testdata/mixed is claimed by two — a parser claiming
                   on another runner's noise now adds a phantom runner and
                   fails here
                 - Result() called twice on every claiming parser for every
                   fixture returns equal Results (merge must not double-close)
                 - two claims, zero failures, exit 1 still prints the fallback
                   (TestAmbiguousClaimFallsBack rewritten: two claims + exit 0
                   → PASS with both counts)
                 - mixed header line stays under LineCut with both runner names
                   intact
       depends:  t-1, t-2, t-3, t-4, t-5, t-6

Wave 3
  t-13 Record after-leg; gate the flow-cost record
       files:    .dross/phases/test-summary-parsers/flow-cost.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-11
       desc:     `make install` (r-01), confirm `dross version` names the phase
                 head. Re-apply t-10's patches, measure per-parser red bytes and
                 the same flow; fold flow/before.toml + [after] + [scenario]
                 into flow-cost.toml. Add flowLeg fields red_bytes_gradle /
                 _dart / _pytest and a `test-summary-parsers` case.
       contract: - TestFlowCostRecords/test-summary-parsers: both legs carry all
                   three per-parser red byte counts > 0, and [after]'s
                   binary_version commit differs from [before]'s 54a96123 — a
                   stale binary after-leg fails
                 - checkLegPair: the after leg's dross_calls ⊆ the before
                   leg's — an added round-trip fails
                 - each after-leg red output was a summary (recorded
                   `red_summarised = true` per parser) — a fallback after-run
                   recorded as "after" fails
                 - "test-summary-parsers" is in the required-phases list, so
                   deleting the record fails
       depends:  t-1 … t-12
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 gradle | t-1 (parser + fixtures), t-12 (claim isolation) |
| c-2 dart/flutter | t-2 (parser + fixtures), t-12 (claim isolation) |
| c-3 pytest | t-3 (parser + fixtures), t-12 (claim isolation) |
| c-4 30 KB bound | t-4 |
| c-5 go test blocks | t-5 |
| c-6 vitest FAIL / it.each | t-6 |
| c-7 stryker-net head | t-7 |
| c-8 verify collect | t-8 |
| c-9 3/4/7 tail | t-9 |
| c-10 run log pending lines | t-11 |
| c-11 before/after numbers | t-10 (before + patches), t-13 (after + gate) |
| c-12 mixed-runner segments | t-12 |

12/12 covered.

## Judgment calls

- **Claim-isolation matrix lives in t-12 (merge), not in each parser task** — rejected per-parser "doesn't claim others" checks because the danger only exists once the merge removes the ambiguous-fallback; one owner keeps the matrix complete as fixtures are added.
- **c-4 enforced at render time, not in Failure.Line** — rejected the parser-side bound because vitest re-stamps Name after lines are budgeted and the marker's log path is unknown until render; render is the one choke point.
- **Both-red mixed fixture from a live non-`-e` shell line in feastahead, not the wrapper** — the wrapper's `set -eo pipefail` makes both-red impossible; a concatenation of two captures would be synthesized and break fixture_provenance.
- **c-5 fixtures captured live from a scratch Go module, not from equerry/feastahead/sunday-rider** — fixture_provenance names those three repos for gradle/flutter/vitest/pytest only; the existing gotest fixtures are scratch-module captures and stay consistent.
- **Header pins widened to a recorded version set, not dropped** — vitest 4.1.11 vs feastahead's ^4.1.12 and go1.27.1 vs local go1.27.3 would fail the pins; deleting them loses the provenance guard.
- **`# pkg` block confirmed by a later result line, plus closed on non-compiler lines** — rejected "require a `/` in the name" because single-segment module paths are legal and a test can print `# a/b` too.
- **Before-leg written to flow/before.toml, folded into flow-cost.toml only by t-13** — a before-only flow-cost.toml turns TestFlowCostRecords red for the whole phase.
- **Red breaks saved as patches by t-10 and replayed by t-13** — rejected re-improvising the break at the end; different breaks make the byte comparison meaningless.
- **Touched flow measured on sunday-rider (pytest)** — needs only docker, unlike equerry (Android SDK) or flutter; one flow suffices for "zero added round-trips", per-parser bytes cover all three.
- **t-11 sequenced after t-9** — both edit test_output.go; rejected same-wave parallelism because two writers on one file is a merge-conflict failure mode, not a real dependency saving.
- **t-7 kept as its own task despite being one test file** — merging it with t-8 would join two packages and two unrelated risks in one commit; it's also the c-7 red-proof site, which needs an observed red.
- **Runner name for c-2 is one name for the shared package:test reporter** — rejected sniffing flutter-only markers to choose `flutter test` vs `dart test`; the reporter format is identical and the sniff would be a new forgery surface.
- **Not owned: a wrapper's loud warning (`!! PHONE LANE SKIPPED`) is hidden by a green summary** — real, but outside every criterion; flag it for a deferred item rather than widen c-12.
- **`dart test` proven via flutter's reporter only** — feastahead has no pure-dart package; both use package:test's reporter, and inventing a dart repo would break fixture_provenance.
