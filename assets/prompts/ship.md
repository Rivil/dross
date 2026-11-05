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
4. Read `.dross/phases/<phase-id>/verify.toml` — `[verify].verdict` must be `pass`. If not, stop. A pass is also refused when it is **stale**: `dross ship` compares the tree the run measured (`[verify].measured_tree`) with the work tree, and a file outside `.dross/` and `ARCHITECTURE.md` that changed, appeared or went away since the run means the verdict no longer covers what would ship — it names those files; re-run `/dross-verify <phase-id>`. A verdict recorded before trees were kept ships with a freshness-unknown warning. Override either refusal only if the user explicitly accepts the risk: `dross ship --force-unverified`.
5. **Verify HEAD is on `phase/<id>`** with `git symbolic-ref --short HEAD`. `dross ship` requires this — the phase branch is what gets pushed. If not on it: `dross phase checkout <phase-id>` (it should exist from `dross phase create`). Use the dross verb, never raw git — it switches through the guard that refuses a branch whose tracked `.dross/state.json` would overwrite your live machine-local one.
6. `git status --porcelain` — must be empty. If dirty, ask user to commit or stash first.

**When** `auto=on` — `--auto` is in this command's arguments: `dross prompt partial ship auto`

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

Before pushing, fold this phase's landmarks into the architecture doc so the PR carries an up-to-date `ARCHITECTURE.md`.

1. `dross changes show <phase-id>` prints JSON; each task record carries a typed `landmarks` array of `{feature, symbol, loc, what}` objects (written by execute §1f). Read those **structured fields** directly — never parse the free-form `notes` string for a landmark.
2. **Self-heal an absent doc.** If `ARCHITECTURE.md` is absent at repo root, **automatically backfill it now** instead of punting to a manual run: read `~/.claude/dross/prompts/architecture.md` and run its backfill generation (scan the code + git history, emit the feature-organized doc from the skeleton/`EntryTemplate`), then continue to the merge below. **Non-blocking:** if the backfill can't run or fails, skip it with a one-line note — a missing doc must never block the ship. (`--auto` skips this whole section, backfill included.)
3. Merge each landmark into the entry whose **feature** matches, **in place**: refresh the one-line description and symbol-link bullets, keeping the fixed entry template (heading + one-line + symbol bullets + provenance — no prose paragraphs), and append `· extended <phase-id>` to the provenance breadcrumb (unless already credited) plus the representative commit short-sha. Create a **new** entry only when no feature matches. **Never** add a per-phase heading or a "Phase NN" section — the doc is organized by feature; a per-phase heading means the merge regressed.
4. Show the `git diff` of `ARCHITECTURE.md`. On the user's OK, run the full suite — a bare `dross test`, no selector and no `--files` — before committing: the commit gate admits a commit only with a full green recorded for exactly that tree, which the doc edit changed:
   ```
   dross test
   ```
   On green, commit it onto `phase/<id>` (the provider squash-merge carries it into the PR):
   ```
   git add ARCHITECTURE.md
   git commit -m "docs(<phase-slug>): merge phase landmarks into ARCHITECTURE.md"
   ```
   Match the repo's trailer convention. The tree is clean again for §4's re-check. A gate refusal is a stop: read its rule and remedy, and never route around one.

## 4. Ship

Run `dross ship <phase-id>`, optionally with `--draft` and/or `--body-file`.

The CLI:
1. Re-checks that HEAD is on `phase/<id>`, then the verify gate — a pass whose measured tree no longer matches the work tree is refused as stale, naming the changed files
2. Sends `.dross` chores left on the base (a pause snapshot, a gate auto-commit) to origin: pushed straight to it when origin takes direct pushes, or — when the base is protected (`dross protect --check <base>` reads `protected`) — through a **chore PR** from `dross-chores/<base>`, armed to auto-merge as a merge commit. It prints the chore PR's URL; if auto-merge is unavailable on the repo it says so, and that PR is merged by hand. An `unknown` answer pushes nothing and names the fix.
3. Gates `phase/<id>` on origin — fetches and compares; a branch that is ahead or new is pushed (`-u`), a level one is left alone, a behind-only one refuses naming `git pull --rebase origin phase/<id>`, a diverged one refuses naming the pull or `dross ship --force`
4. Opens the PR via the provider API — skipped when the phase's record (or the provider, when the record carries no number) already has an open PR for `phase/<id>`
5. Requests reviewers
6. Commits the PR record (`chore(dross): record PR #N for <id>`) and pushes it through the same origin gate
7. **Only then** marks the phase shipped — `state.json` and the record's status flip together, and the marker commit is pushed. Until that push lands the phase still reads verified, and `dross status` names the retry.

A non-zero `dross ship` exit, or a dirty tree after it, goes to the Recovery fetch at the end of §6.

## 5. CI gate

After the PR opens, watch CI to completion. Skip this section ONLY if the repo has no `.github/workflows/`, `.forgejo/workflows/`, `.gitea/workflows/`, `.gitlab-ci.yml`, AND the provider reports no checks for the head SHA.

**Watch checks** — SHA = head of `phase/<id>` (`git rev-parse origin/phase/<id>`):
- GitHub: never gate on `gh pr checks --watch`. It exits 0 as soon as the checks registered **so far** pass, and a fast third-party check (GitGuardian, say) can be the only one registered while the Actions run is still queued — it reads green with CI unrun. Watch the runs themselves, in this order:
  1. Wait for the runs to register: poll `gh run list --commit <sha> --json databaseId,workflowName,status` every ~15s until it lists at least one run, for up to ~10 minutes. A repo with workflows whose run never registers in that window joins the no-checks path below — surface it, never read it as a pass.
  2. Watch each run to completion: `gh run watch <run-id> --exit-status` — non-zero on failure.
  3. Once every run has finished, read the full check set: `gh pr checks <pr-url>` (no `--watch`). Every check must pass, third-party ones included; one still pending → re-read after ~30s; any failure → CI failed.
- Forgejo / Gitea: every ~30s `GET <api_base>/repos/<owner>/<repo>/commits/<sha>/status` (header `token $<auth_env>`) until `state` ∈ `success | failure | error`.
- GitLab: every ~30s `GET <api_base>/projects/<id>/pipelines?sha=<sha>` (header `PRIVATE-TOKEN: $<auth_env>`, or `Authorization: Bearer` when `remote.auth_scheme = bearer`; `<id>` = URL-encoded `owner/repo` or numeric `remote.project_id`). The latest pipeline's `status`, per the locked mapping: `success` → pass; `failed` / `canceled` → fail; `running` / `pending` / `created` / `preparing` → keep polling; `manual` / `skipped` → **do not guess** — ask the user whether to proceed without a green pipeline.

No checks registered — GitHub/Forgejo report none, **or** GitLab returns an empty pipelines array — surface it and ask whether to proceed without CI or stop.

**When** `ci=failed` — a check or pipeline failed: `dross prompt partial ship ci-failure`

**On pass:** continue to §6.

## 6. Merge gate

**Re-check freshness first:** re-run `dross ship <phase-id>` before asking to merge. A fix pushed after the PR opened is in the merge, and a pass that predates it does not cover it. If ship refuses the pass as stale, do not merge: run `/dross-verify <phase-id>`, commit its record, re-run `dross ship <phase-id>`, and go back to §5's Watch checks.

`AskUserQuestion`: **"All checks passed on PR #N. Merge now?"** — options: `merge` / `hold`.

If `hold`: skip to §7 with status `awaiting-merge`.

If `merge`:

1. **Squash-merge via provider — merge only, no branch teardown:**
   - GitHub: `gh pr merge <pr-url> --squash`
   - Forgejo / Gitea: `POST <api_base>/repos/<owner>/<repo>/pulls/<n>/merge` body `{"Do":"squash"}`
   - GitLab: `PUT <api_base>/projects/<id>/merge_requests/<iid>/merge` body `{"squash":true}` (auth header as in §5)

   **`dross phase complete` performs the local AND remote `phase/<id>` deletion, on every provider.** The merge call is the whole step — no delete flag, no remove-source-branch field, no branch-removal API call. GitHub's delete-on-merge does a raw checkout of the base outside dross's guard, which once destroyed a live `state.json`.
2. **Mark the board issue shipped** (it exits 0 and does nothing when `[board].enabled` is false; a non-zero exit is a board failure — surface it, never read it as the disabled no-op): `dross issue phase sync <phase-id> --status shipped`. The PR is merged but the phase isn't finalized, so `shipped` is the honest state.
3. **Finalize locally and on the board**: `dross phase complete <phase-id>` — switches to the phase's recorded base (never inferred), fast-forwards it from origin, writes the completion record into the machine-local, gitignored `.dross/state.json` (no commit carries it), then deletes `phase/<id>` locally and on origin. Last, it finalizes the board (same `[board].enabled` rule): every task card moves to `task-complete` and the phase card to `complete`, all closed, and a card never created is created closed; the `board.json` links are committed and published like any `.dross` chore. An unreachable board never undoes the completion — complete exits non-zero naming `dross issue phase finalize <phase-id>`, a board failure to surface (see Recovery), not the disabled no-op.
4. **Reconcile the milestone backlog** (same board rule; a non-zero exit is a failure to surface): `dross issue backlog sync --phase <phase-id>`. It resolves the version from the phase's `spec.toml`, pushes the live backlog, then closes the mirrors whose artefact resolved — a `slug:` whose phase is scaffolded, a dismissed idea, a routed item with a disposition record. A routed item is NOT closed because its target phase shipped. A mirror it cannot show resolved is named on stderr and left open, never closed on a guess. Exits 0 doing nothing for a phase outside any milestone.

> **Two merge levels.** Phase PRs **squash-merge** into their base — `milestone/<version>` when a milestone is active, else `main`. The milestone lands separately: `dross milestone complete` opens **one integration PR against the branch it was cut from** — the recorded parent `milestone/<version>` while that is unmerged, else `main` (read the command's output). Merge it as a **merge commit, not a squash**, even if `repo.squash_merge` is set; then `dross milestone complete <version> --finalize` fast-forwards `main` only when the merge landed there and deletes the milestone branch, refusing while a stacked milestone or an open PR still depends on it.

**When** `ship_result=failed` — `dross ship` or `dross phase complete` exited non-zero, or left the tree dirty: `dross prompt partial ship recovery`

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

state is on disk — safe to /clear · fresh session: /dross-status
```

If the PR opened but reviewer-request failed, surface that — it's non-fatal but the user should know.
