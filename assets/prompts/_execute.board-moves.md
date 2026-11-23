`pull:` reported cards moved on the board that `plan.toml` doesn't reflect yet.
Nothing was written: applying rewrites task statuses, changing what `dross task
next` returns, so it is the user's call.

- **Moves reported** → surface them in one line each (task id, plan status → board
  status) and ask whether to apply. On acceptance:
  ```
  dross issue task pull <phase> --apply
  ```
- **A board start refused** → `pull:` says a card moved to in_progress has
  preconditions. Nothing writes it — `--apply` exits non-zero on it after
  applying the rest. Tell the user; the task starts only when §1a picks it,
  through `dross task status <phase> <task-id> in_progress`, the call that runs
  its preconditions. Never set its status by hand in `plan.toml`.
- **A task changed on both sides** → `pull:` marks it CONFLICT and prints both
  values; `--apply` refuses it by name. dross does not pick a winner and neither do
  you: surface the conflict and let the user say which side is right.
- **`--apply` refused for the provider** → boards that are open/closed only
  (Forgejo, Gitea, GitLab) carry no workflow state to read, and the command says
  so by name. A configuration fact, not an error — note it once and move on.
