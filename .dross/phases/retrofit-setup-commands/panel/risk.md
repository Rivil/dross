Phase 12-retrofit-setup-commands — 9 tasks across 3 waves

Lens: RISK. The graph is shaped by failure modes, not by file convenience.
Every named failure mode below is owned by exactly one task and tested by exactly
one test so a regression points at a single owner. The dominant risks for this
phase:

  R1  A prompt silently drops the `dross interaction show` emitter → the live
      command stops delivering the playbook (the c-1 failure; nested @-include
      already proved dead in the phase-10 pilot).
  R2  A prompt carries the dead `@~/.claude/dross/prompts/_interaction.md` include
      and a reader/author trusts it → false delivery.
  R3  The "interaction playbook" contract phrase is absent → the binding style is
      invisible to a reader (c-2), so the next editor reverts to broadcast shape.
  R4  A multi-field "Ask: name, description, core value, audience, non-goals"
      block stays a mega-form → bundled decisions (c-3). High-density risk in
      init §1, onboard §1, and the options section-pick gate.
  R5  options' section-pick gate is implemented as one giant AskUserQuestion over
      all settings → the locked options_walk is violated even though §1-§11 each
      "walk one at a time".
  R6  milestone collapses criteria/non-goals/phase-order into one accept-the-set
      turn → milestone_walk violated (c-3).
  R7  quick/inbox diverge from execute's already-shipped gate shape instead of
      mirroring verbatim → drift between two near-identical flows
      (quick_inbox_mirror).
  R8  A composed artifact (project.toml / milestone.toml / rules / options diff)
      gets pasted back wholesale for review (c-4).
  R9  An audit row is flipped to ✅ but its decision-point list no longer matches
      the rewritten prompt → the audit lies (c-5 vacuous-pass).
  R10 A new anchor test keys on a phrase the rewrite doesn't actually emit, or on
      a too-generic phrase, so it passes vacuously / never fails on regression.

Each prompt edit must account for RULE r-01: `make install` re-links assets into
~/.claude before the change is live. Tasks below are source-and-test only; the
execute step runs `make install` before any live invocation. The grep/anchor
tests read assets/prompts/*.md from source, so they gate the retrofit without a
live run — that is deliberate (closes R10's "I tested the stale binary" trap).

Wave 1 — emitter + contract delivery (R1, R2, R3); the spine every other risk sits on

  t-1  Wire emitter + contract phrase into all 7 prompts
       files:    assets/prompts/init.md, assets/prompts/onboard.md,
                 assets/prompts/options.md, assets/prompts/rule.md,
                 assets/prompts/inbox.md, assets/prompts/quick.md,
                 assets/prompts/milestone.md
       covers:   c-1, c-2
       contract: Add `dross interaction show` beside `dross rule show` in each
                 prompt's pre-flight, and an intro line carrying the exact phrase
                 "interaction playbook". If init.md drops the emitter call, or
                 milestone.md keeps a `dross rule show` with no adjacent
                 `dross interaction show`, t-2's TestSetupPromptsWireEmitter fails
                 for that named prompt (R1). rule.md has no "## 0. Pre-flight" —
                 the emitter line is added under a new pre-flight step; if it
                 lands as a dead @-include instead, the deadIncludeLine half of
                 t-2 fails (R2).

  t-2  Add grep guards for emitter + contract (7 prompts)
       files:    internal/cmd/interaction_setupcmds_test.go (new)
       covers:   c-1, c-2
       contract: New `setupPrompts = {init,onboard,options,rule,inbox,quick,
                 milestone}`. TestSetupPromptsWireEmitter asserts each file
                 contains "dross interaction show" and NOT deadIncludeLine —
                 mirrors TestCoreLoopPromptsWireEmitter, reusing repoRootFromTest
                 + deadIncludeLine. TestSetupPromptsReferenceContract asserts each
                 contains interactionRefPhrase ("interaction playbook"). Drop the
                 emitter from any one prompt → the per-prompt subtest names the
                 file that broke (R1/R2/R3). Independent of t-1's content; can be
                 authored in parallel — both must be present for the suite to pass.

Wave 2 — per-prompt decision-shape rewrites (R4–R8); each owns its bundling/paste risk

  t-3  Rewrite init + onboard identity walks one-decision-per-turn
       files:    assets/prompts/init.md, assets/prompts/onboard.md
       covers:   c-3, c-4
       contract: init §1 (vision) and onboard §1 (identity) become per-field
                 AskUserQuestion turns (name, then description, then core value,
                 then audience, then non-goals) instead of one bulleted "Ask:
                 A,B,C,D" block; init §3 stack and §4/§6/§7 remote/runtime each
                 stay one decision per turn; the saved project.toml is confirmed
                 by one-line summary, never pasted. t-7's per-prompt anchor test
                 fails if init §1's "Project name?" / "core value" turns are
                 recombined into a single AskUserQuestion, or if a "paste
                 project.toml back" directive appears (R4, R8).

  t-4  Rewrite options section-pick gate + per-setting walk
       files:    assets/prompts/options.md
       covers:   c-3, c-4
       contract: Implements locked options_walk — a FIRST AskUserQuestion gate
                 picks section(s) (project / defaults / rules / profile / env),
                 THEN within each chosen section the Keep·Change·Skip turn runs
                 one setting at a time. The §12 wrap prints a changed/skipped
                 summary, never the full project.toml. t-7's options anchor test
                 asserts a section-pick gate turn exists distinct from the
                 per-setting turn (the gate's option labels appear; "one setting
                 at a time" prose present) — fails if the rewrite makes the gate a
                 mega-form over all settings (R5) or pastes the config (R8).

  t-5  Rewrite milestone scoping walk (criteria/non-goals/phases each its own segment)
       files:    assets/prompts/milestone.md
       covers:   c-3, c-4
       contract: Implements locked milestone_walk — §3 success criteria walked one
                 criterion per turn (accept/reword/drop), §4 non-goals its own
                 segment, §5 phase order its own confirm/revise turn; the written
                 milestone.toml is confirmed by the §7 one-line summary, never
                 dumped. t-7's milestone anchor test asserts criteria are
                 per-criterion (an "each criterion" / one-at-a-time anchor) and the
                 §7 summary is a count line, not a toml paste — fails if §3 reverts
                 to one accept-the-whole-set turn (R6) or pastes the toml (R8).

  t-6  Mirror execute's gates into quick; walk inbox per-issue
       files:    assets/prompts/quick.md, assets/prompts/inbox.md
       covers:   c-3, c-4
       contract: quick §2 approval + §4 red-path reuse execute.md's verbatim
                 propose-and-react shape (proceed/steer/show/abort; fix/abort) —
                 honoring quick_inbox_mirror; inbox §2 triage stays one
                 AskUserQuestion per issue (the existing "#<n> ... what should
                 happen?" turn) and never bundles issues. t-7's quick anchor test
                 asserts quick's §2 option set matches execute's tokens (proceed /
                 steer / abort present, no merged gate); inbox anchor asserts the
                 per-issue turn phrase survives — fails if quick invents a
                 divergent gate (R7) or inbox batches issues into one turn (R4).

Wave 3 — vacuous-pass guards + audit truth (R9, R10); depend on the rewrites' stable anchors

  t-7  Add per-prompt decision-shape anchor tests
       files:    internal/cmd/interaction_setupcmds_test.go
       covers:   c-3, c-4
       contract: Extend the wave-1 test file (reusing promptSection) with one
                 anchor test per rewritten prompt, each keyed on a STABLE string
                 the rewrite emits and counting AskUserQuestion within a
                 promptSection slice — NOT a whole-file count (R10). Specifically:
                 options' section-pick gate is its own turn distinct from the
                 per-setting turn; milestone §3 walks criteria one at a time;
                 init/onboard identity fields are separate turns; quick's gate
                 tokens match execute's; no prompt contains a "paste <artifact>
                 back" directive (c-4). Each assertion names the prompt+section so
                 a regression points at one owner. Depends on t-3..t-6 for the
                 anchors to key on.

  t-8  Flip the 7 audit rows to ✅ and reconcile decision points
       files:    docs/interaction-audit.md
       covers:   c-5
       contract: In the "## Setup & config (phase 12)" section, every 🟡/⬜ marker
                 across the 7 commands' rows becomes ✅, and each row's
                 decision-point list is reconciled to match the rewritten prompt
                 (e.g. milestone gains the per-criterion row; options shows the
                 section-pick gate row). Depends on t-3..t-6 so the rows describe
                 the shipped shape, not the pre-retrofit one (R9). Tested by t-9.

  t-9  Add audit-conformance guard for the 7 setup sections
       files:    internal/cmd/interaction_setupcmds_test.go
       covers:   c-5
       contract: TestSetupAuditSectionsConform mirrors TestCoreLoopAuditSectionsConform
                 (reusing coreLoopAuditSection-style slicing) over setupPrompts:
                 each `### dross-<name>` section under the phase-12 heading must
                 contain ✅ and must NOT contain ⬜/🟡/❌. Leave one row 🟡 (R9) →
                 the named subtest fails. Does not duplicate
                 interaction_audit_test.go's "a section exists per command" guard;
                 this checks the section's *conformance marker*. Depends on t-8.

## Coverage

  c-1 (emitter in all 7 pre-flights, grep-verifiable)        → t-1 (edit), t-2 (guard)
  c-2 ("interaction playbook" phrase per prompt)             → t-1 (edit), t-2 (guard)
  c-3 (no bundled multi-decision turn; per-decision walks)   → t-3, t-4, t-5, t-6 (edits), t-7 (guard)
  c-4 (no wholesale artifact paste; one-line summary)        → t-3, t-4, t-5, t-6 (edits), t-7 (guard)
  c-5 (audit rows mapped + propose-and-react confirmed)      → t-8 (doc), t-9 (guard)

  All 5 criteria owned. Every criterion has both an editing task and a test task —
  no criterion is left to a doc-proxy only (the gap that left c-3/c-4 "weak" at
  phase-11's first verify; t-7 closes it here with real per-prompt anchors).

## Judgment calls

- Split the 7 prompt edits into t-1 (mechanical emitter/phrase, one risk class
  across all 7) + t-3..t-6 (per-prompt decision-shape, different risk per prompt).
  Rejected: one task editing all 7 in full. Why: bundling emitter wiring with
  decision-shape rewrites would make one task own R1 *and* R4–R8, so a bundling
  regression and a dropped-emitter regression hit the same task — the lens
  demands one risk per owner.
- t-2 (emitter/phrase guards) is wave 1, parallel to t-1, not wave 2. Rejected:
  gating tests behind the edit. Why: the grep guards read source and don't depend
  on the edit's *content*, only that both land before the suite is green; keeping
  them wave 1 maximizes parallelism and the test exists to catch t-1 getting it
  wrong.
- Grouped init+onboard (t-3) and quick+inbox (t-6) two-to-a-task; kept options
  (t-4) and milestone (t-5) solo. Rejected: one task per prompt (would be 5
  decision-shape tasks). Why: init/onboard share the identity-walk pattern and
  quick/inbox are both short, but options' section-pick gate and milestone's
  three-segment walk are each a locked decision with its own distinct failure
  mode — they earn isolation so R5 and R6 each have a single owner.
- t-7 anchor tests count AskUserQuestion *within a promptSection slice*, not
  whole-file. Rejected: a whole-file `Count(body,"AskUserQuestion")==N` assertion.
  Why: a whole-file count passes vacuously when two turns are merged but a third
  is split, and breaks on unrelated edits — section-scoped + stable-anchor keying
  is what makes R10 (vacuous pass) actually testable.
- t-8 audit flip depends on t-3..t-6 (wave 3), not done early alongside t-1.
  Rejected: flipping rows to ✅ in wave 1 with the emitter wiring. Why: a row is
  only honestly ✅ once its prompt's decision shape is reconciled; flipping before
  the rewrite is precisely R9 (audit lies). The dependency is real, not cosmetic.
