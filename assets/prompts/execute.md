# /dross-execute

Run a phase plan to completion. **Pair-mode by default**: propose, pause, steer, then write. `--solo` opts into autonomous execution for trivial phases.

## 0. Pre-flight (run once)

1. Run `dross rule show` and treat output as MUST-FOLLOW.
2. Resolve target phase from `$ARGUMENTS` or `state.json`'s `current_phase`. Fail if neither is set.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`. If `plan.toml` is missing, route the user to `/dross-plan` and stop.
4. Read `.dross/project.toml` — specifically `runtime.*` (test/typecheck/lint commands), `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
5. Check git state with `git status --porcelain`. If working tree is dirty:
   - Surface the diff to the user
   - Ask: "Commit existing work first / stash / abort"
   - Do not proceed with execute on a dirty tree — atomic commits per task require a clean baseline.
6. Parse flags from `$ARGUMENTS`:
   - `--solo` → autonomous mode
   - `--from <task-id>` → start at this task (skip earlier-wave done tasks; resume mid-phase)
7. Detect resume state: if any task has `status = "in_progress"`, ask user "continue task X / reset to pending and pick fresh".

Print one orientation block:
```
Executing phase <id> (<title>)
Plan: <task-count> tasks across <wave-count> waves — <pending> pending, <done> done, <failed> failed
Mode: <pair | solo>
Test command: <runtime.test_command or "(none — verify will catch this later)">
```

## 1. Per-task loop

Repeat until `dross task next <phase>` returns nothing:

### 1a. Pick

```
TASK_ID=$(dross task next <phase>)
```

If empty: jump to step 2. Otherwise mark in progress:

```
dross task status <phase> $TASK_ID in_progress
```

Read the task with `dross task show <phase> $TASK_ID`. Display its full record to the user.

### 1b. Code insight (without codex — use file/git tools directly)

For each file in `task.files`:
- If it exists: `Read` it and surface key symbols/structure (just what's relevant).
- If it doesn't exist: note as "new file".
- `git log -n 5 --oneline -- <file>` to surface recent activity in that area.

Look for **sibling patterns** the task should mirror:
- For each existing file in `task.files`, list other files in the same directory via `Bash` (`ls`).
- If 1-2 look relevant, `Read` them briefly to extract pattern (validation shape, error format, exports).

Don't dump everything — surface the 3-5 most useful observations.

### 1c. Propose approach

In one block, write 3-7 lines covering:
- What you'll change in each file
- Where you'll write tests (per `test_contract`)
- Patterns you'll mirror from existing code
- Anything you're uncertain about

Then in **pair mode** (the default), use `AskUserQuestion` with options:
- `proceed` — write the code as proposed
- `steer` — user gives free-form direction; revise approach
- `show me <X>` — user requests more context (treat as steer with `<X>` as the ask)
- `skip` — mark task as `failed` with reason "skipped by user", advance to next

In **solo mode** (`--solo`): skip the pause, proceed directly. Note in the resulting commit body that the task was executed without explicit user approval ("solo: yes").

### 1d. Implement

Write code via `Edit`/`Write`. Constraints:
- Touch only files in `task.files`. If you need to touch others, **pause and ask** before doing so — this is a plan deviation worth surfacing.
- Honor every `locked = true` decision in `spec.toml`. If a decision conflicts with what you'd write, stop and ask the user to either revise the task or unlock the decision in spec.
- Respect rules.toml — especially "always run X via docker compose exec" patterns. If the rule says route through docker and you'd type `pnpm install` directly, you've violated the rule.

Write tests too — per the `test_contract` field. A task isn't complete until the contract has at least one test that would fail if the contract broke.

### 1e. Diff + verify

Show `git diff` (filtered to `task.files` if helpful). Run `dross validate` to ensure no schema drift in dross artefacts.

Run the test command:
```
<runtime.test_command>
```

Three outcomes:

**Green** → continue to commit step.

**Red, pair mode** → surface the failure tail (last 30-40 lines). Ask via `AskUserQuestion`:
- `fix here` — address the failure inline, re-run
- `mark failed` — set status to `failed`, advance (later tasks that don't depend on this one keep going)
- `abort phase` — stop the loop entirely; current state is preserved

**Red, solo mode** → try one bounded fix (max one Edit pass). If still red, mark `failed` and continue.

**No test command configured** → warn once at the start of the phase: "no `runtime.test_command` set, skipping per-task test gate. /dross-verify will catch unverified work later." Don't repeat the warning per task.

### 1f. Commit + record

Atomic commit, one task per commit. Use specific files, never `git add -A`:
```
git add <task.files>
```

Commit message format depends on `repo.commit_convention`:

**conventional**:
```
<type>(<phase-slug>): <task.title>

Task: <task-id>
Covers: <criterion-ids>
EOF
```
where `<type>` is `feat` for new behaviour, `fix` for bug fixes, `refactor` for structural changes, `chore` for tooling.

**freeform** (default if unset):
```
<task.title>

Task: <task-id>
Covers: <criterion-ids>
```

Do **not** add Co-Authored-By trailers unless the user has asked for them. Do not skip hooks (`--no-verify`) unless explicitly asked. If a pre-commit hook fails, treat it the same as a test failure (step 1e red branch) — fix inline, then commit fresh, never amend.

After commit:
```
SHA=$(git rev-parse --short HEAD)
dross changes record <phase> <task-id> --files <task.files (csv)> --commit $SHA
dross task status <phase> <task-id> done
dross state touch "executed <task-id> (<task.title>)"
```

Loop back to 1a.

## 2. Phase completion

When `dross task next` returns empty, print a wrap-up block:
```
Phase <id> execution complete.
  Done:    <N>/<total>
  Failed:  <M>     (use `dross task show <phase> <id>` to inspect)
  Skipped: <K>

Next: /dross-verify <phase> to check criterion coverage and test efficacy.
```

Update state:
```
dross state set current_phase_status complete
dross state touch "phase <id> executed (<done>/<total> done)"
```

If any tasks are `failed` or `pending`-but-blocked: do not mark the phase complete. Print:
```
Phase NOT marked complete — <N> tasks still need attention. Re-run /dross-execute when ready, or revise the plan via /dross-plan.
```
and update state status to `partial` instead.

## Hard rules

- **Pair mode is the default.** Never write code without an explicit user `proceed` in pair mode. The whole point is the user is part of the loop.
- **Atomic commits.** Exactly one commit per completed task. No batched multi-task commits.
- **Touch only `task.files`.** Plan deviation requires explicit user OK.
- **Honor locked decisions and rules.** If a constraint conflicts with the task, surface and ask — never silently work around it.
- **No `git add -A`.** Always specify files explicitly.
- **No `--no-verify`** unless the user asks. If a hook fails, fix the underlying issue.
- **No commit amending.** Failed pre-commit → new commit after fix, not amend.
- **Don't auto-retry test failures more than once in solo mode.** Bounded automation; failures need eyes.
