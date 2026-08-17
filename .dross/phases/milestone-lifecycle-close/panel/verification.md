# Panel draft — verification lens

Every task below was derived by writing its test first: what test would fail if this
criterion regressed, and what is the smallest change that makes that test writable?
Two criteria (c-2, c-4) need two tasks because their contract needs a surface that
does not exist yet — a classifier that can be asserted without git ancestry, and a
machine-readable progress fact the prompt can dispatch on.

```
Phase milestone-lifecycle-close — 7 tasks across 3 waves

Wave 1
  t-1  Classify finalize state before any teardown
       files:    internal/cmd/milestone_finalize_state.go,
                 internal/cmd/milestone_finalize_state_test.go
       covers:   c-2, c-3
       contract: with .dross/milestones/v1.3.toml at status="complete" and
                 origin/milestone/v1.3 deleted, the classifier returns
                 alreadyFinalized — it never reaches an ancestry query, so the
                 fixture with no branch anywhere still classifies finalized
                 (locked already_finalized_evidence)
       contract: origin/milestone/v1.3 absent + local milestone/v1.3 an ancestor of
                 origin/main classifies branchGone, and its rendered message
                 contains "gone" and does NOT contain "is not merged into"
       contract: origin/milestone/v1.3 absent + local milestone/v1.3 carrying one
                 commit missing from origin/main classifies goneUnmerged; the
                 message names the gone remote ref AND the surviving local commits,
                 and finalize refuses
       contract: a child merged into its recorded base but not into origin/main
                 classifies merged with target = the base branch — the existing
                 stacking arm is pinned, not rewritten

  t-2  Measure stale branches against origin main
       files:    internal/cmd/milestone_stale.go,
                 internal/cmd/milestone_stale_test.go,
                 internal/cmd/milestone_prune_test.go
       covers:   c-6
       contract: milestone/v1.0 squash-merged onto LOCAL main only (never pushed)
                 is absent from staleMilestoneBranches; after `git push origin main`
                 on the same fixture it is reported — the two runs differ only in
                 origin/main's position
       contract: the ancestry arm follows the same ref: a branch fast-forwarded into
                 local main with origin/main still behind is not "merged"
       contract: a repo with no origin/<main> ref at all still reports a
                 locally-merged milestone branch stale (fallback survives, offline
                 and no-remote repos keep working)

  t-3  Add milestone remove and replace verbs
       files:    internal/cmd/milestone_listedit.go,
                 internal/cmd/milestone_listedit_test.go,
                 internal/cmd/milestone.go, README.md
       covers:   c-7
       contract: `dross milestone remove v1.3 phases b` on phases = [a,b,c] leaves
                 exactly [a,c] on disk, order preserved; the identical second remove
                 exits non-zero naming the missing value — never a silent no-op
                 (locked remove_addressing)
       contract: `dross milestone replace v1.3 phases b x` on [a,b,c] yields
                 [a,x,c] — the replaced entry keeps index 1, so a phase-order fix is
                 not a reorder
       contract: remove against a scalar path (milestone.title) errors naming the
                 three valid list fields and leaves the toml byte-identical
       contract: bare list names resolve like `add` does — `remove v1.3
                 success_criteria "<text>"` and `remove v1.3 scope.success_criteria
                 "<text>"` hit the same field
       contract: README's `dross milestone {…}` row names `remove` and `replace`
                 (same grep guard shape as TestReadmeMilestoneRowDocumentsBaseFlag)

Wave 2 (depends t-1, t-2, t-3)
  t-4  Write milestone status complete on finalize
       files:    internal/cmd/milestone.go,
                 internal/cmd/milestone_finalize_test.go
       covers:   c-1, c-2
       depends:  t-1
       contract: after `dross milestone complete v1.3 --finalize` on a merged
                 milestone, .dross/milestones/v1.3.toml reads
                 [milestone].status = "complete"
       contract: the second `--finalize` on the same repo exits 0, prints
                 "already finalized", and its output/error contains neither
                 "is not merged into" nor a git delete attempt
       contract: a child merged only into its recorded parent base finalizes to
                 status="complete" while local main's SHA is unchanged
                 (locked stacked_child_status)
       contract: ordering — with a pre-receive hook on the bare origin rejecting
                 branch deletes, finalize returns an error naming the failed remote
                 delete, YET v1.3.toml already reads status="complete" and the
                 re-run exits 0 already-finalized (status is written before teardown)

  t-5  Gate stale detector on milestone status
       files:    internal/cmd/milestone_stale.go, internal/cmd/doctor.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_stale_test.go,
                 internal/cmd/doctor_test.go
       covers:   c-5
       depends:  t-2
       contract: with .dross/milestones/v1.0.toml at status="active", a
                 squash-merged milestone/v1.0 is absent from
                 staleMilestoneBranches and `dross doctor` prints no "Stale
                 milestone branches:" section; flipping that one field to
                 "complete" makes both report it — nothing else in the fixture moves
       contract: milestone/v9.9 with no .dross/milestones/v9.9.toml is never
                 reported stale even when squash-merged
                 (locked toml_less_branch_not_stale)
       contract: `dross milestone prune` leaves an active milestone's squash-merged
                 branch in place — prune consumes the same gated detector, so the
                 doctor fix cannot drift from the destructive consumer
       contract: doctor's issue count is unchanged by the gate when the milestone is
                 complete (the section still counts toward a non-zero exit)

  t-6  Add milestone progress --json
       files:    internal/cmd/milestone_progress.go,
                 internal/cmd/milestone_progress_test.go,
                 internal/cmd/milestone.go, README.md
       covers:   c-4
       depends:  t-3
       contract: phases = [a,b], both scaffolded, state history carrying
                 "completed a" only → `--json` reports phases_done=1,
                 remaining=["b"], all_done=false
       contract: a slug in `phases` with no directory under .dross/phases/ counts
                 as not-done and is listed under `unscaffolded`, even when history
                 carries "completed <slug>" (locked phases_done_test)
       contract: a phase whose only breadcrumb is "shipped <slug>" counts as done
                 (shipped and complete both close a phase)
       contract: the emitted `status` is the milestone toml's [milestone].status
                 verbatim, so the prompt dispatches on one field rather than
                 re-deriving it
       contract: all_done=true only when every listed slug is both scaffolded and
                 breadcrumbed; the exit code is 0 in every arm (dispatch data, not
                 a gate)

Wave 3 (depends t-4, t-6)
  t-7  Dispatch /dross-milestone on milestone progress
       files:    assets/prompts/milestone.md,
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-4
       depends:  t-4, t-6
       contract: milestone.md invokes `dross milestone progress --json` in its
                 pre-flight — the invocation appears BEFORE the "## 2. Title"
                 heading, so scoping is an arm of the dispatch, not the default path
       contract: the all-phases-done arm names `dross milestone complete` and
                 `--finalize`, and states "merge commit" + "not squash" inside that
                 same arm (the c-4 gate is in the arm the user actually reads)
       contract: the phases-outstanding arm names the `remaining` field and routes
                 to /dross-spec — it never reaches `dross milestone create` or the
                 success-criteria walk
       contract: the pre-flight still runs `dross rule show` + `dross interaction
                 show` and milestone.md still carries NO clear-point sentinel, so
                 the interaction-coverage and footer-audit Exempt gates stay green
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 finalize flips status to complete | t-4 |
| c-2 re-finalize reports already-finalized, exits 0 | t-1, t-4 |
| c-3 missing branch ≠ unmerged branch | t-1 |
| c-4 /dross-milestone dispatches on status | t-6, t-7 |
| c-5 stale check skips active milestones | t-5 |
| c-6 stale check measures against origin/main | t-2 |
| c-7 remove/replace a wrong list entry | t-3 |

7/7 criteria covered.

## Judgment calls

- **Status is written BEFORE the branch teardown, not after.** Chose it over
  write-last because c-2's marker must survive a failed remote delete; rejected
  write-last because an interrupted teardown would then leave status="active" with
  origin's ref already gone — the exact c-3 wedge. Precedent: `dross phase complete`
  writes its completion record before teardown for the same stated reason
  (internal/cmd/phase.go). A branch left behind by a failed delete falls to
  `dross milestone prune`, which t-5 makes able to see it (status is no longer active).
- **Branch-gone is a two-way classification, not one refusal.** Chose: origin ref
  absent + local absent-or-contained → finalize proceeds (ff, delete local, flip
  status), because "delete branch on merge" on the forge is the common real cause;
  origin ref absent + local carrying commits off origin/main → refuse, message
  naming both facts. Rejected a single blanket refusal: it would wedge every repo
  whose forge deletes merged branches, which is what c-3 is reported from.
- **A new `dross milestone progress --json` carries c-4, not the prompt alone.**
  Chose to put the phases-done test (locked phases_done_test) in Go where it is
  unit-testable; rejected having the prompt eyeball `.dross/phases/` because a
  markdown-only criterion can only ever be grep-tested, and the locked decision
  deserves a real assertion.
- **Phase doneness reads state-history breadcrumbs (`completed <slug>` /
  `shipped <slug>`) plus directory existence.** Rejected verify.toml verdict=="pass"
  (that is *verified*, not shipped — it would close a milestone over unmerged
  phases) and rejected `state.current_phase_status` (only ever describes one phase).
  Known limit to state in the command's doc comment: history is capped at 50
  entries, so a very long milestone can lose an old breadcrumb and under-report.
- **`replace` ships alongside `remove` rather than leaning on remove+add.**
  Chose it because `phases` is an ordered array and remove+add moves the entry to
  the end — silently reordering delivery. The locked remove_addressing decision
  covers remove's signature only, so replace mirrors it (exact old value, error on
  no match).
- **t-2 and t-5 are separate tasks on the same function.** Split by contract:
  t-2 changes which ref merged-ness is measured against, t-5 changes which branches
  are eligible at all. Serialized (not parallel) only because they edit the same
  loop — stated rather than pretended to be a data dependency.
- **t-6 depends on t-3 for the shared `Milestone()` AddCommand list**, not for
  behaviour. Two new subcommands registering in the same call in
  internal/cmd/milestone.go is a merge conflict if run in parallel; the ordering is
  arbitrary and either could go first.
- **Stale detector's origin/main fallback stays local.** Chose falling back to
  refs/heads/<main> when origin/<main> does not exist; rejected erroring, because
  `staleMilestoneBranches` is also called by doctor in repos with no remote and an
  error there turns a diagnostic into a failure.
