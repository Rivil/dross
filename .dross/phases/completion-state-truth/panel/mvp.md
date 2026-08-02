# Panel draft — MVP lens

Phase completion-state-truth — 4 tasks across 2 waves

```
Wave 1
  t-1  Move completion write to phase complete
       files:    internal/cmd/ship.go, internal/cmd/phase.go,
                 internal/cmd/ship_test.go, internal/cmd/phase_test.go
       covers:   c-2, c-3, c-5
       contract: ship stops clearing state — after `dross ship`, state.json has
                 current_phase = <id>, current_phase_status = "shipped", and no
                 `completed <id>` history entry; complete writes the transition
                 (clear both fields + append `completed <id>`), a second
                 `dross phase complete <id>` exits 0 with exactly one such entry,
                 and complete's final output names the branch HEAD landed on, the
                 deleted phase/<id> (local + remote) and "not yet on main".

  t-2  Ship prompt: complete owns branch teardown
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go
       covers:   c-1, c-3
       contract: the prompt-content assertion fails if §6 still contains
                 `--delete-branch`, `should_remove_source_branch`, or a
                 `DELETE .../branches/phase%2F<id>` call; and fails if §6 does not
                 name `dross phase complete` as the sole deleter of local+remote
                 phase/<id>, or still says complete "switches to main … records
                 the merge with a chore commit".

  t-3  Status: shipped state + standing topology line
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-5, c-6
       contract: with current_phase set, current_phase_status = "shipped" and an
                 unmerged PR on the phase branch, status prints `(shipped)` and
                 prints NO `stale:` line — the test fails if either half regresses;
                 and on milestone/v1.2 holding 3 commits absent from main, status
                 prints a standing branch line naming `milestone/v1.2` and the
                 count 3 — the test fails if the line is absent or the count is
                 wrong.

Wave 2 (depends t-1)
  t-4  End-to-end live-state regression for ship→complete
       files:    internal/cmd/state_clobber_regression_test.go
       covers:   c-4
       contract: fixture = base branch whose tree still tracks .dross/state.json
                 plus a live untracked copy of 12 entries at version 9.9.9.9. After
                 ship → phase complete, the test fails if the live file has fewer
                 than 12 entries, if its version is not 9.9.9.9, or if history
                 lacks the `completed <id>` entry (that last assertion is what
                 fails against current code, where ship writes it pre-merge and
                 complete writes nothing). A subtest control does a raw
                 `git checkout <base>` and asserts THAT clobbers, so the fixture
                 is proven to contain a real clobber.
```

## Task detail

**t-1 — Move completion write to phase complete** (wave 1, depends: —)
`internal/cmd/ship.go` step 5: replace the `s.CurrentPhase = ""` /
`s.CurrentPhaseStatus = ""` / `s.Touch("completed <id>")` block with
`s.CurrentPhaseStatus = "shipped"` (current_phase left set) and a `shipped <id>`
touch; drop the `Completion record folded into … squash-merge will land it on …`
narration and the step-5 comment block that explains the fold.
`internal/cmd/phase.go`: replace the "No state write here" comment at the end of
`phaseComplete`'s RunE with the actual write — reload state after the checkout/ff,
clear `CurrentPhase` + `CurrentPhaseStatus`, `Touch("completed <id>")` guarded by a
history scan so a re-run appends nothing, and `Save`. The write is local-only and
commits nothing: state.json is gitignored, so it cannot re-seed base divergence —
that is why the fold into ship is no longer needed. Rewrite the `Long` help text
(lines 236-249) which currently states ship folds the record into the squash.
Same RunE: replace the single `completed %s — %s is at origin, phase/%s deleted`
line with the topology statement (HEAD branch, phase/<id> deleted local+remote,
work is on <base> and not yet on main).

**t-2 — Ship prompt: complete owns branch teardown** (wave 1, depends: —)
`assets/prompts/ship.md` §6 step 1: GitHub becomes `gh pr merge <pr-url> --squash`;
Forgejo/Gitea loses the `DELETE …/branches/phase%2F<id>` call; GitLab's merge body
becomes `{"squash":true}`. Step 3's description of `dross phase complete` is
rewritten to say it clears the phase state, fast-forwards the recorded base and
deletes phase/<id> local + remote, and that HEAD stays on phase/<id> until it runs.
Add the matching assertions to `internal/cmd/ship_prompt_test.go` using the
existing `shipPromptContent` helper. Run `make install` after editing (rule r-01).

**t-3 — Status: shipped state + standing topology line** (wave 1, depends: —)
`internal/cmd/status.go`: `staleCompletedState` (and its `stateRecordsCompleted`
helper) keys off a `completed <id>` breadcrumb that ship no longer writes — retire
the `stale:` warning block at lines 69-82 and let `renderPhase` carry the truth via
`current_phase_status = "shipped"`. Add a standing branch line (after the phase
block) naming the current work branch and its commit count not yet on the main
branch, computed from local refs only (`git rev-list --count <main>..<branch>`) so
status stays network-free.

**t-4 — End-to-end live-state regression** (wave 2, depends: t-1)
`internal/cmd/state_clobber_regression_test.go`: new test reusing the existing
`incidentRepo` / `hasAction` / `actions` helpers and `stubPRMerged`, driving
`Ship()` then `Phase() complete` in-process against a base branch that tracks a
stale `.dross/state.json`, asserting the live copy survives with its history and
version and carries complete's `completed <id>` entry.

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (no unguarded branch switch in ship; complete owns every checkout/ff/delete) | t-2 |
| c-2 (complete records the transition itself; idempotent) | t-1 |
| c-3 (no surface claims the record rides the squash) | t-1, t-2 |
| c-4 (end-to-end regression, fails against current code) | t-4 |
| c-5 (topology reported at completion + standing in status) | t-1, t-3 |
| c-6 (shipped-but-unmerged reads `shipped`, no stale warning) | t-3 |

## Judgment calls

- **Merged the ship-side and complete-side state writes into one task (t-1)** rather
  than one task per file: split, the intermediate commit has *nobody* writing the
  completion record, which is a strictly worse state than today's. Rejected the
  cleaner-looking two-task split for that reason.
- **Removed the remote branch-delete from all three providers, not just GitHub's
  `--delete-branch` flag.** The `teardown_owner` decision names only GitHub, but c-1
  says every branch deletion "local and remote" is performed by `dross phase
  complete`, and complete's `ls-remote`-guarded delete is already idempotent.
- **Folded complete's topology statement into t-1 instead of giving c-5 its own
  task.** It is a one-line change to a print statement in a file t-1 already
  rewrites; a separate task would be under ten minutes of work.
- **Retired the `stale:` warning outright rather than re-keying it on `shipped`.**
  c-6 says the shipped-but-unmerged state is normal and must read as `shipped`, not
  as a warning; keeping a re-keyed warning would re-create the thing c-6 forbids.
  The squash-resolution machinery (`resolveSquashCommit`, `staleSquashScanLimit`)
  goes with it unless another caller needs it.
- **No new CLI verb.** Considered `dross phase topology` / `dross state set
  current_phase_status shipped` as explicit surfaces; neither is traceable to a
  criterion — ship already writes state inline and status already renders lines.
- **Left README.md and docs/roadmap.md out of scope.** c-3 names code comments, CLI
  narration and prompts; the roadmap line is a historical "candidate fix" note, not
  a claim about current behaviour, and no README line asserts the record rides the
  squash.
- **Kept t-4 in wave 2.** It is the only task that strictly needs another task's
  output (the ship→complete flow it drives); everything else is parallel wave 1.
