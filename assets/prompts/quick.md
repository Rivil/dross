# /dross-quick

Run a one-shot task with dross's atomic-commit + test-gate guarantees, **without** the spec → plan ceremony. **Pair-mode by default** — the user is the checker. Pass `--solo` to run autonomously for a trivial, well-specified change (mechanical refactor, config tweak, dependency bump): it skips only the approval gate, never the test gate or the safety rules.

Use when:
- A small change needs to land but doesn't justify a whole phase.
- You're mid-phase and a tangentially-related fix surfaces that would otherwise pollute the current task's diff.

`$ARGUMENTS` is the **freeform task description** — what you want done, in plain prose. The model interprets the intent and proposes the change.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): in pair mode, surface the approach and any red-test choice as their own propose-and-react turns.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Read `.dross/project.toml` — `runtime.*` (test/typecheck/lint), `paths.*`, `repo.commit_convention`, `repo.git_main_branch`, `stack.locked`.
3. Read `.dross/state.json`. Note `current_phase` (may be empty — that's fine, standalone mode).
4. **Verify the current branch matches the mode** with `git symbolic-ref --short HEAD`:
   - **In-phase** (`current_phase` set, `current_phase_status` NOT `shipped`): branch must be `phase/<current_phase>`. If not, switch to it with `dross phase checkout <current_phase>` (or stop if it doesn't exist locally). Quick changes inside a phase belong on the phase branch — they ship together with the phase.
   - **Shipped phase** (`current_phase` set, `current_phase_status` = `shipped`): the phase's PR is already open and may already be merged; `dross phase complete` is about to delete `phase/<current_phase>` locally and on origin, so work committed there would be lost or need re-shipping. Treat this as **standalone** and follow the rule below. `dross ship` leaves `current_phase` set until a confirmed merge, which is what makes this window reachable — surface it to the user in one line rather than silently routing.
   - **Standalone** (no `current_phase`, or a shipped one per the case above): run `dross base-branch` to get the base — the active milestone's integration branch (`milestone/<version>`) when one exists, else the configured main branch. Don't hardcode `repo.git_main_branch`; `dross base-branch` resolves the milestone-vs-main cutover for you (and nudges on stderr when no milestone is active). Then ask whether origin takes direct pushes to it: `dross protect --check <base>` prints exactly one answer, and that picks the route below. Only the direct route records its base with `dross local set quick_base <base>`; the PR route records none.
     - `unprotected` or `not checked (<provider>)` → **direct route**: the branch must be the base itself, and the quick lands on it as a small commit. Once the branch is confirmed, record it: `dross local set quick_base <base>` — `dross ship` and `dross phase complete` reconcile that recorded branch rather than re-deriving one, so an unpushed `.dross` chore left here can't re-seed divergence later. The store is `.dross/local.toml`, gitignored so the value never rides cumulative history.
     - `protected` or `unknown` → **PR route**: origin refuses direct pushes to the base, or can't say whether it does, so the quick reaches the base through a PR. From the base, up to date with origin, create `quick/<preview-version>` — the version the orientation block previews, which `dross quick end`'s bump makes `<NEW_VERSION>` — with `git branch quick/<preview-version>`, move onto it with `dross checkout quick/<preview-version>`, and work there. Record **no** quick_base: the PR carries the work and its `.dross` bookkeeping both, and a feature branch recorded as a base would have ship and complete reconcile it. §6 opens the PR.
5. Check `git status --porcelain`. If working tree is dirty:
   - Surface the diff to the user.
   - Ask via `AskUserQuestion`: "commit existing work first / stash / abort". Atomic commit semantics require a clean baseline.
6. Parse `$ARGUMENTS`:
   - Strip a leading/trailing `--solo` flag → **solo mode** (autonomous, no approval gate). Default without it is **pair mode**.
   - The remainder is the freeform task description. If it's empty, ask for one and stop until the user provides it — quick can't operate without intent. (In solo mode an empty description is a hard stop, not a prompt — there's no one to answer.)
7. Record the quick for the tool gates — its mode and its description, which is the solo reviewer's only spec source:
   ```
   dross quick begin "<description>"
   ```
   or, only in solo mode:
   ```
   dross quick begin --solo "<description>"
   ```
   A solo begin refuses unless the solo task reviewer is installed and current — **stop and ask the user to run `dross install`**. From here on, every way this run ends runs `dross quick end`: after the commit (§5), and on every abort path below, always **after** discarding the working changes. An abort has committed nothing, so its end bumps nothing.

**When** `mode=solo` — begin recorded solo: `dross prompt partial quick solo`

Print one orientation block:
```
Quick task: <one-line summary of $ARGUMENTS>
Mode: pair | solo
Phase context: <phase-id> | standalone
Test command: <runtime.test_command or "(none — quick will skip the test gate)">
Version: <state.version> (will bump to <preview>.+1 on success)
```

## 1. Insight

Surface the 3-5 most useful observations before proposing.

- Identify the **likely files** the task touches. Use the task description as a guide:
  - Grep for distinctive nouns/identifiers if the task names them.
  - Otherwise infer from `paths.*` + the area the task describes.
- For each candidate file: `Read` it (relevant section only) and note pattern/structure.
- `git log -n 5 --oneline -- <file>` for recent activity.
- Look for sibling files in the same dir for pattern-mirroring (test layout, validation shape).

If the task is ambiguous about which file(s) it concerns, ask **one** targeted clarifying question via `AskUserQuestion` before proposing.

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
- `abort` — stop without writing anything, then `dross quick end`

Never write code without an explicit `proceed` in pair mode — that's the contract.

## 3. Implement

Use `Edit`/`Write`. Constraints:
- Touch only the files agreed in the proposal. If implementation reveals you need to touch others, **pause and re-confirm** before doing so.
- Honor every `locked = true` decision in any active `spec.toml` (when in a phase) and every project-level rule. Surface conflicts; do not silently work around them.
- Respect `runtime.*` routing (e.g. "always run X via docker compose exec").

If `runtime.test_command` is set and the change is behavioural, **write/update tests in the same diff**. A quick task isn't done until the new behaviour has at least one test exercising it.

## 4. Diff + test gate

Show `git diff` (filtered to the touched files). Run `dross validate` if any `.dross/` files changed.

Check consent first — dross runs no repo command that spawns a process until this machine trusts it:
```
dross trust --check
```
Non-zero means untrusted or stale: **stop and show the user the exact `runtime.test_command` line**, then let them run `dross trust`. Never run `dross trust` on their behalf: the gate exists so a human reads the line.

If `runtime.test_command` is set, run the suite through dross, never raw. `dross test lane list` prints `vouch: true` on a vouching lane; with one, run the touched files, one `--files` per path — `scoped green: <n> covered` says the commit needs no full run:
```
dross test --files <touched-files[0]> --files <touched-files[1]> ...
```
Run the whole suite in a repo with no vouching lane — with lanes declared, its `--files` run records nothing and the commit would be refused — and in a vouching repo only after a refused `git commit` names a path only a full run clears (build config, a deletion, a path with no related test):
```
dross test
```
Either runs on the granted host if any (`--local`: this machine), logs to `.dross/logs/`, and prints counts on green; on red each failing test, then `+N more failing — see <log>` past ten. Exit **1** is a red suite; any other non-zero but **5** means the run did not happen, in whole or in part, and is never a reason to commit.

**Green** → continue to commit. `uncovered:` reports the asked list, not the commit, so it alone never triggers a bare run: commit and let the gate judge.

**Exit 5** → a test-less or docs-only quick: nothing was measured, which is no red. Paths that all match `runtime.test_exempt` commit with no run on a current grant; a commit refused for want of one waits on the user's own `dross trust --test-exempt`, never run on their behalf. Any other path takes a test and a re-run, or the bare run once its commit is refused.

**Red, pair mode** → surface the summary's failures; open the log only where a block was cut. Ask via `AskUserQuestion`:
- `fix here` — address inline, re-run
- `abort` — discard the working changes (`git checkout -- <files>` for tracked, `rm` for newly-written files), then `dross quick end`; state stays untouched

**No test command configured** → warn once. In pair mode, ask via `AskUserQuestion` (`proceed without tests` / `abort`).

A solo quick reviews here, after §4's run and before the commit.
**Re-fetch** if context was compacted: `dross prompt partial quick solo`

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

**Match the repository's existing trailer convention.** Check recent history (`git log -1 --format=%B`): if commits carry a `Co-Authored-By` trailer, include one; if they don't, omit it. Do not skip hooks (`--no-verify`). If a pre-commit hook fails, treat it as a red test (step 4) — fix inline, commit fresh, never amend.

Once the commit lands, end the quick:
```
dross quick end
```
Seeing the commit, it does the bookkeeping: bumps the internal version, records `quick-<n>` in the phase's changes.json (in-phase and not shipped only), touches state, opens the quick's board issue, and prints `version: <NEW_VERSION>`. **No `version:` line means a stale binary**: stop and ask the user to run `make install` (`dross update` outside a dross checkout) — the stale end cleared the record without bumping. `board: off` is the `[board].enabled` = false no-op; a non-zero exit is a board failure — surface `board: FAILED`, never read it as the no-op, and retry with the `dross issue quick <version> "<summary>"` line it prints (the rest is already written).

**Commit the bookkeeping** — it wrote under `.dross/` after the work commit, so it cannot fold into it, and a dirty `.dross/` breaks the commit-hygiene rule:
```
git add .dross/
git commit -m "chore(dross): record quick <NEW_VERSION>"
```
Match the repo's trailer convention, as above.

## 6. Wrap-up

The quick task is committed and done, so close its board issue **first, before anything switches branch** (same board rule: silent only when `[board].enabled` is false, a non-zero exit is a failure to surface):
```
dross issue quick $NEW_VERSION --close
```
The close finds the issue through the link `dross quick end` wrote into `.dross/board.json`, so it must run on the branch holding that link. On the PR route that is `quick/<NEW_VERSION>`: the base has no copy until the PR merges, and a close run after `dross checkout <base>` fails with `no board issue linked to quick ref`. The close writes nothing under `.dross/`, so the tree stays clean for the push.

**PR route only** (§0.4): the work commit and §5's bookkeeping commit both sit on `quick/<NEW_VERSION>`. Publish the branch and open its PR into the base, then go back to the base:
```
git push -u origin quick/<NEW_VERSION>
gh pr create --base <base> --head quick/<NEW_VERSION> --fill
dross checkout <base> && git pull --ff-only
```
No auto-merge and no admin merge: the user merges it once CI is green, like any PR. Surface the PR URL `gh` prints, and never push the base itself.

Print:
```
Quick task complete.
  Commit:   <SHA> "<commit subject>"
  Version:  <prev> → <new>
  Phase:    <phase-id> (recorded as quick-N in changes.json) | standalone
  Route:    direct to <base> | PR <url> into <base>   ← standalone only
  Files:    <touched-files>

Next: continue working, or /dross-quick "<another task>" — another small change.
      ↳ --solo — run it autonomously when the change is trivial and well-specified.

state is on disk — safe to /clear · fresh session: /dross-status
```

## Hard rules

- **Pair mode by default; `--solo` is opt-in.** §0's solo fetch carries its rules.
- **One commit per quick** for the actual work, plus a single follow-up `chore(dross):` commit for the version bump + state/changes bookkeeping `dross quick end` writes (§5). If the *work* itself naturally splits into two commits, it's not a quick — route to a phase or run /dross-quick twice.
- **Touch only what you proposed.** Mid-implementation scope expansion requires re-confirmation.
- **No `git add -A`.** Always specify files explicitly.
- **No `--no-verify`** unless the user asks.
- **No retries on red tests.** One fix attempt, then: pair mode asks (fix again / abort); solo mode aborts and discards (no bump). Never loop on red.
- **Internal counter bumps on success only.** If the user aborts mid-flow, the version stays where it was.
