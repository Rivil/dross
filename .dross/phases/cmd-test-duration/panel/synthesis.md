# cmd-test-duration: panel synthesis

Inputs: risk.md, mvp.md, verification.md, spec.toml (5 criteria, 5 locked decisions).
Claims checked against the repo during judging are marked **(checked)**.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 5/5. Each criterion has an owning task. c-2's non-failing warning has an owner (t-4). The mutation-ts pipe is required, so c-3's "every run" is met. c-5 carries an honest caveat. | 5/5. Reference-equivalence oracle plus fuzz seeds with named mutants. Real go1.27 test2json fixtures for timeout and build-fail. A 1 MiB line, a summary-write failure, and an R1–R18 ownership table. | 4/5. 6 tasks, one risk owner each. The summarizer is split into verdict and presentation. | 3/5. The `-timeout` drop (t-5) depends only on t-3. It lands after the cuts only because of wave numbering. No observed CI reading gates it; the early draft PR is "optional". |
| mvp | 3/5. All five criteria are named, but c-2, c-3 and c-4 share one task. The techdebt cut is missing even though mvp's own projection is 230–320s. With `$GITHUB_STEP_SUMMARY` unset its tool writes nothing, so its own c-5 laptop instruction produces no table. | 3/5. Sharp necessity property (regex ⇒ Admits) and ci.yml checker list. But verbatim echo turns the log into a full `-v` dump. No timeout, build-fail or truncated-stream contract. Exit depends on fail events alone. | 2/5. t-2 bundles a new binary, the workflow wiring, the timeout drop and concurrency across three criteria. | 3/5. The timeout drop correctly follows the cut (t-2 depends on t-1). But concurrency is needlessly serialized behind t-1, and no CI reading gates the drop. |
| verification | 5/5. All five criteria have a `[CI evidence]` line. The scan tests get per-test CI thresholds (≤15s, ≤5s). c-5 is flagged at risk. | 4/5. Named tests, boundary mutants, TestStdlibOnly and a non-vacuity fixture for the step scanner. But TestAnchorRejectsOrdinaryLine's "every benignCorpus() row" clause is unsatisfiable (Disagreement 6), and the timeout stream is hand-written. | 5/5. 6 tasks. The timeout drop is isolated in its own task. | 5/5. 3 waves. The drop waits for an observed CI reading (global rule 8). Concurrency is independent in wave 1. |

**Skeleton: verification.** Its waves are the only ones where an observed CI result authorizes dropping `-timeout 20m`, and that is what the user's global rule 8 requires. risk's contracts are grafted in almost wholesale. risk's summarizer split is adopted as well, because the human gate already sits between waves 2 and 3, so the split costs no wave.

## Merged plan

```
Phase cmd-test-duration: 7 tasks across 3 waves (+ one human gate between waves 2 and 3)

Wave 1
  t-1  Prefilter secretscan rules with literal needles                 [risk+verification+mvp]
       files:    internal/secretscan/rules.go, internal/secretscan/secretscan.go,
                 internal/secretscan/prefilter_test.go
       covers:   c-1, c-5
       desc:     Each Rule gets an unexported needle set: any-of byte literals checked with
                 bytes.Contains/Index. That code is assembly, so it avoids the ~20x -race tax
                 the regex VM pays. The two (?i) rules (authorization-header, key-context) also
                 get an ASCII-fold flag. scanLine runs a rule's regex only when a pure
                 `candidates(line)` bitmask sets that rule's bit.
                 Fail-open: a rule with no needles always runs its regex. So does a fold rule on
                 a line holding U+017F or U+212A. The trigger is those two exact byte sequences,
                 not "any non-ASCII", because em-dashes are common in .dross prose.
                 No package-level counter: forge and ship scan concurrently, so a counter would
                 itself fail under -race. The hit set is unchanged.
       contract: - Equivalence (FuzzPrefilterMatchesRegex; seeds run under plain go test). Seeds:
                   · every hitCorpus() line
                   · 500 randomLine() instances per rule
                   · every benignCorpus() row
                   · all-caps and mixed-case variants (`PASSWORD: …`, `AUTHORIZATION: BEARER …`),
                     which reach the A/Z fold bounds
                   · U+017F and U+212A spellings, built at runtime
                   · multi-rule lines, CRLF lines, allow-marker lines
                   For every seed, prefiltered scanLine == a reference loop that runs every
                   regex, and a regex match ⇒ the rule's candidate bit is set. These mutants
                   must each fail, naming the rule: dropping `ASIA` from aws-access-key;
                   dropping `token` from key-context; checking a (?i) rule against the
                   un-folded line; removing the U+017F/U+212A fallback.  [risk+verification+mvp]
                 - TestUnicodeFoldBypass: `paſſword = <hi-entropy>` and a Kelvin-sign
                   `toKen: <hi-entropy>`, both built at runtime, still yield a key-context
                   hit.  [verification]
                 - Pruning: candidates() returns an empty mask for needle-free lines
                   (`func main() { fmt.Println("hi") }`, plain TOML/markdown/Go). This kills
                   the all-rules mutant that silently restores full cost. It does NOT assert
                   this for benignCorpus rows (Disagreement 6).  [risk+verification]
                 - Fail-open: a synthetic Rule with nil needles is always a candidate.
                   TestEveryRuleHasNeedles fails, naming the rule, for any rule in Rules() with
                   an empty needle set.  [risk+mvp]
                 - These stay unedited and green: TestEveryRuleHasAHitCase,
                   TestBenignCorpusZeroHits, TestKeyContextIsKeyAware,
                   TestCarveOutsAreNotVacuous, TestLongLineIsScannedToTheEnd,
                   TestAllowMarkerSilencesOnlyItsLine, and internal/cmd's
                   TestDrossTreeHasNoUnmarkedHits, TestSelfScanIsNotVacuous and
                   TestValidateOnDrossItself. Same files, same assertions
                   (coverage_preserved).  [all three]
                 - prefilter_test.go builds every credential shape at runtime, so
                   TestDrossTreeHasNoUnmarkedHits stays at zero hits and pinnedMarkerSites
                   needs no edit.  [risk]
                 - [CI evidence] On the gate run, those three internal/cmd tests are each ≤15s
                   in the top 20, or absent. Local -race before the fix: ≈74s, 74s,
                   96s.  [verification]
       depends:  —

  t-2  Prefilter techdebt marker scan with needles                       [risk+verification]
       files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
       covers:   c-1, c-5
       desc:     scanContent runs markerRe only on lines where a pure mayHaveMarker(line) finds
                 TODO/FIXME/HACK/XXX (strings.Contains). The needle gates the \b regex and never
                 replaces it. The long-line and oversized-file heuristics are untouched.
       contract: - Reference equivalence: prefiltered scanContent == regex-on-every-line. The
                   corpus covers `TODOList` (needle present, no \b match), `xTODO`, `XXXX`,
                   lowercase `todo`, a marker at end of line, CRLF, a marker at char 500 of a
                   long line, binary content, `x := 1 // FIXME later`, `/* HACK */` and
                   `XXX:`.  [risk+verification]
                 - Dropping any of the four words from the needle list fails the equivalence
                   test and the existing TestScanMarkersMidLineAllKinds.  [verification]
                 - mayHaveMarker is false for `return nil` or any needle-free line (kills the
                   always-true mutant) and true for each of the four words.  [risk+verification]
                 - TestScanMarkerWordBoundary stays unchanged and green.  [verification]
                 - [CI evidence] TestTechdebtSelfScanExcludesOwnPackage and
                   TestTechdebtDrossTreeNoFixtureFindings are each ≤5s in the top 20.
                   Local -race before the fix: ≈8–9.5s per whole-tree pass, and the first
                   test does two passes.  [verification]
       depends:  —

  t-3  Add go test -json reducer with failure echo and verdict  [risk; contracts +verification+mvp]
       files:    cmd/testsummary/main.go, cmd/testsummary/stream.go,
                 cmd/testsummary/stream_test.go, cmd/testsummary/fixture_test.go
       covers:   c-3
       desc:     A stdlib-only package main. It is not built by goreleaser, whose only main is
                 ./cmd/dross (checked).
                 It reads a `go test -json` stream on stdin, line by line, with a reader that
                 tolerates long lines. It prints a plain go-test log:
                 · ok/FAIL per package
                 · the buffered output of failed tests
                 · the package-level output of failed packages (timeout panics included)
                 · build errors
                 Passing tests' output is dropped. Non-JSON lines pass through.
                 It exits non-zero on any fail event, on an empty stream, or on a stream with no
                 package terminal event. main is `os.Exit(run(stdin, stdout, getenv))`.
                 The fixture harness writes a throwaway module from INLINE sources into
                 t.TempDir(), never tracked .go files. It runs the real `go test -json ./...`
                 once (sync.Once) with GOTOOLCHAIN=local, GOWORK=off and GOPROXY=off.
                 (checked) subprocargs_audit_test.go:344-360 and execconsent_audit_test.go:153-168
                 walk cmd/ with testdata/ included, and t.Fatalf on a parse error. A tracked
                 broken .go fixture would therefore abort both sweeps.
       contract: - Real-toolchain fixture with one passing and one failing test: the `--- FAIL`
                   line and the t.Error text reach stdout; the passing test's t.Log and
                   `=== RUN` lines do not; exit 1.  [risk+verification]
                 - Real-toolchain fixture blocked past `-timeout 1s`: stdout carries
                   `panic: test timed out` and the running test's name; exit 1. The shape is
                   what go1.27.1's test2json actually emits, not a hand-written guess.  [risk]
                 - Real-toolchain fixture that fails to compile: the compiler's file:line
                   reaches stdout; exit 1.  [risk+verification]
                 - An all-pass stream exits 0. An empty stream exits non-zero and prints "no
                   test events". A stream cut mid-package flushes its buffered output and exits
                   non-zero.  [risk+verification]
                 - A 1 MiB single output line and a non-JSON line pass through without the
                   reader aborting. A 64 KiB bufio.Scanner ceiling fails this.  [risk+mvp]
                 - TestStdlibOnly: no import in cmd/testsummary's non-test files has a dot in
                   its first path element. Adding a third-party import fails it (locked
                   timing_tooling).  [verification]
       depends:  —

  t-4  Add PR-only cancel-in-progress concurrency group      [risk+verification; mvp inside its t-2]
       files:    .github/workflows/ci.yml, internal/cmd/ci_concurrency_test.go
       covers:   c-4
       desc:     Before editing, audit ci.yml against reference_ci_supply_chain_hardening.md
                 and reference_ci_pipeline_optimisation.md (global CI hygiene rule).
                 Add a top-level `concurrency:` block:
                   group: `ci-${{ github.event_name == 'pull_request' && github.ref || github.run_id }}`
                   cancel-in-progress: `${{ github.event_name == 'pull_request' }}`
                 Add a pure, line-based concurrencyProblems(workflow) checker plus a live sweep.
                 It reuses splitYAMLComment (action_pins_test.go:70) and readRepoFile
                 (release_signing_test.go:16) and follows the TestSetupGoStepProblems shape
                 (toolchain_source_test.go:202).
       contract: - One synthetic fixture per rejection branch:
                   · `cancel-in-progress: true` is flagged.
                   · A bare `${{ github.ref }}` group with no run_id arm is flagged. GitHub
                     replaces a pending run in a shared group even with cancel-in-progress
                     false.
                   · A group without the `ci-` prefix is flagged.
                   · Job-level-only concurrency is flagged.
                   · The correct block yields zero problems.  [risk+verification]
                 - Live sweep: ci.yml has exactly one top-level (indent 0) concurrency block,
                   no job-level override, and zero problems. Deleting the block fails the
                   sweep, naming ci.yml.  [risk+verification]
                 - TestWorkflowsHaveNoExpressionsInRun and TestWorkflowActionsArePinned stay
                   green: the expressions live in concurrency:, not in run:, and no `uses:` is
                   added.  [verification+mvp]
                 - [CI evidence] On this phase's ship,
                   `gh run list --branch phase/cmd-test-duration` shows every superseded
                   pull_request run `cancelled` and only the newest completed. After merge, no
                   main push run is `cancelled`.  [all three]
       depends:  —

Wave 2
  t-5  Render timing table and 300s budget warning      [risk; contracts +mvp+verification]
       files:    cmd/testsummary/table.go, cmd/testsummary/table_test.go, cmd/testsummary/main.go
       covers:   c-3, c-2, c-5
       desc:     Built from t-3's reduced events: a markdown table of the 20 slowest top-level
                 tests (package, test, seconds, result) plus per-package totals. It is appended
                 to $GITHUB_STEP_SUMMARY, or printed to stdout when that is unset (the c-5
                 laptop run). One `::warning` line is printed per package over 300s. Neither
                 the table nor the warning changes the exit code.
       contract: - A 25-test, 2-package stream yields exactly 20 rows in descending Elapsed
                   order, and the 21st-slowest is absent. A subtest (`TestA/x`) never takes a
                   row. This kills the cap, sort-direction and subtest
                   mutants.  [verification+mvp+risk]
                 - Each per-package total equals the package-level pass/fail event's Elapsed,
                   not the sum of its test rows.  [mvp+verification]
                 - Warning boundary:
                   · A package at 300.0s produces no warning.
                   · At 300.1s there is exactly one line starting `::warning`, naming the
                     package and its seconds.
                   · An all-pass stream with a 301s package exits 0.  [all three]
                 - The real timed-out fixture stream from t-3 still renders the table. It shows
                   the package as FAIL with its elapsed, names the test running at timeout, and
                   still lists the tests that passed before the panic.  [risk+verification]
                 - GITHUB_STEP_SUMMARY handling:
                   · Pointing at a file that already holds a line: that line is still first
                     afterwards (append, never truncate).
                   · Pointing at a directory: the write fails with a `::warning`, and the exit
                     code still equals the test verdict.
                   · Unset: the table goes to stdout.  [mvp+verification+risk]
                 - [CI evidence] The run page's Summary tab renders both tables, and a package
                   over 300s shows as a warning annotation.  [verification]
       depends:  t-3

  t-6  Pipe every CI go test step through the summarizer   [verification+risk; mvp inside its t-2]
       files:    .github/workflows/ci.yml, internal/cmd/ci_go_test_step_test.go
       covers:   c-3
       desc:     Audit ci.yml first (global CI hygiene rule). Both go test steps (the test job
                 at ci.yml:64 and mutation-ts at ci.yml:138, checked) become
                 `set -o pipefail; go test <flags> -json <pkgs> | go run ./cmd/testsummary`.
                 `-timeout 20m` STAYS in this task. Add a goTestSteps(workflow) scanner and a
                 goTestStepProblems checker. No tee and no upload of the raw -json.
       contract: - goTestStepProblems, driven by synthetic YAML, flags each of these by name:
                   · a piped go test without `set -o pipefail` or `shell: bash`
                   · a go test missing `-json`
                   · a go test not piped into `./cmd/testsummary`
                   · in the test job: `-short`, `-skip` or `-run`, or the loss of `-race`,
                     `-count=1` or `./...`  [risk+verification+mvp]
                 - TestGoTestStepScanner: a fixture holding a single-line `run: go test …`, a
                   `run: |` block, a commented-out `# go test` and a `name: go test` yields
                   exactly the two real invocations (non-vacuity).  [verification]
                 - Live sweep over all .github/workflows/*.yml: exactly 2 go test invocations
                   (test and mutation-ts), both clean. Dropping the mutation-ts pipe turns it
                   red, and so does a missing cmd/testsummary/main.go.  [risk+verification]
                 - No `tee` and no upload-artifact carries the -json stream (timing_surface:
                   raw -json is not retained).  [risk]
                 - These still pass: TestWorkflowsHaveNoExpressionsInRun (the summary path
                   reaches the tool as the env var $GITHUB_STEP_SUMMARY, never as `${{ }}`
                   inside run:), TestToolchainSingleSource, TestWorkflowActionsArePinned and
                   TestCIShellcheckCoversScripts.  [risk+mvp]
                 - [CI evidence] On the draft-PR run, the Summary tab shows the top-20 table
                   and package totals for both jobs, and the step log shows
                   `ok …/internal/cmd Ns`, not raw JSON.  [verification]
       depends:  t-3, t-4 (t-4 dependency: both edit ci.yml, so the edits are ordered)

GATE (human, between wave 2 and wave 3)                          [verification; risk's optional early read]
       After t-1…t-6 have landed, push phase/cmd-test-duration and open a draft PR. This is a
       human-gated external action. ship adopts an open PR via FindOpenPRByHead
       (internal/ship/headpr.go:26, checked). Read internal/cmd's package total from that
       run's step summary, in a separate turn, before queueing t-7.
       · ≤ 300s: queue t-7. If the reading falls between 270s and 300s, surface it to the
         user; it is inside observed runner variance (Disagreement 3).
       · > 300s: `-timeout 20m` stays, and the table's top rows go in via `dross task add`.

Wave 3
  t-7  Drop the -timeout 20m stopgap                                     [verification; risk+mvp bundle it]
       files:    .github/workflows/ci.yml, internal/cmd/ci_go_test_step_test.go
       covers:   c-2, c-1
       desc:     Audit ci.yml first. Remove `-timeout 20m` and the "-timeout 20m … Headroom
                 only" comment block (ci.yml:59-64, checked) from the test job.
                 goTestStepProblems now rejects any `-timeout` in any workflow's go test step.
                 Queued only after the gate reads ≤300s.
       contract: - A synthetic step containing `-timeout 20m`, `-timeout=20m` or `-timeout 0`
                   fails. Re-adding any of them to ci.yml fails the live sweep, naming
                   file:line (locked timeout_ceiling).  [verification+risk]
                 - ci.yml no longer contains "Headroom only" or "timeout 20m" (static grep
                   count = 0).  [all three]
                 - [CI evidence] The run after this lands is green, with internal/cmd's package
                   total ≤300s in the step summary and no `::warning`. That run is the evidence
                   for c-1 and c-2.  [verification]
       depends:  t-1, t-2, t-5, t-6 (+ GATE)
```

### Coverage

| Criterion | Tasks | Evidence |
|---|---|---|
| c-1 | t-1, t-2 (the cuts). t-3, t-5, t-6 are the instrument. t-7 carries the reading. | internal/cmd ≤300s in the post-t-7 step summary |
| c-2 | t-7 (drop the timeout and the comment), t-5 (the non-failing 300s warning that timeout_ceiling requires) | green `-race` run with no `-timeout` |
| c-3 | t-3 (verdict and log), t-5 (table), t-6 (wired on both CI go test steps) | Summary tab on both jobs |
| c-4 | t-4 | `gh run list`: superseded PR runs are `cancelled`, main runs never are |
| c-5 | t-1, t-2 (small savings without -race), t-5 (table on stdout) | Verify-only: one laptop run of `go test -count=1 -json ./internal/cmd \| go run ./cmd/testsummary`, with nothing heavy running |

### Execution notes [risk]

- internal/secretscan, internal/techdebt and cmd/testsummary are small, so they can be tested locally. The internal/cmd tests in t-4, t-6 and t-7 gate on `dross test` (remote), never on a local full run.
- Rule r-01: after t-1 and t-2, run `make install` before verify or ship. The dross binary carries the scanners.

### Residual risks all three drafts share (no divergence, no owning task)

- **c-5 may fail regardless of this plan.** All three measured the no-race scan saving at ≈12–15s. All three put the laptop's ≈600s down to go test's 10m default being reached under memory pressure; risk also cites macOS subprocess cost. No draft has a task that moves it. t-5's stdout table only makes a miss diagnosable.
- **c-1 has no pre-planned third cut.** All three reject pre-planning a task against files the evidence doesn't name, and route an over-budget reading through `dross task add`. The GATE is where that decision happens.

### Checked facts the drafts rely on (all hold)

- All new paths are free.
- These helpers exist: splitYAMLComment, readRepoFile, repoRootFromTest, TestSetupGoStepProblems, TestWorkflowsHaveNoExpressionsInRun, TestToolchainSingleSource, TestWorkflowActionsArePinned, TestCIShellcheckCoversScripts, TestEveryDrossWriterIsDeclared (walks internal/ only).
- The self-scan lines cited as :116, :125 and :236 are where the drafts say.
- validate.go calls scanDrossArtifacts.
- techdebt markerRe is at scan.go:47, and its FindString call at :74.
- The toolchain is go1.27.1.
- 0 of 286 internal/cmd test files call t.Parallel().
- There are no testing.Verbose() or `test.v` reads, so `-json`'s implied `-v` changes no test behaviour.
- release.yml has no concurrency block today.

## Disagreements

**1. Is the `-timeout 20m` drop gated on an observed CI reading?** (structure, highest stakes)
- **verification:** a separate wave-3 task, queued only after a draft-PR run shows internal/cmd ≤300s. The reasoning cites global rule 8: gated work must not share a turn with its gate.
- **mvp:** the drop is inside its single t-2, alongside the wiring and concurrency. It depends on t-1, so it follows the cut, but no reading gates it.
- **risk:** the drop is inside t-5 (the wiring), which depends only on t-3. It follows the cuts only because of wave numbering. The early draft PR is "optional".
- **Default:** verification's split plus the human GATE.
- **Why it matters:** identical code has already hit the 600.0s wall twice (runs 35606976673 and 35846448602). Dropping the ceiling without an observed reading lets a runner-variance swing redden the phase PR. It is also the exact shape rule 8 forbids. Cost: one extra CI round trip and a human-gated push.

**2. Is there a techdebt prefilter task?** (structure)
- **risk and verification:** yes (risk t-2, verification t-3). It is worth ≈24–28s under -race across the 3 whole-tree passes in techdebt_selfscan_test.go (:28 twice, :75 once; checked).
- **mvp:** omitted, and never mentioned.
- **Default:** include it as t-2.
- **Why it matters:** mvp's own projection is 230–320s, so c-1 is borderline. ≈25s is about 8% of the budget, bought with a low-risk change backed by an equivalence test. Omitting it raises the odds that the GATE reads over 300s and the phase needs a `dross task add` round.

**3. FACT: runner variance, and whether the 09-19 +75s is a regression.**
- **verification:** there is an "unexplained +75s at ast-aware-mutation-ranges (366e716)", and variance is ≈1.3×.
- **mvp:** 279.1s on 6bdf288 against 200.8s on 2ded92a is variance on identical Go code, about ±40%.
- **risk:** ±25%.
- **Checked:** `gh run view` maps 35455381954 to 2ded92a and 35455697471 to 6bdf288. `git diff 2ded92a 6bdf288` touches only ci.yml (the mutation-ts selector). **mvp is right.** verification's "regression" is noise, and the observed variance on identical code is up to 1.39×.
- **Default:** treat it as variance. No task chases the +75s. The GATE flags any 270–300s reading.
- **Why it matters:** c-1 is judged on a single CI run. With variance up to 1.39×, a true mean of ≈220s can read over 300s, and a single ≤300s reading does not guarantee the next run stays under. The non-failing 300s warning is the backstop.

**4. FACT: residual cost after the cut, and the c-1 projection.**
- **risk:** its prototype runs at 2.47s per pass under -race; the regex ran on 4,352 of 4.23M line×rule pairs. Projection: 200–300s.
- **verification:** its bytes.Contains anchors plus a lowered copy run at 4.6s per pass. No projection, but it wants a cut added above ≈270s.
- **mvp:** it only measured a global any-literal gate (15s per pass). It estimates 15–60s of scan cost left on CI and projects 230–320s ("t-1 alone may not clear 300s").
- **Smaller numeric spreads:**
  - the #128 step: +247–310s (risk), +250–390s (mvp), +170–310s (verification)
  - a no-race pass: 3.4s (risk/mvp) vs 3.7s (verification)
  - a .dross -race pass: 45.7s (mvp) vs 48s (verification)
  - a techdebt pass: 9.5s (risk) vs 8.0s (verification)
- **Where they agree:** all three agree on the root cause: #128 (a51ecdd) added four whole-tree or .dross regex passes in secretscan_selfscan_test.go, and d93b725 is not causal.
- **Default:** per-rule needles (risk's 2.47s design). No third-cut task; the GATE decides.
- **Why it matters:** these numbers decide whether wave 3 runs at all or the phase grows a `dross task add` cut.

**5. Should the whole-tree self-scan be computed once in the test (sync.Once + walk counter)?**
- **mvp:** yes, inside its t-1, editing internal/cmd/secretscan_selfscan_test.go. TestDrossTreeHasNoUnmarkedHits and TestSelfScanIsNotVacuous would share one walk, and the vacuity test would scan only its planted file on top of it.
- **verification:** explicitly rejects it. It couples tests through order-dependent state, and after the prefilter a pass costs ≈5s anyway.
- **risk:** rejects memoization because it saves 1 of 4 scans.
- **Default:** exclude it. The internal/cmd self-scan tests stay unedited.
- **Why it matters:** today TestSelfScanIsNotVacuous proves that the full walk plus a planted file yields exactly one hit (checked, :125–139). mvp's version proves only that cached hits plus a separate scan of the planted file yield one, which weakens what the non-vacuity test pins, for a saving of ≈2.5–5s after the prefilter.

**6. FACT / contract defect: must benignCorpus rows fail every prefilter anchor?**
- **verification's TestAnchorRejectsOrdinaryLine:** "every benignCorpus() row fail[s] all nine rules' anchors".
- **Checked:** benignCorpus (secretscan_test.go:66–87) includes `token = "$GITHUB_TOKEN"`, `password = abab…c`, `Authorization: Bearer <token>` and the AKIA… doc placeholder. The regexes reject these on value class, entropy or carve-out, not on a missing literal. A necessary-literal prefilter must admit them.
- **risk and mvp:** avoid the trap. risk asserts pruning only on needle-free lines. mvp feeds benignCorpus into the implication (regex ⇒ Admits) only.
- **Default:** drop verification's benignCorpus clause. Keep the needle-free ordinary-line case, and put benignCorpus rows into the equivalence seeds.
- **Why it matters:** as written, that contract can only be satisfied by a prefilter that is too narrow, one that would suppress real key-context and authorization-header hits in the secret gate.

**7. Summarizer log policy: echo everything, or only failures?**
- **mvp:** echo every Output field verbatim. It is the least code and never drops the context around a failure.
- **risk and verification:** buffer, and print only failed tests' output, failed packages' package-level output and build errors. Their contracts assert that passing tests' output is absent.
- **Default:** buffer.
- **Why it matters:** `-json` implies `-v`, so verbatim echo prints `=== RUN` and `--- PASS` for every internal/cmd test, and the step log stops being "failures render human-readable" (timing_tooling). mvp's valid concern, that a timeout's goroutine dump arrives as package-level output, is covered by the package-level-output rule and pinned by t-3's real timeout fixture.

**8. Summarizer exit semantics.**
- **mvp:** exits 1 only on a fail event.
- **risk and verification:** also exit non-zero on an empty stream and on a stream with no package terminal event.
- **Default:** risk+verification.
- **Why it matters:** if go test dies before emitting, or the pipe truncates, mvp's tool exits 0 and the verdict rests on pipefail alone. Both layers are pinned so that neither can silently turn a red run green.

**9. Real-toolchain fixtures or synthetic streams?**
- **risk:** runs the real `go test -json` of the pinned toolchain on a throwaway module for the pass/fail, timeout and build-fail shapes.
- **verification and mvp:** hand-written inline streams.
- **Default:** real fixtures for those three failure shapes (risk), synthetic streams for the cap, sort and boundary tests.
- **Why it matters:** how go1.27 test2json attributes a timeout panic and build output is exactly what a hand-written stream gets wrong, and the timeout is the run where the summary matters most. Cost: a nested go build in cmd/testsummary's tests (small, laptop-safe). The fixtures must be inline sources (see t-3's checked note on the audit sweeps).

**10. Summarizer granularity: one task or two?**
- **risk:** two tasks. t-3 covers the reducer and verdict; t-4 covers the table and warning.
- **verification:** one task (its t-1).
- **mvp:** part of one task that also does the wiring.
- **Default:** split (merged t-3 and t-5). The wiring (t-6) depends only on t-3.
- **Why it matters:** the split isolates the false-green logic in its own pair-reviewed commit. It costs no wave, because the human GATE already separates waves 2 and 3. The cost is one more task, and the draft PR must wait for t-5 as well as t-6.

**11. mvp bundles the summarizer, ci.yml wiring, timeout drop and concurrency into one task, and puts concurrency behind the cut.**
- **mvp:** one t-2 in wave 2 covering c-2, c-3 and c-4. Its argument: splitting leaves either a tool with no caller or a workflow that references a missing path.
- **risk and verification:** separate tasks, with concurrency independent in wave 1.
- **Default:** separate tasks, concurrency in wave 1 (t-4).
- **Why it matters:** pair mode reviews one task at a time. A single failure in mvp's t-2 blocks three criteria. mvp's dangling-path worry is handled by the t-6 sweep failing if cmd/testsummary/main.go is missing.

**12. Summarizer location and name.**
- **risk and mvp:** `cmd/testsummary`, package main only. risk rejected internal/ because TestEveryDrossWriterIsDeclared walks it.
- **verification:** an `internal/testtiming` library plus a thin `cmd/testtiming` main. The library takes only io.Writers, and the file write lives in cmd/.
- **Default:** `cmd/testsummary`.
- **Why it matters:** mostly naming. A single package avoids any interplay with the internal/ writer registry. Both keep the tool out of goreleaser (checked: `main: ./cmd/dross`) and out of the shipped CLI surface.

**13. Where the table goes when `$GITHUB_STEP_SUMMARY` is unset.**
- **risk:** stdout.
- **verification:** stderr.
- **mvp:** nothing is written, which contradicts its own c-5 instruction to pipe the laptop run through the tool.
- **Default:** stdout.
- **Why it matters:** the c-5 laptop run is the only place a local miss can be diagnosed. If nothing is written there, c-5's evidence carries no timings.

**14. The CI pipe step: how the summarizer is invoked, and how pipefail is enforced.**
- **Invocation:** risk runs `go build -o "$RUNNER_TEMP/testsummary"` as its own step, then pipes into the binary. mvp and verification use `go run ./cmd/testsummary` inside the pipe.
- **pipefail:** mvp's checker requires `shell: bash` specifically. risk and verification accept `set -o pipefail` or `shell: bash`.
- **Default:** `go run` in the pipe. ci.yml writes an explicit `set -o pipefail`, and the checker accepts either form.
- **Why it matters:** low stakes. Pre-building would make a summarizer compile error show up as its own failed step rather than a SIGPIPE'd go test; `go run` saves a step per job. Neither adds supply-chain surface.

**15. Concurrency group prefix.**
- **risk:** a literal `ci-` prefix, flagged if missing, because the group namespace is repo-wide and shared with release.yml.
- **verification:** a `${{ github.workflow }}-` prefix, which no contract pins.
- **mvp:** no prefix.
- **Default:** literal `ci-`, the only form a test pins.
- **Why it matters:** low stakes. release.yml has no concurrency block today (checked), so a collision is only prospective.

**16. Prefilter API shape.**
- **risk:** an unexported, pure `candidates(line)` bitmask, with fallback on the two fold runes.
- **verification:** bytes.Contains anchors against one lowered copy of the line, with the same fallback.
- **mvp:** an exported `Rule.Admits(line)` that does fold-aware literal matching with no fallback. It needs to be exported because mvp's test lives in the external secretscan_test.go.
- **Default:** risk's unexported candidates() plus the rune fallback, tested in-package in prefilter_test.go.
- **Why it matters:** it keeps secretscan's public API unchanged. The explicit fallback is easier to pin with a named mutant than a hand-rolled Unicode-fold matcher. risk's design also measured faster (2.47s vs 4.6s per pass), since it avoids a lowered copy of every line.
