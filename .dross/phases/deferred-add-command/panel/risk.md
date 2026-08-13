# risk lens — deferred-add-command

Lens: **failure modes drive the graph.** Every task owns exactly one way this
verb can lie or lose data: no home, a typo'd destination, an index that shifts
under a board link, a network blip that eats the finding.

```
Phase deferred-add-command — 7 tasks across 4 waves

Wave 1
  t-1  Add deferred id + project-level store
       files:    internal/phase/phase.go
                 internal/cmd/deferred_store.go (new)
                 internal/cmd/deferred_store_test.go (new)
                 internal/cmd/validate.go
       covers:   c-2, c-4
       desc:     Add `ID string toml:"id,omitempty"` to phase.Deferred and an
                 `ID string json:"-"` field to deferredEntry. Add the reserved
                 source slug `_project` and `.dross/deferred.toml` (a
                 phase.Spec-shaped store), a `deferredStore(root, source)`
                 resolver every verb routes its path through, and a collision
                 guard: collectDeferred skips a `phases/_project` directory and
                 validate reports it as a reserved-slug problem.
       contract: - `.dross/deferred.toml` round-trips one `[[deferred]]` with
                   id+text+target; dropping the `id` toml tag fails the
                   reload-and-compare assertion.
                 - `deferredStore(root, "_project")` returns
                   `.dross/deferred.toml`; if it falls through to phase.Dir the
                   returned-path assertion fails on
                   `.dross/phases/_project/spec.toml`.
                 - `deferredStore(root, "nope")` errors for a slug with no phase
                   dir instead of returning a path that would be created on save.
                 - a hand-made `.dross/phases/_project/spec.toml` fails
                   `dross validate` with a reserved-slug problem, and its items
                   do NOT appear in `deferred list` — without the skip, two
                   sources share the slug and index 0 is ambiguous.
                 - `deferred list --json` output unmarshalled into
                   `map[string]any` has no `id` key (locked
                   deferred_identity: the id is internal).

  t-2  Validate deferred targets on one rule
       files:    internal/cmd/deferred_target.go (new)
                 internal/cmd/deferred_target_test.go (new)
                 internal/cmd/deferred.go
       covers:   c-7
       desc:     `validDeferredTarget(root, slug)` — accept if a phase dir
                 exists or the slug is in the CURRENT milestone's phases array;
                 otherwise error naming both remedies. Wire it into the existing
                 `deferred route --target` ahead of the spec load.
       contract: - `route --target mutation-score-truth` succeeds on a slug with
                   no phase dir that IS in the current milestone's phases array;
                   deleting the milestone-array branch fails this test.
                 - `route --target typo-slug` errors and the message contains
                   both "phase" and "milestone" remedies — a bare "unknown
                   target" fails the message grep.
                 - a rejected `route --target typo-slug` leaves the milestone
                   toml byte-identical: never auto-appended to `phases`.
                 - a rejected route leaves the source spec.toml byte-identical
                   (validation precedes the spec load and save).
                 - `--target _project` is rejected (the reserved slug is neither
                   a phase dir nor a roadmap entry) — proves the store cannot
                   become a routing destination.

Wave 2 (depends t-1 / t-2)
  t-3  Key the board backlog on the deferred id
       files:    internal/cmd/issue.go
                 internal/cmd/issue_backlog_id_test.go (new)
                 internal/board/board.go
       covers:   c-8
       depends:  t-1
       desc:     Replace syncBacklog's `someday:<phase>#<idx>` key with
                 `someday:id:<id>`; backfill a missing id (crypto/rand hex,
                 collision-checked against every existing id) into the owning
                 spec/store before keying, and remap an existing legacy
                 index-keyed board.json entry onto the new key. Extract the
                 single-item create/update into a helper t-6 reuses.
       contract: - two someday items in one phase, both board-linked; delete
                   index 0 and re-run backlog-sync: the httptest server sees an
                   UpdateIssue for the surviving item's OWN issue key and no
                   request whose title is the survivor's text against the
                   deleted item's key. Index-keyed code fails this — it is c-8.
                 - a board.json holding only the legacy `someday:<phase>#0` key
                   produces zero CreateIssue calls on the next sync (migrated,
                   not orphaned into a duplicate issue).
                 - an id-less spec-authored `[[deferred]]` gains an `id` in its
                   spec.toml after one backlog-sync, and the same id on the
                   second run (no churn, no second issue).
                 - two items backfilled in the same run get different ids (a
                   collision would silently merge two board links).
                 - a routed item still gets no board issue from backlog-sync
                   (the routed filter is untouched — board-sync-truth's remit).

  t-4  Add `dross deferred add` with home selection
       files:    internal/cmd/deferred_add.go (new)
                 internal/cmd/deferred_add_test.go (new)
                 internal/cmd/deferred.go
                 README.md
       covers:   c-1, c-2, c-3
       depends:  t-1, t-2
       desc:     `deferred add "<text>" [--why] [--target]`. Home per locked
                 storage_home: current phase's spec.toml, else `_project`.
                 Assigns a stable id, validates --target via t-2 BEFORE any
                 write, appends and saves. README command row gains `add`.
       contract: - with `state.current_phase = alpha`, add appends to
                   alpha/spec.toml and `list --json` immediately shows
                   source=alpha, index=len(prior)+0; a project-store write fails
                   the source assertion (c-1 + storage_home).
                 - with no current phase, add exits 0 and the item lists under
                   source `_project`; a non-nil error fails the test — c-2's
                   "never refuses for want of a home".
                 - current phase set but its spec.toml absent (created, not yet
                   specced): add exits 0, the item lands in `_project`, and NO
                   spec.toml appears under that phase dir.
                 - `--why` omitted: the written TOML contains no `why =` line;
                   `--why "x"` round-trips through `list --json`.
                 - `add "t" --target beta` in one command: `list --routed
                   --target beta` includes it (c-3).
                 - `add "t" --target typo` returns an error AND the spec/store
                   file is byte-identical afterwards — no orphan item with a
                   dead target.
                 - `add ""` (empty text) is rejected; an empty-text item is
                   unaddressable prose on both the list and the board.

  t-5  Address the project store from every verb
       files:    internal/cmd/deferred.go
                 internal/cmd/deferred_store_addressing_test.go (new)
       covers:   c-4
       depends:  t-1
       desc:     Route `route`, `unroute` and `dismiss` path resolution through
                 `deferredStore` so `_project <idx>` works identically to
                 `<phase> <idx>`; keep list's filters behaving for a store item
                 with no source phase.
       contract: - `route _project 0 --target beta` sets target in
                   `.dross/deferred.toml`; any verb still building its path from
                   phase.Dir fails on a missing
                   `.dross/phases/_project/spec.toml`.
                 - `unroute _project 0` then `dismiss _project 0` round-trip the
                   same item, and `dismiss` on a still-routed store item is
                   refused with the same "un-route before dismissing" error.
                 - `dismiss _project 7` against a 1-item store errors with the
                   same `index 7 out of range` shape as a phase source (no
                   panic, no silent append).
                 - `list --milestone v0.5` excludes `_project` items (no source
                   phase in the array) while the default listing includes them.
                 - a dismissed `_project` item is hidden from the default list
                   and shown by `--dismissed` — the store obeys the same
                   three-state filter as a spec.

Wave 3 (depends t-3, t-4)
  t-6  Mirror an added item to the board, best-effort
       files:    internal/cmd/deferred_add.go
                 internal/cmd/deferred_board_test.go (new)
       covers:   c-6
       depends:  t-3, t-4
       desc:     After the local save succeeds, push the item through t-3's
                 single-item helper and record its id→issue key in board.json.
                 Board failure warns on stdout and returns nil; disabled board
                 or no current milestone is a silent no-op.
       contract: - httptest server counts exactly one create for `[someday]
                   <text>` after `add`; zero calls fails c-6's "in the same
                   command".
                 - server returning 500: `add` exits 0, prints a warning line
                   naming the board, and the item is still in the local TOML —
                   a returned error fails the locked board_push decision.
                 - `board.enabled = false`: the server's request counter stays
                   at 0 and no warning is printed.
                 - `state.current_milestone` empty: no HTTP call, exit 0, item
                   local — the milestone entity has nothing to attach to.
                 - after a successful add, board.json carries
                   `someday:id:<id>` → issue key, and a following `backlog-sync`
                   issues an UpdateIssue rather than a second CreateIssue.
                 - the local write happens BEFORE the push: with a server that
                   500s, the item's presence in the TOML proves ordering.

Wave 4 (depends t-4, t-5, t-6)
  t-7  File the two homeless findings, strip handoff
       files:    .dross/handoff.md
                 .dross/phases/deferred-add-command/spec.toml
                 internal/cmd/deferred_add_test.go
       covers:   c-5
       depends:  t-4, t-5, t-6
       desc:     `make install` (rule r-01), then file the hermeticity gap with
                 no --target (someday) and the verify survivor-vocabulary
                 mismatch with `--target mutation-score-truth`, using the real
                 verb against the live board. Delete both bullets from
                 handoff.md's Open loops.
       contract: - a hermetic regression test replays both findings' exact
                   shapes on a fixture — one bare `add`, one `add --target
                   <milestone-array-only slug>` — and asserts both land with the
                   right target; the second is the ONLY end-to-end exercise of
                   t-2's milestone-array branch through `add` (mutation-score-
                   truth has no phase dir).
                 - `dross deferred list --target mutation-score-truth` names the
                   survivor-vocabulary finding, and `list --someday` names the
                   hermeticity gap, observed against the real repo at execution
                   time (not asserted by a Go test — reading the real .dross
                   from a test is the very hermeticity fault being filed).
                 - `dross validate` passes after the two writes: proof the
                   `mutation-score-truth` target is not dangling.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 `deferred add` + `--why`, visible in list | t-4 |
| c-2 lands somewhere with no current phase | t-1, t-4 |
| c-3 `--target` files+routes in one command | t-4 |
| c-4 same `<phase> <idx>` handle for all four verbs | t-1, t-5 |
| c-5 the two homeless findings filed, handoff stripped | t-7 |
| c-6 board mirror in the same command, warn-on-failure | t-6 |
| c-7 `route --target` rejects unknown slugs on add's rule | t-2 |
| c-8 board link survives an earlier item's removal | t-3 |

8/8 criteria covered. Locked decisions honoured: storage_home (t-1/t-4),
target_validation (t-2), board_push (t-6), deferred_identity (t-1 — `json:"-"`,
no CLI surface for the id).

## Judgment calls

- **Reserved slug `_project`, not `(project)` or `project`.** `phase.Slugify`
  can never emit `_`, so no dross-created phase can collide with it, and it
  needs no shell quoting as an argv handle. Rejected `(project)` (quoting) and
  `project` (a real phase could legitimately be named that).
- **The id is backfilled by `backlog-sync`, not only assigned by `add`.** c-8
  says "a deferred item's" link, not "an added item's"; every spec-authored item
  predating this phase has no id, so an add-only assignment leaves the whole
  existing backlog index-keyed and c-8 false for it. Cost accepted: a sync
  command now writes to spec.toml (announced on stdout). Rejected: id-on-add
  only, which would have kept sync read-only but left the bug live.
- **`add --target X` still pushes to the board**, even though `syncBacklog`
  skips routed items. c-6 is unconditional and non-negotiable; the resulting
  issue simply isn't reconciled by later syncs, which is the spec's own deferred
  item (board-sync-truth's remit). Rejected: mirroring syncBacklog's routed
  filter inside `add`, which would read as c-6 being only half-implemented.
- **A current phase with no spec.toml falls back to `_project`** rather than
  scaffolding a minimal spec. A half-spec would collide with the `/dross-spec`
  write that owns that file. Satisfies c-2 without inventing a `[phase]` block.
- **Validator scoped to the CURRENT milestone's array** per locked
  target_validation, even though `dross validate` accepts a slug from ANY
  milestone's array. add's rule is strictly narrower, so nothing add accepts can
  be flagged later by validate — the divergence is safe in the one direction
  that matters.
- **t-3 owns `internal/cmd/issue.go` alone** and extracts the single-item
  create/update helper; t-6 only consumes it. Splitting board work across two
  tasks editing the same function was the alternative and it makes the wave-3
  failure mode (partial push) impossible to attribute.
- **t-7 sits in its own wave behind the board push** instead of running right
  after `add` exists. Filing the two real findings against the live board IS the
  acceptance run for c-1/c-3/c-6 together; doing it before t-6 would prove only
  the local half.
- **No Go test reads the real `.dross`** in t-7. The first finding being filed
  is precisely "a test read the real repo's gitignored state and only CI caught
  it" — reproducing that pattern to verify it would be self-defeating, so the
  hermetic replay and the observed CLI output are split deliberately.
