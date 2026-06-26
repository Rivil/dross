# Phase phase-lifecycle-commands — 7 tasks across 4 waves

Lens: **failure modes drive the graph.** Each task owns exactly one failure surface
— the array off-by-one, the both/neither flag, the partial dir-move, the dangling
deferred target, the orphaned PR, the stale stored ordinal — and tests it there.

The 4 user verbs (move/insert/rename) decompose into shared risk primitives
(t-1 pure array ops, t-2 the ship-state gate) so the dangerous bits are written
and tested once, not re-derived per command.

Wave 1
  t-1  Pure array reorder/rename primitives
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   (infra for c-1, c-2, c-3)
       contract: InsertRelative(["a","b","c"], anchor="b", before=false) == ["a","x","b","c"]? no —
                 == ["a","b","x","c"]; if the after/before off-by-one regresses the position
                 assertion fails. RenameInArray replaces old in place preserving its index; if it
                 appends instead, the length+order assertion fails. MoveRelative to a slug's own
                 current position returns the slice unchanged; if no-op detection regresses the
                 idempotency case fails. Anchor resolution on a slug absent from the array returns
                 an anchor-not-found error; dropping that check fails TestReorderAnchorMissing.

  t-2  Ship-state inflight guard helper
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   (gate for c-2, c-3)
       contract: guardInflight refuses with a "merge first" error when phase/<slug> exists on
                 origin (shipped/awaiting-merge); if the guard is bypassed,
                 TestRenameRefusesShippedPhase fails. It returns nil for a planning/executing
                 phase with no origin branch; if it over-refuses, TestMoveAllowsInflightPhase
                 fails. Uses `git ls-remote --heads origin phase/<slug>` (token-free local signal,
                 same probe `phase complete` already trusts).

Wave 2 (depend t-1, t-2)
  t-3  `dross phase move` + shared reorder helper
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2
       depends:  t-1, t-2
       contract: `phase move x --after y --before z` errors "exactly one of --after/--before",
                 and the neither-flag invocation errors too — each has its own assertion.
                 An anchor slug not in the current milestone's phases array errors anchor-not-found.
                 After a successful move, every OTHER phase's spec.toml/plan.toml bytes and
                 phase/<slug> branch SHA are byte-for-byte identical to a pre-move snapshot; if
                 move touches anything but milestone.phases order, the byte-equality assertion
                 fails. Moving a phase to the position it already holds prints "already there,
                 nothing to do" and exits 0 (no-op special-cased before the anchor logic).
                 `dross validate` exits 0 afterward. Move on a shipped phase is refused via t-2.

  t-4  `dross phase rename` core (dir + array + id + branch + state)
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-3
       depends:  t-1, t-2
       contract: `phase rename old new` moves phases/old → phases/new, rewrites the milestone
                 array entry in place (index preserved, via t-1 RenameInArray), and sets
                 spec.toml phase.id = new — three separate assertions, each fails if its step lags.
                 Renaming to an existing slug (dir OR array entry) is refused BEFORE the directory
                 moves; if the target-exists check runs after the move, phases/old has already
                 vanished and TestRenameNoPartialMoveOnCollision catches the leftover. Renaming
                 the checked-out phase/old branch leaves HEAD on phase/new (git branch -m, no
                 remote touch) and updates state.current_phase; skipping branch -m fails the HEAD
                 assertion. rename old→old (self) succeeds quietly and is special-cased BEFORE the
                 target-exists check; if not, self-rename errors "already exists". Refused on a
                 shipped phase via t-2.

Wave 3
  t-5  `dross phase insert` command
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-1
       depends:  t-1, t-3
       contract: `phase insert "Title" --after y` scaffolds phases/<slug> (+ phase/<slug> branch)
                 and places the slug immediately after y in milestone.phases; if placement falls
                 through to the array tail, the index-of-new-slug assertion fails. A title whose
                 slug already exists is REFUSED (no UniqueSlug auto-suffix) and leaves no
                 directory behind; if create's auto-suffix path is reused, both the refusal
                 assertion and a stray-dir check fail. Reuses t-3's shared flag-validation helper,
                 so both/neither --after/--before errors here too. Every pre-existing phase's
                 spec/plan bytes are unchanged (snapshot equality); `dross validate` exits 0.

  t-6  Re-point deferred targets on rename
       files:    internal/cmd/phase.go, internal/cmd/deferred.go, internal/cmd/phase_test.go
       covers:   c-3
       depends:  t-4
       contract: after `phase rename old new`, a [[deferred]] item in ANY phase spec whose
                 target == old is rewritten to new; if the cross-phase scan (built on
                 collectDeferred) misses a spec, `dross validate` fails its dangling
                 deferred-target check ("target names no phase dir or milestone.phases entry").
                 Deferred items targeting other slugs are left untouched; if the rewrite is too
                 broad, TestRenameLeavesOtherDeferredTargets fails.

Wave 4
  t-7  Numbering recompute / no-stored-ordinal guard
       files:    internal/cmd/phase_test.go
       covers:   c-4
       depends:  t-3, t-5
       contract: after moving phase p from array position 3 to position 1, `dross phase number p`
                 prints 1; if any code path persisted an ordinal at move/insert time, it would
                 print the stale 3 and the assertion fails. After an insert, the new phase's
                 number equals its array index and the phase previously at that index shifts +1;
                 if numbering read a stored value instead of DisplayNumber(array), the shift
                 assertion fails. Asserts state.json and every spec.toml hold no numeric ordinal
                 field, pinning "ordinals are recomputed, never stored".

## Coverage
- c-1 (insert places at position, others byte-unchanged, validate green) → t-5 (on t-1, t-3)
- c-2 (move reorders array only, nothing else touched, validate green) → t-3 (on t-1, t-2)
- c-3 (rename moves dir+array+spec.id, re-points deferred, renames branch, validate green)
      → t-4 (dir/array/id/branch/state) + t-6 (deferred re-point); gated by t-2 (shipped refusal),
        built on t-1 (RenameInArray)
- c-4 (numbering recomputes from array position, never stale) → t-7 (on t-3, t-5)

Every criterion has a dedicated owning task plus an explicit `dross validate` green
assertion (c-1/c-2/c-3) — the spec's recurring guarantee is itself a test surface.

## Judgment calls
- Split rename into THREE tasks (t-4 mutation, t-2 gate, t-6 deferred) rather than one fat
  command: rename is a cluster of independent failure modes (orphaned PR, partial dir-move,
  dangling deferred target). Bundling them hides which one broke; isolating gives each its own
  red test. Rejected a single rename task as untestable-at-the-seams.
- Shipped/awaiting-merge detected via `git ls-remote --heads origin phase/<slug>` (t-2), not a
  provider PR-status API call. Rationale: `ship` clears state.current_phase, so the phase is no
  longer "current"; the surviving local signal is the pushed remote branch — exactly the probe
  `phase complete` already uses. Rejected an API/PR query (needs a token, network, mocking) and
  rejected reading CurrentPhaseStatus (cleared at ship).
- Flag validation (exactly one of --after/--before) + anchor resolution live in ONE shared cmd
  helper owned by t-3 (move); t-5 (insert) depends on it rather than re-implementing. Keeps the
  both/neither risk owned by exactly one task; the cost is insert→move being a real wave-3
  dependency. Rejected duplicating the helper to flatten the wave (would split one risk across
  two tasks).
- Anchor/order primitives (t-1) live in internal/phase as pure functions, deliberately
  table-tested in isolation from cobra/git/fs. Rejected inlining them into the command RunEs,
  where the off-by-one would only surface through slow end-to-end git fixtures.
- t-2 and t-3/t-4 both edit internal/cmd/phase.go and could collide on merge; kept them in
  separate waves/tasks anyway because the dependency is logical (gate before mutation), and the
  edits land in distinct functions. Sequencing, not file-locking, resolves it.
- c-4 (numbering) kept as a standalone verification task (t-7) even though DisplayNumber already
  derives from array position: the criterion's real risk is a NEW code path in move/insert
  quietly persisting an ordinal. One task owns proving that never happens.
