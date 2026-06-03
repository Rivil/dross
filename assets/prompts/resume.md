# /dross-resume

Pick up where you left off. Read the handoff `/dross-pause` left, replay the thread so you're back in the headspace fast, re-orient on the mechanical position (phase, branch, diff), then prune the items you've dealt with. The handoff is a **living document** — resume edits it in place, it doesn't archive it.

Use at the start of a session when a handoff exists (`dross status` nudges you when one does).

## 0. Read the handoff

1. Run `dross rule show` and treat the output as MUST-FOLLOW.
2. Read `.dross/handoff.md`.
   - **Missing** → there's nothing to resume. Say so in one line and fall back to `/dross-status` for a cold-start orientation. Stop.
   - **Present** → continue.
3. Read the mechanical position so you can cross-check the handoff against reality (it may be stale):
   ```
   dross status
   git symbolic-ref --short HEAD        # current branch
   git status --porcelain               # what's dirty now
   git diff --stat                      # what's changed since
   ```

## 1. Replay the thread

Narrate, don't just dump the file. In a short block:
- **Where you were** — the `## Thread`, in your own words (2-3 sentences). The goal is to re-load the headspace, not re-read the file verbatim.
- **The one next action** — surface the `## Next` item front and centre. This is the single most useful line; lead with it.
- **Open loops** — list them so nothing's forgotten.

Then reconcile handoff against reality and flag drift:
- **Branch mismatch** — handoff says `phase/<id>` but you're on a different branch. Offer to `git checkout phase/<id>` (only if it exists locally; otherwise surface it, don't guess).
- **Dirty drift** — `## Dirty` lists files but the tree is clean now (committed since?), or new dirty files appeared. Note it.
- **Stale next** — the `## Next` action looks already-done given the diff. Call it out rather than re-doing it.

If the handoff is older than a few days, say so — memory may have moved on.

## 2. Prune

The handoff is a checklist. Walk the items and close out what's done. Via `AskUserQuestion` (or just confirm in prose if it's obvious):
- For each `## Next` / `## Open loops` item: **done** (remove it) / **keep** (still open) / **edit** (reword).

Then rewrite `.dross/handoff.md` with only what's left:
- Items still open stay.
- Refresh the `phase:` / `branch:` header line and the `## Dirty` list from current reality.
- If **everything** is resolved → delete the file (`rm .dross/handoff.md`). The `dross status` nudge disappears; you're fully resumed onto a clean slate.

Never silently drop an item the user didn't mark done. Pruning is the user's call, item by item.

## 3. Hand back to the workflow

Resume gets you oriented; it doesn't do the work. Point at the right next command based on the `## Next` action and `dross status`'s `next:` line:
- mid-task → `/dross-execute --from <task-id>`
- needs spec/plan → `/dross-spec` / `/dross-plan`
- ready to verify → `/dross-verify`

Stamp state:
```
dross state touch "resumed — <one-line of what's next>"
```

## 4. Wrap-up

Print:
```
Resumed.
  Thread:  <one-line recap of where you were>
  Next:    <the next action>
  Handoff: <N item(s) still open | cleared — handoff.md removed>

Pick up with <suggested command>.
```

## Hard rules

- **Read-mostly.** Resume edits exactly one file (`.dross/handoff.md`, to prune) and touches state. It does not commit, write code, or change branches *except* an explicit `git checkout` the user okayed to get back onto the phase branch.
- **Handoff can be wrong.** It's a point-in-time note. Always cross-check against `dross status` + git, and trust reality over the note when they conflict — flag the drift, don't blindly follow a stale `## Next`.
- **Prune, don't archive.** Done items are deleted from the file. There is no archive — the living doc shrinks to what's still open, then vanishes when empty.
- **Don't re-do done work.** If the diff shows the `## Next` action already landed, say so and move to the next open loop instead of repeating it.
- **No editorialising.** Re-orient and hand off to the workflow; don't turn resume into a code review or a roadmap.
