Phase context-hygiene — 7 tasks across 3 waves

Lens: **failure modes drive the graph.** Every task owns exactly one way this
feature can silently corrupt user state or lose the workflow thread. The three
sharpest edges — clobbering `~/.claude/settings.json`, clobbering the user's
hand-written `handoff.md`, and a hook firing noise into non-dross repos — are
each isolated into their own wave-1 task with a test that reproduces the break.

Wave 1
  t-1  Hook-merge helper for settings.json
       files:    internal/hooks/settings.go, internal/hooks/settings_test.go
       covers:   c-3, c-5
       contract: installing the PreCompact hook into settings.json twice yields
                 byte-identical output (idempotent); a pre-existing foreign
                 PreCompact/SessionStart entry survives untouched; malformed
                 JSON input returns an error and writes nothing (no partial
                 clobber). TestMergeHookIdempotent / TestMergeHookPreservesForeign
                 / TestMergeHookRejectsGarbage.

  t-2  dross pause --auto snapshot (section-scoped)
       files:    internal/cmd/pause.go, internal/cmd/pause_test.go,
                 internal/cmd/root.go
       covers:   c-3
       description: new non-interactive `dross pause --auto` that writes/merges
                 ONLY a dedicated `## Auto-snapshot` block (branch, dirty files,
                 dross status, injected timestamp) into .dross/handoff.md; never
                 prompts; no-ops (exit 0, no write) outside a dross repo.
       contract: given a handoff.md with a hand-edited `## Thread` and `## Open
                 loops`, `pause --auto` leaves those two sections byte-identical
                 and rewrites only the `## Auto-snapshot` block; a second run
                 updates that block in place (no duplicate section, next `##`
                 heading not consumed); invoked outside a dross repo it exits 0
                 and creates no file. Clock is injected so the timestamp is
                 deterministic in-test.

  t-3  Add clear-point footers to 7 boundary prompts
       files:    assets/prompts/spec.md, assets/prompts/plan.md,
                 assets/prompts/execute.md, assets/prompts/verify.md,
                 assets/prompts/ship.md, assets/prompts/quick.md,
                 assets/prompts/pause.md
       covers:   c-1
       description: each prompt's wrap-up ends with the clear-point footer —
                 the literal "state is on disk — safe to /clear" sentinel plus
                 the exact next command for a fresh session.
       contract: the c-1 gate (t-6) fails naming any of the seven if its footer
                 sentinel is absent; each of the seven prompt files contains the
                 sentinel string. TestBoundaryPromptsCarryFooter greps all seven.

  t-4  dross reentry emitter + status re-entry footer
       files:    internal/cmd/reentry.go, internal/cmd/reentry_test.go,
                 internal/cmd/status.go, internal/cmd/status_test.go,
                 internal/cmd/root.go
       covers:   c-4
       description: `dross reentry` prints one "you are here + exact next
                 command" line (reusing suggestNext); silent exit 0 outside a
                 dross repo (SessionStart-safe). `dross status` ends by emitting
                 the identical line.
       contract: `dross reentry` outside a dross repo exits 0 with empty stdout;
                 inside, its single line names the same next command
                 `suggestNext` returns (no plan → `/dross-spec --new`;
                 next-runnable present → `/dross-execute`; verify verdict flipped
                 to fail → the fail hint); `dross status`'s final printed line is
                 byte-equal to `dross reentry`'s output.

Wave 2 (depends t-1, t-3, t-4)
  t-5  Checkpoint option in execute pair-mode gate
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       covers:   c-2
       depends:  t-4
       description: extend the per-task gate to continue/stop/**checkpoint**;
                 checkpoint confirms plan.toml + state are consistent, then ends
                 the session with the exact re-entry command
                 (/clear → /dross-execute --from <next-task>). Checkpoint is the
                 recommended lead option at wave boundaries.
       contract: execute_prompt_test asserts the gate enumerates a checkpoint
                 option, that checkpoint instructs a plan.toml/state consistency
                 check BEFORE emitting `--from <next-task>`, and that the
                 wave-boundary branch marks checkpoint as the lead/recommended
                 option; removing the checkpoint block from execute.md fails the
                 test.

  t-6  c-1 footer coverage doc + fail-closed test
       files:    internal/cmd/footer_coverage.go,
                 internal/cmd/footer_coverage_test.go, docs/footer-audit.md
       covers:   c-1
       depends:  t-3
       description: mirror the interaction-audit convention — classify every
                 command-backed prompt as footer-bearing (boundary) or
                 exempt-with-reason in docs/footer-audit.md; fail-closed Go gate.
       contract: deleting the footer sentinel from ship.md fails
                 TestFooterCoverageFailClosed naming `ship`; a new boundary
                 prompt with neither a footer nor a `## Exempt` row fails the
                 gate; an exempt row that omits its reason cell fails; a
                 correctly-footed or correctly-exempt prompt passes.

  t-7  Scaffold PreCompact + SessionStart hooks in init/onboard
       files:    internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/init.go, internal/cmd/onboard.go,
                 internal/cmd/init_test.go, internal/cmd/onboard_test.go
       covers:   c-3, c-5
       depends:  t-1, t-2, t-4
       description: an idempotent ensure that wires user-level
                 ~/.claude/settings.json — PreCompact → `dross pause --auto`,
                 SessionStart → `dross reentry` — via t-1's merge helper; both
                 dross init and dross onboard call it.
       contract: running init's hook-ensure twice leaves exactly one dross
                 PreCompact entry and one dross SessionStart entry (idempotent,
                 asserted against a temp CLAUDE_CONFIG_DIR); a pre-existing
                 foreign PreCompact hook is still present afterward; onboard's
                 ensure produces settings.json byte-identical to init's.
                 TestInitEnsuresHooksIdempotent / TestOnboardEnsuresSameHooks.

## Coverage

- c-1 → t-3 (footer content in 7 prompts), t-6 (fail-closed coverage gate)
- c-2 → t-5 (checkpoint gate option + re-entry command)
- c-3 → t-2 (`pause --auto` CLI + section-scoped merge), t-7 (PreCompact scaffold)
- c-4 → t-4 (`dross reentry` + status footer)
- c-5 → t-4 (re-entry line mechanism the hook injects), t-7 (SessionStart scaffold)

Every criterion accounted for (5/5).

## Judgment calls

- **New `internal/hooks` merge helper (t-1) instead of extending
  `internal/statusline/settings.go`.** Chose a purpose-built helper: Claude Code
  hooks are event-keyed arrays with matchers, not the single `statusLine` key
  the statusline merger handles; overloading that package would blur two schemas.
  Rejected reusing it directly — the clobber/idempotency risk is different enough
  to deserve its own tested surface.
- **`pause --auto` (t-2) is a brand-new CLI command, not a flag on the existing
  prompt.** `dross pause` today is prompt-only (no `internal/cmd/pause.go`). The
  PreCompact hook cannot invoke a slash-command prompt, so the mechanical snapshot
  MUST be a binary subcommand. Rejected trying to drive it through the prompt.
- **A shared `dross reentry` emitter (t-4) feeds both status's footer (c-4) and
  the SessionStart hook (c-5).** One derivation of "next command" = one place the
  resume target can be wrong. Rejected computing the line twice (once in status,
  once in the hook script) — divergence there silently resumes the wrong task.
- **Footer content (t-3, wave 1) split from the footer gate (t-6, wave 2).**
  The text edits are independent and parallelizable; the fail-closed test only
  has something to assert once the footers exist, so it strictly depends on t-3.
  Rejected bundling all 9 files into one task (too large, crosses prompt + Go +
  docs layers).
- **Checkpoint (t-5) depends on t-4, not t-7.** The gate needs the re-entry
  command derivation, not the installed hook. Kept it in wave 2 behind t-4 only,
  so it isn't serialized behind the settings.json scaffolding it doesn't use.
- **init + onboard hook-scaffolding merged into one task (t-7).** They share the
  identical ensure helper; splitting would duplicate the logic and the clobber
  risk across two tasks. Rejected per-command tasks — the risk is owned once.
