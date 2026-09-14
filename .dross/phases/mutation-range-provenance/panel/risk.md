# Planner draft — lens: risk

Failure modes drive this graph. Each numbered risk below is owned — built and
tested — by exactly one task; the coverage table at the end maps criteria, the
risk register maps hazards.

## Risk register (what can break, and who owns it)

| # | Failure mode | Owner |
|---|---|---|
| R1 | Cherry-picks land red under the four phases HEAD moved through (toolfence error-constructor ban, pathfence/toolfence field walkers, argfence). Checked: `git apply --check` of 1da3f73 is clean on HEAD; 21e354b only needs 1da3f73 first. Fallback messages use `fmt.Fprintf`, which is outside `errorConstructors`, so the ban is not tripped — but it must be observed green, not assumed. | t-1 |
| R2 | Two validators disagree: verify records a file as ranged while `runArgs` rejects the same range and silently mutates the whole file. The record then lies in exactly the direction c-2 forbids. | t-3 |
| R3 | The recorded ranges are built from a different value than the ranges dispatched (a second loop, a re-derivation), so record and argv drift. | t-4 |
| R4 | Detached collection (`dross verify results`) builds `LanguageRun` by hand at `internal/cmd/verify.go:641` — a second writer that would omit provenance, so a detached gremlins leg reads as "no fallback recorded" while the attached one says "whole file: adapter cannot range". | t-4 |
| R5 | `Scope.Degraded` append on a nil scope (`verify.Run`, unscoped) or the shared pointer being appended once per adapter, producing duplicate/panicking degraded lines. | t-4 |
| R6 | A failed leg (adapter error) drops its provenance, so a failed ranged leg is indistinguishable from a failed whole-file leg. | t-4 |
| R7 | New fields silently fail JSON/TOML round-trip (`omitempty` on maps, `LoadTests` on a pre-phase tests.json without the fields) — c-5 then prints nothing or panics. | t-4 (JSON), t-5 (TOML), t-6 (old record) |
| R8 | New tags trip the schema walkers: a `reason`-tagged string inside the fallback record is a toolfence sink; a `files`-tagged field is a pathfence sink. Undeclared → red. Owned by choosing tags outside both vocabularies (`ranges`, `whole_file`, `pad`) and keeping reasons a closed enum. | t-3 |
| R9 | Path-vocabulary mismatch between `Scope.Hunks` keys, the paths `mutationCandidates` dispatches (`Contained.Rel()`), and `rangeKey` inside stryker with a monorepo `Workdir`. If any link differs every file quietly falls to "absent from hunks" — informational, so nobody notices the run never narrows. | t-7 |
| R10 | `--json` re-marshals rather than emitting verbatim, dropping fields a newer/older writer put in tests.json. | t-6 |
| R11 | compilefence mutants live in a package whose own tests never call the two wrappers (coverage count=0 on :61/:80/:81); a kill written in another package does not count for gremlins' per-package scope. :81 is `+` on string literals — ARITHMETIC_BASE to `-` cannot compile, so it may be non-viable rather than killable. | t-2 |
| R12 | A whole-file run reads as ranged in the human output: the summary prints ranges for one leg and says nothing about the leg that could not range. | t-5 |

---

Phase mutation-range-provenance — 7 tasks across 4 waves

Wave 1
  t-1  Land ranged runner cherry-picks on HEAD
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/mutation/range_test.go, internal/mutation/argv_test.go,
                 internal/mutation/stryker_test.go, internal/verify/verify.go,
                 internal/verify/range_dispatch_test.go
       covers:   c-1
       description: `git cherry-pick -x 1da3f73` then `21e354b` (in that order — the
                 second edits range_dispatch_test.go the first creates). Resolve any
                 drift against HEAD's stryker.go/verify.go (30 commits since the
                 branch point; the dry-run applies clean but HEAD's RunScoped has the
                 RecordLegError/RemoteTransport error-leg shape the pick's hunk sits
                 beside). No new behaviour in this task.
       contract: if RunRanges emits `file` before escapeGlobMeta, TestRunArgsAppendsTheRangeAfterEscaping fails
       contract: if a Scope with no hunks narrows to nothing, TestRunScopedWithNoHunksRunsWholeFiles fails
       contract: if an adapter without RunRanges is asserted rather than checked, TestRunScopedStillRunsAnAdapterThatCannotRange fails
       contract: if padAndMerge stops merging touching ranges, TestPadAndMergeMergesOverlappingHunks fails
       contract: if a fallback message is routed through errors.New/fmt.Errorf/fmt.Sprintf, TestNoToolOutputInsideAnErrorConstructor fails (R1)
       contract: `go test ./internal/mutation ./internal/verify` observed green on this commit; internal/cmd via `dross test` (remote), not --local
       depends_on: []

  t-2  Kill compilefence survivors in their own package
       files:    internal/compilefence/compilefence_test.go
       covers:   c-4
       description: Add a recording testing.TB (embeds *testing.T, overrides Fatalf to
                 capture instead of Goexit) and drive AssertDoesNotCompile and
                 AssertCompiles from inside package compilefence with a compiling and
                 a non-compiling fixture. If the phase's helicon run still lists :81,
                 accept it with `dross survivor accept --reason` citing "ARITHMETIC_BASE
                 on string concatenation does not compile — non-viable, not survived".
       contract: if compilefence.go:61 `err == nil` is negated, TestAssertDoesNotCompileFatalsWhenTheFixtureBuilds fails (a compiling fixture must record exactly one Fatalf naming "COMPILED but should not have")
       contract: if compilefence.go:61 is negated, TestAssertDoesNotCompileIsQuietOnARealFailure also fails (a non-compiling fixture with the right wantMsg must record zero Fatalfs)
       contract: if compilefence.go:80 `err != nil` is negated, TestAssertCompilesIsQuietOnACleanBuild fails (zero Fatalfs on a compiling fixture) and TestAssertCompilesFatalsOnABrokenFixture fails
       contract: if compilefence.go:81's message halves are split or reordered, TestAssertCompilesNamesTheFenceAsBroken fails (recorded text contains both "compile fence itself is broken" and "so every AssertDoesNotCompile")
       contract: if the three are neither killed nor accepted, the phase's own verify run re-lists compilefence.go:61/:80/:81 as unclassified and the gate stays open (R11)
       depends_on: []

Wave 2 (depends t-1)
  t-3  One range validator; classify each file's fate pre-dispatch
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/verify/range_provenance.go, internal/verify/range_provenance_test.go
       covers:   c-3 (classification), c-2 (pad constant recorded)
       description: Add `func (r Range) Valid() bool` (Start ≥ 1, End ≥ Start) and make
                 stryker.runArgs call it instead of its inline check. Add
                 `EffectiveRange{Start,End,Pad}`, the closed reason set
                 (WholeFileNoRangeRunner, WholeFileNoHunks, WholeFileAbsentFromHunks,
                 WholeFileMalformedRange), and a pure `legProvenance(a, files, scope)`
                 returning the dispatch map plus `Ranges map[string][]EffectiveRange`,
                 `WholeFile map[string]string`, and the degraded lines to append (only
                 for malformed / hunk-less-on-capable). Pure: no dispatch, no I/O.
       contract: if verify and stryker validate differently, TestVerifyNeverDispatchesARangeStrykerWouldRefuse fails — for hunks {0,5}, {5,2}, {-3,4} the classifier puts the file under whole_file=malformed, and runArgs given the classifier's output emits `file:start-end` for every key it is handed (R2)
       contract: if a malformed hunk drags the leg's other files to whole-file, TestMalformedHunkFallsBackOnlyItsOwnFile fails
       contract: if a file in scope but absent from Hunks is classified anything other than absent-from-hunks with no degraded line, TestAbsentFromHunksIsInformational fails
       contract: if a non-RangeRunner adapter yields a degraded line, TestNoRangeRunnerIsInformational fails; if a hunk-less scope on a RangeRunner yields no degraded line, TestHunklessScopeOnRangeCapableAdapterDegrades fails
       contract: if any recorded pad ≠ 25 or the recorded start/end ≠ padAndMerge's output, TestEffectiveRangesArePostPadPostMerge fails
       contract: if a WholeFile value is outside the four constants, TestWholeFileReasonsAreAClosedSet fails; if the tags `ranges`/`whole_file`/`pad` enter either walker vocabulary, TestEveryPersistedTextSinkIsDeclared / TestEveryPathShapedFieldIsDeclared go red (R8)
       depends_on: [t-1]

Wave 3 (depends t-3)
  t-4  Record provenance on every leg, attached and detached
       files:    internal/verify/verify.go, internal/verify/range_dispatch_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_results_test.go
       covers:   c-2 (tests.json), c-3 (run record)
       description: `LanguageRun` gains `Ranges map[string][]EffectiveRange json:"ranges,omitempty"`
                 and `WholeFile map[string]string json:"whole_file,omitempty"`. RunScoped
                 replaces runAdapter's inline map with legProvenance, dispatches the SAME
                 map it records, appends degraded lines to scope.Degraded (deduped, nil-safe),
                 and stamps both fields on the success leg AND the error leg. Export
                 `WholeFileLeg(files, reason)` and use it at cmd/verify.go:641 so the
                 detached gremlins leg carries whole_file=no-range-runner.
       contract: if the recorded ranges are derived separately from the dispatched ones, TestRecordedRangesAreTheDispatchedRanges fails (fake RangeRunner captures its map; deep-equals lr.Ranges start/end) (R3)
       contract: if `dross verify results` writes a gremlins leg without whole_file for every file, TestCollectRecordsWholeFileProvenance fails (R4)
       contract: if RunScoped is called with a nil scope or Run() is used, TestNilScopeRecordsNoProvenanceAndDoesNotPanic fails (R5)
       contract: if two RangeRunner adapters on a hunk-less scope produce two identical degraded lines, TestDegradedLinesAreDeduped fails (R5)
       contract: if an adapter returns an error, TestFailedLegKeepsItsProvenance fails when lr.Ranges/lr.WholeFile are empty on the error leg (R6)
       contract: if `ranges`/`whole_file` do not survive Tests.Save → LoadTests, TestProvenanceRoundTripsThroughTestsJSON fails (R7)
       depends_on: [t-3]

Wave 4 (depends t-4)
  t-5  State ranges and fallbacks in verify.toml and output
       files:    internal/verify/verify.go, internal/cmd/verify.go,
                 assets/prompts/verify.md, internal/cmd/verify_scoping_test.go,
                 internal/cmd/verify_prompt_test.go
       covers:   c-2 (verify.toml), c-3 (surfaced)
       description: `LegSummary` gains `Pad int toml:"pad,omitempty"`, `Ranges []string
                 toml:"ranges,omitempty"` ("file:start-end") and `WholeFile []string
                 toml:"whole_file,omitempty"` ("file — reason"); Skeleton fills them from
                 LanguageRun. printScopeSummary prints one line per leg: "ranged N file(s),
                 pad 25" or "whole-file N file(s) — <reason>", capped like the file list.
                 verify.md gains one paragraph under "The score covers only this phase's
                 changed files" telling the agent to read the per-leg ranges/whole_file
                 and never call a whole-file leg ranged. `make install` after.
       contract: if a leg without ranges is printed with the word "ranged" or without its reason, TestVerifyOutputNamesWholeFileLegs fails (R12)
       contract: if verify.toml's [[summary.leg]] for a ranged stryker leg lacks pad=25 and every `file:start-end`, TestVerifyTomlStatesEffectiveRanges fails; if a gremlins leg's whole_file lines are missing, the same test fails
       contract: if the LegSummary fields do not round-trip through Verify.Save → LoadVerify, TestVerifyTomlStatesEffectiveRanges fails on reload (R7)
       contract: if verify.md stops naming `whole_file` and `ranges`, TestVerifyPromptReadsRangeProvenance fails
       depends_on: [t-4]

  t-6  Add `dross verify scope <phase-id>`
       files:    internal/cmd/verifyscope.go, internal/cmd/verify.go,
                 internal/cmd/verifyscope_cmd_test.go, README.md
       covers:   c-5
       description: New cobra subcommand registered beside results/status/finalize
                 (verify.go:162-164). Loads tests.json via verify.LoadTests; prints
                 in-scope files, base, raw hunks, then per leg the effective ranges with
                 pad and every whole_file entry with its reason. `--json` writes the
                 tests.json bytes to stdout unchanged. No tests.json → exit non-zero:
                 "no verify run recorded for <phase> — run `dross verify <phase>` first".
                 README command table gains the row.
       contract: if `--json` re-marshals, TestVerifyScopeJSONIsVerbatim fails (sha256 of stdout ≠ sha256 of tests.json) (R10)
       contract: if the missing-run path exits 0 or omits `dross verify <phase>`, TestVerifyScopeWithoutARunNamesTheFix fails
       contract: if a tests.json written before this phase (no ranges/whole_file) panics or prints a leg as ranged, TestVerifyScopeOnAPreProvenanceRecordSaysSo fails (R7)
       contract: if a whole_file entry's reason is dropped from the text output, TestVerifyScopePrintsEveryFallbackReason fails
       contract: if README lacks the `dross verify scope` row, TestReadmeDocumentsVerifyScope fails
       depends_on: [t-4]

  t-7  Prove narrowing survives the real scope pipeline
       files:    internal/cmd/verify_range_e2e_test.go, internal/cmd/verify_test.go
       covers:   c-1, c-2
       description: A `stubRangeAdapter` (embeds stubMutationAdapter, implements RunRanges,
                 records the map) driven through the real `dross verify` over a
                 scopedVerifyRepo with a two-line edit, so phaseScope → containScope →
                 mutationCandidates → RunScoped → RunRanges runs unstubbed. A second case
                 with a Workdir-style path checks stryker.rangeKey finds the same key.
       contract: if Scope.Hunks keys and the dispatched paths ever differ in form, TestRangedDispatchReachesTheAdapterEndToEnd fails (adapter receives a non-empty map keyed by the edited file, tests.json's languages[0].ranges names it with pad 25, and whole_file is empty) (R9)
       contract: if a monorepo Workdir makes rangeKey miss the map, TestRangeKeyMatchesScopeKeysUnderAWorkdir fails (runArgs on "web/src/a.ts" with ranges keyed "web/src/a.ts" emits `src/a.ts:1-30`)
       contract: if the run silently never narrows, this test fails where every other test in the phase — all fed hand-built scopes — would pass
       depends_on: [t-4]

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-3, t-4, t-5, t-7 |
| c-3 | t-3, t-4, t-5 |
| c-4 | t-2 |
| c-5 | t-6 |

## Judgment calls

- **Validation lives in one method (`Range.Valid`) called by both layers, not two inline checks.** Rejected: leaving the cherry-pick's inline `r.Start <= 0 || r.End < r.Start` in runArgs and re-deriving it in verify — two copies is R2 by construction, and the divergence test in t-3 only means something if there is one definition to diverge from.
- **Classification (t-3) is a pure function split from wiring (t-4).** Rejected: doing both in one task — it would touch mutation + verify + cmd across 6 files, and the fallback-severity rules would be tested only through RunScoped's fake adapters rather than directly against the four named causes.
- **The detached path calls an exported `WholeFileLeg` helper rather than duplicating the map.** Rejected: hand-writing `WholeFile` at cmd/verify.go:641 — that is exactly the second-writer drift R4 describes, and persist_toolfence_test already shows this repo has been bitten by two writers before.
- **`--json` emits tests.json bytes verbatim, not a re-marshal.** Rejected: `json.MarshalIndent(t)` — it is not "the same record verbatim" once any writer adds a field the reader's struct lacks, and the sha-equality test is cheaper than reasoning about which fields survive.
- **Tags `ranges` / `whole_file` / `pad` are chosen to sit outside both walker vocabularies, with reasons as a closed enum.** Rejected: a `{file, reason}` struct list — `reason` is in toolfenceVocabulary and would demand a registry declaration for text that is dross-authored anyway; the closed-set test gives the same guarantee without widening the registry.
- **Provenance is stamped on the error leg too.** Rejected: only on success — a failed ranged leg and a failed whole-file leg would then be the same record (R6), and the leg already carries MeasuredOn on failure for the same reason.
- **t-7 is its own task, not a contract line on t-4.** Every other test in the phase feeds a hand-built Scope; the one failure that hides behind an informational reason is the pipeline never producing keys the adapter recognises. One test owns that, end to end, through the real command.
- **c-4's `:81` may be non-viable rather than killable.** The task writes the message-halves test regardless and falls back to `dross survivor accept --reason` only if the phase's own helicon run still lists it — the spec allows either, and the acceptance reason cites the compile-failure mechanism, not "hard to test".
- **Deleting `feat/mutate-changed-lines` (local + origin) is not a task.** It is locked to happen "once this phase ships", is not testable, and must not run before the PR merges; it belongs in the ship follow-up / handoff, not the plan graph.
- **The verify prompt edit is folded into t-5, not separate.** It is one paragraph with one test pin, and its purpose (the agent never calls a whole-file leg ranged) is the same surfacing concern as printScopeSummary.
