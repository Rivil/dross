# Plan draft — verification lens

Lens: each criterion's ideal test contract was written first; the task is the
smallest change that makes that contract satisfiable and failable.

```
Phase mutation-diff-scope — 8 tasks across 5 waves

Wave 1
  t-1  Attribute every mutant to its file
       files:    internal/mutation/adapter.go,
                 internal/mutation/gremlins.go,
                 internal/mutation/stryker.go,
                 internal/mutation/gremlins_test.go,
                 internal/mutation/stryker_test.go
       covers:   c-2, c-4
       desc:     Add Report.Files []FileStats{File,Killed,Survived,Timeout,Errors,NotCovered}.
                 Populate it in ParseGremlinsJSON and ParseStrykerJSON (the latter also
                 serves stryker-net), preserve per-file rows through mergeInto and
                 Stryker.rePrefixFiles.
       contract: - ParseGremlinsJSON over a fixture with KILLED in a.go and LIVED in b.go
                   yields two FileStats rows ({a.go killed=1}, {b.go survived=1}); if
                   killed counts stay aggregate-only, the per-file killed assertion fails.
                 - ParseStrykerJSON over the equivalent stryker-schema fixture yields the
                   same two rows — the Killed-only-aggregate regression fails on both parsers.
                 - mergeInto of two per-package gremlins reports that both contain a row
                   for internal/x/a.go sums that file's counts into ONE row, and the sum of
                   all FileStats.Killed equals Report.Killed (a drift assertion — an
                   adapter that forgets to fill Files fails here, not silently downstream).
                 - Stryker with Workdir="web" re-prefixes FileStats.File to "web/..." as
                   well as Surviving[].File; a report whose Files rows keep bare paths
                   fails the path-parity assertion against changes.json paths.
       depends:  —

  t-2  Build the phase scope file set
       files:    internal/scope/scope.go, internal/scope/scope_test.go
       covers:   c-6
       desc:     New package. Build(git GitFn, base string, recorded []string) (*Scope, error)
                 returns the UNION of `git diff --name-only merge-base(base,HEAD)..HEAD`
                 and changes.json's recorded files, plus the raw `git diff -U0` text, the
                 resolved merge-base sha, and per-source provenance counts. GitFn is a
                 seam (func(args ...string) (string, error)).
       contract: - Fake git returning "a.go\nb.go", recorded=["c.go"] → Scope.Files is
                   [a.go b.go c.go]. A build that intersects instead of unions returns
                   the empty set and fails TestBuild_UnionFailsOpen.
                 - Fake git whose merge-base call errors → Build still returns
                   Files=recorded with Source="changes-only" and a non-empty Warning; a
                   version that returns (nil, err) fails TestBuild_GitFailureFallsBackToChanges.
                 - recorded=nil and git returning files → Files=git files with
                   Source="git-only" (proves the union is symmetric, not git-gated).
                 - Duplicate across both sources appears exactly once, and Files is
                   sorted — TestBuild_DedupesAndSorts fails on a naive append.
                 - Scope.Base records the resolved merge-base sha, not the branch name:
                   asserting on the fake's `merge-base` stdout catches a build that stores
                   the input ref and makes a stale-base run indistinguishable.
       depends:  —

  t-3  Parse diff hunks and classify origin
       files:    internal/scope/hunks.go, internal/scope/hunks_test.go
       covers:   c-7
       desc:     Pure parser: ParseHunks(unifiedDiff string) map[string][]LineRange over
                 `git diff -U0` output; Origin(hunks, file, line) returns OriginInHunk or
                 OriginProximity. Exports both constants.
       contract: - A two-file -U0 diff with "@@ -10,0 +11,3 @@" yields
                   {b.go: [{11,13}]}; an off-by-one that makes line 13 exclusive fails
                   TestOrigin_LastHunkLineIsInHunk.
                 - Origin for a line in a file present in the map but outside every range
                   returns OriginProximity, NOT OriginInHunk — a classifier that defaults
                   to in-hunk fails TestOrigin_UntouchedLineInTouchedFile.
                 - A pure-deletion hunk ("@@ -5,3 +4,0 @@") contributes no added range, so
                   every mutant in that file classifies as proximity rather than crashing
                   on a zero-length range.
                 - A rename header ("rename to internal/x/b.go") attributes the hunk to the
                   NEW path; keying by the old path fails TestParseHunks_Rename.
       depends:  —

Wave 2 (depends t-1)
  t-4  Filter a normalised Report to in-scope mutants
       files:    internal/mutation/filter.go, internal/mutation/filter_test.go
       covers:   c-1, c-2, c-4, c-7
       desc:     FilterToScope(r *Report, inScope map[string]bool, origin func(string,int) string)
                 returns (kept *Report, dropped *OutOfScope). Recomputes Killed/Survived/
                 Timeout/Errors/NotCovered/Score from in-scope FileStats only, filters
                 Surviving by file, and stamps Mutant.Origin from the callback. dropped
                 carries the out-of-scope survivors (file, line, op) plus their counts.
                 Adds Mutant.Origin string.
       contract: - Report with survivors in a.go (in scope) and b.go (same package,
                   untouched): kept.Surviving contains only a.go; b.go never appears.
                   TestFilter_DropsUntouchedFileSurvivor.
                 - A KILLED mutant in untouched b.go leaves kept.Killed and kept.Score
                   unchanged vs. the same report without it — numerator immunity. A
                   SURVIVED mutant in b.go likewise leaves kept.Score unchanged —
                   denominator immunity. Two assertions, both fail if the filter only
                   prunes the Surviving slice and leaves the counters alone.
                 - Table-driven over a gremlins-parsed and a stryker-parsed report built
                   from the same logical mutant set: both yield identical kept counts and
                   identical dropped counts. A filter that special-cases a tool's path
                   shape fails TestFilter_AdapterAgnostic.
                 - Every mutant in kept.Surviving has non-empty Origin equal to what the
                   callback returned for its (file,line); a survivor with Origin=="" fails
                   TestFilter_EveryKeptSurvivorIsTagged.
                 - dropped.Survivors lists exactly the b.go survivors with file/line/op
                   preserved, and dropped.Count == len(dropped.Survivors) — an
                   implementation that discards them fails on the empty-list assertion.
                 - inScope containing a file with zero mutants does not invent rows; kept
                   with an empty inScope map yields zero counts and Score 0 with
                   dropped holding everything.
       depends:  t-1

Wave 3 (depends t-2, t-4)
  t-5  Record scope and out-of-scope survivors in tests.json
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-3, c-6
       desc:     verify.Run takes a *scope.Scope; each LanguageRun's Report is passed
                 through mutation.FilterToScope before it lands in Tests. Adds
                 Tests.Scope{Base, Files, FromGit, FromChanges, Source, Warning} and
                 LanguageRun.OutOfScope, serialised under their own JSON keys.
       contract: - Run with an adapter returning survivors in a.go and b.go and a Scope
                   listing only a.go: tests.json's languages[0].mutation.surviving has
                   a.go only, and languages[0].out_of_scope lists b.go's survivor with
                   file/line/op. TestRun_OutOfScopeSurvivorsPersistedNotDropped fails if
                   either half is missing.
                 - tests.json round-trips through LoadTests with scope.base, scope.files,
                   scope.from_git, scope.from_changes and scope.source intact — a struct
                   without json tags on the new block fails the reload assertion.
                 - A Scope built from changes.json only (git leg failed) serialises
                   source="changes-only" plus the warning text, so a stale-base run is
                   readable after the fact; TestRun_ScopeWarningPersisted.
                 - Two language legs each get their own out_of_scope list — a merged
                   single list fails TestRun_OutOfScopeIsPerLanguage.
                 - The adapter still receives the unfiltered file list it was dispatched
                   for (filtering is post-Report, not a change to what gets mutated):
                   the fake adapter records its argument and the assertion catches a
                   regression that narrows the tool invocation instead of the attribution.
       depends:  t-2, t-4

Wave 4 (depends t-5)
  t-6  Score, status and NOTE from in-scope mutants only
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-2, c-3, c-5
       desc:     Skeleton sums the FILTERED reports for score/killed/survived/not-covered,
                 adds MutationOutOfScope = "out-of-scope" for a run whose in-scope mutant
                 set is empty while out-of-scope mutants exist, emits one NOTE finding
                 carrying the out-of-scope survivor count, and surfaces
                 Summary.MutantsInScope.
       contract: - Skeleton over a Tests where every mutant landed out of scope sets
                   mutation_status="out-of-scope" — NOT "measured" with 0.00 and NOT
                   "unmeasurable". TestSkeleton_EmptyInScopeGetsOwnStatus asserts all
                   three, so collapsing it into unmeasurable fails.
                 - That same case leaves mutation_score at 0 but records
                   mutants_in_scope=0, so the prompt can tell "nothing of mine was
                   measured" from "my code scored zero".
                 - A measured run with 2 in-scope killed and 5 out-of-scope killed reports
                   mutation_score=1.00 over mutants_in_scope=2 — a Skeleton still reading
                   the package-wide report fails the score assertion with 7/7 vs 2/2.
                 - Exactly ONE NOTE finding is emitted per run for out-of-scope survivors,
                   text carrying the count; a per-survivor FLAG flood fails
                   TestSkeleton_OutOfScopeIsOneNote. Zero out-of-scope survivors emits no
                   such NOTE at all.
                 - No FLAG finding is emitted for any out-of-scope survivor — the
                   existing per-survivor FLAG loop must run over filtered survivors only.
       depends:  t-5

  t-7  Wire scope into `dross verify` and its summary
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go
       covers:   c-1, c-5, c-6
       desc:     verify.go builds the Scope from changes.Base + changes.json via
                 scope.Build (git seam pointed at gitTrim), passes it to verify.Run, and
                 printVerifySummary reports the scoped file count, the in-scope mutant
                 count beside the score, the out-of-scope survivor count, and the
                 out-of-scope status line.
       contract: - Over a real temp git repo (mustGit) with a base commit touching a.go
                   and an untouched b.go in the same dir, `dross verify` output names
                   a.go in the scope line and never names b.go as a survivor.
                   TestVerify_ScopeFromRealRepo.
                 - The printed summary includes "in_scope=<N>" next to score=<X.XX>, so a
                   0.50 over 2 mutants reads as a small sample; a summary printing score
                   alone fails the substring assertion.
                 - mutation_status="out-of-scope" prints its own explanatory line — the
                   existing switch-on-status test table gains a row, and a status with no
                   case falls through silently and fails it.
                 - When changes.json has no Base recorded, verify still runs, prints the
                   scope warning, and does not abort — TestVerify_NoBaseStillRuns.
                 - recordVerifyOutcome's mutation_score number is computed from the
                   in-scope killed/survived, so telemetry and verify.toml cannot disagree;
                   asserting the two are equal catches the duplicated-arithmetic drift.
       depends:  t-5

Wave 5 (depends t-6, t-7)
  t-8  Teach /dross-verify the in-scope contract
       files:    assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-3, c-5, c-7
       desc:     Document the scope block and out_of_scope key in tests.json, add
                 "out-of-scope" to the mutation_status enum and give it a verdict branch,
                 tell the cross-check to read Origin (in-hunk vs proximity) when weighting
                 a survivor, and print in-scope count + filtered count in the §4 report.
                 Run `make install` (rule r-01) before relying on the prompt.
       contract: - verify_prompt_test asserts the prompt names all four statuses
                   (measured|unmeasurable|skipped|out-of-scope); adding the constant in Go
                   without the prompt line fails it.
                 - The prompt's §3 verdict rules contain a branch keyed on "out-of-scope"
                   that does NOT apply the score thresholds — the assertion is on that
                   status appearing in the non-threshold branch, so pasting it into the
                   measured branch fails.
                 - The prompt instructs reading `out_of_scope` from tests.json and forbids
                   re-listing those survivors as phase findings (rule r-02's routing lives
                   with the count NOTE, not per-survivor FLAGs) — asserted by substring on
                   "out_of_scope".
                 - The §4 report template contains "in_scope=" alongside score, keeping the
                   human report and printVerifySummary in agreement.
       depends:  t-6, t-7
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (survivors only from changed files) | t-4, t-7 |
| c-2 (score from in-scope mutants only)  | t-1, t-4, t-6 |
| c-3 (out-of-scope reported, not discarded) | t-4, t-5, t-6, t-8 |
| c-4 (adapter-agnostic)                  | t-1, t-4 |
| c-5 (empty in-scope has explicit status) | t-6, t-7, t-8 |
| c-6 (scoped file set recorded)          | t-2, t-5, t-7 |
| c-7 (in-hunk vs proximity tag)          | t-3, t-4, t-8 |

7/7 criteria covered.

## Judgment calls

- **Per-file counts on `Report` (t-1) rather than filtering at parse time.** Both
  parsers currently increment bare `Killed`/`Timeout` counters with no file
  attribution, so c-2's denominator guarantee is *unsatisfiable* without this.
  Rejected: passing a scope predicate into each parser — that would put the
  filter in three places and make c-4's "same guarantee across adapters" a
  claim rather than a testable property of one function.
- **Filter lives in `internal/mutation`, not `internal/verify`.** c-4 says
  scoping applies to "every configured adapter's normalised Report" — one
  function over `*Report` is the only shape where a single table test proves it
  for all adapters. Rejected: filtering inside `verify.Run`'s per-language loop,
  which is where it would be *called* but not where it can be unit-tested
  adapter-by-adapter.
- **New `internal/scope` package with a `GitFn` seam (t-2).** Rejected: putting
  scope derivation in `internal/cmd` next to the existing `gitTrim`. Every
  union/fallback/dedup contract would then need a real repo; the seam makes the
  fail-open behaviour (git errors → changes.json still scopes) a five-line table
  test, and t-7 still gets one real-repo test for the wiring.
- **Hunk parsing split from scope building (t-3 vs t-2), both wave 1.** They
  share a package but not a dependency: t-2 fetches and stores the raw `-U0`
  text, t-3 parses it. Rejected: one task producing a fully-populated Scope,
  which serialises two independent pure parsers behind one another for no reason.
- **t-4 depends only on t-1, not t-3.** The origin callback is a plain
  `func(string,int) string` and the origin constants live in `internal/scope`, so
  the filter never imports the classifier. Keeps t-3 and t-4 off each other's
  critical path.
- **t-5 and t-6 are separate waves despite both editing `verify.go`.** t-6's
  score/status arithmetic reads the filtered reports and the out-of-scope lists
  that t-5 introduces; running them in parallel means two agents editing the
  same file. Split by dependency, not by file, and t-7 (cmd layer) runs beside
  t-6 because it touches a different file.
- **One NOTE for the out-of-scope count, asserted as exactly one (t-6).** The
  locked `out_of_scope_surface` decision says one NOTE line; the contract asserts
  the *count* of such findings, not just its presence, because the failure mode
  worth pinning is the FLAG flood the current per-survivor loop would produce.
- **No task for a scoping opt-out flag.** `scoping_always_on` is locked; the
  absence is deliberate, and t-7's contract deliberately has no flag assertion.
