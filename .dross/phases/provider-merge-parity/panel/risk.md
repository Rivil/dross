# Plan draft — lens: risk / failure modes

Every task below owns exactly one class of failure. The ordering principle is
**what can silently lie to the completion gate**, ranked:

1. A provider reporting `merged` for a PR that was *closed/declined* — false-completes a
   phase whose work was thrown away (bitbucket's `merged.go:65-70` comment already names
   this trap; gitlab `closed`/`locked` and gitea `state:closed, merged:false` are the same
   shape).
2. A lookup failure collapsing into a *value* — `false, nil` on a 401 produces a hard
   refusal blaming the user for an unmerged PR; an empty `[]BasePR` on a 500 authorizes an
   irreversible branch delete (`basepr.go:33-34`).
3. Truncated pagination — a dependent MR/PR on page 2 is invisible, so the delete guard
   passes when it must not.
4. A degrade nobody sees (c-3) and a stale base nobody checks (c-5).

---

```
Phase provider-merge-parity — 6 tasks across 4 waves

Wave 1
  t-1  Replace PRMerged with a PRStatus struct
       files:    internal/ship/merged.go, internal/ship/merged_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5
       contract: gh --json arg list must carry baseRefName; a DECLINED bitbucket PR
                 reports Merged=false with nil error

Wave 2 (depends t-1)
  t-2  GitLab MR status and open-MR-by-target
       files:    internal/ship/gitlab.go, internal/ship/gitlab_test.go,
                 internal/ship/merged.go, internal/ship/basepr.go
       covers:   c-1, c-4, c-5
       contract: state="closed" -> Merged=false,nil; HTTP 401 -> error; page-2 MRs
                 are returned and every request carries target_branch=<base>

  t-3  Forgejo/Gitea PR status and open-PR-by-base
       files:    internal/ship/forgejo.go, internal/ship/forgejo_test.go,
                 internal/ship/merged.go, internal/ship/basepr.go
       covers:   c-2, c-4, c-5
       contract: {state:closed, merged:false} -> Merged=false,nil; listing filters
                 client-side on base.ref; a mid-listing 500 returns an error, never
                 a partial slice

  t-4  Announce every mergeGate degrade path
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-3
       contract: unsupported-provider, provider-error and no-recorded-PR each print a
                 distinct line; the authoritative merged path prints nothing

Wave 3 (depends t-4)
  t-5  Refuse completion on a retargeted base
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5
       contract: BaseRef differing from the recorded base refuses with HEAD still on
                 phase/<id>, even when Merged=true; an empty BaseRef proceeds

Wave 4 (depends t-2, t-3, t-5)
  t-6  Cross-provider retarget matrix test
       files:    internal/ship/retarget_matrix_test.go,
                 internal/cmd/phase_retarget_matrix_test.go
       covers:   c-5
       contract: each provider's own base JSON field (baseRefName / target_branch /
                 base.ref / destination.branch.name) drives the same refuse verdict
```

---

## Task detail

### t-1 — Replace PRMerged with a PRStatus struct
- **wave** 1 · **depends_on** [] · **status** pending
- **files**: `internal/ship/merged.go`, `internal/ship/merged_test.go`,
  `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- **description**: Introduce `type PRStatus struct { Merged bool; BaseRef string }` and
  `func PRLookup(opts OpenOpts) (PRStatus, error)` with the overridable
  `PRLookupFunc` seam, deleting `PRMerged`/`PRMergedFunc` outright. Port the github
  (`--json state,mergedAt,baseRefName`) and bitbucket (`destination.branch.name`) impls;
  `ErrMergeStatusUnsupported` stays the sentinel for the not-yet-wired cases. Update
  `mergeGate` and `stubPRMerged` to the new seam.
- **covers**: `["c-5"]`
- **test_contract**:
  - If the github path stops asking `gh` for `baseRefName`, `TestPRLookupGitHubRequestsBaseRef`
    fails — it stubs `ghCommand` and asserts the recorded `--json` argument contains
    `baseRefName` alongside `state`.
  - If a bitbucket PR in state `DECLINED` or `SUPERSEDED` is reported merged,
    `TestPRLookupBitbucketDeclinedNotMerged` fails (httptest serves
    `{"state":"DECLINED"}`; expects `Merged=false`, `err=nil`).
  - If `PRLookupFunc` stops defaulting to `PRLookup`, `TestPRLookupFuncDefault` fails.
  - If gitlab/forgejo/gitea stop returning the sentinel at this point,
    `TestPRLookupUnsupportedProvider` (ported from `merged_test.go`) fails.
- **why one task**: a two-step migration leaves the tree non-compiling between commits.
  Deleting `PRMerged` rather than adding a parallel `PRStatus` makes the *compiler* the
  guarantee that no provider can answer "merged" without also answering "base" — the
  single largest silent-gap risk in c-5.

### t-2 — GitLab MR status and open-MR-by-target
- **wave** 2 · **depends_on** `["t-1"]` · **status** pending
- **files**: `internal/ship/gitlab.go`, `internal/ship/gitlab_test.go`,
  `internal/ship/merged.go`, `internal/ship/basepr.go`
- **description**: New `gitlab.go` holding `gitlabPRLookup` (GET
  `/projects/{ref}/merge_requests/{iid}`, map `state=="merged"` only, capture
  `target_branch`) and `gitlabOpenMRsTargeting` (GET `/merge_requests?state=opened&
  target_branch=<base>` with explicit `per_page`/`page` looping). Both reuse `gitlabReq`,
  `gitlabProjectRef` and the missing-APIBase/AuthEnv/token precondition errors from
  `openGitLabPR`. Swap the two dispatch cases.
- **covers**: `["c-1", "c-4", "c-5"]`
- **test_contract**:
  - If `state` mapping widens to `!= "opened"`, `TestGitLabPRLookupClosedNotMerged` fails —
    httptest serves `{"state":"closed"}` and `{"state":"locked"}`, both must yield
    `Merged=false, err=nil`.
  - If an HTTP 401/404 is collapsed into `Merged=false, nil`,
    `TestGitLabPRLookupHTTPErrorIsError` fails.
  - If the auth-scheme branch breaks, `TestGitLabPRLookupAuthHeader` fails — the fake
    server asserts `PRIVATE-TOKEN` by default and `Authorization: Bearer` when
    `AuthScheme="bearer"`.
  - If the listing drops the `target_branch` query param or stops after the first page,
    `TestGitLabOpenMRsPaginates` fails — the server returns a full page then a short page
    and asserts `target_branch=<base>` on every request; the result must contain both
    pages' MRs.
  - If a 500 on page 2 returns page 1's slice with a nil error,
    `TestGitLabOpenMRsPartialPageIsError` fails.
  - If `APIBase`/`AuthEnv`/`$token` are missing and an HTTP call is attempted anyway,
    `TestGitLabLookupPreconditions` fails (no request should reach the fake server).

### t-3 — Forgejo/Gitea PR status and open-PR-by-base
- **wave** 2 · **depends_on** `["t-1"]` · **status** pending
- **files**: `internal/ship/forgejo.go`, `internal/ship/forgejo_test.go`,
  `internal/ship/merged.go`, `internal/ship/basepr.go`
- **description**: New `forgejo.go` with a `jsonGet(endpoint, token)` helper (mirroring
  `jsonPost`'s `Authorization: token` header and 30s client), `forgejoPRLookup` (GET
  `/repos/{o}/{r}/pulls/{index}`, merged from the `merged` bool, base from `base.ref`) and
  `forgejoOpenPRsTargeting` (GET `/repos/{o}/{r}/pulls?state=open` with `page`/`limit`
  looping and **client-side** `base.ref` filtering, since older Gitea has no base filter).
  Swap the two dispatch cases for both `forgejo` and `gitea`.
- **covers**: `["c-2", "c-4", "c-5"]`
- **test_contract**:
  - If merged is derived from `state=="closed"` instead of the `merged` field,
    `TestForgejoPRLookupClosedUnmerged` fails — server returns
    `{"state":"closed","merged":false}`, expects `Merged=false, err=nil`.
  - If `base.ref` extraction breaks, `TestForgejoPRLookupReportsBaseRef` fails.
  - If client-side base filtering is dropped, `TestForgejoOpenPRsFiltersByBase` fails —
    the server returns PRs against two different bases and only the matching ones may be
    returned.
  - If the listing stops paginating, `TestForgejoOpenPRsPaginates` fails (full page then
    short page; both pages' PRs must appear).
  - If any failure yields `nil, nil` or a partial slice with a nil error,
    `TestForgejoOpenPRsErrorNeverEmptySlice` fails — a 500 must produce a non-nil error.
  - If the `gitea` provider string stops routing to the same impl as `forgejo`,
    `TestForgejoLookupCoversGiteaAlias` fails.

### t-4 — Announce every mergeGate degrade path
- **wave** 2 · **depends_on** `["t-1"]` · **status** pending
- **files**: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- **description**: Add `printMergeGateFallback(reason string)` next to `mergeGate`,
  modelled on `printOpenPRSkip` (`milestone_dependents.go:107-109`), and call it at the
  three degrade points: `ErrMergeStatusUnsupported`, a generic provider/network error,
  and `recordedPR == 0`. The authoritative merged/unmerged branches print nothing.
- **covers**: `["c-3"]`
- **test_contract**:
  - If the `ErrMergeStatusUnsupported` fall-through goes silent,
    `TestMergeGateAnnouncesUnsupportedProvider` fails — `captureStdout` must see a line
    naming the provider and the ancestry fallback.
  - If a network/API error falls through silently,
    `TestMergeGateAnnouncesProviderError` fails, and the printed line must contain the
    underlying error text (not a generic "provider unavailable").
  - If the announce leaks onto the authoritative path,
    `TestMergeGateSilentOnAuthoritativeAnswer` fails — with a stub returning
    `Merged=true`, stdout must contain no fallback line.
  - If `recordedPR == 0` produces no announcement,
    `TestMergeGateAnnouncesNoRecordedPR` fails; its message must be distinguishable from
    the unsupported-provider one so a missing record isn't misread as a provider gap.

### t-5 — Refuse completion on a retargeted base
- **wave** 3 · **depends_on** `["t-4"]` · **status** pending
- **files**: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- **description**: Inside `mergeGate`, after the single `PRLookupFunc` call and **before**
  the merged/unmerged verdict, compare the returned `BaseRef` against the recorded base
  (`changes.Base`, falling back to `reconcileBranch` when empty). Normalization
  (`refs/heads/` strip, exact case-sensitive name compare) lives here, in one place.
  A non-empty mismatch refuses with a message naming both branches; an empty `BaseRef`,
  an empty recorded base, or a lookup error announces via t-4's helper and continues.
- **covers**: `["c-5"]`
- **test_contract**:
  - If a retargeted PR is allowed through, `TestPhaseCompleteRefusesRetargetedBase`
    fails — stub `PRLookupFunc` returns `{Merged:true, BaseRef:"main"}` against a recorded
    base of `milestone/v1.2`; complete must return an error naming both branches and leave
    HEAD on `phase/<id>` with no local ref moved.
  - If the retarget check is placed after the merged-return,
    `TestPhaseCompleteRefusesRetargetedEvenWhenMerged` fails (same stub, `Merged=true`).
  - If an empty `BaseRef` is treated as a retarget,
    `TestPhaseCompleteProceedsOnUnknownBaseRef` fails — this is the outage-class inverse:
    a false alarm would block every completion on a provider.
  - If an empty recorded `changes.Base` refuses instead of falling back to
    `reconcileBranch`, `TestRetargetSkipsWhenBaseUnrecorded` fails.
  - If a provider error starts producing a retarget refusal rather than the announced
    ancestry fallback, `TestMergeGateRetargetSkipsOnProviderError` fails.
  - If `refs/heads/main` vs `main` is reported as a retarget,
    `TestRetargetNormalizesRefsHeads` fails.

### t-6 — Cross-provider retarget matrix test
- **wave** 4 · **depends_on** `["t-2", "t-3", "t-5"]` · **status** pending
- **files**: `internal/ship/retarget_matrix_test.go`,
  `internal/cmd/phase_retarget_matrix_test.go`
- **description**: Table test asserting each provider's *raw wire shape* flows into a
  correct `PRStatus.BaseRef`, plus a cmd-level table asserting the same refuse verdict for
  all four. github via stubbed `ghCommand` JSON, gitlab/forgejo/bitbucket via httptest
  servers returning their real field names.
- **covers**: `["c-5"]`
- **test_contract**:
  - If any provider's base field is misnamed or dropped,
    `TestPRLookupBaseRefAcrossProviders` fails — the table feeds `baseRefName` (github),
    `target_branch` (gitlab), `base.ref` (forgejo/gitea) and
    `destination.branch.name` (bitbucket) and expects `BaseRef=="release/x"` from each.
  - If a provider is wired for merged-status but not base-ref,
    `TestRetargetRefusesForEveryProvider` fails — all four providers, same retargeted
    fixture, all four must refuse completion.
- **why a separate task**: this is the only place the per-provider JSON extraction (t-1/t-2/t-3)
  meets the comparator (t-5). Neither side's own tests can catch a field wired to the
  wrong provider — precisely the gap the locked `retarget_scope` decision exists to close.

---

## Coverage

| criterion | tasks |
|---|---|
| c-1 GitLab authoritative PRMerged | t-1 (contract), t-2 |
| c-2 Forgejo/Gitea authoritative PRMerged | t-1 (contract), t-3 |
| c-3 mergeGate announces every fallback | t-4 |
| c-4 GitLab + Forgejo/Gitea OpenPRsTargeting | t-2, t-3 |
| c-5 Base-retarget detection, all 4 providers | t-1, t-2, t-3, t-5, t-6 |

All 5 criteria covered.

---

## Judgment calls

- **Deleted `PRMerged` instead of adding a parallel `PRStatus`.** Rejected the additive
  seam: it lets a provider implement merged-status while silently never reporting a base,
  which is exactly the hole `retarget_scope` (all 4 providers) was locked to close. A
  single struct-returning lookup makes the compiler enforce parity. Costs a mechanical
  update to `stubPRMerged` and its ~6 call sites.
- **One file per provider (`gitlab.go`, `forgejo.go`) rather than extending
  `merged.go`/`basepr.go` in place.** Two parallel wave-2 tasks editing the same function
  bodies is a guaranteed conflict; this way each touches one dispatch *case line* in the
  shared files and owns its own file. Also keeps each provider's distinct failure
  vocabulary (state words, auth header, pagination shape) in one reviewable place.
- **Retarget check runs before the merged/unmerged verdict.** Rejected checking only on
  the not-merged path: a PR merged into a *different* base is the more dangerous case —
  completing it would ff-merge the phase into a branch the work never landed on.
- **Unknown/empty `BaseRef` fails open, a known mismatch fails closed.** Rejected treating
  an unanswerable base as suspicious: a false retarget alarm blocks *every* completion on
  that provider, which is worse than the stale-base risk c-5 targets. The unknown case
  routes through t-4's announce so it is never silent.
- **Explicit pagination in both listing implementations, not a single first-page fetch.**
  Rejected the smaller version: GitLab defaults to 20 per page and Gitea to 30, so a repo
  with more open PRs than that would let `milestone prune` delete a branch with live
  dependents — a silent unsafe pass, not a visible failure.
- **Did not add a task to tighten `printOpenPRSkip`'s error swallowing.** It currently
  turns an auth failure into a skip that authorizes a branch delete, which is a genuine
  risk — but the previously-locked `dependent_detection` decision (recorded in
  `basepr.go:12-15`) makes an announced skip a first-class outcome, and reversing it is
  out of this phase's criteria. Flagging it for the lead as a deferred candidate rather
  than silently absorbing it.
- **No escape hatch added for a legitimate retarget.** `--recover` does not bypass
  `mergeGate` today (`TestPhaseCompleteRecoverUnmergedPRRefusesBeforeReset`), so a user
  whose PR was deliberately retargeted has no flag to proceed. t-5's refusal message must
  therefore name a concrete remedy (re-point the PR, or re-run `dross ship` so the record
  is rewritten). Rejected adding a `--allow-retarget` flag as scope the spec did not ask
  for — but this is the one place the plan could brick a real user, so it is worth the
  lead's explicit ruling.
