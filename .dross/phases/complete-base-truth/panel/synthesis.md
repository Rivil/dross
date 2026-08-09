# Synthesis — complete-base-truth

Judged against the spec's 7 criteria and against the real tree
(`internal/cmd/phase.go`, `basebranch.go`, `ship.go`, `ship_recover.go`,
`internal/changes/changes.go`, `assets/prompts/quick.md`).

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (8 tasks / 4 waves) | 7/7, plus an R1–R8 failure-mode table with exactly one owner per mode — the only draft that names what it is defending against. | Strongest *negative* contracts: omitempty legacy round-trip, `os.Remove(dir)` rollback on a failed fork, `--no-push`/`--print-body` early return, missing quick_base ref = silent skip, no-double-push. Cites no existing test names, so its contracts float free of the fixtures. | Correct write/read split (t-1/t-3 write, t-4 read) with the stated reason that a green write test would otherwise hide a read still calling `resolveNewWorkBase`. | Sound. t-2 (reorder) deliberately ahead of t-4 (base read) because both rewrite the same RunE — the right call. |
| **mvp** (5 tasks / 3 waves) | 7/7 on paper, but t-3 alone owns c-1, c-2, c-3, c-4 and c-6. Uniquely catches that `shipRecover` resolves its base from `p.Repo.GitMainBranch` only (`ship_recover.go:88-100`) — a real second c-4 hole. | Contracts are falsifiable and well-shaped, but t-2's four contracts all target `base-branch --record` and none tests the `quick.md` edit it also ships. t-3's contract block is long enough that a partial implementation passes most of it. | Weakest. The t-3 monolith is justified by "any split leaves an intermediate commit where complete is reordered but still infers its base" — true but not load-bearing: an intermediate commit is not a shipped state, and both other drafts keep the two edits adjacent with an explicit dependency. | 3 waves is fine, but t-3 and t-4 both land base-resolution logic in wave 2 with no ordering between them. |
| **verification** (8 tasks / 4 waves) | 7/7 with a clean 1:1 criterion→surface mapping and an explicit three-way split of c-2 (schema / writers / reader). | Highest. Every contract is written as a falsification ("if X regresses, test Y fails") and anchored to fixtures that actually exist — `completeFixture`, `stubPRMerged`, `shipMockFlow`, `activateMilestone`, `TestPhaseCreateRootsOnMilestoneBranch`, `TestStateSetRejectsUnknownField`, `TestShipPushesPRRecordToPhaseBranch` all verified present. | Cleanest. Each task has one breakable surface; c-6 correctly folded into t-6 (the change that makes the new help text true) rather than a standalone docs task. | Correct. t-4/t-5 co-scheduled with the explicit note that they edit disjoint functions in the same file — a real judgment, not an oversight. |

**Skeleton: verification.** It has the cleanest task boundaries and the only
contracts pinned to fixtures that exist today, so its plan can be executed
without a discovery pass. risk supplies most of the grafted contracts (it
covers failure states verification's contracts do not reach); mvp supplies one
task neither other draft saw.

## Merged plan

Phase complete-base-truth — 9 tasks across 4 waves

```
Wave 1
  t-1  Add base field and SetBase to changes.json          [verification+risk]
       files:    internal/changes/changes.go
                 internal/changes/changes_test.go
       covers:   c-2
       desc:     Add `Base string \`json:"base,omitempty"\`` to changes.Changes beside
                 PR, and SetBase(root, phaseID, branch) mirroring SetPR's
                 load-set-save (changes.go:130-141).
       contract: - Save({phase,pr:42,base:"main"}) then Load() returns Base=="main";
                   a dropped field or json tag fails it                  [verification]
                 - SetPR(42) then SetBase("main"), reloaded, still has pr==42 — a
                   truncating write fails it                        [verification+mvp]
                 - SetBase on a phase dir with no changes.json creates it with
                   base=="milestone/v1.2"                                [verification]
                 - Load on a legacy file with no `base` key returns Base=="" without
                   error, and re-Save emits no `base` key (omitempty round-trip)  [risk]

  t-2  Move complete's branch switch past all refusals     [verification+risk+mvp]
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-1
       desc:     In phaseComplete's RunE, relocate the `git checkout <reconcileBranch>`
                 block (phase.go:344-353) from above the fetch to immediately before
                 `merge --ff-only` (phase.go:397). fetch, pushBaseIfAheadDrossOnly and
                 mergeGate all take branch names, not HEAD, so none needs the switch.
                 Capture phase/<id>'s SHA immediately before the checkout (HEAD when
                 on the phase branch, else refs/heads/phase/<id>) and hold it for t-6.
                 Do NOT add a restore-HEAD-on-error path: a compensating checkout is
                 itself a branch switch that can fail mid-refusal.               [risk]
       contract: - stubPRMerged(t,false) on completeFixture: complete exits non-zero
                   and `symbolic-ref --short HEAD` is still "phase/auth"  [all three]
                 - origin remote rewritten to a nonexistent path: the "git fetch"
                   error surfaces and HEAD is still "phase/auth"         [all three]
                 - a non-.dross commit on local main: the "touching non-.dross paths"
                   refusal fires, HEAD is still "phase/auth" AND `rev-parse main` is
                   byte-identical to its pre-run value            [verification+risk]
                 - all three of the above also assert `branch --list phase/auth` is
                   non-empty — no refusal path moves or deletes a ref     [verification]
                 - TestPhaseCompleteHappyPath still ends on main with phase/* deleted
                   and a clean tree — the reorder changes no success behaviour  [risk]

  t-3  Record quick's forked-from base in state.json       [verification+risk]
       files:    internal/state/state.go
                 internal/cmd/state.go
                 internal/cmd/state_test.go
                 assets/prompts/quick.md
                 internal/cmd/quick_prompt_test.go   (new file; follows the
                 ship_prompt_test.go / inbox_prompt_test.go idiom — no quick.md
                 test exists today)
       covers:   c-7
       desc:     Add QuickBase (`quick_base,omitempty`) to state.State; teach
                 readStateDotted (state.go:72-88) and stateSet (state.go:100-111) the
                 field. quick.md pre-flight §4's **standalone** arm records the
                 resolved branch via `dross state set quick_base <branch>` before
                 implementing; the in-phase arm records nothing (see D4).
       contract: - `state set quick_base main` then `state get quick_base` prints
                   "main"                                          [verification+risk]
                 - TestStateSetRejectsUnknownField still fails loudly on an unknown
                   field — quick_base must not be wired into a permissive default
                   arm                                                  [verification]
                 - a state.json with no quick_base round-trips Load/Save without
                   gaining the key                                             [risk]
                 - quick_prompt_test.go: the normalised prompt's standalone step
                   contains "state set quick_base" and the in-phase step does not —
                   fails if the call is dropped or leaks into the in-phase arm [risk]

Wave 2 (depends t-1, t-2)
  t-4  Write the phase base at fork and at ship            [verification+risk+mvp]
       files:    internal/cmd/phase.go
                 internal/cmd/phase_lifecycle.go
                 internal/cmd/ship.go
                 internal/cmd/phase_test.go
                 internal/cmd/ship_test.go
       covers:   c-2
       depends:  t-1
       desc:     forkPhaseBranch (phase.go:541) records the base it actually forked
                 from — only after `checkout -b` succeeds — deriving the phase id from
                 branchName's "phase/" prefix or taking it as a new parameter;
                 phaseInsert's call site (phase_lifecycle.go:188) passes its id. In
                 ship.go's post-PR record block (ship.go:347-375), SetBase(baseBranch)
                 alongside SetPR so both ride the single `chore(dross): record PR #N`
                 commit that is committed AND pushed to phase/<id>.
       contract: - `phase create auth` on main leaves base=="main" in
                   .dross/phases/auth/changes.json                    [verification+mvp]
                 - TestPhaseCreateRootsOnMilestoneBranch's sibling asserts
                   base=="milestone/<v>" — recording resolveNewWorkBase's answer
                   instead of the checked-out fork point fails it        [verification]
                 - `phase insert --before/--after` records a base too; a base-less
                   inserted phase fails                              [verification+risk]
                 - when `checkout -b` fails (branch already exists), no changes.json
                   is created for that phase id — create's rollback `os.Remove(dir)`
                   (phase.go:149) only removes an EMPTY dir, so a pre-checkout write
                   silently leaks the phase id                                  [risk]
                 - a shipMockFlow test with activateMilestone asserts the post-ship
                   changes.json reads base=="milestone/<v>", not the create-time
                   "main"                                            [verification+risk]
                 - TestShipPushesPRRecordToPhaseBranch's sibling: `git show
                   origin/phase/x:.dross/phases/x/changes.json` contains base — a
                   local-only write that never reaches the pushed ref fails
                                                                  [verification+risk+mvp]
                 - `--no-push` / `--print-body` return before the record block, so the
                   base keeps its create-time value and no extra commit appears  [risk]

  t-5  Resolve complete's base from the record, add --base [verification+risk]
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-2, c-3
       depends:  t-1, t-2
       desc:     Replace resolveNewWorkBase in phaseComplete (phase.go:292) with a
                 resolver reading changes.Load(...).Base for the phase: working tree
                 first, then `git show refs/heads/phase/<id>:.dross/phases/<id>/
                 changes.json` (see D2). A `--base <branch>` flag overrides. No record
                 and no flag = refuse BEFORE any git mutation, naming the phase id,
                 the candidate bases (configured main, plus milestone/<current> when
                 that ref exists) and --base. The resolved base feeds the safety-net
                 push, originRecordedPR, mergeGate and the ff-only merge;
                 resolveNewWorkBase is no longer consulted here.
       contract: - base=="main" recorded, current_milestone set, milestone/<v> present
                   locally: complete lands on main, `rev-parse milestone/<v>` is
                   unchanged, and `rev-parse main == rev-parse origin/main`
                                                                  [verification+risk+mvp]
                 - a changes.json with no base key: non-nil error whose text contains
                   "auth", "--base" and both candidate branch names; HEAD, main and
                   phase/auth all unmoved                        [verification+risk+mvp]
                 - that same base-less fixture run with `complete --base main`
                   succeeds and deletes phase/auth               [verification+risk+mvp]
                 - `--base does-not-exist` refuses naming that branch rather than
                   leaking a raw git checkout error                      [verification]
                 - working tree missing .dross/phases/auth/changes.json while
                   refs/heads/phase/auth carries it: the base is read off the branch
                   ref rather than refusing                                     [risk]
                 - originRecordedPR reads origin/<recorded base>: with base=="main"
                   and a milestone branch present, PR #42 is still found and the
                   ancestry fallback is not taken                               [risk]

Wave 3 (depends t-3, t-4, t-5)
  t-6  Scope --recover to the recorded base and the phase tip  [verification+risk+mvp]
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-4, c-6
       depends:  t-2, t-5
       desc:     Pass t-2's captured phase-branch SHA into runDrossRecovery as
                 preMergeSHA (phase.go:423 passes "" today, so recovery restores the
                 tree of the branch it just checked out) and the t-5 base as
                 baseBranch. runDrossRecovery already takes both parameters
                 (ship_recover.go:137) — no signature change, and `dross ship
                 recover`'s legacy "HEAD holds the pre-merge tree" contract stays
                 untouched.                                                    [risk]
                 Rewrite Long's final paragraph (phase.go:245-250), which currently
                 claims --recover is "not yet supported" under a milestone.
       contract: - a phase-only file (.dross/phases/auth/marker.txt committed on
                   phase/auth) is present in the post-recovery tree; a HEAD-sourced
                   recovery restores the base's tree, which never had it
                                                                  [verification+risk]
                 - the restored .dross/phases/auth/changes.json still contains
                   "pr":42                                                [verification]
                 - base=="main" recorded, stale milestone/<v> present, main diverged:
                   `rev-parse milestone/<v>` is byte-identical before and after
                                                                  [verification+risk+mvp]
                 - a help-text test on phaseComplete().Long: no "not yet supported",
                   and it names both "milestone/" and "--base"    [all three]

  t-7  Reconcile the recorded quick base in ship and complete  [risk+verification]
       files:    internal/cmd/basebranch.go
                 internal/cmd/phase.go
                 internal/cmd/ship.go
                 internal/cmd/phase_test.go
       covers:   c-7
       depends:  t-3, t-5
       desc:     After the phase base's safety-net push, ship and complete also run
                 pushBaseIfAheadDrossOnly against state.QuickBase when it is set,
                 differs from the resolved phase base, and its local ref exists; a
                 missing ref is a silent skip. Refusal text names the quick record as
                 the source. quick_base is NOT cleared on success (see D5).
       contract: - quick_base=="main", a .dross-only chore unpushed on main, phase base
                   milestone/<v>: after the run `rev-list origin/main..main` is empty
                   and origin/milestone/<v> also advanced           [risk+verification]
                 - same fixture with quick_base unset: main is left untouched — proves
                   the record, not an inference, drove the push          [verification]
                 - quick_base naming a branch with no local ref: ship and complete both
                   finish without error and push nothing extra (no rev-list failure
                   leaking out as a hard error)                                [risk]
                 - a non-.dross commit on quick_base: hard refusal whose text names
                   quick_base — not the phase base — as the branch to reconcile
                                                                  [risk+verification]
                 - quick_base equal to the resolved phase base: exactly one push of
                   that branch (no double push)                                [risk]

  t-8  Resolve ship recover's base from the phase record       [mvp]
       files:    internal/cmd/ship_recover.go
                 internal/cmd/ship_recover_test.go
       covers:   c-4
       depends:  t-1, t-4
       desc:     shipRecover resolves its base as the phase's recorded changes.json
                 base, falling back to repo.git_main_branch when there is no record;
                 the on-the-wrong-branch guard (ship_recover.go:98-100) and the
                 runDrossRecovery reset both use it. Today mainBranch is
                 p.Repo.GitMainBranch unconditionally, so under a milestone this
                 command resets main — a branch that is not the phase's base. mvp's
                 additional `else state.quick_base` link in the chain is dropped
                 (see D6).
       contract: - with changes.json base=="milestone/<v>", `dross ship recover auth`
                   run while HEAD is main refuses with "must be on milestone/<v>";
                   the pre-change code accepted that run and would reset main   [mvp]
                 - with no phase record at all, the guard and the reset still resolve
                   repo.git_main_branch — the legacy one-shot path is unchanged  [mvp]

Wave 4 (depends t-4, t-5, t-6)
  t-9  Reproduce the stale-milestone incident end to end   [verification+risk+mvp]
       files:    internal/cmd/phase_base_truth_test.go   (new file)
       covers:   c-5
       depends:  t-4, t-5, t-6, t-7
       desc:     New fixture reproducing the incident: phase forked from main (base
                 recorded as main), current_milestone set with a stale local
                 milestone/<v> branch present, PR #42 squash-merged onto origin/main,
                 PRMergedFunc stubbed merged. One test asserts the success shape, one
                 asserts the refusal shape (base record stripped); both assert the
                 invariants.
       contract: - `rev-parse milestone/<v>` and `rev-parse origin/milestone/<v>` are
                   identical before and after the run, in BOTH the success and the
                   refusal case — any ff of the milestone branch fails here
                                                                  [verification+risk+mvp]
                 - success case: HEAD=="main", `rev-parse main == rev-parse
                   origin/main`, phase/auth deleted locally and on origin
                                                                  [verification+risk]
                 - refusal case: `branch --list phase/auth` and `ls-remote --heads
                   origin phase/auth` are both non-empty — nothing deleted on a
                   refusal path                                   [all three]
                 - the run's stdout/error never names milestone/<v> as the reconcile
                   target; a regression to resolveNewWorkBase surfaces there   [mvp]
                 - an outcome that is neither clean success nor clean refusal has no
                   matching arm in the test's explicit two-outcome switch and fails
                                                                        [verification]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 refusals leave HEAD on phase/<id> | t-2 |
| c-2 base persisted in the phase record and read back | t-1, t-4, t-5 |
| c-3 no record → guided refusal | t-5 |
| c-4 --recover scoped to the recorded base + phase work | t-6, t-8 |
| c-5 end-to-end incident regression | t-9 |
| c-6 --help accurate about --recover under a milestone | t-6 |
| c-7 quick records its base; reconcilers read the record | t-3, t-7 |

7/7 criteria covered.

### Grafts onto the skeleton (verification)

- t-2 gains risk's `rev-parse main` unchanged and happy-path-unchanged contracts,
  and risk's explicit rejection of a restore-HEAD-on-error path.
- t-4 gains risk's fork-rollback contract, which is the only one tied to a real
  landmine: `os.Remove(dir)` at phase.go:149 fails silently on a non-empty dir.
- t-4 gains risk's `--no-push` / `--print-body` early-return contract.
- t-5 gains risk's phase-ref fallback and its originRecordedPR contract (D2).
- t-7 gains risk's missing-ref, code-ahead and no-double-push contracts.
- t-8 is grafted wholesale from mvp (D6).
- t-3 keeps verification's file list; mvp shipped a `quick.md` edit with no test
  for it, which the `quick_prompt_test.go` needle closes.

## Disagreements

### D1 — Granularity of the phaseComplete rewrite

mvp folds c-1, c-2, c-3, c-4 and c-6 into a single task (its t-3), arguing that
any split leaves an intermediate commit where complete has been reordered but
still infers its base — the exact bug state. risk and verification both split
the reorder (c-1) from the base read (c-2/c-3) from the recovery scoping
(c-4/c-6).

**Default taken: split** (merged t-2, t-5, t-6).
**Why it matters:** mvp's intermediate-commit argument is real but not
load-bearing — that commit is never a shipped state, and both split plans keep
the edits adjacent with an explicit dependency. The cost of the monolith is
concrete: one task with a dozen contracts can go green on a partial
implementation, and c-1 is the one fix that must hold *even when base
resolution succeeds*. Splitting also means every later refusal test inherits
t-2's HEAD-unchanged assertion.

### D2 — How complete finds the recorded base when the working tree lacks it

risk falls back to `git show refs/heads/phase/<id>:…changes.json` before
refusing. verification explicitly *rejects* any fallback as chicken-and-egg
("it needs the base in order to know which ref to read from"). mvp wants to
reuse originRecordedPR's local-then-`origin/<base>` shape.

**Default taken: risk's phase-ref fallback; no origin-side fallback.**
**Why it matters:** verification's chicken-and-egg objection is valid only for
`origin/<base>` — `refs/heads/phase/<id>` needs no base at all. Without the
fallback, running complete from the base branch — which is precisely where the
pre-fix code stranded the user, and where they will be when they re-run — hits
a spurious c-3 refusal on a phase that *does* have a base recorded on its own
branch. That turns the new refusal into a nuisance and trains the user to reach
for `--base`, which is the inference this milestone is removing, typed by hand.

### D3 — Where the quick base is written

risk and verification add a `quick_base` field to state.json written by a new
`dross state set quick_base` arm called from quick.md. mvp instead adds a
`--record` flag to `dross base-branch`, on the grounds that quick.md already
calls `dross base-branch` at pre-flight step 4 (confirmed: quick.md line 20), so
the record rides a call that happens anyway — one flag, no new command surface.

**Default taken: `state set quick_base`** (the skeleton's shape).
**Why it matters:** both need the same `state.State` field and json tag, so the
divergence is only over the writer. `base-branch` is a read-only printer whose
entire contract is "stdout carries only the bare branch name" (basebranch.go
:16-19, `Args: cobra.NoArgs`); giving it a write mode makes a resolver mutate
state, and mvp's own contract has to defend that stdout stayed clean. `state
set` is where every other state write already goes. mvp's cost argument is
real and cheap to switch to if the extra prompt step proves flaky.

### D4 — Which quick arm records the base

verification writes the record in BOTH the in-phase and standalone arms of
quick.md §4, with a needle test asserting two occurrences. risk records only in
the standalone arm, because an in-phase quick's commits ride phase/<id>, whose
base t-4 already records.

**Default taken: standalone only** (risk).
**Why it matters:** recording in both arms creates two records of the same fact
with different lifetimes — the phase's changes.json base is authoritative and
gets overwritten by ship, while quick_base is not. They can disagree, and t-7
pushes whatever quick_base says. One fact, one record. verification's second
needle inverts under this default: it must assert the in-phase arm does *not*
record.

### D5 — Clearing quick_base after a successful reconcile

verification's t-7 clears quick_base once the branch is level with origin. risk
leaves it set.

**Default taken: don't clear** (risk).
**Why it matters:** clearing writes state.json on the base branch, producing
exactly the unpushed .dross chore that pushBaseIfAheadDrossOnly exists to mop
up — a self-refilling bucket. It also makes the reconcile non-idempotent: a
second run after a failed push has lost the record. Leaving a stale-but-true
branch name costs one no-op rev-list. Cheap to add later if the record proves
noisy.

### D6 — Is `dross ship recover` in scope for c-4?

mvp says yes: `shipRecover` resolves its base from `p.Repo.GitMainBranch`
alone (verified, ship_recover.go:88-100) and its wrong-branch guard therefore
refuses/resets against main even when the phase forked from a milestone branch.
risk and verification are silent, scoping c-4 to `phase complete --recover`.

**Default taken: include, as t-8, base-resolution only.** mvp's additional
`else state.quick_base, else main` fallback chain is dropped — a three-deep
inference chain is the thing this milestone removes.
**Why it matters:** c-4's literal subject is `--recover`, a flag on `phase
complete`; `ship recover` is a different command, so t-8 is the one merged task
arguing from the criterion's spirit rather than its wording. It is also the
smallest and most isolated task here (two contracts, one file), and both entry
points already share runDrossRecovery, so leaving it out means the shared heal
routine is base-true from one caller and base-inferring from the other. **If
this phase needs scoping down, t-8 is the first task to cut** — nothing else
depends on it.

### D7 — `--no-branch` phases have no base and will refuse

Only risk raises this: a `--no-branch` phase never calls forkPhaseBranch, so
t-4 writes nothing and every such phase hits t-5's c-3 refusal. risk rejects
recording main as a guess ("there was no fork, so there is no forked-from
branch"). mvp and verification do not mention the flag at all.

**Default taken: risk's position — no task, refusal stands, `--base` is the
escape.**
**Why it matters:** it is a silent behaviour change for an existing flag
(phase.go:207) that no draft assigns a contract to. The merged plan does not
add one, but t-5's refusal message must read sensibly for a phase that was
never forked, and the executor should expect any `--no-branch` fixture in
phase_test.go to need `--base` after t-5 lands.
