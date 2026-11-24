# task-preconditions — panel synthesis

## Scores

| Dimension | risk (15 tasks / 5 waves) | mvp (7 / 4) | verification (13 / 5) |
|---|---|---|---|
| Criteria coverage | 11/11, and the most complete in practice. It is the only draft that closes run-time unknown keys, the stale-save race and the board-moves partial. | 11/11 on paper, but c-7 has a hole: `issue task pull --apply` still writes in_progress (task_pull.go:167), and no task guards it. | 11/11 on paper, but c-5 has a hole. Header-form preconditions, which is what `encodeFresh` writes for every fresh plan.toml, are "refused rather than spliced", so they need a hand edit. Run-time unknown keys also pass silently. |
| Test-contract specificity | Strongest. It lists exact edge inputs (`0x1p-2`, `1_000`, `"warming up\n3"`, 10 MiB overflow, 0.3 drift, the `a","b` collision) and names the guard tests it must keep green. | Specific but thin. There are 5–9 lines per task, the fingerprint contract is loose, and it has no NaN, inf or overflow cases. | Strong. It uses string equality to tie c-8's output to c-2's and adds tail-window and cwd probes. The probed facts about BurntSushi decoding are useful. |
| Granularity | Over-fragmented. The patcher is split in two (t-1/t-4), but t-1's span work mostly exists already (see D4). The run/verdict split (t-3/t-5) leaves t-3's real spawn tested only in t-11. t-15 is a doc-only task with no criterion. | Under-split. t-4 carries 7 criteria (evaluator, wiring and `task check` together), so a red gate on it is expensive to localise. | Balanced. Only t-13 (README plus one test) is too small. |
| Wave correctness | Correct, but t-15 is a tail task that only lengthens the chain. | Mostly correct. t-6 depending on t-3 is not strictly needed. | Best. It decouples consent from the fingerprint (consent takes a string), so t-3 runs in wave 1. One stall risk: t-1 changes patcher behaviour that lives in patch.go but omits patch.go and ARCHITECTURE.md from its files. |

**Skeleton: verification.** Its structure survives nearly intact: three parallel wave-1 foundations, an evaluator package with one renderer, wiring, then `task check`, then prompts. The core of each task comes from it. Its one wrong design call (inline-form patching, D1) is overruled with risk and mvp, and most contract grafts come from risk.

## Merged plan

Phase task-preconditions has 12 tasks across 5 waves.

```
Wave 1
  t-1  Patch nested precondition blocks inside task elements            [risk t-1+t-4, mvp t-1; verification t-1 overruled on form — D1, D4]
       files:    internal/project/patch_diff.go, internal/project/patch.go,
                 internal/project/patch_keyed_test.go, ARCHITECTURE.md
       covers:   c-5 (and every later Plan.Save once a task carries a precondition)
       desc:     Inside a keyed [[task]] element, a nested [[task.preconditions]] array is
                 diffed per sub-element. Unchanged produces no op; otherwise append, delete
                 and in-place key edits go inside the parent's span. qualify() stops
                 refusing a nested array under a keyed element. Deeper nesting and nesting
                 under unkeyed arrays stay refused.
       contract: - On a plan where t-2 has two precondition blocks, SetTaskStatus(t-1, done)
                   followed by Save changes exactly one line. Today it fails with "a table
                   nested inside an array-of-tables element cannot be patched".   [all three]
                 - Changing t-2's preconditions[1].max from 3 to 5 rewrites only that value,
                   and comments in t-2's other block stay byte-identical.   [risk]
                 - Adding the first precondition to t-2 writes it under t-2's header, before
                   t-3's [[task]] header. On reload t-3 has no preconditions.   [mvp+risk]
                 - Clearing t-2's preconditions deletes only its blocks, including a stray
                   `mx = 3` line. A comment between t-2 and t-3 survives byte-identical.   [risk+mvp]
                 - Appending a new element that carries preconditions renders them after its
                   [[task]] header, and the reload deep-equals the struct.   [mvp+verification]
                 - Span regression locks: `task move t-3 --before t-1` keeps both of t-3's
                   blocks on t-3. Deleting t-2 removes its blocks, and t-3's blocks do not
                   reattach to t-1. `task add --after t-1` lands after t-1's last sub-block.   [risk]
                 - A status change on an element whose preconditions are written inline
                   (`preconditions = [{…}]`) still saves. Changing that inline list is
                   refused by refuseInline, and the file is byte-unchanged.   [verification, adapted]
                 - A depth-2 table, or a table nested in an unkeyed array element, is still
                   refused by its dotted path. The existing "table nested in array element"
                   row stays green.   [all three]
                 - TestArchitectureNamesLosslessWriter stays green. The `apply` (patch.go:619)
                   and `diff` (patch_diff.go:111) anchors are repointed in this commit.   [risk]
       depends:  —

  t-2  Precondition schema, shape checks, set fingerprint               [verification t-2 + risk t-2 + mvp t-2]
       files:    internal/phase/phase.go, internal/phase/precondition.go (new),
                 internal/phase/precondition_test.go (new), internal/phase/plan_edit.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1 (and the c-4 fingerprint input)
       desc:     Adds phase.Precondition{Run, Max *float64, Min *float64, Timeout, Why} as
                 Task.Preconditions (`toml:"preconditions"`). PreconditionProblems is the
                 single shape check, and ValidatePlan includes it. ParsePrecondition is
                 strict. LoadPlan records undecoded keys under task.preconditions on the
                 Plan but does not fail the load. EffectiveTimeout defaults to 60s.
                 PreconditionSetLine(plan) produces the canonical consent line. Adds the
                 not_paths rows.
       contract: - Each of these is a problem that names the task id and precondition index:
                   run "" or "   ", a run starting with "-", max = nan, max = inf,
                   min = -inf, min 10 with max 5, timeout "soon", timeout "0s", timeout
                   "-1s".   [risk+verification]
                 - min == max is accepted. An integer `max = 5` decodes to 5.0. An empty
                   timeout gives EffectiveTimeout 60s, and "90s" gives 90s.   [all three]
                 - `mx = 3` (header or inline form) is recorded as unknown key "mx" after
                   LoadPlan. An unknown key outside task.preconditions is not recorded.
                   LoadPlan does not fail on either.   [risk; D2]
                 - ParsePrecondition(`{run = "x", bogus = 1}`) errors naming "bogus".   [verification]
                 - PreconditionSetLine changes when run, max, min, timeout or why changes on
                   any task, when a precondition is added or removed, when one moves to
                   another task, or when order within one task changes.   [risk+verification]
                 - PreconditionSetLine does NOT change for comments or reformatting,
                   `max = 3` vs `3.0`, `timeout = ""` vs `"60s"`, a task title or status
                   change, a task reorder (it is sorted by task id), or adding a task with
                   no preconditions.   [risk+verification; D6]
                 - A plan with no preconditions gives "".   [verification]
                 - Encoding is JSON: one precondition with run `a","b` does not collide with
                   two preconditions `a` and `b`.   [risk]
                 - TestEveryPathShapedFieldIsDeclared fails without the
                   phase.Precondition.{Run,Timeout,Why} not_paths rows.   [all three]
       depends:  —

  t-3  Per-phase precondition consent grant                             [verification t-3 + risk t-7 + mvp t-3]
       files:    internal/consent/store.go, internal/consent/preconditions.go (new),
                 internal/consent/preconditions_test.go (new),
                 internal/localstore/store_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-4
       desc:     Adds Grants.TrustedPreconditions as map[phase]sha256
                 (`trusted_preconditions`). PreconditionsConsented(store, repoDir, phase,
                 line) returns (State, error); the name ends in "Consented" so the exec audit
                 counts its callers as gating. Also adds GrantPreconditionsConsent and
                 PreconditionsRefusal. It takes the line as a string, so it does not depend
                 on t-2. New file, so the consent.go:114/:130 anchors do not move.
       contract: - grant(A, L) then Consented(A, L) returns Granted. (A, L') returns Stale
                   with ErrStaleConsent. (B, L) returns Absent with ErrNoConsent. A grant for
                   A never satisfies B, even with an identical line.   [verification+risk]
                 - Granting B leaves A's grant byte-identical (a keyed map, not an
                   overwrite).   [all three]
                 - A git-tracked local.toml is refused via RefuseTrackedLocal before any
                   read. An unparseable local.toml returns not-consented with a wrapped
                   sentinel, never Granted.   [verification+risk]
                 - PreconditionsRefusal text contains `dross trust --preconditions <phase>`
                   and distinguishes stale from absent.   [verification]
                 - TestLocalStoreRoundTripsGrants: trusted_preconditions survives a reload,
                   and a sibling grant's save does not drop it.   [mvp+verification]
                 - The not_paths row for consent.Grants.TrustedPreconditions is present.   [verification+risk]
       depends:  —

Wave 2
  t-4  Evaluate preconditions with one shared renderer                  [verification t-4 + risk t-3/t-5; D5]
       files:    internal/testlane/spawn.go, internal/precond/precond.go (new),
                 internal/precond/precond_test.go (new)
       covers:   c-2, c-3, c-9, c-10
       desc:     Extracts testlane.RunLine(ctx, dir, field, line, stdout, stderr) from
                 RunLocal. RunLocal delegates, so the spawn site stays the same one. Adds
                 precond.Evaluate(ctx, repoDir, []Precondition): it runs in order under
                 context.WithTimeout, stops at the first failure, and returns a Verdict
                 whose Render() is the one text that both c-2 and c-8 print. Stdout is
                 captured capped and kept apart from stderr. The tail is bounded by lines
                 and bytes.
       contract: - `echo boom; exit 3` fails. Render names "precondition 1", the run line,
                   its why, "exit 3" and "boom".   [verification+mvp]
                 - With 50 lines of output, the tail holds lines 31–50 and not line 30. A
                   10 MiB writer keeps stdout at or under the cap and fails a bounded
                   precondition as unparseable.   [verification+risk]
                 - With max 5, `echo 5` passes and `echo 5.0001` fails with "measured
                   5.0001, bound max 5". With min 80, `echo 80` passes and `echo 79.9`
                   fails. Measured 0.3 against `max = 0.3` passes.   [verification+risk]
                 - Each of these fails as "unparseable", quotes the printed text, and fails
                   against both max 5 and min -1 (proof of no coercion to 0): `''`,
                   `12 ms`, `1\n2`, `NaN`, `Inf`, `+Inf`, `infinity`, `0x1p-2`, `1_000`,
                   `1,234`, `3%`, `warming up\n3`.   [verification+risk]
                 - A measurement that prints 3 and exits 1 fails on "exit 1" and is never
                   parsed. A precondition with no bound ignores stdout, so `echo abc` with
                   exit 0 passes.   [all three]
                 - Only stdout is parsed: `echo 3; echo junk >&2` with max 5 passes, and
                   both lines appear in the tail.   [verification+risk]
                 - `sleep 30` with a 200ms timeout fails. Render contains "timed out after
                   200ms" and no "exit", and Evaluate returns within the timeout plus
                   WaitDelay.   [verification+risk]
                 - A spawn error with no exit code is never classified as exit 0.   [risk]
                 - It runs at repoDir: `test -f marker` passes while the process cwd is a
                   subdirectory without the marker.   [verification+risk]
                 - The first failure stops the run: a second `touch ran` precondition leaves
                   no `ran` file.   [verification]
                 - The existing `dross test` RunLocal tests stay green, and the
                   runtime.test_command fence label is preserved.   [verification]
       depends:  t-2

  t-5  Report malformed preconditions in dross validate                 [verification t-5 + risk t-6]
       files:    internal/cmd/validate.go, internal/cmd/validate_precondition_test.go (new)
       covers:   c-1
       desc:     For each phase plan, appends PreconditionProblems and the recorded unknown
                 task.preconditions keys to validate's ✗ list, prefixed by the plan path. It
                 never spawns anything.
       contract: - A plan with `run = ""` makes validate exit non-zero with "✗ <plan path>
                   task t-1 precondition 1: run is empty".   [verification+mvp]
                 - `bogus = 1` (header or inline form) gives a ✗ naming "bogus". `max = nan`
                   gives a ✗ naming the non-finite bound.   [verification+risk]
                 - A plan whose precondition is `touch marker` validates with no marker
                   created.   [risk]
                 - A well-formed plan with `max = 3` and no timeout validates clean.   [risk+verification]
       depends:  t-2

  t-6  Add dross trust --preconditions <phase>                          [verification t-6 + risk t-9 + mvp t-3]
       files:    internal/cmd/trust.go, internal/cmd/trust_preconditions_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-4
       desc:     Mirrors trustReplay. It prints every task id, run, bounds and timeout
                 before writing, then grants the PreconditionSetLine fingerprint. --check is
                 silent on success. A phase with no preconditions gets "nothing to trust".
                 The README trust row names the flag.
       contract: - Even with an unwritable store, the precondition listing still prints,
                   which proves it prints before writing.   [risk]
                 - --check exits 0 after a grant. After one precondition's max is rewritten,
                   --check exits non-zero naming "stale" and `dross trust --preconditions
                   <phase>`. With no grant it names "absent".   [all three]
                 - A phase with no preconditions errors "nothing to trust", and local.toml
                   is unchanged.   [all three]
                 - A tracked local.toml is refused with no write. Granting phase B leaves
                   phase A's --check at exit 0.   [verification+risk]
                 - `dross local set trusted_preconditions x` fails with "unknown local key".   [risk]
                 - cli_tree.txt carries the `--preconditions` flag row, and the README
                   `dross trust` row names `--preconditions <phase-id>`.   [risk+verification]
       depends:  t-2, t-3

  t-7  Set and clear preconditions via task add/edit/show               [verification t-7 + risk t-10 + mvp t-5]
       files:    internal/phase/plan_edit.go, internal/cmd/task.go,
                 internal/cmd/task_precondition_edit_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-5, c-1
       desc:     NewTask and TaskEdit gain Preconditions. `--precondition '<TOML inline
                 table>'` (StringArray, repeatable) goes on both add and edit; on edit it
                 replaces the list. `--add-precondition` appends and
                 `--clear-preconditions` empties; conflicting combinations are refused. A
                 change prints that the phase grant is revoked. `show` prints the
                 preconditions in text and in `--json`. The README `dross task` row names
                 the flags.
       contract: - `task add --precondition '{run = "go test ./x", max = 5}'` reloads with
                   that run and max, and every other task's bytes are unchanged.   [all three]
                 - `{ run = "a, b && c", max = 3 }` keeps the comma: run decodes to exactly
                   "a, b && c".   [risk+verification]
                 - `{run = ""}`, `{run = "x", bogus = 1}`, `{run = "x", timeout = "soon"}`
                   and malformed TOML are each refused, naming the key or the flag, with
                   plan.toml byte-unchanged. On the same malformed fixtures, `dross
                   validate` and `task edit` refuse the same cases.   [all three + risk t-6 parity]
                 - edit `--precondition` replaces the list, `--add-precondition` appends,
                   and `--clear-preconditions` leaves the task with no preconditions,
                   removing a stray unknown key inside the blocks as well.
                   `--clear-preconditions` combined with either other flag is refused.   [risk+verification]
                 - An edit that changes preconditions makes the old grant Stale and prints
                   `dross trust --preconditions <phase>`. A `--title`-only edit prints no
                   revocation line.   [verification]
                 - `task show` lists run, bounds, timeout and why. `--json` carries a
                   `preconditions` array.   [risk+verification]
                 - task edit has no flag that marks a precondition as skipped (no_bypass).   [verification]
                 - cli_tree.txt carries the new add/edit flags.   [all three]
       depends:  t-1, t-2

  t-8  Keep board pull from starting precondition tasks                 [verification t-8 + risk t-8; mvp has none — D3]
       files:    internal/boardsync/task_pull.go, internal/boardsync/task_pull_test.go
       covers:   c-7, c-11
       desc:     In ReportTaskMoves with --apply, a board move from pending or failed into
                 in_progress, for a task that has preconditions, is reported but not
                 written. The agreement point is not advanced, and the output names
                 `dross task status <phase> <id> in_progress`. Clean moves still apply, and
                 the command exits non-zero at the end.
       contract: - A board drag of a pending task with preconditions to task-in-progress
                   leaves the plan status pending and the board.json link unchanged, prints
                   the `task status` route, and exits non-zero.   [risk+verification]
                 - The same drag for a task without preconditions still applies.   [both]
                 - in_progress → done for a task with preconditions still applies, as do
                   the other clean moves in the same --apply run.   [both]
       depends:  t-2

Wave 3
  t-9  Gate task status in_progress on preconditions                    [verification t-9 + risk t-11 + mvp t-4]
       files:    internal/cmd/task.go, internal/cmd/task_precondition.go (new),
                 internal/cmd/task_precondition_test.go (new),
                 internal/phase/plan_lossless_test.go
       covers:   c-2, c-3, c-4, c-9, c-10, c-11
       desc:     The order is requireExecConsent → load → task exists →
                 refuseSecondInProgress. If the task is already in_progress, it skips the
                 rest. Otherwise, when the task has preconditions: shape and unknown-key
                 problems are checked, then PreconditionsConsented, then precond.Evaluate.
                 On pass it re-loads, re-checks the status and fingerprint, and saves. Exit
                 3 means a precondition refusal; exit 4 means a missing or stale grant. The
                 shared helper runTaskPreconditions is reused by t-11.
       contract: - `echo boom; exit 3` exits 3. plan.toml is byte-identical (the task stays
                   pending), and the output has "precondition 1", "exit 3" and "boom". A
                   failed task stays failed with its reason byte-identical.   [all three]
                 - `echo 7` with max 5 is refused "measured 7, bound max 5". `echo 5` with
                   max 5 moves the task to in_progress. `echo '12 ms'` is refused as
                   unparseable. `sleep 30` with a 200ms timeout is refused naming "timed out
                   after 200ms".   [verification+mvp]
                 - With no grant: exit 4 naming `dross trust --preconditions <phase>`, and
                   no `ran` file. After a grant and a hand-edited run line: exit 4 as stale,
                   and `ran` is still absent.   [all three; exit codes risk — D9]
                 - A malformed or unknown-key precondition refuses before the consent check
                   and before any spawn.   [risk; D2]
                 - A task with no preconditions, in a phase with no precondition grant,
                   still goes to in_progress.   [risk+verification]
                 - c-11: a task already in_progress with a failing precondition and no grant
                   re-sets with exit 0, and the marker stays absent. From failed or pending
                   the preconditions re-run.   [all three]
                 - A second task's refuseSecondInProgress fires before any precondition
                   spawns.   [verification]
                 - A seam that sets another task in_progress between the run and the save
                   makes the command refuse, and plan.toml keeps that concurrent write.   [risk — D9]
                 - Run from a subdirectory, `test -f go.mod` passes.   [risk]
                 - The `task status` flags are exactly {--reason}, with no bypass flag.   [verification]
                 - SetTaskStatus followed by Save, on a plan whose task carries
                   preconditions, changes only the status line.   [verification]
                 - TestEverySpawnSiteGatedOrExempt stays green, with no new
                   //dross:exec-exempt.   [all three]
       depends:  t-1, t-3, t-4

  t-10 Teach planner and plan review about preconditions                [verification t-10 + mvp t-6 + risk t-13]
       files:    assets/prompts/plan.md, assets/prompts/plan-review.md,
                 internal/cmd/plan_prompt_test.go
       covers:   c-6
       desc:     plan.md gets a `preconditions` row in the §2 field table: any "before X,
                 check Y" gate becomes a precondition via `dross task add/edit
                 --precondition`, never prose. Prose is trimmed to stay ≤ 15000 bytes (it is
                 14857 today). plan-review.md gets a "Prose gates" check: a task whose
                 description or test_contract states a gate with no precondition is
                 BLOCKING (D7).
       contract: - The expanded §2 field table has a `preconditions` row naming "before X,
                   check Y" and `--precondition`. A test fails if either is dropped.   [all three]
                 - plan-review.md has a "Prose gates" check at BLOCKING that names
                   "precondition".   [mvp; severity D7]
                 - TestPromptByteBudget: plan.md ≤ 15000 with the cap not raised. Every
                   conservation_test row for plan.md is still present.   [all three + verification]
                 - The new test reads through expandedPrompt, and rawPromptReadAllowlistSize
                   stays 19.   [risk+verification]
       depends:  t-7

Wave 4
  t-11 Add dross task check <phase> <task>                              [verification t-11 + risk t-12]
       files:    internal/cmd/task.go, internal/cmd/task_check_test.go (new),
                 cmd/dross/testdata/cli_tree.txt, README.md
       covers:   c-8, c-4
       desc:     A read-only verb: it resolves the task, then calls PreconditionsConsented
                 and runTaskPreconditions, and prints Render(). It uses the same exit codes 3
                 and 4. It never saves and evaluates regardless of status. Gated through
                 PreconditionsConsented, not added to execGatedCommands. The README task
                 brace list names `check`.
       contract: - On t-9's failing fixture, the output and exit code equal those of
                   `task status … in_progress` (string equality).   [verification+risk]
                 - plan.toml bytes are identical after a pass, a fail, and a check on an
                   in_progress or done task.   [all three]
                 - With no grant: exit 4 naming `dross trust --preconditions`, and the `ran`
                   marker is absent.   [risk+verification]
                 - A task with no preconditions exits 0 saying it has none.   [risk+verification]
                 - The exec audit lists `task check` as gated on the testlane site with no
                   ungatedVia. TestExecGatedSetIsExplicit stays green.   [risk+verification]
                 - cli_tree.txt has the `dross task check` row, and the README task brace
                   list contains `check`.   [risk+verification]
       depends:  t-9

Wave 5
  t-12 Stop execute on a precondition refusal                           [verification t-12 + risk t-14 + mvp t-7]
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 assets/prompts/_execute.board-moves.md, internal/cmd/execute_prompt_test.go
       covers:   c-7
       desc:     In §1a, the issue-sync line runs only after `task status … in_progress`
                 exits 0. On exit 3 in pair mode, AskUserQuestion offers fix (fix the
                 condition, or `dross task edit` plus a user-run re-trust, then `dross task
                 check`), replan, or fail (`task status … failed --reason`). On exit 3 in
                 solo mode, the run halts. On exit 4, it stops and the user runs `dross
                 trust --preconditions`. The board-moves partial routes a refused gated
                 move to `task status`.
       contract: - In §1a, issue sync runs only on status exit 0.
                   TestTaskEdgesPairTheBoardStatusWithThePlanStatus stays green.   [risk+verification]
                 - The pair arm offers exactly fix / replan / fail and names `dross task
                   check`.   [all three]
                 - The expanded solo partial says a refusal halts, with no `dross task
                   next`, `task edit` or `--clear-preconditions` in that arm.   [verification+risk]
                 - The exit-4 arm asks the user to run `dross trust --preconditions`.
                   TestPromptsNeverGrantConsentForTheUser stays green.   [risk+mvp]
                 - Across every expanded assets/prompts and assets/agents file, the only
                   `task status … in_progress` is execute.md §1a. No prompt starts a task by
                   hand-editing plan.toml or with `issue task pull --apply`, and none names
                   skip, force or bypass.   [all three]
                 - TestPromptByteBudget, TestTestStepPromptsNotGrown (25854) and
                   TestExecuteAbsorbedProseDeleted (25500) pass with no pin raised.   [risk+verification]
       depends:  t-6, t-8, t-11
```

Coverage of the merged plan: c-1 → t-2, t-5, t-7 · c-2 → t-4, t-9 · c-3 → t-4, t-9 · c-4 → t-3, t-6, t-9, t-11 · c-5 → t-1, t-7 · c-6 → t-10 · c-7 → t-8, t-12 · c-8 → t-11 · c-9 → t-4, t-9 · c-10 → t-4, t-9 · c-11 → t-9, t-8. All 11 are covered.

Dropped from the drafts:
- risk t-15 (the ARCHITECTURE.md entry) — see D8.
- verification t-13 (the README task): its rows are folded into t-6, t-7 and t-11 — see D8.

Merged:
- risk t-1+t-4 → t-1 (D4).
- risk t-3+t-5 → t-4 (D5).

## Disagreements

**D1 — Precondition patching: header form or inline form.** This is the most consequential divergence.
- **risk and mvp:** patch `[[task.preconditions]]` header blocks inside the task's span.
- **verification:** write an inline `preconditions = [{…}]` leaf, and refuse any change to a header-form list.
- **Default:** header form. The repo decides it:
  - On a fresh file, `saveLossless` writes through `encodeFresh` (project.go:563), and BurntSushi encodes a `[]struct` as header blocks. Every plan.toml first written with preconditions would therefore be header form.
  - Under verification's design, `task edit --precondition` would then refuse it, and c-5 ("never needs a hand edit") fails for the most common case.
  - Separately, `refuseInline` (patch.go:697) refuses a key op on an inline array. Verification's t-1 omits patch.go from its files, so the writer would stall.
- **Sub-divergence within header form:**
  - **risk:** in-place sub-element ops.
  - **mvp:** replace the element's nested blocks wholesale.
  - **Default:** risk's in-place ops. Wholesale replacement drops comments inside the blocks, which regresses the lossless guarantee for the very blocks users annotate.
- **Why it matters:** the wrong choice either breaks c-5 or ships a writer stall in wave 1, which blocks t-7 and t-9.

**D2 — Where an unknown precondition key is caught.**
- **mvp:** LoadPlan rejects it.
- **verification:** only `dross validate` sees it. The run path ignores it.
- **risk:** LoadPlan records it, and both validate and the start gate refuse on it. saveIfValid never does.
- **Default:** risk's approach.
  - mvp's approach makes a typo brick the whole plan: no CLI verb can load it to `--clear-preconditions` it, so the only fix is a hand edit, which goes against no_bypass.
  - verification's approach lets `mx = 3` silently turn a bounded measurement into an exit-0 check that passes. That is a false green at the gate this phase exists to build.
- **Why it matters:** it decides whether a one-character typo fails closed, fails bricked, or passes silently.

**D3 — The board-pull route into in_progress.**
- **risk and verification:** both include a guard (t-8).
- **mvp:** has none and argues only from the criteria list.
- **Default:** include. `ReportTaskMoves --apply` calls `plan.SetTaskStatus(…, in_progress)` directly (task_pull.go:167), and `_execute.board-moves.md` has the agent run it after the user accepts. Without t-8, c-7's "no prompt path starts the task by another route" is false on the first board drag.
- **Why it matters:** it is a real bypass of the whole gate.

**D4 — One patcher task or two, and whether ARCHITECTURE.md belongs in it.**
- **risk:** splits span ownership (t-1) from the nested diff (t-4).
- **mvp and verification:** one task.
- **Default:** one task, keeping risk's span contracts as regression locks. Risk's t-1 premise is mostly already handled in the code:
  - `elemSpan` (patch.go:949) already extends an element over any sub-header that shares its seg prefix (`hasSegPrefix`).
  - `indexDoc` numbers a nested array per parent element (`qualifiedKey`).
  - What actually refuses is `qualify()` (patch.go:676) and `differ.nested` (patch_diff.go:183), and both belong to one change.
- **Risk is right that ARCHITECTURE.md must be in the task's files.** `TestArchitectureNamesLosslessWriter` resolves the `apply` (patch.go:619) and `diff` (patch_diff.go:111) anchors against source lines. mvp and verification omitted the file, and a writer confined to task.files could not repoint the anchors.
- **Why it matters:** the default saves one commit-gate cycle; leaving out the file would cause a stall.

**D5 — Evaluator structure and spawn seam.**
- **risk:** `internal/precondition`, with an injected spawn func (no os/exec) plus a separate verdict task. The real RunLocal is wired only in t-11.
- **mvp:** everything goes in `internal/cmd/precondition.go`, together with wiring and `task check`, in one 7-criterion task.
- **verification:** `internal/precond` uses a new `testlane.RunLine` extracted from RunLocal. It is the same spawn site, with a per-field fence label.
- **Default:** verification's design, with risk's parse and overflow contracts grafted on.
  - Real `sh` in the evaluator's own tests catches the timeout and cwd behaviour that a fake spawn hides until wave 3.
  - The field parameter fixes the fence that would otherwise mislabel a precondition refusal as `runtime.test_command`. Risk instead relies on shape validation reaching it first.
  - mvp's single task is too large to localise a red gate.
- **Why it matters:** it decides when timeout and kill behaviour is first proven, and whether the exec audit sees a new site. None of the three designs adds one.

**D6 — Fingerprint ordering and home.**
- **risk:** plan order, built in the consent package from the Plan, so a `task move` revokes the grant.
- **verification:** sorted by task id and built in phase. Consent takes a plain string, so a move does not revoke.
- **mvp:** in phase, ordering unspecified.
- **Default:** sorted by task id, keeping order within a task, JSON-encoded (risk's collision-proof encoding), and living in phase. This lets t-3 run in wave 1.
- **Why it matters:** with plan order, a harmless reorder forces a re-trust. Too loose an encoding could let two different sets share a hash.

**D7 — Severity of the prose-gate review finding.**
- **mvp:** BLOCKING ("as a FLAG it gets waved through").
- **verification:** FLAG.
- **risk:** "a finding", with no class.
- **Default:** BLOCKING. The phase exists because prose gates get ignored, and the fix (`task edit --precondition`) is cheap.
- **Cost:** a false positive from the reviewer's heuristic forces a replan cycle. If that proves noisy, step it down to FLAG.

**D8 — Standalone documentation tasks.**
- **risk:** t-15 re-anchors ARCHITECTURE.md and adds an entry. It covers no criterion.
- **verification:** t-13 is a README task with a pinning test.
- **mvp:** neither ("no criterion names them").
- **Default:** drop both as standalone tasks, and fold the README rows into the CLI tasks (t-6, t-7, t-11), as risk did.
- **Evidence:** outside the Configuration entry (handled in t-1), anchor drift is only reported by `dross doctor` and `dross architecture check`. No test enforces it, and no repo-wide test checks the README's command surface, so t-15 buys a doctor-clean tree for a commit-gate cycle. If you want the precondition ARCHITECTURE entry, it is risk's t-15 verbatim as a wave-5 tail.

**D9 — Distinct exit codes and a re-load before save (risk only).**
- **risk:** exit 3 means a precondition refusal and exit 4 means a missing or stale grant. Risk also re-loads the plan after a run, which can take minutes, before saving.
- **verification:** its execute prompt treats any non-zero exit as a refusal and puts consent under "fix".
- **mvp:** saves the plan it loaded before the run.
- **Default:** both of risk's points are grafted into t-9, t-11 and t-12.
- **Why it matters:**
  - Without distinct codes, the agent must parse error text to tell "run `dross trust`" from "the measurement failed". Confusing the two invites the agent to treat a consent refusal as a fixable failure.
  - Without the re-load, a long run that saves a stale plan reverts concurrent edits, and widens the second-in-progress window from milliseconds to minutes.

**D10 — A trust instruction in plan.md at lock.**
- **mvp:** plan.md tells the user to run `dross trust --preconditions` after lock (a "let them run" line).
- **verification:** rejects this; plan.md has about 143 bytes of budget, and the refusal text already names the command.
- **risk:** omits it.
- **Default:** omit. t-12's exit-4 arm surfaces the instruction at first start.
- **Why it matters (minor):** the first start of every gated phase pauses once for the grant.

**D11 — Minor shape points.**
- **TOML key:**
  - **risk:** `precondition` (singular, like `[[task]]`).
  - **mvp and verification:** `preconditions`.
  - **Default:** `preconditions`, matching Task's other list fields.
- **`--add-precondition`:**
  - **risk and verification:** included.
  - **mvp:** omitted.
  - **Default:** include.
- **Why it matters:** these are cheap now and expensive to rename after plans carry them.
