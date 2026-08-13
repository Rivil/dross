# mvp lens — deferred-add-command

Phase deferred-add-command — 4 tasks across 3 waves

Wave 1
  t-1  Key backlog sync by a stable deferred id
       files:    internal/phase/phase.go, internal/cmd/issue.go,
                 internal/phase/phase_test.go, internal/cmd/issue_test.go
       covers:   c-8
       desc:     Add `ID string `toml:"id,omitempty"`` to phase.Deferred. syncBacklog
                 keys someday items as `someday:<id>` instead of
                 `someday:<source>#<index>`, and stamps+saves a generated id onto any
                 entry that lacks one before mirroring it.
       contract: two someday items in one phase, both synced; delete index 0, re-run
                 backlog-sync — the surviving item's board.json key (`someday:<id>`) is
                 unchanged and its issue is updated with its own text, and no issue
                 previously created for the removed item is re-titled with the
                 survivor's text. Separately: a spec [[deferred]] entry with no `id`
                 has one written back into spec.toml by the first sync, and a second
                 sync creates nothing new.
       depends:  —

  t-2  Address a project-level deferred store
       files:    internal/cmd/deferred.go, internal/cmd/deferred_test.go
       covers:   c-4
       desc:     Reserved source slug `_unphased` backed by `.dross/deferred.toml`,
                 stored in phase.Spec shape so phase.LoadSpec/Save round-trip it. One
                 `deferredStorePath(root, source)` helper replaces the inline
                 `phases/<id>/spec.toml` join in route/dismiss/unroute, and
                 collectDeferred appends the store's entries with Source `_unphased`.
       contract: with two entries in .dross/deferred.toml, `deferred list --json`
                 returns them as source `_unphased` idx 0/1; `route _unphased 1
                 --target beta`, `unroute _unphased 1`, `dismiss _unphased 1` each
                 mutate .dross/deferred.toml and are visible to the next `list`;
                 `route _unphased 9` fails with the same "index 9 out of range" shape a
                 phase spec gives, and writes nothing.
       depends:  —

Wave 2 (depends t-1, t-2)
  t-3  Add `dross deferred add` with --why/--target
       files:    internal/cmd/deferred.go, internal/cmd/deferred_test.go, README.md
       covers:   c-1, c-2, c-3, c-6, c-7
       desc:     New `add "<text>" [--why] [--target]` subcommand: writes into
                 state.current_phase's spec.toml, else (unset, or that spec.toml
                 missing) into the `_unphased` store; stamps a stable id; validates
                 --target via one `validDeferredTarget` helper that deferredRoute now
                 calls too; then best-effort board mirror (openBoard → skip when
                 disabled or state.current_milestone empty → syncBacklog → warn on
                 error). README's `dross deferred {list,route,unroute,dismiss}` row
                 gains `add`.
       contract: (a) `add "x" --why "y"` then `list --json` returns the entry with
                 text x, why y, empty target — same run, no second command;
                 (b) with current_phase unset the entry is in .dross/deferred.toml
                 under source `_unphased`, and the command exits 0 (never "no current
                 phase" error); (c) `add "x" --target <existing-phase-dir>` and
                 `--target <slug-only-in-current-milestone.phases>` both succeed with
                 target set, while `--target nope` exits non-zero, names both ways to
                 make a target valid, appends nothing to any spec.toml, and does NOT
                 append `nope` to milestone.phases; (d) `route <phase> 0 --target nope`
                 is rejected by the same rule and leaves the entry's old target intact;
                 (e) against a stub board server, `add` creates the backlog issue in
                 the same invocation, and when the stub returns 500 the command still
                 exits 0, prints a warning, and the spec.toml entry is present.
       depends:  t-1, t-2

Wave 3 (depends t-3)
  t-4  File the two homeless findings, prune handoff
       files:    .dross/phases/deferred-add-command/spec.toml, .dross/handoff.md,
                 internal/cmd/deferred_homeless_repo_test.go
       covers:   c-5
       desc:     Run the new verb twice for real: the test-suite hermeticity gap
                 (someday, no target) and the verify survivor-vocabulary mismatch
                 (`--target mutation-score-truth`); delete both bullets from
                 handoff.md's Open loops. A repo-level gate test pins the migration.
       contract: over this repo's real .dross (like survivor_backlog_repo_test.go), the
                 test fails if collectDeferred yields no entry mentioning "hermetic",
                 fails if no entry has target `mutation-score-truth`, and fails if
                 .dross/handoff.md still contains "hermeticity" or
                 "unclassified_in_scope" — so re-adding either bullet to handoff
                 instead of filing it turns CI red.
       depends:  t-3

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-3 |
| c-2 | t-2 (the store), t-3 (the fallback choice) |
| c-3 | t-3 |
| c-4 | t-2 |
| c-5 | t-4 |
| c-6 | t-3 |
| c-7 | t-3 |
| c-8 | t-1 |

8/8 criteria covered.

## Judgment calls

- **Board mirror folded into t-3, not its own task.** It is ~15 lines inside add's
  RunE in the same file; a separate task would exist only to own a stub-server test,
  which t-3's contract already carries. Rejected: a 5th task for c-6.
- **c-6 reuses `syncBacklog(ctx, currentMilestone)` wholesale rather than a
  single-item push.** It is already idempotent and already the code that decides what
  a backlog item looks like; a bespoke one-item create would be a second, drifting
  definition. Cost: `add` may also mirror other pending backlog items — accepted, they
  were due anyway.
- **Routed adds are mirrored only insofar as syncBacklog mirrors them (i.e. not).**
  The spec's own `[[deferred]]` assigns "routed items never reach the board" to
  board-sync-truth, so c-6 is read as "goes through the same mirror path as any
  someday item", not "invent routed-item board support here".
- **Project store is phase.Spec-shaped TOML at `.dross/deferred.toml`, not a new
  schema.** Reusing LoadSpec/Save means route/unroute/dismiss need a path swap and
  nothing else — a new `deferredStore` type would duplicate load/save/round-trip tests
  for zero criterion. Reserved slug `_unphased` because phase.Slugify can only emit
  `[a-z0-9-]`, so it cannot collide with a real phase dir.
- **t-1 stamps ids during backlog-sync instead of a one-shot `deferred migrate`.**
  board.json's `backlog` map is currently `{}` in this repo, so there are no live
  index-keyed links to re-point; a migration verb would be dead code on arrival.
  Consequence accepted: any repo that *did* have index-keyed backlog links gets one
  duplicate issue per item on the first sync after upgrade.
- **c-8 keys by id for every item, not just added ones.** Restricting the fix to
  new items would leave the criterion ("a deferred item's board link") false for the
  items that actually exist today.
- **`repointDeferredTarget` (phase rename) and `dross validate` are left blind to
  `_unphased`.** No criterion names rename or validate, and c-4 lists exactly four
  verbs. Flagging it as the known follow-up rather than smuggling it in.
- **`add` with current_phase set but no spec.toml falls back to `_unphased` rather
  than scaffolding a spec.** c-2 demands the command never refuse for want of a home;
  writing a spec.toml is /dross-spec's job.
