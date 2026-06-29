# MVP lens — youtrack-board-integration

Phase youtrack-board-integration — 7 tasks across 3 waves

Wave 1
  t-1  Add [board] config block + doctor validation
       files:    internal/project/project.go
                 internal/cmd/project.go
                 internal/cmd/doctor.go
       covers:   c-1
       contract: If the [board] struct/toml tags or a readDotted/writeDotted arm is missing,
                 a `dross project set/get board.provider` + `board.milestone_mode` round-trip
                 test fails. If doctor skips [board], a doctor test with board.enabled=true +
                 unset auth_env (and one with an unrecognised provider) reports 0 issues and fails.

  t-2  Store readable string issue ids in board.json
       files:    internal/board/board.go
                 internal/board/board_test.go
       covers:   c-4
       contract: If phase/quick links keep an int db-id field instead of a readable string id,
                 a SetPhase("p","PROJ-123")→PhaseIssue round-trip test won't compile/round-trip.
                 If IsLinked/IsDismissed no longer match a string readable id, the pull-dedupe
                 test (issue already linked is skipped) fails.

  t-3  YouTrack issue CRUD backend + provider factory
       files:    internal/forge/youtrack.go
                 internal/forge/forge.go
                 internal/forge/youtrack_test.go
       covers:   c-2, c-3
       contract: Against an httptest mock — if NewBoard returns ErrNotImplemented/error for
                 provider=youtrack, or Create/Get/List don't hit /api/issues(/<id>), omit the
                 `Authorization: Bearer` header, ignore board.auth_env, or fail to scope List by
                 the configured project query, TestYouTrackCRUD fails. If the returned Issue.ID
                 isn't the readable PROJ-123 id, the id-mapping assertion fails.

Wave 2 (depends t-3)
  t-4  YouTrack milestone entity per milestone_mode
       files:    internal/forge/youtrack.go
                 internal/forge/youtrack_test.go
       covers:   c-4
       contract: Against an httptest mock — if mode=version doesn't ensure a version bundle via
                 /api/admin/projects/<id>, mode=agile doesn't target /api/agiles, or mode=epic
                 doesn't create-or-reuse an Epic issue phases attach to, the per-mode
                 TestYouTrackMilestone_{version,agile,epic} fails. Re-running must not duplicate.
       depends_on: t-3

  t-5  YouTrack state map (default + override + warn-skip)
       files:    internal/forge/youtrack.go
                 internal/forge/youtrack_test.go
       covers:   c-5
       contract: If a mapped phase state (default map or [board].state_map override) doesn't PATCH
                 the issue's State custom field, or an unmapped/non-existent target state fails the
                 sync instead of warning and leaving the rest of the issue update applied,
                 TestYouTrackStateMap fails.
       depends_on: t-3

  t-6  YouTrack backlog ensure primitive (idempotent)
       files:    internal/forge/youtrack.go
                 internal/forge/youtrack_test.go
       covers:   c-6
       contract: Against an httptest mock — if re-ensuring the same backlog item (by slug/idea key)
                 creates a duplicate instead of updating, or created items aren't put in the
                 backlog/Open state and attached to the milestone-mode entity,
                 TestYouTrackBacklogIdempotent fails.
       depends_on: t-3

Wave 3 (depends t-1, t-2, t-4, t-5, t-6)
  t-7  Re-route board ops to [board]; wire phase/backlog sync + inbox gate
       files:    internal/cmd/issue.go
                 assets/prompts/inbox.md
                 internal/cmd/inbox_prompt_test.go
       covers:   c-1, c-3, c-4, c-5, c-6
       contract: If openBoard builds its client from [remote] not [board] (a youtrack [board] +
                 github [remote] must yield a youtrack client, not ErrNotImplemented), the routing
                 test fails. If phase-sync doesn't persist the returned readable id in board.json
                 or doesn't pass --status through t-5's state map, the phase-sync wiring test fails.
                 If pull stops surfacing unlinked/undismissed YouTrack issues or drops the --labels
                 filter, the pull test fails. If backlog-sync doesn't gather unscaffolded
                 milestone.phases slugs + unrouted someday deferred, c-6 wiring test fails.
                 If inbox.md still gates on remote.board_sync, a grep test asserting `board.enabled`
                 in inbox.md fails (r-01: run `make install` after the prompt edit).
       depends_on: t-1, t-2, t-4, t-5, t-6

## Coverage
- c-1 → t-1 (config block + doctor validation), t-7 (ops resolve client from [board], enable/migration guidance)
- c-2 → t-3 (YouTrack CRUD backend, factory returns working client for provider=youtrack)
- c-3 → t-3 (ListIssues over /api/issues), t-7 (pull routing + label filter + link/dismiss dedupe)
- c-4 → t-2 (readable-id links in board.json), t-4 (milestone entity per mode), t-7 (phase-sync create/update + link)
- c-5 → t-5 (state map + warn-skip + State custom field), t-7 (phase-sync --status wiring)
- c-6 → t-6 (idempotent backlog primitive), t-7 (gather unscaffolded slugs + unrouted someday, drive it)

## Judgment calls
- forge-extend vs new-client vs interface: chose a **sibling concrete `youtrack` type in package forge behind a small `BoardClient` interface** (the method set issue.go already calls), with a `forge.NewBoard(cfg)` factory dispatching forgejo/gitea/gitlab→existing *Client and youtrack→youtrack client. Rejected branching YouTrack inside the existing provider-switched *Client — YouTrack's query language, custom-field State, readable-id, three milestone modes and `/api/...` paths would bloat every forge method's switch. Rejected a separate top-level package — would duplicate the Issue/IssueInput/IssuePatch/IssueFilter types and the httptest test harness; same-package reuse is the minimal move that still respects the divergent API. The interface is required (not speculative): issue.go's `ctx.client` must now be one of two concrete types.
- 3 milestone_modes → tasks: all three (version/agile/epic) collapse into **one task (t-4)** — same surface (ensure-milestone-entity), same httptest harness, one method branching on `board.milestone_mode` from Config; each mode gets its own assertion in the contract rather than its own task (a per-mode task would be a sub-10-min split).
- board.json string ids (t-2) split from config (t-1): the locked readable-id identity forces a link-value type change in a different package (internal/board) with its own round-trip test; merging into the config task would cross three packages for one commit. Forge backends keep working by storing their int number as its string form.
- Single cmd integration task (t-7) at wave 3: all issue.go rewiring (openBoard re-route, enable guidance, phase-sync, backlog gather, inbox gate) lands in one task so the file is touched once; the heavy logic is unit-tested in the forge layer (t-3..t-6), leaving t-7 as the wiring seam. The inbox.md prompt one-liner is folded in (too small to stand alone) with its own grep contract per r-01 (existing internal/cmd/inbox_prompt_test.go is the precedent).
- Deferred github/jira providers: not in scope — no task.
