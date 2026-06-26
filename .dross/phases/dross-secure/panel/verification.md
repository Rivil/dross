# Phase 05-dross-secure — verification-lens plan

Lens: design backward from test contracts. For each criterion I first state the
*ideal* test contract, then derive the smallest task that makes that contract
satisfiable. The split is explicit per the locked `testable_surface` decision:

- **Go-tested (real unit tests):** c-1 (run-dir/run-id), c-4 (scanner detect/gate),
  c-5 (findings.toml → spec.toml scaffold writer). These get `internal/secure` + a
  thin `dross security` command and `_test.go` assertions.
- **Prompt-asserted (content assertions on secure.md):** c-2 (refute-panel),
  c-3 (context-free audit), c-6 (read-only / no --fix). Verified the way
  `commands_parity_test.go` already asserts prompt content — by grepping the
  installed `assets/prompts/secure.md` for mandated sections.

Severity ordering (critical→low) is a pure function in the Go layer so it is
unit-testable from a `findings.toml` fixture rather than left to the LLM.

---

## Ideal test contracts (derived first)

- **c-1** — `secure.RunID(ts, sha)` returns `<timestamp>-<short-sha>`; `secure.NewRun(root, ts, sha)`
  creates `.dross/security/<run-id>/` and seeds empty `report.md` + `findings.toml`.
  Ideal test: feed a fixed timestamp + sha, assert the dir path and that both files exist;
  assert a finding written to `findings.toml` round-trips with its severity + refutation fields.
- **c-4** — `secure.Detect(root)` returns languages from the codebase; `secure.Plan(langs)`
  maps each to its scanner set (+ agnostic: gitleaks/semgrep/trivy); `secure.Availability(plan)`
  partitions installed vs missing with install hints. Ideal test: a Go-only fixture tree yields
  the Go catalog + agnostic tools; a missing scanner appears in `Missing` with a non-empty install
  hint; a coverage manifest lists every planned tool with installed=true/false.
- **c-5** — `secure.Scaffold(findings)` emits a `phase.Spec` whose criteria are one-per-finding,
  ordered critical→low, each carrying a `findings.toml` ledger id. Ideal test: a findings fixture
  in low,critical,medium order yields criteria in critical,medium,low order; criterion count ==
  finding count; each criterion text references its ledger id.
- **c-2 / c-3 / c-6** — content assertions on `assets/prompts/secure.md` (mandated sections present),
  mirroring `commands_parity_test.go`.

---

## Plan

```
Phase 05-dross-secure — 7 tasks across 3 waves

Wave 1
  t-1  Add secure run-dir + findings ledger
       files:    internal/secure/secure.go
                 internal/secure/secure_test.go
       covers:   c-1
       contract: secure.RunID("20260620-1200","abc1234") not returning
                 "20260620-1200-abc1234" fails TestRunID; NewRun not creating
                 .dross/security/<run-id>/report.md + findings.toml fails
                 TestNewRunCreatesArtifacts; a Finding with severity+refutation
                 written then reloaded that drops either field fails the
                 round-trip assertion in TestFindingsRoundTrip.

  t-2  Add scanner catalog + availability detect
       files:    internal/secure/scanners.go
                 internal/secure/scanners_test.go
       covers:   c-4
       contract: Detect over a Go-only fixture omitting "go" fails
                 TestDetectGo; Plan(["go"]) missing gitleaks/semgrep/trivy in
                 the agnostic set fails TestPlanAgnosticTools; Availability
                 marking a stubbed-missing scanner as installed, or giving it an
                 empty install hint, fails TestAvailabilityMissingHasHint; the
                 coverage manifest omitting any planned tool fails
                 TestCoverageManifestListsAll.

  t-3  Add severity-ordered scaffold writer
       files:    internal/secure/scaffold.go
                 internal/secure/scaffold_test.go
       covers:   c-5
       contract: Scaffold over findings in [low,critical,medium] order not
                 emitting criteria in [critical,medium,low] order fails
                 TestScaffoldSeverityOrder; criterion count != surviving-finding
                 count fails TestScaffoldOnePerFinding; a criterion whose text
                 omits its findings.toml ledger id fails
                 TestScaffoldCriterionCitesLedger.

  t-4  Author secure.md orchestration prompt
       files:    assets/prompts/secure.md
       covers:   c-2, c-3, c-6
       contract: secure.md missing a "## Refute panel" section (or the phrase
                 "majority vote") fails TestSecurePromptHasRefutePanel;
                 missing the "context-free" / "does not read .dross/ planning"
                 directive fails TestSecurePromptContextFree; missing the
                 "read-only" / "no --fix" / "never edits application code"
                 directive fails TestSecurePromptReadOnly. (Assertions land in
                 t-7.)

  t-5  Gitignore the security run artifacts
       files:    .gitignore
       covers:   c-6
       contract: .gitignore not matching ".dross/security/" fails
                 TestSecurityArtifactsGitignored (which shells `git check-ignore
                 .dross/security/x/report.md` and asserts it is ignored).

Wave 2 (depends t-1, t-2, t-3)
  t-6  Wire `dross security` command
       files:    internal/cmd/security.go
                 internal/cmd/security_test.go
                 cmd/dross/main.go
       covers:   c-1, c-4, c-5
       contract: `dross security` absent from root.AddCommand fails the
                 existing subcommand-guard/parity coverage and
                 TestSecurityCommandRegistered; `dross security detect` not
                 printing the installed-vs-missing manifest fails
                 TestSecurityDetectOutput; `dross security run` not creating the
                 run dir under .dross/security/ fails TestSecurityRunCreatesDir;
                 `dross security scaffold <findings.toml>` not writing a
                 severity-ordered spec.toml fails TestSecurityScaffoldWrites.

Wave 2 (depends t-4)
  t-7  Add dross-secure command shim + prompt assertions
       files:    assets/commands/dross-secure.md
                 internal/cmd/secure_prompt_test.go
       covers:   c-2, c-3, c-6
       contract: assets/commands/dross-secure.md absent breaks
                 TestCommandsPromptsParity (command without matching prompt);
                 the new test asserts secure.md contains the refute-panel,
                 context-free, and read-only sections — removing any one section
                 from secure.md fails the corresponding sub-assertion in
                 secure_prompt_test.go.
```

---

## Coverage

| criterion | tasks | how verified |
| --------- | ----- | ------------ |
| c-1 | t-1, t-6 | Go unit tests on RunID/NewRun/findings round-trip + command run-dir creation |
| c-2 | t-4, t-7 | content assertion: secure.md refute-panel + majority-vote section |
| c-3 | t-4, t-7 | content assertion: secure.md context-free directive |
| c-4 | t-2, t-6 | Go unit tests on Detect/Plan/Availability/manifest + `security detect` output |
| c-5 | t-3, t-6 | Go unit tests on severity-ordered, one-per-finding scaffold + `security scaffold` |
| c-6 | t-4, t-5, t-7 | content assertion (no --fix) + `git check-ignore` on .dross/security/ |

All of c-1..c-6 covered (6/6). Every criterion has at least one *specific*
failing-test contract; the prompt-shaped criteria (c-2/c-3/c-6) are pinned by
content assertions exactly as `commands_parity_test.go` pins existing prompts.

---

## Judgment calls

- **Severity ordering lives in Go, not the prompt.** Chose a pure `Scaffold`
  function so critical→low ordering is unit-testable from a fixture; rejected
  leaving ordering to the LLM in secure.md, which would leave c-5's ordering
  guarantee with only a weak content assertion. The locked `criterion_unit`
  decision (severity drives wave order, one criterion per finding) is mechanical
  and deserves a real test.
- **Split scanner catalog into its own file/test (t-2), separate from run-dir
  (t-1).** Chose two wave-1 tasks because they are independent surfaces (c-4 vs
  c-1) and bundling them would exceed the 2-layer rule and blur which test guards
  which criterion; rejected one fat `secure.go` task.
- **c-6 needs two tasks, not one.** The "no --fix / read-only" half is a prompt
  guarantee (t-4/t-7 content assertion) but the "only gitignored artifacts" half
  is a filesystem fact best pinned by `git check-ignore` (t-5). Chose to split so
  each half has its own observable contract; rejected folding gitignore into the
  command task where it would have no dedicated test.
- **Prompt assertions live in a new `secure_prompt_test.go`, not appended to
  `commands_parity_test.go`.** Chose a dedicated test file so each mandated
  section (refute-panel, context-free, read-only) is an individually-failing
  sub-assertion; the parity test only guards command↔prompt existence, which
  t-7 still relies on for the shim. Rejected overloading the parity test.
- **`dross security` is one wave-2 command with three subcommands (detect / run
  / scaffold), not three commands.** Mirrors the existing verb model
  (`changes record/show`, `task next/show/status`); each subcommand maps to a
  wave-1 library surface so its test gates a distinct criterion. Rejected three
  top-level commands as inconsistent with the repo's command tree.
- **Catalog ships Go-complete + agnostic only (locked `catalog_scope`).** The
  test asserts Go gets its dedicated set and non-Go languages receive only the
  agnostic tools with their catalogs marked stub — encodes the locked Go-first
  non-goal as a test, not just a comment.

verification: 7 tasks across 3 waves, criteria covered 6/6
