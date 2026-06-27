Phase ship-complete-recovery-hardening — 4 tasks across 2 waves

Wave 1
  t-1  Extract shared recovery routine; gate commit on delta
       files:    internal/cmd/ship_recover.go
                 internal/cmd/ship_recover_test.go
       covers:   c-6
       desc:     Pull ship recover's fetch→reset→restore-.dross/→commit body into an
                 internal recoverDross(repoDir, root, mainBranch, phaseID, sha) helper that
                 `ship recover` now calls. Commit only when restored .dross/ is a real delta
                 vs origin (git status --porcelain after add); else print a no-op message and
                 return nil. Reuse the existing "chore(dross): restore .dross/ after
                 squash-merge for <id> + merge" message.
       contract: New test: run recover when local main already equals origin/main with .dross/
                 intact -> assert `rev-list --count origin/main..HEAD` == "0" and no error. If
                 the delta gate regresses, an empty restore commit appears and the count is "1"
                 -> test fails. TestShipRecoverHappyPath still asserts the 1-commit restore on a
                 real divergence, proving the gate didn't break the commit path.

  t-3  Surface stale completed-state in status
       files:    internal/cmd/status.go
                 internal/cmd/status_test.go
                 assets/prompts/resume.md
       covers:   c-4
       desc:     In Status(), when HEAD is a phase/<id> branch and branch-local state reads the
                 phase completed but origin/<main> carries no `completed <id>` record, print a
                 warn-only line with a reconcile pointer and do NOT render the phase as done.
                 Add one drift-reconcile line to resume.md §1 so `dross resume` (which runs
                 `dross status`) surfaces it. No state mutation.
       contract: New status test: fixture on a phase/<id> branch, state completed, origin/main
                 without the `completed <id>` record -> captured status output contains the
                 reconcile-pointer warning AND does not present the phase as finished. If
                 detection is dropped or it auto-treats the phase as done, the asserted warning
                 string is absent -> test fails.

  t-4  Document recovery recipe in ship.md + guard test
       files:    assets/prompts/ship.md
                 internal/cmd/ship_prompt_test.go
       covers:   c-5
       desc:     Add a Recovery section to ship.md covering the three mid-merge failure states
                 (ff-abort / diverged main / dirty post-push tree), each naming the exact dross
                 command (`dross phase complete --recover`, `dross ship recover`) and no manual
                 .dross/ git. Add a prompt-guard test in the execute_prompt_test.go style.
       contract: ship_prompt_test asserts ship.md contains the recovery section and the literal
                 `complete --recover` command for the diverged-main state, AND asserts the
                 absence of manual-surgery phrases (`checkout` ... `-- .dross/`, `git add
                 .dross/`). Remove the recipe or reintroduce manual .dross/ surgery -> a
                 Contains/NotContains assertion fails.

Wave 2
  t-2  Add complete --recover delegating to shared routine
       files:    internal/cmd/phase.go
                 internal/cmd/phase_test.go
       covers:   c-1, c-2, c-3
       desc:     Add a `--recover` bool flag to `phase complete`. When the ff-only step would
                 fail because origin/<main> diverged: without the flag, stop with a one-line
                 pointer naming `--recover` and change nothing; with the flag, call recoverDross
                 (t-1) for reset-to-origin + restore .dross/ in one shot. The pre-existing
                 clean-tree check stays ahead of the reset.
       depends:  t-1
       contract: New phase_test cases on a diverged-main fixture: (a) `complete` (no flag) ->
                 error mentioning `--recover` and `rev-parse HEAD` unchanged (no reset fired);
                 (b) `complete --recover` -> HEAD at origin/main plus exactly one restore commit
                 and the HEAD .dross/ tree contains a PRIOR phase's file as well as the current
                 phase's (the c-2 cumulative/partial-restore guard); (c) dirty tree or non-main
                 branch -> abort with no reset. Each regression (missing gate, partial restore,
                 lost guard) fails its matching case.

## Coverage
  c-1  -> t-2
  c-2  -> t-2
  c-3  -> t-2
  c-4  -> t-3
  c-5  -> t-4
  c-6  -> t-1

## Judgment calls
- Chose one shared-routine extraction task (t-1) over separate "extract" and "no-op gate"
  tasks: both touch only ship_recover.go and the delta gate IS the c-6 no-op, so splitting
  would be sub-10-minute fragments. Rejected a standalone refactor task with no criterion.
- Folded all recovery tests into the task that ships the behavior (t-1, t-2) rather than a
  separate verification wave; c-2's partial-restore guard is one assertion inside t-2's
  recover case, not its own task — it has no production code of its own.
- Made t-3 (status c-4) and t-4 (doc c-5) pure wave-1: neither needs recoverDross output, so
  forcing them behind t-1 would be false sequencing. Only t-2 strictly consumes t-1.
- Surfaced c-4 via status.go alone (resume.md runs `dross status`), adding just one drift line
  to resume.md instead of duplicating detection logic in a (non-existent) `dross resume` Go
  command. Rejected building a parallel resume code path.
- Routed c-1's "stop with a clear pointer" into t-2 rather than a separate guard task: it is
  the else-branch of the same `--recover` gate, one error string, not independent work.
