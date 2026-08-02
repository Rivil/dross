# Panel synthesis — state-json-branch-safety

Judge notes: every path named by the three drafts was checked against the tree.
Findings that moved the scoring are inline below.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (10 tasks / 3 waves) | 7/7 mapped, but c-3 is under-covered in fact: misses `internal/cmd/ship.go:232` (`git add .dross/state.json`, which hard-errors once the path is ignored) and `internal/cmd/status.go:487` (`git show origin/<main>:.dross/state.json`, permanently dead after untracking). | Strongest on negative controls — `TestCheckoutRefusalPassesThroughOtherErrors`, `TestRecoveryStillRestoresPhaseArtifacts`, `TestDoctorCleanRepoStaysGreen` each pin the over-fire side, not just the fire side. Named Go test IDs throughout. | Best-calibrated: 10 tasks, one failure mode each, detection (t-7) deliberately split from destructive deletion (t-9). t-4 (checkout-refusal wrapper) is the only task no criterion demands. | Sound. 7 independent wave-1 tasks is real parallelism, not wishful — all seven touch disjoint files. t-10's `depends t-1,t-3,t-4` is correct. |
| **mvp** (6 tasks / 3 waves) | 7/7 claimed, thinnest in practice. Same two misses as risk. c-5 is folded into t-1, so the "fails against pre-milestone code" criterion has no independently checkable owner. | Good prose contracts with real negative controls (delta-gate no-op, corrupt state.json must still fail loudly), but no test names — weakest traceability of the three. | Too coarse. t-1 carries untrack + ignore helper + init wiring + the whole e2e regression; t-3 carries the version writer + release.yml + the in-repo data fix across 5 files and 2 criteria. Both are multi-concern tasks whose green light means less. | One real defect: t-5 (bootstrap) is listed `depends t-3`, but its actual precondition is t-1 — untracking is what makes a clone state-less; t-3 only supplies the seed value. Otherwise correct, and it is the only draft that names the doctor.go merge hazard as the reason to keep doctor a single task. |
| **verification** (11 tasks / 3 waves) | Best. 7/7 plus the two fallout paths no one else found — `ship.go`'s `git add` on a now-ignored path (ship fails outright) and `status.go`'s origin-state read (fails *silently* into a warning that never fires again). Confirmed both at those exact lines. | Best. Every contract names the assertion that flips and often the mutation that flips it ("delete the project.toml write and the first assertion fails"). t-11's `git add -f` sibling sub-test — proving the main assertion has teeth — is the single sharpest contract in the panel. | 11 tasks, one mechanism each. t-10 (history-survives table test) partly overlaps t-11 but is justified: c-3 spans four commands and only two are touched by code tasks. | Best. Real two-level dependency structure rather than a flat wave 1; t-6 correctly waits on the ignore landing, t-8 on all four of its inputs. |

**Skeleton: `verification`.** It is the only draft whose task set actually leaves
the tree working after the untrack — `ship` and `status` both break on paths the
other two never open — and its contracts are written as failing tests first,
which is what a phase gated by `dross verify` needs. risk supplies the sharper
task boundaries and negative controls; mvp supplies two concrete corrections
(squash mechanism, detector placement) where the skeleton is factually wrong.

## Merged plan

12 tasks across 3 waves.

### Wave 1

```
t-1  Untrack state.json and gitignore it                       [verification+risk]
     files:    .gitignore, internal/cmd/gitignore.go,
               internal/cmd/gitignore_test.go, internal/cmd/init.go,
               internal/cmd/onboard.go
     covers:   c-1
     depends:  —
     desc:     New ensureDrossGitignore(repoDir) mirroring ensureDrossGitattributes
               (gitattributes.go:22); called from init.go:94 and onboard.go:112
               beside the existing ensureDrossGitattributes calls. This repo's
               .gitignore gains the line next to the .dross/local.toml block, and
               `git rm --cached .dross/state.json` takes it out of the index.
     contract: - TestStateJSONNotTracked: `git ls-files .dross/state.json` in the
                 repo root is empty. It prints a path today — that flip is the task.
               - TestInitScaffoldsStateIgnore / TestOnboardScaffoldsStateIgnore:
                 after each, `git check-ignore .dross/state.json` exits 0 and
                 `git ls-files` prints nothing.
               - TestEnsureDrossGitignoreIsIdempotent: called twice, the line
                 appears exactly once (grep count == 1).
               - A .gitignore already matching via a broader pattern
                 (`.dross/*.json`) is left byte-for-byte unchanged.        [verification]
               - TestGitignoredStateSurvivesAdd: `git add .dross` leaves
                 `git diff --cached --name-only` without .dross/state.json. [risk]
     note:     mvp additionally asserts `git status --porcelain` shows no untracked
               .dross/state.json — keep it; it proves the ignore pattern matches the
               path git actually reports, which a `.dross/state.json` vs
               `/.dross/state.json` slip would break.

t-2  Materialize a missing state.json locally                    [verification+mvp]
     files:    internal/cmd/root.go, internal/cmd/root_test.go,
               internal/cmd/state.go, internal/cmd/incompleteroot_test.go
     covers:   c-1
     depends:  —
     desc:     state.json leaves RequiredRootFiles (root.go:27). A shared
               ensureState(root) creates it on demand, seeding Version from
               project.toml [project].version and an empty History
               (history_durability). Never overwrites an existing file.
     contract: - In a .dross/ holding project.toml + rules.toml and NO state.json,
                 `dross status` exits 0 and afterwards state.json exists with
                 version == project.toml's [project].version and len(History)==0.
                 Today FindRoot returns IncompleteRootError — that is the flip.
               - A checkout missing project.toml still errors, naming
                 .dross/project.toml only; state.json must not be in Missing.
               - ensureState run twice against a 4-entry state.json leaves the 4
                 entries intact.
               - A present-but-corrupt state.json still fails loudly with an
                 unmarshal error, never silently replaced by a fresh one.    [mvp]

t-3  One writer for both version homes                     [risk+mvp+verification]
     files:    internal/cmd/state.go, internal/cmd/state_test.go,
               internal/cmd/project.go, internal/cmd/project_test.go,
               .dross/project.toml
     covers:   c-4
     depends:  —
     desc:     writeVersion(root, v) validates the 4-part form, then writes BOTH
               state.json's version and project.toml's [project].version.
               `state set version` (state.go:102), `state bump internal`
               (state.go:171) and `project set project.version` (project.go:316)
               all route through it. Also corrects .dross/project.toml's dead
               0.2.0.0 to the live value (state.json currently reads 1.2.1.0).
     contract: - TestBumpInternalWritesBothFiles: from 1.2.1.0, `state bump
                 internal` leaves state.json AND project.toml at 1.2.1.1;
                 project.toml re-read from disk, not from memory.       [verification]
               - TestProjectSetVersionMirrorsToState: the reverse direction —
                 `project set project.version 1.3.0.0` leaves state.json at
                 1.3.0.0 too.                                                [risk+mvp]
               - TestWriteVersionRejectsBeforeWriting: a 3-part "1.2.3" exits
                 non-zero and BOTH files still hold the previous value.        [risk]
               - TestWriteVersionFailureNamesBothPaths: with project.toml
                 chmod 0444, exit non-zero, error text contains "project.toml",
                 and state.json is not left ahead of it — no half-write.
               - In-repo parity: .dross/project.toml [project].version equals
                 .dross/state.json's version (guards the dead-value regression). [mvp]

t-4  Read the release version from project.toml                    [risk skeleton]
     files:    scripts/release-version.sh, .github/workflows/release.yml,
               internal/cmd/release_version_test.go
     covers:   c-2
     depends:  —
     desc:     POSIX sh script resolving [project].version from .dross/project.toml
               with grep/sed — no jq, no dross binary. release.yml's "Determine
               release tag" step (release.yml:39-63) calls it; the `jq -r '.version
               // empty' .dross/state.json` read at line 49 and the stale header
               comment at lines 3-4 go with it.
     contract: - TestReleaseVersionScriptResolvesTag: `sh scripts/release-version.sh`
                 against a fixture .dross/project.toml with version = "1.2.3.4"
                 prints v1.2.3, in a temp dir with no state.json and no dross
                 binary — that absence IS the test.               [risk+verification]
               - TestReleaseVersionScriptRejectsMissingVersion: a [project] table
                 with no version line exits non-zero, prints nothing on stdout,
                 and names .dross/project.toml on stderr.
               - TestReleaseVersionScriptIgnoresOtherTables: a project.toml where
                 [stack] also carries `version = "9.9.9.9"` below [project] still
                 yields v1.2.3 — the grep must be anchored to [project].
               - TestReleaseWorkflowHasNoStateJSONRead: release.yml contains
                 neither "state.json" nor "jq" in the release job.

t-5  Detect stale milestone branches, squash-aware       [risk+mvp+verification]
     files:    internal/cmd/milestone_stale.go, internal/cmd/milestone_stale_test.go
     covers:   c-7
     depends:  —
     desc:     staleMilestoneBranches(repoDir, mainBranch) lists refs/heads/milestone/*,
               marking a branch stale via ancestry first (reason "merged"), then a
               squash-aware content check (reason "squash-merged"). Reports HasRemote
               per branch so prune knows whether to issue `push --delete`. Read-only:
               no ref is written or deleted here.
     contract: - Fixture: milestone/v1.0 whose 3 commits were squash-merged into
                 main as one commit. `git branch --merged main` does NOT list it;
                 the detector DOES, with reason "squash-merged".
               - ALSO: the same fixture with main carrying 2 further commits after
                 the squash, and 1 commit landing on main between the fork and the
                 merge. See the disagreement note — both drafts' named mechanisms
                 fail this shape against this repo's own history.
               - A fast-forwarded milestone/v1.1 is reported with reason "merged",
                 not "squash-merged" — the two reasons must not collapse. [verification]
               - milestone/v1.2 with one commit absent from main in any form is not
                 in the result at all.
               - HasRemote true for a pushed branch, false for a local-only one.
               - A stale phase/foo branch is never in the result set.          [risk]
               - No milestone/* refs at all → empty slice, nil error.  [verification]
```

### Wave 2

```
t-6  Stop staging and restoring state.json                  [verification only]
     files:    internal/cmd/ship.go, internal/cmd/ship_test.go,
               internal/cmd/ship_recover.go, internal/cmd/ship_recover_test.go
     covers:   c-3
     depends:  t-1, t-2
     desc:     ship.go:231-233 drops the `git add .dross/state.json` fold and keeps
               the local Touch. runDrossRecovery's `git checkout <sha> -- .dross/`
               (ship_recover.go:177) gains `:(exclude).dross/state.json`, as does
               the `git add .dross/` that follows, so restoring from a pre-untrack
               commit neither overwrites the live file nor re-stages it. The now-false
               comment at ship_recover.go:184 is corrected.
     contract: - `dross ship` in a repo where .dross/state.json is gitignored exits 0.
                 Left unchanged, `git add` on an ignored untracked path errors
                 "paths are ignored by one of your .gitignore files" and ship fails
                 outright — the sharpest single assertion in the phase.
               - `dross ship` still records "completed <id>" in the LOCAL history,
                 and `git show HEAD:.dross/state.json` fails.
               - TestRecoveryDoesNotRestoreStateJSON: pre-merge SHA carries a
                 2-entry state.json, working tree has 40; after runDrossRecovery
                 the on-disk file still has 40.                                [risk]
               - TestRecoveryDoesNotRetrackStateJSON: `git ls-files
                 .dross/state.json` empty, and the recovery commit's
                 `git show --name-only` does not list it.                      [risk]
               - TestRecoveryStillRestoresPhaseArtifacts: the same run DOES restore
                 .dross/phases/<id>/spec.toml — proving the exclusion is scoped to
                 one file, not a disabled restore.                             [risk]
               - The delta gate still no-ops: a base already carrying the full
                 .dross/ tree produces zero commits — the exclude pathspec must not
                 manufacture a phantom delta.                                   [mvp]

t-7  Re-source status's stale-completion warning            [verification only]
     files:    internal/cmd/status.go, internal/cmd/status_test.go
     covers:   c-3
     depends:  t-1
     desc:     staleCompletedState (status.go:473) reads `git show
               origin/<main>:.dross/state.json` at line 487, which is permanently
               unreadable once the file is untracked. Re-source the
               "shipped but unmerged" signal from the phase's changes.json PR record
               (or drop the check), so the warning is not silently dead.
     contract: - On a phase branch whose local state records "completed <id>" and
                 whose PR is NOT merged, `dross status` still prints the
                 shipped-but-unmerged warning naming <id>. Today that path returns
                 ("", false) the moment the git show fails.
               - On a phase branch whose work IS on origin/<main>, no such warning
                 — the false-positive side a naive "always warn" fix would break.
               - With no origin configured, `dross status` exits 0 and prints no
                 warning (silence on uncertainty preserved).

t-8  Diagnose a checkout blocked by live state.json                 [risk only]
     files:    internal/cmd/switchbranch.go, internal/cmd/switchbranch_test.go,
               internal/cmd/phase.go, internal/cmd/milestone.go
     covers:   c-1, c-3
     depends:  t-1
     desc:     New checkoutBranch(repoDir, branch) used by phase.go:429 and
               milestone.go:148. When git refuses because an untracked
               .dross/state.json would be overwritten, return a dross error naming
               the file and the back-up/restore commands instead of a raw pathspec
               dump. Never moves or deletes the live file.
     contract: - TestCheckoutRefusalNamesStateJSON: switching to a branch that still
                 tracks .dross/state.json while an untracked live copy exists
                 returns an error containing ".dross/state.json" and
                 "cp .dross/state.json"; the live file keeps its N entries and HEAD
                 has not moved.
               - TestCheckoutRefusalPassesThroughOtherErrors: a checkout blocked by
                 an unrelated untracked file (README.md) returns git's own message
                 unmodified — the classifier must not swallow every failure.
               - TestPhaseCompleteSurfacesCheckoutRefusal: phase complete against a
                 base branch in that state exits non-zero with the named fix and
                 deletes neither the local nor the remote phase branch.

t-9  Add doctor checks for state, version drift, stale branches
                                                        [risk+mvp+verification]
     files:    internal/cmd/doctor.go, internal/cmd/doctor_test.go
     covers:   c-6, c-7
     depends:  t-1, t-2, t-3, t-5
     desc:     Three new doctor sections: .dross/state.json still in the index;
               project.toml [project].version missing or diverged from state.json;
               stale milestone/* branches reported read-only via t-5's detector.
               Each prints the exact fix command. Also drops state.json from the
               foundational-files trio, which t-2 made optional.
     contract: - `git ls-files .dross/state.json` non-empty → doctor exits non-zero
                 and the output contains the literal `git rm --cached
                 .dross/state.json` — the exact fix, not a description of it.
               - project.toml at 1.1.0.0 while state.json is at 1.2.1.0 → non-zero
                 exit, BOTH values printed on one line so the reader knows which is
                 stale without opening either file.
               - Missing [project].version → non-zero exit naming project.toml, as
                 an issue distinct from the drift message; not a nil-deref or an
                 empty-string ✓.                                               [risk]
               - Versions equal, state untracked, no stale branch → all three
                 sections print ✓ and contribute 0 to the issue count.         [risk]
               - A squash-merged milestone/v1.0 is listed with the
                 `dross milestone prune v1.0` fix, and the branch still exists
                 after doctor returns (read-only, per prune_surface).
               - A repo with no .dross/state.json no longer reports it as a missing
                 foundational file.                                             [mvp]

t-10 Add `dross milestone prune`                        [risk+mvp+verification]
     files:    internal/cmd/milestone.go, internal/cmd/milestone_prune_test.go
     covers:   c-7
     depends:  t-5
     desc:     Explicit subcommand deleting stale milestone/* branches local and
               (when HasRemote) remote, via t-5's detector. Never touches a branch
               the detector did not mark stale. Refuses on a dirty tree and when the
               branch is current HEAD.
     contract: - Fixture with a squash-merged milestone/v1.0 (local + pushed) and an
                 unmerged milestone/v1.2: after prune, `git rev-parse --verify
                 refs/heads/milestone/v1.0` fails, `git ls-remote --heads origin
                 milestone/v1.0` is empty, and BOTH refs for milestone/v1.2 still
                 exist. Deleting the unmerged branch is the failure this asserts
                 against.
               - Prune while HEAD is on a stale milestone branch exits non-zero
                 naming the branch and deletes nothing — a clean refusal, not a
                 half-done sweep.
               - A stale local-only branch is deleted with no `push --delete`
                 attempted (asserted with no origin configured; still exits 0).
               - Prune with nothing stale exits 0 with a nothing-to-prune line;
                 running it twice is a clean no-op.
```

### Wave 3

```
t-11 Assert history survives every branch switch            [verification only]
     files:    internal/cmd/state_history_test.go
     covers:   c-3
     depends:  t-6, t-7, t-8
     desc:     One table-driven test over the four branch-switching commands, each
               fixture seeded with N history entries, asserting every pre-switch
               entry is still present afterwards in order. Carries any fix it
               surfaces.
     contract: - For each of `phase complete`, `phase complete --recover`,
                 `ship recover`, `milestone complete --finalize`: state.json seeded
                 with 12 distinct actions; after the command all 12 are present in
                 the original order, plus the command's own entry appended last.
               - Each case additionally asserts state.json's version field is
                 unchanged by the switch — the incident's second symptom gets its
                 own assertion.
               - `phase complete --recover` specifically: entries survive even
                 though the recovery does `git reset --hard origin/<base>` and a
                 tree restore — the two operations that used to clobber.

t-12 Reproduce the incident end to end                  [risk+mvp+verification]
     files:    internal/cmd/state_clobber_regression_test.go
     covers:   c-5
     depends:  t-1, t-6, t-8
     desc:     TestStaleBranchCheckoutCannotClobberLiveState: build the repo through
               `dross init` (not by hand-writing .gitignore), accumulate N history
               entries, check out a long-lived branch created before untracking that
               still tracks a 2-entry state.json, assert the live state survives.
     contract: - The fixture builds .gitignore only via `dross init`, so the test
                 fails against pre-milestone code: there the checkout succeeds and
                 state.json drops from 12 entries to 2. The assertion is
                 "history length >= 12 AND the first seeded action is still
                 present", with the checkout allowed to have exited non-zero.
               - A sibling sub-test pins the opposite direction: with state.json
                 deliberately re-added via `git add -f`, the same checkout DOES
                 clobber it — proving the assertion has teeth and is not passing for
                 an unrelated reason.                                  [verification]
               - TestIncidentRecoverKeepsLiveHistory: `phase complete --recover`
                 against a pre-merge SHA carrying the 2-entry copy ends with the
                 live entries on disk and no state.json in the resulting commit. [risk]
               - After the surviving checkout, `dross state get version` still
                 prints the live version, not the stale branch's.
               - TestIncidentStateNeverEntersIndex: at every step,
                 `git ls-files .dross/state.json` stays empty.                 [risk]
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 (checkout cannot silently replace live state) | t-1, t-2, t-8 |
| c-2 (release.yml resolves version, no dross binary) | t-4 |
| c-3 (branch-switching commands preserve history) | t-6, t-7, t-8, t-11 |
| c-4 (release and dross versions cannot diverge) | t-3, t-9 |
| c-5 (end-to-end incident regression) | t-12 |
| c-6 (doctor detects tracked state / stale version) | t-9 |
| c-7 (stale milestone branch, incl. squash-merged) | t-5, t-9, t-10 |

7/7 covered.

## Disagreements

### 1. Squash-merge detection mechanism — and both proposals are empirically wrong

**Diverged:** how t-5 identifies a branch whose work reached main via squash.
**risk / mvp:** synthesize the branch as one commit off its merge-base
(`git commit-tree`) and test that patch-id against main with `git cherry`.
mvp explicitly rejects a tree diff because "a tree comparison is wrong the moment
main carries unrelated later work".
**verification:** `git diff --quiet <main> <branch>` — an empty content diff,
explicitly rejecting patch-id because "a squash collapses N commits into one, so
per-commit patch-ids do not match".

**Provisional default: the risk/mvp mechanism (commit-tree + `git cherry`).**
verification's argument is aimed at the wrong target — mvp is not comparing
per-commit patch-ids, it is comparing one synthetic squash against main's squash,
which is exactly the shape c-7 names.

**But I tested both against this repo's own history and both fail it.** Against
`origin/phase/complete-base-truth`, squash-merged as a8d6a5c (#70):

- `git branch --merged main` does not list it — c-7's premise confirmed.
- `git diff --quiet main <branch>` is **non-empty** → verification's arm misses it.
- commit-tree from `git merge-base` + `git cherry main <synth>` prints `+` → **also
  misses it.** Cause: the merge-base is d78135d, but the squash commit's parent is
  5a1a6c3, one commit later (`chore(dross): scope milestone v1.2` landed on main
  mid-phase). The two diffs differ by 1 file / 8 lines, so the patch-ids differ.

**Why it matters:** dross scopes milestones onto main while a phase branch is
open, so "main advanced between fork and merge" is the *normal* shape here, not an
edge case. A detector built to either draft's letter reports nothing stale, c-7
passes vacuously, and `milestone prune` becomes a command that never fires. The
merged plan therefore keeps the mechanism as the default but adds a required
fixture — main advancing both before and after the squash — that both drafts'
contracts would pass without exercising. Executing t-5 should expect to need a
third arm (e.g. patch-id against the squash commit's own parent, or path-scoped
content equality) rather than either draft as written.

### 2. Where the stale-branch detector lives

**risk / verification:** `internal/milestone/stale.go`. **mvp:**
`internal/cmd/milestone_prune.go`.
**Provisional default: mvp's — `internal/cmd`.** The 2-of-3 majority is wrong on
the facts: `internal/milestone` is a pure TOML-file package (`grep -c os/exec` → 0),
while every git helper the detector needs (`gitTrim`, `gitCombined`, `gitNoOut`)
lives in `internal/cmd` (ship_recover.go:239/248, phase.go:714). Putting a
git-shelling detector in `internal/milestone` means duplicating those helpers or
giving a config package a subprocess dependency.
**Why it matters:** picking the majority here costs a helper duplication that
every later git-touching milestone feature inherits. Split the file name
(`milestone_stale.go`) so t-5 and t-10 don't collide in one file.

### 3. Fresh-clone bootstrap: explicit command vs auto-materialize

**risk:** a new `dross state init` that refuses to overwrite, plus an
`IncompleteRootError` that names it — "a read path that silently writes to disk is
a worse failure mode than a clear refusal".
**mvp / verification:** drop state.json from `RequiredRootFiles` and have
load/`ensureState` create it on demand.
**Provisional default: auto-materialize (mvp/verification).** A CI checkout and a
fresh clone must work with no human step, and c-2 explicitly requires release.yml
to run against a clean checkout; a refusal that names a command nobody is present
to type is a broken pipeline. risk's concern is answered by the never-overwrite
and fail-loudly-on-corrupt assertions, both of which are kept.
**Why it matters:** it decides whether `dross <anything>` works in a fresh clone
or errors "not a dross repo" until a human intervenes. If the auto-create is
rejected in review, risk's `dross state init` is the ready-made fallback — but the
release workflow must then not depend on it.

### 4. `project set project.version`: refuse, or write both ways

**verification:** refuse it, pointing at `dross state set version`, so there stays
exactly one writer. **risk / mvp:** route it through the same `writeVersion`, so
either entry point writes both files.
**Provisional default: bidirectional (risk/mvp).** `project.go:316` already
handles `project.version` today; removing a working CLI surface is a behaviour
change no criterion asks for, and c-4 says "one bump writes both" — one writer
*function*, not one entry point.
**Why it matters:** verification's shape is defensible and arguably cleaner, but
it is a silent CLI removal inside a safety phase. If the author wants the single
entry point, that is a deliberate call to record, not a side effect of t-3.

### 5. Explaining the checkout refusal (t-8) exists in one draft only

**risk** has it; **mvp and verification** have no equivalent task at all, treating
the locked `state_tracking` acceptance ("git refuses the checkout, which c-1
accepts") as the end of the story.
**Provisional default: keep it, demoted to wave 2.** `milestone/v1.2` still tracks
the file right now, so the first `phase complete` after t-1 lands hits
`error: untracked working tree file '.dross/state.json' would be overwritten` for
real, mid-command. c-1 accepts the refusal; it does not accept a raw pathspec dump
as the user-facing output of `phase complete`.
**Why it matters:** it is the only task here that is pure UX and the only one no
criterion strictly demands — the first candidate to cut if the phase runs long.
Cutting it makes the phase's own first branch switch ugly but not unsafe.

### 6. Post-untrack fallout: `ship.go` and `status.go`

Not a stated divergence — a coverage gap, recorded here because risk and mvp both
claim c-3 complete without it.
**verification alone** identified `ship.go:231-233` (`git add .dross/state.json`,
which errors on an ignored untracked path and fails `dross ship` outright) and
`status.go:487` (`git show origin/<main>:.dross/state.json`, which after untracking
fails forever, silently killing the shipped-but-unmerged warning).
**Provisional default: both in scope** (t-6, t-7). Verified at those lines.
**Why it matters:** without t-6, this phase ships a tree where `dross ship` cannot
run — the phase would fail its own ship step. The two failures are kept as separate
tasks per verification's reasoning: one is loud, one is silent, and folding them
lets the loud one's green test stand in for both.

### 7. The c-5 regression test: own task or folded in

**risk / verification:** its own task (risk t-10, verification t-11).
**mvp:** folded into t-1, on the grounds that it exercises exactly t-1's mechanism.
**Provisional default: own task (t-12).** c-5 requires a test that *fails against
pre-milestone code*; that property is only checkable if the test is identifiable
and can be run against the base commit. Buried inside t-1 alongside the untrack, it
is trivially green for the wrong reason.
**Why it matters:** c-5 is the criterion most at risk of passing vacuously. mvp's
cost argument (one extra wave hop) is real but small.

### 8. Release version: extracted script vs inline YAML

**risk:** `scripts/release-version.sh`, called by the workflow step, runnable under
`sh` in a Go test. **mvp / verification:** keep the logic inline in release.yml and
have the test extract the step body and run it.
**Provisional default: the script (risk).** c-2 demands the resolution work with no
dross binary; a real file is directly executable in a test, whereas extracting a
YAML step body couples the test to the workflow's formatting. Note `scripts/` does
not exist in this repo — t-4 creates it.
**Why it matters:** low stakes, but a YAML-string-extraction test is the kind that
passes on a substring while the anchor is wrong. All three drafts independently
wrote the [project]-anchoring assertion, which is the real protection either way.

### 9. Doctor's stale-branch section: issue or advisory

**mvp:** advisory warning, no exit-code change — "prune_surface locks doctor as
read-only; deletion staying explicit means the report must not gate CI".
**risk / verification:** counts as an issue and raises the exit code.
**Provisional default: issue (risk/verification).** `prune_surface` makes doctor
read-only about *acting*, not about *reporting severity*, and a stale milestone
branch has an unambiguous named fix (`dross milestone prune <v>`) — the same test
the merged plan applies to the tracked-state and version-drift sections.
**Why it matters:** it decides whether a leftover milestone branch reddens the
author's `dross doctor`. Cheap to flip either way; worth being deliberate, because
an advisory-only line is one nobody ever acts on.
