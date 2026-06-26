# Risk-lens decomposition — Phase 05-dross-secure

Lens: failure modes drive the graph. Each break (run-id collision, missing
scanner, malformed scanner output, invalid scaffold TOML, leaking .dross/
context, accidental writes outside the run dir) is owned and tested by exactly
one task.

```
Phase 05-dross-secure — 8 tasks across 3 waves

Wave 1
  t-1  Create run-dir + collision-safe run-id
       files:    internal/security/run.go
                 internal/security/run_test.go
       covers:   c-1, c-6
       contract: if a second run on the same commit reuses the run-id,
                 run_test.go's same-commit-twice case fails (second run must
                 get a unique suffix, not clobber the first dir); if NewRun
                 writes anywhere outside .dross/security/<run>/, the
                 write-boundary assertion fails; if a short-sha can't be read
                 (no git / detached), it falls back to "nogit" rather than
                 erroring.

  t-2  Scanner catalog + availability detection
       files:    internal/security/catalog.go
                 internal/security/catalog_test.go
       covers:   c-4
       contract: if a core Go-or-agnostic scanner (gitleaks, semgrep, trivy,
                 govulncheck, gosec) is dropped from the catalog, the
                 catalog-completeness test fails; if a missing scanner is
                 reported without its install instruction, the
                 missing-has-install-hint test fails; if a non-Go language
                 (e.g. python) yields zero applicable agnostic tools instead
                 of the gitleaks/semgrep/trivy set, the agnostic-fallback
                 test fails.

  t-3  Findings ledger schema + malformed-input guard
       files:    internal/security/findings.go
                 internal/security/findings_test.go
       covers:   c-1, c-2
       contract: if a finding with an out-of-range / empty severity is
                 accepted, the severity-validation test fails; if a finding
                 lacking refutation evidence is treated as survived, the
                 unrefuted-dropped test fails; if Load on a truncated/garbled
                 findings.toml panics instead of returning an error, the
                 malformed-ledger test fails.

Wave 2 (depends t-1, t-2)
  t-4  Language detection + tool-coverage manifest
       files:    internal/security/recon.go
                 internal/security/recon_test.go
       covers:   c-3, c-4
       contract: if detection reads any .dross/ planning file (spec/rules/
                 goals) the context-free test fails (fixture has a planted
                 .dross/ — manifest must ignore it); if the manifest omits a
                 detected-but-uninstalled scanner (records only ran tools),
                 the coverage-manifest-completeness test fails; if an unknown
                 file extension crashes detection instead of being ignored,
                 the unknown-ext test fails.

Wave 3 (depends t-1, t-2, t-3, t-4)
  t-5  Findings→spec.toml + findings.toml scaffold writer
       files:    internal/security/scaffold.go
                 internal/security/scaffold_test.go
       covers:   c-5, c-1
       contract: if the emitted spec.toml fails to round-trip through
                 phase.LoadSpec, the valid-spec test fails; if two findings
                 collapse into one criterion (not one-criterion-per-finding),
                 the per-finding-criterion test fails; if criteria/waves are
                 not emitted criticals-first, the severity-order test fails;
                 if a finding id in spec.toml has no matching findings.toml
                 ledger entry, the ledger-linkage test fails; if the writer
                 emits with zero surviving findings, the empty-scaffold guard
                 test fails (refuses rather than writing a vacuous phase).

  t-6  Wire `dross security` subcommand
       files:    internal/cmd/security.go
                 internal/cmd/security_test.go
                 cmd/dross/main.go
       covers:   c-1, c-4, c-6
       contract: if `dross security` is not registered on the root command,
                 the subcommand-known/parity test fails; if the run command
                 ever writes to a path outside .dross/security/ (e.g. touches
                 app source), the read-only-boundary test fails; if a run with
                 no available scanners hard-errors instead of proceeding with
                 partial coverage + manifest, the partial-coverage test fails.

  t-7  Author secure.md + dross-secure.md prompts
       files:    assets/prompts/secure.md
                 assets/commands/dross-secure.md
       covers:   c-2, c-3, c-5, c-6
       contract: if commands/prompts parity breaks (one of the pair missing),
                 TestCommandsPromptsParity fails; if secure.md omits the
                 refute-panel majority-vote drop rule, the prompt-content
                 assertion for c-2 fails; if it omits the "read no .dross/
                 planning artifacts" instruction, the c-3 assertion fails; if
                 it omits "no --fix / never edit or commit app code", the c-6
                 assertion fails; if it omits "propose-then-ask before
                 locking" the scaffold, the c-5 assertion fails.

  t-8  Gitignore + content-assertion harness for prompts
       files:    .gitignore
                 internal/cmd/secure_prompt_test.go
       covers:   c-6, c-2, c-3, c-5
       contract: if .dross/security/ is not gitignored, the
                 run-artifacts-ignored test fails (raw findings must not be
                 committable on this public repo); if the secure.md content
                 assertions (refute-panel, context-free, read-only,
                 propose-then-ask) are not exercised by a real test, the
                 harness-presence check fails.
```

## Coverage

- c-1 (severity-ranked report.md, run-id = <timestamp>-<short-sha>, refutation evidence per finding): t-1, t-3, t-5, t-6
- c-2 (no LLM-guessed findings; refute-panel majority vote drops failures): t-3, t-7, t-8
- c-3 (context-free; reads no .dross/ planning artifacts): t-4, t-7, t-8
- c-4 (pre-sweep triage: detect langs → map scanners → installed-vs-missing + install hints → gate → coverage manifest): t-2, t-4, t-6
- c-5 (scaffold remediation phase: per-finding criteria, findings.toml ledger, severity drives wave order, propose-then-ask): t-5, t-7, t-8
- c-6 (read-only; no --fix; only gitignored run artifacts + gated scaffold): t-1, t-6, t-7, t-8

All criteria c-1..c-6 covered.

## Judgment calls

- Split run-dir (t-1), catalog (t-2), and ledger (t-3) into three wave-1
  tasks rather than one `internal/security` mega-package task: each owns a
  distinct failure surface (collision/boundary, missing-scanner, malformed
  input) so a contract maps to exactly one task. Rejected the single-package
  task — its contract would have to name three unrelated breaks, violating
  one-risk-one-owner.
- Made the scaffold writer (t-5) its own wave-3 task depending on the ledger
  (t-3) and recon/catalog, not folded into the CLI wiring (t-6). The invalid-
  spec.toml risk (round-trip + per-finding + severity-order + ledger-linkage)
  is the single highest-consequence break in this phase (it produces the
  committed public artifact) and deserves an isolated owner. Rejected merging
  into t-6, which would bury spec-validity tests under CLI plumbing.
- Separated prompt content (t-7) from the content-assertion harness + gitignore
  (t-8). The prompt is verified by content assertions per the locked
  testable_surface; putting the test in its own task keeps the c-2/c-3/c-6
  "no leaks / read-only / refute-panel" guarantees enforced by code even
  though the prompt itself isn't executable. Rejected trusting parity alone —
  parity proves the file exists, not that it says the right things.
- Put detection (t-4) in wave 2 depending on the catalog (t-2): mapping
  detected languages to scanners needs the catalog's data. Kept it out of
  wave 1 because the context-free + coverage-manifest risks genuinely consume
  catalog output. The unknown-extension and planted-.dross/ guards live here
  because this is the one task that walks the target tree.
- Chose to validate emitted spec.toml via the real `phase.LoadSpec`
  round-trip (t-5) rather than a bespoke parser, matching rule r-06 (check the
  reference before generating a structured artifact) and reusing the project's
  own decoder so a schema drift can't pass a hand-rolled check.
