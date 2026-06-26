Phase phase-lifecycle-commands — 2 tasks across 1 wave

Lens: smallest task set that satisfies every criterion. The four criteria collapse onto
two machinery clusters: the array-placement family (insert + move share anchor resolution,
the splice, the exactly-one-of guard, idempotency, and validate-green) and the identity
family (rename, which shares directory/spec-id/branch machinery with the existing
`phase migrate`). c-4 needs no production code — ordinals are already derived from array
position (`phase.DisplayNumber`, `dross phase number`, status, PR title, version patch all
read the array), so it is delivered purely by tests inside t-1.

Wave 1
  t-1  Add phase insert + move commands
       files:    internal/phase/phase.go, internal/cmd/phase.go,
                 internal/phase/phase_test.go, internal/cmd/phase_test.go
       covers:   c-1, c-2, c-4
       desc:     Add pure array helpers InsertAt/MoveTo(order, slug, anchor, before) to
                 internal/phase/phase.go (anchor-not-found error; move-to-current-position
                 returns the slice unchanged for the no-op path). Wire `phase insert <title>`
                 and `phase move <slug>` in internal/cmd/phase.go via a shared resolveAnchor
                 helper (exactly one of --after/--before; anchor must be in the current
                 milestone's phases array). insert scaffolds the new phase like phaseCreate
                 (dir + branch + state + milestone registration) but splices its slug at the
                 anchor position instead of appending, refusing a slug that already exists;
                 move only re-orders the milestone array. Register both in Phase().AddCommand.
       contract: - if InsertAt/MoveTo compute the wrong index, the phase-pkg unit test
                   asserting the resulting []string for both --after and --before anchors fails.
                 - if insert touches any existing phase, the cmd test diffing every other
                   phase's dir/id/branch/artifacts byte-for-byte across the insert fails (the
                   byte-for-byte-untouched guarantee).
                 - if insert/move accept both or neither of --after/--before, the cmd test
                   asserting the "exactly one of --after/--before" error fails.
                 - if the anchor slug is absent from the milestone array, the cmd test
                   asserting the anchor-not-found error fails.
                 - if moving a phase to the position it already holds errors instead of
                   succeeding quietly, the move idempotency no-op test fails.
                 - if insert refuses to splice (or appends instead), the test asserting the
                   milestone phases array equals the expected order after --after/--before fails.
                 - if ordinals don't recompute, the c-4 test asserting `dross phase number
                   <slug>` returns the new 1-based position after an insert and after a move fails.
                 - if either op leaves the tree invalid, the test running `dross validate` and
                   expecting exit 0 after insert and after move fails.

  t-2  Add phase rename command
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-3
       desc:     Add `phase rename <old-slug> <new-slug>` in internal/cmd/phase.go: refuse a
                 new-slug already taken (dir or array) but special-case rename-to-own-slug as a
                 quiet no-op before that check; os.Rename phases/<old>→phases/<new>; rewrite
                 spec.toml phase.id via the existing rewritePhaseID (migrate.go); replace the
                 <old> entry in the milestone phases array in place; re-point every other
                 spec's [[deferred]] item whose Target==<old> to <new>; `git branch -m
                 phase/<old> phase/<new>` when that local ref exists; update
                 state.current_phase when the renamed phase is current. Inflight guard: refuse
                 with merge-first guidance when `git ls-remote --heads origin phase/<old>` is
                 non-empty (shipped/awaiting-merge proxy — planning/executing phases have no
                 remote branch). Register in Phase().AddCommand.
       contract: - if the dir isn't moved, the test asserting phases/<new> exists and
                   phases/<old> is gone fails.
                 - if spec.toml phase.id isn't rewritten, `dross validate`'s id-matches-dir
                   check fails in the rename test.
                 - if a deferred item targeting <old> isn't re-pointed, the test asserting the
                   re-pointed Target==<new> and `dross validate` exit 0 (no dangling target) fails.
                 - if the local phase/<old> branch isn't renamed, the test asserting
                   `git rev-parse refs/heads/phase/<new>` succeeds and phase/<old> is gone fails.
                 - if rename proceeds when origin carries phase/<old>, the inflight-guard test
                   asserting the merge-first refusal error fails.
                 - if renaming the current phase doesn't update state, the test asserting
                   state.json current_phase==<new> fails.
                 - if rename-to-own-slug errors instead of a quiet no-op, the idempotency test
                   fails; if renaming onto a different existing slug doesn't error, the
                   collision test fails.
                 - if any other phase is mutated, the test asserting untouched sibling dirs and
                   `dross validate` exit 0 fails.

## Coverage
- c-1 (insert): t-1
- c-2 (move): t-1
- c-3 (rename): t-2
- c-4 (ordinals recompute from array position): t-1

## Judgment calls
- 2 tasks, not 3+: folded the shared array-splice/anchor helpers INTO t-1 rather than a
  standalone helper task — they have exactly one consumer pair (insert+move) and are unit-tested
  at the phase-pkg level inside t-1; a separate task would add a wave and structure the lens rejects.
- c-4 gets no task of its own: ordinals are already derived from array position everywhere
  (`phase number`, status, PR title, version patch read `phase.DisplayNumber`), so c-4 is pure
  test coverage attached to t-1's insert/move contracts — not new production code.
- Split seam is array-family vs identity-family, not per-verb: insert+move share splice/anchor
  machinery so they co-locate; rename shares dir/spec-id/branch machinery with `phase migrate`
  (reuses rewritePhaseID) so it stands alone. Rejected one-task-per-verb (too granular) and
  one-task-for-all-three (too large, incoherent test contract).
- Both tasks are wave 1: rename needs none of t-1's output, so it stays wave 1 for parallelism
  even though both edit internal/cmd/phase.go (same-file edits to Phase().AddCommand are a merge
  concern, not an output dependency — waves encode dependency only).
- Inflight guard uses `git ls-remote --heads origin phase/<old>` as the shipped/awaiting-merge
  proxy rather than a provider PR-status API call: the spec's own branch_on_rename note states
  planning/executing phases have no remote branch, so remote-branch-present ≈ open PR, and it
  needs no GITHUB_TOKEN round-trip. Rejected adding a provider PR lookup as out-of-scope weight.
