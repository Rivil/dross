# solo-task-subagent — panel synthesis

Checked against the repo while judging:
- `internal/gate/testdata/` holds no PreToolUse payload from inside a subagent. The only `agent_type` fixture is `reviewer_subagent_stop.json` (a SubagentStop). The capture-first claim from risk and verification stands: nothing pins that a writer's own PreToolUse carries `agent_id`/`agent_type`.
- `dross test --files` in a repo with no lanes "ignores it and runs the whole suite" (internal/cmd/test.go help text), and it writes green.json. The writer's own run therefore produces a green that commit-green would accept on the same tree. The c-3 hole that risk flagged is real. mvp's "existing gates already refuse it" is wrong.
- `syncAgents` (internal/cmd/reviewer_agent.go) already installs and prunes every embedded `dross-*` agent. risk t-13's install.go change is unnecessary.
- `dross-agent-gate` is rule text only (internal/rules/rules.go:129). No hook enforces it today. The `plan-edit` gate already refuses direct plan.toml edits, and `solo-review` already refuses any commit that has no reviewer pass.
- A SubagentStop recorder already exists for the reviewer (internal/gate/review.go), so a writer stop recorder copies a pattern that is already proven.

## Scores

| draft | dimension | score | note |
|---|---|---|---|
| mvp | criteria coverage | 3/5 | 6/6 on paper, but c-3 has a hole (writer's green.json is accepted). c-1's "one" and "never sets task status" and all of c-5 are prose-only |
| mvp | test-contract specificity | 4/5 | Named tests with boundary values (threshold−1, 10,000-line task). Prompt-text contracts are crisp |
| mvp | granularity | 5/5 | 5 tasks, each one coherent unit. t-5 bundles a lot, but it is one prompt file |
| mvp | wave correctness | 5/5 | Calibration comes before dispatch, the gate comes after the definition, the prompt comes last. No file collisions within a wave |
| risk | criteria coverage | 5/5 | Every criterion is gate-enforced, and its risk register maps each failure to one owner |
| risk | test-contract specificity | 5/5 | The sharpest edges: split messages, sidechain 900k, zombie agent_id, opaque `sh -c`, green-before-stop |
| risk | granularity | 2/5 | 16 tasks, plus ledger, compaction and cross-session miners. t-13 changes install code that already works. Calibration uses a factor of 2 the planner chose itself |
| risk | wave correctness | 4/5 | Deps are sound. Ids are out of order across waves (t-6 in wave 1, t-5 in wave 2). t-16 waits on six tasks |
| verification | criteria coverage | 5/5 | Full gate enforcement, and ClearGreen-at-stop closes c-3 by construction |
| verification | test-contract specificity | 5/5 | Measured baseline buckets, boundary tables (24.9 / 25.0 / 25.1), digest parity |
| verification | granularity | 2/5 | 15 tasks. A report parser, brief digest and status verb are built around a report the main loop already receives in the completion notice |
| verification | wave correctness | 5/5 | Clean 5-wave DAG. t-12 correctly pins the number t-5 left provisional |

**Skeleton: mvp.** Its 5-task / 3-wave shape fits "speed over ceremony", and its gaps are local. The c-3 green hole, an unenforced "one writer", and prose-only fencing are each closed below by one graft from risk or verification. Taking risk or verification as the skeleton would mean pruning ~7 tasks of state machinery. Neither criteria nor runtime need those tasks.

## Merged plan

```
Phase solo-task-subagent — 9 tasks across 3 waves

Wave 1
  t-1  Capture writer hook payloads                                   [risk+verification]
       files:    internal/gate/testdata/writer_pre_edit.json (new), internal/gate/testdata/writer_pre_bash.json (new),
                 internal/gate/testdata/writer_spawn_pre.json (new), internal/gate/testdata/writer_subagent_stop.json (new),
                 internal/gate/testdata/writer_resume_post.json (new), internal/gate/testdata/main_pre_edit.json (new),
                 internal/gate/fixture_test.go
       covers:   c-1, c-5
       desc:     Go/no-go capture via headless `claude -p` in a scratch repo, with a temporary stdin-dump hook in the scratch
                 repo's .claude/settings.local.json (never ~/.claude). This is the method solo-task-review t-1 used. A background
                 scratch dross-task-writer does one Edit and one Bash, then is resumed once with SendMessage. If the writer's
                 PreToolUse payloads lack agent_type, STOP before t-8 and drop it (see Disagreement 1).
       contract: - writer_pre_edit / writer_pre_bash not real PreToolUse captures (session_id, transcript_path, toolu_ id) carrying
                   agent_type "dross-task-writer" and a non-empty agent_id → TestCapturedWriterPreToolUse fails (NO-GO signal)
                 - main_pre_edit.json carrying any agent_type or agent_id key → TestCapturedMainLoopHasNoAgentType fails
                 - the resumed writer's stop carrying a different agent_id from its launch → TestCapturedWriterResume fails
                   (decides whether the t-9 fix round can bind to the same writer)                           [risk]
                 - writer_subagent_stop.json's last_assistant_message holding no dross-writer-report fence → TestCapturedWriterStop fails
                 - a grep for non-trivial ~/.claude/CLAUDE.md lines across the fixtures must count 0 before commit [verification]

  t-2  Record the v1.7 baseline and calibrate the gate                [mvp+verification]
       files:    .dross/phases/solo-task-subagent/kill-switch.toml (new), internal/cmd/killswitch_record_test.go (new)
       covers:   c-6
       desc:     Mine the v1.7 solo transcripts (solo-task-review → review-comment-ingest) with a jq filter recorded in the
                 file, following the flow-cost.toml pattern. Count main-loop entries only (isSidechain false, model not
                 <synthetic>), split per task from `task status … in_progress` to done|failed, with message.id deduped. Each
                 [[before.task]] row holds peak_context, context_growth, wall_clock_s, turns, and surface_bytes (task.files at
                 the task commit's parent, via `git cat-file -s`). [thresholds] context_drop = 0.25, wall_clock_rise = 0.25.
                 [calibration] holds threshold and method (provisional 50,000 bytes from verification's probe). [trial]
                 status = "pending". The file holds counts only.
       contract: - a v1.7 solo phase with no [[before.task]] row, or a row with zero peak_context / wall_clock_s / turns, or a
                   negative context_growth (a compaction segment counted raw) → TestKillSwitchBaselineRecorded fails  [mvp+risk]
                 - [thresholds] other than 0.25 drop / 0.25 rise, or any turn-count threshold → TestKillSwitchThresholdsLocked fails
                 - median growth of [before] rows at/above [calibration].threshold not greater than that of rows below it
                   → TestThresholdCalibrated fails                                                         [verification]
                 - no [after] with [trial].status ≠ "pending" → TestKillSwitchTrialPending fails             [verification]
                 - any string value outside the declared keys (phase, task, method, filter, status) → TestKillSwitchCopiesNoContent
                   fails, so no transcript text leaks into a tracked file                                   [risk]

  t-3  Ship the dross-task-writer definition                          [mvp+risk+verification]
       files:    assets/agents/dross-task-writer.md (new), internal/review/writer.go (new),
                 internal/review/writer_test.go (new), internal/cmd/install_agent_test.go
       covers:   c-1, c-2
       desc:     Frontmatter: name dross-task-writer, tools exactly Read, Grep, Glob, Edit, Write, Bash. It has no model key
                 (it inherits the session model), no memory key, and no omitClaudeMd. Body: read the task via `dross task show`
                 and the spec. Edit only task.files and never .dross/. Test only with `dross test --files`. Never git
                 add/commit/stash, never `dross task status`, `changes record`, `phase`, `ship` or `issue`. End with one fenced
                 dross-writer-report (files touched, `test: green|red` + first failing test, notes ≤5 lines, no code, no diff).
                 writer.go holds WriterAgent and WriterPromptLine(phase, task). syncAgents installs the agent with no code
                 change.
       contract: - a `model:` or `memory:` key, Edit/Write missing, or tools gaining Agent, Task, WebFetch, WebSearch or
                   NotebookEdit (or dropped to inherit-all) → TestWriterDefinitionShape fails          [mvp+verification+risk]
                 - frontmatter name drifting from WriterAgent → TestWriterNameMatchesConst fails        [verification+risk]
                 - the body no longer forbidding git commit/stash, `dross task status`, `changes record`, the finalize verbs, or
                   edits under .dross/ → TestWriterNeverCommitsOrDecides fails                          [mvp+verification]
                 - a report spec that drops files/test/notes or permits pasting code or the diff → TestWriterReportBounded fails
                 - WriterPromptLine not a single line naming both phase and task → TestWriterPromptLineShape fails [verification]
                 - syncAgents not writing dross-task-writer.md beside the reviewer → TestSyncAgentsInstallsWriter fails;
                   TestEmbedDrift keeps passing                                                         [mvp+verification]

  t-4  Amend the dross-agent-gate builtin                             [verification+risk; mvp t-3 text half]
       files:    internal/rules/rules.go, internal/rules/rules_test.go, docs/subagent-offload-audit.md
       covers:   c-1, c-4
       desc:     The builtin gains a solo-only exception: under /dross-execute --solo, one dross-task-writer per task may
                 edit that task's files. It never commits, sets task status, writes .dross/ or finalizes. /dross-quick --solo
                 is named as excluded. The read-only fan-out clause and the pair clause stay byte-identical. The audit doc's
                 boundary paragraph and its `### execute` section name the writer.
       contract: - the rendered rule losing "writing and deciding stay gated", "--solo", "one", dross-task-writer, "per task",
                   or "never commits, sets task status or finalizes" → TestAgentGateAllowsSoloWriter fails [mvp+verification]
                 - the pair sentence changing by a byte → TestAgentGatePairSentenceStable fails (c-4)   [risk]
                 - the /dross-quick exclusion dropped → TestAgentGateQuickExcluded fails (quick_solo)   [risk]
                 - TestSubagentOffloadAuditCoversEveryPrompt keeps passing, and the execute section names the writer [verification]

  t-5  Store the writer record behind tamper-guard                    [verification+risk]
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/gate/override.go,
                 internal/gate/override_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-5
       desc:     .dross/gate/writer.json {phase, task, agent_id, state launched|stopped, stopped_at}, written with the
                 store's atomic temp+rename. A missing file reads as none. A corrupt one is an error naming the path. A
                 git-tracked one is refused unread. Tamper-guard covers it, because a deleted record would re-allow a second
                 writer and lift the takeover refusal. No ledger, no report field (see Disagreement 2).
       contract: - a truncated writer.json decoding to a zero record instead of an error naming .dross/gate/writer.json, or a
                   missing one returning other than (nil, nil) → TestWriterRecordDecode fails
                 - Write/Edit of writer.json, or Bash `echo x >`, `rm`, `mv` on it, getting past tamper-guard → the tamper table fails
                 - a force-added (tracked) writer.json read instead of refused → TestTrackedRecordRefused fails
                 - unfiled json-tagged fields → TestEveryPathShapedFieldIsDeclared fails

Wave 2
  t-6  Gate writer spawns; record launch and stop                     [mvp+verification+risk]
       depends:  t-1, t-3, t-5
       files:    internal/gate/writer.go (new), internal/gate/writer_test.go (new), README.md
       covers:   c-1, c-3, c-4
       desc:     A Workflow, liftable gate "solo-writer" claims Agent/Task calls whose subagent_type is dross-task-writer. It
                 admits a spawn only when ArmedScope is a solo execute task (not quick) on phase/<id>, the prompt is exactly
                 WriterPromptLine(phase, task) for that task, run_in_background is true, no `model` input is given, and no
                 writer record exists for this task. PostToolUse binds the launch's agentId. The bound writer's SubagentStop
                 sets stopped + stopped_at and calls ClearGreen, so the writer's own `dross test` green never reaches a commit.
                 Unbound agent_ids change nothing. README gets a gate row.
       contract: - with no execute mode, pair mode, a solo quick armed, no task in_progress, or HEAD off phase/<id>, the spawn is
                   refused naming why → TestWriterSpawnRefusedOutsideSolo (c-4)                         [mvp+verification]
                 - solo t-3 in progress: WriterPromptLine(p,"t-3") admitted; a prompt naming t-4 or with any appended text
                   refused → TestWriterSpawnBoundToArmedTask (c-2)                                       [mvp]
                 - run_in_background false/absent, or any `model` key → refused → TestWriterSpawnShape (writer_spawn) [verification+risk]
                 - a second spawn after a launched/stopped record for the same (phase, task) refused naming "one writer per
                   task" → TestOneWriterPerTask (c-1)                                                   [verification+risk]
                 - green.json written before the bound writer's SubagentStop surviving that stop (also on the resumed writer's
                   second stop, from writer_resume_post) → TestWriterStopClearsGreen fails (c-3)        [verification; risk resume]
                 - a stop whose agent_id is not the bound one writes nothing → TestWriterStopIgnoresUnbound   [risk+verification]
                 - with execute.json pair, launch/stop payloads record nothing → TestWriterRecorderPairSilent (c-4) [verification]
                 - an Explore, general-purpose or dross-task-reviewer spawn not claimed → TestWriterGateIgnoresOtherAgents [mvp+risk]

  t-7  Add the dross task dispatch size gate and kill switch          [mvp; contracts from verification+risk]
       depends:  t-2, t-3
       files:    internal/cmd/task_dispatch.go (new), internal/cmd/task.go, internal/cmd/task_dispatch_test.go (new),
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-2, c-4, c-6
       desc:     `dross task dispatch <phase> <task-id>` sums the bytes of task.files at HEAD (new or missing files count 0;
                 paths go through pathfence). If surface ≥ writerSurfaceBytes and writerEnabled holds, it prints
                 `dispatch: writer`, `subagent_type: dross-task-writer` and `prompt: <WriterPromptLine>`. Otherwise it prints
                 `dispatch: inline (<why>)` and no prompt. It exits non-zero unless a solo execute run has that task in
                 progress. writerEnabled is the compile-time kill switch. Compare(before, after) filters both legs to
                 above-gate rows and applies the locked thresholds.
       contract: - threshold−1 bytes → inline with no prompt line; threshold → writer + the exact WriterPromptLine; a dirty
                   working-tree edit does not change the surface → TestDispatchSizeGate                 [mvp+risk]
                 - a "../x" or absolute path errors and is never counted as 0 → TestSurfaceFencesPaths   [risk]
                 - pair mode, a solo quick, or a task not in_progress → non-zero exit with no prompt → TestDispatchRefusesOutsideSolo
                 - writerEnabled = false dispatches a 10 MB task inline → TestDispatchKillSwitchForcesInline  [mvp]
                 - writerSurfaceBytes ≠ kill-switch.toml [calibration].threshold → TestDispatchThresholdMatchesBaseline fails
                 - Compare: drop 25.0% + rise 25.0% → pass; drop 24.9% → fail naming context; rise 25.1% → fail naming wall;
                   turns tripled → still pass; no above-gate after rows → error, never pass → TestCompareThresholds [verification+risk]
                 - once [after] exists, writerEnabled ≠ Compare(before, after).Pass → TestKillSwitchVerdictHolds fails (c-6) [mvp+verification]

  t-8  Fence the writer; refuse inline takeover                       [risk; contracts from verification t-9/t-11]
       depends:  t-1, t-5
       files:    internal/gate/writerfence.go (new), internal/gate/writerfence_test.go (new)
       covers:   c-1, c-3, c-5
       desc:     This task depends on t-1's GO. Calls carrying agent_type dross-task-writer are confined to the following.
                 Edit/Write: only the bound task's files, never .dross/. Bash runs on an allowlist: git
                 status/diff/log/show; dross test --files, validate, rule show and task show; go vet/build and gofmt -l;
                 non-git/non-dross commands. Agent/Task spawns are refused. Calls from a non-bound agent_id are refused.
                 Main-loop calls (no agent_type) on a writer-owned task: Edit/Write outside .dross/ are refused, and while
                 the record is launched so are bare `dross test`, `dross review context` and `git commit`. Stash and
                 `task status … failed` always pass.
       contract: - from writer_pre_bash.json: `git commit -m x`, `git stash`, `git reset --hard`, `git push`, `dross task status p t-3
                   done`, `dross changes record …`, `dross phase complete p`, `gh pr create` → each refused naming the agent
                   gate; `dross test --files a.go`, `git diff`, `go vet ./...` → admitted → TestWriterBashAllowlist (c-1) [verification+risk]
                 - `git commit` hidden behind `sh -c` or `eval` → refused, never allowed → TestFenceFailsClosedOnOpaqueShell [risk]
                 - from writer_pre_edit.json: an Edit of .dross/phases/p/plan.toml or of a file outside task.files → refused;
                   an Edit of a task.files path → admitted → TestWriterEditScope                          [verification+risk]
                 - a writer call carrying a stale agent_id (task failed, or not the bound one) → refused → TestZombieWriterFenced [risk]
                 - from main_pre_edit.json on a writer-owned task: Edit a.go → refused naming
                   `git stash push -u -- . ':(exclude).dross'` and `failed --reason`; Edit .dross/x, the stash and `failed`
                   → admitted → TestNoInlineTakeover (c-5)                                               [verification+risk]
                 - record launched: main-loop bare `dross test`, `dross review context`, `git commit` → refused naming "wait for
                   the writer's completion notice" → TestMainLoopWaitsForWriter (c-3)                    [risk+verification]
                 - no writer record, or pair mode → every claimed call passes untouched → TestFenceSilentUnarmed (c-4) [verification+risk]

Wave 3
  t-9  Route large solo tasks to the writer                           [mvp+risk+verification]
       depends:  t-3, t-6, t-7, t-8
       files:    assets/prompts/_execute.solo.md, assets/prompts/_execute.recovery.md,
                 internal/cmd/execute_prompt_test.go, internal/cmd/quick_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     Solo partial: after §1a, run `dross task dispatch`. On inline, run today's §1b–§1e unchanged. On writer,
                 skip §1b–§1e, spawn with the printed subagent_type and prompt verbatim with run_in_background, wait for the
                 completion notice, and read only the dross-writer-report: never Read task.files, never `git diff`. A spawn
                 error, `test: red` or no report → stash, then `failed --reason "writer: …"`, and never fix inline. Green →
                 §1f in the main loop: bare `dross test`, reviewer, commit. A blocking review's one fix round goes back to
                 the same writer via SendMessage (load it with ToolSearch first; if unavailable, or the reply is red or
                 missing, fail the task). Recovery partial: a writer record still launched on resume → stash + failed,
                 never resume inline. Run `make install` before relying on it (r-01).
       contract: - the writer branch spawning without first calling `dross task dispatch`, or dropping the verbatim-prompt or
                   background rule → TestSoloWriterDispatch fails (c-2)                                  [mvp+risk]
                 - writer-green not followed in order by bare `dross test` → `dross review context` → commit, or calling the
                   writer's report sufficient → TestSoloWriterGatesStayInMainLoop fails (c-3)          [mvp+verification]
                 - the writer branch telling the main loop to Read task.files, run `git diff`, or do §1b/§1d →
                   TestSoloWriterReportOnly fails (c-2)                                                  [mvp+risk]
                 - any failure arm (error, red, empty, spawn error, fix round red/missing) offering anything but stash then
                   `failed --reason`, or offering an inline fix → TestSoloWriterFailurePath fails (c-5)  [mvp+risk]
                 - execute.md, any partial fetched with mode=pair, quick.md or _quick*.md mentioning dross-task-writer or
                   `task dispatch` → TestPairPromptHasNoWriter / TestQuickHasNoWriter fail (c-4, quick_solo) [mvp+verification]
                 - the recovery partial lacking the writer-in-flight arm (stash + failed) → TestRecoveryWriterInFlight fails [risk]
                 - TestPromptByteBudget keeps passing with execute.md unchanged                           [verification]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-3, t-4, t-5, t-6, t-8 |
| c-2 | t-3, t-6, t-7, t-9 |
| c-3 | t-6 (ClearGreen at stop), t-8 (main loop waits), t-9 |
| c-4 | t-4, t-6, t-7, t-8, t-9 |
| c-5 | t-1, t-5, t-8, t-9 |
| c-6 | t-2, t-7 |

6/6 covered. The [after] leg is the next real solo phase (locked `baseline`). This phase ships [before], the tooling and `[trial] status = "pending"`. The verifier should expect c-6's "after" half to be satisfiable but not yet satisfied. The baseline transcripts expire around 2026-11-03; the main loop archives them outside this plan.

## Disagreements

1. **Enforce writer confinement and no-takeover in gates, or in prose.** risk and verification gate both. Each spends a capture go/no-go task, a writer Bash allowlist and Edit scope, and a main-loop takeover refusal keyed on PreToolUse `agent_type` (risk t-1/t-9; verification t-1/t-9/t-11). mvp rejects all of it: the agent definition plus the prompt carry c-1's "never commits/sets status" and c-5's "never takes over". **Default: gate it (t-1 + one combined t-8, risk's shape).** Solo has no human gate, and c-5's "never" is untestable as prose. If t-1 is NO-GO, drop t-8 and fall back to mvp's posture. Existing `solo-review` and `plan-edit` still block a writer commit or a plan.toml edit. This decides 2 of the 9 tasks and whether the plan pivots mid-phase.

2. **State machinery around the report.** verification and risk add a ParseReport fence parser with caps and diff-marker rejection, a brief/context file with a sha256 digest, a writer ledger (risk: fixing/conflict states), and `dross writer status`/`brief`/`prompt` verbs (about 4 tasks). mvp reads the fenced report straight from the completion notice and binds the spawn to a plain WriterPromptLine. **Default: mvp.** The completion notice already puts the writer's final message in the main loop's context, so a parser cannot un-show a pasted diff. Its cap is cosmetic for c-2. Revisit if t-1 shows background notices omit the final message.

3. **Who does the review fix round.** mvp and risk resume the same writer via SendMessage. verification keeps it inline in the main loop after a writer green, because a second writer breaks "one" and failing on any block is stricter than today. **Default: SendMessage (2 of 3).** An inline fix reads the files (breaks c-2), and t-8's takeover refusal stays a simple "main loop never edits a writer task's code" with no outcome states. If t-1 shows a resumed writer gets a new agent_id, the fix round becomes "fail the task", and verification's inline variant needs t-8 to track red/green state.

4. **How the writer's own green is kept from certifying (c-3).** mvp says existing commit-green/solo-review suffice. That is wrong here: with no lanes, `--files` runs the full suite and writes green.json. verification calls ClearGreen at the writer's SubagentStop. risk has commit-green admit only a green.json whose At is after stopped_at, and edits commit.go/soloreview.go. **Default: ClearGreen at stop (t-6).** No commit-gate edit, and it reuses the reviewer's stop recorder. Cost nobody planned for: a writer task runs the full suite twice (writer + main loop), which pressures the ≤25% wall-clock threshold.

5. **Baseline mining: jq-in-toml or a Go miner and verb.** mvp records a jq filter plus rows in kill-switch.toml, following the flow-cost.toml precedent. risk (internal/killswitch + `writer measure`) and verification (internal/writercost + `writer cost`) ship a tested Go miner that covers compaction segments, split messages, cross-session dedupe and leg isolation (2 tasks each). **Default: jq (t-2),** with risk's no-negative-growth and no-content checks grafted as record tests. This costs reproducibility of the [after] leg if transcript shape drifts before the next solo phase. Fallback: promote it to Go then.

6. **One writer per task: stated or enforced.** mvp states it only in the builtin and binds the spawn to the armed task's prompt line; it rejects a launch recorder because no criterion asks for a refusal. risk and verification refuse a second spawn from a launch record, and risk adds parallel-spawn conflict detection. **Default: enforce it with a launch record (t-5 + t-6), without the parallel-conflict ledger.** The existing PostToolUse fixture already carries agentId, so this needs no new capture. It adds the store task.

7. **Size metric and calibration rule.** mvp uses summed task.files lines with threshold = median of the before rows (≈1,380). verification uses bytes, provisionally 50,000 from a 39-task probe that put ~24 above, and tests that above-gate growth exceeds below-gate growth. risk uses file count + bytes with threshold = smallest surface whose median growth > 2× the measured spawn overhead. **Default: bytes at the parent commit with verification's separation test.** verification's probe shows growth is not monotonic in size (≥100 kB → 23k < 70–100 kB → 49k), so whichever rule is picked decides which tasks get a writer.

8. **Kill-switch form and comparison population.** mvp and verification use a compile-time writerEnabled that a record test forces to match the verdict. risk reads a runtime verdict with a one-named-phase trial, where unjudged or fail means inline, plus a doctor section. mvp compares all tasks; risk and verification compare above-gate tasks only. **Default: compile-time switch, above-gate rows, `[trial] status = "pending"`.** risk's doctor nag for an unjudged trial is dropped, so nothing reminds anyone to run the [after] leg.

9. **Writer readiness at `execute begin --solo` and in doctor.** risk (t-13/t-14) and verification (t-13) generalise reviewer readiness (missing/stale/shadowed) and refuse a solo begin without a current writer. mvp rejects it: syncAgents already installs the writer, and a missing one fails the spawn into c-5's red path. **Default: none.** Note the cost: a stale or missing writer fails every above-gate task in the run instead of refusing once at begin. risk's install.go change is unnecessary in any case.

10. **How pair byte-for-byte (c-4) is proven.** risk pins a golden of execute.md's pair CommonPath expansion at base commit 29fc87db. mvp and verification assert that no writer text appears in execute.md, pair-fetched partials or quick prompts, and that TestPromptByteBudget holds. **Default: the no-writer-text scan.** A golden breaks on every unrelated execute.md edit, while the scan misses a non-writer byte change made by this phase. Low plan impact either way.
