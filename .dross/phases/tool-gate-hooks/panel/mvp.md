# Planner panel draft — MVP lens

Lens: the fewest tasks that satisfy every criterion. Every task maps to a criterion. One shared
engine package (`internal/gate`) holds all rule logic. One thin cobra verb (`dross gate`) is the
hook entry point. The only new store is a user-level override file. Per-repo records ride the
existing gitignored `state.json`.

```
Phase tool-gate-hooks — 8 tasks across 3 waves

Wave 1
  t-1  Add gate engine with secret guards
       files:    internal/gate/gate.go, internal/gate/shell.go, internal/gate/secrets.go,
                 internal/gate/gate_test.go
       covers:   c-2, c-6, c-7
       desc:     New package internal/gate. Payload decode (tool_name, tool_input, tool_response, cwd).
                 Decision{Gate, Reason, Use} whose Message() names the rule and the sanctioned path.
                 Pre(payload, overrides) returns nil to allow; Post(payload) is the recorder dispatch
                 (empty until t-6). FindRoot walks up from the target path/cwd to a .dross dir, so
                 workflow rules run only under a root (guard_scope). shell.go is a quote-aware segment
                 splitter: `&&` `||` `;` `|` `|&` and newline, env-assignment prefixes stripped, and
                 per-segment MergesStderr for `2>&1` / `&>` / `|&`. secrets.go holds the built-in lists
                 (secret tools starting with pass-cli; `*.env`, `.env*`, `*.pem`, `*.key`, `id_*` except
                 `*.pub`, age keys `*/age/keys.txt` and `*.agekey`). It has three rules: secret-merge,
                 secret-read (Read tool, plus Bash cat/head/tail/less/sed; `source`/`.` exempt) and
                 gate-override (any Bash segment running `dross gate off`, never skippable).
       contract: Bash `pass-cli item view x 2>&1 | head` is refused with Gate=="secret-merge", and so are
                   the `|&` and `&>` forms and `FOO=1 pass-cli ...`. `go test ./... 2>&1` returns nil.
                 Bash `cat .env && echo ok` and `sed -n 1p deploy.key` are refused with Gate=="secret-read",
                   and the message contains "source". `source .env && go test` returns nil.
                 Read of `/x/id_ed25519` is refused and `/x/id_ed25519.pub` returns nil. Read of
                   `<tmp>/norepo/.env` with no .dross ancestor is still refused (secret guards fire everywhere).
                 Bash `cd x && dross gate off commit-green` is refused with Gate=="gate-override" even when
                   every gate name is in overrides. With "secret-read" in overrides, `cat .env` returns nil.
                 Decode of a non-JSON payload returns an error and a payload for an unknown tool returns nil.

  t-2  Wire PreToolUse/PostToolUse gate hooks
       files:    internal/hooks/settings.go, internal/cmd/hooks.go, internal/cmd/hooks_test.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-1
       desc:     Add EventPreToolUse/EventPostToolUse. Replace the two-entry loop in ensureUserHooks with
                 one exported-in-package table of {event, command, gate bool}: PreCompact `dross pause
                 --auto`, SessionStart `dross reentry`, PreToolUse `dross gate pre`, PostToolUse `dross
                 gate post`. Every entry is merged through the unchanged hooks.MergeHook as a matcher-less
                 group. Update the `hooks ensure` Short and print line, and regenerate the cli_tree golden.
       contract: `dross hooks ensure` on a settings.json holding a foreign PreToolUse group (matcher "Bash",
                   foreign command) keeps that group byte-for-byte. It adds `dross gate pre` under PreToolUse
                   and `dross gate post` under PostToolUse, and leaves PreCompact/SessionStart in place.
                 A second ensure leaves settings.json byte-identical and prints "already wired".
                 `Init()` in a temp CLAUDE_CONFIG_DIR writes all four commands to that dir's settings.json.
                 If any hook command constant is mistyped, the exact-string assertion fails.

Wave 2 (t-3 depends t-2; t-4..t-7 depend t-1)
  t-3  Report dross hook wiring in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
                 internal/cmd/testdata/cli_surface/doctor_run.txt
       covers:   c-9
       desc:     New "Hooks:" section that reads userSettingsPath() (honours CLAUDE_CONFIG_DIR). For each
                 t-2 table entry it tests wiring as bytes.Equal(MergeHook(b, ev, cmd), b), so no new
                 settings code is needed. A missing gate hook prints ✗ and counts as an issue (exit 1). A
                 missing PreCompact/SessionStart prints ⚠ as a warning. Regenerate the doctor_run golden.
       contract: A fixture built with Init() (all four wired) prints ✓ for PreCompact, SessionStart,
                   PreToolUse and PostToolUse, and the hooks section adds no issue.
                 After `dross gate pre` is removed from settings.json, doctor prints ✗ PreToolUse and
                   returns a non-nil error (non-zero exit).
                 With PreCompact removed only, doctor prints ⚠ and the hooks section adds no issue.
                 If CLAUDE_CONFIG_DIR points at an empty dir while hooks are wired elsewhere, doctor fails.

  t-4  Gate plan.toml edits and curated shrinks
       files:    internal/gate/drossfiles.go, internal/gate/drossfiles_test.go
       covers:   c-3, c-8
       desc:     Two workflow rules that run only under a .dross root.
                 plan-edit: Edit/MultiEdit on an existing `.dross/phases/*/plan.toml` is refused (Use: `dross
                 task add/edit/move/remove`). Write is allowed when the file is absent, or when
                 phase.LoadPlan shows every task pending. Otherwise it is refused, and a load error is
                 refused naming the error.
                 curated-shrink: a Write over an existing project.toml, milestones/*.toml,
                 phases/*/spec.toml, handoff.md or rules.toml is refused when len(content) < size/2.
       contract: An Edit or MultiEdit payload on an existing `<repo>/.dross/phases/p/plan.toml` is refused
                   with Gate=="plan-edit", and the message contains "dross task edit".
                 A Write creating a new plan.toml returns nil, and so does a Write over a plan whose tasks
                   are all pending. Flipping one task to "in_progress" makes the same Write refused.
                 A Write over a plan.toml holding invalid TOML is refused, and the message names the parse
                   error (closed in-domain).
                 A Write of 499 bytes over a 1000-byte project.toml is refused with Gate=="curated-shrink".
                   Writes of 500 bytes and 2000 bytes return nil, and so does an Edit.
                 The same shrinking Write on a non-curated `.dross/phases/p/notes.md`, or on a project.toml
                   with no .dross ancestor, returns nil.

  t-5  Gate git commit on recorded green tree
       files:    internal/gate/commit.go, internal/gate/commit_test.go, internal/state/state.go,
                 internal/cmd/test.go, internal/cmd/test_test.go
       covers:   c-4
       desc:     TreeFingerprint excludes `.dross/` and runs git through internal/gitrun with positionals
                 behind `--`. The working-tree side is `ls-files -co --exclude-standard` plus `hash-object
                 --stdin-paths`. The index side is `ls-files -s`. `dross test` takes a fingerprint before
                 the spawn. It writes state.GreenTree only on exit 0 of a full run: a bare run (no selector,
                 no lane path) or a lane run where every declared lane ran green. A red full run clears it.
                 commit-green rule: detects `git [-C dir] [-c k=v] commit`. A staged set entirely under
                 `.dross/` passes. `-a`/`--all`/pathspec forms are refused as unjudgeable. Otherwise the
                 index fingerprint must equal GreenTree. A git or state.json error is refused, naming it.
       contract: A stubbed-green bare `dross test` writes a GreenTree that equals TreeFingerprint of the
                   staged tree, and `git commit -m x` then returns nil.
                 A selector run (`dross test ./internal/x`) writes nothing, and so does a lane run that
                   hits 1 of 2 declared lanes. A red bare run clears an existing GreenTree.
                 Staging one extra file after the green makes `git commit` refused with
                   Gate=="commit-green", and the message contains "dross test".
                 With no GreenTree (only a raw `go test` was run) the commit is refused.
                 Editing a `.dross/` file after the green does not invalidate it.
                 A staged set of only `.dross/state.json` passes with no GreenTree.
                 `git -C <repo> commit`, `FOO=1 git commit` and `make && git commit` are each detected, all
                   with payload cwd outside the repo where -C is used. `git commit -am x` is refused as
                   unjudgeable.
                 A commit in a temp git repo with no .dross dir returns nil.

  t-6  Gate pair-mode edits on recorded approval
       files:    internal/gate/pair.go, internal/gate/pair_test.go, internal/state/state.go
       covers:   c-5
       desc:     Add state fields ExecuteMode ("", pair, solo) and Approval{Phase, Task, Head, At}.
                 pair-approval rule: an Edit/Write/MultiEdit/NotebookEdit outside `<root>/.dross/` is armed
                 when current_phase's plan has an in_progress task and ExecuteMode != solo. Armed, it
                 needs Approval == (phase, that task, current HEAD), otherwise it refuses with Use:
                 "AskUserQuestion offering `approve t-N`". Missing state or plan means not armed.
                 Unparseable state, unparseable plan or git failure is refused, naming the error.
                 Post recorder: AskUserQuestion answers (tool_response/tool_input `answers` values) that
                 exactly equal "approve <in_progress id>" write Approval at HEAD. Anything else writes
                 nothing.
       contract: With t-2 in_progress and ExecuteMode unset, an Edit to `<repo>/main.go` is refused with
                   Gate=="pair-approval", and the message contains "approve t-2". An Edit to
                   `<repo>/.dross/handoff.md` returns nil.
                 A Post payload answering "approve t-2" makes the same Edit return nil.
                 Answers "steer", "approve t-1", "approve t-2 please" and freeform text record nothing, and
                   the Edit stays refused.
                 After a new commit moves HEAD, the earlier approval no longer unlocks the Edit.
                 ExecuteMode=solo returns nil with no approval, and so does having no in_progress task.
                 A corrupt state.json gives a refusal naming the parse error.

  t-7  Add dross gate pre/post/off command
       files:    internal/cmd/gate.go, internal/cmd/gate_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-2
       desc:     `dross gate pre` reads stdin and calls gate.Pre with the active overrides. A refusal is
                 ExitCodeError{2} with the Message on stderr. An allow exits 0 silently. A decode failure
                 exits 0 with a stderr warning. `dross gate post` calls gate.Post and always exits 0.
                 `dross gate off <name> [--for 1h]` validates the name against the gate list. It writes
                 {name, until} to GlobalDir()/gates.toml, and expired entries are ignored. Register the
                 verb in main.go, skip RecordCLIEvent for gate pre/post (they fire on every tool call),
                 and regenerate cli_tree.
       contract: `dross gate pre` fed a Read-.env payload returns exit code 2, and stderr contains
                   "secret-read" plus the sanctioned path.
                 Fed a Read-README.md payload it returns 0 with empty stdout and stderr.
                 Fed "not json" it returns 0 with a warning on stderr.
                 `dross gate post` fed any payload returns 0 with no stdout.
                 After `dross gate off secret-read --for 10m` the Read-.env payload returns 0. An entry
                   whose expiry has passed is refused again.
                 `dross gate off nope` errors and lists the valid gate names.
                 The cli_tree golden shows `dross gate pre|post|off`.

Wave 3 (depends t-6)
  t-8  Teach execute.md approve label and modes
       files:    assets/prompts/execute.md, internal/cmd/execute_prompt_test.go, internal/cmd/state.go,
                 internal/cmd/state_test.go
       covers:   c-5, c-4
       desc:     Add `dross state set|get execute_mode <pair|solo>`; other values are refused. In
                 execute.md:
                 - pre-flight sets execute_mode from --solo every run;
                 - §1c offers `approve <task-id>` in place of `proceed` and says only that exact label
                   unlocks edits;
                 - the hard rule names the approve label;
                 - §1e/1f says a commit needs a full `dross test` green for the staged tree, so a
                   lane-scoped `--files` run that missed a lane needs a bare `dross test` first;
                 - gate refusals are hard stops, and `dross gate off` is human-only.
                 r-01: run `make install` before relying on it.
       contract: TestExecutePrompt* asserts that §1c contains "`approve <task-id>`" and no longer offers
                   "`proceed`" as the approval option.
                 It asserts that pre-flight contains `dross state set execute_mode`, for both the pair
                   and the solo branch.
                 It asserts that the commit step contains "bare `dross test`" ahead of `git commit`.
                 `dross state set execute_mode solo` round-trips through `state get`, and `dross state set
                   execute_mode banana` returns an error naming pair|solo.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (ensure/init/onboard wire Pre/PostToolUse, byte-stable, foreign entries survive) | t-2 |
| c-2 (refusal blocks with rule + sanctioned path; allow is silent) | t-1 (Decision/Message, per-rule text), t-7 (exit 2 + stderr end to end, silent allow); every rule's message is asserted in t-4, t-5, t-6 |
| c-3 (plan.toml Edit refused, Write create allowed) | t-4 |
| c-4 (commit refused without a `dross test` green for the exact tree; `.dross/`-only passes; raw go test doesn't count) | t-5 (record + gate), t-8 (execute loop runs the full green before commit) |
| c-5 (pair-mode edits need an approval since the last commit; solo silent) | t-6 (gate + recorder), t-8 (approve label, mode declaration) |
| c-6 (stderr merge on secret-emitting tool refused) | t-1 |
| c-7 (Read/cat/head/tail/less/sed of secret paths refused; source allowed) | t-1 |
| c-8 (curated `.dross/` Write shrinking below half refused) | t-4 |
| c-9 (doctor reports four hooks, fails on missing gate hook) | t-3 |

Locked decisions: hook_scope t-2 · guard_scope t-1 routing, asserted in t-1/t-4/t-5 · gate_override
t-1 (refusal) + t-7 (verb) · pair_approval_signal t-6 + t-8 · gate_error_posture t-7 (payload) and
t-4/t-5/t-6 (in-domain) · plan_overwrite t-4 · shell_detection_depth t-1 parser + t-5 forms ·
green_run_definition t-5 · gate_list_source t-1.

## Judgment calls

- **Matcher-less hook groups.** I chose groups with no matcher and a single dispatcher binary over adding matcher support to MergeHook. MergeHook stays untouched, so its byte-stable contract needs no new proof. The cost is one fast process spawn per tool call.
- **No list-extension mechanism in this phase.** I chose built-in lists only (gate_list_source leaves the mechanism to the planner) over user-level `secret_tools`/`secret_paths` arrays in gates.toml. No criterion needs extension, and adding it later doesn't break anything.
- **Green, Approval and ExecuteMode live in `.dross/state.json`.** I rejected a new `.dross/gate.json`, which would need gitignore plumbing and would ride `git add .dross/`. I also rejected local.toml, which is typed config with a closed Keys set. state.json is already gitignored, dross-owned position data, and `state set`'s closed switch keeps green/approval out of CLI reach.
- **Overrides in `GlobalDir()/gates.toml` (user-level).** I chose this over per-repo storage because secret gates fire outside dross repos, where no `.dross/` exists.
- **The pair gate is armed by default.** It arms when an in_progress task exists and ExecuteMode is not solo. I rejected an explicit "pair begin" marker because a forgotten marker would silently disable the gate (fail-open), which repeats the 2026-05-29 incident.
- **Approval is bound to (task, HEAD).** That implements "since the previous task's commit" without a second PostToolUse recorder on `git commit`, and the task id in the label stops a t-N approval from unlocking t-N+1.
- **The fingerprint excludes `.dross/`, and the commit side reads the index.** `-a`/pathspec commits are refused as unjudgeable rather than modelled with a temp index. gitrun has no env seam for GIT_INDEX_FILE, gate_error_posture allows the refusal, and execute.md already forbids those forms.
- **A bare `dross test` counts as full even in a lane repo.** It runs runtime.test_command, which test.go itself defines as the whole suite. A lane run counts only if every declared lane ran green. A red full run clears the record, so a flaky green can't be committed past a later red.
- **c-1 and c-9 are split against the lens.** Each regenerates a different golden (cli_tree.txt vs doctor_run.txt), and merged they come to 7 files. Ordering t-3 after t-2 also keeps the Init-based doctor fixtures green, because Init wires the gate hooks.
- **t-5 is kept at 5 files against the split rule.** The fingerprint writer (`dross test`) and reader (the commit gate) must land together. Split, either half alone is untestable against the other, and an installed reader with no writer refuses every commit.
- **docs/interaction-audit.md is not updated.** Its execute row still says "leads with `proceed`" after t-8. That doc drift has no criterion and no test, and the judge may fold it into t-8.
- **Telemetry skip for gate pre/post is folded into t-7.** main.go is already in t-7's files, and a telemetry.jsonl line per tool call would be a regression introduced by this phase.
