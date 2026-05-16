# /dross-quick

Run a one-shot task with dross's atomic-commit + test-gate guarantees, **without** the spec → plan ceremony. Pair-mode only (no `--solo`); the user IS the checker.

Use when:
- A small change needs to land but doesn't justify a whole phase (typo fix, small refactor, dependency bump, README touch-up).
- You're mid-phase and a tangentially-related fix surfaces that would otherwise pollute the current task's diff.

`$ARGUMENTS` is the **freeform task description** — what you want done, in plain prose. The model interprets the intent and proposes the change.

## 0. Pre-flight

1. Run `dross rule show` and treat the output as MUST-FOLLOW.
2. Read `.dross/project.toml` — `runtime.*` (test/typecheck/lint), `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
3. Read `.dross/state.json`. Note `current_phase` (may be empty — that's fine, standalone mode).
4. Check `git status --porcelain`. If working tree is dirty:
   - Surface the diff to the user.
   - Ask via `AskUserQuestion`: "commit existing work first / stash / abort". Atomic commit semantics require a clean baseline.
5. Parse `$ARGUMENTS`. If empty, ask for a task description and stop until the user provides one — quick can't operate without intent.

Print one orientation block:
```
Quick task: <one-line summary of $ARGUMENTS>
Mode: pair (always)
Phase context: <phase-id> | standalone
Test command: <runtime.test_command or "(none — quick will skip the test gate)">
Version: <state.version> (will bump to <preview>.+1 on success)
```

## 1. Insight

Surface the 3-5 most useful observations before proposing. Don't dump everything.

- Identify the **likely files** the task touches. Use the task description as a guide:
  - Grep for distinctive nouns/identifiers if the task names them.
  - Otherwise infer from `paths.*` + the area the task describes.
- For each candidate file: `Read` it (relevant section only) and note pattern/structure.
- `git log -n 5 --oneline -- <file>` for recent activity.
- Look for sibling files in the same dir for pattern-mirroring (test layout, validation shape).

If the task is ambiguous about which file(s) it concerns, ask **one** targeted clarifying question via `AskUserQuestion` before proposing — better than guessing and steering later.

## 2. Propose

In one block, 3-6 lines covering:
- What you'll change in each file.
- Where you'll write/update tests (if `runtime.test_command` is set and the change is behavioural).
- Patterns you'll mirror from existing code.
- Anything you're uncertain about.

Then via `AskUserQuestion`:
- `proceed` — write the code as proposed
- `steer` — user gives free-form direction; revise the proposal
- `show me <X>` — user requests more context; treat as steer
- `abort` — stop without writing anything

Never write code without an explicit `proceed`. Pair mode is non-negotiable for quick — that's the contract.

## 3. Implement

Use `Edit`/`Write`. Constraints:
- Touch only the files agreed in the proposal. If implementation reveals you need to touch others, **pause and re-confirm** before doing so.
- Honor every `locked = true` decision in any active `spec.toml` (when in a phase) and every project-level rule. Surface conflicts; do not silently work around them.
- Respect `runtime.*` routing (e.g. "always run X via docker compose exec"). Match the user's environment, not the most convenient shell.

If `runtime.test_command` is set and the change is behavioural, **write/update tests in the same diff**. A quick task isn't done until the new behaviour has at least one test exercising it.

## 4. Diff + test gate

Show `git diff` (filtered to the touched files). Run `dross validate` if any `.dross/` files changed.

If `runtime.test_command` is set, run it:
```
<runtime.test_command>
```

Three outcomes:

**Green** → continue to commit.

**Red** → surface the failure tail (last 30-40 lines). Ask via `AskUserQuestion`:
- `fix here` — address inline, re-run
- `abort` — discard the working changes (`git checkout -- <files>` for tracked, `rm` for newly-written files), state stays untouched

**No test command configured** → warn once, ask via `AskUserQuestion` to confirm proceeding without a gate:
- `proceed without tests` — commit anyway, note in the body
- `abort` — discard

## 5. Commit

Atomic. Specific files only — never `git add -A`:
```
git add <touched-files>
```

Commit message depends on `repo.commit_convention`.

**conventional**:
```
<type>(quick): <one-line summary of the task>

Quick: <internal-version-after-bump>
[Phase: <phase-id>]   ← only if running inside a phase
```
`<type>` = `feat` for new behaviour, `fix` for bug fixes, `refactor` for structural changes, `chore` for tooling/docs.

**freeform** (default if unset):
```
quick: <one-line summary of the task>

Quick: <internal-version-after-bump>
[Phase: <phase-id>]
```

Do **not** add Co-Authored-By trailers unless the user asked for them. Do not skip hooks (`--no-verify`). If a pre-commit hook fails, treat it as a red test (step 4) — fix inline, commit fresh, never amend.

## 6. Record + bump version

```
SHA=$(git rev-parse --short HEAD)
dross state bump internal
NEW_VERSION=$(dross state show | jq -r .version)   # for the wrap-up line; use sed -n 's/.*"version": "\(.*\)".*/\1/p' if jq isn't available
```

If running **inside a phase** (state.json's `current_phase` is set), record to the phase's changes.json with a synthesised task-id of `quick-<internal-segment>`:
```
QUICK_ID="quick-$(dross state show | jq -r .version | awk -F. '{print $4}')"
dross changes record <phase> $QUICK_ID --files <touched-files (csv)> --commit $SHA --notes "quick: $ARGUMENTS"
```

(If `jq`/`awk` aren't on the user's `runtime.shell_helpers`, derive the segment from the bump output — it prints `<prev> → <new>`.)

If running **standalone** (no `current_phase`), skip the changes record. The git log + state history are the trail.

Always touch state:
```
dross state touch "quick: <one-line summary of the task>"
```

## 7. Wrap-up

Print:
```
Quick task complete.
  Commit:   <SHA> "<commit subject>"
  Version:  <prev> → <new>
  Phase:    <phase-id> (recorded as quick-N in changes.json) | standalone
  Files:    <touched-files>

Next: continue working, or /dross-quick <another task> for another small change.
```

## Hard rules

- **Pair mode only.** No `--solo` flag. Quick tasks must have a human in the loop — that's how they stay "quick" without becoming sloppy.
- **One commit per quick.** If the task naturally splits into two commits, it's not a quick — route to a phase or run /dross-quick twice.
- **Touch only what you proposed.** Mid-implementation scope expansion requires re-confirmation.
- **No `git add -A`.** Always specify files explicitly.
- **No `--no-verify`** unless the user asks. Pre-commit hook failures get fixed and re-committed, not amended away.
- **No retries on red tests.** One fix attempt, then abort or accept the failure with explicit user OK.
- **Internal counter bumps on success only.** If the user aborts mid-flow, the version stays where it was — quick tasks earn their version bump.
