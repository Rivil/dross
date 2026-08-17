# Panel draft — verification lens (test contracts first)

Phase deferred-add-command — 6 tasks across 4 waves

```
Wave 1
  t-1  Make the reserved project store addressable
       files:    internal/phase/phase.go
                 internal/cmd/deferred_store.go
                 internal/cmd/deferred.go
                 internal/cmd/validate.go
                 internal/cmd/deferred_store_test.go
       covers:   c-4
       contract: .dross/deferred.toml entries surface in `deferred list --json` as
                 source "_project" with their own 0-based indices
       contract: `deferred route _project 0 --target alpha` rewrites
                 .dross/deferred.toml and leaves every phases/*/spec.toml
                 byte-identical (compared before/after)
       contract: `deferred dismiss _project 1` then `list --dismissed` shows it and
                 `list --someday` does not — the reserved source runs the same filters
       contract: `deferred unroute _project 0` clears the target; `route _project 9`
                 errors "out of range" naming _project and the store's item count
       contract: phase.Slugify can never emit "_project" — if slugify starts keeping
                 leading underscores, this test fails before a real phase can shadow
                 the store
       contract: `phase rename alpha alpha2` repoints a _project entry routed to
                 alpha (repointDeferredTarget now walks the store)
       contract: a bogus target in .dross/deferred.toml makes `dross validate` exit
                 non-zero naming deferred.toml — the hand-edit path is not a hole
       contract: phase.Deferred.Id round-trips through LoadSpec/Save and is absent
                 from the TOML on an entry without one (omitempty)

  t-2  Share one target validator with route
       files:    internal/cmd/deferred_target.go
                 internal/cmd/deferred.go
                 internal/cmd/deferred_target_test.go
       covers:   c-7
       contract: `deferred route alpha 1 --target no-such-phase` exits non-zero, the
                 message names BOTH remedies (scaffold the phase / add the slug to the
                 current milestone's phases array), and alpha/spec.toml is unchanged
                 on disk
       contract: `--target future-x`, a slug in the current milestone's phases array
                 with no phase directory, is ACCEPTED — the arm a directory-only check
                 would wrongly reject
       contract: `--target beta`, a scaffolded phase absent from the milestone array,
                 is accepted
       contract: an unknown target never appends the slug to any milestone.toml —
                 milestone file bytes compared before/after the rejection
       contract: validation precedes the write: after a rejected route the spec's
                 content hash is identical and no board HTTP call was made

Wave 2 (depends t-1, t-2)
  t-3  Add `dross deferred add` verb
       files:    internal/cmd/deferred.go
                 internal/cmd/deferred_add_test.go
       covers:   c-1, c-2, c-3
       depends:  t-1, t-2
       contract: with state.current_phase=alpha, `deferred add "x" --why "y"` appends
                 to alpha/spec.toml and `list --json` in the next call returns
                 {source:alpha, index:2, text:"x", why:"y", target:""}
       contract: with current_phase unset on a repo with zero phase dirs, `add "x"`
                 exits 0 and the item appears under source _project — the command has
                 no refusal path for want of a home
       contract: current_phase names a scaffolded dir with no spec.toml → the item
                 lands in _project, stdout names the fallback, and NO criteria-less
                 spec.toml is created (asserted absent)
       contract: `add "x" --target no-such-slug` produces byte-identical error text to
                 the same rejection from `route` (one assertion comparing both
                 outputs, so the two paths cannot drift), and writes nothing
       contract: `add "x" --target future-x` lands target=future-x in one command:
                 `list --target future-x` includes it, `list --someday` excludes it
       contract: `add ""` is rejected instead of appending a blank [[deferred]] entry
       contract: two adds into the same phase produce two different non-empty ids

  t-4  Key backlog items by deferred id
       files:    internal/cmd/issue.go
                 internal/cmd/issue_backlog_id_test.go
       covers:   c-8
       depends:  t-1
       contract: 3 someday entries → `backlog-sync` creates 3 issues keyed
                 someday:<id> in board.json; delete entry index 0, re-run → 0 creates
                 and the issue linked to the entry formerly at index 1 is updated with
                 ITS OWN title (per-issue PATCH bodies inspected), i.e. no issue is
                 re-pointed at another entry's text
       contract: board.json pre-seeded with the legacy key someday:01-done#0 → after
                 sync the same readable id is stored under someday:<id> and creates==0
                 (no duplicate board issue for an already-mirrored item)
       contract: an entry with no id gets one stamped into its spec.toml by sync, so
                 the following sync is already id-keyed (spec.toml on disk gained
                 `id = `)
       contract: syncBacklog's create/update loop is extracted to
                 pushBacklogItems(ctx, version, items) and syncBacklog still passes
                 the existing TestIssueBacklogSyncYouTrack{Idempotent,EpicMode,
                 AgileMode} unchanged

Wave 3 (depends t-3, t-4)
  t-5  Mirror an added item onto the board
       files:    internal/cmd/deferred.go
                 internal/cmd/issue.go
                 internal/cmd/deferred_board_test.go
       covers:   c-6
       depends:  t-3, t-4
       contract: board enabled + current_milestone set → `deferred add "x"` issues
                 exactly ONE create against the fake board and board.json maps
                 someday:<new id> to the returned readable id
       contract: a following `backlog-sync <version>` updates that issue and creates
                 nothing — add and sync share one key space
       contract: fake board returns 500 on issue create → `add` exits 0, the item is
                 in spec.toml, and the output carries a warning naming the board
                 failure (local write authoritative)
       contract: board.enabled=false → the fake server's handler is never hit (handler
                 t.Error's on any request) and the item is still written
       contract: no current milestone → zero board calls, item written, no error and
                 no warning that reads like a failure

Wave 4 (depends t-5)
  t-6  File the two homeless findings, sync docs
       files:    .dross/handoff.md
                 README.md
                 assets/prompts/pause.md
                 internal/cmd/deferred_homeless_repo_test.go
       covers:   c-5
       depends:  t-5
       contract: repo-level test over the real .dross: collectDeferred finds an entry
                 whose text names the test-suite hermeticity gap, and one whose text
                 names the verify survivor-vocabulary / unclassified_in_scope mismatch
                 with target="mutation-score-truth"
       contract: the same test asserts .dross/handoff.md contains NEITHER finding's
                 marker text — re-parking either one in handoff.md fails CI
       contract: README's deferred row reads `dross deferred {list,route,unroute,
                 dismiss,add}` (docText assertion on the command table)
       contract: assets/prompts/pause.md tells the author to file a homeless finding
                 with `dross deferred add` rather than leaving it under "Open loops",
                 and the existing TestPromptsTeachNoBrokenInvocation corpus guard
                 still passes over the edited prompt
```

Both c-5 items are filed with the shipped verb (not hand-edited into the spec):

```
dross deferred add "Test-suite hermeticity gap: a test reading the real repo's gitignored state.json passed the per-task gate and a full verify; only CI caught it. ARCHITECTURE.md carries a Test-suite hermeticity entry and nothing enforces it." --why "found by CI, not by dross — the principle exists unenforced"
dross deferred add "dross verify stdout printed 'survivors: 4 in-diff, 0 routed, 71 accepted, 0 unclassified' while the same run's verify.toml set unclassified_in_scope = 4 — one set of survivors, two vocabularies, and the stdout line reads all-clear while the gate is open." --why "not reproduced on the 08-13 re-run (0 in-diff); unfixed, not disproven" --target mutation-score-truth
```

With current_phase = deferred-add-command these land in this phase's own spec.toml,
which is the intended dogfood: t-5's board mirror fires against the live DRO board on
the first item, so c-6 gets a real-world proof and not only a fake-server one.

## Coverage

| criterion | tasks |
|---|---|
| c-1 `add "<text>"` + `--why`, visible in `list` | t-3 |
| c-2 always lands somewhere addressable | t-3 (store from t-1) |
| c-3 `--target` files and routes in one command | t-3 |
| c-4 added item indistinguishable to list/route/unroute/dismiss | t-1 (proven again end-to-end in t-3) |
| c-5 two homeless findings filed, handoff.md cleaned | t-6 |
| c-6 board mirror in the same command, warn-and-stand on failure | t-5 |
| c-7 `route --target` rejects unknown slugs on add's rule | t-2 |
| c-8 board link survives removal of an earlier item | t-4 |

8/8 criteria covered.

## Judgment calls

- Reserved source slug is `_project`, store file `.dross/deferred.toml`, shaped as a
  `phase.Spec` with `[phase] id = "_project"`. Chose spec-shaped over a bespoke schema
  so `phase.LoadSpec`/`Save` and every existing route/dismiss/unroute code path work
  unchanged — the alternative (a new store type) duplicates four verbs' worth of logic.
  Leading underscore chosen because `phase.Slugify` strips it, so no real phase can ever
  collide with the reserved slug; t-1 pins that.
- Wrote the store into every *reader* in t-1 (collect, repointDeferredTarget, validate)
  rather than only into `list`. Rejected the narrower version: `phase rename` and
  `validate` would silently skip store entries, which is the same class of
  silently-unreachable destination c-7 exists to kill.
- t-1 touches 5 files, over the stated soft cap. Kept it whole because the 5th
  (validate.go) is three lines that make the store visible to the dangling-target check;
  splitting it would ship a store that `validate` cannot see, which is a worse artefact
  than a slightly fat task.
- The shared validator honours the locked `target_validation` wording — *current*
  milestone's phases array — while `dross validate` keeps its existing all-milestones
  tolerance. Rejected unifying them: tightening validate is not in scope and would fail
  on pre-existing entries. t-2 pins the asymmetry with a test so it reads as deliberate.
- `add` with `current_phase` set but no spec.toml on disk falls back to the project
  store instead of fabricating a spec. Rejected auto-creating a criteria-less spec.toml:
  it would satisfy the locked storage_home wording but hand `dross validate` (and
  `/dross-spec`) a half-born spec.
- c-8 is delivered by an id key plus a legacy-key migration, not by an id key alone.
  Without the migration the first sync after this phase re-creates every existing
  someday item under a new key — technically not "re-pointing" but duplicating live DRO
  issues; the migration is the difference between the letter and the point of c-8.
- Board-item creation is extracted into `pushBacklogItems` in t-4 (the task that already
  owns issue.go) so t-5 calls one seam. Rejected letting `add` build its own create call:
  two creation paths is exactly how add and backlog-sync end up with divergent keys.
- t-6 is wave 4, after the board mirror, so filing the two findings exercises c-6
  against the real board rather than proving it only against httptest.
