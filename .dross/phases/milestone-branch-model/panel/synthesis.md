# milestone-branch-model — cold-judge synthesis

Three drafts judged: `risk.md` (9 tasks), `mvp.md` (5 tasks), `verification.md`
(7 tasks). I authored none. Below: scores, the merged plan built on the strongest
skeleton with concrete grafts from the runners-up, and an honest record of where
the lenses genuinely disagree.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
| --- | --- | --- | --- | --- |
| risk (9t) | Full c-1..c-6 + every locked decision owned by a named task; over-attributes (c-4 split across 3 tasks) | Highest volume — each contract names the exact test AND the regression it trips; but t-1 resolver is name-only, so its own cutover contract mis-encodes v0.7 (would return `milestone/v0.7` with no ref) | Over-split: t-3 (branch cmd), t-7/t-8 (PR vs cleanup), t-9 (`milestone base`) split single owners into extra tasks; some splits (insert, unmerged-guard) earn their keep | 2 waves, correct; two independent foundation files (t-1 pure, t-2 git) genuinely parallel |
| mvp (5t) | All c-1..c-6 but thin; c-3 is prompt-only with **no Go test** (acknowledged) — weakest pin | Decent, names tests, good merge-base assert; but c-3 unverifiable and t-3 fuses two distinct git ops (create-fork + complete-ff) under one contract | Minimal; merges create+complete (t-3) — different failure modes, one owner; resolver folded into milestone.go (t-1), not its own file → less parallel | 2 waves ok, but resolver buried in t-1 reduces reuse clarity |
| verification (7t) | All c-1..c-6, each pinned to named Go tests; coverage table explicit; enabler t-2 explicitly carries cutover + fallback | Sharpest reasoning: `merge-base --is-ancestor` of a milestone-only commit (tip-equality is insufficient when main==milestone — a real bug risk misses); mock captures POST `doc["base"]/["head"]`; c-3 made testable via `dross base-branch` CLI | 7 tasks, balanced; own `basebranch.go` (t-2) enables parallelism; cleanly folds cleanup into t-7 | 2 waves clean; t-1 (create) and t-2 (resolver) independent in wave 1; consumers in wave 2 |

**Skeleton: `verification.md`.** It has the highest-quality test contracts (the
ancestor-probe and PR-body-capture assertions actually distinguish correct from
wrong behaviour where the other two would false-pass), it makes the prompt-only
c-3 verifiable with a real CLI test (satisfying "every criterion has a specific
test_contract"), and — decisively — it encodes the locked `rollout_cutover` as a
*mechanism* (`milestone/<version>` branch exists?) that is asserted by a test,
whereas risk's pure resolver would hand back a nonexistent `milestone/v0.7` for
the currently-active milestone. Risk is the strongest runner-up and donates four
concrete grafts below (insert fork-point, ship remote-base guard, cleanup
unmerged-data-loss guard, complete divergence-abort).

## Merged plan

Phase milestone-branch-model — 7 tasks across 2 waves.

### Wave 1

**t-1 — Create + push `milestone/<version>` at scope time**  `[verification+mvp]`
- files: `internal/cmd/milestone.go`, `internal/cmd/milestone_test.go`, `assets/prompts/milestone.md`
- covers: c-1 (locks `milestone_branch_push`)
- contract: `TestMilestoneCreateCutsBranchFromMain` — after `dross milestone create v0.9` in a git repo, `git rev-parse --verify refs/heads/milestone/v0.9` succeeds, its tip == main's tip, and HEAD stays on main (branch created without checkout — see divergence b/rationale). `TestMilestoneCreatePushesEagerly` — against a bare origin, `git ls-remote --heads origin milestone/v0.9` is non-empty; drop the push → fails. `TestMilestoneCreateRerunIdempotent` (grafted from risk t-3) — a second `create`/scope run with the ref already on origin no-ops rather than erroring. `TestMilestoneCreateNoGitSkips` — in a non-git dir, create still writes the toml and does not error. milestone.md §Activate wiring is prompt-only per r-01; its contract is the CLI tests above.

**t-2 — Extract shared, existence-aware new-work base resolver**  `[verification]`
- files: `internal/cmd/basebranch.go`, `internal/cmd/basebranch_test.go`
- covers: enabler — powers c-2, c-3, c-4, c-6; delivers no criterion alone but is where `rollout_cutover` + `no_milestone_fallback` are pinned
- contract: `resolveNewWorkBase(repoDir, root) -> (base, milestoneActive, err)`. `TestResolveBase_MilestoneBranchExists` — `current_milestone=v0.9` AND `milestone/v0.9` ref present → `("milestone/v0.9", true)`. `TestResolveBase_CutoverNoBranch` — `current_milestone=v0.7` but NO `milestone/v0.7` ref → `(git_main_branch, false)`, proving the v0.7 non-retrofit cutover. `TestResolveBase_NoMilestone` — empty `current_milestone` → `(git_main_branch, false)`. Remove the ref-existence check (blindly return `milestone/<v>`) → the cutover test fails.

### Wave 2 (depends t-1, t-2)

**t-3 — Root `phase/<id>` (create AND insert) on the resolved base + no-milestone nudge**  `[verification+risk]`
- files: `internal/cmd/phase.go`, `internal/cmd/phase_lifecycle.go`, `internal/cmd/phase_test.go`, `internal/cmd/phase_lifecycle_test.go`
- covers: c-2 (with `no_milestone_fallback`)
- depends: t-1, t-2
- contract: `TestPhaseCreateRootsOnMilestoneBranch` — put a commit on `milestone/v0.9` NOT on main, set `current_milestone=v0.9`, run create; assert `git merge-base --is-ancestor <that-commit> phase/<id>` succeeds (the ancestor probe distinguishes a milestone root from a main root even when tips coincide). `TestPhaseCreateRootsOnMainNoMilestone` — no milestone branch → `phase/<id>` tip == main tip. `TestPhaseCreateNudgesNoMilestone` — output names `dross milestone`. **Grafted from risk t-4:** `TestPhaseInsertForksOffMilestone` — `phase insert` (a real second `checkout -b` call site in phase_lifecycle.go) forks off the same resolved base. Both call sites route through one `forkPhaseBranch` helper; `preflightPhaseBranch`'s must-be-on-main check is relaxed to an explicit `checkout -b <base>` (clean-tree + no-existing-ref guards retained).

**t-4 — `base-branch` CLI + quick roots on it**  `[verification+risk]`
- files: `internal/cmd/basebranch.go`, `internal/cmd/basebranch_test.go`, `assets/prompts/quick.md`, `cmd/dross/main.go`
- covers: c-3
- depends: t-1, t-2
- contract: `TestBaseBranchCmdPrintsMilestone` — `dross base-branch` prints `milestone/v0.9` when that branch exists and `current_milestone=v0.9`. `TestBaseBranchCmdPrintsMainNoMilestone` — prints `git_main_branch` otherwise. quick.md §0 standalone branch step is rewritten to use `dross base-branch` output instead of hardcoding `repo.git_main_branch`; the Go tests guard the exact string the prompt consumes, so a resolver regression is caught even though the markdown isn't unit-testable (r-01).

**t-5 — Ship PR base = resolved base + missing-remote-base guard**  `[verification+risk]`
- files: `internal/cmd/ship.go`, `internal/cmd/ship_test.go`
- covers: c-4
- depends: t-1, t-2
- contract: replace the hardcoded `baseBranch := p.Repo.GitMainBranch` (ship.go:222) with `resolveNewWorkBase`. Extend the provider mock to capture `doc["base"]`/`doc["head"]` from the POST body. `TestShipTargetsMilestoneBranch` — with `milestone/v0.9` present + `current_milestone=v0.9`, the single opened PR has `base=="milestone/v0.9"`, `head=="phase/<id>"`. `TestShipTargetsMainNoMilestone` — no milestone branch → `base=="main"`. **Grafted from risk t-5:** `TestShipRefusesMissingRemoteBase` — a pre-flight guard (before push and OpenPR) refuses when the resolved base is absent on origin, pointing at scope-time creation, instead of opening a PR against a nonexistent base.

**t-6 — `phase complete` fast-forwards the milestone branch (not main)**  `[verification+risk]`
- files: `internal/cmd/phase.go`, `internal/cmd/phase_test.go`
- covers: c-6
- depends: t-1, t-2
- contract: `TestPhaseCompleteFastForwardsMilestone` — with `current_milestone=v0.9` and `origin/milestone/v0.9` advanced to carry the `completed <id>` record, complete ends with local `milestone/v0.9 == origin/milestone/v0.9` and `phase/<id>` deleted locally+remote; the merged-record guard reads `origin/milestone/v0.9:.dross/state.json`, not `origin/main`. `TestPhaseCompleteNoMilestoneFfsMain` — no milestone branch → original main-ff preserved (cutover regression guard). **Grafted from risk t-6:** `TestPhaseCompleteMilestoneDivergedAborts` — a diverged local `milestone/<v>` aborts non-destructively (ff-only fails, nothing reset) without `--recover`. NOTE: full `--recover` reconcile-branch parameterization of `runDrossRecovery` is **deferred** (see disagreement e).

**t-7 — `dross milestone complete`: one PR into main + post-merge cleanup**  `[verification+risk]`
- files: `internal/cmd/milestone.go`, `internal/cmd/milestone_test.go`, `assets/prompts/ship.md`
- covers: c-5 (locks `milestone_main_merge`, `milestone_branch_cleanup`)
- depends: t-1
- contract: reuse `buildOpenOpts` + `ship.OpenPR`. `TestMilestoneCompleteOpensSinglePRToMain` — mock records exactly one POST `/pulls` with `base=="main"`, `head=="milestone/v0.9"`; a second run opens no duplicate; swapped head/base → fails. Finalize path (post-merge, mirrors phase complete): `TestMilestoneCompleteFinalizeCleansUp` — once `origin/main` carries the merge, ff local main from origin and delete `milestone/v0.9` locally + on origin; assert neither ref remains. **Grafted from risk t-8:** `TestMilestoneCleanupRefusesUnmerged` — refuse to delete the branch while `origin/main` does NOT yet contain the merge (guards against destroying unmerged integration work). ship.md's merge-method note (non-squash merge-commit, per `milestone_main_merge`) is prompt-only per r-01.

### Coverage check
- c-1 → t-1 · c-2 → t-3 · c-3 → t-4 · c-4 → t-5 · c-5 → t-7 · c-6 → t-6 · enabler t-2 (c-2/c-3/c-4/c-6 ride on it). Every criterion c-1..c-6 has a specific test_contract.

## Disagreements

**(a) Resolver shape: pure name-only vs existence-aware.**
risk (t-1) makes `resolveBaseBranch` a *pure* function of `current_milestone`
with no git access, pushing every "does the branch exist?" check onto each
consumer (its stated reason: local-ref for create, origin-ref for ship,
origin-tracking for complete are genuinely different probes). verification (t-2)
and mvp (t-1) make the resolver *existence-aware* so the cutover rule lives in
one place. **Provisional default: existence-aware `resolveNewWorkBase` (verification).**
Why it matters: risk's pure resolver returns `milestone/v0.7` for the active v0.7
milestone (which has no branch) — silently breaking the locked `rollout_cutover`
unless every consumer re-guards. Centralising the existence check makes cutover a
single tested mechanism. Risk's valid point is preserved partially: the resolver
settles the *name/fallback*, and consumers that need a *remote* existence check
still own it (ship's `TestShipRefusesMissingRemoteBase`, grafted into t-5).

**(b) How the milestone branch is created at scope: new `dross milestone branch`
subcommand vs folded into `milestone create` vs skill-driven.**
risk (t-3) adds a dedicated `dross milestone branch [version]` command and wires
milestone.md to call it. mvp (t-1) and verification (t-1) fold create+push
directly into `dross milestone create`. Neither proposes pure skill/prompt git
steps. **Provisional default: fold into `milestone create` (mvp+verification, 2 of 3).**
Why it matters: a separate subcommand is extra surface the criterion (c-1) doesn't
name, and it introduces a two-step "create then branch" ordering that can be
skipped; folding makes the branch an unconditional side effect of scoping.
Trade-off surfaced: risk's separate command is easier to re-run idempotently and
to invoke for repair — that idempotency is recovered by t-1's
`TestMilestoneCreateRerunIdempotent` contract rather than a new command.

**(c) `milestone complete` as a new Go command vs a skill/ship extension —
CONVERGED.** All three lenses make it a new Go command (risk t-7/t-8, mvp t-5,
verification t-7); none proposes a skill-only path. No divergence to resolve;
recorded for transparency. The only sub-disagreement (split vs single) folds into
(d)/(e).

**(d) Task count / granularity: 5 vs 7 vs 9.**
mvp merges create+complete and keeps 5; verification splits to 7; risk splits to
9 (separate branch command t-3, PR/cleanup split t-7/t-8, standalone `milestone
base` t-9). **Provisional default: 7 tasks (verification).** Why it matters: mvp's
merge of phase-create and phase-complete (t-3) fuses two distinct git operations
with different failure modes under one contract, and leaves c-3 unverifiable;
risk's extra splits mostly re-attribute a single owner across tasks (which its own
"one risk, one owner" principle argues against). 7 keeps each criterion's distinct
failure mode individually owned and tested without over-fragmenting. Risk's splits
that carried *real distinct risk* (insert fork-point, ship remote-base guard,
cleanup unmerged-guard) are not discarded — they are grafted as extra contracts
into t-3/t-5/t-7 rather than as standalone tasks.

**(e) Scope of `phase_lifecycle` insert and the divergence-recovery machinery.**
risk pulls `phase insert` into t-4 (correct — it is a genuine second `checkout -b`
site, confirmed in phase_lifecycle.go:190) and parameterizes `runDrossRecovery`'s
reconcile branch in t-6 so `phase complete --recover` under a milestone heals
`milestone/<v>` instead of main. mvp and verification touch neither.
**Provisional default: INCLUDE insert (grafted into t-3), DEFER the recovery-
machinery parameterization (`ship_recover.go`/`doctor.go`/`gitattributes.go`).**
Why it matters: insert is cheap correctness — omitting it leaves phase insert
silently rooting on main under a milestone, a c-2 hole. The recovery
parameterization is real but larger surface; t-6 instead aborts
*non-destructively* on a diverged milestone branch (grafted
`TestPhaseCompleteMilestoneDivergedAborts`), which is safe on its own.
CAVEAT to flag at execution: with the reconcile branch still hardcoded to main,
`phase complete --recover` under a milestone would reset to `origin/main` — so
`--recover` must be documented as unsupported-under-milestone until a follow-up,
or the abort message must steer away from it.

**(f) How the locked clean-cutover is encoded: branch-existence vs version/date/flag.**
verification and mvp encode `rollout_cutover` as "`milestone/<version>` ref
exists?" — v0.7 has no such ref (it was scoped pre-cutover) so it falls back to
main with zero extra state, and the fallback is asserted by a test. risk encodes
fallback on `current_milestone` presence (which, as noted in (a), mis-handles the
active v0.7). No lens proposes a stored date or "scoped-under-new-model" flag.
**Provisional default: branch-existence mechanism (verification+mvp).** Why it
matters: it is the only encoding that both satisfies the locked decision for the
in-flight v0.7 *and* is directly observable in a test; a stored flag would be a
new schema/migration surface that can drift from the branch's actual existence.
