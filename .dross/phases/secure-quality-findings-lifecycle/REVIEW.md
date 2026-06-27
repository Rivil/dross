# Plan Review — secure-quality-findings-lifecycle

Reviewed: 2026-06-27
Plan: 6 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [coverage-behavioral] c-1 ("After a fresh run completes, its findings ARE
  reconciled...") and c-3 are worded as if reconciliation happens automatically
  on run completion, but no task wires reconcile into the scan path (run.go) or
  the dross-secure/dross-quality skill prompt. The plan delivers only a manual
  `findings reconcile <run-dir>` verb (t-4) — nothing invokes it after a run.
  The `covers` fields are satisfied (c-1/c-3 each appear in a task), so this is
  not a strict coverage gap, but the criteria's behavioral promise ("are
  reconciled") is only met if a human types the verb or a prompt orchestrates it.
  Suggestion: confirm whether auto-post-run reconciliation is in scope. If the
  intended trigger is the skill prompt (an assets/ change governed by rule r-01),
  state that explicitly in the spec/plan so a verifier doesn't read c-1 literally
  and fail it; if auto-wiring into run.go is expected, it needs a task.

- [test-contract / over-coverage-tag] t-6's description claims it "Includes the
  end-to-end fold-survives-rerun path for c-3," and its contract has the
  fold-survives-rerun test, but t-6's `covers` lists only c-1/c-2/c-5 (not c-3).
  Harmless for coverage (t-3 covers c-3), but the `covers` field understates what
  t-6 actually exercises, which obscures traceability at verify time.
  Suggestion: add c-3 to t-6's `covers`, or drop the c-3 claim from its
  description so the two agree.

## NOTE
- [locked-decision / cli_shape] The author's reading holds. The locked cli_shape
  decision enumerates the *state-command* surface (`findings list`,
  `findings <id> --state ...`); it does not declare an exhaustive verb set, and
  `reconcile` is a lifecycle trigger, not a state-setter. The locked
  reconciliation_timing decision actively *requires* a post-scan reconcile step
  to exist somewhere, so surfacing it as a sibling verb is consistent, not
  contradictory. The only tension is the rationale "keeps the verb surface
  small" — the group is now three verbs (list / reconcile / <id>). Not blocking;
  worth recording that the verb-surface rationale was stretched.

- [test-contract] Test contracts are a strength of this plan — each names the
  exact failing test and the precise breaking condition (line-number-in-hash,
  Severity-fed-where-Class-belongs, atomic-write truncation, '' / 'bogus' state).
  Well above the "tests pass" bar; no vague contracts found.

- [edge-cases] Edge coverage is thorough: deleted-file retention,
  identical-fingerprint dedup, corrupt/missing TOML on load, interrupted atomic
  write (temp+rename), empty file/title inputs, and no-mutation-of-scan. This
  anticipates the failure modes that usually surface in review.

- [design] Clean shared-core architecture: one `internal/findings` package plus
  one descriptor-parameterized cobra group reused by both tools, avoiding
  duplicated command logic; the new `lifecycle.go` files correctly sit beside
  the existing `findings.go` (verified both `internal/security/findings.go` and
  `internal/quality/findings.go` already exist) rather than colliding with it.
  Adapter contracts also match reality — security's Finding has Class+Severity,
  quality's has Dimension+Risk, exactly as the t-5/t-6 contracts assume.

- [granularity / waves] Granularity and wave order are sound: every task is
  impl+test (2 files) or one cmd + one adapter + one test (3 files, ≤2 layers),
  none crosses the 5-file/3-layer line and none is a trivial one-file merge
  candidate; each wave-N+1 task strictly consumes a wave-N output (t-3 needs the
  fingerprint+store; t-4's reconcile subcommand needs t-3; t-5/t-6 need t-4's
  shared group and run in parallel).

- [gitignore] No .gitignore edit is needed — the root .gitignore already ignores
  `.dross/security/` and `.dross/quality/` wholesale, so `state.toml` inherits
  coverage and the t-5/t-6 `git check-ignore` assertions will pass against
  current config.

- [rules] No forbidden-action conflicts. The only project rule (r-01,
  make-install after Go/prompt edits) is an execution-time hygiene rule, not a
  plan-structure constraint; no global rules.toml exists. The plan touches no
  assets/ prompts.

## Summary
Structurally strong, well-tested, no blockers — but settle whether
reconciliation is meant to fire automatically after a run (the criteria read
that way) before executing, since the plan currently ships only a manual verb.
