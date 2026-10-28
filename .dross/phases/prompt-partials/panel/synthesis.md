# prompt-partials: panel synthesis

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk (11 tasks / 5 waves) | 6/6. c-4 is checked both statically (t-10) and by observation (t-11), and c-6 extends the existing `flow-cost.toml` / `TestFlowCostRecords` machinery. | Highest. Nearly every line names a surface and a mutation that breaks it (e.g. "a run that resets internal to 0 fails"). One line is factually wrong: t-5 says bare `dross prompt` exits non-zero, but `TestEnforceSubcommandKnown_NoArgsStillPrintsHelp` pins exit 0 with help for a bare parent. | Mostly right. t-8 and t-9 each have 5 files (a prompt, 3 partials and a test), which is at the split line. The loader tasks t-6/t-7 miss 3 raw readers: `lane_toolchain_docs_test.go` (reads execute.md's exit-code list through `readDoc`, so t-8 moving exits 2–8 breaks it), `pad_residue_test.go` and `execconsent_docs_test.go`. | Correct. t-1 runs in parallel with the Go tasks on a base-built binary, and t-1 comes before any prompt edit. One weakness: the trigger and reference checks land in t-10, after both splits, so they do not constrain the splits while those are written. |
| mvp (7 tasks / 5 waves) | 6/6 on paper. c-6 has no gate, and its samples cannot be compared: the execute before-leg is a Go-absorb task while the after-leg is a prompt-split task, and verify-before is a mutation run while verify-after is `--skip-mutation` in a worktree. | Mixed. t-1 and t-2 are specific. t-6's "existing verify prompt assertions pass via WithPartials" and t-7's "rejected at /dross-verify" amount to "tests pass". | Too coarse. t-1 has 6 files over 2 layers, t-4 has 10 files, t-5 and t-6 have 6 files each. | Over-serialized. t-4 waits on t-2 and t-3 without needing their output. t-6 waits on t-5 only because they share a test file. t-2 edits execute.md while that same task is the "before" sample. |
| verification (13 tasks / 7 waves) | 6/6, and the strongest on c-4 and c-5. c-4 gets an executable closed trigger vocabulary plus `TestCommonPathInline`. c-5 gets a conservation manifest plus a ratcheted meta-guard. c-6 uses its own scripts and `measurements.toml` instead of the milestone's flow-cost convention. | Very high, on par with risk. Contracts land green and the draft says why. | Mostly right. t-8 has 7 files. t-10 and t-12 have 5 files each. t-5 misses `cmd/dross/testdata/cli_tree.txt`, so `TestCLITreeGolden` goes red when `prompt` is registered. Its README row is a nice-to-have that is not grafted here. | Over-serialized. t-1 sits alone in wave 1 and every task waits on it, although t-2 and t-3 touch no prompt. That adds a wave. t-4 → t-3 is legitimate because they share a helper and the emit-set source. |

**Skeleton: risk.** It has the densest falsifiable contracts and wave edges that are actually needed. It also measures c-6 through `flow-cost.toml` + `TestFlowCostRecords`, which already globs `phases/*/flow-cost.toml` and gates the two earlier v1.8 phases. From verification it takes the early-landing registry/grammar/common-path tests, the conservation manifest, the meta-guard ratchet and the finer split ordering. From mvp and verification it takes in-place expansion and the stray readers risk missed.

Checked against the repo:
- Base sizes are 25853 (execute.md) and 25864 (verify.md) at both 3ee0fe71 and HEAD.
- `~/.claude/dross/prompts` is a symlink to `assets/prompts`, so a prompt edit is live at once.
- The commit gate requires a full green `dross test` (execute.md §1f, `internal/gate/commit.go`).
- `finalizeVerify` is called from ship.go:185 and phase.go:477.
- `emittedStatuses` has 3 sources today.
- `TestExecuteBeginLeavesStateAlone`, `TestTestStepPromptsNotGrown` (cap 25854, reads through `rawPrompt`), `TestTaskEdgesPairTheBoardStatusWithThePlanStatus` (raw `os.ReadFile` of execute.md, order-sensitive) and `TestFlowCostRecords` all exist.
- `TestCLITree` is actually named `TestCLITreeGolden`, and `TestPromptCommandRegistered` does not exist yet (it is new in verification's t-5).

## Merged plan

```
Phase prompt-partials — 16 tasks across 7 waves

Wave 1
  t-1  Capture before-leg flow cost on base prompts                              [risk+verification]
       files:    .dross/phases/prompt-partials/flow-cost-before.toml
       covers:   c-6
       depends:  —
       desc:     Build two scratch fixtures with the inline setup_sh pattern from
                 test-output-summaries' flow-cost.toml: a pair execute task, and a verify
                 run whose gremlins legs kill every mutant. Use fixed GIT_*_DATE so the
                 fixture tree is reproducible. Drive one pair /dross-execute task and one
                 attached /dross-verify on a dross built from base 3ee0fe71, with prompts
                 still unedited. Record [before]/[verify_before]: prompt_bytes,
                 partial_fetches, agent_turns, turn_tools, wall_clock_s (human waits
                 excluded), human_wait_s, dross_calls, verdict, binary_version,
                 session_id, fixture_tree. The human drives both sessions. The file name
                 is deliberately outside the flow-cost.toml glob, so it is not gated
                 until t-16.
       contract: t-16's TestFlowCostRecords/prompt-partials fails if
                 [before].prompt_bytes ≠ 25853 or [verify_before].prompt_bytes ≠ 25864,
                 so a leg run on already-edited (symlinked) prompts fails.
                 [verify_before].verdict must be "pass" with unclassified_in_scope = 0. A
                 fixture with an equivalent mutant (Clamp's `x < lo` → `x <= lo`)
                 cannot reach pass and fails.
                 Running the fixture setup twice gives the same
                 `git rev-parse HEAD^{tree}`, recorded as fixture_tree. A
                 non-deterministic fixture shows up here, before an after-leg depends
                 on it.

  t-2  Add partial registry, fetch grammar and in-place Expand                   [verification+risk+mvp]
       files:    assets/partials.go, assets/partials_test.go
       covers:   c-3, c-4, c-5
       depends:  —
       desc:     The registry is the filename rule `prompts/_<prompt>.<name>.md` (name
                 `[a-z][a-z0-9-]*`); there is no Go map. Add Partial, PartialNames,
                 FetchPoints and Expand. Expand inlines each partial right after its
                 fetch line. All of them take fs.FS. Fetch-line grammar:
                 `**When** \`<key>=<value>\` — <prose>: \`dross prompt partial <p> <n>\``.
                 The closed vocabulary is mode{pair,solo}, preflight{ok,recovery},
                 board_pull{none,reported}, test_exit{0,1,not-run}, leg{attached,detached},
                 mutation{ran,failed}, scope{ranged,degraded}, unclassified{zero,nonzero},
                 reviews{none,present}, verdict{pass,partial,fail}. The common path is
                 every first value plus verdict=pass. Registry, grammar, common-path and
                 common-path-inline tests run over the real assets.FS. They are green
                 today: there are no partials yet, and the raw prompts hold every
                 common-path command.
       contract: Partial("execute","../verify"), Partial("execute","Solo") and
                 Partial("interaction",x) each return an error and never read outside
                 prompts/.
                 `_interaction.md` (no `.<name>` segment) is never listed as a partial
                 and is never reported as an orphan.
                 On a MapFS, a fetch line naming a missing partial fails the registry
                 check with "execute.md:<line>". A `_execute.ghost.md` that no fetch
                 line names fails as an orphan.
                 A fetch line without `**When** \`k=v\``, or with an unknown key
                 (`colour=red`), fails the grammar check.
                 A synthetic trigger `mode=pair` or `test_exit=0` makes
                 TestCommonPathFetchesNothing fail.
                 A trigger whose prose names `whole_file`,
                 `adapter-lacks-range-runner` or gremlins fails. Gremlins legs are
                 always whole_file, so such a trigger would fire on the Go common path.
                 A prompt that names `_execute.solo.md` or a
                 `~/.claude/dross/prompts/_` path (telling the agent to Read the file
                 instead of fetching it) fails.
                 Expand of a prompt with no fetch lines (quick.md) is byte-identical to
                 the prompt. On a MapFS, index(fetch) < index(body) <
                 index(next base line).
                 TestCommonPathInline fails if raw execute.md loses
                 `git add <task.files>`, `dross changes record`, `dross test --files`,
                 `approve <task-id>` or `### 1g`. It also fails if raw verify.md loses
                 `dross verify <phase>`, `dross verify finalize`,
                 `covered`/`weak`/`uncovered` or "If verdict is `pass`:".

  t-3  Absorb version, board sync and pull into execute begin                    [risk+verification+mvp]
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-1, c-4
       depends:  —
       desc:     After the mode record, begin does three things. It sets the version
                 patch from phase.DisplayNumber, the same source as
                 `dross phase number`, and changes it only when major.minor or patch
                 differ. It runs board phase sync in-progress. It runs a report-only
                 task pull (never --apply). Each outcome prints one labelled line:
                 `version:`, `board:`, `pull:`. A `// absorbs: <command>` marker sits at
                 each call. emittedStatuses gains a 4th source: the literal statuses
                 passed to boardsync.SyncPhase in internal/cmd.
       contract: In milestone v1.8 with phases [a,p] and version 1.8.1.3, `begin p`
                 writes 1.8.2.0 to both state.json and project.toml.
                 On resume with 1.8.2.1 already set, begin prints
                 `version: … unchanged` and project.toml is byte-identical. A run that
                 resets internal to 0 fails.
                 With no current milestone, a phase missing from the array or a
                 non-vX.Y milestone version, begin prints `version: skipped (<reason>)`
                 and writes nothing.
                 A refused begin (no plan, `../p`, or --solo with no reviewer) leaves
                 project.toml and state.json byte-identical and the httptest board
                 sees 0 requests.
                 On a youtrack httptest board, begin sends in-progress for p. When a
                 card was moved on the board, the output lists "t-2 pending → done" and
                 plan.toml stays byte-identical (report-only), and the same holds for
                 a both-sides conflict.
                 When the board returns 500 on phase sync, execute.json still records
                 the mode, the exit is non-zero and the output includes
                 `board: FAILED`. With the board off, begin prints `board: off` and
                 makes 0 HTTP requests.
                 On a Forgejo/Gitea/GitLab board the pull refusal prints `pull: n/a …`
                 and exits 0. Only a real HTTP failure exits non-zero.
                 TestExecuteBeginLeavesStateAlone is narrowed, not deleted:
                 current_phase, current_phase_status and current_milestone are
                 unchanged and only version may differ.
                 Every non-refused begin prints exactly one `version:` line, because
                 t-11's stale-binary check depends on it.
                 Deleting the SyncPhase call from begin makes
                 TestEmittedStatusesAreTheLifecycleSet report in-progress missing once
                 execute.md stops emitting it.

Wave 2
  t-4  Absorb uat sync and verified status into the verify CLI (depends t-3)     [risk+verification+mvp]
       files:    internal/cmd/verify.go, internal/cmd/verify_absorb_test.go
       covers:   c-2, c-4
       depends:  t-3
       desc:     After tests.json and verify.toml are written, finishVerify (used by
                 attached `dross verify <phase>`, `--skip-mutation` and
                 `verify results`) syncs the board to uat through t-3's helper and
                 prints a `board:` line. It does not sync on --detach dispatch. The
                 RunE of `verify finalize` (not finalizeVerify) sets
                 current_phase_status=verified and touches
                 "verified <id>: <verdict> (<covered>/<total>, mutation <score>)". Each
                 of these carries an absorbs marker.
       contract: When the board returns 500 after an attached run, tests.json and
                 verify.toml are on disk with their measured counts, the output names
                 `board: FAILED`, and the exit differs from a mutation-tool failure's.
                 A mutation tool error before the skeleton exists makes 0 board
                 requests.
                 `verify --detach` dispatch makes 0 board requests. `verify results`
                 makes exactly one uat sync, so a detached verdict is not left at
                 in-progress.
                 The shared leg runner, called directly, makes 0 board requests, so a
                 remote host without a token never syncs.
                 finalize on a pass verify.toml sets current_phase_status to
                 "verified", and the last history entry starts "verified p: pass". A
                 finalize refused on verdict "pending" leaves state.json
                 byte-identical. A second finalize appends no second history entry.
                 A `dross ship` / `phase complete` auto-finalize with
                 current_phase_status=shipped leaves it shipped. Moving the state
                 write into finalizeVerify fails this.
                 With the `--status uat` literal deleted from verify.md,
                 TestEmittedStatusesAreTheLifecycleSet still sees uat through the CLI
                 source.

  t-5  Add `dross prompt partial` fetch command (depends t-2)                    [risk+mvp+verification]
       files:    internal/cmd/prompt.go, internal/cmd/prompt_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-3
       depends:  t-2
       desc:     `dross prompt partial <prompt> <name>` prints assets.Partial verbatim.
                 There is no session dedupe (no showOnce), so a re-fetch after
                 compaction prints in full. Wire it into newRoot and regenerate the CLI
                 tree golden (DROSS_UPDATE_GOLDEN).
       contract: Stdout is byte-equal to a MapFS-seeded partial, with no trailing
                 newline added or stripped.
                 Two calls under one CLAUDE_CODE_SESSION_ID both print the full bytes.
                 A dedupe marker on the second call fails.
                 An unknown name exits non-zero and the error lists that prompt's valid
                 partial names, never an empty print.
                 `execute ../verify`, `interaction x` and `execute.md solo` are each
                 refused.
                 TestCLITreeGolden fails until cli_tree.txt names `dross prompt partial`.

  t-6  Record instruction-conservation manifest and guard (depends t-2)          [verification]
       files:    assets/testdata/prompt_instructions.txt, assets/conservation_test.go
       covers:   c-5
       depends:  t-2
       desc:     Extract every fenced command line and every `- **…**` hard-rule
                 lead-in from today's execute.md and verify.md into the manifest. Each
                 row must appear in Expand(prompt) or be claimed by a matching
                 `// absorbs: <row>` marker in non-test internal/cmd source. The guard
                 is green when it lands.
       contract: Removing `dross verify finalize <phase-id>` from verify.md and every
                 partial fails TestPromptInstructionsConserved, naming the row.
                 A row claimed only by an absorbs marker fails once that marker is
                 deleted.
                 The extractor finding fewer than 40 rows in execute.md or fewer than
                 25 in verify.md fails, so the manifest cannot go silently vacuous.

  t-7  Route central prompt helpers through Expand; add meta-guard (depends t-2) [verification+risk]
       files:    internal/cmd/prompt_expand_test.go, internal/cmd/prompt_test_command_test.go,
                 internal/cmd/consent_surface_test.go, internal/cmd/execute_prompt_test.go
       covers:   c-5
       depends:  t-2
       desc:     Add an expandedPrompt(t,name) helper. promptBody, rawPrompt, readPrompt
                 and executePromptContent return Expand(name). Add a meta-guard,
                 TestPromptReadsGoThroughExpansion, with a ratchet allowlist naming the
                 stray readers t-8 and t-10 migrate, plus install_test.go, which
                 compares install bytes on purpose.
       contract: A new `os.ReadFile(filepath.Join(root,"assets","prompts","verify.md"))`
                 in any internal/ test file outside the allowlist fails the meta-guard,
                 naming file:line.
                 A helper reverted to os.ReadFile fails TestPromptLoadersCompose
                 (helper(name) == Expand(name) for every prompt).
                 When a MapFS partial holds a banned phrase, a negative assertion over
                 promptBody sees it, which proves negative checks cover partials.
                 TestTestStepPromptsNotGrown now measures the expanded prompt against
                 25854, so the split cannot grow the total instruction set.

Wave 3
  t-8  Migrate verify-side prompt readers to Expand (depends t-7)                [risk+verification+mvp]
       files:    internal/cmd/verify_prompt_test.go, internal/cmd/options_docs_test.go,
                 internal/cmd/phase_test.go, internal/cmd/pad_residue_test.go
       covers:   c-5
       depends:  t-7
       desc:     verifyPromptBody, verify_prompt_test's inline read (:21),
                 options_docs_test's verify.md read (:505), phase_test's
                 readRepoFile(verify.md) (:657) and pad_residue_test's verify.md doc
                 read (:123) all read the expanded verify.md. README.md stays a raw
                 read in pad_residue.
       contract: The detached-path phrases options_docs_test pins (:516–533) are still
                 found once they live only in _verify.detached.md. A loader left on the
                 bare prompt fails as soon as t-12 moves them.
                 pad_residue's "names every whole-file reason" check still fails if
                 _verify.scope.md drops one.
                 The verify threshold-count test ("exactly 1") counts across prompt and
                 partials, so text duplicated into a partial fails it.

  t-9  Move solo and pre-flight recovery branches to partials                    [verification+risk+mvp]
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_execute.recovery.md, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-4, c-5
       depends:  t-1, t-5, t-6, t-7
       desc:     Precondition: `make install` with t-5 live (r-01). Move §1c, §1e, §1f
                 and §1g solo text and the solo retry rule into _execute.solo.md,
                 fetched once right after the `begin --solo` fence (`mode=solo`). §1f
                 keeps a one-line solo hook between the bare `dross test` and
                 `git add <task.files>`, naming the re-fetch command for a context
                 that has been compacted. The refused-solo-begin text stays inline. Move
                 wrong-branch, dirty-tree and in_progress resume handling to
                 _execute.recovery.md (`preflight=recovery`). Retarget the solo tests
                 to the partial and keep their names.
       contract: The `execute solo` fetch line must be the first fetch after the
                 `dross execute begin <id> --solo` fence in §0. Moving it into §1 fails
                 (locked solo_branch).
                 TestExecuteSoloReviewPlacement fails if base §1f's solo hook leaves
                 the span between the bare `dross test` and `git add <task.files>`. It
                 also fails if, inside the partial, re-run `dross test` <
                 `dross review context` < spawn < `dross review status` breaks.
                 TestExecuteSoloRedStashes (retargeted) still fails if `failed`
                 precedes `git stash push`.
                 TestExecutePromptSoloBeginNeedsReviewer still reads raw §0. Moving the
                 refusal into the partial fails it.
                 TestTaskEdgesPairTheBoardStatusWithThePlanStatus (still a raw read
                 until t-10) fails if the task-in-progress/task-in-review edges leave
                 execute.md.
                 The footer coverage gate (footer_coverage.go reads execute.md alone)
                 fails if a "state is on disk" footer moves into a partial.

Wave 4
  t-10 Migrate execute-side stray readers; empty the allowlist (depends t-3, t-8) [verification+mvp]
       files:    internal/cmd/lane_toolchain_docs_test.go, internal/cmd/execconsent_docs_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go,
                 internal/cmd/prompt_expand_test.go
       covers:   c-5
       depends:  t-3, t-8
       desc:     lane_toolchain's readDoc(execute.md), the verify/execute/quick loop in
                 TestPromptsDescribeTheGateAsEnumerated, and TaskEdges' raw read
                 become expanded reads (assets.Expand in boardsync). The allowlist
                 shrinks to install_test.go only, and a stale-exemption check is added.
       contract: The meta-guard asserts the allowlist is exactly {install_test.go}.
                 Re-adding a stray file fails, and an entry that matches no read fails
                 as a stale exemption.
                 Once TaskEdges reads Expand("execute"), in-place expansion keeps the
                 done/task-in-review pair together when it moves into a partial, and
                 transposing the pair still fails.
                 A banned consent phrase placed only in a partial fails
                 TestPromptsDescribeTheGateAsEnumerated.

  t-11 Replace absorbed §0 prose; move board-move reactions (depends t-3, t-9)   [verification+risk+mvp]
       files:    assets/prompts/execute.md, assets/prompts/_execute.board-moves.md,
                 internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-4, c-5
       depends:  t-3, t-9
       desc:     Precondition: `make install` with t-3 live (r-01). §0 drops the
                 version, sync and pull mechanics and keeps how to read begin's three
                 lines. A missing `version:` line means a stale binary: stop and ask
                 for `make install`. The pull reactions (--apply, conflicts, refused
                 provider) move to _execute.board-moves.md (`board_pull=reported`).
                 Retarget TestExecutePromptWiresPhaseNumber, keeping its name, to drive
                 `execute begin` on a milestone fixture.
       contract: A new test fails unless §0 names the missing-`version:`-line
                 stale-binary stop.
                 TestExecutePromptPullsBoardTaskMoves (expanded) still fails without
                 `dross issue task pull <phase> --apply` in the board-moves partial.
                 TestEveryPromptIssueInvocationResolves walks _execute.board-moves.md
                 through the top-level glob and fails on a mistyped verb there.
                 TestPromptInstructionsConserved passes only because t-3's absorbs
                 markers claim the version, sync and pull rows.
                 TestExecutePromptWiresPhaseNumber fails if begin stops deriving the
                 patch from the phase ordinal.

  t-12 Move verify detached-leg and degraded-scope branches (depends t-8)        [verification+risk+mvp]
       files:    assets/prompts/verify.md, assets/prompts/_verify.detached.md,
                 assets/prompts/_verify.scope.md, internal/cmd/verify_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       depends:  t-1, t-5, t-6, t-8
       desc:     Precondition: `make install` with t-5 live. The long-leg, detach,
                 collect, exit 10–15 and host-lock text moves to _verify.detached.md
                 (`leg=detached`). verify.md keeps a one-line "go detached when…"
                 trigger: estimate from the mutant-site count. The range-provenance
                 paragraph moves to _verify.scope.md (`scope=degraded`, meaning verify
                 printed a `scope degraded:` line). Merge the two duplicate
                 denominator paragraphs.
       contract: options_docs detach tests (expanded) still fail if
                 _verify.detached.md loses exit 10/11/12/13/14 or "A non-finished
                 result is not a verdict".
                 TestVerifyPromptReadsRangeProvenance (expanded) still fails if
                 _verify.scope.md loses a whole_file reason.
                 New TestVerifyScopeTriggerIsTheDegradedLine: the scope fetch's prose
                 names `scope degraded:`, the literal printScopeSummary prints, so a
                 renamed output line fails.

Wave 5
  t-13 Move test-not-run exits; pin execute budget (depends t-10, t-11)          [risk+verification]
       files:    assets/prompts/execute.md, assets/prompts/_execute.test-not-run.md,
                 assets/budget_test.go
       covers:   c-1, c-3, c-4, c-5
       depends:  t-10, t-11
       desc:     Exit codes 2–8 ("the run did not happen") move to
                 _execute.test-not-run.md (`test_exit=not-run`). Exit 1 (red) stays
                 inline. Compress the landmark, svelte, offload and §1e prose to
                 ≤14,500 B for headroom. Create TestPromptByteBudget over the embedded
                 assets.FS, with the execute.md row.
       contract: TestPromptByteBudget fails at 15,001 B of execute.md, naming the file
                 and the overage.
                 lane_toolchain exit-code tests (expanded since t-10) still fail if
                 _execute.test-not-run.md drops code 8 or the "2, 3, 4, 5, 6, 7 and 8
                 all mean the run did not happen" line.
                 TestPromptCommitsSatisfyGreenGate's vacuity check fails if §1f's code
                 `git add` moves out of execute.md.
                 With pair facts (mode=pair, test_exit=0),
                 TestCommonPathFetchesNothing fetches none of the four execute
                 partials.

  t-14 Move mutation-error and survivor-drain branches; drop absorbed uat/status prose (depends t-4, t-12) [risk+verification+mvp]
       files:    assets/prompts/verify.md, assets/prompts/_verify.mutation-error.md,
                 assets/prompts/_verify.drain.md, internal/cmd/verify_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       depends:  t-4, t-12
       desc:     Precondition: `make install` with t-4 live. The install/skip/abort menu
                 moves to _verify.mutation-error.md (`mutation=failed`). The drain
                 verbs, the lifecycle table and the unclassified-survivor detail move
                 to _verify.drain.md (`unclassified=nonzero`). §1's uat sync block
                 becomes the reaction to `board: FAILED`. §5 drops
                 `state set current_phase_status verified` and the touch.
       contract: A test fails if verify.md §5 still instructs
                 `dross state set current_phase_status verified`, because t-4's
                 finalize already writes it and doing both is double bookkeeping.
                 TestVerifyPromptNamesTheDrainVerbs / RestatesTheDrainPolicy
                 (expanded) still fail without `dross survivor accept|route` in
                 _verify.drain.md.
                 The `dross issue phase sync <phase> --status uat` and state-set rows
                 stay conserved only through t-4's absorbs markers.
                 The footer coverage gate fails if a verify footer moves into a
                 partial.

Wave 6
  t-15 Move not-pass and solo-review branches; pin verify budget (depends t-13, t-14) [verification+mvp]
       files:    assets/prompts/verify.md, assets/prompts/_verify.not-pass.md,
                 assets/prompts/_verify.solo-review.md, assets/budget_test.go
       covers:   c-2, c-3, c-4, c-5
       depends:  t-13, t-14
       desc:     The fail/partial recommendations and the next block move to
                 _verify.not-pass.md (`verdict=partial` and `verdict=fail` fetch
                 lines). The §0 reviews note and the §4 Solo review block move to
                 _verify.solo-review.md (`reviews=present`). The §4 report block, the
                 §5 next-phase resolution and the footers stay inline. Add the
                 verify.md row and the exact-set guard to TestPromptByteBudget.
       contract: TestPromptByteBudget fails at 15,001 B of verify.md. It also fails if
                 the budgeted set is not exactly {execute.md, verify.md}, so dropping a
                 row fails.
                 TestVerifyShowsReviews (expanded) still fails if
                 _verify.solo-review.md drops "UNRESOLVED".
                 TestVerifyPromptReadsTheMilestoneArray / GatesTheLastPhaseClaim /
                 KeepsTheNoMilestoneFallback still read the inline pass block, and
                 prompt_next_phase_test's sectionOf(verify.md) still finds it.
                 With verdict=pass, unclassified=zero, leg=attached and
                 mutation=ran facts, TestCommonPathFetchesNothing fetches no verify
                 partial.

Wave 7
  t-16 Record after-legs; gate flow cost (depends t-1, t-13, t-15)               [risk+verification]
       files:    .dross/phases/prompt-partials/flow-cost.toml,
                 .dross/phases/prompt-partials/flow-cost-before.toml,
                 internal/cmd/flowcost_record_test.go
       covers:   c-6, c-4
       depends:  t-1, t-13, t-15
       desc:     `make install` at the phase head. Rebuild t-1's fixtures and re-drive
                 both scenarios the same way. Merge [before]/[verify_before] into
                 flow-cost.toml with [after]/[verify_after], recording loaded_bytes as
                 prompt bytes plus fetched partial bytes, and delete
                 flow-cost-before.toml. Extend flowCost with the verify legs plus
                 prompt_bytes, partial_fetches, verdict and fixture_tree. Add
                 checkPromptPartialsFlow, and add prompt-partials to the required-phase
                 list.
       contract: after.prompt_bytes > 15000 fails, and so does an after value that is
                 not below before.
                 partial_fetches ≠ 0 on [after] or [verify_after] fails (the observed
                 half of c-4). A non-zero value is recorded as it was seen, never
                 edited away.
                 The existing after ⊆ before dross_calls gate fails if either after-leg
                 ran `dross prompt partial`.
                 [verify_after].verdict ≠ "pass" fails, and so does detached = true.
                 [after].fixture_tree ≠ [before].fixture_tree fails, and so does the
                 same binary_version on both legs.
                 flow-cost-before.toml still being present fails.
                 [before].prompt_bytes ≠ 25853 or [verify_before].prompt_bytes ≠ 25864
                 fails (t-1's honesty pin).
                 Turns and wall-clock are recorded, not gated (user decision
                 2026-10-08).
```

Coverage:
- c-1: t-3, t-9, t-11, t-13
- c-2: t-4, t-12, t-14, t-15
- c-3: t-2, t-5, t-9, t-11 through t-15
- c-4: t-2 (static), t-3, t-4 (common-path mechanics move to the CLI), t-9 through t-15 (trigger placement), t-16 (observed)
- c-5: t-2, t-6, t-7, t-8, t-10, and the retargets in the split tasks
- c-6: t-1, t-16

6/6 criteria covered.

## Disagreements

1. **How c-6 is measured.**
   - risk: base-built fixture legs, recorded in `flow-cost.toml` and gated by an extended `TestFlowCostRecords`.
   - verification: `fixture.sh` and `measure.sh` with self-tests, writing to `measurements.toml`.
   - mvp: opportunistic in-phase samples with no Go test. Execute-before is this phase's absorb task, execute-after is the verify-split task, verify-before is test-output-summaries' shipped mutation run, and verify-after is `--skip-mutation` in a worktree.
   - **Default:** risk's format, plus verification's fixture determinism (`fixture_tree`) and loaded-bytes definition.
   - **Why it matters:** `TestFlowCostRecords` already globs every `phases/*/flow-cost.toml` and pins the milestone c-6 for context-boundaries and test-output-summaries. A second record format would split the milestone's evidence. mvp's before and after legs measure different task shapes, so no delta between them can be attributed to the prompt.

2. **Where partials live.**
   - mvp: `assets/prompts/partials/<prompt>/<name>.md`.
   - risk and verification: top-level `_<prompt>.<name>.md`.
   - **Default:** top-level `_`.
   - **Checked:** 9 `os.ReadDir(assets/prompts)` walkers in tests skip entries with `IsDir()`, so they would silently stop checking moved text in a subdir. Those are prompt_test_command ×2, prompt_commit_gate, protected_prompts, ship_prompt ×2, subagent_offload_audit, and board_lifecycle_divergence ×2 (including the emit-set scan). Copy-mode `syncSkills` (install.go:254) skips dirs too.
   - Things that already handle a top-level `_` file: `commands_parity_test.go:88`, `interaction_coverage.go:114` and `subagent_offload_audit_test.go:27` skip `_` names (all correctly, since partials are not commands). `all:` embedding plus `TestEmbedDrift` already cover `_` files.
   - **Why it matters:** with a subdir, the test-pinned rules (board failure sentence, commit-green audit, emit-set) would quietly stop applying to moved text.

3. **How the test view composes prompt and partials.**
   - risk: `Composed` appends partials after the prompt, and order-sensitive TaskEdges is allowlisted.
   - mvp and verification: expand in place at the fetch line.
   - **Default:** in place.
   - **Why it matters:** `sectionOf(promptBody, "### 1e.", "### ")` (execute_prompt_test.go:383) and the order tests (TaskEdges, test before `git add`) keep working only if moved text lands where its branch lives.
   - risk's objection still holds for solo: the locked fetch point is §0, so in-place expansion puts solo §1f text in §0, and the solo tests are retargeted to the partial anyway.
   - Caveat from the code: `sectionOf` ends at the next `### `, so a partial carrying its own `### ` heading would truncate the slice it is expanded into.

4. **Exit status when the board fails inside begin or verify.**
   - risk: record first, then exit non-zero with a labelled `board: FAILED` line.
   - verification: exit 0 with a named line.
   - mvp: non-zero for verify; "reported, not fatal" for begin.
   - **Default:** risk.
   - **Why it matters:** every prompt teaches "a non-zero exit is a board failure", and `board_switch_prompt_test.go` pins that sentence. With exit 0, an agent that skims past the line drops the failure silently. With non-zero, the prompt has to tell a board failure apart from a refusal or a mutation-tool failure by its label, which t-3 and t-4 make testable.

5. **Whether the red path (exit 1) fetches a partial.**
   - mvp and verification: any non-zero `dross test` exit fetches.
   - risk: exit 1 stays inline, and only 2–8 ("the run did not happen") fetch.
   - **Default:** risk, against the 2–1 majority.
   - **Why it matters:** spec c-4 defines the common path as green → commit, so both readings meet the spec. But red is routine in a real pair task (test-output-summaries' own flow scenario goes red at §1e), and the milestone forbids added round-trips on a command's common path. Moving the red path would also cost bytes that t-13 then has to find elsewhere.

6. **Version on resume.**
   - mvp: begin always writes `<mm>.<ordinal>.0`. That is parity with today's execute.md ("internal resets to 0") and the global versioning rule.
   - risk and verification: write only when major.minor or patch differ, so internal survives a resume.
   - **Default:** preserve internal.
   - **Why it matters:** this changes current behaviour. Today `/dross-execute --from` mid-phase zeroes a quick task's internal bump. The user should confirm, because the global rule only says "phase execution start".

7. **How much of verify is absorbed into the CLI.**
   - mvp: only the uat sync, and it also syncs on `--detach` dispatch.
   - risk and verification: uat in finishVerify / `verify results` (never on dispatch), plus `current_phase_status=verified` and touch in finalize's RunE.
   - **Default:** risk and verification. The state write goes in the RunE, not in `finalizeVerify`, because ship.go:185 and phase.go:477 auto-heal through `finalizeVerify` and must not regress shipped → verified.
   - **Why it matters:** it is the cheapest zero-fetch byte saving, and the locked cli_absorb decision covers it.

8. **The instruction-conservation manifest (t-6).** Only verification has it.
   - **Default:** include it.
   - **Why it matters:** c-5's existing tests only catch phrases someone already asserted. Compressing to hit 15 KB can drop untested instructions silently.
   - **Cost:** every absorbed step needs a `// absorbs:` marker in Go source, and the manifest is one more file that a later prompt edit must respect. Dropping t-6 removes one task and those markers.

9. **Which branches go into which verify partials.**
   - risk: {detached, mutation-error, survivors}.
   - mvp: {detached, survivor-drain, not-pass}, with mutation-error and solo-review folded into not-pass.
   - verification: {detached, scope, drain, solo-review, not-pass}, with mutation-error inline.
   - **Default:** the union, six partials, one trigger each. That needs three verify split tasks (t-12, t-14, t-15) to stay ≤4 files each.
   - risk rejects any partial gated on `whole_file`. **Checked:** verification's scope trigger is the `scope degraded:` line, not whole_file. Gremlins' no-range-runner path sets `WholeFile` but appends nothing to `Degraded` (range_provenance.go:115–119), so that trigger stays off the Go common path. Both positions are kept: t-2 bans whole_file/gremlins triggers, and t-12 gates on `scope degraded:`.

10. **Trigger form.**
    - verification: a closed `key=value` vocabulary.
    - risk: a closed prose trigger set, checked in t-10 after both splits.
    - mvp: a When/If/Only-when prefix.
    - **Default:** verification's grammar, landed green in t-2, with risk's `mutation=failed` and `test_exit=not-run` values added.
    - **Why it matters:** only a closed vocabulary makes "the common path fetches zero" an executable assertion, and landing it early constrains every split as it is written, rather than auditing afterwards.

11. **When the budget test lands.**
    - risk: one pin task after both splits. Its other checks moved to t-2, t-7 and t-10 here, which would leave a one-file task.
    - verification: the execute row lands with the last execute split, and the verify row plus the set guard with the last verify split.
    - mvp: rows land with each split.
    - **Default:** verification.
    - All three agree it cannot land red first. Checked: the commit gate requires a full green `dross test` (execute.md §1f).

12. **Whether wave 1 is serialized behind the before-measurement.**
    - verification: t-1 runs alone and every task depends on it.
    - risk: t-1 runs in parallel with the Go tasks on a base-built binary.
    - **Default:** risk. Only prompt tasks (t-9 and later) depend on t-1.
    - **Why it matters:** the contamination path is the prompts dir, which is a symlink to `assets/prompts` (checked). t-2 and t-3 edit no prompt, and t-1 pins the binary to the 3ee0fe71 build. Serializing everything costs a wave of wall-clock for no protection.

13. **`TestTestStepPromptsNotGrown`.**
    - mvp: keeps reading the raw file, which the shrink makes trivially true.
    - risk: measures prompt plus partials against 25854.
    - **Default:** risk, which falls out of `rawPrompt` → Expand in t-7.
    - **Why it matters:** with risk's version the split cannot grow the total instruction set. The fetch lines and the reaction-to-begin prose have to fit inside what absorption frees.
