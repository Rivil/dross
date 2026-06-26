# Synthesis — 06-dross-quality (cold judge over risk / mvp / verification)

I authored none of the three drafts. Below: scores, the merged plan grafted onto the
strongest skeleton, and the genuine disagreements left explicit rather than papered over.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
| --- | --- | --- | --- | --- |
| **risk** | 5/5; uniquely splits risk-ranking (t-3) from the ledger and gives gitignore its own owner — every failure mode mapped to exactly one task. | Strongest: every contract phrased as "remove X → test Y fails", named per-test; carries unique guards (write-boundary walk on NewRun, RunID `nogit` fallback, unknown-rank-sorts-last). | 8 tasks; folds catalog+recon into one 4-file task (t-4) — the one place it relaxes its own one-surface rule, justified by the shared coverage-floor invariant. | Correct 3-wave DAG; honestly flags t-8 (prompt) as the one deliberate non-minimal placement (content-gated, could be wave 1, parked in wave 3 for CLI-name alignment). |
| **mvp** | 5/5 but thinnest; folds risk-ranking into the ledger and gitignore+main.go into the CLI task — coverage holds, but several distinct failure surfaces share a task. | Good and concrete, but coarser: one `findings_test.go` owns both the refute gate and risk ordering, so a ledger-sort vs refute-gate regression points at the same file. | 5 tasks; deliberately collapses run+catalog+recon into a 6-file t-1 (at the granularity ceiling) — defensible but loses the coverage-floor single-owner clarity. | Correct 2-wave DAG; soft-justifies pulling the prompt into wave 2 for subcommand-name alignment. Fewer waves, less parallelism lost. |
| **verification** | 5/5; designs assertion-first, so the code/prompt split from `testable_surface` is explicit and each criterion shows a code half and/or prompt half. | Strongest on round-trip/semantics: TestScaffoldRoundTrips through `phase.LoadSpec`, TestCatalogExcludesCosmetic (guards locked `quality_scope`), Risk-not-Severity type-name contract. | 8 tasks; same separation as risk BUT keeps catalog (t-2) and recon (t-4) as two single-file tasks across waves — cleaner than risk's merged t-4, matches the secure mirror's actual file layout. | Correct 3-wave DAG; authors prompt (t-7) in wave 1 and its guarding test (t-8) in wave 3 — the sharpest wave reasoning of the three. |

**Skeleton: `verification`.** It scores top on contract specificity and wave correctness,
its 8-task graph matches the *actual* secure-mirror file layout (confirmed: `internal/security/`
ships separate run/catalog/recon/findings/scaffold + a standalone gitignore test), and its
assertion-first design makes the locked `testable_surface` code/prompt cleavage explicit.
Risk is a near-tie and donates the sharpest individual contracts; mvp is the dissenting
minority on task count (see Disagreements).

## Merged plan

```
Phase 06-dross-quality — 8 tasks across 3 waves

Wave 1
  t-1  Run-dir + run-id writer                                          [verification+risk]
       files:    internal/quality/run.go, internal/quality/run_test.go
       covers:   c-1, c-5
       contract: RunID(now,sha) == "<timestamp>-<short-sha>", == "<ts>-nogit" when sha=="";
                 NewRun creates exactly one dir under .dross/quality/ and, called twice in the
                 same second on the same sha, appends a collision suffix instead of clobbering —
                 break the suffix loop and TestNewRunNoClobber sees one dir overwritten and fails;
                 if RunID drops the <timestamp>-<short-sha> shape or the empty-sha→"nogit"
                 fallback, TestRunID fails. [GRAFT from risk] NewRun writing anything outside
                 .dross/quality/<id>/ fails TestNewRunWriteBoundary's path-set walk (c-5 owned at
                 the writer, not only at the CLI).

  t-2  Dimension→analyzer catalog + detection                          [verification]
       files:    internal/quality/catalog.go, internal/quality/catalog_test.go
       covers:   c-3
       contract: Catalog() carries every core Go analyzer (gocyclo/complexity, dupl, deadcode,
                 errcheck, ineffassign) each flagged Core; AnalyzersFor("go") returns the full Go
                 toolbelt; AnalyzersFor(stub-lang) returns ONLY the agnostic set, never empty;
                 Detect() under an all-missing lookPath marks every analyzer !Installed and keeps a
                 non-empty Install hint. Drop a core analyzer → TestCatalogCompleteness fails; break
                 the agnostic fallback → TestAnalyzersForAgnosticFallback fails; a cosmetic-only
                 lint in the table → TestCatalogExcludesCosmetic fails (guards locked quality_scope).

  t-3  Maintainability-risk findings ledger                            [verification+risk]
       files:    internal/quality/findings.go, internal/quality/findings_test.go
       covers:   c-1, c-2
       contract: Finding carries Risk (contextual maintainability-risk, NOT nominal category),
                 File, Line, Evidence, Refutation. Survived() is false with empty Refutation →
                 TestUnrefutedIsNotSurvivor (an unrefuted candidate never reaches Survivors()).
                 Ledger.Validate() rejects empty/duplicate ids and unknown Risk → TestLedgerDuplicateID,
                 TestLedgerInvalidRisk fail if negated. Survivors() returns highest-risk-first
                 regardless of input order → TestSurvivorsRiskOrder. Load() on garbled TOML returns
                 an error, never panics → TestLoadMalformed. [GRAFT from risk] blast-radius weighting
                 is part of the order contract: a high-complexity finding on a cold path must sort
                 BELOW a moderate one on a core/hot path, and an unknown rank sorts last, not first.

  t-7  Quality orchestration prompt content                            [verification+mvp]
       files:    assets/prompts/quality.md, assets/commands/dross-quality.md
       covers:   c-2, c-3, c-4, c-5
       contract: Authors the markdown its guard (t-8) content-gates; wave 1 because the markdown
                 needs no Go output, only its test waits. Must contain: "refute"/"majority vote"/
                 "drop" (c-2); "calibrate"/"downrank"/"never suppress" AND the code-only-sweep note
                 that the tool sweep reads no .dross planning artifacts (locked context_model);
                 "no --fix"/"never edit" application code (c-5); "propose-then-ask before locking"
                 (c-4); detect→plan→gate→sweep + "tool-coverage manifest" continue/install gate (c-3).
                 Command shim @-includes the prompt with read-only-plus-Write/AskUserQuestion tools.

Wave 2 (depends t-2, t-3)
  t-4  Tool-coverage manifest + language recon                         [verification]
       files:    internal/quality/recon.go, internal/quality/recon_test.go
       covers:   c-3
       contract: DetectLanguages walks a tree, returns sorted de-duped langs, never descends
                 .dross/.git/node_modules/vendor → TestDetectLanguages on main.go + .dross/x.go
                 asserts ["go"] and proves .dross is skipped. BuildManifest records BOTH ran
                 (installed) and skipped (missing) analyzers → TestManifestRecordsSkipped under
                 all-missing lookPath has len(Ran())==0 and len(Skipped())>0; if Skipped() silently
                 dropped missing tools a thin toolbelt would read "all clear" and this fails.
                 (Depends t-2 for AnalyzersFor / Detect.)

  t-5  Risk-ordered remediation scaffold writer                        [verification+risk]
       files:    internal/quality/scaffold.go, internal/quality/scaffold_test.go
       covers:   c-4
       contract: ScaffoldSpec emits exactly one criterion per SURVIVING finding, highest-risk
                 first → TestScaffoldOnePerFinding + TestScaffoldRiskOrder (low-before-critical
                 ledger ⇒ criterion[0] cites the critical's id). Each criterion cites its
                 findings.toml ledger id → TestScaffoldCitesLedger (no finding hides behind a
                 tier-level test). Zero survivors → ScaffoldSpec returns an error and writes nothing
                 → TestScaffoldEmptyRefuses. Emitted spec.toml round-trips through phase.LoadSpec
                 → TestScaffoldRoundTrips. (Depends t-3 for Ledger/Survivors.)

Wave 3 (depends t-1, t-2, t-4, t-5, t-7)
  t-6  `dross quality {detect,run,scaffold}` command + containment     [verification+mvp]
       files:    internal/cmd/quality.go, internal/cmd/quality_test.go, cmd/dross/main.go
       covers:   c-1, c-3, c-5
       contract: Quality() registers detect/run/scaffold → TestQualityCommandRegistered.
                 `quality detect <dir>` prints a scanners section with installed/missing markers
                 → TestQualityDetectOutput. `quality run .` succeeds with ZERO analyzers installed
                 (partial coverage, no hard-error) and writes report.md containing the tool-coverage
                 manifest under one .dross/quality/ run dir → TestQualityRunCreatesDir +
                 TestQualityRunWritesManifest. containedPath refuses a finding-derived "../x" /
                 deep traversal, accepts "report.md" → TestQualityRunReadOnly; a full run touches
                 only paths under .dross/quality/ (walk asserts the prefix) — c-5 at the CLI.
                 scaffold on a valid ledger writes spec.toml; on a zero-survivor ledger returns an
                 error → TestQualityScaffold / TestQualityScaffoldEmptyErrors.
                 (NOTE: .gitignore moved OUT of this task — see t-9; provisional default.)

  t-8  Prompt-content assertions + commands/prompts parity            [verification+risk]
       files:    internal/cmd/quality_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       contract: For each locked prompt rule a SEPARATE sub-test fails if its phrase is absent from
                 a normalised (lowercased, backtick/emphasis-stripped) quality.md:
                 {c-2 refute majority-vote drop}, {context calibrate-only downrank / never suppress},
                 {c-5 read-only no --fix / never edit}, {c-4 propose-then-ask before locking},
                 {c-3 tool-coverage manifest gate}. Removing any one mandated section fails exactly
                 that sub-test. The existing TestCommandsPromptsParity already guards dross-quality.md
                 ↔ quality.md 1:1, so no extra parity task is needed.

  t-9  Gitignore quality run artifacts (behavioural)                   [risk]  ← provisional
       files:    .gitignore, internal/quality/gitignore_test.go
       covers:   c-5
       contract: if ".dross/quality/" is absent/wrong in .gitignore, TestQualityArtifactsGitignored
                 fails — `git check-ignore .dross/quality/x/report.md` exits non-zero, proving raw
                 findings would be committable. Standalone owner so a CLI regression and a gitignore
                 regression don't share one task status / test file.
```

Origin note: t-9 is renumbered from risk's t-7 to avoid colliding with the skeleton's
t-7 (prompt). The skeleton's wave-3 dependency list gains t-9 only transitively (it is
independent — could be wave 1); it is kept in wave 3 next to the artifacts it guards.

## Disagreements

### D1 — Task count: 8 (risk, verification) vs 5 (mvp)
- **risk / verification:** split run, catalog, recon, findings, risk-ordering and scaffold
  into separate single-surface owners so each distinct failure mode has exactly one killing
  test; matches the secure mirror's real file layout (separate `*.go` per concern, confirmed
  on disk).
- **mvp:** collapse run+catalog+recon into one 6-file task and fold risk-ranking into the
  ledger — "no per-file vanity tasks", every task criterion-forced, 5 tasks / 2 waves.
- **Provisional default: 8 tasks (skeleton).** Chosen because the locked `ranking_model`
  and the coverage-floor invariant are genuinely distinct failure surfaces, and single-owner
  tasks let verify map one killing test per regression. **Why it matters:** mvp's collapse is
  not wrong — it stays under the granularity ceiling — but it makes one `findings_test.go` own
  both the refute gate and risk ordering, so verify can't distinguish a sort bug from a
  gate bug by task status. If the executor prefers fewer waves over per-surface isolation,
  mvp's 5-task graph is the live alternative and should be reconsidered as a whole, not
  by deleting individual tasks from the 8.

### D2 — Gitignore: standalone task (risk) vs folded into the CLI task (mvp, verification)
- **risk:** dedicated task (its own behavioural test, no Go source) so the no-pre-disclosure
  guarantee (c-5) has an unambiguous owner; a CLI regression and a gitignore regression must
  not share one task status.
- **mvp / verification:** fold the one-line `.gitignore` edit into the CLI task (t-6) — a
  standalone task is a sub-10-min, one-file unit that the granularity floor discourages.
- **Provisional default: standalone (t-9, from risk).** The secure mirror ships a standalone
  `internal/security/gitignore_test.go` with no paired Go source — so risk's split is the
  *as-built* precedent, not a novel invention. **Why it matters:** if the team applies the
  granularity-floor rule strictly, fold t-9 into t-6 and drop to 7 tasks; the contract and
  test are identical either way, only ownership/atomic-commit boundary differs. This is the
  lowest-stakes divergence — purely a commit-granularity call.

### D3 — Catalog + recon: two tasks (verification) vs one task (risk)
- **verification:** catalog (t-2, wave 1) and recon/manifest (t-4, wave 2) as two single-file
  tasks — recon depends on catalog's `AnalyzersFor`/`Detect`, so the wave split is real and
  each file gets its own contract.
- **risk:** fold both into one 4-file wave-1 task (t-4) because the coverage-floor invariant
  ("zero applicable analyzers / thin toolbelt reads as all-clear") spans catalog (supplies the
  set) and recon (builds the manifest) and has "no single home" if split.
- **Provisional default: two tasks (verification skeleton).** The dependency is genuine
  (recon needs catalog's types) and matches the mirror's separate files; the coverage-floor
  invariant is still single-owned — its *manifest* half lives in t-4's TestManifestRecordsSkipped
  and its *catalog* half in t-2's TestAnalyzersForAgnosticFallback, two different surfaces.
  **Why it matters:** risk's concern is legitimate — if the floor invariant is treated as one
  indivisible contract it wants one owner. The merged plan answers by keeping the two halves
  but naming both floor tests explicitly so neither falls between the tasks.

### D4 — Prompt wave placement: wave 1 author + wave 3 guard (verification) vs prompt in last wave (risk t-8) / wave 2 (mvp)
- **verification:** author quality.md in wave 1 (t-7, no Go dependency) and place only its
  content-gate test (t-8) in wave 3 — maximises parallelism, the markdown is independent text.
- **risk:** keep the prompt in wave 3, deliberately non-minimal, to align its c-3/c-4 wording
  with the as-built CLI/scaffold surface names (a prompt naming a flag the CLI lacks is itself
  a failure mode).
- **mvp:** prompt in wave 2 for the same subcommand-name-alignment reason (soft dependency).
- **Provisional default: author in wave 1, guard in wave 3 (verification skeleton).** Chosen
  for parallelism. **Why it matters:** risk/mvp raise a real drift hazard — if the prompt is
  authored before the CLI exists, it may name `dross quality run/detect/scaffold` subcommands
  that t-6 then defines differently, and TestCommandsPromptsParity checks only file pairing,
  not subcommand names. Mitigation baked into the merged plan: the locked `testable_surface`
  decision already fixes the three subcommand names (run-dir/tool-detection/scaffold-writer),
  so the prompt author has the authoritative names without waiting for t-6. If that mitigation
  is judged too thin, move t-7 to wave 3 alongside t-8 (risk's placement) at the cost of
  parallelism.
```
