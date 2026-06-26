# Risk-lens plan — deferred-item-routing

Lens: failure modes drive the graph. Start from what can break — back-compat
decode, dangling targets, filter leakage, duplicate re-surface, partial routing —
and make each failure mode owned and tested by exactly one task.

```
Phase deferred-item-routing — 6 tasks across 4 waves

Wave 1
  t-1  Add optional target field + round-trip
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-1
       contract: A target-less [[deferred]] entry loaded then Saved emits no
                 `target =` key (omitempty back-compat); an entry stamped
                 target="foo-slug" reads the slug back. If `target` is dropped
                 from the Deferred struct or loses omitempty, phase_test.go's
                 deferred round-trip test fails (a spurious target="" would
                 break every legacy spec on re-save).

Wave 2 (depends t-1)
  t-2  Validate accepts/guards target
       files:    internal/cmd/validate.go, internal/cmd/cmd_test.go
       covers:   c-1
       depends:  t-1
       contract: `dross validate` exits 0 on a spec with a target-less
                 [[deferred]] AND on one with target=<existing phase slug>; it
                 reports a problem when target names a slug that is neither an
                 existing phase dir nor any milestone.phases entry. If the
                 target-optional acceptance OR the dangling-slug check regresses,
                 the validate-deferred case in cmd_test.go fails.

  t-3  dross deferred list command + flags
       files:    internal/cmd/deferred.go, internal/cmd/deferred_test.go,
                 cmd/dross/main.go
       covers:   c-4, c-6
       depends:  t-1
       contract: Over a fixture of phases/*/spec.toml, `deferred list --someday`
                 omits every entry that carries a target; `--target <slug>` lists
                 only entries stamped with that slug; `--routed` is the exact
                 complement of `--someday`; `--milestone <v>` restricts to phases
                 in that milestone's phases array; `--json` emits each entry's
                 source phase id. If --someday leaks a routed entry or the source
                 column/field is absent, deferred_test.go fails.

Wave 3 (depends t-3)
  t-4  spec.md §4 routes every deferred item
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-2, c-3
       depends:  t-3
       contract: §4 instructs routing each deferred item to one of
                 pull-into-phase / park-in-backlog / attach-to-named-phase,
                 leaving it unrouted ONLY on an explicit "someday" pick; park
                 coins a slug, calls `dross milestone add <v> phases <slug>`, and
                 stamps target=<slug>. If the four-way funnel OR the
                 `dross milestone add` park step is removed from spec.md,
                 spec_prompt_test.go's routing-anchor assertions fail. (r-01: live
                 only after `make install`; test reads assets/ source.)

  t-6  inbox.md lists someday items as 2nd source
       files:    assets/prompts/inbox.md, internal/cmd/inbox_prompt_test.go
       covers:   c-6
       depends:  t-3
       contract: inbox.md pulls someday/unrouted deferred items via
                 `dross deferred list --someday --json` as a triage source
                 alongside board issues, and routes each through the same
                 new-phase / milestone-backlog / quick / dismiss funnel. If the
                 deferred triage source or its --someday backing is removed,
                 inbox_prompt_test.go fails; the interaction-audit section for
                 inbox still resolves.

Wave 4 (depends t-4)
  t-5  spec.md re-surface seed + dedup guard
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-4, c-5
       depends:  t-3, t-4
       contract: The create-flow (§0/§1) seeds candidate criteria from
                 `dross deferred list --target <new-slug>` (CLI, not a prompt
                 grep) and drops any whose text already matches an existing
                 [[criteria]] entry, so re-scaffolding the same phase proposes no
                 duplicate; an item whose target is already set is never re-offered
                 for routing in §4. If the dedup instruction or the
                 `dross deferred list --target` backing is removed,
                 spec_prompt_test.go's re-surface/dedup assertions fail.
```

## Coverage

| criterion | tasks      |
| --------- | ---------- |
| c-1       | t-1, t-2   |
| c-2       | t-4        |
| c-3       | t-4        |
| c-4       | t-3, t-5   |
| c-5       | t-5        |
| c-6       | t-3, t-6   |

All six criteria owned. Each headline failure mode has a single primary owner:
back-compat decode → t-1; dangling target → t-2; filter leakage → t-3;
incomplete routing → t-4; duplicate re-surface → t-5; inbox double-feed → t-6.

## Judgment calls

- Split the spec.md prompt into t-4 (routing completeness, c-2/c-3) and t-5
  (re-surface dedup, c-4/c-5) instead of one prompt task. Rejected the merge
  because the two headline risks of this phase — "an item left unrouted by
  accident" vs "an item re-surfaced twice" — are distinct failure modes; the
  RISK lens wants each independently testable. Serialized t-5 behind t-4
  (depends_on, wave 4) so two edits to the same file never race in parallel
  execution.
- Added a dangling-target check to t-2 beyond c-1's literal "validate passes
  either way." Rejected a decode-only validate task because a target naming a
  non-existent slug is a silent re-surface failure (the parked item never comes
  back) — exactly the kind of partial-failure the RISK lens must own. Kept it
  inside validate.go's existing artifact-checking remit.
- Made `dross deferred list` (t-3) the single backing for both the spec
  re-surface (c-4) and the inbox feed (c-6), per locked decision
  deferred_list_contract ("not a prompt-side grep"). Rejected duplicating scan
  logic in each prompt; one CLI with tested filters means the dedup/leak risk is
  verified once, in deferred_test.go, not re-litigated per consumer.
- Put the prompt tasks (t-4/t-5/t-6) in waves that depend on t-3 even though the
  *_prompt_test.go assertions are text-only and could technically run first.
  Rejected flattening them into wave 2 because the prompts must reference the
  CLI's real flag names; depending on t-3 keeps the plan honest about that
  contract coupling rather than referencing a flag surface that doesn't exist yet.
- Folded schema + loader into one wave-1 task (t-1) rather than separating the
  struct field from the round-trip test. They are one file-pair and <2 layers;
  the omitempty back-compat behavior only means anything when tested against the
  loader, so they ship together.
