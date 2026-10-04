# solo-task-review — risk-lens draft

Lens: every task owns a named set of failure modes (`owns:`) and is the only task whose tests pin them.

```
Phase solo-task-review — 17 tasks across 5 waves

Wave 1
  t-1  Capture real Agent PostToolUse payloads
       files:    internal/gate/testdata/agent_reviewer_post.json (new),
                 internal/gate/testdata/agent_background_post.json (new),
                 internal/gate/fixture_test.go
       covers:   c-1, c-5
       desc:     Go/no-go for review_pass_signal, like tool-gate-hooks t-20. With the human's go-ahead, in a scratch repo only:
                 a project-level .claude/agents/dross-task-reviewer.md (omitClaudeMd: true, tools: Read), a project CLAUDE.md
                 carrying a canary, and a temporary matcher-less stdin-dump PostToolUse hook in .claude/settings.local.json
                 (never ~/.claude). Capture one foreground spawn ending in a ```dross-verdict fence and one run_in_background
                 spawn, then remove the hook. If the reply text is not in tool_response, or subagent_type is not in tool_input,
                 STOP and re-decide review_pass_signal before t-9.
       owns:     unknown Agent hook shape (tool_name Agent vs Task); background-launch shape; omitClaudeMd not honoured on 2.1.289
       contract: - if the reviewer fixture stops being a real capture (session_id, transcript_path, toolu_ tool_use_id) whose
                   tool_name is Agent or Task, whose tool_input.subagent_type is dross-task-reviewer, and whose tool_response
                   text holds exactly one ```dross-verdict fence, TestCapturedAgentReviewerFixture fails — the stop signal
                 - if the scratch CLAUDE.md canary appears anywhere in the captured reviewer reply (it was told to quote every
                   instruction it received), TestCapturedReviewerIsolation fails — c-5 must be re-decided
                 - if the background fixture's tool_response carries a dross-verdict fence (a launch ack could pass for a
                   verdict), TestCapturedBackgroundAgentFixture fails

  t-2  Parse reviewer verdicts and track fix rounds
       files:    internal/review/verdict.go (new), internal/review/verdict_test.go (new),
                 internal/review/ledger.go (new), internal/review/ledger_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-2, c-3, c-6
       desc:     ReviewerAgent = "dross-task-reviewer". ParseVerdict reads exactly one ```dross-verdict JSON fence
                 {verdict, findings[{kind spec|quality, severity BLOCKING|FLAG|NOTE, criterion, text}]}; spec findings are forced
                 BLOCKING; pass is derived from the findings and cross-checked against the stated verdict. Ledger:
                 Round{tree, digest, outcome pass|block|unavailable|stale, findings, cause, at}; State() → none / pass(tree) /
                 blocked / exhausted / unavailable; Resolve() labels every finding.
       owns:     ambiguous or self-contradicting verdicts; a spec gap graded down to FLAG; re-rolling the reviewer past the one
                 fix round; an unavailable review later "healed" by a pass
       contract: - parse table: zero fences, two fences, malformed JSON, unknown kind or severity, a finding with empty text →
                   each errors; "pass" carrying a quality BLOCKING, or "block" carrying only FLAG/NOTE → error naming the
                   inconsistency, never a pass — TestParseVerdict
                 - if a spec finding graded FLAG or NOTE stays non-blocking, TestSpecFindingsAlwaysBlock fails: it comes back
                   BLOCKING and a "pass" carrying it does not parse as a pass (c-2)
                 - if quality FLAG/NOTE start blocking, TestQualityGrades fails: "pass" with one FLAG and one NOTE is a pass with
                   both findings kept (quality_blocking)
                 - ledger table: [] none; [pass T1] pass(T1); [block] blocked; [block, pass T2] pass(T2); [block, block]
                   exhausted; [pass T1, block, block] exhausted; [block, stale, block] exhausted (stale never counts);
                   [block, block, pass] exhausted; [unavailable, pass] unavailable — any drift fails TestLedgerState (c-3)
                 - if Resolve mislabels, TestResolve fails: [block{B1,F1}, pass{N1}] → B1 "fixed in the fix round", F1 and N1
                   "non-blocking, left"; [block{B1}, block{B2}] → B2 "unresolved — task failed"
                 - an unfiled json-tagged string field in internal/review fails TestEveryPathShapedFieldIsDeclared

  t-3  Build the review context from the whole diff
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go,
                 internal/review/context.go (new), internal/review/context_test.go (new)
       covers:   c-1, c-5, c-8
       desc:     treefp gains Base (HEAD's tree minus .dross/, the empty tree on an unborn HEAD), Patch(base, work) via
                 `git diff-tree -p --no-color --no-ext-diff --no-textconv --no-renames --src-prefix=a/ --dst-prefix=b/`, and
                 Dirty (code paths differing from Base). review.BuildContext(root, Scope, withhold) renders the task record
                 (id, title, description, files, covers, test_contract — never status), covered criteria text only, locked
                 decisions and hard rules — or a quick's description — then Patch(Base, WorkingTree); withheld paths are named,
                 their content omitted. Returns text, tree and a short digest.
       owns:     untracked files invisible to `git diff HEAD`; .dross/ rendered as deleted; repo diff drivers executing inside a
                 hook; git config making the bytes (and so the digest) unstable; secret content in the context; the real
                 index touched
       contract: - if a staged, an unstaged or an untracked-unignored file is missing from the patch, or an ignored file or any
                   .dross/ path (added, edited or shown deleted) appears, TestPatchWholeDiff fails (review_input)
                 - if repo config color.ui=always, diff.external=<script touching a marker>, a .gitattributes textconv driver,
                   diff.noprefix=true or diff.renames=copies changes the context bytes, adds ANSI escapes or creates the
                   marker, TestPatchConfigIndependent fails
                 - if an unborn HEAD doesn't show every file as added against the empty tree, TestPatchUnbornHead fails
                 - if the real index's sha256 or `git diff --cached --name-only` differs before and after BuildContext and
                   Dirty in a partially staged repo, TestContextLeavesIndexAlone fails
                 - if a withheld path's content appears (its path must), TestContextWithholdsSecretPaths fails
                 - if an uncovered criterion's text, the task's status or an unlocked decision appears, or flipping the task
                   pending→in_progress changes the digest, TestContextScope fails; a quick scope carries the description and
                   no plan content
                 - if two builds on an unchanged tree differ in digest, or a one-byte edit leaves it equal, TestContextDigest
                   fails
                 - if Dirty reports a .dross/-only change or misses an untracked unignored file, TestDirtyIgnoresDross fails

  t-4  Record a failure reason on tasks
       files:    internal/phase/phase.go, internal/cmd/task.go, internal/cmd/task_reason_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-3
       desc:     Task.Reason (toml reason,omitempty). `dross task status <p> <t> failed --reason <text>` stores it; any other
                 status clears it; --reason with a non-failed status errors; `task show` prints it. (cli_tree.txt is a
                 regenerated golden; not_paths.txt a ledger row.)
       owns:     the failure cause lost; a stale reason surviving a retry
       contract: - if `task status p t-2 failed --reason "x"` doesn't survive a plan.toml round-trip (newline, quotes and `]]`
                   included) or `task show` omits it, TestTaskFailedReason fails
                 - if setting t-2 back to pending or in_progress keeps the reason, TestReasonClearedOnRetry fails
                 - if --reason with done or pending is accepted instead of erroring, TestReasonOnlyWithFailed fails
                 - if a status write leaves a reason-less plan with a new `reason` key, TestReasonOmitEmpty fails
                 - an unfiled phase.Task.Reason fails TestEveryPathShapedFieldIsDeclared; a missing `--reason` row fails
                   TestCLITreeGolden

  t-5  Add a reviews map to the change record
       files:    internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-6
       desc:     Changes.Reviews map[taskID]TaskReview{outcome, rounds, findings[{round, kind, severity, criterion, text,
                 resolution}]} (json reviews,omitempty) on changes' own types — importing internal/review would cycle through
                 phase→changes. SetReview(root, phase, task, rv) writes only that entry; Record() never touches Reviews.
       owns:     a failed task's review entering verify's mutation scope through Tasks.Files; re-execution wiping a review;
                 old changes.json files drifting
       contract: - if a changes.json fixture without reviews changes a byte across Load→Save, TestReviewsOmitEmpty fails
                 - if Record(t-3, …) after SetReview(t-3) drops the review, or SetReview creates a Tasks entry (a failed task
                   has no TaskRecord), TestReviewsSeparateFromTasks fails
                 - if SetReview on a missing changes.json doesn't create one holding only phase + reviews, or clobbers pr,
                   base, base_commit or status on an existing one, TestSetReviewPreservesRecord fails
                 - an unfiled new tagged field fails TestEveryPathShapedFieldIsDeclared

Wave 2 (depends t-2, t-5)
  t-6  Store review, quick and context records
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/gate/override_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-4, c-8
       depends:  t-2
       desc:     review.json {kind task|quick, phase, task, attempt, rounds []review.Round}, quick.json {mode, description,
                 head, at}, review-context.md (text) under .dross/gate/: atomic temp+rename, missing = none, corrupt = error
                 naming the path, git-tracked = refused unread. RemoveQuick treats none as success.
       owns:     a half-written review record read by the commit gate; a cloned repo arriving pre-reviewed; review/quick/context
                 records forged through tool calls
       contract: - if a truncated review.json or quick.json decodes to a zero record instead of an error naming
                   .dross/gate/<file>, or a missing one returns anything but (nil, nil), TestReviewRecordDecode fails
                 - if N concurrent SaveReview writers and LoadReview readers ever observe a partial record, the concurrency
                   test fails
                 - if a force-added review.json or quick.json is read instead of refused, TestTrackedRecordRefused fails
                 - if Write/Edit of .dross/gate/review.json, quick.json or review-context.md, or Bash
                   `echo x > .dross/gate/review.json` / `cp /tmp/r .dross/gate/review-context.md`, gets past tamper-guard,
                   the tamper table fails
                 - unfiled tagged fields fail TestEveryPathShapedFieldIsDeclared

  t-7  Ship the isolated reviewer definition
       files:    assets/agents/dross-task-reviewer.md (new), assets/embed.go, assets/embed_test.go,
                 internal/review/agentdef_test.go (new)
       covers:   c-5, c-2
       depends:  t-2
       desc:     Frontmatter: name dross-task-reviewer, omitClaudeMd: true, tools: Read, Grep, Glob, no memory, no model
                 (inherits, reviewer_model). Body: Read the context file the prompt names and emit no verdict if its digest
                 header differs; enumerate each covered criterion's named sub-surfaces, any with no code or test in the diff is
                 a spec finding (always BLOCKING); grade code quality BLOCKING (correctness bug, unmet test_contract,
                 locked-decision or rule violation) / FLAG / NOTE; end with exactly one dross-verdict fence. embed gains
                 `all:agents`.
       owns:     isolation silently lost; a reviewer able to mutate the tree it reviews; definition and parser drifting apart
       contract: - if omitClaudeMd is removed or false, a `memory` key appears, a `model` other than inherit appears, `tools`
                   is dropped (inherit-all) or gains Bash, Edit, Write, MultiEdit, NotebookEdit, Agent, Task, WebFetch or
                   WebSearch, TestReviewerDefinitionIsolation fails (c-5)
                 - if the frontmatter name drifts from review.ReviewerAgent, TestReviewerNameMatchesRecorder fails
                 - if the body's worked example stops parsing with review.ParseVerdict, or the body stops saying spec findings
                   are always BLOCKING and naming the three quality grades, TestReviewerBodyMatchesParser fails
                 - if the embedded assets/agents set differs from disk (a narrowed embed pattern), the embed drift guard fails

  t-8  Render reviewer findings in the PR body
       files:    internal/ship/body.go, internal/ship/body_test.go, internal/cmd/ship.go,
                 internal/boardsync/toolfence_composer_test.go
       covers:   c-6
       depends:  t-5
       desc:     BuildPRBody(spec, v, reviews) adds "## Reviewer findings" per task id (sorted): outcome, then each finding's
                 severity, kind, criterion, text and resolution. Text collapsed to one line and pipe-escaped; per-finding and
                 section caps end in "… N more in changes.json"; no section when reviews are empty. ship.go passes
                 changes.Reviews; the composer canary test calls the new signature.
       owns:     PR creation failing on an oversized body; markdown injection from finding text; unstable re-ship bodies
       contract: - if nil or empty reviews change today's body by a byte, the existing TestBuildPRBody* tests fail
                 - if finding text "x\n## Fake" or "a|b" yields a new heading line or an unescaped pipe,
                   TestReviewSectionEscapes fails
                 - if 500 findings of 2 KB each produce a body over 60,000 chars, or the tail miscounts the dropped findings,
                   TestReviewSectionCapped fails
                 - if two calls on the same reviews map differ, or a timestamp appears, TestReviewSectionStable fails
                 - if `dross ship --print-body` on a phase whose changes.json has reviews omits the section,
                   TestShipPrintBodyShowsReviews fails

Wave 3 (depends t-1, t-2, t-3, t-4, t-5, t-6, t-7)
  t-9  Record reviewer verdicts from PostToolUse
       files:    internal/gate/review.go (new), internal/gate/review_test.go (new)
       covers:   c-1, c-3, c-4, c-8
       depends:  t-1, t-2, t-3, t-6
       desc:     ArmedScope(root): a solo quick (quick.json solo with head == HEAD; attempt = its begin time), else a solo
                 execute task (execute.json solo for current_phase, exactly one in_progress, HEAD on phase/<id>; attempt =
                 HEAD), else nil; a pair quick never disarms a solo task. Withheld(home) is the shared secret-path matcher.
                 Recorder claims Agent/Task calls whose subagent_type is review.ReviewerAgent; regenerates the context for
                 ArmedScope and compares its digest with the prompt's `dross-review-context <digest>` line; appends pass/block
                 (bound to the context tree), stale (digest mismatch or no line) or unavailable (parse failure) to review.json,
                 starting a fresh record when scope or attempt changed. Background launches and unarmed runs record nothing,
                 with one warning.
       owns:     a pass from a foreign agent or a forged reply; a pass for a stale context (the fix-round re-review shown the
                 round-1 diff); a pass recorded in pair mode; rounds leaking across tasks or attempts; a second writer
       contract: - from t-1's captured envelope carrying a pass verdict and a matching context line, in a solo-armed fixture
                   repo, the recorder appends a pass round whose tree equals treefp.WorkingTree; with subagent_type
                   general-purpose, or tool_name Bash, nothing is written — TestRecorderClaims
                 - if a file edited after `dross review context` and before the verdict, or a prompt without the context line,
                   yields anything but a stale round plus a warning naming `dross review context`, TestRecorderStaleContext
                   fails
                 - if an unparseable reply records anything but an unavailable round naming the parse error, or a later pass
                   for the same attempt reads as pass, TestRecorderUnavailableSticky fails (review_unavailable)
                 - if t-1's background-launch fixture writes review.json or emits other than one warning,
                   TestRecorderBackgroundLaunch fails
                 - if a reviewer run with execute.json pair or absent, no task in_progress, or HEAD off phase/<id> writes
                   review.json, TestRecorderPairSilent fails (c-1)
                 - scope table: t-3's rounds then t-4 in_progress → fresh record; HEAD moved for the same task → fresh record;
                   solo quick at HEAD → quick record; quick.json pair while t-3 is solo in_progress → still t-3; two tasks
                   in_progress → ArmedScope errors naming both — TestArmedScope
                 - if any non-test file outside internal/gate calls gatestate.SaveReview, TestReviewRecordSingleWriter fails
                   (review_pass_signal: no CLI verb can record a pass)
                 - if either tool name "Agent" or "Task" stops being claimed, TestRecorderToolRename fails

  t-10 Install the reviewer and check readiness
       files:    internal/cmd/install.go, internal/cmd/reviewer_agent.go (new), internal/cmd/install_agent_test.go (new),
                 internal/cmd/cmd_test.go
       covers:   c-7
       depends:  t-7
       desc:     agentsDir() = $CLAUDE_CONFIG_DIR/agents, else ~/.claude/agents. install copies (copy mode) or symlinks
                 (link mode) dross-task-reviewer.md there, prunes dross-*.md agents this version dropped, never touches
                 others. ReviewerStatus(repoDir) → ok / missing (incl. dangling link) / stale (bytes ≠ embedded) / shadowed
                 (repo .claude/agents/dross-task-reviewer.md) — the one readiness check doctor and solo begin share. chdir()
                 seeds a current reviewer the way it seeds hooks.
       owns:     install and readiness reading different dirs; a repo-level agent shadowing dross's; a stale link-mode
                 definition; the existing suite going red once readiness is required
       contract: - if copy mode leaves other than the embedded bytes, link mode other than a symlink to
                   <src>/agents/dross-task-reviewer.md, or a mode switch leaves the old form, TestInstallReviewer fails
                 - if install with CLAUDE_CONFIG_DIR=X writes under $HOME/.claude/agents, or ReviewerStatus reads a different
                   dir than install wrote, TestReviewerDirAgreement fails
                 - if a foreign agents/my-agent.md is touched or a stale agents/dross-old.md survives, TestInstallAgentPrune
                   fails
                 - status table: absent → missing; dangling symlink → missing; one byte changed → stale; repo
                   .claude/agents/dross-task-reviewer.md → shadowed naming that file; fresh install → ok — TestReviewerStatus
                 - if chdir() stops seeding a current reviewer, the existing TestExecuteBeginRecordsMode and Doctor() tests
                   go red once t-13/t-14 land

  t-11 Attach reviews and guard failed-task marking
       files:    internal/cmd/changes.go, internal/cmd/task.go, internal/cmd/review_attach.go (new),
                 internal/cmd/review_attach_test.go (new)
       covers:   c-6, c-3
       depends:  t-2, t-3, t-4, t-5, t-6
       desc:     `changes record <p> <t>` and `task status <p> <t> failed` copy review.json's rounds for (p, t), resolved by
                 review.Resolve, into changes.Reviews — never from a flag. `failed` without --reason derives it from an
                 exhausted ("review blocked after the fix round: <first BLOCKING text>") or unavailable ("review unavailable:
                 <cause>") record. While execute.json is solo for <p>, `failed` refuses when treefp.Dirty reports code
                 changes, naming the stash command. An attach error warns and never blocks the record.
       owns:     c-6 resting on the agent's paraphrase; a failed task's code committed anyway or bleeding into the next task's
                 whole-tree review (c-3 "is not committed")
       contract: - with review.json [block, pass] for (p, t-3), `changes record p t-3 --files a.go --commit abc` writes
                   reviews.t-3 outcome pass, rounds 2, B1 "fixed in the fix round", FLAGs "non-blocking, left"; a record for
                   t-2 or another phase attaches nothing — TestChangesRecordAttachesReview
                 - if a pair-mode task's changes.json gains a "reviews" key, TestPairRecordHasNoReview fails
                 - if `task status p t-3 failed` (no --reason) over an exhausted record doesn't set the derived reason and
                   attach "unresolved — task failed", over an unavailable record doesn't name the cause, or with --reason "x"
                   doesn't keep "x", TestFailedReasonFromReview fails
                 - if `failed` succeeds in a solo run while a.go is modified or an untracked b.go exists, or refuses when only
                   .dross/ is dirty or the run is pair, TestSoloFailedRequiresStash fails
                 - if a corrupt review.json makes `changes record` exit non-zero or skip the task record, TestAttachErrorWarns
                   fails
                 - if `changes record` or `task status` gains a flag carrying review content, TestNoReviewFlags fails

Wave 4 (depends t-3, t-6, t-9, t-10)
  t-12 Add the dross review context verb
       files:    internal/cmd/review.go (new), internal/cmd/review_test.go (new), cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-5, c-8
       depends:  t-3, t-6, t-9
       desc:     `dross review context` resolves gate.ArmedScope, builds with gate.Withheld, writes .dross/gate/review-context.md
                 and prints only its path, the digest, the subagent_type and the exact prompt line
                 `dross-review-context <digest> <path>` — never the diff. Refuses, writing nothing, with nothing armed (pair has
                 no reviewer) or an empty code diff. (cli_tree.txt is a regenerated golden.)
       owns:     the verb and the recorder computing different digests (every review "stale"); the diff and withheld secrets
                 entering the executing transcript; a review run in pair mode
       contract: - if the printed digest differs from the one t-9's recorder regenerates for the same armed scope — including
                   with a secret path added via gates.toml — TestContextDigestParity fails
                 - if stdout carries any line of the diff, TestContextVerbPrintsNoDiff fails
                 - if pair mode, no task in_progress, or a .dross/-only diff writes a context file instead of erroring (pair
                   naming "the human is the gate"), TestContextVerbRefuses fails
                 - if the verb changes the index, state.json or plan.toml bytes, TestContextVerbNoSideEffects fails;
                   cli_tree.txt must list `review context`

  t-13 Add quick begin/end and solo readiness
       files:    internal/cmd/quick.go (new), internal/cmd/execute.go, internal/cmd/solo_begin_test.go (new),
                 cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-8, c-1
       depends:  t-6, t-10
       desc:     `dross quick begin [--solo] <description>` writes quick.json {mode, description, head, at}; `dross quick end`
                 removes it. `quick begin --solo` and `execute begin --solo` refuse unless ReviewerStatus is ok, naming the
                 status and `dross install`. `execute begin` (either mode) removes quick.json. (cli_tree.txt is a regenerated
                 golden.)
       owns:     a solo run implementing every task before discovering it has no usable reviewer; a stale solo-quick marker
                 gating later commits
       contract: - if `quick begin --solo "fix x"` records other than mode solo, the description verbatim and the current HEAD,
                   or an empty description is accepted, TestQuickBegin fails
                 - if `execute begin p --solo` or `quick begin --solo x` succeeds with the reviewer missing, stale or shadowed,
                   or the error omits `dross install` (shadowed: the repo file), TestSoloBeginNeedsReviewer fails; pair
                   begins succeed regardless
                 - if `execute begin p` leaves an earlier quick.json in place, or `quick end` with none errors,
                   TestQuickMarkerLifetime fails
                 - the existing TestExecuteBeginRecordsMode / TestExecuteBeginLeavesStateAlone must pass unedited;
                   cli_tree.txt must list `quick begin --solo` and `quick end`

  t-14 Report the reviewer in dross doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_reviewer_test.go (new),
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-7
       depends:  t-10
       desc:     A Reviewer section renders ReviewerStatus: ✓ ok; ✗ issue (non-zero exit) for missing, stale and shadowed, each
                 naming the path and the fix. (doctor_run.txt is a regenerated golden.)
       owns:     doctor green while solo review cannot run
       contract: - if doctor exits 0 with the definition deleted, one byte changed, a dangling symlink, or a repo
                   .claude/agents/dross-task-reviewer.md, TestDoctorReviewer fails; each line names the path and
                   `dross install` (shadowed: the repo file to remove)
                 - if doctor reads $HOME/.claude/agents while CLAUDE_CONFIG_DIR points elsewhere, TestDoctorReviewerDir fails
                 - if a fresh install isn't exit 0 with ✓, or doctor_run.txt stops pinning the section, the golden fails

Wave 5 (depends t-9, t-11, t-12, t-13)
  t-15 Run the reviewer in solo execute
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-2, c-3, c-6
       depends:  t-11, t-12, t-13
       desc:     §0: a refused `execute begin --solo` stops and asks the user to run `dross install`. §1f solo: after the full
                 green `dross test`, `dross review context`, then a FOREGROUND Agent spawn (subagent_type dross-task-reviewer,
                 the printed prompt line verbatim); pass → commit; blocked → one fix round (edit, `dross test`,
                 `dross review context`, re-spawn); still blocked, unavailable or the spawn errored →
                 `git stash push --include-untracked -- . ':(exclude).dross'`, `dross task status <p> <t> failed` (reason
                 derived; --reason only for a spawn error), then `dross task next`. The solo red path stashes the same way.
                 Pair mode spawns no reviewer; `changes record` carries no review flags.
       owns:     the reviewer skipped or run in pair; a background spawn (no verdict ever recorded); a second fix round; a
                 failed diff left in the tree
       contract: - if `dross review context` and the dross-task-reviewer spawn stop appearing after §1f's bare `dross test`
                   and before `git commit` in the solo branch, or appear in the pair branch or §1c,
                   TestExecuteSoloReviewPlacement fails (c-1)
                 - if the spawn's subagent_type differs from review.ReviewerAgent, or the spawn paragraph permits
                   run_in_background, TestExecuteReviewerSpawnForeground fails
                 - if the fix round may run more than once, the failed path marks failed before stashing, or the stash
                   pathspec doesn't exclude .dross, TestExecuteReviewFailurePath fails (c-3)
                 - if the solo red-test path marks failed without stashing first, TestExecuteSoloRedStashes fails
                 - TestPromptCommitsSatisfyGreenGate must still match execute §1f; TestNoPromptTellsAgentToLiftGate stays green

  t-16 Review solo quicks and show reviews in verify
       files:    assets/prompts/quick.md, internal/cmd/quick_prompt_test.go, assets/prompts/verify.md,
                 internal/cmd/verify_prompt_test.go
       covers:   c-8, c-6
       depends:  t-11, t-12, t-13
       desc:     quick.md: `dross quick begin --solo "<description>"` (solo) or `dross quick begin "<description>"` (pair)
                 before §3; solo §4→§5 runs review context + a foreground reviewer spawn after `dross test` and before
                 `git commit`, one fix round, then discard + `dross quick end` + report findings with no bump; every abort path
                 runs `dross quick end`. verify.md reads changes.json `reviews` and reports each solo task's outcome,
                 findings and resolutions, flagging unresolved ones.
       owns:     a solo quick committed unreviewed; an aborted quick leaving its marker armed; reviews persisted but never read
       contract: - if quick.md's solo branch loses the review between `dross test` and `git commit`, or the pair branch gains a
                   reviewer spawn, TestQuickSoloReview fails (c-8)
                 - if `dross quick begin` stops preceding §3 Implement in either mode, or an abort path (solo red, blocked
                   review, user abort) omits `dross quick end`, TestQuickMarkerClosed fails
                 - if verify.md stops reading the `reviews` key or reporting resolutions, TestVerifyShowsReviews fails (c-6)

  t-17 Gate solo commits on a recorded review
       files:    internal/gate/commit.go, internal/gate/soloreview.go (new), internal/gate/soloreview_test.go (new),
                 internal/cmd/gate_override_test.go, README.md
       covers:   c-4, c-8
       depends:  t-9, t-12, t-13
       desc:     Extract commit-green's line parse + candidate into one builder both gates call. solo-review (Workflow,
                 Liftable): while ArmedScope is set, a code commit needs review.json for that scope + attempt in State pass
                 with tree == candidate; .dross/-only passes; each refusal names its state's remedy. While armed with Dirty
                 code it also refuses Bash `dross execute begin <p>` without --solo, `dross quick begin` without --solo and
                 `dross quick end`. README gate row. Last id in the plan, so `make install` arms it on this repo only after
                 the prompts teach the loop.
       owns:     an unreviewed or stale-reviewed solo commit; the two commit gates disagreeing on what is committed; a
                 mid-task downgrade to pair skipping review; a buggy gate blocking pair work
       contract: - with t-3 solo in progress: pass for candidate T passes; no record refuses naming `dross review context` and
                   dross-task-reviewer; pass for T then a one-byte edit refuses "tree changed since the review"; blocked names
                   the fix round; exhausted or unavailable names `dross task status <p> t-3 failed`; a pass for t-2 refuses —
                   TestSoloReviewGate (c-4)
                 - if a .dross/-only commit while armed refuses, or pair mode, no task in_progress or HEAD off phase/<id>
                   refuses or spawns any treefp git argv (gitrun.ArgvRecorder), TestSoloReviewSilent fails (c-4)
                 - quick table: solo quick at HEAD is gated against its quick record; pair quick is silent; after the quick's
                   commit moves HEAD it is silent — TestSoloReviewQuick (c-8)
                 - if solo-review and commit-green judge `git add a.go && git commit -m x` against different candidates,
                   TestSharedCandidate fails; the existing commit_test.go suite stays green across the extraction
                 - downgrade table: armed + a.go dirty → `dross execute begin p`, `dross quick begin "x"`, `dross quick end`
                   refuse; armed + only .dross/ dirty → pass; `dross execute begin p --solo` → pass — TestSoloDowngradeGuard
                 - if a corrupt review.json or quick.json, or an unreadable plan.toml, while armed passes instead of refusing
                   naming the error, TestSoloReviewClosedPosture fails
                 - TestGateRegistryNames gains solo-review; TestGateRemediesResolve fails if a remedy names a verb missing
                   from newRoot()
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 (shape), t-3 (whole diff), t-9 (recorder, pair silent), t-12 (verb refuses pair), t-13 (solo begin), t-15 (execute.md placement) |
| c-2 | t-2 (spec/quality split, spec always BLOCKING), t-7 (definition instructs it), t-15 |
| c-3 | t-2 (one fix round, exhausted), t-4 (reason field), t-11 (derived reason, not committed), t-15 (fix round, stash, next task) |
| c-4 | t-6 (records), t-9 (pass bound to tree), t-17 (commit gate) |
| c-5 | t-1 (canary on 2.1.289), t-3 (context contents), t-7 (definition isolation test), t-12 (no diff in transcript) |
| c-6 | t-2 (resolutions), t-5 (schema), t-8 (PR body), t-11 (attach), t-15, t-16 (verify.md) |
| c-7 | t-10 (install + readiness), t-14 (doctor) |
| c-8 | t-3 (quick context), t-6 (quick.json), t-9 (quick scope), t-12, t-13 (quick begin/end), t-16 (quick.md), t-17 (gate) |

All 8 criteria covered.

## Judgment calls

- Context goes by file (tamper-guarded .dross/gate/review-context.md + a short digest in the prompt), not pasted inline: an LLM retyping a large diff changes bytes, and the diff stays out of the executing transcript.
- The recorder regenerates the context and compares digests instead of trusting the prompt. This catches the likeliest honest mistake: a fix-round re-review that was shown the round-1 diff.
- The verdict is exactly one fenced `dross-verdict` JSON block. Pass is derived from the findings and cross-checked against the stated verdict. Rejected "last block wins" because the closed posture fits gate_error_posture.
- The one-fix-round cap is enforced in the recorder's ledger (a second blocking review exhausts the task). Rejected prompt-only enforcement: re-rolling the reviewer until it passes would make the gate advisory.
- A stale context or a background launch records no counted round and can be re-spawned. Only a foreground reviewer's error or an unparseable verdict is terminal (review_unavailable).
- solo-review is a separate liftable gate that shares commit-green's candidate builder. Rejected folding it into commit-green, because lifting one would lift both and their refusals would blur.
- Reviews live in a separate `reviews` map in changes.json, not a TaskRecord field. A failed task would otherwise need a TaskRecord, and Tasks.Files feeds verify's mutation scope.
- changes defines its own review structs. Importing internal/review would cycle (review→phase→changes).
- Reviewer tools are Read, Grep and Glob. Rejected no tools (it must Read the context file and nearby code) and Bash (it could change the tree it is reviewing). c-5's "sees only" is read as excluding the bias sources it names, which the c-5 test pins.
- The context includes locked decisions and hard rules alongside the task record. quality_blocking's locked-decision/rule BLOCKING grade can't be judged without them. The alternative was having the reviewer Read spec.toml/rules.toml itself, which is nondeterministic and outside the digest.
- The agents dir honours CLAUDE_CONFIG_DIR for install, readiness and doctor alike. Rejected reusing the skills' hard-coded $HOME/.claude, because Claude Code and doctor would then read different dirs.
- `execute begin --solo` / `quick begin --solo` refuse when the reviewer isn't ready. Rejected finding out at the first review, after which every task would be implemented and then fail.
- In a solo run, `task status failed` refuses while code is uncommitted, so the agent stashes first. This enforces c-3's "is not committed" and keeps the next task's whole-tree review clean. Pair is untouched.
- Execute attempts are keyed by HEAD and quick attempts by quick.json's begin time, so review.json keeps a single writer. Rejected clearing it from `task status` writes.
- No Claude Code version probe. Rejected `claude --version` in doctor: it adds an exec site to the audited baseline, and t-1 proves omitClaudeMd works on the installed 2.1.289.
- The downgrade guard (pair begin / quick end refused while solo is armed and code is dirty) mirrors pair-approval's refusal of a mid-task switch to solo. Without it, switching to pair skips review for the commit.
- c-6 persistence covers /dross-execute tasks only, because c-6 names the "task's change record". Solo-quick reviews stay machine-local, since standalone quicks have no changes.json.
- The reviewer is spawned in the foreground even though this repo's fan-outs run in the background. The verdict only exists on a foreground Agent's PostToolUse result.
- The fix-round re-review is not required to run on a changed tree. c-3 governs, and re-rolls are capped by the ledger anyway.
- The solo-review gate takes the last id in wave 5, after the prompt tasks. `make install` (r-01) then arms it on this repo only once execute.md/quick.md teach the loop. Same reasoning as tool-gate-hooks t-17.
- Tasks t-2, t-4, t-13 and t-17 reach 5 files only through a regenerated golden (cli_tree.txt), a ledger row (not_paths.txt) or README. Splitting them would separate a verb from its registration.
