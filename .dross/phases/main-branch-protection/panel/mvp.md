# mvp — main-branch-protection

Lens: smallest task set that satisfies every criterion. Each task traces to a criterion.

```
Phase main-branch-protection — 6 tasks across 3 waves

Wave 1
  t-1  Build rulesets from pull_request workflow jobs
       files:    internal/ship/ruleset.go (new), internal/ship/ruleset_test.go (new),
                 internal/cmd/branch_protection_test.go (new)
       covers:   c-1, c-2, c-4, c-6, c-7, c-10
       desc:     Pure code, no network. PRCheckNames(repoDir) scans .github/workflows/*.{yml,yaml}
                 line by line (the repo has no YAML dep). It handles the on: scalar, list and map
                 forms, and keeps only workflows triggered on `pull_request` (pull_request_target
                 does not count). It returns their check names: the job's `name:` if set, else
                 the job id. A matrix job is refused with an error that names it.
                 DesiredRulesets(mainBranch, checks) returns two payloads:
                 "dross: main" on refs/heads/<main> has bypass_actors [], deletion,
                 non_fast_forward, pull_request (0 approvals, merge allowed) and
                 required_status_checks (strict=false, one context per check, no integration_id).
                 "dross: milestone branches" on refs/heads/milestone/* has bypass_actors [] and
                 pull_request only.
                 The live test sweeps the real .github/.
       contract: - Unit fixtures: a push-only workflow and a pull_request_target workflow add no
                   checks. `on: [push, pull_request]` is detected. A `name:` override replaces
                   the job id. A `strategy: matrix` job returns an error naming the job.
                 - The main payload fails the test if bypass_actors is non-empty, if deletion or
                   non_fast_forward is missing (c-7), if any rule is required_linear_history, or
                   if pull_request.allowed_merge_methods leaves out "merge" (c-4).
                 - The milestone payload fails the test if it carries a deletion rule (finalize
                   must still delete, c-10) or lacks a pull_request rule (c-10).
                 - Live (c-2): branch_protection_test reads ci.yml's indent-2 job keys with its
                   own scan, not a hand-kept list. It fails if any of them is missing from
                   DesiredRulesets("main", PRCheckNames(repo)). A bite subtest appends a `lint:`
                   job to a copy of ci.yml and asserts it is required.
                 - Live (c-6): fails if any pull_request-triggered workflow lacks a
                   workflow_dispatch trigger (the pin-currency bump PR's only CI path), if ci.yml's
                   pull_request trigger gains a branches: or paths: filter (a filtered-out
                   Dependabot PR never gets its required check), or if strict is true (that would
                   force hand rebases).
                 - Live (c-4): fails if release.yml stops triggering on `push: branches: [main]`
                   or pushes anything other than a tag.

  t-2  Add GitHub protection read/apply plumbing via gh
       files:    internal/ship/protection.go (new), internal/ship/protection_test.go (new),
                 internal/cmd/hermetic_env_test.go
       covers:   c-3, c-5, c-8, c-9, c-10
       desc:     GitHub only, through screenedGH `gh api`:
                 - GetBranchProtection(opts, branch) reads
                   GET repos/{owner}/{repo}/rules/branches/<b>, then each ruleset_id's
                   bypass_actors. It returns RequiresPR, RequiredChecks, BypassAllowed and
                   BypassKnown.
                 - ApplyRuleset(opts, name, payload any) issues PUT when a ruleset with that name
                   exists and POST otherwise.
                 - SetRepoAutoMerge(opts) sends PATCH allow_auto_merge=true.
                 - EnableAutoMerge(opts, n) runs `gh pr merge <n> --auto --squash`.
                 Exported seams: GetBranchProtectionFunc and EnableAutoMergeFunc. A non-GitHub
                 provider returns ErrProtectionNotChecked; a failed or unauthenticated gh returns
                 an error wrapping ErrProtectionUnknown. TestMain installs non-dialing defaults
                 for both seams, so no cmd test can reach gh.
       contract: - A ghCommand double proves that the rules response with a pull_request rule and
                   required_status_checks contexts [test, shellcheck] yields RequiresPR=true and
                   RequiredChecks=[test shellcheck].
                 - A gh exit 1 (or a 401/404 body) returns errors.Is(err, ErrProtectionUnknown)
                   and never RequiresPR=false with a nil error. That is the false-green guard in
                   doctor_reach.
                 - A ruleset body with no bypass_actors key gives BypassKnown=false. One with
                   [{actor_type:RepositoryRole}] gives BypassAllowed=true.
                 - ApplyRuleset sends `-X PUT …/rulesets/<id>` when the listing holds the same
                   name, and `-X POST …/rulesets` when it does not.
                 - EnableAutoMerge's argv carries --auto.
                 - Provider "forgejo" returns ErrProtectionNotChecked without running gh.
                 - TestHermeticBranchProtection_NeverDials fails if TestMain stops replacing
                   either seam.

  t-4  Auto-merge Dependabot minor/patch PRs
       files:    .github/workflows/dependabot-auto-merge.yml (new), .github/dependabot.yml,
                 internal/cmd/dependabot_automerge_test.go (new)
       covers:   c-8, c-6
       desc:     New workflow. Trigger: pull_request_target only. Workflow permissions:
                 contents: read. One job, guarded on
                 github.event.pull_request.user.login == 'dependabot[bot]', with job-level
                 contents: write and pull-requests: write. It runs dependabot/fetch-metadata
                 (pinned by SHA with a `# vX.Y.Z` comment), then `gh pr merge --auto --squash`
                 when steps.meta.outputs.update-type != 'version-update:semver-major'.
                 It has no checkout step. A header comment says the workflow needs
                 allow_auto_merge, which `dross protect --apply` sets.
                 Rewrite dependabot.yml's stale "No auto-merge (locked decision auto_merge)"
                 paragraph.
                 Per CLAUDE.md, audit this workflow against
                 ~/.claude/memory/reference_ci_supply_chain_hardening.md first.
       contract: dependabot_automerge_test fails if:
                 - the trigger is anything but pull_request_target (on pull_request it would
                   become a required check the GITHUB_TOKEN-opened pin-currency PR never runs,
                   which breaks c-6)
                 - the dependabot[bot] guard is missing
                 - the merge step is not gated on the semver-major exclusion (majors must stay
                   manual)
                 - `gh pr merge` lacks --auto
                 - any actions/checkout step appears (pull_request_target must never check out
                   PR code)
                 - the workflow-level permissions are not read-only
                 Separately, action_pins_test fails on an unpinned fetch-metadata.

Wave 2 (t-3 depends t-1, t-2; t-5 depends t-2)
  t-3  Add `dross protect` verb and doctor protection section
       files:    internal/cmd/protect.go (new), internal/cmd/protect_test.go (new),
                 internal/cmd/doctor.go, cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-9, c-3, c-1, c-2, c-7, c-8, c-10
       desc:     `dross protect [--apply]`:
                 - Builds DesiredRulesets(project repo.git_main_branch,
                   PRCheckNames(repoDir)).
                 - Without --apply, prints both payloads and "would enable allow_auto_merge",
                   and writes nothing.
                 - With --apply, calls ApplyRuleset ×2 and then SetRepoAutoMerge.
                 - A non-GitHub provider is refused.
                 ProtectionSection lives in protect.go and doctor.go calls it as an advisory
                 block:
                 - Live main rules vs PRCheckNames. The gaps are "unprotected (no PR rule)",
                   "required check <job> missing" and "admin bypass allowed". Each one names
                   `dross protect --apply` as the fix.
                 - ErrProtectionUnknown prints `unknown`, never ✓.
                 - ErrProtectionNotChecked prints `not checked (<provider>)`.
                 - The block never changes doctor's exit code.
                 Register the verb in main.go and regenerate the CLI golden.
       contract: - A preview run with an ApplyRuleset counting stub makes 0 calls (the apply_verb
                   gate).
                 - --apply on a fixture with git_main_branch="trunk" sends a main payload
                   targeting refs/heads/trunk whose required contexts equal the fixture's
                   pull_request job names, plus exactly one SetRepoAutoMerge call.
                 - Doctor with GetBranchProtectionFunc stubbed:
                   - RequiresPR=false prints a ⚠ line containing "unprotected" and
                     "`dross protect --apply`".
                   - RequiredChecks missing "shellcheck" prints a line naming shellcheck.
                   - BypassAllowed=true prints "admin bypass allowed".
                   - ErrProtectionUnknown prints "unknown" and no ✓ line.
                   - Provider forgejo prints "not checked (forgejo)".
                   - The exit code is unchanged in every case.
                 - TestNarratedCommandsResolveAgainstTheTree fails if the verb is unregistered
                   while doctor narrates it.

  t-5  Route protected-base .dross chores through auto-merge PR
       files:    internal/cmd/chorepr.go (new), internal/cmd/chorepr_test.go (new),
                 internal/cmd/basebranch.go, internal/cmd/milestone.go
       covers:   c-5, c-10
       desc:     pushBaseIfAheadDrossOnly resolves project and OpenOpts from repoDir. When
                 GetBranchProtectionFunc reports RequiresPR for <base>, the .dross-only ahead
                 commits go to publishChores:
                 - Push them to origin `dross-chores/<base>`. When an open chore PR exists
                   (FindOpenPRByHeadFunc), they are cherry-picked onto its branch instead.
                 - Open a PR only when none is open.
                 - Call EnableAutoMergeFunc. If that fails, print the PR URL and "auto-merge
                   unavailable" and return nil.
                 - Move local <base> back to origin/<base>: update-ref when it is not checked
                   out, `reset --keep` when it is.
                 Unprotected bases, non-GitHub providers and unknown protection keep today's
                 direct push.
                 pushMilestoneHeadIfAhead's purely-ahead arm takes the same path when the
                 milestone branch is protected.
                 Callers in ship.go, phase.go and ship_recover.go are unchanged; they all funnel
                 through pushBaseIfAheadDrossOnly.
       contract: The bare origin has a pre-receive hook that rejects refs/heads/main and
                 refs/heads/milestone/*, and GetBranchProtectionFunc is stubbed to protected.
                 - `dross ship` on a main-based phase with a .dross chore on local main exits 0.
                   Origin main's sha is unchanged, origin dross-chores/main holds the chore, and
                   `rev-list origin/main..main` is empty (c-5).
                 - `dross phase complete` on a merged main-based phase exits 0, its completion
                   record lands on dross-chores/main, and local main == origin/main (c-5).
                 - A second chore, with FindOpenPRByHeadFunc returning #7, is appended to the
                   same chore branch, and the mock records 0 new PR POSTs.
                 - EnableAutoMergeFunc returning an error still exits 0 and prints the PR URL.
                 - `dross milestone complete` with a .dross-only ahead milestone head pushes
                   nothing to refs/heads/milestone/<v> and opens dross-chores/milestone/<v>
                   (c-10).
                 - A code-ahead protected base still refuses and pushes nothing.
                 - With the stub reporting unprotected, TestPushBaseDrossOnlyAheadPushes still
                   pushes directly.

Wave 3 (depends t-3, t-5)
  t-6  Protect Rivil/dross with `dross protect --apply`
       files:    (none — changes live GitHub settings on Rivil/dross; no tracked file)
       covers:   c-1, c-2, c-7, c-8, c-10
       desc:     1. `make install` (r-01). The chore path must be live before milestone/* is
                    protected, or this phase's own bookkeeping push to milestone/v1.7 is
                    refused.
                 2. Run `dross protect` and show the preview to the user. That is the pair gate.
                 3. `dross protect --apply`, then `dross doctor`.
                 4. Probe c-10 with a throwaway `milestone/protect-probe`: create it, attempt a
                    direct commit push (expect rejection), then `git push --delete` it (expect
                    success).
                 Never probe main with a real push, because main auto-releases.
       contract: - `dross doctor` on this repo prints main protection with zero gaps (no
                   "unprotected", "missing" or "admin bypass" line, no "unknown").
                 - `gh api repos/Rivil/dross/rules/branches/main` lists pull_request,
                   required_status_checks with contexts {test, mutation-ts, goreleaser-check,
                   shellcheck}, non_fast_forward and deletion.
                 - `gh api repos/Rivil/dross --jq .allow_auto_merge` prints true.
                 - The probe's commit push is refused with GH013 and its delete succeeds. If the
                   probe's creation push is refused, `dross milestone create` is broken under
                   the ruleset. That is a blocking finding, not a pass.
```

## Coverage

| Criterion | Tasks | How |
|---|---|---|
| c-1 no direct push to main, admin included | t-1, t-3, t-6 | The main payload has a pull_request rule and empty bypass_actors (t-1). The verb applies it (t-3). It is live on Rivil/dross and doctor shows no bypass gap (t-6). |
| c-2 every ci.yml job required; test reads ci.yml | t-1, t-3, t-6 | The live test compares ci.yml's own job keys with the built required contexts, with a bite subtest (t-1). The verb applies them (t-3) and they are live (t-6). |
| c-3 doctor reports live protection + names gaps | t-2, t-3 | The live read separates unknown from not-checked (t-2). The doctor section produces the three gap lines (t-3). |
| c-4 milestone merge commits + release tags from main | t-1 | Tests guard that the payload has no linear-history rule and allows merge, and that release.yml still tags on push to main. |
| c-5 ship/complete on a main-based phase: no direct push, nothing stuck locally | t-5 | The chore-PR publisher plus a local base reset, tested with a hook that rejects refs/heads/main. |
| c-6 Dependabot + pin-currency PRs can pass required checks hands-free | t-1, t-4 | t-1: strict=false, every PR workflow can be dispatched, the pull_request trigger is unfiltered. t-4: the auto-merge job stays off pull_request, so the bump PR is never waiting on it. |
| c-7 main can't be force-pushed or deleted, admin included | t-1, t-6 | The payload carries deletion and non_fast_forward with empty bypass (t-1), and the live rules API confirms it (t-6). |
| c-8 Dependabot minor/patch auto-merge, majors manual | t-4, t-2, t-3, t-6 | The workflow and its shape test (t-4). allow_auto_merge is set by --apply (t-2 plumbing, t-3 verb, t-6 live). |
| c-9 verb applies ruleset from PR jobs to configured main; doctor names it | t-1, t-3 | The builder works from PR jobs (t-1). `dross protect` targets git_main_branch, and the doctor gap lines name it (t-3). |
| c-10 milestone/* refuses direct pushes, bookkeeping via chore PR, finalize can delete | t-1, t-5, t-6 | The milestone payload has a pull_request rule and no deletion rule (t-1). Milestone bases and the milestone head go through the chore path (t-5). The live probe pushes and deletes a throwaway branch (t-6). |

## Judgment calls

- **Doctor gaps are advisory.** I chose ⚠ lines that never change the exit code. I rejected ✗ issues because /dross-ship and /dross-review gate on doctor and doctor_reach covers any GitHub repo, so every unprotected user repo would stop shipping.
- **Dependabot auto-merge runs on `pull_request_target` in its own workflow.** On `pull_request` its job becomes a required check that the GITHUB_TOKEN-opened pin-currency PR never runs, which breaks c-6. I also rejected putting it in ci.yml, which would add write scopes to a read-only workflow.
- **Two rulesets, main and milestone/\*.** One ruleset can't forbid deletion on main and still let `--finalize` delete milestone branches.
- **The milestone/\* ruleset has only a pull_request rule.** I rejected adding required checks and non_fast_forward to it, because c-10 asks only that direct pushes are refused.
- **Protection is detected through the `rules/branches/<b>` API.** I rejected push-then-catch-GH013 because c-5 says "without pushing directly", and an attempted push is still an attempt. When protection is unknown, dross falls back to today's direct push, which GitHub rejects as a hard error. That avoids a new refusal path.
- **Chore commits go to `dross-chores/<base>` with squash auto-merge, and local base is reset to origin afterwards.** I rejected leaving the commits on local base until the merge. c-5 forbids stuck commits, and the next phase squash-merge would turn them into a divergence. The cost is that local `.dross` lags until the chore PR merges.
- **The safety-net signature stays the same.** pushBaseIfAheadDrossOnly resolves project and OpenOpts from repoDir, so ship.go, phase.go and ship_recover.go don't change and every bookkeeping push funnels through one dispatch.
- **The verb and the doctor section are one task with 5 files.** They share the desired-vs-live comparison, and doctor's narration of `dross protect --apply` needs the verb registered (TestNarratedCommandsResolveAgainstTheTree). Splitting them would cost a wave for a one-line call in doctor.go.
- **The shape guards for c-2, c-4, c-6, c-7 and c-10 share t-1's live test file.** I rejected one workflow test file per criterion.
- **The verb is a top-level `dross protect --apply`.** It mirrors `dross remote bootstrap --apply`. I rejected nesting it under `dross remote`, because that tree authorizes remote execution hosts, not forge settings.
- **Matrix jobs are refused by the reader, not guessed.** ci.yml has none, and a guessed check name would be a silently unenforced requirement.
- **Required contexts are not pinned to an integration_id.** That keeps the workflow_dispatch runs on the pin-currency bump branch able to satisfy them (c-6).
- **Doctor does not read classic branch protection.** protection_mechanism locks rulesets. A classic-protected repo reads as an advisory "unprotected" whose fix is the verb.
- **No README, man page or prompt task.** No criterion needs one. The README parity test only rejects advertised commands that don't exist, so an unlisted verb passes.
- **The dogfood is its own last task with no tracked files.** apply_verb requires this repo to be protected by the verb. It must run after t-5 is installed (r-01), or this phase's own bookkeeping push to milestone/v1.7 is refused.
- **Open risk.** I haven't verified whether a ruleset pull_request rule refuses *creating* a milestone/* branch, which ensureMilestoneBranch does. t-6's probe tests creation explicitly, and a refusal is a blocking finding rather than a pass.
