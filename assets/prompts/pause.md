# /dross-pause

Capture a handoff before you stop — so next session your brain doesn't blank out. The point is to write down the **thread** (what you were doing and *why*), the **exact next action**, and the **open loops**, while it's all still fresh in this session. `/dross-resume` replays it.

Use when:
- You're stopping mid-phase (or even mid-task) and won't finish today.
- You're stopping at a clean boundary but the *why* / next-move lives only in this chat.

`$ARGUMENTS` is optional freeform — a one-line "what I was doing" hint. If empty, infer it from the session.

The handoff is a **living document** at `.dross/handoff.md`: one file, gitignored, pruned by resume. Not an archive — pausing again updates the same file.

## 0. Pre-flight

1. Run `dross rule show` and treat the output as MUST-FOLLOW.
2. Read `.dross/state.json`. Note `current_phase`, `current_phase_status`, `version`. (May be empty — that's fine, you can pause standalone work too.)
3. Capture the mechanical snapshot:
   ```
   git symbolic-ref --short HEAD        # current branch
   git status --porcelain               # dirty / untracked files
   dross status                         # phase + task progress + next-runnable
   ```
4. If `.dross/handoff.md` **already exists**, read it. This is a re-pause — you'll merge into it (keep still-open loops, add new ones), not clobber it.

## 1. Draft the handoff

**You draft it — don't interview the user line by line.** The whole value is that you reconstruct the thread now, while you still remember it. Pull from: this session's work, `$ARGUMENTS`, the git diff, `dross status`.

Compose the document in this shape (omit a section if genuinely empty):

```markdown
# Handoff — paused <YYYY-MM-DD HH:MM>
phase: <current_phase or "standalone"> · branch: <branch> · v<version>

## Thread
<2-5 sentences: what you were doing and WHY. The narrative that evaporates —
the decision you'd reached, the thing you'd just figured out, the dead end you
ruled out. Write it so a cold reader (future you) re-enters the headspace fast.>

## Next
- [ ] <the single exact next action you were about to take — be specific:
      file, function, command. "apply the guard in issue.go:142 then re-run
      phase-sync", not "continue the fix">

## Open loops
- [ ] <decisions made but not yet applied, things to double-check, deferred bits>
- [ ] <"X test is flaky on cold cache — ignore, not my bug">

## Dirty
- <file> (uncommitted)   ← from git status; helps you not lose in-flight edits
```

Keep it tight. A handoff that's a wall of text is as useless as none — favour a sharp `## Next` and a few real `## Open loops` over exhaustive prose.

## 2. Confirm + amend

Show the drafted handoff inline. Then via `AskUserQuestion`:
- `save` — write it as drafted
- `amend` — user gives corrections/additions in free-form; revise and re-show
- `cancel` — stop without writing anything

Don't write the file until `save`. The user knows what's in their head that you can't see — give them the edit pass.

On a re-pause: show what you're *keeping* from the old file vs *adding*, so nothing silently drops.

## 3. Write + ignore

1. Write the confirmed content to `.dross/handoff.md` (overwrites — it's the single living doc).
2. Ensure it's gitignored so it never lands in a commit or PR. Check the project's `.gitignore`; if `.dross/handoff.md` isn't covered, append it:
   ```
   # dross handoff — local working memory, not tracked
   .dross/handoff.md
   ```
   (Skip silently if already ignored, or if there's no `.gitignore` and `.dross/` as a whole is already ignored.)
3. Stamp state so the pause shows in activity:
   ```
   dross state touch "paused — <one-line thread summary>"
   ```

## 4. Wrap-up

Print:
```
Handoff saved → .dross/handoff.md
  Phase:  <current_phase or standalone> · branch: <branch>
  Next:   <the ## Next item>
  Loops:  <N> open

Next: /dross-resume — replay this handoff next session.
```

Then stop. Pause does not commit, stash, or change branches — it only records. If the working tree is dirty, that's expected and the `## Dirty` section captured it; leave the user's in-flight edits exactly as they are.

## Hard rules

- **Record only — never mutate work.** No commits, no stashing, no `git checkout`, no code edits. Pause writes one markdown file and touches state. That's the whole contract.
- **One living file.** Always `.dross/handoff.md`. Never timestamped copies, never an archive directory — re-pausing updates the same file.
- **Gitignored, always.** The handoff is local working memory. If you can't confirm it's ignored, ignore it before writing the content.
- **You draft, the user steers.** Don't make the user dictate the whole thing — reconstruct from the session, then let them amend. But never invent a `## Next` you can't justify from what actually happened.
- **Don't editorialise the work.** This isn't a review. Capture where things stand; don't suggest refactors or relitigate decisions.
