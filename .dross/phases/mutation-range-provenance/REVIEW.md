# Plan Review — mutation-range-provenance

Reviewed: 2026-09-14
Plan: 7 tasks across 4 waves

## BLOCKING
(none)

## FLAG
- [antipattern/files] t-6 lists `internal/cmd/verifyscope.go` and `verifyscope_test.go` as if new ("New cobra subcommand ... files = [...]"). Both already exist: `verifyscope.go` holds `phaseScope` (:32), `gitReason` (:97) and `containScope` (:119) — the scope-derivation code, not a command. An executor that treats it as a fresh file clobbers the scope pipeline t-7 depends on.
  Suggestion: say explicitly "append the subcommand to the existing verifyscope.go" or name a new file (e.g. `verify_scope_cmd.go`) so the intent is unambiguous.

- [antipattern/files] t-6 introduces `verify.Provenance{Phase, GeneratedAt, Files, Hunks, Legs[]...}` — a type in package `verify` — but its files list contains no `internal/verify/*` file. Nothing earlier in the plan creates it (t-3's `range_provenance.go` defines EffectiveRange/RangePlan only).
  Suggestion: add `internal/verify/range_provenance.go` to t-6's files, or define the projection struct in `internal/cmd` and drop the `verify.` prefix from the contract.

- [test-contract] t-7's second contract (`TestRangeKeyMatchesScopeKeysUnderAWorkdir`: "runArgs on a Stryker with Workdir "web" ... emits `src/a.ts:1-30`") calls the unexported `runArgs`/`rangeKey`, so it can only live in `internal/mutation` — but t-7's files are `internal/cmd/verify_range_e2e_test.go` and `internal/cmd/verify_test.go`. It also duplicates a test t-1 already lands: the pick's `TestRunArgsEmitsLineRanges` uses `Stryker{Workdir: "web"}`, a range keyed `web/src/lib/server/recipe.ts`, and asserts `--mutate src/lib/server/recipe.ts:120-124`.
  Suggestion: drop the second contract from t-7 and cite `TestRunArgsEmitsLineRanges` (t-1) as the pin, or move it to t-1's file list in `internal/mutation/range_test.go`.

- [antipattern] t-4 says `collectDetached` "builds its gremlins leg through the same PlanRanges call over the gremlins adapter". The detached-collect path has no adapter in hand — it reads the remote report and hand-builds the `LanguageRun` at `internal/cmd/verify.go:646`; no `mutation.Gremlins` is constructed there (the only constructor, `mutationTuning.gremlins` at :982, needs projectRoot/project/cacheVars). `PlanRanges(a mutation.Adapter, ...)` needs a value.
  Suggestion: state what is passed — a zero `&mutation.Gremlins{}` is fine since PlanRanges is pure and only type-asserts RangeRunner — so the executor doesn't wire the full tuning constructor into a path that must not run anything.

- [coverage/criterion-fidelity] c-5 says `--json` "emits the same record verbatim". t-6 deliberately emits a projection ("a projection, not the raw tests.json bytes ... copied field-for-field"). Value-identical, but not verbatim; a verifier reading c-5 literally can mark t-6 short. c-5 is a criterion, not a lock, so this is a reading, not a conflict.
  Suggestion: either marshal the loaded record's `Scope` and per-leg `{Name,Tool,Files,Ranges,WholeFile}` from the actual `*Tests` value (still a projection, but declared as such), or note in the task that "verbatim" is read as "same values, no re-rendering" so the verify step judges against that reading.

- [locked-decision] `landing_method` has a second clause — "delete the feature branch local + origin once this phase ships" — with no home in the plan. Both `feat/mutate-changed-lines` and `origin/feat/mutate-changed-lines` still exist (as does `fix/stryker-mutate-glob-escaping`, likely the same family). Not blocking: the lock itself defers it to post-ship.
  Suggestion: record it as a ship-time action (handoff or the ship checklist) so the "must not linger as a second history" intent isn't lost when the phase completes.

- [wave-order] t-5 and t-6 are both wave 4 and both edit `internal/cmd/verify.go` (t-5: `printScopeSummary` ~:1419; t-6: `AddCommand` registration ~:162). Distinct regions, so a sequential executor is fine, but parallel wave execution would collide on one file. t-7 lists `internal/cmd/verify_test.go` too though it only *embeds* `stubMutationAdapter` from it — if it edits nothing there, drop it from files.
  Suggestion: either order t-5 → t-6 within the wave or accept the conflict risk explicitly.

- [granularity] t-6 touches 6 files across the cmd package, the verify prompt, and README. The three doc-side edits (verify.md paragraph, verify_prompt_test.go, README row) are small and cohesive with the command, so this is a mild split candidate rather than a problem.
  Suggestion: leave as is unless the executor wants a separate docs commit.

## NOTE
- [test-contract] t-3 and t-4 diverge on the nil-scope case only implicitly: t-3 says "a RangeRunner with Hunks nil → every file scope-has-no-hunks + one Degraded line", t-4 says "Run() with nil scope → legs with no ranges/whole_file". Consistent if `PlanRanges(nil *Scope)` returns an empty plan while `&Scope{Hunks: nil}` classifies — but t-3 never states the nil-pointer branch. `verify.Run` has no production caller (`phaseScope` returns nil only alongside an error, which aborts verify), so this is test-only surface; worth one sentence in t-3 so the executor doesn't make PlanRanges(nil) emit scope-has-no-hunks and break t-4's test.
- [antipattern] Line anchors drifted slightly: `collectDetached` is at `internal/cmd/verify.go:526`, the leg it hand-builds is at ~:646 (the number the plan cites). Harmless.
- [granularity] t-1's 7 files are dictated by the two picks, not by authoring choice — not a split candidate. Verified read-only: `git diff 47ca955 21e354b | git apply --check` is clean on HEAD, so the *second* pick also lands without conflict (the plan only claims this for the first). The stated idiom adaptation (`head.contains(strykerDropWarningText)` vs the pick's `strings.Contains(head.buf.String(), ...)`) is real — the pick's diff carries the `strings.Contains` form and `headBuffer.contains` exists at `toolfail.go:184`.
- [locked-decision] t-5's `LegSummary.Ranges []string` / `WholeFile []string` carry file paths under tags (`ranges`, `whole_file`) outside `pathShapedTags`, so the pathfence walker won't demand a registry entry. This matches the artifact_scope lock's intent (fields that reach `os.*`) and the precedent of `Scope.Hunks` (a path-keyed map, unregistered). Fine — just be aware it is a vocabulary sidestep, not a registration.
- [test-contract] t-2's reasoning on :81 checks out: the survivor is the `+` joining the two Fatalf string literals; a string `-` doesn't compile, so once the line is covered gremlins should report NOT VIABLE rather than survived. The prior run listed it because coverage was count=0 (gremlins never attempted it). The plan correctly makes tests primary and accept conditional on the drain output being read.
- [strengths] (1) Every contract is exact-value and names the test that fails and what the mutation would be — `{100,104}→{75,129}`, `pad = 25`, `"whole-file" on the same line as "adapter-lacks-range-runner"` — which is what makes them verifiable rather than aspirational. (2) t-3's single `Range.Valid()` shared by verify and stryker, pinned by `TestVerifyNeverDispatchesARangeStrykerWouldRefuse`, closes the exact class of bug (dispatch map and argv disagreeing) that provenance is supposed to make impossible; malformed-before-pad is the right order and the plan says why. (3) The fallback_severity lock is mapped 1:1 to a test per arm (`TestFileAbsentFromHunksIsInformational`, `TestNoRangeRunnerIsInformational`, `TestHunklessScopeOnACapableAdapterDegrades`), and the toolfence/pathfence vocabularies are handled up front instead of discovered red. (4) t-7 is the one test that would catch a path-vocabulary mismatch hidden behind an informational fallback — the plan names that failure mode explicitly.

## Summary
No blocking findings; the plan is tight and verifiable, with fixes needed in t-6 (existing-file collision, missing verify-package file) and t-7 (a contract that can't live where it's filed and duplicates a t-1 test), plus a one-line home for the branch-deletion clause of the landing_method lock.
