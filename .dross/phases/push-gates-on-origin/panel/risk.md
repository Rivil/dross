# Risk-lens draft — push-gates-on-origin

Lens: every task owns one failure mode and its test. The graph is shaped by
what breaks, not by which file is touched.

Failure modes that drive the graph (each owned by exactly one task):

| # | What breaks today | Owner |
|---|---|---|
| F1 | Record commit exists locally, not on origin; re-run stages nothing → never pushed (`ship.go:432` gates on the index) | t-6 |
| F2 | Two copies of the origin comparison drift apart (base net has one at `basebranch.go:63-86`; record push has none) | t-1 |
| F3 | Diverged phase branch: raw `git push` non-ff error, or an unguided force clobbers review commits | t-6 |
| F4 | `state.json` flips shipped at `ship.go:269` before any push; `changes.json` status flips at `ship.go:422` before the record push → failed push still reads shipped | t-8 |
| F5 | `status.go:597-600` raises "shipped:" on `ch.PR > 0` alone; `suggestNext` (`status.go:295-311`) says "merge the open PR" when the record never left the machine | t-4 |
| F6 | Re-run opens a second PR: `ship.OpenPR` is unconditional at `ship.go:371` | t-7 |
| F7 | Provider lookup error read as "no PR" → duplicate PR on a flaky network; closed PR with same head matched → stale number recorded | t-2, t-3 |
| F8 | Prompt tells the agent to never re-run ship (`ship.md:134`), so the retry path is unreachable from the workflow | t-5 |

```
Phase push-gates-on-origin — 8 tasks across 4 waves

Wave 1
  t-1  Extract shared origin-ahead comparison helper
       files:    internal/cmd/originahead.go (new), internal/cmd/originahead_test.go (new),
                 internal/cmd/basebranch.go
       covers:   c-1
       description:
                 Add compareWithOrigin(repoDir, branch) → (cmp originCmp, ahead []string, err):
                 fetches origin, then classifies into originMissing / originInSync /
                 originBehindOnly / originAheadOnly / originDiverged from the two rev-list
                 ranges basebranch.go:69-79 already run. Rewrite pushBaseIfAheadDrossOnly
                 (basebranch.go:62-109) to consume it, keeping its .dross-only filter and
                 its diverged→no-op policy in the caller. No behaviour change on the base
                 path; the 8 tests in basebranch_test.go:150-320 stay green untouched.
       contract: If the helper skips the fetch, TestOriginCmpSeesUnfetchedRemoteCommit fails:
                 a commit pushed to origin from a second clone must classify as
                 originBehindOnly, and without the fetch it reads originInSync.
                 If diverged is misreported as aheadOnly, TestOriginCmpDivergedIsNotAhead
                 fails (ahead+behind fixture built like TestPushBaseDivergedNoop) AND
                 TestPushBaseDivergedNoop fails because the base net would push into it.
                 If the helper swallows a missing origin/<branch> as an error,
                 TestOriginCmpMissingRemoteRef fails: a branch never pushed must classify
                 originMissing with err == nil (the first-ship case).
                 If the base caller loses its .dross-only filter during the refactor,
                 TestPushBaseCodeAheadRefusesAndPushesNothing fails.
                 If the helper's fetch error is dropped, TestPushBaseUnreachableOriginHardError
                 fails.
       depends_on: []

  t-2  Add FindOpenPRByHead for GitHub and Forgejo
       files:    internal/ship/headpr.go (new), internal/ship/headpr_test.go (new),
                 internal/ship/forgejo.go, internal/ship/forgejo_test.go
       covers:   c-5
       description:
                 New headpr.go: FindOpenPRByHead(opts OpenOpts, head string) (*OpenResult, error)
                 with provider dispatch mirroring OpenPRsTargeting (basepr.go:36-49), an
                 exported FindOpenPRByHeadFunc seam (like basepr.go:55), ErrHeadPRLookupUnsupported
                 sentinel, and gitHubOpenPRByHead via ghCommand
                 (`gh pr list --head <head> --state open --json number,url`). forgejoOpenPRByHead
                 in forgejo.go pages /repos/o/r/pulls?state=open like forgejoOpenPRsTargeting
                 (forgejo.go:122-166) and filters head.ref == head client-side. Not found →
                 (nil, nil); any HTTP/parse failure → (nil, err), never (nil, nil).
       contract: If a lookup failure is returned as (nil, nil), TestForgejoHeadLookupHTTP500IsError
                 fails: a 500 from the mock must yield a non-nil error (the caller would
                 otherwise create a duplicate PR on a flaky network).
                 If the closed-state filter is dropped from the query, TestForgejoHeadLookupQueriesOpenOnly
                 fails: the mock asserts state=open is in r.URL.Query().
                 If head matching is by substring or prefix, TestForgejoHeadLookupExactRef fails:
                 heads "phase/x" and "phase/x-2" both open, only #7 (head phase/x) is returned.
                 If gh's --head value escapes its flag slot, TestGitHubHeadLookupArgv fails: the
                 ghCommand stub asserts args[i]=="--head" and args[i+1]==head, and "--state","open"
                 present (same index-walk style as TestOpenPRArgvWalk in gharg_test.go).
                 If page 2 is never fetched, TestForgejoHeadLookupPaginates fails: 50 PRs on page 1
                 with the match on page 2 must still be found.
       depends_on: []

  t-3  Add FindOpenPRByHead for GitLab and Bitbucket
       files:    internal/ship/gitlab.go, internal/ship/gitlab_test.go,
                 internal/ship/bitbucket.go, internal/ship/bitbucket_test.go
       covers:   c-5
       description:
                 gitlabOpenMRByHead: GET /projects/<ref>/merge_requests?state=opened&source_branch=<head>
                 via gitlabReq (gitlab.go:195), pattern of gitlabOpenMRsTargeting (gitlab.go:135-173);
                 returns iid + web_url. bitbucketOpenPRByHead: GET /repositories/ws/slug/pullrequests
                 with q=source.branch.name="<head>" AND state="OPEN" via bbRequest (bitbucket.go:50)
                 and bbCredentials (bitbucket.go:88); returns id + links.html.href. Wire both into
                 t-2's dispatch switch (headpr.go). Both: hit → result, none → (nil, nil),
                 failure → (nil, err).
       contract: If source_branch is not URL-escaped, TestGitLabHeadLookupEscapesBranch fails:
                 head "phase/x" must arrive as source_branch=phase%2Fx in the mock's raw query.
                 If bitbucket falls back to the sentinel instead of a real query,
                 TestBitbucketHeadLookupFindsOpenPR fails: the mock returns one OPEN PR and the
                 function must return its id, not ErrHeadPRLookupUnsupported.
                 If bitbucket sends anything but Basic auth, TestBitbucketHeadLookupBasicAuth fails
                 (mirrors the auth assertion pattern in bitbucket_test.go).
                 If either provider returns (nil, nil) on HTTP 401, TestGitLabHeadLookup401IsError /
                 TestBitbucketHeadLookup401IsError fail.
                 If two GitLab MRs share the source branch (different targets), TestGitLabHeadLookupMultiple
                 fails unless the one whose target_branch == opts.BaseBranch is returned.
       depends_on: []

  t-4  Key status consumers on the durable shipped marker
       files:    internal/cmd/status.go, internal/cmd/status_test.go,
                 internal/watch/drift_test.go
       covers:   c-2
       description:
                 shippedUnmergedPhase (status.go:575-600): the record-only fallback keys on
                 ch.Status == changes.StatusShipped instead of ch.PR > 0. suggestNext
                 (status.go:283-311): after the phaseMergeState consult, when the record carries
                 a PR number but Status is neither shipped nor complete and the state is not
                 mergeMerged, return "re-run `dross ship` — PR #N is open but its record has
                 not reached origin". Update shippedBranchFixture (status_test.go:881) to write
                 "status":"shipped" so the fresh-clone fallback test keeps its meaning. Add a
                 watch pin only (drift.go unchanged: classifyPhase's AND at drift.go:110 already
                 keeps a state that never flipped in the verified_unshipped bucket).
       contract: If the "shipped:" line still fires on PR alone, TestStatusPRRecordWithoutShippedStatusIsSilent
                 fails: changes.json {pr:42, status:""} + state status "" → no "shipped:" in output.
                 If the fresh-clone fallback is lost, TestStatusShippedFromPRRecordAlone (updated
                 fixture: {pr:42,status:"shipped"}, state status cleared) fails.
                 If the retry arm is missing, TestSuggestNextRetryShipWhenRecordUnpushed fails:
                 {pr:42,status:""}, PR not merged → next step contains "dross ship"; and if the
                 arm is ordered before the merged consult, TestSuggestNextMergedLegacyRecordStillCompletes
                 fails: {pr:42,status:""} with phase/x pushed onto origin/main → next step names
                 `dross phase complete`, not ship.
                 If watch ever suppresses drift on a record-only PR, TestDriftPROpenButStateNotShipped
                 fails: {pr:42,status:""} + verify pass + state not shipped → verified_unshipped, ok=true.
       depends_on: []

  t-5  Replace prompt's never-re-run guidance with retry
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-3
       description:
                 ship.md:134 → "`git push origin phase/<id>` appends to the open PR; if
                 `dross ship` reported the PR record push failed, re-run `dross ship` — it
                 recognises the open PR and pushes the pending record, never a second PR."
                 Recovery item 3 (ship.md:200) → rename to "Record push failed / phase still
                 reads verified": symptom is status naming `re-run dross ship`; fix is that
                 re-run. Add a §5 note that --force-with-lease is the only path when ship
                 refuses a diverged phase branch.
       contract: If the forbidden sentence returns, TestShipPromptRetryIsReRun fails on
                 strings.Contains(content, "Do NOT re-run") or "would open a second PR".
                 If the retry is not named, the same test fails on !Contains("re-run `dross ship`")
                 in both §5 On-failure and the Recovery section (asserted by section-slicing
                 the content, the way TestShipPromptRecoverySection does).
                 Existing TestShipPromptRecoverySection must keep its required phrases; if the
                 rewrite of item 3 drops "dross phase complete --recover" or "dross ship recover"
                 it fails.
       depends_on: []

Wave 2 (depends t-1)
  t-6  Route both phase-branch pushes through the gate
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-1
       description:
                 New pushPhaseBranch(repoDir, branch string, force bool) (pushed bool, err error)
                 in ship.go, built on compareWithOrigin: originMissing → `push -u`;
                 originAheadOnly → `push -u`; originInSync / originBehindOnly → no push,
                 pushed=false; originDiverged → guided error naming
                 `git pull --rebase origin phase/<id>` and `dross ship --force`; with force,
                 skip the refusal and push --force-with-lease. Replace the initial push
                 (ship.go:336-347) and the record push (ship.go:440-447) with it. The record
                 block's index gate (ship.go:432) stays only around the *commit*; the push
                 decision is the helper's alone.
       contract: If the push is still gated on the index, TestShipPushesLocalOnlyRecordOnRerun
                 fails: after a successful ship, hard-reset the bare remote's phase/x back one
                 commit (drops the record from origin), re-run ship with a clean tree → origin
                 phase/x tip must equal local HEAD again and `git show phase/x:.dross/phases/x/changes.json`
                 on the remote must carry pr 99.
                 If divergence is merged or force-pushed unguided, TestShipRefusesDivergedPhaseBranch
                 fails: push a review commit to origin phase/x from a throwaway branch (the
                 TestPushBaseDivergedNoop recipe) and add a local commit → ship returns an error
                 containing "--force" and "pull", and the remote tip is unchanged.
                 If --force loses --force-with-lease, TestShipForceOverridesDivergence fails: same
                 fixture + `--force` → remote tip == local HEAD.
                 If an in-sync branch triggers a push, TestShipInSyncIssuesNoPush fails: a
                 pre-receive hook on the bare remote that always exits 1 must not fire on a
                 re-run whose branch is already on origin (assert via a marker file the hook writes).
                 Existing TestShipPushesPRRecordToPhaseBranch (localHead == remoteHead) must stay
                 green.
       depends_on: [t-1]

Wave 3 (depends t-2, t-3, t-6)
  t-7  Resolve existing PR: record, then provider, then create
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go, internal/cmd/hostpolicy_test.go
       covers:   c-3, c-5
       description:
                 Before ship.OpenPR (ship.go:371): load changes.json; if ch.PR > 0, synthesise
                 res{Number: ch.PR} and narrate "PR #N already open (recorded) — pushing pending
                 record"; else call ship.FindOpenPRByHeadFunc(opts, phaseBranch): hit → res from
                 it, narrate "found open PR #N for phase/<id>"; ErrHeadPRLookupUnsupported →
                 narrate the skip and create; any other error → return it (never create on a
                 failed lookup); nil → OpenPR as today. Tag telemetry/--json result "existing"
                 for the first two paths (shipResultTag gains a reused flag). Update every mock
                 handler in ship_test.go (shipMockFlow, TestShipFullFlowAgainstMockProvider,
                 TestShipPushesPRRecordToPhaseBranch, TestShipDoesNotPersistPRWhenOpenFails,
                 TestShipIsReShippable) to answer GET .../pulls with `[]` so first ships still
                 reach POST. hostpolicy_test.go: add the FindOpenPRByHeadFunc stub-and-restore
                 alongside the PRStatusFunc one (hostpolicy_test.go:125).
       contract: If the record path is skipped, TestShipRerunReusesRecordedPR fails: two ships
                 against a mock that counts POST /pulls → count == 1, second run's output names
                 "#99", and the mock's GET /pulls counter is 0 on the second run (record wins,
                 provider never asked).
                 If the provider fallback is missing, TestShipRecoversPRFromProviderWhenRecordEmpty
                 fails: changes.json pr=0, mock GET /pulls returns [{number:77, head.ref:"phase/x"}]
                 → POST count 0, pushed changes.json on the bare remote carries pr 77.
                 If a lookup error is treated as "none", TestShipLookupFailureNeverCreates fails:
                 mock GET /pulls → 500 → ship returns error, POST count 0, changes.json pr stays 0.
                 If a closed/other-head PR is matched, TestShipIgnoresOtherHeads fails: GET returns
                 [{number:5, head.ref:"phase/y"}] → POST fires, pr recorded is 99 not 5.
                 If --json drops the reused number, TestShipJSONReportsExistingPR fails: second
                 run under --json → {"number":99,"result":"existing"}.
       depends_on: [t-6, t-2, t-3]

Wave 4 (depends t-7)
  t-8  Flip shipped only after the record push
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-1, c-2, c-4
       description:
                 Delete the early state flip (ship.go:269-287). New order after PR resolution:
                 (1) changes.SetPR + SetBase → commit "chore(dross): record PR #N for <id>" if
                 staged → pushPhaseBranch (failure: return the error; local commit kept per
                 failed_push_residue; nothing else written). (2) On success: s.CurrentPhaseStatus =
                 "shipped" + history-guarded Touch + s.Save; changes.SetStatus(StatusShipped) →
                 commit "chore(dross): mark <id> shipped" if staged → pushPhaseBranch. A failure
                 of push (2) returns a non-zero error whose text says the phase IS shipped and
                 names `dross ship` to push the marker; state stays shipped (the PR record is on
                 origin, which is the locked flip point). The "Marked shipped" narration
                 (ship.go:388) moves after the flip.
       contract: If shipped is written before the record push, TestShipRecordPushFailureIsNotShipped
                 (c-4) fails: a pre-receive hook on the bare remote that exits 1 only when
                 `git log -1 --format=%s $newrev` contains "record PR" → ship returns error;
                 state.json current_phase_status != "shipped"; changes.json status == "" and
                 pr == 99 (residue kept); local HEAD subject is "chore(dross): record PR #99 for x";
                 remote phase/x tip's changes.json has pr 0; phaseDone(root,"x") == false
                 (phase list / status counts, phasedone.go:50); `dross status` output lacks
                 "shipped:" and contains "dross ship". Then remove the hook and re-run:
                 exit nil, POST count still 1, remote tip changes.json pr == 99 and status
                 "shipped", state.json shipped, tree clean.
                 If the flip is rolled back or the record commit reset on failure,
                 the same test's HEAD-subject and pr==99 assertions fail.
                 If the marker push failure is silent, TestShipMarkerPushFailureStaysShippedAndErrors
                 fails: hook rejects only subjects containing "mark x shipped" → ship returns an
                 error mentioning "dross ship", state.json AND changes.json both read shipped,
                 re-run pushes the marker (remote tip subject == "chore(dross): mark x shipped").
                 If the two writes split, TestShipFlipIsAtomic fails: after a clean ship,
                 state.json shipped ⇔ changes.json status shipped (both true); after the c-4
                 failure fixture both false.
                 Existing TestShipIsReShippable (single `shipped x` history row, clean tree),
                 TestShipRecordsShippedNotCompleted and TestShipPushesPRRecordToPhaseBranch
                 must stay green — a duplicated history row or a dirty tree after the marker
                 commit fails them.
       depends_on: [t-7]
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (shared comparison), t-6 (index gate → origin gate, divergence), t-8 (residue re-pushed on next run) |
| c-2 | t-4 (status/phase list/watch consumers), t-8 (single flip after push) |
| c-3 | t-7 (record-first reuse, no second PR), t-5 (prompt retry) |
| c-4 | t-8 (failure-path test with message-selective pre-receive hook) |
| c-5 | t-2 (github+forgejo lookup), t-3 (gitlab+bitbucket lookup), t-7 (ship wires record→provider→create) |

Locked decisions → owner: shipped_timing → t-8; existing_pr_source → t-7; failed_push_residue → t-8; diverged_phase_branch → t-6; shared_origin_gate → t-1.

## Judgment calls

- **Two commits, two gated pushes, not one.** The PR number must be *in* the pushed commit, but the locked flip says changes.json's status flips *after* that push — so status=shipped cannot ride the record commit. Rejected: leaving changes.json dirty (breaks the clean-tree invariant TestShipIsReShippable pins, and `phase complete` refuses dirty trees); leaving the marker commit unpushed (perpetual "ahead by 1", and `phase complete` deletes a branch with unpushed work). Chosen: a second "mark shipped" commit through the same gate; its push failure is an error but does not un-ship, because the record is already on origin.
- **Marker-push failure exits non-zero while state reads shipped.** Rejected warn-and-exit-0: `--auto`/`--json` loops would never learn a commit was left behind. The error text says "shipped" and names the retry so the two signals don't contradict.
- **Consumers key on `changes.Status`, not `ch.PR > 0`.** `TestStatusShippedFromPRRecordAlone`'s fresh-clone rationale survives (status is tracked too), but the PR-alone trigger is exactly the failed-push window c-2 forbids. Fixture updated rather than test deleted.
- **Retry arm ordered after the merged consult.** Legacy records (pr set, status empty, PR long merged, pre-backfill) would otherwise be told to re-ship. Merged wins, then retry, then open.
- **Lookup error aborts ship; it never creates.** A flaky provider that returns "none" is the one path back to a duplicate PR. Only the explicit ErrHeadPRLookupUnsupported sentinel is an announced skip, mirroring basepr.go — and t-3 wires Bitbucket so no shipped provider hits it.
- **Initial push goes through the same gate as the record push.** c-1 only names the record push, but a diverged branch fails the initial push first with a raw non-ff error; routing both through pushPhaseBranch gives the guided error at whichever push meets divergence, and makes "re-run pushes the pending record" true at the first push rather than a second one.
- **Provider lookup split across two tasks by transport, not one per provider.** Four one-file tasks would each be under the size floor; one task would span 6+ files. GitHub+Forgejo share the "list and filter client-side" shape; GitLab+Bitbucket both filter server-side by query.
- **Failure injection via a message-selective pre-receive hook.** An always-reject hook (TestPushBaseRejectedPushSurfacesGitOutput's recipe) would fail the initial push and never reach the PR. Matching on the commit subject makes each push individually rejectable, which is what "fails after the PR opens" requires.
- **Watch unchanged, test only.** `classifyPhase` ANDs state-shipped with the record, and state never flips in the failure window, so it is already correct; a pin test guards it from a future OR.
