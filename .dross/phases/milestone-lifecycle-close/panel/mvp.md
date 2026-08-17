# mvp lens — milestone-lifecycle-close

Phase milestone-lifecycle-close — 5 tasks across 2 waves

Wave 1
  t-1  Finalize writes complete, reruns short-circuit
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-1, c-2, c-3
       desc:     milestoneFinalize loads the milestone toml first and returns exit 0 with an
                 already-finalized line when status == "complete"; the merge guard splits the
                 missing-origin-ref case out with its own "branch is gone" error; on the
                 success path it sets status = "complete" and saves the toml after the local +
                 remote deletes land, before the closing Printf.
       contract: - after `milestone complete <v> --finalize` on the merged fixture,
                   .dross/milestones/<v>.toml reads status = "complete"; drop the Save and the
                   field assertion fails
                 - a second `--finalize` on that same fixture exits nil and prints
                   already-finalized; remove the short-circuit and it returns the
                   "is not merged into origin/main yet" refusal, failing the test
                 - with status still active and origin/milestone/<v> deleted, the error names
                   the branch as gone and contains no "not merged" substring

  t-2  Gate stale detector on status and origin main
       files:    internal/cmd/milestone_stale.go, internal/cmd/doctor.go,
                 internal/cmd/milestone.go, internal/cmd/milestone_stale_test.go
       covers:   c-5, c-6
       desc:     staleMilestoneBranches takes root as well as repoDir: it skips any
                 milestone/<v> whose .dross/milestones/<v>.toml is absent or reads
                 status == "active", and runs both the ancestry and squash checks against
                 origin/<main> when refs/remotes/origin/<main> resolves (local ref only as the
                 offline fallback). doctor.go and milestonePrune pass root at their call sites.
       contract: - a squash-merged milestone/v1.1 whose toml reads status = "active" is absent
                   from staleMilestoneBranches, and `dross doctor` prints no "Stale milestone
                   branches" line for it
                 - milestone/v1.2 (status complete) cut from a local main that sits one
                   unpushed commit ahead of origin/main is not returned; measuring ancestry
                   against the local ref returns it as "merged" and the assertion fails
                 - a milestone/vX branch with no .dross/milestones/vX.toml is never returned
                   (locked toml_less_branch_not_stale)

  t-3  Add `dross milestone remove`
       files:    internal/cmd/milestone.go, internal/cmd/milestone_test.go
       covers:   c-7
       desc:     New subcommand `remove [version] <list.path> <value>` mirroring `add`'s
                 signature and normalizeListField spellings; deletes the entry equal to the
                 exact value from phases / scope.success_criteria / scope.non_goals, preserving
                 the order of the rest, and errors when no entry matches. Registered in
                 Milestone(). Replace is remove-then-add — no second verb.
       contract: - `milestone remove <v> phases foo` on phases = ["a","foo","b"] leaves
                   ["a","b"] in that order; a filter that drops by prefix or index fails the
                   remaining-entries assertion
                 - removing a value that is not present exits non-zero with an error quoting
                   the value; a silent no-op (nil error, toml unchanged) fails
                 - `milestone remove <v> milestone.title x` errors as not-a-list-field and
                   leaves the toml byte-identical

  t-4  Add `dross milestone progress` phase-done report
       files:    internal/cmd/milestone_progress.go, internal/cmd/milestone.go,
                 internal/cmd/milestone_progress_test.go
       covers:   c-4
       desc:     New subcommand printing each slug in the milestone's phases array with
                 done/not-done plus a done/total tally, and --json for the prompt. Done means
                 the phase directory exists AND (state history carries "completed <slug>", or
                 state marks it shipped, or verify.toml's verdict is "pass"); an unscaffolded
                 slug is not-done (locked phases_done_test). Reuses historyHasAction and
                 readVerifyVerdict.
       contract: - a milestone listing three slugs where one has no phase directory reports
                   2/3 and lists that slug as not-done; counting an unscaffolded slug as done
                   fails the tally assertion
                 - a slug whose verify.toml verdict is "partial" reads not-done while a slug
                   with a "completed <slug>" state-history entry reads done
                 - `--json` emits one entry per slug with a boolean done field — N slugs in
                   the phases array yield N entries

Wave 2 (depends t-1, t-4)
  t-5  Dispatch /dross-milestone on milestone status
       files:    assets/prompts/milestone.md, internal/cmd/milestone_prompt_test.go
       covers:   c-4
       desc:     Prompt gains a §0 dispatch step reading milestone status (`dross milestone
                 show --json`) and `dross milestone progress --json`, then branches three ways:
                 no active milestone → the existing scope flow; active with every phase done →
                 drive completion (`dross milestone complete <v>`, merge as a MERGE COMMIT not
                 a squash, then `--finalize`, which reports already-finalized on a re-run);
                 active with phases left → report the remaining slugs and stop.
       depends:  t-1, t-4
       contract: - milestone.md names all three dispatch arms and the literal
                   `dross milestone progress --json`; deleting the completion arm fails the
                   arm-coverage test
                 - the completion arm carries the merge-commit-not-squash instruction —
                   dropping "merge commit" or the "not squash" qualifier fails that assertion
                 - the scope arm still names `dross milestone create`, so the
                   no-active-milestone path is provably unchanged

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-1 |
| c-3 | t-1 |
| c-4 | t-4, t-5 |
| c-5 | t-2 |
| c-6 | t-2 |
| c-7 | t-3 |

7/7 criteria covered.

## Judgment calls

- **c-1/c-2/c-3 merged into one task**, not three: all three edit one function
  (`milestoneFinalize`), and the already-finalized short-circuit (c-2) is only writable once
  the status write (c-1) exists. Splitting would have produced two tasks that cannot compile
  independently.
- **Status is written after the branch deletes, not before.** A delete failure then leaves
  status active so a re-run retries the teardown; writing it first would let the c-2
  short-circuit skip a cleanup that never happened.
- **A gone origin ref stays an error, with a different message.** c-3 only demands the message
  distinguish the cases; auto-finalizing a milestone whose branch vanished would invent a merge
  nobody observed. The error names the remedy (`dross milestone set <v> status complete`).
- **c-5 + c-6 in one task, not two.** Both are single-predicate changes inside
  `staleMilestoneBranches`, and c-5 forces the `root` parameter that c-6's fixtures need
  anyway. Two tasks would mean two conflicting signature changes to the same call sites.
- **origin/<main> falls back to the local ref when the remote ref is missing.** Rejected
  erroring out: the detector runs inside `dross doctor`, which must work offline and in
  remote-less fixtures. c-6's bug only exists when origin/<main> is present, so the fallback
  cannot reintroduce it.
- **A new `milestone progress` command instead of parsing `dross status`.** Rejected reusing
  status: it renders only `state.current_milestone`, counts done by verify-verdict alone, and
  emits a progress bar no prompt can branch on. The locked `phases_done_test` rule needs a
  test, and a rule buried in markdown cannot have one.
- **t-4 and t-5 kept apart** despite the merge-aggressively bias: together they are 5 files
  across CLI + prompt asset, and their tests are Go-behaviour vs prompt-text — the split falls
  on the natural commit boundary.
- **No task for `milestone replace`.** c-7 says "removed or replaced"; remove + the existing
  `add` covers replacement, and a third verb would be structure no criterion asks for.
- **Existing stale-detector fixtures must gain milestone tomls.** The locked
  `toml_less_branch_not_stale` rule makes every current `milestone_stale_test.go` fixture
  read not-stale; updating them is part of t-2, not a follow-up.
