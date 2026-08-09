# Panel draft — MVP lens

Phase state-json-branch-safety — 6 tasks across 3 waves

```
Wave 1
  t-1  Untrack and gitignore .dross/state.json
       files:    .gitignore,
                 internal/cmd/gitignore.go,
                 internal/cmd/init.go,
                 internal/cmd/state_untrack_test.go
       covers:   c-1, c-3, c-5
       description:
                 `git rm --cached .dross/state.json` + a `.dross/state.json` line in
                 .gitignore (comment mirroring the local.toml block). New
                 internal/cmd/gitignore.go adds ensureDrossStateIgnored /
                 hasDrossStateIgnoreLine, mirroring gitattributes.go, called from
                 init.go beside ensureDrossGitattributes. Same task carries the
                 end-to-end regression test.
       contract: - checkout regression: repo with live .dross/state.json holding 40
                   history entries, a long-lived branch whose tree carries a 2-entry
                   copy; `git checkout <that branch>` leaves the working-tree
                   state.json byte-identical with all 40 entries. Fails on
                   pre-milestone code, where the checkout overwrites it with the
                   2-entry copy.
                 - `git ls-files .dross/state.json` is empty after the untrack, and
                   `git status --porcelain` shows no untracked `.dross/state.json`
                   (proves the ignore line matches the path git actually reports).
                 - ensureDrossStateIgnored is idempotent: second call on a .gitignore
                   that already carries the line appends nothing (byte-compare).
                 - hasDrossStateIgnoreLine returns false for a .gitignore containing
                   only `.dross/local.toml`.

  t-2  Keep live state out of the .dross restore
       files:    internal/cmd/ship_recover.go,
                 internal/cmd/ship_recover_test.go
       covers:   c-3
       description:
                 runDrossRecovery's `git checkout <sha> -- .dross/` resurrects a
                 tracked state.json from the pre-merge SHA and re-stages it. Add a
                 `:(exclude).dross/state.json` pathspec to that checkout and to the
                 `git add .dross/` calls, and correct the now-false comment claiming
                 the same add stages the state touch.
       contract: - runDrossRecovery against a pre-merge SHA whose tree carries a
                   2-entry .dross/state.json leaves the live file's history at its
                   pre-call N entries, not 2 — and `git ls-files .dross/state.json`
                   stays empty, so the restore does not re-track it.
                 - the delta gate still no-ops: a base already carrying the full
                   .dross/ tree produces zero commits (commit count unchanged),
                   proving the exclude pathspec did not manufacture a delta.
                 - the recovery commit's `git show --stat` names no
                   .dross/state.json path.

  t-3  Make project.toml the single version home
       files:    internal/cmd/state.go,
                 internal/cmd/project.go,
                 .dross/project.toml,
                 .github/workflows/release.yml,
                 internal/cmd/version_sync_test.go
       covers:   c-2, c-4
       description:
                 One writer (writeVersionBothPlaces) behind `state set version`,
                 `state bump internal` and `project set version` — each writes
                 state.json's version and project.toml's [project].version in the
                 same call. release.yml's "Determine release tag" step reads
                 [project].version out of .dross/project.toml with grep/sed instead
                 of `jq` over state.json. Correct the dead 0.2.0.0 in
                 .dross/project.toml to the live value.
       contract: - `dross state bump internal` from 1.2.0.0 leaves BOTH
                   .dross/state.json version and project.toml [project].version at
                   1.2.0.1; removing either write fails the parity assertion.
                 - `dross project set version 1.2.1.0` leaves state.json's version at
                   1.2.1.0 too (the reverse direction of the same parity).
                 - the "Determine release tag" step body contains neither `jq` nor
                   `.dross/state.json`; running its extraction expression against a
                   fixture project.toml containing `  version = "1.2.3.4"` prints
                   1.2.3.4, and against one with no version field exits non-zero.
                 - .dross/project.toml's [project].version equals .dross/state.json's
                   version in this repo (guards the dead-value regression).

  t-4  Detect and prune stale milestone branches
       files:    internal/cmd/milestone_prune.go,
                 internal/cmd/milestone.go,
                 internal/cmd/milestone_prune_test.go
       covers:   c-7
       description:
                 staleMilestoneBranches(repoDir, mainBranch) lists milestone/* refs
                 (local + origin) whose work is already on main, using the
                 squash-aware check — synthesize the branch as one commit off its
                 merge-base (commit-tree) and test that patch-id against main with
                 `git cherry`, since per-commit ancestry misses a squash. New
                 `dross milestone prune [version]` deletes the stale ref local +
                 remote; refuses on anything not reported stale.
       contract: - a branch whose 3 commits were squash-merged into main as one
                   commit (per-commit patch-ids therefore absent from main, and
                   `git branch --merged` does not list it) IS reported stale.
                 - a branch with one commit not present on main in any form is NOT
                   reported stale, and `dross milestone prune` on it exits non-zero
                   leaving both refs/heads/milestone/<v> and the origin ref intact.
                 - `dross milestone prune <v>` on a stale branch removes
                   refs/heads/milestone/<v> AND the origin ref (`git ls-remote
                   --heads origin milestone/<v>` empty afterwards).
                 - prune on a version with no milestone branch at all exits 0 with a
                   "nothing to prune" message, not a git error.

Wave 2 (depends t-3)
  t-5  Recreate state.json when a clone lacks it
       files:    internal/cmd/root.go,
                 internal/cmd/state.go,
                 internal/cmd/state_bootstrap_test.go
       covers:   c-1
       description:
                 Untracked state means a fresh clone has none, so every command
                 would report "not a dross repo". Drop state.json from
                 RequiredRootFiles and have loadState create it from state.New()
                 when absent, seeding Version from project.toml's [project].version
                 (t-3's home) and leaving history empty per history_durability.
       contract: - with .dross/state.json deleted and project.toml holding
                   version = "1.2.0.0", `dross state show` exits 0 and prints
                   version 1.2.0.0 with an empty history, instead of erroring
                   "not a dross repo".
                 - the recreated file exists on disk afterwards (a second `state
                   get version` reads it without recreating).
                 - a .dross/ holding project.toml but no state.json resolves through
                   FindRoot with no IncompleteRootError; one missing project.toml
                   still errors as before.
                 - a present-but-corrupt state.json still fails loudly (unmarshal
                   error), never silently replaced by a fresh one.

Wave 3 (depends t-1, t-3, t-4, t-5)
  t-6  Add doctor checks for state and version drift
       files:    internal/cmd/doctor.go,
                 internal/cmd/doctor_test.go
       covers:   c-6, c-7
       description:
                 Three doctor additions: state.json still in the index (issue, names
                 `git rm --cached .dross/state.json` + the .gitignore line);
                 project.toml [project].version missing or != state.json's version
                 (issue, names both values and `dross state set version`); stale
                 milestone/* branches from t-4's detector (advisory warning, names
                 `dross milestone prune <v>`). Also drop state.json from the
                 foundational-files trio, which t-5 made optional.
       contract: - doctor in a repo where `git ls-files .dross/state.json` is
                   non-empty prints the `git rm --cached .dross/state.json` fix and
                   raises the issue count by 1; in an untracked repo that section
                   prints ✓ and adds nothing.
                 - doctor with project.toml version 1.1.0.0 and state.json version
                   1.2.0.0 reports drift naming both strings and raises the issue
                   count; equal versions print ✓.
                 - doctor with a missing [project].version reports it as an issue
                   distinct from the drift message.
                 - doctor lists a squash-merged milestone/v1.1 as stale and prints
                   `dross milestone prune v1.1`, and doctor's exit code is unchanged
                   by that section alone (advisory, per prune_surface).
                 - a repo with no .dross/state.json no longer reports it as a missing
                   foundational file.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (stale branch cannot silently replace live state) | t-1 (untrack + ignore), t-5 (fresh clone still works without the tracked file) |
| c-2 (release.yml resolves version, no dross binary) | t-3 |
| c-3 (branch-switching commands preserve history) | t-1 (phase complete / milestone complete / finalize checkouts — nothing to clobber once untracked), t-2 (`phase complete --recover` and `ship recover`, whose path-restore would otherwise resurrect a stale copy) |
| c-4 (release version and dross version cannot diverge) | t-3 (one writer), t-6 (stale copy is a detectable error) |
| c-5 (end-to-end regression test, fails pre-milestone) | t-1 |
| c-6 (doctor: tracked state, missing/stale version file) | t-6 |
| c-7 (stale milestone/* reported, squash case included) | t-4 (detector + prune), t-6 (report surface) |

## Judgment calls

- Folded the c-5 regression test into t-1 rather than giving it its own task: it exercises exactly t-1's mechanism, and a separate task would only add a wave-2 hop for one test file.
- Added t-5 (recreate state.json when absent) even though no criterion names it. Untracking makes a fresh clone state-less, and without this every command in a fresh clone errors "not a dross repo" — c-1's locked mechanism is not shippable otherwise. Traced to c-1 rather than invented as new scope.
- Kept t-2 separate from t-1. Same criterion (c-3), but the recovery-path bug is a distinct mechanism — `git checkout <sha> -- .dross/` re-materialises AND re-stages a file the untracking just removed — and merging would give t-1 six files across two concerns.
- Chose a single doctor task (t-6) at the end over two parallel ones. c-6 and c-7's doctor surfaces are different checks but the same RunE, and two wave-2 tasks editing doctor.go concurrently is a merge hazard, not parallelism.
- Split the c-7 detector+prune (t-4) away from its doctor surface (t-6) so t-4 drops to wave 1 — it depends on nothing, and only its *reporting* needs to be sequenced.
- Doctor's stale-milestone section is a warning (no exit-code change), while tracked-state and version-drift are issues. prune_surface locks doctor as read-only for pruning; deletion staying explicit means the report must not gate CI. The version/tracking checks have an unambiguous local fix, so they count.
- Made the version writer bidirectional (`project set version` also writes state.json) rather than only state→project. c-4 says one bump writes both; leaving one direction one-way would leave a second way to create the drift the same phase adds a detector for.
- Rejected a new version.json / Makefile version var for c-2: version_home is locked to project.toml.
- Rejected any dross-side snapshot/restore guard around checkouts for c-1: state_tracking is locked, and a guard would not cover a manual `git checkout`.
- Used commit-tree + `git cherry` for squash detection in t-4 rather than a tree diff against main. A tree comparison is wrong the moment main carries unrelated later work; synthesizing the branch as one squash commit and testing its patch-id is the case `git branch --merged` misses, which c-7 names explicitly.
