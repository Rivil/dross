# Plan Review — milestone-push-failure-tests

Reviewed: 2026-08-30
Plan: 3 tasks across 2 waves

## BLOCKING

(none)

The prior BLOCKING is resolved. I re-derived reachability from source rather than
taking the amendment on trust — each arm the amended `push_test_fixture` now claims
is genuinely reachable:

- c-5 (repoint origin → nonexistent path): `gitCombined(repoDir, "fetch", "origin")`
  at internal/cmd/milestone.go:1026 returns `git fetch: %w\n%s` before anything else.
  Reachable, and the contract's claim holds in the swallow case too: with a stale
  local `refs/remotes/origin/<branch>` surviving the repoint, dropping the fetch
  guard falls through to the rev-list and returns `(false, 0, nil)` — the exact
  "reports the branch as not-ahead" failure c-5 names.
- c-1 (pre-receive hook + unpushed branch): fetch succeeds against a real bare origin,
  the `refs/remotes/origin/<branch>` check at :1029 misses, and the `push -u` at :1030
  is refused by the hook → `push %s to origin: ...`, which names the branch. Reachable.
- c-2 (pre-receive hook + branch 2 ahead): :1046 → `push of %d local commit(s) on %s
  failed: ... would be lost at --finalize`. Reachable, and the literal the contract
  pins renders exactly as `push of 2 local commit(s)`.

The four prior FLAGs are all genuinely applied, not merely acknowledged: t-1 now
states the c-1 test is one test making a direct-call observation plus a
`milestone complete` observation on one fixture; contract 3 pins the literal phrase
rather than the bare digit; t-2 states the 2-ahead/1-behind asymmetry in both the
description and contract 1; and t-3's contracts 2 and 3 are now distinct (below).

## FLAG

- [antipattern/duplication] t-1 prescribes building machinery that already exists in
  package `cmd`. `originOf(t, dir)` (internal/cmd/milestone_finalize_test.go:22) is
  verbatim `git remote get-url origin`, and `rejectPushes(t, origin)` (same file, :30)
  installs exactly the refusing pre-receive hook the amended decision names — it is the
  second copy of basebranch_test.go:302's script and would become the third. t-1 instead
  describes widening `milestonePushHeadFixture`'s 3-value return to expose the origin
  path and writing the hook fresh.
  Suggestion: drop the origin-path widening entirely — call `originOf(t, dir)` and
  `rejectPushes(t, originOf(t, dir))` from the tests. Keep only the fixture work that is
  actually new: the variant leaving the milestone branch unpushed.

- [antipattern/duplication] t-3's "Extend the fakeStryker seam so a test can make the
  fake process emit output" is already done. `noisyStryker(t, s, out, code, place)` at
  internal/mutation/stryker_test.go:696 — in the very file t-3 edits — prints arbitrary
  output via a quoted heredoc, exits with a chosen code, and optionally places a report.
  `noisyStryker(t, s, <header + a failing test>, 1, nil)` produces precisely the case
  t-3 needs: `sh` exits 1 → tolerated as `*exec.ExitError` → no report on disk → the
  "did not write a report" branch at stryker.go:142, with the header sitting in the head
  buffer. No seam change is required.
  Suggestion: reuse `noisyStryker` and delete the seam-extension work from the task.

- [wave-order] t-2's `depends_on = ["t-1"]` is justified in its description as needing
  "t-1's widened fixture". If the flag above is taken, that widening does not happen and
  t-2 needs nothing t-1 produces — its divergence and clean-ahead fixtures both build on
  `milestonePushHeadFixture` exactly as it stands today. The real coupling is that both
  tasks write internal/cmd/milestone_push_head_test.go, so wave 2 still buys zero
  parallelism (t-1 || t-3 remains the only concurrent pair).
  Suggestion: keep the serialization, but restate the dependency as file-level rather
  than output-level, so it is not later "optimized" into wave 1 on the grounds that the
  fixture dependency turned out to be imaginary.

## NOTE

- [test-contract] t-3 contracts 2 and 3 are now genuinely distinct, and I checked the
  unique-failure sets rather than the wording. Editing the constant alone fails contract
  2 only (it feeds a hardcoded literal, so the gate stops matching) and leaves contract 3
  green (it feeds the constant on both sides). Rewording the note alone fails contract 3
  only. They overlap only on "the note is dropped entirely", which is ordinary, not an
  artificial split.

- [test-contract] Contract 2's guarded change — an edit to a string constant — is not in
  gremlins' mutator set (arithmetic, conditionals, increments, inverts; no string-literal
  mutator). It is a plain regression guard and will not show up in the phase's mutation
  numbers. Worth knowing before verify, so its absence from the score is not read as a
  gap.

- [coverage] t-1's `cap.created == 0` assertion is load-bearing only because nothing in
  the open path pushes before internal/cmd/milestone.go:261. I checked: `milestonePRBase`
  (:322) does refs reads and `milestoneVerifyGate` reads phase state; neither pushes. So
  with the hook installed the head push is the first thing it refuses, and the assertion
  fails for the right reason. If a push is ever added earlier in the open path, that test
  starts passing vacuously — worth a line in the test's comment.

- [coverage] c-6 overlaps `TestMilestoneCompletePushesLocalCommitsBeforeOpeningPR`, which
  already pins origin's tip after a clean-ahead push at command level. The new test's
  added value is the return values the command swallows — `pushed == true` and
  `commits == 2` by value on a two-commit branch, where the existing test makes one
  commit and can only see the printed line. t-2 already asks for a comment saying so.

- [locked-decision-conflict] No conflicts. `note_trigger` (constant beside
  `strykerDropWarningText`, conditional attachment) and `note_adapter_scope` (stryker.go
  only) both match t-3's files and description; gremlins is untouched. The amended
  `push_test_fixture` matches t-1 and t-2 — real fixtures, bare origin, no seam.

- [granularity/forbidden-actions] No task touches 5+ files or spans multiple layers;
  every file named exists. Nothing violates r-01 or `runtime.mode = "native"` — t-3
  correctly scopes `make install` to manual checks of the new message, not to the Go
  tests, which compile from source.

- [strengths] Three things this plan gets right:
  1. The amendment cites its own evidence and names the line that forced it, rather than
     silently swapping the mechanism. `TestPushBaseRejectedPushSurfacesGitOutput` is the
     right precedent to have found, and c-2 being added to the decision's criterion list
     is a correction the review did not ask for.
  2. Contracts are stated as the mutant that must die, not as coverage — off-by-one in
     the count, counts swapped, note attached unconditionally, fetch error swallowed. The
     `push of 2 local commit(s)` literal in particular defeats the specific hazard that
     git's appended combined output creates.
  3. The 2-ahead/1-behind asymmetry and the negative no-note case are both the assertions
     that usually get omitted, and both are here for stated reasons.

## Summary

No blocking issues: the amended mechanism reaches every arm it claims and all four prior
flags are properly applied — the remaining findings are that t-1 and t-3 each plan to
build a helper the package already has.
