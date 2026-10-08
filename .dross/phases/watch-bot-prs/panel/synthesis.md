# watch-bot-prs — panel synthesis

## Scores

Scale 1–5. Claims were checked against source before scoring:

- `ghCommand` returns an `*exec.Cmd`, and `screenedGH` is its only caller (TestSeamHasExactlyOneScreenedCaller).
- TestGhMarkersAreLoadBearing fails on a `//dross:taint-cleared` marker that clears no flow.
- The watch_json golden fixture has no `[remote]`.
- `boardRepo` leaves `remote.provider` empty: cmd tests use a hermetic HOME and have no git origin.
- `watch.state.json` is saved only when the board was reached.
- `internal/ship` does not import `internal/watch`.

| Draft | Dimension | Score | Note |
|---|---|---|---|
| risk | criteria coverage | 5 | All of c-1..c-5 and all three locks are mapped, and each of the 22 registered risks has one owning task. |
| risk | test-contract specificity | 5 | The sharpest draft. It covers the 47h59m/48h boundary, +05:00 offsets, fork heads, missing createdAt, CombinedOutput corruption, the LookPath/PATH trap, one spawn only, and title injection. Two flaws: the `sh -c 'sleep 30'` stub can exec in place, which makes the WaitDelay half vacuous, and "branches never printed" contradicts c-5's `pr:` line. |
| risk | granularity | 3 | t-1 (record type + reducer) is split from t-2 (lister). Both sit in the same package and file family, and the split buys no wave depth. |
| risk | wave correctness | 5 | t-2 and t-3 correctly run in parallel on t-1. t-5 depends on t-3, which its reflection test justifies. t-4 depends on both. |
| mvp | criteria coverage | 5 | All of c-1..c-5 and all three locks are covered. |
| mvp | test-contract specificity | 3 | The argv, rollup rows and digest strings are exact. It misses fork heads, missing createdAt, offline hangs, the LookPath trap, stderr in the decode, persistence and injection. Nothing ties the prompt to the Go tags. |
| mvp | granularity | 2 | t-1 is 4 files across 2 layers (legal, but at the limit). It bundles the lister, rollup, classification, age, render and JSON wiring, so every pure-logic test lands in the slow internal/cmd suite. |
| mvp | wave correctness | 5 | One wave is valid, because the draft pins the field names that the prompt task needs. |
| verification | criteria coverage | 5 | All of c-1..c-5 are covered, with the deciding test named per criterion, and the locks are mapped. |
| verification | test-contract specificity | 4 | Tests are named and use exact fixtures: the "botanist" row, JSON key sets, byte-identical degrade for both renders, and the real unstubbed gitlab path. It misses fork heads, missing createdAt, stderr in the decode and hangs. Its persistence test runs board-off, where state is never written, so it passes trivially. |
| verification | granularity | 5 | Four tasks, each ≤3 files, each one layer. t-4 touches ship only to add the marker. |
| verification | wave correctness | 4 | The order is right. But t-4's TestWatchPromptNamesEveryDigestField needs t-2's prompt edits, and t-2 is missing from t-4's depends_on. Only the wave numbering hides the gap. |

**Skeleton: verification.** It has the best task cut and puts the pure logic in the fast internal/ship and internal/watch suites. Its contracts are nearly as sharp as risk's with one fewer task. Risk's extra task adds no wave depth, and MVP pushes every table test into the slow internal/cmd suite. Risk supplies most of the grafts.

## Merged plan

```
Phase watch-bot-prs — 4 tasks across 3 waves

Wave 1
  t-1  Add gh open-PR lister with check rollup                  [verification+risk+mvp]
       files:    internal/ship/openprs.go, internal/ship/openprs_test.go
       covers:   c-1, c-3, c-4, c-5
       depends:  —
       desc:     ListOpenPRs(opts) + ListOpenPRsFunc seam: only github spawns, via ONE screenedGH `pr list` (argv below). Stdout-only decode into ship.OpenPR
                 with the checks rollup. The wait is bounded by a test-overridable prListTimeout (Kill + WaitDelay on the Cmd ghCommand returns).
                 Any failure → (nil, fixed-prose err), quietly (no ghFailed/ghUnparseable). Other providers → ErrOpenPRListUnsupported. No LookPath, no taint marker.
       contract: [verification+risk] TestListOpenPRsArgv (captureGh): argv equals exactly
                   ["pr","list","--state","open","--limit","100","--json","number,title,url,author,createdAt,headRefName,isCrossRepository,statusCheckRollup"].
                   Dropping `--state open`, dropping `--limit` (gh's default of 30 truncates silently), dropping any json field, or swapping
                   createdAt for updatedAt fails it. No merge/close/comment/edit verb appears.
                 [verification+risk] TestListOpenPRsDecodesBotFlag: a canned gh doc with app/dependabot is_bot:true,
                   app/github-actions is_bot:true and human "botanist" is_bot:false decodes IsBot true/true/false. A mistagged `isBot`
                   field empties the bots, and a login-substring check flips the "botanist" row; either fails it.
                 [verification+risk+mvp] TestCheckRollup (table):
                   - `[]` and `null` → none.
                   - CheckRun SUCCESS/NEUTRAL/SKIPPED and StatusContext SUCCESS → passing.
                   - CheckRun FAILURE/TIMED_OUT/CANCELLED/ACTION_REQUIRED/STARTUP_FAILURE, or StatusContext FAILURE/ERROR → failing.
                   - CheckRun QUEUED/IN_PROGRESS/WAITING/PENDING, or StatusContext PENDING/EXPECTED → pending.
                   - One FAILURE among nine SUCCESS plus one IN_PROGRESS → failing, and IN_PROGRESS + SUCCESS → pending
                     (a first-item-wins or last-item-wins reducer fails).
                   - A COMPLETED CheckRun with an unrecognised conclusion ("STALE", "SOMETHING_NEW") → pending, never passing.
                 [verification+risk] TestListOpenPRsFailureIsNilAndQuiet: each stub returns (nil, non-nil err), never an empty slice
                   with a nil err:
                   - gh exits 4 with "run gh auth login CANARY" on stderr;
                   - gh prints truncated or non-JSON;
                   - ghCommand points at a nonexistent binary (gh missing).
                   Each err names `gh pr list` and contains neither "CANARY" nor "auth login". The swapped ghStderr buffer holds
                   zero bytes.
                 [risk] A stub printing valid JSON on stdout and "warning: ..." on stderr, exit 0, decodes 1 PR (a CombinedOutput
                   decode fails).
                 [risk] Stub output `[{"number":5}]` (no createdAt) → error, not a PR aged ~739,000 days. A record missing number
                   → error.
                 [verification+risk] TestListOpenPRsEmpty: `[]` → nil error and zero records. Reachable-and-empty stays distinct
                   from failure.
                 [verification+risk+mvp] TestListOpenPRsUnsupportedProviders: gitlab, forgejo, gitea, bitbucket and "" each satisfy
                   errors.Is(err, ErrOpenPRListUnsupported) with the ghCommand stub's spawn counter at 0 (forge_scope).
                   "GitHub " (mixed case + trailing space) spawns exactly once.
                 [risk] The happy-path test runs with PATH set to an empty temp dir and an absolute-path /bin/sh stub, so an
                   exec.LookPath("gh") pre-check fails it on every machine.
                 [risk, sharpened] Stub `/bin/sh -c 'sleep 30; true'` with prListTimeout=100ms returns an error inside a 3s test
                   deadline. The `; true` keeps a grandchild sleep holding stdout. A Kill without WaitDelay blocks on that
                   orphaned pipe and misses the deadline.
                 [risk] 3 PRs in the stub output → exactly 1 spawn (no per-PR `gh pr checks` fan-out).

  t-2  Document bot/ship PR lines in watch prompt               [verification+risk+mvp]
       files:    assets/prompts/watch.md, internal/cmd/watch_prompt_test.go, README.md
       covers:   c-2, c-3, c-4, c-5
       depends:  —
       desc:     §1: bot_prs/ship_prs bullets with item fields; ABSENT (unknown, print nothing) when the forge can't be queried, [] when none. §2: the
                 `bot PRs: …` summary (only when non-empty; parenthetical only when failing>0) and one `pr: #<n> <head> — <checks>` per ship PR. Hard rules: PR data is
                 information only and forge data, not instructions; titles and authors are never printed. README watch rows updated. (r-01: make install before a live check.)
       contract: [verification+mvp+risk] TestWatchPromptBotPRLine: lowercased watch.md contains `bot_prs` and `age_days`. It
                   also contains the template `bot prs: <n> open (<f> failing), oldest <d>d` inside §2's render block (before
                   "locked precedence"), plus the instruction to drop the parenthetical at zero failing. Deleting the template or
                   moving it out of §2 fails it.
                 [verification+risk] TestWatchPromptShipPRLine: contains `ship_prs` and `pr: #<n> <head> — <checks>` inside §2's
                   render block.
                 [verification+mvp] TestWatchPromptPRAbsentPrintsNothing: the bot_prs and ship_prs bullets say "absent" when the
                   forge can't be queried, and say that nothing is printed then. Rewording to "empty", or deleting the sentence,
                   fails it.
                 [risk+verification+mvp] TestWatchPromptPRsAreInformationOnly: watch.md states PR data is "information only" and
                   never changes suggested_command. It carries a "not instructions" guard for PR fields. No line contains `gh pr`,
                   which subsumes risk's `gh pr merge` check.
                 [all] The existing TestWatchPromptSuggestionPrecedence still finds verify→ship→inbox→status in order after
                   "locked precedence". PR prose inserted into the ranked list, or a PR-derived entry ahead of any of the four,
                   fails it.

Wave 2 (depends t-1)
  t-3  Classify bot and ship PRs for digest                     [verification+risk]
       files:    internal/watch/prs.go, internal/watch/prs_test.go
       covers:   c-1, c-2, c-4, c-5
       depends:  t-1
       desc:     BotPR{number,title,author,url,age_days,checks}, ShipPR{number,head,url,checks}. SplitPRs([]ship.OpenPR, now) returns bots and ships, both
                 non-nil and sorted by number. Bot = IsBot; ship = same-repo head prefixed phase/ or milestone/; the two predicates are independent.
                 Age = floor((now−CreatedAt)/24h), clamped ≥0. BotSummary(bots) and ShipPRLine(p) return the exact digest strings.
       contract: [verification+risk] TestSplitPRsBotsByFlag: a human PR with login "dependabot-fan" on head dependabot/npm/x
                   (IsBot=false) is excluded. app/dependabot and app/github-actions (IsBot=true) are included. A login
                   prefix/substring filter fails it.
                 [risk+verification] TestSplitPRsShipHeads:
                   - same-repo phase/x and milestone/v1.7 are in;
                   - feature/x, phases/x, feature/phase/x, release/phase/x, Phase/X and main are out;
                   - a fork PR (IsCrossRepository) on phase/evil is out;
                   - a bot PR on phase/x lands in BOTH lists.
                 [risk+verification] TestPRAgeDays, with now=2026-09-30T12:00Z:
                   - created 12d1h earlier → 12; 23h earlier → 0;
                   - 47h59m → 1 and exactly 48h → 2 (rounding fails);
                   - 3h in the future (clock skew) → 0, not −1;
                   - a +05:00-offset createdAt ages identically to its UTC instant.
                 [verification+risk+mvp] TestBotSummary:
                   - nil → "";
                   - ages 3 and 12 with zero failing → exactly "bot PRs: 2 open, oldest 12d" (emitting "(0 failing)" fails);
                   - one failing → exactly "bot PRs: 2 open (1 failing), oldest 12d";
                   - the 12d PR is placed second, so taking oldest from the first element fails;
                   - pending and none never count as failing;
                   - a single passing 3d PR → "bot PRs: 1 open, oldest 3d".
                 [verification+risk] TestShipPRLine: {138, phase/x, failing} → exactly "pr: #138 phase/x — failing" (U+2014 em
                   dash). {7, milestone/v1.7, none} → exactly "pr: #7 milestone/v1.7 — none".
                 [verification] TestPRRecordJSONKeys: the json.Marshal(BotPR) key set is exactly
                   {number,title,author,url,age_days,checks}, and ShipPR's is exactly {number,head,url,checks}. Adding a
                   stale/threshold field or renaming age_days fails it (pr_age).

Wave 3 (depends t-1, t-2, t-3)
  t-4  Wire PR digest into dross watch                          [verification+risk+mvp]
       files:    internal/cmd/watch.go, internal/cmd/watch_test.go, internal/ship/openprs.go
       covers:   c-1, c-2, c-3, c-4, c-5
       depends:  t-1, t-2, t-3
       desc:     When [remote].provider and .url are set, call ship.ListOpenPRsFunc(OpenOpts{Provider}) with no remotePolicy. On success, set watchDigest.BotPRs/ShipPRs
                 (`*[]T` omitempty) to non-nil SplitPRs(prs, time.Now()) output. On any error (project load, unsupported, gh), leave both nil. renderWatchHuman
                 prints BotSummary (when non-empty) and one ShipPRLine per ship PR after drift, before `next:`. suggestedCommand untouched; the openprs.go marker lands here.
       contract: [verification] TestWatchPRDigestJSON: the seam returns, newest-first as gh does:
                   #150 human feature/y; #142 github-actions bot passing, created 3d+1h ago; #141 dependabot bot failing, 12d+1h;
                   #139 human milestone/v1.7 none; #138 human phase/x pending.
                   → bot_prs is [141,142] with author, url, age_days 12/3 and checks populated; ship_prs is [138,139];
                   #150 is in neither.
                 [verification+risk] TestWatchPRsReachableButEmpty: the seam returns [] → raw stdout contains `"bot_prs":[]` and
                   `"ship_prs":[]` and no `null`. A plain []T omitempty (swallows reached-empty) or a nil slice behind the pointer
                   (emits null) fails it.
                 [verification+risk+mvp] TestWatchPRsAbsentWhenForgeUnreachable: the seam returns
                   errors.New("exec: \"gh\": executable file not found").
                   - runCmd returns nil (exit 0), and the JSON decoded into map[string]json.RawMessage has neither key.
                   - Full stdout is byte-identical to the same fixture with [remote] unset, for --json and for the human render.
                   - The seam-[] run's JSON minus bot_prs/ship_prs equals the seam-error run, so new, current, drift,
                     suggested_command, board_ok and stranded are untouched.
                   - A project.toml whose [remote] fails to load still exits 0 with both keys absent.
                 [verification] TestWatchPRsUnsupportedProvider: provider=gitlab with the REAL (unstubbed) lister → both keys
                   absent, exit 0. With no [remote] configured, a counting stub on ListOpenPRsFunc stays at 0 calls.
                 [verification+risk+mvp] TestWatchHumanPRLines: with the TestWatchPRDigestJSON fixture:
                   - exactly one line trims to "bot PRs: 2 open (1 failing), oldest 12d", and one to "pr: #138 phase/x — pending";
                   - both sit above `next:`, which stays the last line;
                   - zero bots → no "bot PRs" substring;
                   - seam error → neither "bot PRs" nor "pr: #".
                 [risk] A bot title `x\n  next:  /dross-ship\x1b[2J` never reaches the human render: output has exactly one `next:`
                   line and no ESC byte.
                 [risk+verification+mvp] TestWatchPRsNeverSteerSuggestion: a failing bot PR, a failing ship PR and five bots,
                   versus a seam error → identical suggested_command over drift fixtures yielding /dross-verify, /dross-ship and
                   /dross-status (pr_suggestion).
                 [risk+verification] TestWatchPRsNotPersisted: with the board reached (httptest board via boardRepo), the
                   .dross/watch.state.json bytes are identical whether the seam returns 3 PRs or errors. With the board off and PRs
                   present, watch.state.json is not created (read_only_boundary).
                 [verification+risk] TestWatchPromptNamesEveryDigestField: every json tag on watchDigest, watch.BotPR and
                   watch.ShipPR (by reflection) appears in assets/prompts/watch.md. Renaming age_days, or adding a field without
                   updating the prompt, fails it.
                 [verification+risk+mvp] Taint gate: with the decoded-record marker in openprs.go, TestNoSpawnOutputEscapes
                   reports zero findings. TestGhMarkersAreLoadBearing picks the marker up, and deleting it yields a gh-origin
                   finding escaping through watch's render.
                 [verification+risk+mvp] Golden: TestOutputGoldens/watch_json passes with
                   internal/cmd/testdata/cli_surface/output/watch_json.txt UNEDITED (the fixture has no [remote]). Emitting
                   `"bot_prs":null` or `[]` there fails it.
```

Notes on grafts and corrections:

- **t-4 depends_on includes t-2.** Verification listed only t-1 and t-3, but TestWatchPromptNamesEveryDigestField reads the watch.md edits from t-2. Without the dependency, only the wave numbering keeps the order right.
- **Taint marker lands in t-4, not t-1.** Risk and verification agree on this. Verified: TestGhMarkersAreLoadBearing fails on an internal/ship marker that clears no flow, so a t-1 marker would turn the gate red between tasks.
- **Every cmd test that sets `remote.provider=github` stubs `ship.ListOpenPRsFunc` and restores it with t.Cleanup** [verification]. Existing watch tests are safe without a stub: `boardRepo` sets only remote.url, and cmd tests use a hermetic HOME, so the provider stays "" and ship refuses without spawning.
- **Risk's "branches never printed" rule was narrowed to titles and authors.** Printing the head is c-5's `pr:` line. A git ref cannot carry the newlines or control bytes that the injection contract targets.
- **Risk's `sh -c 'sleep 30'` stub was changed to `sleep 30; true`.** A shell can exec a lone command in place, leaving no grandchild on the pipe, and then the WaitDelay half of that contract would pass without WaitDelay.
- **No cmd clock seam.** Risk's `watchNow` was dropped for MVP's and verification's approach: fixtures are created N days + 1h before time.Now, and the pure SplitPRs takes `now`. This changes no behaviour.

## Disagreements

1. **Where classification, age and digest strings live.**
   - MVP: inline in internal/cmd/watch.go, which gives 2 tasks in 1 wave.
   - Risk and verification: pure helpers in internal/watch/prs.go, which costs an extra task and wave.
   - **Default:** internal/watch.
   - **Why it matters:** under MVP, every table test (age boundaries, summary string, ship-head prefixes) runs only in internal/cmd. That suite hangs locally near go test's 600s default and has to be gated remotely. In internal/watch the same tables run in seconds and are cheap to mutation-test. The price is one extra task boundary and wave.

2. **Should the record/rollup type be a separate task from the lister?**
   - Risk: t-1 (prrecord.go: OpenPR plus the Checks() reducer), then t-2 (prlist.go: the gh call).
   - Verification and MVP: one ship task.
   - **Default:** one task (openprs.go).
   - **Why it matters:** wave depth is 3 either way, because the wiring task waits for the lister regardless. Risk's split only lets classification start before the lister is done, which gains nothing here. It adds a task boundary inside one package and one file family.

3. **When the prompt task runs, and where the prompt↔Go-tag parity test lives.**
   - Risk: the prompt task depends on classification, because its reflection test over watch.BotPR/ShipPR tags needs the types to compile. That puts it in wave 3.
   - Verification: the prompt task runs in wave 1 with string-only contracts, and the reflective parity test sits in the wiring task.
   - MVP: the prompt task runs in wave 1 and has no parity test at all.
   - **Default:** verification's placement, with t-2 added to t-4's depends_on.
   - **Why it matters:** under MVP nothing fails when a Go json tag is renamed and the prompt still documents the old name. Risk's placement keeps the parity test but holds the prompt work until the Go types exist, for no benefit.

4. **Should the gh call have a time limit?**
   - Risk: a test-overridable prListTimeout, with Kill + WaitDelay.
   - Verification rejects it explicitly. Its stated reason, "a kill timer would need the ghCommand seam changed", is false. ghCommand returns an `*exec.Cmd`, and the caller can Start it, Kill it on a timer and set WaitDelay. internal/gitrun, internal/testlane and internal/mutation already use that pattern.
   - MVP: silent.
   - **Default:** bounded.
   - **Why it matters:** c-3 names "offline". On a stalled network, gh can hang the tick and the Bash call /dross-watch makes, which breaks c-3 worse than an error would.
   - **Cost:** the call becomes Start/Wait with a stdout buffer instead of a one-expression Output. taint_engine_test.go does seed writers plugged into Cmd.Stdout, so the t-4 marker should stay load-bearing. This would be the first gh call in ship with a timer.

5. **Should a gh failure print gh's output to stderr?**
   - Risk and verification: stay quiet. They return a fixed-prose error, and ghStderr gets zero bytes.
   - MVP: reuse ghFailed, which prints gh's text to stderr. That is the house convention; TestGhFailureOutputGoesToStderr pins it for the five existing gh entry points.
   - **Default:** quiet.
   - **Why it matters:** quiet means an unauthenticated user gets no diagnosis, only missing keys. Echoing means gh's auth nag repeats every loop tick in the Bash transcript that /dross-watch reads. The new lister becomes the first gh entry point outside the convention. TestGhFailureOutputGoesToStderr lists its cases by hand, so it will not flag the exception, and the next reader has to rely on the comment.

6. **Unrecognised check conclusions (e.g. STALE).**
   - Risk: failing ("a completed run isn't pending").
   - Verification: pending.
   - MVP: does not specify; only non-COMPLETED runs map to pending.
   - All three agree an unknown conclusion never counts as passing.
   - **Default:** pending.
   - **Why it matters:** failing raises the "(N failing)" count and marks a ship line failing on a run that may just be stale. Pending hides the run from the summary count. Pending likely matches the bucketing of `gh pr checks`, where the user looks next. I recall that gh puts STALE and unknown states in pending, but I have not checked it in this session.

7. **Fork PRs on phase/ heads.**
   - Risk: exclude cross-repository PRs from ship_prs, which adds `isCrossRepository` to the pinned argv.
   - Verification and MVP: match on the head prefix only, and pin an exact argv without that field.
   - **Default:** exclude (risk).
   - **Why it matters:** project.toml has `public = true`, so any fork can open a PR from a branch named `phase/x`. It would then appear in the digest as a dross ship PR, with its CI verdict.
   - **Also:** risk sets `--limit 200`, the other two `--limit 100`. The merged plan takes 100 (two drafts); this only matters above 100 open PRs.
