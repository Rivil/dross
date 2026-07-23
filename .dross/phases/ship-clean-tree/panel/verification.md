# Draft: ship-clean-tree (lens — design backward from test contracts)

Method: for each criterion I first wrote the ideal failing test, then derived the
smallest code change that makes that test pass. Waves reflect *strict* output
dependencies only.

```
Phase ship-clean-tree — 4 tasks across 2 waves

Wave 1
  t-1  Auto-commit .dross-only dirty gates
       files:    internal/cmd/cleantree.go (new), internal/cmd/ship.go,
                 internal/cmd/phase.go
       covers:   c-1
       contract: dirty tree with ONLY .dross/handoff.md → shared helper stages
                 .dross/ and makes one `chore(dross):` commit, command proceeds
                 (assert exactly one new commit, all paths under .dross/);
                 a modified internal/foo.go (non-.dross dirt) → helper returns
                 dirtyTreeError naming internal/foo.go and command aborts with
                 zero new commits; mixed .dross + non-.dross dirt also refuses
                 (no partial auto-commit of the .dross half).

  t-2  Push base branch when ahead by .dross chores
       files:    internal/cmd/basebranch.go, internal/cmd/ship.go,
                 internal/cmd/phase.go, internal/cmd/ship_recover.go
       covers:   c-2
       contract: local main 1 commit ahead of origin/main touching only .dross/
                 (reuse doctor.go's phaseCommitsOnMain / rev-list origin..HEAD to
                 detect) → ship/complete/recover pushes main, so
                 `git rev-list --count origin/main..main` == 0 afterward;
                 when the safety-net push fails (injected push error) the command
                 returns a non-nil hard error, not warn-and-continue;
                 a `dross pause` write commits locally but does NOT push
                 (origin/main SHA unchanged after pause).

  t-3  Resolve recorded PR from origin tree in mergeGate
       files:    internal/cmd/phase.go
       covers:   c-3
       contract: working-tree changes.json missing/PR=0 but
                 origin/<base>:.dross/phases/<id>/changes.json records PR #7 →
                 complete resolves recordedPR=7 and mergeGate calls
                 ship.PRMergedFunc with 7 (provider path), NOT the
                 merge-base ancestry fallback; when neither working tree nor
                 origin carries a PR record, recordedPR stays 0 and mergeGate
                 still takes the ancestry fallback (unchanged).

Wave 2 (depends t-3)
  t-4  Milestone-aware recover after merge verification
       files:    internal/cmd/phase.go, internal/cmd/ship_recover.go
       covers:   c-4, c-5
       depends:  t-3
       contract: diverged/stale main + PR #N merged upstream →
                 `phase complete --recover` confirms the merge via mergeGate
                 (origin PR, from t-3) then resets main to origin/main and
                 restores .dross/, and does NOT refuse (assert HEAD==origin/main,
                 .dross/ present);
                 PR #N NOT merged → `--recover` refuses BEFORE any reset (assert
                 local main HEAD unchanged — proves merge verification precedes
                 the destructive reset);
                 active milestone with base milestone/v0.10 and a diverged local
                 milestone/v0.10 → `--recover` resets to origin/milestone/v0.10
                 and restores .dross/, and the "does not yet support milestone
                 branches" error is NOT returned (assert HEAD==origin/milestone/v0.10).
                 Existing tests to flip: TestPhaseCompleteMilestoneDivergedAborts
                 (phase_test.go:1284, currently asserts the abort) and extend
                 TestPhaseCompleteRecoverHeals (:974) into the stale/PR-merged case.
```

## Coverage

| Criterion | Task(s) |
|-----------|---------|
| c-1 (auto-commit .dross-only dirt in ship/complete/create; non-.dross still refuses) | t-1 |
| c-2 (base chores no longer unpushed; local base == origin/base after ship/complete/recover) | t-2 |
| c-3 (mergeGate resolves recorded PR from origin when working tree stale) | t-3 |
| c-4 (--recover heals in the stale state; merge verification precedes destructive reset) | t-4 |
| c-5 (--recover supports milestone reconcile branch) | t-4 |

Every criterion accounted for (5/5).

## Judgment calls

- **Merged c-4 + c-5 into one task (t-4), not two.** Both ideal test contracts
  drive the *same* code region — the `--recover` path in `phaseComplete` plus
  `runDrossRecovery` in ship_recover.go (~40 lines). The smallest change
  satisfying c-5 (parameterize `runDrossRecovery`'s target branch, drop the
  hardcoded `milestoneActive` abort at phase.go:352-361) overlaps the smallest
  change for c-4 (let recovery run once mergeGate confirms, without depending on
  the ff-only attempt). Splitting would force two tasks to rewrite the same
  block sequentially and invite a merge conflict; two distinct contracts on one
  task is the honest decomposition. Rejected: separate t-4/t-5 tasks.

- **t-4 depends on t-3; the rest are wave 1.** c-4's contract ("recover works in
  the diverged/stale state its own error recommends") is only satisfiable once
  mergeGate can confirm the merge from origin (c-3) — otherwise mergeGate refuses
  before recovery ever runs, which is the exact bug. That is a strict output
  dependency, so t-4 is wave 2. t-1/t-2/t-3 touch independent surfaces
  (dirty-autocommit at gate entry, base-push at gate exit, PR resolution inside
  mergeGate) with no output dependency between them, so all three are wave 1 for
  parallelism — even though t-1/t-2 both wire into ship.go and phase.go, they
  edit different functions.

- **New helpers get dedicated homes to keep wave-1 tasks from colliding.** t-1's
  auto-commit helper → new `internal/cmd/cleantree.go`; t-2's safety-net push
  helper → existing `internal/cmd/basebranch.go` (base-branch concern). Rejected:
  parking both in phase.go, which would pile all three wave-1 tasks into one file.

- **c-2 push wired into ship, complete, AND runDrossRecovery — not just
  "ship/complete pre-flight".** The locked `chore_push` decision names ship and
  complete, but `runDrossRecovery` itself commits a restore chore to the base
  (ship_recover.go:199-205) and would leave it unpushed, violating "after any
  ...recover, local base == origin/base". Adding the push at the recovery's own
  network-bearing tail honors the decision's rationale ("the commands already
  doing network absorb the push") without adding network to a local-only writer.

- **c-1 auto-commit is refuse-first on any non-.dross dirt.** The helper
  partitions `git status --porcelain` and commits only when *every* dirty path is
  under `.dross/`; any single non-.dross path routes to the existing
  `dirtyTreeError`. Rejected: committing the .dross subset and refusing the rest
  (a partial commit that hides real work-in-progress).
