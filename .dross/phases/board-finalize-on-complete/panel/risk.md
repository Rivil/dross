# board-finalize-on-complete — risk-lens draft

Lens: every task owns a named way this phase can break, and its test_contract is
the test that catches that break. Where two failure modes would share a task,
the task is split so each one has exactly one owner.

```
Phase board-finalize-on-complete — 16 tasks across 4 waves

Wave 1
  t-1  Silence unknown identity-label lookups across providers
       files:    internal/forge/forge.go, internal/forge/identity_label_test.go,
                 internal/boardsync/reap_discover.go, internal/boardsync/identity_label_drift_test.go
       covers:   c-3
       depends:  —
       desc:     forge gains IdentityLabelPrefixes (dross/task:, dross/phase:, dross/deferred:, dross/target:)
                 and WarnDroppedLabels drops identity labels from its warning — one edit point reached by all
                 four ListIssues paths (forge.go:653, github.go:214, jira.go:206, youtrack.go:680).
                 reap_discover.go's identityLabels reads its prefixes from that list instead of restating them.
       contract: - against each of the github, forgejo, jira and youtrack httptest label indices,
                   ListIssues(Labels:[dross/task:p/t-1]) on a board that lacks the label returns no issues,
                   nil error, and writes nothing to stderr
                 - the same four with Labels:[bug] still print "does not know the label(s) bug"
                 - a mixed query [dross/phase:x, bug] warns naming bug and NOT dross/phase:x
                 - an unknown dross/status:done still warns (the dross/ prefix alone is not identity)
                 - drift test: every prefix boardsync stamps (PhaseLabel/TaskLabel/DeferredLabel/TargetLabel
                   and reap_discover's identityLabels) satisfies forge.IsIdentityLabel, and the forge list
                   holds no prefix boardsync never stamps — adding a fifth identity label to one side fails it

  t-2  Generalize project.toml patcher over any struct
       files:    internal/project/patch_diff.go, internal/project/patch_save.go,
                 internal/project/project.go, internal/project/patch_save_test.go
       covers:   c-4, c-5
       depends:  —
       desc:     toTree/diff take `any` instead of *Project; a new exported door (patch_save.go) does
                 read → decode → diff → apply → verifyPatched → atomic write for any struct, with the caller's
                 decode. project.Save becomes a caller of that door. The patcher stays in place — no file moves.
       contract: - the existing internal/project/save_lossless_test.go suite (TestSaveNoopIsByteIdentical,
                   TestSaveRefusesUnverifiedPatch, TestSaveFailureLeavesOriginalBytes, TestSavePreservesFileMode,
                   …) passes UNMODIFIED — any project.toml regression from the generalization fails it
                 - a non-Project fixture struct (root scalar, one [table], an [[item]] array with a multi-line
                   array and trailing comments) changing one scalar in item 2 changes exactly that one line
                 - a no-op save of the fixture performs no write (mtime unchanged)
                 - forcing a verify mismatch through the planOps seam leaves the fixture file byte-identical and
                   returns an error naming its path
                 - TestNoTestLost stays green (no recorded patch test is renamed, moved or copied)

  t-3  Add criterion absorbed-ids and validate them
       files:    internal/phase/phase.go, internal/cmd/validate.go, internal/cmd/validate_absorb_test.go
       covers:   c-7
       depends:  —
       desc:     Criterion gains `Deferred []string toml:"deferred,omitempty"`. `dross validate` checks every
                 absorbed id against deferred.Collect (every phase spec + .dross/deferred.toml): it must name an
                 entry whose Target is the spec's own phase.
       contract: - validate is green for a criterion listing the id of an item routed to that phase, whether the
                   item lives in another phase's spec or in the project store
                 - validate fails, naming spec path + criterion id + deferred id, when the id names no item,
                   an item routed to a different phase, an unrouted (someday) item, or a dismissed item
                 - LoadSpec of `deferred = ["abc"]` under [[criteria]] populates Criterion.Deferred
                 - a spec with no absorbed ids round-trips through Spec.Save without gaining a `deferred` key

  t-4  Refuse routing to a completed phase
       files:    internal/cmd/deferred_target.go, internal/cmd/deferred.go, internal/cmd/deferred_add.go,
                 internal/cmd/survivor.go, internal/cmd/route_complete_test.go
       covers:   c-8
       depends:  —
       desc:     One refuseCompleteTarget(root, slug) helper over changes.Complete() on the target's
                 changes.json, called by `deferred route`, `survivor route` and `deferred add --target` before
                 anything is loaded or written. NOT folded into validDeferredTarget, which validate also reads.
       contract: - `deferred route <src> <idx> --target done` (done's changes.json status=complete) errors naming
                   "done" and "complete", and the sha256 of every spec.toml + deferred.toml is unchanged
                 - `survivor route f.go:12 --op X --target done` refuses the same way, before ResolveAt runs
                   (a fixture with an unresolvable line still gets the complete-phase error, not a resolve error)
                 - `deferred add --target done` refuses with files unchanged
                 - a target whose record says shipped, and an unscaffolded roadmap slug, are both still accepted
                 - `dross validate` stays green on a repo that already holds an item routed to a complete phase
                   (the refusal must not turn feastahead's existing routes into validate failures)

  t-5  Build idempotent completion-gated phase board finalizer
       files:    internal/boardsync/finalize.go, internal/boardsync/finalize_test.go,
                 internal/boardsync/task.go, internal/boardsync/phase.go
       covers:   c-1
       depends:  —
       desc:     FinalizePhase(ctx, id): refuses unless the phase's changes.json is Complete(); ensures the phase
                 card; closes every task card at task-complete (plan tasks, plus any marker card carrying
                 dross/phase:<id> and a dross/task:<id>/ label), then the phase card at complete. A card already
                 at its terminal status label AND done is skipped with no write. Missing cards are created then
                 closed; milestone linking uses only board.json's cached id — never EnsureMilestoneLink.
                 Per-card failures are recorded and the rest continue.
       contract: - on the stateBoard double, a phase card + 3 task cards in task-in-review end with
                   dross/status:task-complete + closed on every task card and dross/status:complete + closed on
                   the phase card
                 - a second FinalizePhase on that board logs only Get/List calls (zero Create/Update/Close/
                   SetState) and leaves board.json byte-identical
                 - changes.json status=shipped → error before any board call (faultBoard.calls is empty)
                 - faultBoard.refuseClose on t-2's card → t-1, t-3 and the phase card still close; the error
                   names t-2
                 - plan.toml absent → the phase card closes and a task card found by its dross/phase:<id> label
                   closes too; a marker card with dross/phase:<id> but no dross/task: label is not touched
                 - missing phase card for a phase whose milestone has no cached board id → card created and
                   closed, faultBoard.milestones stays empty (catch-up must not mint an open historical epic)

  t-6  Name [board].enabled and non-zero failure in prompts
       files:    assets/prompts/execute.md, assets/prompts/plan.md, assets/prompts/quick.md,
                 assets/prompts/milestone.md, assets/prompts/verify.md, assets/prompts/ship.md,
                 internal/cmd/board_switch_prompt_test.go
       covers:   c-10
       depends:  —
       desc:     Replace every "(no-op unless `[remote].board_sync` is on — safe to always run)" and the
                 "same no-op rule" / "no-op unless board sync is on" variants (execute.md:57,62,396;
                 plan.md:252; quick.md:203,240; milestone.md:111; verify.md:91; ship.md:163,167) with: exits 0
                 silently when `[board].enabled` is false; a non-zero exit is a board failure to surface, not
                 the disabled no-op. r-01: `make install` before dogfooding the wording.
       contract: - board_switch_prompt_test fails if any assets/prompts/*.md contains "board_sync"
                 - it fails if any prompt carrying a `dross issue {phase,task,milestone,backlog} sync` or
                   `dross issue quick` call lacks "[board].enabled", or lacks the "non-zero exit" surfacing
                   sentence
                 - the existing TestInboxPromptReadsBoardEnabled stays green (inbox.md is not reworded)

Wave 2
  t-7  Match keyed array-of-tables elements by identity          (depends t-2)
       files:    internal/project/patch.go, internal/project/patch_diff.go,
                 internal/project/patch_save.go, internal/project/patch_keyed_test.go
       covers:   c-4
       depends:  t-2
       desc:     The door takes an identity map (array path → key field, e.g. task→id). Keyed arrays match
                 elements by that key, never by position: a reorder becomes a block move, an insertion at
                 index i an insert-before op, a removal a block delete. Comment lines directly above a
                 [[header]] (no blank line between) travel with that element. Unkeyed arrays keep today's
                 positional semantics.
       contract: - keyed reorder [t-1,t-2,t-3]→[t-1,t-3,t-2] relocates t-3's block whole; neither block has a
                   field rewritten in place (positional diff would rewrite both — the comment-mismatch bug)
                 - inserting a new element at index 1 lands it between t-1 and t-2, not at the tail, and
                   verifyPatched accepts it (an append would fail the canonical-order verify)
                 - a comment directly above t-3's header moves with t-3; one separated by a blank line stays put
                 - a duplicate identity value on either side → error, no write
                 - TestDiffArrayOfTablesEditInPlace (unkeyed stack.locked) stays green

  t-8  Write survivors.toml through the lossless patcher         (depends t-2)
       files:    internal/survivor/store.go, internal/survivor/store_lossless_test.go,
                 internal/cmd/survivor_marker_validate_test.go
       covers:   c-5
       depends:  t-2
       desc:     survivor.Save validates, then writes through the door (fresh encode only when the file does not
                 exist). Accept, Retire, category pruning and the drain all inherit it.
       contract: - cmd test: a tracked survivors.toml holding an entry whose text line ends
                   `# dross:allow-secret`; `dross survivor accept` adds an entry; that line is byte-identical and
                   `dross validate` reports no secret
                 - negative control in the same test: the identical store re-encoded with toml.NewEncoder makes
                   `dross validate` report a secret hit — proves the fixture trips the scanner and the file is
                   tracked, so the green above is not vacuous
                 - `survivor retire <k>` removes only k's block; every other entry (markers included) and an
                   unpruned category are byte-identical; a pruned category's block goes alone
                 - `survivor route` leaves survivors.toml byte-identical (it writes spec.toml only)
                 - corpus: the repo's own .dross/survivors.toml copied to a temp dir, one acceptance added →
                   the original bytes survive unchanged ahead of the new block

  t-9  Add deferred absorb verb for /dross-spec                  (depends t-3)
       files:    internal/cmd/deferred_absorb.go, internal/cmd/deferred.go,
                 internal/cmd/deferred_absorb_test.go, assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-7
       depends:  t-3
       desc:     `dross deferred absorb <source> <idx> --criterion <c-id> [--phase <id>]` resolves the item by its
                 `<source> <idx>` handle, stamps an id if it has none, and appends that id to the criterion's
                 `deferred` list in the (current) phase's spec. spec.md §5 tells /dross-spec to run it for every
                 criterion accepted from a §1 parked item, after spec.toml is written. No toml-tagged types in cmd.
       contract: - absorbing an item routed to the current phase writes its id into c-2's deferred list; a second
                   identical run changes no byte (no duplicate id, no write)
                 - an id-less legacy item gets an id stamped in its source spec, and that same id lands in the
                   criterion
                 - an item routed to another phase, an unknown criterion, or an out-of-range idx → refused with
                   every spec byte-identical
                 - spec_prompt_test fails if spec.md stops instructing `dross deferred absorb` for criteria seeded
                   from a parked item, or orders it before spec.toml is written

  t-10 Re-route an already-routed survivor in place              (depends t-4)
       files:    internal/cmd/survivor.go, internal/deferred/deferred.go, internal/cmd/survivor_reroute_test.go
       covers:   c-9
       depends:  t-4
       desc:     deferred.FindBySurvivor(root, key) searches every deferred source. `survivor route`: one existing
                 entry → rewrite its Target in its own source spec (id kept); same target → no write at all;
                 >1 entries → refuse naming each `<source> <idx>`; none → append as today. The t-4 refusal runs
                 first.
       contract: - key routed from phase A (entry id X in A's spec), re-routed from current phase B to T2 → A's
                   entry now targets T2 with id X; B's spec is byte-identical; exactly one entry carries the key
                 - re-route to the same target → every spec byte-identical and mtime unchanged
                 - backlog sync on the fakeBoard after the re-route creates no issue: the dross/deferred:X card
                   gains dross/target:T2 and loses the old target label
                 - two pre-existing entries carrying the key → refused naming both handles, files unchanged
                 - re-route to a complete target → the c-8 refusal, files unchanged

  t-11 Close routed mirrors only on disposition records          (depends t-3)
       files:    internal/boardsync/disposition.go, internal/boardsync/disposition_test.go,
                 internal/boardsync/backlog.go, internal/boardsync/reap.go
       covers:   c-6
       depends:  t-3
       desc:     One Disposed(root, entry) (bool, why) both paths call. Survivor: key accepted in survivors.toml,
                 OR the target's tests.json exists, is newer than the source phase's tests.json, its leg for the
                 language ran without error, its scope.files reaches the survivor's directory, and neither
                 Surviving nor OutOfScope carries the key. Deferred item: target changes.json Complete() AND a
                 target-spec criterion lists the item's id. Anything unreadable → not disposed. SyncBacklog
                 drops disposed items from the live push set; BacklogVerdictFor and reapBacklogVerdict/
                 routedVerdict resolve them; target completion alone still never does.
       contract: - routed item, target complete, no criterion lists its id → backlog sync closes nothing and reap
                   reports it unattributable (the 47a5c93 guard stays)
                 - target complete + target criterion lists the id → one close in backlog sync; reap lists it
                   stranded with a why naming phases/<target>/spec.toml and the criterion id
                 - criterion lists the id but target is only shipped → open; criterion listing it sits in a
                   complete phase that is NOT the item's target → open
                 - routed survivor accepted in survivors.toml → closes in both paths, why names survivors.toml
                 - routed survivor: target tests.json missing → open; present + scope reaches the dir + key absent
                   → closes; key in OutOfScope → open; scope never touched the dir → open; leg error → open;
                   target run older than the source run → open; deferred text that does not parse to a file → open
                 - after a disposition close, a second backlog sync makes zero Update/Close calls for that card
                 - an orphan card found only by its dross/deferred: label reaches the same verdict as a linked one

  t-12 Finalize the board at end of phase complete               (depends t-5)
       files:    internal/cmd/phase.go, internal/cmd/phase_finalize.go, internal/cmd/issue.go,
                 internal/cmd/phase_complete_board_test.go, internal/cmd/testdata/cli_surface/issue.txt
       covers:   c-1
       depends:  t-5
       desc:     phaseComplete calls finalizeBoard(id) after the remote-branch delete. finalizeBoard: openBoard
                 (disabled → return nil, no client built) → FinalizePhase → autoCommitDrossDirt + routeBaseChores
                 for the board.json it wrote. Any failure (openBoard error included) leaves the completion intact,
                 exits non-zero, and names `dross issue phase finalize <id>`. That verb is the same finalizeBoard,
                 refusing unless HEAD is on the phase's recorded base. issue.txt golden re-minted.
       contract: - git fixture + squash-merged phase + youtrack httptest board: after `dross phase complete` every
                   task card and the phase card read closed at task-complete / complete server-side, the tree is
                   clean, and local base == origin/base (board.json committed and published, not left ahead)
                 - board fake returning 500 on close → exit non-zero naming `dross issue phase finalize <id>`,
                   yet state.json carries "completed <id>", changes.json status=complete, and phase/<id> is gone
                   locally and on origin
                 - [board].enabled=false with base_url pointing at a counting server → zero requests, exit 0, no
                   board line in output
                 - re-running complete after success → zero non-GET board requests, HEAD unchanged (no chore
                   commit), board.json byte-identical
                 - [board].auth_env names an unset variable → complete exits non-zero naming the finalize verb with
                   the completion record already written (openBoard runs after teardown, not before)
                 - TestCLISurfacePinned fails if `issue phase finalize` is missing from issue.txt

  t-13 Reap creates missing cards for completed phases          (depends t-1, t-5)
       files:    internal/boardsync/reap_catchup.go, internal/boardsync/reap_catchup_test.go,
                 internal/boardsync/reap_apply.go, internal/cmd/issue_reap_cmd.go, internal/cmd/issue_reap_cmd_test.go
       covers:   c-11
       depends:  t-1, t-5
       desc:     Inventory also walks phase dirs whose changes.json is Complete(): one State "all"
                 dross/phase:<id> query per phase (phase and task cards both carry it) decides which phase/task
                 cards are missing. Dry-run prints a "Missing" section and writes nothing. --apply calls
                 FinalizePhase per phase, record-and-continue. Honors --namespace (Phases/Tasks). Creations are
                 not journaled as closes.
       contract: - dry run with a complete phase the board has never heard of → listed with its N tasks; zero
                   non-GET requests; board.json byte-identical; no "does not know the label" line on stderr
                 - a complete phase whose card exists but is CLOSED → not listed (an open-only lookup would
                   report it missing and --apply would duplicate it)
                 - a shipped-but-not-complete phase without a card → not listed
                 - --apply → phase card at complete and each task card at task-complete, all closed, with the
                   labels FinalizePhase writes
                 - one phase's create failing → the other phases are still created; exit non-zero naming it
                 - --namespace Backlog → no catch-up lookup is made
                 - `reap --undo` after a catch-up apply changes no created card
                 - a complete phase with no spec.toml → listed as cannot-create with the reason, skipped by --apply

Wave 3
  t-14 Write plan.toml through the keyed lossless patcher         (depends t-7)
       files:    internal/phase/phase.go, internal/phase/plan_lossless_test.go, internal/cmd/task_lossless_test.go
       covers:   c-4
       depends:  t-7
       desc:     Plan.Save goes through the door with identity task→id (fresh encode only for a new file);
                 Spec.Save keeps saveTOML. task status/add/edit/move/remove, `issue task pull` and `phase migrate`
                 all inherit it via the one Plan.Save choke point.
       contract: - on a plan with a header comment, a comment above t-3 and a trailing comment in t-2:
                   `task status t-2 done` changes exactly t-2's status line
                 - `task add --after t-1` inserts the new block between t-1 and t-2; outside it only task_seq
                   changes
                 - `task edit t-3 --title X` changes only t-3's title line
                 - `task move t-4 --before t-2` relocates t-4's block with its leading comment and rewrites only the
                   wave lines of reflowed pending dependents; t-2 and t-3 bytes unchanged
                 - `task remove t-3 --force` deletes only t-3's block and the "t-3" entries in dependents'
                   depends_on lines
                 - corpus: every .dross/phases/*/plan.toml in the repo, copied to a temp dir, with its first task's
                   status flipped → the diff is that one line (or one inserted key) and LoadPlan matches
                 - a forced verify mismatch → plan.toml byte-identical and `task status` errors naming plan.toml

  t-15 Drop ship §6 close steps; repoint lifecycle guard          (depends t-5, t-6, t-12)
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-2
       depends:  t-5, t-6, t-12
       desc:     Remove §6 steps 4 and 5; step 3 says complete closes the task and phase cards; Recovery gains a
                 board-finalize failure entry naming `dross issue phase finalize <phase-id>`. emittedStatuses
                 gains a third source — FinalizePhase's terminal status arguments parsed from finalize.go — and
                 mirrorLanes' Phases/Tasks rows point at it. The two recorded ship tests keep their names and get
                 inverted bodies (tests_before.txt is not edited). r-01: `make install` before dogfooding.
       contract: - TestShipPromptClosesTaskCardsAfterPhaseComplete (name kept) fails if ship.md contains
                   `dross issue task sync <phase-id> --status task-complete --close`
                 - TestShipPromptEmitsTerminalBoardStatuses (name kept) fails if ship.md contains
                   `dross issue phase sync <phase-id> --status complete --close`, if `--status shipped` no longer
                   precedes `dross phase complete <phase-id>`, or if Recovery stops naming
                   `dross issue phase finalize`
                 - deleting the task-complete argument from finalize.go fails TestEmittedStatusesAreTheLifecycleSet
                   (complete/task-complete stay emitted only through the Go source, never via ctx.go constants)
                 - TestEveryMirrorLaneHasATerminalEmission still fails for a board namespace with no terminal
                 - TestNoTestLost stays green

Wave 4
  t-16 Sync README with finalize, absorb, catch-up                (depends t-9, t-12, t-13, t-15)
       files:    README.md, internal/cmd/readme_board_finalize_test.go
       covers:   —
       depends:  t-9, t-12, t-13, t-15
       desc:     Issue verb list gains `phase finalize`; reap paragraph gains the catch-up; `dross phase complete`
                 documented as finishing the board; `dross deferred absorb` listed; footprint token rows for
                 ship.md, spec.md and the t-6 prompts refreshed.
       contract: - TestEveryDocumentedIssueVerbResolves stays green with `phase finalize` in README's issue list
                 - readme_board_finalize_test fails if README omits `dross deferred absorb` or stops saying
                   `dross phase complete` closes the board cards
```

## Coverage

| Criterion | Tasks | Risk each task owns |
|---|---|---|
| c-1 | t-5, t-12 | t-5: finalizer idempotency, partial failure, completion gate, no epic minting · t-12: ordering after teardown, board-off silence, board.json left dirty/ahead, rerun no-op |
| c-2 | t-15 | removed steps re-appearing; lifecycle guard going red or vacuous once the prompt lines leave |
| c-3 | t-1 | per-provider suppression, mixed-query over-suppression, identity list drift |
| c-4 | t-2, t-7, t-14 | t-2: project.toml regression · t-7: positional rewrite on move/insert · t-14: every plan writer + real-plan corpus |
| c-5 | t-2, t-8 | t-8: marker stripping + a vacuous validate green |
| c-6 | t-11 | false close on completion alone, on a stale/unscoped run, or on a criterion in the wrong phase |
| c-7 | t-3, t-9 | t-3: schema + validate refusal · t-9: /dross-spec writes ids without hand-copying them |
| c-8 | t-4 | file writes before refusal; validate turned red on historical routes |
| c-9 | t-10 | new entry minted instead of updated; cross-phase entry; duplicates; same-target rewrite |
| c-10 | t-6 | stale wording in any prompt; a non-zero exit read as the disabled no-op |
| c-11 | t-13 | dry-run writes, duplicate creation from open-only lookups, one failure stranding the rest |

11/11 criteria covered. t-16 covers none by design (see judgment calls).

## Judgment calls

- **Patcher generalized in place (internal/project), not extracted to a new package.** Rejected internal/tomlpatch: it moves 2,767 lines and ~70 tests recorded in tests_before.txt, and the Project-specific differ tests would need an import cycle to follow. The decision asks for "generalized", not a new home. phase/survivor → project creates no cycle because project imports nothing internal.
- **Keyed identity split from the generalization (t-7 vs t-2).** The diff's positional array matching would make `task move` rewrite every shifted task and leave comments sitting above the wrong task, and `task add --after` would fail verify. That risk gets its own task, separate from the project.toml-regression risk.
- **survivors.toml does not wait for keyed identity (t-8 depends on t-2 only).** Accept only appends and retire/prune only delete, and keys are unique, so deep-equal matching is exact. Making it wait would serialize for no failure mode.
- **Absorbed ids are written by a new verb (`deferred absorb <source> <idx>`), not by exposing ids in `deferred list --json`.** Exposing ids would break the locked deferred_identity decision (deferred-add-command: verbs address by `<phase> <idx>`, ids stay internal) and the deferred_list_json output golden, and it would make the agent hand-copy hex ids. The verb satisfies both locked decisions and stamps ids on legacy entries.
- **The finalizer gates itself on changes.json Complete().** The alternative was gating in each caller. With the gate in FinalizePhase, `issue phase finalize` on an in-flight phase, reap catch-up and complete itself share one check that cannot close live cards early.
- **Idempotency is gated on terminal status label + done, not a full body/label diff.** Providers normalize bodies, so a body diff would write on every rerun and break "re-running changes nothing". The cost: a card already terminal keeps a stale body.
- **The finalizer never calls EnsureMilestoneLink and links only through board.json's cached id.** Catch-up over old phases would otherwise mint open epics for milestones closed long ago.
- **complete commits and publishes the board.json the finalizer writes** (autoCommitDrossDirt + routeBaseChores) instead of leaving it dirty. The rejected alternative was leaving it for a prompt to commit, which recreates the unpushed-base-chore divergence. The finalize verb shares that tail and refuses off the recorded base.
- **The c-8 refusal stays out of validDeferredTarget and is also applied to `deferred add --target`.** validate reads validDeferredTarget, so putting it there would fail every historical route to a finished phase. `add --target` is the third stamping path the route_to_complete decision describes.
- **Duplicate entries for one survivor key are refused, not merged.** Updating "the first" would leave the other entry's card live with a stale target, which is the duplicate-card failure c-9 exists to stop.
- **Survivor absence counts as disposition only when it is fail-closed:** the run is newer than the source phase's run, the leg ran without error, and the scope reaches the survivor's directory. Diff-scoped mutation runs never mutate untouched packages, so plain absence would repeat the 527-card false close in a new form.
- **Catch-up detects with one State "all" dross/phase:<id> query per phase, not a bulk marker listing.** GitHub and Forgejo ListIssues fetch a single 50-item page with no pagination, so a bulk listing silently truncates and "truncated" reads as "missing". Creation still goes through the finalizer's own per-card resolution, which is the second guard against duplicates.
- **c-10 is one 7-file task.** It is a single wording sweep with one global test. Splitting it would leave that test red between halves. ship.md lines 163/167 are reworded here (wave 1), and t-15 edits §6 afterwards, so the two never touch ship.md concurrently.
- **Recorded test names are kept and their bodies inverted (t-15).** Deleting TestShipPromptClosesTaskCardsAfterPhaseComplete / TestShipPromptEmitsTerminalBoardStatuses fails TestNoTestLost. Editing tests_before.txt would weaken the ratchet.
- **README gets a single owner at the end (t-16, covers nothing).** Folding README edits into t-9/t-12/t-13/t-15 would have three wave-2 tasks editing one file concurrently. README sync is a standing repo rule, so it gets a task even though it delivers no criterion.
- **Residual risk accepted:** retiring an acceptance after its routed mirror closed leaves that card closed while the item is live again. The identity lookup prevents a duplicate, but nothing reopens the card, and c-6 does not ask for it.
