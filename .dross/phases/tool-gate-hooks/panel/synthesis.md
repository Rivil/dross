# tool-gate-hooks — panel synthesis

## Scores

| Dimension | risk (17 tasks / 5 waves) | mvp (8 tasks / 3 waves) | verification (15 tasks / 5 waves) |
|---|---|---|---|
| Criteria coverage | 5/5. Covers 9/9, plus the regressions the others miss: resume.md's handoff rewrite tripping c-8, init/ship commits, a forged green via Write, exit-1 fail-open, panic exit-2. | 3/5. Covers 9/9 on paper, but its own gates break add-then-commit chains, init's first commit and resume's prune. `state set execute_mode solo` is an unguarded agent override of c-5. | 4/5. Covers 9/9, adds remedy resolution, guard_scope and the no-write locator. Misses the resume/pause c-8 trap and record tampering. |
| Test-contract specificity | 5/5. Every contract names a failure mode (heredoc commit, real-index sha256, 499/500 boundary, exit 2 not 1). t-8's zero-telemetry contract can't pass within its listed files. | 3/5. Names gate and message text and has the 499/500 boundary. Few failure modes: no real-index safety, heredoc or concurrency case. | 5/5. Criteria-first allow/refuse tables (wc/shasum/grep -q allowed; a replayed add equals a real add). Names the test functions. |
| Granularity | 4/5. Every task has ≤4 files and ≤2 layers. t-12 leaves out cli_tree.txt even though its new verbs change that golden. | 2/5. t-5 has 5 files across 3 layers (gate/state/cmd). t-1 bundles engine, scanner and secret gates. t-8 bundles a prompt with a state-CLI change. | 3/5. t-2 has 6 authored files; the onboard_test edit is unneeded. t-14 sits at the limit. The rest are fine. |
| Wave correctness | 3/5. t-16 depends on 9 tasks for safety ordering, which the draft admits is not a data dependency. t-14 depends on t-6 with no data need. Otherwise data-correct. | 4/5. Data-correct. But wiring in wave 1 points at verbs from wave 2, and doctor's fail-on-absent lands before any gate exists, which is a dogfood hazard. | 3/5. t-13 waits on all four gate tasks only for a registry-equality contract, which pushes human verbs to wave 4 and execute.md to wave 5. Wiring in wave 1 names verbs that only arrive in wave 3. |

**Skeleton: risk.** It is the only draft whose tasks all fit the size limits. It owns the most failure modes with the sharpest contracts. It is the only one that found the resume/pause c-8 trap and the localstore truncation fact. Verification supplies most of the grafts: allow/refuse tables, remedy resolution, the no-write locator, ship/init in wave 1. MVP supplies two: the MergeHook-equality presence check and the interaction-audit doc drift.

### Code claims checked

- **boundary_test.go bans `encoding/json` and `BurntSushi/toml` in non-test `internal/cmd`.** True, as verification says (`forbiddenInCmd`). Boundary rule 2 also forbids any internal package from importing `internal/cmd`. So `internal/gate` cannot reuse `FindRoot`/`GlobalDir` and needs its own locator. `FindRoot` writes state.json via `ensureState`, which makes verification's "locator never writes" contract necessary, not cosmetic.
- **`chdir()` (cmd_test.go:29) pins `CLAUDE_CONFIG_DIR` to an empty `t.TempDir()`.** True. There are about 105 `Doctor()` invocations across 15 test files. Risk's "~100" is right; verification's "about 20" undercounts. MVP says its Init-based fixtures stay green. That holds for the 33 `Init()` calls in doctor_test.go, but test_lane_consent_test, hostile_config_test, gitseparator_remaining_test and execconsent_docs_test call `Doctor()` with no Init. Seeding in `chdir` (risk+verification) is the safe fix.
- **`localstore.Save` truncates in place.** True, as risk says: store.go:582 uses `os.Create(path)` with no temp+rename. `state.Save` truncates too (`os.WriteFile`, state.go:58). Neither is atomic.
- **`dross state set` is a generic writer an agent could use to forge a mode (risk).** False today. state.go:102 is a closed switch over version/current_milestone/current_phase/current_phase_status, so MVP is right. Risk's concern becomes true under MVP's own t-8, which adds `execute_mode` to that switch.
- **state.json and local.toml are gitignored.** Both are, in .gitignore, and init/onboard scaffold the entries (gitignore_test.go).
- **gitrun has no env seam.** True (`Options` = Timeout, NoOptionalLocks, Stdin). Risk and verification both add `IndexFile`.
- **`ensureUserHooks` write path.** It writes with a plain `os.WriteFile` (0644 when new, non-atomic). `hooks.MutateSettings` already does temp+rename at 0600, so risk's atomic-write contract reuses an existing pattern.
- **MergeHook keys idempotency on the command string and ignores matchers.** True (`groupRunsCommand`). So MVP's presence check (`MergeHook(b,ev,cmd) == b`) counts a matcher-restricted group as wired. Risk's extra matcher check in doctor is needed.
- **Exit codes.** main.go maps untagged errors to exit 1 via `cmd.ExitCode`. The root has `SilenceErrors: false`, so cobra prints `Error:` as well as main's `dross:` line. Verification's "SilenceErrors on the verb → stderr once" is a real requirement.
- **Telemetry.** `RecordCLIEvent` runs for every command from main. Risk t-8 and mvp t-7 both promise "zero telemetry lines" without listing `telemetry.go` or a cmd/dross test, so neither contract can be met in-process with their files. Verification lists `telemetry.go`, which is right.
- **resume.md §2 rewrites handoff.md and pause.md §3 overwrites it.** True: resume.md:44 says "rewrite `.dross/handoff.md` with only what's left", and pause.md:92 says "(overwrites)". Risk's t-14 premise holds: c-8 would refuse a prune that more than halves the file.
- **init.md:123 and ship.md:101 commit non-.dross paths; quick.md:99 already runs a bare `dross test`.** All true.
- **`TestNarratedCommandsResolveAgainstTheTree` scans only `internal/cmd/*.go`.** True. Remedies in `internal/gate` are unchecked, so verification's `TestGateRemediesResolve` fills a real gap.
- **`TestOnboardEnsuresSameHooks` compares init and onboard settings byte for byte.** True. It already covers four hooks without edits, so verification's onboard_test.go edit is unnecessary.
- **execute.md supports repos with no test_command.** True: it prints "(none — verify will catch this later)". This bears on disagreement 5.

## Merged plan

Convention: a mechanically regenerated golden (`cli_tree.txt`, `doctor_run.txt`) is listed and tagged `(golden)`. It does not count toward the 5-file split threshold.

```
Phase tool-gate-hooks — 18 tasks across 4 waves

Wave 1
  t-1  Add lenient shell command scanner  [risk+verification]
       files:    internal/shellscan/scan.go, internal/shellscan/scan_test.go
       desc:     Quote/escape-aware lexer. Splits a Bash line into simple commands on && || ; | |& & and newline.
                 Per command: argv with VAR=val and env/command/sudo/nohup/time prefixes stripped, argv0 as
                 basename, redirections with fd attribution, `cd` dir tracking, `git -C`. Recurses into
                 $(…)/backticks, skips heredoc bodies, propagates group/subshell redirects inward. An unbalanced
                 quote sets Partial=true and never panics. (scanner depth: disagreement 12)
       covers:   c-4, c-6, c-7
       depends:  —
       contract: `git commit -m "$(cat <<'EOF'\nstop cat .env 2>&1; a && b\nEOF\n)"` → one git-commit command
                   plus a nested `cat` with NO file operand. If heredoc bodies become commands,
                   TestHeredocCommitIsOneCommand fails, and every commit message mentioning .env would be refused.
                 `a && b; c | d || e` → 5 commands in order. `echo "a && b"` and `echo 'x; y'` stay one command.
                 MergesStderr true for `x 2>&1`, `x &>f`, `x &>>f`, `x >&f`, x in `x |& y`, and inside
                   `( pass-cli view ) 2>&1`. False for `x 2>/dev/null`, `x >&2`, `x 1>&2`, `echo "2>&1"`.
                   `pass-cli x |& head` attributes the merge to pass-cli, not head.
                 `FOO=1 env BAR=2 /usr/bin/pass-cli item view x` → argv0 `pass-cli`, args [item view x].
                 `cd sub && git -C ../other commit` resolves the commit dir to <cwd>/other. `cat < .env` exposes
                   `.env` as an input operand.
                 `sh -c "pass-cli x 2>&1"` → argv0 `sh`, body not descended (shell_detection_depth out-of-scope).
                 An unbalanced quote → Partial=true. A fuzz seed corpus never panics.

  t-2  Fingerprint trees on a temporary git index  [risk+verification]
       files:    internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go,
                 internal/treefp/treefp.go, internal/treefp/treefp_test.go
       desc:     Add gitrun.Options.IndexFile (GIT_INDEX_FILE). treefp computes three things:
                   WorkingTree() — tracked + untracked-unignored files, excluding .dross/
                   Candidate(adds, all) — a copy of the real index with earlier `git add` arg-lists (and -a) replayed
                   ChangedPaths — staged paths that differ from HEAD
                 Handles an unborn HEAD and linked worktrees (`rev-parse --git-path index`). Every git call has a
                 timeout.
       covers:   c-4
       depends:  —
       contract: The real index is never touched. With a partially staged repo, the index file's sha256 and
                   `git diff --cached --name-only` match before and after WorkingTree() and Candidate(). An empty
                   IndexFile must not fall through to the real index.
                 WorkingTree()==Candidate() after `git add -A`. They differ when one modified file is left
                   unstaged. Editing .dross/state.json changes neither. An untracked unignored file changes
                   WorkingTree() and an ignored one does not. chmod +x changes it.
                 Candidate(["--","a.go"]) equals the plain candidate after really running `git add a.go`. If the
                   replay diverges from git, TestCandidateMatchesRealAdd fails.
                 ChangedPaths with only .dross/x staged → exactly [".dross/x"], including on an unborn HEAD.
                   A linked worktree resolves its own index.
                 A git failure returns an error, never "" (an empty fingerprint could equal an empty record).
                   RawWith(Options{IndexFile: tmp}, "ls-files") lists the temp index.
                   TestNoUnseparatedGitPositional stays green.

  t-3  Gate engine: payload, registry, dispatch, root locator, lists  [risk+verification+mvp]
       files:    internal/gate/engine.go, internal/gate/payload.go, internal/gate/lists.go,
                 internal/gate/engine_test.go
       desc:     Decode the hook payload (tool_name, tool_input, tool_response, cwd, session_id). Gates
                 {Name, Scope always-on|workflow, Liftable, Claims, Judge} and Recorders register at init.
                 Check runs every non-lifted gate with per-gate panic recovery and joins refusals. A Refusal
                 needs a Rule and a Remedy and carries the human-escape hint. Read-only root locator: requires
                 .dross/project.toml and never writes. Default lists are unioned with an additive
                 ~/.claude/dross/gates.toml. (malformed-file posture: disagreement 7)
       covers:   c-2
       depends:  —
       contract: An unparseable payload (`{`, empty stdin, missing tool_name) → Allow + one warning, zero Judge calls.
                 A Judge error → refused naming the gate and the error. A claiming gate whose Judge panics →
                   refused naming the gate + "internal error". An uncaught panic would exit 2 and block every
                   call with a stack trace.
                 NewRefusal with an empty Rule or Remedy returns an error. Every refusal contains the gate name,
                   "Use: <remedy>", and "a human can run `dross gate off <name>` in their own terminal".
                 guard_scope: a workflow fake gate is NOT invoked when the target has no root; an always-on fake
                   gate IS. A `.dross/` without project.toml counts as no root. The locator leaves no state.json
                   in a fixture root that lacked one (FindRoot's ensureState would create one).
                 An unclaimed tool (Glob) → Allow, empty output, no Judge call, zero git argv (gitrun.ArgvRecorder).
                 A lifted gate is skipped while the others run. A Liftable=false gate ignores overrides. Two
                   refusing gates → both rules in one message.
                 `secret_tools = []` in gates.toml cannot remove pass-cli. A malformed gates.toml makes the
                   list-extensible gates refuse their in-domain calls naming the file path. Other gates judge
                   normally.

  t-4  Add machine-local gate state store  [risk; graft verification]
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go
       desc:     `.dross/gate/` holds a self-ignoring `.gitignore` (`*`) and three single-writer JSON records:
                 green.json {tree, at, runner}, execute.json {phase, mode, at}, approval.json {phase, task, head, at}.
                 Writes are atomic (unique temp + rename). A missing file means no record; a corrupt one is an
                 error naming the path. (state location: disagreement 1)
       covers:   c-4, c-5
       depends:  —
       contract: After the first write, `git status --porcelain` shows nothing under .dross/gate/ and
                   `git add .dross/` stages nothing from it, with NO root .gitignore entry.
                 N concurrent writers and readers never see a partial record: every read is not-exist or decodes.
                   An in-place truncating writer (localstore.Save / state.Save style) fails this test.
                 A truncated green.json → error naming .dross/gate/green.json, not a zero record. A missing one →
                   (nil, nil). Writing a green with an empty tree is rejected.
                 A force-added (git-tracked) .dross/gate/*.json is refused unread, so a cloned repo cannot ship
                   its own green or approval. This mirrors consent.RefuseTrackedLocal [graft: verification t-4].

  t-5  Run dross test before prompt commits that stage code  [verification+risk]
       files:    assets/prompts/ship.md, assets/prompts/init.md, internal/cmd/ship_prompt_test.go,
                 internal/cmd/prompt_test_command_test.go
       desc:     ship.md §3 runs a bare `dross test` before the ARCHITECTURE.md docs commit. When
                 runtime.test_command is set, init.md §9 runs `dross test` (after the human's `dross trust`) between
                 the .gitignore step and `git add . && git commit`. No prompt tells the agent to run `dross gate off`.
                 Wave 1 because it needs no gate code. (prompt-flow inclusion: disagreement 8)
       covers:   c-4
       depends:  —
       contract: TestShipPromptTestsBeforeDocsCommit: in ship.md a `dross test` line precedes `git commit -m "docs(`.
                   Deleting or moving it fails the test.
                 In init.md, `dross test` sits between the .gitignore step and `git add . && git commit`.
                 ship.md and init.md join suiteRunningPrompts, so a raw `<runtime.test_command>` line fails
                   TestPromptsRunDrossTest.
                 TestNoPromptTellsAgentToLiftGate: any assets/prompts/*.md telling the agent to run `dross gate off`
                   fails. Asking the user to run it in their own terminal passes.

  t-6  Prune and re-pause handoff.md with Edit  [risk]
       files:    assets/prompts/resume.md, assets/prompts/pause.md, internal/cmd/resume_prompt_test.go,
                 internal/cmd/pause_prompt_test.go
       desc:     resume.md §2 prunes with Edit instead of rewriting handoff.md. pause.md uses Write only to create
                 handoff.md and Edit to update an existing one. Without this, c-8 refuses both routine flows. Moved
                 from risk's wave 3: it needs nothing from the curated-shrink gate. (disagreement 8)
       covers:   c-8
       depends:  —
       contract: resume_prompt_test fails if §2 goes back to rewriting .dross/handoff.md wholesale instead of using Edit.
                 pause_prompt_test fails if pause.md overwrites an existing handoff.md with Write.

Wave 2
  t-7  Always-on secret guards and gate-off guard  [risk+verification+mvp]
       files:    internal/gate/secrets.go, internal/gate/secrets_test.go, internal/gate/selfguard.go,
                 internal/gate/selfguard_test.go
       desc:     secret-stream (c-6) refuses a command whose argv0 is in SecretTools when stderr is merged into
                 stdout. secret-read (c-7) refuses a Read, or cat/head/tail/less/sed with an operand or `<` input,
                 of a path matching SecretPaths: *.env, .env*, *.pem, *.key, id_* (not *.pub), *.agekey,
                 sops/age/keys.txt. `source` and `.` are never claimed. gate-off-guard (Liftable=false) refuses any
                 Bash `dross gate off`. All three fire in every repo.
       covers:   c-6, c-7, c-2
       depends:  t-1, t-3
       contract: c-6 refused: `pass-cli item view x 2>&1 | head`, `… |& head`, `… &>out`, `FOO=1 pass-cli … 2>&1`,
                   `cd d && pass-cli … 2>&1`, `/opt/bin/pass-cli … 2>&1`. The message names secret-stream and
                   `>file 2>/dev/null`.
                 c-6 allowed silently: `make build 2>&1`, `go test ./... 2>&1 | tail`,
                   `pass-cli item view x >f 2>/dev/null`, `echo hi 2>&1; pass-cli item view x >f 2>/dev/null`,
                   `echo "pass-cli x 2>&1"`.
                 c-7 Read refused: .env, prod.env, .env.local, server.pem, tls.key, ~/.ssh/id_ed25519,
                   ~/.config/sops/age/keys.txt, ops.agekey. Allowed: id_ed25519.pub, envoy.yaml, main.go, and a
                   keys.txt outside sops/age.
                 c-7 Bash refused: `cat .env`, `head -n 3 config/prod.env`, `tail -f .env.local`, `less id_rsa`,
                   `sed -n 1p ~/.ssh/id_ed25519`, `cat < .env`, `cd x && cat .env`. Allowed: `source .env && make`,
                   `. ./.env`, `wc -c .env`, `shasum -a 256 .env`, `grep -q KEY .env`, `cat ~/.ssh/id_ed25519.pub`.
                 Both secret gates refuse from a cwd with no .dross ancestor (guard_scope) and spawn zero git argv.
                 Adding `op` to secret_tools in gates.toml makes `op read x 2>&1` refused.
                 gate-off-guard refuses `dross gate off secret-read`, `FOO=1 dross gate off x` and
                   `make && /usr/local/bin/dross gate off x`, even with every gate lifted. It allows
                   `dross gate status`, `dross gate on x` and `echo dross gate off`.

  t-8  Gate plan.toml edits and curated-file shrinks  [risk+mvp+verification]
       files:    internal/gate/drossfiles.go, internal/gate/drossfiles_test.go
       desc:     plan-edit (c-3, plan_overwrite via phase.LoadPlan): Edit/MultiEdit of an existing
                 .dross/phases/*/plan.toml is refused with remedy `dross task add/edit/move/remove`. Write that
                 creates the file is allowed. Write over it is allowed only while every task is "" or pending.
                 curated-shrink (c-8): a Write replacing an existing curated file with less than half its bytes is
                 refused, remedy Edit. Both are workflow-scoped.
       covers:   c-3, c-8
       depends:  t-3
       contract: Edit and MultiEdit on an existing plan.toml are refused naming `dross task edit`. A Write that
                   creates one is allowed. A Write over an all-pending plan (empty status included) is allowed;
                   over a plan with an in_progress, done or failed task it is refused.
                 A Write over an unparseable plan.toml is refused naming the decode error. A curated file that
                   can't be read (chmod 000) → refused naming the read error.
                 Table over every default curated path (project.toml, milestones/v1.toml, phases/p/spec.toml,
                   handoff.md, rules.toml), each 1000 bytes: a 499-byte Write is refused, 500 and 2000 are allowed,
                   and an Edit of any size is allowed.
                 A new milestones/v2.toml Write is allowed. Shrinking non-curated phases/p/changes.json or notes.md
                   is allowed. docs/plan.toml is ignored by plan-edit. A curated path added via gates.toml is enforced.
                 `.dross/phases/x/../x/plan.toml` is normalised and caught. The same layout with no project.toml
                   ancestor is allowed.

  t-9  Record full green runs from dross test  [risk+verification+mvp]
       files:    internal/cmd/test.go, internal/cmd/test_green_test.go
       desc:     Write green.json from treefp.WorkingTree only for a full run. A full run is either a
                 selector-free test_command run (including `--files` in a lane-less repo, per bare_test_run) or a
                 lane run where every declared lane ran unscoped and green. Take the fingerprint before and after;
                 a mismatch records nothing. A red full run on the recorded tree clears the record. Runs that never
                 happened (exit 3–8) leave it alone. A recorder failure warns and never changes the exit status.
       covers:   c-4
       depends:  t-2, t-4
       contract: A green bare `dross test` writes a tree equal to treefp.Candidate() after `git add -A`. If the
                   recorder and the commit side disagree, TestGreenMatchesCandidate fails.
                 `dross test ./internal/x`, a red run, a lane run matching 1 of 2 declared lanes, and a lane line
                   carrying a derived selector each record nothing.
                 In a lane-less repo, a green `dross test --files a.go` records (this is execute.md's form).
                 A file edited mid-run through the spawn seam → nothing recorded.
                 Green then red on the same tree → record cleared. Exit 3 or 4 with an existing record → record intact.
                 A green through the remote spawn seam records the same fingerprint as a local one.
                 A forced fingerprint failure → exit 0, output unchanged except one stderr warning.

  t-10 Gate git commit on a recorded green tree  [risk+verification+mvp]
       files:    internal/gate/commit.go, internal/gate/commit_test.go
       desc:     commit-green claims Bash `git [-C d] [-c k=v] commit` in a .dross repo, including env-prefixed,
                 chained and cd-tracked forms. The candidate is the real index plus replayed earlier `git add`
                 segments (plus -a). A .dross-only or empty candidate passes; otherwise the candidate must equal the
                 green.json tree. Refuses pathspec/--only/--include/-p commits, `git add -p`, and chains where
                 anything other than `git add`/`cd` comes before the commit. Silent in a repo with neither
                 test_command nor lanes. (disagreements 4, 5, 6)
       covers:   c-4
       depends:  t-1, t-2, t-3, t-4
       contract: With a green for tree T, `git commit -m x` with candidate T is allowed. Change one byte after the
                   green → refused, naming a full `dross test` and stating that a raw `go test` does not count.
                 `git add a.go && git commit -m x` is judged against index+a.go (allowed when it equals T), not the
                   pre-add index.
                 `sed -i s/a/b/ a.go && git add a.go && git commit -m x` → refused (unpredictable predecessor).
                 Only .dross/phases/p/plan.toml staged, no green → allowed. `git commit -am x` with only tracked
                   .dross edits → allowed. A staged code file with green.json absent (a raw go test leaves none) → refused.
                 `git -C ../other commit` and `cd ../other && git commit` judge ../other. If ../other has no .dross
                   → allowed without spawning git.
                 `git commit a.go -m x`, `git commit -p` and `git add -p && git commit` → refused with
                   "stage with git add, then run a plain git commit".
                 `git commit-tree`, `echo git commit` and `git log --grep commit` are not claimed. `ls -la` spawns
                   zero git argv.
                 A repo with neither runtime.test_command nor a test lane → allowed silently.
                 A git failure building the candidate, a corrupt index, or an unreadable green.json → refused
                   naming the error.

  t-11 Pair-mode approval recorder and edit gate  [risk+verification+mvp]
       files:    internal/gate/pair.go, internal/gate/pair_test.go,
                 internal/gate/testdata/askuserquestion_post.json
       desc:     Recorder: capture a real PostToolUse AskUserQuestion payload as the fixture first. Record
                 {phase, task, HEAD} only when a tool_response answer exactly equals ApproveLabel(the in_progress
                 task). Gate: armed when current_phase's plan has an in_progress task, the phase's recorded mode
                 isn't solo (absent = pair), and HEAD is on phase/<id>. When armed, it refuses in-repo
                 Edit/Write/MultiEdit/NotebookEdit outside .dross/ unless approval == (phase, task, current HEAD).
                 It also refuses Bash `dross execute begin … --solo`. (disagreements 2, 9, 11)
       covers:   c-5
       depends:  t-1, t-3, t-4
       contract: Fixture answer "approve t-3" with t-3 in_progress → recorded. "approve t-2", "steer", "reject",
                   "Approve t-3", "approve t-3 ", "yes, approve t-3" and multi-select "approve t-3, steer" record
                   nothing (pair_approval_signal).
                 Answers present only in tool_input → nothing recorded. A tool_response with no readable answers →
                   warning, nothing recorded.
                 Armed with no approval → an Edit of internal/x.go is refused naming "approve t-3" and
                   AskUserQuestion. An Edit of .dross/handoff.md and a Write outside the repo are allowed.
                 Approval at HEAD A → allowed. After a new commit → refused. An approval for t-1 while t-2 is
                   in_progress → refused.
                 Recorded mode solo, no in_progress task, or HEAD on a branch other than phase/<id> → silent.
                 An unreadable state.json, plan.toml or execute.json while a task is in_progress → refused naming
                   the error. Two in_progress tasks → refused naming the ambiguity.
                 Bash `dross execute begin p --solo` → refused while armed, allowed when nothing is in_progress.

  t-12 Add dross gate check/record hook verbs  [risk+verification+mvp]
       files:    internal/cmd/gate.go, internal/cmd/gate_test.go, internal/cmd/telemetry.go,
                 cmd/dross/main.go, cmd/dross/testdata/cli_tree.txt (golden)
       desc:     `dross gate check` (PreToolUse) reads InOrStdin and runs gate.Check. A refusal returns
                 ExitCodeError{Code:2} with the text on stderr exactly once (SilenceErrors on the verb). An allowed
                 call exits 0 with no output. `dross gate record` (PostToolUse) runs the recorders and never blocks.
                 Exports the hook-command constants that wiring consumes. RecordCLIEvent skips both verbs.
       covers:   c-2
       depends:  t-3
       contract: A payload refused by a test-registered gate → ExitCode(err)==2. A plain error would exit 1, which
                   Claude Code treats as non-blocking (fail-open). Rule + remedy appear on stderr exactly once;
                   stdout is empty.
                 An allowed payload → nil error, zero bytes on stdout AND stderr.
                 Garbage stdin → nil error, empty stdout, one warning line on stderr.
                 `gate record` returns nil for every payload in a table, including a recorder error (exit 0 +
                   stderr warning).
                 With telemetry enabled, three `gate check` runs append zero lines to telemetry.jsonl while
                   `dross status` still appends. Refusal text can carry a secret path into err_detail.
                 The hook constants equal "dross " + the verb paths. cli_tree.txt lists `gate check` and
                   `gate record`. Removing cmd.Gate() from main.go fails TestCLITreeGolden.

  t-13 Add dross execute begin mode marker  [risk]
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt (golden)
       desc:     `dross execute begin <phase> [--solo]` checks the phase exists with a plan.toml, then writes
                 execute.json {phase, mode, at} and prints the mode it recorded. A same-mode re-run leaves the
                 record untouched. t-11's gate refuses a mid-task solo downgrade arriving through Bash.
                 (mode verb: disagreement 2)
       covers:   c-5
       depends:  t-4
       contract: `begin p` → mode=pair. `begin p --solo` → mode=solo. A same-mode re-run leaves execute.json
                   byte-identical.
                 An unknown phase or a missing plan.toml → error, no file written.
                 begin leaves state.json's sha256 unchanged (no generic writer, no clobber path).
                 cli_tree.txt lists `execute begin` with `--solo`.

  t-14 Override store and record tamper guard  [risk+verification]
       files:    internal/gate/override.go, internal/gate/override_test.go
       desc:     Override store at ~/.claude/dross/gate-overrides.json holding {name, until}. Writes are atomic and
                 it plugs into the engine's Overrides. Expired entries are ignored; a corrupt file lifts nothing.
                 tamper-guard (always-on, Liftable=false) refuses Edit/Write/MultiEdit of <repo>/.dross/gate/** and
                 of the overrides file. Split from risk t-12, following verification's guard/verb split.
                 (disagreements 7, 10)
       covers:   c-2
       depends:  t-3, t-4
       contract: An override for commit-green until T → that gate is skipped before T and enforced at T+1s
                   (injected clock). secret-read stays on.
                 A corrupt overrides file → no gate lifted, and the error is surfaced for `status`.
                 A Write to <repo>/.dross/gate/green.json, approval.json or the overrides file is refused (forged
                   green, approval or override), even with every liftable gate lifted. Reads of them are allowed.
                 A malformed gates.toml leaves the overrides store readable, because it is a separate file.

Wave 3
  t-15 Human gate off/on/status verbs  [risk+verification+mvp]
       files:    internal/cmd/gate.go, internal/cmd/gate_override_test.go, cmd/dross/gate_remedy_test.go,
                 README.md, cmd/dross/testdata/cli_tree.txt (golden)
       desc:     `dross gate off <name> [--for 1h]` with a 24h cap, `gate on <name>`, and `gate status`, which shows
                 every registered gate, on/off, expiry and any overrides-file parse error. Adds a README row.
                 t-7's guard refuses Bash `gate off`, so only the human's own terminal reaches it. Adds the
                 cli_tree.txt regen that risk t-12 left out.
       covers:   c-2
       depends:  t-7, t-8, t-10, t-11, t-12, t-14
       contract: A direct `gate off secret-read --for 30m` → `gate check` on a Read-.env payload exits 0. With the
                   clock at +31m → exit 2.
                 `gate on secret-read` → the next check refuses. `--for 0`, a negative value, or `--for 25h` →
                   error naming the cap, store unchanged.
                 An unknown name → error listing valid names. `gate off gate-off-guard` and `gate off tamper-guard`
                   → refused (Liftable=false).
                 `gate status` lists every registered gate. A corrupt overrides file → status reports the parse error.
                 gate.All() names == {secret-stream, secret-read, gate-off-guard, tamper-guard, plan-edit,
                   curated-shrink, commit-green, pair-approval}. A dropped registration fails.
                 TestGateRemediesResolve: every `dross <verb> <sub>` in any gate Remedy or hint resolves in newRoot().
                   Renaming `task edit` or `gate off` fails it. TestNarratedCommandsResolveAgainstTheTree scans only
                   internal/cmd, so nothing else checks remedies in internal/gate.

  t-16 Align execute.md with the pair and commit gates  [risk+verification+mvp]
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go,
                 internal/cmd/prompt_commit_gate_test.go, docs/interaction-audit.md
       desc:     Pre-flight runs `dross execute begin <id>` (plus `--solo` when passed) before the per-task loop.
                 §1c and the hard rule offer the literal `approve <task-id>` label in place of `proceed`. §1e/§1f
                 run a bare `dross test` before `git commit` when lanes are declared. Gate refusals are hard stops.
                 `dross gate off` is the human's escape, run in their own terminal. The interaction-audit execute
                 row is updated [mvp]. Risk's cross-prompt commit audit lands here because it has to see execute.md
                 after the change.
       covers:   c-5, c-4
       depends:  t-11, t-13
       contract: TestExecutePromptOffersApproveLabel: §1c lists the label in gate.ApproveLabel's exact format with
                   no bare `proceed` option, and steer/show me/skip are still listed. Reverting the label or drifting
                   from the constant fails it.
                 `dross execute begin`, including the --solo branch, appears before "## 1. Per-task loop".
                 §1f has a bare `dross test` before `git commit` when `[[runtime.test_lane]]` is declared.
                 TestPromptCommitsSatisfyGreenGate: every fenced block with `git commit` whose `git add` names a
                   non-.dross path has a bare `dross test` earlier in the same section. Deleting ship.md's step
                   (t-5) fails it. quick.md passes unchanged. .dross-only commits (verify.md, review.md,
                   execute §2) are exempt.
                 t-5's TestNoPromptTellsAgentToLiftGate stays green.

  t-17 Wire gate hooks into user settings.json  [risk+verification+mvp]
       files:    internal/hooks/settings.go, internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/init_test.go
       desc:     Add EventPreToolUse and EventPostToolUse. ensureUserHooks wires matcher-less groups for t-12's
                 `dross gate check` / `dross gate record` constants beside PreCompact/SessionStart, through the
                 unchanged MergeHook. The write is temp+rename and keeps the existing mode (0600 when new).
                 `hooks ensure` names all four. Numbered last in wave 3 so pair execution arms nothing on this repo
                 before the override verbs and execute.md exist. (disagreement 3)
       covers:   c-1
       depends:  t-12
       contract: ensure on an empty config writes four dross groups. A second run is byte-identical with mtime
                   unchanged (no write).
                 A foreign PreToolUse group {"matcher":"Bash", …/usr/local/bin/lint.sh…} and unrelated keys survive
                   verbatim and in order, with dross's group appended after them.
                 A pre-existing 2-hook install gains exactly the two gate groups. hooks.PreToolUse as an object →
                   error, file bytes unchanged.
                 A 0600 settings.json stays 0600. A failed write leaves the original bytes intact.
                 TestInitEnsuresHooksIdempotent: two inits leave exactly one entry per event for all four.
                   TestOnboardEnsuresSameHooks's byte-parity then covers onboard with no edit. Dropping the gate
                   pair fails both.

Wave 4
  t-18 Report hook wiring in dross doctor  [risk+verification+mvp]
       files:    internal/cmd/doctor.go, internal/cmd/doctor_hooks_test.go, internal/cmd/cmd_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt (golden)
       desc:     New Hooks section reading userSettingsPath() (CLAUDE_CONFIG_DIR honoured), one ✓/✗ line per
                 event. Presence = MergeHook(b, ev, cmd) returns b unchanged [mvp]. The matcher and
                 disableAllHooks checks go through hooks.ReadSettings, since internal/cmd can't import
                 encoding/json. chdir() seeds the four hooks so existing doctor tests stay green.
                 These conditions are an issue (non-zero exit):
                   - a gate hook is absent
                   - a gate command sits only under a matcher-restricted group
                   - disableAllHooks is true
                   - the file can't be parsed
                 A missing PreCompact or SessionStart is a ⚠ warning.
       covers:   c-9
       depends:  t-17
       contract: Empty CLAUDE_CONFIG_DIR → non-zero exit, ✗ PreToolUse naming `dross gate check` and
                   `dross hooks ensure`, ✗ PostToolUse naming `dross gate record`. After `dross hooks ensure` →
                   exit 0 and ✓ for all four.
                 Only PreCompact (or only SessionStart) missing → ⚠, exit code unaffected.
                 A gate command only under matcher "Bash" → issue naming the matcher. `"disableAllHooks": true` → issue.
                 A malformed settings.json → issue naming the parse error (no crash, no pass).
                 A fully wired $HOME/.claude/settings.json with CLAUDE_CONFIG_DIR pointing at an empty dir → still
                   fails, because doctor reads the effective file.
                 doctor_run.txt pins the section. The ~105 existing Doctor() invocations across 15 test files
                   still pass with chdir's seeded hooks, including the four files that never run Init().
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-17 |
| c-2 | t-3, t-7, t-12, t-14, t-15 |
| c-3 | t-8 |
| c-4 | t-1, t-2, t-4, t-5, t-9, t-10, t-16 |
| c-5 | t-4, t-11, t-13, t-16 |
| c-6 | t-1, t-7 |
| c-7 | t-1, t-7 |
| c-8 | t-6, t-8 |
| c-9 | t-18 |

How each locked decision is pinned:

- **hook_scope:** t-17
- **guard_scope:** t-3 (fake workflow gate not invoked with no root), t-7 (fires with no .dross ancestor)
- **gate_override:** t-7 (gate-off-guard), t-14 (store and tamper guard), t-15 (human verbs)
- **pair_approval_signal:** t-11 (exact label only), t-16 (label offered)
- **gate_error_posture:** t-3 (both sides), plus a "cannot judge → refused naming the error" contract in t-8, t-10 and t-11
- **plan_overwrite:** t-8
- **shell_detection_depth:** t-1, t-10
- **green_run_definition:** t-9
- **gate_list_source:** t-3 and t-7 (defaults plus additive gates.toml)

All 9 criteria are covered.

## Disagreements

1. **Where gate state lives**
   - What diverged: the location of the green, approval and execute-mode records.
   - risk: a new self-ignoring `.dross/gate/` directory with atomic single-writer JSON files.
   - mvp: fields in `.dross/state.json`.
   - verification: typed records in `.dross/local.toml` via localstore. This draft is the only one that inherits `RefuseTrackedLocal`.
   - Provisional default: risk's `.dross/gate/`. Verification's tracked-file refusal is grafted onto t-4.
   - Why: both existing stores truncate in place, so neither is atomic: `localstore.Save` uses `os.Create` and `state.Save` uses `os.WriteFile`. Other verbs also write both files (consent grants and detached runs in local.toml; `state touch` and task status in state.json). A hook that reads on every tool call can catch a half-written file. Under gate_error_posture that becomes a random closed refusal. A recorder writing at the same moment can also overwrite another verb's update.
   - On the facts: MVP is right that `state set` is a closed switch today and risk's "generic writer" claim is wrong. But MVP's own t-8 adds `execute_mode` to that switch, which creates exactly the agent-writable forge path risk warned about.
   - This choice decides t-4's shape, every reader in t-9/t-10/t-11/t-13, and whether t-14's tamper guard has one directory to protect.

2. **How the pair gate arms, and who records the mode**
   - risk: opt-in. The gate is live only when `dross execute begin` has written execute.json with mode=pair, the phase is current_phase, HEAD is on the phase branch and tasks remain unfinished. An absent marker means the gate is silent.
   - mvp: default-armed whenever an in_progress task exists and ExecuteMode ≠ solo. The mode is set with an agent-runnable `dross state set execute_mode`, which nothing refuses.
   - verification: default-armed the same way. The mode is set with `dross gate mode`, and a mid-task solo flip arriving through Bash is refused.
   - Provisional default: default-armed (mvp+verification), absent marker = pair, mid-task solo flip refused (risk+verification). Risk's phase-branch condition is kept. The marker verb is risk's `dross execute begin`; MVP's `state set` path is rejected.
   - Why: opt-in fails open whenever execute.md skips `begin`, which is the 2026-05-29 shape. Default-armed blocks Claude's edits whenever a task is left in_progress. The branch condition limits that, for example during a `/dross-quick` on main in the middle of a phase. MVP's mode setter is an agent-runnable override of c-5, which gate_override forbids.
   - This changes t-11's arming logic, t-13's purpose and t-16's pre-flight.

3. **When hook wiring and the doctor check land**
   - risk: wiring goes last (wave 4, depending on 9 tasks) and doctor after it (wave 5), because `dross hooks ensure` on this repo would arm a commit gate nothing can satisfy.
   - mvp and verification: wiring in wave 1, doctor in wave 2.
   - Provisional default: data-driven placement. Wiring depends on t-12's hook-command constants (wave 3) and is numbered last in its wave. Doctor depends on wiring (wave 4). The remaining safety ordering against t-15/t-16 is by id, not a dependency, because the wave rule forbids non-data dependencies.
   - Why: once doctor fails on absent gate hooks, `make doctor` (r-01) goes red. The natural fix is `dross hooks ensure`, which arms whatever gates already exist. If commit-green is armed before t-9's recorder or t-15's override exists, every remaining commit in the phase is blocked.

4. **How the commit gate builds the candidate tree**
   - mvp: reads the live index. It refuses `-a` and pathspec commits as unjudgeable, judges `make && git commit` against the current index, and adds no gitrun env seam.
   - risk and verification: replay earlier `git add` segments, plus -a, onto a temp-index copy via `gitrun.Options.IndexFile`.
   - Provisional default: replay (t-2 + t-10).
   - Why: Claude routinely sends `git add <files> && git commit …` as one Bash call. PreToolUse sees the pre-add index, so MVP's gate refuses that common honest shape, or allows it when a stale index happens to match. This choice decides whether t-2 exists at all.

5. **Commit gate in a repo with no test command and no lanes**
   - verification: commit-green stays silent there.
   - risk: rejects carve-outs explicitly, though only for root and doc commits; it never considers repos without tests.
   - mvp: does not address it.
   - Provisional default: include the carve-out.
   - Why: `dross test` cannot record a green in such a repo, so without the carve-out commit-green refuses every agent commit there forever. execute.md already supports that case ("none — verify will catch this later"), and a greenfield `init.md` first commit hits it.
   - The catch: this reads c-4 ("refused unless `dross test` recorded a green") as applying only where a green can exist. The user should confirm that reading. If it is rejected, drop that contract from t-10 and make t-5's init.md step unconditional.

6. **What may precede `git commit` in a chained command**
   - risk: refuses any chain where something other than `git add`/`cd` comes before the commit (`gofmt -w . && git add -A && git commit`, `make && git commit`).
   - verification: does not say.
   - mvp: detects `make && git commit` and judges it against the index as it stands when the hook runs.
   - Provisional default: risk's refusal.
   - Why: a formatter or `sed -i` before the commit changes what actually gets committed after the gate projected the candidate. That is a false allow of untested bytes. The cost is that the agent has to split such chains into separate calls.

7. **Extending the lists, broken extension files, and where overrides are stored**
   - mvp: no extension mechanism (built-in lists only). Overrides go in `GlobalDir()/gates.toml`.
   - risk: additive `~/.claude/dross/gates.toml`. If it is malformed, the defaults are still enforced plus a warning. Overrides live in a separate `gate-overrides.json`.
   - verification: one gates.toml holds both overrides and lists. If it is malformed, the list-extensible gates refuse their in-domain calls, and `gate off` refuses to overwrite the file.
   - Provisional default: include the additive extension (2 of 3). A malformed file is handled closed (verification, the literal reading of gate_error_posture). Overrides go in risk's separate file, so a broken extension file does not also disable the human escape.
   - Why: the closed posture blocks every Read, every reader Bash command and every stderr merge, in every repo, until the human fixes the file. The open posture silently drops user-added patterns; its warning goes to stderr on an exit-0 hook, which Claude never sees.

8. **Prompt flows that the new gates would break**
   - risk: has both prompt tasks — pause/resume moved to Edit, and ship/init running `dross test`, in wave 4 with a cross-prompt audit test.
   - verification: has ship/init only, in wave 1.
   - mvp: has neither.
   - Provisional default: include both. Both move to wave 1, since neither needs gate code. Risk's audit test moves into t-16, because it has to see execute.md after the change.
   - Why: resume.md:44 rewrites handoff.md with "only what's left", so c-8 refuses the routine prune. init.md:123 and ship.md:101 commit code-path files without a green, so c-4 refuses them. Leaving these out ships gates that break dross's own slash commands.

9. **Which AskUserQuestion field counts as the answer**
   - verification: reads only `tool_response`. `tool_input` is agent-authored and ignored; an unknown shape gives a warning and records nothing.
   - mvp: accepts answers from either `tool_response` or `tool_input`.
   - risk: a fixture captured from a real PostToolUse payload, without naming the field.
   - Provisional default: tool_response only, with risk's real captured fixture as t-11's first step.
   - Why: if Claude Code puts the answers only in `tool_input`, a tool_response-only recorder never fires and the pair gate blocks every edit. If `tool_input` is accepted, an agent-authored field can unlock edits. The captured fixture settles which case applies. If it shows tool_input-only, stop and re-decide before building on it.

10. **Whether to guard the record files against agent writes**
    - risk: refuses Edit/Write of `.dross/gate/**` and the overrides file.
    - mvp and verification: no such guard.
    - Provisional default: include it, in t-14.
    - Why: without it, a single Write forges a green, an approval or an override. That makes the gates advisory through a file instead of through the `gate off` command that gate_override already blocks. No legitimate flow writes those paths.

11. **How long an approval stays valid**
    - mvp and verification: the approval must match the current HEAD exactly.
    - risk: the approval's HEAD only needs to be an ancestor of HEAD, with nothing but .dross/ paths changed since.
    - Provisional default: exact HEAD.
    - Why: risk's version lets a .dross-only bookkeeping commit made between approval and edit keep the approval. Exact match is simpler and fails closed. execute.md commits bookkeeping only at §2, so exact HEAD doesn't trip the normal per-task loop. It would revoke approval after an out-of-band board-sync commit.

12. **How deep the shell scanner reads**
    - risk: recurses into `$(…)`/backticks, skips heredoc bodies, and marks an unbalanced quote as Partial (lenient).
    - verification: treats `$(…)` as opaque and returns an error on an unterminated quote.
    - Provisional default: risk's.
    - Why: it decides whether `X=$(cat .env)` or `echo $(cat .env)` is refused under c-7. shell_detection_depth names eval and `sh -c` as out of scope but says nothing about command substitution. Both drafts keep the heredoc commit message from being read as commands.
