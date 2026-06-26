# Verification-biased decomposition — deferred-item-routing

Lens: design backward from the test contract. For each criterion I wrote the
ideal failing test first, then derived the smallest task that makes that test
green. Every task below names the concrete surface a regression would break.

Phase deferred-item-routing — 4 tasks across 3 waves

Wave 1
  t-1  Add optional target to Deferred schema
       files:    internal/phase/phase.go, internal/phase/phase_test.go, internal/cmd/cmd_test.go
       covers:   c-1
       contract: phase.TestSpecRoundTrip gains a Deferred{Target:"x"} entry that must survive
                 Save -> LoadSpec; drop the `toml:"target"` tag from the Deferred struct and the
                 reloaded Target is "" and the assertion fails.
       contract: a cmd_test validate case feeds one spec.toml with [[deferred]] target=<slug> and a
                 sibling whose [[deferred]] omits target; `dross validate` must exit 0 for both. If
                 validate ever rejects a target-bearing deferred entry, the present-target case fails.

Wave 2 (depends t-1)
  t-2  Add `dross deferred` (list + route) command
       files:    internal/cmd/deferred.go, internal/cmd/deferred_test.go, cmd/dross/main.go
       covers:   c-3, c-4, c-5, c-6
       depends:  t-1
       contract: with two phase specs on disk (one [[deferred]] target=alpha, one with no target),
                 `dross deferred list --someday` prints only the target-less row; if --someday stops
                 excluding routed entries the alpha row appears and the test fails.
       contract: `dross deferred list --target alpha --json` returns exactly the entries whose
                 target==alpha, each carrying a `source` field naming its originating phase; break the
                 --target match or omit the source field and the JSON length/source assertion fails.
       contract: `dross deferred list --routed` lists only target-bearing entries and
                 `--milestone <v>` scopes to that milestone's phases; dropping either filter changes
                 the row count and fails the test.
       contract: `dross deferred route <phase> <index> --target beta` stamps target=beta on that
                 phase's Nth [[deferred]] entry on disk; reload the spec and assert Target=="beta" —
                 fails if the stamp is not persisted (round-trip via t-1's field).

Wave 3 (depends t-2)
  t-3  Route deferred items in spec.md §4 + re-surface seed
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-2, c-3, c-5
       depends:  t-2
       contract: spec_prompt_test asserts §4 names all four destinations — pull-into-current-phase,
                 park-in-milestone-backlog, attach-to-named-future-phase, someday/unrouted; remove any
                 destination phrase and its sub-assertion fails.
       contract: asserts the park flow text chains `dross milestone add … phases <slug>` then
                 `dross deferred route … --target <slug>`; drop the append+stamp sequence and the
                 substring check fails (c-3 prompt half).
       contract: asserts the create-flow (§0/§1) seeds candidate criteria via
                 `dross deferred list --target <new-slug>` and instructs skipping any deferred item
                 that already has a target (no duplicate routing / no duplicate candidates); remove the
                 skip-already-routed instruction and the c-5 sub-assertion fails.

  t-4  Add deferred someday items as inbox triage source
       files:    assets/prompts/inbox.md, internal/cmd/inbox_prompt_test.go
       covers:   c-6
       depends:  t-2
       contract: inbox_prompt_test asserts inbox.md reads someday deferred items via
                 `dross deferred list --someday --json` as a second triage source alongside
                 `dross issue pull`, and routes each through the new-phase / milestone-backlog /
                 quick-task / dismiss funnel; remove the deferred source line or any funnel destination
                 and the matching substring assertion fails.

## Coverage
- c-1 -> t-1
- c-2 -> t-3
- c-3 -> t-2 (route stamp mechanism), t-3 (prompt: milestone add + route orchestration)
- c-4 -> t-2
- c-5 -> t-2 (--someday excludes routed, --target match), t-3 (prompt skip-already-routed + 1:1 re-surface)
- c-6 -> t-2 (--someday filter), t-4 (inbox prompt)

## Judgment calls
- Stamping target via a real CLI (`dross deferred route`) rather than prompt-side TOML edits — chosen because a Go mutation primitive is unit-testable (reload + assert Target), whereas a prompt editing spec.toml by hand is only assertable as prompt text; rejected the prompt-only stamp because c-3 demands the on-disk entry actually reflect the destination.
- Bundled `list` + `route` into one task (t-2) instead of two — they share the new deferred.go parent command and both scan .dross/phases/*/spec.toml; splitting them into separate waves would force same-file concurrent edits or push `route` to wave 3 and the spec.md prompt that calls it to wave 4. One CLI-layer task with four sharp contracts keeps regressions isolated without the file-coupling.
- Pull-into-current-phase left as prompt-orchestrated (Write moves [[deferred]] -> [[criteria]]) with no CLI primitive — chosen because no criterion tests that move's on-disk result directly (c-2 only asserts the funnel offers it); adding a `deferred pull` command would be scope the test contracts don't demand. Rejected gold-plating it.
- c-1's validate assertion placed in the existing internal/cmd/cmd_test.go (which already exercises `dross validate`) rather than a new file — the field addition alone satisfies the contract since LoadSpec is non-strict; the test is the proof, so it rides the existing harness.
- No interaction-audit.md change — `deferred list`/`route` are non-interactive; spec and inbox already have audit sections, and the audit test only enumerates interactive commands, so nothing new is required.
- Prompt tasks (t-3, t-4) waved after t-2 because their text must name t-2's real command/flag spelling; their tests read assets/ source so they pass pre-`make install` (r-01 still applies before the prompts are live in ~/.claude).
