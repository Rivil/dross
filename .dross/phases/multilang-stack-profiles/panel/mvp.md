# MVP lens — task decomposition

Bias: smallest task set that satisfies c-1/c-2/c-3. Profiles are data, grouped by the
locked full-vs-stub distinction (2 tasks, not 10). One mechanism task does the c-2/c-3
single-sourcing. Tests ride inside their owning task — no standalone test tasks.

```
Phase multilang-stack-profiles — 3 tasks across 2 waves

Wave 1
  t-1  Author 3 full non-Go profiles
       files:    internal/stack/profiles/python.toml
                 internal/stack/profiles/javascript.toml
                 internal/stack/profiles/csharp.toml
       covers:   c-1
       contract: With python.toml present, Detect on a .py-only tree resolves
                 "python" and AnalyzersFor("python") includes its dedicated
                 [[tools]] analyzer; delete python.toml's analyzer [[tools]] block
                 and the c-1 "analyzer shows in a quality run" assertion
                 (BuildManifest over a .py tree lists the python analyzer) fails;
                 drop ".py" from [signals].exts and Detect stops resolving "python".

  t-2  Author 7 detection-only stub profiles
       files:    internal/stack/profiles/ruby.toml
                 internal/stack/profiles/rust.toml
                 internal/stack/profiles/java.toml
                 internal/stack/profiles/c.toml
                 internal/stack/profiles/cpp.toml
                 internal/stack/profiles/php.toml
                 internal/stack/profiles/swift.toml
       covers:   c-2
       contract: Each stub carries only id+title+[signals].exts — a stub with no
                 [loadout]/[runtime]/[[tools]] still passes Decode/Validate (drop
                 the title and Validate is unaffected; a malformed-empty stub would
                 fail Embedded()). Each stub's exts back one row of the residual
                 set: remove ".swift" from swift.toml and the t-3 residual-coverage
                 regression loses its "swift" row and fails.

Wave 2 (depends t-1, t-2)
  t-3  Single-source ext→lang from profiles; delete extLang map
       files:    internal/stack/detect.go
                 internal/stack/detect_test.go
       covers:   c-2, c-3
       contract: Delete the hardcoded extLang map and derive ext→lang wholly from
                 loaded profiles' [signals].exts (key = profile id; higher
                 Signals.priority wins, ties lexicographic by id), via an
                 unexported parameterized core detectLanguagesFrom(root, profiles)
                 that DetectLanguages(root) wraps with LoadAll(). Specific breaks:
                 (a) regression — a table asserting every previously-mapped
                 language (go, python, javascript, typescript, ruby, rust, java,
                 kotlin, dart, svelte, sql, c, cpp, csharp, php, swift) is still
                 returned by DetectLanguages on a matching tree; drop any profile's
                 ext and that row fails. (b) clash tie-break — two in-memory
                 profiles sharing ".x" at equal priority resolve to the
                 lexicographically-smaller id; swap their priorities and the
                 higher-priority id wins. (c) zero-code-change proof — a brand-new
                 profile (id "zig", exts [".zig"]) dropped into a temp user dir and
                 loaded via loadAllFrom makes detectLanguagesFrom return "zig" and
                 ByID surface its analyzer tool, with no edit to detect.go.
                 (d) source guard — `extLang = map[string]string` no longer appears
                 in detect.go.
       depends:  t-1, t-2
```

## Coverage

- c-1 → t-1 (python full profile: Detect-resolved + analyzer in a quality run; needs
  no Go change because Detect/Catalog/AnalyzersFor are already profile-driven).
- c-2 → t-2 (residual stub coverage so no language regresses), t-3 (delete extLang,
  derive ext→lang from profiles, no-regression table).
- c-3 → t-3 (parameterized core + brand-new "zig" toml proven detectable and
  recon-visible via loadAllFrom with zero detect.go edit; t-1/t-2 demonstrate the
  add-a-profile-is-pure-data thesis).

All three criteria accounted for.

## Judgment calls

- Grouped the 10 tomls into 2 tasks (3 full / 7 stub) — chose split-by-kind / rejected
  10-per-toml tasks and rejected 1 mega-task / why: full vs stub is the locked
  full_profile_set distinction with distinct contracts (c-1 analyzer-in-run vs c-2
  residual coverage), and 10 files in one task violates the 5+-file split rule.
- Kept DetectLanguages(root) signature; load profiles internally via LoadAll() —
  chose internal load / rejected threading profiles through to security & quality
  recon.go callers / why: the signature ripple buys nothing for the criteria and an
  unexported detectLanguagesFrom(root, profiles) core already gives tests their seam.
- No standalone test tasks — chose tests-ride-inside-owning-task / rejected a
  dedicated c-3 verification task / why: each task already carries a specific
  contract; a separate test task would add a wave-3 dependency for zero new coverage.
- t-3 is wave 2, not wave 1 — chose depends-on-t-1/t-2 / rejected parallel deletion /
  why: deleting extLang before the profiles exist regresses coverage, and t-3's
  no-regression table needs the profiles present to pass.
- c-3 recon-visibility proven in-package via loadAllFrom(tempUserDir) — chose the
  existing unexported seam / rejected adding an env-var or injectable-profiles
  refactor to quality/security recon / why: MVP avoids new public surface; the loaded
  profile's analyzer tool is exactly the data recon consumes, so asserting on it
  proves recon-visibility without a mechanism change.
