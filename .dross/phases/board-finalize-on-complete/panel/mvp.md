# MVP lens — board-finalize-on-complete

Lens: smallest task set that satisfies every criterion; merge aggressively; every task traces to a criterion.

```
Phase board-finalize-on-complete — 9 tasks across 2 waves

Wave 1
  t-1  Extract raw-byte patcher into internal/tomlpatch
       files:    internal/tomlpatch/patch.go, internal/tomlpatch/diff.go,
                 internal/tomlpatch/patch_test.go, internal/tomlpatch/diff_test.go,
                 internal/project/project.go, ARCHITECTURE.md,
                 internal/cmd/lossless_docs_test.go, internal/cmd/project_writer_pin_test.go
                 (moves out of internal/project/patch.go, patch_diff.go, patch_value.go
                 and their _test.go files, which are deleted)
       desc:     Move indexDoc/apply/diff/value rendering to internal/tomlpatch, generic over
                 any encodable value plus a caller-supplied decode for the verify-then-write
                 step. Add keyed array-of-tables matching (the caller names the identity key
                 per array path) and an insert-at-index op, so a reorder or anchored insert
                 moves whole blocks. project.Save calls it with behaviour unchanged; re-point
                 the ARCHITECTURE anchors and the encoder/writer pins.
       covers:   c-4, c-5
       depends:  —
       contract: - tomlpatch diff_test: in a commented [[task]] doc, moving id=t-3 before id=t-1
                   emits one delete plus one insert of t-3's block, and every other line
                   (comments included) stays byte-identical. Positional matching rewrites
                   t-1..t-2 field by field and fails this test.
                 - tomlpatch patch_test: inserting a keyed element at index 1 puts its block
                   between elements 0 and 1, not at EOF.
                 - internal/project TestSaveRefusesUnverifiedPatch still fails Save when the
                   patched bytes decode differently from p, so dropping the verify gate in the
                   move is caught.
                 - lossless_docs_test fails while ARCHITECTURE.md's apply/diff/encodeFresh
                   anchors still name the deleted internal/project/patch*.go.

  t-2  Finalize the board inside dross phase complete
       files:    internal/boardsync/finalize.go, internal/boardsync/finalize_test.go,
                 internal/cmd/issue.go, internal/cmd/phase.go,
                 internal/cmd/phase_complete_board_test.go,
                 internal/cmd/testdata/cli_surface/issue.txt
       desc:     boardsync.FinalizePhase(ctx, id) = SyncPhase(complete, close) then
                 SyncTasks(task-complete, close). It creates any missing card and is idempotent.
                 `dross issue phase finalize <id>` exposes it (no-op when [board].enabled is
                 off). phaseComplete calls it last, after the record and the branch teardown,
                 only when the board is enabled. A board error leaves the completion intact,
                 exits non-zero, and names `dross issue phase finalize <id>`. Regenerate the
                 issue CLI-surface golden.
       covers:   c-1
       depends:  —
       contract: - finalize_test: on a fake board with one open phase card and two open task
                   cards, FinalizePhase leaves the phase card complete+closed and both task
                   cards task-complete+closed. A second call creates no card and leaves every
                   card's state and labels unchanged.
                 - phase_complete_board_test (reuses phase_test.go's completeFixture):
                   [board].enabled=false and a tracker server that t.Fatal's on any request.
                   `dross phase complete` succeeds and the server records zero hits.
                 - phase_complete_board_test: the tracker returns 500 on issue writes.
                   `dross phase complete` exits non-zero and the error names
                   `dross issue phase finalize <id>`. changes.json still reads status=complete
                   and phase/<id> is gone locally.
                 - phase_complete_board_test: running `dross phase complete <id>` twice against
                   a recording tracker gives zero creates on the second run, and card states
                   equal the first run's.

  t-3  Silence unknown-identity-label lookups, keep filter warnings
       files:    internal/forge/forge.go, internal/forge/forge_test.go,
                 internal/boardsync/identity_label_test.go
       desc:     Add forge.IsIdentityLabel (dross/task:, dross/phase:, dross/deferred:,
                 dross/target:). WarnDroppedLabels, which all four ListIssues share, skips
                 identity labels. They are still dropped, so the lookup returns no card. Unknown
                 dross/status:, the dross marker and non-dross labels still warn.
       covers:   c-3
       depends:  —
       contract: - forge_test: forgejo and youtrack ListIssues filtered on an unknown
                   `dross/task:p/t-1` write nothing to stderr and return zero issues.
                 - forge_test: a filter naming unknown `bug` and unknown `dross/phase:x` warns
                   naming `bug` only.
                 - identity_label_test: PhaseLabel, TaskLabel, DeferredLabel and TargetLabel
                   outputs classify as identity. StatusLabel, LabelMarker and LabelQuick do
                   not. A new identity constructor with an unrecognised prefix fails here.

  t-4  Refuse routes to complete phases; re-route in place
       files:    internal/cmd/deferred_target.go, internal/cmd/deferred.go,
                 internal/cmd/survivor.go, internal/cmd/deferred_test.go,
                 internal/cmd/survivor_test.go
       desc:     A new refuseCompleteTarget(root, slug) (changes.Complete) runs in
                 `deferred route` and `survivor route` before anything is read or written,
                 naming the phase. survivor route first finds an existing entry with the same
                 survivor key across all stores (deferred.Collect) and rewrites its target in
                 its own source spec, keeping its id. The same target writes nothing.
       covers:   c-8, c-9
       depends:  —
       contract: - deferred_test: `deferred route p 0 --target done` where done's changes.json
                   reads complete exits non-zero naming `done`, and p's spec.toml bytes are
                   unchanged.
                 - survivor_test: `survivor route f.go:3 --target done` likewise refuses,
                   current spec bytes unchanged.
                 - survivor_test: with key K already routed (entry id "abc" in phase a's spec),
                   routing K to y rewrites that entry's target with id still "abc" and adds no
                   [[deferred]] entry to the current spec. Routing K to y again leaves both
                   specs byte-identical.

  t-5  Close routed backlog only on a disposition record
       files:    internal/phase/phase.go, internal/boardsync/disposition.go,
                 internal/boardsync/backlog.go, internal/boardsync/reap.go,
                 internal/boardsync/disposition_test.go
       desc:     Add `Deferred []string toml:"deferred,omitempty"` to phase.Criterion.
                 RoutedDisposed(root, entry) is answered from records only:
                 - survivor item: survivors.toml accepts the key, or the target's tests.json
                   exists and lists the key in neither Surviving nor OutOfScope;
                 - plain item: the target is complete AND one of its spec criteria lists the
                   item's id.
                 BacklogVerdictFor's live-routed branch and reap's routedVerdict resolve on it.
                 Target completion alone never closes.
       covers:   c-6, c-7
       depends:  —
       contract: - disposition_test: an item routed to a complete target whose spec names no
                   criterion with its id. Backlog sync leaves the mirror open and reap lists it
                   unattributable. Adding deferred=["<id>"] to a target criterion makes backlog
                   sync close it and reap list it stranded.
                 - disposition_test: deferred=["<id>"] on an incomplete target leaves the
                   mirror open.
                 - disposition_test: a routed survivor stays open while the complete target's
                   tests.json still lists its key surviving. Accepting the key in
                   survivors.toml closes it. A target tests.json without the key also closes it.

Wave 2 (depends t-1, t-2, t-5)
  t-6  Route plan.toml and survivors.toml through tomlpatch
       files:    internal/phase/phase.go, internal/survivor/store.go,
                 internal/cmd/task_lossless_test.go, internal/cmd/survivor_test.go,
                 internal/survivor/store_test.go
       desc:     Plan.Save patches an existing plan.toml through tomlpatch, keyed task→id. A
                 fresh encode happens only when the file is absent. survivor.Save does the same,
                 keyed accepted→key and category→name. Spec.Save stays on saveTOML.
       covers:   c-4, c-5
       depends:  t-1
       contract: - task_lossless_test: on a plan.toml with a header comment, per-task comments
                   and a trailing comment, each verb (`task status`, `task add --after`,
                   `task edit`, `task move --before`, `task remove`) leaves every line byte-identical
                   except the blocks of tasks whose decoded fields changed and the top-level
                   task_seq line.
                 - survivor_test: the store holds an entry whose key line ends
                   `# dross:allow-secret`. `survivor accept` of a new key leaves that entry's
                   lines byte-identical and `dross validate` exits 0. Reverting Save to
                   toml.NewEncoder strips the marker and the secret self-scan fails validate.
                 - store_test: Retire of a different key leaves the marked entry byte-identical.

  t-7  Add `deferred absorb` and validate absorbed ids
       files:    internal/cmd/deferred_absorb.go, internal/cmd/deferred.go,
                 internal/cmd/validate.go, internal/cmd/deferred_absorb_test.go,
                 internal/cmd/validate_test.go, assets/prompts/spec.md,
                 internal/cmd/spec_prompt_test.go
       desc:     `dross deferred absorb <source> <idx> --criterion <c-id>` refuses unless the
                 item is routed to a phase whose spec has that criterion. It mints the item's
                 id if missing, then appends the id once to that criterion's `deferred` list.
                 `dross validate` refuses an absorbed id naming no deferred item routed to that
                 phase. /dross-spec runs absorb, after the §5 write, for each criterion seeded
                 from a parked item.
       covers:   c-7
       depends:  t-5
       contract: - validate_test: criterion deferred=["nope"] fails validate naming phase,
                   criterion and "nope". The id of an item routed to a different phase also
                   fails. An item routed to this phase passes.
                 - deferred_absorb_test: absorbing an id-less item routed to P stamps a fresh id
                   on the source entry and the same id on P's criterion. Re-running leaves one
                   copy. Absorbing an item routed elsewhere is refused with both specs
                   byte-identical.
                 - spec_prompt_test: spec.md's parked-item path carries
                   `dross deferred absorb <source> <idx> --criterion` after the spec.toml write.

  t-8  Reap creates cards for completed, card-less phases
       files:    internal/boardsync/reap.go, internal/boardsync/reap_apply.go,
                 internal/cmd/issue_reap_cmd.go, internal/boardsync/reap_missing_test.go,
                 internal/cmd/issue_reap_cmd_test.go
       desc:     Classify adds ReapPlan.Missing: every phase whose changes.json reads complete
                 but has no phase card, or has a plan task with no task card. It is built from
                 board.json plus one marker-label listing, not per-card lookups. A dry run
                 prints Missing and writes nothing. --apply runs FinalizePhase per missing
                 phase.
       covers:   c-11
       depends:  t-2
       contract: - reap_missing_test: a complete phase with no cards and two plan tasks. The dry
                   run lists the phase and both tasks, the recording tracker sees zero writes,
                   and board.json is byte-identical.
                 - reap_missing_test: --apply creates one phase card complete+closed and two
                   task cards task-complete+closed. A following dry run lists nothing missing.
                 - reap_missing_test: a phase whose changes.json reads shipped (not complete) is
                   never listed missing.

  t-9  Drop ship's close steps; name [board].enabled everywhere
       files:    assets/prompts/ship.md, assets/prompts/execute.md, assets/prompts/milestone.md,
                 assets/prompts/plan.md, assets/prompts/quick.md, assets/prompts/verify.md,
                 internal/cmd/ship_prompt_test.go,
                 internal/boardsync/board_lifecycle_divergence_test.go,
                 internal/cmd/board_switch_prompt_test.go
       desc:     In ship.md §6, delete steps 4–5 (the task close and the phase close). Step 3
                 now says complete finalizes the board and that a non-zero exit after the record
                 means run `dross issue phase finalize <id>`. Every "no-op unless
                 `[remote].board_sync`" becomes "no-op when `[board].enabled` is false; a
                 non-zero exit is a board failure to surface". Re-point the divergence test's
                 Phases/Tasks lane emission and its emitted-status scan at
                 internal/boardsync/finalize.go. Invert the two ship tests that pinned the
                 close lines.
       covers:   c-2, c-10
       depends:  t-2
       contract: - ship_prompt_test: fails if ship.md contains
                   `issue task sync <phase-id> --status task-complete --close` or
                   `issue phase sync <phase-id> --status complete --close`, or lacks
                   `dross issue phase finalize`.
                 - board_switch_prompt_test: any assets/prompts/*.md containing `board_sync`
                   fails naming file:line. execute/milestone/plan/quick/ship/verify.md each
                   fail if they lack `[board].enabled` or the non-zero-exit-is-a-failure
                   sentence.
                 - divergence test: TestStateMapsKeyExactlyTheEmittedStatuses stays green
                   with ship.md no longer emitting complete/task-complete only because
                   finalize.go's status literals are scanned. Removing them reports both
                   statuses as mapped-but-never-emitted.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-9 |
| c-3 | t-3 |
| c-4 | t-1, t-6 |
| c-5 | t-1, t-6 |
| c-6 | t-5 |
| c-7 | t-5 (Criterion.Deferred field), t-7 (absorb verb, validate, /dross-spec) |
| c-8 | t-4 |
| c-9 | t-4 |
| c-10 | t-9 |
| c-11 | t-8 |

11/11 covered. Every task covers at least one criterion.

## Judgment calls

- **Where the patcher lives:** extracted to a new `internal/tomlpatch`. I rejected exporting it from `internal/project`, because that makes phase and survivor import the config package for a generic engine. The cost is re-pointing ARCHITECTURE anchors and the encoder/writer pin tests inside t-1.
- **Array matching:** array-of-tables match by identity key (task.id, accepted.key, category.name), plus an insert-at-index op. I rejected the existing positional/deep-equality matching: on `task move` it rewrites every task between the endpoints field by field under the wrong comments, and on `task add --before` it appends at EOF. Both fail c-4.
- **What counts as "changed" for c-4:** the top-level `task_seq` line (add/remove) and dependents whose waves reflow (move) or whose depends_on is stripped (`remove --force`) count as changed tasks. They are plan state the verb changes, not collateral. The alternative reading cannot be satisfied.
- **spec.toml writes:** they stay on saveTOML. No criterion asks for lossless spec writes, and putting a third store through the patcher widens t-6 for nothing.
- **c-5's "route":** `survivor route` never writes survivors.toml (it appends to a spec). A lossless Save covers accept and retire; route needs no work of its own.
- **The finalizer:** it composes the existing SyncPhase and SyncTasks, both of which already create a missing card and close on create. I rejected a new sync path. Idempotency is asserted on the outcome (no new card, identical state and labels), not on zero HTTP writes, because a read-before-write skip path is extra machinery no criterion requires.
- **Leftover board.json dirt:** the finalizer's board.json write after complete's record commit stays uncommitted, as ship's §6 board steps leave it today. I rejected adding a second auto-commit and publish to complete; no criterion asks for it.
- **c-3:** the fix is one predicate in the shared `forge.WarnDroppedLabels`, which serves all four providers. I rejected a "quiet" flag on IssueFilter threaded through every identity call site. `dross/target:` counts as an identity label because reap discovery resolves cards by it.
- **c-7:** a new `dross deferred absorb <source> <idx> --criterion` verb. I rejected exposing ids in `deferred list --json`. Spec-authored `[[deferred]]` items stay id-less until a board sync stamps them, so /dross-spec would have no id to write on board-off repos. The JSON route would also break deferred-add-command's locked `deferred_identity` pin, `TestDeferredEntryIDStaysInternal`.
- **Criterion.Deferred placement:** the field lands in t-5 (disposition), not t-7. t-5 can then run in wave 1, and the two `reap.go` editors (t-5, t-8) fall in different waves.
- **c-8:** a separate gate from `validDeferredTarget`. Folding "complete" into the shared target gate would make `dross validate` call every existing route to a since-finished phase dangling, and would change `deferred add --target`. c-8 names only the two route verbs.
- **c-9:** the existing entry is found across every deferred store (`deferred.Collect`), not only the current spec. A survivor routed in an earlier phase lives in that phase's spec.
- **c-6 evidence for survivors:** "no longer lists it surviving" means the target's tests.json exists and the key is in neither Surviving nor OutOfScope. A target with no recorded verify run is not evidence.
- **c-11 discovery:** one marker-label listing plus board.json, rather than per-phase and per-task label lookups (N×M tracker queries per dry run). Created cards are not journaled for `--undo`, which restores prior columns; a card that did not exist has none.
- **c-2 and c-10:** one prompt task in wave 2 (9 files, mechanical wording). Both criteria edit ship.md §6. Removing ship's close lines also removes the corpus's only `complete`/`task-complete` emissions, which breaks the lifecycle-divergence and mirror-lane emission tests until finalize.go is their emitter. So the prompt work must follow t-2, and splitting it would edit ship.md twice across waves.
- **Doc updates:** ARCHITECTURE.md's "leaving `dross phase complete` with no board coupling" prose goes stale but is not refreshed here. No criterion or doc test pins it, and the next architecture pass owns that.
