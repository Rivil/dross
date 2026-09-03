# MVP lens — lane-selector-translation

Phase lane-selector-translation — 4 tasks across 3 waves

Wave 1
  t-1  Declare selector enum, lane fields, validation
       files:    internal/configenum/configenum.go, internal/project/project.go,
                 internal/cmd/validate.go, internal/cmd/validate_lane_test.go,
                 internal/project/project_test.go
       covers:   c-1
       desc:     Add `configenum.SelectorStyles` (empty default, members path | dir |
                 go-package). Add `Selector string \`toml:"selector,omitempty"\`` and
                 `EmptyExit []int \`toml:"empty_exit,omitempty"\`` to project.TestLane.
                 laneProblems gains a style check and an empty_exit sanity check.
       contract: - `dross validate` on a lane with `selector = "modules"` emits a problem
                   naming `runtime.test_lane "go"` and listing `path | dir | go-package`;
                   a lane omitting `selector` emits nothing (validate_lane_test.go, beside
                   TestValidateRejectsUncompilableGlob)
                 - `empty_exit = [0]` is refused naming the lane — 0 is success, and a lane
                   declaring it would report every green run as a selector miss
                 - project_test.go decodes hand-written toml carrying
                   `selector = "go-package"` / `empty_exit = [5]` into Selector/EmptyExit,
                   and TestNoTestLaneIsAbsentFromTheDocument still holds: a lane with
                   neither field Saves without a `selector` or `empty_exit` key
       depends:  —

Wave 2 (depends t-1)
  t-2  Attribute matched paths, derive selectors
       files:    internal/testlane/match.go, internal/testlane/selector.go,
                 internal/testlane/match_test.go, internal/testlane/selector_test.go
       covers:   c-4
       desc:     Selection gains `Paths map[int][]string` — lane index to the normalized
                 in-tree paths that matched THAT lane. New Selector(style, paths) returns
                 the derived argument list: deduplicated, sorted lexicographically,
                 directories collapsed; `path` yields each path, `dir` its parent,
                 `go-package` `./<dir>/...`, an empty style yields nil. Package stays pure —
                 no filesystem.
       contract: - Select over {internal/cmd/test.go, README.md, notes.txt, /etc/hosts}
                   against a go lane and a docs lane puts internal/cmd/test.go only in
                   Paths[0] and README.md only in Paths[1]; the unmatched and out-of-tree
                   paths appear in no Paths entry (this is c-4's whole assertion)
                 - Selector("dir", ["b/z.go","a/y.go","a/x.go"]) == ["a","b"] — two files in
                   one directory collapse to one argument and the result is sorted, so the
                   same set in any argv order produces the same line
                 - Selector("go-package", ["main.go"]) == ["./..."] and
                   Selector("go-package", ["internal/cmd/test.go"]) == ["./internal/cmd/..."]
                 - Selector("", anything) == nil
                 - a table driven off configenum.SelectorStyles.Values() fails when a member
                   has no arm in the derivation switch (the in-package stand-in for
                   enum_divergence_test.go, which scans internal/cmd and forge by function)
       depends:  t-1

  t-3  Add selector flags to lane add and list
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_test.go,
                 README.md, ARCHITECTURE.md, assets/prompts/options.md
       covers:   c-7
       desc:     `dross test lane add` gains --selector <style> and --empty-exit <code>
                 (repeatable); both are validated against configenum before the
                 load-modify-save. `dross test lane list` prints `selector:` and
                 `empty exit:` lines for lanes that declare them. Docs follow the flags.
       contract: - `lane add go --match 'internal/**' --command 'go test -count=1' --selector
                   go-package --empty-exit 5` writes `selector = "go-package"` and
                   `empty_exit = [5]` into project.toml, and `lane list` prints
                   `selector: go-package` and `empty exit: 5` under that lane
                 - `--selector modules` is refused naming the accepted styles and leaves
                   project.toml byte-identical, in the shape
                   TestLaneAddWithoutCommandLeavesTheFileUnchanged already pins
                 - a lane added with no --selector prints no selector line in `lane list`
                   and writes no `selector` key — the opt-in shape stays visible
       depends:  t-1

Wave 3 (depends t-2)
  t-4  Run lanes with derived selectors
       files:    internal/cmd/test.go, internal/cmd/test_lane_selector_test.go
       covers:   c-2, c-3, c-5, c-6
       desc:     runTestLanes derives each lane's selector from sel.Paths[i], drops paths
                 that no longer exist under repoDir, appends the result through
                 testCommandLine (shell-quoted) and prints the derived line in the lane
                 header. A lane whose selector empties never spawns; a lane exiting with a
                 code in its empty_exit is a miss. Misses do not fail the gate, but a run
                 whose every runnable lane missed returns exitNothingMeasured.
                 LaneConsented keeps receiving lane.Command, not the derived line.
       contract: - a go lane with `selector = "go-package"` matched by internal/cmd/test.go
                   spawns `go test -count=1 './internal/cmd/...'` through the spawnLocal
                   seam, and the header line printed for that lane is that same string —
                   header and argv are asserted equal, not each against a literal (c-2)
                 - a lane declaring no selector spawns its command byte-identically while a
                   selector-declaring sibling lane is present in the same run; existing
                   TestLaneLineIsByteIdentical still passes untouched (c-3)
                 - a selector built for the docs lane never contains the go lane's paths,
                   the unmatched path or the out-of-tree path — asserted on the recorded
                   argv, not on Selection (c-4 at the wiring layer)
                 - a lane whose only matched path was deleted from disk never reaches
                   spawnLocal and prints a selector miss naming the lane; a two-lane run
                   where only the docs lane empties exits 0, a run where BOTH empty exits 5
                   (the mixed case is what forbids folding a miss into worseOutcome, whose
                   exitRank would let one miss decide a run that also ran a green lane)
                 - a lane exiting 5 with `empty_exit = [5]` is a miss, not exitSuiteFailed;
                   the same lane exiting 5 with no empty_exit declared exits 1 (c-5)
                 - a lane granted for its declared command line still spawns once a selector
                   is appended — the grant is issued over lane.Command and the run is
                   observed, so re-fingerprinting the derived line fails this test (c-6)
       depends:  t-2

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-4 |
| c-3 | t-4 |
| c-4 | t-2, t-4 |
| c-5 | t-4 |
| c-6 | t-4 |
| c-7 | t-3 |

7/7 criteria covered.

## Judgment calls

- **One wiring task for c-2, c-3, c-5 and c-6, not four.** They all edit the same loop in
  runTestLanes. Split, the intermediate commit spawns a derived line with no miss guard —
  which is the false-green the phase exists to prevent — so the merge is a safety property,
  not just economy.
- **internal/testlane stays pure; the missing-path stat lives in internal/cmd.** Rejected
  passing an `exists func(string) bool` into Selector: repoDir is only known at the command
  layer and the package's own doc comment makes purity load-bearing.
- **Added `Paths map[int][]string` to Selection rather than reshaping `Lanes []int`.**
  Rejected turning Lanes into a struct slice: it would rewrite every existing match_test and
  the runTestLanes caller for no criterion.
- **No new entry in enum_divergence_test.go.** That guard scans internal/cmd and forge
  dispatch sites by function name; a testlane switch does not fit its helper. A table test
  driven off `configenum.SelectorStyles.Values()` gives the same build-break in-package.
- **Docs folded into t-3, not a docs task.** README's ✅ table, the ARCHITECTURE lanes
  section and options.md's lanes block all describe the flag surface c-7 names; a standalone
  docs task traces to no criterion and would ship the flags undocumented for a wave.
- **`empty_exit = [0]` rejected in t-1's validator.** One extra line in a validator that is
  already being edited, against a mistake that would silently convert every green lane into
  a selector miss.
- **No `dross test lane edit` verb.** Locked lane_edit_surface: editing stays
  remove-then-re-add, so --selector and --empty-exit are add-time flags only.
