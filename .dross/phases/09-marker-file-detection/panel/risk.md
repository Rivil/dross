Phase 09-marker-file-detection — 7 tasks across 3 waves

Lens: RISK. The graph starts from what can break. The single highest-risk
surface is the glob matcher (over-broad patterns, case, `.yml`/`.yaml`,
root-vs-subtree scope); it is isolated into one owned task (t-1) so a bad match
has exactly one place to fail and one test to fail it. The second risk is the
additive union mutating the legacy paths (`Detect`/`DetectLanguages` are the
shared detection path security AND quality both depend on) — the union lives in
a NEW function (t-2) so the regression-prone legacy functions are never touched,
and the no-regression guard (t-6) owns proving that.

Wave 1
  t-1  Add file_patterns field and glob matcher
       files:    internal/stack/profile.go, internal/stack/detect.go,
                 internal/stack/detect_test.go
       covers:   c-4
       contract: Add `Signals.FilePatterns []string` (toml `file_patterns`) and a
                 pure `matchPattern(name, pattern string) bool` that lowercases
                 BOTH sides before filepath.Match. If the over-broad guard
                 regresses, a table test fails: `readme.md`/`compose.go`/
                 `mydockerfile.txt` must NOT match `dockerfile`, `*.dockerfile`,
                 `docker-compose*.yml`, while `Dockerfile`, `Dockerfile.dev`,
                 `app.Dockerfile`, `docker-compose-prod.yaml`, `compose.override.yml`
                 MUST. If `.yml`/`.yaml` brace-collapse sneaks in, the test asserting
                 `compose.yaml` does NOT match a `*.yml`-only pattern fails.

Wave 1
  t-3  Ship embedded docker stack profile
       files:    internal/stack/profiles/docker.toml
       covers:   c-1 (partial), c-7
       contract: New docker.toml with id=docker, `[signals].file_patterns`
                 covering the c-4 families and NO `exts`, plus tools hadolint
                 (kind=scanner), hadolint (kind=analyzer, dimension), trivy
                 (kind=scanner). If the loadout drifts, a test enumerating
                 docker.toml's tools fails when dockle/checkov appear or when
                 hadolint is missing either kind. Embedded()/Validate must accept
                 it: if file_patterns or the twin-hadolint entries break decode,
                 TestDetectResolvesEmbeddedProfiles fails.

Wave 2 (depends t-1, t-3)
  t-2  Add additive MarkerProfiles detector
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-5
       contract: New `MarkerProfiles(root string, profiles []*Profile) []string`
                 that walks the tree (reusing skipDirs), matches each basename
                 against every profile's FilePatterns via t-1's matcher, and
                 returns the sorted set of matched profile ids — touching NEITHER
                 Detect NOR DetectLanguages. If genericity regresses, a test
                 feeding a SYNTHETIC pattern-only profile (no exts) plus a matching
                 fixture file gets that profile's id back with zero Docker hardcode;
                 a Docker-specific branch in this function fails the synthetic-profile
                 test. If subtree scope breaks, a fixture with `sub/Dockerfile.dev`
                 (not at root) still returns `docker`.

Wave 3 (depends t-2)
  t-4  Union marker profiles into security manifest
       files:    internal/security/recon.go, internal/security/recon_test.go
       covers:   c-2
       contract: BuildManifest unions stack.MarkerProfiles(root) onto the detected
                 languages before resolving ScannersFor, deduped. If the additive
                 union breaks, a Dockerfile-only fixture (no .go) test fails to find
                 hadolint AND trivy-config in manifest.Tools; if dedup breaks, a
                 Go+Dockerfile fixture surfaces a duplicated scanner entry and the
                 dedup test fails.

Wave 3 (depends t-2)
  t-5  Union marker profiles into quality manifest
       files:    internal/quality/recon.go, internal/quality/recon_test.go
       covers:   c-3
       contract: BuildManifest unions stack.MarkerProfiles(root) onto languages
                 before AnalyzersFor, deduped. If the union breaks, a Dockerfile-only
                 fixture test fails to find hadolint (kind=analyzer) on top of the
                 agnostic scc/jscpd set; the agnostic set must still be present, so a
                 marker-only repo never loses scc/jscpd.

Wave 3 (depends t-2, t-4, t-5)
  t-6  Pin no-regression and over-broad guards end-to-end
       files:    internal/security/recon_test.go, internal/quality/recon_test.go,
                 internal/stack/detect_test.go
       covers:   c-6, c-1 (close)
       contract: Three regression locks. (a) A no-marker fixture (pure-Go, no
                 Dockerfile) produces a manifest with the EXACT pre-phase scanner/
                 analyzer set — `docker` never appears; if a stray match leaks,
                 this fails. (b) A pure-Go repo's manifest.Languages and tool names
                 are byte-identical to a golden recorded without the union path. (c)
                 stack.Detect() on a marker-only Docker repo (Dockerfile + no exts)
                 returns `docker` proving Files-weighted scoring still resolves c-1;
                 if a future edit accidentally feeds file_patterns into Detect's
                 winner-take-all scoring, an over-count assertion fails.

Wave 3 (depends t-3)
  t-7  Verify docker profile via stack CLI
       files:    internal/cmd/stack_test.go
       covers:   c-7
       contract: `dross stack list` output contains a `docker` line and
                 `dross stack show docker` round-trips the profile's file_patterns
                 and hadolint/trivy tools through the TOML encoder without error. If
                 docker.toml fails to embed or show fails to render file_patterns,
                 the list-contains-docker / show-loads-docker assertions fail.

## Coverage
- c-1 → t-3, t-6 (t-3 ships the profile; t-6c proves Detect resolves `docker` on a marker-only repo)
- c-2 → t-4
- c-3 → t-5
- c-4 → t-1
- c-5 → t-2
- c-6 → t-6
- c-7 → t-3, t-7

## Judgment calls
- Isolated the glob matcher (t-1) from the detector that uses it (t-2): chose a pure
  `matchPattern` testable in a table over inlining matching into the walk — the
  over-broad-pattern guard (c-4's biggest risk) gets a dedicated unit test surface
  rather than being reachable only through filesystem fixtures.
- New `MarkerProfiles` function instead of extending `Detect`/`DetectLanguages`:
  chose to leave both legacy functions byte-untouched (locked: winner-take-all
  stays) over threading patterns through `scoreProfile` — the latter would put the
  additive path through the same code the regression criterion (c-6) must hold
  constant, which is exactly the failure mode this lens guards against.
- `MarkerProfiles` walks the subtree (not just rootFilenames): chose tree-walk
  because `Dockerfile.dev` and `compose.override.yml` legitimately live in subdirs;
  rejected root-only matching, which would silently miss real markers — a false
  "no Docker tools" is the manifest blind spot the phase exists to close.
- Split the manifest union into two tasks (t-4 security, t-5 quality) rather than
  one: they edit different packages and own different dedup/agnostic-set risks, so a
  break in one must fail its own package's test, not a shared one.
- Made t-6 a dedicated regression-lock task depending on t-4/t-5 rather than folding
  regression asserts into each: c-6 ("byte-identical to before") is a distinct risk
  owned by one task with golden comparisons, so a leak from ANY of t-1/t-2/t-4/t-5
  surfaces in one named place.
- Kept t-7 (CLI) separate from t-3 (profile ship): `stack show/list` is a different
  surface (TOML encoder round-trip) than embed/validate; a render failure of
  file_patterns must fail a CLI test, not be assumed from the profile decoding.

risk: 7 tasks across 3 waves, criteria covered 7/7
