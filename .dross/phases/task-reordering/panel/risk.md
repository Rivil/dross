# Panel draft — risk lens (failure modes drive the graph)

Failure-mode inventory this plan is built from:
- F1 bad input: unknown task, unknown anchor, anchor==self, both/neither `--before/--after`, no-op move
- F2 dependency-order corruption: task lands before a dep, or after a dependent (incl. hand-edited plans with weird waves)
- F3 execution-history violation: moving a non-pending task; landing before a done/in-progress task (incl. out-of-array-order done tasks, which execute's wave-then-id order can produce)
- F4 partial failure: a rejected move that still rewrites plan.toml (the file must stay byte-identical on any rejection)
- F5 id churn: a move that renumbers t-N or bumps task_seq, silently invalidating history/telemetry references
- F6 wave inconsistency after a legal move: adopted wave leaves a pending dependent at wave <= its dep; `dross validate` breaks
- F7 stale ordering semantics: `NextRunnable` tie-breaks by ID, ignoring array position entirely — a same-wave move today changes NOTHING about `task next`
- F8 prompt regression: the "List 3-7 outcomes" free-recall ask survives (or returns) in spec.md §2; the slate isn't gated per item
- F9 asset drift: a /dross-techdebt shim without an engine prompt (parity break); status actions line keeps pointing at the bare CLI

Each risk is owned and tested by exactly one task below.

```
Phase task-reordering — 7 tasks across 3 waves

Wave 1
  t-1  Add MoveTask splice with id stability
       files:       internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:      c-1, c-4
       depends_on:  (none)
       status:      pending
       description: Add Plan.MoveTask(id, anchor, before): splice the task to the
                    anchor-relative position in p.Task and adopt the anchor's wave
                    (locked move_wave_semantics). Own the F1 bad-input paths:
                    unknown task/anchor, anchor==self, no-op (already in place →
                    return unchanged, mirroring phase.MoveRelative). Never touch
                    any task ID or TaskSeq (F5).
       contract:    - MoveTask("t-3", "t-1", false) on a 4-task plan yields order
                      [t-1,t-3,t-2,t-4]; the test asserts every task keeps its
                      exact pre-move ID and task_seq is unchanged — an id
                      renumber or TaskSeq bump fails it (c-4)
                    - MoveTask with anchor "t-9" returns "anchor task not found:
                      t-9" and the in-memory plan deep-equals the original
                    - MoveTask of t-2 relative to itself errors; moving t-2 to
                      the position it already holds returns nil with the plan
                      unchanged

  t-2  Make NextRunnable respect array position
       files:       internal/phase/phase.go, internal/phase/phase_test.go
       covers:      c-5
       depends_on:  (none)
       status:      pending
       description: Change NextRunnable's tie-break within a wave from lowest ID
                    to lowest array index (F7 — today `task next` ignores plan
                    order entirely, so no move could ever satisfy c-5). Update any
                    existing phase_test.go pins on the id tie-break.
       contract:    - Plan ordered [t-3, t-2], both pending wave 2 with deps done:
                      NextRunnable returns t-3; if the tie-break regresses to ID
                      order the test fails by getting t-2
                    - Wave priority still wins over position: a pending wave-1
                      task listed AFTER a pending wave-2 task is still returned
                      first

  t-3  Replace spec §2 free-recall with candidate slate
       files:       assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:      c-6
       depends_on:  (none)
       status:      pending
       description: Rewrite spec.md §2: open with a proposed candidate-criteria
                    slate derived from milestone scope + CLI/context gap analysis,
                    each item gated accept/reword/drop one-per-turn per
                    _interaction.md; delete the "List 3-7 ... outcomes" ask.
                    Pin the new shape in spec_prompt_test.go (F8).
       contract:    - TestSpecPromptCandidateSlate fails if spec.md §2 still (or
                      again) contains the "List 3-7" free-recall phrase
                    - The same test fails if §2 lacks the accept/reword/drop
                      per-candidate gate or the milestone-scope + gap-analysis
                      derivation instruction
                    - Existing §2 quality-bar pins (testable/measurable pushback)
                      still pass — the bar applies to the slate items

  t-4  Ship /dross-techdebt skill, repoint status
       files:       assets/commands/dross-techdebt.md, assets/prompts/techdebt.md,
                    internal/cmd/status.go, internal/cmd/status_test.go
       covers:      c-7
       depends_on:  (none)
       status:      pending
       description: Add the dross-techdebt shim + a thin engine prompt (run
                    `dross techdebt`, read the newest .dross/techdebt/<id> report,
                    summarize findings — no agent panel). Flip actionCatalog's
                    tech-debt row from "dross techdebt" to "/dross-techdebt"
                    (F9). Makefile install globs dross-*.md, so linking is free.
       contract:    - TestCommandsPromptsParity fails if dross-techdebt.md ships
                      without assets/prompts/techdebt.md (or vice versa)
                    - status_test asserts every actionCatalog command starts with
                      "/" — the tech-debt row regressing to the bare CLI string
                      fails it
                    - The rendered actions block for a stamped techdebt store
                      contains "/dross-techdebt · last run"

Wave 2 (depends t-1)
  t-5  Reject illegal and history-breaking moves
       files:       internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:      c-2
       depends_on:  t-1
       status:      pending
       description: Guard MoveTask before it splices (F2, F3): reject when any
                    direct dep would sit at wave >= the adopted wave or at a later
                    array position, or any dependent at wave <= it / earlier
                    position; reject moving a non-pending task; reject any
                    destination with a done/in_progress task after it (locked
                    move_execution_guard). Every rejection returns an error naming
                    both ids and mutates nothing.
       contract:    - Moving t-4 before its dependency t-2 returns an error naming
                      t-4 and t-2; the in-memory plan deep-equals the original
                    - Moving t-1 after its dependent t-4 is rejected the same way
                    - Moving a done task errors "only pending tasks can be moved";
                      an in_progress task likewise
                    - With array [t-1 done, t-2 pending, t-3 done] (out-of-order
                      history), moving t-2 to any position before t-3 is rejected;
                      moving it after t-3 succeeds

Wave 3 (depends t-1, t-5)
  t-6  Re-derive pending waves after legal move
       files:       internal/phase/plan_edit.go, internal/phase/plan_edit_test.go
       covers:      c-3
       depends_on:  t-1, t-5
       status:      pending
       description: After a guard-approved move, re-derive waves for pending
                    tasks downstream of the moved task (deepest-dep-wave + 1, the
                    deriveWave rule) so no pending task sits at wave <= any dep
                    (F6). Done/in_progress waves stay frozen. Sequenced after t-5
                    so re-derivation can only ever run on the legal path.
       contract:    - Moving t-4 (wave 3, sole dep a done wave-1 task) after a
                      wave-1 anchor adopts wave 1 and its pending dependent t-6
                      re-derives from wave 4 to wave 2
                    - ValidatePlan on the post-move plan returns nil, and `dross
                      validate` over the fixture phase reports no problems
                    - A done task's wave field is asserted unchanged by the
                      re-derivation pass

  t-7  Wire dross task move CLI atomically
       files:       internal/cmd/task.go, internal/cmd/task_test.go
       covers:      c-1, c-2, c-5
       depends_on:  t-1, t-2, t-5
       status:      pending
       description: Add `dross task move <phase-id> <task-id> --before/--after
                    <other-id>` to the task command tree: resolveAnchor for flag
                    validation, MoveTask, then saveIfValid — the error path
                    returns before any Save so a rejected move never touches the
                    file (F4). E2E-proves the move→next chain.
       contract:    - A dep-violating `task move` exits non-zero and plan.toml is
                      byte-identical (file bytes captured before, compared after)
                    - `--before X --after Y` together → resolveAnchor's
                      exactly-one-flag error; neither flag → same error; no file
                      write in either case
                    - E2E: after `task move <phase> t-3 --before t-2` (same
                      wave), `task next` prints t-3 where it printed t-2 before
                      the move
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-5, t-7 |
| c-3 | t-6 |
| c-4 | t-1 |
| c-5 | t-2, t-7 |
| c-6 | t-3 |
| c-7 | t-4 |

## Judgment calls

- **NextRunnable tie-break change is its own wave-1 task, not a move side-effect** — chose changing the engine ordering (wave, then array index) because `NextRunnable` currently ignores plan order entirely (wave then lowest ID), making c-5 unsatisfiable by any move alone; rejected "renumber ids on move to force order" as a direct c-4 violation.
- **Guards live in the engine (phase pkg), CLI only adds file atomicity** — an engine-level reject-mutating-nothing protects every future caller, and the existing `saveIfValid` + atomic temp-rename Save already gives byte-identical-on-reject for free; rejected CLI-side backup/restore of plan.toml as redundant machinery.
- **Splice mechanics (t-1) and guards (t-5) are separate, sequenced tasks** — same file, so they can't share a wave, and each owns distinct failure modes (bad input/id churn vs dep order/frozen history); rejected one mega-task because a single task owning five failure modes is exactly how one of them ships untested.
- **Re-derivation touches pending tasks only; done/in_progress waves frozen** — follows the locked move_execution_guard's "history stays frozen"; rejected a full-plan wave reflow, which could relabel completed tasks' waves and corrupt what telemetry/changes.json recorded.
- **Legality checked on direct deps via adopted-wave comparison plus resulting array position** — position matters because c-5 makes array order execution-authoritative, and wave-only checks miss hand-edited plans with same-wave deps; transitive safety then follows per-task after t-6's re-derivation. Rejected wave-only and position-only variants as each blind to half the corruption cases.
- **Out-of-order done tasks get an explicit guard test** — execute's wave-then-id order can complete t-3 while t-2 is pending, so "no destination before a done task" must be tested against a done task sitting mid-array, not just at the head; rejected treating history as always a contiguous prefix.
- **c-6 pins go into the existing spec_prompt_test.go, not a new test file** — that file already pins §1/§2 behavior, and a regression test beside the existing pins is the one that actually runs; rejected prose-only prompt edits with no test, since prompts regress silently.
- **/dross-techdebt prompt is deliberately thin** — run the deterministic CLI, read the newest report, summarize; rejected mirroring dross-secure's multi-pass agent panel because c-7 says "thin skill over `dross techdebt`" and the scan is already deterministic.
