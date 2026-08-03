# Plan Review — provider-merge-parity

Reviewed: 2026-08-03
Plan: 8 tasks across 3 waves

## BLOCKING

- [wave order] **t-6b sits in wave 2 while depending on t-6a, which is also wave 2.** This
  violates the plan schema's own rule (`assets/prompts/plan.md:37` — "`depends_on` | Task ids
  in *lower* waves") and the invariant the code enforces: `internal/phase/plan_edit.go:294`
  ("every dependent's wave must stay strictly greater than its dependency's") and
  `plan_edit.go:284`, which errors with "adopted wave %d is not after its dependency %s
  (wave %d)" on exactly this shape. `dross task move`/`add` would refuse to produce this
  plan. It is a defect *introduced by the t-6 split* — the old single t-6 had no intra-wave
  dep. `phase.NextTask` (`internal/phase/phase.go:315-330`) gates on dependency *status*, so
  execution order still comes out right, but the wave labels now lie and the wave-boundary
  checkpoint logic (`execute.md:218`) will treat t-6a→t-6b as mid-wave when it is a hard
  serialization point.
  Suggestion: t-6b → wave 3, t-7 → wave 4. (Or, if the intent was that t-6b is genuinely
  independent of t-6a, drop the `depends_on` — but it is not: t-6b's contract line
  `TestMergeGateRetargetSkipsOnProviderError` asserts against "t-6a's announced ancestry
  fallback", so the dependency is real.)

- [antipattern: squashed / test contract] **t-1's stubPRMerged requirement is not achievable
  as written.** The task freezes the signature (`stubPRMerged(t *testing.T, merged bool)` —
  "keeps its exact signature") *and* requires "its new default BaseRef equals the calling
  test's recorded base". There is no channel between the two. The stub body is a
  `func(ship.OpenOpts) (PRStatus, error)` closure, and the recorded base never reaches
  `OpenOpts`: `buildOpenOpts` (`internal/cmd/ship.go:27-38`) never sets `BaseBranch`, and
  `mergeGate` sets only `opts.PRNumber` before the call (`internal/cmd/phase.go:732`). The
  base is passed to `mergeGate` as the separate `reconcileBranch` parameter and stops there.
  Hardcoding `"main"` does not rescue it: `phase_test.go:2223` records
  `"base":"milestone/<version>"` and stubs merged at `:2246` and `:2266`, so a hardcoded
  `main` would make those fixtures trip t-6b's retarget refusal. The implementer hits this
  at t-6b, with ~46 fixtures failing and no stated resolution.
  Suggestion: name the mechanism in t-1. The natural one is `mergeGate` setting
  `opts.BaseBranch = reconcileBranch` before calling `PRStatusFunc` (a one-line change that
  also makes the seam self-describing), so the stub can echo it back; t-6b's own tests then
  override with a deliberately mismatched BaseRef. Alternatively relax the frozen signature
  to a variadic base override and accept touching the 46 call sites. Either way the plan has
  to say which.

## FLAG

- [antipattern: missing file] **t-2 and t-3 both edit `internal/ship/open.go`, which neither
  lists.** t-2 extracts "the APIBase/AuthEnv/token/splitOwnerRepo/gitlabProjectRef preamble
  currently inlined in openGitLabPR" — `openGitLabPR` is `open.go:155`, `gitlabReq` is
  `open.go:243`, `splitOwnerRepo` is `open.go:302`, `gitlabProjectRef` is `open.go:220`.
  t-3 adds `jsonGet` "next to jsonPost" (`open.go:336`) and `forgejoTarget` from
  `openForgejoPR` (`open.go:99`). An extraction that leaves the original call sites intact
  means editing open.go; `open_test.go` may follow.
  Suggestion: add `internal/ship/open.go` to both `files` lists. This also makes the
  t-2/t-3 collision surface explicit rather than only basepr.go.

- [test contract] **t-5's stated end-state for the unsupported-provider table is
  unreachable.** t-5 says the table drops "forgejo/gitea/Forgejo, leaving it asserting only
  genuinely unknown providers". But the current test (`internal/ship/merged_test.go:56-66`)
  asserts `errors.Is(err, ErrMergeStatusUnsupported)`, and an unknown provider does *not*
  satisfy that — `PRMerged`'s default arm returns a plain `fmt.Errorf`, a distinction an
  existing sibling test (`TestPRMergedUnknownProvider`, `merged_test.go:70-78`) exists
  specifically to pin. After t-4 drops gitlab and t-5 drops forgejo/gitea/Forgejo, the table
  is empty and the test asserts nothing.
  Suggestion: state that the ship-layer unsupported test is either deleted (its coverage
  moving to t-6a's stub-driven forward-seam test) or converted to drive the sentinel through
  an injected fake dispatch. Don't leave "assert only unknown providers" — that is already a
  different test with a different sentinel.

- [antipattern: stale prose] **t-6a's ARCHITECTURE.md scope is too narrow, and no task owns
  the Go doc comments the rename invalidates.** t-6a corrects lines 306 and 361; but line 508
  (`bitbucketPRMerged (authoritative state == MERGED) — internal/ship/merged.go:71`) is
  invalidated by t-1's rename, and the ship-section prose at line 502 ("`bitbucketPRMerged`
  reports the authoritative merged status") goes stale for the same reason and additionally
  no longer describes GitLab/Forgejo, which t-4/t-5 make authoritative. Separately, no task's
  contract covers the doc comments that will read false after this phase:
  `internal/ship/merged.go:13-18` ("for providers whose ... lookup isn't wired yet
  (forgejo/gitea/gitlab)", "GitHub is the only authoritative provider today"),
  `merged.go:20-24`, and `internal/ship/basepr.go:11-16` / `:27-31` ("the unwired providers
  return ErrBasePRLookupUnsupported").
  Suggestion: extend t-6a's ARCHITECTURE.md line list to 502/508, and add a contract line to
  t-1 (merged.go) and t-2/t-3 (basepr.go) requiring the sentinel doc comments be rewritten to
  describe the forward-seam role rather than the now-false provider list.

- [antipattern: factual] **t-1's description mis-states the stubPRMerged blast radius.** It
  names seven other test files, two of which (`ship_test.go`, `enum_divergence_test.go`)
  contain zero `stubPRMerged` calls — the real set is five: `completion_state_truth_test.go`,
  `state_history_test.go`, `switchbranch_test.go`, `phase_base_truth_test.go`,
  `state_clobber_regression_test.go`. And "those eight existing fixtures" understates by ~6x:
  there are 46 call sites (38 in `phase_test.go`, 8 across the other five).
  Suggestion: correct the counts. The 46-vs-8 gap matters because it is the true cost of the
  BLOCKING signature question above — an implementer reading "eight" will underweight it.

- [test contract] **t-1 contradicts itself on whether the stub is renamed.** The description
  says stubPRMerged "keeps its exact signature and all seven other cmd-test call sites ...
  unchanged; only its body moves onto PRStatusFunc", but contract line 6 says the two phase
  tests "keep passing through the **renamed** stub".
  Suggestion: pick one and say it. If the name stays `stubPRMerged` while the seam becomes
  `PRStatusFunc`, that mismatch is worth one sentence of justification (46 call sites) rather
  than leaving it ambiguous.

- [test contract] **t-3's basepr table line omits the capitalized alias.** The actual table
  (`internal/ship/basepr_test.go:120`) is `{"bitbucket","forgejo","gitea","gitlab","Forgejo"}`.
  t-3 says it "no longer lists forgejo/gitea" — "Forgejo" must go too, and t-5's parallel
  contract line does name it.
  Suggestion: mirror t-5's wording: "drops forgejo/gitea/Forgejo, leaving bitbucket".

## NOTE

- [coverage] Every criterion is covered: c-1→t-4, c-2→t-5, c-3→t-6a, c-4→t-2/t-3,
  c-5→t-1/t-4/t-5/t-6b/t-7. c-5's five-task spread is broad but each is a genuine slice
  (seam, two provider readers, the compare, the cross-provider matrix), not padding.

- [locked decisions] No conflicts. `retarget_scope`'s widening to all five providers is
  honoured concretely — t-1 populates Bitbucket's BaseRef from `destination.branch.name`
  rather than carrying an empty ref, and t-7's matrix asserts all five refuse.
  `retarget_response` (warn-and-refuse) is t-6b's contract. `retarget_timing` (completion-time,
  piggybacked on mergeGate's existing query) is satisfied — t-6b adds no second API call.

- [forbidden actions] No rule violations. `runtime.mode = "native"`, `go test -count=1 ./...`;
  nothing in the plan invokes a forbidden toolchain. r-01 (`make install` before relying on a
  prompt/binary change) is not implicated — no `assets/` edits. r-02 is not implicated at plan
  time.

- [granularity] t-1's six files across two packages trips the 5+-file split heuristic, but a
  split is the wrong call: renaming a seam and moving its callers is atomic or
  `go build ./internal/cmd` breaks. Recorded, no action.

- [wave order] The same-wave file collisions the plan warns about in prose (t-2/t-3 on
  basepr.go, t-4/t-5 on merged.go) are moot under `dross execute`: `phase.NextTask`
  (`internal/phase/phase.go:315-330`) returns a single task, so tasks in a wave run
  sequentially, never concurrently. The prose is harmless but describes a race that the
  execution model cannot produce.

- [strength] **The prior pass's BLOCKING issue is genuinely fixed, not relabelled.** t-6b now
  compares against `reconcileBranch` — mergeGate's own parameter, which `phase.go:426` passes
  straight from `resolveCompleteBase` (`phase.go:645-666`) — and the description explicitly
  forbids the re-read of `changes.Base`, naming both consequences (bypassing `--base`,
  dropping the `phaseRefRecordedBase` tier). Verified against source; the fix is correct and
  complete.

- [strength] **The contracts are unusually falsifiable.** Nearly every line names the wrong
  implementation that would pass today's tests and fail the new one: `state != "OPEN"`,
  `state != "opened"`, Gitea reporting a merged PR as `closed`, `id` vs `iid`, first-page-only
  pagination, `head.label` vs `head.ref`, and — best of the set — "an impl that puts the
  comparison after the `merged → return nil` short-circuit passes the previous test and fails
  this one". This is what a test contract is for.

- [strength] **The remedies asserted in t-6b's error message were checked against the code,
  not assumed.** `--recover` really is applied at `phase.go:466`, after the mergeGate call at
  `:426`, so "`--recover` does not bypass mergeGate today" is accurate; and `dross ship`
  really does rewrite the base record (`internal/cmd/ship.go:374`, `changes.SetBase`), so the
  suggested "re-run `dross ship`" remedy actually works. Both were verifiable and both held.

## Summary

The prior pass's BLOCKING fix landed correctly and the FLAG fixes mostly landed, but the
rewrite introduced one new invariant violation (t-6b's intra-wave dependency) and left one
requirement that cannot be satisfied as stated (stubPRMerged's frozen signature vs. its
per-fixture BaseRef default) — both cheap to fix, neither safe to execute around.
