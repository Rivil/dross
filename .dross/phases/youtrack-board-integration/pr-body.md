# youtrack-board-integration — YouTrack board integration

Decouples issue-board sync from the code host: a new `[board]` config block drives
all board operations independently of `[remote]`, so a repo can ship code to one
host (e.g. GitHub) and track issues on another (YouTrack). Adds a full YouTrack
REST backend behind a `BoardClient` interface, string readable-id links in
board.json, lifecycle→State mapping, and milestone + backlog sync across all three
milestone modes.

## Acceptance criteria

| ID | Criterion | Status | Tests |
|---|---|---|---|
| `c-1` | `[board]` is the single source for board sync (provider/base_url/auth_env/project/enabled/milestone_mode/state_map); ops resolve from it, not `[remote]`; `doctor` validates it. | covered | `TestProjectBoardRoundTrip`, `TestBoardDottedArmsRoundTrip`, `TestOpenBoardResolvesFromBoardBlock`, `TestDoctorValidatesBoardBlock` |
| `c-2` | YouTrack REST backend (CRUD scoped to the project, bearer permanent-token); factory returns a working client for `provider=youtrack`. | covered | `TestNewAcceptsYouTrack`, `TestYouTrack{Create,List,Get,Update}Issue`, `TestYouTrackBearerAuth` |
| `c-3` | `issue pull --json` returns open YouTrack issues not linked/dismissed, with the label filter passed through; feeds `/dross-inbox`. | covered | `TestIssuePullYouTrackFiltersLinkedAndDismissed`, `TestInboxPromptReadsBoardEnabled` |
| `c-4` | `phase-sync` creates/updates a YouTrack issue (criteria + checklist) linked by readable id; `milestone-sync` ensures the entity per `milestone_mode`. | covered | `TestIssuePhaseSyncYouTrackCreatesThenUpdates`, `TestIssueMilestoneSyncYouTrack`, `TestYouTrackMilestone{Version,Agile,Epic}Mode` |
| `c-5` | `phase-sync --status` maps lifecycle→State via the default map + `[board].state_map`; an unmapped state warns and skips without failing the sync. | covered | `TestYouTrackSetStateMapsAndUpdates`, `TestYouTrackSetStateUnmappedWarnsSkips` |
| `c-6` | `issue backlog-sync` mirrors unscaffolded slugs + unrouted someday ideas as Open backlog items attached to the entity per mode, idempotent. | covered | `TestIssueBacklogSyncYouTrack{Idempotent,EpicMode,AgileMode}` |

## Decisions locked

- **board_config_source** 🔒 — board sync is driven solely by `[board]`; no `[remote]` fallback. `[board].provider` accepts forgejo \| gitea \| gitlab \| youtrack.
- **milestone_mode** 🔒 — a dross milestone maps to YouTrack per `[board].milestone_mode`: `version` (Version bundle value, default), `agile` (existing Agile board), or `epic` (Epic issue, phases/backlog as subtasks). All three implemented.
- **phase_state_mapping** 🔒 — built-in lifecycle→State default map (planned→Open … complete→Verified), overridable via `[board].state_map`; a missing target State warns and skips.
- **board_auth** 🔒 — `Authorization: Bearer <token>` from `board.auth_env` (never the value in config).
- **issue_identity** 🔒 — board.json links by the human-readable issue id (e.g. `PROJ-123`), not the internal database id.

## Efficacy

- Criteria coverage: **6/6** covered, 0 weak, 0 uncovered.
- Mutation: **measured 0.86** (1031 killed / 174 survived). Every survivor is a *not-covered* mutant; **efficacy on covered mutants = 1.00**. ~90% of survivors are in files this phase never touched (module-wide gremlins scan, not regressions); phase-touched survivors are secondary paths (HTTP error branches, `buildQuery` closed/tag clauses, the YouTrack `CloseIssue`/`EnsureMilestone` stubs, and `LinkSubtask` — exercised by the cmd integration test but not a forge-package unit test). No core-criterion behavior is untested. Full mutant list in `.dross/phases/youtrack-board-integration/verify.toml`.
- Verdict: **PASS**.

## Notes

- YouTrack REST shapes were verified against the official API docs but are **not yet exercised against a live instance** (dross's own board sync is off; all tests use httptest mocks).
