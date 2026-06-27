# Synthesis — multilang-analyzer-catalogs

Cold judge over three independent decompositions (risk / mvp / verification). I
authored none. Facts below were checked against the real source, not the drafts:

- `internal/security/catalog_test.go:81` `TestScannersForLanguageProfilesAgnosticOnly`
  iterates `{kotlin, dart, svelte, sql, typescript}` and asserts each profile's
  scanner set **equals** the agnostic set `{gitleaks, semgrep, trivy}` (membership
  *and* a "no unexpected dedicated scanner" guard). It goes **red the instant a
  `kind="scanner"` lands in svelte/typescript/dart.toml** → hard green-blocker.
- `internal/quality/catalog_test.go:109` `TestAnalyzersForLanguageProfiles` asserts
  the named dedicated analyzer (svelte/ts→`eslint`, dart→`dcm`) is present,
  non-agnostic, substantive. Adding *more* analyzers does **not** break it — eslint
  and dcm stay present. So it is **not** a green-blocker; it merely under-asserts c-2.
- `internal/security/recon.go:56` / `internal/quality/recon.go:56` `BuildManifest`
  dedups tools by `Name` (`seen[s.Name]`) → dart's two `dcm` entries collapse unless
  distinctly named. Risk's collapse risk is real.
- Pre-existing `TestAvailabilityMissingHasHint` / `TestDetectMissingHasHint` assert
  every catalog tool has a non-empty `Install` over `Catalog()` → a hint-less new
  tool reddens them (mvp's c-4 angle is a real gate).
- Both `recon_test.go` files exist. No `testdata/`, `fixtures/`, or `examples/`
  directory exists anywhere in the repo (no fixture-location convention to inherit).

## Scores

Scale 1–5 (5 best).

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| risk         | 4 — all four criteria, but c-4 tested only on the security side; quality-side manifest skip + "3 dimensions reach a run" untested | 4 — failure-mode-sharp (dedup-collapse, optional-skip), but doesn't pin exact Go test func names | 3 — 8 tasks, but t-8 spans two files that other wave-2 tasks also edit | 2 — wave-2 collisions: t-4 & t-8 both edit security/catalog_test.go; t-5 & t-8 both edit quality/catalog_test.go |
| mvp          | 2 — c-1's "detect lists it for a repo of that language" clause has no manifest test; c-4 leans on pre-existing generic has-hint tests that never prove the *new* tools surface/skip per-language; no c-3 go-test half | 3 — accurate refs to real existing tests, but one coarse test task | 2 — 4 tasks; folds both packages + two criteria into one task and drops the manifest layer entirely | 4 — clean: one wave-2 task, no file collisions |
| verification | 5 — full: c-1 (catalog + manifest), c-2 (catalog + manifest), c-3 (fixture + go-test plumbing), c-4 on both security and quality sides | 5 — writes exact Go test func names, exact break conditions, distinct-Name rationale, unambiguous .svelte/.dart ext choice | 5 — 8 tasks, one concern each | 5 — wave-2 tasks deliberately file-disjoint (incl. a new c3_plumbing_test.go) for conflict-free parallel commits |

**Skeleton: verification.** It has complete criteria coverage, the most directly
implementable contracts, and the only wave-2 layout with zero file collisions. The
runners-up graft in three real improvements the skeleton lacked: mvp's install-hint
c-4 gate, risk's dcm dedup-collapse contract, and a fact-correction on the quality
test (see Disagreements).

## Merged plan

8 tasks across 2 waves. Each task tagged `[origin]`. Format: id · title — files /
covers / test_contract / depends_on.

### Wave 1 — profile declarations (parallel, independent)

**t-1 [verification + mvp + risk]** Add svelte+typescript scanner & analyzer loadout
- files: `internal/stack/profiles/svelte.toml`, `internal/stack/profiles/typescript.toml`
- covers: c-1, c-2, c-4 (declaration side)
- test_contract: Per `js_ts_loadout`, add to **both** files (duplicated verbatim per
  `jsts_tool_sharing` — no inherit): scanners `osv-scanner`(core),
  `eslint-plugin-security`, `retire.js`(optional); analyzers `knip`(dead-code),
  `dependency-cruiser`(coupling), `typescript-eslint`(error-handling) on top of the
  existing `eslint`(complexity). Each analyzer keeps a **distinct `Name`** (eslint vs
  typescript-eslint) so manifest dedup-by-`Name` can't collapse them [verification].
  **Every new tool carries a non-empty `install` hint** or the pre-existing
  `security.TestAvailabilityMissingHasHint` / `quality.TestDetectMissingHasHint` over
  `Catalog()` go red [mvp graft — confirmed real gate]. Preserve the existing
  per-profile eslint install strings (`eslint-plugin-svelte` vs `typescript-eslint`)
  so `TestSvelteAndTypescriptEslintInstall` still passes.
- depends_on: —

**t-2 [verification + risk + mvp]** Add dart scanner & analyzer loadout
- files: `internal/stack/profiles/dart.toml`
- covers: c-1, c-2, c-4 (declaration side)
- test_contract: Per `dart_loadout`, add scanner `osv-scanner`(optional, on
  pubspec.lock) and analyzers `dcm unused-code`(dead-code) + `dart analyze`
  (error-handling), **distinctly named** from the existing `dcm`(complexity).
  **dedup guard [risk graft]:** because `BuildManifest` dedups by `Name`
  (`recon.go:56`), the two dcm entries MUST carry different `Name`s — name them
  identically and the dead-code dimension silently vanishes from the manifest. Each
  new tool carries an `install` hint [mvp graft]. osv-scanner is `optional=true` so
  t-5 can prove it surfaces as missing-with-hint without aborting.
- depends_on: —

### Wave 2 — tests + fixture (file-disjoint, parallel)

**t-3 [verification + mvp + risk]** Rewrite security catalog test (hard green-blocker)
- files: `internal/security/catalog_test.go`
- covers: c-1
- test_contract: Rewrite `TestScannersForLanguageProfilesAgnosticOnly` to iterate
  only `{kotlin, sql}` (those must STILL equal the agnostic set — smuggling a scanner
  into kotlin reddens it [risk]). Add `TestScannersFor_dedicatedScanner` with subtests
  svelte/typescript/dart: each `ScannersFor(lang)` contains a non-`Agnostic()`
  `osv-scanner` AND still contains gitleaks/semgrep/trivy; the subtest fails if
  osv-scanner is dropped from that profile or loses its `Languages` tag.
- depends_on: t-1, t-2

**t-4 [verification + mvp]** Extend quality catalog test to gate ≥3 dimensions
- files: `internal/quality/catalog_test.go`
- covers: c-2
- test_contract: Extend `TestAnalyzersForLanguageProfiles` (or add a `_threeDimensions`
  sibling): for each of svelte/typescript/dart, collect the **distinct `IsSubstantive`
  Dimensions across the dedicated (non-`Agnostic()`) analyzers** in `AnalyzersFor(lang)`
  and assert `len >= 3`; two analyzers sharing a dimension trips it even at tool-count
  3. dart subtest fails if `dart analyze` is dropped (2 left). Also assert no analyzer
  carries a non-substantive dimension (quality_scope guard). NOTE: not a compile/green
  blocker — see Disagreement 2; it is the only real c-2 gate.
- depends_on: t-1, t-2

**t-5 [verification + risk t-6/t-7]** Security recon manifest: surfaces + skips
- files: `internal/security/recon_test.go`
- covers: c-1, c-4
- test_contract: `TestBuildManifest_svelteRepo_listsOsvScanner` — `BuildManifest` on a
  tree with one `widget.svelte` file includes osv-scanner in `m.Tools` (the c-1
  "detect lists it for a repo of that language" clause; `.svelte` maps unambiguously,
  avoiding bare `.ts` which resolves to both svelte and typescript). Same for a
  `foo.dart` tree. `TestBuildManifest_missingDedicatedScannerSkipped` — under an
  all-missing lookPath, svelte's osv-scanner is in `m.Skipped()` with a non-empty
  Install hint AND `BuildManifest` returns a **nil error** (c-4: reported unavailable,
  run continues, no abort).
- depends_on: t-1, t-2

**t-6 [verification]** Quality recon manifest: surfaces 3 dimensions + skip
- files: `internal/quality/recon_test.go`
- covers: c-2, c-4
- test_contract: `TestBuildManifest_svelteRepo_surfacesThreeDimensions` — quality
  `BuildManifest` on a `widget.svelte` tree carries knip, dependency-cruiser, and
  typescript-eslint alongside scc/jscpd in `m.Tools` (proves all three dimensions
  reach a real run). `TestBuildManifest_missingDedicatedAnalyzerSkipped` — on a
  `foo.dart` tree under an all-missing lookup, dart's `dcm unused-code` is in
  `m.Skipped()` with a non-empty Install hint and `BuildManifest` returns nil. (Risk
  omitted the quality recon side entirely — grafted-in coverage.)
- depends_on: t-1, t-2

**t-7 [verification + mvp + risk]** Commit c-3 fixture + manual-run record (artifact half)
- files: `fixtures/multilang-c3/<lang>-deadcode/` (a small TypeScript project: package.json,
  tsconfig.json, src with a deliberately unused export), `fixtures/multilang-c3/expected-finding.txt`,
  `fixtures/multilang-c3/RUN.md`
- covers: c-3
- test_contract: Per `findings_proof` (committed fixture + documented manual run, **NOT
  a go-test gate** — no JS/dart toolchain in CI). The fixture plants an unused export
  that `knip`(dead-code) flags but the agnostic `scc/jscpd/semgrep` fallback does not.
  `expected-finding.txt` pins the exact symbol knip reports; `RUN.md` records the two
  commands (knip vs the agnostic fallback) and their outputs so verify can cite a
  reproducible run. Deleting the planted export breaks reproducibility of the recorded
  run.
- depends_on: t-1

**t-8 [verification]** c-3 plumbing go-test (plumbing half)
- files: `internal/quality/c3_plumbing_test.go` (new file — keeps wave-2 files disjoint)
- covers: c-3
- test_contract: `TestC3DedicatedDistinctFromAgnostic` — the c-3 demonstrator tool
  `knip` is present in `AnalyzersFor("typescript")` (shared loadout) as a non-`Agnostic()`
  analyzer AND is **not** one of the agnostic fallback bins `{scc, jscpd}`. This is the
  go-test half the locked decision permits: dedicated tool is wired into the merged
  catalog and is distinct from the agnostic set. Renaming/removing knip fails it.
- depends_on: t-1

**Coverage roll-up:** c-1 → t-1,t-2 (declare), t-3 (catalog lists), t-5 (detect lists
per repo). c-2 → t-1,t-2 (declare), t-4 (catalog gates ≥3), t-6 (3 dims reach a run).
c-3 → t-7 (fixture + manual run = the proof), t-8 (go-test plumbing). c-4 → t-1,t-2
(install hints), t-5 (security skip+nil), t-6 (quality skip+nil).

## Disagreements

**D1 — Granularity: 4 tasks (mvp) vs 8 (risk, verification).**
mvp folds every test edit into one task and drops the manifest layer; risk and
verification keep 8. *Default taken: 8 (verification skeleton).* Why it matters: at 4
tasks mvp has **no test for c-1's "and `dross security detect` lists that scanner for a
repo of that language"** clause (that needs a `BuildManifest` test, not a `ScannersFor`
unit test), and leans c-4 entirely on the pre-existing generic has-hint tests — which
run over `Catalog()` and never prove the *new* per-language tools surface and skip in a
real manifest. Those are two genuine coverage holes, not just structure. The 8-way
split also keeps wave-2 file-disjoint for parallel atomic commits.

**D2 — Does the quality catalog test have to be rewritten? (fact-level conflict).**
verification asserts BOTH `TestScannersForLanguageProfilesAgnosticOnly` *and*
`TestAnalyzersForLanguageProfiles` "assert the OPPOSITE of c-1/c-2 and must be
rewritten." Checked against source: only the **security** test is a hard green-blocker
(strict-equality guard → red the moment scanners land). The **quality** test keeps
passing after the loadout lands — `eslint`/`dcm` remain present, so adding analyzers
never trips it. mvp framed it correctly as *extend*, not rewrite. *Default taken:* t-3
**rewrites** the security test (mandatory for green); t-4 **extends** the quality test
(mandatory for c-2 *coverage*, not for the build to go green). Why it matters: if an
executor believes the quality test is a blocker, it may "fix" a test that isn't broken;
if it believes the security test is merely a coverage nicety, the phase won't compile
green. The two existing tests are in different states and must be owned differently.

**D3 — c-3 fixture location.**
risk: `examples/findings-proof/typescript/`. mvp: `internal/quality/testdata/c3-typescript/`.
verification: `fixtures/multilang-c3/` with `expected-finding.txt` + `RUN.md`. No such
directory exists in the repo today, so there is no convention to inherit. *Default
taken: `fixtures/multilang-c3/` (verification).* Why it matters: the locked decision
requires a *documented manual run* artifact, and only verification's layout ships the
run record (`RUN.md`) plus a pinned `expected-finding.txt` for verify to cite. mvp's
`testdata/` is automatically ignored by `go test` (a genuine plus) but couples a
JS/TS fixture to a Go package directory; the top-level `fixtures/` dir keeps it out of
every Go package's path while staying discoverable.

**D4 — c-3 fixture defect & language.**
risk: TypeScript with **two** defects — unused export (knip) + circular import
(dependency-cruiser). mvp: TypeScript, single knip unused-export. verification: a
svelte/ts dir, single knip dead-code. *Default taken: a TypeScript project with a
single planted unused export flagged by knip(dead-code).* Why it matters: the locked
`findings_proof` needs exactly **one** dedicated-only finding the agnostic set misses;
the single knip dead-code delta is the cleanest reproducible-by-hand proof. risk's
circular-import adds a second toolchain (dependency-cruiser config) to the manual run
for no extra credit; a plain TS project avoids a svelte runtime in the fixture.

**D5 — Recon coverage split.**
risk splits surfacing and missing-tool into two separate tasks (t-6, t-7) **both in
`internal/security/recon_test.go`** and never touches `internal/quality/recon_test.go`.
verification uses one task per package (t-5 security, t-6 quality), each covering both
surface and skip. *Default taken: verification's one-task-per-package.* Why it matters:
risk's split puts two tasks on the same file (serial, not parallel, and a merge hazard)
while leaving the **quality-side c-4 path and "3 dimensions reach a run" entirely
untested**. One task per package keeps wave-2 file-disjoint and closes the quality-side
gap. (Minor sub-point folded here: mvp places the fixture in wave 1 as independent;
the merged plan keeps t-7 in wave 2 `depends_on: t-1` so `RUN.md`/`expected-finding.txt`
cite the tool names exactly as declared.)
