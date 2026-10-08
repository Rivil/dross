# debug-sessions — panel synthesis

Drafts: `risk.md` (8 tasks / 4 waves), `mvp.md` (5 / 2), `verification.md` (6 / 4).
Repo facts that decided the scoring were checked against source. Each one is cited where it is used.

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | 5 — 9/9 with layered owners (parser → store → CLI → nudges → prompt → docs) and a failure-mode→owner table | 3 — 9/9 on paper, but c-1's ignore goes into the tracked root `.gitignore`, which dirties the tree in this repo (see D-1), and there is no docs task | 5 — 9/9, every criterion mapped to a named contract that proves it |
| Test-contract specificity | 5 — adversarial at every boundary: fence/comment/CRLF variants, real-`/bin/sh` injection oracle, lost-update Rewrite, telemetry bucket, chore-commit ride, hostile `.gitignore` | 3 — concrete happy/edge rows but thin on hostile input (quoting probe is `'` + `$HOME` only, no malformed-file matrix) | 4 — "fails if X" rows aimed at mutants (off-by-one replan table, section scope, template↔parser drift round-trip), fewer hostile inputs |
| Granularity | 4 — 8 tasks, ≤5 files, pure parser/close split from I/O; **but t-7 is under-scoped**: it omits `docs/subagent-offload-audit.md` and the clear-point footer, so `TestSubagentOffloadAuditCoversEveryPrompt` and `TestFooterCoverageFailClosed` are red at its commit | 2 — t-1 spans parser + fs store + cobra (3 layers); adding `.dross/debug/` to `drossIgnoreEntries` turns `TestEnsureDrossGitignoreRespectsBroaderPattern` red (fixtures 2–4 no longer "already covering"), and `gitignore_test.go` is not in t-1's files | 4 — 6 tasks, green at every commit; t-6 at 7 files is justified for the audit gates but also bundles README |
| Wave correctness | 5 — t-6 correctly needs only the store (it narrates `/dross-debug`, which the narration regex `` `dross `` does not match); prompt waits for verbs. Nit: t-8 documents `/dross-debug` in parallel with t-7, which creates it | 3 — prompt t-3 runs parallel to t-4, which adds `close`, so the prompt narrates a verb that may not exist at its commit, and nothing resolves prompt verbs to catch it | 5 — nudges after store, prompt after verbs, no hidden edges |

**Skeleton: risk.** It has the sharpest contracts and the finest task boundaries, and every failure mode has exactly one owner. Its one real defect is t-7 missing two fail-closed audit inputs. The fix is mechanical, grafted from mvp + verification.

## Merged plan

```
Phase debug-sessions — 8 tasks across 5 waves

Wave 1
  t-1  Parse session markdown into counted sections                [risk+verification]
       files:    internal/debugsession/parse.go, internal/debugsession/parse_test.go
       covers:   c-2, c-4, c-5
       desc:     New pure package. Parse([]byte) -> Session{Status, Hypotheses, FailedSinceReplan,
                 Signals, Prevention, Problems}; State() -> open | needs-replan | resolved | abandoned.
                 Exports headings, markers ([failed], [replan]) and state words for template + prompt.
                 Only column-0 list items outside ```/~~~ fences and <!-- --> count; status read above first `## `.
       contract: - REPLAN TABLE [verification+risk]: failed×2 -> open; ×3 -> needs-replan; ×3,[replan] -> open;
                   ×3,[replan],×2 -> open; ×3,[replan],×3 -> needs-replan; failures above the last [replan]
                   never count; `[failed]`, `[FAILED]`, `[ Failed ]` all count
                 - SECTION SCOPE [verification]: 3 `- [failed]` under ## Hypotheses -> open
                 - CLOSED WINS: resolved/abandoned header with 5 failures reads resolved/abandoned
                 - IMMUNITY: items inside ``` / ~~~, inside a multi-line <!-- -->, or indented sub-bullets are
                   not counted (1 hypothesis + 2 sub-bullets = 1); `## Fix attempts` inside a fence is not a
                   heading; `status: resolved` in a body section or fence leaves the header open
                 - PROBLEMS: unclosed fence, missing / duplicated fixed heading, `### Fix attempts`, and an
                   unrecognised status value each yield a named Problem; unrecognised status reads open (D-3)
                 - Resolution: identical items (whitespace-normalised) count once (D-8); empty `- ` counts 0.
                   Prevention of only a comment / blanks / empty bullet / fenced block reads empty; one prose
                   line reads non-empty
                 - a CRLF file parses identically to its LF twin
                 - Session has no toml/json tags: TestEveryPathShapedFieldIsDeclared green, no not_paths rows;
                   coverfloor (>=50% own-package, CI) holds for parse.go [verification]
       depends:  —

  t-2  Add debug sessions to the curated-shrink gate                [risk+mvp+verification]
       files:    internal/gate/lists.go, internal/gate/drossfiles_test.go
       covers:   c-8
       desc:     Add `debug/*.md` to Defaults().CuratedFiles beside handoff.md; extend
                 TestCuratedShrinkBoundary with `debug/s.md` plus scope probes.
       contract: - 499-byte Write over 1000-byte .dross/debug/s.md refused, refusal names Edit; 500 allowed;
                   an Edit never judged; dropping the pattern turns these rows red
                 - a Write creating a not-yet-existing .dross/debug/x.md is allowed
                 - shrinking .dross/debug/.gitignore, .dross/debug/notes.txt or .dross/debug/sub/x.md is not
                   claimed (a wider `debug/*` fails this; path.Match `*` does not cross `/`)
                 - gates.toml `curated_files = []` still enforces `debug/*.md` (additive union)
       depends:  —

Wave 2 (depends t-1)
  t-3  Scaffold, load and self-ignore session files                 [risk+verification]
       files:    internal/debugsession/store.go, internal/debugsession/store_test.go,
                 internal/secretscan/writers.go, internal/cmd/secretscan_writer_enum_test.go
       covers:   c-1, c-2
       desc:     ValidSlug (^[a-z0-9]+(-[a-z0-9]+)*$, <=64) + Template(slug, now) from t-1's headings.
                 Create ensures `.dross/debug/.gitignore` (`*`), verifies `git check-ignore -q --` via gitrun
                 in a work tree, then O_EXCL-writes. Load (newest mtime first), Rewrite (atomic, refuses if
                 changed since read). MachineLocal under new DebugIgnoreSeed + preparer.
       contract: - TEMPLATE ROUND-TRIP [verification+risk]: Parse(Template) reads open, 0/0/0, empty
                   Prevention, no Problems, 7 headings in spec order; appending 3 `- [failed]` under the
                   template's OWN `## Fix attempts` reads needs-replan (kills heading drift); no placeholder
                   satisfies the --fixed gate
                 - SLUG TABLE: accepts flaky-login, x1; rejects "", "..", "a/b", "../x", ".x", "-x", "x-", "A",
                   "a.md", "a b", "é", 65 chars — each writes nothing and does not create .dross/debug/
                 - DUPLICATE: second Create -> "already exists" naming the slug; existing file byte-identical,
                   including when resolved
                 - IGNORED BEFORE WRITE: in a git repo after Create, `git status --porcelain` empty and
                   check-ignore succeeds; `.gitignore` pre-created as a DIRECTORY -> error, no <slug>.md
                   [verification]; pre-existing `.gitignore` holding `!*` -> refuse naming it, no session [risk];
                   outside a work tree -> succeeds, still writes `*`; recorded argv (gitrun.ArgvRecorder) has `--`
                 - LOAD: no dir -> empty, nil; newest-mtime first; skips `.gitignore`, `.x.md.123.tmp`,
                   `Bad Name.md`, `notes.txt`, an ESC-byte name, dir `d.md/`; chmod-000 session -> entry with a
                   Problem, siblings kept
                 - REWRITE: file changed on disk since read -> error, newer bytes stay; success keeps mode,
                   leaves no temp file
                 - REGISTRY: TestMachineLocalWritersAreReallyIgnored proves debug/<name>.md and debug/.gitignore
                   via the DebugIgnoreSeed preparer; TestEveryDrossWriterIsDeclared, TestEveryFileConstIsDeclared,
                   pathfence unwrap + `..`-literal scans green; no os/exec in non-test debugsession files;
                   coverfloor holds for store.go
       depends:  t-1

  t-4  Compute close gates, header rewrite, rule command            [risk+verification+mvp]
       files:    internal/debugsession/close.go, internal/debugsession/close_test.go
       covers:   c-5, c-6, c-7
       desc:     Pure. FixedCloseGaps names every missing part at once; CloseText rewrites only the header
                 status line and adds `closed:` (+ `reason:` when abandoned) header lines; RuleAddCommand
                 flattens Prevention to one line and returns `dross rule add -- '<text>'` (POSIX '\'' escaping).
       contract: - GAP TABLE [verification+mvp+risk]: (1 signal, empty Prevention) -> two gaps, Resolution
                   ("1 of 2") + Prevention; (1, prose) -> Resolution only; (2, empty) -> Prevention only;
                   (2, Prevention = template comment / whitespace) -> Prevention; (2, prose) -> none; missing
                   `## Resolution` -> gap naming the heading; signals inside a fence do not count
                 - HEADER-ONLY: every byte from the first `## ` on equals the input; Parse(out).State() is
                   resolved / abandoned; errors on an already-closed header and on no status line
                 - REASON: a reason containing "\n## Resolution\n- x" errors; empty / whitespace abandon reason errors
                 - SHELL ORACLE: the line run under real `/bin/sh -c` with `dross` shadowed by a printf function
                   yields argv exactly [rule add -- <flattened>] for a Prevention holding ', ", $(touch pwned),
                   backticks, !, \, a leading `- ` and a 3-line list; no `pwned` file; no newline in the text
                 - coverfloor holds for close.go
       depends:  t-1

Wave 3
  t-5  Add `dross debug new|list|close` commands                    [risk+verification+mvp]   (depends t-3, t-4)
       files:    internal/cmd/debug.go, internal/cmd/debug_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-2, c-4, c-5, c-6, c-7
       desc:     Thin cobra layer over debugsession (writes nothing itself), registered in newRoot, golden
                 re-minted. new prints path + `/dross-debug <slug>`; list prints slug, state, hypotheses,
                 failed-since-replan (D-7), UTC mtime, indented Problems; close prints the rule line on --fixed only.
       contract: - NEW: in a temp dross+git repo creates 7 headings + `status: open`; second `new` errors;
                   `new ../x` errors with nothing written outside .dross/debug
                 - NO CHORE RIDE [risk]: after `debug new x`, autoCommitDrossDirt(repo, "ship") -> committed=false,
                   HEAD unchanged
                 - LIST: open / needs-replan (3 [failed]) / resolved / abandoned fixtures print exactly that state
                   word + counts; os.Chtimes-pinned mtime rendered UTC; swapping hypothesis/failed columns fails
                   [verification]; empty dir -> no-sessions line, exit 0
                 - FIXED REFUSAL: 1 signal + empty Prevention -> non-zero naming Resolution and Prevention, file
                   byte-identical, a sentinel planted in Resolution absent from the error
                 - FLAG MATRIX: `--fixed --abandoned`, neither, `--fixed --reason r`, `--abandoned` w/o `--reason`
                   each refuse naming the working form [verification], file untouched
                 - ABANDON: `--abandoned --reason "gave up"` on a 0-signal session succeeds; file exists with
                   abandoned header; no rule line printed
                 - PRINTS, NEVER ADDS: --fixed success prints exactly one `dross rule add ` line; project rules.toml
                   and $HOME/.claude/dross/rules.toml byte-identical (or absent); the line's sh-split argv fed to
                   Rule() in a temp repo stores exactly the flattened Prevention, incl. one starting `- `
                 - unknown slug errors naming `dross debug list`; closing a resolved session refuses, untouched
                 - TELEMETRY [risk]: telemetry.ClassifyError is never "other" for invalid slug, already exists,
                   unknown slug, gate gaps, already closed, bad flag combination
                 - SURFACE: TestCLITreeGolden carries the three verbs + flags;
                   TestNarratedCommandsResolveAgainstTheTree and TestEveryDrossWriterIsDeclared green
       depends:  t-3, t-4

  t-6  Name open sessions in status and reentry                     [risk+verification+mvp]   (depends t-3, t-4)
       files:    internal/cmd/status.go, internal/cmd/reentry.go, internal/cmd/status_debug_test.go
       covers:   c-2, c-3, c-4, c-6
       desc:     status prints one `debug:` line naming every open session (needs-replan labelled) with
                 `/dross-debug <slug>`; reentryLine (not suggestNext) appends ` · debug: <slug> <state> —
                 /dross-debug <slug>` for the newest open session + "(+N more)". Narrates only the slash
                 command (no t-5 dependency); any debugsession error prints nothing and never fails.
       contract: - NOTHING: no dir, or only resolved/abandoned -> no `debug:` line, status byte-identical to
                   the no-dir run, TestOutputGoldens/reentry unchanged
                 - ONE LINE: two open sessions (one with 3 [failed]) + a runnable plan task -> exactly one line
                   naming both, the second labelled needs-replan; appending `- [replan]` makes it read open
                 - REENTRY: envelope names the newer open session + `/dross-debug <slug>`; 3 open -> "+2 more";
                   needs-replan labelled in the clause
                 - CLOSE DROPS [verification]: after CloseText + Rewrite (abandoned) neither status nor reentry
                   names the slug; the file still exists
                 - BYTE-EQUAL: with an open session, status's last line == systemMessage == additionalContext
                 - HOOK SAFETY: `.dross/debug` as a regular file, as a chmod-000 dir, and a session with its
                   status line deleted / unrecognised -> status + reentry exit 0, rest intact, session still named
                 - `dross pause --auto`'s `- next:` line and every TestReentryMatchesSuggestNext case unchanged
       depends:  t-3, t-4

Wave 4 (depends t-5)
  t-7  Ship /dross-debug command and prompt                         [risk+mvp+verification]
       files:    assets/commands/dross-debug.md, assets/prompts/debug.md, docs/interaction-audit.md,
                 docs/subagent-offload-audit.md, internal/cmd/debug_prompt_test.go, cmd/dross/main_test.go
       covers:   c-9
       desc:     Interactive shim (AskUserQuestion) + prompt: pre-flight `dross rule show` [mvp] + `dross
                 interaction show`; the c-9 probe loop, BLOCKED hard stop, fix routing, independence and rule
                 offer; clear-point footer to `/dross-debug <slug>` [mvp+verification]. Adds `### dross-debug` (✅)
                 and `### debug` offload section [mvp+verification]. `make install` afterwards (r-01).
       contract: - PER-REQUIREMENT SUBTESTS, each failing by name when its anchor is deleted [verification+risk]:
                   one hypothesis per probe; evidence + verdict written to the session before the next probe
                   (located in the probe-loop section); tree back to the pre-session `git status --porcelain`
                   snapshot after each probe; `dross debug list` after every Fix-attempts entry, needs-replan
                   or any Problem -> BLOCKED on line one in pair AND solo; fix routed to /dross-quick
                   (standalone) or `dross task add` (mid-phase); /dross-debug never commits
                 - NO COMMIT / DESTRUCTIVE PATH [risk]: codeAdds(debug.md) empty; no fenced `git commit`,
                   `git reset --hard`, `git clean`, `git stash`, `git checkout -- .` or `dross rule add` line
                 - PARITY: every marker, heading and state word the prompt names equals debugsession's exported
                   constants [risk+mvp]
                 - requires Edit for session updates, forbids Write over an existing session
                 - INDEPENDENCE: names repro gone, red-before/green-after regression test, suite or CI green, and
                   the landed-commit citation; rule offered in pair, listed in solo wrap-up
                 - forbids session content in a commit, PR body or board issue
                 - no raw `<runtime.test_command>` line; probes run tests through `dross test`
                 - RESOLVES [risk]: new TestDebugPromptCommandsExist (cmd/dross) fails on any `dross <verb> <sub>`
                   in debug.md the tree lacks (e.g. `dross debug resume`)
                 - CLASSIFIED [verification]: prompt carries "interaction playbook" + `dross interaction show`;
                   audit section ✅ with no ⬜/🟡/❌; promptFooterState(root,"debug") == footerPresent
                 - green: TestCommandsPromptsParity, TestInteractionAuditEnumeratesEveryInteractiveCommand,
                   TestInteractionCoverageFailClosed, TestSubagentOffloadAuditCoversEveryPrompt,
                   TestFooterCoverageFailClosed, assets TestEmbedDrift
       depends:  t-5
       note:     6 files. risk's 5-file version is red: offloadAuditGaps requires `### debug` in
                 subagent-offload-audit.md for every prompt, and footerCoverage requires the footer (or an Exempt
                 row) for every command-backed prompt. The four non-test files must land together.

Wave 5 (depends t-7)
  t-8  Document debug sessions in README, man page, footer audit    [risk+verification]
       files:    README.md, docs/dross.1, internal/cmd/readme_doc_test.go, docs/footer-audit.md
       covers:   c-1, c-9
       desc:     README: `debug/` artefacts line (gitignored, machine-local), `dross-debug/SKILL.md` in install
                 layout, `dross debug {new,list,close}` row, `/dross-debug` row, v1.7 roadmap line. Man page:
                 CLI COMMANDS, SLASH COMMANDS/Continuity, FILES. footer-audit.md Footer-bearing row [verification].
       contract: - TestReadmeAdvertisesOnlyRealCommands green with the `dross debug` row
                 - new readme test fails if the slash-command table lacks `/dross-debug`, or if the artefacts
                   block's debug/ line does not say gitignored
       depends:  t-5, t-7
       note:     risk had this parallel to t-7 (depends t-5 only). Moved after t-7: every row it adds describes
                 what t-7 ships, and the README guard checks only `dross <cmd>` over-claims, so a slash command
                 advertised before it exists would pass silently.
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3 (template, slug, self-ignore + check-ignore, O_EXCL), t-5 (verb e2e, no chore ride), t-8 (documented gitignored) |
| c-2 | t-1 (counts), t-3 (load + mtime), t-5 (list), t-6 (status line) |
| c-3 | t-6 |
| c-4 | t-1 (threshold), t-5 (list), t-6 (status) |
| c-5 | t-1 (counting), t-4 (gaps), t-5 (refusal + abandoned path) |
| c-6 | t-4 (header rewrite), t-5 (file stays), t-6 (dropped from nudges) |
| c-7 | t-4 (quoting oracle), t-5 (printed, rules untouched, Rule() accepts it) |
| c-8 | t-2 |
| c-9 | t-7 (shim, prompt, both audits, footer), t-8 (docs) |

9/9 covered.

## Disagreements

Ordered by how much the choice matters.

### D-1 — How `.dross/debug/` gets ignored
- **risk + verification:** a self-ignoring `.dross/debug/.gitignore` (`*`), following the gatestate precedent (`.dross/gate/.gitignore`). risk also verifies with `git check-ignore` and refuses on a hostile file (`!*`).
- **mvp:** append `.dross/debug/` to `drossIgnoreEntries` and have `new` call `ensureDrossGitignore` on the root `.gitignore`, reusing the existing seed.
- **Default:** self-ignore, with risk's check-ignore verification.
- **Why it matters:** the mvp route fails on two counts:
  - It is red as drafted. `TestEnsureDrossGitignoreRespectsBroaderPattern` fixtures 2–4 stop being "already covering", and `gitignore_test.go` is not in mvp t-1's files.
  - It dirties a tracked file. This repo's root `.gitignore` lacks both `.dross/debug/` and `StrykerOutput/`, so the first `dross debug new` would append both. That breaks the clean baseline /dross-quick needs (locked `fix_path`) and stales a pass verdict.

  The remaining choice inside the default is check-ignore (one gitrun spawn per `new`) versus trusting the file, as verification and gatestate do.

### D-2 — Where session logic lives
- **risk:** new pure package `internal/debugsession`.
- **verification:** the same structure, named `internal/debug`.
- **mvp:** everything in `internal/cmd/debug.go`, on the grounds that every consumer is in cmd.
- **Default:** `internal/debugsession`.
- **Why it matters:** coverfloor applies to `internal/*` except `internal/cmd`, and gremlins runs per package. A pure package gets its parser mutants killed without the ~600s `internal/cmd` suite, which is the gate on every phase's verify leg. mvp's "no second consumer" point is true but costs that. The name (`debug` vs `debugsession`) is cosmetic: nothing imports `runtime/debug` today.

### D-3 — What a malformed session reads as
- **verification:** a fifth state, `malformed`, shown in `list` and nudged as open.
- **risk:** no fifth state. It reads `open` with a named Problem, and the prompt treats any Problem as BLOCKED.
- **mvp:** only "never makes status/reentry error".
- **Default:** risk.
- **Why it matters:** c-2 enumerates exactly four states. The choice also decides whether a mangled file (unclosed fence, renamed heading) halts /dross-debug or lets it keep probing against counts it can no longer trust.

### D-4 — Fix-attempt marker syntax
- **risk:** `- [failed]` / `- [replan]`, case-insensitive.
- **mvp:** `- failed:` / `- replan:`.
- **verification:** `- failed:` / `- re-plan:`.
- **Default:** risk.
- **Why it matters:** this is the durable on-disk format the agent types mid-investigation, and a marker it mistypes silently disables the needs-replan hard stop. All three pin prompt↔parser constant parity, so whichever wins is enforced. Changing it after sessions exist strands old files.

### D-5 — README/docs, and the prompt task's size
- **mvp:** no docs task. No gate requires it, though mvp itself notes the user's README-sync convention would.
- **verification:** README rows inside the prompt task, making it 7 files.
- **risk:** a separate docs task (README + man page + readme test).
- **Default:** risk's separate t-8, moved after t-7 and carrying verification's footer-audit row. t-7 lands at 6 files: shim + prompt + 2 audits are inseparable for green, plus 2 test files.
- **Why it matters:** dropping docs stays green, since `TestReadmeAdvertisesOnlyRealCommands` checks over-claims only, but it breaks the README-sync convention. Bundling inflates the one task that is already over plan.md's 5-file guideline.

### D-6 — Wave depth: prompt alongside the verbs or after them
- **mvp:** 2 waves. The prompt runs parallel to `close` and the nudges.
- **risk + verification:** the prompt waits for the registered verbs (wave 4).
- **Default:** after the verbs, giving 5 waves with the D-5 move.
- **Why it matters:** the prompt narrates `dross debug close`, and mvp has no prompt-verb resolution test, so it can commit a prompt naming a verb that does not yet exist. risk's `TestDebugPromptCommandsExist` cannot be green before t-5. The cost is serial depth in a solo run (5 waves vs 2).

### D-7 — The failed-fix count `list` shows
- **risk + mvp:** count failures since the last re-plan (the number the threshold reads).
- **verification:** the lifetime total. The state column already carries the threshold, and the total keeps history visible after a re-plan.
- **Default:** since the last re-plan.
- **Why it matters:** c-2 says only "failed-fix count". Under since-re-plan, a just-re-planned session reads 0 and hides prior failures; under the total, it reads 3 and looks stuck while showing `open`. Either is a one-line change.

### D-8 — Dedupe identical Resolution signals
- **risk:** whitespace-normalised duplicates count once, so pasting one signal twice cannot clear the 2-signal gate.
- **mvp + verification:** count raw top-level items.
- **Default:** dedupe (risk).
- **Why it matters:** it tightens how the CLI counts under locked `close_gates`, which leaves independence to the prompt. It is arguably within "the CLI counts them", but it is a user-visible refusal the locked text does not name.
