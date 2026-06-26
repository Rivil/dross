Phase 07-stack-profiles — 6 tasks across 3 waves

Wave 1
  t-1  Build stack profile package + Go profile
       files:    internal/stack/profile.go, internal/stack/profile_test.go,
                 internal/stack/profiles/go.toml, internal/stack/embed.go
       covers:   c-2, c-3, c-5 (schema + load/merge mechanism)
       description: New internal/stack/ package. Declarative Profile schema
                 (id, detect signals, runtime command slots with multiple
                 commands/slot + package-manager variants, scanner+analyzer
                 tool loadout with optional/per-OS names, agent loadout:
                 mcp tools/guardrails/conventions). go:embed profiles/*.toml;
                 Load() merges embedded with ~/.claude/dross/profiles/*.toml,
                 user dir winning. Ship go.toml as the one real profile.
       contract: if the embedded/userdir merge regresses, a test placing a
                 go.toml override under a temp HOME and asserting the user
                 field wins over the embedded value fails; if the schema drops
                 multi-command-per-slot or pm-variants, decoding go.toml into
                 Profile fails to round-trip those fields.

  t-2  Centralize stack detection over signals
       files:    internal/stack/detect.go, internal/stack/detect_test.go
       covers:   c-1, c-5
       description: Detect(root) walks signal files (go.mod etc., reusing the
                 ext-based DetectLanguages already in recon.go via a shared
                 call) and returns the matched profile id or an explicit
                 Unsupported result. Matches "go" for this repo; a package.json-
                 only fixture returns unsupported, never Go.
       contract: if detection falls back to assuming Go, the test running
                 Detect on a package.json-only temp fixture (no go.mod) fails
                 because it expects Unsupported, not "go"; a go.mod fixture
                 asserting id=="go" guards the positive path; adding a second
                 fixture profile and asserting Detect selects it proves c-5
                 (selection by data, no mechanism change).

Wave 2 (depends t-1, t-2)
  t-3  Source scanner+analyzer catalogs from profile
       files:    internal/security/catalog.go, internal/security/catalog_test.go,
                 internal/quality/catalog.go, internal/quality/catalog_test.go
       covers:   c-3
       description: Replace the inline Go-language scanner/analyzer entries in
                 both catalogs with reads of the matched stack profile's tool
                 loadout (agnostic tools stay inline). ScannersFor/AnalyzersFor
                 keep their signatures; their Go-dedicated rows now come from
                 the profile.
       depends_on: t-1
       contract: if the Go loadout stops feeding the catalog, ScannersFor("go")
                 no longer contains govulncheck/gosec (sourced from go.toml) and
                 the security catalog test fails; same for AnalyzersFor("go")
                 missing gocyclo in the quality catalog test.

  t-4  Seed runtime commands from profile in init/onboard
       files:    internal/cmd/init.go, internal/cmd/onboard.go,
                 internal/cmd/onboard_test.go
       covers:   c-2
       description: When detection matches a profile, populate project.toml
                 [runtime] (test/typecheck/format/build) from the profile's
                 command slots instead of the hardcoded guesses in toProject().
                 User owns the values after write.
       depends_on: t-1, t-2
       contract: if seeding reverts to hardcoded guesses, an onboard test on a
                 go.mod fixture asserting Runtime.TestCommand=="go test ./..."
                 (from go.toml, not an invented string) fails; a non-Go /
                 unsupported fixture leaves the runtime command slots empty
                 rather than Go defaults.

  t-5  Add `dross stack` command (detect/show/list/apply/loadout)
       files:    internal/cmd/stack.go, internal/cmd/stack_test.go,
                 cmd/dross/main.go
       covers:   c-1, c-4
       description: cmd.Stack() parent with subcommands detect, show, list,
                 apply, loadout; registered in main.go AddCommand list. detect
                 prints matched id or unsupported; apply write-through re-syncs
                 [runtime] + stores [stack].profile; loadout emits the agent
                 loadout (mcp tools, guardrails, locked conventions) from
                 stack.locked as a markdown block to stdout.
       depends_on: t-1, t-2
       contract: if loadout stops deriving from stack.locked, the stack_test
                 asserting the emitted markdown contains a guardrail/convention
                 line from the Go profile fails; if detect is wired wrong, the
                 subcommand test on a package.json-only dir printing "unsupported"
                 (not "go") fails.

Wave 3 (depends t-5)
  t-6  Wire loadout block into a prompt + document adding a stack
       files:    assets/prompts/execute.md, internal/stack/profiles/README.md
       covers:   c-4, c-5
       description: Reference `dross stack loadout` as the inline-injectable
                 block in the execute prompt (per the markdown-block decision).
                 Add a short README in profiles/ documenting that a new stack is
                 a single go.toml-style TOML drop-in with zero code change
                 (the c-5 documentation requirement).
       depends_on: t-5
       contract: if the prompt no longer injects the loadout, a grep-based test
                 (or doc-presence check) asserting `dross stack loadout` appears
                 in execute.md fails; the README must state the zero-code drop-in
                 path or the c-5 documentation criterion is unmet. (Per r-01,
                 `make install` is required before the prompt edit is live.)

## Coverage
- c-1 (detection, Go-or-unsupported): t-2, t-5
- c-2 (Go profile is single runtime-command source; init/onboard seed it): t-1, t-4
- c-3 (profile declares scanner+analyzer loadout; catalogs consume it): t-1, t-3
- c-4 (`dross stack loadout` emits markdown block from stack.locked; prompt injects): t-5, t-6
- c-5 (new stack = one declarative entry, documented, proven by a 2nd non-Go fixture profile selected by detection): t-1, t-2, t-6

## Judgment calls
- Merged schema + Go profile + embed/merge into one wave-1 task (t-1): the schema is meaningless without a real profile to validate it and the embed/userdir merge is the locked profile_home contract; splitting would create a sub-10-min stub task. Kept under the 5-file cap.
- Reused existing DetectLanguages rather than adding a third copy (t-2): spec/c-1 + the layout note both demand centralize-not-duplicate; t-2 calls the existing ext walk instead of inventing new signal-walking scaffolding.
- Did NOT de-duplicate the two recon.go DetectLanguages copies as its own task: no criterion requires it; the MVP lens drops work not traceable to a criterion. t-2 reuses one; full de-dup is out of scope.
- Folded `dross stack` detect/show/list/apply/loadout into ONE command task (t-5) instead of one-per-subcommand: they share the parent constructor and one test file; per-subcommand tasks would each be <10 min and over-fragment.
- Catalogs (t-3) and runtime seeding (t-4) split because they are different layers (tool tables vs config-writing commands) with no shared edit; both depend only on t-1's profile, so both sit in wave 2 for parallelism.
- loadout markdown lives in the command (t-5), prompt wiring deferred to wave-3 t-6: only the prompt edit needs the command to exist first; everything else in t-5 is independent of prompts. Chose to wire one prompt (execute.md) rather than all, since c-4 only needs to prove the block is injectable.
- No new `internal/stack` vs `internal/profile` reuse: honored the locked name-collision warning — stack profiles are a fresh package, the behavioural profile is untouched.
