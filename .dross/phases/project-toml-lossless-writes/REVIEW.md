# Plan Review — project-toml-lossless-writes

Reviewed: 2026-09-13
Plan: 6 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [wave-order] t-2 (wave 1) consumes the `op` type that t-1 (wave 1) defines in `internal/project/patch.go` — `diff(old, new *Project) ([]op, error)`. Run in parallel, `patch_diff.go` cannot compile until t-1 lands, so the wave-1 parallelism claim is false: t-2 is a wave-2 task in disguise, or t-2 must own the `op` declaration itself.
  Suggestion: either move t-2 to wave 2 with `depends_on = ["t-1"]`, or move the `op` struct (and the append-element / delete-element variants) into t-2's `patch_diff.go` so t-1's `apply` consumes a type t-2 owns — then both genuinely sit in wave 1. Pick one and say which; don't leave it to whoever executes first.

- [antipattern: one task that should be two] t-1 carries 21 test contracts across four files and three separable concerns — the line classifier (`indexDoc`, 6 `TestIndex*` contracts), the editing engine (`apply`, 14 `TestApply*` contracts) and `renderValue` (1 contract). The indexer is a strict prerequisite of `apply` and independently testable, and it is the riskiest code in the phase (multi-line strings, CRLF, quoted/dotted keys). One atomic commit for all of it means an indexer bug and an apply bug land in the same diff.
  Suggestion: split into t-1a "index project.toml lines" (`patch.go` indexDoc + `TestIndex*` + the fixture) and t-1b "apply ops + renderValue" (`patch_value.go`, `TestApply*`, `TestRenderValueMatchesEncoder`), t-1b depending on t-1a. Nothing else in the plan changes.

- [locked-decision / t-5 clause (c)] t-2 converts each `*Project` to a generic tree via `toml.Marshal`, but t-5's pin only counts `toml.NewEncoder` call sites inside `internal/project` ("exactly one … its enclosing FuncDecl is encodeFresh"). `toml.Marshal` IS an encoder: a future "re-encode fallback" written as `toml.Marshal(p)` inside `internal/project` passes `TestEncodeFreshIsTheOnlyEncoder`, which is exactly the regression c-3 exists to prevent. Not a locked-decision conflict (same BurntSushi dependency), but the pin has a hole the plan's own design creates.
  Suggestion: t-2's tree conversion should go through the one encoder (`encodeFresh` bytes → `toml.Unmarshal`), and t-5 clause (c) should count `toml.Marshal` alongside `toml.NewEncoder` inside `internal/project`, with zero allowed. Since t-2 lands before t-3 creates `encodeFresh`, t-2 needs a local encode helper that t-3 then folds into `encodeFresh` — state that hand-off in t-3's description so it isn't dropped.

- [wave-order] t-6 (wave 4) does not strictly need t-4's output. The README subsection needs nothing from code; the ARCHITECTURE.md `file:line` pointers need `internal/project/patch.go` (t-1) and `Save` (t-3). The stated rationale ("claim written only once the tests that prove it exist") is a sequencing preference, not a data dependency — t-6 could run in wave 3 beside t-4 and t-5.
  Suggestion: keep it if the author wants the ordering as a discipline (it costs one wave on a pair-mode phase, which is nothing); otherwise drop t-6 to wave 3 with `depends_on = ["t-3"]`.

## NOTE
- [files] t-4's description centres on "one fixture project.toml seeded with …" but `files` lists only `internal/cmd/project_lossless_test.go`. Fine if the fixture is a string constant in the test; if it is meant to be a `testdata/` file, add it to `files` so the executor doesn't reach for t-1's `internal/project/testdata/lossless.toml`, which has a different shape (no go.mod sibling, no three-lane [runtime]).

- [accuracy] t-3 says "onboard's overwrite of an existing project.toml becomes a patch as a consequence." Checked `internal/cmd/onboard.go:60-95`: `--force` does `os.RemoveAll(root)` first, so `Save` sees an absent path (→ `encodeFresh`); adopt mode keeps an existing file and never calls `Save`. No onboard path patches. Harmless, but the sentence describes a case that doesn't occur.

- [design] t-1 specifies key insertion indent as "copying sibling indent (header indent + indent unit when empty)" but never says where the indent unit comes from — detected from the file, or BurntSushi's fixed `"  "`. `TestApplyNewSubtableMatchesEncoderLayout` only pins the repo's own 2-space shape. A hand-written flat (no-indent) project.toml is the case that will surface the choice; it does not violate c-1 (only NEW lines are affected) so this is an executor decision, but it should be a stated one.

- [behaviour change] Inline-table targets (`state_map = { planned = "x" }`) are refused with a rewrite hint instead of being silently converted as today's whole-file re-encode would. Correct under c-1, and `TestApplyRefusesInlineTableByLine` pins it, but it is a user-visible regression for anyone who hand-wrote an inline table. Worth one line in the README subsection t-6 adds, since the PR body (criteria verbatim) will not mention it.

- [test strength] t-2's `TestDiffNoChangeIsNoOps` diffs `Load(x)` against `Load(x)` — two decodes of the same bytes trivially yield identical trees. It is the round-trip through the patcher in t-3's `TestSaveNoopIsByteIdentical` that actually catches a spurious op; t-2's version is a smoke test, which is fine as long as nobody reads it as the guard.

- [strengths] Three things the plan gets right:
  1. Tag semantics come from the encoder, not a second tag interpreter (t-2's Marshal→Unmarshal tree). I checked the struct tags the contracts depend on — `Board.Enabled` omitempty → delete line, `Repo.SquashMerge` / `Repo.Layout` required → set literal, `Stack.Profile` omitempty → delete — and every contract matches `internal/project/project.go:41,217,223,254`. `TestDiffCoversEveryProjectValueKind` makes a new field shape red the day it lands.
  2. Verify-before-write plus temp/fsync/rename plus `TestSaveFailureLeavesOriginalBytes` closes today's `os.Create` truncate-then-fail shape explicitly, and "zero ops → no write" is pinned by mtime, not by inspection.
  3. c-3 is made structural (t-5's go/ast pin with a FLAG/PASS self-check, same technique as `hermetic_dross_read_test.go`) rather than asserted, and t-4 drives the real cobra commands through `runCmd`/`chdir`/`mustRead` (all present in `internal/cmd/cmd_test.go:26,44,473`) over a fixture that carries every c-2 hazard at once. Every referenced helper, command (`stack apply`, `test lane add/edit/remove`, `issue enable/disable`, `board.state_map.*` / `board.fields.*` dotted writes), doc anchor (`README.md:148 ### Updating`, ARCHITECTURE.md project-config section with existing file:line pins at lines 234–239) and `ship.BuildPRBody` criteria rendering exists as described.

## Summary
No blockers; coverage is complete and no task contradicts a locked decision — fix the t-1/t-2 wave dependency and close the `toml.Marshal` hole in t-5's encoder pin before executing, and consider splitting t-1.
