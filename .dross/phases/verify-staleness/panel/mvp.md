# verify-staleness — MVP lens draft

Phase verify-staleness — 4 tasks across 2 waves

```
Wave 1
  t-1  Add verify tree fingerprint and freshness check
       files:    internal/treefp/treefp.go,
                 internal/verify/verify.go,
                 internal/verify/freshness.go (new),
                 internal/verify/freshness_test.go (new),
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     treefp gains a WorkingTree variant that also drops literal top-level
                 paths (WorkingTree itself is unchanged). verify.Tests gains Commit/Tree
                 (json, omitempty) and VerifyMeta gains Commit/Tree (toml
                 "commit"/"tree", omitempty). Skeleton copies them across the way it
                 copies MeasuredOn. freshness.go adds CaptureBaseline(repoDir) (HEAD sha
                 plus a fingerprint of the working tree with .dross/ and ARCHITECTURE.md
                 taken out) and Freshness(repoDir, VerifyMeta) -> {Unknown bool; Changed
                 []string}, which uses treefp.Diff to name changed files. Four not_paths
                 rows: verify.Tests.{Commit,Tree}, verify.VerifyMeta.{Commit,Tree}.
       covers:   c-1, c-3, c-5
       contract: - after CaptureBaseline, an edit to ARCHITECTURE.md and to a .dross/ file
                   -> Freshness.Changed is empty (the c-3 exemption in the fingerprint itself)
                 - an uncommitted edit, a new untracked non-ignored file and a deleted
                   tracked file are each named in Changed. A new .gitignored file is
                   not named (fingerprint_scope)
                 - CaptureBaseline on a dirty tree returns Commit == `git rev-parse HEAD`
                   and a Tree != the clean-HEAD fingerprint (uncommitted changes included)
                 - VerifyMeta with empty Tree -> Freshness returns Unknown=true and no
                   error (c-5 legacy path)
                 - treefp.WorkingTree still changes when ARCHITECTURE.md changes, so the
                   commit gate and ship.md §3.5's `dross test` keep seeing the doc edit
                 - Skeleton(t) with t.Commit/t.Tree set writes them under [verify]. A
                   VerifyMeta with neither round-trips through Save with no commit/tree
                   keys
       depends:  —
       status:   pending

  t-4  Re-run ship after fixes and before merge
       files:    assets/prompts/ship.md,
                 internal/cmd/ship_prompt_test.go,
                 README.md
       desc:     §5 On-failure step 3: after committing the fix, re-run
                 `dross ship <phase-id>`. It re-checks the verify gate and then pushes. A
                 stale-verdict refusal means: re-verify (/dross-verify <phase-id>), commit
                 the verdict, then re-run ship. §6: before the merge call, re-run
                 `dross ship <phase-id>`. A refusal is a stop and never a merge. Keep §6
                 to one AskUserQuestion. README rows for `dross verify`,
                 `dross verify results`, `dross status` and `dross ship` describe the
                 recorded tree and the stale gate. Refresh the ship.md token row if the
                 edit moves it by more than 50 bytes.
       covers:   c-7
       contract: - new TestShipPromptReShipsAfterFixBeforeMerge fails if §5 On-failure has
                   no `dross ship <phase-id>` re-run AFTER its "commit the fix" step, or if
                   §6 has no `dross ship <phase-id>` re-run BEFORE the first provider merge
                   call (`gh pr merge`)
                 - the same test fails if neither §5 nor §6 names re-verifying on a stale
                   refusal
                 - the existing TestShipPromptReRunIsTheRetry ("re-run dross ship",
                   "dross ship --force", "git pull --rebase origin phase/<id>") and
                   TestShipPromptDecisionTurnsSeparate (one §6 AskUserQuestion) still pass
       depends:  —
       status:   pending

Wave 2 (depends t-1)
  t-2  Record the measured baseline on every verify run
       files:    internal/cmd/verify.go,
                 internal/localstore/store.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt,
                 internal/cmd/verify_baseline_test.go (new)
       desc:     Attached and --skip-mutation RunE: CaptureBaseline before RunScoped,
                 then set t.Commit/t.Tree before finishVerify. dispatchDetached:
                 CaptureBaseline before detachSync and store Commit/Tree on
                 DetachedRun. collectDetachedFrom: set t.Commit/t.Tree from the record,
                 never from a collect-time capture. When the current fingerprint
                 differs, print every file changed since dispatch (treefp.Diff). A
                 record with no tree writes no tree, so it reads as "unknown". Two
                 not_paths rows: localstore.DetachedRun.{Commit,Tree}.
       covers:   c-1, c-4
       contract: - `dross verify x --skip-mutation` in a git fixture with an uncommitted
                   edit writes verify.toml with [verify].commit == HEAD and tree ==
                   CaptureBaseline taken just before. Dropping the stamp from RunE fails
                   TestVerifySkipMutationStampsMeasuredTree
                 - dispatchDetached (detachSync/detachSpawn/remoteProbeFn stubbed) leaves
                   a local.toml DetachedRun whose Commit/Tree equal the pre-dispatch
                   baseline (TestDispatchStoresBaselineInRunRecord)
                 - edit a file after dispatch, then collect: verify.toml's [verify].tree
                   is the RECORD's tree, not the collect-time fingerprint, and stdout
                   names the edited file (TestCollectKeepsDispatchTreeAndNamesDrift)
       depends:  t-1
       status:   pending

  t-3  Gate ship and status on stale verdicts
       files:    internal/cmd/ship.go,
                 internal/cmd/ship_test.go,
                 internal/cmd/status.go,
                 internal/cmd/status_test.go
       desc:     ship: after the on-branch check (and before --print-body/--no-push), a
                 pass verdict without --force-unverified runs verify.Freshness. If any
                 files changed, refuse, naming each file, `/dross-verify <id>` and
                 --force-unverified. Unknown prints a "freshness unknown" warning on
                 stderr and proceeds. --force-unverified skips the check. status: when
                 HEAD is phase/<current>, the verdict is pass and Freshness reports N > 0
                 changed, print a `stale:` line with N. Any Freshness error stays silent
                 (status is a hook target).
       covers:   c-2, c-3, c-5, c-6
       contract: - shipFixture + a recorded tree, then commit an edit to src/tag.ts, add
                   src/new.ts and delete a tracked file: `ship --no-push` errors and the
                   error names all three paths, "/dross-verify x" and
                   "--force-unverified" (TestShipRefusesStaleVerdictNamingFiles)
                 - the same stale fixture with --force-unverified -> `ship --no-push`
                   succeeds
                 - commit edits to ARCHITECTURE.md and a .dross/phases/x file after the
                   recorded tree -> `ship --no-push` succeeds
                   (TestShipStaysFreshAcrossExemptChanges, c-3)
                 - the stock shipFixture verify.toml (no tree) -> `ship --no-push`
                   succeeds and captured stderr contains "freshness unknown" (c-5)
                 - status on phase/x with a pass verdict and 2 changed files prints
                   "stale" and "2 file"; with a fresh verdict, no stale line
                   (TestStatusMarksStaleVerdictWithCount, c-6)
       depends:  t-1
       status:   pending
```

## Coverage

| Criterion | Tasks | How |
|---|---|---|
| c-1 | t-1, t-2 | t-1 defines the fingerprint (whole tree, untracked included, exemptions out) and the [verify].commit/tree schema. t-2 stamps it on the attached, --skip-mutation and detached paths. |
| c-2 | t-3 | ship refuses a stale pass, names the added, changed and removed files and the re-verify command. --force-unverified overrides. |
| c-3 | t-1, t-3 | t-1 keeps .dross/** and ARCHITECTURE.md out of the fingerprint. t-3 proves ship proceeds over an exempt-only commit, which covers §3.5's landmark merge. |
| c-4 | t-2 | dispatch captures and stores the tree. Collect writes the record's tree and names drift. |
| c-5 | t-1, t-3 | t-1: no tree reads as Unknown. t-3: ship warns and proceeds. |
| c-6 | t-3 | status `stale:` line with the changed-file count. |
| c-7 | t-4 | ship.md §5 and §6 re-ship ordering, pinned by a prompt test. |

7/7 criteria covered.

## Judgment calls

- **Fingerprint.** Chose the treefp scratch-index `write-tree` id plus `treefp.Diff`. Rejected a per-file hash list in verify.toml: it bloats the readable summary and would be a second fingerprint implementation beside the one the commit gate already trusts.
- **ARCHITECTURE.md exemption.** Chose a new treefp variant. Rejected changing `WorkingTree`: the commit gate and ship.md §3.5's pre-commit `dross test` depend on it seeing the doc edit.
- **Baseline plumbing.** Chose to carry the baseline on `verify.Tests` and let Skeleton copy it, as MeasuredOn already does. Rejected a new `finishVerify` parameter: it would touch 8 call sites in two more test files and push t-2 past 5 files. tests.json gets the record as a side effect.
- **Ship check placement.** Chose after the on-branch check. Rejected placing it beside the verdict switch: off the phase branch, the working tree is not the phase's, so the gate would refuse "stale" instead of "wrong branch".
- **Status scope.** Chose to mark staleness only for the current phase, and only while HEAD is `phase/<id>`. Rejected scanning every verified phase: their branches are not checked out, so each would read as permanently stale. Rejected marking while on main for the same reason (the SessionStart hook runs from main).
- **Pruned recorded tree** (unreachable loose object, gc'd after about 2 weeks). Chose: Freshness errors, ship refuses, status stays silent. Rejected warn-and-proceed: that would let an old verdict ship unverified without anyone noticing.
- **Legacy warning channel.** Chose stderr, so `--auto --json` still gets the c-5 warning while stdout keeps its single JSON object. Rejected `narrate`, which `--json` silences.
- **Merged ship gate and status mark into one task** (one API, one package, 4 files). Rejected separate tasks: status is about 20 lines on the same Freshness call.
- **No flag or help-text change.** `--force-unverified` keeps its current help, so the CLI goldens (cli_surface/*.txt, cli_tree.txt) don't move. Rejected rewording it to mention staleness.
- **README folded into t-4.** The README sync rule is a repo convention, not a criterion, so it rides the docs task instead of becoming its own task.
- **Collect-time drift is a printed notice, not a refusal.** c-4 asks only that it be named, and t-3's ship gate already refuses the drifted tree.
- **suggestNext unchanged.** It still says /dross-ship for a stale pass. c-6 asks only that the phase be marked, and ship's refusal names /dross-verify. Rejected rerouting the next step.
- **verify.md not edited.** /dross-verify edits the skeleton in place, so [verify].commit/tree survive. A hand-rewrite that dropped them degrades to "unknown" (warn), which is the risk lens's call to harden.
- **Exemption matching.** Chose literal top-level `ARCHITECTURE.md` (the file ship.md §3.5 writes), matching treefp's top-level `:/.dross`. Rejected matching the basename at any depth.
- **Red-proof repoint.** Ship's own commit edits a doc outside .dross. That stales a re-ship with no fix in it, and I accepted this rather than widening the locked exemption list.
