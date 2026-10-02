# main-branch-protection — panel synthesis

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | 5: all 10 covered; each failure mode has one owning task; says plainly that c-8 can't be watched live | 3: all 10 on paper, but with no prompt task, quick.md's "go to that base directly" strands code on a protected milestone/* (c-1/c-10); also skips the README update the project requires | 5: all 10, each with a static and a live contract; leaves out the release-tag guard on purpose |
| Test-contract specificity | 5: names the breaking surface throughout (hook log, argv, `[]` vs null, parity with release-version.sh); a few cases have no test name | 4: concrete fixtures and payload checks; the c-5 contract misses the case where the chore PR is pending and origin has moved; its integration_id reasoning is wrong (see D10) | 5: sharpest (named tests, red-proofs, evaluate mode, current_user_can_bypass, read-back after apply); one contract is wrong: `pr merge 7 --auto --merge` puts a derived positional where argfence's gh policy requires a `--` before it |
| Granularity | 4: mostly 2–4 files; t-10 and t-12 reach 5 files, and t-12 spans diag, cmd and README; the three wave-1 gh tasks are thin | 2: t-1 bundles the parser, the builder and three live guards (6 criteria); t-3 is the verb plus doctor in 5 files; t-5 does base and milestone routing in one task | 4: a clean pure core and a test-only guard task; t-5 (8 seams) and t-7 (publish plus route, 9 contracts) are heavy |
| Wave correctness | 5: every edge is a real data dependency; independent gh seams run in parallel in wave 1 | 4: the edges are correct, but coarse tasks make coarse waves | 4: t-5 holds the dependency-free auto-merge seam back to wave 2; the t-11→t-7 edge is spurious |

**Skeleton: risk.** It has the best wave graph. It is also the only draft whose task list already holds every task the merge needs: the release-tag guard, separate publish and route steps, the ship_recover narration and `--check`. Grafted from verification: its contracts, the pure `Assess` layering (which splits risk's 5-file doctor task), and the read-back after `--apply`. Grafted from mvp: its live c-4 and c-6 workflow guards.

## Merged plan

```
Phase main-branch-protection — 16 tasks across 5 waves

Wave 1
  t-1  Scan pull_request workflow jobs  [risk+verification+mvp]
       files:    internal/protect/workflows.go (new), internal/protect/workflows_test.go (new)
       covers:   c-2, c-9
       depends:  —
       desc:     Pure line scanner, stdlib only, over {path: content}, so the verb can feed it
                 origin/<main>'s tree (D7). It keeps workflows whose `on:` carries pull_request
                 and gives each job the context `name:`, or the job id when there is none. Any job
                 whose check name can't be known statically is refused by name.
       contract: - If pull_request_target prefix-matches pull_request, TestPullRequestTargetExcluded
                   fails (`on: [push, pull_request_target]` and the t-5 shape must yield 0 jobs).
                 - If the scalar, flow-list, block and quoted-"on" forms yield different job sets,
                   the form-parity table fails.
                 - If `name: Unit tests` yields the id instead of "Unit tests", the context test fails.
                 - A matrix job, a job-level `uses:`, `name: ${{ … }}`, or a pull_request trigger
                   with paths/branches filters must error, naming the workflow and job. If any
                   yields a context instead, the refusal table fails.
                 - A `run: |` line `  fake:` at job-key indent, a `# jobs:` comment, or a step-level
                   `name:` must add no job. If one does, the false-job table fails.

  t-2  Build main and milestone rulesets  [verification+risk+mvp]
       files:    internal/protect/ruleset.go (new), internal/protect/ruleset_test.go (new)
       covers:   c-1, c-4, c-6, c-7, c-9, c-10
       depends:  —
       desc:     MainRuleset(branch, contexts) builds "dross: main": deletion, non_fast_forward,
                 pull_request (0 approvals), required_status_checks (strict=false, no integration_id,
                 D10). MilestoneRuleset() builds "dross: milestones" on refs/heads/milestone/*,
                 pull_request only (D6). Both are target=branch, enforcement=active, bypass_actors=[].
       contract: - If bypass_actors marshals as null or is omitted, TestBypassActorsExplicitlyEmpty
                   fails for both rulesets (an omitted key lets a PUT keep a stale admin bypass).
                 - If main gains required_linear_history or merge_queue, or allowed_merge_methods
                   stops being exactly ["merge","squash"], TestMainRulesetKeepsMergeCommits fails (c-4).
                 - If deletion or non_fast_forward is dropped, TestMainRulesetBlocksForcePushAndDelete
                   fails (c-7).
                 - If required_approving_review_count != 0 or strict is true, the params test fails.
                 - If MainRuleset("main", nil) returns a ruleset instead of an error, or "trunk"
                   doesn't yield include refs/heads/trunk, the builder test fails.
                 - If the milestone ruleset gains a deletion rule,
                   TestMilestoneRulesetAllowsFinalizeDelete fails.
                 - If either ruleset includes refs/tags/*, ~ALL, dependabot/** or pin-currency/bump,
                   the include test fails (release.yml pushes tags; bot branches stay pushable).

  t-3  Assess live branch rules for gaps  [verification; risk t-12 classifier]
       files:    internal/protect/assess.go (new), internal/protect/assess_test.go (new)
       covers:   c-3, c-5, c-7
       depends:  —
       desc:     LiveRule and LiveRuleset types (the GET rules/branches/{b} and rulesets/{id} shapes).
                 Assess(rules, rulesets, wantContexts) returns []Gap: Unprotected, MissingCheck,
                 StaleCheck, AdminBypass, BypassUnknown, ForcePushAllowed, DeletionAllowed.
                 RequiresPR(rules) is the predicate the chore router branches on.
       contract: - Zero rules, or a ruleset in "evaluate" or "disabled" enforcement, must return
                   exactly [Unprotected]. Otherwise the enforcement table fails.
                 - Want {test, shellcheck} with required {test} must yield MissingCheck(shellcheck).
                   A required context with no job must yield StaleCheck (it never reports, so every
                   PR blocks). Otherwise the check-diff test fails.
                 - Non-empty bypass_actors, or current_user_can_bypass of always or
                   pull_requests_only, must yield AdminBypass. An absent bypass_actors key must
                   yield BypassUnknown; if it yields no gap, TestUnreadableBypassIsUnknown fails.
                 - If all the c-3 gaps present at once come back as fewer gaps, the multi-gap test
                   fails.
                 - RequiresPR must be false for [non_fast_forward] or [deletion] alone, and true for
                   pull_request, update or required_status_checks. Otherwise the predicate table
                   fails.

  t-4  Project release tag from project.toml bytes  [risk]  (D3)
       files:    internal/project/release.go (new), internal/project/release_test.go (new)
       covers:   c-4, c-5
       depends:  —
       desc:     ReleaseTag(src) returns "vX.Y.Z" from [project].version, the same projection
                 release.yml makes through scripts/release-version.sh. t-9 uses it to refuse a
                 chore batch that would cut a tag.
       contract: - If ReleaseTag disagrees with `sh scripts/release-version.sh` on any of 5 fixtures
                   (4-part, 3-part, a `version` under [stack] before [project], a missing key, a
                   trailing comment), the parity test fails.
                 - 1.7.19.0 → 1.7.19.1 must project the same tag and 1.7.19.1 → 1.8.0.0 a
                   different one. Otherwise the bump table fails.

  t-5  Add Dependabot minor/patch auto-merge workflow  [mvp+verification+risk]  (D2)
       files:    .github/workflows/dependabot-automerge.yml (new), .github/dependabot.yml,
                 internal/cmd/dependabot_automerge_test.go (new)
       covers:   c-8, c-6
       depends:  —
       desc:     Trigger pull_request_target only. Permissions: workflow contents: read; job
                 contents: write and pull-requests: write. Guarded on the PR author being
                 dependabot[bot]. fetch-metadata pinned by SHA; `gh pr merge --auto --squash` for
                 minor/patch only; no checkout. Rewrite dependabot.yml's "No auto-merge" paragraph.
                 Audit it against ~/.claude/memory/reference_ci_supply_chain_hardening.md first.
       contract: - If the trigger becomes pull_request, TestAutoMergeIsNotARequiredCheck fails.
                 - If any actions/checkout step appears, TestAutoMergeNeverChecksOutCode fails.
                 - If the merge step's `if:` admits semver-major or has no update-type condition,
                   TestAutoMergeSkipsMajor fails.
                 - If `gh pr merge` lacks --auto, TestAutoMergeWaitsForChecks fails.
                 - The shape test fails if the guard is missing or keys on github.actor alone, or if
                   the workflow-level permissions aren't read-only.
                 - If any dependabot.yml group's update-types gains "major",
                   TestDependabotGroupsExcludeMajor fails.
                 - If fetch-metadata is unpinned or `${{` reaches run:, TestWorkflowActionsArePinned
                   or TestWorkflowsHaveNoExpressionsInRun fails on the new file.

  t-6  Add auto-merge and PR-open seams  [risk; verification t-5]
       files:    internal/ship/automerge.go (new), internal/ship/automerge_test.go (new),
                 internal/ship/open.go
       covers:   c-5, c-8
       depends:  —
       desc:     AutoMergePR(opts, n, method) runs `gh pr merge --auto --<method> -- <n>` through
                 screenedGH. Adds the AutoMergePRFunc and OpenPRFunc seams (OpenPRFunc = OpenPR)
                 so internal/cmd tests can open and arm PRs without gh.
       contract: - The argv test fails if argv isn't `pr merge --auto --merge -- <n>` with flags
                   ahead of `--` (argfence's gh Separator policy, internal/argfence/policy.go:72), or
                   if it ever carries --admin.
                 - The outcome table fails unless gh output maps as follows: "will be automatically
                   merged" → AutoEnabled; "Merged" → Merged=true; "auto merge is not allowed" or a
                   clean-status refusal → ErrAutoMergeUnavailable carrying gh's reason.
                 - If OpenPRFunc stops defaulting to OpenPR, the default-seam test fails.

Wave 2
  t-7  Read live rules and write rulesets via gh  [verification t-5+risk t-3+risk t-4+mvp t-2]
       files:    internal/ship/rulesets.go (new), internal/ship/rulesets_test.go (new),
                 internal/cmd/hermetic_env_test.go
       covers:   c-1, c-3, c-5, c-8, c-9
       depends:  t-2, t-3
       desc:     Through screenedGH `gh api`: BranchRules returns protect.LiveRule* or
                 Unknown{reason}; UpsertRuleset PUTs when the name matches and POSTs otherwise, body
                 on stdin; SetAllowAutoMerge. TestMain stubs BranchRulesFunc, the write seams and
                 AutoMergePRFunc with non-dialing stubs. OpenPRFunc keeps its OpenPR default.
       contract: - A gh exit of 401/403/404 or "gh missing", or unparseable JSON, must return
                   Unknown with the reason, never (nil, nil) or Known with zero rules. Otherwise
                   the unknown table fails.
                 - If a gh stub that never exits isn't cut off within the (shortened) bound, the
                   timeout test fails.
                 - The argv-fence test fails if milestone/v1.7 reaches the API path unescaped, or if
                   an owner/repo starting with `-` or containing `..` reaches gh.
                 - An existing "dross: main" must get exactly one PUT and zero POSTs. A 403 on the
                   list call must error naming the admin permission, not fall through to a blind
                   POST. Otherwise the upsert test fails.
                 - The transport test fails if ruleset JSON appears in argv, a body skips
                   secretscan.ScanPayload, or exec.LookPath is called.
                 - If the PATCH body is anything but {"allow_auto_merge":true},
                   TestRepoPatchTouchesOnlyAutoMerge fails (D11).
                 - If TestMain stops installing the stubs, TestHermeticBranchProtection_NeverDials
                   fails.

  t-8  Guard ci.yml as the complete merge gate  [risk+verification+mvp]
       files:    internal/cmd/ci_required_checks_test.go (new), internal/cmd/release_pipeline_test.go
       covers:   c-2, c-4, c-6
       depends:  t-1, t-2
       desc:     Test-only sweeps over the live .github/workflows. If D2 flips to risk's trigger,
                 this task also edits pin-currency.yml and its test so every pull_request workflow
                 gets dispatched, and it then depends on t-5.
       contract: - The test reads ci.yml's job ids with its own indent-2 scan, not protect's parser
                   and not a literal list. If any id is missing from the MainRuleset contexts,
                   TestEveryCIJobIsRequired fails. Red-proof: an appended `extra:` job must appear;
                   a parser that drops the last job fails.
                 - If a job in a workflow that contributes required contexts gains a job-level `if:`,
                   TestCIJobsHaveNoJobLevelIf fails (a skipped required check reports success).
                 - If ci.yml's pull_request trigger gains paths or branches filters (or their
                   -ignore forms) or types, TestCIPullRequestTriggerUnfiltered fails.
                 - If ci.yml references any secret other than GITHUB_TOKEN, TestCIReadsNoSecrets
                   fails.
                 - If a workflow that contributes required contexts lacks workflow_dispatch,
                   TestRequiredChecksAreDispatchable fails.
                 - If release.yml stops triggering on push to [main], or pushes anything but
                   "$tag", TestReleaseTagsFromMainOnly fails (c-4).

  t-9  Publish .dross chores as an auto-merge PR  [risk+verification+mvp]
       files:    internal/cmd/chorepr.go (new), internal/cmd/chorepr_test.go (new)
       covers:   c-5, c-10
       depends:  t-2, t-4, t-6
       desc:     Pushes the local base tip, exact SHAs, fast-forward only, to the
                 dross-chores/<base, "/"→"-"> branch (D13). Joins the open PR on that branch or
                 opens one, then arms auto-merge with "merge" (D1). If auto-merge fails it still
                 succeeds and reports the URL. On main, it refuses up front a batch that would change
                 ReleaseTag (D3).
       contract: - With an open chore PR, a second batch must be a plain fast-forward push with 0
                   OpenPRFunc calls. Otherwise the join test fails.
                 - A diverged chore branch with an open PR must error naming the URL and push
                   nothing. With no open PR it must be recreated with --force-with-lease and get a
                   new PR. Otherwise the divergence table fails.
                 - If AutoMergePRFunc is called with "squash", TestChorePRMergesAsMergeCommit fails.
                 - If ErrAutoMergeUnavailable surfaces as an error instead of nil plus the URL plus
                   "merge it by hand", the degraded-path test fails.
                 - If a 1.7.19.1 → 1.8.0.0 batch on main moves any origin ref, the release-guard
                   test fails.
                 - If the chore name for main or milestone/v1.7 matches either include pattern
                   (path.Match), the pattern test fails.
                 - The publish test fails if origin/<chore> != the local base tip afterwards, or if
                   an ahead-and-behind base leaves `rev-list <base> --not origin/<base>
                   origin/<chore>` non-empty.

Wave 3
  t-10 Add `dross protect` verb  [risk+verification+mvp]
       files:    internal/cmd/protect.go (new), internal/cmd/protect_test.go (new), cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-9, c-1, c-7, c-8, c-10
       depends:  t-1, t-2, t-3, t-7
       desc:     Previews by default. `--apply` upserts both rulesets, sets allow_auto_merge, then
                 reads main back through Assess. `--check <branch>` prints protected, unprotected,
                 unknown or not-checked. Jobs come from origin/<main>'s tree (D7). Exports
                 ProtectFixHint. 5 files, but 3 are one-line registration/docs edits.
       contract: - If a run without --apply records any Create, Update or SetAllowAutoMerge call,
                   the preview test fails (locked apply_verb).
                 - If a pull_request_target or release.yml job is in the preview, or "trunk"
                   doesn't target refs/heads/trunk, the build-input test fails.
                 - Re-applying with both names listed must make no Create. With neither listed it
                   must make exactly Create ×2 and SetAllowAutoMerge(true). Otherwise the upsert
                   test fails.
                 - The apply-safety test fails if a read-back missing a context exits 0, or if a
                   matrix job fails to abort before any write.
                 - If the milestone upsert fails after main succeeds, the run must exit non-zero
                   and name which ruleset applied. Otherwise the partial-apply test fails.
                 - A job present in the working tree but not on origin/<main> must be named "not on
                   main yet", not required. Otherwise the source test fails.
                 - If provider=forgejo makes any gh call, the provider test fails.
                 - A flag rename fails Lookup("apply"), and a verb rename fails
                   TestNarratedCommandsResolveAgainstTheTree. TestCLITreeGolden and
                   TestReadmeAdvertisesOnlyRealCommands pin registration and the README row.

  t-11 Route base chore push by live protection  [risk+verification+mvp]
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_chores_test.go (new)
       covers:   c-5, c-10, c-1
       depends:  t-3, t-7, t-9
       desc:     pushBaseIfAheadDrossOnly probes only for github and only when .dross-only commits
                 are ahead (ahead-and-behind included). RequiresPR routes to t-9 and returns a result
                 the callers narrate. Unprotected or non-GitHub keeps today's push. Unknown also
                 keeps it, and a refusal names the probe reason (D5).
                 pushQuickBaseIfRecorded inherits this.
       contract: Fixture: a bare origin whose pre-receive hook logs and rejects updates to
                 refs/heads/main and refs/heads/milestone/*.
                 - If a protected, purely-ahead base logs any base update, or t-9 didn't run, the
                   protected-route test fails.
                 - Unknown probe plus hook refusal must not report pushed=true and must name both
                   reasons. Otherwise the unknown test fails.
                 - If forgejo makes any BranchRulesFunc call, or any existing TestPushBase* case
                   changes, the legacy test fails.
                 - A protected base with code ahead must push nothing, and its message must name
                   quick/ plus a PR into the base instead of "push … manually". Otherwise the
                   code-ahead test fails.
                 - quick_base=main with phase base milestone/v1.7, both protected, must give two
                   chore branches and two PRs. Otherwise the two-base test fails.

Wave 4
  t-12 Report main's live protection in doctor  [verification+risk+mvp]
       files:    internal/cmd/doctor_protection.go (new), internal/cmd/doctor.go,
                 internal/cmd/doctor_protection_test.go (new)
       covers:   c-3, c-7, c-9
       depends:  t-1, t-3, t-7, t-10
       desc:     A `Branch protection:` section: BranchRules → Assess against origin/<main>'s jobs,
                 one ⚠ per gap ending in ProtectFixHint. Forgejo or GitLab print `not checked
                 (<provider>)`; an Unknown result prints `unknown (<reason>)`. Gaps are warnings and
                 the exit code doesn't change.
       contract: - The gap-render table fails unless each gap prints its own line: zero rules →
                   "unprotected" plus the fix; missing shellcheck → a line naming it; bypass
                   "always" → "admin bypass allowed". All three at once must print three lines.
                 - If a BranchRules error renders ✓ or "protected",
                   TestProtectionUnknownNeverReadsProtected fails. BypassUnknown must render
                   "unknown", never "none".
                 - If gitlab or forgejo makes any seam call, the provider test fails.
                 - If the exit code differs with and without gaps, the exit test fails
                   (/dross-ship gates on doctor).
                 - If doctor's output changes with no [remote] configured, TestDoctorRunGolden fails.
                   With no origin/<main>, the section must name its workflow source.

  t-13 Wire chore routing into ship and complete  [risk+verification]
       files:    internal/cmd/ship.go, internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/protected_base_test.go (new)
       covers:   c-5
       depends:  t-11
       desc:     The three safety-net callers narrate the chore PR; `ship --json` gains chore_pr
                 (omitempty); the phase-PR open goes through OpenPRFunc. When complete's
                 fast-forward fails because the ahead commits sit on an open chore PR, it refuses
                 with the PR URL and "re-run once it merges" instead of offering --recover.
       contract: Setup: main-based phase, github provider, hook rejecting refs/heads/main, seams
                 stubbed, protection reads as protected.
                 - Run ship, an origin squash-merge, an origin chore merge commit, then complete.
                   Any main update in the hook log, a non-zero exit, or a non-empty
                   origin/main..main after fetch fails the end-to-end test.
                 - With the chore PR still open and origin/main moved, complete must refuse,
                   must not mention --recover, and must move no local ref. Otherwise the
                   pending-chore test fails.
                 - If the unprotected-stub flow stops pushing main directly, the regression test
                   fails.
                 - TestShipJSONGolden must be unchanged with no chore PR. With one, stdout must be
                   a single JSON document with chore_pr.url. Otherwise the JSON test fails.

  t-14 Keep milestone pushes off protected branches  [risk+verification+mvp]
       files:    internal/cmd/milestone.go, internal/cmd/milestone_protected_test.go (new)
       covers:   c-10
       depends:  t-11
       desc:     pushMilestoneHeadIfAhead sends a .dross-only ahead set to the chore router and
                 refuses code-ahead. `milestone complete` refuses to open the integration PR while
                 a chore PR into the milestone is open (D9). Create, finalize and prune are
                 unchanged. The OpenPR call goes through OpenPRFunc.
       contract: Hook: rejects updates to existing refs/heads/milestone/*, allows create and delete.
                 - If create, prune or `complete --finalize` fails under the hook, or any step
                   updates an existing milestone ref, the ref-lifecycle test fails.
                 - A .dross-only commit ahead on milestone/v1 must open a chore PR
                   (dross-chores/milestone-v1 → milestone/v1) with an empty hook log. Otherwise the
                   chore-route test fails.
                 - Code ahead must push nothing, open no milestone PR, and name the phase-PR
                   route. Otherwise the code-ahead test fails.
                 - If an open chore PR into milestone/v1 lets `milestone complete` open the
                   integration PR, the pending-chore test fails.

  t-15 Route protected-base work in prompts  [risk+verification]  (D8)
       files:    assets/prompts/quick.md, assets/prompts/ship.md, assets/prompts/milestone.md,
                 internal/cmd/protected_prompts_test.go (new)
       covers:   c-1, c-5, c-10
       depends:  t-10, t-11
       desc:     quick.md: when `dross protect --check <base>` says protected, a standalone quick
                 commits on quick/<NEW_VERSION> and opens a PR into the base. ship.md narrates the
                 chore PR and the re-run step. milestone.md: the chore PR merges before the
                 milestone PR, and finalize still deletes. Run `make install` afterwards (r-01).
       contract: - If quick.md loses the --check → quick/ + PR route, or still says unconditionally
                   "go to that base directly", the quick-route test fails.
                 - If any prompt says `git push origin main` or `git push origin <base>`, the
                   push-ban test fails.
                 - If ship.md stops naming the chore PR and the re-run step, or milestone.md stops
                   saying the chore PR merges first and --finalize still deletes, the narration test
                   fails.
                 - If `dross protect --check` is misspelled, TestShipPromptCommandsExist fails.

Wave 5
  t-16 Apply and prove protection on Rivil/dross  [risk+verification+mvp]
       files:    .dross/phases/main-branch-protection/protection-proof.md (new)  (D14)
       covers:   c-1, c-2, c-3, c-4, c-6, c-7, c-8, c-9, c-10
       depends:  t-1 … t-15
       desc:     Run `make install`. Push anything in origin/main..main and
                 origin/milestone/v1.7..milestone/v1.7 while both are still unprotected. Show the
                 user the `dross protect` preview (pair gate), then run `--apply` and read back.
                 Probe c-10 on a throwaway milestone/zz-protect-probe. No push to main (D4).
       contract: - Any ⚠ or "unknown" in doctor's section, or a `--check main` or `--check
                   milestone/v1.7` that isn't "protected", records c-3/c-9 red. Unknown on
                   milestone/v1.7 means t-7's path escaping is wrong live.
                 - current_user_can_bypass != "never" on either ruleset (read as admin) records
                   c-1/c-7 red.
                 - rules/branches/main must list deletion, non_fast_forward and pull_request, plus
                   required_status_checks whose contexts equal ci.yml's jobs. Anything less records
                   c-2/c-7 red.
                 - allow_auto_merge or allow_merge_commit reading false records c-8/c-4 red.
                 - If the probe's create is refused, STOP: ensureMilestoneBranch would break, and
                   that blocks. An accepted direct commit records c-10 red. A refused delete means
                   finalize is broken.
                 - If re-running --apply leaves anything other than 2 rulesets, idempotence is
                   red.
                 - On an open Dependabot or pin-currency PR, any unreported required context
                   records c-6 red. If none is open, c-6/c-8 live are recorded as unobserved.
```

**Coverage:**

| Criterion | Tasks |
|---|---|
| c-1 | t-2, t-10, t-11, t-15, t-16 |
| c-2 | t-1, t-2, t-8, t-16 |
| c-3 | t-3, t-7, t-12, t-16 |
| c-4 | t-2, t-4, t-7, t-8, t-16 |
| c-5 | t-3, t-4, t-6, t-9, t-11, t-13, t-15 |
| c-6 | t-2, t-5, t-8, t-16 |
| c-7 | t-2, t-3, t-12, t-16 |
| c-8 | t-5, t-6, t-7, t-10, t-16 |
| c-9 | t-1, t-2, t-7, t-10, t-12, t-16 |
| c-10 | t-2, t-9, t-11, t-14, t-15, t-16 |

**Repo facts checked:**

- release.yml tags whenever the tag projected from `.dross/project.toml` has no tag yet (D3).
- pin-currency's bump job dispatches only ci.yml (D2).
- ci.yml has 4 jobs (test, mutation-ts, goreleaser-check, shellcheck). Its pull_request trigger is unfiltered, and it has no job-level `if:` and no `secrets.` references.
- argfence's gh policy requires `--` before a derived positional. This resolves the gh argv in t-6 in risk's favour: verification's and mvp's argv would fail the spawn audit.
- `ship.OpenPRFunc` does not exist yet; ship.go:411 and milestone.go:286 call `ship.OpenPR` directly.
- TestNarratedCommandsResolveAgainstTheTree scans only `internal/cmd/*.go`, which is why the fix-hint literal stays in cmd.
- mergeGate in phase.go falls back to git ancestry instead of blocking when the provider can't answer (D5).

**Open risks no draft closes hermetically:**

- What `gh pr merge --auto` does on a clean PR (D6).
- Whether a ruleset's pull_request rule refuses creating a milestone/* branch (t-16 has a STOP condition for this).
- Whether a GITHUB_TOKEN-armed auto-merge can merge an actions-ecosystem PR that edits workflow files (risk).

**Not in any draft:** the README prompt-token rows that t-15's prompt edits would change under the README-sync convention. Raise it at plan review; this synthesis doesn't add it.

## Disagreements

**D1. Chore-PR merge method, and what happens to local base after publishing.**

- **risk and verification:** push the exact local SHAs and auto-merge with `--merge`. Local base keeps its chores and fast-forwards once the PR merges. `phase complete` refuses while the chore PR is pending and origin has moved.
- **mvp:** squash auto-merge, then reset local base to origin (update-ref or `reset --keep`), and cherry-pick later chores onto the open chore branch.
- **Default:** risk and verification. Squash rewrites SHAs, so chores kept locally diverge. A reset builds the next batch without the previous one, and the cherry-pick can conflict on board.json or changes.json.
- **Cost of the default:** every protected-main `phase complete` waits for a full ci.yml run on the chore PR before it can fast-forward.

**D2. Trigger for the Dependabot auto-merge workflow.**

- **mvp and verification:** `pull_request_target` with no checkout. It is not a pull_request job, so it is never required, and pin-currency doesn't change.
- **risk:** `pull_request` plus `workflow_dispatch` (GitHub's documented example). Its test fails on any `pull_request_target`.
- **Consequences of risk's option:** the job becomes a required check, as c-9 literally says. pin-currency.yml must dispatch every pull_request workflow. The job-level-`if:` guard has to narrow to ci.yml only.
- **Default:** `pull_request_target`, held off from the pwn-request risk only by the no-checkout test; the alternative is a larger required set plus pin-currency changes. Bot merges also split: `--squash` (mvp, verification) or `--merge` (risk); the default is `--squash`, and t-2 allows both.

**D3. Release-tag guard on chore PRs into main.**

- **risk:** t-4 plus the guard in t-9. **verification:** omitted on purpose ("direct push has the same exposure today"). **mvp:** absent.
- **Checked:** `[project].version` lives under `.dross/`, and release.yml tags any version whose projected tag is new. A `.dross`-only merge can therefore cut a release. chore_push's "why" assumes it can't.
- **Default:** include the guard. Auto-merge would make a premature tag (the v1.0.0 incident) hands-free. The guard enforces the locked decision's premise; it doesn't override the decision.

**D4. Probing main live.**

- **verification:** one admin push of an empty commit to main, expecting a GH013 refusal.
- **risk and mvp:** never push to main; prove c-1 and c-7 by ruleset readback plus doctor.
- **Checked:** origin/main as last fetched is at 1.6.9.1 and the tag v1.6.9 exists. An accepted probe would therefore cut no release, so risk's and mvp's "auto-release" fear is overstated. The real cost is an unreviewed commit landing on main.
- **Default:** no push to main. The push is the only behavioural proof of "admin included"; readback with `current_user_can_bypass=never` only proves the configuration.

**D5. What the push router does when the protection probe returns Unknown.**

- **risk:** fall back to today's push, let the server refuse it, and report both reasons. **mvp:** fall back to today's push.
- **verification:** hard error, no push. It reads the fallback as breaking locked chore_push ("never as a direct push") and the spirit of doctor_reach.
- **Default:** risk's fallback, which is not adopted as a lock conflict. With an empty bypass list, no direct push can land on a protected branch. The same choice is how mergeGate already degrades instead of blocking.
- **Flip to verification** if you read "never as a direct push" as "never attempt one".

**D6. What the milestone/* ruleset contains.**

- **mvp and verification:** pull_request only. Verification's test fails on required_status_checks because it would block creating the branch at scope time.
- **risk:** also required_status_checks with `do_not_enforce_on_create`. Its reason: with nothing pending, gh refuses auto-merge with "clean status", so milestone chore PRs would need merging by hand.
- **Unverified:** what gh does on a clean PR. ci.yml's checks do run on PRs into milestone/*.
- **Default:** pull_request only; t-16 watches the first milestone chore PR. This decides whether milestone bookkeeping is hands-free.

**D7. Where the required-check list comes from.**

- **risk:** the workflows in origin/<main>'s tree. A job only in the working tree is shown as "not on main yet".
- **mvp and verification:** the working tree.
- **Default:** origin/<main>, the minority view. A required job that main's workflows can't produce leaves every PR into main pending forever, Dependabot's included. The cost is one re-run after merge, which doctor's MissingCheck flags. Under D2's default this phase's own dogfood sees no difference.

**D8. Whether there is a prompt task, and how quick.md detects protection.**

- **risk and verification:** edit the quick, ship and milestone prompts. **mvp:** no prompt task ("no criterion needs one").
- quick.md currently says "go to that base directly". With milestone/* protected, that code has no path to origin.
- **Probe:** risk uses `dross protect --check`. verification uses doctor's section, but that section reports only main (c-3), never the milestone quick base.
- **Default:** include the task, with `--check`.

**D9. `milestone complete` while a chore PR into the milestone is open.**

- **risk:** refuse to open the integration PR, naming the chore PR.
- **verification:** open the integration PR anyway and say the chore PR must merge first.
- **Default:** refuse. If the integration PR merges first, the chore commits miss main, and the chore PR is closed when `--finalize` deletes its base.

**D10. Pinning `integration_id` on required checks.**

- **verification:** pin 15368 (GitHub Actions), so a hand-posted status can't satisfy a required check.
- **risk:** no pin. A wrong ID blocks every PR, and spoofing a status already needs write access.
- **mvp:** no pin, but its reason (dispatched runs couldn't satisfy a pinned check) is wrong. workflow_dispatch runs are Actions check runs too.
- **Default:** no pin.

**D11. The repo-settings PATCH body.**

- **verification and mvp:** exactly `{"allow_auto_merge":true}`.
- **risk:** sets allow_auto_merge and keeps `allow_merge_commit=true`, which means it may send that key too.
- **Default:** the exact body, so the verb never changes a merge setting the user chose. The cost: on a repo with merge commits disabled, `--merge` chore PRs (D1) fail until someone fixes the setting by hand.

**D12. Where the pure logic lives.**

- **verification:** internal/protect (scan, build, assess).
- **risk:** an internal/ghworkflow leaf, a classifier in internal/diag, and release in internal/project.
- **mvp:** internal/ship.
- **Default:** internal/protect, with the release tag in internal/project. That gives one pure core with its own tests, which matters for the per-package coverage floor. The doctor hint stays in internal/cmd so the narrated-command test can see it.

**D13. The chore branch name.**

- risk: `dross-chores/<base with / → ->`. mvp: `dross-chores/<base>`. verification: `dross/chore/<base>`.
- **Default:** risk's flattened form. All three agree on the one property that matters: the name must match neither ruleset include.

**D14. Where the dogfood evidence lives.**

- risk: a proof doc in the phase directory. verification: the task's commit message. mvp: nothing tracked.
- **Default:** the proof doc, because a task with no file has nothing to put in an atomic commit.
