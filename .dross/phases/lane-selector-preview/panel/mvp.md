# MVP lens — lane-selector-preview

Phase lane-selector-preview — 5 tasks across 3 waves

Wave 1
  t-1  Report dropped paths from lane derivation
       files:    internal/cmd/test.go, internal/cmd/test_lane_selector_test.go
       covers:   c-1, c-2
       desc:     laneRunLine returns a laneDerivation{Line, Selector, Dropped, Reason, OK}
                 instead of (string, []string, bool). The existence filter records each
                 path it drops; Reason distinguishes "every matching path is gone" from
                 "the selector derived nothing". runTestLanes (test.go:555) reads .Line /
                 .Selector / .OK and prints exactly what it prints today.
       contract: a new unit test over laneRunLine with one live and one deleted path
                 asserts Dropped == ["internal/gone.go"] and that Line names only the live
                 path — if the filter stops recording drops, it fails. The gate's own
                 behaviour is pinned by the existing TestDeletedPathsAreDroppedFromTheSelector,
                 TestALaneWhoseEveryPathIsGoneDoesNotSpawnAndIsNotGreen and
                 TestASelectorlessLaneIsStillByteIdentical, which must stay green across
                 the signature change.

  t-2  Resolve preview locality without failing
       files:    internal/cmd/lane_preview_locality.go, internal/cmd/lane_preview_locality_test.go
       covers:   c-6
       desc:     previewLocality(root, repoDir string, lanes []matchedLane, probe bool)
                 returns one verdict per lane carrying Site/Host/Absent plus two states the
                 gate has no use for: unprobed (--no-probe) and unresolved (transport
                 fallback or a probe error). It reuses readRemoteGrants, selectRemoteTarget
                 and laneLocality, and never syncs the tree.
       contract: with remoteProbeFn stubbed to return remote.ErrTransport, every verdict is
                 unresolved and the function returns a nil error — a stub returning a
                 non-transport probe error yields unresolved too, so no preview can exit
                 non-zero on a host it could not reach. With probe=false a call-counting
                 remoteProbeFn stub records 0 calls and the verdict names the configured
                 host as unprobed. With the probe answering and `go` in Missing, the lane's
                 verdict is siteLocal with Absent == ["go"].

  t-3  Print selector-template and join in list
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_test.go
       covers:   c-4
       desc:     testLaneList calls the existing printLaneTemplate after its `selector:`
                 line, so the listing renders selector_template and selector_join through
                 the same helper `lane add` / `lane edit` echo them with.
       contract: a lane declaring --selector-template "--package {path}" --selector-join "|"
                 makes `dross test lane list` output contain "selector-template: --package
                 {path}" and "selector-join: |"; a lane declaring neither emits no
                 selector-template/selector-join line at all (the opt-in field-hiding rule
                 the surrounding fields already follow).

Wave 2 (depends t-1, t-2)
  t-4  Assemble preview findings from a file set
       files:    internal/cmd/test_lane_preview.go, internal/cmd/test_lane_preview_test.go
       covers:   c-1, c-2, c-3
       desc:     previewLanes(root, repoDir, proj, files, laneName) builds the report:
                 testlane.Select for lane hits / unmatched / out-of-tree, laneRunLine
                 (t-1) for each hit lane's line, dropped paths and scoped-to-nothing
                 reason, LaneConsented+ConsentState.String() for consent, previewLocality
                 (t-2) for the site. Unknown laneName is the only error it returns;
                 unmatched, out-of-tree and scoped-to-nothing are fields on the report.
       contract: swap spawnLocal and remoteProbeFn for recorders — a preview over a file
                 set hitting two granted lanes records 0 spawnLocal calls and at most 1
                 probe, and no tree sync happens (nothing calls syncTreeTo). An out-of-tree
                 path lands in report.OutOfTree while the in-tree lanes still render, where
                 runTestLanes refuses the whole set with exitBadFileSet. With no grant
                 recorded at all the lane still appears, with Consent == "absent" and no
                 error returned. A lane whose every matching path was deleted appears with
                 its Dropped list and Scoped=="" rather than being omitted.

Wave 3 (depends t-4, t-3)
  t-5  Add `dross test lane preview` verb and --json
       files:    internal/cmd/test_lane_preview.go, internal/cmd/test_lane.go, README.md,
                 internal/cmd/test_lane_preview_test.go
       covers:   c-1, c-2, c-3, c-5, c-6
       desc:     Cobra command registered in testLane(): --files (repeatable), --lane,
                 --no-probe, --json. A bare invocation takes the working tree's changes via
                 `git status --porcelain --untracked-files=all` parsed with porcelainPaths
                 and says how many files it took. Human render prints per lane the derived
                 line, consent state and site; --json emits the same report struct through
                 emitJSON. README's `dross test lane {...}` row gains preview and the
                 selector-template/join listing from t-3.
       contract: `--json` output parses and lanes[0].line is byte-identical to the line the
                 human render prints for that lane, and the JSON carries dropped,
                 unmatched, out_of_tree, consent and site — a field rendered in one and not
                 the other fails TestPreviewJSONCarriesTheSameFacts. A bare preview in a
                 repo with one untracked file under a lane's globs prints "1 file from the
                 working tree" and previews that lane (an untracked directory must not
                 arrive as `dir/`). `dross test lane preview --lane nope` exits non-zero
                 naming the declared lanes, while a preview whose file set matches no lane
                 prints "no lane matches: ..." and exits 0. README grep test: the lane row
                 contains "dross test lane preview".

## Coverage

| criterion | tasks |
|---|---|
| c-1 exact line through the gate's own path, spawns nothing | t-1, t-4, t-5 |
| c-2 names unmatched / out-of-tree / dropped / scoped-to-nothing | t-1, t-4, t-5 |
| c-3 names consent state, never refuses | t-4, t-5 |
| c-4 `lane list` prints selector_template + selector_join | t-3 |
| c-5 `--json` carries the same facts | t-5 |
| c-6 names where each lane would run; unreachable is unresolved | t-2, t-4, t-5 |

## Judgment calls

- Extended laneRunLine in place rather than giving preview its own derivation — a second
  function that builds "the same" line is exactly the divergence c-1 forbids; one signature
  change to the gate's function is cheaper than a drift test between two.
- laneDerivation carries a Reason string rather than an enum — the gate ignores it and
  preview prints it; an enum would need a String() and a parity test for two consumers.
- Split assembly (t-4) from render + CLI (t-5) rather than one preview task — c-5's "same
  facts" is only cheaply testable when one report struct feeds both renderers, and merging
  would put file-set resolution, findings, human render, JSON and cobra wiring in a single
  task.
- previewLocality is its own wave-1 task rather than inline in the preview command — it is
  the only piece that touches the network, and the transport-failure→unresolved and
  probe-error→unresolved arms have no analogue in laneLocality, which falls back to local.
- Bare preview uses `git status --porcelain --untracked-files=all` reusing porcelainPaths,
  not gitStatusRaw verbatim — the default porcelain collapses an untracked directory to
  `dir/`, which no lane glob written against files can match, so the most common bare
  invocation would silently preview nothing.
- README folded into t-5 instead of a docs task — documentation traces to no criterion, and
  the one row edit describes surfaces t-3 and t-5 create.
- No task for a `--files`-plus-positional refusal, a consent-line echo, or a preview cache:
  none of the six criteria asks for them.
