# Plan Review — survivor-lifecycle

Reviewed: 2026-08-10 (second pass, post-amendment)
Plan: 9 tasks across 4 waves

## BLOCKING

(none)

Coverage is complete: c-1 (t-5, t-8, t-9), c-2 (t-2, t-5, t-7, t-8), c-3 (t-2, t-7, t-8),
c-4 (t-1, t-5), c-5 (t-6, t-7, t-8), c-6 (t-3), c-7 (t-4, t-5, t-7, t-9). All six locked
decisions survive the amendment: `acceptance_store` (t-2, t-7), `survivor_identity` (t-1,
t-5), `acceptance_granularity` (t-2), `routed_state_source` (t-5, t-7),
`cli_namespace` (t-7), `no_acceptance_ttl` (t-6) — each pinned by at least one contract
line, two by name. No rule violation: `runtime.mode = "native"`, and r-01's `make install`
obligation is restated in t-9, the only task touching `assets/`. Every anchor the plan
quotes exists: `assets/prompts/verify.md:78` carries both the "survivor-lifecycle phase to
route" sentence and the "Do not re-list the out_of_scope entries" section;
`TestReadmeStatesScopingGuarantee` is at `internal/cmd/verify_prompt_test.go:162`;
`rules.Builtins` (`internal/rules/rules.go:117`) still holds exactly 3 entries;
`EnforceSubcommandKnown` and `ignoresPath` both exist. No task depends on a file that no
earlier task creates.

## FLAG

- [wave order] t-3 is wave 3 and `depends_on = ["t-7"]` — but t-7 is *also* wave 3. Every
  other dependency in this plan crosses a wave boundary, and the repo's own rule
  (`deriveWave`, `internal/phase/plan_edit.go:54`) is "one past the deepest wave among its
  depends_on". By that rule t-3 is wave 4. This does not deadlock — `NextRunnable`
  (`internal/phase/phase.go:303`) gates on dependencies before comparing waves — but the
  label is wrong, and it makes t-3 (wave 3) pre-empt t-9 (wave 4) even though t-3 derives
  one wave *later* than t-9's own deps. Separately, judging whether the dependency is real:
  it is not a build dependency. t-3 adds a static string to `rules.Builtins` and deletes a
  line from `.dross/rules.toml`; nothing in it imports or invokes t-7's CLI. It is a
  coherence dependency only — "don't let the rule text name verbs that don't ship yet" —
  which inside a single squash-merged phase has no external observer.
  Suggestion: pick one. Keep the dependency and make t-3 wave 4 next to t-9, or drop it and
  return t-3 to wave 1 with a one-line note that policy text may name verbs landing later
  in the same phase. The current state is self-inconsistent either way.

- [wave order / dependencies] t-8 has no `depends_on` on t-4, but it needs t-4's output.
  t-8's summary contract asserts real routed counts in a live verify run; a routed survivor
  is only identifiable via the `survivor` field t-4 adds to `phase.Deferred`
  (`internal/phase/phase.go:248`). Without t-4 the routed bucket is structurally always
  zero. Ordering happens to work (t-4 is wave 1), so this is a declared-graph gap rather
  than an execution hazard — but it is the exact class of gap the amendment fixed for
  t-1/t-2 and did not carry over. t-5 has the same question and, unlike t-2, does not
  declare its independence: if the classifier takes `[]phase.Deferred`, t-5 needs t-4 too;
  if it takes an abstract routed-key map, say so.
  Suggestion: add t-4 to t-8's `depends_on`, and either add it to t-5's or state in t-5's
  description that routed input arrives as opaque keys — the same sentence t-2 got.

- [antipattern/missing source] t-8's description names one input: "Load the repo-root
  store". Nothing says where routed entries come from, yet t-8 owns the printed summary
  whose counts must include them. The mechanism exists and is in-package —
  `collectDeferred(root)` at `internal/cmd/deferred.go:40` walks every phase spec — so this
  is cheap to close, but as written an implementer can satisfy every t-8 contract line
  except the routed count and never notice.
  Suggestion: name `collectDeferred` (or whatever feeds routed state) in t-8's description
  alongside the store load.

- [test contract] The lifecycle field has two names and no pinned wire form. t-1 and t-5
  both call the field `Lifecycle`; t-5's contract observes `state == ""` and t-8's observes
  "the resolved key and state". Worse, the two carriers serialize differently:
  `mutation.Mutant` (`internal/mutation/adapter.go:89`) has **no** json tags — it reaches
  tests.json through `languages[].mutation.surviving` as `"File"`, `"Line"`, `"Op"` — while
  `verify.OutOfScopeMutant` (`internal/verify/verify.go:96`) is fully tagged lowercase. Add
  `Key/Lifecycle/Note` to both as described and the same three fields appear in one
  tests.json in two casings, under a key no contract line names.
  Suggestion: fix the field name in the prose, and add a contract line pinning the wire
  names on both types (or explicitly accept the pre-existing casing split).

- [test contract] t-3's r-02 regression line will fire on an unrelated future change. It
  asserts this repo's `.dross/rules.toml` has no rule "with id r-02 **or** text containing
  'Pre-existing faults are not furniture'". `nextID` (`internal/cmd/rule.go:241`) starts at
  `len(set.Rules)+1` and returns the first free slot — after r-02 is deleted the set has one
  rule, so the very next `dross rule add` in this repo allocates `r-02` again and trips the
  id half of the assertion with entirely unrelated text.
  Suggestion: assert on the text only. The id carries no meaning once the rule is gone.

## NOTE

- [strengths] The three substantive prior flags were fixed at the mechanism level, not
  papered over. t-7 does not just name the destination spec — it says *why* the assertion
  must name the exact file ("deferred list --target collects across specs and cannot
  distinguish them"), which is the reasoning that made the flag necessary. t-2's "keys on an
  opaque string, so it builds independently of t-1's resolver — the wave-1 parallelism is
  deliberate, not an undeclared dependency" is the right shape of answer: it converts an
  ambiguity into a design constraint an implementer can violate visibly.

- [strengths] The t-1 / t-5 split of the struct change is correct as re-drawn, and I checked
  the reason it works: t-1's round-trip line now names only `mutation.Mutant`, which reaches
  tests.json via `languages[].mutation.surviving` without touching `verify.go` — so t-1
  genuinely does not need the file the prior review asked for, and t-5, which owns
  `verify.go`, picked up the `OutOfScopeMutant` half.

- [strengths] c-4's false-suppression half is now contracted twice at two levels — t-1 at
  the hash ("replacing the line's source text while holding file+line+op fixed still
  produces the old key") and t-5 at the classifier ("suppressed instead of re-emitting as a
  new survivor"). A unit-level and an integration-level assertion of the same safety
  property is the right redundancy for the one bug class that silently hides real defects.

- [granularity] t-5 is the phase's concentration point: 4 files, 4 criteria, 13 contract
  lines — a new classifier, a schema change to `OutOfScopeMutant`, and the out_of_scope
  wiring. It does not trip the split thresholds (one layer, under 5 files) and the pieces
  are genuinely coupled, so no action — but it is where a slip costs the most, and it is
  the task most likely to be committed half-done.

- [routed_state_source] The lock says routing reuses "the existing `dross deferred route`
  machinery"; t-7 states plainly that no append path exists and that it adds one. That is a
  reuse of the deferred *data model and re-surfacing pathway* rather than of the `route`
  command itself. Not a contradiction of the lock's substance, and the plan is honest about
  it rather than implying a reuse that isn't there — recorded so the gap between the lock's
  wording and the work is not rediscovered mid-execution.

- [scope] Still no task drains the existing ~76-entry backlog the `acceptance_granularity`
  rationale cites, and no criterion asks for one. Unchanged from the prior review; reads as
  deliberately a later phase's work.

## Prior review disposition

- [granularity/files] t-1 missing `internal/verify/verify.go` for `OutOfScopeMutant` —
  **addressed**. The `OutOfScopeMutant` half moved to t-5, which lists `verify.go`; t-1's
  description now says so explicitly and its round-trip line names only `mutation.Mutant`.
- [test contract] c-4's false-suppression half untested — **addressed**. New t-1 line
  ("if the key stops being text-derived…") and new t-5 line ("if the false-suppression guard
  breaks…").
- [antipattern/files] `survivor route` had no reuse and no named write target —
  **addressed**. t-7 now names the CURRENT phase's spec.toml, acknowledges there is no
  append path today, and adds two contract lines: exact-file assertion (with the reason) and
  the no-current-phase path.
- [wave order] t-3 naming verbs that ship later — **partially addressed**. Moved from wave 1
  to wave 3 with `depends_on = ["t-7"]`, but wave 3 is t-7's own wave, so the fix
  contradicts the repo's own wave derivation and the dependency itself is coherence-only.
  See the first FLAG.
- [wave order] t-1 / t-2 undeclared coupling — **addressed**. t-2's description declares the
  opaque-string key and the deliberate parallelism.
- [test contract] routed destination label untested — **addressed**. New t-5 line asserting
  a routed record reaching tests.json with an empty target fails.

## Summary

The amendment fixed five of six prior flags cleanly and at the right level; what remains is
one half-fix (t-3's wave number now contradicts the tool's own `deriveWave` rule) plus four
new gaps the amendment surfaced — a missing t-4 dependency on t-8, an unnamed routed-state
input in t-8, a lifecycle field with two names and no pinned wire form, and an r-02
assertion that `nextID` will trip on an unrelated future rule.
