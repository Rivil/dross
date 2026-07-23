Phase context-hygiene — 6 tasks across 2 waves

Lens: smallest task set that satisfies every criterion. One task per irreducible
deliverable; both hooks collapsed into one task (one mechanism), all seven footers
into one task (one uniform edit). Nothing speculative.

Wave 1
  t-1  Add `dross pause --auto` + auto-snapshot merge
       files:    internal/cmd/pause.go, internal/cmd/pause_test.go,
                 cmd/dross/main.go
       covers:   c-3
       desc:     New non-interactive `dross pause` cobra verb with `--auto`. Writes a
                 dedicated auto-snapshot section (branch, dirty files, `dross status`,
                 timestamp) into .dross/handoff.md by merge — never prompts, never
                 touches the hand-written `## Thread` / `## Open loops` sections. Merge
                 logic lives in pause.go as a unit-testable function; registered in main.go.
       contract: `dross pause --auto` on a handoff.md that already has a hand-written
                 `## Thread` section rewrites ONLY the auto-snapshot section and leaves
                 the `## Thread` bytes byte-identical (pause_test.go); running it with no
                 handoff.md creates one containing just the auto-snapshot section.

  t-2  Add re-entry line + `--reentry` to `dross status`
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-4
       desc:     `dross status` ends with a 'you are here' + exact-next-command re-entry
                 line (reusing suggestNext). Add a `--reentry` flag that prints ONLY that
                 line and no-ops (no output, exit 0) outside a .dross repo — the emitter
                 the SessionStart hook (t-6) invokes.
       contract: `dross status` final printed line matches the 'you are here …' re-entry
                 format and names suggestNext's command; `dross status --reentry` run
                 outside a .dross repo prints nothing and exits 0 (status_test.go).

  t-3  Add clear-point footer to 7 boundary prompts
       files:    assets/prompts/spec.md, assets/prompts/plan.md,
                 assets/prompts/execute.md, assets/prompts/verify.md,
                 assets/prompts/ship.md, assets/prompts/quick.md,
                 assets/prompts/pause.md
       covers:   c-1
       desc:     Append a clear-point footer to each command's wrap-up: the literal
                 'state is on disk — safe to /clear' plus the exact next command for a
                 fresh session. Uniform wording across all seven so the coverage gate
                 (t-4) can match one sentinel.
       contract: each of the 7 prompts contains the 'safe to /clear' footer sentinel
                 followed by a concrete `/dross-…` next command; the footer coverage test
                 (t-4) enumerates all 7 and fails if any is missing it.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Add fail-closed footer coverage test + convention
       files:    internal/cmd/footer_coverage.go,
                 internal/cmd/footer_coverage_test.go,
                 docs/interaction-audit.md
       covers:   c-1
       depends:  t-3
       desc:     Mirror the interaction-audit convention (interaction_coverage.go pattern):
                 classify every durable-boundary command as footer-bearing or
                 exempt-with-reason via a table in docs/interaction-audit.md; Go test reads
                 the doc + prompts and fails closed on any unclassified boundary command.
       contract: deleting the footer sentinel from any of the 7 prompts fails
                 TestFooterCoverageFailClosed; adding a new durable-boundary command that is
                 neither footer-bearing nor in the exempt table fails the same test
                 (footer_coverage_test.go).

  t-5  Add checkpoint option to execute per-task gate
       files:    assets/prompts/execute.md,
                 internal/cmd/execute_prompt_test.go,
                 docs/interaction-audit.md
       covers:   c-2
       depends:  t-3
       desc:     Add a 'checkpoint' choice to the pair-mode per-task gate alongside
                 continue/stop: it confirms plan.toml + state are consistent, then ends the
                 session with the re-entry command `/clear → /dross-execute --from
                 <next-task>`. Present at every task gate; recommended lead option at wave
                 boundaries (checkpoint_posture). Record the new decision point in the
                 interaction audit's dross-execute section.
       contract: execute.md's per-task gate lists a checkpoint option whose text emits the
                 `/dross-execute --from <next-task>` re-entry command; execute_prompt_test.go
                 asserts both the checkpoint option and the `--from` re-entry string are
                 present in the prompt.

  t-6  Scaffold PreCompact + SessionStart hooks in init/onboard
       files:    internal/hooks/settings.go, internal/hooks/settings_test.go,
                 internal/cmd/init.go, internal/cmd/onboard.go,
                 internal/cmd/init_test.go
       covers:   c-3, c-5
       depends:  t-1, t-2
       desc:     New internal/hooks merge helper (modeled on internal/statusline/settings.go
                 ordered-object merge): idempotently ensure a user-level PreCompact hook
                 running `dross pause --auto` and a SessionStart hook running
                 `dross status --reentry` in ~/.claude/settings.json, preserving all other
                 keys/hooks. Wire the helper into init.go and onboard.go scaffolding
                 (hook_scope: user-level, idempotent).
       contract: after `dross init`, ~/.claude/settings.json contains a PreCompact hook
                 invoking `dross pause --auto` and a SessionStart hook invoking `dross status
                 --reentry`; a second run is a byte-stable no-op and a pre-existing unrelated
                 hook survives untouched (settings_test.go + init_test.go).

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (clear-point footers + fail-closed coverage test) | t-3, t-4 |
| c-2 (execute checkpoint gate option) | t-5 |
| c-3 (`dross pause --auto` + PreCompact hook) | t-1 (CLI), t-6 (hook) |
| c-4 (`dross status` re-entry line) | t-2 |
| c-5 (SessionStart hook injects re-entry line) | t-6 |

All 5 criteria covered.

## Judgment calls

- Both hooks (PreCompact + SessionStart) into ONE task (t-6), not two: identical
  mechanism (settings.json ordered-merge) and identical wiring points (init +
  onboard). Splitting would duplicate the merge helper and the cmd wiring. Rejected a
  per-hook split.
- All 7 footers into ONE task (t-3), not per-command: it's a single uniform
  content edit and a single footer wording the coverage gate matches. Touches 7 files
  but one layer, low complexity — the size guideline is about complexity, not raw
  file count.
- Kept the footer coverage test (t-4) separate from the footers (t-3) instead of
  merging: merging makes a 9-file task spanning content + Go gate + doc, and the test
  strictly needs the footers present to pass. The gate/content split is the natural
  cut and gives a clean wave dependency. Rejected the single mega-task.
- Put `dross status --reentry` (the SessionStart emitter) in t-2 with the c-4
  re-entry line, not in t-6: it's a two-line addition to the same status surface, and
  co-locating keeps the re-entry wording single-sourced. t-6 only references the
  command string.
- t-5 and t-4 depend on t-3 (wave 2): both build on the footer'd prompts —
  t-4 gates them, t-5 edits a different region of execute.md that t-3 also touches, so
  serialize. Rejected forcing them to wave 1 to avoid the shared-file edit racing.
- No separate `internal/handoff` package for t-1: the auto-snapshot merge is a
  self-contained function that lives in pause.go and is unit-tested there. A package
  would be speculative structure for one caller.
