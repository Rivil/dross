# risk lens — lane-selector-translation

Lens: **failure modes drive the graph.** Every task below owns exactly one way
this feature can lie to a caller who is deciding whether to commit. The risks,
named first so the tasks can be read against them:

| # | Failure mode | Owner |
|---|---|---|
| R1 | A hand-edited `selector = "pytst"` reaches a spawn and the lane silently runs unscoped | t-1 |
| R2 | Lane A's selector carries lane B's paths, or an unmatched / out-of-tree path | t-2 |
| R3 | Selector args are argv-order-dependent, duplicated, or `-`-prefixed and read as runner flags | t-3 |
| R4 | A style is added to the enum and the translator's switch never learns it (or vice versa) | t-3 |
| R5 | A lane declares a selector on a command that already ends in `./...`, so the "scoped" run is the whole suite | t-4 |
| R6 | The header prints one line and a different line spawns; the remote path never gets the selector | t-5 |
| R7 | A deleted path lands in the selector and the runner's hard error reads as a red gate | t-5 |
| R8 | A lane with no style stops being byte-identical, or a selector re-fingerprints and untrusts every lane | t-5 |
| R9 | A selector that collected nothing reads as green — or, inverted, one miss fails a run whose other lanes passed | t-6 |
| R10 | `empty_exit = [0]` (or `255` on the remote path) turns a pass — or an ssh failure — into a "miss" | t-1 + t-6 |
| R11 | The flags exist and no surface names them; `options_docs_test` passes because its needle list is stale | t-7 |

---

Phase lane-selector-translation — 7 tasks across 4 waves

Wave 1

```
  t-1  Declare selector fields and their enum
       files:    internal/project/project.go
                 internal/configenum/configenum.go
                 internal/cmd/validate.go
                 internal/cmd/validate_lane_test.go
                 internal/project/project_test.go
       covers:   c-1
       desc:     Adds TestLane.Selector (string, omitempty) and TestLane.EmptyExit
                 ([]int, omitempty); adds configenum.SelectorStyles =
                 newSet("none", "none", "path", "dir", "go-package"); extends
                 laneProblems with an unrecognised-style problem and an
                 out-of-range empty_exit problem, both routed through laneLabel.
       contract: - a lane with `selector = "pytst"` makes `dross validate` emit a
                   problem line containing both `runtime.test_lane "go"` and the
                   accepted set rendered from SelectorStyles.List() — the literal
                   list is never typed into the message, so a style added to the
                   Set cannot leave the refusal naming a stale set
                 - a lane with `empty_exit = [0]` is a validate problem naming the
                   lane, because 0 is the runner's success code and accepting it
                   would classify every green lane as a miss
                 - a lane with `empty_exit = [255]` is a validate problem, because
                   remote.Classify already spends 255 on ssh transport failure and
                   a lane claiming it would report an unreachable host as "no tests"
                 - a lane with `empty_exit = [5]` and `selector = "go-package"`
                   produces ZERO problems (well-formed lanes stay silent)
                 - project.Save on a TestLane with neither field set round-trips a
                   project.toml byte-identical to one written before this phase —
                   omitempty is asserted on the rendered bytes, not on the struct
```

```
  t-2  Attribute matched paths per lane
       files:    internal/testlane/match.go
                 internal/testlane/match_test.go
                 internal/cmd/test.go
       covers:   c-4
       desc:     Selection.Lanes becomes []LaneHit{Index int, Paths []string} —
                 the lane index and the normalized paths that hit it, carried in
                 one value so the two cannot drift. Select fills Paths as it
                 matches; runTestLanes' existing loop is updated to read .Index.
                 Package stays pure: no filesystem, no config.
       contract: - Select([go:["internal/**"], docs:["docs/"]],
                   ["internal/a.go","docs/x.md","scripts/s.sh","/abs/z.go"])
                   returns the go hit holding exactly ["internal/a.go"] and the
                   docs hit exactly ["docs/x.md"] — scripts/s.sh appears only in
                   Unmatched and /abs/z.go only in OutOfTree, in neither hit
                 - a path matched by two globs of ONE lane appears once in that
                   lane's Paths (the existing single-index rule extended to paths)
                 - a path matched by two DIFFERENT lanes appears in both lanes'
                   Paths — cross-cutting work is genuinely tested on both sides,
                   the locked multi_lane rule
                 - `./internal/a.go` and `internal/a.go` in one argv yield a single
                   Paths entry in normalized form, since the selector is a machine
                   argument and not a message quoting the caller's spelling
                 - Paths preserves Select's existing declaration-order/dedup
                   properties: a hit is never present with an empty Paths slice
```

Wave 2 (depends t-1)

```
  t-3  Translate matched paths into selector args
       files:    internal/testlane/selector.go            (new)
                 internal/testlane/selector_test.go       (new)
                 internal/cmd/enum_divergence_test.go
       covers:   c-2, c-4
       depends:  t-1
       desc:     SelectorArgs(style string, paths []string) ([]string, error) —
                 the pure translation. Dedup, sort lexicographically, then map by
                 style; unknown style is an error, never a silent passthrough.
                 Adds the dispatch/Set pairing guard beside the existing ship and
                 board ones.
       contract: - SelectorArgs("path", ["b.go","a.go","b.go"]) == ["a.go","b.go"]
                   — sorted and deduped, so two argv orderings of one file set
                   produce byte-identical command lines
                 - SelectorArgs("dir", ["internal/cmd/a.go","internal/cmd/b.go",
                   "internal/x.go"]) == ["internal","internal/cmd"] — repeated
                   directories collapse to one argument (locked selector_derivation)
                 - SelectorArgs("go-package", ["internal/cmd/a.go"]) ==
                   ["./internal/cmd/..."]
                 - SelectorArgs("go-package", ["main.go"]) == ["./..."], not
                   "././..." — the root package's dir is "." and the naive
                   "./<dir>/..." concatenation emits a path go rejects
                 - SelectorArgs("", paths) and SelectorArgs("none", paths) both
                   return a nil slice with no error — the no-selector lane is a
                   value in the same function, not a branch its callers each have
                   to remember
                 - SelectorArgs("pytst", paths) returns an error naming both the
                   style and the accepted set, and returns NO args — a caller that
                   ignored the error would otherwise spawn unscoped
                 - a derived argument that would begin with "-" (a file literally
                   named "-x.go") is emitted as "./-x.go", so a matched path can
                   never arrive at the runner as an option
                 - TestSelectorDispatchMatchesSelectorStyles parses
                   internal/testlane/selector.go and fails when SelectorArgs' case
                   literals and configenum.SelectorStyles.Values() diverge in
                   either direction, and asserts the switch tag is
                   configenum.Normalize (not strings.ToLower)
```

```
  t-4  Add selector flags to lane add and list
       files:    internal/cmd/test_lane.go
                 internal/cmd/test_lane_test.go
       covers:   c-7
       depends:  t-1
       desc:     `dross test lane add` gains --selector <style> and --empty-exit
                 <code> (repeatable); both are validated against configenum before
                 the load-modify-save. `dross test lane list` prints a selector
                 line and an empty-exit line for lanes that declare them. Editing
                 stays remove-then-re-add (locked lane_edit_surface).
       contract: - `lane add go --match "internal/**" --command "go test ./..."
                   --selector pytst` exits non-zero, names the accepted set, and
                   leaves project.toml byte-for-byte unchanged (asserted on file
                   bytes, matching TestLaneAddWithoutCommandLeavesTheFileUnchanged)
                 - the same refusal fires for --empty-exit 0 and --empty-exit 300,
                   so the CLI cannot write a lane `dross validate` would then reject
                 - `lane add ... --selector go-package --empty-exit 5` writes
                   selector and empty_exit into the [[runtime.test_lane]] block,
                   and `lane list` prints both under that lane's name
                 - `lane list` on a lane with no selector prints NO selector line —
                   an "selector: none" row on every pre-existing lane would make an
                   opt-in field look like something the user has to set
                 - `lane add go --selector go-package --command "go test ./..."`
                   succeeds but WARNS that the command already ends in a whole-tree
                   selector, naming it: appending "./internal/cmd/..." to a line
                   that already says "./..." runs the union, i.e. the whole suite,
                   which is the c-2 promise silently unmet
                 - a lane declared through these flags survives a `lane remove` of a
                   DIFFERENT lane unchanged (the existing rewrite path now carries
                   two more fields)
```

Wave 3 (depends t-2, t-3)

```
  t-5  Spawn each lane with its derived line
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_selector_test.go  (new)
       covers:   c-2, c-3, c-6
       depends:  t-2, t-3
       desc:     runTestLanes derives per-lane selector args from that lane's own
                 LaneHit.Paths, drops paths absent from disk (locked missing_paths),
                 builds ONE line via testCommandLine(lane.Command, args), prints
                 that line in the header and passes the same string to runOneLane
                 (local and remote). Consent still resolves against lane.Command.
       contract: - a lane with selector "go-package" matched by internal/cmd/a.go
                   spawns exactly `go test -count=1 ./internal/cmd/...`, recorded by
                   the existing spawnRecorder — not the bare command, not the
                   whole-tree selector
                 - the string printed by the `lane <name>: ` header is compared for
                   equality against rec.lines[0]; a header built from lane.Command
                   while the spawn used the derived line fails here, because a
                   transcript that names a command nobody ran is worse than none
                 - a lane with NO selector still spawns lane.Command byte-for-byte
                   with --files supplied (c-3): the existing
                   TestLaneLineIsByteIdentical must keep passing unmodified
                 - a two-lane run where only the go lane declares a selector spawns
                   the go lane scoped and the docs lane untouched in the same run —
                   the styles are per lane, not per run
                 - an ungranted lane declaring a selector is still refused with
                   exitLaneRefused and spawns nothing; the refusal names
                   lane.Command, not the derived line, since the grant is over the
                   declared command (locked selector_consent)
                 - a lane granted BEFORE this phase (fingerprint over lane.Command)
                   runs green with a selector appended — appending must not
                   re-fingerprint, or every existing grant would go stale on upgrade
                 - a selector arg containing a space or a quote reaches sh through
                   shellQuoteArg: the recorded line contains the single-quoted form,
                   so a path with a space cannot split into two runner arguments
                 - the remote path gets the derived line too: with spawnRemote
                   recording, the ssh script for a selector-bearing lane contains
                   the selector — asserted, because runRemoteLine is a second call
                   site that a local-only wiring silently skips
                 - a matched path deleted from disk is absent from the spawned line;
                   when the filter empties a lane's selector the lane does not spawn
                   at all (rec.count() == 0 for that lane) rather than spawning
                   `go test ./gone/...`, whose hard runner error would read as red
                 - the existence filter applies ONLY to lanes with a selector: a
                   lane with no style matched by a non-existent path still spawns,
                   so every pre-selector fixture keeps its behaviour
```

Wave 4 (depends t-4, t-5)

```
  t-6  Report selector misses and refuse an all-miss run
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_selector_test.go
       covers:   c-5
       depends:  t-5
       desc:     A lane is a selector MISS when its filtered selector is empty, or
                 when it exits with a code listed in the lane's empty_exit (read
                 from exitCoder locally and from remote.ExitError.Code remotely).
                 Misses are collected separately from worst — they never enter
                 worseOutcome — and only become an exitNothingMeasured error when
                 every lane that reached a spawn decision was a miss.
       contract: - a two-lane run where the docs lane is a selector miss and the go
                   lane passes exits 0, and stdout names the docs lane AND its
                   derived selector — a miss must not fail a gate that measured
                   real code, and a miss with no selector printed leaves the user
                   unable to see what collected nothing
                 - a run where BOTH matched lanes are selector misses exits
                   exitNothingMeasured (5), not 0 — this is the phase's own
                   false-green, and the assertion is on ExitCode(err), not on the
                   message
                 - a lane with `empty_exit = [5]` whose command exits 5 is a miss
                   and does NOT contribute a red; the SAME lane exiting 1 is a red
                   suite (exitSuiteFailed) — one integer apart, opposite verdicts
                 - a lane with NO empty_exit exiting 5 is a red suite: without a
                   declaration, only the empty-filter can produce a miss (locked
                   empty_detection), so no exit code is interpreted by default
                 - a run with one miss and one RED lane exits exitSuiteFailed, and a
                   run with one miss and one consent-REFUSED lane exits
                   exitLaneRefused — the existing exitRank order is preserved and
                   a miss never outranks a real failure
                 - stdout is never scraped: a lane whose command prints "collected 0
                   items" and exits 0 is reported as a pass, since output matching
                   is the mechanism empty_detection forbids
```

```
  t-7  Document the selector surface
       files:    README.md
                 ARCHITECTURE.md
                 assets/prompts/options.md
                 internal/cmd/options_docs_test.go
       covers:   c-1, c-2, c-5, c-7
       depends:  t-4, t-5
       desc:     Extends the three lane sections with the selector styles, the
                 empty_exit declaration, the widened meaning of exit 5 and the
                 remove-then-re-add edit story; extends TestReadmeDocumentsTestLanes
                 with the new needles so the doc guard cannot pass on stale text.
       contract: - TestReadmeDocumentsTestLanes fails when README.md loses any of
                   "--selector", "go-package", "empty_exit" or "selector miss" —
                   the guard's needle list grows with the surface, which is the
                   one thing the existing test could not catch
                 - options.md's lane section names `--selector` and `--empty-exit`
                   and still routes editing through remove-then-re-add; a new
                   assertion fails if it tells the reader to hand-edit
                   runtime.test_lane
                 - TestOptionsSectionNumbersAreContiguous keeps passing after the
                   edit (the lane text grows in place; no new numbered section)
                 - README's exit-code table row for 5 reads as "the file set matched
                   no lane, or every matched lane's selector collected nothing" —
                   an agent that saw only the old wording would treat an all-miss
                   run as an unexplained non-zero and could route it as a red suite
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-3, t-5, t-7 |
| c-3 | t-5 |
| c-4 | t-2, t-3 |
| c-5 | t-6, t-7 |
| c-6 | t-5 |
| c-7 | t-4, t-7 |

7/7 criteria covered.

## Judgment calls

- **`Selection.Lanes` becomes `[]LaneHit` rather than gaining a parallel
  `Paths map[int][]string`.** The map is the smaller diff and touches no caller;
  it is also a shape where a lane can exist in one field and not the other, and
  R2 is exactly a lane getting the wrong paths. One value that cannot half-exist
  costs a one-line caller edit in `runTestLanes`.
- **Existence filtering lives in `internal/cmd`, not in `internal/testlane`.**
  `match.go`'s doc comment makes "no filesystem" a load-bearing property of the
  package, and the pure translator is where the derivation tests get to be
  exhaustive string tests. The filter is policy at the spawn site, so it sits
  with the spawn.
- **Reuse `exitNothingMeasured` (5) for an all-miss run rather than minting a
  seventh code.** Both outcomes mean the same thing to a caller — the run
  measured nothing — and `exitRank` already places 5 correctly beneath red and
  refused. A new code would need its own rank, its own README row, and would
  make the two indistinguishable-to-the-caller states distinguishable for no
  action. Cost: t-7 must widen the README wording, which is why that is an
  explicit contract.
- **A miss is collected separately instead of being folded through
  `worseOutcome`.** Folding is the smaller change and gets c-5's second half
  free, but it fails c-5's first half: `worseOutcome(nil, miss)` returns the
  miss, so one missing lane would fail a run whose other lane genuinely passed.
  The separate list is the only shape that satisfies both halves.
- **`empty_exit` rejects 0 and 255 at declaration time.** Neither is prohibited
  by the spec, and both are traps: 0 turns every pass into a miss, and 255 is
  already spent on ssh transport failure in `remote.Classify`, so a lane
  claiming it would report an unreachable host as "no tests collected". Refusing
  at `lane add` and in `validate` rather than warning, because there is no
  legitimate runner that means "collected nothing" by either.
- **A command already ending in `./...` gets a warning, not a refusal.** It is
  the most likely way c-2 is unmet in practice (`go test ./...` is exactly what
  a Go lane's command is), but detecting it requires guessing at runner syntax,
  and a false refusal would block a legitimate lane. A named warning at
  declaration time puts it in front of the user at the one moment they can act.
- **`go-package` on a root-level file emits `./...`, i.e. the whole tree.** The
  faithful reading of the locked `./<dir>/...` for `dir == "."`, and the
  alternative (`.`, the root package alone) would silently skip subpackages that
  a root-level edit can break. It is a footgun — editing `main.go` costs the
  whole suite — and it is the correct one; the miss/scoping story cannot fix a
  file whose blast radius genuinely is the repo.
- **`none` is a member of `SelectorStyles`, not only its default.** Mirrors
  `RepoLayouts` and `AuthSchemes` in the same file, keeps `List()` from printing
  a set that omits a value a user may reasonably write explicitly, and means an
  explicit `selector = "none"` and an omitted field are the same lane.
