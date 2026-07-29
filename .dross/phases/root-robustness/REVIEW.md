# Plan Review — root-robustness

Reviewed: 2026-07-29
Plan: 9 tasks across 3 waves

## BLOCKING
(none)

## FLAG

- [antipatterns] t-6 leaves `--force` on an *incomplete* root undefined, and an existing test
  already pins the opposite of what the description implies. `TestOnboardCover_ForceRemovesAndRecreates`
  (internal/cmd/onboard_test.go:38) builds exactly the incomplete fixture t-6 is about — a bare
  `.dross/` containing only a sentinel file — runs `onboard --force`, and asserts the sentinel is
  **wiped**. t-6's description says an incomplete root is "adopt[ed] in place, preserving files
  already present". Whether the new incomplete branch runs before or after the `--force` RemoveAll
  decides whether that test stays green, and nothing in the plan says which. t-6's contract covers
  `--force` only against a *complete* root ("still refuses ... unless --force").
  Suggestion: state in t-6's description that `--force` keeps its wipe semantics on both complete
  and incomplete roots (the branch order), and add one contract row pinning it — otherwise the
  executor discovers the collision as a mystery red in a pre-existing test.

- [test-contract] t-2's probe, `dross state show`, cannot distinguish "no root" from "corrupt
  state.json", and the plan gives it no row for the corrupt case. After t-3, `state show` is
  non-zero for three different reasons: absent `.dross/`, incomplete `.dross/`, and a `.dross/`
  that is complete but holds unparseable JSON. The first two are what locked `pause_refusal`
  wants. The third makes `/dross-pause` print "not a dross repo — run `dross onboard`", which is a
  misdiagnosis of a broken state (locked `completeness_check`: a corrupt file "is a real error,
  reported loudly everywhere") *and* a dead-end pointer, since after t-6 `onboard` still refuses a
  complete root. The CLI stays loud; it is the prompt's rendering that flattens three signals into
  one wrong sentence.
  Suggestion: either have the refusal surface the probe's own error text rather than a fixed
  not-a-dross-repo line, or add a t-2 needle stating that the gate distinguishes a failed probe
  caused by a corrupt file from an uninitialised root. One row, not a redesign.

## NOTE

- [coverage] All five criteria are covered: c-1 (t-1, t-9), c-2 (t-3, t-4, t-9), c-3 (t-1, t-6,
  t-7, t-8, t-9), c-4 (t-2, t-9), c-5 (t-5).

- [locked-decisions] No conflicts. `walk_stop` is pinned three ways (t-1 row 3's
  parent-complete/child-incomplete fixture, t-6's explicit "onboard must NOT call LocateRoot",
  t-9 row 5's caller-set row). `completeness_check` is pinned by the `{{{` row requiring FindRoot
  to *succeed*, plus the corrupt-loud rows in t-3, t-4 and t-8. `pause_refusal` is pinned by t-2's
  both-cases-as-separate-needles row and "never instructs running `dross init` or `dross onboard`
  itself".

- [forbidden-actions] No violation. The only project rule is r-01 (`make install` after prompt/Go
  edits), cited explicitly in t-2. `~/.claude/dross/rules.toml` does not exist. Nothing reaches
  outside `go test -count=1 ./...`.

- [antipatterns] Blast radius independently re-derived and the file lists hold. FindRoot has ~29
  non-test callers, but only three swallow it silently via `err == nil` — telemetry.go:32,
  telemetry.go:91, verify.go:311 — and t-8 now owns all three. `ErrNoRoot` appears in exactly four
  non-test files today (root.go, reentry.go, pause.go, rule.go); t-9's allowlist adds status.go and
  state.go, matching t-3 exactly. On test fixtures: the only bare-`.dross` roots that reach FindRoot
  are `chdirDross` (findings_test.go:15) and telemetry_test.go's RepoHash test — cmd_test.go:103 and
  :118 are `Init()` refusal tests that never call FindRoot, cmd_test.go:423's `TestFindRootWalksUp`
  scaffolds via `runCmd(t, Init())` so it stays complete, and onboard_test.go:42 belongs to t-6.
  t-1's five-file list is neither over- nor under-scoped.

- [test-contract] Two small factual slips in t-1's last row, neither load-bearing: `chdirDross` has
  three call sites (findings_test.go:53, :75, :103), not four; and
  `TestTelemetryCover_RepoHashInRepo` starts at telemetry_test.go:106 (:108 is its `MkdirAll`).
  Every other line reference in the plan re-verified exact — root.go, pause.go:47, doctor.go:31/:55/:423,
  onboard.go:37, rule.go:224, telemetry.go:32/:91, verify.go:311, state.go:42/:46, status_test.go:18,
  pause.md:27/:68.

- [coverage] c-3's wording ("a non-hook command ... fails with a non-zero exit") is blanket, and
  two non-hook commands are deliberate carve-outs: `onboard` succeeds by adopting (t-6), and
  `doctor` succeeds at loading and then diagnoses (t-5). Both carve-outs are correct — c-3's own
  "and the repair command" clause presupposes the repair works — and the plan assigns them
  explicitly. Recorded so the verify pass doesn't read c-3 literally and call them regressions.

- [granularity] Thresholds crossed by t-1 (5 files) and t-3 (5 files, two packages). Neither should
  be split. t-1's three test files are fixture fallout from the FindRoot semantics change and must
  land in the same commit or the package goes red; t-3's `internal/state/state.go:46` path-wrap is
  a two-line prerequisite for its own corrupt-file rows and is worthless as a standalone commit.
  Recorded because the threshold was crossed, not because a split is advisable.

- [wave-order] Waves are tight. Every wave-2 task consumes a t-1 output by name (LocateRoot,
  MissingRootFiles, RepairHint, or the new ErrNoRoot-wrapping semantics), t-9 legitimately needs
  all of wave 2 because it pins the post-wave-2 file sets, and t-2 is prompt-text-only so its
  wave-1 placement is real parallelism, not padding. No task could drop a wave.

- [strengths] The contracts name the fix they kill, not the behaviour they restate — "a mutant that
  parses to decide completeness fails here", "moving the swallow into loadState() makes show silent
  and fails here", "an implementation that swallows every loadState error in the handler instead of
  only ErrNoRoot passes every other row in this plan". That last one is the plan reviewing itself.

- [strengths] t-6, t-7 and t-8's verify.go half are second-order consequences found by reading the
  tree, not derivable from spec.toml: onboard.go:37 turns the advertised repair command into a dead
  end on exactly the roots it should repair; rule.go:224's ErrNoRoot swallow silently widens into a
  live c-3 hole the moment IncompleteRootError wraps the sentinel.

- [strengths] t-9 converts a convention into a filename-level source scan with an explicit
  precedence rule ("if the derived set differs, surface the difference as a finding rather than
  widening the allowlist to match"). That is the right shape for a boundary that has to survive
  command 57, and the ambiguity in the prior revision's derive-vs-hardcode instruction is gone.

- [prior-review] All nine flags from the previous round are addressed in this revision: reentry's
  silent case (t-4 row 5), the "compiling green" row restated as a runtime gate (t-1 row 11), t-5's
  equality row scoped to its own output block with the trio/pair split spelled out in the
  description, internal/state/state.go added to t-3 with a row pinning the :46 path wrap, the
  `state touch` corrupt-file row (t-3 row 6), verify.go added to t-8 *and* to t-9's LocateRoot set,
  t-9's derive-vs-pin precedence made explicit, t-2's byte-offset row re-anchored to pause.md:27,
  and t-1's granularity consciously left as-is.

## Summary
No blockers and clear diminishing returns — this revision closes every prior finding, its line
references and blast-radius claims survive independent re-derivation, and the two remaining flags
(`--force` semantics on an incomplete root in t-6, and the `dross state show` probe conflating a
corrupt root with an uninitialised one in t-2) are each a one-row clarification rather than a
structural change; the plan is execution-ready.
