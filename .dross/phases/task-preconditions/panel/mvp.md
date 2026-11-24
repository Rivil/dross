# task-preconditions — mvp lens

Phase task-preconditions — 7 tasks across 4 waves

```
Wave 1
  t-1  Patch nested task.preconditions tables losslessly
       files:    internal/project/patch_diff.go, internal/project/patch.go,
                 internal/project/patch_keyed_test.go
       covers:   c-2, c-5
       desc:     The lossless patcher refuses any table nested in an array-of-tables
                 element, and it refuses even when that table is unchanged, so with the
                 patcher as it is, a plan that carries one precondition can't save a status
                 change. Teach it to handle an array-of-tables nested in a KEYED element
                 (task.preconditions): unchanged → no op; changed → replace that
                 element's nested blocks; an appended element renders its nested blocks.
       contract: - a keyed doc whose [[task.preconditions]] is unchanged but whose
                   task.status changes produces exactly one key op and saves (the status save
                   currently fails with "a table nested inside an array-of-tables element")
                 - adding a nested block to the t-2 element lands under t-2's [[task]]
                   header, before t-3's header; decoding the result gives t-3 no preconditions
                 - clearing t-2's nested blocks removes only t-2's [[task.preconditions]]
                   headers; a comment line between t-2 and t-3 survives byte-identical
                 - appending a new element that has nested blocks writes them after its
                   [[task]] header, and the reload equals the saved struct
                 - a table nested in an UNKEYED array element is still refused (the
                   existing patch_test "table nested in array element" row stays green)

  t-2  Precondition schema, validation and set fingerprint
       files:    internal/phase/phase.go, internal/phase/plan_edit.go,
                 internal/cmd/validate.go, internal/phase/plan_edit_test.go,
                 internal/cmd/validate_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1
       desc:     Add phase.Precondition {run, max *float64, min *float64, timeout, why}
                 and Task.Preconditions. LoadPlan rejects undecoded keys under
                 task.preconditions. A shared PreconditionProblems runs from ValidatePlan
                 (task add/edit) and from `dross validate`, and checks for a blank run, a
                 timeout that is not a positive Go duration, and min > max.
                 (*Plan).PreconditionSet() gives the canonical byte string that consent
                 fingerprints.
                 Also add NewTask.Preconditions and TaskEdit.Preconditions
                 (*[]Precondition: nil means unchanged, empty means clear). File the rows
                 phase.Precondition.{Run,Timeout,Why} in not_paths.txt.
       contract: - `dross validate` on a plan with a precondition `run = "  "` prints a ✗
                   naming the task id and exits non-zero
                 - `dross validate` on a precondition carrying `command = "x"` (unknown
                   key) exits non-zero and the message names `command`
                 - timeout = "soon", timeout = "-1s" and min = 5 / max = 1 each produce a
                   ValidatePlan problem naming the task id; timeout = "90s" with max only
                   is clean
                 - `max = 120` (integer literal) decodes to 120.0 rather than failing to load
                 - PreconditionSet changes when any run, bound or timeout of any task in the
                   phase changes, or when a precondition is added; it is identical across
                   two loads of the same file
                 - TestEveryPathShapedFieldIsDeclared stays green with the three new rows

Wave 2 (t-3 depends t-2; t-5 depends t-1, t-2)
  t-3  Per-phase precondition consent and trust flag
       files:    internal/consent/consent.go, internal/consent/store.go,
                 internal/cmd/trust.go, internal/consent/consent_test.go,
                 internal/cmd/trust_test.go, internal/localstore/store_test.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-4
       desc:     Grants.TrustedPreconditions map[phase]sha256(PreconditionSet).
                 consent.PreconditionsConsented(store, phase, set) returns (state, error)
                 with absent and stale kept apart (its name ends "Consented" for the
                 exec-audit gating rule); GrantPreconditionsConsent records one.
                 `dross trust --preconditions <phase> [--check]` mirrors --replay: it
                 prints the full set before writing, refuses a phase with no
                 preconditions, and RefuseTrackedLocal runs first.
       contract: - granting phase A then phase B leaves A's grant in place (keyed map,
                   not overwrite)
                 - after a grant, editing one precondition's run (or adding one) turns
                   PreconditionsConsented to stale; an untouched phase stays granted
                 - `trust --preconditions x --check` exits non-zero with no grant, the
                   message contains `dross trust --preconditions x`, and it prints nothing
                   on success
                 - `trust --preconditions x` on a phase with no preconditions errors
                   "nothing to trust" and writes no local.toml key
                 - TestLocalStoreRoundTripsGrants: trusted_preconditions survives a
                   reload, and other grants survive a preconditions grant

  t-5  task add/edit --precondition and --clear-preconditions
       files:    internal/cmd/task.go, internal/cmd/task_lossless_test.go,
                 cmd/dross/testdata/cli_tree.txt
       covers:   c-5
       desc:     `--precondition '<toml inline table>'` (StringArray, repeatable) on add
                 and edit. It is decoded strictly, so an unknown key is refused before
                 anything is written. On edit it replaces the whole list, and
                 `--clear-preconditions` empties it; passing both is refused. Everything
                 is validated through saveIfValid (t-2).
       contract: - `task add --precondition '{run = "test -f x", why = "x exists"}'` writes
                   a [[task.preconditions]] under the new task and reloads with run/why intact
                 - `task edit t-2 --precondition '{run = "echo 3", max = 5}'` on a plan with
                   comments leaves every other task and every comment byte-identical
                 - `task edit t-2 --clear-preconditions` leaves t-2 with zero preconditions
                   and the rest of the file byte-identical
                 - `--precondition '{run = "x", maxx = 1}'` and `--precondition '{run = ""}'`
                   fail and plan.toml is byte-unchanged
                 - `--precondition` + `--clear-preconditions` together is refused

Wave 3 (t-4 depends t-1, t-2, t-3; t-6 depends t-3, t-5)
  t-4  Run preconditions on start; add task check
       files:    internal/cmd/precondition.go (new), internal/cmd/precondition_test.go (new),
                 internal/cmd/task.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-2, c-3, c-4, c-8, c-9, c-10, c-11
       desc:     runPreconditions acts on PreconditionsConsented first, then runs each
                 precondition through testlane.RunLocal at the repo root with a
                 context.WithTimeout (default 60s). A non-zero exit or a timeout fails.
                 A bounded precondition parses trimmed stdout as one float and fails
                 when value > max or value < min. Bounds are inclusive. A value that
                 doesn't parse fails and quotes what was printed. `task status … in_progress`
                 calls it only when the task is not already in_progress, before the save.
                 New `task check <phase> <task>` prints the same verdict and never writes.
       contract: - `exit 3` after printing 30 lines → status in_progress errors, plan.toml
                   byte-identical (task still pending), output names the precondition,
                   "exit 3", and the last lines of its output but not the first
                 - max = 120: `echo 120` starts the task; `echo 121` refuses with
                   "measured 121" and "max 120"; min = 80: `echo 80` passes, `echo 79.9`
                   refuses
                 - `echo abc`, empty stdout and `echo "1 2"` under a bound each refuse
                   as unparseable quoting the printed text; empty is never read as 0
                 - a bounded precondition that prints 1 (within bound) but exits 1
                   refuses on the exit code
                 - timeout = "200ms" on `sleep 5` refuses in well under 5s and names
                   "timed out after 200ms"
                 - no grant / grant then edit run: refused with
                   `dross trust --preconditions <phase>`, and the run's sentinel
                   `touch ran` file does not exist
                 - task already in_progress + failing precondition: re-set succeeds and
                   the sentinel counter is not bumped; from failed and from pending the
                   counter bumps (re-run)
                 - `task check` on a failing precondition exits non-zero with verdict
                   text equal to the status refusal's; on pass exits 0; plan.toml
                   byte-identical in both cases
                 - TestEverySpawnSiteGatedOrExempt stays green, and repoExecGraph
                   marks cmd.runPreconditions as gating

  t-6  Plan + plan-review prompts encode gates as preconditions
       files:    assets/prompts/plan.md, assets/prompts/plan-review.md,
                 internal/cmd/plan_prompt_test.go
       covers:   c-6
       desc:     plan.md: add a `preconditions` row to the §2 field table saying any
                 "before X, check Y" gate becomes a precondition via
                 `dross task add/edit --precondition`, never prose. At lock, a plan with
                 preconditions asks the user to run `dross trust --preconditions <phase>`
                 (let them run it). Trim existing prose so plan.md stays ≤ 15000 bytes
                 (it is 14857 now). plan-review.md: new check "Prose gates" — a task
                 whose description or test_contract states a before/only-if gate with no
                 precondition = BLOCKING.
       contract: - the expanded plan.md §2 field table has a `preconditions` row naming
                   `--precondition`; a test fails if the row or the flag name is dropped
                 - plan.md names `dross trust --preconditions` only on a line containing
                   "let them run"
                 - plan-review.md's reviewer brief has a "Prose gates" check classed
                   BLOCKING that names "precondition"
                 - TestPromptByteBudget stays green for plan.md

Wave 4 (depends t-4)
  t-7  Execute stops on a precondition refusal
       files:    assets/prompts/execute.md, assets/prompts/_execute.solo.md,
                 internal/cmd/execute_prompt_test.go
       covers:   c-7
       desc:     §1a: if `dross task status … in_progress` refuses on a precondition or its
                 consent, don't start the task or sync its card. Never set it
                 in_progress another way, and never loosen or remove the precondition to
                 get past it. Pair: AskUserQuestion fix / replan / fail, where fail runs
                 `task status … failed --reason "precondition: …"`. A consent refusal asks
                 the user to run `dross trust --preconditions` (let them run it). Solo
                 partial: halt the run and report the refusal.
       contract: - execute.md §1a contains the refusal arm with the labels fix, replan and
                   fail, placed before the `dross issue task sync` line
                 - the expanded solo partial says a precondition refusal halts the run
                 - across every expanded prompt in assets/prompts and assets/agents,
                   `task status … in_progress` appears only in execute.md §1a, so no
                   second start route exists
                 - TestPromptsNeverGrantConsentForTheUser stays green with the new
                   `dross trust --preconditions` line
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-4 (t-1 makes a plan with preconditions saveable) |
| c-3 | t-4 |
| c-4 | t-3 (grant + stale detection), t-4 (refusal, nothing run) |
| c-5 | t-5 (t-1 is the patcher capability it needs) |
| c-6 | t-6 |
| c-7 | t-7 |
| c-8 | t-4 |
| c-9 | t-4 |
| c-10 | t-4 |
| c-11 | t-4 |

11/11 covered.

## Judgment calls

- **Patcher extension (t-1) instead of a full-encode fallback in Plan.Save.** The fallback is smaller. I rejected it because it silently drops comments from any plan that carries a precondition, which regresses the lossless-plan guarantee the task verbs shipped on. Leaving the patcher alone isn't an option: it refuses an UNCHANGED nested table (`differ.change` → `nested()`), so status saves break.
- **Unknown-field rejection in LoadPlan (Undecoded keys under task.preconditions), not a separate validate pass.** One check then covers validate, task status and task check, and a typo'd bound can't be silently ignored by a run. I rejected strict decoding of the whole plan because it widens scope to fields no criterion names.
- **The spawn reuses testlane.RunLocal under context.WithTimeout. No new os/exec site.** internal/cmd holds no os/exec by design. RunLocal already has WaitDelay for kill-on-timeout. I rejected a new testlane.RunPrecondition: it's an extra spawn site for the exec-consent audit, and the only gain is a fence label.
- **Fingerprint covers run + bounds + timeout for every task, via (*Plan).PreconditionSet.** The locked decision says "any add or edit revokes", and loosening a bound is an edit. Fingerprinting run lines only would let a max be loosened without re-consent.
- **TrustedPreconditions is a phase-keyed map, not a comma-separated set.** It mirrors TrustedLaneCommands: one phase going stale refuses only itself, and stale is told apart from absent. A set can't say "changed since consent", which c-4 distinguishes.
- **Precondition consent stays out of execGatedCommands.** That roster is the runtime.test_command grant family, and the replay grant isn't in it either. The precondition gate lives inside runPreconditions, so the exec audit sees it whichever command reaches it.
- **CLI form is `--precondition '<toml inline table>'` plus `--clear-preconditions`.** I rejected per-field flags (--precondition-run/--precondition-max…) because they can't pair fields across a repeated list. I rejected a JSON flag because plan.toml is TOML and the strict TOML decode gives c-1's unknown-key refusal for free.
- **Prose-gate review finding is BLOCKING, not FLAG.** A prose gate can't stop a task from starting, which is the whole point of this phase and the v1.9 criterion. As a FLAG it would get waved through.
- **c-2/c-3/c-4(refusal)/c-8–c-11 merged into one task (t-4).** They're one evaluator with two thin callers (status, check). Splitting the evaluator from its wiring would leave a task whose only test is the next task's. I kept consent (t-3) separate because it touches a different layer (consent + trust + localstore).
- **Prompts split in two (t-6 plan side, t-7 execute side).** Merging them would put 7 files across two command surfaces with different dependencies (t-6 needs the CLI flags; t-7 needs the refusal output).
- **No README/doctor/roadmap task.** No criterion names them. The README success-criterion row is ticked at milestone close.
- **plan.md budget handled inside t-6 by trimming, not by a new partial.** A new partial needs a rawPromptReadAllowlist entry, and that list is pinned "may only shrink". Gate guidance must apply on every plan, not behind a conditional fetch.
