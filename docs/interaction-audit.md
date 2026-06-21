# Interaction audit

Tracks how each interactive dross command conforms to the **propose-and-react,
one-decision-per-turn** contract (`dross-interaction-contract` builtin rule +
`assets/prompts/_interaction.md` playbook).

**Scope.** "Interactive" = any command whose `assets/commands/dross-<name>.md`
wrapper lists `AskUserQuestion` in `allowed-tools`. Read-only commands (`status`)
and subagent-only commands (`plan-review`) are out of scope and intentionally
absent. The Go test in `internal/cmd/interaction_audit_test.go` fails if an
interactive command has no section here.

**Conformance legend** (filled in by phases 11–13):

- ✅ conforms — one decision per turn, proposes a default, references the playbook via `dross interaction show`
- 🟡 partial — drives conversationally but doesn't yet invoke the `dross interaction show` emitter
- ⬜ pending — not yet audited
- ❌ violates — batches decisions or dumps an artifact/agenda wall

Each command lists its **decision points** (the moments it asks the user to
choose) one row each — not one row per command — so the retrofit can confirm the
pattern point by point.

---

## Pilot result (phase 10 — c-3)

`dross-spec` is the pilot that proved the snippet-delivery mechanism before
phases 11–13 repeat it. The pilot ran in a fresh `/dross-spec` session on
**2026-06-21**.

**Outcome: nested `@`-include FAILED; resolved via the `dross interaction show`
emitter.** Loading `/dross-spec` expands the command wrapper's top-level
`@`-include of `spec.md`, but `spec.md`'s own `@`-include of the snippet
(`@~/.claude/dross/prompts/_interaction.md`) arrives as literal text — the
two-level (wrapper → spec.md → _interaction.md) expansion does **not** resolve,
so the snippet sentinel never reaches the model through the include. Per the
locked `snippet_delivery` decision, the `dross interaction show` CLI emitter
(embeds `_interaction.md`, prints it verbatim, mirrors `dross rule show`) was
adopted and wired into `spec.md`'s pre-flight.

| Check | Result | When |
|---|---|---|
| Nested `@`-include delivers the snippet to the model | ❌ FAILED — arrives as literal text | 2026-06-21 |
| `dross interaction show` prints the playbook verbatim from the binary (`TestInteractionShowEmitsPlaybook`, single-source `TestInteractionPlaybookSingleSource`) | ✅ resolved via the `dross interaction show` emitter | 2026-06-21 |
| `spec.md` pre-flight invokes the emitter and dropped the dead `@`-include (`TestSpecPilotUsesEmitter`) | ✅ pass | 2026-06-21 |

**Pattern for phases 11–13:** each interactive prompt's pre-flight runs
`dross interaction show` (alongside `dross rule show`) — grep-verifiable, no
dependency on nested include expansion.

---

## Core loop (phase 11 — retrofit-core-loop)

The spec→plan→execute→verify→ship pipeline plus the PR-review panel. `dross-spec`
is the phase-10 pilot; `dross-plan/execute/verify/ship/review` are retrofitted in
phase 11. (`dross-milestone` and `dross-quick` are scoping/one-off commands —
retrofitted under Setup & config in phase 12.)

### dross-spec

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Phase resolution / create | AskUserQuestion new/resume | ✅ | pilot — pre-flight runs `dross interaction show` |
| Each acceptance criterion | one criterion per turn, accept/reword/drop | ✅ | one-at-a-time |
| Gray-area selection | multiSelect AskUserQuestion | ✅ | |
| Each gray-area deep-dive | one focused exchange per area | ✅ | |
| Lock spec | one-line summary, y/edit | ✅ | never pastes the TOML |

### dross-plan

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Panel disagreements | one propose-and-react turn per divergence, leads with judge's pick | ✅ | `panel_disagreement_walk`; no full-list wall |
| Steer-or-proceed | single AskUserQuestion, leads with `proceed` | ✅ | |
| Coverage gap resolution | add task / move to deferred | ✅ | |
| Lock plan | one-line summary, y/edit — no toml dump | ✅ | c-4 |

### dross-execute

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Per-task approach | proceed/steer/show/skip, leads with `proceed` | ✅ | pair-mode; next task never bundled behind current |
| Red test outcome | fix/mark-failed/abort | ✅ | own turn |
| Dirty-tree pre-flight | commit/stash/abort | ✅ | |

### dross-verify

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Verdict + criterion-map surface | verdict + compact criterion→test/status map, no `verify.toml` dump | ✅ | `verify_surface`; surfaced as a report, not asked (no AskUserQuestion turn — c-3 satisfied by absence) |

### dross-ship

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| PR-body preview | shown in full before the post is authorized | ✅ | `ship_body_preview` — deliberate outward-facing exception to c-4 |
| Body override | AskUserQuestion generated/own | ✅ | own turn |
| Reviewer selection | propose-and-react turn (use these / change / none) | ✅ | converted from a silent config-write |
| Merge gate | merge/hold | ✅ | own turn |

### dross-review

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Post findings comment | single post/skip turn, leads with default | ✅ | composed comment shown in full before posting (outward-facing exception) |

---

## Setup & config (phase 12 — retrofit-setup-commands)

### dross-milestone

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Resolve milestone version | single AskUserQuestion, default = next minor | ✅ | pre-flight runs `dross interaction show` |
| Title | proposed default, accept/override | ✅ | own turn |
| Success criteria | one criterion per turn, accept/reword/drop | ✅ | `milestone_walk` — per-criterion walk like spec |
| Non-goals | own segment, accept/revise | ✅ | separate segment, not bundled with criteria |
| Phase breakdown | confirm/revise order | ✅ | own turn; wrap confirms via count line, never a toml dump |

### dross-quick

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Approach approval | proceed/steer/show/abort (pair-mode) | ✅ | `quick_inbox_mirror` — mirrors execute §1c, adapted to a single task |
| Red test outcome | fix/abort | ✅ | mirrors execute §1e (no task-loop mark-failed) |

### dross-init

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Identity walk | one field per turn (name → … → non-goals) | ✅ | per-field, not one bundled questionnaire; confirmed by one-line summary |
| Stack choices | per-choice confirmation | ✅ | one decision per turn |
| Runtime mode | options | ✅ | own turn |
| Rules import | accept/edit | ✅ | own turn |

### dross-onboard

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Identity capture | one field per turn | ✅ | summary-confirm, never a `project.toml` paste-back |
| Detected-signal confirmation | per-signal accept/correct | ✅ | one signal per turn |
| Runtime capture | options | ✅ | own turn |
| Rule import | accept/edit | ✅ | own turn |

### dross-options

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Section pick | section-pick gate (multiSelect), its own turn | ✅ | `options_walk` — gate distinct from the per-setting turn |
| Which setting to change | Keep · Change · Skip, one setting at a time | ✅ | walked within chosen section only |
| New value per setting | proposal + react | ✅ | save-per-option |
| Wrap summary | compact one-line-per-category | ✅ | never pastes the full `project.toml` |

### dross-rule

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Action select | intent → add/list/remove/promote | ✅ | resolved in Parse intent, its own step |
| Scope | proposal turn (project / global) | ✅ | separate proposal turn |
| Severity | proposal turn (hard / soft) | ✅ | separate proposal turn |
| Wording | proposal turn (accept / reword) | ✅ | confirmed by one-line summary, never a rules-block dump |

### dross-inbox

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Per-issue triage | one issue per turn (phase / milestone / quick / dismiss / skip) | ✅ | never bundles multiple issues into one turn |
| Target destination | routed by the triage choice | ✅ | follows from the per-issue decision |

---

## Other interactive commands (audited in phase 13)

The read-only-leaning commands one tier out from the loop: the two heavy audits
(`secure`/`quality`), the backfill engine (`architecture`), and the handoff pair
(`pause`/`resume`). Each has essentially **one gated decision**, so the retrofit is
uniform — wire `dross interaction show` into the pre-flight (per the
`scan_command_emitter` and `handoff_emitter_exception` decisions) and confirm the
single propose-and-react turn. Two carry a deliberate artifact-preview exception
(the drafted doc / handoff *is* the thing being confirmed, like `ship`'s PR-body
preview).

### dross-architecture

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| ARCHITECTURE.md write approval | §3 propose→approve→write, leads with the drafted doc, single proceed/steer gate | ✅ | pre-flight runs `dross interaction show`; read-only fan-out maps features, the write is the only gated turn. Showing the full drafted doc is a deliberate artifact-preview exception (the deliverable), like ship's PR-body preview |

### dross-secure

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Remediation-phase scaffold | §7 propose-then-ask — show criteria, confirm before locking, exactly like /dross-spec | ✅ | pre-flight runs `dross interaction show`; the emitter shapes only this gated turn — audit scope stays context-free |

### dross-quality

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Remediation-phase scaffold | §7 propose-then-ask — show criteria, confirm before locking, exactly like /dross-spec | ✅ | pre-flight runs `dross interaction show`; the emitter shapes only this gated turn — tool sweep stays code-only, calibrate-only context |

### dross-pause

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Confirm + amend handoff | §2 single AskUserQuestion (save / amend / cancel) | ✅ | pre-flight runs `dross interaction show`. Inline handoff-draft preview is a documented exception — the user confirms their own working memory, like ship's PR-body preview, not an artifact-dump violation |

### dross-resume

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Prune handoff items | §2 walks each ## Next / ## Open-loops item one at a time (done / keep / edit) | ✅ | pre-flight runs `dross interaction show`; "pruning is the user's call, item by item" — never a batched dump |
