# Planner draft — lens: verification

Method: for each criterion I wrote the test contract that would prove it
*from the artifact a reader actually opens* (argv, tests.json bytes, verify.toml
bytes, stdout, exit code), then derived the smallest task that makes that
contract satisfiable. Tasks are ordered by what the contracts need, not by
layer.

## Test contracts, designed first

**c-1 (ranged runner landed).** The proof is the argv and the dispatch seam,
both already written on `feat/mutate-changed-lines` and currently not
compiling on HEAD:
- `Stryker.runArgs(["src/a.ts"], {"src/a.ts": [{10,12}]})` → `--mutate` value is
  exactly `src/a.ts:10-12` (TestRunArgsEmitsLineRanges); with a bracket path the
  range sits AFTER the escaped glob (TestRunArgsAppendsTheRangeAfterEscaping).
- `RunScoped` with a fake `RangeRunner` and hunks calls `RunRanges` with the
  padded ranges, never `Run` (TestRunScopedPassesPaddedRanges); with a plain
  adapter it calls `Run` (TestRunScopedStillRunsAnAdapterThatCannotRange); with
  no hunks / nil scope it calls `Run` (TestRunScopedWithNoHunksRunsWholeFiles,
  TestRunScopedWithANilScopeRunsWholeFiles).
- `var _ RangeRunner = (*Stryker)(nil)` compiles (TestStrykerImplementsRangeRunner)
  and `(*Gremlins)(nil)` does NOT satisfy it (new one-liner — the criterion
  says gremlins runs whole files and today nothing pins that it cannot range).
- `checkInstrumented` tolerates a narrowed file absent from the report only
  while the head buffer lacks `strykerDropWarningText` (the three
  TestCheckInstrumented* cases — adapted from `head.buf.String()` to
  `head.contains()` which is what HEAD's `toolfail.go` exposes).
- `padAndMerge([{10,12}])` = `[{1,37}]` (clamped at 1, +25 on the end);
  `[{10,12},{30,31}]` merges to one range; `[{10,12},{200,201}]` stays two.

**c-2 (provable scope).** Read off the BYTES, not the struct:
- After `RunScoped` with a ranging adapter and hunk `{10,12}` on `src/a.ts`,
  `Tests.Save` → raw tests.json contains `"ranges":{"src/a.ts":[{"start":1,"end":37,"pad":25}]}`
  under `languages[0]`, and `"hunks":{"src/a.ts":[{"start":10,"end":12}]}` under
  `scope` — both in one file, so raw vs effective are diffable.
- `LoadTests` round-trips `Ranges` and `WholeFile` exactly (reflect.DeepEqual).
- `Skeleton` → `Verify.Save` → raw verify.toml contains `pad = 25` and a
  `[[summary.leg.range]]` block naming `src/a.ts`, `start = 1`, `end = 37`.
- The gremlins leg of the same run has `ranges` ABSENT and `pad` absent
  (omitempty) — a leg that ranged nothing must not claim a pad.

**c-3 (every fallback named).** One contract per fallback cause, keyed on a
named constant, plus the severity split from the lock:
- plain adapter → `leg.WholeFile[f] == WholeFileNoRangeRunner` for EVERY file
  in the leg; `Scope.Degraded` unchanged (informational).
- ranging adapter, `Scope.Hunks == nil` → `WholeFile[f] == WholeFileNoHunks`
  for every file AND `Scope.Degraded` gains one entry containing the adapter
  name (structural loss on a capable adapter).
- ranging adapter, hunks for `a.ts` only, files `a.ts,b.ts` → `WholeFile["b.ts"]
  == WholeFileAbsentFromHunks`, `Ranges["a.ts"]` set, `Degraded` unchanged.
- ranging adapter, raw hunk `{Start:10,End:5}` on `a.ts` → `WholeFile["a.ts"]
  == WholeFileMalformedRange`, `RunRanges` receives NO entry for `a.ts`,
  `Degraded` gains an entry naming `a.ts` and `10-5`. Checked on the RAW hunk
  before padding — padAndMerge would clamp `{0,3}` into a legal range and hide
  the malformation.
- `dross verify` stdout for a gremlins-only phase contains
  `whole-file` and `adapter-lacks-range-runner`; for a stryker phase with one
  ranged file it contains `ranged 1 file(s) (pad 25)`. A run that measured whole
  files therefore never prints as a ranged one.
- `dross verify results` (detached, gremlins) writes a go leg whose `whole_file`
  names every dispatched file with `adapter-lacks-range-runner` — the detached
  path builds its `LanguageRun` by hand at internal/cmd/verify.go:646 and would
  otherwise be the one leg with no provenance.

**c-4 (three compilefence survivors).** The survivors are an ordinary
coverage gap: `AssertDoesNotCompile` (:61) and `AssertCompiles` (:80-81) are
called only from other packages, and gremlins scopes per package. The
contracts are the four arms those two functions have:
- `AssertDoesNotCompile(rec, emptyLiteral, "anything")` on a recording TB →
  Fatalf text contains `COMPILED but should not have` (kills :61 negation —
  the mutant would instead pass a compiling fixture).
- `AssertDoesNotCompile(rec, setsUnknownField, "cannot refer to unexported field")`
  → Fatalf text contains `WRONG reason` (pins the branch below :61 so the
  negation cannot escape through the second Fatalf).
- `AssertCompiles(rec, setsUnexportedField)` → Fatalf text contains
  `compile fence itself is broken` (kills :80 negation and, if viable, the :81
  `+` — the message is the concatenation).
- `AssertCompiles(rec, emptyLiteral)` → no Fatalf (the other side of :80).
- Gate: `dross survivor drain internal/compilefence --phase mutation-range-provenance`
  reports 0 outstanding and none of the keys `365503d33114f65e`,
  `5d74aadbbae679b7`, `695a2b44f32b1b43` appear. If :81 (`ARITHMETIC_BASE` on
  a string `+`) turns out non-viable/equivalent under gremlins, it is accepted
  via `dross survivor accept internal/compilefence/compilefence.go:81 --op ARITHMETIC_BASE --reason ...`
  citing that — the drain contract is the same either way.

**c-5 (`dross verify scope`).** Exit code and bytes:
- no tests.json for the phase → returns an error whose text contains
  `dross verify <phase-id>` (the fix), exit non-zero via cobra.
- text mode over a tests.json with one stryker leg (ranged `src/a.ts`
  `{1,37}` pad 25) and one gremlins leg → stdout contains `src/a.ts`, the raw
  hunk `10-12`, the effective `1-37 (pad 25)`, and `whole-file` beside
  `adapter-lacks-range-runner` for the go leg.
- `--json` → stdout decodes into `verify.Provenance` and its `legs[i].ranges`
  / `legs[i].whole_file` are `reflect.DeepEqual` to `LoadTests(...).Languages[i].Ranges`
  / `.WholeFile`, and `files`/`hunks` equal `Scope.Files`/`Scope.Hunks` —
  "verbatim" means the same values, not a re-rendering.
- verify.md names the subcommand (prompt phrase pinned in verify_prompt_test.go).

## Plan

Phase mutation-range-provenance — 6 tasks across 4 waves

Wave 1
  t-1  Cherry-pick 1da3f73 onto HEAD, adapt
       files:    internal/mutation/adapter.go, internal/mutation/stryker.go,
                 internal/mutation/range_test.go, internal/mutation/argv_test.go,
                 internal/mutation/stryker_test.go, internal/verify/verify.go,
                 internal/verify/range_dispatch_test.go
       description: `git cherry-pick 1da3f73` (RangeRunner interface, Stryker.RunRanges,
                 runArgs ranges, narrowedSet, checkInstrumented narrowed tolerance,
                 verify.runAdapter). Resolve the two known conflicts: stryker.go
                 `strings.Contains(head.buf.String(), strykerDropWarningText)` →
                 `head.contains(strykerDropWarningText)` (headBuffer moved to
                 toolfail.go, buf is private-by-convention now); verify.go RunScoped's
                 `report, err := a.Run(...)` hunk sits beside HEAD's
                 `mutation.RecordLegError(err)` block — keep both. range_test.go's
                 three checkInstrumented tests construct `&headBuffer{limit: 1<<10}`
                 which still exists; the `Write` call is unchanged. Add
                 TestGremlinsDoesNotImplementRangeRunner. `make install`.
       covers:   c-1
       contract: TestRunArgsEmitsLineRanges — runArgs with {"src/a.ts":[{10,12}]} emits
                 --mutate "src/a.ts:10-12"; drop the range suffix and it fails
       contract: TestRunArgsAppendsTheRangeAfterEscaping — for "web/src/routes/[id]/+page.ts"
                 the ":10-12" follows the escaped "[[]id[]]" form; escape-after-append fails it
       contract: TestRunArgsFallsBackWholesaleWhenAnyRangeIsMalformed — one bad range in a
                 file's set yields the bare path and no "a.ts:1-2" beside it
       contract: TestRunScopedStillRunsAnAdapterThatCannotRange — a plain adapter with
                 hunks present gets Run, not a type-assertion panic or a skipped leg
       contract: TestCheckInstrumentedToleratesANarrowedFileWithNoMutants passes and
                 TestCheckInstrumentedStillRefusesWhenStrykerWarnedItDropped still errors
                 — the drop-warning guard survives narrowing
       contract: TestStrykerImplementsRangeRunner compiles; new
                 TestGremlinsDoesNotImplementRangeRunner asserts `_, ok := any(&Gremlins{}).(RangeRunner); !ok`
       contract: `go test ./internal/mutation ./internal/verify` green and `go vet ./...` clean
       depends_on: []

  t-6  Kill or accept the compilefence survivors
       files:    internal/compilefence/compilefence_test.go, .dross/survivors.toml
       description: Add a recording testing.TB (embeds *testing.T, captures Fatalf via a
                 recovered panic) and four tests exercising both arms of
                 AssertDoesNotCompile and AssertCompiles from inside the package. Run
                 `dross survivor drain internal/compilefence --phase mutation-range-provenance`;
                 if :81 survives as a non-viable string-concat mutant, accept it through the
                 CLI with a reason that says so. Never hand-edit survivors.toml.
       covers:   c-4
       contract: TestAssertDoesNotCompileRefusesACompilingFixture — emptyLiteral through a
                 recording TB yields a Fatalf containing "COMPILED but should not have";
                 negate :61 and no Fatalf fires
       contract: TestAssertDoesNotCompileRefusesTheWrongReason — setsUnknownField with
                 wantMsg "cannot refer to unexported field" yields a Fatalf containing
                 "WRONG reason"
       contract: TestAssertCompilesRefusesABrokenFixture — setsUnexportedField yields a
                 Fatalf containing "compile fence itself is broken"; negate :80 and the
                 Fatalf fires on emptyLiteral instead (TestAssertCompilesAcceptsACompilingFixture
                 asserts no Fatalf there)
       contract: `dross survivor drain internal/compilefence --phase mutation-range-provenance`
                 prints "0 outstanding" and lists none of 365503d33114f65e,
                 5d74aadbbae679b7, 695a2b44f32b1b43 — killed, or accepted with a reason
       depends_on: []

Wave 2 (depends t-1)
  t-2  Cherry-pick 21e354b: pad and merge hunks
       files:    internal/verify/verify.go, internal/verify/range_dispatch_test.go
       description: `git cherry-pick 21e354b` (hunkContextLines = 25 with its measured
                 rationale, padAndMerge, runAdapter uses it). Applies cleanly on top of t-1's
                 runAdapter; keep the constant untuned (pad_constant lock).
       covers:   c-1
       contract: TestPadAndMergeWidensEachHunk — {10,12} becomes {1,37}; a pad of 24 or 26
                 fails the exact-value assert
       contract: TestPadAndMergeClampsToTheFirstLine — Start never < 1, so runArgs'
                 Start<=0 fallback is unreachable from a real hunk
       contract: TestPadAndMergeMergesOverlappingHunks — {10,12},{30,31} → one range
                 {1,56}; TestPadAndMergeKeepsDistantHunksSeparate — {10,12},{200,201} stay two
       contract: TestRunScopedPassesPaddedRanges — the ranging adapter receives {1,37}
                 for hunk {10,12}, not the raw {10,12}
       depends_on: [t-1]

Wave 3 (depends t-2)
  t-3  Record effective ranges and fallbacks on the leg
       files:    internal/verify/verify.go, internal/verify/range_record_test.go,
                 internal/verify/range_dispatch_test.go
       description: Add `EffectiveRange{Start,End,Pad int}` and on LanguageRun
                 `Ranges map[string][]EffectiveRange json:"ranges,omitempty"` +
                 `WholeFile map[string]string json:"whole_file,omitempty"`. Add the four
                 reason constants (WholeFileNoRangeRunner, WholeFileNoHunks,
                 WholeFileAbsentFromHunks, WholeFileMalformedRange). Replace runAdapter's
                 inline logic with exported `PlanRanges(a, files, scope) RangePlan`
                 returning ranges, wholeFile and degraded reasons; RunScoped dispatches
                 from the plan, stamps Ranges/WholeFile on the leg (both success and
                 error legs), and appends the plan's degraded reasons to scope.Degraded.
                 Malformed check runs on the RAW hunk, before padAndMerge. Extend the
                 rangingAdapter fake to record the ranges it received.
       covers:   c-2, c-3
       contract: TestLegRecordsEffectiveRangesWithPad — raw tests.json bytes after
                 Tests.Save contain `"ranges":{"src/a.ts":[{"start":1,"end":37,"pad":25}]}`
                 and `"hunks":{"src/a.ts":[{"start":10,"end":12}]}`; LoadTests round-trips
                 both maps DeepEqual
       contract: TestPlainAdapterRecordsNoRangeRunnerPerFile — every file in the gremlins
                 leg maps to WholeFileNoRangeRunner; Ranges is nil; Scope.Degraded length
                 unchanged
       contract: TestHunklessScopeOnACapableAdapterDegrades — ranging adapter, Hunks nil:
                 every file → WholeFileNoHunks AND Scope.Degraded gains one entry naming
                 the adapter; the same scope on a plain adapter adds nothing to Degraded
       contract: TestFileAbsentFromHunksIsInformational — files a.ts,b.ts, hunks only for
                 a.ts: WholeFile["b.ts"]==WholeFileAbsentFromHunks, Ranges has a.ts only,
                 Degraded unchanged
       contract: TestMalformedRawHunkFallsBackAndDegrades — hunk {10,5} on a.ts: the
                 adapter's RunRanges map has no "a.ts" key, WholeFile["a.ts"]==
                 WholeFileMalformedRange, Degraded gains an entry containing "a.ts" and
                 "10-5"; pass {0,3} instead and it is caught the same way (not clamped away)
       contract: TestFailedLegStillCarriesItsPlan — an adapter returning an error still
                 records WholeFile/Ranges on the error leg (a leg that died mid-run must
                 not read as unplanned)
       contract: TestEveryPersistedTextSinkIsDeclared (internal/cmd) stays green — the new
                 fields are maps, not string/[]string sinks, so no toolfence entry is owed
       depends_on: [t-2]

Wave 4 (depends t-3)
  t-4  State ranges in verify.toml and verify output
       files:    internal/verify/verify.go, internal/verify/range_summary_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_scoping_test.go,
                 internal/cmd/verify_results_test.go
       description: LegSummary gains `Pad int toml:"pad,omitempty"`,
                 `Ranges []RangeSummary toml:"range,omitempty"` ({File,Start,End}) and
                 `WholeFile map[string]string toml:"whole_file,omitempty"`; Skeleton fills
                 them from the leg for both success and error legs. printScopeSummary prints
                 one line per leg: `ranged N file(s) (pad 25)` and `whole-file N file(s) —
                 <reason>: <files>` grouped by reason. collectDetached builds its go leg
                 through PlanRanges so the detached path records
                 adapter-lacks-range-runner too. `make install`.
       covers:   c-2, c-3
       contract: TestVerifyTomlStatesEffectiveRanges — raw verify.toml bytes after
                 Skeleton+Save contain `pad = 25`, a `[[summary.leg.range]]` with
                 `file = "src/a.ts"`, `start = 1`, `end = 37`, and a `whole_file` table
                 mapping "src/b.ts" to the absent-from-hunks reason; LoadVerify round-trips
       contract: TestGremlinsLegHasNoPad — the gremlins leg's summary omits `pad` and
                 `range` entirely (grep the bytes), and its whole_file names every file
       contract: TestVerifyOutputNamesWholeFileFallback — TestScopingAttributionHoldsEndToEnd's
                 fixture through captureStdout: stdout contains "whole-file" and
                 "adapter-lacks-range-runner"; a stub RangeRunner in the same harness prints
                 "ranged 1 file(s) (pad 25)" and never the word "whole-file" for that file
       contract: TestCollectRecordsWholeFileProvenance — extend
                 TestCollectWritesTheSameArtefactsAnAttachedRunWould: the collected go leg's
                 whole_file has an entry for "a.go" equal to adapter-lacks-range-runner
       contract: TestLegacyVerifyTomlLoads (existing) still green — a verify.toml without
                 the new keys round-trips unchanged
       depends_on: [t-3]

  t-5  Add `dross verify scope` subcommand
       files:    internal/cmd/verify_scope_cmd.go, internal/cmd/verify_scope_cmd_test.go,
                 internal/cmd/verify.go, assets/prompts/verify.md,
                 internal/cmd/verify_prompt_test.go
       description: New cobra command `scope <phase-id>` registered beside results/status/
                 finalize; loads tests.json via verify.LoadTests, builds a
                 `verify.Provenance{Files, Hunks, Legs[]{Name,Tool,Ranges,WholeFile}}`
                 struct (defined in internal/verify/verify.go under t-3's types — add it
                 here if t-3 did not), prints text or `--json` (json.MarshalIndent of the
                 struct, nothing else on stdout). Missing tests.json returns an error naming
                 `dross verify <phase-id>`. verify.md §files list gains one line naming the
                 subcommand. `make install`.
       covers:   c-5
       contract: TestVerifyScopeWithoutARunNamesTheFix — no tests.json: runCmd returns a
                 non-nil error whose text contains "dross verify <id>"; stdout is empty
       contract: TestVerifyScopePrintsRawAndEffective — a seeded tests.json (stryker leg
                 ranged src/a.ts {1,37} pad 25, hunk {10,12}; gremlins leg whole-file):
                 stdout contains "src/a.ts", "10-12", "1-37 (pad 25)", and "whole-file"
                 on the same line as "adapter-lacks-range-runner"
       contract: TestVerifyScopeJSONIsTheRecordVerbatim — `--json` stdout unmarshals into
                 verify.Provenance; Legs[i].Ranges and Legs[i].WholeFile are DeepEqual to
                 LoadTests(...).Languages[i].Ranges/WholeFile, Files/Hunks DeepEqual to
                 Scope.Files/Scope.Hunks; stdout has no prose before the "{"
       contract: TestVerifyPromptNamesScopeSubcommand — verify.md contains
                 "dross verify scope <phase>"
       depends_on: [t-3]

## Coverage

- c-1 → t-1, t-2
- c-2 → t-3, t-4
- c-3 → t-3, t-4
- c-4 → t-6
- c-5 → t-5

## Judgment calls

- **Two cherry-pick tasks, not one.** 1da3f73 is 7 files across two packages and
  has two real conflicts; 21e354b is 2 files and applies clean on top. Keeping
  them separate keeps each commit's measured rationale in its own history
  (the reason the landing_method lock chose cherry-pick over rewrite) and makes
  the conflict resolution reviewable on its own.
- **t-1 is 7 files, over the 5-file guide.** Rejected splitting by package: a
  cherry-pick is one git operation and splitting it means hand-carving a
  commit, which is the "fresh rewrite" the lock forbids.
- **Malformed-range detection moved up into verify.PlanRanges, on the raw hunk.**
  The ported Stryker fallback stays as belt-and-braces, but it cannot be the
  recorder — it lives in the adapter, prints to stderr, and runs after
  padAndMerge has already clamped `{0,3}` into a legal range. c-3 needs the
  reason on the leg and the lock needs it on Scope.Degraded; both are only
  reachable from the verify side.
- **whole_file is `map[string]string`, not `[]struct{File,Reason}`.** The lock
  says map file→reason; a struct with a `reason` tag would also be an
  undeclared toolfence sink (the vocabulary includes "reason") and owe a
  NotToolStream declaration. The map shape keeps TestEveryPersistedTextSinkIsDeclared
  green without a registry edit. Reasons are named constants, not prose.
- **Per-file NoRangeRunner entries on gremlins legs are verbose (56 identical
  values on a big Go phase).** Kept: c-3 says "per file/adapter" and the
  alternative — a leg-level flag — would leave `whole_file` empty on the one
  leg that is entirely whole-file. Terminal output groups by reason so the
  screen stays one line.
- **Detached path goes through PlanRanges rather than a hand-written map.**
  collectDetached builds LanguageRun by hand; one shared planner is the only
  way the attached and detached records cannot drift.
- **`--json` emits a Provenance projection, not the whole tests.json.** c-5
  says "the same record verbatim" — the values are copied unchanged and the
  contract asserts DeepEqual against LoadTests, but surviving-mutant lists are
  not provenance and dumping them would bury the record.
- **c-4 contract is the drain, not "gremlins re-run over the whole repo".**
  `dross survivor drain internal/compilefence --phase mutation-range-provenance`
  treats survivors routed to this phase as outstanding, which is exactly the
  "not re-listed" clause; it is package-scoped so it runs in minutes on the
  laptop.
- **:81 may be a non-viable mutant.** ARITHMETIC_BASE on a string `+` should
  fail to compile; if the drain shows it still listed, the accept path with a
  reason saying so satisfies c-4 and is the honest record. Not pre-deciding
  it — the drain output decides.
- **Feature-branch deletion is not a task.** The lock says delete local +
  origin "once this phase ships"; that is a ship-time action after plan
  execution, so it belongs on the ship checklist, not in plan.toml.
- **Prompt edit folded into t-5** rather than its own task: one line in
  verify.md plus a phrase pin is under ten minutes.
