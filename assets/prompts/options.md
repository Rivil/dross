# /dross-options

Full editor for every dross-managed setting. Designed to be run rarely (after milestone changes, when adopting new conventions, when something feels stale) and to take its time. **Save-per-option:** every change persists immediately via the relevant `dross X set` — stopping mid-way never loses prior edits.

## 0. Pre-flight

1. `dross rule show` — treat as MUST-FOLLOW.
2. Confirm `.dross/` exists. If not, suggest `/dross-init` (greenfield) or `/dross-onboard` (existing repo) and stop.
3. Capture current state once, up front:
   - `dross project show` — every project.toml field
   - `dross defaults show` — global defaults
   - `dross rule list` — merged rules
   - `dross env list` — settings.json env keys (values masked)
4. Track `changed: []` and `skipped: []` lists in your head; use them in the wrap-up.

## How each section works

For each field below: state the current value, ask `AskUserQuestion` with options **Keep · Change · Skip section**. On Change, gather the new value and immediately run the listed `dross X set ...` command. On any error from `dross X set`, surface it and offer Retry / Skip. On Skip section, move to the next section.

For booleans: Yes / No. For CSVs: ask for comma-separated input. For secrets: see §12 — never ask the user to paste tokens in chat.

If you see a field in `dross project show` that isn't covered below, **stop** and tell the user — that's a schema-vs-prompt drift bug to fix, not something to silently invent prompts for.

## 1. Project identity

Fields: `project.name`, `project.description`, `project.version`, `goals.audience`, `goals.core_value`, `goals.non_goals` (csv), `goals.differentiators` (csv).

Persist with `dross project set <field> "<value>"`.

## 2. Stack

Fields: `stack.languages` (csv), `stack.frameworks` (csv), `stack.package_manager`, `stack.type_checker`, `stack.linter`, `stack.formatter`, `stack.test_runner`, `stack.e2e_runner`.

`[[stack.locked]]` blocks live in `project.toml` as TOML arrays — surface them read-only here; if the user wants to add/remove a locked choice, point at editing `project.toml` directly (CLI doesn't model array-of-tables yet).

## 3. Runtime

Fields: `runtime.mode`, `runtime.dev_command`, `runtime.stop_command`, `runtime.test_command`, `runtime.test_watch`, `runtime.e2e_command`, `runtime.typecheck_command`, `runtime.lint_command`, `runtime.format_command`, `runtime.build_command`, `runtime.migrate_command`, `runtime.seed_command`, `runtime.shell_command`, `runtime.logs_command`.

For each command, show what's currently set. Empty = "not configured". Encourage **exact** strings, including docker prefixes.

`[runtime.services]` is a TOML map — surface read-only; for changes, edit `project.toml` directly.

## 4. Repo conventions

Fields: `repo.layout`, `repo.root_run_dir`, `repo.workspaces` (csv), `repo.git_main_branch`, `repo.branch_pattern`, `repo.commit_convention`, `repo.squash_merge` (bool).

## 5. Remote / git host

Fields: `remote.url`, `remote.provider`, `remote.public` (bool), `remote.api_base`, `remote.log_api` (bool), `remote.auth_env`, `remote.reviewers` (csv).

Cross-check: if `remote.url` is set, run `git remote get-url origin` and warn if it differs.

If the user has updated `provider`, suggest `dross defaults save` at the end of this section so future projects pre-fill from these values.

## 6. Paths

Fields: `paths.source`, `paths.tests`, `paths.e2e`, `paths.migrations`, `paths.schemas`, `paths.i18n`, `paths.public`.

Verify each non-empty path exists with `ls -d` and warn (don't block) on missing dirs.

## 7. Env files

Fields: `env.files` (csv, ordered), `env.secrets_location`, `env.gitignored` (bool).

For each file in `env.files`, check it exists and is in `.gitignore` if `env.gitignored = true`.

## 8. Global defaults (~/.claude/dross/defaults.toml)

Show output of `dross defaults show`. Ask whether to:
- **Save current project's [remote] as defaults** → `dross defaults save`
- **Skip** — leave defaults untouched

## 9. Rules

Show `dross rule list --scope project` and `dross rule list --scope global`. Ask:
- Add a project rule? → `dross rule add --scope project "<text>"`
- Add a global rule? → `dross rule add --scope global "<text>"`
- Disable / re-enable an existing rule? → `dross rule disable <id>` / `enable <id>`
- Promote a project rule to global? → `dross rule promote <id>`
- Remove a rule? → `dross rule remove <id>` (confirm twice)

## 10. Profile

`dross profile show` to display merged profile. If the user wants edits, edit `.dross/profile.toml` directly — profile.toml is hand-curated free-form TOML (no dotted-path CLI yet).

## 11. Settings env vars (~/.claude/settings.json)

`dross env list` already ran in pre-flight. For each key the project might use (the project's `remote.auth_env` first, then any common ones like `GITHUB_TOKEN`, `FORGEJO_TOKEN`):

- If **set** (`set (length N)`): ask "Update?" Yes/No. If yes: tell the user *"Run `dross env set <KEY>` in your own shell — that prompts for the new value with input hidden. Don't paste tokens here."* Wait for confirmation before continuing.
- If **NOT SET** (and the project depends on it): same — point at `dross env set <KEY>`.

**NEVER** ask for, accept, or repeat a token value in this conversation. Tokens leave the user's keyboard, go directly to settings.json via `dross env set`, and never enter Claude Code's context.

If the user asks to add an entirely new env var dross doesn't currently know about, that's still fine — `dross env set FOO` works for any KEY.

## 12. Wrap

1. `dross validate` — surface any schema problems and offer to fix interactively.
2. `dross doctor` — surface any drift between `[remote]` and reality.
3. Print compact summary:
   ```
   Reviewed 12 sections.
   Changed: project.description, runtime.test_command, remote.auth_env (3)
   Skipped: profile, paths (2)
   No changes: 7
   ```
4. `dross state touch "options reviewed"`.
