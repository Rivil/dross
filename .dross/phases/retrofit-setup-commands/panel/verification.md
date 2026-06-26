# Phase 12 — verification-biased decomposition

Designed backward from the test contracts. For each criterion I first wrote the
ideal Go test (function name, what it asserts, over which files), then derived
the smallest task that makes that contract pass. The grep criteria (c-1, c-2,
c-5) collapse into three table-driven tests over a `setupConfigPrompts` slice
(direct twins of phase 11's `interaction_coreloop_test.go`). The structural
criteria (c-3, c-4) each demand a *named per-prompt anchor test* keyed on a
stable string the rewritten prompt must carry — so the prompt edits are not free
prose, they are written to satisfy a specific `strings.Contains` assertion.

Test contracts I am committing the prompts to (the new file
`internal/cmd/interaction_setupconfig_test.go`, package `cmd`, reusing
`repoRootFromTest`, `deadIncludeLine`, `interactionRefPhrase`, `promptSection`,
`coreLoopAuditSection`):

- `setupConfigPrompts = []string{"init","onboard","options","rule","inbox","quick","milestone"}`
- **c-1** `TestSetupConfigPromptsWireEmitter` — per prompt: body contains
  `dross interaction show` AND does NOT contain `deadIncludeLine`.
- **c-2** `TestSetupConfigPromptsReferenceContract` — per prompt: body contains
  `interactionRefPhrase` (`"interaction playbook"`).
- **c-5** `TestSetupConfigAuditSectionsConform` — per prompt: `coreLoopAuditSection`
  for `dross-<name>` contains `✅` and none of `⬜ 🟡 ❌`.
- **c-3 options** `TestOptionsPromptSectionGate` — §0/§"How each section works"
  region carries the section-pick gate anchor (`project / defaults / rules /
  profile / env`) AND each numbered section drives one `AskUserQuestion` per
  setting (the per-setting walk), never one mega-form.
- **c-3 milestone** `TestMilestonePromptWalksSegments` — §3 success criteria, §4
  non-goals, §5 phase order are each their own `AskUserQuestion` segment (one
  per section), proving the spec-style walk.
- **c-3 quick** `TestQuickPromptMirrorsExecuteGates` — §2 approval and §4 red-path
  are each exactly one `AskUserQuestion`, carrying execute's `proceed`/`steer`
  and `fix`/`abort` anchors verbatim.
- **c-3 generic** `TestSetupConfigNoBundledTurns` — per prompt, every `## `
  decision section flagged as a turn holds exactly one `AskUserQuestion`.
- **c-4** `TestSetupConfigNoArtifactDump` — init/onboard/options/milestone/rule
  each carry their one-line-summary anchor and a no-paste directive, so a
  composed `project.toml`/`milestone.toml`/rules/diff is confirmed by summary,
  not pasted back wholesale.

---

## The plan

Phase 12-retrofit-setup-commands — 8 tasks across 2 waves

### Wave 1

```
t-1  Wire emitter + contract into all 7 prompts
     files:    assets/prompts/init.md, assets/prompts/onboard.md,
               assets/prompts/options.md, assets/prompts/rule.md,
               assets/prompts/inbox.md, assets/prompts/quick.md,
               assets/prompts/milestone.md
     covers:   c-1, c-2
     contract: TestSetupConfigPromptsWireEmitter fails if any of the 7 drops
               `dross interaction show` from pre-flight or keeps the dead
               @-include line; TestSetupConfigPromptsReferenceContract fails if
               any drops the literal "interaction playbook" intro phrase.

t-2  Restructure options.md into section-gate + per-setting walk
     files:    assets/prompts/options.md
     covers:   c-3, c-4
     contract: TestOptionsPromptSectionGate fails if the section-pick gate
               anchor "project / defaults / rules / profile / env" is missing
               or any numbered section walks settings as a single bundled
               AskUserQuestion; TestSetupConfigNoArtifactDump (options arm)
               fails if the §12 wrap stops confirming via the compact
               "Reviewed N sections" summary and pastes project.toml back.

t-3  Restructure milestone.md into spec-style scoping walk
     files:    assets/prompts/milestone.md
     covers:   c-3, c-4
     contract: TestMilestonePromptWalksSegments fails if §3 criteria, §4
               non-goals, or §5 phase order is not its own single
               AskUserQuestion segment; TestSetupConfigNoArtifactDump
               (milestone arm) fails if the wrap pastes milestone.toml back
               instead of the "<N> criteria, <M> phases" summary.

t-4  Restructure quick.md gates to mirror execute verbatim
     files:    assets/prompts/quick.md
     covers:   c-3
     contract: TestQuickPromptMirrorsExecuteGates fails if §2 approval or §4
               red-path holds != 1 AskUserQuestion, or if the proceed/steer
               and fix/abort anchors diverge from execute's shipped wording.

t-5  Restructure init/onboard/rule/inbox interactive turns
     files:    assets/prompts/init.md, assets/prompts/onboard.md,
               assets/prompts/rule.md, assets/prompts/inbox.md
     covers:   c-3, c-4
     contract: TestSetupConfigNoBundledTurns fails if any flagged decision
               section in these four holds >1 AskUserQuestion (e.g. init §1
               vision must split into per-field turns, not one mega-form);
               TestSetupConfigNoArtifactDump (init/onboard/rule arms) fails if
               a composed project.toml or rules block is pasted back rather
               than confirmed by a one-line summary.

t-6  Flip 7 audit sections to conforming, sync decision points
     files:    docs/interaction-audit.md
     covers:   c-5
     contract: TestSetupConfigAuditSectionsConform fails if any of the 7
               `### dross-<name>` sections lacks ✅ or still carries ⬜/🟡/❌;
               rows must match the rewritten prompts (e.g. options shows the
               section-pick gate, milestone shows per-criterion walk).
```

### Wave 2 (depends t-1..t-6)

```
t-7  Add interaction_setupconfig_test.go (all criteria)
     files:    internal/cmd/interaction_setupconfig_test.go
     covers:   c-1, c-2, c-3, c-4, c-5
     depends:  t-1, t-2, t-3, t-4, t-5, t-6
     contract: This file IS the contract surface. Each named test above lives
               here; if a wave-1 prompt/audit edit regresses, its specific test
               (named per criterion+prompt) goes red. Mirrors phase 11's
               interaction_coreloop_test.go structure and reuses its helpers.

t-8  make install + run full suite green
     files:    (no source edits — gate task)
     covers:   c-1, c-2, c-3, c-4, c-5
     depends:  t-7
     contract: RULE r-01 — prompt/Go edits are inert until `make install`
               re-links assets and rebuilds the binary. Run `make install`
               then `go test -count=1 ./...`; the run must be observed green
               before this task is marked done. No test asserts this; it is the
               human/agent gate that the contracts above were actually executed
               against the installed artifact, not just authored.
```

---

## Coverage

| Criterion | Delivered by | Test function (in t-7) |
|---|---|---|
| c-1 | t-1 (edit), t-7 (test), t-8 (gate) | `TestSetupConfigPromptsWireEmitter` |
| c-2 | t-1 (edit), t-7 (test), t-8 (gate) | `TestSetupConfigPromptsReferenceContract` |
| c-3 | t-2, t-3, t-4, t-5 (edits), t-7 (tests), t-8 (gate) | `TestOptionsPromptSectionGate`, `TestMilestonePromptWalksSegments`, `TestQuickPromptMirrorsExecuteGates`, `TestSetupConfigNoBundledTurns` |
| c-4 | t-2, t-3, t-5 (edits), t-7 (test), t-8 (gate) | `TestSetupConfigNoArtifactDump` |
| c-5 | t-6 (edit), t-7 (test), t-8 (gate) | `TestSetupConfigAuditSectionsConform` |

Every criterion c-1..c-5 has at least one named test in t-7 and a prompt/audit
edit feeding it. 5/5 covered.

## Judgment calls

- **One test file, table-driven for grep + per-prompt for structure** (vs. a
  test file per prompt). Chose: single `interaction_setupconfig_test.go`
  mirroring phase 11's one-file model — c-1/c-2/c-5 loop the 7-prompt slice,
  c-3/c-4 get named per-prompt funcs. Rejected: seven files. Why: phase 11's
  proven shape, and the slice catches a dropped prompt that seven hand-written
  files could silently omit.
- **c-3 needs both a generic and three specific tests** (vs. one generic
  count). Chose: `TestSetupConfigNoBundledTurns` (generic, catches any future
  bundling) PLUS the three locked-decision anchor tests. Rejected: generic
  count only. Why: the locked decisions (options gate, milestone walk, quick
  mirror) name concrete shapes a bare count can't assert — phase 11's verify
  found c-3 "weak (audit-doc proxy only)"; anchoring on the decision strings is
  exactly the fix that history demands.
- **t-2/t-3/t-4 split from t-5** (vs. one prompt-edit task). Chose: separate
  the three prompts with their own locked-decision anchor test from the four
  generic ones. Rejected: a single 7-file restructure task. Why: granularity
  rule caps a task at <5 files / 2 layers, and each split maps cleanly to a
  distinct named contract — a failing test points at one task.
- **t-8 is a real task, not a checklist note.** Chose: an explicit
  `make install` + observed-green gate task. Rejected: folding it into t-7's
  description. Why: RULE r-01 (hard) plus the Execution-Safety rule that a
  result must be *observed* before claiming pass — the gate has to be its own
  step so the green run is read before the phase is called done.
- **Audit sync is t-6, separate from prompt edits.** Chose: one task owning
  docs/interaction-audit.md, depending conceptually on the rewritten shapes.
  Rejected: editing the audit inside each prompt task. Why: c-5's test asserts
  the audit *matches* the prompts; keeping it one task lets the row-vs-prompt
  consistency be reviewed in a single diff against all 7 rewrites.
