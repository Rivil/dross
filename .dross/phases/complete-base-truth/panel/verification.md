# complete-base-truth — verification lens

Phase complete-base-truth — 8 tasks across 4 waves

```
Wave 1
  t-1  Add base field and SetBase to changes.json
       files:    internal/changes/changes.go
                 internal/changes/changes_test.go
       covers:   c-2
       desc:     Add `Base string \`json:"base,omitempty"\`` to changes.Changes beside PR,
                 and SetBase(root, phaseID, branch) mirroring SetPR's load-set-save.
       contract: - if Base loses its json tag or is dropped from the struct, a round-trip
                   test that Save()s {phase,pr:42,base:"main"} and Load()s it back fails on
                   base == ""
                 - if SetBase overwrites instead of merging, a test that calls SetPR(42)
                   then SetBase("main") and asserts the reloaded record still has pr==42
                   fails
                 - if SetBase can't create a record from nothing, a test calling SetBase on
                   a phase dir with no changes.json and asserting base=="milestone/v1.2"
                   afterwards fails

  t-2  Record quick's forked-from base in state.json
       files:    internal/state/state.go
                 internal/cmd/state.go
                 internal/cmd/state_test.go
                 assets/prompts/quick.md
                 internal/cmd/quick_prompt_test.go
       covers:   c-7
       desc:     Add QuickBase (`quick_base,omitempty`) to state.State; teach
                 readStateDotted + stateSet the field; quick.md pre-flight §4 writes the
                 resolved branch via `dross state set quick_base <branch>` in BOTH the
                 in-phase and standalone arms.
       contract: - if `state set quick_base main` stops persisting, a test that sets it then
                   runs `state get quick_base` and expects stdout "main" fails
                 - if quick_base gets wired into stateSet's default arm (accepting any
                   field), TestStateSetRejectsUnknownField's existing unknown-field case
                   still fails loudly
                 - if quick.md §4 loses the record step, a quick_prompt_test.go needle test
                   asserting the normalised prompt contains "state set quick_base" fails
                 - if only the standalone arm records, a second needle asserting the
                   in-phase arm also records (two occurrences) fails

  t-3  Move complete's branch switch past all refusals
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-1
       desc:     In phaseComplete's RunE, relocate the `git checkout <reconcileBranch>`
                 block from before the fetch to immediately before the `merge --ff-only`.
                 fetch, pushBaseIfAheadDrossOnly and mergeGate all operate on branch names,
                 not HEAD, so none of them need the switch.
       contract: - if the checkout creeps back above mergeGate, a test running
                   completeFixture + stubPRMerged(t,false) and asserting
                   `symbolic-ref --short HEAD` is still "phase/auth" after the refusal fails
                 - if the checkout precedes the fetch, a test that rewrites the origin
                   remote to a nonexistent path, expects the "git fetch" error, and asserts
                   HEAD is still "phase/auth" fails
                 - if the checkout precedes the safety-net push, a test that commits a
                   non-.dross file onto local main (making it code-ahead of origin/main),
                   expects the "touching non-.dross paths" refusal, and asserts HEAD is
                   still "phase/auth" AND `rev-parse main` is unchanged fails
                 - if any refusal path moves a ref, each of the three tests above also
                   asserts `branch --list phase/auth` is non-empty

Wave 2 (depends t-1, t-3)
  t-4  Write phase base at fork and at ship
       files:    internal/cmd/phase.go
                 internal/cmd/phase_lifecycle.go
                 internal/cmd/ship.go
                 internal/cmd/phase_test.go
                 internal/cmd/ship_test.go
       covers:   c-2
       depends:  t-1
       desc:     forkPhaseBranch takes phaseID and calls changes.SetBase with the base it
                 actually forked from, after the checkout; phaseInsert's call site passes
                 its id. In ship.go's post-PR record block, SetBase(baseBranch) alongside
                 SetPR so the same commit+push carries both.
       contract: - if forkPhaseBranch stops recording, a test running `phase create auth`
                   and reading .dross/phases/auth/changes.json expects base=="main" and
                   fails on ""
                 - if it records resolveNewWorkBase's answer instead of the branch it
                   checked out from, TestPhaseCreateRootsOnMilestoneBranch's sibling asserts
                   base=="milestone/v0.9" and fails
                 - if the insert path skips the write, a test running `phase insert
                   --before` and reading the new phase's changes.json base fails on ""
                 - if ship stops overwriting, a shipMockFlow test with activateMilestone
                   asserts the post-ship changes.json has base=="milestone/v0.9" (not the
                   create-time "main") and fails
                 - if ship writes base outside the SetPR commit, TestShipPushesPRRecordToPhaseBranch's
                   sibling asserting `git show origin/phase/x:.dross/phases/x/changes.json`
                   contains "base" fails

  t-5  Resolve complete's base from record, add --base
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-2, c-3
       depends:  t-1, t-3
       desc:     Replace resolveNewWorkBase in phaseComplete with a resolver that reads
                 changes.Load(...).Base for the phase; a --base <branch> flag overrides it.
                 No record and no flag = refusal naming the phase id, the candidate bases
                 (configured main, plus milestone/<current_milestone> when that ref exists)
                 and the --base escape. The resolved base feeds the safety-net push, the
                 merge gate and the ff-only merge.
       contract: - if complete falls back to the milestone-inferred base, a test with
                   changes.json base=="main", current_milestone set, and a local
                   milestone/v0.4 branch present asserts `rev-parse milestone/v0.4` is
                   unchanged and HEAD ends on main — and fails
                 - if the recorded base isn't threaded into the ff, the same test asserting
                   `rev-parse main == rev-parse origin/main` fails
                 - if the no-base refusal silently falls back, a test on a changes.json with
                   no base key expects a non-nil error whose text contains the phase id
                   "auth", the word "--base", and both candidate branch names — and asserts
                   HEAD is still "phase/auth"
                 - if --base is not honoured, the same base-less fixture run with
                   `complete --base main` is expected to succeed and delete phase/auth
                 - if --base is allowed to name a branch with no local ref, a test passing
                   `--base does-not-exist` expects a refusal naming that branch rather than
                   a raw git checkout error

Wave 3 (depends t-2, t-5)
  t-6  Scope --recover to recorded base and phase tip
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-4, c-6
       depends:  t-5
       desc:     Capture the phase branch tip SHA before the (now-late) checkout and pass
                 it to runDrossRecovery as preMergeSHA, so the restored .dross/ tree comes
                 from this phase's work rather than the just-checked-out base. Recovery
                 resets only the resolved recorded base. Rewrite the Long text's final
                 paragraph: --recover works on main AND milestone/<version>, and document
                 --base.
       contract: - if preMergeSHA stays defaulted to HEAD, a test that commits a
                   phase-only file .dross/phases/auth/marker.txt on phase/auth, diverges
                   the base, runs `complete --recover`, and asserts that file exists in the
                   post-recovery tree fails (HEAD-sourced recovery restores the base's tree,
                   which never had it)
                 - if recovery sources from the base, the same test asserting the restored
                   .dross/phases/auth/changes.json still contains "pr":42 fails
                 - if --recover resets an inferred base, a test with base=="main" recorded,
                   a stale milestone/v0.4 present, and main diverged asserts
                   `rev-parse milestone/v0.4` is byte-identical before and after — and fails
                 - if the stale help text survives, a test on phaseComplete().Long asserting
                   it does NOT contain "not yet supported" and DOES contain both
                   "milestone/" and "--base" fails

  t-7  Reconcile recorded quick base in ship and complete
       files:    internal/cmd/basebranch.go
                 internal/cmd/phase.go
                 internal/cmd/ship.go
                 internal/cmd/phase_test.go
       covers:   c-7
       depends:  t-2, t-5
       desc:     After the phase base's safety-net push, ship and complete also run
                 pushBaseIfAheadDrossOnly against state.QuickBase when it is set and differs
                 from the resolved phase base, clearing quick_base once that branch is level
                 with origin. The refusal text names the quick record as the source.
       contract: - if the recorded quick base is ignored, a test that sets quick_base=main,
                   commits a .dross-only chore onto local main, runs complete against a
                   phase whose recorded base is milestone/v0.4, and asserts
                   `rev-list origin/main..main` is empty afterwards fails
                 - if it infers the base instead of reading the record, the same test with
                   quick_base unset asserts main is left unpushed (untouched) and fails
                 - if quick_base isn't cleared after a successful reconcile, the first test
                   asserting `state get quick_base` prints empty fails
                 - if a code-ahead quick base is silently pushed, a test committing a
                   non-.dross file on main expects a refusal whose text names both "main"
                   and "quick"

Wave 4 (depends t-4, t-5, t-6)
  t-8  Add stale-milestone incident regression test
       files:    internal/cmd/phase_base_truth_test.go
       covers:   c-5
       depends:  t-4, t-5, t-6
       desc:     New fixture reproducing the incident end to end: phase forked from main
                 (base recorded as main), current_milestone set with a stale local
                 milestone/<v> branch present, PR #42 merged and squashed onto origin/main.
                 One test asserts the success shape, one asserts the refusal shape (base
                 record stripped), both assert the invariants.
       contract: - if complete ever fast-forwards the stale milestone branch, the test
                   asserting `rev-parse milestone/v1.2` equals the SHA captured before the
                   run fails, in BOTH the success and refusal cases
                 - if completion succeeds but against the wrong base, the success case
                   asserting `rev-parse main == rev-parse origin/main` and HEAD=="main"
                   fails
                 - if the refusal case deletes the phase branch, the assertion that
                   `branch --list phase/auth` is non-empty and `ls-remote --heads origin
                   phase/auth` is non-empty fails
                 - if completion neither succeeds nor refuses cleanly (e.g. panics or exits
                   0 having moved nothing), the test's explicit two-outcome switch on the
                   error has no matching arm and fails
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (no branch switch on any refusal path) | t-3 |
| c-2 (base persisted in phase record, read back by complete) | t-1, t-4, t-5 |
| c-3 (no recorded base → guided refusal, not silent fallback) | t-5 |
| c-4 (--recover scoped to recorded base, sourced from phase work) | t-6 |
| c-5 (end-to-end incident regression) | t-8 |
| c-6 (--help accurate about --recover under a milestone) | t-6 |
| c-7 (quick records its base; reconcilers use the record) | t-2, t-7 |

7 of 7 criteria covered.

## Judgment calls

- Split c-2 across three tasks (schema t-1, writers t-4, reader t-5) rather than one
  "base truth" task: the criterion has three independently-breakable surfaces, and a
  single task's contract would collapse to "base works".
- Made the c-1 reorder its own wave-1 task instead of folding it into t-5: it needs no
  schema change, and it is the one fix that must hold even when base resolution succeeds.
  Landing it first also means every later refusal test inherits the HEAD-unchanged
  assertion for free.
- Merged c-6 into t-6 rather than a standalone docs task: a Long-text edit is under ten
  minutes, and t-6 is precisely the change that makes the new text true. Rejected pairing
  it with t-5 because t-5 only adds --base; the --recover-under-milestone claim is t-6's.
- Chose state.json `quick_base` for c-7's storage. Rejected a per-quick record directory
  (there is no `dross quick` Go command at all — quick is prompt-driven, so a new on-disk
  record needs new CLI surface to write it) and rejected reusing changes.json (the locked
  base_storage decision is phase-scoped, and a standalone quick has no phase dir).
- Chose pushBaseIfAheadDrossOnly as c-7's "command that later reconciles": it is the only
  thing in the tree that reconciles a base branch today. Rejected inventing a
  `dross quick reconcile` command — new surface for a criterion the existing safety net
  already almost satisfies, just against an inferred base instead of a recorded one.
- Base resolution in t-5 reads the working-tree changes.json only, with no origin-side
  fallback like originRecordedPR's. Rejected the fallback because it needs the base in
  order to know which ref to read from — chicken-and-egg. The create-time write (t-4)
  guarantees the record sits on phase/<id>, which is where complete starts.
- Put t-4 and t-5 in the same wave despite both touching internal/cmd/phase.go: they edit
  disjoint functions (forkPhaseBranch vs phaseComplete's RunE). Rejected serialising them
  for a conflict risk a single sequential executor does not have.
- Kept c-5 as its own wave-4 task rather than extra assertions inside t-5: the criterion
  asks for a reproduction of the incident, which needs its own fixture (stale local
  milestone branch + squash landed on main), and it must exercise t-4/t-5/t-6 together.
