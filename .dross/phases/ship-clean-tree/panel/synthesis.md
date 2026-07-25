# Synthesis — ship-clean-tree

Judged three independently-drafted decompositions (risk / mvp / verification).
Scored, picked a skeleton, grafted concrete improvements, and recorded the
genuine structural divergences rather than papering over them.

## Scores

Scale: ✓✓ strong · ✓ adequate · ~ weak. One assessment per draft per dimension.

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| **risk** (7t/3w) | ✓✓ 5/5; c-2 owned by two tasks matching the spec's two named failure sources (pause-snapshot vs recovery-restore) | ✓✓ richest edge contracts — rename `R a -> b`, `.drosszz`/`notes-.dross.txt` false-prefix, empty/phantom-commit, SHA-unchanged-on-refusal | ~ finest but over-split — 7 tasks; some splits (t-6/t-7) driven by file-overlap not risk | ~ 3 waves, but wave 3 sequencing is same-region-conflict avoidance (executor concern), not strict output dependency |
| **mvp** (3t/1w) | ✓ 5/5; one task per criterion, c-3+c-4+c-5 all in t-3 | ✓ solid but coarse — three criteria's contracts bundled into t-3's acceptance block | ~ coarsest — t-3 is one non-atomic diff spanning three criteria and two files | ✓ all wave 1, internally consistent (bundling removes the cross-task dep), but hides the c-4→c-3 dependency inside one task |
| **verification** (4t/2w) | ✓✓ 5/5; c-3 isolated, c-4+c-5 merged (genuinely coupled reset block) | ✓✓ sharpest actionable contracts — names exact tests to flip (`TestPhaseCompleteMilestoneDivergedAborts` phase_test.go:1284, `TestPhaseCompleteRecoverHeals` :974), `PRMergedFunc` provider path vs ancestry fallback | ✓✓ best middle — one task per criterion except the two truly-coupled recover criteria | ✓✓ cleanest — only the strict output dep (c-4 recover needs c-3 origin-PR resolution) forces wave 2; file overlap explicitly rejected as a wave concern |

**Skeleton: verification.** It has the cleanest wave graph (a single, defensible
strict dependency instead of file-overlap sequencing), the most executable
contracts (concrete existing tests to flip, exact code paths), and a granularity
that splits by failure mode without the risk draft's over-fragmentation. Its two
weak spots — a slightly under-specified c-1 contract and a c-2 that under-plays
the recovery-restore push as a distinct failure source — are exactly what the
runners-up supply as grafts.

## Merged plan

Format: `t-N  [origin]  title — criteria`. 4 tasks across 2 waves.

### Wave 1

**t-1  [verification+mvp skeleton · risk contracts]  Auto-commit .dross-only dirty gates — c-1**
- files: `internal/cmd/cleantree.go` (new), `internal/cmd/ship.go`, `internal/cmd/phase.go`, `internal/cmd/cleantree_test.go` (new), `internal/cmd/ship_test.go`, `internal/cmd/phase_test.go`
- desc: One shared helper beside `dirtyTreeError`: partition `git status --porcelain`; all-under-`.dross/` → `git add .dross/` + one `chore(dross):` commit and proceed; any path outside → `dirtyTreeError`, stage nothing. Wire into all three gates named by the `autocommit_coverage` decision: `phaseComplete` (phase.go:299-305 gate), `forkPhaseBranch` (phase.go:478-484 gate), and a NEW pre-stage gate in ship RunE (ship has none today).
- contracts:
  - only `.dross/handoff.md` dirty → exactly one `chore(dross):` commit, command proceeds; same tree + `README.md` dirty → `dirtyTreeError`, zero commits (no partial commit of the .dross half). *[all three drafts]*
  - ship with a stray `src/x.go` edit refuses before any push; ship with only `.dross/` dirt auto-commits then pushes. *[risk+mvp]*
  - **[graft: risk]** rename entry `R  .dross/a -> .dross/b` parses both sides as under `.dross`; a path named `.drosszz/x` or `notes-.dross.txt` is NOT under `.dross` (prefix guard is `.dross/`, not substring) → refuses; a clean tree produces zero commits (no empty/phantom commit).

**t-2  [verification+mvp · mvp contract]  Safety-net push of base-ahead .dross chores — c-2**
- files: `internal/cmd/basebranch.go` (helper home, reusing doctor.go's `phaseCommitsOnMain`/rev-list), `internal/cmd/ship.go`, `internal/cmd/phase.go`, plus tests
- desc: `pushBaseIfAheadDrossOnly(repoDir, base)`: fetch; `rev-list origin/<base>..<base>` empty → no-op; every ahead commit `.dross`-only → `git push origin <base>`; any ahead commit touches non-`.dross` → refuse, push nothing (never auto-push code). Call as ship + complete pre-flight. Push failure is a hard returned error (`push_failure` decision). `pause.go` unchanged.
- contracts:
  - local base one `.dross`-only chore ahead → pre-flight pushes; `git rev-list origin/<base>..<base>` empty afterward. *[all]*
  - local base ahead by a `src/` commit → pre-flight refuses, pushes nothing; a push that fails (origin unreachable) aborts with a non-nil error and prints no success line. *[risk+mvp]*
  - **[graft: mvp]** after `dross pause`, local base may be ahead of origin — pause performs no push (writers stay local-only).

**t-3  [verification skeleton · risk+mvp]  Resolve recorded PR from origin in mergeGate — c-3**
- files: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- desc: In `phaseComplete`, resolve `recordedPR` from `git show origin/<base>:.dross/phases/<id>/changes.json` (post-fetch) when the working-tree `changes.json` lacks it (currently only the stale local read at phase.go:315). `mergeGate` gates on the origin-sourced PR.
- contracts:
  - working-tree `changes.json` absent/PR=0 but `origin/<base>` records PR #7 (merged) → `mergeGate` resolves 7 and calls `ship.PRMergedFunc` (provider path), NOT the merge-base ancestry fallback. *[verification's sharp form]*
  - **[graft: verification]** neither working tree nor origin carries a PR → `recordedPR` stays 0 and `mergeGate` still takes the ancestry fallback (unchanged behavior preserved).

### Wave 2 (depends t-3)

**t-4  [verification skeleton · risk grafts]  Milestone-aware recover after merge verification — c-4, c-5**
- files: `internal/cmd/phase.go`, `internal/cmd/ship_recover.go`, `internal/cmd/phase_test.go`, `internal/cmd/ship_recover_test.go`
- depends: t-3 (recover's pre-reset verification must gate on origin-sourced PR, not the stale file)
- desc:
  - (c-4) Restructure the `--recover` path so merge verification (`mergeGate` with the t-3-resolved PR) runs, THEN the destructive reset/heal (`runDrossRecovery`), THEN completion — so `--recover` heals the diverged/stale state its own error recommends it for, and never resets an unmerged branch.
  - (c-5) Parameterize `runDrossRecovery` by branch (drop the hardcoded `mainBranch`); remove the milestone abort at phase.go:352-361; pass the `resolveNewWorkBase` milestone branch so `--recover` resets/restores against `origin/milestone/<version>`.
  - **[graft: risk c-2 recovery source]** Push the restored `.dross/` tree at `runDrossRecovery`'s network-bearing tail, targeting the branch c-5 parameterized (main OR milestone), reusing t-2's hard-error-on-push-failure policy. This is the recovery-restore push the safety-net pre-flight cannot catch (the restore commit lands after pre-flight ran). Covers c-2 for both `dross ship recover` and `phase complete --recover`; c-2 is thus owned by t-2 (pre-flight) + t-4 (recovery tail).
- contracts:
  - diverged/stale main + PR #N merged + `--recover` → confirms merge via origin PR (t-3), resets main to `origin/main`, restores `.dross/`, does NOT refuse (assert `HEAD==origin/main`, `.dross/` present). *[verification+risk]*
  - **[graft: risk]** `--recover` with an UNMERGED recorded PR refuses BEFORE any `git reset --hard` — assert local base SHA unchanged after the refusal (proves verify-before-reset).
  - active milestone, base `milestone/v0.10`, diverged local `milestone/v0.10` + `--recover` → resets to `origin/milestone/v0.10` and restores; the "does not yet support milestone branches" abort is NOT returned (assert `HEAD==origin/milestone/v0.10`). *[all recover drafts]*
  - after a restore commit, local base == origin/base — `git rev-list origin/<base>..<base>` empty (recovery-restore push landed). *[graft: risk t-7]*
  - **[graft: verification]** existing tests to flip: `TestPhaseCompleteMilestoneDivergedAborts` (phase_test.go:1284, currently asserts the abort); extend `TestPhaseCompleteRecoverHeals` (:974) into the stale/PR-merged case.

### Coverage
- c-1 → t-1 · c-2 → t-2 (pre-flight) + t-4 (recovery tail) · c-3 → t-3 · c-4 → t-4 · c-5 → t-4. All 5/5 owned.

## Disagreements

### D1 — c-1: one task or two (classifier vs gate-wiring)?
- **risk** splits it: t-1 = pure classifier + commit helper (isolated unit tests for the parsing failure modes), t-2 = wiring the three gates. Argument: rename/quoting/`.drosszz` false-prefix correctness is where bugs live and deserves a test surface unpolluted by cobra/git integration.
- **mvp + verification** keep it as one task (helper + wiring together).
- **Provisional default: one task (t-1).** Majority (2 of 3) and it keeps the wave-1 surface small. But the risk edge-case contracts are grafted into t-1's acceptance, so the isolation benefit is preserved as tests even without the split.
- **Why it matters:** if the executor wants each atomic commit to prove one thing, the split gives the classifier its own regression net; folding risks a fat t-1 commit where a parser bug and a wiring bug are indistinguishable in history. Reversible cheaply — the helper is a pure function either way.

### D2 — c-2: where does the recovery-restore push live?
- **risk** makes it a dedicated task (t-7) in a *later wave*, sequenced AFTER the milestone-branch parameterization (t-6), so it pushes the correct (main OR milestone) base. Rationale: the restore commit lands after the pre-flight safety net already ran, and `dross ship recover` has no pre-flight at all — one helper can't catch it.
- **verification** folds the recovery push into the recover-rework task (its t-4) at `runDrossRecovery`'s tail.
- **mvp** folds it into the single c-2 push task (its t-2), pushing at the end of `runDrossRecovery`.
- **Provisional default: verification's placement — the recovery push lives in t-4** (the recover rework), NOT in the c-2 pre-flight task. Reason: t-4 already parameterizes `runDrossRecovery` by branch for c-5, so the push naturally targets the right base there; putting it in t-2 (wave 1) would push before c-5 exists and mis-target the milestone branch — the exact hazard risk's late-wave sequencing was guarding against, solved without a 7th task.
- **Why it matters:** get this wrong and either the milestone recovery push targets `main` (re-seeding the divergence the phase exists to kill) or the restore commit is left unpushed. This is the single highest-stakes structural call in the merge.

### D3 — c-3 / c-4 / c-5 grouping.
- **mvp** merges all three into one task (t-3): "one cohesive change to the completion/recover machinery."
- **risk** fully separates: c-3 (data source, t-4), c-4 (control-flow order, t-5), c-5 (milestone branch, t-6) — "they fail independently."
- **verification** splits c-3 alone, merges c-4+c-5 (same ~40-line reset block).
- **Provisional default: verification's split** (c-3 separate; c-4+c-5 together). c-3 changes WHERE the PR is read — a data-source bug with its own regression test and no shared code with the reset block. c-4/c-5 are the same reset-block rework (WHEN heal runs / WHICH branch) and would collide if split. Rejects mvp's all-in-one (non-atomic, three criteria in one diff) and risk's all-separate (c-4's reorder is barely testable apart from c-3's resolution — but that's a *dependency*, handled by the wave edge, not a reason to keep them in one task).
- **Why it matters:** the grouping decides how many independent regression tests gate the stale-tree completion bug. Too coarse (mvp) and a c-5 milestone regression hides behind a passing c-3 test; too fine (risk) and c-4/c-5 fight over the same 40 lines across two commits.

### D4 — wave count: 1, 2, or 3?
- **mvp**: everything wave 1 (bundling removed the cross-task dep; file overlap declared an executor merge concern).
- **verification**: 2 waves — only c-4's recover (needs c-3's origin-PR resolution) is a strict output dependency.
- **risk**: 3 waves, with wave 3 driven partly by same-region file-conflict avoidance (t-6/t-7 both mutate `runDrossRecovery`'s tail).
- **Provisional default: 2 waves (verification).** The one real build-time dependency is c-4→c-3; everything else is file overlap, which is an executor sequencing/merge concern, not a wave barrier. mvp's flat wave hides that genuine dependency inside a bundled task; risk's third wave pays wall-clock latency to dodge a merge conflict the executor can resolve.
- **Why it matters:** waves gate parallelism. An extra wave that only avoids co-editing serializes work that could run concurrently; a missing wave (mvp) lets an executor start the recover reorder before origin-PR resolution exists, reproducing the catch-22.
