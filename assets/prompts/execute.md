# /dross-execute

Run a phase plan to completion. **Pair-mode by default**: propose, pause, steer, then write. `--solo` opts into autonomous execution for trivial phases.

**Run this as a conversation, not a broadcast.** Follow the interaction playbook (`_interaction.md`, printed by `dross interaction show` below): in pair mode each decision — the §1c approval, the §1e red path, the §1g gate — is its own `AskUserQuestion`, leading with the default.

## 0. Pre-flight (run once)

1. Run `dross rule show` and `dross interaction show`; the rules are MUST-FOLLOW and the playbook governs every turn.
2. Resolve the phase from `$ARGUMENTS` or `state.json`'s `current_phase`; fail if neither is set.
3. Read `.dross/phases/<id>/spec.toml` and `plan.toml`. No `plan.toml` → route the user to `/dross-plan` and stop.
4. Read `.dross/project.toml`: `runtime.*`, `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
5. **Verify current branch is `phase/<id>`** with `git symbolic-ref --short HEAD`. Phase work must never land on the main branch.
6. Check the tree is clean with `git status --porcelain`.
7. Flags: `--solo` → autonomous mode; `--from <task-id>` → start at this task (resume mid-phase).
8. Detect resume state: does any task have `status = "in_progress"`?

**When** `preflight=recovery` — wrong branch, dirty tree, or a task left `in_progress`: `dross prompt partial execute recovery`

9. Record the run's mode for the tool gates, once, before the per-task loop:
   ```
   dross execute begin <id>
   ```
   or, only when `--solo` was passed:
   ```
   dross execute begin <id> --solo
   ```
   In pair mode the pair-approval gate then refuses `Edit`/`Write` outside `.dross/` until the user approves the task (§1c), and refuses a switch to `--solo` while a task is in progress.

   A `--solo` begin refuses unless the solo task reviewer (§1f) is installed and current — it names the problem and the fix. **Stop and ask the user to run `dross install`** (or remove the repo-level definition it names); never start a solo run without the reviewer.

**When** `mode=solo` — begin recorded solo: `dross prompt partial execute solo`

Begin also does the mechanical start — the version patch from the phase ordinal (`dross phase number`), the board issue to in-progress, a report-only pull of board task moves — one labelled line each:

- `version:` — a bump, `unchanged`, or `skipped (<why>)`. **No `version:` line means a stale binary**: stop and ask the user to run `make install` (`dross update` outside a dross checkout), then re-run begin.
- `board:` — `off` when `[board].enabled` is false; `FAILED` (non-zero exit) is a board failure to surface.
- **No moves** — `pull:` says no task moves, `off` or `n/a`: carry on.

**When** `board_pull=reported` — `pull:` names a task move or a CONFLICT: `dross prompt partial execute board-moves`

Print an orientation block: phase id and title, task counts by wave and status, mode, and `runtime.test_command` (or "none — verify will catch this later").

Load the stack loadout and **inject the block inline** as standing guidance for every task (on `no stack profile matches here`, skip it):
```
dross stack loadout
```

## 1. Per-task loop

Repeat until `dross task next <phase>` returns nothing.

### 1a. Pick

```
TASK_ID=$(dross task next <phase>)
```
Empty → step 2. Otherwise mark it in progress; the second line moves its board card, so call it unconditionally:
```
dross task status <phase> $TASK_ID in_progress
dross issue task sync <phase> $TASK_ID --status task-in-progress
```
Show the user its full record: `dross task show <phase> $TASK_ID`.

### 1b. Code insight

For each file in `task.files`: `Read` it if it exists and surface the relevant symbols (else note "new file"), and run `git log -n 5 --oneline -- <file>`. Skim 1-2 relevant siblings in its directory for patterns to mirror. Surface the 3-5 most useful observations, not everything.

**Offload the reading when the task's file surface is large.** Fan out read-only subagents that do the §1b reading and return the key observations and sibling patterns. Per `dross-agent-gate` they never edit files or commit; only the main loop writes code. For a typical small task, stay inline (docs/subagent-offload-audit.md).

### 1c. Propose approach

In 3-7 lines: what changes in each file, where the tests go (per `test_contract`), patterns you'll mirror, anything uncertain. Then in **pair mode**, `AskUserQuestion` with:
- `approve <task-id>` — the literal label with this task's id (e.g. `approve t-3`). Only exactly this answer records the approval the pair-approval gate unlocks edits on, until this task's commit.
- `steer` — the user gives direction; revise the approach
- `show me <X>` — more context (a steer with `<X>` as the ask)
- `skip` — mark the task `failed` with reason "skipped by user", advance

### 1d. Implement

Write code via `Edit`/`Write`. In pair mode the gate refuses either outside `.dross/` until `approve <task-id>` is recorded: go back to §1c, never around it.
- Touch only `task.files`; any other file is a plan deviation — **pause and ask** first.
- Honor every `locked = true` decision in `spec.toml`; on conflict, stop and ask.
- Respect rules.toml (e.g. "always run X via docker compose exec").
- Guessing a library API? Query `mcp__plugin_context7_context7__query-docs` first when available — stale training data ships wrong signatures past the test gate.

Write tests per `test_contract`: at least one that fails if the contract breaks.

### 1e. Diff + verify

Show `git diff`; run `dross validate`. If `.svelte` files changed and `mcp__svelte__svelte-autofixer` is available, run it on each until clean (likewise any language autofixer MCP).

Check consent first — dross runs no repo command that spawns a process until this machine trusts it:
```
dross trust --check
```
Non-zero means untrusted or stale: **stop and show the user the exact `runtime.test_command` line**, then let them run `dross trust`. Never run `dross trust` on their behalf: the gate exists so a human reads the line.

Run the suite through dross with **this task's own `task.files`** (from `plan.toml`, never the git diff), one `--files` per path — a comma is legal in a path:
```
dross test --files <task.files[0]> --files <task.files[1]> ...
```
It runs only the `[[runtime.test_lane]]` lanes those paths hit; a repo with no lanes runs `runtime.test_command` unchanged. It runs on the granted remote host if any (`--local` forces this machine), logs full output under `.dross/logs/`, and prints a summary: counts on green; on red each failing test, then `+N more failing — see <log>` past ten. A bare `dross test` runs the whole suite.

Read the exit status. Only **0** means the code was measured and passed:

- **1** — a red suite. The one status that is a verdict about the code.

**When** `test_exit=not-run` — exit 2 to 8: `dross prompt partial execute test-not-run`

**Green** → continue to §1f.

**Red, pair mode** → surface the summary's failures; open the log only where a block was cut. `AskUserQuestion`:
- `fix here` — fix inline, re-run
- `mark failed` — set `failed`, advance (independent tasks keep going)
- `abort phase` — stop the loop; state is preserved

**No test command configured** → warn once per phase that the per-task gate is skipped and /dross-verify will catch it.

### 1f. Commit + record

The commit gate admits a code commit only after a **full** green `dross test` on exactly the tree being committed. With no lanes, §1e's run was that. With `[[runtime.test_lane]]` declared, §1e's `--files` run recorded nothing, so run the full suite bare first:
```
dross test
```
Only exit **0** lets you commit; re-run if anything outside `.dross/` changed since. A refused `git commit` is a hard stop, never something to route around.

A solo run reviews here, before the commit.
**Re-fetch** if context was compacted: `dross prompt partial execute solo`

One task per commit, specific files only:
```
git add <task.files>
```
Message per `repo.commit_convention` — **conventional**:
```
<type>(<phase-slug>): <task.title>

Task: <task-id>
Covers: <criterion-ids>
```
with `<type>` `feat`, `fix`, `refactor` or `chore`; **freeform** (the default) drops the `<type>(<phase-slug>): ` prefix. Match the repo's trailer convention (`git log`): a `Co-Authored-By` trailer only if it already uses one. No `--no-verify` unless asked; a failing pre-commit hook is a red test (§1e) — fix, then commit fresh, never amend.

**Capture a landmark** — the durable "what shipped here" that `/dross-ship` merges into `ARCHITECTURE.md` by feature (`internal/architecture` `EntryTemplate`):

- **feature** — the capability delivered, as something the system *does* (`phase lifecycle`), never a module or the phase id. Reuse an existing feature name when extending one.
- **symbol** + **loc** — the primary symbol introduced/changed and its `file:line`.
- **what** — one dense clause.

Pass it as **one** `--landmark` flag of comma-separated `key=value` pairs (each splits on its first `=`), never in `--notes`. A pure chore still records a `feature=<area>` landmark.

After commit:
```
SHA=$(git rev-parse --short HEAD)
dross changes record <phase> <task-id> --files <task.files (csv)> --commit $SHA \
  --landmark "feature=<capability>, symbol=<Symbol>, loc=<file:line>, what=<one line what>"
dross task status <phase> <task-id> done
dross state touch "executed <task-id> (<task.title>)"
dross issue task sync <phase> <task-id> --status task-in-review
```
The card moves to review: done, awaiting the phase verdict. Silent when `[board].enabled` is false; a non-zero exit is a board failure to surface.

### 1g. Post-commit gate

The commit is a durable boundary. In **pair mode**, one `AskUserQuestion`:
- `continue` — loop back to 1a.
- `checkpoint` — first confirm state is consistent — run `dross validate` and `dross task next <phase>` (it prints `<next-task>`) — **then** print the re-entry line and stop (nothing printed → step 2):
  ```
  checkpoint — state is on disk — safe to /clear · fresh session: /dross-execute --from <next-task>
  ```
- `stop` — halt here; state is preserved.

**Option order follows the wave boundary (checkpoint_posture).** When the task closes its wave — `dross task next` names a *higher* wave, or nothing — lead with `checkpoint`. Mid-wave, lead with `continue`.

## 2. Phase completion

When `dross task next` returns empty, print:
```
Phase <id> execution complete.
  Done:    <N>/<total>
  Failed:  <M>     (use `dross task show <phase> <id>` to inspect)
  Skipped: <K>

Next: /dross-verify <phase> — check criterion coverage and test efficacy.

state is on disk — safe to /clear · fresh session: /dross-verify <phase>
```
If the phase changed no measurable Go (or Stryker isn't installed), add under `Next:`: `↳ --skip-mutation — skip the mutation pass when there's nothing for it to measure.`

Then:
```
dross state set current_phase_status complete
dross state touch "phase <id> executed (<done>/<total> done)"
```
Commit §1f's `.dross/` bookkeeping once, so verify and ship get a clean tree:
```
git add .dross/
git commit -m "chore(dross): execute <id> bookkeeping"
```
Re-sync the board issue's checklist (a non-zero exit is a failure to surface):
```
dross issue phase sync <id>
```
If any task is `failed` or blocked `pending`, set status `partial`, not `complete`, and print: `Phase NOT marked complete — <N> tasks still need attention. Re-run /dross-execute when ready, or revise the plan via /dross-plan.`

## Hard rules

- **Follow the interaction playbook (`_interaction.md`).** One decision per `AskUserQuestion`: §1c, the §1e red path and §1g are separate turns, and the next task's approval never rides behind the current one.
- **Pair mode is the default.** Never write code without the user picking `approve <task-id>` for that task in pair mode.
- **A gate refusal is a hard stop.** Follow its remedy; never route around it. If the gate is wrong, tell the user: only they can lift it, with `dross gate off <name>` in their own terminal.
- **Phase work commits to `phase/<id>`, never the main branch.**
- **Atomic commits.** One commit per completed task.
- **Touch only `task.files`.** Deviation needs explicit user OK.
- **Honor locked decisions and rules.** On conflict, surface and ask.
- **No `git add -A`.** Name the files.
- **No `--no-verify`** unless the user asks.
- **No commit amending.** A failed hook → fix, then a new commit.
