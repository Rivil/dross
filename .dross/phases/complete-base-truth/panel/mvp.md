# mvp lens — complete-base-truth

```
Phase complete-base-truth — 5 tasks across 3 waves

Wave 1
  t-1  Persist forked-from base on changes.json
       files:    internal/changes/changes.go,
                 internal/cmd/phase.go,
                 internal/cmd/ship.go
       covers:   c-2
       contract: `dross phase create auth` with milestone/v1.2 checked out writes
                 base="milestone/v1.2" into .dross/phases/auth/changes.json; if the
                 fork-time write is dropped, the create test asserting that field fails.
       contract: ship's PR-record commit carries the base — `git show
                 phase/auth:.dross/phases/auth/changes.json` reads base=<PR base> after
                 ship opens a PR; if SetBase is not folded into the existing
                 SetPR add/commit/push, the staged-content assertion fails.
       contract: SetBase on a phase with an existing changes.json preserves pr and
                 tasks (round-trip test in internal/changes); a truncating write fails it.

  t-2  Record the base a quick task forked from
       files:    internal/state/state.go,
                 internal/cmd/basebranch.go,
                 assets/prompts/quick.md
       covers:   c-7
       contract: `dross base-branch --record` writes quick_base into state.json while
                 stdout stays the bare branch name (no extra line) — the stdout
                 assertion catches any narration leaking onto stdout.
       contract: `dross base-branch` without --record leaves state.json byte-identical;
                 if the write is made unconditional, that assertion fails.
       contract: with milestone/v1.2 present, --record stores "milestone/v1.2", not
                 "main" — the milestone-active fixture fails if the recorded value is
                 taken from repo.git_main_branch.

Wave 2 (depends t-1, t-2)
  t-3  Gate complete before any branch switch
       files:    internal/cmd/phase.go,
                 internal/cmd/phase_test.go
       covers:   c-1, c-2, c-3, c-4, c-6
       description: Reorder phaseComplete's RunE — fetch, safety-net push, recorded-base
                 read, mergeGate all run before `git checkout <base>`. reconcileBranch
                 comes from the phase's recorded base (working-tree read, then the
                 origin/<base> fallback that originRecordedPR already does) instead of
                 resolveNewWorkBase; no record refuses with the phase id, the candidate
                 bases and the --base flag. Capture phase/<id>'s SHA before the checkout
                 and pass it to runDrossRecovery as preMergeSHA. Rewrite the --recover
                 paragraph of Long. Existing fixtures gain "base":"main" /
                 "base":"milestone/<v>" in their seeded changes.json.
       contract: with the PR stubbed unmerged, complete exits non-zero and
                 `git symbolic-ref --short HEAD` still reads phase/auth — the same
                 assertion repeated for a failing fetch (origin remote URL pointed at a
                 nonexistent path) and for a safety-net push refusal (a non-.dross
                 commit ahead on local main).
       contract: a phase whose changes.json records base="main" completes against main
                 even while a local milestone/v1.2 branch exists and is the
                 resolveNewWorkBase answer; rev-parse milestone/v1.2 is unchanged.
       contract: a changes.json with no base field refuses with an error naming "auth",
                 both candidate bases and "--base"; passing `--base main` on that same
                 fixture completes.
       contract: `complete --recover` on a diverged base restores a .dross/ path that
                 exists only on phase/auth (a file written on the phase branch and never
                 on the base) — sourcing the tree from the checked-out base drops it and
                 fails the assertion; and rev-parse of the *other* base (milestone/v1.2
                 when the record says main) is unchanged by the reset.
       contract: a help-text test asserts phase complete's Long no longer contains
                 "not yet supported" and states that --recover resets the recorded base
                 under a milestone.

  t-4  Recover against the recorded base
       files:    internal/cmd/ship_recover.go,
                 internal/cmd/ship_recover_test.go
       covers:   c-4, c-7
       description: shipRecover resolves its base as the phase's recorded changes.json
                 base, else state.quick_base, else repo.git_main_branch, and the
                 on-the-wrong-branch guard + runDrossRecovery reset both use it.
       contract: with changes.json base="milestone/v1.2", `dross ship recover auth` run
                 while HEAD is main refuses with "must be on milestone/v1.2"; the
                 pre-change code accepted that run and would reset main.
       contract: with no phase record but quick_base="milestone/v1.2" in state.json,
                 recover resolves the same branch; clearing quick_base makes it fall
                 back to main — both directions asserted.

Wave 3 (depends t-3, t-4)
  t-5  Regression test for the stale-milestone incident
       files:    internal/cmd/phase_test.go
       covers:   c-5
       description: New fixture: phase forked from main (base recorded as main), a stale
                 local milestone/<v> branch present and current_milestone set, PR
                 squash-merged to origin/main. Run complete and assert the outcome.
       contract: after the run, `git rev-parse milestone/<v>` equals the SHA captured
                 before the run — a completion that ff's the milestone branch fails it.
       contract: if the run returns an error, `git rev-parse refs/heads/phase/auth`
                 still resolves and `git ls-remote --heads origin phase/auth` is
                 non-empty — a refusal that deleted either ref fails it.
       contract: the run's stdout/error never names milestone/<v> as the reconcile
                 target; a regression back to resolveNewWorkBase surfaces there.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3 |
| c-2 | t-1, t-3 |
| c-3 | t-3 |
| c-4 | t-3, t-4 |
| c-5 | t-5 |
| c-6 | t-3 |
| c-7 | t-2, t-4 |

7/7 criteria covered.

## Judgment calls

- Merged c-1, c-3, c-6 and complete's half of c-4 into one task (t-3) rather than four: they are all edits to a single function's control flow, and any split leaves an intermediate commit where complete has been reordered but still infers its base from `current_milestone` — the exact bug state.
- Made the recorded-base read reuse the existing local-then-`origin/<base>` fallback shape (`originRecordedPR`) rather than adding a second lookup path; one helper returning the whole record replaces the PR-only one.
- Stored the quick base in `state.json` as `quick_base`, not in a new quick-scoped changes.json: `base_storage` locks the phase-scoped record for phases only, quick has no phase directory, and the quick flow already writes state.json in §6.
- Wrote that record from `dross base-branch --record` instead of a new `dross quick base` command: quick.md already calls `dross base-branch` at pre-flight step 4, so the record is written by a call that happens anyway — one flag, no new command surface.
- Put recovery's base resolution in `shipRecover` only (t-4) and had `phaseComplete` pass its already-resolved base explicitly, rather than duplicating the resolver on both entry points.
- No backfill task: `legacy_escape` locks refusal + an explicit `--base`, so the fixture updates in t-3 are the only migration work.
- No README/docs task: no parity test enforces flag-level README content (readme_doc_test.go only greps install/update), so a README edit is not traceable to a criterion.
- t-5 kept separate from t-3 rather than folded into its test file edits: it needs a whole new fixture (stale milestone branch + merge to main) and it exercises t-4's output as well as t-3's.
