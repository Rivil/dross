# synthesis — lane-selector-translation

Judged cold: none of the three drafts is mine. Every file, symbol and existing
test name reproduced below was checked against the working tree before it was
accepted (`internal/cmd/test.go`, `test_lane.go`, `validate.go`, `trust.go`,
`enum_divergence_test.go`, `options_docs_test.go`, `internal/testlane/match.go`,
`internal/project/project.go`, `internal/configenum/configenum.go`,
`internal/remote/remote.go`). Two spot-checks changed the merge and are recorded
under Disagreements: **D3** (an empty-default `Set` rejects the empty string, so
an omitted `selector` needs an explicit guard) and **D5** (`enumScanSites` takes
an arbitrary repo path, so mvp's reason for skipping the divergence guard is
false).

## Scores

Scale: 1–5 per dimension.

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** (7 tasks / 4 waves) | 5 — all 7 covered; t-7 over-claims c-1/c-2/c-5 for a docs edit that satisfies none of them | 5 — every bullet is an input→output on a real symbol; uniquely catches the remote spawn site, the pre-existing grant, `-x.go` as a runner flag, and the rendered-bytes round-trip | 5 — one failure mode per task, and the R-table makes each task's reason for existing auditable | 3 — t-7 asserts the "selector miss" README needle and the reworded exit-5 row while depending only on t-4/t-5; the miss wording lands in t-6, so wave 4 contains a task that documents its sibling |
| **mvp** (4 tasks / 3 waves) | 4 — all 7 reached, but c-2+c-3+c-5+c-6 collapse into one task, so a red t-4 says nothing about which criterion broke | 3 — sound where present; no remote assertion anywhere, no shell-quoting assertion despite the locked decision naming it, no header-equals-argv comparison for the docs lane | 2 — t-4 carries the derivation wiring, the on-disk filter, the miss classifier, the all-miss exit and the consent invariant in one commit; that is the phase's whole risk surface in one reviewable unit | 3 — waves are internally consistent, but folding docs into wave-2 t-3 means the README exit-5 row is written before misses exist and never revisited |
| **verification** (7 tasks / 5 waves) | 5 — all 7, each mapped to the task whose named test actually asserts it; c-7 correctly split across t-4 (behaviour) and t-7 (discoverability) | 5 — contracts are named test functions with both directions asserted; uniquely pins `configenum.Normalize` case-insensitivity, the `requiresNormalize` scan-site registration, and the remote miss path | 4 — clean sizes; t-6 carries the existence filter *and* the exit classifier, which is the one task doing two jobs | 5 — the only draft whose docs task depends on the miss task; t-1/t-2 correctly parallel, t-5→t-6→t-7 correctly serial |

**Skeleton: verification.** It is the only draft whose dependency graph is
actually right — docs after the wording they document — and its contracts are
written as named tests against symbols that exist, which makes them checkable
before a line is written rather than after. risk scores level with it on
contracts and beats it on task-boundary reasoning, but ships a wave defect;
mvp's four-task shape is the wrong granularity for a phase whose entire subject
is not lying about what ran.

Grafted from **risk**: the 255 refusal, the already-`./...` warning, the
header-equals-spawn equality, the remote derived line, the grant-issued-before-
this-phase assertion, the leading-dash guard, the rendered-bytes round-trip,
the stdout-is-never-scraped negative, the existence filter moved into t-5.
Grafted from **mvp**: nothing structural survives, but its objection to the
t-5/t-6 split is what forced the filter to move (D2).

## Merged plan

Phase lane-selector-translation — 7 tasks across 5 waves.

### Wave 1

```
  t-1  Declare selector fields, the enum and the validate refusal
       origin:   [verification] skeleton, [risk+mvp] contract grafts
       files:    internal/configenum/configenum.go
                 internal/project/project.go
                 internal/cmd/validate.go
                 internal/cmd/validate_lane_test.go
                 internal/project/project_test.go
       covers:   c-1
       depends:  —
       desc:     Add `configenum.SelectorStyles = newSet("", "path", "dir",
                 "go-package")`. Add `Selector string` (toml
                 `selector,omitempty`) and `EmptyExit []int` (toml
                 `empty_exit,omitempty`) to project.TestLane. Extend
                 laneProblems with a style check, an empty-exit range check and
                 an empty-exit-without-selector check, each naming the lane
                 through the existing laneLabel.
       contract: - [verification] a lane with `selector = "packages"` produces a
                   problem containing `runtime.test_lane "go"` and the accepted
                   styles; the message is asserted to carry the lane NAME, so a
                   refusal naming only the ordinal fails
                 - [risk] the accepted set in that message is rendered from
                   SelectorStyles.List(), never typed as a literal, so a style
                   added to the Set cannot leave the refusal naming a stale set
                 - [verification] a lane with Selector "" and a lane with
                   "go-package" both yield ZERO problems — the empty case is
                   guarded explicitly, because Set.Has("") is false for a Set
                   with no code default (see D3)
                 - [verification] `selector = "GO-PACKAGE"` passes via
                   configenum.Normalize; a hand-rolled `==` fails
                 - [risk+mvp+verification] `empty_exit = [0]` raises a problem
                   naming the lane: 0 is the runner's success code and a lane
                   declaring it would report every green run as a miss
                 - [risk] `empty_exit = [255]` raises a problem: remote.Classify
                   already spends 255 on ssh transport failure, so a lane
                   claiming it would report an unreachable host as "no tests"
                 - [verification] `empty_exit = [300]` raises a problem, and
                   `empty_exit = [5]` on a lane with NO selector raises
                   TestValidateRejectsEmptyExitWithoutSelector — a code that can
                   never fire is a silent misconfiguration
                 - [risk] project.Save on a TestLane with neither field set
                   round-trips project.toml byte-identically to one written
                   before this phase; asserted on the rendered bytes, beside
                   TestNoTestLaneIsAbsentFromTheDocument
                 - [mvp] project_test.go decodes hand-written toml carrying
                   `selector = "go-package"` / `empty_exit = [5]` into
                   Selector/EmptyExit
```

```
  t-2  Report which paths matched which lane
       origin:   [verification+mvp] shape, [risk+verification] contract
       files:    internal/testlane/match.go
                 internal/testlane/match_test.go
       covers:   c-4
       depends:  —
       desc:     Add `Matched map[int][]string` to Selection, populated in
                 Select with the NORMALIZED path for each lane a path hits.
                 `Lanes []int` is unchanged, so every existing caller and test
                 is untouched. Package stays pure: no filesystem, no config.
       contract: - [verification] lanes go(`internal/**`) and docs(`docs/`)
                   against [internal/a.go docs/x.md README.md /etc/passwd] give
                   Matched[0] == ["internal/a.go"] and Matched[1] ==
                   ["docs/x.md"] exactly, with non-membership of README.md and
                   /etc/passwd asserted in BOTH entries — an implementation that
                   appends every in-tree path to every hit lane fails here rather
                   than at the run site
                 - [verification] `./internal/a.go` arrives in Matched as
                   `internal/a.go` while Unmatched keeps the caller's spelling;
                   a single shared slice breaks one of the two assertions
                 - [verification+risk] a lane with globs [`internal/**`,
                   `internal/cmd/*.go`] against internal/cmd/test.go yields a
                   one-element Matched[0] — the existing one-hit-settles-a-lane
                   rule extended to paths
                 - [risk] a path matched by two DIFFERENT lanes appears in both
                   lanes' entries, matching Select's existing per-lane `hit[i]`
                   behaviour
                 - [verification] a lane no path hit has no Matched key at all
                   (not a nil-valued one), so a `for i := range Matched` at the
                   run site can never resurrect a lane Lanes excluded
```

### Wave 2 (depends t-1)

```
  t-3  Derive a selector from matched paths
       origin:   [verification] skeleton, [risk] error contract + edge cases
       files:    internal/testlane/selector.go            (new)
                 internal/testlane/selector_test.go       (new)
                 internal/cmd/enum_divergence_test.go
       covers:   c-2
       depends:  t-1
       desc:     New pure `Derive(style string, paths []string) ([]string,
                 error)`: dedupe, sort lexicographically, then dispatch on
                 configenum.Normalize(style) — `path` emits each path, `dir`
                 each distinct parent, `go-package` `./<dir>/...`. Empty style
                 returns (nil, nil). An unrecognised style returns an error and
                 NO args (D4). Register the switch in enumScanSites with
                 requiresNormalize true.
       contract: - [verification] Derive("dir", [b/x.go a/y.go a/z.go]) and
                   Derive("dir", [a/z.go a/y.go b/x.go]) return the identical
                   ["a","b"] — argv order leaking into the selector fails the
                   equality and the collapse assertion at once (locked
                   selector_derivation)
                 - [verification] three files in internal/cmd under `go-package`
                   yield exactly ["./internal/cmd/..."], length asserted
                 - [verification+mvp+risk] `go-package` on main.go yields
                   ["./..."] — pinned as a decision; "." or "././..." fails
                 - [verification] `path` on two distinct files gives two
                   arguments; the same file twice gives one
                 - [risk] a derived argument that would begin with "-" (a file
                   named `-x.go`) is emitted as "./-x.go", so a matched path can
                   never reach the runner as an option
                 - [risk] Derive("", paths) returns a nil slice and no error —
                   the no-selector lane is a value in the same function, not a
                   branch each caller has to remember
                 - [risk] Derive("packages", paths) returns an error naming the
                   style and the accepted set, and returns NO args; a caller
                   that ignored the error would otherwise spawn unscoped
                 - [risk+verification] TestSelectorDispatchMatchesSelectorStyles
                   in enum_divergence_test.go (the
                   TestMilestoneModeDispatchMatchesConfigenum pattern) fails when
                   Derive's case literals and SelectorStyles diverge in EITHER
                   direction; registered with requiresNormalize true, so a switch
                   over strings.ToLower fails TestDispatchUsesConfigenumNormalize
```

```
  t-4  Accept selector flags on lane add and list
       origin:   [verification] skeleton, [risk] warning + byte-identity graft
       files:    internal/cmd/test_lane.go
                 internal/cmd/test_lane_test.go
       covers:   c-7
       depends:  t-1
       desc:     `dross test lane add` gains `--selector <style>` and repeatable
                 `--empty-exit <code>`, both validated against configenum before
                 the load-modify-save. `dross test lane list` prints `selector:`
                 and `empty-exit:` for the lanes that declare them. Editing stays
                 remove-then-re-add (locked lane_edit_surface).
       contract: - [verification] after `lane add go --match 'internal/**'
                   --command 'go test' --selector go-package --empty-exit 5`,
                   reloading project.toml gives Selector == "go-package" and
                   EmptyExit == []int{5}, and `lane list` prints both labels
                 - [verification+risk+mvp] `--selector packages` is refused
                   naming the accepted styles AND project.toml is asserted
                   byte-identical afterwards, in the shape
                   TestLaneAddWithoutCommandLeavesTheFileUnchanged already pins —
                   a refusal that ran after the save passes an error-only test
                 - [risk] the same refusal fires for `--empty-exit 0`,
                   `--empty-exit 255` and `--empty-exit 300`, so the CLI cannot
                   write a lane `dross validate` would then reject
                 - [verification] `--empty-exit 5` with no `--selector` is
                   refused before the write, the same rule validate enforces
                 - [verification+risk] a lane declaring neither prints NEITHER
                   label — asserted as absence, so an opt-in field never looks
                   like something every pre-existing lane must set
                 - [verification] `--selector ' GO-PACKAGE '` persists as
                   `go-package`, so list and validate never disagree with what
                   was typed
                 - [risk] `lane add go --selector go-package --command "go test
                   ./..."` succeeds but WARNS, naming the trailing `./...`:
                   appending `./internal/cmd/...` to a line that already says
                   `./...` runs the union, i.e. the whole suite, which is c-2's
                   promise silently unmet. A warning, not a refusal — detecting
                   it means guessing at runner syntax, and a false refusal blocks
                   a legitimate lane
                 - [risk] a lane declared through these flags survives a
                   `lane remove` of a DIFFERENT lane unchanged (the existing
                   rewrite path now carries two more fields)
```

### Wave 3 (depends t-2, t-3)

```
  t-5  Append the derived selector to the lane run
       origin:   [verification] skeleton, [risk] filter placement + remote/grant
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_selector_test.go  (new)
       covers:   c-2, c-3, c-4, c-6
       depends:  t-2, t-3
       desc:     runTestLanes carries the lane INDEX alongside the lane through
                 matched/runnable so sel.Matched[i] stays attached; per lane it
                 drops matched paths that no longer exist under repoDir (locked
                 missing_paths), derives with testlane.Derive(lane.Selector, …),
                 builds ONE line via the existing testCommandLine, prints that
                 line in the header and passes the same string to runOneLane
                 (local and remote). A lane whose filtered selector empties does
                 not spawn — its reporting lands in t-6. Derive's error is
                 raised in the existing up-front fence beside shArgvFor, before
                 any lane spawns. Consent still resolves against lane.Command;
                 runOneLane's "no selector appended" doc comment is rewritten,
                 not left contradicting the code.
       contract: - [verification+mvp] a `go` lane with command `go test -count=1`
                   and selector go-package, run as `--files internal/cmd/test.go`,
                   records exactly one spawn of
                   `go test -count=1 './internal/cmd/...'` — a lane that ran its
                   whole suite fails on the recorded line, which is c-2's "scoped
                   run rather than the lane's whole suite" stated as a string
                 - [risk] the string printed by the `lane <name>: ` header is
                   compared for EQUALITY against the recorded spawn line, not
                   each against a literal; a header built from lane.Command while
                   the derived line spawned fails here
                 - [verification] the header index is asserted to precede the
                   spawn sentinel, keeping the existing header-first property
                 - [verification+mvp] a lane with NO selector spawns its command
                   byte-for-byte with --files supplied, while a
                   selector-declaring sibling is present in the same run;
                   TestLaneLineIsByteIdentical must keep passing unmodified (c-3)
                 - [verification] a file set of [internal/cmd/test.go docs/x.md
                   README.md] records a go line containing `./internal/cmd/...`
                   and NOT `docs`, and a docs line (selector dir) containing
                   neither `internal` nor `README` — c-4 asserted on argv, not on
                   Selection
                 - [verification] a lane granted for `go test -count=1` only
                   still spawns, and the spawned line DIFFERS from the granted
                   one: fingerprinting the derived line fails on the refusal,
                   dropping the selector to keep the grant valid fails on the
                   difference (c-6, locked selector_consent)
                 - [risk] a lane granted BEFORE this phase runs green with a
                   selector appended — appending must not re-fingerprint, or
                   every existing grant goes stale on upgrade
                 - [verification] an ungranted lane declaring a selector records
                   ZERO spawns and exits 6; [risk] the refusal names lane.Command,
                   not the derived line
                 - [verification+risk] a matched path containing a space arrives
                   single-quoted in the recorded line via shellQuoteArg
                 - [risk] the remote path gets the derived line too: with
                   spawnRemote recording, the ssh script for a selector-bearing
                   lane contains the selector — asserted, because runRemoteLine
                   is a second call site a local-only wiring silently skips
                 - [risk+verification] two matched paths, one deleted from the
                   fixture: the spawned line carries only the survivor's package,
                   and a lane whose every matched path was deleted records ZERO
                   spawns rather than `go test ./gone/...` (locked missing_paths)
                 - [risk] the existence filter applies ONLY to lanes with a
                   selector: a lane with no style matched by a non-existent path
                   still spawns, so every pre-selector fixture keeps its behaviour
```

### Wave 4 (depends t-5)

```
  t-6  Report selector misses without failing the gate
       origin:   [verification] skeleton, [risk] negatives + rank preservation
       files:    internal/cmd/test.go
                 internal/remote/remote.go
                 internal/cmd/test_lane_miss_test.go      (new)
       covers:   c-5
       depends:  t-5
       desc:     A lane is a selector MISS when its filtered selector emptied in
                 t-5, or when it exits with a code listed in lane.EmptyExit.
                 remote.ExitError gains ExitCode() so the existing exitCoder
                 interface classifies both transports through one path. Misses
                 are printed and counted separately from `worst` — they never
                 enter worseOutcome — and become exitNothingMeasured only when
                 EVERY lane that reached a spawn decision was a miss.
       contract: - [verification+risk+mvp] go lane green, docs lane a miss →
                   ExitCode(err) == 0, and the transcript names the docs lane AND
                   its derived selector. This is c-5's load-bearing half: folding
                   the miss through worseOutcome fails on the exit code, because
                   exitRank puts exitNothingMeasured above nil
                 - [verification+risk] both matched lanes miss → exit 5 and zero
                   spawns; asserted on ExitCode(err), not on the message
                 - [verification+mvp] a lane declaring `empty_exit = [5]` whose
                   spawn returns exit 5 is a miss; the SAME lane exiting 1 is
                   exitSuiteFailed with a `test lane "go" failed` message — one
                   integer apart, opposite verdicts, and the empty-exit arm must
                   not swallow a real red
                 - [risk] a lane with NO empty_exit exiting 5 is a red suite:
                   without a declaration only the empty-filter can produce a miss
                   (locked empty_detection)
                 - [verification] a remote lane exiting 5 through remoteFailure
                   is classified as a miss — fails unless
                   remote.ExitError.ExitCode() exists for errors.As to reach
                 - [risk] a run with one miss and one RED lane exits
                   exitSuiteFailed; a run with one miss and one consent-REFUSED
                   lane exits exitLaneRefused — the existing exitRank order is
                   preserved and a miss never outranks a real failure
                 - [risk] stdout is never scraped: a lane printing "collected 0
                   items" and exiting 0 is reported as a PASS, since output
                   matching is the mechanism empty_detection forbids
                 - [verification] the printed miss line contains both the lane
                   name and the derived selector text, per c-5's wording
```

### Wave 5 (depends t-4, t-6)

```
  t-7  Document the selector surface
       origin:   [verification] skeleton + dependency, [risk] exit-5 rewording
       files:    README.md
                 ARCHITECTURE.md
                 assets/prompts/options.md
                 internal/cmd/options_docs_test.go
       covers:   c-7
       depends:  t-4, t-6
       desc:     Extend README's lane row and exit-code contract, the
                 test-suite-runner and exec-consent sections of ARCHITECTURE.md,
                 and the test-lanes block of options.md (currently lines ~174–180)
                 with the selector styles, the empty-exit codes, the miss outcome
                 and the unchanged grant story. Extend the existing needle guards
                 to cover them.
       contract: - [risk+verification] TestReadmeDocumentsTestLanes gains the
                   needles `--selector`, `go-package`, `--empty-exit` and
                   `selector miss`; a shipped flag with no README line fails it,
                   the same rule that already holds for `dross test --files`
                 - [risk] README's exit-status contract for `5` reads "the file
                   set matched no lane, or every matched lane's selector
                   collected nothing" — and keeps the existing "matched no lane"
                   needle intact. An agent reading only the old wording would
                   treat an all-miss run as an unexplained non-zero and could
                   route it as a red suite
                 - [verification] TestOptionsCoversTheConsentVerbs gains
                   `--selector`, so the settings surface cannot claim to reach
                   every dross-managed setting while omitting the one field that
                   changes what a consented lane spawns
                 - [risk] options.md still routes editing through
                   remove-then-re-add; a new assertion fails if it tells the
                   reader to hand-edit runtime.test_lane
                 - [verification] every `dross test lane add` example in
                   options.md and README is parsed for flags the cobra command
                   actually registers, so a doc advertising `--selector-template`
                   fails
                 - [risk] TestOptionsSectionNumbersAreContiguous keeps passing:
                   the lane text grows in place, no new numbered section
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-3, t-5 |
| c-3 | t-5 |
| c-4 | t-2, t-5 |
| c-5 | t-6 |
| c-6 | t-5 |
| c-7 | t-4, t-7 |

7/7 criteria covered. No task traces to no criterion.

## Disagreements

### D1 — the shape of the per-lane path attribution

- **risk**: replace `Selection.Lanes []int` with `[]LaneHit{Index, Paths}` — one
  value that cannot half-exist, because "lane gets the wrong paths" is the
  failure mode (R2) and a parallel field is a shape where the two can drift.
- **mvp** and **verification**: add `Matched map[int][]string` alongside `Lanes`,
  leaving the existing type and all of match_test.go untouched.
- **Provisional default: the additive map.** Two lenses of three, and the
  existing test file stays intact so any regression this phase causes surfaces as
  a NEW failure rather than a rewritten assertion.
- **Why it matters:** the map permits a state the struct forbids — a lane in
  `Lanes` with no `Matched` key, or the reverse. t-2's contract closes exactly
  that hole from the producer side (no key for an unhit lane; no empty entry for
  a hit one), and t-5 reads `Matched` only for lanes `Lanes` already listed. If
  the executor finds that pairing awkward at the call site in `runTestLanes`
  — which currently discards the index when it builds `matched
  []project.TestLane` — risk's LaneHit is the fallback, and it is a one-line
  caller change plus a rewrite of match_test.go.

### D2 — one wiring task or two, and where the on-disk filter lives

- **mvp**: c-2/c-3/c-5/c-6 are one task, because the intermediate commit spawns a
  derived line with no miss guard — "the merge is a safety property, not just
  economy".
- **risk** and **verification**: split spawn (t-5) from miss reporting (t-6).
  They then disagree on the filter: risk puts the missing-path drop in t-5 with
  the spawn, verification puts it in t-6 with the miss classifier.
- **Provisional default: split, with risk's filter placement.** mvp's objection
  is real and names a specific hazard — under verification's boundary, t-5 ships
  a commit that can spawn `go test ./gone/...` and read as a red gate, which is
  the exact confusion locked `missing_paths` exists to prevent. Moving the filter
  into t-5 removes that hazard while keeping the split, and both halves of the
  arrangement exist in a draft.
- **Why it matters:** the residual transient is now smaller but not zero — after
  t-5 and before t-6, a lane whose selector emptied is silently skipped and the
  run can exit 0 having measured less than it claims. That is one commit wide and
  visible in the transcript (no header, no output for that lane); spawning a hard
  runner error at a gate is not. If the executor will not tolerate any
  false-green window, mvp's merge is the answer and it costs reviewability of the
  phase's densest change.

### D3 — `none` as an enum member, and what an omitted `selector` validates as

- **risk**: `newSet("none", "none", "path", "dir", "go-package")` — "none" is
  both the code default and a writable member, mirroring `RepoLayouts` and
  `CommitConventions`, so `Set.Has("")` is true and an explicit `selector =
  "none"` means the same as an omitted field.
- **mvp** and **verification**: `newSet("", "path", "dir", "go-package")` — the
  three styles the locked `selector_styles` decision names, and nothing else.
- **Provisional default: the empty default, three members.** The locked decision
  enumerates exactly `path`, `dir`, `go-package` "with the field omitted meaning
  no selector"; adding a fourth accepted literal widens a closed enum a locked
  decision closed.
- **Why it matters — and this is a mechanical trap, not a preference.**
  `Set.Has(v)` returns `s.def != ""` for an empty `v`
  (`internal/configenum/configenum.go:51`). With `def == ""`, `Has("")` is
  **false**, so a validator that calls `SelectorStyles.Has(lane.Selector)`
  directly reports a problem for every pre-existing lane in every repo — the
  opposite of c-3's opt-in. t-1 must guard the empty string before the Has call,
  and verification's `TestValidateAcceptsALaneWithNoSelector` is what catches its
  absence. risk's "none" shape gets this for free, which is a real argument for
  it if the executor would rather not carry the guard.

### D4 — an unrecognised style at derive time

- **risk**: `SelectorArgs` returns an error naming the style and the accepted
  set, and no args — "a caller that ignored the error would otherwise spawn
  unscoped" (R1).
- **verification** (and **mvp** by omission): `Derive` returns nil for an unknown
  style, "so a style that slipped past validate can never inject an argument".
- **Provisional default: risk's error**, raised in `runTestLanes`' existing
  up-front fence beside `shArgvFor`, before any lane spawns.
- **Why it matters:** this is the one place the two lenses aim at different
  failures. Returning nil is memory-safe and runs MORE than asked — the lane's
  whole suite — which cannot produce a false green, but silently breaks the c-2
  promise for a hand-edited `selector = "pytst"` with no message anywhere.
  Returning an error refuses the run loudly and reuses a fence that already
  exists for malformed lane command lines, where the precedent is that broken
  lane config stops the run before anything spawns. Note this is 1-of-3 promoted
  over 2-of-3: if the executor prefers the majority, the mitigation is a printed
  warning on the nil path, and t-3's contract bullet flips to assert the warning
  rather than the error.

### D5 — registering the derivation switch in the enum-divergence guard

- **risk** and **verification**: add a scan site so a style added to
  `configenum` with no arm in the derivation switch fails the build.
- **mvp**: rejected — "that guard scans internal/cmd and forge dispatch sites by
  function name; a testlane switch does not fit its helper" — and substitutes an
  in-package table test driven off `SelectorStyles.Values()`.
- **Provisional default: register the scan site.** mvp's stated reason is false.
  `enumScanSites` (`internal/cmd/enum_divergence_test.go:204`) is a
  `{label, path, fn, minCases, requiresNormalize}` registry read through
  `readRepoFile`, and it already carries sites in `internal/ship`,
  `internal/forge` and `internal/project` — an arbitrary repo path is exactly
  what it takes.
- **Why it matters:** only the scan site catches divergence in BOTH directions
  (a member with no arm, and an arm configenum does not accept), and only it
  reaches `TestDispatchUsesConfigenumNormalize`, which is what stops the switch
  being written over `strings.ToLower`. A `Values()`-driven table catches the
  first direction only.

### D6 — when the docs land

- **mvp**: folded into wave-2 t-3 with the flags — "a standalone docs task traces
  to no criterion and would ship the flags undocumented for a wave".
- **risk**: a wave-4 task depending on t-4 and t-5.
- **verification**: a wave-5 task depending on t-4 and t-6.
- **Provisional default: verification's.** The needle guards assert against the
  shipped flag names AND the miss wording; the miss wording does not exist until
  t-6, so risk's dependency set is incomplete and mvp's placement writes the
  exit-5 row before misses exist and never revisits it.
- **Why it matters:** README line 216 already states the exit contract, and
  `TestReadmeDocumentsTestLanes` guards the phrase `matched no lane`. Widening
  that row is the difference between an agent reading an all-miss run as
  "nothing measured, do not commit" and routing it as a red suite. Docs written
  early get the flags right and that row wrong.

### D7 — how far `empty_exit` validation goes

- **mvp**: reject `0` only.
- **risk**: reject `0` and `255` (255 is spent on ssh transport failure in
  `remote.Classify`, `internal/remote/remote.go:534`).
- **verification**: reject `0`, out-of-range (`300`), and `empty_exit` declared
  on a lane with no `selector`.
- **Provisional default: the union — 0, 255, out-of-range, and
  empty-exit-without-selector**, enforced identically in `validate` (t-1) and in
  `lane add` (t-4).
- **Why it matters:** each refusal blocks a config that produces a wrong verdict
  rather than an error — `0` reclassifies every green run as a miss, `255`
  reports an unreachable host as "no tests collected", and an inert declaration
  on a selector-less lane is a user believing they configured something they did
  not. The cost is that the union is the strictest reading and none of it is
  required by the spec; if any of it proves wrong in practice, dropping a check
  is a one-line change, while a shipped lane relying on `empty_exit = [255]` is
  not.

## Note carried out of the spot-check

risk's t-2 cites "the locked multi_lane rule" and both risk and mvp cite locked
decisions (`unmatched_files`, `bare_test_run`) that appear in earlier lane
phases' specs, not in this phase's `spec.toml`. The behaviour they name is real
and already implemented in `Select` and `runTestLanes`, so nothing in the merged
plan depends on a decision that does not exist — but the merged plan cites those
properties as existing behaviour, not as locked decisions of this phase.
