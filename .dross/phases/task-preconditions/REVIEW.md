# Plan Review — task-preconditions

Reviewed: 2026-11-22
Plan: 12 tasks across 5 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-9's failing fixture is `echo boom; exit 3`, and t-9 also assigns dross exit 3 to "precondition refusal". The contract "does not exit 3" passes just as well if `task status` passes the child's exit code straight through instead of mapping a refusal to 3, so the test cannot tell the two apart. t-11 inherits the same fixture ("on t-9's failing fixture … exit code differs"), so its exit-code check has the same hole.
  Suggestion: give the fixture an exit code that is not 3 or 4 (e.g. `exit 7`). Assert that dross exits 3 and that the rendered text says "exit 7".

- [test-contract] c-9 says a precondition that runs past its timeout "is killed". t-4 reuses RunLocal, which uses `exec.CommandContext` with no process group and `WaitDelay = 5s`, so a timeout kills only `sh`. The fixture `sleep 30` is a single simple command, and both bash (macOS sh) and dash exec it directly, so no grandchild exists and the test passes. A real precondition such as `go test ./x | tail -1` would leave `go test` running after the "timed out" refusal. Evaluate would return only after WaitDelay closes the pipes, and the contract's "timeout plus WaitDelay" bound accepts that.
  Suggestion: add a forking fixture (`sleep 30 | cat`) to t-4's timeout contract and assert that the grandchild is gone after Evaluate returns. That probably requires process-group kill via `Cmd.Cancel` in the precond path. Decide whether RunLine shares it with `dross test` or keeps it precondition-only.

- [test-contract] t-12's last route check fails if any expanded assets/prompts or assets/agents file "names skip, force or bypass". 34 files under assets/prompts and assets/agents already contain one of those words in unrelated contexts. Read literally, the contract is red on day one. Read loosely, it does not say what it checks.
  Suggestion: scope the check to precondition text, e.g. lines or sections that mention "precondition", or the §1a and refusal arms.

- [test-contract] t-12 contradicts itself. The description has `_execute.board-moves.md` route "a refused gated move to `task status`". The contract fails if any assets/prompts file other than execute.md §1a contains `task status … in_progress`. `_execute.board-moves.md` is a separate file, fetched with `dross prompt partial execute board-moves`, not inlined into §1a. If the partial spells out the route command, t-12 fails its own test.
  Suggestion: either have the partial point back to §1a without spelling out the command, or name the partial in the contract's allowlist.

- [test-contract] t-7: "a precondition edit leaves the grant Granted" counts as a failure. t-2 deliberately keeps the fingerprint unchanged for normalised-equal edits (`timeout = ""` → `"60s"`, `max = 3` → `3.0`) and for a no-op `--precondition` that rewrites the same values. On those edits the grant correctly stays Granted, so either t-7's contract goes red or t-7 prints a "grant revoked" line that is false.
  Suggestion: in t-7, decide revocation by comparing PreconditionSetLine before and after the edit. Print the revocation line only when the line changed, and add a no-op-edit row to the contract.

- [wave-order] t-8 has `depends_on = ["t-2"]`, but its contract applies "in_progress → done for a task with preconditions". That saves a plan carrying `[[task.preconditions]]` blocks, and the save is refused ("a table nested inside an array-of-tables element cannot be patched") until t-1 lands. Wave 2 placement hides this, but the dependency graph is wrong.
  Suggestion: add t-1 to t-8's depends_on.

- [granularity] t-2, t-3 and t-7 each list 5 files, which trips the 5+ file threshold. In t-2 and t-3 the 5th file is the not_paths.txt companion. t-7's extra files are the cli_tree.txt and README companions, and its real spread is phase (plan_edit.go) plus cmd (task.go). Splitting t-2 or t-3 would be artificial. t-7 is the heaviest: three flags plus conflict rules, a revocation message and `show` text/JSON in one commit, with 8 contract rows.
  Suggestion: no split for t-2 or t-3. For t-7, consider moving `task show` output into its own task only if t-7 runs long in execution. Otherwise accept as is.

## NOTE
- [strengths] The test contracts are among the most specific this repo produces. Each row is phrased as "if X regresses, observable Y happens", with exact fixtures: the off-by-one pair `echo 5` / `5.0001`, an exhaustive unparseable list including `0x1p-2`, `1_000` and `Inf`, a fingerprint collision probe `a","b`, and an inline-table comma split `a, b && c`. The edge classes in spec.toml (boundary, precision, idempotency) are all exercised.
- [strengths] The plan names the guard, golden and budget companions up front: not_paths.txt rows, cli_tree.txt, README rows, ARCHITECTURE.md anchors, `rawPromptReadAllowlistSize` 19, the 15000/25854/25500 prompt pins, and the exec-audit tests. This closes the companion-files gap recorded after milestone-closeout-check.
- [strengths] The gate design holds up against the code. `SetTaskStatus` has exactly two production callers, `internal/cmd/task.go:262` (gated by t-9) and `internal/boardsync/task_pull.go:167` (blocked by t-8), so c-7's "no other route" is enforced in Go as well as in prompts. t-4 extracts RunLine so the spawn-site audit still sees one site. t-2's leading-dash refusal matches `argfence.RejectLeadingDash` in ShArgv, so validate catches what spawn would refuse. t-9 re-loads the plan and re-checks status and fingerprint before saving, which closes the time-of-check/time-of-use window.
- [consistency] t-11 (`task check`) does not call `requireExecConsent` (the test_command grant), but `task status … in_progress` calls it first. In a tree whose test_command grant is missing or stale, `status` refuses with exit 1 before evaluating, while `check` evaluates and may report a pass. This does not break c-8, whose verdict concerns preconditions, but the two commands are "the same" only once the test_command grant is in place.
- [execution] Wave 1 (t-2, t-3) both edit `not_paths.txt`. Wave 2 (t-6, t-7) both edit `cli_tree.txt` and `README.md`. t-7, t-9 and t-11 all edit `internal/cmd/task.go`. Pair-mode sequencing makes this safe, but the waves cannot run in parallel worktrees without merge conflicts.
- [rules] No rule violations. Under r-01, t-10 and t-12 edit prompts under assets/, and verify needs `make install` before the prompt changes are relied on live. Also, `plan.md` is at 14857 of 15000 bytes (143 bytes of headroom). t-10's "trim prose" is load-bearing, not optional.

## Summary
Coverage is complete, no task conflicts with a locked decision, and the gate wiring is sound, but six gaps are worth fixing before execution: the exit-3 fixture collision, the grandchild that survives a timeout, t-12's unscoped bypass-word check, t-12's own board-moves partial tripping its route check, t-7's revocation message on no-op edits, and t-8's missing dependency on t-1. The seventh flag, the 5-file granularity threshold, needs no split.
