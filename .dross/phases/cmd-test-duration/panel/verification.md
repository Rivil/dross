# cmd-test-duration: verification-lens draft

Lens: I wrote each criterion's ideal test contract first, then derived the smallest task that makes it satisfiable. Where the behaviour only happens on a GitHub runner, the contract is tagged **[CI evidence]**: a Go test can't see it, and /dross-verify reads it from a real run.

```
Phase cmd-test-duration — 6 tasks across 3 waves

Wave 1
  t-1  Build go-test-json timing summarizer
       files:    internal/testtiming/testtiming.go, internal/testtiming/testtiming_test.go,
                 cmd/testtiming/main.go, cmd/testtiming/main_test.go
       desc:     Stdlib-only Summarize(events, log, summary, budget) over test2json events: log gets
                 package ok/FAIL lines plus buffered output of failing tests (and package-level output
                 on a package fail); summary gets markdown top-20 slowest top-level tests + per-package
                 totals; one `::warning` per package over budget. Thin main: stdin in, log→stdout,
                 summary appended to $GITHUB_STEP_SUMMARY (stderr when unset), exit 1 on any failure.
       covers:   c-3, c-2
       contract: TestTopTwentyOrderAndCap: a 25-test/2-package inline stream yields exactly 20 rows
                 in descending elapsed; the 21st-slowest is absent; subtest `TestA/x` is never a row
                 (a sort-direction or `<=20` off-by-one mutant fails)
                 TestPackageTotalsUsePackageElapsed: the totals row equals the package-level pass event's
                 Elapsed, not the sum of its tests
                 TestFailuresRenderHumanReadable: the log carries `--- FAIL: TestBroken` and its t.Errorf
                 line; passing tests' `=== RUN` lines and stdout chatter are absent
                 TestTimeoutPanicSurfacesAndStillSummarizes: a stream with package output
                 "panic: test timed out after 10m0s" + "running tests:" and a package fail but no per-test
                 fail → both lines are in the log, failed=true, and tests that passed before the panic
                 still appear in the table
                 TestBuildFailIsFailure: a build-fail event → failed=true and the compiler output echoed
                 TestEmptyStreamIsFailure: empty input, or a stream with no package terminal event →
                 failed=true (a `go test` that died before emitting can't pipe to green)
                 TestBudgetWarningIsNonFailing: a package at 300.1s → exactly one `::warning` line naming
                 the package and the 300s budget; at 300.0s → none; a passing stream with a 301s
                 package still exits 0 (the `>` boundary and "warning never fails" are both pinned)
                 TestStdlibOnly: every import in testtiming.go and main.go is stdlib (first path element
                 has no dot); adding a third-party import fails (locked timing_tooling)
                 main_test: run() on a failing stream returns 1, on a passing stream 0; with
                 GITHUB_STEP_SUMMARY → a temp file holding prior content, the tables are APPENDED and the
                 prior content survives
                 [CI evidence] the run page's Summary tab renders both tables; a >300s package shows as
                 a warning annotation on the run page

  t-2  Prefilter secretscan rules with literal anchors
       files:    internal/secretscan/rules.go, internal/secretscan/secretscan.go,
                 internal/secretscan/prefilter_test.go
       desc:     Each Rule gets required literals checked with bytes.Contains before its regex runs.
                 (?i) rules check one lowered copy of the line, and bypass the prefilter when the line
                 holds U+017F (ſ) or U+212A (K), which Go's (?i) folds to s/k. scanLine's hit set is
                 unchanged.
       covers:   c-1, c-5
       contract: TestAnchorsAreNecessary: for every rule, over every hitCorpus() line, 500 randomLine()
                 instances per rule, and upper/mixed-case variants (`PASSWORD: …`,
                 `AUTHORIZATION: BEARER …`), regex match ⇒ anchor passes; dropping `ASIA` from
                 aws-access-key or checking a (?i) rule against the un-lowered line fails it, naming
                 the rule
                 TestUnicodeFoldBypass: `paſſword = <hi-entropy>` and `toKen: <hi-entropy>` (Kelvin
                 sign) still yield a key-context hit (verified: Go's (?i) matches both, and an
                 ASCII-lowered `pass`/`token` anchor would hide them)
                 TestAnchorRejectsOrdinaryLine: `func main() { fmt.Println("hi") }` and every
                 benignCorpus() row fail all nine rules' anchors (kills the always-true anchor mutant
                 that silently restores full cost)
                 FuzzAnchorsAreNecessary: seeded fuzz target (seeds run under plain go test);
                 regex match ⇒ anchor, for any line
                 TestEveryRuleHasAHitCase, TestBenignCorpusZeroHits, TestCarveOutsAreNotVacuous,
                 TestAllowMarkerSilencesOnlyItsLine, and internal/cmd's TestDrossTreeHasNoUnmarkedHits /
                 TestSelfScanIsNotVacuous / TestValidateOnDrossItself stay unedited and green
                 (coverage_preserved: same files, same assertions)
                 [CI evidence] in the first summarized CI run, those three internal/cmd tests are each
                 ≤ 15s in the top-20 (or absent). Local -race pre-fix is ~74s / ~74s / ~96s.

  t-3  Anchor techdebt marker regex on literals
       files:    internal/techdebt/scan.go, internal/techdebt/scan_test.go
       desc:     scanContent runs markerRe only on lines containing TODO|FIXME|HACK|XXX (strings.Contains);
                 the long-line / oversized-file heuristics are untouched.
       covers:   c-1, c-5
       contract: TestMarkerAnchorIsNecessary: every line markerRe matches across scan_test fixtures plus
                 `x := 1 // FIXME later`, `/* HACK */`, `XXX:` passes the anchor; dropping any word from
                 the anchor list fails it and the existing TestScanMarkersMidLineAllKinds
                 TestMarkerAnchorRejectsPlainLine: `return nil` fails the anchor (always-true mutant)
                 TestScanMarkerWordBoundary (`TODOList` not a finding) unchanged and green: the anchor
                 gates the \b regex and never replaces it
                 [CI evidence] TestTechdebtSelfScanExcludesOwnPackage and
                 TestTechdebtDrossTreeNoFixtureFindings each ≤ 5s in the top-20. Local -race pre-fix
                 is ~8s per whole-tree pass; the first test does two passes.

  t-4  Add PR-only concurrency group to ci.yml
       files:    .github/workflows/ci.yml, internal/cmd/ci_concurrency_test.go
       desc:     Workflow-level `concurrency:` with group
                 `${{ github.workflow }}-${{ github.event_name == 'pull_request' && github.ref || github.run_id }}`
                 and `cancel-in-progress: ${{ github.event_name == 'pull_request' }}`. Pure line-based
                 concurrencyProblems(workflow) checker plus a live sweep of ci.yml (reuses
                 splitYAMLComment / readRepoFile).
       covers:   c-4
       contract: TestCIConcurrencyLive: ci.yml carries a top-level (indent 0) `concurrency:` block;
                 deleting it fails, naming ci.yml
                 TestConcurrencyProblems (one synthetic fixture per rejection branch, TestSetupGoStepProblems
                 shape): `cancel-in-progress: true` → rejected (cancels push-to-main runs);
                 group `${{ github.ref }}` with no per-run key for non-PR events → rejected (GitHub
                 replaces a PENDING run in the same group even without cancel-in-progress, so a queued
                 main push would be dropped); job-level-only concurrency → rejected; the correct block
                 → zero problems
                 TestWorkflowsHaveNoExpressionsInRun stays green (the expressions live in
                 concurrency:, never in run:)
                 [CI evidence] on this phase's ship, `gh run list --branch phase/cmd-test-duration`
                 shows every superseded pull_request run `cancelled` and only the newest completed;
                 after merge, no main push run is `cancelled`

Wave 2 (depends t-1, t-4)
  t-5  Pipe every go test step through summarizer
       files:    .github/workflows/ci.yml, internal/cmd/ci_gotest_step_test.go
       desc:     Both go test steps (test job and mutation-ts) become
                 `set -o pipefail; go test <flags> -json <pkgs> | go run ./cmd/testtiming`. `-timeout 20m`
                 STAYS for now. Adds goTestSteps(workflow) scanner + goTestStepProblems checker. After
                 landing: push + open a draft PR (human-gated) so CI produces the first table.
       covers:   c-3
       contract: TestEveryGoTestStepIsSummarized (live sweep, all .github/workflows/*.yml): a `go test`
                 run line lacking `-json`, lacking `| go run ./cmd/testtiming`, or lacking
                 `set -o pipefail`/`shell: bash` fails, naming file:line. GitHub's default
                 `bash -e` has no pipefail.
                 TestGoTestStepScanner: an inline fixture with a single-line `run: go test …`, a
                 `run: |` block, a commented-out `# go test`, and `name: go test` → exactly the two real
                 invocations are found (non-vacuity)
                 TestCITestJobKeepsRaceOnEverything: the test job's go test carries `-race`, `-count=1`,
                 `./...` and no `-short`, `-run`, `-skip`; each violation in a synthetic fixture fails
                 (locked coverage_preserved)
                 the sweep fails if cmd/testtiming/main.go is missing (the pipe target can't dangle)
                 [CI evidence] the draft-PR run's Summary tab shows top-20 + package totals for both jobs;
                 the step log shows `ok  …/internal/cmd  Ns`, not raw JSON. GATE: read the internal/cmd
                 total here before t-6 is queued.

Wave 3 (depends t-2, t-3, t-5)
  t-6  Drop the -timeout 20m stopgap
       files:    .github/workflows/ci.yml, internal/cmd/ci_gotest_step_test.go
       desc:     Remove `-timeout 20m` and the "-timeout 20m … Headroom only" comment block from the test
                 job; goTestStepProblems rejects any `-timeout` in any workflow go test step. Queued only
                 after the t-5 draft-PR run shows internal/cmd ≤ 300s.
       covers:   c-2, c-1
       contract: TestNoGoTestTimeoutOverride: `-timeout 20m`, `-timeout=20m` and `-timeout 0` in a
                 synthetic step each fail; re-adding any of them to ci.yml fails the live sweep, naming
                 file:line (locked timeout_ceiling: 10m default)
                 static verify evidence: `grep -c 'Headroom only\|timeout 20m' .github/workflows/ci.yml` = 0
                 [CI evidence] the run after this lands is green, with the internal/cmd package total
                 ≤ 300s in the step summary and no `::warning` annotation. That run is c-1's and c-2's
                 evidence.
```

## Coverage

- **c-1** (internal/cmd ≤ 300s under -race on CI): t-2 and t-3 are the cuts. t-6's post-drop run is the evidence. t-1 and t-5 are the instrument the reading comes from.
- **c-2** (no `-timeout 20m`, comment gone, green under the 10m ceiling): t-6 drops it, and t-1 supplies the non-failing 300s warning.
- **c-3** (every CI go test run shows slowest tests): t-1 builds the tool, and t-5 wires it into both go test steps with a sweep over all workflows.
- **c-4** (per-ref concurrency, cancel superseded): t-4.
- **c-5** (laptop `go test -count=1 ./internal/cmd` < 10m, verify evidence): t-2 and t-3 are the only CPU cuts. t-1 makes the one local run diagnosable (`… -json ./internal/cmd | go run ./cmd/testtiming`). **At risk** (see Investigation notes): the prefilters save only ~13s without race.

## Judgment calls

- **Prefilters in wave 1, not after a CI "before" table.** I rejected serializing instrument → before-table → cut. A local -race run of the copied scanner already pins the cost (74s per whole-tree pass → 4.6s anchored), and the CI jump lines up exactly with the secret-detection merge. One more serialized pipeline buys attribution the benchmark already gives.
- **The `-timeout` drop is its own wave-3 task, gated on the t-5 CI reading.** I rejected folding it into t-5. Dropping the ceiling is exactly the work the CI timing reading is meant to authorize, so it can't share a turn with the gate (global rule 8). If the gate reads > 300s, the ceiling stays and a cut is added.
- **No speculative "cut the rest" task.** If the t-5 draft-PR table shows internal/cmd > ~270s (runner variance was 1.3× on identical code: 447s vs 589s), the table's top rows name the next cut, added with `dross task add`. I rejected pre-planning it because a task with no named test can't carry a specific contract.
- **Scanner-level speedup, not test-level caching.** I rejected a sync.Once cache shared by TestDrossTreeHasNoUnmarkedHits and TestSelfScanIsNotVacuous. It saves one pass but couples tests through order-dependent state, and after the prefilter a pass is ~5s anyway. The prefilter also speeds up production `dross validate` and ship's pre-flight, which scan all of `.dross` on every call.
- **The anchor-necessity property runs over the corpus plus fuzz seeds, not the tracked tree.** A property test that runs all nine regexes over 23.6 MB under -race would bring back the ~74s it removes.
- **Unicode-fold bypass for (?i) rules.** I rejected a plain ASCII lowercase anchor: it silently hides `paſſword=…` and `toKen:…` hits (confirmed against Go's regexp). The bypass keys on the two exact byte sequences. It doesn't key on "any non-ASCII byte", because em-dashes are common in `.dross` prose and would send most lines to the full regex.
- **The summarizer lives at internal/testtiming (library) + cmd/testtiming (thin main).** I rejected a jq script (no Go test can pin it; jq isn't on the laptop) and a hidden dross subcommand (it would move TestCLISurfacePinned's golden and ship CI tooling to users). goreleaser builds only ./cmd/dross. The file write sits in cmd/, outside the internal/-scoped secretscan writer registry, and the library only takes io.Writers.
- **Top-20 lists top-level tests only.** I rejected including subtests, because a parent's elapsed already includes them and listing both double-counts one cost across rows.
- **Pipefail AND summarizer-exits-1, both pinned.** Either alone has a hole. Without pipefail, a crash the summarizer misreads exits 0. Without the summarizer's own exit, an empty stream with pipefail still depends on go test's code alone.
- **Concurrency group keys on `github.run_id` for non-PR events.** I rejected `github.ref` for everything. GitHub replaces a pending run in the same group even with cancel-in-progress false, which breaks the locked "push-to-main runs are never cancelled".
- **Early draft PR for the t-5 reading, not a `workflow_dispatch` trigger.** Ship adopts an existing open PR for the head (internal/ship/headpr.go FindOpenPRByHead), so a draft needs no ci.yml trigger change. Opening it is an external action, so it's human-gated.
- **CI hardening audit of ci.yml (global rule), no findings.** Every `uses:` is SHA-pinned with a `# vX.Y.Z` comment. `permissions: contents: read`. Go jobs set GOFLAGS/GOPROXY/GOSUMDB. govulncheck is pinned at v1.8.0, `npm ci --ignore-scripts` is used, and node is pinned exactly. `go run ./cmd/testtiming` compiles stdlib-only in-repo code under -mod=readonly, so it adds no new supply-chain surface. Pipeline-optimisation step 0.3 (auto-cancel) is c-4, and govulncheck scoping and the shellcheck fold are already deferred in spec.toml.

## Investigation notes

- **The jump is a single step at secret-detection (#128, a51ecdd, 2026-09-20).** These are internal/cmd package times from CI logs:
  - mutation-range-provenance run 35427961496: 204.3s
  - ast-aware-mutation-ranges run 35455697471: 279.1s
  - secret-detection runs 35512718209 and 35512724506: 589.4s and 447.8s
  - remote-host-mutex run 35608340127: 441.6s
  - cmd-package-decomposition run 35706528623: 452.6s
  - dependency-update-automation run 35850995391: 597.3s

  The runs measured on the go test step were 255s → 314s → 639s/487s → 474s → 491s → 646s. The other packages all finish in under 10s.
- **Root cause, statically pinned and measured.** a51ecdd added internal/cmd/secretscan_selfscan_test.go, which regex-scans the repo's real tracked tree:
  - TestDrossTreeHasNoUnmarkedHits: one whole-tree pass (secretscan_selfscan_test.go:116)
  - TestSelfScanIsNotVacuous: another whole-tree pass (:125)
  - TestValidateOnDrossItself (:236) calls validate twice. Since a51ecdd, validate.go runs scanDrossArtifacts over all of `.dross`, so that's two more `.dross` passes.

  The scanner runs nine regexes per line (internal/secretscan/rules.go), and none has a usable literal prefix. The `\b`, the alternations and key-context's leading `(?i)[A-Za-z0-9_.-]*` all defeat Go's prefix skip.

  I timed a copy of the scanner over this checkout (1,883 files, 23.6 MB, 468,630 lines) on this laptop:
  - Without race: 3.7s per whole-tree pass.
  - Under -race: 74s per whole-tree pass and 48s per `.dross` pass. key-context alone is 32s of the 74s.
  - Total for these tests: ≈ 244s locally, which fits the +170-310s CI jump.
  - With a bytes.Contains anchor prefilter under -race: 4.6s per pass (about 16x), with the same match set.

  `bytes.Index` is assembly, so the race detector doesn't instrument it. That's why the anchor escapes the ~20x -race tax that the regex VM pays.
- **Secondary: the techdebt self-scan** (internal/cmd/techdebt_selfscan_test.go, added 2026-09-13, so already in the 204s baseline) makes three whole-tree `markerRe` passes. Under -race each is 8.0s of regex; anchored it takes 16ms with an identical match count.
- **The d93b725 lead is directionally right but not causal.** Both timeouts ran on d93b725 (changes.json status flip only), but other commits of the same PR ran 612-688s end-to-end. Identical code spans 447-597s, so d93b725 just crossed 600s by runner variance. The real link to "tests that read the repo's own `.dross`" is that `.dross` is 14.9 MB of the 23.6 MB tracked tree, and the tracked tree grew from 21.1 MB at 1a1025e to 23.6 MB at 72c5ed8. So whole-tree scan cost regrows with every phase. The 300s warning annotation is what catches that.
- **The timeout dump names the wrong suspect.** Run 35846448602's panic lists `TestTechdebtDrossTreeNoFixtureFindings (4s)` as the only running test, with a goroutine in regexp backtrack from techdebt/scan.go:74. internal/cmd has 0 t.Parallel files (2,521 tests run serially), so the dump only names whichever test happened to be live at 600s. That's why c-3's per-test table is needed.
- **Unexplained: the +75s at ast-aware-mutation-ranges (366e716).** It added AST sweeps (pad_residue_test.go) and verify-scoping tests. Nothing is pinned yet, and the first t-5 table will show it.
- **No wall-clock waits were added in the window.** remote-host-mutex's test_wait_test.go and verify_lock_test.go use fake durations only. `.git` is 11 MB, so TestValidateOnDrossItself's `git clone --no-hardlinks` is not the cost.
- **c-5 risk.** Without race, the prefilters save only ~13s (≈12s of secretscan, ≈1.5s of techdebt). The ~602s laptop "hang" is more likely go test's 10m default reached under memory pressure (subprocess stalls) than scanner CPU. If the one laptop run still exceeds 10m, this phase's cuts didn't cause it, and the summarizer's local table is the diagnostic.
