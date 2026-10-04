# solo-task-review — panel synthesis

## Scores

| Dimension | Draft | Score | Basis |
|---|---|---|---|
| Criteria coverage | mvp | 3/5 | 8/8 is claimed, but c-6's "so /dross-verify and the PR body can show" is left unrendered (the draft admits this). c-5's "sees only covered criteria" is weakened because the reviewer runs `dross phase show --json`, which prints every criterion. The c-3 round cap is enforced by the prompt only. |
| Criteria coverage | risk | 5/5 | 8/8, and every locked decision is pinned by a test. c-6 is rendered in both the PR body and verify.md. c-5 is held by a definition test, a live canary, a scoped context and keeping the diff out of the executor's transcript. One gap: nothing checks at runtime that the spawn prompt carries only the context line. |
| Criteria coverage | verification | 5/5 | 8/8 including c-6 rendering. A fixed SpawnPrompt enforces c-5 at runtime, and the draft flags the c-5 constraints tension itself. Flaw: a failed task gets a TaskRecord with Files (via `changes record` without `--commit`), which widens verify's mutation scope (`internal/cmd/verify.go:92`, `:707`). |
| Test-contract specificity | mvp | 3/5 | It names the surfaces that break, but the edge cases are thin. It has no corrupt-record or closed-posture rows, no check that the diff is independent of git config, and no canary proving omitClaudeMd works. |
| Test-contract specificity | risk | 5/5 | Every line names a test and the surface that breaks it. Tables pin the ledger states, digest parity, config-independent patches, the index left untouched, secret withholding and the closed posture. |
| Test-contract specificity | verification | 4/5 | Sharp and code-observable, with the strongest c-5 runtime row (TestRecorderRejectsWidenedPrompt). Several tests go unnamed ("the record test", "the unborn test", "the pair test"), and it has no rows for diff config or secret paths. |
| Granularity | mvp | 2/5 | t-3 puts a human-in-the-loop fixture capture, the recorder, the gate and a commit.go refactor in one task, with the go/no-go buried mid-task. t-6 covers two prompts, and t-5 covers install and doctor across 6 files. |
| Granularity | risk | 4/5 | Tasks are 2–5 files in one layer. Exception: t-16 bundles two unrelated prompts (quick.md for c-8, verify.md for c-6), which drags verify.md to wave 5. |
| Granularity | verification | 5/5 | One concern per task, and each prompt has its own task. The smallest tasks (t-6, t-16) still own distinct contracts. |
| Wave correctness | mvp | 4/5 | Every dependency is real. But the gate (t-3, wave 2) arms before the prompts (wave 3) teach the review loop, so this repo's own solo commits are gated after `make install` (r-01). |
| Wave correctness | risk | 5/5 | Every depends_on is real. The agent and recorder come before the prompts. The gate is the last id in wave 5, so `make install` arms it only after execute.md and quick.md teach the loop. |
| Wave correctness | verification | 4/5 | Dependencies are correct, but the gate (t-12, wave 4) lands before execute.md and quick.md (wave 5), the same r-01 exposure as mvp. |

**Skeleton: risk.** It has the most specific contracts and the only wave order that respects r-01 on both sides. It is also the only draft whose reviewer has no Bash, so it cannot change the tree it reviews. Its context is scoped, independent of git config, secret-withholding and checked by digest.

## Merged plan

```
Phase solo-task-review — 18 tasks across 5 waves

Wave 1
  t-1  Capture real Agent PostToolUse payloads                                  [risk+verification]
       files:      internal/gate/testdata/agent_reviewer_post.json (new),
                   internal/gate/testdata/agent_background_post.json (new), internal/gate/fixture_test.go
       covers:     c-1, c-5
       depends_on: —
       desc:       Human-approved go/no-go in a scratch repo: a scratch reviewer agent (omitClaudeMd, tools Read), a CLAUDE.md canary and a
                   temporary stdin-dump PostToolUse hook in .claude/settings.local.json (never ~/.claude). Capture one foreground spawn (told
                   to quote every instruction) and one background spawn. STOP before t-10 if no final text, no subagent_type, or canary leaks.
       contract:   - if the reviewer fixture stops being a real capture (session_id, transcript_path, toolu_ id) with
                     tool_name Agent or Task, tool_input.subagent_type dross-task-reviewer, and exactly one dross-verdict
                     fence readable from tool_response alone, TestCapturedAgentReviewerFixture fails
                   - if the scratch CLAUDE.md canary appears anywhere in the captured reviewer reply,
                     TestCapturedReviewerIsolation fails (omitClaudeMd not honoured; re-decide reviewer_isolation)
                   - if the background fixture's tool_response carries a dross-verdict fence, TestCapturedBackgroundAgentFixture fails

  t-2  Parse reviewer verdicts and track fix rounds                             [risk; spec/quality split from mvp+verification]
       files:      internal/review/verdict.go (new), internal/review/verdict_test.go (new), internal/review/ledger.go (new),
                   internal/review/ledger_test.go (new), internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:     c-2, c-3, c-6
       depends_on: —
       desc:       ReviewerAgent = "dross-task-reviewer". ParseVerdict reads one ```dross-verdict JSON fence {verdict, spec[], quality[]
                   (severity BLOCKING|FLAG|NOTE)}; spec findings always block; pass is derived and cross-checked against the stated verdict.
                   Ledger Round{tree, digest, outcome pass|block|unavailable|stale, findings, cause}; State(); Resolve() labels every finding.
       contract:   - parse table: zero fences, two fences, malformed JSON, unknown severity, a spec finding with no criterion,
                     an empty finding text → each errors; "pass" carrying a quality BLOCKING, or "block" carrying only
                     FLAG/NOTE → an error naming the inconsistency, never a pass — TestParseVerdict
                   - if a spec finding tagged FLAG or NOTE stays non-blocking, or a "pass" carrying it parses as a pass,
                     TestSpecFindingsAlwaysBlock fails (c-2)
                   - if ParseVerdict returns spec and quality findings in one list instead of separate Spec and Quality
                     fields, TestFindingsKeepKind fails (c-2 "reports … separately")
                   - if quality FLAG/NOTE start blocking, TestQualityGrades fails: "pass" with one FLAG and one NOTE is a pass
                     that keeps both (quality_blocking)
                   - ledger table: [] none; [pass T1] pass(T1); [block] blocked; [block, pass T2] pass(T2); [block, block]
                     exhausted; [pass T1, block, block] exhausted; [block, stale, block] exhausted; [block, block, pass]
                     exhausted; [unavailable, pass] unavailable — any drift fails TestLedgerState (c-3)
                   - if Resolve mislabels, TestResolve fails: [block{B1,F1}, pass{N1}] → B1 "fixed in the fix round", F1 and
                     N1 "non-blocking, left"; [block{B1}, block{B2}] → B2 "unresolved — task failed"
                   - an unfiled json-tagged string field in internal/review fails TestEveryPathShapedFieldIsDeclared

  t-3  Build the review context from the whole diff                             [risk; tree-match + undeclared-edit rows from verification]
       files:      internal/treefp/treefp.go, internal/treefp/treefp_test.go, internal/review/context.go (new),
                   internal/review/context_test.go (new)
       covers:     c-1, c-5, c-8
       depends_on: —
       desc:       treefp gains Base (HEAD minus .dross/; empty tree if unborn), a config-independent Patch (`git diff-tree -p --no-color
                   --no-ext-diff --no-textconv --no-renames`) and Dirty. review.BuildContext renders the task record (no status), covered criteria
                   only, locked decisions + hard rules (or a quick's description), then the patch; secret paths named, content withheld.
       contract:   - if a staged, unstaged or untracked-unignored file, or an edit to a file no task declares, is missing from
                     the patch, or an ignored file or any .dross/ path appears, TestPatchWholeDiff fails (review_input)
                   - if BuildContext's tree differs from treefp.WorkingTree() on the same repo,
                     TestContextTreeMatchesWorkingTree fails (the pass would vouch for a tree the gate never compares)
                   - if color.ui=always, diff.external=<marker script>, a .gitattributes textconv driver, diff.noprefix=true
                     or diff.renames=copies changes the context bytes, adds ANSI escapes or creates the marker,
                     TestPatchConfigIndependent fails
                   - if an unborn HEAD does not show every file as added against the empty tree, TestPatchUnbornHead fails
                   - if the real index's sha256 or `git diff --cached --name-only` differs across BuildContext and Dirty in a
                     partially staged repo, TestContextLeavesIndexAlone fails
                   - if a withheld path's content appears (its path must appear), TestContextWithholdsSecretPaths fails
                   - if an uncovered criterion's text, the task's status or an unlocked decision appears, or flipping the task
                     pending→in_progress changes the digest, TestContextScope fails; a quick scope carries the description
                     and no plan content
                   - if two builds on an unchanged tree differ in digest, or a one-byte edit leaves it equal, TestContextDigest fails
                   - if Dirty reports a .dross/-only change or misses an untracked unignored file, TestDirtyIgnoresDross fails

  t-4  Record a failure reason on tasks                                         [risk]  (see D9)
       files:      internal/phase/phase.go, internal/cmd/task.go, internal/cmd/task_reason_test.go (new),
                   cmd/dross/testdata/cli_tree.txt (regenerated golden), internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:     c-3
       depends_on: —
       desc:       Task.Reason (toml reason,omitempty). `dross task status <p> <t> failed --reason <text>` stores it, any
                   other status clears it, --reason with a non-failed status errors, and `task show` prints it.
       contract:   - if `task status p t-2 failed --reason "x"` does not survive a plan.toml round-trip (newline, quotes and `]]`
                     included), or `task show` omits it, TestTaskFailedReason fails
                   - if setting t-2 back to pending or in_progress keeps the reason, TestReasonClearedOnRetry fails
                   - if --reason with done or pending is accepted instead of erroring, TestReasonOnlyWithFailed fails
                   - if a status write leaves a reason-less plan with a new `reason` key, TestReasonOmitEmpty fails
                   - an unfiled phase.Task.Reason fails TestEveryPathShapedFieldIsDeclared; a missing `--reason` row fails
                     TestCLITreeGolden

  t-5  Add a reviews map to the change record                                   [risk; round-trip row from verification]  (see D8)
       files:      internal/changes/changes.go, internal/changes/changes_test.go,
                   internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:     c-6
       depends_on: —
       desc:       Changes.Reviews map[taskID]TaskReview{outcome, rounds, findings[{round, kind, severity, criterion, text,
                   resolution}]} (json reviews,omitempty), on changes' own types: importing internal/review would cycle,
                   since phase/done.go imports changes. SetReview writes only that entry, and Record() never touches Reviews.
       contract:   - if a changes.json fixture without reviews changes a byte across Load→Save, TestReviewsOmitEmpty fails
                   - if Record(t-3, …) after SetReview(t-3) drops the review, or SetReview creates a Tasks entry (a failed task
                     has no TaskRecord, so its files stay out of verify's mutation scope), TestReviewsSeparateFromTasks fails
                   - if SetReview on a missing changes.json does not create one holding only phase + reviews, or clobbers pr, base,
                     base_commit or status on an existing one, TestSetReviewPreservesRecord fails
                   - if a TaskReview does not round-trip through Save/Load with kind, severity and resolution intact,
                     TestReviewsRoundTrip fails
                   - an unfiled new tagged field fails TestEveryPathShapedFieldIsDeclared

Wave 2
  t-6  Store review, quick and context records                                  [risk+verification+mvp]
       files:      internal/gatestate/store.go, internal/gatestate/store_test.go, internal/gate/override_test.go,
                   internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:     c-4, c-8
       depends_on: t-2
       desc:       New .dross/gate/ records: review.json {kind task|quick, phase, task, attempt, rounds []review.Round}, quick.json {mode,
                   description, head, at} and review-context.md, on the store's atomic temp+rename: missing = none, corrupt = error naming
                   the path, git-tracked = refused unread. RemoveQuick treats none as success.
       contract:   - if a truncated review.json or quick.json decodes to a zero record instead of an error naming
                     .dross/gate/<file>, or a missing one returns anything but (nil, nil), TestReviewRecordDecode fails
                   - if a pass round with an empty tree is accepted, TestReviewEmptyTreeRejected fails (an empty tree matches
                     only another empty fingerprint)
                   - if spec and quality findings collapse into one list on disk, TestReviewRoundTrip fails
                   - if N concurrent SaveReview writers and LoadReview readers ever observe a partial record, the concurrency test fails
                   - if a force-added review.json or quick.json is read instead of refused, TestTrackedRecordRefused fails
                   - if Write/Edit of .dross/gate/review.json, quick.json or review-context.md, or Bash
                     `echo x > .dross/gate/review.json` / `cp /tmp/r .dross/gate/review-context.md`, gets past
                     tamper-guard, the tamper table fails
                   - unfiled tagged fields fail TestEveryPathShapedFieldIsDeclared

  t-7  Ship the isolated reviewer definition                                    [risk+mvp+verification; tool set from risk]
       files:      assets/agents/dross-task-reviewer.md (new), assets/embed.go, assets/embed_test.go,
                   internal/review/agentdef_test.go (new)
       covers:     c-5, c-2, c-7
       depends_on: t-2
       desc:       Frontmatter: name dross-task-reviewer, omitClaudeMd: true, tools Read/Grep/Glob, no memory, no model. Body: Read the named
                   context file (no verdict if its digest differs); a covered criterion's sub-surface with no code or test is a spec finding
                   (always blocks); grade quality BLOCKING/FLAG/NOTE; end with one dross-verdict fence. embed.go gains `all:agents`.
       contract:   - if omitClaudeMd is removed or false, a `memory` key appears, a `model` other than inherit appears, `tools`
                     is dropped (inherit-all) or gains Bash, Edit, Write, MultiEdit, NotebookEdit, Agent, Task, WebFetch or
                     WebSearch, TestReviewerDefinitionIsolation fails (c-5)
                   - if the frontmatter name drifts from review.ReviewerAgent, TestReviewerNameMatchesRecorder fails
                   - if the body's worked example stops parsing with review.ParseVerdict into at least one spec and one quality finding,
                     or the body stops saying spec findings always block and naming the three quality grades,
                     TestReviewerBodyMatchesParser fails
                   - if the embedded assets/agents set differs from disk (a narrowed embed pattern), TestEmbedDrift fails

  t-8  Render reviewer findings in the PR body                                  [risk+verification]  (see D7)
       files:      internal/ship/body.go, internal/ship/body_test.go, internal/cmd/ship.go,
                   internal/boardsync/toolfence_composer_test.go
       covers:     c-6
       depends_on: t-5
       desc:       BuildPRBody(spec, v, reviews) adds "## Reviewer findings" before the footer: per sorted task id, its outcome and each
                   finding's severity, kind, criterion, text, resolution (one line, pipe-escaped, capped "… N more in changes.json"); none
                   when empty. ship.go passes changes.Reviews; the composer canary test (line 95) moves to the new signature.
       contract:   - if nil or empty reviews change today's body by a byte, the existing TestBuildPRBody* tests fail
                   - if finding text "x\n## Fake" or "a|b" yields a new heading line or an unescaped pipe,
                     TestReviewSectionEscapes fails
                   - if 500 findings of 2 KB each produce a body over 60,000 chars, or the tail miscounts the dropped findings,
                     TestReviewSectionCapped fails
                   - if two calls on the same reviews map differ, or a timestamp appears, TestReviewSectionStable fails
                   - if `dross ship --print-body` on a phase whose changes.json has reviews omits the section,
                     TestShipPrintBodyShowsReviews fails

  t-9  Surface reviewer findings in verify.md                                   [verification+risk]  (see D17)
       files:      assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:     c-6
       depends_on: t-4, t-5
       desc:       §0 reads changes.json `reviews`. §4's report gains a "Solo review:" block giving each reviewed task's
                   outcome, its findings and their resolutions with unresolved ones flagged, and each failed task with its `task show` reason.
       contract:   - if verify.md stops reading the `reviews` key, or §4's template loses the "Solo review:" block, a finding's
                     resolution or a failed task's reason, TestVerifyShowsReviews fails (c-6)

Wave 3
  t-10 Record reviewer verdicts from PostToolUse                                [risk; widened-prompt row from verification, forged-record row from mvp]
       files:      internal/gate/review.go (new), internal/gate/review_test.go (new)
       covers:     c-1, c-2, c-3, c-4, c-5, c-8
       depends_on: t-1, t-2, t-3, t-6
       desc:       ArmedScope: solo quick at HEAD, else solo execute task (one in_progress, on phase/<id>), else nil. Recorder claims Agent/Task
                   calls for review.ReviewerAgent, regenerates the context, compares digests: pass/block bound to its tree, stale (digest
                   mismatch, no context line), unavailable (parse failure, widened prompt). Background or unarmed: records nothing.
       contract:   - from t-1's captured envelope carrying a pass verdict and a matching context line, in a solo-armed fixture
                     repo, the recorder appends a pass round whose tree equals treefp.WorkingTree; subagent_type
                     general-purpose, fork or absent, or tool_name Bash, writes nothing — TestRecorderClaims
                   - if a verdict declaring "pass" with one spec finding records a pass round, TestRecorderSpecBlocks fails (c-2)
                   - if a prompt carrying the context line plus any other text (e.g. pasted conversation) records anything
                     but an unavailable round naming the prompt, TestRecorderRejectsWidenedPrompt fails (c-5)
                   - if a file edited after `dross review context` and before the verdict, or a prompt with no context line,
                     yields anything but a stale round plus a warning naming `dross review context`,
                     TestRecorderStaleContext fails
                   - if an unparseable reply records anything but an unavailable round naming the parse error, or a later pass
                     for the same attempt reads as pass, TestRecorderUnavailableSticky fails (review_unavailable)
                   - if t-1's background fixture writes review.json or emits other than one warning,
                     TestRecorderBackgroundLaunch fails
                   - if a reviewer run with execute.json pair or absent, no task in_progress, or HEAD off phase/<id> writes
                     review.json, TestRecorderPairSilent fails (c-1)
                   - scope table: t-3's rounds then t-4 in_progress → fresh record; HEAD moved for the same task → fresh
                     record; solo quick at HEAD → quick record; quick.json pair while t-3 is solo in_progress → still t-3;
                     two tasks in_progress → ArmedScope errors naming both — TestArmedScope
                   - if any non-test file outside internal/gate calls gatestate.SaveReview, TestReviewRecordSingleWriter
                     fails; the same test asserts Bash `dross gate record` fed a forged reviewer payload stays refused by
                     gate-off-guard (review_pass_signal)
                   - if either tool name "Agent" or "Task" stops being claimed, TestRecorderToolRename fails

  t-11 Install the reviewer and check readiness                                 [risk+mvp+verification]
       files:      internal/cmd/install.go, internal/cmd/reviewer_agent.go (new), internal/cmd/install_agent_test.go (new),
                   internal/cmd/cmd_test.go
       covers:     c-7
       depends_on: t-7
       desc:       agentsDir() = $CLAUDE_CONFIG_DIR/agents else ~/.claude/agents: copy or symlink there, prune dropped dross-*.md, never touch
                   others; chdir() seeds a current reviewer. ReviewerStatus (shared by doctor and solo begin) → ok / missing (incl. dangling
                   link) / stale (bytes through any link ≠ embedded) / shadowed (repo .claude/agents copy).
       contract:   - if copy mode leaves other than the embedded bytes, link mode other than a symlink to
                     <src>/agents/dross-task-reviewer.md, or a mode switch leaves the old form, TestInstallReviewer fails
                   - if a second install changes the definition's bytes, TestInstallReviewerIdempotent fails
                   - if install with CLAUDE_CONFIG_DIR=X writes under $HOME/.claude/agents, or ReviewerStatus reads a different
                     dir than install wrote, TestReviewerDirAgreement fails
                   - if a foreign agents/my-agent.md changes bytes or mtime, or a stale agents/dross-old.md survives,
                     TestInstallAgentPrune fails
                   - status table: absent → missing; dangling symlink → missing; one byte changed → stale; repo
                     .claude/agents/dross-task-reviewer.md → shadowed naming that file; fresh install → ok — TestReviewerStatus
                   - if chdir() stops seeding a current reviewer, the existing TestExecuteBeginRecordsMode and Doctor() tests
                     go red once t-14/t-15 land

  t-12 Attach reviews and guard failed-task marking                             [risk; quick-record row from verification]
       files:      internal/cmd/changes.go, internal/cmd/task.go, internal/cmd/review_attach.go (new),
                   internal/cmd/review_attach_test.go (new)
       covers:     c-6, c-3
       depends_on: t-2, t-3, t-4, t-5, t-6
       desc:       `changes record <p> <t>` and `task status <p> <t> failed` copy review.json's rounds for (p, t), resolved by review.Resolve,
                   into changes.Reviews. They never take review content from a flag. Without --reason, `failed` derives the reason from an exhausted or
                   unavailable record. In a solo run, `failed` refuses while treefp.Dirty reports code changes and names the stash command.
       contract:   - with review.json [block, pass] for (p, t-3), `changes record p t-3 --files a.go --commit abc` writes
                     reviews.t-3 outcome pass, rounds 2, B1 "fixed in the fix round", FLAGs "non-blocking, left"; a record for
                     t-2, another phase, or a quick record attaches nothing — TestChangesRecordAttachesReview
                   - if a pair-mode task's changes.json gains a "reviews" key, TestPairRecordHasNoReview fails
                   - if `task status p t-3 failed` (no --reason) over an exhausted record does not set the derived reason and
                     attach "unresolved — task failed", over an unavailable record does not name the cause, or with --reason "x"
                     does not keep "x", TestFailedReasonFromReview fails
                   - if `failed` succeeds in a solo run while a.go is modified or an untracked b.go exists, or refuses when only
                     .dross/ is dirty or the run is pair, TestSoloFailedRequiresStash fails
                   - if a corrupt review.json makes `changes record` exit non-zero or skip the task record, TestAttachErrorWarns fails
                   - if `changes record` or `task status` gains a flag carrying review content, TestNoReviewFlags fails

Wave 4
  t-13 Add the dross review context and status verbs                            [risk; `review status` from verification]
       files:      internal/cmd/review.go (new), internal/cmd/review_test.go (new), cmd/dross/main.go,
                   cmd/dross/testdata/cli_tree.txt (regenerated golden)
       covers:     c-1, c-3, c-5, c-8
       depends_on: t-3, t-6, t-10
       desc:       `dross review context` writes .dross/gate/review-context.md for the ArmedScope, withholding secret paths. It prints only the path,
                   digest, subagent_type and exact prompt line, never the diff, and refuses when unarmed or with an empty code diff.
                   `dross review status` is read-only. It prints the scope's ledger State() and whether a pass stands for the current WorkingTree.
       contract:   - if the printed digest differs from the one t-10's recorder regenerates for the same armed scope (including
                     with a secret path added via gates.toml), TestContextDigestParity fails
                   - if stdout carries any line of the diff, TestContextVerbPrintsNoDiff fails
                   - if pair mode, no task in_progress, or a .dross/-only diff writes a context file instead of erroring (pair
                     naming "the human is the gate"), TestContextVerbRefuses fails
                   - if `review context` changes the index, state.json or plan.toml bytes, or `review status` changes any byte
                     under .dross/, TestReviewVerbsNoSideEffects fails (no verb records a pass: review_pass_signal)
                   - if `review status` reports passing for an exhausted ledger ([block, block, pass]) or for a pass whose
                     tree has since changed, TestReviewStatusAgreesWithLedger fails
                   - if cli_tree.txt stops listing `review context` and `review status`, TestCLITreeGolden fails

  t-14 Add quick begin/end and solo readiness                                   [risk+verification]  (see D12, D14)
       files:      internal/cmd/quick.go (new), internal/cmd/execute.go, internal/cmd/solo_begin_test.go (new),
                   cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt (regenerated golden)
       covers:     c-8, c-1
       depends_on: t-6, t-11
       desc:       `dross quick begin [--solo] <description>` writes quick.json {mode, description, head, at}, and `dross quick end`
                   removes it. `quick begin --solo` and `execute begin --solo` refuse unless ReviewerStatus is ok, naming the
                   status and `dross install`. `execute begin`, in either mode, removes quick.json.
       contract:   - if `quick begin --solo "fix x"` records other than mode solo, the description verbatim and the current
                     HEAD, or `quick begin "x"` records other than mode pair, or an empty or whitespace-only description is
                     accepted, TestQuickBegin fails
                   - if `execute begin p --solo` or `quick begin --solo x` succeeds with the reviewer missing, stale or
                     shadowed, or the error omits `dross install` (shadowed: the repo file), TestSoloBeginNeedsReviewer fails;
                     pair begins succeed regardless
                   - if `execute begin p` leaves an earlier quick.json in place, or `quick end` with none errors,
                     TestQuickMarkerLifetime fails
                   - if `quick begin` or `quick end` changes the sha256 of state.json or any plan.toml, TestQuickNoClobber fails
                   - the existing TestExecuteBeginRecordsMode / TestExecuteBeginLeavesStateAlone pass unedited; cli_tree.txt
                     must list `quick begin --solo` and `quick end`

  t-15 Report the reviewer in dross doctor                                      [risk+mvp+verification]
       files:      internal/cmd/doctor.go, internal/cmd/doctor_reviewer_test.go (new),
                   internal/cmd/testdata/cli_surface/doctor_run.txt (regenerated golden)
       covers:     c-7
       depends_on: t-11
       desc:       A Reviewer section renders ReviewerStatus: ✓ for ok, or a ✗ issue (non-zero exit) for missing, stale or shadowed. Each issue
                   names the path and the fix: `dross install`, `make install` for a link-mode dev checkout (r-01), or the
                   repo file to remove when shadowed.
       contract:   - if doctor exits 0 with the definition deleted, one byte changed, a dangling symlink, a link to a source
                     file edited past the binary, or a repo .claude/agents/dross-task-reviewer.md, TestDoctorReviewer fails;
                     each line names the path and the fix
                   - if doctor reads $HOME/.claude/agents while CLAUDE_CONFIG_DIR points elsewhere, TestDoctorReviewerDir fails
                   - if a fresh install is not exit 0 with ✓, or doctor_run.txt stops pinning the section, the golden fails

Wave 5
  t-16 Run the reviewer in solo execute                                         [risk+verification+mvp]
       files:      assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       covers:     c-1, c-2, c-3, c-6
       depends_on: t-12, t-13, t-14
       desc:       §0: refused `execute begin --solo` → ask for `dross install`. §1f solo, after green `dross test`: `dross review context`,
                   FOREGROUND spawn with the printed line, branch on `dross review status`: pass → commit; blocked → one fix round; still
                   blocked/unavailable → `git stash push -u -- . ':(exclude).dross'`, `task status … failed`, `task next`. Pair: none.
       contract:   - if `dross review context`, the dross-task-reviewer spawn and the `dross review status` read stop appearing
                     after §1f's bare `dross test` and before `git commit` in the solo branch, or appear in the pair branch or
                     §1c, TestExecuteSoloReviewPlacement fails (c-1)
                   - if the spawn's subagent_type differs from review.ReviewerAgent, the prompt is not the printed line
                     verbatim, or the spawn paragraph permits run_in_background, TestExecuteReviewerSpawnForeground fails
                   - if the fix round may run more than once, skips re-running `dross test` before the re-review, the failed
                     path marks failed before stashing, the stash pathspec does not exclude .dross, or the loop stops instead
                     of `dross task next`, TestExecuteReviewFailurePath fails (c-3)
                   - if the solo red-test path marks failed without stashing first, TestExecuteSoloRedStashes fails
                   - TestPromptCommitsSatisfyGreenGate must still match §1f; TestNoPromptTellsAgentToLiftGate stays green

  t-17 Review solo quicks                                                       [verification+risk]
       files:      assets/prompts/quick.md, internal/cmd/quick_prompt_test.go
       covers:     c-8
       depends_on: t-13, t-14
       desc:       §0 runs `dross quick begin "<description>"`, adding --solo in solo mode, before §3. Solo §4→§5: after the green `dross test`,
                   run review context, spawn the reviewer in the foreground, and branch on review status, with one fix round. Still blocked or unavailable: discard,
                   `dross quick end`, report the findings, no bump. Every abort path runs `dross quick end`. Pair mode spawns none.
       contract:   - if quick.md's solo branch loses `dross review context` and the foreground dross-task-reviewer spawn between
                     `dross test` and `git commit`, or the pair branch gains a spawn, TestQuickSoloReview fails (c-8)
                   - if `dross quick begin` (with --solo only on the solo branch) stops preceding §3 Implement, or an abort
                     path (solo red, blocked review, user abort) omits `dross quick end`, TestQuickMarkerClosed fails
                   - if the blocked path allows a second fix round, or omits discard, `dross quick end` or no-version-bump,
                     TestQuickSoloReviewBlocked fails

  t-18 Gate solo commits on a recorded review                                   [risk+verification+mvp]  (see D6)
       files:      internal/gate/commit.go, internal/gate/soloreview.go (new), internal/gate/soloreview_test.go (new),
                   internal/cmd/gate_override_test.go, README.md
       covers:     c-4, c-8, c-3
       depends_on: t-10, t-13, t-14
       desc:       Extract commit-green's candidate builder so both gates share it. solo-review (Workflow, Liftable): while armed, a code
                   commit needs a pass in that scope's ledger with tree == candidate, and a .dross/-only commit passes. While armed with dirty code it refuses
                   a non-solo `execute begin`/`quick begin` and `quick end`. README gets a gate row. This is the last id, so `make install` arms it only after t-16/t-17.
       contract:   - with t-3 solo in progress: a pass for candidate T passes; no record refuses naming `dross review context`
                     and dross-task-reviewer; a pass for T then a one-byte edit refuses "tree changed since the review";
                     blocked names the fix round; exhausted or unavailable names `dross task status <p> t-3 failed` and
                     repeats the recorded cause; a pass for t-2 refuses — TestSoloReviewGate (c-4, c-3)
                   - if a .dross/-only commit while armed refuses, or pair mode, no task in_progress or HEAD off phase/<id>
                     refuses or spawns any treefp git argv (gitrun.ArgvRecorder), TestSoloReviewSilent fails (c-4)
                   - quick table: solo quick at HEAD gated against its quick record; a pass from another quick attempt
                     refuses; pair quick silent; silent once the quick's commit moves HEAD — TestSoloReviewQuick (c-8)
                   - if solo-review and commit-green judge `git add a.go && git commit -m x` against different candidates,
                     TestSharedCandidate fails; the existing commit_test.go suite stays green across the extraction
                   - downgrade table: armed + a.go dirty → `dross execute begin p`, `dross quick begin "x"`, `dross quick end`
                     refuse; armed + only .dross/ dirty → pass; `dross execute begin p --solo` → pass — TestSoloDowngradeGuard
                   - if a corrupt review.json or quick.json, an unreadable plan.toml, or a failing candidate build while armed
                     passes instead of refusing with the error named, TestSoloReviewClosedPosture fails
                   - TestGateRegistryNames gains solo-review (nine names); a README gate row missing a registered workflow
                     gate fails TestReadmeNamesEveryGate (new); TestGateRemediesResolve fails if a remedy names a verb
                     missing from newRoot()
```

Coverage: c-1 t-1,3,10,13,14,16 · c-2 t-2,7,10,16 · c-3 t-2,4,12,13,16,18 · c-4 t-6,10,18 · c-5 t-1,3,7,10,13 · c-6 t-2,5,8,9,12,16 · c-7 t-7,11,15 · c-8 t-3,6,10,13,14,17,18. 8/8.
Locked decisions: quality_blocking t-2/t-7 · reviewer_model t-7 · review_pass_signal t-10 (single writer, gate-off-guard), t-6 (tamper rows), t-13 (verbs record nothing) · reviewer_isolation t-1 (canary) + t-7 · review_input t-3 (see D5) · review_unavailable t-10 (sticky), t-16, t-18 (closed posture).

## Disagreements

**D1. How big the plan is: mvp's 6 tasks against 17 (risk) and 18 (verification).**
- mvp gets 8/8 coverage by putting work into prompts and leaving some of it unbuilt. Risk and verification enforce in code the things mvp leaves to prompts.
- Default: 18 tasks, using the risk skeleton plus verification's split of verify.md from quick.md.
- Why it matters: mvp's 6 leave out the c-6 rendering, a code-level round cap, closed-posture records, the canary proof of omitClaudeMd and a config-independent diff. The price is about 3× the tasks and a much longer run.

**D2. How the reviewer gets its input, and therefore what tools it has.**
- risk: `dross review context` writes a tamper-guarded context file. The reviewer is Read/Grep/Glob only and gets a digest line in its prompt, which the recorder regenerates and checks.
- verification: a Bash-capable reviewer runs `dross review packet`. Its prompt must be exactly SpawnPrompt, and its verdict echoes the tree.
- mvp: the reviewer collects its own input through `dross task show`, `dross phase show --json` and `git diff HEAD`, which shows it uncovered criteria and a diff shaped by repo git config.
- Default: risk's design, plus verification's check that rejects a widened prompt.
- Why it matters: only the default gives a reviewer that cannot touch the tree and a context that is scoped and withholds secrets. The cost is t-3 and t-13.

**D3. Where the one-fix-round cap is enforced.**
- risk puts it in the recorder's ledger: a second blocked round marks the task exhausted, and stale rounds don't count.
- verification puts it in the gate: a pass counts only at round n ≤ 2.
- mvp leaves it to the prompt.
- Default: risk's ledger, with the gate reading State().
- Why it matters: with a prompt-only cap, the agent can keep re-running the reviewer until it passes, which makes c-3 advisory. Risk and verification agree it belongs in code and differ only on where.

**D4. Shared tension: the reviewer's context includes locked decisions and hard rules.**
- All three drafts include them. Verification flags that c-5 says the reviewer "sees only the task record, covered criteria text and the task's diff".
- Default: keep them, because quality_blocking's "locked-decision or rule violation" grade can't be judged without them.
- Why it matters: verify may judge c-5 unmet under a strict reading. The alternatives are to drop the constraints section, so decision violations go unflagged, or to amend c-5.

**D5. The review patch leaves out .dross/, against review_input's "whole uncommitted diff against HEAD".**
- risk and verification exclude .dross/. Verification flags this as possibly narrowing the locked decision. mvp's `git diff HEAD` includes it.
- Default: exclude it. This matches treefp's fingerprint domain (the treefp.go header says ".dross/ taken out"), so the pass binds to exactly what was reviewed.
- Why it matters: it narrows a locked decision's literal text, so a human should confirm before verify judges it.

**D6. Whether the gate lands before or after the prompts (r-01).**
- risk makes the gate the last id in wave 5, after execute.md and quick.md.
- verification puts the gate in wave 4, before the prompts. mvp puts it in wave 2 and argues instead that prompts must wait for the agent and recorder.
- Default: risk's order. The agent, recorder and install still come before the prompts, which also meets mvp's concern.
- Why it matters: if the gate lands first, `make install` arms solo-review on this repo while no prompt teaches the review loop. The phase's own later solo commits would then be refused.

**D7. Showing c-6 findings in the PR body and verify.md.**
- risk and verification render them (t-8, t-9). mvp stores them only and explicitly expects to be challenged on that.
- Default: render them (include-first). c-6 names both surfaces.
- Why it matters: rendering means changing BuildPRBody's signature (ship.go:238 and the composer canary test). Without it, c-6 holds only through `dross changes show`.

**D8. Where reviews live in changes.json.**
- risk keeps a separate `Reviews` map. mvp and verification add `TaskRecord.review`.
- Default: the separate map. verify.go:92/:707 feed every `ch.Tasks[*].Files` into the mutation scope, so a failed task's TaskRecord would add uncommitted, stashed files to the scope.
- Why it matters: under the other shape, a failed task needs a TaskRecord, and verification's t-13 does create one via `changes record` without `--commit`.

**D9. Where a failed task's reason is stored.**
- risk: a new plan.toml `Task.Reason` plus `task status failed --reason`, derived from review.json when the flag is omitted.
- verification: `review.reason` in changes.json, plus `--review-unavailable`.
- mvp: `changes record --notes`.
- Default: risk's. It is the literal reading of c-3 ("marked failed with the finding as its reason"), and `task show` shows it.
- Why it matters: it is a plan.toml schema change that the other two drafts both rejected.

**D10. Guard against downgrading to pair mid-task.**
- risk and verification refuse a non-solo `execute begin`/`quick begin` and `quick end` while the run is armed and code is uncommitted.
- mvp rejects the guard as outside the threat model (`shell_detection_depth`).
- Default: include it (t-18).
- Why it matters: without it, re-running begin in pair mode skips the review on the very next commit. It mirrors pair-approval's existing refusal of a mid-task switch to solo.

**D11. Which wins when a solo execute and a solo quick are both armed.**
- risk: the quick wins, and `execute begin` clears quick.json.
- mvp: the execute run wins.
- verification: it is an error naming both.
- Default: risk's.
- Why it matters: mvp's concern stands. A solo quick that aborted without `quick end`, at the current HEAD, would gate a pair execute that was already running. Verification's error is closed, but the remedy, `quick end`, is itself refused by D10's guard while code is dirty.

**D12. How the quick-mode marker is set.**
- risk and verification: `dross quick begin/end` (a new top-level verb, registered in main.go).
- mvp: `dross execute quick [--solo]` with no end verb, because an end verb would give the agent a way out of the gate.
- Default: `quick begin/end`. D10's guard closes mvp's escape hatch, and the marker carries the description the reviewer checks against.
- Why it matters: it adds a top-level verb. If D10 is rejected, mvp's objection comes back in full.

**D13. Whether the failed-task stash is enforced in code.**
- risk: in a solo run, `task status failed` refuses while code is dirty (t-12, TestSoloFailedRequiresStash).
- mvp and verification: the prompt tells the agent to stash.
- Default: enforce it in code.
- Why it matters: c-3 says the failed task "is not committed". Leftover changes would also leak into the next task's whole-tree review.

**D14. Checking the reviewer is ready at solo begin, and the "shadowed" status.**
- Only risk has this: `execute/quick begin --solo` refuse unless ReviewerStatus is ok, and doctor also flags a repo-level copy of the agent that shadows dross's.
- Default: include it (t-11, t-14, t-15).
- Why it matters: without the check, a run with no usable reviewer implements every task and then fails each one under review_unavailable. Doctor failing on "shadowed" goes beyond c-7's "missing or stale".

**D15. Fixture capture: its own task, or folded into the recorder task.**
- risk and verification make it a wave-1 go/no-go with a CLAUDE.md canary. risk also captures a background-launch fixture.
- mvp folds the capture into its recorder+gate task, with a STOP and no canary.
- Default: a separate t-1 with the canary and both fixtures.
- Why it matters: only the canary proves omitClaudeMd works on the installed Claude Code before code is built on it.

**D16. How FLAG/NOTE findings from earlier rounds are resolved.**
- risk labels a round-1 FLAG "non-blocking, left" even when round 2 passes. verification drops round-1 FLAGs. mvp labels final-round findings only.
- Default: risk's, so every finding gets a resolution.
- Why it matters: c-6 asks for "how each was resolved". Dropping findings shrinks the audit trail; keeping them may list FLAGs the fix already addressed.

**D17. verify.md: bundled with quick.md, or its own task.**
- risk bundles them (t-16), which pushes verify.md to wave 5. verification gives verify.md its own task.
- Default: split it out (t-9, wave 2).
- Why it matters: the split task is 2 files, at the size floor where tasks get merged. The bundle joined unrelated criteria (c-6 and c-8) only to clear that floor.

**D18. Names and verdict format.**
- Agent name: `dross-task-reviewer` (mvp, risk) against `dross-reviewer` (verification).
- Verdict block: a JSON fence (mvp `dross-review`, risk `dross-verdict`) against a TOML block that echoes subject and tree (verification).
- Default: `dross-task-reviewer` and a JSON `dross-verdict` fence with separate spec[]/quality[] arrays. Gate records are already JSON, and the TOML lock covers .dross config, not model output.
- Why it matters: this is cosmetic, except that dropping verification's tree echo leaves risk's digest check as the only defence against an edit made during the review.
