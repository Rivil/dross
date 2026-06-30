# MVP plan — architecture-doc-enhancements

Phase architecture-doc-enhancements — 5 tasks across 2 waves

Wave 1
  t-1  Add typed landmark to changes model + CLI
       files:    internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/changes.go
       covers:   c-1
       contract: Add a structured `Landmarks []Landmark{Feature,Symbol,Loc,What}`
                 field to TaskRecord (Notes stays a valid free-form field, no
                 longer a landmark — legacy_notes). `dross changes record` gains a
                 repeatable `--landmark key=value` flag parsed into that array.
       test:     If Record/Load drop the typed fields, a round-trip test asserting
                 reloaded Tasks["t-1"].Landmarks[0].Feature=="x" fails; if the
                 --landmark parser mis-splits a `feature=...` / `loc=file:line`
                 pair (e.g. on the `:` in loc), the changes-record flag test fails.

  t-2  Lift architecture regen guard; merge over existing
       files:    assets/prompts/architecture.md
       covers:   c-2
       contract: Remove §0.3's first-creation-only stop and replace it with the
                 refresh_merge_strategy: per-entry merge keyed by feature heading
                 (refresh symbol bullets + provenance in place, keep existing
                 one-line unless empty, add new capabilities, flag-not-drop entries
                 the scan missed), gated by the existing propose→approve diff.
       test:     Grep guard — if architecture.md still contains the "stop: this
                 engine is scoped to generating the doc when it's absent" sentence,
                 or still lacks a feature-keyed merge step, the prompt-content check
                 fails.

  t-3  Symbol-link resolver + doctor stale-link warning
       files:    internal/architecture/links.go, internal/architecture/links_test.go,
                 internal/cmd/doctor.go
       covers:   c-3
       contract: New links.go parses `Symbol — file:line` bullets from
                 ARCHITECTURE.md and resolves each symbol's current line by textual
                 search in its file, classifying ok / moved / missing. doctor.go
                 calls it and prints `⚠` advisories WITHOUT incrementing `issues`.
       test:     On a fixture where a bullet says `:3` but the symbol token sits on
                 line 10, links_test must classify it moved; a doctor test must
                 assert doctor still returns nil (exit 0) when stale links are
                 present — if the check increments issues and blocks, it fails.

Wave 2
  t-4  Wire execute + ship prompts to typed --landmark   (depends t-1)
       files:    assets/prompts/execute.md, assets/prompts/ship.md
       covers:   c-1
       contract: execute.md §landmark capture emits `--landmark feature=… symbol=…
                 loc=file:line what=…` instead of folding the landmark into
                 `--notes`; ship.md §3.5 reads the structured landmark fields from
                 `dross changes show` instead of parsing the notes string.
       test:     Grep guard — if execute.md's landmark block still routes the
                 landmark through `--notes` (no `--landmark`), or ship.md §3.5 still
                 reads `notes` as the landmark source, the prompt-content check fails.

  t-5  Add `dross architecture check --fix` command       (depends t-3)
       files:    internal/cmd/architecture.go, internal/cmd/architecture_test.go,
                 cmd/dross/main.go
       covers:   c-4
       contract: New `architecture check --fix` cobra subcommand, registered in
                 main.go root.AddCommand, reuses t-3's resolver to rewrite moved
                 symbols' line numbers in place in ARCHITECTURE.md; skips
                 renamed/deleted symbols.
       test:     A registration guard test (mirroring interaction_test/techdebt_test)
                 fails if `architecture` is absent from root.AddCommand; a fix test
                 on a fixture ARCHITECTURE.md must assert a symbol that moved 3→10 is
                 repaired to `:10` while a deleted symbol's bullet is left untouched —
                 if --fix rewrites or drops the deleted one, it fails.

## Coverage
- c-1 → t-1 (typed flag + changes.json model), t-4 (execute emits / ship reads)
- c-2 → t-2 (guard lifted + feature-keyed merge in architecture.md)
- c-3 → t-3 (resolver + advisory, non-blocking doctor warning)
- c-4 → t-5 (`architecture check --fix`, reuses t-3 resolver)

All criteria c-1..c-4 accounted for.

## Judgment calls
- Folded the c-3 stale-link resolver engine into the doctor task (t-3) and let
  c-4 reuse it, rather than a standalone "engine" task — rejected a 3rd task
  because the resolver has no user-facing surface of its own; one function serves
  both consumers, keeping the count at 5.
- Kept c-1's prompt wiring (t-4) separate from its Go flag (t-1) — rejected
  merging them because the combined task hits 5 files (the split threshold) and
  mixes go-tested code with grep-only prompt edits, muddying the contract.
- Chose a language-agnostic textual symbol resolver (search the file for the
  symbol token) over a Go-AST / `dross codex` resolver — rejected AST because MVP
  only needs "does the symbol still exist, and on what line," which textual search
  answers for any linked file type, not just Go.
- Made t-4 wave-2 depends-t-1 instead of parallel — the prompt names the exact
  `--landmark` key set t-1 defines, so it is a real contract dependency; the
  small parallelism loss buys protection against flag/prompt drift.
- Treated c-2 as a pure prompt edit (no Go regen engine) — rejected adding Go for
  the merge because `/dross-architecture` is prompt-only and refresh_merge_strategy
  is executed by the LLM under the existing propose→approve diff gate.
