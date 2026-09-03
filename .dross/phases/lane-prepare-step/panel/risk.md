# risk lens — lane-prepare-step

Phase lane-prepare-step — 8 tasks across 4 waves

The graph is shaped by the failure modes, not by the layers. Each risk below is
owned by exactly one task, and the task that owns it is the task whose tests
would go red if it regressed.

| # | failure mode | owner |
|---|---|---|
| R1 | a broken bootstrap reported as a red suite (exit 1) | t-2, t-7 |
| R2 | one lane's failed prepare kills the whole run | t-6 |
| R3 | a lane that never spawns still pays for its prepare | t-6 |
| R4 | prepare+command fingerprint collides under re-splitting | t-3 |
| R5 | `lane edit` silently drops a grant or a sibling field | t-5 |
| R6 | a prepare line reaching `sh` unfenced, mid-run | t-6 |
| R7 | prepare runs here while the suite runs there | t-6 |
| R8 | a remote prepare failure classified as a red suite | t-7 |
| R9 | a run where every prepare failed reported as exit 5 / green | t-6 |
| R10 | a user granting a prepare line they were never shown | t-3, t-8 |

## Wave 1

  t-1  Add lane prepare field and validate rules
       files:    internal/project/project.go, internal/cmd/validate.go,
                 internal/cmd/validate_lane_test.go
       covers:   c-1
       depends:  —
       desc:     TestLane gains `Prepare string \`toml:"prepare,omitempty"\``.
                 laneProblems reports a whitespace-only prepare — the shape a
                 hand-edited project.toml produces, which is non-empty for the
                 fingerprint and empty for the reader.
       contract: - a lane with `prepare = "   "` is reported by `dross validate`
                   as a prepare that can never run; a lane with no prepare key
                   produces zero prepare problems
                 - a lane with Prepare empty re-saves with NO `prepare =` key, so
                   a project.toml written before this phase round-trips
                   byte-identically — a missing omitempty fails the byte compare
                 - a fixture written with `prepare = "make build"` loads into
                   TestLane.Prepare and re-saves under the same key; a mis-tagged
                   struct field fails on the reload, not on a later run

  t-2  Define exitPrepareFailed and rank it
       files:    internal/cmd/test.go, internal/cmd/test_lane_consent_test.go
       covers:   c-3
       depends:  —
       desc:     exitPrepareFailed = 7 with the comment stating why it is not
                 exit 1. exitRank gains it between exitPartial and
                 exitSuiteFailed, renumbering the family; TestExitPrecedenceIsTotal's
                 order slice gains it in that slot.
       contract: - worseOutcome(prepare-failed, red) returns 7 in BOTH argument
                   orders — a code left out of exitRank falls into the unknown
                   default rank and ties with red, which this table catches
                 - exitRank(exitPrepareFailed) sits strictly above
                   exitRank(exitSuiteFailed) and strictly below exitRank(exitPartial),
                   asserted on the ranks so renumbering cannot silently reorder it
                 - a red lane in a repo declaring no prepare still exits 1: the new
                   code is unreachable without a prepare line

## Wave 2 (depends t-1)

  t-3  Fingerprint prepare and command per lane
       files:    internal/cmd/trust.go, internal/cmd/trust_lane_test.go,
                 internal/cmd/test.go, internal/cmd/doctor.go
       covers:   c-4
       depends:  t-1
       desc:     LaneFingerprint(prepare, command): sha256(command) when prepare
                 is empty, else sha256 over the two fields' own hashes behind a
                 NUL-separated domain tag. LaneConsented / GrantLaneConsent /
                 laneConsentRefusal / trustLane carry the prepare line; the two
                 call sites in test.go and doctor.go pass lane.Prepare.
       contract: - LaneFingerprint("a", "b\nc") != LaneFingerprint("a\nb", "c") —
                   naive concatenation makes these equal, which is a lane re-split
                   across its two fields keeping a grant issued for neither
                 - a lane with no prepare fingerprints byte-identically to
                   Fingerprint(command): an already-granted lane stays granted
                   across the upgrade rather than mass-refusing on first run
                 - no (prepare, command) pair can produce a no-prepare lane's
                   fingerprint: the tagged form is unreachable from TOML because
                   its separator is NUL, asserted by hashing the tagged bytes as a
                   bare command and requiring a MISS
                 - adding a prepare to a granted lane returns ConsentStale, not
                   ConsentAbsent, and the refusal text contains BOTH the prepare
                   line and the command line — a refusal showing only the command
                   lets a user re-grant a bootstrap they never read
                 - `dross trust --lane x` prints the prepare line and the command
                   line BEFORE GrantLaneConsent writes; asserted on output order
                   against the store's mtime-free content, so a write-then-print
                   fails

  t-4  Accept --prepare on lane add and list it
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_test.go
       covers:   c-1
       depends:  t-1
       desc:     `lane add --prepare "<cmd>"` trims and writes the field;
                 `lane list` prints a `prepare:` row only for lanes declaring one.
       contract: - `lane add x --match a --command c --prepare "make build"`
                   followed by `lane list` prints a `prepare:` row for x and no
                   such row for a lane that declared none
                 - `--prepare "   "` is refused BEFORE the load-modify-save and
                   project.toml is byte-identical afterwards — the whitespace lane
                   t-1's validate rule would otherwise have to report
                 - a lane added with a prepare starts UNGRANTED: the add output
                   names `dross trust --lane x` and trusted_lane_commands has no
                   entry for x

## Wave 3 (depends t-1, t-2, t-3)

  t-5  Add `dross test lane edit --prepare`
       files:    internal/cmd/test_lane.go, internal/cmd/test_lane_edit_test.go
       covers:   c-6
       depends:  t-1, t-3, t-4
       desc:     `lane edit <name> --prepare "<cmd>"` sets or clears the prepare
                 in place, touching no other field and never calling
                 RevokeLaneConsent — the grant entry stays so the next run reads
                 STALE rather than ABSENT.
       contract: - after an edit, Match, Command, Selector and EmptyExit are
                   byte-identical and the lane keeps its position in project.toml;
                   a rebuild-the-block implementation fails the round-trip compare
                 - `--prepare ""` clears an existing prepare, while omitting
                   --prepare entirely is refused as a no-op edit — read from
                   cobra's Changed, so "not passed" and "passed empty" cannot
                   collapse into one clearing behaviour
                 - after an edit the lane's entry is still PRESENT in local.toml
                   and `dross trust --lane go --check` reports STALE, not absent:
                   remove-then-re-add's silent grant loss is the failure this verb
                   exists to fix
                 - `lane edit nope --prepare x` errors listing the declared lane
                   names and leaves project.toml unwritten

  t-6  Run each lane's prepare between sync and test
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_test.go
       covers:   c-2, c-3, c-5
       depends:  t-1, t-2, t-3
       desc:     In runTestLanes: the up-front fence sweep gains
                 shArgvFor(laneField(name), lane.Prepare); per runnable lane, after
                 the laneRunLine ok-check and before the lane header, the prepare
                 is printed and spawned through the SAME transport call the test
                 line uses. A failed prepare skips that lane's command, folds
                 exitPrepareFailed into worst, and continues the loop; it is
                 counted apart from misses.
       contract: - one lane with a prepare records exactly [prepare, command] in
                   that order; a run that spawned the command first, or spawned
                   only the command, fails on the recorder's slice
                 - the prepare line is printed before the prepare's own output and
                   before the `lane <name>:` header — asserted on byte offsets in
                   captured stdout, not on presence
                 - lane A's prepare fails: A's command is never spawned, lane B's
                   prepare AND command both spawn, and the run exits 7 — an early
                   return from the lane loop fails B's spawn assertion
                 - a consent-refused lane and a lane whose selector filtered to
                   nothing each record ZERO prepare spawns (locked
                   prepare_selector_miss)
                 - two lanes declaring the identical prepare line record it TWICE
                   (locked prepare_scope); a dedup cache fails on the count
                 - with a granted remote, the recorded remote scripts are rsync,
                   then the prepare, then the command, with zero local spawns — and
                   the prepare string is byte-identical to the one recorded under
                   --local (c-5)
                 - a prepare beginning with `-` is refused by the up-front sweep
                   naming runtime.test_lane[<name>] with zero spawns of any kind;
                   a fence moved inside the loop leaves the earlier lane already run
                 - a run where EVERY lane's prepare failed exits 7, not 5 — prepare
                   failures must not be counted by the all-missed check

## Wave 4 (depends t-6)

  t-7  Classify a remote prepare failure as exit 7
       files:    internal/cmd/test.go, internal/cmd/test_lane_prepare_remote_test.go
       covers:   c-3, c-5
       depends:  t-6
       desc:     A remote prepare failure comes back through remoteFailure, which
                 maps remote.ErrRemoteCommand to exitSuiteFailed. On the prepare
                 leg that verdict is re-tagged to exitPrepareFailed; the transport
                 and partial arms are passed through untouched.
       contract: - a remote prepare exiting non-zero is reported as exit 7 naming
                   the prepare line, not exit 1 "test suite failed on <host>" —
                   without the re-tag a dead bootstrap is indistinguishable from a
                   red suite, which is the whole reason exit 7 exists
                 - a prepare failing with ssh's transport code stays exit 3 and an
                   incomplete rsync stays exit 4: the re-tag must not swallow the
                   transport family, which outranks 7
                 - a failed remote prepare leaves NO ssh invocation carrying that
                   lane's test line: the recorded scripts hold rsync and the
                   prepare only

  t-8  Surface prepare in doctor and README
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go,
                 README.md, internal/cmd/test_lane_prepare_doc_test.go
       covers:   c-1, c-4
       depends:  t-2, t-3, t-5, t-6
       desc:     reportLaneConsent prints the prepare line beneath the command for
                 a lane declaring one, in the granted and stale rows alike. README's
                 `dross test`, `dross test lane` and `dross trust` rows gain exit 7,
                 `--prepare` and the `edit` verb.
       contract: - doctor's row for a lane declaring a prepare prints that line
                   under the command, and a lane declaring none prints no prepare
                   line — a doctor that showed only the command hides half of what
                   the stale fingerprint is about
                 - doctor's stale row for a lane whose PREPARE changed still names
                   `dross trust --lane <name>` as the fix, so the state that
                   refuses and the state doctor reports agree
                 - README's `dross test` row names exit `7` and places it between
                   partial and red in the stated precedence order; the `dross test
                   lane` row names `--prepare` and the `edit` verb — asserted by
                   reading the row text, in the shape deferred_add_test.go already
                   uses

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-4, t-8 |
| c-2 | t-6 |
| c-3 | t-2, t-6, t-7 |
| c-4 | t-3, t-8 |
| c-5 | t-6, t-7 |
| c-6 | t-5 |

6/6 criteria covered.

## Judgment calls

- **A no-prepare lane keeps the bare `Fingerprint(command)`, rather than moving
  every lane onto the new encoding.** Rejected the uniform re-encode: it is safe
  (fail-closed) but silently invalidates every existing lane grant on upgrade, so
  the first `dross test --files` after a `dross update` refuses every lane. The
  cost is two encodings, which is why t-3 owns a test that no tagged pair can
  forge an untagged fingerprint.
- **Hash-of-hashes with a NUL-tagged domain separator, not length prefixes.**
  Both defeat the re-split collision the lock names. Hash-of-hashes is
  fixed-length by construction, so the ambiguity cannot come back through an
  unusual field value, and NUL is unreachable from a TOML string — which is what
  makes the untagged/tagged separation provable rather than conventional.
- **`lane edit` does NOT revoke the grant.** The lock says the changed lane
  "re-prompts rather than silently losing its grant". Deleting the entry gives
  ConsentAbsent ("never trusted"); leaving it gives ConsentStale ("this CHANGED
  since you trusted it"), which is the louder of the two and the one that tells
  the user something was edited. Rejected revoke-on-edit for that reason alone.
- **The remote prepare re-tag is its own task (t-7), not folded into t-6.**
  It is a one-line-looking change over a classification path that already has
  three arms, and getting it wrong reproduces exactly the confusion exit 7 was
  added to remove. Splitting it means the tests that prove transport still
  outranks prepare cannot be written as an afterthought to the ordering work.
- **The prepare goes through the existing runOneLane/runRemoteLine path rather
  than a parallel spawn helper.** A second spawn site is how a prepare ends up
  running locally while the suite runs remotely, or in a different working
  directory; reusing the path makes c-5 structural instead of a thing tests have
  to keep catching.
- **Prepare failures are counted apart from selector misses.** They are not
  folded into `misses`, because `misses == len(runnable)` returns
  exitNothingMeasured — a run where every bootstrap died would then report 5
  instead of 7, and 5 ranks last.
- **The prepare is fenced in the existing up-front sweep, not at its spawn.**
  A fence at the spawn discovers lane 3's malformed prepare with lanes 1 and 2
  already built and tested; the sweep is the established pattern for exactly
  that reason.
- **No `validate` rule for a leading-dash prepare.** validate does not check
  lane.Command for it either, and adding the asymmetry would put one of the two
  lines under two gates and the other under one. The run-site fence covers both.
