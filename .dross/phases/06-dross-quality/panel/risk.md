# Risk-lens decomposition — 06-dross-quality

Lens: failure modes drive the graph. The deterministic Go surface is split so each
distinct way a run can break — run-id collision, write-boundary escape, malformed
ledger, duplicate ids, invalid risk rank, empty survivors, zero-tool coverage, a
missing-tool gate that silently reads as clean, a finding-derived path that escapes
the sandbox, artifacts that aren't actually gitignored, and a prompt that lets an
unrefuted or context-suppressed finding through — is **owned and killed by exactly
one task's test**.

```
Phase 06-dross-quality — 8 tasks across 3 waves

Wave 1
  t-1  Run-dir + run-id with collision guard
       files:    internal/quality/run.go, internal/quality/run_test.go
       covers:   c-1, c-5
       contract: if the same-second/same-sha collision suffixing is removed,
                 TestNewRunCollision fails (second run reuses the first dir); if
                 NewRun writes anything outside .dross/quality/<id>/,
                 TestNewRunWriteBoundary's path-set walk fails; if RunID drops the
                 <timestamp>-<short-sha> shape or the empty-sha→"nogit" fallback,
                 TestRunID fails.

  t-2  Findings ledger: parse + validity guards
       files:    internal/quality/findings.go, internal/quality/findings_test.go
       covers:   c-1, c-2
       contract: if the malformed-TOML guard in Load is dropped,
                 TestLoadMalformedLedger fails (a truncated findings.toml panics or
                 returns nil err instead of an error); if the unique-id constraint
                 is removed, TestLedgerValidateDuplicateID fails; if the invalid /
                 out-of-range maintainability-risk-rank check is removed,
                 TestLedgerValidateRank fails; if Survivors stops requiring
                 refutation evidence, TestSurvivorsRequireRefutation lets an
                 unrefuted candidate through.

  t-3  Risk ranking: order + blast-radius weighting
       files:    internal/quality/risk.go, internal/quality/risk_test.go
       covers:   c-1
       contract: if rank ordering is flipped or blast-radius weighting is dropped,
                 TestRiskOrdering fails — a high-complexity finding on a cold path
                 must sort BELOW a moderate one on a core/hot path, and Survivors()
                 must return highest-risk-first; an unknown rank must sort last,
                 not first (TestRiskUnknownSortsLast).

  t-4  Dimension→analyzer catalog + coverage manifest
       files:    internal/quality/catalog.go, internal/quality/catalog_test.go,
                 internal/quality/recon.go, internal/quality/recon_test.go
       covers:   c-3
       contract: if AnalyzersFor returns nothing for an unknown/stub language,
                 TestAnalyzersForUnknownLangFloor fails (a run must never have zero
                 applicable analyzers — the agnostic set is the floor); if the
                 manifest stops recording skipped/missing analyzers,
                 TestManifestRecordsSkipped fails (a thin toolbelt would read as a
                 clean all-clear); if a detected Go file doesn't map complexity/
                 duplication/dead-code/coupling/test-gap analyzers,
                 TestCatalogGoComplete fails.

Wave 2 (depends t-2, t-3)
  t-5  Scaffold writer: per-finding criteria + empty guard
       files:    internal/quality/scaffold.go, internal/quality/scaffold_test.go
       covers:   c-4
       contract: if the zero-survivor guard is removed,
                 TestScaffoldEmptyLedgerErrors fails (a vacuous remediation phase
                 gets written instead of erroring); if criteria stop being one-per-
                 surviving-finding or stop citing the findings.toml ledger id,
                 TestScaffoldOneCriterionPerFinding fails; if highest-risk-first
                 ordering is lost, TestScaffoldRiskOrdered fails (wave-1 criterion
                 is not the top-risk finding).

Wave 3 (depends t-1, t-4, t-5)
  t-6  `dross quality` CLI + path-traversal sandbox
       files:    internal/cmd/quality.go, internal/cmd/quality_test.go,
                 cmd/dross/main.go
       covers:   c-1, c-3, c-5
       contract: if containedPath stops refusing a finding-derived "../x" name,
                 TestQualityRunReadOnly fails (a traversal path resolves outside the
                 run dir); if `quality run` hard-errors when no analyzers are
                 installed, TestQualityRunPartialCoverage fails (it must proceed
                 with partial coverage, not abort); if any of detect/run/scaffold is
                 unregistered, TestQualityCommandRegistered fails; if a run touches a
                 path outside .dross/quality/, the run_test write-walk fails.

  t-7  Gitignore quality run artifacts (behavioural)
       files:    .gitignore, internal/quality/gitignore_test.go
       covers:   c-5
       contract: if `.dross/quality/` is absent or wrong in .gitignore,
                 TestQualityArtifactsGitignored fails — `git check-ignore
                 .dross/quality/x/report.md` exits non-zero, proving raw findings
                 would be committable.

  t-8  quality.md + dross-quality.md prompt rule gates
       files:    assets/prompts/quality.md, assets/commands/dross-quality.md,
                 internal/cmd/quality_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       contract: TestQualityPromptMandatedSections is one failing sub-assertion per
                 mandated rule — remove the refute-panel "majority vote / drop"
                 wording and the c-2 sub-test fails; remove the downrank-only
                 context boundary ("context can only downrank, never suppress") and
                 the c-2/context sub-test fails; remove the detect→plan→gate→sweep +
                 tool-coverage-manifest wording and the c-3 sub-test fails; remove
                 "propose-then-ask before locking" and the c-4 sub-test fails;
                 remove "no --fix / never edit" and the c-5 sub-test fails. The
                 command↔prompt 1:1 parity is caught by the existing
                 TestCommandsPromptsParity if either file is missing.
```

## Coverage

| Criterion | Delivered by | How the risk is owned |
| --------- | ------------ | --------------------- |
| c-1 (impact-ranked report, run-id = <timestamp>-<short-sha>, rank+location+evidence) | t-1, t-2, t-3, t-6 | run-id shape/collision (t-1), Finding carries rank+file+line+evidence and parses (t-2), ranking honesty (t-3), CLI emits run dir + report (t-6) |
| c-2 (no LLM-guessed findings; refute-panel majority drop) | t-2, t-8 | Survivors require refutation evidence in the ledger (t-2); prompt mandates majority-vote-or-drop + downrank-only context (t-8) |
| c-3 (pre-sweep triage: languages→analyzers, installed-vs-missing+install, gate, run, coverage manifest) | t-4, t-6, t-8 | catalog + manifest with zero-tool floor and skipped-recording (t-4), `quality detect` output + partial-coverage run (t-6), detect→plan→gate→sweep prose (t-8) |
| c-4 (scaffold remediation phase: per-finding criterion, findings.toml ledger, risk drives wave order, propose-then-ask) | t-5, t-8 | per-finding criteria + ledger citation + risk ordering + empty guard (t-5); propose-then-ask gate wording (t-8) |
| c-5 (read-only, no --fix, only gitignored artifacts + gated scaffold) | t-1, t-6, t-7, t-8 | write-boundary on NewRun (t-1), containedPath traversal refusal (t-6), artifacts actually gitignored (t-7), no-fix/never-edit prompt rule (t-8) |

All criteria c-1..c-5 covered.

## Judgment calls

- Split the ledger (t-2) from risk ranking (t-3) into two wave-1 tasks rather than
  one `findings.go`. Chose separation because the ranking_model is its own
  failure surface (cold-path-vs-hot-path inversion) distinct from parse/validity
  guards; rejected merging because one task would then own two unrelated test
  contracts and a regression in either would point at the same file ambiguously.
  In the secure mirror severity lived inside findings.go; here risk is richer
  (blast-radius weighted) and earns its own owner.
- Folded catalog + recon (t-4) into one task across two files. The mirror keeps
  them separate, but the *risk* — "a run with zero applicable analyzers, or a
  thin toolbelt reading as all-clear" — spans both (catalog supplies the set,
  recon builds the manifest), so one owner keeps the missing-tool gate from
  falling between two tasks. Two files / one layer stays within the granularity
  cap; rejected splitting because the coverage-floor invariant has no single home
  if catalog and manifest are owned separately.
- Put t-7 (gitignore) as its own wave-3 task instead of folding it into the CLI
  task. The security mirror has no gitignore.go — the `.dross/security/` line was
  a one-line .gitignore edit guarded purely by a behavioural `git check-ignore`
  test. Chose a dedicated owner so the no-pre-disclosure guarantee (c-5) has an
  unambiguous failing test; rejected merging into t-6 because a CLI regression and
  a gitignore regression would then share one test file and one task status.
- Kept t-6 in wave 3 (depends t-1, t-4, t-5) and t-8 (prompt) also wave 3, but
  did NOT make t-8 depend on the Go tasks — the prompt is content-gated text with
  no code dependency, so it could be wave 1. Placed it in wave 3 only to keep its
  c-3/c-4 wording aligned with the as-built CLI/scaffold surface names; chose
  alignment over maximal parallelism because a prompt that names a flag or command
  the CLI doesn't expose is itself a failure mode. This is the one deliberate
  non-minimal wave placement.
- Made the malformed-ledger guard (t-2), the path-traversal sandbox (t-6), and the
  empty-survivor guard (t-5) each land in a *different* task. They are the three
  classic "bad/partial input" risks; assigning each to exactly one owner with one
  killing test is the core of the risk lens — rejected the tempting consolidation
  of "all input-validation in one hardening task" because that task would span 4
  files and 2 layers and violate the granularity cap.
```
