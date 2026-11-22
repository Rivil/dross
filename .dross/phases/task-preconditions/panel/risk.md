# task-preconditions — risk lens

Phase task-preconditions — 15 tasks across 5 waves

Lens: every task owns one failure class end to end. The graph is shaped by what breaks first and does the most damage. The worst case is that adding a precondition to plan.toml makes every later plan save fail, which stops the whole loop. A gate that can be bypassed, or that passes when it should not, comes next. Prompts come last.

Failure classes found in the tree (not hypothetical):

- **F1, plans cannot be saved.** `internal/project/patch_diff.go` `differ.nested` refuses any table nested inside an array-of-tables element. `change()` calls it *before* it compares values, so once any task carries a `[[task.precondition]]`, every `Plan.Save` fails: `task status`, `task edit`, board pull and `phase complete`. Keyed element moves, deletes and inserts are also at risk of separating a `[[task]]` header from the sub-blocks that belong to it.
- **F2, bounds lost silently.** `phase.LoadPlan` ignores undecoded keys, so a typo such as `mx = 3` turns a bounded measurement into an exit-0 check that passes. TOML also allows `nan` and `inf` as bounds, and every comparison against NaN is false, so the check always passes.
- **F3, measurements coerced.** `strconv.ParseFloat` accepts `NaN`, `Inf`, `+Inf`, `infinity` and hex floats. A measurement command that exits non-zero after printing `0` must not pass.
- **F4, hangs and unbounded memory.** Killing `sh` does not stop its children, and they keep its pipe open. Unbounded output capture can exhaust memory. A command that reads stdin can block.
- **F5, consent gaps.** `task status in_progress` already calls `requireExecConsent` (the test-command grant). `TestEverySpawnSiteGatedOrExempt` would therefore count a precondition spawn as gated even with no precondition grant. The exec-consent audit cannot see this gap, so only behavioural tests catch it. The fingerprint must also be collision-free and must ignore formatting, while still covering every field that changes what runs or passes.
- **F6, starting a task without its gate.** `boardsync.ReportTaskMoves` with `--apply` calls `plan.SetTaskStatus(…, in_progress)` directly when a card is dragged. execute.md §1a runs `issue task sync --status task-in-progress` unconditionally after `task status`.
- **F7, the plan changes while preconditions run.** A run takes up to N×60s between load and save, and a stale save reverts concurrent edits. The window in which a second task can be started grows from milliseconds to minutes.
- **F8, regression in phases with no preconditions.** If the precondition grant were required whenever *any* preconditions exist, every existing phase would break.
- **F9, guard and golden tests.** plan.md is 14857/15000 bytes. execute.md has expanded-size pins (`TestExecuteAbsorbedProseDeleted` 25500, `TestTestStepPromptsNotGrown` 25854). `TestTaskEdgesPairTheBoardStatusWithThePlanStatus` requires the board sync line to follow the in_progress write. ARCHITECTURE.md's `apply` (patch.go:619) and `diff` (patch_diff.go:111) anchors are enforced by `TestArchitectureNamesLosslessWriter`. `TestEverySpawnSiteGatedOrExempt` flags a spawn site that no command reaches, so a new `exec.Command` added before it is wired in would turn it red.

```
Wave 1
  t-1  Keep nested precondition blocks inside their task
       files:    internal/project/patch.go, internal/project/patch_nested_test.go, ARCHITECTURE.md
       covers:   c-5
       desc:     The span of a keyed [[task]] element includes its [[task.precondition]] sub-blocks,
                 so opMoveElem, opDeleteElem and opInsertElem move, delete or skip past the whole
                 task. Nesting deeper than one level under a keyed array is still refused by name.
       contract: - Moving a task that has 2 precondition blocks (`task move t-3 --before t-1`)
                   produces a file that decodes with both preconditions still on t-3, and t-1's
                   comments are byte-identical.
                 - Removing t-2 removes its precondition blocks and the comment above them. t-3's
                   preconditions do not reattach to t-1.
                 - `task add --after t-1`, where t-1 has preconditions, inserts the new [[task]]
                   after t-1's last precondition block, not between t-1's header and its
                   sub-blocks.
                 - A table nested two levels deep, or nested inside an unkeyed array (project.toml),
                   is still refused with the "cannot be patched" error.
                 - TestArchitectureNamesLosslessWriter still passes. The `apply` anchor resolves after
                   line shifts.
       depends:  —

  t-2  Precondition schema, shape checks, unknown-key capture
       files:    internal/phase/phase.go, internal/phase/precondition.go (new),
                 internal/phase/precondition_test.go (new), internal/phase/plan_edit.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1
       desc:     phase.Precondition {run, max *float64, min *float64, timeout string, why} as
                 Task.Preconditions `toml:"precondition"`. PreconditionProblems is the single shape
                 check. LoadPlan records the undecoded keys under task.precondition on the Plan
                 (unexported field). ValidatePlan includes the shape check. Run, Timeout and Why are
                 filed in not_paths.txt.
       contract: - Each of these is reported naming the task id and precondition index: run = "",
                   run = "   ", a run starting with "-", max = nan, min = inf, min > max,
                   timeout "abc", timeout "0s", timeout "-5s".
                 - `mx = 3` on a precondition is reported as an unknown key "mx" after LoadPlan.
                   An unknown key elsewhere in plan.toml is not reported (the scope is
                   task.precondition only).
                 - `max = 3` (a TOML integer) decodes to 3.0 with no error, and min == max is
                   accepted.
                 - An empty timeout has an effective timeout of 60s.
                 - TestEveryPathShapedFieldIsDeclared passes with the three new string fields
                   filed as not-paths.
       depends:  —

  t-3  Bounded, timeout-first precondition evaluator over a spawn seam
       files:    internal/precondition/run.go (new), internal/precondition/run_test.go (new)
       covers:   c-9, c-2
       desc:     Run(ctx, line, timeout, spawn) wraps a context timeout around an injected spawn
                 func and returns ExitCode, TimedOut, Stdout (capped), Tail (last 40 lines of the
                 combined output, byte-capped) and SpawnErr. It contains no os/exec. The real spawn
                 is wired in t-11, through an existing gated site.
       contract: - A fake spawn that blocks until ctx is done and then returns an exit error
                   produces TimedOut=true with the configured duration. Timeout is checked before
                   the exit status, so a timeout is never reported as exit -1.
                 - A fake that writes 10 MiB keeps Stdout at or under the cap with Overflow=true,
                   and Tail holds at most 40 lines and at most the byte cap.
                 - Stdout is separated from stderr. A fake that writes "7" to stdout and noise to
                   stderr yields Stdout "7", and both appear in Tail.
                 - A spawn error with no exit code (sh missing) returns a non-nil SpawnErr and is
                   never classified as exit 0.
       depends:  —

Wave 2
  t-4  Diff nested precondition arrays as sub-element ops
       files:    internal/project/patch_diff.go, internal/project/patch.go,
                 internal/project/patch_nested_test.go, ARCHITECTURE.md
       covers:   c-5
       desc:     Inside an element of a keyed array, a nested array-of-tables is diffed. When
                 unchanged it produces zero ops. Otherwise it produces append, delete and in-place
                 key edits on [[task.precondition]] blocks within the parent task's span.
                 verifyPatched remains the backstop.
       contract: - On a plan where t-2 has two precondition blocks, `SetTaskStatus(t-1, done)` then
                   Save changes exactly one line. This is the case where every later save would
                   otherwise fail.
                 - Changing t-2's precondition[1].max from 3 to 5 rewrites only that value; the
                   comments in t-2's other block stay byte-identical.
                 - Clearing t-2's preconditions deletes both blocks, including a stray `mx = 3`
                   line inside one, and the file re-decodes with no undecoded keys.
                 - Adding the first precondition to a task with none appends the block inside that
                   task, before the next [[task]] header.
                 - TestArchitectureNamesLosslessWriter still passes. The `diff` anchor resolves.
       depends:  t-1

  t-5  Measurement parse and bound verdict
       files:    internal/precondition/verdict.go (new), internal/precondition/verdict_test.go (new)
       covers:   c-3, c-10
       desc:     Verdict(pre, result) decides pass or fail. Any non-zero exit fails as a command
                 failure without parsing. For a bounded precondition, the trimmed stdout must be
                 one finite number; the measurement fails if it is above max or below min.
       contract: - Measured "3" with max 3 passes and "3.0000001" fails. Measured "10" with min 10
                   passes and "9.99" fails.
                 - Measured "0.3" with max = 0.3 written in TOML passes; both sides go through
                   decimal parsing, so there is no 0.1+0.2 drift.
                 - Each of these fails as unparseable and quotes what was printed: "NaN", "Inf",
                   "+Inf", "infinity", "0x1p-2", "1_000", "", "3 4", "3%", "3\n4", and
                   "warming up\n3". None is coerced to 0.
                 - Exit 2 with stdout "0" fails as "exit 2", not as a measurement.
                 - Stdout overflow fails as unparseable.
                 - The refusal text states the measured value and the bound, e.g.
                   "measured 4 > max 3".
                 - A precondition with no bounds ignores stdout, so exit 0 with output "abc"
                   passes.
       depends:  t-2

  t-6  `dross validate` precondition parity with the write guard
       files:    internal/cmd/validate.go, internal/cmd/validate_precondition_test.go (new)
       covers:   c-1
       desc:     For each phase, validate reports PreconditionProblems and the unknown
                 task.precondition keys, naming the plan path, task id and index. It never spawns
                 anything.
       contract: - A table over t-2's malformed fixtures, run through both `dross validate` and
                   `dross task edit` (saveIfValid), shows both refusing every struct-visible case.
                 - Unknown key `mx` is reported by validate. A rejected edit leaves plan.toml
                   byte-identical.
                 - Validate over a plan whose precondition is `touch marker` exits 0 with no
                   marker file created.
                 - A valid plan with `max = 3` and no timeout validates clean.
       depends:  t-2

  t-7  Per-phase precondition-set fingerprint and grant store
       files:    internal/consent/preconditions.go (new), internal/consent/preconditions_test.go (new),
                 internal/consent/store.go, internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-4
       desc:     PreconditionSetLine(plan) serialises (task id, run, min, max, effective timeout,
                 why) in plan order as JSON. Grants.TrustedPreconditions is map[phase]sha256.
                 PreconditionsConsented and GrantPreconditionConsent return Absent, Stale or
                 Granted and fail closed on an unparseable or tracked store. The code goes in a new
                 file so the anchors on consent.go:114 and :130 do not shift.
       contract: - Each of these changes the fingerprint: run text, max, min, timeout, why, moving
                   a precondition to another task, adding one, removing one, and reordering within
                   a task.
                 - These leave it unchanged: TOML comments or reformatting, `max = 3` vs
                   `max = 3.0`, `timeout = ""` vs `"60s"`, any task title or status change, and
                   adding a task with no preconditions.
                 - A run of `a","b` in one precondition and two preconditions `a` and `b` do not
                   collide, because the line is encoded as JSON and never joined with separators.
                 - A grant for phase A does not satisfy phase B when their sets are identical.
                 - An unparseable local.toml returns not-consented with a wrapped sentinel, never
                   Granted.
                 - TestEveryPathShapedFieldIsDeclared passes with Grants.TrustedPreconditions
                   filed.
       depends:  t-2

  t-8  Board pull cannot start a gated task
       files:    internal/boardsync/task_pull.go, internal/boardsync/task_pull_precondition_test.go (new)
       covers:   c-7, c-11
       desc:     In ReportTaskMoves with --apply, a board move from pending or failed into
                 in_progress for a task with at least one precondition is refused. The move is
                 reported and not applied, the agreement point is not advanced, and the output
                 names `dross task status <phase> <task> in_progress`. Clean moves still apply,
                 and the command exits non-zero at the end, as it does for a conflict.
       contract: - A board drag of t-2 (2 preconditions) to task-in-progress leaves plan.toml's
                   t-2 status as pending, leaves board.json's link unchanged, prints the
                   `dross task status` instruction, and exits non-zero.
                 - The same drag for a task with no preconditions still applies (regression).
                 - In the same --apply run, t-5's in_progress → done move still applies.
                 - in_progress → done for a gated task applies; only the transition into
                   in_progress is gated.
       depends:  t-2

Wave 3
  t-9  `dross trust --preconditions <phase>` grant verb
       files:    internal/cmd/trust.go, internal/cmd/trust_preconditions_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-4
       desc:     Prints every precondition (task id, run, bounds, timeout) and then writes the
                 fingerprint. --check reports current, absent or stale, mirroring the shape of
                 --replay. The grant key is not settable through `dross local set`.
       contract: - With an unwritable store the printed listing still appears, which proves it
                   prints before writing.
                 - A phase with zero preconditions is refused with "nothing to trust", and
                   local.toml is unchanged.
                 - --check exits 0 when current. After `task edit --precondition`, --check exits
                   non-zero naming "stale" plus the re-grant command, and an absent grant names
                   "absent".
                 - `dross local set trusted_preconditions x` fails with "unknown local key".
                 - A tracked local.toml is refused unread.
                 - cli_tree.txt gains the --preconditions flag row, and the README trust row
                   names it.
       depends:  t-7

  t-10 `task add/edit/show` carry preconditions losslessly
       files:    internal/cmd/task.go, internal/phase/plan_edit.go,
                 internal/cmd/task_precondition_flags_test.go (new), cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-5
       desc:     A repeatable --precondition '<TOML inline table>' (StringArray) on add and edit,
                 where edit replaces the whole set. Edit also gets --add-precondition (append) and
                 --clear-preconditions. show prints each precondition, and --json carries them.
       contract: - `--precondition '{ run = "a, b && c", max = 3 }'` keeps the comma: the run
                   decodes to exactly "a, b && c".
                 - `{ runn = "x" }` is refused naming the key "runn". Malformed TOML is refused
                   naming the --precondition flag. A shape problem (empty run, nan) is refused by
                   saveIfValid.
                 - --precondition together with --clear-preconditions, or with --add-precondition,
                   is refused. Every refusal leaves plan.toml byte-identical.
                 - --clear-preconditions removes the blocks together with a stray unknown key
                   inside them.
                 - `task show` lists run, max/min and timeout. `show --json` has a `precondition`
                   array.
       depends:  t-2, t-4

  t-11 `task status … in_progress` runs preconditions, fail-closed
       files:    internal/cmd/task.go, internal/cmd/task_precondition.go (new),
                 internal/cmd/task_precondition_test.go (new)
       covers:   c-2, c-3, c-4, c-9, c-11
       desc:     The order is: requireExecConsent, load, task exists, refuseSecondInProgress. If
                 the task is already in_progress, everything else is skipped. Otherwise: shape and
                 unknown-key problems, then PreconditionsConsented (only when the task has at least
                 one precondition), then the preconditions run in order through the existing
                 testlane.RunLocal spawn at the repo root. The first failure refuses. On pass the
                 plan is re-loaded, the task's status and set fingerprint are re-checked, and the
                 plan is saved. The command exits 3 on a precondition refusal and 4 when the
                 precondition grant is missing or stale.
       contract: - Unconsented `touch marker`: exits 4 with `dross trust --preconditions <phase>`,
                   creates no marker, and leaves plan.toml byte-identical.
                 - Granted, then `task edit` changes the run: exits 4 as stale, and no marker is
                   created.
                 - A task with no preconditions starts with no precondition grant (regression for
                   every existing phase).
                 - `exit 3` with "boom" on stderr: exits 3 naming the task, precondition index,
                   why, run, "exit 3" and the tail containing "boom". The task stays pending. A
                   failed task stays failed with its reason byte-identical.
                 - `sleep 30` with timeout "300ms": returns within 10s and refuses naming
                   "timed out after 300ms".
                 - A task already in_progress, with a precondition that would fail and no grant:
                   re-setting exits 0 and creates no marker. failed → in_progress re-runs.
                 - A seam that sets another task to in_progress between run and save makes the
                   command refuse, and plan.toml keeps that concurrent write.
                 - Run from a subdirectory, `test -f go.mod` passes (the working directory is the
                   repo root).
                 - A malformed precondition refuses before the consent check and before any spawn.
                 - TestEverySpawnSiteGatedOrExempt still classifies RunLocal's site as gated, adds
                   no new //dross:exec-exempt and reports no ambiguous dispatch.
       depends:  t-3, t-4, t-5, t-7

Wave 4
  t-12 `dross task check` — read-only twin of the start gate
       files:    internal/cmd/task.go, internal/cmd/task_check_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-8, c-4
       desc:     Registers `task check <phase> <task>`. It calls the same evaluator as t-11 and
                 uses the same exit codes 3 and 4. It never saves, never releases the writer
                 record, and evaluates even when the task is in_progress. It is gated through
                 PreconditionsConsented, not requireExecConsent, so the execGatedCommands roster
                 is unchanged.
       contract: - On the same failing fixture, check and status produce an identical refusal body
                   and the same exit code.
                 - plan.toml is byte-identical after check on pass, on fail, and on an in_progress
                   task.
                 - Without a grant, exits 4 and creates no marker.
                 - A task with no preconditions exits 0 with "no preconditions".
                 - TestEverySpawnSiteGatedOrExempt shows `task check` reaching RunLocal as gated.
                   TestExecGatedSetIsExplicit still passes.
                 - cli_tree.txt has the `dross task check` row, and the README task brace list
                   names `check`.
       depends:  t-11

  t-13 Planner encodes gates; plan review flags prose gates
       files:    assets/prompts/plan.md, assets/prompts/plan-review.md,
                 internal/cmd/plan_prompt_test.go, internal/cmd/plan_review_prompt_test.go (new)
       covers:   c-6
       desc:     The §2 field table gains a `precondition` row: any "before X, check Y" becomes a
                 [[task.precondition]] (run, max/min, timeout, why), and later changes go through
                 `dross task add/edit --precondition`. The plan-review checklist gains: a task whose
                 description states a gate in prose with no precondition is a finding.
       contract: - TestPromptByteBudget passes with plan.md ≤ 15000 bytes. It is 14857 today, so
                   prose must be trimmed elsewhere, and the cap is never raised.
                 - A plan_prompt_test pins "before X, check Y", "precondition" and
                   "--precondition" inside the §2 field table.
                 - The new test reads plan-review.md through expandedPrompt, so
                   rawPromptReadAllowlistSize stays 19, and pins that a prose gate without a
                   precondition is named as a finding.
       depends:  t-10

Wave 5
  t-14 Execute stops on a refusal; no other route starts the task
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_execute.board-moves.md, internal/cmd/execute_prompt_test.go
       covers:   c-7
       desc:     §1a runs the board sync only after `task status … in_progress` exits 0. Exit 3:
                 pair mode shows the refusal and asks fix / replan (`dross task edit`, then
                 re-trust) / fail. Solo mode halts the run, with no `task next` and no failed
                 write. Exit 4: stop and show the precondition lines; the user runs
                 `dross trust --preconditions`. The board-moves partial routes a refused gated
                 move to `task status`.
       contract: - A test pins that in execute.md the issue-sync line runs only on status exit 0,
                   and TestTaskEdgesPairTheBoardStatusWithThePlanStatus still passes, with no
                   status write between the in_progress line and its sync.
                 - The solo partial's refusal branch contains "halt" and no `dross task next`
                   within it.
                 - Pair mode offers exactly fix / replan / fail.
                 - No prompt under assets/ contains hand-editing plan.toml status or
                   `issue task pull --apply` as a way to start a task.
                 - TestPromptByteBudget, TestTestStepPromptsNotGrown (25854) and
                   TestExecuteAbsorbedProseDeleted (25500) pass without raising any pin.
       depends:  t-8, t-9, t-11, t-12

  t-15 ARCHITECTURE.md: precondition entry + re-anchor touched files
       files:    ARCHITECTURE.md
       covers:   —  (support: keeps the drift reporter honest for c-2/c-4)
       desc:     Adds an entry for the precondition gate (evaluator, verdict, the
                 consent family, the board-pull refusal). Repoints the anchors into phase.go,
                 validate.go, trust.go and task_pull.go that this phase shifted.
       contract: - `dross architecture check` reports no stale link in any file this phase
                   touched.
                 - The new entry's anchors resolve to lines that mention the symbols they name.
                 - TestArchitectureNamesLosslessWriter still passes.
       depends:  t-6, t-8, t-9, t-10, t-12
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (declare + validate rejects malformed) | t-2 (schema, shape checks, unknown-key capture), t-6 (validate CLI, parity with the write guard) |
| c-2 (status runs preconditions; refusal names precondition, exit code, tail) | t-11 (integration, refusal text), t-3 (exit code and tail capture) |
| c-3 (bounded measurement, exact bound passes) | t-5 (verdict), t-11 (end to end) |
| c-4 (never runs without consent; changed = refused) | t-7 (fingerprint and store), t-9 (grant verb), t-11 (status gate), t-12 (check gate) |
| c-5 (add/edit set and clear, no hand edit) | t-10 (flags), t-1 + t-4 (lossless save of nested blocks, without which the flags cannot be written) |
| c-6 (planner encodes gates; review flags prose gates) | t-13 |
| c-7 (execute stops; no other route starts the task) | t-14 (prompts), t-8 (board pull cannot start a gated task) |
| c-8 (`task check`, same verdict, no status change) | t-12 |
| c-9 (timeout kills, counts as failed, named) | t-3 (timeout checked before exit status), t-11 (real process killed within the bound) |
| c-10 (unparseable never coerced or passed) | t-5 |
| c-11 (only the transition into in_progress) | t-11 (re-set skips, retry re-runs), t-8 (board path respects the same edge) |

11/11 criteria covered.

## Judgment calls

- **Patcher work comes first and is split in two** (t-1 spans, t-4 nested diff). I rejected a fallback full re-encode when the patcher refuses a shape: it silently drops comments and breaks the lossless guarantee. I also rejected one combined patcher task, because span corruption (a sub-block reattaching to the wrong task) and the save failure on every write are separate failure modes, each needing its own owner and tests.
- **No new spawn site; reuse `testlane.RunLocal`.** I rejected a dedicated runner with `Setpgid` process-group kill. A new `exec.Command` that no command reaches until t-11 would turn `TestEverySpawnSiteGatedOrExempt` red for two waves. `Setpgid` also detaches the child from the terminal's Ctrl-C. Cost: grandchildren of `sh -c` can outlive a timeout. The gate still returns promptly (WaitDelay) and reports failure, which is what c-9 requires.
- **The leading-dash fence is enforced in shape validation (t-2), not by a relabelled argv builder.** RunLocal's fence names `runtime.test_command`. Because the runner executes only validated preconditions, that misleading label is never reached.
- **Unknown keys are caught at load and at run time, not in saveIfValid.** A struct cannot see an unknown key. If saveIfValid refused on one, the only way to fix a typo would be a hand edit, which contradicts no_bypass. With this design, `task edit --precondition` or `--clear-preconditions` deletes the stray line (t-4, t-10).
- **Distinct exit codes: 3 for a precondition refusal, 4 for a missing or stale grant.** I rejected branching on the text of the error. If the prompt confuses the two cases, the agent may run `dross trust` itself or treat a consent refusal as a failed measurement.
- **A measurement command that exits non-zero fails as a command failure and is never parsed.** I rejected parsing its stdout anyway, because a command that crashes after printing `0` would otherwise pass.
- **Stop at the first failing precondition** rather than running all of them. Measurements can be expensive and have side effects, and c-2 needs to name only the failing one.
- **The fingerprint covers task id, run, min, max, effective timeout and why, JSON-encoded, in plan order, and is keyed per phase.** Leaving out `why` would let the plan's stated reason change under an existing grant. Joining fields with separators allows collisions. Including status would make every in_progress transition revoke the grant.
- **The grant is required only when the task being started has preconditions.** Requiring it whenever the phase has any would break every task that has none, and the locked decision is per phase in scope, not per task.
- **Re-load and re-check after the run, before saving.** I rejected keeping the plan loaded before the run, because a save after a run of up to minutes reverts concurrent edits and widens the second-in-progress race.
- **The board-pull bypass is in scope (t-8).** It is the one CLI path that can move a task into in_progress without `task status`, and the execute board-moves partial calls it. Leaving it out would make c-7 false.
- **The precondition grant is not added to options.md.** It has 280 bytes left under its cap. The refusal message and the README trust row tell the user about `dross trust --preconditions`.
- **The TOML key is `[[task.precondition]]` (singular) and the flag takes a TOML inline table.** I rejected a `run=…,max=…` mini-syntax: commands contain commas and `=`, and an ad-hoc parser is a new place for bugs. The inline table uses the same spelling and decoder as plan.toml, and unknown-key detection comes free.
- **t-15 covers no criterion.** Anchor drift across five touched files is a real failure: the docs end up pointing at the wrong code. Collecting it in one owner avoids every code task re-anchoring separately. The enforced patcher anchors stay with t-1 and t-4 so those commits stay green.
