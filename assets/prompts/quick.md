# /dross-quick

Run a one-shot task with dross's atomic-commit + test-gate guarantees, **without** the spec → plan ceremony. **Pair-mode by default** — the user is the checker. Pass `--solo` to run autonomously for a trivial, well-specified change (mechanical refactor, config tweak, dependency bump): it skips only the approval gate, never the test gate or the safety rules.

Use when:
- A small change needs to land but doesn't justify a whole phase (typo fix, small refactor, dependency bump, README touch-up).
- You're mid-phase and a tangentially-related fix surfaces that would otherwise pollute the current task's diff.

`$ARGUMENTS` is the **freeform task description** — what you want done, in plain prose. The model interprets the intent and proposes the change.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): in pair mode, surface the approach and any red-test choice as their own propose-and-react turns.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Read `.dross/project.toml` — `runtime.*` (test/typecheck/lint), `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
3. Read `.dross/state.json`. Note `current_phase` (may be empty — that's fine, standalone mode).
4. **Verify the current branch matches the mode** with `git symbolic-ref --short HEAD`:
   - **In-phase** (`current_phase` set): branch must be `phase/<current_phase>`. If not, switch to it (or stop if it doesn't exist locally). Quick changes inside a phase belong on the phase branch — they ship together with the phase.
   - **Standalone** (no `current_phase`): branch must be the configured main branch (`repo.git_main_branch`). Standalone quick changes go to main directly via small commit.
5. Check `git status --porcelain`. If working tree is dirty:
   - Surface the diff to the user.
   - Ask via `AskUserQuestion`: "commit existing work first / stash / abort". Atomic commit semantics require a clean baseline.
6. Parse `$ARGUMENTS`:
   - Strip a leading/trailing `--solo` flag → **solo mode** (autonomous, no approval gate). Default without it is **pair mode**.
   - The remainder is the freeform task description. If it's empty, ask for one and stop until the user provides it — quick can't operate without intent. (In solo mode an empty description is a hard stop, not a prompt — there's no one to answer.)

Print one orientation block:
```
Quick task: <one-line summary of $ARGUMENTS>
Mode: pair | solo
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

**Pair mode** (default) — then via `AskUserQuestion`:
- `proceed` — write the code as proposed
- `steer` — user gives free-form direction; revise the proposal
- `show me <X>` — user requests more context; treat as steer
- `abort` — stop without writing anything

Never write code without an explicit `proceed` in pair mode — that's the contract. These gates mirror `/dross-execute`'s shape — the §1c approval (proceed/steer/show/abort) and the §4 red-test gate below the §1e fix/abort — adapted to a single task with no task-loop `skip`/`mark failed`.

**Solo mode** (`--solo`) — skip the approval gate: still print the proposal block (it's the audit trail of intent), then proceed straight to implement. Solo is for changes trivial enough that a human gate adds nothing. It is **not** licence to guess: if §1 turns up real ambiguity, a locked-decision conflict, or scope beyond what was described, stop and surface it rather than pressing on.

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

**Red, pair mode** → surface the failure tail (last 30-40 lines). Ask via `AskUserQuestion`:
- `fix here` — address inline, re-run
- `abort` — discard the working changes (`git checkout -- <files>` for tracked, `rm` for newly-written files), state stays untouched

**Red, solo mode** → try **one** bounded fix (a single Edit pass), then re-run. If still red, abort: discard the working changes (`git checkout -- <files>`, `rm` newly-written files), leave state untouched, and report the failure. No version bump on a failed solo quick. Never loop on red.

**No test command configured** → warn once. In pair mode, ask via `AskUserQuestion` (`proceed without tests` / `abort`). In solo mode, proceed without a gate and note `no test gate` in the commit body.

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

In **solo mode**, add a `solo: yes` line to the body (below `Quick:`) so autonomous runs are auditable in history.

**Match the repository's existing trailer convention.** Check recent history (`git log -1 --format=%B`): if commits carry a `Co-Authored-By` trailer, include one; if they don't, omit it. Don't introduce the trailer into a repo that doesn't already use it, and don't strip it from one that does. Do not skip hooks (`--no-verify`). If a pre-commit hook fails, treat it as a red test (step 4) — fix inline, commit fresh, never amend.

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

Mirror the quick task onto the issue board, keyed by the new version (no-op unless `[remote].board_sync` is on — safe to always run):
```
dross issue quick $NEW_VERSION "quick: <one-line summary>"
```

**Commit the dross bookkeeping.** The bump, the changes record (if any), and the state touch all wrote under `.dross/` *after* the work commit — the internal counter only bumps once the work commit succeeds (see Hard rules), so they can't fold into it. Commit them now so the quick never ends with a dirty `.dross/`, which the commit-hygiene rule forbids and which would trip the next `phase create`/`complete`:
```
git add .dross/
git commit -m "chore(dross): record quick <NEW_VERSION>"
```
Match the repo's trailer convention, as in §5.

## 7. Wrap-up

Print:
```
Quick task complete.
  Commit:   <SHA> "<commit subject>"
  Version:  <prev> → <new>
  Phase:    <phase-id> (recorded as quick-N in changes.json) | standalone
  Files:    <touched-files>

Next: continue working, or /dross-quick "<another task>" — another small change.
      ↳ --solo — run it autonomously when the change is trivial and well-specified.
```

The quick task is committed and done, so close its board issue (no-op unless board sync is on):
```
dross issue quick $NEW_VERSION --close
```

## Hard rules

- **Pair mode by default; `--solo` is opt-in.** Pair keeps a human in the loop (the checker). `--solo` runs autonomously for trivial, well-specified changes — it skips *only* the approval gate, never the test gate, the atomic-commit rule, or the locked-decision / project-rule checks. If solo hits real ambiguity or a constraint conflict, stop and surface it instead of guessing.
- **One commit per quick** for the actual work, plus a single follow-up `chore(dross):` commit for the version bump + state/changes bookkeeping (§6) — the counter only bumps after the work commit succeeds, so the two can't merge. If the *work* itself naturally splits into two commits, it's not a quick — route to a phase or run /dross-quick twice.
- **Touch only what you proposed.** Mid-implementation scope expansion requires re-confirmation.
- **No `git add -A`.** Always specify files explicitly.
- **No `--no-verify`** unless the user asks. Pre-commit hook failures get fixed and re-committed, not amended away.
- **No retries on red tests.** One fix attempt, then: pair mode asks (fix again / abort); solo mode aborts and discards (no bump). Never loop on red.
- **Internal counter bumps on success only.** If the user aborts mid-flow, the version stays where it was — quick tasks earn their version bump.
