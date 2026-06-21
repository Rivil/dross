# /dross-execute

Run a phase plan to completion. **Pair-mode by default**: propose, pause, steer, then write. `--solo` opts into autonomous execution for trivial phases.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): in pair mode, surface one decision per turn — the §1c approach approval and the §1e red-path choice are each their own `AskUserQuestion`, leading with the default and letting the user react. Never batch the next task's approval behind the current one.

## 0. Pre-flight (run once)

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Resolve target phase from `$ARGUMENTS` or `state.json`'s `current_phase`. Fail if neither is set.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`. If `plan.toml` is missing, route the user to `/dross-plan` and stop.
4. Read `.dross/project.toml` — specifically `runtime.*` (test/typecheck/lint commands), `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
5. **Verify current branch is `phase/<id>`** with `git symbolic-ref --short HEAD`. Phase work must never land on the main branch — that's the divergence pattern the phase-branch model is designed to prevent. If on a wrong branch:
   - If `phase/<id>` exists locally: `git checkout phase/<id>` and proceed.
   - Otherwise: stop. The phase wasn't created with `dross phase create` (which auto-checks out). Have the user run `dross phase create` or migrate existing work to a branch before executing.
6. Check git state with `git status --porcelain`. If working tree is dirty:
   - Surface the diff to the user
   - Ask: "Commit existing work first / stash / abort"
   - Do not proceed with execute on a dirty tree — atomic commits per task require a clean baseline.
7. Parse flags from `$ARGUMENTS`:
   - `--solo` → autonomous mode
   - `--from <task-id>` → start at this task (skip earlier-wave done tasks; resume mid-phase)
8. Detect resume state: if any task has `status = "in_progress"`, ask user "continue task X / reset to pending and pick fresh".

Print one orientation block:
```
Executing phase <id> (<title>)
Plan: <task-count> tasks across <wave-count> waves — <pending> pending, <done> done, <failed> failed
Mode: <pair | solo>
Test command: <runtime.test_command or "(none — verify will catch this later)">
```

Mark the board issue in-progress (no-op unless `[remote].board_sync` is on — safe to always run):
```
dross issue phase-sync <id> --status in-progress
```

Load the stack loadout once and keep it in working context for the whole phase:
```
dross stack loadout
```
This prints a markdown block — the recommended MCP tools, guardrails, and locked
conventions for the detected stack, plus which tools are installed. **Inject that
block inline** and treat it as standing guidance while you implement every task
(e.g. honour the guardrails before guessing an API, prefer the listed MCP tools).
If it prints `no stack profile matches here`, there's no profile for this stack
yet — skip it and proceed.

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
- Before invoking a library API you're guessing at (Playwright matchers, Vitest/Jest assertions, Drizzle query helpers, framework hooks, etc.), if `mcp__plugin_context7_context7__query-docs` is available, query it first. Training data is often stale on APIs that have changed in the last 12 months and the failure mode is shipping a wrong signature past the test gate.

Write tests too — per the `test_contract` field. A task isn't complete until the contract has at least one test that would fail if the contract broke.

### 1e. Diff + verify

Show `git diff` (filtered to `task.files` if helpful). Run `dross validate` to ensure no schema drift in dross artefacts.

If the touched files include `.svelte` and `mcp__svelte__svelte-autofixer` is available, run it on each touched component before the test gate and re-apply fixes until clean. The autofixer catches Svelte 4 → Svelte 5 syntax drift (runes, `onclick` vs `on:click`, snippets vs slots, deprecated APIs) that training data otherwise keeps reaching for. Same pattern for any future language-specific MCP autofixer.

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

**Match the repository's existing trailer convention.** Check recent history (`git log`): include a `Co-Authored-By` trailer only if the repo already uses one, and don't strip it from a repo that does. Don't introduce it into a repo that doesn't. Do not skip hooks (`--no-verify`) unless explicitly asked. If a pre-commit hook fails, treat it the same as a test failure (step 1e red branch) — fix inline, then commit fresh, never amend.

**Capture a landmark.** Before recording, write a one-line *landmark* for the
task — the durable "what shipped here" that `/dross-ship` later merges into
`ARCHITECTURE.md` by feature. It has three parts, aligned to that doc's entry
template (`internal/architecture` `EntryTemplate`):

```
feature: <user-facing capability> · <Symbol> @ <file> · <one line: what it does>
```

- **feature** — the capability this task delivered, phrased as something the
  system *does* (e.g. `phase lifecycle`, `architecture doc format`), never a
  module name and never the phase id. Reuse an existing feature name if the task
  extended one, so ship updates that entry in place instead of forking a new one.
- **symbol@file** — the primary symbol introduced/changed and its file (the
  symbol-link target). One is enough; ship resolves precise `file:line` later.
- **what** — a single dense clause. No prose paragraphs.

Pass it through the existing free-form `--notes` (there is no typed `--landmark`
field — that's deferred). If a task genuinely has no user-facing landmark (pure
tooling/chore), still record it with a `feature: <area>` landmark so the trail
stays complete.

After commit:
```
SHA=$(git rev-parse --short HEAD)
dross changes record <phase> <task-id> --files <task.files (csv)> --commit $SHA \
  --notes "feature: <capability> · <Symbol> @ <file> · <one line what>"
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

Next: /dross-verify <phase> — check criterion coverage and test efficacy.
```
When this phase changed no measurable Go (or Stryker isn't installed), append the conditional hint under the `Next:` line:
```
      ↳ --skip-mutation — skip the mutation pass when there's nothing for it to measure.
```

Update state:
```
dross state set current_phase_status complete
dross state touch "phase <id> executed (<done>/<total> done)"
```

**Commit the dross bookkeeping.** Each task's `dross changes record` + `state touch` (§1f) wrote under `.dross/` after its code commit, so `.dross/` is now dirty. Commit it once here, matching the repo's trailer convention, so the phase hands a clean tree to `/dross-verify` and `/dross-ship` (the commit-hygiene rule — ship refuses a dirty tree):
```
git add .dross/
git commit -m "chore(dross): execute <id> bookkeeping"
```

Re-sync the board issue so its checklist reflects the completed tasks (no-op unless board sync is on):
```
dross issue phase-sync <id>
```

If any tasks are `failed` or `pending`-but-blocked: do not mark the phase complete. Print:
```
Phase NOT marked complete — <N> tasks still need attention. Re-run /dross-execute when ready, or revise the plan via /dross-plan.
```
and update state status to `partial` instead.

## Hard rules

- **Follow the interaction playbook (`_interaction.md`).** Drive each pair-mode turn as a single-decision `AskUserQuestion` that leads with the default — the §1c approval and §1e red-path are separate turns, never bundled, and the next task's approval never rides along behind the current one.
- **Pair mode is the default.** Never write code without an explicit user `proceed` in pair mode. The whole point is the user is part of the loop.
- **Phase work commits to `phase/<id>`, never the main branch.** If `git symbolic-ref --short HEAD` returns the main branch, stop and fix before continuing.
- **Atomic commits.** Exactly one commit per completed task. No batched multi-task commits.
- **Touch only `task.files`.** Plan deviation requires explicit user OK.
- **Honor locked decisions and rules.** If a constraint conflicts with the task, surface and ask — never silently work around it.
- **No `git add -A`.** Always specify files explicitly.
- **No `--no-verify`** unless the user asks. If a hook fails, fix the underlying issue.
- **No commit amending.** Failed pre-commit → new commit after fix, not amend.
- **Don't auto-retry test failures more than once in solo mode.** Bounded automation; failures need eyes.
