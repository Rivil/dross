# lane-remote-locality — verification-lens draft

Phase lane-remote-locality — 6 tasks across 4 waves

Wave 1
  t-1  Derive lane toolchain; add override field
       files:    internal/testlane/toolchain.go, internal/testlane/toolchain_test.go,
                 internal/project/project.go, internal/cmd/validate.go,
                 internal/cmd/validate_lane_test.go
       covers:   c-7
       description:
                 New `testlane.Toolchain(command, prepare string, override []string) []string`:
                 first whitespace token of command and of prepare, deduped in that order,
                 replaced wholesale by override when non-empty. Adds
                 `TestLane.Toolchain []string` (`toml:"toolchain,omitempty"`) and a
                 `laneToolchainProblems` arm in laneProblems.
       contract: - `Toolchain("go test -count=1 ./...", "make build", nil)` returns
                   `["go","make"]` — command token first, prepare second, and a lane
                   whose prepare repeats the command's tool (`go build`) returns `["go"]`,
                   not `["go","go"]`, so the probe never asks twice for one binary.
                 - `Toolchain("go test ./...", "", []string{"gotestsum"})` returns exactly
                   `["gotestsum"]`: the override REPLACES the derived list, it does not
                   extend it. If someone implements it as an append, this fails.
                 - `Toolchain("  ", "", nil)` and `Toolchain("", "", nil)` return an empty
                   slice, never `[""]` — a blank token would probe `command -v ""` on the
                   host and report every lane as missing its toolchain.
                 - laneProblems reports `toolchain = ["", "go"]` and `toolchain = ["go test"]`
                   as project.toml faults naming the lane; a lane with no toolchain key
                   produces no problem line at all (the field is opt-in).
                 - project round-trip: a project.toml with no `toolchain` key saves back
                   byte-identically (omitempty), so declaring nothing keeps every existing
                   repo's file unchanged.

Wave 2 (depends t-1)
  t-2  Probe every lane's toolchain in one pass
       files:    internal/cmd/test.go, internal/cmd/test_lane_toolchain_probe_test.go
       covers:   c-4
       depends:  t-1
       description:
                 runTestLanes builds the union of every runnable lane's toolchain and
                 passes it to resolveTestTarget, which forwards it to
                 selectRemoteTarget/preflightRemote and returns the resulting
                 remote.Readiness alongside the target. syncTreeTo is skipped when every
                 runnable lane's toolchain intersects Readiness.Missing.
       contract: - With two granted lanes (`go test` + `pytest`), the fake probe seam is
                   called exactly ONCE for the whole run and the `tools` argument it
                   receives is the union `["go","pytest"]` — a per-lane implementation
                   calls it twice and fails.
                 - The recorded spawn order shows the probe call strictly before the first
                   rsync argv: asserted by index against the recorder, not by presence, so
                   a probe moved after the sync fails even though it still happened.
                 - When the probe reports BOTH lanes' tools missing, the remote recorder
                   holds zero argvs — no rsync, no ssh. A run that cannot send any lane
                   remote never pays for the transfer.
                 - When the probe reports one lane's tool missing, the rsync argv IS
                   recorded exactly once: a partial fallback must not suppress the sync
                   the remote-going lane needs.
                 - `dross test` with no lanes (whole-suite path) still probes with an empty
                   tool list — the existing `resolveTestTarget(..., nil)` behaviour — so
                   the lane-less repo's transcript is unchanged.

  t-3  Add --toolchain to lane add/edit and list
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_toolchain_test.go
       covers:   c-7
       depends:  t-1
       description:
                 `--toolchain` (StringArrayVar, repeatable) on add and on edit, validated
                 through laneToolchainProblems before the write; `edit` accepts it as a
                 second changeable field alongside --prepare, keyed on Flags().Changed.
                 `list` prints a `toolchain:` line for every lane, marked derived or
                 overridden.
       contract: - `lane add go --match ... --command "go test ./..." --toolchain gotestsum
                   --toolchain make` writes `toolchain = ["gotestsum","make"]` into
                   project.toml, in flag order.
                 - `lane add go ... --toolchain ""` is refused and project.toml is byte-for-byte
                   unchanged afterwards (compare the file bytes, not just the error) — the
                   CLI must never write a lane `dross validate` would then reject.
                 - `lane edit go --toolchain gotestsum` changes ONLY the toolchain: the lane's
                   match, command, prepare and index in project.toml are identical before
                   and after, and its consent grant still reads GRANTED (not stale) — the
                   toolchain names binaries that are probed, never spawned, so it is not
                   part of laneConsentLine.
                 - `lane edit go` with neither --prepare nor --toolchain refuses and names
                   BOTH flags; `lane edit go --prepare ""` still clears the prepare and
                   leaves an existing toolchain in place.
                 - `lane list` on a lane declaring no toolchain prints
                   `toolchain: go (derived)` for `command = "go test ./..."`; on one declaring
                   `toolchain = ["gotestsum"]` it prints `gotestsum (overridden)`. Both lines
                   are present — an implementation that prints the line only for declared
                   overrides fails, because c-7 is about inspecting the probe set without
                   opening project.toml.

  t-4  Report lane toolchains in doctor's Remote section
       files:    internal/cmd/doctor.go, internal/cmd/doctor_lane_toolchain_test.go
       covers:   c-8
       depends:  t-1
       description:
                 checkRemoteMutation folds every declared lane's toolchain into the same
                 remoteProbeFn call as the mutation tools and prints one row per lane —
                 name, its tools, and which of them the host lacks.
       contract: - With lanes `go` (go test) and `docs` (markdownlint) plus the gremlins
                   adapter, remoteProbeFn is called ONCE and its tools argument contains
                   gremlins, go and markdownlint — doctor asking a second question is the
                   drift c-8's "never disagree" clause forbids.
                 - The fake probe returning `Missing: ["markdownlint"]` produces a doctor
                   line naming lane `docs`, the binary `markdownlint` and the host, and
                   increments the issue count by exactly 1; the `go` lane's row reports it
                   as present and adds no issue.
                 - Doctor's derived tool set for a lane equals `testlane.Toolchain` for the
                   same lane — asserted by calling both in one test — so a lane doctor calls
                   ready cannot be one the run then falls back on.
                 - A repo declaring no lanes prints no lane toolchain rows at all, and the
                   existing "reachable — workdir, N cores" line is unchanged (golden string).
                 - With no grant, no lane row prints and the existing "no remote granted"
                   advisory is unchanged, still not an issue.

Wave 3 (depends t-2)
  t-5  Route and announce per-lane toolchain fallback
       files:    internal/cmd/test.go, internal/cmd/test_lane_locality_test.go,
                 internal/cmd/test_lane_locality_refusal_test.go
       covers:   c-1, c-2, c-3, c-5, c-6
       depends:  t-2
       description:
                 Per lane, a tool in Readiness.Missing sends that lane's prepare and command
                 to the local spawn seam instead of the remote one, announced before the lane
                 header. When the tool is also absent locally (execLookPath) the lane does
                 not spawn and takes a new exitToolchainMissing (8), ranked in exitRank.
       contract: - Probe reports `Missing: ["pytest"]`, both lanes granted: the recorded ssh
                   scripts contain the `go test` line and NOT the `pytest` line, and the local
                   spawn recorder holds the `pytest` line and NOT the `go test` line. Locality
                   asserted per lane on both recorders — a run-wide fallback fails the go half,
                   a run-wide remote fails the pytest half.
                 - In that same run the local `pytest` spawn returns exit 1 and the ssh `go`
                   spawn returns nil: the run exits exitSuiteFailed (1) with a message naming
                   lane `docs`. A "command not found" surfacing as exitTransport/exit 3 fails.
                 - The fallback line is printed BEFORE that lane's `lane docs: pytest ...`
                   header — asserted by byte offset in the captured output — and contains the
                   lane name, `pytest`, and the host `helicon`.
                 - An unreachable host (probe returns remote.ErrTransport) still produces the
                   existing `remote: could not reach helicon (...) — running locally instead`
                   string verbatim, and BOTH lanes spawn locally. The toolchain-fallback line
                   and the transport-fallback line do not share a message body: a test asserts
                   the transport wording is absent from a toolchain-fallback run and vice versa.
                 - After a run where the `docs` lane fell back, `.dross/local.toml` and
                   project.toml are byte-identical to before it, and a second run with a probe
                   reporting nothing missing sends `pytest` over ssh with no user action.
                 - Tool missing on the remote AND execLookPath failing for it: that lane never
                   reaches either spawn seam (neither recorder holds its line), the run exits 8,
                   and the message contains "neither" and the binary name. A green neighbour
                   lane does not mask it, and exitRank(8) sits below exitSuiteFailed and above
                   exitLaneRefused — pinned in the same ordered-list test as the other codes.
                 - No remote granted at all: a lane whose derived tool is absent from PATH still
                   runs exactly as today (its line reaches spawnLocal, exit 8 unreachable). The
                   local probe is on the fallback path only.

Wave 4 (depends t-3, t-5)
  t-6  Document toolchain locality and exit 8
       files:    README.md, assets/prompts/options.md, assets/prompts/execute.md
       covers:   c-2, c-5, c-7, c-8
       depends:  t-3, t-5
       description:
                 README's `dross test` and `dross test lane` rows gain the toolchain fallback
                 and exit 8; options.md gains the `--toolchain` paragraph; execute.md's exit
                 code list gains 8 and its remedy.
       contract: - A test in options_docs_test.go's style asserts assets/prompts/options.md
                   contains `--toolchain`, `dross test lane edit`, and the phrase distinguishing
                   a toolchain fallback from an unreachable host; missing any one fails.
                 - README's `dross test` row names exit 8 and the string "neither", mirroring the
                   existing "does not name exit 7" assertion.
                 - execute.md lists 8 with an instruction that does NOT tell the agent to install
                   the binary (that is the deferred remote-toolchain-install phase) — asserted by
                   the presence of 8 and the absence of an install verb in its bullet.
                 - Every exit const in test.go appears in execute.md's list: a table-driven test
                   over {1,2,3,4,5,6,7,8} fails when a future code is added without a doc line.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-5 |
| c-2 | t-5, t-6 |
| c-3 | t-5 (routing), t-2 (per-lane probe data) |
| c-4 | t-2 |
| c-5 | t-5, t-6 |
| c-6 | t-5 |
| c-7 | t-1 (semantics + schema), t-3 (CLI), t-6 (docs) |
| c-8 | t-4, t-6 |

8/8 criteria covered.

## Judgment calls

- **Reused `selectRemoteTarget(targets, tools)` / `preflightRemote` rather than adding a
  second probe.** The existing preflight already takes a tool list, already returns
  `Readiness.Missing`, and already runs before `syncTreeTo` — and `remote_pool.go` already
  documents that a reachable host missing tools is deliberately not skipped. c-4 is
  therefore satisfied by threading the union through the call that exists. A new
  lane-probe pass would have been a second question about the same host, which is the
  doctor/run disagreement c-8 forbids.
- **Split the probe pass (t-2) from the routing it feeds (t-5) despite both editing
  test.go.** t-2 lands a decision nothing acts on, which is the same shape the existing
  `runTestLanes` comment uses for resolution-before-consent. It buys a wave-2 task whose
  contract ("probe called once, before rsync, union of tools; no rsync when all lanes fall
  back") is testable with no fallback behaviour in place at all — c-4 is the criterion most
  easily made untestable by fusing it into the routing.
- **Merged the local-absence refusal into t-5 instead of a task of its own.** They are the
  same branch: a fallback that routed first and checked PATH afterwards has a window where
  a lane lands on a machine without the tool — precisely what the `local_absence` lock
  exists to close. Splitting would create a commit that is wrong by the phase's own lock.
- **The local PATH probe fires only on the fallback path, never on a plain local run.**
  Probing every lane locally would let a mis-derived first token (a `cd x && go test` lane)
  refuse a run that works today. The lock says "before falling back", and that reading is
  the one that keeps lane-less and remote-less repos byte-identical.
- **`toolchain` is NOT part of `laneConsentLine`.** The fingerprint covers lines that are
  spawned; toolchain names binaries that are only probed. Folding it in would stale every
  grant the moment a user set an override, and would report a lane whose spawned lines never
  changed as one whose command CHANGED. Rejected including it; t-3's contract pins the grant
  staying granted across a `--toolchain` edit.
- **`--toolchain` is editable in place, unlike match/command/selector.** Same reasoning
  `lane edit --prepare` already won on: remove-then-re-add drops the grant, and here it would
  drop it for a field that changes nothing about what runs. Rejected the remove-then-re-add
  path for this field.
- **`lane list` prints the toolchain row for every lane, derived ones included** — against
  the file's existing "print opt-in fields only when declared" convention. c-7 asks for the
  *effective* toolchain to be inspectable without reading project.toml, and a row that
  appears only for overrides leaves the derived case — the common one, and the one that
  causes surprise fallbacks — invisible. The `(derived)` / `(overridden)` marker keeps the
  convention's intent.
- **New exit code 8, ranked below red and above refused.** Mirrors the existing
  `exitLaneRefused` reasoning verbatim: a red suite is a fact about the code, a binary
  missing on both machines is a fact about these machines. Rejected ranking it with
  `exitPrepareFailed` (above red), which would report a configuration gap while a genuinely
  broken suite sat unmentioned.
