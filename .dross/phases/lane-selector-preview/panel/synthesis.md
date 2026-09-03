# lane-selector-preview — panel synthesis

Judged cold: I authored none of the three drafts. Every file path, helper and
cited existing test in all three was checked against the tree — `laneRunLine`
(test.go:689), `printLaneTemplate` (test_lane.go:689), `porcelainPaths`
(cleantree.go:64), `laneLocality`/`laneFallbackLine`/`absentTools`
(lane_locality.go), `LaneConsented`/`laneConsentLine`/`findLane` (trust.go),
`emitJSON`/`jsonFlagUsage` (jsonout.go), `readmeRow` /`readmeRowContaining`,
`preflightRemote`, `remoteProbeFn`, `testlane.Select`, and the README row at
README.md:220. No draft references anything that does not exist. Nothing below
is invented; every task traces to at least one draft.

## Scores

| Draft | Criteria coverage | Test-contract specificity | Granularity | Wave correctness |
|---|---|---|---|---|
| **risk** (6/3) | All six; c-4 owned twice (code t-2, docs t-6); coverage table honest but one-directional | **Strongest** — named tests plus an explicit "what breaks this" per arm, and the only draft whose c-1 proof is a *byte-identity differential* over one shared string | Mixed — t-2/t-3/t-6 tight, but t-1 (whole resolution lift) and t-4 (command + findings + full JSON struct, 3 criteria) are heavy | 3 waves, deps sound; t-3 correctly parallelised into wave 1; t-6→t-4 (not t-5) deliberate and defended |
| **mvp** (5/3) | All six, leanest mapping; c-5 rests on one task | **Weakest** — mostly prose, two named tests; leans on existing tests staying green rather than new named assertions | Worst — t-5 carries cobra wiring + working-tree collection + human render + JSON + README across 5 criteria | 3 waves, deps correct, but the riskiest parsing (porcelain untracked dirs) hides inside the largest task instead of owning a wave-1 slot |
| **verification** (7/4) | **Strongest** — the only bidirectional table (every criterion has a task *and* every task a criterion), no orphan work | Near-equal to risk; edges ahead on the JSON arms (`assertBareJSONDocument`, empty-match payload) and on `readmeRow` per-row assertion; adds the siteRefused-*carried*-not-returned arm | **Finest** — 7 tasks, none over two criteria | 4 waves, and the fourth is the weak point: JSON is retrofitted last onto whatever t-6 produced, when serialisation has no real dependency on the consent/locality *values* |

**Skeleton: risk.** It has the sharpest contracts, the correct 3-wave shape, and
it is the only draft that makes locked `preview_exit_status` *structural* — a
plan returning data that holds no spawn seam cannot leak an exit code or spawn a
suite, where mvp's and verification's caller-keeps-policy framing still relies on
preview not re-raising what it was handed. verification is the close runner-up
and supplies most of the grafts below.

## Merged plan

7 tasks across 3 waves. Origin tags mark where each task and each grafted arm
came from.

### Wave 1

```
t-1  Extract spawn-free lane resolution, name dropped paths          [risk, grafts: mvp, verification]
     files:    internal/cmd/lane_plan.go, internal/cmd/test.go,
               internal/cmd/lane_plan_test.go
     covers:   c-1, c-2
     depends:  —
     what:     lanePlan(repoDir, proj, files) -> lanePlan{OutOfTree, Unmatched,
               Lanes[]{Name, Index, Line, Selector, Dropped, ScopedToNothing,
               FenceErr}} holding the whole of runTestLanes' resolution:
               testlane.Select, the up-front shArgvFor/Derive/Expand fence, the
               os.Stat existence filter and the laneRunLine derivation. It
               returns DATA, never an *ExitCodeError, and holds no spawn seam.
               laneRunLine (test.go:689) is widened so the existence filter
               reports what it removed [mvp+verification]; runTestLanes is
               rewritten to consume the plan and keeps its refusal policy
               exactly where it is today.
     contract: - TestPlanLineIsTheSpawnedLine [risk]: for a selector="go-package"
                 lane over three paths, the recorded spawnLocal argv from
                 `dross test --files ...` is byte-identical to plan.Lanes[0].Line.
                 Re-deriving the line anywhere but lanePlan fails it.
               - TestLaneRunLineNamesDroppedPaths [risk+mvp+verification]: a lane
                 matched by two paths, one not on disk, yields Dropped==[that path]
                 and a Line scoped to the survivor only. If the filter stops
                 recording drops, c-2 becomes unimplementable.
               - TestEverySelectorPathGoneScopesToNothing [risk]: with both paths
                 deleted, ScopedToNothing is true and Line is "".
               - TestResolveReportsOutOfTreeAsData [verification]: `--files /abs/x.go`
                 lands in OutOfTree with a NIL error — if exitBadFileSet moves into
                 the shared function, preview can never honour locked
                 preview_exit_status.
               - TestMalformedTemplateIsFencedBeforeAnySpawn [risk]: a lane whose
                 selector_template carries no placeholder puts FenceErr on that lane
                 and records zero spawnLocal calls.
               - Gate unchanged by the extraction [verification, named]:
                 TestSelectorScopesTheSpawnedLine, TestTemplatedLaneSpawnsTheExpandedLine,
                 TestDeletedPathsAreDroppedFromTheSelector,
                 TestALaneWhoseEveryPathIsGoneDoesNotSpawnAndIsNotGreen,
                 TestASelectorlessLaneIsStillByteIdentical and TestPartialMissNamesTheRest
                 all stay green byte-for-byte; `dross test --files` still exits 2
                 out-of-tree and 5 on an unmatched-only set [risk].

t-2  Print selector_template and selector_join in lane list          [risk+mvp+verification]
     files:    internal/cmd/test_lane.go, internal/cmd/test_lane_list_fields_test.go
     covers:   c-4
     depends:  —
     what:     testLaneList calls the existing printLaneTemplate (today only
               `lane add`/`lane edit` render those two fields) directly after its
               `selector:` line, so every field shaping the derived line is
               readable from the listing. No second renderer.
     contract: - TestLaneListPrintsTemplateFields [all three]: a lane declaring
                 --selector-template "--package {path}" --selector-join "|" prints
                 `selector-template: --package {path}` and `selector-join: |`;
                 deleting either Printf fails it.
               - TestListOmitsTemplateFieldsForALaneWithout [all three]: a lane
                 with a bare --selector prints NEITHER key — no
                 `selector-template: -` placeholder, matching the opt-in
                 field-hiding every other optional field in the listing follows.
               - TestListAndAddRenderTemplatesIdentically [risk]: the two lines
                 `lane list` prints are the exact lines `lane add` echoes for the
                 same lane; a list-local renderer makes the strings diverge.

t-3  Derive the bare preview file set from the working tree          [risk+verification]
     files:    internal/cmd/worktree_files.go, internal/cmd/worktree_files_test.go
     covers:   c-1
     depends:  —
     what:     worktreeChangedFiles(repoDir) ([]string, error) — the file set a
               bare `preview` uses (locked bare_preview_default). `git status
               --porcelain --untracked-files=all` parsed through the existing
               porcelainPaths/unquotePath (cleantree.go), returning repo-relative
               paths, deduped, in git's order. gitStatusRaw is left alone — its
               only caller (autoCommitDrossDirt) wants the collapsed cheap form.
               Deletions are KEPT, so preview can name them as dropped rather
               than hide them.
     contract: - TestWorkingTreeFilesExpandsUntrackedDirectories [risk+verification]:
                 a new directory holding two untracked .go files returns both FILE
                 paths, not git's collapsed `dir/`. Dropping -uall reports "no lane
                 matches dir/" for files that do match — the most common bare
                 invocation would silently preview nothing.
               - TestDeletedFileIsStillInTheSet [risk+verification]: a staged
                 deletion appears in the set, because c-2 requires preview to name
                 it; filtering at collection makes it unnameable at derivation.
               - TestQuotedPathIsUnquoted [risk+verification]: a path with a space
                 comes back unquoted, matching what `--files` would have been handed.
               - TestRenameContributesOnlyTheDestination [risk]: an `R  old.go ->
                 new.go` line yields ["new.go"]. **See Disagreement 2.**
               - TestCleanTreeReturnsAnEmptySetNotAnError [risk]: a clean repo
                 returns (nil, nil) so a caller prints "0 files" and exits 0.

t-4  Preview host: probed, unprobed, unresolved                      [mvp+verification]
     files:    internal/cmd/lane_preview_locality.go,
               internal/cmd/lane_preview_locality_test.go
     covers:   c-6
     depends:  —
     what:     previewHost resolves where lanes would run without committing to a
               run: readRemoteGrants, then probe through preflightRemote (the same
               remoteProbeFn seam doctor and the run use) unless probing is off,
               classifying the answer none/probed/unprobed/unresolved. Per-lane
               verdicts come from the EXISTING laneLocality, called with host=""
               whenever the host is unprobed or unresolved — no new laneSite enum
               values, so no switch in the run path silently accepts one
               [verification]. It never syncs the tree.
     contract: - TestUnreachableHostIsUnresolvedNotAFailure [all three]: with the
                 probe seam returning remote.ErrTransport, State=="unresolved", the
                 host is named, and the error is NIL. Returning the transport
                 failure would turn preview into the non-zero verdict locked
                 preview_exit_status forbids. A verdict of `local` here would claim
                 a fallback the probe never proved [risk].
               - TestNoProbeNeverOpensAConnection [all three]: with probing off, a
                 call-counting remoteProbeFn records 0 calls and the host is
                 reported `unprobed`.
               - TestMissingRemoteToolFallsBackNamingTheBinary [all three]: with the
                 probe reporting pnpm missing, the web lane is siteLocal carrying the
                 laneFallbackLine that names pnpm, helicon and `dross test lane
                 install web`, while the go lane is siteRemote. A preview that
                 re-derived locality instead of calling laneLocality fails on that
                 exact wording [verification].
               - TestRefusedLaneCarriesItsErrorRatherThanReturningIt [verification]:
                 a lane whose tool is on neither machine comes back siteRefused with
                 its exitToolchainMissing error CARRIED on the verdict, not returned.
```

### Wave 2 (depends t-1, t-3)

```
t-5  Add `dross test lane preview` with findings and --json          [risk, grafts: mvp, verification]
     files:    internal/cmd/lane_preview.go, internal/cmd/test_lane.go,
               internal/cmd/lane_preview_test.go
     covers:   c-1, c-2, c-5
     depends:  t-1, t-3
     what:     New `preview` subcommand under the `lane` group: cobra.NoArgs,
               --files (repeatable), --lane <name> (narrowing via findLane), --json.
               No lane positional (locked preview_invocation). It calls lanePlan and
               nothing else that can execute — no requireExecConsent, no
               resolveTestTarget, no syncTreeTo, no runOneLane, no runLanePrepare.
               Bare invocation takes worktreeChangedFiles and says how many files it
               took. Declares the FULL previewReport struct — lanes[] (name, line,
               selector, dropped[], scoped_to_nothing, fence_err, consent, locality),
               unmatched[], out_of_tree[], files_taken — and fills every field except
               consent/locality, which t-6 populates through the same struct, so the
               stable shape c-5 asks for is not retrofitted around whatever t-6
               happened to produce [risk]. Exit 0 on every finding; non-zero only for
               an unreadable project.toml and an unknown --lane name.
     contract: - TestPreviewSpawnsNothing [all three]: with spawnLocal, spawnRemote and
                 remoteProbeFn replaced by recorders, a preview over a file set hitting
                 a lane that declares a --prepare records zero spawnLocal calls, zero
                 remote spawn events and no rsync in the runLog [verification's three
                 arms]. Any route into runOneLane/runLanePrepare/syncTreeTo fails it.
               - TestPreviewLineIsTheLineTheGateWouldSpawn [risk+verification]: one
                 fixture, two invocations — `dross test --files internal/cmd/test.go`
                 under installSpawnRecorder records line L; preview over the same set
                 prints a line whose command text equals L exactly. This is c-1's
                 same-code-path guarantee made observable.
               - TestPreviewExitsZeroWhereTheGateRefuses [risk]: over an out-of-tree
                 path preview exits 0 and prints an out-of-tree line while the gate
                 exits 2; over an unmatched-only set preview exits 0 while the gate
                 exits 5.
               - TestPreviewNamesEveryNonRunningOutcome [risk+verification]: ONE
                 invocation carrying an unmatched in-tree path, an out-of-tree path, a
                 matched-but-deleted path and a selector lane whose every path is gone
                 prints four distinct findings, each naming the path AND its reason.
                 A silent omission of any one fails.
               - TestMalformedLaneIsAFindingNotAFault [verification]: a lane carrying
                 t-1's FenceErr is rendered with its problem named; preview exits 0.
               - TestUnknownLaneNameIsAFault [risk+mvp+verification]: `--lane nope`
                 exits non-zero and its message lists the declared lane names
                 (findLane's own refusal); `--lane go` on a set also hitting the docs
                 lane renders the go lane only [verification].
               - TestBarePreviewTakesTheWorkingTree [all three]: with one untracked
                 internal/new/a.go and no --files, preview says it took 1 file and
                 names the go lane.
               - TestPreviewJSONIsBareAndCarriesTheFindings [risk+verification]:
                 assertBareJSONDocument holds (first non-space byte '{', no `# <path>`
                 header, parses), and lanes[0].line, dropped[], unmatched[] and
                 out_of_tree[] equal the human rendering's facts. --json gets its own
                 usage string, not jsonFlagUsage — that constant promises "instead of
                 toml", and preview has no toml rendering [verification].
               - TestPreviewJSONReportsAnEmptyMatchWithoutFailing [verification]:
                 --json on an unmatched-only set emits an object with an empty lanes
                 array and the unmatched paths present, and exits 0. A bare `{}` or a
                 non-zero exit fails.
```

### Wave 3 (depends t-4, t-5)

```
t-6  Annotate previewed lanes with consent and locality             [all three]
     files:    internal/cmd/lane_preview.go,
               internal/cmd/lane_preview_consent_test.go
     covers:   c-3, c-6
     depends:  t-4, t-5
     what:     Adds --no-probe and fills the consent/locality fields of
               previewReport. Consent: LaneConsented(root, repoDir, name,
               laneConsentLine(lane)) — the same call the gate makes — read for its
               ConsentState only; the ConsentState.String() value is rendered and
               the error is DISCARDED, so an ungranted lane annotates rather than
               refuses. laneConsentRefusal's text is never rendered: it reads as
               though preview blocked something [risk]. Locality: t-4's previewHost
               verdicts. Both reported, never acted on.
     contract: - TestPreviewRendersAnUngrantedLaneWithoutRefusing [all three]: a lane
                 with no grant renders `consent: absent`, its derived line still
                 prints, preview exits 0 and spawns nothing — while `dross test
                 --files` over the same lane refuses. Reusing runTestLanes'
                 laneConsentRefusal path fails it.
               - TestStaleAndGrantedAreDistinguished [risk+verification]: a lane whose
                 command changed after `dross trust --lane` renders `stale`, not
                 `absent`; the granted lane beside it renders `granted`. Asserted
                 against ConsentState.String() so a hand-written second vocabulary
                 fails [verification].
               - TestUnreachableHostStillExitsZero [all three]: with the probe seam in
                 transport failure every lane renders unresolved (naming the host) and
                 preview exits 0.
               - TestNoProbeAsksNothing [all three]: with --no-probe the probe counter
                 is 0 and lanes report the configured host as unprobed.
               - TestJSONCarriesConsentAndLocality [risk+verification]: --json's
                 lanes[].consent and lanes[].locality hold the SAME strings the human
                 rendering printed (ConsentState.String(), the site name) — a payload
                 inventing `"consent": "ok"` fails the comparison.

t-7  Document preview and the listing's template fields             [risk]
     files:    README.md, internal/cmd/lane_preview_docs_test.go
     covers:   c-4
     depends:  t-2, t-5
     what:     Extends the single `dross test lane {add,list,edit,remove,install}`
               row (README.md:220) to `{...,preview}`: the verb's invocation, its
               bare working-tree default, --lane, --no-probe, --json, the always-0
               exit policy, and the sentence that `lane list` now prints
               selector-template and selector-join. ONE task owns that row so two
               parallel edits cannot collide on it.
     contract: - TestReadmeDocumentsThePreviewVerb [risk]: the row containing
                 "dross test lane", located per-row via readmeRow, names `preview`,
                 `--no-probe` and `--json` — same shape as the existing
                 TestReadmeDocumentsTheLaneInstallVerb. Per-row assertion so the claim
                 cannot be satisfied by a mention three commands away [verification].
               - TestReadmeDocumentsTheListingTemplateFields [risk+verification]: that
                 row names `selector-template` and `selector-join` as fields
                 `lane list` prints.
               - TestPreviewExitPolicyIsDocumented [risk]: the row states preview
                 exits 0 for "no lane matches" — the one sentence that stops it being
                 wired as a CI gate.
```

### Coverage

| Criterion | Tasks |
|---|---|
| c-1 same-code-path derivation, spawns nothing | t-1, t-3, t-5 |
| c-2 names the non-running outcomes | t-1, t-5 |
| c-3 consent state named, never refused | t-6 |
| c-4 `lane list` prints template + join | t-2, t-7 |
| c-5 `--json` carries the same facts | t-5, t-6 |
| c-6 host / local+binary / unresolved | t-4, t-6 |

Bidirectional [verification's discipline]: every criterion has a task, and every
task carries at least one criterion — including t-7, which rides the surfaces
t-2 and t-5 create.

## Disagreements

### 1. Shape of the shared extraction: new plan struct vs widened `laneRunLine`

- **risk**: lift the whole of `runTestLanes`' resolution into a new
  `lanePlan(repoDir, proj, files)` returning a struct; the existence filter moves
  out of `laneRunLine` into the plan.
- **mvp**: do NOT lift anything. Change `laneRunLine` in place from
  `(string, []string, bool)` to a `laneDerivation{Line, Selector, Dropped, Reason, OK}`;
  `runTestLanes` reads `.Line/.Selector/.OK` and prints what it prints today.
  One signature change, no new file, blast radius confined to test.go.
- **verification**: both — lift `Select` → `matchedLane` → fence into
  `resolveLanes` *and* widen `laneRunLine` to `(line, args, dropped, ok)`.

**Provisional default: risk + verification — extract the plan AND widen
laneRunLine.** Why it matters: this is the single biggest structural bet in the
phase and it is what c-1 actually turns on. mvp's in-place widening is the
smallest diff and genuinely lower-risk against a 300-line `runTestLanes`
(test.go:365–660), but it leaves preview needing its own copy of the
`Select` → out-of-tree → unmatched → fence sequence, which is exactly the
divergence c-1 forbids — it would make c-1 a drift test rather than a structural
guarantee. If t-1 proves too large in execution, mvp's version is the documented
fallback and only costs preview a duplicated resolution prologue.

### 2. Renames in the bare working-tree file set

- **risk**: destination path only. `old.go` is not on disk and would surface as a
  phantom "dropped" path on every rename.
- **verification**: BOTH sides, explicitly, "so the gone half reaches t-1's
  dropped report instead of vanishing".
- **mvp**: silent — names only the untracked-directory hazard.

**Provisional default: risk (destination only).** Why it matters: the two drafts
assert opposite test outcomes for the same input, so whichever is chosen pins a
test that the other would fail. Note the cost asymmetry, which neither draft
states: `porcelainPaths` (cleantree.go:64–75) **already returns both sides** for
`R`/`C` lines, so verification's behaviour is free and risk's needs an added
filter. I still default to risk because the bare file set is *inferred*, not
asked for — reporting "dropped: old.go (not on disk)" on every rename is noise
about a path the user never named, and c-2's naming duty is owed to paths the
caller supplied. This is the divergence most likely to be worth reversing if the
lead disagrees; it is a two-line change either way.

### 3. Where the `--json` shape is decided

- **risk**: declare the complete `previewReport` struct in the preview task (t-5),
  fill consent/locality later through the same struct.
- **verification**: a dedicated wave-4 task (t-7) owning `--json` and the README.
- **mvp**: folded into its single mega-task t-5.

**Provisional default: risk.** Why it matters: it is the difference between a
3-wave and a 4-wave plan. verification's separate JSON task buys the sharpest
JSON contracts (grafted into t-5 above regardless) but retrofits the stable shape
c-5 asks for around whatever the consent task produced, and adds a wave for
serialisation that depends only on field *presence*, not field *values*.

### 4. Which API preview probes locality through

- **risk**: `readRemoteGrants` + `remoteProbeFn` **directly**, with preview's own
  four-verdict vocabulary. Explicitly rejects `resolveTestTarget` (it prints its
  own fallback line and collapses an unreachable host into a nil target — which
  preview would have to render as `local`, the one answer c-6 says must be
  `unresolved`).
- **mvp**: `readRemoteGrants` + `selectRemoteTarget` + `laneLocality`.
- **verification**: `preflightRemote` (which itself goes through `remoteProbeFn`,
  remote_preflight.go:55) + the existing `laneLocality` called with `host=""` for
  unprobed/unresolved, and **no new `laneSite` enum values**.

**Provisional default: verification.** This overrides the skeleton. Why it
matters: all three agree `resolveTestTarget` is wrong; the live question is
whether preview re-derives per-lane verdicts or calls `laneLocality`. Calling it
keeps one derivation of "would this lane run here", which is what makes the
grafted `TestMissingRemoteToolFallsBackNamingTheBinary` assert the *exact*
`laneFallbackLine` wording the run would print. risk's own vocabulary would give
doctor, the run and preview three answers to one question. risk's substantive
point survives: unprobed/unresolved must stay OFF the `laneSite` enum and live on
the `previewHost` result, which is verification's design too.

### 5. Wave placement of the locality resolver

- **risk**: no wave-1 locality task; the probe lands inside the wave-3 annotation
  task (its t-5).
- **mvp** and **verification**: its own wave-1 task, independent of the plan
  extraction.

**Provisional default: mvp + verification (wave 1).** Why it matters: it is the
only network-touching piece and depends on nothing else in the phase, so parking
it in wave 3 serialises work that can run in parallel and puts two criteria
(c-3 and c-6) plus a flag plus a new file in one task. Splitting it is what takes
the merged skeleton from risk's 6 tasks to 7.

### 6. Who owns the README row

- **risk**: a single dedicated task (t-7 here), explicitly so two parallel edits
  cannot collide on one row.
- **mvp**: folded into its mega-task; "documentation traces to no criterion".
- **verification**: split across TWO tasks — its t-3 (list fields) and its t-7
  (the preview verb) both edit README.md.

**Provisional default: risk.** Why it matters: README.md:220 is one enormous
single-line table row. verification's split has two tasks rewriting that same
line, which is a guaranteed conflict if they land in different waves and a merge
hazard if they land in the same one. mvp's objection is answered by mapping the
docs task to c-4, which explicitly says the fields must be "readable".

### 7. A malformed lane: finding or fault

- **verification**: explicit — a lane with a bad selector style or template is a
  *finding*; preview names it and exits 0, the same treatment c-3 mandates for an
  ungranted lane.
- **risk**: records `FenceErr` on the lane in t-1 but never states it is rendered
  as a preview finding.
- **mvp**: silent.

**Provisional default: verification — render it, exit 0.** Why it matters: the
gate's up-front fence *refuses* on a malformed lane, so without an explicit
rendering rule the obvious implementation inherits that refusal and breaks locked
`preview_exit_status` on a case no other contract covers. Cheap to add now
(`TestMalformedLaneIsAFindingNotAFault` in t-5), expensive to discover at verify.
