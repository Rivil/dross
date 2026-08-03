# Plan draft — MVP lens

Phase milestone-stacking — 5 tasks across 4 waves

```
Wave 1
  t-1  Store the milestone base as a fact
       files:      internal/milestone/milestone.go
                   internal/cmd/milestone_base.go
                   internal/cmd/milestone_base_test.go
       desc:       Add `base` to milestone.Meta (toml `base,omitempty`). New milestone_base.go
                   holds the two readers every later task shares: milestoneRecordedBase(root,
                   version, mainBranch) — absent/empty base reads as mainBranch — and
                   branchMergedIntoMain(repoDir, branch, mainBranch) (best-effort `git fetch
                   origin`, `merge-base --is-ancestor origin/<branch> origin/<main>`, falling
                   back to local refs and reporting offline=true).
       covers:     c-2
       depends_on: []
       contract:   - a v1.1.toml with no `base` key resolves to "main", not "" — if the zero
                     value leaks to callers, TestMilestoneBaseAbsentReadsMain's branch-name
                     assertion fails
                   - branchMergedIntoMain reports merged=false while milestone/v1.1 carries a
                     commit absent from main and merged=true once that commit is on main; after
                     `git remote remove origin` it still answers off refs/heads and returns
                     offline=true — an origin-refs-only implementation fails the offline case
                   - a Milestone saved with Base set round-trips: the encoded toml contains a
                     `base =` line and Load returns it; a missing/misspelt toml tag fails it
       status:     pending

Wave 2 (depends t-1)
  t-2  Cut and target milestone branches from the recorded base
       files:      internal/cmd/milestone.go
                   internal/cmd/milestone_test.go
       desc:       ensureMilestoneBranch takes a resolved base branch instead of hardcoding main
                   and returns the one it used. `milestone create` resolves it — state
                   .current_milestone read before any state write, ignored when it names the
                   version being created or when milestone/<cur> is merged/absent, else
                   milestone/<cur> — honours a new `--base <branch>` flag, writes the result to
                   the new toml's base, and prints `cut <branch> from <base>`. `milestone
                   complete`'s open path sets opts.BaseBranch from the recorded base while that
                   branch is unmerged, main once it has merged.
       covers:     c-1, c-2, c-3
       depends_on: [t-1]
       contract:   - with current_milestone=v1.1 and an unmerged commit on milestone/v1.1,
                     `milestone create v1.2` leaves `rev-parse milestone/v1.2` equal to
                     `rev-parse milestone/v1.1`, and v1.2.toml records base = "milestone/v1.1"
                   - once milestone/v1.1 is an ancestor of main the same create points
                     milestone/v1.2 at main and records base = "main"; unset current_milestone
                     and a milestone/<cur> ref that does not exist do the same
                   - `--base milestone/v1.0` wins over both and is the value recorded; the
                     `cut … from <branch>` line names the branch actually cut from, so a
                     hardcoded "main" in the message fails the stacked case
                   - milestone complete for v1.2 with base=milestone/v1.1 unmerged: the
                     httptest forgejo POST /pulls body carries "base":"milestone/v1.1"; after
                     milestone/v1.1 lands on main the same POST carries "base":"main"
       status:     pending

Wave 3 (depends t-2)
  t-3  Refuse deleting a branch a child depends on
       files:      internal/cmd/milestone_deps.go
                   internal/cmd/milestone.go
                   internal/cmd/milestone_deps_test.go
       desc:       New milestone_deps.go: dependentMilestones(root, repoDir, branch, mainBranch)
                   scans .dross/milestones/*.toml for milestones whose recorded base is `branch`
                   and which are not themselves merged into main (branchMergedIntoMain). Wire it
                   into milestonePrune (before any delete, per candidate branch) and
                   milestoneFinalize (before the local/remote delete) as a refusal naming the
                   dependent version.
       covers:     c-4
       depends_on: [t-2]
       contract:   - prune refuses to delete milestone/v1.1 while v1.2.toml records
                     base="milestone/v1.1" and milestone/v1.2 holds a commit not on main; the
                     error text contains "v1.2", and refs/heads/milestone/v1.1 plus
                     origin/milestone/v1.1 both still exist afterwards
                   - once milestone/v1.2's work is on main the same prune deletes
                     milestone/v1.1 — a guard that keys off the record alone, ignoring the
                     child's merged state, wedges this case permanently
                   - `milestone complete v1.1 --finalize` (with origin/milestone/v1.1 already an
                     ancestor of origin/main, so the merge guard passes) hits the same refusal
                     naming v1.2 and leaves both refs in place
       status:     pending

  t-5  State the conditional cut rule in every doc
       files:      assets/prompts/milestone.md
                   README.md
                   docs/roadmap.md
                   internal/cmd/readme_doc_test.go
       desc:       Replace milestone.md:14's "cuts + pushes the milestone/<version> integration
                   branch from main" with the conditional rule plus `--base`; extend README's
                   `dross milestone {…}` row (line ~190) with the conditional cut, the recorded
                   base and `--base`; add the v1.2 branch-model entry to docs/roadmap.md and fix
                   any surviving unconditional claim there. Rule r-01: `make install` after the
                   prompt edit.
       covers:     c-5
       depends_on: [t-2]
       contract:   - TestMilestoneCutDocsStateConditionalRule reads README.md, docs/roadmap.md
                     and assets/prompts/milestone.md from repoRootFromTest and fails if the
                     phrase "integration branch from main" survives in any of them
                   - the same test requires each of the three files to name both arms — the
                     current milestone's branch when unmerged, main otherwise — so deleting the
                     old claim without stating the new rule still fails
                   - the README `dross milestone` row must mention `--base`; dropping the flag
                     from the docs while it exists in the CLI fails the row grep
       status:     pending

Wave 4 (depends t-3)
  t-4  Extend the delete guard with an open-PR check
       files:      internal/ship/basepr.go
                   internal/ship/basepr_test.go
                   internal/cmd/milestone_deps.go
                   internal/cmd/milestone_deps_test.go
       desc:       ship.OpenPRsTargeting(opts, base) lists open PRs whose base is `base`,
                   mirroring PRMerged's shape: github via `gh pr list --base <b> --state open
                   --json number,url`, other providers return ErrPRListUnsupported; exported
                   seam OpenPRsTargetingFunc for cmd tests. dependentMilestones calls it when
                   [remote].provider/url are set, adds the returned PRs to the refusal, and on
                   unsupported/failed lookup prints a "provider check skipped" line and decides
                   on the toml scan alone.
       covers:     c-6
       depends_on: [t-3]
       contract:   - with OpenPRsTargetingFunc stubbed to return PR #77 based on
                     milestone/v1.1, prune refuses and the error names "#77" even though no
                     milestone toml records that base
                   - with the stub returning ErrPRListUnsupported (and again with an HTTP
                     error), the command output contains a "provider check skipped" line and the
                     toml scan still decides — swallowing the error silently fails the output
                     assertion, and aborting on it fails the "still deletes an undepended
                     branch" case
                   - with no [remote].provider configured the stub is never called and the same
                     skip line is printed
                   - ship.OpenPRsTargeting on the github arm invokes `gh pr list` with `--base
                     <branch>` and `--state open` (asserted through the ghCommand seam) and
                     parses the numbers out of the JSON; provider "forgejo" returns
                     ErrPRListUnsupported
       status:     pending
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2 |
| c-2 | t-1, t-2 |
| c-3 | t-2 |
| c-4 | t-3 |
| c-5 | t-5 |
| c-6 | t-4 |

6 of 6 criteria covered.

## Judgment calls

- **Merged `milestone create` and `milestone complete` into one task (t-2)** rather than one task per criterion: both live in `internal/cmd/milestone.go`, both are the write/read halves of the same recorded fact, and splitting them puts two tasks in the same wave editing the same file. Two files, one layer — under the split threshold.
- **Split the shared readers out into t-1** instead of folding them into t-2: t-2, t-3 and t-4 all consume them, and folding them in would put t-2 at five files. This is the only reason wave 1 exists.
- **`base` goes on `milestone.Meta` (`[milestone] base = …`)**, not a new `[branch]` table — Meta already carries version/status/started/shipped, and `milestone get/set milestone.base` then works with no new dotted-path plumbing. Rejected: a separate table, which would need its own reader/writer arms in `readMilestoneDotted`/`writeMilestoneDotted` for no gain.
- **The merged test lives in `internal/cmd` (`milestone_base.go`), not `internal/milestone`** — it needs the package's git helpers (`gitNoOut`, `gitTrim`) and `internal/milestone` is deliberately pure toml I/O. Rejected: exporting a git shim from the milestone package.
- **t-3 is wave 3, depending on t-2**, because the scan reads the base record t-2 is the only writer of; before t-2 there is nothing on disk for it to find. It does not additionally need t-5.
- **c-6 implements one provider arm (github) plus an explicit unsupported error**, copying `ship.PRMerged`'s precedent, instead of a REST implementation per backend. The spec's own wording ("when a provider is configured and reachable … when the call fails, say the check was skipped") makes the skip path first-class, so unimplemented backends are already a supported outcome, and github is this repo's provider.
- **No migration task.** Locked `absent_base_reads_main` makes a base-less toml read as `main`, which is today's behaviour, so the back-compat cost is one branch in t-1's reader.
- **The refusal is a hard error, not a `--force`-able warning.** Nothing in c-4/c-6 asks for an override, and the deferred auto-retarget entry is the named escape hatch; adding a bypass flag would be speculative structure.
