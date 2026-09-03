# lane-selector-preview — risk lens

Phase lane-selector-preview — 6 tasks across 3 waves

Lens: every task owns exactly one way this feature can lie. The four lies
available here are (1) preview and the gate deriving different lines,
(2) preview spawning something, (3) preview inheriting the gate's refusals as
exit codes, (4) preview reporting a locality it never established. Each has one
owner below.

## Wave 1

```
t-1  Extract spawn-free lane resolution from the gate
     files:    internal/cmd/lane_plan.go, internal/cmd/test.go,
               internal/cmd/lane_plan_test.go
     covers:   c-1, c-2
     depends:  —
     what:     New lanePlan(repoDir, proj, files) -> lanePlan{OutOfTree,
               Unmatched, Lanes[]{Name, Index, Line, Selector, Dropped,
               ScopedToNothing, FenceErr}} holding the whole of runTestLanes'
               resolution: testlane.Select, the up-front shArgvFor/Derive/Expand
               fence, the os.Stat existence filter and the laneRunLine
               derivation. It returns DATA, never an *ExitCodeError, and it
               touches no seam. runTestLanes is rewritten to consume it and to
               turn that data into the refusals and exit codes it already
               produces; laneRunLine's os.Stat filter moves into the plan and
               now records what it dropped instead of silently shortening the
               line.
     contract: - TestPlanLineIsTheSpawnedLine: for a lane with
                 selector="go-package" over three paths, the recorded spawnLocal
                 argv from `dross test --files ...` is byte-identical to
                 plan.Lanes[0].Line. Re-deriving the line anywhere but lanePlan
                 makes this fail.
               - TestDeletedSelectorPathIsRecordedAsDropped: a lane matched by
                 two paths, one of which is not on disk, yields
                 Dropped=["<that path>"] and a Line derived from the survivor
                 only. If the existence filter moves back out of the plan, the
                 field goes empty and this fails.
               - TestEverySelectorPathGoneScopesToNothing: with both matched
                 paths deleted, ScopedToNothing is true, Line is "" — and the
                 gate still prints its "selector miss" line and counts a miss
                 (existing test_lane_miss_test.go stays green).
               - TestGateExitCodesSurviveTheExtraction: `dross test --files`
                 with an out-of-tree path still exits 2 and with an unmatched-
                 only set still exits 5 (existing tests in test_lane_test.go).
                 The plan reporting them as data must not soften the gate.
               - TestMalformedTemplateIsFencedBeforeAnySpawn: a lane whose
                 selector_template carries no placeholder puts a FenceErr on
                 that lane and records zero spawnLocal calls.

t-2  Print selector_template and selector_join in lane list
     files:    internal/cmd/test_lane.go, internal/cmd/test_lane_test.go
     covers:   c-4
     depends:  —
     what:     testLaneList calls the existing printLaneTemplate (today only
               `lane add` / `lane edit` render those two fields) directly after
               its `selector:` line, so the listing shows every field that
               shapes the derived line.
     contract: - TestListPrintsTemplateAndJoin: a lane declared with
                 --selector-template "-R {paths}" --selector-join "|" prints
                 both `selector-template:` and `selector-join:` lines under its
                 `selector:` line.
               - TestListOmitsTemplateFieldsForALaneWithout: a lane with a bare
                 --selector prints neither key — no `selector-template: -`
                 placeholder, matching every other opt-in field in the listing.
               - TestListAndAddRenderTemplatesIdentically: the two lines `lane
                 list` prints are the exact lines `lane add` echoes, asserted
                 against the same lane; if list grows its own renderer instead
                 of calling printLaneTemplate, the strings diverge and this
                 fails.

t-3  Derive the bare preview file set from the working tree
     files:    internal/cmd/worktree_files.go,
               internal/cmd/worktree_files_test.go
     covers:   c-1
     depends:  —
     what:     worktreeChangedFiles(repoDir) ([]string, error) — the file set a
               bare `preview` uses. `git status --porcelain --untracked-files=all`
               parsed through the existing porcelainPaths/unquotePath helpers in
               cleantree.go, returning repo-relative paths, deduped, in git's
               order. Rename lines contribute the DESTINATION path only.
               Deletions are kept, so preview can name them as dropped rather
               than hiding them.
     contract: - TestUntrackedDirectoryIsExpandedToItsFiles: a new directory
                 holding two untracked .go files returns both file paths, not
                 git's collapsed `dir/` entry. Dropping -uall makes the set a
                 directory path that matches no lane glob, which would report
                 "no lane matches dir/" for files that do match — this test
                 fails on that.
               - TestRenameContributesOnlyTheDestination: an `R  old.go ->
                 new.go` line yields ["new.go"]; old.go is not returned (it is
                 not on disk and would surface as a phantom dropped path).
               - TestDeletedFileIsStillInTheSet: a staged deletion appears in
                 the set, because c-2 requires preview to name it.
               - TestQuotedPathIsUnquoted: a tracked-modified file whose name
                 contains a space comes back unquoted and without git's
                 surrounding quotes.
               - TestCleanTreeReturnsAnEmptySetNotAnError: a clean repo returns
                 (nil, nil) — a caller must be able to print "0 files" and exit
                 0 rather than raise.
```

## Wave 2 (depends t-1, t-3)

```
t-4  Add `dross test lane preview` with findings and --json
     files:    internal/cmd/lane_preview.go, internal/cmd/test_lane.go,
               internal/cmd/lane_preview_test.go
     covers:   c-1, c-2, c-5
     depends:  t-1, t-3
     what:     New `preview` subcommand under the `lane` group: cobra.NoArgs,
               --files (repeatable), --lane <name>, --json. It calls lanePlan
               and nothing else that can execute — no requireExecConsent, no
               resolveTestTarget, no syncTreeTo, no runOneLane. Bare invocation
               takes worktreeChangedFiles and prints how many files it took.
               Declares the full previewReport JSON struct — lanes[] (name,
               line, selector, dropped[], scoped_to_nothing, consent, locality),
               unmatched[], out_of_tree[], files_taken — and fills every field
               except consent/locality, which t-5 populates through the same
               struct. Exit status is 0 for every finding; only an unreadable
               project.toml and an unknown --lane name are non-zero.
     contract: - TestPreviewSpawnsNothing: with spawnLocal, spawnRemote and
                 remoteProbeFn replaced by recorders, a preview over a file set
                 hitting a lane that declares a prepare records zero spawnLocal
                 and zero spawnRemote calls. Any route into runOneLane,
                 runLanePrepare or syncTreeTo fails this.
               - TestPreviewAndGateAgreeOnTheLine: the line preview prints for a
                 file set is the exact string `dross test --files <same set>`
                 hands to spawnLocal, asserted in one test over one fixture.
               - TestPreviewExitsZeroWhereTheGateRefuses: over a path outside
                 the repo, preview exits 0 and prints an out-of-tree line while
                 `dross test --files` on the same path exits 2; over a file set
                 matching no lane, preview exits 0 while the gate exits 5.
               - TestUnknownLaneNameIsAFault: `preview --lane nope` exits
                 non-zero and names the declared lanes.
               - TestPreviewNamesTheFourNonRunningOutcomes: one invocation whose
                 file set carries an unmatched path, an out-of-tree path, a
                 path not on disk and a lane whose selector scoped to nothing
                 prints a line naming each, each with its reason.
               - TestPreviewJSONIsBareAndCarriesTheFindings: --json output's
                 first non-space byte is '{' (assertBareJSONDocument), and the
                 document's lanes[0].line, dropped[], unmatched[] and
                 out_of_tree[] equal the human rendering's facts.
               - TestBarePreviewTakesTheWorkingTree: with one modified file in
                 the tree and no --files, preview reports "1 file" and previews
                 the lane that file hits.
```

## Wave 3 (depends t-4)

```
t-5  Report each lane's consent state and where it would run
     files:    internal/cmd/lane_preview_locality.go, internal/cmd/lane_preview.go,
               internal/cmd/lane_preview_locality_test.go
     covers:   c-3, c-6
     depends:  t-4
     what:     Adds --no-probe and fills the consent/locality fields of
               previewReport. Consent: LaneConsented(root, repoDir, name,
               laneConsentLine(lane)) read for its ConsentState only — the
               ConsentState.String() value is rendered and the error is
               discarded, so an ungranted lane annotates rather than refuses.
               Locality: readRemoteGrants + remoteProbeFn directly, NOT
               resolveTestTarget — that one prints its own fallback line and
               collapses an unreachable host into a nil target, which is exactly
               the "unresolved" case c-6 needs kept apart from "local". Verdicts:
               remote <host> / local (naming the absent binary, via the existing
               absentTools + testlane.Toolchain) / unprobed (--no-probe) /
               unresolved (<host>, probe transport failure). Every one exits 0.
     contract: - TestUngrantedLaneIsAnnotatedNotRefused: a lane with no grant
                 renders `consent: absent`, its derived line is still printed,
                 and preview exits 0 — while `dross test --files` over the same
                 lane exits 6.
               - TestStaleAndGrantedAreDistinguished: a lane whose command
                 changed after `dross trust --lane` renders `consent: stale`,
                 not `absent`; the granted lane beside it renders `granted`.
               - TestUnreachableHostIsUnresolvedNotLocal: with remoteProbeFn
                 returning a transport error, the lane renders
                 `locality: unresolved (helicon)` and preview exits 0. A verdict
                 of `local` here would claim a fallback the probe never proved.
               - TestNoProbeAsksNothing: with --no-probe, remoteProbeFn records
                 zero calls and the lane renders `locality: unprobed (helicon)`.
               - TestMissingRemoteToolFallsBackNamingTheBinary: with the probe
                 reporting `pnpm` missing, the lane renders `locality: local`
                 naming pnpm, matching what laneFallbackLine would say at run
                 time.
               - TestJSONCarriesConsentAndLocality: --json's lanes[0].consent
                 and lanes[0].locality hold the same two values the human
                 rendering printed for that lane.

t-6  Document preview and the listing's template fields
     files:    README.md, internal/cmd/lane_preview_docs_test.go
     covers:   c-4
     depends:  t-2, t-4
     what:     Extends the single `dross test lane {add,list,edit,remove,install}`
               row (README.md:220) to `{...,preview}`: the verb's invocation, its
               bare working-tree default, --lane, --no-probe, --json, the
               always-0 exit policy, and the sentence that `lane list` now prints
               selector-template and selector-join. One task owns that row so two
               parallel edits cannot collide on it.
     contract: - TestReadmeDocumentsThePreviewVerb: the README row containing
                 "dross test lane" names `preview`, `--no-probe` and `--json`;
                 a verb shipped undocumented fails here, the same shape as the
                 existing TestReadmeDocumentsTheLaneInstallVerb.
               - TestReadmeDocumentsTheListingTemplateFields: that row names
                 `selector-template` and `selector-join` as fields `lane list`
                 prints.
               - TestPreviewExitPolicyIsDocumented: the row states preview exits
                 0 for "no lane matches" — the one sentence that stops it being
                 wired as a CI gate.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (same-code-path derivation, spawns nothing) | t-1, t-3, t-4 |
| c-2 (names the non-running outcomes) | t-1, t-4 |
| c-3 (consent state named, never refused) | t-5 |
| c-4 (`lane list` prints template + join) | t-2, t-6 |
| c-5 (`--json` carries the same facts) | t-4, t-5 |
| c-6 (locality: host / local+binary / unresolved) | t-5 |

## Judgment calls

- **Extraction before the command (t-1 in wave 1), not preview calling
  runTestLanes.** Rejected: having preview call the gate behind a `dryRun bool`.
  A boolean that has to be honoured at four separate spawn sites is one missed
  `if` away from preview running a suite; a function that returns data and holds
  no seam cannot spawn at all. It also makes c-1's guarantee a differential
  assertion over one shared string rather than a claim.
- **The plan returns data, the gate turns data into exit codes.** Rejected:
  the plan returning *ExitCodeError as runTestLanes does today. Preview would
  then have to catch and downgrade codes 2/5/6, and every future code added to
  the gate would silently leak into preview's exit status. Locked
  preview_exit_status becomes structural instead of a filter someone maintains.
- **Preview probes through readRemoteGrants + remoteProbeFn, not
  resolveTestTarget.** Rejected: reusing resolveTestTarget for symmetry with the
  gate. It prints "remote: <why>" of its own and returns a nil target for an
  unreachable host — which preview would have to render as `local`, the one
  answer c-6 says must be `unresolved`. The derivation shared with the gate is
  the LINE (t-1); the locality answer is deliberately preview's own because the
  gate's version destroys the distinction.
- **Consent read for its state, error discarded (t-5).** Rejected: rendering
  laneConsentRefusal's text. That text is written as a refusal ("refusing to
  run…") and would read as though preview had blocked something. The
  ConsentState ladder already has the five words c-3 needs.
- **Working-tree collection is its own wave-1 task (t-3), not a helper inside
  the command.** Its failure modes — git's collapsed untracked directory, a
  rename's two paths, a deletion, a quoted path — are all silent: each produces
  a plausible-looking but wrong file set, and the wrongness surfaces as a lane
  that "does not match" rather than as an error. That earns an owner and a test
  file of its own, and it parallelises with t-1.
- **`-uall` for the preview file set, while cleantree keeps plain
  `--porcelain`.** Rejected: changing gitStatusRaw. Its caller
  (autoCommitDrossDirt) only asks whether dirt is under .dross/, where the
  collapsed form is correct and cheaper; widening it would change the
  auto-commit gate for no reason.
- **t-2 kept standalone despite being small.** Merging it into t-6 (the only
  other c-4 task) would mix a code change into a docs commit; merging it into
  t-4 would put an unrelated verb's rendering inside the preview commit. It is
  the only task touching testLaneList, which is the point.
- **t-6 depends on t-4, not t-5.** `--no-probe` is named literally in the locked
  locality_probe decision, so the row can be written from the spec before the
  probe lands. If t-5 renames the flag, t-6's grep fails loudly rather than
  silently — which is the behaviour I want from a docs gate anyway.
- **JSON declared in t-4 and filled in t-5, rather than a wave-4 --json task.**
  The struct is the stable shape c-5 asks for; declaring it once, in the task
  that owns the surface, stops the shape being retrofitted around whatever t-5
  happened to produce.
