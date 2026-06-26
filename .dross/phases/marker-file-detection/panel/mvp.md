Phase 09-marker-file-detection — 3 tasks across 2 waves

Wave 1
  t-1  Add data-driven marker pattern detection engine
       files:    internal/stack/profile.go
                 internal/stack/detect.go
       covers:   c-4, c-5
       contract: Add `Signals.FilePatterns []string` (toml `file_patterns`) to
                 profile.go; in detect.go add a case-insensitive glob matcher
                 (lowercase both sides, no brace expansion, filepath.Match
                 semantics) and a new exported `MarkerProfiles(root, profiles)
                 []string` that walks rootFilenames and returns ids of every
                 profile whose file_patterns match a root entry; also fold
                 pattern matching into scoreProfile so Detect() resolves a
                 pattern-only profile. contract: if the glob matcher loses
                 case-insensitivity, brace-expands, or matches a non-marker
                 file, a TestMarkerProfiles table case in detect_test.go fails
                 (Dockerfile / Dockerfile.dev / app.Dockerfile /
                 docker-compose-prod.yaml / compose.override.yml match; a
                 README.md / notes.yml does NOT match → over-broad guard). If
                 scoreProfile ignores file_patterns, the Detect()-resolves-
                 pattern-only-profile case fails.

  t-2  Add embedded docker stack profile
       files:    internal/stack/profiles/docker.toml
       covers:   c-1
       contract: New docker.toml with id="docker", `[signals].file_patterns`
                 covering Dockerfile, Dockerfile.*, *.dockerfile, *.Dockerfile,
                 docker-compose*.yml/.yaml, compose*.yml/.yaml, and tools:
                 hadolint (kind=scanner), hadolint (kind=analyzer), trivy config
                 (kind=scanner). contract: if file_patterns are wrong/missing or
                 the profile fails Validate, the embedded-profiles test
                 (Embedded() / TestEmbeddedDockerProfile) errors and a
                 detect_test.go case asserting Detect() returns "docker" on a
                 Dockerfile-only tree fails.

Wave 2 (depends t-1, t-2)
  t-3  Surface marker profiles additively in secure/quality manifests
       files:    internal/security/recon.go
                 internal/quality/recon.go
       covers:   c-2, c-3, c-5, c-6, c-7
       depends:  t-1, t-2
       contract: In both BuildManifest, after the language loop, call
                 stack.MarkerProfiles(root, ...) and add(ScannersFor(id)) /
                 add(AnalyzersFor(id)) for each matched marker profile id —
                 additive, de-duped via the existing `seen` set, languages list
                 unchanged. contract: if marker surfacing is dropped or made
                 non-additive, a recon_test.go case fails — a Dockerfile-only
                 repo's security manifest omits hadolint+trivy config (c-2), its
                 quality manifest omits hadolint analyzer (c-3); a Go+Dockerfile
                 repo loses either the Go tools or the Docker tools (c-6
                 additive); a synthetic pattern-only profile in a temp user
                 profile dir is not surfaced (c-5); and a marker-free Go repo
                 surfaces `docker` or changes its language/tool manifest (c-6
                 regression). c-7: a stack_test.go case asserting `dross stack
                 list` includes `docker` and `stack show docker` decodes cleanly
                 fails if docker.toml is malformed.

## Coverage
- c-1 → t-2
- c-2 → t-3
- c-3 → t-3
- c-4 → t-1
- c-5 → t-1 (generic engine, no docker hardcode), t-3 (synthetic-profile surfacing)
- c-6 → t-3 (regression + additive cases)
- c-7 → t-3 (stack list/show docker; existing cmd code, test-only)

## Judgment calls
- Merged the `FilePatterns` field, the glob matcher, `MarkerProfiles`, and the `Detect()` scoreProfile fold into ONE wave-1 task (t-1): they are one cohesive layer in one package, all under ~2 files, and splitting field-from-matcher-from-wiring would create sub-10-min fragments. Rejected a separate "add field" task as too-small.
- Merged both security and quality recon wiring into ONE task (t-3): it is the identical ~4-line additive edit in two mirror files with no order dependency between them; MVP merges rather than mirroring the package split into two near-duplicate tasks.
- Folded c-7 (`stack list`/`show docker`) into t-3 as a test-only addition rather than its own task: the cmd code already iterates LoadAll(), so docker.toml (t-2) is the only production change c-7 needs; a standalone task would be a sub-10-min test stub. Rejected giving it its own task.
- docker.toml (t-2) is data with no code dependency, so it sits in wave 1 alongside the engine rather than being dropped to wave 2; its detection/Validate tests run as part of t-2 but the cross-cutting Detect()-returns-docker assertion is exercised against t-1's engine (captured in t-1's and t-3's contracts). Rejected a separate "author profile + write detection test" split.
- Kept t-3 in wave 2 (strict dep on t-1's `MarkerProfiles` export and t-2's profile existing); no task was promoted to a later wave without a real cross-task output dependency.
- No Docker-specific code anywhere (locked: generic data-driven) — t-1 is profile-agnostic, the only Docker artifact is the .toml; this is what makes c-5's "future marker profile = new .toml only" true and is asserted by the synthetic-profile case in t-3.
