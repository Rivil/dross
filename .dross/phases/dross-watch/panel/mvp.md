# dross-watch — MVP decomposition

Lens: **MVP** — smallest task set that satisfies every criterion. The delta engine, drift classification, read-only board pull, and precedence-ranked suggestion all live in one cohesive cobra command (`internal/cmd/watch.go`, same package as `status.go`, so it reuses `readVerifyVerdict`, `phase.List/LoadPlan`, `plan.Summary/NextRunnable`, and `stateRecordsCompleted` directly — no new package, no duplicated logic). The skill is a thin renderer over the command's JSON.

```
Phase dross-watch — 2 tasks across 2 waves

Wave 1
  t-1  Add `dross watch --json` command
       files:    internal/cmd/watch.go, internal/cmd/watch_test.go, cmd/dross/main.go
       covers:   c-1, c-2, c-4, c-5, c-3 (machine half: suggested_command)
       description:
         New cobra `watch` command. Reuses openBoard() for a READ-ONLY inbound
         pull (ListIssues + IsLinked/IsDismissed filter, never --mark/MarkPulled).
         Diffs the inbound id->state set against .dross/watch.state.json to split
         new vs current (first run seeds baseline: all current, new empty; a
         reopened issue re-enters the feed and surfaces as new; a retitle does
         not). Classifies phase drift (in_progress / complete_unverified /
         verified_unshipped) with the same helpers status.go uses. Emits a JSON
         digest incl. a precedence-ranked suggested_command
         (verify->ship->inbox->status). The ONLY write is watch.state.json, and
         it is written only when the board was actually reached (board-off /
         unreachable preserves the prior baseline). Registers watch in main.go.
       contract:
         - c-1: watch_test runs `dross watch --json` against an httptest forge
           serving 2 open issues + fixture phases; asserts stdout unmarshals to a
           digest with inbound.new/current and drift buckets populated, exit code 0.
           If the JSON shape or a bucket is dropped, the unmarshal/field assertion fails.
         - c-2: seed watch.state.json with issue set {#1,#2}; second run with the
           board still serving {#1,#2} yields inbound.new == [] and current == [#1,#2];
           adding #3 yields new == [#3]. If the state diff regresses (e.g. re-flags
           carried-over items), the "zero new on unchanged second run" assertion fails.
         - c-4: the httptest handler records HTTP method+path; after `dross watch`
           it asserts only GET /issues was hit (no POST/PATCH/PUT/DELETE) and that
           board.json bytes (incl. last_pull) are byte-identical before/after. If
           watch ever calls MarkPulled or any mutating forge method, the
           method-log / board.json-unchanged assertion fails.
         - c-5: with board.enabled=false, `dross watch --json` exits 0 with a valid
           drift-only digest (board_enabled=false, inbound empty); with the board
           enabled but the httptest server returning 500 (unreachable), it still
           exits 0 (board_reachable=false, drift present) AND leaves the prior
           watch.state.json issue baseline unclobbered. If watch errors out or
           overwrites the baseline on an unreachable board, these assertions fail.
         - c-3 (machine half): precedence test — given complete_unverified non-empty
           it emits "/dross-verify"; verified_unshipped -> "/dross-ship"; else new
           inbound -> "/dross-inbox"; else "/dross-status". If precedence order
           breaks, the suggested_command assertion fails.

Wave 2 (depends t-1)
  t-2  Author the /dross-watch skill
       files:    assets/commands/dross-watch.md, assets/prompts/watch.md,
                 internal/cmd/watch_prompt_test.go
       covers:   c-3
       description:
         Command frontmatter (name, description, allowed-tools: Read + Bash only —
         non-interactive per the locked `/loop`-driven substrate, no AskUserQuestion)
         @-referencing prompts/watch.md. Prompt: pre-flight `dross rule show`, run
         `dross watch --json`, render a compact human block (new issues, carried-over
         count, drift buckets), and end with exactly the one suggested_command from
         the digest. Mirrors inbox.md's board-off degradation phrasing.
       contract:
         - c-3: watch_prompt_test asserts assets/prompts/watch.md invokes
           `dross watch --json` and instructs printing the single digest
           suggested_command verbatim, and that dross-watch.md's allowed-tools omit
           AskUserQuestion (non-interactive). If the prompt drops the command
           invocation, the one-command ending, or reintroduces interactivity, a
           token assertion fails.
```

## Coverage

| Criterion | Delivered by |
|-----------|--------------|
| c-1 (JSON digest: inbound + drift, exit 0, no mutation) | t-1 |
| c-2 (new-vs-carried diff against watch.state.json; unchanged -> zero new) | t-1 |
| c-3 (skill renders compact block + exactly one suggested command) | t-1 (computes suggested_command via precedence), t-2 (renders + prints it) |
| c-4 (only watch.state.json written; board.json/git/phases untouched) | t-1 |
| c-5 (board off/unreachable -> drift-only digest, exit 0, never errors) | t-1 |

Every criterion c-1..c-5 has a home. All locked decisions are honored: read_only_boundary (no --mark, only-write-is-watch.state.json), first_run_baseline (seed + empty new), delta_identity (id+state, reopen surfaces as new), suggestion_precedence (verify->ship->inbox->status in Go), drift_signals (reuses status.go's phase-state helpers), substrate (thin `/loop`-driven skill over `dross watch --json`).

## Judgment calls

- **One command file, not a separate `internal/watch` package.** Chose to keep the delta engine + drift classification inside `internal/cmd/watch.go`; rejected a dedicated `internal/watch` pkg. It's one cohesive layer, reuses status.go's in-package helpers verbatim (no duplication), and the c-2/delta logic is still unit-tested directly — a package would add files without adding coverage.
- **Precedence computed in Go (t-1), not in the skill.** Chose to emit `suggested_command` from the digest so the locked verify->ship->inbox->status ranking has a real Go test; rejected leaving the ranking to the prompt, which is only checkable by reading markdown.
- **Skill is wave 2, depends t-1.** Chose to author the prompt against the real emitted JSON schema; rejected parallelizing it in wave 1 — it strictly parses t-1's field names (`inbound.new`, `drift`, `suggested_command`) and getting them wrong silently breaks c-3.
- **watch.state.json is written only when the board was reached.** Chose to preserve the prior issue baseline on board-off/unreachable; rejected always rewriting it, which would seed an empty set and wrongly flag the whole backlog as "new" on the next healthy tick.
- **Drift reuses `stateRecordsCompleted` for "verified-but-unshipped".** Chose verdict==pass && no `completed <id>` in state history (pure state read, no git); rejected inventing a new shipped-signal — keeps watch git-free, trivially satisfying c-4's no-git-mutation.
- **Read-only pull via the existing default path.** Chose `ListIssues` + the IsLinked/IsDismissed filter exactly as `issue pull` does WITHOUT `--mark`; rejected adding any new board method — guarantees c-4 by construction.
```
