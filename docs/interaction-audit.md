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

- ✅ conforms — one decision per turn, proposes a default, references the snippet
- 🟡 partial — drives conversationally but doesn't yet `@`-include the snippet
- ⬜ pending — not yet audited
- ❌ violates — batches decisions or dumps an artifact/agenda wall

Each command lists its **decision points** (the moments it asks the user to
choose) one row each — not one row per command — so the retrofit can confirm the
pattern point by point.

---

## Core loop (phase 11 — retrofit-core-loop)

### dross-milestone

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Resolve milestone version | single AskUserQuestion, default = next minor | 🟡 | doesn't @-include snippet yet |
| Title | proposed default, accept/override | 🟡 | |
| Success criteria | accept/revise set | 🟡 | could go per-criterion |
| Non-goals | accept/revise set | 🟡 | |
| Phase breakdown | confirm/revise order | 🟡 | |

### dross-spec

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Phase resolution / create | AskUserQuestion new/resume | 🟡 | reference pilot lands here (t-6) |
| Each acceptance criterion | one criterion per turn, accept/reword/drop | 🟡 | already one-at-a-time |
| Gray-area selection | multiSelect AskUserQuestion | 🟡 | |
| Each gray-area deep-dive | one focused exchange per area | 🟡 | |
| Lock spec | one-line summary, y/edit | 🟡 | never pastes the TOML — good |

### dross-plan

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Panel disagreements | walk each divergence | 🟡 | |
| Steer-or-proceed | iterate until accept | 🟡 | |
| Coverage gap resolution | add task / move to deferred | 🟡 | |
| Lock plan | y/edit | 🟡 | |

### dross-execute

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Per-task approach | proceed/steer/show/skip | 🟡 | pair-mode only |
| Red test outcome | fix/mark-failed/abort | 🟡 | |
| Dirty-tree pre-flight | commit/stash/abort | 🟡 | |

### dross-quick

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Approach approval | proceed/steer (pair-mode) | 🟡 | |
| Red test outcome | fix/mark-failed/abort | 🟡 | |

---

## Setup & config (phase 12 — retrofit-setup-commands)

### dross-init

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Vision / core value | freeform + proposal | ⬜ | |
| Stack choices | per-choice confirmation | ⬜ | |
| Runtime mode | options | ⬜ | |
| Rules import | accept/edit | ⬜ | |

### dross-onboard

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Detected-signal confirmation | per-signal accept/correct | ⬜ | |
| Runtime capture | options | ⬜ | |
| Rule import | accept/edit | ⬜ | |

### dross-options

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Which setting to change | picklist | ⬜ | |
| New value per setting | proposal + react | ⬜ | |

### dross-rule

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Add / list / remove / promote | action select | ⬜ | |
| Rule text + severity | proposal | ⬜ | |

### dross-inbox

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Per-issue triage | milestone/phase/quick/drop | ⬜ | |
| Target destination | picklist | ⬜ | |

---

## Other interactive commands (audited in phase 13)

### dross-architecture

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Scope of backfill | options | ⬜ | |

### dross-secure

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Remediation phase scaffold | confirm | ⬜ | |

### dross-quality

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Remediation phase scaffold | confirm | ⬜ | |

### dross-review

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Post findings comment | confirm | ⬜ | |

### dross-verify

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Pass/fail/partial verdict | confirm proposed verdict | ⬜ | |

### dross-ship

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Reviewer selection | picklist | ⬜ | |
| Open PR confirmation | confirm | ⬜ | |

### dross-pause

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Handoff contents | confirm draft | ⬜ | |

### dross-resume

| Decision point | Current pattern | Conforms | Notes |
|---|---|---|---|
| Resume vs fresh | options | ⬜ | |
