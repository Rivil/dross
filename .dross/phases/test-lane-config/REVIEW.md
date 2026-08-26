# Plan Review — test-lane-config

Reviewed: 2026-08-25
Plan: 8 tasks across 4 waves

## BLOCKING

(none)

Coverage is complete — c-1 t-1, c-2 t-2/t-5, c-3 t-6, c-4 t-6, c-5 t-5, c-6 t-4/t-6/t-8,
c-7 t-7, c-8 t-5, c-9 t-3/t-8. No task description, file list or contract contradicts a
locked decision; lane_input, unmatched_files, bare_test_run, multi_lane and lane_consent
each have a contract line that would fail if the decision were violated. No rules.toml
violation — r-01 is explicitly answered by t-7's guard reading `assets/prompts/execute.md`
directly rather than the installed symlink.

## FLAG

- [missed-files] No task touches `internal/cmd/doctor.go`. Doctor's own comment at
  doctor.go:1200-1210 states the reason that section exists: "the gate refuses at the
  moment of use, but nothing tells the user what state they are in until something has
  already refused. Doctor is where that becomes visible before it bites." The plan adds an
  entire new class of grants (per-lane fingerprints, t-4) that doctor will never report, so
  the first signal a lane is ungranted or stale is t-6's exit 6 mid-gate — exactly the state
  the Exec consent section was written to prevent.
  Suggestion: add doctor.go to t-4's files (or a task in wave 3), reporting each declared
  lane as granted / absent / stale on the existing severity split. t-4 already builds
  `--lane <name> --check`, so the state machine is there.

- [missed-files] Same file, second defect: doctor's `ConsentNotApplicable` branch prints
  "no runtime.test_command is configured, so the loop commands refuse." t-6 deliberately
  makes that false — a lanes-only repo with an empty `test_command` runs its granted lanes.
  After t-6 lands, doctor tells the user their loop is broken in exactly the repo shape
  lanes are most wanted for.
  Suggestion: whoever owns the doctor change makes that branch lane-aware; if doctor is
  left out of scope, record it as a known-wrong message rather than leaving it silent.

- [wave-order] t-8 is in wave 4 with `depends_on = ["t-3", "t-4"]` — both wave 2. It needs
  nothing from t-5, t-6 or t-7; it documents the lane verbs and the lane grant, both of
  which exist at the end of wave 2.
  Suggestion: drop t-8 to wave 3. It then runs alongside t-6 instead of behind it.

- [granularity] t-6 is a split candidate. Two files, but eleven contract items spanning four
  separable concerns: the matched-lane spawn loop, the per-lane grant gate replacing
  `requireExecConsent`, the exit-code precedence lattice, and a refactor of
  `runTestRemotely` into separable sync/ssh halves. The remote refactor is the odd one out —
  it is testable on its own (`TestMultiLaneSyncsOnce`), it touches the one function in
  test.go with the most delicate ordering property (sync-before-run, documented at
  test.go:279-284), and a failure there is a transport bug, not a consent bug.
  Suggestion: split the `runTestRemotely` decomposition into its own wave-3 task that t-6
  depends on, so a bad refactor is bisectable separately from the consent gate.

- [false-green] t-5 leaves one commit in which `dross test --files internal/a.go docs/x.md`
  exits 0 in a lane-declaring repo having spawned nothing — its own contract asserts that
  ("reports docs/x.md as unmatched and exits 0 having resolved the go lane"). That is the
  shape c-8 exists to forbid, present for the length of one commit. The plan's rationale for
  the split is sound (t-5 must not execute an unfingerprinted line), so this is a tension,
  not an error — but a bisect landing on t-5 in a lane repo reads green on a run that
  measured nothing.
  Suggestion: have t-5's resolve-only output say so in words ("resolved N lanes; nothing
  run") and add that string to the contract, so the intermediate state is self-describing.
  Alternatively note in the task that t-5 and t-6 are not a valid stopping point.

- [test-contract] t-5's out-of-tree refusal has no exit code and no mixed-set behaviour.
  `TestAbsolutePathRefusalSaysOutOfTree` pins the message wording but not the status, while
  the sibling unmatched refusal is pinned to exactly 5 and cross-checked against 1/3/4. And
  nothing says what `--files internal/a.go /abs/x.go` does: refuse the whole run, or resolve
  the go lane and name the out-of-tree path the way a partial miss is named.
  Suggestion: pin both — a code for the out-of-tree refusal, and one contract line for the
  mixed set. t-2 deliberately made out-of-tree its own category; the decision that category
  forces has to be stated somewhere, and t-5 is the only task that can state it.

- [test-contract] Exit precedence is pinned pairwise, not as a lattice. t-6 fixes 1 > 6 and
  says transport wins over 1, but 3 vs 6, 5 vs 6, and 4 vs anything are unstated across a
  multi-lane run where several can co-occur.
  Suggestion: one contract line naming the full order (transport > red > partial > refused >
  nothing-measured, or whatever the intended order is). With five codes and per-lane
  outcomes, "worst outcome wins" is not self-evident from the pairwise cases.

- [missed-files] Grant lifecycle on removal is unowned. `dross test lane remove docs` (t-3)
  deletes the lane from project.toml; nothing deletes `trusted_lane_commands["docs"]` from
  local.toml. t-4 names the orphan explicitly for the RENAME case ("renaming a lane leaves
  its old grant orphaned") but neither task owns removal. The security consequence is small
  — a re-added lane with a changed command still reads stale — but a re-added lane with an
  identical command silently inherits a grant the user may have made months ago.
  Suggestion: decide it deliberately in t-3 or t-4 (drop the grant on remove, or keep it and
  say why), and pin whichever with a contract line.

- [same-file] t-3 and t-5 both edit `internal/cmd/test.go` in wave 2 with no dependency
  between them, so execution order is unconstrained. The plan handles this well — t-3's
  description explicitly cedes everything but the `c.AddCommand(testLane())` line — but a
  reader who runs the wave in a different order has nothing enforcing that boundary.
  Suggestion: add `depends_on = ["t-3"]` to t-5 (cheap, and t-5 is already the wave's
  heaviest task) or leave it and accept the ownership comment as the contract.

## NOTE

- [strengths] The test contracts are the strongest part of this plan. Nearly every line
  names a test AND the failure it catches ("fails if ** collapses to one segment", "fails if
  the two messages collapse, which would send a caller to fix their lane config over an argv
  problem"). Several encode a locked decision directly — t-4's first line is the
  lane_consent rationale expressed as an assertion. That is the standard, not the exception.

- [strengths] The t-5 / t-6 split is a real security property, not granularity inflation:
  t-5 resolves without ever spawning, so no commit in this phase executes a command line out
  of project.toml before the per-lane fingerprint check exists. The description says exactly
  that, which is why the false-green FLAG above is a tension worth stating rather than a
  mistake worth fixing blind.

- [strengths] Routing lane spawns through `shArgvFor("runtime.test_lane[<name>]", line)`
  rather than reusing `shArgv`'s hardcoded `runtime.test_command` label (test.go:129-136) is
  the kind of detail that normally gets found in review, not in the plan. Same for
  StringArray over StringSlice on `--files` because a comma is legal in a path.

- [self-hosting] This phase's own gate is safe to change mid-phase: dross's project.toml
  declares no `[[runtime.test_lane]]`, so t-5's byte-identity guarantee means t-7's rewritten
  execute.md gate behaves exactly as the current one for t-8. Worth not adding a lane to
  dross's own project.toml until the phase ships.

- [scope] c-7 names only execute.md, and t-7 respects that. `quick.md` and `verify.md` also
  call `dross test` and keep whole-repo semantics — correct per the locked bare_test_run
  decision, and no action here, but it means the lane gate is execute-only until someone
  scopes the rest.

## Summary
No blocking defects — coverage is complete and the contracts are unusually specific; the
real gaps are the unowned `doctor.go` surface (which the codebase's own comments argue is
where consent state must become visible), an under-pinned exit-code and out-of-tree
lattice, and t-8 sitting a wave later than its dependencies require.
