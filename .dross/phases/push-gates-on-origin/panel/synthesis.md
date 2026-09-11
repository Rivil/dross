# Synthesis — push-gates-on-origin

Judge: cold merge of the risk / mvp / verification drafts. Every file, function
and test name cited by the three drafts was checked against the tree; all
resolve (ship.go:269 flip, :336-347 push, :371 OpenPR, :422 SetStatus, :432
index gate, :440-447 record push; basebranch.go:62-86; status.go:283-311 and
:575-600; basepr.go:36-55; forgejo.go:122-166; gitlab.go:135-173; bitbucket.go:50/88/112;
drift.go:109; phasedone.go:50; hostpolicy_test.go:125; phase_test.go:29;
gharg_test.go:131; the 12 named ship_test.go / status_test.go / basebranch_test.go
tests). Two draft defects: verification marks `internal/cmd/ship_prompt_test.go`
as "(new)" — it exists (TestShipPromptRecoverySection, line 36); risk's t-3
edits `headpr.go`, a file its own t-2 creates, but declares `depends_on: []`
and sits in wave 1 (a wave-correctness defect).

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk | 5/5 criteria + all 5 locked decisions mapped to an owner; c-5 real on all four providers | Highest volume: every task names mutant → failing test, with fixture recipes (message-selective pre-receive hook, marker-file hook); one fixture cite off by ~25 lines (status_test.go:881 vs 855) | 8 tasks; ship.go is edited in three serial tasks (t-6 → t-7 → t-8) so t-7's resolution code is written against the old flip order and then restructured by t-8 — churn; t-8 alone owns c-1, c-2, c-4 | 4 waves — deepest; t-3 → headpr.go dependency on t-2 missing (both wave 1); otherwise sound |
| mvp | 5/5 nominally, but c-5 is hollow on Bitbucket (`OpenPRsTargetingFunc` → unsupported → create, i.e. the duplicate the criterion forbids) and capped at gh's default 30 rows on GitHub; c-2 designed around an origin-tip read | Good: named tests with mutant statements; missing a ship-level diverged re-run test and a lookup-error-fails-closed test | 3 tasks; t-3 is a monolith (ship.go rewrite + prompt + 5 criteria + 6 tests) — one red test blocks the whole phase's atomic-commit cadence | 2 waves, dependencies correct; t-2 legitimately wave 1 given its origin-read design |
| verification | 5/5 criteria + all locked decisions, plus a source-pin test for shared_origin_gate's "neither carries its own copy"; c-5 real on all four providers; also catches ship.md:107-115 (the CLI step list, which lists the state write before the push) which the others miss | Highest precision: `-u` upstream assertion, level-branch + always-refuse hook proving "no push", pagination fixture, unknown-provider vs sentinel distinction; one file mislabelled new | 7 tasks; t-6 is large but is ONE coherent restructure of ship.go, t-7 layers the lookup on top — no rework between them; provider work split by transport with a real dependency | 3 waves, every dependency real (t-5 → t-2, t-7 → t-2 + t-6); t-3/t-4 in wave 1 justified (they seed shapes directly) |

**Skeleton: verification.** Same coverage as risk, tighter waves (3 vs 4), no
serial rework on ship.go, correct t-5 → t-2 dependency that risk drops, and the
ship.md step-list fix nobody else saw. Risk is the primary graft source (its
contracts are sharper in places); mvp contributes two contract lines and the
structural alternatives recorded under Disagreements.

## Merged plan

```
Phase push-gates-on-origin — 7 tasks across 3 waves

Wave 1
  t-1  Extract shared origin gate; add phase-branch push     [verification+risk+mvp]
       files:    internal/cmd/originpush.go (new), internal/cmd/originpush_test.go (new),
                 internal/cmd/basebranch.go
       covers:   c-1; locked shared_origin_gate, diverged_phase_branch
       depends:  []
       description:
                 Lift basebranch.go:63-86 (fetch, rev-parse origin ref, two rev-lists) into
                 compareWithOrigin(repoDir, branch) (originDelta{Missing bool; Ahead []string;
                 Behind bool}, error); pushBaseIfAheadDrossOnly keeps its exact policy (.dross-only
                 filter, diverged → silent no-op, hard error on push failure) but consumes the
                 delta. Add pushPhaseBranch(repoDir, branch string, force bool) (pushed bool, err
                 error): Missing → `push -u origin <b>`; Ahead && !Behind → `push -u origin <b>`;
                 Ahead && Behind && !force → guided error naming `git pull --rebase origin <b>`
                 and `dross ship --force`; force → `push -u --force-with-lease`. All argv via
                 gitRefArgs (the subprocargs audit test rejects anything else).
       contract:
         - [verification] Local phase/x one commit ahead of origin/phase/x with a CLEAN index →
           pushPhaseBranch pushes; `git rev-list origin/phase/x..phase/x` empty afterwards. A gate
           reading `git diff --cached` (today's ship.go:432) pushes nothing and fails here.
         - [verification] Level branch + a pre-receive hook refusing everything → pushed=false,
           err=nil (an always-push mutant errors).
         - [verification+mvp] Diverged fixture (TestPushBaseDivergedNoop recipe on phase/x) →
           err mentions "git pull" and "--force", origin tip unchanged; same fixture force=true →
           origin tip == local HEAD.
         - [verification] origin/phase/x missing → push succeeds AND `git rev-parse --abbrev-ref
           phase/x@{upstream}` == origin/phase/x (the -u is load-bearing for the prompt's later
           `git push`).
         - [risk] TestOriginCmpSeesUnfetchedRemoteCommit: a commit pushed to origin from a second
           clone classifies Behind; a helper that skips the fetch reads in-sync and fails.
         - [risk] TestOriginCmpMissingRemoteRef: a never-pushed branch → Missing=true, err == nil
           (the first-ship case); swallowing it as an error fails.
         - [verification] Shared-helper proof: a mutant inverting Behind fails BOTH the existing
           TestPushBaseDivergedNoop (basebranch_test.go:237) AND the new
           TestPushPhaseBranchDivergedRefuses. TestPushBaseDrossOnlyAheadPushes, TestPushBaseNotAheadNoop,
           TestPushBaseCodeAheadRefusesAndPushesNothing, TestPushBaseRejectedPushSurfacesGitOutput and
           TestPushBaseUnreachableOriginHardError stay green unchanged.
         - [verification] Source-pin: a test reads internal/cmd/basebranch.go and fails if it still
           contains a `rev-list` argv of its own (the "neither carries its own copy" half of the lock).

  t-2  Add FindOpenPRByHead seam: github + forgejo            [verification+risk]
       files:    internal/ship/headpr.go (new), internal/ship/headpr_test.go (new),
                 internal/ship/forgejo.go, internal/ship/forgejo_test.go
       covers:   c-5 (provider half)
       depends:  []
       description:
                 FindOpenPRByHead(opts OpenOpts, head string) (*OpenResult, error) dispatching like
                 OpenPRsTargeting (basepr.go:36-49); exported FindOpenPRByHeadFunc seam (merged.go:53
                 pattern); ErrHeadPRLookupUnsupported sentinel returned by the gitlab/bitbucket arms
                 until t-5 lands. Not-found is (nil, nil); any lookup failure is a non-nil error, never
                 nil-nil. GitHub: `gh pr list --head <h> --state open --json number,url`, head in the
                 value slot of --head. Forgejo: pull the page loop out of forgejoOpenPRsTargeting
                 (forgejo.go:122-164) into forgejoListOpenPRs(opts) returning raw items; both callers
                 filter on it (base.ref vs head.ref).
       contract:
         - [verification+risk] ghCommand fake asserts by index that argv[0..1] == pr,list, the token
           after "--head" is exactly "phase/x", and "--state" is followed by "open" (TestOpenPRArgvWalk
           style, gharg_test.go:131); fake prints `[{"number":7,"url":".../pull/7"}]` → Number == 7.
         - [verification] Fake prints `[]` → (nil, nil). Fake exits 1 → err != nil, result == nil
           (a lookup that swallows failure into nil-nil lets ship open a duplicate on a flaky network).
         - [verification] Forgejo httptest serves two pages of 50: page 1 only head.ref "other",
           page 2 holds head.ref "phase/x" number 12 → Number == 12; no match → (nil, nil);
           HTTP 500 → error.
         - [risk] TestForgejoHeadLookupExactRef: heads "phase/x" and "phase/x-2" both open → only the
           phase/x PR is returned (substring/prefix matching fails).
         - [risk] TestForgejoHeadLookupQueriesOpenOnly: the mock asserts state=open in r.URL.Query().
         - [verification] TestForgejoOpenPRsTargetingFiltersByBaseRef and TestForgejoOpenPRsPaginates
           (forgejo_test.go:161, 251) stay green through the shared lister.
         - [verification] FindOpenPRByHeadFunc defaults to FindOpenPRByHead (mirror of
           TestOpenPRsTargetingFuncDefaultsToOpenPRsTargeting, basepr_test.go:143).

  t-3  Status reads shipped from the record, names retry     [verification+risk]
       files:    internal/cmd/status.go, internal/cmd/status_test.go, internal/watch/drift_test.go
       covers:   c-2 (status / phase list / watch consumers)
       depends:  []
       description:
                 Derived "record pending" shape: ch.PR > 0 && ch.Status != changes.StatusShipped &&
                 !changes.Complete — no new field or marker. shippedUnmergedPhase (status.go:575-600):
                 the shipped signal becomes `state shipped || ch.Status == StatusShipped`; the pending
                 shape prints "pending: phase/<id> — PR #N is open but its record has not reached
                 origin; re-run `dross ship <id>`" instead of the shipped line. suggestNext
                 (status.go:295-311): split the phaseMergeState consult so the order is mergeMerged →
                 pending-retry arm ("`dross ship <id>` — push the PR record for #N") → mergeOpen
                 (see Disagreement 4). drift.go unchanged; pin it with a test.
       contract:
         - [verification+risk] Fixture: phase/x checked out, changes.json {pr:42, base:main, no
           status}, state status cleared → `dross status` contains "re-run `dross ship x`" and NOT
           "shipped:"; suggestFor returns a string containing "dross ship x" and not "merge the open
           PR". Today (status.go:597-600 keys on pr != 0) prints the shipped line.
         - [verification+risk] TestStatusShippedFromPRRecordAlone (status_test.go:993) re-seeded with
           changes.json status="shipped" (the tracked flip is what a fresh clone actually carries);
           must still print "shipped:" and "#42".
         - [risk] TestSuggestNextMergedLegacyRecordStillCompletes: {pr:42, status:""} with phase/x
           already in origin/main's ancestry → next step names `dross phase complete`, not ship
           (a retry arm ordered before the merged consult fails this).
         - [verification] Control: same fixture with changes.json status="complete" → neither line
           (Complete suppressor unchanged).
         - [verification+risk] watch pin (TestDriftPROpenButStateNotShipped): state {} + changes
           {pr:42, no status} + verdict pass → DriftVerifiedUnshipped, ok=true; state shipped +
           changes status shipped + pr 42 → no drift.
         - [verification] TestStatusCountsShippedPhaseDone (status_test.go:370) still counts a
           status="shipped" record as done through phaseDone (phasedone.go:50).
         - [judge, from mvp's observation] TestSuggestNextOpenPRAdvisesTheMerge (status_test.go:1582)
           is seeded by writeOracleChanges (status_test.go:1333) as {pr:42, base:main} with NO
           status — under the derived design that IS the pending shape, so the retry arm fires and
           the test goes red. Treatment: writeOracleChanges gains a status argument (or the
           terminalPhaseFixture call site writes status="shipped"), the same re-seed as
           TestStatusShippedFromPRRecordAlone; the arm is never weakened to make it pass.
           TestSuggestNextMergedPRAdvisesCompletion and TestSuggestNextNoPRFallsThrough (same
           fixture) must stay green through the re-seed.

  t-4  Replace ship prompt's do-not-re-run guidance          [verification+risk]
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go (exists)
       covers:   c-3 (prompt half)
       depends:  []
       description:
                 ship.md:134 — replace "Do NOT re-run `dross ship` (would open a second PR)" with:
                 re-running `dross ship` is safe — it recognises the open PR, pushes anything pending
                 on phase/<id>, and reports the existing PR; `dross ship --force` is the path when ship
                 refuses a diverged branch. ship.md:107-115 — the CLI step list becomes: gate phase/<id>
                 on origin, open the PR (skipped when the record or the provider already has one),
                 commit + push the PR record, THEN mark shipped. Recovery (ship.md:190-202): add item 4
                 "Record push failed": status reads verified and names the retry; run `dross ship
                 <phase-id>` again. rules.toml r-01: `make install` after the edit.
       contract:
         - [verification+risk] A test reads assets/prompts/ship.md via repoRootFromTest and fails on
           strings.Contains(content, "Do NOT re-run") or "would open a second PR"; fails unless the
           §5 On-failure block contains "re-run `dross ship`" (or "re-running `dross ship`"); fails
           unless the Recovery section names "record push" and `dross ship` — asserted by section-
           slicing, the way TestShipPromptRecoverySection does.
         - [risk] Existing TestShipPromptRecoverySection keeps its required phrases; dropping
           "dross phase complete --recover" or "dross ship recover" fails it.
         - [verification] TestPromptsNeverStageStateJSONByPath (ship_test.go:1412) stays green.

Wave 2
  t-5  Add gitlab + bitbucket by-head lookups                  [verification+risk]
       files:    internal/ship/headpr.go, internal/ship/gitlab.go, internal/ship/bitbucket.go,
                 internal/ship/gitlab_test.go, internal/ship/bitbucket_test.go
       covers:   c-5 (provider half, remaining providers)
       depends:  [t-2]
       description:
                 Swap the two sentinel arms for gitlabOpenMRBySource (GET /projects/<ref>/merge_requests
                 ?state=opened&source_branch=<h> via gitlabReq, gitlab.go:195; iid → Number, web_url →
                 URL) and bbOpenPRBySource (GET /repositories/<ws>/<slug>/pullrequests?q=
                 source.branch.name="<h>" AND state="OPEN" via bbRequest + bbCredentials,
                 bitbucket.go:50/88; values[0].id → Number, links.html.href → URL). Keep the sentinel
                 declared for the deferred azure-devops provider.
       contract:
         - [verification+risk] GitLab httptest asserts the raw query carries source_branch=phase%2Fx
           and state=opened, plus the auth header for both private-token and bearer schemes;
           `[{"iid":5,"web_url":"..."}]` → Number 5; `[]` → nil,nil; 500 → err.
         - [risk] TestGitLabHeadLookupMultiple: two MRs share the source branch with different
           targets → the one whose target_branch == opts.BaseBranch is returned.
         - [verification+risk] Bitbucket httptest asserts the q query decodes to contain
           source.branch.name="phase/x" and state="OPEN", and that Basic auth carries auth_user;
           `{"values":[{"id":9,...}]}` → Number 9; `{"values":[]}` → nil,nil; missing auth_user →
           error (bbCredentials path).
         - [risk] TestGitLabHeadLookup401IsError / TestBitbucketHeadLookup401IsError: a 401 is
           (nil, err), never (nil, nil).
         - [verification] TestOpenPRMessageListsShipProviders-style check: an unknown provider string
           still returns the "unsupported provider" error, distinguishable from
           ErrHeadPRLookupUnsupported.

  t-6  Gate record push on origin; flip shipped after         [verification+risk+mvp]
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-1, c-2, c-3 (CLI half), c-4; locked shipped_timing, failed_push_residue,
                 diverged_phase_branch
       depends:  [t-1]
       description:
                 Restructure ship.go:269-449 into ordered stages, all after the base safety net:
                   (a) load changes.json; existingPR := ch.PR.
                   (b) pushPhaseBranch(repoDir, phaseBranch, forcePush) replaces the unconditional
                       push at 336-347 (a diverged branch refuses BEFORE any PR exists).
                   (c) existingPR == 0 → ship.OpenPR as today; else res = {Number: existingPR},
                       narrate "PR #N already open — pushing the pending record", shipResultTag
                       gains an "existing" arm, --json emits the existing number.
                   (d) SetPR/SetBase (no SetStatus) → git add → commit "chore(dross): record PR #N
                       for <id>" only if staged.
                   (e) pushPhaseBranch again — hard error on failure, wording ends "re-run `dross
                       ship <id>` to push the record"; the record commit is left in place.
                   (f) only now: state.CurrentPhaseStatus = "shipped" + history-guarded `shipped <id>`
                       + Save; changes.SetStatus(StatusShipped) → commit "chore(dross): mark <id>
                       shipped" if staged → pushPhaseBranch a third time. Failure here: see
                       Disagreement 1 (provisional: non-zero error whose text says the phase IS
                       shipped and names `dross ship` to push the marker; state stays shipped).
                 Delete the pre-push state save + "chore(dross): ship" commit at 269-287. Move the
                 "Marked shipped" narration after (f).
       contract:
         - [verification] Happy path (TestShipFullFlowAgainstMockProvider, ship_test.go:322,
           updated): tree clean; local HEAD == `git rev-parse phase/x` in the bare origin; `git show
           phase/x:.dross/phases/x/changes.json` in origin has pr==99 AND status=="shipped"; state
           shipped; log has no "chore(dross): ship x".
         - [verification+risk+mvp] c-4 (TestShipFailedRecordPushIsNotShipped): bare origin gets a
           pre-receive hook that exits 1 only when `git log -1 --format=%s "$new"` contains
           "record PR" (first push and PR open succeed, record push fails). After run 1: err != nil
           and contains "dross ship x"; state CurrentPhaseStatus != "shipped" and history has no
           "shipped x"; local changes.json pr==99 and status != "shipped" (residue locked); local
           HEAD subject == "chore(dross): record PR #99 for x"; origin's changes.json pr==0;
           phaseDone(root,"x") == false and `dross phase list` line for x has no "✓"; `dross
           status` output lacks "shipped:" and contains "dross ship". A mutant that flips state
           before the push fails the status assertion; one that keeps SetStatus in the record
           commit fails the local changes.json status assertion.
         - [verification+risk] c-4 second run: hook removed; run 2 returns nil; POST /pulls count
           across both runs == 1 (c-3); origin phase/x tip's changes.json pr==99 and status
           shipped; state shipped; tree clean; output contains "#99".
         - [verification+risk] c-1 (TestShipPushesLocalOnlyRecordWithNothingStaged): after a
           successful ship, `git update-ref` origin's phase/x back one commit in the bare repo —
           index clean — re-run ship: origin tip == local HEAD again. Today's `diff --cached
           --quiet` gate (ship.go:432) pushes nothing and this fails.
         - [risk] TestShipInSyncIssuesNoPush: a pre-receive hook on the bare remote that always
           exits 1 and writes a marker file must not fire on a re-run whose branch is already on
           origin.
         - [verification+risk] c-3 (TestShipIsReShippable, ship_test.go:819, tightened): POST
           /pulls == 1 over two runs; second run's stdout contains "#99"; `shipped x` history entry
           count == 1.
         - [verification+risk] Diverged phase branch: push a foreign commit to origin phase/x from a
           throwaway branch after the first ship, add a local commit, re-run → err mentions "git
           pull" and "--force", POST count stays 1, origin tip unchanged; with --force the re-run
           succeeds and origin tip == local HEAD.
         - [risk] TestShipMarkerPushFailure…: hook rejects only subjects containing "mark x shipped"
           → per Disagreement 1's default, ship returns an error mentioning "dross ship"; state.json
           AND changes.json both read shipped; re-run pushes the marker (remote tip subject ==
           "chore(dross): mark x shipped").
         - [risk] TestShipFlipIsAtomic: after a clean ship state.json shipped ⇔ changes.json status
           shipped (both true); after the c-4 failure fixture both false.
         - [mvp] TestShipDoesNotPersistPRWhenOpenFails (ship_test.go:546) extended: when the provider
           500s, still no "record PR" commit and pr==0, AND state must still read the pre-ship
           status, not shipped (a state flip left before OpenPR fails this).
         - [verification] TestShipEarlyReturnsLeaveBaseIntact, TestShipNoPushIssuesNoRecordPush
           unchanged and green: --no-push/--print-body make no push and no commit.
         - [verification+risk] TestShipCover_ResultTag (ship_test.go:1023) gains the "existing" arm;
           TestShipJSONReportsExistingPR: second run under --json → {"number":99,"result":"existing"}.
         - [risk] TestShipRecordsShippedNotCompleted (ship_test.go:1341) and
           TestShipPushesPRRecordToPhaseBranch (ship_test.go:453) stay green.

Wave 3
  t-7  Provider fallback when record has no PR                [verification+risk]
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go, internal/cmd/hostpolicy_test.go
       covers:   c-5 (CLI half); locked existing_pr_source
       depends:  [t-2, t-5, t-6]
       description:
                 Between stages (b) and (c) of t-6: when existingPR == 0 call
                 ship.FindOpenPRByHeadFunc(opts, phaseBranch) (remotePolicy + buildOpenOpts hoisted
                 above it). Hit → treat as the existing PR (record it, narrate "found open PR #N for
                 phase/<id>", no OpenPR). (nil, nil) → OpenPR. errors.Is(err,
                 ship.ErrHeadPRLookupUnsupported) → narrate the skip and OpenPR. Any other error →
                 refuse before OpenPR ("could not check origin for an open PR on phase/<id>: …; fix
                 and re-run `dross ship`") — fail-closed. shipFixture (ship_test.go:110) installs a
                 default stubOpenPRByHead(t, nil, nil) (t.Cleanup-restored, mirroring stubPRMerged,
                 phase_test.go:29) so the hand-rolled forgejo mocks never see a GET /pulls; c-5 tests
                 override it. hostpolicy_test.go:125: add the FindOpenPRByHeadFunc stub-and-restore
                 alongside the PRStatusFunc one.
       contract:
         - [verification+risk] Ship-died-after-open shape: after a successful ship, rewrite
           changes.json without pr and commit; stub returns {Number: 77, URL: ".../77"}; re-run →
           stub called once with head "phase/x"; POST /pulls count stays 1; origin phase/x tip's
           changes.json pr==77; stdout contains "#77".
         - [verification] Stub returns (nil, nil) on a first ship → POST /pulls exactly once.
         - [verification+risk] Stub returns errors.New("boom") → ship returns an error naming
           "dross ship"; POST count == 0; no "record PR" commit; changes.json pr stays 0.
         - [verification+mvp] Stub returns ErrHeadPRLookupUnsupported → narration contains
           "skipped" and POST count == 1.
         - [verification+risk] Record-first: with changes.json pr==99 the stub's call count is 0
           across a re-run (a lookup-first mutant fails; the record wins, the provider is never asked).
         - [verification] --no-push and --print-body: stub call count 0.
```

Coverage: c-1 → t-1, t-6; c-2 → t-3, t-6; c-3 → t-4, t-6; c-4 → t-6;
c-5 → t-2, t-5, t-7. Locked: shipped_timing → t-6 (f); existing_pr_source →
t-6 (c) + t-7; failed_push_residue → t-6 c-4 test (record commit is HEAD);
diverged_phase_branch → t-1 + t-6; shared_origin_gate → t-1 (source pin).

## Disagreements

### 1. Marker-push failure semantics (first-class — flagged by the lead)

All three drafts converge on the same mechanism: the locked shipped_timing
forces a second `.dross`-only commit ("chore(dross): mark <id> shipped")
committed and pushed AFTER the record push, because changes.json is tracked and
cannot flip to shipped inside the commit that is itself the thing being pushed.
Rejected by all three: a dirty tree (TestShipIsReShippable, `phase complete`
refuses), an unpushed marker (perpetual "ahead by 1", `phase complete` deletes a
branch with unpushed work), a rollback (violates failed_push_residue), a second
"record pending" marker (the lock's "two states to keep honest").

They diverge on what happens when THAT third push fails:

- risk: non-zero error whose text says the phase IS shipped and names `dross
  ship` to push the marker; state stays shipped. Reason: `--auto`/`--json` loops
  never learn a commit was left behind from a warning.
- verification: warning, exit 0, --json intact. Reason: the durable point has
  passed; failing there reports a shipped phase as failed and suppresses --json.
- mvp: silent — "the next run's origin-ahead gate pushes the leftover commit
  (c-1)"; no narration or error specified.

Provisional default: **risk** (non-zero, text names both "shipped" and the
retry). Why it matters: it is the only failure in the flow where exit code and
state disagree; the choice decides whether a scripted `dross ship --auto` can
ever notice a marker left local. A warning is invisible to a caller that only
reads the exit code; an error is visible but must not be read as "not shipped"
— the wording carries that. If the user prefers exit 0, t-6's
TestShipMarkerPushFailure… contract flips from "returns an error" to "returns
nil and narrates". Also note: with the default, `--json` output must still be
emitted before the error return or the JSON caller loses the PR number —
verification's objection stands and the executor must handle it.

### 2. c-5 provider lookup: dedicated by-head seam vs reuse of OpenPRsTargeting

- mvp: reuse `ship.OpenPRsTargetingFunc(opts, base)` and filter on
  `BasePR.HeadRefName == phase/<id>`; Bitbucket's ErrBasePRLookupUnsupported →
  announced skip → create. Zero new provider code.
- risk + verification: new `FindOpenPRByHead` seam with one arm per provider
  (two tasks, t-2 + t-5). Reasons: the base-filter misses a retargeted PR,
  inherits `gh pr list`'s default 30-row cap on GitHub, and is a silent no-op
  on Bitbucket — the duplicate PR c-5 forbids, on one of four shipped providers.

Provisional default: **dedicated seam** (2 of 3, and c-5 reads "queries the
provider for an open PR with head phase/<id>", which the base-filter does not
literally do). Cost: two extra tasks in package ship. If the user takes mvp's
route, t-2 and t-5 drop, t-7 stubs OpenPRsTargetingFunc instead, and c-5 is
knowingly hollow on Bitbucket.

### 3. "Record pending" derivation: local fields vs origin-tip read

- risk + verification: derived locally as `pr > 0 && status != shipped &&
  !complete`; network-free, no new field. Requires re-seeding
  TestStatusShippedFromPRRecordAlone with status="shipped".
- mvp: positive observation — `git show origin/phase/<id>:.dross/phases/<id>/
  changes.json` and compare its pr to the local one; a missing origin ref is
  "unknown, changes nothing". Keeps every existing status fixture untouched;
  needs a fetched origin ref to answer at all.

Provisional default: **derived** (matches the exact shape c-4 asserts —
"changes.json status ==''" — and the lock's "two states to keep honest").
Why it matters: mvp's form is the only one that is right on a fresh clone of a
pre-StatusShipped record without touching fixtures, and the only one that goes
wrong (stays silent) when origin has never been fetched. The derived form has
one known hazard — legacy records with pr and no status. Verified cost: the
oracle fixture writeOracleChanges (status_test.go:1333) seeds exactly that
shape, so TestSuggestNextOpenPRAdvisesTheMerge goes red under the derived
design and must be re-seeded with status="shipped" (t-3 contract); the merged
legacy case is handled by Disagreement 4's ordering. Neither risk nor
verification noticed this fixture; mvp did, which is why it chose the
origin-tip read. If the user wants zero fixture churn, mvp's recordPending
replaces the derived shape in t-3 and the shippedUnmergedPhase guard becomes
`!shippedStatus && (sh.pr == 0 || recordPending(...))`.

### 4. suggestNext: where the retry arm sits relative to phaseMergeState

- verification + mvp: retry arm BEFORE the phaseMergeState consult, so
  mergeOpen can never say "merge the open PR" for a record origin never
  received.
- risk: AFTER the merged consult (merged → retry → open), so a legacy record
  (pr set, status empty, PR long merged) is told `dross phase complete`, not to
  re-ship.

Provisional default: **risk's ordering** — it satisfies both concerns (the
switch is split: mergeMerged first, then the pending arm, then mergeOpen).
Why it matters: a wrong order either sends a merged legacy phase back to ship
or sends an unpushed record to "merge the PR"; the split costs nothing.

### 5. Shape of the ship.go work: one restructure vs three serial tasks vs a monolith

- risk: t-6 (route pushes through the gate) → t-7 (resolve existing PR) →
  t-8 (delete early flip, reorder, marker commit) — 4 waves, ship.go edited
  three times, t-7's code written against the old order then moved by t-8.
- verification: t-6 (one restructure: gate + record-first + late flip + c-4
  test) → t-7 (provider lookup inserted between stages) — 3 waves.
- mvp: one task for all of ship.go plus the prompt.

Provisional default: **verification's split**. Why it matters: it decides
whether the c-4 failure test can go green in a single task (it can — t-6 needs
nothing from the provider lookup) and how many times the same 180-line block is
re-shaped. Risk's finer split buys nothing the contracts distinguish.

### 6. Isolating cmd tests from the new lookup

- risk: teach every hand-rolled forgejo mock in ship_test.go (shipMockFlow,
  TestShipFullFlowAgainstMockProvider, TestShipPushesPRRecordToPhaseBranch,
  TestShipDoesNotPersistPRWhenOpenFails, TestShipIsReShippable) to answer
  GET .../pulls with `[]`.
- verification: shipFixture installs a default `FindOpenPRByHeadFunc` stub
  returning (nil, nil), t.Cleanup-restored, mirroring stubPRMerged; only c-5
  tests override it. Real HTTP/gh paths are proven in package ship.

Provisional default: **verification** (one seam, no refactor of seven tests).
Why it matters: risk's route exercises the real forgejo lookup end-to-end in
cmd; verification's does not — so if the user wants an e2e proof of the
by-head lookup through ship, one risk-style mock (the c-4 test) should also
answer GET /pulls.

### 7. Minor structure calls (recorded, default = skeleton)

- Prompt edit as its own task (risk, verification) vs folded into the ship
  task (mvp) → own task (t-4); different file, different test, wave 1.
- Recovery section: add item 4 "Record push failed" (verification) vs rename
  item 3 (risk, mvp) → add item 4; item 3's dirty-tree case is still a real
  legacy state.
- Forgejo: shared `forgejoListOpenPRs` lister refactor (verification) vs a
  parallel `forgejoOpenPRByHead` (risk) → shared lister; existing pagination
  tests pin it.
- `push -u` on every non-force push (risk) vs `-u` only when the origin ref is
  missing (verification) → `-u` always; harmless and keeps the upstream the
  prompt's later `git push` relies on.
