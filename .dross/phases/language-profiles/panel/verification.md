# Phase 08-language-profiles — VERIFICATION lens

Designed backward from the test contracts. Each criterion's ideal failing test is
written first; the task is the smallest change that makes that named test pass.
The keystone is c-5: the four profiles must light up detection AND AnalyzersFor
with **no mechanism-code change** beyond the four extLang lines. So the plan splits
the *only* code edit (extLang in detect.go) into its own wave-1 task, keeps the
four profiles as pure-data wave-1 drop-ins, and makes every later task a test that
would fail if any wire were missed — never new mechanism.

## Test contracts derived first (the spec being coded to)

- **K-detect/lang**: `extLang[".dart"]=="dart"`, `[".svelte"]=="svelte"`, `[".sql"]=="sql"`,
  `[".kt"]=="kotlin"` (already). If a line is dropped, `DetectLanguages` on a fixture
  of that ext omits the language → AnalyzersFor/ScannersFor never load the dedicated tool.
- **K-profile-id**: each new `<id>.toml` has `id` exactly equal to its extLang token.
  If `dart.toml` shipped `id="flutter"`, `AnalyzersFor("dart")` would omit `dcm`.
- **K-detect-resolve**: `Detect(fixtureRoot, Embedded())` returns the right id per stack,
  and a non-matching fixture returns `Unsupported`, not a false match.
- **K-analyzer**: `AnalyzersFor("<id>")` contains the one dedicated analyzer + the
  agnostic pair; its dimension is substantive (passes TestCatalogExcludesCosmetic).
- **K-scanner-agnostic**: `ScannersFor("<id>")` returns ONLY {gitleaks,semgrep,trivy}.
- **K-runtime**: ResolveRuntime on kotlin/dart/svelte yields non-empty test+build;
  sql yields empty.

---

Phase 08-language-profiles — 9 tasks across 2 waves

Wave 1
  t-1  Add .dart/.svelte/.sql to extLang
       files:    internal/stack/detect.go
       covers:   c-1, c-5
       contract: K-detect/lang — a new test on a temp tree containing one .dart, one
                 .svelte, one .sql file expects DetectLanguages to include
                 "dart","svelte","sql"; deleting any one extLang line drops that
                 language from the returned slice and the test fails. This is the
                 ONLY mechanism edit the phase permits (the c-5 keystone).

  t-2  Add embedded kotlin profile
       files:    internal/stack/profiles/kotlin.toml
       covers:   c-2, c-3, c-4
       contract: id="kotlin"; signals.exts=[".kt",".kts"]; one analyzer detekt
                 kind=analyzer dimension=complexity core=true; runtime test/typecheck/
                 format/build via gradle; no scanner tools. Verified by t-6 (Detect),
                 t-7 (AnalyzersFor("kotlin") has detekt), t-8 (ScannersFor agnostic-
                 only), t-9 (ResolveRuntime non-empty). If id != "kotlin",
                 AnalyzersFor("kotlin") omits detekt → t-7 fails.

  t-3  Add embedded dart profile
       files:    internal/stack/profiles/dart.toml
       covers:   c-2, c-3, c-4
       contract: id="dart"; signals.files=["pubspec.yaml"]; signals.exts=[".dart"];
                 one analyzer dcm kind=analyzer dimension=complexity core=true; runtime
                 dart test/analyze/format/compile; no scanner tools. Verified by
                 t-6/t-7/t-8/t-9. If dimension were cosmetic, t-7's
                 TestCatalogExcludesCosmetic assertion on dcm fails.

  t-4  Add embedded svelte profile
       files:    internal/stack/profiles/svelte.toml
       covers:   c-2, c-3, c-4
       contract: id="svelte"; signals.files=["svelte.config.js"]; signals.exts=
                 [".svelte"]; one analyzer eslint (bin eslint, install pulls
                 eslint-plugin-svelte) kind=analyzer dimension=complexity core=true;
                 runtime vitest/svelte-check/prettier/vite build; no scanner tools.
                 Verified by t-6/t-7/t-8/t-9. If exts omit ".svelte", Detect on the
                 svelte fixture returns the wrong id → t-6 fails.

  t-5  Add embedded sql profile
       files:    internal/stack/profiles/sql.toml
       covers:   c-2, c-3
       contract: id="sql"; signals.exts=[".sql"]; one analyzer sqlfluff kind=analyzer
                 dimension=dead-code core=true; NO runtime block; no scanner tools.
                 Verified by t-7 (sqlfluff present, dimension dead-code passes
                 TestCatalogExcludesCosmetic), t-8, and t-9 (ResolveRuntime("sql")
                 test+build both empty). If sqlfluff used a non-allowlisted dimension,
                 TestCatalogExcludesCosmetic fails on sqlfluff.

Wave 2 (depends t-1..t-5)
  t-6  Test detection resolves each new profile id
       files:    internal/stack/detect_test.go
       covers:   c-1, c-5
       contract: K-detect-resolve — for each of {kotlin:.kt, dart:pubspec.yaml,
                 svelte:svelte.config.js, sql:.sql} a fixture built in t.TempDir,
                 Detect(dir, Embedded()) == that id; AND a fixture with only a
                 README/.txt returns Unsupported (no false-match). Plus a polyglot
                 guard: a .sql file dropped beside go.mod still detects "go" (file
                 signal beats ext), so .sql doesn't hijack a Go repo. Uses
                 stack.Embedded() so it proves the SHIPPED profiles, not inline stubs;
                 deleting kotlin.toml fails the kotlin case.
       depends_on: t-1, t-2, t-3, t-4, t-5

  t-7  Test AnalyzersFor surfaces each dedicated analyzer
       files:    internal/quality/catalog_test.go
       covers:   c-3, c-5
       contract: K-analyzer — table test asserts AnalyzersFor("kotlin")⊇{detekt,scc,
                 jscpd}, ("dart")⊇{dcm,...}, ("svelte")⊇{eslint,...}, ("sql")⊇
                 {sqlfluff,...}; each dedicated analyzer's Dimension passes
                 IsSubstantive (the existing TestCatalogExcludesCosmetic over Catalog()
                 already guards the whole table, and now includes these four). If
                 dcm were dropped from dart.toml, AnalyzersFor("dart") omits dcm and
                 the table row fails.
       depends_on: t-1, t-2, t-3, t-4, t-5

  t-8  Test ScannersFor returns agnostic-only for each
       files:    internal/security/catalog_test.go
       covers:   c-3
       contract: K-scanner-agnostic — for each of {kotlin,dart,svelte,sql},
                 ScannersFor(id) every entry .Agnostic()==true and the name set ==
                 {gitleaks,semgrep,trivy} exactly (no dedicated scanner). If any
                 profile accidentally declared a kind="scanner" tool, the set would
                 grow and this test fails (enforces security_agnostic_only).
       depends_on: t-2, t-3, t-4, t-5

  t-9  Test runtime seeding present for 3, absent for sql
       files:    internal/stack/runtime_test.go
       covers:   c-4
       contract: K-runtime — ResolveRuntime(ByID(Embedded(),"kotlin"),...) .Test and
                 .Build non-empty (same for dart, svelte); ResolveRuntime for "sql"
                 has .Test=="" and .Build=="". Proves init/onboard/apply seed runtime
                 from kotlin/dart/svelte and seed nothing for sql. If sql.toml grew a
                 [runtime.test], the sql assertion fails.
       depends_on: t-2, t-3, t-4, t-5

## Coverage

| criterion | covered by |
|-----------|------------|
| c-1 (detection recognises 4 langs; extLang gains 3; fixtures resolve; no false-match) | t-1, t-6 |
| c-2 (4 profiles ship embedded; appear in list; load via show) | t-2, t-3, t-4, t-5 (embedded via go:embed; t-6 loads them via Embedded(), which is exactly what `stack list`/`stack show` call through LoadAll → proves CLI surface without a redundant cmd test) |
| c-3 (AnalyzersFor surfaces dedicated analyzer w/ valid dimension; ScannersFor agnostic-only) | t-2..t-5 (data), t-7 (analyzer), t-8 (scanner) |
| c-4 (kotlin/dart/svelte declare runtime; sql declares none) | t-2, t-3, t-4, t-5 (data), t-9 (proof) |
| c-5 (each profile proven by detect-resolves AND AnalyzersFor-surfaces, with no mechanism change beyond extLang) | t-1 (the sole edit), t-6 + t-7 (the dual proof, run over Embedded()) |

Every criterion c-1..c-5 has at least one task whose named test fails if the
criterion regresses.

## Judgment calls

- Chose to give c-2 (`stack list`/`stack show`) NO dedicated cmd-layer test; rejected
  adding a cmd/stack_test.go case. Reason: `stackList`/`stackShow` call `stack.LoadAll()`
  → `Embedded()`, the exact path t-6 exercises. A profile that fails to embed/decode
  makes `Embedded()` error and breaks t-6 first; a cmd test would only re-prove the
  same wiring. Verification lens spends a test only where it can fail independently.
- Chose ONE consolidated detection test (t-6) and ONE consolidated AnalyzersFor test
  (t-7) as table tests, not four tests each. Rejected per-profile test files. Reason:
  the contract is identical across the four; a table row is the smallest unit that
  makes each profile's contract independently fail-visible without four near-duplicate
  funcs.
- Chose to make t-1 (extLang) its own wave-1 task, separate from the profiles.
  Rejected folding it into a profile task. Reason: c-5's keystone is "no mechanism
  change beyond the named extLang additions" — isolating the single permitted code
  edit makes it auditable and gives K-detect/lang a clean home; if any later task
  needs a *second* code edit, the isolation makes that violation obvious.
- Chose to assert ScannersFor name-set EQUALITY (== {gitleaks,semgrep,trivy}), not
  mere membership. Rejected a looser "contains agnostic" check. Reason:
  security_agnostic_only is locked; only equality catches an accidentally-added
  dedicated scanner. The failing surface must be the over-inclusion, not an omission.
- Chose detekt/dcm/eslint dimension=complexity and sqlfluff=dead-code exactly as the
  locked tool_loadout/sqlfluff_dimension decisions state; no alternative considered
  (locked). Noted only to show the dimension values in t-2..t-5 are not my invention.
- Chose svelte's dedicated analyzer Name="eslint" with Bin="eslint" (install string
  pulls eslint-plugin-svelte). Rejected Name="eslint-plugin-svelte" because Bin must
  resolve on PATH (Validate + Detect lookPath) and the plugin has no own binary; the
  runnable binary is eslint. The plugin is conveyed via the Install hint.
