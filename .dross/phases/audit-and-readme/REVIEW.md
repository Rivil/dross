# Plan Review — 13-audit-and-readme

Reviewed: 2026-06-21
Plan: 4 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [granularity] t-1 touches 5 files (architecture/secure/quality/pause/resume prompts),
  which trips the 5+ file split rule. Mitigating: the five edits are the same mechanical
  change (add `dross interaction show` to pre-flight + the "interaction playbook" prose
  phrase) applied uniformly, and the matching guard test (t-4) iterates the five as one
  set — splitting would fragment an atomic, single-concept change.
  Suggestion: keep as one task; the homogeneity justifies it. No action needed unless the
  per-prompt pre-flight shapes differ enough to need bespoke edits.

- [test-contract] t-2's contract guards the "## Interaction" heading and the
  propose-and-react / one-decision-per-turn phrasing, but NOT the placement the locked
  `readme_section` decision mandates ("right after ## Concept"). The README guard test in
  t-4 likewise greps heading + phrasing only. A future edit could move the section above
  Concept or to the bottom and still pass.
  Suggestion: either add a placement assertion to t-4's README test (Interaction heading's
  index falls between Concept and the following ## heading), or consciously accept that
  placement is a one-time authoring concern not worth a regression guard. Not blocking —
  the locked decision is still binding on the author; this is only about test coverage.

## NOTE
- [coverage] Complete. c-1 → {t-3, t-4}; c-2 → {t-1, t-4}; c-3 → {t-2, t-4}. Every
  criterion appears in a `covers` field.

- [locked-decisions] No conflicts. t-3 records pause as a documented exception and resume
  as walking items one at a time, matching `handoff_emitter_exception`; t-1 wires the
  emitter into all five per `scan_command_emitter` + the exception; t-2's section matches
  `readme_section` verbatim. The two deferred items (Go-level linter, pause per-field walk)
  are honored — neither task attempts them.

- [wave-order] t-3 (wave 2, depends_on t-1) is a genuine dependency, not artificial: the
  audit doc records each command's "real current pattern," and the ✅ verdict is only
  truthful once t-1 has actually wired the emitter — recording ✅ before the wiring lands
  would be a false-green audit row. Correctly sequenced. t-4 (wave 3) depending on all of
  t-1/t-2/t-3 is right: it asserts the combined post-retrofit state. t-2 (wave 1) is
  correctly parallel to t-1 — README work needs nothing from the prompt edits.

- [strength] Test contracts are specific and name the surface that breaks: the emitter
  guard, the "interaction playbook" reference phrase, the "## Interaction" heading, and the
  ⬜/🟡/❌ audit markers — no "tests pass" vagueness.

- [strength] The plan reuses the established phase-11/12 test infrastructure: the new
  `interaction_othercmds_test.go` mirrors `interaction_setupcmds_test.go`, and the
  "interaction playbook" phrase matches the existing `interactionRefPhrase` constant and
  the audit markers match `coreLoopAuditSection`'s slicing — so the new guards drop into a
  proven pattern rather than inventing one.

- [strength] t-4 explicitly bakes in `make install` before observing green, honoring the
  hard rule r-01 (assets/ edits aren't live until re-linked) — the exact failure mode that
  has bitten ship runs on this repo.

- [antipatterns] None of the LLM-plan smells are present: no "set up X" task without
  concrete files, no artificial split (t-1's 5 files are one concept), no squashed task
  hiding two concerns, and every referenced file either exists in the repo (verified) or is
  the one new test file t-4 creates.

## Summary
A tight, well-sequenced plan with full coverage and no locked-decision conflicts — the only
substantive note is that the README guard tests heading+phrasing but not the mandated
placement.
