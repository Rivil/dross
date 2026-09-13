# Verification-lens plan — scanner-self-exclusion

Lens: every criterion was turned into its ideal test contract first; each task is the
smallest change that makes that contract satisfiable. Real-repo pins are paired with a
negative control wherever the pin could pass vacuously.

Probe facts this plan rests on (observed 2026-09-12, gitleaks 8.30.1 on PATH):
- `stack.DetectLanguages(<dross root>)` today returns more than `["go"]`:
  `fixtures/multilang-c3/ts-deadcode/src/*.ts` and `internal/mutation/testdata/ts-project`
  surface typescript. The c-1 real-repo pin is red today, green after t-1.
- `internal/techdebt/` carries 13 marker hits today (scan.go 4, scan_test.go 5,
  run_test.go 4) — the c-2 negative control has material to bite on.
- `gitleaks dir .` on dross: 241 hits; `gitleaks git .`: 245. With the allowlist body
  below (`[extend] useDefault = true` + `[[allowlists]] regexTarget = "line"`),
  dir-mode → 4, git-mode → 1. Every cleared hit is the 16-hex id/key shape (incl. the
  two in `internal/cmd/deferred_test.go` and `internal/phase/phase_test.go`). The
  residual is a prose hit (`public key, ed25519-verify` in a tracked spec.toml) that
  is outside the locked shape-only allowlist scope — see Judgment calls.
- A tests.json line that gitleaks actually flags (entropy > 3.5) is
  `"key": "b91bfa24fdf586c0"`; `50919a010c495368` is NOT flagged (entropy too low), so
  fixtures for the positive control must use the former.

Verified gitleaks config body (do not author from memory — this exact shape ran):

```toml
[extend]
useDefault = true

[[allowlists]]
description = "dross identity ids: 16-hex hashes in id/key context (survivors.toml keys, tests.json Key/key)"
regexTarget = "line"
regexes = ['''(?i)\b(id|key)\b["']?\s*[:=]\s*["']?[0-9a-f]{16}["']?''']
```

---

Phase scanner-self-exclusion — 7 tasks across 3 waves

Wave 1
  t-1  Add testdata+fixtures to shared skipDirs
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1
       description: Add "testdata" and "fixtures" to skipDirs; export `SkipDir(name string) bool`
                 and `SkipDirs() []string` (sorted copy) as the one shared definition. No walk
                 logic changes — extsInTree, detectLanguagesFrom and MarkerProfiles already
                 consult the map.
       contract:
         - TestDetectLanguagesSkipsFixtureDirs: temp root with main.go + testdata/x.ts +
           fixtures/y.py → detectLanguagesFrom == ["go"]; if either name is missing from
           skipDirs the result gains typescript/python and the test fails.
         - TestDetectFixtureOnlyExtIsUnsupported: temp root with only fixtures/a.kt (no
           root marker) → Detect returns Unsupported; today it returns kotlin.
         - TestMarkerProfilesSkipsTestdata: temp root with only testdata/Dockerfile →
           MarkerProfiles == []; today ["docker"].
         - TestDetectLanguagesDrossRootIsGoOnly: DetectLanguages(repoRoot) == exactly
           ["go"] (red today per the probe; passes only with the skip).
         - TestSkipDirsSingleDefinition: grep of every non-_test .go under internal/ for the
           string literal `"testdata"` in a map/slice literal finds only
           internal/stack/detect.go — a second copy per scanner fails this.
       depends_on: []
       status: pending

  t-2  Add [techdebt] exclude to project.toml schema
       files:    internal/project/project.go, internal/project/project_test.go
       covers:   c-2
       description: Add `Techdebt Techdebt` to Project (`toml:"techdebt,omitempty"
                 json:"techdebt,omitempty"`) with `Exclude []string` (`toml:"exclude,omitempty"`).
                 Doc comment states the glob semantics t-4 implements.
       contract:
         - TestProjectTechdebtExcludeRoundTrip: Load of a file containing
           `[techdebt]\n  exclude = ["internal/techdebt/", "*.golden"]` yields exactly that
           slice; Save then Load round-trips it; a file with no [techdebt] table loads with
           an empty slice (no error).
         - Existing TestTomlFieldsCarryMatchingJSONTags (internal/cmd/json_tag_parity_test.go)
           fails if the new fields lack matching json tags.
       depends_on: []
       status: pending

  t-3  Emit gitleaks identity-id allowlist per run
       files:    internal/security/allowlist.go, internal/security/allowlist_test.go
       covers:   c-4
       description: New file: `const AllowlistName = "gitleaks.toml"`, `IdentityIDPattern`
                 (the regex string above), `renderAllowlist() string` (pure body, the verified
                 config), `WriteAllowlist(runDir string) (string, error)` writing through
                 pathfence.Contain + pathfence.WriteFile and returning the written path.
       contract:
         - TestIdentityIDPatternMatchesShape: Go regexp (same RE2 engine gitleaks uses)
           compiled from IdentityIDPattern matches `"Key": "50919a010c495368"`,
           `"key": "b91bfa24fdf586c0"`, `key = "919acc418a9a0821"`, `id = 30dcd7db2eecf398`;
           does NOT match `api_key = "sk_live_…"` (built by string concat, never a literal),
           a 32-hex `key = "0123456789abcdef0123456789abcdef"`, or a 17-char
           `key = "50919a010c4953680"`. Widening the class or dropping the id/key context
           fails the negative rows.
         - TestAllowlistExtendsDefaultRules: rendered body contains `[extend]` +
           `useDefault = true` and `[[allowlists]]` with `regexTarget = "line"`; without
           useDefault a --config run would execute zero rules (allowlist would blind the
           scanner) — pinned textually here and behaviourally below.
         - TestWriteAllowlistRefusesEscape: WriteAllowlist("../x") style traversal returns
           the pathfence refusal; a good runDir yields <runDir>/gitleaks.toml on disk.
         - TestGitleaksAllowlistIntegration (t.Skip when `gitleaks` not on PATH, never a
           silent pass): temp tree with tests.json containing `"key": "b91bfa24fdf586c0"`
           and leak.txt with a concat-built stripe test token; `gitleaks dir <tmp>
           --no-banner --exit-code 1 --config <emitted> --report-format json --report-path
           <tmp>/out.json` reports the stripe rule and NO finding for tests.json; the same
           command without --config reports the generic-api-key hit on tests.json (positive
           control that the fixture is flaggable).
         - TestGitleaksDrossTreeNoIdentityHits (gitleaks-gated, skipped under -short):
           `gitleaks git <repoRoot> --config <emitted> --report-format json` yields no
           finding whose Secret matches `^[0-9a-f]{16}$`. This is the c-4 "zero
           generic-api-key hits on dross" leg scoped to the locked allowlist scope.
       depends_on: []
       status: pending

  t-4  Techdebt exclude-glob path filter
       files:    internal/techdebt/filter.go, internal/techdebt/filter_test.go
       covers:   c-2
       description: `Filter(repoDir string, paths, excludes []string) ([]string, error)`:
                 each path is made repo-relative; an entry ending in "/" is a directory
                 prefix; any other entry is path.Match'd against the rel path and, when the
                 pattern has no "/", also against the base name. path.ErrBadPattern is
                 returned wrapped with the offending entry.
       contract:
         - TestFilterDirPrefix: `internal/techdebt/` drops internal/techdebt/scan.go and
           internal/techdebt/sub/x.go, keeps internal/techdebtx/y.go and internal/x.go.
         - TestFilterGlobs: `*.golden` drops docs/a.golden and a.golden; `docs/*.md` drops
           docs/a.md but keeps docs/sub/a.md (path.Match star does not cross "/").
         - TestFilterBadPatternErrors: excludes = ["["] returns an error whose text names
           `[`; a silently-ignored bad pattern fails this.
         - TestFilterEmptyIsIdentity: nil excludes returns the input unchanged.
       depends_on: []
       status: pending

Wave 2
  t-5  Name exclusions in security detect/run/report  (depends t-1, t-3)
       files:    internal/security/recon.go, internal/security/recon_test.go,
                 internal/cmd/security.go, internal/cmd/security_test.go
       covers:   c-5, c-4
       description: `Exclusions{SkippedDirs []string; Allowlist string}` built from
                 stack.SkipDirs() + AllowlistName, carried on Manifest. `detect` prints
                 "skipped directories: …" and "gitleaks allowlist: gitleaks.toml (written into
                 the run dir by dross security run)". `run` calls WriteAllowlist, prints
                 "  gitleaks allowlist: <run-dir>/gitleaks.toml", and writeRunReport adds an
                 "## Exclusions" section listing skipped dirs + the allowlist path.
       contract:
         - TestManifestCarriesExclusions: BuildManifest(...).Exclusions.SkippedDirs contains
           "testdata", "fixtures", ".dross" and equals stack.SkipDirs() (drift between the
           printed set and the walk's set fails here).
         - TestSecurityDetectNamesExclusions: captureStdout of `security detect <tmp>`
           contains "skipped directories", "testdata", "fixtures" and "gitleaks.toml".
         - TestSecurityRunWritesAllowlist: after `security run .`, <run-dir>/gitleaks.toml
           exists and its body contains `useDefault = true`; stdout contains
           "gitleaks allowlist: " + that path.
         - TestSecurityRunReportRecordsExclusions: report.md contains "## Exclusions",
           "testdata", and "gitleaks.toml"; a run that narrows silently fails here.
         - Existing TestSecurityRunReadOnly stays green (the allowlist lands inside
           .dross/security/<run>).
       depends_on: [t-1, t-3]
       status: pending

  t-6  Wire skip set + excludes into dross techdebt  (depends t-1, t-2, t-4)
       files:    internal/cmd/techdebt.go, internal/cmd/techdebt_test.go, .dross/project.toml
       covers:   c-2, c-3
       description: trackedFiles drops any path with a component for which stack.SkipDir is
                 true (replaces the ls-files `.dross` prefix check and the walk's
                 `.git/.dross` switch — .git stays skipped via the set). The command loads
                 project.toml, applies techdebt.Filter with Techdebt.Exclude, surfaces a bad
                 pattern as the command error. dross's own .dross/project.toml gains
                 `[techdebt]\n  exclude = ["internal/techdebt/"]`.
       contract:
         - TestTechdebtDrossTreeNoMarkersInTechdebtPkg (real repo): load
           .dross/project.toml, trackedFiles(repoRoot) → Filter(excludes) → Scan; zero
           findings with Class marker whose File is under internal/techdebt/. Negative
           control in the same test: the unfiltered path set yields ≥1 marker there (13
           today), so deleting the project.toml exclude line or the Filter call fails it.
         - TestTrackedFilesSkipsFixtureDirs (synthetic git repo): tracked fixtures/big.txt
           (700 lines) + testdata/wide.txt (one 500-char line) + src/ok.go; trackedFiles
           returns only src/ok.go; Scan over it yields 0 findings, while Scan over the
           three raw files yields 1 oversized-file + 1 long-line (proves thresholds would
           have fired). Same assertion for the no-git walk fallback.
         - TestTechdebtDrossTreeNoFixtureFindings (real repo): full scan over
           trackedFiles(repoRoot) has no finding whose File contains "/testdata/" or
           "/fixtures/".
         - TestTechdebtBadExcludeErrors: project.toml with exclude = ["["] makes
           `dross techdebt` return an error naming the entry, and writes no run dir.
         - Existing TestTrackedFilesExcludesDrossDir and TestTechdebtEnumeratesTrackedFiles
           stay green unchanged.
       depends_on: [t-1, t-2, t-4]
       status: pending

Wave 3
  t-7  secure.md passes --config run-dir allowlist  (depends t-5)
       files:    assets/prompts/secure.md, internal/cmd/secure_prompt_test.go
       covers:   c-4, c-5
       description: In "2. Tooling sweep": Detect step says `dross security detect` also
                 names the exclusions in effect; Sweep step adds the gitleaks exemplar
                 `gitleaks git --config <run-dir>/gitleaks.toml -- .` explaining the run dir's
                 gitleaks.toml (identity-id allowlist, extends default rules, .dross not
                 path-excluded). Note r-01: `make install` before relying on it.
       contract:
         - TestSecurePromptMandatedSections gains case "c-4 gitleaks run-dir allowlist via
           --config" with needles "gitleaks", "--config", "gitleaks.toml", "allowlist";
           removing the --config instruction fails exactly that sub-test.
         - Same test gains case "c-5 detect names exclusions" with needle "exclusions" in
           the Detect step; the prompt must tell the agent the manifest records scope.
       depends_on: [t-5]
       status: pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-4, t-6 |
| c-3 | t-1, t-6 |
| c-4 | t-3, t-5, t-7 |
| c-5 | t-5, t-7 |

All 5 criteria covered.

## Judgment calls

- **Skip set applied in trackedFiles, not in Filter.** Chose: trackedFiles drops
  stack.SkipDir components on both branches; techdebt.Filter handles project excludes
  only. Rejected: one Filter doing both. Why: keeps t-4 a pure wave-1 task with no stack
  dependency, keeps the existing TestTrackedFilesExcludesDrossDir green untouched, and
  leaves exactly one place (the enumerator) that knows about directory skipping.
- **Consequence accepted: tracked `build/` and `dist/` now leave the tech-debt scan.**
  The shared set already holds them; the locked one-definition decision means techdebt
  inherits them. Rejected: a techdebt-specific subset (that is the drift the decision
  forbids).
- **Allowlist is shape-only, no `paths` entries, even for testdata/fixtures.** Chose:
  the emitter consumes the shared skip set only for the c-5 Exclusions record (report.md
  + detect), not as gitleaks path allowlists. Rejected: path-allowlisting skipDirs minus
  .dross. Why: locked allowlist_scope says shape only; a secret in testdata/ is a classic
  real finding; the probe shows the shape rule alone clears every identity hit.
- **c-4 "zero hits" is pinned as "zero identity-shape hits", not "zero generic-api-key
  hits".** The probe shows one residual git-mode generic-api-key hit
  (`.dross/phases/release-trust-and-distribution/spec.toml:21`, secret "ed25519-verify")
  that is prose, not the 16-hex shape, and cannot be cleared under the locked shape-only
  scope. Recommend the lead handle it via `dross security findings` dismissal or a
  one-word rewording of that spec line; the plan does not widen the allowlist to chase it.
- **Glob semantics: trailing "/" = prefix; else path.Match on rel path plus base name
  when the pattern has no "/".** Rejected: pulling in a doublestar dependency, or
  prefix-only. Why: `internal/techdebt/` (the only committed use) and `*.golden` both
  behave as a user expects, with no new module and no `**` promise.
- **Bad exclude pattern is a hard error, not a skip.** A silently ignored `[` would make
  the exemption look applied when it is not — the same silent-narrowing c-5 exists to
  stop.
- **gitleaks-backed tests skip (not pass) when the binary is absent, and the fixture
  secret is concat-built.** A literal stripe token in a _test.go would trip dross's own
  gitleaks run; `t.Skip` keeps a laptop without gitleaks honest rather than green.
- **Not in scope, deliberately:** `dross options` / options.md surfacing the new
  [techdebt] knob, and README schema prose. No criterion asks for it; flag for the lead
  as a follow-up quick.
