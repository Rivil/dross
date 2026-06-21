# Plan Review — 08-language-profiles

Reviewed: 2026-06-20
Plan: 6 tasks across 2 waves

## BLOCKING
(none)

## FLAG
- [coverage / c-2 CLI clause] c-2 has two halves: (a) four profiles ship embedded
  via go:embed, and (b) each "appears in 'dross stack list' and loads cleanly via
  'dross stack show <id>'". Only half (a) is tested. t-2's contract proves the
  TOMLs load via stack.Embedded(), but no task drives the actual CLI commands. I
  confirmed `stack list` / `stack show` (internal/cmd/stack.go:50-91) are fully
  data-driven over stack.LoadAll() — new profiles auto-surface with zero code
  change — so the clause is *mechanically* guaranteed once valid TOMLs embed. The
  risk is narrow: a profile that loads via Embedded() but, say, has an empty Title
  or an id that collides could still misrender in `list`/`show` without any test
  catching it.
  Suggestion: either add one assertion to the t-3/t-4 wave (or an existing
  internal/cmd/stack_test.go case) that `stack list` output contains each of the
  four ids and `stack show <id>` round-trips, OR record explicitly in the plan
  that c-2(b) is satisfied transitively by the data-driven CLI + t-2's embed-load
  proof, so the verifier doesn't read it as a coverage gap.

## NOTE
- [coverage] Every criterion appears in at least one task's `covers`:
  c-1 (t-1, t-3), c-2 (t-2), c-3 (t-4, t-5), c-4 (t-2, t-6), c-5 (t-1, t-3, t-4).
  No missing coverage.
- [locked-decisions] All five locked decisions are honored by the plan and, where
  relevant, defended by a test: profile_id_equals_lang (t-2 + t-4 negative case),
  sqlfluff_dimension=dead-code (t-2 + t-4 cosmetic-exclusion guard), seed_runtime
  with SQL having none (t-6), security_agnostic_only (t-5 equality assertion),
  tool_loadout per-stack analyzers (t-2 + t-4). No contradictions found.
- [antipatterns / file existence] Verified every referenced file: detect.go,
  detect_test.go, runtime_test.go (internal/stack), catalog_test.go
  (internal/quality, internal/security) all exist; the four profiles/*.toml are
  created by t-2. No phantom files.
- [test-contract specificity] Contracts are unusually strong — each names the
  exact surface that breaks and frames it as a mutation ("deleting any one extLang
  line drops that language and the test fails", "dropping dcm from dart.toml fails
  the dart row", ScannersFor "EQUALS {gitleaks, semgrep, trivy} exactly —
  equality, not membership"). I cross-checked the agnostic sets against source:
  quality agnostic = {scc, jscpd} (catalog.go:96), security agnostic = {gitleaks,
  semgrep, trivy} (catalog.go:52) — both match the contracts. substantive dims
  include complexity and dead-code (catalog.go:36-39), so the dimension choices
  are valid.
- [wave order] Sound. Wave 1 = t-1 (extLang) and t-2 (profiles), genuinely
  independent. t-3 needs both → wave 2. t-4/t-5/t-6 each depend only on t-2 (in
  wave 1), so they cannot drop to wave 1; wave 2 is the earliest legal placement.
  No task is parked in a later wave than its dependencies require.
- [granularity] t-2 touches 4 files but they are one layer (embedded data) and one
  conceptual unit (the four profiles) — below the 5-file split threshold and not an
  artificial split. The four separate test tasks (t-3..t-6) map cleanly to distinct
  surfaces (detection / analyzers / scanners / runtime) rather than being granularity
  inflation. No merge/split candidates.
- [forbidden actions] Only project rule is r-01 (run `make install` after editing
  prompts/Go code). No task violates it; it is an execution-time reminder, not a
  plan-structure constraint. No global rules.toml exists. No violations.
- [implementation detail worth flagging to executor, not a plan defect] AnalyzersFor
  / ScannersFor resolve the profile via stack.LoadAll() (catalog.go:124,
  security/catalog.go:79), not stack.Embedded(). LoadAll merges embedded + user-dir
  profiles, so the t-4/t-5 tests work — but a stray profile in the dev's
  ~/.claude/dross/profiles/ could perturb a strict equality assertion. Tests that
  go through AnalyzersFor/ScannersFor inherit this; acceptable, just be aware.

## Summary
Solid, well-coordinated plan with rigorous mutation-framed test contracts and full
locked-decision coverage; the only soft spot is that c-2's CLI-surfacing clause is
satisfied transitively rather than directly tested — worth one assertion or an
explicit note before verify.
