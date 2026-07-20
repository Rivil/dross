# dross-watch — RISK lens

Failure modes drive this graph. Every way a watch tick can break — a missing or
corrupt `watch.state.json`, a partial write, a reopened vs merely-retitled issue,
a disabled board, an unreachable board, an accidental board.json/last_pull/git
mutation — is owned and tested by exactly one task. The pure logic (state IO,
delta, drift, suggestion, digest) lives in a new `internal/watch` package so each
failure mode is unit-testable without the cobra/forge layer; the command
(`internal/cmd/watch.go`) only wires them and enforces the no-mutation boundary.

```
Phase dross-watch — 8 tasks across 3 waves

Wave 1
  t-1  Watch state load/save: missing, corrupt, atomic
       files:    internal/watch/state.go, internal/watch/state_test.go
       covers:   c-2, c-4
       contract: if Load treats a missing watch.state.json as an error instead of
                 an empty baseline, TestLoadMissingIsBaseline fails; if malformed
                 JSON returns an error instead of degrading to an empty baseline,
                 TestLoadCorruptDegrades fails; if Save writes the file in place
                 rather than temp-file+rename, TestSaveAtomicRename (a simulated
                 mid-write interruption must leave the prior file intact) fails.

  t-2  Board delta by readable-id + open/closed identity
       files:    internal/watch/delta.go, internal/watch/delta_test.go
       covers:   c-2
       contract: if a retitled issue (same id, same state) is flagged new,
                 TestDiffRetitleNotNew fails; if a reopened issue (identity flips
                 closed→open) is NOT flagged new, TestDiffReopenIsNew fails; if a
                 first run against an empty baseline reports any new items,
                 TestDiffFirstRunEmptyNew fails; if a second identical run reports
                 nonzero new, TestDiffNoChangeZeroNew fails.

  t-3  Phase-drift classifier reusing status signals
       files:    internal/watch/drift.go, internal/watch/drift_test.go
       covers:   c-1
       contract: if a phase with all tasks done and no verify.toml isn't
                 classified complete-but-unverified, TestDriftUnverified fails; if
                 a phase with verify verdict=pass but no ship/changes record isn't
                 verified-but-unshipped, TestDriftUnshipped fails; if a phase with
                 a runnable task isn't in-progress, TestDriftInProgress fails; a
                 phase dir with a missing/garbled plan.toml must not panic
                 (TestDriftMissingPlanTolerated).

  t-4  Suggestion precedence ranker
       files:    internal/watch/suggest.go, internal/watch/suggest_test.go
       covers:   c-3
       contract: if the ranking isn't verify→ship→inbox→status, TestSuggestPrecedence
                 fails (e.g. complete-but-unverified AND new issues present must
                 yield the verify command, not /dross-inbox); if the function ever
                 returns empty or more than one command, TestSuggestExactlyOne fails;
                 with nothing pressing and no new issues it must return /dross-status
                 (TestSuggestIdleIsStatus).

  t-5  Read-only inbound board source with degradation
       files:    internal/cmd/watch.go, internal/cmd/watch_board_test.go
       covers:   c-4, c-5
       contract: if fetchInbound ever calls MarkPulled/board.Save (mutating
                 last_pull), TestFetchLeavesBoardByteIdentical fails (board.json
                 hash compared before/after); if board.enabled=false returns an
                 error or nonzero status instead of (nil, skipped, nil),
                 TestFetchDisabledSkips fails; if a ListIssues transport error
                 propagates instead of degrading to skipped-unreachable with a nil
                 error, TestFetchUnreachableSkips fails; if it stops filtering
                 linked/dismissed issues, TestFetchFiltersLinkedDismissed fails.

Wave 2 (depends t-2, t-3, t-4)
  t-6  Digest assembler + JSON schema
       files:    internal/watch/digest.go, internal/watch/digest_test.go
       covers:   c-1, c-2
       depends:  t-2, t-3, t-4
       contract: if the marshaled digest drops either the board-inbound set or the
                 phase-drift set, TestDigestJSONShape fails; if the new/current
                 partition in the digest doesn't equal what Diff returned,
                 TestDigestCarriesDelta fails; if the digest's suggested_command
                 field isn't the single value the ranker produced,
                 TestDigestEmbedsSuggestion fails; if the returned next-baseline
                 doesn't reflect the current issue set (so the next tick re-flags
                 everything), TestDigestAdvancesBaseline fails.

Wave 2 (depends t-1, t-5, t-6)
  t-7  Wire `dross watch --json` command
       files:    internal/cmd/watch.go, internal/cmd/watch_test.go, cmd/dross/main.go
       covers:   c-1, c-4, c-5
       depends:  t-1, t-5, t-6
       contract: if a watch run writes anything but .dross/watch.state.json,
                 TestWatchOnlyWritesStateFile fails (phase files + board.json hashed
                 before/after; git status must be clean); if `--json` exits nonzero
                 or emits non-parseable JSON on the happy path, TestWatchJSONExitZero
                 fails; if a board failure (disabled or unreachable) makes the run
                 exit nonzero instead of emitting a drift-only digest and exit 0,
                 TestWatchBoardDownStillExitsZero fails; if the command isn't
                 registered on root, TestWatchRegistered fails.

Wave 3 (depends t-7)
  t-8  /dross-watch skill: render + single suggestion + board-off branch
       files:    assets/commands/dross-watch.md, assets/prompts/watch.md,
                 internal/cmd/watch_prompt_test.go
       covers:   c-3, c-5
       depends:  t-7
       contract: if watch.md drops the `dross watch --json` invocation or the
                 compact-render instruction, TestWatchPromptMandatedSections fails;
                 if it doesn't instruct ending with exactly the digest's single
                 suggested_command, the "single suggested command" sub-case fails;
                 if it lacks the board-off / board-unreachable degradation branch
                 (mirroring inbox.md's board.enabled=false skip), the "board-off
                 degradation" sub-case fails; frontmatter shape asserted against
                 dross-inbox.md's.
```

## Coverage
- c-1  → t-3, t-6, t-7
- c-2  → t-1, t-2, t-6
- c-3  → t-4, t-8
- c-4  → t-1, t-5, t-7
- c-5  → t-5, t-7, t-8

## Judgment calls
- Split state IO (t-1), delta (t-2), and drift (t-3) into three wave-1 pure files rather than one `watch.go` blob — each owns a distinct failure mode (corrupt-file, mis-identity, mis-classification) so a regression fails one named test, not a coarse pass/fail.
- Put the read-only board fetch (t-5) in wave 1, not folded into the command — the "board.json byte-identical / no last_pull mutation" risk (c-4) and the "skip on disabled/unreachable" risk (c-5) are the fetch's alone, testable via a client seam without a live server; the command (t-7) then owns only the git/phase-file non-mutation boundary.
- Made `suggested_command` a field the Go layer computes and the digest carries (t-4→t-6), so c-3's "exactly one, ranked" guarantee is enforced by a unit test, not left to the markdown skill; rejected computing precedence inside the prompt, which is untestable.
- Honored the locked 4-way precedence (verify→ship→inbox→status) over c-3's narrower inbox-vs-status wording — the decision block expands the criterion and is non-negotiable.
- Chose temp-file+rename for the single state write (t-1) over an in-place `os.WriteFile` — a crash mid-write on the sole mutable file must not corrupt the baseline that all delta correctness (c-2) depends on; rejected trusting WriteFile atomicity.
- Gave the skill (t-8) a real `watch_prompt_test.go` content-gate mirroring secure_prompt_test.go, rather than leaving c-3/c-5's skill half untested — a deleted degradation section then fails exactly that sub-test.
- Kept the board-fetch helper (t-5) and the command wiring (t-7) in the same `internal/cmd/watch.go` across waves (t-7 builds on t-5's file) instead of a throwaway third file — sequential same-file edits are clean here and avoid an extra indirection.
