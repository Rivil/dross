Phase lane-remote-locality — 4 tasks across 3 waves

Wave 1
  t-1  Add lane toolchain field, derivation and flags
       files:    internal/project/project.go, internal/testlane/toolchain.go,
                 internal/testlane/toolchain_test.go, internal/cmd/test_lane.go,
                 internal/cmd/test_lane_toolchain_test.go
       covers:   c-7
       description:
                 Add `Toolchain []string` (toml `toolchain,omitempty`) to project.TestLane.
                 New pure `testlane.Tools(command, prepare string, override []string) []string`
                 — first token of each line, deduped in order, replaced wholesale by override.
                 `lane add`/`lane edit` gain a repeatable `--toolchain`; `lane list` prints the
                 effective list, marked derived or overridden.
       contract: testlane.Tools("go test -count=1 ./...", "make build", nil) returns
                 ["go","make"]; the same call with override ["mise"] returns ["mise"] and
                 contains neither "go" nor "make"; an empty prepare adds no second entry.
       contract: `dross test lane add web --command "npx vitest" --toolchain pnpm
                 --toolchain node` writes toolchain = ["pnpm","node"] to project.toml, and
                 `dross test lane list` prints "toolchain: pnpm node" for it and
                 "toolchain: npx (derived)" for a lane that declares no override.
       contract: `dross test lane edit go --prepare "make build"` with no --toolchain leaves
                 the lane's existing toolchain list byte-identical in project.toml — the
                 Changed-guard, not emptiness, decides whether the field is rewritten.

Wave 2 (depends t-1)
  t-2  Route each lane by remote toolchain presence
       files:    internal/cmd/test.go, internal/cmd/test_locality.go,
                 internal/cmd/test_locality_test.go
       covers:   c-1, c-2, c-3, c-4, c-5, c-6
       depends:  t-1
       description:
                 runTestLanes probes the union of every matched lane's tools in ONE
                 preflight, before syncTreeTo. Each lane then gets its own target: remote when
                 the host has all its tools, local when any is missing (announced), and no
                 spawn at all when the tool is also absent from local PATH. New
                 exitToolchainMissing (8) joins the const block and exitRank; the tree sync is
                 skipped when no lane resolves to the remote.
       contract: with the probe reporting Missing ["npx"] for a two-lane run (go lane, web
                 lane), the spawnRemote seam records only the go lane's ssh line and the
                 spawnLocal seam records only the web lane's line — neither records both.
       contract: the fallback line names all three of lane name, missing binary and host,
                 and is emitted before that lane's "lane <name>:" header in the captured
                 output.
       contract: when every matched lane's tool is missing on the host, spawnRemote records
                 zero invocations — no rsync argv at all — and the run still reports each
                 lane's own suite result.
       contract: a probe failing with remote.ErrTransport still prints
                 "could not reach <host> ... running locally instead", prints NO per-lane
                 toolchain line, and sends every lane local; a probe returning Missing prints
                 no transport wording.
       contract: after a run in which a lane fell back on toolchain, .dross/local.toml and
                 .dross/project.toml are byte-identical to their pre-run contents.
       contract: a lane whose tool is missing on the host AND absent from local PATH does not
                 reach either spawn seam, its message contains "neither host has", and the run
                 exits 8 — not 1, which would read as a red suite.

  t-3  Report lane toolchains in doctor's Remote section
       files:    internal/cmd/doctor.go, internal/cmd/doctor_lane_toolchain_test.go
       covers:   c-8
       depends:  t-1
       description:
                 checkRemoteMutation probes the mutation tools plus every declared lane's
                 tools in the same single remoteProbeFn call, and prints one line per lane
                 naming its tools, with a ✗ per missing binary naming lane, binary and host.
       contract: with two declared lanes and a probe returning Missing ["npx"], doctor's
                 Remote section prints a ✗ line containing the lane name, "npx" and the host,
                 prints a ✓ line for the other lane, and the returned issue count rises by
                 exactly one.
       contract: the tool list doctor hands remoteProbeFn for a given project contains every
                 element the run's union helper produces for the same project's lanes — a test
                 comparing the two fails if either grows a list of its own.

Wave 3 (depends t-1, t-2)
  t-4  Document --toolchain and the new exit code
       files:    README.md, docs/dross.1, assets/prompts/options.md,
                 assets/prompts/execute.md, internal/cmd/toolchain_docs_test.go
       covers:   c-7
       depends:  t-1, t-2
       description:
                 README and the man page document `--toolchain` on `dross test lane
                 add|edit` and the toolchain line in `lane list`. options.md gains the field in
                 its test-lane section; execute.md's exit-code list gains 8 and the fallback
                 wording the agent will read.
       contract: a test reading README.md and docs/dross.1 fails when either omits
                 "--toolchain" — the same shape as the `remote bootstrap`/`--apply` assertion
                 in remote_bootstrap_cmd_test.go.
       contract: a test reading assets/prompts/execute.md fails when its exit-code list has no
                 entry for 8, so an agent cannot read a missing-toolchain lane as a red suite.

## Coverage

- c-1 → t-2
- c-2 → t-2
- c-3 → t-2
- c-4 → t-2
- c-5 → t-2
- c-6 → t-2
- c-7 → t-1, t-4
- c-8 → t-3

8/8 criteria covered.

## Judgment calls

- Kept c-1..c-6 in ONE task (t-2) rather than splitting probe / routing / reporting: they are
  a single control flow inside runTestLanes, and any split would chain waves through the same
  function while leaving intermediate commits that route lanes without announcing it.
- Put derivation in internal/testlane as `Tools(command, prepare, override)` taking strings,
  not project.TestLane: that package deliberately imports no project, and the string form is
  what makes the first-token rule testable without a config fixture.
- Ran doctor (t-3) parallel with the engine (t-2), both depending only on t-1: doctor shares
  the derivation helper, not the run path, so sequencing it after t-2 would buy nothing.
- Added NO validate.go rules: no criterion asks for one, an override list has no malformed
  shape the CLI can write, and a derived list cannot be invalid.
- Rejected a separate task for exitToolchainMissing and its exitRank entry: three lines in the
  same file as the fallback that produces them.
- Kept docs as one task rather than folding each doc into the surface it describes: t-1 would
  otherwise touch six files across config, CLI and three doc surfaces, and the README/man
  assertion this repo already uses is one test in one place.
