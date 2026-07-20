# dross-watch — VERIFICATION-lens decomposition

Design order: for each criterion I wrote the ideal test contract first, then derived
the smallest task that makes that contract satisfiable. Every acceptance criterion is
anchored to a Go test that fails when the behaviour regresses — no criterion rests on
"a human eyeballs the digest".

Grounding notes (read before planning):
- Inbound feed = the exact logic inside `issuePull`'s RunE (internal/cmd/issue.go:600-625):
  `ListIssues` → drop `IsLinked`/`IsDismissed`. Watch must reuse it **without `--mark`**
  (read_only_boundary), so the filter is extracted into a shared, mark-free helper first.
- Board off/unreachable pattern mirrors `issuePull`'s `!enabled` branch (issue.go:605-610)
  and inbox.md §0.
- Drift buckets are the three status already defines — in-progress (plan `NextRunnable`
  or failed), complete-but-unverified (no runnable, verify verdict empty/pending/absent),
  verified-but-unshipped (verify verdict `pass`, no `completed <id>` in state.History,
  cf. status.go `stateRecordsCompleted`/`readVerifyVerdict`). No new thresholds.
- Command registration site is `cmd/dross/main.go` (root.AddCommand list), not root.go.
- `/dross-watch` is a broadcast (non-interactive) → it must be enrolled in
  docs/interaction-audit.md `## Exempt` in the SAME commit that adds the shim, or
  `interaction_coverage_test.go` (fail-closed) breaks the build.

---

Phase dross-watch — 5 tasks across 3 waves

Wave 1
  t-1  Watch state file + new/carried diff
       files:    internal/watch/watch.go, internal/watch/watch_test.go
       covers:   c-2
       contract: TestWatchDiff — feeding a feed byte-identical to the prior state's
                 seen-set yields New=∅ (second-tick-no-change); an id absent from prior
                 → in New; the SAME id with state flipped open→closed (reopen identity)
                 → in New; a same-id+same-state entry whose title changed → NOT in New
                 (cosmetic edit). TestWatchFirstRun — a nil/absent prior state seeds the
                 baseline: New=∅, every feed item in Current. TestWatchStateRoundTrip —
                 Save then Load of .dross/watch.state.json reproduces the seen-set;
                 Load of a missing file returns an empty baseline, not an error.

  t-2  Phase-drift classifier over all phases
       files:    internal/watch/drift.go, internal/watch/drift_test.go
       covers:   c-1, c-5
       contract: TestClassifyDrift over a fixture .dross tree — a phase whose plan has a
                 runnable/failed task lands in `in_progress`; all-tasks-done + verify.toml
                 absent-or-verdict-empty/pending lands in `complete_unverified`; verify
                 verdict `pass` with no `completed <slug>` in state.History lands in
                 `verified_unshipped`; verify `pass` WITH a `completed <slug>` record is
                 omitted (shipped, no drift). TestClassifyDriftNoBoard — classifier reads
                 only phase files + state.json, so it returns a full result with the board
                 client nil (this is the drift-only path c-5 leans on).

  t-4  Extract mark-free inbound helper from issue pull
       files:    internal/cmd/issue.go, internal/cmd/issue_test.go
       covers:   c-4
       contract: TestCollectInboundNoMark — `collectInbound(ctx, filter)` returns the
                 IsLinked/IsDismissed-filtered issue set AND leaves board.LastPull zero
                 (byte-identical board.json), proving the reuse path never stamps last_pull.
                 TestIssuePullStillMarks — `dross issue pull --mark` still updates
                 last_pull and plain `pull` still doesn't (existing behaviour preserved
                 through the refactor).

Wave 2 (depends t-1, t-2, t-4)
  t-3  `dross watch --json` command + registration
       files:    internal/cmd/watch.go, cmd/dross/main.go, internal/cmd/watch_test.go
       covers:   c-1, c-2, c-4, c-5
       description: New cobra `watch` command: collect inbound via t-4's helper (no --mark),
                 diff via t-1 against .dross/watch.state.json, classify drift via t-2,
                 marshal the digest to stdout on `--json`, then write ONLY watch.state.json.
                 Register in main.go's root.AddCommand list.
       contract: TestWatchJSONShapeAndExit — `dross watch --json` in a fixture repo prints
                 JSON that unmarshals into {new, current, drift{...}} and exits 0
                 (c-1). TestWatchReadOnlyBoundary — capture board.json bytes before/after a
                 run; assert byte-identical incl. last_pull, and that the only file written
                 under .dross is watch.state.json (c-4). TestWatchSecondRunZeroNew — two
                 consecutive runs against an unchanged feed: run-2's `new` array is empty
                 (c-2). TestWatchBoardDisabled — with board.enabled=false the digest carries
                 an empty board set + populated drift and exits 0 (c-5). TestWatchBoardUnreachable —
                 with board enabled but the client erroring (unset token / ErrNotImplemented
                 provider), the error is swallowed, a drift-only digest is emitted, exit 0;
                 a propagated error fails this test (c-5).

Wave 3 (depends t-3)
  t-5  /dross-watch skill + prompt tests + Exempt enrollment
       files:    assets/prompts/watch.md, assets/commands/dross-watch.md,
                 docs/interaction-audit.md, internal/cmd/watch_prompt_test.go
       covers:   c-3, c-5
       description: Prompt renders `dross watch --json` output as a compact block and ends
                 with exactly one suggested command, ranked verify → ship → /dross-inbox →
                 /dross-status. Broadcast shim (no AskUserQuestion). Enroll `watch` in the
                 interaction-audit `## Exempt` table in the same commit so the fail-closed
                 coverage gate stays green.
       contract: TestWatchPromptInvokesCommand — watch.md calls `dross watch --json`.
                 TestWatchPromptSuggestionPrecedence — the prompt states the ranked order
                 verify → ship → /dross-inbox → /dross-status AND routes to /dross-inbox
                 when new board issues exist, /dross-status otherwise (c-3, suggestion_precedence).
                 TestWatchPromptBoardOffPath — mirrors inbox: announces skipping the board
                 source and still renders a drift-only digest when board is off (c-5).
                 TestWatchShimNonInteractive — dross-watch.md omits AskUserQuestion.
                 TestInteractionCoverageFailClosed (existing) — passes only because `watch`
                 is enrolled in `## Exempt`; a missing row flips it red, proving the
                 broadcast classification. TestCommandsPromptsParity (existing) — the
                 shim+prompt pair keeps the 1:1 invariant.

## Coverage

- c-1  (JSON digest, exit 0, no mutation)        → t-2 (drift content), t-3 (shape/exit/JSON)
- c-2  (new-vs-carried diff, zero-new on repeat) → t-1 (diff unit), t-3 (end-to-end second run)
- c-3  (skill render + single ranked suggestion) → t-5
- c-4  (mutates only watch.state.json)           → t-4 (mark-free reuse), t-3 (byte-identical boundary)
- c-5  (board off/unreachable → drift-only, 0)   → t-2 (nil-board classify), t-3 (disabled+unreachable), t-5 (prompt board-off path)

All of c-1..c-5 accounted for.

## Judgment calls

- Split the watch logic into a pure `internal/watch` package (t-1 diff, t-2 drift) rather
  than inlining it in cmd/watch.go — chose testability of the diff/reopen identity and drift
  buckets without cobra/stdout capture; rejected an all-in-one command file because c-2's
  reopen-vs-retitle contract and c-5's nil-board path are far cheaper to pin as unit tests.
- Reimplemented the three drift definitions in internal/watch (t-2) instead of importing
  status.go's helpers — chose this because status only classifies the *current* phase and
  its logic lives in cmd (import-cycle risk); rejected refactoring status.go into a shared
  classifier as too broad a blast radius for this phase (touches suggestNext/spineIdle). The
  drift_signals decision is honoured by reusing the same signal *definitions* (NextRunnable,
  verify `pass`, `completed <slug>` in state.History), pinned by t-2's fixture table.
- Extracted a mark-free `collectInbound` helper (t-4) as its own wave-1 task rather than
  duplicating the filter in watch — chose one filter implementation so the read_only_boundary
  (no --mark) is structurally guaranteed and testable in isolation; rejected copy-pasting
  issuePull's inline loop, which would let the two drift and silently re-introduce a mark.
- Folded the interaction-audit Exempt enrollment into t-5 (the shim-adding task) instead of a
  separate task — chose a single green commit because interaction_coverage_test is fail-closed:
  a shim landing without its Exempt row breaks the build between commits, violating commit-safety.
- Board-unreachable degradation lives in the command (t-3) swallowing the client error, not in
  the helper — chose to keep t-4's helper honestly returning its error so `dross issue pull`
  still surfaces board failures loudly; only watch (a passive broadcast) downgrades them to a
  drift-only digest, which is exactly c-5's "never errors out".
