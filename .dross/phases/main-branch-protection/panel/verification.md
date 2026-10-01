# main-branch-protection — verification-lens draft

Lens: every task below was derived backward from the test contract that proves its
criterion. The ideal contract came first; the task is the smallest change that makes
that contract satisfiable. Where a criterion is about origin's live state (c-1, c-7,
c-8, c-10), the static contract pins the ruleset/workflow shape, and t-13 carries the
live contract against Rivil/dross itself (locked `apply_verb`: dogfood proves c-9).

Fixed names the tasks share (plan-level, so parallel tasks agree without a code dependency):
- verb: `dross protect` (preview) / `dross protect --apply` (write); fix hint constant `"dross protect --apply"`
- rulesets: `dross: main` → `refs/heads/<git_main_branch>`; `dross: milestones` → `refs/heads/milestone/*`
- chore branch: `dross/chore/<base>` (e.g. `dross/chore/main`, `dross/chore/milestone/v1.7`)
- doctor heading: `Branch protection:`
- new package: `internal/protect` (pure logic, no exec, no network)

```
Phase main-branch-protection — 13 tasks across 5 waves

Wave 1
  t-1  Scan workflows for pull_request job checks
       files:    internal/protect/workflows.go (new), internal/protect/workflows_test.go (new)
       covers:   c-2, c-9
       desc:     PullRequestJobs(repoDir) []string — line scanner over .github/workflows/*.y{a,}ml;
                 a workflow counts only if `on:` carries `pull_request` (scalar, flow list, block,
                 quoted "on"); context = job `name:` if set, else job id. No YAML dependency.
       contract: - fixtures `on: pull_request`, `on: [push, pull_request]`, block `on:\n  pull_request:`,
                   and `"on":` each yield their jobs; a workflow with only `pull_request_target`
                   (the t-4 auto-merge shape) yields NONE — if the scanner matched the
                   `pull_request` prefix, TestPullRequestTargetExcluded fails
                 - release.yml-shaped fixture (push + workflow_dispatch) yields nothing
                 - job with `name: Lint Go` yields context "Lint Go", not "lint" (GitHub reports
                   the name; requiring the id would block every PR forever)
                 - job with `strategy: matrix:` or `name: ${{ … }}` returns an error naming the
                   job (its check names can't be predicted), never a silently-wrong context
                 - a `# jobs:` comment line and a step-level `name:` are not read as jobs

  t-2  Build main and milestone rulesets
       files:    internal/protect/ruleset.go (new), internal/protect/ruleset_test.go (new)
       covers:   c-1, c-4, c-6, c-7, c-10
       desc:     Ruleset types + MainRuleset(branch, checks) and MilestoneRuleset() with the
                 REST create/update JSON shape; MainRuleset refuses an empty checks list.
       contract: - marshaled JSON of BOTH rulesets contains the literal `"bypass_actors":[]`; if it
                   marshals as null or is omitted, a PUT would keep a stale admin bypass, and
                   TestBypassActorsExplicitlyEmpty fails (c-1, c-7 "admin included")
                 - main rule types are exactly {deletion, non_fast_forward, pull_request,
                   required_status_checks}; adding required_linear_history or merge_queue fails
                   TestMainRulesetKeepsMergeCommits (c-4, locked merge_history); dropping deletion
                   or non_fast_forward fails TestMainRulesetBlocksForcePushAndDelete (c-7)
                 - pull_request params: required_approving_review_count == 0 (locked approvals),
                   allowed_merge_methods == ["merge","squash"] — "merge" missing fails (milestone
                   PRs, c-4); "rebase" present fails (a rebased chore PR strands local commits)
                 - required_status_checks: strict_required_status_checks_policy == false (bot PRs
                   are never rebased by hand, c-6) and every check pins integration_id 15368
                   (GitHub Actions — dispatched runs satisfy it, a hand-posted status can't)
                 - include == ["refs/heads/<branch>"] for branch "trunk" (configured main, c-9)
                 - MainRuleset("main", nil) returns an error: a main ruleset requiring zero checks
                   would let a red PR merge (c-2)
                 - MilestoneRuleset: include ["refs/heads/milestone/*"], rules exactly
                   {pull_request}; a `deletion` rule fails TestMilestoneRulesetAllowsFinalizeDelete
                   (c-10 finalize), a `required_status_checks` rule fails it too (blocks the
                   scope-time branch creation)

  t-3  Assess live branch rules for gaps
       files:    internal/protect/assess.go (new), internal/protect/assess_test.go (new)
       covers:   c-3, c-5, c-7
       desc:     LiveRule/LiveRuleset types (shape of GET rules/branches/{b} and GET rulesets/{id});
                 Assess(rules, rulesets, wantChecks) → []Gap{Unprotected, MissingCheck(name),
                 AdminBypass(ruleset), DeleteOrForcePushAllowed, Unknown(why)}; RequiresPR(rules).
       contract: - zero rules → exactly [Unprotected]; a ruleset with enforcement "evaluate" or
                   "disabled" also → Unprotected (evaluate mode blocks nothing — a false green if read
                   as protected)
                 - want {test, shellcheck}, required {test} → [MissingCheck("shellcheck")]
                 - bypass_actors non-empty OR current_user_can_bypass ∈ {always,
                   pull_requests_only} → AdminBypass naming the ruleset
                 - bypass_actors absent from the response (token can't see it) → Unknown, never
                   no-gap — TestUnreadableBypassIsUnknown fails if Assess returns an empty gap list
                 - rules lacking deletion or non_fast_forward → DeleteOrForcePushAllowed (c-7 monitor)
                 - all three c-3 gaps present at once → three gaps returned, none collapsed
                 - RequiresPR: [] → false, [deletion] → false, [non_fast_forward] → false,
                   [pull_request] → true, [update] → true (the predicate the chore path routes on)

  t-4  Add Dependabot minor/patch auto-merge workflow
       files:    .github/workflows/dependabot-automerge.yml (new),
                 internal/cmd/dependabot_automerge_test.go (new), .github/dependabot.yml
       covers:   c-8, c-6
       desc:     pull_request_target workflow, guarded to dependabot[bot]; dependabot/fetch-metadata
                 pinned by SHA; `gh pr merge --auto --squash` only for semver-minor/patch.
                 Rewrite dependabot.yml's "No auto-merge (locked decision auto_merge)" note.
                 Before editing: read ~/.claude/memory/reference_ci_supply_chain_hardening.md and
                 audit the new file (global CI-hygiene rule); surface findings.
       contract: - trigger is `pull_request_target` only; switching to `pull_request` fails
                   TestAutoMergeIsNotARequiredCheck (its job would become a required check that
                   never runs on the workflow_dispatch'd pin-currency PR → c-6 blocks forever)
                 - no `actions/checkout` step anywhere in the file (pull_request_target + checkout
                   of PR code = pwn request) — adding one fails TestAutoMergeNeverChecksOutCode
                 - the merge step's `if:` names 'version-update:semver-minor' and
                   'version-update:semver-patch' and not 'semver-major'; an unconditional merge
                   step fails TestAutoMergeSkipsMajor (c-8 "major still manual")
                 - merge command carries `--auto` (waits for required checks); an immediate
                   `gh pr merge` without it fails TestAutoMergeWaitsForChecks
                 - job guard is `github.event.pull_request.user.login == 'dependabot[bot]'`
                 - workflow-level permissions read-only; job-level exactly contents: write +
                   pull-requests: write
                 - every dependabot.yml group's update-types ⊆ {minor, patch}; adding "major" to a
                   group fails TestDependabotGroupsExcludeMajor (a grouped major would auto-merge)
                 - existing TestWorkflowActionsArePinned and TestWorkflowsHaveNoExpressionsInRun
                   stay green over the new file (SHA pin; `${{ }}` reaches run: only via env:)

Wave 2 (depends t-1, t-2, t-3)
  t-5  gh-backed ruleset, repo-setting and auto-merge plumbing
       files:    internal/ship/rulesets.go (new), internal/ship/rulesets_test.go (new),
                 internal/ship/automerge.go (new), internal/ship/automerge_test.go (new)
       covers:   c-1, c-3, c-5, c-8, c-9
       desc:     Via screenedGH `gh api`: BranchRules, ListRulesets, GetRuleset, CreateRuleset,
                 UpdateRuleset, SetAllowAutoMerge; AutoMergePR(n, method) via `gh pr merge`.
                 Exported seams: BranchRulesFunc, ListRulesetsFunc, GetRulesetFunc,
                 CreateRulesetFunc, UpdateRulesetFunc, SetAllowAutoMergeFunc, AutoMergePRFunc,
                 OpenPRFunc (= OpenPR). owner/repo parsed from [remote].url and validated.
       depends:  t-2, t-3
       contract: - (ghCommand stub records argv+stdin) CreateRuleset → `api --method POST
                   repos/<o>/<r>/rulesets --input -` with stdin containing `"bypass_actors":[]`;
                   UpdateRuleset → PUT `.../rulesets/<id>`; a POST on update fails the test
                 - SetAllowAutoMerge(true) PATCH body is exactly {"allow_auto_merge":true}; any
                   other key (allow_merge_commit, allow_squash_merge…) fails
                   TestRepoPatchTouchesOnlyAutoMerge (c-4: the apply must not disable merge commits)
                 - BranchRules on gh exit 1 / unparseable JSON returns a non-nil error, never
                   (nil, nil) — nil rules would read as "unprotected" and route a direct push
                 - BranchRules against a gh stub that never exits returns a timeout error within
                   the (test-shortened) bound — doctor must stay usable offline
                 - AutoMergePR(7, "merge") argv == `pr merge 7 --auto --merge`; gh output "Merged"
                   → Merged=true (clean PR merged at once — still a PR merge, not a push);
                   "will be automatically merged" → AutoEnabled=true; "auto merge is not allowed"
                   → ErrAutoMergeUnavailable
                 - owner "-x" or a non-GitHub URL is refused before gh runs (argv fence)
                 - no exec.LookPath on these paths (external_cli_audit: a test must not depend on
                   gh being installed — helicon has none)
                 - request bodies pass secretscan.ScanPayload before gh is spawned

  t-6  Pin every ci.yml job as required
       files:    internal/cmd/ci_required_checks_test.go (new), internal/cmd/release_pipeline_test.go
       covers:   c-2, c-4, c-6
       desc:     Test-only guards over the live workflows: the c-2 test itself plus the workflow
                 properties that keep required checks honest for humans and bots.
       depends:  t-1, t-2
       contract: - TestEveryCIJobIsRequired: a test-local scan of ci.yml's `jobs:` children
                   (independent of protect.PullRequestJobs) must equal the contexts of
                   protect.MainRuleset("main", protect.PullRequestJobs(repoRoot)). No hand-kept
                   job list anywhere in the test. Red-proof: a temp copy of ci.yml with an
                   appended `extra:` job must surface "extra" in the ruleset; a PullRequestJobs
                   that dropped the last job fails the equality
                 - TestCIJobsHaveNoJobLevelIf: no job in a pull_request workflow carries a
                   job-level `if:` — a skipped required check reports success and lets a PR merge
                   unverified (the c-2 hole)
                 - TestCIPullRequestTriggerUnfiltered: no paths/paths-ignore/branches/
                   branches-ignore/types under ci.yml's pull_request — a filtered Dependabot PR
                   would leave required checks pending forever (c-6)
                 - TestCIReadsNoSecrets: ci.yml references no `secrets.` other than GITHUB_TOKEN —
                   Dependabot PRs get no Actions secrets (c-6)
                 - TestRequiredChecksAreDispatchable: every workflow contributing required
                   contexts also declares workflow_dispatch with no required inputs — the
                   pin-currency PR only ever gets dispatched runs (c-6)
                 - TestReleaseTagsFromMainOnly: release.yml still triggers on push to [main], and
                   every `git push` in it pushes "$tag" only — a branch push would hit the
                   empty-bypass ruleset and fail the release (c-4)

Wave 3 (depends wave 1–2)
  t-7  Route .dross chores through a chore PR
       files:    internal/cmd/chorepr.go (new), internal/cmd/basebranch.go,
                 internal/cmd/chorepr_test.go (new), internal/cmd/hermetic_env_test.go
       covers:   c-5, c-10, c-1
       desc:     pushBaseIfAheadDrossOnly: for provider github, probe BranchRulesFunc only when the
                 base has .dross-only commits ahead; RequiresPR → push base tip to
                 dross/chore/<base>, join the open PR on that head or open one, AutoMergePR(merge).
                 Unprotected / non-github → today's direct push. TestMain pins every new ship seam
                 to a counting stub that fails the binary if reached unstubbed.
       depends:  t-3, t-5
       contract: (fixture: basePushFixture + pre-receive hook that rejects and LOGS any update to
                  refs/heads/main or refs/heads/milestone/*; ship seams stubbed)
                 - protected, purely ahead → hook log has zero base updates; origin
                   dross/chore/main == local main tip; OpenPRFunc called once with head
                   dross/chore/main, base main; AutoMergePRFunc called with method "merge"
                   (a "squash" call fails TestChorePRMergesAsMergeCommit — squash strands the local
                   chores, the c-5 failure)
                 - FindOpenPRByHeadFunc returns #7 → no OpenPRFunc call, chore branch advanced to
                   the new tip, output names #7 (locked: new chores join the open PR)
                 - base ahead AND behind origin, .dross-only → chore PR still opened/joined:
                   `git rev-list main --not origin/main origin/dross/chore/main` is empty (nothing
                   stranded on one machine)
                 - AutoMergePRFunc → ErrAutoMergeUnavailable → returns nil, output carries the PR
                   URL and "merge it by hand" (locked: PR still opened, URL reported)
                 - BranchRulesFunc error → hard error naming the probe; hook log empty (no ref
                   pushed anywhere — never guess "unprotected")
                 - protected + code-ahead → refusal names the PR route and does not say "push
                   … manually"; zero pushes
                 - provider forgejo, or rules without pull_request → existing direct push;
                   every existing TestPushBase* stays green unmodified
                 - chore branch for base milestone/v1 does not match `refs/heads/milestone/*`
                   (path.Match) — else the chore branch would itself be protected
                 - suite-wide: the TestMain counting stub reports zero unstubbed seam calls

  t-8  Add `dross protect` preview/apply verb
       files:    internal/cmd/protect.go (new), internal/cmd/protect_test.go (new),
                 cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-9, c-1, c-7, c-10, c-8
       desc:     Builds both rulesets from protect.PullRequestJobs + git_main_branch; prints them;
                 `--apply` creates-or-updates by name, sets allow_auto_merge, then reads main back
                 through Assess. Exports ProtectFixHint = "dross protect --apply".
       depends:  t-1, t-2, t-3, t-5
       contract: - no flag → zero Create/Update/SetAllowAutoMerge calls on the recording seams;
                   output lists `refs/heads/main`, `refs/heads/milestone/*` and every job context
                   from the fixture's pull_request workflow (locked apply_verb: preview default)
                 - fixture with a pull_request_target workflow and release.yml → their jobs are
                   absent from the preview (built from pull_request jobs only)
                 - git_main_branch = "trunk" → preview/apply target `refs/heads/trunk`
                 - `--apply`, no rulesets exist → CreateRulesetFunc ×2 (names `dross: main`,
                   `dross: milestones`) + SetAllowAutoMergeFunc(true); exit 0 when read-back is
                   gap-free
                 - `--apply` with both names already listed → UpdateRulesetFunc by their ids,
                   zero Create calls (re-apply never duplicates)
                 - read-back stub missing a check → non-zero exit naming the missing context
                 - provider forgejo → error "github only", zero seam calls
                 - a matrix job in the workflows → error before any write call
                 - Protect().Flags().Lookup("apply") != nil and ProtectFixHint ==
                   "dross protect --apply"; renaming the flag fails here, renaming the verb fails
                   TestNarratedCommandsResolveAgainstTheTree (the hint literal lives in internal/cmd)
                 - cli_tree.txt golden updated; TestCLITreeGolden green

Wave 4 (depends wave 3)
  t-9  Prove ship and complete on protected main
       files:    internal/cmd/protected_base_test.go (new), internal/cmd/ship.go, internal/cmd/phase.go
       covers:   c-5
       desc:     End-to-end over a main-based phase (no milestone) with github provider, stubbed
                 seams and the logging hook; ship.go's phase-PR open goes through ship.OpenPRFunc;
                 phase complete's ff-abort names a pending chore PR instead of offering --recover.
       depends:  t-7
       contract: - `dross ship` with .dross chores ahead on main → hook log has zero
                   refs/heads/main updates, chore PR + phase PR both opened, phase marked shipped
                 - origin merges the phase PR (fixture update-ref), `dross phase complete` → zero
                   main updates in the hook log; the completion record reaches
                   origin dross/chore/main; after the fixture merges that chore PR as a merge
                   commit, fetch + `git rev-list origin/main..main` is empty (no .dross commit
                   stuck on local main)
                 - chore PR still open AND origin/main advanced → phase complete exits non-zero,
                   message names the chore PR number and "re-run after it merges", does not
                   mention --recover; local main unchanged; zero main updates
                 - same flow with an unprotected stub still pushes main directly (no regression)

  t-10 Route milestone head chores; keep finalize delete
       files:    internal/cmd/milestone.go, internal/cmd/milestone_protected_test.go (new)
       covers:   c-10
       desc:     pushMilestoneHeadIfAhead: protected milestone/<v> with .dross-only ahead commits
                 → chore PR (via t-7's helper), still opens the milestone PR; code-ahead → refuse.
                 milestone.go's ship.OpenPR call goes through ship.OpenPRFunc.
       depends:  t-7
       contract: (pre-receive hook rejects non-delete updates to refs/heads/milestone/*, allows
                  creation and deletion, logs all)
                 - `milestone complete` with .dross-only commits ahead on milestone/v1 → zero
                   milestone/v1 updates in the log, chore PR head dross/chore/milestone/v1 base
                   milestone/v1, milestone PR still opened, output says the chore PR must merge
                   before the milestone PR
                 - code commit ahead → error names "phase PR" as the route; nothing pushed; no
                   milestone PR opened
                 - `milestone complete --finalize` under the same hook → origin milestone/v1
                   deleted, exit 0 (finalize can still delete)
                 - `milestone create` (ensureMilestoneBranch) under the hook → new branch created
                   on origin (creation is not an update)

  t-11 Report main's live protection in doctor
       files:    internal/cmd/doctor_protection.go (new), internal/cmd/doctor.go,
                 internal/cmd/doctor_protection_test.go (new)
       covers:   c-3, c-9
       desc:     `Branch protection:` section: provider github → BranchRules + GetRuleset → Assess
                 against protect.PullRequestJobs; each gap one ⚠ line ending with
                 `Fix: \`dross protect --apply\``; forgejo/gitlab → `not checked (<provider>)`;
                 reader error → `unknown`. Gaps are warnings (exit code unchanged).
       depends:  t-1, t-3, t-5, t-7, t-8
       contract: - stub zero rules → line containing "unprotected" and "dross protect --apply"
                 - stub required {test} vs workflow jobs {test, shellcheck} → line naming
                   "shellcheck" as a missing required check, with the fix
                 - stub ruleset current_user_can_bypass "always" → "admin bypass allowed" line
                 - all three at once → three distinct lines
                 - BranchRulesFunc error → line contains "unknown" and the section has no ✓ line
                   (TestProtectionUnknownNeverReadsProtected — locked doctor_reach)
                 - provider forgejo → exactly "not checked (forgejo)", zero seam calls
                 - fully protected stub → one ✓ line naming `dross: main` and the check count
                 - doctor exit code identical with and without gaps
                 - TestDoctorRunGolden stays byte-identical (no [remote] → section silent)

  t-12 Narrate protected-base flows in prompts
       files:    assets/prompts/ship.md, assets/prompts/milestone.md, assets/prompts/quick.md,
                 internal/cmd/protected_prompts_test.go (new)
       covers:   c-5, c-10, c-1
       desc:     ship.md: chores go out as an auto-merge `.dross`-only chore PR on a protected base;
                 a pending chore PR means re-run complete after it merges. milestone.md: milestone/*
                 refuses pushes, chore PR merges before the milestone PR, finalize still deletes.
                 quick.md: standalone on a protected base → `quick/<slug>` branch + PR, never code
                 left on the base. `make install` after (r-01).
       depends:  t-7, t-8
       contract: - protected_prompts_test asserts ship.md names "chore PR" and the
                   re-run-after-merge step; milestone.md says the chore PR merges before the
                   milestone PR and that --finalize still deletes the branch; quick.md's standalone
                   bullet names `Branch protection` (doctor) and `quick/` + PR, and no longer says
                   unconditionally "go to that base directly"
                 - TestShipPromptCommandsExist resolves every `dross …` ship.md now narrates
                   (incl. `dross protect`)

Wave 5 (depends all)
  t-13 Protect Rivil/dross with the verb; prove live
       files:    — (no repo files; mutates Rivil/dross rulesets + allow_auto_merge)
       covers:   c-1, c-2, c-3, c-4, c-6, c-7, c-8, c-9, c-10
       desc:     `make install`; `dross protect` preview shown to the user (pair gate); then
                 `dross protect --apply`; then the live probes below. Evidence (exit codes, field
                 values — no tokens) goes in the task commit message.
       depends:  t-1 … t-12
       contract: - `dross doctor` Branch protection section: one ✓, no ⚠, no "unknown" (c-3, c-9)
                 - `gh api repos/Rivil/dross/rulesets/<id>` reports current_user_can_bypass
                   "never" for both rulesets, run as the admin (c-1, c-7 "admin included")
                 - `gh api repos/Rivil/dross/rules/branches/main` lists deletion,
                   non_fast_forward, pull_request and required_status_checks whose contexts equal
                   ci.yml's jobs (c-2, c-7)
                 - `gh api repos/Rivil/dross` → allow_auto_merge true, allow_merge_commit true
                   (c-8, c-4)
                 - admin push of an empty commit to main is refused (GH013) — the only main
                   push probe; no force/delete probe against main (c-1)
                 - throwaway `milestone/zz-protect-probe`: creation at origin/main's sha succeeds,
                   a direct push of a new commit to it is refused (GH013), deletion succeeds (c-10)
                   — if creation is refused, ensureMilestoneBranch breaks at next scope: STOP and
                   raise it, do not close the task
                 - re-running `dross protect --apply` issues updates, not new rulesets
                   (`gh api …/rulesets` count stays 2)
                 - any open Dependabot / pin-currency PR shows all required contexts reported on
                   its head commit; if none is open, record c-6/c-8 live as unobserved
```

## Coverage

| Criterion | Tasks | Static contract | Live contract |
|---|---|---|---|
| c-1 | t-2, t-5, t-7, t-8, t-12, t-13 | t-2 `"bypass_actors":[]` + pull_request rule | t-13 bypass "never", refused admin push |
| c-2 | t-1, t-2, t-6, t-13 | t-6 TestEveryCIJobIsRequired (ci.yml-derived, independent scan), no job-level `if:` | t-13 rules/branches contexts == ci.yml jobs |
| c-3 | t-3, t-5, t-11, t-13 | t-3 Assess table, t-11 doctor lines incl. unknown | t-13 doctor clean on Rivil/dross |
| c-4 | t-2, t-5, t-6, t-13 | merge in allowed_merge_methods, no linear history; PATCH touches only allow_auto_merge; release pushes only tags | t-13 allow_merge_commit true |
| c-5 | t-3, t-5, t-7, t-9, t-12 | hook-logged zero main updates; merge-commit auto-merge; rev-list empty after merge | (dogfood: this phase's own ship/complete) |
| c-6 | t-2, t-4, t-6, t-13 | strict=false, integration_id, unfiltered PR trigger, no secrets, dispatchable, auto-merge not a required check | t-13 open bot PR checks reported (or unobserved) |
| c-7 | t-2, t-3, t-13 | deletion + non_fast_forward on main, explicit empty bypass | t-13 rules/branches + bypass "never" |
| c-8 | t-4, t-5, t-8, t-13 | auto-merge workflow shape (minor/patch only, --auto) | t-13 allow_auto_merge true |
| c-9 | t-1, t-5, t-8, t-11, t-13 | verb preview/apply/idempotence, trunk targeting, hint resolves | t-13 applied by the verb itself |
| c-10 | t-2, t-7, t-10, t-12, t-13 | milestone ruleset has no deletion; hook-logged chore PR; finalize deletes | t-13 throwaway milestone/* probe |

All 10 criteria covered.

## Judgment calls

- Auto-merge workflow on `pull_request_target`. I rejected two alternatives. On `pull_request`, its job would become a required check that never runs on the dispatched pin-currency PR, which breaks c-6. As a job in ci.yml it would need a job-level `if:`, and a skipped required check reads as a pass.
- For c-2, the required list is derived from the workflows by the builder. The test checks it against a second, independent scan of ci.yml, and doctor catches live drift. I rejected a committed ruleset snapshot, which is a hand-kept list under another name. I also rejected an "all-green" aggregator job, which conflicts with locked doctor_reach and with c-9's "built from the repo's pull_request jobs".
- Chore PRs auto-merge with a merge commit. Squash and rebase both create new SHAs, and new SHAs leave the local base's chores stuck (c-5). For the same reason the main ruleset's allowed_merge_methods is [merge, squash]: rebase is excluded.
- If a chore PR is still pending when origin moves, `phase complete` refuses, names the PR and says to re-run. I rejected rebasing the local base automatically, because that rewrites commits already in an open PR. With the merge-commit method, the wait resolves itself.
- The protection probe is a live `rules/branches/<base>` read, and it runs only when the base is ahead. A probe error is a hard error. I rejected two alternatives: push first and fall back on rejection (locked chore_push says "never as a direct push"), and a config flag (it would drift from the live settings).
- The milestone ruleset has only the pull_request rule. A deletion rule would break finalize. A required_status_checks rule would block the scope-time branch creation and isn't asked for by c-10. I have not verified that GitHub lets a branch be created under the pull_request rule, so t-13 probes it live and has a stop condition.
- Doctor protection gaps are warnings, not issues. As issues, doctor would go red in every GitHub repo that hasn't run the verb, and that would block `/dross-ship`'s doctor gate. An `unknown` result never renders ✓, which honours doctor_reach.
- Doctor gets a fourth gap, "delete/force-push not blocked", on top of c-3's three. Without it c-7 has nothing watching it live.
- The workflow parser is a line scanner in internal/protect, with no new dependency. The repo has no YAML dependency (precedent: ci_concurrency_test), and internal/cmd may not import parsers.
- GitHub API calls go through gh in internal/ship, using the existing exec seam and secret screen. I rejected forge.GitHubClient REST: its config and errors are shaped around `[board]`, and ship's GitHub path already uses only gh, so this keeps one auth story.
- The live proof for c-1/c-7 uses the API (current_user_can_bypass=never, rules/branches) plus one refused empty-commit push to main. I rejected force-push and delete probes against main: if protection were broken, the force push would rewrite main. GitHub always refuses deleting the default branch, so a delete probe would prove nothing.
- I included the quick.md change under c-1. Without it, a standalone quick task on a protected base leaves a code commit on the base that can never be pushed. This is a direct consequence of c-1, not a separate capability.
- I left out a release-version guard on chore PRs into main. Without it, a scope-time major.minor bump in a chore would tag a release when the PR merges. Direct push has the same exposure today and this phase doesn't change it. The judge may promote it into t-7.
- Dependabot auto-merge uses squash, which matches the repo's convention for phase PRs. A merge made with GITHUB_TOKEN triggers no push workflows, so release.yml doesn't run, which is harmless because bot merges don't change [project].version. I've recorded this rather than fixed it.
- t-12 (prompts) sits in wave 4 alongside t-10. It describes the behaviour the spec locks, not t-10's code, so it doesn't strictly need t-10's output.
