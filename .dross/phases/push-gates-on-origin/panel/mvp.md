# MVP-lens plan — push-gates-on-origin

Phase push-gates-on-origin — 3 tasks across 2 waves

Design the tasks assume (derived from the locked decisions, spelled out so the
judge can check the tasks against it):

- The PR record commit carries `changes.json{pr, base}` only. `status =
  shipped` is NOT in that commit (today `ship.go:422` writes it into the same
  commit, which is exactly what lets a failed push report shipped).
- The single flip point runs after the record push succeeds: `state.json`
  `current_phase_status = shipped` + `changes.SetStatus(shipped)`, committed as
  `chore(dross): mark <id> shipped` and pushed through the same gate. If THAT
  push fails the phase is still honestly shipped (the PR record is on origin)
  and the next run's origin-ahead gate pushes the leftover commit (c-1).
- "Record pending" is a positive observation, never an inference: origin's
  `phase/<id>` ref exists and its `changes.json` does not carry the local PR
  number. A missing origin ref is unknown and changes nothing (this keeps
  `terminalPhaseFixture`-based status tests, which never push `phase/<slug>`,
  green without touching their fixtures).
- c-5's provider lookup reuses the existing `ship.OpenPRsTargetingFunc(opts,
  base)` (internal/ship/basepr.go:36-55) filtered on `BasePR.HeadRefName ==
  "phase/<id>"`. It already answers for github/gitlab/forgejo/gitea and
  returns `ErrBasePRLookupUnsupported` for bitbucket, which ship narrates as a
  skipped lookup and falls through to create. No new provider dispatch.

```
Phase push-gates-on-origin — 3 tasks across 2 waves

Wave 1
  t-1  Extract shared origin gate, add phase pusher
       files:    internal/cmd/basebranch.go, internal/cmd/basebranch_test.go
       covers:   c-1
       contract: If compareOrigin reports a branch that is ahead AND behind
                 as merely ahead, TestPushBaseDivergedNoop (existing) fails —
                 the safety net would push into a diverged base.
                 If pushBranchIfAhead force-pushes or merges a diverged
                 phase/<id> instead of refusing, the new
                 TestPushBranchDivergedRefusesAndNamesForce fails: origin's
                 phase/x tip must be unchanged and the error must name
                 `--force`.
                 If pushBranchIfAhead(force=true) drops --force-with-lease,
                 TestPushBranchDivergedForcePushes fails: after the call
                 origin's phase/x tip == local HEAD.
                 If pushBranchIfAhead gates on the index rather than on
                 origin, TestPushBranchLocalOnlyCommitWithEmptyIndex fails:
                 a committed-but-unpushed commit (clean index) lands on
                 origin and `rev-list origin/phase/x..phase/x` is empty.
                 If pushBranchIfAhead pushes when local == origin,
                 TestPushBranchNotAheadNoop fails (pushed must be false).
                 If the base safety net stops filtering to .dross-only
                 paths after the refactor, TestPushBaseCodeAheadRefusesAndPushesNothing
                 (existing) fails.

  t-2  Status names re-run when record not on origin
       files:    internal/cmd/status.go, internal/cmd/status_test.go
       covers:   c-2
       contract: If recordPending reads the local changes.json instead of
                 `origin/phase/<id>:.dross/phases/<id>/changes.json`,
                 TestStatusShippedFromPRRecordAlone (existing, record IS on
                 origin) loses its `shipped:` line and fails.
                 If suggestNext consults phaseMergeState before the
                 record-pending arm, the new TestStatusPendingRecordNamesRetry
                 fails: with origin/phase/x at a tip whose changes.json has
                 pr=0 and local changes.json pr=42, the suggestion must
                 contain "re-run `dross ship`" and must not contain "merge
                 the open PR"; the `shipped:` line must be absent.
                 If a MISSING origin/phase/<id> ref is treated as pending,
                 TestSuggestNextOpenPRAdvisesTheMerge (existing; fixture
                 never pushes phase/auth) fails.

Wave 2 (depends t-1)
  t-3  Rewrite ship: record-first, gated pushes, late flip
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go, assets/prompts/ship.md
       covers:   c-1, c-2, c-3, c-4, c-5
       contract: c-4 — TestShipRecordPushFailureIsRetried: a pre-receive hook
                 on the bare origin refuses any push whose new tip subject
                 contains "record PR" while a flag file exists. Run 1 must
                 return an error; afterwards state.CurrentPhaseStatus !=
                 "shipped", changes.json Status != "shipped", `dross phase
                 list` prints "  x" not "✓ x", origin's phase/x changes.json
                 has pr=0, and local HEAD subject is "chore(dross): record PR
                 #99 for x" (failed_push_residue: nothing rolled back). Flag
                 removed, run 2 with a clean index: the mock's POST /pulls
                 counter stays at 1, origin's phase/x changes.json pr == 99,
                 state and changes read shipped, local HEAD == origin tip.
                 If the flip is written before the record push, the run-1
                 assertions fail; if the second run gates on the index, the
                 run-2 origin-tip assertion fails; if it re-opens, the POST
                 counter is 2.
                 c-3 — TestShipIsReShippable (existing, extended): POST
                 /pulls counter must be exactly 1 across two runs and the
                 second run's narration must name "#99". If ship ignores
                 changes.json's pr, the counter is 2.
                 c-5 — TestShipAdoptsOpenPRFromProvider: stub
                 ship.OpenPRsTargetingFunc to return one BasePR{Number:77,
                 HeadRefName:"phase/x"}; changes.json starts with pr=0; the
                 mock's POST /pulls must never be hit and origin's phase/x
                 changes.json must carry pr=77. Control cases in the same
                 test: a hit with HeadRefName "phase/other" → POST fires
                 once; ErrBasePRLookupUnsupported → POST fires once and the
                 narration names the skipped lookup. If ship matches on
                 number alone or ignores the head filter, the control fails.
                 c-2 — TestShipDoesNotPersistPRWhenOpenFails (existing):
                 still no `record PR` commit and pr=0 when the provider 500s;
                 additionally state must still read the pre-ship status, not
                 shipped — if the state flip is left before OpenPR, this
                 fails.
                 c-1 — TestShipPushesPRRecordToPhaseBranch (existing): local
                 HEAD == origin phase/x tip after a clean ship — proves the
                 mark-shipped commit is pushed, not left local.
                 c-3 prompt — TestShipPromptNamesReRunAsTheRetry: reads
                 assets/prompts/ship.md; must not contain "Do NOT re-run
                 `dross ship`" and the CI-fix step and Recovery section must
                 contain "re-run `dross ship`" with the phrase "pushes the
                 pending record". If the old line is restored, this fails.
```

## Task detail

### t-1 — Extract shared origin gate, add phase pusher (wave 1)

`internal/cmd/basebranch.go`:
- Extract the fetch + `rev-parse --verify refs/remotes/origin/<b>` + two
  `rev-list` calls from `pushBaseIfAheadDrossOnly` (basebranch.go:62-86) into
  `compareOrigin(repoDir, branch string) (originCmp, error)` with
  `originCmp{exists bool; ahead []string; behind bool}`.
- Rewrite `pushBaseIfAheadDrossOnly` on top of it — identical behaviour (no
  origin ref → no-op; not ahead → no-op; diverged → silent no-op; .dross-only
  filter; hard error on push failure).
- Add `pushBranchIfAhead(repoDir, branch string, force bool) (pushed bool, err
  error)`: no origin ref → `push -u origin <branch>`; not ahead → no-op;
  ahead+behind and !force → error "origin/<branch> has commits this clone
  lacks — `git pull --rebase origin <branch>` then re-run `dross ship`, or
  `dross ship --force` to force-with-lease" (diverged_phase_branch); otherwise
  `push -u [--force-with-lease] origin <branch>` via `gitRefArgs`. Push failure
  surfaces git's output (mirror basebranch.go:103-107).

`internal/cmd/basebranch_test.go`: the four new tests named in the contract,
built on `basePushFixture` + a `phase/x` branch; the diverged fixture follows
`TestPushBaseDivergedNoop`'s upstream-sim pattern (basebranch_test.go:237-267).

### t-2 — Status names re-run when record not on origin (wave 1)

`internal/cmd/status.go`:
- Add `recordPending(repoDir, phaseID string, pr int) bool`: `rev-parse
  --verify --quiet refs/remotes/origin/phase/<id>`; if missing → false.
  `git show origin/phase/<id>:.dross/phases/<id>/changes.json` via `gitTrim`
  + `gitRefArgs`; parse error → false; return `parsed.PR != pr`. Network-free,
  like everything else in status.go.
- `suggestNext` (status.go:~288, inside the `!changes.Complete` block, BEFORE
  the `phaseMergeState` switch): load the record; if `ch.PR > 0 &&
  recordPending(...)` return "re-run `dross ship` — PR #<n> is open but its
  record has not reached origin/phase/<id>".
- `shippedUnmergedPhase` (status.go:597): the guard becomes `if !shippedStatus
  && (sh.pr == 0 || recordPending(repoDir, phaseID, sh.pr)) { return none,
  false }` so the `shipped:` line stays silent on a pending record while the
  fresh-clone shape (record on origin's tip) still raises it.

`internal/cmd/status_test.go`: `TestStatusPendingRecordNamesRetry` built on
`shippedBranchFixture` + `clearShippedStatus`: push phase/x to origin at the
pre-record tip, then commit a local changes.json with pr=42; assert the
suggestion and the silent `shipped:` line. A second case pushes the record
commit too and asserts the retry line is gone (positive-observation guard).

### t-3 — Rewrite ship: record-first, gated pushes, late flip (wave 2, depends t-1)

`internal/cmd/ship.go`, replacing steps 5–8 (ship.go:256-449):
1. Hoist `remotePolicy` + `buildOpenOpts` (ship.go:350-370) above the push so
   opts exist for the lookup.
2. Delete the early state flip and its dead `chore(dross): ship` commit block
   (ship.go:256-287).
3. After the base safety net (ship.go:318-331): `ch, _ := changes.Load(...)`;
   `pr := ch.PR`. If `pr == 0`: `hits, err :=
   ship.OpenPRsTargetingFunc(opts, baseBranch)`; `errors.Is(err,
   ship.ErrBasePRLookupUnsupported)` → narrate "provider cannot list open PRs;
   creating"; other err → return; a hit with `HeadRefName == phaseBranch` →
   `pr, prURL = hit.Number, hit.URL`, narrate "found open PR #n for
   phase/<id>", `commitPRRecord(...)`.
4. Replace the unconditional `git push -u` (ship.go:336-347) with
   `pushBranchIfAhead(repoDir, phaseBranch, forcePush)` — carries any pending
   record commit (c-1 re-run path).
5. If `pr == 0`: `ship.OpenPR` as today (ship.go:371-387) → `commitPRRecord`
   → `pushBranchIfAhead` (replaces the `diff --cached --quiet` gate at
   ship.go:432-448). Else narrate "PR already open: #n (record pushed)".
6. Flip (shipped_timing): `s.CurrentPhaseStatus = "shipped"`,
   history-guarded `Touch`, `Save`; `changes.SetStatus(root, phaseID,
   changes.StatusShipped)`; `git add` changes.json; commit "chore(dross): mark
   <id> shipped" only when staged; `pushBranchIfAhead`. Then the existing
   "Marked shipped — once the PR merges…" narration.
7. `commitPRRecord(repoDir, root, phaseID string, pr int, base string) error`:
   `changes.SetPR` + `changes.SetBase` + `git add` + commit "chore(dross):
   record PR #n for <id>" when staged. Telemetry/`--json`: `res` for a re-run
   is `&ship.OpenResult{Number: pr, URL: prURL}` so `shipResultTag` and the
   JSON object keep their shape; add tag `existing=true` on the re-run path.

`assets/prompts/ship.md`: line 134 becomes "`git push origin phase/<id>` —
appends to the open PR; or re-run `dross ship`, which recognises the open PR,
pushes the pending record and never opens a second one. If you rebase or
amend, `dross ship --force`." Recovery item 3 (line 200) becomes "**Record
push failed after the PR opened.** `dross ship` errored on the PR-record push;
status reads verified and names the retry. Fix: re-run **`dross ship`** — it
recognises the open PR and pushes the pending record."

`internal/cmd/ship_test.go`: tests as named in the contract; the refusing
pre-receive hook follows `TestPushBaseRejectedPushSurfacesGitOutput`
(basebranch_test.go:298+) with an added `git log -1 --format=%s $newrev |
grep -q "record PR"` + flag-file check; the provider stub saves/restores
`ship.OpenPRsTargetingFunc` via `t.Cleanup`.

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (gate helper, origin comparison), t-3 (ship calls it for every push; existing record test proves local == origin) |
| c-2 | t-3 (flip after record push; changes.Status untouched until then → phase list + watch read verified for free via `phaseIsDone`/`stateHasShipped`), t-2 (status names the retry) |
| c-3 | t-3 (record-first re-run, POST counter test, prompt rewrite + prompt guard test) |
| c-4 | t-3 (`TestShipRecordPushFailureIsRetried`) |
| c-5 | t-3 (provider fallback via `OpenPRsTargetingFunc` head filter, `TestShipAdoptsOpenPRFromProvider`) |

Every criterion accounted for; every task traces to at least one.

## Judgment calls

- **Reused `OpenPRsTargetingFunc` + head filter for c-5** instead of a new
  `FindOpenPRByHead` dispatch across four providers (~4 files, 4 test files).
  The existing lookup already returns `HeadRefName` for every provider that
  can answer, and bitbucket's `ErrBasePRLookupUnsupported` is an announced
  skip → create, which is the same fall-through the criterion allows.
- **`status=shipped` leaves the record commit and rides a second, later
  commit** rather than staying in the record commit with readers taught to
  distrust it. The locked shipped_timing says changes.json flips only after
  the push; the only way to honour that with a tracked file is a post-push
  write. Rejected: leaving that write uncommitted (dirty tree breaks
  `TestShipIsReShippable`) and leaving it unpushed (breaks
  `TestShipPushesPRRecordToPhaseBranch`'s local==origin assertion and nags
  "ahead by 1" on every shipped branch). Cost: one extra push per ship.
- **Record-pending is a positive observation on origin's tip**, not
  "local ahead of origin". Rejected the rev-list form because the fresh-clone
  shape (`TestStatusShippedFromPRRecordAlone`) and the never-pushed fixtures
  (`terminalPhaseFixture`) would both misread; reading
  `origin/phase/<id>:changes.json` answers the exact question c-4 asserts.
- **No watch / phase-list / phasedone code change.** `watch/drift.go:109`
  already requires `stateHasShipped && phaseHasOpenPR`, and `phaseIsDone`
  reads `changes.Status` — moving the writers is enough; adding readers would
  be speculative structure. c-2's watch/phase-list halves are pinned by t-3's
  failure test asserting `phase list` output and `changes.Status`.
- **Prompt edit folded into t-3**, not its own task: one file, minutes of
  work, same criterion (c-3), and the guard test lives next to
  `TestPromptsNeverStageStateJSONByPath` in ship_test.go.
- **t-2 runs in wave 1 alongside t-1** — it reads local refs only and never
  calls the new pusher; its fixtures build the origin state by hand.
- **Rejected a `record pending` marker in state.json** — the locked
  shipped_timing forbids a second state; the origin comparison is the marker.
