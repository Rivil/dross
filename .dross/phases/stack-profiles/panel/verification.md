# Verification-first decomposition — 07-stack-profiles

Lens: every task exists to make a *named* test pass. Each criterion's ideal test
contract was written first; the task is the smallest change that makes that
contract satisfiable. The test_contract is the deliverable; the code is downstream.

## Test contracts (derived first, before tasks)

- **c-1** — `internal/stack/detect_test.go`
  - `TestDetect_GoRepoMatchesGo`: detect on this repo's root (go.mod present) returns profile id `go`.
  - `TestDetect_UnsupportedFixtureNotGo`: detect on a fixture dir containing only `package.json` (no profile matches) returns `("", unsupported)` — NOT `go`, NOT a panic.
  - `TestDetect_UsesSharedLanguageScan`: detect calls the single shared `DetectLanguages`; deleting the security/quality copies and pointing them here keeps `TestBuildManifest` green (no third copy).
- **c-2** — `internal/stack/runtime_test.go` + `internal/cmd/init_test.go`
  - `TestRuntimeFromProfile_Go`: the Go profile yields exactly `test=go test -count=1 ./...`, `typecheck=go vet ./...`, `format=gofmt -l .`, `build=make build` (the four slots seeded into `[runtime]`).
  - `TestInitSeedsRuntimeFromProfile`: after init on a Go repo, `project.toml [runtime]` test/typecheck/format/build equal the profile's, not hardcoded literals — change a profile command and the seeded value changes.
- **c-3** — `internal/security/catalog_test.go` + `internal/quality/catalog_test.go`
  - `TestScannersForGo_FromProfile`: `ScannersFor("go")` returns the scanner set declared in the Go profile's `scanners` block; renaming a tool in the profile changes this output (proves no inline map remains).
  - `TestAnalyzersForGo_FromProfile`: same for `AnalyzersFor("go")` against the profile's `analyzers` block; `TestCatalogExcludesCosmetic` still passes (profile data routed through the existing guard).
- **c-4** — `internal/stack/loadout_test.go` + `internal/cmd/stack_test.go`
  - `TestLoadout_RendersLockedConventions`: rendering the Go profile's loadout emits a markdown block containing its declared MCP tools, guardrails, and locked conventions (assert specific headings + one known line).
  - `TestStackLoadoutCmd_EmitsMarkdownBlock`: `dross stack loadout` writes that markdown to stdout (block delimiters present) and exits 0 for the Go repo.
- **c-5** — `internal/stack/extensibility_test.go`
  - `TestSecondProfileLoadsWithoutCodeChange`: dropping a second TOML profile (a non-Go fixture, e.g. `node`) into the loader's user-dir search path makes it loadable and selectable by detection on a matching fixture — with zero edits to any `.go` mechanism file in the test.
  - `TestUserDirOverridesEmbedded`: a user-dir profile with id `go` shadows the embedded `go` profile (user wins on merge).

---

## Phase 07-stack-profiles — 8 tasks across 3 waves

### Wave 1

```
t-1  Define stack profile schema + embedded Go profile
     files:    internal/stack/profile.go
               internal/stack/profiles/go.toml
               internal/stack/profile_test.go
     covers:   c-2, c-3, c-4 (schema only)
     contract: TestLoadGoProfile decodes profiles/go.toml into a Profile with
               non-empty runtime{test,typecheck,format,build}, scanners[], analyzers[],
               and loadout{mcp_tools,guardrails,conventions}; if the schema drops any
               of those slots or go.toml omits one, the decode/assert fails. The schema
               carries package-manager variants, multi-command slots, optional/gated
               tools, and per-OS tool names (locked schema_extensibility): a fixture
               using each shape round-trips, and removing a struct field fails its sub-assert.
```

```
t-2  Centralize language detection into shared scan
     files:    internal/stack/detect.go
               internal/stack/detect_test.go
     covers:   c-1 (shared-scan half)
     contract: TestDetect_GoRepoMatchesGo returns id "go" on a go.mod fixture;
               TestDetect_UnsupportedFixtureNotGo returns ("", unsupported) on a
               package.json-only fixture (asserts it is NOT "go" and does not panic).
               The walker/extension logic is the one canonical copy — a table test of
               the same extension cases security/quality used passes here.
```

### Wave 2 (depends t-1, t-2)

```
t-3  Embedded+userdir profile loader with merge
     files:    internal/stack/loader.go
               internal/stack/loader_test.go
     covers:   c-5 (loader half), profile_home (locked)
     contract: TestUserDirOverridesEmbedded — a user-dir go.toml shadows the embedded
               go profile (user field wins). TestSecondProfileLoadsWithoutCodeChange —
               a node.toml dropped only into the user-dir search path is loadable and
               carries its own detect signals; no .go file is edited in the test. If the
               merge prefers embedded, or the loader ignores the user dir, both fail.
     depends_on: t-1
```

```
t-4  Wire detection selection to loaded profiles
     files:    internal/stack/select.go
               internal/stack/select_test.go
     covers:   c-1, c-5 (selection half)
     contract: TestSelect_GoRepo picks profile "go" from the loaded set on a go.mod
               fixture. TestSelect_SecondProfileSelected — with the node fixture profile
               loaded, a node-signature fixture selects "node", proving detection keys
               off declared profile signals, not a hardcoded language switch. Breaking
               the signal-match wiring fails the second-profile case specifically.
     depends_on: t-1, t-2, t-3
```

```
t-5  Route runtime commands from the Go profile
     files:    internal/stack/runtime.go
               internal/stack/runtime_test.go
     covers:   c-2 (derivation half)
     contract: TestRuntimeFromProfile_Go maps the Go profile's runtime block to the
               four project.toml [runtime] slots (test/typecheck/format/build) exactly;
               editing a command in go.toml changes the derived value. A package-manager
               variant fixture resolves the correct command per locked schema_extensibility.
     depends_on: t-1
```

```
t-6  Source scanner+analyzer catalogs from profile
     files:    internal/security/catalog.go
               internal/security/catalog_test.go
               internal/quality/catalog.go
               internal/quality/catalog_test.go
     covers:   c-3
     contract: TestScannersForGo_FromProfile — ScannersFor("go") equals the Go
               profile's scanners block; renaming a scanner in go.toml changes the result
               (proves the inline `var catalog` map is gone for the Go entries).
               TestAnalyzersForGo_FromProfile — same for AnalyzersFor("go").
               TestCatalogExcludesCosmetic still passes (profile data flows through the
               existing cosmetic guard). Agnostic tools (gitleaks/scc/...) stay available.
     depends_on: t-1
```

```
t-7  Render agent loadout markdown from profile
     files:    internal/stack/loadout.go
               internal/stack/loadout_test.go
     covers:   c-4 (renderer half)
     contract: TestLoadout_RendersLockedConventions — rendering the Go profile emits a
               markdown block with the declared MCP tools, guardrails, and locked
               conventions (assert specific headings + one known declared line). Dropping
               a loadout field from go.toml drops its line from the output, failing the
               assert. Empty loadout renders an explicit "(none)" rather than a blank.
     depends_on: t-1
```

### Wave 3 (depends wave 2)

```
t-8  Add `dross stack` command + init/onboard apply
     files:    internal/cmd/stack.go
               internal/cmd/stack_test.go
               internal/cmd/main.go (registration via cmd.Stack())
               internal/cmd/init.go
               internal/cmd/onboard.go
               internal/cmd/init_test.go
     covers:   c-1, c-2, c-4 (surface), config_integration + command_surface (locked)
     contract: TestStackCmd_HasSubcommands — `stack` exposes detect/show/list/apply/
               loadout. TestStackDetectCmd_Go prints id "go"; on a non-Go fixture prints
               unsupported, not go. TestStackLoadoutCmd_EmitsMarkdownBlock — `stack
               loadout` writes the markdown block to stdout, exit 0. TestInitSeedsRuntime
               FromProfile — init writes [runtime] test/typecheck/format/build from the
               profile and stores the matched id in [stack].profile; `stack apply`
               re-syncs [runtime] on demand (changing the profile then apply updates the
               file). Removing the AddCommand wiring fails TestStackCmd_HasSubcommands.
     depends_on: t-1, t-4, t-5, t-7
```

> Note (rule r-01): t-7/t-8 produce the `dross stack loadout` block consumed by
> `assets/prompts/*`; prompt-side injection edits are NOT in scope here (no criterion
> requires editing the prompt text), and any such edit would need `make install` to go
> live. Tasks are gated on Go tests only — all contracts above run under
> `go test -count=1 ./...` with no prompt dependency.

---

## Coverage

| criterion | tasks |
|-----------|-------|
| c-1 (detection, unsupported-not-go, shared scan) | t-2, t-4, t-8 |
| c-2 (Go profile is runtime source; init/onboard seed) | t-1, t-5, t-8 |
| c-3 (profile drives scanner/analyzer catalogs) | t-1, t-6 |
| c-4 (`stack loadout` markdown from stack.locked) | t-1, t-7, t-8 |
| c-5 (new stack = one TOML, no mechanism code; proven by 2nd profile) | t-1, t-3, t-4 |

Every criterion c-1..c-5 has at least one task whose test_contract names the exact
surface that breaks it. Locked decisions are each pinned to a contract: profile_home →
t-3 (TestUserDirOverridesEmbedded), schema_extensibility → t-1 (variant/gated/per-OS
fixtures), config_integration + command_surface → t-8 (apply re-sync, subcommand set,
[stack].profile), agent_loadout_shape → t-7 (markdown block, no agent .md files),
unify_operational_and_loadout → one Profile struct (t-1) supplies both runtime/catalogs
and loadout, scope_go_first → only go.toml ships embedded; node.toml exists only as a
test fixture (t-3/t-4).

## Judgment calls

- Split detection into t-2 (shared scan / unsupported result) and t-4 (profile
  selection): chose two contracts over one because c-1's "matches Go" and "unsupported,
  not Go" are a different test surface than c-5's "second profile is selectable" — one
  fat task would hide which behaviour regressed. Rejected: a single `detect.go` task.
- Centralize `DetectLanguages` into `internal/stack` and have security/quality call it,
  rather than leaving the two copies and adding a third. Chose the de-dup the prompt
  flagged; rejected a stack-local third copy because c-1's contract explicitly tests
  "no third copy" (TestDetect_UsesSharedLanguageScan). This makes t-6 touch the catalog
  files anyway, so the import swap lands there cheaply.
- Profiles as TOML under `internal/stack/profiles/*.toml` via go:embed (no existing
  embed in repo, but BurntSushi/toml v1.4.0 is present to decode). Chose embed+userdir
  merge per locked profile_home; rejected a Go-literal profile table because c-5's
  contract requires a *new TOML file* to be loadable with zero `.go` edits — a literal
  table cannot satisfy TestSecondProfileLoadsWithoutCodeChange.
- New `internal/stack/` package, not extending `internal/profile/`. Forced by the
  documented name collision; rejected reuse because the behavioural-profile tests would
  entangle and command_surface locks a separate `dross stack` namespace.
- `node.toml` ships only as a *test fixture*, never embedded. Chose this to honor
  scope_go_first (Go-only ships) while still proving extensibility; rejected embedding a
  real node profile because the milestone non-goal forbids shipping a second stack.
- Catalog rewire (t-6) covers both security and quality in one task (4 files, 1 layer).
  Chose to keep them together because they share the identical profile-sourcing change
  and the granularity rule's 5-file cap bends for a single-layer mechanical mirror;
  rejected splitting into two near-identical tasks that would duplicate the contract.
