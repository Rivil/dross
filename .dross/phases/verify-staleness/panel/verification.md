# verify-staleness — verification-lens draft

Lens: every criterion's ideal test contract was written first (ledger below); each task is the
smallest change that makes its contracts satisfiable. A contract that could not be stated
concretely meant the behaviour was underspecified, and that gap became a judgment call.

## Contract ledger (written before the tasks)

| crit | ideal contract — "if X breaks, Y fails" | derived task |
|---|---|---|
| c-1 | uncommitted tracked edit and untracked-unignored file each change the fingerprint; ignored file, `.dross/x`, root `ARCHITECTURE.md` do not | t-1 |
| c-1 | `dross verify x --skip-mutation` writes `[verify].tree` == the measured tree incl. uncommitted edits, `[verify].commit` == HEAD | t-4 |
| c-1 | adapter edits a file mid-run → recorded tree is the run-START tree, and the file is named | t-4 |
| c-1 | `/dross-verify`'s hand-edit of verify.toml keeps `commit`/`tree` (else a pass silently degrades to legacy) | t-2 |
| c-2 | modified + added + removed non-exempt files → ship refuses naming all three plus `dross verify x`; nothing pushed, no PR call | t-3, t-5 |
| c-2 | same stale tree + `--force-unverified` → passes the gate, narration says stale | t-5 |
| c-2 | recorded tree object pruned/absent and ids differ → still Stale, never Fresh/Unknown | t-3 |
| c-3 | commits touching only `.dross/**` + root `ARCHITECTURE.md` → Fresh; ship proceeds | t-3, t-5 |
| c-3 | lookalikes `docs/ARCHITECTURE.md`, `ARCHITECTURE.md.bak`, `.drossrc` → Stale | t-1, t-5 |
| c-3 | ship re-run after its own PR-record commit → still Fresh | t-5 |
| c-4 | `verify results` writes the record's dispatch tree, not the collect-time tree, and names files changed in between | t-8 |
| c-4 | dispatch measures BEFORE the rsync push (order `probe measure sync spawn`) | t-7 |
| c-4 | a record written by an older dross (no tree) → verify.toml records no tree, never the collect-time one | t-8 |
| c-5 | verify.toml with no tree → ship exit 0 with "freshness unknown"; under `--json` stdout is still one JSON object | t-3, t-5 |
| c-6 | on phase/x, 2 non-exempt files changed after a pass → status prints stale + "2 files"; fresh/legacy/non-pass → no marker | t-6 |
| c-6 | status run from main while phase/x is unchanged → no marker (main's own tree never leaks in) | t-6 |
| c-7 | ship.md §5 on-failure: fix commit < `dross ship <phase-id>` < loop-back; §6: `dross ship <phase-id>` < every provider merge call | t-2 |

## Plan

```
Phase verify-staleness — 8 tasks across 5 waves

Wave 1
  t-1  Add exemption-aware tree fingerprints to treefp
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go
       does:     WorkingTreeWithout(dir, exclude...) = WorkingTree minus literal top-level paths;
                 CommitTreeWithout(dir, rev, exclude...) = same recipe over a commit's tree (no
                 work-tree hashing); HasObject(dir, id) via bounded `git cat-file -e`.
       covers:   c-1, c-3
       contract: - WorkingTreeWithout(dir,"ARCHITECTURE.md"): editing root ARCHITECTURE.md or .dross/x
                   leaves the id byte-equal; editing docs/ARCHITECTURE.md or creating
                   ARCHITECTURE.md.bak changes it (a basename/prefix match fails here)
                 - an uncommitted edit to a tracked file changes the id; a new untracked-unignored
                   file changes it; a new .gitignored file does not
                 - CommitTreeWithout(dir,"phase/x") == WorkingTreeWithout on a clean checkout of
                   phase/x, and != CommitTreeWithout(dir,"main") when the branches differ
                 - HasObject returns false for a well-formed id never written, true for a fresh id
                 - TestRealIndexUntouched extended to call all three: real index byte-identical after
                 - TestEveryGitCallIsBounded extended: a hanging stub git fails each new call within Timeout
       depends:  —

  t-2  Pin re-ship order and tree preservation in prompts
       files:    assets/prompts/ship.md, assets/prompts/verify.md,
                 internal/cmd/ship_prompt_test.go, internal/cmd/verify_prompt_test.go
       does:     §5 On-failure: after the fix is committed/pushed, re-running `dross ship <phase-id>` is
                 REQUIRED (not "also safe"); a stale refusal routes to re-verify. §6: run
                 `dross ship <phase-id>` (must exit 0) before the squash-merge call. §0.4/§4.1 mention
                 freshness. verify.md §3: leave `[verify].commit` and `tree` exactly as written.
                 (r-01: `make install` before relying on the edited prompts.)
       covers:   c-7, c-1
       contract: - TestShipPromptReShipsAfterEveryCIFix: in §5 "on failure", index(commit-the-fix step)
                   < index("dross ship <phase-id>") < index("loop back"); the retired phrase
                   "re-running `dross ship` is also safe" is absent
                 - TestShipPromptReShipsBeforeMerge: in §6, index("dross ship <phase-id>") precedes
                   "gh pr merge", the forgejo `{"do":"squash"}` POST and the gitlab `{"squash":true}`
                   PUT — moving the re-ship below any one provider's merge call fails
                 - both sections name `dross verify <phase-id>` as the answer to a stale refusal
                 - TestShipPromptReRunIsTheRetry (existing) still passes — "re-run dross ship" kept
                 - TestVerifyPromptKeepsTheMeasuredTree: verify.md §3 names `commit` and `tree` under
                   `[verify]` with a leave-as-written instruction; deleting the line fails
       depends:  —

Wave 2 (depends t-1)
  t-3  Add verify freshness model and tree fields
       files:    internal/verify/verify.go, internal/verify/freshness.go,
                 internal/verify/freshness_test.go, internal/cmd/testdata/pathfence_scan/not_paths.txt
       does:     VerifyMeta gains Commit `commit,omitempty` + Tree `tree,omitempty`. freshness.go:
                 Measured{Commit,Tree}; MeasureTree(repoDir); MeasureBranch(repoDir, branch);
                 Check(repoDir, meta, current) → Freshness{State fresh|stale|unknown, Changed, Listed}.
                 The one exemption list lives here (.dross via treefp + "ARCHITECTURE.md").
                 not_paths rows: verify.VerifyMeta.Commit commit, verify.VerifyMeta.Tree tree.
       covers:   c-1, c-2, c-3, c-5
       contract: - MeasureTree: Commit == `git rev-parse HEAD`; on an unborn HEAD Commit == "" and Tree is
                   still set
                 - Check with meta.Tree == "" returns unknown even in a NON-git dir (proves no git call;
                   legacy can never error into a refusal)
                 - modify a.go, add b.go, delete c.go (plus .dross/x and ARCHITECTURE.md edits) → stale,
                   Changed == [a.go b.go c.go] sorted, exempt paths absent
                 - README.md, .github/workflows/ci.yml and x_test.go each alone → stale (the locked
                   "code, tests, README, CI config" list, pinned behaviourally)
                 - only .dross/** + ARCHITECTURE.md committed since → fresh, Changed empty
                 - meta.Tree a well-formed id absent from the store and != current → stale, Listed=false;
                   a regression returning fresh or unknown fails
                 - a legacy verify.toml (no commit/tree) Load→Save is byte-identical (omitempty); a new
                   one round-trips both fields
                 - TestEveryPathShapedFieldIsDeclared passes with the two new rows
       depends:  t-1

Wave 3 (depends t-3)
  t-4  Record the run-start tree on attached runs
       files:    internal/cmd/verify.go, internal/cmd/verify_finish_test.go, internal/cmd/verify_test.go
       does:     seam `measureTreeFn = verify.MeasureTree`; Verify RunE measures after the no-changes
                 early return and before RunScoped (covers --skip-mutation), re-measures after, prints
                 files changed during the run; finishVerify gains `measured verify.Measured` and stamps
                 [verify].commit/tree. A measure error refuses before any adapter runs.
       covers:   c-1
       contract: - TestVerifySkipMutationRecordsTheMeasuredTree: git fixture with an uncommitted edit +
                   untracked file → verify.toml tree == verify.MeasureTree(dir).Tree, != the HEAD-only
                   tree, commit == HEAD
                 - TestVerifyRecordsTheTreeAtRunStart: stub adapter's Run rewrites a tracked file →
                   recorded tree == pre-run tree, stdout names that file
                 - TestVerifyRefusesWhenTheTreeCannotBeMeasured: measureTreeFn errors → no tests.json,
                   no verify.toml, stub adapter Run count == 0
                 - TestFinishVerifyStampsCommitAndTree: Measured{"c0","t0"} → both under [verify];
                   zero Measured → file text has no `tree =` / `commit =` keys
       depends:  t-3

  t-5  Gate ship on a fresh pass verdict
       files:    internal/cmd/ship.go, internal/cmd/ship_stale_test.go, cmd/dross/testdata/cli_tree.txt
       does:     after the on-branch check and before --print-body/--no-push: pass + recorded tree →
                 Check vs MeasureTree; stale refuses naming every changed file, `dross verify <id>` then
                 /dross-verify + finalize, and --force-unverified; with --force-unverified narrate the
                 override; unknown → warning (stderr under --json) and proceed. --force-unverified usage
                 text names stale verdicts (cli_tree golden).
       covers:   c-2, c-3, c-5
       contract: - TestShipRefusesAStalePass: stamped fixture, then commit an edit to src/tag.ts, add
                   src/new.ts, delete README.md → `ship --no-push` errors naming all three paths,
                   "dross verify x" and "--force-unverified"
                 - TestShipStaleCountsUncommittedEdits: an uncommitted src/tag.ts edit alone → refused as
                   stale (not as dirty tree — the stale gate runs first)
                 - TestShipStaleRefusalPushesNothing: mock-remote flow → no phase/x on origin, no POST
                   /pulls captured, changes.json PR unset
                 - TestShipForceUnverifiedOverridesStale: same stale tree + --force-unverified → exit 0,
                   output contains "stale"
                 - TestShipExemptChangesStayFresh: commit ARCHITECTURE.md + .dross/phases/x/notes.md edits
                   → exit 0, no "stale" in output
                 - TestShipExemptLookalikesGoStale: docs/ARCHITECTURE.md, then .drossrc → each refused
                 - TestShipReRunAfterRecordCommitStaysFresh: full mock flow twice on a stamped fixture →
                   second run passes the gate (its PR-record commit is .dross-only)
                 - TestShipLegacyVerifyWarnsAndProceeds: no tree → exit 0 + "freshness unknown"; with
                   --json stdout decodes as exactly one object and the warning is on stderr
                 - TestShipOffBranchRefusalBeatsStale: stale + off-branch → the branch refusal, not stale
                 - cli_tree.txt golden updated for the --force-unverified usage string
       depends:  t-3

  t-6  Mark stale pass verdicts in dross status
       files:    internal/cmd/status.go, internal/cmd/status_stale_test.go
       does:     for the current phase with verdict pass, not complete, and a recorded tree: compare
                 against MeasureTree when HEAD is phase/<id>, else MeasureBranch(phase/<id>); print a
                 `stale:` line with the changed-file count; suggestNext names re-verify instead of
                 /dross-ship. Any git failure → silent (status is a hook target).
       covers:   c-6
       contract: - TestStatusMarksAStalePassWithCount: on phase/x, commit edits to 2 non-exempt files
                   after the stamp → stdout has "stale" and "2 files"
                 - TestStatusNoMarkerWhenFreshOrLegacy: fresh stamp, exempt-only change, and no-tree
                   verify.toml each print no "stale"
                 - TestStatusNoMarkerOnNonPass: pending and fail verdicts over a differing tree → none
                 - TestStatusStaleFromMainReadsTheBranchTip: from main with phase/x unchanged → none;
                   after a post-verify commit on phase/x → marker with count 1
                 - TestStatusStaleSilentOutsideGit: non-git dross root with a tree-carrying pass →
                   exit 0, no marker
                 - TestStatusStaleSuggestsReverify: stale pass → next line names `dross verify x`, not
                   /dross-ship
                 - TestStatusStaleWithPrunedTree: recorded tree absent from the store, ids differ →
                   marker shown without a count, never omitted
       depends:  t-3

Wave 4 (depends t-4)
  t-7  Capture the dispatch tree in detached runs
       files:    internal/localstore/store.go, internal/cmd/testdata/pathfence_scan/not_paths.txt,
                 internal/cmd/verify.go, internal/cmd/verify_detach_test.go
       does:     DetachedRun gains Commit `commit,omitempty` + Tree `tree,omitempty` (+ not_paths rows);
                 dispatchDetached calls measureTreeFn after the probe and before detachSync, carries the
                 result into the record; a measure error refuses before the push.
       covers:   c-4, c-1
       contract: - TestDispatchPushesBeforeItStarts order assertion becomes "probe measure sync spawn" —
                   measuring after the push, or not at all, fails it
                 - TestDispatchRecordsTheMeasuredTree: seam returns {"c0ffee","7ree"} → FindDetachedRun
                   (read back through local.toml) carries both
                 - TestDispatchMeasuresBeforeThePush: detachSync stub rewrites a tracked file → record.Tree
                   == the pre-sync tree
                 - TestDispatchRefusesAnUnmeasurableTree: seam errors → no sync, no spawn, no record
                 - TestVerifyDetachDispatchesThroughTheCommand (existing, git fixture) additionally
                   asserts record.Tree == verify.MeasureTree(dir).Tree with the real seam
       depends:  t-4 (measureTreeFn seam; same file)

Wave 5 (depends t-4, t-7)
  t-8  Collect detached runs against the dispatch tree
       files:    internal/cmd/verify.go, internal/cmd/verify_results_test.go
       does:     collectDetachedFrom passes Measured{rec.Commit, rec.Tree} to finishVerify; measures the
                 tree now and names every file changed since dispatch; a tree-less record (older dross)
                 records no tree and says freshness is unknown — never substitutes the collect tree.
       covers:   c-4, c-1, c-5
       contract: - TestCollectRecordsTheDispatchTreeNotTheCollectTree: record carries T0, a.go edited
                   before collect → verify.toml tree == T0 (!= current), stdout names a.go, and
                   verify.Check on the result reports stale naming a.go (dispatch→collect→ship chain)
                 - TestCollectUnchangedTreeNamesNothing: no edit → no "changed since dispatch" line
                 - TestCollectFromATreelessRecordRecordsNoTree: record without Tree → verify.toml has no
                   `tree =` key, stdout says freshness unknown; recording the collect-time tree fails it
                 - TestCollectStampsTheDispatchCommit: [verify].commit == rec.Commit, not today's HEAD
       depends:  t-4, t-7
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1, t-3, t-4, t-7, t-8, t-2 (verify.md keeps the fields through the LLM edit) |
| c-2 | t-3, t-5 |
| c-3 | t-1, t-3, t-5 |
| c-4 | t-7, t-8 |
| c-5 | t-3, t-5, t-8 |
| c-6 | t-6 |
| c-7 | t-2 |

7/7 criteria covered.

## Judgment calls

- Exemptions are removed inside the fingerprint (treefp exclude), not filtered from a Diff afterwards. Rejected diff-time filtering: c-1 defines the fingerprint over non-exempt files, and with filtering an ARCHITECTURE.md-only change still yields unequal ids, so freshness would depend on the object store.
- The ARCHITECTURE.md exemption is the root file, matched literally. Rejected basename or prefix matching: ship §3.5 writes only the root doc, and the locked decision makes every other path invalidating. The lookalike contracts in t-1/t-5 pin this.
- An attached run records the tree at run start and names files changed during the run. Rejected capture at finish, which vouches for edits no mutant saw, and rejected discarding the run, which throws away an hour of work. The ship gate catches the drift either way.
- An unmeasurable tree fails closed: verify and dispatch refuse before running. Rejected writing a tree-less verify.toml, because it reads as legacy (warn and proceed) and would silently bypass c-2.
- A recorded tree that has been pruned or is absent, with differing ids, counts as stale with an unlisted file set. Rejected "unknown", which would turn a `git gc` or a fresh clone into a bypass. Rejected pinning the tree with a ref, which adds a new writer into .git.
- The ship gate goes after the on-branch check and before the --print-body/--no-push returns. Rejected placing it beside the verdict switch: off-branch, it would measure the wrong branch's tree and report a misleading stale list.
- Under --json the legacy warning goes to stderr. Rejected narrate(), which is silent under --json and would swallow the c-5 warning. Rejected stdout, which breaks the single-object contract and the ship_json golden.
- Status checks only the current phase: the work tree on phase/<id>, the branch-tip tree elsewhere. Rejected skipping when off-branch, because SessionStart usually runs from main and the marker would rarely show. Rejected scanning every phase, which adds noise from completed phases whose branches linger.
- suggestNext names re-verify for a stale pass. Rejected keeping "/dross-ship", which contradicts the marker on the line above and leads to a refusal.
- verify.md gets a keep-the-tree pin. Rejected trusting the LLM to preserve unknown keys: a dropped `tree` degrades a fresh pass to legacy and a stale one to warn-and-proceed.
- The --force-unverified usage text is updated, which changes the cli_tree golden. Rejected leaving "verify must be pass", which understates what the one override now covers (locked stale_override).
- The stale refusal lists every changed file, with no cap. Rejected truncation: the refusal is rare, c-2 says "naming the changed files", and the list is the evidence.
- Dispatch (t-7) and collect (t-8) are separate tasks chained across waves. Rejected one 5-file, 2-layer task. All three cmd/verify.go tasks are sequenced (t-4 → t-7 → t-8) so no wave edits the same file twice.
- `dross phase complete` and `milestone complete` stay ungated. Rejected adding freshness there: the merge has already happened by then, and c-7's pre-merge re-ship is the gate the spec asks for.
- The detached capture goes through a `measureTreeFn` seam. Rejected capturing in RunE and passing it down: only a seam lets the existing order test pin "measure before sync", and the existing non-git dispatch fixtures stay valid.
