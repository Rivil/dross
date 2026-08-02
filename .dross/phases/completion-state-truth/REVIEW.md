# Plan Review — completion-state-truth

Reviewed: 2026-08-02 (second pass)
Plan: 8 tasks across 3 waves

## BLOCKING

- [locked-decision / correctness] **t-1's completion write will silently truncate live
  history on the `--recover` path.** t-1 places the write "after the fast-forward
  succeeds and BEFORE the local and remote branch deletions" and says nothing about
  where the `*state.State` comes from. The only `s` in scope at that point is the one
  loaded at `internal/cmd/phase.go:273` — before `autoCommitDrossDirt`, before the
  checkout, before the ff, and before `runDrossRecovery`. On the `--recover` branch
  (phase.go:443-482) recovery reloads state from disk into `rs` and then does
  `rs.Touch("merged <id>")` + `rs.Save` (`internal/cmd/ship_recover.go:218-219`).
  Saving the stale top-of-RunE `s` after that drops `merged <id>` and anything else
  written to the live file since line 273. This is the exact hazard the existing code
  already documents in prose at phase.go:451-456 ("a stale in-memory State would have
  its history written back over the live file by runDrossRecovery's own s.Save") — and
  it is the same failure class (live state.json loses entries) that this whole phase
  exists to close. No test_contract item in t-1 drives `--recover`, so nothing catches it.
  Suggestion: make t-1's description state that the write re-loads state from disk
  immediately before writing (or reuses `rs` when recovery ran), and add a contract
  item: `complete --recover` on a diverged base leaves BOTH `merged <id>` and
  `completed <id>` in live history, and the pre-existing entries intact.

## FLAG

- [test contract / t-1] Description and contract disagree on ship's touch. The prose
  says ship writes "a `shipped <id>` touch" with no dedupe; `TestShipIsReShippable`
  requires "one `shipped <id>` entry, not two" after a second ship. The `completed <id>`
  write is explicitly described as "guarded by a history scan" — the `shipped` one isn't.
  Suggestion: say the ship touch is history-scan-guarded too, or drop the count assertion.

- [test contract / t-1] The `ship_test.go:668` premise is wrong in a way that matters.
  That needle lives at ship_test.go:676 inside `TestShipJSONEmitsSingleObjectAndSuppresses
  Narration`'s loop over `[]string{"Pushed", "PR opened", "Completion record folded"}` —
  it asserts the string is **absent** under `--json`. It does not "assert the opposite";
  it is a negative assertion that goes silently **vacuous** once ship stops emitting the
  string, rather than failing. "Inverted here, not deleted" will lead the implementer to
  expect a red test that never appears, and the JSON-suppression check quietly loses a
  third of its coverage.
  Suggestion: swap the needle for the new narration string so the suppression test keeps teeth.

- [test contract / t-1] t-8's grep guard scans `internal/cmd/*.go`, which includes test
  files — and `folds the completion` / `folded into the squash` both currently hit
  `internal/cmd/ship_test.go` (the comment blocks at ~:305 and ~:330). t-1 describes
  inverting assertions but never says to rewrite those comments. t-8 fails until they are.
  Suggestion: name the ship_test.go comment rewrite in t-1's description.

- [t-6] `shippedUnmergedPhase` may claim a merged PR is still waiting. The function it
  replaces (`staleCompletedState`, status.go:491-530) deliberately returns false once the
  branch is on the base — ancestry check, then a squash-aware `resolveSquashCommit`
  content check. t-6 re-keys it on `current_phase_status == "shipped"` with the PR number
  as fallback and is silent on whether the merged-check survives. Every contract fixture
  is "unmerged on origin", so the post-merge / pre-complete window — which this phase
  deliberately makes reachable and longer-lived — is untested. In it, status would print
  a `shipped:` line naming "the base it waits on" for a PR that already landed.
  Suggestion: state whether the merged-check is retained, and add a sub-case: merged on
  origin but complete not yet run.

- [test contract / t-6] The description instructs a `spineIdle` change ("check `spineIdle`
  in the same edit — it now sees a shipped phase as in-phase idle") with no contract item
  covering it. Reading status.go:275-306, a shipped phase with a finalized `pass` verdict
  and no runnable/failed task already returns true, so the edit may be a no-op — but
  nothing in the plan establishes which.
  Suggestion: either add an assertion (actions block still renders on a shipped phase) or
  drop the instruction.

- [test contract / t-8] The grep guard's phrase family is tuned to paraphrase, not to the
  tree. Six of seven needles do hit today (verified: `folds the completion` → phase.go:278
  + ship_test.go; `folded into the squash` → ship_test.go; `folded into state history by
  ship` → drift.go:25; `squash-merge will land it` → ship.go:339; `carries the completion
  record to main` → ARCHITECTURE.md; `records the merge in state.json with a chore commit`
  → ship.md:152; `rides the squash` matches nothing). But the two remaining live claims
  match **no** needle because they wrap across comment lines: phase.go:507-511 ("already
  folded the cleared / current_phase + \"completed <id>\" record into the squash") and
  status.go:70-71 ("has already folded the `completed <id>` record into / branch-local
  state"). Both blocks are deleted by t-1/t-6, so re-adding their exact original text
  would pass the guard.
  Suggestion: collapse whitespace before scanning (the `strings.Fields` idiom already used
  at ship_prompt_test.go:161) and add a needle matching the "folded the cleared
  current_phase" wording.

- [t-8 / stale claim not caught] `internal/telemetry/telemetry.go:294` says the
  shipped-but-unmerged window is where "branch-local state records `completed <id>`".
  After t-1 it records `shipped <id>` — so this becomes a false claim about which command
  writes what, which is squarely c-3's second clause. It matches no needle, and
  telemetry.go appears in t-8's scan list but not in t-8's `files`, so the task as written
  cannot fix it. (`internal/changes/changes.go:138` is in the same position but its claim
  stays true.)
  Suggestion: add telemetry.go to t-8's `files` and name the sentence.

- [granularity / t-8] 7 files spanning Go source, two prompts, ARCHITECTURE.md and a new
  test file — over the 5-file line and 3+ layers. It also mixes a pure prose sweep with a
  behavioural change (quick.md's shipped-window routing rule, which decides where a quick
  fix lands and is the only genuinely new logic in the task).
  Suggestion: split the quick.md routing rule out; it is the one part with a real failure
  mode if it lands wrong.

- [antipattern / naming] t-7 creates `internal/cmd/completion_state_truth_test.go` and t-8
  creates `internal/cmd/completion_truth_test.go` — near-identical names in one package,
  one an incident reproduction and one a grep guard.
  Suggestion: rename t-8's to something naming what it does (e.g. `squash_claim_guard_test.go`).

## NOTE

- [coverage] Complete. c-1 → t-3, t-4; c-2 → t-1; c-3 → t-1, t-4, t-8; c-4 → t-7;
  c-5 → t-2, t-5, t-6; c-6 → t-6.

- [locked-decision] The `teardown_owner` widening to Forgejo/GitLab is consistent with the
  decision's own `why` ("restores one behaviour across every provider") even though its
  `choice` text names only GitHub, and t-4 declares the gitlab-ship-provider c-4
  supersession openly. The pinned needle is real and correctly located:
  `internal/cmd/ship_prompt_test.go:147` asserts `shouldremovesourcebranch`, and t-4 lists
  that file. Recorded as deliberate; not re-litigated.

- [wave order] No violation found. t-4→t-3 is a genuine output dependency (its
  `TestShipPromptCommandsExist` resolves `dross phase checkout` against the cobra tree).
  t-5/t-6→t-2 is a genuine dependency on the renderer. t-5/t-6/t-8→t-1 is partly file
  contention (phase.go, status.go, ship.go) rather than output, but those tasks are in
  wave 2+ on other grounds anyway.

- [verification] Every cited helper and line reference checked and present:
  `incidentRepo` state_clobber_regression_test.go:28, `stubPRMerged` phase_test.go:25,
  `hasAction`, `incidentLiveEntries` = 12, `resolveNewWorkBase` basebranch.go:159 (self-
  contained: loads project + state itself), `checkoutBranch` → `guardLiveState`
  switchbranch.go:36/88, `newRoot` cmd/dross/main.go:16, quick.md:19's in-phase branch rule,
  ship.md:19 / :148 / :150 / :152 / :191. The only raw `git checkout` left in non-test Go
  is ship_recover.go:181, a path-scoped tree restore that already excludes state.File — so
  c-1's remaining hole really is the prompt.

- [verification] t-1's phase_test.go fixture line numbers drift by 4-12 and omit three call
  sites: actual `foldCompletion` at 458, 1149, 1240, 1372, 1877, 2092 and `writeCompletion`
  at 1388. Cheap to re-locate; noted so the implementer greps rather than trusting the numbers.

- [fixture risk] t-1's `TestCompleteRecordsBeforeTeardown` needs a server-side `update` hook
  on a bare origin to reject the branch deletion. There is no precedent for git hooks
  anywhere in the test suite. Workable, but a simpler forced failure exists if it fights back.

- [t-3 collision] Registration lands at phase.go:28, far from t-1's 211-518 and t-5's ~518 —
  the "does not collide" claim holds. No doc-parity or command-enumeration test requires a
  new subcommand in README or docs/dross.1, so t-3 will not trip a guard elsewhere.

- [out of scope, worth knowing] README.md:271 and docs/dross.1:56 both describe
  `dross phase complete` as fast-forwarding **main** — the pre-recorded-base wording. Neither
  claims the record rides the squash, so neither violates c-3, and neither is in any task's
  scope. Flagging only because they are the first surface a reader hits.

- [strength] t-7's scope-honesty paragraph is the best thing in the plan: it states plainly
  that no Go test can execute `gh pr merge --delete-branch`, that the main assertion fails
  pre-phase for a different reason than the incident, and that the executable guard is t-4's
  prompt grep. It then mandates a control sub-test so the assertions aren't vacuous.

- [strength] t-2's `workOverride` parameter and t-5's insistence on passing
  `resolveCompleteBase`'s answer in — rather than letting the helper re-infer — correctly
  preserves the previous phase's central lesson (resolveCompleteBase's doc comment,
  phase.go:529-542).

- [strength] t-1 names its blast radius (existing assertions and fixture comments that
  encode the old premise) as expected work rather than leaving it to be discovered mid-task.

## Summary

Structurally sound and unusually well-grounded in the actual code, but t-1's completion
write is specified without saying where the state object comes from, which reintroduces the
live-history-truncation failure on the `--recover` path that this phase exists to eliminate —
fix that before proceeding.
