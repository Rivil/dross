# Plan Review — review-edge-lenses

Reviewed: 2026-11-14
Plan: 6 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-4's `TestParseVerdictEdgeHitBlocks` cannot fail from anything this phase changes. `ParseVerdict` already rejects a "pass" verdict that carries a BLOCKING quality finding: the generic check is at `internal/review/verdict.go:129-131` and the case is already pinned at `internal/review/verdict_test.go:28` ("pass carrying a quality BLOCKING"). `ParseVerdict` has no notion of an "edge-check" finding, and t-4 says it "is not changed", so the new test only repeats that existing case.
  Suggestion: Drop it, or replace it with an assertion on something t-4 actually adds, such as the reviewer's worked example showing an edge hit graded BLOCKING under "block".

- [test-contract] t-4 contract 5 contradicts itself: "the no-.dross-path assertion fails and TestReviewerBodyLimitsReads stays green". It names no test. The existing `TestReviewerBodyLimitsReads` (agentdef_test.go:110) only checks that a single sentence is present, so it cannot catch a new `.dross/` or verify.toml reference. As written, an executor can't tell which test owns the assertion.
  Suggestion: Name the test that holds the no-`.dross/`-path / no-verify.toml assertion over the new section.

- [test-contract] c-7 ("a hit fails its verdict") is proven only by prompt-text assertions in t-4 (shared `> Rule:` lines, BLOCKING needle, no `> Severity:` text). c-1/c-2 get a live proof run in t-6, but nothing shows the dross-task-reviewer actually returning "block" on the seeded fixture. Verify will map c-7 to wording tests alone.
  Suggestion: Accept this knowingly (the spec doesn't require a live c-7 run), or record in t-4's contract that c-7's evidence is textual so verify doesn't overclaim.

- [test-contract] t-1's language_scope contract says "if a new template omits `catch` or `recover` … TestNewLensesLanguageAgnostic fails." Taken literally, this makes the edge-case template mention catch/recover, which has nothing to do with value sets. The intent is clearly the silent-failure template.
  Suggestion: Scope the catch/recover requirement to the silent-failure template, and keep the `err != nil` / `.go` exclusions on both.

- [granularity] t-3 combines two separate changes across 5 files: (a) the two new `###` comment sections plus the SplitReview/Items tests (c-3), and (b) a new `--diff <patch>` invocation mode for /dross-review. That mode changes §0 (PR number no longer required, doctor skipped), §1 (fetch), §5 (post) and §6 (commit), plus the command's argument-hint. (b) is its own behaviour change with its own contracts (`TestReviewDiffModeNeverPosts`, `TestReviewDiffModeSkipsFetch`) and is only an enabler for t-6.
  Suggestion: Split --diff mode into its own wave-2 task that t-6 depends on, or keep them together on purpose and say why.

- [antipattern] `--diff` mode writes `review-diff.patch` and `review-comment.md` into `.dross/phases/review-edge-lenses/`, the same paths a real /dross-review of this phase's PR uses, and skips the §6 commit. After t-6 those fixture-run files are left as uncommitted `.dross` changes. Ship's clean-tree step auto-commits `.dross` changes, so the fixture comment would land in history as this phase's review record. On a phase that already has a real review, --diff would also overwrite it.
  Suggestion: Have t-3 send --diff output somewhere other than the phase's review-record paths, or have t-6 remove/restore those files after copying them into the fixture dir.

- [antipattern] t-6 can't be run by the `--solo` writer. Running `/dross-review` means a main-loop session spawning six subagents via the Task tool, and the dross-task-writer's tools are Read/Grep/Glob/Edit/Write/Bash with no Agent/Task. The plan doesn't say t-6 must run in pair mode or the main loop. If the phase is executed --solo, t-6 will either fail or the writer will make up the recorded comments.
  Suggestion: Mark t-6 explicitly as main-loop/pair-only in its description, so a solo run stops there and does not fake the proof.

## NOTE
- [coverage] All seven criteria are covered: c-1 (t-1, t-6), c-2 (t-1, t-6), c-3 (t-3, t-6), c-4 (t-2, t-3, t-6), c-5 (t-1, t-5), c-6 (t-1, t-6), c-7 (t-4). No task contradicts a locked decision. t-4 leaving the `severity` rule out of the reviewer is consistent: `severity` governs the lenses, and c-7 explicitly makes a reviewer hit blocking.

- [forbidden-actions] Only one hard rule exists (r-01, project). t-6 runs `make install` before the live run. No global rules.toml exists. No violations.

- [granularity] t-2 reaches the 5-file mark, but four of the files are tiny fixture sources plus one shape test. Splitting it would be artificial.

- [wave-order] The wave order holds up. t-4 needs t-1's `> Rule:` lines verbatim. t-5's `TestNoFourLensSurface` scans `assets/**`, which would stay red until t-1 removes "four" from review.md and the command. t-6 needs t-2's fixtures and t-3's sections and --diff mode.

- [antipattern] The seeded fixture (`fixtures/review-edge-lenses-c4/seeded/handler.go`) will be in this phase's own PR diff. Once the new lenses are installed, the phase's own /dross-review will flag its deliberate defects, and under the locked `severity` rule they may be graded BLOCKING because c-4 covers that path. Expect to reject those in /dross-respond. This is also the first Go source under `fixtures/`. The nested go.mod correctly keeps it out of `go test ./...`, `go vet ./...` and gremlins, but `gofmt -l .` still walks it, so the fixture files must be gofmt-clean. Nothing checks that the fixture compiles. The shape tests only parse it, so "error-returning call" in t-2's contract can only be checked as "a call result", not by type.

- [strengths] t-2 puts the value set in `order/status.go`, outside the reviewed diff, and pins that with a shape test. That directly tests the locked `value_set_source` decision ("value sets usually live outside the diff") instead of handing the lens an easy in-diff const block.

- [strengths] The near-miss pairs are well chosen: a silent default vs a loud default, and an uncommented discard vs one with a reason comment. Together with the go/parser-computed spans in t-6, which fail when the fixture shifts without a re-run, the c-4 proof can't silently drift away from what was recorded. This follows the `fixtures/multilang-c3/RUN.md` precedent.

- [strengths] t-1/t-4's `TestReviewerSharesLensRules` (verbatim `> Rule:` sentences shared between review.md and the task reviewer) turns c-7's "same rules" into a drift check instead of two hand-maintained copies. t-5's guard has a visited-count floor, so an empty glob can't pass vacuously.

## Summary
The plan is sound and fully covered with no locked-decision conflicts. Before execution, fix t-4's redundant and self-contradictory contracts, write --diff output somewhere other than the phase's review-record paths, and mark t-6 as main-loop/pair-only.
