# prompt-partials: risk-lens draft

Lens: failure modes drive the graph. Each task owns a named set of breakages and
the test that catches them. No risk is owned by two tasks.

```
Phase prompt-partials — 11 tasks across 5 waves

Wave 1
  t-1  Capture before-leg flow cost on base prompts
       files:    .dross/phases/prompt-partials/flow-cost-before.toml
       covers:   c-6
       desc:     Build two scratch fixtures (pair execute task; verify run whose gremlins
                 legs kill every mutant, so it can reach pass) per the replayable
                 test-output-summaries setup.sh pattern. Drive one pair /dross-execute
                 task and one /dross-verify run on a dross built from base 3ee0fe71, with
                 the working-tree prompts still unedited. Record [before] and
                 [verify_before]: prompt_bytes, partial_fetches, agent_turns, turn_tools,
                 wall_clock_s, human_wait_s, dross_calls, verdict, binary_version,
                 session_id. Window: slash-command message → task commit / verify-artefacts
                 commit. Human drives both sessions.
       contract: t-11's TestFlowCostRecords/prompt-partials fails if [before].prompt_bytes
                 ≠ 25853 or [verify_before].prompt_bytes ≠ 25864. Those are the base
                 sizes, so a leg run on already-edited (symlinked) prompts fails.
                 [verify_before].verdict must be "pass" with unclassified_in_scope = 0;
                 a fixture with an equivalent mutant (e.g. Clamp's `x < lo` → `x <= lo`)
                 cannot reach pass and fails that check.

  t-2  Add embedded partial registry + Composed
       files:    assets/embed.go, assets/partials_test.go
       covers:   c-3
       desc:     The single registry is the filename rule `prompts/_<prompt>.<name>.md`
                 (name `[a-z][a-z0-9-]*`). Add Partial(fsys, prompt, name),
                 PartialNames(fsys, prompt) and Composed(fsys, prompt): the prompt
                 followed by its partials in sorted order. All three take fs.FS so tests
                 use fstest.MapFS.
       contract: Partial("execute", "../verify"), Partial("execute", "Solo") and
                 Partial("interaction", x) each return an error. `_interaction.md` is
                 never listed as any prompt's partial (it has no dot). Composed of a
                 prompt with no partials is byte-identical to that prompt (quick.md).
                 Composed of a MapFS holding `_execute.b.md` and `_execute.a.md` puts a
                 before b, so a non-deterministic order fails.

  t-3  Absorb version/board/pull into execute begin
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go
       covers:   c-1
       desc:     After the mode record, begin sets the version patch from
                 phase.DisplayNumber (the same source as `dross phase number`), runs
                 board phase sync --status in-progress, and runs task pull report-only
                 (never --apply). Each outcome is one labelled line: `version:`,
                 `board:`, `pull:`. A refusal (no plan, bad id, solo with no reviewer)
                 does nothing and exits 1. A board failure exits non-zero but still
                 prints all three lines, so it is distinguishable from a refusal.
       contract: - A refused begin (no plan / ../p / --solo with no reviewer) leaves
                   project.toml and state.json byte-identical, and the httptest board
                   sees zero requests.
                 - Resume never lowers the version. With 1.8.3.2 already set and
                   ordinal 3, begin prints `version: … unchanged` and project.toml is
                   byte-identical. A run that resets internal to 0 fails.
                 - With no current milestone, a phase missing from the array, or a
                   milestone version that is not vX.Y, begin prints `version: skipped
                   (<reason>)` and writes nothing.
                 - On a Forgejo/Gitea/GitLab board, the pull refusal prints
                   `pull: n/a …` and exits 0. Only a real HTTP failure exits non-zero.
                 - When the pull reports a both-sides conflict or moves, plan.toml stays
                   byte-identical.
                 - The board returns 500 on phase sync → execute.json is still
                   recorded, the exit is non-zero, and output includes `board: FAILED`.
                 - Board disabled → `board: off` and zero HTTP requests.
                 - TestExecuteBeginLeavesStateAlone is narrowed, not deleted:
                   current_phase, current_phase_status and current_milestone stay
                   unchanged, and only version may differ.
                 - Every non-refused begin prints exactly one `version:` line. t-8's
                   stale-binary check depends on that line being there.

  t-4  Absorb uat sync + status into verify CLI
       files:    internal/cmd/verify.go, internal/cmd/verify_absorb_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-2, c-5
       desc:     The RunE of `dross verify <phase>` (attached and --skip-mutation)
                 and of `dross verify results` syncs the board to uat after
                 tests.json/verify.toml are written, and prints a `board:` line. The
                 RunE of `dross verify finalize` (not the shared finalizeVerify) sets
                 current_phase_status=verified and touches state. emittedStatuses gains
                 a 4th source: the Go call sites of these syncs.
       contract: - The board returns 500 after the attached run → tests.json and
                   verify.toml are still on disk with their measured counts, the
                   output names `board: FAILED`, and the exit differs from a
                   mutation-tool failure's.
                 - A mutation tool error before the skeleton exists → zero board
                   requests.
                 - `verify --detach` dispatch → zero board requests. `verify results`
                   → one uat sync.
                 - The shared leg runner, called directly, makes zero board requests,
                   so a remote host with no token never syncs.
                 - `dross ship` / `dross phase complete` auto-finalize, with
                   current_phase_status=shipped, leaves it shipped. Moving the state
                   write into finalizeVerify fails this.
                 - With the `--status uat` literal deleted from verify.md,
                   TestEmittedStatusesAreTheLifecycleSet still sees uat via the CLI
                   source. A fixture that removes the Go call site makes the scanner
                   report uat missing.

Wave 2 (t-5, t-6, t-7 depend t-2)
  t-5  Add `dross prompt partial` fetch command
       files:    internal/cmd/prompt.go, internal/cmd/prompt_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-3
       desc:     `dross prompt partial <prompt> <name>` prints assets.Partial verbatim.
                 There is no session dedupe (no showOnce): a fetch always prints in
                 full. Wire it into newRoot and update the CLI tree golden.
       contract: - Output is byte-identical to the embedded partial (MapFS swap).
                 - Two calls under one CLAUDE_CODE_SESSION_ID both print the full
                   bytes. A dedupe marker on the second call fails.
                 - Unknown name → non-zero exit, and the message lists that prompt's
                   valid names.
                 - `execute ../verify`, `interaction x` and `execute.md solo` are each
                   refused.
                 - Bare `dross prompt` exits non-zero (EnforceSubcommandKnown), not
                   help with exit 0.
                 - TestCLITree golden drift fails until cli_tree.txt names the command.

  t-6  Route execute-side prompt test loaders through Composed
       files:    internal/cmd/prompt_test_command_test.go, internal/cmd/execute_prompt_test.go,
                 internal/cmd/consent_surface_test.go
       covers:   c-5
       desc:     promptBody, rawPrompt, executePromptContent and readPrompt return
                 assets.Composed(name) rather than os.ReadFile of the prompt alone.
                 Section helpers (sectionOf) are unchanged. Composed appends partials
                 after the prompt, so §-scoped slices still see only the prompt.
       contract: - TestPromptLoadersCompose asserts each helper(name) ==
                   assets.Composed(name) for every prompt. A helper reverted to
                   os.ReadFile fails as soon as execute.md has a partial.
                 - TestTestStepPromptsNotGrown now measures prompt+partials against
                   25854. The split cannot grow the total instruction set without
                   failing it.

  t-7  Route verify-side prompt test loaders through Composed
       files:    internal/cmd/verify_prompt_test.go, internal/cmd/options_docs_test.go,
                 internal/cmd/phase_test.go
       covers:   c-5
       desc:     verifyPromptBody, verify_prompt_test's direct read at :21,
                 options_docs_test's verify.md read at :505, and phase_test's
                 readRepoFile(verify.md) all read assets.Composed("verify.md").
       contract: - The detached-path phrases that options_docs_test pins (:516–533)
                   are found once they live only in a partial. A loader left on the
                   bare prompt fails as soon as t-9 moves them.
                 - The verify threshold-count test ("exactly 1") counts across prompt
                   and partials, so text duplicated into a partial fails it.

Wave 3
  t-8  Split execute.md to ≤15,000 bytes (depends t-1, t-3, t-5, t-6)
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_execute.test-not-run.md,
                 assets/prompts/_execute.board-moves.md, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-4, c-5
       desc:     Precondition: `make install` of t-3+t-5 is live. Prompts are
                 symlinked, so an edit goes live before the binary otherwise.
                 - §0 drops the version/board/pull prose and keeps the reaction to
                   begin's three lines. A missing `version:` line means a stale
                   binary: stop and ask for `make install`.
                 - _execute.solo.md holds the solo review, solo red, solo begin
                   refusal, solo §1g skip and the solo retry rule. It is fetched once,
                   right after `begin --solo`.
                 - _execute.test-not-run.md holds exits 2–8. It is fetched when
                   `dross test` exits with anything other than 0 or 1.
                 - _execute.board-moves.md is fetched when begin's `pull:` line
                   reports moves or a conflict.
                 - §1f keeps a one-line solo hook between the bare `dross test` and
                   `git add <task.files>`. It names the re-fetch command for a context
                   that lost the partial (compaction).
                 - The solo ordering tests (soloReview, TestExecuteSoloRedStashes,
                   TestExecutePromptSoloBeginNeedsReviewer) are retargeted to
                   assets.Partial("execute", "solo"), with the same assertions.
       contract: - TestExecuteSoloReviewPlacement fails if the §1f solo hook leaves the
                   span between the bare `dross test` and `git add <task.files>`.
                 - Inside the partial, re-running `dross test` comes before
                   `dross review context`, which comes before the spawn, which comes
                   before `dross review status`. A reorder fails it.
                 - The footer coverage gate (production footer_coverage.go reads
                   execute.md alone) fails if a "state is on disk" footer moves into
                   a partial.
                 - TestTaskEdgesPairTheBoardStatusWithThePlanStatus fails if the
                   task-in-progress/task-in-review edges leave execute.md.
                 - TestPromptCommitsSatisfyGreenGate's vacuity check fails if §1f's
                   code `git add` moves out.
                 - A new test fails unless §0 names the missing-`version:`-line
                   stale-binary stop.

  t-9  Split verify.md to ≤15,000 bytes (depends t-1, t-4, t-5, t-7)
       files:    assets/prompts/verify.md, assets/prompts/_verify.detached.md,
                 assets/prompts/_verify.mutation-error.md, assets/prompts/_verify.survivors.md,
                 internal/cmd/verify_prompt_test.go
       covers:   c-2, c-4, c-5
       desc:     Precondition: `make install` of t-4+t-5 is live.
                 - The long-leg/detached section moves to _verify.detached.md.
                   verify.md keeps the one-line "go detached when…" trigger (estimate
                   from the mutant-site count).
                 - The install/skip/abort branch moves to _verify.mutation-error.md.
                 - The drain verbs and the unclassified-survivor detail move to
                   _verify.survivors.md, fetched when unclassified_in_scope > 0.
                 - The uat sync line becomes the reaction to `board: FAILED`, and §5
                   drops `state set current_phase_status verified`.
                 - The §4 report block, the §5 next-phase resolution and the footers
                   stay in verify.md.
       contract: - No fetch trigger in verify.md fires on a Go/gremlins leg that
                   reaches pass. Gremlins legs are always whole_file. A test fails if
                   any trigger names `whole_file`, `adapter-lacks-range-runner` or
                   gremlins, because the common Go path would then fetch.
                 - prompt_next_phase_test's sectionOf(verify.md) still finds the pass
                   branch.
                 - A test fails if verify.md §5 still instructs
                   `dross state set current_phase_status verified` (t-4's finalize now
                   writes it; doing both is double bookkeeping).
                 - The footer coverage gate fails if a verify footer moves into a
                   partial.

Wave 4 (depends t-8, t-9)
  t-10 Pin budget, references, triggers, direct reads
       files:    assets/budget_test.go, internal/cmd/prompt_partials_test.go,
                 internal/cmd/prompt_direct_read_test.go
       covers:   c-1, c-2, c-3, c-4, c-5
       desc:     The budget test reads FS "prompts/execute.md" and "prompts/verify.md",
                 cap 15000. Reference/trigger test: every `dross prompt partial p n`
                 line in any prompt resolves via assets.Partial, every partial has at
                 least one fetch line in its own prompt, and every fetch line sits
                 under a trigger from a closed off-common-path set (`--solo`, exit not
                 0/1, pull moves/conflict, detached, mutation failed to run,
                 unclassified > 0). Direct-read guard: no internal/**/*_test.go reads
                 execute.md/verify.md except through a composed helper or an
                 allowlisted (test, reason) entry. TestTaskEdges is allowlisted
                 because it is order-sensitive.
       contract: - A 15,001-byte execute.md fails, and so does 15,001 bytes of
                   verify.md. A budget table that drops either name fails its
                   exact-set check.
                 - `dross prompt partial execute sol` (a typo) fails as a dangling
                   reference. An `_execute.x.md` that no fetch line names fails as
                   an orphan.
                 - A fetch line with no trigger line, or with a trigger outside the
                   closed set, fails. That is the static half of c-4.
                 - A prompt that names `_execute.solo.md` or the
                   ~/.claude/dross/prompts/_ path (telling the agent to Read it
                   instead of fetching) fails.
                 - A new os.ReadFile(".../verify.md") in any test fails the guard.
                   An allowlist entry that no longer matches any read also fails
                   (stale exemption).

Wave 5 (depends t-1, t-10)
  t-11 Record after legs; gate flow-cost
       files:    .dross/phases/prompt-partials/flow-cost.toml,
                 .dross/phases/prompt-partials/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-6, c-4
       desc:     On the `make install`ed phase head, re-drive t-1's two scenarios
                 identically. Merge [before]/[verify_before] into flow-cost.toml with
                 [after]/[verify_after] and delete flow-cost-before.toml. Extend
                 flowCost with the verify legs and prompt_bytes / partial_fetches /
                 verdict. Add checkPromptPartialsFlow and add prompt-partials to the
                 required-phase list.
       contract: - after.prompt_bytes > 15000 fails, and so does an after count not
                   below before.
                 - partial_fetches ≠ 0 on [after] or [verify_after] fails. That is
                   the observed half of c-4.
                 - The existing after ⊆ before dross_calls gate fails if either after
                   leg ran `dross prompt partial` (no before leg ran it).
                 - [verify_after].verdict ≠ "pass" fails, and so does detached = true.
                 - Matching binary_version on both legs fails.
                 - flow-cost-before.toml still present fails.
                 - [before].prompt_bytes ≠ 25853 or [verify_before].prompt_bytes ≠
                   25864 fails (t-1's honesty pin).
                 - Turns and wall-clock are recorded, not gated (user decision
                   2026-10-08).
```

## Coverage

- c-1 (execute.md ≤15,000 bytes with a budget test): t-3 (absorbs the mechanical prose with zero fetches), t-8 (the split), t-10 (budget test on the embedded asset)
- c-2 (verify.md ≤15,000 bytes, same test): t-4 (absorbs the uat/status prose), t-9 (the split), t-10 (same budget table)
- c-3 (one fetch command, trigger named at each fetch, dangling/orphan test): t-2 (single registry), t-5 (`dross prompt partial`), t-10 (references, orphans, triggers, no direct Read of partials)
- c-4 (pair path fetches zero partials): t-8 and t-9 (trigger placement and the gremlins whole_file trap), t-10 (static closed-set trigger check), t-11 (observed partial_fetches = 0, plus the after ⊆ before call gate)
- c-5 (existing prompt tests pass against prompt + partials): t-6 and t-7 (composed loaders), t-4 (the lifecycle emit-set keeps uat through the CLI source), t-8 and t-9 (section-scoped tests retargeted, same assertions), t-10 (direct-read guard so no test silently reads the shrunken prompt alone)
- c-6 (before/after bytes, turns and wall-clock recorded): t-1 (before legs on base prompts), t-11 (after legs plus the TestFlowCostRecords extension)

## Judgment calls

- **Where partials live.** Chose top-level `assets/prompts/_<prompt>.<name>.md`. Rejected a `prompts/partials/` subdir: about 15 directory scanners (board failure sentence, never-push-base, no-trust-grant, commit-green audit, lifecycle emit-set) use `if e.IsDir() continue`, so they would silently stop checking the moved text. Copy-mode install also skips subdirs.
- **One registry.** Chose a filename rule plus `assets.Composed`, shared by the CLI and by the tests in both packages. Rejected a Go map of partials, because it can drift from the files on disk. Rejected per-package test-side composition: cmd and boardsync would carry two registries.
- **Board failure inside begin and verify.** Chose to complete the mode record and the measured files first, then exit non-zero with labelled lines. Rejected exit 0 with a FAILED line, which breaks the "non-zero exit is a board failure" rule every prompt teaches. Rejected abort-before-record, which would lose a 2h mutation leg or block execute on a flaky tracker.
- **Resume version bump.** Chose to bump only when major.minor or patch differ, leaving internal alone. Rejected parity with today's unconditional `state set`, which resets a mid-phase quick's internal counter to 0 on every `/dross-execute --from`.
- **The verify status write.** Chose finalize's RunE only. Rejected finalizeVerify: ship and phase complete auto-heal through it, which would regress current_phase_status from shipped to verified.
- **Partial fetches are never deduped.** Chose always-full output. Rejected reusing showOnce: after compaction the session id is unchanged, so a re-fetch would print a marker instead of the solo rules the context just lost.
- **Measurement.** Chose fixture scenarios driven on a base-commit build (t-1) and on the installed head (t-11), extending the existing flow-cost.toml/TestFlowCostRecords machinery with verify legs. Rejected comparing two different real tasks of this phase (not comparable). Rejected a new transcript-parsing Go tool, since jq_flow already counts turns by distinct message.id.
- **Ordering edge t-1 → t-8/t-9.** This is a validity dependency, not a data one. With symlink installs, editing assets/prompts changes the live prompt, so the baseline must be taken before any prompt edit lands. It is pinned by checking before.prompt_bytes against the base sizes.
- **Binary before prompt.** t-8 and t-9 depend on the CLI tasks and carry a `make install` precondition (r-01). A prompt that trusts begin to bump the version while a stale binary does not would lose the bump silently. Begin's always-present `version:` line makes that detectable.
- **What stays in the prompts.** Chose to keep the §5 next-phase block and §2 phase completion in the prompts and compress them. Rejected moving them to partials (a fetch on every phase's common path; milestone c-6 forbids added round-trips) and rejected absorbing next-phase into the CLI (prompt_next_phase_test pins that text; c-5).
- **Scope-provenance text in verify.md.** Chose not to gate a partial on `whole_file`. Gremlins legs are always whole_file, so every Go verify would fetch it and break c-4 on this repo's own path.
- **Budget test landing.** Chose to land it after both splits (t-10). Rejected red-first: the commit-green gate refuses a code commit on a red tree, so a failing budget test cannot be committed ahead of the split.
- **Solo ordering tests.** Chose to retarget them to the partial with identical assertions, plus a §1f hook-position check. Rejected splicing the partial into §1f in the composed view: the locked solo_branch decision fetches it in §0, so a §1f splice would test a layout that never exists.
