# phase-lifecycle-commands — plan (lens: design backward from test contracts)

Bias: write each criterion's ideal test contract first, then derive the smallest
task that makes it satisfiable. The load-bearing contract is "byte-for-byte
untouched" — so the first thing built is the *assertion machinery* that can prove
it (hash every OTHER phase dir + its array entry + its spec.id + its branch ref,
before and after), and every mutator's test is written to fire that assertion.

```
Phase phase-lifecycle-commands — 5 tasks across 3 waves

Wave 1
  t-1  Milestone array-order helpers
       files:    internal/milestone/milestone.go
                 internal/milestone/milestone_test.go
       covers:   c-1, c-2
       contract: InsertRelative([a,b,c], "x", anchor="b", before=false) → [a,b,x,c]
                 and before=true → [a,x,b,c]; an anchor not in the slice returns
                 ErrAnchorNotFound (not an end-of-array append); both/neither
                 before+after is a caller error; Move preserves every non-moved
                 element's relative order. If any of these regress, the table tests
                 in milestone_test.go fail — the cmd layer never has to re-test
                 placement arithmetic.

  t-2  Byte-for-byte snapshot test harness
       files:    internal/cmd/phase_lifecycle_test.go
       covers:   c-1, c-2, c-3
       contract: snapshotPhases(t,root) returns slug→sha256 over (relpath,bytes)
                 of every file under each phases/<slug>/, plus the phase's milestone
                 array entry and spec.phase.id; assertUntouched(t,before,after,
                 except...) fails if any non-excepted phase differs. A self-test
                 mutates one byte of a bystander phase's spec.toml and asserts the
                 hash flips and assertUntouched reports it — proving the "untouched"
                 guarantee is enforced, not vacuous. This harness is the substrate
                 the t-3/t-4/t-5 contracts assert through.

Wave 2 (depends t-1, t-2)
  t-3  Implement `dross phase move` + shared lifecycle plumbing
       files:    internal/cmd/phase.go
                 internal/cmd/phase_lifecycle.go
                 internal/cmd/phase_lifecycle_test.go
       covers:   c-2, c-4
       depends:  t-1, t-2
       description:
                 New phase_lifecycle.go houses the verbs and the helpers move/insert/
                 rename share: anchor-flag validation (exactly one of --after/--before),
                 loadCurrentMilestone, refuseIfShipped (git ls-remote --heads origin
                 phase/<slug> non-empty ⇒ open-PR window), and the idempotent no-op
                 check ordered BEFORE target-exists. move reorders the current
                 milestone's phases array via milestone.Move and writes nothing else.
                 Wired into Phase().AddCommand in phase.go.
       contract: after `phase move c --after a` the milestone array is [a,c,b] AND
                 assertUntouched(except=nothing) passes — move touches ZERO phase
                 directories, the strongest form of the byte-for-byte contract;
                 `phase move c --after c` (self-move) prints "already there" and leaves
                 the milestone .toml byte-identical (the no-op idempotency path, checked
                 before target-exists); passing both --after and --before, or neither,
                 errors; an anchor not in the array surfaces the anchor-not-found error;
                 `dross phase number b` reflects b's new slot and `dross validate` exits 0.

Wave 3 (depends t-3)
  t-4  Implement `dross phase insert`
       files:    internal/cmd/phase.go
                 internal/cmd/phase_lifecycle.go
                 internal/cmd/phase_lifecycle_test.go
       covers:   c-1, c-4
       depends:  t-1, t-2, t-3
       description:
                 Scaffold a new phase (dir + phase/<slug> branch, reusing phaseCreate's
                 machinery) then place it with milestone.InsertRelative at the anchor.
                 Refuse a target slug whose dir or array entry already exists (no
                 auto-suffix), reusing t-3's anchor-flag validation.
       contract: `phase insert "P" --after a` creates phases/<slug-of-P>/ and the array
                 becomes [a,<P>,b,c]; assertUntouched(except=<P>) proves a/b/c's dirs,
                 spec.phase.id values, branch refs, and array entries are byte-for-byte
                 unchanged; inserting a title that slugs to an existing phase errors with
                 the target-exists message instead of suffixing "-2"; both/neither anchor
                 flag errors; `dross validate` exits 0 and `dross phase number b` shows
                 b shifted down by one.

  t-5  Implement `dross phase rename`
       files:    internal/cmd/phase.go
                 internal/cmd/phase_lifecycle.go
                 internal/cmd/phase_lifecycle_test.go
       covers:   c-3, c-4
       depends:  t-1, t-2, t-3
       description:
                 Rename phases/<old>→phases/<new>, rewrite spec.phase.id (reuse
                 migrate.go's rewritePhaseID), swap the milestone array entry in place,
                 re-point every other spec's [[deferred]] whose Target==<old> to <new>,
                 and `git branch -m phase/<old> phase/<new>` when that local branch
                 exists. refuseIfShipped (t-3) blocks a phase with a live origin branch;
                 self-rename is the idempotent no-op.
       contract: after `phase rename old new`, phases/old is gone, phases/new exists with
                 spec.phase.id=="new", and assertUntouched(except=old/new) proves sibling
                 phases are byte-for-byte unchanged; a [[deferred]] item in a DIFFERENT
                 phase whose target was "old" now reads "new" (and `dross validate` stays
                 green — no dangling target); the local branch phase/old is renamed to
                 phase/new while other branches are untouched; when origin/phase/old
                 exists (shipped/open-PR), rename refuses with merge-first guidance and
                 leaves both the branch and phases/old on disk unchanged; `phase rename
                 old old` prints "already there" before the target-exists check and writes
                 nothing.
```

## Coverage

| criterion | tasks            |
| --------- | ---------------- |
| c-1       | t-1, t-2, t-4    |
| c-2       | t-1, t-2, t-3    |
| c-3       | t-2, t-5         |
| c-4       | t-3, t-4, t-5    |

Every criterion is covered. c-4 (ordinals recomputed from array position, never
stale) is delivered by the existing phase.DisplayNumber / `dross phase number`
reading the post-mutation array — each mutator task asserts a sibling's number
shifts, so no task stores an ordinal.

## Judgment calls

- Made the byte-for-byte snapshot harness (t-2) a first-class wave-1 task rather
  than inlining an ad-hoc "did the dir change" check in each command's test.
  Rejected inlining: it lets each author write a weaker, possibly vacuous
  assertion; a shared hashing harness with a self-test makes the hardest contract
  provable and uniform across c-1/c-2/c-3.
- Detect "shipped/awaiting-merge (open PR)" via `git ls-remote --heads origin
  phase/<slug>` being non-empty. Rejected a provider-API/PR-state lookup (network,
  unmockable in unit tests) and rejected a new state field: the locked
  branch_on_rename decision already says planning phases have no remote branch, so
  a pushed origin branch IS the in-repo proxy for the open-PR window, and it is
  testable with a local bare remote.
- Put the pure array-position arithmetic (InsertRelative/Move) in
  internal/milestone (t-1), unit-tested with table tests, not beside appendUnique
  in cmd/milestone.go. Rejected cmd-layer placement: positioning logic shouldn't
  require a full repo+git fixture to test, and keeping it pure lets the cmd tests
  focus on the side effects (dirs, branches, validate).
- Landed the shared cmd plumbing (anchor-flag validation, refuseIfShipped, no-op
  ordering) in the `move` task (t-3) and made insert/rename depend on it. Rejected
  a standalone "helpers" task (over-decomposed, no criterion of its own) and
  rejected duplicating the guard in two tasks: move is the minimal array-only
  mutator, so it is the natural, smallest carrier for the shared scaffold and it
  proves the "zero directories touched" extreme of the byte-for-byte contract.
- Applied refuseIfShipped to move as well as rename, per the locked inflight_guard
  decision wording ("move/rename ... refuse"), even though move only reorders the
  array — honoring the locked decision over the narrower technical reading that
  move can't orphan a PR.
- Reused migrate.go's rewritePhaseID for the spec.phase.id rewrite in rename
  rather than writing a second id-rewrite path. Chosen so validate's
  dir-name==plan/spec.id invariant has exactly one code path to satisfy.
