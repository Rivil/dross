# Board sync — Jira e2e proof (2026-07-25)

Live instance: fluffyleopardlabs.atlassian.net · project MYJ (my-journal)
Auth: HTTP Basic, admin@fluffyleopardlabs.com + API token (ATATT…, env JIRA_TOKEN)

## Round-trip (all against live Jira Cloud)

1. `dross issue milestone-sync v1.0`  → `milestone v1.0 -> board 10000` (version created/found)
2. `dross issue phase-sync prove-or-demote-board-sync` → created real issue **MYJ-13**
   - confirmed via API: summary "prove-or-demote-board-sync — prove-or-demote-board-sync",
     status "To Do", labels ['dross', 'dross/status:planning'], linked to version 10000
3. `dross issue pull` → read back 8 unlinked MYJ issues, correctly EXCLUDING the linked MYJ-13

The JiraClient create + version-map + list/read paths are all exercised against a
real instance. Board sync is PROVEN against Jira, not merely wired.

## Bugs this e2e surfaced and fixed (the never-run-e2e gaps)

- **board.auth_user unsettable** — `NewJira` requires auth_user but `dross project set`
  omitted the arm, so the Jira backend was unusable via its own CLI. Fixed both
  get/set arms + round-trip test case (the existing test omitted auth_user, which
  is why it shipped).
- **List used the removed /rest/api/3/search endpoint** — Jira Cloud returns HTTP 410
  (CHANGE-2046). Migrated to /rest/api/3/search/jql (same params + `issues` envelope);
  the test had pinned the removed path, which is why the bug shipped.

## Cleanup (proof_then_revert)

- MYJ-13 deleted, milestone version 10000 deleted — MYJ returned to prior state.
- Board sync disabled; [board] jira config removed from project.toml so the committed
  repo carries no live board dependency.
- API token used inline for the run only; never committed, stashed in the session
  scratchpad and deleted at wrap.
