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

## 5. CI gate

After the PR opens, watch CI to completion. Skip this section ONLY if the repo has no `.github/workflows/`, `.forgejo/workflows/`, `.gitea/workflows/`, AND the provider reports no checks for the head SHA.

**Watch checks:**
- GitHub: `gh pr checks <pr-url> --watch --fail-fast` — blocks until all checks finish; non-zero exit on failure.
- Forgejo / Gitea: poll every ~30s — `GET <api_base>/repos/<owner>/<repo>/commits/<sha>/status` (auth header `token $<auth_env>`). Stop when `state` ∈ `success | failure | error`. SHA = head of `pr/<phase-id>`.

If the provider reports no checks were registered (CI workflow missing or not triggered), surface that to the user and ask whether to proceed without CI or stop.

**On failure:**
1. Pull failing logs:
   - GitHub: `gh run view --log-failed` for the run linked from the PR.
   - Forgejo: log URL is in the commit status payload (`target_url`); `WebFetch` it.
2. Diagnose. Edit + commit the fix on the active source branch (the one you ran `dross ship` from), one commit per logical fix following `repo.commit_convention`.
3. Update the PR head: `git push origin <source>:pr/<phase-id> --force-with-lease`. Do NOT re-run `dross ship` — that opens a second PR. (`dross ship --force-branch` rebuilds the squash branch locally if you'd rather rebuild from scratch; you still need the manual force-push.)
4. Loop back to "Watch checks". Cap at 3 fix iterations — if checks still fail after 3 cycles, stop and hand back to the user.

**On pass:** continue to §6.

## 6. Merge gate

`AskUserQuestion`: **"All checks passed on PR #N. Merge now?"** — options: `merge` / `hold`.

If `hold`: skip to §7 with status `awaiting-merge`.

If `merge`:

1. **Squash-merge via provider:**
   - GitHub: `gh pr merge <pr-url> --squash --delete-branch` — also deletes the remote `pr/<phase-id>` branch.
   - Forgejo / Gitea: `POST <api_base>/repos/<owner>/<repo>/pulls/<n>/merge` body `{"Do":"squash"}`, then `DELETE <api_base>/repos/<owner>/<repo>/branches/pr%2F<phase-id>` to remove the remote branch.
2. **Sync local main:** `git fetch origin && git checkout <main-branch> && git pull --ff-only` (use `repo.git_main_branch` from project.toml). If fast-forward fails (local main diverged because phase commits live on it), stop and surface to the user — don't auto-resolve.
3. **Delete local PR branch:** `git branch -D pr/<phase-id>`.

## 7. Wrap

Commit the post-ship state update so `.dross/` doesn't sit dirty:

```
git add .dross/state.json
git commit -m "chore(dross): record ship for <phase-id>"
```

(Use `repo.commit_convention` from project.toml. If a merge happened in §6, include `+ merge` in the message.)

Print:

```
Shipped 01-meal-tagging
PR:  https://forge.example/me/proj/pulls/42
Reviewers: alice, bob
CI:  passed
Status: merged | awaiting-merge
Next: /dross-status (or /dross-spec --new for the next phase)
```

If the PR opened but reviewer-request failed, surface that — it's non-fatal but the user should know.

## Subagent review panel — DEFERRED

The original design called for a 4-lens subagent review panel (security, code quality, test efficacy, spec fidelity) posting findings as PR comments. v1 ships without this; track it as a separate task once `/dross-ship` has been validated against real Forgejo + GitHub PRs.
