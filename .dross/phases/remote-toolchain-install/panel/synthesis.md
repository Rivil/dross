# Panel synthesis — remote-toolchain-install

Merged from three independent drafts (risk / mvp / verification). Claims were
checked against the tree: `bootstrapStep`, `bootstrapRecipes`,
`planRemoteBootstrap`, `reportBootstrap`, `remoteExecFn`, `remoteProbeFn`,
`remoteProbeTools`, `remoteMutationTools`, `laneToolUnion`, `laneFallbackLine`,
`laneConsentLine`, `TrustedLaneCommands`, `localKeys`, `laneToolchainProblems`,
`laneToolchainRefusal`, `findLane`, `laneLookPath`, `spawnLocal`,
`RevokeLaneConsent` all exist as named. Two draft claims were corrected in the
merge (see Grafts).

## Scores

| Dimension | risk (7 tasks / 4 waves) | mvp (6 / 3) | verification (6 / 3) |
|---|---|---|---|
| **Criteria coverage** | 9/9, every criterion has a code owner *and* a failure-mode owner (F1–F8 table); c-9 is the only criterion in the panel given a defender rather than a footnote | 9/9, but c-9 rides as one bullet inside t-5 and c-3 is owned solely by the plan layer — the bootstrap report's "no install line" wording has no owner | 9/9, and the only draft that assigns the doc surfaces (ARCHITECTURE.md → t-2, README rows → t-6) so c-2/c-5/c-6 are documented where they are built |
| **Test-contract specificity** | Highest: contracts name the test *and* the mutation it kills (`TestDeclaredLineIsNotArgv0`, `TestInstallFrameIsDisjoint`, `TestBootstrapProbesOnce` asserting call count *and* argument) | Good — seam call-counts rather than prose ("remoteExecFn once, localInstallFn zero") — but t-3's contract says a declared line is planned as *argv*, which is the one contract in the panel that encodes a bug | Very strong, and uniquely adds *structural* sweeps: every table row names a runtime, no row's argv starts with apt/brew/curl/dnf, no key sets both Unknown and Refusal — audits that survive future table edits |
| **Granularity** | 7 tasks; t-7 is test-only and thin, but it is the only place c-9 can fail loudly. t-2 bundles table + resolver + a `bootstrapStep` arm across two files — the largest single task in the panel | Leanest; t-1's schema+validate+flags bundle is right (one field, three call sites). No task exceeds five files | Same bundling as mvp, plus the correct observation that the split rule is per *layer*. t-2 taking `(tool, declared string)` — not a lane — makes it genuinely dependency-free, which no other draft noticed |
| **Wave correctness** | 4 waves. t-4 (offer) correctly dep-free in wave 1. t-7 in wave 4 serializes a cheap test behind both surfaces — defensible, not free | 3 waves, deps all real. But t-3 (resolver) is made to depend on t-1 for no reason its own description needs | 3 waves, but t-4 (offer) is pushed to wave 2 by the suppression rule, and t-3 (consent) depends on t-2 for the shared refusal helper — both are honest deps, and it is the only draft whose wave graph matches its own contracts exactly |

**Skeleton: risk.** It is the only draft that gives every failure mode a single
owner, its contracts name the mutation each test kills rather than the behaviour
it likes, and it is the only draft that caught the argv-vs-shell-line hazard in a
declared `install` line — the defect that would ship as a binary literally named
`npm install -g pnpm`. Its 7-task shape is kept; three of its dependency edges
are replaced with verification's.

## Merged plan

7 tasks across 4 waves.

### Wave 1

```
  t-1  Declare the lane install line                        [risk+mvp+verification]
       files:    internal/project/project.go,
                 internal/cmd/validate.go,
                 internal/cmd/test_lane.go,
                 internal/cmd/validate_lane_test.go,
                 internal/cmd/test_lane_edit_test.go
       covers:   c-4, c-7
       depends:  —
       what:     `Install string` on project.TestLane (`install,omitempty`).
                 laneProblems gains the whitespace-only-install rule inline beside the
                 EXISTING prepare rule (validate.go:227) — not a new
                 laneInstallProblems function; the prepare precedent is an inline arm.
                 `dross test lane add --install` / `edit --install` (set, and
                 `--install ""` to clear through the per-flag Changed guard), quoted
                 through laneToolchainRefusal's existing refusal path; `lane list`
                 prints the line.
       contract: - `install = "   "` is reported by `dross validate` with the prepare
                   rule's wording (reads as absent, fingerprints as present); a lane
                   with NO install key adds ZERO problems, so a validator that nagged
                   every pre-existing lane fails
                 - `dross test lane edit go --install "npm i -g pnpm"` leaves
                   laneConsentLine(lane) BYTE-IDENTICAL and a previously-granted lane
                   still reads ConsentGranted — c-7's non-staleness half proved at the
                   cheapest layer
                 - `--install ""` round-trips to a project.toml with no `install` key
                   at all — asserted on the key's absence, not on an empty string
                 - `dross test lane edit go --toolchain go` leaves an existing install
                   line intact; collapsing the per-flag Changed guard fails it
                 - `--install` alone is not the "nothing to change" refusal
                 - `lane add x --install "go install ./x"` writes install= and
                   `lane list` prints it

  t-2  Install recipe table and resolver                    [verification+risk+mvp]
       files:    internal/cmd/lane_install.go       (new),
                 internal/cmd/lane_install_test.go  (new),
                 ARCHITECTURE.md
       covers:   c-3, c-4, c-8
       depends:  —
       what:     `resolveInstall(tool, declared string) installStep` returning EXACTLY
                 one of: recipe-argv / declared-line / refusal / unknown. Package-class
                 lane tools only, plus explicit runtime refusal rows (go, node, npx,
                 dotnet). A step carries EITHER Argv (dross's own recipe, argv through
                 the seam per locked install_transport) OR Line (the lane's declared
                 string, destined for `sh -c` exactly as Prepare is) — never one shape
                 pretending to be the other. Also declares the two install exec seams
                 both surfaces run through, and `laneInstallable` for t-3's offer.
                 ARCHITECTURE.md gains the component where it is introduced.
       contract: - resolveInstall("go", "") returns a Refusal naming go as a runtime
                   with a NIL Argv; deleting the runtime row turns it into Unknown and
                   the test fails (locked install_scope_for_lanes guard)
                 - resolveInstall("pnpm", "corepack enable pnpm") returns the DECLARED
                   line, and the built-in pnpm entry appears NOWHERE in the returned
                   step — override, never append (c-4)
                 - a tool with no declared line whose tool IS in the table returns the
                   table argv — the half that makes the feature work on lanes already
                   declared (c-4)
                 - a tool in neither returns Unknown with an EMPTY Refusal, carrying
                   the "no install line; add one or install <tool> on the host by
                   hand" wording; a table-driven sweep asserts no key ever sets both
                   Unknown and Refusal (locked undeclared_exit, at the type level)
                 - a declared line is carried as Line, and rendering it as a single
                   argv element is caught by TestDeclaredLineIsNotArgv0 — a line
                   quoted whole would exec a binary literally named
                   `npm install -g pnpm`
                 - a STRUCTURAL sweep over every table row: each installable row names
                   a runtime, and no row's argv starts with apt / brew / curl / dnf —
                   a row that installed a runtime fails the audit

  t-3  Name the remedy on a toolchain fallback              [risk+mvp]
       files:    internal/cmd/lane_locality.go,
                 internal/cmd/lane_locality_test.go,
                 internal/cmd/lane_locality_wiring_test.go
       covers:   c-1, c-5
       depends:  —
       what:     laneFallbackLine appends the exact lane-scoped invocation
                 `dross test lane install <name>` — bare, without --apply — for the ONE
                 lane that fell back. Empty-host and no-absent-tools arms stay silent.
                 Nothing installs.
       contract: - a lane falling back for a missing `pnpm` prints the fallback line
                   AND `dross test lane install <lane>` in one Announce, with that
                   lane's own name
                 - the offer never names `dross remote bootstrap`; the whole-host verb
                   appearing fails the assertion (locked offer_scope)
                 - the offer is the BARE verb, not `--apply` — every other dross offer
                   prints the ceremony's first step, and the verb's own dry run ends
                   with the --apply hint
                 - a lane that went remote, and a run with host == "", produce an EMPTY
                   Announce
                 - a full `runTestLanes` / `dross test --files` fallback run leaves
                   BOTH install seams' call counters at 0 — replaced with recorders,
                   asserted on counts (c-5)
```

### Wave 2

```
  t-4  Grant the install line its own consent namespace     [risk+mvp+verification]
       files:    internal/cmd/local.go,
                 internal/cmd/trust.go,
                 internal/cmd/lane_install.go,
                 internal/cmd/test_lane.go,
                 internal/cmd/trust_lane_install_test.go  (new)
       covers:   c-7, c-8
       depends:  t-1, t-2
       what:     `TrustedLaneInstalls map[string]string` on localStore, ABSENT from
                 localKeys on the TrustedLaneCommands precedent. laneInstallConsentLine
                 in its OWN NUL frame; LaneInstallConsented / GrantLaneInstallConsent /
                 RevokeLaneInstallConsent; `dross trust --lane-install <name>` (with
                 --check, mirroring the existing --check flag) printing the line before
                 it writes; laneInstallRefusal's absent/stale ladder. RevokeLaneConsent
                 drops BOTH grants so the pair stays atomic at the single remove site
                 (test_lane.go:439). The shared install helper refuses an ungranted
                 DECLARED line before it reaches either seam.
       contract: - adding an install line to a lane that already runs green leaves
                   LaneConsented at ConsentGranted while LaneInstallConsented reports
                   ConsentAbsent — one edit, two independent answers; folding Install
                   into laneConsentLine fails it (the exact regression install_consent
                   is locked against)
                 - editing a granted install line reports ConsentStale, NOT
                   ConsentAbsent, and leaves the lane's TEST grant untouched
                 - the two fingerprint namespaces are disjoint: framed install bytes
                   fed back as a bare command line produce a different fingerprint, so
                   a command cannot forge an install grant
                 - an install line containing a NUL yields an empty consent line and is
                   refused as un-grantable, mirroring laneConsentLine
                 - `dross local set trusted_lane_installs …` is refused as an unknown
                   key
                 - `dross test lane remove go` leaves neither trusted_lane_commands[go]
                   nor trusted_lane_installs[go]; a lane re-added under the same name
                   starts ungranted on both
                 - an ungranted declared line: the helper refuses and NEITHER exec seam
                   is called; after `dross trust --lane-install <name>` the same call
                   reaches the seam exactly once
                 - a lane with NO install line whose tool has a built-in recipe reaches
                   the seam with an EMPTY grant store — the table is not gated
```

### Wave 3

```
  t-5  Add `dross test lane install <name> [--apply] [--local]`   [risk+mvp+verification]
       files:    internal/cmd/test_lane_install.go       (new),
                 internal/cmd/test_lane.go,
                 internal/cmd/test_lane_install_test.go  (new),
                 README.md
       covers:   c-5, c-6, c-7, c-8
       depends:  t-1, t-2, t-4
       what:     The lane-scoped verb owning BOTH sides. ONE probe of the lane's
                 toolchain picks the side: gap on the granted host → install there; host
                 fine, or no grant, or --local → install here through a NEW
                 `localInstallFn` argv seam, not spawnLocal (spawnLocal takes a shell
                 LINE — test.go:195 — and recipes are argv). Dry-run by default; --apply
                 is the only path that reaches a seam, and an ungranted declared line is
                 refused BEFORE any I/O. Every line says which machine it acted on.
                 Owns the README rows for the verb, `trust --lane-install`, and
                 bootstrap's lane coverage.
       contract: - host missing the tool → remoteExecFn called once, localInstallFn
                   zero, header names the HOST; gap on this machine → counts and named
                   machine invert, header says "this machine" (c-6, on call counts, not
                   prose)
                 - a transport failure during the probe installs on NEITHER side and
                   exits non-zero naming the host; a local install firing on the
                   strength of an unanswered probe fails the test
                 - without --apply BOTH seams are called ZERO times and the output
                   still prints the argv (or line) it would run, ending with the
                   --apply hint (c-5)
                 - a table-recipe install needs NO grant; the same lane with its own
                   `install` line and no grant is REFUSED, printing the line and
                   `dross trust --lane-install <name>`, and no seam is reached
                 - a runtime refusal exits with the refusal wording and is NOT counted
                   or worded as a failed attempt — the two produce different text and
                   different counters (c-8)
                 - a tool with no recipe and no install line prints the "add one or
                   install <tool> by hand" report and exits 0; a declared install whose
                   exec returns non-zero exits 1 (locked undeclared_exit — two cases,
                   two exit codes, one test each)
                 - a lane both machines already satisfy prints "nothing to install" and
                   exits 0
                 - `dross test lane install nope` lists the declared lane names through
                   the shared findLane and does NOT probe
                 - the README row for the verb names --apply, the `dross trust` row
                   names --lane-install, and the `dross remote bootstrap` row says it
                   covers declared lanes

  t-6  Plan lane toolchains inside `dross remote bootstrap`  [risk+mvp+verification]
       files:    internal/cmd/remote_bootstrap.go,
                 internal/cmd/remote_bootstrap_cmd.go,
                 internal/cmd/remote_bootstrap_lane_test.go  (new),
                 internal/cmd/remote_bootstrap_cmd_test.go
       covers:   c-2, c-3, c-8
       depends:  t-2, t-4
       what:     planRemoteBootstrap probes through doctor's EXISTING remoteProbeTools
                 (doctor.go:1435 — it already unions remoteMutationTools with
                 laneToolUnion) rather than re-deriving the union, plus
                 bootstrapRuntimes for both halves, in the one existing round trip.
                 Lane steps are planned through t-2's resolver and tagged with the lane
                 that wants them (remoteProbeTools returns needBy for adapter tools
                 only, so the lane attribution is a new field on bootstrapStep, rendered
                 `(lane <name>)` beside the adapter's `(<adapter>)`). reportBootstrap
                 gains the Unknown bucket, which prints and does NOT count toward the
                 exit; an ungranted lane install line is a REFUSAL there too.
       contract: - a repo with one adapter and two lanes issues EXACTLY ONE
                   remoteProbeFn call whose tool list is the deduplicated union — the
                   recorder asserts both the count and the argument; a second probe is
                   a second answer that can differ from the first (c-2)
                 - `go`, wanted by both a Go lane and gremlins' recipe, appears as ONE
                   step; a duplicate line fails
                 - bootstrap and doctor's Remote section derive their tool list from the
                   SAME remoteProbeTools — either growing a private derivation fails,
                   because the drift shows up as doctor passing on a host the run then
                   falls back from
                 - lane and adapter steps render through the same already-installed /
                   would-install / refused vocabulary: a present tool produces the same
                   `✓ … already installed` shape on both
                 - a lane step with no recipe and no install line prints "no install
                   line" and does NOT increment the refusal counter: it exits 0 when it
                   is the only gap, while an unchanged mutation refusal in the same run
                   still exits 1 (locked undeclared_exit, at the report layer)
                 - a lane tool that IS a runtime exits 1 as a refusal, counted in
                   `refused` and never in `failed`
                 - under --apply a lane's declared install reaches remoteExecFn as
                   `sh -c <line>` ONLY when its install grant passes; ungranted, the
                   exec recorder stays EMPTY and the step reports a refusal, never a
                   failed attempt — bootstrap is not a way around t-4's gate
                 - without --apply no lane step reaches remoteExecFn (c-5)
```

### Wave 4

```
  t-7  Prove an install leaves no residue                   [risk]
       files:    internal/cmd/lane_install_residue_test.go  (new)
       covers:   c-9
       depends:  t-5, t-6
       what:     The c-9 invariant asserted across BOTH surfaces rather than assumed:
                 an applied install writes nothing anywhere, and the next run routes the
                 lane remotely on the probe's answer alone.
       contract: - `.dross/local.toml` and `project.toml` are BYTE-IDENTICAL before and
                   after an applied `dross test lane install --apply` and an applied
                   `dross remote bootstrap --apply` — fails the moment either surface
                   caches an "installed" flag
                 - with the probe now reporting the tool PRESENT, laneLocality returns
                   siteRemote for that lane with no grant read and no user gesture (the
                   "no user action" half of c-9)
                 - an install grant recorded for a DECLARED line is not consulted by the
                   locality decision at all — routing must not start depending on
                   consent state
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 fallback names the lane-scoped remedy | t-3 |
| c-2 bootstrap plans lanes + adapters, one probe, one vocabulary | t-6 |
| c-3 never guess a recipe | t-2, t-6 (report) |
| c-4 line overrides table, table serves undeclared lanes | t-1, t-2 |
| c-5 dry-run default, `dross test` never installs | t-3, t-5, t-6 |
| c-6 installs on the side with the gap, and says which | t-5 |
| c-7 install line's own consent fingerprint | t-1, t-4, t-5 |
| c-8 refusal ≠ failed attempt | t-2, t-4, t-5, t-6 |
| c-9 no sticky state | t-7 |

### Grafts onto the skeleton

- **verification** → t-2 is dependency-FREE. Its resolver takes `(tool, declared
  string)`, not a lane, so it needs nothing from t-1's schema. risk and mvp both
  chained it behind t-1 for no reason either description needed; freeing it puts
  three tasks in wave 1 instead of two.
- **verification** → t-6 reuses doctor's existing `remoteProbeTools`. Checked:
  that function already unions the mutation tools with `laneToolUnion`. risk and
  mvp both wrote "probe the union of remoteMutationTools and laneToolUnion",
  which re-implements a function that is already there and is exactly the second
  derivation risk's own F7 is about.
- **verification** → the structural table sweep (no row's argv starts with
  apt/brew/curl/dnf; every installable row names a runtime) and the doc
  assignments (ARCHITECTURE.md → t-2, README → t-5).
- **mvp** → a NEW `localInstallFn` argv seam rather than reusing `spawnLocal`.
  Checked: `runLocalCommand(dir, line string, …)` (test.go:195) is a shell-LINE
  seam; pushing an argv recipe through it re-introduces the quoting bootstrap's
  `install_transport` lock removed.
- **mvp + verification** → bootstrap refuses an ungranted lane install line
  (t-6). risk's t-6 is silent on it, which would have left the whole-host verb as
  the way around the gate t-4 builds.
- **mvp** → `--check` on `dross trust --lane-install`, matching the existing flag
  (trust.go:620); and the `--install ""` round-trip asserted on the KEY's absence
  rather than an empty string.
- **Correction to risk and verification**: both describe the whitespace-only rule
  as living beside `laneToolchainProblems`. In the tree it is an INLINE arm in
  laneProblems (validate.go:227). mvp's phrasing is the accurate one and is what
  t-1 carries.

## Disagreements

### 1. Does c-9 get its own task?
- **risk**: yes — t-7, a test-only task in its own wave. An invariant of the form
  "we simply don't write anything" has no defender unless something fails when
  it is violated.
- **mvp** / **verification**: no — one contract bullet inside the install verb
  ("local.toml and project.toml byte-identical after --apply").
- **Default taken**: risk's separate task, kept in wave 4.
- **Why it matters**: folded into t-5, c-9 is only ever asserted on the lane-verb
  surface — bootstrap's `--apply` could start caching a probe result and no test
  would notice. The cost is one serialized wave holding a single cheap test file;
  the alternative is that the panel's only *absence* invariant is owned by the
  task most likely to be edited later. If the wave-4 tail is judged too
  expensive, the fallback is mvp's: fold the two byte-identity assertions into
  t-5 and t-6 respectively and drop the wave.

### 2. How a lane's declared `install` line reaches the host
- **risk**: a step carries EITHER `Argv` (dross's own recipe) OR `Line` (the
  declared string, run through `sh -c` like Prepare) — never one shape pretending
  to be the other, guarded by `TestDeclaredLineIsNotArgv0`.
- **verification**: same, stated as a contract at the bootstrap surface (`sh -c
  <line>`).
- **mvp**: says the plan holds "that line's argv" and asserts "the planned argv
  is the lane's, not the table's" — a shell line collapsed into the argv field.
- **Default taken**: risk's two-field split.
- **Why it matters**: `install = "npm install -g pnpm"` quoted whole into an argv
  would exec a binary literally named `npm install -g pnpm`; split across spaces
  instead, any quoted argument in the line breaks. Only a distinct `Line` field
  routed to `sh -c` is correct, and it is the difference between the feature
  working and the escape hatch being unusable. mvp is not offering a design here
  so much as under-specifying one, but its contract as written would pass on the
  broken shape, which is why the default is recorded rather than assumed.

### 3. Which seam performs the LOCAL half of the install
- **mvp**: a new `localInstallFn`, on the grounds that recipes are argv and
  re-joining them to a shell line puts quoting back where bootstrap removed it.
- **risk** / **verification**: assert against `spawnLocal` / "the local spawn
  seam", implying reuse.
- **Default taken**: mvp's new `localInstallFn` for table argv; a declared LINE
  goes through the shell-line path, consistent with disagreement 2.
- **Why it matters**: `spawnLocal` is `runLocalCommand(dir, line string, …)` — a
  shell-line seam. Reusing it for an argv recipe means joining the argv back into
  a string at the exact point `install_transport` was locked to prevent it. The
  cost of the graft is that risk's and verification's t-5/t-6 contracts, which
  name `spawnLocal` as the seam they assert zero calls on, must instead assert on
  `localInstallFn` — the assertions survive, the seam name changes.

### 4. Is the c-1 offer suppressed when nothing in the gap is installable?
- **verification**: SUPPRESSED. An offer whose answer is "no install line"
  teaches the reader to skim fallback lines, which is the one thing c-1 cannot
  afford. This is what pushes its offer task into wave 2, behind the resolver.
- **risk** / **mvp**: printed unconditionally on any fallback; the offer task is
  dep-free in wave 1.
- **Default taken**: unconditional (risk + mvp), t-3 stays in wave 1.
- **Why it matters**: c-1's text conditions on the fallback, not on
  installability — "the fallback line is followed by the exact `dross test lane
  install` invocation for that lane". And the suppressed case is not a dead end:
  running the offered verb prints undeclared_exit's own "add one or install
  <tool> on the host by hand", which IS the remedy for that lane. Suppression
  also costs a dependency edge and a wave slot for the one task that owns the
  "a fallback installs nothing" invariant. If the offer is later judged noisy on
  unrecipe-able tools, verification's `laneInstallable` gate is a one-line change
  inside t-3 — but it should then move to wave 2.

### 5. Where the recipe table lives
- **risk**: generalize the EXISTING `bootstrapRecipes` in remote_bootstrap.go
  into the one table both surfaces read — two tables is the mechanism by which
  two surfaces start disagreeing about the same host.
- **mvp**: a separate `laneInstallRecipes` beside it, also in remote_bootstrap.go.
- **verification**: a new table plus `resolveInstall` in a new lane_install.go,
  leaving `bootstrapRecipes` alone.
- **Default taken**: verification's — new table + resolver in lane_install.go,
  `bootstrapRecipes` left in place, with t-6 routing lane steps through the
  resolver.
- **Why it matters**: `bootstrapRecipes` is keyed by the tool names
  `remoteMutationTools` produces and its entries carry a `runtime` prerequisite
  that `bootstrapRuntimes` walks; merging the lane table into it means the
  runtime-probe derivation starts walking lane rows too, which is a live
  behaviour change to the mutation half of a command that works today. Two tables
  in one file, read by one resolver, is one vocabulary in the sense c-2 asks for
  — the vocabulary is the four-arm step type, not the map literal. risk's
  objection is real but applies to two *resolvers*, and the merged plan has one.

### 6. The fourth arm's name and its exit semantics
- **risk**: `Unavailable`, a distinct arm so "does this count toward exit 1" is a
  type question rather than a string comparison; a lane RUNTIME refusal still
  counts toward exit 1.
- **verification**: `Unknown`, with the same rule spelled out as a judgment call
  — runtime refusals count, "no recipe and no line" does not.
- **mvp**: `Undeclared`, asserted as three separate field checks; silent on
  whether a lane runtime refusal counts.
- **Default taken**: `Unknown` (verification's name, since the resolver lives in
  its file), a distinct arm (risk's reasoning), and a lane runtime refusal DOES
  count toward the non-zero exit.
- **Why it matters**: the name is cosmetic; the exit rule is not. undeclared_exit
  locks only the no-recipe/no-line case as exit-neutral, and c-8 requires a
  refusal to name an owner action — which the runtime rows do and the unknown row
  does not. Getting this backwards either makes every lane repo start exiting 1
  on a command that passed the day before (if Unknown counts) or silently softens
  the mutation half's existing `npx`/`dotnet` refusals (if runtime refusals stop
  counting). Both drafts that addressed it agree; the default records it so mvp's
  silence is not read as assent to either error.
