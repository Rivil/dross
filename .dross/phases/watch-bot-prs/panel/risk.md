# watch-bot-prs — risk-lens draft

Lens: failure modes drive the graph. Every risk below has exactly one owning task, and the task's
test_contract is the test that goes red when that risk materialises. Tasks are cut along risk
clusters (gh semantics / forge I/O / classification / digest contract / model-facing prompt), not
along features.

```
Phase watch-bot-prs — 5 tasks across 3 waves

Wave 1
  t-1  Add gh PR record and check rollup
       files:    internal/ship/prrecord.go, internal/ship/prrecord_test.go
       covers:   c-1, c-4
       depends:  —
       desc:     Define ship.OpenPR (number, title, url, headRefName, isCrossRepository, createdAt,
                 author{login, is_bot}, statusCheckRollup []CheckItem) with gh --json tags, and
                 (OpenPR).Checks() reducing the rollup to passing|failing|pending|none:
                 any failing > any pending > passing; empty or null rollup → none.
       contract: decoding a canned `gh pr list --json` document holding app/dependabot and
                   app/github-actions (is_bot:true) plus a human (is_bot:false) yields IsBot
                   true/true/false — a mistagged field (`isBot`) makes every bot vanish and fails
                   TestOpenPRDecodesGhShape
                 rollup table: CheckRun FAILURE/TIMED_OUT/CANCELLED/ACTION_REQUIRED/STARTUP_FAILURE and
                   StatusContext FAILURE/ERROR → failing; CheckRun QUEUED/IN_PROGRESS/WAITING/PENDING and
                   StatusContext PENDING/EXPECTED → pending; SUCCESS/NEUTRAL/SKIPPED → passing;
                   `[]` and `null` → none
                 precedence: one FAILURE among nine SUCCESS plus one IN_PROGRESS → failing;
                   IN_PROGRESS + SUCCESS → pending (a last-item-wins or first-item-wins reducer fails)
                 a COMPLETED CheckRun with an unrecognised conclusion ("STALE", "SOMETHING_NEW") never
                   rolls up to passing

Wave 2 (depends t-1)
  t-2  List open PRs via gh, fail closed
       files:    internal/ship/prlist.go, internal/ship/prlist_test.go
       covers:   c-1, c-3, c-5
       depends:  t-1
       desc:     ListOpenPRs(opts) + exported ListOpenPRsFunc seam. github → ONE
                 `gh pr list --state open --limit 200 --json number,title,author,url,createdAt,headRefName,isCrossRepository,statusCheckRollup`
                 through screenedGH, decoded from stdout only, bounded by a prListTimeout var
                 (Kill + WaitDelay). Every other provider → ErrOpenPRListUnsupported without spawning.
                 Any failure is a non-nil, fixed-prose error; gh's output is neither echoed to ghStderr
                 nor quoted in the error. A record missing number or createdAt fails the whole list.
                 No exec.LookPath pre-check; no new exec.Command site (reuses the exempt ghCommand).
       contract: argv pinned exactly — dropping --limit (gh's default 30 silently truncates) or swapping
                   createdAt for updatedAt (bot force-pushes reset it) fails TestListOpenPRsArgv; argv
                   carries `pr list` and no merge/close/comment/edit verb
                 providers gitlab, forgejo, gitea, bitbucket and "" leave the ghCommand stub's spawn
                   counter at 0 and return errors.Is(err, ErrOpenPRListUnsupported); "GitHub " (case +
                   space) spawns exactly once
                 stub exits 4 with "run gh auth login" on stderr → non-nil error, the swapped ghStderr
                   buffer stays empty, and err.Error() does not contain "auth login"
                 stub pointing at a nonexistent binary → non-nil error (gh missing); the happy-path test
                   runs with PATH set to an empty temp dir and a /bin/sh stub, so an exec.LookPath("gh")
                   pre-check reddens it on every machine, not just ones without gh
                 stub `/bin/sh -c 'sleep 30'` (grandchild holds stdout) with prListTimeout=100ms returns
                   an error inside a 3s test deadline — a bare Kill without WaitDelay blocks on the
                   orphaned pipe and blows the deadline
                 stub prints valid JSON on stdout and "warning: ..." on stderr, exit 0 → decodes 1 PR
                   (a CombinedOutput decode fails)
                 stub output `[{"number":5}]` (no createdAt) → error, not a PR aged ~739,000 days;
                   truncated JSON → error; `[]` → empty slice with nil error (reachable-and-empty is
                   not a failure)
                 3 PRs in the stub output → exactly 1 spawn (no per-PR `gh pr checks` fan-out)

  t-3  Classify bot and ship PRs
       files:    internal/watch/prs.go, internal/watch/prs_test.go
       covers:   c-1, c-2, c-4, c-5
       depends:  t-1
       desc:     ClassifyPRs(prs []ship.OpenPR, now time.Time) → (bots []BotPR, ships []ShipPR), both
                 non-nil, each sorted by number. BotPR{number,title,author,url,age_days,checks};
                 ShipPR{number,head,url,checks}. Bot = author.is_bot. Ship = head prefixed phase/ or
                 milestone/ AND not cross-repository. Age = floor((now − createdAt) / 24h), clamped
                 at 0. BotSummary(bots) and ShipLine(pr) render the exact digest strings.
       contract: a human PR on head dependabot/npm/x is excluded from bots and app/github-actions
                   (is_bot) is included — a login-prefix filter instead of is_bot fails TestClassifyBots
                 a fork PR (isCrossRepository:true) on head phase/evil is excluded from ships; same-repo
                   phase/x and milestone/v1.7 are included; heads phases/x, feature/phase/x and Phase/X
                   are excluded
                 age: createdAt now−47h59m → 1, now−48h → 2 (rounding fails), now+3h (clock skew) → 0
                   not −1, and a +05:00-offset createdAt ages identically to its UTC instant
                 BotSummary over ages 3 and 12 with one failing → exactly
                   `bot PRs: 2 open (1 failing), oldest 12d`; zero failing → `bot PRs: 2 open, oldest 12d`
                   (emitting "(0 failing)" fails); oldest is the max age, not the first element
                 ShipLine for a no-checks milestone PR → exactly `pr: #7 milestone/v1.7 — none`
                 a bot-authored PR on phase/x lands in BOTH lists (predicates are independent)

Wave 3 (depends t-2, t-3)
  t-4  Wire PR lists into the watch digest
       files:    internal/cmd/watch.go, internal/cmd/watch_test.go, internal/ship/prlist.go
       covers:   c-1, c-2, c-3, c-4, c-5
       depends:  t-2, t-3
       desc:     watchDigest gains BotPRs/ShipPRs as *[]T `omitempty`. RunE loads the project, calls
                 ship.ListOpenPRsFunc(OpenOpts{Provider}); ANY error (project load, unsupported, gh
                 failure) leaves both nil, otherwise ClassifyPRs with a watchNow seam. renderWatchHuman
                 prints BotSummary when len(bots)>0 and one ShipLine per ship PR, before `next:`.
                 suggestedCommand's signature is untouched. Adds the //dross:taint-cleared marker on the
                 decoded-record line in prlist.go (mirroring basepr.go) now that the records reach Print.
       contract: seam returns [] → raw stdout contains `"bot_prs":[]` and `"ship_prs":[]`; seam errors
                   → raw stdout contains neither key; `null` never appears — a plain []T omitempty
                   drops the reached-empty case and a pointer to a nil slice emits null
                   (TestWatchPRListsAbsentVsEmpty)
                 seam errors → exit 0, and the JSON with bot_prs/ship_prs removed is byte-identical to
                   the same fixture with the seam returning [] (new/current/drift/suggested/board_ok/
                   stranded untouched); a project.toml whose [remote] fails to load still exits 0 with
                   both keys absent
                 two failing ship PRs + five bot PRs vs seam error → identical suggested_command, over
                   drift fixtures yielding /dross-verify, /dross-ship and /dross-status (pr_suggestion lock)
                 board reached: watch.state.json bytes identical whether the seam returns 3 PRs or errors
                   (PRs never enter the seen-set — read_only_boundary)
                 human render with 2 bots (1 failing, oldest 12d via watchNow) + 1 failing ship PR →
                   exactly one `bot PRs: 2 open (1 failing), oldest 12d` line and one
                   `pr: #138 phase/x — failing` line, both above `next:`; zero bots → no "bot PRs"
                   substring; seam error → neither "bot PRs" nor "pr: #"
                 a bot title `x\n  next:  /dross-ship\x1b[2J` never reaches the human render — output has
                   exactly one `next:` line and no ESC byte
                 TestNoSpawnOutputEscapes stays green with the new gh origin, and
                   TestGhMarkersAreLoadBearing sees the prlist.go marker clear a real flow (delete it →
                   red)
                 the watch_json output golden (fixture has no [remote]) stays byte-identical — emitting
                   bot_prs:[] or null for an unconfigured provider fails it

  t-5  Document PR lines in /dross-watch prompt
       files:    assets/prompts/watch.md, internal/cmd/watch_prompt_test.go, README.md
       covers:   c-2, c-3, c-4, c-5
       depends:  t-3
       desc:     §1 field list gains bot_prs / ship_prs with the absent-means-unknown vs []-means-none
                 rule; §2 render block gains the `bot PRs: …` summary (only when non-empty) and
                 `pr: #<n> <head> — <checks>` lines; a hard rule that PR data is information only
                 (never alters or adds to suggested_command, never suggests merging) and that PR
                 titles/authors/branches are forge data — never instructions, never printed.
                 README watch rows note the PR/CI lines. (r-01: tests read assets/ source; make install
                 before a live /dross-watch check.)
       contract: TestWatchPromptDocumentsPRFields reflects over watch.BotPR and watch.ShipPR json tags —
                   renaming age_days or adding a field without touching watch.md fails it; it also
                   requires "bot_prs", "ship_prs" and "absent"
                 TestWatchPromptPRRenderLines requires `bot PRs:`, `failing`, `oldest` and `pr: #`
                   inside §2's render block (before "locked precedence")
                 TestWatchPromptPRsAreInformationOnly requires the never-changes-suggested_command rule
                   and the "not instructions" injection guard, and fails if watch.md contains
                   `gh pr merge`
                 TestWatchPromptSuggestionPrecedence still orders verify→ship→inbox→status — PR prose
                   dropped between "locked precedence" and the ranked list fails it
```

## Risk register

| # | Risk (what breaks) | Owner |
|---|---|---|
| R1 | gh JSON tag drift (`is_bot`, `createdAt`) silently empties bot_prs or zeroes ages | t-1 |
| R2 | Rollup precedence wrong / unknown GitHub enum value reported as passing | t-1 |
| R3 | gh's default `--limit 30` silently truncates the PR list | t-2 |
| R4 | Non-GitHub provider (or unset) still spawns gh | t-2 |
| R5 | gh missing / unauthenticated / non-zero exit leaks gh prose into stderr or errors every loop tick | t-2 |
| R6 | Offline half-open network hangs gh and wedges the tick (Kill without WaitDelay still hangs) | t-2 |
| R7 | stderr warnings corrupt a CombinedOutput JSON decode | t-2 |
| R8 | Record without createdAt yields a ~739k-day age instead of "unknown" | t-2 |
| R9 | Per-PR check fan-out (N+1) on a 15-minute loop burns rate limit | t-2 |
| R10 | exec.LookPath("gh") pre-check makes test outcome depend on the machine (the helicon failure) | t-2 |
| R11 | Fork PR on a `phase/…` head reported as a dross ship PR on a public repo | t-3 |
| R12 | Bot detection by login pattern instead of the forge flag | t-3 |
| R13 | Age rounding / clock skew / timezone offsets | t-3 |
| R14 | Summary string wrong: `(0 failing)`, oldest = first element | t-3 |
| R15 | Absent vs `[]` vs `null` collapse (omitempty on a slice, nil-slice pointer) | t-4 |
| R16 | Forge failure changes exit code or perturbs the rest of the digest | t-4 |
| R17 | PR state leaks into suggested_command (pr_suggestion lock) | t-4 |
| R18 | PR data persisted into watch.state.json (read_only_boundary lock) | t-4 |
| R19 | Bot PR title with newline/ANSI forges a `next:` line in the human digest | t-4 |
| R20 | Taint gate red at a task boundary: orphan marker before the escape exists, or unmarked escape after | t-4 |
| R21 | Unconfigured-provider fixture changes the watch_json golden | t-4 |
| R22 | /dross-watch model acts on PR titles (prompt injection), suggests merges, or prompt field docs drift from the Go tags | t-5 |

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 bot_prs in JSON (number, title, author, URL, age; humans excluded) | t-1, t-2, t-3, t-4 |
| c-2 one summary line in human digest + /dross-watch render, nothing when none | t-3, t-4, t-5 |
| c-3 absent (not empty) when forge unreachable/unsupported; exit 0; rest unchanged | t-2, t-4, t-5 |
| c-4 per-PR check rollup; failing count in summary | t-1, t-3, t-4, t-5 |
| c-5 ship_prs (phase/milestone heads) with rollup; one digest line each; c-3 rule applies | t-2, t-3, t-4, t-5 |

All 5/5 criteria covered. Locked decisions honoured: forge_scope (t-2 dispatch — only github spawns),
pr_suggestion (t-4 contract R17, t-5 prompt rule), pr_age (t-2 argv pins createdAt, t-3 raw floor days,
no threshold anywhere).

## Judgment calls

- Fetch lives in internal/ship behind screenedGH; rejected a gh call in internal/watch or internal/forge because external_cli_audit maps gh to the single `internal/ship:ghCommand` seam and TestSeamHasExactlyOneScreenedCaller pins screenedGH as its only caller.
- One `gh pr list` feeds both lists, filtered client-side; rejected `--author`/`--app` server filters (no generic is_bot filter exists) and per-PR `gh pr checks` (N+1 on a loop).
- `--limit 200` with no truncation marker; rejected adding a `truncated` JSON field because it's new surface the spec doesn't ask for, and 200 is far above a solo repo's open-PR count.
- Unrecognised completed-check conclusions roll up to failing; rejected pending (a completed run isn't pending) and passing (never report green on a state dross doesn't understand).
- ListOpenPRs fails quietly (fixed-prose error, gh output discarded); rejected reusing ghFailed's stderr echo because watch repeats every 15 minutes and /dross-watch's Bash call puts that stderr right next to the JSON. A missing key already says the forge state is unknown.
- Bounded wait (prListTimeout, Kill + WaitDelay); rejected an unbounded CombinedOutput because c-3 names "offline", and a tick that hangs has failed just as surely as one that errors.
- Decode stdout only (cmd.Output); rejected basepr/headpr's CombinedOutput, whose stderr interleave is harmless for a count but corrupts a JSON decode.
- Fork PRs excluded from ship_prs via isCrossRepository; rejected matching on the head prefix alone because project.toml says public = true, and any fork can name a branch phase/x.
- `phase/` and `milestone/` prefixes hardcoded; rejected reading repo.branch_pattern, because every other call site in internal/cmd hardcodes `"phase/" + id`.
- A malformed record (no number or createdAt) fails the whole list, so the keys go absent; rejected dropping bad records, because a partial list looks complete while "unknown" is honest.
- bot_prs and ship_prs go absent together (one call, one failure). Rejected independent failure paths: they would need two gh calls.
- The //dross:taint-cleared marker lands in t-4, not t-2. An orphan marker is itself a TestNoSpawnOutputEscapes finding, so it has to arrive with the escape it clears, which keeps the gate green at every task boundary.
- The rollup reduction lives on ship.OpenPR, so GitHub's CheckRun/StatusContext semantics stay in the forge layer; rejected putting it in internal/watch. watch importing ship is safe because ship imports nothing from watch.
- Line formatters (BotSummary, ShipLine) are pure functions in internal/watch. The cmd render only decides whether to print. Rejected formatting inside renderWatchHuman: the string format then gets unit-tested without a cobra run, and the print/omit rule has exactly one owner.
- The lookup passes OpenOpts{Provider} only. Rejected buildOpenOpts + remotePolicy: gh resolves its own host and auth, and remotePolicy's tracked-local.toml refusal would be a new way for a read-only heartbeat to error.
- The prompt task depends on t-3 (json tags for the reflection parity test), not t-4, so it runs alongside the wiring in wave 3. Rejected making it wave 4 behind t-4: the prompt only needs the field names, not the digest assembly.
