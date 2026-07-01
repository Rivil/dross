# milestone-branch-model — VERIFICATION lens draft

Design principle for this draft: every criterion's behaviour is pinned to a Go
test *before* the task is sized. The keystone move is extracting the "where does
new work root?" decision into ONE unit-testable helper (`resolveNewWorkBase`)
that phase-create, quick, ship, and phase-complete all call. That helper is also
where the locked `rollout_cutover` lives, expressed as a *mechanism* rather than
a date: work roots on `milestone/<version>` **iff that branch actually exists**;
v0.7 was scoped under the old model so no `milestone/v0.7` ref exists, so it
falls back to main automatically — and that fallback is asserted by a test, not
assumed. Prompt-only behaviour (quick) is made testable by having the prompt
call a tiny CLI (`dross base-branch`) that returns the helper's result, so the
branch-selection contract lives in Go, not in un-testable markdown (r-01).

```
Phase milestone-branch-model — 7 tasks across 2 waves

Wave 1
  t-1  Create + push milestone/<version> at scope time
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-1
       contract: TestMilestoneCreateCutsBranchFromMain — after `dross milestone
                 create v0.9` in a git repo, `git rev-parse --verify
                 refs/heads/milestone/v0.9` succeeds and its tip == main's tip
                 (cut from main, HEAD stays on main). TestMilestoneCreatePushesEagerly
                 — with a bare origin wired, `git ls-remote --heads origin
                 milestone/v0.9` is non-empty. If branch creation or the eager
                 push is dropped, the respective assertion fails. TestMilestoneCreateNoGitSkips
                 — in a non-git dir create still writes the toml and does not error.

  t-2  Extract shared new-work base-branch resolver
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_test.go
       covers:   (enabler — powers c-2, c-3, c-4, c-6; delivers no criterion alone)
       contract: resolveNewWorkBase(repoDir, root) -> (base, milestoneActive, err).
                 TestResolveBase_MilestoneBranchExists: state.current_milestone=v0.9
                 AND refs/heads/milestone/v0.9 (or origin ref) present -> returns
                 ("milestone/v0.9", true). TestResolveBase_CutoverNoBranch:
                 current_milestone=v0.7 but NO milestone/v0.7 ref -> returns
                 (git_main_branch, false) — proves the v0.7 non-retrofit cutover.
                 TestResolveBase_NoMilestone: current_milestone empty -> returns
                 (git_main_branch, false). If the existence check is removed (so it
                 blindly returns milestone/<v>), the cutover test fails.

Wave 2
  t-3  Root phase/<id> on resolved base + no-milestone nudge
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2
       depends:  t-1, t-2
       contract: TestPhaseCreateRootsOnMilestoneBranch — put a commit on
                 milestone/v0.9 that is NOT on main, set current_milestone=v0.9,
                 run phase create; assert `git merge-base --is-ancestor <that
                 commit> phase/<id>` succeeds (phase branch descends from the
                 milestone tip, not main). If phaseCreate reverts to `checkout -b`
                 off HEAD/main, the milestone-only commit is unreachable and this
                 fails. TestPhaseCreateRootsOnMainNoMilestone — no milestone branch:
                 phase/<id> tip == main tip. TestPhaseCreateNudgesNoMilestone —
                 with no active milestone, command output contains a nudge naming
                 `dross milestone` (locked no_milestone_fallback).

  t-4  base-branch CLI + quick roots on it
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_test.go,
                 assets/prompts/quick.md, cmd/dross/main.go
       covers:   c-3
       depends:  t-1, t-2
       contract: TestBaseBranchCmdPrintsMilestone — `dross base-branch` prints
                 `milestone/v0.9` when that branch exists and current_milestone=v0.9;
                 TestBaseBranchCmdPrintsMainNoMilestone — prints the git_main_branch
                 otherwise. quick.md's standalone branch step is rewritten to use
                 `dross base-branch` output instead of hardcoding repo.git_main_branch;
                 the Go tests guard the exact string the prompt consumes, so a
                 resolver regression is caught even though the markdown is not
                 unit-testable (r-01).

  t-5  Ship PR base = resolved base branch
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-4
       depends:  t-1, t-2
       contract: Extend the Forgejo mock in ship_test to capture doc["base"] and
                 doc["head"] from the POST /pulls body. TestShipTargetsMilestoneBranch —
                 milestone/v0.9 present + current_milestone=v0.9: the single opened
                 PR has base=="milestone/v0.9", head=="phase/<id>". TestShipTargetsMainNoMilestone —
                 no milestone branch: base=="main". If ship keeps the hardcoded
                 p.Repo.GitMainBranch base, the milestone assertion fails.

  t-6  phase complete ff's milestone branch (not main)
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-6
       depends:  t-1, t-2
       contract: TestPhaseCompleteFastForwardsMilestone — with current_milestone=v0.9
                 and origin/milestone/v0.9 advanced to carry a `completed <id>`
                 state record, phase complete ends with local milestone/v0.9 ==
                 origin/milestone/v0.9 and phase/<id> deleted locally+remote. The
                 merged-record guard reads origin/milestone/v0.9:.dross/state.json
                 (not origin/main). If complete still targets main, local
                 milestone/v0.9 stays behind origin and the equality assertion fails.
                 TestPhaseCompleteNoMilestoneFfsMain — no milestone branch: original
                 main-ff behaviour preserved (regression guard for cutover repos).

  t-7  dross milestone complete: PR into main + post-merge cleanup
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-5
       depends:  t-1
       contract: Reuse buildOpenOpts + ship.OpenPR. TestMilestoneCompleteOpensSinglePRToMain —
                 mock provider records exactly one POST /pulls with base=="main"
                 and head=="milestone/v0.9"; a second run does not open a duplicate.
                 If head/base are swapped the assertion fails. Finalize path
                 (post-merge, mirrors phase complete): TestMilestoneCompleteFinalizeCleansUp —
                 once origin/main carries the merge, ff local main from origin and
                 delete milestone/v0.9 locally + on origin (locked milestone_branch_cleanup);
                 assert no refs/heads/milestone/v0.9 locally and none on origin.
```

## Coverage

| criterion | delivered by | verifying test(s) |
| --------- | ------------ | ----------------- |
| c-1 | t-1 | TestMilestoneCreateCutsBranchFromMain, TestMilestoneCreatePushesEagerly |
| c-2 | t-3 (via t-2) | TestPhaseCreateRootsOnMilestoneBranch, TestPhaseCreateRootsOnMainNoMilestone |
| c-3 | t-4 (via t-2) | TestBaseBranchCmdPrintsMilestone, TestBaseBranchCmdPrintsMainNoMilestone |
| c-4 | t-5 (via t-2) | TestShipTargetsMilestoneBranch, TestShipTargetsMainNoMilestone |
| c-5 | t-7 | TestMilestoneCompleteOpensSinglePRToMain, TestMilestoneCompleteFinalizeCleansUp |
| c-6 | t-6 (via t-2) | TestPhaseCompleteFastForwardsMilestone, TestPhaseCompleteNoMilestoneFfsMain |

Enabler t-2 (`resolveNewWorkBase`) has no numbered criterion of its own but its
three-branch unit test is where the locked `rollout_cutover` and
`no_milestone_fallback` decisions are actually pinned; c-2/c-3/c-4/c-6 each ride
on top of it.

## Judgment calls

- Chose a single shared `resolveNewWorkBase` helper over duplicating the
  milestone-vs-main branch decision inside phase.go, quick.md, ship.go, and
  phase.go-complete. Rejected inlining because the cutover rule would then be
  copy-pasted into four call sites (one of them un-testable markdown) and could
  drift; one helper = one test suite owns the decision.
- Chose to express `rollout_cutover` as "milestone branch exists?" rather than a
  version comparison or a date/flag. Rejected a stored "scoped under new model"
  flag because the branch's existence is already the eager side effect of c-1 and
  is directly observable in a test — v0.7 has no `milestone/v0.7` ref, so it
  falls back to main with zero extra state.
- Chose to add a `dross base-branch` CLI command so quick.md can consume the
  resolver's output. Rejected leaving c-3 as a pure prompt edit: r-01 says prompt
  changes aren't Go-testable, so without the command c-3 would have no verifying
  test — the whole point of this lens.
- Chose to fold the milestone-branch cleanup (locked milestone_branch_cleanup)
  into `dross milestone complete` as a post-merge finalize path rather than a
  separate task, mirroring phase complete's ls-remote merged-detection so it is
  test-drivable against a bare origin. Rejected a standalone cleanup task as too
  small (one command, no distinct criterion).
- Chose to assert phase-create rooting via `merge-base --is-ancestor` of a
  milestone-only commit, not by string-comparing branch tips. Rejected the tip
  comparison because right after creation `phase/<id>` and `milestone/<v>` tips
  are equal even if the branch was wrongly cut from main (when main==milestone);
  the ancestor probe against a commit unique to the milestone branch actually
  distinguishes the two roots.
- Chose to have `dross milestone create` create the branch WITHOUT switching HEAD
  (git branch + push, staying on main). Rejected `checkout -b` because the user
  then immediately runs phase create, whose preflight requires being on main;
  switching would force an extra checkout and break that invariant.
