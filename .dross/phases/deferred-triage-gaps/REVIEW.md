# Plan Review — deferred-triage-gaps

Reviewed: 2026-06-26
Plan: 4 tasks across 3 waves

## BLOCKING
(none)

Coverage is complete (c-1: t-1+t-2; c-2: t-1+t-3; c-3: t-4; c-4: t-4). No
task contradicts a locked decision — the boolean `dismissed` field
(dismissed_state), the `--undo` clear (dismiss_reversibility), the
someday-only guard with un-route guidance (dismiss_scope), and the §0
board-skip line (board_skip_signal) are all faithfully reflected. No
forbidden action: r-01 (make install after prompt/Go edits) is honored
explicitly in t-4.

## FLAG
- [granularity / wave-parallelism] t-2 and t-3 are both wave 2 and edit the
  identical file set — `internal/cmd/deferred.go` and
  `internal/cmd/deferred_test.go`. Tasks in the same wave are meant to run
  in parallel; two tasks mutating the same two files concurrently will
  collide (overlapping edits to `deferredList()`, `deferredEntry`, and the
  shared test file).
  Suggestion: either merge t-2+t-3 into one "dismiss command + list filter"
  task, or move one to wave 3 so they run sequentially.

- [wave-order] t-4 (wave 3) declares `depends_on = ["t-2"]`, but it does not
  strictly consume t-2's output. t-4 only edits `assets/prompts/inbox.md`
  and `inbox_prompt_test.go`; the prompt references the command by name
  (`dross deferred dismiss <source> <idx>` — already fixed by the spec/c-4)
  and the prompt test asserts on prompt text, not on the compiled command.
  The dependency is a soft "don't wire a command before it exists"
  preference, not a build dependency.
  Suggestion: drop t-4 to wave 2 for parallelism, or keep the ordering but
  record that the coupling is interface-contract, not output.

- [test-contract specificity] t-4's contract covers two of c-3's facets
  (board-off branch announces skip + continues; dismiss funnel references
  the CLI) but not the rest of c-3: that someday/unrouted items are actually
  listed and triaged "through the same funnel (phase / milestone-backlog /
  quick / dismiss)". Nothing asserts the four triage destinations are
  reachable for deferred items when board_sync is off.
  Suggestion: add a contract clause asserting the inbox prompt routes a
  board-off deferred item to the phase/milestone-backlog/quick/dismiss
  options, so the funnel half of c-3 has a named failure surface.

## NOTE
- [test-contract specificity] t-1, t-2, t-3 contracts are exemplary — each
  names the exact assertion, the file it lives in, the command that triggers
  it, and the precise failure (e.g. "an active entry round-trips with a
  spurious `dismissed = false` line"). This is the standard to keep.
- [forbidden-actions] t-1/t-2/t-3 edit Go but omit a make-install note; this
  is correct, not an omission — the gate is `go test -count=1 ./...` which
  compiles from source, so the installed binary's staleness is irrelevant
  until t-4 wires the installed prompt (where r-01 is correctly invoked).
- [structure] Clean dependency layering: schema (t-1) → command + list
  filter (t-2/t-3) → prompt wiring (t-4). Each wave genuinely builds on the
  prior one's contract.

## Summary
A tight, well-contracted plan with full coverage and no locked-decision or
rule violations; the only real issue is two wave-2 tasks editing the same
files, which should be merged or serialized before execution.
