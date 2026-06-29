# /dross-inbox

Triage inbound issues from the project's issue board — bugs and feature requests filed by humans on the board — into dross work: a phase, a milestone backlog entry, a quick task, or dismissed. The board is the intake; dross is where the work gets planned.

This is the inbound half of board sync. The outbound half (planning artefacts → board issues) is automatic in `/dross-milestone`, `/dross-plan`, `/dross-execute`, `/dross-verify`, `/dross-ship`. This command pulls the other direction.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): triage one issue per turn, leading with a recommended destination the user accepts or redirects.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Check board sync: `dross project get board.enabled`. If `false`, there's no board to read — print one line (`board sync off — skipping board issues, triaging local deferred items only`) and **skip the board-issue source** (§1's `dross issue pull`), proceeding straight to the deferred `someday` source below. Don't stop: the local deferred backlog is still triageable on a board-less repo. (To wire a board up later, the user runs `dross issue enable` and sets `[board].provider`, `base_url`, `auth_env`.)

## 1. Pull the inbox

```
dross issue pull --mark --json
```

(Skip this pull entirely when `board.enabled` is off — §0 already routed you past the board source straight to the deferred ideas below.)

`--mark` records the pull time. The result is a JSON array of open board issues **not** already linked to a dross phase/quick and **not** previously dismissed. Default filter is none; pass `$ARGUMENTS` through, e.g. `dross issue pull --mark --labels bug,enhancement --json`, when the user wants to scope by label.

**Second source — `someday` deferred ideas.** The board isn't the only intake: ideas punted during `/dross-spec` and never routed anywhere ("someday") are the local half of the backlog. Pull them too:
```
dross deferred list --someday --json
```
Each entry carries its originating `source` phase and `index` — the handle you'll pass to `dross deferred route` when triaging it. Treat them as a second batch of inbound items alongside the board issues.

If **both** sources are empty: print `Inbox clear — no new board issues or deferred ideas.` and stop.

## 2. Triage each issue

Show the user a compact list first (number, title, labels). Then walk them **one issue per turn** — never bundle multiple issues into a single triage decision. For each issue, use `AskUserQuestion`:

**"#<n> <title> — what should happen?"**

- **New phase** — this is a unit of work. Route to `/dross-spec --new "<title>"` to create the phase (that command owns phase-directory creation). Once the phase id exists (e.g. `04-…`), adopt the board issue so it becomes the phase's tracking issue rather than spawning a duplicate:
  ```
  dross issue link <phase-id> <n>
  ```
  From here the normal flow takes over — `/dross-plan` will update issue #<n> with the task checklist.
- **Milestone backlog** — relevant but not now. Add it as a phase id (intent) to a milestone's `phases` list via `dross milestone add <version> phases "<short-id>"`, and leave the issue open (it'll be adopted when that phase is specced). Note for the user which milestone.
- **Quick task** — small one-shot. Route to `/dross-quick "<title>"`. The quick flow opens its own board issue; dismiss the original so it doesn't double up: `dross issue dismiss <n>`.
- **Dismiss** — not actionable / wontfix / duplicate. `dross issue dismiss <n>` so it never resurfaces here. (This does **not** close the issue on the board — the human who filed it still owns it; dismiss only removes it from dross triage.)
- **Skip** — leave it for next time (no state change).

**Deferred `someday` ideas route through the same funnel** — one item per turn, same triage gate, same four destinations:
- **New phase** — route to `/dross-spec --new "<text>"`; once the phase id exists, mark the idea routed: `dross deferred route <source> <index> --target "<new-slug>"`.
- **Milestone backlog** — coin a slug, then `dross milestone add <version> phases "<slug>"` and `dross deferred route <source> <index> --target "<slug>"` so it re-surfaces 1:1 when that phase is specced.
- **Quick task** — route to `/dross-quick "<text>"` for a one-shot; the idea is handled outside the phase flow.
- **Dismiss** — not worth doing. Retire it with `dross deferred dismiss <source> <index>` (reversible via `--undo`): the item moves to a dismissed state and stops appearing in `dross deferred list --someday`, with no hand-editing of specs.

`<source>` and `<index>` come straight from the `dross deferred list --someday --json` entry.

Honor the user's call exactly — don't auto-decide. If they want to batch ("dismiss all the stale ones"), confirm the set before acting.

## 3. Wrap

Print a one-block summary:
```
Triaged <N> issue(s):
  → phase:     #12 (linked to 04-rate-limiting)
  → milestone: #15 (v0.3 backlog)
  → quick:     #18
  → dismissed: #21, #22
  → skipped:   #25

Next: /dross-spec --new "<title>" — scaffold the phases you adopted (or /dross-status).
```

## Hard rules

- **Read-only on the board, except dismiss/link bookkeeping.** This command never closes or edits issues on the board (the filer owns them) and never opens new ones — `/dross-spec`, `/dross-plan`, `/dross-quick` do that downstream. The only writes are dross-side: `dross issue link` / `dross issue dismiss` (board.json) and any milestone/phase artefacts the user explicitly chose.
- **Don't create phase directories directly.** Adopting a phase means routing to `/dross-spec --new` (single owner of `dross phase create`), then `dross issue link`. Never `dross phase create` from here.
- **One issue, one decision.** Don't silently fold multiple board issues into one phase unless the user asks — surface them separately so the board↔phase mapping stays 1:1 and `phase-sync` can keep the right issue updated.
- **Dismiss is reversible only by hand.** A dismissed issue won't reappear in `/dross-inbox`. If the user is unsure, prefer Skip.
