# mvp lens — extracted-package-test-parity

```
Phase extracted-package-test-parity — 5 tasks across 2 waves

Wave 1
  t-1  Cover backlog, inbound, milestone in-package
       files:    internal/boardsync/backlog_test.go (new), internal/boardsync/inbound_test.go (new),
                 internal/boardsync/milestone_test.go (new)
       covers:   c-2, c-1
       desc:     Package-local tests for SyncBacklog/PushBacklogItems/ReconcileBacklog/BacklogVerdictFor/
                 IssueIsDone/adoptLegacyBacklogKey, CollectInbound/EmitPullEnvelope/ReportBoardFailure, and
                 CheckMilestoneClosable/EnsureMilestoneLink/MilestoneBody, driven by sync_test.go's fakeBoard
                 (plus the existing httptest YouTrack fakes for the *forge.YouTrackClient-only milestone arms)
                 over a t.TempDir .dross tree. Test-only diff; every new func Test* name absent from
                 internal/cmd/testdata/cli_surface/tests_before.txt.
       contract: - BacklogVerdictFor: a `slug:` key with no phase dir returns BacklogUnattributable and a
                   scaffolded one BacklogResolved — inverting the os.Stat check fails the verdict table.
                 - ReconcileBacklog: closing a still-live routed mirror keeps its board.json backlog key
                   (drop it and the key-kept assertion fails); a mirror already Resolved on the board is not
                   closed again (fakeBoard.closed stays empty).
                 - adoptLegacyBacklogKey: a legacy key whose stored issue has a different title is NOT
                   adopted — fakeBoard.created gains one issue and the legacy link is untouched.
                 - CollectInbound drops linked, dismissed and marker-labelled issues (3-row table, each row
                   fails if its exclusion is removed); EmitPullEnvelope(nil, nil) prints `"issues":[]` and
                   `"error":null`; ReportBoardFailure returns the error only when humanFatal.
                 - CheckMilestoneClosable refuses a non-YouTrack client and a YouTrack client with
                   milestone_mode != "epic" (message names the mode); MilestoneBody("t","") == "t".
                 - per-file own-package coverage from `go test -coverprofile ./internal/boardsync/` is
                   >=80% for backlog.go, inbound.go and milestone.go.

  t-2  Cover reap apply and undo in-package
       files:    internal/boardsync/reap_apply_test.go (new), internal/boardsync/reap_undo_test.go
       covers:   c-2, c-1
       desc:     Package-local tests for Inventory/Apply/relabelReapedCard/dropBacklogLink/appendReapRun/
                 ValidateReapNamespaces/BoardNamespaceNames and Undo/restoreDroppedLink, using a fakeBoard
                 variant that also implements forge.StateWriter and a temp reap-log.json. Test-only diff;
                 new Test names absent from tests_before.txt.
       contract: - Apply with a card whose close is refused still closes every later card and returns an
                   error naming the refused issue id (break failure isolation -> later-card assertion fails).
                 - The journalled PriorState is the column read BEFORE the close; a journal built from the
                   post-close read-back fails the prior-state assertion.
                 - Undo on a client that is not a StateWriter returns the "no column model" refusal and the
                   fake records zero writes; on a StateWriter it restores prior column + labels, and a
                   Backlog-class card's DroppedLink is re-set in board.json (BacklogID returns the issue).
                 - ValidateReapNamespaces rejects an unknown namespace by name.
                 - per-file own-package coverage >=80% for reap_apply.go and reap_undo.go.

  t-3  Cover secretscan writers; retire cross-package acceptances
       files:    internal/secretscan/writers_test.go (new), .dross/survivors.toml
       covers:   c-2, c-1
       desc:     Table test driving ValidateWriters with one malformed entry per error arm (asserting the
                 message substring), the live Writers() registry validating clean, and ArtifactNames across
                 all three dispositions. Then `dross survivor retire` the 18 cross-package-only-coverage keys
                 (retire prunes the now-orphaned [[category]] block). New Test names absent from
                 tests_before.txt.
       contract: - each ValidateWriters arm (empty File, no disposition, >1 disposition, under-dross with no
                   Artifacts, `.dross/`-prefixed or absolute artifact, machine-local missing Path / IgnoreSeed
                   / Why, outside-dross missing Why, agent-authored not under-dross, duplicate File) has a row
                   asserting its message; negating any arm's condition fails that row or the
                   live-registry-validates-clean assertion.
                 - ArtifactNames on a 3-entry registry returns UnderDross.Artifacts, MachineLocal.Path and
                   OutsideDross.Paths — dropping any case arm fails.
                 - mutating the slice Writers() returns does not change a second Writers() call.
                 - writers.go own-package coverage >=80%; `grep -c cross-package-only-coverage
                   .dross/survivors.toml` == 0.

Wave 2 (depends t-1, t-2, t-3)
  t-4  CI per-file coverage floor with self-test
       files:    cmd/coverfloor/main.go (new), cmd/coverfloor/main_test.go (new),
                 .github/workflows/ci.yml, internal/verify/detach_test.go (new)
       covers:   c-4, c-3
       depends:  t-1, t-2, t-3
       desc:     cmd/coverfloor parses a go-cover profile with golang.org/x/tools/cover (already a direct
                 require), sums statements per file, and exits 1 naming every file under
                 github.com/Rivil/dross/internal/ but not internal/cmd/ below 50%; flat constant, no flags,
                 no allowlist; fails closed on a missing/empty profile or one with no internal/ files. ci.yml
                 `test` job: add `-coverprofile=coverage.out` to the existing go test line and a following
                 `go run ./cmd/coverfloor coverage.out` step (no upload-artifact); supply-chain-hardening
                 audit of ci.yml in the same edit per the global rule. detach_test.go covers the one
                 non-phase below-floor file (guard_shape). Finally a remote `dross test` of ./internal/cmd.
       contract: - self-test: a synthetic profile with internal/x/low.go at 49% exits non-zero naming low.go;
                   the same file at exactly 50% passes (kills a < -> <= boundary mutant).
                 - internal/cmd/foo.go at 0% and cmd/dross/main.go at 0% pass; internal/cmdx/bar.go at 0%
                   fails (the exclusion is the internal/cmd/ directory, not the string prefix).
                 - a missing file, a mode-line-only profile, and a profile holding no internal/ file each exit
                   non-zero (no vacuous pass).
                 - main_test reads ../../.github/workflows/ci.yml and fails if the test job's go test lacks
                   -coverprofile=<p> or no later step runs `go run ./cmd/coverfloor <p>` with the same <p>.
                 - detach_test: RunDetachArgv([]string{"false"}) returns non-nil and ["true"] returns nil —
                   swallowing cmd.Run's error fails the "false" case.
                 - local: `go test -coverprofile=c.out ./internal/boardsync/ ./internal/secretscan/
                   ./internal/diag/ ./internal/verify/` then `go run ./cmd/coverfloor c.out` exits 0.
                 - c-3: remote `dross test` over ./internal/cmd is green — TestCIGoTestStepsLive still finds
                   exactly one go test in job test with -race/-count=1/./... and no -run/-short/-timeout;
                   TestNoTestLost passes (a new name colliding with tests_before.txt fails it);
                   TestCLISurfacePinned and the issue_*/doctor_* e2e tests pass unedited; `git diff
                   milestone/v1.7 --stat -- '*.go' ':!*_test.go'` lists only cmd/coverfloor/main.go.

  t-5  Drain three packages; dispose every survivor
       files:    .dross/survivors.toml, internal/boardsync/sync_test.go, internal/boardsync/reap_classify_test.go,
                 internal/boardsync/reap_discover_test.go, internal/diag/trust_test.go,
                 internal/diag/diag_test.go, internal/secretscan/secretscan_test.go
                 (plus t-1/t-2/t-3's new test files where the drain places a LIVED mutant)
       covers:   c-1
       depends:  t-1, t-2, t-3
       desc:     Run `dross survivor drain --packages ./internal/boardsync,./internal/diag,./internal/secretscan`
                 detached (estimate from a go/ast mutant-site count; nohup past the ~1h48m reap). For each
                 undisposed survivor add a killing assertion in that package's own tests, or — only where
                 the drain evidence marks it ceiling-eligible (covered, reported NOT COVERED) — `dross
                 survivor accept --category` an existing gremlins ceiling category. Re-drain until clean;
                 that final run is c-1's evidence. New Test names absent from tests_before.txt.
       contract: - the final three-package drain reports 0 survivors without a disposition.
                 - none of the 18 keys retired in t-3 reappears in the final drain (writers_test.go kills
                   them in-package, not internal/cmd).
                 - every acceptance on internal/boardsync, internal/diag, internal/secretscan in `dross
                   survivor list --json` names one of gremlins-attribution-ceiling,
                   gremlins-switch-case-ceiling, const-initializer-arithmetic, gremlins-var-initializer —
                   a free-text reason or any other category fails the check; no new acceptance sits on a
                   line the drain evidence reports as coverage "not covered".
                 - each added killing assertion drives the value its mutant changes (e.g. the boundary value
                   for a CONDITIONALS_BOUNDARY on phase.go/reap.go), confirmed by that key leaving the
                   re-drain's list.
```

## Coverage

- c-1 → t-5 (drain + disposition, the proof run), t-3 (retires the 18 cross-package acceptances and adds the in-package tests that kill them), t-1, t-2 (close the NOT COVERED bulk in boardsync so the drain is mop-up, not a flood)
- c-2 → t-1 (backlog.go, inbound.go, milestone.go), t-2 (reap_apply.go, reap_undo.go), t-3 (writers.go)
- c-3 → t-4 (remote internal/cmd run after all wave-1 additions and the ci.yml change; non-test .go diff limited to cmd/coverfloor/main.go); enforced along the way by the test-only / fresh-test-name constraint on t-1, t-2, t-3, t-5
- c-4 → t-4 (checker, self-test, CI wiring, detach.go test)

## Judgment calls

- Floor runs as a CI step reusing the existing `go test` via `-coverprofile`, not as a Go test in `go test ./...`. A Go test would have to spawn a second `go test -cover` over every internal package, running those suites twice. A separate CI coverage step would add a second `go test` to job `test`, which TestCIGoTestStepsLive rejects (it requires exactly one), so its assertion would need editing. Adding one flag keeps every property that pin checks.
- Known cost: the flag instruments internal/cmd under -race too. That adds overhead to the suite nearest the 10m wall. I accepted it over re-running tests. testsummary's 300s-per-package warning will show any regression in the first CI run.
- The checker is new code in cmd/coverfloor/main.go using golang.org/x/tools/cover. I rejected internal/survivor.ParseProfile because its blocks carry no statement counts. I rejected folding the checker into cmd/testsummary because that reads the -json stream, and the profile is only complete after go test exits.
- The floor measures what the CI platform compiles. It does not check every file on disk. freespace_other.go (`!unix`) is never compiled on ubuntu. A check over files on disk would therefore need an allowlist or stay permanently red, and the spec rules out an allowlist.
- The floor fails closed on an empty profile or one with no internal files. This costs one test row, and without it a dropped `-coverprofile` would pass silently.
- The checker has no `-floor`/`-min` flag, and c-2's 80% is checked with a per-file sum over the profile instead. A threshold flag is speculative structure and the first step toward a per-file exemption.
- detach.go's test goes in t-4, not a task of its own. It is 3 statements, and it exists only to make c-4 pass without an exemption (guard_shape).
- The 18 retirements go in t-3 with writers_test.go, not in t-5. They replace the cross-package acceptance with in-package tests as one step, and un-silencing them early makes the first drain actually test them. Retire prunes the orphaned category, so nothing needs hand-editing.
- t-5 is one task even though it spans more than 5 files. I rejected splitting it by package. Each split means another long gremlins run and more edits to survivors.toml, and the test files can't be known until the drain names survivors. The listed files are where the known coverage gaps sit (phase.go, reap.go, reap_discover.go, trust.go, roadmap.go, secretscan.go).
- There is no wave-1 task to cover the non-c-2 boardsync gaps ahead of the drain (phase.go 34, reap.go 37, task.go 25 uncovered statements). c-1 only needs mutants killed, not blocks covered. Only lines with operators produce mutants, and the drain names them exactly.
- There is no separate "final drain" task. Per c1_evidence, the last re-drain in t-5 is the proof run, so a separate task would only collect evidence.
- t-4 and t-5 both wait on all of wave 1 but not on each other. t-4 needs the six files above 50% before the CI floor can be green, and t-5 needs the wave-1 tests so the drain isn't flooded with NOT COVERED. The c-3 remote run in t-4 does not cover names t-5 adds later, so t-5 carries the same tests_before.txt name constraint.
