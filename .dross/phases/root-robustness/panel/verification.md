# root-robustness — verification lens

Designed backward from test contracts. Each task exists because a specific
assertion needs to become writable; nothing is here that no test can pin.

```
Phase root-robustness — 7 tasks across 2 waves

Wave 1
  t-1  Add LocateRoot and incomplete-root sentinel
       files:    internal/cmd/root.go, internal/cmd/root_test.go,
                 internal/cmd/findings_test.go, internal/cmd/telemetry_test.go
       covers:   c-1
       desc:     Split the up-walk into LocateRoot() (root, missing []string, err)
                 and FindRoot(), which turns a missing project.toml/state.json into
                 an IncompleteRootError wrapping ErrNoRoot. Add the shared RepairHint
                 constant. Migrate the two bare-`.dross` test fixtures that now
                 resolve to not-a-repo.
       contract: - TestFindRootIncompleteIsNotARepo: `.dross/` holding project.toml
                   but no state.json -> FindRoot returns err with
                   errors.Is(err, ErrNoRoot) == true. Unwrapping the sentinel flips
                   this to false and every hook target goes loud.
                 - TestFindRootIncompleteNamesMissingFileAndRepair: table over
                   {state.json absent, project.toml absent, both absent}; the error
                   string names each absent path (".dross/state.json") and contains
                   RepairHint. Dropping a name from the message fails exactly that row.
                 - TestFindRootStopsAtFirstDross (walk_stop): parent has a COMPLETE
                   `.dross/`, child has an INCOMPLETE one, cwd is child/deep ->
                   FindRoot errors and never returns the parent path. A "keep
                   climbing when incomplete" mutant returns the ancestor root and
                   fails this test on the returned path, not on the error.
                 - TestFindRootIgnoresCorruptContent (completeness_check): state.json
                   containing `{{{` -> FindRoot SUCCEEDS. A mutant that parses to
                   decide completeness turns the corrupt file into "uninitialised"
                   and fails here.
                 - TestLocateRootReportsMissingWithoutError: same incomplete fixture
                   -> LocateRoot returns the `.dross` path, missing == [".dross/state.json"],
                   err == nil. This is the surface doctor needs; if it starts
                   returning an error, t-5's diagnosis regresses to "failed to load".
                 - Fixture migration is proven by the package compiling green: the
                   4 chdirDross() call sites in findings_test.go and the bare-`.dross`
                   fixture at telemetry_test.go:108 fail with the new sentinel unless
                   they scaffold project.toml + state.json.
       depends:  -
       status:   pending

  t-2  Refuse /dross-pause without an initialised root
       files:    assets/prompts/pause.md, internal/cmd/pause_prompt_test.go
       covers:   c-4
       desc:     Add a §0 pre-flight gate: run `dross state show`; on a non-zero exit
                 print one refusal line naming why and pointing at `dross onboard`,
                 then stop. Add a hard rule forbidding creation of `.dross/`.
       contract: - TestPausePromptRefusesWithoutRoot, one failing subtest per needle
                   over the normalised prompt (same helper shape as
                   resume_prompt_test.go's resumePromptContent): "probe" -> the
                   pre-flight names `dross state show`; "refusal" -> the words
                   "not a dross repo" and "stop"/"write nothing"; "repair pointer"
                   -> "dross onboard". Deleting the gate paragraph fails all three
                   with the missing phrase named.
                 - TestPausePromptForbidsCreatingRoot: the Hard rules section
                   contains "never create .dross" AND "handoff.md". Removing just
                   the hard rule (leaving the pre-flight) still fails this one, so
                   the two halves of c-4 are pinned independently.
                 - TestPausePromptGateIsBeforeTheWrite: the byte offset of the
                   refusal gate is lower than the offset of "Write the confirmed
                   content to `.dross/handoff.md`". A gate documented after the
                   write step cannot gate anything and fails on ordering alone.
       depends:  -
       status:   pending

Wave 2 (all depend on t-1)
  t-3  Silence status and state touch on a non-root
       files:    internal/cmd/status.go, internal/cmd/state.go,
                 internal/cmd/status_test.go, internal/cmd/state_test.go
       covers:   c-2
       desc:     Status and stateTouch treat errors.Is(err, ErrNoRoot) as exit 0 with
                 no output. The branch goes in the two handlers, never in loadState —
                 `state show` / `set` / `bump` stay loud.
       contract: - TestStatusSilentOnIncompleteRoot: `.dross/` with project.toml only
                   -> runCmd(Status()) returns nil AND captureStdout returns "".
                   The current `return err` fails on the non-nil error; a partial fix
                   that prints a header before bailing fails on the non-empty stdout.
                 - TestStatusSilentOutsideDrossRepo (replaces
                   TestStatusFreshDirSuggestsInit): bare temp dir -> nil error, empty
                   stdout. Pins the deliberate behaviour change so absent and
                   incomplete stay one signal (c-1).
                 - TestStateTouchSilentOnIncompleteRoot: `dross state touch "x"`
                   returns nil, prints nothing, and `.dross/state.json` still does
                   not exist afterwards. Kills a "silently create the file" fix.
                 - TestStateShowFailsOnIncompleteRoot: same fixture, `state show`
                   returns a non-nil error naming ".dross/state.json". This is the
                   scoping test — moving the swallow into loadState() makes show
                   silent and fails here (c-3 boundary).
                 - TestStatusFailsOnCorruptState (completeness_check): state.json
                   present but `{{{` -> Status returns a non-nil error mentioning
                   state.json. A swallow-everything fix passes t-3's other rows and
                   dies here.
       depends:  t-1
       status:   pending

  t-4  Silence pause --auto, keep corrupt files loud
       files:    internal/cmd/pause.go, internal/cmd/pause_test.go
       covers:   c-2
       desc:     pauseAuto already returns nil on ErrNoRoot, which now covers the
                 incomplete case. Change autoSnapshot to surface project.toml /
                 state.json decode failures instead of degrading past them, so a
                 corrupt file is loud in the PreCompact hook too.
       contract: - TestPauseAutoWritesNothingOnIncompleteRoot: `.dross/` with
                   project.toml only -> pause --auto returns nil AND
                   `.dross/handoff.md` does not exist. The file-existence half is the
                   real assertion: a fix that resolves the root anyway would return
                   nil and still write a snapshot.
                 - TestPauseAutoFailsOnCorruptState: `.dross/state.json` = `{{{`,
                   project.toml valid -> pause --auto returns a non-nil error naming
                   state.json AND handoff.md is NOT written. Today's `if stErr == nil`
                   swallow returns nil and writes a snapshot with the phase line
                   missing — this test is the one that fails on current code.
                 - TestPauseAutoFailsOnCorruptProject: same shape for a project.toml
                   that fails TOML decode; separate row so fixing only the state path
                   leaves a named failure.
                 - TestPauseAutoStillDegradesOnMissingGit: existing no-git fixture
                   still renders "- branch: (no git)" and exits 0. Pins the line
                   between "corrupt dross file" (loud) and "no git" (soft) so the
                   loudness fix doesn't over-reach.
       depends:  t-1
       status:   pending

  t-5  Diagnose an incomplete root in doctor
       files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
       covers:   c-5
       desc:     Doctor resolves its root via LocateRoot so an incomplete `.dross/`
                 reaches the foundational-files block instead of erroring at
                 FindRoot; the missing-file lines reuse the shared RepairHint.
       contract: - TestDoctorDiagnosesIncompleteRoot: `.dross/` with project.toml
                   only -> stdout contains "✗ .dross/state.json — missing" AND the
                   returned error message contains "project-level issue". The second
                   half is c-5's "rather than failing to load": leaving doctor on
                   FindRoot returns the not-a-dross-repo error instead and fails on
                   that assertion while printing nothing.
                 - TestDoctorNamesEveryMissingFile: `.dross/` holding only rules.toml
                   -> output names BOTH ".dross/project.toml" and ".dross/state.json".
                   A short-circuit after the first miss fails the second needle.
                 - TestDoctorRepairHintIsShared: doctor's missing-file output contains
                   cmd.RepairHint verbatim. If doctor and root.go drift to two
                   different repair strings, this fails without anyone re-reading
                   both files.
                 - Existing TestDoctorFlagsMissingFoundationalFile must stay green —
                   it is the regression pin on the rules.toml third file, which is
                   doctor's trio and deliberately NOT part of root completeness.
       depends:  t-1
       status:   pending

  t-6  Fail loudly everywhere else, close the allowlist
       files:    internal/cmd/rule.go, internal/cmd/rule_test.go,
                 internal/cmd/incompleteroot_test.go
       covers:   c-3
       desc:     loadMerged stops treating an incomplete root as "no project rules";
                 it degrades only for a genuinely absent `.dross/`. Add the
                 cross-command loud-failure table and the swallow-site allowlist guard.
       contract: - TestIncompleteRootFailsNonHookCommands: table running
                   `state show`, `project show`, `validate`, `task list`, `verify`,
                   `rule show`, `phase list` against a `.dross/` missing state.json;
                   every row must return a non-nil error whose message contains
                   ".dross/state.json" and RepairHint. Each command is its own
                   subtest, so a single command that regresses to silent exit 0 is
                   named in the failure.
                 - TestErrNoRootSwallowSitesAreAllowlisted: source scan (same shape as
                   interaction_coverage_test.go) over non-test internal/cmd/*.go for
                   `ErrNoRoot`; the file set must be a subset of {root.go, reentry.go,
                   status.go, state.go, pause.go}. Adding a silent-exit-0 branch to
                   any other command fails with that filename, which is exactly c-3's
                   "scoped to the hook targets only".
                 - TestRuleShowStillWorksOutsideAnyDrossRepo: bare temp dir ->
                   `rule show` exits 0 listing global rules only. Pins that the
                   rule.go change narrows the degradation to absent-root and does not
                   delete it.
       depends:  t-1
       status:   pending

  t-7  Bucket the incomplete-root error in telemetry
       files:    internal/cmd/telemetry.go, internal/telemetry/telemetry.go,
                 internal/telemetry/telemetry_test.go
       covers:   c-3
       desc:     Add an `incomplete_root` class rule above `no_root`, and switch the
                 RepoHash lookups in cmd/telemetry.go to LocateRoot so an incomplete
                 repo still attributes its failures.
       contract: - TestClassifyIncompleteRoot: ClassifyError on the real
                   IncompleteRootError message returns "incomplete_root", not "other"
                   and not "no_root". Without the rule the message matches nothing
                   and lands in the opaque bucket.
                 - TestNoTokenShadowing (existing) must stay green with the new rule
                   inserted — it is the guard that "exists but is incomplete" does not
                   make a later rule unreachable.
                 - TestTelemetryRepoHashOnIncompleteRoot: RecordCLIEvent inside a
                   `.dross/` missing state.json still writes a non-empty RepoHash.
                   Leaving telemetry.go on FindRoot silently drops attribution for
                   exactly the repos this phase is about.
       depends:  t-1
       status:   pending
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 — incomplete `.dross/` reads as not-a-dross-repo to callers | t-1 |
| c-2 — reentry / status / state touch / pause --auto exit 0 silently | t-3, t-4 (reentry needs no code change; its `errors.Is(err, ErrNoRoot)` branch inherits the new sentinel from t-1, and t-1's `TestFindRootIncompleteIsNotARepo` is what proves it) |
| c-3 — every other command fails non-zero naming file + repair | t-6, t-7 |
| c-4 — `/dross-pause` refuses and writes nothing | t-2 |
| c-5 — `dross doctor` names the incomplete diagnosis | t-5 |

5/5 criteria covered.

## Judgment calls

- **IncompleteRootError wraps ErrNoRoot** rather than being a sibling sentinel. Chose the wrap so all ~52 existing `FindRoot()` call sites and the 3 existing `errors.Is(err, ErrNoRoot)` branches get c-1 for free; rejected a parallel sentinel, which would have needed 52 edits and made c-1 a per-site promise instead of a type guarantee.
- **Split LocateRoot out of FindRoot** instead of giving doctor a `--allow-incomplete` flag or letting it stat files itself. Doctor is the one caller that must see past the classification; a second, honest resolver is cheaper to test (`missing []string`, nil error) than a boolean parameter, and it is also what telemetry needs in t-7.
- **`dross status` goes silent for an absent root too**, not only an incomplete one. c-2 only names the incomplete case, but keeping the absent case loud would let a caller distinguish the two, which is precisely what c-1 and the pause_refusal decision forbid. Cost: `TestStatusFreshDirSuggestsInit` is replaced, and a human who mistypes a directory gets nothing. Accepted — discoverability moves to `dross doctor` (t-5).
- **The silence branch lives in the `state touch` handler, not in `loadState()`**. Rejected the loadState placement even though it is one line shorter: it would silence `state show`, `set` and `bump` as well, violating c-3. `TestStateShowFailsOnIncompleteRoot` exists specifically to fail if someone later takes the shorter route.
- **`/dross-pause` probes with `dross state show`** rather than a new `dross root` command. Rejected the new verb: it would drag in README parity (`commands_parity_test.go`) and a fourth surface for one concept. `state show` is already a non-hook command that fails loudly with the exact message c-3 mandates, so the prompt gate and the CLI contract cannot drift.
- **pause --auto stops swallowing decode errors.** This directly contradicts the existing comment in `autoSnapshot` ("unparseable state must never make the PreCompact hook fail a compaction"). The completeness_check decision is locked and says the opposite, so the comment loses; t-4 keeps a separate test row pinning that missing-git still degrades softly, so only dross-owned files became loud.
- **Test-fixture migration is inside t-1, not its own task.** It is mechanical and under ten minutes, but `findings_test.go`'s `chdirDross` and the bare-`.dross` fixture in `telemetry_test.go` go red the instant the sentinel lands — splitting them out would mean committing t-1 with a red package, which the repo's commit-gating rule forbids.
- **Root completeness is project.toml + state.json; doctor's trio keeps rules.toml.** Locked decision fixes the pair, and a missing rules.toml genuinely does not stop the CLI loading. Rejected unifying the two lists; t-5's contract pins that doctor still flags rules.toml as its own issue class.
- **Only two waves.** t-2 (prompt) has no code dependency — its content test passes standalone — so it stays in wave 1 rather than being parked behind t-1 for narrative tidiness. Everything else genuinely needs `LocateRoot`/the sentinel to exist, and nothing in wave 2 needs anything else in wave 2.
