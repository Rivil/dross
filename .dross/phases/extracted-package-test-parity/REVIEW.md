# Plan Review — extracted-package-test-parity

Reviewed: 2026-09-26
Plan: 12 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-6 contract 4 says the titled path "calls EnsureMilestone(title, "title\n\nSuccess criteria:\nc1\nc2")". The code does not do that. milestone.go:66 calls `ctx.Client.EnsureMilestone(version, MilestoneBody(title, desc))`, so the first argument is the version. The title only reaches the body. If the test asserts that the recorded first argument equals the title, it fails on correct code. The only ways out are an edit to production code, which t-12 forbids, or weakening the test on the fly.
  Suggestion: reword the contract to EnsureMilestone(version, "<title>\n\nSuccess criteria:\nc1\nc2"). The kill for line 50 comes from the body prefix: untitled gives a body starting with the version, titled gives one starting with the title.

- [test-contract] t-8's TestApplyThenUndoRoundTrip claims that every card's label set ends up reflect.DeepEqual to the snapshot taken before Apply. That does not hold for a card that had no labels before Apply. relabelReapedCard (reap_apply.go:~198) adds `dross/status:<T>` to such a card, and Undo skips the label restore when `len(card.PriorLabels) == 0` (reap_undo.go:61), so the label stays. The sibling fixtures use exactly this shape: t-7 contract 3 has "no prior labels", and t-8 contract 3 has "closed B (no labels, …)". Reusing either fixture for the round trip makes the test fail on current code.
  Suggestion: restrict the round-trip fixture to cards that have labels before Apply. Separately, decide now whether the asymmetry is a defect to file with `dross deferred add`, rather than finding out mid-execute.

- [test-contract] Two evidence commands leave out the argument that `-coverprofile` requires. t-2 contract 5 is "`go test -coverprofile ./internal/secretscan/`". t-12 contract 5 is "`go test -count=1 -coverprofile ./internal/boardsync/ ./internal/secretscan/`". In both, `-coverprofile` takes the first package path as its output file, so the c-2 measurement fails as written. In t-12 only secretscan would be tested, and the write would target a directory.
  Suggestion: `-coverprofile=$T/c2.out ./internal/boardsync/ ./internal/secretscan/`, then `go tool cover -func` or a per-file statement sum over that file.

- [granularity] t-3 touches 5 files in two unrelated packages. It combines the new coverfloor checker (main.go, floor.go, floor_test.go, fixture_test.go) with a helper-process test for verify.RunDetachArgv in internal/verify. The detach test shares no code or fixture with the checker. It is linked to t-3 only through the calibration contract.
  Suggestion: split internal/verify/detach_test.go into its own wave-1 task. t-10 already waits on wave 1, so no ordering changes.

- [wave-order] t-3's calibration contract says the checker "lists exactly backlog.go … writers.go and verify/detach.go before detach_test.go lands". t-2 is in the same wave and adds writers_test.go, which takes writers.go to about 100%. The contract is therefore true only if t-3 runs before t-2, and nothing enforces that order. "Base tree" does not settle it either: the second half of the contract ("the same six … after") measures a tree that has detach_test.go, which is not the base tree.
  Suggestion: run the calibration in a worktree at 018d325, adding only detach_test.go for the second half. Alternatively, drop writers.go from the "after" list when t-2 has already landed.

- [antipattern: files] t-11 lists 8 files, all conditional fallbacks. It leaves out the files its own description names as the first choice ("add the killing test in the owning test file (the wave-2 file, else a listed fallback)"), meaning backlog_push_test.go, backlog_reconcile_test.go, inbound_test.go, milestone_test.go, reap_apply_test.go, reap_undo_test.go and sync_paths_test.go. It also omits internal/diag/redproof_test.go. That is the file that owns tests for the only diag survivors (redproof.go 91, 94), and the listed diag fallbacks (trust_test.go, diag_test.go) do not own them.
  Suggestion: make `files` match the described routing. Add the wave-2 test files and redproof_test.go, and drop fallbacks that no known survivor maps to.

- [locked-decision adjacent] t-10 contract 3 treats a file that fails only under the runner-like PATH as "STOP-and-ask (defer or add a test)". Under the locked guard_shape (flat floor, no allowlist), deferring leaves that file below the floor, so the PR's CI job goes red. It cannot ship, and main auto-releases on push. Deferring is not an actual option, and presenting it as one invites someone to add an allowlist later under pressure.
  Suggestion: make the options "add a test in this phase" or "stop and re-open c-4/guard_shape with the user". Drop "defer".

## NOTE
- [strengths] The survivor-to-test mapping is exhaustive. I checked every "kills N" annotation against the routed list in cmd-package-decomposition/verify.toml. Every routed survivor in the six c-2 files maps to a named test: backlog.go 51/51 across t-4 and t-5, reap_apply.go 30/30, reap_undo.go 11/11, inbound.go 4/4, milestone.go 4/4. t-9 covers the 16 routed survivors outside those files exactly, and t-11 carries the 2 diag/redproof.go survivors as predicted ceilings. The writers.go split (9 killable in t-2, 9 predicted ceilings in t-11) also matches the 18 cross-package-only-coverage entries line for line.

- [strengths] Behaviour preservation is proven structurally, not by assertion. t-12 checks a git diff over internal/cmd and over non-test .go files, and a remote-only full suite runs after the drain to avoid host-lock contention. All 52 new Test names in the plan are absent from tests_before.txt and from the repo, so TestNoTestLost cannot trip on a homonym.

- [strengths] The test infrastructure guards against its own vacuity. The strict tracker fails on unused routes. TestDoubleCapabilitiesAreExact prevents t-8's StateWriter refusal test from passing vacuously. The t-3 fixture runs the real toolchain and includes the internal/lib case, which is covered only by internal/user's tests, proving that the floor measures own-package coverage. t-10 also opens with the CI supply-chain audit that the global CLAUDE.md requires.

- [test-contract] t-11 maps four of survivor.Derive's evidence outcomes. "attribution ceiling" and "no coverage block" lead to accept; "killable" and "line never executes" lead to STOP. Derive (internal/survivor/evidence.go:317-333) has three more: Inapplicable ("does not apply to this line (string operands)"), "no coverage profile — unknown", and "undecidable". Contract 3 forbids accepting these, but the description gives them no route. The practical risk is low: the only package-level concatenation sites in the three packages (secretscan/rules.go 101/108/109) are already accepted. Still, "any other evidence line → STOP and ask" would close the gap.

- [spec-drift] The "why" of the locked c1_evidence decision says "This phase's diff is test files". The plan adds production files cmd/coverfloor/main.go and floor.go, so verify's diff-scoped mutation will generate mutants there. t-3's boundary contracts cover floor.go. Keep main.go free of mutable operators (a bare `os.Exit(run(...))`), or any branch in main() will show up at verify as a NOT COVERED survivor.

- [fragility] t-12 contracts 1 and 2 hardcode 018d325 as the merge-base. This branch has already been rebased once (2cd629e). If milestone/v1.7 moves and the branch is rebased again, the `018d325...HEAD -- internal/cmd` diff would pull in other phases' changes and go red. Resolve the base from changes.json `base_commit` or `git merge-base HEAD milestone/v1.7` at run time.

- [environment] t-10 contract 2 runs a targeted `go test -run '…' ./internal/cmd/` on the laptop. This builds the full internal/cmd test binary locally, which the project notes flag as unreliable under memory pressure. The selected tests only read static files, so it will probably be fine. If it stalls, run it through remote `dross test` instead of retrying locally.

- [evidence-timing] t-10 contract 4 can only be observed on the PR's CI run: the floor step green, and no new >300s internal/cmd warning. That run is the only place the cost of coverage instrumentation under `-race` (atomic mode) on internal/cmd is measured. t-10 cannot be fully closed at execute time, so expect to confirm it during /dross-ship.

- [test-contract] t-4 contract 7 ("if DeferredBacklogItem shape drifts, its test fails") names no test, unlike every other contract in the plan. The behaviour it pins is specific enough, but the test should be named for traceability in verify's criterion mapping.

## Summary
The plan is strong and ready to proceed. Coverage, locked decisions and rules are clean, and its per-mutant contracts are unusually precise. Fix the t-6 EnsureMilestone argument, the t-8 round-trip fixture and the two malformed `-coverprofile` commands before execute, because each would produce a false red on correct code.
