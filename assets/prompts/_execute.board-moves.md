`pull:` reported cards moved on the board that `plan.toml` doesn't reflect yet.
Nothing was written: applying rewrites task statuses, changing what `dross task
next` returns, so it is the user's call.

- **Moves reported** → surface them in one line each (task id, plan status → board
  status) and ask whether to apply. On acceptance:
  ```
  dross issue task pull <phase> --apply
  ```
- **A task changed on both sides** → `pull:` marks it CONFLICT and prints both
  values; `--apply` refuses it by name. dross does not pick a winner and neither do
  you: surface the conflict and let the user say which side is right.
- **`--apply` refused for the provider** → boards that are open/closed only
  (Forgejo, Gitea, GitLab) carry no workflow state to read, and the command says
  so by name. A configuration fact, not an error — note it once and move on.
