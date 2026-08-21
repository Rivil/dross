# Plan Review — board-mirror-reaper

Reviewed: 2026-08-21
Plan: 12 tasks across 5 waves

## BLOCKING

- [forbidden-actions] t-12 runs `dross issue reap` / `--apply` against the **live** DRO board but never states a `make install` step. Project rule r-01 (severity `hard`) says prompt and Go edits are not live until `make install` re-links them and that the installed binary can be stale versus source. t-12 depends on t-8 and t-10 — every line of reap is Go code written earlier in this phase — so an unrebuilt binary either lacks `reap` entirely or carries a mid-phase version of it, and this is the one task in the plan that issues irreversible writes to a real tracker. The repo has been bitten by exactly this before (a stale binary re-seeded a phase divergence during a ship).
  Suggestion: make `make build && make install` (or `make doctor`) an explicit first step in t-12's description, and add a contract line asserting proof.md records the binary version/commit the live run used.

## FLAG

- [wave-order] t-5 and t-7 are both wave 2 and both edit `internal/cmd/board_lifecycle_divergence_test.go`. t-5 rewrites `mirrorLanes`' emission literals and `taskSyncEdgeRE`; t-7 adds a `reapClass`/`terminal` column to the same `mirrorLanes` map. Run in parallel these conflict in one map literal; run sequentially the second task rebases onto a changed fixture its contract was written against.
  Suggestion: move t-7 to wave 3 (its only stated dependency is t-2, so nothing else breaks), or state in t-7 that it lands after t-5 within the wave.

- [antipattern] t-6 declares `--apply` but wires it in t-8, and t-6's test contract does not cover the flag's behaviour at t-6. With atomic commits per task, t-6's commit ships a destructive-sounding flag on a board-writing verb that silently does nothing — the worst failure mode for a flag whose whole purpose is gating writes.
  Suggestion: either drop `--apply` from t-6 entirely (t-8 owns `issue_reap_cmd.go` anyway) or add a t-6 contract line that `--apply` errors "not implemented" and writes nothing.

- [locked-decision / test-contract] t-2's quick-card evidence is `state.json` version ordering: "a ref older than current is finished, equal may be in flight", and its contract bakes that in ("a quick at the current state.json version is not classified; a ref older than current is"). Version monotonicity proves that *another* version bump happened, not that the quick finished. A quick that bumped `1.5.3.1`, was abandoned, and was followed by an unrelated `1.5.3.2` is indistinguishable from a completed one and gets closed. c-3's closing sentence is universal — "A card whose artefact is not complete is never closed" — and this is the one lane with no completion record behind it.
  Suggestion: either name a real completion record for quicks (the quick's own commit/changes artefact, if one exists) or classify quicks `unattributable` by default and let the operator close them by hand; if the version heuristic is accepted deliberately, record it as a locked decision in spec.toml so the weaker evidence is visible rather than buried in a task description.

- [coverage / test-contract] c-9 says "a supported verb restores those cards", but t-10 adds a raw state writer to `YouTrackClient` only. `BoardClient` (internal/forge/forge.go:166) exposes no reopen and no raw state write; the forge `*Client` closes via `UpdateIssue(IssuePatch{State:&closed})`, `JiraClient` has `SetState`, `GitHubClient` has only `CloseIssue`. Nothing in t-10 says how undo behaves on those three, and none of its six contract lines exercises a non-YouTrack backend. `--undo` on a Jira, forge or GitHub board is unspecified — most likely a silent partial restore.
  Suggestion: add a t-10 contract line pinning the non-YouTrack behaviour, even if the answer is "refuses by name and writes nothing on a backend with no raw state writer" — the same shape `milestone-sync --close` already uses when the entity is a version bundle.

- [coverage] The rename's guards only scan prose files: t-5 scans `assets/prompts/*.md`, t-11 extends that to README/ARCHITECTURE. Nothing scans Go source. t-1's file list correctly includes `internal/cmd/deferred_add.go`, `internal/forge/forge.go`, `internal/forge/youtrack.go` and `internal/configenum/configenum_test.go`, but none of t-1's four contract lines covers them — and `deferred_add.go:173` is a **user-facing** Printf ("`dross issue backlog-sync %s` will mirror it later"). Miss it and the compiler stays green while dross tells a user to run a verb that no longer exists. The other three are comments/error strings that will simply rot.
  Suggestion: extend the t-5/t-11 corpus scan to `**/*.go` (or add a t-1 contract line asserting zero `dross issue [a-z]+-[a-z]+` hits under internal/), which also catches the `.Printf` site by construction.

## NOTE

- [strengths] The test contracts are written as falsification conditions, not descriptions: "restoring `task-sync` reddens it", "a classifier reading `iss.State` or the `dross/status` label fails here", "a log written from the post-close read-back records the terminal state and makes undo a no-op, and that fixture fails". That is the difference between a contract and a wish, and it is sustained across all twelve tasks.

- [strengths] Anti-vacuity is treated as a first-class risk — `TestTaskSyncEdgeRegexIsNotVacuous` catches the specific way this rename could silently empty the existing lifecycle guard's emit-set, and t-7 requires an empty lane registry or empty reflection result to be a `t.Fatal`. Likewise `TestEveryPromptIssueInvocationResolves` walks the real cobra tree instead of allowlisting the five new spellings, which is what actually catches a prompt repointed at a verb t-1 never registered.

- [strengths] t-12 depends on t-10 so the first live batch is reversible, and t-6 is a genuine wave-2 dependency of t-9 (watch.md's reap remedy line has to resolve against the registered command for t-5's corpus guard to pass) — that dependency is subtle and correct rather than decorative.

- [wave-order] t-1 removes the hyphenated verbs in wave 1 while t-5 repoints the prompt corpus in wave 2, so there is a one-commit window where the installed prompts invoke verbs that no longer exist. The existing suite stays green across it (`mirrorLanes` still names hyphenated emissions matching unchanged prompts), so this is a deliberate, contained cost — but per r-01 anyone running `make install` between the two commits gets a broken prompt set.

- [granularity] `.dross/reap-log.json` is the sole input to `--undo`. Whether it is git-tracked (as board.json is) or filtered out at ship decides whether undo survives a branch switch or a second machine. t-3 does not say.

## Summary
The plan is unusually well-specified and its guards are hard to fool, but t-12 writes to a live board with no rebuild step against a hard project rule, and three narrower gaps — the quick-card evidence, undo on non-YouTrack backends, and the unguarded Go-source verb references — should be closed before execution.
