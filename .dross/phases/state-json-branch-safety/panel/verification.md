# Panel draft — verification lens

Every task below was derived by writing the criterion's ideal failing test
first, then asking what is the smallest change that makes that test pass.

Phase state-json-branch-safety — 11 tasks across 3 waves

## Wave 1

```
t-1  Untrack state.json and gitignore it
     files:    .gitignore,
               internal/cmd/gitignore.go,
               internal/cmd/gitignore_test.go,
               internal/cmd/init.go,
               internal/cmd/onboard.go
     covers:   c-1
     desc:     New ensureDrossGitignore(repoDir), mirroring ensureDrossGitattributes,
               appends `.dross/state.json` to .gitignore; init and onboard call it.
               This repo's own .gitignore gains the line and the file leaves the index
               (git rm --cached .dross/state.json).
     contract: - After `dross init` in a throwaway repo, `git check-ignore
                 .dross/state.json` exits 0 and `git ls-files .dross/state.json`
                 prints nothing. If the init path stops calling
                 ensureDrossGitignore, both assertions flip.
               - ensureDrossGitignore called twice appends the line exactly once
                 (grep count == 1) — the idempotence surface; a naive append
                 duplicates and the test catches it.
               - A .gitignore that already carries the line via a broader pattern
                 (`.dross/*.json`) is left byte-for-byte unchanged.
               - Live state.json with 9 history entries + a branch that still
                 tracks a 2-entry copy: `git checkout <branch>` either exits
                 non-zero or leaves the working file at 9 entries. Never 2.

t-2  Materialize a missing state.json locally
     files:    internal/cmd/root.go,
               internal/cmd/root_test.go,
               internal/cmd/doctor.go,
               internal/cmd/incompleteroot_test.go
     covers:   c-1, c-4
     desc:     state.json leaves RequiredRootFiles and doctor's foundational trio.
               A shared ensureState(root) creates it on demand, seeding Version from
               project.toml [project].version and an empty History.
     contract: - In a checkout holding .dross/project.toml + .dross/rules.toml and
                 NO state.json, `dross status` exits 0 and afterwards
                 .dross/state.json exists with version == project.toml's
                 [project].version and History length 0. Today FindRoot returns
                 IncompleteRootError and the command exits non-zero — that is the
                 assertion that flips.
               - A checkout missing project.toml still reports "not a dross repo
                 — .dross/ is incomplete" naming .dross/project.toml only;
                 state.json must not appear in the Missing slice.
               - ensureState never overwrites: run twice against a state.json
                 holding 4 history entries, the entries survive.

t-3  Dual-write the version to project.toml
     files:    internal/cmd/state.go,
               internal/cmd/state_test.go,
               internal/cmd/project.go,
               internal/cmd/project_test.go
     covers:   c-4, c-2
     desc:     `state set version` and `state bump internal` write .dross/project.toml's
               [project].version in the same call. `project set project.version` is
               refused, pointing at `dross state set version`, so there stays exactly
               one writer.
     contract: - `dross state set version 1.2.3.4` → `dross project get
                 project.version` prints 1.2.3.4 and `dross state get version`
                 prints 1.2.3.4. Delete the project.toml write and the first
                 assertion fails.
               - `dross state bump internal` from 1.2.3.4 leaves BOTH files at
                 1.2.3.5 (project.toml re-read from disk, not from memory).
               - A read-only .dross/project.toml makes `state set version` exit
                 non-zero with an error naming project.toml, and state.json is
                 left at its previous value — no half-write.
               - `dross project set project.version 9.9.9.9` exits non-zero and the
                 message contains "dross state set version"; project.toml is
                 unchanged on disk.

t-4  Read the release version from project.toml
     files:    .github/workflows/release.yml,
               internal/cmd/release_version_source_test.go
     covers:   c-2
     desc:     The "Determine release tag" step resolves the 4-part version by
               grep/sed over .dross/project.toml's [project].version instead of
               `jq .version .dross/state.json`, and the step comment follows.
     contract: - The step's shell body, extracted from release.yml and run by
                 `sh -e` in a temp dir containing only .dross/project.toml with
                 version = "1.2.3.4", writes new_tag=v1.2.3 to $GITHUB_OUTPUT.
                 No dross binary, no state.json — that absence IS the test.
               - The same body against a project.toml with no version line exits
                 non-zero and its stderr names .dross/project.toml.
               - A grep assertion over release.yml: zero occurrences of
                 `state.json` and zero of `jq` in the release job. If someone
                 re-adds the old lookup, this fails.
               - The sed must not be fooled by another table's version key: a
                 project.toml where [stack] carries `version = "9.9.9.9"` below
                 [project] still yields v1.2.3.

t-5  Detect stale milestone branches, squash-aware
     files:    internal/milestone/stale.go,
               internal/milestone/stale_test.go
     covers:   c-7
     desc:     StaleBranches(repoDir, mainBranch) lists refs/heads/milestone/*, marking
               a branch stale when it is an ancestor of main (reason "merged") or when
               `git diff --quiet <main> <branch>` reports no content difference
               (reason "squash-merged"). Reports whether origin carries each ref.
     contract: - Fixture: milestone/v1.0 whose three commits were replayed into
                 main as one squash commit with different SHAs. `git branch
                 --merged main` does NOT list it; StaleBranches DOES, with reason
                 "squash-merged". Drop the tree-diff arm and this is the only
                 assertion that fails.
               - milestone/v1.1 fast-forwarded into main is reported with reason
                 "merged", not "squash-merged" — the two reasons must not collapse.
               - milestone/v1.2 carrying one commit absent from main is not in the
                 result at all.
               - A branch pushed to origin is reported HasRemote true; a
                 local-only one false — the field `milestone prune` needs to know
                 whether to issue a `push --delete`.
               - A repo with no milestone/* refs returns an empty slice and a nil
                 error (not an error).
```

## Wave 2 (depends on wave 1)

```
t-6  Stop staging and restoring state.json          [depends t-1, t-2]
     files:    internal/cmd/ship.go,
               internal/cmd/ship_recover.go,
               internal/cmd/ship_test.go,
               internal/cmd/ship_recover_test.go
     covers:   c-3
     desc:     ship drops the `git add .dross/state.json` fold (ship.go:227-232) and
               keeps the local Touch. runDrossRecovery's `git checkout <sha> -- .dross/`
               gains `:(exclude).dross/state.json` so restoring from a pre-untrack
               commit neither overwrites the live file nor re-adds it to the index.
     contract: - `dross ship` in a repo where .dross/state.json is gitignored exits
                 0. Left unchanged, `git add .dross/state.json` errors "paths are
                 ignored by one of your .gitignore files" and ship fails outright
                 — this is the sharpest single assertion in the phase.
               - `dross ship` still records "completed <id>" in the LOCAL
                 state.json history, and `git show HEAD:.dross/state.json` fails
                 (the file is not in the commit).
               - `dross ship recover --pre-merge-sha=<sha that still tracks
                 state.json>`: afterwards the live state.json still has its 9
                 history entries, `git ls-files .dross/state.json` is empty, and
                 the restore commit still contains .dross/phases/01-x/spec.toml.
                 Without the exclude pathspec the first two both fail.
               - The recovery delta gate still no-ops when origin already carries
                 the full .dross/ tree: TestShipRecoverIdempotentNoOp keeps
                 passing with no phantom commit, now that state.json can no longer
                 manufacture a delta.

t-7  Re-source status's stale-completion warning     [depends t-1]
     files:    internal/cmd/status.go,
               internal/cmd/status_test.go
     covers:   c-3
     desc:     staleCompletedState reads `git show origin/<main>:.dross/state.json`,
               which is now permanently unreadable. Re-source the "shipped but
               unmerged" signal from the phase's changes.json base/PR record (or drop
               the check), so the warning is not silently dead.
     contract: - On a phase branch whose local state records "completed <id>" and
                 whose PR is NOT merged, `dross status` still prints the
                 shipped-but-unmerged warning naming <id>. Today that path returns
                 ("", false) the moment the git show fails, so a
                 read-origin-state implementation fails this.
               - On a phase branch whose work IS on origin/<main>, status prints no
                 such warning — the false-positive side, which a naive "always
                 warn when history says completed" fix would break.
               - In a repo with no origin configured, `dross status` exits 0 and
                 prints no warning (silence on uncertainty is preserved).

t-8  Add doctor checks for state and version drift   [depends t-1, t-2, t-3, t-5]
     files:    internal/cmd/doctor.go,
               internal/cmd/doctor_test.go
     covers:   c-6, c-4, c-7
     desc:     Three new doctor sections: .dross/state.json still tracked; project.toml
               [project].version missing or diverged from state.json; stale milestone/*
               branches reported read-only via milestone.StaleBranches.
     contract: - In a repo where `git ls-files .dross/state.json` is non-empty,
                 doctor exits non-zero and its output contains the literal
                 `git rm --cached .dross/state.json` — the exact fix, not a
                 description of it.
               - project.toml [project].version empty → doctor exits non-zero and
                 the output contains `dross state set version`.
               - project.toml at 1.1.0.0 while state.json is at 1.2.3.0 → doctor
                 exits non-zero and prints BOTH values in one line, so the reader
                 knows which is stale without opening either file.
               - The two versions equal → that section prints a ✓ and contributes
                 nothing to the issue count.
               - A squash-merged milestone/v1.0 present locally → doctor names it
                 and prints `dross milestone prune`; the count of issues rises by
                 exactly one per stale branch, and an unmerged milestone branch
                 adds none.

t-9  Add `dross milestone prune`                     [depends t-5]
     files:    internal/cmd/milestone.go,
               internal/cmd/milestone_prune_test.go
     covers:   c-7
     desc:     New explicit subcommand: delete every stale milestone/* branch local and
               (when origin carries it) remote, via milestone.StaleBranches. Never
               touches a branch the detector did not mark stale.
     contract: - Fixture with a squash-merged milestone/v1.0 (local + pushed) and an
                 unmerged milestone/v1.2: after `dross milestone prune`,
                 `git rev-parse --verify refs/heads/milestone/v1.0` fails,
                 `git ls-remote --heads origin milestone/v1.0` is empty, and BOTH
                 refs for milestone/v1.2 still exist. Deleting the unmerged branch
                 is the failure mode this asserts against.
               - Prune while HEAD is checked out on a stale milestone branch exits
                 non-zero naming the branch, and deletes nothing — `git branch -D`
                 of the current branch is refused by git anyway, so this must be a
                 clean refusal, not a half-done sweep.
               - A stale local-only branch is deleted with no `push --delete`
                 attempted (asserted with no origin configured: the command still
                 exits 0).
               - Prune in a repo with no stale branches exits 0 and prints a
                 nothing-to-do line; running it twice is a clean no-op.
```

## Wave 3 (depends on wave 2)

```
t-10 Assert history survives every branch switch     [depends t-6, t-7]
     files:    internal/cmd/state_history_test.go
     covers:   c-3
     desc:     One table-driven test over the four branch-switching commands, each
               fixture seeded with N history entries, asserting the pre-switch entries
               are all still present afterwards in order. Carries any fix it surfaces.
     contract: - For each of `phase complete`, `phase complete --recover`,
                 `ship recover`, `milestone complete --finalize`: state.json seeded
                 with 12 distinct history actions; after the command the file
                 still contains all 12, in the original order, plus the command's
                 own entry appended last. A command that reloads state from a
                 checked-out tree instead of the live file drops the tail and
                 fails here.
               - Each case additionally asserts state.json's version field is
                 unchanged by the switch — the incident's second symptom (a stale
                 branch's version replacing the live one) has its own assertion.
               - `phase complete --recover` specifically: the entries survive even
                 though the recovery does `git reset --hard origin/<base>` and a
                 tree restore, which are the two operations that used to clobber.

t-11 Reproduce the incident end to end               [depends t-1, t-6]
     files:    internal/cmd/state_clobber_regression_test.go
     covers:   c-5
     desc:     TestStaleBranchCheckoutCannotClobberLiveState: build the repo through
               `dross init` (not by hand-writing .gitignore), accumulate N history
               entries, check out a long-lived branch created before untracking that
               still tracks a 2-entry state.json, assert the live state survives.
     contract: - The fixture builds .gitignore only via `dross init`, so the test
                 fails against pre-milestone code: there, the checkout succeeds and
                 .dross/state.json drops from 12 entries to 2. The assertion is
                 exactly "history length >= 12 AND the first seeded action is still
                 present", with the checkout allowed to have exited non-zero.
               - A sibling sub-test pins the opposite direction: with state.json
                 deliberately re-added to the index (`git add -f`), the same
                 checkout DOES clobber it — proving the assertion has teeth and is
                 not passing for an unrelated reason.
               - After the surviving checkout, `dross state get version` still
                 prints the live version, not the stale branch's — the version half
                 of the incident, asserted separately from the history half.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (checkout cannot silently replace live state) | t-1, t-2 |
| c-2 (release.yml resolves version from clean CI checkout) | t-3, t-4 |
| c-3 (branch-switching commands preserve history) | t-6, t-7, t-10 |
| c-4 (release version and dross version cannot diverge) | t-2, t-3, t-8 |
| c-5 (end-to-end incident regression) | t-11 |
| c-6 (doctor detects tracked state / stale version file) | t-8 |
| c-7 (stale milestone branch reported + removable) | t-5, t-8, t-9 |

7/7 criteria covered.

## Judgment calls

- **Made "fresh clone has no state.json" its own wave-1 task (t-2) rather than a
  footnote on t-1.** Untracking the file means every clone and every CI checkout
  starts without it, and `FindRoot` currently rejects that as not-a-dross-repo —
  so every dross command in a fresh clone breaks the moment t-1 lands. Rejected
  folding it into t-1: t-1 would then span the index, the scaffold and the root
  resolver, and its test contract would stop being a single assertion.
- **Seeded the materialized state.json's version from project.toml rather than
  from `state.New()`'s hard-coded "0.1.0.0".** project.toml is the tracked half
  of c-4's pair, so it is the only value present in a fresh clone; seeding from
  the constant would manufacture the exact divergence c-4 forbids.
- **Made `dross project set project.version` a refusal (t-3) rather than a second
  writer that syncs back.** Two writers that each mirror the other is the sync
  problem the locked history_durability decision rejects elsewhere; one writer
  plus a doctor check is the shape already used for local.toml.
- **Chose `git diff --quiet <main> <branch>` over `git cherry` / patch-id for the
  squash-merged detection (t-5).** A squash collapses N commits into one, so
  per-commit patch-ids do not match and `git cherry` misses exactly the case c-7
  singles out; an empty content diff against main is true for the squash case by
  construction. Rejected `git branch --merged` outright — c-7 names it as the
  thing that does not detect this.
- **Split the untracking fallout into t-6 (write/restore paths) and t-7 (status's
  read path) rather than one "make everything work again" task.** They fail
  differently: ship.go's `git add .dross/state.json` errors loudly on an ignored
  path, while status.go's `git show origin/main:.dross/state.json` fails silently
  into a warning that never fires again. One task would have let the loud failure
  stand in for both contracts and left the silent one untested.
- **Kept t-10 as a test-only task.** Its behaviour is delivered by t-6 and t-7,
  but c-3 asserts across four commands and only two of them are touched by those
  tasks — `phase complete` and `milestone complete --finalize` are claimed to be
  correct-by-construction once state.json is untracked, and a criterion resting on
  "correct by construction" needs the assertion written down.
- **Excluded state.json from the recovery restore with a pathspec rather than
  snapshot-and-restore around it (t-6).** The locked state_tracking decision
  rejects dross-side snapshot guards as the mechanism; the same reasoning applies
  inside the restore — `:(exclude)` means the file is never written, so there is
  no window in which a crash leaves the stale copy in place.
- **Did not touch assets/prompts/execute.md or quick.md.** Both already call
  `dross state set version` / `dross state bump internal`, which t-3 turns into
  dual writes — the prompts stay correct with no edit, and editing them would
  drag rule r-01's `make install` requirement into the phase for no behaviour
  change.
