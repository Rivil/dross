# Plan Review — red-proof-repoint

Reviewed: 2026-08-16 (second pass, post-amendment)
Plan: 7 tasks across 4 waves

## BLOCKING

(none)

## FLAG

- [wave-order] t-6 has an undeclared same-wave dependency on t-5 — the exact class of edge the
  amendment just fixed for t-2→t-1. Both are wave 3, and `TestDoomedPinIsNotRotted` asserts "the
  same fixture run through **t-5's verb** reports nothing-to-do ... both assertions live in one
  test". A test that drives the cobra verb needs the verb to exist, and `depends_on` for t-6 lists
  t-1..t-4 only. The schema (assets/prompts/plan.md:37) restricts `depends_on` to lower waves, so
  this cannot be expressed as written.
  Suggestion: assert the nothing-to-do half through t-2's `planRedProofRepoint` (the decision layer
  that actually holds the c-5/c-7 tension) rather than through the verb; or move t-6 to wave 4 with
  `depends_on = [..., "t-5"]`.

- [granularity] t-4 is now 5 files and 3 layers — the fix for the previous review's flag is what
  pushed it over. `internal/cmd/redproof_replay.go`, `test.go`, `trust.go`, `local.go` plus its test:
  the schema's threshold is "5+ files OR more than 2 layers → split" (plan.md:44). The three layers
  are separable and land in different blast radii: the shared spawn seam in test.go (every spawn
  site in the binary depends on it), the consent store + grant verb (local.go/trust.go, a
  security-relevant surface), and the detached-worktree runner itself. Nine contract lines in one
  task is the other symptom.
  Suggestion: split the seam + consent grant (test.go, local.go, trust.go) into its own wave-1 task
  and leave t-4 as the worktree runner depending on it — or say in the description why the three
  must land atomically.

- [test-contract] t-4's grant path does not say how the verb learns *which* replay line to grant.
  `Trust()` (trust.go:245) is `Args: cobra.NoArgs` and reads `runtime.test_command` out of
  project.toml; a replay line lives in one phase's changes.json. Granting it therefore requires a new
  positional phase-id or a flag, and a change to the verb's argv contract — no contract line pins
  either. `TestTrustGrantsReplayCommand` says the verb "must show the replay line it is about to
  fingerprint", which is silent on invocation. Nothing asserts the existing no-args
  `dross trust` (test-command) behaviour survives the change.
  Suggestion: name the invocation shape in t-4's description, and add a line asserting the
  test-command grant path is unchanged.

- [test-contract] The replay gate's *error* states have undefined repoint behaviour. t-4 produces
  four outcomes — red, green, unconsented refusal, timeout-as-refusal — plus an implied spawn/worktree
  error path (`TestReplayCleansWorktree` names "spawn error"). t-5 pins the repoint's response to
  only three inputs: green (refuse), no Replay (unverified), recorded-but-unconsented (repoint,
  unverified). What the repoint does when the worktree add fails, the spawn errors, or the replay
  times out is unspecified in both tasks — and those are precisely the states that are neither red
  nor green, where "refuse" and "repoint as unverified" are both defensible and materially different.
  Suggestion: one t-5 contract line deciding whether a replay that could not be *run* refuses the
  repoint or repoints it as unverified.

- [test-contract] t-6's `TestShipRepointLeavesBaseClean` is vacuous for the failure it names. Its
  fixture is "a repo whose pin is already sound" — under the doomed predicate the hook is a no-op
  there, writes nothing, and commits nothing. A hook that wrote all over the base branch would pass
  this test unchanged. The named failure mode ("if the hook writes on the base branch") is only
  observable when the hook actually fires.
  Suggestion: run it over a doomed pin — the case where the hook writes two files and commits — and
  assert `pushBaseIfAheadDrossOnly` (basebranch.go:62, refusal at :96) still returns without refusing.

- [test-contract] The unconsented-replay resolution deviates from c-8's literal text and is recorded
  only in a test-contract line. c-8 reads: "Where one is recorded, repoint runs it in a detached
  worktree ... and refuses the repoint unless it goes red; where none is recorded, repoint states the
  repair is unverified." The plan introduces a third state — recorded but not consented on this
  machine → repoint and report unverified (t-5 `TestRepointUnconsentedReplayRepoints`, matched by
  t-6). That is the right resolution of the previous review's finding and it is what keeps c-1's
  "single command" true, but verify grades against the criterion text, which knows only
  recorded/not-recorded.
  Suggestion: record the three-state split as a `[[decisions]]` entry in spec.toml so verify reads
  c-8 the way the plan means it, rather than as a half-met criterion.

- [granularity] t-6 still bundles two independent behaviours and the amendment did not address it.
  The added prose argues the `repoint_commit` tension (which it now does well) but says nothing about
  size: ship.go's pre-stage hook and phase.go's `complete` warning share only the doomed-pin
  predicate, and exactly one of six contract lines (`TestCompleteWarnsAndFinishes`) is about the
  complete side.
  Suggestion: split the complete-side warning into a wave-4 task depending on t-6, or add one line
  saying the pairing is deliberate so a later reader does not re-open it.

## NOTE

- [coverage] All eight criteria remain covered: c-1 (t-5, t-7), c-2 (t-1, t-2, t-5), c-3 (t-2, t-5),
  c-4 (t-2, t-5), c-5 (t-2, t-5), c-6 (t-1), c-7 (t-6), c-8 (t-3, t-4, t-5). No orphan criteria, no
  task without one.

- [locked-decision] No task contradicts a locked decision. `doc_rewrite_scope` is honoured by t-1's
  ">=7-char prefix" rule (all three occurrences in fixtures/hostile-config-c5/RUN.md — :92 `BASE=`,
  :99 `base commit:`, :143 the worktree recipe — are full 40-char, and the unrelated
  `d62be414…` at :106 is protected by keying on the old pin's value). `repoint_surface`,
  `repoint_target_selection` and `repoint_commit` are all satisfied, the last now with an explicit
  argument in t-6.

- [antipattern] t-6 keys the doomed predicate on a hardcoded `refs/remotes/origin/phase/<id>` while
  `[repo].branch_pattern` is configurable — but phase.go:351 already does
  `phaseBranch := "phase/" + phaseID`, so the plan matches the tree's existing convention rather than
  introducing drift. No action.

- [forbidden-actions] rules.toml carries one rule (r-01, `make install` after Go/prompt edits) and no
  task violates it. It bites at execution time: t-5's `TestRepointClearsDoctor`, t-6's ship
  assertions and t-7's doctor-hint change all involve surfaces where a stale installed binary has
  produced false results in this repo before.

- [strengths] The t-1 amendment is the best fix in the set. Rewriting a *copy* keyed to whatever pin
  the copy carries, plus a live-fixture round-trip that names no SHA value, keeps real-fixture
  coverage while removing the self-invalidation — the test can no longer be broken by this phase's
  own first `repoint --apply`.

- [strengths] `TestRepointBlanketContinuesPastRefusal` pins partial-failure semantics for the blanket
  scan (three rotted pins, middle one refuses, other two still repaired, exit non-zero). That is the
  behaviour a blanket writer gets wrong by default, and most plans leave it to the implementer.

- [strengths] `TestPlanIndeterminateIsNotRotted` (shallow clone and a repo with no
  refs/remotes/origin/* both yield nothing-to-do) and `TestPlanDoesNotCacheForkPoint` (forkpoint.go
  :47-50 writes `BaseCommit` and `Save()`s it, so a dry-run planner must not go through it) are both
  failure modes discovered by reading the tree, not by imagining the feature.

## Resolved since first pass

- t-2→t-1 wave edge: t-2 is now wave 2 with `depends_on = ["t-1"]`, and t-5/t-6 moved to wave 3, t-7
  to wave 4.
- t-6's `depends_on` now includes t-3, so `changes.RedProof.Replay` exists before the ship hook reads
  it.
- t-4 now lists `internal/cmd/test.go` and names the mechanism (an `exec.CommandContext` variant),
  with `TestSpawnLocalCtxCancels` asserting the kill path and existing callers' behaviour.
- t-4's grant path now has assertions: `TestTrustGrantsReplayCommand` (prints then writes) and
  `TestReplayTrustNotInLocalKeys` (absent from `localKeys`, local.go:143; file gitignored). The file
  is named (trust.go); the invocation shape is not — see FLAG above.
- The recorded-but-unconsented case is now decided and consistent across t-5 and t-6:
  repoint-and-report-unverified, not refuse.
- t-6's description now argues why riding the phase's own PR satisfies `repoint_commit`'s rationale.
- t-1 no longer hardcodes the live `a6ef7295` pin or the swapped :92/:99 line labels.

## Summary

The seven amendments landed cleanly and nothing is blocking, but the fixes reintroduced one
same-wave dependency (t-6→t-5), pushed t-4 past the split threshold, and left three behavioural
gaps — the grant verb's invocation, the replay's error/timeout states, and a vacuous
base-branch test — that should be closed before executing.
