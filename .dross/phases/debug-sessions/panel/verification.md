# Panel draft — verification lens

Lens: each criterion's ideal test contract was written first, and each task is the
smallest change that makes those contracts satisfiable. Every task also names the
existing fail-closed gate that turns red if the task is skipped or half-done.
These are tests that already exist in the repo and that a new command, writer or
prompt trips automatically.

```
Phase debug-sessions — 6 tasks across 4 waves

Wave 1
  t-1  Model debug session markdown (pure)
       files:    internal/debug/session.go, internal/debug/session_test.go
       covers:   c-1, c-2, c-4, c-5, c-7
       desc:     New package internal/debug: the fixed heading list (one source for template + parser),
                 Template(slug, now), ValidSlug, Parse(body) → Session{status, hypotheses, failed,
                 failedSinceReplan, resolutionSignals, prevention}, State() (open | needs-replan |
                 resolved | abandoned | malformed), CloseGaps() and RuleCommand(prevention).
       contract: - TEMPLATE ROUND-TRIP: Parse(Template("x")) reads open / 0 hypotheses / 0 failed /
                   0 signals / empty Prevention. Three `- failed:` items appended under the
                   template's OWN "## Fix attempts" heading must read needs-replan. If the template
                   and parser heading spellings drift apart (e.g. "## Fix Attempts"), the appended
                   items go uncounted and the test fails.
                 - REPLAN TABLE (c-4): failed×2 → open; failed×3 → needs-replan; failed×3,re-plan →
                   open; failed×3,re-plan,failed×2 → open; failed×3,re-plan,failed×3 → needs-replan.
                   An off-by-one threshold (>3 or >=2) or a "since" counter that never resets fails a
                   named row.
                 - SECTION SCOPE: three `- failed:` items under ## Hypotheses → open. A counter that
                   greps the whole file fails.
                 - FENCE IMMUNITY: three `- failed:` lines inside a ``` block under ## Fix attempts →
                   open. A `## Resolution` line inside a fence does not switch section. Evidence quotes
                   command output (locked session_format), so a line-grep parser fails here.
                 - NESTING: one hypothesis with two indented evidence sub-bullets counts as 1.
                   Indented `  - failed:` does not count as an attempt.
                 - CLOSED WINS: header `status: resolved` (or `abandoned`) with failed×3 reads resolved
                   (or abandoned), not needs-replan.
                 - MALFORMED: a body with no `status:` header line, or an unknown status value, reads
                   malformed, never open/resolved by default.
                 - SLUG TABLE (c-1): accepts flaky-login, x1. Refuses "", -x, x-, A, "a b", ../x, a/b,
                   x.md and a 65-char slug. Dropping the regex, or relying on pathfence.Segment alone,
                   lets "a b" or "A" through and fails the test.
                 - CLOSE GAPS (c-5): (0 signals, empty Prevention) → gaps name BOTH "Resolution" and
                   "Prevention". (1, text) → only Resolution, with "1"/"2". (2, empty) → only
                   Prevention. (2, Prevention holding only the template's <!-- guidance --> comment or
                   whitespace) → Prevention. (2, text) → no gaps. Signals inside a fence don't count.
                 - RULE COMMAND (c-7): RuleCommand(p), with `dross` swapped for a printf-NUL shell
                   function under real /bin/sh -c, yields exactly ["rule","add",<p collapsed to one
                   line, list markers stripped>]. The input p contains ', $(touch pwned), a backtick
                   and a newline. The test fails if quoting breaks or if a substitution executes
                   (pwned file appears).

  t-2  Add debug sessions to curated-shrink list
       files:    internal/gate/lists.go, internal/gate/drossfiles_test.go
       covers:   c-8
       desc:     Append "debug/*.md" to Defaults().CuratedFiles. Extend TestCuratedShrinkBoundary's
                 path list with "debug/s.md" and add a scope probe.
       contract: - A 499-byte Write over a 1000-byte .dross/debug/s.md is refused and the refusal names
                   Edit. A 500-byte Write is allowed. An Edit is never judged. Removing "debug/*.md"
                   from Defaults() turns the debug/s.md rows of TestCuratedShrinkBoundary red.
                 - A 1-byte Write over a 1000-byte .dross/debug/.gitignore is allowed (only *.md is
                   curated). A wider pattern ("debug/*") fails this probe.

Wave 2 (depends t-1)
  t-3  Store: ignore-first create, list, close-write
       files:    internal/debug/store.go, internal/debug/store_test.go,
                 internal/secretscan/writers.go, internal/cmd/secretscan_writer_enum_test.go
       covers:   c-1, c-2, c-5, c-6
       desc:     Create(root, slug, now) writes .dross/debug/.gitignore ("*") BEFORE the session file,
                 through pathfence. List(root) returns sessions newest-mtime first. Close(root, slug,
                 mode, reason, now) gates on CloseGaps and rewrites only the header. Declare the writer
                 MachineLocal with a new DebugIgnoreSeed and add its preparer to
                 TestMachineLocalWritersAreReallyIgnored.
       contract: - IGNORED BEFORE WRITE (c-1): in a fresh `git init` repo after Create:
                   `git check-ignore -q .dross/debug/<slug>.md` succeeds AND `git status --porcelain`
                   is empty. The self-ignoring .gitignore must ignore itself, or the tree is dirty and
                   /dross-quick's clean baseline breaks (locked fix_path).
                 - ORDERING: with .dross/debug/.gitignore pre-created as a DIRECTORY (so the ignore
                   write fails), Create returns an error and <slug>.md does not exist. Writing the
                   session first fails this.
                 - DUPLICATE: Create over an existing slug (open or closed) errors naming the slug,
                   and the file stays byte-identical.
                 - INVALID SLUG: Create("../x") errors before any FS mutation (.dross/debug not
                   created).
                 - LIST (c-2): with no debug dir → empty, nil error. With 4 fixtures + os.Chtimes →
                   order is newest-first and each carries its parsed counts and mtime. `.gitignore`,
                   `notes.txt` and `Bad Name.md` are skipped. A malformed file is listed with state
                   malformed, not returned as an error.
                 - CLOSE REFUSAL (c-5): Close --fixed on a fresh template errors naming Resolution and
                   Prevention, and the file is byte-identical afterwards (no partial header write).
                 - CLOSE WRITE (c-6): a successful fixed/abandoned close changes only the header
                   region (status line, and reason for abandoned). Every byte from the first "## "
                   heading on is identical. The file still exists. Closing an already-closed session
                   errors. Closing an unknown slug errors naming it.
                 - REGISTRY: TestMachineLocalWritersAreReallyIgnored proves "debug/<name>.md" ignored
                   by the DebugIgnoreSeed preparer. TestEveryDrossWriterIsDeclared fails if store.go
                   writes undeclared.
                 - The coverfloor (>=50% per file from own package) holds for session.go and
                   store.go.

Wave 3 (depends t-3)
  t-4  Wire `dross debug {new,list,close}` verbs
       files:    internal/cmd/debug.go, internal/cmd/debug_test.go, cmd/dross/main.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-1, c-2, c-5, c-6, c-7
       desc:     Thin cobra layer over internal/debug (no os/exec, json or toml: boundary ban). new
                 prints the path and `/dross-debug <slug>`. list prints one row per session. close
                 takes --fixed | --abandoned --reason. A fixed close prints the RuleCommand line.
                 Register in newRoot and re-mint cli_tree.txt.
       contract: - NEW (c-1): `debug new flaky-login` in a temp dross+git repo creates the file with
                   all seven headings in order and `status: open`. A second `new flaky-login` errors.
                   `new ../x` errors and no file appears outside .dross/debug.
                 - LIST (c-2): four fixtures (open, needs-replan, resolved, abandoned) with fixed
                   mtimes. Each stdout row carries slug, state, hypothesis count, failed-fix count
                   and the formatted mtime. Swapping the hypothesis/failed columns or dropping the
                   mtime fails it. Empty → "(no debug sessions)", exit 0.
                 - FLAG MATRIX (c-5): --abandoned without --reason, --fixed with --abandoned, and
                   neither flag each error naming the working form. `--abandoned --reason "dup"` on
                   an untouched template closes (header abandoned + reason) and prints NO
                   `dross rule add`.
                 - FIXED CLOSE PRINTS, NEVER ADDS (c-7): after adding two signals and Prevention,
                   `close --fixed` exits 0. Stdout has exactly one line starting `dross rule add `.
                   .dross/rules.toml is byte-identical, and the temp-HOME global rules file is absent.
                   The printed line's args (via the /bin/sh printf oracle) run through Rule()
                   in the same repo add a rule whose text equals the collapsed Prevention. This proves
                   the line is ready to run.
                 - SURFACE: cmd/dross TestCLITreeGolden (cli_tree.txt) pins `dross debug new|list|close` and the
                   --fixed/--abandoned/--reason flags, so renaming a verb or flag fails it.
                   TestNarratedCommandsResolveAgainstTheTree goes red if debug.go narrates a verb that
                   doesn't exist, or if cmd.Debug() is dropped from newRoot.

  t-5  Nudge open sessions in status + reentry
       files:    internal/cmd/status.go, internal/cmd/reentry.go, internal/cmd/debug_nudge_test.go
       covers:   c-2, c-3, c-4, c-6
       desc:     status prints one `debug:` line naming each non-closed session with its state and
                 the newest one's `/dross-debug <slug>`. reentryLine (shared by the status footer and
                 the hook envelope) appends a debug segment naming the newest non-closed session,
                 its resume command, and "+N more". Malformed sessions nudge as open.
       contract: - ONE LINE / NOTHING (c-2): one open session → exactly one stdout line starting
                   "debug:", naming the slug and `/dross-debug <slug>`. Only closed sessions, or no
                   debug dir → zero "debug:" lines AND the status output is byte-identical to the
                   no-dir run (reentry footer included).
                 - NEEDS-REPLAN SURFACED (c-4): a fixture with failed×3 → the debug line reads
                   `needs-replan` for that slug. After a `- re-plan:` item is appended → `open`.
                 - REENTRY (c-3): the decoded hook envelope line contains the slug and
                   `/dross-debug <slug>`. With two open sessions (Chtimes) it names the newer one's
                   resume command plus "+1 more". With an open session, status's last line is
                   byte-equal to the envelope line (the TestStatusEndsWithReentryLine invariant,
                   re-asserted with a session present).
                 - CLOSE DROPS (c-6): after debug.Close (abandoned), neither status nor reentry names
                   the slug, and the file still exists.
                 - NO REGRESSION: with no sessions, the reentry.txt golden in TestOutputGoldens stays
                   byte-identical, so a debug segment leaking with zero sessions fails it.
                 - HOOK SAFETY: a session with its status line deleted → `dross reentry` exits 0 and
                   still names the slug (fail toward the nudge, never toward silence or a loud hook).

Wave 4 (depends t-4)
  t-6  Ship /dross-debug prompt, shim, classification
       files:    assets/prompts/debug.md, assets/commands/dross-debug.md, docs/interaction-audit.md,
                 docs/subagent-offload-audit.md, docs/footer-audit.md, internal/cmd/debug_prompt_test.go,
                 README.md
       covers:   c-9 (and the prompt halves of c-5 close_gates / c-7 prevention_rule)
       desc:     Interactive shim (AskUserQuestion) @-including prompts/debug.md. The prompt runs
                 `dross interaction show` pre-flight, then a probe loop, a needs-replan stop, fix
                 routing, the close + independence definition, the pair offer / solo wrap-up of the
                 printed rule command, and a clear-point footer to `/dross-debug <slug>`. Add the
                 `### dross-debug` ✅ audit section, the `### debug` offload section, the footer row
                 and the README rows.
       contract: - Per-requirement subtests over debug.md (lowercased, backticks stripped), each failing
                   by name when its anchor is deleted:
                   ONE-HYPOTHESIS ("one hypothesis per probe");
                   WRITE-BEFORE-NEXT (evidence + verdict written to the session file "before the next
                   probe", located inside the probe-loop section);
                   TREE-RESTORE ("pre-session" + `git status --porcelain` baseline compared after
                   every probe);
                   HARD-STOP ("blocked" on line one, "needs-replan" read from `dross debug list`,
                   "pair and solo alike");
                   FIX-ROUTING (`/dross-quick` AND `dross task add`, "never commits").
                 - NO COMMIT PATH: codeAdds("debug", body) is empty, and no fenced line in debug.md
                   starts with `git commit`. No fenced line starts with `dross rule add` (the command
                   is relayed from close output, offered in pair via AskUserQuestion, listed in the
                   solo wrap-up). Adding a commit step fails it.
                 - INDEPENDENCE: the close section names repro gone, red-before/green-after, and
                   suite or CI green as distinct signal kinds (locked close_gates).
                 - RESOLVES: every `dross debug <sub>` in debug.md resolves to a Debug() subcommand,
                   so narrating a nonexistent `dross debug show` fails.
                 - CLASSIFIED: interactionCoverage(root) lists "debug" Covered, and its audit section
                   carries ✅ with no ⬜/🟡/❌. The prompt contains `dross interaction show` and
                   interactionRefPhrase. promptFooterState(root,"debug") == footerPresent with
                   /dross-debug.
                 - The existing fail-closed gates (TestCommandsPromptsParity,
                   TestInteractionAuditEnumeratesEveryInteractiveCommand, TestFooterCoverageFailClosed,
                   TestSubagentOffloadAuditCoversEveryPrompt, assets TestEmbedDrift) go red if any
                   one of the seven files is missing.
                 - TestReadmeAdvertisesOnlyRealCommands keeps the `dross debug` README row honest.
```

## Coverage

| Criterion | Tasks | The contract that proves it |
|---|---|---|
| c-1 new / template / ignore-first / refusals | t-1, t-3, t-4 | template round-trip + slug table (t-1); ignored-before-write, ordering, duplicate (t-3); e2e new (t-4) |
| c-2 list + status line | t-1, t-3, t-4, t-5 | counts parsing (t-1); List order/skip/malformed (t-3); list rows (t-4); one-line-or-nothing (t-5) |
| c-3 reentry names session + resume command | t-5 | envelope contains slug + `/dross-debug <slug>`; status footer byte-equal |
| c-4 three failed since re-plan → needs-replan | t-1, t-5 | replan table + fence/section scope (t-1); surfaced in status (t-5) |
| c-5 fixed-close gates / abandoned bypass | t-1, t-3, t-4 | CloseGaps table (t-1); refusal leaves file byte-identical (t-3); flag matrix (t-4) |
| c-6 closed stays on disk, leaves nudges | t-3, t-4, t-5 | header-only rewrite (t-3); e2e close (t-4); close drops from status + reentry (t-5) |
| c-7 prints `dross rule add`, never adds | t-1, t-4 | /bin/sh round-trip (t-1); rules.toml byte-identical + printed line runs through Rule() (t-4) |
| c-8 curated-shrink covers debug/*.md | t-2 | 499/500 boundary on debug/s.md; .gitignore not curated |
| c-9 /dross-debug installed, classified, prompt requirements | t-6 | five named requirement subtests, no-commit scan, resolves, coverage gates |

9/9 criteria covered.

## Judgment calls

- **Ignore mechanism.** Chose a self-ignoring `.dross/debug/.gitignore` (`*`), like gatestate.ensureDir. Rejected adding `.dross/debug/` to drossIgnoreEntries and appending to the root .gitignore, because that dirties a tracked file in every onboarded repo (this one included) and breaks /dross-quick's clean baseline, which the locked fix_path decision depends on.
- **Package placement.** Logic lives in a new internal/debug package with package-local tests; internal/cmd stays a thin cobra layer. The alternative of putting it all in internal/cmd was rejected because the boundary ban and the >=50%-per-file coverfloor push it out anyway, and because gremlins then kills parser mutants without the ~600s cmd suite.
- **Re-plan marker placement.** The re-plan marker is a `- re-plan:` item inside ## Fix attempts, so "since the last re-plan" is plain list order. Rejected a separate heading or a header counter: the CLI reads only headings and markers (locked session_format).
- **Counting rule.** Only top-level list items outside code fences count. Rejected a whole-file line grep, because sessions quote command output and a quoted `- failed:` would falsely trip needs-replan.
- **`list` failed-fix count.** `list` shows the total failed-fix count; the state column carries the since-re-plan threshold. Rejected showing only the since-re-plan count, because it hides history after a re-plan.
- **Reentry segment placement.** The reentry segment goes inside the shared reentryLine. Rejected putting it only in the hook envelope, because that would break the status-footer byte-equality invariant the existing tests pin.
- **Malformed sessions.** A malformed session nudges as open. Rejected dropping it (a mangled header would vanish from the nudges) and rejected erroring (it would turn the SessionStart hook loud over agent-edited markdown).
- **Interaction classification.** /dross-debug is interactive (audit section, not Exempt), because the pair-mode rule offer and the fix routing are user decisions.
- **Footer.** /dross-debug carries the clear-point footer (`/dross-debug <slug>`) rather than an Exempt row. Write-as-you-go makes every probe boundary durable, which is the reason this feature exists.
- **Rule text shape.** Prevention is collapsed to one line, with list markers stripped, for the printed `dross rule add`. Rules render one per line, so a multi-line argv would break the `<rules>` block.
- **Shell-quoting test oracle.** The test oracle is real /bin/sh. Rejected internal/shellscan, which describes itself as "deliberately not a shell" and so cannot prove that the quoting survives a shell.
- **No `debug show` verb.** The spec doesn't ask for one, and adding it would trip TestEveryStructuredShowAcceptsJSON. The prompt reads the session file directly.
- **t-6 size.** t-6 is a 7-file task. Splitting it would leave a red intermediate commit, because the fail-closed audit gates fire the moment the prompt and shim exist and the commit-green gate refuses it. All seven files are assets/docs plus one prompt test.
- **c-8 as its own task.** c-8 is a separate task even though it is small: it touches a different package (internal/gate) with no dependency on anything else, so it runs in parallel in wave 1.
- **No hooks change.** The SessionStart hook is registered without a matcher, so `dross reentry` already fires after /clear and after compaction. c-3 is proven at the reentry output.
- **Writer registry with the writer.** The writer registry entry ships in t-3, together with the writer. Deferring it would leave the writer-walk enum test red at t-3's commit.
