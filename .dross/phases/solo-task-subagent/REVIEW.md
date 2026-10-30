# Plan Review — solo-task-subagent

Reviewed: 2026-10-28
Plan: 9 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [antipattern: one task that should be two / design gap] Nothing ever clears `.dross/gate/writer.json`, and two contracts lock in the consequence. t-6's TestOneWriterPerTask refuses a second spawn once a launched or stopped record exists for (phase, task). t-8's TestNoInlineTakeover refuses main-loop Edit/Write on a "writer-owned" task, with no qualifier for the task's status. tamper-guard is not liftable and already covers all of `.dross/gate/` (override.go `isGateRecord`), so the agent cannot remove the record. Retrying the same task id after the c-5 failure path (writer red → stash → `failed`, then reset to pending) is therefore refused through both the writer and inline. Only a human running `rm` in their own terminal unwedges it. review.json avoids this because its scope changes with every task; writer.json is keyed on the same (phase, task) a retry reuses.
  Suggestion: in t-5/t-6/t-8, define when the record stops counting: cleared or superseded when the task leaves `failed`, or ignored once the task's status is not in_progress after a stop. Then add a retry case to TestOneWriterPerTask and TestNoInlineTakeover.

- [antipattern: files not listed] t-6 adds a registered gate ("solo-writer"), but `internal/cmd/gate_override_test.go` is not in its files. TestGateRegistryNames pins the exact registered set (`commit-green … tamper-guard`), so t-6 turns that test red. t-8's fence will also hit it, and the README `dross gate {…}` row (TestReadmeNamesEveryGate), if the fence registers under its own name. The plan does not say whether it does. t-6 and t-8 are both in wave 2, so two separate gates means two tasks editing README.md and gate_override_test.go in parallel.
  Suggestion: state whether t-8's fence is part of `solo-writer` or its own gate. Add gate_override_test.go to the owning task(s). If there are two gates, put the README row and the registry list in one task, or order t-8 after t-6.

- [antipattern: files not listed] t-4's contract extends TestSubagentOffloadAuditCoversEveryPrompt, which lives in `internal/cmd/subagent_offload_audit_test.go`. That file is not in t-4's files.
  Suggestion: add it to t-4.

- [test-contract specificity] t-7's Compare contract ("drop 25.0% → pass; drop 24.9% → fail") does not say which metric the drop is computed on (peak_context or context_growth, both recorded by t-2) or how rows are aggregated (median? per-task paired?). The choice matters. Per-task peak in a solo run mostly tracks the task's position in the run (context accumulates until compaction), not what the task cost. The calibration test already uses growth. As written, any of four Compare implementations satisfies the table.
  Suggestion: name the column and the aggregate in the t-7 description and its TestCompareThresholds row.

- [test-contract specificity / coverage depth] c-6 says "the writer ships only if the thresholds hold, else solo reverts to inline". The plan never states writerEnabled's initial value. TestKillSwitchVerdictHolds is vacuous until [after] exists, and no task or prompt step owns producing [after] in the next solo phase. The synthesis itself notes that "nothing reminds anyone to run the [after] leg". If writerEnabled ships true, the writer goes live before the thresholds are checked. If it ships false, the next solo phase cannot collect the [after] leg.
  Suggestion: state the initial writerEnabled value and the rationale in t-7. Name where the [after] procedure lives (e.g. the [trial] table or the method key in kill-switch.toml), so verify can judge c-6 as "satisfiable, not yet satisfied" on purpose rather than by omission.

- [design risk vs locked threshold] With no `[[runtime.test_lane]]` in this repo, the writer's `dross test --files` runs the full suite, and the main loop's bare `dross test` (c-3) runs it again. Every writer task pays for the suite twice. The synthesis records this cost; the plan does nothing about it. The locked threshold is a median per-task wall-clock rise of ≤25%. On a suite of roughly 10 minutes that is likely to trip the kill switch on suite time alone, even if the context saving is real.
  Suggestion: make an explicit choice in t-3/t-9. Either accept the doubling and say so in the [trial] notes, or tell the writer to skip the suite when no lane matches (c-3 already says its green certifies nothing).

- [test-contract specificity] t-3's report allows only `test: green|red`. A `dross test` that does not run (exit 2–8: stale trust, remote unreachable) is neither. The writer has to misreport it as red, which fails the task via c-5, or as green. That bypasses execute.md's `test-not-run` partial, which treats not-run as distinct from a red verdict.
  Suggestion: add a `not-run` value to the report spec and give it a t-9 arm, or state that not-run maps to red on purpose.

- [antipattern: gap] The solo-begin readiness check (`reviewerStatusIn`, reviewer_agent.go) covers only dross-task-reviewer: missing, stale or shadowed. t-3 relies on syncAgents to install the writer "with no code change", and nothing checks the writer. A stale installed writer (older body, missing a forbid) runs silently. A repo-level `.claude/agents/dross-task-writer.md` shadows dross's definition and can carry any tool set, and no task detects it. A missing writer means every above-gate task fails through c-5 with no up-front warning.
  Suggestion: extend the readiness check (and doctor) to the writer in t-3 or t-7, or record why it is not needed.

- [test-contract specificity] t-8's writer Bash allowlist ends with "non-git/non-dross commands", which makes it a denylist. The contract tests `sh -c` and `eval`. It does not test `go run ./cmd/dross task status …`, `./bin/dross …` or an absolute dross path, `make install`, `xargs git commit`, or `find -exec`. Bash file writes (`sed -i`, redirects) to paths outside task.files are not fenced at all; only Edit/Write are scoped. The solo-review commit gate still blocks a commit, but task-status and file-scope escapes are not covered.
  Suggestion: add those shapes to TestWriterBashAllowlist / TestFenceFailsClosedOnOpaqueShell, or note in t-8 that file scope is prose-only for Bash.

- [test-contract specificity] t-9's review fix round resumes the writer through SendMessage. Nothing moves the record back from `stopped` to `launched` on that resume, and t-6 binds only on Agent PostToolUse. During the fix round, TestMainLoopWaitsForWriter (keyed on `launched`) therefore does not fire, and the main loop can run `dross test` or `dross review context` while the writer is still editing. The second-stop ClearGreen keeps a stale green from reaching a commit, so this costs wasted runs rather than a false green. It is also unclear whether `writer_resume_post.json` is a SendMessage PostToolUse or the second SubagentStop: t-1 calls it the resumed writer's stop, and t-6 uses it for the "second stop".
  Suggestion: decide the record state during a resume, and say in t-1 which hook event that fixture is.

- [test-contract specificity] t-9's recovery arm ("a writer record still launched on resume → stash + failed") has no way to observe the record. t-5 says "no ledger, no report field", and no CLI verb prints writer.json. The arm depends on the main loop either reading the gate file directly or hitting a t-8 refusal first.
  Suggestion: say how the recovery partial detects the launched record (e.g. `dross review status`/`gate status` gains a line, or the partial reads writer.json), and make TestRecoveryWriterInFlight check for that mechanism.

- [test-contract specificity] t-9's contract "if execute.md changes or exceeds its byte budget, TestPromptByteBudget fails" overstates the test. assets/budget_test.go fails only above 15,000 bytes, so a change under the cap passes. That leaves execute.md's c-4 byte-for-byte claim unguarded.
  Suggestion: drop "changes", or name a test that pins execute.md's bytes or hash.

- [locked-decision tension] The locked `dispatch` decision says "The planner calibrates the number from baseline data." The plan ships a provisional 50,000 bytes and moves calibration to t-2's executor. TestThresholdCalibrated plus TestDispatchThresholdMatchesBaseline keep it data-driven, so this is not a contradiction. Still, the plan does not carry a calibrated number, and the synthesis shows growth is not monotonic in size, so the final threshold may differ materially.
  Suggestion: accept it explicitly, e.g. a line in t-2 that the planner hands calibration to t-2 with the 50,000 probe as the prior.

- [granularity] t-7 bundles two concerns. One is the `task dispatch` CLI (surface sum, pathfence, armed-scope refusal, cli_tree). The other is the kill-switch evaluator (Compare plus TestKillSwitchVerdictHolds), which belongs with t-2's measurement and is used only by tests. Seven contract rows across both.
  Suggestion: optional. Splitting Compare out, or moving it next to t-2's record tests, would keep dispatch reviewable on its own. Low priority.

- [granularity] t-1 (7 files) and t-5 (5 files across gatestate, gate and cmd/testdata) trip the 5-file heuristic. Both are cohesive: six capture fixtures plus one test, and one record plus its guard row plus a one-line not_paths entry.
  Suggestion: no change recommended.

## NOTE
- [wave order] t-8 confines calls by agent_type `dross-task-writer` and should use t-3's WriterAgent const, but it lists depends_on [t-1, t-5] only. Waves are unaffected because t-3 is wave 1, but the edge is missing.
- [c-4 scope] Amending the dross-agent-gate builtin (t-4) changes the rule text rendered in pair mode as well. Only the pair sentence is held byte-stable. syncAgents also installs dross-task-writer for pair users. Both stay within the spirit of c-4 (spawns refused, gates silent), but "byte-for-byte" holds for behaviour, not for rendered rule text.
- [README] t-7 adds `dross task dispatch`, but the README's `dross task {next,list,show,…}` row is not in its files. No test forces it.
- [strength] t-1 is a real go/no-go. It captures hook payloads from a headless scratch session, uses fixtures as the contract, has an explicit NO-GO path (drop t-8 and fall back to prose), checks agent_id stability before relying on SendMessage, and greps fixtures against CLAUDE.md lines before committing.
- [strength] The plan catches a non-obvious false-green: with no test lanes, the writer's `--files` run writes green.json. ClearGreen at the bound writer's SubagentStop (t-6), including on the resumed stop, keeps c-3 true without touching commit.go/soloreview.go.
- [strength] Every locked decision is pinned by a named test rather than prose. Examples: no model key and background-only spawn (TestWriterDefinitionShape, TestWriterSpawnShape), quick excluded (TestAgentGateQuickExcluded, TestQuickHasNoWriter), thresholds exact with turns non-gating (TestKillSwitchThresholdsLocked, TestCompareThresholds). Pair-mode silence is tested at every layer (t-6, t-7, t-8, t-9).

## Summary
Coverage is complete and no locked decision is contradicted. Two gaps should be fixed before execution: the writer-record lifecycle wedges any retry of a failed writer task, and the gate-registry test file is missing from t-6. The Compare metric and the writerEnabled ship state also need pinning so c-6 is verifiable.
