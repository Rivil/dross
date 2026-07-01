# Plan draft — RISK lens

Bias: failure modes drive the graph. The branch model multiplies the ways git
state can be wrong (branch already exists, push half-lands, base absent on
remote, local/origin divergence, no-git repo, no active milestone). Each of
those is assigned to exactly one owning task with a test that trips when the
guard regresses. Two small foundation modules concentrate the git primitives so
every risky operation is written and tested once, not re-implemented per caller.

Phase milestone-branch-model — 9 tasks across 2 waves

Wave 1
  t-1  Base-branch resolver (pure, fallback-owning)
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_test.go
       covers:   c-2, c-3, c-4 (shared decision), no_milestone_fallback
       desc:     resolveBaseBranch(root, p) -> (base, nudge, err): current_milestone set
                 -> ("milestone/<version>", "", nil); empty -> (git_main_branch,
                 "<nudge to scope a milestone>", nil). Plus milestoneBranchName(version).
                 No git calls, no state field added — base is derived from
                 current_milestone only.
       contract: if resolveBaseBranch returns "milestone/<v>" or an error when
                 current_milestone is empty (breaking the locked fallback),
                 TestResolveBaseFallbackToMain fails; if it returns the main branch
                 while a milestone is active, TestResolveBaseActiveMilestone fails;
                 if the fallback drops the nudge string, TestResolveBaseNudgeEmitted fails.

  t-2  Milestone-branch git primitives
       files:    internal/cmd/milestonebranch.go, internal/cmd/milestonebranch_test.go
       covers:   c-1, c-4, c-6 (machinery), milestone_branch_push, milestone_branch_cleanup
       desc:     createMilestoneBranch(repoDir, main, version): refuse when
                 milestone/<v> already exists locally OR on origin, else checkout -b
                 off main and eager `push -u origin`; idempotent no-op when already
                 present+pushed. ffMilestoneBranchFromOrigin. deleteMilestoneBranch
                 (local + remote, idempotent via ls-remote probe).
                 milestoneBranchOnRemote(repoDir, version) probe. Tested against a
                 real bare remote (ship_recover_test.go pattern).
       contract: against a bare remote, if createMilestoneBranch stops pushing the
                 branch to origin, TestCreateMilestonePushesEagerly (asserts
                 ls-remote shows the ref) fails; if it silently succeeds when
                 milestone/<v> already exists on origin instead of no-op/refuse,
                 TestCreateMilestoneExistingRemote fails; if deleteMilestoneBranch
                 errors when the remote ref is already gone, TestDeleteMilestoneIdempotent
                 fails; if a failed push is reported as success, TestCreateMilestonePushFailurePropagates fails.

Wave 2 (depends t-1, t-2)
  t-3  Milestone-branch create command + scope-time wiring
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go,
                 assets/prompts/milestone.md
       covers:   c-1
       depends:  t-2
       desc:     Add `dross milestone branch [version]` calling createMilestoneBranch
                 off git_main_branch. Wire milestone.md §6 Activate to run it right
                 after `dross state set current_milestone`. Non-git dir -> skip with
                 a printed note, not a panic.
       contract: against a bare remote, if `dross milestone branch v0.9` fails to
                 create-and-push milestone/v0.9 rooted on main,
                 TestMilestoneBranchCreatesAndPushes fails; if a re-run errors on an
                 existing origin branch instead of no-op'ing, TestMilestoneBranchRerunIdempotent
                 fails; if run in a repo with no .git it aborts hard instead of
                 skipping, TestMilestoneBranchNoGit fails. (milestone.md edit is
                 prompt-only per r-01; its contract is the CLI test above.)

  t-4  Root phase/<id> (create + insert) on the milestone branch
       files:    internal/cmd/phase.go, internal/cmd/phase_lifecycle.go,
                 internal/cmd/phase_test.go, internal/cmd/phase_lifecycle_test.go
       covers:   c-2
       depends:  t-1
       desc:     Add forkPhaseBranch helper: resolve base via resolveBaseBranch, then
                 `git checkout -b phase/<id> <base>`. phaseCreate and phaseInsert both
                 use it. Relax preflightPhaseBranch: keep clean-tree + no-existing-
                 phase-ref guards; drop must-be-on-main; add guard refusing when a
                 milestone is active but milestone/<v> is missing locally (point at
                 `dross milestone branch`).
       contract: with current_milestone=v0.9 and milestone/v0.9 present, if phase
                 create forks off main/HEAD instead of milestone/v0.9,
                 TestPhaseCreateForksOffMilestone (asserts merge-base == milestone/v0.9
                 tip) fails; if the milestone branch is absent and create silently
                 forks off HEAD instead of refusing with a create-branch nudge,
                 TestPhaseCreateMissingMilestoneBranchRefuses fails; if `phase insert`
                 doesn't fork off the same base, TestPhaseInsertForksOffMilestone
                 fails; with no milestone, TestPhaseCreateFallbackForksOffMain must
                 still pass.

  t-5  Ship targets the milestone branch as PR base
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-4
       depends:  t-1, t-2
       desc:     Compute ship's baseBranch via resolveBaseBranch instead of the
                 hardcoded git_main_branch. Add a pre-flight guard (before push and
                 OpenPR) refusing when the resolved base is absent on origin
                 (milestoneBranchOnRemote), pointing at `dross milestone branch`.
       contract: with an active milestone, if opts.BaseBranch is not milestone/<v>,
                 TestShipBaseIsMilestone fails; if the milestone branch is missing on
                 origin and ship pushes / opens a PR against a nonexistent base
                 instead of failing pre-flight, TestShipRefusesMissingRemoteBase
                 fails; with no milestone, TestShipBaseFallbackMain (base==main) must
                 still pass.

  t-6  Phase complete fast-forwards the milestone branch
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-6
       depends:  t-1, t-2
       desc:     When a milestone is active, phaseComplete checks out milestone/<v>,
                 reads its `completed <id>` origin guard from
                 origin/milestone/<v>:.dross/state.json, ff-only from origin, then
                 deletes phase/<id> (local+remote). Divergence between local
                 milestone/<v> and origin aborts non-destructively; --recover targets
                 the milestone branch (parameterize runDrossRecovery's reconcile
                 branch). No active milestone -> unchanged main behavior.
       contract: with an active milestone, if complete ff's main instead of
                 milestone/<v>, TestPhaseCompleteFfsMilestoneBranch fails; if a
                 diverged local milestone/<v> is silently reset (or the ff no-ops)
                 without --recover, TestPhaseCompleteMilestoneDivergedAborts fails; if
                 the origin `completed <id>` guard still reads origin/main under a
                 milestone, TestPhaseCompleteReadsMilestoneGuard fails; with no
                 milestone, TestPhaseCompleteFallbackMain must still pass.

  t-7  `dross milestone complete` opens one PR into main
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go,
                 assets/prompts/ship.md
       covers:   c-5
       depends:  t-1, t-2
       desc:     Add `dross milestone complete [version]`: ensure milestone/<v> is on
                 origin (push if needed), open a single PR head=milestone/<v>
                 base=git_main_branch via ship.OpenPR. Body/prompt instruct a
                 merge-commit (non-squash) merge so main stays ff-able
                 (milestone_main_merge). Refuse when the milestone branch isn't
                 pushable / base main absent on remote.
       contract: if milestone complete opens a PR whose head/base is not
                 milestone/<v> -> main, TestMilestoneCompleteHeadBase fails; if it
                 opens more than one PR (e.g. one per phase), TestMilestoneCompleteSinglePR
                 fails; if milestone/<v> is absent on origin and it opens a PR anyway
                 instead of refusing, TestMilestoneCompleteRefusesUnpushed fails.
                 (ship.md merge-method note is prompt-only per r-01.)

  t-8  Milestone post-merge cleanup (ff main + delete branch)
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-5 (cleanup half), milestone_branch_cleanup
       depends:  t-2
       desc:     Add the cleanup path (flag or `milestone complete --finish`): after
                 the milestone PR merges, checkout git_main_branch, ff from origin,
                 then deleteMilestoneBranch(local+remote), and set milestone status
                 shipped. Refuse to delete if main hasn't actually absorbed the merge
                 (guard on origin/main containing the merge) so a stale branch isn't
                 destroyed prematurely.
       contract: against a bare remote, if cleanup leaves milestone/<v> on origin OR
                 locally, TestMilestoneCleanupDeletesBoth fails; if it deletes the
                 branch while origin/main hasn't merged it (data loss), TestMilestoneCleanupRefusesUnmerged
                 fails; if main isn't ff'd to origin after cleanup, TestMilestoneCleanupFfsMain fails.

  t-9  `dross milestone base` CLI + quick branches off it
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go,
                 assets/prompts/quick.md
       covers:   c-3
       depends:  t-1
       desc:     Add `dross milestone base` printing resolveBaseBranch's base to
                 stdout (nudge to stderr on fallback). Edit quick.md §0 pre-flight
                 standalone branch-check to branch off `dross milestone base` output
                 instead of repo.git_main_branch.
       contract: if `dross milestone base` prints main while a milestone is active
                 (so standalone quick would root on main), TestMilestoneBasePrintsMilestone
                 fails; if it doesn't print main with no active milestone,
                 TestMilestoneBaseFallback fails. (quick.md edit is prompt-only per
                 r-01; its contract is the CLI test above.)

## Coverage
- c-1 (milestone scope creates milestone/<version> off main): t-2 (create+push primitive), t-3 (command + milestone.md wiring)
- c-2 (phase create roots on milestone branch): t-1 (resolver), t-4
- c-3 (quick roots on milestone branch): t-1 (resolver), t-9 (`milestone base` CLI + quick.md)
- c-4 (ship PR base = milestone branch): t-1 (resolver), t-2 (remote probe), t-5
- c-5 (milestone complete = one PR into main): t-7 (open PR), t-8 (post-merge cleanup)
- c-6 (phase complete ff's milestone branch): t-1 (resolver), t-2 (ff primitive), t-6

## Judgment calls
- No new state field: derive the milestone branch as `milestone/<current_milestone>` (chosen) vs adding a `current_milestone_branch` field (rejected) — a field is a schema+migration surface that can drift from current_milestone; deriving removes that whole class of stale-state bug.
- Two foundation modules (t-1 pure resolver, t-2 git primitives) in separate files (chosen) vs inlining base logic into each caller (rejected) — inlining would re-implement the "branch exists / push half-lands / remote absent" guards per call site, so each risk would be owned by 3-4 tasks; centralizing makes each risk testable exactly once, and separate files let t-1/t-2 run truly parallel with no merge conflict.
- resolveBaseBranch is name-only; existence/divergence guards live with each consumer (chosen) vs a single resolver that also probes git existence (rejected) — "branch missing" means different things per caller (local ref for phase create's fork, origin ref for ship's PR base, origin-tracking for complete's ff), so one probe can't serve all three; each consumer owns the existence risk that matters to it.
- Split milestone complete into t-7 (open PR) and t-8 (cleanup) (chosen) vs one task (rejected) — the PR-open risk (wrong head/base, multiple PRs) and the cleanup risk (premature branch deletion / data loss) are distinct failure modes; splitting keeps each owned and tested by exactly one task, per the lens.
- Relax phaseCreate's must-be-on-main preflight to fork explicitly off the resolved base (chosen) vs require the user to be sitting on the milestone branch first (rejected) — an explicit `checkout -b phase/<id> <base>` removes the "wrong current branch" failure mode entirely and works whether HEAD is on main or the milestone branch; the clean-tree and no-existing-phase-ref guards are retained.
- phase_lifecycle insert reuses the same forkPhaseBranch helper as create, folded into t-4 (chosen) vs a separate task (rejected) — both share one fork-point risk; a separate task would split that single risk across two owners, which the lens forbids. (move/rename need no change: move only reorders the array, rename renames an existing branch — neither picks a fork point.)
- ship_recover / doctor legacy machinery left for v0.7 only, not retrofitted (honoring locked rollout_cutover); the only recovery change is parameterizing runDrossRecovery's reconcile branch so phase complete --recover under a milestone heals milestone/<v>, not main.
