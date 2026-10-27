# Plan Review — prompt-partials

Reviewed: 2026-10-27 (re-review after amendments)
Plan: 16 tasks across 7 waves

## Status of the prior review's blocking items

- **Prior B1 (TestNoPromptPerformsRawBranchSwitch / raw `promptContent`): resolved.** t-7 now lists ship_prompt_test.go and routes `promptContent` through Expand. t-9's contract names the test. `dross phase checkout` is still visible in expanded execute.md once it moves into `_execute.recovery.md`.
- **Prior B2 (no headroom under TestTestStepPromptsNotGrown at t-9): resolved in substance.** t-11 now runs before t-9 (wave 3 vs wave 4) and deletes the absorbed §0 prose. Measured against the current file:
  - **Removed:** about 1,317 B (lines 44–60 and 62–67).
  - **Moved:** about 1,021 B (lines 68–88), which stays in the expansion.
  - **Result:** expanded execute.md lands near 24,600–25,100 B. That leaves room for t-9's fetch and re-fetch lines (about 350–500 B) under the 25,854 B cap.

  See the FLAG below on t-11's own "< 25,000 B" contract.
- **Prior B3 (board-caller floor of 6): resolved.** t-14 keeps `dross issue phase sync <phase> --status uat` inline in verify.md as the `board: FAILED` retry. verify.md stays a `boardCallRe` match, and board_switch_prompt_test.go's `len(callers) < 6` floor holds.

## BLOCKING
- [antipattern / contract] The scope of t-7's meta-guard `TestPromptReadsGoThroughExpansion` is undefined, and neither plausible reading satisfies t-10's contract that the allowlist "must be exactly {install_test.go}".

  **The broad reading.** t-7 says it bans "os.ReadFile of assets/prompts files" in "any internal/ test". Read that way, about 20 raw readers that no task migrates or lists trip the guard on landing:
  - quick_prompt_test.go:20
  - plan_prompt_test.go:17
  - spec_prompt_test.go:17
  - architecture_prompt_test.go:16
  - resume_prompt_test.go:16
  - pause_prompt_test.go:16
  - watch_prompt_test.go:17
  - quality_prompt_test.go:17
  - secure_prompt_test.go:17
  - milestone_dispatch_prompt_test.go:15
  - plan_review_tiers_test.go:24
  - interaction_pilot_test.go:22
  - interaction_setupcmds_test.go:60
  - interaction_test.go:61
  - readme_doc_test.go:244
  - options_docs_test.go:19 (options.md; t-8 only migrates :505)
  - internal/boardsync/board_lifecycle_divergence_test.go:644 (`rawPrompt`, used for quick/milestone/ship; t-10 only migrates TaskEdges at :470)

  The guard is then red at t-7, or these files must stay allowlisted, which breaks t-10's exact-set contract.

  **The narrow reading.** If the guard only matches literal `execute.md`/`verify.md` paths, it never sees the variable-name helpers that matter (`promptBody`, `promptContent`, `readPrompt`, boardsync `rawPrompt`). Allowlisting install_test.go is itself a sign that the rule is undefined: install_test.go reads `~/.claude/dross/prompts/execute.md` and `assets.FS`, not `os.ReadFile` of assets/prompts.

  Suggestion: state the detection rule in t-7, for example "os.ReadFile of a path under assets/prompts naming a prompt that has partials (execute.md, verify.md), or a variable-name helper that the compose test covers". Alternatively, keep the broad rule and list every reader above in a migration task's files. Either way, make t-10's exact-allowlist contract consistent with that rule.

## FLAG
- [test-contract] t-7's `TestPromptLoadersCompose` asserts "helper(name) == Expand(name) for every prompt", but `promptContent` (ship_prompt_test.go:14) and `executePromptContent` (execute_prompt_test.go:18) normalise their output: they lowercase it and strip `` ` ``, `*` and `_`. As written, the contract either fails or forces the normalisation out, and dozens of assertions that match normalised text would break. The description has the same problem ("promptBody, rawPrompt, readPrompt, executePromptContent and ship_prompt_test promptContent return Expand(name)").
  Suggestion: state the contract as helper(name) == normalise(Expand(name)) for the normalising helpers.
- [test-contract] Several contracts assume that moving text out of the base prompt turns a test red, but those tests read through `promptBody`/`promptContent`, which become Expand after t-7. Expand inlines the partial in place, so a move into a partial stays green:
  - **t-15 contract 3.** `TestVerifyPromptReadsTheMilestoneArray`, `GatesTheLastPhaseClaim` and `KeepsTheNoMilestoneFallback` all read `promptBody(t, "verify.md")` (prompt_next_phase_test.go:12). A fetch line inside §5 would keep `sectionOf` green, so "if the pass block leaves verify.md … fail" cannot bite. t-2's `TestCommonPathInline` only pins `If verdict is \`pass\`:`, not the next-phase resolution text.
  - **t-13 contract 3.** The vacuity check in `TestPromptCommitsSatisfyGreenGate` also reads expanded text. Here t-2's raw `git add <task.files>` pin does backstop the surface, but the contract names the wrong test.
  - **t-9 vs t-10 contradiction.** t-9 says `TestTaskEdgesPairTheBoardStatusWithThePlanStatus` fails "if board edges leave execute.md". t-10 (same wave, possibly landing first) moves that test onto Expand(execute), and its own contract talks about the pair "after moving into a partial". Once t-10 lands, t-9's claim no longer holds.

  Suggestion: where the intent is "this text stays in the base prompt", point the contract at a raw `assets.FS` read (the sanctioned raw path t-9 already uses for the solo-begin refusal) or at `TestCommonPathInline`. Make t-9 and t-10 agree on whether task edges may move.
- [locked-decision cli_absorb rationale / c-4] t-12 moves the whole range-provenance paragraph (verify.md:87) into `_verify.scope.md`, triggered only on a `scope degraded:` line. `printScopeSummary` (verify.go:1749) prints that line only for `t.Scope.Degraded` entries, such as AST failures. But the paragraph also carries common-path guidance: "gremlins always is [whole-file]" and "Judge the score against what was actually instrumented, not against the phase's diff". `printRangeProvenance` prints whole-file lines on every gremlins run, so the common path loses its reading guide. The conservation guard won't notice, because it tracks only fenced `dross`/`git` lines and hard-rule lead-ins. This runs against cli_absorb's stated "why" ("without pushing common-path text into partials").
  Suggestion: in t-12, split the paragraph. The whole-file/judge-against-instrumented sentences stay inline (compressed); only the `ast-unavailable`/degraded detail and the `dross verify scope` pointer move.
- [test-contract] t-16's pin "[before].binary_version … must name the 3ee0fe71 base build" is stricter than it needs to be, and t-1 doesn't say how to satisfy it. The Makefile stamps `COMMIT` from `git rev-parse --short HEAD`, and the phase branch is already at 1f37af4a (only .dross/ chores since 3ee0fe71). A natural `make install` before t-1 therefore produces a functionally identical binary that fails the pin. Building from a worktree at 3ee0fe71 and running `make install` there re-points the `~/.claude/dross/prompts` symlink at the worktree.
  Suggestion: pin on "no diff in assets/, internal/ or cmd/ between the recorded commit and 3ee0fe71", or spell out the build steps in t-1 (detached checkout, build, install, and a re-install from the phase branch afterwards).
- [feasibility] The byte estimates run optimistic, which pushes the c-1 risk back onto t-13.
  - **t-11.** Its "~23,500 B raw" requires the new §0 prose (how to read begin's three lines, the stale-binary stop and the board-moves fetch line) to cost nothing. Measured: raw = 25,853 − 1,317 deleted − 1,021 moved + new prose. That gives about 24,000 B raw and about 25,000 B expanded, so t-11's own "expanded below 25,000 B" contract is a coin flip on a faithful implementation.
  - **Downstream.** Carried forward, t-9 lands at about 20,000 B raw and t-13 needs about 4–4.5 KB of common-path compression, not the stated ~3.5 KB.

  Suggestion: either relax t-11's hard < 25,000 B contract (the real constraint is t-9 fitting under 25,854 B), or have t-11 budget its added prose explicitly. Revise t-13's compression figure so its size is visible up front.

## NOTE
- [coverage] Every criterion is covered:

  | Criterion | Tasks |
  |---|---|
  | c-1 | t-3, t-9, t-11, t-13 |
  | c-2 | t-4, t-12, t-14, t-15 |
  | c-3 | t-2, t-5, t-9, t-11–t-15 |
  | c-4 | t-2, t-3, t-4, t-9, t-11–t-16 |
  | c-5 | t-2, t-6–t-15 |
  | c-6 | t-1, t-16 |

  The locked decisions are honoured:
  - **fetch_command:** one generic `dross prompt partial`.
  - **budget_cap:** 15,000 B, checked on embedded `assets.FS`.
  - **solo_branch:** the fetch comes first after the `begin --solo` fence. The §1f line is a non-inlined `**Re-fetch**` reference, pinned by `TestRefetchLineIsAReference`.
  - **cli_absorb:** begin owns version, board and pull; verify owns uat and finalize.
- [forbidden-actions] There are no rules.toml conflicts: runtime is native, and there is no global rules file. The r-01 `make install` handling is now correct, with a post-commit install after every partial-creating task and pre-conditions where the binary must carry Go changes.
- [wave-order] Every dependency is real. The reorder of t-11 ahead of t-9 is the right fix for B2, and t-12 runs in parallel with t-9 on disjoint files. No task can drop a wave.
- [granularity] t-7 touches 5 files, which trips the split heuristic. It is a single mechanical layer (test loaders plus one guard), and splitting it would be artificial. No action.
- [antipattern] After t-11 deletes the §0 phase sync, execute.md:115–116 ("call it unconditionally — the same way the phase-sync above is called") points at text that no longer exists. t-11's description doesn't mention it, though the fix is a one-line touch-up.
- [test-contract] t-14's board-caller contract names only the `[board].enabled` sentence. `TestBoardCallingPromptsSurfaceFailure` also requires "a non-zero exit is a board failure", so the rewritten `board: FAILED` reaction must keep both. The test will catch an omission, so this is informational only.
- [strength] The amendments are precise rather than cosmetic:
  - A sanctioned raw path (`assets.FS`) for the base-prompt-only checks.
  - Expand semantics pinned for partial names, `.md` suffixes and double references.
  - t-6's extractor rule and marker reconciliation are now concrete enough to implement without guessing. One marker per absorbed row; `PATCH=$(…)` correctly falls outside the "starts with `dross `" rule.
- [strength] t-3's contracts cover the awkward paths of putting HTTP into `execute begin`:
  - Refusals have zero side effects.
  - A board failure is recorded before the non-zero exit.
  - A provider refusal is not a failure.
  - Resume keeps the internal version counter.
  - The early-return "already recorded" path still has to print `version:` ("every non-refused begin").

## Summary
All three prior blockers are fixed against the code. One new blocker remains: t-7's prompt-read meta-guard has no defined detection rule, so either t-7 goes red on about 20 unlisted readers or t-10's exact-allowlist contract cannot be met. Five flags are mostly contracts that stop biting once reads go through Expand, plus one locked-rationale concern about range-provenance text leaving the common path.
