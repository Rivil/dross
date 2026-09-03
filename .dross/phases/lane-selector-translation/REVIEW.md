# Plan Review — lane-selector-translation

Reviewed: 2026-08-26
Plan: 7 tasks across 5 waves

## BLOCKING
(none)

## FLAG

- [granularity] t-1 touches 5 files across three layers (leaf enum package, project schema,
  cmd validator) and carries two unrelated concerns: the selector-style enum + its refusal
  (which c-1 names), and three empty-exit refusal rules (which no criterion names). Nine
  contract lines in one commit.
  Suggestion: split at the natural seam — selector field + `SelectorStyles` + style refusal in
  one task, `EmptyExit` field + its three refusals in another. The second half is only consumed
  by t-6, so it could even move to wave 2 alongside t-4.

- [scope] The `empty_exit`-without-`selector` refusal (t-1 contract line 7, mirrored as a CLI
  refusal in t-4 line 4) is invented by the plan. Spec's locked `empty_detection` says only that
  a lane "may declare the exit codes that mean 'collected no tests'"; nothing ties that to a
  declared selector. A lane whose whole suite legitimately collects nothing (an empty package,
  a filtered-out pytest dir) would be refused by validate under this rule. It is a defensible
  reading of c-5 ("naming the lane and the selector"), but it is a new refusal users hit, and it
  is not derived from any criterion.
  Suggestion: either accept it deliberately and say so in the task description ("a code that can
  never fire is a misconfiguration" is the argument, but it should be stated as a choice, not
  asserted as an existing rule), or drop it and let a no-selector lane's empty_exit be inert.
  The 0 and 255 refusals are well-argued and should stay either way.

- [wave-order / atomicity] t-5 ships the missing-path filter but explicitly defers its reporting
  to t-6 ("A lane whose filtered selector empties does not spawn — its reporting lands in t-6").
  At t-5's commit, a run whose only matched lane had every path deleted spawns nothing, prints
  nothing, and returns nil — `dross test --files` exits 0 having measured nothing. That is the
  exact failure mode `exitNothingMeasured` exists to prevent, shipped as an atomic, test-gated
  commit. Compare the existing runTestLanes comment, which splits resolution from spawning
  precisely because the interim state there is *safe*; this interim state is not.
  Suggestion: pin the interim in t-5's contract — an emptied-selector lane must not leave the run
  exiting 0 (return exitNothingMeasured provisionally, refined by t-6) — or move the existence
  filter into t-6 so the filter and its verdict land together.

- [locked-decision tension] t-3 pins `go-package` on a root-level file to `["./..."]` and
  explicitly fails `"."`. `./...` is the whole module — so a task touching one root `.go` file
  produces a run of the entire suite, which is what c-2 says must not happen ("a task touching
  one file produces a scoped run rather than the lane's whole suite"). It is also the exact
  hazard deferred item 41d3a87cd97fcb38 describes, arriving through dross's own derivation rather
  than through the user's command. `.` (or `./`) is the scoped run of the root package.
  The `dir` style has the same root case unpinned: `path.Dir("main.go") == "."`, and a bare `.`
  handed to pytest is the whole tree.
  Suggestion: re-justify `./...` in the task description if it is intended (the argument would be
  "a root-level change is module-wide", which is inference), or pin `.` and add the `dir` root
  case to the contract. Either way the `dir` root case needs a contract line.

- [antipattern / missing detail] t-3 says "Register the switch in enumScanSites with
  requiresNormalize true" but `scanSite` is a 5-field tuple — label, path, fn, minCases,
  requiresNormalize — and the plan does not state minCases. With three case literals the value
  is 3; a registration that guesses 2 silently weakens `TestDispatchSitesAreStillSwitches` for
  this site.
  Suggestion: name minCases = 3 in the description.

## NOTE

- [files] Every existing symbol and test the plan names was verified present: `testCommandLine`,
  `shArgvFor`, `shellQuoteArg`, `laneLabel`, `laneProblems`, `worseOutcome`/`exitRank`,
  `remote.ExitError`, `exitCoder`, `remoteFailure`, `enumScanSites`, and all seven named existing
  tests (`TestLaneLineIsByteIdentical`, `TestReadmeDocumentsTestLanes`,
  `TestOptionsCoversTheConsentVerbs`, `TestLaneAddWithoutCommandLeavesTheFileUnchanged`,
  `TestNoTestLaneIsAbsentFromTheDocument`, `TestOptionsSectionNumbersAreContiguous`,
  `TestMilestoneModeDispatchMatchesConfigenum`). The three files that do not exist
  (internal/testlane/selector.go, internal/cmd/test_lane_selector_test.go,
  internal/cmd/test_lane_miss_test.go) are each created by the task that lists them. No dangling
  references.

- [coverage] All seven criteria are covered: c-1→t-1, c-2→t-3+t-5, c-3→t-5, c-4→t-2+t-5,
  c-5→t-6, c-6→t-5, c-7→t-4+t-7. Wave dependencies are all real: t-3 and t-4 need t-1's field and
  Set; t-5 needs t-2's Matched and t-3's Derive; t-6 needs t-5's empty-filter; t-7 needs the
  flags t-4 registers. Nothing could drop a wave.

- [test-contract] Contract quality is the strongest part of this plan. Every line names the
  implementation mistake it kills rather than the behaviour it hopes for — the shared-slice
  aliasing in t-2 line 2, the header-vs-spawn *equality* comparison in t-5 line 2 (not each
  against a literal), and the one-integer-apart 5-vs-1 opposite verdicts in t-6 line 3. There is
  not a single vague contract in the file.

- [locked-decisions] All six locked decisions are honoured and cited by key in the task
  descriptions: `selector_derivation` in t-3, `selector_consent` and `missing_paths` in t-5,
  `empty_detection` in t-6, `lane_edit_surface` in t-4. t-6's "stdout is never scraped" line
  tests the *absence* forbidden by `empty_detection`, which is harder to write than the positive
  case and easy to skip.

- [cross-layer reasoning] The 255 empty-exit refusal (t-1) was traced through `remote.Classify`,
  where 255 is ssh transport failure — so a lane could not declare an unreachable host as "no
  tests". That interaction is two packages away from the field being validated and would be very
  easy to miss.

- [tests] t-6 edits internal/remote/remote.go but does not list internal/remote/remote_test.go.
  The new `ExitError.ExitCode()` is a one-line accessor exercised only through internal/cmd's
  remote-miss assertion. Acceptable, but the remote package gains an exported method with no
  test in its own package.

- [rules] rules.toml r-01 applies at execution time to t-7 (assets/prompts/options.md): the edit
  is not live until `make install`. The guard test reads the repo copy via os.ReadFile, so the
  test gate is unaffected — only manual verification of the installed prompt needs the re-link.
  No task implies a forbidden action; runtime.mode is native and every command is `go test`.

- [coverage bookkeeping] t-7 claims c-7, but c-7 is satisfied by t-4 alone — t-7 documents it.
  Harmless, and the doc guards it extends are real work; noted only so the covers map is not read
  as "c-7 is not done until t-7".

## Summary
No blockers — the plan is well-grounded in the actual code and its contracts are unusually sharp;
the four things worth fixing before execution are t-1's size, the invented empty-exit-needs-
selector rule, t-5's interim exit-0-on-empty window, and the `./...` root-package derivation that
turns a one-file change into a whole-suite run.
