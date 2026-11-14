# Panel synthesis — spec-edge-probe

Judge notes on repo facts checked before scoring:
- `mintDeferredID` lives in `internal/cmd/deferred_add.go:49`; `Deferred()` registers verbs at `internal/cmd/deferred.go:27`; `Phase()` at `internal/cmd/phase.go:28`. No `edges` verb exists yet.
- `stateSet` (`internal/cmd/state.go:95`) calls `s.Touch(...)` (appends history) then `s.Save` — so "no history row on refusal" is a real, checkable contract.
- `TestPromptByteBudget` is in `assets/budget_test.go`. The conservation test is `TestPromptInstructionsConserved` (`assets/conservation_test.go`) — risk's "TestInstructionConservation" does not exist. spec.md is 13,445 B (1,555 B headroom); plan.md 14,452 B (548 B).
- `"accept / reword / drop"` already appears twice in spec.md (§1 line 36, §4 line 118), so a whole-file needle for c-4 passes vacuously. Verification's section-scoping point is correct; risk's c-4 contract is vacuous on that phrase.
- `internal/phase/plan_lossless_test.go` already globs `../../.dross/phases/*/plan.toml` (148 phase dirs, all tracked), so mvp's real-corpus c-5 check has precedent and is not vacuous.
- `TestTomlFieldsCarryMatchingJSONTags` walks nested struct fields from `phase.Spec{}`, so it covers a new `Edge` type automatically.
- `TestSpecPromptSkipRuleStaysInBaseSection4` pins §4's deferred skip rule to base spec.md, not the resume partial. This is the precedent mvp cites for keeping the c-6 skip rule in base.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 7/7. c-6 is backed by a mechanical status listing. c-5 is proven at both the schema and the CLI layer. | Highest. Each defect has its own table row (case/whitespace class, `dismiss`, partial-criteria, empty deferred id, record order). Two flaws: the c-4 `accept / reword / drop` needle is whole-file and therefore vacuous, and it cites a nonexistent test name. | 7 tasks. The schema-only t-1 and the rules-only t-2 sit in one package and could be one task, so the split adds a wave without adding parallelism. | 4 waves. The t-1→t-2 edge is real but self-inflicted by the split. The rest is correct. |
| mvp | 7/7, but c-6 is proven by a prompt needle only (no mechanical resolved/open partition). | Good. §2b-scoped needles. The §5 schema block decodes into a Spec and must give zero problems. The real-corpus c-5 check is the best c-5 contract of the three. Cmd-layer contracts are thin. | 3 tasks, too coarse. t-2 bundles two separate surfaces (validate and the state gate). t-3 mixes a Go verb with a prompt rewrite across two layers. | 2 waves, correct. Merging the verb into the prompt task loses wave-1 parallelism. |
| verification | 7/7. Every criterion has a Go-level test surface, including c-6 via `--json` partition. c-5 is checked at the gate too. | Strong. Section-scoped c-4 needles avoid the vacuous pass. A Go↔prompt class-name divergence check. `root.Find` wiring checks. Cross-surface identical-text check (validate vs gate). Weakness: the §5 schema contract is string needles, not a decode. | 6 tasks, each one surface, ≤4 files, one layer. | 3 waves. Every dependency is strict: t-6's `root.Find` contract needs t-2 and t-5. |

**Skeleton: verification.** It has the right task boundaries (one surface per task, schema and rules merged so no wave is wasted), it is the only draft with no vacuous contract, and each of its dependency edges is needed by a named test. Risk supplies the sharper defect rows and the safety contracts. Mvp supplies the real-corpus c-5 check, the decode-based §5 contract and the §2b placement.

## Merged plan

```
Phase spec-edge-probe — 6 tasks across 3 waves

Wave 1
  t-1  Add [[edges]] schema, EdgeProblems and EdgeStatus        [verification+mvp, risk contracts]
       files:    internal/phase/phase.go, internal/phase/edge.go, internal/phase/edge_test.go,
                 internal/deferred/deferred_test.go
       covers:   c-2, c-3, c-5
       description: Add Edge{Class, Outcome, Criteria []string, Reason, Deferred string} with matching
                    toml/json tags (omitempty), and Spec.Edges `toml:"edges,omitempty"`. In edge.go, add:
                    EdgeClasses (the eight, in spec order, each with a one-line hint), the outcome set
                    (covered|dismissed|deferred), and EdgeProblems(spec) []string, which emits one line per
                    unresolved record in record order, each naming `edges[i] (<class>)` and the defect.
                    Add EdgeStatus(spec), which partitions classes into resolved / unresolved / open.
                    Deferred ids resolve against this spec's own [[deferred]] only. This is the ONLY rule set:
                    validate, the state gate and the listing all call it.
       contract: - EdgeProblems table, one row per defect, each asserting the message names index + class:
                   class "nullness" / "Boundary" / " boundary" / "" → unknown-class problem listing the eight [verification+risk R3];
                   outcome "maybe" / "dismiss" / "" → problem [verification+risk R4];
                   covered with criteria=[] → problem; covered ["c-1","c-9"] where only c-1..c-3 exist → names c-9 and not c-1 [risk R5];
                   dismissed with reason "" or "   " → problem [all];
                   deferred "abc" with no [[deferred]] id "abc" → problem naming "abc"; adding [[deferred]] id="abc" clears it [mvp];
                   deferred "" while an id-less [[deferred]] exists → still a problem (no empty==empty match) [risk R7];
                   two records with class "empty" → duplicate problem naming both indices [risk R9 — see D5]
                 - one well-formed record per outcome → nil (no over-eager rule such as requiring a reason on covered) [verification]
                 - a spec with 3 bad records returns them in record order on 20 repeated calls (no map-order output) [risk R10]
                 - TestEdgeClassesExact: EdgeClasses == [boundary empty encoding ordering precision idempotency
                   concurrency adjacency]; adding or renaming a class fails it [verification+risk]
                 - Save→LoadSpec on a spec with three records preserves every Edge field [all]
                 - a legacy spec with no [[edges]] goes through LoadSpec→Save byte-identical, and dropping
                   omitempty makes it fail [risk R1+verification]
                 - deferred.EnsureIDs on a phase spec that holds [[edges]] plus an id-less [[deferred]] backfills
                   the id and leaves Edges intact. Fails if a deferred-verb re-save strips edges [risk R2]
                 - every checked-in ../../.dross/phases/*/spec.toml returns nil EdgeProblems. The glob must find
                   >0 specs, as in plan_lossless_test.go, so the check cannot pass vacuously [mvp]
                 - EdgeStatus: boundary covered + empty dismissed(reason) + encoding dismissed("") →
                   resolved=[boundary empty], unresolved=[encoding], open=the other five in EdgeClasses order;
                   a legacy spec → open = all eight [verification+risk R17]
                 - existing TestTomlFieldsCarryMatchingJSONTags fails if Edge's json tags drift from its toml tags [verification]
       depends_on: —

  t-2  Add `dross deferred new-id` minting verb                 [risk+verification]
       files:    internal/cmd/deferred.go, internal/cmd/deferred_newid.go, internal/cmd/deferred_newid_test.go, README.md
       covers:   c-2
       description: A read-only verb that prints one id from mintDeferredID(root), unique across every phase spec
                    and the _project store, and writes nothing. Registered in Deferred()'s AddCommand. The README
                    `dross deferred {…}` row gains `new-id`. (Risk puts the verb in a new deferred_newid.go and
                    verification in deferred_add.go next to mintDeferredID. The merged plan takes the new file so
                    deferred_add.go is untouched.)
       contract: - stdout is exactly one line matching ^[0-9a-f]{16}$. A label or prefix fails it [all]
                 - a fixture .dross tree with no deferred.toml: after the call the file is still absent and every
                   spec.toml is byte-identical. A version that files an item or backfills ids fails [risk R11+verification]
                 - with "aaaaaaaaaaaaaaaa" seeded as the only used id (in a phase spec AND the _project store), the
                   printed id differs from it, and two consecutive calls print different ids [mvp+verification]
                 - a malformed spec.toml in another phase → non-zero exit with that spec's path in the error [risk R12]
                 - root.Find([]string{"deferred","new-id"}) resolves [verification]
                 - the README deferred row without `new-id` fails the readme needle test (pattern of
                   deferred_add_test.go's TestReadme…) [risk+verification]
       depends_on: —

Wave 2 (depends t-1)
  t-3  Report unresolved edges in `dross validate`              [verification+risk+mvp]
       files:    internal/cmd/validate.go, internal/cmd/validate_edges_test.go
       covers:   c-3, c-5
       description: In the per-phase walk (validate.go ~L135, beside danglingTargets/absorbedProblems), append
                    EdgeProblems(spec) prefixed with the spec path for every spec that decoded, complete phases
                    included. A spec that failed to decode is already reported and gets no edge lines.
       contract: - one fixture per c-3 shape (dismissed no reason, covered → missing criterion, deferred id with no
                   [[deferred]] match, unknown class) → validate errors, and stdout carries "✗ <specpath>: edges[N] (<class>) …"
                   naming that record. One subtest each [verification+risk]
                 - a spec with two bad records prints two ✗ lines, so there is no early return after the first [verification+risk R13]
                 - a spec that fails to decode reports exactly one problem line for that file, with no edge lines [risk R13]
                 - c-5: a fixture copied from a real pre-phase spec (criteria + decisions + deferred, no [[edges]])
                   plus a plan → "✓ all dross artefacts valid", exit 0 [all]
                 - a deferred edge whose id matches a [[deferred]] id in the SAME spec passes; the same id present
                   only in another phase's spec fails (pins same-spec matching) [verification+risk R7]
       depends_on: t-1

  t-4  Refuse plan-lock on unresolved edges                     [verification+risk+mvp]
       files:    internal/cmd/state.go, internal/cmd/state_edges_gate_test.go
       covers:   c-7, c-5
       description: In stateSet, before the switch mutates anything: when field=current_phase_status and
                    value=planned, load the current phase's spec.toml and refuse with every EdgeProblems line.
                    Empty current_phase or no spec.toml → allow. A spec that fails to decode → refuse, naming
                    the spec path.
       contract: - current phase spec has covered → "c-9" (missing) → `state set current_phase_status planned`
                   errors with "edges[0]" and the class, and state.json is byte-identical afterwards (status
                   unchanged, no history row from s.Touch) [verification+risk R14]
                 - the refusal text equals validate's line for the same fixture minus the ✗/path prefix, so a
                   second, divergent validator fails this [verification+risk R16]
                 - all-resolved spec → status reads "planned"; legacy spec (no [[edges]]) → "planned" [verification+mvp]
                 - same bad spec: `set current_phase_status in-progress` and `set current_phase other` both
                   succeed (the gate is scoped to the plan-lock value) [risk R15+verification+mvp]
                 - current_phase empty → `planned` succeeds; current phase with no spec.toml → succeeds [risk R15]
                 - malformed spec.toml → `planned` refused with the spec path in the error [risk R15]
                 - existing TestStateSetSupportedFields stays green [verification]
       depends_on: t-1

  t-5  Add `dross phase edges <id>` resolution report           [verification+risk]
       files:    internal/cmd/phase.go, internal/cmd/phase_edges.go, internal/cmd/phase_edges_test.go, README.md
       covers:   c-6, c-1
       description: Reports EdgeStatus for the phase's spec. Default output (risk): all eight classes in
                    EdgeClasses order, each with its hint and `open` / `<outcome> (resolved)` / `unresolved: <problem>`,
                    with unknown-class records listed after. `--json` (verification): {resolved, unresolved, open}
                    class arrays, which the prompt consumes. Registered in Phase()'s AddCommand. The README phase
                    row gains `edges`. See D7 on output shape.
       contract: - boundary covered + empty dismissed(reason) + encoding dismissed("") → --json resolved=["boundary","empty"],
                   unresolved=["encoding"], open = exactly the other five; "boundary"/"empty" never appear in open
                   (the c-6 resume guarantee) [verification+risk R17]
                 - in plain output, the dismissed-without-reason class prints `unresolved:` and never `resolved` [risk R17]
                 - a record with class "latency" shows as unknown in the listing and does not vanish [risk]
                 - legacy spec or no spec.toml → open == EdgeClasses (all eight), and resolved/unresolved are empty arrays, not null [verification+risk]
                 - unknown phase id → non-zero exit naming the id [verification+risk]
                 - plain-output line order follows phase.EdgeClasses, so reordering the slice fails the golden order check [risk R18]
                 - root.Find([]string{"phase","edges"}) resolves; the README phase row without `edges` fails the readme needle test [verification+risk]
       depends_on: t-1

Wave 3 (depends t-1, t-2, t-5)
  t-6  Add edge-probe step to /dross-spec prompt                [verification skeleton, mvp placement, risk decode contract]
       files:    assets/prompts/spec.md, assets/prompts/_spec.resume.md, internal/cmd/spec_prompt_test.go
       covers:   c-1, c-2, c-4, c-6
       description: A new "## 2b. Edge probe" section in base spec.md, after §2 and before §3 (see D4). It runs
                    `dross phase edges <id> --json` and proposes the 2–3 likeliest open classes, one
                    AskUserQuestion per class, never all eight, with no minimum. Each class is recorded as
                    covered (criteria ids), dismissed (reason) or deferred (mint the id with
                    `dross deferred new-id`, and the same id goes on the [[deferred]] entry). A must-NOT
                    ("never X when Y") candidate goes through §2's accept / reword / drop gate. Accepted → covered
                    naming the new criterion id. Dropped → dismissed or deferred, never covered. §5's schema
                    gains an [[edges]] example and `id` on [[deferred]]. In _spec.resume.md, propose only classes
                    not shown resolved, and replace resets [[edges]] (see D3). Base spec.md stays ≤ 15,000 B.
       contract: - section-scoped (text between the §2b heading and the next "\n## "): contains "2–3", "one per turn"
                   and "never walk all eight". Deleting the cap sentence fails [verification+mvp]
                 - every name in phase.EdgeClasses appears in the §2b section, so a ninth class added in Go without
                   the prompt fails [verification+mvp]
                 - c-4: the §2b section ITSELF contains "never x when y" and "accept / reword / drop"; §1/§4's existing
                   copies cannot satisfy it. It also says a dropped must-NOT is recorded dismissed or deferred,
                   never covered [verification+mvp scoping, risk R21]
                 - §5's fenced toml block, toml.Decode'd into phase.Spec, has MetaData.Undecoded()==0, ≥1 Edge, and
                   zero EdgeProblems. Renaming `criteria`→`criterion` in the prompt fails it [risk R20+mvp]
                 - §2b names all three outcomes, "dross deferred new-id", and the same-id-on-[[deferred]] instruction [all]
                 - zero_edges: §2b says no minimum and that a non-applicable proposal is recorded dismissed with a reason [verification]
                 - c-6: expanded _spec.resume.md contains "dross phase edges" and an instruction to skip any class
                   already resolved [verification+risk]
                 - every `dross deferred new-id` / `dross phase edges` span in the expanded prompt resolves via
                   root.Find (TestProtectedPromptCommandsResolve pattern), so a renamed verb fails [verification]
                 - existing TestPromptByteBudget (assets/budget_test.go) keeps prompts/spec.md ≤ 15,000 B;
                   TestSpecPromptBranchesFetchTheirPartial's common-path row still fetches nothing;
                   TestPromptInstructionsConserved (assets/conservation_test.go) rows for spec.md are all still
                   present; TestSpecPromptSkipRuleStaysInBaseSection4 stays green [risk+verification, names corrected]
       depends_on: t-1, t-2, t-5
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-6 (cap, one per turn), t-5 (open-class source), t-1 (EdgeClasses exact set) |
| c-2 | t-1 (schema + round-trip + EnsureIDs survival), t-2 (deferred id minting), t-6 (prompt records each outcome; §5 decodes) |
| c-3 | t-1 (rules), t-3 (validate surfaces and names the record) |
| c-4 | t-6 |
| c-5 | t-1 (byte-clean legacy save + real-corpus nil), t-3 (legacy validates), t-4 (legacy still locks) |
| c-6 | t-5 (resolved/open partition), t-6 (resume partial uses it) |
| c-7 | t-4 |

7/7 covered.

## Disagreements

**D1 — Schema and rules: one task or two.**
- risk: t-1 is schema only (struct, classes, round-trip) and t-2 is rules (`edges_check.go`) in wave 2.
- mvp and verification: one task.
- Provisional default: one task (t-1).
- Why it matters: both halves sit in one package and the rules cannot be tested without the struct. The split adds a whole wave (4 instead of 3) and gives nothing to run in parallel. Risk's reason for splitting was to give R1/R2 (byte-clean save, edge survival) a dedicated owner. The merged t-1 keeps every R1/R2 contract, so that ownership is kept without the extra wave.

**D2 — Whether `dross phase edges` exists at all.**
- risk and verification: add the verb, so c-6 is a Go partition test and resolution logic is not restated in the prompt.
- mvp: rejects it. Base §2b says "skip any class already in [[edges]]", and c-6 is proven by a prompt needle.
- Provisional default: include it (t-5).
- Why it matters: without the verb, the c-6 test only shows that a sentence exists, not that resolved classes are excluded. A dismissed-without-reason record also looks resolved to an agent skimming the TOML. The cost is a new CLI verb plus a README row, and t-6 picks up a third dependency.

**D3 — Where the c-6 resume skip rule lives.**
- mvp: base §2b, mirroring §4's "already has a target" rule. The precedent is `TestSpecPromptSkipRuleStaysInBaseSection4`, which deliberately pins that rule to base and keeps it out of the resume partial.
- risk and verification: `_spec.resume.md`, since a resolved edge only exists when `spec_file=present`, which is exactly the resume partial's trigger.
- Provisional default: `_spec.resume.md`. The base step also asks only for `open` classes from `dross phase edges`, so base already cannot re-propose a resolved class.
- Why it matters: the §4 precedent argues against the default. If the rule ends up only in the partial, a run that reaches an existing spec some other way would rely on the CLI output alone. Confirm at plan-lock whether the §4 precedent should bind here.

**D4 — Position of the edge step in spec.md.**
- mvp and risk: §2b, right after the criteria walk and before gray areas, so a must-NOT goes through §2's gate while it is still open and covered edges can name criteria just accepted.
- verification: between §3 and §4, after decisions are locked.
- Provisional default: §2b.
- Why it matters: placing it after §3 means a must-NOT criterion is added after the criteria slate closes, which re-opens §2. Placing it before §3 means edges are judged before the locked decisions are known, and a decision could make a class moot. The test anchors (section-scoped needles) depend on the heading chosen.

**D5 — Extra validator rules beyond c-3's four shapes.**
- risk: also flags duplicate class records (R9, "the on-disk trace of a c-6 failure") and records carrying another outcome's field (R8, dismissed + criteria).
- mvp: explicitly rejects both as speculative, since no criterion asks for them.
- verification: silent.
- Provisional default: keep the duplicate-class rule and drop the foreign-field rule.
- Why: a duplicate is mechanically tied to c-6. Risk itself flagged R8 as one "the judge may relax", and a dismissed+criteria record is still resolved by its reason.
- Why it matters: every extra rule is another way a hand-edited spec can fail validate and block plan-lock (t-4). Too strict blocks real work; too lax lets ambiguous records through.

**D6 — Whether `deferred new-id` is its own task.**
- risk and verification: a separate wave-1 task.
- mvp: folds it into the prompt task (its only consumer), to avoid a near-trivial standalone task.
- Provisional default: separate (t-2).
- Why it matters: folding it in makes the prompt task span two layers (Go verb + prompt prose) and moves the verb into the last wave. Keeping it separate lets it run in parallel with t-1 and lets t-6's `root.Find` contract depend on a verb that already exists. It is 4 files and above the merge threshold, so the granularity rule does not force a merge.

**D7 — Output shape of `dross phase edges`.**
- risk: a human listing of all eight classes with per-class hints and status, which moves the class hints out of spec.md and saves prompt bytes.
- verification: a `--json` {resolved, unresolved, open} partition the prompt consumes and the test asserts.
- Provisional default: both. Plain output carries risk's hints; `--json` carries verification's partition and is what t-6 calls.
- Why it matters: if only `--json` ships, spec.md has to carry the eight class hints itself, inside 1,555 B of headroom. If only the plain listing ships, the c-6 test asserts on human-formatted text. Keeping both adds output-format surface to test.
