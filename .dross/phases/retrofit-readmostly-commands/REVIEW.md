# Plan Review — retrofit-readmostly-commands

Reviewed: 2026-06-30
Plan: 4 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [granularity / antipattern] t-2 reuses t-1's "shared classifier", but the classifier's
  three inputs (command shims, prompts, and the exempt list in docs/interaction-audit.md)
  do not all exist at doctor *runtime*. The Go test reads them from the dross source tree
  via repoRootFromTest (fine). But `dross doctor` is designed to run "inside any
  dross-onboarded project" (doctor.go header), where the dross source tree is absent.
  assets/embed.go embeds only `all:commands all:prompts` — docs/interaction-audit.md (which
  holds BOTH the audit sections and the exempt list the classifier reads) is NOT embedded,
  so doctor cannot recover the exempt list at runtime outside the dross repo. The plan never
  states where doctor sources these inputs or how the check behaves when run in a non-dross
  project.
  Suggestion: in t-2, specify doctor's input story — either gate the check on "am I in the
  dross source repo?" (assets/ + docs/interaction-audit.md present on disk) and no-op
  otherwise, or accept that this lint is dross-repo-only and say so. The test (the gate per
  the locked check_home decision) is unaffected; this is purely the doctor surface.

- [coverage] c-2 requires the exempt list to *replace* "the prose-only Scope sentence" in
  interaction-audit.md. t-1's description only says "Add a structured `## Exempt` list" and
  its test_contract only checks the list's presence — neither removes/updates the existing
  Scope paragraph ("Read-only commands (status) and subagent-only commands (plan-review) are
  out of scope and intentionally absent. The Go test in
  internal/cmd/interaction_audit_test.go fails if..."). Left as-is, that prose duplicates the
  list and still names the old enforcing test.
  Suggestion: have t-1 (or t-4's header rewrite) explicitly replace the prose Scope sentence
  and update the enforcer reference, so the doc has one structured source of truth.

- [granularity / antipattern] t-3 is a confirm-and-pin task whose only guaranteed concrete
  output is guard comments in two existing test files (pause/resume already conform — the
  existing TestOtherCmds* tests pass today). The "fix any deviation" work is contingent and
  likely empty. This is thin.
  Suggestion: keep it if traceability comments for c-3/c-6 are the real deliverable, but be
  honest in execution that it is a pin, not new coverage; don't manufacture a fix that isn't
  needed.

## NOTE
- [coverage] All six criteria are covered exactly once: c-1,c-2→t-1; c-3,c-6→t-3; c-4→t-4;
  c-5→t-2. No gaps.

- [coverage / antipattern] c-1's text says "Extends interaction_audit_test.go," but t-1
  instead adds new files interaction_coverage.go + interaction_coverage_test.go. The
  functional intent (a fail-closed Go test) is met and the file name isn't a locked decision,
  so this is an interpretation call, not a violation — recording it so it's a conscious choice.

- [test-contract] t-3 naming TestOtherCmdsWireEmitter / TestOtherCmdsReferenceContract /
  TestCommandsPromptsParity as the contract is acceptable, NOT a dodge: those tests genuinely
  exist and genuinely cover pause/resume (otherCmdPrompts includes both) and parity, and the
  contract names the exact breakage. One omission: c-3 also asserts "audit sections reflect
  reality," which is covered by TestOtherCmdsAuditSectionsConform — the t-3 contract doesn't
  cite it. Minor.

- [wave-order] t-4's depends_on=["t-1"] is justified by file contention (both edit
  docs/interaction-audit.md) rather than a strict data dependency on t-1's classifier code.
  With atomic per-task commits, sequencing them avoids a same-file conflict, so the wave-2
  placement is reasonable even though the convention text itself doesn't need t-1's output.

- [locked-decisions] No task contradicts a locked decision. exempt_mechanism is honored — no
  task introduces a prompt-level `<!-- interaction: exempt -->` marker; the exempt list lives
  in docs/interaction-audit.md and is read by the classifier. check_home is honored — the test
  is the gate (t-1), doctor only surfaces (t-2). coverage_universe is honored — t-1 keys on
  command-backed prompts with _-partials excluded.

- [forbidden-actions] No rule violation. Project rule r-01 (prompt/Go edits not live until
  `make install`) is informational here: the t-2/t-4 test contracts are in-process Go tests
  (doctor uses Print/Printf helpers, testable without the installed binary), so they don't
  depend on a stale `~/.claude` link. Any *manual* `dross doctor` verification would need
  `make install` first.

- [strength] The exempt list is provably exhaustive: a disk scan confirms exactly two
  command-backed prompts lack AskUserQuestion (status, plan-review) and t-1 adds precisely
  those two — so the new fail-closed test will pass on first run with no hidden third case.

- [strength] Test contracts are specific throughout — each names the exact test function and
  the exact regression that trips it (dropped emitter line, removed exempt entry, lost prompt,
  deleted convention statement), not "tests pass."

- [strength] t-2 and t-4 correctly reuse t-1's shared classifier instead of duplicating the
  logic, matching c-5's explicit "rather than duplicating" requirement.

## Summary
Coverage, locked-decision fidelity, and rule-compliance are all clean and the plan is mostly
well-formed; the one substantive gap is t-2's unstated doctor-runtime input story (the exempt
list lives in an un-embedded doc the classifier needs outside the dross repo), plus a missed
"replace the prose Scope sentence" requirement in c-2.
