# Plan review — subagent-offload-audit

Independent review of `plan.toml` against `spec.toml`, `project.toml`, `rules.toml`, and the repo.

## BLOCKING

(none)

- Coverage is complete: c-1 → t-1, c-2 → t-2, c-3 → t-3, c-4 → t-2 + t-3. Every criterion sits in some task's `covers`.
- No locked-decision conflicts: t-2/t-3 descriptions restate the size-gated conditional posture ("large surface … ->", "many/large task files ->") with heuristic thresholds, matching `offload_posture`; t-1's doc+test shape at `docs/subagent-offload-audit.md` matches `audit_convention` exactly.
- No rules violations in the plan itself (see NOTE on r-01 for execution).

## FLAG

1. **Test contracts pin guidance presence + boundary, but not the size-gate itself** (t-2 lines 38-41, t-3 lines 57-60). The locked `offload_posture` decision requires offload to be a *conditional* preference, never mandatory. The contracts assert failure when the guidance or the boundary sentence is dropped, but nothing pins the conditional/threshold phrasing ("when the surface is large…", "small phases stay inline"). A later edit could rewrite either passage as an unconditional "always fan out subagents" and both pinned tests would still pass, silently violating the locked decision. Fix at execution time: have `TestVerifyPromptOffloadGuidance` / `TestExecutePromptOffloadGuidance` also assert a size-gate phrase (and ideally the inline-fallback clause) sits in the passage.

## NOTE

1. **Strength — file targets are reality-checked.** `assets/prompts/verify.md` §2 ("Criterion-to-test mapping", line 41) and `execute.md` §1b ("Code insight", line 80) both exist and are exactly the steps the spec names. `internal/cmd/execute_prompt_test.go` exists, so "extend" is correct for t-3; `verify_prompt_test.go` does **not** exist, and t-2 correctly frames it as a *new* prompt test rather than an extension. No dangling references; every new file is created by the plan.
2. **Strength — wave order encodes a real dependency.** t-2/t-3 both say "per the audit": the disposition doc written in wave 1 determines what guidance the wave-2 prompt edits carry. Sequencing the audit first is the right call, and t-2/t-3 are correctly parallel within wave 2 (disjoint files).
3. **Strength — faithful convention mirror.** t-1's design (walk `assets/prompts`, require a `### <name>` section, exempt `_`-prefixed partials, fail naming the offender) mirrors `interaction_coverage_test.go` + `docs/interaction-audit.md` precisely, including the `_interaction.md` partial exemption. The named test `TestSubagentOffloadAuditCoversEveryPrompt` with a "fails naming it" contract is concrete and fail-closed as c-1 demands. (Executor: the "prompt added without a section" contract clause will need a temp-dir fixture in the style of `writeCoverageFixture` — you can't add a prompt to the live tree in a test.)
4. **Execution reminder, not a plan defect** — rules.toml r-01 (hard): after t-2/t-3 edit prompts under `assets/`, run `make install` before relying on the changes live; `go test` reads from the repo tree so the new pins pass regardless, but the installed skills stay stale until re-linked.

## Summary

A tight, well-grounded plan: 3 tasks, 2 files each, full criterion coverage, correct new-vs-extend file framing, and wave order that makes the audit inform the guidance. The one flag is an under-pinned test contract: nothing machine-checks the *conditional* half of the locked offload posture, so add a size-gate phrase assertion to both prompt tests when writing them. Nothing blocks execution.

**0 BLOCKING / 1 FLAG / 4 NOTE**
