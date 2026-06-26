# MVP draft — 05-dross-secure

Lens: smallest task set that makes every criterion testable. One Go package
(`internal/security`) owns all three deterministic concerns; one cmd file wires
the `dross security` subcommand tree; two prompt files carry the orchestration
criteria. No speculative packages, no per-concern split that a criterion
doesn't force.

```
Phase 05-dross-secure — 5 tasks across 2 waves

Wave 1
  t-1  Add run-dir + run-id creation
       files:    internal/security/run.go
                 internal/security/run_test.go
                 .gitignore
       covers:   c-1, c-6
       contract: if run-id format drifts, run_test.go fails asserting
                 run-id matches <timestamp>-<short-sha> and the dir is
                 .dross/security/<run-id>/; if .gitignore omits
                 .dross/security/, run_test.go's gitignore-line assertion
                 fails (proves artifacts are gitignored, read-only posture).

  t-2  Add scanner catalog + availability detection
       files:    internal/security/scanners.go
                 internal/security/scanners_test.go
       covers:   c-4
       contract: if the Go catalog or agnostic-tool set (gitleaks, semgrep,
                 trivy) loses an entry, scanners_test.go fails the
                 catalog-membership assertion; if availability detection
                 stops splitting installed-vs-missing (PATH-stubbed), the
                 missing-with-install-instructions assertion fails.

  t-3  Add findings.toml -> spec.toml scaffold writer
       files:    internal/security/scaffold.go
                 internal/security/scaffold_test.go
       covers:   c-5
       contract: a findings.toml with two findings (high, low) produces a
                 spec.toml with two acceptance criteria, criticals-first
                 severity order, one criterion per finding id; if ordering or
                 1:1 finding->criterion mapping breaks, scaffold_test.go fails.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Wire `dross security` subcommand tree
       files:    internal/cmd/security.go
                 internal/cmd/security_test.go
                 cmd/dross/main.go
       covers:   c-1, c-4, c-5
       contract: `dross security run-init` creates the run dir and prints the
                 run-id; `dross security scan-plan` prints installed-vs-missing;
                 `dross security scaffold <phase>` writes spec.toml from
                 findings.toml. If any subcommand is unregistered or its RunE
                 stops calling the internal/security function, security_test.go
                 (cobra-invoked) fails; commands_parity / subcommand_guard
                 catch an unregistered verb.

  t-5  Write secure prompt + command deliverables
       files:    assets/prompts/secure.md
                 assets/commands/dross-secure.md
                 internal/cmd/commands_parity_test.go
       covers:   c-1, c-2, c-3, c-6
       contract: a content-assertion test (extend commands_parity_test.go)
                 fails if secure.md drops the refute-panel/majority-vote step
                 (c-2), the context-free clause naming code+manifests+entry-
                 points-only (c-3), the severity-ranked report.md +
                 run-id+findings.toml handoff to the CLI (c-1), or the
                 read-only / no --fix / no-app-edit clause (c-6); dross-secure.md
                 must exist and pair with the prompt (parity test).
```

## Coverage

| criterion | tasks            |
| --------- | ---------------- |
| c-1       | t-1, t-4, t-5    |
| c-2       | t-5              |
| c-3       | t-5              |
| c-4       | t-2, t-4         |
| c-5       | t-3, t-4         |
| c-6       | t-1, t-5         |

All of c-1..c-6 covered.

## Judgment calls

- One `internal/security` package (run.go + scanners.go + scaffold.go) instead
  of three packages — chose a single package because all three are the "thin
  slice" the locked `testable_surface` names together; three packages add
  import surface no criterion needs.
- Three Wave-1 files but one package, split by concern into separate tasks —
  chose to keep t-1/t-2/t-3 as distinct tasks (each its own _test.go, each a
  distinct criterion) rather than one mega-task, because a 3-criterion 6-file
  task violates the >5-file / >2-layer split rule; they stay parallel (no
  inter-dependency) in Wave 1.
- Folded `.gitignore` into t-1 rather than a standalone task — the gitignore
  line is the read-only/no-disclosure guarantee for the run dir t-1 creates;
  it's <10 min and traces to the same surface, so it merges.
- c-6 verified as prompt assertion (t-5) + gitignore assertion (t-1), no Go
  "guard" that blocks writes — rejected building an enforcement layer because
  read-only is a property of *not* shipping a --fix path, not something a
  criterion asks the CLI to actively police; a guard would be speculative.
- Subcommand verbs `run-init` / `scan-plan` / `scaffold` kept minimal and
  mechanical — rejected a richer findings-state CLI (explicitly deferred in
  spec.toml); these three are exactly what c-1/c-4/c-5 need to test.
- Refute-panel / context-free / majority-vote (c-2, c-3) live only in the
  prompt with content-assertion tests — rejected any Go modeling of the panel
  because the locked decision assigns orchestration to the prompt, and c-2/c-3
  are named as prompt assertions.
