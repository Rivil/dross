# prompt-partials — MVP lens draft

Phase prompt-partials — 7 tasks across 5 waves

Wave 1
  t-1  Add partial store and `dross prompt partial`
       files:    assets/embed.go, assets/partials_test.go, internal/cmd/prompt_partial.go,
                 internal/cmd/prompt_partial_test.go, cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-3
       desc:     Partials live at assets/prompts/partials/<prompt>/<name>.md (the embed FS dir IS the
                 registry — no Go map). assets gains Partial(prompt,name), a fetch-line scanner, and
                 WithPartials(name) = the prompt with each fetch point expanded in place by its partial.
                 `dross prompt partial <prompt> <name>` prints the embedded bytes (pathfence on both args);
                 register in main.go, regenerate the CLI-tree golden.
       contract: over an fstest.MapFS fixture, a prompt line `dross prompt partial execute ghost` with no
                 partials/execute/ghost.md fails the dangling-ref check; a partials/execute/orphan.md named by
                 no prompt fails the orphan check; a fetch line whose preceding non-blank line does not open
                 with When/If/Only when fails the trigger check; the same three checks run against assets.FS
                 (vacuous until t-5); `dross prompt partial execute nope` exits non-zero listing available
                 names, and `../x` is refused by pathfence
       depends:  —

  t-2  Absorb bump/board/pull into `execute begin`
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go, assets/prompts/execute.md,
                 internal/cmd/execute_prompt_test.go
       covers:   c-1
       desc:     executeBegin sets state.version to <milestone major.minor>.<phase.DisplayNumber>.0 (skip at 0),
                 calls boardsync.SyncPhase(…,"in-progress") and taskPull(…, apply=false) when the board is
                 enabled, printing what it did; a RequireWorkflowState refusal is reported, not fatal.
                 execute.md §0 drops the bump/sync/pull mechanics and keeps only how to react to the report
                 (moves → ask → `dross issue task pull <phase> --apply`; both-sides conflict; refusal).
       contract: begin in a v1.8 milestone with the phase 3rd in its array leaves state.version = 1.8.3.0
                 (replaces TestExecutePromptWiresPhaseNumber); begin with a fake board records a
                 phase-sync call carrying in-progress and leaves plan.toml byte-identical when the pull
                 reports a move; board disabled → no board call and exit 0; execute.md still names
                 `dross issue task pull` and `--apply` (TestExecutePromptPullsBoardTaskMoves unchanged)
       depends:  —

  t-3  Absorb the UAT board sync into `dross verify`
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go, assets/prompts/verify.md,
                 internal/cmd/verify_prompt_test.go
       covers:   c-2
       desc:     `dross verify <phase>` (attached and --detach dispatch) calls boardsync.SyncPhase(…,"uat")
                 when the board is enabled, surfacing a board error as non-zero; verify.md §1 loses the
                 `dross issue phase sync --status uat` block and keeps one line on reacting to a board failure.
       contract: dross verify against a fake board records exactly one phase-sync with status uat; with
                 the board disabled no board call is made and the verify exit code is unchanged; a fake
                 board returning 500 makes dross verify exit non-zero naming the board
       depends:  —

Wave 2 (depends t-1, t-2, t-3)
  t-4  Route prompt-content tests through prompt+partials
       files:    internal/cmd/prompt_test_command_test.go, internal/cmd/execute_prompt_test.go,
                 internal/cmd/consent_surface_test.go, internal/cmd/verify_prompt_test.go,
                 internal/cmd/board_switch_prompt_test.go, internal/cmd/options_docs_test.go,
                 internal/cmd/lane_toolchain_docs_test.go, internal/cmd/phase_test.go,
                 internal/cmd/pad_residue_test.go, internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-5
       desc:     Mechanical reader swap: promptBody, rawPrompt, readPrompt, executePromptContent,
                 verifyPromptBody, rawPrompts and the direct execute.md/verify.md reads (options_docs,
                 lane_toolchain readDoc, phase_test readRepoFile, pad_residue, boardsync divergence) all go
                 through assets.WithPartials. TestTestStepPromptsNotGrown keeps reading the raw file.
       contract: a fixture partial that carries an asserted phrase, removed from the prompt but still
                 fetched, keeps the phrase visible to promptBody (in-place expansion — sectionOf over §1e
                 still finds it); deleting that partial makes the reader's output lose the phrase, so the
                 existing assertion fails — no instruction can vanish in a move unnoticed
       depends:  t-1, t-2, t-3

Wave 3 (depends t-4)
  t-5  Split execute.md to ≤15,000 bytes
       files:    assets/prompts/execute.md, assets/prompts/partials/execute/solo.md,
                 assets/prompts/partials/execute/test-exit.md, assets/prompts/partials/execute/preflight-repair.md,
                 assets/partials_test.go, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-4, c-5
       desc:     Solo review + solo red-path → partials/execute/solo.md, fetched once in §0 right after
                 `dross execute begin --solo` (locked solo_branch). Non-zero `dross test` exit list + red-path
                 → test-exit.md fetched "If dross test exits non-zero". Wrong-branch/dirty-tree/resume/
                 incomplete-phase handling → preflight-repair.md. Tighten remaining prose. Add
                 TestPromptBudget (embedded assets.FS bytes, cap 15000) with execute.md. Retarget the
                 §1f soloReview/red-solo section anchors to the solo partial. `make install` after commit (r-01).
       contract: TestPromptBudget fails when assets.FS prompts/execute.md exceeds 15000 bytes;
                 TestExecuteCommonPathFetchesNoPartial fails if any `dross prompt partial execute` line sits
                 under a trigger not naming --solo, a non-zero dross test exit, or a pre-flight anomaly
                 (wrong branch / dirty tree / in_progress task / tasks left failed or pending) — so a fetch
                 on the pick→approve→green→commit path fails; the solo partial's fetch line must appear in
                 §0 after `dross execute begin <id> --solo` or the test fails; t-1's dangling/orphan/trigger
                 checks now run non-vacuously against the three real partials
       depends:  t-1, t-2, t-4

Wave 4 (depends t-5)
  t-6  Split verify.md to ≤15,000 bytes
       files:    assets/prompts/verify.md, assets/prompts/partials/verify/detached.md,
                 assets/prompts/partials/verify/survivor-drain.md, assets/prompts/partials/verify/not-pass.md,
                 assets/partials_test.go, internal/cmd/verify_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     Detached-leg dispatch/collect/exit 10–15/host-lock text → detached.md ("When the mutation
                 leg is long or a run is in flight"). Lifecycle table + accept/route drain → survivor-drain.md
                 ("If tests.json lists any unclassified survivor"). Fail/partial next-steps, mutation-tool-
                 failed menu, solo-review report block → not-pass.md (trigger names verdict partial|fail,
                 mutation not running, or reviews present). Tighten §1 scope/provenance prose. Add
                 verify.md to TestPromptBudget's table; retarget moved-section anchors. `make install` (r-01).
       contract: TestPromptBudget fails when assets.FS prompts/verify.md exceeds 15000 bytes;
                 TestVerifyCommonPathFetchesNoPartial fails if any `dross prompt partial verify` line sits
                 under a trigger not naming --detach/a non-finished results exit, an unclassified survivor,
                 a partial|fail verdict, a mutation tool that failed to run, or solo reviews — so a fetch on
                 the attached-run → covered criteria → pass path fails; existing verify prompt assertions
                 (mutation_status branches, threshold count = 1, lifecycle states) pass via WithPartials
       depends:  t-3, t-5

Wave 5 (depends t-6)
  t-7  Record before/after measurements
       files:    .dross/phases/prompt-partials/measurements.toml
       covers:   c-6, c-4
       desc:     Read numbers from the Claude Code session JSONL transcripts, not from memory.
                 Execute before = t-2 (pair, run on the old 25,853-byte execute.md); execute after = t-6
                 (pair, fresh session after t-5's `make install`): bytes = prompt bytes + any partial
                 bytes fetched, turns = assistant turns from `task status … in_progress` to `… done`,
                 wall-clock = timestamp delta (human think-time noted). Verify before = the shipped
                 test-output-summaries /dross-verify transcript; verify after = the same phase re-verified
                 with the new verify.md in a throwaway git worktree, --skip-mutation, board disabled there;
                 both wall-clocks also given with the `dross verify` command duration subtracted.
                 Each row records the count of `dross prompt partial` calls observed (expected 0).
       contract: the record is rejected at /dross-verify if any of its 12 cells (execute|verify ×
                 before|after × bytes|turns|wall_clock) is empty, if an after-bytes value differs from
                 `wc -c` of the prompt at the measured commit plus fetched-partial bytes, or if either
                 after row shows a non-zero partial-fetch count (that falsifies c-4)
       depends:  t-6

## Coverage

| criterion | tasks |
|---|---|
| c-1 execute.md ≤15 KB + budget test | t-2 (absorbed prose), t-5 (split + TestPromptBudget) |
| c-2 verify.md ≤15 KB, same budget test | t-3 (absorbed prose), t-6 (split + table entry) |
| c-3 one fetch command, named triggers, dangling/orphan test | t-1 (command + checks), t-5, t-6 (real partials make checks bite) |
| c-4 common pair path fetches zero partials | t-5, t-6 (common-path trigger tests), t-7 (observed zero fetches in real runs) |
| c-5 existing prompt tests run against prompt+partials | t-4 (reader swap), t-5, t-6 (anchor retargets) |
| c-6 before/after bytes, turns, wall-clock recorded | t-7 |

6/6 criteria covered.

## Judgment calls

- Registry = the embed directory `assets/prompts/partials/<prompt>/<name>.md`, not a Go map — rejected a declared registry table because the filesystem already is one list and the dangling/orphan test needs nothing more; copy-mode install skips subdirs and the binary serves partials, so install needs no change.
- Test composite expands each partial **in place at its fetch line**, not appended — rejected append-at-end because in-place keeps the `sectionOf` anchors (§1e exit codes etc.) working, so only the locked §0-solo move forces anchor retargets.
- t-1 merges the assets store and the CLI command (6 files, 2 layers) — rejected splitting them because the command is a ~30-line wrapper whose only consumer is the store; splitting adds a wave for no isolation gain.
- t-4 is one mechanical reader-swap over 10 test files — rejected spreading the swaps into the split tasks because which assertions break is unknown at plan time and task.files must be concrete; a no-behaviour-change swap up front is safe and green on its own.
- verify split (t-6) waits on execute split (t-5) — the shared TestPromptBudget table forces it, and it also makes t-6 the honest "after" pair-execute sample (first task run on the new execute.md after `make install`).
- Before-execute sample comes from this phase's own t-2 (old prompt still live) — rejected an older phase's task as a baseline because same repo/phase/session shape is the fairer comparison and needs no reconstruction.
- After-verify sample re-verifies test-output-summaries in a throwaway worktree with board off — rejected using this phase's own verify (circular: c-6 can't be recorded by the run that judges it) and rejected a pre-final verify of this phase (it would fail on c-6 and fetch the not-pass partial, i.e. not the common path).
- No Go test for measurements.toml — rejected a permanent test that reads a phase bookkeeping file; the contract is a mechanical check at verify. Flagged: a judge who requires a Go test should add a throwaway assertion, not a lasting one.
- cli_absorb split into two tasks (execute, verify) rather than one — together they are 8 files across Go + prompt + tests; separately each is one command and its prompt.
- No new partials for options/ship/plan — deferred to prompt-budgets-rest per spec.
