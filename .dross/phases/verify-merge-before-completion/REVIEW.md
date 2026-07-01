# Plan Review — verify-merge-before-completion
Reviewed: 2026-07-01
Plan: 3 tasks across 2 waves

## BLOCKING
- [feasibility / missing-task] t-3 as specified regresses all three "stay green"
  fixtures — TestPhaseCompleteHappyPath, TestShipToCompleteLeavesZeroManualGit,
  TestConsecutivePhasesNoDivergence — and the plan has no task/step to migrate
  them. Verified against the actual fixtures:
  - `completeFixture` (phase_test.go:310) never runs ship, so there is no
    recorded PR number, and it never pushes `origin/phase/auth`. Under the new
    gate: no PR → fallback → `merge-base --is-ancestor origin/phase/auth
    origin/main` errors (ref absent) → inconclusive → refuse. HappyPath now fails.
  - The two ship-driven tests use the **forgejo** mock provider, which t-2 maps
    to `ErrMergeStatusUnsupported` → fallback. Their squash-sim (checkout of
    `src/`+`state.json` onto a fresh commit on origin/main, phase_test.go:604-608
    / 683-690) is **not** an ancestor of origin/main, and `origin/phase/x` is
    either non-ancestor or deleted (providerDeleted case). So ancestry is
    false/errors → refuse. Both fail.
  Per c-5 these fixtures now *correctly must be refused* unless migrated — the
  plan owes that migration. t-3's test_contract just asserts they "stay green"
  with no mechanism.
    Suggestion: add explicit fixture-migration work (name it in t-3 or split a
    t-4). Options: (a) switch the ship-driven fixtures to the github provider
    with an injectable merged-status stub returning MERGED for the recorded PR;
    and give completeFixture a recorded PR + stubbed merged-status; or (b)
    rebuild each squash-sim as a *real* ancestor merge so the ancestry fallback
    passes with no provider. Pick one and write it as a task deliverable.

- [feasibility / dirty-tree + read-timing] t-1 "persist res.Number into the
  phase's changes.json (post-push)" writes a tracked file (changes.json IS
  git-tracked — confirmed via `git ls-files`) but the plan never commits it.
  complete's clean-tree guard (phase.go:290-296, `git status --porcelain` →
  `dirtyTreeError`) will see the modified/added changes.json and refuse *before*
  reaching the new gate — breaking the feature and every complete test.
  Compounding: because the write is post-push it never reaches origin/main, and
  complete does `git checkout <reconcile>` at phase.go:304, after which the
  working-tree changes.json reverts to the committed (PR-less) version.
    Suggestion: t-1 must COMMIT the changes.json write to phase/<id> (a local
    post-push commit is safe — the branch is deleted at complete, so it never
    touches main and won't re-seed divergence). t-3 must read the recorded PR
    number from the phase branch BEFORE the checkout at phase.go:304. State both
    explicitly in the task descriptions.

## FLAG
- [feasibility] t-3's ancestry fallback must treat a **missing** origin/phase/<id>
  ref (deleted by `gh pr merge --squash --delete-branch`) as inconclusive→refuse,
  not as a propagated error/crash — c-5 requires "no crash offline".
  `merge-base --is-ancestor origin/phase/<id> origin/<base>` errors on the absent
  ref, and squash rewrites SHAs so it's false even when the ref survives. The
  fallback is therefore only ever meaningful for merge-commit/standalone repos;
  on this squash repo it can never PASS, so a github repo with an unreachable
  provider will always refuse (acceptable per design, but say so).
    Suggestion: map both the git-error and the false result onto the same guided
    refusal path; add a test with the ref deleted proving no crash.

- [feasibility] t-1 must guard the persist against `res == nil` (OpenPR failed,
  or `--no-push` returned earlier at ship.go:169) and `res.Number <= 0`
  (`parsePRNumber` returns 0 on an unparseable gh URL, open.go:312). Writing
  PR:0 would be read by t-3 as "no PR" and silently route to the fallback.
    Suggestion: only persist when `res != nil && res.Number > 0`.

- [testability] t-2 exposes only the package-`ship` unexported `ghCommand` seam.
  phase_test.go is package `cmd` and cannot set it, so a cmd-level test can't
  drive `PRMerged == true` for a github fixture — which is what proves the c-3
  *primary* (provider) gate end-to-end (the existing ship tests use forgejo+
  httptest precisely because they can't override gh). Without a reachable seam,
  the provider-merged happy path is only covered at the unit level in t-2.
    Suggestion: expose an exported, cmd-overridable seam (e.g. `ship.PRMergedFunc`
    var or an injectable interface) so phase_test can stub merged=true without a
    real gh binary or network.

- [granularity] t-3 bundles the gate logic (c-1/c-2/c-3), the offline fallback
  (c-5), AND the fixture-migration/seam work into one task spanning production
  logic + a test-suite rewrite. Consider splitting the fixture-compat work into
  its own task so the gate change and the test-migration are reviewable and
  committable independently.

## NOTE
- [feasibility check A] `res.Number` IS in scope where t-1 needs it: ship.go
  calls `res, err := ship.OpenPR(opts)` (ship.go:264) and already reads
  `res.Number` (ship.go:269). OpenPR is a library call from the CLI and the CLI
  holds the result, so t-1's persistence assumption holds — no reachability gap.
- [pr_persistence] changes.json is git-tracked but phase-scoped (each phase has
  its own `.dross/phases/<id>/` dir), so it is genuinely drag-proof for the
  cumulative-History problem the decision targets. Adding a `PR int` field is
  additive and back-compatible with existing changes.json readers.
- complete's Long help text (phase.go:231-232) still describes the breadcrumb
  ("Refuses ... when origin/<branch> carries no `completed <id>` record") as THE
  guard; after t-3 demotes it to a hint, update the help so the docs don't
  misdescribe the gate.

## Summary
Coverage/decisions/waves are clean, but t-3 will regress three green tests
because their fixtures can't satisfy the new gate, and t-1's uncommitted
post-push write trips complete's clean-tree guard — both must be resolved before
execution.
