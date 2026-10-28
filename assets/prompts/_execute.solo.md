Solo run: these replace the pair-mode pauses for the whole run.

- **Don't auto-retry test failures more than once in solo mode.** Bounded automation; failures need eyes.

**§1c, solo** → skip the approval pause and proceed directly. Note "solo: yes" in the commit body: no user approved it.

**Writer dispatch (`--solo` only).** After §1a, before §1b, ask dross whether the task's code goes to a writer:
```
dross task dispatch <phase> <task-id>
```
- `dispatch: inline (…)` → §1b–§1e as written.
- `dispatch: writer` → skip §1b–§1e. Spawn with the Agent tool: the printed `subagent_type`, the printed `prompt:` line **verbatim** as the whole prompt, `run_in_background: true`, no `model`. **Wait for its completion notice**, then read only its `dross-writer-report` block: never Read the task's files, never `git diff` — the review reads the code.
  - `test: green` → §1f in the main loop, in order: bare `dross test`, then `dross review context` and the reviewer, then the commit. The writer's green certifies nothing.
  - **Writer failed** — the spawn errored, the report says `test: red`, there is no report, or the bare `dross test` after it is red → the task fails; never fix it inline. Set its code aside first:
    ```
    git stash push -u -- . ':(exclude).dross'
    dross task status <phase> <task-id> failed --reason "writer: <the error, the red test, or no report>"
    dross task next <phase>
    ```
  - A `blocked` review's one fix round goes to the same writer, never inline: load `SendMessage` via ToolSearch and send the writer's agent the blocking findings. Wait for its reply and read only its new report. SendMessage unavailable, a red report or no reply → **Writer failed**; green → re-run bare `dross test`, then `dross review context` and the reviewer, as the `blocked` branch below says.

**Red, solo mode** (§1e) → try one bounded fix (max one Edit pass). If still red, set the task's code aside **first**, then mark it failed and continue — `failed` refuses while a solo task's code is uncommitted:
```
git stash push -u -- . ':(exclude).dross'
dross task status <phase> <task-id> failed --reason "<the failing test, one line>"
dross task next <phase>
```

**Solo review (`--solo` only).** No human approves the task, so a cold reviewer does — in §1f, after the full green bare `dross test`, before the commit. In **pair mode skip this entirely**: the human is the gate. A task that changed only `.dross/` (no code) also skips it: a `.dross/`-only commit is ungated.

1. Build the review context:
   ```
   dross review context
   ```
   It prints the reviewer's `subagent_type` and a `prompt:` line. It never prints the diff — don't paste it either.
2. Spawn the reviewer with the Agent tool: `subagent_type: "dross-task-reviewer"`, and the printed prompt line **verbatim** as the whole prompt. Add nothing to it: a widened prompt makes the review unavailable. It may run in the background: **wait for its completion notice** before reading the status — its verdict reaches dross only when it finishes.
3. Read where it stands:
   ```
   dross review status
   ```
   - `pass` → commit.
   - `blocked` → **one fix round**, never more: address every blocking finding it lists, re-run `dross test` (bare), then `dross review context` and the reviewer again, and re-read `dross review status`. A second block exhausts the review.
   - `pass-stale` → the tree moved after the review: re-run `dross test`, `dross review context` and the reviewer.
   - `exhausted` or `unavailable`, **or the spawn itself errored** (the recorder never sees an errored call, so status still reads `none`) → the task fails. Set its code aside first, then mark it:
     ```
     git stash push -u -- . ':(exclude).dross'
     dross task status <phase> <task-id> failed
     dross task next <phase>
     ```
     For a spawn error add `--reason "<the error>"` to the `failed` line; otherwise the reason comes from the review record.

The commit gate checks this too: it refuses a solo code commit without a passing review of exactly that tree.

**§1g, solo** → skip the post-commit gate; loop back to 1a.
