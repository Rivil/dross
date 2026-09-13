# Planner draft — MVP lens

Bias: smallest task set that satisfies every criterion. No task exists that
does not trace to a criterion id. No new packages beyond the one file the
allowlist emitter needs.

Phase scanner-self-exclusion — 6 tasks across 3 waves

Wave 1
  t-1  Share skipDirs; add testdata + fixtures
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1
       desc:     Add "testdata" and "fixtures" to the existing skipDirs map in
                 internal/stack/detect.go and export it as the single shared
                 definition: `SkipDir(name string) bool` (segment test) and
                 `SkipDirs() []string` (sorted names, for reporting). Detect,
                 detectLanguagesFrom and MarkerProfiles already read the map —
                 no walker changes. Tests: DetectLanguages(repoRoot) == ["go"]
                 on the dross checkout (walk up from the test's cwd to go.mod);
                 a temp tree holding only testdata/a.ts, fixtures/b.svelte and
                 fixtures/Dockerfile yields DetectLanguages == [], Detect ==
                 Unsupported, MarkerProfiles == [].
       contract: if "fixtures" is dropped from the set, TestDetectLanguagesRepoRootIsGoOnly
                 fails with ["go","javascript","svelte","typescript"] (the
                 pre-phase output of `dross security detect .`);
                 if "testdata" is dropped, TestSkipDirsHideFixtureOnlyStacks
                 fails on the DetectLanguages sub-assertion;
                 if MarkerProfiles stops consulting the shared set, the
                 fixtures/Dockerfile sub-assertion fails with ["docker"].
       depends:  —
       status:   pending

  t-2  Add [techdebt] exclude to project.toml schema
       files:    internal/project/project.go, internal/project/project_test.go,
                 .dross/project.toml
       covers:   c-2
       desc:     Add `Techdebt Techdebt` (toml:"techdebt,omitempty") to
                 project.Project with `Exclude []string` (toml:"exclude").
                 Doc the entry semantics on the field: repo-relative, a
                 trailing "/" means directory prefix, otherwise path.Match glob.
                 Add `[techdebt]\n  exclude = ["internal/techdebt/"]` to
                 dross's own .dross/project.toml.
       contract: if the field's toml tag is wrong, TestLoadDecodesTechdebtExclude
                 (body `[techdebt]\n exclude = ["internal/techdebt/", "*.gen.go"]`)
                 fails with an empty Exclude; if the json tag is omitted, the
                 existing TestTomlFieldsCarryMatchingJSONTags fails naming
                 Techdebt.Exclude; if dross's project.toml omits the entry,
                 t-4's TestTechdebtSelfScanExcludesOwnPackage fails.
       depends:  —
       status:   pending

  t-3  Secure prompt passes --config <run-dir>/gitleaks.toml
       files:    assets/prompts/secure.md, internal/cmd/secure_prompt_test.go
       covers:   c-4
       desc:     In §2 Sweep, add a gitleaks bullet: run
                 `gitleaks detect --config <run-dir>/gitleaks.toml --source <path>`
                 where <run-dir> is the directory `dross security run` printed;
                 state that the file carries the identity-id allowlist and is
                 regenerated per run (never committed). The filename is fixed by
                 the locked allowlist_delivery decision, so this needs no code.
                 Add a needle row to TestSecurePromptMandatedSections.
       contract: if the `--config` / `gitleaks.toml` / `run dir` wording is
                 removed from secure.md, the new "c-4 gitleaks allowlist via
                 --config" sub-test of TestSecurePromptMandatedSections fails.
       depends:  —
       status:   pending

Wave 2 (depends on wave 1)
  t-4  Filter techdebt paths by skip set + exclude
       files:    internal/cmd/techdebt.go, internal/cmd/techdebt_test.go
       covers:   c-2, c-3
       desc:     Add `excludePaths(repoDir string, paths, excludes []string) []string`
                 applied once after trackedFiles (so both the ls-files path and
                 the no-git walk are covered): drop any path with a segment for
                 which stack.SkipDir is true, and any repo-relative path matched
                 by a [techdebt].exclude entry (prefix when the entry ends in
                 "/", else path.Match). The RunE loads project.toml via
                 loadProject() and passes p.Techdebt.Exclude.
       contract: TestTechdebtSelfScanExcludesOwnPackage runs trackedFiles +
                 excludePaths over the real repo root with the excludes read
                 from .dross/project.toml, scans with DefaultThresholds, and
                 fails on the first ClassMarker finding whose File sits under
                 internal/techdebt/ — today that is 16 hits (scan.go's regex
                 line, the package doc, scan_test.go / run_test.go fixtures);
                 TestExcludePathsSkipsFixtureDirs seeds a temp git repo with
                 testdata/big.txt (700 lines), fixtures/long.txt (one 500-char
                 line) and code.go, and fails if the filtered set contains
                 either fixture path or if Scan yields any long-line /
                 oversized-file finding; if the segment check regresses to a
                 substring match, the `testdata-like/keep.go` case in the same
                 test fails.
       depends:  t-1, t-2
       status:   pending

  t-5  Emit gitleaks.toml allowlist in security run
       files:    internal/security/gitleaks.go, internal/security/gitleaks_test.go,
                 internal/cmd/security.go
       covers:   c-4
       desc:     New internal/security/gitleaks.go: const GitleaksConfigName =
                 "gitleaks.toml"; const IdentityIDAllow =
                 `(?i)\b(id|key)"?\s*[:=]\s*"?[0-9a-f]{16}\b` ; func
                 RenderGitleaksConfig(skip []string) string emitting
                 `[extend] useDefault = true` then `[allowlist]` with
                 `regexTarget = "line"`, `regexes = ['''<IdentityIDAllow>''']`,
                 and `paths` = one anchored regex per shared skip dir EXCEPT
                 ".dross" (locked allowlist_scope: .dross is never a blind
                 spot); func WriteGitleaksConfig(runDir string) error writes it
                 through pathfence.Contain/WriteFile. securityRun calls it
                 after writeRunReport and prints
                 `  gitleaks allowlist: <run-dir>/gitleaks.toml`.
       contract: TestIdentityIDAllowMatchesShape compiles IdentityIDAllow and
                 fails if it does not match `"key": "08ec1d7666c48b32"` or
                 `id = "5c88045d72401675"`, or if it DOES match an 18-hex value,
                 a 16-hex value under a `token =` key, or a bare 16-hex with no
                 id/key context; TestGitleaksConfigOmitsDrossPath fails if the
                 rendered paths list contains ".dross" or lacks "testdata";
                 TestSecurityRunWritesGitleaksConfig fails if the run dir has no
                 gitleaks.toml or the file lacks `useDefault = true` (a config
                 without extend would silently disable every built-in rule).
       depends:  t-1
       status:   pending

Wave 3 (depends on wave 2)
  t-6  Name exclusions in detect output and run report
       files:    internal/security/recon.go, internal/security/recon_test.go,
                 internal/cmd/security.go, internal/cmd/security_test.go
       covers:   c-5
       desc:     Add `Exclusions []string` to security.Manifest, populated in
                 BuildManifest from stack.SkipDirs(). securityDetect prints
                 `excluded dirs: <comma list>` and
                 `gitleaks allowlist: .dross/security/<run-id>/gitleaks.toml
                 (written by dross security run)`; writeRunReport adds an
                 `## Exclusions` section listing the skipped dirs and the
                 run-relative gitleaks.toml path before `## Findings`.
       contract: TestManifestCarriesExclusions fails if BuildManifest on a
                 temp go tree returns a Manifest whose Exclusions lacks
                 "testdata" or "fixtures"; TestSecurityDetectNamesExclusions
                 fails if detect stdout lacks "excluded dirs:" with "testdata"
                 or lacks "gitleaks.toml"; TestSecurityRunReportNamesExclusions
                 fails if report.md has no "## Exclusions" heading naming
                 "fixtures" and "gitleaks.toml".
       depends:  t-1, t-5
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2, t-4 |
| c-3 | t-4 |
| c-4 | t-3, t-5 |
| c-5 | t-6 |

Every criterion accounted for; every task traces to at least one criterion.

## Judgment calls

- One filter function after enumeration (t-4) instead of teaching both the
  ls-files branch and the no-git walk about skip dirs: covers both branches
  with one code path and one test. Rejected: editing the WalkDir `switch` and
  the ls-files loop separately — two copies of the same rule.
- Skip set exported as `SkipDir(name) bool` + `SkipDirs() []string` (t-1), not
  a copied slice per consumer: the locked skip_dir_set decision demands one
  definition; techdebt needs a segment predicate, the emitter and manifest
  need the list. Rejected: a new `internal/scanscope` package — nothing else
  would live there.
- Gitleaks `paths` allowlist = shared skip set minus `.dross` (t-5). The two
  locked decisions pull apart on a literal read: skip_dir_set says the emitter
  consumes the shared set; allowlist_scope says "identity-hash shape only" and
  "`.dross/` is not path-excluded". I honoured the specific prohibition (.dross
  stays scanned) and the shared-consumer requirement; the test pins `.dross`
  absent and `testdata` present. If the judge reads "only" strictly, drop the
  `paths` line and its assertion — nothing else in the plan changes.
- `regexTarget = "line"` for the allowlist regex so the id/key context is part
  of the match. Rejected: default secret-target `^[0-9a-f]{16}$` — that would
  allowlist any 16-hex secret regardless of context, wider than
  allowlist_scope permits.
- c-4 proof is a unit test on the exported regex plus a run-dir write test,
  not a gitleaks-installed e2e: the criterion asks that "a test pins that the
  allowlist matches the shape". Rejected: `t.Skip` unless gitleaks on PATH —
  speculative, and a skipped test proves nothing in CI.
- t-3 (prompt edit) sits in wave 1, not behind t-5: the filename is fixed by
  the locked allowlist_delivery decision, so the prompt needs no code output.
  Rejected: merging t-3 into t-5 — that made a 5-file, two-layer task.
- t-2 and t-4 split by layer (schema vs command). Rejected: one task — 5 files
  across config + cmd, and t-2 has value alone (adopters get the knob).
- Detect (t-6) prints the allowlist location as a pattern
  (`.dross/security/<run-id>/gitleaks.toml`) because detect creates no run
  dir. Rejected: making detect write a file — detect is read-only today and
  TestSecurityRunReadOnly's sibling expectations should stay that way.
- [techdebt].exclude semantics: trailing "/" = prefix, else path.Match on the
  repo-relative path. Rejected: pulling in doublestar for `**` globs — the
  locked example is a directory prefix; one stdlib matcher suffices.
