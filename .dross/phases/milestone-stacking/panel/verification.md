# milestone-stacking — verification lens

Phase milestone-stacking — 8 tasks across 3 waves

Wave 1
  t-1  Record the cut base in milestone toml
       files:    internal/milestone/milestone.go, internal/milestone/milestone_test.go
       covers:   c-2
       description: Add `Base string \`toml:"base,omitempty"\`` to milestone.Meta and a
                    `(*Milestone) BaseOr(mainBranch string) string` accessor that returns
                    mainBranch when Base is empty. No caller changes.
       contract: - Save/Load round-trips Base="milestone/v1.1"; dropping the toml tag makes
                   TestMilestoneBaseRoundTrips read back "".
                 - BaseOr("main") on a Meta with no base returns "main"; removing the
                   empty-string guard returns "" and every consumer silently targets nothing.
                 - A pre-v1.2 fixture toml (version/title/status/started/scope/phases, no
                   `base` key) decodes without error and BaseOr yields the main branch —
                   making the field required breaks every milestone shipped before v1.2.
       depends:  —
       status:   pending

  t-2  Add merged-status probe with offline fallback
       files:    internal/cmd/milestone_merged.go, internal/cmd/milestone_merged_test.go
       covers:   c-1, c-3
       description: `milestoneMergedIntoMain(repoDir, branch, mainBranch) (merged, localOnly
                    bool, err error)` — best-effort `fetch origin`, then
                    `merge-base --is-ancestor origin/<branch> origin/<main>`, falling back to
                    refs/heads when the fetch or the remote refs are unavailable.
       contract: - Against the milestoneFinalizeFixture(t, true) shape (milestone merged onto
                   origin/main) the probe returns merged=true; the (t, false) shape returns
                   false. Swapping the is-ancestor argument order flips both.
                 - With origin's URL rewritten to a nonexistent path the probe still answers
                   from refs/heads and returns localOnly=true; deleting the fallback makes it
                   return an error, which would make create/complete/prune unusable offline.
                 - A branch origin has never seen is answered from local refs with
                   localOnly=true, not an "unknown revision" error.
       depends:  —
       status:   pending

  t-3  Add ship.OpenPRsTargeting provider query
       files:    internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-6
       description: `OpenPRsTargeting(opts OpenOpts, base string) ([]BasePR, error)` with
                    OpenPR's provider dispatch (github via the ghCommand seam, forgejo/gitea
                    and gitlab via REST), plus the exported `OpenPRsTargetingFunc` override
                    seam and `ErrBasePRLookupUnsupported`.
       contract: - github: ghCommand stubbed to print
                   `[{"number":7,"url":"https://x/pull/7","title":"milestone v1.3"}]` yields
                   one BasePR{Number:7}; a stub exiting non-zero returns an error, never an
                   empty slice — an empty slice reads as "no dependents" and authorizes a delete.
                 - forgejo: an httptest server asserts the request carries base=milestone/v1.2
                   and state=open, and its mocked list decodes into the returned BasePRs.
                 - An unwired provider returns ErrBasePRLookupUnsupported (errors.Is), and
                   OpenPRsTargetingFunc is a non-nil var defaulting to OpenPRsTargeting.
       depends:  —
       status:   pending

Wave 2 (depends t-1, t-2)
  t-4  Cut from the recorded parent and record it
       files:    internal/cmd/milestone.go, internal/cmd/milestone_stacking_test.go
       covers:   c-1, c-2, c-5
       description: milestoneCreate resolves the cut point from state.current_milestone via
                    t-2's probe (unmerged parent → milestone/<cur>, else main), passes it to
                    ensureMilestoneBranch, writes it to the new milestone's `base`, and prints
                    the branch it cut from. Adds `--base <branch>` to force the cut point.
       contract: - current_milestone=v1.1 with milestone/v1.1 unmerged: `milestone create v1.2`
                   leaves `rev-parse milestone/v1.2` == `rev-parse milestone/v1.1`, writes
                   `base = "milestone/v1.1"` into v1.2.toml, and prints
                   "cut milestone/v1.2 from milestone/v1.1".
                 - After the v1.1→main merge is simulated on origin, the same create cuts at
                   main's tip and records base = "main". A create keyed on milestone.status
                   instead of ancestry passes the first case and fails this one.
                 - current_milestone unset while an unmerged milestone/v1.1 exists: cut point
                   is main and recorded base is "main" (locked stacking_parent — no topology scan).
                 - `milestone create v1.3 --base milestone/v0.9` cuts at milestone/v0.9's tip
                   and records it verbatim even though current_milestone names v1.1.
                 - Non-git dir: create still writes the toml and exits 0 with no base recorded
                   (TestMilestoneCreateNoGitSkips keeps passing).
       depends:  t-1, t-2
       status:   pending

  t-5  Target the recorded parent in the milestone PR
       files:    internal/cmd/milestone.go, internal/cmd/milestone_pr_base_test.go
       covers:   c-3
       description: milestoneComplete's open mode reads the milestone's recorded base and sets
                    opts.BaseBranch to it while t-2's probe says that parent is unmerged,
                    falling back to main once it has merged.
       contract: - v1.2.toml with base="milestone/v1.1" and v1.1 unmerged: the mock provider's
                   first POST /pulls body has base="milestone/v1.1", head="milestone/v1.2" —
                   so the diff is v1.2's own commits, not v1.1's.
                 - Same toml once origin/main contains milestone/v1.1: the POST body has
                   base="main", never a branch --finalize is about to delete.
                 - v1.2.toml with no `base` key: POST body base="main"
                   (locked absent_base_reads_main) — pre-v1.2 milestones open exactly the PR
                   they open today, which the existing
                   TestMilestoneCompleteOpensSinglePRToMain still asserts.
       depends:  t-1, t-2
       status:   pending

  t-6  Refuse deletes that strand an unmerged child
       files:    internal/cmd/milestone_dependents.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_dependents_test.go
       covers:   c-4
       description: `dependentMilestones(root, repoDir, branch, mainBranch)` scans
                    .dross/milestones/*.toml for milestones whose BaseOr equals branch and
                    which t-2's probe reports unmerged; milestonePrune and milestoneFinalize
                    call it before any branch delete and return a refusal naming the dependent.
       contract: - v1.3.toml records base="milestone/v1.2" and v1.3 is unmerged: `milestone
                   prune` (with milestone/v1.2 squash-merged, so the stale detector names it)
                   errors with "v1.3" in the message, and milestone/v1.2 still exists locally
                   and on origin.
                 - `milestone complete v1.2 --finalize` under the same shape refuses naming
                   v1.3, deletes neither the local nor the remote ref, and leaves local main
                   un-fast-forwarded.
                 - Once v1.3 is merged into origin/main, prune deletes milestone/v1.2 — a gate
                   keyed on "a dependent record exists" rather than "an *unmerged* dependent
                   exists" wedges the repo permanently and fails this case.
                 - A milestone toml with no `base` key is never a dependent of anything (it
                   reads as main), so TestPruneDeletesOnlyStaleBranches keeps passing unchanged.
       depends:  t-1, t-2
       status:   pending

Wave 3
  t-7  Add the provider open-PR gate with announced skip
       files:    internal/cmd/milestone_dependents.go, internal/cmd/milestone_provider_gate_test.go
       covers:   c-6
       description: After the toml scan, when [remote].provider/.url are configured, call
                    ship.OpenPRsTargetingFunc for the branch about to be deleted and refuse
                    naming any open PR. An unconfigured provider or a lookup error prints an
                    explicit skip line and proceeds on the toml scan alone.
       contract: - Seam stubbed to return PR #7 based on milestone/v1.2: prune refuses with
                   "#7" in the message and deletes nothing, even though the toml scan found no
                   dependent.
                 - Seam stubbed to return an error: the command prints a line saying the
                   provider check was skipped (with the reason), then applies the toml scan —
                   a clean scan deletes the branch and exits 0. A silent swallow fails the
                   output assertion; a hard error fails the exit-0 assertion.
                 - Project with no [remote].provider: the same skip line, and the stub records
                   zero invocations.
                 - The recorded stub argument is the branch being deleted (milestone/v1.2),
                   not main and not the milestone version — re-deriving the branch inside ship
                   fails this assertion.
       depends:  t-3, t-6
       status:   pending

  t-8  Rewrite the branch-cut narration in prompt and docs
       files:    assets/prompts/milestone.md, README.md, docs/roadmap.md,
                 internal/cmd/milestone_narration_test.go
       covers:   c-5
       description: Replace the unconditional "integration branch from main" claim with the
                    conditional rule (unmerged current milestone's branch, else main) plus
                    `--base`, in the prompt, the README milestone-branch entry and the
                    roadmap's milestone-branch-model entry. A grep test pins all three.
       contract: - The test fails while assets/prompts/milestone.md still contains the literal
                   "integration branch from main"; it must instead name both arms (the
                   milestone/<version> parent AND the merged→main fallback) and mention `--base`.
                 - README's milestone-branch row and docs/roadmap.md's milestone-branch-model
                   entry each carry the conditional phrasing; the failure message names the
                   file that still carries the unconditional claim, so a partial doc pass is
                   not green.
                 - `dross milestone create`'s own "cut X from Y" line is asserted in t-4, so
                   the docs and the runtime narration cannot drift apart silently.
       depends:  t-4
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 (conditional cut point + states the branch) | t-2, t-4 |
| c-2 (base recorded at create, read back, never re-inferred) | t-1, t-4 |
| c-3 (PR targets recorded parent while unmerged, else main) | t-2, t-5 |
| c-4 (prune / --finalize refuse, naming the dependent) | t-6 |
| c-5 (narration states the conditional rule everywhere) | t-4, t-8 |
| c-6 (provider open-PR gate, announced skip) | t-3, t-7 |

All 6 criteria covered.

## Judgment calls

- Split the ancestry probe (t-2) out of `milestone create` rather than inlining it: three
  commands ask the same merged/unmerged question, and the offline fallback is only cheaply
  testable as a git-level unit — a cobra-level test can reach it only by breaking the remote.
- Put `OpenPRsTargeting` in `internal/ship`, not `internal/forge`: forge is the issue-board
  layer (its github backend returns ErrNotImplemented), while OpenPR/PRMerged already live in
  ship with the `ghCommand` + exported-Func seams cmd tests need. Rejected: extending
  forge.BoardClient.
- Gave t-3 wave 1 rather than folding it into t-7: it has no dependency on the toml scan and
  its provider matrix is testable standalone, so it parallelizes with t-1/t-2.
- `BaseOr` takes the main branch as an argument instead of hardcoding "main": honors
  absent_base_reads_main for repos whose git_main_branch is `master` without the milestone
  package importing project config. Rejected: a bare `"main"` constant in the schema layer.
- One new test file per wave-2 task (milestone_stacking_test.go / milestone_pr_base_test.go /
  milestone_dependents_test.go) instead of appending to milestone_test.go — matches the repo's
  per-phase test-file convention (phase_base_truth_test.go, completion_state_truth_test.go)
  and keeps three same-wave tasks off one file. internal/cmd/milestone.go is still touched by
  t-4/t-5/t-6, so they execute serially within the wave.
- t-8 sits in wave 3 behind t-4: the doc claim is only true once the behavior exists, so its
  grep test is written against shipped output rather than a promise.
- t-6 keys the gate on "unmerged dependent", asserted by a case that requires the branch to
  become prunable again after the child merges. A record-exists gate would look safer and
  permanently wedge the repo.
