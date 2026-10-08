# Verification-lens draft: board-finalize-on-complete

Bias: each task is the smallest change that makes its contract satisfiable. Several contracts also name an existing guard test that the change would otherwise break or leave vacuous. Those are listed on purpose, because a plan that ignores them stalls at the first `dross test`.

```
Phase board-finalize-on-complete — 16 tasks across 3 waves

Wave 1
  t-1  Extract TOML patcher into internal/tomlpatch
       files:    internal/project/patch.go, internal/project/patch_value.go, internal/project/patch_diff.go,
                 internal/project/project.go, internal/tomlpatch/patch.go, internal/tomlpatch/diff.go,
                 internal/tomlpatch/save.go, internal/cmd/project_writer_pin_test.go,
                 internal/cmd/lossless_docs_test.go, ARCHITECTURE.md
                 (patch_test.go / patch_diff_test.go move with the code into internal/tomlpatch/)
       covers:   c-4, c-5
       depends:  —
       desc:     Move the raw-byte patcher (indexDoc/apply/ops, differ, value renderer, atomic writer,
                 verify-before-write) into a generic internal/tomlpatch.Save[T]. project.Project.Save
                 delegates, passing its decode/refuseRemote check as the load-validator. No behaviour change.
       contract: - the existing lossless suite (internal/project/save_lossless_test.go,
                   internal/cmd/project_lossless_test.go incl. TestLosslessRealProjectTomlOneLineDiff)
                   passes unchanged through the delegate. A patcher regression in the move breaks the
                   one-line diff on the real project.toml
                 - the planOps "deliberately wrong op" seam still proves verify-before-write refuses and
                   leaves project.toml byte-identical, now through tomlpatch.Save
                 - TestEncodeFreshIsTheOnlyEncoder scans internal/tomlpatch: an injected toml.Marshal
                   fallback or a second NewEncoder there is reported by name
                 - TestArchitectureNamesLosslessWriter resolves the patcher anchors at their new
                   internal/tomlpatch file:line. A stale internal/project/patch.go anchor fails it

  t-2  Quiet unknown dross identity-label lookups
       files:    internal/forge/forge.go, internal/forge/forge_test.go, internal/forge/youtrack_test.go,
                 internal/boardsync/identity_label_test.go
       covers:   c-3
       depends:  —
       desc:     Add forge.IsIdentityLabel (prefixes dross/phase:, dross/task:, dross/deferred:,
                 dross/target:). WarnDroppedLabels names only non-identity drops. When every requested
                 label is unknown, ListIssues still returns nothing (no card), on all four backends
                 (shared helper).
       contract: - WarnDroppedLabels("youtrack", [dross/task:p/t-1, dross/phase:p, dross/deferred:d1,
                   dross/target:q]) writes nothing to stderr. With [dross/phase:p, bug] the line names
                   `bug` and not `dross/phase:p`
                 - TestRESTListIssuesDropsUnknownLabels and the YouTrack twin gain an identity row:
                   Labels=[dross/phase:never-synced] against an index lacking it returns zero issues,
                   makes zero issue-list requests and leaves stderr empty. The existing `typo` rows
                   still warn
                 - ResolvePhaseIssue over a YouTrack fake whose tag list lacks dross/phase:new returns
                   "" with empty captureStderr output, so it resolves to no card silently
                 - identity_label_test: every prefix in reap_discover.go's identityLabels, and the
                   output of PhaseLabel/TaskLabel/DeferredLabel/TargetLabel, satisfies
                   forge.IsIdentityLabel. StatusLabel("x"), LabelQuick and LabelMarker do not. A new
                   dross/<kind>: identity constructor whose prefix is not registered fails here

  t-3  Name [board].enabled in board prompts
       files:    assets/prompts/execute.md, assets/prompts/milestone.md, assets/prompts/plan.md,
                 assets/prompts/quick.md, assets/prompts/ship.md, assets/prompts/verify.md,
                 internal/cmd/board_prompt_wording_test.go
       covers:   c-10
       depends:  —
       desc:     Replace every "no-op unless `[remote].board_sync` is on — safe to always run" (execute
                 :57,:396, milestone :111, plan :252, quick :203,:240, ship :163, verify :91) and the bare
                 "no-op when board sync is off" (execute :116,:345) with wording that names
                 `[board].enabled` and says a non-zero `dross issue …` exit is a board failure to
                 surface, not the disabled no-op.
       contract: - board_prompt_wording_test reads assets/prompts/*.md RAW (promptContent strips
                   underscores, so a normalised scan would miss it) and fails on any `board_sync`.
                   Reverting execute.md:57 alone fails it
                 - every prompt with a `dross issue {phase sync|task sync|quick|milestone sync}` call
                   line must contain `[board].enabled` and the non-zero-exit-is-a-failure sentence.
                   Dropping the sentence from verify.md fails by file name
                 - anti-vacuity: fails if fewer than 6 prompts with a `dross issue` call are found

  t-4  Add idempotent board phase finalizer
       files:    internal/boardsync/finalize.go, internal/boardsync/finalize_test.go,
                 internal/boardsync/ctx.go
       covers:   c-1
       depends:  —
       desc:     FinalizePhase(ctx, phaseID) refuses unless changes.json reads complete. It then
                 resolves or creates the phase card and drives it to complete+closed, and drives every
                 plan task card to task-complete+closed (creating missing ones). Cards the tracker
                 already reads done are skipped. board.json is saved once. Adds the StatusComplete
                 constant.
       contract: - faultBoard seeded with an open phase card at shipped and three task cards at
                   task-in-review: afterwards the phase card carries dross/status:complete and reads
                   closed, and each task card carries dross/status:task-complete and reads closed
                 - no cards at all: the phase card and every task card are created and then closed
                   (close-on-create), and board.json gains the phase link and three task links
                 - a second FinalizePhase on the same board records zero
                   CreateIssue/UpdateIssue/CloseIssue calls (callsOf shows reads only)
                 - faultBoard refuses the close of t-2: t-1 and t-3 still close, and the error names
                   t-2. The TaskCloseError aggregation is not short-circuited
                 - no plan.toml: the phase card is finalized, nil error, zero task-card calls
                 - changes.json status=shipped: the error names "shipped" and zero board calls are made
                 - issue_test's ctx.go-constant membership check covers StatusComplete

  t-5  Record absorbed deferred ids on criteria
       files:    internal/phase/phase.go, internal/cmd/validate.go, internal/cmd/validate_test.go
       covers:   c-7
       depends:  —
       desc:     Criterion gains `Deferred []string` (toml/json "deferred,omitempty"). validate walks
                 every phase spec's criteria and refuses an absorbed id that names no deferred item
                 (phase specs + .dross/deferred.toml) whose target is that phase.
       contract: - spec P, criterion c-1 deferred=["d-404"] with no such id anywhere: validate exits
                   non-zero with a ✗ line naming P's spec path, c-1 and d-404
                 - an id that exists but is routed to Q is refused naming Q. An id that exists but is
                   unrouted (someday) is refused
                 - an id routed to P, held in another phase's spec or in .dross/deferred.toml, passes (✓)
                 - a criterion with no absorbed ids round-trips byte-identical (omitempty), and
                   json_tag_parity_test stays green

  t-6  Refuse routing to a complete phase
       files:    internal/cmd/deferred_target.go, internal/cmd/deferred.go, internal/cmd/survivor.go,
                 internal/cmd/deferred_add.go, internal/cmd/route_complete_test.go
       covers:   c-8
       depends:  —
       desc:     refuseCompleteTarget(root, slug), built on changes.Complete, runs right after
                 validDeferredTarget and before any read-modify-write in `deferred route`,
                 `survivor route` and `deferred add --target`. validate's dangling-target walk is
                 left as is.
       contract: - `deferred route alpha 0 --target done-phase` (done-phase changes.json
                   status=complete) errors naming done-phase and "complete". A hash of the whole .dross
                   tree is identical before and after
                 - `survivor route x.go:3 --op … --target done-phase` errors naming done-phase. The
                   current spec.toml and survivors.toml are byte-identical
                 - `deferred add --target done-phase` is refused the same way, with no spec or
                   deferred.toml write and zero board requests
                 - control: the same three commands succeed when the target is at status=shipped. The
                   gate keys on complete, not on "has a changes.json"
                 - `dross validate` on a repo already holding an item routed to done-phase still
                   passes (historical routes are not retro-flagged)

Wave 2
  t-7  Keyed array diff with insert/move ops   (depends t-1)
       files:    internal/tomlpatch/diff.go, internal/tomlpatch/patch.go, internal/tomlpatch/save.go,
                 internal/tomlpatch/keyed_test.go
       covers:   c-4, c-5
       depends:  t-1
       desc:     tomlpatch.Save takes per-array identity keys (task→id, accepted→key, category→name).
                 Keyed arrays match elements by key, emit per-field ops inside a matched element,
                 insert a new element at its position, delete by key, and relocate a reordered block.
                 A comment run directly above a [[header]] (no blank line between) belongs to that
                 element.
       contract: - fixture [[task]] t-1..t-4 with `# about t-3` directly above t-3's header and an inline
                   `# keep` on t-1's title: inserting t-5 between t-2 and t-3, then cutting t-5's block
                   out of the result, gives back the original bytes exactly
                 - deleting t-2 keeps `# about t-3` (it belongs to t-3) and removes t-2's own body comment
                 - moving t-3 before t-1 relocates t-3's header, body and `# about t-3` together, and the
                   t-1, t-2 and t-4 blocks remain byte-identical substrings
                 - changing t-4.status while removing t-2 in the same diff touches exactly two regions:
                   t-4's status line and t-2's block
                 - an unkeyed array ([[runtime.test_lane]]) keeps positional/deep-equal behaviour, and the
                   project lossless suite stays green
                 - a keyed patch that would not decode to the saved value is refused, and the file is
                   left untouched

  t-8  Finalize the board from phase complete   (depends t-4)
       files:    internal/cmd/phase.go, internal/cmd/issue.go, internal/cmd/phase_complete_board_test.go,
                 internal/cmd/testdata/cli_surface/issue.txt, README.md
       covers:   c-1
       depends:  t-4
       desc:     After branch teardown, complete runs openBoard and then boardsync.FinalizePhase, and
                 commits and publishes board.json through autoCommitDrossDirt + routeBaseChores. A board
                 error exits non-zero naming `dross issue phase finalize <id>`, with the completion
                 record intact. Adds `dross issue phase finalize <id>`: the same finalizer, a no-op when
                 the board is off.
       contract: - e2e (shipFixture + simulateSquashMerge, with taskCloseFake as a forgejo board and
                   [board].enabled=true): after complete, the phase card and every task card read closed
                   with dross/status:complete and dross/status:task-complete. `git status --porcelain` is
                   empty and local main == origin/main
                 - re-running `dross phase complete x` makes zero create/update/close requests and leaves
                   the base sha unchanged
                 - [board].enabled=false with the fake still configured: complete exits 0 and the fake
                   counts zero requests
                 - fake 500s on issue create: complete returns an error containing
                   "dross issue phase finalize x". state.json still holds `completed x`, changes.json is
                   status=complete, and phase/x is gone locally and on origin. A following
                   `dross issue phase finalize x` closes every card
                 - `issue phase finalize x` with the board off exits 0 with zero requests. The
                   cli_surface issue.txt golden lists `issue phase finalize`, so TestCLISurface fails
                   without it

  t-9  Reap creates missing completed-phase cards   (depends t-4, t-2)
       files:    internal/boardsync/reap_missing.go, internal/boardsync/reap_missing_test.go,
                 internal/boardsync/reap.go, internal/boardsync/reap_apply.go,
                 internal/cmd/issue_reap_cmd.go
       covers:   c-11
       depends:  t-4, t-2
       desc:     The ReapPlan gains Missing: completed phases (changes.Complete) with no
                 dross/phase:<id> card in any state, and completed phases' plan tasks with no
                 dross/task: card. Both are read from one State:"all" marker listing, not per-label
                 probes. Dry-run prints them; --apply calls FinalizePhase for each.
       contract: - dry run over writingIsFatalYT with two completed, card-less phases lists both phases
                   and their tasks. board.json and reap-log.json stay byte-identical, and any write
                   request fails the test
                 - a completed phase whose card exists CLOSED but is absent from board.json is not
                   listed
                 - a completed phase that has its card but one card-less task lists only that task
                 - a status=shipped phase with no card is not listed (deny-by-default on the record)
                 - --apply: every listed card is created and reads closed at complete or task-complete,
                   and a second dry run lists nothing
                 - the dry run's stderr has no "does not know the label" line

  t-10 Disposition-record predicate for routed items   (depends t-5)
       files:    internal/boardsync/disposition.go, internal/boardsync/disposition_test.go
       covers:   c-6
       depends:  t-5
       desc:     RoutedDisposition(root, entry) reads disk only. Survivor item: disposed when
                 survivors.toml accepts its key, or when the target's finalized verify run measured the
                 survivor's package and lists the key in neither languages[].mutation.surviving nor
                 out_of_scope. Plain item: disposed when the target is complete AND a target criterion's
                 `deferred` holds the item's id.
       contract: table rows, one assertion each:
                 - survivor key accepted in survivors.toml → disposed, and why names survivors.toml
                 - key absent from the target's tests.json, verify.toml finalized, run measured the
                   survivor's dir → disposed. Key still in out_of_scope → not. No tests.json → not. Run
                   measured only other packages → not. verify.toml not finalized → not
                 - plain item: target complete + criterion deferred=[id] → disposed. Target complete
                   with no absorbing criterion → not (the 47a5c93 rule). Absorbing criterion but target
                   not complete → not. Item with an empty id → not
                 - the signature takes (root, entry) and no board client, so a decision derived from
                   card state cannot compile

  t-11 Re-route a survivor in place   (depends t-6)
       files:    internal/cmd/survivor.go, internal/cmd/survivor_route_test.go
       covers:   c-9
       depends:  t-6
       desc:     survivor route first searches every deferred source for an undismissed entry with the
                 key. If found, it sets that entry's Target in its own source spec (id unchanged); the
                 same target means no write. If not found, it appends as today, minting an id.
       contract: - route K→alpha, then K→beta: exactly one [[deferred]] across all sources carries
                   survivor=K, with its id unchanged and target=beta
                 - backlog sync after each route (TestReroutedDeferredSwapsItsTargetLabel harness): the
                   second sync updates the same issue key (label swapped to dross/target:beta) with zero
                   creates
                 - routing K→beta again leaves the source spec byte-identical with mtime unchanged, and
                   stdout says it is already routed
                 - an entry that lives in another phase's spec is updated there, and the current phase's
                   spec is untouched
                 - re-routing to a complete target is refused by t-6's gate before the lookup, and the
                   entry keeps its old target

  t-12 Add deferred absorb verb for /dross-spec   (depends t-5, t-6)
       files:    internal/cmd/deferred_absorb.go, internal/cmd/deferred_absorb_test.go,
                 internal/cmd/deferred.go, assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-7
       depends:  t-5, t-6
       desc:     `dross deferred absorb <phase> <criterion-id> <source> <idx>` resolves the parked item,
                 requires its target to be <phase>, stamps an id if it has none, and appends that id to
                 the criterion's `deferred` list (idempotent). spec.md tells /dross-spec to run it for
                 each criterion seeded from a parked item, after §5 writes spec.toml.
       contract: - absorb writes the item's stable id, never `<source> <idx>`, into the criterion.
                   `dross validate` then passes, and a second run leaves spec.toml byte-identical
                 - an item routed elsewhere or unrouted is refused naming its target, and spec.toml is
                   byte-identical
                 - an unknown criterion id is refused naming it
                 - TestDeferredEntryIDStaysInternal is unchanged: `deferred list --json` still has no
                   `id` key
                 - spec_prompt_test: spec.md contains `dross deferred absorb` on the parked-item path,
                   after the `## 5. Write spec.toml` heading. Deleting the line fails the test

Wave 3
  t-13 Write plan.toml through the patcher   (depends t-7)
       files:    internal/phase/phase.go, internal/phase/plan_lossless_test.go,
                 internal/phase/phase_test.go, internal/cmd/task_lossless_test.go
       covers:   c-4
       depends:  t-7
       desc:     Plan.Save goes through tomlpatch.Save with [[task]] keyed by id; Spec.Save stays on
                 saveTOML. This one seam covers every plan writer: task status/add/edit/move/remove,
                 `issue task pull --apply`, migrate.
       contract: - task_lossless_test drives `task status`, `task add --after`, `task edit --title`,
                   `task move --before` and `task remove --force` over a fixture with a header comment,
                   `# about t-3`, inline comments on untouched task lines and a hand-wrapped multi-line
                   test_contract. For each verb, every line outside the changed task block(s) is
                   byte-identical (on add, the task_seq line may also change)
                 - move t-4 before t-2 with a pending dependent whose wave reflows: the changed regions
                   are t-4's relocated block and the dependent's wave line, nothing else
                 - remove --force t-2 with dependent t-3: only t-2's block and t-3's depends_on line
                   change
                 - TestSaveTOMLAtomicFailurePreservesFile is re-pointed to a failure the new writer
                   actually hits (read-only phase dir; the `<path>.tmp` dir trick no longer blocks a
                   CreateTemp name) and still proves plan.toml byte-identical
                 - a no-op status write (done→done) writes nothing (mtime unchanged)

  t-14 Write survivors.toml through the patcher   (depends t-7)
       files:    internal/survivor/store.go, internal/survivor/store_test.go,
                 internal/cmd/survivor_lossless_test.go
       covers:   c-5
       depends:  t-7
       desc:     survivor.Save goes through tomlpatch.Save with [[accepted]] keyed by key and
                 [[category]] by name. Store.validate still runs before the write.
       contract: - fixture repo with a COMMITTED survivors.toml (the secret self-scan reads tracked
                   files only) whose accepted entry has a GitHub-token-shaped text and a trailing
                   `# dross:allow-secret`: `dross survivor accept` of a new survivor keeps every
                   original line byte-identical, and `dross validate` exits 0
                 - control in the same test: re-encoding that store with toml.NewEncoder drops the
                   marker and `dross validate` reports a secret, which proves the marker is load-bearing
                   and the scan sees the file
                 - `survivor retire` of the middle of three entries leaves the other two blocks,
                   markers included, byte-identical
                 - accept with a new category inserts its [[category]] block after the last existing
                   category, with accepted blocks untouched
                 - `survivor route` leaves survivors.toml byte-identical (route writes only a spec)

  t-15 Drop ship §6 close steps; re-point guards   (depends t-8, t-3)
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-2
       depends:  t-8, t-3
       desc:     Remove ship.md §6 steps 4–5. Step 3 now says `dross phase complete` finishes the board,
                 and a board failure there is retried with `dross issue phase finalize <id>`. Rewrite
                 the ship prompt tests to pin the absence, and point the lifecycle divergence guards at
                 FinalizePhase's emissions in Go source.
       contract: - ship.md contains neither `issue task sync <phase-id> --status task-complete --close`
                   nor `issue phase sync <phase-id> --status complete --close`, and does name
                   `dross issue phase finalize` in §6. Restoring either line fails the test
                 - the squash-merge → `--status shipped` → `dross phase complete` ordering assertion is
                   kept. TestShipPromptClosesTaskCardsAfterPhaseComplete is replaced, and the
                   "phase complete gains no board coupling" check is deleted (superseded by
                   board_failure_posture)
                 - emittedStatuses parses FinalizePhase's status arguments: dropping StatusTaskComplete
                   from the finalizer makes TestEmittedStatusesAreTheLifecycleSet fail on task-complete
                   (keyed by both state maps, emitted by nothing)
                 - mirrorLanes Phases/Tasks point at internal/boardsync/finalize.go, so
                   TestEveryMirrorLaneHasATerminalEmission fails if FinalizePhase stops closing either
                   lane
                 - TestCloseEmissionsCarryAValidStatus counts finalizer emissions, so it stays
                   non-vacuous once the prompt `--status … --close` lines are gone

  t-16 Close routed mirrors on disposition only   (depends t-10, t-9)
       files:    internal/boardsync/backlog.go, internal/boardsync/reap.go,
                 internal/boardsync/reap_discover.go, internal/boardsync/backlog_reconcile_test.go,
                 internal/boardsync/reap_classify_test.go
       covers:   c-6
       depends:  t-10, t-9
       desc:     BacklogVerdictFor resolves a live routed item only when RoutedDisposition says it is
                 disposed. routedVerdict (for linked cards and discovered dross/deferred: cards)
                 strands only on disposition and otherwise keeps the unattributable "finished
                 destination" reason. A live key keeps its link after the close; IssueIsDone prevents a
                 re-close.
       contract: - TestBacklogVerdictTable gains rows: a routed survivor that is accepted → Resolved; a
                   routed item with its target complete and no absorbing criterion → StillOpen (the
                   47a5c93 row is kept verbatim)
                 - agreement test: over the same fixture rows, BacklogVerdictFor==Resolved iff
                   reapBacklogVerdict==ReapStranded, so the two lanes share one predicate and cannot
                   drift
                 - ReconcileBacklog closes an absorbed routed item's mirror once and keeps its
                   board.json key. A second sync makes zero CloseIssue calls
                 - a label-discovered dross/deferred:<id> orphan whose item is disposed strands, with
                   why naming the record. With the target complete and no record it stays
                   unattributable
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 complete finishes the board; idempotent; silent when off | t-4, t-8 |
| c-2 ship §6 drops the close steps, prompt test pins it | t-15 |
| c-3 unknown identity labels are quiet and resolve to no card | t-2 |
| c-4 plan.toml writes are lossless | t-1, t-7, t-13 |
| c-5 survivors.toml writes are lossless, markers survive, validate green | t-1, t-7, t-14 |
| c-6 routed mirrors close only on a disposition record | t-10, t-16 |
| c-7 criteria record absorbed ids; /dross-spec writes them; validate refuses bad ids | t-5, t-12 |
| c-8 route verbs refuse a complete target | t-6 |
| c-9 re-routing a survivor updates in place | t-11 |
| c-10 prompts name `[board].enabled`; non-zero exit is a failure | t-3 |
| c-11 reap finds and creates missing completed-phase cards | t-9 |

All 11 criteria are covered.

## Judgment calls

- **Identity-label quieting is prefix-based in forge, with a boardsync guard (t-2).** Rejected: an `IssueFilter.Identity` flag set by each caller. A prefix rule covers every current and future caller, and the guard makes "every other dross/ identity label" a testable property.
- **`dross/target:` counts as an identity label.** reap's identityLabels already treats it that way. `dross/status:`, `dross/quick` and the marker do not count.
- **The patcher is extracted to internal/tomlpatch rather than exported from internal/project.** plan and survivor should not import the config package. t-1 is not split even though it touches many files: every intermediate state of the move fails the writer-pin and anchor guards.
- **Keyed arrays plus insert and move ops are added (t-7).** Rejected: reusing the positional/deep-equal differ. AddTask inserts mid-list and MoveTask reorders. A positional diff would rewrite the neighbours' lines, and verify-before-write would refuse the reorder outright.
- **A comment run directly above a header belongs to the element below it.** Without this, the contracts for remove and move are undefined. The current blockEnd rule would delete t-3's note when t-2 is removed.
- **Spec.Save stays on saveTOML.** spec.toml lossless writes are not in c-4/c-5, and the routing verbs that write specs keep today's behaviour.
- **Absorbed ids are written by a new `dross deferred absorb` verb (t-12).** Rejected: exposing ids in `deferred list --json`. That route would break the older locked deferred_identity decision (TestDeferredEntryIDStaysInternal). The verb honours absorption_record, keeps ids off the prompt wire, and makes the write testable end to end.
- **FinalizePhase refuses unless changes.json reads complete.** One guard protects all three callers (complete, the retry verb, reap --apply) from closing an unfinished phase's cards.
- **"Re-running changes nothing" is tested as zero tracker writes, so FinalizePhase skips cards the tracker already reads done.** Rejected: re-closing on every run. YouTrack's CloseIssueAs rewrites state on every call.
- **complete commits and publishes board.json after finalization.** board.json is tracked, and complete's contract is a clean base level with origin. Rejected: leaving it dirty for the next command.
- **Kill evidence requires a finalized verify run that measured the survivor's package.** Rejected: "tests.json doesn't list the key" on its own. That would close survivors on a target that never measured them, the same false-close shape as feastahead's 527 closures.
- **Survivor items use only survivor evidence (accepted or killed), not absorption.** Absorption counts only once the target is complete, as c-6 says.
- **The complete-target refusal also covers `deferred add --target` (include-first).** route_to_complete is about routing, and add --target routes. validate's dangling check is not extended, because retro-flagging historical routes would turn validate red on existing repos.
- **Missing-card detection uses one State:"all" marker listing.** Rejected: per-phase label probes, which cost N phases plus their tasks in round trips. A closed card must count as present.
- **Cards created by reap --apply are not recorded as undoable closes.** --undo reopens closes, and reopening a card created at its terminal state would invent a state no record ever held.
- **survivor route mints an id on first route.** The board identity is then fixed before the first backlog sync, which c-9's "same id, same card" relies on.
- **Some dependencies only order shared files and do not consume output:** t-6→t-11 (survivor.go), t-6→t-12 (deferred.go), t-9→t-16 (reap.go), t-3→t-15 (ship.md).
- **The "phase complete gains no board coupling" check in TestShipPromptEmitsTerminalBoardStatuses is deleted.** It encoded the old terminal_emit_sites decision, which this phase's locked board_failure_posture supersedes.
