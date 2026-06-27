# Verification-lens plan — multilang-analyzer-catalogs

Designed backward from the test contract for each criterion: the ideal `go test`
name is written first, then the smallest task that makes it satisfiable. Every
contract below is concrete enough to translate directly into a Go test function.

Phase multilang-analyzer-catalogs — 8 tasks across 2 waves

Wave 1
  t-1  Add svelte+typescript scanner & analyzer loadout
       files:    internal/stack/profiles/svelte.toml
                 internal/stack/profiles/typescript.toml
       covers:   c-1, c-2
       contract: Per js_ts_loadout — add to BOTH files: scanners osv-scanner(core),
                 eslint-plugin-security, retire.js(optional); analyzers knip(dead-code),
                 dependency-cruiser(coupling), typescript-eslint(error-handling) on top of
                 the existing eslint(complexity). Each new analyzer gets a DISTINCT Name
                 (eslint vs typescript-eslint) so manifest dedup-by-name can't collapse them.
                 Contract A: TestScannersFor_svelte_includesOsvScanner / _typescript_ fail if
                 the kind="scanner" osv-scanner entry is missing from either profile.
                 Contract B: counting distinct substantive Dimensions in AnalyzersFor("svelte")
                 must yield >=3 (complexity, dead-code, coupling, error-handling) — dropping
                 the dependency-cruiser(coupling) entry drops it to <3 and fails the test in t-4.

  t-2  Add dart scanner & analyzer loadout
       files:    internal/stack/profiles/dart.toml
       covers:   c-1, c-2
       contract: Per dart_loadout — add scanner osv-scanner(optional, on pubspec.lock) and
                 analyzers "dcm unused-code"(dead-code) + "dart analyze"(error-handling),
                 distinctly named from the existing dcm(complexity).
                 Contract A: TestScannersFor_dart_includesOsvScanner fails if dart.toml has no
                 kind="scanner" tool. Contract B: AnalyzersFor("dart") must expose 3 distinct
                 substantive dimensions; deleting the "dart analyze" entry drops it to 2 and
                 fails the dart row of t-4's three-dimension test.

Wave 2
  t-3  Test dedicated scanners in security catalog
       files:    internal/security/catalog_test.go
       covers:   c-1
       depends_on: t-1, t-2
       contract: The existing TestScannersForLanguageProfilesAgnosticOnly asserts the OPPOSITE
                 of c-1 for svelte/dart/typescript (no dedicated scanner) — it must be rewritten
                 to keep only {kotlin, sql} as agnostic-only. Add TestScannersFor_dedicatedScanner
                 with subtests svelte/typescript/dart: each ScannersFor(lang) must contain a
                 non-Agnostic() osv-scanner AND still contain gitleaks/semgrep/trivy. The svelte
                 subtest fails if osv-scanner is removed from svelte.toml or loses its Languages tag.

  t-4  Test three analyzer dimensions per language
       files:    internal/quality/catalog_test.go
       covers:   c-2
       depends_on: t-1, t-2
       contract: Extend TestAnalyzersForLanguageProfiles into TestAnalyzersForLanguageProfiles_
                 threeDimensions: for each of svelte/typescript/dart, collect the distinct
                 IsSubstantive Dimensions across the dedicated (non-Agnostic) analyzers in
                 AnalyzersFor(lang) and assert len >= 3. Removing knip from svelte.toml leaves
                 {complexity, coupling, error-handling}=3 (still passes) but removing TWO drops
                 below 3 and fails; the dart subtest fails if "dart analyze" is dropped (2 left).
                 Also assert no analyzer carries a non-substantive dimension (quality_scope guard).

  t-5  Test scanner manifest surfaces & skips
       files:    internal/security/recon_test.go
       covers:   c-1, c-4
       depends_on: t-1, t-2
       contract: TestBuildManifest_svelteRepo_listsOsvScanner — BuildManifest on a tree with one
                 widget.svelte file must include osv-scanner in m.Tools (the c-1 "detect lists it
                 for a repo of that language" clause; .svelte maps unambiguously to svelte).
                 TestBuildManifest_dartRepo_listsOsvScanner — same for a foo.dart tree.
                 TestBuildManifest_missingDedicatedScannerSkipped — under an all-missing lookup,
                 svelte's osv-scanner appears in m.Skipped() with a non-empty Install hint AND
                 BuildManifest returns a nil error (c-4: reported unavailable, run continues, no abort).

  t-6  Test analyzer manifest surfaces & skips
       files:    internal/quality/recon_test.go
       covers:   c-2, c-4
       depends_on: t-1, t-2
       contract: TestBuildManifest_svelteRepo_surfacesThreeDimensions — quality BuildManifest on a
                 widget.svelte tree must carry knip, dependency-cruiser, and typescript-eslint
                 alongside scc/jscpd in m.Tools (proves all three dimensions reach a real run).
                 TestBuildManifest_missingDedicatedAnalyzerSkipped — on a foo.dart tree under an
                 all-missing lookup, dart's "dcm unused-code" is in m.Skipped() with a non-empty
                 Install hint and BuildManifest returns nil (c-4 on the quality side).

  t-7  Commit c-3 fixture and run record
       files:    fixtures/multilang-c3/svelte-deadcode/  (small svelte/ts project)
                 fixtures/multilang-c3/expected-finding.txt
                 fixtures/multilang-c3/RUN.md
       covers:   c-3
       depends_on: t-1
       contract: Per findings_proof (committed fixture + documented manual run, NOT a go-test gate
                 — no npm toolchain in CI). The fixture contains a deliberate unused export that
                 knip(dead-code) flags but the agnostic fallback scc/jscpd/semgrep does NOT.
                 expected-finding.txt pins the exact symbol knip reports; RUN.md records the two
                 commands (knip vs the scc/jscpd/semgrep fallback) and their outputs, so verify can
                 cite a reproducible run. Contract: deleting the planted unused export, or the
                 fixture dir, breaks reproducibility of the recorded run; expected-finding.txt is the
                 checked-in proof artifact verify references.

  t-8  Test c-3 tool distinct from fallback
       files:    internal/quality/c3_plumbing_test.go  (new file, keeps wave-2 files disjoint)
       covers:   c-3
       depends_on: t-1
       contract: TestC3DedicatedDistinctFromAgnostic — the c-3 demonstrator tool (knip) must be
                 present in AnalyzersFor("svelte") as a non-Agnostic() analyzer AND must NOT be one
                 of the agnostic fallback bins {scc, jscpd}. This is the go-test half of c-3 the
                 locked decision permits (plumbing: dedicated tool is in the merged catalog and is
                 distinct from the agnostic set). Renaming/removing knip in svelte.toml fails it.

## Coverage
- c-1 → t-1, t-2 (declare scanners), t-3 (catalog lists them), t-5 (detect lists them for a repo of that language)
- c-2 → t-1, t-2 (declare 3-dimension analyzers), t-4 (catalog enforces >=3), t-6 (3 dimensions reach a run)
- c-3 → t-7 (committed fixture + recorded manual run = the proof), t-8 (go-test plumbing: dedicated tool in merged catalog, distinct from agnostic fallback)
- c-4 → t-5 (security: missing scanner Skipped w/ hint, nil error), t-6 (quality: missing analyzer Skipped w/ hint, nil error)

## Judgment calls
- chose distinct Name per analyzer (eslint / typescript-eslint, dcm / "dcm unused-code" / "dart analyze"); rejected reusing one "eslint" Name for two dimensions — manifest dedup-by-name would collapse them (the "trivy config" vs "trivy" precedent).
- chose to REWRITE TestScannersForLanguageProfilesAgnosticOnly (drop svelte/dart/typescript, keep kotlin/sql); rejected leaving it — it asserts the exact opposite of c-1 and would go red the moment the scanners land.
- chose knip(dead-code) as the c-3 demonstrator; rejected typescript-eslint typed error-handling — typed rules need a full tsconfig + typed-lint setup, far heavier to reproduce by hand in RUN.md.
- chose a committed fixture + RUN.md + expected-finding.txt under fixtures/multilang-c3/; rejected a go-test gate for c-3 (locked findings_proof: no JS/dart toolchain in CI).
- chose to put c-3 plumbing in a new file internal/quality/c3_plumbing_test.go; rejected appending to catalog_test.go — keeps all six wave-2 tasks file-disjoint so they can run without edit conflicts.
- chose to demonstrate c-4 at the manifest level (Skipped() + nil error) on BOTH security and quality sides; rejected a catalog-only assertion — the manifest Skipped path is the actual "report unavailable and continue" seam the criterion names.
- chose .svelte / .dart fixtures for the e2e manifest tests (unambiguous ext→lang); avoided a bare .ts file, which extLangFor maps to BOTH svelte and typescript and would blur which profile surfaced the tool.
