# Plan Review — deferred-item-routing

Reviewed: 2026-06-26
Plan: 5 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [contract-specificity / antipattern] t-3 makes `dross deferred route <phase> <idx>`
  addressable by positional index, but the JSON shape it asserts only pins a `source`
  field — there is no test that `deferred list --json` exposes the per-entry index that
  `route` consumes. The spec.md flow in t-4 (`list --json` → `route <phase> <idx>`) has an
  unverified handoff: nothing guarantees the prompt can recover a stable idx to pass to
  route, and a re-ordered/edited [[deferred]] array could shift it.
  Suggestion: add a t-3 test_contract entry pinning the addressing handle (idx, or a stable
  id) in `list --json`, so the list→route handoff the prompt depends on is actually covered.

- [coverage / scope] t-2 adds a dangling-target guard (validate reports a problem when a
  target names no phases/<slug> dir or milestone.phases entry). No criterion requires
  validate to *fail* on a dangling target — c-1 only requires validate to *pass* whether
  target is present or absent, which t-1's cmd_test already covers (sibling-without-target
  exits 0). The new guard is reasonable integrity for the re-surface model but is scope not
  traceable to any criterion.
  Suggestion: confirm the dangling-target check is wanted; if so it is arguably its own
  small concern, if not it is gold-plating beyond c-1.

- [wave-order] t-4 and t-5 are in wave 3 (depends_on t-3), but their test contracts are
  prompt-text assertions (grep spec.md / inbox.md for command strings) — the same
  `*_prompt_test.go` pattern used elsewhere, which does not execute or compile against t-3's
  command. They consume no wave-2 *output*, so mechanically they could drop to wave 2. The
  dependency is logical coherence (don't document a command before it exists), not a strict
  output need.
  Suggestion: keep as-is if you value the coherence ordering, but note these are not hard
  data dependencies.

- [granularity / squash] t-3 is the heaviest task: a new `deferred.go` implementing two
  distinct surfaces (read-only `list` with 5 flags + JSON + source column, and a disk-mutating
  `route`), plus main.go wiring, carrying 4 criteria (c-3/c-4/c-5/c-6). It is under the 5-file
  / 3-layer mechanical threshold and the two subcommands share scan/parse infrastructure, so
  bundling is defensible — but route (write) and list (read) are genuinely separate surfaces.
  Suggestion: consider splitting `route` into its own task so the write path gets isolated
  commit + contract; borderline, not required.

## NOTE
- [locked-decision] d4 (deferred_list_contract) locks the contract of `dross deferred list`
  only; it does not say the deferred command is list-only, nor that target is stamped by
  hand. t-3 adds a sibling `route` subcommand. This is a defensible superset, not a conflict:
  c-3 ("stamps target=<slug> on the deferred entry") requires a write mechanism on disk, and
  a `route` CLI is a sounder choice than a prompt hand-editing a specific [[deferred]] entry's
  TOML. Not blocking. If you want to honor d4's spirit explicitly, record route in the decision.

- [strength] Test contracts are unusually strong — every entry names the exact surface and the
  mutation that breaks it ("if omitempty is dropped … the round-trip assertion fails";
  "if --routed is not the exact complement of --someday …"). This is the mutation-oriented
  style the verify step rewards; no vague "tests pass" contracts anywhere.

- [strength] Coverage is complete and the dependency spine is clean: every criterion (c-1..c-6)
  maps to at least one task, and the whole graph roots correctly at the t-1 schema change that
  everything else needs. Referenced files/commands all check out (cmd_test.go exists,
  milestone.Phases exists, `dross milestone add <v> phases <slug>` is real, prompt-test pattern
  matches existing `*_prompt_test.go`).

## Summary
A genuinely solid plan — complete coverage, sharp contracts, correct waves — with no blockers;
the one finding worth fixing before execute is pinning the list→route index handoff (t-3 JSON
shape) that the spec.md flow silently depends on.
