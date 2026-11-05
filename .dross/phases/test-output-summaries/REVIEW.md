# Plan Review — test-output-summaries

Reviewed: 2026-10-09
Plan: 17 tasks across 5 waves

## BLOCKING
- [locked-decision conflicts] The plan contradicts `tool-output-not-persisted`'s locked `stderr_pointer` decision and does not handle it. That decision reads "No log file is written and no path is recorded", and its why says "Writing a log file would re-persist exactly what the milestone forbids". The live prose is at `internal/mutation/toolfail.go:118`: every adapter failure's persisted error ends with "the full output of <tool> went to this run's stderr, not to any file dross writes".
  - After t-5, t-6 and t-12, a non-TTY leg sends its full stream to `.dross/logs/verify-…log` only. A TTY leg sends it to the log plus stderr.
  - The sentence is therefore false in both modes. It still lands in tests.json (`LanguageRun.Error`), in verify.toml findings, and through `renderable_derivation` in PR bodies and board comments. It sends the reader to stderr and leaves out the one pointer that would help, the log path.
  - t-5's `TestGremlinsFailureHeadStaysOnStderr` locks in the unchanged error ("keeps its RecordToolFailure kind"). No task touches toolfail.go or the two tests that pin the phrase: `internal/verify/persist_toolfence_test.go:63` and `internal/boardsync/toolfence_composer_test.go:60`.
  - The plan does amend another shipped phase's locked decision (t-2, `terminal_definition`), so it treats cross-phase locks as binding. This one was missed. The panel never mentions `stderr_pointer` either.

  Suggestion: get a human call on the new pointer wording. Should it name the log, given that pruning to 20 makes a recorded path go stale? Then record the override, and amend `stderr_pointer` the same way t-2 amends `terminal_definition`. Put the toolfail.go prose change and its two pinning tests into the first task that moves the stream off stderr (t-5).

- [locked-decision conflicts / test-contract] t-3's `TestMassBreakSize` (500 failures × 100 lines of 2,000 chars → "≤ 320 lines and ≤ 30,000 bytes") can't pass under t-3's own description together with the locked `failure_bound`.
  - The locked decision prints the first 10 failures in full at up to 30 lines each, which is about 300 lines. t-3 cuts printed lines at 240 chars. That gives 300 × 241 ≈ 72,000 bytes, well over 30,000.
  - The risk lens meant 30,000 as a run-level byte cap ("a run's output stays at or under 30,000 bytes", risk.md:564), but t-3 never describes one. At 240-char lines, any such cap would cut output after about 4 blocks, which breaks the locked "first 10 failures in full".
  - The line bound is also tight. 10 × (30 + the `… N more lines` marker) = 310 lines, plus the `+490 more failing` line, the counts line and the log line, comes to about 314. If a block carries a name line outside its 30, the total exceeds 320.

  Suggestion: decide which number is wrong before execution. Either tighten the line cut (about 95 chars fits 30 KB) or drop or raise the byte bound. A byte cap that truncates the 10 blocks is not an option without reopening `failure_bound`. Also state whether a failure's name line counts toward its 30.

## FLAG
- [antipatterns/files] Several file lists leave out files that repo ratchets will force the executor to touch. Each one costs a red remote gate plus a plan-deviation ask.
  - t-2: `internal/runlog/runlog.go` calls `os.OpenFile` with `O_CREATE|O_EXCL` and writes `.gitignore`, so `TestEveryDrossWriterIsDeclared` fails until `internal/secretscan/writers.go` gets a MachineLocal entry. That entry needs a new IgnoreSeed constant. `TestMachineLocalWritersAreReallyIgnored` then fails with "no preparer for ignore seed" until `internal/cmd/secretscan_writer_enum_test.go`'s `seeds` map gets a preparer. `debugsession` is the exact precedent (`DebugIgnoreSeed`). Neither file is in t-2.
  - t-4: if `showmark.go` writes through anything other than store.go's `saveBytes`, it needs its own writers.go entry too, as `nudge.go` has. t-2 and t-4 are both wave 1, so they would both edit writers.go in parallel.
  - t-11: adding `--full` to `rule show` and `interaction show` changes `cmd/dross/testdata/cli_tree.txt` (lines 72 and 249; `TestCLITreeGolden`, package `./cmd/dross`), which t-11's files don't list.
  - t-7: the contract names `TestVerifySummaryPrintsPackagesAndSurvivors (internal/cmd)`, but t-7 lists no internal/cmd test file. `legsummary_test.go` is in internal/verify.

  Suggestion: add writers.go and the enum test to t-2, add cli_tree.txt to t-11, and add the cmd test file to t-7. Then either have t-4 write only through `saveBytes` (say so) or serialise t-4's writers.go edit after t-2.

- [test-contract] t-11's marker text can't meet its own bound. The verbatim line in the description, `rules unchanged — identical to the full block printed earlier in this session; still MUST-FOLLOW (dross rule show --full reprints)`, is 133 bytes with its newline (the em dash is 3 bytes). `TestRuleShowRepeatPrintsMarker` requires "under 120 bytes". The playbook line is 143 bytes.
  Suggestion: shorten the wording or raise the bound (t-16's `< 300` is unaffected), and keep the two consistent.

- [test-contract] t-14's `TestGreenRunPrintsSummaryOnly` "≤ 300 bytes" is set by the temp-dir length, not by summary size. D9 prints `log: <abs path>` twice. Under a macOS `t.TempDir()` (`/var/folders/…/T/TestGreenRunPrintsSummaryOnly<n>/001/.dross/logs/test-<ns-ts>.log`), each log line is about 150–170 bytes. The two log lines alone come to about 300–330 bytes, before the counts line. Linux `/tmp` lands at about 270. The test is green on helicon/CI and red locally for a reason unrelated to c-2. Its wording, "the log line" (singular), also contradicts D9's two prints.
  Suggestion: bound the bytes excluding the `log:` lines, or bound only the non-path content, and assert exactly two `log:` lines.

- [antipatterns/fixtures] Fixture provenance is pinned to versions this machine doesn't run, and the provenance tests only check a prefix.
  - t-8 says to capture with go1.26.1, but go.mod declares `toolchain go1.27.1` and local `go version` is go1.27.1.
  - t-9 says vitest 4.1.11 "pinned in internal/mutation/testdata/ts-project, from a scratch copy". The lockfile does pin 4.1.11, but the gitignored `node_modules/vitest` on disk is 3.2.7 (installed Aug 17). A scratch copy that brings node_modules along captures 3.2.7 output.
  - `TestGoFixturesCaptured` checks only `# captured: go1.`, and `TestJSFixturesCaptured` checks only `# captured: vitest@`/`jest@`. A mislabelled capture passes both.

  Suggestion: capture go fixtures with the repo's toolchain (go1.27.1), or say why 1.26.1. Require a fresh `npm ci --ignore-scripts` in the vitest scratch copy. Have the header tests assert the exact pinned versions.

- [test-contract] t-5 and t-6 change a spawn stream's sink from the concrete `os.Stderr` in `io.MultiWriter(os.Stderr, head)` (gremlins.go:222, stryker.go:155, stryker_net.go:155) to an `io.Writer` field set by `SetStream(w)`. In the taint engine, `precomputeTerminal` treats a field as terminal only when every store into it is terminal, and `isTerminal` returns false for an `*ssa.Parameter`. The setter's store therefore makes the field non-terminal. Its only production callers (t-12, t-17) land a wave later.
  - Whether the gremlins and stryker tees still pass `TestNoSpawnOutputEscapes` at t-5's and t-6's own gates is decided by engine paths no contract in those tasks exercises. They name only `TestToolRunnersCarryNoMarker`.
  - This is the "seam whose caller lands in a later task" shape that has already cost remote gates (exec-consent audit, 2026-10-05 and 2026-10-08).

  Suggestion: add `TestNoSpawnOutputEscapes` (and `TestLiveTreeHasNoUnresolvedDispatch`) to t-5's and t-6's contracts, so a red result surfaces in wave 1 rather than at t-12.

- [locked-decision conflicts] t-2 changes the meaning of exec-taint-enumeration's locked `terminal_definition` but edits only its `why`. The `choice` still reads "Terminal means the concrete os.Stdout / os.Stderr and cobra's OutOrStdout / ErrOrStderr", and after t-2 that is false.
  - The panel's D1 default was a single marker. It said the runlog terminal sink "needs a human call", and this spec.toml records no decision for that call.
  - t-2 also squashes a security-gate policy change in with a new package and a TTY seam.

  Suggestion: amend the `choice` text, not only the `why`. Record the D1 outcome as a decision in this phase's spec, so verify and later phases can see why a file sink became "terminal".

- [wave-order / antipatterns] The same helper gets written three times in one wave. t-10 needs a writer that opens the run log lazily, tees to the terminal on a TTY, and falls back to the terminal with one warning when the log can't open. t-12 needs one for verify and t-17 one for drain. All three are wave 2 in package cmd, in different files (test_output.go, verify.go, survivor_drain.go). Run in parallel, they either collide on a helper name or ship three copies.
  Suggestion: put the shared "terminal-or-log writer with fallback" in t-2 (runlog), or make t-17 depend on t-12 and reuse its helper. t-17 → wave 3 costs nothing, since t-16 is wave 5.

- [antipatterns/squashed] t-7 bundles the `adapterLanguage` fix ("stryker.net" → "stryker-net" → csharp). The panel synthesis (line 618) said it "predates the phase and should be filed". It isn't needed for c-5, because LegLines groups by directory. It also changes persisted output for C# repos: tests.json `LanguageRun.Name` and `OutOfScopeMutant.Language` go from "stryker-net" to "csharp".
  Suggestion: file it as a quick task, or keep it and say in the commit body that it changes a persisted field.

- [test-contract] t-1's field list leaves out `session_id`. Its third contract names agent_turns, turn_tools, dross_calls and *_bytes, and the description lists wall_clock_s, but `TestFlowCostRecords` also fails on an empty `session_id` for either leg. A missing value is only found at t-16, after the base-binary session, which a human drives.
  Suggestion: add `session_id` (and `wall_clock_s > 0`) to t-1's contract.

- [antipatterns/late premise] c-6 and c-7 rest on `CLAUDE_CODE_CHILD_SESSION` being unset in a main session. t-16 says "if it is set there, c-6 never fires". That premise is checked only in t-16 (wave 5), after t-4, t-11 and t-13 are built on it. In this subagent the variable is set, which matches the synthesis. The main-session side is unproven.
  Suggestion: move the one-line env check (main session vs subagent, names only) into t-1, which is already a live-session task in wave 1.

- [antipatterns/docs] The README sync is partial. t-15 updates the `dross test` row and the `dross rule` row. c-5 also changes `dross verify`'s output (per-package rows, survivor lines, a log path) and `dross survivor drain`'s (a log path), and adds `.dross/logs/`. The verify row (README:222), the verify results row (224) and the survivor drain row (253) are untouched.
  Suggestion: extend t-15's README edit to those rows.

## NOTE
- [coverage] Coverage is complete:
  - c-1: t-2, t-10
  - c-2: t-3, t-8, t-9, t-14
  - c-3: t-3, t-8, t-9, t-14, t-15
  - c-4: t-3, t-10, t-14
  - c-5: t-2, t-5, t-6, t-7, t-12, t-17
  - c-6 and c-7: t-4, t-11, t-13
  - c-8: t-1, t-15, t-16

  `parser_coverage`, `log_location`, `log_retention` and `tty_behaviour` are honoured: there are no extra parsers, the self-ignoring `.dross/logs/.gitignore` is written before the log, timestamped files are pruned to 20 by mtime, and `TestNoStreamingFlag` pins "no flag". r-01 is honoured, with `make install` in t-15 and t-16 and the before-leg on a temp build of the base commit. There is no global `~/.claude/dross/rules.toml`.
- [interpretation] D8 (lazy log) means refused runs (exit 6, exit 8, a pre-spawn refusal) write no log and print no path. c-1 says "every `dross test` run". The plan reads "run" as "something spawned". That's defensible, but /dross-verify may read c-1 literally, so the mapping should cite D8.
- [strength] The test contracts are unusually concrete. Nearly every one names a test, a fixture shape and the exact output that breaks. The plan also finds existing tests this change would break: the three lane-ordering tests ride t-10 by name for `TestNoTestLost`, `TestOutputIsNeverScraped` is kept, and the hermetic `TestMain` env hole is fixed in t-4. Few plans check that far.
- [strength] The flow-cost measurement is designed around the existing guard. The before-leg goes into a glob-safe `flow-cost-before.toml`, so `TestFlowCostRecords` never sees a half record. The leg is measured on a temp build of base commit 54ad9064 before any `make install`. t-16 extends `found` so a missing record fails the suite.
- [strength] The safety directions are right. A non-zero exit can never render green (t-3, t-14). The marker fails toward a full print on any doubt: unseen session, subagent, store error, EPIPE before the record. Store and log failures never change an exit code (t-11, t-12, t-17).

## Summary
The plan isn't ready to run. It ships a false persisted claim by contradicting `stderr_pointer` without handling it, and t-3's mass-break test can't pass without breaking `failure_bound`. Both need decisions before execution. The flags are concrete cleanups: missing ratchet files, two self-contradicting byte bounds, fixture provenance, a cross-wave taint risk and a triplicated helper.
