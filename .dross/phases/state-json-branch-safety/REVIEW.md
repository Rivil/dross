# Plan Review — state-json-branch-safety

Reviewed: 2026-08-01
Plan: 13 tasks across 3 waves

## BLOCKING

- [coverage] **Untracking state.json breaks 11 fixture call sites that no task owns.**
  `completeFixture` (`internal/cmd/phase_test.go:329`, also used by `phase_base_truth_test.go`)
  builds its repo with `runCmd(t, Init())` at line 337 and then does
  `mustGit(t, dir, "add", filepath.Join(".dross", "state.json"))` at line 383. Once t-1's
  `ensureDrossGitignore` fires from `Init()`, that explicit-path add hard-fails with
  `paths are ignored by one of your .gitignore files`. The same shape recurs at
  `phase_base_truth_test.go:51`, `phase_test.go:1057`, `:1140`, `:1281`, `:1799`, `:2023`,
  and the read side at `phase_test.go:900` (`git show origin/main:.dross/state.json`) and
  `phase_test.go:1056` / `:1139` (`git checkout <branch> -- .dross/state.json`).
  Only `ship_test.go:333` is owned (by t-6); `phase_test.go` and `phase_base_truth_test.go`
  appear in no task's `files`. t-1 cannot commit green under the execute test-gate.
  Suggestion: give t-1 explicit ownership of `internal/cmd/phase_test.go` and
  `internal/cmd/phase_base_truth_test.go`, with a contract item covering the fixture's
  squash-simulation (it currently *relies* on state.json riding onto origin/main —
  see the comment at `phase_test.go:365-368`), or split a dedicated fixture-migration
  task into wave 1 that t-6/t-8/t-11/t-12 depend on.

- [coverage] **t-2 directly contradicts an existing test in a file it does not own.**
  `TestFrictionWindow_IncompleteRoot` (`internal/cmd/friction_window_test.go:30-54`)
  removes `.dross/state.json` after `Init()` and asserts `dross task list` *fails*, naming
  the file and carrying `RepairHint` + `dross onboard`. t-2 makes exactly that case
  auto-materialize and succeed. `friction_window_test.go` is in no task's `files`.
  Suggestion: add it to t-2 and state the replacement contract — that the friction
  window now covers a missing `project.toml` only, and that a missing `state.json`
  is no longer an incomplete-root signal.

- [test contract / locked-decision conflict] **t-3's parity assertion cannot hold in CI
  under the `state_tracking` locked decision.** The final contract item is
  "`.dross/project.toml [project].version != .dross/state.json`'s version". After t-1,
  `.dross/state.json` is untracked, so `actions/checkout` in `.github/workflows/ci.yml`
  produces a tree with no `.dross/state.json` at all — the assertion is either red or
  vacuously skipped in the only place it would be enforced. The one-time 0.2.0.0 → 1.2.1.0
  correction the item exists to gate therefore goes unverified.
  Suggestion: re-express the correction as a fixture-level assertion (a temp repo where
  `writeVersion` is called and both files are re-read), plus a separate repo-root check
  that `.dross/project.toml` parses and carries a 4-part version — no cross-file parity
  against a machine-local file.

## FLAG

- [wave order] **Two same-file collisions inside a single wave.** t-2 and t-3 both list
  `internal/cmd/state.go` in wave 1; t-8 and t-10 both list `internal/cmd/milestone.go`
  in wave 2. Waves are the parallelism unit, so both pairs will contend on the same file.
  Suggestion: sequence t-3 after t-2 (t-3 also *conceptually* depends on it — t-2's
  `ensureState` seeds Version from `project.toml`, which is exactly the invariant
  `writeVersion` owns), and move t-8 to wave 1 (see below), leaving t-10 alone on
  `milestone.go` in wave 2.

- [test contract] **t-11 asserts an entry that two of its four commands never write.**
  `milestoneFinalize` (`internal/cmd/milestone.go:143-174`) does the checkout,
  `merge --ff-only`, and both branch deletes without a single `s.Touch` or `s.Save` —
  there is no state load on that path at all. And `runDrossRecovery`'s
  `s.Touch("merged <id>")` sits *after* the delta gate's early return
  (`internal/cmd/ship_recover.go:196-201`), a return that t-6's `:(exclude)` pathspec
  makes materially more reachable (state.json was previously the thing guaranteeing a
  delta — hence the existing comment at `ship_recover.go:183-185`). So
  "plus the command's own entry appended last" fails for `milestone complete --finalize`
  always, and for `ship recover` in the in-sync case.
  Suggestion: either scope t-11 to "every pre-switch entry survives" and drop the
  appended-entry clause, or add `internal/cmd/milestone.go` to its `files` and state the
  Touch as intended work. "Carries any fix it surfaces" with a one-file `files` list is a
  blank cheque the execute gate can't honour.

- [antipattern: underspecified mechanism] **t-5 never says how the squash commit is
  identified.** "compare its patch-id against the squash commit's own first parent"
  presupposes you already know which commit on `main` is the squash of that branch.
  Finding it is the hard half of the task (walk `main`'s commits not in the branch,
  patch-id each, match against the synthesized branch diff — and decide what to do when
  two match, or when the squash was subsequently amended).
  Suggestion: name the resolution step in the description, and add a contract item for
  the ambiguous case (no match vs. multiple matches on `main`).

- [antipattern: underspecified mechanism] **t-7's named source cannot answer the question
  its contract asks.** `changes.json` carries `PR int` (`internal/changes/changes.go:28`)
  — a number, not a merge status. Contract item 2 ("a phase branch whose work IS on
  origin/<main> still prints the shipped-but-unmerged warning") requires deciding
  merged-vs-not. That leaves two options the description doesn't choose between:
  a provider API call, which would introduce a *new network dependency* into `dross status`
  (today's `staleCompletedState` is a purely local `git show origin/...` at
  `internal/cmd/status.go:487` — no network), or a local ancestry/patch-id check, which is
  t-5's detector under another name.
  Suggestion: pick the mechanism in the description. If it's the local check, declare
  `depends_on = ["t-5"]`; t-7 currently depends only on t-1.

- [wave order] **t-7 and t-8 consume no output from t-1.** Both declare
  `depends_on = ["t-1"]`, but neither calls `ensureDrossGitignore` or any symbol it
  introduces — t-8's fixture ("a branch that still tracks .dross/state.json while an
  untracked live copy exists") is something the test constructs directly, and t-7's
  re-sourcing is orthogonal to whether the file is ignored. Both could be wave 1.
  Suggestion: drop t-8 to wave 1 — it also dissolves the `milestone.go` collision with
  t-10. t-7 is the weaker case given the mechanism question above.

- [antipattern: stale-comment coverage gap] **The doc-purge task misses two rationale
  blocks that t-6 falsifies.** `internal/cmd/phase.go:450-462` states that on `--recover`
  "state comes from the base: reload it from the (now checked-out) base working tree" and
  that "runDrossRecovery's own `s.Save(rs)` writes that copy last, so it wins over whatever
  the tree restore put there" — both false once the restore excludes state.json and the
  reset can't touch it. `internal/cmd/ship_recover.go:203-205` ("state.json is inside
  .dross/, so the same `git add .dross/` stages the touch") is likewise inverted. t-13's
  `files` covers `local.go` and `.gitignore` but not `phase.go`; t-6 mentions only "the
  now-false comment about state.json riding the same `git add`".
  Suggestion: name both sites explicitly — `phase.go`'s `--recover` block in t-8's or
  t-13's `files`, `ship_recover.go:203-205` in t-6's description.

- [granularity] **Three tasks at or over the 5-file threshold.** t-1 (5 files: gitignore
  helper + two call sites + repo `.gitignore`), t-6 (5 files, spanning Go and a prompt
  asset), t-13 (6 files spanning README, man page, two prompts, a Go comment, and
  `.gitignore`). t-13 in particular is a pure doc sweep whose only real coupling is to
  t-9's shipped behaviour.
  Suggestion: acceptable as-is if the executor commits per-file; flagged only because
  t-1's blast radius is the one the BLOCKING findings above extend.

- [forbidden actions / CI hygiene] **t-4 edits a workflow without triggering the workflow
  audit, and the new script escapes lint.** The global CLAUDE.md rule requires that any
  edit to `.github/workflows/*.yml` be audited against
  `~/.claude/memory/reference_ci_supply_chain_hardening.md` in the same edit; t-4 says
  nothing about it. Separately, `ci.yml`'s `shellcheck` job runs `shellcheck install.sh`
  only — `scripts/release-version.sh` would ship unlinted, and it is a `set -euo pipefail`
  POSIX script doing grep/sed parsing on untrusted-ish input.
  Suggestion: add the hardening audit to t-4's description, and either extend the
  shellcheck job to `scripts/*.sh` or add a contract item asserting the script is linted.
  (Not BLOCKING: this is a global CLAUDE.md rule, not an entry in the project or global
  `rules.toml`. `.dross/rules.toml` carries only r-01, which the plan honours.)

## NOTE

- [test contract] Contract specificity is the strongest part of this plan. Every one of
  the ~55 items is counterfactual ("if X is dropped, test Y fails: <observable>") and names
  a concrete surface — a file's exit status, a grep count, a history length. There is not a
  single "tests pass" or "covered by integration". t-6's delta-gate item and t-12's
  "sibling sub-test with `git add -f`" are genuinely good: they guard against the assertion
  passing for the wrong reason.

- [forbidden actions] r-01's stale-binary trap is pre-empted correctly. t-2, t-6, t-8 and
  t-12 each open with the in-process cobra-root stipulation, and t-6/t-13 both call out
  `make install` for the `assets/prompts/` edits. t-12's phrasing ("`dross init` in
  particular must exercise the in-package command, not whatever `make install` last
  linked") is exactly the right paranoia.

- [locked decisions] t-3's description claims the repo currently holds `state.json` at
  1.2.1.0 against `project.toml` at 0.2.0.0. Verified — `.dross/project.toml:3` reads
  `version = "0.2.0.0"` and `.dross/state.json` reads `1.2.1.0`. The stated
  "cannot be satisfied incidentally by a later bump" reasoning holds.

- [coverage] All seven criteria are covered: c-1 (t-1, t-8, t-13), c-2 (t-4), c-3 (t-6,
  t-7, t-8, t-11), c-4 (t-3), c-5 (t-12), c-6 (t-9), c-7 (t-5, t-9, t-10). No task
  contradicts `state_tracking`, `version_home`, `migration_scope`, `history_durability`
  or `prune_surface` other than the t-3 contract item under BLOCKING.

- [coverage] c-2's causal chain survives the untracking, which is worth recording because
  it is not obvious: `autoCommitDrossDirt` (`internal/cmd/cleantree.go:34`) stages with
  `git add .dross` — the *directory* form, which skips ignored paths. So a `writeVersion`
  bump to the tracked `.dross/project.toml` still reaches `main` via the dirt gate even
  after t-6 removes ship's explicit `git add .dross/state.json`, while the ignored
  `state.json` does not ride along. `jq` appears exactly once in `release.yml` (line 49),
  so t-4's "the release job still contains 'jq'" contract item has no false-positive risk.

## Summary

Structurally sound and unusually well-specified, but it under-scopes its own blast radius:
untracking `state.json` and making it optional breaks fixtures in three test files no task
owns, and t-3's parity assertion contradicts the locked decision it ships alongside.
