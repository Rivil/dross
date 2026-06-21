# Plan Review — 11-retrofit-core-loop

Reviewed: 2026-06-21
Plan: 7 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [granularity / antipattern] t-3 claims `covers = ["c-3", "c-4"]` for verify.md, but
  verify.md contains **zero** `AskUserQuestion` turns (grep: 0 matches) and composes no
  user-facing decision turn — its verdict in §4 is computed, then printed as a summary,
  not asked. c-3 ("no interactive turn bundles multiple unrelated decisions") is
  vacuously satisfied for verify.md because there is no interactive turn to fix, and the
  §4 summary already conforms to verify_surface/c-4 today. So t-3's real, testable work
  is just the two grep sentinels (emitter call + contract phrase) plus the §4 wording —
  the c-3 claim in its `covers` is hollow.
  Suggestion: keep t-3 (the emitter + contract-reference wiring is real), but drop `c-3`
  from its `covers` or note explicitly that verify.md satisfies c-3 by absence. Don't let
  the coverage matrix imply a c-3 fix landed in verify.md when nothing changes for it.

- [antipattern] c-3 names ship's body-override and reviewers as the canonical "separate
  turns" example, and t-4 says "Split §2 body-override and §3 reviewers into separate
  single-decision turns." But in current ship.md they are **already not bundled**: §2 is
  an `AskUserQuestion`, and §3 (reviewers) is not a turn at all — it's bare
  `dross project get/set` with no prompt. There is nothing to split. ship.md's two actual
  `AskUserQuestion`s are §2 (body) and §6 (merge gate) — already separate.
  Suggestion: re-word t-4 so it doesn't imply un-bundling work that isn't needed. If c-3's
  intent is that reviewers *should* become a proper propose-and-react turn (it's currently
  a silent config write), say that; otherwise t-4's c-3 work is just confirming the
  existing separation holds.

- [forbidden-actions / r-01] Neither the plan nor any task accounts for project rule r-01
  ("after editing a prompt or Go code, run `make install` before relying on the change").
  t-1..t-6 edit prompts/docs and t-7 adds Go code. t-7's guard test reads `assets/prompts/`
  directly via `repoRootFromTest` (like the existing pilot/audit tests), so the *test*
  doesn't need install — but the retrofit's whole point is the live `/dross-plan` etc.
  behaving differently, and that only happens after `make install` re-links the prompts.
  Suggestion: have /dross-verify (or a wrap step) run `make install` before relying on the
  retrofitted prompts, and confirm the installed binary isn't stale (`make doctor`). At
  minimum, record that t-7's assertions are source-tree checks and the behavioural change
  needs `make install` to take effect.

- [antipattern] t-7's contract asserts each prompt "carries no dead nested @-include line."
  None of the five core prompts has ever carried `@~/.claude/dross/prompts/_interaction.md`
  (grep: NONE present) — only spec.md did, and the phase-10 pilot already removed it. So
  this assertion guards against a regression that cannot occur for these five files.
  Suggestion: keep it if cheap (it's harmless and mirrors the pilot test), but don't count
  it as meaningful coverage; the load-bearing assertions are the emitter call and contract
  phrase.

## NOTE
- [coverage] t-5 lists `c-4` in review.md's `covers`, but review.md composes
  `review-comment.md` — an **outward-facing** publish artifact (it gets posted to the PR),
  and §4 already shows the full composed comment intentionally. That is the same class as
  ship_body_preview's exception, not an internal .dross artifact c-4 governs. The c-4 work
  for review.md is therefore near-nil; the real change is the §5 post-or-skip turn (already
  a single AskUserQuestion) plus the emitter/phrase wiring.

- [wave-order] t-7's prompt-wiring half (emitter + contract phrase across the 5 prompts)
  depends only on t-1..t-5, not t-6. It sits in wave 3 because it *also* asserts the audit
  sections are conforming (needs t-6). That coupling is legitimate, but it means a single
  test guards two unrelated surfaces; if the audit-conformance assertion proves fiddly it
  could block the prompt-wiring guard. Acceptable as-is; just be aware they're fused.

- [coverage] Coverage matrix is otherwise complete: c-1 (t-1..5,t-7), c-2 (t-1..5,t-7),
  c-3 (t-1..5), c-4 (t-1..4), c-5 (t-6,t-7). Every criterion has at least one covering task.

- [granularity] All five wave-1 tasks touch exactly one file. They are kept separate (one
  prompt each) rather than merged, which is correct here — retrofit_depth requires a uniform
  rewrite of every interactive turn per prompt, so each is real work, not a < 10-min edit.

- [strength] The plan correctly follows the phase-10 pilot's proven mechanism: emitter call
  (`dross interaction show`) + grep-verifiable contract-reference phrase, rather than the
  nested @-include the pilot disproved. The test_contracts key off the same falsifiable
  sentinels the existing pilot test uses.

- [strength] t-6 explicitly reconciles the audit-doc phase grouping (moving verify/ship/
  review out of the "phase 13" section into phase-11 scope). Without this the doc would
  contradict the actual scope, and the existing `interaction_audit_test.go` would still
  pass while the doc lied — a real, easy-to-miss bookkeeping fix the author caught.

- [strength] Locked decisions are respected throughout: ship_body_preview (t-4 keeps the
  full PR-body preview as the explicit c-4 exception), verify_surface (t-3 emits verdict +
  compact mapping, not raw verify.toml), panel_disagreement_walk and retrofit_depth (t-1).

## Summary
Coverage is complete and locked decisions are honored, but three of the five wave-1 tasks
claim c-3/c-4 coverage that is hollow for their file (verify.md has no interactive turn;
ship's reviewers aren't bundled; review's comment is outward-facing) — fix the `covers`
framing — and the plan never accounts for r-01's `make install` before relying on the
retrofitted prompts.
