# Plan Review — cmd-test-duration

Reviewed: 2026-09-23
Plan: 7 tasks across 3 waves

## BLOCKING

(none)

Coverage is complete: c-1 → t-1, t-2, t-7; c-2 → t-5, t-7; c-3 → t-3, t-5, t-6;
c-4 → t-4; c-5 → t-1, t-2, t-5 (but see the c-5 flag). No task contradicts a
locked decision. t-1's walk-once cache still reads the real tracked tree for both
tests (coverage_preserved), and t-6's checker rejects -short/-skip/-run and any
loss of -race/-count=1/./... in the test job. t-5 keeps the >300s signal a
non-failing `::warning` and t-7 drops -timeout entirely (timeout_ceiling). No
tee or upload of the -json stream (timing_surface). TestStdlibOnly pins
timing_tooling. t-4's run_id arm keeps main out of any shared group
(concurrency_scope). rules.toml r-01 is honoured by t-2's `make install` note.
There is no global `~/.claude/dross/rules.toml`. Every missing file the plan names
(prefilter_test.go, cmd/testsummary/*, ci_concurrency_test.go,
ci_go_test_step_test.go) is created by the task that lists it. Every existing
helper or test it names (hitCorpus, randomLine, benignCorpus, splitYAMLComment,
readRepoFile, TestSetupGoStepProblems, FindOpenPRByHead, the unedited secretscan
and techdebt tests) exists.

## FLAG

- [files] t-1's prefilter test can't live in the one test file its `files` lists.
  FuzzPrefilterMatchesRegex gets its seeds from hitCorpus(), randomLine() and
  benignCorpus(). Those are declared in internal/secretscan/secretscan_test.go
  under `package secretscan_test`, the external test package. The same fuzz
  test also needs the unexported candidates() bitmask and needle set, and so do
  TestCandidatesPrunesNeedleFreeLines, TestNoNeedlesFailsOpen (a synthetic Rule
  with nil needles, which are an unexported field) and TestEveryRuleHasNeedles.
  Those identifiers are only visible from `package secretscan`. An
  internal-package file can't see the external package's helpers, and an
  external-package file can't see unexported identifiers. Bridging the two needs
  a file t-1 doesn't list: an export_test.go in package secretscan that
  re-exports candidates and a way to build a Rule with needles, or a move of the
  corpus helpers (an edit to secretscan_test.go).
  Suggestion: add the bridging file to t-1's files (e.g.
  internal/secretscan/export_test.go) and name the package that prefilter_test.go
  declares. That way the executor isn't left to choose mid-task. Copying the
  corpus into prefilter_test.go would split the seed set the differential test
  depends on.

- [granularity] t-1 combines two independent mechanisms. (a) A prefilter in
  production security-gate code: secretscan backs the validate, ship and forge
  publish gates, so a prefilter false negative lets a real credential through.
  (b) A test-only walk-once cache in internal/cmd. The task spans two packages,
  carries nine contract items, and has one CI-evidence line that can't show
  which mechanism saved which seconds. Either change can land or be reverted
  without the other.
  Suggestion: split into a prefilter task (rules.go, secretscan.go,
  prefilter_test.go plus the bridging file) and a self-scan cache task
  (secretscan_selfscan_test.go), both in wave 1.

- [test-contract] t-4's CI-evidence item can't be met with the plan's own
  sequencing. It expects `gh run list --branch phase/cmd-test-duration` to show
  "every superseded pull_request run `cancelled` and only the newest
  completed". t-7's gate needs the draft-PR run to *complete*, because its step
  summary is read before t-7 is queued. t-7's push then supersedes that run,
  which shows as `completed`, not `cancelled`. The same happens for any two
  pushes spaced further apart than one pipeline run.
  Suggestion: reword it as "every pull_request run still in progress when a
  newer push arrived is `cancelled`" and check it against ship's pushes, which
  are what c-4 is about. Keep the existing "no main push run is `cancelled`"
  check.

- [coverage] c-5 is covered in name only. No contract item in any task names the
  laptop measurement (`go test -count=1 ./internal/cmd` finishing within go
  test's 10m default). t-5 lists c-5 because the table prints to stdout when
  GITHUB_STEP_SUMMARY is unset, "the c-5 laptop run". But c-5's command has no
  -json and no pipe, so the summarizer never runs on it. t-1's
  "internal/cmd tests gate on `dross test` (remote), never a local full run"
  reads as forbidding the exact run c-5 asks for. The project's own history also
  shows internal/cmd reaching about 600s locally under memory pressure, which
  makes this the evidence most likely to flake at verify.
  Suggestion: add a "[verify evidence]" item to t-1 or t-2. It should name the
  command and the 10m bound, and say this is a one-off measurement exempt from
  the remote-only gating rule. Drop c-5 from t-5's covers.

- [test-contract] t-3's verdict contract doesn't handle the `skip` terminal
  action. Checked against go1.27.1: a package with no test files ends with
  `"Action":"skip"`, not pass or fail. Every package has tests today, so nothing
  fails yet. But with `set -o pipefail`, the reducer's own exit status can fail
  CI even when go test passes. A reducer that only treats pass and fail as
  terminal would turn the first package without tests (a new cmd/ helper, say)
  into a red "cut mid-package" run. TestReducerVerdict only lists all-pass, empty
  and cut-mid-package streams.
  Suggestion: add a package with no test files to the real-toolchain fixture
  module, or to the verdict cases, and assert exit 0.

- [antipattern] t-7's gate opens a draft PR by hand, and ship is expected to
  "adopt" it later through FindOpenPRByHead. internal/cmd/ship.go:371-406 shows
  that adopting an existing PR skips OpenPR completely. So ship's generated
  title and .dross-filtered body, its reviewer requests and draft=false are never
  applied. The PR stays a draft with whatever body it was opened with, and no
  step in the plan un-drafts it before merge.
  Suggestion: say in t-7's gate how the draft PR is opened (with the title and
  body ship would generate). Also say who runs `gh pr ready` and updates the
  body after ship adopts it. Otherwise, record this as a ship gap.

## NOTE

- [test-contract] t-1's cache: suppose the cached helper calls t.Skip or t.Fatal
  inside sync.Once.Do. The Once still counts as done after the Goexit, so every
  later caller reads a zero-value cache. For example, if TestSelfScanIsNotVacuous
  skips or fails inside the walk, TestDrossTreeHasNoUnmarkedHits then passes
  over zero files, and the same holds the other way round. The first caller
  still reports, and .git is synced to the remote runner, so the practical risk
  is low. Storing the walk's error or skip reason and raising it again in each
  caller keeps both tests honest. TestSelfScanWalksOnce doesn't test this path.

- [test-contract] t-1's note "remove its mustWrite and it must see 0 hits, not 1"
  describes the mutant wrongly. Without the write, scanTracked calls t.Fatalf on
  the read error. The test still fails, but not for the stated reason.

- [test-contract] t-1's and t-2's CI-evidence items accept "≤15s / ≤5s *or*
  absent from the top 20". The table is a global top 20 across all packages. If
  the run's 20th-slowest test is over the bound, absence doesn't prove the
  bound. This is weak but acceptable, because c-1 is judged on the package total.

- [budget] There is little headroom. At the CI-evidence ceilings (3×15s for the
  secretscan tests plus 2×5s for the techdebt tests, 55s in total) on top of the
  ~204s baseline from before a51ecdd, internal/cmd lands near 260s. The plan
  itself cites 1.39x runner variance, which puts the same code anywhere up to
  about 360s. So t-7's 270–300s "ask the user first" band and its >300s
  `dross task add` fallback are a likely outcome, not an edge case. On slow
  runners, the non-failing 300s `::warning` may also fire on unchanged code.

- [wave-order] t-6's CI-evidence item ("Summary tab shows the top-20 table and
  package totals") needs t-5, which isn't in t-6's depends_on. In practice this
  changes nothing, because both land before the gate push. It does mean t-6
  can't be signed off on its own evidence.

- [mutation cost] cmd/testsummary's real-toolchain fixture compiles a module and
  runs a test that waits out a ≥1s timeout, once per test binary. If gremlins
  runs the package's tests for every mutant, that cost is paid per mutant. The
  remote mutation leg already takes about 1h40m against a reap at about 1h48m,
  so plan time for it (run verify detached).

- [strength] t-1's case-folding analysis is correct and precise. U+017F and
  U+212A are the only non-ASCII simple folds of ASCII letters that Go's (?i)
  honours. Triggering the fallback on those exact byte sequences, rather than on
  "any non-ASCII", keeps the prefilter working on .dross prose, which is full of
  em-dashes. The differential fuzz test also names the specific mutants it kills:
  dropping ASIA, dropping the `token` needle, checking the un-folded line, and
  removing the fallback.

- [strength] The gating is done right. t-6 keeps `-timeout 20m` until the gate.
  t-7 is queued only after the draft-PR run's step summary has been read in a
  separate turn, with explicit branches for ≤300s, 270–300s and >300s. t-6's
  goTestStepProblems makes coverage_preserved a checked property: -race,
  -count=1 and ./... are required, and -short/-skip/-run are rejected. t-4's
  run_id arm covers the fact that GitHub replaces a pending run in a shared
  group even without cancel-in-progress.

- [strength] t-3 tests the reducer against real toolchain output (a timeout panic
  and a build failure) rather than hand-written JSON. It also writes its fixtures
  inline into t.TempDir(). That matters because the internal/cmd audit sweeps
  (subprocargs, execconsent) walk cmd/ and parse every non-test .go file, and
  subprocargs fails the test on a parse error. Nested `go` resolves to the
  running go1.27.1 toolchain even when an older go is first on PATH (checked),
  so GOTOOLCHAIN=local in the harness is safe.

## Summary

The plan is sound and well researched, with nothing blocking. Before execution,
fix t-1's missing test file across the package boundary and t-4's CI-evidence
wording, which can't be met as written. Also decide how c-5's laptop run is
evidenced and how the hand-opened draft PR is un-drafted.
