# Plan Review — board-sync-truth

Reviewed: 2026-08-13
Plan: 13 tasks across 3 waves

## BLOCKING

(none)

Coverage is complete: c-1 → t-1/t-2/t-3/t-13, c-2 → t-3/t-4, c-3 → t-5/t-8,
c-4 → t-6/t-9, c-5 → t-11, c-6 → t-6/t-10, c-7 → t-7/t-12. Every file the plan
names exists (`internal/project/project.go`, `internal/board/board.go`,
`internal/cmd/deferred_add.go`, all four forge backends) except the test files
each task creates. No task implies a rules.toml violation — `runtime.mode` is
`native`, r-01 is the only rule, and there is no global `~/.claude/dross/rules.toml`.

## FLAG

- [wave-order] The plan uses "wave" inconsistently — as a parallelism unit in one
  place and a checkpoint boundary in another. Wave 1's seven tasks collide heavily
  on files: `youtrack.go` in t-1, t-3, t-6, t-7; `jira.go` in t-1, t-3, t-7;
  `forge.go` in t-3, t-5, t-7; `internal/cmd/issue.go` in t-4 and t-5. Those cannot
  run in parallel. Yet t-11 is pushed to wave 3 behind t-8 for what looks like the
  same reason inverted — t-11's contracts never reference a configured field name,
  so it needs no *output* from t-8, only non-collision on `SetState`/`ensureVersion`.
  Separately, `depends_on` is documented as naming lower waves only, so the real
  intra-wave-1 ordering (t-1/t-2 rewrite the query builders, t-3 then wires
  `FilterKnownLabels` into those same `ListIssues` bodies) is unexpressed anywhere.
  Suggestion: pick one reading. If waves are sequential checkpoints, say so and
  t-11 is fine where it is. If they are parallelism units, t-3 belongs in wave 2
  behind t-1/t-2 — accepting that this cascades t-13 to wave 3 and t-11/t-12 to
  wave 4.

- [antipattern] The stale-`board.json` duplicate class that t-9 closes for phase
  issues stays open for deferred backlog items, and t-10 widens the exposure.
  `mirrorDeferredAdd` (internal/cmd/deferred_add.go:152) already pushes a routed
  item's issue at `deferred add` time and records the link in board.json keyed by
  `deferredBacklogKey(d.ID)`; t-10 removes the `d.Target != ""` skip at
  internal/cmd/issue.go:454 so backlog-sync now touches the same items. Dedupe
  still rests entirely on board.json — the exact store the `mapping_authority`
  decision demotes because `phase complete` deletes the branch it was written on.
  Add on a phase branch, ship, re-sync from base: second issue. t-10's contract
  cannot catch this — "edit the item's text and re-sync: ONE update on the same
  key, zero creates" passes trivially with board.json intact.
  Suggestion: either add a resolve-before-create arm to t-10 mirroring t-9 (t-6
  makes a `dross/deferred:<id>` tag actually reach a YouTrack issue, so the same
  query works), or state explicitly in the plan that backlog items keep the
  cache-only mapping and record it as deferred.

- [locked-decision] t-9 deviates from `mapping_authority` as literally worded. The
  decision says resolve "by querying for the dross marker label plus the phase id";
  t-9's contract requires the opposite — "the resolver's filter carries exactly ONE
  label", with the marker checked as a post-filter ("a ListIssues hit whose labels
  lack `dross` is NOT adopted"). The deviation is correct and t-9 explains why: t-1
  makes multi-label filters OR, so a two-label query would adopt an arbitrary dross
  issue. Net semantics (marker AND phase-id) are preserved, which is why this is a
  flag and not blocking.
  Suggestion: amend the decision's wording to "query the phase label, verify the
  marker on the result" so the spec and plan don't read as contradicting.

- [test-contract] t-3 does not pin the empty-label short-circuit, and one existing
  caller depends on it. internal/cmd/watch.go:47 calls `collectInbound` with
  `forge.IssueFilter{State: "open"}` and no labels, and assets/prompts/watch.md
  promises `dross watch` "never errors out" and runs on a `/loop` timer. t-3's arm
  "a 500 from the label endpoint surfaces as an error and zero ListIssues calls"
  turns a label-endpoint blip into a failed heartbeat if the index is fetched
  unconditionally, and adds a request to every unlabelled pull. t-2 pins this for
  GitHub only ("an unlabelled filter issues exactly 1 request"); t-3 and t-13 don't.
  Suggestion: add an arm to t-3 — empty `Labels` → zero label-index requests, on
  every provider.

- [test-contract] t-4's status.md arm misses the line that actually causes the
  fault. assets/prompts/status.md:10 currently instructs "on any error, skip that
  source's contribution silently (never block)" — that instruction, not a bare
  `--json`-as-array step, is what makes /dross-status count an unreachable board as
  zero. t-4's contract ("references `.issues` / `.error` and the unreachable
  wording; no bare `--json`-as-array step survives") does not require its removal,
  so a compliant edit could add the envelope handling and leave the silent-skip
  instruction standing above it.
  Suggestion: add an arm asserting the silent-skip instruction is gone from
  status.md.

- [granularity] Three split candidates. t-3 is the worst: 8 files, the forge core
  plus three independent provider wiring sites, each with its own label endpoint
  (`/api/issueTags`, `/rest/api/3/label`, `/repos/o/r/labels`) — that is one helper
  plus four separable jobs, and it is also the task creating the wave-1 collision
  above. t-11 is 7 files spanning forge (two providers) and cmd. t-5 is 6 files
  spanning `internal/project`, `internal/cmd`, `internal/forge` and README.
  Suggestion: split t-3 at minimum — helper + per-provider wiring.

- [antipattern] t-13 covers forgejo/gitea/gitlab, which no criterion asks for — c-1
  scopes the OR fix to "youtrack, jira and github". It is the same bug in the shared
  REST client so fixing it alongside is defensible, but its own contract concedes
  "no live server verification is claimed for this backend", meaning it cannot be
  proven the way the in-scope providers can.
  Suggestion: keep it, but mark it explicitly as opportunistic so an unprovable
  gitlab arm never becomes the reason verify returns partial.

## NOTE

- [antipattern] `dross watch --json` already emits `board_ok` for exactly the fact
  the new envelope's `error` field carries (assets/prompts/watch.md:19, and
  watch.md:34 already renders an "off / unreachable" digest). The plan introduces a
  second vocabulary for one condition without reconciling them. The
  `pull_failure_signal` decision is locked on the envelope shape, so this is
  observation only — but the two surfaces should not drift in wording.

- [antipattern] t-10's last arm removes the stale "left to board-sync-truth" note
  from `mirrorDeferredAdd`'s doc comment (internal/cmd/deferred_add.go:151). A
  second stale reference exists at internal/cmd/issue_backlog_id_test.go:323
  ("routed items is board-sync-truth's remit") and is not in any task's file list.

- [test-contract] t-9's "State \"all\"" arm needs no new plumbing —
  `IssueFilter.State` already documents `"all"` (internal/forge/forge.go:212) and
  YouTrack's `buildQuery` switch already falls through for it, adding neither
  `#Unresolved` nor `#Resolved`. Worth knowing so the task isn't budgeted for it.

- [strengths] The contracts are falsifiable rather than descriptive. Most name the
  behaviour that must fail today — "today's two space-joined `tag:` clauses (the AND
  bug) fail it", "a nil return fails, because that silent nil IS the c-5 bug",
  "without the read-back this test cannot fail". That is the difference between a
  test contract and a restated task title.

- [strengths] The plan found real bugs the spec only gestures at, and all three
  check out in the repo: `YouTrackClient.CloseIssue` is a bare `return nil`
  (internal/forge/youtrack.go:167-169), `CreateIssue` never sends tags, and the
  routed skip sits at internal/cmd/issue.go:454. Diagnoses verified, not assumed.

- [strengths] Negative and non-regression assertions are used deliberately —
  `(*GitHubClient)(nil)` must NOT satisfy `IssueLinker`, zero POSTs on a re-run,
  board.json byte-identical after a failed `--mark`, `statusCategory != Done` still
  AND-joined, a zero-value Config still yielding today's literals. These target the
  silent-success failure mode this whole phase exists to kill.

## Summary

No blockers — coverage is complete, the diagnoses are verified against the code,
and the contracts are unusually specific; the substantive gaps are the unresolved
duplicate-issue path for routed deferred items and an inconsistent reading of what
a wave means.
