# /dross-inbox

Triage inbound issues from the project's issue board — bugs and feature requests filed by humans on the board — into dross work: a phase, a milestone backlog entry, a quick task, or dismissed. The board is the intake; dross is where the work gets planned.

This is the inbound half of board sync. The outbound half (planning artefacts → board issues) is automatic in `/dross-milestone`, `/dross-plan`, `/dross-execute`, `/dross-verify`, `/dross-ship`. This command pulls the other direction.

## 0. Pre-flight

1. Run `dross rule show` and treat output as MUST-FOLLOW.
2. Confirm board sync is on: `dross project get remote.board_sync`. If `false`, tell the user to run `dross issue enable` (and set `[remote].provider`, `api_base`, `auth_env` if unset) and stop — there's no board to read otherwise.

## 1. Pull the inbox

```
dross issue pull --mark --json
```

`--mark` records the pull time. The result is a JSON array of open board issues **not** already linked to a dross phase/quick and **not** previously dismissed. Default filter is none; pass `$ARGUMENTS` through, e.g. `dross issue pull --mark --labels bug,enhancement --json`, when the user wants to scope by label.

If the array is empty: print `Inbox clear — no new board issues.` and stop.

## 2. Triage each issue

Show the user a compact list first (number, title, labels). Then walk them one at a time. For each issue, use `AskUserQuestion`:

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

Next: /dross-spec --new for the phases you adopted, or /dross-status.
```

## Hard rules

- **Read-only on the board, except dismiss/link bookkeeping.** This command never closes or edits issues on the board (the filer owns them) and never opens new ones — `/dross-spec`, `/dross-plan`, `/dross-quick` do that downstream. The only writes are dross-side: `dross issue link` / `dross issue dismiss` (board.json) and any milestone/phase artefacts the user explicitly chose.
- **Don't create phase directories directly.** Adopting a phase means routing to `/dross-spec --new` (single owner of `dross phase create`), then `dross issue link`. Never `dross phase create` from here.
- **One issue, one decision.** Don't silently fold multiple board issues into one phase unless the user asks — surface them separately so the board↔phase mapping stays 1:1 and `phase-sync` can keep the right issue updated.
- **Dismiss is reversible only by hand.** A dismissed issue won't reappear in `/dross-inbox`. If the user is unsure, prefer Skip.
