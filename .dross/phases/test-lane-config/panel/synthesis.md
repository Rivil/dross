# Panel synthesis — test-lane-config

Judged cold: I authored none of the three drafts. Spot-checks against the repo
are recorded inline where they decide a disagreement.

## Scores

Scale 1–5.

| Dimension | risk | mvp | verification |
|---|---|---|---|
| **Criteria coverage** | 5 — 9/9, c-6 honestly split store (t-4) / enforcement (t-7); coverage table matches the task list | 4 — 9/9 but one-task-per-criterion is optimistic: c-5 and c-8 are asserted in the same task that introduces the dispatch, so nothing independent pins them | 5 — 9/9 with the most honest multi-task mapping (c-2 → t-2+t-5, c-6 → t-4+t-6+t-8, c-9 → t-3+t-8) |
| **Test-contract specificity** | 4 — every contract names the mutant it kills ("a resolver that stopped at the first hit fails…"); no test function names, so a contract can be satisfied by a differently-shaped test | 3 — concrete and executable, but thin: 4–5 contracts for a task covering four criteria (t-2), and no contract at all pins c-4's *ordering* (header before output) | 5 — each contract names the test function AND the failure it detects; only draft that pins header-before-output positionally ("strictly lower index than that lane's output sentinel") |
| **Granularity** | 5 — 8 tasks, one failure mode each; t-7 carrying three refusal paths is the only overload | 2 — t-1 merges schema + validator + matcher + resolver across two packages; t-2 covers c-3/c-4/c-5/c-8; t-4 then *retrofits* consent into t-2's freshly-written dispatch, which is rework disguised as a task | 4 — 8 tasks, but t-5 covers five criteria and is the fattest single task in any draft; t-3's file list omits `internal/cmd/test.go`, which it must edit to attach the subcommand (mvp caught this, verification did not) |
| **Wave correctness** | 3 — correct but over-serialized at 5 waves; t-6 and t-7 both edit `internal/cmd/test.go` in consecutive waves purely to split dispatch from refusal | 3 — 3 waves, but t-4's dependency on t-2 is a rewrite dependency, not a build-on dependency; wave 3 exists only to undo an ungated wave 2 | 4 — 3 waves with real parallelism (t-1‖t-2, then t-3‖t-4‖t-5); t-7 deliberately held to wave 3 with a stated reason (r-01: a live prompt naming a flag that does not exist yet is a false-green generator) |

**Defects found by spot-check:** none of the three names a non-existent
existing file. Every path checked exists — `internal/project/project.go`,
`internal/cmd/{validate,test,local,trust,gitignore}.go`,
`internal/cmd/{options_docs,prompt_test_command,execute_prompt,validate}_test.go`,
`cmd/dross/{main,main_test}.go`, `assets/prompts/{execute,options}.md`,
`README.md`. Every named existing test function exists
(`TestPromptsRunDrossTest`, `TestOptionsCoversTheConsentVerbs`,
`TestDocsCoverAllowHosts`, `TestExecGatedSetIsExplicit`,
`TestTomlFieldsCarryMatchingJSONTags`). The only *file* defect is an omission:
verification's t-3 does not list `internal/cmd/test.go` despite needing the
`AddCommand` line there.

**Skeleton: verification.** It wins on the two dimensions a plan is hardest to
repair after the fact — contract specificity and wave shape. Its contracts name
the test and the mutant, so a task is done when a named test is green rather
than when an author feels finished; its three-wave shape parallelises the two
independent halves (schema, matcher) and the three mid-tier tasks without
inventing a dependency. Risk is a close second and supplies most of the grafts
below; it loses on wave economy, not on thinking. mvp is the weakest skeleton
(its wave 3 is rework of its wave 2) but contributes two corrections that the
other two got wrong.

## Merged plan

Phase test-lane-config — 8 tasks across 3 waves.

### Wave 1

```
t-1  Add test_lane schema and validate rule            [verification+risk]
     files:      internal/project/project.go
                 internal/project/project_test.go
                 internal/cmd/validate.go
                 internal/cmd/validate_lane_test.go
     covers:     c-1
     depends_on: —
     desc:       Add `project.TestLane{Name string, Match []string, Command string}`
                 with matching toml+json tags, and `TestLane []TestLane` on
                 `Runtime` under `[[runtime.test_lane]]`. validate.go appends one
                 problem per lane missing name, match or command, plus a
                 duplicate-lane-name problem; a nameless lane is identified by
                 ordinal (`runtime.test_lane[0]`).
     test_contract:
       - a project.toml carrying one `[[runtime.test_lane]]` (name / match /
         command) round-trips through project.Load into a 1-element
         Runtime.TestLane — TestTestLaneDecodes fails on a zero-length slice if
         the toml tag or the field is dropped
       - a lane declaring name+match but no command makes `dross validate` exit
         non-zero with a problem line containing that lane's name —
         TestValidateNamesLaneMissingCommand fails if the per-field check is
         dropped or the message is generic
       - a lane with a name and command but an EMPTY match array is reported by
         name, not accepted as a lane that matches nothing            [graft: risk]
       - a lane with an empty `name` is reported as `runtime.test_lane[0]`, not
         as `""` — TestValidateIdentifiesNamelessLaneByIndex
       - two lanes sharing one name are reported as a duplicate; deleting this
         check silently gives one grant in t-4's name-keyed map authority over
         two commands                                                 [graft: risk]
       - a repo with zero `[[runtime.test_lane]]` entries produces zero new
         validate problems — lanes are opt-in and the validator must not invent
         work for every existing repo                                 [graft: risk]
       - the existing TestTomlFieldsCarryMatchingJSONTags walk fails if
         TestLane's fields carry a toml tag without an identical json tag
```

```
t-2  Add lane path matcher and selector                 [verification+risk]
     files:      internal/testlane/match.go
                 internal/testlane/match_test.go
     covers:     c-2
     depends_on: —
     desc:       Pure package over `[][]string` lane globs and `[]string` paths:
                 `Select(globs [][]string, paths []string) (indices []int,
                 unmatched []string)`. Hand-rolled `**` over segment-wise
                 filepath.Match; path normalization strips `./`, forces forward
                 slashes, and refuses absolute and `..`-escaping paths; a
                 trailing `/` means everything beneath.
     test_contract:
       - `internal/**/*.go` matches `internal/cmd/test.go` and
         `internal/a/b/c.go`, and does not match `main.go`, while
         `internal/*.go` matches only `internal/x.go` —
         TestDoubleStarCrossesDirectories fails if `**` collapses to one segment
       - `docs/*.md` does not match `docs/api/v1.md` —
         TestSingleStarStopsAtSeparator
       - lane 0 = ["internal/**"], lane 1 = ["**/*.go"], path
         `internal/cmd/test.go` yields indices [0 1] exactly once each in
         declaration order — TestSelectIsOrderedAndDeduped fails on a duplicate
         or on array-position precedence
       - a path matching two globs inside the SAME lane yields that lane index
         once, not twice — a duplicate would spawn one lane's command twice
                                                                      [graft: risk]
       - `./internal/cmd/test.go` and `internal/cmd/test.go` give the same
         answer, so an agent's `./`-prefixed argv is not a silent miss
                                                                      [graft: risk]
       - `docs/` matches `docs/x.md` and `docs/sub/y.md`               [graft: risk]
       - `/etc/passwd` and `../outside/x.go` match nothing rather than being
         normalized into a lane                                        [graft: risk]
       - a pattern with no metacharacter matches only that exact path  [graft: risk]
       - every path matching no lane is returned in `unmatched` —
         TestSelectReportsUnmatchedPaths fails if a miss is dropped
       - zero input paths returns zero indices and zero unmatched: the selector
         states facts, t-5 decides that a nothing-matched run is a refusal
                                                                      [graft: risk]
```

### Wave 2

```
t-3  Add dross test lane add|list|remove                [mvp+verification]
     files:      internal/cmd/test_lane.go
                 internal/cmd/test_lane_test.go
                 internal/cmd/test.go            (one AddCommand line — see D-8)
     covers:     c-9
     depends_on: t-1
     desc:       `lane` subcommand group attached in Test(). `add <name> --match
                 <glob> (repeatable) --command <line>`, `list`, `remove <name>`.
                 All three go through project.Load / (*Project).Save.
     test_contract:
       - `dross test lane add go --match 'internal/**' --command 'go test ./...'`
         followed by a fresh project.Load yields exactly one lane with those
         three values — TestLaneAddPersists fails if the Save is skipped or a
         field is dropped on the way to disk
       - adding a second lane named `go` exits non-zero naming the existing lane
         and leaves project.toml with one lane — TestLaneAddRejectsDuplicateName
       - `dross test lane remove docs` with `go` and `docs` declared leaves `go`
         intact AND leaves the [runtime] scalars (test_command, mode) intact
         across the Load→Save round trip — TestLaneRemoveKeepsOtherLanes    [graft: mvp]
       - `dross test lane remove nosuch` exits non-zero naming the unknown lane
         — TestLaneRemoveUnknownIsAnError fails if a no-op reports success
       - `dross test lane add go --match '*.go'` with no --command exits
         non-zero and leaves project.toml byte-for-byte unchanged        [graft: mvp]
       - `dross test lane list` prints each lane's name, its match globs and its
         command line — TestLaneListShowsCommand fails if the command is
         omitted, which is the value consent binds to
       - `dross test lane list` on a lane-less repo prints that none are
         configured and exits 0 — a lane-less repo is legal (c-5), not an error
                                                                       [graft: risk]
```

```
t-4  Store per-lane consent fingerprints                [risk+mvp]
     files:      internal/cmd/local.go
                 internal/cmd/trust.go
                 internal/cmd/trust_lane_test.go
     covers:     c-6
     depends_on: t-1
     desc:       Add `TrustedLaneCommands map[string]string`
                 (`trusted_lane_commands`, lane name → sha256 of that lane's
                 command) to localStore, deliberately ABSENT from localKeys.
                 `LaneConsented(root, name, line)` and `GrantLaneConsent(root,
                 name, line)`. `dross trust --lane <name>` prints the command
                 line before it writes; `--lane <name> --check` reports
                 granted / absent / stale.
     test_contract:
       - editing ONE lane's command makes LaneConsented false for that lane and
         leaves every other lane's grant true — an aggregate hash over all lanes
         fails this, which is the locked lane_consent rationale
       - granting `docs` after `go` keeps `go`'s fingerprint —
         TestTrustLaneAccumulates fails if the second grant replaces the first
       - `dross local set trusted_lane_commands <hash>` is rejected as an
         unknown key — TestLaneGrantKeyIsNotSettable fails if the key leaks into
         localKeys and the generic writer can grant consent
       - `dross trust --lane <name>` prints the exact command BEFORE the store
         is written, and leaves trusted_test_command and trusted_run_commands
         byte-unchanged
       - `dross trust --lane nosuch` exits non-zero naming the declared lanes
         rather than writing a fingerprint of the empty string
       - a git-tracked `.dross/local.toml` makes the lane grant refuse UNREAD via
         refuseTrackedLocal, exactly like every other grant in that file
                                                                       [graft: risk]
       - renaming a lane leaves its old grant orphaned and the new name
         ungranted — the lookup misses and refuses, it never inherits  [graft: risk]
       - `dross trust --lane go --check` distinguishes granted from absent from
         stale, on the existing `--check` precedent (trust.go:420)      [graft: mvp]
```

```
t-5  Dispatch --files to the resolved lanes             [verification+risk]
     files:      internal/cmd/test.go
                 internal/cmd/test_files_test.go
     covers:     c-2, c-3, c-4, c-5, c-8
     depends_on: t-1, t-2
     desc:       Add `--files` as a StringArray (repeated flag, never
                 comma-split — a comma is legal in a path). With lanes declared
                 and --files given: select, then for each matched lane in
                 declaration order print `lane <name>: <command>` and spawn that
                 lane's line VERBATIM through the existing spawnLocal seam;
                 aggregate with the worst outcome winning and at most one remote
                 sync per run. Zero matched lanes returns
                 `ExitCodeError{Code: exitNothingMeasured = 5}` naming the
                 unmatched paths and spawns nothing. No lanes declared, or no
                 --files, falls through to today's requireExecConsent +
                 testCommandLine path untouched.
     test_contract:
       - with a `go` lane and a `docs` lane declared, `dross test --files
         internal/cmd/test.go` records exactly one spawned command line through
         spawnRecorder, and the docs lane's command string appears in none of
         the recorded lines — TestFilesRunsOnlyMatchedLanes fails if the loop
         runs every lane
       - each lane's header (name + the exact command line) appears in stdout at
         a strictly lower index than that lane's own output sentinel —
         TestLaneHeaderPrecedesLaneOutput fails if the header is printed after
         the run, which is the case where a transcript cannot attribute a result
       - the line spawned for a lane is byte-identical to its configured
         command — no --files paths appended as a selector, so the fingerprint
         t-4 checks covers the line that actually ran                  [graft: risk]
       - in a repo with zero `[[runtime.test_lane]]` entries, both `dross test`
         and `dross test --files internal/a.go` spawn exactly one command line
         equal byte-for-byte to runtime.test_command —
         TestNoLanesIsByteIdentical fails if --files appends a selector or trips
         the unmatched refusal (c-5's "c-8 never fires" half)
       - `dross test --files docs/x.md` with only a `go` lane returns an error
         whose text contains `docs/x.md`, exits 5, and records ZERO spawns —
         TestUnmatchedFileSetRefusesWithoutRunning
       - exit 5 is pairwise distinct from 1 (red suite), 3 (transport) and 4
         (partial); a test asserts all four differ, so a caller cannot read
         "nothing ran" as "your code is broken"                        [graft: risk]
       - `dross test --files internal/a.go docs/x.md` with only a `go` lane
         spawns the go lane once, exits 0, and prints `docs/x.md` as unmatched —
         TestPartialMissRunsMatchedLanesAndNamesTheRest (locked unmatched_files)
       - with two matched lanes where the first goes red, the run still spawns
         the second and exits 1 — TestRedLaneFailsTheRunAndRunsTheRest fails if
         a green later lane masks the earlier red (locked multi_lane)
       - with a remote grant present and two lanes matched, the tree is synced
         ONCE, not once per lane, and a transport failure on any lane exits 3,
         never 1 — "the host was down" must never read as "your code is broken"
                                                                       [graft: risk]
```

### Wave 3

```
t-6  Gate each lane run on its own grant                [verification+mvp]
     files:      internal/cmd/test.go
                 internal/cmd/test_lane_consent_test.go
     covers:     c-6
     depends_on: t-4, t-5
     desc:       Before spawning a matched lane, check its own grant. An
                 ungranted or stale lane is skipped and named, and forces a
                 non-zero exit (`exitLaneRefused = 6`), while every granted lane
                 still runs. On the lane path this per-lane gate REPLACES the
                 global requireExecConsent check rather than stacking on it
                 (see D-3); the bare `dross test` path keeps requireExecConsent
                 untouched, which is what keeps c-5 trivially true and
                 TestGatedCommandsRefuse's `test` row green.
     test_contract:
       - with `go` granted and `docs` ungranted, `dross test --files
         internal/a.go docs/x.md` spawns the go lane's command exactly once,
         spawns the docs lane's command zero times, names `docs` in the refusal
         text, and returns a non-zero exit — TestUngrantedLaneRefusesOnlyItself
         fails if the granted lane is blocked or the ungranted lane runs
       - editing the `go` lane's command after granting it produces a refusal
         whose text distinguishes STALE from NEVER-TRUSTED —
         TestStaleLaneRefusalIsDistinct fails if the two collapse, which is the
         rewritten-command case reported as a routine first run
       - a run where every matched lane is refused spawns nothing and exits
         non-zero — TestAllLanesRefusedIsNotGreen
       - a red lane alongside a refused lane exits 1, not 6 —
         TestRedBeatsRefusedInExitPrecedence fails if a broken suite is reported
         as a consent problem
       - a lanes-only repo with an EMPTY runtime.test_command still runs its
         granted lanes: the lane path must not reach ConsentNotApplicable
         (trust.go:122), which would refuse every lane run in exactly the repo
         lanes are most wanted for                                     [graft: risk]
```

```
t-7  Point execute's pre-commit gate at --files         [risk+mvp+verification]
     files:      assets/prompts/execute.md
                 internal/cmd/prompt_test_command_test.go
     covers:     c-7
     depends_on: t-5, t-6
     desc:       Rewrite execute.md's step-1e test-gate block to run `dross test
                 --files <task.files from plan.toml>`, document the per-lane
                 trust check and the two new exit codes, and say what to do when
                 a run refuses (declare a lane, or run the full suite
                 deliberately — never commit on a refusal). Add the grep-based
                 drift guard beside the existing prompt guards.
     test_contract:
       - execute.md's step-1e gate contains `dross test --files` and names
         `task.files` as the source of the argument —
         TestExecutePromptPassesTaskFilesToTest fails if the gate reverts to a
         bare `dross test`
       - execute.md documents exit 5 (nothing measured) and 6 (lane refused)
         alongside the existing 1 / 3 / 4 meanings, all with "did not run"
         semantics — TestExecutePromptDocumentsUnmatchedExit fails if the new
         codes are undocumented, which is the case where an agent reads a
         non-zero exit and commits anyway
       - the existing TestPromptsRunDrossTest still passes — the rewrite must
         not reintroduce a bare `<runtime.test_command>` fence
       - the guard reads assets/prompts/execute.md directly, so it fails in CI
         regardless of whether `make install` re-linked the prompt locally (r-01)
                                                                       [graft: risk]
```

```
t-8  Document lane verbs and the lane grant             [verification]
     files:      README.md
                 assets/prompts/options.md
                 internal/cmd/options_docs_test.go
     covers:     c-6, c-9
     depends_on: t-3, t-6
     desc:       Add the lane surface to the two places a user discovers
                 settings: a README command-table row for the lane verbs and
                 `dross test --files`, and an options.md section naming
                 `dross trust --lane` and `trusted_lane_commands`. Extend
                 TestOptionsCoversTheConsentVerbs' want and forbidden lists.
     test_contract:
       - options.md mentions `dross test lane add`, `dross trust --lane` and
         `trusted_lane_commands` — TestOptionsCoversTheConsentVerbs, with those
         three added to its want list, fails if the settings surface omits the
         new grant
       - options.md never tells the user to write the grant through the generic
         key-writer — the same test's forbidden list, extended with
         `local set trusted_lane_commands`, fails if it does
       - README documents `dross test --files` and the lane verbs —
         TestReadmeDocumentsTestLanes fails if the flag or verb is renamed
         without a doc update, on the TestDocsCoverAllowHosts precedent
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 lane schema + validator refusal | t-1 |
| c-2 resolve lanes, name unmatched paths | t-2 (selection rule), t-5 (unmatched named in output) |
| c-3 only resolved lanes' commands run | t-5 |
| c-4 lane name + exact line printed first | t-5 |
| c-5 lane-less repo byte-identical | t-5 |
| c-6 per-lane consent | t-4 (store + grant verb), t-6 (run-time refusal), t-8 (surfaced) |
| c-7 execute gate passes task files | t-7 |
| c-8 zero-match set exits non-zero | t-5 |
| c-9 lanes managed through the CLI | t-3 (verbs), t-8 (surfaced) |

9/9 criteria covered.

## Disagreements

**D-1 — Consent storage: name→fingerprint map vs comma-separated set.**
risk and mvp both store `TrustedLaneCommands map[string]string` (lane name →
sha256 of its command). verification rejects the map and copies
`trusted_run_commands`' flat comma-separated fingerprint SET, arguing a
name-keyed map "would let a rewritten command inherit the grant issued for the
old one".
*Default taken: the map (risk+mvp).* The locked `lane_consent` decision says
"local.toml stores a per-lane command fingerprint map" in as many words, so
verification's choice is a spec violation, and its stated reason is wrong on the
mechanics — the map's VALUE is the fingerprint, so a rewritten command misses
the stored hash and goes stale exactly as intended. The set is also
self-defeating against verification's own t-6 contract: a bare set of hashes
keyed by nothing can only report "this fingerprint is absent", never
"this lane has a grant, but for a different line", so
`TestStaleLaneRefusalIsDistinct` is unsatisfiable under it.
*Why it matters:* this is the one divergence where a runner-up would have
shipped a refusal message that cannot tell a first run from a rewritten command
— the precise conflation `consentRefusal` (trust.go:321) exists to prevent.

**D-2 — Where the lane verbs live: `dross lane` vs `dross test lane`.**
risk argues a top-level group, because `dross test [selector...]` takes
arbitrary positionals and a `lane` subcommand would shadow a legitimate
`dross test lane/...` path selector. mvp and verification both nest under
`dross test`; mvp verified cobra 1.8.1 routes positionals fine when a
subcommand is attached, verification notes `Test()` already has a RunE so
`EnforceSubcommandKnown` leaves it alone.
*Default taken: `dross test lane …` (mvp+verification, 2–1).* The verb reads
where the user already is, and no `cmd/dross/main.go` edit is needed.
*Why it matters:* risk's collision is real but narrow — confirmed at
`internal/cmd/test.go:172` that `test` takes selectors, and cobra routes on
`args[0]` matching a subcommand name, so a package literally named `lane` at
the repo root becomes unreachable via `dross test lane`. Go selectors normally
carry a `./` or `/...`, so the collision is unlikely rather than impossible. If
it bites, moving the group to top-level is a rename, not a redesign.

**D-3 — Does the lane path still require the global `runtime.test_command`
grant?** risk and mvp say no: the per-lane gate replaces `requireExecConsent`
on the `--files` path. verification says yes, keeping it unconditional to
protect `TestExecGatedSetIsExplicit`, and explicitly accepts the consequence
that "a lane-declaring repo must still trust runtime.test_command".
*Default taken: replace it on the lane path only (risk+mvp).* Verified at
`internal/cmd/trust.go:122` that `CheckConsent` returns
`ConsentNotApplicable` — a hard refusal — when `runtime.test_command` is empty.
So verification's choice does not merely inconvenience a lanes-only repo, it
bricks it: every lane run refuses, in exactly the repo shape lanes exist for.
The bare `dross test` path keeps `requireExecConsent` as its first statement,
which is what `TestGatedCommandsRefuse`'s `test` row drives, so the pinned set
stays green.
*Why it matters:* it decides whether a repo can adopt lanes without also
declaring a whole-suite command, and it is the one place a runner-up's
"conservative" choice is the more dangerous one.

**D-4 — Number and naming of new exit codes.** risk adds one
(`exitNothingMeasured = 5`). verification adds two (5 for the unmatched set, 6
for a lane refusal) with a stated precedence: red (1) beats refused (6), and 5
cannot co-occur with either. mvp adds none, returning a generic non-zero
`ExitCodeError`.
*Default taken: both 5 and 6 (verification).* c-6 requires a refusal to make
the run exit non-zero *while other lanes ran*, which is a genuinely different
outcome from "nothing matched" and from "the suite is red"; 1/3/4 are already
taken (`test.go:45-52`).
*Why it matters:* mvp's generic non-zero is the weakest option — /dross-execute
reads exit status to decide whether to commit, and an unclassified non-zero is
the input to exactly the false-green this phase exists to stop.

**D-5 — Matcher home and API shape.** risk: `internal/lane`, resolving over
`[]project.TestLane` and returning a `Resolution` struct. mvp: no new package —
`internal/project/testlane.go`, because there is one consumer. verification:
`internal/testlane`, operating on `[][]string` and returning lane INDICES.
*Default taken: verification's `internal/testlane` over `[][]string`.* The
index-based API is the only one of the three that makes t-2 genuinely
independent of t-1, which is what lets both sit in wave 1; it also lets the
whole selection algebra be table-tested without constructing a Project.
*Why it matters:* mvp's placement inside `internal/project` would force t-1 and
t-2 into one task (which mvp indeed does), collapsing wave 1 and coupling the
glob semantics to the schema. All three agree on rejecting a `doublestar`
dependency; that is not a divergence.

**D-6 — Splitting dispatch from refusal.** risk splits the `--files` dispatch
(t-6) from the three refusal paths (t-7) into consecutive waves, both editing
`internal/cmd/test.go`. verification folds c-5 and c-8 into the dispatch task
and splits out only the consent gate. mvp folds everything into one task and
retrofits consent afterwards.
*Default taken: verification's fold (c-5 and c-8 inside t-5).* A dispatch that
does not yet refuse a zero-match set is a shippable false-green, so the two
should not be separate commits.
*Why it matters:* the merged t-5 is the fattest task in the plan (five
criteria). If the executor wants a smaller commit, risk's split is the
principled place to cut — carve the c-8 refusal out of t-5 into its own wave-3
task rather than carving anywhere else.

**D-7 — Whether the phase owns a docs task.** verification adds t-8
(README + options.md + extending `options_docs_test.go`). risk folds a README
row into its CLI task. mvp explicitly rejects any docs task, arguing
`cmd/dross/main_test.go`'s guard runs README→cobra only, so a new subcommand
forces no doc edit.
*Default taken: keep t-8 (verification).* mvp's fact is correct — verified at
`cmd/dross/main_test.go:35`, `TestReadmeAdvertisesOnlyRealCommands` parses
`dross <cmd>` out of the README and checks each exists in the cobra tree, not
the reverse; and `TestOptionsCoversEveryLocalKey` iterates `localKeys`, which
`trusted_lane_commands` is deliberately absent from. So nothing existing goes
red without t-8.
*Why it matters:* the guard's direction means an undocumented `dross trust
--lane` is invisible in the one prompt (`options.md`) that exists to enumerate
grants the generic key-writer cannot reach — the same discovery gap
`TestOptionsCoversTheConsentVerbs` was written for. t-8 is the cheapest task in
the plan and the easiest to drop if the phase runs long.

**D-8 — Who edits `internal/cmd/test.go` in wave 2.** Not argued by any draft,
but forced by D-2: with the verbs nested under `dross test`, t-3 must add an
`AddCommand` line in `Test()` — the same file t-5 rewrites for `--files`.
verification's t-3 file list omits it; mvp's lists it.
*Default taken: both stay in wave 2, with t-3 owning ONLY the one-line
`c.AddCommand(testLane())` in `Test()` and t-5 owning the `--files` flag
registration and the RunE body.* The regions do not overlap.
*Why it matters:* if wave 2 is executed by parallel agents in one worktree,
this is the plan's only same-file collision. Sequencing t-3 after t-5 within
the wave costs nothing and removes it; under risk's top-level `dross lane`
(D-2) the collision does not exist at all.
