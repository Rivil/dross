# /dross-ship

Open a PR for a verified phase. Filters `.dross/` out of the review branch, pushes, opens the PR via the project's provider (GitHub or Forgejo), and requests human reviewers.

## 0. Pre-flight

1. `dross rule show` — MUST-FOLLOW.
2. `dross status` — confirm a current phase exists; resolve its id (or accept user's `<phase-id>` argument).
3. `dross doctor` — must exit 0. Surfaces:
   - missing `[remote]` config
   - mismatched git origin vs `[remote].url`
   - unset `$auth_env`
   If anything's off, stop and have the user fix via `/dross-options` or `dross env set <KEY>` before continuing.
4. Read `.dross/phases/<phase-id>/verify.toml` — `[verify].verdict` must be `pass`. If not, stop. Override only if the user explicitly accepts the risk: `dross ship --force-unverified`.
5. `git status --porcelain` — must be empty. `dross ship`'s branch filter requires a clean working tree. If dirty, ask user to commit or stash first.

## 1. Preview

Run `dross ship --no-push <phase-id>` — this builds the squash branch `pr/<phase-id>` locally without pushing, so the user can:
- `git diff main..pr/<phase-id>` to inspect the actual review diff (no `.dross/`)
- decide whether the title/body need overriding

Show the user:
- the resolved title (`phase <id>: <spec title>` by default)
- the resolved body (`dross ship --print-body <phase-id>` — prints the generated markdown without building or pushing)
- the reviewers list (`dross project get remote.reviewers`)

## 2. Body override (optional)

Ask the user (`AskUserQuestion`): **Use generated body, or write your own?**

If override:
1. Drop a starter file at `.dross/phases/<phase-id>/pr-body.md` containing the generated body (the user edits it).
2. Wait for the user to confirm they're done editing.
3. Pass `--body-file .dross/phases/<phase-id>/pr-body.md` to `dross ship`.

## 3. Reviewers (optional)

`dross project get remote.reviewers` shows the configured list. If empty or the user wants different reviewers for this PR, run `dross project set remote.reviewers "alice,bob"` before shipping. (Per-PR overrides aren't supported in v1 — the project default applies.)

## 4. Ship

Run `dross ship <phase-id>`, optionally with `--draft` and/or `--body-file`.

The CLI:
1. Re-checks the verify gate
2. FilterSquash → `pr/<phase-id>`
3. `git push -u origin pr/<phase-id>`
4. Opens the PR via provider API
5. Requests reviewers
6. Updates `state.json` with the shipped action + PR URL

## 5. Wrap

Print the PR URL, reviewers, and a one-line next step:

```
Shipped 01-meal-tagging
PR:  https://forge.example/me/proj/pulls/42
Reviewers: alice, bob
Next: /dross-status (or open the PR for review)
```

If the PR opened but reviewer-request failed, surface that — it's non-fatal but the user should know.

## Subagent review panel — DEFERRED

The original design called for a 4-lens subagent review panel (security, code quality, test efficacy, spec fidelity) posting findings as PR comments. v1 ships without this; track it as a separate task once `/dross-ship` has been validated against real Forgejo + GitHub PRs.
