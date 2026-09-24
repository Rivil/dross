# Plan Review — exec-taint-enumeration

Reviewed: 2026-09-24
Plan: 21 tasks across 7 waves

## BLOCKING
- [wave-order / test-contract] t-15's scoped gate fails by construction in wave 5. Its contract says "the scoped gate over the 14 migrated files reports any finding". But gitTrim (internal/cmd/ship_recover.go:263) returns `strings.TrimSpace(string(out))` with no marker until t-19 in wave 6, and 10 of t-15's 14 files use gitTrim results. One concrete case: internal/cmd/basebranch.go:79-87 loops over `d.Ahead`, which holds rev-list output set at originpush.go:40. It passes each SHA into a second gitTrim (diff-tree, :80), then formats both the SHA and a diff-tree path into `fmt.Errorf`. That is an error escape in a t-15 file, and only t-19's marker clears it. t-19's own description admits that phase.go, ship_recover.go and cleantree.go carry t-19-type findings, and t-15's gate covers those files too. The panel called the same failure the top coverage defect in the gitCombined case: a gate that goes red on findings no task owns yet.
  Suggestion: limit t-15's gate to findings whose origin is gitCombined's `CombinedOutput` (ship_recover.go:278), or clear gitTrim's output before t-15 runs. Settle this together with the gitTrim-marker item below, because that fix changes which task owns these findings.

- [coverage — c-4/c-9 remediation] A live raw read of a declared path field has no owning task. survivor.Acceptance.File (`toml:"file"`, internal/survivor/store.go:39) is serialized into the committed .dross/survivors.toml. It is opened raw at internal/survivor/stale.go:101: `os.ReadFile(filepath.Join(root, a.File))`, with no pathfence.Contain. The field lives in a schema dir that isn't listed, under a tag word that isn't listed (`file` is not in pathShapedTags). That makes it exactly the c-9 blind spot, and it is live today. t-8 will enumerate the field and must declare it. Either disposition makes t-12's live gate trip: Consumed means a raw os.ReadFile with no Contain, and NotConsumed means the field reaches os.*. t-12 forbids redeclaring the field to escape. The fix is production code in internal/survivor/stale.go, probably plus a Carrier entry that pulls in pathfence_carrier_test.go, and no task lists either file. t-12's files are all tests. Exec taint has a discovery step in t-9; path taint has none.
  Suggestion: give the stale.go fix to t-8 or t-12 and add the file to that task. Give t-8 a discovery step like t-9's: when it lands, run the enumeration and the path policy over the live tree, and `dross task add` any site no task owns.

- [locked-decision: clearance_model, taint_sources] t-19's single gitTrim marker clears every gitTrim result, for every caller now and later. The plan puts one marker at gitTrim's conversion (ship_recover.go:271, `return strings.TrimSpace(string(out)), nil`) with prose "true for every caller". gitTrim is called from 22 files, and not all of them receive a SHA or branch:
  - verifyscope.go:87 reads a `git diff -U0` patch body.
  - verifyscope.go:71 reads `--name-only` path lists that end up in tests.json.
  - repair_state.go:87 and phase_backfill.go:101 read `git log` subjects.

  clearance_model places the marker "at the conversion site" where "a SHA or branch [is] sliced out of output". For these callers gitTrim is not that site. taint_sources rejects deciding which binaries are safe, and its reasoning names git log output as able to carry secrets. A marker on the generic runner makes that decision one helper at a time. It would also clear any future gitTrim caller (say `gitTrim(dir, "show", ref)`) with no review. t-10's orphan rule can't catch this, because the marker always clears something.
  Suggestion: put markers at the caller sites where the SHA, branch or path is sliced out. Alternatively, split gitTrim in two: a ref-resolving helper for rev-parse, symbolic-ref, merge-base and rev-list, whose output shape is fixed, may carry one marker, while diff, log and show reads go through an unmarked runner. Either way t-19's file list grows (at least verifyscope.go, repair_state.go and phase_backfill.go).

## FLAG
- [test-contract, c-8] t-21's c-8 evidence can't be collected as written. Three problems:
  - origin/main has no cmd/testsummary. The timing table arrived with #132 on milestone/v1.7. main's latest CI run (2026-09-04, failed) predates every v1.7 phase. So "main's latest run + 15s" has no table and the wrong baseline.
  - CI runs only on pull_request and on pushes to main, and /dross-ship opens the phase PR only after verify passes. At verify time there is no "phase PR's CI timing table".
  - "The local -race elapsed time" is measured on the laptop, where this project already treats local internal/cmd runs as unreliable. t-1 correctly uses `dross test` instead.

  A single package-total delta on internal/cmd is also noisy against a 15s budget.
  Suggestion: use the latest PR run with the timing table on the milestone/v1.7 base (#132's) as the baseline. Measure with the remote runner's `go test -race -json ./internal/cmd | go run ./cmd/testsummary` before and after, or sum the scan tests' own durations from the -json stream. Write down which number verify reads.

- [test-contract, terminal_definition] Nothing classifies writers that print to stdout implicitly. t-5's terminal set is os.Stdout/os.Stderr plus cobra OutOrStdout/ErrOrStderr, and "any other external callee receiving taint is an escape". Read literally, that makes all of these escapes: `fmt.Println(out)`, `fmt.Printf`, cobra's `cmd.Println`/`cmd.Printf`, and the package wrappers `Print`/`Printf` in internal/cmd/root.go:144-147. Non-test source has about 700 such print calls. If they count as escapes, t-9's discovery run floods and the burn-downs start adding markers to prints. If they count as terminal, no contract pins that, and a regression in either direction goes unnoticed.
  Suggestion: state the classification in t-5. Add must-not-trip rows for `fmt.Println(out)` and `cmd.Println(out)`, and a must-trip row for `fmt.Fprintln(&buf, out)`.

- [test-contract, c-4/c-9] Nothing can prove a not_paths.txt row wrong. t-12's sources are only pathfence.Fields() entries, so a path field filed in not_paths.txt is never traced, and no check confirms that a not_paths.txt field never reaches an os.* call. fields.go's own standard is that a disposition must be "a declaration a test can falsify". The survivor.Acceptance.File case above could be turned green just by filing the field in not_paths.txt.
  Suggestion: have t-12 treat not_paths.txt rows like NotConsumed, so a row that reaches any os.* function is a finding.

- [test-contract, c-9] t-8 enumerates only tagged fields whose underlying type is string or []string, and no contract says what happens to other path-carrying shapes:
  - verify's `WholeFile map[string]string` (internal/verify/verify.go:237, json `whole_file`) and `Ranges map[string][]EffectiveRange` are keyed by file path in tests.json.
  - An untagged exported string field is serialized under its Go name by both BurntSushi/toml and encoding/json.
  - `*string` fields.

  A future path field in any of these shapes would sit outside both registries.
  Suggestion: pin each shape as enumerated or explicitly out of scope, each with a fixture row. Write the boundary into the path-containment entry t-21 rewrites in ARCHITECTURE.md.

- [locked-decision: marker_grammar / clearance_model] t-10 clears "the tainted values defined on [the marked] line" without requiring that line to be a conversion. A marker directly above `out, err := ghCommand(args...).Output()`, or above `c.Stdout = &buf`, clears the raw output for every later use, error paths included. One comment would undo the fix-not-mark rule. Both locks put the marker above the conversion. The CANARY tests protect only today's sites.
  Suggestion: add a t-10 must-trip row: a marker whose bound line defines a source value (an Output/CombinedOutput result, or a Stdout/Stderr referent) is itself a finding.

- [test-contract, clearance_model] t-5 clears any external call's "numeric/bool" result. byte and rune are numeric types (uint8/int32). So `utf8.DecodeRune(out)`, or `bufio.Reader.ReadByte`/`ReadRune` over a StdoutPipe, yields clean values that `b.WriteRune(r)` or `string(r)` can reassemble into the whole output with no marker. The lock's premise, "a bool or int cannot carry secret text", doesn't hold for a byte stream, and no t-5 row pins this case.
  Suggestion: exclude byte- and rune-typed results from clearing, or clear only bool and int. Add must-trip rows for a ReadRune loop and a DecodeRune loop.

- [antipattern: files / granularity] t-15 leaves out files that its own compile-atomic change breaks:
  - switchbranch.go:88/101 return gitCombined's `(string, error)` from guardedFF/guardedResetHard. Deleting gitCombined changes those signatures, and internal/cmd/gitseparator_test.go:188,193 and internal/cmd/refguard_test.go:140,144 call both functions in the two-value form. Neither test file is listed.
  - internal/cmd/testdata/subprocargs_audit/snippets.txt has six gitCombined rows whose verdicts change once gitCombined leaves gitCallFuncs.
  - The divergence text at phase.go:414 (help) and :616-619 tells the user to "read the abort first" while printing the output this task removes from the error.

  With 18 listed files (21 counting these), t-15 is already the largest task in the plan. "Compile-atomic" only holds for the final deletion: gitRun can land beside gitCombined and callers can migrate in batches.
  Suggestion: add the three files and reword the divergence text. Consider splitting "add gitRun and migrate callers" from "delete gitCombined".

## NOTE
- [antipattern: description accuracy] t-18 says "The `git ls-files` lists in survivor_drain and techdebt get conversion markers." survivor_drain.go:40 actually runs `go list -f {{.Dir}} ./...`; only techdebt.go:90 runs `git ls-files -z`. update.go is in t-18's files and gate scope, but the description never says what changes there (update.go:198-201 sends the install output to `o.out`).
- [TestNoTestLost] boundary_test.go's TestNoTestLost pins about 4k test names from tests_before.txt. Several tests will survive this phase by name only:
  - TestDroppingAScanRootFailsTheFloor and TestExecConsentScansTheSpawningPackages: execConsentScanRoots goes away once t-11 reads the whole-module program, and t-11's description never mentions that deletion.
  - TestUnwrapScanSkipsTestFiles: a Tests:false load makes it trivially true.
  - TestEveryPathShapedFieldIsDeclared: t-8 deletes pathShapedTags.

  Only t-6 and t-8 mention this constraint. The suite enforces it anyway, so this is a warning, not a gap.
- [forbidden-actions] There is no global rules file at ~/.claude/dross/rules.toml. Project rule r-01 ("make install" before relying on a change) isn't triggered. t-1's `dross test` and t-21's `dross architecture check` run through the installed binary, but neither depends on Go code this phase changes. No task edits a CI workflow.
- [strength] The plan's factual claims match the tree:
  - 39 gitCombined call sites in 14 files, 10 of them in milestone.go.
  - 28 spawn files.
  - Exactly five gh `%w\n%s` sites in internal/ship.
  - The assertion at comment_test.go:230-231.
  - The 6f27eaa^ stryker.go line ranges t-13 pins: 115-118, 142, 150, 201-222 and 315.
- [strength] The engine fails closed by default. Unknown external calls are escapes, a tainted return is an escape until t-9 lands, and every package in the transform set must be exercised by a corpus line. Fixtures build in a separate ssa.Program, so fixture types can't invent edges in the live VTA/CHA graph.
- [strength] The burn-downs test behaviour, not just scanner verdicts. CANARY stubs check that the tool output lands on stderr and stays out of the returned error (t-14, t-15, t-17, t-18). The global gate lands only after every burn-down. For exec taint, t-9's live discovery run closes the ownership gap the panel found for gitCombined.

## Summary
The plan is thorough and its groundwork checks out, but three things block execution: t-15's gate can't go green before t-19 lands, the live raw read of survivor.Acceptance.File (stale.go:101) has no owning task, and t-19's single gitTrim marker would clear diff and log content that the taint_sources lock treats as unsafe.
