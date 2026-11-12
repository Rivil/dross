# Planner draft — verification lens (test-contract first)

Method: for each criterion I wrote the ideal failing test first, then sized the smallest task that makes it satisfiable. Every task exists because a named test needs a surface to call.

## Ideal contracts, written before any task

- **c-3**: a pure `phase.EdgeProblems(spec)` table test with one row per unresolved shape (covered→`c-9` missing, covered with no ids, dismissed with blank or whitespace reason, deferred→id absent from the spec's `[[deferred]]`, deferred with empty id, class `nullness`, outcome `maybe`). Each row's message must name the record by index and class. A cmd test then checks that `dross validate` prints that same line and exits non-zero.
- **c-5**: a legacy fixture spec (no `[[edges]]`) gets `✓ all dross artefacts valid`. Load→Save of a legacy spec writes no `edges` key, so the file stays byte-clean.
- **c-2**: a spec with one record per outcome round-trips through Load/Save field for field. `dross deferred route` on that spec keeps the `[[edges]]`, because every deferred verb re-saves the Spec, so a field missing from the struct would silently strip them. The `/dross-spec` §5 schema shows `[[edges]]` and `[[deferred]] id`.
- **deferred_link (locked)**: `dross deferred new-id` prints `^[0-9a-f]{16}$` and leaves `.dross/` byte-identical.
- **c-7**: `dross state set current_phase_status planned` against a spec with a bad edge exits non-zero, names the record, and leaves `state.json` unchanged. Its message string is identical to validate's line for the same record, because there is one validator.
- **c-6**: needs a deterministic, machine-checkable partition (resolved / unresolved / open). Without one, c-6 can only be tested as a prompt needle. So this lens adds `dross phase edges <id> --json`.
- **c-1 / c-4**: prompt needles scoped to the edge-probe section, not the whole file. §2 already contains "accept / reword / drop", so a whole-file needle passes vacuously. A divergence test also ties the eight class names in the prompt to `phase.EdgeClasses`.

---

```
Phase spec-edge-probe — 6 tasks across 3 waves

Wave 1
  t-1  Add [[edges]] schema and edge validator
       files:    internal/phase/phase.go, internal/phase/edge.go, internal/phase/edge_test.go
       covers:   c-2, c-3, c-5
       description: Add Spec.Edges []Edge (toml/json "edges", omitempty) with Edge{Class, Outcome,
                    Criteria []string, Reason, Deferred}. In edge.go: EdgeClasses (the eight, in spec
                    order), the outcome set, EdgeProblems(spec) []string (one line per unresolved record,
                    naming index + class), and EdgeStatus(spec) returning resolved/unresolved/open classes.
       contract: - EdgeProblems table test: covered naming c-9 when only c-1..c-3 exist → one problem
                   containing "edges[0]", the class and "c-9"; covered with empty criteria → problem;
                   dismissed with reason "" or "   " → problem naming the class; deferred "abc" with no
                   [[deferred]] id "abc" in the same spec → problem naming "abc"; deferred "" → problem;
                   class "nullness" → problem listing the eight valid classes; outcome "maybe" → problem.
                 - A spec with one well-formed record per outcome → EdgeProblems returns nil (catches
                   over-eager checks: e.g. requiring a reason on covered).
                 - TestEdgeClassesExact: EdgeClasses == [boundary empty encoding ordering precision
                   idempotency concurrency adjacency]. Adding/renaming a class fails it.
                 - Round-trip: Save→LoadSpec on a spec with three records preserves every field.
                 - Legacy byte-clean: LoadSpec→Save of a spec with no [[edges]] produces no "edges" text.
                 - EdgeStatus: boundary covered + empty dismissed(reason) + encoding dismissed("") →
                   resolved=[boundary empty], unresolved=[encoding], open=the other five in EdgeClasses
                   order; a legacy spec → open = all eight.
                 - existing TestTomlFieldsCarryMatchingJSONTags (phase.Spec{} already in its list) fails
                   if Edge's json tags drift from its toml tags.
       depends:  —

  t-2  Add `dross deferred new-id` verb
       files:    internal/cmd/deferred.go, internal/cmd/deferred_add.go, internal/cmd/deferred_newid_test.go, README.md
       covers:   c-2
       description: New `deferred new-id` subcommand that prints mintDeferredID(root) (unique across
                    every phase spec + the project store) and writes nothing. Register it in
                    deferred.go's AddCommand and add `new-id` to the README `dross deferred {…}` row.
       contract: - Output is exactly one line matching ^[0-9a-f]{16}$.
                 - Fixture .dross tree (spec + deferred.toml) is byte-identical before/after the call —
                   a version that files an item or backfills ids fails.
                 - Two consecutive calls print different ids.
                 - root.Find([]string{"deferred","new-id"}) resolves (the verb is wired, not orphaned).
       depends:  —

Wave 2 (depends t-1)
  t-3  Report unresolved edges in `dross validate`
       files:    internal/cmd/validate.go, internal/cmd/validate_edges_test.go
       covers:   c-3, c-5
       description: In the per-phase spec walk, append EdgeProblems(spec) prefixed with the spec path,
                    for every spec that decoded (complete phases included — legacy ones have no edges).
       contract: - Fixture phase whose spec has `dismissed` with no reason → validate returns error,
                   stdout carries "✗ <specpath>: edges[N] (<class>) …" naming that record.
                 - One fixture per remaining shape (missing criterion, deferred id with no [[deferred]]
                   match, unknown class) → each fails and names its own record; a spec with two bad
                   records prints two ✗ lines (no early return after the first).
                 - c-5: fixture identical to a pre-phase spec (criteria + decisions + deferred, no
                   [[edges]]) → "✓ all dross artefacts valid", exit 0.
                 - A deferred edge whose id matches a [[deferred]] id in the SAME spec passes; the same id
                   present only in another phase's spec fails (pins same-spec matching).
       depends:  t-1

  t-4  Refuse plan-lock on unresolved edges
       files:    internal/cmd/state.go, internal/cmd/state_edges_gate_test.go
       covers:   c-7
       description: In stateSet, when field is current_phase_status and value is "planned", load the
                    current phase's spec.toml and refuse with EdgeProblems output before mutating state.
                    A missing spec or empty current_phase is not refused here.
       contract: - Current phase spec has `covered` → "c-9" (missing) → `state set current_phase_status
                   planned` returns error containing "edges[0]" and the class; state.json bytes unchanged
                   (CurrentPhaseStatus still its prior value, no history row appended).
                 - The refusal text equals the line validate prints for the same fixture minus the ✗/path
                   prefix — a second, divergent validator fails this.
                 - Spec with all-resolved edges → status becomes "planned".
                 - Legacy spec (no [[edges]]) → status becomes "planned" (c-5 holds at the gate too).
                 - Same bad spec, `state set current_phase_status in-progress` → succeeds (gate is scoped
                   to the plan-lock value only).
                 - existing TestStateSetSupportedFields stays green (no regression on other fields).
       depends:  t-1

  t-5  Add `dross phase edges` resolution report
       files:    internal/cmd/phase_edges.go, internal/cmd/phase.go, internal/cmd/phase_edges_test.go, README.md
       covers:   c-6, c-1
       description: `dross phase edges <id> [--json]` prints EdgeStatus for the phase's spec as
                    {resolved, unresolved, open} class lists (open in EdgeClasses order). Register in
                    Phase()'s AddCommand; add `edges` to the README `dross phase {…}` row.
       contract: - Spec with boundary covered, empty dismissed(reason), encoding dismissed("") → --json
                   has resolved=["boundary","empty"], unresolved=["encoding"], open lists exactly the
                   other five; "boundary" and "empty" never appear in open (the c-6 resume guarantee).
                 - Legacy spec → open == EdgeClasses (all eight), resolved/unresolved empty arrays, not null.
                 - Unknown phase id → non-zero exit naming the id.
                 - root.Find([]string{"phase","edges"}) resolves.
       depends:  t-1

Wave 3 (depends t-1, t-2, t-5)
  t-6  Add edge-probe step to /dross-spec prompt
       files:    assets/prompts/spec.md, assets/prompts/_spec.resume.md, internal/cmd/spec_prompt_test.go
       covers:   c-1, c-2, c-4, c-6
       description: Add a terse edge-probe step between §3 and §4 of spec.md. It runs `dross phase edges
                    <id> --json`, proposes the 2–3 open classes that apply, one AskUserQuestion per class,
                    and records each as covered / dismissed (reason) / deferred (mint the id via
                    `dross deferred new-id`, write [[deferred]] id). It may also propose a must-NOT
                    criterion through §2's gate. Extend §5's schema with `id` on [[deferred]] and an
                    [[edges]] block. Resume partial: propose only open/unresolved classes.
       contract: - Section-scoped (text between the edge heading and the next "## "): contains "2–3",
                   "one per turn" and "never walk all eight"; deleting the cap sentence fails.
                 - Divergence: every name in phase.EdgeClasses appears in the edge section — a ninth
                   class added in Go without the prompt fails.
                 - Section names all three outcomes plus "dross deferred new-id"; §5's schema block
                   contains "[[edges]]", "outcome", "reason" and an `id` key under [[deferred]].
                 - c-4: the edge section itself contains "never x when y" and "accept / reword / drop"
                   (so §2's own copy cannot satisfy it) and says an accepted must-NOT is recorded
                   covered naming its new criterion id.
                 - zero_edges: edge section says no minimum and that a non-applicable proposal is
                   recorded dismissed with a reason.
                 - c-6: expanded _spec.resume.md contains "dross phase edges" and an instruction to skip
                   any class already resolved.
                 - Every `dross deferred new-id` / `dross phase edges` span in the expanded prompt
                   resolves via root.Find (the TestProtectedPromptCommandsResolve pattern), so a renamed verb fails.
                 - existing TestPromptByteBudget keeps prompts/spec.md ≤ 15000 bytes (≈1.5 KB headroom
                   today); existing conservation manifest (testdata/prompt_instructions.txt) rows for
                   spec.md all still present; existing partials_test spec.md anchors untouched.
       depends:  t-1, t-2, t-5
```

## Coverage

- c-1 → t-6 (proposal behaviour, cap, one per turn), t-5 (open-class source), t-1 (EdgeClasses exact set)
- c-2 → t-1 (schema + round-trip), t-2 (deferred id minting), t-6 (prompt records each outcome)
- c-3 → t-1 (EdgeProblems rules), t-3 (validate surfaces + names record)
- c-4 → t-6
- c-5 → t-1 (byte-clean legacy save), t-3 (legacy spec validates), t-4 (legacy spec still locks)
- c-6 → t-5 (resolved/open partition), t-6 (resume partial uses it)
- c-7 → t-4

7/7 criteria covered.

## Judgment calls

- **Added `dross phase edges`; rejected having the prompt read `[[edges]]` from spec.toml by hand.** Without the verb, c-6 can only be tested as a prompt needle. With it, c-6 is a Go partition test, and the base prompt no longer has to restate the resolution logic. That matters with spec.md about 1.5 KB under its byte cap.
- **One validator (`phase.EdgeProblems`) shared by validate and state set; rejected separate checks.** t-4's contract asserts the two surfaces produce identical text, so they cannot drift.
- **A deferred edge matches a `[[deferred]]` id in the same spec; rejected project-wide matching (deferredByID).** The id is minted at spec-write time into this spec, and route never moves an item, so a cross-spec match could only be a mistake. t-3 pins this.
- **Unknown outcome counts as unresolved; rejected it as a decode error.** c-3 names only four shapes, but an outcome outside covered|dismissed|deferred cannot be resolved by definition. Reporting it through the same named-record path keeps the message shape consistent.
- **Complete phases are still edge-checked; rejected copying absorbedProblems' complete-phase skip.** Edges never change after spec lock, and every pre-phase spec has none, so the skip would protect nothing.
- **The c-7 gate fires only on value "planned"; rejected gating every status write or the adopt path in phase.go:291.** c-7 names the plan-lock step, and phase-adopt is not a plan lock.
- **A missing current-phase spec does not block "planned".** Refusing would be a new spec-presence gate that no criterion asks for.
- **Edge step kept in base spec.md; rejected a partial.** Every spec run probes edges, and partials are for off-common-path branches. The trigger vocabulary forbids a common-path fetch. Resume-only logic (c-6) does go in the existing `_spec.resume.md`.
- **No plan.md change; rejected adding a "refused → re-run /dross-spec" note.** c-7 is enforced by the CLI error text, and plan.md has under 600 bytes of budget headroom.
- **README rows ride the verb tasks (t-2, t-5); rejected a separate docs task.** Each is one line, and keeping it with the verb stops the docs drifting from it.
