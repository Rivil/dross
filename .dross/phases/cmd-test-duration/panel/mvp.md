# cmd-test-duration: MVP lens

```
Phase cmd-test-duration: 2 tasks across 2 waves

Wave 1
  t-1  Gate secretscan rules on literals; scan once
       files:    internal/secretscan/rules.go, internal/secretscan/secretscan.go,
                 internal/secretscan/secretscan_test.go, internal/cmd/secretscan_selfscan_test.go
       covers:   c-1, c-5
       desc:     Each Rule declares the literal(s) its regex cannot match without. For the two (?i)
                 rules (authorization-header, key-context) these are matched under Go's case folding,
                 including U+017F (long s) and U+212A (Kelvin K). scanLine skips a rule's regex on any
                 line that lacks them; the check is exposed as Rule.Admits(line). In the self-scan
                 test, the full-tree hit set is computed once per test binary (sync.Once), so
                 TestDrossTreeHasNoUnmarkedHits and TestSelfScanIsNotVacuous share one walk, and
                 the vacuity test scans only its planted file on top of it.
       contract: - Property test (secretscan_test.go): take every line in hitCorpus, benignCorpus
                   and a set of fold variants (all-caps PASSWORD label, long-s label spelling, a
                   Kelvin-K spelling of api_key, an all-caps AUTHORIZATION Bearer header). For
                   every rule r in Rules(), r.Regex.Match(line) must imply r.Admits(line).
                   Dropping "token" from key-context's literals fails it. So does gating a (?i)
                   rule on an ASCII-only lowercase.
                 - TestEveryRuleHasLiterals fails for any rule in Rules() with an empty literal
                   set, so a new rule cannot bypass the gate unnoticed.
                 - TestEveryRuleHasAHitCase, TestBenignCorpusZeroHits, TestCarveOutsAreNotVacuous
                   and TestLongLineIsScannedToTheEnd pass with no edits: the gate changes no
                   verdict.
                 - TestSelfScanIsNotVacuous still fails when the planted gitlab-pat file is not
                   reported: remove its mustWrite and it sees 0 hits, not 1. Calling the cached
                   tree-hits helper twice walks the tracked tree once (a walk counter is asserted
                   to equal 1).

Wave 2 (depends t-1)
  t-2  Emit test timing summary; drop timeout; add concurrency
       files:    cmd/testsummary/main.go, cmd/testsummary/main_test.go,
                 .github/workflows/ci.yml, internal/cmd/ci_workflow_test.go
       covers:   c-2, c-3, c-4 (and the surface c-1 is read from)
       desc:     New stdlib-only main. It reads `go test -json` on stdin and echoes every Output
                 field verbatim to stdout. It appends a markdown table (top 20 slowest tests plus
                 per-package totals) to the file named by $GITHUB_STEP_SUMMARY, prints one
                 `::warning` line per package that ran over 300s, and exits 1 on any fail event.
                 ci.yml: both `go test` steps (test job and mutation-ts) become
                 `go test -json ... | go run ./cmd/testsummary` under `shell: bash`. The test job
                 keeps `-race -count=1 ./...`. `-timeout 20m` and its "Headroom only" comment are
                 deleted. A workflow-level `concurrency:` block is added: the group is keyed on
                 github.ref for pull_request and on github.run_id otherwise, and
                 cancel-in-progress is `${{ github.event_name == 'pull_request' }}`.
       contract: - Summarizer: a synthetic stream of 25 passing tests across 2 packages produces
                   exactly 20 test rows, sorted by Elapsed descending (the 21st-slowest test is
                   absent), plus one total row per package. Each total comes from that package's
                   test-less pass/fail event Elapsed, not from summing test rows.
                 - A package Elapsed of 300.0 emits no `::warning`. 300.01 emits exactly one line
                   starting `::warning` that names the package and its seconds. Exit code is 0 in
                   both cases, because the warning never fails the step (timeout_ceiling).
                 - A stream holding a `fail` event for TestX puts its `--- FAIL: TestX` line and
                   the t.Errorf message line on stdout verbatim, and the process exits non-zero.
                   The same stream with every event flipped to pass exits 0.
                 - If GITHUB_STEP_SUMMARY points at a file already holding a line, that line is
                   still first afterwards (the tool appends, never truncates). With the variable
                   unset, nothing is written and the exit is still 0.
                 - A non-JSON input line (go test emits some on build failures) is echoed as-is
                   and is not fatal.
                 - ci_workflow_test.go (line-based, reusing splitYAMLComment and readRepoFile)
                   fails if any of these hold:
                   - any `go test` line in ci.yml carries `-timeout`;
                   - the test job's `go test` lacks `-race` or `-json`;
                   - any `go test` step is not piped into `./cmd/testsummary`;
                   - a piped step lacks `shell: bash` (the only runner shell with pipefail);
                   - the phrase `Headroom only` reappears;
                   - top-level `concurrency:` is missing;
                   - `cancel-in-progress:` is not the pull_request-conditional expression;
                   - the group expression has no `github.run_id` arm. A plain per-ref group lets
                     GitHub replace a pending main run even with cancel-in-progress false, which
                     would break concurrency_scope.
                 - TestWorkflowsHaveNoExpressionsInRun and TestWorkflowActionsArePinned stay
                   green. The summary path reaches the tool as an env var, not an inline `${{ }}`,
                   and no `uses:` is added.
                 - Only visible on a real CI run, so recorded as verify evidence: the step-summary
                   table renders on the run page; internal/cmd <= 300s, read from that table
                   (c-1); a second quick push to the PR branch leaves the superseded run
                   `cancelled` in `gh run list` (c-4); main push runs are never cancelled.
```

## Coverage

| Criterion | Tasks | Notes |
|---|---|---|
| c-1 | t-1, t-2 | t-1 removes the measured cost. t-2's summary is where the <= 300s figure is read, on the phase PR's CI run. |
| c-2 | t-2 | The `-timeout` removal is in wave 2 so it is green only because t-1 already landed. |
| c-3 | t-2 | Covers both ci.yml `go test` steps, as "every CI go test run" requires. |
| c-4 | t-2 | Concurrency is limited to PRs by locked decision `concurrency_scope`. |
| c-5 | t-1 | Verify evidence only, per the spec. Run `go test -json -count=1 ./internal/cmd \| go run ./cmd/testsummary` once on the laptop. |

## Judgment calls

- **Fixed the measured cause, not a general speed-up programme.** Rejected rolling out t.Parallel or splitting internal/cmd. About 240s of the roughly 330s step lives in four secretscan passes (see notes). Anything else is speculative until the t-2 summary exists.
- **The literal gate lives in the production scanner, not only as test-side dedupe.** Dedupe alone saves one full-tree scan. TestValidateOnDrossItself scans .dross twice through production `scanDrossArtifacts`. Only a faster scanner reaches those scans without swapping real-repo reads for fixtures, which `coverage_preserved` forbids.
- **Per-rule gate, not one global any-literal gate.** In the experiment, a global gate still let through 19% of bytes (long lines that mention "token" or "secret") and left the race scan at 15s. Per-rule gating keeps key-context, measured at 31s, off lines that only carry provider prefixes.
- **Rejected goroutine fan-out of the scan.** It only helps on idle runner cores and adds ordering code to a security gate. The gate removes work instead.
- **Rejected a regex rewrite.** For example, dropping key-context's leading `[A-Za-z0-9_.-]*`. Proving it equivalent is harder than proving a necessary-literal gate, and the property test pins the gate cleanly.
- **One task holds the summarizer, ci.yml and the content test.** The tool's only caller is ci.yml. Splitting them leaves either a tool with no caller or a workflow that references a missing path.
- **t-2 is in wave 2 even though the tool has no dependency on t-1.** Dropping `-timeout 20m` only passes c-2 once t-1 has landed. I accepted losing some parallelism to get one fewer task.
- **The tool lives at `cmd/testsummary`, not as a hidden `dross` subcommand.** A subcommand would enter the CLI-surface, parity and interaction tests, and would ship to users for a CI-only need.
- **Echo all Output verbatim instead of buffering only failing tests' output.** It needs less code and never drops the context around a failure. For example, a timeout panic's goroutine dump arrives as package-level Output, not per-test Output.
- **Concurrency group is keyed on `github.run_id` for non-PR events, not just `github.ref`.** GitHub cancels the pending run in a shared group even when cancel-in-progress is false. A per-ref group would cancel main runs during a merge train, which `concurrency_scope` forbids.
- **Both `shell: bash` (pipefail) and a non-zero tool exit on fail.** Without pipefail, go test's exit status is lost in the pipe. The tool's exit alone misses a go test crash that emits no fail event.
- **No pre-planned "cut the next offender" task.** Without the timings, its contract could not be specific. If the phase PR's summary still shows internal/cmd over 300s, the offender it names goes in via `dross task add`.
- **c-5 gets no task of its own.** The spec calls it a one-off verify measurement. t-1 plus the locally runnable t-2 tool are what serve it.

## Investigation notes

**Where the jump happened.** internal/cmd `-race` durations come from read-only `gh run view --log` on Rivil/dross ci.yml runs:

| Date | Runs | internal/cmd duration |
|---|---|---|
| 09-09 → 09-14 | several | 149.6s → 202.8s (slow growth) |
| 09-19 | 5 runs | 176.2s, 204.3s, 203.5s, 200.8s, and 279.1s on 6bdf288 |
| 09-20 (secret-detection #128, a51ecdd) | 2 runs | 447.8s, 589.4s |
| 09-21 | 3 runs | 441.6s, 587.7s (FAIL), 600.0s (timeout, run 35606976673) |
| 09-22 | 3 runs | 452.6s, 469.3s, 575.5s |
| 09-23 | 4 runs | 584.9s, 525.3s, 600.0s (timeout, d93b725), 597.3s |

- **Runner variance is large.** 6bdf288 ran at 279.1s against 200.8s for 2ded92a, and the only diff between them is a ci.yml change to the mutation-ts selector. That is ±40% on identical Go code.
- **The step change is at a51ecdd (#128), not d93b725.** The first timeout (09-21, run 35606976673) predates d93b725. The d93b725 timeout is variance on a package that already sat around 525–597s. The changes.json lead is ruled out as the cause.
- **The tests running at each timeout were incidental.** They were TestTechdebtDrossTreeNoFixtureFindings (4s in) and TestNoBodySinkReadsARecordedField (0s in), i.e. whatever happened to be running at 600s.

**What #128 added.** Three tests in `internal/cmd/secretscan_selfscan_test.go`:
- TestDrossTreeHasNoUnmarkedHits (:116) runs `secretscan.Scan` over all 1883 tracked files (23.6 MB).
- TestSelfScanIsNotVacuous (:125) repeats that same full walk.
- TestValidateOnDrossItself (:236) clones the repo and runs Validate twice. Each run goes through `scanDrossArtifacts` (`internal/cmd/secretscan.go:29`) over 1001 .dross files (14.9 MB).

**Measured cost.** I measured with a standalone harness in my scratchpad: a copy of `internal/secretscan`, not the suite.

| Scope | Without -race | With -race |
|---|---|---|
| One full-tree scan | 3.4s | 73.7s (22× slower) |
| .dross only | 2.1s | 45.7s |

- The four scans add up to about 240s under -race on this laptop. That matches the +250–390s CI step.
- Per-rule split under -race, from a sibling planner's harness in the shared scratchpad:

  | Rule | Seconds |
  |---|---|
  | key-context | 31.5 |
  | aws | 6.2 |
  | slack | 6.2 |
  | github | 6.0 |
  | gitlab | 6.0 |
  | atlassian | 5.9 |
  | sk | 5.9 |
  | authorization | 5.0 |
  | pem | 0.4 |

- Instrumented stdlib regexp is the cost. `bufio.ReadBytes` over the whole tree under -race takes only 0.1s.

**Gate experiment.** A line-level any-literal prefilter cut the -race full-tree scan from 74s to 15s. Only 12,327 of 468,852 lines (2.6%) pass it, but they hold 4.4 MB (19% of bytes), which is why the plan uses per-rule gating.

**Equivalence trap.** Go's `(?i)` matches U+017F (long s) for `s` and U+212A (Kelvin sign) for `k`. An ASCII-lowercase gate would silently narrow key-context and authorization-header, so t-1's property test includes those spellings.

**Why the cost keeps regrowing.** It scales with tracked-tree bytes, and .dross grows every phase (panel notes, verification.md). That growth is why the 300s warning in `timeout_ceiling` matters.

**Residual risk to c-1.** The projection adds three parts:
- the pre-#128 baseline of 176–279s;
- the scan cost left after t-1 (about 15–60s on CI, estimated);
- unmeasured additions from #129–#131: `test_wait_test.go`, `verify_lock_test.go`, `boundary_test.go`, `dependabot_config_test.go`.

That puts internal/cmd at roughly 230–320s, so t-1 alone may not clear 300s. t-2's summary will name what is left.

**c-5.** t-1 barely moves the non-race laptop run, because scans take 3.4s each without -race. The laptop's roughly 600s "hang" matches go test's 10m default under memory pressure.

**No parallel tests.** None of the 286 `internal/cmd` test files uses `t.Parallel`, so the package runs strictly serially on a 4-vCPU runner. That lever is untaken here: chdir/env tests make it a big-bang change.

**ci.yml audit against `reference_ci_supply_chain_hardening.md`.** No findings:
- every `uses:` is SHA-pinned and carries a version comment;
- `permissions: contents: read`;
- GOFLAGS, GOPROXY and GOSUMDB are set;
- govulncheck is pinned at v1.8.0;
- `npm ci --ignore-scripts`, an exact Node pin, and `npm audit --audit-level=high`.

t-2 adds no action and no install. The tool is in-repo and runs via `go run` under `-mod=readonly`.

**Pipeline-optimisation reference.** Step 0.3 (auto-cancel superseded runs) is c-4. Items 10 (fold shellcheck) and 11 (scope govulncheck) are already deferred in spec.toml with measurements.
