# MVP lens — 06-dross-quality task decomposition

Bias: smallest task set that satisfies every criterion. Every task traces to a
criterion; no per-file vanity tasks. The security mirror shipped 9 tasks (one per
`internal/security/*.go` file plus cmd/prompt/gitignore). MVP collapses the
cohesive Go domain package into criterion-aligned tasks and folds the gitignore +
sandbox into the tasks the criteria already force, dropping from 9 to 5.

```
Phase 06-dross-quality — 5 tasks across 2 waves

Wave 1
  t-1  Quality run-dir, manifest, analyzer catalog
       files:    internal/quality/run.go, internal/quality/run_test.go,
                 internal/quality/catalog.go, internal/quality/catalog_test.go,
                 internal/quality/recon.go, internal/quality/recon_test.go
       covers:   c-1, c-3
       contract: run_test.go: RunID(t, "") yields "<ts>-nogit" and a second NewRun on
                 the same second+sha gets a "-2" suffix instead of clobbering — break
                 the suffix-collision loop and TestNewRunNoClobber fails.
                 catalog_test.go: AnalyzersFor("go") returns the Go complexity/
                 duplication/dead-code/coupling/test-gap analyzers AND every agnostic
                 one; drop an agnostic analyzer from the table and TestAnalyzersForGo
                 fails. Detect with a fake lookPath that returns "not found" marks the
                 analyzer missing AND preserves its Install hint — blank the hint and
                 TestDetectKeepsInstallHint fails.
                 recon_test.go: BuildManifest over a fixture tree with a .go file lists
                 "go" in Languages and partitions Ran/Skipped by the injected lookPath;
                 make BuildManifest descend .dross/ and TestReconSkipsDross fails.

  t-2  Quality findings ledger + maintainability-risk ranking
       files:    internal/quality/findings.go, internal/quality/findings_test.go
       covers:   c-1, c-2
       contract: findings_test.go: a Finding with empty Refutation reports Survived()
                 == false (refute-panel gate) and is excluded from Survivors() — remove
                 the TrimSpace(Refutation) check and TestUnrefutedDropped fails.
                 Survivors() returns survivors sorted highest-risk-first by the Risk
                 rank map; swap two risk ranks and TestSurvivorsRiskOrder fails.
                 Validate() rejects a duplicate finding id and an unknown risk value;
                 remove the dup-id guard and TestLedgerRejectsDupID fails. Load() on
                 malformed TOML returns an error, never panics — feed garbage and
                 TestLoadMalformedErrors fails.

  t-3  Findings→remediation-spec scaffold writer
       files:    internal/quality/scaffold.go, internal/quality/scaffold_test.go
       covers:   c-4
       contract: scaffold_test.go: ScaffoldSpec emits one criterion per surviving
                 finding, highest-risk-first, each citing its ledger id; a ledger with
                 zero survivors returns an error (no vacuous phase) — remove the
                 empty-survivors guard and TestScaffoldRefusesEmpty fails. Round-trip:
                 WriteScaffoldSpec output reloads via phase.LoadSpec with N criteria for
                 N survivors; drop a criterion and TestScaffoldCriterionPerFinding fails.

Wave 2 (depends t-1, t-2, t-3)
  t-4  dross quality CLI + gitignore sandbox
       files:    internal/cmd/quality.go, internal/cmd/quality_test.go,
                 cmd/dross/main.go, .gitignore
       covers:   c-1, c-3, c-4, c-5
       contract: quality_test.go: `quality run` creates .dross/quality/<id>/ and writes
                 report.md containing the tool-coverage manifest (ran vs skipped) — break
                 writeRunReport and TestQualityRunWritesManifest fails. `quality scaffold`
                 with a finding-derived name like "../x" is refused by containedPath —
                 remove the escape guard and TestScaffoldPathEscapeRefused fails.
                 A behavioural `git check-ignore .dross/quality/x/report.md` exits 0;
                 remove the .gitignore line and TestQualityArtifactsGitignored fails.
                 cmd/dross/main.go registers cmd.Quality(); drop it and the CLI has no
                 quality verb (TestQualityCommandRegistered via root command lookup).

  t-5  quality.md + dross-quality.md prompt orchestration
       files:    assets/prompts/quality.md, assets/commands/dross-quality.md
       covers:   c-2, c-3, c-4, c-5
       contract: quality_prompt_test.go (in internal/cmd): content-gates quality.md for
                 "refute"/"majority vote"/"drop" (c-2), the detect→plan→gate→sweep
                 tool-coverage flow + install instructions (c-3), "propose-then-ask
                 before locking" (c-4), and "no --fix"/"never edit" + downrank-only
                 context calibration (c-5); remove any mandated section and exactly that
                 sub-test fails. The existing TestCommandsPromptsParity fails if either
                 quality.md or dross-quality.md is missing its pair.
```

## Coverage

| Criterion | Delivered by | Notes |
| --- | --- | --- |
| c-1 (impact-ranked report at .dross/quality/<run>/report.md; rank+location+evidence) | t-1, t-2, t-4 | run-id/run-dir (t-1), Finding fields + risk rank (t-2), report.md writer (t-4) |
| c-2 (no LLM-guessed findings; refute-panel majority-vote drop) | t-2, t-5 | Survived() gate (t-2, deterministic); panel orchestration + drop rule (t-5, prompt) |
| c-3 (triage langs→analyzers, installed-vs-missing+install, gate, coverage manifest) | t-1, t-4, t-5 | catalog+recon+manifest (t-1), detect/run cmd + manifest in report (t-4), gate flow (t-5) |
| c-4 (scaffold remediation phase; per-finding criterion; risk wave order; propose-then-ask) | t-3, t-4, t-5 | scaffold writer (t-3), scaffold subcommand (t-4), propose-then-ask gate (t-5) |
| c-5 (read-only; no --fix; only gitignored artifacts + gated scaffold) | t-4, t-5 | containedPath sandbox + .gitignore (t-4), no-fix/never-edit rules (t-5) |

Every criterion c-1..c-5 is covered. No task exists without a criterion forcing it.

## Judgment calls

- Collapsed security's run.go + catalog.go + recon.go into one task (t-1): all three
  are read-by-`detect`/`run` and share the manifest assembly; splitting them per-file
  buys no parallelism (same wave, same package) and 3 sub-10-min tasks. Rejected the
  per-file split because nothing forces it — it was mirror-shape, not criterion-driven.
  Kept it under the 6-file ceiling (6 files, 1 layer).
- Kept findings.go (t-2) and scaffold.go (t-3) as separate tasks rather than merging
  into t-1: findings carries the c-2 refute gate + c-1 ranking, scaffold is the c-4
  writer; they're distinct test surfaces and t-3 depends on t-2's Ledger type. Merging
  would make one task span both the ledger contract and the spec-writer contract —
  two surfaces, harder to gate cleanly. Rejected the merge.
- Folded gitignore (its own task + Go file in the mirror... actually mirror had no Go
  file, just a test) into t-4 rather than a standalone task: the .gitignore line is a
  one-line edit and its only test (`git check-ignore`) naturally belongs with the CLI
  task that creates the artifacts it guards. A standalone gitignore task would be the
  exact "< 10 min, one file" merge target the rules forbid. Rejected standalone.
- Folded `cmd/dross/main.go` registration into t-4 rather than its own wiring task:
  one-line AddCommand edit, no independent criterion. Rejected standalone.
- Put the CLI (t-4) in wave 2 depending on t-1/t-2/t-3: the subcommands call
  quality.BuildManifest, quality.Load, quality.WriteScaffoldSpec — it strictly needs
  their types/functions to compile. The prompt (t-5) is pure-asset and could be wave 1,
  but I placed it in wave 2 so its content-gate test can reference the same `quality`
  verb names the CLI establishes; this is a soft dependency. Considered dropping t-5 to
  wave 1 for parallelism — rejected because the prompt must name the exact subcommands
  (`dross quality run/detect/scaffold`) t-4 defines, and authoring them against a
  not-yet-existing CLI risks drift the parity test won't catch (it checks file pairing,
  not subcommand names).
- Did NOT add a dedicated "context-model calibration" task: the downrank-only context
  rule (locked context_model) is a prompt instruction, not deterministic code, so it
  lives inside t-5's content gate. A separate task for it would have no code surface.
