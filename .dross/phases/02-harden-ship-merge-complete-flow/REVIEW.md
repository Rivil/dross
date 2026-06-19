# Plan Review — 02-harden-ship-merge-complete-flow

Reviewed: 2026-06-19
Plan: 3 tasks across 2 waves

## BLOCKING
(none)

## FLAG

- [coverage / ordering] t-1 commits ship's state write at step 7 (ship.go:184–187),
  which runs *after* the push at step 5 (ship.go:142). The new `chore(dross): ship <id>`
  commit therefore lands on the local `phase/<id>` branch but is never pushed to origin.
  origin/phase/<id> (the ref the provider squash-merges) will not contain it, so the
  shipped-action + PR-URL content never reaches main's history — it only exists on a
  local branch that `phase complete` then force-deletes (`branch -D`, phase.go:245).
  c-1's text ("committed as part of ship") is satisfied locally and the tree is clean,
  so this is not a coverage gap — but the plan never states whether the commit should
  be pushed, and the author likely assumed parity with phaseComplete's commit (which
  is fine to leave local because complete runs entirely on main, never a doomed branch).
  Suggestion: t-1's description should explicitly decide push-or-not. If the intent is
  only "clean tree on return" (which c-1 literally requires), local-only is correct and
  the commit being discarded by complete is harmless — say so. If the shipped/PR-URL
  audit record is meant to survive, the ordering or a push-after-commit step is needed.

- [test contract] t-1's two contracts both presume a "real-ship test" that pushes to a
  live/origin remote and opens a PR via the provider. ship.go's PR step calls
  ship.OpenPR against `p.Remote.Provider` (github). The contract names the breaking
  surface well (dirty `git status --porcelain`; HEAD message + content), but it does not
  say how the provider/push are stubbed in `ship_test.go`. Existing ship_test.go is 7KB;
  if it has no push/PR harness, t-1 silently grows a fixture that isn't scoped here.
  Suggestion: name the existing test seam (mock provider? `--no-push`? local bare repo
  as origin?) the contract relies on, so the executor doesn't invent one.

- [wave order] t-3 (wave 2) genuinely depends on t-1 + t-2 (it's the end-to-end of both),
  so the wave split is justified — no change needed there. But note t-1 and t-2 are
  independent and correctly both in wave 1; good. (No action.)

## NOTE

- [strengths] Locked decision (fix_locus: fix in ship, keep complete's clean-tree
  assumption) is respected by every task — t-1 puts the commit in ship.go, t-2 leaves
  phaseComplete's `status --porcelain` clean-tree guard (phase.go:194–200) untouched,
  and adds only the remote-delete. No task contradicts the lock.

- [strengths] t-2's idempotency contract is concrete and correct: it names both failure
  modes (ref still on origin when delete is missing; non-nil error when the remote ref is
  already absent). phaseComplete already guards local `branch -D` behind a
  `rev-parse --verify` existence check (phase.go:244), so the executor has a clear
  pattern to mirror for the remote-delete's idempotency.

- [strengths] Every criterion is covered exactly once at the unit level (c-1→t-1,
  c-2→t-2) and then re-asserted end-to-end (c-3→t-3). Clean coverage with no orphans
  and no double-counting.

- [granularity] All tasks are 1–2 files, single-layer (CLI/Go). No split or merge
  candidates. t-3 is test-only (phase_test.go) which is appropriate for an e2e flow task.

- [check 6] No "set up X" stubs, no phantom files — all four referenced source/test
  files exist (verified on disk). No granularity inflation.

## Summary
Solid, well-scoped plan with clean coverage and a respected lock; the one substantive gap
is that t-1 never decides whether ship's new commit should be pushed, leaving its audit
content liable to be force-deleted by `phase complete` — clarify intent before executing.
