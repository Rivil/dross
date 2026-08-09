# provider-merge-parity — verification lens

Every task below was derived backward from a test contract: the assertion was written
first, then the smallest change that makes it satisfiable.

```
Phase provider-merge-parity — 6 tasks across 2 waves

Wave 1
  t-1  Swap PRMerged seam for PRStatus
       files:    internal/ship/merged.go, internal/ship/merged_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5
       desc:     Replace PRMerged/PRMergedFunc with PRStatus(opts) (PRStatus, error) /
                 PRStatusFunc, where PRStatus is {Merged bool; BaseRef string}. GitHub adds
                 baseRefName to its `gh pr view --json` field list; Bitbucket returns Merged
                 with an empty BaseRef. mergeGate and stubPRMerged migrate to the new seam.
       contract: TestPRStatusGitHubReturnsBaseRef — canned ghCommand output
                 {"state":"MERGED","mergedAt":"...","baseRefName":"main"} yields
                 {Merged:true, BaseRef:"main"}; the same test asserts the recorded
                 ghCommand args contain "state,mergedAt,baseRefName", so dropping
                 baseRefName from the --json list fails it.
       contract: TestPRStatusGitHubClosedIsNotMerged — state "CLOSED" yields Merged false.
                 A `state != "OPEN"` implementation passes today's tests and fails this one.
       contract: TestPRStatusBitbucketMergedHasEmptyBaseRef — httptest MERGED payload yields
                 Merged true and BaseRef "", proving an unpopulated base ref is empty rather
                 than a stale or guessed branch name.
       contract: TestPRStatusFuncDefaultsToPRStatus — the exported var is non-nil and
                 delegates; gitlab still returns ErrMergeStatusUnsupported at this point.
       contract: Existing TestPhaseCompleteRefusesUnmergedUpstream and
                 TestPhaseCompleteMergeGateRefusalLeavesHEADOnPhase keep passing through the
                 renamed stub; a missed mergeGate call site fails `go build ./internal/cmd`.

  t-2  GitLab open-MRs by target branch
       files:    internal/ship/gitlab.go (new), internal/ship/gitlab_test.go (new),
                 internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-4
       desc:     New gitlab.go holding gitlabTarget(opts) (the APIBase/AuthEnv/token/
                 splitOwnerRepo/gitlabProjectRef preamble currently inlined in openGitLabPR)
                 and gitlabOpenMRsTargeting via
                 GET /projects/{ref}/merge_requests?state=opened&target_branch={base}.
                 OpenPRsTargeting dispatches gitlab to it.
       contract: TestGitLabOpenPRsTargetingUsesIID — response [{"iid":7,"id":901,...}] yields
                 BasePR.Number == 7; an implementation reading "id" returns 901 and fails.
       contract: TestGitLabOpenPRsTargetingFieldMapping — web_url maps to URL and
                 source_branch maps to HeadRefName for each returned MR.
       contract: TestGitLabOpenPRsTargetingQueryAndAuth — the httptest handler asserts the
                 request carries state=opened and target_branch=<base> and a PRIVATE-TOKEN
                 header; a sub-case with AuthScheme "bearer" asserts Authorization: Bearer
                 and no PRIVATE-TOKEN.
       contract: TestGitLabOpenPRsTargetingHTTP500IsError — a 500 yields a non-nil error and
                 a nil slice, never an empty slice (an empty slice reads as "no dependents"
                 and authorizes an irreversible branch delete).
       contract: TestGitLabOpenPRsTargetingMissingToken — with $AuthEnv unset the error
                 mentions `dross env set` and the httptest server records zero requests.
       contract: basepr_test's TestOpenPRsTargetingUnsupportedProvider table no longer lists
                 "gitlab"; bitbucket still yields ErrBasePRLookupUnsupported.

  t-3  Forgejo/Gitea open-PRs by base
       files:    internal/ship/forgejo.go (new), internal/ship/forgejo_test.go (new),
                 internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-4
       desc:     New forgejo.go holding a jsonGet helper (Authorization: token, 30s client,
                 status check — no GET helper exists today, only jsonPost), forgejoTarget(opts),
                 and forgejoOpenPRsTargeting via GET /repos/{owner}/{repo}/pulls?state=open
                 filtered client-side on base.ref. Dispatch routes both forgejo and gitea.
       contract: TestForgejoOpenPRsTargetingFiltersByBaseRef — the server returns three open
                 PRs, two with base.ref "main" and one with base.ref "dev"; exactly the two
                 main-based PRs come back. Gitea's list-pulls endpoint has no base filter, so
                 an implementation trusting a `base=` query param returns the dev PR and fails.
       contract: TestForgejoOpenPRsTargetingFieldMapping — number maps to Number, html_url to
                 URL, and head.ref (not head.label, which carries an owner prefix) to
                 HeadRefName.
       contract: TestForgejoOpenPRsTargetingAuthHeader — the handler asserts
                 "Authorization: token <tok>" and state=open in the query string.
       contract: TestForgejoOpenPRsTargetingHTTP404IsError — a 404 yields an error and a nil
                 slice, not an empty result.
       contract: TestForgejoOpenPRsTargetingGiteaAlias — OpenPRsTargeting with Provider
                 "gitea" and "Gitea" reaches the same handler, proving the dispatch alias and
                 configenum.Normalize path, not just the "forgejo" literal.
       contract: basepr_test's unsupported-provider table no longer lists "forgejo"/"gitea".

Wave 2 (depends t-1, t-2, t-3)
  t-4  GitLab authoritative MR status + target branch
       files:    internal/ship/gitlab.go, internal/ship/gitlab_test.go,
                 internal/ship/merged.go, internal/ship/merged_test.go
       covers:   c-1, c-5
       depends:  t-1, t-2
       desc:     gitlabPRStatus does GET /projects/{ref}/merge_requests/{iid} through
                 gitlabTarget + gitlabReq, mapping state == "merged" to Merged and
                 target_branch to BaseRef. PRStatus dispatch routes gitlab to it.
       contract: TestGitLabPRStatusMerged — {"state":"merged","target_branch":"main"} yields
                 {Merged:true, BaseRef:"main"}, replacing the ErrMergeStatusUnsupported the
                 same call returns today.
       contract: TestGitLabPRStatusClosedIsNotMerged — {"state":"closed"} yields Merged false.
                 A `state != "opened"` implementation would complete a phase whose MR was
                 closed without ever landing; this test is the guard.
       contract: TestGitLabPRStatusOpenedIsNotMerged — {"state":"opened"} yields false.
       contract: TestGitLabPRStatusReportsTargetBranch — {"state":"opened",
                 "target_branch":"milestone/v1.2"} yields BaseRef "milestone/v1.2"; this is
                 the exact field t-6's retarget comparison consumes.
       contract: TestGitLabPRStatusEndpoint — the handler asserts the path is
                 /projects/owner%2Frepo/merge_requests/<PRNumber>; a sub-case with
                 ProjectID set asserts the numeric project ref instead of the escaped path.
       contract: TestGitLabPRStatusHTTP404IsError and TestGitLabPRStatusNeedsPRNumber —
                 both yield errors rather than (false, nil), so mergeGate announces and falls
                 back instead of reading a lookup failure as "not merged".
       contract: merged_test's TestPRStatusUnsupportedProvider table drops "gitlab".

  t-5  Forgejo/Gitea authoritative PR status + base ref
       files:    internal/ship/forgejo.go, internal/ship/forgejo_test.go,
                 internal/ship/merged.go, internal/ship/merged_test.go
       covers:   c-2, c-5
       depends:  t-1, t-3
       desc:     forgejoPRStatus does GET /repos/{owner}/{repo}/pulls/{index} through
                 forgejoTarget + jsonGet, mapping the "merged" boolean to Merged and base.ref
                 to BaseRef. PRStatus dispatch routes forgejo and gitea to it.
       contract: TestForgejoPRStatusUsesMergedFlag — {"merged":true,"state":"closed",
                 "base":{"ref":"main"}} yields {Merged:true, BaseRef:"main"}. Gitea reports a
                 merged PR as state "closed", so an implementation reading state alone reports
                 a landed PR as unmerged and fails here.
       contract: TestForgejoPRStatusClosedUnmergedIsNotMerged — {"merged":false,
                 "state":"closed"} yields Merged false, so a declined PR refuses completion.
       contract: TestForgejoPRStatusOpenPopulatesBaseRef — an open PR yields Merged false with
                 BaseRef still read from base.ref, which is what makes the retarget check work
                 on an unmerged PR.
       contract: TestForgejoPRStatusGiteaAlias — Provider "gitea" and "Gitea" reach the same
                 implementation.
       contract: TestForgejoPRStatusEndpointAndAuth — the handler asserts the path
                 /repos/{owner}/{repo}/pulls/<PRNumber> and an "Authorization: token" header.
       contract: TestForgejoPRStatusHTTP500IsError — a non-2xx yields an error, not
                 (false, nil).
       contract: merged_test's TestPRStatusUnsupportedProvider table drops
                 "forgejo"/"gitea"/"Forgejo", leaving it asserting only genuinely unknown
                 providers.

  t-6  mergeGate announces fallback, refuses retargeted base
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go, ARCHITECTURE.md
       covers:   c-3, c-5
       depends:  t-1
       desc:     mergeGate prints a reason line on both fall-through paths (unsupported
                 provider at phase.go:741, provider error at :743) in printOpenPRSkip's shape,
                 and — before the merged short-circuit at :735 — compares PRStatus.BaseRef
                 against reconcileBranch, refusing completion when they differ. An empty
                 BaseRef announces a skipped retarget check and proceeds. ARCHITECTURE.md's
                 provider index lines (306, 361) are corrected in the same commit.
       contract: TestMergeGateAnnouncesUnsupportedProvider — stub PRStatusFunc to return
                 ErrMergeStatusUnsupported; captureStdout contains a line naming the provider
                 and the git-ancestry fallback. Deleting the Printf leaves stdout empty and
                 fails the assert — this is the silent-degrade path c-3 names.
       contract: TestMergeGateAnnouncesProviderError — stub returns a network error; the
                 printed line contains that error's text, so a generic "check skipped"
                 message that swallows the cause fails the substring assert.
       contract: TestMergeGateHappyPathIsSilent — {Merged:true, BaseRef:"main"} with
                 reconcileBranch "main" returns nil and prints nothing, so the announce
                 doesn't become background noise on every successful completion.
       contract: TestPhaseCompleteRefusesRetargetedBase — stub returns {Merged:false,
                 BaseRef:"other"} with reconcileBranch "main"; `dross phase complete` exits
                 non-zero, the error names both "other" and "main", and HEAD is still on
                 phase/<id> afterwards.
       contract: TestPhaseCompleteRefusesRetargetedBaseEvenWhenMerged — {Merged:true,
                 BaseRef:"other"} still refuses. An implementation that puts the retarget
                 comparison after the `merged → return nil` short-circuit passes the previous
                 test and fails this one.
       contract: TestMergeGateEmptyBaseRefAnnouncesSkip — {Merged:true, BaseRef:""} (bitbucket,
                 or a provider that errored) proceeds and prints a "base-retarget check
                 skipped" line, so the unknown-base case is announced rather than refused.
       contract: TestMergeGateNoRecordedPRStillFallsBack — recordedPR == 0 takes the ancestry
                 path with no provider call and no announce, preserving the existing
                 TestPhaseCompleteRefusesUnmergedNoLocalBranch behaviour.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 GitLab authoritative PRMerged | t-4 |
| c-2 Forgejo/Gitea authoritative PRMerged | t-5 |
| c-3 mergeGate announces ancestry fallback | t-6 |
| c-4 GitLab + Forgejo/Gitea OpenPRsTargeting | t-2, t-3 |
| c-5 Base-retarget detection across all 4 providers | t-1 (github + seam), t-4 (gitlab), t-5 (forgejo/gitea), t-6 (comparison + refusal) |

All 5 criteria covered. Locked decisions honoured: retarget_scope (t-1/t-4/t-5 supply BaseRef
for all four named providers), retarget_response (t-6 refuses, does not warn-and-continue),
retarget_timing (the base ref rides on the single PRStatus call mergeGate already makes — no
second API round trip, no ship-time check).

## Judgment calls

- **Replaced `PRMerged`/`PRMergedFunc` outright with `PRStatus`/`PRStatusFunc` returning
  `{Merged, BaseRef}`.** Rejected: a separate per-provider base-ref lookup alongside the
  existing seam. The locked `retarget_timing` decision says the check piggybacks on mergeGate's
  existing provider query, which means one round trip; and `PRMergedFunc` has exactly one
  production caller (`internal/cmd/phase.go:733`), so keeping it as a wrapper would be dead code.
- **Split by API surface (open-PR lookup in wave 1, merged-status in wave 2) rather than by
  provider vertical.** Rejected: one task per provider doing both surfaces. The surface split
  puts the shared plumbing each provider needs — `gitlabTarget`, `forgejoTarget`, and the
  `jsonGet` helper Forgejo lacks entirely — in wave 1 where wave 2 consumes it, and it makes
  each task map to exactly one criterion, so a red task names a red criterion.
- **Forgejo/Gitea open-PR filtering is client-side on `base.ref`.** Rejected: passing a `base=`
  query param. Gitea's list-pulls endpoint ignores unknown params, so a server-side filter would
  silently return PRs targeting other branches — which for the branch-delete guard means a false
  "no dependents" on an irreversible delete. The filter is asserted by a dedicated test rather
  than assumed.
- **The retarget comparison runs before the merged short-circuit.** Rejected: checking only on
  the unmerged path. A PR that was retargeted and then merged into the wrong base is precisely
  the case that completes a phase against a base that never received the work; a dedicated test
  pins the ordering.
- **Empty `BaseRef` announces a skip instead of refusing.** Rejected: treating `""` as a
  mismatch. Bitbucket (outside the four providers `retarget_scope` names) and any errored lookup
  yield `""`; refusing there would break Bitbucket completion for a check the spec does not ask
  of it. The announce keeps it non-silent, satisfying c-3's spirit.
- **Bitbucket gains merge status parity only, no base ref.** Rejected: populating it from the
  payload `bitbucketPRMerged` already fetches, which would be nearly free. `retarget_scope` names
  four providers and Bitbucket is not one of them; that is a separate decision to make explicitly.
- **ARCHITECTURE.md's stale provider lines (306, 361) are fixed inside t-6, not as their own
  task.** A doc edit carries no test contract, and a standalone one-file sub-ten-minute task is
  exactly what the granularity rules reject.
- **Accepted a two-line dispatch collision between sibling tasks.** t-2/t-3 each add one case arm
  to `basepr.go` and one row to `basepr_test.go`'s unsupported-provider table; t-4/t-5 do the same
  in `merged.go`/`merged_test.go`. Rejected: a wave-0 task creating stub provider files so the
  waves are file-disjoint — that ceremony costs more than the collision it avoids.
