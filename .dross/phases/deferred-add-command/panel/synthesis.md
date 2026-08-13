# Panel synthesis — deferred-add-command

Judge notes: file references and code claims in all three drafts were checked
against `internal/cmd/deferred.go`, `internal/cmd/issue.go` (`syncBacklog`,
key `someday:<source>#<idx>` at issue.go:364), `internal/cmd/validate.go`
(dangling-target check at validate.go:70-121, iterates phase dirs only),
`internal/phase/phase.go` (`Deferred` struct has no `ID`; `Slugify` drops a
leading `_` because `b.Len() == 0`), `internal/board/board.go`,
`assets/prompts/pause.md`, `internal/cmd/hints_test.go`
(`TestPromptsTeachNoBrokenInvocation`), `internal/cmd/issue_test.go`
(`TestIssueBacklogSyncYouTrack{Idempotent,EpicMode,AgileMode}` all exist),
`.dross/board.json` (no `backlog` key at all), `.dross/milestones/v1.3.toml`.
All file references in all three drafts are real. Two claims decided merges:

- **`mutation-score-truth` is in v1.3's `phases` array and has no phase
  directory.** risk's t-7 claim is exact — filing the second homeless finding
  with `--target mutation-score-truth` is the only end-to-end exercise of the
  milestone-array acceptance branch.
- **`.dross/handoff.md` is gitignored** (`.gitignore:12`, alongside
  `state.json` at :23); `spec.toml` is tracked. This decides disagreement D5
  against a full repo-level gate on handoff.md content.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** (7/4) | 8/8, plus an explicit locked-decision map (all four honoured, incl. `deferred_identity` pinned by a "`list --json` has no `id` key" assertion no other draft has) | Strongest kill-framing: most contracts name the mutation they die to ("deleting the milestone-array branch fails this test", "byte-identical afterwards") | Best — 7 tasks, one failure mode each; `issue.go` owned by exactly one task so a partial board push is attributable | Correct and maximally parallel: t-3/t-4/t-5 all fan out of wave 1 on real edges (t-3←t-1, t-4←t-1+t-2, t-5←t-1) |
| **verification** (6/4) | 8/8, and the only draft that closes the loop on the *writer* of handoff.md (`pause.md`) | Finest-grained and most mechanically checkable: "per-issue PATCH bodies inspected", "handler `t.Error`s on any request", "byte-identical error text from `add` and `route`", "existing YouTrack sync tests pass unchanged" | Good but front-loaded: t-1 is 5 files / 9 contracts (self-admitted over cap) and mixes store creation with rename+validate readers | Correct; t-6←t-5 elides a real t-3 edge (transitive, harmless) |
| **mvp** (4/3) | 8/8 on paper, but c-6 is thin: reusing `syncBacklog` wholesale cannot mirror a *routed* add at all (issue.go:360 skips `Target != ""`), and its per-invocation blast radius includes every other pending backlog item | Weakest: prose paragraphs, contracts bundled per task rather than per behaviour; several are unfalsifiable as written ("visible to the next `list`") | Too coarse: t-3 covers five criteria in one commit, so a wave-2 failure is unattributable; deliberately the point of the lens, but it loses the per-criterion gate | Correct edges, only 3 waves; nothing wrong, just less parallel |

**Skeleton: risk.** Finest attribution (one failure mode per task, `issue.go`
single-owner), the only explicit locked-decision map, and the only wave graph
that isolates the two independent correctness fixes (store, validator) from all
board work. verification supplies the sharper contracts, which are grafted in
throughout; mvp supplies one real correction (D3's cost accounting) and one
task-shape argument that is recorded as a divergence rather than adopted.

## Merged plan

7 tasks across 4 waves.

```
Wave 1
  t-1  Add deferred id + reserved project store                        [risk]
       files:    internal/phase/phase.go
                 internal/cmd/deferred_store.go (new)
                 internal/cmd/deferred_store_test.go (new)
                 internal/cmd/deferred.go
                 internal/cmd/validate.go
       covers:   c-2, c-4
       desc:     Add `ID string toml:"id,omitempty"` to phase.Deferred (and
                 `json:"-"` on deferredEntry — the id stays internal per the
                 locked deferred_identity). Reserved source slug `_project`
                 backed by `.dross/deferred.toml` in phase.Spec shape, so
                 phase.LoadSpec/Save work unchanged. One
                 `deferredStore(root, source)` resolver every verb routes its
                 path through. collectDeferred appends store entries as source
                 `_project` and skips a `phases/_project` directory; validate
                 reports such a directory as a reserved-slug problem, and also
                 walks the store for dangling targets; repointDeferredTarget
                 walks the store so `phase rename` can't orphan a store entry.
       contract: - `.dross/deferred.toml` round-trips one `[[deferred]]` with
                   id+text+target; dropping the `id` toml tag fails the
                   reload-and-compare assertion.                       [risk]
                 - phase.Deferred.ID is absent from the emitted TOML on an
                   entry without one (omitempty).            [verification]
                 - `deferredStore(root, "_project")` returns
                   `.dross/deferred.toml`; falling through to phase.Dir fails
                   the returned-path assertion on
                   `.dross/phases/_project/spec.toml`.                 [risk]
                 - `deferredStore(root, "nope")` errors for a slug with no
                   phase dir instead of returning a creatable path.    [risk]
                 - a hand-made `.dross/phases/_project/spec.toml` fails
                   `dross validate` with a reserved-slug problem and its items
                   do NOT appear in `deferred list` — without the skip two
                   sources share the slug and index 0 is ambiguous.    [risk]
                 - `phase.Slugify` can never emit `_project`: if slugify starts
                   keeping leading underscores this test fails before a real
                   phase can shadow the store.               [verification]
                 - `deferred list --json` unmarshalled into `map[string]any`
                   has no `id` key (locked deferred_identity).         [risk]
                 - a bogus target in `.dross/deferred.toml` makes `dross
                   validate` exit non-zero naming deferred.toml — the
                   hand-edit path is not a hole.             [verification]
                 - `phase rename alpha alpha2` repoints a `_project` entry
                   routed to alpha.                          [verification]

  t-2  Share one target validator with route                           [risk+verification]
       files:    internal/cmd/deferred_target.go (new)
                 internal/cmd/deferred_target_test.go (new)
                 internal/cmd/deferred.go
       covers:   c-7
       desc:     `validDeferredTarget(root, slug)` — accept if a phase dir
                 exists OR the slug is in the CURRENT milestone's phases array;
                 otherwise error naming both remedies. Wired into the existing
                 `deferred route --target` ahead of the spec load.
       contract: - `route --target <slug in current milestone.phases, no phase
                   dir>` succeeds; deleting the milestone-array branch fails
                   this test.                                          [risk]
                 - `--target <scaffolded phase absent from the milestone
                   array>` is accepted — the arm a milestone-only check would
                   wrongly reject.                           [verification]
                 - `route --target typo-slug` errors and the message contains
                   both the "scaffold a phase" and "add to the current
                   milestone's phases array" remedies; a bare "unknown target"
                   fails the grep.                                     [risk]
                 - a rejected route leaves the milestone toml byte-identical:
                   never auto-appended to `phases`.           [risk+verification]
                 - a rejected route leaves the source spec.toml byte-identical
                   AND makes no board HTTP call — validation precedes both the
                   save and any mirror.                       [risk+verification]
                 - `--target _project` is rejected (neither a phase dir nor a
                   roadmap entry) — the store cannot become a routing
                   destination.                                        [risk]

Wave 2 (depends wave 1)
  t-3  Key the board backlog on the deferred id                        [risk+mvp+verification]
       files:    internal/cmd/issue.go
                 internal/cmd/issue_backlog_id_test.go (new)
                 internal/board/board.go
       covers:   c-8
       depends:  t-1
       desc:     Replace syncBacklog's `someday:<source>#<idx>` key with
                 `someday:id:<id>`; backfill a missing id (crypto/rand hex,
                 collision-checked against existing ids) into the owning
                 spec/store before keying, and remap an existing legacy
                 index-keyed board.json entry onto the new key. Extract the
                 create/update loop into `pushBacklogItems(ctx, version,
                 items)` as the single seam t-6 consumes.
       contract: - two someday items in one phase, both board-linked; delete
                   index 0 and re-run backlog-sync: the server sees an
                   UpdateIssue for the survivor's OWN issue key and no request
                   whose title is the survivor's text against the deleted
                   item's key (per-issue PATCH bodies inspected). Index-keyed
                   code fails this — it is c-8.               [risk+verification]
                 - board.json holding only the legacy `someday:<phase>#0` key
                   produces zero CreateIssue calls on the next sync — migrated,
                   not orphaned into a duplicate live issue. [risk+verification]
                 - an id-less spec-authored `[[deferred]]` gains an `id` in its
                   spec.toml after one sync, and the same id on the second run
                   (no churn, no second issue).              [all three]
                 - two items backfilled in the same run get different ids (a
                   collision silently merges two board links).         [risk]
                 - a routed item still gets no board issue from backlog-sync —
                   the routed filter is untouched (board-sync-truth's remit).
                                                                       [risk]
                 - `TestIssueBacklogSyncYouTrack{Idempotent,EpicMode,AgileMode}`
                   pass unchanged after the extraction. [verification]

  t-4  Add `dross deferred add` with home selection                    [all three]
       files:    internal/cmd/deferred_add.go (new)
                 internal/cmd/deferred_add_test.go (new)
                 internal/cmd/deferred.go
                 README.md
       covers:   c-1, c-2, c-3
       depends:  t-1, t-2
       desc:     `deferred add "<text>" [--why] [--target]`. Home per locked
                 storage_home: current phase's spec.toml, else `_project`.
                 Assigns a stable id; validates `--target` through t-2's helper
                 BEFORE any write; appends and saves. README's deferred row
                 gains `add`.
       contract: - with `state.current_phase = alpha`, add appends to
                   alpha/spec.toml and `list --json` immediately shows
                   source=alpha at the next index; a project-store write fails
                   the source assertion (c-1 + storage_home).          [risk]
                 - with no current phase, add exits 0 and the item lists under
                   source `_project`; a non-nil error fails the test — c-2's
                   "never refuses for want of a home".       [all three]
                 - current phase set but its spec.toml absent: add exits 0, the
                   item lands in `_project`, stdout names the fallback, and NO
                   spec.toml appears under that phase dir.    [risk+verification]
                 - `--why` omitted: the written TOML contains no `why =` line;
                   `--why "x"` round-trips through `list --json`.      [risk]
                 - `add "t" --target beta` in one command: `list --routed
                   --target beta` includes it, `list --someday` excludes it.
                                                              [risk+verification]
                 - `add "t" --target typo` errors with error text BYTE-IDENTICAL
                   to the same rejection from `route` (one assertion comparing
                   both outputs, so the two paths cannot drift) and the
                   spec/store file is byte-identical afterwards — no orphan item
                   with a dead target.                        [verification+risk]
                 - `add ""` is rejected instead of appending a blank
                   `[[deferred]]`.                            [risk+verification]
                 - two adds into the same phase produce two different non-empty
                   ids.                                      [verification]
                 - README's deferred row reads
                   `dross deferred {list,route,unroute,dismiss,add}` (docText
                   assertion on the command table).          [verification]

  t-5  Address the project store from every verb                       [risk]
       files:    internal/cmd/deferred.go
                 internal/cmd/deferred_store_addressing_test.go (new)
       covers:   c-4
       depends:  t-1
       desc:     Route `route`, `unroute` and `dismiss` path resolution through
                 `deferredStore` so `_project <idx>` behaves identically to
                 `<phase> <idx>`; keep list's filters correct for a store item
                 with no source phase.
       contract: - `route _project 0 --target beta` rewrites
                   `.dross/deferred.toml` and leaves every phases/*/spec.toml
                   byte-identical; any verb still building its path from
                   phase.Dir fails on a missing
                   `.dross/phases/_project/spec.toml`.        [risk+verification]
                 - `unroute _project 0` then `dismiss _project 0` round-trip the
                   same item, and `dismiss` on a still-routed store item is
                   refused with the same "un-route before dismissing" error.
                                                                       [risk]
                 - `dismiss _project 7` against a 1-item store errors with the
                   same `index 7 out of range` shape as a phase source, naming
                   `_project` and the store's item count, and writes nothing —
                   no panic, no silent append.                [risk+mvp+verification]
                 - `list --milestone v1.3` excludes `_project` items (no source
                   phase in the array) while the default listing includes them.
                                                                       [risk]
                 - a dismissed `_project` item is hidden from the default list
                   and from `--someday`, and shown by `--dismissed` — the store
                   obeys the same three-state filter as a spec.  [risk+verification]

Wave 3 (depends t-3, t-4)
  t-6  Mirror an added item to the board, best-effort                  [risk+verification]
       files:    internal/cmd/deferred_add.go
                 internal/cmd/deferred_board_test.go (new)
       covers:   c-6
       depends:  t-3, t-4
       desc:     After the local save succeeds, push the item through t-3's
                 `pushBacklogItems` seam and record its id→issue key in
                 board.json. Board failure warns and returns nil; disabled board
                 or no current milestone is a silent no-op.
       contract: - exactly ONE create for `[someday] <text>` after `add`; zero
                   calls fails c-6's "in the same command", more than one fails
                   the single-item scope.                      [risk+verification]
                 - server returning 500: `add` exits 0, prints a warning naming
                   the board failure, and the item is still in the local TOML —
                   a returned error fails the locked board_push decision.
                                                              [all three]
                 - the local write happens BEFORE the push: with a 500ing
                   server, the item's presence in the TOML proves ordering.
                                                                       [risk]
                 - `board.enabled = false`: the fake server's handler `t.Error`s
                   on any request, and the item is still written. [verification]
                 - `state.current_milestone` empty: zero board calls, exit 0,
                   item local, and no output that reads like a failure.
                                                              [risk+verification]
                 - after a successful add, board.json maps `someday:id:<id>` to
                   the returned key, and a following `backlog-sync` issues an
                   UpdateIssue rather than a second CreateIssue — add and sync
                   share one key space.                        [risk+verification]

Wave 4 (depends t-4, t-5, t-6)
  t-7  File the two homeless findings, strip handoff                   [all three]
       files:    .dross/handoff.md
                 .dross/phases/deferred-add-command/spec.toml
                 internal/cmd/deferred_add_test.go
                 internal/cmd/deferred_homeless_repo_test.go (new)
                 assets/prompts/pause.md
       covers:   c-5
       depends:  t-4, t-5, t-6
       desc:     `make install` first (rule r-01), then file both findings with
                 the real verb against the live board: the test-suite
                 hermeticity gap bare (someday), and the verify
                 survivor-vocabulary / `unclassified_in_scope` mismatch with
                 `--target mutation-score-truth`. Delete both bullets from
                 handoff.md's Open loops. Teach `assets/prompts/pause.md` to
                 file a homeless finding with `dross deferred add` instead of
                 parking it under "Open loops".
       contract: - a hermetic regression test replays both findings' shapes on a
                   fixture — one bare `add`, one `add --target <milestone-array-
                   only slug>` — and asserts both land with the right target;
                   the second is the only end-to-end exercise of t-2's
                   milestone-array branch through `add` (verified: mutation-
                   score-truth is in v1.3's phases array with no phase dir).
                                                                       [risk]
                 - a repo-level test over this repo's TRACKED .dross (pattern of
                   survivor_backlog_repo_test.go) fails if collectDeferred
                   yields no entry naming the hermeticity gap, and fails if no
                   entry carries target `mutation-score-truth` — re-parking
                   either finding instead of filing it turns CI red.
                                                              [mvp+verification]
                 - the handoff.md side is asserted by OBSERVED CLI output at
                   execution time, not by a Go test: `.dross/handoff.md` is
                   gitignored (.gitignore:12), so a test reading it is both
                   vacuous in CI and a re-commission of the exact fault being
                   filed.                                   [risk, evidence-backed]
                 - `dross deferred list --target mutation-score-truth` names the
                   survivor-vocabulary finding and `list --someday` names the
                   hermeticity gap, observed against the real repo.     [risk]
                 - `dross validate` passes after both writes: proof the
                   `mutation-score-truth` target is not dangling.       [risk]
                 - `TestPromptsTeachNoBrokenInvocation` still passes over the
                   edited pause.md.                          [verification]
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 `add` + `--why`, visible in `list` | t-4 |
| c-2 lands somewhere with no current phase | t-1 (store), t-4 (fallback) |
| c-3 `--target` files+routes in one command | t-4 |
| c-4 same `<phase> <idx>` handle for all four verbs | t-1, t-5 |
| c-5 two homeless findings filed, handoff stripped | t-7 |
| c-6 board mirror in the same command, warn-on-failure | t-6 |
| c-7 `route --target` rejects unknown slugs on add's rule | t-2 |
| c-8 board link survives an earlier item's removal | t-3 |

8/8. Locked decisions: storage_home (t-1/t-4), target_validation (t-2),
board_push (t-6), deferred_identity (t-1's no-`id`-in-JSON assertion).

## Disagreements

### D1 — How the board mirror is built: extracted seam vs `syncBacklog` wholesale
- **risk / verification:** extract the create/update loop out of `syncBacklog`
  (`pushBacklogItems`) and have `add` push exactly its own item.
- **mvp:** call `syncBacklog(ctx, currentMilestone)` wholesale — ~15 lines, no
  refactor, no second definition of "what a backlog item looks like"; accepts
  that `add` may also mirror other pending backlog items ("they were due
  anyway").
- **Provisional default: the extracted seam (t-3 → t-6).** Two reasons beyond
  the 2-1 split: `syncBacklog` also creates issues for every unscaffolded
  roadmap slug (issue.go:344) and prints `backlog <version> -> N created`, so
  one `deferred add` would emit unrelated board writes; and verification's
  "exactly ONE create" contract — the only contract that actually pins c-6's
  "in the same command" to the new item — is unprovable under the wholesale
  call.
- **Why it matters:** it decides whether `deferred add` has a bounded,
  testable board footprint or an open-ended one, and it is the difference
  between c-6 being verified and merely exercised.

### D2 — Does `add --target X` push to the board at all?
- **risk:** yes, unconditionally. c-6 has no exception for routed items; the
  resulting issue simply isn't reconciled by later syncs, which is the spec's
  own `[[deferred]]` (board-sync-truth's remit).
- **mvp:** no, by construction — `syncBacklog` skips `Target != ""`
  (issue.go:360), so a routed add mirrors nothing. Reads c-6 as "goes through
  the same mirror path as any someday item".
- **verification:** silent; its t-5 contracts all use unrouted adds.
- **Provisional default: push regardless of target (risk).** c-6's text is
  unconditional, and the spec's deferred item scopes the *syncBacklog* filter
  to board-sync-truth, not `add`'s behaviour.
- **Why it matters:** under mvp's reading, `add "x" --target y` silently
  produces no board issue — a user-visible hole in c-6 that no test would
  catch, because every test would use the someday path.

### D3 — Legacy index-keyed board.json entries: migrate or duplicate?
- **risk / verification:** remap an existing `someday:<phase>#<idx>` entry onto
  the new id key; verification calls the migration "the difference between the
  letter and the point of c-8".
- **mvp:** skip it — `.dross/board.json` has no live index-keyed links, so a
  migration verb is "dead code on arrival"; accepts one duplicate issue per
  item on the first sync in any repo that *did* have them.
- **Provisional default: migrate (t-3).** mvp's premise checks out for this repo
  (board.json currently has no `backlog` key at all), but the dogfood target and
  any other adopter can hold live index keys, and the failure mode is duplicate
  issues on a live tracker — silent and manual to clean up. The remap is a few
  lines inside code t-3 is already rewriting.
- **Why it matters:** mvp is right that it costs something and buys this repo
  nothing today; if the lead decides dross has exactly one user with one board,
  dropping the remap is a legitimate simplification of t-3.

### D4 — Which readers learn about the project store
- **verification:** all of them — `collectDeferred`, `repointDeferredTarget`
  (phase rename) and `validate`'s dangling-target check — because a store
  invisible to rename/validate is "the same class of silently-unreachable
  destination c-7 exists to kill".
- **mvp:** explicitly rejects both. No criterion names rename or validate, and
  c-4 lists exactly four verbs; flags it as the known follow-up instead.
- **risk:** touches `validate.go` only for the reserved-slug collision guard,
  not for store dangling targets; silent on rename.
- **Provisional default: verification's wider version, folded into t-1.**
  Confirmed against the code: `validate.go:87` iterates phase dirs only and
  `repointDeferredTarget` (deferred.go:313) walks `phase.List` only, so both
  are genuinely blind to the store as written — and both are a few lines.
- **Why it matters:** it is the one place the merged plan widens a task past
  its criteria. If the lead wants t-1 kept to c-2/c-4, cut the rename and
  validate-dangling contracts and take mvp's follow-up note instead — but then
  `phase rename` can silently orphan a store entry's target.

### D5 — A repo-level CI gate for c-5
- **mvp / verification:** a `deferred_homeless_repo_test.go` over the real
  `.dross` that fails if either finding is missing *and* fails if
  `handoff.md` still contains the marker text — so re-parking a finding in
  handoff turns CI red.
- **risk:** refuses on principle. The first finding being filed is "a test read
  the real repo's gitignored state and only CI caught it"; reproducing that
  pattern to verify it is self-defeating. Splits into a hermetic fixture replay
  plus observed CLI output.
- **Provisional default: split the gate — keep it, drop the handoff half.**
  Decided on evidence rather than vote: `.dross/handoff.md` is gitignored
  (`.gitignore:12`), so the handoff assertion is vacuous in CI and is exactly
  risk's objection; `spec.toml` is tracked, and `survivor_backlog_repo_test.go`
  already establishes the tracked-`.dross` audit pattern, so the
  "both findings exist, one targets mutation-score-truth" half is safe and
  earns its keep.
- **Why it matters:** the full mvp/verification gate would pass green in CI
  while asserting nothing, which is worse than no gate — it would read as
  enforcement.

### D6 — Task shape for `add`: one task or three
- **mvp:** one task (t-3) covers c-1, c-2, c-3, c-6 and c-7 — the validator, the
  verb and the board mirror are one file's worth of change.
- **risk / verification:** split across the validator (t-2), the verb (t-4) and
  the mirror (t-6), each with its own commit and gate.
- **Provisional default: split (risk's 7-task graph).** The board mirror depends
  on t-3's seam, which the single task cannot depend on without also owning
  `issue.go`; and a five-criteria commit makes a wave-2 failure unattributable.
- **Why it matters:** it is the whole 7-vs-4 task delta. If the lead wants
  fewer commits, the cheapest legitimate merge is folding t-5 into t-1 (as
  verification does) — not folding t-6 into t-4.

### D7 — Reserved slug name
- **risk / verification:** `_project`. **mvp:** `_unphased`.
- **Provisional default: `_project`** (2-1; both rationales are identical and
  correct — `phase.Slugify` drops a leading `_` because `b.Len() == 0` at
  phase.go:194, so no dross-created phase can ever collide).
- **Why it matters:** it is a permanent user-facing argv handle
  (`dross deferred dismiss _project 0`) and appears in `list` output, so it is
  cheap now and awkward later. `_unphased` is arguably the more honest name for
  "no phase was current"; `_project` matches the locked decision's own
  "project-level store" wording.

### D8 (structural, noted not blocking) — `pause.md`
Only verification edits the prompt that *writes* handoff.md. No criterion names
it, so it is scope beyond c-5; it is kept in t-7 because without it the next
`/dross-pause` re-parks the next homeless finding under "Open loops" and c-5
gets re-earned by hand. Cut it if the lead wants t-7 strictly criterion-scoped.
