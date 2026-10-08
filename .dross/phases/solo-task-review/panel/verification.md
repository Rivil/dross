# solo-task-review — verification-lens draft

Lens: each criterion's ideal test contract was written first; every task below is the smallest change that makes one or more of those contracts satisfiable. Where a criterion could be enforced either by prompt wording or by code, this draft enforces it in code, so a test can observe it: pass derivation, the round cap, the spawn-prompt shape, subagent identity and tree binding. Prompt tests only pin ordering and wording.

```
Phase solo-task-review — 18 tasks across 5 waves

Wave 1
  t-1  Capture a live reviewer Agent payload
       files:    internal/gate/testdata/agent_reviewer_post.json (new), internal/gate/fixture_test.go
       desc:     Go/no-go for reviewer_isolation and review_pass_signal. This needs the human's go-ahead. In a scratch
                 repo, set up three things: a scratch .claude/agents/dross-reviewer.md (omitClaudeMd: true, no memory,
                 no model); a CLAUDE.md that holds a canary string; and a temporary stdin-dump PostToolUse hook in
                 .claude/settings.local.json (never ~/.claude/settings.json). Spawn the agent in the foreground and ask
                 it to quote any canary it can see. Save the payload and remove the hook. If the canary is visible, or
                 tool_response carries no final text, STOP before t-11.
       covers:   c-5, c-4
       contract: - if the fixture stops being a captured PostToolUse with all three of: tool_name = the Agent tool,
                   tool_input.subagent_type = "dross-reviewer", and the reviewer's final text readable from
                   tool_response alone, TestCapturedReviewerAgentFixture fails. t-11's recorder is built against this
                   shape
                 - if the fixture's tool_response contains the canary string, the same test fails. That means
                   omitClaudeMd did not drop CLAUDE.md, which is the stop signal for reviewer_isolation

  t-2  Parse reviewer verdict blocks
       files:    internal/review/verdict.go (new), internal/review/verdict_test.go (new)
       desc:     Pure package. Holds AgentName = "dross-reviewer", SpawnPrompt (the one fixed spawn text), and
                 Finding{Kind spec|quality, Severity, Criterion, Text}. Parse(text) reads exactly one ```dross-review
                 TOML block (subject, tree, [[spec]], [[quality]]). Pass is DERIVED, never declared: a block passes when
                 it has no spec finding and no BLOCKING quality finding.
       covers:   c-2
       contract: - if a block that declares verdict = "pass" while holding one [[spec]] finding (even one tagged
                   severity = "NOTE") parses as a pass, TestSpecFindingAlwaysBlocks fails
                 - if derivation drifts, the verdict table fails: FLAG/NOTE-only quality → pass; one BLOCKING
                   quality → blocked; no findings → pass
                 - if Parse returns spec and quality findings in one list instead of under separate Spec and Quality
                   fields, TestFindingsKeepKind fails
                 - if any of these malformed blocks parses instead of erroring with the problem named, the malformed
                   table fails: no block, two blocks, severity "MAJOR", a spec finding with no criterion, an empty
                   tree, an empty subject, invalid TOML. These errors are what review_unavailable acts on

  t-3  Snapshot the work tree as a patch
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go
       desc:     WorkingPatch(dir) returns WorkingTree's fingerprint plus a diff of that same temp-index snapshot
                 against HEAD (the empty tree when HEAD is unborn). The diff holds staged, unstaged and
                 untracked-unignored changes, with .dross/ excluded. Text and tree come from one snapshot, so they
                 cannot disagree.
       covers:   c-5, c-4
       contract: - if WorkingPatch's tree differs from WorkingTree() on the same repo, TestWorkingPatchTreeMatches fails.
                   Otherwise the packet would vouch for a tree the commit gate never compares
                 - if the patch drops a staged edit, an unstaged edit, an untracked new file, or an edit to a file no
                   task declares, or if it shows a .dross/state.json edit or an ignored file, the coverage table
                   fails (review_input)
                 - if the real index's sha256 or `git diff --cached --name-only` changes across WorkingPatch,
                   TestRealIndexUntouched fails (extended to cover the new call)
                 - if an unborn HEAD errors instead of diffing against the empty tree, the unborn test fails

Wave 2 (t-4, t-5 depend on t-2)
  t-4  Ship the dross-reviewer agent definition
       files:    assets/agents/dross-reviewer.md (new), assets/embed.go, assets/embed_test.go,
                 internal/review/definition_test.go (new)
       desc:     Frontmatter: name dross-reviewer, a description, tools Read/Grep/Glob/Bash, omitClaudeMd: true, no
                 memory, no model. Body: run `dross review packet`; judge each covered criterion's named sub-surfaces
                 (spec); grade quality as BLOCKING/FLAG/NOTE in plan-review's vocabulary; never edit; emit exactly one
                 dross-review block that echoes subject and tree. The embed directive gains all:agents.
       covers:   c-5, c-2, c-7
       depends:  t-2
       contract: - if the shipped definition drops `omitClaudeMd: true`, gains a `memory` key, or pins a `model`
                   (reviewer_model), TestReviewerDefinitionIsolation fails
                 - if its tools gain Edit, Write, MultiEdit, NotebookEdit or Agent, the same test fails. A reviewer
                   that can write could change the tree it vouches for. One that can spawn could fork the parent's
                   context
                 - if its frontmatter name stops equalling review.AgentName, TestReviewerDefinitionName fails, because
                   the recorder would never match it
                 - if the definition's worked example block stops parsing through review.Parse into one spec finding
                   and at least one quality finding, TestReviewerExampleParses fails (the definition and the parser
                   have drifted apart)
                 - if agents/dross-reviewer.md is missing from assets.FS (for example, all:agents was dropped), the
                   embed-drift guard in assets/embed_test.go fails

  t-5  Store review rounds and quick mode
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     review.json is one subject's log: {subject{kind execute|quick, phase, task, description},
                 rounds[{n, tree, verdict pass|blocked|unavailable, cause, spec, quality, at}]}. AppendReviewRound
                 restarts the log at n=1 for a new subject and appends otherwise. quick.json holds
                 {mode, description, head, at}, with Save/Load/Clear. Both use the same atomic write and
                 tracked-refusal as green, execute and approval.
       covers:   c-3, c-4, c-6, c-8
       depends:  t-2
       contract: - if a new subject's round appends to the old log instead of restarting at n=1, or a same-subject round
                   is not n = previous+1, TestReviewRoundsPerSubject fails
                 - if a pass round with an empty tree is accepted, the empty-tree test fails. An empty tree could only
                   ever match another empty fingerprint
                 - if a truncated review.json or quick.json decodes as a zero record instead of an error naming its
                   path, or a missing one returns anything but (nil, nil), the record test fails
                 - if a force-added .dross/gate/review.json is read instead of refused, TestTrackedRecordRefused fails
                   (table extended)
                 - if spec and quality findings collapse into one list on disk, the round-trip test fails
                 - if a new tagged field is filed in neither pathfence.Fields() nor not_paths.txt,
                   TestEveryPathShapedFieldIsDeclared fails

Wave 3 (t-6, t-7, t-8 depend on t-5; t-9 depends on t-4)
  t-6  Resolve the armed solo review subject
       files:    internal/gate/review_subject.go (new), internal/gate/review_subject_test.go (new)
       desc:     ReviewSubject(root) returns one of three results:
                 - an execute subject, when current_phase's single in_progress task runs under execute.json mode=solo
                   for that phase on phase/<id>
                 - a quick subject, when quick.json is mode=solo at the current HEAD
                 - nil otherwise
                 Both armed at once is an error. It reuses pair.go's inProgress/onBranch/headOf.
       covers:   c-4, c-8
       depends:  t-5
       contract: - if arming drifts, TestReviewSubjectArming fails:
                   - solo with t-2 in_progress on phase/p → execute p/t-2
                   - mode pair, mode absent, no task in_progress, or a branch other than phase/p → nil
                   - quick.json solo at HEAD → quick
                   - quick.json solo after a new commit → nil
                   - quick.json pair → nil
                 - if solo execute and solo quick both armed resolve to either one instead of an error naming both,
                   the ambiguity test fails
                 - if an unreadable state.json, plan.toml, execute.json or quick.json resolves to nil instead of an
                   error, the closed-posture test fails (gate_error_posture)

  t-7  Add dross quick begin/end mode marker
       files:    internal/cmd/quick.go (new), internal/cmd/quick_test.go (new), cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross quick begin "<description>" [--solo]` writes quick.json {mode, description, current HEAD}.
                 `dross quick end` removes it. Neither writes anything else. (cli_tree.txt is a regenerated golden.)
       covers:   c-8
       depends:  t-5
       contract: - if `quick begin "x" --solo` records anything but mode=solo, description x and the current HEAD, or
                   `quick begin "x"` records anything but mode=pair, TestQuickBeginRecordsMode fails
                 - if an empty or whitespace-only description writes a record instead of erroring, the precondition
                   test fails. A solo review would have nothing to check spec compliance against
                 - if begin or end changes the sha256 of state.json or any plan.toml, the no-clobber test fails
                 - if `quick end` leaves quick.json in place, or errors when none exists, the end test fails
                 - if cli_tree.txt stops listing `quick begin` with --solo and `quick end`, TestCLITreeGolden fails

  t-8  Add review records to task changes
       files:    internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     TaskRecord gains an optional
                 Review{verdict, rounds, reason, findings[{kind, severity, criterion, text, resolution}]}.
                 ResolveReview(rounds) applies these rules:
                 - earlier-round blocking findings → fixed, when the final round passes
                 - final-round non-blocking findings → left
                 - final-round blocking findings → unresolved, and reason = their text
                 - a final unavailable round → reason = its cause
       covers:   c-6, c-3
       depends:  t-5
       contract: - if resolution drifts, the resolution table fails:
                   - [r1 blocked: spec c-2 + BLOCKING + FLAG; r2 pass: NOTE] → spec fixed, BLOCKING fixed, NOTE
                     left, and no round-1 FLAG
                   - [r1 pass: FLAG] → FLAG left
                   - [r1 blocked, r2 blocked] → verdict blocked, with reason = r2's BLOCKING text
                   - [r1 unavailable] → verdict unavailable, with reason = its cause
                 - if a TaskRecord with no review serializes any review key (so a pair task's changes.json bytes
                   change), the omitempty test fails
                 - if Review does not round-trip through Save/Load with kind, severity and resolution intact, the
                   round-trip test fails
                 - if the new tagged fields are filed in neither pathfence.Fields() nor not_paths.txt,
                   TestEveryPathShapedFieldIsDeclared fails

  t-9  Install agent definitions into Claude config
       files:    internal/cmd/install.go, internal/cmd/install_test.go
       desc:     The installer syncs embedded agents/*.md into the effective agents dir (CLAUDE_CONFIG_DIR/agents, else
                 ~/.claude/agents). Copy mode writes copies. Link mode symlinks assets/agents/<f>. It prunes
                 dross-*.md files it no longer ships and never touches other agents. One resolver is shared with
                 doctor.
       covers:   c-7
       depends:  t-4
       contract: - if copy mode does not write agents/dross-reviewer.md byte-equal to assets.FS, or link mode does not
                   symlink it to <source>/agents/dross-reviewer.md, TestInstallReviewerAgent fails
                 - if CLAUDE_CONFIG_DIR is set and the definition lands under $HOME/.claude/agents instead, the
                   config-dir test fails
                 - if a stale agents/dross-old.md survives a reinstall, or a foreign agents/my-agent.md changes bytes
                   or mtime, the prune test fails
                 - if a second install changes the definition's bytes, the idempotency test fails

Wave 4 (depends t-1, t-3, t-5, t-6, t-8, t-9)
  t-10 Add dross review packet/status verbs
       files:    internal/cmd/review.go (new), internal/cmd/review_test.go (new), cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross review packet` prints:
                 - the armed subject
                 - t-3's tree and patch
                 - the task record (title, description, files, covers, test_contract)
                 - the text of the covered criteria only
                 - the phase's locked decisions and hard rules
                 For a quick subject, the description replaces the task and criteria. `dross review status` prints the
                 subject's rounds and whether a pass stands for the current tree. Both verbs are read-only. With no
                 subject armed, both error and name `--solo`.
       covers:   c-5, c-8, c-3
       depends:  t-3, t-5, t-6
       contract: - if the packet carries an uncovered criterion's text, or omits a covered one or the task's
                   test_contract, TestReviewPacketContents fails
                 - if the packet's patch drops an untracked new file or an edit outside task.files,
                   TestReviewPacketWholeDiff fails (review_input)
                 - if the packet's `tree:` line differs from treefp.WorkingTree(), TestReviewPacketTree fails
                 - if a quick packet lacks quick.json's description or prints any criterion, TestReviewPacketQuick fails
                 - if `review packet` or `review status` changes any byte under .dross/ (sha256 of every file before
                   and after), TestReviewVerbsReadOnly fails. No verb may record a pass (review_pass_signal)
                 - if `review status` calls a round-3 pass "passing", or a pass whose tree has since changed,
                   TestReviewStatusAgreesWithGate fails
                 - if cli_tree.txt stops listing `review packet` and `review status`, TestCLITreeGolden fails

  t-11 Record reviewer verdicts from Agent results
       files:    internal/gate/review_record.go (new), internal/gate/review_record_test.go (new),
                 internal/gate/override_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     A PostToolUse recorder that claims Agent calls (shape per t-1) where tool_input.subagent_type ==
                 review.AgentName. For the armed subject it appends a pass or blocked round from review.Parse. It
                 appends an unavailable round naming the cause when any of these hold: the text does not parse, the
                 prompt is not exactly review.SpawnPrompt, or the echoed subject or tree differs from the live subject
                 or WorkingTree. With no armed subject, or with run_in_background=true, it writes nothing and warns.
       covers:   c-2, c-3, c-4, c-5
       depends:  t-1, t-2, t-5, t-6
       contract: - if t-1's captured fixture, with its text swapped for a valid pass block for the armed subject and live
                   tree, does not record a round-1 pass, TestRecorderReadsCapturedShape fails
                 - if an Agent result from subagent_type "general-purpose", "fork" or none records anything,
                   TestRecorderOnlyTrustsReviewer fails. A context-inheriting agent must never produce a pass (c-5)
                 - if a spawn prompt carrying anything beyond review.SpawnPrompt (for example, pasted conversation)
                   records a pass instead of an unavailable round naming the prompt, TestRecorderRejectsWidenedPrompt
                   fails
                 - the review_unavailable table fails if any of these records a pass instead of an unavailable round
                   naming the cause: an unparseable verdict, an echoed tree ≠ WorkingTree (something edited during
                   review), or an echoed subject for another task
                 - if a declared-pass block with one [[spec]] finding records verdict pass, TestRecorderSpecBlocks fails
                 - if pair mode (no armed subject) or a run_in_background launch writes review.json, the silent test
                   fails. A background launch carries no verdict and must not burn the fix round
                 - if any non-test file outside internal/gate/review_record.go calls gatestate.AppendReviewRound
                   (module-wide source scan), TestOnlyRecorderWritesReview fails (review_pass_signal)
                 - if a Write or Bash redirect to .dross/gate/review.json or quick.json passes, the tamper-guard tables
                   fail (rows added)

  t-12 Gate solo commits on a recorded review pass
       files:    internal/gate/review_gate.go (new), internal/gate/review_gate_test.go (new),
                 internal/cmd/gate_override_test.go, README.md
       desc:     A new gate, solo-review (Workflow, Liftable). It claims Bash `git commit` (the same claim as
                 commit-green) plus `dross execute begin`, `dross quick begin` and `dross quick end`. With a subject
                 armed, a code commit needs that subject's log to end in a pass at n ≤ 2 on tree ==
                 treefp.Candidate. A .dross-only commit passes. A non-solo begin, or an end, is refused while the
                 subject is armed and code changes are uncommitted. Remedies name the dross-reviewer Agent spawn and
                 `dross task status … failed`. The README gate row lists solo-review.
       covers:   c-4, c-8, c-3
       depends:  t-5, t-6
       contract: - if a matching pass stops admitting the commit, or an edit after the pass still passes,
                   TestSoloReviewTreeMatch fails. With a pass for p/t-2 at T, `git commit -m x` on candidate T
                   passes. After a one-byte edit it refuses and names a re-review
                 - if a pass for t-1 admits a commit while t-2 is in progress, or a quick pass for another description
                   admits a quick commit, the subject-match test fails
                 - if a round-3 pass admits the commit instead of refusing with "one fix round" and
                   `dross task status <phase> <task> failed`, TestReviewRoundCap fails (c-3)
                 - if a last round that is blocked or unavailable admits the commit, or the refusal omits the
                   blocking count or the cause, the blocked test fails
                 - TestSoloReviewSilentCases fails if a .dross-only commit with no review is refused, or if any of
                   these is gated: pair mode, mode absent, no task in progress, off phase/<id>, or a quick whose HEAD
                   moved
                 - the downgrade table fails if `dross execute begin p` (no --solo), `dross quick begin "x"` or
                   `dross quick end` passes while armed with an uncommitted code edit, or refuses on a clean tree
                 - if a corrupt review.json or a failing git candidate build passes instead of refusing with the error
                   named, the closed-posture test fails
                 - if gate.All() names drift from the nine expected, the registry test fails
                 - if README's `dross gate` row omits a registered workflow gate, TestReadmeNamesEveryGate (new) fails
                 - TestGateRemediesResolve stays green: remedies name only verbs that already exist

  t-13 Fold the review log into changes record
       files:    internal/cmd/changes.go, internal/cmd/changes_test.go
       desc:     `dross changes record <phase> <task>` reads review.json. When the log's subject is that execute task,
                 it stores changes.ResolveReview(rounds) on the record, so the findings are the reviewer's own and
                 never agent-typed. `--review-unavailable <cause>` records verdict unavailable when no round exists. It
                 is refused over a log whose last round passed.
       covers:   c-6, c-3
       depends:  t-5, t-8
       contract: - if `changes record p t-2 --files a.go --commit X` with a p/t-2 log persists no review, or a p/t-1 or
                   quick log is folded into t-2, TestChangesRecordFoldsReview fails
                 - if a record with no log (pair mode) gains a review key, the pair test fails
                 - if `changes record p t-2 --files a.go` (no commit) after two blocked rounds persists anything but
                   verdict blocked with reason = round 2's BLOCKING text, TestChangesRecordFailedReason fails (c-3)
                 - the unavailable test fails if `--review-unavailable "agent not installed"` with no log does not
                   persist verdict unavailable naming that cause, or if the flag is accepted over a passing last round

  t-14 Fail doctor on a missing or stale reviewer
       files:    internal/cmd/doctor.go, internal/cmd/doctor_agents_test.go (new), internal/cmd/cmd_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       desc:     A new "Agents:" section:
                 - dross-reviewer missing → an issue naming `dross install`
                 - installed bytes (read through a link) ≠ embedded bytes → a stale issue naming `dross install` /
                   `make install` (r-01)
                 - equal → ✓
                 chdir() seeds the definition the same way it seeds the hooks.
       covers:   c-7
       depends:  t-9
       contract: - if an empty agents dir leaves doctor at exit 0, or the ✗ line does not name dross-reviewer and
                   `dross install`, TestDoctorReviewerMissing fails
                 - if either of these leaves doctor at exit 0 instead of reporting a stale issue,
                   TestDoctorReviewerStale fails: an installed definition with one byte appended, or a link to a
                   source file edited past the binary
                 - if doctor reads $HOME/.claude/agents while CLAUDE_CONFIG_DIR points elsewhere, the effective-dir
                   test fails
                 - if doctor_run.txt stops pinning the section, the golden fails
                 - if chdir() stops seeding the definition, the ~105 existing Doctor() invocations go red

  t-15 Render solo review in the PR body
       files:    internal/ship/body.go, internal/ship/body_test.go, internal/cmd/ship.go
       desc:     BuildPRBody takes the phase's changes record. When any task carries a review, it adds a
                 "## Solo review" section before the footer: per task, its verdict, plus each finding's kind/severity,
                 text and resolution. `dross ship` (and --print-body) passes changes.json in.
       covers:   c-6
       depends:  t-8
       contract: - if a record with t-2 reviewed (spec c-2 fixed, FLAG left) does not yield a "## Solo review" section
                   before the footer that names t-2 and each finding's severity, text and resolution,
                   TestPRBodySoloReview fails
                 - if a phase with no reviewed task changes any byte of the body, the existing body tests fail
                 - if a finding text containing `|` or a newline breaks the section's markdown, the escape test fails
                 - if `dross ship --print-body` stops reading the phase's changes.json, TestShipPrintBodyShowsReview
                   fails

  t-16 Surface solo review findings in verify.md
       files:    assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       desc:     §0 reads each task's `review` from changes.json. §4's report gains a "Solo review:" block listing, per
                 reviewed task, the verdict and the FLAG/NOTE findings left, plus failed tasks with their reason.
       covers:   c-6
       depends:  t-8
       contract: - if verify.md's §4 report template loses the "Solo review:" block, or stops naming changes.json's
                   `review` field and a failed task's reason, TestVerifyPromptSurfacesSoloReview fails

Wave 5 (depends t-4, t-7, t-10, t-11, t-12, t-13)
  t-17 Wire solo review into execute.md
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       desc:     §1f solo path: after the full green and before `git add`/`git commit`, spawn the Agent in the
                 foreground with subagent_type dross-reviewer and exactly review.SpawnPrompt, then read
                 `dross review status`. If blocked, run one fix round: edit, re-run the test gate, re-review. If still
                 blocked, or unavailable:
                 1. `git stash push -u` the task's work
                 2. `dross changes record` without --commit (add --review-unavailable when no round was recorded)
                 3. `dross task status … failed`
                 4. continue with `dross task next`
                 The pair path spawns no reviewer. Then `make install` (r-01).
       covers:   c-1, c-3, c-5, c-6
       depends:  t-4, t-10, t-11, t-12, t-13
       contract: - if the solo dross-reviewer spawn leaves its slot between §1f's last `dross test` and `git commit`, or
                   the pair path mentions spawning it, TestExecuteSoloReviewBeforeCommit fails
                 - if execute.md's spawn text is not byte-equal to review.SpawnPrompt, names a subagent_type other than
                   review.AgentName, or allows run_in_background, TestExecuteReviewerSpawnTemplate fails. The recorder
                   would then refuse the verdict or never see it
                 - if the blocked path allows a second fix round, skips re-running the test gate before re-review,
                   commits, or stops the loop instead of continuing with `dross task next`, TestExecuteSoloFixRound
                   fails
                 - if the failed path drops the stash or `dross changes record` (so c-6 loses the failed task's
                   findings), the failed-path test fails
                 - TestNoPromptTellsAgentToLiftGate and TestPromptCommitsSatisfyGreenGate stay green

  t-18 Wire solo review into quick.md
       files:    assets/prompts/quick.md, internal/cmd/quick_prompt_test.go
       desc:     §0 runs `dross quick begin "<description>"` (plus --solo in solo mode) once the description is
                 parsed. In solo mode, between §4 and §5, spawn the reviewer (foreground, review.SpawnPrompt) after
                 green and before `git commit`. If blocked, run one fix round. If still blocked, or unavailable,
                 discard, run `dross quick end`, skip the bump and report the finding. Every abort path runs
                 `dross quick end`. Pair mode spawns none.
       covers:   c-8, c-5
       depends:  t-4, t-7, t-10, t-11, t-12
       contract: - if `dross quick begin` (with --solo only on the solo branch) stops preceding §3 Implement,
                   TestQuickBeginsMode fails
                 - if the solo spawn leaves its slot between §4's green and §5's `git commit`, or the pair path spawns
                   it, TestQuickSoloReviewBeforeCommit fails
                 - if quick.md's spawn text is not byte-equal to review.SpawnPrompt, or names another subagent_type,
                   TestQuickReviewerSpawnTemplate fails
                 - if the blocked path allows a second fix round, or omits any of discard, `dross quick end` and
                   no-version-bump, TestQuickSoloReviewBlocked fails
```

## Coverage

| Criterion | Tasks | What makes it test-observable |
|---|---|---|
| c-1 | t-17 (enforced at runtime by t-12) | Prompt ordering test (spawn sits between the last `dross test` and `git commit` on the solo path, absent on the pair path), backed by a gate that refuses any solo code commit lacking a pass |
| c-2 | t-2, t-4, t-11 | Pass is derived in code, so a spec finding cannot be declared non-blocking. The definition's example block must parse with the same parser |
| c-3 | t-5, t-8, t-10, t-12, t-13, t-17, t-18 | Round cap enforced by the gate (n ≤ 2); failure reason derived from the log into changes.json; "next independent task" relies on the existing NextRunnable dependency skip |
| c-4 | t-3, t-5, t-6, t-11, t-12 | Tree-bound pass compared to treefp.Candidate; .dross-only and pair are silent; a source scan proves no non-recorder writer exists |
| c-5 | t-1, t-3, t-4, t-10, t-11, t-17, t-18 | Live canary capture; frontmatter isolation test; recorder refuses non-reviewer subagent_types and widened spawn prompts; packet content test |
| c-6 | t-5, t-8, t-13, t-15, t-16, t-17 | Resolution table; fold-on-record; PR body section; verify.md report block |
| c-7 | t-4, t-9, t-14 | Embed guard; install copy/link/prune; doctor missing/stale exit codes |
| c-8 | t-5, t-6, t-7, t-10, t-11, t-12, t-18 | Quick marker bound to HEAD; quick subject in packet, recorder and gate; quick.md ordering and spawn template |

8/8 criteria covered.

## Judgment calls

1. **Pass is derived in code, not trusted from the reviewer.** Pass means no spec finding and no BLOCKING quality finding. Rejected: trusting a declared `verdict` field. That way c-2's "every spec finding is blocking" holds even when the model mislabels a severity.
2. **The reviewer fetches its own input.** It runs `dross review packet`, and the recorder enforces a fixed spawn prompt (review.SpawnPrompt). Rejected: the executing agent pasting the diff and criteria into the prompt. That paste is exactly how conversation would leak in (c-5) and how a diff could be filtered (review_input).
3. **The tree is bound by an echo.** The packet prints `tree:`, the verdict must echo it, and the recorder compares it to WorkingTree at PostToolUse. Rejected: a recorder-time fingerprint alone, because a Bash-capable reviewer could edit mid-review and vouch for a tree nobody reviewed. Also rejected: a PreToolUse gate that writes a "review started" record, because Judges must not write.
4. **TENSION, the judge should rule: the packet also carries locked decisions and hard rules.** c-5 says the reviewer sees *only* the task record, covered criteria and diff. But the locked quality_blocking decision makes "locked-decision or rule violation" BLOCKING, which the reviewer cannot judge without seeing them. I kept quality_blocking satisfiable and kept CLAUDE.md, memory and conversation excluded. If the judge reads c-5 strictly, drop the constraints section from t-10 and accept that the reviewer cannot flag decision violations. The alternative is to amend c-5.
5. **The patch excludes `.dross/`.** This matches treefp's tree domain, which is what the pass binds to. During a run, `.dross/` changes are plan-status and changes.json bookkeeping that commits ungated. The rest of review_input is kept: everything outside `.dross/`, including files no task declares. I'm flagging this in case it reads as narrowing the locked decision.
6. **A separate `solo-review` gate instead of extending commit-green.** A human can lift it separately (`dross gate off solo-review`) and its refusal text stays distinct. The cost is a duplicate Candidate computation per commit.
7. **The round cap lives in the gate (n ≤ 2), not only in the prompts.** This makes c-3's "one fix round" a property a test can observe. A prompt-only cap could be exceeded silently.
8. **One review.json per subject, restarting when the subject changes.** Rejected: per-task log files. The flow always records changes before the next task's review, and per-task files would need cleanup and a GC story.
9. **The failure reason lives in changes.json's review, derived from the log.** Rejected: a new plan.toml `reason` field plus `task status --reason`. c-6 already puts the review in the change record, and an agent-typed reason could diverge from the reviewer's actual finding. `--review-unavailable <cause>` is the one agent-typed path. It covers only "the reviewer never ran", where no round exists, and it is refused over a pass.
10. **A failed execute task is stashed (`git stash push -u`), not discarded.** The next independent task needs a clean tree for its whole-diff review, and stash keeps the work recoverable. Quick discards instead, matching its existing solo red-test abort.
11. **Quick gets a new HEAD-bound `dross quick begin/end` marker.** Rejected: reusing execute.json (quick has no phase or plan) and time-based expiry. HEAD binding disarms the gate automatically at the work commit, so §6's bookkeeping commit passes. `quick end` cleans up after an abort.
12. **Mode-downgrade guard.** It mirrors tool-gate-hooks' solo-upgrade refusal, but fires only while code changes are uncommitted. Without it, a solo run could re-`begin` in pair mode and commit unreviewed. A downgrade on a clean tree stays allowed, for abort cleanup or a fresh pair run.
13. **TENSION with the user's background-only subagent memory: the reviewer runs in the foreground.** The recorder ignores `run_in_background` launches because a background launch's PostToolUse carries no verdict, and the gate needs the result before the commit. Ignoring them, rather than recording an unavailable round, keeps such a launch from burning the single fix round.
14. **Agents install under `CLAUDE_CONFIG_DIR/agents`, else `~/.claude/agents`.** This differs from skills, which ignore CLAUDE_CONFIG_DIR. Claude Code reads agents from the config dir, and pinning CLAUDE_CONFIG_DIR in chdir() keeps the doctor tests hermetic.
15. **Stale means installed bytes (read through a symlink) ≠ embedded bytes.** That catches both an old copy and a dev link whose source was edited past the binary (r-01). Rejected: checking only the symlink target, which misses the second case.
16. **t-1's capture is a wave-1, human-in-the-loop go/no-go using a scratch definition.** That puts the canary proof of reviewer_isolation ahead of any code that depends on omitClaudeMd. t-4's test then pins the shipped definition's keys.
17. **Gate remedies name only verbs that already exist** (the Agent spawn, `dross task status … failed`), not `dross review status`. This keeps t-12 parallel with t-10 in wave 4 without breaking TestGateRemediesResolve.
18. **Quick reviews are not folded into changes.json.** c-6 scopes persistence to solo execute tasks, and a standalone quick has no change record. Its audit trail is the review.json round and the `solo: yes` commit body.
