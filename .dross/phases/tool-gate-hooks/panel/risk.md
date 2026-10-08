# tool-gate-hooks — risk lens draft

Lens: the graph is shaped by what can break. Every failure mode I could name is
owned by exactly one task, and that task's test_contract pins it. Three
process-level facts drive most of the shape:

- **Claude Code only blocks on exit 2.** Cobra's default error exit is 1, and
  Claude Code treats 1 as a non-blocking error, so every in-domain refusal would
  quietly fail open. On the other side, an uncaught Go panic exits 2, which
  Claude Code reads as a block with a stack trace attached, so one nil-deref
  would block every tool call.
- **The hook runs on every tool call in every repo.** The quiet path has to
  print nothing, spawn no git and write no telemetry.
- **This repo dogfoods it.** If wiring lands before the gates, the recorder and
  the override exist, `dross hooks ensure` arms a commit gate that nothing can
  satisfy. That blocks every remaining commit in this phase.

```
Phase tool-gate-hooks — 17 tasks across 5 waves

Wave 1
  t-1  Add lenient shell command scanner package
       files:    internal/shellscan/scan.go, internal/shellscan/scan_test.go
       desc:     Quote/escape-aware lexer splitting a Bash command into simple commands: operators
                 (&&, ||, ;, |, |&, &, newline), redirections with fd attribution, env-var prefixes,
                 wrapper stripping (env/command/sudo/nohup/time), `cd` dir tracking, `git -C`,
                 recursion into $(…)/backticks, heredoc bodies skipped, group/subshell redirects
                 propagated inward. Unbalanced quote → Partial=true, never a panic.
       covers:   c-4, c-6, c-7
       contract: `git commit -m "$(cat <<'EOF'\nstop cat .env 2>&1\nEOF\n)"` yields one git-commit
                   command plus a nested `cat` with NO file operand — a heredoc body never becomes
                   a command (else every commit message mentioning .env is refused)
                 `echo "2>&1"` carries no stderr-merge redirect; `pass-cli x |& head` attributes
                   the merge to pass-cli, not head; `( pass-cli view ) 2>&1` propagates it inward
                 `FOO=1 /opt/bin/pass-cli item view x` → argv0 basename `pass-cli`
                 `cd sub && git -C ../other commit` resolves the commit's dir to <cwd>/other
                 an unbalanced quote returns Partial=true; a fuzz seed corpus never panics

  t-2  Fingerprint trees on a temporary git index
       files:    internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go,
                 internal/treefp/treefp.go, internal/treefp/treefp_test.go
       desc:     Add Options.IndexFile (GIT_INDEX_FILE) to gitrun. treefp returns .dross/-excluded
                 tree hashes for the working tree (tracked + untracked-unignored) and for a commit
                 candidate (copy of the real index, replayed `git add` pathspecs, optional -a). It
                 also reports whether the staged changes vs HEAD are all under .dross/. Handles an
                 unborn HEAD and linked worktrees (`rev-parse --git-path index`); every git call
                 has a timeout.
       covers:   c-4
       contract: the REAL index is never touched: with a partially staged repo, the index file's
                   sha256 and `git diff --cached --name-only` are identical before/after
                   WorkingTree() and Candidate() (an empty IndexFile must not fall through to it)
                 WorkingTree() == Candidate() after `git add -A`; they differ when one modified
                   file is left unstaged; editing .dross/state.json changes neither
                 an untracked unignored file changes WorkingTree(); an ignored one does not
                 unborn HEAD → Candidate/DrossOnly diff against the empty tree without error;
                   a linked worktree resolves its own index
                 a git failure returns an error, never "" (an empty tree string could equal an
                   empty record); TestNoUnseparatedGitPositional stays green (pathspecs after --)

  t-3  Gate engine: payload decode, verdicts, dispatch
       files:    internal/gate/engine.go, internal/gate/payload.go, internal/gate/lists.go,
                 internal/gate/engine_test.go
       desc:     Decode the hook payload (tool_name, tool_input, tool_response, cwd, session_id).
                 Gate (Name/Claims/Judge) and Recorder interfaces register at init time. Check runs
                 every non-lifted gate (Overrides interface) with per-gate panic recovery and joins
                 all refusals; Refusal requires a non-empty Rule and Remedy. Default lists (secret
                 tools, secret path globs, curated .dross files) are unioned with an additive
                 ~/.claude/dross/gates.toml.
       covers:   c-2
       contract: unparseable payload (`{`, empty stdin, missing tool_name) → Allow + one warning;
                   no gate's Judge is called (gate_error_posture)
                 a claiming gate whose Judge panics → refused naming the gate + "internal error";
                   a Judge error → refused with the error text (closed in-domain)
                 NewRefusal with an empty Remedy or Rule returns an error (no rule-less blocks)
                 malformed gates.toml → defaults still enforced (pass-cli in SecretTools) + warning;
                   `secret_tools = []` cannot remove a default
                 a lifted gate is skipped while the others still run; two refusing gates → both
                   rules in one message

  t-4  Add machine-local gate state store
       files:    internal/gatestate/store.go, internal/gatestate/store_test.go
       desc:     `.dross/gate/` with a self-ignoring `.gitignore` (`*`) and single-writer JSON
                 records: green.json {tree, at, runner}, execute.json {phase, mode, at},
                 approval.json {phase, task, head, at}. Writes are atomic (unique temp + rename).
                 A missing file means no record; a corrupt file is an error naming the path.
       covers:   c-4, c-5
       contract: after the first write, `git status --porcelain` shows nothing under .dross/gate/
                   and `git add .dross/` stages nothing from it — with NO root .gitignore entry
                   (repos onboarded before this phase are covered)
                 concurrent writers/readers (N goroutines) never observe a partial record: every
                   read either is not-exist or decodes
                 a truncated green.json → error naming .dross/gate/green.json, not a zero record;
                   a missing one → (nil, nil)
                 writing a green with an empty tree string is rejected

Wave 2
  t-5  Secret guards: stream merges and secret reads   (depends t-1, t-3)
       files:    internal/gate/secrets.go, internal/gate/secrets_test.go
       desc:     Two gates that fire in every repo. secret-stream (c-6): refuse a simple command
                 whose tool is in SecretTools when its stderr is merged into stdout (2>&1, &>, &>>,
                 >&file, |&, or a group merge). secret-read (c-7): refuse a Read of file_path, or
                 cat/head/tail/less/sed with an operand or `<` input, whose basename matches
                 SecretPaths. `source` / `.` are never claimed.
       covers:   c-6, c-7
       contract: `pass-cli item view x 2>&1 | head` refused naming secret-stream;
                   `make build 2>&1` allowed silently; `pass-cli item view x >f 2>/dev/null` allowed
                 `cat .env`, `head -n 3 config/prod.env`, `sed -n 1p ~/.ssh/id_ed25519`,
                   `cat < .env`, Read of /x/server.pem refused; `cat ~/.ssh/id_ed25519.pub`,
                   `source .env && make`, `. ./.env` allowed
                 `keys.txt` under …/sops/age/ refused; a plain notes.txt is not
                 both gates refuse from a cwd with no .dross ancestor (guard_scope) and spawn no
                   git (gitrun.ArgvRecorder empty)
                 gates.toml adding `op` to secret_tools makes `op read x 2>&1` refused

  t-6  Gate plan.toml edits and curated-file shrinks   (depends t-3)
       files:    internal/gate/drossfiles.go, internal/gate/drossfiles_test.go
       desc:     plan-edit (c-3): refuse Edit/MultiEdit of an existing .dross/phases/*/plan.toml
                 (remedy: `dross task add/edit/move/remove`). Write that creates the file is
                 allowed; Write over it is allowed only while every task is "" or pending.
                 curated-shrink (c-8): refuse a Write that replaces an existing curated file with
                 less than half its current bytes (remedy: Edit). Both stay silent when no
                 .dross/project.toml is an ancestor.
       covers:   c-3, c-8
       contract: Edit on an existing plan.toml refused naming `dross task edit`; Write of a new
                   plan.toml allowed; Write over an all-pending plan allowed, over one with an
                   in_progress task refused (plan_overwrite)
                 unparseable existing plan.toml → Write refused naming the decode error
                 1000-byte spec.toml: Write of 499 bytes refused, 500 allowed (boundary), 2000
                   allowed; Edit of any size allowed; a 1-byte Write to non-curated
                   .dross/phases/x/notes.md allowed
                 `.dross/phases/x/../x/plan.toml` is normalised and caught; the same layout with
                   no project.toml ancestor is allowed
                 an unreadable curated file (chmod 000) → refused naming the read error

  t-7  Record full green runs from dross test   (depends t-2, t-4)
       files:    internal/cmd/test.go, internal/cmd/test_green_test.go
       desc:     Write green.json with treefp.WorkingTree only for a full run: either the
                 whole-suite path with no selector (including `--files` in a lane-less repo), or a
                 lane run where every declared lane spawned unscoped and passed with no miss. The
                 fingerprint is taken before and after; a mismatch records nothing. A red full run
                 on the recorded tree clears it. A recorder failure warns and never changes exit status.
       covers:   c-4
       contract: a green bare `dross test` writes a tree equal to treefp.Candidate() after
                   `git add -A` (recorder and commit side cannot disagree)
                 `dross test ./internal/x`, a red run, exit 3/4/5/6/7/8 runs, and a lane run
                   matching 1 of 2 declared lanes each leave green.json absent
                 lane-less repo: `dross test --files a.go` records green (it ran test_command)
                 a file edited mid-run through the spawn seam → nothing recorded
                 a forced fingerprint failure → exit code still 0, output unchanged except one
                   stderr warning
                 a red full run on the same tree as the existing green removes green.json

  t-8  Add dross gate check/record hook verbs   (depends t-3)
       files:    internal/cmd/gate.go, internal/cmd/gate_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross gate check` (PreToolUse) reads stdin, runs gate.Check, and either exits 2
                 with the refusal on stderr or exits 0 silently. `dross gate record` (PostToolUse)
                 runs the recorders and never blocks. Both export hook-command constants for wiring,
                 skip telemetry, and keep exit 2 intact through main's exit path.
       covers:   c-2
       contract: built binary fed a refusing payload exits 2 (not 1 — cobra's default would fail
                   open) with rule + remedy on stderr; an allowed payload exits 0 with zero bytes on
                   stdout AND stderr
                 garbage stdin → exit 0, empty stdout, one warning line on stderr
                 telemetry enabled: three `dross gate check` runs append zero lines to
                   telemetry.jsonl (default-ON telemetry would log every tool call)
                 a recorder error under `dross gate record` → exit 0 + stderr warning
                 cli_tree.txt golden lists `gate check` and `gate record`

  t-9  Gate git commit on a recorded green tree   (depends t-1, t-2, t-3, t-4)
       files:    internal/gate/commit.go, internal/gate/commit_test.go
       desc:     commit-green claims Bash git-commit segments in a .dross repo (env prefix, -C,
                 -c k=v, chained). It builds the candidate by replaying earlier `git add` segments
                 and honouring -a, passes .dross-only or empty candidates, and otherwise requires
                 candidate == green.json tree. It refuses pathspec/--only/--include/-p commits, and
                 chains where any command other than `git add` or `cd` precedes the commit.
       covers:   c-4
       contract: green for tree T; `git commit -m x` with candidate T allowed; one byte changed
                   after the green → refused naming a full `dross test`
                 `git add a.go && git commit -m x` allowed when the replayed candidate == T;
                   `sed -i s/a/b/ a.go && git add a.go && git commit -m x` refused (unpredictable)
                 staging only .dross/phases/p/plan.toml with no green → allowed; a code file
                   staged with green.json absent (a raw `go test` leaves none) → refused
                 `git -C ../other commit` judges the other repo; `git commit-tree`, `echo git
                   commit`, `git log --grep commit` are not claimed; `ls -la` spawns zero git argv
                 `git commit -am x` includes tracked worktree edits; `git commit a.go -m x`
                   refused with "stage, then plain git commit"
                 git failure building the candidate → refused naming the error; a repo with no
                   .dross → allowed without spawning git

  t-10 Pair-mode approval recorder and edit gate   (depends t-1, t-3, t-4)
       files:    internal/gate/pair.go, internal/gate/pair_test.go,
                 internal/gate/testdata/askuserquestion_post.json
       desc:     Recorder: an AskUserQuestion answer exactly equal to ApproveLabel(the in_progress
                 task) records {phase, task, HEAD}; anything else records nothing. The fixture is
                 captured from a real PostToolUse payload. Gate: in a live pair session (mode=pair,
                 phase == current_phase, on the phase branch, unfinished tasks) refuse in-repo
                 Edit/Write/MultiEdit/NotebookEdit outside .dross/ without a valid approval, and
                 refuse Bash `dross execute begin … --solo`.
       covers:   c-5
       contract: fixture answer "approve t-3" with t-3 in_progress → recorded; "approve t-2",
                   "steer", "Approve t-3", freeform "yes go", multi-select "approve t-3, steer"
                   record nothing (pair_approval_signal)
                 live pair session, no approval → Edit of internal/x.go refused naming
                   "approve t-3"; Edit of .dross/handoff.md and a Write outside the repo allowed
                 approval at HEAD A, then a .dross-only commit → edits still allowed; a commit
                   touching code → refused; after `git reset --hard A~1` → refused
                 mode=solo, or a branch other than the phase branch → silent and spawns no git
                 unreadable state.json/plan.toml in a live session → refused naming the error;
                   two in_progress tasks → refused naming the ambiguity
                 Bash `dross execute begin p --solo` refused during a live pair session, allowed
                   with no session

  t-11 Add dross execute begin session marker   (depends t-4)
       files:    internal/cmd/execute.go, internal/cmd/execute_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross execute begin <phase> [--solo]` checks the phase exists with a plan.toml,
                 then writes execute.json {phase, mode, at}. Re-running with the same phase and
                 mode leaves the record as it is. It prints the mode it recorded.
       covers:   c-5
       contract: `begin p` → mode=pair; `begin p --solo` → mode=solo; a same-mode re-run leaves
                   execute.json byte-identical
                 unknown phase or missing plan.toml → error, no file written
                 state.json sha256 unchanged by begin (no generic-writer / clobber path)
                 cli_tree.txt lists `execute begin` with `--solo`

Wave 3
  t-12 Human-only gate override and tamper guard   (depends t-1, t-3, t-8)
       files:    internal/gate/override.go, internal/gate/override_test.go, internal/cmd/gate.go,
                 internal/cmd/gate_override_test.go
       desc:     `dross gate off <name> --for <dur>` (expiry required, capped), `dross gate on`,
                 `dross gate status`, stored atomically in ~/.claude/dross/gate-overrides.json and
                 plugged into the engine's Overrides. An always-on guard that cannot be lifted
                 refuses Bash `dross gate off` in every repo, and Edit/Write/MultiEdit of
                 .dross/gate/** or the overrides file.
       covers:   c-2
       contract: after a direct `dross gate off commit-green --for 1h`, a commit with no green is
                   allowed; at expiry+1s (injected clock) it is refused again; secret-read stays on
                 Bash `dross gate off secret-read`, `FOO=1 dross gate off x`, and
                   `make && /usr/local/bin/dross gate off x` are refused even with every gate lifted;
                   Bash `dross gate status` allowed
                 Write to <repo>/.dross/gate/green.json or the overrides file refused (forged
                   green/override); Read of them allowed
                 unknown gate name → error listing valid names; `--for 0`, negative, or over-cap
                   → error, store unchanged
                 corrupt overrides file → no gate lifted; `status` reports the parse error

  t-13 Align execute.md with pair and commit gates   (depends t-7, t-10, t-11)
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       desc:     Pre-flight runs `dross execute begin <id>` (plus `--solo` when passed). The §1c
                 approval leads with the literal `approve <task-id>` label in place of `proceed`.
                 §1e/§1f run a full bare `dross test` before `git commit` when lanes are declared,
                 and the prompt names `dross gate off` as the human's escape.
       covers:   c-5, c-4
       contract: §1c offers the label in gate.ApproveLabel's exact format; if `proceed` returns
                   as the approval option or the label drifts from the gate constant,
                   TestExecutePromptOffersApproveLabel fails; steer/show me/skip still listed
                 the pre-flight contains `dross execute begin` before the per-task loop
                 §1f contains a bare `dross test` step before `git commit` for lanes repos

  t-14 Switch handoff pruning to Edit in prompts   (depends t-6)
       files:    assets/prompts/resume.md, assets/prompts/pause.md,
                 internal/cmd/resume_prompt_test.go, internal/cmd/pause_prompt_test.go
       desc:     resume.md prunes done items with Edit instead of rewriting handoff.md. pause.md
                 uses Write only to create handoff.md and Edit to replace an existing one. Without
                 this, c-8 refuses both routine flows.
       covers:   c-8
       contract: resume_prompt_test fails if the prune step goes back to rewriting
                   .dross/handoff.md wholesale instead of using Edit
                 pause_prompt_test fails if pause.md overwrites an existing handoff.md with Write

Wave 4
  t-15 Make prompt commit steps satisfy the green gate   (depends t-7, t-9, t-13)
       files:    assets/prompts/init.md, assets/prompts/ship.md,
                 internal/cmd/prompt_commit_gate_test.go
       desc:     Every prompt `git commit` that stages a path outside .dross/ comes after a full
                 bare `dross test`. That covers init's first commit and ship's ARCHITECTURE.md
                 commit. A new audit test enforces this across assets/prompts/*.md.
       covers:   c-4
       contract: TestPromptCommitsSatisfyGreenGate: for each fenced block with `git commit` whose
                   `git add` names a non-.dross path, a bare `dross test` step appears earlier in
                   the same section; deleting ship.md's new step fails it
                 quick.md's existing bare `dross test` and execute.md (post t-13) pass unchanged;
                   .dross-only commits (verify.md, review.md) are exempt

  t-16 Wire gate hooks into user settings.json   (depends t-5, t-6, t-7, t-8, t-9, t-10, t-12, t-13, t-14)
       files:    internal/hooks/settings.go, internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 internal/cmd/init_test.go
       desc:     Add EventPreToolUse/EventPostToolUse. ensureUserHooks wires matcher-less
                 `dross gate check` / `dross gate record` groups beside PreCompact/SessionStart via
                 MergeHook, with a temp+rename write that keeps the existing file mode (0600 when
                 new). `dross hooks ensure` names all four. This lands last so ensure never arms a
                 partial gate set on this repo.
       covers:   c-1
       contract: ensure on an empty config writes four dross groups; a second run is
                   byte-identical with mtime unchanged (no write)
                 a foreign PreToolUse group (matcher "Bash", custom command) plus unrelated keys
                   survive verbatim and in order, with dross's group appended after it
                 a pre-existing 2-hook install gains exactly the two gate groups; hooks.PreToolUse
                   as an object (not array) → error, file bytes unchanged
                 a 0600 settings.json stays 0600; a failed write leaves the original bytes intact
                 init_test: two inits leave exactly one entry for each of the four events

Wave 5
  t-17 Report hook wiring in dross doctor   (depends t-16)
       files:    internal/cmd/doctor.go, internal/cmd/doctor_hooks_test.go, internal/cmd/cmd_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       desc:     New Hooks section: each dross hook reported wired/absent in the effective
                 settings.json (CLAUDE_CONFIG_DIR honoured). An issue (non-zero exit) for an absent
                 gate hook, a gate command only under a matcher-restricted group,
                 disableAllHooks:true, or an unparseable file. A warning for missing
                 PreCompact/SessionStart. chdir seeds the four hooks so existing doctor pass tests
                 stay green.
       covers:   c-9
       contract: empty CLAUDE_CONFIG_DIR → non-zero exit with ✗ PreToolUse and ✗ PostToolUse;
                   after `dross hooks ensure` → exit 0, ✓ for all four
                 only PreCompact missing → ⚠ line, exit code unaffected
                 gate command only under matcher "Bash" → issue naming the matcher;
                   `"disableAllHooks": true` → issue
                 malformed settings.json → issue naming the parse error (no crash, no pass)
                 doctor_run.txt golden pins the Hooks section; the pre-existing doctor_test pass
                   cases still exit 0 with the seeded hooks
```

## Coverage

| Criterion | Tasks | Risk each task owns |
|---|---|---|
| c-1 | t-16 | idempotent and foreign-preserving wiring, upgrade from 2 hooks, mode and atomicity, init/onboard |
| c-2 | t-3, t-8, t-12 | t-3: no refusal without a rule and remedy, panic posture. t-8: exit 2 not 1, silent allow, no telemetry. t-12: override refusal names the human path |
| c-3 | t-6 | Edit refused, Write create allowed, plan_overwrite pending-only, unparseable plan |
| c-4 | t-1, t-2, t-4, t-7, t-9, t-13, t-15 | t-1: commit detection. t-2: tree identity and real-index safety. t-4: green storage. t-7: what counts as a full green. t-9: gate policy. t-13/t-15: prompt flows that would otherwise always be refused |
| c-5 | t-4, t-10, t-11, t-13 | t-10: label exactness and approval validity. t-11: session marker. t-13: execute.md offers the label. t-4: approval/marker storage |
| c-6 | t-1, t-5 | t-1: merge attribution through quotes, `\|&` and groups. t-5: tool list and in-every-repo scope |
| c-7 | t-1, t-5 | t-1: operand and heredoc parsing. t-5: path patterns, `.pub` excluded, source allowed |
| c-8 | t-6, t-14 | t-6: half-size boundary, Edit passes. t-14: pause/resume flows no longer trip it |
| c-9 | t-17 | per-hook report, gate-absent fail, matcher/disableAllHooks/parse cases, existing doctor tests |

All 9 criteria covered.

## Judgment calls

1. **Matcher-less hook groups, filtered by tool_name in the dispatcher.** Rejected per-tool matchers. MergeHook's idempotency keys on the command, so a matcher that changes between versions would never be rewritten on upgrade. A drifted matcher could also silently drop Read from c-7. The cost is one process spawn per tool call, and the early exit keeps that cheap.
2. **Exit 2 plus stderr as the block channel.** Rejected JSON `permissionDecision`. Exit 2 blocks on the code alone and keeps stdout empty for c-2. A refusal must carry `ExitCodeError{Code:2}`, because cobra's default exit 1 is non-blocking (fail-open).
3. **Hand-rolled lenient lexer.** Rejected `mvdan.cc/sh`. The user's shell is zsh, and a strict bash parser errors on zsh-isms, which pushes more calls into the cannot-judge path. A lenient lexer degrades instead of failing, and the repo gains no new dependency.
4. **Gate state in a self-ignoring `.dross/gate/` with single-writer JSON files.** Rejected local.toml and state.json:
   - `localstore.Save` truncates in place (`os.Create`) and holds consent grants that a per-tool-call writer could clobber.
   - state.json has a generic `dross state set` writer an agent could use to forge a mode.
   - The `*` .gitignore inside the directory covers repos onboarded before this phase without touching gitignore.go.
5. **Tree identity is a .dross/-excluded tree hash built on a copy of the real index.** Rejected `git stash create` and write-tree on the live index. `stash create` drops untracked files, so new test files that were actually tested would be falsely refused. Excluding .dross/ means `dross task status` and `changes record` bookkeeping between test and commit doesn't invalidate the green.
6. **Replay earlier `git add` segments onto the temp index; refuse any other predecessor.** Rejected refusing all chains, because add-then-commit is the shape execute.md and quick.md produce. Rejected predicting arbitrary chains, because a `sed -i` or `gofmt -w` before the commit would make the prediction wrong, which is a false allow.
7. **Pathspec, --only, --include and -p commits are refused with "stage, then plain git commit".** Rejected modelling them. They are rare, and getting `--only` semantics wrong is a false-allow risk.
8. **What counts as a full green.** Either the whole-suite run with no selector (including `--files` in a lane-less repo, which runs test_command per locked bare_test_run), or a lane run where every declared lane ran unscoped. A red full run on the recorded tree clears the green. Rejected treating any exit-0 run as green, which green_run_definition forbids.
9. **No carve-out for root commits or doc-only commits.** This covers init's first commit and ship's ARCHITECTURE.md commit. The spec says "exact tree", so t-15 changes those prompts to run a full `dross test` instead. This costs a full suite at ship.
10. **Pair-session liveness is derived** from execute.json + current_phase + the phase branch + unfinished tasks. Rejected binding to session_id, because CLI commands never see it. Rejected a `dross execute end` verb, because an agent-runnable end is a gate bypass. A stale marker goes inert on its own.
11. **Approval stays valid while its HEAD is an ancestor of current HEAD with only .dross/ paths changed since.** Rejected exact HEAD equality, because board-sync or bookkeeping commits between approval and edit would falsely revoke it.
12. **Bash `dross execute begin --solo` is refused during a live pair session.** Downgrading to solo is lifting c-5, which gate_override reserves for the human.
13. **Tamper guard on Write/Edit of `.dross/gate/**` and the overrides file.** Without it, one Write forges a green or an override, which makes gate_override advisory. Bash-level writes stay out of scope per shell_detection_depth. Forging approval by piping a payload into `dross gate record` is deliberate evasion, so it is not guarded either.
14. **A panic inside a claiming gate refuses the call, naming the internal error** (closed in-domain). A panic before any gate claims passes with a warning. Recovery is mandatory either way: an uncaught Go panic exits 2, which blocks with a stack trace.
15. **The extension mechanism is an additive user-level `~/.claude/dross/gates.toml`.** Rejected project.toml, because the secret guards fire outside dross repos (guard_scope). Additive-only means an extension can never remove a default. A malformed file falls back to the defaults plus a warning, never to fewer guards.
16. **Default patterns are taken literally.** `.env*` also refuses `.env.example`. The spec states the default; no carve-outs were added.
17. **Wiring goes last (t-16, wave 4).** This is a safety-ordering dependency, not a data one. Arming the hooks before the recorder (t-7), the override (t-12) and the execute.md label (t-13) exist would block this repo's own remaining commits. t-15 is a soft dependency left out, because the commit refusal text already names `dross test`.
18. **Doctor test fixture:** `chdir` seeds the four hooks via ensureUserHooks. Rejected seeding fixture by fixture across ~100 doctor invocations in 10 test files. Tests that need an empty config already set their own CLAUDE_CONFIG_DIR.
19. **Telemetry is skipped for `dross gate check/record`.** Default-ON telemetry would otherwise append a line for every tool call.
20. **Shared golden files are not serialized.** t-8, t-11 and t-12 all edit cli_tree.txt or main.go, and pair execution is sequential, so each task regenerates the golden instead.
21. **c-5 claims only in-repo paths outside .dross/.** Scratchpad and `~/.claude` memory writes are not this repo's code, and blocking them adds friction without protection.
22. **c-8's sanctioned path is Edit,** and t-14 moves pause/resume to Edit. Rejected exempting handoff.md, which the criterion names explicitly.
23. **`cd <dir> &&` tracking was added beyond the locked detection list.** It is an honest shape, and without it the commit gate would judge the wrong repo's tree.
