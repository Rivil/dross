# solo-task-subagent — MVP lens

```
Phase solo-task-subagent — 5 tasks across 3 waves

Wave 1
  t-1  Record the v1.7 kill-switch baseline
       files:    .dross/phases/solo-task-subagent/kill-switch.toml (new),
                 internal/cmd/killswitch_record_test.go (new)
       desc:     Mine the v1.7 solo transcripts (solo-task-review, board-finalize-on-complete,
                 verify-staleness, debug-sessions, review-comment-ingest) with a jq filter recorded in the
                 file (flow-cost.toml pattern): main-loop entries only (assistant, isSidechain false, model
                 not <synthetic>), segmented per task from `dross task status <phase> <t> in_progress` to
                 its done|failed. Per [[before.task]] row: peak_context (max input+cache_read+cache_creation),
                 context_growth (peak − first in segment), wall_clock_s, turns (distinct message.id),
                 surface_lines (task.files line count at the task commit's parent, via changes.json).
                 [thresholds] context_drop = 0.25, wall_clock_rise = 0.25; [calibration] surface_lines =
                 median before surface_lines (≈1,380 on today's file sizes; the parent-commit count decides).
                 Counts only — no transcript content copied.
       covers:   c-6
       contract: a v1.7 solo phase with no [[before.task]] row, or a row with zero peak_context /
                 wall_clock_s / turns, fails TestKillSwitchBaselineRecorded;
                 [thresholds] other than 0.25 drop / 0.25 rise, or a turn-count threshold, fails
                 TestKillSwitchThresholdsLocked;
                 [calibration].surface_lines ≠ the median of the before rows' surface_lines fails
                 TestKillSwitchCalibrationDerived

  t-2  Ship the dross-task-writer definition
       files:    assets/agents/dross-task-writer.md (new), internal/review/writer.go (new),
                 internal/review/writer_test.go (new), internal/cmd/install_agent_test.go
       desc:     Agent definition: tools Read, Edit, Write, Grep, Glob, Bash; no `model:` (inherits the
                 session model); reads its task via `dross task show`, touches only task.files, runs
                 `dross test --files …`; never git add/commit/stash, never `dross task status`,
                 `changes record`, `phase`, `ship`, `issue`; ends with one fenced dross-writer-report
                 (files touched, `test: green|red` + first failing test, notes ≤5 lines, no code, no diff).
                 writer.go holds WriterAgent and WriterPromptLine(phase, task). syncAgents already installs
                 every embedded dross-* agent — no install code change.
       covers:   c-1, c-2
       contract: a `model:` line in the frontmatter, or Edit/Write missing from tools, fails
                 TestWriterDefinitionShape;
                 a body that stops forbidding git commit, `dross task status` or the finalize verbs fails
                 TestWriterNeverCommitsOrDecides;
                 a report spec that drops files/test/notes or permits pasting code or the diff fails
                 TestWriterReportBounded;
                 `dross install` (syncAgents) not writing dross-task-writer.md beside the reviewer fails
                 TestSyncAgentsInstallsWriter

Wave 2 (depends t-1, t-2)
  t-3  Gate writer spawns to solo tasks           (depends t-2)
       files:    internal/rules/rules.go, internal/rules/rules_test.go,
                 internal/gate/writer.go (new), internal/gate/writer_test.go (new)
       desc:     Amend the dross-agent-gate builtin text: under --solo only, one writer subagent per task
                 may edit; it never commits, sets task status or finalizes. New Workflow, liftable gate
                 "solo-writer": claims Agent/Task calls with subagent_type dross-task-writer; admits only
                 when ArmedScope is a solo execute task (Kind task, not quick) and the prompt is exactly
                 WriterPromptLine(phase, task) for that task; refuses otherwise with rule + remedy.
       covers:   c-1, c-4
       contract: a writer spawn with no execute mode recorded, pair mode with a task in progress, a solo
                 quick armed, or solo execute with no task in progress is refused —
                 TestWriterSpawnRefusedOutsideSolo;
                 solo execute with t-3 in progress admits WriterPromptLine(phase,"t-3") but refuses a
                 prompt naming t-4 or with text appended — TestWriterSpawnBoundToArmedTask;
                 an Explore or dross-task-reviewer spawn is not claimed — TestWriterGateIgnoresOtherAgents;
                 builtin text losing "--solo", "one writer subagent per task" or "never commits, sets task
                 status or finalizes" (or the existing "writing and deciding stay gated") fails
                 TestAgentGateAllowsSoloWriter

  t-4  Add the dross task dispatch size gate     (depends t-1, t-2)
       files:    internal/cmd/task_dispatch.go (new), internal/cmd/task.go,
                 internal/cmd/task_dispatch_test.go (new), cmd/dross/testdata/cli_tree.txt
       desc:     `dross task dispatch <phase> <task-id>`: surface = summed line counts of the task's
                 task.files at HEAD (missing/new files count 0). surface ≥ writerSurfaceLines and
                 writerEnabled → prints `dispatch: writer`, `subagent_type: dross-task-writer`,
                 `prompt: <WriterPromptLine>`; otherwise `dispatch: inline (surface N < T)` and no prompt.
                 Exits non-zero unless a solo execute run has that task in progress. writerEnabled is the
                 kill switch.
       covers:   c-2, c-4, c-6
       contract: a task at threshold−1 lines prints dispatch: inline and no prompt line; at threshold it
                 prints writer plus the exact WriterPromptLine; new files add 0 — TestDispatchSizeGate;
                 a pair-mode run or a task not in_progress exits non-zero with no prompt printed —
                 TestDispatchRefusesOutsideSolo;
                 writerEnabled = false dispatches a 10,000-line task inline — TestDispatchKillSwitchForcesInline;
                 writerSurfaceLines ≠ kill-switch.toml [calibration].surface_lines fails
                 TestDispatchThresholdMatchesBaseline;
                 kill-switch.toml carrying [after] rows whose median per-task context_growth dropped < 25%
                 or whose median wall_clock_s rose > 25% vs [before], while writerEnabled is true, fails
                 TestKillSwitchVerdictHolds

Wave 3 (depends t-3, t-4)
  t-5  Route large solo tasks to the writer
       files:    assets/prompts/_execute.solo.md, internal/cmd/execute_prompt_test.go,
                 docs/subagent-offload-audit.md
       desc:     Solo partial: after §1a run `dross task dispatch`; inline → today's §1b–§1e unchanged;
                 writer → skip §1b–§1d, spawn with the printed subagent_type and prompt verbatim,
                 run_in_background, no model override, wait for its completion, read only its
                 dross-writer-report (never Read task.files, never `git diff`). Spawn error, `test: red`
                 or no report → stash, `failed --reason "writer: …"`, next task — never fix inline.
                 Green → §1f in the main loop: bare `dross test`, reviewer, commit (the writer's green
                 certifies nothing). A blocking review's one fix round goes back to the same writer by
                 SendMessage with the findings; a red or missing reply fails the task. Doc's execute
                 section records the solo writer as the one sanctioned editing subagent.
       covers:   c-2, c-3, c-4, c-5
       contract: a partial that spawns without first calling `dross task dispatch`, or stops requiring
                 the printed prompt verbatim in a background spawn, fails TestSoloWriterDispatch;
                 a writer-green path that skips bare `dross test` or the reviewer before commit, or calls
                 the writer's report sufficient, fails TestSoloWriterGatesStayInMainLoop;
                 a writer path that lets the main loop Read task.files or run `git diff` fails
                 TestSoloWriterReportOnly;
                 an error / red / empty-report path that stops stashing before `failed --reason`, or that
                 allows an inline takeover, fails TestSoloWriterFailurePath;
                 execute.md, quick.md or any non-solo partial mentioning dross-task-writer or
                 `task dispatch` fails TestPairPromptHasNoWriter
```

## Coverage

- c-1 → t-2 (writer definition forbids commit/status/finalize), t-3 (builtin amended + spawn gate)
- c-2 → t-2 (bounded report spec), t-4 (size-gated dispatch + dross-printed prompt line), t-5 (main loop reads only the report)
- c-3 → t-5 (bare `dross test`, reviewer, commit stay in the main loop after the writer returns; existing commit-green and solo-review gates already refuse a commit without them)
- c-4 → t-3 (spawn refused unless solo execute task), t-4 (dispatch refuses outside solo), t-5 (no writer text reaches pair or quick prompts)
- c-5 → t-5 (error/red/empty → stash + `failed --reason`, no inline takeover)
- c-6 → t-1 (before metrics, thresholds, calibration recorded in the phase dir), t-4 (writerEnabled kill switch + verdict test that forces inline when [after] breaches the thresholds)

6/6 criteria covered.

## Judgment calls

- Kill switch = a `writerEnabled` var plus a test that fails when the recorded [after] breaches the locked thresholds while the writer is on; rejected a runtime read of a verdict file from a hard-coded phase dir — CI enforces it deterministically with no new state.
- Before metrics recorded as data + jq filter in kill-switch.toml (flow-cost.toml precedent); rejected a `dross solo metrics` transcript-parsing command — one-off measurement, the 'after' run reuses the same filter.
- Verdict compares median per-task context_growth (peak − segment start) rather than the run-wide peak; both are recorded — the locked baseline decision says per task, and run-wide peak is dominated by session length.
- Size gate = summed task.files line count at HEAD, threshold = median baseline surface (≈1,380 lines now; parent-commit counts decide); rejected file count — v1.7 tasks are 2–7 files almost uniformly, so a file count barely separates them. Rejected a hand-picked number — the spec says calibrate from baseline.
- Calibration (t-1) runs before dispatch (t-4) so the constant is derived, not guessed; this costs one wave of ordering.
- One-writer-per-task is stated in the builtin and enforced only by the dispatch→spawn binding; rejected a PostToolUse launch recorder + second-spawn refusal — no criterion asks for it to be refused, and it needs new captured fixtures.
- No Go parser for the writer report; the main loop reads the fenced report itself — nothing in dross consumes it, so a parser would be dead code.
- Writer keeps CLAUDE.md (no omitClaudeMd), unlike the reviewer: it is an implementer bound by the user's hard rules (secret handling, commit safety); isolation is the reviewer's job.
- No writer readiness check in doctor / `execute begin --solo`: syncAgents installs it, and a missing writer fails the spawn into c-5's red path. Rejected generalising reviewerReadiness — no criterion needs it.
- Review fix round goes back to the same writer by SendMessage, not inline; inline would put files in the main loop (c-2) and be the takeover c-5 forbids, and a second spawn would break "one writer per task".
- Pair byte-for-byte proved by keeping execute.md and quick.md untouched and testing that no writer text appears outside _execute.solo.md; rejected pinning execute.md's sha256 — it would break on every unrelated edit.
- Doc update to subagent-offload-audit.md folded into the prompt task rather than its own task — one paragraph.
