# mvp lens — mutation-range-provenance

Phase mutation-range-provenance — 4 tasks across 3 waves

Bias applied: one task per criterion cluster, nothing that a criterion does not
name. c-2 and c-3 are one task because they are one data structure (the leg
record) written by one function; c-1 is the cherry-pick and nothing else;
c-4 and c-5 are each exactly one surface.

## Wave 1

  t-1  Port ranged runner from feat/mutate-changed-lines
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/mutation/range_test.go, internal/mutation/argv_test.go,
                 internal/mutation/stryker_test.go, internal/verify/verify.go,
                 internal/verify/range_dispatch_test.go
       covers:   c-1
       depends:  —
       description:
         `git cherry-pick -n 1da3f73 21e354b` onto the phase branch, then adapt
         to HEAD (30 commits on): in stryker.go checkInstrumented use
         `head.contains(strykerDropWarningText)` (headBuffer moved to
         toolfail.go; `buf` still exists but `contains` is the HEAD idiom);
         in verify.go RunScoped replace `a.Run(byAdapter[name])` with
         `runAdapter(a, byAdapter[name], scope)` while keeping HEAD's
         `mutation.RecordLegError(err)` / `RemoteTransport` error branch.
         Lands: mutation.Range, mutation.RangeRunner, Stryker.RunRanges,
         runArgs(files, ranges), rangeKey, narrowedSet, narrowed-tolerant
         checkInstrumented; verify.hunkContextLines=25 (untouched — pad_constant
         lock), padAndMerge, runAdapter. Plus the 235+209 lines of tests those
         two commits carry. One commit for the task.
       test_contract:
         - TestRunArgsEmitsLineRanges: Stryker.runArgs({"src/a.ts"}, {"src/a.ts":[{10,12}]}) puts `src/a.ts:10-12` in --mutate; a build that ignores `ranges` fails it.
         - TestRunArgsAppendsTheRangeAfterEscaping: a `[id]` route path with a range yields `<escaped-path>:10-12` — if the suffix is glued on before escapeGlobMeta, the bracket expression swallows the colon and this fails.
         - TestRunArgsFallsBackWholesaleWhenAnyRangeIsMalformed: ranges [{1,5},{0,3}] for one file emit the bare path once and NO `:1-5` entry — emitting good ranges beside the fallback fails it.
         - TestRunScopedNarrowsToTheChangedLines / TestRunScopedPassesPaddedRanges: a rangingAdapter given a Scope with hunk 30-31 receives RunRanges with exactly {5,56}; if RunScoped still calls Run, or pads by anything but 25, it fails.
         - TestRunScopedStillRunsAnAdapterThatCannotRange: a plainAdapter (no RunRanges) under a hunked Scope has Run called once — an unchecked type assertion panics here.
         - TestRunScopedWithNoHunksRunsWholeFiles + TestRunScopedWithANilScopeRunsWholeFiles: RunRanges is never called, Run is — the fail-open limb.
         - TestPadAndMergeMergesOverlappingHunks: 10-12 and 40-42 become one range 1-67; TestPadAndMergeKeepsDistantHunksSeparate: 10-12 and 100-102 stay two.
         - TestCheckInstrumentedToleratesANarrowedFileWithNoMutants passes and TestCheckInstrumentedStillRefusesWhenStrykerWarnedItDropped errors — the adapted `head.contains` call is what separates them; reverting to a stale `strings.Contains(head.buf.String())` still passes, but deleting the guard fails the second.
         - TestStrykerImplementsRangeRunner: `var _ mutation.RangeRunner = (*Stryker)(nil)` compiles.
         - `go test -count=1 ./internal/mutation/ ./internal/verify/` green on the phase branch — the criterion's own words.

  t-2  Kill the three compilefence survivors
       files:    internal/compilefence/compilefence_test.go
       covers:   c-4
       depends:  —
       description:
         Add a recordingTB (embeds testing.TB, overrides Fatalf to append the
         formatted message and return, Helper to no-op) and drive
         AssertDoesNotCompile / AssertCompiles through it from compilefence's
         OWN package — today every test here calls build() directly, so :61,
         :80 and :81 read count=0 (the tracked-path-containment FLAG says so;
         not a ceiling, not an accept). Gated behind !testing.Short() like the
         file's other build tests. No `dross survivor accept` — the tests are
         the cheaper and honest disposition.
       test_contract:
         - TestAssertDoesNotCompileRefusesACompilingFixture: AssertDoesNotCompile(rec, importsInternal, "x") records a first Fatalf containing "COMPILED but should not have"; negating `err == nil` at compilefence.go:61 records nothing and fails it (kills :61 CONDITIONALS_NEGATION).
         - TestAssertDoesNotCompileRefusesTheWrongReason: (rec, setsUnknownField, "cannot refer to unexported field") records a Fatalf containing "WRONG reason"; TestAssertDoesNotCompileAcceptsTheRightRefusal: (rec, setsUnexportedField, "cannot refer to unexported field") records zero Fatalfs.
         - TestAssertCompilesRefusesABrokenFixture: AssertCompiles(rec, setsUnexportedField) records a Fatalf containing "compile fence itself is broken"; negating `err != nil` at :80 records nothing and fails it (kills :80 CONDITIONALS_NEGATION; :81's Fatalf line is now executed, so its ARITHMETIC_BASE mutant is compiled and either non-viable or killed by the message assertion).
         - TestAssertCompilesAcceptsACleanFixture: AssertCompiles(rec, importsInternal) records zero Fatalfs.
         - Observed at this phase's verify, not unit-asserted: the gremlins report over internal/compilefence lists none of 365503d33114f65e, 5d74aadbbae679b7, 695a2b44f32b1b43 as survived/not-covered, so `dross survivor drain --phase mutation-range-provenance` no longer counts them outstanding.

## Wave 2 (depends t-1)

  t-3  Record effective ranges and fallbacks per leg
       files:    internal/verify/verify.go, internal/verify/range_dispatch_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_scoping_test.go
       covers:   c-2, c-3
       depends:  t-1
       description:
         verify.go: add `EffectiveRange{Start,End,Pad int}`; on LanguageRun add
         `Ranges map[string][]EffectiveRange` (json "ranges") and
         `WholeFile map[string]string` (json "whole_file") — the range_record_home
         lock. Replace runAdapter's inline map-building with an exported pure
         planner `LegProvenance(a mutation.Adapter, files []string, scope *Scope)
         (toolRanges map[string][]mutation.Range, ranges map[string][]EffectiveRange,
         whole map[string]string)` that names four reasons as constants
         (ReasonAdapterCannotRange "adapter has no range runner",
         ReasonScopeHasNoHunks "scope has no hunks", ReasonFileAbsentFromHunks
         "file absent from hunks", ReasonMalformedRange "malformed range %d-%d")
         and applies the fallback_severity lock: malformed range / hunk-less
         scope on a range-capable adapter also append to scope.Degraded; the
         other two do not. Raw hunks are validated BEFORE padAndMerge so a
         malformed hunk never reaches the tool (stryker's own runArgs fallback
         stays as the belt). RunScoped stamps Ranges/WholeFile on the leg in
         BOTH the success and error branches (provenance is known before the
         tool runs). LegSummary gains `Pad int` (toml "pad"), `Ranges []string`
         ("file:start-end", sorted) and `WholeFile []string` ("file — reason",
         sorted); Skeleton fills them for every leg including error legs.
         cmd/verify.go: the detached results path builds its gremlins leg
         through a new `detachedLeg(files, scope, host, kept)` helper that
         calls LegProvenance; printScopeSummary prints, per leg, one
         `  ranged <tool> <file>: s-e[, s-e] (pad 25)` line per ranged file and
         one `  whole-file <tool> ×N — <reason>` line per distinct reason.
       test_contract:
         - TestRunScopedRecordsEffectiveRanges: rangingAdapter + Scope hunk 30-31 → leg.Ranges["a.ts"] == [{5,56,25}] and leg.WholeFile is empty; a Pad other than 25 or a range that is the raw hunk fails it (pins pad_constant).
         - TestRunScopedNamesEveryWholeFileReason, four sub-cases each asserting the reason string AND the Degraded delta: plainAdapter under a hunked Scope → WholeFile[f]=="adapter has no range runner" for every file, Degraded unchanged; rangingAdapter under a hunk-less Scope → "scope has no hunks", Degraded gains exactly one entry naming the adapter; rangingAdapter with a.ts hunked and b.ts not → Ranges has a.ts only, WholeFile["b.ts"]=="file absent from hunks", Degraded unchanged; hunk {0,3} on a.ts → WholeFile["a.ts"] starts "malformed range 0-3", RunRanges receives no a.ts entry, Degraded gains one entry. Collapsing the severity split to all-degraded or none fails two of the four.
         - TestRunScopedStampsProvenanceOnAFailedLeg: an adapter whose RunRanges errors still yields a leg with Error set AND WholeFile/Ranges populated — deleting the assignment from the error branch fails it.
         - TestSkeletonWritesLegProvenance: Skeleton over a Tests with one ranged leg and one whole-file leg → LegSummary{Pad:25, Ranges:["a.ts:5-56"]} and LegSummary{WholeFile:["b.ts — adapter has no range runner"]}; Save then toml-decode the verify.toml and the same strings come back (the TOML tags exist and round-trip).
         - TestDetachedLegRecordsWholeFile (cmd): detachedLeg over three files and a gremlins adapter → WholeFile has all three keyed to "adapter has no range runner" and Ranges is nil — a results-path leg that forgets provenance fails here without needing ssh.
         - TestScopeSummaryPrintsProvenance (cmd, captureStdout over printScopeSummary): a ranged stryker leg prints a line containing "ranged stryker a.ts: 5-56 (pad 25)"; a gremlins leg over 3 files prints "whole-file gremlins ×3 — adapter has no range runner"; a Tests with only whole-file legs prints no "ranged" line — a run that measured whole files never reads as a ranged one.

## Wave 3 (depends t-3)

  t-4  Add `dross verify scope` subcommand
       files:    internal/cmd/verifyscope.go, internal/cmd/verify.go,
                 internal/cmd/verifyscope_test.go, README.md
       covers:   c-5
       depends:  t-3
       description:
         verifyScope() cobra command, `Use: "scope <phase-id>"`, registered in
         verify.go beside finalize/results/status. Loads tests.json via
         verify.LoadTests(verify.FilePaths(root, id)); nil → error
         "no verify run recorded for <id> — run `dross verify <id>` first"
         (non-zero exit through the existing RunE error path). Human output:
         scope header (source, base, N files), each in-scope file with its raw
         hunks, then per leg the ranged files with `s-e (pad N)` and the
         whole-file files with their reason. `--json` marshals a scopeRecord
         {phase, generated_at, scope, legs:[{name, tool, files, ranges,
         whole_file}]} copied field-for-field from the loaded Tests — no
         re-rendering. One README row in the verify command table.
       test_contract:
         - TestVerifyScopePrintsTheLastRun: a tests.json fixture under chdirDross with Scope{Files:[a.ts,b.ts], Hunks:{a.ts:[{30,31}]}} and a stryker leg {Ranges:{a.ts:[{5,56,25}]}, WholeFile:{b.ts:"file absent from hunks"}} → runCmd(verifyScope(), id) output contains "a.ts", "30-31", "5-56 (pad 25)" and "b.ts" followed by "file absent from hunks"; dropping any of the four sections fails it.
         - TestVerifyScopeJSONIsTheRecord: `--json` output unmarshals and its scope.hunks and legs[0].ranges / legs[0].whole_file deep-equal the fixture's decoded values — a pretty-printed string form (e.g. "5-56") in place of {start,end,pad} fails it.
         - TestVerifyScopeWithoutARunNamesTheFix: no tests.json → runCmd returns a non-nil error whose text contains the phase id and "dross verify <id>"; a nil return or a generic "not found" fails it.
         - TestReadmeDocumentsVerifyScope: README.md contains a row starting "| `dross verify scope <phase>`".

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3 |
| c-3 | t-3 |
| c-4 | t-2 |
| c-5 | t-4 |

Every criterion has exactly one owning task; every task traces to a criterion.

## Judgment calls

- t-1 touches 7 files, over the 5-file guideline. Kept as one task: the locked landing_method is a cherry-pick, which is atomic — splitting it leaves a task with a half-applied commit that does not compile (runArgs' signature change spans both packages' tests). One layer (adapter seam + its dispatch); 4 of the 7 are the ported test files. Rejected: a "port mutation half" / "port verify half" split.
- c-2 and c-3 merged into t-3. Rejected: separate record-vs-surface tasks. The reasons and the ranges are produced by the same planner call and written to the same leg; two tasks would mean the first ships a record with fields nothing prints.
- Provenance planned in verify (LegProvenance) rather than reported back by the adapter. Rejected: extending RangeRunner's return to carry "what I actually ran". The lock says the leg owns the record and the adapter is a seam; the effective ranges are exactly what verify hands the adapter, so verify already knows them. Malformed hunks are therefore caught in verify before padAndMerge — the stryker-side fallback stays but is never the path that records a reason.
- c-4 via tests only, no `dross survivor accept`. The routing note and the tracked-path-containment FLAG both say these are a plain coverage gap with a named fix (a recording TB); accepting them would contradict that record. Rejected: a bookkeeping task to touch tracked-path-containment/spec.toml — drain keys on the live gremlins report, and a killed mutant is not re-listed.
- No prompt edit (assets/prompts/verify.md). No criterion requires the prompt to mention provenance; c-3's "verify output" is the CLI's printScopeSummary. Rejected as speculative.
- `--json` emits a copied record, not the raw tests.json bytes. "Verbatim" is satisfied by field-for-field copies of the same values the human view prints; dumping the whole tests.json would drown the record in mutation reports.
- Feature-branch deletion (landing_method: "once this phase ships") is a ship-time action, not a plan task — noted here so the executor carries it to /dross-ship.
- README row folded into t-4 rather than its own task (one line, repo convention of README sync); pinned by a one-assert test so it is not a file with no contract.
