# /dross-pause

Capture a handoff before you stop — so next session your brain doesn't blank out. The point is to write down the **thread** (what you were doing and *why*), the **exact next action**, and the **open loops**, while it's all still fresh in this session. `/dross-resume` replays it.

Use when:
- You're stopping mid-phase (or even mid-task) and won't finish today.
- You're stopping at a clean boundary but the *why* / next-move lives only in this chat.

`$ARGUMENTS` is optional freeform — a one-line "what I was doing" hint. If empty, infer it from the session.

The handoff is a **living document** at `.dross/handoff.md`: one file, gitignored, pruned by resume. Not an archive — pausing again updates the same file.

**Confirm the handoff as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by `dross interaction show` in the pre-flight below): the §2 confirm+amend is a single propose-and-react turn. Showing the drafted handoff inline is a deliberate exception — the user is confirming their own working memory, like ship's PR-body preview — not an artifact-dump violation.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for the §2 confirm+amend turn.
2. **Gate — is there an initialised root to pause?** Probe with:
   ```
   dross state show
   ```
   Three outcomes:

   - **Exit 0** — there's a root. Continue to step 3.
   - **Non-zero, and the message is the not-a-dross-repo signal** (`no .dross directory found…`, or `not a dross repo: … is incomplete`) — the root is either **absent** or **incomplete**, and those are one case, not two. Print exactly one line and stop:
     ```
     not a dross repo — nothing to pause. Run `dross onboard` to adopt this directory into dross.
     ```
     Then **write nothing**: no `.dross/`, no `.dross/handoff.md`, no `.gitignore` edit, no `dross state touch`. Never run `dross onboard or dross init` yourself — name the repair and let the user choose it. Skip every remaining step, including the §4 wrap-up block.
   - **Non-zero with any other message** — the root is there but a file under it is broken (a corrupt `state.json`, an unreadable `.dross/`). Surface that error as-is and stop. Do *not* call this "not a dross repo" and do *not* point at `dross onboard`: the repair is fixing the file the error names, and adopting the directory would not fix it.
3. Read `.dross/state.json`. Note `current_phase`, `current_phase_status`, `version`. (May be empty — that's fine, you can pause standalone work too.)
4. Capture the mechanical snapshot:
   ```
   git symbolic-ref --short HEAD        # current branch
   git status --porcelain               # dirty / untracked files
   dross status                         # phase + task progress + next-runnable
   ```
5. If `.dross/handoff.md` **already exists**, read it. This is a re-pause — you'll merge into it (keep still-open loops, add new ones), not clobber it.

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
- [ ] <decisions made but not yet applied, things to double-check>
- [ ] <"X test is flaky on cold cache — ignore, not my bug">

## Dirty
- <file> (uncommitted)   ← from git status; helps you not lose in-flight edits
```

Keep it tight. A handoff that's a wall of text is as useless as none — favour a sharp `## Next` and a few real `## Open loops` over exhaustive prose.

**A finding is not an open loop — file it.** If something you'd write under `## Open loops` is really a *finding* — a bug, a gap, a piece of work someone will need to do later — it belongs in the deferred backlog, not in a document that only this handoff's reader ever sees. File it instead:

```
dross deferred add "<the finding>" --why "<what makes it real>"
dross deferred add "<the finding>" --target <phase-slug>    # when you know where it belongs
```

It lands in the current phase's spec (or the project-level store when there's no usable phase home — the verb never refuses for want of one), shows up in `dross deferred list`, and mirrors onto the issue board in the same command. Handing a finding to the next session as a bullet is how it goes homeless: `handoff.md` is gitignored and gets rewritten on the next pause. Keep `## Open loops` for things that are genuinely about *this* pause — a decision you haven't applied yet, a check to redo on resume.

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

state is on disk — safe to /clear · fresh session: /dross-resume
```

Then stop. Pause does not commit, stash, or change branches — it only records. If the working tree is dirty, that's expected and the `## Dirty` section captured it; leave the user's in-flight edits exactly as they are.

## Hard rules

- **No initialised root → refuse and write nothing.** An absent `.dross/` and an incomplete one are the same refusal (§0 step 2): never create `.dross`, never write `.dross/handoff.md`, never edit `.gitignore`, never touch state. Pause records a session; it does not bootstrap a project.
- **Record only — never mutate work.** No commits, no stashing, no `git checkout`, no code edits. Pause writes one markdown file and touches state. That's the whole contract.
- **One living file.** Always `.dross/handoff.md`. Never timestamped copies, never an archive directory — re-pausing updates the same file.
- **Gitignored, always.** The handoff is local working memory. If you can't confirm it's ignored, ignore it before writing the content.
- **You draft, the user steers.** Don't make the user dictate the whole thing — reconstruct from the session, then let them amend. But never invent a `## Next` you can't justify from what actually happened.
- **Don't editorialise the work.** This isn't a review. Capture where things stand; don't suggest refactors or relitigate decisions.
