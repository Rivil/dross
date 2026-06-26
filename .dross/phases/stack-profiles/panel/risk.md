# Risk-lens decomposition — 07-stack-profiles

Lens: **failure modes drive the graph.** Each task owns exactly one class of
breakage and carries the test that fails when that breakage returns. The riskiest
seams in this phase are: (a) the embedded-vs-userdir profile merge, (b) detection
misfiring on a polyglot or non-Go fixture (false-Go), (c) a profile referencing a
tool/command that isn't installed, (d) the two duplicated `DetectLanguages`
copies silently diverging from the new central detector, and (e) catalogs read
*live* from the profile drifting from what dross-secure / dross-quality actually run.

---

Phase 07-stack-profiles — 9 tasks across 3 waves

Wave 1

  t-1  Define stack profile schema + loader
       files:    internal/stack/profile.go
                 internal/stack/profile_test.go
       covers:   c-3, c-5
       contract: Parsing a profile TOML with a package-manager variant, two
                 commands in one runtime slot, and an availability-gated +
                 per-OS tool name round-trips into the struct; if the schema
                 drops any of those shapes (multi-command slot, optional/gated
                 tool, per-OS name), TestProfileSchemaShapes fails. A profile
                 with an empty `id` is rejected with an error, not loaded as a
                 nameless profile — TestProfileRejectsEmptyID.

  t-2  Centralize language detection in stack pkg
       files:    internal/stack/detect.go
                 internal/stack/detect_test.go
       covers:   c-1
       contract: Detection on a fixture containing ONLY package.json (no go.mod,
                 no .go files) returns an explicit "unsupported stack" result —
                 NOT "go" and NOT empty — so TestDetectNonGoFixtureUnsupported
                 fails if the detector defaults to Go. On a polyglot fixture
                 (go.mod + a stray .py) the Go profile still matches via the
                 go.mod signal and TestDetectPolyglotPrefersSignal fails if a
                 lone foreign source file flips the result to unsupported.

Wave 2 (depends t-1)

  t-3  Embed Go profile + merge user-dir profiles
       files:    internal/stack/embed.go
                 internal/stack/profiles/go.toml
                 internal/stack/embed_test.go
       covers:   c-2, c-3, c-5
       contract: With a user-dir profile (~/.claude/dross/profiles/) sharing the
                 embedded Go profile's id but a different test_command, Load
                 returns the USER value — TestUserDirWinsOnIDCollision fails if
                 the embedded copy wins. A malformed *.toml in the user dir is
                 reported as an error naming the file and does NOT silently drop
                 the embedded profile — TestMalformedUserProfileSurfacedNotSwallowed.
                 With HOME pointed at a dir that has no profiles/ subdir, Load
                 still returns the embedded Go profile — TestUserDirAbsentFallsBack.

  t-4  Resolve runtime command set from profile
       files:    internal/stack/runtime.go
                 internal/stack/runtime_test.go
       covers:   c-2
       contract: The Go profile resolves test/typecheck/format/build to the
                 exact strings in project.toml [runtime] today
                 ("go test -count=1 ./...", "go vet ./...", "gofmt -l .",
                 "make build"); TestGoRuntimeMatchesLocked fails if any slot
                 drifts. A profile slot with multiple commands resolves to the
                 first available variant; TestRuntimeSlotPicksAvailableVariant
                 fails if it picks an unavailable one or concatenates them.

Wave 2 (depends t-1, t-2)

  t-5  Add `dross stack` command tree
       files:    internal/cmd/stack.go
                 cmd/dross/main.go
                 internal/cmd/stack_test.go
       covers:   c-1
       contract: `dross stack detect` on a package.json-only path prints
                 "unsupported stack" and exits 0 (advisory, not a hard error);
                 TestStackDetectNonGoExitsZeroUnsupported fails if it exits
                 non-zero or prints "go". `dross stack show <unknown-id>` exits
                 non-zero with an id-not-found error; TestStackShowUnknownIDErrors.
                 EnforceSubcommandKnown still rejects `dross stack bogus`.

Wave 3 (depends t-3)

  t-6  Catalogs consume the Go profile loadout
       files:    internal/security/catalog.go
                 internal/quality/catalog.go
                 internal/stack/loadout.go
                 internal/security/catalog_test.go
                 internal/quality/catalog_test.go
       covers:   c-3
       contract: The scanner set ScannersFor("go") derives from the profile's
                 tool loadout, so removing a tool entry from go.toml drops it
                 from the live scanner list — TestScannersTrackProfile fails if
                 the inline map is still authoritative. Same for analyzers via
                 TestAnalyzersTrackProfile. A profile tool with no `bin` is
                 rejected at load so it can never produce a silently-unrunnable
                 catalog entry — TestProfileToolRequiresBin.

  t-7  Apply embedded recon to security + quality recon
       files:    internal/security/recon.go
                 internal/quality/recon.go
                 internal/security/recon_test.go
                 internal/quality/recon_test.go
       covers:   c-1
       contract: Both packages' DetectLanguages delegate to internal/stack;
                 TestSecurityReconDelegatesToStack and
                 TestQualityReconDelegatesToStack fail if a third independent
                 copy of the ext→lang map reappears (assert identical results to
                 stack.Detect on a shared fixture, including the non-Go-only
                 case returning the same answer in all three call sites).

Wave 3 (depends t-3)

  t-8  Seed init/onboard runtime from profile
       files:    internal/cmd/init.go
                 internal/cmd/onboard.go
                 internal/cmd/init_test.go
                 internal/cmd/onboard_test.go
       covers:   c-2
       contract: After `dross init` in a Go repo, project.toml [runtime] test/
                 typecheck/format/build equal the Go profile's values, and
                 [stack].profile == the matched id; TestInitSeedsRuntimeFromProfile
                 fails on any hardcoded guess. When detection returns unsupported,
                 init writes NO fabricated runtime commands (empty slots, not
                 wrong ones) — TestInitUnsupportedLeavesRuntimeUnseeded.

  t-9  `dross stack loadout` markdown emitter
       files:    internal/cmd/stack.go
                 internal/stack/loadout.go
                 internal/cmd/stack_test.go
       covers:   c-4
       contract: `dross stack loadout` emits a markdown block built from
                 stack.locked (recommended MCP tools, guardrails, locked
                 conventions); TestLoadoutRendersFromLocked fails if a locked
                 convention from the profile is missing from the output. A
                 loadout that lists an availability-gated tool absent from PATH
                 marks it as such rather than presenting it as ready —
                 TestLoadoutMarksUnavailableGatedTool.

---

## Coverage

- **c-1** (detection, non-Go→unsupported, not hardcoded Go): t-2 (core detector),
  t-5 (`dross stack detect` surface), t-7 (recon de-duplication so all call sites
  share the same non-Go-aware detector)
- **c-2** (Go profile is single source for runtime command set; init/onboard seed
  from it): t-4 (resolve runtime from profile), t-8 (init/onboard write-through)
- **c-3** (profile declares tool loadout that secure/quality consume, replacing
  inline maps): t-1 (schema carries the loadout), t-3 (embedded Go profile data),
  t-6 (catalogs read it live)
- **c-4** (`dross stack loadout` markdown block from stack.locked): t-9
- **c-5** (adding a stack = one declarative entry, zero mechanism change, proven
  by a second non-Go profile that loads and is selected): t-1 (schema is the only
  thing a new stack edits), t-3 (embed + user-dir drop-in path; a second non-Go
  profile fixture loads and detection selects it)

All criteria c-1..c-5 covered (5/5).

> Note on c-5's "second non-Go profile selected by detection" proof: it rides on
> t-3's embed_test (a fixture profile in the user dir) plus t-2's detector
> (selection by signal). No mechanism code changes between adding the embedded Go
> profile and adding the fixture profile — that *is* the test. If a code change is
> needed to make the fixture profile load/select, t-1 or t-3's schema was too narrow
> and their contracts fail first.

---

## Judgment calls

- **Split detection (t-2) from the command surface (t-5)** rather than one big
  "stack command" task: the false-Go failure mode lives in the pure detector and
  must be unit-tested without cobra in the way; bundling them would let a CLI test
  mask a detector bug. Rejected: a single t covering detect+cmd.
- **t-7 (recon de-duplication) is its own task, not folded into t-2.** The risk is
  *divergence of three copies*, not detection logic itself; isolating it means the
  "no third copy / all call sites agree" assertion is owned and can fail loudly.
  Rejected: deleting the two recon copies inside t-2 (would couple a refactor of
  two other packages to the detector task and blur the contract).
- **t-6 and t-7 both touch security/quality but stay separate.** t-6 owns the
  *catalog→profile* live-read drift risk; t-7 owns the *detection* duplication
  risk. They're different failure modes in the same packages; merging would create
  a 4+ file task spanning two concerns. They share no files (t-6: catalog.go;
  t-7: recon.go) so they run in the same wave in parallel.
- **Put `dross stack loadout` (t-9) in wave 3, not wave 2.** Its markdown is built
  from the resolved profile incl. availability-gated tools, so it needs t-3's
  merged/embedded profile loaded; gating it on t-3 keeps the unavailable-tool
  marking honest. Rejected: wave-2 placement reading raw stack.locked from
  project.toml (would bypass the profile and miss the gated-tool failure mode).
- **`dross stack detect` exits 0 on unsupported (advisory), `dross stack show
  <unknown>` exits non-zero (lookup error).** Detection is a report, not a gate —
  a non-Go repo is a valid answer, not a failure; an unknown explicit id IS an
  error. Rejected: making detect exit non-zero on unsupported (would break init's
  write-through, which must proceed and leave runtime unseeded per t-8).
- **Schema (t-1) is wave 1 and deliberately over-shaped** (multi-command slots,
  gated/per-OS tools) even though Go alone doesn't exercise all of it — locked
  decision schema_extensibility makes that non-negotiable, and t-1's contract
  tests the unused shapes now so the next-phase drop-in doesn't reshape the schema.
  Rejected: a Go-only minimal schema (would violate the locked decision and push
  the cost to the next phase).
- **Did NOT touch internal/profile/ or internal/cmd/profile.go.** Name collision
  with the behavioural GSD profile; stack profiles are a brand-new internal/stack/
  package. No task references the old package — a plan that extended it would be
  wrong per the repo-layout note.
- **assets/prompts changes intentionally omitted as a task.** c-4 only requires
  `dross stack loadout` to *emit* the injectable block; rule r-01 (make install)
  governs prompt wiring, which is consumption, not this phase's mechanism. The
  emitter (t-9) is the deliverable; wiring a specific prompt to call it is left to
  whoever injects it, avoiding a make-install-gated task with a weak contract.
