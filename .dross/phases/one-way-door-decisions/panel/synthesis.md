# Panel synthesis — one-way-door-decisions

Repo facts checked before judging (they decide several grafts):

- `assets/prompts/spec.md` is 14,835 / 15,000 bytes; `plan.md` is 14,904. All three drafts quote these correctly.
- `TestCommonPathVocabularyPinned` (assets/partials_test.go:215) pins every `TriggerVocabulary` key and checks `len(got) != len(want)`. A new `door` key fails it unless `assets/partials_test.go` is edited. **Risk's t-8 leaves that file out**; mvp and verification list it.
- `TestSpecSchemaExampleDecodes` already rejects undecoded keys (`md.Undecoded()`). It does not call any reversibility rule yet, so the "§5 example carries a valid tag" contract is new assertion work in the prompt task.
- `changes.Complete` treats a missing or unreadable record as not complete. Under that predicate, `one-way-door-decisions` is the only open phase with `locked = true` decisions (150 specs hold locked decisions). So after `make install`, `dross validate` (and anything that calls it) goes red on exactly this phase's own spec, and on nothing else.
- No Go test runs validate over the live `.dross` tree, so leaving the own spec untagged would not show up in `go test`. It would show up in the installed `dross validate` and in ship's pre-flight.
- The secretscan `key-context` rule keys on password/secret/token/api_key/authorization and so does not match `confirmed = "…"`. Risk's R6 (scanner false positive) is therefore unlikely, but the contract is a cheap regression pin, so it is kept.
- `review.Context.Digest` is sha256 over Body, so risk's "tag flip changes Digest" contract holds once the marker is rendered into Body.
- `TestReadmeAdvertisesOnlyRealCommands` checks only that commands the README advertises exist. Nothing forces a new verb into the README.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk (9 tasks) | 9/9, plus edges mapped. It is the only draft that closes the live-tree side of c-7 (t-9 runs validate against the real 150 specs). | Highest. Pinned hash literal, separator collision, the casing variants of unknown tags (`One-way`, `one_way`, ` one-way`), bytes unchanged on every refusal, duplicate-key refusal, a choice with backticks and newlines surviving the round-trip, the legacy ship line byte-identical, Digest changing on a tag flip, and the gate leaving state.json unchanged. | Mixed. t-4 and t-5 are each a one-file render change plus its test, below the floor. t-9 is one file, but it is an interactive task with a live check, so it earns its slot. t-8 would be 6 files once its missing companion is added. | Correct. t-6 waits on t-1 for the schema-decode rule, t-7 on t-3 for the narrated verb, t-8 on t-3 and t-6 because they share spec.md and its budget. Missed companion: `assets/partials_test.go` in t-8. |
| mvp (5 tasks) | 9/9 by mapping. c-7 is checked on fixtures only, and nothing tags this phase's own spec, so live validate goes red after install. | Adequate but thin. No separator-collision test, no legacy-line pin, no idempotent re-run, no traversal refusal, no state.json-unchanged check after a refusal is stated (it is implied). | Too coarse in two places. t-4 merges validate and the gate (5 criteria in one task). t-5 is 5 files spanning a prompt, a partial, Go assets and two tests. | Mostly correct. Bundling validate with the gate drags validate into wave 3 even though validate needs only t-1. |
| verification (7 tasks) | 9/9. Own-spec tagging is folded into the validate task, with a `go run ./cmd/dross validate` exits-0 contract. | High. Strong on predicates: a hash copied from another key is rejected, unknown values are reported in both modes, legacy saves stay byte-clean, and the interaction-audit row is asserted in a test. Weaker than risk on the confirm verb (no duplicate-key or multi-line-choice case) and on the ship legacy pin. | Good. Ship and review are merged into one task. t-7 is 6 files, all of them guard companions. | Correct, and it lists `assets/partials_test.go`. One coupling: t-3 tags the own spec in wave 2, before the confirm verb exists, so confirmations cannot be recorded in the same task. |

**Skeleton: risk.** It has the sharpest contracts per task and the fullest set of companion files (not_paths, cli_tree, README, interaction-audit). Its failure-mode table ties every contract to a named risk. Grafts below fix its two weaknesses: the sub-floor ship/review split, taken from mvp+verification, and the missing `assets/partials_test.go`, taken from verification.

## Merged plan

```
Phase one-way-door-decisions — 8 tasks across 3 waves

Wave 1
  t-1  Add decision tag fields and shared rules                         [risk+verification]
       files:    internal/phase/phase.go,
                 internal/phase/decision.go (new),
                 internal/phase/decision_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-7, c-8, c-9
       desc:     Decision gains `Reversibility` (toml "reversibility,omitempty") and
                 `Confirmed` (toml "confirmed,omitempty"); consts DoorReversible/DoorOneWay.
                 decision.go: DecisionHash(key, choice) over key+"\x00"+choice;
                 ReversibilityProblems(spec, requireTags bool); UnconfirmedOneWay(spec)
                 → []Unconfirmed{Key, Stale}; DoorLabel(d) → "one-way" | "one-way,
                 unconfirmed" | "reversible" | "" (untagged legacy). Two not_paths rows.
                 One rule set is shared by validate, the gate, the verb and both renderers.
       contract: - DecisionHash("ab","c") != DecisionHash("a","bc"), and DecisionHash("k","x")
                   equals a pinned literal — a separator-less or re-salted hash fails both
                 - changing one byte of choice, or the key, changes the hash; the phase id is
                   not an input (a phase rename keeps the confirmation)
                 - ReversibilityProblems(spec, true) on a locked decision with reversibility ""
                   returns one line naming `decisions[0] (<key>)` as missing; "One-way",
                   "one_way", " one-way", "oneway" each report unknown, quote the value and list
                   `reversible, one-way`; neither valid value reports anything
                 - requireTags=false suppresses missing but still reports an unknown value;
                   an unlocked untagged decision is never reported in either mode; an unlocked
                   decision with an unknown value is reported
                 - UnconfirmedOneWay: matching hash → empty; empty confirmed → listed, Stale
                   false; hash of the previous choice → listed, Stale true; a Confirmed hash
                   copied from another key with the same choice → listed; reversible,
                   untagged or unlocked one-way → not listed; nil Decisions → empty
                 - a Spec with reversibility+confirmed saves and reloads equal; a legacy Spec
                   (no reversibility/confirmed keys) saves with neither key in the bytes
                 - TestEveryPathShapedFieldIsDeclared fails until
                   `phase.Decision.Reversibility reversibility` and
                   `phase.Decision.Confirmed confirmed` are filed in not_paths.txt
       depends:  —

Wave 2 (each depends t-1 only)
  t-2  Report reversibility problems in dross validate                  [risk+verification]
       files:    internal/cmd/validate.go,
                 internal/cmd/validate_reversibility_test.go (new)
       covers:   c-1, c-7
       desc:     In the per-phase walk, append phase.ReversibilityProblems(spec,
                 !changes.Complete(root, id)), each line prefixed with the spec path. This is
                 the same completeness predicate absorbedProblems uses. An unconfirmed one-way
                 decision is NOT a validate problem: that is the gate's job, and §6 runs
                 validate mid-spec-flow.
       contract: - fixture phase with no completion record and an untagged locked decision →
                   validate exits non-zero with `✗ <specPath>: decisions[0] (<key>): missing
                   reversibility`
                 - same spec under a changes.json with status complete → "✓ all dross
                   artefacts valid" (c-7)
                 - `reversibility = "oneway"` under a complete record is still a ✗ line
                 - a phase with no changes.json counts as open (missing tag reported); this
                   pins the boundary so a predicate swap is caught
                 - a tagged, unconfirmed one-way locked decision yields no ✗ line
                 - one untagged decision yields exactly one line (no double report)
       depends:  t-1

  t-3  Add dross phase confirm verb                                     [risk+verification]
       files:    internal/cmd/phase_confirm.go (new), internal/cmd/phase.go,
                 internal/cmd/phase_confirm_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-3, c-8
       desc:     `dross phase confirm <phase-id> <key>` (ExactArgs(2), shaped like
                 phase_mark_new.go): ContainID the phase, load the spec, find the single
                 locked one-way decision named key, write confirmed = phase.DecisionHash(key,
                 choice), Spec.Save. Every check runs before the write, so any refusal leaves
                 the file untouched. Register it in Phase(), add the cli_tree golden row, and
                 add confirm to the README `dross phase {…}` row.
       contract: - on a locked one-way decision, reload shows confirmed ==
                   phase.DecisionHash(key, choice), the reloaded spec's UnconfirmedOneWay is
                   empty, and criteria/edges/deferred/new reload equal
                 - an unknown key is refused naming it and listing the spec's decision keys;
                   a reversible, untagged or unlocked decision is refused ("not a locked
                   one-way decision"); a key matching two decisions is refused as ambiguous;
                   spec.toml bytes are unchanged in every case
                 - phase id "../x" is refused by ContainID before any read; an unknown phase
                   and a missing spec are refused
                 - re-running on a current confirmation prints "already confirmed" and leaves
                   spec.toml byte-identical (idempotent)
                 - after the choice is hand-edited, confirm overwrites the stale hash with
                   the new one (the re-confirm path for c-8)
                 - a choice holding backticks, quotes and a multi-line string: the hash
                   written still matches after Save → LoadSpec
                 - scanDrossArtifacts over a tree holding a confirmed spec returns zero hits
                 - TestCLITreeGolden fails until cli_tree.txt carries `dross phase confirm`;
                   TestReadmeAdvertisesOnlyRealCommands keeps the README phase row honest
       depends:  t-1

  t-4  Mark one-way decisions in PR body and reviewer context           [mvp+verification structure, risk contracts]
       files:    internal/ship/body.go, internal/ship/body_test.go,
                 internal/review/context.go, internal/review/context_test.go
       covers:   c-5, c-6
       desc:     Both renderers use phase.DoorLabel(d) in a fixed marker slot. Ship
                 "## Decisions locked": `- **key** 🔒 **[one-way]** — choice` /
                 `**[reversible]**`. Reviewer renderDecisions: `- [one-way] key: choice` /
                 `- [reversible] key: choice`. Untagged legacy decisions render exactly as
                 today in both, and unlocked decisions stay omitted from the reviewer context.
       contract: - ship body: a one-way and a reversible decision render different marker
                   tokens in the slot before " — "; the reversible line does not carry the
                   one-way token
                 - a reversible decision whose choice text contains "one-way" has no
                   [one-way] token in the marker slot
                 - a one-way decision whose confirmed hash is stale renders
                   "[one-way, unconfirmed]" (ship and reviewer)
                 - the pinned legacy line "- **storage** 🔒 — drizzle JSON column
                   *(avoids join complexity)*" is byte-identical
                 - reviewer context: one-way and reversible locked decisions render distinct
                   bracket tokens; untagged renders "- key: choice" with no bracket; an
                   unlocked one-way decision still does not appear
                 - flipping one decision's tag from reversible to one-way changes
                   Context.Digest
       depends:  t-1

  t-5  Propose reversibility in /dross-spec §3c and §5                  [risk+verification]
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-2
       desc:     §3c: every locked decision gets a proposed tag. The default is one-way for
                 file/schema formats, public CLI surface and new dependencies, reversible
                 otherwise, and the user may override. The §5 schema example shows
                 `reversibility = "one-way"`, plus a prose note that `confirmed` is written
                 only by `dross phase confirm` and kept verbatim on resume, never as a
                 `confirmed =` key line. Tighten existing base prose to free ≥ 300 bytes for
                 this and t-7's fetch line, without dropping a conservation-manifest row.
       contract: - TestPromptByteBudget/spec.md fails if spec.md exceeds 15,000 bytes
                 - a spec prompt test fails unless the expanded §3c names all three one-way
                   defaults (file/schema format, public CLI surface, new dependency), the
                   reversible fallback, and the user override
                 - TestSpecSchemaExampleDecodes fails if the §5 example carries a key
                   phase.Spec does not read, or if ReversibilityProblems(spec, true) over it
                   is non-empty
                 - a test fails unless §5 states `confirmed` is never hand-written, and the
                   §5 example carries no `confirmed =` line
                 - TestPromptInstructionsConserved fails if the trim drops a manifest row
       precondition: run `wc -c < assets/prompts/spec.md`, max 14835 (baseline: nobody has
                 spent the headroom first)
       depends:  t-1

Wave 3
  t-6  Add one-way gate to plan-lock                                    [risk+verification]
       files:    internal/cmd/state.go,
                 internal/cmd/state_oneway_gate_test.go (new)
       covers:   c-4, c-8, c-9
       desc:     planLockOneWayGate(root, phaseID) runs after planLockEdgeGate and
                 planLockFactsGate, and before s.CurrentPhaseStatus is written. No phase or no
                 spec.toml → pass. Undecodable spec → refuse, naming the path.
                 ReversibilityProblems(spec, true) or any UnconfirmedOneWay → refuse, listing
                 every key (stale ones marked "choice changed since confirmation") and routing
                 the user to /dross-spec, with `dross phase confirm <phase> <key>` run only
                 after an explicit yes.
       contract: - two unconfirmed one-way keys → `state set current_phase_status planned`
                   errors naming both keys, and state.json is byte-identical afterwards
                 - after `dross phase confirm` (the real verb) on both keys, plan-lock passes
                 - confirm, then edit one decision's choice in spec.toml → lock refuses naming
                   only that key as changed; confirm again → lock succeeds (c-8)
                 - no [[decisions]], only reversible, and only unlocked one-way decisions each
                   lock successfully, one subtest each (c-9)
                 - an untagged locked decision on the phase being locked refuses, naming it
                   (legacy_boundary)
                 - no current phase or no spec.toml → passes; undecodable spec → refuses
                   naming the path
                 - the refusal text names "/dross-spec" and the exact `dross phase confirm`
                   form; TestNarratedCommandsResolveAgainstTheTree fails if the verb is not
                   registered
                 - setting any status other than "planned" never runs the gate
       precondition: run `grep -q 'phaseConfirm()' internal/cmd/phase.go` (the verb the
                 refusal narrates is registered, else the narrated-command guard goes red)
       depends:  t-1, t-3

  t-7  Add one-way confirm turn partial                                 [risk skeleton + verification files/contracts]
       files:    assets/prompts/spec.md, assets/prompts/_spec.one-way.md (new),
                 assets/partials.go, assets/partials_test.go,
                 internal/cmd/spec_prompt_test.go, docs/interaction-audit.md
       covers:   c-3
       desc:     New trigger key `door: {reversible, one-way}` in TriggerVocabulary, with
                 TestCommonPathVocabularyPinned updated. §3c gains the When line
                 `door=one-way` → `dross prompt partial spec one-way`. The partial: right after
                 an area resolves one-way, one AskUserQuestion — confirm / make reversible /
                 reopen (reopen returns to §3b). After §5's write, run `dross phase confirm
                 <phase-id> <key>` once per yes, never for a no and never without the answer.
                 Add an interaction-audit row for the new turn.
       contract: - TestSpecPromptBranchesFetchTheirPartial gains {"door","one-way"} →
                   ["one-way"]; the common path still fetches nothing
                   (TestCommonPathFetchesNothing)
                 - TestCommonPathVocabularyPinned fails unless "door" is pinned with common
                   value "reversible"
                 - CheckPartials fails on an orphan _spec.one-way.md or an unknown trigger key
                 - the expanded partial offers exactly confirm / make reversible / reopen as
                   one AskUserQuestion, and runs the verb only after §5 writes spec.toml and
                   only on a yes
                 - every `dross phase confirm` in the expanded prompt resolves in Phase(), and
                   the prompt never shows a `confirmed =` line to hand-write
                 - docs/interaction-audit.md's `### dross-spec` table has a one-way confirm
                   row, asserted in spec_prompt_test
                 - TestPromptByteBudget/spec.md still ≤ 15,000 with the fetch line added
       precondition: run `wc -c < assets/prompts/spec.md`, max 14850 (t-5 left room for the
                 ~140-byte fetch line)
       depends:  t-3, t-5

  t-8  Tag this phase's own locked decisions                            [risk; verification's live-validate contract]
       files:    .dross/phases/one-way-door-decisions/spec.toml
       covers:   c-1, c-7
       desc:     Add reversibility to the four locked decisions, with values agreed with the
                 user and never guessed (confirmation_record is a spec.toml format, so it
                 defaults to one-way). Ask the user per one-way decision (AskUserQuestion),
                 then run `dross phase confirm one-way-door-decisions <key>` per yes. Then
                 run validate over the live tree.
       contract: - installed `dross validate` prints no reversibility ✗ line for this
                   phase's spec — an untagged decision left behind shows up by name
                 - the same run prints no reversibility ✗ line for any of the ~150 complete
                   phase specs — a legacy-boundary regression on real data shows up there
                   even if t-2's fixtures passed
       precondition: run `dross phase confirm --help` (r-01: the installed binary carries
                 the verb; a stale binary would also strip the new fields on any rewrite)
                 and `grep -c 'locked = true' .dross/phases/one-way-door-decisions/spec.toml`,
                 min 4 (the backfill tags exactly the decisions present at plan time)
       depends:  t-2, t-3
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 (rule), t-2 (validate surface), t-8 (own spec) |
| c-2 | t-5 |
| c-3 | t-3 (record), t-7 (turn) |
| c-4 | t-6 |
| c-5 | t-4 |
| c-6 | t-4 |
| c-7 | t-1 (requireTags switch), t-2 (fixture), t-8 (live tree) |
| c-8 | t-1 (hash), t-3 (re-confirm), t-6 (gate end-to-end) |
| c-9 | t-1 (predicate), t-6 (gate) |

Edges: adjacency (c-7) maps to t-2's complete-phase subtest plus t-8's live run. Idempotency (c-8, c-4) maps to t-3's re-run no-op plus t-6's edit-voids-then-reconfirm. Empty (c-9) maps to t-1 plus t-6's no-decisions subtest.

### Grafts applied to the risk skeleton

- Risk t-4 and t-5 are merged into one t-4, taking the structure from mvp+verification and keeping every risk contract.
- `assets/partials_test.go` is added to t-7, from mvp and verification. The vocabulary pin's length check fails without it.
- From verification: the copied-hash contract and `oneway` unknown value in t-1, the legacy-saves-byte-clean wording, the "lists the spec's decision keys" refusal in t-3, the asserted interaction-audit row and the "no `confirmed =` line" contract in t-7, and the `locked = true` count precondition in t-8.
- t-5's §5 note on `confirmed` is worded so that risk's "states it is never hand-written" and verification's "no `confirmed =` line" both hold.

## Disagreements

1. **Own-spec tagging: separate task, folded in, or absent.**
   - *Risk:* a separate wave-3 task (t-9) that tags the decisions, runs the confirm turn through the real verb and checks the installed `dross validate` against the live tree.
   - *Verification:* folds the tagging into the wave-2 validate task, with tags only and no confirmations because the verb does not exist yet, and checks via `go run ./cmd/dross validate`.
   - *Mvp:* omits it, saying execute/verify handle it.
   - *Provisional default:* risk's separate task (t-8), plus verification's precondition.
   - *Why it matters:* this phase is the only open phase with locked decisions, so after `make install` both validate and ship's pre-flight refuse on this spec until it is tagged. Folding the tags into t-2 lands them before the verb exists, which leaves the own one-way decisions unconfirmed and rendering as "[one-way, unconfirmed]" in this phase's own PR body. Omitting the task surfaces as a red validate at verify or ship. Open choice: the installed binary (risk, needs r-01 discipline) or `go run` from source (verification, immune to a stale binary but not what users run).

2. **Hash format written into `confirmed`.**
   - *Risk:* `"sha256:" + full hex`, plus a pinned-literal test.
   - *Mvp:* bare full hex.
   - *Verification:* truncated to 16 hex, like deferred ids.
   - *Provisional default:* risk's prefixed full hex with the pinned literal. All three agree on the NUL separator.
   - *Why it matters:* this is a file-format choice, so by this phase's own c-2 rule it is one-way. Every confirmation committed into spec.toml carries it, and changing it later voids every confirmation in the repo. The prefix lets the algorithm change later; 16 hex matches house style and is shorter in diffs. Worth a deliberate pick at plan steering rather than a default.

3. **Validate wiring and plan-lock gate: one task or two.**
   - *Mvp:* merges them into one wave-3 task, reasoning that both are thin callers in one package and a split buys no useful parallelism.
   - *Risk and verification:* split them.
   - *Provisional default:* split (t-2 in wave 2, t-6 in wave 3).
   - *Why it matters:* merging forces validate, which needs only t-1, behind the verb, puts 5 criteria and 4 files in one task, and gives one task-review verdict over two independent surfaces.

4. **Ship body and reviewer context: one task or two.**
   - *Risk:* two tasks.
   - *Mvp and verification:* one task. Verification states that each half is a one-file render change below the granularity floor.
   - *Provisional default:* merged (t-4), keeping all of risk's contracts.
   - *Why it matters:* only granularity and review overhead. Both halves read the same DoorLabel, so a single task also keeps the two marker vocabularies from drifting.

5. **Prompt work: one task or two.**
   - *Mvp:* one 5-file task covering §3c tagging, the partial, partials.go, partials_test.go and the spec prompt test.
   - *Risk and verification:* split base §3c tagging (c-2) from the confirm-turn partial (c-3).
   - *Provisional default:* split (t-5, then t-7).
   - *Why it matters:* the 165-byte headroom makes the trim-and-fit work in t-5 a separate risk from adding the partial. Splitting lets t-5 land before the verb exists, gives each half a byte-budget precondition, and keeps c-2 reviewable apart from c-3. The cost is a 6-file t-7 (see 7).

6. **Marker vocabulary and stale-confirmation rendering.**
   - *Risk:* a shared DoorLabel with a third state, "[one-way, unconfirmed]", for a stale or empty hash, so UnconfirmedOneWay returns {Key, Stale}.
   - *Mvp:* marks only one-way ("⚠ one-way"), leaves reversible unmarked and ignores confirmation state.
   - *Verification:* `[one-way]` / `[reversible]` with no confirmation state, and UnconfirmedOneWay returns []string.
   - *Provisional default:* risk's three-state label and struct return.
   - *Why it matters:* the "unconfirmed" state is the one signal that a choice was edited after plan-lock, and the PR reader and the task reviewer are who should see that drift. It goes beyond c-5/c-6's literal "distinct from reversible", though, and it ties both renderers to the hash rule. If dropped, t-1's return type can shrink to []string and t-6's "choice changed" wording loses its source.

7. **docs/interaction-audit.md row for the confirm turn.**
   - *Risk and verification:* include it. Verification also asserts it in spec_prompt_test.
   - *Mvp:* omits it, since no test enforces per-turn rows and no criterion asks for one, and suggests the judge add it as a companion.
   - *Provisional default:* include it, asserted. That makes t-7 six files, above the 5-file guideline.
   - *Why it matters:* the extra files are all guard or doc companions, not extra layers, and splitting would mean inventing a task. The alternative is dropping the audit row, as mvp does, to get back to 5 files, at the cost of an undocumented AskUserQuestion turn in a repo that audits its interaction points.

8. **README mention of the new verb.**
   - *Risk:* adds README.md to the verb task.
   - *Mvp and verification:* leave it out.
   - *Provisional default:* include it (t-3 at 5 files).
   - *Why it matters:* no test forces it (`TestReadmeAdvertisesOnlyRealCommands` checks only the reverse direction), so leaving it out stays green but leaves the README's `dross phase {…}` row silently incomplete. Including it puts t-3 at the file-count ceiling.
