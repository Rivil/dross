# lane-prepare-step — verification lens

Phase lane-prepare-step — 7 tasks across 4 waves

Wave 1
  t-1  Add lane prepare field, add and list
       files:    internal/project/project.go, internal/cmd/test_lane.go,
                 internal/cmd/validate.go, internal/cmd/test_lane_test.go,
                 internal/cmd/validate_lane_test.go
       covers:   c-1
       desc:     Adds `Prepare string` to project.TestLane with `toml:"prepare,omitempty"`,
                 a `--prepare` flag on `dross test lane add` (TrimSpace-normalized like
                 Command), a `prepare:` row in `dross test lane list` printed only when
                 declared, and a laneProblems rule for a hand-edited empty prepare.
       contract: - `dross test lane add p --match a --command c --prepare "make build"`
                   then project.Load: lane.Prepare == "make build"; a flag that is parsed
                   but not written into the block fails TestLaneAddWritesPrepare.
                 - a lane added WITHOUT --prepare writes no `prepare =` key at all — grep
                   the saved project.toml; losing omitempty fails the round-trip test that
                   asserts a lane-less-prepare repo is byte-identical to one written today.
                 - `dross test lane list` prints "  prepare: make build" for the declaring
                   lane and NO prepare row for its neighbour; dropping the field from the
                   listing fails TestLaneListPrintsPrepare.
                 - a hand-appended `prepare = ""` block is reported by `dross validate`
                   naming that lane; a block with the key absent produces no problem line.
                 - `dross test lane add p --prepare "   "` stores no prepare (whitespace
                   normalizes to absent) and project.toml gains no `prepare =` key.
       depends:  —
       status:   pending

  t-2  Rank a prepare failure above a red suite
       files:    internal/cmd/test.go, internal/cmd/test_lane_consent_test.go
       covers:   c-3
       desc:     Declares exitPrepareFailed = 7 and slots it into exitRank between
                 exitPartial and exitSuiteFailed, renumbering the ranks above it.
                 No spawn-site change — the constant and its order only.
       contract: - TestExitPrecedenceIsTotal's `order` slice becomes
                   {transport, partial, prepare, suiteFailed, laneRefused, nothingMeasured}
                   and the existing table drives every co-occurring pair in BOTH argument
                   positions: any rank swap fails it, in both directions.
                 - worseOutcome(tagged(7), tagged(1)) exits 7 — a failed bootstrap beside a
                   red suite must not be reported as the red suite.
                 - worseOutcome(tagged(3), tagged(7)) exits 3 — a dead transport still
                   outranks a prepare failure, because nothing was measured either way and
                   the host is the thing to go and look at.
                 - exitRank(exitPrepareFailed) is strictly between exitRank(exitPartial)
                   and exitRank(exitSuiteFailed); a prepare that ranked at unknown's
                   default (== red's rank) fails the strict comparison.
       depends:  —
       status:   pending

Wave 2 (depends t-1)
  t-3  Fingerprint prepare into the lane grant
       files:    internal/cmd/trust.go, internal/cmd/test.go, internal/cmd/doctor.go,
                 internal/cmd/trust_lane_test.go
       covers:   c-4
       desc:     Adds laneConsentInput(lane) — a length-framed encoding of prepare plus
                 command — and moves LaneConsented/GrantLaneConsent onto it, updating the
                 three call sites (run site, trustLane, reportLaneConsent). trustLane and
                 doctor print the prepare line alongside the command before granting.
       contract: - laneConsentInput({prepare:"a", command:"bc"}) !=
                   laneConsentInput({prepare:"ab", command:"c"}); a naive concatenation
                   collides here and fails TestLanePrepareFingerprintIsUnambiguous.
                 - a lane declaring NO prepare fingerprints to Fingerprint(lane.Command)
                   byte-for-byte: a grant written into local.toml before this phase still
                   reads ConsentGranted. A framing applied unconditionally fails this and
                   would silently stale every existing lane grant on every machine.
                 - grant a lane, then append a prepare to it: LaneConsented returns
                   ConsentStale (not ConsentGranted, not ConsentAbsent) and
                   `dross trust --lane go --check` exits non-zero carrying the "has
                   CHANGED since you trusted it" wording.
                 - `dross trust --lane go` output contains the prepare line AND the command
                   line, with the prepare's index before the "recorded in .dross/local.toml"
                   line — a grant that wrote before printing both fails the ordering assert.
                 - `dross doctor`'s row for an untrusted lane that declares a prepare prints
                   the prepare line as well as the command; printing only the command fails
                   TestDoctorLaneRowNamesPrepare, since the user cannot otherwise see which
                   of the two lines the grant went stale on.
       depends:  t-1
       status:   pending

  t-4  Run each lane's prepare before its command
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_test.go
       covers:   c-2, c-5, c-4
       desc:     In runTestLanes: fence every matched lane's prepare in the same up-front
                 sweep as its command, and inside the run loop — after the selector-miss
                 check, after the sync — spawn the lane's prepare through the same
                 transport helper the command uses, printing its line first.
       contract: - remote fixture, one prepared lane: the recorded remote spawns are rsync,
                   then an ssh script carrying the prepare line, then an ssh script carrying
                   the command. A prepare hoisted above the sync fails the index assert.
                 - two prepared lanes interleave PER LANE: recorded lines are
                   [prep-go, go-cmd, prep-docs, docs-cmd] — a batched
                   [prep-go, prep-docs, go-cmd, docs-cmd] fails it.
                 - two lanes declaring the IDENTICAL prepare line spawn it twice (locked
                   prepare_scope); a dedup that ran it once fails the count assert.
                 - transcript order: index("lane go prepare: make build") <
                   index("lane go: go test -count=1 ./...") < index of the spawn seam's
                   LANE-OUTPUT-SENTINEL.
                 - `--local` against a granted-remote fixture records exactly
                   [prepare, command] on the local seam and nothing on the remote seam, and
                   the recorded prepare string is equal to the one carried inside the remote
                   script for the same fixture — a prepare wired only into the remote leg
                   fails the local half, a rebuilt-per-transport line fails the equality.
                 - a lane whose selector filtered down to nothing records NO prepare spawn,
                   and neither does an ungranted lane — the recorder holds neither its
                   prepare nor its command (locked prepare_selector_miss).
                 - a prepare beginning with a dash is refused by the up-front fence naming
                   `runtime.test_lane[go]`, with zero spawns of any kind; a fence check
                   moved inside the loop fails the zero-spawn assert on a two-lane fixture.
       depends:  t-1
       status:   pending

Wave 3
  t-5  Fail a lane on its prepare, spare the rest
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_test.go
       covers:   c-3
       desc:     Classifies a prepare spawn failure as ExitCodeError{exitPrepareFailed},
                 on both transports, skips that lane's command, folds the result through
                 worseOutcome and continues to the next lane.
       contract: - the docs lane's prepare fails: docsCmd appears NOWHERE in the recorder,
                   goCmd does, and the run exits 7. A `return` instead of a `continue`
                   fails the goCmd assert; a missing skip fails the docsCmd assert.
                 - the go lane's suite goes red AND the docs lane's prepare fails: the run
                   exits 7, not 1 — proving the result was folded through worseOutcome
                   rather than left to the last lane.
                 - the failure message names the lane and the prepare line and does not
                   read as `test lane %q failed`; a shared wrapper fails the substring
                   assert that the two failure kinds are distinguishable in the transcript.
                 - a prepare failing over ssh with a remote-command exit status exits 7, NOT
                   the exitSuiteFailed that remoteFailure hands back for the command leg —
                   reusing remoteFailure verbatim on the prepare leg fails this and reports
                   a broken bootstrap as a red suite.
                 - a TRANSPORT failure (ssh 255) during a prepare still exits 3, not 7: the
                   prepare classification must sit inside the remote-command arm only.
       depends:  t-2, t-4
       status:   pending

  t-6  Add `dross test lane edit --prepare`
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go
       covers:   c-6
       desc:     New `edit <name> --prepare "<cmd>"` verb under `dross test lane`: rewrites
                 only that lane's Prepare in place (set or clear), leaves the grant entry
                 alone, and prints the re-trust line when the fingerprint changed.
       contract: - after `dross test lane edit go --prepare "make build"` the reloaded
                   lane's Match slice and Command are byte-identical to their pre-edit
                   values and only Prepare differs (whole-struct compare) — a
                   remove-then-append implementation reorders the block and fails the
                   declaration-order assert against its neighbour lane.
                 - `dross test lane edit go --prepare ""` leaves no `prepare =` line for
                   that lane in the saved project.toml.
                 - after an edit that changed the fingerprint, local.toml still HOLDS a
                   trusted_lane_commands entry for that lane and `dross trust --lane go
                   --check` fails with the "has CHANGED" wording, not the "not been
                   trusted" wording. Calling RevokeLaneConsent here fails both halves.
                 - the edit prints `dross trust --lane go` when the fingerprint changed and
                   prints no trust instruction when the edit was a no-op (same value
                   re-set) — a message printed unconditionally fails the no-op case.
                 - `dross test lane edit nope --prepare x` errors listing the declared lane
                   names and leaves project.toml byte-identical (content compare before and
                   after).
                 - `dross test lane edit go` with no --prepare flag errors instead of
                   rewriting the file; a nil-default implementation that treats "absent" as
                   "clear" fails this by silently dropping an existing prepare.
       depends:  t-1, t-3
       status:   pending

Wave 4 (depends t-4, t-5, t-6)
  t-7  Document prepare, exit 7 and lane edit
       files:    README.md, assets/prompts/execute.md,
                 internal/cmd/readme_doc_test.go, internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-6
       desc:     Updates the `dross test` and `dross test lane` README rows and execute.md's
                 exit-status list, and adds the grep gates that keep both true.
       contract: - a new TestReadmeDocumentsLanePrepare fails, naming the missing needle, if
                   the `dross test lane` row loses `--prepare` or the `edit` verb, or if the
                   `dross test` row's exit list omits `7`.
                 - a new assertion in execute_prompt_test.go fails if execute.md's
                   exit-status list carries no **7** entry, or if its "the run did not
                   happen ... none of them is a reason to commit" sentence still enumerates
                   only 2, 3, 4, 5 and 6 — an agent that reads 7 as a red suite goes hunting
                   a bug in code that never ran.
       depends:  t-4, t-5, t-6
       status:   pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-7 |
| c-2 | t-4 |
| c-3 | t-2, t-5, t-7 |
| c-4 | t-3, t-4 |
| c-5 | t-4 |
| c-6 | t-6, t-7 |

6/6 criteria covered.

## Judgment calls

- **A prepare-less lane fingerprints to `Fingerprint(lane.Command)` exactly**, not to the
  framed encoding. Chose grant survival across the upgrade; rejected framing every lane
  unconditionally, which is tidier but silently stales every existing lane grant on every
  machine — a re-consent prompt no criterion asks for and no user would understand.
- **`lane edit` leaves the stale grant entry in place** rather than calling
  RevokeLaneConsent. Chose the "its command has CHANGED since you trusted it" refusal over
  the "has not been trusted on this machine" one; the locked why names silent grant loss as
  the bug this verb exists to fix, and revoking reproduces it with extra steps.
- **c-5 is proved structurally, not by a parallel test task**: prepare goes through the same
  transport helper the lane's command already uses, so local/remote parity is a property of
  the call site rather than of an assertion. Rejected a dedicated wave-3 parity task — it
  would have been test-only, and a parity test over two separate code paths pins today's
  agreement without preventing tomorrow's divergence.
- **The prepare leg gets its own remote classification (t-5)** instead of reusing
  remoteFailure. Rejected reuse: remoteFailure maps a remote-command exit to
  exitSuiteFailed, so a failed bootstrap on the granted host would report as a red suite —
  precisely the collision exit 7 was locked to prevent.
- **Whitespace-only `--prepare` normalizes to absent; only a hand-edited `prepare = ""`
  is a validate problem.** Rejected refusing whitespace at the CLI: `--prepare ""` is the
  documented clear in c-6, and a trim rule that refused would make clearing depend on how
  many spaces were typed.
- **The exit taxonomy is a wave-1 task of its own (t-2).** The rank order is testable with
  no lane machinery at all, through the table TestExitPrecedenceIsTotal already drives.
  Folding it into the run-site task would land the constant and its only consumer in one
  commit, with the ordering proved only indirectly by an end-to-end run.
- **t-4 and t-5 are split though both edit internal/cmd/test.go**, and sequenced rather
  than parallel: "the other lanes still run" is not a meaningful assertion until a prepare
  spawns at all, so the isolation contract needs t-4's output to be checkable.
- **t-3 and t-4 share internal/cmd/test.go inside wave 2** — different hunks (t-3 owns the
  LaneConsented call, t-4 owns the run loop). Flagged for the judge: if the merged plan runs
  waves concurrently rather than as a dependency order, t-4 should drop to depend on t-3.
- **Docs get grep gates, not just prose.** Nothing currently pins execute.md's exit-code
  list, so an unpinned list drifts the moment a code is added; the gate is what makes t-7 a
  task rather than a chore.
