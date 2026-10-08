# Plan Review — context-boundaries

Reviewed: 2026-10-08
Plan: 11 tasks across 5 waves

## BLOCKING
(none)

Coverage is complete. c-1 is covered by t-2, t-5, t-8 and t-10. c-2 by t-3, t-4 and t-7. c-3 by t-4, t-7, t-8 and t-11. c-4 by t-6 and t-11. c-5 by t-3, t-7 and t-8. c-6 by t-5, t-9 and t-10. c-7 by t-3, t-8 and t-11. c-8 by t-1, t-6, t-7 and t-11. c-9 by t-6.

No task contradicts the five locked decisions. execute.md is not edited, the 50k step is an unexported constant, no gate makes a context call, --solo is informational, and the line carries all four locked parts.

The only rule is r-01, and t-11 runs `make install` before the live after-leg, so it is honoured. There is no global `~/.claude/dross/rules.toml`.

## FLAG
- [test-contract] The c-7 budget is first measured in t-11, and the plan's own 10 MiB `tool_response` stressor probably breaks it. I measured two things on this machine:
  - A narrow `json.Unmarshal` (only the 6 fields t-7 decodes) of a ~10 MiB PostToolUse-shaped payload takes 33–36 ms (min of 7).
  - `dross reentry`, which runs the same `suggestNext` the emit path calls through `Env.Reentry`, takes 20–40 ms wall-clock in this repo.

  Adding process start, a 10 MiB stdin read and the tail read puts the emit path at about 60+ ms, over t-11's "every path's p95_ms ≤ 50". The below and claimed paths sit at about 45 ms, which is borderline. t-8 lists c-7 in `covers` but has no latency contract. t-3's timing test covers only the transcript read, not the payload decode or the verb. So the first red arrives in wave 5, after every code task is committed.
  Suggestion: give t-7 or t-8 a contract that times `Run` or the verb over a 10 MiB payload, on both the below path and the emit path. A miss then surfaces in wave 2–3, and the decode strategy or stressor size can be settled there rather than in the measurement task.

- [antipatterns/files] t-7's Reentry-counter contract says the counter "stays 0 … when the band is already claimed". That needs a read-only "is this band, or a higher one, claimed?" check before composing. t-4 specifies only `ClaimNudge`, which creates the file. The panel synthesis had `gatestate.NudgeClaimed / ClaimNudge`, but plan.toml dropped `NudgeClaimed`. t-7's files don't include `internal/gatestate/nudge.go`, so the executor must either edit an out-of-scope file or copy the `sha256(session)[:16]-<band>` naming into ctxnudge.
  Suggestion: restore the peek in t-4's description. Give it a contract that it never creates a file or calls `ensureDir`, so the below-threshold path stays write-free.

- [antipatterns/docs] `options.md` §15 ("Claude Code hooks") still says ensure wires "the four dross-owned hooks". That is already stale at five, and no task updates it to name `PostToolUse → dross hooks nudge`. That section is where /dross-options explains hook wiring. Documenting the hook there would also fail t-6's `TestContextRuleAddsNoCalls`, which bans the literal string "dross hooks nudge" from every `assets/prompts/*.md`.
  Suggestion: have t-5 (or t-9) update §15. Narrow the t-6 ban to invocations, or exempt options.md §15. The test's purpose is "no prompt calls it", not "no prompt names it".

- [t-5 description] t-5 says options.md §8 names the key "so /dross-options reaches it". §8 offers only `dross defaults save` (Remote) or skip, and no `dross defaults set` exists. `dross defaults show` also omits `[context]` when the key is unset, because the plan's own golden keeps defaults_show.txt byte-identical. A user walking §8 never sees the key, and the agent has no verb to write it.
  Suggestion: state the write path explicitly in §8, for example "hand-edit `~/.claude/dross/defaults.toml`: `[context] threshold = N`, 0 = off". Add that phrase to `TestOptionsNamesContextThreshold`.

- [antipatterns/missing step] t-11 runs `make install` and then expects the nudge to fire in the after-leg. `make install` runs `dross install --link`, which does not call `ensureUserHooks`. Only `hooks ensure`, init and onboard do. Unless the scratch repo is re-created with `dross init`, user-level settings.json still has five hooks, and `after.nudge_fired` will be false. TestFlowCostRecords catches that, but only after a wasted human-driven replay.
  Suggestion: add "`dross hooks ensure`, then confirm `dross doctor` shows ✓ PostToolUse → dross hooks nudge" to t-11 before the replay.

- [antipatterns/squashed] t-11 bundles four independent things:
  - the live after-leg replay, which a human drives
  - the [control] replay
  - a 50-fire latency harness over three paths
  - the permanent `TestFlowCostRecords` guard

  Given the first flag, the latency leg is the likeliest to go red. Because everything sits in one task and one record, a latency miss keeps the c-3/c-4/c-8 flow evidence from committing.
  Suggestion: split the latency measurement from the flow replay, running them sequentially since they share flow-cost.toml. A latency fix then doesn't force a second human replay.

- [test-contract] The c-8 and c-4 gates in t-11 rest on one LLM run per leg: `after.agent_turns ≤ before.agent_turns` and `after.gate_lead == "checkpoint"`. A single incidental extra tool call in the after-leg fails c-8 for a reason unrelated to the nudge. A lucky run can also pass §1g's lead when the rule is unreliable.
  Suggestion: record the per-turn sequence (tool name per main-agent turn) in [before] and [after], so a turn-count difference can be attributed. Alternatively, state how many runs per leg and how they are aggregated.

- [test-contract] t-9's contract names `TestEnsureUserHooksUpgradesTwoHookInstall` as the deliberate 1 → 2 change. `TestEnsureUserHooksWiresGates` (hooks_test.go:220–226) also asserts that PostToolUse is exactly `[dross gate record]`, and it breaks the same way.
  Suggestion: name it in the contract too, so the executor treats it as an intended change and doesn't read it as a regression to route around.

- [antipatterns/secret-handling] t-7's capture uses "a temporary stdin-dump hook" without saying where it is wired. If it goes in user-level `settings.json`, it dumps every tool payload from every live session while installed, including the session running t-7, along with any `tool_response` content those sessions print. The captured fixtures are then committed to a public repo (`[remote].public = true`) and carry `cwd`/`transcript_path` under `/Users/rivil/...` plus real session ids.
  Suggestion: specify the scratch repo's `.claude/settings.local.json` for the dump hook. Before committing the fixtures, scrub home paths and session ids, keeping key presence and shape.

- [locked-decision fidelity] `nudge_content` says the line names "the re-entry command SessionStart will print". t-8 uses `suggestNext` only, and `TestNudgeNamesSessionStartReentry` compares only the "next: …" segment. When a debug session is open, SessionStart (`reentryLine`) also prints `· debug: <slug> … — /dross-debug <slug>`. Mid-/dross-debug, the nudge would name `/dross-execute` while the real re-entry is `/dross-debug <slug>`.
  Suggestion: either reuse reentryLine's tail, which still fits on one line, or record in t-8's description that the debug suffix is deliberately dropped.

## NOTE
- [granularity] t-8 touches 5 files, which trips the split heuristic. The extra files are a one-line telemetry skip, a one-line `AddCommand` and a golden re-mint, so splitting them out would create sub-10-minute tasks. Keep it as is. The synthesis argued this, but plan.toml lost the justification.
- [test-contract] t-1's two contracts are enforced only by `TestFlowCostRecords`, which t-11 creates in wave 5. t-1's commit is ungated, and its [before] record stays unchecked for four waves. That is acceptable for a measurement task, but a malformed [scenario] won't show up until the after-leg tries to replay it.
- [test-contract] t-6's description includes "Never run a dross command to check context". `TestContextRuleAddsNoCalls` requires that the section "names no `dross <verb>`". A `dross [a-z]+` matcher hits "dross command", so the description's own sentence can trip the test. Word it without "dross" or pin the matcher to code spans.
- [wave-order] Every dependency edge is strict: t-6→t-4 for Marker, t-7→t-3/t-4, t-8→t-2/t-7, t-9→t-8, t-10→t-9 for userHooks and doctor_run.txt, and t-11→t-9. No task could drop a wave, and no two tasks in the same wave share a file. One side effect: t-5 adds a ✅ README row for `dross hooks nudge` two waves before the verb exists, so a stalled phase leaves README overclaiming.
- [compaction] The accepted D-4 cost appears only in panel/synthesis.md, not in spec.toml or plan.toml. After auto-compaction, a session can sit past the threshold with no nudge until it passes its old high-water mark plus 50k. The original nudge text has likely been summarised away by then, so the playbook's "nudge in context" rule (c-9) lapses. Worth a `[[deferred]]` entry so it doesn't get lost.
- [portability] Windows is a goreleaser target. `syscall.O_NONBLOCK` in transcript.go compiles for GOOS=windows (verified). The FIFO (`syscall.Mkfifo`) and mode-000 cases in t-3's tests need a unix build constraint. CI is ubuntu-only, so a missing tag won't be caught.
- [execution] t-1 and t-11 need a human to drive live Claude Code sessions in a scratch repo, including `/clear`. This phase cannot run `--solo` end to end.
- [strength] The plan is grounded in the repo, and the claims I checked hold:
  - defaults_show_json.txt gains `"context": {}` because encoding/json never omits an empty struct, while the TOML golden stays identical.
  - The §1g needle is written in `executePromptContent`'s normalised form.
  - `TestRootHelperCallersAreAllowlisted` matches `gate.LocateRoot` by identifier, and the plan routes root resolution into ctxnudge to avoid it.
  - Doctor already grades non-gate hooks ⚠ (doctor.go:1149–1162).
  - All 9 footer-bearing prompts already load the playbook.
- [strength] The hot-path design is careful. The pipeline sits in the fast `internal/ctxnudge` package, and the cmd verb is a thin shell that never fails. `suggestNext` (which runs git) is proven off the below-threshold and claimed paths by a call counter. Compose-before-claim means a panic can't burn a band. O_EXCL claims come with a 32-goroutine exactly-once test.
- [strength] The c-8 evidence is guarded in CI. TestFlowCostRecords rejects a missing record, an after-leg run on the same binary as the baseline, a nudge that never fired, and a §1g that still led with `continue`, and the below-threshold [control] leg anchors c-4's "unchanged" half. Subagent detection is settled by capturing real payloads rather than assuming `agent_id`.

## Summary
The plan covers all nine criteria and respects every locked decision, so it can proceed. Before execution, fix three gaps:
- the missing `NudgeClaimed` peek
- the missing `dross hooks ensure` step in t-11
- the c-7 latency risk that the 10 MiB payload creates but that nothing tests before wave 5
