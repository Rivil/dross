# risk lens — milestone-stacking

Failure modes drive the graph. Every task owns one class of breakage: a wrong
cut point, a lost record, a PR pointed at a doomed branch, a delete that orphans
a child, an unreachable forge, a doc that still lies. The three engine pieces
(ancestry, provider query, schema) are pure and land first so the command
wiring above them has nothing left to guess at.

```
Phase milestone-stacking — 7 tasks across 3 waves

Wave 1
  t-1  Record a base on the milestone schema
       files:    internal/milestone/milestone.go, internal/milestone/milestone_test.go
       covers:   c-2
       desc:     Add Meta.Base (toml+json tags), RecordedBase() defaulting to "main"
                 when unset, and LoadAll(root) returning every milestone keyed by
                 version — erroring (never skipping) on an unreadable toml.
       contract: a toml carrying base = "milestone/v1.1" loads with Base set and
                 re-saves it byte-equivalent — if Save drops the field the
                 round-trip test fails; a milestone with no base line makes
                 RecordedBase() return "main" (absent_base_reads_main), so a
                 return of "" fails the default test; LoadAll over a directory
                 holding one syntactically broken toml returns an error naming
                 that file rather than a short map — a silent skip fails the
                 fail-closed test; TestJSONTagParity fails if Base has a toml
                 tag and no json tag.

  t-2  Resolve the cut point from git ancestry
       files:    internal/cmd/milestone_base.go, internal/cmd/milestone_base_test.go
       covers:   c-1
       desc:     resolveMilestoneCutPoint(repoDir, mainBranch, currentMilestone)
                 -> (branch, why string, offline bool, err). Best-effort fetch,
                 then merge-base --is-ancestor origin/milestone/<cur> origin/<main>;
                 falls back to local refs when the fetch or an origin ref is
                 missing and reports offline. No command wiring, no writes.
       contract: fixture where origin/milestone/v1.1 is NOT an ancestor of
                 origin/main resolves to "milestone/v1.1"; after main is
                 fast-forwarded over it the same fixture resolves to "main" — a
                 resolver that reads milestone.status instead of ancestry fails
                 the merged-but-still-active case; an origin remote pointed at a
                 nonexistent path still resolves from local refs with offline=true
                 — if the fetch failure propagates as an error the offline test
                 fails; currentMilestone == "" resolves to "main" even with an
                 unmerged milestone/* ref present (stacking_parent locked), so a
                 ref-scanning implementation fails that case; a currentMilestone
                 whose milestone/<cur> ref does not exist at all resolves to "main".

  t-3  Ask the forge for PRs based on a branch
       files:    internal/ship/basepr.go, internal/ship/basepr_test.go
       covers:   c-6
       desc:     OpenPRsTargeting(opts OpenOpts, base string) ([]PRRef, error)
                 plus the OpenPRsTargetingFunc seam, mirroring PRMerged: GitHub via
                 the ghCommand stub (`gh pr list --base <b> --state open --json
                 number,title,url,headRefName`), ErrBasePRQueryUnsupported for
                 forgejo/gitea/gitlab/bitbucket, unknown provider errors.
       contract: a gh stub emitting one PR with headRefName milestone/v1.3 returns
                 that number and URL; a stub exiting non-zero returns an error, not
                 an empty slice — collapsing "could not ask" into "no PRs" fails the
                 unreachable-forge test and would silently unblock a delete;
                 provider "gitlab" returns ErrBasePRQueryUnsupported without
                 shelling out (fails if any exec happens); malformed JSON on stdout
                 returns a parse error rather than zero PRs.

Wave 2 (depends t-1, t-2, t-3)
  t-4  Cut milestone create from the resolved base
       files:    internal/cmd/milestone.go
       covers:   c-1, c-2, c-5
       desc:     Replace the unconditional main cut: ensureMilestoneBranch takes the
                 resolved cut point, add --base <branch> (validated against local
                 refs), record the branch used into <version>.toml via Meta.Base
                 before the push, and print which branch it cut from, why, and the
                 offline caveat when the ancestry answer came from local refs.
       depends:  t-1, t-2
       contract: `milestone create v1.3` in a fixture whose current milestone
                 branch is unmerged cuts milestone/v1.3 from milestone/v1.2 AND
                 writes base = "milestone/v1.2" into v1.3.toml — recording without
                 cutting (or vice versa) fails; the same fixture with v1.2 merged
                 into main cuts from main and records "main"; `--base nope` fails
                 with "no such local branch" leaving neither a toml nor a branch
                 behind — a partial write fails the atomicity test; `--base
                 milestone/v1.0` overrides the resolver and is recorded verbatim;
                 re-create after the toml was deleted while the branch survives
                 still records a base (the prompt's replace flow) rather than
                 leaving it empty; the cut line names the source branch and the
                 rule — reverting to a hardcoded "from main" string fails the
                 narration assertion; a non-git dir still creates the toml and
                 records nothing rather than erroring.

  t-5  Target complete's PR at the recorded parent
       files:    internal/cmd/milestone.go
       covers:   c-3
       desc:     milestoneComplete reads Meta.Base and opens against it while
                 origin/<parent> is unmerged, against main once it has merged or
                 its origin ref is gone; milestoneFinalize's ancestry guard checks
                 the same resolved target instead of hardcoded origin/<main>.
       depends:  t-1
       contract: with v1.3.toml recording base milestone/v1.2 and origin/milestone/v1.2
                 unmerged, `milestone complete v1.3` opens with --base
                 milestone/v1.2 (asserted on the captured gh args) — a PR opened
                 against main here would carry v1.2's commits as its own diff and
                 fails the stacked-target test; once origin/main contains v1.2 the
                 same command targets main; with origin/milestone/v1.2 deleted
                 outright it targets main instead of erroring on a dead ref;
                 the already-exists idempotent path reports the resolved base, not
                 main; `complete v1.3 --finalize` after v1.3 merged into
                 milestone/v1.2 (but not yet main) passes its ancestry guard — a
                 guard still fixed on origin/main refuses forever and fails that
                 test; a milestone recording its own branch as base is refused
                 rather than opening a self-targeted PR.

  t-6  Refuse deletes that orphan a stacked child
       files:    internal/cmd/milestone_dependents.go,
                 internal/cmd/milestone_dependents_test.go, internal/cmd/milestone.go
       covers:   c-4, c-6
       desc:     dependentsOf(root, repoDir, branch) scans .dross/milestones/*.toml
                 (via LoadAll) for unmerged milestones recording that branch as base,
                 then — when [remote].provider is set — adds open PRs from
                 OpenPRsTargetingFunc, returning a skip reason instead when no
                 provider is configured or the call fails. milestonePrune and
                 milestoneFinalize consult it and refuse before any branch delete.
       depends:  t-1, t-3
       contract: with v1.3.toml recording base milestone/v1.2 and v1.3 unmerged,
                 both `milestone prune` and `milestone complete v1.2 --finalize`
                 exit non-zero naming v1.3 and leave refs/heads/milestone/v1.2 and
                 the origin ref intact — a delete that still happens fails the
                 branch-survives assertion; once v1.3's own branch is merged into
                 main the refusal lifts and prune deletes; a milestone recording
                 base "main" never blocks anything; a milestone naming itself as
                 base does not self-block; an unreadable milestone toml makes the
                 gate refuse (fail closed) rather than deleting on a short scan;
                 with no [remote].provider the refusal path still runs the toml
                 scan and prints a "provider check skipped" line — a silent skip
                 fails the announce test; with a gh stub reporting an open PR
                 based on milestone/v1.2 and no recording toml at all, prune
                 refuses naming that PR number; a gh stub that errors makes the
                 command announce the skipped check and continue on the toml
                 verdict rather than abort.

Wave 3 (depends t-4)
  t-7  State the conditional cut rule in prompt and docs
       files:    assets/prompts/milestone.md, README.md, docs/roadmap.md,
                 internal/cmd/milestone_doc_test.go
       covers:   c-5
       desc:     Rewrite the milestone.md §0.3 branch sentence as the conditional
                 rule, document --base, and update the README milestone-branch row
                 and roadmap milestone-branch-model paragraph; add a doc test that
                 pins the claim.
       depends:  t-4
       contract: the doc test fails if assets/prompts/milestone.md still contains
                 the unconditional "integration branch from main" claim, or if any
                 of the three files lacks the stacked-cut sentence; it also fails
                 if the prompt does not name `dross milestone create --base` — so a
                 future edit that drops the flag from the narration is caught.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-2, t-4 |
| c-2 | t-1, t-4 |
| c-3 | t-5 |
| c-4 | t-6 |
| c-5 | t-4, t-7 |
| c-6 | t-3, t-6 |

6/6 criteria covered.

## Judgment calls

- **Three pure engines in wave 1, wiring in wave 2** — rejected one task per
  command (create / complete / prune). Ancestry, the forge query and the schema
  are exactly where the edge cases live (offline, dead ref, unreadable toml, gh
  absent); isolating them means each failure mode gets a table-driven test
  without a cobra fixture around it.
- **t-5 fixes finalize's ancestry guard too** — it currently hardcodes
  `origin/<main>`, so a child merged into its parent could never be finalized.
  Strictly that is c-3's stacking coherence rather than its literal text; leaving
  it out ships a stacking model whose second half deadlocks. Called out here
  because it is the one place I read past the criterion's wording.
- **The provider query is a `ship` seam, not `forge`** — rejected extending
  `forge.BoardClient`, which is issue-board shaped and returns
  `ErrNotImplemented` for GitHub, the only provider that can answer this. `ship`
  already owns PR-shaped provider dispatch with the `ghCommand` stub and the
  `PRMergedFunc` seam pattern; `basepr.go` is a direct copy of that shape.
- **The dependent gate fails closed on an unreadable toml** — rejected
  skip-and-continue. A parse error on `.dross/milestones/*.toml` is
  indistinguishable from "no dependents", and the consequence of guessing wrong
  is an irreversible remote branch delete under someone's open PR. Hence
  `LoadAll` errors instead of returning a partial map (t-1).
- **The forge skip is announced but non-fatal** — the toml scan is the primary
  gate (locked dependent_detection), so a dead network must not block a prune.
  The counterpart in t-3 is that a failed `gh` call returns an error rather than
  an empty list, so "no PRs" and "could not ask" stay distinguishable at the
  call site that decides the wording.
- **`dross doctor` left alone** — it reports stale milestone branches and will
  now list branches prune refuses to delete. Out of the criteria, and folding a
  gate into a pure diagnostic contradicts the prune_surface locked decision from
  the earlier phase. Worth a deferred entry, not a task.
- **No separate migration task** — `absent_base_reads_main` is locked, so v1.2's
  own toml and every earlier one read as `main` through `RecordedBase()`. A
  backfill pass would be write traffic with no behaviour change.
