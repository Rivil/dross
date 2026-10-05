# debug-sessions — risk-lens draft

Lens: every failure mode has exactly one owning task, and that task's test
contract is the test that fails when the failure mode comes back.

The graph follows from what can break. Most of it breaks at the parser, so the
parser is the root:

- **Counting is the safety system.** needs-replan (the hard stop) and the
  `--fixed` gate are both counts of list items under fixed headings. Captured
  evidence often quotes `git diff` output, whose `- ` lines look like list
  items. If the counter reads quoted output, the stop fires falsely and the
  close gate passes falsely. If it misses a `[FAILED]` written in another case,
  the stop never fires. So the parser is a pure package with its own task.
- **Every write must leave the tree clean.** A session that dirties the tree
  breaks three things. It stales a pass verdict (verify-staleness). It dirties
  /dross-quick's clean baseline. It rides `autoCommitDrossDirt` into a phase or
  chore PR, which the locked `session_tracking` forbids. Precedent: the gate
  record store's self-ignoring `.dross/gate/.gitignore`. Editing the root
  `.gitignore` would itself be a tracked change.
- **SessionStart is a hook.** `dross reentry` runs in every session in every
  repo. A broken `.dross/debug/` must never make it fail or print junk into
  the hook JSON. `reentryLine` is shared with `dross status`'s footer, and a
  test pins the two byte-equal.
- **The repo's own guards will fire on new code.** These include the writer
  registry (`secretscan/writers.go` plus its enum tests), the pathfence unwrap
  ban, the import-boundary rules (no cobra or os/exec outside their homes),
  the narrated-command and cli_tree goldens, and the command/prompt parity and
  interaction-coverage gates. Each one belongs to the task whose code trips it.

```
Phase debug-sessions — 8 tasks across 4 waves

Wave 1
  t-1  Parse session markdown into counted sections
       files:    internal/debugsession/parse.go, internal/debugsession/parse_test.go
       covers:   c-2, c-4, c-5
       desc:     New pure package. Parse([]byte) -> Session{Status, Hypotheses,
                 FailedSinceReplan, Signals, Prevention, Problems}. State() gives
                 open | needs-replan | resolved | abandoned. It exports the heading
                 set, the markers ([failed], [replan]) and the state words, which the
                 template and the prompt reuse. The header status line is read only
                 above the first `## `. Fenced blocks (``` and ~~~) and <!-- -->
                 comments are ignored everywhere. Only column-0 list items count.
                 Session fields carry no toml/json tags.
       contract: - 3 `- [failed]` items under Fix attempts -> needs-replan; 2 -> open;
                   `[failed]`, `[FAILED]` and `[ Failed ]` all count
                 - `- [replan]` after 3 failures -> open; 3 more failures after it ->
                   needs-replan again; failures above the last [replan] never count
                 - a resolved or abandoned header with 5 failures reads resolved or
                   abandoned, never needs-replan
                 - `- [failed]` / `- x` lines inside a ``` or ~~~ fence, inside a
                   multi-line <!-- --> comment, or indented as sub-bullets are not
                   counted. A quoted diff cannot trip needs-replan or fill Resolution
                 - `status: resolved` inside a body section or fence leaves the header
                   status open; `## Fix attempts` inside a fence is not a heading
                 - an unclosed fence, a missing fixed heading, a duplicated fixed
                   heading, `### Fix attempts` (wrong level) and an unrecognised status
                   value each yield a named Problem. An unrecognised status reads open,
                   so the session never drops out of the nudges
                 - Resolution: two identical items (whitespace-normalised) count once;
                   an empty `- ` item counts zero
                 - Prevention that holds only a comment, only blanks, only an empty
                   bullet or only a fenced block reads empty; one prose line reads
                   non-empty
                 - a CRLF file gives the same Session as its LF twin
                 - TestEveryPathShapedFieldIsDeclared stays green with no new
                   not_paths.txt rows
       depends:  —

  t-2  Add debug sessions to the curated-shrink gate
       files:    internal/gate/lists.go, internal/gate/drossfiles_test.go
       covers:   c-8
       desc:     Add `debug/*.md` to Defaults().CuratedFiles, beside handoff.md.
       contract: - TestCuratedShrinkBoundary gains `debug/s.md`: a 499-byte Write over
                   1000 bytes is refused and the refusal names Edit; 500 bytes is allowed;
                   an Edit is never judged
                 - a Write creating a new `.dross/debug/x.md` (no file yet) is allowed
                 - shrinking `.dross/debug/.gitignore`, `.dross/debug/notes.txt` or
                   `.dross/debug/sub/x.md` is not claimed
                 - a gates.toml with `curated_files = []` still enforces `debug/*.md`
                   (additive union)
       depends:  —

Wave 2 (depends t-1)
  t-3  Scaffold, load and self-ignore session files
       files:    internal/debugsession/store.go, internal/debugsession/store_test.go,
                 internal/secretscan/writers.go,
                 internal/cmd/secretscan_writer_enum_test.go
       covers:   c-1, c-2
       desc:     ValidSlug: ^[a-z0-9]+(-[a-z0-9]+)*$, at most 64 chars, joined with
                 filepath.Join (no Contained unwrap).
                 Template(slug, now): t-1's headings, with HTML-comment placeholders only.
                 Create:
                   - ensures `.dross/debug/.gitignore` (`*`)
                   - inside a git work tree, verifies `git check-ignore -q -- <path>`
                     through gitrun
                   - then writes with O_EXCL
                 Load: every <valid-slug>.md with its Session and mtime, sorted by
                 mtime desc then slug.
                 Rewrite: atomic temp+rename that refuses if the bytes changed since
                 read.
                 Writer registry: MachineLocal{debug/<name>.md, Also .gitignore} under a
                 new DebugIgnoreSeed, plus its preparer in the enum test.
       contract: - Parse(Template(..)) reads open with 0/0/0 counts, empty Prevention,
                   no Problems, and all 7 headings in spec order. A template placeholder
                   can never satisfy the --fixed gate
                 - ValidSlug rejects "", "..", "a/b", "../x", ".x", "-x", "x-", "A",
                   "a.md", "a b", "é" and 65 chars. Create with each writes nothing
                   and does not create .dross/debug/
                 - a second Create of the same slug returns an "already exists" error
                   and leaves the existing file byte-identical, including when it is
                   resolved
                 - in a git repo after Create: `git status --porcelain` is empty and
                   check-ignore succeeds on the session path
                 - a pre-existing `.dross/debug/.gitignore` holding `!*` makes Create
                   refuse, naming that file, and no session is written
                 - outside a git work tree, Create succeeds and still writes the `*`
                   .gitignore
                 - the recorded check-ignore argv (gitrun.ArgvRecorder) carries `--`
                   before the path
                 - Load:
                   - with no .dross/debug it returns empty, nil
                   - it skips `.gitignore`, `.x.md.123.tmp`, `Bad Name.md`, a file named
                     with an ESC byte, and a directory `d.md/`
                   - a chmod-000 session yields an entry carrying a Problem, never an
                     error that drops its siblings
                 - Rewrite after the file changed on disk since read returns an error
                   and the newer bytes stay. A successful Rewrite keeps the file mode
                   and leaves no temp file
                 - TestMachineLocalWritersAreReallyIgnored proves debug/<name>.md and
                   debug/.gitignore are ignored via the DebugIgnoreSeed preparer.
                   TestEveryDrossWriterIsDeclared, TestEveryFileConstIsDeclared and the
                   pathfence unwrap/literal scans stay green, and debugsession imports
                   no os/exec in non-test files
       depends:  t-1

  t-4  Compute close gates, header rewrite, rule command
       files:    internal/debugsession/close.go, internal/debugsession/close_test.go
       covers:   c-5, c-6, c-7
       desc:     All pure functions.
                 FixedCloseGaps(Session) names each missing part.
                 CloseText(content, kind, reason, now):
                   - rewrites only the header status line
                   - adds a closed: line, plus a reason: line when abandoned
                 RuleAddCommand(prevention):
                   - flattens the prevention to one line (markers and comments
                     stripped)
                   - returns `dross rule add -- '<text>'` with POSIX '\'' escaping
       contract: - 1 signal + empty Prevention -> two gaps, one naming Resolution
                   ("1 of 2") and one naming Prevention. 2 signals + one prose line ->
                   no gaps. A missing `## Resolution` heading -> a gap naming the
                   heading
                 - every byte of CloseText's output from the first `## ` onward equals
                   the input. Parse(output).State() is resolved, or abandoned
                 - CloseText errors on a resolved or abandoned header, and on content
                   with no header status line
                 - a reason containing "\n## Resolution\n- x" errors (single line
                   only); an empty or whitespace reason for abandoned errors
                 - RuleAddCommand's line is run through `/bin/sh -c` with `dross`
                   shadowed by a function that prints "$@". The argv is exactly
                   [rule add -- <flattened text>] for a prevention holding `'`, `"`,
                   `$(touch pwned)`, backticks, `!`, `\`, a leading `- ` and a 3-line
                   list. No `pwned` file appears, and the flattened text has no
                   newline
       depends:  t-1

Wave 3
  t-5  Add `dross debug new|list|close` commands     (depends t-3, t-4)
       files:    internal/cmd/debug.go, internal/cmd/debug_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-2, c-4, c-5, c-6, c-7
       desc:     Cobra tree, registered in main.go.
                 - `new <slug>`: prints the path and `/dross-debug <slug>`
                 - `list`: per session, slug, state word, hypotheses,
                   failed-since-replan and last update in UTC; Problems on an
                   indented line
                 - `close <slug> --fixed | --abandoned --reason <text>`: exactly one
                   mode; prints t-4's rule line on --fixed only
                 All file I/O goes through debugsession; debug.go writes nothing
                 itself. Refusals are phrased so they classify into named telemetry
                 buckets, and they never quote session text.
       contract: - `debug new x` in a git repo, then autoCommitDrossDirt(repo, "ship"):
                   committed=false and HEAD unchanged. A session never rides
                   ship/phase-complete's chore commit
                 - `debug list` over an open, a needs-replan (3 [failed]), a resolved
                   and an abandoned session prints each slug with exactly that state
                   word and its counts. Last update equals an os.Chtimes-pinned mtime
                   rendered in UTC. An empty dir prints a no-sessions line and exits 0
                 - `close x --fixed` on a 1-signal / empty-Prevention session:
                   - exits non-zero, and the error names Resolution and Prevention
                   - the file stays byte-identical
                   - a sentinel token planted in Resolution is absent from the error
                 - `--fixed --abandoned`, neither, `--fixed --reason r` and `--abandoned`
                   with no `--reason` each refuse with the file untouched
                 - `close x --abandoned --reason "gave up"` on a 0-signal session
                   succeeds. The file still exists with an abandoned header, and no
                   rule line is printed
                 - `close x --fixed` success prints the rule line. Project rules.toml
                   and $HOME/.claude/dross/rules.toml are byte-identical (or absent)
                   afterwards. Feeding the line's sh-split argv to Rule() in a temp
                   repo stores exactly the flattened Prevention, including one that
                   starts with `- `
                 - an unknown slug errors and names `dross debug list`. Closing a
                   resolved session refuses and leaves it byte-identical
                 - telemetry.ClassifyError is never "other" for any refusal: invalid
                   slug, already exists, unknown slug, gate gaps, already closed, bad
                   flag combination
                 - TestCLITreeGolden carries the three verbs and their flags.
                   TestNarratedCommandsResolveAgainstTheTree and
                   TestEveryDrossWriterIsDeclared stay green
       depends:  t-3, t-4

  t-6  Name open sessions in status and reentry     (depends t-3)
       files:    internal/cmd/status.go, internal/cmd/reentry.go,
                 internal/cmd/status_debug_test.go
       covers:   c-2, c-3, c-4, c-6
       desc:     status prints one `debug:` line naming every open session, with
                 needs-replan labelled, and `/dross-debug <slug>`. It does this
                 whatever the spine state. reentryLine (not suggestNext) appends
                 ` · debug: <slug> <state> — /dross-debug <slug>` for the most recently
                 updated open session, plus "(+N more)", so the status footer stays
                 byte-equal. Closed sessions are skipped. Any debugsession error
                 prints nothing and never fails. The strings narrate only the slash
                 command, so this task does not depend on t-5's verbs.
       contract: - no .dross/debug, or only resolved/abandoned sessions: status has no
                   `debug:` line and TestOutputGoldens/reentry is byte-identical
                 - two open sessions, one with 3 [failed], and a runnable task in the
                   plan: exactly one status line names both, the second labelled
                   needs-replan
                 - the reentry envelope names the newer open session and
                   `/dross-debug <slug>`. With 3 open it carries "+2 more", and a
                   needs-replan session is labelled in the clause
                 - rewriting a session's header to resolved removes it from both
                 - with an open session present: status's last line equals reentry's
                   systemMessage, and systemMessage equals additionalContext
                 - `.dross/debug` as a regular file, as a chmod-000 dir, and holding a
                   session whose header is unrecognised: status and reentry exit 0
                   with the rest of the output intact. The unrecognised session is
                   still named as open
                 - `dross pause --auto`'s `- next:` line and every
                   TestReentryMatchesSuggestNext case are unchanged
       depends:  t-3

Wave 4 (depends t-5)
  t-7  Ship /dross-debug command and prompt
       files:    assets/commands/dross-debug.md, assets/prompts/debug.md,
                 docs/interaction-audit.md, internal/cmd/debug_prompt_test.go,
                 cmd/dross/main_test.go
       covers:   c-9
       desc:     Shim lists AskUserQuestion. The prompt:
                 - pre-flight runs `dross interaction show`
                 - `dross debug new|list|close`; the session is kept current with Edit
                 - one hypothesis per probe; evidence and verdict are written before
                   the next probe
                 - after each probe, the probe's own files are reverted until
                   `git status --porcelain` matches the pre-session snapshot
                 - `dross debug list` runs after every Fix-attempts entry;
                   needs-replan or any Problem means BLOCKED on line one, pair and
                   solo alike
                 - the fix goes to /dross-quick or `dross task add`
                 - signal independence is defined in three kinds, and the landed
                   commit is cited among the signals
                 - the rule is offered in pair mode and listed in the solo wrap-up
                 Audit doc gets a `### dross-debug` section. Run `make install`
                 afterwards (r-01).
       contract: - debug_prompt_test fails if the prompt drops any of these: one
                   hypothesis per probe; evidence + verdict written to the session
                   before the next probe; the tree back to the pre-session porcelain
                   snapshot after each probe; BLOCKED on line one for needs-replan in
                   pair and in solo; routing to /dross-quick (standalone) or
                   `dross task add` (mid-phase); /dross-debug never commits
                 - it fails on any fenced `git commit`, `git reset --hard`, `git clean`,
                   `git stash` or `git checkout -- .` line, if codeAdds(debug.md) is
                   non-empty, or on a fenced `dross rule add` line. `git clean -x` and
                   `stash -a` would delete the ignored session itself
                 - every marker, heading and state word the prompt names equals
                   debugsession's exported constants, so a rename in t-1 fails here
                 - it requires Edit for session updates and forbids Write over an
                   existing session
                 - it names the three signal kinds (repro gone, red-before/green-after
                   regression test, suite or CI green) and the landed-commit citation
                 - it fails if session content is allowed into a commit, PR body or
                   board issue
                 - no raw `<runtime.test_command>` line; probes run tests through
                   `dross test`
                 - new TestDebugPromptCommandsExist (cmd/dross) fails on any
                   `dross <verb> <sub>` in debug.md that the tree lacks (e.g.
                   `dross debug resume`)
                 - TestCommandsPromptsParity,
                   TestInteractionAuditEnumeratesEveryInteractiveCommand and
                   TestInteractionCoverageFailClosed are green with dross-debug
                   sectioned, not Exempt
       depends:  t-5

  t-8  Document debug sessions in README and man page
       files:    README.md, docs/dross.1, internal/cmd/readme_doc_test.go
       covers:   c-1, c-9
       desc:     README additions:
                 - a `debug/` line in the per-project artefacts block, marked gitignored
                   and machine-local
                 - `dross-debug/SKILL.md` in the install layout
                 - a `dross debug {new,list,close}` row in the commands table
                 - a `/dross-debug` row in the slash-command table
                 - a v1.7 roadmap line
                 Man page: the same in CLI COMMANDS, SLASH COMMANDS/Continuity and
                 FILES.
       contract: - TestReadmeAdvertisesOnlyRealCommands is green with the
                   `dross debug` row
                 - a new readme test fails if the slash-command table lacks
                   `/dross-debug`, or if the artefacts block's debug/ line does not say
                   gitignored
       depends:  t-5
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 `new` scaffolds the fixed template marked open, ignores first, refuses bad or duplicate slug | t-3 (template, slug, self-ignore, O_EXCL), t-5 (verb, chore-commit proof), t-8 (documented as gitignored) |
| c-2 `list` shows state + counts + last update; status adds one line, nothing when none open | t-1 (counts), t-3 (load + mtime), t-5 (list), t-6 (status line) |
| c-3 reentry names the open session + `/dross-debug <slug>` | t-6 |
| c-4 3 failed since last re-plan -> needs-replan in list and status; re-plan clears it | t-1 (threshold), t-5 (list), t-6 (status) |
| c-5 `--fixed` refuses without 2 signals + Prevention, names each missing part; `--abandoned --reason` closes without them | t-1 (counting), t-4 (gaps), t-5 (refusal, abandoned path) |
| c-6 close marks the header, keeps the file, drops it from the nudges | t-4 (header rewrite), t-5 (file stays), t-6 (dropped from status/reentry) |
| c-7 `--fixed` prints a ready-to-run `dross rule add`, never adds it | t-4 (quoting), t-5 (printed, rules untouched, cobra accepts it) |
| c-8 shrink-Write of a session refused like handoff.md | t-2 |
| c-9 /dross-debug installed, classified, prompt requirements | t-7 (shim, prompt, audit doc), t-8 (docs) |

All 9 criteria are covered.

### Failure mode -> owning task

| failure mode | owner |
|---|---|
| quoted output (fences, comments, sub-bullets) inflates failed or signal counts | t-1 |
| marker case drift silently disables the needs-replan stop | t-1 |
| re-plan reset counts failures from before the last [replan] | t-1 |
| a closed session reads needs-replan | t-1 |
| malformed file (unclosed fence, missing/duplicate heading, bad status) silently loses structure | t-1 (detects as Problem) |
| template placeholder satisfies the --fixed gate | t-3 |
| slug traversal, case collision or flag-shaped slug | t-3 |
| `new` overwrites an existing or closed session | t-3 |
| session not actually ignored (hostile/odd `.dross/debug/.gitignore`, non-git root) | t-3 |
| hand-made filenames (ESC bytes, spaces) reach hook JSON or the terminal | t-3 (Load skips them) |
| new writer undeclared / registry or pathfence guard drift | t-3 |
| lost update between the agent's Edit and close's rewrite | t-3 |
| close re-renders and corrupts the body | t-4 |
| `--reason` newline injects a fake heading | t-4 |
| shell injection in the printed rule command | t-4 |
| session rides ship/phase-complete's .dross auto-commit | t-5 |
| `close` adds the rule itself | t-5 |
| printed command breaks cobra parsing (leading `-` in Prevention) | t-5 |
| flag combinations bypass the close gate | t-5 |
| refusal text leaks session content into telemetry's `other` err_detail | t-5 |
| broken `.dross/debug` makes the SessionStart hook or status fail | t-6 |
| status footer diverges from the reentry line | t-6 |
| reentry output changes when no session is open | t-6 |
| suggestNext / pause snapshot changed as a side effect | t-6 |
| t-6 narrates `dross debug …` before t-5 registers it (wave race) | t-6 |
| shrink-Write regenerates a session from partial memory | t-2 |
| probe revert destroys user work or the session (`clean -x`, `stash -a`, `reset --hard`) | t-7 |
| /dross-debug commits a fix around /dross-quick's gates | t-7 |
| needs-replan not stopping in solo | t-7 |
| prompt names a CLI verb or marker that does not exist | t-7 |
| docs claim sessions are tracked or omit them | t-8 |

## Judgment calls

- **Ignore mechanism.** Chose a self-ignoring `.dross/debug/.gitignore`, the gatestate precedent. Rejected adding a `drossIgnoreEntries` line to the root `.gitignore`: that edits a tracked file mid-phase, staling a pass verdict and dirtying /dross-quick's baseline, and the change itself would have to ride a PR.
- **Verify the ignore.** Verify with `git check-ignore` after ensuring, and refuse if the path is not ignored. Rejected gatestate's "file exists, so trust it": a `.gitignore` the user or a repo altered would let captured output into a commit.
- **Package.** New package `internal/debugsession`: pure parser, one writer file for the registry, no clash with the stdlib's `debug`. Rejected logic in `internal/cmd`: the boundary rules and the cmd test harness make the parser harder to test in isolation.
- **Marker syntax.** Bracket tags `[failed]` / `[replan]`, case-insensitive, in Fix attempts. Rejected free-text matching ("failed" anywhere) as too fuzzy, and glyphs (✗) as untypable and easy to drift. The CLI vocabulary is only these two tags.
- **Fences and comments.** CommonMark-faithful: fences and comments are ignored everywhere, and an unclosed fence is a Problem. Rejected "fixed headings always break a fence": quoting a session file in evidence, which is likely when debugging dross itself, would split sections.
- **Malformed sessions.** A malformed session reads open with a named Problem, and the prompt treats a Problem as a stop. Rejected a fifth state, because c-2 names four. Rejected reading it as needs-replan, which conflates "file broken" with "model wrong". Failing toward visibility keeps it in the nudges.
- **failed-fix count in `list`.** Counts failures since the last [replan], the number the threshold reads. Rejected the lifetime total: a just-re-planned session would look stuck.
- **Duplicate signals.** Resolution signals are deduped on normalised text. Rejected a raw count, where pasting one signal twice clears the 2-signal gate. Judging independence stays in the prompt (locked close_gates).
- **Fenced-only Prevention.** Reads empty: everything the CLI judges ignores fences, and a rule must be stateable in prose.
- **Where the reentry clause goes.** Appended in `reentryLine`, so the status footer stays byte-equal (TestStatusEndsWithReentryLine). Rejected putting it in `suggestNext`: that changes the pause snapshot and overrides the phase's own next step.
- **How many sessions to name.** Status names every open session on one line. Reentry names the most recently updated one plus a count, keeping the hook line short.
- **Last update.** Taken from the file mtime. Rejected a header `updated:` field: the agent edits with Edit and would forget to bump it.
- **No `--json` and no `show` verb.** The spec names neither. A `show` would trip the every-show-accepts-json gate, and tagged fields would need not_paths rows.
- **`--fixed` while needs-replan.** Not refused by the CLI. c-5 lists exactly two gates, and the prompt's hard stop owns the replan rule.
- **No new tool gate.** Nothing new enforces the needs-replan stop or blocks hand-edits to the header or `git clean -x` over sessions. c-9 makes these prompt duties. A gate would block unrelated work while a stale session lingers, and handoff.md has no such guard either.
- **Symlinks.** No symlink resolution on `.dross/debug`, matching pathfence's locked lexical-only posture.
- **Slug containment.** The slug is checked by a strict regex and joined with filepath.Join. Rejected `pathfence.Contain` + `os.OpenFile(c.String())`: the unwrap ban forbids it, and O_EXCL needs OpenFile.
- **Rule command shape.** It carries a `--` separator, because a Prevention starting with `- ` would parse as a flag. The Prevention is flattened to one line, since rules are one-line MUST-FOLLOWs and the `<rules>` block renders per line.
- **Lost updates.** `Rewrite` refuses if the file changed since it was read. Rejected a plain atomic rename, which would silently drop an Edit the agent landed during a close.
- **Docs task.** Split from the prompt task (5-file cap). It sits in wave 4 because the README row must resolve against the cobra tree.
- **Out of scope.** `dross pause --auto` does not snapshot sessions, because SessionStart already fires after compaction and names them. The phase-execution version bump is workflow tooling, not a task.
