# Risk-lens decomposition — multilang-analyzer-catalogs

Lens: failure modes drive the graph. Each enumerated way this phase can ship a
silently-broken catalog is owned and tested by exactly one task. The sharp edges:
(a) the *existing* `TestScannersForLanguageProfilesAgnosticOnly` HARD-CODES that
svelte/ts/dart carry no scanners — this phase deliberately breaks it, so the red
is a regression that must be re-owned, not a surprise; (b) `BuildManifest` dedups
tools by `Name`, so dart's two dcm entries (complexity + dead-code) collapse to one
unless distinctly named (the "trivy config" precedent); (c) three analyzers can
still be <3 *distinct* dimensions; (d) an `optional` scanner must still surface as
missing-with-hint, not be silently omitted; (e) the c-3 proof is unreproducible
unless the fixture actually trips a dedicated-tool-only finding.

```
Phase multilang-analyzer-catalogs — 8 tasks across 2 waves

Wave 1
  t-1  Add JS/TS loadout to svelte+typescript
       files:    internal/stack/profiles/svelte.toml, internal/stack/profiles/typescript.toml
       covers:   c-1, c-2
       contract: if osv-scanner's kind is mistyped (not "scanner") it stops appearing
                 in ScannersFor; if knip/dependency-cruiser/typescript-eslint share a
                 dimension with eslint, the distinct-dimension count test (t-5) drops
                 below 3 — both profiles must carry 4 analyzers across {complexity,
                 dead-code, coupling, error-handling}, duplicated per jsts_tool_sharing.
  t-2  Add Dart loadout to dart profile
       files:    internal/stack/profiles/dart.toml
       covers:   c-1, c-2, c-4
       contract: the two dcm analyzers must be named distinctly ("dcm" complexity,
                 "dcm unused-code" dead-code) — name them identically and t-8's
                 dedup-collapse assertion fails because dead-code vanishes from the
                 manifest; osv-scanner must be kind="scanner" optional=true so t-7
                 sees it surface as missing-with-hint.
  t-3  Commit findings-proof fixture
       files:    examples/findings-proof/typescript/flawed.ts,
                 examples/findings-proof/typescript/package.json,
                 examples/findings-proof/typescript/README.md
       covers:   c-3
       contract: the fixture must contain an unused export + a circular import that
                 knip/dependency-cruiser flag but scc/jscpd do NOT — if the defect is
                 a mere clone/LOC spike, the agnostic fallback would also catch it and
                 the c-3 "dedicated-only finding" claim is unprovable.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Re-own the agnostic-scanner regression guard
       files:    internal/security/catalog_test.go
       covers:   c-1
       contract: split TestScannersForLanguageProfilesAgnosticOnly — kotlin/sql must
                 STILL reject any dedicated scanner (equality with agnostic set), while
                 a new svelte/typescript/dart row asserts osv-scanner is present and
                 non-agnostic. Drop osv-scanner from a profile and the matching row goes
                 red; smuggle a scanner into kotlin and the agnostic-only row goes red.
       depends:  t-1, t-2
  t-5  Assert >=3 distinct dimensions per JS/TS/Dart
       files:    internal/quality/catalog_test.go
       covers:   c-2
       contract: count DISTINCT substantive dimensions in AnalyzersFor(lang) for each
                 of svelte/typescript/dart; fewer than 3 fails. Two analyzers sharing
                 a dimension (e.g. both tagged complexity) trips it even though the
                 tool count is 3.
       depends:  t-1, t-2
  t-6  Assert new scanners surface through recon
       files:    internal/security/recon_test.go
       covers:   c-1
       contract: BuildManifest over a temp tree with a tsconfig.json/.ts (and a
                 pubspec.yaml/.dart tree) must list osv-scanner in m.Tools by name —
                 proving `dross security detect` reaches the profile scanner, not just
                 ScannersFor in isolation. Break the ext->profile surfacing and the
                 manifest omits osv-scanner.
       depends:  t-1, t-2
  t-7  Assert optional/missing tool reported, run continues
       files:    internal/security/recon_test.go
       covers:   c-4
       contract: under an all-missing lookPath on a dart tree, BuildManifest returns
                 nil error AND m.Skipped() contains dart's optional osv-scanner with a
                 non-empty Install hint — proving optional=true does NOT silently omit
                 the tool and a missing tool never aborts the run.
       depends:  t-2
  t-8  Assert no name-dedup collapse + c-3 plumbing
       files:    internal/quality/catalog_test.go, internal/security/catalog_test.go
       covers:   c-2, c-3
       contract: AnalyzersFor("dart") must contain BOTH "dcm" (complexity) and
                 "dcm unused-code" (dead-code) as separate entries with distinct
                 dimensions; collapse the names and the dead-code dimension disappears.
                 Plus: ScannersFor("typescript") includes osv-scanner — the static
                 plumbing assertion that underwrites the committed-fixture proof
                 (no JS/dart toolchain in go test, per findings_proof).
       depends:  t-1, t-2
```

## Coverage
- c-1 (dedicated scanner declared + surfaced by `dross security detect`): t-1, t-2 (declare), t-4 (catalog regression guard), t-6 (recon/manifest surfacing)
- c-2 (>=3 substantive quality dimensions): t-1, t-2 (declare), t-5 (distinct-dimension count), t-8 (dcm name-collision guard so dead-code is not lost)
- c-3 (dedicated-only finding, committed fixture + verify): t-3 (fixture), t-8 (ScannersFor/AnalyzersFor plumbing assertion that the fixture's tools are wired); manual-run record is captured in verify per findings_proof, not a go-test gate
- c-4 (missing dedicated tool reported unavailable + continues): t-2 (optional osv-scanner declared), t-7 (Skipped-with-hint + no-abort assertion)

## Judgment calls
- One task for svelte+typescript together (t-1), not two: jsts_tool_sharing locks duplicated entries, so splitting them invites the two files drifting out of sync — co-editing keeps the duplication a single reviewable diff.
- Dart split into its own profile task (t-2): its collision risk (two dcm names) and optional-scanner semantics are distinct failure modes from the JS/TS loadout; folding it into t-1 would let one task own three unrelated risks.
- t-4 modifies the existing agnostic-only test rather than adding a parallel one: leaving the old equality assertion in place would force every svelte/ts/dart run red — the regression must be re-owned at its source, not shadowed.
- Chose c-3's fixture defect as unused-export + circular-import (knip/dependency-cruiser territory), rejecting a duplication/complexity defect: scc/jscpd already cover those, so only a dead-code/coupling defect proves a *dedicated-only* finding the agnostic fallback misses.
- t-8 pairs the dcm dedup guard with the ScannersFor plumbing assertion (same "names must survive into the catalog" risk family) rather than scattering them; rejected wiring JS/dart toolchains into go test per findings_proof — the static assertion is the reproducible-by-CI half, the fixture run is the manual half recorded in verify.
- t-6 and t-7 stay separate despite sharing recon_test.go: surfacing-a-tool and handling-a-missing-tool are different failure modes, and the risk lens forbids one task owning both.
