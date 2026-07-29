# root-robustness — panel synthesis

Judged cold: I authored none of the three drafts. Claims below were checked against
`internal/cmd/root.go`, `pause.go`, `status.go`, `state.go`, `doctor.go`, `onboard.go`,
`rule.go`, `telemetry.go`, `ship_recover.go`, `findings_test.go`, `status_test.go` and
`assets/prompts/pause.md`.

## Scores

Scale 1–5.

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (7 tasks / 3 waves) | **4** — 5/5 covered with real redundancy, and the only draft to notice that the repair command it tells users to run (`dross onboard`) currently refuses; but misses `rule.go`'s live `ErrNoRoot` swallow, which is an actual hole in its own c-3 story | **4** — outstanding behavioural rows (byte-identical dir snapshot, zero-byte files still complete, `.dross` as a regular file, chmod 000), but no test names and its source-guard allowlist is factually wrong: it lists `telemetry.go`, which contains no `ErrNoRoot`, and omits `rule.go:224`, which does | **5** — 7 tasks, an explicit failure-mode table with exactly one owner per mode; nothing oversized | **4** — 3 waves; correctly argues the allowlist guard belongs last so it is a regression gate rather than a rewrite, but the wave-3 dependency edge is wider than the work needs |
| **mvp** (5 tasks / 2 waves) | **2** — 5/5 on paper, but c-3 rests entirely on "56 call sites propagate for free" and it explicitly rejects any cross-command check; `rule.go:224` silently converts an incomplete root into "no project rules", so `rule show` would exit 0 and falsify the claim | **3** — sound rows, fewer of them; uniquely correct in listing `ship_recover.go` as an *edit* (verified: `ship_recover.go:60` calls `FindRoot`, and recovery must survive a half-wiped `.dross/`) | **3** — coarsest; folding the parse-loudness fix into the hook-silencing task hides a locked-decision reversal inside another task's diff | **4** — clean 2 waves, correct dependency reasoning, no false edges |
| **verification** (7 tasks / 2 waves) | **5** — 5/5 plus the two code paths the others miss (`rule.go` loadMerged, telemetry `RepoHash`), and honest accounting where a criterion needs no code change (`reentry` inherits the sentinel) | **5** — named tests, per-row mutant-kill rationale, and the only draft to spot that `chdirDross` (`findings_test.go:15`) plus 4 other bare-`.dross` fixtures go red the instant the sentinel lands — a real blocker, verified | **5** — 7 well-sized tasks; the fixture migration correctly sits *inside* t-1 rather than as a task that would force a red commit | **4** — 2 waves, verified correct (every wave-2 task depends only on t-1); loses a point for putting the allowlist guard in the same wave as the changes it is meant to pin |

**Skeleton: `verification`.** It has the sharpest contracts, it is the only draft that
identified the test-fixture breakage that would otherwise stop t-1 landing green, and it
found two live swallow paths (`rule.go`, telemetry attribution) that the other two lenses
walked past. Its structure is kept; risk supplies the missing onboard task, the strongest
write-side contracts, and the wave-3 guard placement; mvp supplies `ship_recover.go`.

## Merged plan

Phase root-robustness — 9 tasks across 3 waves

### Wave 1

```
t-1  Add LocateRoot and the incomplete-root sentinel          [verification+mvp+risk]
     files:    internal/cmd/root.go, internal/cmd/root_test.go,
               internal/cmd/ship_recover.go,
               internal/cmd/findings_test.go, internal/cmd/telemetry_test.go
     covers:   c-1, c-3
     depends:  —
     desc:     Split the up-walk into LocateRoot() (root, missing []string, err) —
               locate the first `.dross/` directory and stop there, reporting which of
               project.toml / state.json are absent without erroring — and FindRoot(),
               which turns a non-empty missing list into an IncompleteRootError that
               wraps ErrNoRoot. Add the shared RepairHint constant. `ship recover`
               switches to LocateRoot so the escape hatch still works on a half-wiped
               `.dross/` [mvp]. Migrate the bare-`.dross` test fixtures that now resolve
               to not-a-repo [verification].
     contract: - TestFindRootIncompleteIsNotARepo: `.dross/` with project.toml but no
                 state.json -> errors.Is(err, ErrNoRoot) == true. Unwrapping the
                 sentinel flips this false and every hook target goes loud. [verification]
               - TestFindRootIncompleteNamesMissingFileAndRepair: table over
                 {state.json absent, project.toml absent, both absent}; the message names
                 each absent path and contains RepairHint. The single-miss row must NOT
                 name the file that is present. [verification+risk]
               - TestFindRootStopsAtFirstDross (walk_stop): parent has a COMPLETE
                 `.dross/`, child an INCOMPLETE one, cwd = child/deep -> FindRoot errors
                 and never returns the parent path. A keep-climbing mutant fails on the
                 returned path, not on the error. [verification+mvp]
               - TestFindRootIgnoresCorruptContent (completeness_check): state.json =
                 `{{{` -> FindRoot SUCCEEDS. A mutant that parses to decide completeness
                 fails here. [verification]
               - zero-byte project.toml + state.json still resolve successfully —
                 completeness is existence, never parse. [risk]
               - `.dross` present as a regular file does not stop the walk and does not
                 panic; `.dross` present but chmod 000 returns a non-ErrNoRoot error
                 rather than being misread as not-a-dross-repo. [risk]
               - TestLocateRootReportsMissingWithoutError: same fixture -> LocateRoot
                 returns the `.dross` path, missing == [".dross/state.json"], err == nil.
                 This is the surface doctor needs. [verification]
               - `dross ship recover` still resolves the root when only
                 `.dross/project.toml` survives. [mvp]
               - Fixture migration proven by the package compiling green: chdirDross
                 (findings_test.go:15, 4 call sites) and the bare-`.dross` fixture at
                 telemetry_test.go:108 must scaffold both files. [verification]

t-2  Refuse /dross-pause without an initialised root          [verification+mvp+risk]
     files:    assets/prompts/pause.md, internal/cmd/pause_prompt_test.go
     covers:   c-4
     depends:  —
     desc:     Add a §0 pre-flight gate: probe with `dross state show`; on a non-zero
               exit print one refusal line naming why and pointing at `dross onboard`,
               then stop — no `.dross/`, no handoff.md, no gitignore edit, no
               `dross state touch`, and never run onboard itself. Add a matching hard
               rule. The §4 wrap-up block must not print in the refusal case — it would
               claim a save that never happened [risk]. (r-01: prompt edits need
               `make install` to go live.)
     contract: - TestPausePromptRefusesWithoutRoot, one failing subtest per needle over
                 the normalised prompt (same helper shape as resume_prompt_test.go's
                 resumePromptContent): probe names `dross state show`; refusal contains
                 "not a dross repo" and a stop/write-nothing phrase; repair pointer
                 contains "dross onboard". [verification]
               - the refusal must cover BOTH the absent and the incomplete case as
                 separate needles (locked pause_refusal: one concept, not two). [risk+mvp]
               - TestPausePromptForbidsCreatingRoot: the Hard rules section contains
                 "never create .dross" AND "handoff.md" — removing only the hard rule and
                 leaving the pre-flight still fails, so the two halves of c-4 are pinned
                 independently. [verification]
               - TestPausePromptGateIsBeforeTheWrite: byte offset of the gate is lower
                 than that of "Write the confirmed content to `.dross/handoff.md`".
                 [verification+mvp+risk]
               - the prompt never instructs running `dross init` or `dross onboard`
                 itself. [risk]
```

### Wave 2 — all depend on t-1

```
t-3  Silence status and state touch on a non-root             [verification+mvp+risk]
     files:    internal/cmd/status.go, internal/cmd/state.go,
               internal/cmd/status_test.go, internal/cmd/state_test.go
     covers:   c-2
     depends:  t-1
     desc:     Status and stateTouch treat errors.Is(err, ErrNoRoot) as exit 0 with no
               output. The branch goes in the two handlers, never in loadState() —
               `state show` / `set` / `bump` stay loud.
     contract: - TestStatusSilentOnIncompleteRoot: `.dross/` with project.toml only ->
                 runCmd(Status()) returns nil AND captureStdout returns "". A partial fix
                 that prints a header before bailing fails on the non-empty stdout.
                 [verification]
               - TestStatusSilentOutsideDrossRepo, replacing the existing
                 TestStatusFreshDirSuggestsInit (status_test.go:18): bare temp dir -> nil
                 error, empty stdout. Pins the deliberate change so absent and incomplete
                 stay one signal. [verification+mvp]
               - TestStateTouchSilentOnIncompleteRoot: returns nil, prints nothing, and
                 `.dross/state.json` still does not exist afterwards. Kills a
                 "silently create the file" fix. [verification+risk]
               - TestStateShowFailsOnIncompleteRoot: same fixture, non-nil error naming
                 ".dross/state.json". This is the scoping test — moving the swallow into
                 loadState() makes show silent and fails here. [verification+mvp+risk]
               - TestStatusFailsOnCorruptState: state.json = `{{{` -> non-nil error
                 mentioning state.json. A swallow-everything fix passes the other rows
                 and dies here. [verification+mvp]

t-4  Silence pause --auto, keep corrupt files loud            [verification+risk]
     files:    internal/cmd/pause.go, internal/cmd/pause_test.go,
               internal/cmd/reentry_test.go
     covers:   c-2
     depends:  t-1
     desc:     pauseAuto already returns nil on ErrNoRoot (pause.go:47), which now covers
               the incomplete case. Change autoSnapshot to surface project.toml /
               state.json decode failures instead of degrading past them (today's
               `if stErr == nil` skip), so a corrupt file is loud in the PreCompact hook
               too. Git failures keep degrading — an environment fact, not broken state.
     contract: - TestPauseAutoWritesNothingOnIncompleteRoot: `.dross/` with project.toml
                 only -> returns nil AND `.dross/handoff.md` does not exist. The
                 file-existence half is the real assertion. [verification+mvp]
               - TestPauseAutoFailsOnCorruptState: state.json = `{"version":` truncated,
                 project.toml valid -> non-nil error naming state.json AND handoff.md not
                 written; pre-existing handoff.md bytes are byte-identical after the run.
                 This is the row that fails on current code. [verification+risk]
               - TestPauseAutoFailsOnCorruptProject: same shape for a project.toml that
                 fails TOML decode — a separate row so fixing only the state path leaves
                 a named failure. [verification]
               - TestPauseAutoStillDegradesOnMissingGit: existing no-git fixture still
                 renders "- branch: (no git)" and exits 0. Pins loud-vs-soft. [all three]
               - `dross reentry` against a project.toml containing bad TOML exits
                 non-zero naming project.toml, rather than the ErrNoRoot silence. [risk]

t-5  Diagnose an incomplete root in doctor                    [verification+mvp+risk]
     files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
     covers:   c-5
     depends:  t-1
     desc:     Doctor resolves its root via LocateRoot (doctor.go:31 currently uses
               FindRoot, which after t-1 would die before reaching the diagnosis) so an
               incomplete `.dross/` reaches the existing foundational-files block; the
               missing-file lines reuse the shared RepairHint.
     contract: - TestDoctorDiagnosesIncompleteRoot: `.dross/` with project.toml only ->
                 stdout contains "✗ .dross/state.json — missing" AND the returned error
                 names a project-level issue. Leaving doctor on FindRoot returns the
                 not-a-dross-repo error and prints nothing, failing both halves — this is
                 c-5's "rather than failing to load". [verification]
               - TestDoctorNamesEveryMissingFile: `.dross/` holding only rules.toml ->
                 output names BOTH ".dross/project.toml" and ".dross/state.json". A
                 short-circuit after the first miss fails the second needle. [verification]
               - TestDoctorRepairHintIsShared: doctor's missing-file output contains
                 RepairHint verbatim, so doctor and root.go cannot drift to two repair
                 strings. [verification+risk]
               - the missing-file list doctor prints for an incomplete root equals the
                 Missing slice LocateRoot returns for the same directory — single source
                 of truth; fails if doctor keeps a hardcoded trio. [risk]
               - existing TestDoctorFlagsMissingFoundationalFile stays green — the
                 regression pin on rules.toml, which is doctor's trio and deliberately NOT
                 part of root completeness. [verification+mvp]

t-6  Let onboard adopt an incomplete .dross                   [risk]
     files:    internal/cmd/onboard.go, internal/cmd/onboard_test.go
     covers:   c-3
     depends:  t-1
     desc:     onboard.go:37 refuses whenever `.dross/` exists at all, and the `--force`
               escape does os.RemoveAll on it — so the repair command every new error
               message names is a dead end on exactly the roots it is meant to repair,
               and the only way through destroys the surviving project.toml. Narrow the
               refusal to a COMPLETE root; an incomplete one is adopted in place,
               preserving files already present.
     contract: - `dross onboard` in a dir whose `.dross/` holds only project.toml exits 0,
                 creates state.json, and leaves the existing project.toml byte-identical
                 — today this run fails with ".dross already exists". [risk]
               - `dross onboard` against a complete root still refuses with the existing
                 message unless --force. [risk]
               - after the adopting run, `dross status` in the same dir produces output —
                 the round trip the error message promises actually terminates. [risk]

t-7  Narrow the rule.go degradation to an absent root         [verification]
     files:    internal/cmd/rule.go, internal/cmd/rule_test.go
     covers:   c-3
     depends:  t-1
     desc:     loadMerged (rule.go:224) swallows ErrNoRoot and treats project rules as
               empty. After t-1 that swallow silently absorbs the incomplete case too, so
               `rule show` would exit 0 on a broken root — a live c-3 hole. Degrade only
               for a genuinely absent `.dross/`.
     contract: - `rule show` against a `.dross/` missing state.json returns a non-nil
                 error naming ".dross/state.json" and RepairHint. [verification]
               - TestRuleShowStillWorksOutsideAnyDrossRepo: bare temp dir -> exits 0
                 listing global rules only. Pins that the change narrows the degradation
                 rather than deleting it. [verification]

t-8  Bucket the incomplete-root error in telemetry            [verification]
     files:    internal/cmd/telemetry.go, internal/telemetry/telemetry.go,
               internal/telemetry/telemetry_test.go
     covers:   c-3
     depends:  t-1
     desc:     Add an `incomplete_root` class rule above `no_root`, and switch the
               RepoHash lookups (telemetry.go:32, :91) to LocateRoot so an incomplete
               repo still attributes its failures.
     contract: - TestClassifyIncompleteRoot: ClassifyError on the real
                 IncompleteRootError message returns "incomplete_root", not "other" and
                 not "no_root". [verification]
               - existing TestNoTokenShadowing stays green with the new rule inserted —
                 the guard that the new tier does not make a later rule unreachable.
                 [verification]
               - TestTelemetryRepoHashOnIncompleteRoot: RecordCLIEvent inside a `.dross/`
                 missing state.json still writes a non-empty RepoHash. [verification]
```

### Wave 3 — depends on t-3, t-4, t-6, t-7

```
t-9  Fail loudly everywhere else, close the allowlist         [risk+verification]
     files:    internal/cmd/incompleteroot_test.go
     covers:   c-1, c-2, c-3, c-4
     depends:  t-3, t-4, t-6, t-7
     desc:     One new test file holding the cross-cutting guarantees: the cross-command
               loud-failure table, the no-write assertion for the silent set, and the
               source-level guard pinning which files may swallow ErrNoRoot or call
               LocateRoot. Placed last so its allowlist is a genuine regression gate for
               command 57 rather than something rewritten mid-phase [risk].
     contract: - TestIncompleteRootFailsNonHookCommands: table over `state show`,
                 `project show`, `validate`, `task list`, `verify`, `rule show`,
                 `phase list`, `milestone show` against a `.dross/` missing state.json;
                 every row returns a non-nil error whose message contains
                 ".dross/state.json" AND RepairHint. Each command is its own subtest so a
                 regression names the command. [verification+risk]
               - no-write snapshot: after running `reentry`, `status`, `state touch x`
                 and `pause --auto` in a dir with an incomplete `.dross/`, a recursive dir
                 snapshot taken before and after is byte-identical — no handoff.md, no
                 state.json, no new `.dross/`. [risk]
               - the same four commands in a dir with NO `.dross/` at all also create
                 nothing and print nothing — the c-1 indistinguishability assertion. [risk]
               - TestErrNoRootSwallowSitesAreAllowlisted: source scan over non-test
                 internal/cmd/*.go for ErrNoRoot; the file set must be a subset of
                 {root.go, reentry.go, status.go, state.go, pause.go, rule.go}, and the
                 set calling LocateRoot exactly {root.go, doctor.go, onboard.go,
                 ship_recover.go, telemetry.go}. A new command copying the silent pattern
                 fails by filename. [verification+risk, allowlists corrected — see D9]
```

Coverage: c-1 → t-1, t-9 · c-2 → t-3, t-4, t-9 · c-3 → t-1, t-6, t-7, t-8, t-9 ·
c-4 → t-2, t-9 · c-5 → t-5. All 5 criteria covered; no criterion rests on a single
task's internal reasoning alone.

## Disagreements

**D1 — Does onboard need fixing so the repair advice works?**
*risk* adds t-6 on the grounds that every new error message names `dross onboard` and
onboard currently refuses on any existing `.dross/`, making the advice a dead end.
*mvp* and *verification* have no onboard task at all and neither mentions the problem.
**Provisional default: include t-6 (risk).** Verified in source — `onboard.go:37` returns
".dross already exists" whenever the directory is present, and the `--force` path at
`onboard.go:40` does `os.RemoveAll(root)`, which would delete the surviving project.toml
the user is trying to repair around. **Why it matters:** without this, c-3 is satisfied
literally (the message names a repair command) while being false in practice, and the
only path the user can find destroys their data. Two lenses missing it is not agreement —
neither considered it.

**D2 — Is telemetry in scope?**
*verification* adds t-8 (an `incomplete_root` bucket plus RepoHash via LocateRoot).
*risk* explicitly rejects it: "Left telemetry alone… an incomplete root costs only a
missing repo_hash." *mvp* is silent.
**Provisional default: include t-8 (verification).** risk's stated reason is correct on
the write-side question it asks (telemetry writes to `~/.claude/dross/`, so it cannot
violate c-4), but it is answering a different question. Verified: `telemetry.go:32` and
`:91` both use `FindRoot`, so after t-1 every failure in exactly the repos this phase
exists to fix loses its attribution, and the new error message classifies into the opaque
"other" bucket. **Why it matters:** this is the phase most likely to need telemetry to
tell whether it worked, and blinding it is the cheapest thing here to get wrong.

**D3 — Does `rule.go`'s ErrNoRoot swallow need narrowing?**
*verification* changes loadMerged so it degrades only for a genuinely absent root.
*mvp* rejects any call-site work as "structure without a criterion behind it".
*risk* omits rule.go entirely — its own allowlist does not even list it.
**Provisional default: include (verification), split out as t-7.**
Verified: `rule.go:224` is a live `errors.Is(err, ErrNoRoot)` swallow, one of only three
in non-test code. **Why it matters:** it is a direct counterexample to mvp's central
claim that c-3 comes free from error propagation — `rule show` would exit 0 against a
broken root. Splitting it out of verification's combined task lets the allowlist guard
move to wave 3 without dragging a production edit with it.

**D4 — Two waves or three?**
*verification* and *mvp* use 2 waves, with the allowlist guard alongside the changes it
pins. *risk* uses 3, arguing the guard's allowlist encodes the phase's end state, so
authoring it early guarantees one rewrite.
**Provisional default: 3 waves (risk).** **Why it matters:** low stakes for correctness —
the dependency graph is identical either way — but it decides whether the guard is written
once as a gate or twice as a moving target. If wave count is being minimised for
throughput, collapsing t-9 into wave 2 is safe.

**D5 — What shape is doctor's verdict?**
*risk* wants a new verdict distinguishing "not a dross repo (incomplete `.dross/`)" from
the ordinary repairable rules.toml issue, with a test asserting the two are never
collapsed. *mvp* and *verification* keep doctor's existing foundational-files block
(`doctor.go:51`, trio project.toml + rules.toml + state.json) and only add the repair hint.
**Provisional default: mvp/verification's minimal version.**
Verified: doctor already prints per-file misses today, so c-5 is mostly about not
*regressing* once FindRoot tightens. **Why it matters:** risk's split is the better UX and
its "doctor's list equals LocateRoot's Missing slice" contract is worth keeping (grafted
into t-5) — but a new verdict class is scope c-5 does not ask for, and risk's own note
concedes rules.toml is required by loadProject regardless.

**D6 — How does the pause prompt detect the root?**
*verification* probes with `dross state show` so the prompt and the CLI cannot drift.
*mvp* has the prompt stat project.toml and state.json itself, rejecting a CLI dependency.
*risk* says only "resolve the root first" and does not specify.
**Provisional default: the `dross state show` probe (verification).** **Why it matters:**
mvp's version keeps the prompt honest if the binary is stale (rule r-01 makes staleness a
recurring hazard here), but it duplicates the completeness rule in markdown, where nothing
enforces it. The locked pause_refusal decision says the prompt and `pause --auto` must
agree on what counts as a repo — one probe is the only way that is structurally true.
Note that this makes t-2's runtime behaviour depend on t-1 even though its *test* does not,
so t-2 stays in wave 1 only because its contract is a content gate.

**D7 — Is a cross-command loud-failure table worth a task?**
*risk* (t-7) and *verification* (t-6) both build one. *mvp* explicitly rejects it: the call
sites already propagate to a non-zero exit, so a sweep is structure without a criterion.
**Provisional default: include (t-9).** **Why it matters:** c-3 is a statement about every
non-hook command, and D3 already produced one command (`rule show`) where propagation does
not happen. mvp's reasoning is sound for the sites that do `return err` and wrong for the
sites that swallow — and nothing distinguishes the two without the table.

**D8 — What is the bare-walk resolver called, and what does it return?**
*verification*: exported `LocateRoot() (root, missing []string, err)`.
*mvp*: exported `FindRootDir()` returning just the path.
*risk*: unexported `findRootDir()` returning just the path.
**Provisional default: `LocateRoot` with the missing slice (verification).**
**Why it matters:** the missing-slice return is what makes t-5's "doctor's list equals the
resolver's list" contract writable at all; with a path-only resolver, doctor has to
re-derive the trio and the single-source-of-truth guarantee evaporates. risk's unexported
form works (all callers are in package `cmd`) but blocks the telemetry use in t-8.

**D9 — Which files may swallow ErrNoRoot?**
*risk*'s guard asserts exactly {reentry.go, pause.go, status.go, state.go, telemetry.go}.
*verification*'s asserts a subset of {root.go, reentry.go, status.go, state.go, pause.go}.
**Provisional default: subset of {root.go, reentry.go, status.go, state.go, pause.go,
rule.go}, and LocateRoot callers exactly {root.go, doctor.go, onboard.go, ship_recover.go,
telemetry.go}.** Both draft allowlists are factually wrong and were corrected against
source, not arbitrated: `grep -rn ErrNoRoot` over non-test `internal/cmd/*.go` returns
exactly `root.go:16,35`, `reentry.go:39`, `pause.go:47`, `rule.go:224` — `telemetry.go`
contains no reference to it, and `rule.go` is absent from both lists. The LocateRoot set
follows from t-1/t-5/t-6/t-8 plus mvp's `ship_recover.go` catch. **Why it matters:** an
allowlist guard that ships with a wrong list fails on day one and gets "fixed" by widening
it, which is exactly how the F8-style drift it exists to prevent starts. Whoever executes
t-9 must re-derive both sets from the tree as it stands after wave 2, not copy either draft.
