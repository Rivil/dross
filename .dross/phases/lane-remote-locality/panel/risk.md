# risk lens — lane-remote-locality

Phase lane-remote-locality — 7 tasks across 4 waves

The graph is shaped by the failure modes, one owner each:

| risk | owner |
|---|---|
| derivation picks the wrong token, an empty one, or the same one twice | t-1 |
| a missing toolchain is laundered into a red suite / swallowed by a neighbour's red | t-2 |
| mid-run discovery, a second probe pass, transport confused with a toolchain gap | t-3 |
| lanes all move together, the tree is pushed for nothing, the fallback goes sticky | t-4 |
| a typo'd or garbage `toolchain` entry silently pins a lane to local forever; an edit stales a grant it should not | t-5 |
| doctor and the run disagree about what the host has | t-6 |
| the flag ships undocumented | t-7 |

---

Wave 1

```
  t-1  Derive a lane's toolchain set
       files:    internal/project/project.go
                 internal/testlane/toolchain.go
                 internal/testlane/toolchain_test.go
       covers:   c-1 (derivation half)
       desc:     Add `Toolchain []string` (toml `toolchain,omitempty`) to project.TestLane.
                 Add testlane.Toolchain(lane) []string — the first whitespace token of
                 Prepare then Command, deduped in that order, replaced wholesale by the
                 override when the field is set.
       contract: - a lane with prepare `make build` and command `go test ./...` derives
                   [make go] in that order; deriving from the command alone fails it
                 - `toolchain = ["mise"]` on a lane whose command starts with `go` derives
                   exactly [mise] — an implementation that appended to the derived list
                   returns [mise go] and fails
                 - prepare `go build ./...` + command `go test ./...` derives [go] ONCE;
                   a duplicate doubles the `command -v` count and fails the length assert
                 - a lane with an empty or whitespace-only command derives no tool, never
                   an empty-string entry that would be probed as `command -v ''`
                 - `FOO=1 go test ./...` derives `FOO=1` — the locked toolchain_source
                   rule is first-token verbatim, and a test that expected `go` here would
                   be encoding an override of the lock (t-5 flags the token instead)
       depends:  —

  t-2  Add exitToolchainMissing and rank it
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_consent_test.go
       covers:   c-1 (never-a-red-suite half)
       desc:     New const exitToolchainMissing = 8 with its doc comment; renumber exitRank
                 so it sits between exitPrepareFailed and exitSuiteFailed; add
                 toolchainFailure(lane, tool, host) error, the neither-host message.
       contract: - exitRank(exitToolchainMissing) > exitRank(exitSuiteFailed) and
                   < exitRank(exitPrepareFailed); either inequality flipped fails
                 - worseOutcome(red-lane err, toolchain-missing err) returns the
                   toolchain-missing one — a lane that never ran must not be hidden by a
                   neighbour's red
                 - toolchainFailure's message names the tool AND both hosts and does not
                   contain "test suite failed"; ExitCode of it is 8, never 1
                 - the existing rank order transport > partial > prepare > red > refused >
                   nothing-measured survives the renumber (assert all six pairs)
       depends:  —
```

Wave 2 (depends t-1, t-2)

```
  t-3  Decide per-lane locality from one probe
       files:    internal/cmd/lane_locality.go
                 internal/cmd/lane_locality_test.go
                 internal/cmd/test.go
       covers:   c-2, c-4, c-5
       desc:     Give testTarget/resolveTestTarget a `tools []string` parameter so the lane
                 union rides the EXISTING preflight probe (selectRemoteTarget already takes
                 tools and returns preflight.Ready.Missing) — one pass, no second ssh.
                 New laneLocality(lanes []matchedLane, missing []string, lookPath func(string)
                 (string, error)) returning, per lane: remote / local-with-reason / refused,
                 plus the announcement lines. runTest passes nil.
       contract: - remote missing `pnpm`, having `go`: lane web comes back local and lane go
                   comes back remote from ONE call — a plan that moves both fails
                 - two lanes both needing `go` contribute one entry to the tools union;
                   asserted on the recording probe seam as a single `command -v go`
                 - a probe returning remote.ErrTransport yields the existing preflight
                   fallback (Fallback set, Why naming the host) and ZERO per-lane toolchain
                   lines — the c-5 split, asserted on both fields
                 - a tool the remote lacks and the injected lookPath also rejects yields the
                   lane's refused verdict carrying exitToolchainMissing, not a local spawn
                 - a lane whose PREPARE tool is missing but whose command tool is present
                   comes back local in full (prepare_toolchain: one requirement set)
                 - the announcement for a fallen-back lane names the lane, the binary and
                   the host in one line; a line missing any of the three fails
                 - with target nil (--local) the probe seam records zero calls
       depends:  t-1, t-2

  t-5  Add --toolchain to lane add/edit/list
       files:    internal/cmd/test_lane.go
                 internal/cmd/validate.go
                 internal/cmd/test_lane_toolchain_test.go
                 internal/cmd/validate_lane_test.go
       covers:   c-7
       desc:     Repeatable --toolchain on `lane add` and `lane edit` (edit's "nothing to
                 change" gate now accepts either flag); `lane list` prints `toolchain:` for
                 every lane, marking a derived list as derived. laneToolchainProblems in
                 validate.go, quoted by the CLI the way laneSelectorRefusal quotes
                 laneSelectorProblems, so the CLI can never write what validate rejects.
       contract: - `lane add x --command "go test" --toolchain go --toolchain make` writes
                   toolchain = ["go","make"]; project.Load round-trips both
                 - `lane add x --toolchain ""` is refused and project.toml is byte-identical
                   afterwards (compare bytes, not a reparse)
                 - `--toolchain "FOO=1"` and `--toolchain "./x"` are refused as not bare
                   binary names — this is the trap that would otherwise pin a lane to local
                   on every future run with no message pointing at the cause
                 - `lane list` prints `toolchain: go (derived)` for an unoverridden lane and
                   `toolchain: mise` for an overridden one; the derived marker missing fails
                 - `lane edit x --toolchain go` leaves Prepare unchanged AND leaves the grant
                   GRANTED — LaneConsented returns the same state before and after; an
                   implementation that folded Toolchain into laneConsentLine returns stale
                   and fails
                 - `lane edit x --toolchain ""` clears the override back to derived
                 - `lane edit x` with neither --prepare nor --toolchain still refuses
                 - validate reports a hand-written `toolchain = [""]` and a `toolchain` on a
                   lane with no command
       depends:  t-1
```

Wave 3 (depends t-3)

```
  t-4  Wire per-lane locality into runTestLanes
       files:    internal/cmd/test.go
                 internal/cmd/lane_locality_wiring_test.go
       covers:   c-1, c-2, c-3, c-4, c-6
       desc:     Compute the tools union from `runnable` after the consent loop, pass it to
                 resolveTestTarget, apply laneLocality's plan: sync only when at least one
                 lane stays remote, per-lane target through runLanePrepare/runOneLane, fold
                 refusals through worseOutcome.
       contract: - every matched lane falling back: the recording sync seam records ZERO
                   calls — one rsync fails it (c-4's "never pays for the transfer")
                 - the fallback line for lane A appears in the captured output BEFORE A's
                   `lane A prepare:` line and before its `lane A: <cmd>` header; ordering
                   asserted by index, not by presence (c-2)
                 - lane A falls back and its LOCAL suite goes red: the run exits 1 naming
                   lane A, not 3, and the transcript contains no remote "command not found"
                   for A (c-1)
                 - one invocation, remote has go and not pnpm: spawnRemote records the go
                   lane's line and spawnLocal records the pnpm lane's line, each exactly
                   once; both suite results reach worseOutcome (c-3)
                 - .dross/local.toml and .dross/project.toml are byte-identical before and
                   after a run that fell back, and a second run with the tool now present
                   spawns that lane remotely with no intervening command (c-6)
                 - a lane refused as neither-host does not spawn on either seam and is not
                   counted as a selector miss (a miss would sink it to nothing-measured)
       depends:  t-3

  t-6  Report lane toolchains in doctor's Remote section
       files:    internal/cmd/doctor.go
                 internal/cmd/doctor_lane_toolchain_test.go
       covers:   c-8
       desc:     checkRemoteMutation probes the mutation tools UNION the declared lanes'
                 toolchains (testlane.Toolchain, same function the run uses) through the same
                 remoteProbeFn, and prints one row per lane naming its tools and which the
                 host lacks. Lane rows are advisory, mutation rows keep their issue count.
       contract: - a repo declaring lanes go and web, with the fake probe reporting pnpm
                   missing: doctor prints a row for web naming pnpm and the host, and a row
                   for go saying its toolchain is present
                 - a repo declaring NO lane prints no lane row at all — the section is
                   byte-identical to today's output for a laneless repo
                 - one fake probe fixture drives both doctor and a `dross test --files` run:
                   the binary doctor names missing is the binary the run's fallback line
                   names; a divergence fails (c-8's "never disagree")
                 - a lane toolchain gap leaves doctor's issue count unchanged while a missing
                   mutation adapter tool still increments it
                 - a lane whose DERIVED token is not a bare binary name (`FOO=1`) is named
                   with the `--toolchain` fix, so the locked first-token rule cannot strand a
                   lane silently
       depends:  t-1, t-3
```

Wave 4 (depends t-4, t-5, t-6)

```
  t-7  Document the toolchain flag and fallback
       files:    README.md
                 assets/prompts/options.md
                 internal/cmd/lane_toolchain_docs_test.go
       covers:   — (no criterion; documentation of the surface t-4/t-5/t-6 deliver)
       desc:     Extend README's `dross test lane` and `dross test` rows with --toolchain,
                 the per-lane fallback and exit 8; extend options.md's test-lane section the
                 same way. Tick the milestone checkbox line for the detection half only.
       contract: - a test in the readmeBody style fails if README's `dross test lane` row
                   does not mention `--toolchain`, and if the `dross test` row's exit-code
                   list does not carry `8`
                 - a test in the options_docs_test.go style fails if options.md's test-lane
                   section does not name the per-lane local fallback
                 - `make install` is run before the prompt change is relied on (rules.toml
                   r-01) — asserted by nothing, stated as a step
       depends:  t-4, t-5, t-6
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 lane falls back locally, reports its own suite result | t-2, t-4 (t-1 supplies the tool name) |
| c-2 every fallback announced before the lane runs | t-3 (wording), t-4 (ordering) |
| c-3 locality decided per lane in one invocation | t-4 |
| c-4 one probe pass, before sync, no transfer when nothing goes remote | t-3 (one pass), t-4 (before sync, skip) |
| c-5 transport fallback vs toolchain fallback stay distinct | t-3 |
| c-6 never sticky — nothing written | t-4 |
| c-7 `--toolchain` on add/edit, effective toolchain in list | t-5 |
| c-8 doctor reports each lane's toolchain against the host | t-6 |

All 8 covered. t-7 covers none and is kept anyway: the docs drift test needs one owner.

## Judgment calls

- **New exit code (8) rather than reusing exitLaneRefused.** Refused means "this machine has not trusted this line" and sends the reader to `dross trust`; a tool on neither host means no machine can run it and sends them to an install. Collapsing them sends the user to a gate that is already open.
- **Ranked above exitSuiteFailed, below exitPrepareFailed.** Rejected ranking it below red: a run where lane A never ran and lane B went red would then report red, and the lane that measured nothing would be invisible to the caller deciding whether to commit — the exact laundering the locked local_absence decision names.
- **Locality decided by a pure function in its own file, not inlined in runTestLanes.** c-3, c-4 and c-5 are all ORDER and SET properties; inlined, each one is only reachable through a full fake-seam run, and the union/dedup arithmetic in particular would be untestable without spawning.
- **The lane tool union rides the existing preflight probe** (selectRemoteTarget already accepts tools and returns Ready.Missing) rather than adding a second probe call. A second pass would satisfy "one pass before sync" only by accident and would double the ssh round trips on every lane run.
- **Toolchain stays OUT of laneConsentLine.** Rejected including it: the field is never spawned, and staling every grant on a metadata edit trains the user to re-trust reflexively — which is how the next real staleness gets waved through.
- **Derivation keeps the first token verbatim, including `VAR=x` prefixes and wrapper paths** (locked toolchain_source), and validate/doctor NAME such a token with the `--toolchain` fix instead of stripping it. Rejected stripping env prefixes: it reads as helpful and is an override of the lock. Flagging is the risk mitigation the lock leaves room for — without it, local_absence turns a working lane into a non-spawning one with no line explaining why.
- **doctor's lane rows are advisory, not issues.** A lane that falls back still runs and still reports its own result; a doctor that goes red on a working configuration is a doctor people stop reading. Mutation adapter gaps keep their issue count — those genuinely leave a phase unmeasured.
- **The tree is not synced at all when every lane fell back**, rather than synced for consistency. c-4 states it; the cost is that a later `--files` run in the same session re-pays, which is the trade the criterion already made.
