# Phase 06-dross-quality — Verification-lens draft

Lens: design backward from the test contract. For each criterion I wrote the ideal
assertion first, then sized the smallest task that makes that assertion satisfiable.
The security sibling (internal/security/*, internal/cmd/security.go, secure_prompt_test.go)
is the mirror; every file path and contract below has a security analog already proven green.

The spec's `testable_surface` decision splits the work two ways, and the verification
lens makes that split the primary cleavage:
- CODE-testable (real unit tests): run-dir/run-id, language→analyzer catalog + detection,
  maintainability-risk ledger + survivors ordering, risk-ordered scaffold writer, read-only
  path containment, gitignore behaviour.
- PROMPT-asserted (content assertions in quality_prompt_test.go): refute-panel majority-vote
  drop (c-2), calibrate-only/downrank-never-suppress context model, read-only/no --fix (c-5),
  propose-then-ask before locking (c-4), tool-coverage-manifest gate (c-3 orchestration half).

```
Phase 06-dross-quality — 8 tasks across 3 waves

Wave 1
  t-1  Run-dir + run-id writer
       files:    internal/quality/run.go, internal/quality/run_test.go
       covers:   c-1
       contract: RunID(now,sha) == "<timestamp>-<short-sha>" and == "<ts>-nogit" when sha=="";
                 NewRun creates exactly one dir under .dross/quality/ and, called twice in the
                 same second on the same sha, appends "-2" instead of clobbering — if the
                 collision-suffix loop breaks, TestNewRunNoClobber sees one dir overwritten and fails.

  t-2  Dimension→analyzer catalog + detection
       files:    internal/quality/catalog.go, internal/quality/catalog_test.go
       covers:   c-3
       contract: Catalog() carries every core Go analyzer (gocyclo/complexity, dupl, deadcode,
                 errcheck, ineffassign) each flagged Core; AnalyzersFor("go") returns the full
                 Go toolbelt; AnalyzersFor("python") (stub lang) returns ONLY the agnostic set,
                 never empty; Detect() under an all-missing lookPath marks every analyzer
                 !Installed and keeps a non-empty Install hint. Drop a core analyzer from the
                 table → TestCatalogCompleteness fails; break the agnostic fallback →
                 TestAnalyzersForAgnosticFallback fails. Each Dimension is one of the locked
                 quality_scope set (complexity/duplication/dead-code/coupling/test-gap/risky-lint);
                 a cosmetic-only lint in the table fails TestCatalogExcludesCosmetic.

  t-3  Maintainability-risk findings ledger
       files:    internal/quality/findings.go, internal/quality/findings_test.go
       covers:   c-1, c-2
       contract: Finding carries Risk (critical..info contextual maintainability-risk, NOT nominal
                 category), File, Line, Evidence, Refutation. Survived() is false with empty
                 Refutation → TestUnrefutedIsNotSurvivor: a candidate with no refutation never
                 reaches Survivors(). Ledger.Validate() rejects empty/duplicate ids and unknown
                 Risk → TestLedgerDuplicateID, TestLedgerInvalidRisk fail if those guards are
                 negated. Survivors() returns highest-risk-first regardless of input order →
                 TestSurvivorsRiskOrder feeds a low-then-critical ledger and asserts critical is [0].
                 Load() on garbled TOML returns an error and never panics → TestLoadMalformed.

  t-7  Quality orchestration prompt content
       files:    assets/prompts/quality.md, assets/commands/dross-quality.md
       covers:   c-2, c-3, c-4, c-5
       contract: quality_prompt_test.go (wave 3) content-gates these phrases; this task authors
                 them. (Listed wave 1 because writing the markdown needs no Go output — only the
                 test that guards it is wave 3.) Must contain: "refute"/"majority vote"/"drop"
                 (c-2); "calibrate"/"downrank"/"never suppress" AND the code-only-sweep note that
                 the tool sweep reads no .dross planning artifacts (context_model); "no --fix"/
                 "never edit" application code (c-5); "propose-then-ask before locking" (c-4);
                 "tool-coverage manifest" gate continue/install (c-3). Command shim @-includes
                 ~/.claude/dross/prompts/quality.md with read-only-plus-Write/AskUserQuestion tools.

Wave 2 (depends t-2, t-3)
  t-4  Tool-coverage manifest + language recon
       files:    internal/quality/recon.go, internal/quality/recon_test.go
       covers:   c-3
       contract: DetectLanguages walks a tree, returns sorted de-duped langs, never descends
                 .dross/.git/node_modules/vendor, ignores unknown extensions → TestDetectLanguages
                 on a fixture with main.go + a .dross/x.go asserts ["go"] and proves .dross is
                 skipped. BuildManifest records BOTH ran (installed) and skipped (missing) analyzers
                 → TestManifestRecordsSkipped under all-missing lookPath has len(Ran())==0 and
                 len(Skipped())>0; if Skipped() silently dropped missing tools a thin toolbelt would
                 read "all clear" and this fails. (Depends t-2 for AnalyzersFor / Detect.)

  t-5  Risk-ordered remediation scaffold writer
       files:    internal/quality/scaffold.go, internal/quality/scaffold_test.go
       covers:   c-4
       contract: ScaffoldSpec emits exactly one criterion per SURVIVING finding, highest-risk
                 first → TestScaffoldOnePerFinding (count) + TestScaffoldRiskOrder (low-before-
                 critical ledger ⇒ criterion[0] cites the critical's id). Each criterion text
                 cites its findings.toml ledger id → TestScaffoldCitesLedger, so no finding hides
                 behind a tier-level test. Zero survivors → ScaffoldSpec returns an error and writes
                 nothing → TestScaffoldEmptyRefuses. Emitted spec.toml round-trips through
                 phase.LoadSpec → TestScaffoldRoundTrips. (Depends t-3 for Ledger/Survivors.)

Wave 3 (depends t-1, t-2, t-4, t-5, t-7)
  t-6  `dross quality {detect,run,scaffold}` command + containment
       files:    internal/cmd/quality.go, internal/cmd/quality_test.go, cmd/dross/main.go, .gitignore
       covers:   c-1, c-3, c-5
       contract: Quality() registers detect/run/scaffold → TestQualityCommandRegistered.
                 `quality detect <dir>` prints a scanners section with installed/missing markers →
                 TestQualityDetectOutput. `quality run .` succeeds with ZERO analyzers installed
                 (partial coverage, no hard-error) and writes report.md under one .dross/quality/
                 run dir → TestQualityRunCreatesDir. containedPath refuses "../main.go" and deep
                 traversal, accepts "report.md" → TestQualityRunReadOnly; a full run touches only
                 paths under .dross/quality/ (walk asserts the prefix) — proves c-5 at the CLI.
                 scaffold on a valid ledger writes spec.toml; on a zero-survivor ledger returns an
                 error → TestQualityScaffold / TestQualityScaffoldEmptyErrors. .gitignore adds
                 ".dross/quality/" → TestQualityArtifactsGitignored runs `git check-ignore
                 .dross/quality/x/report.md` and fails if not ignored.
                 (Depends t-1/t-2/t-4/t-5 for the wired package; main.go registers Quality().)

  t-8  Prompt-content assertions + commands/prompts parity
       files:    internal/cmd/quality_prompt_test.go
       covers:   c-2, c-4, c-5
       contract: For each locked prompt rule a SEPARATE sub-test fails if its phrase is absent
                 from a normalised (lowercased, backtick/emphasis-stripped) quality.md:
                 {"c-2 refute majority-vote drop": refute, majority vote, drop},
                 {"context calibrate-only downrank": calibrate, downrank, never suppress},
                 {"c-5 read-only no --fix": no --fix, never edit},
                 {"c-4 propose-then-ask": propose-then-ask before locking},
                 {"c-3 tool-coverage gate": tool-coverage manifest}.
                 Removing any one mandated section from quality.md fails exactly that sub-test.
                 The existing TestCommandsPromptsParity already guards that dross-quality.md and
                 quality.md co-exist 1:1 — adding the command without the prompt (or vice versa)
                 fails parity, so no extra parity task is needed.
```

## Coverage

| criterion | delivered by | guarding tests |
| --------- | ------------ | -------------- |
| c-1 run-dir/run-id, finding carries rank+location+evidence | t-1, t-3, t-6 | TestNewRunNoClobber, RunID format; Finding has Risk/File/Line/Evidence; TestQualityRunCreatesDir |
| c-2 refute-panel majority-vote drop, no LLM-guesses | t-3, t-7, t-8 | TestUnrefutedIsNotSurvivor (code: unrefuted never surfaces); quality.md "refute/majority vote/drop" sub-test |
| c-3 triage detect→map→report installed/missing→gate→sweep→manifest | t-2, t-4, t-6, t-7 | TestCatalogCompleteness, TestAnalyzersForAgnosticFallback, TestManifestRecordsSkipped, TestQualityDetectOutput; quality.md gate phrasing |
| c-4 scaffold one-criterion-per-finding, risk-ordered, ledger-cited, propose-then-ask | t-5, t-7, t-8 | TestScaffoldOnePerFinding, TestScaffoldRiskOrder, TestScaffoldCitesLedger, TestScaffoldEmptyRefuses, TestScaffoldRoundTrips; quality.md "propose-then-ask before locking" |
| c-5 read-only, no --fix, only gitignored artifacts + gated scaffold | t-6, t-7, t-8 | TestQualityRunReadOnly (containment + run-touches-only-.dross/quality), TestQualityArtifactsGitignored; quality.md "no --fix/never edit" |

Every criterion c-1..c-5 has at least one CODE contract OR a prompt-content contract,
and the c-2/c-3/c-4/c-5 split deliberately carries both a code half and a prompt half
where the locked `testable_surface` decision divides the work.

## Judgment calls

- Split the ledger (t-3) and the scaffold (t-5) into separate tasks rather than one
  "findings" task: chose isolation so TestSurvivorsRiskOrder (ledger ordering) and
  TestScaffoldRiskOrder (criterion ordering) guard *different* surfaces — rejected the
  merged task because a single test couldn't distinguish a ledger-sort bug from a
  scaffold-sort bug.
- Authored quality.md (t-7) in wave 1, not alongside its test (t-8) in wave 3: chose to
  decouple because the markdown needs no Go output, so it parallelises with the unit
  work; only the assertion that guards it (t-8) must wait. Rejected pairing prompt+test
  in one wave-3 task as needless serialisation.
- Mirrored the contextual-risk ledger as maintainability-Risk, NOT a copy of security's
  exploitability-Severity: chose Risk per the locked `ranking_model` so the type name and
  field make the "blast-radius-weighted, not nominal category" contract testable
  (TestSurvivorsRiskOrder feeds a cold-path-critical vs hot-path-low case). Rejected reusing
  the `Severity` name to keep the quality semantics honest.
- Put the calibrate-only/downrank context rule entirely in the prompt half (t-7/t-8), not
  the Go catalog: chose this because the locked `context_model` says the *tool sweep* reads
  no .dross artifacts (code-only) while only the *judgment panel* may calibrate — that's an
  orchestration rule the prompt owns; a Go-side context reader would contradict the locked
  code-only-sweep boundary. Rejected adding any .dross-reading code path.
- Folded the .gitignore edit + main.go registration into the CLI task (t-6) instead of a
  standalone task: chose merge because each is a one-line change with no independent test of
  its own beyond what t-6's TestQualityArtifactsGitignored / TestQualityCommandRegistered
  already assert — a separate task would be sub-10-min and below the granularity floor.
- Reused the existing TestCommandsPromptsParity for command/prompt 1:1 rather than writing a
  quality-specific parity test: chose reuse because that test already scans the whole
  assets/ tree, so dross-quality.md without quality.md fails it automatically. Rejected a
  redundant parity assertion.
```

verification: 8 tasks across 3 waves, criteria covered 5/5
