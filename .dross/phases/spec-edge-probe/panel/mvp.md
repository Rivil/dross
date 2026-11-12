# Panel draft: mvp lens

Phase spec-edge-probe: 3 tasks across 2 waves

Wave 1
  t-1  Add [[edges]] schema and resolution check
       files:    internal/phase/phase.go, internal/phase/phase_test.go
       covers:   c-2, c-3, c-5
       description:
         Add `Edge{Class, Outcome, Criteria []string, Reason, Deferred string}` (toml+json tags) and
         `Spec.Edges` (`toml:"edges,omitempty"`), plus an exported `EdgeClasses` list of the eight classes.
         Add `func (s *Spec) EdgeProblems() []string`. Each problem names `edges[i] (<class>)`: unknown class,
         unknown/empty outcome, covered with no criteria or naming a missing criterion id, dismissed with a
         blank reason, deferred whose id matches no `id` in this spec's [[deferred]].
       contract:
         - an edge `covered` with criteria=["c-9"] in a spec that has only c-1 gives a problem containing "edges[0]" and "c-9"
         - `dismissed` with reason "" or "   " is a problem; with a non-blank reason it is not
         - `deferred = "abc"` with no [[deferred]] id "abc" is a problem; adding `[[deferred]] id="abc"` clears it
         - class "timezone" gives an unknown-class problem; outcome "maybe" or empty gives an unknown-outcome problem
         - a spec with no [[edges]] returns nil, and every checked-in `.dross/phases/*/spec.toml` (all legacy) returns nil (c-5 checked against real specs, not only a fixture)
         - LoadSpec→Save→LoadSpec keeps every edge field (otherwise `deferred route`/`add`, which re-save the spec, would silently drop edge records); a spec without edges saves with no `edges` key
       depends:  none

Wave 2 (depends t-1)
  t-2  Gate validate and plan-lock on edges
       files:    internal/cmd/validate.go, internal/cmd/validate_test.go, internal/cmd/state.go, internal/cmd/state_test.go
       covers:   c-3, c-5, c-7
       description:
         In the `dross validate` phase walk, add `spec.EdgeProblems()`, each prefixed with the spec path.
         In `state set`, when the field is `current_phase_status` and the value is `planned`, load
         `phase.Dir(root, s.CurrentPhase)/spec.toml`. If EdgeProblems is non-empty, return an error listing
         them and leave state.json unwritten. No current phase or no spec file means nothing is gated.
       contract:
         - `dross validate` on a fixture whose phase spec has a covered edge naming a missing criterion exits non-zero, and the output contains the spec path and "edges[0] (boundary)"
         - `dross validate` on a fixture with a legacy spec (no [[edges]]) plus a plan exits 0 (c-5 end-to-end through the command)
         - `state set current_phase_status planned` with an unresolved deferred edge errors and names the class; state.json's current_phase_status is byte-unchanged afterwards
         - the same call with every edge resolved sets "planned"; `state set current_phase_status executing` with an unresolved edge is NOT refused (the gate is scoped to the plan-lock value)
       depends:  t-1

  t-3  Add edge-probe step to /dross-spec
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go, internal/cmd/deferred.go, internal/cmd/deferred_test.go
       covers:   c-1, c-2, c-4, c-6
       description:
         Add a "## 2b. Edge probe" section to spec.md. It picks the 2–3 likeliest of the eight classes, one
         AskUserQuestion per turn, and never walks all eight. Each class gets an outcome: covered, dismissed
         or deferred. A proposed must-NOT ("never X when Y") criterion goes through §2's accept / reword /
         drop gate. Classes already recorded in [[edges]] are skipped. The §5 schema gains an [[edges]]
         example and an `id` on [[deferred]]. Add a `dross deferred new-id` subcommand that reuses
         mintDeferredID, prints a project-unique id and writes nothing. spec.md must stay ≤15000 bytes.
       contract:
         - every `phase.EdgeClasses` name appears in the §2b section, and §2b contains "2–3", "one per turn" and "never walk all eight"; drop any one and TestSpecPromptEdgeProbe fails
         - §2b contains "never x when y" and "accept / reword / drop" (must-NOT criteria use the §2 gate, c-4)
         - the expanded prompt tells the agent to skip any class already in [[edges]] (resume, c-6); deleting that sentence fails the test
         - the ```toml schema block in §5 decodes via phase.LoadSpec, contains ≥1 [[edges]] entry, and gives zero EdgeProblems (prompt and validator cannot drift); §5 or §2b names `dross deferred new-id`
         - `dross deferred new-id` on a fixture whose only deferred id is "aaaaaaaaaaaaaaaa" prints one line matching ^[0-9a-f]{16}$ that differs from it, and the fixture spec.toml is byte-identical afterwards
         - spec.md above 15000 bytes fails the existing TestPromptByteBudget, which bounds how much prose this step may add
       depends:  t-1

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (2–3 classes, one per turn, never all eight) | t-3 |
| c-2 (edge recorded with covered/dismissed/deferred outcome) | t-1 (schema + round-trip), t-3 (prompt writes it + `deferred new-id`) |
| c-3 (validate fails unresolved edge, names it) | t-1 (check), t-2 (wired into `dross validate`) |
| c-4 (must-NOT criteria through accept/reword/drop) | t-3 |
| c-5 (legacy specs still pass validate) | t-1 (nil on no edges + all real specs), t-2 (validate exit 0 on legacy fixture) |
| c-6 (resume proposes no resolved class) | t-3 |
| c-7 (plan-lock `state set ... planned` refuses unresolved edge) | t-2 |

All 7/7 covered.

## Judgment calls

- **One shared `Spec.EdgeProblems()` in internal/phase, rejected separate checks in validate.go and state.go.** c-3 and c-7 must agree on what "unresolved" means. One function means one test matrix (t-1). The cmd wiring then only needs smoke-level contracts.
- **Deferred link resolves against the same spec's [[deferred]] ids, rejected the project-wide `deferredByID`.** The spec writes its own [[deferred]] entries, and routing only sets `target` without moving them. A spec-local check keeps EdgeProblems pure, so `state set` can call it without a project-wide collect.
- **`criteria` is a list, rejected a single string.** One edge class can be covered by more than one criterion. It also matches the existing `Criterion.Deferred []string` shape.
- **Gate only the value `planned`, rejected gating every status write.** c-7 names the plan-lock step. Gating `executing`, `verified` and the rest would make a stale spec block recovery paths that have nothing to do with planning.
- **No current phase or no spec.toml lets `planned` through, rejected refusing.** Refusing would break plan-lock in repos or standalone paths with no spec. Validate already reports a missing spec where that matters.
- **The `new-id` verb rides in the prompt task, rejected a separate task or folding it into t-2.** Its only consumer is the spec prompt. A standalone task would be one small file, and adding it to t-2 would push that task to 6 files.
- **Edge step is §2b in the base spec.md, rejected a partial.** It runs on every spec, which is the common path, and partials must never be fetched on the common path. That puts the step under the 15000-byte cap (spec.md is 13445 now), so t-3 may need to trim existing prose. The budget test is the gate.
- **§2b sits after the criteria walk, before gray areas, rejected after §3/§4.** A must-NOT criterion (c-4) lands while §2's gate is still open, and covered edges can name the criteria just accepted. A deferred outcome flows into §4 routing normally.
- **Resume skip (c-6) goes in base §2b, rejected editing `_spec.resume.md`.** It mirrors §4's existing "skip any deferred item that already has a target" rule in the base prompt, and saves a file.
- **No duplicate-class or field-mismatch policing (e.g. covered + reason), rejected as speculative.** No criterion asks for it. c-6's skip-resolved rule keeps duplicates out in practice.
- **No edge check skip for completed phases (unlike absorbedProblems), rejected as speculative.** Deferred entries are dismissed, never deleted, so a completed spec's edge records stay resolvable.
