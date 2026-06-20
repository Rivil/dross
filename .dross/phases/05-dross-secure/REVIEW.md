# Plan Review — 05-dross-secure

Reviewed: 2026-06-20
Plan: 9 tasks across 3 waves

## BLOCKING
(none)

All six criteria (c-1…c-6) appear in at least one `covers` field, no task
contradicts a `locked = true` decision, and no task implies a forbidden action.

## FLAG

- [wave-order] t-4 (wave 2) `depends_on = ["t-1","t-2"]`, but its stated work —
  walk the tree, detect languages, emit a coverage manifest, and a context-free
  guard — has no actual code dependency on t-1 (run-dir/run-id) or t-2
  (catalog/availability) being *implemented first*. Detection consumes the
  catalog's *data shape*, not run-dir creation; t-1 in particular is unrelated to
  recon. The manifest needs t-2's installed-vs-missing partition, so a t-2 link is
  defensible, but t-1 is not a true predecessor.
  Suggestion: drop the t-1 dependency from t-4; if t-2 is only a type/shape
  dependency (not a runtime-output one), consider whether t-4 can co-run in wave 1
  behind a shared catalog type, otherwise keep only the t-2 link.

- [granularity / merge-candidate] t-9 is a single-line `.gitignore` edit (add
  `.dross/security/`) — well under 10 minutes. It is structurally identical to
  the read-only/write-boundary concern already owned by t-1 (which asserts writes
  stay inside `.dross/security/<run>/`) and t-6 (read-only boundary). Confirmed
  against the repo: `.gitignore` currently ignores `/dross` (the binary) but NOT
  `.dross/`, and `git check-ignore .dross/security/x/report.md` returns
  not-ignored — so the task is genuinely needed, just too small to stand alone.
  Suggestion: fold the `.gitignore` line + its `git check-ignore` test into t-1
  (the run-dir task that owns artifact placement) rather than carrying a separate
  wave-1 task.

- [test-contract] t-6's contract "if `run` ever writes to a path outside
  `.dross/security/` (touches app source), the read-only-boundary test fails" and
  t-1's "if NewRun writes anywhere outside `.dross/security/<run>/`, the
  write-boundary assertion fails" both assert a negative (no write escapes the
  sandbox). A negative-write assertion is only as strong as the paths it exercises;
  neither contract names *how* the boundary is enforced or what path the test
  attempts. As written they risk passing vacuously (a test that never triggers a
  stray write trivially "verifies" no stray write).
  Suggestion: have each name the concrete escape it attempts and expects to be
  refused/contained (e.g. "attempts a finding whose path resolves to ../main.go
  and asserts the write is rejected, not silently placed").

- [test-contract] t-7's second contract item ("secure.md must contain the
  refute-panel … each is asserted in t-8") delegates its own enforcement to t-8.
  t-7 (the prompt-authoring task) therefore has no self-contained gate on the four
  mandated sections; if t-8 slips or its assertions weaken, t-7's content
  obligations have no independent check. This is a cross-task IOU, not a contract
  on t-7's own surface.
  Suggestion: keep t-8 as the real harness, but either (a) merge t-7+t-8 so the
  prompt and its content harness land together, or (b) restate t-7's contract as
  "TestCommandsPromptsParity covers the pair's existence; section content is
  gated by t-8" so the delegation is explicit rather than implied.

## NOTE

- [granularity] t-6 touches 3 files across 2 layers (`internal/cmd/security.go`,
  its test, and `cmd/dross/main.go` wiring) and exposes three subcommands
  (detect/run/scaffold). Under the 5-file / 3-layer threshold, so not a flag, but
  it is the heaviest task and the integration point for all of wave 1+2 — worth
  watching if it grows.

- [r-01 / make install] Per project rule r-01, prompt edits (t-7) and Go edits
  (t-1…t-6) are not live until `make install`. No task violates this, but verify
  for this phase must `make install` before exercising `dross security` or the
  installed slash command, or it will test a stale binary/prompt. Recording so
  the verify step doesn't false-green on stale assets.

- [strengths] Coverage is clean and slightly redundant in a good way — c-6
  (read-only) is asserted from five angles (run-dir boundary, gitignore behaviour,
  prompt rule, prompt-content harness, CLI boundary), so a single weak assertion
  can't let a write-escape through unnoticed.

- [strengths] Test contracts are mostly behavioural, not string-match: t-9
  explicitly shells `git check-ignore` rather than grepping the `.gitignore` line,
  and t-5 round-trips the emitted spec through the real `phase.LoadSpec`
  (confirmed to exist at internal/phase/phase.go:206) rather than asserting on
  text. These resist the "asserts the literal we just wrote" trap.

- [strengths] The criterion_unit locked decision (one acceptance criterion per
  surviving finding) is faithfully encoded in t-5's contract
  (TestScaffoldOnePerFinding + the ledger-id citation test), so the "no finding
  hides behind a tier-level test" intent survives into the scaffold writer.

## Summary
Structurally sound and well-covered plan; the only real cleanups are dropping
t-4's spurious t-1 dependency, folding the trivially-small t-9 into t-1, and
tightening two negative/IOU test contracts — none blocking.
