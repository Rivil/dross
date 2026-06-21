# Plan Review — 11-retrofit-core-loop (t-8 amendment)

Reviewed: 2026-06-21
Plan: 8 tasks across 4 waves (t-1..t-7 executed; t-8 new)

## BLOCKING
(none)

## FLAG
- [achievability/4] t-8's c-4 negative sentinel is a strawman. The proposed assertion is
  "plan.md §5 ... carries no artifact-dump cue (e.g. \"Show final content\")". The string
  `Show final content` appears nowhere in plan.md (or any prompt) today — grep confirms
  zero hits. Asserting the absence of a string that was never present guards nothing: it
  passes today regardless of plan.md's content and would not catch the real c-4 regression
  (someone re-adding a wholesale `plan.toml` paste to §5).
  Suggestion: anchor c-4 on the stable strings that actually exist and that a regression
  would delete — assert §5 contains the literal one-line confirm `Plan written: N tasks
  across W waves` (line 204) AND the directive `Don't paste the toml back` (line 204). For
  the negative half, slice the §5 region (between the `## 5.` heading and `## 6.`) and
  assert it contains no `[[task]]` literal — that targets the actual artifact-dump shape,
  not an invented cue.

- [test-contract/3 + achievability/4] t-8's c-3 "separate turns" assertion doesn't name
  the surface that distinguishes "separate" from "bundled", and the obvious implementation
  is fragile. A global count of `AskUserQuestion` in ship.md is brittle: there are
  currently 4 occurrences — the three real turns (§2/§3/§6) plus one in the line-5 intro
  prose that enumerates them — so a naive `count == 3` is already wrong, and any edit to
  the line-5 prose shifts the count and false-fails.
  Suggestion: assert per-section using the heading-slicer already in this file
  (`coreLoopAuditSection`-style). Require each of `## 2.`, `## 3.`, `## 6.` to contain
  exactly one `AskUserQuestion`, keyed on the stable anchors: §2 `Use generated body, or
  write your own?`, §3 `Request reviewers`, §6 `Merge now?`. For the "not a silent
  config-write" half, assert §3 contains both `AskUserQuestion` and the literal `rather
  than silently writing config` (line 42).

## NOTE
- [coverage/1] Coverage is complete. c-1/c-2 (t-1..t-5, t-7), c-5 (t-6, t-7), and after
  t-8, c-3/c-4 gain direct prompt-sentinel tests. t-8 closes exactly the two criteria the
  verify.toml partial verdict left "weak" (its own FLAG finding asked for this same test) —
  correctly scoped to the gap, no scope creep, no new criteria.

- [locked-decisions/2] No locked-decision conflict. t-8 is consistent with
  `ship_body_preview` (the §1 PR-body preview stays as the c-4 exception) and
  `retrofit_depth`. It doesn't touch verify.md, so `verify_surface` is unaffected — and
  c-3 is correctly satisfied by absence in verify.md (verify.md has no AskUserQuestion
  turns), which t-3 already declined to claim.

- [granularity/5] Wave 4 with `depends_on = [t-1, t-4, t-7]` is precise. t-8 asserts
  against plan.md (t-1) and ship.md (t-4) and extends the test file authored by t-7. It
  does not depend on t-2/t-3/t-5/t-6 because it makes no assertions about those prompts —
  a tight dependency set rather than an over-broad one. Wave 4 is correct since t-7 must
  exist first.

- [antipatterns/6] None. t-8 extends the existing `interaction_coreloop_test.go` rather
  than spawning a parallel file (no artificial split); no empty set-up task; both prompts
  it targets (plan.md, ship.md) exist and contain the sections it asserts against.

- [strengths/7] (a) t-8's test_contract names concrete regressions — "collapses
  body-override and reviewers into one turn", "reverts reviewers to a silent config-write",
  "re-adds an artifact-dump cue" — specific per the plan's own §2 contract bar, not "tests
  pass". (b) The amendment honestly graduates the exact criteria flagged weak in
  verify.toml instead of re-litigating covered ones. (c) ship.md and plan.md already carry
  the stable prose anchors a sharper sentinel would need (`Request reviewers`, `rather than
  silently writing config`, `Plan written: N tasks across W waves`, `Don't paste the toml
  back`) — so the FLAGs above are a tightening, not a blocker; the surfaces exist.

## Summary
t-8 is well-scoped, correctly waved, and completes coverage — but both named sentinels are
weak as written (the c-4 "Show final content" cue never existed in plan.md, and the c-3
"separate turns" check needs section-scoped anchors rather than a fragile global
AskUserQuestion count); re-key both onto the stable strings that actually exist before
executing.
