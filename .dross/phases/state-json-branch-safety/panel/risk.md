# Panel draft — risk lens

Phase state-json-branch-safety — 10 tasks across 3 waves

Lens: every task owns exactly one way this can break. The failure modes that
drive the graph, in order of blast radius:

1. Untracking `.dross/state.json` makes a fresh clone have **no** state.json —
   `RequiredRootFiles` (internal/cmd/root.go:27) lists it, so every command
   dies with "not a dross repo". The fix is a task, not a footnote (t-2).
2. Untracking does **not** close the clobber: `runDrossRecovery` runs
   `git checkout <sha> -- .dross/` (internal/cmd/ship_recover.go:177), and old
   commits still carry state.json per the locked `migration_scope`. That
   restore both overwrites the live file *and* re-stages it into the index,
   silently re-tracking it — then `git add .dross/` (ship_recover.go:186)
   commits it back (t-3).
3. Every existing branch (including `milestone/v1.2`) still tracks the file, so
   the first `phase complete` after the untrack hits `error: untracked working
   tree file '.dross/state.json' would be overwritten by checkout`. c-1 accepts
   the refusal; a raw git pathspec dump mid-complete is still a broken command
   (t-4).
4. Two version writers can half-write: state.json updated, project.toml not
   (t-5) — and the release pipeline reads whatever survived (t-6).
5. `git branch --merged` cannot see a squash-merge, so naive staleness either
   never fires or fires on unmerged work — and c-7's remedy deletes a **remote**
   branch (t-7 detection, t-9 destructive command, deliberately separate).

---

## Wave 1

```
t-1  Untrack state.json and ignore it everywhere
     files:    .gitignore, internal/cmd/gitignore.go, internal/cmd/init.go,
               internal/cmd/onboard.go
     covers:   c-1
     depends:  —
     desc:     git rm --cached .dross/state.json in this repo and add the ignore
               line next to the existing .dross/local.toml block. New
               ensureDrossGitignore(repoDir), mirroring ensureDrossGitattributes,
               called from init.go:94 and onboard.go:112 so onboarded repos get
               the ignore before their first commit.
     contract: - TestStateJSONNotTracked: `git ls-files .dross/state.json` in the
                 repo root returns empty; it returns a path today, so the task is
                 not done until it flips.
               - TestEnsureDrossGitignoreIsIdempotent: two calls on a .gitignore
                 that already carries the line leave the file byte-identical and
                 do not duplicate the entry.
               - TestGitignoredStateSurvivesAdd: in a temp repo scaffolded by
                 ensureDrossGitignore, `git add .dross` leaves
                 `git diff --cached --name-only` without .dross/state.json.
               - TestInitScaffoldsStateIgnore / TestOnboardScaffoldsStateIgnore:
                 after each command, `git check-ignore .dross/state.json` exits 0.

t-2  Bootstrap state.json for fresh clones
     files:    internal/cmd/state.go, internal/cmd/root.go
     covers:   c-1
     depends:  —
     desc:     Add `dross state init`: writes a fresh state.json (empty history,
               version seeded from project.toml's [project].version) when absent,
               refuses when one exists. IncompleteRootError names that command
               when state.json is the only missing required file.
     contract: - TestStateInitSeedsFromProjectVersion: in a .dross/ holding
                 project.toml (version 1.2.0.0) and no state.json, `state init`
                 writes state.json with version 1.2.0.0 and len(history)==0.
               - TestStateInitRefusesOverwrite: with an existing 12-entry
                 state.json, `state init` exits non-zero and the file still has
                 12 entries and its original version.
               - TestIncompleteRootNamesStateInit: an IncompleteRootError whose
                 Missing is exactly [.dross/state.json] renders a message
                 containing "dross state init"; one missing project.toml keeps
                 the existing RepairHint wording.

t-3  Exclude state.json from .dross tree restore
     files:    internal/cmd/ship_recover.go, internal/cmd/phase.go
     covers:   c-3
     depends:  —
     desc:     runDrossRecovery's `git checkout <sha> -- .dross/` gains an
               exclude pathspec for .dross/state.json, and the staging step that
               follows must not re-add it to the index. The live file is the
               survivor of both `ship recover` and `phase complete --recover`.
     contract: - TestRecoveryDoesNotRestoreStateJSON: pre-merge SHA carries a
                 2-entry state.json, working tree has 40 entries; after
                 runDrossRecovery the on-disk file still has 40.
               - TestRecoveryDoesNotRetrackStateJSON: after the same run,
                 `git ls-files .dross/state.json` is empty and the recovery
                 commit's `git show --name-only` does not list it.
               - TestRecoveryStillRestoresPhaseArtifacts: the same run does
                 restore .dross/phases/<id>/plan.toml from the SHA — proving the
                 exclusion is scoped to one file, not a disabled restore.

t-4  Diagnose checkout blocked by live state.json
     files:    internal/cmd/switchbranch.go, internal/cmd/phase.go,
               internal/cmd/milestone.go
     covers:   c-1, c-3
     depends:  —
     desc:     New checkoutBranch(repoDir, branch) wrapper used by phase.go:429
               and milestone.go:148. When git refuses because an untracked
               .dross/state.json would be overwritten, return a dross error
               naming the file and the back-up/restore commands instead of the
               raw pathspec output. Never moves or deletes the live file.
     contract: - TestCheckoutRefusalNamesStateJSON: switching to a branch that
                 still tracks .dross/state.json while an untracked live copy
                 exists returns an error containing ".dross/state.json" and
                 "cp .dross/state.json"; the live file still has its N history
                 entries and HEAD has not moved.
               - TestCheckoutRefusalPassesThroughOtherErrors: a checkout blocked
                 by an unrelated untracked file (README.md) returns git's own
                 message, unmodified — the classifier must not swallow every
                 checkout failure.
               - TestPhaseCompleteSurfacesCheckoutRefusal: phase complete against
                 a base branch in that state exits non-zero with the named fix
                 and deletes neither the local nor the remote phase branch.

t-5  One writer for both version files
     files:    internal/cmd/versionwrite.go, internal/cmd/state.go,
               internal/cmd/project.go
     covers:   c-4
     depends:  —
     desc:     writeVersion(root, v) validates the 4-part form, then writes
               state.json's version and project.toml's [project].version.
               `state set version`, `state bump internal` (state.go:102,171) and
               `project set version` (project.go:317) all route through it.
     contract: - TestBumpInternalWritesBothFiles: from 1.2.0.0, `state bump
                 internal` leaves state.json AND project.toml at 1.2.0.1.
               - TestWriteVersionRejectsBeforeWriting: a 3-part "1.2.3" exits
                 non-zero and both files still hold the previous value (no
                 half-write).
               - TestWriteVersionFailureNamesBothPaths: with project.toml
                 chmod 0444, the command exits non-zero, the error text contains
                 "project.toml", and state.json is not left ahead of it.
               - TestProjectSetVersionMirrorsToState: `project set version
                 1.3.0.0` leaves state.json's version at 1.3.0.0 too.

t-6  Read release version from project.toml
     files:    scripts/release-version.sh, .github/workflows/release.yml,
               internal/cmd/release_version_test.go
     covers:   c-2
     depends:  —
     desc:     POSIX sh script resolving [project].version from
               .dross/project.toml with grep/sed (no jq, no dross binary) and
               printing the 3-part tag. release.yml's "Determine release tag"
               step calls it; its state.json jq read and the stale header comment
               go with it.
     contract: - TestReleaseVersionScriptResolvesTag: sh scripts/release-version.sh
                 against a fixture .dross/project.toml with version = "1.2.3.4"
                 prints "v1.2.3".
               - TestReleaseVersionScriptRejectsMissingVersion: a project.toml
                 whose [project] table has no version line exits non-zero and
                 prints nothing on stdout.
               - TestReleaseVersionScriptIgnoresOtherTables: a project.toml where
                 a later table also has `version = "9.9.9.9"` still prints
                 v1.2.3 — the grep must be anchored to [project].
               - TestReleaseWorkflowHasNoStateJSONRead: .github/workflows/
                 release.yml contains neither "state.json" nor "jq -r '.version".

t-7  Detect squash-merged stale milestone branches
     files:    internal/milestone/stale.go
     covers:   c-7
     depends:  —
     desc:     StaleBranches(repoDir, mainBranch) reports milestone/* branches
               whose work is already on main: ancestry first, then the
               commit-tree + `git cherry` equivalence check that catches a
               squash-merge. Read-only — no ref is written or deleted here.
     contract: - TestSquashMergedBranchReportedStale: a fixture repo where
                 milestone/v1.0's commits were squash-merged onto main (no
                 ancestry) reports milestone/v1.0 stale; the ancestry-only path
                 alone leaves it unreported.
               - TestUnmergedBranchNotStale: milestone/v1.1 with one commit whose
                 diff is absent from main is not reported.
               - TestMergeCommitBranchReportedStale: an ordinary --no-ff merged
                 milestone branch is still reported (the ancestry path keeps
                 working).
               - TestNonMilestoneBranchIgnored: a stale phase/foo branch is never
                 in the result set.
```

## Wave 2 (depends on wave 1)

```
t-8  Doctor: tracked state, version drift, stale branches
     files:    internal/cmd/doctor.go
     covers:   c-6, c-7
     depends:  t-2, t-5, t-7
     desc:     Three new doctor sections: state.json still in the index; project
               .toml version missing or diverged from state.json; stale
               milestone/* branches from StaleBranches. Each prints the exact fix
               command. Runs after the foundational-files block so a fresh clone
               reports the missing-state.json path first.
     contract: - TestDoctorFlagsTrackedStateJSON: with .dross/state.json in the
                 index, doctor exits non-zero and prints
                 "git rm --cached .dross/state.json".
               - TestDoctorFlagsVersionDivergence: state.json 1.2.1.0 vs
                 project.toml 1.2.0.0 → non-zero exit and a line naming
                 "dross project set version 1.2.1.0".
               - TestDoctorFlagsMissingProjectVersion: project.toml with no
                 [project].version → non-zero exit naming project.toml, not a
                 nil-deref or an empty-string "✓".
               - TestDoctorCleanRepoStaysGreen: untracked state.json, matching
                 versions, no stale milestone branch → those three sections all
                 print ✓ and contribute 0 to the exit code.
               - TestDoctorReportsStaleMilestoneBranchReadOnly: a squash-merged
                 milestone/v1.0 is listed with the `dross milestone prune v1.0`
                 fix, and the branch still exists after doctor returns.

t-9  Add explicit `dross milestone prune`
     files:    internal/cmd/milestone.go
     covers:   c-7
     depends:  t-7
     desc:     `dross milestone prune <version>` deletes milestone/<version>
               local and remote, but only when StaleBranches reports it stale.
               Refuses on a dirty tree, when the branch is the current HEAD, and
               when the work is not on main. Idempotent when the branch is gone.
     contract: - TestMilestonePruneDeletesSquashMergedBranch: local ref and the
                 fixture remote's ref are both gone afterwards.
               - TestMilestonePruneRefusesUnmerged: a branch with a commit absent
                 from main → non-zero exit, and both local and remote refs still
                 resolve.
               - TestMilestonePruneRefusesCurrentBranch: run while HEAD is on
                 milestone/<version> → non-zero exit, HEAD unchanged.
               - TestMilestonePruneIsIdempotent: a second run with no local and no
                 remote ref exits 0 and pushes nothing.
```

## Wave 3 (depends on wave 1)

```
t-10 Reproduce the state-clobber incident end to end
     files:    internal/cmd/state_clobber_regression_test.go
     covers:   c-5
     depends:  t-1, t-3, t-4
     desc:     One test file driving the whole incident on a real temp git repo:
               a live state.json with 30 history entries, a long-lived branch
               carrying a 2-entry copy, and the checkout that used to swap them.
               Scaffolds through the production helpers (ensureDrossGitignore,
               checkoutBranch, runDrossRecovery) so the guards are what it
               exercises, not a re-implementation.
     contract: - TestIncidentCheckoutKeepsLiveHistory: after checking out the
                 stale branch, the on-disk state.json either still has 30 entries
                 or the checkout returned an error — the 2-entry outcome, which
                 is what today's tree produces, fails the test.
               - TestIncidentRecoverKeepsLiveHistory: `phase complete --recover`
                 against a pre-merge SHA carrying the 2-entry copy ends with 30
                 entries on disk and no state.json in the resulting commit.
               - TestIncidentStateNeverEntersIndex: at every step of the repro,
                 `git ls-files .dross/state.json` stays empty.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (checkout cannot silently replace live state) | t-1, t-2, t-4 |
| c-2 (release.yml resolves version, no dross binary) | t-6 |
| c-3 (branch-switching commands keep history) | t-3, t-4 |
| c-4 (release version and dross version cannot diverge) | t-5 |
| c-5 (end-to-end incident regression) | t-10 |
| c-6 (doctor detects tracked state / stale version file) | t-8 |
| c-7 (stale milestone branch, incl. squash-merged) | t-7, t-8, t-9 |

7/7 criteria covered.

## Judgment calls

- **Fresh-clone bootstrap is a task, not a side effect.** `RequiredRootFiles`
  (root.go:27) makes a state.json-less clone "not a dross repo". I chose an
  explicit `dross state init` plus a targeted error message over auto-creating
  the file inside `FindRoot` — a read path that silently writes to disk is a
  worse failure mode than a clear refusal, and doctor (t-8) needs a command to
  name anyway.
- **The recovery exclusion (t-3) is separate from the untrack (t-1).** Untracking
  alone leaves `git checkout <sha> -- .dross/` restoring *and re-staging* the old
  copy. Merging them into one "make state.json local" task would hide the second,
  larger bug behind the first one's green test.
- **Checkout refusals are explained, never auto-healed (t-4).** A move-aside /
  restore-after wrapper would be a snapshot guard — the exact mechanism
  `state_tracking` rejected — and its failure mode is losing the live file. It
  reports the fix and touches nothing.
- **One version writer, accepting project.toml re-encode.** `writeVersion` goes
  through the existing `project.Load`/`Save` round-trip, which drops comments —
  the precedent `project set version` already set. The alternative (surgical
  sed on the version line) avoids the churn but adds a second, unvalidated
  parser of the file dross already has a parser for.
- **Version divergence is a doctor issue, not a `validate` failure.** validate
  runs inside every slash-command wrap; a new failing check there breaks repos
  mid-loop. That is the same reasoning the `status_check_home` decision recorded
  for task statuses.
- **Staleness detection (t-7) and deletion (t-9) are different tasks.** The
  detector's risk is a false positive; prune's risk is an irreversible remote
  delete. Splitting them means the false-positive guard is tested without a
  destructive fixture, and prune's refusal paths get their own tests.
- **The release version lives in a script, not inline YAML.** Locked
  `version_home` fixes the source (project.toml, grep/sed); putting the four
  lines in `scripts/release-version.sh` makes them executable under `sh` in a Go
  test. Inline YAML could only be string-matched, and a wrong grep anchor passes
  a substring assertion.
- **t-6 is wave 1, not wave 2 behind t-5.** The script's correctness is testable
  against fixture project.toml files; only the *live* value depends on the
  writer, and that link is what t-8's divergence check enforces.
