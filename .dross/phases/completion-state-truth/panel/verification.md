# Plan — completion-state-truth (verification lens)

Phase completion-state-truth — 7 tasks across 3 waves

Every task below was derived by writing the failing test first and then asking
what the smallest change is that makes that test satisfiable.

## Wave 1

```
t-1  Move completion record to phase complete
     files:    internal/cmd/ship.go, internal/cmd/phase.go,
               internal/cmd/phase_test.go, internal/cmd/ship_test.go
     covers:   c-2
     desc:     ship.go stops clearing current_phase; it sets
               current_phase_status = "shipped" and touches `shipped <id>`.
               phase.go's complete writes the transition after the branch
               teardown: current_phase = "", status = "", one `completed <id>`
               history entry, skipping the append when one already exists.
               phase_test.go's writeCompletion/foldCompletion fixtures stop
               clearing current_phase (they modelled ship's old write).
     contract: - After `dross ship`, state.json has current_phase == "<id>" and
                 current_phase_status == "shipped", and history contains no
                 `completed <id>` — TestShipRecordsShippedNotCompleted. Restoring
                 ship.go's `s.CurrentPhase = ""` fails it.
               - After `dross phase complete`, state.json has current_phase == ""
                 and exactly one `completed auth` entry —
                 TestPhaseCompleteWritesCompletionRecord. Deleting complete's
                 state write fails it (today the assertion only passes because
                 ship wrote it).
               - `dross phase complete auth --base main` run twice exits 0 both
                 times and still leaves exactly one `completed auth` entry —
                 TestPhaseCompleteIsIdempotent. An unconditional append (two
                 entries) or a second-run error fails it.
     depends:  —
     status:   pending

t-2  Add branch-topology helper
     files:    internal/cmd/topology.go, internal/cmd/topology_test.go
     covers:   c-5
     desc:     New branchTopology(repoDir, root) returning {Head, Work, Main,
               AheadOfMain, OnMain} — Work from resolveNewWorkBase, AheadOfMain
               from `git rev-list --count <main>..<work>`, plus a one-line
               renderer both status and complete print. Pure read, no network,
               degrades to Work=main/AheadOfMain=0 on any git failure.
     contract: - In a fixture where milestone/v1.2 carries 3 commits main does
                 not, branchTopology returns Work="milestone/v1.2",
                 AheadOfMain=3, OnMain=false — TestBranchTopologyCountsAheadOfMain.
                 Counting the wrong direction (`<work>..<main>`) yields 0 and
                 fails it.
               - In a repo with no milestone branch and no origin, it returns
                 Work="main", AheadOfMain=0 and a nil error —
                 TestBranchTopologyNoMilestoneNoRemote. A helper that errors on
                 a missing ref fails it (status must never break on it).
               - The renderer emits both the branch name and the distance
                 clause, and says "not yet on main" only when AheadOfMain > 0 —
                 TestRenderTopologyLine table cases (0 / 3 commits).
     depends:  —
     status:   pending

t-3  Strip branch teardown from ship merge step
     files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
     covers:   c-1
     desc:     §6.1 loses `--delete-branch` (GitHub), the `DELETE
               …/branches/phase%2F<id>` call (Forgejo) and
               `should_remove_source_branch` (GitLab); §6.3 stops claiming
               complete "records the merge in state.json with a chore commit"
               and names it the sole owner of every checkout, fast-forward and
               local+remote branch deletion. §7/Recovery prose that references
               the provider's `--delete-branch` is rewritten to match.
     contract: - ship.md from `## 4. Ship` to EOF contains none of
                 `--delete-branch`, `should_remove_source_branch`,
                 `/branches/phase`, `git checkout`, `git switch`, `git branch
                 -d` — TestShipPromptMergeStepSwitchesNoBranch. Reintroducing
                 any one of them fails it. (The §0 pre-flight `git checkout
                 phase/<id>` is deliberately out of scope — see judgment calls.)
               - §6 names `dross phase complete` as performing the local AND
                 remote phase-branch deletion, and the string "chore commit"
                 no longer appears near it —
                 TestShipPromptNamesCompleteAsTeardownOwner.
     depends:  —
     status:   pending
```

## Wave 2 (depends on wave 1)

```
t-4  Report shipped and topology in status
     files:    internal/cmd/status.go, internal/cmd/status_test.go
     covers:   c-5, c-6
     desc:     staleCompletedState's `completed`-keyed warning is replaced by a
               shipped reporter keyed on current_phase set +
               current_phase_status == "shipped" (or a recorded PR) with the
               work absent from origin/<main>; it prints `shipped: <id> — PR #N
               open, not merged into origin/<main>`. Adds the standing
               `branch:` topology line from t-2's renderer, printed on every
               status run.
     contract: - With current_phase=auth, current_phase_status=shipped, PR 42
                 recorded, HEAD on phase/auth and origin/main lacking the work,
                 `dross status` prints a `shipped:` line naming auth and PR #42
                 and prints no `stale:` line and no "state reads completed" —
                 TestStatusReportsShippedPhaseNotStale.
               - After a completed phase (current_phase empty, `completed auth`
                 in history), status prints neither `shipped:` nor `stale:` —
                 TestStatusSilentAfterCompletion. Keeping the old
                 history-keyed warning fires it and fails.
               - status always prints a `branch:` line; on a milestone repo it
                 names milestone/v1.2 with its commit distance from main and
                 the "not yet on main" clause, on a main-only repo it names main
                 with no distance clause — TestStatusPrintsStandingTopologyLine.
                 Gating the line behind "only when a phase is active" fails the
                 between-phases case.
     depends:  t-1, t-2
     status:   pending

t-5  State branch topology at completion
     files:    internal/cmd/phase.go, internal/cmd/phase_test.go
     covers:   c-5
     desc:     Replace complete's single "completed X — Y is at origin" line
               with the topology statement built from t-2: branch HEAD now sits
               on, which refs were deleted (local phase/<id>, origin's copy, or
               "already gone"), and where the work now lives relative to main.
     contract: - `dross phase complete` stdout names all three: the branch HEAD
                 ends on, the deleted local phase/auth AND the deleted remote
                 ref, and a clause stating the work is on <base> and not yet on
                 main — TestPhaseCompletePrintsTopologyStatement asserts each
                 substring separately, so dropping any one clause fails.
               - When the remote ref was already absent, the statement says so
                 rather than claiming a deletion that did not happen —
                 sub-test "remote branch already deleted" (reuses
                 TestPhaseCompleteRemoteDeleteIdempotent's fixture shape).
               - With no milestone active the statement names main as the
                 landing branch and omits the "not yet on main" clause —
                 sub-test "no milestone".
     depends:  t-1, t-2
     status:   pending

t-6  Add end-to-end live-state incident regression
     files:    internal/cmd/completion_state_incident_test.go
     covers:   c-4
     desc:     New test: dross repo whose origin/main still tracks a 2-entry
               .dross/state.json, live untracked copy holding 12 entries and
               version 9.9.9.9. Drives real `dross ship`, then a provider-merge
               simulation whose steps are parsed out of ship.md §6, then
               `dross phase complete`, asserting the live copy survives.
     contract: - After ship → simulated merge → complete, .dross/state.json
                 still parses, holds >= 12 history entries, contains the first
                 live action and reads version 9.9.9.9 —
                 TestShipToCompletePreservesLiveState. A non-zero exit from
                 complete is an allowed outcome (refusing IS survival, per the
                 existing switchbranch guard); a truncated 2-entry state.json
                 is not.
               - The merge simulation reads the §6 GitHub merge command from
                 assets/prompts/ship.md and performs the raw `git checkout
                 <base>` that `gh pr merge --delete-branch` performs iff that
                 flag is present. Reintroducing `--delete-branch` in ship.md
                 makes this test clobber the live copy and fail — that is the
                 "fails against the current code" property.
               - Control sub-test "raw checkout still clobbers" performs the
                 unguarded `git checkout main` directly and asserts the live
                 copy IS truncated to 2 entries — a harness that cannot detect
                 the incident makes the assertion above vacuous.
     depends:  t-1, t-3
     status:   pending
```

## Wave 3 (depends on wave 2)

```
t-7  Gate and fix every stale completion claim
     files:    internal/cmd/completion_truth_test.go, internal/cmd/phase.go,
               internal/cmd/ship.go, internal/cmd/status.go,
               internal/changes/changes.go, internal/telemetry/telemetry.go,
               ARCHITECTURE.md
     covers:   c-3
     desc:     New scan test over a fixed path list rejects the
               record-rides-the-squash phrasing; the same task rewrites every
               hit it finds — phase.go's Long help + the four inline comments,
               ship.go's step-5 comment, status.go's stale-guard comment,
               changes.go's breadcrumb aside, telemetry.go's merge_pending
               comment, and ARCHITECTURE.md's phase-complete + ship paragraphs —
               to say the record is written by `dross phase complete` into
               machine-local, gitignored state.json.
     contract: - The scan fails on any of "folds the completion record into the
                 squash", "folds the cleared current_phase", "rides the squash",
                 "carries the completion record to main", "complete writes no
                 commit of its own", "records the merge in state.json with a
                 chore commit" appearing in any listed path —
                 TestNoSurfaceClaimsCompletionRidesTheSquash. It fails today on
                 phase.go:236, ship.go:211, status.go:70, ARCHITECTURE.md:470.
               - The scan requires phase.go's `complete` Long help to contain
                 "writes the completed-state transition" and the word
                 "machine-local" — TestCompleteHelpNamesRecordOwner. Deleting
                 that sentence fails it, so the positive claim cannot be lost
                 the way the old one rotted.
               - assets/prompts/ship.md is in the scanned path list, so t-3's
                 §6 rewrite stays honest under later prompt edits.
     depends:  t-1, t-3, t-4, t-5
     status:   pending
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (no unguarded branch switch in ship) | t-3, verified end-to-end by t-6 |
| c-2 (complete owns the record, idempotent) | t-1 |
| c-3 (no surface claims the record rides the squash) | t-7 |
| c-4 (end-to-end incident regression) | t-6 |
| c-5 (topology stated at completion + standing in status) | t-2, t-4, t-5 |
| c-6 (shipped-but-unmerged reads `shipped`) | t-4 |

Every criterion has at least one task, and every task carries at least one
contract that names the surface it breaks.

## Judgment calls

- **The c-4 regression parses its merge step out of ship.md.** Chose a
  prompt-derived harness over hardcoding the raw checkout. Hardcoding it makes
  the test permanently red; hardcoding its absence makes it permanently green
  and it proves nothing. Only reading the instruction the agent actually follows
  makes the test track the fix.
- **"Failing against the current code" is discharged by a control sub-test, not
  by committing a red test.** Chose the repo's own idiom
  (TestStaleBranchCheckoutCannotClobberLiveState's "re-tracked state is still
  clobbered" control) over sequencing the test before the fix, because the
  execution gate forbids committing an observed-red test. The control performs
  the unguarded checkout and asserts the clobber, so the harness's ability to
  detect the incident is itself asserted.
- **Ship's write and complete's write are one task, not two.** Rejected
  splitting c-2 across ship.go and phase.go: neither half is correct alone (ship
  keeping current_phase with complete not writing the record loses the record
  entirely), so a split produces a wave that cannot be committed green.
- **The §0 pre-flight `git checkout phase/<id>` stays out of the c-1 scan.**
  c-1 scopes the guarantee to "HEAD stays on phase/<id> through the provider
  merge"; the pre-flight switch happens before the flow and there is no
  `dross phase checkout` primitive to route it through. Flagged rather than
  silently widened — inventing that command is a separate phase.
- **The truth pass is one 7-file task despite the split-at-5 rule.** Its gate
  test is atomic: a scan that lists a path the same wave has not yet fixed is
  red at commit time. Splitting by surface type would need the gate to grow its
  path list in a later wave, which leaves a window where a stale claim passes.
  All seven files are comment/prose only — one layer, no behaviour.
- **The status topology line is unconditional.** Rejected printing it only when
  a phase is active: the doubt it closes ("is milestone/v1.2 stuck?") is loudest
  between phases, which is exactly when a phase-gated line would be absent.
- **c-6's shipped signal keys on `current_phase_status == "shipped"` with the
  recorded PR as fallback**, not on history. Rejected keeping a history-keyed
  check: under record_owner a `completed <id>` entry now means genuinely
  complete, so keying on it would warn about phases that are actually done.
