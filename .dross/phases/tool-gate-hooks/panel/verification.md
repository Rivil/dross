# tool-gate-hooks — verification-lens draft

Lens: each criterion's ideal test contract was written first, then the smallest
task that makes that contract satisfiable was derived from it. Tasks exist to
make contracts pass. A foundation task appears only where three or more
contracts need the same primitive (shell segmenting, tree fingerprints, gate
records, the dispatcher).

Constraints found in the tree that shape the plan:
- `boundary_test.go` bans `encoding/json` and `BurntSushi/toml` in non-test
  `internal/cmd` files. Payload decoding and `gates.toml` therefore live in a
  new `internal/gate` package, and `internal/cmd/gate.go` is cobra wiring only.
- `chdir()` in `cmd_test.go` points `CLAUDE_CONFIG_DIR` at an empty temp dir.
  Once doctor fails on a missing gate hook (c-9), about 20 existing doctor tests
  would go red unless that helper seeds the hooks. This is handled in t-12.
- `cmd/dross/testdata/cli_tree.txt` and `internal/cmd/testdata/cli_surface/doctor_run.txt`
  are goldens. New verbs and the new doctor section regenerate them
  (`DROSS_UPDATE_GOLDEN=1`).
- Every git spawn goes through `internal/gitrun`, which has no env support. A
  temp-index fingerprint needs `GIT_INDEX_FILE`, so `gitrun.Options` gains an
  `IndexFile` field. This adds no new `exec.Command` site.
- PostToolUse fires for every tool except EndConversation, so AskUserQuestion
  is included. Its `tool_response` shape is not documented, so the recorder has
  to tolerate an unknown shape: it warns and records nothing.

```
Phase tool-gate-hooks — 15 tasks across 5 waves

Wave 1
  t-1  Add shell command segment scanner
       files:    internal/shellscan/shellscan.go, internal/shellscan/shellscan_test.go
       covers:   c-4, c-6, c-7
       desc:     Split a Bash command line into segments on && || ; | |& and newline. Quotes, backslash
                 escapes, heredoc bodies and $(...) are opaque. Per segment: argv with leading VAR=val and
                 an `env [VAR=val…]` prefix stripped, argv0 as basename, redirection targets, MergesStderr().
       contract: `FOO=1 env BAR=2 /usr/bin/pass-cli item view x` → one segment, Cmd()=="pass-cli", args
                 [item view x]. If env-prefix stripping breaks, TestEnvPrefixStripped fails.
                 `a && b; c | d || e` → 5 segments in order. `echo "a && b"` / `echo 'x; y'` stay one segment.
                 `git commit -m "$(cat <<'EOF'\nfix; a && b\nEOF\n)"` → ONE segment with Cmd git, arg commit.
                 A heredoc or $(...) that gets split fails TestHeredocCommitIsOneSegment.
                 MergesStderr true: `x 2>&1`, `x &>f`, `x &>>f`, `x >&f`, and x in `x |& y`.
                 MergesStderr false: `x 2>/dev/null`, `x >&2`, `x 1>&2`, `echo "2>&1"`.
                 `cat < .env` exposes `.env` as an input-redirect target.
                 `sh -c "pass-cli x 2>&1"` → Cmd()=="sh", body not descended (pins the shell_detection_depth
                 out-of-scope line).
                 An unterminated quote returns an error, never a partial split.

  t-2  Wire gate hooks beside PreCompact/SessionStart
       files:    internal/hooks/settings.go, internal/hooks/settings_test.go, internal/cmd/hooks.go,
                 internal/cmd/hooks_test.go, internal/cmd/init_test.go, internal/cmd/onboard_test.go
       covers:   c-1
       desc:     Add EventPreToolUse/EventPostToolUse and HasHook(settings, event, command) to
                 internal/hooks. ensureUserHooks adds matcher-less `dross gate check` (PreToolUse) and
                 `dross gate record` (PostToolUse). `hooks ensure` output names all four.
       contract: After two ensures there is exactly one `dross gate check` under PreToolUse and one
                 `dross gate record` under PostToolUse, and the second run is byte-identical
                 (TestEnsureUserHooksIdempotent extended).
                 A foreign PreToolUse group {"matcher":"Bash","hooks":[…/usr/local/bin/lint.sh…]}
                 survives verbatim, matcher included.
                 TestInitEnsuresHooksIdempotent and TestOnboardEnsuresSameHooks assert all four events.
                 Dropping the gate pair from ensureUserHooks fails both.
                 HasHook(s,e,c) is true exactly when MergeHook(s,e,c) returns s unchanged (table:
                 present / absent / foreign-only). Garbage JSON makes both return an error.

  t-3  Add working-tree and projected-index fingerprints
       files:    internal/treeprint/treeprint.go, internal/treeprint/treeprint_test.go,
                 internal/gitrun/gitrun.go, internal/gitrun/gitrun_test.go
       covers:   c-4
       desc:     Fingerprint(repoDir, adds...) copies the real index to a temp file and applies each
                 `git add` arg-list to it (gitrun Options.IndexFile → GIT_INDEX_FILE). It returns sha256
                 over `git ls-files -s` minus `.dross/` entries. ChangedPaths(repoDir, adds...) lists
                 projected paths differing from HEAD. WorkTree = Fingerprint(dir, ["-A"]).
       contract: WorkTree changes on a tracked-file edit, a new untracked non-ignored file, or a chmod
                 +x. It does NOT change for an ignored file or any edit under `.dross/`.
                 Staging every change makes Fingerprint(dir)==WorkTree(dir). Leaving one edited file
                 unstaged makes them differ.
                 `.git/index` bytes are identical before and after WorkTree and after a projected add
                 (the real index is never mutated).
                 Fingerprint(dir, ["--","a.go"]) equals the plain fingerprint after really running
                 `git add a.go`.
                 ChangedPaths with only `.dross/x` staged returns exactly [".dross/x"], including in a
                 repo with no HEAD (initial commit).
                 gitrun: RawWith(Options{IndexFile: tmp}, "ls-files") lists the temp index, not the
                 real one.

  t-4  Add machine-local gate records to local.toml
       files:    internal/localstore/store.go, internal/localstore/gate.go, internal/localstore/gate_test.go
       covers:   c-4, c-5
       desc:     Typed records GateGreen{Tree, At}, ExecMode{Phase, Mode} and
                 GateApproval{Phase, Task, Head, At}, each with Record/Read/Clear helpers. None is exposed
                 through `dross local set`.
       contract: Each record round-trips. Recording one leaves quick_base, remote grants and
                 detached_run entries intact (load-compare).
                 Reading any gate record from a git-tracked local.toml returns the RefuseTrackedLocal
                 error, so a committed `gate_green` cannot pre-authorize a cloned repo's commits.
                 KeyNames() contains no gate_* key.
                 A missing local.toml reads as "no record", not an error.

  t-5  Add gate dispatcher, payload decode, gates.toml
       files:    internal/gate/gate.go, internal/gate/gate_test.go, internal/gate/config.go,
                 internal/gate/config_test.go
       covers:   c-2
       desc:     Decode the hook payload (tool_name, tool_input, tool_response, cwd). Gate registry entries
                 carry Name, Remedy, Scope (always-on|workflow), Liftable, ListExtensible, Claims(tool) and
                 Judge. Check/Record dispatchers return a Verdict. Add a read-only dross-root locator for a
                 target path, plus ~/.claude/dross/gates.toml (overrides with expiry; extension lists for
                 secret tools, secret path patterns and curated files).
       contract: An unparseable payload → Allow with a non-empty Warning and zero Judge calls (the open
                 side of gate_error_posture).
                 A Judge error → Refuse whose text names the gate and the error (the closed side).
                 Every refusal contains the gate name, "Use: <remedy>", and the "a human can run
                 `dross gate off <name>`" hint. A registry test fails for any gate with an empty Remedy.
                 guard_scope: a workflow fake gate is NOT invoked when the target resolves to no
                 `.dross/project.toml` root. An always-on fake gate IS invoked. A `.dross/` without
                 project.toml counts as no root.
                 An unclaimed tool (Glob) → Allow, empty output, no Judge call, zero git spawns (counting
                 seam).
                 An override on X with a future expiry skips X only. An expired override is ignored.
                 Liftable=false gates ignore overrides.
                 Malformed gates.toml → ListExtensible gates refuse their in-domain calls naming the file
                 path. Other gates judge normally.
                 The locator never writes (no state.json appears in a fixture root that lacked one).

  t-6  Make ship/init run dross test before committing
       files:    assets/prompts/ship.md, assets/prompts/init.md, internal/cmd/ship_prompt_test.go,
                 internal/cmd/prompt_test_command_test.go
       covers:   c-4
       desc:     ship.md §3 runs a bare `dross test` before the ARCHITECTURE.md docs commit. init.md §9
                 writes .gitignore, then runs `dross test` (after the human's `dross trust`) before the
                 initial commit when runtime.test_command is set. No prompt tells the agent to run
                 `dross gate off`.
       contract: TestShipPromptTestsBeforeDocsCommit: in ship.md a `dross test` line precedes
                 `git commit -m "docs(`. Deleting or moving it fails.
                 init.md: `dross test` sits between the .gitignore step and `git add . && git commit`.
                 ship.md and init.md join suiteRunningPrompts, so a raw `<runtime.test_command>` line
                 fails TestPromptsRunDrossTest.
                 TestNoPromptTellsAgentToLiftGate (mirror of TestNoPromptGrantsTrustForTheUser): any
                 assets/prompts/*.md instructing the agent to run `dross gate off` fails.

Wave 2
  t-7  Add always-on secret and gate-off guards
       files:    internal/gate/secrets.go, internal/gate/secrets_test.go, internal/gate/selfguard.go,
                 internal/gate/selfguard_test.go
       covers:   c-6, c-7, c-2
       depends:  t-1, t-5
       desc:     Always-on gates: `secret-stderr` (c-6), `secret-read` (c-7) and `gate-off-guard`
                 (gate_override, Liftable=false). Built-in defaults are tools [pass-cli]; paths *.env,
                 .env*, *.pem, *.key, id_* except *.pub, *.agekey, sops/age/keys.txt; readers cat head tail
                 less sed. gates.toml extensions are merged on top.
       contract: c-6 refused: `pass-cli item view x 2>&1 | head`, `… |& head`, `… &>out`,
                 `FOO=1 pass-cli … 2>&1`, `cd d && pass-cli … 2>&1`, `/opt/bin/pass-cli … 2>&1`. The
                 message names secret-stderr and `>file 2>/dev/null`.
                 c-6 allowed: `pass-cli item view x >f 2>/dev/null`, `go test ./... 2>&1 | tail`,
                 `echo hi 2>&1; pass-cli item view x >f 2>/dev/null`, `echo "pass-cli x 2>&1"`. A tool
                 added via gates.toml secret_tools is refused the same way.
                 c-7 Read refused: .env, prod.env, .env.local, server.pem, tls.key, ~/.ssh/id_ed25519,
                 ~/.config/sops/age/keys.txt, ops.agekey.
                 c-7 Read allowed: id_ed25519.pub, envoy.yaml, main.go, a keys.txt outside sops/age.
                 c-7 Bash refused: `cat .env`, `head -n1 .env`, `tail -f .env.local`, `less id_rsa`,
                 `sed -n 1p .env`, `cat < .env`, `cd x && cat .env`.
                 c-7 Bash allowed: `source .env && go test ./...`, `. ./.env`, `wc -c .env`,
                 `shasum -a 256 .env`, `grep -q KEY .env`.
                 Both fire with payload cwd in a temp dir with no .dross (guard_scope).
                 gate-off-guard refused in any repo: `dross gate off secret-read`,
                 `FOO=1 dross gate off x`, `git status && dross gate off x`,
                 `/usr/local/bin/dross gate off x`.
                 gate-off-guard allowed: `dross gate status`, `dross gate on x`, `echo dross gate off`.

  t-8  Add plan-edit and curated-shrink gates
       files:    internal/gate/drossfiles.go, internal/gate/drossfiles_test.go
       covers:   c-3, c-8
       depends:  t-5
       desc:     Workflow gates on Edit/MultiEdit/Write targets under a dross root's `.dross/`:
                 `plan-edit` (c-3 + plan_overwrite via phase.LoadPlan) and `curated-shrink` (c-8, Write
                 only, len(content) < size/2).
       contract: Edit and MultiEdit on an existing `.dross/phases/p/plan.toml` → refused naming
                 `dross task add/edit/move/remove`.
                 Write creating a non-existent plan.toml → allowed. Write over a plan whose tasks are all
                 pending, empty status included → allowed. If one task is in_progress, done or failed →
                 refused.
                 Write over an existing but unparseable plan.toml → refused naming the parse error.
                 `docs/plan.toml` and `.dross/phases/p/spec.toml` are untouched by plan-edit.
                 c-8 table over every default curated path (project.toml, milestones/v1.toml,
                 phases/p/spec.toml, handoff.md, rules.toml), against a 100-byte file: a 49-byte Write is
                 refused, 50 bytes allowed, 150 bytes allowed. Edit of any size is allowed.
                 A new milestones/v2.toml Write is allowed. Shrinking non-curated
                 phases/p/changes.json is allowed. A curated path added via gates.toml is enforced. A
                 stat error other than not-exist → refused naming it.

  t-9  Add commit-green gate on git commit
       files:    internal/gate/commit.go, internal/gate/commit_test.go
       covers:   c-4
       depends:  t-1, t-3, t-4, t-5
       desc:     For each `git [-C d] [-c k=v] commit` segment, tracking `cd` and -C, project the index
                 the commit will record: earlier `git add` segments in the chain are applied, and -a adds
                 -u. Pass if ChangedPaths are all under `.dross/`. Otherwise require GateGreen.Tree ==
                 the projected fingerprint.
       contract: Temp git repo with .dross, a staged src change and no green → refused, naming
                 `dross test` and stating that a raw `go test` does not count.
                 Green recorded for WorkTree, then `git add -A && git commit -m x` → allowed. Edit one
                 more byte, stage it, commit → refused (tree mismatch).
                 `git add a.go && git commit -m x` is judged against index+a.go, not the pre-add index.
                 Only `.dross/` staged, no green → allowed. `git commit -am x` with only tracked .dross
                 edits → allowed.
                 `git -C ../other commit` and `cd ../other && git commit` judge ../other. If ../other has
                 no .dross → allowed.
                 `git commit -m x -- a.go`, `git commit -p` and `git add -p && git commit` → refused as
                 unjudgeable: "stage with git add, then run a plain git commit".
                 A repo with neither runtime.test_command nor a test lane → allowed silently.
                 `git log`, `echo git commit` and `git commit-tree` are not claimed.
                 A corrupt index, a non-git dir under a .dross root, or a ReadGreen error (tracked
                 local.toml) → refused naming the error.

  t-10 Add pair-approval gate and approval recorder
       files:    internal/gate/pair.go, internal/gate/pair_test.go
       covers:   c-5
       depends:  t-1, t-4, t-5
       desc:     Workflow gate `pair-approval` on Edit/MultiEdit/Write outside `.dross/`. It is active
                 while current_phase has an in_progress task and that phase's ExecMode is not solo.
                 PostToolUse recorder: store GateApproval{phase,task,HEAD} only when an AskUserQuestion
                 tool_response answer equals `approve <in-progress-id>` exactly. Refuse Bash
                 `dross gate mode solo` while a task is mid-flight.
       contract: Fixture with t-2 in_progress and no approval → Edit src/a.go refused, naming
                 `approve t-2` and AskUserQuestion. Edit under .dross/ → allowed.
                 Approval {p,t-2,HEAD} → allowed. After one new commit → refused again.
                 Approval for t-1 while t-2 is in_progress → refused. No in_progress task → allowed.
                 ExecMode solo → allowed with no output.
                 Unreadable state.json or plan.toml → refused naming the error.
                 Recorder: `approve t-2` records. `Approve t-2`, `approve t-2 `, `approve t-3`, `steer`,
                 `reject` and `yes, approve t-2` record nothing. An answers map present only in
                 tool_input (agent-authored) records nothing. A tool_response without readable answers
                 → Warning, nothing recorded. Other tools → no-op.
                 `dross gate mode solo p` via Bash → refused while a task is in_progress under pair/unset
                 mode. Allowed when nothing is in_progress.

  t-11 Record dross test greens for full runs
       files:    internal/cmd/test.go, internal/cmd/test_green_test.go
       covers:   c-4
       depends:  t-3, t-4
       desc:     After a green full run, record GateGreen{WorkTree}. A full run is a selector-free
                 test_command run, or a --files lane run where every declared lane ran unscoped and green.
                 A red full run clears the record. Partial runs and runs that did not happen leave it
                 untouched.
       contract: Bare `dross test` with a green spawn seam → local.toml gate_green.tree ==
                 treeprint.WorkTree(repo).
                 A no-lanes repo running `dross test --files a.go` green → records (files are ignored and
                 the whole suite ran; this is execute.md's form).
                 `dross test ./internal/x` green → no record.
                 With lanes: --files hitting 1 of 2 lanes → no record. Hitting both with no selector →
                 record. A lane line carrying a derived selector → no record.
                 A seam that edits a tracked file mid-run → no record (start and end fingerprints differ).
                 Green then red on the same tree → record cleared. Exit 3/4 → an existing record stays
                 intact.
                 A green from the remote spawn seam records the same fingerprint as a local one.

  t-12 Report dross hooks in dross doctor
       files:    internal/cmd/doctor_hooks.go, internal/cmd/doctor_hooks_test.go, internal/cmd/doctor.go,
                 internal/cmd/cmd_test.go, internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-9
       depends:  t-2
       desc:     New "Hooks:" section that reads userSettingsPath() through hooks.HasHook, one ✓/✗ line
                 per event. A missing or unreadable PreToolUse/PostToolUse hook is an issue (non-zero). A
                 missing PreCompact/SessionStart is an advisory ⚠. chdir() seeds all four hooks so
                 existing doctor tests stay green.
       contract: All four wired → four ✓ lines naming each command. The section adds no issue.
                 PreToolUse removed → doctor returns an error and prints ✗ naming `dross gate check` and
                 `dross hooks ensure`. Same for PostToolUse with `dross gate record`.
                 SessionStart removed only → ⚠, and doctor still returns nil on an otherwise healthy
                 fixture.
                 A fully-wired $HOME/.claude/settings.json with an empty CLAUDE_CONFIG_DIR still fails
                 (the effective file is the one read).
                 Malformed settings.json → ✗ naming the parse error, non-zero.
                 doctor_run.txt is regenerated with the section.

Wave 3 (depends t-2, t-7, t-8, t-9, t-10)
  t-13 Add gate check/record hook verbs
       files:    internal/cmd/gate.go, internal/cmd/gate_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt, internal/cmd/telemetry.go
       covers:   c-2, c-1
       depends:  t-2, t-7, t-8, t-9, t-10
       desc:     `dross gate check` (PreToolUse) and `dross gate record` (PostToolUse) read
                 InOrStdin and delegate to internal/gate. A refusal maps to ExitCodeError{2} with the
                 reason on stderr once (SilenceErrors on the verb). Both verbs skip CLI telemetry.
                 Register cmd.Gate() and regenerate cli_tree.txt.
       contract: Read `/r/.env` payload → ExitCode(err)==2, stderr has `secret-read` plus the remedy
                 exactly once, stdout empty.
                 Read `/r/main.go` → nil error, stdout AND stderr empty (c-2 "no output, no block").
                 Garbage stdin → nil error, stderr warning.
                 `gate record` returns nil for every payload in a table (PostToolUse cannot block).
                 gate.All() names == {plan-edit, commit-green, pair-approval, secret-stderr, secret-read,
                 curated-shrink, gate-off-guard}. A dropped registration fails.
                 preToolUse/postToolUse hook constants equal "dross " + the verb paths. Removing
                 cmd.Gate() from main.go fails TestCLITreeGolden.
                 RecordCLIEvent for `gate check` leaves telemetry.jsonl unchanged, while `dross status`
                 still appends.

Wave 4 (depends t-13)
  t-14 Add human gate off/on/status/mode verbs
       files:    internal/cmd/gate.go, internal/cmd/gate_human_test.go, cmd/dross/testdata/cli_tree.txt,
                 cmd/dross/gate_remedy_test.go, README.md
       covers:   c-2, c-5
       depends:  t-4, t-13
       desc:     `dross gate off <name> [--for 1h]` (24h cap), `gate on <name>`, `gate status`, and
                 `gate mode <pair|solo> <phase>` (writes ExecMode). Adds a README row and regenerates
                 the golden.
       contract: `gate off secret-read --for 30m` → `gate check` on Read .env exits 0. With the clock
                 seam +31m → exit 2.
                 `--for 25h` → refused naming the cap. An unknown name → refused listing valid names.
                 `gate off gate-off-guard` → refused.
                 `gate on secret-read` → the next check refuses.
                 `gate status` lists every registered gate with on/off and expiry.
                 `gate mode solo p` writes ExecMode{p,solo}. `gate mode turbo p` → refused.
                 Malformed gates.toml → `gate off` refuses naming the file and leaves it byte-identical.
                 TestGateRemediesResolve: every `dross <verb> <sub>` narrated in any gate Remedy or hint
                 resolves in newRoot(). Renaming `task edit` or `gate off` fails it, so c-2's "sanctioned
                 path" cannot point at a dead command.

Wave 5 (depends t-10, t-14)
  t-15 Wire execute.md to approval and mode gates
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go
       covers:   c-5, c-4
       depends:  t-10, t-14
       desc:     At flag parse, run `dross gate mode solo|pair <phase>`. §1c offers the exact
                 `approve <task-id>` label in place of `proceed`. A repo with test lanes runs a bare
                 `dross test` before the §1f commit. On a gate refusal, surface it to the user and never
                 work around it.
       contract: TestExecutePromptOffersApproveLabel: the §1c option list contains `approve <task-id>` and
                 no bare `proceed` option. Reverting the label fails it (pair_approval_signal).
                 TestExecutePromptRecordsMode: `dross gate mode solo` (--solo) and `dross gate mode pair`
                 both appear before the "## 1. Per-task loop" heading.
                 TestExecutePromptFullRunBeforeCommit: a bare `dross test` is required before commit
                 when `[[runtime.test_lane]]` is declared (green_run_definition).
                 t-6's TestNoPromptTellsAgentToLiftGate stays green. The only allowed phrasing is asking
                 the user to run `dross gate off` in their own terminal.
```

## Coverage

Each criterion's ideal contract is listed first, then the tasks that make it satisfiable.

- **c-1**: ideal contract is "after ensure, all four events carry exactly one dross entry, a re-run is byte-identical, a foreign matcher group survives, and init/onboard reach the same state". Tasks: **t-2**, with **t-13** guaranteeing the wired strings are real verbs.
- **c-2**: ideal contract is "refused payload → exit 2, stderr names gate and remedy; allowed payload → exit 0, zero bytes out; the remedy names a command that exists". Tasks: **t-5** (refusal shape), **t-7** (first real refusals), **t-13** (exit 2 / silence), **t-14** (remedy resolution).
- **c-3**: ideal contract is "Edit/MultiEdit on an existing plan.toml refused naming `dross task …`; Write-create allowed; Write-overwrite allowed while all pending, refused after". Task: **t-8**.
- **c-4**: ideal contract is "commit refused without a dross-test green for the projected tree; .dross-only passes; a green recorded by anything but a full `dross test` does not exist". Tasks: **t-1** (detection), **t-3** (tree identity), **t-4** (record), **t-9** (gate), **t-11** (only `dross test` writes the record), **t-6** and **t-15** (prompt flows that commit).
- **c-5**: ideal contract is "in-progress task, no exact `approve t-N` since HEAD → Edit outside .dross refused; solo silent; reject or freeform records nothing". Tasks: **t-4** (records), **t-10** (gate + recorder), **t-14** (`gate mode` verb), **t-15** (execute.md label + mode).
- **c-6**: ideal contract is "merge operators on pass-cli refused across chains, env prefixes and absolute paths; same merge on go/echo passes". Tasks: **t-1**, **t-7**.
- **c-7**: ideal contract is "Read or cat/head/tail/less/sed of each default pattern refused; .pub and lookalikes pass; `source` passes; fires outside dross repos". Tasks: **t-1**, **t-7**.
- **c-8**: ideal contract is "49/100 Write refused, 50/100 passes, Edit passes, table over every curated path". Task: **t-8**.
- **c-9**: ideal contract is "missing gate hook → doctor non-zero naming the hook and `dross hooks ensure`; missing PreCompact/SessionStart → advisory; reads the effective (CLAUDE_CONFIG_DIR) file". Task: **t-12**.

Locked decisions, each pinned by a contract:
- hook_scope: t-2
- guard_scope: t-5 (workflow gates skipped with no root), t-7 (fires outside a dross repo)
- gate_override: t-7 (`gate-off-guard`), t-14 (verbs, expiry, cap)
- pair_approval_signal: t-10 (exact match only), t-15 (label offered)
- gate_error_posture: t-5 (both sides), plus a "cannot judge → refused naming error" contract in t-8, t-9 and t-10
- plan_overwrite: t-8
- shell_detection_depth: t-1 (chains, env prefixes, `sh -c` not descended), t-9 (`git -C`)
- green_run_definition: t-11
- gate_list_source: t-7 and t-8 (built-in defaults), t-5 (gates.toml extension)

Criteria covered: 9/9.

## Judgment calls

- **Hook entries are matcher-less (`dross gate check` / `dross gate record`), rejecting per-tool matchers.** MergeHook stays unchanged and already tested, and a gate claiming a new tool never needs a settings migration. The cost is one process spawn per tool call. t-5's "unclaimed tool → no Judge, no git" contract keeps that cost bounded.
- **Blocking uses exit code 2 + stderr, rejecting the JSON `permissionDecision: deny` output.** Exit 2 is the documented blocking path. It keeps "allowed = zero bytes out" trivially testable, and it does not depend on the hookSpecificOutput schema.
- **Gate logic lives in a new `internal/gate` package, with `internal/cmd/gate.go` as wiring only.** This is forced by boundary_test's ban on encoding/json and BurntSushi/toml in `internal/cmd`. It also keeps gate tests out of the slow `internal/cmd` package.
- **Shell scanning is a hand-rolled `internal/shellscan`, rejecting a mvdan.cc/sh dependency.** shell_detection_depth caps the needed depth, and a new parser dependency widens the supply chain for a single-binary tool. The heredoc and `$(...)` opacity contract covers the realistic failure: Claude Code's own heredoc commit messages.
- **Tree identity is a sha256 over `git ls-files -s` of a temp-index copy, excluding `.dross/`, rejecting `git write-tree` and `git stash create`.** It never mutates the real index, it includes untracked files, and it lets the commit gate project the index after chained `git add` and `-a`. `.dross/` is excluded on both sides because bookkeeping churn must not void a green.
- **Commit forms the gate cannot project (pathspec commits, `-p`/`--interactive`, `git add -p`) are refused as unjudgeable, not approximated.** This applies gate_error_posture's closed side. The remedy is "stage, then plain commit".
- **A repo with no test_command and no lanes leaves commit-green silent, rather than blocking every commit forever.** It mirrors execute.md's existing "no test command → skip the per-task gate".
- **A bare selector-free test_command run counts as full even in a repo with lanes; a --files lane run counts only when every declared lane ran unscoped.** The decision's intent is "a partial run proves less". A whole-suite run is not partial.
- **A red full run clears the green; a run that did not happen (exit 3/4/5) leaves it.** This rejects "only greens write". A later red on the same tree is evidence the earlier green is stale.
- **Gate records go in `.dross/local.toml` via localstore (already gitignored, single writer, RefuseTrackedLocal), rejecting state.json or a new file.** The tracked-local.toml refusal is the one guard that stops a cloned repo shipping its own green.
- **Overrides and extension lists live user-level in `~/.claude/dross/gates.toml`, rejecting project.toml.** guard_scope means the secret guards run where no project.toml exists. A malformed file makes the list-extensible gates refuse in-domain calls (closed), and `gate off` refuses to overwrite it.
- **`gate off` defaults to `--for 1h` and is capped at 24h, rejecting indefinite lifts.** "Until an expiry" implies the gate comes back on without anyone remembering to re-enable it.
- **Pair mode is active only while the current phase has an in_progress task, rejecting "the mode marker alone".** A leftover pair marker would otherwise block the user's ordinary edits after a checkpoint. A missing marker counts as pair because pair is the default.
- **A pair→solo flip via Bash is refused mid-task (t-10), rejecting an unconditional `gate mode`.** Otherwise `dross gate mode solo` becomes exactly the agent-runnable override that gate_override's rationale forbids.
- **Approval is bound to {phase, task, HEAD}.** HEAD movement is the mechanical meaning of "since the previous task's commit". The task binding stops a t-1 approval from unlocking t-2.
- **The recorder reads answers only from `tool_response`, never from `tool_input`.** tool_input is agent-authored and could pre-fill an approval. The undocumented response shape is handled by warning and recording nothing, which leaves the gate closed.
- **Workflow gates resolve their repo from the target (file path, or cwd/-C/cd for Bash), rejecting "the session cwd".** An edit to /tmp or to a non-dross repo during an execute run falls outside guard_scope's ".dross/ repo".
- **`gate check`/`gate record` are exempt from CLI telemetry.** Otherwise every tool call appends an event, and refusal text, which can carry a secret file's path, lands in the "other" bucket's err_detail.
- **ship.md/init.md edits sit in wave 1 (t-6), not after the gate.** They need no gate output, and landing early is harmless. init.md has to work for any user whose hooks are already wired from another repo, because `.dross/` appears mid-session and the commit gate reads it live.
- **The default secret-emitting tool list is pass-cli only, rejecting a speculative list (op, bw, sops).** pass-cli is the recorded stderr-leak incident. Other tools are added via gates.toml once their stream behaviour is established.
