# lane-remote-locality — panel synthesis

Judged cold: I authored none of the three drafts. Every file named by every draft
was checked against the tree, and every claimed seam was read.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** (7 tasks / 4 waves) | 8/8, and the only draft that splits a criterion by *half* — c-1's derivation half (t-1) from its never-a-red-suite half (t-2), c-2's wording (t-3) from its ordering (t-4); the mapping survives a task being cut | Strongest. Nearly every contract names the counterfactual implementation it kills ("an implementation that appended returns `[mise go]` and fails"); byte-comparison rather than reparse on the no-write assertions; ordering asserted by index. The only draft that follows the locked first-token rule to its `FOO=1` consequence and gives it an owner | Slightly over-split: t-2 is a const, a rank arm and an error constructor. Defensible — it isolates the contested ranking into its own commit — but it is the smallest task in the panel | Correct. t-1 ∥ t-2 are genuinely independent; t-3 ∥ t-5 both depend only on wave 1; t-4 ∥ t-6 in wave 3. No declared dep contradicts a wave |
| **mvp** (4 tasks / 3 waves) | 8/8 on paper, but c-1…c-6 all land on a single task. A partial phase leaves six criteria unmeasured together, and c-7 is credited to a docs task that changes no behaviour | Good and seam-real (`spawnRemote`/`spawnLocal` recorders, `remote.ErrTransport`), but six criteria share six contracts — roughly one assertion per criterion where the other two drafts carry three to six | Coarsest. t-2 is the whole engine; t-1 bundles schema + derivation + CLI + `list` across five files while its own judgment call objects to a six-file task | Correct, and its t-3 ∥ t-2 parallelism (doctor shares the helper, not the run path) is right — both other drafts reach the same shape |
| **verification** (6 tasks / 4 waves) | 8/8 with the most explicit table, including shared ownership (c-3 → t-5 routing + t-2 probe data). Credits docs (t-6) with c-2/c-5/c-8, which is generous — those are behaviours, not prose | Equal to risk, and sharper in two places: it pins *unchanged* output with golden strings (laneless repo, no-grant advisory) and it asserts doctor's derived set **equals** `testlane.Toolchain` by calling both in one test — the tightest reading of c-8's "never disagree" in the panel | Well-sized at 6. Its probe/routing split is the panel's most contested boundary, but both halves are independently testable | Correct. 4 waves, every dep honoured; t-2 ∥ t-3 ∥ t-4 off wave 1 is the widest correct parallelism offered |

**Skeleton: risk.** It wins on the two dimensions that decide whether a plan
survives execution — contract specificity and task boundaries chosen for
testability. Its `laneLocality()` as a pure function in its own file is the only
decomposition where c-3, c-4 and c-5 (all set-and-order properties) are provable
without spawning through a fake seam, and its judgment calls are the only ones
that reason from the locked decisions rather than around them.

### Verified against the tree

- Every file named by all three drafts exists. `internal/testlane/` currently holds
  `match.go` / `selector.go` only — the new `toolchain.go` is a new file in an
  existing package, as all three assume.
- `selectRemoteTarget(targets, tools)` (`internal/cmd/remote_pool.go:22`) and
  `preflightRemote(t, tools)` (`internal/cmd/remote_preflight.go:55`) already take a
  tool list and already return `preflight.Ready.Missing`; `resolveTestTarget`
  (`internal/cmd/test.go:769`) calls it with `nil` and discards the preflight. All
  three drafts are right that c-4 is satisfied by threading, not by a new probe.
- `exitRank` (`internal/cmd/test.go:98`) ranks prepare **above** red with an explicit
  written reason: the lane it belongs to measured nothing. That comment bears directly
  on disagreement 1.
- `laneConsentLine` (`internal/cmd/trust.go:298`) hashes Command + Prepare only, so
  the "grant stays GRANTED across a `--toolchain` edit" contract is checkable today.
- `internal/testlane` imports `configenum` and **not** `project` — mvp's stated reason
  for the string-form signature is true.
- `remote_bootstrap_cmd_test.go:320` does iterate `README.md` and `docs/dross.1`, so
  mvp's cited precedent is real — but `docs/dross.1` mentions neither `dross test` nor
  `test lane` anywhere. See disagreement 4.
- `assets/prompts/execute.md` does carry a per-code exit list (1–7) plus a summary
  line "**2, 3, 4, 5, 6 and 7 all mean the run did not happen**". Both mvp and
  verification are right that it is a real surface; neither names the summary line.

## Merged plan

Phase lane-remote-locality — 7 tasks across 4 waves

### Wave 1

```
  t-1  Derive a lane's toolchain set; add the field and its validate rule   [risk skeleton
       files:    internal/project/project.go                                 + verification
                 internal/testlane/toolchain.go                              + mvp]
                 internal/testlane/toolchain_test.go
                 internal/cmd/validate.go
                 internal/cmd/validate_lane_test.go
       covers:   c-1 (derivation half), c-7 (semantics + schema)
       desc:     Add `Toolchain []string` (toml `toolchain,omitempty`) to project.TestLane.
                 New pure `testlane.Toolchain(command, prepare string, override []string)
                 []string` — first whitespace token of command then prepare, deduped in
                 that order, replaced WHOLESALE by override when non-empty. Add a
                 `laneToolchainProblems` arm to laneProblems.
       contract: - prepare `make build` + command `go test ./...` derives [go make] —
                   command token first, prepare second; deriving from the command alone
                   fails it                                                      [risk+verification]
                 - override ["mise"] over a `go` command returns exactly [mise] —
                   an append implementation returns [mise go] and fails                  [all three]
                 - prepare `go build ./...` + command `go test ./...` derives [go]
                   ONCE; a duplicate doubles the `command -v` count and fails the
                   length assert                                                 [risk+verification]
                 - `Toolchain("  ", "", nil)` and `Toolchain("", "", nil)` return an
                   empty slice, never `[""]` — a blank token probes `command -v ""`
                   and reports every lane as missing its toolchain                     [verification]
                 - `FOO=1 go test ./...` derives `FOO=1`: the locked toolchain_source
                   rule is first-token verbatim, and a test expecting `go` here would
                   encode an override of the lock. t-5/t-6 flag the token instead            [risk]
                 - laneProblems reports `toolchain = ["", "go"]` and
                   `toolchain = ["go test"]` naming the lane; a lane with no toolchain
                   key produces no problem line at all                                 [verification]
                 - a project.toml with no `toolchain` key saves back byte-identically
                   (omitempty), so every existing repo's file is unchanged              [verification]
       depends:  —

  t-2  Add exitToolchainMissing and rank it                                            [risk]
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_consent_test.go
       covers:   c-1 (never-a-red-suite half)
       desc:     New const exitToolchainMissing = 8 with its doc comment; renumber
                 exitRank so it sits between exitPrepareFailed and exitSuiteFailed; add
                 toolchainFailure(lane, tool, host) error, the neither-host message.
       contract: - exitRank(exitToolchainMissing) > exitRank(exitSuiteFailed) and
                   < exitRank(exitPrepareFailed); either inequality flipped fails
                   — see disagreement 1
                 - worseOutcome(red-lane err, toolchain-missing err) returns the
                   toolchain-missing one — a lane that never ran must not be hidden
                   by a neighbour's red
                 - toolchainFailure's message names the tool AND both hosts and does
                   not contain "test suite failed"; ExitCode of it is 8, never 1
                 - the existing rank order transport > partial > prepare > red >
                   refused > nothing-measured survives the renumber (all six pairs)
       depends:  —
```

### Wave 2 (depends t-1, t-2)

```
  t-3  Decide per-lane locality from one probe               [risk skeleton + verification]
       files:    internal/cmd/lane_locality.go
                 internal/cmd/lane_locality_test.go
                 internal/cmd/test.go
       covers:   c-2, c-4, c-5
       desc:     Give resolveTestTarget a `tools []string` parameter and have it return
                 the resulting remote.Readiness alongside the target, so the lane union
                 rides the EXISTING preflight probe — one pass, no second ssh. New pure
                 laneLocality(lanes []matchedLane, missing []string, lookPath func(string)
                 (string, error)) returning, per lane: remote / local-with-reason /
                 refused, plus the announcement lines. runTest passes nil.
       contract: - remote missing `pnpm`, having `go`: lane web comes back local and
                   lane go comes back remote from ONE call — a plan moving both fails     [risk]
                 - two lanes both needing `go` contribute one entry to the union;
                   asserted on the recording probe seam as a single `command -v go`       [risk]
                 - the probe seam is called exactly ONCE for the whole run and the
                   `tools` argument is the union — a per-lane implementation calls
                   it twice and fails                                                [verification]
                 - the probe call is recorded strictly BEFORE the first rsync argv,
                   asserted by index against the recorder, not by presence — a probe
                   moved after the sync fails even though it still happened          [verification]
                 - a probe returning remote.ErrTransport yields the existing preflight
                   fallback (Fallback set, Why naming the host) and ZERO per-lane
                   toolchain lines — the c-5 split, asserted on both fields               [risk]
                 - a tool the remote lacks and the injected lookPath also rejects
                   yields the lane's refused verdict carrying exitToolchainMissing,
                   not a local spawn                                                      [risk]
                 - a lane whose PREPARE tool is missing but whose command tool is
                   present comes back local in full (prepare_toolchain: one set)          [risk]
                 - the announcement for a fallen-back lane names the lane, the binary
                   and the host in one line; a line missing any of the three fails        [risk]
                 - with target nil (--local) the probe seam records zero calls            [risk]
                 - `dross test` with no lanes still probes with an empty tool list —
                   the existing resolveTestTarget(..., nil) behaviour — so the
                   lane-less repo's transcript is unchanged                          [verification]
       depends:  t-1, t-2

  t-5  Add --toolchain to lane add/edit/list                  [risk skeleton + mvp + verification]
       files:    internal/cmd/test_lane.go
                 internal/cmd/test_lane_toolchain_test.go
       covers:   c-7 (CLI surface)
       desc:     Repeatable --toolchain (StringArrayVar) on `lane add` and `lane edit`
                 (edit's "nothing to change" gate now accepts either flag, keyed on
                 Flags().Changed); `lane list` prints `toolchain:` for EVERY lane,
                 marked derived or overridden. Validated through t-1's
                 laneToolchainProblems before the write, quoted the way
                 laneSelectorRefusal quotes laneSelectorProblems, so the CLI can never
                 write what validate rejects.
       contract: - `lane add x --command "go test" --toolchain go --toolchain make`
                   writes toolchain = ["go","make"] in flag order; project.Load
                   round-trips both                                                       [risk]
                 - `lane add x --toolchain ""` is refused and project.toml is
                   byte-identical afterwards (compare bytes, not a reparse)       [risk+verification]
                 - `--toolchain "FOO=1"` and `--toolchain "./x"` are refused as not
                   bare binary names — the trap that would otherwise pin a lane to
                   local on every future run with no message pointing at the cause        [risk]
                 - `lane list` prints `toolchain: go (derived)` for an unoverridden
                   lane and `toolchain: mise (overridden)` for an overridden one;
                   BOTH lines present — printing only for declared overrides fails,
                   because c-7 is about inspecting the effective set          [risk+verification]
                 - `lane edit x --toolchain go` leaves match, command, prepare and
                   index identical AND leaves the grant GRANTED — LaneConsented
                   returns the same state before and after; folding Toolchain into
                   laneConsentLine returns stale and fails                        [risk+verification]
                 - `lane edit go --prepare "make build"` with no --toolchain leaves an
                   existing toolchain list byte-identical: the Changed-guard, not
                   emptiness, decides whether the field is rewritten                       [mvp]
                 - `lane edit x --toolchain ""` clears the override back to derived        [risk]
                 - `lane edit x` with neither --prepare nor --toolchain refuses and
                   names BOTH flags                                               [risk+verification]
       depends:  t-1
```

### Wave 3 (depends t-3)

```
  t-4  Wire per-lane locality into runTestLanes         [risk skeleton + mvp + verification]
       files:    internal/cmd/test.go
                 internal/cmd/lane_locality_wiring_test.go
       covers:   c-1, c-2, c-3, c-4, c-6
       desc:     Compute the tools union from `runnable` after the consent loop, pass it
                 to resolveTestTarget, apply laneLocality's plan: sync only when at least
                 one lane stays remote, per-lane target through runLanePrepare/runOneLane,
                 fold refusals through worseOutcome.
       contract: - every matched lane falling back: the recording sync seam records
                   ZERO calls — one rsync fails it (c-4's "never pays for transfer")  [all three]
                 - when only ONE lane's tool is missing, the rsync argv IS recorded
                   exactly once: a partial fallback must not suppress the sync the
                   remote-going lane still needs                                 [verification]
                 - the fallback line for lane A appears BEFORE A's `lane A prepare:`
                   line and before its `lane A: <cmd>` header; ordering asserted by
                   index, not presence (c-2)                                     [risk+mvp]
                 - lane A falls back and its LOCAL suite goes red: the run exits 1
                   naming lane A, not 3, and the transcript contains no remote
                   "command not found" for A (c-1)                               [risk+verification]
                 - one invocation, remote has go and not pnpm: spawnRemote records the
                   go lane's line and spawnLocal the pnpm lane's, each exactly once;
                   both suite results reach worseOutcome (c-3)                   [all three]
                 - .dross/local.toml and .dross/project.toml are byte-identical before
                   and after a fallen-back run, and a second run with the tool now
                   present spawns that lane remotely with no intervening command (c-6) [all three]
                 - a lane refused as neither-host reaches NEITHER spawn seam, its
                   message contains "neither", the run exits 8 not 1, and it is not
                   counted as a selector miss (a miss would sink it to nothing-measured)
                                                                                 [risk+verification]
                 - no remote granted at all: a lane whose derived tool is absent from
                   PATH still runs exactly as today — the local probe is on the
                   fallback path only                                            [verification]
       depends:  t-3

  t-6  Report lane toolchains in doctor's Remote section  [risk skeleton + verification + mvp]
       files:    internal/cmd/doctor.go
                 internal/cmd/doctor_lane_toolchain_test.go
       covers:   c-8
       desc:     checkRemoteMutation probes the mutation tools UNION every declared lane's
                 toolchain (testlane.Toolchain, the same function the run uses) through
                 the same remoteProbeFn call, and prints one row per lane naming its
                 tools and which the host lacks.
       contract: - lanes go and web, fake probe reporting pnpm missing: doctor prints a
                   row for web naming the lane, `pnpm` and the host, and a row for go
                   reporting its toolchain present                        [risk+mvp+verification]
                 - remoteProbeFn is called ONCE and its tools argument contains the
                   adapter tool and both lanes' tools — doctor asking a second question
                   is the drift c-8's "never disagree" clause forbids             [verification]
                 - doctor's derived tool set for a lane EQUALS testlane.Toolchain for
                   the same lane, asserted by calling both in one test              [verification]
                 - one fake probe fixture drives both doctor and a `dross test --files`
                   run: the binary doctor names missing is the binary the run's
                   fallback line names; a divergence fails                                [risk]
                 - a repo declaring NO lane prints no lane row at all and the existing
                   "✓ <host> reachable — workdir <w>, N cores" line is unchanged
                   (golden string); with no grant, the "no remote granted" advisory is
                   unchanged and still not an issue                                [verification]
                 - a lane whose DERIVED token is not a bare binary name (`FOO=1`) is
                   named with the `--toolchain` fix, so the locked first-token rule
                   cannot strand a lane silently                                          [risk]
                 - issue-count behaviour: see disagreement 5 — provisional default is
                   +1 per missing lane tool [mvp+verification], attributed to the lane
                   rather than to an adapter
       depends:  t-1, t-3
```

### Wave 4 (depends t-4, t-5, t-6)

```
  t-7  Document the toolchain flag, the fallback and exit 8   [risk skeleton + verification + mvp]
       files:    README.md
                 assets/prompts/options.md
                 assets/prompts/execute.md
                 internal/cmd/lane_toolchain_docs_test.go
       covers:   — (no criterion of its own; documents the surface t-4/t-5/t-6 deliver)
       desc:     Extend README's `dross test lane` and `dross test` rows with
                 --toolchain, the per-lane fallback and exit 8; extend options.md's
                 test-lane section the same way; add 8 to execute.md's exit-code list.
                 Run `make install` before relying on the prompt change (rules.toml r-01).
       contract: - a readme_doc_test-style test fails if README's `dross test lane` row
                   does not mention `--toolchain`, and if the `dross test` row's
                   exit-code list does not carry `8` and the string "neither" —
                   mirroring options_docs_test.go's existing "does not name exit 7"
                   assertion                                                  [risk+verification]
                 - an options_docs_test-style test fails if options.md's test-lane
                   section does not name `--toolchain` AND the phrase distinguishing a
                   toolchain fallback from an unreachable host              [risk+verification]
                 - execute.md lists 8 with a remedy that does NOT tell the agent to
                   install the binary (that is the deferred remote-toolchain-install
                   phase) — asserted by presence of 8 and absence of an install verb  [verification]
                 - every exit const in test.go appears in execute.md's list: a
                   table-driven test over {1,2,3,4,5,6,7,8} fails when a future code
                   ships without a doc line                                       [verification]
                 - execute.md's summary sentence "2, 3, 4, 5, 6 and 7 all mean the run
                   did not happen" names 8 too — the table-driven test above passes on
                   a list entry alone and would leave that line stale
                   [merge note: the line exists at assets/prompts/execute.md:214 and no
                    draft names it; grafted onto verification's contract, not a new task]
       depends:  t-4, t-5, t-6
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 lane falls back locally, reports its own suite result | t-2, t-4 (t-1 supplies the tool name) |
| c-2 every fallback announced before the lane runs | t-3 (wording), t-4 (ordering) |
| c-3 locality decided per lane in one invocation | t-4 (routing), t-3 (per-lane probe data) |
| c-4 one probe pass, before sync, no transfer when nothing goes remote | t-3 (one pass, before rsync), t-4 (skip + partial-sync) |
| c-5 transport fallback vs toolchain fallback stay distinct | t-3 |
| c-6 never sticky — nothing written | t-4 |
| c-7 `--toolchain` on add/edit, effective toolchain in list | t-1 (semantics + schema + validate), t-5 (CLI) |
| c-8 doctor reports each lane's toolchain against the host | t-6 |

8/8 covered. t-7 owns no criterion and is kept anyway: the docs drift tests need
one owner, and both runners-up independently reached the same conclusion.

## Disagreements

### 1. Where exitToolchainMissing ranks against a red suite

- **risk**: above red, below prepare — "a run where lane A never ran and lane B went
  red would then report red, and the lane that measured nothing would be invisible
  to the caller deciding whether to commit."
- **verification**: below red, above refused — mirrors the existing red-outranks-refused
  reasoning, and explicitly "rejected ranking it with exitPrepareFailed (above red),
  which would report a configuration gap while a genuinely broken suite sat unmentioned."
- **mvp**: silent. Adds 8 to the const block and to exitRank without stating a position.

**Provisional default: risk's — above red, below prepare.** Not on a 1-1 vote but on
the repo's own written doctrine: `exitRank`'s doc comment (`internal/cmd/test.go:98`)
places prepare-failed above red for exactly this reason — "the lane it belongs to
measured nothing, so reporting a neighbour's red would tell the user their code is
broken while leaving the lane that never ran invisible." A neither-host toolchain gap
is the same shape as a failed prepare, not the same shape as an ungranted lane
(consent is a gate the user can open and then commit through; a missing binary is not).

**Why it matters:** it decides what a mixed run reports, and it is pinned by an
assertion in t-2 that the whole wave-3 wiring then inherits. Flipping it later means
rewriting contracts in t-2, t-4 and t-7. If the lead prefers verification's reading,
change it before t-2 is committed, not after.

### 2. How the probe and the routing it feeds are decomposed

- **mvp**: one task (t-2) owning c-1 through c-6 — "they are a single control flow
  inside runTestLanes, and any split would leave intermediate commits that route lanes
  without announcing it."
- **risk**: split by testability — a pure `laneLocality()` in its own file (t-3), then
  the wiring into runTestLanes (t-4). "c-3, c-4 and c-5 are all ORDER and SET
  properties; inlined, each one is only reachable through a full fake-seam run."
- **verification**: split by control-flow stage — probe/union threading (t-2), then
  routing and announcement (t-5), both inside test.go.

**Provisional default: risk's split**, with verification's two sync contracts grafted
into t-3 and t-4. It is the only decomposition that makes the union/dedup arithmetic
testable without spawning, and it answers mvp's objection: t-3 lands a decision
function nothing calls yet, not a router that routes silently.

**Why it matters:** this is the phase's largest task either way. Under mvp's shape a
single red test blocks six criteria at once; under the merged shape wave 3 can fail
while wave 2's decision function stays green and committed.

### 3. Whether validate.go gains a toolchain rule

- **risk** and **verification**: yes — `laneToolchainProblems`, so `dross validate`
  catches a hand-written `toolchain = [""]` or `toolchain = ["go test"]`.
- **mvp**: explicitly no — "no criterion asks for one, an override list has no
  malformed shape the CLI can write, and a derived list cannot be invalid."

**Provisional default: include it, in t-1** (verification's placement, not risk's t-5),
so the wave-2 CLI task can quote a rule that already exists — which is what makes
risk's "the CLI can never write what validate rejects" property true rather than
aspirational. mvp's argument holds only for lists the CLI wrote; project.toml is
hand-editable by design, and a `toolchain = [""]` probes `command -v ""` and reports
every lane as missing its toolchain.

**Why it matters:** it is the difference between a typo surfacing at `dross validate`
and surfacing as an unexplained all-lanes-local run.

### 4a. docs/dross.1 as a documentation surface

- **mvp**: yes — includes it in t-4 and asserts a test fails when README **or**
  `docs/dross.1` omits `--toolchain`, citing `remote_bootstrap_cmd_test.go`.
- **risk** and **verification**: neither touches the man page.

**Provisional default: drop it.** The cited precedent is real —
`remote_bootstrap_cmd_test.go:320` does iterate both files — but `docs/dross.1`
mentions neither `dross test` nor `test lane` anywhere. Satisfying mvp's contract
means writing a `dross test` man section from nothing, which is a documentation phase,
not this one. **This is the panel's one scoring defect**: the file exists, but the
claim about what it documents does not hold.

**Why it matters:** left in, t-7 silently expands into authoring man-page coverage for
a command family the page has never described.

### 4b. assets/prompts/execute.md as a documentation surface

- **mvp** and **verification**: yes — execute.md's exit-code list gains 8.
- **risk**: no — README and options.md only.

**Provisional default: include it.** execute.md carries a real per-code list (1–7) and
tells the agent that codes 2–7 "all mean the run did not happen". Ship 8 without
touching it and the agent reads a missing toolchain as a red suite — the precise
laundering the locked `local_absence` decision exists to prevent.

**Why it matters:** this is the only doc surface an agent reads mid-run. It is the one
that changes behaviour, not just comprehension.

### 5. Whether a lane's toolchain gap counts as a doctor issue

- **risk**: advisory, issue count unchanged — "a lane that falls back still runs and
  still reports its own result; a doctor that goes red on a working configuration is a
  doctor people stop reading." Its contract asserts the count does **not** move.
- **mvp** and **verification**: an issue, count +1 per missing lane tool. Their
  contracts assert exactly +1.

The contracts are directly contradictory: one asserts unchanged, two assert +1.

**Provisional default: +1 (mvp + verification)**, on the 2-1 split and on consistency —
`checkRemoteMutation` already increments per missing adapter tool, and a reader
scanning the ✗ lines would have no way to tell which ✗ counts. Risk's argument is the
better *reasoned* one and the lead may prefer it; it is a one-line change either way.

**Implementation note either way:** `checkRemoteMutation` currently prints missing
tools through `for _, missing := range ready.Missing` with `needBy[missing]` naming the
*adapter* that wanted it. Folding lane tools into the same probe call — which c-8's
"never disagree" requires and all three drafts do — makes a missing lane tool fall into
that loop and print "the  adapter needs it there" with an empty name. Whichever way
the issue count goes, t-6 must attribute each missing tool to lane(s) or adapter(s)
before printing. No draft names this collision; it is grafted into t-6's contract as
"attributed to the lane rather than to an adapter", not as a new task.

### 6. The signature of the derivation helper

- **risk**: `testlane.Toolchain(lane) []string` — takes a `project.TestLane`.
- **mvp** and **verification**: `testlane.Toolchain(command, prepare string, override
  []string) []string` — strings only, because "that package deliberately imports no
  project, and the string form is what makes the first-token rule testable without a
  config fixture."

**Provisional default: the string form.** Verified: `internal/testlane` imports
`configenum` only. A `project` import would not be an import cycle (project imports no
internal package), so risk's form is workable — but it costs the package its purity and
forces every derivation test to build a config fixture for a function that reads two
strings.

**Why it matters:** low. It is a signature, settled once in t-1, and every dependent
contract reads the same either way.

### 7. Whether the new exit code deserves its own task

- **risk**: yes, t-2 in wave 1 — a const, an exitRank arm, an error constructor, and
  the six-pair rank-order assertion.
- **mvp**: explicitly no — "three lines in the same file as the fallback that produces
  them."
- **verification**: folded into t-5, the routing task.

**Provisional default: keep it separate (risk's t-2).** mvp is right that it is small.
It is kept because disagreement 1 is unresolved: isolating the ranking into its own
commit means the contested decision is one revert away rather than tangled into the
wiring. If the lead settles disagreement 1 first, folding t-2 into t-3 is reasonable
and drops the plan to 6 tasks across 4 waves — wave 1 then holds t-1 alone.

**Why it matters:** only for revertability. No criterion coverage changes either way.
