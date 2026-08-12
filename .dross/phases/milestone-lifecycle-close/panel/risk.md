# risk lens — milestone-lifecycle-close

Failure modes first. Each way this phase can lie about a milestone's state is owned by
exactly one task, and named in that task's contract:

| # | Failure mode | Owner |
|---|---|---|
| F1 | Finalize succeeds but the toml still says `active` → the milestone is never closed on disk | t-1 |
| F2 | Re-running finalize hits a merge guard against refs that finalize itself deleted → false "not merged" refusal | t-1 |
| F3 | A hand-set `status = complete` short-circuits finalize while the branch is still live → silent leftover | t-1 |
| F4 | Local main is ahead of origin → a freshly-cut branch reads as merged → prune deletes live work | t-2 |
| F5 | No origin at all (offline / no remote) → the origin-only comparison errors or reports nothing | t-2 |
| F6 | `remove` addressed by a value that isn't there silently no-ops → user believes the entry is gone | t-3 |
| F7 | Replacing a `phases` entry via remove+add appends it → phase order silently reordered | t-3 |
| F8 | An unscaffolded slug counts as done → milestone closes with unbuilt criteria on its roadmap | t-4 |
| F9 | Branch gone from origin reported as "not merged" → user re-opens a PR for work already merged | t-5 |
| F10 | Doctor calls the *in-flight* milestone's branch prunable → prune deletes the branch every phase forks from | t-6 |
| F11 | A `milestone/*` branch with no toml is judged by an absent status → unknown case deleted | t-6 |
| F12 | The prompt guesses milestone state from prose output and drives completion mid-milestone, or squash-merges the integration PR | t-7 |
| F13 | t-1's flip and t-6's gate are individually correct but disagree at the seam → a finalized milestone stays unprunable forever, or an active one becomes prunable | t-8 |

---

```
Phase milestone-lifecycle-close — 8 tasks across 3 waves

Wave 1
  t-1  Flip status complete; short-circuit re-finalize
       files:    internal/cmd/milestone.go, internal/cmd/milestone_finalize_test.go, README.md
       covers:   c-1, c-2
       depends:  —
       status:   pending
       desc:     milestoneFinalize writes [milestone].status = "complete" as its last step,
                 after both branch deletes and the ff succeed. A milestone already at
                 "complete" returns before the fetch/merge guard with an already-finalized
                 line; when a live milestone/<version> ref still exists it names it and
                 points at `dross milestone prune`. README milestone row updated.
       contract: (a) after a successful --finalize on a merged milestone,
                 .dross/milestones/v1.0.toml reads status = "complete" — asserting the field
                 fails if the write is dropped;
                 (b) the flip is last: a fixture where `git push origin --delete` fails leaves
                 status = "active", so a retry still runs the real finalize;
                 (c) a second --finalize on that toml exits 0, prints "already finalized", and
                 its combined output does NOT contain "is not merged into" — a regression that
                 re-enters the merge guard fails on the absent-string assertion;
                 (d) status = "complete" with refs/heads/milestone/v1.0 still present prints the
                 branch name and "dross milestone prune", and deletes no ref (the test
                 re-checks rev-parse --verify after the run).

  t-2  Measure staleness against origin main
       files:    internal/cmd/milestone_stale.go, internal/cmd/milestone_stale_test.go
       covers:   c-6
       depends:  —
       status:   pending
       desc:     staleMilestoneBranches resolves its comparison ref to
                 refs/remotes/origin/<main> when it exists, falling back to refs/heads/<main>
                 only when origin carries no such ref. Both the ancestry check and the
                 squash-content walk use the resolved ref.
       contract: (a) repo where refs/heads/main carries the merge commit but
                 refs/remotes/origin/main does not → the detector returns zero entries;
                 after `git push origin main` the identical repo returns the branch with
                 Reason == "merged". A detector still reading local main passes the second
                 half and fails the first;
                 (b) same split for the squash path: content squashed onto local main only is
                 not stale, and becomes Reason == "squash-merged" once origin/main has it;
                 (c) a repo with no origin remote at all still reports a locally-merged
                 milestone branch (non-empty result, no error) — the fallback is exercised,
                 not just declared.

  t-3  Add milestone remove and replace verbs
       files:    internal/cmd/milestone_remove.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_remove_test.go, README.md
       covers:   c-7
       depends:  —
       status:   pending
       desc:     `dross milestone remove [version] <list.path> "<exact value>"` and
                 `dross milestone replace [version] <list.path> "<old>" "<new>"` over
                 phases / scope.success_criteria / scope.non_goals, sharing normalizeListField
                 and the version-defaulting of add. Non-matching value is an error. Registered
                 in Milestone(); README milestone row lists both verbs.
       contract: (a) `remove v1.0 scope.success_criteria "nope"` exits non-zero, the message
                 quotes the value, and the toml bytes read back byte-identical to before the
                 run — a silent no-op fails both halves;
                 (b) removing the middle entry of phases = ["a","b","c"] yields exactly
                 ["a","c"] — order-preserving, so a swap-with-last implementation fails;
                 (c) `replace v1.0 phases "b" "b2"` yields ["a","b2","c"], not ["a","c","b2"] —
                 pins F7 against a remove-then-append implementation;
                 (d) a scalar path (`milestone.title`) errors naming the three valid list
                 fields rather than mangling the scalar.

  t-4  Add milestone progress readout
       files:    internal/cmd/milestone_progress.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_progress_test.go, README.md
       covers:   c-4
       depends:  —
       status:   pending
       desc:     `dross milestone progress [version] [--json]` answers the dispatch question in
                 one deterministic call: milestone status, done/total phases, and the pending
                 slugs. Per locked phases_done_test a slug is done only when phases/<slug>
                 exists AND state history carries `completed <slug>` or its changes.json
                 records a PR; an unscaffolded slug is pending.
       contract: (a) milestone listing ["a","b","c"] where a has a `completed a` history entry,
                 b has changes.json with pr > 0, and c has NO directory reports done=2,
                 total=3, all_done=false, and lists "c" under pending — an implementation that
                 counts array entries, or one that skips missing dirs, fails on c;
                 (b) all three recorded done → all_done=true, exit 0, empty pending;
                 (c) --json parses as an object carrying status / done / total / all_done /
                 pending, and exits 0 for a milestone whose toml has status = "planning";
                 (d) no version arg and no state.current_milestone → non-zero with the
                 no-current-milestone message, not a nil-deref.

Wave 2 (depends on wave 1)
  t-5  Distinguish a gone branch from an unmerged one
       files:    internal/cmd/milestone.go, internal/cmd/milestone_finalize_test.go
       covers:   c-3
       depends:  t-1
       status:   pending
       desc:     Before the ancestry check, milestoneFinalize probes
                 refs/remotes/origin/milestone/<version> (post-fetch). Absent → an error saying
                 the branch is gone from origin, naming `dross milestone set <v>
                 milestone.status complete` as the fix if it was already finalized. Present but
                 not contained → the existing unmerged refusal, unchanged.
       contract: (a) fixture with origin/milestone/v1.0 deleted and the toml still
                 status = "active": the error contains "gone from origin" and does NOT contain
                 "is not merged into origin/";
                 (b) fixture with origin/milestone/v1.0 present and genuinely unmerged: the
                 error still contains "is not merged into origin/" — the old message survives
                 for the case it was written for;
                 (c) ordering pin: status = "complete" AND no origin branch prints
                 "already finalized" and never the gone error, proving the probe sits after
                 t-1's short-circuit.

  t-6  Gate stale detector on milestone status
       files:    internal/cmd/milestone_stale.go, internal/cmd/doctor.go,
                 internal/cmd/milestone_stale_test.go
       covers:   c-5
       depends:  t-2
       status:   pending
       desc:     staleMilestoneBranches takes the .dross root, loads
                 milestones/<version>.toml for each milestone/<version> branch, and skips the
                 branch when status is `active` — and also when no toml exists (locked
                 toml_less_branch_not_stale). doctor.go and milestonePrune pass the root.
       contract: (a) milestone/v1.1 squash-merged onto origin/main with
                 .dross/milestones/v1.1.toml at status = "active" is absent from the result;
                 rewriting that one field to "complete" makes the same branch appear with
                 Reason == "squash-merged";
                 (b) milestone/v9.9 with no toml at all is absent from the result — the
                 fail-closed case, distinct from (a) because no status was read;
                 (c) status = "planning" (branch cut, work not started) is likewise absent;
                 (d) `dross doctor` in a repo whose only milestone branch is the active one
                 prints no "Stale milestone branches:" block and does not increment its issue
                 count.

  t-7  Dispatch /dross-milestone on milestone status
       files:    assets/prompts/milestone.md, internal/cmd/milestone_prompt_test.go
       covers:   c-4
       depends:  t-1, t-4
       status:   pending
       desc:     milestone.md opens with a dispatch step reading `dross milestone progress
                 --json`: no active milestone → the existing scope flow; all_done → drive
                 completion (integration PR, merge-commit-not-squash, then --finalize);
                 otherwise → report the pending slugs and stop. Scoping body moves under the
                 first branch.
       contract: (a) the prompt text contains `dross milestone progress` and all three branch
                 labels — a prompt that still opens straight into scoping fails;
                 (b) the completion branch contains "merge commit", "not squash" and
                 "--finalize" — dropping the squash gate fails the assertion that pins c-4's
                 "gating merge-commit-not-squash";
                 (c) the report branch instructs naming the `pending` slugs from the readout
                 and forbids `phase list` / verify.toml scanning as the source of doneness;
                 (d) the prompt names the already-finalized outcome (re-running --finalize is
                 safe) so the dispatch never treats it as an error state.

Wave 3 (depends on wave 2)
  t-8  Pin the close-path seam end to end
       files:    internal/cmd/milestone_lifecycle_close_test.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-5, t-6, t-7
       status:   pending
       desc:     One scripted lifecycle over a real temp repo + bare origin, asserting the
                 handoff between the status flip (t-1) and the status gate (t-6) — the seam
                 neither task can test alone.
       contract: single fixture, asserted in order: (1) active milestone whose branch is merged
                 to origin/main → staleMilestoneBranches returns nothing for it;
                 (2) --finalize → toml status "complete" AND refs/heads/milestone/v1.0 and
                 refs/remotes/origin/milestone/v1.0 both absent;
                 (3) re-run --finalize → exit 0, "already finalized", no "is not merged into";
                 (4) recreate milestone/v1.0 at the merged commit → the same detector now
                 returns it with Reason "merged", so a finalized milestone's leftover branch
                 IS prunable. Flipping either the flip or the gate breaks a numbered step.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 (primary), t-8 |
| c-2 | t-1 (primary), t-8 |
| c-3 | t-5 (primary), t-8 |
| c-4 | t-4 (readout), t-7 (prompt dispatch) |
| c-5 | t-6 (primary), t-8 |
| c-6 | t-2 |
| c-7 | t-3 |

7/7 criteria covered.

## Judgment calls

- **Status flip is the last step of finalize, not the first.** Chose flip-after-deletes over
  flip-first. Flip-first turns any failed branch delete into a permanent already-finalized
  short-circuit (F3) — the exact trap c-2 exists to remove, re-created one step earlier.
  Rejected an "early-return also re-runs the cleanup" design: after the branch is gone the
  merge state is unknowable, so the recovery path would run destructive git with no guard.
- **The already-finalized short-circuit reports leftovers instead of acting on them.** A
  hand-set `status = complete` with a live branch prints the branch and routes to
  `dross milestone prune` rather than deleting it. Rejected deleting it anyway (finalize's
  merge guard hasn't run, so it would be an unguarded delete) and rejected staying silent
  (F3 becomes invisible).
- **Gone-branch is an error, not an exit-0.** c-3 only demands the message distinguish the two.
  Chose non-zero: a gone branch with a still-`active` toml means something went wrong that the
  user must resolve, and the fix (`milestone set status complete`) is named in the message.
  Exit 0 here would let a broken finalize look successful.
- **A new `milestone progress` command rather than the prompt deriving doneness.** Rejected
  having milestone.md shell `phase list` + read verify.toml, because `phase list` cannot see an
  unscaffolded slug — the one case locked phases_done_test explicitly calls not-done (F8). One
  deterministic CLI answer keeps the locked rule in Go, where a test can hold it.
- **Doneness derived from `completed <slug>` history + changes.json PR, not verify verdict.**
  The locked decision says complete-or-shipped; verify verdict = "pass" means verified, which
  is a strictly earlier state. Rejected reusing renderMilestone's verdict count (it would call
  a verified-but-unshipped phase done).
- **`replace` shipped alongside `remove`.** c-7 says "removed or replaced"; remove+add appends,
  which silently reorders `phases` (F7). Rejected leaving replace to remove+add, and rejected
  index-based addressing — locked remove_addressing fixes exact-value addressing, and replace
  inherits it.
- **c-6 before c-5 in the detector (t-2 → t-6), not merged into one task.** Two distinct
  failure modes (wrong comparison ref; missing status gate) with different fixtures. Sequenced
  rather than parallel because the status gate must apply to whatever ref t-2 settles on —
  merging them would let a passing test for one mask the other.
- **t-8 exists despite adding no production code.** The flip/gate seam (F13) is invisible to
  both owning tasks: t-1's tests never load the detector, t-6's never run finalize. A cheap
  scripted fixture is the only place that pairing is asserted.
