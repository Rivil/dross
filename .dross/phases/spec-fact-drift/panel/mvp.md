# mvp draft — spec-fact-drift

Lens: smallest task set that satisfies every criterion. One engine, one consumer task holding both callers (CLI + gate) so "cannot disagree" is a single test, one prompt task.

```
Phase spec-fact-drift — 4 tasks across 3 waves

Wave 1
  t-1  Facts engine: path and file:line citations
       files:    internal/facts/facts.go (new), internal/facts/facts_test.go (new),
                 internal/phase/phase.go, internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go
       desc:     New package `facts`: Check(root, spec, plan-or-nil) returns findings
                 {field, kind, citation, status, new_line}. Scans ONLY the locked
                 scanned_fields (criteria text, decisions choice; task files, description,
                 test_contract), labelling the field ("criteria c-2", "decisions <key> choice",
                 "task t-3 description"). Extracts path and `path:line` citations, strips
                 wrapping punctuation/backticks/parens, routes every path through
                 pathfence.Contain (escaping or absent = missing). A `path:line` with an
                 adjacent backticked symbol ("`Sym` at/—/( path:line") resolves via the
                 unchanged architecture.Resolve on the contained path: Moved → "moved"
                 + new line, Unresolved → "gone", Ambiguous/Skipped → never reported.
                 Exempt: any path in any task's `files`, anything in spec `new`. Add
                 `Spec.New []string toml:"new,omitempty"` and declare it NotConsumed in
                 the pathfence registry (+ its name in fields_test's not-consumed list).
       covers:   c-1, c-2, c-7
       contract: - criteria c-1 citing internal/nope.go yields exactly one finding
                   {field:"criteria c-1", kind:path, citation:"internal/nope.go"}; the same
                   path in a decision `why`, a [[deferred]] entry and an edge reason yields none
                 - a path cited in task t-3 description but listed in task t-1 `files` yields
                   none; a path listed in spec `new` yields none; a nonexistent `files`
                   entry itself yields none
                 - fixture x.go with `func Foo` at line 5: "`Foo` at x.go:5" → none;
                   "`Foo` at x.go:9" → moved, new_line 5; "`Bar` (x.go:5)" → gone;
                   two methods both named Foo on different receivers → none (ambiguous)
                 - "see x.go:5." / "(x.go:5)," / "`x.go:5`;" all resolve as x.go line 5;
                   "see missing.go." reports citation "missing.go" (no trailing dot)
                 - "e.g.", "v1.9", an https:// URL and `.dross/phases/<id>/spec.toml`
                   are never extracted as paths
                 - "../outside.go" is reported missing and never os.Stat'd outside root
                 - spec.toml with `new = ["a.go"]` round-trips through LoadSpec/Save;
                   TestEveryPathShapedFieldIsDeclared passes with phase.Spec.New declared

Wave 2 (depends t-1)
  t-2  Symbol citations against the codex index
       files:    internal/facts/symbols.go (new), internal/facts/symbols_test.go (new),
                 internal/facts/facts.go, internal/codex/codex.go,
                 internal/codex/codex_test.go
       desc:     Extract backticked tokens of the locked symbol_shape (`Ident(.Ident)*`,
                 optional `()`, dotted or call-shaped); skip commands, flags, TOML keys,
                 bare lowercase words. Build the tree index lazily (only when ≥1 symbol
                 citation exists) by walking root with stack.SkipDir and calling a new
                 callers-free codex.FileSymbols(file). Strict match, no last-component
                 fallback: `Recv.Method` must equal a codex method name; `pkg.Name` /
                 `pkg.Recv.Method` additionally requires the defining file's package
                 (Go package clause; dir base otherwise) to be pkg. No match → finding
                 (kind symbol, status unresolved); ≥1 match → nothing. architecture/links.go
                 is NOT edited.
       covers:   c-3, c-7, c-8
       contract: - fixture tree with package codex defining Index: `codex.Index` → none,
                   `codex.Missing` → finding, `quoted.String()` (a String method exists on
                   another type, no package quoted) → finding, `Changes.Record()` defined as
                   a method → none, `Name()` defined in two packages → none
                 - `dross phase facts`, `--json`, `current_phase_status`, `foo`, bare `Foo`
                   are never symbol citations
                 - a symbol listed in spec `new` yields none; a symbol in a decision `why`
                   yields none
                 - "`codex.Index`." and "(`codex.Index()`)," resolve to codex.Index
                 - c-8: architecture.ResolveAllIn over a fixture ARCHITECTURE.md bullet
                   "`wrongpkg.Index` — codex/codex.go:N" still returns StatusOK (fallback
                   intact) while facts reports the backticked `wrongpkg.Index` unresolved —
                   fails if the strict matcher leaks into matchSymbols
                 - codex.FileSymbols(x.go) equals codex.Index([x.go]).Symbols; on an
                   unsupported extension it returns nil, nil
                 - a tree with no symbol citations never walks the tree (index builder
                   call count 0)

  t-3  `dross phase facts` command and plan-lock gate
       files:    internal/cmd/phase_facts.go (new), internal/cmd/phase_facts_test.go (new),
                 internal/cmd/phase.go, cmd/dross/testdata/cli_tree.txt,
                 internal/cmd/state.go, internal/cmd/state_facts_gate_test.go (new)
       desc:     `facts <phase-id> [--json]` registered under `phase`: one shared loader
                 (unknown phase → error naming the dir; no spec.toml → error naming it;
                 plan.toml optional) feeds facts.Check; prints each finding or "nothing
                 unresolved"; non-zero exit when any finding; --json emits
                 {phase, unresolved:[...]} with a non-nil list; reads only. planLockFactsGate
                 sits beside planLockEdgeGate in the `current_phase_status planned` branch,
                 uses the same loader + facts.Check, absence-allows like the edges gate,
                 and refuses listing every finding.
       covers:   c-5, c-6, c-9
       contract: - clean fixture spec with no plan.toml → exit 0, stdout says nothing is
                   unresolved; the same spec citing a missing path → non-zero, output names
                   "criteria c-1" and the path
                 - --json on the clean fixture emits "unresolved":[] (not null); a finding
                   carries field/kind/citation/status/new_line
                 - snapshot of every file (path, size, mtime) under the fixture root is
                   identical before and after a run, clean and unresolved
                 - unknown id → non-zero, error contains the phase dir; existing dir with no
                   spec.toml → non-zero, error contains ".../spec.toml", stdout lacks
                   "nothing unresolved"
                 - `state set current_phase_status planned` with a spec citing two missing
                   paths → refused, error lists both, state.json status unchanged; adding
                   both to spec `new` → locks
                 - parity: on one fixture, the citations in the gate's refusal equal the
                   CLI --json unresolved citations as a set
                 - cli_tree.txt rows for `phase facts` and its --json flag match the
                   registered command

Wave 3 (depends t-3)
  t-4  /dross-plan runs facts and walks citations
       files:    assets/prompts/plan.md, assets/testdata/prompt_instructions.txt,
                 internal/cmd/plan_prompt_test.go
       desc:     In plan.md §1, before §2 decomposition: run `dross phase facts <id> --json`;
                 walk each unresolved citation one per turn via AskUserQuestion — fix the
                 spec (edit the citing field) or confirm it is new in this phase (append it
                 to spec `new`) — re-run until clean, then decompose. Add the
                 prompt_instructions row.
       covers:   c-4
       contract: - TestPlanPromptFactsWalk: the expanded plan.md contains
                   `dross phase facts <id> --json` positioned before the
                   "## 2. Goal-backward decomposition" heading; deleting the step fails it
                 - the walk text names "one per turn", both options (fix the spec / `new`),
                   and re-running facts before composing
                 - prompt_instructions row `plan.md	dross phase facts <id> --json` fails the
                   instructions test if the line is removed from plan.md
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (surfaced by t-3) |
| c-2 | t-1 |
| c-3 | t-2 |
| c-4 | t-4 |
| c-5 | t-3 |
| c-6 | t-3 |
| c-7 | t-1, t-2 |
| c-8 | t-2 |
| c-9 | t-3 |

9/9 covered.

## Judgment calls

- **CLI and plan-lock gate in one task (t-3, 6 files, one layer)** rather than two: c-5's "cannot disagree" becomes one parity test over a shared loader. The cost is going one file over the 5-file split guideline.
- **Symbol check split from the path/line engine (t-2 apart from t-1)** rather than one 10-file engine task: it is the only part that needs a tree index and a codex change, and it can run in parallel with t-3 because t-3 only calls facts.Check.
- **Do not touch architecture/links.go**: c-2 reuses the exported `architecture.Resolve` on a pathfence-contained absolute path. I rejected adding a `ResolveIn` refactor, because c-8 then holds by construction and its test guards the one real risk, which is the strict c-3 matcher leaking into `matchSymbols`.
- **New `codex.FileSymbols` export** rather than one big `codex.Index(allFiles)`: Index also runs a findCallers grep pass plus git log per directory, which is wasted cost on every plan-lock.
- **Only a backticked symbol right next to a `path:line` names the symbol for c-2.** It can sit before the citation, separated by `at`, `—` or `(`. A bare `path:line` gets only the c-1 existence check. I rejected scanning the whole sentence for a symbol, because it guesses wrong in sentences that name several symbols.
- **A path needs a `/` or a known source/doc extension.** I rejected architecture's `fileOnlyRe` as-is, because it would extract `e.g` and `v1.9` from prose and report false misses.
- **Every extracted path goes through `pathfence.Contain`** before any Stat. Criteria text, decision choice and task text are filed in not_paths.txt, and the pathtaint audit reports any of them that reaches the filesystem. Containing the path fixes that and also reports `../` escapes as missing.
- **Spec `new` is declared in the pathfence registry as NotConsumed** (facts compares it and never opens it), not filed in not_paths.txt, because it can hold paths. Each field is filed exactly once.
- **Task `files` entries are scanned but always count as planned** (locked `new_names`), so they never produce a finding of their own. They only exempt matching citations elsewhere.
- **The gate allows when the spec is absent**, like `planLockEdgeGate`. c-9's non-zero exit for a missing spec applies only to the CLI. Plan-lock never runs on a spec-less phase in practice.
- **No dedicated schema task.** The `new` field and its pathfence rows ride in t-1, the first task that reads them.
- **Risk I have not planned a task for:** existing internal/cmd tests that plan-lock fixture specs (state_edges_gate_test, phase_test, execute_test, …) may cite nonexistent paths and start failing under the new gate. t-3's writer must check this. If any do, the fixture edits go into t-3's files through `dross task edit`.
