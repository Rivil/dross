# provider-merge-parity — panel synthesis

Judge did not author any draft. Skeleton picked on merit, grafts named by origin.

## Scores

| dimension | risk | mvp | verification |
|---|---|---|---|
| criteria coverage | 5/5, c-5 traced through 5 tasks — but leaks Bitbucket base-ref into c-5, which the locked `retarget_scope` does not name | 5/5, but c-3 and c-5's gate half collapse into one task, so a red task no longer names a red criterion | **5/5 with the cleanest 1:1 map** — t-2/t-3→c-4, t-4→c-1, t-5→c-2, t-6→c-3+c-5; every task traces to one surface |
| test-contract specificity | strong on *failure modes*: pagination, 500-must-never-return-an-empty-slice, `refs/heads/` normalization — weaker on wire field names | thinnest — contracts named but no pagination, no error-vs-value discipline on the listings, no endpoint assertions | **strongest** — exact payloads, exact field names (`iid` not `id`, `head.ref` not `head.label`, gitea's `merged` flag not `state`), and each contract names the wrong implementation it kills |
| granularity | over-split at the gate: t-4 (announce) and t-5 (refuse) are sequential edits to one function with a declared dependency between them | t-4 too coarse — two criteria, two distinct behaviours, one commit | **even** — one provider × one surface per task; gate work bundled once |
| wave correctness | 4 waves; every dependency is real, but t-4→t-5 serializes what is one function body. Only draft with a cross-provider integration wave | 2 waves, minimal — but wave 2's three tasks all edit `merged.go`+`basepr.go` with no acknowledgement of the collision | 2 waves; the plumbing-before-consumer split (`gitlabTarget`/`forgejoTarget`/`jsonGet` in wave 1) is the genuine dependency, and it names and accepts the dispatch collision. **Missing** an integration wave |

**Skeleton: verification.** It has the only decomposition where a failing task names a single failing criterion, and its contracts are the only ones that pin provider wire shapes precisely enough to fail a plausible-but-wrong implementation (gitea reporting a merged PR as `state:"closed"` is the case that would otherwise ship broken). Its two structural gaps — no pagination, no cross-provider integration test — are exactly what risk supplies.

## Merged plan

**7 tasks across 3 waves.**

```
Wave 1
  t-1  Swap PRMerged seam for PRStatus                    [all three]
       files:    internal/ship/merged.go, internal/ship/merged_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5 (github slice + seam)
       depends:  —
       desc:     Replace PRMerged/PRMergedFunc with PRStatus(opts) (PRStatus, error) /
                 PRStatusFunc, PRStatus = {Merged bool; BaseRef string}. GitHub adds
                 baseRefName to its `gh pr view --json` list. Bitbucket migrates to the
                 struct return with an empty BaseRef and no new base work. mergeGate and
                 stubPRMerged move onto the new seam in the same commit.
       contract: TestPRStatusGitHubReturnsBaseRef — canned ghCommand output
                 {"state":"MERGED","mergedAt":"…","baseRefName":"main"} yields
                 {Merged:true, BaseRef:"main"}, AND the recorded ghCommand args contain
                 "state,mergedAt,baseRefName"; dropping the field fails it.   [verification+mvp]
       contract: TestPRStatusGitHubClosedIsNotMerged — "CLOSED" yields Merged false; a
                 `state != "OPEN"` impl passes today's tests and fails this one. [verification]
       contract: TestPRStatusBitbucketMergedHasEmptyBaseRef — MERGED payload yields
                 Merged true, BaseRef "" — an unpopulated base is empty, never guessed. [verification]
       contract: TestPRStatusFuncDefaultsToPRStatus — exported var non-nil, delegates. [risk+verification]
       contract: TestPRStatusUnsupportedProvider — gitlab/forgejo/gitea still return the
                 sentinel at this point; the table shrinks only in t-4/t-5.          [risk]
       contract: Existing TestPhaseCompleteRefusesUnmergedUpstream and
                 TestPhaseCompleteMergeGateRefusalLeavesHEADOnPhase keep passing through
                 the renamed stub; a missed call site fails `go build ./internal/cmd`. [verification]

  t-2  GitLab open-MRs by target branch                   [verification, paginated per risk]
       files:    internal/ship/gitlab.go (new), internal/ship/gitlab_test.go (new),
                 internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-4 (gitlab)
       depends:  —
       desc:     New gitlab.go holding gitlabTarget(opts) — the APIBase/AuthEnv/token/
                 splitOwnerRepo/gitlabProjectRef preamble currently inlined in openGitLabPR
                 — and gitlabOpenMRsTargeting via
                 GET /projects/{ref}/merge_requests?state=opened&target_branch={base},
                 with explicit per_page/page looping. OpenPRsTargeting dispatches gitlab.
       contract: TestGitLabOpenPRsTargetingUsesIID — [{"iid":7,"id":901}] yields
                 BasePR.Number == 7; an impl reading "id" returns 901 and fails. [verification]
       contract: TestGitLabOpenPRsTargetingFieldMapping — web_url→URL,
                 source_branch→HeadRefName.                                    [verification]
       contract: TestGitLabOpenPRsTargetingQueryAndAuth — handler asserts state=opened and
                 target_branch=<base> on the query, plus PRIVATE-TOKEN; a bearer sub-case
                 asserts Authorization: Bearer and no PRIVATE-TOKEN.     [verification+mvp]
       contract: TestGitLabOpenMRsPaginates — server returns a full page then a short page;
                 both pages' MRs must appear and every request must carry
                 target_branch=<base>. GitLab defaults to 20/page, so a first-page-only
                 impl lets `milestone prune` delete a branch with live dependents. [risk]
       contract: TestGitLabOpenPRsTargetingHTTP500IsError — a 500 (including on page 2)
                 yields a non-nil error and a nil slice, never an empty or partial slice;
                 an empty slice reads as "no dependents" and authorizes an irreversible
                 branch delete.                                            [risk+verification]
       contract: TestGitLabOpenPRsTargetingMissingToken — with $AuthEnv unset the error
                 mentions `dross env set` and the httptest server records zero requests. [verification+risk]
       contract: basepr_test's unsupported-provider table no longer lists "gitlab";
                 bitbucket still yields ErrBasePRLookupUnsupported.            [verification]

  t-3  Forgejo/Gitea open-PRs by base                     [verification, paginated per risk]
       files:    internal/ship/forgejo.go (new), internal/ship/forgejo_test.go (new),
                 internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-4 (forgejo/gitea)
       depends:  —
       desc:     New forgejo.go holding a jsonGet helper (Authorization: token, 30s client,
                 status check — only jsonPost exists today), forgejoTarget(opts), and
                 forgejoOpenPRsTargeting via GET /repos/{owner}/{repo}/pulls?state=open
                 with page/limit looping and client-side base.ref filtering. Dispatch
                 routes both forgejo and gitea.
       contract: TestForgejoOpenPRsTargetingFiltersByBaseRef — server returns three open
                 PRs, two on base.ref "main" and one on "dev"; exactly the two come back.
                 Gitea's list-pulls endpoint has no base filter, so an impl trusting a
                 `base=` query param returns the dev PR and fails.     [verification+mvp+risk]
       contract: TestForgejoOpenPRsTargetingFieldMapping — number→Number, html_url→URL,
                 head.ref (not head.label, which carries an owner prefix)→HeadRefName. [verification]
       contract: TestForgejoOpenPRsTargetingAuthHeader — handler asserts
                 "Authorization: token <tok>" and state=open in the query.  [verification+mvp]
       contract: TestForgejoOpenPRsPaginates — full page then short page; both pages'
                 PRs must appear. Gitea defaults to 30/page.                     [risk]
       contract: TestForgejoOpenPRsErrorNeverEmptySlice — a 404/500, including mid-listing,
                 yields an error and a nil slice, never a partial result. [risk+verification]
       contract: TestForgejoOpenPRsTargetingGiteaAlias — Provider "gitea" and "Gitea" reach
                 the same handler, exercising the configenum.Normalize path rather than
                 the "forgejo" literal.                                 [verification+risk]
       contract: basepr_test's unsupported-provider table no longer lists forgejo/gitea. [verification]

Wave 2
  t-4  GitLab authoritative MR status + target branch     [verification]
       files:    internal/ship/gitlab.go, internal/ship/gitlab_test.go,
                 internal/ship/merged.go, internal/ship/merged_test.go
       covers:   c-1, c-5 (gitlab)
       depends:  t-1, t-2
       desc:     gitlabPRStatus does GET /projects/{ref}/merge_requests/{iid} through
                 gitlabTarget + gitlabReq, mapping state == "merged" to Merged and
                 target_branch to BaseRef. PRStatus dispatch routes gitlab.
       contract: TestGitLabPRStatusMerged — {"state":"merged","target_branch":"main"} yields
                 {Merged:true, BaseRef:"main"}, replacing today's sentinel.  [verification]
       contract: TestGitLabPRStatusClosedIsNotMerged — {"state":"closed"} AND
                 {"state":"locked"} both yield Merged false, err nil. A `state != "opened"`
                 impl completes a phase whose MR was closed without landing. [risk+verification]
       contract: TestGitLabPRStatusOpenedIsNotMerged — {"state":"opened"} yields false. [verification]
       contract: TestGitLabPRStatusReportsTargetBranch — {"target_branch":"milestone/v1.2"}
                 yields that BaseRef; this is the exact field t-6 consumes.  [verification]
       contract: TestGitLabPRStatusEndpoint — handler asserts the path is
                 /projects/owner%2Frepo/merge_requests/<PRNumber>; a ProjectID sub-case
                 asserts the numeric ref instead.                            [verification]
       contract: TestGitLabPRStatusAuthHeader — PRIVATE-TOKEN by default, Bearer when
                 AuthScheme is "bearer".                                          [risk]
       contract: TestGitLabPRStatusHTTP404IsError and TestGitLabPRStatusNeedsPRNumber —
                 both yield errors rather than (false, nil), so mergeGate announces and
                 falls back instead of reading a lookup failure as "not merged". A 401
                 collapsed to false produces a hard refusal blaming the user. [risk+verification]
       contract: merged_test's TestPRStatusUnsupportedProvider table drops "gitlab". [verification]

  t-5  Forgejo/Gitea authoritative PR status + base ref   [verification]
       files:    internal/ship/forgejo.go, internal/ship/forgejo_test.go,
                 internal/ship/merged.go, internal/ship/merged_test.go
       covers:   c-2, c-5 (forgejo/gitea)
       depends:  t-1, t-3
       desc:     forgejoPRStatus does GET /repos/{owner}/{repo}/pulls/{index} through
                 forgejoTarget + jsonGet, mapping the "merged" boolean to Merged and
                 base.ref to BaseRef. PRStatus dispatch routes forgejo and gitea.
       contract: TestForgejoPRStatusUsesMergedFlag — {"merged":true,"state":"closed",
                 "base":{"ref":"main"}} yields {Merged:true, BaseRef:"main"}. Gitea reports
                 a merged PR as state "closed", so an impl reading state alone calls a
                 landed PR unmerged and fails here.                  [verification+risk+mvp]
       contract: TestForgejoPRStatusClosedUnmergedIsNotMerged — {"merged":false,
                 "state":"closed"} yields Merged false, so a declined PR refuses
                 completion rather than false-completing discarded work. [verification+risk]
       contract: TestForgejoPRStatusOpenPopulatesBaseRef — an open PR yields Merged false
                 with BaseRef still read from base.ref; this is what makes the retarget
                 check work on an unmerged PR.                               [verification]
       contract: TestForgejoPRStatusGiteaAlias — "gitea" and "Gitea" reach the same impl. [verification+risk+mvp]
       contract: TestForgejoPRStatusEndpointAndAuth — handler asserts
                 /repos/{owner}/{repo}/pulls/<PRNumber> and Authorization: token. [verification]
       contract: TestForgejoPRStatusHTTP500IsError — non-2xx yields an error, not
                 (false, nil).                                          [verification+risk]
       contract: merged_test's unsupported table drops forgejo/gitea/Forgejo, leaving it
                 asserting only genuinely unknown providers.                 [verification]

  t-6  mergeGate announces fallback, refuses retargeted base   [verification+mvp; risk's
                                                                two-task split rejected]
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go, ARCHITECTURE.md
       covers:   c-3, c-5 (comparison + refusal)
       depends:  t-1
       desc:     mergeGate prints a reason line on both fall-through paths
                 (ErrMergeStatusUnsupported at phase.go:741, generic error at :743) in
                 printOpenPRSkip's shape, and — before the merged short-circuit at :733 —
                 compares PRStatus.BaseRef against the RECORDED base (changes.Base,
                 falling back to reconcileBranch when empty), refusing completion when
                 they differ. Normalization (strip refs/heads/, exact case-sensitive
                 compare) lives here, in one place. An empty BaseRef announces a skipped
                 retarget check and proceeds. ARCHITECTURE.md's stale provider index
                 lines (306 OpenPRsTargeting, 361 ship.PRMergedFunc) are corrected in
                 the same commit — verified stale against the current file.
       contract: TestMergeGateAnnouncesUnsupportedProvider — stub PRStatusFunc to return
                 ErrMergeStatusUnsupported; captureStdout contains a line naming the
                 provider and the git-ancestry fallback.                [all three]
       contract: TestMergeGateAnnouncesProviderError — stub a network error; the printed
                 line contains that error's text, so a generic "check skipped" that
                 swallows the cause fails the substring assert.      [verification+risk+mvp]
       contract: TestMergeGateHappyPathIsSilent — {Merged:true, BaseRef:"main"} against a
                 recorded base of "main" returns nil and prints nothing, so the announce
                 doesn't become noise on every successful completion.  [verification+risk]
       contract: TestPhaseCompleteRefusesRetargetedBase — stub {Merged:false,
                 BaseRef:"other"} against recorded base "main"; complete exits non-zero,
                 the error names both branches, and HEAD is still on phase/<id>. [all three]
       contract: TestPhaseCompleteRefusesRetargetedBaseEvenWhenMerged — {Merged:true,
                 BaseRef:"other"} still refuses. An impl that puts the comparison after
                 the `merged → return nil` short-circuit passes the previous test and
                 fails this one.                                    [verification+risk+mvp]
       contract: TestMergeGateEmptyBaseRefAnnouncesSkip — {Merged:true, BaseRef:""}
                 (bitbucket, or a provider that errored) proceeds and prints a
                 "base-retarget check skipped" line. A false retarget alarm would block
                 every completion on that provider — worse than the risk c-5 targets.
                                                                    [verification+risk+mvp]
       contract: TestRetargetNormalizesRefsHeads — refs/heads/main vs main is not a
                 retarget.                                                        [risk]
       contract: TestMergeGateRetargetSkipsOnProviderError — a provider error produces the
                 announced ancestry fallback, never a retarget refusal.            [risk]
       contract: TestRetargetSkipsWhenBaseUnrecorded — an empty recorded base falls back
                 to reconcileBranch rather than refusing.                          [risk]
       contract: TestMergeGateNoRecordedPRStillFallsBack — recordedPR == 0 takes the
                 ancestry path with no provider call and no announce, preserving
                 TestPhaseCompleteRefusesUnmergedNoLocalBranch.   [verification; see D-3]
       contract: The refusal message must name a concrete remedy (re-point the PR on the
                 forge, or re-run `dross ship` so the record is rewritten) — --recover
                 does not bypass mergeGate today, so without a named remedy a deliberate
                 retarget has no path forward.                                     [risk]

Wave 3
  t-7  Cross-provider retarget matrix test                [risk]
       files:    internal/ship/retarget_matrix_test.go (new),
                 internal/cmd/phase_retarget_matrix_test.go (new)
       covers:   c-5
       depends:  t-4, t-5, t-6
       desc:     Table test asserting each provider's raw wire shape flows into a correct
                 PRStatus.BaseRef, plus a cmd-level table asserting the same refuse
                 verdict for all four spec providers. github via stubbed ghCommand JSON,
                 gitlab/forgejo via httptest servers returning their real field names.
       contract: TestPRStatusBaseRefAcrossProviders — the table feeds baseRefName (github),
                 target_branch (gitlab), base.ref (forgejo and gitea) and expects
                 BaseRef == "release/x" from each; a misnamed or dropped field fails.
       contract: TestRetargetRefusesForEveryProvider — all four spec providers, the same
                 retargeted fixture, all four refuse completion. A provider wired for
                 merged-status but not base-ref fails only here.
       why:      the only place the per-provider JSON extraction (t-1/t-4/t-5) meets the
                 comparator (t-6). Neither side's own tests catch a field wired to the
                 wrong provider — precisely the gap locked `retarget_scope` exists to close.
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 GitLab authoritative merge status | t-4 |
| c-2 Forgejo/Gitea authoritative merge status | t-5 |
| c-3 mergeGate announces every provider-degrade fallback | t-6 |
| c-4 GitLab + Forgejo/Gitea OpenPRsTargeting | t-2, t-3 |
| c-5 base-retarget across all 4 providers | t-1 (github + seam), t-4 (gitlab), t-5 (forgejo/gitea), t-6 (comparison + refusal), t-7 (cross-provider) |

5/5 covered. Locked decisions honoured: `retarget_scope` (t-1/t-4/t-5 supply BaseRef for the four named providers; t-7 proves all four), `retarget_response` (t-6 refuses, does not warn-and-continue), `retarget_timing` (the base ref rides the single PRStatus call mergeGate already makes — no second round trip, no ship-time check).

Known file collisions, accepted rather than engineered around: t-2/t-3 each add one dispatch arm to `basepr.go` and one table row to `basepr_test.go`; t-4/t-5 do the same in `merged.go`/`merged_test.go`. A wave-0 stub-file task to make the waves file-disjoint costs more than the collision. [verification]

Flagged, no task (risk raised both; neither is in any draft as a task, so neither was added): `printOpenPRSkip` still turns an auth failure into a skip that authorizes a branch delete — deferred candidate for the lead, since the locked `dependent_detection` decision makes an announced skip a first-class outcome; and there is no `--allow-retarget` escape hatch, mitigated only by t-6's remedy-naming contract.

## Disagreements

**D-1 — Is Bitbucket a fifth provider for base-retarget?**
- risk: yes in practice — t-1 asserts Bitbucket DECLINED→not-merged and t-6's matrix expects `destination.branch.name` to produce a real BaseRef.
- mvp: no — "Bitbucket carried, not extended", migrated to the struct return with no base work.
- verification: no, and asserts the negative — `TestPRStatusBitbucketMergedHasEmptyBaseRef` pins BaseRef to `""`.
- **Provisional default: exclude Bitbucket from base-ref work** (mvp+verification, and it is what the text says). Locked `retarget_scope` names github, gitlab, forgejo, gitea; Bitbucket is not among them.
- **Why it matters:** the cost is asymmetric. Populating Bitbucket's base is nearly free, so excluding it is cheap to reverse later; but including it silently widens a locked decision, and t-6's empty-BaseRef-proceeds path exists specifically to keep Bitbucket completions working. If the lead wants Bitbucket in, it is a one-line change in t-1 plus a row in t-7 — but it should be an explicit ruling, not a side effect.

**D-2 — What does the fetched BaseRef get compared against?**
- risk: the *recorded* base, `changes.Base`, falling back to `reconcileBranch` when empty.
- mvp: `reconcileBranch`.
- verification: `reconcileBranch`.
- **Provisional default: risk's — `changes.Base`, falling back to `reconcileBranch`.** The minority wins on evidence: `resolveCompleteBase` (`phase.go:645-653`) normally *returns* `changes.Base`, so the two coincide in the ordinary case — but `--base <branch>` short-circuits it at line 646, and `changes.Base` is written at ship time as "what the PR was actually opened against" (`ship.go:374`, locked `base_write_timing`).
- **Why it matters:** c-5 says "retargeted **since it was recorded**". Comparing against `reconcileBranch` means `dross phase complete --base other` fabricates a retarget refusal against a PR nobody touched, and — worse — a user who passes `--base` matching the *new* forge base would have a genuine retarget silently pass. Getting this wrong inverts the criterion in both directions.

**D-3 — Does `recordedPR == 0` get an announcement?**
- risk: yes, and its message must be distinguishable from the unsupported-provider one, so a missing record isn't misread as a provider gap.
- mvp: silent (not addressed).
- verification: explicitly no — `TestMergeGateNoRecordedPRStillFallsBack` asserts no provider call and no announce.
- **Provisional default: verification's — no announce**, taken on the literal criterion text: c-3 says "because the configured provider *can't answer authoritatively*", and a missing PR record is not a provider inability.
- **Why it matters:** this is the one place the merged plan has a contract that *forbids* output where another lens demands it, so it cannot be fudged at execution time. Risk's argument is real — an unrecorded PR falling back to ancestry is still an unseen degrade — and if the lead reads c-3 as "announce every fallback" rather than "every provider-caused fallback", `TestMergeGateNoRecordedPRStillFallsBack` must be inverted before t-6 is written, not after.

**D-4 — New per-provider files, or extend the existing dispatch files?**
- risk: new `gitlab.go` / `forgejo.go`, so each parallel task owns a file and touches only one case line in the shared ones.
- mvp: no new files — extend `merged.go` / `basepr.go` / `open.go`; a file split is a refactor no criterion asks for.
- verification: new `gitlab.go` / `forgejo.go`, with `jsonGet` living in `forgejo.go`.
- **Provisional default: new files** (risk+verification).
- **Why it matters:** four of the seven tasks run in parallel pairs that would otherwise be co-editing two ~65-line files, and each provider's distinct vocabulary (state words, auth header, pagination shape) stays in one reviewable place. mvp's objection has force on the `jsonGet` placement specifically — it sits naturally beside `jsonPost` at `open.go:334` — so if the lead prefers, `jsonGet` in `open.go` with only the forgejo-specific functions in `forgejo.go` is a clean middle.

**D-5 — Decompose by provider vertical or by API surface?**
- risk: provider vertical — one task does GitLab's merged-status *and* open-MR listing together.
- mvp: collapsed — three wave-2 tasks, one per provider group plus one gate task.
- verification: by surface — open-PR listing in wave 1, merged-status in wave 2, so the shared plumbing (`gitlabTarget`, `forgejoTarget`, `jsonGet`) lands where wave 2 consumes it.
- **Provisional default: by surface** (verification).
- **Why it matters:** the surface split is what makes each task map to exactly one criterion, so a red task names a red criterion — the property that made verification the skeleton. The cost is that GitLab's plumbing is written in t-2 and consumed in t-4 by a different commit, so if t-2's `gitlabTarget` signature is wrong, t-4 pays for it. Risk's vertical keeps each provider's work in one head at the price of two-criteria tasks.

**D-6 — Are "announce" and "refuse" one task or two?**
- risk: two — t-4 announces, t-5 refuses and depends on t-4's helper, across two waves.
- mvp: one (its t-4).
- verification: one (its t-6).
- **Provisional default: one task** (mvp+verification), carried as t-6.
- **Why it matters:** both edits land in the same ~25-line function, and risk's own split creates a wave boundary around a single function body. Against that, the merged t-6 carries two criteria (c-3 and c-5) and eleven contracts, making it the heaviest task in the plan — if execution finds it unwieldy, risk's split is the pre-vetted way to cut it, and the seam is clean (announce helper first, comparator second).

**D-7 — Do the open-PR listings paginate?**
- risk: required, with dedicated tests, in both implementations.
- mvp: not mentioned.
- verification: not mentioned.
- **Provisional default: paginate** (risk), grafted into t-2 and t-3.
- **Why it matters:** the majority is silent rather than opposed, so this is a gap not a conflict — but it is a safety gap. GitLab defaults to 20 results per page and Gitea to 30; a first-page-only listing feeding `guardMilestoneBranchDelete` returns "no dependents" for a repo with more open PRs than that, and authorizes an irreversible branch delete. It fails as a silent pass, not a visible error, which is why it needs its own test rather than a code comment.
