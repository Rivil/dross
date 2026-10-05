# verify-staleness — panel synthesis

Sources: risk.md (7 tasks / 3 waves), mvp.md (4 / 2), verification.md (8 / 5).

I checked the drafts against the source before scoring. Their key claims hold:

- `treefp.tree()` strips only `:/.dross`, and `treefp.Diff` already names the changed paths.
- `Skeleton` copies `Tests.MeasuredOn`.
- `finalizeVerify` is Load→mutate→Save (internal/cmd/verify.go:1096).
- The ship gate order is: auto-finalize → verdict switch → branch check → `--print-body`/`--no-push` → repoint → `autoCommitDrossDirt`.
- `suggestNext` also feeds `reentryLine` (the SessionStart hook).

Three defects turned up, and the merged plan fixes each one:

- Verification's t-4 adds a `finishVerify` parameter but omits `internal/cmd/verify_notcovered_test.go`. That file holds 3 of the 8 test call sites, so it would not compile.
- Verification's Forgejo anchor `{"do":"squash"}` doesn't match ship.md, which writes `{"Do":"squash"}`.
- Risk's detached task never lists `internal/cmd/verify_detach_test.go`. That file holds `detachRecorder.install` and the exact-order assertion. Its `chdirDross` fixtures are non-git, so a fail-closed dispatch capture breaks them unless the seam is stubbed there.

## Scores

Scale 1–5.

| dimension | draft | score | note |
|---|---|---|---|
| criteria coverage | risk | 5 | 7/7, and every failure mode has exactly one owning task. c-1 is covered across attached, `--skip-mutation`, detached, an LLM edit and a stale-binary Save. |
| criteria coverage | mvp | 3 | 7/7 nominally. What happens when the tree can't be captured is unspecified, and the tree can still be dropped from verify.toml. Either way verify.toml reads as legacy and ships with a warning, which quietly undoes c-2. |
| criteria coverage | verification | 5 | 7/7, derived from a contract ledger. Adds c-3 lookalikes (`ARCHITECTURE.md.bak`, `.drossrc`) and ship's own PR-record commit staying fresh. |
| test-contract specificity | risk | 4 | Behavioural contracts for every edge: index byte-identity, unborn HEAD, malformed id, `--json` stdout. Few tests are named. |
| test-contract specificity | mvp | 3 | Names 4 tests and pins the happy paths and c-5 well. No contract for capture failure, mid-run edits, a pruned object, or `--json` purity. |
| test-contract specificity | verification | 5 | A named test per contract, an exact order-string assertion, and merge ordering per provider. One anchor is wrong (`{"do":"squash"}`). |
| granularity | risk | 4 | 7 tasks of 2–4 files, one concern each. Dispatch and collect are kept together because they share one file chain. |
| granularity | mvp | 2 | Too coarse. t-1 spans treefp and verify (5 files). t-3 fuses the ship gate with status. t-2 fuses the attached and detached paths. |
| granularity | verification | 4 | 8 tasks, the finest split. The dispatch/collect split is clean but spends a whole wave on a ~1-file change. |
| wave correctness | risk | 5 | 3 waves with no same-file edits inside a wave. The injected `diffFn` lets the classifier run beside treefp, and the `cmd/verify.go` edits are sequenced. It misses one test file (see above). |
| wave correctness | mvp | 4 | 2 waves, conflict-free. The parallelism comes from fusing tasks, not from splitting them. |
| wave correctness | verification | 3 | 5 waves, conflict-free but over-serialised: the classifier waits on treefp, and t-4 → t-7 → t-8 is a chain. t-4 misses `verify_notcovered_test.go`, so it won't compile. |

**Skeleton: risk.** It is the only draft with full coverage, specific contracts, conflict-free waves and real parallelism. Verification has the sharper contracts, and they are grafted in wholesale. Its 5-wave chain, its compile break and its no-carve-out refusal are not taken.

## Merged plan

Task ids are renumbered in wave order. The origin tag names the lenses whose content the task carries, and the risk id is given for traceability.

```
Phase verify-staleness — 7 tasks across 3 waves

Wave 1
  t-1  Add exemption-aware measured fingerprint to treefp        [risk+verification+mvp]  (risk t-1)
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go
       desc:     New MeasuredTree(dir) -> {Commit, Tree}. It runs `add -A` into a scratch index,
                 drops .dross/ and ARCHITECTURE.md (literal paths, anchored at dir), then runs
                 write-tree. Commit = HEAD, or "" on an unborn HEAD. WorkingTree and tree() are
                 unchanged. Verification's CommitTreeWithout is not taken (see D8). HasObject is
                 not taken either: a Diff error already classifies as unlistable.
       covers:   c-1, c-3
       contract: - if the exemption matches by basename or prefix, or misses one of the two paths,
                   TestMeasuredTreeExemptions fails. Editing root ARCHITECTURE.md or .dross/x must
                   leave Tree byte-equal. Editing docs/ARCHITECTURE.md, or creating
                   ARCHITECTURE.md.bak, sub/.dross/x or a root .drossrc, must change it.
                   [risk+verification]
                 - if MeasuredTree stops covering the whole work tree, TestMeasuredTreeCoversTheWorkTree
                   fails. Each of these alone must change Tree: an uncommitted tracked edit, an
                   untracked non-ignored file, a deleted tracked file, chmod +x, README.md,
                   .github/workflows/ci.yml, x_test.go. A .gitignore'd file must not.
                   [all three; the locked list pinned behaviourally, from verification]
                 - if the new exemption leaks into the shared tree(),
                   TestWorkingTreeStillCountsArchitecture fails: WorkingTree must still move when root
                   ARCHITECTURE.md changes. The commit gate and the `dross test` green record depend
                   on it. [risk+mvp]
                 - if MeasuredTree writes to the real index, TestRealIndexUntouched fails. Extend it
                   to call MeasuredTree with a partially staged file present. [verification+risk]
                 - if MeasuredTree makes an unbounded git call, TestEveryGitCallIsBounded fails.
                   Extend it so a hanging stub git fails within Timeout. [verification]
                 - if Tree is derived from HEAD, TestMeasuredTreeIgnoresCommitMoves fails. A
                   .dross-only commit, and a commit of exactly the measured dirty changes, must each
                   leave Tree identical while Commit moves. [risk]
                 - if Diff over two MeasuredTree ids names an exempt path or misses a non-exempt one,
                   TestMeasuredTreeDiffNamesChanges fails. It must list exactly the added, removed and
                   modified non-exempt paths. [risk]
                 - if an edge repo is mishandled, TestMeasuredTreeEdgeRepos fails. An unborn HEAD
                   must give Commit "" plus a tree. A non-git dir must give an error, never "".
                   [risk+verification]
                 - if the exemptions anchor at the git top instead of dir,
                   TestMeasuredTreeAnchorsAtDrossRoot fails. With dir a git subdirectory holding
                   .dross, dir/.dross and dir/ARCHITECTURE.md must be the exempt paths. [risk, see D7]
       depends:  —

  t-2  Record measured tree; classify verdict freshness          [risk+verification]  (risk t-2)
       files:    internal/verify/verify.go, internal/verify/freshness.go,
                 internal/verify/freshness_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     Add measured_commit and measured_tree to Tests (json, omitempty) and to VerifyMeta
                 (toml, omitempty). Skeleton copies them, the way it copies measured_on. Add a pure
                 Classify(verify, tests, currentTree, diffFn) returning fresh | stale{changed,
                 listErr} | unknown | malformed. Add four not_paths rows.
       covers:   c-1, c-2, c-5
       contract: - if Skeleton stops copying the fields, or a finalize-shaped Load → set
                   verdict+finalized → Save drops them, TestSkeletonCarriesMeasuredTree fails.
                   [risk+mvp]
                 - if omitempty is lost, TestLegacyVerifyRoundTripsByteIdentical fails. A pre-phase
                   verify.toml must Load→Save byte-identical with no measured_* keys.
                   [verification+risk]
                 - if a legacy file can reach git or classify as anything but unknown,
                   TestClassifyLegacyIsUnknownWithoutGit fails. With no recorded tree, Classify must
                   return unknown, never call diffFn, and do so in a non-git dir.
                   [verification+risk+mvp]
                 - if the tests.json fallback is lost, TestClassifyFallsBackToTestsJSON fails. A
                   verify.toml without measured_tree, beside a tests.json that has one at an EQUAL
                   generated_at, must classify against tests.json's tree. An unequal generated_at
                   must classify unknown. [risk, see D5]
                 - if freshness is compared by commit, TestClassifyFreshByTreeNotCommit fails. Equal
                   trees must classify fresh even when measured_commit != HEAD. [risk]
                 - if a malformed id reaches git, TestClassifyRejectsMalformedTree fails. "HEAD",
                   "--output=x" and "abc" must classify malformed with diffFn never called. [risk]
                 - if a stale classification loses its names, or an unlistable diff reads as fresh or
                   unknown, TestClassifyStaleNamesAndUnlistable fails. Unequal trees must classify
                   stale with diffFn's names, sorted. A diffFn error must classify stale with listErr
                   set. [risk+verification]
                 - if a field is undeclared, TestEveryPathShapedFieldIsDeclared fails. It must pass
                   with the four new rows. [risk+mvp]
       depends:  —

  t-3  Re-ship before merge in ship.md; keep the tree in verify.md   [risk+verification+mvp]  (risk t-7)
       files:    assets/prompts/ship.md, assets/prompts/verify.md,
                 internal/cmd/ship_prompt_test.go, internal/cmd/verify_prompt_test.go, README.md
       desc:     §5 On failure: after `git push origin phase/<id>`, re-running `dross ship
                 <phase-id>` becomes required and the "is also safe" wording goes. A stale refusal
                 routes to /dross-verify, then commit, re-ship, Watch checks. §6: re-run `dross ship
                 <phase-id>` before the provider merge call; a refusal stops the merge, and §6 keeps
                 one AskUserQuestion. §0 step 4 and §4 step 1 name the stale-pass refusal. verify.md
                 §3: leave [verify].measured_commit and measured_tree exactly as written. README rows
                 for `dross verify`, `verify results`, `status` and `ship` describe the recorded tree
                 and the stale gate; refresh the ship.md and verify.md token rows if either moves
                 >50 bytes [mvp]. (r-01: run `make install` afterwards.)
       covers:   c-7, c-1
       contract: - if §5 On failure's `dross ship <phase-id>` is missing, isn't between
                   `git push origin phase/<id>` and "Loop back", or the retired "Re-running
                   `dross ship` is also safe" survives, TestShipPromptReShipsAfterEveryCIFix fails.
                   [risk+verification, see D12]
                 - if §6's `dross ship <phase-id>` does not precede every provider merge call,
                   TestShipPromptReShipsBeforeMerge fails. The calls are `gh pr merge`, the Forgejo
                   `/pulls/<n>/merge` POST (`{"Do":"squash"}`, capital D as ship.md writes it) and the
                   GitLab `{"squash":true}` PUT. Moving the re-ship below any one of them must fail
                   it. [verification, anchor corrected]
                 - if neither §5 On failure nor §6 names /dross-verify as the answer to a stale
                   refusal, TestShipPromptReShipsBeforeMerge fails. [risk+verification+mvp]
                 - if the retry guidance or §6's single decision turn regresses,
                   TestShipPromptReRunIsTheRetry and TestShipPromptDecisionTurnsSeparate
                   (internal/cmd/interaction_coreloop_test.go) fail. Both are existing tests that
                   must stay green. [risk+mvp]
                 - if verify.md §3 stops telling the agent to leave measured_commit and measured_tree
                   untouched, TestVerifyPromptKeepsTheMeasuredTree fails. [risk+verification, see D5]
                 - if the README verify row loses its lifecycle wording during the edit,
                   TestReadmeVerifyRowDescribesLifecycle (existing) fails. [mvp]
       depends:  —

Wave 2 (depends t-1, t-2)
  t-4  Capture tree at attached verify start                     [risk+verification+mvp]  (risk t-3)
       files:    internal/cmd/verify.go, internal/cmd/verify_measured_test.go
       desc:     Add a measuredTreeFn seam (= treefp.MeasuredTree). On the attached path in
                 `dross verify` RunE, capture after requireExecConsent and the nothing-to-do return,
                 before RunScoped. The --detach branch captures in dispatchDetached instead (t-6).
                 Stamp t.Measured* before finishVerify, which keeps its signature (see D3). In a git
                 tree a capture error aborts; outside one, record nothing and print one line (D6).
                 When t carries a tree, finishVerify re-fingerprints and names every path that moved
                 since capture, with a .gitignore hint.
       covers:   c-1
       contract: - if the tree is captured at finish instead of start, TestVerifyRecordsTheTreeAtRunStart
                   fails. A stub adapter that edits a non-exempt file during RunScoped must leave the
                   PRE-run tree in verify.toml, with stdout naming that file. [risk+verification]
                 - if tool-output drift is left unexplained, TestVerifyNamesUnignoredToolOutput fails.
                   An adapter writing an untracked reports/x.json must get that path named with the
                   add-it-to-.gitignore hint. [risk]
                 - if --skip-mutation stops stamping, TestVerifySkipMutationRecordsTheMeasuredTree
                   fails. Run in a git fixture with an uncommitted edit and an untracked file:
                   measured_tree must equal MeasuredTree taken before the run and differ from the
                   HEAD-only tree, and measured_commit must equal HEAD. [all three]
                 - if a capture failure fails open, TestVerifyRefusesWhenTheTreeCannotBeMeasured
                   fails. With measuredTreeFn erroring in a git tree: non-zero exit, adapter Run count
                   0, and neither tests.json nor verify.toml written. [risk+verification]
                 - if capture moves ahead of consent, TestVerifyConsentPrecedesCapture fails. A
                   requireExecConsent refusal must return before measuredTreeFn is called. [risk]
                 - if the non-git path refuses, TestVerifyOutsideGitRecordsNoTree fails, and so does
                   the existing non-git TestVerifyWritesSkeletonWithSkipMutation. verify.toml must
                   carry no measured_* keys, plus one freshness-not-recorded line. [risk, see D6]
                 - if finishVerify recaptures with no recorded tree, or its signature changes, the
                   existing verify_finish_test.go and verify_notcovered_test.go cases fail. [risk]
       depends:  t-1, t-2

  t-5  Refuse stale pass verdict in ship                          [risk+verification+mvp]  (risk t-5)
       files:    internal/cmd/ship.go, internal/cmd/verdict_fresh.go,
                 internal/cmd/ship_stale_test.go, cmd/dross/testdata/cli_tree.txt
       desc:     Add verdictFreshness(root, repoDir, phaseID). It loads verify.toml and tests.json,
                 takes MeasuredTree, and calls verify.Classify with treefp.Diff. Ship runs it for a
                 pass verdict AFTER the branch check and BEFORE --print-body / --no-push / repoint /
                 autoCommitDrossDirt. Stale, malformed or a capture error refuses, naming every
                 changed file (D10), `dross verify <id>`, /dross-verify and --force-unverified.
                 --force-unverified proceeds with an override line on stderr. Unknown prints a
                 warning on stderr and proceeds. Update the --force-unverified usage text and the
                 cli_tree golden (D11).
       covers:   c-2, c-3, c-5
       contract: - if the gate misses a change kind, TestShipRefusesAStalePass fails. Modify, add and
                   delete a non-exempt file after the run: `ship --no-push` must exit non-zero naming
                   all three paths, `dross verify <id>` and --force-unverified, without
                   auto-committing a dirty .dross file (HEAD unchanged). [all three]
                 - if the gate moves after the dirty-tree check, TestShipStaleCountsUncommittedEdits
                   fails. An uncommitted src/tag.ts edit alone must be refused as stale, not as a
                   dirty tree. [verification]
                 - if a stale refusal lets anything out, TestShipStaleRefusalPushesNothing fails. In
                   the mock-remote flow: no phase/x on origin, no POST /pulls captured, and the
                   changes.json PR left unset. [verification]
                 - if exempt changes stale the verdict, TestShipExemptChangesStayFresh fails. After a
                   committed ARCHITECTURE.md landmark merge plus .dross commits (verify artefacts,
                   auto-finalize marker), `ship --no-push` must pass the gate. [risk+verification+mvp]
                 - if exemption matching widens, TestShipExemptLookalikesGoStale fails.
                   docs/ARCHITECTURE.md, then .drossrc, must each be refused. [verification]
                 - if ship's own PR-record commit stales its verdict, which would break c-7's §6
                   re-ship, TestShipReRunAfterRecordCommitStaysFresh fails. Run the full mock flow
                   twice on a stamped fixture; the second run must pass the gate. [verification]
                 - if the stale gate runs before the branch check, TestShipOffBranchRefusalBeatsStale
                   fails. A stale tree off phase/<id> must get the must-be-on-phase-branch error, not
                   a staleness list. [risk+verification]
                 - if the override stops working or goes silent, TestShipForceUnverifiedOverridesStale
                   fails. --force-unverified on a stale pass must pass the gate and print a stderr line
                   that says stale and gives the changed-file count. [risk+verification+mvp]
                 - if the legacy path refuses or pollutes stdout, TestShipLegacyVerifyWarnsAndProceeds
                   fails. With no tree: exit 0 and "freshness unknown" on stderr. With --json, stdout
                   must decode as exactly one object. [all three]
                 - if an unlistable diff proceeds, TestShipRefusesUnlistableTree fails. A well-formed
                   recorded tree whose object is absent must refuse, saying the changed files could
                   not be listed. [risk+verification]
                 - if a malformed field reaches git, TestShipRefusesMalformedTree fails. The refusal
                   must name the field. [risk]
                 - if the refusal truncates, TestShipStaleNamesEveryFile fails. 25 changed files must
                   all be named. [verification+mvp, see D10]
                 - if the usage text drifts from the golden, TestCLITreeGolden (cmd/dross) fails.
                   [risk+verification, see D11]
       depends:  t-1, t-2

Wave 3
  t-6  Carry dispatch-time tree through detached runs            [risk+verification+mvp]  (risk t-4; verification t-7+t-8)
       files:    internal/localstore/store.go, internal/cmd/verify.go,
                 internal/cmd/verify_detach_tree_test.go, internal/cmd/verify_detach_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       desc:     DetachedRun gains commit/tree (toml, omitempty). dispatchDetached captures through
                 measuredTreeFn after the probe and before detachSync, and stores the result in the
                 record (still written last). collectDetachedFrom stamps t from the record, never
                 from a collect-time capture, so finishVerify's drift naming (t-4) names
                 dispatch→collect changes. A record without a tree collects with no measured_* keys
                 and says freshness is unknown. detachRecorder.install stubs measuredTreeFn and
                 records "measure", which keeps the non-git chdirDross fixtures valid
                 [verification]. Two not_paths rows.
       covers:   c-1, c-4, c-5
       contract: - if dispatch captures after the push, or not at all, TestDispatchPushesBeforeItStarts
                   fails. Its order assertion becomes "probe measure sync spawn". [verification]
                 - if edits during the push are vouched for, TestDispatchMeasuresBeforeThePush fails.
                   A detachSync stub that rewrites a tracked file must leave the pre-sync tree in the
                   record, and `verify results` must write that tree and name the file.
                   [risk+verification]
                 - if collect substitutes the collect-time tree, which would break the locked
                   detached_baseline, TestCollectRecordsTheDispatchTreeNotTheCollectTree fails. If
                   a.go is edited between dispatch and collect, measured_tree must equal the record's
                   tree and differ from MeasuredTree at collect. stdout must name a.go, and
                   verify.Classify on the result must report stale naming a.go. [risk+verification+mvp]
                 - if drift naming misfires, TestCollectUnchangedTreeNamesNothing fails. With no edit
                   there must be no changed-since-dispatch line. [verification]
                 - if collect stamps today's HEAD, TestCollectStampsTheDispatchCommit fails.
                   measured_commit must equal the record's commit. [verification]
                 - if a capture failure at dispatch fails open, TestDispatchRefusesAnUnmeasurableTree
                   fails. With measuredTreeFn erroring: no detachSync, no detachSpawn, no record.
                   [risk+verification]
                 - if the record-last order breaks with the new fields,
                   TestDispatchSpawnFailureLeavesNoRecord fails. detachSpawn failing after capture
                   must leave no record. [risk]
                 - if the record loses the tree, or a pre-phase record is filled from the collect
                   tree, TestDetachedRunTreeRoundTrip fails. RecordDetachedRun → FindDetachedRun must
                   round-trip commit/tree through local.toml. A tree-less record must collect with no
                   measured_* keys and print a freshness-unknown line. [risk+verification]
                 - if the real seam isn't wired into the command, TestVerifyDetachDispatchesThroughTheCommand
                   (existing; detachCmdRepo is a git fixture) fails. It gains the assertion record.Tree
                   == MeasuredTree(dir).Tree. [verification]
                 - if a field is undeclared, TestEveryPathShapedFieldIsDeclared fails. It must pass
                   with the DetachedRun.Commit/Tree rows. [risk+mvp]
       depends:  t-1, t-2, t-4

  t-7  Mark stale verdict in dross status                         [risk+verification+mvp]  (risk t-6)
       files:    internal/cmd/status.go, internal/cmd/status_stale_test.go
       desc:     Applies to the current phase when its verdict is pass and it records a tree, HEAD is
                 on phase/<id> (D8), and the phase is not complete. Status prints "stale: verdict
                 predates N changed file(s) — re-verify with /dross-verify". suggestNext names
                 /dross-verify instead of /dross-ship for a stale pass (D9). Uses t-5's
                 verdictFreshness. Any freshness error prints nothing, because status is a hook
                 target.
       covers:   c-6
       contract: - if the marker or its count is lost, TestStatusMarksAStalePassWithCount fails. On
                   phase/x, with 2 non-exempt files changed after the stamp, stdout must contain
                   "stale" and "2 file". [all three]
                 - if exempt or legacy states mark, TestStatusNoMarkerWhenFreshOrLegacy fails. A fresh
                   stamp, a change confined to .dross/ and ARCHITECTURE.md, a tree-less verify.toml
                   and a complete phase must each print no "stale". [risk+verification]
                 - if non-pass verdicts mark, TestStatusNoMarkerOnNonPass fails. Pending and fail
                   verdicts over a differing tree must print none. [verification, see D8]
                 - if the work tree of another branch leaks in, TestStatusNoMarkerFromMain fails. With
                   HEAD on main, the SessionStart shape, a phase verified on phase/<id> must print no
                   stale line. [risk+mvp, see D8]
                 - if status breaks in a hook, TestStatusStaleSilentOnError fails. With
                   measuredTreeFn erroring, or a non-git dross root carrying a tree: exit 0, no marker,
                   rest of the output intact. [risk+verification]
                 - if an unlistable diff hides the marker, TestStatusStaleWithPrunedTree fails. It
                   must print "stale (changed files could not be listed)", without a count but never
                   omitted. [risk+verification]
                 - if status still points a stale pass at ship, TestStatusStaleSuggestsReverify fails.
                   The re-entry footer must name /dross-verify, not /dross-ship, and stay byte-equal to
                   `dross reentry`'s line. [risk+verification, see D9]
       depends:  t-5
```

### Coverage

| criterion | tasks |
|---|---|
| c-1 records commit + tree on every run kind | t-1 (scope), t-2 (fields), t-4 (attached, --skip-mutation), t-6 (detached), t-3 (verify.md keeps the fields) |
| c-2 ship refuses a stale pass, names files + re-verify, override | t-2 (classifier), t-5 (gate) |
| c-3 .dross/** + ARCHITECTURE.md changes stay fresh | t-1 (exempt fingerprint), t-5 (landmark merge, record commit, lookalikes) |
| c-4 detached baseline = dispatch tree, names dispatch→collect drift | t-6 |
| c-5 legacy verify.toml warns, never refuses | t-2 (unknown), t-5 (warn + proceed), t-6 (tree-less record) |
| c-6 status marks a stale verdict with its count | t-7 |
| c-7 ship.md re-ships after a fix and before merge, pinned by a prompt test | t-3 |

All 7 criteria are covered. The waves are conflict-free:

- W1 file sets are disjoint.
- In W2, `cmd/verify.go` is edited only by t-4.
- In W3, `cmd/verify.go` is edited only by t-6, and `not_paths.txt` is edited only by t-6 (t-2 edits it in W1).

### Residuals carried from the drafts

These are accepted in the drafts, not new decisions:

- **Red-proof repoint stales the re-ship.** Ship's own red-proof repoint commits a non-exempt doc, so the §6 re-ship c-7 requires reads stale after a ship that repointed, and needs a re-verify or --force-unverified. Risk and MVP accept this explicitly; verification is silent.
- **Adapter output can stale every run.** Output written to non-ignored dirs stales the run. t-4 names it with a .gitignore hint rather than exempting it. [risk]
- **`dross milestone complete` stays ungated.** It is outside what the spec names. [risk+verification]

## Disagreements

**D1. Wave depth: does the classifier wait on treefp?**
- Risk makes `Classify` pure, with `currentTree` and `diffFn` injected. That puts it in wave 1 beside treefp: 3 waves.
- Verification puts `MeasureTree`/`MeasureBranch` inside `internal/verify`, wrapping treefp, so its model task depends on t-1. Combined with its dispatch→collect chain, that makes 5 waves.
- MVP reaches 2 waves by fusing treefp and the model into one task.
- Default: risk's 3 waves, with the capture living in treefp and the classifier pure.
- Why it matters: verification's shape serialises 5 tasks behind a single-file change. The cost is that risk's classifier is tested against a fake diffFn, so the real-git freshness behaviour is pinned in t-1, t-5 and t-6 instead of in the model's own tests.

**D2. Detached work: one task or two?**
- Verification splits dispatch (t-7) from collect (t-8) across waves 4 and 5.
- Risk keeps both in one task.
- MVP folds the attached and detached paths into a single task.
- Default: risk's single detached task (t-6), carrying all of verification's dispatch and collect contracts.
- Why it matters: t-6 is 5 files across two layers (localstore and cmd). The split would make each review smaller but add a wave whose only file is `cmd/verify.go`.

**D3. How the tree reaches verify.toml.**
- Risk and MVP put measured fields on `verify.Tests` (json) and on `VerifyMeta`, and let `Skeleton` copy them the way `measured_on` is copied. finishVerify's signature is unchanged.
- Verification adds a `measured verify.Measured` parameter to `finishVerify` and puts the fields on `VerifyMeta` only.
- Default: the Tests carrier.
- Why it matters: verification's route touches 10 call sites, and its file list misses `verify_notcovered_test.go` (3 calls), which would break the build. The Tests carrier is also the precondition for D5's tests.json fallback. Choosing verification's route means dropping that fallback.

**D4. Key names.**
- Risk writes `measured_commit` / `measured_tree`.
- MVP and verification write bare `commit` / `tree` under `[verify]`.
- Default: `measured_*`, the minority choice. It parallels the existing `measured_on`, and it names its meaning to the /dross-verify agent that hand-edits the file, which is the actor most likely to drop an opaque hash.
- Why it matters: the names get baked into the verify.md keep-pin, the prompt test, the pathfence rows and every verify.toml written from now on. Renaming later is a migration.

**D5. Hardening against a dropped tree.**
- Risk adds a verify.md keep-pin, plus a tests.json fallback at an equal `generated_at`.
- Verification has the keep-pin only.
- MVP has neither, and explicitly accepts the degrade to "unknown".
- Default: both of risk's measures.
- Why it matters: `finalizeVerify` is Load→mutate→Save (verify.go:1096), so a stale installed binary (r-01, a recurring problem on this repo) re-saves verify.toml without the new keys. A dropped tree reads as legacy, which warns and ships, quietly turning c-2's refusal into a warning. The fallback costs one more input to Classify.

**D6. Capture-failure policy.**
- Risk fails closed inside a git work tree; outside git it records nothing and prints one line.
- Verification fails closed everywhere: verify and dispatch refuse.
- MVP leaves it unspecified.
- Default: risk's policy.
- Why it matters: the existing `--skip-mutation` tests run `dross init` in non-git temp dirs (verify_test.go:56+), so verification's rule breaks them. Risk's carve-out writes a legacy-shaped verify.toml there. That isn't a bypass, because ship cannot run outside git.

**D7. Exemption anchoring and API shape.**
- Risk's `MeasuredTree(dir)` hard-codes both exemptions, anchored at `dir` (the dross root, `filepath.Dir(.dross)`).
- MVP and verification add a generic "drop literal top-level paths" variant (`WorkingTreeWithout(dir, exclude...)`), matching treefp's existing git-top `:/.dross`. Verification keeps the exemption list in `internal/verify`.
- Default: risk's anchoring.
- Why it matters: `repo.layout` allows `monorepo`, so `.dross` can sit below the git top. With top-level anchoring, `sub/.dross` stays in the fingerprint, verify's own verify.toml write stales its run, and every ship refuses. The generic exclude API is more reusable but leaves the anchor to each caller.

**D8. Status off the phase branch, and which verdicts are marked.**
- Risk and MVP compute freshness only when HEAD is `phase/<id>`.
- Verification compares `phase/<id>`'s branch-tip tree from any other branch, which needs `CommitTreeWithout` in treefp.
- Risk marks any resolved verdict; verification marks pass only.
- Default: phase-branch only, pass only, and no CommitTreeWithout.
- Why it matters: the SessionStart hook runs from main, so under the default the marker rarely appears there, which gives c-6 less reach. Verification's route reaches main but can false-stale a run that measured uncommitted content not yet on the branch tip. Pass-only follows c-6's "verified phase"; a stale fail is already blocked.

**D9. suggestNext for a stale pass.**
- Risk redirects to `/dross-verify`.
- Verification redirects to `dross verify x`.
- MVP keeps `/dross-ship`, reasoning that c-6 only asks for a mark.
- Default: redirect to `/dross-verify`. A re-verify needs both the mechanical run and the judgement step, and the slash command does both.
- Why it matters: suggestNext also feeds `reentryLine`, the SessionStart output (status.go:141-145), so the change reaches every session start. Keeping `/dross-ship` sends the user straight into ship's refusal.

**D10. Cap on the refusal list.**
- Risk prints 20 names plus "and N more".
- Verification explicitly lists every file.
- MVP names each file.
- Default: no cap.
- Why it matters: a base merge into `phase/<id>` can change hundreds of paths, producing a wall of output. c-2 says "naming the changed files", and a cap stops naming some of them.

**D11. --force-unverified help text.**
- Risk and verification reword it to cover stale verdicts and update `cmd/dross/testdata/cli_tree.txt`.
- MVP keeps the current text so the CLI goldens don't move.
- Default: reword.
- Why it matters: after this phase the locked stale_override makes the flag cover stale passes, and the help would understate that. The cost is a golden diff in t-5.

**D12. Where the §5 re-ship sits.**
- Risk and verification keep `git push origin phase/<id>`, then make `dross ship <phase-id>` mandatory.
- MVP drops the raw push: re-running ship right after the fix commit does the push, so the gate runs before the push.
- Default: push, then ship. c-7 says "after any fix pushed to phase/<id>".
- Why it matters: under MVP's order a stale refusal keeps the fix off the PR, so CI can't re-run on it until a re-verify. Under the default the fix reaches CI first, and §6's re-ship still blocks the merge.
