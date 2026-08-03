# Panel draft — MVP lens

Phase provider-merge-parity — 4 tasks across 2 waves

```
Wave 1
  t-1  Replace PRMerged with base-aware PRStatus
       files:    internal/ship/merged.go, internal/ship/merged_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-5 (github slice)
       contract: - TestPRStatusGitHub fails with an empty BaseRef if `baseRefName` is
                   dropped from the `gh pr view --json` field list
                 - TestPRStatusGitHub fails if a canned `mergedAt`-populated payload
                   stops mapping to Merged==true
                 - internal/cmd compiles only once mergeGate + stubPRMerged are moved
                   onto the PRStatusFunc seam; TestPhaseCompleteMergeGateRefusalLeavesHEADOnPhase
                   fails if the refusal path is lost in the swap
       depends:  —
       status:   pending

Wave 2 (all three depend on t-1's PRState/PRStatusFunc seam only — no cross-dependency)
  t-2  GitLab merge-request status + open-by-base
       files:    internal/ship/merged.go, internal/ship/basepr.go,
                 internal/ship/merged_test.go, internal/ship/basepr_test.go
       covers:   c-1, c-4 (gitlab), c-5 (gitlab)
       contract: - TestPRStatusGitLab drives an httptest server that 404s any path other
                   than /projects/{ref}/merge_requests/{iid}; a wrong endpoint fails it
                 - a canned MR with `state: "opened"` asserts Merged==false, `"merged"`
                   asserts true — inverting the comparison fails the table
                 - TestPRStatusGitLab asserts BaseRef comes from `target_branch`; dropping
                   the field yields "" and fails
                 - TestOpenPRsTargetingGitLab's handler asserts the query string carries
                   state=opened and target_branch=<base>; omitting either fails
                 - the handler asserts the PRIVATE-TOKEN header (Bearer when AuthScheme
                   is "bearer"); auth regressions fail before any parsing
       depends:  t-1
       status:   pending

  t-3  Forgejo/Gitea PR status + open-by-base
       files:    internal/ship/open.go, internal/ship/merged.go, internal/ship/basepr.go,
                 internal/ship/merged_test.go, internal/ship/basepr_test.go
       covers:   c-2, c-4 (forgejo/gitea), c-5 (forgejo/gitea)
       contract: - TestJSONGet asserts the new GET helper sends `Authorization: token <tok>`
                   and returns a non-nil error on a 404 body
                 - TestPRStatusForgejo: canned `{"merged": true, "base": {"ref": "main"}}`
                   asserts Merged==true and BaseRef=="main"; a `"merged": false` payload
                   asserts false — both provider spellings ("forgejo", "gitea") run the
                   same table, so wiring only one fails the other
                 - TestOpenPRsTargetingForgejo feeds a list containing one PR based on
                   the requested branch and one based on another; if the base.ref filter
                   is dropped the test fails with 2 results instead of 1
       depends:  t-1
       status:   pending

  t-4  Announce ancestry fallback; refuse retargeted base
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-3, c-5 (gate behaviour)
       contract: - TestMergeGateAnnouncesAncestryFallback stubs PRStatusFunc to return
                   ErrMergeStatusUnsupported and asserts captureStdout contains the
                   fallback notice; deleting the Printf fails it
                 - a second case stubs a generic network error and asserts the same
                   notice with the error text — the silent generic fall-through fails it
                 - TestPhaseCompleteRefusesRetargetedBase stubs PRStatusFunc to return
                   Merged==true with BaseRef=="some-other-branch" and asserts complete
                   refuses with a retarget message and leaves HEAD on the phase branch;
                   removing the comparison lets completion proceed and fails it
                 - TestPhaseCompleteProceedsWhenBaseMatches asserts BaseRef equal to the
                   reconcile branch does NOT refuse, and BaseRef=="" (provider could not
                   answer) also does not refuse — a stricter check fails here
       depends:  t-1
       status:   pending
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 GitLab authoritative PRMerged | t-2 |
| c-2 Forgejo/Gitea authoritative PRMerged | t-3 |
| c-3 mergeGate announces ancestry fallback | t-4 |
| c-4 GitLab + Forgejo/Gitea OpenPRsTargeting | t-2, t-3 |
| c-5 base-retarget detected across all 4 providers | t-1 (github + seam), t-2 (gitlab), t-3 (forgejo/gitea), t-4 (gate refusal) |

5/5 criteria covered. Every task traces to at least one criterion.

## Judgment calls

- **One provider call, not two.** c-1/c-2 (merged?) and c-5 (current base ref) are answerable from the same single-PR GET on every provider. Chose to widen the existing seam into `PRStatus(opts) (PRState{Merged bool, BaseRef string}, error)` rather than add a parallel `PRBaseRef` function. Rejected: separate per-criterion functions — that doubles the API surface and violates the locked `retarget_timing` decision ("piggybacked on mergeGate's existing provider query").
- **Replace `PRMerged`, don't wrap it.** Chose a hard swap of `PRMerged`/`PRMergedFunc` → `PRStatus`/`PRStatusFunc`, updating `mergeGate` and `stubPRMerged` in the same task. Rejected: keeping `PRMerged` as a thin bool wrapper for compatibility — this is a single-consumer internal package, and two seams for one call is exactly the compat shim the repo's rules say not to add.
- **t-1 is not scaffolding.** It carries GitHub's slice of c-5 (adding `baseRefName` to the `gh --json` list and returning it), so the wave-1 task ships criterion value rather than existing only to unblock others.
- **t-4 sits in wave 2, not wave 3.** It needs only `PRStatus`'s BaseRef field from t-1; its tests stub `PRStatusFunc` directly and never touch a real provider, so waiting on t-2/t-3 would be false serialization.
- **Split GitLab from Forgejo/Gitea; merge Forgejo with Gitea.** GitLab reuses the existing `gitlabReq(method, ...)` helper, while Forgejo/Gitea needs a brand-new `jsonGet` (only `jsonPost` exists) — different work, and together they would exceed the 5-file ceiling. Forgejo and Gitea are one API, so they are one task.
- **Base-retarget refuses only on a positive mismatch.** An empty `BaseRef` (provider could not answer, or bitbucket) is treated as "no evidence" and does not block completion; only a non-empty ref differing from the reconcile branch refuses. Rejected: refusing on any inconclusive lookup — that would turn every transient API failure into a blocked completion, which the locked `retarget_response` decision does not ask for.
- **Bitbucket carried, not extended.** The locked `retarget_scope` decision names four providers; bitbucket's existing `bitbucketPRMerged` is migrated to the new struct return in t-1 to keep the package compiling, with no base-ref work and no new tests.
- **No new provider files.** New code extends `merged.go` / `basepr.go` / `open.go` alongside the existing per-provider functions. Rejected: creating `gitlab.go` / `forgejo.go` — a file split is a refactor no criterion asks for, and it would churn every test file in the package.
