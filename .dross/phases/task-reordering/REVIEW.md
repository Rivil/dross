# Plan Review — task-reordering

Reviewed: 2026-07-23
Plan: 5 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [test-contracts] t-3's third contract line — "if the quality bar is dropped, the existing §2 quality-bar pins (testable/measurable pushback) fail" — references tests that do not exist. All ten tests in internal/cmd/spec_prompt_test.go pin §3/§4 (routing destinations, defer-first framing, gray-area walking); none pin §2's quality bar. An executor reading "existing" will look for pins to preserve, find none, and either silently skip the contract line or lose time hunting.
  Suggestion: reword the contract to "add §2 quality-bar pins (testable/measurable pushback) alongside the slate needles" so t-3 is explicitly on the hook for creating them.
- [forbidden-actions] rules.toml r-01 (severity: hard) requires `make install` after editing prompts under assets/ or Go code before relying on the change. Every task in this plan edits one or the other, and no task or plan note schedules `make install`. The in-task go-test contracts read repo files directly so they are safe, but the phase's verify/ship step will exercise the installed binary and linked prompts — this exact gap produced the 2026-06-20 stale-binary ship incident.
  Suggestion: add a plan-level note (or fold into t-5, the last task) that `make install` must run before phase verify.

## NOTE
- [granularity] t-2 is small — a one-comparison change in NextRunnable (`t.ID < best.ID` → array position) plus tests — and sits at the merge-candidate boundary. It is defensible as-is: it isolates c-5, lives in a different file than t-1, and runs parallel in wave 1. No action needed.
- [strengths] Plan premises are verified against real code, not imagined: resolveAnchor and saveIfValid exist in internal/cmd/task.go exactly as described, actionCatalog's tech-debt entry is literally `command: "dross techdebt"` in status.go, NextRunnable's tie-break is indeed lexicographic (phase.go:326), and spec.md line 47 contains the exact "List 3-7" phrase t-3 pins against. The two files that don't exist (assets/commands/dross-techdebt.md, assets/prompts/techdebt.md) are created by the task that references them.
- [strengths] Test contracts are failure-mode-shaped and unusually specific — named test functions, exact seed states ([t-1 done, t-2 pending, t-3 done]), exact error text ("anchor task not found: t-9"), byte-identity checks on rejected writes. t-2 even declares which test must fail on current code, making the behavioral change regression-provable.
- [strengths] Locked decisions are faithfully encoded: t-1's description restates move_execution_guard verbatim (pending-only, never before done/in-progress) and adds the mid-array done-task edge case the decision implies but doesn't spell out; move_wave_semantics's adopt-or-reject contract maps directly to TestMoveTaskAdoptsAnchorWaveAndReflows / TestMoveTaskRejectsWaveIllegalAdoption. Wave structure is honest: four genuinely independent wave-1 tasks, and t-5's deps are both real (MoveTask engine from t-1; the move→next e2e asserts t-2's document-order tie-break, which lexicographic ordering would fail).

## Summary
Solid, well-grounded plan — full criteria coverage, no locked-decision conflicts, verifiably real premises; fix the phantom "existing §2 pins" wording in t-3 and schedule `make install` before verify, then proceed.
