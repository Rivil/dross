# watch-bot-prs — verification lens (design backward from test contracts)

Method: for each criterion I wrote the ideal test first, then asked which package
that test can run in cheaply, then cut the smallest task that makes it satisfiable.
Two consequences drive the shape: the classification logic lives in `internal/watch`
and `internal/ship` (fast, isolated suites) so c-1/c-2/c-4/c-5 are pure table tests,
and `internal/cmd` (the slow suite) only carries the wiring contracts that cannot be
proven anywhere else — absent-vs-empty JSON, exit 0, byte-identical degrade, and the
taint gate.

```
Phase watch-bot-prs — 4 tasks across 3 waves

Wave 1
  t-1  Add gh open-PR lister with check rollup
       files:    internal/ship/openprs.go, internal/ship/openprs_test.go
       covers:   c-1, c-3, c-4, c-5
       desc:     ListOpenPRs(opts) dispatches on provider: github runs ONE screenedGH
                 `pr list --state open --limit 100 --json number,title,url,author,createdAt,headRefName,statusCheckRollup`
                 and decodes into ship.OpenPR{Number,Title,URL,Author,IsBot,CreatedAt,HeadRefName,Checks};
                 every other provider returns ErrOpenPRListUnsupported without spawning.
                 checkRollup folds statusCheckRollup into passing|failing|pending|none.
                 Failures return (nil, err) QUIETLY (no ghFailed stderr dump). Exported
                 ListOpenPRsFunc seam for cmd tests. No taint marker yet (t-4 adds it).
       contract: TestListOpenPRsArgv — argv must equal exactly
                   ["pr","list","--state","open","--limit","100","--json","number,title,url,author,createdAt,headRefName,statusCheckRollup"];
                   dropping `--state open`, `--limit 100` or any json field fails it, and it
                   asserts `updatedAt` is NOT in the field list (pr_age: created-at only).
                 TestListOpenPRsDecodesBotFlag — canned gh doc with app/dependabot is_bot:true,
                   app/github-actions is_bot:true, human "botanist" is_bot:false decodes
                   IsBot true/true/false; a tag typo (`isBot`) or a login-substring check fails
                   on the "botanist" row.
                 TestCheckRollup (table) — [] → none; CheckRun SUCCESS+NEUTRAL+SKIPPED and
                   StatusContext SUCCESS → passing; any CheckRun FAILURE|TIMED_OUT|CANCELLED|
                   ACTION_REQUIRED|STARTUP_FAILURE or StatusContext FAILURE|ERROR → failing;
                   CheckRun QUEUED|IN_PROGRESS|WAITING or StatusContext PENDING|EXPECTED (no
                   failure) → pending; FAILURE + IN_PROGRESS → failing; unrecognised conclusion
                   "BOGUS" → pending, never passing.
                 TestListOpenPRsFailureIsNilAndQuiet — a stub gh exiting 1 printing CANARY, a
                   stub printing non-JSON, and a stub pointing at a nonexistent binary each
                   return (nil, non-nil err) — never an empty slice with nil error, which would
                   read as "reachable, zero PRs" — the error names `gh pr list`, excludes CANARY,
                   and the swapped ghStderr buffer holds zero bytes.
                 TestListOpenPRsUnsupportedProviders — gitlab, forgejo, gitea, bitbucket each
                   satisfy errors.Is(err, ErrOpenPRListUnsupported) with the ghCommand stub's
                   call count at 0 (forge_scope).
                 TestListOpenPRsEmpty — gh printing `[]` returns nil error and zero records
                   (reachable-and-empty stays distinct from failure).

  t-2  Document bot/ship PR lines in watch prompt
       files:    assets/prompts/watch.md, internal/cmd/watch_prompt_test.go, README.md
       covers:   c-2, c-3, c-4, c-5
       desc:     §1 gains bot_prs / ship_prs bullets (item fields; ABSENT, not empty, when
                 the forge can't be queried). §2 render template gains
                 `bot PRs: <n> open (<f> failing), oldest <d>d` (line only when bot_prs is
                 present and non-empty; parenthetical only when f > 0) and one
                 `pr: #<n> <head> — <checks>` per ship PR. §3 + hard rules: PR data is
                 information only and never changes suggested_command. README watch rows
                 (command table, slash-command table, roadmap line) name the PR lines.
                 Prompt is not live until `make install` (r-01); tests read assets/ directly.
       contract: TestWatchPromptBotPRLine — lowercased watch.md must contain `bot_prs`, the
                   template `bot prs: <n> open (<f> failing), oldest <d>d`, and the instruction
                   to drop the parenthetical at zero failing; deleting the template fails it.
                 TestWatchPromptShipPRLine — must contain `ship_prs` and `pr: #<n> <head> — <checks>`.
                 TestWatchPromptPRAbsentPrintsNothing — the bot_prs and ship_prs bullets must say
                   "absent" when the forge can't be queried and that nothing is printed then;
                   rewording to "empty" fails it.
                 TestWatchPromptNeverDrivesPRs — no line of watch.md contains `gh pr` (the prompt
                   never queries, merges or closes a PR itself), and the existing
                   TestWatchPromptSuggestionPrecedence verify→ship→inbox→status order still
                   holds with the PR section added above §3 (pr_suggestion).

Wave 2 (depends t-1)
  t-3  Classify bot and ship PRs for digest
       files:    internal/watch/prs.go, internal/watch/prs_test.go
       covers:   c-1, c-2, c-4, c-5
       desc:     BotPR{number,title,author,url,age_days,checks} and ShipPR{number,head,url,checks}.
                 SplitPRs([]ship.OpenPR, now) returns bots (IsBot) and ship PRs (head prefixed
                 `phase/` or `milestone/`) as independent predicates; age = floor(days since
                 CreatedAt), clamped ≥ 0. BotSummary(bots) and ShipPRLine(p) render the exact
                 digest strings so cmd and prompt share one spelling.
       contract: TestSplitPRsBotsByFlag — human PR with login "dependabot-fan" and IsBot=false
                   is excluded; dependabot + github-actions with IsBot=true are included; a
                   login-based filter fails it.
                 TestPRAgeDays — now=2026-09-30T12:00Z: created 12d1h earlier → 12; 23h earlier
                   → 0; 2h in the future (clock skew) → 0; rounding instead of flooring, or a
                   negative age, fails it.
                 TestSplitPRsShipHeads — phase/x and milestone/v1.7 are in; feature/x, phases/x,
                   release/phase/x and main are out; a bot PR headed phase/x lands in BOTH lists.
                 TestBotSummary — nil → ""; two passing, oldest 12d → "bot PRs: 2 open, oldest 12d";
                   one failing → "bot PRs: 2 open (1 failing), oldest 12d"; pending and none are
                   never counted as failing.
                 TestShipPRLine — {138, phase/x, failing} → exactly "pr: #138 phase/x — failing"
                   (U+2014 em dash).
                 TestPRRecordJSONKeys — json.Marshal(BotPR) key set is exactly
                   {number,title,author,url,age_days,checks} and ShipPR exactly
                   {number,head,url,checks}; adding a stale/threshold field or renaming age_days
                   fails it (pr_age).

Wave 3 (depends t-1, t-3)
  t-4  Wire PR digest into dross watch
       files:    internal/cmd/watch.go, internal/cmd/watch_test.go, internal/ship/openprs.go
       covers:   c-1, c-2, c-3, c-4, c-5
       desc:     When [remote].provider and .url are set, call ship.ListOpenPRsFunc; on success
                 set watchDigest.BotPRs / .ShipPRs (`*[]T`, `omitempty`) to NON-NIL slices from
                 watch.SplitPRs(prs, time.Now()); on any error, a project load failure, or no
                 remote, leave both nil. renderWatchHuman prints BotSummary (when non-empty) and
                 one ShipPRLine per ship PR, after drift and before `next:`. suggestedCommand is
                 untouched. Adds the //dross:taint-cleared marker on openprs.go's decoded-record
                 line (the render is what makes it load-bearing).
       contract: TestWatchPRDigestJSON — seam returns #141 dependabot bot failing 12d, #142
                   github-actions bot passing 3d, #150 human on feature/y, #138 human on phase/x
                   pending, #139 human on milestone/v1.7 none → bot_prs is [141,142] with author,
                   url, age_days, checks populated; ship_prs is [138,139]; #150 is in neither.
                 TestWatchPRsAbsentWhenForgeUnreachable — seam returns
                   errors.New("exec: \"gh\": executable file not found") → runCmd returns nil, the
                   JSON decoded into map[string]json.RawMessage has NO bot_prs and NO ship_prs key,
                   and full stdout is byte-identical to the same fixture with [remote] unset —
                   for --json and for the human render.
                 TestWatchPRsReachableButEmpty — seam returns zero PRs → raw stdout contains
                   `"bot_prs":[]` and `"ship_prs":[]`; a nil slice behind the pointer (emits
                   null) or a dropped pointer (omitempty swallows it) fails it.
                 TestWatchPRsUnsupportedProvider — provider=gitlab with the REAL (unstubbed)
                   lister → both keys absent, exit 0; with no [remote] configured, a counting
                   stub on ListOpenPRsFunc stays at 0 calls.
                 TestWatchHumanPRLines — human render contains exactly one line
                   "bot PRs: 2 open (1 failing), oldest 12d" and a line "pr: #138 phase/x — pending",
                   both before the `next:` line, which stays last; with zero bots no line
                   contains "bot PRs".
                 TestWatchPRsNeverSteerSuggestion — no drift, board off, seam returns a failing
                   bot PR and a failing ship PR → suggested_command == "/dross-status", the same
                   value as the seam-error run (pr_suggestion).
                 TestWatchPRsNotPersisted — board off, PRs present → .dross/watch.state.json is
                   not created; PR data never becomes watch state.
                 TestWatchPromptNamesEveryDigestField — every json tag on watchDigest,
                   watch.BotPR and watch.ShipPR appears in assets/prompts/watch.md; renaming a
                   field without updating the prompt (t-2) fails it.
                 Taint gate — with the openprs.go marker, TestNoSpawnOutputEscapes reports zero
                   findings; TestGhMarkersAreLoadBearing picks up the new marker and deleting it
                   yields a gh-origin finding escaping through watch's render.
                 Golden — TestOutputGoldens/watch_json passes with
                   internal/cmd/testdata/cli_surface/output/watch_json.txt UNEDITED (the fixture
                   has no [remote]); emitting `"bot_prs":null` or `[]` there fails it.
```

Every cmd-level test that sets `remote.provider=github` uses `boardRepo(t, url, false)` plus
`remote.provider=github` / `remote.url=https://github.com/o/r`, and stubs `ship.ListOpenPRsFunc`
with a `t.Cleanup` restore, so no watch test spawns a real `gh`. The external-CLI audit stays
green by construction, not by exemption.

## Coverage

| Criterion | Tasks | Where the deciding test lives |
|---|---|---|
| c-1 bot_prs in JSON, bots only, age in days | t-1, t-3, t-4 | is_bot decode (t-1), flag-not-login + floor age (t-3), end-to-end JSON with the human PR excluded (t-4) |
| c-2 one summary line, nothing when none | t-2, t-3, t-4 | exact string (t-3), human render once / zero lines (t-4), prompt template (t-2) |
| c-3 absent not empty, exit 0, digest unchanged | t-1, t-2, t-4 | (nil, err) on every failure mode + unsupported providers (t-1), absent keys + byte-identical stdout + untouched golden (t-4), prompt "absent" wording (t-2) |
| c-4 check rollup + failing count | t-1, t-2, t-3, t-4 | rollup table (t-1), "(1 failing)" count (t-3), per-PR `checks` in JSON + human (t-4), template (t-2) |
| c-5 ship_prs + one line per PR | t-1, t-2, t-3, t-4 | headRefName decode (t-1), prefix filter + line string (t-3), JSON + render + absent rule (t-4), template (t-2) |

Locked decisions: forge_scope → t-1 (non-GitHub never spawns gh), t-4 (gitlab → keys absent);
pr_suggestion → t-2 (prompt precedence unchanged), t-4 (suggested_command unchanged by failing
PRs); pr_age → t-1 (createdAt requested, updatedAt not), t-3 (floor days, no stale field in the
JSON key set).

5/5 criteria covered.

## Judgment calls

- **Quiet failure in the lister**, not the `ghFailed` stderr dump. Rejected the house convention because a `/loop` heartbeat with gh unauthenticated would reprint gh's auth nag every tick into the same Bash transcript `/dross-watch` parses; the absent key is the signal. Pinned by a zero-bytes-on-ghStderr assertion so it can't drift back.
- **Taint marker lands in t-4, not t-1.** Until watch prints a title nothing escapes, so a t-1 marker would be an orphan and TestNoSpawnOutputEscapes would go red mid-phase. The marker ships with the task that makes it load-bearing.
- **Logic in internal/watch + internal/ship, wiring only in internal/cmd.** Rejected putting the partition/formatting in cmd/watch.go: its contracts would then only run in the slow internal/cmd suite that hangs locally. The pure tables run in seconds.
- **watch imports ship.OpenPR (t-3 is wave 2)** rather than a duplicated watch-owned input type plus a mapping loop in cmd. The mirror type would buy wave-1 parallelism for a ~100-line task, at the cost of a second struct and extra mapping code for mutation to cover.
- **Provider dispatch in ship (sentinel error), and cmd only checks that [remote] is configured.** This mirrors FindOpenPRByHead/OpenPRsTargeting. Rejected a GitHub check in cmd because it would duplicate the switch.
- **`*[]T` + `omitempty` for absent vs empty.** Rejected json.RawMessage and a custom MarshalJSON. The reachable-but-empty test pins the one trap (a nil slice behind the pointer marshals to `null`).
- **One gh call feeds both lists.** Rejected separate bot and ship queries: two spawns per tick, and two failure modes that could disagree about reachability.
- **`--limit 100`.** gh's default of 30 truncates silently. Rejected pagination: an information-only heartbeat doesn't need it, and the argv test pins the flag.
- **No exec.LookPath preflight.** A missing gh shows up as the exec error the seam can reproduce. A LookPath call would make test outcomes depend on the machine, which is the helicon bug external_cli_audit_test.go exists for.
- **Rollup precedence: failing > pending > passing. CANCELLED counts as failing, and an unknown state counts as pending.** An unrecognised enum must never read as green.
- **`(N failing)` only when N > 0.** I read c-2's format and c-4's format as one template, so a clean set prints c-2's exact line.
- **Bot and ship predicates are independent.** A bot PR on `phase/x` shows in both lists, because the spec defines the two sets separately. Rejected if/else exclusivity.
- **The `phase/` / `milestone/` prefixes are hard-coded** to match the rest of internal/cmd, which hard-codes "phase/" everywhere. Rejected reading repo.branch_pattern, which nothing else honours.
- **The rollup word is printed verbatim, "none" included** (`pr: #139 milestone/v1.7 — none`), so JSON and human output use the same vocabulary. Rejected a "no checks" rewording.
- **No `--repo` flag and no gh timeout**, consistent with ship's existing gh calls (which rely on cwd and have no timer). A kill timer would need the ghCommand seam changed, which is out of scope for an information-only line.
- **README goes into the prompt task.** ARCHITECTURE.md is not a task, because `dross ship` merges landmarks into it.
