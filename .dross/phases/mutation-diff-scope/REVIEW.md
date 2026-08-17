# Plan Review — mutation-diff-scope

Reviewed: 2026-08-09
Plan: 8 tasks across 5 waves

Prior-review fixes verified present in the current file, individually:
t-7 has `depends_on = ["t-5", "t-6"]` and `wave = 5`; t-4's description carries an
explicit compile seam clause plus the "t-4 and t-5 must each compile and pass
`go test ./...` standalone" statement; t-1 lists `stryker_net.go` /
`stryker_net_test.go` and has the repo-relative path-shape contract; t-6 has the
"empty changes.json no longer short-circuits" contract; t-5's FLAG assertion is
count-scoped ("exactly ONE FLAG — the in-scope survivor's"). All five hold.

Coverage is complete: c-1 (t-4, t-6, t-8), c-2 (t-1, t-4, t-5), c-3 (t-5, t-6,
t-7, t-8), c-4 (t-1, t-4), c-5 (t-5, t-6, t-7, t-8), c-6 (t-2, t-3, t-5, t-6),
c-7 (t-2, t-4, t-7). No locked-decision conflicts found across all five locks.

## BLOCKING

- [antipatterns — nonexistent file] t-1's gremlins contract mandates "the fixture
  is a checked-in `reports/gremlins/*.json`". `/reports` is line 3 of `.gitignore`
  and `git ls-files reports` is empty — nothing under it is tracked. That directory
  is local mutation-run output that dross itself writes at
  `internal/mutation/gremlins.go:106`. A test reading it passes only on a machine
  that has already run gremlins against that package, and its contents change
  every run. The existing convention in `internal/mutation/gremlins_test.go:12` is
  an inline `fixtureGremlins` const (its comment cites a `testdata/normal_output.json`
  that also does not exist — `internal/mutation/testdata/` is absent).
  Suggestion: copy one real payload into `internal/mutation/testdata/` as a tracked
  file, or inline it as a const, and reword the contract to "a real gremlins payload
  carrying its bare basename" — keep the intent (real tool output, not a
  hand-written repo-relative path), drop the untracked location.

- [test contract] t-6's "telemetry cannot disagree with verify.toml: the outcome
  event's mutation_score is computed from the in-scope killed/survived and the two
  are asserted equal" asserts an equality that does not hold under existing
  arithmetic, and no task in the plan makes it hold. Three separate formulas are
  in play today:
    - `verify.toml`'s `mutation_score` is `combineScore`'s **mean across legs**
      (`internal/verify/verify.go:330`), seeded from each leg's `Report.Score`.
    - `Report.Score` is `killed/(killed+survived+timeout)` — see the implementation
      comment at `internal/mutation/gremlins.go:342` and the calculation at :383.
      The struct doc at `internal/mutation/adapter.go:24` claims
      `killed/(killed+survived)` and is simply wrong.
    - `recordVerifyOutcome` (`internal/cmd/verify.go:293`) computes **pooled**
      `killed/(killed+survived)` over all legs, timeouts excluded.
  So any two-leg run with unequal leg sizes diverges (1/1 and 0/9 → mean 0.50 vs
  pooled 0.10), and even a single-leg run diverges as soon as one mutant times out.
  t-4 and t-5 both push toward multi-leg fixtures ("the same table runs inside a
  multi-leg Tests", "a two-leg run's dropped survivors carry the leg's language"),
  so an executor writing t-6's assertion gets a red and will most likely "fix"
  `combineScore` — silently changing the number every phase's 0.95 / 0.80 / 0.60
  thresholds are applied to. That is a gate-semantics change this phase's spec
  never scopes and the `small_denominator_gate` lock's "no new way to launder a
  survivor" reasoning argues against. The alternative outcome is worse in a
  different way: the executor quietly narrows the fixture to one timeout-free leg
  and the contract catches nothing.
  Suggestion: decide which formula is normative and say so in the task, or scope
  the assertion to the conditions under which it can hold (single leg, zero
  timeouts) and record the mean-vs-pooled and timeout-in-denominator divergence as
  a deferred item against `survivor-lifecycle`. Either way it should be an explicit
  decision, not something discovered at a red test.

## FLAG

- [antipatterns — missing file] c-7's origin tag has nowhere to live.
  `mutation.Mutant` (`internal/mutation/adapter.go:38`) is `File / Line / Op /
  Snippet` — no tag field. t-4 is the task that stamps it ("each kept survivor
  stamped in-hunk or inherited") but its `files` are only
  `internal/verify/verify.go` + `verify_test.go`; t-1, the one task that opens
  `adapter.go`, adds per-file counters to `Report` and says nothing about `Mutant`.
  The field has to land on `Mutant` specifically, not on a verify-side wrapper,
  because kept survivors reach tests.json through `languages[].mutation.surviving`,
  which serialises `[]mutation.Mutant` — and t-7 then instructs the prompt to read
  that tag. Wave order is fine (t-1 wave 1, t-4 wave 2), only the declaration is
  missing.
  Suggestion: add the `Mutant` tag field to t-1's description, or add
  `internal/mutation/adapter.go` to t-4's file list.

- [test contract] t-4 recomputes Score from in-scope per-file rows, but no contract
  pins the denominator. Its concrete-arithmetic contract uses killed=1 / survived=1
  with zero timeouts, where both live conventions give 0.50 — so the choice between
  `killed/(killed+survived)` and `killed/(killed+survived+timeout)` goes untested,
  and a recomputation written from the (wrong) `adapter.go:24` doc comment shifts
  every phase's score without failing anything.
  Suggestion: add a timeout-bearing row to t-4's concrete-arithmetic contract so
  the denominator convention is pinned by a test.

- [antipatterns — unbounded side effect] t-6 widens adapter dispatch to the
  git-derived union, and nothing bounds the bookkeeping files that arrive with it.
  `verify.Run` sends every file with no matching adapter to `Tests.Skipped`
  (`internal/verify/verify.go:148`), and `Skeleton` emits one NOTE finding per
  skipped file (`internal/verify/verify.go:319`). Every dross phase branch's diff
  carries `.dross/phases/<id>/spec.toml`, `plan.toml`, `changes.json`, and at
  verify time `verify.toml` — this branch's own
  `git diff --name-only merge-base..HEAD` already shows two of them. Today the file
  list comes from changes.json, which records source files only (checked two recent
  phases: 9 and 16 entries, all `internal/**`), so `Skipped` is near-empty. After
  t-6 every verify.toml gains ~4-6 standing NOTEs about dross's own bookkeeping.
  Rule r-02 says the standing backlog only ever shrinks.
  Suggestion: add a t-6 contract that git-side non-source paths reach the scope set
  for filtering but do not produce per-file skipped NOTEs — the same shape as the
  "deleted paths" contract already there.

- [granularity] t-1 is 7 files and carries the plan's only genuine unknown. Its own
  contract states the failure mode plainly ("a solution- or project-relative path
  shape would filter every C# mutant out as out-of-scope and score the leg a
  vacuous 0/0") but neither its description nor any other task says what to do if
  the shape turns out wrong. `StrykerNet.Run`
  (`internal/mutation/stryker_net.go:52-98`) has no `Workdir` and never calls
  `rePrefixFiles`; fixing it means a new re-prefix mechanism nobody has budgeted,
  inside a wave-1 task that t-4 — and therefore the entire rest of the plan —
  blocks on. The test seam itself is fine (`strykerNetBuildCmd` is a stubbable var).
  Suggestion: resolve the .NET path shape before execution starts (a single real
  report settles it), or split the stryker-net leg into its own task so an unknown
  there cannot stall the Go and TS path.

- [test contract] t-1's "gremlins path re-prefix" contract points at the wrong
  seam. `ParseGremlinsJSON` takes bytes and no package argument; the package
  identity only exists in the per-package loop in `Gremlins.Run`
  (`internal/mutation/gremlins.go:114-166`). The description's "populated in
  ParseGremlinsJSON ... Gremlins re-prefixes its bare file_name with the package
  dir it was invoked for" blurs the two, and the contract's "parsing a report ...
  while invoked for ./internal/changes" is not something a parser-level unit test
  can express.
  Suggestion: name the seam (Run's loop, or an exported re-prefix helper taking
  pkg) so the test is written where the package dir is actually known.

## NOTE

- [strengths] t-1 is a real prerequisite, not scaffolding for granularity's sake.
  `Report.Surviving` is the only per-mutant record that exists — killed mutants
  have no per-mutant representation anywhere in `mutation.Report`. Without
  per-file counters there is no way to recompute a filtered numerator at all, only
  to prune the surviving list. t-4's "numerator immunity" / "denominator immunity"
  pair is precisely the assertion that catches an implementation that stops at
  pruning. The plan found the load-bearing dependency.

- [strengths] The contracts are unusually specific and assert counts rather than
  presence: "exactly ONE NOTE carrying the count 7, and exactly ONE FLAG ... assert
  the counts, not presence, so a per-survivor loop over the unfiltered set fails
  with 8"; "score 0.50 / killed=1 / survived=1 — not 0.91 / 10 / 1"; "NOT
  'measured' with 0.00, NOT 'unmeasurable'". Several name the exact wrong
  implementation they are built to fail. Nothing in the file matches the vague
  "tests pass" pattern.

- [wave order] The wave graph is minimal — every wave-N+1 task strictly needs a
  wave-N output, and no task could be pulled a wave earlier. t-3 (wave 2) needs
  t-2's Scope type; t-4 (wave 2) needs t-1's per-file rows and t-2's Contains;
  t-5 (wave 3) needs t-4's filter; t-6 (wave 4) needs t-3's git derivation and
  t-5's persisted shape; t-7 and t-8 (wave 5) both need t-6's wiring.

- [granularity] t-4 and t-5 edit the same two files in consecutive waves. This is a
  real split, not an inflated one — each carries 9-10 distinct contracts, and t-4's
  in-memory filter is independently testable before t-5 gives it a schema. No
  action.

- [coverage] t-8's end-to-end regression uses a Go stub adapter only, so c-4's
  cross-adapter guarantee rests entirely on t-4's table test. Defensible — a real
  stryker or dotnet run in a unit test is not on — but the file-granular leg is
  never proven through `dross verify` itself.

- [truth-pass scope] `assets/prompts/verify.md:88` says `mutation_status` comes
  "from tests.json"; it is written to verify.toml (`internal/verify/verify.go:97`),
  not tests.json. t-7 is already editing that exact line to add the fourth status
  and could correct the source attribution in the same edit.
