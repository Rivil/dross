# task-preconditions — verification lens

Method: each criterion's ideal test contract was written first, then the smallest
task that makes it satisfiable. Two contracts drove structure more than any feature:

- **Every plan save must survive a task that carries preconditions.** Probed against
  `project.SaveTOML` (the lossless door `Plan.Save` writes through): a `[[task]]`
  element carrying `[[task.preconditions]]` *or* inline `preconditions = [{…}]`
  makes every save fail with `task.preconditions: a table nested inside an
  array-of-tables element cannot be patched` (internal/project/patch_diff.go
  `differ.nested`), even when the preconditions did not change. Without t-1, the
  first precondition in a plan bricks `task status … done`, `task add/edit`, and
  board pull. That makes t-1 a wave-1 prerequisite for every cmd-level contract.
- **"Same verdict and output" (c-8 vs c-2) is a string-equality test**, so both
  commands must render through one function (t-4's renderer), not two.

Also probed: BurntSushi decodes `max = 5` (integer) into `*float64` fine,
reports unknown keys in both header and inline forms via `MetaData.Undecoded()`
(`task.preconditions.bogus`), and accepts `max = nan` — so a NaN bound, which
makes every comparison false and every measurement pass, needs an explicit
validation contract.

Phase task-preconditions — 13 tasks across 5 waves

Wave 1
  t-1  Patch nested arrays-of-tables inside keyed elements
       files:    internal/project/patch_diff.go, internal/project/patch_value.go,
                 internal/project/patch_keyed_test.go
       covers:   c-1, c-5
       desc:     Within a keyed [[task]] element, a nested array of flat tables
                 (leaf-only) is diffed as one leaf: unchanged → no op; added/changed →
                 one inline `key = [{…}]` line rendered with renderValue/renderKey per
                 field; removed → the line goes. Deeper nesting stays refused by path.
       depends:  —
       contract:
         - a status change on an element carrying header-form [[task.preconditions]]
           rewrites only the status line (today: "cannot be patched" error)
         - same for an element carrying inline `preconditions = [{run = "x", max = 5.0}]`
         - adding a nested list to an existing element writes exactly one inline
           `preconditions = [{…}]` line inside that element's block; reload deep-equals
           the struct; every other element's bytes are unchanged
         - removing the nested list deletes only that line
         - appending a new element (opAppendElem) that carries a nested list renders it
           inline in the new block and reloads equal
         - changing a header-form nested list is refused with the file byte-unchanged
           (no duplicate-key document written)
         - a table nested inside the nested element (depth 2) is still refused, naming
           its dotted path

  t-2  Add Precondition schema, parser and set fingerprint
       files:    internal/phase/phase.go, internal/phase/precondition.go (new),
                 internal/phase/precondition_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-3, c-9, c-4
       desc:     phase.Precondition{Run, Max *float64, Min *float64, Timeout, Why} and
                 Task.Preconditions (toml/json "preconditions,omitempty").
                 PreconditionProblems(plan) []string; ParsePrecondition(s) strict
                 (unknown field = error); UnknownPreconditionFields(path) via
                 MetaData.Undecoded; EffectiveTimeout() (default 60s);
                 PreconditionSetLine(plan) canonical line for consent. not_paths rows:
                 phase.Precondition.Run run / .Timeout timeout / .Why why.
       depends:  —
       contract:
         - run "" or "   " → problem "task t-1 precondition 1: run is empty"
         - timeout "soon", "0s", "-1s" each refused naming the value; "" →
           EffectiveTimeout() == 60s; "90s" → 90s
         - min 10 / max 5 refused (no value can pass); min == max accepted
         - max = nan, max = inf, min = -inf each refused as non-finite
         - ParsePrecondition(`{run = "x", bogus = 1}`) errors naming "bogus";
           UnknownPreconditionFields on a plan file with `bogus` in either header or
           inline form returns ["bogus"]
         - integer `max = 5` decodes as bound 5.0
         - PreconditionSetLine changes when any of run/max/min/timeout/why changes on
           any task, or a precondition is added/removed; it does NOT change when tasks
           are reordered (sorted by task id); a plan with none → ""
         - TestEveryPathShapedFieldIsDeclared fails without the three not_paths rows

  t-3  Add per-phase precondition consent grant
       files:    internal/consent/store.go, internal/consent/preconditions.go (new),
                 internal/consent/preconditions_test.go (new),
                 internal/localstore/store_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-4
       desc:     Grants.TrustedPreconditions map[phase]sha256 (`trusted_preconditions`).
                 PreconditionsConsented(store, repoDir, phaseID, line) (State, error)
                 — name ends in "Consented" so the exec-consent audit counts its callers
                 as gating; GrantPreconditionsConsent; PreconditionsRefusal naming
                 `dross trust --preconditions <phase>`. Takes the line as a string, so
                 no dependency on t-2.
       depends:  —
       contract:
         - grant(A, L) then Consented(A, L) = Granted; Consented(A, L') = Stale with
           ErrStaleConsent; Consented(B, L) = Absent with ErrNoConsent
         - granting B leaves A's grant byte-identical (map keyed by phase)
         - a git-tracked .dross/local.toml → refused via RefuseTrackedLocal before any read
         - PreconditionsRefusal(phase, Stale/Absent) text contains
           "dross trust --preconditions <phase>" and distinguishes stale from absent
         - TestLocalStoreRoundTripsGrants: trusted_preconditions survives a reload and
           a sibling grant's save does not drop it
         - not_paths row `consent.Grants.TrustedPreconditions trusted_preconditions`
           present (path-shaped-field guard fails without it)

Wave 2
  t-4  Evaluate preconditions with one shared renderer
       files:    internal/testlane/spawn.go, internal/precond/precond.go (new),
                 internal/precond/precond_test.go (new)
       covers:   c-2, c-3, c-9, c-10
       desc:     Extract testlane.RunLine(ctx, dir, field, line, stdout, stderr) out of
                 RunLocal (RunLocal delegates; same single spawn site, already reached by
                 gated `dross test`, so the exec audit never sees an unreached site).
                 precond.Evaluate(ctx, repoDir, []Precondition) runs in order via
                 `sh -c` at repoDir, stops at the first failure, and returns a Verdict
                 whose Render() is the one text c-2 and c-8 print.
       depends:  t-2
       contract:
         - `echo boom; exit 3` → failed; Render names "precondition 1", the run line,
           its why, "exit 3", and "boom"
         - 50-line output → Render's tail holds lines 31–50 and not line 30 (tail bound)
         - max 5: `echo 5` passes (at bound); `echo 5.0001` fails "measured 5.0001,
           bound max 5"; min 80: `echo 80` passes, `echo 79.9` fails "measured 79.9,
           bound min 80"
         - unparseable: `printf ''`, `echo '12 ms'`, `echo 1; echo 2`, `echo NaN`, `echo 1,234`
           each fail as "unparseable" quoting the printed text; none passes against
           max 5 or min -1 (proves no coercion to 0)
         - a measurement that prints `3` and exits 1 fails on the exit, not the bound
         - only stdout is parsed: `echo 3; echo junk >&2` with max 5 passes
         - `sleep 30` with timeout 200ms → failed, Render contains "timed out after
           200ms", contains no "exit", and Evaluate returns within timeout + WaitDelay
         - runs at repoDir: `test -f marker` passes with marker at repoDir while the
           process cwd is a subdirectory without it
         - first failure stops the run: a second precondition `touch ran` leaves no
           `ran` file
         - a run line starting with `-` is refused by argfence and nothing spawns
         - existing `dross test` RunLocal tests stay green (delegation preserves the
           runtime.test_command label)

  t-5  Report malformed preconditions in dross validate
       files:    internal/cmd/validate.go,
                 internal/cmd/validate_precondition_test.go (new)
       covers:   c-1
       desc:     Per phase plan, append PreconditionProblems and
                 UnknownPreconditionFields to validate's ✗ list, prefixed by plan path.
       depends:  t-2
       contract:
         - plan with `run = ""` → validate exits non-zero with
           "✗ <plan path> task t-1 precondition 1: run is empty"
         - plan with `bogus = 1` in a precondition → ✗ line naming "bogus" (unknown field)
         - plan with `max = nan` → ✗ line naming non-finite bound
         - plan with well-formed preconditions (header and inline form) →
           "✓ all dross artefacts valid"

  t-6  Add dross trust --preconditions <phase>
       files:    internal/cmd/trust.go,
                 internal/cmd/trust_preconditions_test.go (new),
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-4
       desc:     Mirror trustReplay: print every task id + run + bounds + timeout before
                 writing; grant fingerprints PreconditionSetLine; --check is silent on
                 success. A phase with no preconditions is "nothing to trust".
       depends:  t-2, t-3
       contract:
         - grant prints each precondition's run line before local.toml changes, then
           trusted_preconditions[<phase>] is set
         - --check exits 0 after grant; after rewriting one precondition's max in
           plan.toml, --check exits non-zero naming stale and
           "dross trust --preconditions <phase>"
         - phase with no preconditions → error "nothing to trust", local.toml unchanged
         - tracked local.toml → refused, no write
         - granting phase B leaves phase A's --check at exit 0
         - cli_tree.txt golden carries the `--preconditions` flag row

  t-7  Set and clear preconditions via task add/edit/show
       files:    internal/phase/plan_edit.go, internal/cmd/task.go,
                 internal/cmd/task_precondition_edit_test.go (new),
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-5, c-1
       desc:     NewTask/TaskEdit gain Preconditions; ValidatePlan appends
                 PreconditionProblems. add: repeatable StringArray `--precondition
                 '<inline table>'` parsed by ParsePrecondition. edit: `--precondition`
                 replaces, `--add-precondition` appends, `--clear-preconditions`
                 empties; conflicting combos refused. A change prints that the phase's
                 precondition grant is revoked. show prints them (text + --json).
       depends:  t-1, t-2
       contract:
         - `task add --precondition '{run = "go test ./x", max = 5}'` → reloaded task
           has run "go test ./x", max 5; every other task's bytes unchanged
         - a comma inside the run (`{run = "a, b"}`) stays one precondition
         - `{run = ""}`, `{run = "x", bogus = 1}`, `{run = "x", timeout = "soon"}` each
           refused with plan.toml byte-unchanged
         - edit --precondition replaces the list; --add-precondition appends after the
           existing entries; --clear-preconditions leaves the task with none and no
           `preconditions` key; --clear-preconditions with --precondition refused
         - after an edit that changes preconditions, consent.PreconditionsConsented for
           the old grant returns Stale and the output names
           "dross task edit" … "dross trust --preconditions <phase>"; an edit that
           touches only --title prints no revocation line
         - `task show` prints each precondition's run/bounds/timeout/why; `--json`
           includes a `preconditions` array
         - task edit has no flag that marks a precondition skipped (no_bypass)
         - cli_tree.txt golden carries the new add/edit flags

  t-8  Keep board pull from starting precondition tasks
       files:    internal/boardsync/task_pull.go,
                 internal/boardsync/task_pull_test.go
       covers:   c-7
       desc:     In ReportTaskMoves --apply, a board move whose NewStatus is in_progress
                 for a task with preconditions is reported, not written, naming
                 `dross task status <phase> <id> in_progress` as the route.
       depends:  t-2
       contract:
         - board card at task-in-progress for a pending task WITH preconditions →
           --apply leaves plan status pending and prints the task status route
         - same move for a task WITHOUT preconditions still applies (no regression)
         - a done move for a precondition task still applies (only the in_progress
           transition is gated, per c-11)
         - the other clean moves in the same pull still apply and are saved

Wave 3
  t-9  Gate task status in_progress on preconditions
       files:    internal/cmd/task.go,
                 internal/cmd/task_precondition_test.go (new),
                 internal/phase/plan_lossless_test.go
       covers:   c-2, c-3, c-4, c-9, c-10, c-11
       desc:     Order: requireExecConsent → load → task exists → refuseSecondInProgress
                 → if current status != in_progress and the task has preconditions:
                 PreconditionsConsented (refuse with PreconditionsRefusal) →
                 precond.Evaluate → refuse with Render() before any save. Shared helper
                 runTaskPreconditions for t-11.
       depends:  t-1, t-3, t-4
       contract:
         - failing precondition (`echo boom; exit 3`) → error; plan.toml byte-identical
           (status still pending); output has "precondition 1", "exit 3", "boom"
         - `echo 7` max 5 → refused "measured 7, bound max 5"; `echo 5` max 5 → task
           becomes in_progress
         - `echo '12 ms'` max 5 → refused "unparseable" quoting `12 ms`, still pending
         - `sleep 30` timeout 200ms → refused naming "timed out after 200ms", still pending
         - no grant → refused with "dross trust --preconditions <phase>" and a
           `touch ran` precondition left no `ran` file; grant, then hand-edit the run
           line → refused as stale, `ran` still absent
         - c-11: task already in_progress + failing precondition → re-set succeeds and
           `ran` marker stays absent; from failed → preconditions run (marker
           created/refused); from pending after a reset → run again
         - a second task's in_progress refusal (refuseSecondInProgress) fires before
           any precondition spawns (marker absent)
         - a task with no preconditions in a phase with no precondition grant still
           goes in_progress
         - `task status` flags are exactly {--reason}: no bypass flag (no_bypass)
         - phase lossless: SetTaskStatus + Save on a plan whose task carries
           preconditions changes only the status line (cmd-level proof of t-1)

  t-10 Teach planner and plan review about preconditions
       files:    assets/prompts/plan.md, assets/prompts/plan-review.md,
                 internal/cmd/plan_prompt_test.go
       covers:   c-6
       desc:     plan.md: field-table row — a "before X, check Y" gate is a
                 `preconditions` entry, never prose; §5 schema shows the inline form
                 `preconditions = [{run = "…", max = N, why = "…"}]`; trim prose to stay
                 under the 15000-byte cap (14857 today). plan-review.md: new check —
                 a task whose description/test_contract states a gate in prose with no
                 precondition is a FLAG.
       depends:  t-7
       contract:
         - expanded plan.md names `preconditions` and maps "before X, check Y" to it
         - plan.md §5 schema contains the inline `preconditions = [{run =` form (the
           form t-1 can edit losslessly)
         - plan-review.md's reviewer prompt has a check naming prose gate + precondition
           at FLAG severity
         - assets/budget_test.go: plan.md ≤ 15000 bytes
         - assets/conservation_test.go: every recorded plan.md instruction row still
           present (the trim cut prose, not instructions)
         - rawPromptReadAllowlistSize stays 19 (tests read via expandedPrompt)

Wave 4
  t-11 Add dross task check <phase> <task>
       files:    internal/cmd/task.go,
                 internal/cmd/task_check_test.go (new),
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-8, c-4
       desc:     Read-only verb: resolve task, PreconditionsConsented, Evaluate via
                 runTaskPreconditions, print Render(); non-zero on failure; never
                 Save. Evaluates regardless of current status.
       depends:  t-9
       contract:
         - on the t-9 failing fixture, `task check` output equals the refusal text
           `task status … in_progress` returns (string equality — same renderer)
         - exits non-zero on failure, 0 on pass; plan.toml bytes identical after both
         - a task already in_progress or done is still evaluated, status unchanged
         - no grant → refused naming "dross trust --preconditions", `ran` marker absent
         - task with no preconditions → exit 0, says it has none
         - exec audit: the testlane RunLine site lists `task check` among gatedVia and
           has no ungatedVia (TestEverySpawnSiteGatedOrExempt stays green)
         - cli_tree.txt golden carries `dross task check`

Wave 5
  t-12 Stop execute on a precondition refusal
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 internal/cmd/execute_prompt_test.go
       covers:   c-7
       desc:     §1a: a non-zero `task status … in_progress` is a precondition refusal —
                 skip the board sync line; pair: AskUserQuestion fix (fix the condition,
                 or `dross task edit` + user-run `dross trust --preconditions`, then
                 `dross task check`) / replan / fail (`task status … failed --reason`).
                 Solo partial: halt the run, report the refusal; never edit, clear or
                 route around the precondition.
       depends:  t-11, t-6
       contract:
         - §1a of expanded execute.md: after the status line, a refusal arm naming
           fix / replan / fail and `dross task check`
         - §1a says the `issue task sync … task-in-progress` line runs only after the
           status line exits 0; boardsync lifecycle divergence guard stays green
         - expanded _execute.solo.md: precondition refusal → halt, with no
           `dross task next` and no `--clear-preconditions` / `task edit` in that arm
         - sweep over every expanded assets/prompts/*.md and assets/agents/*.md: the
           only `task status … in_progress` is execute.md §1a, and no prompt names a
           skip/force/bypass for preconditions
         - TestExecuteAbsorbedProseDeleted (25500) and TestTestStepPromptsNotGrown
           (25854) stay green; budget_test execute.md ≤ 15000; conservation rows intact

  t-13 Document preconditions in README command rows
       files:    README.md, internal/cmd/readme_preconditions_test.go (new)
       covers:   c-5, c-8, c-4
       desc:     `dross task {…}` row gains check and the precondition flags;
                 `dross trust` row gains `--preconditions <phase-id>`.
       depends:  t-11, t-6, t-7
       contract:
         - README `dross task {…}` brace list contains `check` and the row names
           `--precondition`, `--add-precondition`, `--clear-preconditions`
         - README `dross trust [...]` row names `--preconditions <phase-id>` and
           `trusted_preconditions`

## Coverage

- c-1  → t-1, t-2, t-5, t-7
- c-2  → t-4, t-9
- c-3  → t-2, t-4, t-9
- c-4  → t-2, t-3, t-6, t-9, t-11, t-13
- c-5  → t-1, t-7, t-13
- c-6  → t-10
- c-7  → t-8, t-12
- c-8  → t-11, t-13
- c-9  → t-2, t-4, t-9
- c-10 → t-4, t-9
- c-11 → t-9 (and t-8 keeps board pull to the in_progress transition only)

11/11 criteria covered. Edges: boundary (c-3 at-bound, c-9 timeout) in t-4/t-9;
precision (c-3 5 vs 5.0001, c-10 NaN/empty/units) in t-2/t-4; idempotency (c-11
re-set vs retry) in t-9.

## Judgment calls

- Patcher extension (t-1) over a custom MarshalTOML on Precondition: rejected the
  encoder trick because renderValue refuses multi-line/table values anyway and the
  differ refuses at classify time; the fix belongs where the refusal lives.
- Inline form is what the CLI writes and the plan prompt teaches; header form is
  read and preserved but a CLI edit of it is refused rather than spliced into a
  duplicate-key file.
- Reuse the single testlane spawn site (RunLine extraction) over a new site in
  internal/precond: a new site is "reached by no command" until t-9 lands, which
  fails TestEverySpawnSiteGatedOrExempt at t-4's commit.
- Process-group kill for timeouts rejected: sh-kill + WaitDelay matches every other
  repo-command spawn; pgid would be a Unix-only divergence in a shared seam.
- Stop at the first failing precondition rather than run all: measurements can be
  expensive and the refusal is already decided; rejected "report all" despite the
  one-round-trip argument.
- Fingerprint includes `why` and is sorted by task id: the locked decision says any
  edit revokes; a `task move` is not an edit of the set, so it must not revoke.
- Consent is checked only when the task being started has preconditions; a phase
  with preconditions elsewhere does not need a grant to start an unguarded task.
- `task check` gates on the precondition grant only, not runtime.test_command
  consent, and is not added to execGatedCommands (that roster is the
  requireExecConsent set; the audit counts it gating via the *Consented name).
- min > max and non-finite bounds are "malformed" for c-1: both make the check
  vacuous (never passes / always passes), which is the false-green c-10 forbids.
- Unknown-field detection stays out of ValidatePlan (validate-time only via
  UnknownPreconditionFields): otherwise a plan with a stray key blocks the very
  `--clear-preconditions` edit that removes it.
- Board pull guard (t-8) included rather than deferred: `issue task pull --apply`
  is a prompt-driven route that writes in_progress without `task status`, exactly
  what c-7's "no prompt path starts the task by another route" forbids.
- No `dross doctor` row for the precondition grant: doctor already omits replay and
  run grants; adding one is a separate consistency change, not a criterion.
- No plan.md instruction to run `dross trust --preconditions` after lock: the
  refusal text names it, and plan.md has ~140 bytes of budget headroom.
