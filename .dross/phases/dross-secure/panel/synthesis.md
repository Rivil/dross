# Synthesis — Phase 05-dross-secure

Judge over three independent decompositions (risk / mvp / verification). I authored
none of them. Goal: merge the strongest plan and surface the real disagreements
rather than paper over them. All six locked decisions in spec.toml were treated as
non-negotiable; every merged task was checked against them (notes inline).

## Scores

Scoring per dimension, one line per draft. 1 = weak, 5 = strong.

| Draft | criteria coverage | test-contract specificity | granularity | wave correctness |
| ----- | ----------------- | ------------------------- | ----------- | ---------------- |
| risk | 5 — all six covered, c-6 redundantly pinned (gitignore + boundary + prompt); only draft that owns the context-free recon walk (t-4) as a distinct surface | 5 — every contract names the *specific* break and its failing test (collision suffix, write-boundary, empty-scaffold guard, ledger-linkage); sharpest of the three | 4 — finest split (8 tasks); ledger-as-own-task and recon-as-own-task are justified by one-risk-one-owner, but t-8 mixes gitignore + prompt-harness (two surfaces) | 5 — recon correctly gated behind catalog (t-4 dep t-2); scaffold gated behind ledger+recon; clean 3-wave shape |
| mvp | 4 — all six covered but thinly; c-3 (context-free) rests only on a prompt content assertion, no Go recon test exercises the planted-.dross/ ignore | 3 — contracts are correct but coarser ("if ordering breaks … fails"); names fewer distinct failing tests; c-4 availability contract is solid | 5 — tightest defensible split (5 tasks); explicitly rejects speculative packages and a write-guard; folds gitignore into the task that creates the dir it protects | 4 — only 2 waves; correct deps but loses the recon/scaffold ordering nuance by folding recon into the prompt/CLI surface |
| verification | 5 — all six covered; best criterion→contract mapping table; c-6 split into prompt-half + filesystem-half, each with its own observable check | 5 — derives the *ideal* contract first then the task; named test functions per assertion (TestScaffoldSeverityOrder, TestAvailabilityMissingHasHint); severity-order as a pure Go fn is the strongest c-5 pin | 4 — 7 tasks; clean prompt/shim split and standalone gitignore (t-5) with a real `git check-ignore` contract; folds ledger into run (loses risk's malformed-ledger guard) | 5 — 3 waves; t-7 correctly gated on the prompt task (t-4) not the CLI; recognises the two independent wave-2 chains |

**Skeleton: `risk`.** It has the finest-grained, one-risk-one-owner graph, the
sharpest contracts, the only standalone context-free recon task (the genuine c-3
test surface), and the only malformed-ledger guard. Its weaknesses are local:
t-8 bundles gitignore with the prompt harness, and its gitignore contract is a
Go `.gitignore`-line read rather than a behavioural check. Both are fixed by
grafting from verification (standalone gitignore t-5 with `git check-ignore`) and
borrowing verification's named-test-function specificity. I keep risk's package
name `internal/security` (mvp agrees; verification dissents — see Disagreements).

## Merged plan

Skeleton = risk's 8-task / 3-wave graph. Grafts: verification's standalone
`git check-ignore` gitignore task (replaces the gitignore half of risk's t-8) and
its named-test-function contract style; mvp's confirmation that gitignore belongs
near the run-dir surface (kept as its own task per verification, not folded).
9 tasks across 3 waves.

```
Phase 05-dross-secure — 9 tasks across 3 waves

Wave 1
  t-1  Create run-dir + collision-safe run-id            [risk]
       files:    internal/security/run.go
                 internal/security/run_test.go
       covers:   c-1, c-6
       contract: if a second run on the same commit reuses the run-id, the
                 same-commit-twice case fails (second run must get a unique
                 suffix, not clobber the first dir); if NewRun writes anywhere
                 outside .dross/security/<run>/, the write-boundary assertion
                 fails; if a short-sha can't be read (no git / detached) it
                 falls back to "nogit" rather than erroring; RunID(ts,sha) not
                 returning "<timestamp>-<short-sha>" fails TestRunID.
       depends_on: []
       [locks: report_artifact (run-id = <timestamp>-<short-sha>, dir under
        .dross/security/<run>/); severity_model untouched here]

  t-2  Scanner catalog + availability detection          [risk+verification]
       files:    internal/security/catalog.go
                 internal/security/catalog_test.go
       covers:   c-4
       contract: if a core Go-or-agnostic scanner (gitleaks, semgrep, trivy,
                 govulncheck, gosec) is dropped from the catalog, the
                 catalog-completeness test fails; if a missing scanner is
                 reported without its install instruction,
                 TestAvailabilityMissingHasHint fails; if a non-Go language
                 (e.g. python) yields zero applicable agnostic tools instead of
                 the gitleaks/semgrep/trivy set, the agnostic-fallback test
                 fails (and its dedicated catalog is marked stub).
       depends_on: []
       [locks: scanner_contract (detect→plan→gate→sweep partition into
        installed/missing+hints); catalog_scope (Go-complete + agnostic, other
        langs stub)]

  t-3  Findings ledger schema + malformed-input guard     [risk]
       files:    internal/security/findings.go
                 internal/security/findings_test.go
       covers:   c-1, c-2
       contract: if a finding with an out-of-range / empty severity is
                 accepted, the severity-validation test fails; if a finding
                 lacking refutation evidence is treated as survived, the
                 unrefuted-dropped test fails; if Load on a truncated/garbled
                 findings.toml panics instead of returning an error, the
                 malformed-ledger test fails; a finding's severity + refutation
                 fields must round-trip.
       depends_on: []
       [locks: severity_model (contextual severity stored, not nominal);
        criterion_unit (ledger tracks every finding by id)]

  t-9  Gitignore the security run artifacts               [verification]
       files:    .gitignore
       covers:   c-6
       contract: .gitignore not matching ".dross/security/" fails
                 TestSecurityArtifactsGitignored, which shells
                 `git check-ignore .dross/security/x/report.md` and asserts the
                 path is ignored (a behavioural check, not a string-match on the
                 .gitignore line).
       depends_on: []
       [locks: report_artifact (run artifacts gitignored by default — no
        pre-disclosure on the public repo)]

Wave 2 (depends t-1, t-2)
  t-4  Language detection + tool-coverage manifest        [risk]
       files:    internal/security/recon.go
                 internal/security/recon_test.go
       covers:   c-3, c-4
       contract: if detection reads any .dross/ planning file (spec/rules/
                 goals) the context-free test fails (fixture has a planted
                 .dross/ — manifest must ignore it); if the manifest omits a
                 detected-but-uninstalled scanner (records only ran tools), the
                 coverage-manifest-completeness test fails; if an unknown file
                 extension crashes detection instead of being ignored, the
                 unknown-ext test fails.
       depends_on: [t-1, t-2]
       [locks: scanner_contract (records tool-coverage manifest); catalog_scope
        (detection maps to the data-driven catalog). NOTE: this task is the
        only *executable* guard on c-3 context-free — see Disagreement D-2]

Wave 3 (depends t-1, t-2, t-3, t-4)
  t-5  Findings→spec.toml + findings.toml scaffold writer  [risk+verification]
       files:    internal/security/scaffold.go
                 internal/security/scaffold_test.go
       covers:   c-5, c-1
       contract: if the emitted spec.toml fails to round-trip through
                 phase.LoadSpec, the valid-spec test fails; if two findings
                 collapse into one criterion (not one-criterion-per-finding),
                 TestScaffoldOnePerFinding fails; if criteria/waves are not
                 emitted criticals-first, TestScaffoldSeverityOrder fails; if a
                 criterion's text omits its findings.toml ledger id,
                 TestScaffoldCriterionCitesLedger fails; if a finding id in
                 spec.toml has no matching ledger entry, the ledger-linkage test
                 fails; if the writer emits with zero surviving findings, the
                 empty-scaffold guard test fails (refuses rather than writing a
                 vacuous phase).
       depends_on: [t-1, t-2, t-3, t-4]
       [locks: criterion_unit (one criterion per finding, criticals-first,
        ledger id per criterion); severity_model (severity drives wave order).
        Severity ordering is a pure Go fn — verification's strongest graft]

  t-6  Wire `dross security` subcommand                   [risk+mvp+verification]
       files:    internal/cmd/security.go
                 internal/cmd/security_test.go
                 cmd/dross/main.go
       covers:   c-1, c-4, c-6
       contract: if `dross security` is not registered on the root command, the
                 subcommand-known/parity test fails; the cobra-invoked
                 subcommands (detect/run/scaffold) must each call their wave-1
                 library surface — detect not printing installed-vs-missing
                 fails TestSecurityDetectOutput, run not creating the dir under
                 .dross/security/ fails TestSecurityRunCreatesDir, scaffold not
                 writing a severity-ordered spec.toml fails
                 TestSecurityScaffoldWrites; if the run command ever writes to a
                 path outside .dross/security/ (e.g. touches app source), the
                 read-only-boundary test fails; if a run with no available
                 scanners hard-errors instead of proceeding with partial
                 coverage + manifest, the partial-coverage test fails.
       depends_on: [t-1, t-2, t-3, t-4]
       [locks: scanner_contract (no hard refusal on empty toolbelt — partial
        coverage); criterion_unit/report_artifact via the scaffold subcommand.
        Three subcommands under one command, matching the repo verb model]

  t-7  Author secure.md + dross-secure.md prompts          [risk]
       files:    assets/prompts/secure.md
                 assets/commands/dross-secure.md
       covers:   c-2, c-3, c-5, c-6
       contract: if the commands/prompts pair is broken (one of the two
                 missing), TestCommandsPromptsParity fails; if secure.md omits
                 the refute-panel majority-vote drop rule, the c-2 assertion
                 fails; if it omits the "read no .dross/ planning artifacts"
                 instruction, the c-3 assertion fails; if it omits "no --fix /
                 never edit or commit app code", the c-6 assertion fails; if it
                 omits "propose-then-ask before locking" the scaffold, the c-5
                 assertion fails.
       depends_on: []
       [locks: testable_surface (prompt orchestrates recon/fan-out/refute-panel);
        all prompt-asserted criteria c-2/c-3/c-6 + c-5 propose-then-ask gate.
        Note: prompt file authored here; the assertions that pin it live in t-8]

  t-8  Content-assertion harness for the secure prompt     [risk+verification]
       files:    internal/cmd/secure_prompt_test.go
       covers:   c-2, c-3, c-5, c-6
       contract: if the secure.md content assertions (refute-panel + majority
                 vote, context-free, read-only / no --fix, propose-then-ask) are
                 not each exercised by a real failing sub-assertion, the
                 harness-presence check fails; removing any one mandated section
                 from secure.md must fail its corresponding sub-assertion in
                 secure_prompt_test.go (a dedicated test file, not appended to
                 commands_parity_test.go — each section is an individually
                 failing line item).
       depends_on: [t-7]
       [locks: testable_surface (prompt assertions are the named verify path for
        c-2/c-3/c-6). gitignore was REMOVED from this task vs risk's t-8 and
        promoted to standalone t-9 — see Disagreement D-3]
```

Coverage check (every criterion has ≥1 task; load-bearing ones have a code test):

- c-1: t-1, t-3, t-5, t-6
- c-2: t-3, t-7, t-8
- c-3: t-4 (executable), t-7, t-8
- c-4: t-2, t-4, t-6
- c-5: t-5, t-7, t-8
- c-6: t-1, t-6, t-7, t-8, t-9

All c-1..c-6 covered. Lock decisions all honoured (annotated per task).

## Disagreements

### D-1 — Go package name: `internal/security` vs `internal/secure`
- **risk** and **mvp**: `internal/security`.
- **verification**: `internal/secure` (and `internal/cmd/security.go` for the cmd).
- **Provisional default: `internal/security`** (2 of 3 drafts; the cmd file is
  `security.go` in all three, so `secure` would split the package name from the
  command name).
- **Why it matters:** it's a one-shot naming choice that every test import path
  and `cmd` wiring depends on; cheap to settle now, annoying to rename across
  9 tasks later. Not a correctness issue — flagged so the executor picks once.

### D-2 — Is language-detection / recon its own task?
- **risk**: yes — standalone t-4 (recon.go) in wave 2, the *only executable*
  guard that c-3 (context-free) is honoured (planted-.dross/ fixture must be
  ignored) and that the coverage manifest lists detected-but-uninstalled tools.
- **mvp** and **verification**: no — detection is folded into the catalog/CLI
  surface (mvp) or the prompt content assertion (verification); neither has a Go
  test that walks a target tree and proves .dross/ is skipped.
- **Provisional default: keep recon as its own task (t-4).** Without it, c-3 is
  pinned *only* by a prompt content assertion — a grep for a directive, not proof
  the code ignores .dross/. The locked `testable_surface` decision says c-3 is a
  prompt assertion, so this is defensible to drop; but risk's executable guard is
  strictly stronger and costs one wave-2 task.
- **Why it matters:** this is the sharpest real divergence. If the executor
  follows mvp/verification, c-3 has no code test and a regression (recon starts
  reading rules.toml) ships green. Keeping t-4 is the conservative call and the
  reason risk was chosen as skeleton.

### D-3 — How is the gitignore half of c-6 owned and tested?
- **risk**: gitignore lives in t-8, bundled with the prompt-assertion harness;
  contract reads the `.gitignore` line as a string.
- **mvp**: gitignore folded into t-1 (the run-dir task); contract asserts the
  gitignore line is present.
- **verification**: standalone t-5; contract shells `git check-ignore` — a
  behavioural check that the path is actually ignored.
- **Provisional default: standalone task (t-9) with verification's
  `git check-ignore` contract.** A `git check-ignore` test catches a wrong
  pattern (e.g. `security/` not matching `.dross/security/`) that a line-string
  match would pass. Splitting it out also un-bundles risk's t-8 (which mixed two
  surfaces — gitignore + prompt harness — violating one-owner).
- **Why it matters:** the gitignore is the no-pre-disclosure guarantee for a
  *public* repo (locked `report_artifact`). A string-match contract can go green
  on a non-matching pattern; the behavioural check can't. Low cost, real safety.

### D-4 — Is the prompt deliverable one task or split?
- **mvp**: one task (t-5) — secure.md + dross-secure.md + the content-assertion
  test together.
- **risk**: prompt content (t-7) split from the assertion harness + gitignore (t-8).
- **verification**: prompt secure.md (t-4) split from the dross-secure.md shim +
  assertions (t-7), with gitignore separate again (t-5).
- **Provisional default: prompt files together (t-7), assertion harness separate
  (t-8), gitignore separate (t-9).** Authoring both markdown files in one task
  keeps the command↔prompt parity pair atomic (mvp's point); pulling the test
  into its own task keeps each mandated section an individually failing
  sub-assertion (risk/verification's point) and respects rule r-01 — the prompt
  isn't live until `make install`, so authoring and asserting are genuinely
  separate steps.
- **Why it matters:** middle-ground merge. It avoids verification's split of the
  two markdown files across tasks (which would let parity break mid-phase) while
  keeping the test isolated so c-2/c-3/c-5/c-6 each have a named failing
  sub-assertion rather than one coarse pass/fail.

### D-5 — Is the findings ledger its own task?
- **risk**: yes — standalone t-3 (findings.go), the only draft with a
  malformed-ledger guard (truncated findings.toml returns an error, not a panic).
- **mvp**: folded into the scaffold task surface.
- **verification**: folded into t-1 (run-dir), as part of NewRun seeding
  findings.toml.
- **Provisional default: keep the ledger as its own task (t-3).** It owns two
  distinct failure surfaces the other drafts don't test — severity validation and
  malformed-input parsing — and the scaffold writer (t-5) consumes it, so a clean
  dependency edge is worth the separation.
- **Why it matters:** the ledger is the machine-readable artifact the scaffold
  reads; a garbled-ledger panic would crash the scaffold writer with no owned
  test in mvp/verification. Keeping t-3 gives the malformed-input guard an owner.
```
synthesis: 9 tasks across 3 waves, 5 disagreements
```
