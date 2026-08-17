# Plan draft — risk lens

Phase mutation-diff-scope — 8 tasks across 5 waves

The failure this phase exists to kill is *false attribution*. The failure this
phase can **create** is worse: a scope set that matches nothing, so every
survivor is filtered out and every phase passes vacuously. Confirmed live in
this repo — `reports/gremlins/internal_changes.json` reports `file_name` as
`"changes.go"`, a bare basename, while `changes.json` and `git diff` speak
`internal/changes/changes.go`. A naive matcher scores 0 in-scope mutants on
*every* Go phase. That risk gets an owner (t-1), a distinct status (t-5) and an
end-to-end regression (t-8).

Wave 1
  t-1  Attribute every mutant to its file
       files:    internal/mutation/adapter.go, internal/mutation/gremlins.go,
                 internal/mutation/stryker.go, internal/mutation/gremlins_test.go,
                 internal/mutation/stryker_test.go
       covers:   c-2, c-4
       desc:     Add `Report.ByFile map[string]FileStat` (killed/survived/not-covered/
                 timeout/errors per file), populated in ParseGremlinsJSON and
                 ParseStrykerJSON and merged by mergeInto. Gremlins re-prefixes its
                 bare `file_name` with the package dir it invoked; Stryker's
                 rePrefixFiles re-prefixes ByFile keys as well as Surviving[].File.
       contract: - if ByFile isn't populated per status, TestGremlinsByFileCounts fails:
                   a report with KILLED+LIVED in file1.go and NOT COVERED in file2.go
                   yields ByFile[file1]={Killed:1,Survived:1} and
                   ByFile[file2]={Survived:1,NotCovered:1}, and the ByFile column sums
                   equal Report.Killed/Survived/NotCovered/Timeout
                 - if gremlins' bare `file_name` isn't re-prefixed with the invoked
                   package, TestGremlinsPathsAreRepoRelative fails: running `./internal/
                   changes` over a report naming `changes.go` must surface
                   `internal/changes/changes.go` in both Surviving[].File and ByFile
                   keys (the checked-in reports/gremlins/*.json carry the bare name)
                 - if per-package reports are merged by basename, TestGremlinsMerge
                   KeepsPackagesDistinct fails: `./a` and `./b` each reporting `x.go`
                   must yield two ByFile keys, not one summed entry
                 - if Stryker's Workdir re-prefix misses ByFile,
                   TestStrykerRePrefixCoversByFile fails: Workdir="web" must turn key
                   `src/a.ts` into `web/src/a.ts`, matching its Surviving entry

  t-2  Add pure scope model and hunk parser
       files:    internal/verify/scope.go, internal/verify/scope_test.go
       covers:   c-6, c-7
       desc:     New `verify.Scope`: normalised file set, per-file changed line ranges,
                 and a Sources/Degraded record of where the set came from, plus
                 Contains(file) and InHunk(file,line). Includes path normalisation and a
                 `@@` hunk-header parser. Pure — no git, no I/O.
       contract: - if normalisation drops a form, TestScopeNormalizeForms fails:
                   `./internal/a.go`, `internal/a.go`, `<root>/internal/a.go` and
                   `internal\a.go` all resolve to one key and Contains() is true for each
                 - if the hunk parser mis-reads counts, TestParseHunkRanges fails:
                   `@@ -1,0 +5 @@` → [5,5]; `@@ -3,2 +7,3 @@` → [7,9]; `@@ -4,2 +6,0 @@`
                   (pure deletion) contributes no range
                 - if a malformed hunk header aborts the parse,
                   TestParseHunkGarbageDegrades fails: the file keeps ranges parsed so
                   far and gains a Degraded entry naming it, instead of erroring out
                 - if the union isn't fail-open and deduped, TestScopeUnion fails:
                   git-side [a.go] ∪ changes-side [a.go, b.go] = [a.go, b.go]; an empty
                   git side still yields the changes-side files plus a Degraded entry
                 - if a path escaping the repo is silently accepted,
                   TestScopeRejectsEscapingPath fails: a changes.json entry of
                   `../../etc/passwd` or `/etc/passwd` is recorded as Degraded and never
                   becomes a Contains() match

Wave 2 (depends t-1, t-2)
  t-3  Derive the phase scope from git
       files:    internal/cmd/verifyscope.go, internal/cmd/verifyscope_test.go
       covers:   c-6
       depends:  t-2
       desc:     Build a verify.Scope for a phase: merge-base(changes.json `base`, HEAD)
                 → `git diff --name-only --no-renames` for files and `git diff -U0` for
                 hunks, unioned with changes.json's per-task files. Every git failure
                 degrades to the changes.json side with a recorded reason.
       contract: - if a missing/unknown base aborts or empties the scope,
                   TestScopeFallsBackToChanges fails: a phase whose changes.json has no
                   `base` still yields the recorded task files, returns no error, and
                   carries a Degraded entry naming the missing source
                 - if rename detection collapses a path,
                   TestScopeKeepsBothRenameSides fails: a.go→b.go renamed in the phase
                   still puts a.go in scope (--no-renames emits both sides)
                 - if git's quoted output isn't unquoted,
                   TestScopeUnquotesGitPaths fails: a committed file named
                   `internal/naïve file.go` comes back as that path and matches a mutant
                   reported under it
                 - if changes.json's `base` reaches git unfenced,
                   TestScopeRefusesOptionLikeBase fails: base `--output=<tmp>/pwned`
                   must leave that file non-existent (the gitArgvTap audit also fails if
                   the diff refs precede --end-of-options)
                 - if hunks aren't collected, TestScopeRecordsHunkRanges fails: a phase
                   commit editing lines 10-12 of a.go yields Hunks["a.go"] = [{10,12}]

  t-4  Filter mutation reports to in-scope mutants
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-1, c-2, c-4, c-7
       depends:  t-1, t-2
       desc:     verify.Run takes a Scope and splits every adapter Report: counts and
                 Score are recomputed from ByFile entries whose file is in scope,
                 out-of-scope survivors are collected aside, and each in-scope survivor
                 is tagged in-hunk or inherited.
       contract: - if filtering doesn't apply, TestScopedReportDropsUntouchedFile fails:
                   a gremlins report with a survivor in touched.go and one in
                   untouched.go of the same package leaves exactly one entry in
                   Surviving and the other in the out-of-scope list
                 - if the denominator isn't recomputed,
                   TestScopedScoreIgnoresUntouchedMutants fails: killed=1 survived=1 in
                   touched.go plus killed=9 in untouched.go must give score 0.50,
                   killed=1, survived=1 — not 0.91/10/1
                 - if scoping is adapter-conditional,
                   TestScopingAppliesToEveryAdapter fails: the same in/out split holds
                   for a stryker leg and a gremlins leg inside one Tests run, including
                   the file-granular leg whose report was already narrow
                 - if the hunk tag is wrong, TestSurvivorHunkTag fails: a survivor on a
                   line inside Hunks[file] is tagged `in-hunk`, one in the same file
                   outside every hunk is tagged `inherited`, and BOTH stay in the
                   denominator and in Surviving
                 - if a failed adapter leg loses its scope treatment,
                   TestScopedRunKeepsFailedLeg fails: a leg with Error set and no report
                   still appears in Languages and doesn't nil-panic the filter

Wave 3 (depends t-4)
  t-5  Persist out-of-scope survivors and scope status
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-3, c-5, c-6
       depends:  t-4
       desc:     tests.json gains `out_of_scope` (file/line/op per filtered survivor)
                 and `scope` (the exact file set, sources, degraded reasons). Skeleton
                 emits one NOTE finding carrying the filtered count, records
                 mutants_in_scope in the summary, and sets a new MutationOutOfScope
                 status when every mutant landed in untouched files.
       contract: - if filtered survivors are discarded instead of recorded,
                   TestOutOfScopePersisted fails: tests.json round-trips one
                   {file,line,op} entry per filtered survivor under `out_of_scope`, and
                   none of them appear under languages[].mutation.surviving
                 - if the NOTE is missing or emitted per mutant,
                   TestOutOfScopeNoteIsSingleLine fails: 7 filtered survivors produce
                   exactly one NOTE finding carrying the count 7 and zero FLAG findings
                 - if an empty in-scope set reads as a measured 0.00,
                   TestEmptyInScopeStatus fails: a run where every mutant is out of
                   scope sets mutation_status="out-of-scope" (not measured, not
                   unmeasurable) with mutants_in_scope=0
                 - if the scope record isn't written, TestScopeRecordedInTests fails:
                   tests.json carries the scoped file list and every Degraded reason, so
                   a stale-changes run is diagnosable after the fact
                 - if the in-scope count isn't surfaced,
                   TestSummaryCarriesInScopeCount fails: verify.toml's [summary] has
                   mutants_in_scope=3 for a 3-mutant in-scope run (the sample-size
                   signal the small_denominator_gate decision requires)

Wave 4 (depends t-3, t-5)
  t-6  Wire diff scoping into dross verify
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go
       covers:   c-3, c-5, c-6
       depends:  t-3, t-5
       desc:     `dross verify` builds the scope before running adapters, feeds the
                 union to the adapters and to verify.Run, prints in-scope / filtered /
                 degraded lines, and tags the telemetry outcome with the scope status.
       contract: - if the union isn't the adapter input, TestVerifyMutatesGitOnlyFile
                   fails: a file changed in git but absent from changes.json is still
                   handed to the adapter and its survivors still gate
                 - if the summary hides scoping, TestVerifySummaryShowsScope fails:
                   output carries the in-scope mutant count and "filtered N out-of-scope
                   survivors", plus a degraded-source line when the base was unresolvable
                 - if a git-deleted path is handed to a mutation tool,
                   TestVerifySkipsDeletedScopeFiles fails: the path stays in the scope
                   set for filtering but never reaches the adapter's file list
                 - if the new status isn't tagged, TestVerifyTelemetryScopeTag fails:
                   the outcome event's mutation_status tag reads "out-of-scope" on an
                   all-filtered run

  t-7  Truth-pass the verify prompt and README
       files:    assets/prompts/verify.md, README.md,
                 internal/cmd/verify_prompt_test.go
       covers:   c-3, c-5
       depends:  t-5
       desc:     Verdict rules gain the out-of-scope status (criterion coverage alone,
                 NOTE recorded), the in-scope mutant count in the §4 report block as the
                 small-sample signal, and the instruction that the survivor cross-check
                 reads in-scope survivors only. README's verify row states the scoping
                 guarantee. Prompt edits need `make install` (rule r-01).
       contract: - if the prompt still lists three statuses,
                   TestVerifyPromptCoversOutOfScope fails: verify.md must name
                   "out-of-scope" in both the status list and the verdict branch, and
                   must not apply the 0.80/0.60 thresholds to it
                 - if sample size isn't surfaced,
                   TestVerifyPromptShowsInScopeCount fails: the §4 report block must
                   print the in-scope mutant count next to the score
                 - if the docs still claim package-wide attribution,
                   TestVerifyDocsStateScopingGuarantee fails: README.md's `dross verify`
                   row must say the score covers only the phase's changed files

Wave 5 (depends t-6)
  t-8  Regression: untouched survivor never gates
       files:    internal/cmd/verify_scoping_test.go
       covers:   c-1, c-3, c-5
       depends:  t-6
       desc:     End-to-end through `dross verify` over a real temp git repo with a stub
                 adapter: mutants in a touched file and an untouched file of the same
                 package. Asserts the survivor set, the score, the out-of-scope list,
                 and that a wholesale path mismatch surfaces as out-of-scope rather than
                 a clean pass.
       contract: - if attribution regresses, TestVerifyEndToEndScoping fails: a survivor
                   in the untouched sibling file appears only under out_of_scope in
                   tests.json and produces no FLAG finding in verify.toml, while the
                   touched file's survivor produces one
                 - if the vacuous-pass mode reappears,
                   TestVerifyAllFilteredIsNotAPass fails: an adapter whose report paths
                   match nothing in scope yields mutation_status="out-of-scope" and a
                   NOTE naming the count — never verdict inputs indistinguishable from a
                   clean measured run
                 - if scoping is made conditional, TestVerifyHasNoScopeOptOut fails: no
                   flag on `dross verify` disables it (scoping_always_on lock)

## Coverage

| criterion | tasks |
|---|---|
| c-1 (survivors only from changed files) | t-4, t-8 |
| c-2 (score from in-scope mutants only)  | t-1, t-4 |
| c-3 (out-of-scope reported, not dropped)| t-5, t-6, t-7, t-8 |
| c-4 (adapter-agnostic)                  | t-1, t-4 |
| c-5 (empty in-scope set has a status)   | t-5, t-6, t-7, t-8 |
| c-6 (scoped file set recorded)          | t-2, t-3, t-5, t-6 |
| c-7 (in-hunk vs inherited tag)          | t-2, t-4 |

Every criterion has at least one task; 7/7 covered.

## Judgment calls

- **Per-file counts before anything else (t-1).** `Report` today carries file
  identity only on `Surviving`; `Killed`/`Timeout` are bare ints. Rejected:
  filtering only the survivor list and leaving the score alone — that satisfies
  c-1 and silently fails c-2, which is the criterion that changes verdicts.
- **Gremlins path re-prefix is in scope and is task one.** Rejected: treating
  path shape as an implementation detail of the filter. Verified against
  `reports/gremlins/internal_changes.json`: `file_name` is `changes.go`, not
  `internal/changes/changes.go`. Without the re-prefix, correct filter code
  scopes out 100% of Go mutants and the phase ships a vacuous gate.
- **Git derivation in `internal/cmd`, matching in `internal/verify` (t-2/t-3).**
  Rejected: one `internal/scope` package owning both. `internal/cmd` already has
  the fenced git helpers and the `gitArgvTap` argv audit that a config-derived
  `base` must pass through; splitting keeps the matcher pure and unit-testable
  with no repo on disk.
- **Hunk *parsing* (t-2) split from hunk *collection* (t-3).** The `@@` header
  arithmetic is where off-by-ones live and it needs literal-string tests, not a
  git fixture.
- **`out-of-scope` is a fourth `mutation_status`, not a reuse of
  `unmeasurable`.** Rejected: folding it in. They have opposite causes — nothing
  to measure vs. plenty measured, none of it yours — and c-5 demands an explicit
  status. Cost: three consumers (Skeleton, printVerifySummary, verify.md verdict
  branch) must learn it; `mutation_status` is not in `configenum`, so there is no
  enum-divergence gate to catch a missed one, which is why t-7 is a task rather
  than a cleanup.
- **Out-of-scope survivors get one NOTE, not N FLAGs.** Follows the
  `out_of_scope_surface` lock; the machine-readable list lives in tests.json for
  the survivor-lifecycle phase. Note the tension with rule r-02 (pre-existing
  faults are not furniture): these are recorded, counted and handed to a named
  successor phase, not re-listed inertly.
- **Adapter input becomes the union, not just changes.json (t-6).** The
  `scope_source` lock defines the change set as the union; letting the adapter
  input stay narrower would leave a git-changed file un-mutated and therefore
  un-gated — scoping in reverse.
- **Deleted files stay in scope but leave the adapter list (t-6).** Passing a
  deleted path to `stryker --mutate` fails the leg; keeping it in the scope set
  costs nothing since a deleted file has no mutants.
- **A dedicated end-to-end task (t-8) despite per-unit contracts.** Each wave-1–4
  task tests its own layer with the layers below stubbed; the failure mode that
  destroys the gate lives in the seam between them (path shape × union × filter).
  One test that runs the real command over a real git repo is the only thing that
  observes it.
- **t-1 touches five files.** Three sources plus their two tests, one package,
  one layer. Splitting the gremlins re-prefix out would put two tasks in the same
  wave editing `gremlins.go`; sequencing them into separate waves would be worse.
