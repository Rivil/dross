Phase remote-toolchain-install — 6 tasks across 3 waves

Wave 1
  t-1  Declare the lane install line
       files:    internal/project/project.go, internal/cmd/test_lane.go,
                 internal/cmd/validate.go, internal/cmd/validate_lane_test.go,
                 internal/cmd/test_lane_edit_test.go
       covers:   c-4, c-7
       desc:     Adds `Install string` to project.TestLane, `laneInstallProblems`
                 to validate, and --install on `test lane add` / `test lane edit`
                 plus an `install:` row in `test lane list`.
       contract: - `install = "   "` makes `dross validate` report a
                   whitespace-only install (the prepare rule's sibling); a lane
                   with no `install` key adds ZERO problems, so a validator that
                   nagged every pre-existing lane fails.
                 - `dross test lane edit go --install "npm i -g pnpm"` leaves
                   laneConsentLine(lane) byte-identical before and after, and a
                   lane granted before the edit still reads ConsentGranted —
                   c-7's non-staleness proved at the cheapest layer.
                 - `--install ""` on edit clears the key and omitempty leaves it
                   out of project.toml; `--install` beside no other flag is not
                   the "nothing to change" refusal.
       depends:  —

  t-2  Add install recipe table and resolver
       files:    internal/cmd/lane_install.go (new),
                 internal/cmd/lane_install_test.go (new), ARCHITECTURE.md
       covers:   c-3, c-4, c-8
       desc:     One table of package-class lane tools plus explicit runtime
                 refusal rows; `resolveInstall(tool, declared string)` returns
                 exactly one of recipe-argv / declared-line / refusal / unknown.
                 Also declares the two install exec seams both surfaces run
                 through, and `laneInstallable` for c-1's offer.
       contract: - resolveInstall("go", "") returns a Refusal naming go as a
                   runtime with a nil Argv; deleting the runtime row turns it
                   into Unknown and this test fails (install_scope guard).
                 - resolveInstall("pnpm", "corepack enable pnpm") returns the
                   DECLARED line and the built-in pnpm argv appears nowhere in
                   the returned step — override, never append.
                 - resolveInstall("frobnicate", "") returns Unknown with an
                   EMPTY Refusal; a table-driven sweep asserts no key ever sets
                   both Unknown and Refusal.
                 - a structural sweep over every table row: each installable row
                   names a runtime, and no row's argv starts with apt / brew /
                   curl / dnf — a row that installed a runtime fails the audit.
       depends:  —

Wave 2 (depends t-1, t-2)
  t-3  Grant the install line its own consent
       files:    internal/cmd/local.go, internal/cmd/trust.go,
                 internal/cmd/lane_install.go,
                 internal/cmd/trust_lane_install_test.go (new)
       covers:   c-7, c-5, c-8
       desc:     TrustedLaneInstalls map on localStore; laneInstallLine,
                 LaneInstallConsented, GrantLaneInstallConsent,
                 RevokeLaneInstallConsent; `dross trust --lane-install <name>`
                 prints the line before writing; RevokeLaneConsent drops both
                 grants; the shared install helper refuses an ungranted declared
                 line before it reaches a seam.
       contract: - an ungranted `install` line: the helper returns a refusal and
                   NEITHER exec seam is called; after `dross trust
                   --lane-install <name>` the same call reaches the seam once.
                 - adding an install line to a lane already granted for testing
                   leaves LaneConsented at ConsentGranted while
                   LaneInstallConsented reports ConsentAbsent — one edit, two
                   independent answers.
                 - editing an existing install line moves LaneInstallConsented
                   to ConsentStale and leaves the lane's TEST grant untouched.
                 - `dross test lane remove <name>` leaves neither
                   trusted_lane_commands[name] nor trusted_lane_installs[name]
                   in local.toml; a re-added lane starts ungranted on both.
                 - a lane with NO install line whose tool has a built-in recipe
                   reaches the seam with an empty grant store — the table is not
                   gated.
       depends:  t-1, t-2

  t-4  Name the remedy in the fallback line
       files:    internal/cmd/lane_locality.go,
                 internal/cmd/lane_locality_test.go,
                 internal/cmd/lane_locality_wiring_test.go
       covers:   c-1, c-5
       desc:     laneFallbackLine gains the lane-scoped install offer, emitted
                 only when at least one absent tool is installable from the
                 table or from that lane's own install line.
       contract: - a lane falling back for a missing `pnpm` prints the fallback
                   and, on the next line, exactly `dross test lane install
                   <lane>` — its own name, never `dross remote bootstrap`
                   (offer_scope).
                 - a lane falling back for a tool with no recipe and no install
                   line prints the fallback with NO offer line.
                 - a lane that went remote, and a run with host == "", print
                   neither a fallback nor an offer.
                 - a full `dross test --files` run through a fallback leaves
                   both install seams' call counters at 0 — `dross test` never
                   installs as a side effect (c-5).
       depends:  t-1, t-2

Wave 3 (depends t-3, t-4)
  t-5  Cover lanes in `dross remote bootstrap`
       files:    internal/cmd/remote_bootstrap.go,
                 internal/cmd/remote_bootstrap_cmd.go,
                 internal/cmd/remote_bootstrap_test.go,
                 internal/cmd/remote_bootstrap_cmd_test.go
       covers:   c-2, c-3, c-8
       desc:     planRemoteBootstrap probes ONE union (mutation tools + every
                 declared lane's derived toolchain + both sets' runtimes) via
                 doctor's remoteProbeTools, returns steps tagged with the
                 adapter or the lane that needs them, and reportBootstrap counts
                 unknown apart from refused.
       contract: - exactly ONE probe per bootstrap, and its tool list is the
                   union of remoteMutationTools and laneToolUnion plus their
                   runtimes — a second probe call, or a union missing a declared
                   lane's tool, fails the recorder.
                 - a lane step with no recipe and no install line prints "no
                   install line" and does NOT increment the refusal counter: it
                   exits 0 when it is the only gap, while an unchanged mutation
                   refusal in the same run still exits 1.
                 - under --apply a lane's declared install reaches remoteExecFn
                   as `sh -c <line>` only when its install grant passes;
                   ungranted, the exec recorder stays EMPTY and the step reports
                   a refusal, never a failed attempt.
                 - a lane whose tool the host already has prints "already
                   installed" and plans no argv; a lane step renders `(lane
                   <name>)` and a mutation step `(<adapter>)`.
       depends:  t-3, t-4

  t-6  Add `dross test lane install <name>`
       files:    internal/cmd/test_lane_install.go (new),
                 internal/cmd/test_lane.go,
                 internal/cmd/test_lane_install_test.go (new), README.md
       covers:   c-5, c-6, c-9
       desc:     The lane-scoped install verb: resolves the lane, picks the side
                 with the gap from one probe (--local forces this machine),
                 dry-run by default, --apply installs. Owns the README rows for
                 the verb, `trust --lane-install`, and bootstrap's lane coverage.
       contract: - with a grant whose probe reports the lane's tool missing on
                   the host, `--apply` sends the install through remoteExecFn
                   and NEVER through the local spawn seam, and the header names
                   the host; with no grant (or --local), the reverse, and the
                   header says "this machine".
                 - without `--apply` neither seam is called and the output ends
                   with the re-run hint.
                 - after a successful `--apply`, .dross/local.toml AND
                   project.toml are byte-identical to their pre-run bytes; a
                   following laneLocality with a probe reporting the tool
                   present routes the lane to siteRemote with no user action
                   (c-9).
                 - a lane whose tool is present on the gap side exits 0 saying
                   nothing to install; an unknown lane name lists the declared
                   lanes rather than saying "unknown lane".
                 - the README row for `dross test lane install` names `--apply`,
                   the `dross trust` row names `--lane-install`, and the `dross
                   remote bootstrap` row says it covers declared lanes.
       depends:  t-3, t-4

## Coverage

| criterion | tasks |
|---|---|
| c-1 fallback names the remedy | t-4 |
| c-2 bootstrap plans lanes + adapters, one probe | t-5 |
| c-3 never guess a recipe | t-2, t-5 |
| c-4 lane line overrides, table still works | t-1, t-2 |
| c-5 dry-run default, test never installs | t-3, t-4, t-6 |
| c-6 installs on the side with the gap, says which | t-6 |
| c-7 separate install fingerprint | t-1, t-3 |
| c-8 refusal by name, never a failed attempt | t-2, t-3, t-5 |
| c-9 no sticky state after an install | t-6 |

9/9 criteria covered.

## Judgment calls

- A table RUNTIME refusal (node, dotnet, go) counts toward bootstrap's non-zero
  exit; "no recipe and no install line" does not. undeclared_exit names only the
  second case, and a runtime row carries an owner action (c-8) — which is what
  the existing npx refusal already exits on. Rejected: making every lane gap
  exit-neutral, which would silently soften the mutation half too.
- The c-1 offer prints the bare `dross test lane install <name>`, not
  `--apply`. Rejected: an applying command pasted straight out of a transcript.
  Every other dross offer prints the ceremony's first step, and the verb's own
  dry run ends with the --apply hint.
- The offer is SUPPRESSED when nothing in the gap is installable, rather than
  printed with a caveat. An offer whose answer is "no install line" teaches the
  reader to skim fallback lines, which is the one thing c-1 cannot afford.
- Consent gates the DECLARED line only; a built-in recipe install needs no
  grant. The table is dross's own code, like gremlinsPin — a prompt in front of
  it would put ceremony on exactly the case the table exists to make work
  without edits.
- RevokeLaneConsent drops BOTH grants rather than adding a second call at the
  remove site. Keeps the pair atomic, so no future caller can drop one and
  leave the other keyed under a dead lane name.
- One invocation acts on ONE machine, chosen by where the gap is, with --local
  as the override. Rejected: installing on both sides in one run — c-6 asks
  which machine it acted on, singular, and a two-sided install makes the answer
  a list.
- Bootstrap reuses doctor's remoteProbeTools union rather than re-deriving the
  lane half. Two derivations of "what does this host need" drift, and the drift
  shows up as doctor passing on a host bootstrap then reports as incomplete.
- ARCHITECTURE.md goes to t-2 (where the component is introduced) and every
  README edit to t-6, so no two tasks in one wave write the same doc.
- File counts include test files. The 5-file split rule is applied per LAYER:
  t-1 is schema + validate + CLI-flag on one field, which is one change with
  three call sites rather than three tasks.
