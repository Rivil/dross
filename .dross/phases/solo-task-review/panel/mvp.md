# mvp lens — solo-task-review

Bias: the smallest task set that satisfies every criterion. Every task traces to a criterion; no speculative structure.

```
Phase solo-task-review — 6 tasks across 3 waves

Wave 1
  t-1  Ship the dross-task-reviewer agent definition
       files:    assets/agents/dross-task-reviewer.md (NEW), assets/embed.go, assets/embed_test.go
       desc:     Custom agent: frontmatter name/description, tools Read/Grep/Glob/Bash only, `omitClaudeMd: true`, no `memory`, no `model`.
                 Body: gather own inputs (`dross task show`, covered criteria + locked decisions from `dross phase show --json`, `dross rule show`,
                 or the quick description given in the prompt) and the whole diff vs HEAD (`git diff HEAD` + `git ls-files --others --exclude-standard`).
                 It emits one fenced `dross-review` JSON block: spec[] {criterion, surface, finding}, kept apart from quality[] {grade BLOCKING|FLAG|NOTE, finding}.
                 Every spec finding blocks. embed.go adds `all:agents`.
       covers:   c-2, c-5
       contract: TestReviewerIsolation fails if the EMBEDDED agents/dross-task-reviewer.md frontmatter drops `omitClaudeMd: true`, or gains a `memory` or `model` key
                 the same test fails if `tools` gains Edit, Write, MultiEdit, NotebookEdit or Agent (the reviewer must not change the tree it reviews)
                 the same test fails if the body stops naming both `git diff HEAD` and the untracked listing (review_input: whole diff, not task.files)
                 TestEmbedDrift fails if assets/agents/*.md on disk and the embedded agents/ set differ (a dropped `all:agents`)

  t-2  Add review and quick-mode gate records
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go, internal/cmd/execute.go, internal/cmd/execute_test.go,
                 cmd/dross/testdata/cli_tree.txt (regenerated golden)
       desc:     gatestate: review.json {phase, task, rounds[{tree, pass, cause, findings[{kind spec|quality, grade, criterion, text}], at}]} written by
                 AppendReviewRound, which drops prior rounds when (phase, task) changes. quick.json {mode, head, at}. New `dross execute quick [--solo]`
                 records the /dross-quick mode at the current HEAD. Both records sit under .dross/gate/, so the existing tamper-guard already covers them.
       covers:   c-4, c-6, c-8
       contract: AppendReviewRound for (p, t-4) after rounds for (p, t-3) leaves only t-4's round, so a stale task's pass never carries over
                 a review.json whose round has pass=true and an empty tree errors on load (an empty fingerprint could only match another empty one)
                 `dross execute quick --solo` writes quick.json {mode: solo, head: <HEAD sha>}; a following plain `dross execute quick` overwrites it with mode pair

Wave 2 (t-3 depends t-1, t-2 · t-4 depends t-2 · t-5 depends t-1)
  t-3  Record reviewer verdicts; gate solo commits on them
       files:    internal/gate/review.go (NEW), internal/gate/review_test.go (NEW), internal/gate/commit.go,
                 internal/gate/testdata/agent_reviewer_post.json (NEW fixture)
       desc:     First, capture a real Agent PostToolUse payload for dross-task-reviewer. Do it in a scratch repo's .claude/settings.local.json and
                 .claude/agents/, with the human's go-ahead, as t-20 did last phase. STOP if tool_response carries no readable final text.
                 Recorder: claims Agent calls whose subagent_type is dross-task-reviewer. It parses the dross-review block and sets pass itself:
                 no spec finding and no BLOCKING quality finding. It binds the round to treefp.WorkingTree. A missing block, an unparseable block or
                 a background spawn appends a non-pass round with its cause.
                 Gate `solo-review` (Workflow, Liftable): reuses commit-green's candidate logic, factored into a helper in commit.go. It is armed when
                 (a) execute.json is solo for current_phase, one task is in_progress and HEAD is on phase/<id>, or else (b) quick.json is solo with
                 head == current HEAD. An execute run takes precedence over the quick marker. While armed it refuses a code commit unless the last
                 round passed, is for the armed (phase, task) and has tree == candidate. A .dross/-only commit passes.
       covers:   c-2, c-4, c-8
       contract: replaying the captured fixture (verdict with one FLAG only) through `gate record` appends a pass round whose tree == treefp.WorkingTree
                 a verdict with one spec-compliance finding records pass=false even when the block's own verdict field says "pass"
                 a response with no parseable dross-review block records a non-pass round whose cause names the parse failure, and the commit refusal repeats that cause
                 an Agent call with subagent_type general-purpose emitting a passing block records nothing
                 execute.json solo + t-2 in_progress on phase/<id>: a code commit with no review is refused; a pass on tree T admits candidate T; candidate T' != T is refused
                 the same armed repo admits a .dross/-only commit; execute.json mode pair is silent; quick.json solo at current HEAD arms; quick.json solo at an older HEAD is silent
                 the example verdict in the embedded reviewer definition parses with ParseVerdict, so the prompt and the parser cannot drift
                 Bash `dross gate record` piped a forged reviewer payload is refused by gate-off-guard (review_pass_signal: no agent-reachable pass path)

  t-4  Fold review rounds into the task change record
       files:    internal/changes/changes.go, internal/changes/changes_test.go, internal/cmd/changes.go, internal/cmd/changes_test.go
       desc:     TaskRecord gains `review` {verdict pass|blocked|unavailable, findings[{kind, grade, criterion, text, resolution}]}.
                 `dross changes record <phase> <task>` attaches review.json when its (phase, task) match. Resolution per finding: blocking in an
                 earlier round with a final pass → "fixed in fix round"; FLAG/NOTE in the final round → "non-blocking, left"; blocking in a final
                 blocked round → "unresolved".
       covers:   c-6
       contract: rounds [blocked: spec c-3 + FLAG naming], [pass: FLAG naming] fold to verdict pass, spec finding "fixed in fix round", FLAG "non-blocking, left"
                 a final blocked round folds to verdict blocked with its blocking finding "unresolved"
                 `dross changes record p t-4` does not attach a review.json held for t-3; with no review.json the written record has no `review` key (pair mode unchanged)

  t-5  Install the reviewer agent; doctor fails without it
       files:    internal/cmd/install.go, internal/cmd/install_test.go, internal/cmd/doctor.go, internal/cmd/doctor_hooks_test.go,
                 internal/cmd/cmd_test.go, internal/cmd/testdata/cli_surface/doctor_run.txt (regenerated golden)
       desc:     installer syncs embedded agents/*.md into <claude config dir>/agents/: CLAUDE_CONFIG_DIR is honoured, as Claude Code does, otherwise ~/.claude.
                 Link mode symlinks; copy mode writes the bytes. Stale dross-* agents are pruned and other agents are never touched. Doctor gets a new
                 "Agents:" section: an issue (non-zero exit) when the installed dross-task-reviewer.md is missing or its bytes differ from the
                 embedded copy (stale); otherwise ✓. chdir() seeds the agent as it seeds the four hooks.
       covers:   c-7
       contract: copy-mode install writes $CLAUDE_CONFIG_DIR/agents/dross-task-reviewer.md byte-equal to the embedded file; link mode makes it a symlink into assets/agents/
                 an installed agents/dross-old.md is pruned while a non-dross agents/foo.md survives verbatim
                 doctor exits non-zero naming the reviewer when the file is absent, and again when its bytes differ from the embedded copy; equal bytes print ✓ and add no issue

Wave 3 (depends t-1, t-2, t-3, t-5)
  t-6  Run the reviewer in solo execute and quick
       files:    assets/prompts/execute.md, assets/prompts/quick.md, internal/cmd/execute_prompt_test.go, internal/cmd/quick_prompt_test.go
       desc:     execute.md gets a solo-only review step between the full `dross test` green and `git commit`. It spawns a fresh FOREGROUND Agent
                 (subagent_type dross-task-reviewer) passing only the phase id and task id. A block opens one fix round: edit, re-run the test gate,
                 re-review. Still blocked or unavailable: stash the work under a named stash, run `dross task status … failed` and
                 `dross changes record … --notes "review: <finding or cause>"`, then continue. Pair mode spawns none.
                 quick.md pre-flight runs `dross execute quick --solo`, or plain `dross execute quick` in pair mode. Solo then reviews against the
                 verbatim description before §5, with one fix round; still blocked means abort and discard with no bump, and re-record pair.
       covers:   c-1, c-3, c-5, c-8
       contract: execute_prompt_test fails if the review step stops sitting between the bare `dross test` and `git commit`, or stops naming subagent_type dross-task-reviewer as a foreground spawn
                 the same test fails if execute.md stops saying pair mode spawns no reviewer, or if the spawn inputs grow beyond phase id + task id (c-5: no conversation excerpt)
                 the same test fails if the one-fix-round rule or the still-blocked path (stash, `task status … failed`, `changes record … --notes` naming the finding, continue) is dropped
                 quick_prompt_test fails if solo pre-flight stops running `dross execute quick --solo`, pair stops running plain `dross execute quick`, or the solo review stops preceding §5's `git commit`
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 solo-only reviewer between green and commit | t-6 (prompt step, pair spawns none), t-3 (gate arms only in solo) |
| c-2 spec findings kept apart from quality findings, all blocking | t-1 (verdict format), t-3 (parser derives pass: any spec finding blocks) |
| c-3 one fix round, then failed + not committed + next task | t-6 (`dross task next` already skips tasks that depend on a failed task) |
| c-4 solo commit gated on a pass for the exact tree; .dross-only and pair ungated | t-3 (solo-review gate), t-2 (review.json) |
| c-5 reviewer isolation + a test that fails if it regresses | t-1 (definition + isolation test), t-6 (Agent spawn with ids only) |
| c-6 verdict, findings and resolution persisted with the change record | t-4 (fold + resolution), t-2 (round history) |
| c-7 install the agent; doctor fails when missing or stale | t-5 |
| c-8 quick --solo reviewed and commit-gated; pair quick ungated | t-2 (quick.json + verb), t-3 (quick arming), t-6 (quick.md) |

Locked decisions:
- **quality_blocking:** t-1 grades findings; t-3 blocks only on BLOCKING.
- **reviewer_model:** t-1 pins no model, and its test asserts that.
- **review_pass_signal:** only t-3's PostToolUse recorder writes a pass, and the existing tamper-guard and gate-off-guard keep the agent off review.json and `gate record`.
- **reviewer_isolation:** t-1 sets `omitClaudeMd` and adds no `memory`; t-6 spawns it through the Agent tool.
- **review_input:** t-1's body reviews the whole diff vs HEAD, untracked files included.
- **review_unavailable:** t-3 records a non-pass round with its cause and refuses the commit; t-6 marks the task failed naming that cause.

8/8 criteria covered.

## Judgment calls

- **Reviewer inputs:** the reviewer gathers them itself with existing verbs (`dross task show`, `dross phase show --json`, `dross rule show`, `git diff HEAD` plus the untracked listing). I rejected a new `dross review packet` verb: it is one more surface, and because the reviewer reads git itself the executing agent cannot hand it a doctored diff.
- **Gate placement:** `solo-review` is a separate gate in review.go rather than an extra check inside commit-green. commit-green goes silent in a repo with no tests (`no_test_repo`), which would quietly drop c-4's requirement. A separate gate also gives the human a separate off switch.
- **Quick mode marker:** it is `dross execute quick [--solo]` writing quick.json bound to HEAD, so the work commit itself spends it. I rejected a top-level `dross quick begin`, which needs main.go registration for no gain. I also rejected an explicit "end" verb: the agent could run it, so it would be a way out of the gate.
- **Arming precedence:** an in-progress execute run wins over the quick marker. A stale solo marker left by an aborted solo quick would otherwise gate a pair-mode execute commit, and c-4 says pair mode stays ungated.
- **Where c-6 is written:** `dross changes record` folds the machine-local review.json into the record. I rejected having the hook write changes.json directly. Hooks keep writing only .dross/gate/, as tool-gate-hooks set up, and resolution is computed from the round history in one place.
- **Failed-task reason:** it goes through the existing `dross changes record --notes`. I rejected adding a `reason` field to `dross task status`: that is a schema change no criterion asks for, and the review fold already puts the findings on that record.
- **Failed task's diff:** it is stashed under a named stash, not discarded. review_input is the whole uncommitted diff, so leftovers would contaminate the next task's review. A stash keeps the work recoverable.
- **Who decides pass:** the recorder derives it (no spec finding and no BLOCKING finding) and does not trust the reviewer's own verdict field. That makes c-2's "every spec finding blocks" a property of the code and testable, not a property of a prompt.
- **Foreground spawn:** the reviewer must run in the foreground, because a background spawn's PostToolUse carries no verdict. This overrides this repo's "background-only agents" habit for this one spawn, and t-6 says so in the prompt.
- **Fixture capture:** it is folded into t-3 as its first step, with a STOP condition, rather than a separate go/no-go task. It is the same capture t-20 did; I kept one task with a guard instead of two tasks.
- **Agents directory:** it honours CLAUDE_CONFIG_DIR, because that is where Claude Code reads user agents and the chdir test helper already isolates it. I rejected matching install's `home/.claude` used for skills, which would leave doctor and Claude Code disagreeing whenever CLAUDE_CONFIG_DIR is set.
- **t-6 in wave 3:** it waits for t-3 and t-5 even though the prompt text only needs names from t-1 and t-2. A link-mode install makes prompt edits live immediately, so no prompt may tell a solo run to spawn a reviewer before the agent is installed and the recorder exists.
- **t-5 size:** six files, kept as one task. Two of them are mechanical: the doctor golden is regenerated and the chdir seed is one line. Splitting install from doctor would leave the install half under 10 minutes of work and checking nothing.
- **Not in the plan: review findings in the PR body or the verify report.** c-6 asks for persistence "so /dross-verify and the PR body can show" the findings. They are persisted and `dross changes show` exposes them. I rejected rendering them: BuildPRBody takes no change record today, so it would mean a ship-layer signature change. This is the omission most likely to be challenged.
- **Not in the plan: a guard against downgrading solo to pair mid-task** (`dross execute begin <id>` or `dross execute quick` while a solo review is armed). No criterion asks for it, and deliberate evasion is outside the locked threat model (`shell_detection_depth`).
