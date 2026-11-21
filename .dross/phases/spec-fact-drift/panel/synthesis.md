# synthesis — spec-fact-drift

## Repo checks behind this synthesis

These are the draft claims that change the plan's shape. Each was checked read-only against the tree.

- **Path-taint audit** (`internal/cmd/pathtaint_audit_test.go`). The sources are every read of a `pathfence.Fields()` entry and of every not_paths.txt row. The sinks are os.* string params plus `pathfence.ReadFile/WriteFile/Stat`. `clears(origin)` is true only when *every* field read at that origin is `pathConsumed`. The doc comment says it directly: "A NotConsumed field or a ledger row keeps its taint through Contain". `Criterion.Text`, `Decision.Choice`, `Task.Description` and `Task.TestContract` are ledger rows (not_paths.txt:222/223/239/242), and `Task.Files` is NotConsumed.
  - **mvp is rejected by the audit.** It runs Contain then Stat on a value derived from a ledger row, which is a finding at the Stat. Its c-2 route through the unchanged `architecture.Resolve` → `codex.Index` → `siblings`' `os.ReadDir` / git log on the cited path is a second finding.
  - **risk is accepted.** Once the four fields are reclassified Consumed with a `pathfence.Contained` carrier, Contain clears them. That is the remedy the audit prints and the not_paths.txt header prescribes ("fixed by declaring the field a path and routing it through pathfence.Contain — never by leaving it here"). It needs a `carrierType` case in `internal/cmd/pathfence_carrier_test.go`, which risk lists.
  - **verification is accepted, with a constraint.** Map lookups are not sinks, so a `bool` membership result is never a finding. But `taintEngine.forward` taints a `*ssa.Lookup` result when its key is tainted (taint_engine_test.go:923). Any value fetched with a cited-path key, such as walked paths from a basename map, is therefore tainted, and opening it is a finding. verification's rule "no lookup-by-cited-path into FileSymbols" is what keeps it clean. Every later edit has to preserve that rule.
- **plan.md byte cap: confirmed.** `wc -c assets/prompts/plan.md` = 14452. `promptByteCap = 15000` lives in `assets/budget_test.go:17`, not in prompt_expand_test.go. That leaves 548 bytes of headroom. mvp's inline walk in plan.md is at risk of breaching it; the partial route avoids that.
- **`codex.SupportsFile` / ast-grep: confirmed.** SupportsFile is dispatch-by-extension (codex.go:106; its doc comment says so). `AstGrepIndexer.Symbols` returns `nil, nil` when `astGrepAvailableFn()` is false (ast_grep.go:59). ast-grep is **not installed on this laptop**, and dross tracks `.ts`/`.svelte` files (fixtures/multilang-c3, internal/mutation/testdata). `codex.Symbol` has no package field. `dross codex` renders text (internal/cmd/codex.go), not JSON, so adding a field does not change its stdout.
- **Other facts that affect tasks:**
  - `LoadSpec` is a non-strict BurntSushi `toml.DecodeFile`, so a misplaced `new = [...]` is dropped silently.
  - `TestCommonPathVocabularyPinned` (assets/partials_test.go:215) checks `len(CommonPath())` against a pinned map, so adding a `facts` trigger key has to edit partials_test.go. risk's t-11 misses this file.
  - plan_prompt_test pins the line before `## 2G` as the panel fetch line, so the facts fetch line must go in §1, not before §2G.
  - Backticked stdlib qualifiers appear in only 5 citations across 151 phase dirs.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| risk | 9/9, every criterion delivered. It is the only draft with a writer that makes the c-4 "confirm new" outcome durable. | High: exact field labels, a stat-seam zero-call assertion, gate/command parity table. Gap: t-11 misses the partials_test vocabulary pin. | Good. t-1 is one file but justified (pin before refactor). t-6 is 6 files in one layer. | Correct but conservative at 6 waves: the CLI waits on symbol resolution. t-6 shares pathfence files with t-4 without declaring the dependency (harmless because they are in different waves). |
| mvp | 9/9 on paper. In practice c-3's "can't index → never reported" is not delivered (FileSymbols ignores ast-grep absence), and c-1/c-2's Contain-on-ledger route fails the path-taint audit. | Specific, but several contracts can't go green: the taint route, and `.dross/phases/<id>/…` vs Contain. No byte-budget contract for an inline walk with 548 bytes of headroom. | Too coarse. t-1 is 5 files across engine, schema and pathfence registry. t-3 is two commands in 6 files. | Valid: t-2 and t-3 run in parallel after t-1, t-4 after t-3. 3 waves. |
| verification | 9/9. c-4's "new" is a hand-edit, so it is fragile (see D2). | Highest: exact warning lines, real-tree regression, synthetic-finding renderer test, vocabulary pins. | Good, 10 tasks. t-10 at 6 files is justified. | Best parallelism (4 waves, CLI not waiting for symbols). t-5 uses `Symbol.Package` from t-3 without declaring `depends t-3`. |

**Skeleton: risk.** It is the only draft whose structural choices all survive the repo checks:
- an audit-accepted taint route that does not rest on a "never key a lookup by a cited path" invariant
- a writer instead of a hand-edit that silently drops
- an ast-grep-aware skip that leaves `SupportsFile` (and therefore c-8) untouched

verification is a close second and supplies most of the grafts.

## Merged plan

```
Phase spec-fact-drift — 11 tasks across 6 waves

Wave 1
  t-1  Pin architecture link-check output before refactor            [risk+verification]
       files:    internal/cmd/architecture_link_parity_test.go (new)
       desc:     Characterisation test written and observed green on TODAY's code. It uses a temp repo with one Go
                 file plus ARCHITECTURE.md bullets for ok / moved (cited :3, lives :5) / unresolved / ambiguous (two
                 `Fetch` methods) / skipped (.py, no line), and a `pkg.Bar` bullet that resolves ONLY through
                 the lastComponent fallback.
       covers:   c-8
       contract: the exact architectureLinkWarnings lines are pinned ("Foo — x.go:3 moved to line 5", "Gone — x.go:9
                 no longer resolves", nothing for ambiguous/skipped/fallback), along with the exact
                 `dross architecture check` stdout including the "N moved, N unresolved, N ambiguous" summary;
                 removing the lastComponent fallback turns the `pkg.Bar` bullet into an unresolved line and fails
                 the stdout pin;
                 reclassifying the ambiguous bullet as unresolved adds a warning and fails the warnings pin

  t-2  Add codex Symbols, CanIndex and Symbol.Package                [risk+verification]
       files:    internal/codex/codex.go, internal/codex/go_indexer.go, internal/codex/ast_grep.go,
                 internal/codex/codex_test.go
       desc:     Export Symbols(file): indexer dispatch only, with no findCallers, siblings or git log.
                 Export CanIndex(file) = SupportsFile && (Go || ast-grep available). SupportsFile stays
                 byte-identical. GoIndexer sets Symbol.Package from the package clause; Name is unchanged ("T.M").
       covers:   c-3, c-8
       contract: with astGrepAvailableFn stubbed false, CanIndex("a.ts") is false while SupportsFile("a.ts") stays
                 true (a SupportsFile change would shift architecture's Skipped set);
                 CanIndex("a.go") is true with ast-grep stubbed absent;
                 Symbols(f) equals Index([f]).Symbols for a Go file in a non-git dir, with no error where Index
                 would shell out to git;
                 a `package foo` file sets Package "foo" on every symbol including `T.M`; `cmd/dross`'s file yields
                 Package "main" (not the dir base);
                 `dross codex` stdout for the same fixture is byte-identical;
                 Symbols on a syntactically broken .go file returns an error and no panic

  t-3  Extract and classify citations from prose                    [risk+verification]
       files:    internal/facts/extract.go (new), internal/facts/extract_test.go (new)
       desc:     Extract(text) → []Citation{Kind: path|line|symbol, Raw, Path, Line, Symbol}.
                 Path: an extension in the closed allowlist (codex-indexed exts + toml/md/json/yml/yaml/txt/sh/mod/sum);
                 slashed = repo-relative, otherwise a bare basename.
                 file:line pairs with an adjacent backticked symbol (`Sym` — f:N / `Sym` (f:N) / `Sym` at f:N).
                 Symbol: the locked symbol_shape. A dotted token with a lowercase, non-call final component is a
                 TOML key and is skipped.
       covers:   c-7, c-1, c-2, c-3
       contract: `see foo.go:12.` / `(foo.go:12)` / `` `foo.go:12`, `` / `foo.go:12;` / `foo.go:12:` each yield Path
                 "foo.go", Line 12; a leading `.` survives (`.dross/x.toml`);
                 `e.g.`, `i.e.`, `v1.9`, `go1.27.3`, `1.9.4.0`, URLs, `milestone/v1.9`, `origin/main`, `~/…`, absolute
                 paths and tokens holding `<` `*` `{` (e.g. `.dross/phases/<id>/spec.toml`) yield no path (D7);
                 `` `dross phase facts` ``, `` `--json` ``, `` `paths.source` ``, `` `current_phase_status` ``,
                 `` `facts` `` yield no symbol;
                 `` `quoted.String()` ``, `` `phase.Spec` ``, `` `cmd.planLockEdgeGate()` `` yield symbols with `()` stripped;
                 `` `architecture.Resolve` — internal/architecture/links.go:192 `` yields one line citation carrying
                 Symbol "architecture.Resolve"; a bare `x.go:5` carries no symbol;
                 `` `spec.toml` `` is a path, never a symbol

  t-4  Add spec-level `new` list to the schema                      [risk+mvp; verification diverges, D6]
       files:    internal/phase/phase.go, internal/phase/phase_test.go, internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go
       desc:     Spec.New []string `toml:"new,omitempty"`, declared NotConsumed in pathfence.Fields() (compared,
                 never opened; reader internal/cmd/phase_facts.go). The name is added to
                 TestRegistryCoversEveryKnownFieldByName.
       covers:   c-4, c-5
       contract: Spec{New: ["internal/facts/extract.go", "facts.Check"]}.Save writes `new = [...]` in order BEFORE
                 the `[phase]` header, and LoadSpec round-trips it;
                 an empty New writes no `new` key (an existing spec saves byte-identically);
                 a hand-written `new = [...]` under `[phase]` decodes to an empty Spec.New (pins the silent drop
                 that t-9 exists to prevent);
                 deleting the registry entry fails TestEveryPathShapedFieldIsDeclared /
                 TestRegistryCoversEveryKnownFieldByName

Wave 2 (depends t-1, t-2, t-3, t-4)
  t-5  Strict symbol resolver sharing link-check classification     [risk+verification+mvp]
       files:    internal/architecture/links.go, internal/architecture/links_test.go, internal/facts/resolve.go (new),
                 internal/facts/resolve_test.go (new)
       desc:     Export architecture.Classify(link, syms, match), which is the resolveAgainst switch with a pluggable
                 matcher. ResolveAllIn and Resolve pass the existing matchSymbols.
                 facts.StrictMatch requires every qualifier (Symbol.Package, then Type.Method) and has no
                 last-component fallback.
                 A lazy tree index (WalkDir skipping .git and node_modules; parse errors non-fatal; codex.Symbols)
                 is built only when a symbol or paired file:line citation exists. It is marked Degraded when the
                 tree holds files with SupportsFile && !CanIndex (D4).
       depends:  t-1, t-2, t-3
       covers:   c-2, c-3, c-8
       contract: Classify(`pkg.Bar`, [{Name:"Bar", Package:"other"}], StrictMatch) → Unresolved, while the same link
                 with matchSymbols → OK (the matcher, not the classifier, owns the fallback);
                 an ARCHITECTURE.md bullet `wrongpkg.Index` — codex/codex.go:N still resolves OK through
                 ResolveAllIn while facts reports `wrongpkg.Index` unresolved (fails if strict matching leaks
                 into matchSymbols);
                 `quoted.String()`, with a `String` method on an unrelated type, is Gone (a re-added fallback
                 resolves it and fails the test);
                 `phase.Spec` resolves; `cmd.Spec` is Gone when Spec lives only in package phase;
                 `Changes.Record` defined in two files is ambiguous and never unresolved;
                 `Resolve` — links.go:N with the func at N+3 is Moved, NewLine N+3, and Gone after deletion;
                 a line citation into a .py file is Skipped;
                 with astGrepAvailableFn stubbed false and a .ts file in the tree, an unmatched symbol is Skipped
                 and the index reports Degraded (not Gone);
                 an unparsable testdata .go file doesn't stop the other files indexing;
                 phase input with no symbol or paired-line citation never builds the index (builder call count 0);
                 real-tree regression: against this repo, StrictMatch resolves `codex.Index` and
                 `architecture.Classify` and does not resolve `quoted.String()`;
                 t-1's pins stay green

  t-6  Route cited paths through pathfence; settle taint ledger      [risk; verification diverges, D1]
       files:    internal/facts/paths.go (new), internal/facts/paths_test.go (new), internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt,
                 internal/cmd/pathfence_carrier_test.go
       desc:     facts.ContainCitation(root, raw) (pathfence.Contained, error), with existence checked via
                 pathfence.Stat. A path escaping the root is reported missing with no fs call. A bare basename
                 resolves when any tree file has that name.
                 Moves Criterion.Text, Decision.Choice, Task.Description and Task.TestContract from not_paths.txt
                 to Consumed (carrier facts.ContainCitation), with a carrierType case added.
                 Task.Files stays NotConsumed (string-compared only).
       depends:  t-3, t-4 (shared pathfence files)
       covers:   c-1
       contract: `../../etc/passwd` is reported missing and the stat seam records zero calls for it;
                 `internal/cmd/state.go` resolves; bare `nosuch.go` is missing; bare `spec.toml` resolves via
                 .dross/phases/x/spec.toml;
                 `./internal/x.go` and `internal/x.go` compare equal (path.Clean) against the planned set;
                 TestNoDeclaredPathFieldReachesOS reports nothing in internal/facts, and re-filing Criterion.Text in
                 the ledger turns the ContainCitation Stat into a finding at that line;
                 TestDeclaredCarriersAreContained binds facts.ContainCitation to pathfence.Contained, and a
                 string-returning carrier fails it;
                 each moved field appears exactly once (registry, not ledger) or TestEveryPathShapedFieldIsDeclared
                 fails

Wave 3 (depends t-4, t-5, t-6)
  t-7  Assemble facts check over scanned fields                      [risk+verification+mvp]
       files:    internal/facts/facts.go (new), internal/facts/facts_test.go (new)
       desc:     Check(repoDir, spec, plan *phase.Plan) → Report{Unresolved []Finding{Field, Kind, Citation,
                 Status, NewLine}, Degraded}. It scans only the locked scanned_fields.
                 Planned set = every task's files, after Clean. New set = spec.New, compared after normalising.
                 Ordering is deterministic (spec order, then plan order).
                 Finding.String() renders "<field>: <kind> <citation>[ now at line N]".
       depends:  t-4, t-5, t-6
       covers:   c-1, c-2, c-3
       contract: the same missing path cited only in a decision `why`, a [[deferred]] text/why or an [[edges]]
                 reason is NOT reported;
                 the Field labels are exact: "criteria c-2", "decisions symbol_shape choice", "task t-3 description",
                 "task t-3 test_contract[1]";
                 a path in task t-2's files suppresses the same path cited in a criterion AND in another task's
                 description; a nonexistent `files` entry itself yields no finding;
                 spec.New ["foo.go"] suppresses `foo.go:12.` and `foo.go` but not `fooo.go`;
                 spec.New ["pkg.Name"] suppresses `` `pkg.Name()` ``;
                 a nil plan checks the spec alone with no error;
                 two runs over the same inputs return byte-identical reports

Wave 4 (depends t-7)
  t-8  Add `dross phase facts` read-only command                    [risk+verification]
       files:    internal/cmd/phase_facts.go (new), internal/cmd/phase_facts_test.go (new), internal/cmd/phase.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross phase facts <id> [--json]`. The spec is required and the plan optional. It prints one
                 Finding.String() line per finding, or "nothing unresolved" naming what was checked (spec only /
                 spec + plan), and exits non-zero on any finding. It does not narrate `mark-new` (that lands in
                 t-9).
       depends:  t-7
       covers:   c-6, c-9, c-1, c-2, c-3
       contract: a clean spec with no plan.toml exits 0 and prints "nothing unresolved" plus "spec only";
                 one missing path → non-zero, and the line names "criteria c-1" and the path;
                 --json emits {phase, checked, unresolved:[{field,kind,citation,line,new_line}]}; stdout parses
                 alone with errors on stderr; `unresolved` is [] (never null) when clean; it still exits non-zero
                 when not empty;
                 synthetic moved / gone / unresolved-symbol Findings render with their kind and new line;
                 a hash of every file under the fixture root is identical before and after, clean or not, and no
                 *.tmp remains;
                 no spec.toml → non-zero naming .../spec.toml; unknown id → non-zero naming the phases/<id> dir;
                 id `../x` refused; none of these prints "nothing unresolved";
                 a spec or plan.toml that fails to decode → non-zero naming that file;
                 cli_tree.txt has the `phase facts` row and its --json flag row

Wave 5 (depends t-8)
  t-9  Add `dross phase mark-new` spec writer                       [risk; verification+mvp reject, D2]
       files:    internal/cmd/phase_mark_new.go (new), internal/cmd/phase_mark_new_test.go (new),
                 internal/cmd/phase.go, cmd/dross/testdata/cli_tree.txt, internal/cmd/phase_facts.go
       desc:     `dross phase mark-new <id> <citation>...` normalises each citation through facts.Extract, refuses
                 any that isn't currently unresolved, and appends deduped entries to spec.New via spec.Save.
                 phase_facts.go's text output gains the "confirm new with `dross phase mark-new`" hint.
       depends:  t-8
       covers:   c-4
       contract: mark-new `foo.go:12.` stores "foo.go:12" and the next `dross phase facts` exits 0;
                 mark-new `fooo.go` (not unresolved) exits non-zero naming it, with the spec unchanged on disk;
                 marking the same citation twice leaves one entry;
                 the written spec keeps `new` above `[phase]` and preserves every criterion, decision, edge and
                 deferred entry (LoadSpec before == after, except New);
                 a spec that fails to decode is refused without writing;
                 cli_tree.txt has the `phase mark-new` row

  t-10 Gate plan-lock on unresolved citations                       [risk+verification+mvp]
       files:    internal/cmd/state.go, internal/cmd/state_facts_gate_test.go (new)
       desc:     planLockFactsGate runs after planLockEdgeGate on `current_phase_status planned`. It calls
                 facts.Check through the same spec/plan loader as `phase facts` and refuses listing every
                 Finding.String() line plus `dross phase facts <id>` and spec `new` (no `mark-new`: t-9 runs in
                 parallel).
                 Absence allows (no current phase / no spec), mirroring the edge gate. A malformed spec or plan
                 refuses, naming its path.
                 Before writing, check existing internal/cmd fixtures that plan-lock a spec (state_edges_gate_test,
                 execute_test, phase_test, …) for citations that now refuse. Fold any fixture edits into this
                 task's files via `dross task edit` [mvp].
       depends:  t-7, t-8
       covers:   c-5
       contract: 3 unresolved citations split across spec and plan → refused; the error lists all 3 and names
                 `dross phase facts <id>`; state.json's current_phase_status is unchanged on disk;
                 the same fixture with each citation in spec `new` (or the path in a task's files) locks;
                 a fixture table (clean / missing path / moved line / why-only citation / new-marked / planned
                 file) runs through the gate and `dross phase facts --json`: the gate refuses iff the command's
                 unresolved list is non-empty, and the gate's lines equal the command's lines;
                 a malformed plan.toml refuses, naming the plan path;
                 `in_progress` / `executing` never run the gate; no current phase or no spec.toml allows
                 (TestPlanLockAbsenceAllows and execute_test's spec-less fixture stay green);
                 on a spec with both problems the edge gate still fires first

Wave 6 (depends t-8, t-9)
  t-11 Walk unresolved citations in /dross-plan                    [risk+verification]
       files:    assets/prompts/plan.md, assets/prompts/_plan.facts.md (new), assets/partials.go,
                 assets/partials_test.go, assets/testdata/prompt_instructions.txt, internal/cmd/plan_prompt_test.go
       desc:     plan.md §1 runs `dross phase facts <id> --json` before §2 and carries the fetch line
                 **When** `facts=unresolved` — `dross phase facts` exited non-zero: `dross prompt partial plan facts`.
                 The partial walks one citation per AskUserQuestion turn (fix the spec text / confirm new via
                 `dross phase mark-new`) and re-runs facts until it exits 0, then composes.
                 TriggerVocabulary gains "facts": {"clean", "unresolved"}.
       depends:  t-8, t-9
       covers:   c-4
       contract: TestPromptByteBudget (assets/budget_test.go) stays green for plan.md (14452 today, cap 15000);
                 the walk lives in the partial;
                 the facts run in expanded plan.md comes before "## 2. Goal-backward decomposition", and moving it
                 after fails a plan_prompt_test assertion;
                 the last line before `## 2G` is still the panel fetch line (existing pin stays green);
                 the expanded partial names AskUserQuestion, one citation per turn, both outcomes,
                 `dross phase mark-new`, and the re-run before §2;
                 TestCommonPathVocabularyPinned and TestCommonPathFetchesNothing carry facts=clean as the common
                 path (the length check fails without the partials_test.go edit);
                 TestPromptInstructionsConserved carries the new plan.md rows (append-only manifest)
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-3, t-6, t-7, t-8 |
| c-2 | t-3, t-5, t-7, t-8 |
| c-3 | t-2, t-3, t-5, t-7, t-8 |
| c-4 | t-4, t-9, t-11 |
| c-5 | t-4, t-10 |
| c-6 | t-8 |
| c-7 | t-3 |
| c-8 | t-1, t-2, t-5 |
| c-9 | t-8 |

9/9 covered.

### Grafts applied to the risk skeleton

- **t-2:** `Symbol.Package` via go_indexer.go [verification]. risk's strict matcher needed a package name and named no source for it.
- **t-3:** verification's non-path exclusions (`origin/main`, `~/`, `<*{`, absolute paths), leading-dot survival and `foo.go:12:`.
- **t-4:** a mixed path+symbol round-trip [verification].
- **t-5:** the Classify-matcher split contract [verification], the ResolveAllIn leak test [mvp], the index-not-built contract [mvp], and the real-tree regression [verification].
- **t-6:** `depends t-4`, because both edit the pathfence files.
- **t-7:** Finding.String() [verification] and the "nonexistent files entry yields nothing" contract [mvp].
- **t-8:** the checked scope, the JSON shape and the synthetic-renderer contract [verification].
- **t-10:** the 3-citation spec+plan fixture, equal-lines parity, and the edge-first contract [verification]; the existing-fixture check [mvp].
- **t-11:** `assets/partials_test.go` plus the vocabulary-pin contracts [verification], and the §2G pin note (repo check).

## Disagreements

**D1. How cited paths satisfy the path-taint audit.**
- **mvp:** keeps the four prose fields in not_paths.txt and runs Contain → Stat, then calls architecture.Resolve on the contained path.
- **risk:** reclassifies Criterion.Text, Decision.Choice, Task.Description and Task.TestContract as Consumed, with carrier `facts.ContainCitation`.
- **verification:** keeps them in the ledger, walks the tree into a set and only does membership lookups, building the symbol index from walked paths.
- **Repo check:** mvp fails, because ledger taint survives Contain and `codex.Index` → `os.ReadDir` on the cited dir is a second sink. risk and verification both pass.
- **Default: risk.** It is the remedy the audit and the ledger header prescribe, and it keeps the c-2 lazy per-file index legal.
- **Why it matters:**
  - Under verification, any later edit that fetches a value by a cited-path key and opens it (the engine taints Lookup results from tainted keys) becomes a finding. c-2 must also pre-index every tree source file.
  - Under risk, four prose fields are registered as "path fields". That is semantically odd and permanently widens the Consumed surface the carrier test guards.

**D2. Who writes spec `new`.**
- **risk:** adds a `dross phase mark-new` writer (t-9). It normalises the citation, refuses one that isn't currently unresolved, dedupes, and places `new` above `[phase]` via Save.
- **verification and mvp:** have the prompt hand-edit spec.toml, relying on facts normalising entries on read.
- **Repo check:** LoadSpec is a non-strict BurntSushi DecodeFile, and specs end in `[[edges]]`/`[[deferred]]` tables. A `new = [...]` appended at EOF lands inside the last table entry and is dropped silently, so the citation stays unresolved forever.
- **Default: keep t-9.**
- **Why it matters:** one more task, CLI surface and wave (t-11 must wait for t-9 because of the narrated-command guard), weighed against a c-4 walk that can look like it worked while recording nothing.

**D3. Reuse architecture.Resolve as-is, or refactor to a shared Classify with an injectable matcher.**
- **mvp:** leaves links.go untouched, so c-8 holds by construction. c-2's paired symbol goes through `matchSymbols` with its lastComponent fallback.
- **risk and verification:** export `Classify(link, syms, match)` and pass a strict matcher for facts. This requires the t-1 pin written before the refactor.
- **Default: refactor (t-1 + t-5).** The locked symbol_shape forbids last-component fallback for any backticked symbol, including one paired with file:line. mvp's route also runs findCallers and git log per citation and trips the taint audit (D1).
- **Why it matters:** touching links.go is the only way c-8 can regress. The refactor makes t-1's observed-green-on-old-code pin load-bearing, and it is why t-1 is a separate wave-1 task.

**D4. What c-3 does when ast-grep is absent.**
- **risk:** a repo-wide Degraded flag. With any SupportsFile && !CanIndex file in the tree, every unmatched symbol is Skipped.
- **verification:** a per-file `indexed=false` from FileSymbols. A symbol is skipped only when its qualifier names a tree file or dir whose sources can't be indexed.
- **mvp:** no handling, so an unindexed TS symbol is reported Gone.
- **Repo check:** ast-grep is not installed on this laptop, and dross tracks .ts/.svelte files. Under risk's rule, c-3 is effectively off for dross's own phases on this machine.
- **Default: risk's Degraded.** It is the only rule that guarantees c-3's "a file codex can't index is never reported" for unqualified call-shaped symbols.
- **Why it matters:** risk trades c-3 detection on ast-grep-less machines for zero false refusals. Plan-lock then passes locally where it would refuse on a host with ast-grep, which is the machine-dependence risk claims to avoid, only in the other direction. verification keeps detection but can false-report an unqualified `Widget()` defined in a .ts file. **This one deserves a user call before lock.**

**D5. External-package qualifiers (`os.Stat`, `cobra.Command`, `exec.LookPath`).**
- **verification:** skips any qualifier that a tree Go file imports from outside the module.
- **risk and mvp:** silent, so under strict matching these report as unresolved.
- **Default: report them (no skip).** c-3's text ("matches no symbol the codex index finds in the tree") and the locked symbol_shape ("`pkg.Name` needs package pkg to define Name") list no such exemption. Adding one is a spec-level change, not a planning choice. Measured impact is low: 5 backticked stdlib citations across 151 phase dirs.
- **Why it matters:** with no skip, the c-4 walk will ask whether `exec.LookPath` is "new in this phase". The honest answer is "fix the spec" (unbacktick it), and marking it new would record a false fact. If the user prefers verification's skip, amend symbol_shape first.

**D6. Where spec `new` is filed for the field audit.**
- **risk and mvp:** pathfence.Fields() as NotConsumed, naming its reader.
- **verification:** not_paths.txt.
- **Default: fields.go NotConsumed.** The planner brief says a field that can hold paths goes to fields.go, and the ledger header says to file a row there "only if the value is not a path". `new` holds paths and symbols.
- **Why it matters:** low. Both pass the audit while nothing opens `new`. It changes t-4's file list and which test guards a future reader that opens it (NotConsumed names its readers; a ledger row does not).

**D7. Extraction scope at the edges.**
- **risk:** extracts absolute paths and reports them missing with no fs call. It has no directory citations, and nothing in it excludes templated paths.
- **verification:** never extracts absolute paths, `~/…`, `origin/main`, `milestone/v1.9`, or tokens with `<` `*` `{`. It resolves directory citations ending `/`.
- **mvp:** excludes `.dross/phases/<id>/spec.toml`.
- **Default:** verification's exclusions in t-3, with `../` escapes still reported missing (risk + mvp). Directory citations are left out, because c-1 says "file path".
- **Why it matters:** each of these shapes recurs in dross specs (`~/.claude`, `.dross/phases/<id>/…`, `milestone/v1.9`). Extracting them produces false plan-lock refusals on nearly every phase. Dropping directory citations means a mistyped `internal/codexx/` goes unreported.

**D8. Wave structure: does the CLI wait for symbol resolution, and do the CLI and gate share a task.**
- **verification:** 4 waves. A path-only check.go ships first and t-8 runs in parallel with symbol resolution, tested on synthetic moved/gone findings.
- **risk:** 6 waves. t-7 assembles after both engines, then t-8, then t-9 ∥ t-10, then t-11.
- **mvp:** one task holds both the CLI and the gate, so parity is a single test (6 files, two commands).
- **Default: risk's ordering, CLI and gate separate.**
- **Why it matters:** verification's split saves a wave but makes check.go a file two tasks edit in sequence. mvp's merge makes a two-command task that breaks the 5-file guideline. risk's chain also keeps the narrated-command guard satisfied (t-10 after t-8, t-11 after t-9) with no temporary narratedIgnore entry.
