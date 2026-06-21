# Phase 09-marker-file-detection — VERIFICATION lens

Designed backward from test contracts. For each criterion I wrote the ideal contract
that would fail if the behaviour regressed, then derived the smallest task that makes
that contract satisfiable. The load-bearing seam is a NEW generic
`stack.MarkerProfiles(root, profiles) []string` that returns marker-matched profile ids,
which the secure/quality `BuildManifest` union onto `DetectLanguages` results — the
single new surface every recon criterion (c-2/c-3/c-5/c-6) asserts against.

---

## Ideal test contracts (written first)

- **c-1** Detect `docker` from a marker only. Contract: a temp dir with ONLY `Dockerfile`
  (no `.go`/source ext) → some matcher resolves `docker`. Two failure modes: pattern
  matching broken, or the profile absent. → `stack.MarkerProfiles` test + `embed` test.
- **c-2** Security manifest surfaces hadolint+trivy on a Dockerfile-only repo. Contract:
  `security.BuildManifest(tmp{Dockerfile}, allMissing).Skipped()` names contain `hadolint`
  AND `trivy config`. If the union step is dropped, names omit them → fails.
- **c-3** Quality manifest surfaces hadolint(kind=analyzer). Contract:
  `quality.BuildManifest(tmp{Dockerfile}, allMissing)` Skipped names contain `hadolint`,
  alongside the agnostic `scc`/`jscpd`. Drop union → fails.
- **c-4** Filename families match; unrelated file does not. Contract: a table over
  `Dockerfile`, `Dockerfile.dev`, `app.Dockerfile`, `app.dockerfile`,
  `docker-compose-prod.yaml`, `compose.override.yml`, `compose.yaml` → each yields
  `docker`; `notes.txt`, `README.md`, `mydockerfile.go` → do NOT. Over-broad glob → the
  negative rows fail.
- **c-5** Generic, no docker hardcode. Contract: a SYNTHETIC profile (id `widget`,
  `file_patterns=["*.widget"]`, no exts) passed to `MarkerProfiles` over a `x.widget`
  tree → returns `widget`; AND a grep-style guard test asserting detect/recon source has
  no literal `"docker"`/`"Dockerfile"`. Hardcode → guard fails.
- **c-6** No-marker regression. Contract: tmp with only `main.go` → `MarkerProfiles`
  returns empty (no `docker`), and `BuildManifest` Skipped names are exactly the pre-phase
  Go+agnostic set (no hadolint/trivy-config). Accidental match → docker leaks in → fails.
- **c-7** CLI surface. Contract: `stack list` output contains `docker`; `stack show docker`
  exits 0 and its TOML output contains `file_patterns` and `hadolint`. Missing/ broken
  profile → fails.

---

Phase 09-marker-file-detection — 7 tasks across 3 waves

Wave 1
  t-1  Add file_patterns field with glob matcher
       files:    internal/stack/profile.go
                 internal/stack/profile_test.go
       covers:   c-4, c-5
       contract: TestFilePatternMatch in profile_test.go drives a table: a
                 `MatchesPattern(name string) bool` (or exported helper on Signals)
                 returns true for Dockerfile/Dockerfile.dev/app.Dockerfile/
                 app.dockerfile/docker-compose-prod.yaml/compose.override.yml/
                 compose.yaml and FALSE for notes.txt/README.md/mydockerfile.go,
                 with patterns + filename both lowercased; if matching becomes
                 substring/regex/case-sensitive a row fails. Decode test asserts
                 `[signals].file_patterns` round-trips into Signals.FilePatterns.

  t-2  Add generic MarkerProfiles detector
       files:    internal/stack/detect.go
                 internal/stack/detect_test.go
       covers:   c-1, c-5, c-6
       contract: TestMarkerProfiles in detect_test.go: (a) tmp with only `Dockerfile`
                 + the embedded docker profile → result contains `docker` (c-1);
                 (b) a synthetic in-memory profile {id:"widget",
                 FilePatterns:["*.widget"]} over a `x.widget` tree → contains
                 `widget`, proving data-driven, no docker hardcode (c-5);
                 (c) tmp with only `main.go` → result is EMPTY (c-6). MarkerProfiles
                 walks rootFilenames, returns every profile id with a file_patterns
                 hit; depends on t-1's matcher. If it scans subtree exts or hardcodes
                 ids a case fails.

  t-3  Ship embedded docker stack profile
       files:    internal/stack/profiles/docker.toml
                 internal/stack/embed_test.go
       covers:   c-1, c-7
       contract: TestEmbeddedDocker in embed_test.go: `Embedded()` includes id
                 `docker`; ByID(docker) has FilePatterns covering Dockerfile +
                 compose globs, and Tools = hadolint(scanner) + hadolint(analyzer) +
                 `trivy config`(scanner), no dockle/checkov. If the TOML is malformed
                 Embedded() errors; if loadout drifts the tool assertions fail.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Union marker profiles into security manifest
       files:    internal/security/recon.go
                 internal/security/recon_test.go
       covers:   c-2, c-6
       contract: TestBuildManifestMarkerDocker: BuildManifest(tmp{Dockerfile},
                 allMissing).Skipped() names contain `hadolint` AND `trivy config`
                 (c-2). TestBuildManifestNoMarkerRegression: BuildManifest(
                 tmp{main.go}) Skipped names contain the Go+agnostic set and do NOT
                 contain `hadolint`/`trivy config` (c-6). BuildManifest now calls
                 MarkerProfiles and add(ScannersFor(id)) per matched marker id atop
                 DetectLanguages; if the union is removed the docker test fails, if it
                 over-matches the regression test fails.

  t-5  Union marker profiles into quality manifest
       files:    internal/quality/recon.go
                 internal/quality/recon_test.go
       covers:   c-3, c-6
       contract: TestQualityManifestMarkerDocker: BuildManifest(tmp{Dockerfile},
                 allMissing).Skipped() names contain `hadolint` (kind=analyzer) atop
                 agnostic `scc`/`jscpd` (c-3). TestQualityNoMarkerRegression:
                 tmp{main.go} → no `hadolint` (c-6). Mirrors t-4 via
                 MarkerProfiles + AnalyzersFor; drop union → docker test fails.

Wave 3 (depends t-2, t-4, t-5)
  t-6  Guard: no Docker hardcode in detect/recon
       files:    internal/stack/detect_test.go
       covers:   c-5
       contract: TestNoDockerHardcode reads detect.go + security/recon.go +
                 quality/recon.go source and fails if any contains the literal
                 `"docker"` or `"Dockerfile"` — proving marker detection is purely
                 data-driven (mirrors the existing TestNoDuplicateExtLangMap idiom).
                 Needs the wave-2 recon files to exist in their final shape to assert
                 against.

  t-7  Verify docker profile on stack list/show CLI
       files:    internal/cmd/stack_test.go
       covers:   c-7
       contract: TestStackListIncludesDocker: `stack list` captured stdout contains
                 `docker`. TestStackShowDocker: `stack show docker` returns nil error
                 and its encoded TOML output contains `file_patterns` and `hadolint`.
                 No production change expected (list/show are already generic over
                 LoadAll); if docker.toml fails to embed or the field doesn't encode,
                 these fail. Depends on the docker profile (t-3) reaching the merged
                 set, asserted end-to-end through the cmd layer.

---

## Coverage

| criterion | tasks            |
| --------- | ---------------- |
| c-1       | t-2, t-3         |
| c-2       | t-4              |
| c-3       | t-5              |
| c-4       | t-1              |
| c-5       | t-1, t-2, t-6    |
| c-6       | t-2, t-4, t-5    |
| c-7       | t-3, t-7         |

All of c-1..c-7 accounted for (7/7).

## Judgment calls

- Chose a single generic `stack.MarkerProfiles(root, profiles) []string` as the shared
  seam (t-2); rejected duplicating marker-walk logic inside each recon package, because
  c-5's no-hardcode + synthetic-profile contract is only cleanly testable at one
  data-driven surface, and recon must not grow a second detector (mirrors the existing
  single-`DetectLanguages` rule).
- Put the glob matcher (t-1) as its own wave-1 task with the c-4 table as its driving
  contract; rejected folding it into MarkerProfiles, because the over-broad-pattern guard
  (negative rows: `mydockerfile.go`, `README.md`) is the highest-risk regression and
  deserves a focused unit table, not assertion through a tree walk.
- Split security (t-4) and quality (t-5) recon unions rather than one task, because they
  are independent files in independent packages with distinct tool-loadout contracts
  (scanner pair vs analyzer); merging would make one task touch two layers with two
  separate assertions — the split keeps each contract sharp and each commit atomic.
- Made the no-hardcode guard (t-6) wave-3, depending on the recon files reaching final
  shape — a guard that scans source must run against the post-edit source, else it asserts
  nothing. Rejected putting it in wave-1 for the same reason.
- Kept c-7 as a test-only task (t-7) through the real `cmd` layer rather than asserting on
  `Embedded()` alone (already covered by t-3), because c-7 names `stack list`/`stack show`
  specifically — the contract must exercise that exact surface end-to-end.
- Did NOT add a Detect() winner-take-all change: locked decision keeps single-id selection
  unchanged, so no task touches scoreProfile/Detect — marker detection is additive-only via
  the new MarkerProfiles path.
