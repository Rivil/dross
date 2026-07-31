# Plan draft — risk lens

Failure modes this phase must own (each is assigned to exactly one task):

| # | Failure mode | Owner |
|---|---|---|
| R1 | A refusal fires *after* `checkout <base>`, stranding HEAD off the phase branch | t-2 |
| R2 | The base is re-derived from `current_milestone`, so a stale local `milestone/<v>` becomes the reconcile target (the incident) | t-4 |
| R3 | The base record is never written, or written on the wrong branch / lost on a failed fork | t-1 |
| R4 | The record is written at create but the PR targets something else (base drift) | t-3 |
| R5 | `--recover` resets an inferred branch and restores `.dross/` from the tree it just checked out | t-5 |
| R6 | `--help` documents behaviour that no longer exists | t-5 |
| R7 | A standalone quick's base is inferred later, so its unpushed `.dross` chores re-seed divergence | t-6, t-7 |
| R8 | The whole class regresses silently because nothing reproduces the incident | t-8 |

Phase complete-base-truth — 8 tasks across 4 waves

Wave 1
  t-1  Record fork base on changes.json
       files:    internal/changes/changes.go, internal/changes/changes_test.go,
                 internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2
       description: Add `Base string \`json:"base,omitempty"\`` to changes.Changes beside PR,
                 plus SetBase(root, phaseID, base) mirroring SetPR. forkPhaseBranch writes the
                 base it actually forked from — only after `checkout -b` succeeds, deriving the
                 phase id by trimming the "phase/" prefix from branchName.
       contract: `dross phase create auth` on main leaves base="main" in
                 .dross/phases/auth/changes.json; with milestone/v1.2 present and current it
                 leaves base="milestone/v1.2".
       contract: when `checkout -b` fails (t-1 fixture: branch already exists), no changes.json
                 is created for that phase id — the create rollback (`os.Remove(dir)`) still
                 leaves an empty dir removable.
       contract: changes.Load on a legacy file with no `base` key returns Base=="" without
                 error, and re-Save emits no `base` key (omitempty round-trip).
       contract: `dross phase insert --after X "title"` records a base too — it shares
                 forkPhaseBranch, so a base-less inserted phase fails this.

  t-2  Move every refusal before the branch switch
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-1
       description: Reorder phaseComplete's RunE: fetch, pushBaseIfAheadDrossOnly and mergeGate
                 all run while HEAD is still on phase/<id> (none of them needs the base checked
                 out — they read origin/* refs and rev-list). Capture the phase-branch SHA
                 (HEAD, or refs/heads/phase/<id> when HEAD is elsewhere) immediately before the
                 checkout; checkout moves to just above the ff-only merge.
       contract: with PRMergedFunc returning not-merged, complete exits non-zero and
                 `git symbolic-ref --short HEAD` is still phase/auth (today it is main).
       contract: with origin pointed at a deleted path, the fetch failure surfaces and both
                 HEAD and `git rev-parse main` are byte-identical to their pre-run values.
       contract: with a non-.dross commit sitting unpushed on local main, the safety-net
                 code-ahead refusal fires with HEAD still on phase/auth and no reflog entry
                 for a checkout.
       contract: the happy path is unchanged — TestPhaseCompleteHappyPath still ends on main
                 with phase/* deleted and a clean tree.

  t-6  Record standalone quick's base in state
       files:    internal/state/state.go, internal/cmd/state.go, internal/cmd/state_test.go,
                 assets/prompts/quick.md, internal/cmd/quick_prompt_test.go
       covers:   c-7
       description: Add QuickBase (`quick_base,omitempty`) to state.State, readable via
                 `dross state get quick_base` and writable via `dross state set quick_base`.
                 quick.md §0 standalone branch records the resolved base before implementing;
                 the in-phase branch records nothing (its commits ride phase/<id>, whose base
                 t-1 already records).
       contract: `dross state set quick_base main` then `dross state get quick_base` prints
                 main; `dross state set nonsense x` still errors with "unknown field".
       contract: a state.json written with no quick_base round-trips through Load/Save without
                 gaining a `quick_base` key.
       contract: quick.md's standalone step contains the `dross state set quick_base` call and
                 its in-phase step does not — the prompt sentinel test fails if either the call
                 is dropped or it leaks into the in-phase branch.

Wave 2 (depends t-1, t-2)
  t-3  Overwrite base with the PR's base at ship
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-2
       depends:  t-1
       description: In the post-open record block, write the resolved PR base alongside the PR
                 number so both land in the single `chore(dross): record PR #N` commit that is
                 pushed to phase/<id> (and therefore squashed onto the base).
       contract: after a stubbed ship against main, `git show phase/auth:.dross/phases/auth/
                 changes.json` contains both pr and base="main" — a local-only write that never
                 reaches the pushed ref fails this.
       contract: when the create-time base and the PR base differ (phase forked from main, a
                 milestone branch created and made current afterwards), the post-ship record
                 reads the PR base, not the create-time one.
       contract: `--no-push` and `--print-body` return before any record write — the base field
                 keeps its create-time value and no extra commit appears on phase/<id>.

  t-4  Reconcile against the recorded base only
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-2, c-3
       depends:  t-1, t-2
       description: phaseComplete reads Base from the phase's changes.json (working tree first,
                 then `git show refs/heads/phase/<id>:…` when the tree lacks it) and uses it as
                 the reconcile branch, the mergeGate target and the originRecordedPR ref;
                 resolveNewWorkBase is no longer consulted here. No base and no `--base <branch>`
                 → refuse before any git mutation, naming the phase and the candidate bases.
                 Adds the `--base <branch>` override flag.
       contract: a phase with base="main" recorded completes against main while
                 current_milestone=v1.2 and milestone/v1.2 exists locally — `git rev-parse
                 milestone/v1.2` is unchanged and main is at origin/main.
       contract: a phase whose changes.json has no base key refuses with an error naming the
                 phase id, both candidate bases (main and milestone/<version>) and `--base`;
                 HEAD, main and phase/auth all unmoved afterwards.
       contract: the same legacy phase completes when run as `dross phase complete --base main`.
       contract: with the working tree missing .dross/phases/auth/changes.json but
                 refs/heads/phase/auth carrying it, the base is read off the branch ref rather
                 than refusing.
       contract: the origin-side PR fallback reads origin/<recorded base>:…changes.json — with
                 base=main recorded and a milestone branch present, PR #42 is still found and
                 the ancestry fallback is not taken.

Wave 3 (depends t-4, t-6)
  t-5  Make --recover base-true and fix its help
       files:    internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-4, c-6
       depends:  t-2, t-4
       description: On the ff abort, pass the recorded base and the phase-branch SHA captured
                 by t-2 into runDrossRecovery (as baseBranch and preMergeSHA), so the reset
                 targets only the recorded base and the `.dross/` restore is sourced from the
                 phase's own work commit rather than the branch just checked out. Rewrite the
                 Long text's --recover paragraph to match (it currently claims --recover is
                 unsupported under a milestone).
       contract: --recover on a diverged base with base="main" recorded leaves
                 `git rev-parse milestone/v1.2` unchanged while main ends at origin/main plus
                 one restore commit.
       contract: after --recover, a file that exists only on phase/auth
                 (.dross/phases/auth/plan.toml) is present in the restore commit — sourcing the
                 checked-out base's tip instead leaves it absent and fails here.
       contract: `dross phase complete --help` contains no "not yet supported" claim about
                 --recover under a milestone and names the recorded base as the reset target.

  t-7  Push the recorded quick base too
       files:    internal/cmd/ship.go, internal/cmd/phase.go, internal/cmd/phase_test.go
       covers:   c-7
       depends:  t-4, t-6
       description: After the phase-base safety-net push, ship and complete also run
                 pushBaseIfAheadDrossOnly on state.QuickBase when it is set, differs from the
                 recorded base, and its local ref exists; a missing ref is a silent skip.
       contract: with quick_base="main", a .dross-only commit unpushed on main, and the phase
                 base = milestone/v1.2, ship advances origin/main as well as origin/milestone/v1.2.
       contract: with quick_base naming a branch that no longer exists locally, ship and
                 complete both finish without error and push nothing extra (no rev-list failure
                 leaking out as a hard error).
       contract: with a non-.dross commit unpushed on quick_base, the run refuses and the error
                 names quick_base — not the phase base — as the branch to reconcile.
       contract: with quick_base equal to the recorded base, exactly one push of that branch
                 happens (no double push).

Wave 4 (depends t-1..t-7)
  t-8  Reproduce the stale-milestone incident end to end
       files:    internal/cmd/phase_complete_incident_test.go
       covers:   c-5
       depends:  t-1, t-2, t-3, t-4, t-5, t-7
       description: New test file with a fixture that reconstructs the incident: phase forked
                 from main (base recorded), a stale local milestone/v1.2 at the old baseline
                 with current_milestone=v1.2, the PR squash-merged into origin/main, PRMergedFunc
                 stubbed merged. Asserts on both possible outcomes.
       contract: `git rev-parse milestone/v1.2` and `git rev-parse origin/milestone/v1.2` are
                 identical before and after the run, whether complete succeeds or refuses —
                 an ff of the milestone branch fails here.
       contract: when the run returns an error, refs/heads/phase/auth and the origin phase ref
                 both still exist (nothing deleted on a refusal path).
       contract: when the run succeeds, HEAD is main, main == origin/main, and phase/auth is
                 deleted locally and on origin.
       contract: the same fixture run against the pre-fix ordering (base inferred from
                 current_milestone) is what this test is written to catch — it must fail if
                 phaseComplete goes back to calling resolveNewWorkBase for its reconcile branch.

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-1, t-3, t-4 |
| c-3 | t-4 |
| c-4 | t-5 |
| c-5 | t-8 |
| c-6 | t-5 |
| c-7 | t-6, t-7 |

7/7 criteria covered.

## Judgment calls

- Split the base *write* (t-1, t-3) from the base *read* (t-4) rather than one "base plumbing"
  task: the write is the record's integrity, the read is the incident. One task owning both
  would let a green test on the write hide a read still calling resolveNewWorkBase.
- Put the ordering fix (t-2) in wave 1 ahead of the base read (t-4), even though c-1 and c-2 are
  independent criteria. Both rewrite the same RunE; sequencing them means the second edit sees
  the first's shape instead of two drafts colliding in one function.
- Refusals move above the checkout by *relocating the checkout*, not by adding a
  restore-HEAD-on-error path. A compensating checkout is itself a branch switch that can fail
  mid-refusal; never switching is the only version that cannot half-apply.
- `--recover` gets the phase SHA captured by t-2 rather than re-deriving it inside
  runDrossRecovery. Rejected the alternative of making runDrossRecovery smarter about HEAD: it
  is shared with `dross ship recover`, whose legacy contract genuinely *is* "HEAD holds the
  pre-merge tree". Passing preMergeSHA explicitly leaves that path untouched.
- The base read falls back to `git show refs/heads/phase/<id>:…changes.json` before refusing.
  Rejected "working tree only": complete is routinely run from a base branch whose tree predates
  the record, and that shape would send every such run down the c-3 refusal for no reason.
  Rejected an origin-side fallback: reading origin/<base> requires already knowing the base.
- `--no-branch` phases record no base and therefore hit the c-3 refusal. Rejected recording
  main as a guess — there was no fork, so there is no forked-from branch, and a guess here is
  exactly the inference the milestone is removing.
- c-7 stores the quick base in state.json rather than a new file: only a *standalone* quick
  needs it (an in-phase quick commits on phase/<id>, already covered by t-1), and state.json is
  the one record a phase-less quick already writes.
- A code-ahead quick_base is a hard refusal, matching the existing base policy, not a warning.
  Rejected warn-and-continue: it preserves the silent-divergence failure mode this whole
  milestone exists to kill, and the message names the branch so the fix is one push away.
- t-8 is its own task rather than assertions bolted onto t-4's tests. It is the only task whose
  fixture spans create → ship → squash → complete, and c-5 names it as a deliverable.
