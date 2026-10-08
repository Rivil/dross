# Plan Review — push-gates-on-origin

Reviewed: 2026-09-11
Plan: 7 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [antipattern/precision] t-3's derived "record pending" shape (`ch.PR > 0 && ch.Status != StatusShipped && !Complete`) and its shipped signal (`state shipped || ch.Status == StatusShipped`) are both true for a legacy record: state.json shipped, changes.json pr set, no status field (phases shipped before `Status` existed). The description says pending prints "instead of the shipped line" but never states which signal wins in that overlap, and the only fixture (contract line 1) clears state status, so the overlap is untested. Read literally, every legacy shipped-not-yet-complete phase would tell the user to re-run `dross ship`.
  Suggestion: state the precedence in the description (pending requires `!stateShipped`, i.e. state.json shipped outranks a missing changes.json status) and add one contract line: state shipped + {pr:42, no status} prints `shipped:` and not the retry.

- [locked-decision/consequence] t-6 stage (f) adds a third push (the `mark <id> shipped` commit) with its own failure state: state.json and changes.json read shipped locally, marker unpushed, error text "phase IS shipped, re-run to push the marker". That is a second retry state alongside c-4's, which the `shipped_timing` why ("a second marker would be two states to keep honest") was written to avoid. It does not contradict the locked choice (the flip is still one point after the record push), and it follows from `failed_push_residue` making status unable to ride the record commit — but nothing downstream reads origin's `status` field (mergeGate reads `pr`; phaseDone reads local), so a hard non-zero error for that push is arguably louder than the state warrants.
  Suggestion: either downgrade the (f) push failure to a warning (the next ship or complete carries it), or keep the error but make the contract explicit that this is a deliberate second state and why it cannot lie. Also: when the (f) push refuses on divergence (review commits pushed elsewhere, then re-run), "re-run `dross ship <id>`" alone loops on the same refusal — the error must surface pushPhaseBranch's pull/--force guidance, not only the re-run.

- [antipattern/precision] t-6 stage (b) claims "a diverged branch refuses BEFORE any PR exists", but pushPhaseBranch as specified in t-1 treats behind-only (origin ahead, local has nothing new) as a silent no-op. On a first ship in that shape the PR opens against origin's tip, then the record commit at (d) makes the branch Ahead&&Behind and (e) refuses — after the PR exists. Recoverable (pull, re-run hits the existing-PR arm) but it is exactly the ordering the stage claims to prevent.
  Suggestion: decide whether behind-only at (b) should refuse (local must be up to date to ship) or is accepted; write it into t-1's description and add a behind-only contract line to t-1 either way.

- [granularity] t-6 existing-PR arm reports a number only: changes.json has no URL field (`internal/changes/changes.go` — pr, base, status; no url), so `--json` on a re-run emits an empty `url` and the narration cannot print one. ship.md §5/§6 drive `gh pr checks <pr-url>` and `gh pr merge <pr-url>` off the URL. c-3 says "reports the existing PR", so a number is within spec, but t-4's rewritten prompt should not imply a URL comes back on re-run.
  Suggestion: add a contract line to t-6 pinning the `--json` shape on re-run (`url` empty or derived), and have t-4's re-run guidance say the number is reported and the URL is the one from the first run / the provider.

- [granularity] t-5 touches 5 files, and the gitlab and bitbucket halves share nothing but the two sentinel arms in headpr.go. As one task it serialises two independent provider implementations.
  Suggestion: split into t-5a gitlab / t-5b bitbucket, both wave 2, both depending on t-2; the headpr.go arm swap is a one-line edit each.

- [granularity] t-6 is the phase's centre of gravity: six ordered stages, 13 contract lines, a `--json`/telemetry arm and an existing-PR narration arm on top of the gate-and-flip restructure. It is one file pair, so it does not trip the file-count rule, but it is the task most likely to stall mid-way in pair mode.
  Suggestion: consider peeling stage (c) + shipResultTag's `existing` arm + TestShipJSONReportsExistingPR into a follow-on task after the restructure lands; the c-4 second-run test needs (c), so the split would have to keep (c) minimal in t-6 and move only the reporting surface. Acceptable to leave as-is if the executor prefers one commit for the reorder.

- [wave-order] t-4 (wave 1) rewrites ship.md to describe a ship that does not exist until t-6 (wave 2), and r-01's `make install` at t-4 time re-links that prompt into ~/.claude. For the commits between t-4 and t-6 the installed prompt tells the user re-running ship is safe when the installed binary still opens a second PR.
  Suggestion: move t-4 to wave 2 with `depends_on = ["t-6"]` (or wave 3), or accept with the understanding that nothing ships mid-phase.

## NOTE
- [wave-order] t-7's `depends_on` includes t-5, but t-7 consumes only the seam (`FindOpenPRByHeadFunc`) and the sentinel (`ErrHeadPRLookupUnsupported`), both from t-2; every t-7 test stubs the func. The extra dependency is harmless (t-6 already forces wave 3) but is not a true need.
- [precision] t-1 leaves the `force` semantics unspecified for the in-sync and behind-only classifications: does `--force` push a level branch, or only rescue the diverged one? Every t-6 stage calls pushPhaseBranch with `forcePush`, so this decides whether `dross ship --force` on a clean re-run issues three pushes or none.
- [coverage] All five criteria are covered: c-1 (t-1, t-6), c-2 (t-3, t-6), c-3 (t-4, t-6), c-4 (t-6), c-5 (t-2, t-5, t-7). Both files the plan creates (`internal/cmd/originpush.go`, `internal/ship/headpr.go`) are absent today and introduced by wave-1 tasks; every other referenced file exists.
- [forbidden-actions] No violations. runtime.mode is native with `go test`; t-4 names `make install` per r-01; no global rules file exists.
- [strength] Every line-number and identifier reference was checked against the tree and holds: basebranch.go:63-86, ship.go:269-449 and the `diff --cached` gate at 432, status.go:295-311 and 575-600, ship.md:107-115/134/190-202, all 30+ named tests and seams (shipFixture:110, writeOracleChanges:1333, stubPRMerged:29, hostpolicy_test.go:125, forgejoOpenPRsTargeting:122, gitlabReq:195, bbRequest:50, bbCredentials:88). The plan was written against the code, not from memory.
- [strength] Test contracts are mutant-aware throughout: each names the specific wrong implementation that must fail (state flipped before push, SetStatus riding the record commit, lookup-first instead of record-first, helper skipping the fetch, nil-nil on lookup failure). The shared-helper proof in t-1 (one inverted-Behind mutant must break both TestPushBaseDivergedNoop and the new phase-branch test) is a direct test of the `shared_origin_gate` decision rather than a claim about it.
- [strength] t-7 anticipates a real execution trap: the hand-rolled forgejo mocks in ship_test.go answer `{}` to any unmatched GET, which the new by-head lookup would misparse; installing a default `stubOpenPRByHead(t, nil, nil)` in shipFixture with t.Cleanup restore keeps every existing ship test green without touching their mocks.

## Summary
No blockers: coverage is complete, every locked decision is honoured, and the references are real; the flags are precision gaps (legacy-record precedence in t-3, behind-only ordering at stage (b), the third-push failure state) and two granularity choices the author can take or leave.
