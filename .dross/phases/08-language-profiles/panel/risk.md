# Phase 08-language-profiles — RISK lens

Bias: failure modes drive the graph. Each profile is a separate task because each
profile is a separate failure surface (wrong dimension, id≠token, missing runtime,
no dedicated scanner leaking in). The shared mechanism risks — extLang false-matching
and the cosmetic-dimension leak — are isolated into their own owned tasks so a
profile bug can't masquerade as a mechanism bug or vice-versa.

```
Phase 08-language-profiles — 7 tasks across 2 waves

Wave 1
  t-1  Add extLang entries for dart, svelte, sql
       files:    internal/stack/detect.go
       covers:   c-1
       contract: DetectLanguages on a tree containing only foo.dart returns
                 ["dart"] (today it returns []); same for .svelte->svelte,
                 .sql->sql. If .svelte is mistyped as ".svelt" or maps to
                 "javascript", the per-ext assertion for svelte fails. .kt is
                 left untouched (regression check: .kt still -> kotlin).

  t-2  Embed kotlin profile (detekt, gradle runtime)
       files:    internal/stack/profiles/kotlin.toml
       covers:   c-2, c-3, c-4
       contract: AnalyzersFor("kotlin") includes "detekt" with
                 Dimension=="complexity"; if id is written as "Kotlin" or the
                 file lists .kts under signals but id stays kotlin, ByID lookup
                 misses and AnalyzersFor("kotlin") omits detekt -> fails. detekt
                 carries kind="analyzer" (not "scanner"), so ScannersFor("kotlin")
                 must equal the 3 agnostic scanners only — a stray kind="scanner"
                 fails the "no dedicated scanner" assertion. Runtime.test resolves
                 to a non-empty "./gradlew test" command.

  t-3  Embed dart profile (dcm, dart runtime)
       files:    internal/stack/profiles/dart.toml
       covers:   c-2, c-3, c-4
       contract: AnalyzersFor("dart") includes "dcm" with Dimension=="complexity";
                 ScannersFor("dart") returns exactly the agnostic set (any
                 kind="scanner" tool in dart.toml fails the assertion). id=="dart"
                 (== extLang token) or the dedicated dcm never loads. Runtime.test
                 resolves to non-empty "dart test".

  t-4  Embed svelte profile (eslint+plugin, vite runtime)
       files:    internal/stack/profiles/svelte.toml
       covers:   c-2, c-3, c-4
       contract: AnalyzersFor("svelte") includes the eslint analyzer with
                 Dimension=="complexity"; the eslint-plugin-svelte requirement is
                 captured in the tool's Install string (assert Install contains
                 "eslint-plugin-svelte"). ScannersFor("svelte") == agnostic only.
                 id=="svelte". Runtime.test resolves to non-empty "vitest"/
                 "vitest run".

  t-5  Embed sql profile (sqlfluff, no runtime)
       files:    internal/stack/profiles/sql.toml
       covers:   c-2, c-3, c-4
       contract: AnalyzersFor("sql") includes "sqlfluff" with
                 Dimension=="dead-code" (NOT a new dimension); a fresh dimension
                 token e.g. "correctness" or any cosmetic dimension in sql.toml
                 trips TestCatalogExcludesCosmetic via IsSubstantive. The sql
                 profile declares NO [runtime] table — ResolveRuntime over the
                 sql profile yields empty Test/Build commands (asserts SQL seeds
                 nothing). id=="sql".

Wave 2 (depends t-1..t-5)
  t-6  Detection fixture tests incl. false-match guard
       files:    internal/stack/detect_test.go
       covers:   c-1
       contract: Detect on a temp dir holding one Main.kt (loading the embedded
                 set via Embedded()) returns "kotlin"; same single-ext fixtures
                 for .dart->dart, .svelte->svelte, .sql->sql. A control fixture
                 holding only foo.rb (ruby, unsupported here) must return
                 stack.Unsupported, NOT kotlin/dart/svelte/sql — if a new profile
                 over-matched (e.g. empty/over-broad signals) this assertion fails.

  t-7  Keystone test: detect+analyzer per profile, no mechanism edit
       files:    internal/stack/detect_test.go
       covers:   c-5
       contract: A table-driven test asserts, for each of
                 {kotlin:detekt, dart:dcm, svelte:eslint, sql:sqlfluff}, that
                 Detect on a single-ext fixture returns id AND
                 quality.AnalyzersFor(id) contains the dedicated analyzer name —
                 proving the only edits were the t-1 extLang lines + profile TOMLs
                 (no Detect/catalog code change). If detect.go's matcher were
                 special-cased per language, removing a profile's [signals] would
                 break this; if AnalyzersFor stopped being profile-derived, the
                 analyzer half fails.
```

## Coverage
- c-1 (extLang + fixtures + no false-match): t-1, t-6
- c-2 (4 profiles embedded, surface in stack list/show): t-2, t-3, t-4, t-5 (auto-embedded via go:embed; each loads cleanly through Embedded()/Decode validation exercised by its analyzer assertion)
- c-3 (dedicated analyzer per profile + agnostic-only scanners): t-2, t-3, t-4, t-5
- c-4 (runtime declared for kotlin/dart/svelte, none for sql): t-2, t-3, t-4, t-5
- c-5 (keystone: detect+analyzer, zero mechanism change): t-7

## Judgment calls
- One task per profile (t-2..t-5) rather than one "add 4 profiles" task: each profile is an independent failure surface (id≠token, wrong dimension, leaked scanner, missing/extra runtime) and the RISK lens demands each risk be owned and tested by exactly one task; a single bundled task would let one profile's bug hide behind another's green.
- extLang (t-1) split out from the profiles: detection-by-recon is a distinct failure mode (ext maps to wrong/no language) from profile loadout, and t-1 is the shared dependency t-6/t-7's fixtures rest on — isolating it pins the false-match risk to one place.
- Profile tasks are wave-1, not gated behind t-1: profiles are pure data files validated by Decode + AnalyzersFor (which keys off id, not extLang), so they don't need the extLang edit; only the *detection* tests (t-6/t-7) need both, hence wave-2. Rejected putting profiles in wave-2 as needless serialization.
- Separate t-6 (false-match guard) from t-7 (keystone): chose to keep the unsupported-repo control assertion (c-1's "does not false-match") in its own task so a too-broad signal in any profile fails a dedicated test, rather than burying it inside the keystone table where a passing-by-luck fixture could mask it. Rejected folding both into one detection test.
- sql dimension assertion (t-5) explicitly checks Dimension=="dead-code" AND relies on TestCatalogExcludesCosmetic: the locked decision forbids a new dimension; testing the exact token catches a well-meaning "correctness" dimension that would silently be a mechanism-code change breaking the keystone. Rejected only trusting the existing cosmetic test, which wouldn't catch a *substantive-looking* invented dimension.
- svelte plugin captured via Install-string assertion rather than a second tool entry: eslint-plugin-svelte is a dependency of the eslint analyzer, not a separate analyzer; adding it as its own [[tools]] entry would either need a bin (it has none) and fail Validate, or duplicate the dimension. Asserting Install mentions it keeps the requirement testable without a phantom tool.
