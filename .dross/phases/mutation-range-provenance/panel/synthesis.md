# Synthesis — mutation-range-provenance

Cold judge over three independent drafts (risk / mvp / verification). Every
checkable claim below was verified read-only against HEAD (1ba964f) before it
was grafted; corrections to the drafts are noted inline.

Facts established during verification (used throughout):

- `git show 1da3f73 | git apply --check` is CLEAN on HEAD. `21e354b` does not
  apply alone (needs 1da3f73's `range_dispatch_test.go` and `runAdapter`).
  HEAD is 30 commits past the merge-base (47ca955).
- `headBuffer.buf` still exists in `internal/mutation/toolfail.go:155`, so the
  pick's `strings.Contains(head.buf.String(), …)` COMPILES on HEAD; the
  adaptation to `head.contains()` (toolfail.go:184) is idiom, not a conflict.
  mvp's wording is the accurate one.
- `toolfenceVocabulary` (internal/cmd/toolfence_enum_test.go:32) contains
  `reason` and `degraded`; `pathShapedTags` (pathfence_fields_test.go:43)
  contains `files` but not `file`; `textSinks` only considers `string` /
  `[]string` fields — a `map[string]string` is never a sink.
  `verify.Scope.Degraded` is already declared `not-tool-stream`
  (internal/toolfence/fields.go:251).
- `errorConstructors` = `errors.New`, `fmt.Errorf`, `fmt.Sprintf`
  (toolfence_residual_test.go:41). The pick's fallback messages go through
  `fmt.Fprintf(os.Stderr, …)`; its `--mutate` spec uses `fmt.Sprintf` over
  path+ints, not tool output.
- The hand-built detached `LanguageRun` is at `internal/cmd/verify.go:646`
  (verification is right; risk's `:641` is five lines off). Subcommands are
  registered at `verify.go:162-164`.
- `dross survivor drain` is `Args: cobra.NoArgs` with `--packages` and
  `--phase` flags (survivor_drain.go:313-438) and requires exec consent at the
  top of RunE. verification's positional `drain internal/compilefence` form is
  WRONG; the merged plan uses `--packages`.
- tracked-path-containment/verify.toml:102 is an explicit FLAG: "The 3
  compilefence survivors are NOT a ceiling and must not be accepted … A
  recording testing.TB stub in compilefence_test.go would exercise both
  wrappers and kill all three." compilefence.go:61 is `if err == nil`, :80 is
  `if out, err := build(t, src); err != nil`, :81 is the string `+` in the
  Fatalf. Fixtures `importsInternal`, `setsUnexportedField`,
  `setsUnknownField`, `emptyLiteral` exist in compilefence_test.go:18-43;
  every existing test calls `build()` directly.
- The pick's `rangingAdapter` ALREADY records `ranRanges` (21e354b's tests read
  it), so verification's "extend the fake to record" is already done. The
  ported tests assert hunk {100,104} → {75,129}, {3,4} → start 1, {100,101}+
  {120,121} → {75,146}, {10,11}+{900,901} → two ranges — NOT the example
  numbers the drafts quote (mvp's 30-31→5-56, verification's 10-12→1-37,
  risk's 40-42→1-67). Contracts below cite ported tests by name and use the
  pick's real values; new tests keep verification's 10-12→1-37 example, which
  is arithmetically correct.
- `assets/prompts/verify.md:72` is the "The score covers only this phase's
  changed files" paragraph risk cites; `internal/cmd/verify_prompt_test.go`
  exists for phrase pins. README's verify command table is at README.md:220-222.

## Scores

Scale 1-5 per dimension.

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk | 5 — every criterion owned by ≥1 task, plus a 12-entry hazard register each owned by one task; only draft with an end-to-end pipeline proof (t-7) | 4 — every contract names a test and its fail condition; a few example values (40-42→1-67, `:641`) do not match the pick; ported-test contracts are paraphrased rather than quoted | 4 — 7 tasks, each ≤5 files except the unavoidable 7-file pick; classifier/wiring split is clean but leaves one wave of uncalled code | 5 — W1 {t-1,t-2} ∥, W2 t-3, W3 t-4, W4 {t-5,t-6,t-7} ∥; every edge is a real compile/data dependency |
| mvp | 5 — exactly one owner per criterion, none over-assigned | 4 — the most concrete values and subtest tables; most accurate on HEAD facts (`head.contains` idiom, `buf` still exists); but the malformed reason carries `%d-%d` prose and `-n` squashes the two picks, which the lock's rationale argues against | 3 — 4 tasks; t-3 spans record + Skeleton + printScopeSummary + detached path across two packages (six behaviours, six contracts) — the widest task in any draft | 4 — W1 {t-1,t-2} ∥, W2 t-3, W3 t-4; correct, but t-3's width makes W2 the long pole |
| verification | 5 — all five, each with byte-level proof (raw tests.json / verify.toml / stdout / exit code) | 5 — sharpest: malformed check on the RAW hunk before padAndMerge (clamping would hide `{0,3}`), gremlins leg must OMIT `pad`/`range` (omitempty), `TestLegacyVerifyTomlLoads` kept green, `TestGremlinsDoesNotImplementRangeRunner` pins the "gremlins runs whole files" clause | 4 — 6 tasks; t-1/t-2 split is over-fine (21e354b is 2 files, applies clean atop t-1, same criterion); lists `.dross/survivors.toml` as a task file while saying never hand-edit it | 3 — t-2 occupies its own wave for a 2-file pick, pushing every downstream task one wave later; `drain` CLI form is wrong (positional vs `--packages`) |

**Skeleton: risk.** It has the strongest structure — a pure classifier the four
fallback causes are tested against directly, a single shared range validator so
verify and stryker cannot disagree, the detached path routed through the same
writer, provenance stamped on the error leg, nil-scope / dedupe hazards owned,
and the only test that runs narrowing through the real `dross verify` pipeline
rather than a hand-built Scope. verification's contracts are sharper and are
grafted onto that skeleton wholesale; mvp contributes the accurate HEAD facts,
the subtest-table shape for the four causes, and the README/detached-leg
specifics.

## Merged plan

Phase mutation-range-provenance — 7 tasks across 4 waves

Wave 1
  t-1  Land the two ranged-runner cherry-picks on HEAD   [risk+mvp+verification]
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/mutation/range_test.go, internal/mutation/argv_test.go,
                 internal/mutation/stryker_test.go, internal/verify/verify.go,
                 internal/verify/range_dispatch_test.go
       covers:   c-1
       description: `git cherry-pick -x 1da3f73` then `git cherry-pick -x 21e354b`, in
                 that order (the second edits range_dispatch_test.go the first creates
                 and hooks padAndMerge into the first's runAdapter). 1da3f73 applies
                 clean on HEAD (checked); adapt idiom, not conflicts: in stryker.go's
                 narrowed-tolerant checkInstrumented use `head.contains(strykerDropWarningText)`
                 (toolfail.go:184) rather than the pick's `strings.Contains(head.buf.String(), …)`
                 — both compile, one is HEAD's idiom; in verify.go RunScoped the
                 `report, err := runAdapter(a, byAdapter[name], scope)` line replaces
                 `a.Run(...)` and sits beside HEAD's `mutation.RecordLegError(err)` /
                 `RemoteTransport` error branch — keep both. Lands: mutation.Range,
                 mutation.RangeRunner, Stryker.RunRanges, runArgs(files, ranges), rangeKey,
                 narrowedSet; verify.hunkContextLines = 25 (UNTOUCHED — pad_constant lock),
                 padAndMerge, runAdapter; the 235+209 lines of tests the two commits carry.
                 Add one new test, TestGremlinsDoesNotImplementRangeRunner. No new behaviour
                 beyond that. `make install` after (r-01).
       contract: TestRunArgsEmitsLineRanges (ported) — runArgs with {"src/a.ts":[{10,12}]} puts `src/a.ts:10-12` in --mutate; a build that ignores `ranges` fails it
       contract: TestRunArgsAppendsTheRangeAfterEscaping (ported) — for a `[id]` route path the `:10-12` follows the escaped `[[]id[]]` form; glue the suffix on before escapeGlobMeta and the bracket expression swallows the colon
       contract: TestRunArgsFallsBackWholesaleWhenAnyRangeIsMalformed (ported) — one bad range in a file's set yields the bare path once and NO `:s-e` entry beside it
       contract: TestRunScopedNarrowsToTheChangedLines / TestRunScopedPassesPaddedRanges (ported) — the ranging adapter receives {75,129} for raw hunk {100,104}, never the raw hunk; RunScoped still calling Run, or a pad other than 25, fails
       contract: TestRunScopedStillRunsAnAdapterThatCannotRange (ported) — a plainAdapter under a hunked Scope has Run called once; an unchecked type assertion panics here
       contract: TestRunScopedWithNoHunksRunsWholeFiles + TestRunScopedWithANilScopeRunsWholeFiles + TestRunScopedWithHunksForOtherFilesRunsWholeFiles (ported) — RunRanges is never called, Run is: the fail-open limb
       contract: TestPadAndMergeWidensEachHunk ({100,104}→{75,129}), TestPadAndMergeClampsToTheFirstLine ({3,4}→start 1), TestPadAndMergeMergesOverlappingHunks ({100,101}+{120,121}→{75,146}), TestPadAndMergeKeepsDistantHunksSeparate ({10,11}+{900,901}→two), TestPadAndMergeOnNoHunksIsNil (all ported) — a pad of 24 or 26 fails the exact-value asserts
       contract: TestCheckInstrumentedToleratesANarrowedFileWithNoMutants passes and TestCheckInstrumentedStillRefusesWhenStrykerWarnedItDropped + TestCheckInstrumentedStillRefusesAnUnnarrowedDrop still error (ported) — the drop-warning guard survives narrowing; deleting the guard fails the second
       contract: TestStrykerImplementsRangeRunner (ported) compiles; NEW TestGremlinsDoesNotImplementRangeRunner asserts `_, ok := any(&Gremlins{}).(RangeRunner); !ok` — the criterion's "gremlins runs whole files" clause is pinned, not assumed [verification]
       contract: TestNoToolOutputInsideAnErrorConstructor stays green — the pick's fallback messages are `fmt.Fprintf(os.Stderr, …)`, outside errorConstructors; a fallback routed through errors.New/fmt.Errorf/fmt.Sprintf over the head fails it (R1) [risk]
       contract: `go test -count=1 ./internal/mutation/ ./internal/verify/` and `go vet ./...` observed green on this commit; internal/cmd via `dross test` (remote), never --local
       depends_on: []

  t-2  Kill the three compilefence survivors in their own package   [risk+mvp+verification]
       files:    internal/compilefence/compilefence_test.go, .dross/survivors.toml
       covers:   c-4
       description: Add a recording testing.TB (embeds *testing.T; overrides Fatalf to
                 capture the formatted message and return instead of Goexit; Helper no-op)
                 and drive AssertDoesNotCompile / AssertCompiles through it from INSIDE
                 package compilefence, using the existing fixtures (importsInternal,
                 setsUnexportedField, setsUnknownField, emptyLiteral). Today every test in
                 the file calls build() directly, so :61/:80/:81 read count=0 in gremlins'
                 per-package scope — the tracked-path-containment FLAG names exactly this
                 fix. Gate behind !testing.Short() like the file's other build tests.
                 Then run `dross survivor drain --packages internal/compilefence --phase
                 mutation-range-provenance` (NoArgs; needs exec consent). Tests are the
                 primary disposition. ONLY if the drain still lists :81 after coverage —
                 ARITHMETIC_BASE turning a string `+` into `-` does not compile, so it should
                 surface as non-viable, not survived — accept it via
                 `dross survivor accept internal/compilefence/compilefence.go:81 --op ARITHMETIC_BASE --reason "…"`
                 citing the compile-failure mechanism. survivors.toml is written by the CLI
                 in that case, never hand-edited.
       contract: TestAssertDoesNotCompileRefusesACompilingFixture — AssertDoesNotCompile(rec, importsInternal, "x") records exactly one Fatalf containing "COMPILED but should not have"; negate `err == nil` at compilefence.go:61 and nothing is recorded (kills :61 CONDITIONALS_NEGATION)
       contract: TestAssertDoesNotCompileAcceptsTheRightRefusal — (rec, setsUnexportedField, "cannot refer to unexported field") records zero Fatalfs — the other side of :61, so the negation cannot pass by firing on every fixture
       contract: TestAssertDoesNotCompileRefusesTheWrongReason — (rec, setsUnknownField, "cannot refer to unexported field") records a Fatalf containing "WRONG reason" — pins the branch below :61 so the negation cannot escape through the second Fatalf
       contract: TestAssertCompilesRefusesABrokenFixture — AssertCompiles(rec, setsUnexportedField) records a Fatalf whose text contains BOTH "compile fence itself is broken" and "so every AssertDoesNotCompile" (the two halves of :81's concatenation); negate `err != nil` at :80 and nothing fires (kills :80; :81's line is now executed so its mutant is compiled and either non-viable or caught by the message assertion)
       contract: TestAssertCompilesAcceptsACleanFixture — AssertCompiles(rec, importsInternal) records zero Fatalfs — the other side of :80
       contract: Observed at the phase's own drain, not unit-asserted: `dross survivor drain --packages internal/compilefence --phase mutation-range-provenance` reports 0 outstanding and lists none of 365503d33114f65e, 5d74aadbbae679b7, 695a2b44f32b1b43 — killed, or (:81 only) accepted with a reason naming non-viability; neither is re-listed for the next run
       depends_on: []

Wave 2 (depends t-1)
  t-3  One range validator; classify each file's fate before dispatch   [risk, contracts from verification+mvp]
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/verify/range_provenance.go, internal/verify/range_provenance_test.go
       covers:   c-3 (classification + severity split), c-2 (pad constant recorded)
       description: Add `func (r Range) Valid() bool` on mutation.Range (Start ≥ 1, End ≥
                 Start) and make stryker.runArgs call it in place of its inline
                 `r.Start <= 0 || r.End < r.Start` — ONE definition both layers share. Add
                 `EffectiveRange{Start, End, Pad int}` (json start/end/pad), the CLOSED
                 reason set as string constants — WholeFileNoRangeRunner =
                 "adapter-lacks-range-runner", WholeFileNoHunks = "scope-has-no-hunks",
                 WholeFileAbsentFromHunks = "file-absent-from-hunks", WholeFileMalformedRange
                 = "malformed-range" — and an exported pure planner
                 `PlanRanges(a mutation.Adapter, files []string, scope *Scope) RangePlan`
                 where RangePlan carries the dispatch map (`map[string][]mutation.Range`),
                 `Ranges map[string][]EffectiveRange`, `WholeFile map[string]string`, and the
                 Degraded lines to append. Malformed is checked on the RAW hunk (converted
                 to mutation.Range, `.Valid()`) BEFORE padAndMerge — padding would clamp
                 {0,3} into a legal range and hide the malformation; the hunk's numbers go
                 into the Degraded line, never into the closed reason value. Severity per
                 the fallback_severity lock: NoRangeRunner and AbsentFromHunks add no
                 Degraded line; MalformedRange and NoHunks-on-a-RangeRunner each add one.
                 Pure: no dispatch, no I/O, nothing calls it until t-4. Tags `ranges` /
                 `whole_file` / `pad` / `start` / `end` sit outside both walker vocabularies;
                 the reason lives in a map VALUE, never a `reason`-tagged string field.
       contract: TestVerifyNeverDispatchesARangeStrykerWouldRefuse — for raw hunks {0,5}, {5,2}, {-3,4} PlanRanges puts the file under WholeFile=WholeFileMalformedRange and omits it from the dispatch map; runArgs given the plan's dispatch map emits `file:start-end` for EVERY key it is handed (no wholesale fallback) — the two validators cannot diverge because there is one (R2) [risk]
       contract: TestMalformedRawHunkIsCaughtBeforePadding — hunk {0,3} on a.ts is classified malformed, NOT clamped to {1,28} and dispatched; hunk {10,5} likewise; the Degraded line contains "a.ts" and the raw numbers "10-5" [verification]
       contract: TestMalformedHunkFallsBackOnlyItsOwnFile — a.ts malformed, b.ts {10,12}: b.ts is in the dispatch map as {1,37} pad 25 and a.ts is not [risk]
       contract: TestFileAbsentFromHunksIsInformational — files a.ts,b.ts, hunks only for a.ts: WholeFile["b.ts"]==WholeFileAbsentFromHunks, Ranges has a.ts only, Degraded delta is empty [risk+verification]
       contract: TestNoRangeRunnerIsInformational — a plainAdapter under a hunked Scope: WholeFile[f]==WholeFileNoRangeRunner for EVERY file, Ranges nil, Degraded delta empty [risk+verification]
       contract: TestHunklessScopeOnACapableAdapterDegrades — a RangeRunner with Hunks nil: every file → WholeFileNoHunks AND exactly one Degraded line naming the adapter; the same scope on a plainAdapter adds nothing (collapsing the split to all-degraded or none fails one of the two) [verification+mvp]
       contract: TestEffectiveRangesArePostPadPostMerge — hunks {10,12} and {30,31} on one file record ONE EffectiveRange {1,56,25}, equal to padAndMerge's output; any Pad ≠ 25, or a recorded range equal to the raw hunk, fails [risk+mvp]
       contract: TestWholeFileReasonsAreAClosedSet — every WholeFile value across all cases is one of the four constants; a value carrying hunk numbers fails [risk]
       contract: TestEveryPersistedTextSinkIsDeclared and TestEveryPathShapedFieldIsDeclared (internal/cmd) stay green with NO registry edit — the new fields are maps and ints under tags outside both vocabularies (R8) [risk+verification]
       depends_on: [t-1]

Wave 3 (depends t-3)
  t-4  Record provenance on every leg — attached, detached, and failed   [risk, contracts from verification]
       files:    internal/verify/verify.go, internal/verify/range_dispatch_test.go,
                 internal/verify/range_record_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_results_test.go
       covers:   c-2 (tests.json), c-3 (run record)
       description: LanguageRun gains `Ranges map[string][]EffectiveRange json:"ranges,omitempty"`
                 and `WholeFile map[string]string json:"whole_file,omitempty"` (range_record_home
                 lock). RunScoped calls PlanRanges once per leg, dispatches the SAME dispatch
                 map it records (RunRanges when the map is non-empty and the adapter is a
                 RangeRunner, else Run), appends the plan's Degraded lines to scope.Degraded
                 nil-safe and deduped, and stamps Ranges/WholeFile on BOTH the success leg and
                 the RecordLegError branch (provenance is known before the tool runs). The
                 pick's runAdapter is replaced by this, not kept beside it. collectDetached
                 (internal/cmd/verify.go:646) builds its gremlins leg through the same
                 PlanRanges call over the gremlins adapter, so the detached leg carries
                 whole_file=adapter-lacks-range-runner for every file rather than nothing.
       contract: TestRecordedRangesAreTheDispatchedRanges — the rangingAdapter's captured `ranRanges` deep-equals lr.Ranges' start/end for every file; a second derivation loop fails it (R3) [risk]
       contract: TestLegRecordsEffectiveRangesWithPad — after Tests.Save the RAW tests.json bytes contain `"ranges":{"src/a.ts":[{"start":1,"end":37,"pad":25}]}` under languages[0] and `"hunks":{"src/a.ts":[{"start":10,"end":12}]}` under scope — raw and effective diffable in one file; LoadTests round-trips Ranges and WholeFile reflect.DeepEqual (R7) [verification]
       contract: TestFailedLegKeepsItsProvenance — an adapter whose RunRanges returns an error still yields a leg with Error set AND Ranges/WholeFile populated; delete the assignment from the error branch and it fails (R6) [risk+mvp+verification]
       contract: TestNilScopeRecordsNoProvenanceAndDoesNotPanic — Run() (nil scope) produces legs with no ranges/whole_file and no panic on the Degraded append (R5) [risk]
       contract: TestDegradedLinesAreDeduped — two RangeRunner adapters on a hunk-less scope produce ONE degraded line per adapter, never a repeated identical line (R5) [risk]
       contract: TestCollectRecordsWholeFileProvenance — extend TestCollectWritesTheSameArtefactsAnAttachedRunWould: the collected go leg's whole_file names every dispatched file with adapter-lacks-range-runner and its ranges is absent (R4) [risk+verification]
       contract: TestGremlinsLegHasNoRanges — a gremlins leg in the attached run has `ranges` ABSENT from the bytes (omitempty) — a leg that ranged nothing must not claim ranges [verification]
       depends_on: [t-3]

Wave 4 (depends t-4)
  t-5  State ranges and fallbacks in verify.toml and verify output   [risk+mvp, contracts from verification]
       files:    internal/verify/verify.go, internal/verify/range_summary_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_scoping_test.go
       covers:   c-2 (verify.toml), c-3 (surfaced)
       description: LegSummary gains `Pad int toml:"pad,omitempty"`, `Ranges []string
                 toml:"ranges,omitempty"` ("file:start-end", sorted) and `WholeFile []string
                 toml:"whole_file,omitempty"` ("file — reason", sorted); Skeleton fills them
                 from LanguageRun for success AND error legs. printScopeSummary prints, per
                 leg, `  ranged <tool> N file(s) (pad 25)` when Ranges is non-empty and
                 `  whole-file <tool> ×N — <reason>` once per distinct reason, capped like the
                 file list. A leg with no ranges never prints the word "ranged".
                 `make install` after.
       contract: TestVerifyTomlStatesEffectiveRanges — Skeleton+Save over a Tests with one ranged stryker leg and one gremlins leg: the RAW verify.toml bytes contain `pad = 25`, `ranges = ["src/a.ts:1-37"]`, and a `whole_file` entry "src/b.ts — file-absent-from-hunks"; LoadVerify round-trips all three (R7) [risk+verification]
       contract: TestGremlinsLegHasNoPad — the gremlins [[summary.leg]] omits `pad` and `ranges` entirely (grep the bytes) and its whole_file names every file [verification]
       contract: TestLegacyVerifyTomlLoads (existing, measured_on_test.go:82) stays green — a verify.toml without the new keys round-trips unchanged [verification]
       contract: TestVerifyOutputNamesWholeFileLegs — TestScopingAttributionHoldsEndToEnd's fixture through captureStdout: stdout contains "whole-file" on the same line as "adapter-lacks-range-runner" and NOT the word "ranged"; a stub RangeRunner in the same harness prints "ranged stryker 1 file(s) (pad 25)" and never "whole-file" for that file (R12) [risk+verification+mvp]
       depends_on: [t-4]

  t-6  Add `dross verify scope <phase-id>`   [risk+mvp+verification]
       files:    internal/cmd/verifyscope.go, internal/cmd/verifyscope_test.go,
                 internal/cmd/verify.go, assets/prompts/verify.md,
                 internal/cmd/verify_prompt_test.go, README.md
       covers:   c-5
       description: New cobra subcommand `scope <phase-id>` registered at verify.go:162-164
                 beside finalize/results/status. Loads tests.json via
                 verify.LoadTests(verify.FilePaths(root, id)); missing → error
                 "no verify run recorded for <id> — run `dross verify <id>` first" through the
                 RunE path (non-zero exit), nothing on stdout. Text mode: scope header
                 (source, base, N files), each in-scope file with its raw hunks, then per leg
                 the ranged files as `s-e (pad N)` and the whole-file files with their reason.
                 `--json` marshals a `verify.Provenance{Phase, GeneratedAt, Files, Hunks,
                 Legs[]{Name, Tool, Files, Ranges, WholeFile}}` copied field-for-field from
                 the loaded Tests — the same values, no re-rendering, nothing else on stdout.
                 verify.md gains one paragraph under "The score covers only this phase's
                 changed files" (line 72) naming per-leg `ranges` / `whole_file`, the
                 `dross verify scope <phase>` command, and the rule that a whole-file leg is
                 never called ranged. README verify command table gains the row.
                 `make install` after.
       contract: TestVerifyScopeWithoutARunNamesTheFix — no tests.json: runCmd returns a non-nil error whose text contains the phase id and "dross verify <id>"; stdout is empty; a nil return or a generic "not found" fails
       contract: TestVerifyScopePrintsRawAndEffective — a seeded tests.json under chdirDross (stryker leg ranged src/a.ts {1,37} pad 25 with raw hunk {10,12}; gremlins leg whole-file): stdout contains "src/a.ts", "10-12", "1-37 (pad 25)", and "whole-file" on the same line as "adapter-lacks-range-runner"; dropping any of the four sections fails
       contract: TestVerifyScopeJSONIsTheRecordVerbatim — `--json` stdout unmarshals into verify.Provenance; Legs[i].Ranges / Legs[i].WholeFile are reflect.DeepEqual to LoadTests(...).Languages[i].Ranges / .WholeFile and Files/Hunks to Scope.Files/Scope.Hunks; a pretty-printed "1-37" string in place of {start,end,pad} fails; stdout has no prose before the "{" (R10)
       contract: TestVerifyScopeOnAPreProvenanceRecordSaysSo — a tests.json written before this phase (no ranges/whole_file on any leg) neither panics nor prints a leg as ranged; each leg prints as "no range provenance recorded" (R7) [risk]
       contract: TestVerifyPromptReadsRangeProvenance — verify.md contains "whole_file", "ranges" and "dross verify scope <phase>" [risk+verification]
       contract: TestReadmeDocumentsVerifyScope — README.md contains a row starting "| `dross verify scope <phase>`" [risk+mvp]
       depends_on: [t-4]

  t-7  Prove narrowing survives the real scope pipeline   [risk]
       files:    internal/cmd/verify_range_e2e_test.go, internal/cmd/verify_test.go
       covers:   c-1, c-2
       description: A `stubRangeAdapter` (embeds the existing stubMutationAdapter at
                 verify_test.go:755, implements RunRanges, records the map it received)
                 driven through the real `dross verify` over a scopedVerifyRepo
                 (verify_test.go:803) with a two-line edit, so phaseScope → containScope →
                 mutationCandidates (Contained.Rel(), verify.go:1226) → RunScoped → PlanRanges
                 → RunRanges runs unstubbed. A second case with a Workdir-style path checks
                 stryker.rangeKey (workdir + "/" + trimmed) finds the same key Scope.Hunks
                 uses. Every other test in the phase feeds a hand-built Scope; this is the one
                 that fails if the pipeline's path vocabulary and the adapter's ever differ —
                 a mismatch that would otherwise hide behind the informational
                 file-absent-from-hunks reason and never narrow a run (R9).
       contract: TestRangedDispatchReachesTheAdapterEndToEnd — the adapter receives a non-empty map keyed by the edited file's repo-relative slash path; tests.json's languages[0].ranges names that file with pad 25 and whole_file is empty; if Scope.Hunks keys and the dispatched paths ever differ in form, this fails while every hand-built-scope test passes
       contract: TestRangeKeyMatchesScopeKeysUnderAWorkdir — runArgs on a Stryker with Workdir "web" given trimmed "src/a.ts" and ranges keyed "web/src/a.ts" emits `src/a.ts:1-30`; a rangeKey that forgets the workdir prefix emits the bare path
       depends_on: [t-4]

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-3, t-4, t-5, t-7 |
| c-3 | t-3, t-4, t-5 |
| c-4 | t-2 |
| c-5 | t-6 |

Not a task (all three drafts agree): deleting `feat/mutate-changed-lines` local +
origin. The lock times it "once this phase ships"; it goes on the ship
checklist / handoff, not in plan.toml.

## Disagreements

1. **Port as one task or two; one commit or two.**
   verification splits the picks into t-1 (1da3f73, 7 files) and t-2 (21e354b,
   2 files, own wave) to keep each commit's rationale reviewable on its own.
   risk lands both in one task with `cherry-pick -x` each. mvp lands both with
   `cherry-pick -n` into a single squashed commit.
   *Default:* one task, two `-x` picks in order (risk). *Why it matters:* a
   separate wave for a 2-file pick that applies clean delays every downstream
   task by a wave for no verification gain; but `-n` squashing discards the
   second commit's message, which carries the measured mutant-count table the
   landing_method lock exists to preserve (the table is also in the
   hunkContextLines doc comment, so the loss is of history, not of code).
   /dross-execute's one-commit-per-task convention is in tension with two
   picks in one task — the executor should confirm the tooling tolerates two
   commits (plus an adapt commit) for t-1, or fall back to verification's split.

2. **Pure classifier split from wiring (t-3 / t-4) vs one record task.**
   risk separates a pure `PlanRanges` (t-3, tested directly against the four
   causes) from the RunScoped/detached wiring (t-4). mvp and verification do
   both in one task, testing the causes through RunScoped with fake adapters.
   *Default:* split (skeleton). *Why it matters:* the split keeps every task at
   ≤5 files and tests the severity lock against a pure function rather than
   through dispatch side-effects; the cost is one wave in which t-3's planner
   is uncalled code, and a task graph one wave longer. Merging would make t-3
   a 7-file, 13-contract task spanning two packages.

3. **Reason values: closed enum vs prose with numbers.**
   risk and verification: four fixed constants, closed-set test. mvp:
   "malformed range %d-%d" carrying the hunk's numbers in the reason itself.
   *Default:* closed enum; the numbers go into the Scope.Degraded line (which
   the severity lock already requires for malformed). *Why it matters:* a
   value with numbers cannot be a closed set, and c-3 says "named reason" —
   a name is an identifier, not a sentence; the Degraded line loses nothing.

4. **c-4: tests only vs tests plus conditional accept.**
   mvp: tests are the whole disposition, no `dross survivor accept` — the
   tracked-path-containment FLAG (verify.toml:102) says these "must not be
   accepted". risk and verification: tests first; if the drain still lists :81
   afterwards, accept it citing non-viability (ARITHMETIC_BASE on a string `+`
   does not compile).
   *Default:* conditional accept, drain output decides. *Why it matters:* the
   FLAG argues the three are a coverage gap, not a ceiling — which the tests
   address; an accept for a non-viable mutant is a different claim and does not
   contradict it. But an accept written reflexively would. The executor must
   observe the drain before taking the accept path, and gremlins should report
   a non-compiling mutant as NOT VIABLE rather than survived, so the accept is
   expected to be unnecessary.

5. **Verify prompt edit (assets/prompts/verify.md).**
   risk (paragraph: never call a whole-file leg ranged) and verification (name
   the subcommand) include it; mvp rejects it as speculative — no criterion
   names the prompt.
   *Default:* include, one paragraph in t-6 naming both the fields and the
   subcommand, pinned by one test. *Why it matters:* /dross-verify's agent reads
   tests.json and verify.toml through that prompt; c-3's "never reads as a
   ranged one" is only enforced on the human path unless the prompt says what
   to read. Cost is one paragraph and one phrase pin.

6. **`--json` verbatim: raw tests.json bytes vs a Provenance projection.**
   risk: write the tests.json bytes unchanged, sha256-equal (survives fields a
   newer writer adds). mvp and verification: a struct copied field-for-field
   from LoadTests, DeepEqual-asserted (does not bury the record under every
   surviving mutant).
   *Default:* projection. *Why it matters:* "the same record" in c-5 is the
   provenance record just enumerated, not the whole file; the human view is
   built from the same LoadTests struct, so the projection cannot drift from
   it. The cost is that a field the Provenance struct does not know is not
   emitted — acceptable because the struct is defined beside the fields it
   projects.

7. **verify.toml shape: strings vs tables.**
   risk and mvp: `ranges = ["file:start-end"]`, `whole_file = ["file — reason"]`.
   verification: `[[summary.leg.range]]` tables `{file, start, end}` and a
   `whole_file` inline table.
   *Default:* strings. *Why it matters:* verify.toml is read by the agent and by
   humans, not parsed for numbers (tests.json is the machine record); strings
   keep the TOML flat and avoid a `file`-tagged struct that a future widening of
   pathShapedTags would catch. Structured would be more robust if a later phase
   ever computes over verify.toml's ranges.

8. **Detached leg: exported `WholeFileLeg(files, reason)` helper vs the planner.**
   risk: a small exported helper that returns the whole-file map for a reason.
   mvp (`detachedLeg` calling LegProvenance) and verification (through
   PlanRanges): the detached path calls the same planner over the gremlins
   adapter.
   *Default:* planner (mvp+verification). *Why it matters:* one writer for
   attached and detached is the point of R4; a second helper that hard-codes
   the reason is a second writer by another name, even if today it produces the
   same map.

9. **End-to-end pipeline task (t-7).**
   Only risk has it; mvp and verification test narrowing exclusively through
   hand-built Scopes. Neither rejects it explicitly.
   *Default:* keep it. *Why it matters:* every other contract in the phase
   passes if Scope.Hunks keys and the dispatched paths differ in form — the run
   would fall to file-absent-from-hunks, which is informational by the severity
   lock, and never narrow. One test through the real command is the only thing
   that catches that.
