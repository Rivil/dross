# watch-bot-prs — MVP decomposition

Lens: **MVP**. This is the smallest task set that satisfies every criterion. One `gh pr list` call feeds both lists. It goes through the gh seam `internal/ship` already owns: `screenedGH` for the secret-scanned argv, `ghFailed` for the stderr discipline, the exec-exempt `ghCommand`, and the taint-cleared decode pattern. So this phase adds no new exec site, no new audit registration and no new package. The command computes the lists and prints the human lines. The prompt documents the fields and renders them.

Pinned JSON shape (both tasks target it, which is why they can run in parallel):

```
bot_prs:  [{number, title, author, url, age_days, checks}]   // author = login
ship_prs: [{number, head, url, checks}]
checks ∈ {"passing", "failing", "pending", "none"}
forge reached, zero matches → key present as []
forge unreachable / non-github / no [remote] → key ABSENT
```

```
Phase watch-bot-prs — 2 tasks across 1 wave

Wave 1
  t-1  Query open PRs; add bot_prs/ship_prs to watch
       files:    internal/ship/openprs.go, internal/ship/openprs_test.go,
                 internal/cmd/watch.go, internal/cmd/watch_test.go
       covers:   c-1, c-2, c-3, c-4, c-5
       description:
         ship: ListOpenPRs(OpenOpts) runs one screenedGH("pr","list","--state","open",
         "--limit","100","--json","number,title,url,author,createdAt,headRefName,statusCheckRollup").
         It decodes author.login/is_bot and createdAt, and reduces statusCheckRollup to one of
         none, failing, pending or passing. Precedence is failing > pending > passing: a
         CheckRun conclusion of FAILURE/TIMED_OUT/CANCELLED/ACTION_REQUIRED/STARTUP_FAILURE or
         a StatusContext state of FAILURE/ERROR counts as failing. A CheckRun that is not
         COMPLETED, or a StatusContext that is PENDING/EXPECTED, counts as pending. An empty
         rollup is none. Any provider other than github returns ErrOpenPRListUnsupported
         without spawning. A gh failure or unparseable output returns an error, never an empty
         slice. The decoded record carries a //dross:taint-cleared marker. The exported seam is
         ListOpenPRsFunc.
         cmd: watch loads [remote], calls ListOpenPRsFunc and splits the result. is_bot
         PRs go to bot_prs, with age_days = floor((now-createdAt)/24h). PRs whose head starts
         with phase/ or milestone/ go to ship_prs. On any error both keys are omitted (omitzero,
         or a pointer slice; plain omitempty is not enough). The human digest adds one
         "bot PRs: N open[ (F failing)], oldest Dd" line only when N>0, then one
         "pr: #N <head> — <checks>" line per ship PR, both ahead of "next:".
         suggestedCommand is not touched.
       contract:
         - ship TestListOpenPRsGitHubArgv (captureGh): the argv equals the literal slice
           above. Dropping author, createdAt, headRefName or statusCheckRollup from --json,
           or dropping the explicit --limit (gh defaults to 30 and truncates "every"), fails it.
         - ship TestOpenPRRollup table: [] → none; SUCCESS → passing; SKIPPED+NEUTRAL →
           passing; IN_PROGRESS → pending; StatusContext PENDING → pending; FAILURE +
           IN_PROGRESS → failing; StatusContext ERROR → failing. Swapping the failing/pending
           precedence reddens the mixed row.
         - ship: gh exits non-zero → (nil, err), and malformed JSON → (nil, err). Provider
           gitlab, forgejo, bitbucket or "" → ErrOpenPRListUnsupported under refuseGh, so
           the unsupported path spawns nothing.
         - cmd TestWatchBotPRs (provider=github, seam stubbed with dependabot[bot]
           is_bot/failing/created 12d+1h ago, github-actions[bot] is_bot/passing/3d+1h, a human
           PR on feature/x, a human PR on phase/x pending, a human PR on milestone/v1.7): bot_prs
           has exactly the two bots, with author, url, age_days 12 and 3, and checks. No human
           PR appears in it. ship_prs is exactly phase/x and milestone/v1.7, each with checks;
           feature/x is absent.
         - cmd human render with the same stub prints exactly one line
           "bot PRs: 2 open (1 failing), oldest 12d" and "pr: #<n> phase/x — pending". With only
           the passing bot it prints "bot PRs: 1 open, oldest 3d" (no parenthetical). With zero
           bot PRs no "bot PRs:" line is printed and the raw JSON carries "bot_prs":[].
         - cmd TestWatchPRsAbsentWhenUnreachable: when the seam returns an error, the raw JSON
           (decoded into map[string]json.RawMessage) has neither a bot_prs key nor a ship_prs
           key, watch exits 0, and stdout is byte-identical to the same fixture with
           [remote].provider unset. TestOutputGoldens/watch_json stays byte-identical, because
           its fixture has no [remote] and so emits no PR keys.
         - cmd pr_suggestion lock: with a failing bot PR and a failing ship PR stubbed,
           suggested_command is still "/dross-status" on the clean fixture. Letting PRs feed
           suggestedCommand reddens it.
         - taint: TestNoSpawnOutputEscapes reddens if the decoded OpenPR record lacks its
           //dross:taint-cleared marker (gh output reaches stdout via Print).
           TestGhMarkersAreLoadBearing then covers the new marker.

  t-2  Document and render PR lines in /dross-watch
       files:    assets/prompts/watch.md, internal/cmd/watch_prompt_test.go, README.md
       covers:   c-1, c-2, c-3, c-4, c-5
       description:
         §1: add bot_prs and ship_prs to the emitted-object list, with their per-item
         fields. Both are absent when the forge was unreachable or is not GitHub, and an
         absent key means "unknown" and prints nothing. §2: add the render templates
         "bot PRs: N open (F failing), oldest Dd" (the parenthetical only when F>0; the whole
         line only when bot_prs is non-empty) and "pr: #N <head> — <checks>" once per ship PR.
         §3 and Hard rules: PR lines are information only and never change or add to
         suggested_command. README: extend the `dross watch` and `/dross-watch` rows to
         mention the bot/ship PR lines.
       contract:
         - TestWatchPromptPRFields: watch.md (lowercased, backticks stripped) contains
           "bot_prs", "ship_prs" and "age_days". The §1 bullet for bot_prs/ship_prs states
           "absent". Deleting the absent-when-unreachable sentence reddens it.
         - TestWatchPromptPRRenderLines: the §2 render block contains "bot prs:", "oldest",
           "failing)" and "pr: #". Dropping the summary template or the per-ship-PR line
           reddens it.
         - TestWatchPromptPRsNeverSuggest: watch.md states PR lines never change
           suggested_command (asserts "information only"). The existing
           TestWatchPromptSuggestionPrecedence still finds verify→ship→inbox→status in order,
           so a PR-derived entry inserted ahead of any of those four reddens it.
```

## Coverage

| Criterion | Tasks | How |
|---|---|---|
| c-1 | t-1, t-2 | t-1 queries, filters on is_bot and emits bot_prs with number, title, author, url and age_days, excluding humans. t-2 documents the field. |
| c-2 | t-1, t-2 | t-1 adds the human digest summary line (only when N>0). t-2 adds the /dross-watch render line. |
| c-3 | t-1, t-2 | t-1 omits the keys on error or on a non-github/absent provider, exits 0 and leaves the rest byte-identical. t-2 has the prompt treat an absent key as unknown. |
| c-4 | t-1, t-2 | t-1 adds the per-PR checks rollup and the failing count in the summary. t-2 adds the "(F failing)" template. |
| c-5 | t-1, t-2 | t-1 adds ship_prs (phase/ and milestone/ heads) with checks, the human "pr:" lines, and the same absence rule. t-2 adds the prompt line. |

## Judgment calls

- I chose 2 tasks, merging the ship query into the cmd wiring (4 files, 2 layers). I rejected a separate ship task because ListOpenPRs has exactly one caller and its shape is dictated by the digest. A split adds a wave and no independent deliverable.
- The query lives in internal/ship through screenedGH, not a new gh seam in internal/watch or internal/forge. ship already owns the exec-exempt, secret-scanned, taint-disciplined gh seam. A new seam needs new externalCLISeams, exec-exempt and taint registrations.
- One `gh pr list` call is partitioned client-side, not two filtered calls (bot vs head). This halves forge round-trips per /loop tick.
- The rollup reduction sits in the ship decoder next to statusCheckRollup's two node shapes (CheckRun and StatusContext), not in cmd. cmd sees only the 4-state string.
- Rollup precedence is failing > pending > passing, with empty → none. I rejected pending outranking failing: one red check makes the PR failing whatever is still running, and c-4 counts failing.
- The split, age and render code lives in internal/cmd/watch.go, not in new pure helpers in internal/watch. There is no second caller, and a new file with its own tests is speculative structure.
- Reached-with-zero is `[]` and unreached is an absent key (omitzero or a pointer slice). I rejected plain omitempty because it collapses "none" into "unknown", which c-3 forbids.
- The provider gate is in ship's dispatch (non-github returns a sentinel without spawning), matching FindOpenPRByHead and OpenPRsTargeting. I rejected a cmd-side provider switch, which would be a second copy of the locked forge_scope.
- Ship-branch detection uses the literal `phase/` and `milestone/` prefixes rather than parsing repo.branch_pattern. Every existing call site hardcodes both prefixes.
- gh failures reuse ghFailed, so gh's text goes to stderr on each failing tick. I rejected silencing it: that bypasses the audited gh-output discipline, and stdout JSON (the digest) stays clean either way.
- The query passes an explicit `--limit 100` rather than gh's default of 30. The default silently truncates c-1's "every open PR".
- There is no clock seam. Fixtures are created N days + 1h ago against time.Now. I rejected an injectable clock as speculative, since the floor arithmetic is testable without one.
- t-2 runs in wave 1, in parallel with t-1, rather than in wave 2. It needs only the field names, and this plan pins them.
- The README row edits ride in t-2 (the non-negotiable README sync convention). ARCHITECTURE.md is left to ship's landmark merge, not given a task.
- There is no change to suggestedCommand (locked pr_suggestion), and a t-1 test pins that PRs cannot move it.
