# Panel draft — verification lens

Method: each criterion's ideal test contract was written first (the failing
assertion a reviewer would want to see), then the smallest task that makes the
contract satisfiable was derived. Every rule lives once in `internal/phase`, so
validate, plan-lock, ship and review all assert against the same predicate,
the way `EdgeProblems` serves validate and the edge gate today.

```
Phase one-way-door-decisions — 7 tasks across 3 waves

Wave 1
  t-1  Add reversibility tag, hash and predicates
       files:    internal/phase/phase.go, internal/phase/decision.go (new),
                 internal/phase/decision_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     Decision gains `Reversibility` (toml "reversibility,omitempty")
                 and `Confirmed` (toml "confirmed,omitempty"); constants
                 reversible / one-way; DecisionHash(key, choice);
                 ReversibilityProblems(spec, requireTag bool) []string;
                 UnconfirmedOneWay(spec) []string (keys, record order).
       covers:   c-1, c-7, c-8, c-9
       contract: ReversibilityProblems(spec, true) on a locked decision with no
                 tag returns one line naming `decisions[0] (<key>)` and
                 "missing"; with requireTag=false it returns nothing (c-7 rule)
       contract: `reversibility = "oneway"` on a locked decision is reported in
                 BOTH modes, the line listing `reversible, one-way`
       contract: an unlocked decision with no tag yields no line in either
                 mode (tag_scope)
       contract: DecisionHash("ab","c") != DecisionHash("a","bc") — fails if
                 key and choice are concatenated without a separator
       contract: UnconfirmedOneWay returns [] when Confirmed ==
                 DecisionHash(key, choice); returns [key] after Choice is
                 edited (c-8); returns [key] when Confirmed is a hash copied
                 from another key with the same choice
       contract: UnconfirmedOneWay never lists a reversible, untagged or
                 unlocked one-way decision, and returns nil for a spec with no
                 [[decisions]] (c-9)
       contract: a spec loaded with no reversibility/confirmed keys saves with
                 neither key present (omitempty — 151 legacy specs stay
                 byte-clean)
       contract: TestEveryPathShapedFieldIsDeclared fails until
                 `phase.Decision.Reversibility reversibility` and
                 `phase.Decision.Confirmed confirmed` are filed in not_paths.txt

Wave 2 (depends t-1)
  t-2  Add `dross phase confirm` verb
       files:    internal/cmd/phase_confirm.go (new),
                 internal/cmd/phase_confirm_test.go (new),
                 internal/cmd/phase.go, cmd/dross/testdata/cli_tree.txt
       desc:     `confirm <phase-id> <key>` writes confirmed =
                 phase.DecisionHash(key, choice) into spec.toml; refuses
                 anything that is not a locked one-way decision; writes
                 nothing on refusal.
       covers:   c-3, c-8
       contract: confirm on a locked one-way decision writes `confirmed` equal
                 to phase.DecisionHash(key, choice); the reloaded spec's
                 UnconfirmedOneWay is empty
       contract: an unknown key is refused naming it and listing the spec's
                 decision keys; spec.toml bytes unchanged
       contract: a reversible, untagged or `locked = false` decision is
                 refused ("not a locked one-way decision"); spec.toml bytes
                 unchanged
       contract: a second confirm on an unchanged decision prints "already
                 confirmed" and leaves spec.toml byte-identical (idempotent)
       contract: after the choice is hand-edited, confirm rewrites the hash to
                 the new choice — the re-confirmation path c-8 relies on
       contract: phase id `../x` is refused by phase.ContainID before any read
       contract: TestCLITree fails until the `dross phase confirm` row is in
                 cli_tree.txt

  t-3  Report reversibility problems in validate
       files:    internal/cmd/validate.go,
                 internal/cmd/validate_reversibility_test.go (new),
                 .dross/phases/one-way-door-decisions/spec.toml
       desc:     Per-phase walk appends ReversibilityProblems(spec,
                 requireTag = !changes.Complete(root, id)), each prefixed by
                 the spec path. Tag this phase's own 4 locked decisions, values
                 agreed with the user (never guessed).
       covers:   c-1, c-7
       contract: an incomplete phase holding a locked untagged decision makes
                 validate print `✗ <specPath>: decisions[0] (<key>): missing
                 reversibility` and return an error
       contract: a phase whose changes.json says status=complete, holding the
                 same untagged decision, produces no reversibility line and
                 validate passes (c-7)
       contract: a complete phase with `reversibility = "oneway"` is still
                 reported (unknown values reported everywhere)
       contract: a phase with no changes.json counts as not complete — its
                 untagged locked decision is reported
       contract: `go run ./cmd/dross validate` on this repo exits 0 — fails if
                 the in-flight phase's own spec is left untagged
       precond:  run = "grep -c 'locked = true' .dross/phases/one-way-door-decisions/spec.toml", min = 4
                 why = "the backfill tags exactly the decisions present at plan time"

  t-5  Mark one-way decisions in PR body and reviewer context
       files:    internal/ship/body.go, internal/ship/body_test.go,
                 internal/review/context.go, internal/review/context_test.go
       desc:     "Decisions locked" lines carry a one-way / reversible marker;
                 renderDecisions prints `- <key> [one-way]: <choice>` /
                 `[reversible]`; untagged legacy decisions render exactly as
                 today.
       covers:   c-5, c-6
       contract: a ship body for a spec with one one-way and one reversible
                 decision: the one-way line contains the one-way marker, the
                 reversible line contains its own marker and NOT the one-way
                 one
       contract: an untagged decision's body line is byte-identical to
                 today's format (regression pin on shipped legacy phases)
       contract: reviewer context renders `- k1 [one-way]: ...` and
                 `- k2 [reversible]: ...`; an unlocked one-way decision is
                 still omitted from "## Locked decisions"

  t-6  Propose reversibility tag in /dross-spec
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       desc:     §3c capture adds `reversibility`, proposed per decision:
                 one-way for file/schema formats, public CLI surface, new
                 dependencies; else reversible; user overrides. §5 schema
                 example shows the tag. Condense existing prose to stay in
                 budget.
       covers:   c-2
       contract: TestSpecPromptProposesReversibility fails unless the expanded
                 §3c names all three one-way defaults (file/schema format,
                 public CLI surface, new dependency), the reversible fallback,
                 and the user override
       contract: TestSpecSchemaExampleDecodes fails if the §5 example's
                 locked decision has no valid tag
                 (ReversibilityProblems(spec, true) non-empty) or carries a
                 key phase.Spec does not read
       contract: the §5 example carries no `confirmed` key — fails if the
                 prompt invites hand-writing the hash
       contract: TestPromptByteBudget/spec.md fails above 15000 bytes
                 (headroom today: 165 bytes)
       precond:  run = "wc -c < assets/prompts/spec.md", max = 15000
                 why = "budget headroom is measured before any text is added"

Wave 3
  t-4  Gate plan-lock on unconfirmed one-way decisions   (depends t-1, t-2)
       files:    internal/cmd/state.go,
                 internal/cmd/state_oneway_gate_test.go (new)
       desc:     planLockOneWayGate runs after the edge gate on
                 `current_phase_status planned`: refuses on
                 ReversibilityProblems(spec, true) or any UnconfirmedOneWay
                 key, naming each, and points at `dross phase confirm`.
       covers:   c-4, c-8, c-9
       contract: a spec with two unconfirmed locked one-way decisions is
                 refused; the error names both keys; state.json is
                 byte-identical afterwards
       contract: after `dross phase confirm` (the real verb) on both keys,
                 plan-lock passes
       contract: confirm, then edit one decision's choice in spec.toml →
                 plan-lock refuses again naming only that key (c-8)
       contract: a locked untagged decision in the phase being locked is
                 refused (legacy_boundary: plan-lock checks its own phase)
       contract: plan-lock passes with no [[decisions]], with only reversible
                 ones, and with only `locked = false` one-way ones (c-9 — one
                 subtest each)
       contract: the narrated-command guard in cmd/dross/main_test.go stays
                 green — the refusal's `dross phase confirm` resolves
       precond:  run = "grep -c 'func phaseConfirm' internal/cmd/phase_confirm.go", min = 1
                 why = "the refusal narrates the verb and the tests drive it"

  t-7  Add one-way confirm turn partial   (depends t-2, t-6)
       files:    assets/prompts/spec.md, assets/prompts/_spec.one-way.md (new),
                 assets/partials.go, assets/partials_test.go,
                 internal/cmd/spec_prompt_test.go, docs/interaction-audit.md
       desc:     §3c fetch line `**When** \`door=one-way\` — a gray area
                 resolved to a locked one-way decision: \`dross prompt partial
                 spec one-way\``; TriggerVocabulary gains door:
                 {reversible, one-way}; partial asks confirm / make
                 reversible / reopen inline and runs `dross phase confirm
                 <phase-id> <key>` per yes after §5's write; audit row added.
       covers:   c-3
       contract: TestSpecPromptBranchesFetchTheirPartial gains
                 {"door","one-way"} → [one-way]; the common path still
                 fetches nothing
       contract: TestCommonPathVocabularyPinned fails unless door's first
                 value is "reversible"
       contract: the expanded partial offers exactly confirm / make
                 reversible / reopen as one AskUserQuestion, and places the
                 verb after §5 writes spec.toml and only on a yes
       contract: `dross phase confirm` in the expanded prompt resolves to a
                 real verb (Find on Phase()), and the prompt never shows a
                 `confirmed =` line to hand-write
       contract: docs/interaction-audit.md's `### dross-spec` table has a
                 one-way confirm row — asserted in spec_prompt_test
       contract: TestPromptByteBudget/spec.md still ≤ 15000 with the fetch
                 line added
```

## Coverage

- c-1 → t-1 (rule), t-3 (validate surface)
- c-2 → t-6
- c-3 → t-2 (the record), t-7 (the confirm turn)
- c-4 → t-4
- c-5 → t-5
- c-6 → t-5
- c-7 → t-1 (requireTag switch), t-3 (complete-phase exemption via changes.Complete)
- c-8 → t-1 (hash mismatch), t-2 (re-confirm), t-4 (gate refuses again)
- c-9 → t-1 (nil/reversible/unlocked predicate), t-4 (gate passes)

9/9 covered. Edge records: adjacency (c-7) → t-3's complete-phase subtest;
idempotency (c-8, c-4) → t-2's second-confirm no-op + t-4's edit-voids test;
empty (c-9) → t-4's no-decisions subtest.

## Judgment calls

- One predicate pair in `internal/phase` (ReversibilityProblems + UnconfirmedOneWay) consumed by validate and plan-lock; rejected per-surface checks — the edge gate shows one rule set is what keeps surfaces from disagreeing.
- `requireTag bool` parameter rather than validate filtering lines; rejected filtering because an unknown value must survive on complete phases and a string filter would be fragile.
- Plan-lock also refuses a missing/unknown tag on its own phase, not just unconfirmed one-ways; rejected "unconfirmed only" because an untagged locked decision could be an unflagged one-way door, and legacy_boundary says plan-lock checks the phase being locked.
- Unknown tag values are reported on any decision, locked or not; rejected locked-only because a typo'd tag is wrong wherever it sits and costs nothing to catch (missing tags stay locked-only per tag_scope).
- DecisionHash joins key and choice with a NUL separator, truncated to 16 hex like deferred ids; rejected bare concatenation (the ("ab","c")/("a","bc") collision is a test contract).
- `dross phase confirm` refuses reversible/untagged/unlocked decisions; rejected a permissive write because a hash on a non-one-way decision is a record nothing reads.
- Gate task (t-4) waits for the verb (t-2) so its tests drive the real verb and the narrated-command guard stays green; rejected parallel waves that would need a hand-computed hash in fixtures.
- Ship and review markers merged into one task (t-5); rejected two tasks — each is a one-file render change, under the granularity floor.
- Inline per-line marker in the PR body; rejected a separate "One-way decisions" subsection — keeps the existing section and its legacy pin intact.
- Prompt work split into t-6 (base §3c tagging, c-2) and t-7 (partial, c-3); spec.md has 165 bytes of headroom under the 15000-byte cap, so the confirm turn goes to `_spec.one-way.md` behind a new `door` trigger, and t-6 must condense prose to fit the tag rule.
- t-3 tags this phase's own spec (the only incomplete phase with locked decisions); rejected leaving it, since `dross validate` on the repo would go red the moment t-3 lands. Tag values come from the user, not the executor.
- No validate check for stale `confirmed` hashes; rejected — plan-lock is the gate c-4/c-8 name, and a validate warning would duplicate it.
