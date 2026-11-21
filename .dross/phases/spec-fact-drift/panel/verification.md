# verification lens — spec-fact-drift

Lens: each criterion's ideal test contract was written first; each task is the smallest change that makes that contract satisfiable. Where a contract has to be observed green on the *old* code (c-8), it gets its own wave-1 task so the pin exists before anything touches the resolver.

```
Phase spec-fact-drift — 10 tasks across 4 waves

Wave 1
  t-1  Pin architecture link output before refactor
       files:    internal/cmd/architecture_links_pin_test.go (new)
       covers:   c-8
       contract: a temp repo with one Go file + ARCHITECTURE.md holding six bullets — ok, moved (cited :3, lives :5),
                 unresolved, ambiguous (two `Fetch` methods cited bare), skipped (`.py`), and a `pkg.Bar` bullet that
                 resolves ONLY through lastComponent fallback — pins architectureLinkWarnings' exact lines
                 ("Foo — x.go:3 moved to line 5", "Gone — x.go:9 no longer resolves", nothing for ambiguous/skipped/
                 fallback) and `dross architecture check`'s exact stdout; written and observed green on today's code.
                 If a later refactor drops the fallback or reclassifies a bullet, this test fails.

  t-2  Extract citations with punctuation stripped
       files:    internal/facts/cite.go (new), internal/facts/cite_test.go (new)
       covers:   c-7, c-1, c-3
       contract: Extract("see foo.go:12.") yields FileLine{Path:"foo.go", Line:12}; table over trailing . , ; : ,
                 wrapping () and backticks, `(`foo.go:12`)`, `foo.go:12:` — none leak into path, line or symbol.
                 Leading `.` survives (`.dross/x.toml` stays `.dross/x.toml`).
       contract: path shapes — a slashed token with a letter-led extension is a path (`internal/a/b.go`); a
                 slashed token ending `/` is a dir (`internal/codex/`); a bare filename is a path only with a known
                 extension (`spec.toml` yes, `v1.9`/`e.g.` no); `milestone/v1.9`, `origin/main`, URLs, `~/…`,
                 absolute paths and anything with `<` `*` `{` are not paths.
       contract: symbol_shape — a single backticked `Ident(.Ident)*` token with optional `()` is a symbol only if
                 dotted or call-shaped: `codex.Index`, `quoted.String()`, `Foo()` are symbols; `dross phase facts`,
                 `--json`, `current_phase_status`, `phase`, `spec.toml` (path wins) and a dotted token with a
                 lowercase, non-call final component (`paths.source`, TOML key) are skipped.
       contract: `` `Foo` — x.go:5 ``, `` `Foo` (x.go:5) `` and `` `Foo` at x.go:5 `` pair Foo with x.go:5;
                 a bare `x.go:5` carries no symbol.

  t-3  Codex: package on symbols, per-file indexed flag
       files:    internal/codex/codex.go, internal/codex/go_indexer.go, internal/codex/codex_test.go
       covers:   c-3, c-2
       contract: GoIndexer on a `package foo` file sets Symbol.Package = "foo" on every symbol (incl. Type.Method);
                 Name is unchanged ("T.M"), so `dross codex` stdout for the same fixture is byte-identical.
       contract: FileSymbols(".py file") → indexed=false; FileSymbols(".ts") with astGrepAvailableFn stubbed false
                 → indexed=false (SupportsFile alone would say true and yield an empty, falsely-"gone" list);
                 FileSymbols(".go") → indexed=true with the same symbols Index returns.

  t-4  Add spec-level new list to schema
       files:    internal/phase/phase.go, internal/phase/phase_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-5, c-4
       contract: a spec without `new` saves with no `new` key (byte-identical round-trip of an existing spec);
                 `new = ["internal/facts/cite.go", "facts.Check"]` round-trips in order.
       contract: TestEveryPathShapedFieldIsDeclared stays green with the row `phase.Spec.New new` — the field
                 holds paths AND symbols, is only ever compared, never opened.

Wave 2
  t-5  Export Classify with an injectable matcher   (depends t-1)
       files:    internal/architecture/links.go, internal/architecture/links_test.go
       covers:   c-2, c-8
       contract: Classify(link, syms, matchSymbols) returns exactly what resolveAgainst did (ok/moved+NewLine/
                 unresolved/ambiguous) — ResolveAllIn now routes through it, and t-1's pin is unchanged.
       contract: Classify(`pkg.Bar`, [{Name:"Bar", Package:"other"}], strict) → Unresolved while the same link
                 with matchSymbols → OK: the matcher, not the classifier, owns the fallback.

  t-6  Check spec/plan paths against the tree   (depends t-2, t-4)
       files:    internal/facts/tree.go (new), internal/facts/check.go (new), internal/facts/check_test.go (new)
       covers:   c-1, c-6, c-9
       contract: fixture repo — criteria c-2 cites `internal/gone.go` → Finding{Field:"criteria c-2",
                 Kind:missing, Cite:"internal/gone.go"}; task t-3 description → "task t-3 description"; decision
                 choice → "decisions <key> choice"; test_contract → "task t-3 test_contract"; Finding.String()
                 renders "<field>: missing path <cite>".
       contract: scanned_fields — the same missing path placed in a decision `why`, a [[deferred]] entry and an
                 edge reason produces zero findings.
       contract: new_names — a missing path listed in any task's `files` is not reported; one listed in spec
                 `new` (in any punctuation, normalised by t-2) is not reported.
       contract: a bare `links.go` resolves when any tree file has that basename; an existing dir citation
                 resolves; `.git/` and `node_modules/` are never walked.
       contract: CheckPhase(repo, dir) with no plan.toml checks the spec alone; with no spec.toml returns an error
                 naming `<dir>/spec.toml`.
       contract: pathtaint_audit_test reports no finding in internal/facts — a cited path is only looked up in
                 the walked tree set, never passed to os.* or pathfence.

Wave 3
  t-7  Resolve file:line and symbol citations   (depends t-3, t-5, t-6)
       files:    internal/facts/symbols.go (new), internal/facts/symbols_test.go (new), internal/facts/check.go
       covers:   c-2, c-3
       contract: c-2 — Go fixture with `func Foo` at :5: `` `Foo` — x.go:5 `` no finding; `` `Foo` — x.go:9 ``
                 → Kind:moved, Status == architecture.StatusMoved, NewLine 5, String() names "now at line 5";
                 `` `Bar` — x.go:5 `` → Kind:gone (StatusUnresolved); two `Fetch` methods → ambiguous, not
                 reported; `` `run` — y.py:3 `` → skipped, not reported; a bare `links.go:12` shared by two files
                 → skipped (never guess the file).
       contract: c-3 strict match — `pkg.Exported` resolves only when a file in package pkg defines Exported;
                 `T.M()` resolves against Name "T.M"; `quoted.String()` is unresolved in a fixture that has a
                 String method on another type (no last-component fallback); an ambiguous match is not reported.
       contract: c-3 skips — a qualifier naming a tree file/dir whose sources codex can't index (`helper.run()`
                 beside helper.py) is not reported; a qualifier that is an external import (`os.Stat`,
                 `cobra.Command`, imported by a tree Go file from outside the module) is not reported.
       contract: real-tree regression — against this repo, strict match resolves `codex.Index` and
                 `architecture.Classify` and does NOT resolve `quoted.String()` (the exec-taint-enumeration case).
       contract: the symbol index is built only when the phase holds a symbol or file:line citation, from walked
                 paths only (no lookup-by-cited-path into FileSymbols).

  t-8  Add dross phase facts command   (depends t-6)
       files:    internal/cmd/phase_facts.go (new), internal/cmd/phase_facts_test.go (new), internal/cmd/phase.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-6, c-9
       contract: clean fixture → exit 0, stdout says nothing is unresolved and names what was checked (spec only
                 when there is no plan.toml; spec + plan otherwise).
       contract: one missing path → ExitCodeError non-zero, each Finding.String() line printed; `--json` emits
                 {phase, checked, unresolved:[{field,kind,cite,line,new_line}]} with unresolved [] (never null)
                 when clean, and still exits non-zero when it is not empty.
       contract: moved/gone/unresolved-symbol Findings (synthetic values) render with the new line / kind — so
                 t-7's kinds need no renderer change.
       contract: the repo tree (paths + sizes + mtimes) is identical before and after a run, clean or not.
       contract: c-9 — no spec.toml → non-zero naming `.dross/phases/<id>/spec.toml`; unknown id → non-zero
                 naming the missing phase dir; neither output contains the nothing-unresolved line.
       contract: cli_tree golden lists `dross phase facts` and its `--json` flag.

Wave 4
  t-9  Gate plan-lock on unresolved citations   (depends t-7, t-8)
       files:    internal/cmd/state.go, internal/cmd/state_facts_gate_test.go (new)
       covers:   c-5
       contract: `state set current_phase_status planned` with 3 unresolved citations split across spec and plan
                 refuses, lists all 3 Finding.String() lines, names `dross phase facts <id>` and spec `new`, and
                 leaves the status unchanged.
       contract: the same fixture with each citation in spec `new` (or the path in a task's `files`) locks.
       contract: shared rules — for a fixture table (clean / missing path / moved line / why-only citation /
                 new-marked / planned file) the gate refuses iff `dross phase facts --json` reports a non-empty
                 unresolved list, and the gate's lines equal the command's lines.
       contract: other statuses (`executing`) never run the gate; no current phase or no spec.toml allows
                 (mirrors planLockEdgeGate, keeps execute_test's spec-less fixture locking); edges gate still
                 fires first on a spec with both problems.

  t-10 Walk unresolved citations in /dross-plan   (depends t-8)
       files:    assets/prompts/plan.md, assets/prompts/_plan.facts.md (new), assets/partials.go,
                 assets/partials_test.go, assets/testdata/prompt_instructions.txt, internal/cmd/plan_prompt_test.go
       covers:   c-4
       contract: expanded plan.md runs `dross phase facts <id> --json` in §1, before the `## 2.` heading, and
                 carries the fetch line **When** `facts=unresolved` — `dross phase facts` exited non-zero:
                 `dross prompt partial plan facts`.
       contract: TriggerVocabulary gains facts:{clean, unresolved}; TestCommonPathVocabularyPinned and
                 TestCommonPathFetchesNothing pin facts=clean as the common path.
       contract: _plan.facts.md asks one AskUserQuestion per unresolved citation (options: fix the spec / new in
                 this phase → append the cite to spec `new = [...]`), re-runs `dross phase facts <id> --json`, and
                 forbids §2 until it exits 0 — plan_prompt_test asserts each phrase on the expanded prompt.
       contract: TestPromptByteBudget stays green for plan.md (14452 bytes today, cap 15000 — the walk lives in
                 the partial, not the base prompt).
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2, t-6 |
| c-2 | t-3, t-5, t-7 |
| c-3 | t-2, t-3, t-7 |
| c-4 | t-4, t-10 |
| c-5 | t-4, t-9 |
| c-6 | t-6, t-8 |
| c-7 | t-2 |
| c-8 | t-1, t-5 |
| c-9 | t-6, t-8 |

9/9 covered.

## Judgment calls

- **c-8 pin is its own wave-1 task (t-1)**, not folded into the refactor. Rejected: writing the pin inside t-5. Why: a characterization test only proves "unchanged" if it was seen green on the old code. Written in the same task as the refactor, it could just describe the new behaviour.
- **The strict matcher is injected into a shared `architecture.Classify`.** Rejected: dropping lastComponent for everyone (breaks c-8) and a second classifier inside facts (breaks c-2's "same classification"). Why: the classifier is shared and only the matcher differs, so both criteria are met.
- **file:line citations also use the strict matcher.** Rejected: architecture's fallback for c-2. Why: locked symbol_shape defines what a symbol citation is with no fallback, and a `pkg.Name — f.go:N` bullet still resolves strictly once codex records Package.
- **`Package` is added to codex.Symbol (GoIndexer).** Rejected: using the directory basename as the package name. Why: it is wrong for `cmd/dross` (package main) and `_test` packages. Name is untouched and `dross codex` prints no Package, so its output and architecture matching don't change.
- **New `codex.FileSymbols` reports indexed=false when ast-grep is absent.** Rejected: reusing SupportsFile. Why: SupportsFile reports which languages are covered, not whether the tool is available. A TS repo without ast-grep would report every TS citation as gone, which breaks c-3's "a file codex can't index is never reported".
- **Existence is decided by walking the tree into a set, never by `os.Stat` on a cited path.** Rejected: stat or pathfence.Contain per citation. Why: Criterion.Text, Choice, Description and TestContract are not_paths ledger rows. Ledger taint survives Contain, so opening a path extracted from them is a pathtaint finding.
- **The `new` field goes in not_paths.txt, not pathfence.Fields().** Rejected: declaring it a path field. Why: it mixes paths and symbols and is only compared for membership, never opened.
- **Missing spec at plan-lock allows (mirrors planLockEdgeGate); the command errors (c-9).** Rejected: refusing lock with no spec. Why: the edges gate set this precedent and execute_test's spec-less fixture locks today. c-5's wording is about a spec that holds citations.
- **External-import qualifiers (`os.Stat`, `cobra.Command`) are skipped.** Rejected: reporting them as "matches no symbol in the tree". Why: the code is outside the tree, which is the same reason c-3 skips languages codex can't index. Reporting stdlib citations would push the c-4 walk into marking `os.Stat` as "new in this phase", which is false.
- **A dotted backticked token with a lowercase, non-call final component is treated as a TOML key and skipped.** Rejected: checking every dotted token. Why: locked symbol_shape skips TOML keys, and `paths.source` and `codex.Index` can only be told apart by the final component. Cost: unexported Go funcs cited without `()` go unchecked.
- **Bare filenames resolve by basename anywhere in the tree, and only with a known extension.** Rejected: requiring repo-relative paths. Why: specs routinely cite `spec.toml` and `links.go` bare. Requiring a known extension keeps `v1.9` and `e.g.` from becoming paths. A bare file:line whose basename matches more than one file is skipped rather than guessed.
- **A symbol pairs with a file:line only in an adjacent form (`Sym — f:N`, `Sym (f:N)`, `Sym at f:N`).** Rejected: the nearest symbol in the sentence. Why: adjacency is deterministic and testable. A lone `f.go:N` is checked as a path only.
- **c-4's `new` list is written by the prompt editing spec.toml, not by a CLI writer.** Rejected: a `dross phase facts` write subcommand. Why: c-6 says the command never writes a file, and facts normalises `new` entries with t-2's stripper, so hand-written punctuation can't cause a miss.
- **The walk lives in a `_plan.facts.md` partial behind a new `facts` trigger.** Rejected: inlining it in plan.md. Why: plan.md has 548 bytes of headroom under the 15000-byte cap, and the clean path should load nothing extra.
- **The gate (t-9) waits for the command (t-8).** Rejected: running it in parallel in wave 3. Why: the refusal names `dross phase facts`, and naming a command before it is registered trips the narrated-command guard. The gate's shared-rule contract also diffs against the command's output.
- **The command (t-8) does not wait for symbol resolution (t-7).** Rejected: putting t-8 behind t-7. Why: c-6 and c-9 are fully testable with path citations, and the kind renderer is tested on synthetic Findings, so t-7 and t-8 share wave 3.
- **t-10 touches 6 files.** Rejected: splitting the vocabulary change out. Why: all 6 are in the prompt layer and the vocabulary pin is meaningless without the fetch line that uses it.
