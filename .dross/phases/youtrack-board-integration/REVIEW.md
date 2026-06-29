# Plan Review — youtrack-board-integration

Reviewed: 2026-06-29
Plan: 10 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [granularity/scope — t-5] The int→string id migration is broader than t-5's
  description and contract admit. Today `internal/board/board.go` keys Phases,
  Quicks AND Milestones as `map[string]int` and Dismissed as `[]int`, and every
  call site in `issue.go` (lines 201/217/280/291/388/410/451/517/551) passes the
  forge `Issue.Number` (int) — while the new YouTrack client exposes a string
  `Issue.Key` (per t-2). To compile, all four board fields must flip to string
  together, plus the `issue dismiss` and manual `issue set-phase` CLI arg parsing
  in the same file. t-5's test_contract only asserts SetPhase/IsLinked/IsDismissed
  and openBoard resolution — it never asserts Quick or Milestone links round-trip
  as strings, nor how the existing forge `Issue.Number` reconciles with the
  string key. A partial migration could ship yet pass the stated tests. Note the
  locked `issue_identity` decision explicitly covers phase *and quick*.
  Suggestion: widen t-5's contract to assert Quick + Milestone string round-trip
  and the forge Number→string-key reconciliation. The atomic-commit instinct is
  correct; the stated blast radius is just too small.

- [wave-order — t-6] t-6 is placed in wave 3 but its only `depends_on` is t-2
  (wave 1); it needs no wave-2 output, so per the wave-order rule it is mis-waved
  and could run in wave 2.
  Suggestion: move t-6 to wave 2, or add the genuine wave-2 dependency if one was
  intended.

## NOTE
- [strength] Test contracts are unusually specific — exact endpoints
  (`POST /api/issues`), exact ids (`PROJ-7`), named failing tests, and negative
  paths (unmapped-state warn-skip, missing-agile-board degrade). Low ambiguity to
  execute against.
- [strength] t-3's prompt test reads `assets/prompts/inbox.md` source directly
  (the test helper's own comment states it "reads the assets/ source directly...
  not make install"), correctly sidestepping the r-01 installed-binary trap.
- [strength] All three locked `milestone_mode` values (version/agile/epic) are
  present in t-6 with one test each incl. the agile graceful-degrade; bearer auth,
  graceful state-skip, [board]-only resolution (no [remote] fallback) and
  readable-id all map to concrete contracts. No locked-decision conflicts found.
- [coverage] All six criteria are covered: c-1 (t-1, t-4, t-5), c-2 (t-2),
  c-3 (t-3, t-8), c-4 (t-5, t-6, t-9), c-5 (t-7), c-6 (t-10 — owned).
- [t-2] "client + factory together" holds: `forge.New` returning the YouTrack
  client requires `YouTrackClient` to exist, so bundling client + interface +
  factory in one compile unit is correct, not an over-merge.
- [t-3] t-3's prompt references `board.enabled`, whose config arm is added by t-1;
  both are wave 1 with no `depends_on` linking them. Harmless because t-3's test
  is a static grep, but the prompt is functionally dead until t-1 lands.

## Summary
Well-specified plan with full criterion coverage and no locked-decision
conflicts; the only real risks are t-5's under-scoped id migration (Quicks,
Milestones and Dismissed all flip int→string and the forge Number→Key
reconciliation, none of which the contract asserts) and a mis-waved t-6.
