# Synthesis — 07-stack-profiles

Judged from three independently-drafted decompositions (risk / mvp / verification).
Author of synthesis authored none of them. Paths below were verified against the
working tree: `internal/stack/` does not yet exist (all new files there are genuinely
new); `internal/cmd/stack.go` is new; `cmd/dross/main.go` is the real registration
site (verification's `internal/cmd/main.go` is a **wrong path** — it does not exist);
`internal/cmd/onboard_test.go` does not exist yet (new test file); catalogs, both
`recon.go` copies, `init.go`, `onboard.go` (`toProject()` seeds runtime),
`assets/prompts/execute.md`, and `ScannersFor/AnalyzersFor` signatures are all real.

## Scores

Scale: ✓✓ strong · ✓ adequate · ✗ weak.

| Dimension | risk | mvp | verification |
|---|---|---|---|
| **Criteria coverage** | ✓✓ all 5, plus distinct task per call-site for c-1 | ✓ all 5, leanest mapping; c-1 thin (no recon de-dup) | ✓✓ all 5, each pinned to a *named* test + every locked decision pinned |
| **Test-contract specificity** | ✓✓ failure-mode contracts name the exact regression (user-dir-wins, malformed-surfaced, no-third-copy) | ✓ contracts real but coarser (one assertion each, some grep-based) | ✓✓ contracts derived first, exact literal command strings + named test funcs |
| **Granularity** | ✓ 9 tasks; t-6/t-7 split same pkgs by *failure mode* (justified but heavy) | ✓✓ 6 tasks, clean ≤5-file cap, no stubs | ✗ 8 tasks but over-split: detection→3 tasks (t-2/t-4), runtime/loadout as solo 2-file tasks risk sub-10-min stubs |
| **Wave correctness** | ✓✓ correct deps; t-9 loadout→wave 3 (needs merged profile for gated-tool marking); catalogs+recon parallel | ✓ correct but loadout folded into cmd (t-5) loses the gated-tool-marking dependency on the merge | ✓ correct dep edges but t-8 is a 6-file wave-3 monolith (cmd+init+onboard+apply) |

**Skeleton: `risk`.** It has the sharpest, most regression-specific contracts and
the only plan that owns *every* failure seam the prompt flagged (embed/userdir merge,
false-Go detection, uninstalled tool, the **three** `DetectLanguages` copies diverging,
live-catalog drift). Its wave/dependency graph is correct, and it is the only draft
that gates `dross stack loadout` on the merged profile so the unavailable-gated-tool
marking stays honest. mvp is grafted in for granularity discipline (collapsing risk's
slightly redundant edges) and verification for the explicit-locked-decision pins and
the "second profile is *selectable*" extensibility contract.

## Merged plan

Skeleton = risk's 9-task / 3-wave graph. Grafts: verification's extensibility-selection
contract onto detection; verification's explicit locked-decision pins (`profile_home`,
`schema_extensibility`, `config_integration`) folded into existing task contracts; mvp's
`internal/stack/profiles/README.md` documentation file added to t-3 (c-5 requires the
zero-code drop-in be *documented*, which the skeleton omitted). No task is invented that
appears in no draft.

### Phase 07-stack-profiles — 9 tasks across 3 waves

#### Wave 1

```
t-1  Define stack profile schema + loader                                   [risk + verification]
     files:      internal/stack/profile.go
                 internal/stack/profile_test.go
     covers:     c-3, c-5
     contract:   A profile TOML exercising a package-manager variant, two commands in
                 one runtime slot, an availability-gated tool, and a per-OS tool name
                 round-trips into the struct; dropping any of those shapes fails
                 TestProfileSchemaShapes (pins locked schema_extensibility [verification]).
                 A profile with empty `id` is rejected with an error, not loaded nameless
                 — TestProfileRejectsEmptyID. A profile tool with no `bin` is rejected at
                 load — TestProfileToolRequiresBin (moved up from risk t-6 so the guard
                 lives with the schema, not the catalog consumer).
     depends_on: —
```

```
t-2  Centralize language detection in stack pkg                             [risk + verification]
     files:      internal/stack/detect.go
                 internal/stack/detect_test.go
     covers:     c-1, c-5
     contract:   Detect on a package.json-only fixture (no go.mod, no .go) returns an
                 explicit "unsupported" — NOT "go", not empty, no panic —
                 TestDetectNonGoFixtureUnsupported. Detect on this repo's root (go.mod)
                 returns id "go" — TestDetect_GoRepoMatchesGo [verification]. On a polyglot
                 fixture (go.mod + stray .py) the go.mod signal still wins —
                 TestDetectPolyglotPrefersSignal. Detection keys off declared profile
                 signals, not a hardcoded language switch: a loaded second-profile fixture
                 is *selectable* by detection on its matching fixture —
                 TestDetect_SecondProfileSelected [verification's selection contract, c-5].
     depends_on: —
```

#### Wave 2

```
t-3  Embed Go profile + merge user-dir profiles + document drop-in         [risk + mvp(README)]
     files:      internal/stack/embed.go
                 internal/stack/profiles/go.toml
                 internal/stack/profiles/README.md          <- grafted from mvp t-6
                 internal/stack/embed_test.go
     covers:     c-2, c-3, c-5
     contract:   A user-dir profile (~/.claude/dross/profiles/) sharing the embedded Go
                 id but a different test_command makes Load return the USER value —
                 TestUserDirWinsOnIDCollision (pins locked profile_home [verification]).
                 A malformed *.toml in the user dir is reported as an error naming the
                 file and does NOT silently drop the embedded profile —
                 TestMalformedUserProfileSurfacedNotSwallowed. HOME with no profiles/
                 subdir still yields the embedded Go profile — TestUserDirAbsentFallsBack.
                 profiles/README.md states a new stack is a single TOML drop-in with zero
                 code change, or the c-5 *documentation* requirement is unmet [mvp].
     depends_on: t-1
```

```
t-4  Resolve runtime command set from profile                              [risk + verification]
     files:      internal/stack/runtime.go
                 internal/stack/runtime_test.go
     covers:     c-2
     contract:   The Go profile resolves test/typecheck/format/build to the exact strings
                 in project.toml [runtime] today ("go test -count=1 ./...", "go vet ./...",
                 "gofmt -l .", "make build") — TestGoRuntimeMatchesLocked; editing a
                 command in go.toml changes the derived value [verification]. A slot with
                 multiple commands resolves to the first *available* variant —
                 TestRuntimeSlotPicksAvailableVariant fails if it picks an unavailable one
                 or concatenates.
     depends_on: t-1
```

```
t-5  Add `dross stack` command tree                                        [risk + mvp]
     files:      internal/cmd/stack.go
                 cmd/dross/main.go                          <- real registration site (NOT internal/cmd/main.go)
                 internal/cmd/stack_test.go
     covers:     c-1
     contract:   `dross stack detect` on a package.json-only path prints "unsupported"
                 and exits 0 (advisory) — TestStackDetectNonGoExitsZeroUnsupported fails
                 if it exits non-zero or prints "go". `dross stack show <unknown-id>`
                 exits non-zero with id-not-found — TestStackShowUnknownIDErrors.
                 Subcommand set detect/show/list/apply/loadout is present and
                 EnforceSubcommandKnown rejects `dross stack bogus`.
     depends_on: t-1, t-2
```

#### Wave 3

```
t-6  Catalogs consume the Go profile loadout                               [risk + mvp + verification]
     files:      internal/security/catalog.go
                 internal/quality/catalog.go
                 internal/stack/loadout.go
                 internal/security/catalog_test.go
                 internal/quality/catalog_test.go
     covers:     c-3
     contract:   ScannersFor("go") derives from the profile's tool loadout, so removing a
                 tool entry from go.toml drops it from the live list —
                 TestScannersTrackProfile fails if the inline map is still authoritative;
                 same for analyzers via TestAnalyzersTrackProfile. Agnostic tools
                 (gitleaks/scc) stay available and TestCatalogExcludesCosmetic still passes
                 (profile data routed through the existing cosmetic guard) [verification].
     depends_on: t-3
```

```
t-7  De-duplicate recon DetectLanguages onto stack pkg                     [risk]
     files:      internal/security/recon.go
                 internal/quality/recon.go
                 internal/security/recon_test.go
                 internal/quality/recon_test.go
     covers:     c-1
     contract:   Both packages' DetectLanguages delegate to internal/stack;
                 TestSecurityReconDelegatesToStack and TestQualityReconDelegatesToStack
                 assert identical results to stack.Detect on a shared fixture (incl. the
                 non-Go-only case) and fail if a third independent ext→lang map reappears.
     depends_on: t-3
```

```
t-8  Seed init/onboard runtime from profile                                [risk + mvp]
     files:      internal/cmd/init.go
                 internal/cmd/onboard.go                    (toProject() is the real seed site)
                 internal/cmd/init_test.go
                 internal/cmd/onboard_test.go               (new test file)
     covers:     c-2
     contract:   After `dross init` in a Go repo, project.toml [runtime] test/typecheck/
                 format/build equal the Go profile's values and [stack].profile == the
                 matched id — TestInitSeedsRuntimeFromProfile fails on any hardcoded guess.
                 When detection returns unsupported, init writes NO fabricated runtime
                 commands (empty slots, not Go defaults) — TestInitUnsupportedLeavesRuntimeUnseeded.
     depends_on: t-3
```

```
t-9  `dross stack loadout` markdown emitter                                [risk + verification]
     files:      internal/cmd/stack.go
                 internal/stack/loadout.go
                 internal/cmd/stack_test.go
     covers:     c-4
     contract:   `dross stack loadout` emits a markdown block built from stack.locked
                 (recommended MCP tools, guardrails, locked conventions) — assert specific
                 headings + one known declared line; TestLoadoutRendersFromLocked fails if
                 a locked convention is missing (pins locked agent_loadout_shape:
                 markdown block, no agent .md files [verification]). An availability-gated
                 tool absent from PATH is marked as such, not presented as ready —
                 TestLoadoutMarksUnavailableGatedTool. Empty loadout renders "(none)",
                 not blank [verification].
     depends_on: t-3
```

Coverage: c-1 → t-2, t-5, t-7 · c-2 → t-4, t-8 · c-3 → t-1, t-3, t-6 · c-4 → t-9 ·
c-5 → t-1, t-2, t-3. All 5 covered. Locked decisions pinned: schema_extensibility→t-1,
profile_home→t-3, config_integration/command_surface→t-5+t-8, agent_loadout_shape→t-9.

> Note (rule r-01): no task edits `assets/prompts/*`. c-4 only requires the emitter
> (t-9); wiring a prompt to inject the block is consumption, make-install-gated, and
> deliberately out of scope — this resolves a divergence, see below.

## Disagreements

**1. Prompt-wiring task (`assets/prompts/execute.md`) — include it or not?**
mvp's **t-6 includes it** (wire `dross stack loadout` into execute.md + write
profiles/README.md), arguing c-4's "prompts can inject inline" and c-5's "documented"
want a concrete consumer. risk **explicitly rejects** it (c-4 only needs the block
*emitted*; prompt wiring is consumption, gated by r-01/make-install, with a weak
grep-based contract). verification also excludes it.
*Provisional default: EXCLUDE the prompt-wiring half (risk/verification, 2–1), but
GRAFT the README.md half of mvp's t-6 into t-3* — because the README satisfies c-5's
*documentation* clause with a strong presence contract, whereas the execute.md edit
adds an r-01-gated task whose only test is a grep. **Why it matters:** if the human
wants c-4 proven end-to-end (a prompt actually injecting the block), re-add the
execute.md edit as a 10th task; the merged plan proves the block is *injectable*, not
that any prompt injects it.

**2. Recon `DetectLanguages` de-duplication — centralize as its own task, or skip?**
risk makes it **t-7, a dedicated task** owning the "three copies diverge" failure mode.
verification **centralizes it but folds the import-swap into the catalog task (t-6)**,
since that task already touches the files. mvp **explicitly declines** to de-dup the two
recon copies ("no criterion requires it; MVP drops untraceable work") and only *reuses*
one copy from the new detector.
*Provisional default: KEEP it as standalone t-7 (risk).* **Why it matters:** the
repo-layout note calls out the duplication explicitly, and c-1's intent is "not
hardcoded to Go" across *all* call sites — a silent third copy is exactly the
regression. A dedicated task makes the "no third copy / all sites agree" assertion
ownable and loud. If granularity pressure wins, fold t-7's edits into t-6 per
verification (they share no files with the catalog change, so it's a clean merge but a
muddier contract). mvp's skip is rejected: it leaves the flagged duplication standing.

**3. Detection granularity — one task or split scan/selection?**
risk and mvp keep **one detection task** (risk t-2, mvp t-2). verification **splits into
t-2 (shared scan / unsupported result) + t-4 (profile selection)**, arguing c-1's
"matches Go / unsupported" and c-5's "second profile selectable" are different surfaces.
*Provisional default: ONE task (risk/mvp, 2–1), with verification's selection assertion
grafted in as a sub-contract of t-2.* **Why it matters:** splitting yields a 2-file
selection task that risks a sub-10-min stub; folding the selection assertion into t-2
keeps the c-5 "selectable by data" proof without fragmenting. If selection logic proves
non-trivial (signal-priority resolution across many profiles), promote it back to a
separate task per verification.

**4. Where the agent-loadout render logic lives.**
risk puts `internal/stack/loadout.go` creation in **t-6 (catalogs)** and reuses it in
t-9. verification gives loadout its **own task t-7** (`internal/stack/loadout.go` +
loadout_test). mvp puts loadout rendering **inside the command (t-5)**, no separate file.
*Provisional default: loadout.go is authored in t-9 (the emitter), and t-6 references it
only as a consumer.* I moved the file's *creation* out of risk's t-6 into t-9 so the
renderer ships with its own contract (TestLoadoutRendersFromLocked) rather than riding a
catalog task. **Why it matters:** if t-6 needs loadout.go before t-9 runs (both are
wave 3, both depend on t-3, no ordering between them), the file must exist when t-6
compiles — so loadout.go's *struct/accessor* may need to land in t-1 (schema) with only
the *renderer* in t-9. Flagged for the executor: if t-6 imports a loadout accessor, pull
the accessor into t-1 and keep only `Render()` in t-9.

**5. Init+onboard+command — one wave-3 monolith or separate tasks?**
verification's **t-8 bundles** stack-command + init + onboard + apply (6 files, wave 3).
risk and mvp **separate** the command (t-5, wave 2) from runtime-seeding (risk t-8 / mvp
t-4, wave 3).
*Provisional default: SEPARATE (risk/mvp).* **Why it matters:** the command tree (t-5)
has no dependency on the merge and belongs in wave 2; folding init/onboard seeding into
it would push the whole command to wave 3 and create a 6-file task over the granularity
cap. Kept t-5 (command, wave 2) and t-8 (seeding, wave 3) distinct.
