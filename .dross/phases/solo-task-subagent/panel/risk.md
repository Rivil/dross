# solo-task-subagent — RISK lens draft

**Time-critical:** the v1.7 baseline transcripts (`6b4c5e5e…` solo-task-review, `08853456…` board-finalize + verify-staleness, `7e2115d6…` debug-sessions, `5036b126…` review-comment-ingest; 2026-10-04..07) sit under Claude Code's default 30-day cleanup (`cleanupPeriodDays` is unset in `~/.claude/settings.json`). They start disappearing around **2026-11-03**. t-7 must run before then, or the user must raise `cleanupPeriodDays` / copy the four files (and their `subagents/` dirs) to a gitignored location first. The `baseline` decision is locked, so losing these files means re-deciding it.

```
Phase solo-task-subagent — 16 tasks across 5 waves

Wave 1
  t-1  Capture live writer hook payloads
       files:    internal/gate/testdata/writer_pre_edit.json (new), internal/gate/testdata/writer_pre_bash.json (new),
                 internal/gate/testdata/writer_spawn_pre.json (new), internal/gate/testdata/writer_spawn_post.json (new),
                 internal/gate/testdata/writer_subagent_stop.json (new), internal/gate/testdata/writer_resume_post.json (new),
                 internal/gate/testdata/main_pre_edit.json (new), internal/gate/fixture_test.go
       desc:     Go/no-go capture via headless `claude -p` in a scratch repo, with a temporary stdin-dump hook in the scratch
                 repo's .claude/settings.local.json (never ~/.claude). Spawn a background scratch writer that does one Edit and
                 one Bash, then resume it once with SendMessage. Pin each payload byte for byte. Record the main-loop token cost
                 of one spawn plus its report (spawn_overhead_tokens) in a capture note inside fixture_test.go.
       covers:   c-1, c-2, c-5
       contract: if writer_pre_edit/pre_bash lack agent_id or agent_type == "dross-task-writer", TestCapturedWriterCallsCarryAgent
                 fails. This is the NO-GO signal: the fence (t-9) cannot tell writer from main loop, so stop and re-decide agent_gate.
                 if main_pre_edit carries a non-empty agent_id, TestCapturedMainLoopHasNoAgent fails (the takeover guard would misfire).
                 if writer_spawn_post's tool_response lacks agentId or status async_launched, TestCapturedWriterLaunch fails.
                 if the resumed writer's stop payload carries a different agent_id than its launch, TestCapturedWriterResume fails.
                 This decides the fix round (see Judgment calls).
                 if any fixture lacks session_id/transcript_path or a toolu_ tool_use_id, the shared real-capture check fails.

  t-2  Mine per-task metrics from transcripts
       files:    internal/killswitch/transcript.go (new), internal/killswitch/transcript_test.go (new),
                 internal/killswitch/testdata/split_message.jsonl (new), internal/killswitch/testdata/compacted.jsonl (new),
                 internal/killswitch/testdata/two_sessions_a.jsonl (new), internal/killswitch/testdata/two_sessions_b.jsonl (new)
       desc:     Mine(phase, transcripts...) → per-task rows {task, attempts, start, end, wall_clock_s, turns, ctx_start,
                 ctx_peak, growth, compacted}. A task window runs from `dross task status <p> <t> in_progress` to that task's
                 next done/failed call. Context = input + cache_read + cache_creation of main-agent messages only. Only numbers,
                 timestamps and tool names are kept; no content.
       covers:   c-6
       contract: one assistant message split over 3 jsonl lines (same message.id) counts as 1 turn, and its usage counts once
                 (TestMineDedupesSplitMessage).
                 a message with isSidechain:true carrying 900k-token usage leaves ctx_peak unchanged (TestMineExcludesSidechain).
                 a task spanning a compact_boundary reports growth = Σ per-segment (peak − segment start), never a negative number,
                 and is flagged compacted (TestMineCompactedTask).
                 a task re-entered in_progress after `failed` is one row with attempts=2 and summed wall-clock (TestMineRetriedTask).
                 a phase split over two transcripts by /clear, with one message uuid duplicated across both (resume), is mined
                 once per uuid (TestMineAcrossSessions).
                 an assistant message with no usage block errors naming the line; it is never counted as 0 (TestMineMissingUsage).
                 a fixture whose message text holds CANARY-7f3a has no "CANARY" in any mined field (TestMineCopiesNoContent).
                 a task with in_progress but no terminal status is reported as unterminated and excluded from the medians.

  t-3  Parse the bounded writer report and ledger
       files:    internal/writer/report.go (new), internal/writer/report_test.go (new), internal/writer/ledger.go (new),
                 internal/writer/ledger_test.go (new), internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     WriterAgent = "dross-task-writer". ParseReport reads exactly one ```dross-writer-report JSON fence
                 {status green|red|error, files[], test{exit, summary}, notes}, enforcing a cap on the whole final message
                 (4 KB) and a 500-char cap on notes. Ledger state machine for one attempt: none → running(agent_id) →
                 returned(green|red|error|unavailable) → fixing (at most once) → returned. A second agent_id → conflict.
       covers:   c-2, c-5
       contract: zero fences, two fences, malformed JSON, unknown status, empty files on green, a message over 4096 bytes,
                 notes over 500 chars → each is unavailable, never green (TestParseReportRejects).
                 a final message carrying `diff --git`, a `@@ -` hunk header or a `+++ b/` line is unavailable even when
                 under the cap; a writer that pastes its diff fails c-2 (TestReportRefusesDiff).
                 a report listing a file outside the task's files is status deviation, not green (TestReportFilesWithinTask).
                 ledger: a second fix round, a fix round from returned(red), or a transition out of a terminal state errors
                 (TestLedgerOneFixRound). A running record seeing a second agent_id becomes conflict → unavailable
                 (TestLedgerConflict).
                 an empty LastMessage (writer died, max turns) is unavailable with cause "writer returned nothing" (TestEmptyReport).

  t-4  Amend the dross-agent-gate builtin
       files:    internal/rules/rules.go, internal/rules/rules_test.go, docs/subagent-offload-audit.md
       desc:     Under /dross-execute --solo only, one dross-task-writer per task may edit that task's files. It never commits,
                 sets task status, writes .dross/ or finalizes. Read-only fan-out text and the pair sentence stay unchanged.
                 /dross-quick --solo is named as excluded. The audit doc's `### execute` gains the writer disposition.
       covers:   c-1, c-4
       contract: if the rendered builtin stops naming --solo, "one", dross-task-writer, commit, task status and finalize together,
                 or drops the /dross-quick exclusion, TestAgentGateWriterClause fails.
                 if the pair-mode sentence ("in pair mode, get explicit user approval before writing code or finalizing") changes
                 by a byte, TestAgentGatePairSentenceStable fails (c-4).
                 if the offload audit's execute section loses the writer disposition, the subagent offload audit test fails.

  t-6  Compute task surface and the writer context
       files:    internal/writer/dispatch.go (new), internal/writer/dispatch_test.go (new), internal/writer/context.go (new),
                 internal/writer/context_test.go (new)
       desc:     Surface(root, rev, files) = file count + total bytes of task.files at a commit, read with `git cat-file -s`.
                 A new file counts 0 bytes. Paths go through pathfence. Decide(surface, threshold, verdict) → writer | inline,
                 with the reason. BuildContext renders the task record, covered criteria, locked decisions, hard rules and the
                 report contract to a context file with a digest. Never the diff or file contents.
       covers:   c-2, c-6
       contract: a dirty working-tree edit to a task file does not change Surface at HEAD (TestSurfaceReadsCommit).
                 a "../x" or absolute path outside root errors and is never treated as 0 (TestSurfaceFencesPaths).
                 a directory entry or a symlink in task.files errors (TestSurfaceRejectsNonFiles).
                 Decide at threshold-1 is inline and at threshold is writer (TestDecideBoundary).
                 a verdict of fail, unjudged or unreadable is inline for a surface of 10 MB (TestDecideFailsClosedToInline, c-6).
                 if BuildContext output contains any line of a task file's contents or a diff marker, TestContextHasNoCode fails.
                 two builds on the same inputs give the same digest (TestContextDigestStable).

Wave 2
  t-5  Store the writer record
       depends:  t-3
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     .dross/gate/writer.json {phase, task, attempt, agent_id, digest, state, fix_rounds, started_at, stopped_at,
                 report} plus writer-context.md, written with the store's atomic temp+rename. Missing = none. Corrupt = error
                 naming the path. A git-tracked file is refused unread. Clear removes it.
       covers:   c-2, c-5
       contract: a truncated writer.json decodes to an error naming .dross/gate/writer.json, never a zero record; missing →
                 (nil, nil) (TestWriterRecordDecode).
                 N concurrent SaveWriter writers (SubagentStop recorder vs `dross writer status` reader) never yield a torn read
                 (TestWriterRecordAtomic).
                 a writer.json tracked by git is refused unread (TestWriterRecordTrackedRefused).
                 a running record with an empty agent_id, or a returned record with a zero stopped_at, is refused on save
                 (TestWriterRecordInvariants).

  t-7  Record the v1.7 baseline; calibrate the gate
       depends:  t-1, t-2, t-6
       files:    internal/killswitch/record.go (new), internal/killswitch/record_test.go (new),
                 .dross/phases/solo-task-subagent/kill-switch.toml (new), internal/cmd/killswitch_record_test.go (new)
       desc:     Mine the four baseline transcripts into [before] rows, one per solo task of the 5 phases. Each row carries the
                 task's Surface at its parent commit. Calibrate: threshold = smallest surface whose ≥-threshold tasks' median
                 growth exceeds 2 × t-1's spawn_overhead_tokens. Record the derivation, the sources' session ids + sha256 and the
                 jq-free method. Judge(before, after) applies the locked thresholds to the above-gate tasks only. The trial is one
                 named phase; an unjudged trial past that phase reads as "unjudged".
       covers:   c-6
       contract: if kill-switch.toml lacks a [before] row for any task marked done in the five baseline phases' plan.toml, or a
                 row has turns/wall_clock_s/ctx_peak ≤ 0, TestKillSwitchBaselineComplete fails.
                 if the recorded threshold differs from Calibrate(rows, overhead) recomputed from the file, TestThresholdReDerivable
                 fails (the gate number cannot drift from its data).
                 Judge: peak drop exactly 25.0% with wall-clock +25.0% → pass; drop 24.9% → fail; wall-clock +25.1% → fail; turns
                 +300% alone → still pass (recorded, not gating) — TestJudgeBoundaries.
                 an even task count uses the mean of the middle two; a zero baseline median errors instead of dividing
                 (TestJudgeMedianEdges).
                 an after leg whose above-gate tasks number 0 is "insufficient", never pass (TestJudgeNoWriterTasks).
                 with no [after] leg, Verdict reads "trial" for the named trial phase and "unjudged" for any later phase
                 (TestTrialIsOnePhase).

  t-15 Ship the writer agent definition
       depends:  t-1, t-3
       files:    assets/agents/dross-task-writer.md (new), internal/writer/agentdef_test.go (new), assets/embed_test.go
       desc:     Frontmatter: name dross-task-writer, tools Read, Grep, Glob, Edit, Write, Bash. No model key (inherits). No
                 omitClaudeMd: the writer follows project conventions. Body: read the named context file (on a digest mismatch,
                 reply `stale context` with no report). Edit only the task's files and never .dross/. Run tests only with
                 `dross test --files`. Never commit, stage, stash, set task status or record changes. One bounded fix attempt on
                 red. End with exactly one dross-writer-report fence and nothing pasted from the diff.
       covers:   c-1, c-2
       contract: if `tools` gains Agent, Task, WebFetch or WebSearch, or is dropped (inherit-all), TestWriterDefinitionTools fails.
                 if a `model` key appears with any value, or `memory` appears, TestWriterInheritsModel fails (locked writer_spawn).
                 if the body's worked example fails writer.ParseReport, or exceeds the report cap, TestWriterExampleParses fails.
                 if the frontmatter name drifts from writer.WriterAgent, TestWriterNameMatches fails.
                 if the body stops forbidding git commit, `dross task status` and .dross/ writes, TestWriterBodyForbids fails.

Wave 3
  t-8  Gate the writer spawn; record its lifecycle
       depends:  t-1, t-3, t-5, t-6, t-7
       files:    internal/gate/writer.go (new), internal/gate/writer_test.go (new), README.md
       desc:     PreToolUse on Agent with subagent_type dross-task-writer refuses unless: execute.json is solo for current_phase,
                 exactly one task is in_progress, HEAD is on phase/<id> and no solo quick is armed. It also refuses unless
                 run_in_background is true with no model override, the prompt is the printed line verbatim with a matching
                 digest, Decide says writer, and the ledger shows no writer for this attempt. PostToolUse binds the launch's
                 agentId. SubagentStop parses the report into the ledger and ignores unbound agent_ids. README gets a gate row.
       covers:   c-1, c-2, c-4, c-5
       contract: from writer_spawn_pre.json in a pair-mode fixture repo (execute.json pair, or absent) the spawn is refused, naming
                 --solo; the same spawn with a solo quick armed is refused naming /dross-quick (TestWriterSpawnSoloOnly, c-4).
                 a second spawn for the same (phase, task, attempt) after one was bound is refused naming "one writer per task";
                 a spawn after a .dross/-only commit counts as the same attempt (TestOneWriterPerTask, c-1).
                 two launches bound in one batch (both PreToolUse allowed before either PostToolUse) leave the ledger in conflict
                 and `dross writer status` unavailable, never two running writers (TestParallelSpawnConflict).
                 run_in_background false/absent, or a `model` key, is refused (TestWriterSpawnShape).
                 a prompt carrying the printed line plus any extra text is refused (TestWriterPromptVerbatim, c-2).
                 a spawn for a task whose Decide reads inline, or while the verdict is fail/unjudged, is refused (TestSpawnHonoursGate).
                 SubagentStop from writer_subagent_stop.json with a 5 KB message records unavailable; one with an agent_id that is not
                 the bound one changes nothing (TestWriterStopRecorder).
                 a general-purpose or dross-task-reviewer spawn is not claimed by this gate (TestWriterGateClaimsOnlyWriter).

  t-9  Fence the writer; refuse inline takeover
       depends:  t-1, t-5
       files:    internal/gate/writerfence.go (new), internal/gate/writerfence_test.go (new)
       desc:     Calls carrying agent_type dross-task-writer are limited to these. Edit/Write: only the bound task's files, never
                 .dross/. Bash allowlist: `dross test`, `dross validate`, read-only git (status/diff/log/show), `go vet`/`go build`/
                 `gofmt -l`. Any call from an agent_id other than the bound running one is refused. Main-loop calls (no
                 agent_id) are refused while the armed task is writer-owned: Edit/Write outside .dross/, and while running also
                 `dross test`, `dross review context`, git add/commit and `dross task next`. Stash and `task status … failed`
                 stay open.
       covers:   c-1, c-3, c-5
       contract: from writer_pre_bash.json rewritten to `git commit -m x`, `git stash`, `dross task status p t-3 done`,
                 `dross changes record …`, `dross phase complete …` or `gh pr create` → each refused naming the agent gate
                 (TestWriterNeverCommitsOrFinalizes, c-1).
                 a writer Edit to .dross/phases/<id>/plan.toml is refused, as is any file outside the task's files
                 (TestWriterFileFence). That is the status backdoor.
                 a writer Edit carrying a stale agent_id (task already failed, or a conflict loser) is refused (TestZombieWriterFenced).
                 from main_pre_edit.json with the armed task writer-owned and returned red, a main-loop Edit to a task file is
                 refused naming "fail it through the solo red path" (TestNoInlineTakeover, c-5).
                 while the writer is running, main-loop bare `dross test` and `git commit` are refused, and
                 `git stash push -u -- . ':(exclude).dross'` plus `dross task status p t-3 failed` pass (TestMainLoopWaitsForWriter).
                 a Bash line that hides `git commit` behind `sh -c`, `eval` or a partial parse is refused, never allowed (TestFenceFailsClosedOnOpaqueShell).
                 with no writer record, every call the fence would claim passes untouched (TestFenceSilentUnarmed). Pair mode is
                 byte-identical.

  t-10 Require main-loop green after the writer
       depends:  t-5
       files:    internal/gate/commit.go, internal/gate/commit_test.go, internal/gate/soloreview.go, internal/gate/soloreview_test.go
       desc:     For a writer-owned task, commit-green admits only a green.json whose At is after the writer's stopped_at, and
                 solo-review only a pass whose review started after it. A commit whose candidate touches code outside the task's
                 files is refused as a deviation.
       covers:   c-3
       contract: a green.json recorded on exactly the candidate tree but before the writer's stopped_at (the writer's own
                 `dross test`) refuses the commit, naming "re-run the bare `dross test` in the main loop" (TestWriterGreenCertifiesNothing, c-3).
                 the same green recorded after stopped_at admits it (TestMainLoopGreenAdmits).
                 a candidate including a code path outside task.files refuses as a deviation (TestWriterDeviationRefused).
                 for an inline (non-writer) solo task, and in pair mode, commit-green behaves exactly as today: the existing
                 TestCommitGreen*/TestSoloReviewGate tables pass unchanged.

  t-12 Fail writer tasks through the red path
       depends:  t-5
       files:    internal/cmd/task.go, internal/cmd/review_attach.go, internal/cmd/writer_attach_test.go (new)
       desc:     `dross task status <p> <t> failed` without --reason derives it from a writer record that is red, error,
                 unavailable, conflict or running-with-no-stop ("writer returned nothing"), one line, newline-stripped. It then
                 clears the binding so the fence refuses the old agent_id. It still refuses while code is uncommitted.
       covers:   c-5
       contract: for each writer state red/error/unavailable/conflict/running, `failed` with no --reason stores a one-line reason
                 naming that state, and the writer's notes never exceed 200 chars in it (TestWriterFailureReason).
                 if `failed` succeeds with the writer's edits still uncommitted and unstashed, TestWriterFailRequiresStash fails.
                 after `failed`, writer.json has no bound agent_id (TestFailedClearsWriterBinding), so t-9's zombie fence applies.

  t-11 Add writer prompt, status and measure verbs
       depends:  t-5, t-6, t-7
       files:    internal/cmd/writer.go (new), internal/cmd/writer_test.go (new), cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       desc:     `dross writer prompt <p> <t>` prints `inline (<reason>)`, or writes writer-context.md and prints subagent_type
                 plus the exact prompt line. It never prints task files or a diff. `dross writer status` prints the bounded
                 record (state, files, test exit, one-line notes, dirty paths outside task.files). `dross writer measure --leg
                 before|after --phase <id> --transcript <path>...` writes kill-switch.toml. Transcripts are named explicitly,
                 never globbed.
       covers:   c-2, c-6
       contract: if `writer prompt`'s printed digest differs from writer.BuildContext's digest on the same repo,
                 TestWriterPromptDigest fails (the t-8 spawn gate would refuse every printed line).
                 if stdout of `writer prompt` or `writer status` carries any line of a task file or a diff marker,
                 TestWriterVerbsPrintNoCode fails (c-2).
                 a below-threshold task, a fail/unjudged verdict, pair mode and a solo quick each print `inline (…)` with exit 0 and
                 write no context file (TestWriterPromptInline, c-4, c-6).
                 `writer status` after a green report but with an extra dirty code path prints deviation, not green
                 (TestWriterStatusDeviation).
                 `writer measure` given a transcript path outside $HOME/.claude/projects or a missing file errors before writing
                 anything, and a re-run leaves kill-switch.toml byte-identical (TestMeasureIdempotent).
                 cli_tree.txt pins the three verbs.

  t-13 Install the writer and report readiness
       depends:  t-15
       files:    internal/cmd/install.go, internal/cmd/reviewer_agent.go, internal/cmd/install_agent_test.go
       desc:     Generalise reviewerStatusIn into an agent readiness check for both dross-task-reviewer and dross-task-writer
                 (ok/missing/stale/shadowed). install copies or links both. Pruning dropped dross-*.md never touches foreign
                 agents.
       covers:   c-2
       contract: if install leaves dross-task-writer.md absent or byte-different from the embedded one (copy or link mode),
                 TestInstallWriter fails.
                 a repo-level .claude/agents/dross-task-writer.md reads shadowed, and a dangling link reads missing
                 (TestWriterReadiness).
                 the existing reviewer readiness tests pass unchanged after the generalisation.

Wave 4
  t-14 Gate solo begin on the writer; doctor reports
       depends:  t-7, t-13
       files:    internal/cmd/execute.go, internal/cmd/solo_begin_test.go, internal/cmd/doctor.go,
                 internal/cmd/doctor_writer_test.go (new), internal/cmd/testdata/cli_surface/doctor_run.txt
       desc:     `execute begin --solo` refuses while the verdict is trial/pass and the writer is not ok, naming `dross install`. A
                 fail verdict does not need the writer. Pair begin is unchanged. Doctor gets a Writer section with readiness plus
                 the kill-switch state ("trial for <phase>: awaiting `dross writer measure --leg after`", "unjudged → inline",
                 "fail → inline").
       covers:   c-4, c-6
       contract: `execute begin p --solo` with the writer missing/stale/shadowed and a trial verdict errors naming `dross install`;
                 with a fail verdict it succeeds; `execute begin p` (pair) succeeds in every case (TestSoloBeginNeedsWriter).
                 doctor exits non-zero with the writer stale under a trial verdict and names the measure command for an unjudged
                 trial (TestDoctorWriter). doctor_run.txt pins the section.

Wave 5
  t-16 Dispatch writers in the solo prompt
       depends:  t-8, t-9, t-10, t-11, t-12, t-14
       files:    assets/prompts/_execute.solo.md, assets/prompts/_execute.recovery.md, internal/cmd/execute_prompt_test.go,
                 internal/cmd/quick_prompt_test.go, internal/cmd/testdata/execute_base_pair.golden (new)
       desc:     Solo partial: at §1a after `task show`, run `dross writer prompt`. On `inline`, carry on with today's §1b–§1e.
                 On a writer line, skip §1b–§1e and spawn in the background with the line verbatim. Wait for the completion
                 notice, then `dross writer status`. green → §1f bare `dross test`, review, commit. Review blocked → one fix round
                 via SendMessage to the same writer (load SendMessage with ToolSearch first; if unavailable, fail).
                 red/error/unavailable/nothing/spawn error → stash, then `failed`. Never Read the task's files or show `git
                 diff` on this path. Recovery partial: a writer record with no stop on resume → stash + failed. Run `make
                 install` before relying on it (r-01).
       covers:   c-2, c-3, c-4, c-5
       contract: if, in the solo partial's writer branch, `dross writer prompt` stops preceding the spawn, the spawn loses
                 run_in_background or the verbatim-line rule, or `dross writer status` stops preceding §1f, TestSoloWriterDispatchOrder
                 fails (c-2).
                 if the writer branch tells the main loop to Read a task file, run `git diff`, or do §1b/§1d itself,
                 TestWriterBranchKeepsCodeOut fails (c-2).
                 if bare `dross test`, `dross review context` and `git commit` stop following `dross writer status`,
                 TestGatesStayInMainLoop fails (c-3).
                 if any failure arm (red, error, unavailable, returned nothing, spawn error, review exhausted) offers anything but
                 stash then `failed`, or offers "fix inline", TestWriterFailurePath fails (c-5).
                 if the pair expansion of execute.md (CommonPath) differs by a byte from execute_base_pair.golden, captured at base
                 commit 29fc87db, or mentions dross-task-writer, TestPairExecuteByteIdentical fails (c-4).
                 if quick.md gains dross-task-writer or `dross writer`, TestQuickStaysInline fails (locked quick_solo).
                 if the recovery partial lacks the writer-in-flight arm (stash + failed, never resume inline),
                 TestRecoveryWriterInFlight fails.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (agent gate: one writer per task under --solo, never commits/status/finalizes) | t-1, t-4, t-8, t-9, t-15 |
| c-2 (size-gated writer from a printed line; main loop sees only the bounded report) | t-1, t-3, t-5, t-6, t-8, t-11, t-13, t-15, t-16 |
| c-3 (bare `dross test`, reviewer and commit stay in the main loop; writer green certifies nothing) | t-9, t-10, t-16 |
| c-4 (spawn refused outside --solo; pair byte-for-byte) | t-4, t-8, t-11, t-14, t-16 |
| c-5 (errored/red/empty writer → stash + failed; no inline takeover) | t-1, t-3, t-5, t-8, t-9, t-12, t-16 |
| c-6 (kill switch: baseline, after leg, thresholds, revert to inline) | t-2, t-6, t-7, t-11, t-14 |

6/6 criteria covered.

### Risk register (each risk → exactly one owning task)

| Risk | Owner |
|---|---|
| Subagent PreToolUse payloads carry no agent_id, so writer and main loop are indistinguishable | t-1 (no-go) |
| Baseline transcripts expire around 2026-11-03 | t-7 (run first in wave 2) |
| Split messages, sidechains, compaction, /clear and resume corrupt the metrics | t-2 |
| Transcript content (secrets) leaks into the tracked record | t-2 |
| Writer pastes its diff or files into its report, so the main loop sees code anyway | t-3 |
| Writer dies, hits max turns or returns empty | t-3 (parse), t-12 (reason) |
| Torn or corrupt writer.json between the recorder and the status read | t-5 |
| Size gate depends on dirty-tree state, or escapes the repo via a path | t-6 |
| Gate number drifts from its data | t-7 |
| Trial never judged, so the writer ships unjudged | t-7 (one-phase trial), t-14 (doctor surfaces it) |
| Pair-mode or quick spawn of a writer | t-8 |
| Two writers for one task, including parallel spawns in one batch | t-8 |
| Writer commits, stashes, sets status, or edits plan.toml (status backdoor) | t-9 |
| Zombie writer edits after its task failed | t-9 |
| Main loop takes over inline after a compaction mid-writer | t-9 |
| Writer's own `dross test` (the dross repo has no lanes) records green.json for the tree | t-10 |
| Writer touches files outside task.files via Bash | t-10 (commit) and t-11 (status) — one surface each |
| Failed reason pasted from unbounded writer text | t-12 |
| Writer definition widens tools or pins a weaker model | t-15 |
| Solo begin with the writer missing or stale | t-14 |
| Main loop still does §1b reading before dispatch, so nothing is shed | t-16 |
| Session dies mid-writer, and resume takes the task over | t-16 (recovery partial) |

## Judgment calls

- **Fix round on a review-blocked writer task:** I chose to resume the same writer via SendMessage, which keeps it "one writer per task". If t-1 shows the resumed agent keeps neither its agent_id nor its SubagentStop, the task fails with no fix round. I rejected an inline main-loop fix (it violates c-5's "never takes the task over inline") and a second spawn (it violates c-1's "one writer").
- **Writer green certifies nothing (c-3):** I chose time-ordering: a commit needs a green.json with At after the writer's stopped_at. I rejected suppressing the writer's green recording, because `dross test` writes green.json itself and subagents share env and session id (memory: CHILD_SESSION ≠ subagent), so the process cannot tell who called it.
- **Parallel-spawn race:** I chose to detect at PostToolUse binding (conflict → unavailable → task fails). I rejected a PreToolUse reservation, because a reservation orphaned by another gate's refusal would block the legitimate spawn forever.
- **Fence keys on payload agent_id/agent_type, gated by a go/no-go capture:** I rejected env-var or session-id detection (proven unreliable 2026-10-09). I rejected a Bash-less writer: it could not report a test outcome (c-2), and c-5's "returns red" presumes it runs tests.
- **Kill-switch metric:** the per-task main-loop context *growth* (peak − start, summed across compaction segments), compared on above-gate tasks only. I rejected the raw phase peak, which saturates at the compaction ceiling in 2 of 4 baseline transcripts. I rejected all-task medians, because small inline tasks dilute the effect either way.
- **Trial lasts exactly one named phase; unjudged after that → inline:** I rejected an open-ended "trial until measured", which lets the writer ship without the locked threshold check.
- **Unreadable kill-switch or verdict fail → inline, never an error that blocks solo:** the failure mode of the kill switch is today's behaviour, not a stalled run.
- **Transcript miner shipped as Go (`internal/killswitch` + `dross writer measure`), not a jq method like flow-cost.toml:** compaction segments, message-id dedupe and cross-session uuids each need a unit test. The after leg must be re-mined later by someone who wasn't here.
- **Size gate measured at the task's start commit (`git cat-file -s`), not the working tree:** it is deterministic and replayable for the baseline rows at their parent commits. That is what makes the threshold re-derivable in t-7.
- **Calibration rule = 2 × the measured spawn+report overhead:** the factor of 2 is my choice. I rejected planner-asserted prose numbers, because the dispatch decision says the planner calibrates from data, and a test re-derives the number from the record.
- **Writer keeps CLAUDE.md (no omitClaudeMd), unlike the reviewer:** the writer has to follow project conventions. Isolation was the reviewer's purpose, not the writer's.
- **The pair byte-identity check is a golden of execute.md's CommonPath expansion at base commit 29fc87db:** all writer prose lives in `_execute.solo.md` and `_execute.recovery.md`. I rejected adding a §1b fetch line to execute.md, which would break c-4's byte-for-byte guarantee.
