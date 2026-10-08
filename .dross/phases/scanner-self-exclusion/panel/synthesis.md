# Synthesis — scanner-self-exclusion

Cold judge over three independent drafts (risk / mvp / verification). Every file
path, helper name and probe fact cited below was checked against the tree on
2026-09-12 (gitleaks 8.30.1 on PATH). Two facts the drafts did not have:

- gitleaks' **default rules already allowlist `AKIAIOSFODNN7EXAMPLE`** (`.+EXAMPLE$`
  on the aws rule): `gitleaks dir` over a file holding it exits 0. risk's live-test
  fixture would therefore read as "config disabled the rules" on a correct config.
  A concat-built `sk_test_…` stripe token does fire (exit 1). verification's fixture
  is the right one.
- The residual prose hit verification names is real:
  `.dross/phases/release-trust-and-distribution/spec.toml:21` ("public key,
  ed25519-verify"). It is not the 16-hex shape and cannot be cleared under the locked
  `allowlist_scope`.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk | 5/5 — all criteria, each split half named | 5/5 — negative controls, over-exclusion bounds, decoded `useDefault`, no-`paths` pin, exact-path Stat; one bad fixture (AKIA…EXAMPLE) and an unprobed `regexTarget = "match"` | 4/5 — t-4/t-5 are 4-file, two-layer tasks; validate glob check is a sound extra | 5/5 — t-6 correctly wave 2 on t-3 only; t-7 wave 3 on t-4 |
| mvp | 5/5 — all criteria | 3/5 — `useDefault` pinned textually only; self-scan pin can pass vacuously (no negative control); emits `paths` against `allowlist_scope`; no live proof | 4/5 — smallest set, clean layer split (t-2/t-4) | 4/5 — t-2 puts the project.toml entry in wave 1 before the mechanism exists; t-5/t-6 overlap on printing the allowlist path |
| verification | 5/5 — all criteria | 5/5 — the only draft whose regex body and fixture actually ran; entropy quirk (`50919a010c495368` does not fire), positive control without `--config`, real-repo negative controls | 4/5 — t-4 pure filter is ideal; t-6 is a 3-file + 5-test task | 4/5 — t-4 correctly wave 1 with no deps; t-7 prompt over-constrained to wave 3 (only the c-5 needle needs t-5) |

**Skeleton: verification.** Its contracts are empirically grounded (the config body
ran against the real tool; it knows which tests.json lines fire), its pure-filter
task sits in wave 1 where it belongs, and it took the shape-only reading of the
allowlist that two of three planners and the locked decision support. risk is a
close second and supplies most of the grafts: the decoded-`useDefault` test, the
no-`paths` pin, segment-not-substring, over-exclusion bounds, the validate glob
check, and the report-path Stat.

## Merged plan

Phase scanner-self-exclusion — 7 tasks across 3 waves

Wave 1
  t-1  Share skipDirs; add testdata + fixtures                    [verification+risk+mvp]
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1, c-3 (skip-set definition)
       depends:  —
       description:
         Add "testdata" and "fixtures" to the existing `skipDirs` map
         (internal/stack/detect.go:27). Export `SkipDir(name string) bool` (segment
         predicate) and `SkipDirs() []string` (sorted copy) as the one shared
         definition. No walker changes: extsInTree, detectLanguagesFrom and
         MarkerProfiles already consult the map; the `path != root` guard stays.
       test_contract:
         - TestDetectLanguagesDrossRootIsGoOnly [all three]: DetectLanguages(repoRoot)
           == exactly []string{"go"}. Red today — `dross security detect .` prints
           "go, javascript, svelte, typescript" from fixtures/ and
           internal/mutation/testdata. If either name leaks back into any of the
           three walks the exact-equality fails.
         - TestSkipDirIsSegmentNotSubstring [risk]: tempdir with `mytestdata/a.py`,
           `testdata.py` (a file) and `testdata/b.rb`; DetectLanguages returns
           python and NOT ruby. A substring/prefix comparison fails the first
           assertion; a missing IsDir guard fails the second.
         - TestDetectFixtureOnlyExtIsUnsupported [verification]: root holding only
           fixtures/a.kt → Detect returns Unsupported (today: kotlin).
         - TestMarkerProfilesSkipsFixtures [risk+verification]: root holding only
           fixtures/iac/Dockerfile → MarkerProfiles == []; move it to root →
           ["docker"]. Fails if MarkerProfiles is not on the shared set.
         - TestSkipDirsSingleDefinition [risk+verification]: SkipDirs() contains
           ".dross", "testdata", "fixtures", "node_modules", "vendor" and is
           sort.StringsAreSorted; a grep of non-_test .go under internal/ finds the
           literal `"testdata"` in a map/slice literal only in
           internal/stack/detect.go — a second copy per scanner fails.

  t-2  [techdebt] exclude schema + validate glob check           [risk+verification+mvp]
       files:    internal/project/project.go, internal/project/project_test.go,
                 internal/cmd/validate.go, internal/cmd/validate_test.go
       covers:   c-2 (schema half)
       depends:  —
       description:
         Add `Techdebt Techdebt` to Project (`toml:"techdebt,omitempty"
         json:"techdebt,omitempty"`) with `Exclude []string`
         (`toml:"exclude,omitempty" json:"exclude,omitempty"`), mirroring Mutation
         (project.go:299). Doc comment states the semantics t-4 implements
         (repo-relative; trailing "/" = directory prefix; else path.Match).
         `dross validate` runs the existing `checkGlob` (validate.go:404) over each
         entry and reports `project.toml: techdebt.exclude[i] %q does not compile`.
       test_contract:
         - TestProjectTechdebtExcludeRoundTrip [risk+verification]: Load a body with
           `[techdebt]\n exclude = ["internal/techdebt/", "*.golden"]`, Save to a
           temp path, Load again → Exclude deep-equals the original in order.
           Dropping omitempty or mis-tagging fails the second Load.
         - TestProjectNoTechdebtSectionIsNil [risk+verification]: no [techdebt]
           table → `p.Techdebt.Exclude == nil`, no error.
         - Existing TestTomlFieldsCarryMatchingJSONTags
           (internal/cmd/json_tag_parity_test.go:48) fails if the json tags are
           missing [mvp+verification].
         - TestValidateFlagsBadTechdebtGlob [risk]: `exclude = ["[abc"]` → validate
           output contains `techdebt.exclude[0]` and `does not compile`;
           `["internal/techdebt/"]` produces no techdebt problem line.

  t-3  Emit gitleaks identity-id allowlist per run                [verification+risk]
       files:    internal/security/gitleaks.go, internal/security/gitleaks_test.go
       covers:   c-4 (emit + shape-pin half)
       depends:  —
       description:
         New file: `const GitleaksConfigName = "gitleaks.toml"`; `IdentityIDAllowlist`
         (compiled *regexp.Regexp, pattern
         `(?i)\b(id|key)\b["']?\s*[:=]\s*["']?[0-9a-f]{16}["']?`);
         `renderGitleaksConfig(skipped []string) string` emitting the verified body —
         `[extend] useDefault = true`, one `[[allowlists]]` with `regexTarget = "line"`,
         the regex, and a `description` that names the skipped dirs (the shared set's
         only role here; never a `paths` key); `WriteGitleaksConfig(runDir string,
         skipped []string) (string, error)` via pathfence.Contain + pathfence.WriteFile,
         returning the written path, idempotent on re-invocation.
       test_contract:
         - TestGitleaksConfigExtendsDefault [risk]: decode the written file with
           BurntSushi into `{Extend struct{UseDefault bool}}` and require true. Drop
           or rename `[extend]` and this fails — the only thing between `--config`
           and a zero-rule scan.
         - TestIdentityIDAllowlistShape [risk+verification+mvp] (table, regex parsed
           back out of the written TOML, not the Go constant): MUST match
           `"key": "b91bfa24fdf586c0"`, `"Key": "50919a010c495368"`,
           `key = "919acc418a9a0821"`, `id = 30dcd7db2eecf398`; MUST NOT match
           `"password": "08ec1d7666c48b32"`, `token = "08ec1d7666c48b32"`, a bare dross:allow-secret
           16-hex with no id/key context, 15-hex, 17-hex, 18-hex, 32-hex, or
           `AKIAIOSFODNN7EXAMPLE`. Widening the class/length fails a NOT row;
           narrowing fails a match row.
         - TestGitleaksConfigNotPathExcluded [risk]: the written TOML has no `paths`
           key at any level — pins allowlist_scope so a later "helpful" path
           exclusion for .dross/, testdata/ or vendor/ is caught.
         - TestGitleaksConfigContained [risk+verification]: a runDir with `..` or a
           symlink out is refused by pathfence; on success the path is
           filepath.Join(runDir, GitleaksConfigName) and a second call succeeds
           with identical bytes.
         - TestGitleaksLiveAllowlist [verification, fixture corrected] (t.Skip unless
           gitleaks on PATH; never a silent pass): temp tree with tests.json holding
           `"key": "b91bfa24fdf586c0"` (entropy high enough to fire — 
           `50919a010c495368` does NOT) and leak.txt holding a concat-built
           `sk_test_…` stripe token (NOT `AKIAIOSFODNN7EXAMPLE` — gitleaks' default
           aws rule allowlists `.+EXAMPLE$`, verified exit 0). `gitleaks dir <tmp>
           --no-banner --exit-code 1 --config <emitted> --report-format json
           --report-path <out>` exits 1 with the stripe finding and NO finding on
           tests.json; the same command without `--config` DOES report tests.json
           (positive control that the fixture is flaggable). Exit 0 with config =
           rules disabled; an identity-id finding = regexTarget/regex wrong for the
           real tool.
         - TestGitleaksDrossTreeNoIdentityHits [verification] (gitleaks-gated,
           skipped under -short): `gitleaks git <repoRoot> --config <emitted>
           --report-format json` yields no finding whose Secret matches
           `^[0-9a-f]{16}$`. This is c-4's "zero hits on dross" leg, scoped to the
           locked shape (see Disagreement 2 for the one residual prose hit).

  t-4  Techdebt exclude-glob path filter                          [verification+risk]
       files:    internal/techdebt/filter.go, internal/techdebt/filter_test.go
       covers:   c-2 (mechanism half)
       depends:  —
       description:
         `Filter(repoDir string, paths, excludes []string) ([]string, error)`: each
         path is made repoDir-relative slash form; an entry ending in "/" is a
         directory prefix; any other entry is path.Match'd against the rel path and,
         when the pattern has no "/", also against the base name. path.ErrBadPattern
         is returned as `techdebt.exclude %q: %w` with a nil slice — never swallowed,
         never the unfiltered input. Pure function; no stack dependency (the skip set
         is applied by the enumerator in t-6 — see Disagreement 4).
       test_contract:
         - TestFilterTrailingSlashIsPrefix [risk+verification]: `internal/techdebt/`
           drops internal/techdebt/scan.go and internal/techdebt/deep/nested.go,
           keeps internal/techdebtx/a.go and internal/techdebt.go; handing the entry
           straight to path.Match keeps the first two and fails.
         - TestFilterGlobIsRepoRelative [risk+verification]: `*.golden` drops
           docs/a.golden and a.golden (base-name rule); `docs/*.md` drops docs/a.md
           but keeps docs/sub/a.md; `internal/*/scan.go` drops
           internal/techdebt/scan.go — matching against the absolute path (which
           starts with the tempdir) fails the last.
         - TestFilterBadPatternErrors [risk+verification]: `["[abc"]` returns an
           error whose text contains `[abc` and a nil slice.
         - TestFilterEmptyIsIdentity [verification]: nil excludes returns the input
           unchanged.

Wave 2
  t-5  Record exclusions in security manifest, detect, run, report   [verification+risk+mvp]
       files:    internal/security/recon.go, internal/security/recon_test.go,
                 internal/cmd/security.go, internal/cmd/security_test.go
       covers:   c-5, c-4 (delivery half)
       depends:  t-1, t-3
       description:
         Manifest (recon.go:16) gains `Exclusions{SkippedDirs []string; Allowlist
         string}` filled by BuildManifest from stack.SkipDirs() + GitleaksConfigName.
         `detect` prints an `exclusions:` block — `skipped directories: …` and
         `gitleaks allowlist: gitleaks.toml (written into the run dir by dross
         security run)` (detect creates no run dir and stays read-only). `run`
         calls WriteGitleaksConfig(runDir, m.Exclusions.SkippedDirs) before
         writeRunReport (security.go:193), overwrites Allowlist with the concrete
         path, prints `  gitleaks allowlist: <path>`, and report.md gains a
         `## Exclusions` section listing the skipped dirs and that path.
       test_contract:
         - TestManifestCarriesExclusions [risk+verification+mvp]:
           BuildManifest(...).Exclusions.SkippedDirs deep-equals stack.SkipDirs()
           (contains "testdata", "fixtures", ".dross"); a private copy in security
           fails — the RISK #4 drift pin.
         - TestSecurityDetectNamesExclusions [all three]: captureStdout of
           `security detect <tmp>` contains `exclusions:`, every name from
           stack.SkipDirs(), and `gitleaks.toml`.
         - TestSecurityRunWritesGitleaksConfig [risk+verification]: after `run .`,
           the sole run dir contains gitleaks.toml whose body has `useDefault =
           true`; stdout contains `gitleaks allowlist: ` + that path; AND report.md's
           `gitleaks allowlist:` line names a path that os.Stat succeeds on — a
           report naming a file that was never written fails the Stat.
         - TestSecurityRunReportRecordsExclusions [verification+mvp]: report.md
           contains `## Exclusions`, "testdata", "fixtures" and "gitleaks.toml".
         - Existing TestSecurityRunReadOnly (security_test.go:343) stays green
           unchanged — the new file lands inside .dross/security/<run>/.

  t-6  Wire skip set + excludes into dross techdebt; pin the self-scan   [verification+risk+mvp]
       files:    internal/cmd/techdebt.go, internal/cmd/techdebt_test.go,
                 internal/cmd/techdebt_selfscan_test.go, .dross/project.toml
       covers:   c-2, c-3
       depends:  t-1, t-2, t-4
       description:
         trackedFiles (techdebt.go:68) drops any path with a component for which
         stack.SkipDir is true, on BOTH the ls-files branch and the no-git walk
         (replacing the hand-rolled `.dross` prefix check and the `.git/.dross`
         switch — both names stay skipped via the set). The RunE loads project.toml
         via loadProject(), applies techdebt.Filter with Techdebt.Exclude, and
         surfaces a bad pattern as the command error before NewRun. dross's own
         .dross/project.toml gains `[techdebt]\n  exclude = ["internal/techdebt/"]`.
       test_contract:
         - TestTechdebtSelfScanExcludesOwnPackage [all three, with risk's bounds]
           (real repo; project.toml is tracked so the hermetic_dross_read guard is
           satisfied): trackedFiles(repoRoot) → Filter(excludes from
           .dross/project.toml) → Scan(DefaultThresholds) yields zero findings whose
           File is under internal/techdebt/. Negative control in the same test: the
           unfiltered set yields ≥1 marker there (13–16 today: markerRe at
           scan.go:47, the package doc, scan_test.go/run_test.go fixtures), so
           deleting the project.toml line or the Filter call fails naming the first
           re-admitted finding. Over-exclusion guard: `len(filtered) > 0`,
           `len(raw) - len(filtered) >= 6` (the package tracks 7 files) and
           `len(raw) - len(filtered) < len(raw)/2`; a match-all exclude fails the
           last bound instead of passing vacuously.
         - TestTrackedFilesSkipsFixtureDirs [risk+verification+mvp] (synthetic git
           repo, both branches as sub-tests): tracked fixtures/big.txt (700 lines),
           testdata/wide.txt (one 500-char line), `testdata-like/keep.go` and
           src/ok.go; trackedFiles returns only the last two; Scan over them yields
           0 findings while Scan over the raw four yields 1 oversized-file + 1
           long-line (proves the thresholds would have fired). Wiring the set into
           only one branch fails the other sub-test; a substring match drops
           `testdata-like/keep.go` and fails.
         - TestTechdebtAppliesProjectExclude [risk] (cmd, both branches): Init a
           tempdir, write `[techdebt] exclude = ["skipme/"]`, files skipme/a.go
           (FIXME) and keep.go (TODO); the report contains keep.go and not
           skipme/a.go under `git init && git add` and without.
         - TestTechdebtDrossTreeNoFixtureFindings [verification] (real repo): no
           finding whose File contains "/testdata/" or "/fixtures/" (69 tracked
           files under those names today).
         - TestTechdebtBadExcludeErrors [verification+risk]: `exclude = ["["]` makes
           `dross techdebt` return an error naming the entry and write no run dir.
         - Existing TestTrackedFilesExcludesDrossDir,
           TestTrackedFilesSegmentMatchIsNotSubstring and
           TestTechdebtEnumeratesTrackedFiles stay green unchanged.

Wave 3
  t-7  secure.md passes --config <run-dir>/gitleaks.toml            [verification+risk+mvp]
       files:    assets/prompts/secure.md, internal/cmd/secure_prompt_test.go
       covers:   c-4 (prompt half), c-5
       depends:  t-5
       description:
         In "## 2. Tooling sweep": the Detect step says `dross security detect` also
         names the exclusions in effect; the Sweep step adds a gitleaks bullet with
         the exemplar `gitleaks git --config <run-dir>/gitleaks.toml -- .`
         (flags before the fenced operand, matching the semgrep discipline already in
         that section), explaining that the run dir's gitleaks.toml extends the
         default rules, allowlists only dross identity ids, and path-excludes
         nothing (.dross stays scanned). Note r-01: `make install` before relying on
         it.
       test_contract:
         - TestSecurePromptMandatedSections (secure_prompt_test.go:29) gains case
           "c-4 gitleaks run-dir allowlist via --config" with needles `gitleaks`,
           `--config`, `gitleaks.toml`, `run-dir`, `allowlist` in one paragraph, and
           `--config` appearing before ` -- ` on that line (fence discipline) —
           removing the bullet or reordering the operand fails exactly this sub-test
           [all three, order check from risk].
         - Same test gains case "c-5 detect names exclusions" with needle
           `exclusions` in the Detect step [verification].

Post-execution (not a task, rule r-01): `make install` before /dross-secure or
/dross-techdebt is trusted to exercise the new prompt line and binary.

### Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2 (schema + validate), t-4 (filter), t-6 (wiring + dross entry + pin) |
| c-3 | t-1 (set), t-6 (enumerator applies it + pins) |
| c-4 | t-3 (emit + shape + live pins), t-5 (run writes it), t-7 (prompt passes it) |
| c-5 | t-5, t-7 |

Locked decisions honoured: one hardcoded skip set in internal/stack, no
project.toml override (t-1); `[techdebt] exclude` glob list, no annotation, no
hardcoded package path (t-2/t-4/t-6); gitleaks.toml written per run into the run
dir and passed via --config, nothing committed to the adopter repo (t-3/t-5/t-7);
allowlist is the identity-hash shape only, no path exclusion (t-3, pinned).

## Disagreements

1. **Does the gitleaks emitter turn the skip set into a `paths` allowlist?**
   mvp: yes — `paths` = shared set minus `.dross`, arguing `skip_dir_set` names the
   emitter as a consumer. risk and verification: no — the emitter records the set in
   the config's `description` only; the functional allowlist is shape-only.
   **Default: no `paths` key**, pinned by TestGitleaksConfigNotPathExcluded (t-3).
   Why it matters: `allowlist_scope` says "shape only" and its stated rationale (no
   blind spot where subprocess output lands) applies equally to fixtures/ and
   testdata/ — where fake credentials get committed — and vendor/ — where real ones
   hide. verification's probe shows the shape rule alone clears every one of the 241
   identity hits, so `paths` buys nothing and costs coverage. mvp itself said
   "if the judge reads *only* strictly, drop the `paths` line — nothing else
   changes"; that is what was done.

2. **c-4's literal "zero generic-api-key hits on dross".** verification probed the
   emitted config against the dross tree: dir-mode 241 → 4, git-mode 245 → 1. The
   git-mode residual is prose ("public key, ed25519-verify" at
   `.dross/phases/release-trust-and-distribution/spec.toml:21`, verified present),
   not the 16-hex shape, and cannot be cleared without widening the allowlist past
   the locked scope. risk and mvp pin the regex shape only and never confront the
   residual. **Default: pin c-4 as "zero identity-shape hits"
   (TestGitleaksDrossTreeNoIdentityHits, t-3) and do NOT widen the allowlist.** The
   lead should either dismiss the residual via `dross security findings` or reword
   that one spec line; the plan does not chase it. Why it matters: a plan that
   promises the literal criterion would either fail verify on a hit the locked
   decision forbids clearing, or tempt a widened regex that suppresses real secrets.
   The four dir-mode residuals are the same class and need the same call.

3. **`regexTarget = "line"` (mvp, verification) vs `"match"` (risk).** risk argues
   "match" is tighter: "line" could suppress a real secret sharing a line with an
   identity id. verification's "line" body is the one that actually ran and cleared
   the hits; "match" was never probed, and what text gitleaks feeds a "match"
   allowlist for the generic-api-key rule's capture group is exactly the thing
   Go-side tests cannot see. **Default: "line".** The live positive control in t-3
   (stripe token fires with the config in place) is the guard for risk's concern. If
   the lead prefers "match", t-3's live test must be re-probed before the value is
   committed — do not swap on theory.

4. **Where the skip set is applied for techdebt.** risk and mvp: inside the filter
   function (FilterPaths applies stack.SkipDir *and* the excludes; one policy
   function covers both enumeration branches). verification: in trackedFiles (the
   enumerator drops SkipDir components on both branches; Filter handles excludes
   only). **Default: verification's split.** Why: it keeps t-4 a pure wave-1 task
   with no stack import; the no-git walk can `SkipDir` at directory level instead of
   descending node_modules/ then dropping every file; it replaces two hand-rolled
   `.dross` checks with one predicate rather than adding a third; and the existing
   TestTrackedFilesExcludesDrossDir / TestTrackedFilesSegmentMatchIsNotSubstring
   stay meaningful. Accepted consequence (verification flagged it, the others did
   not): tracked `build/`, `dist/`, `.idea/`, `.vscode/` files also leave the
   tech-debt scan. dross tracks 0 such files today; an adopter with a committed
   dist/ loses it from techdebt, which the one-definition decision implies anyway.

5. **`dross validate` checks techdebt.exclude globs (risk only).** mvp and
   verification omit it. **Default: include, folded into t-2.** Why: `checkGlob`
   already exists at internal/cmd/validate.go:404 for lane patterns, the addition is
   one loop, and it turns a malformed entry into a validate problem before the scan
   aborts on it (t-6 still hard-errors at scan time so a hand-edit between validate
   and run cannot silently change the finding count). Costs one extra file pair in
   a wave-1 task; no criterion demands it, so the lead may drop it without touching
   any other task.

6. **Wave of the prompt task.** mvp: wave 1 (the filename is fixed by the locked
   decision; no code needed). risk: wave 2, depending only on t-3's constant.
   verification: wave 3, depending on t-5 because the prompt also describes what
   `detect` now prints (c-5 needle). **Default: wave 3 after t-5.** Why: the prompt
   should describe output that exists, not output planned; it is the smallest task
   in the phase and costs nothing to run last. If the lead wants c-4's prompt half
   landed earlier, drop the c-5 needle from t-7 and move it to wave 1 as mvp had it.

7. **Live gitleaks tests: include-with-skip (risk, verification) vs omit (mvp).** mvp
   rejects them as "a skipped test proves nothing in CI". risk and verification
   include them behind `exec.LookPath`. **Default: include (t-3), with
   verification's fixture.** Why: the config's `useDefault` line is the one thing in
   the phase that can fail silently and green; the pure TOML/regex tests remain the
   mandatory CI pins, and the live test is the extra proof on the author's box
   where gitleaks is installed. Fixture correction is load-bearing: risk's
   `AKIAIOSFODNN7EXAMPLE` is allowlisted by gitleaks' default aws rule (exit 0
   verified), so risk's test would report RISK #1 on a correct config.

Minor naming divergences (resolved, no design weight):
- `SkipDirs()` (mvp, verification) over `SkipDirNames()` (risk).
- `internal/security/gitleaks.go` (risk, mvp) over `allowlist.go` (verification).
- Manifest carries an `Exclusions{SkippedDirs, Allowlist}` struct (verification)
  rather than two flat fields (risk) or a bare `[]string` (mvp).
- Prompt exemplar uses `gitleaks git … -- .` (verification; probed, matches the
  section's fence discipline) over `gitleaks detect --no-git … --source` (risk);
  both forms run on 8.30.1.

Out of scope, flagged by verification for a follow-up quick: `dross options` /
options.md surfacing the new `[techdebt]` knob; README schema prose.
