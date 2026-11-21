# risk lens — spec-fact-drift

Failure modes drive the graph. Each risk below has exactly one owning task, and that task's test_contract is the test that fails when the risk happens.

## Risk register (risk → owning task)

| # | What can break | Owner |
|---|---|---|
| R1 | The refactored resolver quietly changes `dross architecture check` / doctor output (c-8). Today `architecture.EntryTemplate` resolves only through `lastComponent`. | t-1 pins the output first; t-5 must keep that pin green |
| R2 | `codex.SupportsFile` is true for .ts/.svelte/.cs/.gd even when ast-grep is absent, so their Symbols() come back empty. A tree-wide index would then call every TS symbol "gone", and plan-lock would pass or fail depending on the machine. | t-2 (`CanIndex`); t-5 (degraded index → skip) |
| R3 | `codex.Index` runs findCallers + git log for every call. Run per file across about 1100 Go files, it is a performance cliff. | t-2 (`codex.Symbols`, symbols only) |
| R4 | Extraction grabs junk (`e.g.`, `v1.9`, URLs, `paths.source`, `--json`, `dross phase facts`), or leaves punctuation on a citation (`foo.go:12.`). Either way the false positives land in plan-lock. | t-3 |
| R5 | Something hand-written as `new = [...]` after `[phase]`, or appended at EOF, decodes as `phase.new` or as an edge key. The decoder is non-strict, so it is dropped silently and the citation stays unresolved forever. | t-4 (schema placement), t-9 (writer) |
| R6 | Path-taint audit: `phase.Criterion.Text`, `Decision.Choice`, `Task.Description` and `Task.TestContract` are filed in not_paths.txt. Statting a path extracted from them is a finding even through Contain. `Task.Files` is NotConsumed, so opening it is also a finding. | t-6 |
| R7 | A cited `../../etc/passwd` or `/etc/hosts` gets statted outside the repo, or it "resolves" because it exists on the host. | t-6 |
| R8 | The `why`, [[deferred]] or edge text gets scanned. The motivating `quoted.String()` sits in a deferred `why`, so it would be reported. | t-7 |
| R9 | The symbol matcher keeps a last-component fallback, so `quoted.String()` resolves against any `String` method (locked symbol_shape). | t-5 |
| R10 | `--json` stdout gets corrupted by the error on the non-zero exit. A missing spec or unknown id reports "nothing unresolved". The command writes a `.tmp`. | t-8 |
| R11 | mark-new accepts a typo (`fooo.go`) and so silences nothing, stores the un-normalised form (`foo.go:12.`) so it never matches, or duplicates entries. | t-9 |
| R12 | The plan-lock gate and `dross phase facts` disagree (two rule sets), or a gate refusal leaves the status mutated. | t-10 |
| R13 | The plan.md byte cap. It is at 14452/15000 today, so an inline walk breaches it. The conservation manifest row goes missing. | t-11 |
| R14 | The narrated-command guard trips. state.go's refusal and the prompt name `dross phase facts` / `mark-new` before those commands are registered. | Ordering: t-10 and t-11 depend on t-8/t-9; t-8 must not narrate mark-new |

```
Phase spec-fact-drift — 11 tasks across 6 waves

Wave 1
  t-1  Pin architecture link-check output before refactor
       files:    internal/cmd/architecture_link_parity_test.go
       desc:     Characterisation test on the CURRENT code. It uses a fixture ARCHITECTURE.md + Go files covering
                 ok / moved / unresolved / ambiguous / skipped (no line, .py file), plus a `pkg.Name` bullet that
                 resolves only via the last-component fallback. It pins the exact `dross architecture check` stdout
                 and the exact architectureLinkWarnings slice.
       covers:   c-8
       contract: removing the lastComponent fallback from matchSymbols turns the `architecture.EntryTemplate`
                 bullet from silent into an "unresolved" line, and the pinned check stdout fails;
                 reclassifying an ambiguous bullet as unresolved adds a doctor warning, and the pinned warnings
                 slice fails;
                 any change to the "N moved, N unresolved, N ambiguous" summary line fails the stdout pin

  t-2  Add codex symbols-only and availability-aware CanIndex
       files:    internal/codex/codex.go, internal/codex/codex_test.go, internal/codex/ast_grep.go
       desc:     Export Symbols(file) (indexer dispatch only: no findCallers, no siblings, no git log) and
                 CanIndex(file) = SupportsFile && (Go || ast-grep available). SupportsFile is left byte-identical.
       covers:   c-3, c-8
       contract: with astGrepAvailableFn stubbed false, CanIndex("a.ts") is false while SupportsFile("a.ts")
                 stays true (a SupportsFile change would shift architecture's Skipped set);
                 CanIndex("a.go") is true with ast-grep stubbed absent;
                 Symbols(f) equals Index([f]).Symbols for a Go file in a dir that is not a git repo, and
                 returns no error where Index would shell out to git;
                 Symbols on a syntactically broken .go file returns an error and no panic

  t-3  Extract and classify citations from prose
       files:    internal/facts/extract.go, internal/facts/extract_test.go
       desc:     Extract(text) → []Citation{Kind: path|line|symbol, Raw, Path, Line, Symbol}. Path = an extension in
                 the closed allowlist (codex-indexed exts + toml/md/json/yml/yaml/txt/sh/mod/sum). A path with a `/`
                 is repo-relative; one without is a bare basename. file:line pairs with the backticked symbol
                 adjacent to it (`Sym` — f:N, `Sym` (f:N), `Sym` at f:N); an unpaired file:line is path-only.
                 Symbol = the locked symbol_shape (single backticked `Ident(.Ident)*` with optional `()`, dotted or
                 call-shaped). A dotted token with no uppercase component and no `()` is a TOML key, so it is skipped.
       covers:   c-7, c-1, c-2, c-3
       contract: `see foo.go:12.` / `(foo.go:12)` / `` `foo.go:12`, `` / `foo.go:12;` each yield Path "foo.go", Line 12;
                 `e.g.`, `i.e.`, `v1.9`, `go1.27.3`, `1.9.4.0` yield nothing (extension not in allowlist);
                 `https://github.com/x/y/blob/main/a.go` yields nothing (URL);
                 `` `dross phase facts` ``, `` `--json` ``, `` `paths.source` ``, `` `current_phase_status` ``,
                 `` `facts` `` yield no symbol;
                 `` `quoted.String()` ``, `` `phase.Spec` ``, `` `cmd.planLockEdgeGate()` `` yield symbols with `()` stripped;
                 `` `architecture.Resolve` — internal/architecture/links.go:192 `` yields one line citation that carries
                 Symbol "architecture.Resolve";
                 `` `spec.toml` `` classifies as a path (allowlisted ext), never as a symbol

  t-4  Add spec-level `new` list to the schema
       files:    internal/phase/phase.go, internal/phase/phase_test.go, internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go
       desc:     Spec.New []string `toml:"new,omitempty"` declared NotConsumed in pathfence.Fields() (string-compared,
                 never opened; reader internal/cmd/phase_facts.go). Name added to TestRegistryCoversEveryKnownFieldByName.
       covers:   c-4, c-5
       contract: Spec{New: ["a.go"]}.Save writes `new = ["a.go"]` BEFORE the `[phase]` header, and LoadSpec
                 round-trips it;
                 an empty New writes no `new` key (a legacy spec saves byte-identically);
                 a hand-written `new = [...]` placed under `[phase]` decodes to an empty Spec.New (pins the
                 silent-drop that t-9's writer exists to prevent);
                 deleting the phase.Spec.New registry entry fails TestEveryPathShapedFieldIsDeclared /
                 TestRegistryCoversEveryKnownFieldByName

Wave 2 (depends t-1, t-2, t-3)
  t-5  Strict symbol resolver sharing link-check classification
       files:    internal/architecture/links.go, internal/architecture/links_test.go, internal/facts/resolve.go,
                 internal/facts/resolve_test.go
       desc:     Export architecture.Classify(link, syms, match) — the resolveAgainst switch with a pluggable
                 matcher. architecture's own callers pass the existing matchSymbols, so output is unchanged.
                 facts.StrictMatch requires every qualifier: Go package clause name, then Type.Method; no
                 last-component fallback. A lazy tree index (WalkDir skipping .git and node_modules; parse errors
                 non-fatal) is built only when a symbol citation exists. It is marked Degraded when the tree holds
                 files with SupportsFile && !CanIndex.
       depends:  t-1, t-2, t-3
       covers:   c-2, c-3, c-8
       contract: `quoted.String()`, with a `String` method on an unrelated type in the tree, is Gone (a re-added
                 last-component fallback makes it resolve and the test fails);
                 `phase.Spec` resolves; `cmd.Spec` is Gone when Spec is defined only in package phase;
                 `Changes.Record` is ambiguous across two files when both define it, and an ambiguous result is
                 never in Unresolved;
                 a line citation `Resolve` — links.go:N with the func at N+3 is Moved with NewLine N+3, and Gone
                 after the func is deleted;
                 a line citation into a .py file is Skipped;
                 with astGrepAvailableFn stubbed false and a .ts file in the tree, an unmatched symbol is Skipped
                 and the index reports Degraded (not Gone);
                 a fixture tree with a deliberately unparsable testdata .go file still indexes the other files;
                 t-1's pinned architecture check / doctor output stays green

  t-6  Route cited paths through pathfence; settle taint ledger
       files:    internal/facts/paths.go, internal/facts/paths_test.go, internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt,
                 internal/cmd/pathfence_carrier_test.go
       desc:     facts.ContainCitation(root, raw) (pathfence.Contained, error) plus existence via pathfence.Stat.
                 An absolute path, or one escaping the root, is reported missing with no fs call. A bare basename
                 resolves if any file of that name exists in the tree. Moves phase.Criterion.Text,
                 Decision.Choice, Task.Description and Task.TestContract from not_paths.txt to Consumed
                 (carrier facts.ContainCitation). Task.Files stays NotConsumed: it is only ever string-compared.
       depends:  t-3
       covers:   c-1
       contract: `../../etc/passwd` and `/etc/hosts` are reported missing and the test's stat seam records zero
                 calls for them;
                 `internal/cmd/state.go` resolves; bare `nosuch.go` is missing; bare `spec.toml` resolves via
                 .dross/phases/x/spec.toml;
                 `./internal/x.go` and `internal/x.go` compare equal (path.Clean) against the planned set;
                 the path-taint scan (pathtaint_audit_test) reports no finding on internal/facts — reverting
                 Criterion.Text to the ledger turns the ContainCitation stat into a finding at that line;
                 TestEveryConsumedCarrierIsBound binds facts.ContainCitation to pathfence.Contained, and a carrier
                 that returns string fails it;
                 the moved fields appear exactly once (registry, not ledger) or TestEveryPathShapedFieldIsDeclared fails

Wave 3 (depends t-4, t-5, t-6)
  t-7  Assemble facts check over scanned fields
       files:    internal/facts/facts.go, internal/facts/facts_test.go
       desc:     Check(repoDir, spec, plan *phase.Plan) → Report{Unresolved []Finding{Field, Kind, Citation, Status,
                 NewLine}, Degraded}. Scans only criteria text, decisions choice, and task files/description/
                 test_contract (locked scanned_fields). The planned set = every task's files, after Clean. The new
                 set = spec.New, compared on the normalised citation. Ordering is deterministic (spec order, then
                 plan order).
       depends:  t-4, t-5, t-6
       covers:   c-1, c-2, c-3
       contract: a missing path cited only in a decision `why`, a [[deferred]] text/why or an [[edges]] reason
                 is NOT reported (scanning any of those fails the test);
                 Field labels are exact: "criteria c-2", "decisions symbol_shape choice", "task t-3 description",
                 "task t-3 test_contract[1]";
                 a path in task t-2's files suppresses the same path cited in a spec criterion AND in another
                 task's description;
                 spec.New ["foo.go"] suppresses `foo.go:12.` and `foo.go`, but not `foo.go` cited as `fooo.go`;
                 spec.New ["pkg.Name"] suppresses `` `pkg.Name()` ``;
                 a nil plan checks the spec alone and returns no error;
                 two runs over the same inputs return byte-identical reports

Wave 4 (depends t-7)
  t-8  Add `dross phase facts` read-only command
       files:    internal/cmd/phase_facts.go, internal/cmd/phase_facts_test.go, internal/cmd/phase.go,
                 cmd/dross/testdata/cli_tree.txt
       desc:     `dross phase facts <id> [--json]`. It loads the spec (required) and the plan (optional). It prints
                 one line per finding (field, kind, citation, moved→line), or "nothing unresolved". It exits
                 non-zero when any finding exists. It never narrates `mark-new` (that hint lands in t-9).
       depends:  t-7
       covers:   c-6, c-9, c-1, c-2, c-3
       contract: a clean spec exits 0 and prints "nothing unresolved"; a spec with no plan.toml checks the spec
                 alone and exits 0;
                 a spec with one missing path exits non-zero and the line names "criteria c-1" and the path;
                 with --json + a finding, stdout parses as JSON on its own (the error goes to stderr) and
                 `unresolved` is [] (never null) on a clean run;
                 a hash of every file under the phase dir is identical before and after, and no *.tmp is left;
                 a phase dir with no spec.toml exits non-zero naming .../spec.toml; an unknown id exits non-zero
                 naming the phases/<id> dir; id `../x` is refused; neither prints "nothing unresolved";
                 a spec or plan.toml that fails to decode exits non-zero naming that file;
                 cli_tree.txt has the `phase facts` row and its --json flag row

Wave 5 (depends t-8)
  t-9  Add `dross phase mark-new` spec writer
       files:    internal/cmd/phase_mark_new.go, internal/cmd/phase_mark_new_test.go, internal/cmd/phase.go,
                 cmd/dross/testdata/cli_tree.txt, internal/cmd/phase_facts.go
       desc:     `dross phase mark-new <id> <citation>...` normalises each citation through facts.Extract and
                 refuses any that is not currently unresolved. It appends deduped entries to spec.New via
                 spec.Save. phase_facts.go's non-json output gains the "confirm new with `dross phase mark-new`"
                 hint.
       depends:  t-8
       covers:   c-4
       contract: mark-new `foo.go:12.` stores "foo.go:12" and a following `dross phase facts` exits 0;
                 mark-new `fooo.go` (not in the unresolved set) exits non-zero naming it, and the spec is
                 unchanged on disk;
                 marking the same citation twice leaves one entry;
                 the written spec keeps `new` above `[phase]` and preserves every criterion, decision, edge and
                 deferred entry (LoadSpec before == after, except New);
                 a spec that fails to decode is refused without writing

  t-10 Gate plan-lock on unresolved citations
       files:    internal/cmd/state.go, internal/cmd/state_facts_gate_test.go
       desc:     planLockFactsGate runs after planLockEdgeGate on `current_phase_status planned`. It calls
                 facts.Check (the same engine as the command) over the spec + plan and refuses with every finding
                 listed. Absence allows (no current phase / no spec), as the edge gate does. A malformed spec or
                 plan refuses, naming its path.
       depends:  t-7, t-8
       covers:   c-5
       contract: a spec with two unresolved citations refuses, the error names both, and state.json's
                 current_phase_status is unchanged on disk;
                 a citation only in a plan task's description refuses;
                 the same citation listed in a task's files, or in spec.New, is allowed;
                 a table of fixtures runs through both the gate and `dross phase facts`, and the gate refuses iff
                 the command exits non-zero (a second rule set fails this parity test);
                 a malformed plan.toml refuses, naming the plan path;
                 `state set current_phase_status in_progress` is never gated;
                 the existing state_edges_gate_test cases stay green (edge gate still first)

Wave 6 (depends t-8, t-9)
  t-11 Walk unresolved citations in /dross-plan
       files:    assets/prompts/plan.md, assets/prompts/_plan.facts.md, assets/partials.go,
                 assets/testdata/prompt_instructions.txt, internal/cmd/plan_prompt_test.go
       desc:     plan.md §1 gets `dross phase facts <id> --json` before §2, plus the fetch line
                 "**When** `facts=unresolved` … `dross prompt partial plan facts`". The new partial walks one
                 citation per AskUserQuestion turn (fix the spec text / confirm new via `dross phase mark-new`)
                 and re-runs facts until it exits 0 before composing. TriggerVocabulary gains
                 "facts": {"clean", "unresolved"}.
       depends:  t-8, t-9
       covers:   c-4
       contract: TestPromptByteBudget stays under 15000 for plan.md (an inline walk breaches it, so it must be a
                 partial);
                 the facts run in expanded plan.md comes before "## 2. Goal-backward decomposition", and moving it
                 after fails a plan_prompt_test assertion;
                 the expanded partial names AskUserQuestion, one citation per turn, both outcomes and
                 `dross phase mark-new`;
                 the common path (facts=clean) fetches nothing, so "clean" must be the first vocabulary value;
                 TestPromptInstructionsConserved carries the new plan.md rows (append-only manifest)
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 path missing, naming field | t-3 (path extraction), t-6 (existence + containment), t-7 (field labels, planned/new suppression), t-8 (CLI surface) |
| c-2 file:line moved/gone, same classification | t-3 (line + paired symbol), t-5 (Classify shared with architecture), t-7, t-8 |
| c-3 backticked symbol unmatched; ambiguous/unindexable never reported | t-2 (CanIndex, Symbols), t-3 (locked shape), t-5 (strict match, ambiguous/degraded skip), t-7, t-8 |
| c-4 /dross-plan runs facts, one-per-turn walk | t-4 (`new` schema), t-9 (mark-new writer), t-11 (prompt + partial) |
| c-5 plan-lock refuses, same rule set | t-10 (with t-4 supplying `new`) |
| c-6 exit codes, nothing-unresolved, no writes, --json, spec-only | t-8 |
| c-7 punctuation/wrapping stripped | t-3 |
| c-8 architecture check/doctor output unchanged | t-1 (pin), t-2 (SupportsFile untouched), t-5 (refactor under the pin) |
| c-9 no spec / unknown id → non-zero naming path | t-8 |

9/9 criteria covered.

## Judgment calls

- **Taint compliance: reclassify the four prose fields as Consumed** (carrier facts.ContainCitation). Rejected: existence by membership in a WalkDir-built set, which never opens a tainted string. pathtaint_audit_test's own remedy text prescribes reclassification. The membership trick depends on how the engine treats map keys, and it would also miss ignored-but-present files.
- **A dedicated `dross phase mark-new` writer.** Rejected: having the prompt hand-edit `new = [...]`. LoadSpec is non-strict, so a `new` placed under `[phase]` or at EOF decodes to nothing and leaves the citation unresolved forever. c-6 forbids `phase facts` writing, so the writer is a separate verb.
- **mark-new refuses citations not currently unresolved.** Rejected: free-form append. A typo then silences nothing while looking recorded.
- **Strict matcher for c-2's paired symbol too, sharing only the classification switch.** Rejected: reusing architecture.Resolve as-is, which brings the last-component fallback. The locked symbol_shape applies to every backticked symbol citation, and the fallback is exactly what hid `quoted.String()`.
- **Refactor links.go into Classify(link, syms, matcher) rather than duplicating the switch.** Rejected: copying the moved/gone/ambiguous logic into facts. "Same classification" is enforceable only if there is one switch. t-1 pins the output first so the refactor is proven invariant (red-provable), not merely asserted.
- **t-1 as a standalone wave-1 characterisation task, despite being one test file.** Rejected: writing the pin inside t-5, where it would pin post-refactor behaviour and prove nothing.
- **A new `codex.CanIndex`; SupportsFile stays unchanged.** Rejected: making SupportsFile ast-grep-aware. That shifts architecture's Skipped set on machines without ast-grep and breaks c-8.
- **A degraded index (ast-grep absent with .ts/.svelte/.cs/.gd present) skips unmatched symbols repo-wide and says so.** Rejected: reporting them as gone, which makes plan-lock machine-dependent. Also rejected: skipping whenever any unindexable file (.sh, .py) exists, which would disable c-3 on dross itself.
- **Extension allowlist for path citations; bare basenames resolve anywhere in the tree.** Rejected: an open `name.ext` regex, which matches `e.g.`, `v1.9` and every `pkg.Name` symbol. Also rejected: requiring a `/`, which would miss `fooo.go` typos.
- **A dotted backtick token with no uppercase component and no `()` is a TOML key (skipped).** This is the only mechanical way to honour "TOML keys are skipped" (`paths.source`) while keeping `cmd.planLockEdgeGate()` (call-shaped) and `phase.Spec`.
- **Unpaired `file:line` (no adjacent backticked symbol) is checked as a path only.** Rejected: guessing the symbol from line content. With no named symbol, there is nothing to classify as moved.
- **Plan-lock keeps "absence allows" for a missing spec, while `phase facts` errors on it (c-9).** "Same rule set" (c-5) governs classification, not absence handling. Changing the gate's absence rule would break TestPlanLockAbsenceAllows for no criterion.
- **Facts gate runs after the edge gate, sequentially.** Rejected: merging both into one combined error. That would rewrite planLockEdgeGate's message and risk the existing state_edges_gate_test pins.
- **The c-4 walk lives in a `_plan.facts.md` partial behind a new `facts` trigger.** plan.md has 548 bytes of headroom under promptByteCap, and the walk text exceeds it.
- **The chain is ordered so that nothing narrates an unregistered command.** t-10 depends on t-8, t-11 on t-9, and t-8 does not mention mark-new. The alternative is a temporary narratedIgnore entry, which the guard asserts must be both used and needed. That makes it a churn source.
