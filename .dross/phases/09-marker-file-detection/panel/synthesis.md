# Phase 09-marker-file-detection — synthesis (cold judge)

Three drafts authored independently through risk / mvp / verification lenses. I
authored none. Below: scores, the merged plan grafted from the strongest, and the
real disagreements left visible rather than papered over.

Source-file adjudication done against `internal/stack/{detect,profile,embed}.go`,
`internal/{security,quality}/recon.go`, and both `catalog.go` files. Key fact that
settles several disputes: `profileScanners`/`profileAnalyzers` key off
`stack.ByID(profiles, lang)` — i.e. **`ScannersFor("docker")` already resolves the
docker profile's tools by profile id**. The additive union all three drafts assume
is therefore wiring-only; no catalog change is needed.

## Scores

Each cell rates the draft 1–5 on that dimension.

| Draft        | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
| ------------ | ----------------- | ------------------------- | ----------- | ---------------- |
| risk         | 5 — 7/7, c-1 split t-3+t-6, c-7 split t-3+t-7 | 5 — sharpest: names exact negative rows + `.yml`/`.yaml` brace-collapse failure | 4 — 7 tasks; t-6 golden-lock is high-value but its "byte-identical golden" is heavier than spec asks | 5 — 3 waves, deps all real (t-2 after t-1+t-3; recon after t-2) |
| mvp          | 5 — 7/7 but c-5/c-6/c-7 all crammed into one t-3 contract | 3 — solid but one mega-contract for t-3 dilutes per-criterion failure isolation | 2 — 3 tasks; merges security+quality recon (two packages, two loadout contracts) into one, and folds the `Detect()` scoreProfile change in (see Disagreement D) | 3 — 2 waves; correct deps but the merge erases the security/quality independence |
| verification | 5 — 7/7, coverage table explicit, contracts written first | 5 — derives task from contract; names exact tmp fixtures + `allMissing` shape | 5 — 7 tasks at clean per-criterion granularity; t-6 is a cheap grep-guard, not a golden | 4 — 3 waves; t-6 (no-hardcode guard) correctly wave-3, but bundles c-1 into t-2 AND t-3 |

**Skeleton: `verification`.** It has the cleanest per-criterion granularity, contracts
derived backward from failures, and the lightest correct regression guard (a
no-hardcode source grep mirroring the existing `TestNoDuplicateExtLangMap` idiom)
rather than risk's heavier byte-identical golden. risk is the strong runner-up and
donates the single best contract (the glob over-broad table) and one task verification
lacks at full strength.

## Merged plan

Phase 09-marker-file-detection — 7 tasks across 3 waves

### Wave 1

**t-1  Add `file_patterns` field with case-insensitive glob matcher**  [verification, contract grafted from risk]
- files: `internal/stack/profile.go`, `internal/stack/profile_test.go`
- covers: c-4, c-5 (field genericity)
- contract: Add `Signals.FilePatterns []string` (toml `file_patterns`) and a pure
  matcher that lowercases BOTH filename and pattern before `filepath.Match` (glob, not
  regex; no brace expansion). `TestFilePatternMatch` table: `Dockerfile`,
  `Dockerfile.dev`, `app.Dockerfile`, `app.dockerfile`, `docker-compose-prod.yaml`,
  `compose.override.yml`, `compose.yaml` MUST match docker's patterns; `notes.txt`,
  `README.md`, `mydockerfile.go`, `compose.go` MUST NOT (over-broad guard). If
  `.yml`/`.yaml` brace-collapses, the row asserting `compose.yaml` does NOT match a
  `*.yml`-only pattern fails (grafted from risk t-1). Decode test asserts
  `[signals].file_patterns` round-trips into `Signals.FilePatterns`.
- depends_on: —

**t-2  Add generic `MarkerProfiles` detector**  [verification, scope-corrected — see Disagreement D]
- files: `internal/stack/detect.go`, `internal/stack/detect_test.go`
- covers: c-1, c-5, c-6
- contract: New exported `MarkerProfiles(root string, profiles []*Profile) []string`
  returning the sorted set of profile ids whose `FilePatterns` match, using t-1's
  matcher. Touches NEITHER `Detect` NOR `DetectLanguages` (risk's isolation rationale).
  `TestMarkerProfiles`: (a) tmp with only `Dockerfile` + docker profile → contains
  `docker` (c-1); (b) synthetic in-memory profile `{id:"widget", FilePatterns:["*.widget"]}`
  over an `x.widget` tree → contains `widget`, proving no docker hardcode (c-5); (c) tmp
  with only `main.go` → EMPTY (c-6). **Default: walk the subtree reusing `skipDirs`**
  (risk) so `sub/Dockerfile.dev` and `compose.override.yml` in subdirs are caught — see
  Disagreement E.
- depends_on: t-1

**t-3  Ship embedded docker stack profile**  [risk+verification, both name it identically]
- files: `internal/stack/profiles/docker.toml`, `internal/stack/embed_test.go`
- covers: c-1 (ship), c-7 (embed/validate)
- contract: New `docker.toml`, `id="docker"`, `[signals].file_patterns` covering the
  c-4 families and NO `exts`; tools = hadolint (kind=scanner), hadolint (kind=analyzer),
  trivy config (kind=scanner) — matching locked `docker_tool_loadout`. `TestEmbeddedDocker`:
  `Embedded()` includes `docker`; `ByID(docker)` has the FilePatterns + exactly that
  loadout; if `dockle`/`checkov` appear or hadolint is missing either kind, the tool
  assertion fails; malformed TOML → `Embedded()` errors.
- depends_on: —  (data file; sits in wave 1, mvp's correct call)

### Wave 2  (depends t-1, t-2, t-3)

**t-4  Union marker profiles into security manifest**  [risk+verification]
- files: `internal/security/recon.go`, `internal/security/recon_test.go`
- covers: c-2, c-6
- contract: `BuildManifest` calls `stack.MarkerProfiles(root, LoadAll())` after the
  language loop and `add(ScannersFor(id))` per matched marker id — additive, deduped via
  the existing `seen` set, `Languages` unchanged. `TestBuildManifestMarkerDocker`:
  `BuildManifest(tmp{Dockerfile}, allMissing).Skipped()` names contain `hadolint` AND
  `trivy config` (c-2). `TestBuildManifestNoMarkerRegression`: `tmp{main.go}` →
  Go+agnostic set, NO `hadolint`/`trivy config` (c-6). Dedup test: Go+Dockerfile repo
  surfaces no duplicated scanner entry (risk's dedup row).
- depends_on: t-2, t-3

**t-5  Union marker profiles into quality manifest**  [risk+verification]
- files: `internal/quality/recon.go`, `internal/quality/recon_test.go`
- covers: c-3, c-6
- contract: Mirror of t-4 via `MarkerProfiles` + `AnalyzersFor`.
  `TestQualityManifestMarkerDocker`: `tmp{Dockerfile}` Skipped names contain `hadolint`
  (kind=analyzer) ON TOP of the agnostic `scc`/`jscpd` set (the agnostic set must still
  be present — a marker-only repo never loses scc/jscpd, risk t-5). `TestQualityNoMarkerRegression`:
  `tmp{main.go}` → no `hadolint` (c-6).
- depends_on: t-2, t-3

### Wave 3  (depends wave 2)

**t-6  Guard: no Docker hardcode in detect/recon source**  [verification]
- files: `internal/stack/detect_test.go`
- covers: c-5 (no-hardcode keystone)
- contract: `TestNoDockerHardcode` reads `detect.go` + `security/recon.go` +
  `quality/recon.go` source and fails on any literal `"docker"`/`"Dockerfile"`, proving
  marker detection is purely data-driven (mirrors existing `TestNoDuplicateExtLangMap`).
  Must run against the post-edit recon files, hence wave 3.
- depends_on: t-2, t-4, t-5

**t-7  Verify docker profile on `stack list`/`show` CLI**  [risk+verification]
- files: `internal/cmd/stack_test.go`
- covers: c-7
- contract: `TestStackListIncludesDocker`: `stack list` stdout contains `docker`.
  `TestStackShowDocker`: `stack show docker` returns nil error and its encoded TOML
  contains `file_patterns` and `hadolint`. Test-only — list/show are already generic over
  `LoadAll()`; fails if `docker.toml` fails to embed or `file_patterns` doesn't encode.
- depends_on: t-3

### Coverage map

| criterion | tasks         |
| --------- | ------------- |
| c-1       | t-2, t-3      |
| c-2       | t-4           |
| c-3       | t-5           |
| c-4       | t-1           |
| c-5       | t-1, t-2, t-6 |
| c-6       | t-2, t-4, t-5 |
| c-7       | t-3, t-7      |

7/7 covered. No task contradicts the three locked decisions (additive union preserved;
new `[signals].file_patterns` field, exact-match `files` untouched; loadout = hadolint
scanner+analyzer + trivy config scanner, dockle/checkov excluded).

## Disagreements

**A. Granularity: 3 tasks (mvp) vs 7 (risk, verification) — the central divergence.**
mvp merges field+matcher+`MarkerProfiles` into one task and security+quality recon into
another; risk and verification keep them split 7 ways. **Default taken: 7 tasks (the
2-of-3 majority and the spec's per-criterion contract style).** Why it matters: mvp's
merged t-3 carries c-2/c-3/c-5/c-6/c-7 in one contract, so a single failing assertion
can't localize which criterion regressed — the opposite of what verification's
backward-from-contract design buys. The split also keeps each commit atomic and keeps the
security/quality recon contracts (scanner-pair vs analyzer) from sharing one test.
**mvp's one win is grafted in regardless:** docker.toml as a wave-1 data task with no code
dep (t-3 above), which risk/verification also place in wave 1 — consensus, not conflict.

**B. Regression guard: golden byte-comparison (risk t-6) vs source grep (verification t-6).**
risk wants a recorded golden manifest compared byte-identical; verification wants a
`TestNoDockerHardcode` source scan plus per-recon regression rows folded into t-4/t-5.
**Default: verification's approach** — regression rows live in t-4/t-5 (where the union
they guard lives), and the standalone wave-3 guard is the cheap no-hardcode grep. Why it
matters: a byte-identical golden is brittle (any unrelated tool-list reorder breaks it and
trains people to regenerate goldens blindly), whereas asserting "Skipped names contain the
Go+agnostic set and NOT hadolint/trivy" is the actual c-6 contract without the brittleness.
risk's *intent* (c-6 owned in a named place) is preserved by keeping the regression
assertions explicit in t-4/t-5.

**C. c-1 ownership.** risk assigns c-1 to t-3+t-6 (t-6c re-proves via `Detect()`);
verification to t-2+t-3. **Default: t-2+t-3** (the `MarkerProfiles` path is where
marker-only resolution actually lives). Why it matters: risk's t-6c asserts
`stack.Detect()` returns `docker` on a marker-only repo — but under the locked additive
decision, `Detect()`'s winner-take-all scoring is explicitly NOT fed file_patterns
(Disagreement D), so a `Detect()`-returns-docker assertion would require either changing
scoreProfile (locked out) or would simply fail. **risk's t-6c is the one task fragment
that risks contradicting a locked decision and is therefore dropped; c-1 is proven via
`MarkerProfiles` (t-2) instead.** This is the clearest place the lenses genuinely conflict.

**D. Does `Detect()`/`scoreProfile` learn about file_patterns? — the locked-decision test.**
mvp t-1 explicitly says "fold pattern matching into `scoreProfile` so `Detect()` resolves a
pattern-only profile." risk and verification explicitly REFUSE to touch `Detect`/`scoreProfile`.
**Default: do NOT touch `Detect`/`scoreProfile` (risk/verification).** Why it matters: the
locked `marker_detection_additive` decision says "`Detect()`'s winner-take-all single-id
selection stays unchanged." mvp's fold is the one concrete instruction across all three
drafts that **contradicts a locked decision**, so it is invalid and excluded — marker
resolution is additive-only via the new `MarkerProfiles` seam. (mvp's c-1 fixture intent —
a Dockerfile-only tree resolves docker — is preserved, just routed through `MarkerProfiles`
not `Detect`.)

**E. Marker scan scope: root-only vs subtree walk.** mvp t-1 walks `rootFilenames` only;
verification t-2 says "walks rootFilenames" too; risk t-2 walks the full subtree (reusing
`skipDirs`) and argues `Dockerfile.dev`/`compose.override.yml` legitimately live in
subdirs. **Default: subtree walk (risk).** Why it matters: c-4's example set includes
`compose.override.yml` and the phase exists to *close* a manifest blind spot — a root-only
scan would silently miss a `sub/Dockerfile.dev` and reintroduce a false "no Docker tools",
the exact failure the phase targets. The cost is matching the existing `skipDirs`/WalkDir
pattern already in `extsInTree`/`DetectLanguages`, so it adds no new traversal machinery.
Note this is a 1-vs-2 split where the minority (risk) is correct on the merits against the
criteria text; flagged rather than resolved by majority.

**F. profile_test.go vs detect_test.go for the matcher's home.** verification puts the
matcher + its table in `profile.go`/`profile_test.go` (matcher as a `Signals` helper);
risk puts the field in `profile.go` but the matcher in `detect.go`/`detect_test.go`.
**Default: matcher in `profile.go` with `TestFilePatternMatch` in `profile_test.go`**
(verification) — it keeps the pure, table-tested glob unit next to the `Signals` type it
operates on and off the filesystem-walking `detect.go`. Minor, but recorded because it
changes which file t-1 vs t-2 own and thus the atomic-commit boundary.

---

synthesis: 7 tasks across 3 waves, 6 disagreements
