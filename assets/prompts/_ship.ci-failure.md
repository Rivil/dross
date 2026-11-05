**On failure:**
1. Pull failing logs:
   - GitHub: `gh run view --log-failed` for the run linked from the PR.
   - Forgejo: log URL is in the commit status payload (`target_url`); `WebFetch` it.
   - GitLab: the failed job's `web_url` is in the pipeline's jobs (`GET <api_base>/projects/<id>/pipelines/<pipeline-id>/jobs`); `WebFetch` the trace or surface the job URL.
2. Diagnose. Edit + commit the fix on `phase/<id>`, one commit per logical fix following `repo.commit_convention`.
3. `git push origin phase/<id>` — appends to the open PR, so CI re-runs on the fix.
4. Then re-run **`dross ship <phase-id>`** — required after every fix, not optional: it recognises the open PR, pushes anything still pending on `phase/<id>`, reports the existing PR by number (the URL is the one from the first run or the provider page; `changes.json` stores no url), and re-checks the verdict's freshness. A fix to any file outside `.dross/` and `ARCHITECTURE.md` makes the pass stale and ship refuses, naming the changed files: run `/dross-verify <phase-id>`, commit its record, re-run `dross ship <phase-id>`, then continue. If ship refuses a diverged branch, `dross ship --force` is the path (it force-with-leases); if it refuses a behind-only one, `git pull --rebase origin phase/<id>` first. If you rebase or amend by hand, use `git push --force-with-lease`.
5. Loop back to "Watch checks". Cap at 3 fix iterations — if checks still fail after 3 cycles, stop and hand back to the user.
