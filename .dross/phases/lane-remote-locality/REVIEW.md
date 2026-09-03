# Plan Review — lane-remote-locality

Reviewed: 2026-08-28
Plan: 6 tasks across 4 waves

## BLOCKING
(none)

Coverage is complete: c-1 (t-3), c-2 (t-2, t-3), c-3 (t-3), c-4 (t-2, t-3), c-5 (t-2),
c-6 (t-3), c-7 (t-1, t-4), c-8 (t-5), c-9 (t-2, t-3). No task contradicts
toolchain_source, local_absence or prepare_toolchain — each locked decision has at least
one contract item defending it. No task implies a rules.toml r-01 violation; runtime.mode
is native/go and nothing in the plan reaches for a foreign toolchain.

## FLAG
- [antipatterns] t-1's field as described carries a toml tag only:
  `Toolchain []string` (toml `toolchain,omitempty`). `internal/cmd/json_tag_parity_test.go`
  (TestTomlFieldsCarryMatchingJSONTags) walks `project.Project` transitively and fails any
  toml-tagged field whose json tag is absent or differs, omitempty included. As written,
  t-1 lands red on an existing test the plan does not mention.
  Suggestion: `toml:"toolchain,omitempty" json:"toolchain,omitempty"`, and say so in the
  description so the executing agent does not discover it from a failure.

- [wave-order] t-5 `depends_on = ["t-1", "t-2"]` is wrong in both directions.
  It does not need t-2: doctor uses `testlane.Toolchain` (t-1) and the existing
  `remoteProbeFn` seam — not `laneLocality`, `exitToolchainMissing` or the changed
  `resolveTestTarget`. It does need t-3: contract item 5 drives one probe fixture through
  "both doctor and a `dross test --files` run" and asserts the binary doctor names is the
  binary "the run's fallback line names" — the run emits no per-lane fallback line until
  t-3 wires `laneLocality` into `runTestLanes`. Run t-5 in parallel with t-3 as declared
  and that item is unsatisfiable.
  Suggestion: either add t-3 to t-5's depends_on (keeping it in wave 3, after t-3), or move
  the cross-check item to t-3 and drop t-5 to wave 2 alongside t-4.

- [granularity] t-2 is a split candidate: 4 files, 14 contract items, three separable
  concerns — (a) the exit-code taxonomy (`exitToolchainMissing`, `exitRank`, `worseOutcome`,
  `toolchainFailure`), (b) the `resolveTestTarget` signature change threading
  `remote.Readiness` back out, (c) the pure `laneLocality` decision function. (a) is
  independently testable against `test_lane_consent_test.go`'s existing precedence test and
  has no dependency on (b) or (c).
  Suggestion: lift (a) into its own wave-1 task parallel with t-1; t-2 then carries 10 items
  over the two concerns that genuinely travel together.

- [locked-decisions] t-2 describes `toolchainFailure(lane, tool, host)` — a single host —
  but its own contract item 4 requires the message to name "the tool AND both hosts", and
  locked `local_absence` requires the wording "neither host has <tool>". The described
  signature cannot produce the message the contract asserts.
  Suggestion: give the constructor both hosts (or the remote host plus a local-absent
  flag), and reconcile the description with contract item 4 before executing.

- [coverage] c-5's second half — "the transport fallback ... sends the whole run local" —
  is asserted only on `resolveTestTarget`'s return value (t-2 item 8: Fallback set, Why
  naming the host, zero toolchain lines). t-3 is where it can actually regress: t-3
  introduces "sync only when at least one lane stays remote", and after a transport
  fallback `target` is nil with every lane local — a skip rule keyed on the wrong condition
  would change the existing wording, the sync count, or emit toolchain lines for a host
  that was never reached. t-3 has no contract item for "unreachable host, lanes declared".
  Suggestion: add one to t-3 — probe returns `remote.ErrTransport`, assert the existing
  preflight line is byte-identical, every lane spawns locally, and no toolchain line prints.

- [coverage] c-8 says doctor and the run "never disagree about what the host has", but they
  read different grants. Doctor uses `readRemoteGrant` (internal/cmd/local.go:404 —
  `effectiveRemote()` only); the run uses `readRemoteGrants` (local.go:368 — scalar grant
  first, then `remote_pool`) and `selectRemoteTarget` takes the first candidate that
  answers. With a pool declared and the scalar host down, doctor reports host A's toolchain
  while the run executes on host B. t-5's cross-check item drives a single fixture through
  both and would not see it. This divergence pre-dates the phase (it already applies to
  adapter tools); flagging because c-8 promotes it to a criterion this phase must satisfy.
  Suggestion: either narrow c-8 to the effective grant explicitly, or add a t-5 contract
  item over a two-host pool.

- [antipatterns] t-4 makes `lane edit` accept `--toolchain`, which falsifies the command's
  own help text: `internal/cmd/test_lane.go:223` states "Only prepare is editable. Changing
  a lane's match, command, selector or empty_exit is still remove-then-re-add." t-4's
  contract pins the widened *refusal* (item 8, names both flags) but nothing pins the
  Long/Short text, and `TestOptionsDocumentsTheSelectorSurfaceCorrectly` only checks the
  docs→registered-flags direction — a doc naming a flag that does not exist. A stale
  "only prepare is editable" survives every test in this plan.
  Suggestion: add a t-4 contract item asserting `lane edit`'s Long names --toolchain.
  Separately: this is the second narrowing of lane-selector-translation's locked
  `lane_edit_surface`, and spec.toml records no decision for it. Not a conflict with this
  spec's own locks, so not blocking — but the widening is currently undocumented.

## NOTE
- [coverage] t-6 is the only task with `covers = []`. Correct — no criterion names README or
  the prompts — but it means verify has nothing to map it to, while exit 8's documentation
  in execute.md is load-bearing for the agent contract. Worth remembering at verify time
  rather than reading the empty list as dead weight.

- [test-contract] t-2 item 2 says "a table-driven test over all six pairs" of
  transport > partial > prepare > red > refused > nothing-measured. Six ranks give 15
  ordered pairs, not six; the existing test being extended is
  `TestExitPrecedenceIsTotal` (internal/cmd/test_lane_consent_test.go:244). Precision only —
  the intent is clear.

- [antipatterns] t-2 lists `internal/cmd/test_lane_consent_test.go` among its files without
  the description saying why. It is evidently where the existing precedence test lives; the
  file list is right, the description just does not account for it.

- [test-contract] `remote.Probe` (internal/remote/remote.go:637) issues one ssh `Exec` per
  tool after the core-count call, so c-4's "one pass" is one probe *call*, not one round
  trip. The plan already treats dedupe as load-bearing (t-2 items 3 and 6), which is the
  right read — the union's size is wall-clock cost, not just tidiness.

- [forbidden-actions] `/Users/rivil/.claude/dross/rules.toml` does not exist; only project
  r-01 applies. t-6 is the only task editing `assets/`, and it names `make install`
  explicitly. Correct handling.

- [strengths] The contracts are written as failure modes against named surfaces, not as
  "tests pass" — and several are assertions that a weaker test would let through:
  index-based ordering ("the probe call is recorded strictly BEFORE the first rsync argv by
  INDEX ... asserting presence alone would still pass"), byte-identical file comparison for
  c-6's non-stickiness, and `ExitCode is 8 rather than 1`. This is the standard the check
  exists to enforce and the plan clears it throughout.

- [strengths] c-4's "never pays for the transfer" gets both directions: zero rsync when
  every lane falls back, AND exactly one rsync when only one lane falls back. The
  counter-test is what makes the first one a gate rather than an invitation to skip the
  sync unconditionally. Same discipline in t-5 (issue count unchanged by lane rows, but
  still incremented by an adapter's missing tool).

- [strengths] The design rides the existing preflight probe (`selectRemoteTarget` →
  `preflightRemote` → `remoteProbeFn`, currently called with nil tools) instead of adding a
  second ssh, and asserts single-call at the seam. That is the cheapest correct place for
  this feature and it also keeps doctor and the run on one question — the mechanism c-8
  depends on.

## Summary
No blocking defects — coverage is complete and the locked decisions are respected — but t-1
lands red on the existing json-tag parity test as described, t-5's dependencies are wrong in
both directions, and c-5's whole-run-local half and c-8's pool case are each asserted one
layer below where they can actually break.
