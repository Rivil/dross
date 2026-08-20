# Plan Review — legacy-phase-backfill

Reviewed: 2026-08-20
Plan: 6 tasks across 3 waves

## BLOCKING
(none)

## FLAG

- [antipattern/collision] t-2 adds a provenance field naming the evidence commit SHA to changes.json, but does not say what JSON key it lands under, and does not list `internal/cmd/doctor.go`. `extractCommitSHAs` (doctor.go:951) scans the raw changes.json body for the literal `"commit":` and feeds every hit into `phaseCommitsOnMain`'s sha→phase map (doctor.go:902). If the provenance serializes as `"commit": "<sha>"` — including nested inside a `backfill` object — then after t-6's sweep 67 evidence SHAs enter that map. Those SHAs are exactly the ship commits on main, so any moment local main is ahead of origin/main (the normal post-merge window before push) doctor reports them as leaked phase commits and increments `issues`, i.e. a non-zero exit.
  Suggestion: pin the wire key in t-2's contract to something extractCommitSHAs cannot match (e.g. `backfill_evidence`/`"sha"`), or add a contract item asserting `phaseCommitsOnMain` ignores a backfilled record.

- [test-contract] No task pins what happens when the origin ref query fails. The locked `backfill_evidence` rule requires proving ref *absence*; `git ls-remote --heads origin phase/<slug>` errors when offline, when origin is unset, or on an auth failure, and if that error is read as "no ref" the gate inverts — `--apply` would mark phases whose branches are still live. t-3's four contract items cover stale-ref, anchor, ordinal and live-ref cases, but not the error case; t-5's doctor section inherits the same question (doctor should stay usable offline).
  Suggestion: add a contract item to t-3 fixing error semantics (fail the run / report the slug unbackfillable) and let t-5's doctor arm state the offline behaviour.

- [test-contract] t-3 says the subject scan reads "main", while the ref check deliberately reads origin via ls-remote. That asymmetry is unpinned: local main lags origin/main after every squash-merge until a fetch (the ship-recovery failure mode already in this repo's history), so a phase whose ship commit is only on origin/main reads unbackfillable and silently falls through to doctor's residue list. Nothing in the contract names which ref supplies the subjects.
  Suggestion: add a contract item pinning the ref (origin/<base> after fetch, or main with an explicit freshness step) so a stale-local-main fixture is red.

- [test-contract/locked-decision] The locked `backfill_write_gate` says the preview "doubles as c-5's residue listing", so it must include phases that failed the evidence tests. t-4's fourth item asserts "each preview line carries slug, verdict and commit SHA" — a residue line has no SHA by definition, so the contract as written is either unsatisfiable or is quietly assuming residue lines are absent. No task asserts the preview lists unbackfillable phases at all.
  Suggestion: split the item — SHA required on backfillable lines, presence (with verdict, no SHA) required on unbackfillable ones.

- [granularity/antipattern] The candidate set for `dross phase backfill` is never named. t-5 scopes doctor to milestone `phases` arrays (per the locked decision); t-6's completion check counts status-less phase *directories*. If backfill iterates directories the two scopes disagree, and the preview stops being the residue listing the write-gate decision says it is. (Today the two sets happen to coincide — v14-mutation-pass is on no roadmap and has no evidence either way — so no test would catch the divergence.)
  Suggestion: state the candidate set in t-4's description and pin it with a contract item.

- [granularity] t-5 applies the residue rule literally, and its fourth contract item locks that literalism in: an in-flight or unscaffolded roadmap phase is named. On this repo the section will permanently list legacy-phase-backfill (until completed) plus mirror-terminal-state, board-mirror-reaper and reentry-signal-truth — four entries that only say "planned work is not done yet", which the roadmap already says. That is the opposite of the standing-visibility the decision wanted.
  Suggestion: confirm the noise is intended, or raise the scoping (e.g. only phases on milestones marked complete) as a spec amendment before executing — not as a silent change at execute time.

## NOTE

- [strengths] The evidence claim checks out empirically. 69 phase records carry no status; the anchored `^phase (NN-)?<slug>:` pattern over main matches exactly 67, and the two misses are exactly legacy-phase-backfill and v14-mutation-pass. v14-mutation-pass is on no milestone's phases array, so the locked residue scope excludes it as claimed.

- [strengths] t-3's ls-remote justification is correct against the code: phase.go:741 uses `git ls-remote --heads origin` for exactly the stated reason, and this repo has one local `phase/*` branch left, so remote-tracking refs would be the wrong source.

- [strengths] t-6 names the real trap most sweeps miss — `liveRecordRoot` (cmd_test.go:533) copies tracked records into fixtures used by redproof_set_test.go:154 and doctor_test.go:2374, so rewriting 67 records can redden tests that never mention backfill. It also honours rule r-01 with `make install` before relying on the new command.

- [granularity] t-1 touches 6 files, which trips the split heuristic, but the split is not available: dropping the `*state.State` parameter from phaseDone/phaseIsDone breaks compilation until every caller moves in the same commit. The file list is complete — the only callers are phase.go:94/108/147, status.go:164 (renderMilestone, called only from status.go:59) and milestone_progress.go:118, plus milestone_progress_test.go's uses of historyCompletedPhase and phaseDoneState. Accept as one task.

- [wave-order] Wave assignment is tight: t-4 genuinely needs t-2's writer and t-3's resolver, t-5 needs t-3's resolver, t-6 needs t-4's command. Nothing in wave 2 or 3 could drop a wave. The t-3/t-4 split over the same file is real (pure verdict vs command wiring) and buys t-3 a wave-1 slot.

- [antipattern] t-6's `files = [".dross/phases"]` is a directory rather than files. Defensible for a 67-record sweep, but the recorded change entry will be less useful than the preview output it is derived from.

## Summary
No blocking defects — coverage is complete, nothing contradicts a locked decision outright, and the contracts are failure-shaped rather than vague — but six specificity gaps remain, of which the changes.json provenance key colliding with doctor's `"commit":` scan and the unspecified ls-remote failure semantics are the two that can turn a green sweep into a wrong one.
