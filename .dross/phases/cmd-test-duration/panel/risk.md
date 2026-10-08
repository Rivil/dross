# cmd-test-duration: risk-lens draft

Lens: start from what can break, then give every failure mode one owning task and one test.
In this phase the failure modes that matter most are not "the suite stays slow". They are
the ways a speed or CI change can make CI **lie**: a secret gate that quietly stops
matching, a pipe that turns a red `go test` green, a summary that goes missing on the
exact run you need it for (a timeout), and a concurrency group that cancels a
main run. main auto-releases, so a cancelled main run is a release without a verdict.

```
Phase cmd-test-duration: 6 tasks across 2 waves

Wave 1
  t-1  Prefilter secretscan rules with literal needles
       files:    internal/secretscan/rules.go, internal/secretscan/secretscan.go,
                 internal/secretscan/prefilter_test.go
       desc:     Each Rule gets an unexported needle set (any-of byte literals) and an
                 ASCII-fold flag for the two (?i) rules. scanLine runs a rule's regex only
                 when candidates(line) sets that rule's bit. It fails open: a rule with no
                 needles, or a fold rule on a line containing U+017F or U+212A, always runs
                 its regex.
       covers:   c-1, c-5
       contract: - Seeds for FuzzPrefilterMatchesRegex: every hitCorpus line, all-caps and
                   mixed-case keyword variants (an all-caps AUTHORIZATION reaches the A and
                   Z fold bounds), keywords spelled with U+017F (long s) or U+212A (Kelvin
                   sign) and built at runtime, multi-rule lines, CRLF lines, and
                   allow-marker lines. For each seed, prefiltered scanLine must equal the
                   reference loop that runs every regex. If the U+017F/U+212A fallback or
                   the A–Z fold is removed, a named seed fails.
                 - For a needle-free line (plain TOML, markdown or Go), candidates()
                   returns an empty mask. This kills the mutant that bypasses the
                   prefilter by returning an all-rules mask.
                 - A synthetic Rule with nil needles is always a candidate (fail-open).
                   Every rule in Rules() has at least one needle, so a new rule added
                   without needles fails by name.
                 - TestEveryRuleHasAHitCase, TestBenignCorpusZeroHits and
                   TestKeyContextIsKeyAware still pass unchanged. The new test file builds
                   every shape at runtime, so TestDrossTreeHasNoUnmarkedHits stays at zero
                   hits.
       depends:  —

  t-2  Prefilter techdebt marker scan with needles
       files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
       desc:     scanContent runs markerRe only on lines for which a pure
                 mayHaveMarker(line) finds TODO/FIXME/HACK/XXX. The long-line and
                 oversized-file heuristics are unchanged.
       covers:   c-1, c-5
       contract: - On a corpus, prefiltered scanContent must equal the reference
                   regex-on-every-line version. The corpus covers "TODOList" (needle
                   present but no \b match), "xTODO", "XXXX", lowercase "todo", a marker at
                   end of line, CRLF, a marker at char 500 of a long line, and binary
                   content.
                 - mayHaveMarker returns false for a needle-free line, which kills the
                   always-true mutant. It returns true for each of the four words.
       depends:  —

  t-3  Add go test -json reducer with failure echo
       files:    cmd/testsummary/main.go, cmd/testsummary/stream.go,
                 cmd/testsummary/stream_test.go, cmd/testsummary/fixture_test.go
       desc:     A stdlib-only package main reads a `go test -json` stream on stdin, line by
                 line. It prints a plain go-test log: ok/FAIL per package, the full output
                 of failed tests, the package-level output of failed packages (timeout
                 panics included), and build errors. Non-JSON lines pass through. The exit
                 code is non-zero on any fail, on an empty stream, or on a truncated
                 stream. main is `os.Exit(run(stdin, stdout, getenv))`. The fixture harness
                 writes a throwaway module from inline sources into t.TempDir() (never
                 tracked .go files) and runs the real `go test -json ./...` once
                 (sync.Once) with GOTOOLCHAIN=local, GOWORK=off and GOPROXY=off.
       covers:   c-3
       contract: - Real-toolchain fixture, one passing and one failing test: the failing
                   test's t.Error text and its "--- FAIL" line reach stdout, the passing
                   test's t.Log line does not, and the exit code is 1.
                 - Fixture test blocked past the fixture run's `-timeout 1s`: stdout
                   carries "panic: test timed out" and the running test's name, and the
                   exit code is 1. The shape is what go1.27's test2json actually emits, not
                   a hand-written guess.
                 - Fixture package that fails to compile: the compiler's file:line error
                   (build-output events) reaches stdout, and the exit code is 1.
                 - An all-pass stream exits 0. An empty stream exits non-zero and prints
                   "no test events". A stream cut mid-package (no terminal event) flushes
                   the buffered output and exits non-zero.
                 - A 1 MiB single output line and a non-JSON line pass through without the
                   reader aborting. A 64 KiB bufio.Scanner ceiling would fail this test.
       depends:  —

  t-6  Add PR-only cancel-in-progress concurrency group
       files:    .github/workflows/ci.yml, internal/cmd/ci_concurrency_test.go
       desc:     Add a top-level `concurrency:` block. The group is `ci-` plus (event is
                 pull_request ? github.ref : github.run_id), and cancel-in-progress is
                 `${{ github.event_name == 'pull_request' }}`. Before editing, audit ci.yml
                 against reference_ci_supply_chain_hardening.md and
                 reference_ci_pipeline_optimisation.md.
       covers:   c-4
       contract: - concurrencyProblems() driven by synthetic blocks:
                   · `cancel-in-progress: true` is flagged.
                   · A bare `${{ github.ref }}` group is flagged: main pushes would share
                     one group, and GitHub replaces a pending run in a shared group, so a
                     main run gets dropped even without cancel-in-progress.
                   · A group without the `ci-` prefix is flagged (the group namespace is
                     repo-wide and shared with release.yml).
                 - Sweep of the real ci.yml: exactly one top-level concurrency block, no
                   job-level concurrency overriding it, and no problems reported. Deleting
                   the block turns the sweep red.
                 - Verify evidence only (real CI): this phase's own ship pushes leave the
                   superseded PR runs `cancelled` and one run `completed` (gh run list).
       depends:  —

Wave 2 (t-4 depends t-3; t-5 depends t-3)
  t-4  Render timing table and 300s budget warning
       files:    cmd/testsummary/table.go, cmd/testsummary/table_test.go,
                 cmd/testsummary/main.go
       desc:     Built from the reduced events, the markdown summary has the top 20
                 slowest top-level tests (package, test, seconds, result) and per-package
                 totals. main appends it to $GITHUB_STEP_SUMMARY, or prints it to stdout
                 when that variable is unset (the laptop c-5 run). One `::warning` line is
                 printed per package whose elapsed is over 300s. Neither the table nor the
                 warning changes the exit code.
       covers:   c-3, c-2, c-5
       contract: - A 50-test stream renders exactly 20 test rows, in descending elapsed
                   order. A subtest (name containing '/') never takes a row. This kills
                   the cap and sort mutants.
                 - Package elapsed 300.0s produces no warning. At 300.1s there is exactly
                   one `::warning` naming the package and its seconds, and an all-pass
                   stream still exits 0. The warning cannot flake CI.
                 - The real timed-out fixture stream from t-3 still renders the table,
                   shows the package as FAIL with its elapsed, and names the test that was
                   running at timeout. This is the run where the table matters most.
                 - If $GITHUB_STEP_SUMMARY points at a directory, the write fails with a
                   ::warning and the exit code still equals the test verdict. If it is
                   unset, the table goes to stdout.
       depends:  t-3

  t-5  Pipe CI go test through summarizer; drop timeout
       files:    .github/workflows/ci.yml, internal/cmd/ci_go_test_step_test.go
       desc:     In both the `test` and `mutation-ts` jobs, a step runs
                 `go build -o "$RUNNER_TEMP/testsummary" ./cmd/testsummary`, then
                 `set -o pipefail; go test … -json … | "$RUNNER_TEMP/testsummary"`.
                 `-timeout 20m` and its "Headroom only" comment are removed. Before
                 editing, audit ci.yml against reference_ci_supply_chain_hardening.md.
       covers:   c-2, c-3
       contract: - goTestStepProblems() driven by synthetic YAML flags each of these by
                   name:
                   · a piped go test without `set -o pipefail` (or `shell: bash`)
                   · any `-timeout`
                   · a go test not using -json or not piped into the summarizer
                   · -short, -skip or -run in the `test` job
                   · loss of -race or -count=1 in the `test` job
                 - Sweep of the real ci.yml finds exactly 2 go test invocations (test and
                   mutation-ts), both clean. Dropping the mutation-ts pipe or re-adding
                   `-timeout 20m` turns it red. ci.yml no longer contains "Headroom only".
                 - No `tee` and no upload-artifact carries the -json stream (raw -json is
                   not retained).
                 - TestWorkflowsHaveNoExpressionsInRun still passes: paths go through
                   $RUNNER_TEMP and $GITHUB_STEP_SUMMARY, never `${{ }}` inside run:.
                   TestToolchainSingleSource, the action-pin tests and
                   TestCIShellcheckCoversScripts also still pass.
       depends:  t-3
```

## Risk ownership

Each failure mode has exactly one owning task and one test.

| Risk | What breaks | Owner | Pinned by |
|---|---|---|---|
| R1 | Prefilter false negative: a real credential skips the ship/validate/forge gate | t-1 | fuzz-seed equivalence against the all-regex reference |
| R2 | (?i) Unicode folding (U+017F→s, U+212A→k) escapes an ASCII-only prefilter | t-1 | fold-rune seeds; fallback-removal mutant fails |
| R3 | Prefilter silently bypassed (correct but slow again): an equivalent mutant survives gremlins | t-1 / t-2 | pure candidates()/mayHaveMarker() returns empty/false on needle-free lines |
| R4 | A global "regex ran" counter races under -race when forge/ship tests scan in parallel | t-1 | design: pure function, no package state (no counter exists to race) |
| R5 | techdebt marker prefilter drops a marker | t-2 | reference-equivalence corpus |
| R6 | Pipe exit status masks a red go test (default `bash -e` has no pipefail) | t-5 | goTestStepProblems pipefail case |
| R7 | Summarizer calls a failed stream green (defense in depth if pipefail is lost) | t-3 | fail/empty/truncated streams exit non-zero |
| R8 | Failures invisible in the log because stdout is JSON | t-3 | real-fixture failing test's text reaches stdout |
| R9 | 10m timeout or compile error undiagnosable | t-3 (log) / t-4 (table) | real timeout + build-fail fixtures |
| R10 | Summarizer aborts on a >64 KiB line or non-JSON line, SIGPIPEs go test, CI goes red for a tooling bug | t-3 | 1 MiB line + non-JSON passthrough |
| R11 | Budget warning changes the exit code (flake on a slow runner) | t-4 | 300.1s all-pass stream exits 0 |
| R12 | Summary write failure changes the verdict | t-4 | GITHUB_STEP_SUMMARY=directory case |
| R13 | A tracked broken .go fixture under cmd/ aborts the execconsent/subprocargs sweeps (they parse every non-test .go under cmd/ and t.Fatalf on a parse error) | t-3 | fixtures written to t.TempDir() from inline sources |
| R14 | Coverage quietly narrowed (-race/-count=1 lost, -short/-run added) | t-5 | goTestStepProblems flag cases |
| R15 | mutation-ts go test left unsummarised, so c-3's "every CI go test run" is only half met | t-5 | real sweep requires 2 piped invocations |
| R16 | A main push is cancelled or dropped as a replaced pending run | t-6 | bare-ref group and `true` cases flagged |
| R17 | Group name collides across workflows (namespace is repo-wide) | t-6 | missing-prefix case flagged |
| R18 | New test file carries a literal credential shape and reddens the self-scan | t-1 | TestDrossTreeHasNoUnmarkedHits stays zero-hit |

## Coverage

- **c-1** → t-1, t-2. These are the cuts backed by the static evidence. The reading instrument is t-3, t-4 and t-5.
  - Verify evidence (real CI only): on the ship run's step summary, internal/cmd's per-package row is ≤300s and no budget `::warning` fires.
  - Residual risk: see Judgment calls #11.
- **c-2** → t-5 (drops the timeout and the comment; pinned by the content test) and t-4 (the >300s warning that timeout_ceiling requires).
  - Verify evidence: the ship run's `-race` job is green with no `-timeout` flag.
- **c-3** → t-3 (reducer, failure echo, verdict), t-4 (table), t-5 (wired on both CI go test steps).
  - Verify evidence: both jobs' step summaries render on the ship run.
- **c-4** → t-6.
  - Verify evidence: gh run list for this phase's PR shows the superseded runs `cancelled` and the last one completed.
  - The "never cancel main" half is pinned only by the Go content test, because observing it would take two rapid main pushes.
- **c-5** → t-1 and t-2 (small savings without -race) and t-4 (the table renders on stdout locally).
  - Verify evidence: one `go test -count=1 -json ./internal/cmd | go run ./cmd/testsummary` run on the laptop with nothing else heavy running.
  - See the caveat in Investigation notes.

## Judgment calls

1. **Where to cut.** I chose a prefilter inside the production scanners. I rejected three alternatives: memoizing the tracked-tree scan across tests (it saves 1 of 4 scans), parallelizing the scans inside the tests (≤4x on 4 vCPU, still about 60s, plus goroutine complexity), and moving the self-scan tests out of internal/cmd (that games a per-package criterion). The prefilter removes about 240s at the source, keeps every self-scan reading the real tree under -race (coverage_preserved), and also speeds up real ship/validate runs.
2. **Prefilter failure direction.** I chose fail-open: no needles, or a line carrying a fold-special rune, runs the regex. The rejected alternative was a pure ASCII-fold needle check, which is a strict false-negative risk under Go's Unicode (?i) folding.
3. **Proving the prefilter prunes.** I chose a pure `candidates(line)` bitmask that tests assert on directly. I rejected a package-level regex-evaluation counter: forge and ship tests call ScanPayload concurrently, so the counter is itself a -race failure.
4. **Equivalence oracle.** I chose an in-test reference loop plus fuzz seeds, which run as plain unit tests. I rejected a differential run over the whole tracked tree, because running the unfiltered reference over 23.6MB brings back the 74s this phase removes.
5. **Separate techdebt task (t-2).** I kept it separate rather than folding it into t-1. It is about 28s under -race (replica), it is the second-largest real-repo scan, and its equivalence risk belongs to a different package with a different regex. It is small but not trivial.
6. **Summarizer location.** I chose `cmd/testsummary` (package main, stdlib only, not in goreleaser's `main: ./cmd/dross`). I rejected three alternatives:
   - An `internal/` package: the GITHUB_STEP_SUMMARY append would have to be declared in secretscan.Writers(), since TestEveryDrossWriterIsDeclared walks internal/.
   - Shell/jq: jq's behaviour isn't pinned by any Go test, and it adds a tool.
   - A hidden dross subcommand: it pollutes the shipped CLI surface.
7. **Fixture streams.** I chose the real `go test -json` output of the pinned toolchain on a throwaway module. I rejected hand-written JSON, because how timeouts and build failures are attributed in go1.27 test2json is exactly the part a hand-written stream would get wrong.
8. **Split of the summarizer into t-3 and t-4.** I split it rather than using one task, so that the false-green and failure-visibility risks (R7–R10) have an owner separate from presentation (R11–R12). t-4 strictly needs t-3's event model.
9. **CI wiring.** I chose a single step, `set -o pipefail` plus a pipe, with the summarizer's own non-zero verdict as a second gate. I rejected `go test > file` followed by a summarizer step with `if: !cancelled()`. That adds step-condition logic to get wrong, and the file is raw -json on disk in any case.
10. **Concurrency group.** I chose `github.run_id` for non-PR events. I rejected a shared `github.ref` group with `cancel-in-progress: false` for push. GitHub replaces a pending run in a shared group, so a main run can be dropped without cancel-in-progress ever firing. That violates the locked concurrency_scope decision.
11. **No speculative third cut.** I did not plan a shared AST-parse cache, t.Parallel, or macOS subprocess work. The projection after t-1 and t-2 is about 200–300s on CI, which is borderline on a slow runner. If the ship run's table shows internal/cmd over 300s, verify routes the table's top rows back through `dross task add`. I rejected planning a task against files the evidence doesn't name. Optional early read: after wave 2, open a draft PR for phase/cmd-test-duration so a real ubuntu-latest `-race` table exists before verify. `dross ship` adopts an already-open PR.
12. **Where the ci.yml tests live.** I put them in internal/cmd, next to TestToolchainSingleSource and TestWorkflowsHaveNoExpressionsInRun, reusing repoRootFromTest/readRepoFile. They read one small file, so their cost is negligible. I rejected a new light package, which would duplicate the helpers.
13. **Gating during execution.** internal/secretscan, internal/techdebt and cmd/testsummary are small, so they are safe to run locally. The internal/cmd tests (t-5, t-6) gate on `dross test` (remote), never on a local full run, per the laptop-starvation note. Rule r-01 applies: after t-1 and t-2, run `make install` before verify or ship, because the dross binary carries the scanners.

## Investigation notes

These are concrete findings. The mechanism is measured on a standalone replica, not the suite. Per-test confirmation still awaits c-3's first CI table.

- **The step change is PR #128 (secret-detection, a51ecdd), not d93b725.** internal/cmd `-race` package time on CI (from `gh run view --log`):
  - #127, 09-19: 200.8s (run 35455381954) and 279.1s (35455697471)
  - #128, 09-20: 447.8s (35512724506) and 589.4s (35512718209)
  - #129, 09-21: 441.6s, 587.7s (a real TestDoctorReportsMissingFlockAsHostTool failure), and a **600.0s timeout** (35606976673, running TestNoBodySinkReadsARecordedField)
  - #130, 09-22: 452.6s, 469.3s, 575.5s
  - #131, 09-23: 525.3s (16bfe62), 584.9s (883bdc7), a 600.0s timeout on d93b725 (35846448602, attempt 2, running TestTechdebtDrossTreeNoFixtureFindings), and 597.3s with `-timeout 20m` (3140364).
- **The d93b725 lead is not causal.** d93b725 changes one line of changes.json. The same Go code passed at 525.3s and 584.9s, and timeouts had already happened on 09-21. The test running at 600s is whichever test happened to be running then.
- **Mechanism.** internal/cmd/secretscan_selfscan_test.go (added in #128) runs the secret scanner over the real repo 4 times:
  - TestDrossTreeHasNoUnmarkedHits (:116) and TestSelfScanIsNotVacuous (:125) each call `secretscan.Scan` on every tracked file: 1883 files, 23.6MB.
  - TestValidateOnDrossItself (:236) clones the repo and runs `Validate()` twice. Each run reaches `scanDrossArtifacts` (internal/cmd/validate.go:152 → internal/cmd/secretscan.go:29) over about 1001 .dross files (14.9MB).
- **Replica timing.** I copied internal/secretscan/{secretscan,rules}.go into a scratch module (the suite was not run).
  - One full-tree Scan takes 3.4s plain and **73.8s under -race** (22x). The key-context rule alone is 32.8s. Every other regex rule costs about 5s under race, because each starts with `\b` or `(?i)` and so gets no literal-prefix acceleration.
  - 4 scans come to about 240s on this laptop, against the +247..310s step observed on CI.
- **Prefilter prototype** (same scratch module: needles, ASCII fold for the (?i) rules, regex fallback on U+017F/U+212A lines): 2.47s under -race. The regex ran on 4,352 of 4,234,617 line×rule pairs.
- **Secondary cost.** internal/cmd/techdebt_selfscan_test.go runs `techdebt.Scan` over the whole tracked tree 3 times (twice in :28, once in :75). The replica takes 9.5s per scan under -race, about 28s in total. This predates the 09-19 baseline (added 09-13) but grows with the tree.
- **Drift and variance.**
  - Tracked bytes went from 21.98MB (a51ecdd) to 23.61MB (HEAD), +7%, and scan cost is linear in bytes.
  - Identical Go code varies about ±25% between runners (447.8 vs 589.4; 525.3 vs 597.3). Most of the 448→597 drift is noise plus growth, not a second regression.
- **Ruled out or deprioritised.**
  - A full AST parse of 715 Go files takes 1.1s under -race, and about 20 test files do wide sweeps. That totals tens of seconds, not hundreds.
  - internal/cmd has zero `t.Parallel()`.
  - No `testing.Verbose()` or `test.v` reads exist in the repo, so `-json`'s implied `-v` changes no test behaviour.
  - Temp-repo clones in redproof, doctor and originpush are small.
- **ci.yml pre-audit** (reference_ci_supply_chain_hardening.md): no findings.
  - Actions are SHA-pinned, govulncheck is pinned at v1.8.0, and `permissions: contents: read` is set.
  - GOFLAGS/GOPROXY/GOSUMDB are set on both Go jobs.
  - npm uses `ci --ignore-scripts`, node is pinned to an exact version, and npm audit is a gate.
  - This plan adds no `uses:` and no `go install`. The summarizer is built from in-repo source.
  - In reference_ci_pipeline_optimisation.md terms, c-4 is step 0.3. The govulncheck-scoping and shellcheck-fold items are already deferred in spec.toml.
- **c-5 caveat.** Without -race, the same scans cost about 3.4s each on this laptop, so t-1 and t-2 remove only about 12–15s of laptop time. The laptop's ~600s has no static explanation (likely macOS subprocess cost and memory pressure). This plan does not claim to move it. t-4's local table is what would make a miss diagnosable.
