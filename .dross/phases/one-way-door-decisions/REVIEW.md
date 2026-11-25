# Plan Review — one-way-door-decisions

Reviewed: 2026-11-25
Plan: 8 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [wave-order] t-8's third contract line needs t-6's gate, but t-8 does not depend on t-6. The line says "`dross state set current_phase_status planned` in a scratch copy (or the gate function in-process) reports no unconfirmed one-way key". t-8's `depends_on = ["t-2", "t-3"]`, and t-6 is also in wave 3. If t-8 runs first, `planLockOneWayGate` does not exist yet, and a scratch-copy plan-lock passes whether or not the confirmations landed. That is a vacuous green.
  Suggestion: either add t-6 to t-8's `depends_on`, which moves t-8 to wave 4, or rewrite that line against `phase.UnconfirmedOneWay` from t-1, which t-8 already has.
- [test-contract] t-6 filters `ReversibilityProblems` to locked decisions only, but t-1 provides no way to do that cleanly. t-1's API is `ReversibilityProblems(spec, requireTags bool)`, which returns `[]string` lines naming `decisions[i] (<key>)`. That leaves t-6 two options:
  - Parse the lines as strings.
  - Run the function on a copy of the spec that holds only the locked decisions. This renumbers the indexes, so the refusal names the wrong `decisions[i]`.

  No contract catches either option. There is also no t-6 line for a **locked** decision with an unknown value (e.g. `"oneway"`) refusing lock. Today's lines cover only "untagged locked refuses" and "unlocked unknown passes".
  Suggestion: give t-1 a locked-only mode or per-decision results, so the gate shares the one rule set without parsing strings. Then add two t-6 contract lines:
  - A locked decision with `reversibility = "oneway"` refuses.
  - A locked untagged decision at index 1, behind an unlocked one at index 0, is named `decisions[1]`.
- [antipattern] t-2 and t-8 both depend on asking the user, and that breaks under `--solo`. t-2 combines a Go change (validate wiring and its tests) with an AskUserQuestion step: tagging this phase's four locked decisions with values "asked from the user … never guessed". t-8 is entirely user confirmations. Under `--solo`, the task writer is an Edit/Write subagent that cannot ask the user. t-2's tagging step would then either stall or be guessed, and guessing is exactly what the description forbids.
  Suggestion: if this phase may run solo, say in t-2 that the main loop asks the tag question before it spawns the writer. Otherwise, record that this phase runs in pair mode.

## NOTE
- [prior-review] Every finding from the previous review is resolved in 3689cae8:
  - t-6 is now locked-only per `tag_scope`, with an unlocked-`"oneway"`-locks contract line.
  - This phase's own spec is tagged in t-2, so validate stays green through t-3 to t-7.
  - t-8 has a binary positive-control precondition and now depends on t-2.
  - t-5 proposes a tag for every decision (c-2).
  - The `DoorLabel` unlocked case is pinned.
  - t-5 now has a ≤ 14,850-byte ceiling contract.
  - t-1 adds JSON tags plus a contract line for them.

  One gap remains: t-4 never checks how the ship body renders an unlocked one-way decision. `internal/ship/body.go:67-80` prints unlocked decisions too, so that rendering is pinned only indirectly, through t-1's `DoorLabel` contract.
- [live-state] Checked against the live tree today:
  - `assets/prompts/spec.md` is exactly 14,835 bytes, which equals t-5's `max`, so there is no slack if anything touches the file first.
  - `dross phase facts one-way-door-decisions` reports nothing unresolved.
  - `dross validate` is green.
  - State is `planned`.
  - The `dross task add/edit` code never sets `current_phase_status`, so t-6's gate will not fire again on this phase during execution.
- [preconditions] Two of t-8's preconditions are fragile:
  - `strings $(command -v dross) | grep -q 'missing reversibility'`: if `dross` is not on PATH, `strings` gets no file argument and reads stdin. The precondition then fails late or opaquely instead of naming the missing binary.
  - `dross phase confirm --help | grep -q 'confirm <phase-id>'`: this depends on t-3's cobra `Use` string, which t-3 does not pin. The `mark-new <phase-id>` convention makes a match likely, but nothing guarantees it.
- [granularity] t-7 (6 files) and t-3 (5 files) pass the 5-file threshold, but each stays in one layer, and the extra files are companion files a guard test checks (the partial, `TriggerVocabulary` and its pin test, the interaction-audit row, the `cli_tree.txt` golden, the README phase row). Splitting them would add tasks without reducing risk.
- [strengths] The amendment turned prose gates into enforced preconditions:
  - t-5 `max = 14835`.
  - t-7 `max = 14850`.
  - t-6 requires `phase_confirm.go` to exist.
  - t-8 checks the verb, the decision count and the binary string.

  So the byte-budget handoff between t-5 and t-7 is now checked at both ends.
- [strengths] t-1's hash contract is specific:
  - a separator collision pair
  - a pinned 64-hex literal
  - phase id excluded as an input
  - a hash copied from another key
  - JSON key casing

  t-3 adds a check that `scanDrossArtifacts` stays silent on committed hashes, which avoids tripping the secret self-scan.
- [strengths] The legacy boundary is tested on real data (t-8 runs over about 150 complete specs) as well as fixtures. t-2 reuses `changes.Complete`, the same predicate `absorbedProblems` uses, so "complete" has one definition.

## Summary
The amendment fixed the previous blocker and all six flags, and the plan is ready to execute. Three cheap fixes remain: t-8 should depend on t-6 or check against `UnconfirmedOneWay`, t-6 needs a locked-only filter that keeps the true indexes, and the user-ask steps in t-2 and t-8 need a decision on pair versus solo mode.
