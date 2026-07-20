# dross-watch — synthesis (cold judge)

Merged from three independently-drafted decompositions (risk / mvp / verification).
I authored none of them. Seams claimed by the drafts were sanity-checked against
source and all hold: `issuePull` in `internal/cmd/issue.go` (ListIssues → drop
IsLinked/IsDismissed, `--mark` stamps last_pull via MarkPulled/Save);
`stateRecordsCompleted` / `readVerifyVerdict` / `NextRunnable` / `suggestNext` /
`spineIdle` in `internal/cmd/status.go`; `root.AddCommand` in `cmd/dross/main.go`;
and `docs/interaction-audit.md` + a genuinely fail-closed
`internal/cmd/interaction_coverage_test.go` (a new non-interactive command that
skips its `## Exempt` row fails the build). That last gate is real and only one
draft caught it.

## Scores

Dimensions scored 1 line per draft. (cov = criteria coverage; spec = test-contract
specificity; gran = granularity; wave = wave correctness.)

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|-------|-------------------|---------------------------|-------------|------------------|
| **risk** (8t/3w) | Strong per-criterion map, but MISSES the fail-closed interaction-audit Exempt gate → c-1 build-break uncovered. | Best per-failure-mode: named test per corrupt/retitle/reopen/atomic case, hash-before/after. | Finest — 5 pure wave-1 files; arguably over-split (state/delta/drift/suggest/board all separate). | Has a real bug: t-6 and t-7 both labelled "Wave 2" though t-7 depends on t-6, so t-7 is actually wave 3. |
| **mvp** (2t/1+1w) | All 5 covered but coarsely; also MISSES the interaction-audit Exempt gate. | Solid end-to-end httptest contracts + HTTP method-log for no-mutation; weak isolation for reopen-vs-retitle inside one blob. | Coarsest — one mega-command task (c-1/c-2/c-4/c-5 + c-3 machine-half); hard to review/commit atomically. | Correct and clean (t-2 depends t-1). |
| **verification** (5t/3w) | Best — all 5 PLUS the interaction-audit Exempt enrollment, the mark-free helper extraction, and the correct registration site. | Test-first, every criterion anchored to a named failing test; slightly folds state+diff vs risk. | Middle ground — 5 tasks, good review/atomic-commit balance. | Correct 3 waves, proper depends_on, folds Exempt into the shim commit (commit-safety honoured). |

**Skeleton: verification.** It is the only draft that catches the fail-closed
interaction-audit gate (a between-commits build-break the other two would hit),
its waves and depends_on are correct (risk's are mislabelled), and its
test-first contracts anchor every criterion. Granularity sits between risk's
over-split 8 and mvp's under-split 2. Runners-up donate sharper sub-contracts.

## Merged plan

Phase dross-watch — 5 tasks across 3 waves

### Wave 1

**t-1  Watch state file + new/carried diff**  [verification + risk]
- files: `internal/watch/watch.go`, `internal/watch/watch_test.go`
- covers: c-2, c-4
- contract: `TestWatchDiff` — feed byte-identical to prior seen-set → New=∅
  (second-tick-no-change); id absent from prior → New; same id state flipped
  open→closed → New (reopen identity); same-id+same-state, title changed → NOT
  New (cosmetic edit). `TestWatchFirstRun` — nil/absent prior seeds baseline:
  New=∅, all feed items Current. `TestWatchStateRoundTrip` — Save→Load
  reproduces seen-set; Load of a missing file returns empty baseline, not error.
  **[grafted from risk]** `TestSaveAtomicRename` — Save is temp-file+rename, a
  simulated mid-write interruption leaves the prior file intact;
  `TestLoadCorruptDegrades` — malformed JSON degrades to empty baseline, not an
  error. (The sole mutable file must never corrupt the baseline all c-2
  correctness rests on.)
- depends_on: none

**t-2  Phase-drift classifier over all phases**  [verification + risk]
- files: `internal/watch/drift.go`, `internal/watch/drift_test.go`
- covers: c-1, c-5
- contract: `TestClassifyDrift` over a fixture `.dross` tree — plan with a
  runnable/failed task → `in_progress`; all-tasks-done + verify.toml
  absent-or-verdict-empty/pending → `complete_unverified`; verify verdict `pass`
  with no `completed <slug>` in state.History → `verified_unshipped`; verify
  `pass` WITH a `completed <slug>` → omitted (shipped, no drift).
  `TestClassifyDriftNoBoard` — classifier reads only phase files + state.json,
  returns a full result with the board client nil (the drift-only path c-5 leans
  on). **[grafted from risk]** `TestDriftMissingPlanTolerated` — a phase dir with
  a missing/garbled plan.toml must not panic.
- depends_on: none

**t-4  Extract mark-free inbound helper from `issue pull`**  [verification]
- files: `internal/cmd/issue.go`, `internal/cmd/issue_test.go`
- covers: c-4
- contract: `TestCollectInboundNoMark` — `collectInbound(ctx, filter)` returns
  the IsLinked/IsDismissed-filtered issue set AND leaves board.LastPull zero
  (board.json byte-identical), proving the reuse path never stamps last_pull.
  `TestIssuePullStillMarks` — `dross issue pull --mark` still updates last_pull
  and plain `pull` still doesn't (existing behaviour preserved through the
  refactor).
- depends_on: none

### Wave 2

**t-3  `dross watch --json` command + registration + suggested_command ranker**  [verification + mvp + risk]
- files: `internal/cmd/watch.go`, `cmd/dross/main.go`, `internal/cmd/watch_test.go`
- covers: c-1, c-2, c-4, c-5, c-3 (machine half: `suggested_command`)
- contract:
  - `TestWatchJSONShapeAndExit` — prints JSON unmarshalling into
    `{new, current, drift{...}, suggested_command}` and exits 0 (c-1).
  - `TestWatchReadOnlyBoundary` — board.json bytes (incl. last_pull)
    byte-identical before/after, and the only file written under `.dross` is
    `watch.state.json` (c-4). **[grafted from mvp]** httptest handler records HTTP
    method+path; assert only `GET /issues` was hit (no POST/PATCH/PUT/DELETE) —
    a MarkPulled/mutating call flips it red.
  - `TestWatchSecondRunZeroNew` — two consecutive runs against an unchanged feed:
    run-2 `new` is empty (c-2).
  - `TestWatchBoardDisabled` — `board.enabled=false` → empty board set + populated
    drift, exit 0 (c-5).
  - `TestWatchBoardUnreachable` — board enabled but client erroring → error
    swallowed, drift-only digest, exit 0; a propagated error fails (c-5).
    **[grafted from mvp]** watch.state.json is written ONLY when the board was
    actually reached, so an unreachable tick preserves the prior baseline (else
    the next healthy tick re-flags the whole backlog as new).
  - **[grafted from risk+mvp]** `TestSuggestPrecedence` — the Go-computed
    `suggested_command` ranks verify→ship→inbox→status (complete-but-unverified
    AND new issues present yields verify, not /dross-inbox); returns exactly one,
    never empty/multiple; idle-with-no-new → /dross-status (c-3 machine half,
    honouring the locked `suggestion_precedence`).
- depends_on: t-1, t-2, t-4

### Wave 3

**t-5  /dross-watch skill + prompt tests + Exempt enrollment**  [verification + risk + mvp]
- files: `assets/prompts/watch.md`, `assets/commands/dross-watch.md`,
  `docs/interaction-audit.md`, `internal/cmd/watch_prompt_test.go`
- covers: c-3, c-5
- contract: `TestWatchPromptInvokesCommand` — watch.md calls `dross watch --json`.
  `TestWatchPromptSuggestionPrecedence` — prompt states the ranked order
  verify→ship→/dross-inbox→/dross-status AND prints exactly the digest's single
  `suggested_command` verbatim (c-3). `TestWatchPromptBoardOffPath` — mirrors
  inbox.md: announces skipping the board source, still renders a drift-only
  digest when board is off (c-5). `TestWatchShimNonInteractive` — dross-watch.md
  omits AskUserQuestion. `TestInteractionCoverageFailClosed` (existing) — passes
  ONLY because `watch` is enrolled in `docs/interaction-audit.md` `## Exempt` in
  THIS commit; a missing row flips it red (the fail-closed gate both other drafts
  missed). `TestCommandsPromptsParity` (existing) — shim+prompt keep the 1:1
  invariant. **[grafted from risk]** frontmatter shape asserted against
  dross-inbox.md's; the content-gate mirrors secure_prompt_test.go.
  **[grafted from mvp]** `allowed-tools: Read + Bash only` (non-interactive per
  the locked `/loop`-driven substrate).
- depends_on: t-3

**Coverage roll-up** — c-1 → t-2, t-3; c-2 → t-1, t-3; c-3 → t-3 (machine),
t-5 (render); c-4 → t-1, t-4, t-3; c-5 → t-2, t-3, t-5. All of c-1..c-5 homed.
All locked decisions honoured (substrate, read_only_boundary, first_run_baseline,
delta_identity, suggestion_precedence, drift_signals).

## Disagreements

Genuine structural divergences left explicit, not papered over. Each records a
provisional default for the merged plan.

**D1 — Package structure: new `internal/watch` package vs inline in `cmd/watch.go`.**
- risk & verification: pure logic (state/diff/drift) lives in a new
  `internal/watch` package, unit-testable without the cobra/stdout layer.
- mvp: no new package — keep everything in `internal/cmd/watch.go`, same package
  as status.go, reusing `readVerifyVerdict`/`NextRunnable`/`stateRecordsCompleted`
  **verbatim** (no duplication).
- **Coupled sub-point:** a separate `internal/watch` package CANNOT import
  status.go's helpers (they live in `internal/cmd` → import cycle). So the
  package split forces verification to *reimplement the drift signal-definitions*
  in `internal/watch`; mvp's inline choice reuses them directly.
- **Provisional default: new `internal/watch` package (skeleton = verification),
  drift definitions reimplemented from the same signals** (NextRunnable, verify
  `pass`, `completed <slug>` in state.History), pinned by t-2's fixture table.
- **Why it matters:** trades one duplicated ~30-line classifier for
  unit-testability of the reopen-vs-retitle diff and the nil-board drift path
  without cobra/stdout capture. If a reviewer would rather not duplicate drift
  logic, D1 flips to mvp's inline model and t-1/t-2 collapse into t-3.

**D2 — Granularity: 8 vs 5 vs 2 tasks.**
- risk: 8 (state, delta, drift, suggest, board-fetch each their own wave-1 file).
- verification: 5. mvp: 2 (one mega-command + skill).
- **Provisional default: 5 (verification), with risk's sharper sub-contracts
  grafted in rather than split out** (atomic-rename, corrupt-degrade,
  garbled-plan-tolerated fold into t-1/t-2; suggest ranker folds into t-3 as mvp
  places it).
- **Why it matters:** 8 gives the finest failure-mode isolation but fragments a
  cohesive layer; 2 is barely reviewable and a regression fails a coarse
  pass/fail. 5 keeps atomic commits meaningful while each grafted named test
  still fails on exactly one regressed behaviour.

**D3 — Extract a mark-free `collectInbound` helper from issue.go, or not.**
- verification: yes — extract `collectInbound(ctx, filter)` as its own wave-1
  task (t-4), refactoring `internal/cmd/issue.go` so the read_only_boundary is
  *structurally* guaranteed (one filter implementation, no --mark).
- risk: no — reimplement the read-only fetch inline in `cmd/watch.go` (its t-5).
- mvp: no — call `ListIssues` + the IsLinked/IsDismissed filter inline via
  `openBoard`, no refactor to issue.go.
- **Provisional default: extract (verification's t-4).** One filter path means
  watch and `issue pull` cannot silently drift and re-introduce a `--mark`.
- **Why it matters:** it touches shared code (issue.go blast radius) and needs
  `TestIssuePullStillMarks` to prove the refactor preserves existing pull
  behaviour. If that blast radius is unwanted, D3 flips and watch copies the
  filter inline (c-4 then rests on the byte-identical boundary test alone, not on
  structural single-sourcing).

**D4 — `suggested_command`: Go-computed digest field vs skill-side prompt logic.**
- risk & mvp: compute the ranked command in Go, carry it as a `suggested_command`
  digest field, and unit-test the precedence (`TestSuggestPrecedence`).
- verification: leans on the prompt (its t-5) to state the ranked order and route,
  with no distinct Go ranker task.
- **Provisional default: Go-computed field (risk + mvp), folded into t-3.** The
  locked 4-way `suggestion_precedence` (verify→ship→inbox→status) gets a real Go
  test; the skill only prints the field verbatim.
- **Why it matters:** precedence in the prompt is only checkable by reading
  markdown; a Go field makes the locked ranking a failing unit test. t-5 still
  asserts the prompt prints that single field, so both halves of c-3 are gated.
