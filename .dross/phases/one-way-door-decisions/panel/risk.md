# Planner draft — lens: risk

Phase one-way-door-decisions — 9 tasks across 3 waves

The failure modes this plan is built around, each owned by exactly one task:

| # | Risk | Owner |
|---|---|---|
| R1 | Hash ambiguity: `key+choice` concatenation lets `("ab","c")` collide with `("a","bc")`; a hash that isn't pinned drifts between builds and silently voids every confirmation | t-1 |
| R2 | Tag value sloppiness: `One-way`, `one_way`, `" one-way"` and `""` are accepted as one-way or reversible instead of reported | t-1 |
| R3 | Round-trip loss: a `Spec.Save` from any other verb (mark-new, deferred absorb, edges) drops `reversibility`/`confirmed`, or writes empty keys into the 151 legacy specs | t-1 |
| R4 | Legacy flood: validate reports missing tags on complete phases (c-7), or the completeness predicate disagrees with the one `absorbedProblems` uses | t-2 |
| R5 | Confirm verb misuse: unknown key, ambiguous duplicate key, reversible/unlocked/untagged decision, traversal phase id, a re-run that rewrites the file, or a write that disturbs other spec sections | t-3 |
| R6 | Secret-scanner false positive: a 64-hex `confirmed` value trips `scanDrossArtifacts`, so validate/ship/auto-commit refuse every confirmed spec | t-3 |
| R7 | Marker spoofing / legacy drift in the ship body: a reversible choice whose prose contains "one-way" reads as one-way; the pinned legacy `🔒` line changes | t-4 |
| R8 | Reviewer context: marker ambiguity, and a tag flip that leaves the context Digest unchanged so a stale verdict is reused | t-5 |
| R9 | Prompt budget: spec.md is 14,835 / 15,000 bytes — 165 bytes of headroom — so the tag proposal, schema line and fetch line cannot all land in base | t-6 (room) / t-8 (fetch line) |
| R10 | Agent fabricates or hand-copies `confirmed`, or drops it on a resume rewrite | t-6 |
| R11 | Rubber-stamp bypass: the gate's own refusal tells an agent to run `dross phase confirm` without asking the user | t-7 |
| R12 | Gate ordering / partial failure: the gate mutates state before refusing, passes an undecodable spec, or refuses a phase with no spec | t-7 |
| R13 | Stale confirmation (c-8) passes because the gate compares only presence, not the hash | t-7 |
| R14 | Self-inflicted red: this phase's own spec holds 4 untagged locked decisions, so `dross validate` goes red after `make install`; plus the live-tree legacy proof against the real 151 specs | t-9 |

```
Wave 1
  t-1  Add decision tag fields and shared rules
       files:    internal/phase/phase.go, internal/phase/decision.go (new),
                 internal/phase/decision_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     Decision gains `Reversibility` (toml "reversibility,omitempty") and
                 `Confirmed` (toml "confirmed,omitempty"); consts DoorReversible="reversible",
                 DoorOneWay="one-way". New decision.go: DecisionHash(key, choice) =
                 "sha256:"+hex(sha256(key+"\x00"+choice)) over the decoded values;
                 ReversibilityProblems(spec, requireTags bool) []string (missing on locked
                 only when requireTags, unknown on any decision always);
                 UnconfirmedOneWay(spec) []Unconfirmed{Key, Stale bool} (locked && one-way &&
                 Confirmed != hash); DoorLabel(d) → "one-way" | "one-way, unconfirmed" |
                 "reversible" | "" (untagged legacy). Two not_paths rows.
       covers:   c-1, c-8, c-9
       contract: - DecisionHash("ab","c") != DecisionHash("a","bc"), and DecisionHash("k","x")
                   equals a pinned literal — a separator-less or re-salted hash fails both
                 - changing one byte of choice, or the key, changes the hash; the phase id is
                   not an input (a phase rename keeps the confirmation)
                 - ReversibilityProblems on a locked decision with reversibility "" names the
                   key as missing; "One-way", "one_way", " one-way" each report unknown and
                   quote the value; neither "reversible" nor "one-way" reports anything
                 - requireTags=false suppresses missing but still reports an unknown value;
                   an unlocked untagged decision is never reported; an unlocked decision with
                   an unknown value is reported
                 - UnconfirmedOneWay: matching hash → empty; empty confirmed → listed, Stale
                   false; hash of the previous choice → listed, Stale true; reversible or
                   unlocked one-way → not listed; nil Decisions → empty slice
                 - a Spec with reversibility+confirmed saves and reloads equal; a legacy Spec
                   saves with no `reversibility` or `confirmed` key in the bytes
                 - TestEveryPathShapedFieldIsDeclared fails if either new field is unfiled
       depends:  —

Wave 2 (depends t-1)
  t-2  Report reversibility problems in dross validate
       files:    internal/cmd/validate.go, internal/cmd/validate_reversibility_test.go (new)
       desc:     In the per-phase walk, append phase.ReversibilityProblems(spec,
                 !changes.Complete(root, id)) prefixed with the spec path — the same
                 completeness predicate absorbedProblems uses. Unconfirmed one-way is NOT a
                 validate problem (it is the gate's job; §6 validate runs mid-spec-flow).
       covers:   c-1, c-7
       contract: - fixture phase with no completion record and an untagged locked decision →
                   validate exits non-zero with a ✗ line naming spec.toml and the key
                 - same spec under a changes.json with status complete → "✓ all dross
                   artefacts valid" (c-7); an unknown value under that complete record is
                   still a ✗ line
                 - a phase with no changes.json is treated as open (missing tag reported) —
                   pins the boundary so a predicate swap is caught
                 - a tagged, unconfirmed one-way locked decision yields no ✗ line
                 - one untagged decision yields exactly one line (no double report)
       depends:  t-1

  t-3  Add dross phase confirm verb
       files:    internal/cmd/phase_confirm.go (new), internal/cmd/phase.go,
                 internal/cmd/phase_confirm_test.go (new), cmd/dross/testdata/cli_tree.txt,
                 README.md
       desc:     `dross phase confirm <phase-id> <key>` (ExactArgs(2)): ContainID the phase,
                 load the spec, find the single locked one-way decision named key, write
                 confirmed = phase.DecisionHash(key, choice), Spec.Save. All checks before
                 the write; any refusal leaves the file untouched. Register in Phase();
                 cli_tree golden row; README phase-row mention.
       covers:   c-3, c-8
       contract: - on a locked one-way decision, reload shows confirmed ==
                   phase.DecisionHash(key, choice) and criteria/edges/deferred/new reload equal
                 - unknown key, reversible decision, untagged decision, unlocked decision and
                   a key matching two decisions are each refused with the reason named and
                   spec.toml bytes unchanged
                 - phase id "../x" is refused by ContainID; unknown phase and missing spec
                   are refused
                 - re-running on a current confirmation prints "already confirmed" and leaves
                   spec.toml bytes unchanged (idempotent)
                 - after the choice is edited, confirm overwrites the stale hash with the new one
                 - a choice holding backticks, quotes and a multi-line string: the hash
                   written still matches after Save → LoadSpec (survives re-encoding)
                 - scanDrossArtifacts over a tree holding a confirmed spec returns zero hits
                 - TestCLITreeGolden fails until cli_tree.txt carries `dross phase confirm`;
                   TestReadmeAdvertisesOnlyRealCommands keeps the README phase row honest
       depends:  t-1

  t-4  Mark one-way decisions in ship PR body
       files:    internal/ship/body.go, internal/ship/body_test.go
       desc:     "Decisions locked" renders phase.DoorLabel(d) in a fixed marker slot after
                 the key/lock (e.g. `- **key** 🔒 **[one-way]** — choice`); reversible gets
                 `[reversible]`; untagged legacy renders exactly as today.
       covers:   c-5
       contract: - a one-way and a reversible decision render different marker tokens in
                   the slot before " — "
                 - a reversible decision whose choice text contains "one-way" has no
                   [one-way] token in the marker slot
                 - a one-way decision whose confirmed hash is stale renders
                   "[one-way, unconfirmed]"
                 - the existing pinned legacy line "- **storage** 🔒 — drizzle JSON column
                   *(avoids join complexity)*" is byte-identical
       depends:  t-1

  t-5  Mark one-way decisions in reviewer context
       files:    internal/review/context.go, internal/review/context_test.go
       desc:     renderDecisions prefixes each locked decision with phase.DoorLabel(d) in
                 brackets (`- [one-way] key: choice`); untagged renders as today; unlocked
                 stay omitted.
       covers:   c-6
       contract: - one-way and reversible locked decisions render distinct bracket tokens;
                   untagged renders "- key: choice" with no bracket
                 - an unlocked one-way decision still does not appear
                 - flipping one decision's tag from reversible to one-way changes Context.Digest
       depends:  t-1

  t-6  Propose reversibility in /dross-spec §3c and §5
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       desc:     §3c: every decision gets a proposed tag, default one-way for file/schema
                 formats, public CLI surface and new dependencies, reversible otherwise;
                 user may override. §5 schema shows `reversibility = "one-way"` and a comment
                 that `confirmed` is written only by `dross phase confirm` and kept verbatim
                 on resume. Tighten existing base prose to free ≥ 300 bytes for this and
                 t-8's fetch line, without dropping a conservation-manifest row.
       covers:   c-2
       contract: - TestPromptByteBudget/spec.md fails if spec.md exceeds 15,000 bytes
                 - a spec prompt test fails unless §3c names all three one-way defaults
                   (file/schema format, public CLI surface, new dependency) and an override
                 - TestSpecSchemaExampleDecodes fails if the §5 example carries a key
                   phase.Spec does not read, or if ReversibilityProblems over it is non-empty
                 - a test fails unless §5 states `confirmed` is never hand-written
                 - TestPromptInstructionsConserved fails if the trim drops a manifest row
       precondition: run `wc -c < assets/prompts/spec.md`, max 14835 (baseline: no one else
                 has spent the headroom first)
       depends:  t-1

Wave 3 (depends t-1, t-2, t-3, t-6)
  t-7  Add one-way gate to plan-lock
       files:    internal/cmd/state.go, internal/cmd/state_oneway_gate_test.go (new)
       desc:     planLockOneWayGate(root, phaseID), run after the edge and facts gates and
                 before s.CurrentPhaseStatus is written: no phase / no spec.toml → pass;
                 undecodable → refuse naming path; ReversibilityProblems(spec, true) or any
                 UnconfirmedOneWay → refuse listing every key (stale ones marked "choice
                 changed since confirmation"), telling the user to re-confirm via /dross-spec
                 and `dross phase confirm <phase> <key>` after an explicit yes.
       covers:   c-4, c-8, c-9
       contract: - two unconfirmed one-way keys → `state set current_phase_status planned`
                   errors naming both keys, and state.json's status is unchanged
                 - confirm via the real verb, edit the choice in spec.toml → lock refuses
                   naming the key as changed; confirm again → lock succeeds
                 - no [[decisions]], only reversible, and an unconfirmed unlocked one-way
                   each lock successfully (c-9)
                 - an untagged locked decision on the phase being locked refuses naming it
                 - no current phase or no spec.toml → passes; undecodable spec → refuses
                   naming the path
                 - the refusal text names "/dross-spec" and the exact `dross phase confirm`
                   form; TestNarratedCommandsResolveAgainstTheTree fails if the verb is
                   not registered
                 - setting any status other than "planned" never runs the gate
       precondition: run `grep -q 'phaseConfirm()' internal/cmd/phase.go` (the verb the
                 refusal narrates is registered, else the narrated-command guard goes red)
       depends:  t-1, t-3

  t-8  Add one-way confirm turn partial
       files:    assets/prompts/spec.md, assets/prompts/_spec.one-way.md (new),
                 assets/partials.go, internal/cmd/spec_prompt_test.go,
                 docs/interaction-audit.md
       desc:     New trigger key `door: {reversible, one-way}` in TriggerVocabulary. §3c gets
                 the When line `door=one-way` → `dross prompt partial spec one-way`. Partial:
                 right after an area resolves one-way, one AskUserQuestion — confirm / make
                 reversible / reopen (reopen returns to §3b); after §5's write, run
                 `dross phase confirm <phase-id> <key>` once per yes, never for a no and
                 never without the answer. interaction-audit row for the new turn.
       covers:   c-3
       contract: - assets.Fetched(spec, door=one-way) == ["one-way"]; the common path still
                   fetches nothing (TestSpecPromptBranchesFetchTheirPartial row)
                 - CheckPartials fails on an orphan _spec.one-way.md or an unknown trigger key
                 - a test fails unless the expanded partial offers exactly confirm / make
                   reversible / reopen as an AskUserQuestion and runs the verb only after §5
                 - every `dross phase confirm` in the expanded prompt resolves in Phase()
                 - TestPromptByteBudget/spec.md still passes with the When line added
       precondition: run `wc -c < assets/prompts/spec.md`, max 14850 (t-6 left room for the
                 ~140-byte fetch line)
       depends:  t-3, t-6

  t-9  Tag this phase's own locked decisions
       files:    .dross/phases/one-way-door-decisions/spec.toml
       desc:     Add reversibility to the four locked decisions (confirmation_record is a
                 spec.toml format → one-way by default); ask the user per one-way decision
                 (AskUserQuestion), then `dross phase confirm one-way-door-decisions <key>`
                 per yes. Then run installed `dross validate` over the live tree.
       covers:   c-1, c-7
       contract: - installed `dross validate` prints no reversibility ✗ line for this
                   phase's spec — an untagged decision left behind shows up by name
                 - the same run prints no reversibility ✗ line for any of the ~151 complete
                   phase specs — a legacy-boundary regression on real data shows up there
                   even if the fixtures in t-2 passed
       precondition: run `dross phase confirm --help` (r-01: the installed binary carries
                 the verb; a stale binary would also strip the new fields on any rewrite)
       depends:  t-2, t-3
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 tag on every decision; validate reports missing/unknown on locked | t-1 (rule), t-2 (validate), t-9 (own spec) |
| c-2 §3 proposes tag with one-way defaults, user override | t-6 |
| c-3 one-way gets its own confirm turn; spec records it | t-3 (record), t-8 (turn) |
| c-4 plan-lock refuses unconfirmed one-way, naming each key | t-7 |
| c-5 ship PR body marks one-way distinctly | t-4 |
| c-6 task-reviewer context marks one-way distinctly | t-5 |
| c-7 no reversibility problem on complete legacy phases | t-2 (fixture), t-9 (live tree) |
| c-8 changed choice voids confirmation | t-1 (hash), t-3 (re-confirm), t-7 (gate end-to-end) |
| c-9 gate passes with no / only reversible / only unlocked decisions | t-1 (rule), t-7 (gate) |

All 9 criteria covered. Spec edges: adjacency c-7 → t-2 + t-9; idempotency c-8/c-4 → t-3 (confirm re-run is a no-op) + t-7 (lock after re-confirm); empty c-9 → t-1 + t-7.

## Judgment calls

- **Hash input = decoded key + NUL + decoded choice, exact bytes, no trimming.** Rejected: whitespace-normalising the choice. A cosmetic re-indent voiding a confirmation fails closed (one re-confirm); normalising would let a real edit through.
- **Rules live in internal/phase (like EdgeProblems), shared by validate, the gate and both renderers.** Rejected: each caller re-deriving "is it one-way and confirmed". Four readers of one fact must not disagree.
- **Completeness = the existing `changes.Complete` predicate, and a phase with no changes.json counts as open.** Rejected: treating a missing record as legacy-complete. That needs a second definition of complete. Downstream repos with record-less legacy phases will see missing-tag lines. t-2 pins the boundary so the choice is visible, not accidental.
- **Unknown tag value is reported on unlocked decisions too.** Rejected: scoping "unknown" to locked only. legacy_boundary says unknown is reported everywhere, and a typo is wrong wherever it sits.
- **validate does not report unconfirmed one-way; only the gate does.** Rejected: validate flagging it. §6 runs `dross validate` right after the §5 write, and an agent shown a red ✗ will "fix" it by running the confirm verb.
- **Plan-lock also refuses a missing tag on the phase being locked.** Rejected: gating only unconfirmed one-way per c-4's literal text. legacy_boundary's "plan-lock checks the phase being locked" puts the missing-tag rule there. Without it, an untagged decision would skip the door entirely.
- **The gate's refusal routes the user back to /dross-spec and says the verb runs only after an explicit yes.** Rejected: a bare "run `dross phase confirm`" hint. That is the rubber-stamp path. The verb cannot detect a fabricated yes, so the message is the only guard. plan.md is left alone: it is 14,904 / 15,000 bytes.
- **Duplicate decision keys: the confirm verb refuses them as ambiguous; validate does not gain a duplicate-key check.** Rejected: a repo-wide duplicate rule. None of the 153 specs has a duplicate today, and that rule is a separate surface.
- **The confirm turn goes in a fetched partial (`door=one-way`), while the tag proposal stays in base §3c.** Rejected: all-inline, which blows the 15,000-byte cap with 165 bytes of headroom. Also rejected: moving the tag proposal into the partial too, because c-2 applies to every decision and would vanish on the common path.
- **"Keep `confirmed` verbatim on resume" goes in §5's schema comment, not in _spec.resume.md.** Rejected: editing the resume partial. The §5 schema is read on both fresh and resume writes, so one line covers both paths, and the confirm-turn task stays at 5 files.
- **t-7 waits on t-3 rather than running beside it.** Rejected: same wave plus a temporary narratedIgnore entry. The gate narrates the verb, and an ignore entry added only to be deleted is churn the guard's stale-entry check would flag.
- **Own-spec tagging (t-9) is a planned task, not a verify-time fix-up.** Rejected: leaving it for verify. After `make install`, this phase's own 4 untagged locked decisions make `dross validate` red. It is also the only check against the real 151 legacy specs.
- **Ship and review show a stale one-way as "one-way, unconfirmed".** Rejected: rendering plain one-way. A choice edited after plan-lock is exactly the drift the PR reader and the reviewer should see.
- **Not mitigated in code: a stale installed binary silently drops the new fields on rewrite, because LoadSpec ignores unknown keys.** Rejected: strict decoding in LoadSpec. That is a cross-cutting change outside this phase. r-01 (`make install`) covers it, and t-9's precondition enforces it once.
