# Panel draft — verification lens

Designed backward from acceptance: each criterion's ideal test contract written
first, then the smallest task that makes that contract satisfiable.

```
Phase test-lane-config — 8 tasks across 3 waves

Wave 1
  t-1  Add test_lane schema and validate rule
       files:    internal/project/project.go
                 internal/project/project_test.go
                 internal/cmd/validate.go
                 internal/cmd/validate_lane_test.go
       covers:   c-1
       contract: a project.toml carrying `[[runtime.test_lane]] name="go"
                 match=["internal/**"] command="go test ./..."` round-trips
                 through project.Load into a 1-element Runtime.TestLane —
                 TestTestLaneDecodes fails on a zero-length slice if the toml
                 tag or the field is dropped
       contract: a lane declaring name+match but no command makes `dross
                 validate` exit non-zero with a problem line containing that
                 lane's name — TestValidateNamesLaneMissingCommand fails if the
                 per-field presence check is dropped or the message is generic
       contract: a lane with an empty `name` is reported as
                 `runtime.test_lane[0]`, not as `""` —
                 TestValidateIdentifiesNamelessLaneByIndex fails if the
                 offending lane becomes unidentifiable
       contract: the existing TestTomlFieldsCarryMatchingJSONTags walk over
                 project.Project fails if TestLane's fields carry a toml tag
                 without an identical json tag

  t-2  Add lane path matcher and selector
       files:    internal/testlane/match.go
                 internal/testlane/match_test.go
       covers:   c-2
       contract: `internal/**/*.go` matches `internal/cmd/test.go` and does not
                 match `main.go` — TestDoubleStarCrossesDirectories fails if
                 `**` collapses to a single path segment
       contract: `docs/*.md` does not match `docs/api/v1.md` —
                 TestSingleStarStopsAtSeparator fails if `*` is allowed to cross
                 a `/`
       contract: with lane 0 = ["internal/**"] and lane 1 = ["**/*.go"], the
                 path `internal/cmd/test.go` yields indices [0 1] exactly once
                 each, in declaration order — TestSelectIsOrderedAndDeduped
                 fails on a duplicate or on array-position precedence
       contract: Select returns every path matching no lane in `unmatched` —
                 TestSelectReportsUnmatchedPaths fails if a miss is dropped
                 rather than returned

Wave 2 (depends t-1, t-2)
  t-3  Add dross test lane add/list/remove
       files:    internal/cmd/test_lane.go
                 internal/cmd/test_lane_test.go
       covers:   c-9
       depends:  t-1
       contract: `dross test lane add go --match 'internal/**' --command 'go
                 test ./...'` followed by a fresh project.Load yields exactly one
                 lane with those three values — TestLaneAddPersists fails if the
                 Save is skipped or a field is dropped on the way to disk
       contract: adding a second lane named `go` exits non-zero naming the
                 existing lane and leaves project.toml with one lane —
                 TestLaneAddRejectsDuplicateName fails if the duplicate is
                 appended
       contract: `dross test lane remove docs` with `go` and `docs` declared
                 leaves `go` intact — TestLaneRemoveKeepsOtherLanes fails if the
                 removal truncates the slice
       contract: `dross test lane remove nosuch` exits non-zero naming the
                 unknown lane — TestLaneRemoveUnknownIsAnError fails if a no-op
                 reports success
       contract: `dross test lane list` prints each lane's name, its match globs
                 and its command line — TestLaneListShowsCommand fails if the
                 command is omitted, which is the value consent binds to

  t-4  Store per-lane consent fingerprints
       files:    internal/cmd/local.go
                 internal/cmd/trust.go
                 internal/cmd/trust_lane_test.go
       covers:   c-6
       depends:  t-1
       contract: `dross trust --lane go` writes sha256 of that lane's command
                 into local.toml's trusted_lane_commands and leaves
                 trusted_test_command and trusted_run_commands byte-unchanged —
                 TestTrustLaneLeavesOtherGrants fails if the grants share a set
       contract: granting `docs` after `go` keeps `go`'s fingerprint in the set —
                 TestTrustLaneAccumulates fails if the second grant replaces the
                 first
       contract: editing the `go` lane's command after granting makes
                 LaneConsented("go") return false —
                 TestLaneConsentGoesStaleOnEdit fails if consent binds to the
                 lane NAME rather than to the command line (locked lane_consent)
       contract: `dross local set trusted_lane_commands <hash>` is rejected as an
                 unknown key — TestLaneGrantKeyIsNotSettable fails if the key
                 leaks into localKeys and the generic writer can grant consent
       contract: `dross trust --lane nosuch` exits non-zero naming the unknown
                 lane rather than writing a fingerprint of the empty string

  t-5  Dispatch --files to the resolved lanes
       files:    internal/cmd/test.go
                 internal/cmd/test_files_test.go
       covers:   c-2, c-3, c-4, c-5, c-8
       depends:  t-1, t-2
       contract: with a `go` lane and a `docs` lane declared, `dross test --files
                 internal/cmd/test.go` records exactly one spawned command line
                 through the spawnLocal seam, and the docs lane's command string
                 appears in none of the recorded lines —
                 TestFilesRunsOnlyMatchedLanes fails if the loop runs every lane
       contract: each lane's header line (name + the exact command line) appears
                 in stdout at a strictly lower index than that lane's own output
                 sentinel — TestLaneHeaderPrecedesLaneOutput fails if the header
                 is printed after the run, which is the case where a transcript
                 cannot attribute a result
       contract: in a repo with zero `[[runtime.test_lane]]` entries, both `dross
                 test` and `dross test --files internal/a.go` spawn exactly one
                 command line equal byte-for-byte to runtime.test_command —
                 TestNoLanesIsByteIdentical fails if --files appends a selector
                 or trips the unmatched refusal (c-5's "c-8 never fires" half)
       contract: `dross test --files docs/x.md` with only a `go` lane returns an
                 error whose text contains `docs/x.md`, exits with the
                 unmatched-set code, and records ZERO spawns —
                 TestUnmatchedFileSetRefusesWithoutRunning fails if anything
                 spawned or if the exit code collides with the red-suite code
       contract: `dross test --files internal/a.go docs/x.md` with only a `go`
                 lane spawns the go lane once, exits 0, and prints `docs/x.md` as
                 unmatched — TestPartialMissRunsMatchedLanesAndNamesTheRest fails
                 if the miss is silent (locked unmatched_files)
       contract: with two matched lanes where the first goes red, the run still
                 spawns the second and exits 1 —
                 TestRedLaneFailsTheRunAndRunsTheRest fails if a green later lane
                 masks the earlier red (locked multi_lane)

Wave 3 (depends t-4, t-5)
  t-6  Gate each lane run on its own grant
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_consent_test.go
       covers:   c-6
       depends:  t-4, t-5
       contract: with `go` granted and `docs` ungranted, `dross test --files
                 internal/a.go docs/x.md` spawns the go lane's command exactly
                 once, spawns the docs lane's command zero times, names `docs` in
                 the refusal text, and returns a non-zero exit code —
                 TestUngrantedLaneRefusesOnlyItself fails if the granted lane is
                 blocked or the ungranted lane runs
       contract: editing the `go` lane's command after granting it produces a
                 refusal whose text distinguishes stale from never-trusted —
                 TestStaleLaneRefusalIsDistinct fails if the two collapse, which
                 is the rewritten-command attack reported as a first run
       contract: a run where every matched lane is refused spawns nothing and
                 exits non-zero — TestAllLanesRefusedIsNotGreen fails if an
                 all-refused gate reports success
       contract: a red lane alongside a refused lane exits 1, not the refusal
                 code — TestRedBeatsRefusedInExitPrecedence fails if a broken
                 suite is reported as a consent problem

  t-7  Point execute's pre-commit gate at --files
       files:    assets/prompts/execute.md
                 internal/cmd/prompt_test_lane_test.go
       covers:   c-7
       depends:  t-5
       contract: execute.md's step-1e gate contains `dross test --files` and
                 names `task.files` as the source of the argument —
                 TestExecutePromptPassesTaskFilesToTest fails if the gate reverts
                 to a bare `dross test`
       contract: execute.md documents the unmatched-set exit code alongside the
                 existing 1 / 3 / 4 meanings —
                 TestExecutePromptDocumentsUnmatchedExit fails if the new code is
                 undocumented, which is the case where an agent reads a non-zero
                 exit and commits anyway
       contract: the existing TestPromptsRunDrossTest still passes — the edited
                 gate must not reintroduce a bare `<runtime.test_command>` line

  t-8  Document lane verbs and the lane grant
       files:    README.md
                 assets/prompts/options.md
                 internal/cmd/options_docs_test.go
       covers:   c-6, c-9
       depends:  t-3, t-6
       contract: options.md mentions `dross test lane add`, `dross trust --lane`
                 and `trusted_lane_commands` — TestOptionsCoversTheConsentVerbs,
                 with those three added to its want list, fails if the settings
                 surface omits the new grant
       contract: options.md never tells the user to write the grant through the
                 generic key-writer — the same test's forbidden list, extended
                 with `local set trusted_lane_commands`, fails if it does
       contract: README documents `dross test --files` and the lane verbs —
                 TestReadmeDocumentsTestLanes fails if the flag or verb is
                 renamed without a doc update, on the TestDocsCoverAllowHosts
                 precedent
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 | t-1 |
| c-2 | t-2 (selection rule), t-5 (unmatched paths named in output) |
| c-3 | t-5 |
| c-4 | t-5 |
| c-5 | t-5 |
| c-6 | t-4 (store + grant verb), t-6 (refusal at run time), t-8 (surfaced) |
| c-7 | t-7 |
| c-8 | t-5 |
| c-9 | t-3 (verbs), t-8 (surfaced) |

9 / 9 criteria covered.

## Judgment calls

- **Base consent gate stays unconditional in `dross test`.** `requireExecConsent()`
  remains the first statement in RunE for both the bare and the `--files` form;
  the per-lane gate is added on top. Rejected: swapping the base gate for the
  lane gate when `--files` is present. `test` is a member of the closed
  `execGatedCommands` set that TestExecGatedSetIsExplicit pins to the call
  sites, and c-5 requires the lane-less `--files` path to behave exactly as
  today. Consequence to accept: a lane-declaring repo must still trust
  `runtime.test_command`.
- **A distinct exit code for "the file set matched no lane" (5), and another for
  "a lane refused" (6).** Rejected: reusing exit 1. Exit 1 already means the
  suite ran and went red; c-8's case measured nothing, and collapsing the two is
  precisely the conflation test.go's exit-code comment exists to prevent.
  Precedence when both apply: a red lane (1) wins over a refusal (6); 5 cannot
  co-occur with either, since zero matched lanes means nothing ran.
- **The matcher is a new pure package `internal/testlane` operating on
  `[][]string`, not on `project.TestLane`.** Rejected: putting the resolver in
  `internal/project` beside the schema. Index-based selection keeps t-2 genuinely
  parallel with t-1 in wave 1 and lets the whole selection algebra be table-tested
  without constructing a Project.
- **`**` is implemented by hand over segment-wise `filepath.Match`.** Rejected:
  adding `github.com/bmatcuk/doublestar`. go.mod carries five direct deps and the
  product is a single static binary; the ~40 lines this costs are exactly what
  t-2's table pins, whereas a dependency moves the contract off-repo.
- **Lane verbs nest under `dross test lane …`.** Rejected: a top-level `dross
  lane`. `Test()` already has a RunE, so `EnforceSubcommandKnown` leaves it
  alone and cobra still routes the existing positional selector; no `main.go`
  edit, and the verb reads where the user already is.
- **Consent is a comma-separated fingerprint SET (`trusted_lane_commands`),
  copied from `trusted_run_commands`.** Rejected: a `name → fingerprint` map. The
  locked `lane_consent` decision binds consent to the command line; a name-keyed
  map would let a rewritten command inherit the grant issued for the old one —
  the attack the binding exists for.
- **t-7 sits in wave 3 depending on t-5 even though its test is a pure grep.**
  Rejected: dropping it to wave 1 on the strict "needs the output" rule.
  execute.md goes live for every agent run the moment `make install` relinks it
  (r-01); a prompt instructing a flag that does not yet exist is a false-green
  generator mid-phase.
- **No `dross doctor` lane-consent section.** Rejected: adding one on the
  existing stale/granted-consent precedent. No criterion asks for it and the
  run-time refusal already names the lane; worth a follow-on, not this phase.
