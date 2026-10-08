# push-gates-on-origin — verification-lens draft

Lens: every task below was derived by writing the criterion's ideal test
first, then finding the smallest code change that makes that test
satisfiable with the harnesses the repo already has (bare-origin repos +
`pre-receive` hooks in `internal/cmd`, `httptest` forgejo/gitlab/bitbucket
mocks and the `ghCommand` fake in `internal/ship`, exported `*Func` seams
stubbed from `internal/cmd` tests).

One structural fact drives the shape of t-6: `changes.json` is tracked, so
"flip `changes.json` to shipped only AFTER the push that carries the PR
record" cannot be one commit — the status flip is a second `.dross`-only
chore commit made after the record push succeeds, and pushed by the same
shared gate. See Judgment calls.

```
Phase push-gates-on-origin — 7 tasks across 3 waves

Wave 1
  t-1  Extract shared origin gate; add phase-branch push
       files:    internal/cmd/originpush.go (new), internal/cmd/basebranch.go,
                 internal/cmd/originpush_test.go (new)
       covers:   c-1 (gate mechanism), locked shared_origin_gate, diverged_phase_branch
       description:
                 Lift basebranch.go:63-86 (fetch, rev-parse origin ref, rev-list
                 ahead/behind) into `compareWithOrigin(repoDir, branch) (originDelta,
                 error)` with `originDelta{Missing bool; Ahead []string; Behind bool}`.
                 `pushBaseIfAheadDrossOnly` keeps its exact policy but consumes the
                 delta instead of running its own rev-lists. Add
                 `pushPhaseBranch(repoDir, branch string, force bool) (pushed bool, err
                 error)`: Missing → `push -u origin <b>`; Ahead && !Behind → `push
                 origin <b>`; Ahead && Behind → guided error naming `git pull --rebase
                 origin <b>` and `dross ship --force`; force=true → `push -u
                 --force-with-lease` regardless. Every argv via gitRefArgs (the
                 subprocargs audit test rejects anything else).
       contract: - Local phase/x is one commit ahead of an existing origin/phase/x
                   with a CLEAN index → pushPhaseBranch pushes; `git rev-list
                   origin/phase/x..phase/x` is empty afterwards. A gate that reads
                   `git diff --cached` (today's ship.go:432 behaviour) sees nothing
                   staged, pushes nothing, and this fails.
                 - Level branch + a pre-receive hook that refuses everything →
                   pushed=false, err=nil (an always-push mutant errors here).
                 - Diverged (origin/phase/x has a commit local lacks AND local has
                   one origin lacks) → err mentions "git pull" and "--force"; origin
                   tip unchanged. Same fixture with force=true → origin tip == local
                   HEAD.
                 - origin/phase/x missing → push succeeds AND `git rev-parse
                   --abbrev-ref phase/x@{upstream}` == origin/phase/x (the `-u` is
                   load-bearing for the prompt's later `git push`).
                 - Shared-helper proof: a mutant inverting `Behind` in
                   compareWithOrigin fails BOTH the existing TestPushBaseDivergedNoop
                   (basebranch_test.go:237) AND the new
                   TestPushPhaseBranchDivergedRefuses. TestPushBaseDrossOnlyAheadPushes,
                   TestPushBaseNotAheadNoop, TestPushBaseCodeAheadRefusesAndPushesNothing
                   and TestPushBaseRejectedPushSurfacesGitOutput stay green unchanged.
                 - Source-pin: a test reads internal/cmd/basebranch.go and fails if
                   it contains a `rev-list` argv of its own — the "neither carries
                   its own copy" half of the locked decision.

  t-2  Add FindOpenPRByHead seam: github + forgejo
       files:    internal/ship/headpr.go (new), internal/ship/forgejo.go,
                 internal/ship/headpr_test.go (new), internal/ship/forgejo_test.go
       covers:   c-5 (provider half)
       description:
                 `FindOpenPRByHead(opts OpenOpts, head string) (*OpenResult, error)`
                 dispatching like basepr.go:36-49; `FindOpenPRByHeadFunc` exported
                 seam like merged.go:53; `ErrHeadPRLookupUnsupported` sentinel
                 returned by the gitlab/bitbucket arms until t-5 lands. Not-found
                 is (nil, nil); any lookup failure is a non-nil error, never nil-nil.
                 GitHub: `gh pr list --head <h> --state open --json number,url`, head in
                 the value slot of --head. Forgejo: pull the page loop out of
                 forgejoOpenPRsTargeting (forgejo.go:122-164) into
                 `forgejoListOpenPRs(opts)` returning the raw items; both callers
                 filter on it (base.ref vs head.ref).
       contract: - ghCommand fake asserts by index that argv[0..2] == pr,list and
                   that the token after "--head" is exactly "phase/x" and that
                   "--state" is followed by "open" (gharg_test.go style); fake
                   prints `[{"number":7,"url":"https://github.com/o/r/pull/7"}]` →
                   result.Number == 7.
                 - Fake prints `[]` → (nil, nil). Fake exits 1 → err != nil and
                   result == nil (a lookup that swallows failures into nil-nil
                   would let ship open a duplicate PR on a flaky network).
                 - Forgejo httptest serves two pages of 50: page 1 holds only PRs
                   with head.ref "other", page 2 holds head.ref "phase/x" number 12
                   → result.Number == 12 (a refactor that drops pagination fails
                   here); no match across pages → (nil, nil); HTTP 500 → error.
                 - TestForgejoOpenPRsTargetingFiltersByBaseRef and
                   TestForgejoOpenPRsPaginates (forgejo_test.go:161,251) stay green
                   through the shared lister.
                 - `FindOpenPRByHeadFunc` defaults to `FindOpenPRByHead`
                   (mirror of TestOpenPRsTargetingFuncDefaultsToOpenPRsTargeting).

  t-3  Status reads shipped from the record, names retry
       files:    internal/cmd/status.go, internal/cmd/status_test.go,
                 internal/watch/drift_test.go
       covers:   c-2 (status / watch consumers)
       description:
                 Introduce the derived "record pending" shape: `ch.PR > 0 &&
                 ch.Status != changes.StatusShipped && !changes.Complete`. In
                 shippedUnmergedPhase (status.go:575-600) the shipped signal becomes
                 `state shipped || ch.Status == StatusShipped`; the pending shape
                 prints `pending:   phase/<id> — PR #N is open but its record has not
                 reached origin; re-run \`dross ship <id>\`` instead of the shipped
                 line. In suggestNext (status.go:295-311) the pending shape returns
                 "`dross ship <id>` — push the PR record for #N" BEFORE the
                 phaseMergeState consult, so mergeOpen cannot answer "merge the open
                 PR" for a record origin never received. No new marker file or
                 field — the shape is derived from fields that already exist.
                 watch/drift.go needs no code change (classifyPhase already requires
                 state shipped + PR); pin it with a test.
       contract: - Fixture: phase/x checked out, changes.json {pr:42, base:main, no
                   status}, state status cleared → `dross status` output contains
                   "re-run `dross ship x`" and does NOT contain "shipped:"; suggestFor
                   returns a string containing "dross ship x" and not "merge the open
                   PR". Today's code (status.go:597-600 keys on pr != 0) prints the
                   shipped line and TestSuggestNextOpenPRAdvisesTheMerge's arm fires.
                 - TestStatusShippedFromPRRecordAlone is re-seeded with
                   changes.json status="shipped" (the tracked flip is what a fresh
                   clone actually carries now) and must still print "shipped:" "#42".
                 - Control: same fixture with changes.json status="complete" → neither
                   line (Complete suppressor unchanged).
                 - watch: state {} + changes {pr:42, no status} + verdict pass →
                   ClassifyDrift reports DriftVerifiedUnshipped (the phase "still
                   reads verified", per locked shipped_timing); state shipped +
                   changes status shipped + pr 42 → no drift.
                 - TestStatusCountsShippedPhaseDone (status_test.go:370) still counts
                   a status="shipped" record as done through phaseDone.

  t-4  Replace ship prompt's do-not-re-run guidance
       files:    assets/prompts/ship.md, internal/cmd/ship_prompt_test.go (new)
       covers:   c-3 (prompt half)
       description:
                 ship.md:134 — replace "Do NOT re-run `dross ship` (would open a
                 second PR)" with: re-running `dross ship` is safe — it recognises
                 the open PR, pushes anything pending on phase/<id>, and reports the
                 existing PR. ship.md:107-115 — the CLI step list becomes: gate
                 phase/<id> on origin, open the PR (skipped when the record or the
                 provider already has one), commit + push the PR record, THEN mark
                 shipped. ship.md Recovery (190-202) — add item 4 "Record push
                 failed": status reads verified and names the retry; run `dross ship
                 <phase-id>` again. rules.toml r-01: `make install` after the edit.
       contract: - A test reads assets/prompts/ship.md via repoRootFromTest and
                   fails if any line contains "Do NOT re-run" or "would open a
                   second PR"; fails unless the §5 "On failure" block contains
                   "re-run `dross ship`" (or "re-running `dross ship`"); fails unless
                   the Recovery section names "record push" and `dross ship`.
                 - TestPromptsNeverStageStateJSONByPath stays green (no git add
                   lines added).

Wave 2
  t-5  Add gitlab + bitbucket by-head lookups   (depends t-2)
       files:    internal/ship/headpr.go, internal/ship/gitlab.go,
                 internal/ship/bitbucket.go, internal/ship/gitlab_test.go,
                 internal/ship/bitbucket_test.go
       covers:   c-5 (provider half, remaining providers)
       description:
                 Swap the two sentinel arms in headpr.go for
                 `gitlabOpenMRBySource` (GET /projects/<ref>/merge_requests?
                 state=opened&source_branch=<h>, via gitlabReq like gitlab.go:135-173,
                 first hit wins, iid → Number) and `bbOpenPRBySource` (GET
                 /repositories/<ws>/<slug>/pullrequests?q=source.branch.name="<h>" AND
                 state="OPEN", via bbRequest + bbCredentials like bitbucket.go:112-150,
                 values[0].id → Number, links.html.href → URL). Keep the sentinel
                 declared for the deferred azure-devops provider.
       contract: - GitLab httptest asserts the request path carries
                   `source_branch=phase%2Fx` and `state=opened`, plus the auth
                   header for both private-token and bearer schemes; body
                   `[{"iid":5,"web_url":"..."}]` → Number 5; `[]` → nil,nil; 500 → err.
                 - Bitbucket httptest asserts the `q` query decodes to contain
                   `source.branch.name="phase/x"` and `state="OPEN"` and that Basic
                   auth carries auth_user; `{"values":[{"id":9,"links":{"html":{"href":
                   "..."}}}]}` → Number 9; `{"values":[]}` → nil,nil; missing
                   auth_user → error (bbCredentials path).
                 - TestOpenPRMessageListsShipProviders-style check: an unknown
                   provider string still returns the "unsupported provider" error,
                   distinguishable from ErrHeadPRLookupUnsupported.

  t-6  Gate record push on origin; flip shipped after   (depends t-1)
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-1, c-2, c-3 (CLI half), c-4; locked shipped_timing,
                 failed_push_residue, diverged_phase_branch
       description:
                 Restructure ship.go:269-449 into ordered stages, all after the
                 base safety net:
                   (a) load changes.json; `existingPR := ch.PR`.
                   (b) `pushPhaseBranch(repoDir, phaseBranch, forcePush)` replaces
                       the unconditional push at 336-347 (a diverged branch refuses
                       BEFORE any PR exists).
                   (c) if existingPR == 0 → ship.OpenPR as today; else res =
                       {Number: existingPR}, narrate "PR #N already open — pushing
                       the pending record", telemetry result tag "existing".
                   (d) SetPR/SetBase (no SetStatus) → `git add` → commit
                       "chore(dross): record PR #N for <id>" only if staged.
                   (e) `pushPhaseBranch` again — hard error on failure, wording
                       ends "re-run `dross ship <id>` to push the record". The record
                       commit is left in place.
                   (f) only now: state.CurrentPhaseStatus = "shipped" + history-guarded
                       `shipped <id>` + Save; changes.SetStatus(StatusShipped) →
                       commit "chore(dross): mark <id> shipped" if staged →
                       `pushPhaseBranch` a third time; a failure HERE narrates a
                       warning naming the re-run (the durable point has already
                       passed — the record is on origin) rather than failing.
                 Delete the pre-push state save + "chore(dross): ship" commit branch
                 at 269-287. Move the "Marked shipped" narration after (f). --json
                 emits the existing number on a re-run.
       contract: - Happy path (TestShipFullFlowAgainstMockProvider updated): tree
                   clean; local HEAD == `git rev-parse phase/x` in the bare origin;
                   `git show phase/x:.dross/phases/x/changes.json` in origin has
                   pr==99 AND status=="shipped"; state shipped; log has no
                   "chore(dross): ship x".
                 - c-4 failure path (new TestShipFailedRecordPushIsNotShipped):
                   bare origin gets a pre-receive hook that `exit 1`s only when
                   `git log -1 --format=%s "$new"` contains "record PR" (so the first
                   push and the PR open succeed, the record push fails). After run 1:
                   err != nil and err.Error() contains "dross ship x"; state
                   CurrentPhaseStatus != "shipped" and history has no "shipped x";
                   local changes.json pr==99 and status != "shipped" (residue
                   locked); local HEAD subject == "chore(dross): record PR #99 for
                   x"; origin `show phase/x:…/changes.json` has pr==0;
                   `dross phase list` output line for x has no "✓". A mutant that
                   flips state before the push fails the status assertion; a mutant
                   that keeps SetStatus in the record commit fails the local
                   changes.json status assertion.
                 - c-4 second run: remove the hook; run 2 returns nil; POST /pulls
                   count across both runs == 1 (c-3); origin phase/x tip's
                   changes.json pr==99; state shipped; local changes.json status
                   shipped; output contains "#99".
                 - c-1 (new TestShipPushesLocalOnlyRecordWithNothingStaged):
                   after a successful ship, `git update-ref` origin's phase/x back
                   one commit in the bare repo (simulating a record that never
                   landed) — index clean, nothing to stage — re-run ship: origin tip
                   == local HEAD again. Today's `diff --cached --quiet` gate
                   (ship.go:432) pushes nothing and this fails.
                 - c-3 (TestShipIsReShippable tightened): count POST /pulls == 1
                   over two runs; second run's stdout contains "#99"; `shipped x`
                   history entry count == 1.
                 - Diverged phase branch (new): push a foreign commit to origin
                   phase/x from a throwaway branch after the first ship, then
                   re-run → err mentions "git pull" and "--force", POST count stays
                   1, origin tip unchanged; with --force the re-run succeeds and
                   origin tip == local HEAD.
                 - TestShipEarlyReturnsLeaveBaseIntact, TestShipNoPushIssuesNoRecordPush
                   and TestShipDoesNotPersistPRWhenOpenFails unchanged and green:
                   --no-push/--print-body make no push and no commit; a rejected
                   PR-open leaves pr==0 and no record commit.
                 - TestShipCover_ResultTag gains the "existing" arm.

Wave 3
  t-7  Provider fallback when record has no PR   (depends t-2, t-6)
       files:    internal/cmd/ship.go, internal/cmd/ship_test.go
       covers:   c-5 (CLI half); locked existing_pr_source
       description:
                 Between stages (b) and (c) of t-6: when existingPR == 0 call
                 `ship.FindOpenPRByHeadFunc(opts, phaseBranch)`. Hit → treat as the
                 existing PR (record it, narrate "found open PR #N for phase/<id>",
                 no OpenPR). (nil, nil) → OpenPR. `errors.Is(err,
                 ship.ErrHeadPRLookupUnsupported)` → narrate the skip and OpenPR.
                 Any other error → refuse before OpenPR ("could not check origin for
                 an open PR on phase/<id>: …; fix and re-run `dross ship`") —
                 fail-closed, because creating on a failed lookup is the duplicate
                 the criterion forbids. shipFixture installs a default
                 `stubOpenPRByHead(t, nil, nil)` (t.Cleanup-restored, mirroring
                 stubPRMerged in phase_test.go:29) so the seven hand-rolled forgejo
                 mocks never see a GET /pulls; c-5 tests override it.
       contract: - Ship-died-after-open shape: after a successful ship, rewrite
                   changes.json without `pr` and commit; stub returns
                   {Number: 77, URL: ".../77"}; re-run → stub was called once with
                   head "phase/x"; POST /pulls count stays 1; origin phase/x tip's
                   changes.json pr==77; stdout contains "#77".
                 - Stub returns (nil, nil) on a first ship → POST /pulls happens
                   exactly once (create still works).
                 - Stub returns errors.New("boom") → ship returns an error naming
                   "dross ship"; POST count == 0; no "record PR" commit.
                 - Stub returns ErrHeadPRLookupUnsupported → narration contains
                   "skipped" and POST count == 1.
                 - Record-first: with changes.json pr==99 the stub's call count is
                   0 across a re-run (a lookup-first mutant fails this).
                 - --no-push and --print-body: stub call count 0.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (shared origin-ahead gate + pushPhaseBranch), t-6 (ship's record push routed through it; local-only-record re-push test) |
| c-2 | t-6 (flip after push; failure leaves verified), t-3 (status/suggestNext name the retry; watch pinned; phase list via phaseDone asserted in t-6's failure test) |
| c-3 | t-6 (record-first re-run, single POST, existing PR reported), t-4 (prompt guidance replaced) |
| c-4 | t-6 (pre-receive hook that rejects only the record push; second run pushes it; origin tip carries the PR number) |
| c-5 | t-2 + t-5 (per-provider by-head lookups behind one seam), t-7 (record → provider → create ordering in ship) |

Locked decisions: shipped_timing → t-6 stage (f); existing_pr_source → t-6 (c) + t-7; failed_push_residue → t-6 failure test asserts the record commit is HEAD; diverged_phase_branch → t-1 guided error + t-6 diverged re-run test; shared_origin_gate → t-1 (compareWithOrigin used by both pushers, source-pin test on basebranch.go).

## Judgment calls

- **Two chore commits per ship, not one.** changes.json is tracked, so "flip changes.json to shipped only after the push that carries the record" is unsatisfiable inside the record commit itself — a status baked into that commit is on disk before any push. Chose: record commit (pr+base) → push → flip commit (status=shipped) → push. Rejected: single commit with status=shipped and a rollback on failure (violates locked failed_push_residue) and single commit with status=shipped read as "not really shipped until state.json agrees" (a second state to keep honest, which the locked decision explicitly rejects). Cost: origin's phase/<id> tip after a ship is "mark shipped", not "record PR" — TestShipFullFlow's HEAD-subject assertion is rewritten to check the file content at origin's tip instead.
- **Flip-commit push failure is a warning, not an error.** The durable point (record on origin) has passed; failing there would report a shipped phase as failed and suppress --json. The next ship/re-run's origin-ahead gate pushes it. Rejected: hard error for symmetry with the base net — symmetry is about the gate, not the failure policy after the flip.
- **The first push also goes through pushPhaseBranch.** Rejected keeping the raw `push -u` at ship.go:336: a diverged branch would then open a PR and only refuse at the record push, leaving an orphan PR. One helper, three call sites, one policy.
- **Dedicated by-head provider query, not a filter over OpenPRsTargeting.** Rejected reusing `OpenPRsTargetingFunc(base)` filtered on HeadRefName: it misses a retargeted PR, inherits `gh pr list`'s default 30-row cap on GitHub, and returns unsupported on Bitbucket — which would make c-5 silently a no-op on one of four providers. Cost: two extra tasks (t-2, t-5) in package ship.
- **Lookup error fails closed; unsupported falls through to create.** A failed lookup followed by a create is precisely the duplicate c-5 forbids; but treating "unsupported" as fatal would make the deferred azure-devops provider unshippable on day one. The two are distinguishable by sentinel, so they get different policies.
- **cmd tests stub the by-head seam in shipFixture rather than teaching seven mocks to answer GET /pulls.** Mirrors stubPRMerged's precedent for PRStatusFunc; the real HTTP/gh paths are proven in package ship (t-2, t-5), the ordering policy in package cmd (t-7). Rejected: a shared forgejo mock helper — a refactor of seven existing tests that adds nothing to any contract.
- **"Record pending" is derived, not stored.** status/suggestNext compute it from `pr > 0 && status != shipped && !complete`, fields that already exist; no new marker, per the locked decision's "two states to keep honest" objection.
- **watch gets a test, not a change.** classifyPhase already needs state shipped AND a PR; with the flip moved after the push, a failed push reads verified_unshipped, which is the locked "still reads verified" wording.
- **t-3 and t-4 sit in wave 1** despite being consumers of t-6's behaviour: they read fields and files that exist today and their tests seed those shapes directly, so nothing in them strictly needs t-6's output.
