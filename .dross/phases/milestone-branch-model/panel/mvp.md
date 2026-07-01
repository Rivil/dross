# MVP draft — milestone-branch-model

Lens: smallest task set that satisfies every criterion. One shared helper feeds
the three consumer commands; phase-create and phase-complete merge into one task
(same file, mirror behaviors); no new abstractions beyond the resolver.

Phase milestone-branch-model — 5 tasks across 2 waves

Wave 1
  t-1  Create+push milestone branch at scope, add resolver
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go, assets/prompts/milestone.md
       covers:   c-1
       contract: If `milestone create` stops cutting `milestone/<version>` from main (or cuts
                 from HEAD), TestMilestoneCreateBranch fails: it asserts refs/heads/milestone/v0.9
                 exists and its merge-base equals the main tip. If the eager push is dropped, the
                 bare-origin fixture assertion that origin carries milestone/v0.9 fails. If the
                 shared resolver returns a branch when current_milestone is empty OR when the
                 branch is absent (cutover case), TestActiveMilestoneBranch fails.

  t-2  Root standalone quick on active milestone branch
       files:    assets/prompts/quick.md
       covers:   c-3
       contract: If quick.md §0.4 standalone still says "branch must be repo.git_main_branch"
                 unconditionally, running /dross-quick with an active milestone roots the change
                 on main instead of milestone/<version> — the acceptance walkthrough for c-3
                 (quick with a scoped milestone lands on the milestone branch) fails. Prompt is
                 markdown (r-01: not Go-unit-testable); the load-bearing text is the milestone/
                 <version> fallback replacing the bare main check.

Wave 2
  t-3  Root and complete phases on the milestone branch
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2, c-6
       depends:  t-1
       contract: If phase create roots off main while a milestone branch exists,
                 TestPhaseCreateRootsOnMilestone fails (asserts phase/<id> merge-base == milestone
                 branch tip, not main tip). If phase complete fast-forwards main instead of the
                 milestone branch — or reads the `completed <id>` merge-guard from origin/main
                 instead of origin/milestone/<version> — TestPhaseCompleteFFMilestone fails
                 (asserts milestone/<version> advanced to origin while main is untouched). With no
                 active milestone branch both fall back to main (TestPhaseCreateFallbackMain).

  t-4  Target ship PR base at the milestone branch
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-4
       depends:  t-1
       contract: If ship sets OpenOpts.BaseBranch to git_main_branch while an active
                 milestone branch exists on origin, TestResolveShipBase fails (expects
                 milestone/<version>). With no current_milestone or no origin milestone branch
                 (v0.7 cutover), the same test expects main.

  t-5  Add `dross milestone complete`: PR into main + cleanup
       files:    internal/cmd/milestone_complete.go, internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-5
       depends:  t-1
       contract: If `milestone complete` opens no PR, or opens one with HeadBranch !=
                 milestone/<version> or BaseBranch != main, TestMilestoneCompletePR fails (it
                 inspects the OpenOpts passed to a stubbed opener). If a re-run after the
                 merge-commit lands on origin/main skips the ff-of-main + delete of
                 milestone/<version> local+remote, TestMilestoneCompleteCleanup fails (asserts
                 both refs gone and main at origin) — this honors the locked
                 milestone_branch_cleanup + milestone_main_merge decisions in the same command.

## Coverage
- c-1 → t-1
- c-2 → t-3
- c-3 → t-2
- c-4 → t-4
- c-5 → t-5
- c-6 → t-3

## Judgment calls
- Branch creation lives in `dross milestone create` (Go, testable), not the milestone.md
  prompt shelling out to git — chose the single-owner CLI side effect over prose git steps;
  rejected a separate `milestone branch` subcommand as speculative structure. Prompt gets a
  one-line note only.
- One shared resolver `activeMilestoneBranch(root, repoDir, remote)` feeds phase create, phase
  complete, and ship — chose reuse over three inline current_milestone lookups; the "branch
  exists" check inside it IS the rollout_cutover guard, so v0.7 (no milestone branch) falls back
  to main with zero retrofit code.
- Merged phase create + phase complete into t-3 — both live in phase.go, both are the
  milestone-branch swap of an existing git op; splitting would be two 10-min tasks in one file.
- Folded post-merge cleanup into t-5 rather than a separate `milestone complete --finalize`
  task — it is the same command re-run (mirrors phaseComplete's two-phase shape) and c-5's
  "completing a milestone" is the traceable owner of the locked cleanup decision. Rejected a
  standalone cleanup task as untraceable to any criterion.
- No new milestone-complete skill prompt — no criterion or locked decision requires one; the
  CLI command is invoked directly. Rejected scaffolding it as gold-plating.
- t-2 (quick) stays prompt-only with no CLI change — c-3 names `dross quick`, which is a skill,
  not a binary; adding a `dross quick` command to make it unit-testable would be new surface the
  criterion doesn't ask for.
