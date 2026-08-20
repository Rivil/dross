# mirror-terminal-state — MVP lens

Phase mirror-terminal-state — 5 tasks across 2 waves

Wave 1
  t-1  Exclude every mirror namespace from pull
       files:    internal/board/board.go, internal/cmd/issue.go,
                 internal/board/board_test.go, internal/cmd/issue_test.go
       covers:   c-1
       desc:     board.IsLinked scans Milestones, Backlog and Tasks alongside Phases and
                 Quicks. collectInbound additionally drops any issue carrying the `dross`
                 marker label, so a mirror created on a branch board.json never saw is
                 still excluded.
       contract: an issue id present only in Board.Backlog, and one present only in
                 Board.Tasks[..].Issue, are each reported linked — a namespace IsLinked
                 does not scan fails board_test by name;
                 collectInbound drops an issue that is in no namespace but carries the
                 `dross` label, and still returns one that is neither (the DRO-133 case) —
                 a feed that returns the labelled mirror fails issue_test.
       depends:  —

  t-2  Add task-complete terminal status and --close
       files:    internal/configenum/configenum.go, internal/forge/youtrack.go,
                 internal/forge/jira.go, internal/cmd/issue_task.go,
                 assets/prompts/ship.md, internal/cmd/issue_task_test.go,
                 internal/cmd/ship_prompt_test.go
       covers:   c-2
       desc:     New `task-complete` member of configenum.LifecycleStatuses, keyed in
                 defaultYouTrackStateMap ("Verified") and defaultJiraStateMap ("Done").
                 task-sync gains --close and validates --status against LifecycleStatuses
                 the way phase-sync does; on forgejo/gitea/gitlab --close writes no state
                 and plainly closes. ship.md's finalize step emits
                 `dross issue task-sync <phase-id> --status task-complete --close`.
       contract: `task-sync <phase> --status task-nonsense` exits non-zero naming the
                 LifecycleStatuses list — today an unknown status is accepted and lands on
                 the card as a label;
                 `task-sync --close` against a YouTrack fake whose read-back answers
                 resolved:null returns an error and prints no "(closed)" line;
                 `task-sync --close` against a forgejo fake performs a plain close and
                 makes zero State writes;
                 ship_prompt_test fails if ship.md's finalize step loses the
                 `task-sync … --status task-complete --close` line.
       depends:  —

  t-3  Close epic and quick through one verified path
       files:    internal/cmd/issue.go, assets/prompts/milestone.md,
                 internal/cmd/issue_close_truth_test.go,
                 internal/cmd/milestone_dispatch_prompt_test.go
       covers:   c-3, c-4
       desc:     closeBoardIssue reads the issue back after the write and errors unless the
                 tracker reports Resolved or State=="closed". `issue quick --close` stops
                 calling client.CloseIssue directly and routes through it (so the project's
                 state_map terminal value is written and verified); `issue milestone-sync
                 <version> --close` closes the linked entity the same way. milestone.md's
                 finalize step emits `dross issue milestone-sync <version> --close`.
       contract: `issue quick <ref> --close` against a YouTrack fake that answers
                 resolved:null returns an error and prints no "(closed)" line — today it
                 reports a close over an issue the tracker still holds open;
                 the same close writes the value from [board].state_map's `complete`
                 override, not the built-in default — the fake records the written State;
                 `milestone-sync --close` on an epic-mode entity closes that issue and
                 fails when the read-back is unresolved; on a version-mode entity id (not
                 an issue) it reports "nothing to close" and exits 0;
                 milestone_dispatch_prompt_test fails if milestone.md's finalize loses the
                 `issue milestone-sync <version> --close` line.
       depends:  —

Wave 2 (depends t-1, t-2, t-3)
  t-4  Close backlog mirrors that left the live set
       files:    internal/cmd/issue.go, assets/prompts/ship.md,
                 internal/cmd/issue_backlog_close_test.go
       covers:   c-6
       desc:     syncBacklog diffs the recorded board.Backlog keys against the item set it
                 just built and closes each dropped mirror through closeBoardIssue. A
                 routed entry whose target phase's issue reads resolved leaves the live
                 set. ship.md's finalize step gains `dross issue backlog-sync <version>`.
       contract: a board.json backlog key for a `slug:` phase that is now scaffolded has
                 its issue closed on the next sync, and a second run closes nothing (the
                 fake counts close calls — a non-idempotent reconcile fails);
                 a `[routed]` entry whose target phase issue reads resolved is closed,
                 while one whose target issue is still open is left open and updated as
                 before — transposing those two fails the test by entry key;
                 ship_prompt_test fails if ship.md's finalize has no `dross issue
                 backlog-sync` call.
       depends:  t-3

  t-5  Guard the namespace filter and terminal paths
       files:    internal/board/namespace_guard_test.go,
                 internal/cmd/board_lifecycle_divergence_test.go
       covers:   c-5
       desc:     A reflection guard over Board's namespace map fields asserts IsLinked
                 consults each one. A terminal-path guard extends the existing lifecycle
                 divergence file: every lane's terminal status is emitted with `--close`
                 from a prompt, and maps to a state both provider maps treat as resolved.
       contract: adding a namespace field (e.g. `Reviews map[string]string`) to
                 board.Board without teaching IsLinked fails the namespace guard, naming
                 the field — the guard reflects over the struct, so a new namespace cannot
                 be added silently;
                 dropping `--close` from ship.md's task-sync finalize line fails the
                 terminal-path guard naming task-complete as a status with no terminal
                 emission;
                 re-pointing task-complete at a non-resolved state (defaultYouTrackStateMap
                 "task-complete": "Open") fails the guard naming the provider and value.
       depends:  t-1, t-2, t-3

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2 |
| c-3 | t-3 |
| c-4 | t-3 |
| c-5 | t-5 |
| c-6 | t-4 |

Every criterion has exactly one owning task; t-3 owns two because both are the same
edit to one function.

## Judgment calls

- **t-2 spans four layers (enum, two forge maps, cmd, prompt) instead of being split.**
  board_lifecycle_divergence_test.go fails the build in both directions: a Set member
  nothing emits, and a `--status` literal the Set does not hold. Adding `task-complete`
  to the vocabulary without the ship.md emission — or the emission without the map keys —
  is red at commit time. Rejected a 3-task split that would have left two red commits.
- **t-3 merges the epic close (c-3) and the quick close (c-4)** rather than keeping one
  task per criterion. Both are "route this close through closeBoardIssue and verify the
  read-back" in internal/cmd/issue.go; split, the second task would be a two-line edit to
  a function the first just rewrote.
- **The read-back verification is generalised in closeBoardIssue, not per-provider.**
  YouTrack's CloseIssueAs already verifies; Jira's toIssue sets Resolved from the done
  status category; the REST forges set State=="closed". One `Resolved || State=="closed"`
  check after the close covers all of them, and it is what makes the flat_board_close
  decision honest instead of assumed. Rejected adding a CloseAndVerify method to
  BoardClient — a new interface method every backend must implement, for one call site.
- **"target phase has shipped" (c-6) is read from the target phase's board issue being
  resolved**, not from local state.json or plan progress. ship.md's finalize is what
  resolves that issue, so the signal is exactly "the phase shipped", it needs no new
  local record, and it works on a clone whose state.json never saw that phase. Rejected
  "phase directory exists" — that means scaffolded, which is not what c-6 says.
- **`milestone-sync --close` reports rather than errors on version/agile modes.** A Jira
  fixVersion and a YouTrack version bundle are not issues, so there is no issue to close;
  erroring would make ship's unconditional-call promise false for every version-mode repo.
  Epic mode — this repo's — closes for real.
- **No new command.** c-6 could have had a `dross issue backlog-close`; the locked
  backlog_close decision puts it inside backlog-sync, and one owner means one test site.
- **t-5 is wave 2 and test-only.** It cannot be written before the surfaces it guards
  exist, and it deliberately holds no production code — a guard that ships alongside its
  own fix cannot demonstrate it catches anything.
