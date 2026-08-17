# /dross-options

Full editor for every dross-managed setting. Designed to be run rarely (after milestone changes, when adopting new conventions, when something feels stale) and to take its time. **Save-per-option:** every change persists immediately via the relevant `dross X set` — stopping mid-way never loses prior edits.

**Run this as a conversation, not a broadcast.** Follow the shared interaction playbook (`_interaction.md`, printed by the `dross interaction show` pre-flight step below): a section-pick gate first, then walk each setting one decision per turn — never one mega-form over every setting.

## 0. Pre-flight

1. Run `dross rule show` and `dross interaction show`; treat the rules as MUST-FOLLOW and follow the printed interaction playbook for every turn of this command.
2. Confirm `.dross/` exists. If not, suggest `/dross-init` (greenfield) or `/dross-onboard` (existing repo) and stop.
3. Capture current state once, up front:
   - `dross project show` — every project.toml field
   - `dross defaults show` — global defaults
   - `dross rule list` — merged rules
   - `dross env list` — settings.json env keys (values masked)
4. Track `changed: []` and `skipped: []` lists in your head; use them in the wrap-up.

## Section pick

Open with a single **section-pick gate** before touching any setting: one `AskUserQuestion` (multiSelect) listing the sections below, so the user picks which to review this run and a one-setting change never forces all sixteen. Lead with the sections most likely stale (e.g. after a milestone change: stack, runtime, repo). Walk only the chosen sections; skip the rest without asking.

Sections: project identity · stack · runtime · repo conventions · remote · paths · env files · global defaults · rules · profile · settings env vars · issue board · mutation adapters · machine-local settings · hooks · constraints and competition.

This gate is **its own turn**, distinct from the per-setting Keep · Change · Skip turn in "How each section works" below — never collapse the two into one mega-form over every setting.

## How each section works

For each field in a chosen section: state the current value, ask `AskUserQuestion` with options **Keep · Change · Skip section**. On Change, gather the new value and immediately run the listed `dross X set ...` command. On any error from `dross X set`, surface it and offer Retry / Skip. On Skip section, move to the next section.

For booleans: Yes / No. For CSVs: ask for comma-separated input. For secrets: see §11 — never ask the user to paste tokens in chat.

If you see a field in `dross project show` that isn't covered below, **stop** and tell the user — that's a schema-vs-prompt drift bug to fix, not something to silently invent prompts for.

Some fields dross **records but does not yet act on** — the value is stored and shown, and no dross code path or prompt reads it. Say so when the user sets one, so they aren't told a value works when it only persists. The list is pinned by `inertKeyReasons` in `internal/cmd/inert_config_test.go`, which fails the build if a settable key goes unread without a stated reason, so it cannot quietly grow:

Recorded only: `runtime.dev_command`, `runtime.stop_command`, `runtime.test_watch`, `runtime.lint_command`, `runtime.migrate_command`, `runtime.seed_command`, `runtime.shell_command`, `runtime.logs_command` (no verb runs them yet), `stack.languages`, `stack.frameworks`, `stack.type_checker`, `stack.linter`, `stack.formatter`, `stack.test_runner` (dross detects the stack from the filesystem), `paths.migrations`, `paths.schemas`, `paths.public`, `repo.layout`, `repo.workspaces`, `repo.root_run_dir`, `remote.public`, `goals.audience`, `env.secrets_location`.

## 1. Project identity

Fields: `project.name`, `project.description`, `project.version`, `goals.audience`, `goals.core_value`, `goals.non_goals` (csv), `goals.differentiators` (csv).

Persist with `dross project set <field> "<value>"`.

## 2. Stack

Fields: `stack.languages` (csv), `stack.frameworks` (csv), `stack.package_manager`, `stack.type_checker`, `stack.linter`, `stack.formatter`, `stack.test_runner`, `stack.e2e_runner`, `stack.profile`.

`stack.profile` is the matched stack-profile id and is normally set by `dross stack` rather than by hand — show it, and if it looks wrong point at `dross stack detect` to re-derive it instead of typing an id. `stack.package_manager` is load-bearing beyond convenience: a remote mutation run refuses rather than guessing when it is unset (see §14).

`[[stack.locked]]` blocks live in `project.toml` as TOML arrays — surface them read-only here; if the user wants to add/remove a locked choice, point at editing `project.toml` directly (CLI doesn't model array-of-tables yet).

## 3. Runtime

Fields: `runtime.mode`, `runtime.dev_command`, `runtime.stop_command`, `runtime.test_command`, `runtime.test_watch`, `runtime.e2e_command`, `runtime.typecheck_command`, `runtime.lint_command`, `runtime.format_command`, `runtime.build_command`, `runtime.migrate_command`, `runtime.seed_command`, `runtime.shell_command`, `runtime.logs_command`.

For each command, show what's currently set. Empty = "not configured". Encourage **exact** strings, including docker prefixes.

`[runtime.services]` is a TOML map — surface read-only; for changes, edit `project.toml` directly.

## 4. Repo conventions

Fields: `repo.layout`, `repo.root_run_dir`, `repo.workspaces` (csv), `repo.git_main_branch`, `repo.branch_pattern`, `repo.commit_convention`, `repo.squash_merge` (bool).

## 5. Remote / git host

Fields: `remote.url`, `remote.provider`, `remote.public` (bool), `remote.api_base`, `remote.log_api` (bool), `remote.auth_env`, `remote.auth_user`, `remote.auth_scheme`, `remote.project_id`, `remote.reviewers` (csv).

Three of those are provider-conditional — surface them only when the provider calls for them, and say why:

- `remote.auth_scheme` — `private-token` (default) · `bearer` · `basic`. Only `basic` needs `remote.auth_user`.
- `remote.auth_user` — the account **user/email** for HTTP Basic (bitbucket, and jira on the board side). It is a username, never a token; the token stays in the env var named by `auth_env`.
- `remote.project_id` — gitlab only: numeric project-id override when it can't be derived from the URL.

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

## 12. Issue board

Fields: `board.provider` (forgejo · gitea · gitlab · youtrack · jira · github), `board.base_url`, `board.auth_env`, `board.auth_user`, `board.project`, `board.github_project`, `board.enabled` (bool), `board.milestone_mode` (version · agile · epic).

`board.auth_user` is jira's account **email** for HTTP Basic — a username, never a token. `board.github_project` is a GitHub Projects v2 board node id. `board.project` is the tracker's project short-name/key, except on github where it is `owner/repo`.

`[board.state_map]` (dross lifecycle state → tracker state value) and `[board.fields]` (`state`, `type`, `fix_versions` field-name overrides) are TOML maps — surface read-only; for changes, edit `project.toml` directly.

`board.enabled` has its own verbs — `dross issue enable` / `dross issue disable` — so use those rather than writing the field. If it is on, offer to run `dross issue pull` as a read-only liveness check: it exercises the base URL, the token in `board.auth_env` and the project key in one call. A board that has quietly stopped authenticating otherwise shows up only as a zero in the `/dross-status` inbox line.

## 13. Mutation adapters

Fields: `mutation.adapters` (csv), `mutation.gremlins.timeout_coefficient`, `mutation.stryker.workdir`.

`[mutation]` is **not settable through `dross project set`** — it has no dotted-path writer. Show the current values and, for a change, edit `.dross/project.toml` directly, then re-run `dross validate`.

`mutation.adapters` is the allowlist of adapters this repo runs. Leaving it empty means *all* adapters are eligible, which is usually wrong in one specific way: `dross doctor` then probes every toolchain, so a Go-only repo fails readiness over a missing `npx` and `dotnet`. Set it to the adapters the repo's languages actually need.

## 14. Machine-local settings (.dross/local.toml)

`.dross/local.toml` is **gitignored**: none of it appears in `dross project show`, and none of it travels with the repo — a fresh clone carries none of these values. That is what makes this section worth walking. Every setting here was written by a one-shot command nobody remembers running, and nothing else surfaces them.

Read any key with `dross local get <key>` (empty output = unset).

**Plain settable keys** — `dross local set <key> "<value>"`:

- `quick_base` — the branch `/dross-quick` forks off on this machine. Machine-local because it tracks whatever you happen to be working from, not a property of the repo.
- `allow_hosts` — csv of extra hosts this machine authorizes dross to reach. It lives here rather than in `project.toml` for a security reason worth repeating to the user: a committed allowlist would be self-authorizing, so a tracked `local.toml` is refused *unread* rather than merged.

**Remote mutation.** Mutation testing can run on another machine: `dross verify` rsyncs this working tree to a host and runs the adapters there. A host named in the tracked `project.toml` is refused by name — only this untracked file can authorize one.

**Current state** — run `dross mutation remote status` and show what it prints (`not granted`, the granted `host:workdir`, or a refusal if `local.toml` is tracked).

**The grant.** The two keys behind it are `mutation_remote_host` and `mutation_remote_workdir` — name them, so a user who opens `local.toml` and finds them knows which verb owns them. `dross local set` refuses both by design, so this section does **not** write them; it invokes the verb that does:

- **Grant / change** → `dross mutation remote grant <host> <workdir>`
- **Revoke** → `dross mutation remote revoke`
- **Keep**

The verb prints the host and workdir it is about to authorize before writing them. Read that line back to the user rather than summarizing it: a grant is code execution on that machine, as them, and a grant they did not read is a rubber stamp.

After any grant change, run `dross doctor` and surface its `Remote mutation:` line — it reports whether the host is reachable and whether the configured adapters' toolchains are installed there. Learning that here costs a second; learning it mid-verify costs a pushed tree and an empty report.

**Tuning knobs** — these *are* settable, via `dross local set <key> "<value>"`:

- `mutation_workers` — how many mutants run at once. **Unset is not zero**; it means the adapter picks its own default (half the local cores, or half the *remote's* cores under a grant). Only set a number if a run has actually misbehaved — a hardcoded value sizes a 32-core host's run by this laptop.
- `mutation_test_cpu` — CPUs each mutant's test run may use. Unset = 1. Raising it multiplies against `mutation_workers`; over-subscribing the box produces spurious timeouts that read as surviving mutants.
- `mutation_remote_env` — csv of variable **NAMES** a remote run needs (e.g. `DATABASE_URL,NODE_ENV`). Names only, never `NAME=value`: dross reads each value from its own environment at run time and stores none of them. A name listed here that is unset locally is an error, not an empty export. Ask for names the way you would any CSV — they are not secrets, so this one is safe to type in chat, unlike §11.

**Trusted test command** — the same consent shape as the grant, a different verb, and the one most likely to be silently stale. `trusted_test_command` records a hash of the `runtime.test_command` you consented to; change that command in `project.toml` and consent lapses, because you never agreed to the new one.

Run `dross trust --check`. Exit 0 means trusted — say so and move on. Non-zero means missing or stale: show the user the **exact** `runtime.test_command` line from `project.toml` and tell them to run `dross trust` themselves. Never run it on their behalf — the gate exists so a human reads the line the repo supplied, and an agent granting it defeats the whole mechanism.

## 15. Claude Code hooks

`dross hooks ensure` idempotently wires the dross-owned `PreCompact` + `SessionStart` hooks into user-level `settings.json`. The `SessionStart` hook is what prints the "you are here / next command" re-entry line at the top of a fresh session.

Offer to run it. It is safe to re-run — that is what idempotent means here — so the honest default when the user is unsure is to run it rather than to inspect `settings.json` by hand. If the re-entry line has stopped appearing at session start, this is the fix.

## 16. Constraints and competition (read-only)

`[constraints]` (a string map) and `[[competition]]` (an array of tables) are captured by `/dross-init` and `/dross-onboard` and have no dotted-path writer. Show them if non-empty so the user can see what the project is carrying; for changes, point at editing `.dross/project.toml` directly.

Do not invent these if absent — an empty `[constraints]` is the normal state for most repos.

## 17. Wrap

1. `dross validate` — surface any schema problems and offer to fix interactively.
2. `dross doctor` — surface any drift between `[remote]` and reality.
3. Print a compact one-line-per-category summary — never paste the full `project.toml` back:
   ```
   Reviewed 5 of 16 sections.
   Changed: project.description, runtime.test_command, remote.auth_env (3)
   Skipped: profile, paths (2)
   No changes: 7
   ```
4. `dross state touch "options reviewed"`.
5. End with a bottom-anchored `Next:` line:
   ```
   Options reviewed. Next: /dross-status — confirm where you are, then back to the spec→plan→execute loop.
   ```
