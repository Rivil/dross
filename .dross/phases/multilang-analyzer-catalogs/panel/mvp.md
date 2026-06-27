# MVP plan — multilang-analyzer-catalogs

Phase multilang-analyzer-catalogs — 4 tasks across 2 waves

Wave 1
  t-1  Add JS/TS loadout to svelte + typescript profiles
       files:    internal/stack/profiles/svelte.toml
                 internal/stack/profiles/typescript.toml
       covers:   c-1, c-2, c-4
       contract: Add the locked js_ts_loadout — scanners osv-scanner(core),
                 eslint-plugin-security, retire.js(optional); analyzers
                 eslint(complexity), knip(dead-code), dependency-cruiser(coupling),
                 typescript-eslint(error-handling) — duplicated verbatim into both
                 files (jsts_tool_sharing: no inherit). Every new tool carries an
                 install hint.
       contract: If a scanner is dropped, security TestScannersFor* (rewritten in
                 t-3) sees ScannersFor("svelte")/("typescript") miss osv-scanner.
                 If an analyzer's dimension is dropped below 3 substantive axes,
                 the quality dimension-count assert in t-3 fails for that id. If any
                 new tool ships without an Install hint, the pre-existing
                 quality.TestDetectMissingHasHint / security.TestAvailabilityMissingHasHint
                 over the expanded Catalog() go red (c-4).

  t-2  Add dart loadout to dart profile
       files:    internal/stack/profiles/dart.toml
       covers:   c-1, c-2, c-4
       contract: Add the locked dart_loadout — scanner osv-scanner on pubspec.lock
                 (optional); analyzers dcm(complexity), dcm-unused-code(dead-code),
                 dart-analyze(error-handling) — three substantive dimensions. Each
                 tool carries an install hint.
       contract: Dropping the scanner makes ScannersFor("dart") miss osv-scanner in
                 t-3's security assert; dropping any analyzer pulls dart below 3
                 dimensions and fails t-3's quality dimension-count assert. A
                 hint-less new tool reddens the pre-existing Detect-has-hint tests (c-4).

  t-4  Commit c-3 fixture (TypeScript dead-code)
       files:    internal/quality/testdata/c3-typescript/package.json
                 internal/quality/testdata/c3-typescript/tsconfig.json
                 internal/quality/testdata/c3-typescript/src/unused.ts
                 internal/quality/testdata/c3-typescript/README.md
       covers:   c-3
       contract: A TypeScript repo with a deliberately unused export that knip
                 (dedicated dead-code analyzer) flags but the agnostic
                 scc/jscpd/semgrep set does not. Lives under testdata/ so `go test`
                 ignores it; README records the exact dedicated-vs-agnostic commands
                 the verify step runs by hand. If the fixture has no
                 dedicated-only finding, the documented manual run in verify can't
                 show a delta and c-3 fails at verify.

Wave 2
  t-3  Update catalog tests to assert new loadouts
       files:    internal/security/catalog_test.go
                 internal/quality/catalog_test.go
       covers:   c-1, c-2
       depends_on: t-1, t-2
       contract: Rewrite security TestScannersForLanguageProfilesAgnosticOnly (which
                 currently asserts svelte/typescript/dart contribute ZERO scanners and
                 would break the instant t-1/t-2 land) into an assert that
                 ScannersFor("svelte"/"typescript"/"dart") each contains the dedicated
                 osv-scanner plus the agnostic set (c-1). Extend quality
                 TestAnalyzersForLanguageProfiles so the svelte/typescript/dart rows
                 assert AnalyzersFor(id) covers >=3 distinct substantive Dimensions
                 beyond complexity (c-2). If a profile under-declares, the named row
                 fails with the missing scanner / short dimension count.

## Coverage
- c-1 (dedicated scanner per lang, listed by detect): t-1, t-2, t-3
- c-2 (>=3 substantive analyzer dimensions per lang): t-1, t-2, t-3
- c-3 (dedicated tool surfaces a finding the agnostic set misses, fixture + verify): t-4
- c-4 (uninstalled declared tool reported unavailable w/ hint, run continues): t-1, t-2 (install hints + pre-existing Detect mechanism/tests)

## Judgment calls
- Merged svelte.toml + typescript.toml into ONE task (t-1): jsts_tool_sharing locks them as duplicated content of a single shared loadout; two near-identical edits are one logical change, well under the 5-file split line. Rejected splitting per-file as speculative structure.
- One consolidated test task (t-3) over both packages, not per-profile test edits: the security agnostic-only test is a single shared surface that both profile edits invalidate; splitting it would make t-1 and t-2 fight over the same file. Put it in wave 2 because the asserts must match the committed loadout from t-1/t-2.
- No new code/types and no c-4-specific code task: Detect() + its existing has-hint tests already implement "report unavailable with hint, continue" generically; new tools flow through Catalog() automatically, so c-4 is satisfied by giving each new tool an install hint. Rejected adding a bespoke c-4 surface as redundant.
- c-3 is a fixture-only task (t-4), no go-test gate: findings_proof is locked to a committed fixture + manual run recorded in verify, not CI. Chose TypeScript/knip dead-code as the cleanest dedicated-vs-agnostic delta; rejected wiring npm/dart into `go test`.
- t-3 is the only wave-2 task: it strictly needs the committed loadouts to pass. t-1/t-2/t-4 are mutually independent and run wave 1 for parallelism.
