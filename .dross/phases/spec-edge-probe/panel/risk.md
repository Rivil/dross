# risk lens — spec-edge-probe

Lens: every task owns a named set of failure modes and the test that kills them. No risk is owned twice; no task exists without a risk it owns.

Risk register (owner in brackets):
- R1 [t-1] Adding `Edges` to `phase.Spec` changes the bytes of a legacy spec on Save (`edges = []` appears) — every deferred verb that Load→Saves a spec (absorb, route, dismiss, EnsureIDs) would rewrite every old spec.
- R2 [t-1] A spec with `[[edges]]` loses them through a Load→Save by a deferred verb (BurntSushi silently drops unknown keys today; a missing/mis-tagged field reproduces that).
- R3 [t-2] Unknown class slips through: case (`Boundary`), whitespace (`" boundary"`), empty string, a ninth class.
- R4 [t-2] Outcome empty / unknown / misspelled (`dismiss`) is treated as resolved.
- R5 [t-2] `covered` with an empty `criteria` list, or one id of several missing, passes.
- R6 [t-2] `dismissed` with a whitespace-only reason passes.
- R7 [t-2] `deferred = ""` matches an id-less `[[deferred]]` entry (empty-string == empty-string) and passes; an id present in another phase's spec or the project store passes for this spec.
- R8 [t-2] A record carrying another outcome's field (dismissed + criteria) is accepted, so what resolved it is ambiguous.
- R9 [t-2] Same class recorded twice (the c-6 regression's footprint) passes silently.
- R10 [t-2] Problem messages don't name the record, or come out in map order (non-deterministic).
- R11 [t-3] `dross deferred new-id` writes something (creates `.dross/deferred.toml`, touches a spec) or returns an id already used anywhere in the project.
- R12 [t-3] new-id output isn't a single bare id line the prompt can paste; a broken spec elsewhere fails it without naming the file.
- R13 [t-4] `dross validate` fails a legacy spec, or double-reports a spec that didn't decode with edge noise, or stops at the first edge problem.
- R14 [t-5] `state set current_phase_status planned` refuses but still writes state.json / appends history (partial write).
- R15 [t-5] The gate fires for other statuses or other fields, or with no current phase / no spec (blocks legacy flows); or lets an undecodable spec through.
- R16 [t-5] Gate and validate disagree on what "unresolved" means (two rule sets).
- R17 [t-6] Resume re-proposes a resolved class because the agent recomputes resolution from memory after compaction; an unresolved record is shown as "resolved" and never fixed.
- R18 [t-6] Class vocabulary drifts between the validator and what the prompt offers.
- R19 [t-7] spec.md crosses the 15,000-byte cap (13,445 B today, 1,555 B headroom) or the edge step moves into a partial the common path must fetch.
- R20 [t-7] The §5 schema example in spec.md names a field the struct doesn't have (`criterion` vs `criteria`), so every spec the prompt writes silently drops it.
- R21 [t-7] A dropped must-NOT criterion still leaves its edge recorded as `covered` naming a criterion that was never written.

```
Phase spec-edge-probe — 7 tasks across 4 waves

Wave 1
  t-1  Add [[edges]] schema to phase.Spec
       files:    internal/phase/phase.go, internal/phase/edges.go, internal/phase/edges_test.go, internal/deferred/deferred_test.go
       covers:   c-2, c-5
       description: Add `Edge{Class, Outcome, Criteria []string, Reason, Deferred string}` with omitempty tags and
                    `Spec.Edges []Edge \`toml:"edges,omitempty"\``; declare the closed EdgeClasses (8, fixed order, each with
                    a one-line hint) and EdgeOutcomes (covered|dismissed|deferred) in edges.go. No rules yet.
       contract: - a legacy spec fixture (no [[edges]]) Load→Save is byte-identical; drop omitempty and `edges = []` appears → fails (R1)
                 - a spec with two [[edges]] (one covered with 2 criteria, one deferred) Load→Save→Load deep-equals the input → fails if any Edge field is untagged/misnamed (R2)
                 - EnsureIDs on a phase spec holding [[edges]] plus an id-less [[deferred]] backfills the id and leaves Edges intact → fails if a deferred-verb save drops them (R2)
                 - EdgeClasses has exactly the 8 spec names in order boundary, empty, encoding, ordering, precision, idempotency, concurrency, adjacency (R18 anchor)
       depends_on: —

  t-3  Add `dross deferred new-id` minting verb
       files:    internal/cmd/deferred.go, internal/cmd/deferred_newid.go, internal/cmd/deferred_newid_test.go, README.md
       covers:   c-2
       description: New read-only verb printing one id from mintDeferredID (unique across every phase spec + the
                    _project store); registered in Deferred(); README's `dross deferred {…}` row gains `new-id`.
       contract: - running it in a fixture with no .dross/deferred.toml leaves that file absent and every spec.toml byte-identical (R11)
                 - with existing ids seeded in a phase spec AND the _project store, a stubbed id source returning a used id first yields a different id (R11)
                 - stdout is exactly one line matching ^[0-9a-f]{16}\n — an extra label/prefix fails it (R12)
                 - a malformed spec.toml in another phase makes it exit non-zero with that spec's path in the error (R12)
                 - README deferred row missing `new-id` fails the readme needle test (same pattern as TestReadme… in deferred_add_test.go)
       depends_on: —

Wave 2 (depends t-1)
  t-2  Implement edge resolution rules + per-class status
       files:    internal/phase/edges_check.go, internal/phase/edges_check_test.go
       covers:   c-3
       description: `Spec.EdgeProblems() []string` (record-ordered, each message naming `edges[i] (class "x")` and the defect)
                    and `Spec.EdgeStatus() map[class]state` (open | resolved | unresolved). The ONLY rule set — validate,
                    the state gate and the listing all call it. Deferred ids resolve against this spec's [[deferred]] only.
       contract: - table test, one row per defect, each asserting the exact message names index + class:
                   class "Boundary" / " boundary" / "" / "latency" → unknown class (R3);
                   outcome "" / "dismiss" → unresolved (R4);
                   covered with criteria=[] → unresolved, with ["c-1","c-9"] where c-9 absent → names c-9 only (R5);
                   dismissed with reason "   " → no reason (R6);
                   deferred = "" with an id-less [[deferred]] present → still a problem; id that exists only in another spec → no matching entry (R7);
                   dismissed carrying criteria → foreign field (R8);
                   two records with class "empty" → duplicate, naming both indices (R9)
                 - a spec with 3 problems returns them in record order on 20 repeated calls (R10)
                 - a spec with zero edges returns nil problems and every class "open" (c-5 at the rule level)
                 - EdgeStatus marks a dismissed-without-reason record "unresolved", not "resolved" (R17 anchor)
       depends_on: t-1

Wave 3 (depends t-2)
  t-4  Wire edge problems into `dross validate`
       files:    internal/cmd/validate.go, internal/cmd/validate_edges_test.go, internal/cmd/testdata/legacy_spec.toml
       covers:   c-3, c-5
       description: In the per-phase walk, append `<specPath>: <EdgeProblems line>` for each decoded spec; nothing for
                    specs that failed to decode (already reported). Applies to every phase, complete or not.
       contract: - a copy of a real pre-phase spec (testdata/legacy_spec.toml, criteria+decisions+deferred, no edges) passes validate with "✓ all dross artefacts valid" (R13, c-5)
                 - a spec with a covered-missing-criterion record AND a dismissed-no-reason record → exit non-zero, output carries BOTH lines, each prefixed with the spec path and naming edges[i] (R13)
                 - a spec that fails to decode reports exactly one problem line for that file — no edge lines (R13)
                 - each of the four c-3 failure kinds (missing criterion, no reason, unmatched deferred, unknown class) fails validate via the CLI, one subtest each
       depends_on: t-2

  t-5  Gate plan-lock status on resolved edges
       files:    internal/cmd/state.go, internal/cmd/state_edges_test.go
       covers:   c-7
       description: In stateSet, before mutating, when field=current_phase_status and value=planned: load
                    state.CurrentPhase's spec; refuse with every EdgeProblems line if any. No current phase or no spec
                    → allow; spec that fails to decode → refuse naming the decode error.
       contract: - unresolved edge → exit non-zero, error names edges[i] + class, and state.json is byte-identical after (no status change, no history entry) (R14)
                 - same spec, `set current_phase_status in-progress` and `set current_phase other` succeed (R15)
                 - current_phase empty → `planned` succeeds; current phase with no spec.toml → succeeds (R15, c-5)
                 - malformed spec.toml → `planned` refused with the spec path in the error (R15)
                 - fully resolved spec → succeeds and status reads "planned"
                 - the refusal text for a fixture equals the EdgeProblems output validate prints for it (R16 — one rule set)
       depends_on: t-2

  t-6  Add `dross phase edges <id>` class listing
       files:    internal/cmd/phase.go, internal/cmd/phase_edges.go, internal/cmd/phase_edges_test.go, README.md
       covers:   c-6, c-1
       description: Prints all 8 classes in EdgeClasses order with hint and status from EdgeStatus: `open`,
                    `<outcome> (resolved)` or `unresolved: <problem>`; unknown-class records listed after. README phase row
                    gains `edges`.
       contract: - spec with boundary covered, empty dismissed-with-reason → those two lines read resolved, the other 6 read open (R17)
                 - dismissed-without-reason record prints `unresolved:` for that class, never `resolved` (R17)
                 - a record with class "latency" appears in the listing flagged unknown rather than vanishing
                 - phase with no spec.toml → all 8 open; nonexistent phase id → non-zero exit naming the id
                 - class order and names come from phase.EdgeClasses — reordering that slice changes output and the golden line order check fails (R18)
                 - README phase row missing `edges` fails the readme needle test
       depends_on: t-2

Wave 4 (depends t-3, t-6)
  t-7  Add edge-probe step to /dross-spec prompt
       files:    assets/prompts/spec.md, assets/prompts/_spec.resume.md, internal/cmd/spec_edges_prompt_test.go
       covers:   c-1, c-2, c-4, c-6
       description: New §2b after the criteria slate: run `dross phase edges <id>`, pick the 2–3 likeliest open classes,
                    one per AskUserQuestion turn, outcome covered / dismissed+reason / deferred (`dross deferred new-id`,
                    same id on the [[deferred]] entry); must-NOT ("never X when Y") candidates go through §2's
                    accept/reword/drop gate. §5 schema gains [[edges]] and [[deferred]] id. Resume partial: propose only
                    classes not shown resolved; replace resets [[edges]]. Hard cap: base prompt ≤ 15,000 B.
       contract: - expanded spec.md missing any of "2–3", "one per turn", "never walk all eight" fails (c-1)
                 - §5's fenced toml block decoded with toml.Decode into phase.Spec has MetaData.Undecoded()==0 and ≥1 Edge with class/outcome set — rename `criteria`→`criterion` in the prompt and it fails (R20, c-2)
                 - prompt missing "dross deferred new-id" or the instruction that the same id goes on the [[deferred]] entry fails (c-2)
                 - prompt missing "never x when y" or the must-NOT routing through the "accept / reword / drop" gate fails; missing the rule that a dropped must-NOT is recorded dismissed or deferred, never covered, fails (c-4, R21)
                 - _spec.resume.md missing "dross phase edges" + "already resolved" fails (c-6)
                 - TestPromptByteBudget/spec.md fails if the base prompt exceeds 15,000 B; TestSpecPromptBranchesFetchTheirPartial's common-path row must still fetch nothing (R19)
                 - existing conservation manifest rows for spec.md still present (TestInstructionConservation unchanged)
       depends_on: t-3, t-6
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 proposes 2–3 applicable classes, one per turn, never all eight | t-7 (prompt), t-6 (class vocabulary the prompt picks from) |
| c-2 each proposed class recorded with an outcome | t-1 (schema), t-3 (deferred id minting), t-7 (prompt writes the record) |
| c-3 validate fails unresolved records and names them | t-2 (rules), t-4 (validate wiring) |
| c-4 must-NOT criteria through accept/reword/drop | t-7 |
| c-5 legacy specs still validate | t-1 (byte-clean save), t-4 (CLI-level legacy fixture) |
| c-6 resume proposes no resolved class | t-6 (deterministic status listing), t-7 (resume partial uses it) |
| c-7 plan-lock status refuses unresolved edges | t-5 |

7/7 criteria covered.

## Judgment calls

- Chose one shared rule function (`Spec.EdgeProblems`) called by validate, the state gate and the listing; rejected separate checks in validate.go and state.go — two rule sets drift (R16), and c-3/c-7 must agree on "unresolved".
- Chose a `dross phase edges <id>` status listing for resume; rejected a prompt-only "read [[edges]] and skip resolved" — after compaction the agent re-derives resolution from memory, and a dismissed-without-reason record looks resolved to a skim (R17). It also holds the class hints, which keeps spec.md under the byte cap.
- Chose to keep the edge step in base spec.md (≤1.5 KB, hints come from the CLI); rejected a `_spec.edges.md` partial — the step runs on every spec, and the common path must fetch nothing (TestSpecPromptBranchesFetchTheirPartial).
- Chose same-spec lookup for a deferred edge's id; rejected project-wide lookup — the locked decision mints the id at spec-write time for this spec's own [[deferred]], and `deferred route` only stamps Target, so the entry never leaves the spec.
- Chose to reject records carrying another outcome's field (dismissed + criteria); rejected lenient extra-field tolerance — an ambiguous record defeats the "what resolved it" audit c-2 exists for. The judge may relax this if it reads as over-strict.
- Chose to flag duplicate class records in validate; rejected leaving dedup to the prompt — a duplicate is the on-disk trace of a c-6 failure and should be caught mechanically.
- Chose to check edges on complete phases too; rejected the absorbedProblems-style complete-phase skip — edge resolution is intra-spec and nothing outside the spec can invalidate it later.
- Chose: state gate allows when no current phase or no spec, refuses on an undecodable spec; rejected refusing on missing spec (breaks c-5 legacy flows) and allowing on undecodable (can't prove resolution).
- Chose to gate only `state set current_phase_status planned`; rejected also gating `phase create` adoption's derived "planned" — c-7 names the plan-lock step, and adoption reads an existing plan rather than locking one.
- Chose not to edit plan.md for the refusal path; rejected adding recovery prose — plan.md has 548 B of headroom and plan §6 already runs `dross validate` (which fails first, naming the same record) before the state set.
- Chose a pure, write-nothing `deferred new-id` over minting inside `deferred add`; the locked decision names a mint-at-write-time helper, and `add` targets current_phase which §6 sets only after the spec is written.
- Chose no guard for two concurrent sessions minting simultaneously; 64-bit random ids plus validate's same-spec match make a collision both negligible and caught.
