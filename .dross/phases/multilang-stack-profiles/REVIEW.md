# Plan Review — multilang-stack-profiles

Reviewed: 2026-06-26
Plan: 4 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [wave-order] t-1 and t-2 are both in wave 1 (parallel) yet both list
  `internal/stack/detect_test.go` in their `files`. Two same-wave tasks editing the
  same test file collide if executed in parallel; the wave boundary implies a
  parallelism the file layout can't safely support.
  Suggestion: serialize them (move t-2 to wave 2 before t-3, or make t-2 depend on t-1),
  or split the detect_test.go additions so each task owns a distinct file (e.g. t-2's
  stub/regression assertions go in profile_test.go, which it already touches).

- [granularity] t-1 touches 5 files and t-2 touches 9 files, both tripping the 5+-file
  heuristic. The trigger is data-file count (3 full + 7 stub profiles), not layer span or
  logical complexity — each task is cohesive (one shape of profile + its verification),
  so a split would be artificial. Flagged for honesty, not as a real defect.
  Suggestion: leave as-is unless execution wants smaller commits; if so, t-2's stubs split
  cleanly by language but gain little.

- [antipattern] t-3 rewires `DetectLanguages(root)` to derive from `LoadAll()`, which reads
  `~/.claude/dross/profiles/`. This turns a previously pure tree-function into one coupled
  to global `$HOME` state. Pre-existing exact-assertion tests that aren't in the plan's
  file list — notably quality `TestDetectLanguages` (asserts exactly `["go"]`) and
  `TestDetectLanguagesUnknownExt` (asserts zero) — become environment-sensitive. (Low real
  risk today: no `~/.claude/dross/profiles/` dir currently exists, so LoadAll = embedded
  only; but the coupling is unguarded and the plan doesn't call out isolating HOME.)
  Suggestion: in t-3, add a `t.Setenv("HOME", t.TempDir())` (or equivalent) to the affected
  exact-match tests so the regression suite is deterministic regardless of the runner's home.

## NOTE
- [test-contract] t-2 row 2 names `TestEmbeddedProfilesAllLoad`, whose natural home is the
  existing `internal/stack/embed_test.go` — which is NOT in t-2's `files` list. The author
  can land it in `profile_test.go` (listed) instead; just confirm placement at execution.
- [locked-decision] ext_clash_resolution's "Detect ties lexicographic by id" is satisfied
  only as an emergent property: `Detect` tie-breaks by priority then keeps the first-seen
  profile, and `LoadAll`/`Merge` happen to return profiles id-sorted. Detect itself does not
  enforce lexicographic ordering. No plan conflict (the plan keeps Detect winner-take-all),
  but the guarantee rests on caller-side sorting, not on Detect.
- [strength] Test contracts are mutation-style and name the exact surface + the specific
  test that breaks (TS-hijack guard, `TestTsNotHijackedBySvelte`, `TestNoDuplicateExtLangMap`,
  `TestStubMinimalShapeLoads`). Strong, falsifiable contracts throughout.
- [strength] Wave dependencies are genuine: t-3 truly needs all profiles present before
  deleting extLang (else the union derivation drops languages and regresses c-2), and t-4
  truly needs t-3's rewrite before the user-overlay path can reach DetectLanguages. No
  artificial serialization.
- [strength] The amended ext_clash_resolution (union for DetectLanguages, winner-take-all for
  Detect) is correctly threaded into t-3, and the plan proactively guards the real
  svelte/typescript `.ts` clash rather than discovering it at runtime.
- [strength] t-2 proactively repairs `TestDetectUnsupportedFixture` (the foo.rb fixture that
  shipping ruby.toml would otherwise legitimately match) in the same commit — catches a real
  test-fixture regression the new profile introduces.

## Summary
Solid, well-guarded plan with genuine wave dependencies and falsifiable contracts; nothing
blocking, but fix the wave-1 same-file (detect_test.go) collision between t-1/t-2 and isolate
HOME in the exact-match DetectLanguages tests before execution.
