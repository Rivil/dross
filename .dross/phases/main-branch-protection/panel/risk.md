# Risk-lens draft: main-branch-protection

Lens: start from what breaks and give each failure mode one owning task with one test that fails when it breaks. The breakages this phase adds are listed below. Each is owned by the task in brackets.

- A required check that never reports blocks every PR: matrix, `name:`, reusable `uses:`, filtered triggers, and workflows on the PR branch but not on main [t-1, t-8, t-10].
- A required job that is skipped reports green [t-8].
- Unknown read as protected or as unprotected [t-3, t-12].
- A chore PR to main cuts a release [t-6, t-9].
- The chore branch collides with, or is refused by, its own ruleset [t-9].
- Lost updates between chore batches [t-9].
- Chore commits on local main diverge when the chore PR has not merged yet but origin moved [t-13].
- Creating or deleting a milestone branch is refused, or a chore PR into a milestone that is about to be deleted loses its commits [t-14].
- Standalone quick code commits are stranded on a protected base [t-11, t-15].
- Protection is applied while bookkeeping commits sit unpushed on local main or milestone [t-16].
- A live probe pushes to the real main [t-16].

```
Phase main-branch-protection — 16 tasks across 5 waves

Wave 1
  t-1  Parse pull_request jobs from workflow YAML
       files:    internal/ghworkflow/jobs.go (new), internal/ghworkflow/jobs_test.go (new)
       desc:     Add a pure, line-based parser with a leaf package and stdlib only, so the boundary
                 direction stays clean. It takes {path: content} and returns the jobs of each
                 workflow triggered on pull_request: workflow, job id, and check context (the
                 job's `name:` if set, else its id).
                 It refuses, by name, any job whose check name can't be known statically: a
                 matrix job, a job-level `uses:`, or a pull_request trigger with
                 branches/paths filters.
       covers:   c-2, c-9
       contract: `on: [push, pull_request_target]` yields zero jobs; pull_request_target must
                 never prefix-match pull_request.
                 `on: pull_request` written as a scalar, a flow sequence and a mapping each
                 yields the same job set.
                 A job with `name: Unit tests` yields context "Unit tests", not its id.
                 A job with `strategy: matrix:` returns an error naming the workflow and the
                 job, never a bare "test" context that would never report.
                 `pull_request: { paths: [...] }` returns an error naming the workflow.
                 A `run: |` block containing a line `  fake:` at job-key indent adds no job.
       depends:  —

  t-2  Build main and milestone rulesets from contexts
       files:    internal/ghworkflow/ruleset.go (new), internal/ghworkflow/ruleset_test.go (new)
       desc:     Add a pure builder: Build(mainBranch, contexts []string) returns two rulesets.
                 "dross: <main>" gets pull_request (0 approvals), required_status_checks,
                 non_fast_forward and deletion. "dross: milestone/*" gets pull_request and
                 required_status_checks with do_not_enforce_on_create=true. It gets no
                 deletion rule.
                 Both rulesets are target=branch, enforcement=active and bypass_actors=[].
                 Exports the include patterns and a JSON payload.
       covers:   c-1, c-4, c-7, c-9, c-10
       contract: bypass_actors serialises as `[]`, never null or omitted. An omitted key lets
                 GitHub keep an existing bypass on PUT.
                 The pull_request rule's allowed_merge_methods contains "merge". Dropping it
                 fails a milestone-PR-merges-as-merge-commit test (c-4).
                 No rule of type required_linear_history in either ruleset.
                 The milestone ruleset carries no deletion rule (finalize must delete).
                 The milestone ruleset sets do_not_enforce_on_create=true, so
                 ensureMilestoneBranch can create the branch.
                 strict_required_status_checks_policy=false. Strict would stall a waiting
                 auto-merge every time main moves.
                 Neither ruleset includes `refs/tags/*`, `~ALL`, `dependabot/**` or
                 `pin-currency/bump`.
                 mainBranch "trunk" yields include `refs/heads/trunk`.
       depends:  —

  t-3  Read live branch rules via gh api
       files:    internal/ship/rules.go (new), internal/ship/rules_test.go (new),
                 internal/cmd/hermetic_env_test.go
       desc:     BranchRules(repo, branch) uses screenedGH to call `gh api` for
                 repos/<o>/<r>/rules/branches/<branch>, then for each ruleset_id it reads
                 rulesets/<id> to get bypass_actors. The result is tri-state: Known{rules,
                 required contexts, PushBlocked, ForceBlocked, DeleteBlocked, Bypass,
                 BypassKnown} or Unknown{reason}.
                 The call is time-bounded like ListOpenPRs. BranchRulesFunc is the seam.
                 TestMain installs a non-dialing stub that answers "unprotected".
       covers:   c-3, c-5
       contract: gh exiting non-zero (404, 401, 403 rate limit, gh not on PATH) and a
                 timeout each return Unknown with the reason, never Known with zero rules.
                 A 200 with `[]` returns Known/unprotected.
                 A ruleset JSON that lacks the `bypass_actors` key (the caller can't see it)
                 gives BypassKnown=false, not an empty bypass list.
                 PushBlocked is true for pull_request, update or required_status_checks, and
                 false when the only rule is non_fast_forward.
                 Branch "milestone/v1.7" reaches the API path escaped. owner/repo comes from
                 [remote].url, and a leading `-` or a `..` segment is refused before gh runs.
                 A cmd test that reaches the real BranchRules fails the binary. A test also
                 asserts that TestMain installed the stub.
       depends:  —

  t-4  Upsert rulesets and repo merge settings
       files:    internal/ship/rulesetwrite.go (new), internal/ship/rulesetwrite_test.go (new)
       desc:     UpsertRuleset(repo, name, payload) lists the repo's rulesets: PUT
                 rulesets/<id> when the name exists, POST otherwise. The payload goes to gh
                 via `--input -` on stdin.
                 EnableMergeSettings(repo) sends a PATCH that sets allow_auto_merge=true and
                 keeps allow_merge_commit=true. Both go through seams.
       covers:   c-9, c-4, c-5
       contract: With an existing ruleset named "dross: main", apply issues exactly one PUT
                 to its id and zero POSTs. A re-run never duplicates the ruleset.
                 The ruleset JSON never appears in gh's argv.
                 The PATCH body sets allow_auto_merge true and never sets allow_merge_commit
                 false. Milestone PRs must still land as merge commits (c-4).
                 A 403 from the list call returns an error naming the admin permission, not
                 "no ruleset", so no blind POST is sent.
       depends:  —

  t-5  Add auto-merge and PR-open seams
       files:    internal/ship/automerge.go (new), internal/ship/automerge_test.go (new),
                 internal/ship/open.go
       desc:     EnableAutoMerge(opts, number) builds `gh pr merge --auto --merge -- <n>`
                 through screenedGH, and EnableAutoMergeFunc is its seam. Also add an
                 OpenPRFunc seam so internal/cmd tests can open a GitHub PR without gh.
       covers:   c-5
       contract: The argv is exactly `pr merge --auto --merge -- <n>`, with the flags ahead
                 of `--` (the cobra `--` trap that killed `pr view`). Never `--squash`,
                 because a squash rewrites SHAs and leaves the local base diverged. Never
                 `--admin`, because that bypasses the ruleset.
                 gh's "auto merge is not allowed" or "clean status" failures surface as an
                 error that carries gh's reason, and stdout carries no API body.
                 OpenPRFunc defaults to OpenPR, so a production caller using it hits gh.
       depends:  —

  t-6  Project release tag from project.toml bytes
       files:    internal/project/release.go (new), internal/project/release_test.go (new)
       desc:     ReleaseTag(src []byte) returns "vX.Y.Z" from [project].version, the same
                 projection as scripts/release-version.sh. The chore-PR guard compares
                 origin/<main> with the local tip using it.
       covers:   c-5, c-4
       contract: A parity test runs `sh scripts/release-version.sh` on 5 fixtures (4-part,
                 3-part, `version` under [stack] before [project], missing key, comment
                 after the value) and fails on any disagreement with ReleaseTag.
                 1.7.19.0 → 1.7.19.1 projects the same tag, so a quick bump on main cuts no
                 release. 1.7.19.1 → 1.8.0.0 projects a different tag.
       depends:  —

  t-7  Add Dependabot minor/patch auto-merge workflow
       files:    .github/workflows/dependabot-automerge.yml (new), .github/dependabot.yml,
                 internal/cmd/dependabot_automerge_test.go (new)
       desc:     Triggered on pull_request and workflow_dispatch, with workflow permissions
                 contents: read; write scopes are job-level only. The job runs only when
                 github.event.pull_request.user.login == 'dependabot[bot]'.
                 dependabot/fetch-metadata is pinned by SHA. The step runs `gh pr merge --auto
                 --merge` only for semver-patch/minor. Rewrite dependabot.yml's
                 "No auto-merge" comment.
                 Audit against ~/.claude/memory/reference_ci_supply_chain_hardening.md.
       covers:   c-8, c-6
       contract: A fixture whose merge-step `if:` admits version-update:semver-major fails
                 the test. So does one with no update-type condition at all.
                 Any `pull_request_target` trigger fails the test, and so does a workflow-level
                 `contents: write`.
                 A job `if:` keyed on `github.actor` alone fails. A human re-run must not be
                 able to arm it.
                 The existing sweeps go red on this file if an action is unpinned
                 (TestWorkflowActionsArePinned) or a `${{` sits inside run:
                 (TestWorkflowsHaveNoExpressionsInRun).
                 Under workflow_dispatch the job `if:` is false, so the check reports skipped
                 (= satisfied) on the pin-currency PR.
       depends:  —

Wave 2
  t-8  Guard ci.yml as the complete merge gate
       files:    internal/cmd/merge_gate_workflow_test.go (new), .github/workflows/pin-currency.yml,
                 internal/cmd/pin_currency_workflow_test.go
       desc:     Live sweeps over the real .github/workflows. The bump job now dispatches
                 every pull_request workflow, not only ci.yml, so every required check can
                 report on the bot PR.
                 Audit pin-currency.yml against the CI hardening reference.
       covers:   c-2, c-6
       contract: c-2: the test lists ci.yml's jobs with its own indent-2 reading of `jobs:`,
                 not via ghworkflow and not from a literal list. Every id must map to a
                 context in t-2's main ruleset built from t-1's output. A ci.yml job that the
                 parser drops or refuses (for example a new matrix job) fails this test by
                 name.
                 c-2: a job-level `if:` on any ci.yml job fails the test. A skipped required
                 job reports success and would pass the gate without running.
                 c-6: any `secrets.` reference in ci.yml fails the test, because Dependabot
                 pull_request runs receive no secrets.
                 c-6: every workflow that contributes a required job must declare
                 workflow_dispatch and be dispatched by pin-currency's bump job. A
                 pull_request workflow the bump job doesn't dispatch fails the test, because
                 its check would never report on the GITHUB_TOKEN-opened PR.
       depends:  t-1, t-2, t-7

  t-9  Publish .dross chores as an auto-merge PR
       files:    internal/cmd/chorepr.go (new), internal/cmd/chorepr_test.go (new)
       desc:     publishDrossChores(repoDir, base, opts) pushes the local base tip, with the
                 exact SHAs, to `dross-chores/<base with / → ->` and fast-forwards only.
                 It reuses the open PR (FindOpenPRByHeadFunc) or opens one into the base
                 (OpenPRFunc), then calls EnableAutoMergeFunc.
                 If auto-merge fails, the call still succeeds and returns the URL and the
                 reason. Before any push, it refuses when base is the main branch and the
                 batch changes ReleaseTag.
       covers:   c-5, c-10
       contract: Join: with an open chore PR and origin/dross-chores/main an ancestor of local
                 main, the second batch is a plain fast-forward push. OpenPRFunc calls = 0.
                 Diverged chore branch with an open PR: the error names the PR URL and
                 nothing is pushed.
                 Diverged chore branch with no open PR (an earlier PR was squash-merged): the
                 branch is recreated with --force-with-lease and a new PR is opened.
                 EnableAutoMergeFunc error: the result is nil-error, and the returned report
                 carries the PR URL and "merge by hand".
                 A batch moving 1.7.19.1 → 1.8.0.0 on main is refused with no ref changed on
                 origin. A premature tag on merge is the v1.0.0 incident shape.
                 The chore names for "main" and "milestone/v1.7" match neither include
                 pattern from t-2. A match would get the chore push itself refused.
                 After publish, origin/<chore> == local <base> tip (same SHA), so a
                 merge-commit merge lets local base fast-forward.
       depends:  t-2, t-5, t-6

  t-10 Add `dross protect` verb
       files:    internal/cmd/protect.go (new), internal/cmd/protect_test.go (new),
                 cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt, README.md
       desc:     `dross protect` previews both rulesets: required contexts, create vs update,
                 and the merge-setting change. `--apply` upserts both rulesets, then patches
                 the merge settings. `--check <branch>` prints one word: protected,
                 unprotected, unknown or not-checked.
                 Jobs come from the workflows in refs/remotes/origin/<main>'s tree, not the
                 working tree. This exposes prWorkflowJobsAt(repoDir, ref) for doctor.
                 Providers other than GitHub are refused with "not supported (<provider>)".
       covers:   c-9, c-1, c-7, c-10
       contract: Without --apply the write seams record zero POST, PUT or PATCH calls.
                 A pull_request job present in the working tree but absent from
                 origin/main's workflows is not required. The preview names it as "not on
                 main yet — re-run after it merges". Requiring it would block every PR into
                 main.
                 A t-1 refusal (a matrix job) aborts --apply before any write.
                 The milestone upsert fails after the main upsert succeeded: the exit is
                 non-zero and the message names the ruleset that was applied and the one
                 that wasn't.
                 provider=forgejo is refused with zero gh calls.
                 The cli_tree.txt golden and the README row are both present, so
                 TestReadmeAdvertisesOnlyRealCommands resolves `dross protect`.
       depends:  t-1, t-2, t-3, t-4

Wave 3
  t-11 Route base chore push by live protection
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_chores_test.go (new)
       desc:     The .dross-only arm of pushBaseIfAheadDrossOnly asks for protection first,
                 and only when [remote].provider is github. Protected: publishDrossChores,
                 with a result struct the callers narrate. Unprotected or not GitHub: today's
                 `git push origin <base>`. Unknown: the old push attempt, and a server refusal
                 errors with the probe's reason.
                 The code-ahead refusal on a protected base names the branch+PR route.
                 pushQuickBaseIfRecorded inherits all of this.
       covers:   c-5, c-10, c-1
       contract: Protected main, using a bare origin whose pre-receive hook logs and rejects
                 refs/heads/main updates: the hook log is empty and publishDrossChores ran.
                 The probe stubbed Unknown and the hook rejects: the error contains both the
                 rejection and the probe reason. It never reports pushed=true.
                 Provider forgejo: BranchRulesFunc calls = 0 and the legacy push runs. The
                 existing basebranch_test.go cases pass unchanged.
                 Protected base with a non-.dross ahead commit: the error names `quick/` and
                 a PR into the base, and nothing is pushed.
                 quick_base=main and phase base=milestone/v1.7, both protected: two distinct
                 chore branches and two PRs.
       depends:  t-3, t-9

  t-12 Report main protection in doctor
       files:    internal/diag/protection.go (new), internal/diag/protection_test.go (new),
                 internal/cmd/doctor.go, internal/cmd/doctor_protection_test.go (new), README.md
       desc:     diag.Protection(result, jobs) is a pure classifier. It returns a "Branch
                 protection:" Section for the configured main branch. It names each gap:
                 unprotected; required check missing (per job); required check with no
                 matching job (never reports, so every PR blocks); admin bypass allowed;
                 force-push allowed; deletion allowed.
                 Each gap's fix names `dross protect --apply`. doctor wires it in through
                 BranchRulesFunc and prWorkflowJobsAt.
       covers:   c-3, c-9, c-7
       contract: An Unknown result renders "unknown (<reason>)" at Warn level. No line
                 contains "protected" and nothing renders OK.
                 Provider gitlab renders "not checked (gitlab)" with BranchRulesFunc calls = 0.
                 BypassKnown=false renders "admin bypass: unknown", never "none".
                 A ci.yml job missing from the required contexts renders a gap naming that
                 job.
                 Gaps are Warn and the doctor exit code stays unchanged. /dross-ship gates on
                 doctor, and an unprotected repo must still ship.
                 The fix hint resolves against the cobra tree (curated-hint parity).
                 No refs/remotes/origin/<main>: the section says which workflow source it
                 compared against.
       depends:  t-1, t-3, t-10

Wave 4
  t-13 Wire chore routing into ship and complete
       files:    internal/cmd/ship.go, internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/ship_protected_test.go (new)
       desc:     The three callers narrate the chore-PR result, and `ship --json` carries
                 `chore_pr` (omitempty). In phase complete, a failed fast-forward whose local
                 ahead commits sit on an open chore PR refuses with that PR's URL and "re-run
                 once it merges", instead of offering --recover.
       covers:   c-5
       contract: Main-based phase, end to end. Origin's hook rejects refs/heads/main. Gh
                 seams are stubbed and protection reads protected. A pause chore sits on
                 local main. Run ship, then a squash-merge on origin, then a chore merge
                 commit on origin, then complete.
                 Expected: the hook log is empty; complete exits 0; every commit in
                 origin/main..main is reachable from origin/dross-chores/main with an open
                 auto-merge PR; and no .dross commit exists only locally.
                 The same flow with the chore PR still open when complete runs: the error
                 names the chore PR URL and no local ref moves.
                 TestShipJSONGolden is unchanged (no chore PR). With one, stdout is a single
                 JSON document that includes chore_pr.url, so a manual-merge URL isn't
                 swallowed by narrate suppression.
       depends:  t-11

  t-14 Keep milestone pushes off protected branches
       files:    internal/cmd/milestone.go, internal/cmd/milestone_protected_test.go (new)
       desc:     On a protected milestone/<v>, pushMilestoneHeadIfAhead sends a .dross-only
                 ahead set through the chore router and refuses code-ahead with the PR route.
                 `milestone complete` refuses to open the integration PR while a chore PR
                 into milestone/<v> is open. Creation (ensureMilestoneBranch) and deletion
                 (finalize, prune) are unchanged.
       covers:   c-10
       contract: The origin hook rejects updates to existing refs/heads/milestone/* and
                 allows create and delete. `milestone create`, `milestone prune` and
                 `milestone complete --finalize` all succeed. A dross step that updates an
                 existing milestone ref turns the test red.
                 Protected milestone/v1 with a .dross-only commit ahead: a chore PR is opened
                 into milestone/v1 and the hook log is empty.
                 An open chore PR into milestone/v1: `milestone complete` errors naming its
                 URL and opens no integration PR. Otherwise the chore commits miss main and
                 are closed with the deleted base.
       depends:  t-11

  t-15 Route protected-base work in prompts
       files:    assets/prompts/quick.md, assets/prompts/ship.md, assets/prompts/milestone.md,
                 internal/cmd/protected_prompts_test.go (new)
       desc:     quick.md: when `dross protect --check <base>` says protected, a standalone
                 quick commits on `quick/<NEW_VERSION>` and opens a PR into the base.
                 ship.md: narrate the chore PR. milestone.md: scope bookkeeping lands on
                 milestone/<v>, never main, and the integration PR waits for open chore PRs.
                 Run `make install` afterwards (r-01).
       covers:   c-1, c-5, c-10
       contract: The test fails if quick.md's standalone step loses the `dross protect
                 --check` branch-and-PR route. A code commit on a protected base has no path
                 to origin.
                 The test fails if any prompt under assets/prompts tells the agent to run
                 `git push origin main` or `git push origin <base>`.
                 TestNarratedCommandsResolveAgainstTheTree fails if `dross protect --check` is
                 misspelled.
       depends:  t-10, t-11

Wave 5
  t-16 Apply and prove protection on Rivil/dross
       files:    .dross/phases/main-branch-protection/protection-proof.md (new)
       desc:     First run `make install`. Before applying, check `origin/main..main` and
                 `origin/milestone/v1.7..milestone/v1.7` and push anything ahead while the
                 branches are still unprotected.
                 Run `dross protect`, have the user approve the preview, then run
                 `dross protect --apply`. Read it back with `dross doctor` and
                 `dross protect --check` for main and milestone/v1.7.
                 Probe only on a throwaway branch: create milestone/zz-protect-probe at
                 origin/main, push a new commit to it (expect GH013), then delete it (expect
                 success). Never push to main to test.
       covers:   c-1, c-3, c-4, c-7, c-9, c-10
       contract: If the live main ruleset has any bypass actor, doctor's Branch protection
                 section prints the admin-bypass gap and the proof doc records c-1/c-7 red.
                 If the readback of rules/branches/main lacks non_fast_forward or deletion,
                 c-7 is recorded red.
                 If the probe's direct-commit push is accepted, c-10 is red. If its delete is
                 refused, `milestone complete --finalize` is proven broken.
                 If `--check milestone/v1.7` prints unknown, t-3's path escaping is wrong
                 live.
                 If allow_auto_merge reads false after apply, the chore-PR path degrades to
                 manual merges, and the proof doc says so.
       depends:  t-7, t-8, t-10, t-12, t-13, t-14, t-15
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 no direct push to main, admin included | t-2, t-10, t-11, t-15, t-16 |
| c-2 every ci.yml job required; test reads ci.yml | t-1, t-2, t-8 |
| c-3 doctor reports live protection + gaps | t-3, t-12, t-16 |
| c-4 milestone PRs merge as merge commits; release tags from main | t-2, t-4, t-6, t-16 |
| c-5 ship/complete on a main-based phase: no direct push, nothing stuck | t-3, t-4, t-5, t-6, t-9, t-11, t-13, t-15 |
| c-6 Dependabot and pin-currency PRs can pass required checks | t-7, t-8 |
| c-7 no force-push or delete on main, admin included | t-2, t-10, t-12, t-16 |
| c-8 Dependabot minor/patch auto-merge; major is manual | t-7 |
| c-9 verb applies a ruleset built from PR jobs; doctor names it | t-1, t-2, t-4, t-10, t-12, t-16 |
| c-10 milestone/* refuses direct push; bookkeeping via chore PR; finalize deletes | t-2, t-9, t-10, t-11, t-14, t-15, t-16 |

All 10 criteria are covered.

## Judgment calls

- **Chore commits stay on local base until merged.** I chose to push the exact local SHAs to the chore branch and merge with `--merge`, so local base fast-forwards afterwards. I rejected resetting local base to origin after publishing. A reset loses updates: a second batch built on origin overwrites the first batch's board.json or changes.json, and the completion record vanishes locally until a pull.
  - For c-5, "stuck" therefore means a commit origin does not carry, not "local is ahead".
- **Chore PRs merge with `--merge`, not `--squash`.** I rejected squash because it rewrites SHAs, which turns every chore batch into a base divergence at the next `phase complete`.
- **Probe first, and the server decides when the probe returns unknown.** I chose to probe protection via the API before any push. Reacting to a push refusal would still be "pushing directly to main" in c-5's terms.
  - When the probe returns unknown, I fall back to today's push and let the server refuse it. I rejected failing closed: GitHub users without `gh` would lose `phase complete` today, and the server enforces protection either way.
  - The "unknown is never a pass" rule binds doctor's reporting, not enforcement.
- **One chore branch per base (`dross-chores/<base>`).** I rejected per-machine names. Two machines pushing concurrently get a refusal that names the open PR, which beats two competing PRs that edit the same .dross files.
- **The release-tag guard on chore PRs into main (t-6/t-9).** The `chore_push` decision assumes ".dross-only leaves the version unchanged", but `[project].version` lives in `.dross/project.toml`. A scope-time bump riding a chore PR would cut a tag early (the v1.0.0 incident). The guard enforces the decision's assumption; it doesn't override it.
- **Build the ruleset from `origin/<main>`'s workflows, not the working tree.** Building from the working tree would require checks that main's own workflows can't produce yet, such as this phase's new dependabot-automerge job, and that blocks every PR into main.
  - Doctor flags the gap after the milestone merges, and the fix is to re-run the verb.
- **The automerge job is required.** The builder includes it, as "jobs of workflows triggered on pull_request" literally says. Excluding it would leave doctor permanently flagging it as a missing required check.
  - The cost is that pin-currency must dispatch every pull_request workflow (t-8), or its PR blocks forever on a check that never reports.
- **The c-2 test lists ci.yml jobs with its own simple reading, not the production parser.** Sharing the parser would let a parser bug drop a job from both sides and pass vacuously. I rejected a committed ruleset snapshot as a second hand-kept source.
- **No `integration_id` on required checks.** Pinning the Actions app ID would stop spoofed commit statuses, but a wrong ID blocks every PR, and spoofing a status already needs write access.
- **The milestone ruleset carries required checks plus `do_not_enforce_on_create`.** I rejected a PR-rule-only ruleset: with nothing pending, GitHub refuses to enable auto-merge ("clean status"), so chore PRs into milestones would pile up for hand merges.
- **Doctor gaps are Warn, not Issue.** /dross-ship and /dross-review gate on doctor, so an Issue would block shipping in every unprotected GitHub repo, including this phase before t-16 applies protection.
- **Standalone quick routing is included (t-15), not deferred.** After c-10, `milestone/v1.7` is the quick base and code commits on it have no path to origin. That is in scope under c-1's "changes land only through a PR". Per include-first, there is no named roadmap phase to defer it to.
- **Never prove c-1/c-7 by pushing to the real main.** A probe push that succeeds would land on main and auto-release. Main is proven by ruleset readback and doctor. Only the milestone probe pushes, and only to a throwaway branch it deletes.
- **c-8 cannot be watched live in this phase.** The workflow reaches main only when v1.7 merges. Inside the phase, its shape is held by t-7's tests. The first Dependabot run after the milestone merge is the live proof.
  - Open risk: whether a GITHUB_TOKEN-armed auto-merge can merge a github-actions-ecosystem PR that edits workflow files. Hermetic tests can't observe it.
