# Panel draft — MVP lens

Phase remote-toolchain-install — 6 tasks across 3 waves

## Wave 1

```
  t-1  Add lane install field and declaration surface
       files:    internal/project/project.go
                 internal/cmd/validate.go
                 internal/cmd/test_lane.go
                 internal/cmd/validate_lane_test.go
                 internal/cmd/test_lane_install_field_test.go
       covers:   c-4, c-7
       description:
                 Adds `Install string` to project.TestLane (toml `install,omitempty`).
                 laneProblems gains the whitespace-only-install rule that Prepare
                 already has; `dross test lane add --install`, `lane edit --install`
                 (set, and `--install ""` to clear via the existing Changed guard),
                 and `lane list` printing it when declared.
       contract: - `laneConsentLine` is NOT touched: a test declares a lane, records
                   Fingerprint(laneConsentLine(lane)), sets `install`, and asserts the
                   fingerprint is byte-identical. Folding Install into the consent
                   line fails it (this is c-7's non-staleness half).
                 - `dross test lane edit go --install ""` writes a project.toml with
                   no `install` key at all — a round-trip test asserts the key is
                   absent, not present-and-empty.
                 - `validate` on a hand-written `install = "   "` emits a problem
                   naming the lane; the same lane with `install = "go install x"`
                   emits none.
                 - `dross test lane add --install` on a lane that also passes
                   --toolchain still refuses through laneToolchainRefusal before any
                   save (project.toml unchanged on the refusal path).

  t-2  Name the install remedy in the fallback line
       files:    internal/cmd/lane_locality.go
                 internal/cmd/lane_locality_test.go
       covers:   c-1, c-5
       description:
                 laneFallbackLine gains a second line: the exact
                 `dross test lane install <name>` invocation for the lane that fell
                 back. laneVerdict.Announce carries both. Nothing installs.
       contract: - A lane that falls back for a missing tool produces an Announce
                   containing the literal string `dross test lane install <lane>`
                   with that lane's own name — a whole-host `dross remote bootstrap`
                   in the offer fails the assertion (locked offer_scope).
                 - A lane that goes remote, and a lane in a run with no host at all,
                   still produce an EMPTY Announce — the offer never appears where
                   there was no fallback.
                 - The fallback path calls neither remoteExecFn nor the local install
                   seam: a test that fails the seams on call asserts a fallback run
                   never installs (c-5).
```

## Wave 2 (depends t-1)

```
  t-3  Add lane install recipe table and plan vocabulary
       files:    internal/cmd/remote_bootstrap.go
                 internal/cmd/remote_bootstrap_test.go
       covers:   c-3, c-4, c-8
       depends:  t-1
       description:
                 Adds `laneInstallRecipes` — package-class lane tools only — plus
                 runtime refusal entries for go/node/npx/dotnet. Adds
                 `planLaneInstall(lane, missing)` returning bootstrapStep values, and
                 a fourth arm on bootstrapStep (`Undeclared string`) for a lane tool
                 with neither table entry nor install line. bootstrapStep gains the
                 lane name so a step can say who wanted the tool.
       contract: - A lane whose `install` line is set plans that line's argv even when
                   laneInstallRecipes has an entry for the same tool — the test
                   asserts the planned argv is the lane's, not the table's (c-4
                   override half).
                 - A lane with no `install` line whose tool IS in the table plans the
                   table's argv (c-4 already-declared-lanes half).
                 - A lane needing `node` plans a Refusal naming node and never an
                   argv, whatever the table grows to hold (locked
                   install_scope_for_lanes).
                 - A lane tool in neither the table nor the lane's line plans a step
                   with Undeclared set and Argv/Refusal both empty — asserted as
                   three separate field checks, because the whole point is that it is
                   not a refusal.

  t-4  Grant lane installs separately from test grants
       files:    internal/cmd/local.go
                 internal/cmd/trust.go
                 internal/cmd/test_lane.go
                 internal/cmd/trust_lane_install_test.go
       covers:   c-7
       depends:  t-1
       description:
                 Adds `TrustedLaneInstalls map[string]string` to localStore (absent
                 from localKeys, like every other grant), `laneInstallConsentLine`
                 in its own NUL-framed namespace, LaneInstallConsented /
                 GrantLaneInstallConsent / RevokeLaneInstallConsent,
                 `dross trust --lane-install <name>` (with --check), and
                 laneInstallConsentRefusal. `lane remove` drops both grants.
       contract: - `dross trust --lane-install go` writes trusted_lane_installs[go]
                   and leaves trusted_lane_commands[go] byte-identical; `dross trust
                   --lane go` does the mirror. Asserted as two independent key reads,
                   so a shared store fails it.
                 - An install line trusted, then edited by one byte, makes
                   LaneInstallConsented return ConsentStale (not ConsentAbsent), and
                   the refusal text says CHANGED.
                 - A lane whose install line is spelled exactly like another lane's
                   *command* does not inherit that lane's command grant — the two
                   fingerprint namespaces are disjoint.
                 - `dross test lane remove go` leaves neither trusted_lane_commands
                   nor trusted_lane_installs holding a `go` key.
                 - `dross local set trusted_lane_installs …` is refused as an unknown
                   key.
```

## Wave 3 (depends t-3, t-4)

```
  t-5  Add `dross test lane install <name> [--apply]`
       files:    internal/cmd/test_lane_install.go        (new)
                 internal/cmd/test_lane.go
                 internal/cmd/test_lane_install_test.go
       covers:   c-5, c-6, c-8, c-9
       depends:  t-3, t-4
       description:
                 New verb under `dross test lane`. Probes the granted host through
                 remoteProbeFn for the lane's toolchain, resolves the local half
                 through laneLookPath, plans through planLaneInstall, and runs the
                 argv on the side with the gap — remoteExecFn for the host,
                 a new localInstallFn seam for this machine. Dry-run by default;
                 --apply installs. Refuses an ungranted lane-declared install line
                 through LaneInstallConsented.
       contract: - With the host missing the tool and this machine having it, --apply
                   calls remoteExecFn once and localInstallFn zero times, and the
                   output names the host; with the gap on this machine the counts and
                   the named machine invert (c-6, asserted on both seams' call
                   counts, not on prose).
                 - Without --apply, both seams are called ZERO times and the output
                   still prints the argv it would have run (c-5).
                 - A lane whose install came from the built-in table installs with no
                   grant; the same lane with its own `install` line and no grant is
                   refused, and remoteExecFn is never called (c-7 + c-8).
                 - A tool with no recipe and no install line prints the "add one or
                   install <tool> by hand" report and the command exits 0; a declared
                   install whose exec returns non-zero exits 1 (locked
                   undeclared_exit — two cases, two exit codes, one test each).
                 - After a successful --apply, .dross/local.toml and project.toml are
                   byte-identical to before the run (c-9, asserted on file bytes).
                 - `dross test lane install nope` lists the declared lane names
                   (shared findLane), and does not probe.

  t-6  Plan lane toolchains in `dross remote bootstrap`
       files:    internal/cmd/remote_bootstrap.go
                 internal/cmd/remote_bootstrap_cmd.go
                 internal/cmd/remote_bootstrap_cmd_test.go
       covers:   c-2, c-5, c-8
       depends:  t-3
       description:
                 planRemoteBootstrap probes the union of remoteMutationTools and
                 laneToolUnion in the SAME single probe (the remoteProbeTools shape
                 doctor already uses) and emits lane steps beside adapter steps.
                 reportBootstrap renders the Undeclared arm and keeps it out of the
                 refusal count; an ungranted lane install line is a refusal.
       contract: - A repo with one adapter and two lanes issues exactly ONE
                   remoteProbeFn call, whose tool list contains both the adapter tool
                   and both lanes' tools, deduplicated (c-2 — asserted on the
                   recorded probe argument, and on the call count being 1).
                 - Both step kinds render through the same already-installed /
                   would-install / refused vocabulary: a lane step and an adapter
                   step for a present tool produce the same `✓ … already installed`
                   shape.
                 - A lane with an Undeclared tool leaves reportBootstrap's exit at 0
                   while a lane whose declared install exec fails exits 1 (locked
                   undeclared_exit, at the report layer this time).
                 - A lane whose own `install` line is ungranted is reported as
                   refused and its argv is never handed to remoteExecFn — bootstrap
                   is not a way around the lane install gate (c-7 at this surface).
                 - Without --apply no lane step reaches remoteExecFn (c-5).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 fallback names the remedy | t-2 |
| c-2 bootstrap plans lanes + adapters, one probe | t-6 |
| c-3 never guess a recipe | t-3 |
| c-4 lane line overrides, table still works | t-1, t-3 |
| c-5 dry-run default, no install as a side effect | t-2, t-5, t-6 |
| c-6 installs on the side with the gap, says which | t-5 |
| c-7 install grant separate from test grant | t-1, t-4 |
| c-8 refusal by name, never a failed attempt | t-3, t-5, t-6 |
| c-9 no sticky state after an install | t-5 |

9/9 criteria covered.

## Judgment calls

- **The `install` field carries its own CLI flags in t-1 rather than being hand-edited.** Rejected: schema + validate only. c-7's scenario is literally "adding an install line to a lane that already runs green", and `lane edit --install` is that gesture; a hand-edited field would also be the only lane field with no CLI writer, against test_lane.go's stated reason for existing.
- **c-1's offer (t-2) is wave 1, not bundled into the install verb (t-5).** Rejected: merging them for wording consistency. The invocation is fixed by the locked `install_surface` decision, not by t-5's implementation, so the offer has no real dependency — and merging would have pushed t-5 to five files across two layers.
- **A built-in recipe's argv needs no consent grant; a lane's own `install` line does.** Rejected: gating both. The table is a pinned constant in dross's own source, like `gremlinsPin`; the lane line arrives in tracked project.toml, which is the exact threat the consent store exists for. Gating the table would re-prompt for something the repo never proposed.
- **Extend `bootstrapStep` with a fourth `Undeclared` arm rather than introducing a parallel lane-step type.** Rejected: a separate `laneStep` struct. c-2 requires one vocabulary across both surfaces; two structs would be two renderers, and they would drift the way doctor and the run were kept from drifting.
- **Bootstrap refuses an ungranted lane install line (t-6) instead of skipping it.** Rejected: letting bootstrap install lane lines ungranted because it is a host-admin verb. A whole-host verb that ran repo-supplied lines without a grant would be the way around the gate t-4 builds.
- **The Undeclared case does not count toward the non-zero exit at either surface.** This is the locked `undeclared_exit` decision, restated as a contract in both t-3 (plan layer) and t-6 (report layer) because they are the two places it can be got wrong independently.
- **t-5 gets a new `localInstallFn` seam rather than reusing `spawnLocal`.** Rejected: shell-lining the argv through spawnLocal. The recipes are argv, and `install_transport` is locked on argv-through-the-seam; re-joining to a shell line here would put quoting back in exactly the place bootstrap removed it from.
