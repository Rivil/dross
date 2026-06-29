# Risk-lens plan — youtrack-board-integration

Failure modes drive the graph. Each risk below is owned and tested by exactly one task.
The chain of failure: a bad/absent `[board]` block → a silent fall-back to `[remote]`
(forbidden) → a wrong-shaped YouTrack request → a State custom-field write against a
column that doesn't exist → a mode (esp. `agile`) that assumes a board that isn't there →
a re-sync that duplicates instead of updates. The waves below pin each of those.

Phase youtrack-board-integration — 9 tasks across 4 waves

Wave 1
  t-1  Add [board] config block + dotted arms
       files:    internal/project/project.go, internal/cmd/project.go, internal/cmd/project_test.go
       covers:   c-1
       contract: if the [board] block fails to decode its fields (provider, base_url,
                 auth_env, project, enabled, milestone_mode, state_map) or
                 `project set board.provider youtrack` / `board.enabled true` doesn't
                 round-trip through writeDotted→Save→Load, the project_test decode +
                 dotted-arm round-trip test fails.

  t-2  YouTrack REST backend client + httptest tests
       files:    internal/youtrack/youtrack.go (new), internal/youtrack/youtrack_test.go (new)
       covers:   c-2
       contract: against an httptest mock — if the create POST /api/issues body shape
                 ({project:{id},summary,description}) regresses, or the readable-id parse
                 (idReadable → "PROJ-123") falls back to the internal db id, or list drops
                 the `query=project:<short>` scoping, or auth isn't `Authorization: Bearer
                 <token-from-auth_env>`, the matching youtrack_test case fails.

Wave 2 (depends t-1, t-2)
  t-3  Validate [board] in dross doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       depends:  t-1
       covers:   c-1
       contract: if doctor stops flagging an unset $board.auth_env, an unrecognised
                 board.provider (not forgejo|gitea|gitlab|youtrack), a malformed base_url,
                 or an invalid milestone_mode, the doctor_test that feeds a broken [board]
                 and asserts the issue count fails.

  t-4  Move inbox board-sync gate to [board].enabled
       files:    assets/prompts/inbox.md
       depends:  t-1
       covers:   c-3
       contract: prompt-presence (grep) test — inbox_prompt_test.go asserts inbox.md gates
                 on `board.enabled` (and `dross issue enable`/[board] migration guidance)
                 and no longer references `remote.board_sync`; fails if the old gate text
                 remains. (r-01: edit assets/ source, then `make install`.)

  t-5  Backend interface + factory + board-id migration + [board] re-route
       files:    internal/issuetracker/issuetracker.go (new), internal/issuetracker/issuetracker_test.go (new), internal/board/board.go, internal/cmd/issue.go
       depends:  t-1, t-2
       covers:   c-1, c-2
       contract: (a) the-[board]-not-[remote] resolution — issue_test with [board].enabled
                 =false but a fully-populated [remote] (provider/url/auth) asserts every
                 board op is a no-op; flip to a github-[remote]+youtrack-[board] and assert
                 the client is built from [board]; fails on any [remote] fall-back.
                 (b) factory test: New(board.provider=youtrack) returns a working Backend,
                 NOT ErrNotImplemented; forgejo/gitea/gitlab still resolve to the forge
                 adapter. (c) board.json identity: SetPhase/PhaseIssue (+Quicks, Dismissed,
                 IsLinked) round-trip a readable string id like "PROJ-123"; board_test
                 fails if a link is stored/compared as an int issue number.

Wave 3 (depends t-5)
  t-6  YouTrack phase-sync + milestone-sync (all 3 milestone_modes)
       files:    internal/youtrack/youtrack.go, internal/cmd/issue.go, internal/youtrack/youtrack_test.go
       depends:  t-5
       covers:   c-4
       contract: four cases, one per failure mode —
                 (version) milestone-sync creates/finds the version bundle and tags the
                 phase issue with it; test fails if the bundle isn't reused on re-run.
                 (agile) milestone-sync looks up the pre-existing board via GET /api/agiles
                 and, when the named board is absent, WARNS-and-skips the sprint/column
                 attach instead of erroring; test fails if a missing board aborts the sync.
                 (epic) the phase issue is linked as a subtask of the Epic issue; test fails
                 if no parent/subtask link is posted.
                 (phase body+link) phase-sync renders title + acceptance criteria + task
                 checklist from plan.toml and stores the returned readable id in board.json;
                 test fails if the checklist is empty or the wrong key is linked.

  t-7  YouTrack inbound pull feed for triage
       files:    internal/cmd/issue.go, internal/youtrack/youtrack.go, internal/cmd/issue_test.go
       depends:  t-5
       covers:   c-3
       contract: `dross issue pull --json` over a youtrack [board] returns only open issues
                 that are neither linked (board.json) nor dismissed, with the --labels/tag
                 filter passed through as a YouTrack `query` clause; issue_test fails if a
                 linked/dismissed issue leaks into the feed or the label filter is dropped.

Wave 4 (depends t-6)
  t-8  phase-sync --status: State custom-field via state_map (warn-and-skip)
       files:    internal/youtrack/youtrack.go, internal/cmd/issue.go, internal/youtrack/youtrack_test.go
       depends:  t-6
       covers:   c-5
       contract: the State custom-field write — maps planned→Open … complete→Verified with
                 [board].state_map overriding; when the resolved target state is absent from
                 the project workflow, the sync WARNS and skips the State update while still
                 persisting the rest of the issue patch (no error). youtrack_test fails if an
                 unmapped/non-existent state errors the whole phase-sync or silently no-ops
                 the warning.

  t-9  Milestone backlog sync (roadmap slugs + someday ideas), idempotent
       files:    internal/cmd/issue.go, internal/board/board.go, internal/cmd/issue_test.go
       depends:  t-6
       covers:   c-6
       contract: syncs milestone.Phases entries with no phase directory + unrouted `someday`
                 deferred ideas into backlog items (Open/backlog state) attached to the
                 milestone entity per milestone_mode; re-running updates the SAME items via
                 their board.json readable-id link. issue_test fails if a second run creates
                 duplicates or if a scaffolded-phase slug is re-emitted as a backlog item.

## Coverage
- c-1 (dedicated [board] is the single sync source + doctor validates it): t-1 (config block + dotted arms), t-5 ([board]-not-[remote] resolution), t-3 (doctor validation)
- c-2 (YouTrack backend + factory returns a working client): t-2 (REST client + httptest), t-5 (factory dispatch)
- c-3 (pull surfaces unlinked YouTrack issues for /dross-inbox): t-7 (pull feed), t-4 (inbox prompt gate move)
- c-4 (phase-sync + milestone-sync keep the phase trackable, all 3 modes): t-6
- c-5 (--status drives the State custom field, warn-and-skip on missing state): t-8
- c-6 (backlog sync of roadmap slugs + someday ideas, idempotent): t-9

## Judgment calls
- Backend abstraction: chose a new `Backend` interface (new internal/issuetracker pkg) with a thin forge adapter wrapping the existing forge.Client + a native youtrack client; rejected extending forge.Client with a `provider==youtrack` branch (its int issue Number, /repos path, and label-id model are wrong for YouTrack's readable ids, custom fields, and project-scoped query) and rejected a standalone sibling with no shared seam (would fork every issue.go op). Why: an interface is the only shape that lets the int-vs-string identity and the two API models diverge without per-call provider switches in cmd.
- Identity migration lives in the keystone t-5, not its own wave-1 task: changing board.json values int→string breaks every issue.go caller at compile time, so it MUST land in the same atomic commit as the op re-route — splitting it would leave a red build between commits (violates atomic-green-commit).
- The three milestone_modes are co-located in t-6 (not three tasks): they are three branches of one ensure-milestone-entity method, so one task owns them — but each gets its OWN test contract, and agile's pre-existing-board dependency is called out explicitly as the sharpest failure mode (warn-and-skip, never abort).
- c-3 is split across t-4 (prompt gate) and t-7 (pull behaviour) deliberately: the inbound risk has two independent failure surfaces — the prompt reading the wrong config key (grep-tested per r-01) and the Go feed leaking linked/dismissed issues (unit-tested) — owned separately.
- Idempotency risk (c-6) is isolated in t-9 rather than folded into t-6: re-sync duplication is a distinct failure mode keyed on the board.json link lookup, and backlog items need a slug/idea→readable-id link that phase issues don't.
