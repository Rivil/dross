# /dross-ship

Open a PR for a verified phase. Pushes the `phase/<id>` branch to the provider (GitHub or Forgejo), opens the PR, and requests human reviewers. The provider's squash-merge collapses per-task commits on merge.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): surface one decision per turn — the §2 body override, §3 reviewers, and §6 merge gate are each their own `AskUserQuestion`, never bundled. The §1 PR-body preview is the deliberate exception to the no-dump rule: it's outward-facing content about to be published, so the user sees it in full before authorizing the post.

## 0. Pre-flight

1. `dross rule show` and `dross interaction show` — treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
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

## 0.5 Non-interactive fast-path (`--auto`)

If `--auto` is in this command's arguments, run to completion non-interactively — suitable to call from a script or loop. Skip every interactive turn below:

- **§1 body-preview dump** — skipped; nothing is shown for approval.
- **§2 body override** — skipped; no AskUserQuestion, the generated body is used.
- **§3 reviewers** — skipped; no AskUserQuestion, zero reviewers are requested for this run (the configured `remote.reviewers` default is left untouched).
- **§3.5 landmark merge + ARCHITECTURE.md auto-backfill** — both skipped under `--auto` (they show a diff / drive LLM generation and ask for an OK, which a non-interactive run can't answer); fold landmarks in — and self-heal an absent doc — later via an interactive ship or `/dross-architecture`.

§0 pre-flight still runs — the verify-pass gate is **not** bypassed (add `--force-unverified` too only if you must ship unverified). Then shell out directly:

```
dross ship --auto <phase-id>
```

Add `--json` for machine-readable output to capture in the caller: `dross ship --auto --json <phase-id>` prints a single `{url, number, result}` object on stdout and suppresses the narration.

`--auto` opens the PR and **returns without merging** — do not drive the §5 CI-watch or §6 merge gate; the caller (or a later interactive `/dross-ship`) owns the merge. Report the PR URL and stop.

Everything below (§1–§7) is the interactive path, taken when `--auto` is absent.

## 1. Preview

Run `dross ship --no-push <phase-id>` for a dry run (no push, no PR). To preview the review diff: `git diff <main>..phase/<id>`. To preview the PR body: `dross ship --print-body <phase-id>`.

Show the user — the PR body is outward-facing content about to be published, so show it **in full** here (the deliberate `ship_body_preview` exception to the no-dump rule):
- the resolved title (`phase <id>: <spec title>` by default)
- the resolved body in full (`dross ship --print-body <phase-id>` — prints the generated markdown without building or pushing)
- the reviewers list (`dross project get remote.reviewers`)

## 2. Body override (optional)

Ask the user (`AskUserQuestion`): **Use generated body, or write your own?**

If override:
1. Drop a starter file at `.dross/phases/<phase-id>/pr-body.md` containing the generated body (the user edits it).
2. Wait for the user to confirm they're done editing.
3. Pass `--body-file .dross/phases/<phase-id>/pr-body.md` to `dross ship`.

## 3. Reviewers

Read the configured list (`dross project get remote.reviewers`) and drive a single propose-and-react turn rather than silently writing config. `AskUserQuestion`: **"Request reviewers `<alice,bob>`?"** — lead with `use these` (the configured default), offer `change` (user names the list) and, if the list is empty, `none for now`. Only on `change` run `dross project set remote.reviewers "alice,bob"` before shipping. (Per-PR overrides aren't supported in v1 — the project default applies, so this write updates the project default.)

## 3.5 Merge landmarks into ARCHITECTURE.md

Before pushing, fold this phase's landmarks into the architecture doc so the PR
carries an up-to-date `ARCHITECTURE.md` (c-6).

1. `dross changes show <phase-id>` prints JSON; each task record carries a typed
   `landmarks` array of `{feature, symbol, loc, what}` objects (written by execute
   §1f). Read those **structured fields** directly — do not parse a `notes` string
   for the landmark (the free-form `notes` field is no longer the landmark
   carrier).
2. **Self-heal an absent doc.** If `ARCHITECTURE.md` is absent at repo root, the
   repo predates the doc — **automatically backfill it now**, don't punt to a
   manual run: read `~/.claude/dross/prompts/architecture.md` and run that backfill
   generation (scan the code + git history, emit the feature-organized doc from the
   skeleton/`EntryTemplate`) so this ship self-heals the repo, then continue to the
   landmark merge below. There is no Go backfill engine — the prose is generated by
   this prompt-driven flow. **Non-blocking:** if the backfill can't run or fails,
   skip it with a one-line note and continue — a missing doc must never block the
   ship. (Under `--auto` this whole §3.5 — backfill included — is skipped per §0.5,
   since a non-interactive run can't drive the generation.)
3. For each landmark, merge it into the entry whose **feature** matches, updating
   that entry **in place**:
   - Refresh the one-line description and symbol-link bullets to reflect the new
     work. Keep the fixed entry template (heading + one-line + symbol bullets +
     provenance) — no prose paragraphs.
   - Update the provenance breadcrumb: append `· extended <phase-id>` (unless this
     phase is already credited) plus the representative commit short-sha.
   - Create a **new** entry only when no existing feature matches. **Never** add a
     per-phase heading or a "Phase NN" section — the doc is organized by feature,
     never by phase. A duplicate per-phase heading appearing means the merge
     regressed.
4. Show the `git diff` of `ARCHITECTURE.md`. On the user's OK, commit it onto
   `phase/<id>` (it lives at repo root, so the provider squash-merge carries it
   into the PR):
   ```
   git add ARCHITECTURE.md
   git commit -m "docs(<phase-slug>): merge phase landmarks into ARCHITECTURE.md"
   ```
   Match the repo's existing trailer convention. The tree is clean again
   afterwards, so §4's clean-tree re-check passes.

## 4. Ship

Run `dross ship <phase-id>`, optionally with `--draft` and/or `--body-file`.

The CLI:
1. Re-checks the verify gate and that HEAD is on `phase/<id>`
2. `git push -u origin phase/<id>`
3. Opens the PR via the provider API
4. Requests reviewers
5. Updates `state.json` with the shipped action + PR URL

## 5. CI gate

After the PR opens, watch CI to completion. Skip this section ONLY if the repo has no `.github/workflows/`, `.forgejo/workflows/`, `.gitea/workflows/`, `.gitlab-ci.yml`, AND the provider reports no checks for the head SHA.

**Watch checks:**
- GitHub: `gh pr checks <pr-url> --watch --fail-fast` — blocks until all checks finish; non-zero exit on failure.
- Forgejo / Gitea: poll every ~30s — `GET <api_base>/repos/<owner>/<repo>/commits/<sha>/status` (auth header `token $<auth_env>`). Stop when `state` ∈ `success | failure | error`. SHA = head of `phase/<id>`.
- GitLab: poll every ~30s — `GET <api_base>/projects/<id>/pipelines?sha=<sha>` (auth header `PRIVATE-TOKEN: $<auth_env>`, or `Authorization: Bearer` when `remote.auth_scheme = bearer`); `<id>` is the URL-encoded `owner/repo` (or numeric `remote.project_id`). Read the latest pipeline's `status` and apply the locked mapping: `success` → pass (go to §6); `failed` or `canceled` → fail (drop to **On failure**); `running` / `pending` / `created` / `preparing` → keep polling; `manual` / `skipped` → **do not guess** — surface to the user and ask whether to proceed without a green pipeline. SHA = head of `phase/<id>`.

If the provider reports no checks were registered — GitHub/Forgejo report none, **or** GitLab returns an empty pipelines array (no pipeline for the SHA) — surface that to the user and ask whether to proceed without CI or stop.

**On failure:**
1. Pull failing logs:
   - GitHub: `gh run view --log-failed` for the run linked from the PR.
   - Forgejo: log URL is in the commit status payload (`target_url`); `WebFetch` it.
   - GitLab: the failed job's `web_url` is in the pipeline's jobs (`GET <api_base>/projects/<id>/pipelines/<pipeline-id>/jobs`); `WebFetch` the trace or surface the job URL.
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
   - GitLab: `PUT <api_base>/projects/<id>/merge_requests/<iid>/merge` body `{"squash":true,"should_remove_source_branch":true}` (auth header as in §5) — the squash collapses per-task commits and `should_remove_source_branch` deletes the remote `phase/<id>` branch in one call.
2. **Finalize locally**: `dross phase complete <phase-id>` — switches to main, fast-forwards from origin (succeeds cleanly because phase work never touched main), deletes local `phase/<id>`, records the merge in state.json with a chore commit.
3. **Close the board issue** (no-op unless `[remote].board_sync` is on — safe to always run): `dross issue phase-sync <phase-id> --close`.

> **Two merge levels (v0.7 branch topology).** Phase PRs **squash-merge** into
> their base — `milestone/<version>` when a milestone is active, else `main` —
> collapsing per-task commits into one per phase. The milestone itself lands in
> `main` separately via `dross milestone complete` (opens one `milestone/<version>
> → main` PR); that integration PR must be merged as a **merge commit, not a
> squash**, so `main` keeps the per-phase history — do this even if
> `repo.squash_merge` is set. After it merges, run `dross milestone complete
> <version> --finalize` to fast-forward `main` and delete the milestone branch.

## 7. Wrap

Print:

```
Shipped 01-meal-tagging
PR:  https://forge.example/me/proj/pulls/42
Reviewers: alice, bob
CI:  passed
Status: merged | awaiting-merge

Next: /dross-status — see where things stand.
      ↳ /dross-spec --new "<title>" — start the next phase.
```

If the PR opened but reviewer-request failed, surface that — it's non-fatal but the user should know.

## Recovery

Most ships finish clean: §6's squash-merge plus `dross phase complete` fast-forwards local main from origin and tears down the branch. When the merge step goes sideways, recover with a dross command — **never hand-edit `.dross/` or re-commit it by hand.** That manual surgery is exactly what drifted in the past; a dross command owns the restore and the commit. The three mid-merge failure states and their one-command fixes:

1. **Fast-forward abort.** `dross phase complete` stops with a "fast-forward … failed" error — local main has diverged from origin/main (a stray commit on main, or a legacy completion chore). Fix: **`dross phase complete --recover`** — it resets main to origin and restores the cumulative `.dross/` tree in one shot, then finishes the completion. Pass `--recover` only after reading the abort: it is a destructive reset of local main.

2. **Diverged main (legacy / strip-filter era).** Local main carries phase commits that origin/main lost to an old `.dross/`-stripping squash, so main holds a parallel history. Fix: **`dross ship recover`** — the standalone healer. Same reset-and-restore as `--recover`, usable outside the merge loop.

3. **Dirty tree after push.** `dross ship` returned but `git status` is not clean — an older ship left its post-push state write uncommitted, which then blocks the provider's `--delete-branch` and `dross phase complete`. Fix: re-run **`dross ship`** — it is idempotent and commits its own post-push `.dross/` state, leaving a clean tree. You stage nothing yourself.

If you find yourself reaching for git plumbing against `.dross/`, stop — one of the three commands above already covers it.

## Subagent review panel — DEFERRED

The original design called for a 4-lens subagent review panel (security, code quality, test efficacy, spec fidelity) posting findings as PR comments. v1 ships without this; track it as a separate task once `/dross-ship` has been validated against real Forgejo + GitHub PRs.
