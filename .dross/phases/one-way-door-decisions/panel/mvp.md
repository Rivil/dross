# mvp draft — one-way-door-decisions

```
Phase one-way-door-decisions — 5 tasks across 3 waves

Wave 1
  t-1  Add reversibility + confirmed to Decision
       files:    internal/phase/phase.go,
                 internal/phase/decision.go (new),
                 internal/phase/decision_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-8, c-9
       desc:     Decision gains `Reversibility` (toml "reversibility,omitempty") and
                 `Confirmed` (toml "confirmed,omitempty"); consts Reversible="reversible",
                 OneWay="one-way". decision.go adds ConfirmHash(d) (sha256 of key+"\x00"+choice,
                 hex), ReversibilityProblems(spec, requireTag bool) and UnconfirmedOneWay(spec)
                 — the one rule set validate, the gate and the verb share. Two not_paths rows.
       contract: - ConfirmHash: same key+choice → same hash; editing choice (or key) → different hash
                 - ReversibilityProblems: locked decision with "" tag reported only when
                   requireTag=true; tag "maybe" reported with requireTag=false too; an unlocked
                   untagged decision never reported
                 - UnconfirmedOneWay: returns the key of a locked one-way with empty Confirmed and
                   with a stale Confirmed (choice edited after hashing); returns nothing for a nil
                   decisions list, reversible-only, or unlocked one-way
                 - TestEveryPathShapedFieldIsDeclared fails if either not_paths row is missing
       pre:      none

Wave 2 (depends t-1)
  t-2  Add `dross phase confirm <phase> <key>` verb
       files:    internal/cmd/phase_confirm.go (new),
                 internal/cmd/phase_confirm_test.go (new),
                 internal/cmd/phase.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-3, c-8
       desc:     Verb (shaped like phase_mark_new.go) writes `confirmed = ConfirmHash(d)` on the
                 named decision and saves spec.toml. Refuses — nothing written — an unknown key,
                 an unlocked decision, or a decision not tagged one-way. Already-current hash is a
                 no-op line. Registered in phase.go; cli_tree.txt golden gains the row.
       contract: - confirm on a locked one-way key writes confirmed equal to phase.ConfirmHash(d)
                   (reloading spec.toml shows it)
                 - after the test edits that decision's choice, re-running confirm rewrites the
                   hash to the new value (re-confirm path for c-8)
                 - confirm on an unknown key / reversible decision / unlocked decision exits
                   non-zero and spec.toml bytes are unchanged
                 - cli_tree golden test fails if the `dross phase confirm` row is absent
       pre:      `go test ./internal/phase/ -run Decision` passes (t-1 landed)

  t-3  Mark one-way decisions in PR body + reviewer context
       files:    internal/ship/body.go,
                 internal/ship/body_test.go,
                 internal/review/context.go,
                 internal/review/context_test.go
       covers:   c-5, c-6
       desc:     "## Decisions locked" in the ship body and renderDecisions in the task-reviewer
                 context tag each one-way decision with a distinct marker (e.g. "⚠ one-way") that a
                 reversible or untagged decision never carries.
       contract: - body for a spec with one one-way + one reversible locked decision: the one-way
                   line contains the one-way marker, the reversible line does not
                 - reviewer Context.Body for the same spec: same split in "## Locked decisions";
                   unlocked decisions still omitted
       pre:      none beyond t-1

Wave 3 (depends t-1, t-2)
  t-4  Validate tags; gate plan-lock on unconfirmed one-way
       files:    internal/cmd/validate.go,
                 internal/cmd/validate_reversibility_test.go (new),
                 internal/cmd/state.go,
                 internal/cmd/state_oneway_gate_test.go (new)
       covers:   c-1, c-4, c-7, c-8, c-9
       desc:     validate adds reversibilityLines per spec: requireTag = !changes.Complete(root,id)
                 (legacy_boundary), unknown values reported everywhere. state.go adds
                 planLockOneWayGate, called with the edge/facts gates on `current_phase_status
                 planned`: refuses on ReversibilityProblems(requireTag=true) or any
                 UnconfirmedOneWay key, naming each key and the `dross phase confirm` fix.
       contract: - validate exits 1 naming the key for a locked untagged decision in an open phase
                 - validate prints ✓ for a complete phase (changes status complete) whose locked
                   decisions carry no tag (c-7)
                 - validate reports `reversibility = "maybe"` even in a complete phase
                 - `state set current_phase_status planned` refuses with two unconfirmed one-way
                   keys, the error names both, and state.json status is unchanged
                 - after `dross phase confirm` on both it succeeds; after editing one choice it
                   refuses again naming only that key (c-8)
                 - gate passes for a spec with no decisions, reversible-only, and an unlocked
                   untagged one (c-9)
       pre:      `dross phase confirm --help` resolves against the built tree (t-2 landed), so
                 the refusal message narrates a registered command

  t-5  Teach /dross-spec tagging + one-way confirm turn
       files:    assets/prompts/spec.md,
                 assets/prompts/_spec.one-way.md (new),
                 assets/partials.go,
                 assets/partials_test.go,
                 internal/cmd/spec_prompt_test.go
       covers:   c-2, c-3
       desc:     spec.md §3c (inline, common path): every locked decision gets a proposed
                 `reversibility`, one-way by default for file/schema formats, public CLI surface and
                 new dependencies, user may override; §5 schema shows the field. A fetch line
                 `**When** \`door=one-way\` … dross prompt partial spec one-way` loads the partial:
                 the AskUserQuestion confirm / make reversible / reopen turn right after the area
                 resolves, and `dross phase confirm <phase> <key>` per yes after §5's write.
                 TriggerVocabulary gains "door": {"reversible","one-way"}; vocab pin updated.
       contract: - spec_prompt test: expanded spec.md §3c names all three one-way defaults and the
                   override; §5 schema contains `reversibility =`
                 - spec_prompt test: the one-way partial contains the three options "confirm",
                   "make reversible", "reopen" as an AskUserQuestion and `dross phase confirm`
                   placed after the §5 write
                 - TestPromptByteBudget fails if spec.md exceeds 15000 bytes (165 bytes headroom
                   today — the confirm turn must live in the partial)
                 - TestCommonPathVocabularyPinned fails unless "door" is pinned with common value
                   "reversible"; TestCommonPathFetchesNothing keeps the partial off the common path
                 - TestShipPromptCommandsExist-style tree check: `dross phase confirm` resolves
       pre:      `wc -c assets/prompts/spec.md` read before editing; budget is 15000
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 tag field + validate reports missing/unknown on locked | t-1, t-4 |
| c-2 /dross-spec proposes tag with one-way defaults, overridable | t-5 |
| c-3 one-way confirm turn + recorded confirmation | t-2, t-5 |
| c-4 plan-lock refuses unconfirmed one-way, naming keys | t-4 |
| c-5 ship PR body marks one-way distinctly | t-3 |
| c-6 task-reviewer context marks one-way distinctly | t-3 |
| c-7 complete phase with untagged decisions: no validate problem | t-4 |
| c-8 edited choice voids confirmation | t-1 (hash), t-2 (re-confirm), t-4 (gate refuses again) |
| c-9 gate passes with no / reversible / unlocked decisions | t-1, t-4 |

9/9 covered.

## Judgment calls

- Merged validate wiring and the plan-lock gate into one task (t-4); rejected separate tasks — both are thin callers of t-1's shared rule set in one package, and splitting buys a task, not parallelism worth having.
- Put t-4 in wave 3 behind the verb; rejected wave 2 + a temporary narratedIgnore entry — the gate's refusal names `dross phase confirm`, and adding then removing an ignore row is churn.
- Hash = sha256(key + "\x00" + choice), full hex; rejected hashing `why` too — the locked decision says key+choice, and a why-tweak should not void a confirmation.
- Plan-lock also refuses a missing/unknown tag on the phase being locked; rejected gating only unconfirmed one-way — legacy_boundary says "plan-lock checks the phase being locked", so an untagged locked decision would otherwise slip past both validate-at-lock and the gate.
- Unknown tag values are reported on any decision, locked or not; rejected locked-only — legacy_boundary says "reported everywhere" and a misspelled enum is wrong wherever it sits.
- The confirm turn goes in a new `_spec.one-way.md` partial with trigger key `door`; rejected inlining — spec.md is 14835/15000 bytes, so inline text busts TestPromptByteBudget. The tag proposal itself stays inline because every locked decision hits it.
- t-5 touches 5 files; kept it as one task anyway because partials.go and partials_test.go are guard companions forced by the new partial, not separate work.
- Left out the docs/interaction-audit.md row for the new confirm turn; no test enforces per-decision-point rows and no criterion asks for it. The judge may add it to t-5 as a one-line companion.
- No `make install` task: r-01 matters only for live use of the prompt, not for any criterion's tests. Execute/verify handle it.
