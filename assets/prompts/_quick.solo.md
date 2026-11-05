Solo quick: these replace the pair-mode gates for the whole run. Solo skips *only* the approval gate — never the test gate, the atomic-commit rule, or the locked-decision / project-rule checks. If it hits real ambiguity or a constraint conflict, stop and surface it instead of guessing.

**§2, solo** → skip the approval gate: still print the proposal block (it's the audit trail of intent), then proceed straight to implement. Solo is for changes trivial enough that a human gate adds nothing. It is **not** licence to guess: if §1 turns up real ambiguity, a locked-decision conflict, or scope beyond what was described, stop and surface it rather than pressing on.

**Red, solo mode** (§4) → try **one** bounded fix (a single Edit pass), then re-run. If still red, abort: discard the working changes (`git checkout -- <files>`, `rm` newly-written files), then `dross quick end`, leave state untouched, and report the failure. No version bump on a failed solo quick. Never loop on red.

**No test command, solo** (§4) → proceed without a gate and note `no test gate` in the commit body.

**Solo review (`--solo` only).** In solo mode no human approves the change, so a cold reviewer checks it against the quick's stated description — after the green `dross test`, before the commit. In **pair mode skip this entirely**: the user is the checker.

1. Build the review context:
   ```
   dross review context
   ```
   It prints the reviewer's `subagent_type` and a `prompt:` line, never the diff.
2. Spawn the reviewer with the Agent tool: `subagent_type: "dross-task-reviewer"`, and the printed prompt line **verbatim** as the whole prompt — add nothing to it; the reviewer sees only the context, and a widened prompt makes the review unavailable. It may run in the background (an interactive session runs every subagent there): **wait for its completion notice** before reading the status — its verdict reaches dross only when it finishes.
3. Read where it stands:
   ```
   dross review status
   ```
   - `pass` → commit (§5).
   - `blocked` → **one fix round**, never more: address every blocking finding it lists, re-run `dross test`, then `dross review context` and the reviewer again, and re-read `dross review status`. A second block exhausts the review.
   - `pass-stale` → the tree moved after the review: re-run `dross test`, `dross review context` and the reviewer.
   - `exhausted` or `unavailable`, **or the spawn itself errored** → abort: discard the working changes (`git checkout -- <files>`, `rm` newly-written files) **first**, then `dross quick end`. Report the reviewer's findings (`dross review status` lists them). No version bump.

**§5, solo** → add a `solo: yes` line to the commit body (below `Quick:`) so autonomous runs are auditable in history. The commit gate refuses a solo code commit unless a passing review was recorded for exactly the tree being committed.
