# Synthesis — youtrack-board-integration

Cold judge over three independently-drafted decompositions (risk / mvp / verification).
I authored none; this merges the strongest skeleton and grafts concrete wins, leaving real
divergences visible rather than papered over.

## Scores

Scored /5 on the four asked dimensions. One line per draft per dimension.

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| **risk** | 5 — all c-1..c-6, each failure mode owned exactly once | 4 — sharp failure-mode prose, but few named test fns / fixtures | 4 — 9 tasks; keystone t-5 overloaded (new pkg + new interface + board migration + re-route in one) | 3 — splits the youtrack client (t-2) from the factory that returns it (t-5) across waves, against the "client+factory in one task" rule; migration itself is coherent |
| **mvp** | 4 — all covered, but c-1/c-3/c-4/c-5/c-6 all funnel through the single cmd task t-7 | 4 — names TestYouTrack* fns + endpoints; thinner on seeded fixtures | 2 — 7 tasks; t-7 buries five criteria and five failure surfaces behind one commit/test bundle | 3 — board.json int→string in wave-1 t-2 but the issue.go re-route that consumes it in wave-3 t-7: signature change risks a red build between waves; client+factory together (good) |
| **verification** | 5 — finest per-criterion mapping, every task traces to one regression test | 5 — exact test fns, endpoints, body fields, seeded fixtures (PROJ-7/12/20/21 linked/dismissed/new) | 4 — 12 tasks, mostly right; over-splits factory-arm (t-2) from CRUD (t-5) and has two near-trivial tasks (t-3 dotted, t-12 prompt) | 5 — config wave 1, migration atomic (t-6), modes one task, deps clean |

**Skeleton = verification.** It has the sharpest contracts, the cleanest waves (config in
wave 1, the board.json migration kept atomic, milestone modes as one task), complete
per-criterion coverage, and it picks the low-risk backend shape. Its only real weakness is
over-granularity, which I trim by grafting **mvp**'s "client + factory + CRUD in one task"
consolidation and **risk**'s "config struct + dotted arms in one task" grouping — landing at
10 tasks across 4 waves.

## Merged plan

Backend shape (provisional default, see Disagreement 1): a sibling concrete `YouTrackClient`
in `internal/forge/youtrack.go` behind a minimal `BoardClient` interface that the existing
concrete `*forge.Client` also satisfies, dispatched by a `forge.New`/`NewBoard` factory arm —
NOT a new `internal/issuetracker` + `internal/youtrack` two-package abstraction.

### Wave 1
- **t-1 — Add [board] config struct + dotted read/write arms** `[risk; +verification contract]`
  - files: `internal/project/project.go`, `internal/project/board_test.go`, `internal/cmd/project.go`, `internal/cmd/project_test.go`
  - covers: c-1
  - contract: `TestProjectBoardRoundTrip` — build `Board{Provider:"youtrack", BaseURL, AuthEnv, Project, Enabled:true, MilestoneMode:"version", StateMap:{"shipped":"Fixed"}}`, Save→Load to a tmp file; every field incl. the `state_map` map and `milestone_mode` survives (fails on a wrong toml tag or a dropped field). Plus a dotted-arm round-trip (extends `TestExpandedDottedPathsRoundTrip`): `project set/get board.provider|base_url|auth_env|project|enabled|milestone_mode` each round-trip through writeDotted→Save→Load so `/dross-options` can set them.
  - depends_on: —
- **t-2 — YouTrack REST client (CRUD) + BoardClient interface + factory arm** `[mvp; merges verification t-2+t-5]`
  - files: `internal/forge/youtrack.go` (new), `internal/forge/youtrack_test.go` (new), `internal/forge/forge.go`
  - covers: c-2
  - contract: `New(Config{Provider:"youtrack",...})` returns a working client, NOT `ErrNotImplemented` (mirrors `TestNewAcceptsGitLab`); compile-time `var _ BoardClient = (*Client)(nil)` and `var _ BoardClient = (*YouTrackClient)(nil)`. httptest mock: `TestYouTrackCreateIssue` asserts `POST /api/issues` body `{project:{…PROJ…},summary,description}` and `idReadable:"PROJ-7"`→`Issue.Key=="PROJ-7"`; `TestYouTrackListIssues` asserts `GET /api/issues?query=project:<short>…&fields=…` scoping; `TestYouTrackGetIssue`/`TestYouTrackUpdateIssue` hit `/api/issues/PROJ-7`; `TestYouTrackBearerAuth` asserts `Authorization: Bearer <token-from-auth_env>`. Fails on wrong path/verb/body/header or an int-id fallback. **Client + factory land together** (per the hard constraint; CRUD methods, not just a stub, in the same task).
  - depends_on: —
- **t-3 — Move inbox board gate to [board].enabled** `[verification t-12 = risk t-4]`
  - files: `assets/prompts/inbox.md`, `internal/cmd/inbox_prompt_test.go`
  - covers: c-3
  - contract: `TestInboxPromptReadsBoardEnabled` — normalised `inbox.md` contains `dross project get board.enabled` (and `dross issue enable`/`[board]` migration guidance), no longer `remote.board_sync` (currently line 12: `dross project get remote.board_sync`). Grep test; r-01 — run `make install` before relying on it live.
  - depends_on: —

### Wave 2 (depends t-1, t-2)
- **t-4 — Validate [board] in dross doctor** `[verification t-4 = risk t-3]`
  - files: `internal/cmd/doctor.go`, `internal/cmd/doctor_test.go`
  - covers: c-1
  - contract: `TestDoctorValidatesBoardBlock` (gitInit+Init+captureStdout) — with `board.enabled=true`: unset `$auth_env` → `✗ … auth_env`; `provider="bogus"` (not forgejo|gitea|gitlab|youtrack) → `✗ … provider`; malformed `base_url` and invalid `milestone_mode` → `✗`; a well-formed youtrack board → a `✓` board line. Fails if doctor ignores `[board]` or rejects the youtrack provider.
  - depends_on: t-1
- **t-5 — Store readable string issue id in board.json + re-route board ops to [board]** `[verification t-6 = risk t-5; the migration-risk owner]`
  - files: `internal/board/board.go`, `internal/board/board_test.go`, `internal/cmd/issue.go`
  - covers: c-1, c-4
  - contract: `TestBoardReadableIDRoundTrip` — `SetPhase("youtrack-x","PROJ-123")`; Save+Load; `PhaseIssue`→`"PROJ-123"`; `IsLinked("PROJ-123")`/`IsDismissed` round-trip a string id (link values become the readable string identity). `TestOpenBoardResolvesFromBoardBlock` — repo with `[remote].provider=github` (no board backend) + `[board].provider` (forgejo or youtrack) `enabled` pointing at an httptest server: every board op resolves its client from `[board]`, hits the BOARD server, and there is NO `[remote]` fallback (a disabled `[board]` + fully-populated `[remote]` is a no-op). Fails if `openBoard` still reads `proj.Remote.BoardSync`/`proj.Remote.*` or links by int Number. **board.go signature change + issue.go re-route land in ONE atomic task** so the build never goes red between commits; the forge path (forgejo|gitea|gitlab) is re-routed through `[board].provider` too.
  - depends_on: t-1, t-2

### Wave 3 (depends wave 2)
- **t-6 — Ensure milestone entity per milestone_mode (version | agile | epic)** `[verification t-9 = mvp t-4 = risk t-6 part]`
  - files: `internal/forge/youtrack.go`, `internal/forge/youtrack_test.go`
  - covers: c-4
  - contract: three mode tests off one dispatch method — `TestYouTrackMilestoneVersionMode` ensures the project Version bundle (GET then POST under `/api/admin/projects/<id>`, default mode); `TestYouTrackMilestoneAgileMode` looks up the **pre-existing** Agile board via `GET /api/agiles` and, when the named board is absent, WARNS-and-skips the sprint/column attach instead of erroring; `TestYouTrackMilestoneEpicMode` creates-or-reuses an Epic issue and returns its idReadable. Re-running reuses, never duplicates. Fails if any mode errors, hits the wrong endpoint, or a missing agile board aborts the sync.
  - depends_on: t-2
- **t-7 — Set YouTrack State via state_map, warn-and-skip** `[verification t-8 = mvp t-5 = risk t-8]`
  - files: `internal/forge/youtrack.go`, `internal/cmd/issue.go`, `internal/forge/youtrack_test.go`
  - covers: c-5
  - contract: `TestYouTrackSetStateMapsAndUpdates` — `phase-sync --status shipped` with `state_map` mapping shipped→Fixed sends `POST /api/issues/PROJ-7` with a State custom-field value `"Fixed"` (built-in default map planned→Open…complete→Verified when `state_map` empty). `TestYouTrackSetStateUnmappedWarnsSkips` — target state absent from the project workflow: no State write is sent, the rest of the issue patch still succeeds (`err==nil`), a warning is printed. Fails if a missing/unmapped state aborts the whole phase-sync or silently no-ops the warning.
  - depends_on: t-2, t-5
- **t-8 — Surface YouTrack issues in `issue pull`** `[verification t-7 = risk t-7]`
  - files: `internal/cmd/issue.go`, `internal/cmd/issue_test.go`
  - covers: c-3
  - contract: `TestIssuePullYouTrackFiltersLinkedAndDismissed` — youtrack httptest returns three open issues (PROJ-12 linked to a phase, PROJ-20 dismissed, PROJ-21 new); board.json seeded with those links; `dross issue pull --labels bug --json` emits only PROJ-21 and the upstream `GET` query carries the bug tag as a YouTrack `query` clause. Fails if a linked/dismissed string-id issue leaks in or the label filter is dropped.
  - depends_on: t-2, t-5

### Wave 4 (depends wave 3)
- **t-9 — Wire phase-sync + milestone-sync to YouTrack** `[verification t-10]`
  - files: `internal/cmd/issue.go`, `internal/cmd/issue_test.go`
  - covers: c-4
  - contract: `TestIssuePhaseSyncYouTrackCreatesThenUpdates` — first `phase-sync` POSTs `/api/issues` with a body carrying acceptance criteria + a `- [ ]`/`- [x]` task checklist (reuse `renderPhaseBody`) and stores `board.json phases[<phase>]=="PROJ-7"`; second sync POSTs `/api/issues/PROJ-7` (update, NO new create). `TestIssueMilestoneSyncYouTrack` — the milestone entity is ensured per `milestone_mode` (calls into t-6) and linked in board.json. Fails if a second sync duplicates the issue or skips the milestone link.
  - depends_on: t-5, t-6, t-7
- **t-10 — Sync milestone backlog (unscaffolded slugs + unrouted someday), idempotent** `[verification t-11 = risk t-9]`
  - files: `internal/cmd/issue.go`, `internal/board/board.go`, `internal/cmd/issue_test.go`
  - covers: c-6
  - contract: `TestIssueBacklogSyncYouTrackIdempotent` — `milestone.phases=["01-done","future-x"]` with only `01-done` scaffolded (phase dir exists) + one unrouted `someday` deferred (`collectDeferred`, `Target==""`); first run POSTs exactly two backlog items (future-x + the deferred) in Open/backlog state attached to the milestone entity per `milestone_mode` and records them in a board.json backlog map; second run POSTs zero new items, updating the SAME items via their readable-id link. Fails if a scaffolded slug leaks in, the deferred item is missed, or re-running duplicates.
  - depends_on: t-5, t-6

**Coverage roll-up:** c-1 → t-1, t-4, t-5 · c-2 → t-2 · c-3 → t-3, t-8 · c-4 → t-5, t-6, t-9 · c-5 → t-7 · c-6 → t-10. All six covered.

## Disagreements

### D1 — Backend structure (the headline divergence)
- **risk:** new `internal/issuetracker` package defining a `Backend` interface + a thin forge adapter wrapping `forge.Client`, plus a native `internal/youtrack` client package. Two new packages, a full interface seam.
- **mvp / verification:** a sibling concrete `YouTrackClient` in `internal/forge/youtrack.go` behind a **minimal** `BoardClient` interface (the method set issue.go already calls) that the existing concrete `*forge.Client` also satisfies; `forge.New` factory dispatches.
- **Provisional default:** the mvp/verification shape (sibling in `internal/forge`, minimal interface).
- **Why it matters:** lowest-risk and matches the existing concrete-`Client` shape; the GitLab phase deliberately rejected a speculative interface, and a two-package abstraction layer is more churn than the divergent API requires. The interface here is NOT speculative — `issue.go`'s `ctx.client` must now hold one of two concrete types, so a minimal seam is genuinely forced. **Trade-off:** if the deferred GitHub *and* Jira backends both land, risk's clean `Backend` package would age better — revisit the extraction then, not now. (Secondary mark against risk: its t-2 builds the youtrack client but the factory that returns it lives in t-5, splitting them across waves — see D4.)

### D2 — Task count / where the cmd wiring lands (7 vs 9 vs 12)
- **mvp (7):** collapses ALL `issue.go` rewiring — re-route, phase-sync, backlog gather, pull, inbox gate, `--status` — into a single wave-3 task t-7 covering c-1/c-3/c-4/c-5/c-6.
- **verification (12):** spreads cmd wiring across t-6/t-7/t-8/t-10/t-11, one failure surface per task.
- **risk (9):** in between — phase-sync+milestone in t-6, pull in t-7, state in t-8, backlog in t-9.
- **Provisional default:** the granular spread (verification, trimmed to ~10 after the D4/D5 merges).
- **Why it matters:** a single cmd task covering five criteria buries five independent regression surfaces behind one commit and one test bundle — you can't tell which criterion broke. Per-surface tasks give honest per-criterion isolation. **Trade-off:** more tasks touch `issue.go`, but they're serialized across waves (t-5 then t-7/t-8 then t-9/t-10), so no merge churn — just more, smaller, gated commits, which matches this repo's atomic-commit discipline.

### D3 — board.json id migration vs the [board] re-route: atomic or split
- **mvp:** board.go int→string in wave-1 t-2, the issue.go re-route that consumes it in wave-3 t-7.
- **risk / verification:** board.go id change + issue.go re-route in ONE task (risk t-5, verification t-6).
- **Provisional default:** atomic — one task (merged t-5).
- **Why it matters:** changing board.json link values int→string changes `board.go` method signatures (`SetPhase(string,int)`→`(string,string)`, `IsLinked(int)`→`(string)`), which breaks every `issue.go` caller at compile time. Splitting across waves leaves a red build between commits — a direct atomic-green-commit violation. mvp's mitigation (`Issue.Key` stringifies the forge int) helps the value model but does not remove the signature-change break. This task is the migration-risk owner the spec flags: it also re-routes the forge path (forgejo|gitea|gitlab) through `[board].provider` so existing forge board behaviour keeps working off the new single source.

### D4 — client + factory + CRUD: one task or split
- **mvp:** one task (t-3) — CRUD client + factory together.
- **verification:** factory-arm + interface (t-2) split from CRUD impl (t-5).
- **risk:** client built in t-2, factory in t-5 — across two waves.
- **Provisional default:** ONE task (graft mvp t-3; merge verification's t-2+t-5 into merged t-2).
- **Why it matters:** the hard constraint — the YouTrack client and the factory/dispatch that returns it must land together (like the GitLab forge work) for compile/test integrity. risk's cross-wave split is the worst here; verification's same-wave split is tolerable but needlessly granular; mvp's single task is the cleanest fit.

### D5 — config struct + dotted arms + doctor: one task or three (minor)
- **risk:** struct + dotted arms in t-1; doctor separate.  **mvp:** struct + doctor in t-1.  **verification:** three separate tasks (struct t-1, dotted arms t-3, doctor t-4).
- **Provisional default:** merge struct + dotted arms (merged t-1); keep doctor separate (t-4).
- **Why it matters:** the struct (`internal/project`) and the `readDotted`/`writeDotted` arms (`internal/cmd/project.go`) are the one cohesive "configure `[board]`" unit and trimming verification's tri-split removes a near-trivial task without losing a contract; doctor validation is a genuinely distinct concern with its own gitInit+captureStdout harness, so it stays on its own.

### Consensus, recorded (NOT a divergence) — milestone_mode
All three drafts agree: the three locked modes (version | agile | epic) are **one task** (merged t-6)
with **three separate per-mode test contracts**, not three tasks — they share one ensure-entity
dispatch and the per-mode delta is only the endpoint. None of the three is dropped. The sharp edge,
called out by risk and verification alike, is `agile`'s dependence on a pre-existing Agile board:
the contract pins WARN-and-skip (never abort) when that board is absent.

---
synthesis: 10 tasks across 4 waves, 5 disagreements
</content>
</invoke>
