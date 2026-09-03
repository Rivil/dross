# Panel draft — RISK lens

Phase remote-toolchain-install — 7 tasks across 4 waves

The graph is shaped by the eight ways this feature can hurt someone, each owned
by exactly one task:

| # | Failure mode | Owner |
|---|---|---|
| F1 | dross guesses an install command, or installs a language runtime on a borrowed host | t-2 |
| F2 | an ungranted install line executes — or adding one staleness-refuses the lane's *test* runs | t-3 |
| F3 | `dross test` installs as a side effect of a fallback | t-4 |
| F4 | the install lands on the machine that did not have the gap, or on the strength of a probe that never answered | t-5 |
| F5 | an install leaves sticky state behind, so the next run needs a second gesture | t-7 |
| F6 | a refusal is counted as a failed install, so every repo with lanes starts exiting 1 | t-2 (vocabulary) + t-6 (counters) |
| F7 | bootstrap and doctor disagree about what the host needs, or probe it twice | t-6 |
| F8 | a malformed `install` line reaches a shell, or one that reads as absent and fingerprints as present | t-1 |

---

Wave 1

```
  t-1  Declare the lane install line
       files:    internal/project/project.go,
                 internal/cmd/validate.go,
                 internal/cmd/test_lane.go,
                 internal/cmd/validate_lane_test.go,
                 internal/cmd/test_lane_edit_test.go
       covers:   c-3, c-4
       depends:  —
       what:     Adds TestLane.Install (`install = "..."`, omitempty), laneInstallProblems
                 in validate.go beside laneToolchainProblems, and --install on
                 `dross test lane add` / `edit` (quoting validate's problems, as
                 laneToolchainRefusal already does). `lane list` prints the line.
       contract: - a lane carrying `install = "   "` is reported by `dross validate` with
                   the whitespace-only wording prepare already has; delete that arm and
                   TestLaneInstallProblems/whitespace fails (it reads as absent to every
                   caller and fingerprints as present to the grant store)
                 - `dross test lane edit go --toolchain go` leaves an existing install line
                   intact; collapse the per-flag Changed guard and
                   TestLaneEditInstallUntouchedByToolchain fails
                 - `dross test lane edit go --install ""` clears the line and leaves the
                   lane's *command* grant GRANTED (not stale) — TestLaneEditInstallClear
                   asserts LaneConsented still returns ConsentGranted afterwards
                 - `dross test lane add x --install "go install ./x"` writes install=,
                   and `lane list` prints it; drop the list line and
                   TestLaneListShowsInstall fails

  t-4  Name the remedy on a toolchain fallback
       files:    internal/cmd/lane_locality.go,
                 internal/cmd/lane_locality_test.go
       covers:   c-1, c-5
       depends:  —
       what:     laneFallbackLine appends the exact lane-scoped invocation
                 (`dross test lane install <name>`) to the fallback announcement, for the
                 ONE lane that fell back. Empty-host and no-absent-tools arms stay silent.
       contract: - a lane falling back for a missing `pnpm` prints the fallback line AND
                   `dross test lane install <lane>` in the same announcement;
                   TestLaneFallbackNamesTheRemedy asserts both substrings on one Announce
                 - the offer names the lane that fell back, never `dross remote bootstrap`
                   (locked offer_scope) — TestFallbackOfferIsLaneScoped fails if the
                   whole-host verb appears
                 - a lane that went remote, and a run with no grant at all, produce an
                   EMPTY Announce; TestFallbackSilentWhenNothingFellBack fails if either
                   starts advertising the verb
                 - a full `runTestLanes` fallback run calls neither remoteExecFn nor
                   spawnLocal with an install argv — TestFallbackInstallsNothing replaces
                   both seams with recorders and asserts the install seam saw zero calls
```

Wave 2 (depends t-1)

```
  t-2  Resolve an install: table, override, refusal, unavailable
       files:    internal/cmd/remote_bootstrap.go,
                 internal/cmd/lane_install.go            (new),
                 internal/cmd/lane_install_test.go       (new)
       covers:   c-3, c-4, c-8
       depends:  t-1
       what:     Generalizes bootstrapRecipes into the ONE recipe table both surfaces
                 read, and gives bootstrapStep a fourth arm — Unavailable (no recipe, no
                 declared line) — distinct from Refusal. lane_install.go resolves one
                 lane: declared line REPLACES the table entry for that tool; table entry
                 used when no line; neither → Unavailable, named not attempted. A step
                 carries EITHER Argv (dross's own recipe) OR Line (the lane's declared
                 string, destined for `sh -c` like Prepare) — never one shape pretending
                 to be the other.
       contract: - a lane whose toolchain contains `go` produces a REFUSAL naming go as a
                   runtime, never an argv; TestLaneInstallNeverInstallsARuntime fails if
                   the go/node/dotnet arms ever return an Argv (locked install_scope)
                 - a lane declaring `install = "brew install foo"` for a tool the table
                   also knows returns the DECLARED line and not the table argv;
                   TestDeclaredInstallReplacesTable fails on extend-instead-of-replace
                 - a lane with no install line and a table-known tool returns the table
                   argv — TestTableInstallsUndeclaredLane is what makes the feature work
                   on lanes already declared (c-4)
                 - a tool in neither table nor line returns Unavailable with the
                   "no install line; add one or install <tool> on the host by hand"
                   wording and an EMPTY Refusal; collapse the two arms and
                   TestUnavailableIsNotARefusal fails (locked undeclared_exit)
                 - a declared line is carried as Line, and rendering it as a single argv
                   element is caught by TestDeclaredLineIsNotArgv0 — a line quoted whole
                   would exec a binary literally named `npm install -g pnpm`

  t-3  Grant the install line its own consent namespace
       files:    internal/cmd/local.go,
                 internal/cmd/trust.go,
                 internal/cmd/trust_lane_install_test.go  (new)
       covers:   c-7
       depends:  t-1
       what:     localStore.TrustedLaneInstalls map[string]string (absent from localKeys,
                 on the TrustedLaneCommands precedent). laneInstallConsentLine with its
                 OWN NUL frame constant, LaneInstallConsented / GrantLaneInstallConsent /
                 RevokeLaneInstallConsent, `dross trust --lane-install <name>` printing the
                 line before it writes, and laneInstallRefusal's absent/stale ladder.
                 `test lane remove` drops the install grant alongside the command grant.
       contract: - adding an install line to a lane that already runs green leaves
                   LaneConsented at ConsentGranted; fold Install into laneConsentLine and
                   TestInstallLineDoesNotStaleTheTestGrant fails (the exact regression
                   install_consent is locked against)
                 - a granted install line whose text then changes reports ConsentStale, not
                   ConsentAbsent — TestInstallGrantGoesStaleNotAbsent
                 - a command whose bytes spell the install frame cannot forge an install
                   grant: TestInstallFrameIsDisjoint feeds the framed bytes back as a bare
                   line and asserts the fingerprints differ
                 - an install line containing a NUL yields an empty consent line and is
                   refused as un-grantable, mirroring laneConsentLine —
                   TestInstallLineRejectsNUL
                 - `dross local set trusted_lane_installs …` is refused;
                   TestInstallGrantAbsentFromLocalKeys fails if the key joins the generic
                   writer
                 - `dross test lane remove go` drops BOTH grants —
                   TestRemoveDropsInstallGrant fails if an install grant survives to
                   authorize a later lane re-added under the same name
```

Wave 3 (depends t-2, t-3)

```
  t-5  Add `dross test lane install <name> [--apply]`
       files:    internal/cmd/test_lane_install.go       (new),
                 internal/cmd/test_lane.go,
                 internal/cmd/test_lane_install_test.go  (new)
       covers:   c-5, c-6, c-7, c-8
       depends:  t-1, t-2, t-3
       what:     The lane-scoped verb, owning BOTH sides. One probe of the lane's
                 toolchain decides the side: gap on the granted host → install there; host
                 fine (or no grant / --local) and this machine missing it → install here.
                 Dry-run by default; --apply is the only path that reaches a seam, and it
                 refuses an ungranted DECLARED line before any I/O. Every line says which
                 machine it acted on.
       contract: - a lane whose tool the granted host lacks installs REMOTELY and prints
                   the host's name; TestInstallActsOnTheHostWithTheGap asserts remoteExecFn
                   saw the call and spawnLocal saw none, and the inverse case asserts the
                   mirror
                 - a transport failure during the probe installs on NEITHER side and exits
                   non-zero naming the host; TestInstallRefusesOnUnreachableHost fails if a
                   local install fires on the strength of an unanswered probe
                 - `dross test lane install go` with no --apply calls no seam at all and
                   prints the argv it would run; TestInstallDryRunTouchesNothing asserts
                   zero exec calls (c-5's "only an explicit apply")
                 - `--apply` on a lane with an UNGRANTED declared install line refuses
                   before the probe, printing the line and `dross trust --lane-install
                   <name>`; TestUngrantedInstallLineIsRefusedNotRun fails if the seam is
                   reached
                 - a table recipe needs NO grant (it is dross's own argv, exactly as
                   bootstrap's gremlins pin is) — TestTableRecipeNeedsNoInstallGrant
                 - a refusal (runtime) exits with the refusal wording and is NOT counted or
                   worded as a failed attempt; TestRefusalIsNotAFailedInstall asserts the
                   two produce different text and different counters (c-8)
                 - a lane both machines already satisfy prints "nothing to install" and
                   exits 0 — TestNothingToInstallIsGreen

  t-6  Plan lane toolchains inside `dross remote bootstrap`
       files:    internal/cmd/remote_bootstrap.go,
                 internal/cmd/remote_bootstrap_cmd.go,
                 internal/cmd/remote_bootstrap_lane_test.go  (new)
       covers:   c-2, c-3, c-8
       depends:  t-2
       what:     planRemoteBootstrap probes the UNION of remoteMutationTools and
                 laneToolUnion(p.Runtime.TestLane) in the one existing round trip, and
                 plans a step per lane tool through t-2's resolver. reportBootstrap gains
                 the Unavailable bucket, which prints and does NOT count toward the exit.
                 A step says which lane (or adapter) wants the tool.
       contract: - a repo with a `pnpm` lane and a gremlins adapter yields ONE probe
                   containing both tool sets; TestBootstrapProbesOnce asserts remoteProbeFn
                   was called exactly once and its argument is the union — a second probe
                   is a second answer that can differ from the first
                 - `go`, wanted by both a Go lane and gremlins' recipe, appears as ONE step;
                   TestBootstrapDedupesSharedTool fails on a duplicate line
                 - a declared lane with no recipe and no install line prints the
                   "no install line" report and bootstrap still exits 0;
                   TestUnavailableDoesNotFailBootstrap is the locked undeclared_exit rule,
                   and its absence makes every lane repo start exiting 1
                 - a lane tool dross genuinely cannot install (a runtime) exits 1 as a
                   refusal, counted in `refused` and never in `failed` —
                   TestLaneRuntimeRefusalCounts
                 - bootstrap and `dross doctor`'s Remote section derive their tool list
                   from the SAME remoteProbeTools; TestBootstrapAndDoctorAgreeOnTools fails
                   if either grows a private derivation (F7 — doctor passing on a host the
                   run then falls back from)
```

Wave 4 (depends t-5, t-6)

```
  t-7  Prove an install leaves no residue
       files:    internal/cmd/lane_install_residue_test.go  (new)
       covers:   c-9
       depends:  t-5, t-6
       what:     The c-9 invariant, asserted across both surfaces rather than assumed:
                 an applied install writes nothing anywhere, and the next run routes the
                 lane remotely on the probe's answer alone.
       contract: - `.dross/local.toml` and `project.toml` are byte-identical before and
                   after an applied `dross test lane install --apply` and an applied
                   `dross remote bootstrap --apply`; TestInstallWritesNoState fails the
                   moment either surface caches an "installed" flag
                 - with the probe now reporting the tool PRESENT, laneLocality returns
                   siteRemote for that lane with no grant read and no user gesture;
                   TestInstalledLaneGoesRemoteNextRun is the "no user action" half of c-9
                 - an install grant recorded for a DECLARED line is not consulted by the
                   locality decision at all; TestLocalityIgnoresInstallGrant fails if
                   routing starts depending on consent state
```

---

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 fallback names the lane-scoped remedy | t-4 |
| c-2 bootstrap plans lanes + adapters, one vocabulary | t-6 |
| c-3 never guess a recipe | t-2, t-6 (report), t-1 (declaration shape) |
| c-4 line overrides table, table serves undeclared lanes | t-1, t-2 |
| c-5 dry-run default, no install from a fallback | t-4, t-5 |
| c-6 installs on the side with the gap, and says which | t-5 |
| c-7 install line's own consent fingerprint | t-3, t-5 (enforcement) |
| c-8 refusal ≠ failed attempt | t-2, t-5, t-6 |
| c-9 no sticky state | t-7 |

Every criterion has an owner; no criterion is owned only by a test-name.

## Judgment calls

- **One recipe table, not two.** t-2 generalizes `bootstrapRecipes` rather than
  adding a lane-side table beside it. Rejected: a separate `laneInstallRecipes`,
  which reads cleaner and drifts — c-2 demands one vocabulary, and two tables is
  the mechanism by which two surfaces start disagreeing about the same host.
- **A fourth arm (`Unavailable`), not a Refusal with a softer message.**
  undeclared_exit is locked on the exit behaviour, and encoding it as message
  text means the exit rule lives in a string comparison. A distinct arm makes
  "does this count toward exit 1" a type question.
- **The table ships Go-package entries only.** `pnpm`/`yarn` via `npm install
  -g` need a writable global prefix — usually root — on a machine dross was
  merely lent, which is the same objection install_scope is locked on. They are
  refusals with the lane's own `install` line as the escape hatch, exactly as
  the lock intends. Rejected: shipping `npm install -g` entries because they
  look package-class.
- **A built-in recipe needs no install grant; a declared line does.** c-7 binds
  consent to *the lane's install line* — a repo-supplied string, the same threat
  class as `command` and `prepare`. A table argv is dross's own, like bootstrap's
  pinned `go install gremlins`, which has never needed a grant. Rejected:
  granting everything, which would make `dross remote bootstrap` start refusing
  on repos where it works today.
- **A transport-failed probe installs on neither side.** Rejected: falling back
  to a local install, on the grounds that the lane would have run locally anyway.
  A host that was never reached told us nothing, and lane_locality already refuses
  to name a host it did not hear from; an install is a larger act than a routing
  decision and deserves the stricter reading.
- **Dry-run does not require a grant; `--apply` does.** The dry run is how the
  user reads the line before consenting — refusing to *print* it would leave
  `dross trust --lane-install` as the only way to see what they are approving,
  which inverts the ceremony.
- **The offer line lives in `laneFallbackLine`, wave 1, ahead of the verb it
  names.** Rejected: sequencing it after t-5 so it can never advertise a
  non-existent command. The string is pure, its contract is testable in
  isolation, and holding it back would serialize the one task that owns the
  "a fallback installs nothing" invariant behind the task that introduces the
  install seam.
- **c-9 gets a task, not a footnote.** A "we simply don't write anything"
  invariant has no defender unless something fails when it is violated; the
  residue test is cheap and is the only thing standing between this feature and
  a future "cache the probe result" optimization.
