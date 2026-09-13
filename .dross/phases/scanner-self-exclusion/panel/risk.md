# Risk-lens plan — scanner-self-exclusion

Lens: every task exists because a specific thing can break. The failure modes
this phase can introduce, in order of blast radius:

1. **Silent scanner disablement** — a gitleaks `--config` file without
   `[extend] useDefault = true` replaces the default ruleset with *nothing*:
   zero rules, zero findings, green report. Worst outcome in the phase.
2. **Silent over-exclusion** — a bad exclude glob or a path-matching bug that
   matches everything hides real debt; a skip-set applied by substring hides
   `mytestdata/`. The scan still "runs" and reports fewer findings.
3. **Silent under-exclusion** — a glob compared against an absolute path, or a
   trailing-slash entry handed to `filepath.Match`, never matches; the entry in
   project.toml is decoration and c-2 fails only when someone looks.
4. **Drift between consumers** — detect prints one skip set, techdebt filters by
   another, gitleaks records a third. The locked `skip_dir_set` decision names
   this as the thing the milestone exists to prevent.
5. **Blind spots created by the fix** — path-excluding `.dross/`, `fixtures/`
   or `vendor/` from the *secret* scanner (fixtures are where fake creds are
   committed; vendor is where real ones hide).

Each risk below is owned by exactly one task and pinned by exactly one named
test contract.

---

Phase scanner-self-exclusion — 7 tasks across 3 waves

Wave 1
  t-1  Export shared skip set; add testdata, fixtures
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1
       depends:  —
       description:
         Add "testdata" and "fixtures" to skipDirs. Export `SkipDir(name string)
         bool` and `SkipDirNames() []string` (sorted) so techdebt and security
         consume the one definition instead of copying it. extsInTree,
         detectLanguagesFrom and MarkerProfiles keep calling the same map — no
         new walker logic. The `path != root` guard stays so a root literally
         named testdata/ is still walked.
       risks owned:
         - segment-not-substring: only a *directory entry* named exactly
           testdata/fixtures is skipped; a file `testdata.py` or a dir
           `mytestdata/` is not
         - MarkerProfiles honours the set (fixtures/ on dross holds Dockerfiles,
           terraform, k8s manifests that currently surface marker stacks)
         - repo-root truth: dross's own tree resolves to exactly ["go"]
       test_contract:
         - TestDetectLanguagesRepoRootIsGoOnly: HOME-isolated
           DetectLanguages(repoRoot(t)) must equal exactly []string{"go"}; if
           testdata/ or fixtures/ leaks back into any of the three walks the
           .ts/.js/.yaml fixtures under internal/mutation/testdata and
           fixtures/ surface typescript/javascript and the exact-equality
           fails.
         - TestSkipDirIsSegmentNotSubstring: tempdir with
           `mytestdata/a.py`, `testdata.py` (file) and `testdata/b.rb`;
           DetectLanguages must return python (from both non-skipped paths) and
           NOT ruby. A substring or prefix comparison fails the first
           assertion; a missing IsDir guard fails the second.
         - TestMarkerProfilesSkipsFixtures: tempdir with
           `fixtures/iac/Dockerfile` only → MarkerProfiles returns []; move the
           Dockerfile to root → returns ["docker"]. If MarkerProfiles is not
           on the shared set (or the set lacks fixtures) the first assertion
           fails.
         - TestSkipDirNamesIsSortedAndComplete: SkipDirNames() contains
           ".dross", "testdata", "fixtures", "node_modules", "vendor" and is
           sort.StringsAreSorted; a second, private copy of the list anywhere
           else in internal/ is caught by TestNoSecondSkipSet (grep of
           non-test .go sources for a map literal keyed "node_modules" outside
           internal/stack/detect.go).

  t-2  Add [techdebt] exclude schema + validate glob check
       files:    internal/project/project.go, internal/project/project_test.go,
                 internal/cmd/validate.go, internal/cmd/validate_test.go
       covers:   c-2 (schema half)
       depends:  —
       description:
         Add `Techdebt Techdebt `toml:"techdebt,omitempty"`` to Project with
         `Exclude []string `toml:"exclude,omitempty"``, mirroring Mutation.
         `dross validate` runs checkGlob over every techdebt.exclude entry (the
         existing lane-match loop pattern) and reports
         `project.toml: techdebt.exclude[i] %q does not compile: %v`.
       risks owned:
         - a malformed glob (`[abc`) must be a *validate* problem, not a silent
           never-matches entry
         - Save round-trip must not drop or reorder the section (hand-edited
           project.toml is re-saved by other commands)
         - absent section decodes to nil, not an error, on every existing
           project.toml
       test_contract:
         - TestProjectTechdebtExcludeRoundTrip: Load a body with
           `[techdebt]\n exclude = ["internal/techdebt/", "*.golden"]`, Save
           to a temp path, Load again → Exclude deep-equals the original
           slice in order. Dropping omitempty or mis-tagging the field fails
           the second Load's equality.
         - TestProjectNoTechdebtSectionIsNil: a body with no [techdebt] loads
           with `p.Techdebt.Exclude == nil` and no error.
         - TestValidateFlagsBadTechdebtGlob: project.toml with
           `exclude = ["[abc"]` → `dross validate` output contains
           `techdebt.exclude[0]` and `does not compile`; a well-formed
           `["internal/techdebt/"]` produces no techdebt problem line. If the
           loop is missing, the first assertion fails.

  t-3  Emit gitleaks allowlist config into a run dir
       files:    internal/security/gitleaks.go, internal/security/gitleaks_test.go
       covers:   c-4 (emit + shape-pin half)
       depends:  —
       description:
         `const GitleaksConfigName = "gitleaks.toml"`. `WriteGitleaksConfig(runDir
         string, skipped []string) (string, error)` writes, via
         pathfence.Contain + pathfence.WriteFile, a config whose body is:
         `[extend] useDefault = true`, then a single allowlist with
         `regexTarget = "match"` and one regex covering the identity-id shape
         — an `id`/`key` keyword, optional quote, `:`/`=`, optional quote, then
         exactly 16 lowercase hex, word-bounded. `skipped` is rendered into the
         allowlist `description` (what language detection + techdebt scoped
         out) so the run dir records it; it is NOT rendered as `paths` (see
         judgment calls). Regex is exported as `IdentityIDAllowlist` (a
         compiled *regexp.Regexp) so the test and the emitter cannot diverge.
       risks owned:
         - RISK #1: missing useDefault silently disables every rule
         - allowlist too wide: any 16-hex, or any hex length, or non-id/key
           context, clears real secrets (allowlist_scope is locked to
           "16-hex in id/key context")
         - allowlist too narrow: the two contexts that actually occur
           (`"key": "…"` JSON in tests.json, `key = "…"` TOML in
           survivors.toml) must both match
         - file must land inside the run dir and be overwritten idempotently on
           a re-run in the same dir (NewRun suffixes, but scaffold-style
           re-invocation must not error on exists)
       test_contract:
         - TestGitleaksConfigExtendsDefault: decode the written file with
           BurntSushi into a struct `{Extend struct{UseDefault bool}}` and
           require UseDefault == true; if the `[extend]` table is dropped or
           renamed, this fails — the test is the only thing standing between
           `--config` and a zero-rule scan.
         - TestIdentityIDAllowlistShape (table): the regex extracted from the
           written TOML (parsed, not the Go constant — proves what gitleaks
           will read) MUST match `"key": "08ec1d7666c48b32"`,
           `key = "5c88045d72401675"`, `"id": "928e3536acd36ece"`, and MUST
           NOT match `"password": "08ec1d7666c48b32"` (wrong keyword),
           `"key": "08ec1d7666c48b32aa"` (18 hex), `"key": "08ec1d7666c48b3"`
           (15 hex), `"key": "AKIAIOSFODNN7EXAMPLE"` (not hex),
           `"key": "08EC1D7666C48B32"` (uppercase — identity ids are
           lowercase; keep the rule tight). Any widening of the character class
           or the length fails a NOT-match row; any narrowing fails a match row.
         - TestGitleaksConfigNotPathExcluded: the written TOML has no `paths`
           key at any level — a path allowlist for .dross/, testdata/ or
           vendor/ would violate allowlist_scope and blind the secret scanner.
         - TestGitleaksConfigContained: runDir with a `..` component or a
           symlink pointing outside is refused by pathfence (reuses
           assertRunDirContainment's shape); on success the returned path is
           filepath.Join(runDir, GitleaksConfigName) and calling it twice
           succeeds with identical bytes.

Wave 2
  t-4  Filter techdebt paths: skip set + exclude globs
       files:    internal/techdebt/filter.go, internal/techdebt/filter_test.go,
                 internal/cmd/techdebt.go, internal/cmd/techdebt_test.go
       covers:   c-3, c-2 (mechanism half)
       depends:  t-1, t-2
       description:
         `techdebt.FilterPaths(repoDir string, paths []string, exclude []string)
         ([]string, error)`: for each path, compute repoDir-relative slash form;
         drop it if any directory segment satisfies stack.SkipDir; drop it if it
         matches an exclude entry — an entry ending in "/" is a directory
         prefix (`internal/techdebt/` drops everything beneath), otherwise
         path.Match against the relative path. A path.ErrBadPattern is returned
         as `techdebt.exclude %q: %w`, never swallowed. `dross techdebt` loads
         project.toml, passes p.Techdebt.Exclude, and applies FilterPaths on
         BOTH the ls-files path and the no-git fallback walk (trackedFiles keeps
         its own .dross filter and existing test).
       risks owned:
         - RISK #2/#3 both live here: over-exclusion (bad pattern treated as
           match-all, or segment test by substring) and under-exclusion (glob
           run against the absolute path, trailing-slash entry handed to
           path.Match)
         - a bad glob at scan time must abort the run with the pattern named
           (validate catches it earlier, but a hand-edit between validate and
           scan must not silently change the finding count either way)
         - the fallback walk must get the same filter or the two enumeration
           paths drift
       test_contract:
         - TestFilterPathsSkipSetBySegment: paths `a/testdata/x.go`,
           `fixtures/y.md`, `a/testdata.go`, `notfixtures/z.go`, `a/b/c.go`
           under repoDir → result is exactly the last three. A substring test
           drops `testdata.go`/`notfixtures/z.go` and fails; a root-only test
           keeps `a/testdata/x.go` and fails.
         - TestFilterPathsTrailingSlashIsPrefix: exclude
           `["internal/techdebt/"]` drops `internal/techdebt/scan.go` and
           `internal/techdebt/deep/nested.go` but keeps
           `internal/techdebtx/a.go` and `internal/techdebt.go`; passing the
           entry straight to path.Match keeps the first two and fails.
         - TestFilterPathsGlobIsRepoRelative: exclude `["*.golden"]` drops
           `internal/cmd/testdata.golden`-style top-level files only per
           path.Match semantics AND `["internal/*/scan.go"]` drops
           `internal/techdebt/scan.go`; matching against the absolute path
           (which starts with the tempdir) fails the second.
         - TestFilterPathsBadGlobErrors: exclude `["[abc"]` returns an error
           whose text contains `[abc`; and the returned slice is nil (not the
           unfiltered input, not empty) so a caller cannot accidentally scan
           anyway.
         - TestTechdebtAppliesProjectExclude (cmd): Init a tempdir, write
           `[techdebt] exclude = ["skipme/"]` into .dross/project.toml, files
           `skipme/a.go` (FIXME) and `keep.go` (TODO); report contains
           `keep.go` and not `skipme/a.go`. Repeat under `git init` +
           `git add` (ls-files branch) and without (fallback branch) — both
           sub-tests must pass; wiring the filter into only one branch fails
           the other.
         - TestTechdebtSkipsFixtureDirs (cmd): tracked `testdata/long.txt`
           with a 500-char line and `fixtures/big.txt` with 700 lines; the
           report has zero long-line and zero oversized-file findings, while a
           sibling `src/long.go` with the same line still reports one — pins
           c-3's "no long-line or oversized-file findings" without pinning
           over-exclusion.

  t-5  Record exclusions in security manifest; run writes gitleaks.toml
       files:    internal/security/recon.go, internal/security/recon_test.go,
                 internal/cmd/security.go, internal/cmd/security_test.go
       covers:   c-5, c-4 (delivery half)
       depends:  t-1, t-3
       description:
         Manifest gains `SkippedDirs []string` (from stack.SkipDirNames()) and
         `GitleaksConfig string`. BuildManifest fills SkippedDirs and sets
         GitleaksConfig to the convention
         `.dross/security/<run-id>/gitleaks.toml`; securityRun overwrites it
         with the concrete path returned by WriteGitleaksConfig(runDir,
         m.SkippedDirs) before writeRunReport. `detect` prints an
         `exclusions:` block (`skipped dirs: …` + `gitleaks allowlist: …`);
         writeRunReport adds a `## Exclusions` section with the same two lines.
       risks owned:
         - RISK #4 drift: detect and report.md print from the same Manifest
           fields; neither re-derives the list
         - the path report.md names must be the file that actually exists —
           a convention string in the report with no file behind it is exactly
           the "silently narrowing" c-5 forbids
         - TestSecurityRunReadOnly must keep passing: the new file is inside
           .dross/security/<run>/
       test_contract:
         - TestSecurityDetectNamesExclusions: `detect <tmp>` stdout contains
           `exclusions:`, every name from stack.SkipDirNames() (so "testdata"
           and "fixtures" specifically), and `gitleaks.toml`. Removing the
           block or hardcoding a stale list fails.
         - TestSecurityRunWritesGitleaksConfig: after `run .`, the sole run
           dir contains gitleaks.toml, AND report.md contains the exact
           absolute-or-run-relative path of that file (read report.md, extract
           the `gitleaks allowlist:` line, os.Stat it). A report line that
           names a path that was never written fails the Stat.
         - TestManifestSkippedDirsIsSharedSet: BuildManifest(...).SkippedDirs
           deep-equals stack.SkipDirNames(); a private copy in security fails.
         - TestSecurityRunReadOnly (existing, unchanged) still walks only
           .dross/security/ — a gitleaks.toml written at repo root or under
           .dross/ directly would fail it.

  t-6  Pass --config to gitleaks in secure prompt; live pin
       files:    assets/prompts/secure.md, internal/cmd/secure_prompt_test.go,
                 internal/security/gitleaks_live_test.go
       covers:   c-4 (prompt half)
       depends:  t-3
       description:
         In secure.md §2 step 3 (Sweep), add a gitleaks bullet: run
         `gitleaks detect --no-git --config <run-dir>/gitleaks.toml --source
         <path>` (flags before the operand, matching the semgrep fence
         discipline), stating the config extends the defaults and allowlists
         only dross identity ids. Add a `gitleaks_live_test.go` that skips
         unless `exec.LookPath("gitleaks")` succeeds, writes a fixture tree,
         and runs the real binary with the emitted config.
       risks owned:
         - the prompt is the only place gitleaks is invoked; an emitted file
           nobody passes is a no-op (argfence notes gitleaks is shell-driven
           from the prompt, not Go)
         - regex semantics: Go's regexp and gitleaks' regexp (Go too, but
           regexTarget selection and the generic-api-key capture group decide
           what text the allowlist sees). Only running the binary proves the
           16-hex hits actually clear and that real secrets still fire.
       test_contract:
         - TestSecurePromptPassesGitleaksConfig (cmd, always runs): normalised
           secure.md contains `gitleaks detect`, `--config`, `gitleaks.toml`
           and the string `run-dir` in one paragraph, and `--config` appears
           before `--source` in that line (fence discipline). Dropping the
           bullet or reordering the operand fails.
         - TestGitleaksLiveAllowlist (security, skips without the binary):
           fixture dir with `tests.json` holding `"key": "08ec1d7666c48b32"`
           and `leak.txt` holding a canonical fake AWS access key
           (`AKIAIOSFODNN7EXAMPLE` + a 40-char secret); `gitleaks detect
           --no-git --config <emitted> --source <dir> --report-format json
           --report-path <tmp>` must exit 1 with a report containing exactly
           the AWS finding(s) and no finding whose Match contains
           `08ec1d7666c48b32`. Exit 0 = the config disabled the default rules
           (RISK #1 caught end-to-end); an identity-id finding = the
           regexTarget/regex is wrong for the real tool.

Wave 3
  t-7  Exclude internal/techdebt/ in dross's project.toml; pin self-scan
       files:    .dross/project.toml, internal/cmd/techdebt_selfscan_test.go
       covers:   c-2
       depends:  t-4
       description:
         Add `[techdebt]\n  exclude = ["internal/techdebt/"]` to
         .dross/project.toml. New test: trackedFiles(repoRootFromTest(t)) →
         FilterPaths with the Exclude loaded from
         filepath.Join(root, ".dross", "project.toml") (tracked — satisfies
         the hermetic_dross_read guard) → Scan with DefaultThresholds → assert
         no finding whose File is under internal/techdebt/.
       risks owned:
         - RISK #2 at repo scale: the test also asserts the filtered path
           list is non-empty, still contains internal/techdebt's *sibling*
           packages (e.g. an `internal/security/` path), and that the raw
           tracked list DID contain internal/techdebt/scan.go — so an
           exclude that matches everything, or a scan that enumerates
           nothing, fails instead of vacuously passing
         - the entry lives in tracked config, so deleting it fails CI on a
           fresh checkout, not just on the author's box
       test_contract:
         - TestTechdebtSelfScanExcludesOwnPackage: over the real tracked tree
           with the real project.toml exclude, zero findings have
           `strings.HasPrefix(rel(File), "internal/techdebt/")`; removing the
           project.toml entry re-admits the markerRe line, the package doc's
           "TODO/FIXME/HACK/XXX", and scan_test.go's fixtures — at least 16
           marker findings — and the test fails naming the first one.
         - Same test, over-exclusion guard: `len(filtered) > 0`,
           `len(raw) - len(filtered) >= 6` (the package's files) and
           `len(raw) - len(filtered) < len(raw)/2`; a match-all exclude fails
           the last bound.

Post-execution (not a task, rule r-01): `make install` before /dross-secure or
/dross-techdebt is trusted to exercise the new prompt line and binary.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2 (schema), t-4 (filter mechanism), t-7 (dross entry + pin) |
| c-3 | t-4 |
| c-4 | t-3 (emit + shape pin), t-5 (run writes it), t-6 (prompt passes it + live pin) |
| c-5 | t-5 |

All 5 criteria covered. Locked decisions honoured: skip set stays a single
hardcoded map in internal/stack (t-1), no project.toml override for it; techdebt
exemption is a `[techdebt] exclude` glob list (t-2/t-4/t-7), no annotation, no
hardcoded package path; gitleaks.toml is written per run into the run dir and
passed via --config (t-3/t-5/t-6), nothing committed to the adopter repo; the
allowlist is the identity-hash shape only, no path exclusion (t-3).

## Judgment calls

- **Gitleaks emitter "consumes" the skip set as a recorded description, not as
  `paths`.** skip_dir_set says the emitter consumes the shared definition;
  allowlist_scope says the allowlist covers the shape *only* and .dross/ is
  not path-excluded. The only reading that satisfies both is: the emitter
  takes the set and writes it into the config's description so the run dir
  records what was scoped out (c-5), while the functional allowlist stays
  shape-only. Rejected: emitting `paths = [testdata, fixtures, vendor, …]` —
  fixtures/ is where fake credentials get committed and vendor/ is where real
  ones hide; a secret-scanner blind spot is the exact failure allowlist_scope
  names. TestGitleaksConfigNotPathExcluded pins this so a later "helpful"
  addition is caught.
- **regexTarget = "match", not "secret" or "line".** "secret" would see only
  the captured 16-hex and could not enforce the id/key context (locked scope).
  "line" would suppress a real secret sharing a line with an identity id.
  "match" sees the keyword + value gitleaks' generic-api-key rule matched,
  which is exactly the context the decision describes. The live test (t-6) is
  what proves this against the real binary, since Go-side regex tests cannot
  see what gitleaks feeds the allowlist.
- **`[extend] useDefault = true` gets its own test and its own risk line.**
  It is one TOML line, but its absence produces a green audit with zero rules.
  Nothing else in the phase can fail as quietly.
- **Bad exclude glob aborts the scan with an error; not warn-and-continue.**
  Both silent alternatives change the finding count: treating ErrBadPattern as
  "no match" re-admits the excluded files, treating it as "match" hides
  everything after it. `dross validate` also reports it (t-2) so the normal
  flow catches it before a run.
- **Trailing-slash = directory prefix, and `**` is not supported.** path.Match
  has no recursive glob; documenting `dir/` as the recursive form (and pinning
  it) beats adding a dependency or hand-rolling `**`. A `**` entry is a
  compiling pattern that matches nothing — that is the under-exclusion risk,
  so the plan pins the supported form rather than pretending `**` works.
- **Filter lives in internal/techdebt, not cmd.** trackedFiles is enumeration;
  FilterPaths is policy and must be identical on both the ls-files and
  fallback branches. Putting it in the package makes it a pure function
  testable without git, and TestTechdebtAppliesProjectExclude drives both
  branches through the cmd to prove the wiring.
- **Self-scan test (t-7) asserts over-exclusion bounds, not just "zero under
  internal/techdebt/".** A zero-findings assertion is vacuously true when the
  scan enumerates nothing or the exclude matches everything. The bounds
  (`raw - filtered` between the package's file count and half the tree) make
  the test fail in both directions.
- **Live gitleaks test skips without the binary rather than being omitted.**
  Skipped in CI is a known weakness (the hermetic guard's own history says
  so), which is why the mandatory pins are the pure TOML/regex tests in t-3 and
  the prompt test in t-6; the live test is the extra proof against the real
  tool on the author's box, where gitleaks is installed.
- **t-6 is wave 2, not wave 3.** It depends only on t-3's file-name constant
  and regex; nothing in the prompt needs t-5's manifest wiring.
