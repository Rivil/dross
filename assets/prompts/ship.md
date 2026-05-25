# /dross-ship

Open a PR for a verified phase. Pushes the `phase/<id>` branch to the provider (GitHub or Forgejo), opens the PR, and requests human reviewers. The provider's squash-merge collapses per-task commits on merge.

## 0. Pre-flight

1. `dross rule show` — MUST-FOLLOW.
2. `dross status` — confirm a current phase exists; resolve its id (or accept user's `<phase-id>` argument).
3. `dross doctor` — must exit 0. Surfaces:
   - missing `[remote]` config
   - mismatched git origin vs `[remote].url`
   - unset `$auth_env`
   - missing `.dross/** linguist-generated=true` in `.gitattributes` (so review UIs collapse planning artefacts)
   - phase commits leaked onto local main (legacy state — heal with `dross ship recover` first)
   If anything's off, stop and have the user fix before continuing.
4. Read `.dross/phases/<phase-id>/verify.toml` — `[verify].verdict` must be `pass`. If not, stop. Override only if the user explicitly accepts the risk: `dross ship --force-unverified`.
5. **Verify HEAD is on `phase/<id>`** with `git symbolic-ref --short HEAD`. `dross ship` requires this — the phase branch is what gets pushed. If not on it: `git checkout phase/<id>` (it should exist from `dross phase create`).
6. `git status --porcelain` — must be empty. If dirty, ask user to commit or stash first.

## 1. Preview

Run `dross ship --no-push <phase-id>` for a dry run (no push, no PR). To preview the review diff: `git diff <main>..phase/<id>`. To preview the PR body: `dross ship --print-body <phase-id>`.

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
1. Re-checks the verify gate and that HEAD is on `phase/<id>`
2. `git push -u origin phase/<id>`
3. Opens the PR via the provider API
4. Requests reviewers
5. Updates `state.json` with the shipped action + PR URL

## 5. CI gate

After the PR opens, watch CI to completion. Skip this section ONLY if the repo has no `.github/workflows/`, `.forgejo/workflows/`, `.gitea/workflows/`, AND the provider reports no checks for the head SHA.

**Watch checks:**
- GitHub: `gh pr checks <pr-url> --watch --fail-fast` — blocks until all checks finish; non-zero exit on failure.
- Forgejo / Gitea: poll every ~30s — `GET <api_base>/repos/<owner>/<repo>/commits/<sha>/status` (auth header `token $<auth_env>`). Stop when `state` ∈ `success | failure | error`. SHA = head of `phase/<id>`.

If the provider reports no checks were registered (CI workflow missing or not triggered), surface that to the user and ask whether to proceed without CI or stop.

**On failure:**
1. Pull failing logs:
   - GitHub: `gh run view --log-failed` for the run linked from the PR.
   - Forgejo: log URL is in the commit status payload (`target_url`); `WebFetch` it.
2. Diagnose. Edit + commit the fix on `phase/<id>`, one commit per logical fix following `repo.commit_convention`.
3. `git push origin phase/<id>` — appends to the open PR. Do NOT re-run `dross ship` (would open a second PR). If you rebase or amend, use `git push --force-with-lease` (or `dross ship --force`).
4. Loop back to "Watch checks". Cap at 3 fix iterations — if checks still fail after 3 cycles, stop and hand back to the user.

**On pass:** continue to §6.

## 6. Merge gate

`AskUserQuestion`: **"All checks passed on PR #N. Merge now?"** — options: `merge` / `hold`.

If `hold`: skip to §7 with status `awaiting-merge`.

If `merge`:

1. **Squash-merge via provider:**
   - GitHub: `gh pr merge <pr-url> --squash --delete-branch` — also deletes the remote `phase/<id>` branch.
   - Forgejo / Gitea: `POST <api_base>/repos/<owner>/<repo>/pulls/<n>/merge` body `{"Do":"squash"}`, then `DELETE <api_base>/repos/<owner>/<repo>/branches/phase%2F<id>` to remove the remote branch.
2. **Finalize locally**: `dross phase complete <phase-id>` — switches to main, fast-forwards from origin (succeeds cleanly because phase work never touched main), deletes local `phase/<id>`, records the merge in state.json with a chore commit.
3. **Close the board issue** (no-op unless `[remote].board_sync` is on — safe to always run): `dross issue phase-sync <phase-id> --close`.

## 7. Wrap

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
