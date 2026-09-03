Phase lane-selector-translation — 7 tasks across 5 waves

Lens: each criterion's ideal test contract was written first, then the smallest
task that makes that contract satisfiable. Where a contract could not be written
against a real symbol, the task grew a seam until it could.

Wave 1
  t-1  Add selector fields, enum and validate refusal
       files:    internal/configenum/configenum.go, internal/project/project.go,
                 internal/cmd/validate.go, internal/cmd/validate_lane_test.go
       covers:   c-1
       description:
                 Add `configenum.SelectorStyles = newSet("", "path", "dir", "go-package")`.
                 Add `Selector string` (toml `selector,omitempty`) and `EmptyExit []int`
                 (toml `empty_exit,omitempty`) to project.TestLane. Extend laneProblems
                 with a style check, an empty-exit range check and an empty-exit-without-
                 selector check, each naming the lane through the existing laneLabel.
       contract: - a lane with `selector = "packages"` makes TestValidateRejectsUnknownSelectorStyle
                   fail unless validate emits `runtime.test_lane "go" declares selector
                   "packages" — expected path | dir | go-package`; the message is asserted to
                   contain the lane NAME, so a refusal that only names the ordinal fails
                 - if the omitted-selector case ever starts reporting a problem,
                   TestValidateAcceptsALaneWithNoSelector fails: a lane with Selector "" and
                   one with "go-package" must both yield zero problems, which is what keeps
                   c-3's opt-in true at the validator
                 - `selector = "GO-PACKAGE"` must pass (configenum.Normalize), pinned by
                   TestSelectorStyleIsCaseAndSpaceInsensitive — a hand-rolled `==` comparison
                   fails it
                 - `empty_exit = [0]` and `empty_exit = [300]` each raise a problem naming the
                   lane (TestValidateRejectsUselessEmptyExitCode): declaring 0 as "collected
                   nothing" would reclassify every green run as a miss
                 - `empty_exit = [5]` on a lane with no selector raises
                   TestValidateRejectsEmptyExitWithoutSelector — codes that can never fire are
                   a silent misconfiguration, not a harmless extra
       depends_on: []

  t-2  Report which paths matched which lane
       files:    internal/testlane/match.go, internal/testlane/match_test.go
       covers:   c-4
       description:
                 Add `Matched map[int][]string` to Selection, populated in Select with the
                 NORMALIZED path for each lane the path hit. Lanes []int stays as it is, so
                 every existing caller and test is untouched.
       contract: - TestMatchedIsPerLaneNotGlobal: lanes go(`internal/**`) and docs(`docs/`)
                   against paths [internal/a.go docs/x.md README.md /etc/passwd] must give
                   Matched[0] == ["internal/a.go"] and Matched[1] == ["docs/x.md"] exactly —
                   the test asserts NON-membership of README.md and /etc/passwd in both
                   entries, so an implementation that appends every in-tree path to every hit
                   lane fails here rather than at the run site
                 - TestMatchedHoldsTheNormalizedForm: `./internal/a.go` must arrive in Matched
                   as `internal/a.go` while Unmatched keeps the caller's spelling — a single
                   shared slice for both breaks one of the two assertions
                 - TestMatchedDedupesWithinALane: a lane with globs [`internal/**`,
                   `internal/cmd/*.go`] against internal/cmd/test.go yields a one-element
                   Matched[0]; a second glob hit that appended again fails it
                 - TestMatchedIsEmptyForUnhitLanes: a lane no path hit has no Matched key at
                   all (not a nil-valued one), so a `for i := range Matched` at the run site
                   can never resurrect a lane Lanes excluded
       depends_on: []

Wave 2 (depends t-1)
  t-3  Derive a selector from matched paths
       files:    internal/testlane/selector.go, internal/testlane/selector_test.go,
                 internal/cmd/enum_divergence_test.go
       covers:   c-2
       description:
                 New pure `Derive(style string, paths []string) []string`: dedupe, sort
                 lexicographically, then dispatch on configenum.Normalize(style) — `path`
                 emits each path, `dir` each distinct parent, `go-package` `./<dir>/...`.
                 Empty style returns nil. Register the switch as a scan site in
                 enumScanSites so the divergence guard covers it.
       contract: - TestDeriveIsOrderIndependent: Derive("dir", [b/x.go a/y.go a/z.go]) and
                   Derive("dir", [a/z.go a/y.go b/x.go]) must return the identical slice
                   ["a", "b"] — argv order leaking into the selector fails both the equality
                   and the collapse assertion at once (locked selector_derivation)
                 - TestGoPackageStyleCollapsesADirectory: three files in internal/cmd yield
                   exactly ["./internal/cmd/..."], length asserted — one selector argument per
                   file fails on the length
                 - TestGoPackageAtRepoRoot pins main.go → "./..." as a decision rather than an
                   accident; a change to "." or "./main.go/..." fails it
                 - TestPathStyleEmitsEveryPathOnce: two distinct files give two arguments, the
                   same file listed twice gives one
                 - TestUnknownStyleDerivesNothing: Derive("packages", …) returns nil, so a
                   style that slipped past validate can never inject an argument
                 - TestSelectorDispatchMatchesSelectorStyles (enum_divergence_test.go, on the
                   TestMilestoneModeDispatchMatchesConfigenum pattern) fails the build if a
                   style is added to configenum with no arm in Derive, or an arm exists that
                   configenum does not accept; the site is registered with requiresNormalize
                   true, so a switch over strings.ToLower fails TestDispatchUsesConfigenumNormalize
       depends_on: [t-1]

  t-4  Accept selector flags on lane add and list
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_test.go
       covers:   c-7
       description:
                 `dross test lane add` gains `--selector <style>` and repeatable
                 `--empty-exit <code>`, validated against configenum.SelectorStyles before the
                 load-modify-save. `dross test lane list` prints `selector:` and `empty-exit:`
                 for the lanes that declare them.
       contract: - TestLaneAddPersistsSelector: after `lane add go --match 'internal/**'
                   --command 'go test' --selector go-package --empty-exit 5`, reloading
                   project.toml gives Selector == "go-package" and EmptyExit == []int{5}
                 - TestLaneAddRejectsUnknownSelectorStyle asserts BOTH the error and that
                   project.toml is byte-identical afterwards — a refusal that ran after the
                   save would pass an error-only assertion
                 - TestLaneAddRefusesEmptyExitWithoutSelector: `--empty-exit 5` with no
                   `--selector` is refused before the write, same rule validate enforces, so
                   the CLI cannot mint a config validate then condemns
                 - TestLaneListPrintsSelector: output for the fixture contains
                   `selector: go-package` and `empty-exit: 5`; a lane declaring neither must
                   print NEITHER label (asserted as absence), so a lane-less-selector repo's
                   listing is unchanged
                 - TestLaneAddSelectorIsNormalized: `--selector ' GO-PACKAGE '` persists as
                   `go-package`, so `list` and validate never disagree with what was typed
       depends_on: [t-1]

Wave 3 (depends t-2, t-3)
  t-5  Append the derived selector to the lane run
       files:    internal/cmd/test.go, internal/cmd/test_lane_selector_test.go
       covers:   c-2, c-3, c-4, c-6
       description:
                 In runTestLanes, carry sel.Matched alongside the matched lane; per lane,
                 derive with testlane.Derive(lane.Selector, matched) and build the spawn line
                 with the existing testCommandLine. Print that same derived line in the lane
                 header. Consent still resolves against lane.Command.
       contract: - TestSelectorScopesTheSpawnedLine: a `go` lane with command `go test -count=1`
                   and selector go-package, run as `--files internal/cmd/test.go`, records
                   exactly one spawn of `go test -count=1 './internal/cmd/...'` — a lane that
                   ran its whole suite fails on the recorded line, which is c-2's "scoped run
                   rather than the lane's whole suite" stated as a string
                 - TestHeaderShowsTheDerivedLine: the transcript contains
                   `lane go: go test -count=1 './internal/cmd/...'` and the header index is
                   asserted to precede the spawn sentinel; printing lane.Command while
                   spawning the derived line fails it, which is the exact mismatch c-2's
                   "prints the exact line that ran" exists to catch
                 - TestNoSelectorStyleRunsUntouched (c-3): the docs lane, selector omitted,
                   spawns `markdownlint docs` byte-for-byte with --files naming two doc files;
                   any trailing space or quoted argument fails the equality
                 - TestSelectorUsesOnlyThisLanesPaths (c-4 at the run site): a file set of
                   [internal/cmd/test.go docs/x.md README.md] against both lanes records a go
                   line containing `./internal/cmd/...` and NOT `docs`, and a docs line
                   containing neither `internal` nor `README` when the docs lane declares
                   selector dir — cross-lane bleed fails on the substring assertions
                 - TestGrantSurvivesTheAppendedSelector (c-6): the lane is granted for
                   `go test -count=1` only; the run must still spawn, and the spawned line must
                   differ from the granted one — an implementation that fingerprinted the
                   derived line fails on the refusal, one that dropped the selector to keep
                   the grant valid fails on the difference
                 - TestUngrantedLaneWithASelectorStillRefuses (c-6, the other direction): an
                   ungranted lane declaring a selector records ZERO spawns and exits 6
                 - TestSelectorArgumentsAreShellQuoted: a matched path containing a space
                   arrives single-quoted in the recorded line (locked selector_consent reuses
                   shellQuoteArg)
       depends_on: [t-2, t-3]

Wave 4 (depends t-5)
  t-6  Report selector misses without failing the gate
       files:    internal/cmd/test.go, internal/remote/remote.go,
                 internal/cmd/test_lane_miss_test.go
       covers:   c-5
       description:
                 Drop matched paths that no longer exist on disk before deriving; a lane whose
                 selector empties is a miss and never spawns. Classify a spawn exit listed in
                 lane.EmptyExit as a miss rather than a red suite (remote.ExitError gains
                 ExitCode() so one classifier reads both transports). Misses print and are
                 counted; a run where every matched lane missed returns exitNothingMeasured.
       contract: - TestDeletedPathIsDroppedBeforeSpawn: two matched paths, one deleted from the
                   fixture, gives a spawned line carrying only the surviving path's package —
                   `go test ./gone/...` reaching the recorder fails it (locked missing_paths)
                 - TestEmptySelectorIsAMissNotASpawn: a lane whose every matched path was
                   deleted records ZERO spawns and prints a line naming the lane
                 - TestSelectorMissDoesNotFailAGreenRun: go lane green, docs lane a miss →
                   ExitCode(err) == 0 and the transcript names the docs lane and its selector.
                   This is the criterion's load-bearing half: folding the miss into
                   worseOutcome would fail on the exit code, because exitNothingMeasured
                   outranks nil
                 - TestEveryLaneMissedIsNotGreen: both matched lanes miss → exit 5 and zero
                   spawns; an implementation that only reported misses fails on the exit code
                 - TestDeclaredEmptyExitIsAMiss: a lane declaring empty_exit = [5] whose spawn
                   returns fakeExit{5} is reported as a miss, and the run exits 5 (nothing
                   measured) rather than 1
                 - TestUndeclaredExitCodeIsStillRed: the same lane returning fakeExit{1} exits
                   1 with a `test lane "go" failed` message — the empty-exit arm must not
                   swallow a real red
                 - TestRemoteEmptyExitIsAlsoAMiss: a remote lane exiting 5 through
                   remoteFailure is classified as a miss, which fails unless
                   remote.ExitError.ExitCode() exists for errors.As to reach
                 - TestMissMessageNamesLaneAndSelector: the printed line contains both the lane
                   name and the derived selector text, per c-5's wording
       depends_on: [t-5]

Wave 5 (depends t-4, t-6)
  t-7  Document the selector surface
       files:    README.md, ARCHITECTURE.md, assets/prompts/options.md,
                 internal/cmd/options_docs_test.go
       covers:   c-7
       description:
                 Extend the lanes rows in README, the test-suite-runner and exec-consent
                 sections of ARCHITECTURE.md, and the test-lanes block of options.md with the
                 selector styles, the empty-exit codes, the miss outcome and the unchanged
                 grant story. Extend the existing needle guards to cover them.
       contract: - TestReadmeDocumentsTestLanes gains needles `--selector`, `go-package`,
                   `selector miss` and `--empty-exit`; a shipped flag with no README line
                   fails it, which is the same rule that already holds for `dross test --files`
                 - TestOptionsCoversTheConsentVerbs gains `--selector`, so the settings surface
                   cannot claim to reach every dross-managed setting while omitting the one
                   field that changes what a consented lane spawns
                 - TestOptionsHasNoStaleLaneAddInvocation: every `dross test lane add` example
                   in options.md and README is parsed for flags that the cobra command
                   actually registers, so a doc advertising `--selector-template` fails
       depends_on: [t-4, t-6]

## Coverage

| criterion | tasks |
|---|---|
| c-1 selector style validated, lane named | t-1 |
| c-2 scoped run + header shows the exact line | t-3, t-5 |
| c-3 no style ⇒ byte-identical | t-5 |
| c-4 selector from this lane's paths only | t-2, t-5 |
| c-5 selector miss reported, all-miss run non-zero | t-6 |
| c-6 grant covers the declared command | t-5 |
| c-7 declared and inspected through the CLI | t-4, t-7 |

7/7 criteria covered.

## Judgment calls

- **`Matched map[int][]string` added to Selection rather than replacing `Lanes []int`.**
  A `[]LaneMatch` redesign is cleaner in the abstract but rewrites all 14 tests in
  match_test.go and every read in runTestLanes for no criterion; the map is additive, so
  wave 1 lands with the existing suite untouched and any regression it causes is a new
  failure rather than a rewritten assertion.
- **Matched carries normalized paths; Unmatched keeps the caller's spelling.** The two
  fields now disagree on purpose. Unmatched is read by a human in a refusal, Matched is
  fed to a runner — a shared representation would make one of the two wrong, so the
  contract pins both spellings in one test.
- **Selector misses are counted, not folded into `worseOutcome`.** exitRank ranks
  exitNothingMeasured above nil, so a miss passed through worseOutcome would turn a run
  with one green lane non-zero — the direct opposite of c-5's "does not fail the gate".
  Counting run-vs-missed lanes and deciding after the loop is what makes both halves of
  c-5 satisfiable at once.
- **exitNothingMeasured (5) is reused for the all-missed run rather than a new code.**
  The reader's next action is identical to the existing 5 — nothing was measured, do not
  commit — and the exit contract's value is that each code sends the reader to one place.
  A new code would also need a row in the README table and the pairwise-distinctness test
  for no behavioural difference.
- **Empty-exit codes take effect only on a selector-scoped run.** Declaring them on a
  lane with no selector is refused by validate and by `lane add` instead of silently
  inert. This keeps c-3's "byte-identical to today" true of the exit status as well as of
  the command line — a lane with no selector cannot acquire a new outcome class.
- **`empty_exit = [0]` is rejected.** Nothing in the spec says so, but a lane declaring 0
  as "collected nothing" reclassifies every passing run as a miss, which manufactures the
  exact false-negative c-5 is written against.
- **remote.ExitError gains ExitCode().** The alternative — a second errors.As arm for the
  remote type inside test.go — would leave local and remote lanes classified by two
  different code paths, and the one that gets exercised least is the one that drifts.
  One method makes the existing `exitCoder` interface reach both.
- **Derivation lives in internal/testlane, the on-disk existence filter in
  internal/cmd.** Derive stays pure and exhaustively table-testable; the filesystem check
  is policy that needs repoDir, and the cmd fixtures are real temp dirs, so deleting a
  file in a test is a real deletion rather than a stubbed one.
- **Docs are one task at the end, not a line in each.** The needle guards assert against
  shipped flag names and the miss wording, both of which are only final after t-4 and
  t-6; a docs edit written earlier would either be re-edited or would pin a name the
  implementation later changed.
