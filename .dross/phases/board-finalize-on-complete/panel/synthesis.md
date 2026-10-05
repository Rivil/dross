# board-finalize-on-complete — cold-judge synthesis

Inputs: risk.md (16 tasks / 4 waves), mvp.md (9 / 2), verification.md (16 / 3).
Every hazard claim below was checked against the repo read-only; file:line evidence is cited where it decided a call.

## Scores

Scale 1–5.

| Draft | Dimension | Score | One line |
|---|---|---|---|
| risk | criteria coverage | 5 | 11/11 covered, plus a README owner task (covers none, by design). |
| risk | test-contract specificity | 4 | Named doubles, negative controls, real-file corpus tests, catch-up truncation and epic-minting guards. Misses the patcher pin/anchor tests, TestSaveTOMLAtomicFailurePreservesFile, and TestCloseEmissionsCarryAValidStatus. |
| risk | granularity | 5 | One failure mode per task. The 7-file c-10 sweep is justified, because one global test would be red between halves. |
| risk | wave correctness | 4 | Dependencies are sound and the ship.md double edit is sequenced. But t-11 (reap.go) and t-13 (catch-up, which needs `ReapPlan` in reap.go:58) share wave 2, and t-2 omits project_writer_pin_test.go and ARCHITECTURE.md, which its door will break. |
| mvp | criteria coverage | 3 | Nominally 11/11. c-6's survivor evidence (key absent from tests.json) is the unsafe shape the lead flagged. c-1's re-run is weakened to "same outcome", which still writes. |
| mvp | test-contract specificity | 2 | Thin contracts. Misses 4 of the 9 flagged hazards: board.json dirt, bulk-listing truncation, survivor-absence evidence, the atomic-save test. No closed-card-counts-as-present case for catch-up. |
| mvp | granularity | 2 | Merges extraction with keyed identity, c-8 with c-9, the c-6 predicate with the c-7 field, and c-2 with c-10. Each merged task has two independent ways to fail under one owner. |
| mvp | wave correctness | 3 | Ordering is correct: prompts come after the finalizer, and the reap.go editors are in different waves. But two waves pack every Go change into wave 2. |
| verification | criteria coverage | 5 | 11/11. Splits c-6 into a pure predicate and the wiring. |
| verification | test-contract specificity | 5 | The strongest guard-test awareness: raw prompt read (promptContent strips `_`), anti-vacuity floors, all four divergence tests, TestSaveTOMLAtomicFailurePreservesFile re-pointed, encoder pin and anchors, a BacklogVerdictFor⇔reap agreement test. |
| verification | granularity | 5 | Fine-grained, and every shared-file dependency is stated as one. |
| verification | wave correctness | 4 | Shared-file ordering is explicit and correct. Survivors needlessly wait on the keyed diff (wave 3). Catch-up detection relies on a bulk marker listing that truncates at 50, which is a design hazard rather than a wave error. |

**Hazard carry** (✓ handled, ✗ missed, ~ partial). The merged plan carries every row.

| Hazard (verified) | risk | mvp | verif | merged |
|---|---|---|---|---|
| ship §6 removal turns the 4 divergence tests red (`checked == 0` Fatal at board_lifecycle_divergence_test.go:692 once only milestone.md/quick.md bare `--close` remain) | ~ (names 2 of 4) | ~ (names 1) | ✓ | t-16 |
| TestNoTestLost: keep the two ship-test names | ✓ | ✓ | ✗ ("replaced") | t-16 |
| issue.txt golden for `issue phase finalize` | ✓ | ✓ | ✓ | t-12 |
| Patcher pins: `only Save may call writeAtomic` and the encodeFresh allowlist (project_writer_pin_test.go:45, :277), ARCHITECTURE anchors, `<path>.tmp` atomic test (phase_test.go:544) | ✗ | ~ | ✓ | t-2, t-14 |
| Keyed identity + insert-at-index (patch_diff.go:269 matches by position or deep-equality) | ✓ | ✓ | ✓ | t-7 |
| c-7 ids hidden (`json:"-"`, deferred.go:39) → `deferred absorb` mints | ✓ | ✓ | ✓ | t-9 |
| board.json is tracked; complete must commit and route it (phase.go:728/736 pattern) | ✓ | ✗ (rejects) | ✓ | t-12 |
| ListIssues takes a single 50-item page (forge.go:712, github.go:228) → per-phase lookups | ✓ | ✗ | ✗ | t-13 |
| Survivor absence from tests.json is not disposal | ✓ | ✗ | ✓ | t-11 |
| *(extra)* SyncPhase calls EnsureMilestoneLink and always UpdateIssue (boardsync/phase.go:93, :126) → catch-up mints open epics and re-runs write | ✓ | ✗ (composes it) | ~ | t-5 |
| *(extra)* reap discovery lists the marker in State "open" only (reap_discover.go:100) → a closed card would read as missing | ✓ | ✗ | ✓ | t-13 |

**Skeleton: risk.** It ties verification on structure. It alone carries the two hazards whose failure shows up on a live board rather than in `dross test`: truncated bulk listings minting duplicate cards, and catch-up minting open historical epics. It also keeps the recorded test names. Verification's unique strengths are guard-test contracts, and those graft cleanly onto risk's tasks. Two structural grafts come from verification: the c-6 predicate/wiring split, which removes risk's wave-2 reap.go collision, and its pin, anchor and atomic-test contracts.

## Merged plan

```
Phase board-finalize-on-complete — 17 tasks across 4 waves

Wave 1
  t-1  Silence unknown identity-label lookups across providers      [risk+verification+mvp]
       files:    internal/forge/forge.go, internal/forge/identity_label_test.go,
                 internal/forge/forge_test.go, internal/forge/youtrack_test.go,
                 internal/boardsync/reap_discover.go, internal/boardsync/identity_label_test.go
       covers:   c-3
       depends:  —
       desc:     forge gains IsIdentityLabel over one prefix list (dross/task:, dross/phase:, dross/deferred:,
                 dross/target:). WarnDroppedLabels (forge.go:344, shared by all four ListIssues) leaves identity
                 labels out of its warning but still drops them, so the lookup resolves to no card.
                 reap_discover.go's identityLabels takes its prefixes from that list.
       contract: - against the github, forgejo, jira and youtrack httptest label indices, ListIssues(Labels:
                   [dross/task:p/t-1]) on a board lacking the label returns no issues and a nil error, makes zero
                   issue-list requests, and writes nothing to stderr
                 - the same four with Labels:[bug] still print "does not know the label(s) bug"
                 - a mixed query [dross/phase:x, bug] warns naming bug and NOT dross/phase:x
                 - unknown dross/status:done, dross/quick and the bare `dross` marker still warn (the dross/ prefix
                   alone is not identity)
                 - TestRESTListIssuesDropsUnknownLabels and its YouTrack twin gain an identity row
                   (dross/phase:never-synced → zero issues, empty stderr). The existing `typo` rows still warn
                 - ResolvePhaseIssue over a YouTrack fake whose tag list lacks dross/phase:new returns "" with empty
                   captureStderr output
                 - drift: every reap_discover identityLabels prefix and every PhaseLabel/TaskLabel/DeferredLabel/
                   TargetLabel output satisfies forge.IsIdentityLabel. StatusLabel/LabelQuick/LabelMarker do not.
                   The forge list holds no prefix boardsync never stamps. A fifth dross/<kind>: constructor
                   registered on one side only fails

  t-2  Generalize the project.toml patcher in place over any struct  [risk; pins/anchors from verification]
       files:    internal/project/patch_diff.go, internal/project/patch_save.go, internal/project/project.go,
                 internal/project/patch_save_test.go, internal/cmd/project_writer_pin_test.go, ARCHITECTURE.md
       covers:   c-4, c-5
       depends:  —
       desc:     toTree/diff take `any` instead of *Project. A new exported door (patch_save.go) does read →
                 caller-supplied decode → diff → apply → verifyPatched → atomic write, with a fresh encode only for
                 an absent path. project.Save becomes a caller. No file moves. The writer pins are widened to admit
                 the door and nothing else, and ARCHITECTURE.md's Save/apply/diff/encodeFresh anchors are
                 re-pointed at their new line numbers.
       contract: - internal/project/save_lossless_test.go (TestSaveNoopIsByteIdentical, TestSaveRefusesUnverifiedPatch,
                   TestSaveFailureLeavesOriginalBytes, TestSavePreservesFileMode, …) and cmd's
                   TestLosslessRealProjectTomlOneLineDiff pass UNMODIFIED
                 - a non-Project fixture struct (root scalar, one [table], an [[item]] array with a multi-line
                   array and trailing comments) changing one scalar in item 2 changes exactly that one line
                 - a no-op save of the fixture performs no write (mtime unchanged)
                 - a verify mismatch forced through the planOps seam leaves the fixture byte-identical and returns
                   an error naming its path
                 - TestEncodeFreshIsTheOnlyEncoder still reports an injected toml.Marshal fallback and a second
                   NewEncoder. Its writeAtomic/encodeFresh allowlists name the door and nothing else, so a stray
                   caller still fails by name
                 - TestArchitectureNamesLosslessWriter resolves every Save/apply/diff/encodeFresh anchor after the
                   edit. A stale line number fails it
                 - TestNoTestLost stays green (no recorded patch test is renamed, moved or copied)

  t-3  Record absorbed deferred ids on criteria and validate them    [risk+verification]
       files:    internal/phase/phase.go, internal/cmd/validate.go, internal/cmd/validate_absorb_test.go
       covers:   c-7
       depends:  —
       desc:     Criterion gains `Deferred []string` (toml/json "deferred,omitempty"). `dross validate` checks every
                 absorbed id against deferred.Collect (every phase spec plus .dross/deferred.toml). The id must name
                 an entry whose Target is the spec's own phase.
       contract: - validate is green for a criterion listing the id of an item routed to that phase, whether the item
                   lives in another phase's spec or in .dross/deferred.toml
                 - validate fails with a ✗ line naming the spec path, the criterion id and the deferred id when the id
                   names no item, an item routed to a different phase (naming that phase), an unrouted (someday) item,
                   or a dismissed item
                 - LoadSpec of `deferred = ["abc"]` under [[criteria]] populates Criterion.Deferred
                 - a spec with no absorbed ids round-trips through Spec.Save without gaining a `deferred` key, and
                   json_tag_parity_test stays green

  t-4  Refuse routing to a completed phase                           [risk+verification]
       files:    internal/cmd/deferred_target.go, internal/cmd/deferred.go, internal/cmd/deferred_add.go,
                 internal/cmd/survivor.go, internal/cmd/route_complete_test.go
       covers:   c-8
       depends:  —
       desc:     One refuseCompleteTarget(root, slug) helper over changes.Complete(). `deferred route`,
                 `survivor route` and `deferred add --target` call it right after validDeferredTarget, before any
                 read-modify-write. It is NOT folded into validDeferredTarget, which validate also reads.
       contract: - `deferred route <src> <idx> --target done` (done's changes.json status=complete) errors naming
                   "done" and "complete", and a hash of the whole .dross tree is unchanged
                 - `survivor route f.go:12 --op X --target done` refuses before ResolveAt runs (a fixture with an
                   unresolvable line still gets the complete-phase error), and the current spec.toml and
                   survivors.toml are byte-identical
                 - `deferred add --target done` refuses with no spec or deferred.toml write and zero board requests
                 - control: all three succeed for a target at status=shipped and for an unscaffolded roadmap slug
                 - `dross validate` stays green on a repo that already holds an item routed to a complete phase

  t-5  Build idempotent completion-gated phase board finalizer       [risk+verification]
       files:    internal/boardsync/finalize.go, internal/boardsync/finalize_test.go,
                 internal/boardsync/task.go, internal/boardsync/phase.go, internal/boardsync/ctx.go
       covers:   c-1
       depends:  —
       desc:     FinalizePhase(ctx, id) refuses unless the phase's changes.json is Complete(). It resolves or
                 creates the phase card and every task card: plan tasks, plus any card carrying dross/phase:<id> and
                 a dross/task:<id>/ label. Task cards close at task-complete, then the phase card at complete. A card
                 already at its terminal label AND done gets no write. Milestone linking uses only board.json's
                 cached id, never EnsureMilestoneLink. Per-card failures are recorded and the rest continue. Adds
                 the StatusComplete constant to ctx.go.
       contract: - stateBoard/faultBoard with a phase card at shipped and 3 task cards at task-in-review: afterwards
                   every task card carries dross/status:task-complete and reads closed, and the phase card carries
                   dross/status:complete and reads closed
                 - no cards at all: the phase card and every task card are created then closed (close-on-create),
                   and board.json gains the phase link and three task links
                 - a second FinalizePhase on that board logs only Get/List calls (zero Create/Update/Close/SetState)
                   and leaves board.json byte-identical
                 - a card that reads done but lacks its terminal status label is relabelled (the skip needs both)
                 - changes.json status=shipped → an error naming "shipped" before any board call
                   (faultBoard.calls is empty)
                 - faultBoard.refuseClose on t-2's card → t-1, t-3 and the phase card still close, and the error
                   names t-2 (TaskCloseError aggregation is not short-circuited)
                 - plan.toml absent → the phase card closes, and a task card found by its dross/phase:<id> +
                   dross/task: labels closes too. A marker card with dross/phase:<id> but no dross/task: label is not
                   touched
                 - a missing phase card for a phase whose milestone has no cached board id → card created and
                   closed, and faultBoard.milestones stays empty
                 - issue_test's ctx.go-constant membership check covers StatusComplete

  t-6  Name [board].enabled and non-zero failure in prompts          [risk+verification]
       files:    assets/prompts/execute.md, assets/prompts/plan.md, assets/prompts/quick.md,
                 assets/prompts/milestone.md, assets/prompts/verify.md, assets/prompts/ship.md,
                 internal/cmd/board_switch_prompt_test.go
       covers:   c-10
       depends:  —
       desc:     Replace every "no-op unless `[remote].board_sync` is on", "same no-op rule" and "no-op when board
                 sync is off" variant: execute.md:57,62,116,345,396; plan.md:252; quick.md:203,240;
                 milestone.md:111,164; verify.md:91; ship.md:163,167. The new wording says the call exits 0 silently
                 when `[board].enabled` is false, and a non-zero exit is a board failure to surface, not the
                 disabled no-op. ship.md:165–166 are left for t-16. r-01: `make install` before dogfooding the
                 wording.
       contract: - the test reads assets/prompts/*.md RAW (promptContent strips `_`, so a normalised scan misses it)
                   and fails on any `board_sync`. Reverting execute.md:57 alone fails it
                 - any prompt carrying a `dross issue {phase sync|task sync|milestone sync|backlog sync|quick}` call
                   that lacks "[board].enabled" or the non-zero-exit surfacing sentence fails, naming the file.
                   Dropping the sentence from verify.md fails
                 - anti-vacuity: fails if fewer than 6 prompts with a `dross issue` call are found
                 - TestInboxPromptReadsBoardEnabled stays green (inbox.md is not reworded)

Wave 2
  t-7  Match keyed array-of-tables elements by identity              [risk+verification]
       files:    internal/project/patch.go, internal/project/patch_diff.go, internal/project/patch_save.go,
                 internal/project/patch_keyed_test.go
       covers:   c-4
       depends:  t-2
       desc:     The door takes an identity map (array path → key field, e.g. task→id). Keyed arrays match by key,
                 never by position: a matched element gets per-field ops, a reorder becomes a block move, an insertion
                 at index i an insert-before op, and a removal a block delete. A comment run directly above a
                 [[header]] (no blank line between) travels with that element. Unkeyed arrays keep today's
                 positional/deep-equal semantics.
       contract: - keyed reorder [t-1,t-2,t-3]→[t-1,t-3,t-2] relocates t-3's block whole. No field is rewritten in
                   place, and the t-1 and t-2 blocks remain byte-identical substrings
                 - inserting a new element at index 1 lands it between t-1 and t-2, not at EOF, and verifyPatched
                   accepts it. Cutting the inserted block back out returns the original bytes exactly
                 - a comment directly above t-3's header moves with t-3, and one separated by a blank line stays put.
                   Deleting t-2 keeps `# about t-3` and removes t-2's own body comment
                 - changing t-4.status while removing t-2 in one diff touches exactly two regions: t-4's status line
                   and t-2's block
                 - a duplicate identity value on either side → error, no write
                 - a keyed patch that would not decode to the saved value is refused and the file is untouched
                 - TestDiffArrayOfTablesEditInPlace (unkeyed stack.locked) and the project lossless suite stay green

  t-8  Write survivors.toml through the lossless patcher             [risk; contracts from verification]
       files:    internal/survivor/store.go, internal/survivor/store_lossless_test.go,
                 internal/cmd/survivor_lossless_test.go
       covers:   c-5
       depends:  t-2
       desc:     survivor.Save validates, then writes through the door, with a fresh encode only when the file does
                 not exist. Accept, Retire and category pruning inherit it. Matching stays positional/deep-equal:
                 Add replaces an existing key in place (store.go:167), new keys append, retire deletes, and keys are
                 unique.
       contract: - cmd test: a COMMITTED survivors.toml (the secret self-scan reads tracked files only) holding an
                   entry with a GitHub-token-shaped text whose line ends `# dross:allow-secret`. `dross survivor
                   accept` adds an entry, every original line stays byte-identical, and `dross validate` exits 0
                 - negative control in the same test: the identical store re-encoded with toml.NewEncoder makes
                   `dross validate` report a secret, proving the fixture trips the scanner and the green is not
                   vacuous
                 - `survivor retire` of the middle of three entries leaves the other two blocks (markers included)
                   and an unpruned category byte-identical. A pruned category's block goes alone
                 - accept with a new category inserts its [[category]] block after the last existing category, and
                   the accepted blocks are untouched
                 - `survivor route` leaves survivors.toml byte-identical (it writes spec.toml only)
                 - corpus: the repo's own .dross/survivors.toml copied to a temp dir, one acceptance added → the
                   original bytes survive unchanged ahead of the new block

  t-9  Add `dross deferred absorb` for /dross-spec                   [risk+mvp+verification]
       files:    internal/cmd/deferred_absorb.go, internal/cmd/deferred.go, internal/cmd/deferred_absorb_test.go,
                 assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-7
       depends:  t-3, t-4
       desc:     `dross deferred absorb <source> <idx> --criterion <c-id> [--phase <id>]` (default: the current
                 phase) resolves the item by its `<source> <idx>` handle and requires its target to be that phase.
                 It stamps an id if the item has none and appends the id once to the criterion's `deferred` list.
                 spec.md tells /dross-spec to run it, after §5 writes spec.toml, for every criterion accepted from a
                 §1 parked item. No toml-tagged types in cmd. (t-4 ordering: shared deferred.go.)
       contract: - absorbing an item routed to the current phase writes its stable id (never `<source> <idx>`) into
                   the criterion's `deferred` list, and `dross validate` then passes. A second identical run changes
                   no byte (no duplicate id, no write)
                 - an id-less legacy item gets an id stamped in its source spec, and that same id lands on the
                   criterion
                 - an item routed to another phase, or unrouted, is refused naming its target. An unknown criterion
                   is refused naming it. An out-of-range idx is refused. Every spec stays byte-identical
                 - TestDeferredEntryIDStaysInternal is unchanged: `deferred list --json` still has no `id` key
                 - spec_prompt_test fails if spec.md stops instructing `dross deferred absorb` for criteria seeded
                   from a parked item, or places it before the `## 5. Write spec.toml` heading

  t-10 Re-route an already-routed survivor in place                  [risk+verification+mvp]
       files:    internal/cmd/survivor.go, internal/deferred/deferred.go, internal/cmd/survivor_route_test.go
       covers:   c-9
       depends:  t-4
       desc:     deferred.FindBySurvivor(root, key) searches every deferred source. If exactly one undismissed entry
                 carries the key, `survivor route` rewrites its Target in its own source spec (id kept). Same
                 target → no write at all. More than one → refuse, naming each `<source> <idx>`. None → append, now
                 minting an id at filing as `deferred add` does (deferred_add.go:60). The t-4 refusal runs first.
       contract: - a key routed from phase A (entry id X in A's spec), re-routed from current phase B to T2 → A's
                   entry now targets T2 with id X, B's spec is byte-identical, and exactly one entry across all
                   sources carries the key
                 - route K→alpha, then K→beta: exactly one [[deferred]] carries survivor=K, with the id minted on the
                   first route unchanged and target=beta
                 - re-routing to the same target → every spec byte-identical, mtime unchanged, and stdout says it is
                   already routed
                 - backlog sync after the re-route (TestReroutedDeferredSwapsItsTargetLabel harness) creates no issue:
                   the same dross/deferred:X card gains dross/target:T2 and loses the old target label
                 - two pre-existing entries carrying the key → refused naming both handles, files unchanged
                 - re-routing to a complete target → the c-8 refusal before the lookup, and the entry keeps its old
                   target

  t-11 Disposition-record predicate for routed items                 [verification structure; risk+verification evidence]
       files:    internal/boardsync/disposition.go, internal/boardsync/disposition_test.go
       covers:   c-6
       depends:  t-3
       desc:     Disposed(root, entry) (bool, why) reads disk only. A routed survivor is disposed when survivors.toml
                 accepts its key, or when the target's tests.json run is finalized, newer than the source phase's
                 run, ran its language leg without error, has scope.Files containing the survivor's file, and lists
                 the key in neither Surviving nor OutOfScope. A plain item is disposed when the target's changes.json
                 is Complete() AND a target-spec criterion lists the item's id. Anything unreadable → not disposed.
       contract: - survivor key accepted in survivors.toml → disposed, and why names survivors.toml
                 - routed survivor: target tests.json missing → not. A finalized run that is newer than the source
                   run and error-free, whose scope contains the file and whose key is absent → disposed. Key in
                   OutOfScope → not. Scope never touched the file → not. Leg error → not. Target run older than the
                   source run → not. verify.toml not finalized → not. Deferred text that does not parse to a file →
                   not
                 - plain item: target complete + target criterion deferred=[id] → disposed, and why names
                   phases/<target>/spec.toml and the criterion id. Target complete with no absorbing criterion → not
                   (the 47a5c93 rule). Absorbing criterion but target only shipped → not. A criterion listing the id
                   in a complete phase that is NOT the item's target → not. Item with an empty id → not
                 - the signature takes (root, entry) and no board client, so a verdict derived from card state cannot
                   compile

  t-12 Finalize the board at the end of `dross phase complete`       [risk+verification]
       files:    internal/cmd/phase.go, internal/cmd/phase_finalize.go, internal/cmd/issue.go,
                 internal/cmd/phase_complete_board_test.go, internal/cmd/testdata/cli_surface/issue.txt
       covers:   c-1
       depends:  t-5
       desc:     phaseComplete calls finalizeBoard(id) after the remote-branch delete. finalizeBoard runs openBoard
                 (disabled → return nil, no client built), then FinalizePhase, then autoCommitDrossDirt +
                 routeBaseChores for the board.json it wrote. Any failure, openBoard's included, leaves the completion
                 intact, exits non-zero, and names `dross issue phase finalize <id>`. That verb is the same
                 finalizeBoard, refusing unless HEAD is on the phase's recorded base. The issue.txt golden is
                 re-minted.
       contract: - git fixture (shipFixture + simulateSquashMerge) + a board fake (youtrack httptest or taskCloseFake
                   as forgejo), [board].enabled=true: after `dross phase complete` every task card and the phase card
                   read closed at task-complete / complete server-side, `git status --porcelain` is empty, and local
                   base == origin/base
                 - board fake returning 500 on close → exit non-zero naming `dross issue phase finalize <id>`, while
                   state.json carries "completed <id>", changes.json reads status=complete, and phase/<id> is gone
                   locally and on origin. A following `dross issue phase finalize <id>` closes every card
                 - [board].enabled=false with base_url pointing at a counting server → zero requests, exit 0, no
                   board line in the output
                 - re-running complete after success → zero non-GET board requests, HEAD unchanged (no chore commit),
                   board.json byte-identical
                 - [board].auth_env naming an unset variable → complete exits non-zero naming the finalize verb, with
                   the completion record already written (openBoard runs after teardown, not before)
                 - `issue phase finalize x` with the board off exits 0 with zero requests. Off the recorded base it
                   refuses
                 - TestCLISurfacePinned fails if `issue phase finalize` is missing from issue.txt

  t-13 Reap creates missing cards for completed phases               [risk; reap.go from mvp+verification]
       files:    internal/boardsync/reap_catchup.go, internal/boardsync/reap_catchup_test.go,
                 internal/boardsync/reap.go, internal/boardsync/reap_apply.go,
                 internal/cmd/issue_reap_cmd.go, internal/cmd/issue_reap_cmd_test.go
       covers:   c-11
       depends:  t-1, t-5
       desc:     ReapPlan (reap.go:58) gains Missing. Inventory walks phase dirs whose changes.json is Complete() and
                 makes one State "all" dross/phase:<id> query per phase (phase and task cards both carry the label)
                 to decide which phase and task cards are missing. A dry run prints a "Missing" section and writes
                 nothing. --apply calls FinalizePhase per phase, record-and-continue. Honors --namespace
                 (Phases/Tasks). Creations are not journaled as closes.
       contract: - dry run (writingIsFatalYT) with a complete phase the board has never heard of → listed with its N
                   tasks, zero non-GET requests, board.json and reap-log.json byte-identical, and no "does not know
                   the label" line on stderr
                 - detection issues one State "all" dross/phase:<id> lookup per complete phase and never a bulk marker
                   listing (asserted on the request log)
                 - a complete phase whose card exists CLOSED and is absent from board.json → not listed
                 - a complete phase that has its card but one card-less task → only that task is listed
                 - a shipped-but-not-complete phase without a card → not listed
                 - --apply → phase card at complete and each task card at task-complete, all closed, with the labels
                   FinalizePhase writes. A following dry run lists nothing missing
                 - one phase's create failing → the other phases are still created, and the exit is non-zero naming
                   the failed phase
                 - --namespace Backlog → no catch-up lookup is made
                 - `reap --undo` after a catch-up apply changes no created card
                 - a complete phase with no spec.toml → listed as cannot-create with the reason, and skipped by
                   --apply

Wave 3
  t-14 Write plan.toml through the keyed lossless patcher            [risk+verification]
       files:    internal/phase/phase.go, internal/phase/plan_lossless_test.go, internal/phase/phase_test.go,
                 internal/cmd/task_lossless_test.go
       covers:   c-4
       depends:  t-7
       desc:     Plan.Save goes through the door with identity task→id, with a fresh encode only for a new file.
                 Spec.Save keeps saveTOML. task status/add/edit/move/remove, `issue task pull --apply` and
                 `phase migrate` all inherit it through the one Plan.Save choke point. task_seq, reflowed waves and
                 stripped depends_on count as changed task state, not collateral.
       contract: - fixture with a header comment, `# about t-3` above t-3, inline comments on untouched lines, a
                   trailing comment in t-2 and a hand-wrapped multi-line test_contract: `task status t-2 done`
                   changes exactly t-2's status line
                 - `task add --after t-1` inserts the new block between t-1 and t-2. Outside it only task_seq changes
                 - `task edit t-3 --title X` changes only t-3's title line
                 - `task move t-4 --before t-2` relocates t-4's block with its leading comment and rewrites only the
                   wave lines of reflowed pending dependents. The t-2 and t-3 bytes are unchanged
                 - `task remove t-3 --force` deletes only t-3's block and the "t-3" entries in dependents' depends_on
                   lines
                 - corpus: every .dross/phases/*/plan.toml in the repo (134 files), copied to a temp dir, with its
                   first task's status flipped → the diff is that one line (or one inserted key) and LoadPlan matches
                 - a forced verify mismatch → plan.toml byte-identical, and `task status` errors naming plan.toml
                 - TestSaveTOMLAtomicFailurePreservesFile (name kept) is re-pointed to a failure the new writer
                   actually hits (a read-only phase dir: the `<path>.tmp` dir trick no longer blocks a CreateTemp
                   name) and still proves plan.toml byte-identical
                 - a no-op status write (done→done) writes nothing (mtime unchanged)

  t-15 Close routed mirrors only on disposition records              [verification structure; risk contracts]
       files:    internal/boardsync/backlog.go, internal/boardsync/reap.go, internal/boardsync/reap_discover.go,
                 internal/boardsync/backlog_reconcile_test.go, internal/boardsync/reap_classify_test.go
       covers:   c-6
       depends:  t-11, t-13
       desc:     Both paths call Disposed. SyncBacklog drops disposed items from the live push set, and
                 BacklogVerdictFor resolves them. routedVerdict and reapBacklogVerdictByID (linked and
                 label-discovered cards) strand only on disposition and otherwise keep today's unattributable
                 "finished destination" reason. Target completion alone never closes. (t-13 ordering: shared
                 reap.go.)
       contract: - TestBacklogVerdictTable gains rows: a routed survivor that is accepted → Resolved. A routed item
                   with its target complete and no absorbing criterion → StillOpen (the 47a5c93 row is kept
                   verbatim)
                 - agreement test: over the same fixture rows, BacklogVerdictFor==Resolved iff
                   reapBacklogVerdict==ReapStranded
                 - target complete + a target criterion listing the id → one close in backlog sync, and reap lists it
                   stranded with a why naming phases/<target>/spec.toml and the criterion id
                 - after a disposition close, a second backlog sync makes zero UpdateIssue/CloseIssue calls for that
                   card
                 - a label-discovered dross/deferred:<id> orphan whose item is disposed strands with the why naming
                   the record. With the target complete and no record it stays unattributable

  t-16 Drop ship §6 close steps; repoint the lifecycle guards        [risk+verification]
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go
       covers:   c-2
       depends:  t-5, t-6, t-12
       desc:     Remove §6 steps 4 and 5. Step 3 says `dross phase complete` closes the task and phase cards, and
                 Recovery gains a board-finalize entry naming `dross issue phase finalize <phase-id>`. emittedStatuses
                 gains a third source: FinalizePhase's status arguments, parsed from finalize.go and resolved through
                 package consts (declarations alone are not counted). mirrorLanes' Phases/Tasks rows point at that
                 source. The two recorded ship tests keep their names and get inverted bodies. tests_before.txt is
                 not edited. r-01: `make install` before dogfooding.
       contract: - TestShipPromptClosesTaskCardsAfterPhaseComplete (name kept) fails if ship.md contains
                   `dross issue task sync <phase-id> --status task-complete --close`
                 - TestShipPromptEmitsTerminalBoardStatuses (name kept) fails if ship.md contains
                   `phase sync <phase-id> --status complete --close`, if `--status shipped` no longer precedes
                   `dross phase complete <phase-id>`, or if §6/Recovery stops naming `dross issue phase finalize`
                 - deleting the task-complete argument from finalize.go fails TestEmittedStatusesAreTheLifecycleSet,
                   and TestStateMapsKeyExactlyTheEmittedStatuses stays green only because finalize.go is scanned
                 - TestEveryMirrorLaneHasATerminalEmission fails if FinalizePhase stops closing either the Phases or
                   the Tasks lane, and still fails for a board namespace with no terminal
                 - TestCloseEmissionsCarryAValidStatus counts the finalizer's close emissions, so it stays non-vacuous
                   once the prompt `--status … --close` lines are gone (milestone.md:161 and quick.md:242 carry no
                   --status)
                 - TestNoTestLost stays green

Wave 4
  t-17 Sync README with finalize, absorb and catch-up                [risk]
       files:    README.md, internal/cmd/readme_board_finalize_test.go
       covers:   —
       depends:  t-9, t-12, t-13, t-16
       desc:     The issue verb list (README.md:242) gains `phase finalize`. The reap paragraph gains the
                 completed-phase catch-up. `dross phase complete` is documented as finishing the board, and
                 `dross deferred absorb` is listed. (risk's "footprint token rows" are dropped: README has none.)
       contract: - TestEveryDocumentedIssueVerbResolves stays green with `phase finalize` in README's issue brace list
                 - readme_board_finalize_test fails if README omits `dross deferred absorb`, stops saying
                   `dross phase complete` closes the board cards, or omits reap's creation of missing completed-phase
                   cards
```

**Coverage:** c-1 t-5, t-12 · c-2 t-16 · c-3 t-1 · c-4 t-2, t-7, t-14 · c-5 t-2, t-8 · c-6 t-11, t-15 · c-7 t-3, t-9 · c-8 t-4 · c-9 t-10 · c-10 t-6 · c-11 t-13 · t-17 covers none (README owner). 11/11.

**Shared-file ordering** (dependencies that only order an edit): t-4→t-9 (deferred.go), t-4→t-10 (survivor.go), t-13→t-15 (reap.go), t-6→t-16 (ship.md). Within each wave, no two tasks edit the same file.

## Disagreements

1. **Where the generalized patcher lives.** mvp and verification extract it to a new `internal/tomlpatch`, arguing that phase/survivor should not import the config package. risk generalizes it in place in `internal/project`, arguing the move is 2,767 lines and the Project-coupled differ tests cannot follow. **Default: in place (risk).** Verified: patch_diff_test.go builds `*Project` fixtures and calls the unexported `diff`/`op`, so moving it into tomlpatch hits an import cycle (internal test) or loses access (external test). `internal/project` imports nothing internal, so phase/survivor importing it creates no cycle. Both shapes have to touch project_writer_pin_test.go and the ARCHITECTURE anchors; risk's draft missed that, and it is grafted into t-2. **Why it matters:** this sets the size of the wave-1 blast radius, and it is the one choice that touches ~70 recorded tests.

2. **Keyed identity: its own task or folded in.** mvp folds keyed matching and insert-at-index into the extraction (t-1). risk and verification make it a separate task. **Default: separate (t-7).** **Why:** a project.toml regression and a positional-rewrite-on-move bug are different failures. Splitting them keeps each one red under exactly one task.

3. **Does survivors.toml need keyed matching?** verification and mvp key `accepted→key` and `category→name`, and verification makes survivors wait on the keyed diff (wave 3). risk uses today's positional/deep-equal matching and lands survivors in wave 2. **Default: risk's (t-8 depends on t-2 only), with verification's contracts grafted.** Verified: `Add` replaces an existing key in place (store.go:167), new keys append, retire deletes, keys are unique, and nothing sorts the store, so deep-equal matching is exact. **Why it matters:** one wave of serialization against a defensive key. If t-8's new-category or retire-middle contract fails, promote it onto t-7.

4. **Finalizer shape and what "re-running changes nothing" means.** mvp composes the existing SyncPhase + SyncTasks and asserts outcome-idempotency (no new card, same labels). risk and verification build a dedicated FinalizePhase asserting zero writes on re-run. They differ on the skip predicate: risk requires terminal label AND done, verification done only. **Default: dedicated finalizer, zero writes, risk's label-AND-done skip.** Verified: SyncPhase calls EnsureMilestoneLink (boardsync/phase.go:93) and UpdateIssue on every call (:126), so composition re-writes every card on re-run and mints open epics during catch-up. **Why:** c-1 says "changes nothing", and catch-up over old milestones must not resurrect epics.

5. **board.json after finalization.** mvp leaves the finalizer's board.json write uncommitted, as ship's §6 does today. risk and verification commit it and route it through autoCommitDrossDirt + routeBaseChores. **Default: commit + route (t-12).** Verified: `.dross/board.json` is git-tracked, and complete's own record path (phase.go:728/736) commits and routes for exactly this reason. **Why:** leaving it reintroduces the unpushed-base-chore divergence that the ship-clean-tree work closed.

6. **How reap catch-up detects missing cards.** mvp and verification use one marker-label listing plus board.json. risk uses one State "all" `dross/phase:<id>` query per complete phase. **Default: per-phase (risk).** Verified: forge/GitHub ListIssues fetch a single 50-item page (forge.go:712, github.go:228), and reap discovery's marker listing is State "open" only (reap_discover.go:100). A bulk listing truncates silently, so "truncated" reads as "missing" and --apply mints duplicates. **Why:** the cost is N lookups per dry run against duplicate cards on a live board. The lookup cost is the cheaper failure.

7. **c-6 task structure.** risk has one task (predicate + both call sites). verification splits a pure predicate (wave 2) from the wiring (wave 3, after catch-up). mvp folds the predicate together with the c-7 schema field. **Default: verification's split (t-11 / t-15).** **Why:** risk's single task and its catch-up task both sit in wave 2 and both need reap.go (`ReapPlan` lives at reap.go:58). The split removes that collision and adds the BacklogVerdictFor⇔reap agreement test.

8. **Survivor disposal evidence.** mvp: tests.json exists and the key is in neither Surviving nor OutOfScope. risk: also newer than the source phase's run, error-free leg, scope reaches the survivor's directory. verification: verify.toml finalized and the run measured the survivor's package. **Default: the union of risk and verification, at file granularity.** Verified: verify.Scope.Files is the in-scope file set and hunks are advisory (scope.go:17–41). **Why:** mutation is diff-scoped. Plain absence would close cards on runs that never mutated the file, which is the 527-closure shape in a new form.

9. **What backlog sync does with a disposed live item after the close.** risk drops it from the live push set: a second sync makes zero Update/Close calls. verification keeps the link and relies on IssueIsDone to avoid a re-close: zero Close calls only. **Default: risk's stricter contract (t-15).** **Why:** a push UpdateIssue on a closed card is a write on every sync, and on some providers it may reopen the card.

10. **Does c-8 also gate `deferred add --target`?** risk and verification: yes, it is the third stamping path the route_to_complete decision describes. mvp: no, because c-8 names only the two route verbs. **Default: include (t-4).** **Why:** leaving `add --target` open lets a finished phase keep absorbing scope through the side door the decision closes. Include-first posture.

11. **Bundling c-8 with c-9.** mvp merges the refusal and the in-place re-route into one task. risk and verification keep them apart. **Default: separate (t-4, t-10).**

12. **Re-route edge cases.** risk refuses when more than one entry carries the key; the others are silent on duplicates. verification mints an id on first route; risk and mvp append id-less as today. **Default: refuse duplicates (risk) AND mint on first route (verification).** Verified: `deferred add` mints at filing (deferred_add.go:60) and the Deferred.ID doc comment says ids are "assigned when it is filed", while `survivor route` appends with no ID (survivor.go:189). **Why:** without minting, c-9's "same id" is checkable only against a pre-seeded fixture. Without the duplicate refusal, the second entry's card stays live with a stale target.

13. **`deferred absorb` argument shape.** risk and mvp: `absorb <source> <idx> --criterion <c-id> [--phase <id>]`. verification: `absorb <phase> <criterion-id> <source> <idx>`. **Default: risk/mvp.** **Why:** it matches the `<source> <idx> --flag` shape of `deferred route` and defaults to the phase /dross-spec is writing.

14. **Where `Criterion.Deferred` lands.** risk and verification: with validate (wave 1). mvp: with the disposition predicate, so the c-6 task can run in wave 1. **Default: with validate (t-3).** Both later consumers (t-9, t-11) depend on it.

15. **c-2 and c-10 structure.** mvp: one 9-file wave-2 prompt task. risk and verification: c-10 sweep in wave 1, c-2 after the finalizer and verb exist. **Default: split (t-6, t-16).** **Why:** c-10 has no Go dependency and should not wait. c-2 cannot land before finalize.go exists without turning the four divergence guards red.

16. **The recorded ship tests.** risk and mvp keep both names with inverted bodies. verification says TestShipPromptClosesTaskCardsAfterPhaseComplete "is replaced" and deletes the "phase complete gains no board coupling" sub-check (ship_prompt_test.go:337–345) as superseded by board_failure_posture. **Default: keep both names (TestNoTestLost, tests_before.txt:3359/3361) and swap that sub-check for the finalize-recovery assertion.** **Why:** a rename or delete fails TestNoTestLost, but the sub-check's rationale (locked terminal_emit_sites) no longer holds.

17. **Docs ownership.** risk gives README a wave-4 owner. verification folds README into the complete task. mvp has no README edit and explicitly leaves ARCHITECTURE.md's board prose stale. **Default: risk's t-17, minus its "footprint token rows" item (README has none).** **Open, not added:** ARCHITECTURE.md:559 still says ship emits `--status complete --close` and `issue task sync --status task-complete --close` and that `dross phase complete` has "no board coupling". After t-16 that is false. No draft owns the fix, and no test pins the prose. Flagging it for the lead rather than inventing a task.

18. **One identity-label list or two.** risk makes reap_discover.go read its prefixes from forge's list. mvp and verification keep both lists and rely on a drift test. **Default: risk's (single source + drift test, t-1).** **Why:** both versions are guarded, but one source removes the edit that would otherwise have to land in two packages.
