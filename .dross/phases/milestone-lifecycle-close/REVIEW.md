# Plan Review — milestone-lifecycle-close

Reviewed: 2026-08-12 (revision 2)
Plan: 9 tasks across 3 waves

## Prior blocking finding — resolved

The revision-1 BLOCKING item (t-6 deriving phase doneness from a 50-entry capped state history, which
had already evicted `completed mutation-diff-scope` and so made c-4's all-done arm unreachable) is
**resolved**. t-9 adds a per-phase-durable `Changes.Status` to `changes.json`, written by
`dross ship` ("shipped") and `dross phase complete` ("complete"), plus a one-off backfill of v1.3's
three finished phase dirs. t-6 now treats that marker as authoritative and history as a fallback
only, and its first contract line pins exactly the case that was broken: a phase counted done with
no breadcrumb anywhere. Re-verified against the repo — `.dross/state.json` still holds exactly 50
entries with no `mutation-diff-scope` breadcrumb, and all three phase dirs carry a `changes.json` for
the backfill to write into.

Two of revision 1's flags are also closed: the `strings.Contains` prefix hazard now has its own
contract line in t-6 (the `mutation-diff` / `mutation-diff-scope` case), and the t-6-vs-`/dross-status`
mechanism divergence is now deliberate and pinned ("verified is not complete"). One flag is carried
forward unchanged (c-3 has no command-level assertion) and one is partly addressed (see below).

## BLOCKING

(none)

## FLAG

- [test contract] **c-3 still has no assertion at the command level.** t-1 asserts the *classifier*
  renders "gone" and not "is not merged into", but t-1 changes nothing in `milestone.go`. t-8 lists
  c-3 in `covers` and none of its four scripted steps exercises it: step 2 deletes both refs, step 3
  hits the alreadyFinalized arm (status is already "complete", so the branch is never looked up).
  t-4 wires the classifier in but its five contract lines cover the status write, idempotence,
  stacking, delete-ordering and the leftover report — none of them the branch-gone message. So an
  implementation that classifies branchGone correctly and still prints the old refusal from
  `milestoneFinalize` passes every test in the plan.
  Suggestion: one contract line on t-4 — `origin/milestone/<v>` absent with `status = "active"` makes
  `dross milestone complete <v> --finalize` emit a message containing "gone" and not "is not merged
  into" — closes it without touching t-8.

- [antipatterns] **t-4 does not say the existing merge guard is removed.** t-1 builds a classifier
  that re-derives the stacked-parent target and the two ancestry checks that `milestone.go:275-298`
  already performs, and t-1's description says "nothing in milestone.go changes yet". t-4 says
  finalize "routes through t-1's classifier" but never states that the old block is deleted. If it
  isn't, the repo ends with two copies of the target-resolution and merge rules and no test that
  notices when they drift — and t-1's own contract line ("the existing stacking arm
  milestone.go:279-298 is pinned, not rewritten") reads as if the old block survives.
  Suggestion: make t-4's description explicit that lines 275-298 are replaced by the classifier call,
  and add a contract asserting only one refusal path exists (e.g. the "is not merged into" string
  appears exactly once in the package).

- [test contract] **t-7 has no arm for `milestone progress` exiting non-zero.** t-6's last contract
  line makes "no version arg and no `state.current_milestone`" a non-zero exit. That is precisely
  c-4's "scope when there is no active milestone" case, and it is the first thing t-7's dispatch step
  runs. Nothing in t-7's contract asserts the prompt tells Claude to read that failure as the
  scope arm rather than as a broken command.
  Suggestion: add a contract line pinning that the no-milestone arm names the non-zero exit / the
  no-current-milestone message as its trigger.

- [test contract] **t-7's three arms are not mutually exclusive.** Immediately after a successful
  finalize, `state.current_milestone` still names the milestone, its status is now "complete", and
  all its phases are done — so both the "no active milestone → scope" arm and the "all_done → drive
  completion" arm match, and the plan does not say which wins. t-6 emits `status` verbatim precisely
  so the dispatch can decide, but no contract pins the precedence.
  Suggestion: one contract line asserting the prompt branches on `status` before `all_done`, so a
  finalized milestone routes to scoping the next one rather than re-running completion.

- [test contract] **t-9 and t-6 assert against this repo's live `.dross` data.** t-9's fourth line
  asserts the three v1.3 phase dirs read `status = "complete"` after the backfill; t-6's second
  asserts a run against the real `.dross` reports those three slugs done and not in `remaining`.
  Both are committed-data assertions in a suite that otherwise builds temp fixtures — and t-3 ships
  `dross milestone remove`, whose entire purpose is editing `v1.3.toml`'s `phases` array. Editing that
  array, or archiving a phase dir, turns these red for reasons unrelated to the code under test.
  Suggestion: keep one of them (t-6's is the more valuable — it is the regression the whole marker
  exists for) and make it slug-membership-only, or move the assertion onto a fixture copy of the
  three `changes.json` files.

- [granularity] **t-9 spans 5 files and three seams** — the `internal/changes` schema, two call sites
  in `internal/cmd` (`ship.go`, `phase.go`), and a one-off data backfill of committed `.dross` state.
  The backfill in particular is the part most likely to be half-done or silently skipped, and it is
  the part t-6 depends on.
  Suggestion: probably keep it whole (the field and its writers are meaningless apart), but call the
  backfill out as its own commit inside the task so a partial run is visible in history.

- [granularity] **t-5 touches 5 files and three seams** — the detector (`milestone_stale.go`), the
  diagnostic reader (`doctor.go`), the destructive consumer (`milestonePrune` in `milestone.go`) —
  while also retrofitting milestone tomls into 13 existing `staleMilestoneBranches` call sites in
  `milestone_stale_test.go`. Carried forward from revision 1, unchanged.
  Suggestion: same as before — keep it whole (the signature change fans out mechanically and a split
  leaves the tree uncompilable mid-wave), but commit the fixture retrofit separately.

- [antipatterns] **t-6's history fallback is close to dead weight and re-imports the fragility the
  marker was added to escape.** After t-9's backfill, the only records the fallback can serve are
  phases from milestones older than v1.3 — whose breadcrumbs the 50-entry cap evicted long ago, so
  `milestone progress v1.2` reports done=0 with or without the arm. Meanwhile the arm is the sole
  reason t-6 needs a contract line defending against `historyHasAction`'s substring match.
  Suggestion: decide deliberately — either keep it and say in the doc-comment that it serves only
  in-window pre-t-9 records, or drop it and let un-backfilled milestones read not-done, which is at
  least honest. Not blocking: the guard rails (dir must exist, token not substring) are pinned either
  way.

- [test contract] **A prematurely hand-set `status = "complete"` is still only half-pinned.** Carried
  forward from revision 1 and partly addressed: t-4's leftover-report line covers `status="complete"`
  with `refs/heads/milestone/<v>` still present. It does not cover the dangerous shape — status
  hand-set complete while `origin/milestone/<v>` exists and is genuinely unmerged, which under the
  locked `already_finalized_evidence` rule makes `--finalize` a permanent silent no-op over unmerged
  work. t-1's contract still recommends `dross milestone set <v> status complete` as the branch-gone
  remedy, so the plan teaches the input that produces it. The lock is not in question; the pinned
  outcome is.
  Suggestion: extend t-4's leftover-report line to the remote ref, so the already-finalized arm names
  a surviving `origin/milestone/<v>` too.

## NOTE

- [coverage] All seven criteria appear in at least one `covers` field: c-1 (t-4, t-8), c-2 (t-1, t-4,
  t-8), c-3 (t-1, t-8), c-4 (t-9, t-6, t-7), c-5 (t-5, t-8), c-6 (t-2), c-7 (t-3). The one weak spot
  is c-3's assertion depth, flagged above — the `covers` field itself is complete.

- [locked decisions] No conflicts. `already_finalized_evidence` (t-1/t-4 read and write
  `[milestone].status`), `stacked_child_status` (pinned by t-1 and t-4), `remove_addressing` (t-3's
  second line pins error-not-no-op with a byte-identical toml), `toml_less_branch_not_stale` (t-5's
  second line, on a deliberately distinct fixture) and `phases_done_test` (t-6, with the
  unscaffolded-slug case pinned) are each honoured by a named contract line. `phases_done_test`'s
  *rationale* — "matches how /dross-status already counts progress" — is now literally inaccurate:
  `renderMilestone` (status.go:150-158) counts `verify.toml verdict == "pass"`, while t-6 counts the
  changes.json marker and explicitly rejects verify-pass. The two agree on v1.3 today (3/13 each) and
  will diverge on any phase that is verified but not yet shipped. The plan makes that divergence
  deliberate, which is the right call; the decision's `why` text is what is now stale.

- [wave-order] Revision 1 flagged that t-4 and t-6 share `milestone.go` and `README.md` in wave 2
  (t-5 touches `milestone.go` too). Downgrading: `dross execute` walks tasks one at a time via
  `dross task next` with an atomic commit each, and treats wave edges only as checkpoint boundaries
  (execute.md:229) — so same-wave file sharing is not a concurrent-write hazard here. t-6's
  `depends_on = ["t-3"]` is still not an output dependency, but it now costs nothing: t-9 pins t-6 to
  wave 2 regardless, so the edge cannot be stealing parallelism.

- [antipatterns] t-1 still lands a classifier with no caller until t-4 a wave later. More defensible
  in this revision — wave 1 now holds four independent tasks, so the split buys a real parallel slot
  and the classifier is genuinely unit-testable in isolation.

- [forbidden actions] No violations. The only rule in scope is r-01 (`make install` after editing
  prompts or Go code) and t-7 names it. `runtime.mode` is `native`, and every command test runs
  in-process through the existing `runCmd(t, Milestone(), …)` harness, so t-8's scripted lifecycle is
  not exposed to stale-binary risk. There is no global `~/.claude/dross/rules.toml`.

- [strengths] The t-9 → t-6 fix is the right shape, not a patch over the symptom: the durable signal
  is written at the two moments a phase actually finishes, the backfill covers the records that
  predate it, and history is demoted rather than deleted. t-9's fifth contract line — status survives
  a later `Record()` of a task — catches the specific way an append-only writer eats a new field.

- [strengths] Test contracts remain unusually falsifiable: they name the wrong implementation that
  fails. "a swap-with-last implementation fails", "the two runs differ only in origin/main's
  position", "dropping the Save fails the field assertion", "an implementation counting array entries
  fails here". t-4's pre-receive-hook line is the standout — it pins ordering by making the second
  half of the operation fail and asserting the first half already landed.

- [strengths] t-5 anticipates the fixture trap (adding the new gate would silently turn all 13
  existing `staleMilestoneBranches` fixtures not-stale) and requires the tomls in the same task, and
  t-8 names the seam it exists for rather than being a generic e2e — its step 4 pins the inverse
  property, so reverting either the flip or the gate fails it.

- [strengths] Every file and line citation re-verified and correct: `normalizeListField`
  (milestone.go:847), `milestoneSettablePaths` (milestone.go:771), the stacking arm
  (milestone.go:279-298), `historyHasAction` (phase.go:39-44, `strings.Contains`), Save-before-deletes
  (phase.go:560/568/586), the PR-number write (ship.go:374), `TestReadmeMilestoneRowDocumentsBaseFlag`
  (milestone_narration_test.go:61), the 50-entry cap (state.go:70). `staleMilestoneBranches` has
  exactly 13 test call sites and all are in the one file t-5 lists — the signature change breaks
  nothing outside it. `dross milestone progress` does not collide with an existing subcommand.

## Summary

The revision-1 blocker is properly fixed rather than papered over, and nothing new rises to blocking;
what remains is a cluster of missing assertions at the wiring seams — c-3's message is never checked
through the command, t-4 never says the old merge guard goes away, and t-7's dispatch arms overlap
without a stated precedence.
