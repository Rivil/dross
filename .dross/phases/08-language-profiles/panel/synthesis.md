# Phase 08-language-profiles — SYNTHESIS

Judge's note: I authored none of the three drafts. The merged plan below grafts the
verification lens's per-profile test granularity onto the risk lens's per-profile data
tasks, taking the verification draft as the skeleton. The MVP draft (3 tasks) is the
lower bound and loses to the explicit phase context: "Mutation testing in verify will
reward dedicated test files; over-merging into one giant task hurts atomic-commit
hygiene." I default toward the finer split, but record every divergence rather than
silently resolving it.

## Scores

| Draft        | Criteria coverage                          | Test-contract specificity                                              | Granularity                                                  | Wave correctness                                  |
| ------------ | ------------------------------------------ | --------------------------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------- |
| risk         | All c-1..c-5 covered; c-2 left implicit.    | Strong — names exact dimension, Install-string svelte assert, false-match guard owned. | Good (7): one data task/profile, extLang split, but only 2 test tasks fold c-3/c-4 into data. | Correct — profiles wave-1, detection tests wave-2; clean rationale. |
| mvp          | All c-1..c-5 covered; c-2 implicit, c-4 has no proof test. | Adequate but coarse — one t-1 contract carries all 4 profiles + scanners + runtime in one paragraph. | Too coarse (3): one bundled TOML task + one bundled test task; collides with atomic-commit/mutation guidance. | Correct waves, but bundling defeats per-commit hygiene. |
| verification | All c-1..c-5 covered with a named failing test per criterion; c-4 gets its own proof (t-9). | Strongest — test contracts derived first; ScannersFor set-EQUALITY, runtime empty/non-empty, real Embedded(). | Best (9): per-profile data tasks + four proof tests split by package (detect/quality-catalog/security-catalog/runtime). | Correct and explicit depends_on per task; security/runtime tests correctly don't depend on t-1. |

Skeleton: **verification** (9 tasks). It has a named, independently-failing test for
every criterion including c-4 (which mvp leaves unproven), splits proof tests along the
real package boundary (`internal/stack`, `internal/quality`, `internal/security`) — each
a clean atomic commit and a distinct mutation surface — and its depends_on edges are
correct (scanner/runtime tests need profiles, not extLang). Confirmed against source:
`detect_test.go`, `runtime_test.go`, and both `catalog_test.go` files exist; `Embedded()`,
`ByID`, `ResolveRuntime(p, goos, lookPath)`, and `Unsupported` are all real.

## Merged plan

Phase 08-language-profiles — 9 tasks across 2 waves

Wave 1

  t-1  Add .dart/.svelte/.sql to extLang  [verification+mvp+risk]
       files:    internal/stack/detect.go
       covers:   c-1, c-5
       contract: add ".dart"->"dart", ".svelte"->"svelte", ".sql"->"sql" to extLang
                 (.kt->kotlin already present, left untouched as a regression check).
                 K-detect/lang: DetectLanguages on a temp tree with one .dart, one
                 .svelte, one .sql file includes "dart","svelte","sql"; deleting any
                 one extLang line drops that language and the test fails. This is the
                 ONLY mechanism edit the phase permits (the c-5 keystone).

  t-2  Add embedded kotlin profile  [verification+risk]
       files:    internal/stack/profiles/kotlin.toml
       covers:   c-2, c-3, c-4
       contract: id="kotlin"; signals.exts=[".kt",".kts"]; one analyzer detekt
                 kind=analyzer dimension=complexity core=true; runtime test/typecheck/
                 format/build via gradle (./gradlew test, compileKotlin, ktlintCheck,
                 build); no kind="scanner" tool. If id != "kotlin", AnalyzersFor
                 ("kotlin") omits detekt -> t-7 fails. Verified by t-6/t-7/t-8/t-9.

  t-3  Add embedded dart profile  [verification+risk]
       files:    internal/stack/profiles/dart.toml
       covers:   c-2, c-3, c-4
       contract: id="dart"; signals.files=["pubspec.yaml"]; signals.exts=[".dart"];
                 one analyzer dcm kind=analyzer dimension=complexity core=true;
                 runtime dart test/analyze/format/compile; no kind="scanner" tool.
                 If dimension were cosmetic, TestCatalogExcludesCosmetic fails on dcm.
                 Verified by t-6/t-7/t-8/t-9.

  t-4  Add embedded svelte profile  [verification+risk]
       files:    internal/stack/profiles/svelte.toml
       covers:   c-2, c-3, c-4
       contract: id="svelte"; signals.files=["svelte.config.js"]; signals.exts=
                 [".svelte"]; one analyzer Name="eslint" Bin="eslint", Install string
                 contains "eslint-plugin-svelte" (the plugin has no own binary; the
                 runnable bin is eslint), kind=analyzer dimension=complexity core=true;
                 runtime vitest/svelte-check/prettier/vite build; no kind="scanner"
                 tool. Verified by t-6/t-7/t-8/t-9; t-7 also asserts Install contains
                 "eslint-plugin-svelte". (Install-string assert grafted from risk.)

  t-5  Add embedded sql profile  [verification+risk]
       files:    internal/stack/profiles/sql.toml
       covers:   c-2, c-3, c-4
       contract: id="sql"; signals.exts=[".sql"]; one analyzer sqlfluff kind=analyzer
                 dimension=dead-code (NOT a new dimension) core=true; NO [runtime]
                 block; no kind="scanner" tool. If a fresh dimension token (e.g.
                 "correctness") is used, t-7's TestCatalogExcludesCosmetic /
                 IsSubstantive trips. Verified by t-7, t-8, and t-9 (sql Test+Build
                 both empty). (c-4 added to covers per risk — sql's no-runtime is part
                 of c-4 and is proven by t-9.)

Wave 2 (depends t-1..t-5)

  t-6  Test detection resolves each new profile id + false-match guard  [verification+risk]
       files:    internal/stack/detect_test.go
       covers:   c-1, c-5
       depends:  t-1, t-2, t-3, t-4, t-5
       contract: K-detect-resolve — table test over {kotlin:.kt, dart:pubspec.yaml,
                 svelte:svelte.config.js, sql:.sql}; for each, a fixture in t.TempDir
                 with stack.Embedded() => Detect(dir, Embedded()) == that id. Control
                 fixtures: (a) only foo.rb / README.txt (unsupported) returns
                 stack.Unsupported, NOT one of the four (risk's dedicated false-match
                 guard, kept in this task); (b) polyglot guard — a .sql beside go.mod
                 still detects "go" (file signal beats ext), so .sql can't hijack a Go
                 repo (grafted from verification). Uses Embedded() so it proves the
                 SHIPPED TOMLs; deleting kotlin.toml fails the kotlin row.

  t-7  Test AnalyzersFor surfaces each dedicated analyzer  [verification+mvp]
       files:    internal/quality/catalog_test.go
       covers:   c-3, c-5
       depends:  t-1, t-2, t-3, t-4, t-5
       contract: K-analyzer — table test: AnalyzersFor("kotlin")⊇{detekt,scc,jscpd},
                 ("dart")⊇{dcm,...}, ("svelte")⊇{eslint,...} AND eslint Install
                 contains "eslint-plugin-svelte", ("sql")⊇{sqlfluff,...}; each
                 dedicated analyzer's Dimension passes IsSubstantive (the existing
                 TestCatalogExcludesCosmetic over Catalog() now also covers these
                 four). Dropping dcm from dart.toml fails the dart row. Pairs with t-6
                 as the dual c-5 keystone proof.

  t-8  Test ScannersFor returns agnostic-only for each  [verification]
       files:    internal/security/catalog_test.go
       covers:   c-3
       depends:  t-2, t-3, t-4, t-5
       contract: K-scanner-agnostic — for each of {kotlin,dart,svelte,sql},
                 ScannersFor(id) name-set EQUALS {gitleaks,semgrep,trivy} exactly
                 (equality, not membership — only equality catches an accidentally
                 added kind="scanner" tool) and every entry .Agnostic()==true.
                 Enforces the locked security_agnostic_only decision. Note: depends on
                 the four profiles but NOT on t-1 (scanner resolution keys off id).

  t-9  Test runtime seeding present for 3, absent for sql  [verification+risk]
       files:    internal/stack/runtime_test.go
       covers:   c-4
       depends:  t-2, t-3, t-4, t-5
       contract: K-runtime — ResolveRuntime(ByID(Embedded(),"kotlin"), goos, lookPath)
                 .Test and .Build non-empty (same for dart, svelte); ResolveRuntime
                 for "sql" has .Test=="" and .Build=="". Proves init/onboard/apply seed
                 runtime from kotlin/dart/svelte and seed nothing for sql. A sneaked-in
                 [runtime.test] in sql.toml fails the sql assertion. Does NOT depend on
                 t-1 (runtime resolution keys off id, not extLang).

Coverage:
  c-1: t-1, t-6
  c-2: t-2, t-3, t-4, t-5 (embedded via go:embed; t-6 loads them via Embedded(), the
       exact path stack list/show call through LoadAll — proves the CLI surface, no
       redundant cmd test)
  c-3: t-2..t-5 (data), t-7 (analyzer), t-8 (scanner)
  c-4: t-2, t-3, t-4, t-5 (data), t-9 (proof)
  c-5: t-1 (sole edit), t-6 + t-7 (dual proof over Embedded())

## Disagreements

### D-1 — Profile data: one bundled task vs one task per profile
- mvp: ONE t-1 task authoring all four TOMLs — "same data shape, four <10-min files,
  merge-it territory; per-stack splitting is speculative structure."
- risk: FOUR tasks (one per profile) — "each profile is an independent failure surface
  (id≠token, wrong dimension, leaked scanner, missing/extra runtime); a bundled task
  lets one profile's bug hide behind another's green."
- verification: FOUR tasks, agreeing with risk for test-isolation reasons.
- Provisional default: **four tasks (t-2..t-5)**. Two of three lenses agree, and the
  phase context is explicit that mutation testing rewards dedicated files and
  over-merging hurts atomic-commit hygiene — which directly favors the split.
- Why it matters: this is the single biggest structural fork (3 vs 9 tasks downstream).
  If the executor values raw speed over per-commit bisectability, mvp's bundle is
  defensible; the default chosen optimizes for clean blame/bisect and per-profile
  mutation surfaces, at the cost of four small commits instead of one.

### D-2 — Proof tests: one consolidated task vs split by package
- mvp: ONE t-3 spanning detect_test.go + catalog_test.go — "keep the keystone proof as
  one deliverable; the per-stack table is shared."
- risk: TWO test tasks — t-6 (detection + false-match) and t-7 (keystone table), both
  in detect_test.go; folds c-3 analyzer/c-4 runtime proofs INTO the data tasks, with no
  standalone scanner or runtime test task.
- verification: FOUR test tasks split by package — detect (t-6), quality catalog (t-7),
  security catalog (t-8), runtime (t-9).
- Provisional default: **four test tasks split by package (verification)**. The three
  test concerns live in three different Go packages (internal/stack, internal/quality,
  internal/security) — one task cannot cleanly own files across all three as a single
  atomic commit, and each is a distinct mutation surface. Confirmed both catalog_test.go
  files and runtime_test.go already exist.
- Why it matters: mvp under-tests c-4 (no dedicated runtime proof) and c-3's scanner
  side; risk buries scanner/runtime assertions inside data tasks, so a scanner-leak
  regression has no independently-named failing test. The split costs more tasks but
  gives every locked decision (sqlfluff dimension, agnostic-only scanners, sql no-runtime)
  its own fail-visible guard.

### D-3 — Does runtime get its own test task (c-4)?
- verification: YES — t-9 (runtime_test.go) explicitly asserts non-empty for 3, empty
  for sql.
- risk: NO standalone runtime test — c-4 is "covered" by the per-profile data tasks
  (t-2..t-5) via their contract text ("Runtime.test resolves to non-empty").
- mvp: NO runtime proof at all — c-4 maps only to t-1 (the data task); no test asserts
  the sql-has-no-runtime invariant.
- Provisional default: **YES, t-9 exists** (verification). c-4 has a concrete
  observable invariant (sql seeds nothing; the other three seed test+build) that no
  other test asserts; without t-9 the "sql declares no runtime" half of c-4 is unproven.
- Why it matters: this is the clearest coverage gap between the drafts — mvp would ship
  c-4 with zero verifying test. Keeping t-9 closes it.

### D-4 — Does ScannersFor get its own test task (c-3 scanner half)?
- verification: YES — t-8 (security/catalog_test.go), name-set EQUALITY against
  {gitleaks,semgrep,trivy}.
- risk: NO standalone task — the "no leaked scanner" assertion rides inside each
  profile data task's contract.
- mvp: folded into t-1's contract paragraph ("ScannersFor returns len>3 if a stray
  scanner present"), no dedicated task.
- Provisional default: **YES, t-8 exists** (verification), with set-EQUALITY not
  membership — only equality catches an accidentally-added dedicated scanner.
- Why it matters: security_agnostic_only is a locked decision; a leaked kind="scanner"
  tool is exactly the regression to guard, and it belongs in the security package's
  test, not folded into a stack-package data task it can't share a commit with.

### D-5 — False-match guard: own task vs folded into the keystone table
- risk: SEPARATE — wants the unsupported-repo control assertion in its own task (t-6)
  distinct from the keystone table (t-7), "so a passing-by-luck fixture can't mask it."
- verification/mvp: FOLDED — the false-match control is one row inside the single
  consolidated detection test.
- Provisional default: **FOLDED into t-6** (verification), but t-6 carries BOTH the
  unsupported control (risk's foo.rb guard) AND verification's polyglot go.mod guard as
  explicit, separately-named sub-assertions.
- Why it matters: risk's concern (false-match hiding) is real, but splitting detection
  into two tasks within the SAME file (detect_test.go) would fragment one atomic commit
  for little gain. Keeping both guards as named assertions in t-6 preserves the
  fail-visibility risk wanted without the extra task. If the executor finds the two
  guards genuinely independent, promoting the false-match guard to its own task is the
  sanctioned fallback (it exists in the risk draft).

### D-6 — Where do the extLang edit's tests live?
- mvp/risk: the extLang lines (t-1) are validated downstream — the DetectLanguages
  assertion lives in the detection test (mvp t-3 / risk t-6), not in t-1.
- verification: t-1 itself carries the K-detect/lang DetectLanguages contract (the
  extLang edit and its direct unit test are the same task).
- Provisional default: **t-1 owns its DetectLanguages test** (verification), AND t-6
  separately exercises full Detect() resolution over Embedded(). Two distinct surfaces:
  t-1 proves the map entries (DetectLanguages), t-6 proves end-to-end profile selection.
- Why it matters: keeping the extLang unit test inside t-1 makes the single permitted
  mechanism edit self-contained and auditable (the c-5 keystone is "no mechanism change
  beyond extLang" — isolating the edit + its test makes any second code edit obvious).
  Risk/mvp's deferral works too but leaves t-1 as a code edit with no in-task test.
