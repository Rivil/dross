# context-hygiene — test-contract-first draft

Lens: design each criterion's ideal test contract first, then derive the smallest
task that makes that contract satisfiable. Every task below exists because a named
test would fail without it.

```
Phase context-hygiene — 8 tasks across 2 waves

Wave 1
  t-1  Footer 6 boundary prompts (non-execute)
       files:    assets/prompts/spec.md, assets/prompts/plan.md,
                 assets/prompts/verify.md, assets/prompts/ship.md,
                 assets/prompts/quick.md, assets/prompts/pause.md
       covers:   c-1
       contract: TestClearPointCoverageFailClosed flags `ship` as unclassified
                 if the literal footer sentinel ("state is on disk — safe to
                 /clear" + an exact next-command line) is deleted from ship.md.

  t-2  Execute checkpoint gate + footer
       files:    assets/prompts/execute.md, docs/interaction-audit.md,
                 internal/cmd/execute_prompt_test.go
       covers:   c-1, c-2
       contract: TestExecutePromptOffersCheckpoint fails if execute.md's §1
                 per-task gate drops the `checkpoint` option, the plan/state
                 consistency check, or the exact re-entry line `/clear →
                 /dross-execute --from <next-task>`; a second assertion fails if
                 the wave-boundary lead-option instruction (checkpoint_posture)
                 is removed; a third fails if execute.md loses the clear-point
                 footer sentinel.

  t-3  Status re-entry line + reentryLine helper
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-4
       contract: TestStatusEndsWithReentryLine fails if `dross status`'s final
                 emitted line is not the "you are here" re-entry line carrying the
                 exact next command (the suggestNext string); asserting on the
                 LAST line catches a regression that prints it mid-output.

  t-4  `dross pause --auto` snapshot writer
       files:    internal/cmd/pause.go, internal/cmd/pause_test.go,
                 cmd/dross/main.go
       covers:   c-3
       contract: TestPauseAutoPreservesHandwritten fails if pause --auto, run
                 against a handoff.md containing a hand-written `## Thread`/`##
                 Next`/`## Open loops`, alters any byte of those sections (it may
                 only write/replace a dedicated `## Auto-snapshot` block carrying
                 branch, dirty files, dross status, timestamp);
                 TestPauseAutoNoPrompt fails if it reads stdin / prompts;
                 TestPauseAutoIdempotentMerge fails if a second run appends a
                 duplicate auto-section instead of replacing the first.

  t-5  Claude Code settings.json hook-merge helper
       files:    internal/claudehook/hooks.go, internal/claudehook/hooks_test.go
       covers:   c-3, c-5
       contract: TestMergeHookPreservesOthers fails if adding a PreCompact hook to
                 a settings.json that already holds a SessionStart hook drops,
                 reorders, or rewrites the SessionStart entry or any sibling key;
                 TestMergeHookIdempotent fails if merging the same hook twice
                 yields a second identical matcher entry.

Wave 2
  t-6  `dross reentry` command (no-op outside repo)   (depends t-3)
       files:    internal/cmd/reentry.go, internal/cmd/reentry_test.go,
                 cmd/dross/main.go
       covers:   c-5
       contract: TestReentryNoopOutsideRepo fails if `dross reentry` run outside a
                 dross repo exits non-zero or prints anything;
                 TestReentryEmitsLineInsideRepo fails if, inside a repo, it does
                 not print the same re-entry line reentryLine() feeds status.

  t-7  Clear-point footer coverage engine + audit doc   (depends t-1, t-2)
       files:    internal/cmd/clearpoint_coverage.go,
                 internal/cmd/clearpoint_coverage_test.go,
                 docs/clear-point-audit.md
       covers:   c-1
       contract: TestClearPointCoverageFailClosed fails, naming the prompt, when a
                 command-backed prompt neither contains the footer sentinel nor
                 appears in docs/clear-point-audit.md's `## Exempt` list;
                 TestClearPointExemptRemovalFails fails if `status` is dropped from
                 that Exempt list; TestClearPointBoundaryCommandsBearFooter fails
                 if any of the 7 boundary prompts (spec/plan/execute/verify/ship/
                 quick/pause) is missing the sentinel.

  t-8  Scaffold PreCompact + SessionStart hooks in init/onboard   (depends t-4, t-5)
       files:    internal/cmd/init.go, internal/cmd/onboard.go,
                 internal/cmd/init_test.go, internal/cmd/onboard_test.go
       covers:   c-3, c-5
       contract: TestInitScaffoldsHooks fails if `dross init` does not ensure, in
                 the ~/.claude/settings.json it merges via t-5's helper, a
                 PreCompact hook whose command is `<bin> pause --auto` and a
                 SessionStart hook whose command is `<bin> reentry`;
                 TestInitHooksIdempotent fails if a second `dross init` duplicates
                 either hook; TestOnboardScaffoldsHooks asserts the same for
                 `dross onboard`.
```

## Coverage

| Criterion | Delivered by |
|---|---|
| c-1 (clear-point footer on 7 boundary commands, fail-closed coverage test) | t-1, t-2 (footers), t-7 (gate) |
| c-2 (execute pair-mode checkpoint gate option + re-entry command) | t-2 |
| c-3 (`dross pause --auto` mechanical snapshot + PreCompact hook scaffold) | t-4 (writer), t-5 (hook helper), t-8 (scaffold) |
| c-4 (`dross status` ends with "you are here" re-entry line) | t-3 |
| c-5 (SessionStart hook injects re-entry line, no-ops outside; init/onboard ensure it) | t-3 (shared reentryLine), t-5 (hook helper), t-6 (`dross reentry` cmd), t-8 (scaffold) |

All 5 criteria covered.

## Judgment calls

- **Auto-detect footer-bearing by sentinel presence, mirroring interaction_coverage.**
  Chose: a prompt is footer-bearing iff it contains the literal sentinel, exempt iff
  listed in a new `docs/clear-point-audit.md ## Exempt`, uncovered otherwise (fail).
  Rejected: a per-command explicit "footer=yes" table. The auto-detect version makes
  "boundary command must bear a footer" fall out of the same fail-closed check the
  footer_coverage decision mandates, and a new command with no footer and no exempt
  entry fails automatically — no second registry to maintain.
- **New `docs/clear-point-audit.md`, not a column bolted onto interaction-audit.md.**
  Chose: a parallel doc. Rejected: extending interaction-audit.md. The footer_coverage
  decision says "mirrors the interaction-audit convention" (same shape), and folding a
  second classification into one doc would couple two independent gates and force every
  new command through one merged table. Cost accepted: a new command may need two
  enrollments; that cost is the fail-closed guarantee.
- **Execute footer lives in t-2, not t-1.** Chose: t-2 owns every edit to execute.md
  (footer + checkpoint). Rejected: t-1 covering all 7 prompts. Two wave-1 tasks editing
  the same file race; splitting execute out keeps t-1 and t-2 conflict-free while both
  still deliver c-1.
- **Reuse the existing `--from <task-id>` execute flag rather than add a CLI verb.**
  execute.md already documents `--from <task-id>` (line 22) and execute has no Go CLI
  verb — it is prompt-orchestrated. So c-2's re-entry command is a prompt-emitted string
  (`/dross-execute --from <next-task>`), not new command surface. The checkpoint's
  "plan.toml + state consistent" confirmation reuses existing `dross status`/`dross
  validate`, so t-2 stays prompt-only.
- **Shared `reentryLine()` helper feeds both status (c-4) and `dross reentry` (c-5).**
  Chose: one generator in status.go, consumed by the new `dross reentry` command.
  Rejected: two independent re-entry renderers. c-5's hook must inject "the short
  re-entry line (from handoff.md / dross status)" — deriving it from the same helper
  guarantees the hook and status can never drift. This makes t-6 depend on t-3.
- **t-8 depends only on t-5 (the merge API) for wave placement; verb strings are
  plan-fixed constants.** Chose: t-8 = wave 2, depends [t-4, t-5], hard-coding the agreed
  verbs `pause --auto` and `reentry`. Rejected: hard-depending on t-6 (which would push
  t-8 to wave 3). The hook command strings are decided here, not discovered from t-6's
  runtime output, so the strict "needs the output of" rule keeps t-8 in wave 2; t-4 is
  listed because the PreCompact hook is dead surface unless `pause --auto` exists.
- **Hook merge is its own helper package (internal/claudehook), not folded into
  internal/statusline.** Chose: a dedicated MergeHook/RemoveHook. Rejected: extending the
  statusline settings merger. Hooks are a nested event/matcher structure, not the flat
  `statusLine` key; a separate helper keeps both mergers single-purpose and lets t-8
  wire both PreCompact and SessionStart through one tested API.
