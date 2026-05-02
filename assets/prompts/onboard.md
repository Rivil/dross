# /dross-onboard

Adopt dross into an **existing** repo. Lighter than `/dross-init`: no scaffolding, just signal scan → confirm → capture → verify.

## 0. Pre-flight

1. Run `dross rule show` and treat the output as MUST-FOLLOW.
2. Stop if `.dross/` already exists (suggest `dross onboard --force`).
3. Run `dross onboard`. It scans signal files (Dockerfile, package.json, lockfiles, tsconfig, go.mod, *.csproj, project.godot, .github/workflows) and writes a draft `project.toml`.

Read the printed "Detected" list and use it to inform the questions below.

## 1. Identity

Ask: name (default = directory name), one-line description, core value, audience, non-goals.

```
dross project set project.name        "<name>"
dross project set project.description "<description>"
dross project set goals.core_value    "<core value>"
```
Audience + non-goals: edit `project.toml` directly.

## 2. Stack confirmation

`dross project show` to display detected stack. Ask the user to confirm or correct:
- Languages
- Frameworks
- Package manager
- Test runner
- Type checker / linter / formatter

Set via `dross project set stack.*` for the simple fields. Locked choices go into `[[stack.locked]]` blocks (edit toml directly).

## 3. Runtime capture

For each command field below, show the detected default (read `package.json` scripts, `Makefile` targets, etc.) and ask the user to confirm the **exact** command they actually use:

- `dev_command`
- `test_command`
- `typecheck_command`
- `lint_command`
- `format_command`
- `build_command`
- `migrate_command`
- `seed_command`
- `shell_command`
- `logs_command`

If `runtime.mode = docker`, prefix correctly (e.g. `docker compose exec app pnpm test`). Persist via `dross project set runtime.<field> "<exact>"`.

Service URLs (app, db, redis, etc.) — edit `[runtime.services]` directly.

## 4. Remote / git host

`dross onboard` already detected `[remote]` from `git remote get-url origin` and overlaid `~/.claude/dross/defaults.toml`. Run `dross project show` and look at the `[remote]` block.

For each field, present the resolved value and ask "confirm or change?":
- **url** — should match the canonical clone URL.
- **provider** — github / forgejo / gitea / bitbucket / none. If empty (self-hosted unknown host), ask the user.
- **public** — can a cloud-side agent (no VPN, no SSH key) `git clone` it? Default no for self-hosted forges.
- **api_base** — REST base URL. github → `https://api.github.com`. Forgejo/Gitea → `https://<host>/api/v1`.
- **log_api** — does the instance expose CI logs via API?
- **auth_env** — env-var name (e.g. `FORGEJO_TOKEN`, `GITHUB_TOKEN`). **Never the token value.**
- **reviewers** — csv of human reviewer usernames `/dross-ship` should auto-assign. Empty = none.

Persist any changes with `dross project set remote.<field> "<value>"` (booleans = `true`/`false`; reviewers = csv).

If `~/.claude/dross/defaults.toml` doesn't exist yet and the user just confirmed values they'd want to reuse, suggest: *"Save these as defaults so the next project pre-fills them?"* — currently a manual edit; `dross defaults save` is on the roadmap.

## 5. Functionality verification

Run each captured command, report pass/fail in a table. Same rules as `/dross-init` step 8. Failing rows must be either fixed or explicitly waived before continuing.

## 6. Conventions

Ask:
- Main branch (default detected from `git symbolic-ref refs/remotes/origin/HEAD` if available)
- Branch naming pattern (e.g. `feature/*`, `<initials>/<topic>`)
- Commit convention (conventional / freeform)
- Squash or merge?
- Monorepo? If yes, list workspaces.

Edit `[repo]` block directly.

## 7. Rules intake

`dross rule list --scope global` — show inherited rules.

Then ask the user, free-form: **"What has Claude or another AI done in this repo that you want to never happen again?"** Capture every answer as a project rule:
```
dross rule add --scope project "<exact rule text>"
```
Tag obvious cross-project ones with `--scope global` instead.

## 8. Goals + non-goals + competition

Optional but valuable. If skipped, mark a TODO and move on.

## 9. Wrap

Run `dross validate`. Print summary:
```
Onboarded. Detected and verified:
  - <languages, frameworks, package manager, runtime mode>
  - <N> commands runtime-verified
  - <N> rules loaded (M global, K project)
Next: /dross-status to see where to focus first.
```
Update state: `dross state touch "onboard complete"`.
