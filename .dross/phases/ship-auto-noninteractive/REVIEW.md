# Plan Review — ship-auto-noninteractive

Reviewed: 2026-07-01
Plan: 3 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [wave-order] t-3 is still `wave = 2` / `depends_on = ["t-1"]`, and this was NOT
  one of the three amendments applied — the concern from the prior review persists.
  t-3 edits only `assets/prompts/ship.md` and `internal/cmd/ship_prompt_test.go`,
  both disjoint from t-1's files (`ship.go` / `ship_test.go`), and its test asserts
  on prompt *text* only via `shipPromptContent` — it never invokes the `--auto`
  binary flag. So t-3 does not strictly need any wave-1 output; the only tie is the
  conceptual "don't document a flag before it exists." Under the strict rule this is
  a flag, but it's mild and a defensible deliberate choice if the author wants the
  prompt copy to trail the implementation. Dropping t-3 to wave 1 would recover the
  parallelism at no correctness cost.

## NOTE
- [resolved] Prior FLAG (t-2 wave-order / composability): RESOLVED. t-2 now carries
  `depends_on = ["t-1"]` and sits in wave 2. Its `ship --auto --json` contract line
  ("emits clean JSON … and zero reviewers requested") now runs strictly after t-1
  lands the `--auto` flag, so the cross-task assertion can pass.

- [resolved] Prior FLAG (t-1/t-2 granularity, same-file collision in wave 1):
  RESOLVED. Serializing t-2 behind t-1 (wave 2) removes the parallel collision on the
  shared `c.Flags()` block and `RunE` in `ship.go` / `ship_test.go`. Keeping them as
  two tasks is fine now that they no longer run concurrently.

- [resolved] Prior FLAG (antipattern, `--auto` reviewer-suppression under-scoped):
  RESOLVED. t-1's description now also gates the reviewer-facing narration and
  telemetry — "suppress the 'Reviewers requested: …' line and count zero reviewers
  in telemetry, rather than reading p.Remote.Reviewers directly." This matches the
  real code: the narration reads `p.Remote.Reviewers` at ship.go:226-228 and
  telemetry counts `len(p.Remote.Reviewers)` at ship.go:249, both independent of
  `opts.Reviewers`. A new, concrete contract covers it ("prints no 'Reviewers
  requested' line and records a reviewers count of 0 in telemetry"), and telemetry
  is test-observable (verify_test.go reads telemetry.jsonl after re-enabling), so the
  clause is feasible, not aspirational.

- [resolved] Prior NOTE (near-vacuous "no merge/branch-delete git call" clause):
  RESOLVED. That clause is gone from t-1's contract; c-4's no-merge guarantee now
  rests on the description ("no-merge behaviour are untouched") plus the verify-gate
  and reviewer contracts, which is the right surface — the binary never merges in any
  mode, so the removed clause could not meaningfully fail.

- [coverage] Full 1:1 criteria coverage confirmed after the amendments:
  c-1/c-3/c-4→t-1, c-5→t-2, c-2→t-3. No criterion is unclaimed.

- [locked-decisions] Both locked decisions remain faithfully reflected and the
  broadening did not introduce a conflict: `reviewers_under_auto` ("does not mutate
  remote.reviewers config") is honoured — suppressing narration and zeroing the
  telemetry count are read-side changes, not config mutations; `explicit_flags_win`
  is honoured with a dedicated contract (`--auto --body 'X'` sends 'X').

- [test-contract] Contracts remain concrete and name the breaking surface (empty
  `OpenOpts.Reviewers`, no "Reviewers requested" line, reviewers count 0, verify-gate
  "pending" error, explicit `--body` winning, "exactly one parseable JSON object with
  keys url/number/result"). No "tests pass" vagueness introduced by the amendments.

## Summary
The three amendments cleanly resolve three of the four prior flags plus the vacuous-
clause note: t-2 is now serialized behind t-1 (fixing both the wave-order and the
same-file collision), and t-1's reviewer-suppression is broadened to the narration +
telemetry path with a concrete, feasible contract. No locked-decision conflict was
introduced, coverage is still complete 1:1, and no new antipattern surfaced. The one
remaining item is the pre-existing t-3 wave-order flag, which the author chose not to
touch — it's a mild parallelism-only concern, non-blocking. Plan is good to execute.
