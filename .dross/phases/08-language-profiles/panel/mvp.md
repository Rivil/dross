# Phase 08-language-profiles — MVP plan

Bias: smallest task set that satisfies every criterion. The phase-07 keystone makes
adding a stack pure data: a `<id>.toml` under `internal/stack/profiles/` is auto-embedded
(embed.go globs `profiles/*.toml`), surfaces in `dross stack list/show`, and feeds
`ScannersFor`/`AnalyzersFor` because those key off `profile.id == extLang token`. The only
mechanism edit the spec permits is three extLang lines. So: one TOML task, one extLang task,
one proof-test task. Nothing else is traceable to a criterion.

```
Phase 08-language-profiles — 3 tasks across 2 waves

Wave 1
  t-1  Add four embedded stack profiles
       files:    internal/stack/profiles/kotlin.toml,
                 internal/stack/profiles/dart.toml,
                 internal/stack/profiles/svelte.toml,
                 internal/stack/profiles/sql.toml
       covers:   c-2, c-3, c-4
       contract: with t-2's extLang edit, AnalyzersFor("kotlin") includes detekt,
                 AnalyzersFor("dart") includes dcm, AnalyzersFor("svelte") includes
                 eslint, AnalyzersFor("sql") includes sqlfluff — each tagged kind=analyzer,
                 detekt/dcm/eslint dimension=complexity and sqlfluff dimension=dead-code;
                 if sqlfluff is mis-tagged with a cosmetic/unknown dimension it fails
                 quality.TestCatalogExcludesCosmetic. ScannersFor("kotlin"/"dart"/
                 "svelte"/"sql") returns ONLY the three agnostic scanners (no kind=scanner
                 tool present) — a stray scanner makes ScannersFor return len>3. kotlin/dart/
                 svelte each declare [runtime.test/typecheck/format/build]; sql declares no
                 [runtime] table — a missing kotlin runtime.test.run breaks runtime seeding.

  t-2  Map .dart/.svelte/.sql in extLang
       files:    internal/stack/detect.go
       covers:   c-1
       contract: add ".dart"->"dart", ".svelte"->"svelte", ".sql"->"sql" to the extLang
                 map (.kt->kotlin already present). If any line is omitted, DetectLanguages
                 on a tree containing that extension omits the language and the proof test
                 for that profile (t-3) fails to resolve its id.

Wave 2 (depends t-1, t-2)
  t-3  Prove detection + analyzer per profile
       files:    internal/stack/detect_test.go,
                 internal/quality/catalog_test.go
       covers:   c-1, c-5
       contract: in detect_test.go, a table-driven test builds a fixture tree per stack
                 (a .kt / .dart / .svelte / .sql file, plus the profile's marker file where
                 one is declared) and asserts Detect(dir, Embedded()) returns the matching
                 id; a control fixture with an unrelated extension (e.g. only .txt) asserts
                 Detect returns stack.Unsupported, not a false kotlin/dart/svelte/sql match.
                 In catalog_test.go, a table-driven test asserts AnalyzersFor(id) contains
                 the dedicated analyzer (detekt/dcm/eslint/sqlfluff) for each id. If detection
                 stops resolving an id, or AnalyzersFor drops a dedicated analyzer, the
                 matching row fails — proving the keystone holds with no mechanism change
                 beyond the t-2 extLang lines.
```

## Coverage

| criterion | tasks            |
| --------- | ---------------- |
| c-1       | t-2, t-3         |
| c-2       | t-1              |
| c-3       | t-1              |
| c-4       | t-1              |
| c-5       | t-3              |

c-2 (`dross stack list`/`show`) is delivered structurally by t-1: embed.go auto-discovers any
`profiles/*.toml`, so a valid embedded TOML is necessarily listed and loadable. t-3's
`Detect(dir, Embedded())` exercises the embed path end-to-end, so a TOML that fails to embed
or decode also fails t-3 — c-2 is implicitly guarded rather than given its own CLI smoke task.

## Judgment calls

- One TOML task, not four. Chose a single t-1 over per-profile tasks: each TOML is the same
  data shape, all four are independent same-layer edits, and four tasks would be four <10-min
  files — merge-it territory. Rejected per-stack splitting as speculative structure.
- Folded the c-2 CLI check into t-3's `Embedded()` assertions rather than a dedicated
  `dross stack list/show` smoke task. Chose this because embed.go's glob guarantees listing
  for any decodable TOML, so a separate CLI task would test the framework, not this phase's
  work. Rejected a standalone c-2 task.
- Detection tests use real `Embedded()` profiles against temp-dir fixtures, not hand-built
  Profile structs (as TestDetect_SecondProfileSelected does). Chose real embedded data so the
  test proves the shipped TOMLs detect — a struct-only test would pass even if the TOML were
  malformed. Rejected the struct-mirror pattern for the new profiles.
- extLang edit (t-2) is its own wave-1 task, not merged into t-1. Chose to keep it separate
  because it is the only mechanism-code edit and the only thing covering c-1's map requirement;
  merging a .go edit into a TOML-data task would blur the "data vs mechanism" line the keystone
  rests on. It still runs in wave 1 (no dependency on t-1 to author).
- One proof-test task spanning two test files, not split by package. Chose to keep detection
  proof and analyzer proof together as the single c-5 "keystone holds" deliverable; they share
  the same per-stack table and splitting would fragment one criterion across two tasks.
```
