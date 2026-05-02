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

## 4. Functionality verification

Run each captured command, report pass/fail in a table. Same rules as `/dross-init` step 7. Failing rows must be either fixed or explicitly waived before continuing.

## 5. Conventions

Ask:
- Main branch (default detected from `git symbolic-ref refs/remotes/origin/HEAD` if available)
- Branch naming pattern (e.g. `feature/*`, `<initials>/<topic>`)
- Commit convention (conventional / freeform)
- Squash or merge?
- Monorepo? If yes, list workspaces.

Edit `[repo]` block directly.

## 6. Rules intake

`dross rule list --scope global` — show inherited rules.

Then ask the user, free-form: **"What has Claude or another AI done in this repo that you want to never happen again?"** Capture every answer as a project rule:
```
dross rule add --scope project "<exact rule text>"
```
Tag obvious cross-project ones with `--scope global` instead.

## 7. Goals + non-goals + competition

Optional but valuable. If skipped, mark a TODO and move on.

## 8. Wrap

Run `dross validate`. Print summary:
```
Onboarded. Detected and verified:
  - <languages, frameworks, package manager, runtime mode>
  - <N> commands runtime-verified
  - <N> rules loaded (M global, K project)
Next: /dross-status to see where to focus first.
```
Update state: `dross state touch "onboard complete"`.
