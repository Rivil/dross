Phase ship-clean-tree — 7 tasks across 3 waves

Lens: failure modes drive the graph. Every task owns one way this can break —
misclassified dirt, an unpushed commit, a stale-tree read, a wrong-branch reset —
and proves it dead with a test that fails if the guard regresses.

The three anchor risks this phase exists to kill:
  R-classify  auto-commit swallows NON-.dross work (or refuses .dross-only work)
  R-unpushed  a .dross chore lands on base locally and is never pushed → re-seeds divergence
  R-stale     complete/recover read a stale post-squash-merge working tree and refuse

Wave 1
  t-1  Add .dross-only dirt classifier + commit helper
       files:    internal/cmd/cleantree.go (new), internal/cmd/cleantree_test.go (new)
       covers:   c-1
       desc:     Parse `git status --porcelain` into paths (handle rename `R old -> new`,
                 quoted/spaced paths, untracked). Add allUnderDross(paths) predicate and
                 autoCommitDrossDirt(repoDir, action, choreMsg): empty tree → no commit;
                 all paths under `.dross/` → `git add .dross/` + chore commit; any path
                 outside → return dirtyTreeError(action, status) and stage nothing.
       contract: a `src/main.go` modification is classified NOT-dross-only, so
                 autoCommitDrossDirt returns dirtyTreeError and creates no commit;
                 a lone `.dross/handoff.md` modification commits exactly one
                 `chore(dross):` commit.
       contract: a path named `.drosszz/x` or `notes-.dross.txt` is NOT treated as
                 under .dross (prefix guard is `.dross/`, not substring) — refuses.
       contract: a rename entry `R  .dross/a -> .dross/b` parses both sides as
                 under .dross → dross-only; a clean tree produces zero commits
                 (no empty/phantom commit).
       depends:  —

  t-4  Source recorded PR from origin, not the working tree
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-3
       desc:     In phase complete, resolve recordedPR by reading the phase changes.json
                 from origin/<base> after fetch (`git show origin/<base>:.dross/phases/
                 <id>/changes.json`), falling back to the working-tree file only when
                 origin lacks it. mergeGate then gates on the origin-sourced PR number.
       contract: with the working-tree `.dross/phases/<id>/changes.json` absent or PR:0
                 but origin/<base> carrying PR #7 (merged), mergeGate resolves 7 and
                 proceeds via PRMergedFunc instead of falling back to the ancestry check
                 and refusing.
       depends:  —

Wave 2 (depends wave 1)
  t-2  Auto-commit .dross dirt at all three gates
       files:    internal/cmd/ship.go, internal/cmd/phase.go, internal/cmd/phase_test.go,
                 internal/cmd/ship_test.go
       covers:   c-1
       desc:     Replace the refuse-only dirty checks with autoCommitDrossDirt at the
                 three gates named by the autocommit_coverage decision: phase complete
                 (phase.go:304), phase create/start (forkPhaseBranch, phase.go:483), and
                 add the gate to ship (which currently has none) before it stages state.
       contract: `dross phase complete` with only `.dross/handoff.md` dirty completes
                 (auto-commits a chore) instead of the dirtyTreeError it raises today;
                 with `README.md` dirty it still refuses with dirtyTreeError.
       contract: `dross ship` on a tree with a stray `src/x.go` edit refuses (new gate),
                 not silently leaving it behind; with only `.dross/` dirt it auto-commits
                 and proceeds to push.
       depends:  t-1

  t-3  Safety-net push of base-ahead .dross chores
       files:    internal/cmd/cleantree.go, internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/ship_test.go, internal/cmd/phase_test.go
       covers:   c-2
       desc:     Add pushBaseChoresIfAhead(repoDir, base): fetch; if `git rev-list
                 origin/<base>..<base>` is empty → no-op; if every ahead commit's diff
                 (`git diff --name-only origin/<base>..<base>`) is under .dross → push;
                 else refuse (never auto-push code). Call it as a ship + complete
                 pre-flight. Push failure is a hard error (push_failure decision).
       contract: local main one commit ahead of origin touching only `.dross/` →
                 pre-flight pushes, and origin/main is fast-forward after; a following
                 `git rev-list origin/main..main` is empty.
       contract: local main ahead by a commit touching `src/` → pre-flight refuses and
                 pushes nothing; a push that fails (origin unreachable) aborts the
                 command with a non-nil error, not warn-and-continue.
       depends:  t-1

  t-5  Reorder --recover to heal before re-gating
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-4
       desc:     Restructure phase complete --recover so that in the diverged/stale
                 state it fetches, runs the authoritative merge verification against
                 origin data (t-4), and only then heals via runDrossRecovery — instead
                 of aborting at the stale gate before recovery can run. Verification
                 still precedes the destructive reset.
       contract: diverged main whose phase/<id> was squash-deleted, PR #N merged,
                 with --recover → completes (heals) instead of the
                 "cannot confirm ... refusing to complete" error it raises today.
       contract: --recover with an UNmerged recorded PR still refuses BEFORE any reset —
                 the test asserts local main's SHA is unchanged after the refusal.
       depends:  t-4

Wave 3 (depends wave 2)
  t-6  Support milestone base in --recover
       files:    internal/cmd/ship_recover.go, internal/cmd/phase.go,
                 internal/cmd/ship_recover_test.go, internal/cmd/phase_test.go
       covers:   c-5
       desc:     Parameterize runDrossRecovery to take the reconcile branch instead of
                 hardcoding p.Repo.GitMainBranch. Remove the milestone abort at
                 phase.go:352-361; pass the resolveNewWorkBase milestone branch so
                 --recover resets/restores against origin/milestone/<version>.
       contract: runDrossRecovery given branch "milestone/v0.10" resets to
                 origin/milestone/v0.10 (asserted), never origin/main.
       contract: milestone active + diverged milestone/<version> + --recover completes
                 by healing the milestone branch, instead of the
                 "does not yet support milestone branches" abort it raises today.
       depends:  t-5

  t-7  Push restored tree after recovery
       files:    internal/cmd/ship_recover.go, internal/cmd/ship_recover_test.go,
                 internal/cmd/phase_test.go
       covers:   c-2
       desc:     After runDrossRecovery commits the restored .dross/ tree, push it to
                 origin/<branch> (reusing t-3's hard-error-on-push-failure policy). The
                 delta no-op path (already in sync) pushes nothing. Covers both entry
                 points: `dross ship recover` and `dross phase complete --recover`.
       contract: after `dross ship recover` produces a restore commit, local <base> ==
                 origin/<base> (the commit reached origin) — `git rev-list
                 origin/<base>..<base>` is empty; a following ff-only from origin no-ops.
       contract: a push failure during recovery aborts with a non-nil error (the
                 restore commit is not left silently unpushed).
       depends:  t-3, t-6

## Coverage
- c-1 → t-1 (classifier/helper), t-2 (wire into ship + complete + create)
- c-2 → t-3 (base-ahead safety-net push, incl. pause auto-snapshot chore), t-7 (recovery-restore push)
- c-3 → t-4 (origin-sourced PR number)
- c-4 → t-5 (heal-before-regate ordering)
- c-5 → t-6 (milestone reconcile branch)

Every criterion c-1..c-5 is owned. 5/5.

## Judgment calls
- Split c-1 into t-1 (pure classifier + commit helper, exhaustively unit-tested for the
  parsing failure modes) and t-2 (wiring the three gates). Rejected one fat task: the
  R-classify risk — renames, quoted paths, `.drosszz` false-prefix, mixed dirt — is
  where correctness lives and deserves its own isolated test surface, unpolluted by the
  cobra/git integration of the gates.
- c-2 is covered by TWO tasks (t-3, t-7) because it has two distinct failure sources the
  spec itself names: "pause auto-snapshot" (a chore that gets auto-committed to base and
  must be pushed by the pre-flight safety net) and "recovery restore" (a commit made
  mid-recover, AFTER the pre-flight already ran, so runDrossRecovery must push it itself).
  One safety-net helper can't catch the recovery commit — it lands too late in complete
  and `dross ship recover` has no pre-flight at all. Rejected a single c-2 task: it would
  leave the recovery-restore commit unpushed, the exact re-seed this phase kills.
- Kept c-3 (t-4, data source) and c-4 (t-5, control-flow order) separate even though both
  target the stale-tree completion bug. t-4 changes WHERE the PR number is read from;
  t-5 changes WHEN heal runs relative to the gate. They fail independently — you can fix
  the data source and still catch-22 the ordering, or vice versa — so each earns a task
  and a distinct regression test. t-5 depends on t-4 so its pre-reset verification uses
  fresh origin data, not the stale file.
- Put the recovery-restore push (t-7) after the milestone-branch change (t-6) rather than
  parallel: both mutate runDrossRecovery's tail, and t-7's push must target the branch
  t-6 parameterized in. Sequencing them avoids a same-region conflict and lets t-7 push
  the correct (main OR milestone) base.
- Rejected merging t-5+t-6 into one "--recover rework": they share a code region but own
  different risks (catch-22 ordering vs wrong-branch reset). The lens keeps them apart so
  a milestone-reset regression can't hide behind an ordering test passing.
- t-3's safety net REFUSES rather than pushes when the ahead commits touch non-.dross
  paths — auto-pushing unreviewed code to a base branch is a worse failure than the
  divergence this phase fixes. The push_failure decision (hard refusal) is honored in
  both t-3 and t-7 so a dropped push can never be silently swallowed.
