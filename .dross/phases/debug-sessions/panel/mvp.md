# Planner draft — mvp lens

```
Phase debug-sessions — 5 tasks across 2 waves

Wave 1
  t-1  Add debug session store, new and list
       files:    internal/cmd/debug.go, internal/cmd/debug_test.go,
                 internal/cmd/gitignore.go, internal/secretscan/writers.go
       desc:     New debug.go: the fixed template (`# Debug — <slug>` + `status: open` header line,
                 then ## Symptom/Hypotheses/Current theory/Next probe/Fix attempts/Resolution/Prevention
                 with `<!-- -->` guidance), a slug check (^[a-z0-9]+(-[a-z0-9]+)*$), and a parser that reads
                 only the status line, the seven headings and top-level `- ` items. It skips ``` fences,
                 counts `- failed:` items after the last `- replan:` under Fix attempts, and derives
                 needs-replan at >=3 for an open header. Also an open-session loader over .dross/debug/*.md
                 (for t-4/t-5), and `Debug()` with `new <slug>` and `list`. list prints slug, state,
                 hypotheses, failed fixes since re-plan, and mtime as last update.
                 `new` calls ensureDrossGitignore BEFORE the write. `.dross/debug/` joins
                 drossIgnoreEntries, and writers.go declares debug.go MachineLocal under the existing seed.
                 Not registered yet, so no "`dross debug" narration in t-1 string literals.
       covers:   c-1, c-2, c-4
       contract: - new in a fresh git repo whose .gitignore lacks the line → `git check-ignore -q
                   .dross/debug/<slug>.md` exits 0; with .gitignore made a directory (ensure fails),
                   new errors and .dross/debug/<slug>.md does not exist (ignore-before-write order)
                 - created file's `## ` headings equal the seven names in order; status line reads `open`
                 - new with "../x", "Foo", "a/b", "", "-x" each errors and leaves .dross/debug/ empty;
                   a second new on an existing slug (open or resolved) errors and the file is byte-identical
                 - parser: 3 `- failed:` → needs-replan; 3 failed + `- replan:` → open; failed,replan,
                   failed,failed → open; resolved header with 3 failed → resolved; 3 `- failed:` lines
                   inside a ``` fence → open; 2 top-level hypotheses + 3 indented sub-bullets → count 2
                 - list over open, needs-replan, resolved and abandoned fixtures prints one row per
                   session with that state, and the hypothesis/failed counts. A session os.Chtimes'd to
                   2026-01-02 03:04 UTC shows that timestamp.
                 - TestMachineLocalWritersAreReallyIgnored / TestEveryDrossWriterIsDeclared fail if the
                   gitignore entry or the writers.go declaration is dropped

  t-2  Add debug sessions to curated-shrink gate
       files:    internal/gate/lists.go, internal/gate/drossfiles_test.go
       desc:     Append "debug/*.md" to Defaults().CuratedFiles; add "debug/s.md" to
                 TestCuratedShrinkBoundary's rel list.
       covers:   c-8
       contract: - a 499-byte Write over a 1000-byte .dross/debug/s.md is refused with text pointing at
                   Edit; a 500-byte Write is allowed; an Edit is not judged
                 - a Write creating a not-yet-existing .dross/debug/new.md is allowed (scaffold path)

Wave 2 (depends t-1)
  t-3  Ship /dross-debug command and classify it
       files:    assets/commands/dross-debug.md, assets/prompts/debug.md, docs/interaction-audit.md,
                 docs/subagent-offload-audit.md, internal/cmd/debug_prompt_test.go
       desc:     Shim (Read/Write/Edit/Bash/AskUserQuestion) + prompt. Pre-flight runs
                 `dross rule show` + `dross interaction show`. Then new-or-resume via
                 `dross debug new|list`, one hypothesis per probe, evidence + verdict Edit-written before the
                 next probe, and the tree restored after each probe. needs-replan is a hard stop with
                 BLOCKED on line one, pair and solo alike. The fix routes to /dross-quick or
                 `dross task add` and is never committed here. Close defines independent signals; the rule
                 add is offered in pair and listed in the solo wrap-up. Ends with the clear-point footer
                 naming `/dross-debug <slug>`. Adds a `### dross-debug` decision-point table and a
                 `### debug` inline-only section.
       covers:   c-9
       contract: - one subtest per c-9 clause (single hypothesis per probe; evidence+verdict written
                   before next probe; tree restored after each probe; needs-replan hard stop with
                   "BLOCKED" in pair and solo; fix routed to /dross-quick or dross task add; never commits)
                   — deleting any one clause from debug.md fails that subtest by name
                 - every heading and every Fix-attempts marker token the t-1 parser reads (its consts)
                   appears verbatim in debug.md; renaming `failed:` in the parser without the prompt fails
                 - debug.md contains no fenced `git add`/`git commit` line
                 - prompt names the two locked close rules: independence = signals of different kinds;
                   the rule add is offered in pair and listed in the solo wrap-up
                 - TestInteractionAuditEnumeratesEveryInteractiveCommand, TestInteractionCoverageFailClosed,
                   TestSubagentOffloadAuditCoversEveryPrompt, TestFooterCoverageFailClosed and
                   TestCommandsPromptsParity each fail naming debug if its section/footer/shim is removed

  t-4  Add debug close and register command
       files:    internal/cmd/debug.go, internal/cmd/debug_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `debug close <slug>` with exactly one of --fixed | --abandoned (--reason required with
                 --abandoned). --fixed counts top-level Resolution items (>=2) and non-comment Prevention
                 lines (>=1), refusing with every missing part named and no write. Close rewrites only the
                 header status line (resolved | `abandoned — <reason>`) and never deletes. --fixed prints
                 `dross rule add '<prevention>'`: lines joined, markers stripped, POSIX single-quoted.
                 Register cmd.Debug() in newRoot and re-mint cli_tree.txt (DROSS_UPDATE_GOLDEN=1).
       covers:   c-5, c-6, c-7
       contract: - --fixed with 1 signal + empty Prevention errors naming both "Resolution" and
                   "Prevention", and the file is byte-identical; 2 signals + empty Prevention names only
                   Prevention; 2 signals + Prevention succeeds
                 - --abandoned --reason "dup" succeeds on empty sections; --abandoned without --reason,
                   both flags, or neither flag each refuse
                 - after either close the file still exists, every byte except the status line is
                   unchanged, and t-1's open-session loader no longer returns the slug
                 - printed rule-add line fed to `sh -c 'set -- <args>; printf %s "$3"'` reproduces a
                   Prevention containing an apostrophe and `$HOME` verbatim; .dross/rules.toml is
                   byte-identical after close (never adds the rule)
                 - TestCLITreeGolden fails if `debug close --reason` or the debug group is dropped;
                   TestNarratedCommandsResolveAgainstTheTree fails if registration is removed while the
                   printed `dross rule add` / any `dross debug` narration remains

  t-5  Surface open sessions in status and reentry
       files:    internal/cmd/status.go, internal/cmd/reentry.go, internal/cmd/status_test.go,
                 internal/cmd/reentry_test.go
       desc:     status gains one `debug:` line (beside handoff:) naming each non-closed session with its
                 state (open | needs-replan) and `/dross-debug <slug>`, and prints nothing when none are
                 open. reentryLine (shared by status's footer and the hook envelope) gains a
                 `· debug: <slug> (<state>) — /dross-debug <slug>` suffix for the most recently updated
                 open session (+N more). A malformed session file never makes status/reentry error.
       covers:   c-2, c-3, c-4, c-6
       contract: - open session "auth-flake" → status has a `debug:` line naming auth-flake; the decoded
                   reentry line contains "/dross-debug auth-flake"
                 - only resolved + abandoned sessions (or no debug dir) → no `debug:` line in status and no
                   "/dross-debug" in the reentry line
                 - fixture with 3 `- failed:` after the last re-plan → status line reads needs-replan;
                   appending `- replan:` makes it read open
                 - two open sessions with different mtimes → reentry names the newer slug
                 - with an open session present, status's last line is still byte-equal to the reentry
                   envelope line (TestStatusEndsWithReentryLine extended with that case)
                 - a .dross/debug/x.md with no status line → status and reentry exit 0
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-1 (`debug list`), t-5 (`dross status` line) |
| c-3 | t-5 |
| c-4 | t-1 (derivation + list), t-5 (status) |
| c-5 | t-4 |
| c-6 | t-4 (header marked, file kept, loader drops it), t-5 (status/reentry nudges drop it) |
| c-7 | t-4 |
| c-8 | t-2 |
| c-9 | t-3 |

All 9/9 criteria covered.

## Judgment calls

- **Session code lives in internal/cmd/debug.go, not a new internal/debug package.** Every caller (list, close, status, reentry) is in internal/cmd, so a package would add a layer with no second consumer.
- **`.dross/debug/` joins drossIgnoreEntries and `new` reuses ensureDrossGitignore** instead of a bespoke single-entry ensure. This lets writers.go declare MachineLocal under the existing seed, and TestMachineLocalWritersAreReallyIgnored proves that declaration. init/onboard also seed the line at no extra cost. Side effect: `new` also appends any other missing seed line (StrykerOutput/ in this repo), which is the same contract init/onboard already have.
- **Registration and the golden re-mint go in t-4, not t-1.** Doing it in t-1 makes t-1 six files and re-mints the golden twice. The cost is that t-1 must not narrate "`dross debug" in literals until t-4 lands.
- **The debug suffix goes in the shared reentryLine.** I rejected appending it only in Reentry(), because that breaks the locked status-footer ↔ hook byte-equality (TestStatusEndsWithReentryLine). status also gets its own `debug:` line, as c-2 asks.
- **needs-replan is derived on read and never written to the header.** The CLI writes the status line only on new and close. Persisting it would need a writer on a read path.
- **The failed-fix count in list is "since the last re-plan", not the lifetime total.** That is the number c-4's threshold reads, so the column explains the state. A judge who reads c-2 literally may want the total. It is a one-line change.
- **Last update is the file mtime.** The locked session_format bars the CLI from reading header fields other than status.
- **t-3 waits on t-1 instead of running in wave 1.** Its test asserts that the parser's marker tokens and headings appear verbatim in debug.md. That catches drift between what the prompt tells the agent to write and what the CLI counts, which hardcoded needles in wave 1 would not.
- **t-3 is five files despite the 5-file cap.** The parity, interaction-coverage, offload-audit and footer gates are fail-closed. Any split leaves an intermediate red tree.
- **The prompt carries the clear-point footer rather than a footer-audit.md Exempt row.** With write-as-you-go, every recorded probe is a durable boundary. A footer needs no doc edit, so footer-audit.md is untouched.
- **c-8 is its own 2-file task, though it is "too small".** Merging it into any other task breaks the 5-file cap. It sits in a different package (gate) and needs nothing from t-1.
- **Abandon reason goes on the header status line, not in Resolution.** A `- ` item under Resolution would count as a fixed-close signal.
- **The --fixed refusal names every missing part in one error,** not just the first one hit. c-5 says "naming each missing part".
- **The parser ignores ``` fences.** Evidence quotes command output, and an output line starting with `- failed:` must not trip needs-replan.
- **No README task.** No criterion asks for one and no test pins it, so the mvp lens drops it. The user's README-sync convention would add a `dross debug` row and a `/dross-debug` row. The judge can fold those into t-4 and t-3 at the cost of the file cap.
