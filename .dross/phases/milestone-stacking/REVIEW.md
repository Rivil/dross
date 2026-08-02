# Plan Review — milestone-stacking

Reviewed: 2026-08-02
Plan: 8 tasks across 3 waves

## BLOCKING

(none)

Coverage is complete (c-1: t-2/t-4, c-2: t-1/t-4, c-3: t-2/t-5, c-4: t-6, c-5: t-4/t-8,
c-6: t-3/t-7). No task contradicts a locked decision. No rule violation: `runtime.mode`
is `native`, contracts run through `go test`, and t-8 carries the r-01 `make install`
step for the `assets/prompts/` edit.

## FLAG

- [test-contract] c-1 names three main-arms — parent merged, parent **absent**, no current
  milestone — and t-4's contract pins only two. Nothing tests `current_milestone = v1.1`
  while `milestone/v1.1` no longer exists (the normal state after `complete --finalize`
  deleted it). That is the arm most likely to be implemented as an error: t-2's probe is
  specified for "a branch origin has never seen" (answer from local refs), but not for
  "neither origin nor local has this ref". If the probe errors there, every
  `milestone create` after a finalize fails until the user clears `current_milestone`.
  t-5 pins exactly this case for the PR path ("origin/milestone/v1.1 deleted outright it
  targets main rather than erroring on a dead ref") — the asymmetry is the tell.
  Suggestion: add a t-2 contract line for "no ref anywhere → merged=true (or an explicit
  absent signal), never an error", and a t-4 line asserting the cut point and recorded
  base are both `main` in that shape.

- [file-conflict] t-4, t-5 and t-6 all sit in wave 2 and all edit
  `internal/cmd/milestone.go`. t-5 and t-6 both edit the *same function*:
  t-5 relaxes `milestoneFinalize`'s ancestry guard (internal/cmd/milestone.go:222) and
  t-6 inserts the dependent gate before the same function's branch deletes
  (internal/cmd/milestone.go:240-256). Run in parallel these two conflict for certain.
  Suggestion: either serialize t-5 → t-6 (or note the sequencing in `depends_on`), or move
  the finalize-guard change out of t-5 and into t-6, which already owns that function.

- [test-contract] t-5 relaxes finalize's guard so a child merged into its parent can be
  finalized, but nothing pins what finalize then *does*. The rest of the function is
  written for a merge that landed on main: it fast-forwards local `main` from
  `origin/main` (internal/cmd/milestone.go:236), deletes the child branch, and prints
  "`<main>` is at origin, `<msBranch>` deleted". In the stacked case main has not advanced,
  and the parent branch — the branch that actually received the merge — is untouched and
  unnarrated. The contract line only says "passes its ancestry guard".
  Suggestion: add a contract line pinning the post-finalize topology and the printed
  line for the merged-into-parent case, so the message can't claim main advanced when it
  did not.

- [test-contract] c-6 requires the provider gate on *both* `prune` and
  `complete --finalize`; all four of t-7's contract lines exercise `prune` only. The
  shared `dependentMilestones` seam probably gives finalize the behaviour for free, but
  "probably wired" is exactly the thing that regresses silently — and the failure mode is
  a deleted branch under an open PR.
  Suggestion: add one t-7 line asserting `complete <v> --finalize` also refuses on the
  stubbed open PR and deletes neither ref.

- [test-contract] The locked `unmerged_test` decision requires the offline fallback to say
  so. t-2 pins the `localOnly` return value, and t-4's description mentions printing the
  caveat, but no contract line in t-4/t-5/t-6 asserts the caveat actually reaches the
  user's output. A `localOnly` that every caller drops satisfies every test in the plan.
  Suggestion: one assertion on create's output in the offline shape.

- [docs] t-8 rewrites the *cut* narration, but t-5 also falsifies the *PR-target*
  narration that the plan leaves in place. `README.md:190` states `complete` "opens the
  milestone→main PR"; `README.md:5` states "the milestone lands in `main` as a merge
  commit"; `assets/prompts/ship.md:160` states `dross milestone complete` "opens one
  `milestone/<version>` → `main` PR". All three become conditionally false the moment t-5
  lands, and ship.md is outside t-8's file list entirely. c-5's own wording — "no surviving
  claim that the branch is cut from main unconditionally" — is about the cut, so this sits
  just outside its literal text, but it is the same class of stale claim the criterion
  exists to kill, and rule r-02 says don't leave it for the next run.
  Suggestion: extend t-8 to the PR-target sentences and add `assets/prompts/ship.md` to
  its file list.

- [api-surface] t-1 adds `Base` to `milestone.Meta`, but `milestone get`/`set` resolve
  through hardcoded switches — `readMilestoneDotted` (internal/cmd/milestone.go:541) and
  `milestoneSettablePaths` (internal/cmd/milestone.go:566). Neither gains
  `milestone.base`, so `dross milestone get base` returns unknown-field for the one fact
  this whole milestone exists to store. `assets/prompts/milestone.md:108` tells every
  agent never to read the toml directly, so `show --json` is the only remaining route.
  (No existing test enumerates struct fields against those switches, so this breaks
  nothing — it is a hole, not a regression.)
  Suggestion: add `milestone.base` to `readMilestoneDotted` in t-1. Leave it out of
  `milestoneSettablePaths` — the locked `base_override` decision makes `create` the sole
  writer, and a settable path would reintroduce hand-reconstruction.

- [wave-order] t-8 is in wave 3 depending on t-4, but it edits three markdown files and a
  grep test. Nothing in it consumes t-4's output — the grep test would pass against the
  docs alone. The ordering is defensible (don't document `--base` before it exists), but
  it is the critical path's tail for no mechanical reason.
  Suggestion: keep it if the intent is "docs never describe unshipped behaviour"; state
  that as the reason. Otherwise it drops to wave 1.

## NOTE

- [locked-decisions] t-4's "current_milestone is ignored when it names the version being
  created" is a refinement of the `stacking_parent` lock, not a conflict — self-parenting
  is incoherent and the lock does not contemplate it. Worth recording as such so it isn't
  read as drift at execute time.

- [granularity] t-6 and t-7 both write `internal/cmd/milestone_dependents.go` and could be
  one task (which would also collapse wave 3 to just t-8). The split is justified — they
  map to distinct criteria (c-4 offline scan vs c-6 provider query) with distinct failure
  modes — so this is an observation, not a recommendation to merge.

- [verification] Every repo claim in the plan checks out against the tree:
  `internal/cmd/milestone.go:342` (`ensureMilestoneBranch`, single caller at :320),
  `internal/cmd/milestone.go:173` (`opts.BaseBranch = mainBranch`),
  `isAncestor` at `internal/cmd/milestone_stale.go:193`, `ghCommand` as
  `gitHubPRMerged`'s seam, `milestone.Milestone{}` already registered at
  `internal/cmd/json_tag_parity_test.go:32`, and all five named fixtures/tests exist.
  t-8's line citations (prompt:14, README:190, roadmap:310) are exact, and its claim that
  the literal "integration branch from main" appears only in `assets/prompts/milestone.md`
  is correct.

- [strength] The test contracts are written as mutations, not descriptions — "swapping the
  is-ancestor argument order flips both", "a hardcoded \"main\" fallback fails this", "a
  create keyed on milestone.status passes the first case and fails this one". Each one
  names the wrong implementation it kills. This is the standard the check exists to
  enforce and the plan clears it throughout.

- [strength] Fail-closed reasoning is stated *and* tested, not just asserted: t-1's
  `LoadAll` must error on an unreadable toml, and t-6 pins that an unreadable toml makes
  the delete gate refuse. The justification — "an unreadable toml is indistinguishable
  from no dependents, and the consequence is an irreversible remote branch delete" — is
  the right frame for a destructive gate.

- [strength] Back-compat is pinned rather than assumed. A pre-v1.2 fixture toml decoding
  without error, and four existing tests named as must-keep-passing
  (TestMilestoneCreateNoGitSkips, TestPruneDeletesOnlyStaleBranches,
  TestMilestoneCompleteOpensSinglePRToMain, TestTomlFieldsCarryMatchingJSONTags), turns
  the `absent_base_reads_main` lock into something a mutation can break.

## Summary

Structurally sound and unusually well-verified against the tree — no blocking defects, but
the untested "parent branch absent" arm of c-1, the t-5/t-6 collision inside
`milestoneFinalize`, and the PR-target doc claims t-5 falsifies should be settled before
execution.
