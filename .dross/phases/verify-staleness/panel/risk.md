# verify-staleness — risk-lens draft

Lens: every failure mode below has exactly one owning task, and that task's
test contract is the test that fails when the failure mode comes back.

Key finding that shapes the graph: `internal/treefp` already fingerprints the
work tree as a git tree object through a scratch copy of the index (used by the
`dross test` green record and the commit gate), and `treefp.Diff` already names
changed paths. This plan reuses it rather than inventing a second fingerprint.
It needs a new entry point, though: `treefp.tree()` takes out `.dross/` only,
and the commit gate depends on ARCHITECTURE.md staying in its fingerprint.

```
Phase verify-staleness — 7 tasks across 3 waves

Wave 1
  t-1  Add exemption-aware measured fingerprint to treefp
       files:    internal/treefp/treefp.go, internal/treefp/treefp_test.go
       covers:   c-1, c-3
       desc:     New MeasuredTree(dir) -> {Commit, Tree}: scratch-index `add -A`, then
                 drop .dross/ AND ARCHITECTURE.md (anchored at dir, the dross root),
                 write-tree; Commit = HEAD ("" on unborn). WorkingTree/tree() unchanged.
       contract: - editing root ARCHITECTURE.md or any .dross/** file leaves Tree unchanged;
                   editing docs/ARCHITECTURE.md, sub/.dross/x or a root `.drossrc` changes it
                 - editing root ARCHITECTURE.md STILL changes WorkingTree() (commit-gate /
                   green-record fingerprint semantics untouched)
                 - adding an untracked non-ignored file, deleting a tracked file without
                   committing, and chmod +x on a tracked file each change Tree; adding a
                   .gitignore'd file does not
                 - the real index file is byte-identical before/after MeasuredTree, with a
                   partially staged file present
                 - a .dross-only commit, and a commit of exactly the dirty changes that were
                   measured, each leave Tree identical while Commit moves
                 - Diff(before, after) on two MeasuredTree ids lists exactly the added,
                   removed and modified non-exempt paths (no .dross/, no ARCHITECTURE.md)
                 - unborn HEAD -> Commit "" plus a tree; a non-git dir -> error, never ""
                 - dir = a git subdirectory holding .dross: dir/.dross and dir/ARCHITECTURE.md
                   are the exempt paths (verify's own verify.toml write can't stale its run)
       depends:  —

  t-2  Record measured tree; classify verdict freshness
       files:    internal/verify/verify.go, internal/verify/freshness.go,
                 internal/verify/freshness_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-2, c-5
       desc:     measured_commit/measured_tree on Tests (json, omitempty) and VerifyMeta
                 (toml, omitempty); Skeleton copies them (the measured_on pattern). Pure
                 Classify(verify, tests, currentTree, diffFn) -> fresh | stale{changed,
                 listErr} | unknown | malformed. Four not_paths.txt rows.
       contract: - Skeleton copies the tests' measured_* into [verify]; Save -> LoadVerify ->
                   set verdict+finalized -> Save (the finalize/auto-heal path) keeps both
                 - a pre-phase verify.toml (no keys) round-trips with no measured_* keys
                   emitted and classifies unknown, never stale
                 - verify.toml lacking measured_tree but tests.json carrying one with an
                   EQUAL generated_at classifies against tests.json's tree (an LLM-dropped
                   or stale-binary-rewritten field still gates); unequal generated_at -> unknown
                 - equal recorded/current trees classify fresh even when measured_commit != HEAD
                 - a recorded tree that is not 40/64-char lowercase hex ("HEAD",
                   "--output=x", "abc") classifies malformed and diffFn is never called
                 - unequal trees classify stale carrying diffFn's names; a diffFn error
                   classifies stale with listErr set — never fresh
                 - TestEveryPathShapedFieldIsDeclared passes with the four new rows
       depends:  —

  t-7  Re-ship before merge in ship.md
       files:    assets/prompts/ship.md, assets/prompts/verify.md,
                 internal/cmd/ship_prompt_test.go, internal/cmd/verify_prompt_test.go
       covers:   c-7
       desc:     §5 On failure: after `git push origin phase/<id>`, re-run `dross ship
                 <phase-id>` (required, not "also safe"); a staleness refusal routes to
                 /dross-verify, commit, re-ship, then Watch checks. §6: re-run `dross ship
                 <phase-id>` before the provider merge call; a refusal stops the merge.
                 §0 step 4 / §4 step 1 name the stale-pass refusal. verify.md §3: leave
                 [verify].measured_commit / measured_tree exactly as written. (r-01: make
                 install afterwards.)
       contract: - new ship-prompt test fails if, in §5 On failure, `dross ship <phase-id>`
                   is not after `git push origin phase/<id>` and before "loop back"
                 - fails if in §6 `dross ship <phase-id>` does not precede the first
                   provider merge call (`gh pr merge`, hence the Forgejo/GitLab lines too)
                 - fails if neither §5 On failure nor §6 names /dross-verify as the answer to
                   a stale-verdict refusal
                 - TestShipPromptReRunIsTheRetry still passes (keeps "re-run dross ship",
                   "dross ship --force", "git pull --rebase origin phase/<id>")
                 - new verify-prompt test fails if §3 stops telling the agent to leave
                   measured_tree / measured_commit untouched
       depends:  —

Wave 2 (depends t-1, t-2)
  t-3  Capture tree at attached verify start
       files:    internal/cmd/verify.go, internal/cmd/verify_measured_test.go
       covers:   c-1
       desc:     measuredTreeFn seam (= treefp.MeasuredTree). In `dross verify` RunE capture
                 after requireExecConsent and the nothing-to-do return, before
                 configuredAdaptersFn/RunScoped; stamp t.Measured* after the run. Capture
                 error in a git tree aborts; outside one, record nothing + one line.
                 finishVerify re-fingerprints when t carries a tree and names every path
                 that moved since capture, with a gitignore hint for tool output.
       contract: - a fake adapter that edits a non-exempt file during RunScoped: verify.toml
                   records the PRE-run tree and stdout names that file as changed since the
                   measured tree was captured
                 - an adapter that writes an untracked non-ignored reports/x.json gets that
                   path named together with the add-it-to-.gitignore hint
                 - `--skip-mutation` writes verify.toml whose measured_tree equals
                   MeasuredTree taken before the run and measured_commit equals HEAD
                 - measuredTreeFn erroring in a git tree: verify exits non-zero, no adapter
                   Run is called, neither tests.json nor verify.toml exists
                 - a requireExecConsent refusal returns before measuredTreeFn is called
                 - outside a git work tree verify still writes verify.toml, with no
                   measured_* keys, and prints one line saying freshness is not recorded
                 - existing verify_finish_test / verify_notcovered_test cases (fabricated
                   Tests, non-git temp dirs) pass unchanged: no recorded tree -> no recapture
       depends:  t-1, t-2

  t-5  Refuse stale pass verdict in ship
       files:    internal/cmd/ship.go, internal/cmd/verdict_fresh.go,
                 internal/cmd/ship_stale_test.go, cmd/dross/testdata/cli_tree.txt
       covers:   c-2, c-3, c-5
       desc:     verdictFreshness(root, repoDir, phaseID): load verify.toml + tests.json,
                 MeasuredTree, verify.Classify with treefp.Diff. Ship runs it for a pass
                 verdict AFTER the branch check and BEFORE --print-body / --no-push /
                 repoint / autoCommitDrossDirt. stale | malformed | capture error -> refuse
                 naming ≤20 files + `dross verify <id>` and /dross-verify;
                 --force-unverified proceeds with a stderr override line; unknown -> stderr
                 warning, proceed. --force-unverified usage text + cli_tree golden.
       contract: - modify, add and delete a non-exempt file after the run: ship exits non-zero
                   naming all three paths and `dross verify <id>`; a dirty .dross file is NOT
                   auto-committed (HEAD unchanged — the gate precedes ship's own writes)
                 - after a committed ARCHITECTURE.md landmark merge plus .dross commits
                   (verify artefacts, auto-finalize marker), `ship --no-push` passes the gate
                 - stale tree on a branch other than phase/<id>: refusal is the
                   must-be-on-phase-branch error, not a staleness list
                 - `--force-unverified` on a stale pass passes the gate and prints an override
                   line with the changed-file count on stderr
                 - legacy verify.toml: ships with a freshness-unknown warning on stderr; with
                   --json, stdout carries nothing but the JSON object
                 - a well-formed recorded tree whose object is absent from the clone refuses
                   saying the changed files could not be listed — never proceeds
                 - malformed measured_tree refuses naming the field; never reaches git
                 - 25 changed files print 20 names plus "and 5 more"
                 - cmd/dross TestCLITree golden carries the new --force-unverified usage
       depends:  t-1, t-2

Wave 3
  t-4  Carry dispatch-time tree through detached runs     (depends t-3)
       files:    internal/localstore/store.go, internal/cmd/verify.go,
                 internal/cmd/verify_detach_tree_test.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt
       covers:   c-1, c-4
       desc:     DetachedRun gains commit/tree (toml, omitempty). dispatchDetached captures
                 via measuredTreeFn BEFORE detachSync and stores it in the record (still
                 written last). collectDetachedFrom stamps t from the record — never a
                 collect-time capture — so finishVerify's drift naming (t-3) names
                 dispatch->collect changes. A record without a tree collects with no
                 measured_* keys and says freshness is unknown. Two not_paths rows.
       contract: - a detachSync seam that edits a tracked file mid-push: the record holds the
                   pre-push tree; `verify results` writes that tree and names the file
                 - a file changed between dispatch and collect: verify.toml's measured_tree
                   equals the record's, not MeasuredTree at collect, and the file is named
                 - measuredTreeFn error at dispatch: detachSync and detachSpawn never called,
                   no record written
                 - detachSpawn failing after capture leaves no record (record-last order
                   holds with the new fields)
                 - RecordDetachedRun -> FindDetachedRun round-trips commit/tree through
                   local.toml; a pre-phase record (no tree) collects with no measured_* keys
                   and prints a freshness-unknown line
                 - TestEveryPathShapedFieldIsDeclared green with the DetachedRun.Commit/Tree rows
       depends:  t-1, t-2, t-3

  t-6  Mark stale verdict in dross status     (depends t-5)
       files:    internal/cmd/status.go, internal/cmd/status_stale_test.go
       covers:   c-6
       desc:     Current phase with a resolved verdict and a recorded tree, HEAD on
                 phase/<id>, phase not complete: print "stale: verdict predates N changed
                 file(s) — re-verify with /dross-verify". suggestNext names /dross-verify
                 instead of /dross-ship for a stale pass. Any freshness error prints nothing.
       contract: - verified phase with 3 non-exempt files changed since the run: status
                   prints a stale line carrying "3"
                 - only .dross/ and ARCHITECTURE.md changed: no stale line
                 - HEAD on main (SessionStart-hook shape) for a phase verified on phase/<id>:
                   no stale line — no false positive from another branch's tree
                 - legacy verify.toml, and a complete phase: no stale line
                 - measuredTreeFn erroring: status exits 0, no stale line, rest of output intact
                 - unlistable diff: "stale (changed files could not be listed)"
                 - stale pass: the re-entry footer names /dross-verify, not /dross-ship, and
                   stays byte-equal to `dross reentry`'s line
       depends:  t-5
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 records commit + tree, every run kind | t-1 (fingerprint scope), t-2 (persisted fields), t-3 (attached, --skip-mutation), t-4 (detached) |
| c-2 ship refuses stale pass, names files + re-verify, --force-unverified overrides | t-5 (gate), t-2 (classifier names changes), t-1 (Diff naming) |
| c-3 .dross/** + ARCHITECTURE.md changes stay fresh | t-1 (exempt fingerprint), t-5 (ship after landmark merge) |
| c-4 detached baseline = dispatch tree, names dispatch->collect changes | t-4 |
| c-5 legacy verify.toml warns, never refuses | t-2 (classified unknown), t-5 (warn + proceed) |
| c-6 status marks stale verdict with count | t-6 |
| c-7 ship.md re-ships after fix and before merge; prompt test pins order | t-7 |

7/7 criteria covered.

### Failure mode → owning task

| failure mode | owner |
|---|---|
| exemption too broad (nested .dross, docs/ARCHITECTURE.md) or too narrow | t-1 |
| commit-gate / green-record fingerprint changes as a side effect | t-1 |
| fingerprinting writes to the user's real index | t-1 |
| .dross or ARCHITECTURE.md commits move the fingerprint (HEAD-based compare) | t-1 |
| finalize / LLM Edit / stale-binary Save drops the recorded tree | t-2 |
| malformed tree string reaches git argv | t-2 |
| unlistable diff (gc'd object, other clone) reads fresh | t-2 (classifier), t-5 (message) |
| edits during an attached run vouched for | t-3 |
| capture failure records no tree and reads as legacy (fail-open) | t-3 (attached), t-4 (detached) |
| tool output in non-ignored dirs makes every run silently stale | t-3 (named with hint) |
| edits during the detached push vouched for | t-4 |
| collect-time capture (locked detached_baseline) | t-4 |
| stale list on the wrong branch / gate after ship's own writes | t-5 |
| legacy warning corrupts --json stdout | t-5 |
| status false-stale from main (SessionStart) or errors in the hook | t-6 |
| status points a stale pass at /dross-ship | t-6 |
| CI fix merges under an old pass | t-7 |

## Judgment calls

- Reuse treefp (git tree id via scratch index, Diff for names); rejected a hand-rolled per-file sha256 manifest in verify.toml (~1.5k lines the /dross-verify agent must Read before it can Edit the file).
- New `treefp.MeasuredTree` entry point; rejected adding ARCHITECTURE.md to the shared `tree()`, because the commit gate and the `dross test` green record must keep counting it (TestShipPromptTestsBeforeDocsCommit depends on that).
- Capture at attached start / before the detached sync, never at write time: every race then fails toward stale, never toward fresh. Rejected end-of-run capture for the reason the locked detached_baseline gives (it vouches for edits the run never saw).
- Capture failure aborts verify/dispatch (fail closed). Rejected greenRecorder's warn-and-continue: a missing tree reads as legacy, which warns and ships. Outside git, record nothing, since ship can't run there anyway.
- tests.json fallback when verify.toml lacks the tree at an equal generated_at. The LLM Edit and a stale-binary finalize Save both sit between record and gate. Rejected trusting verify.toml alone, which would quietly turn the gate into a warning.
- Persist only commit + tree id; changed files are printed, never stored. Rejected storing path lists: path-shaped fields need pathfence declaration and Contain routing, and buy nothing for the gate.
- Malformed tree, unlistable diff and ship-time capture failure refuse. Only a verdict recording no tree at all is c-5's legacy-warn case.
- Gate goes after the branch check and before --print-body/--no-push/repoint/auto-commit. Rejected putting it beside the verdict switch: on the wrong branch it would print a misleading stale list. Rejected putting it after repoint: ship's own writes would stale its own run.
- Status computes freshness only with HEAD on phase/<id>. Rejected computing it elsewhere: the SessionStart hook runs on main and would false-stale every session. Rejected comparing phase/<id>'s commit tree: that drops the uncommitted content the run measured.
- suggestNext is redirected to /dross-verify for a stale pass. This goes beyond the letter of c-6, but without it status sends the user straight into ship's refusal.
- Helper file is named verdict_fresh.go, not staleness.go. "Staleness" already names the survivor lifecycle (appendStalenessNotes) in the same package.
- t-3 → t-4 are sequenced (waves 2→3), not run in parallel: both edit internal/cmd/verify.go, and t-4 relies on t-3's finishVerify drift naming and measuredTreeFn seam.
- --force-unverified usage text is updated, with the cli_tree golden in t-5. Rejected leaving help saying "verify must be pass" now that it also overrides staleness (locked stale_override forbids a new flag).
- Accepted residual: ship's red-proof repoint commits a non-exempt doc, so a re-ship after a repoint reads stale and needs a re-verify or --force-unverified. The locked staleness_exemptions forbids exempting it.
- Accepted residual: adapter output in non-ignored dirs (reports/) stales every run. It is named with a .gitignore hint, not exempted (locked).
- Out of scope: `dross milestone complete`'s own pass gate stays blind to freshness. The spec names only `dross ship`.
