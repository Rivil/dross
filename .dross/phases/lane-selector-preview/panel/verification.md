# lane-selector-preview — verification lens

Phase lane-selector-preview — 7 tasks across 4 waves

Every task below was written by first drafting the test that proves its criterion,
then cutting the task down to the smallest change that makes that test satisfiable.

Wave 1
  t-1  Extract shared lane resolution, name dropped paths
       files:    internal/cmd/lane_resolve.go, internal/cmd/test.go,
                 internal/cmd/lane_resolve_test.go
       covers:   c-1, c-2
       desc:     Lift Select → matchedLane → per-lane fence out of runTestLanes into
                 resolveLanes, returning Unmatched/OutOfTree/Matched as DATA. Widen
                 laneRunLine to `(line, args, dropped []string, ok bool)` so the
                 existence filter reports what it removed. runTestLanes calls the
                 extracted functions; its refusal policy stays where it is.
       contract: - resolveLanes on `--files /abs/x.go` returns that path in OutOfTree
                   with a nil error: if the exitBadFileSet refusal moves into the
                   shared function, TestResolveLanesReportsOutOfTreeAsData fails and
                   preview could never honour locked preview_exit_status.
                 - resolveLanes on an in-tree path matching no lane returns it in
                   Unmatched with a nil error and a non-empty Matched for the paths
                   that did hit — the same split TestPartialMissNamesTheRest asserts
                   at the run site.
                 - laneRunLine for a selector lane matched by two paths, one deleted,
                   returns dropped==[the deleted path] and a line scoped to the
                   survivor only. If the filter stops reporting drops, c-2's
                   "named with its reason" becomes unimplementable and
                   TestLaneRunLineNamesDroppedPaths fails.
                 - the gate is unchanged by the extraction: TestSelectorScopesTheSpawnedLine,
                   TestTemplatedLaneSpawnsTheExpandedLine and
                   TestALaneWhoseEveryPathIsGoneDoesNotSpawnAndIsNotGreen still pass
                   against the recorded spawn line, byte for byte.

  t-2  Default the file set to the working tree
       files:    internal/cmd/lane_preview_files.go, internal/cmd/lane_preview_files_test.go
       covers:   c-1
       desc:     workingTreeFiles(repoDir) runs `git status --porcelain
                 --untracked-files=all` and parses it through the existing
                 porcelainPaths/unquotePath, returning repo-relative paths for the
                 staged, unstaged and untracked set (locked bare_preview_default).
       contract: - a fixture repo with an untracked directory `internal/new/a.go`
                   returns that FILE, not `internal/new/`: dropping
                   `--untracked-files=all` collapses it to the directory and
                   TestWorkingTreeFilesExpandsUntrackedDirectories fails, which
                   would silently under-report lanes for brand-new code.
                 - a renamed file returns BOTH sides (`R old -> new`), so the gone
                   half reaches t-1's dropped report instead of vanishing.
                 - a deleted tracked file IS returned: filtering it here would hide
                   the drop c-2 requires preview to name.
                 - a path with a space comes back unquoted, matching what
                   `--files` would have been handed.

  t-3  Print selector template fields in lane list
       files:    internal/cmd/test_lane.go, README.md,
                 internal/cmd/test_lane_list_fields_test.go
       covers:   c-4
       desc:     testLaneList calls the existing printLaneTemplate after its
                 `selector:` line, so selector_template and selector_join print
                 beside the selector. README's `dross test lane {...}` row gains the
                 claim.
       contract: - `dross test lane list` on a lane declaring
                   selector_template="--package {path}" and selector_join="|" prints
                   `selector-template: --package {path}` and `selector-join: |`;
                   deleting either Printf fails TestLaneListPrintsTemplateFields.
                 - a lane declaring neither prints NEITHER key — the opt-in
                   convention every other optional field in the listing follows;
                   an unconditional `selector-template: -` fails the same test.
                 - the README row for `dross test lane` names selector-template in
                   the sentence about `lane list`, asserted per-row via readmeRow,
                   so the claim cannot be satisfied by a mention three commands away.

  t-4  Preview host: probed, unprobed, unresolved
       files:    internal/cmd/lane_preview_locality.go,
                 internal/cmd/lane_preview_locality_test.go
       covers:   c-6
       desc:     previewHost resolves where lanes would run without committing to a
                 run: read the grants, probe through preflightRemote unless probing
                 is off, and classify the answer none/probed/unprobed/unresolved.
                 Per-lane verdicts come from the existing laneLocality, called with
                 host="" whenever the host is unprobed or unresolved.
       contract: - with a granted host whose probe seam returns remote.ErrTransport,
                   previewHost returns State=="unresolved", the host's name, and a
                   NIL error: returning the transport failure fails
                   TestUnreachableHostIsUnresolvedNotAFailure and would turn preview
                   into the non-zero verdict locked preview_exit_status forbids.
                 - with probing off, fakeProbe's call counter is 0 and State is
                   "unprobed" while the host name is still reported — a version that
                   probes anyway fails TestNoProbeNeverOpensAConnection.
                 - with a probed host missing `pnpm`, the go lane's verdict is
                   siteRemote and the web lane's is siteLocal carrying the
                   laneFallbackLine that names pnpm, helicon and
                   `dross test lane install web`. A preview that re-derived locality
                   instead of calling laneLocality fails on that exact wording.
                 - a lane whose tool is on neither machine comes back siteRefused
                   with its exitToolchainMissing error carried, NOT returned.

Wave 2 (depends t-1, t-2)
  t-5  Add `dross test lane preview` verb
       files:    internal/cmd/test_lane_preview.go, internal/cmd/test_lane.go,
                 internal/cmd/test_lane_preview_test.go
       covers:   c-1, c-2
       depends:  t-1, t-2
       desc:     New subcommand under `dross test lane`: `--files` (repeatable,
                 defaulting to t-2's working-tree set), `--lane <name>` narrowing via
                 findLane. Builds a previewFinding per hit lane from t-1's
                 resolveLanes + laneRunLine and renders lane name, derived line, and
                 the non-running outcomes. Exits 0 on every finding.
       contract: - byte identity, one fixture, two invocations: `dross test --files
                   internal/cmd/test.go` under installSpawnRecorder records line L;
                   `dross test lane preview --files internal/cmd/test.go` prints a
                   line whose command text equals L exactly. A preview that builds
                   its own line fails TestPreviewLineIsTheLineTheGateWouldSpawn — this
                   is c-1's same-code-path guarantee made observable.
                 - preview spawns nothing: spawnRecorder.count()==0, the runLog holds
                   no "rsync" and no remote spawn event, and a lane declaring
                   `--prepare "make build"` produces no prepare spawn. Any of the
                   three failing fails TestPreviewSpawnsNothing.
                 - one invocation carrying an absolute path, an in-tree path matching
                   no lane, a matched-but-deleted path, and a selector lane whose
                   every path is deleted prints four distinct named findings — the
                   path AND its reason in each — and exits 0. A silent omission of
                   any one fails TestPreviewNamesEveryNonRunningOutcome.
                 - `--lane nope` exits non-zero and its message lists the declared
                   lane names (findLane's refusal); `--lane go` on a file set that
                   also hits the docs lane renders the go lane only.
                 - a bare `dross test lane preview` in a tree with one untracked
                   `internal/new/a.go` names the go lane and says it took 1 file;
                   wiring the default to `git status --porcelain` without
                   --untracked-files=all reports 0 lanes and fails here.

Wave 3 (depends t-4, t-5)
  t-6  Annotate previewed lanes with consent and locality
       files:    internal/cmd/test_lane_preview.go,
                 internal/cmd/test_lane_preview_consent_test.go
       covers:   c-3, c-6
       depends:  t-4, t-5
       desc:     Each rendered lane gains its ConsentState (LaneConsented over
                 laneConsentLine, same call the gate makes) and its previewHost
                 verdict from t-4, plus a `--no-probe` flag. Both are reported, never
                 acted on.
       contract: - an UNGRANTED lane is still rendered, with its derived line and the
                   state `absent`; preview exits 0 and spawns nothing.
                   TestPreviewRendersAnUngrantedLaneWithoutRefusing fails if the
                   laneConsentRefusal path from runTestLanes is reused here.
                 - a lane whose command was edited after `dross trust --lane` renders
                   `stale`, not `absent` — the distinction the ladder exists for, and
                   the one that tells the reader to re-read a changed line.
                   Asserted against ConsentState.String() so a hand-written second
                   vocabulary fails.
                 - with a probed host missing pnpm, the web lane's rendered line says
                   it would run locally and NAMES pnpm as the absent binary, while
                   the go lane names helicon; with `--no-probe` both lanes report the
                   configured host as unprobed and fakeProbe's counter is 0.
                 - an unreachable granted host renders every lane as unresolved and
                   preview still exits 0 (TestUnreachableHostStillExitsZero).

Wave 4 (depends t-6)
  t-7  Emit preview findings as JSON, document the verb
       files:    internal/cmd/test_lane_preview.go, README.md,
                 internal/cmd/test_lane_preview_json_test.go
       covers:   c-5
       depends:  t-6
       desc:     `--json` marshals the complete finding set through emitJSON —
                 lanes with their derived line, dropped/unmatched/out-of-tree paths,
                 consent state, locality and host. README's `dross test lane` row
                 documents preview, `--lane`, `--no-probe`, `--json` and the exit-0
                 rule.
       contract: - same-facts, one fixture, two renderings: every lane name and every
                   derived line in the human output appears verbatim in the decoded
                   JSON, and every dropped/unmatched/out-of-tree path in one appears
                   in the other. A field added to the human line and not the payload
                   fails TestPreviewJSONCarriesTheSameFacts.
                 - the payload is a bare document: assertBareJSONDocument's checks
                   (first non-space byte `{`, no `#` line, parses) hold, so a
                   `# <path>` header or an envelope key fails.
                 - consent and locality are emitted as the SAME strings the human
                   output prints (ConsentState.String(), the site name) — a payload
                   inventing `"consent": "ok"` fails the comparison.
                 - `--json` on an unmatched-only file set emits an object with an
                   empty lanes array and the unmatched paths present, and still exits
                   0 — an empty `{}` or a non-zero exit fails
                   TestPreviewJSONReportsAnEmptyMatchWithoutFailing.
                 - the README `dross test lane {...}` row names `preview` and says it
                   exits 0 on findings, asserted per-row via readmeRow.

## Coverage

| criterion | tasks |
|---|---|
| c-1 preview prints the gate's own derived line, spawns nothing | t-1, t-2, t-5 |
| c-2 non-running outcomes are named, not silent | t-1, t-5 |
| c-3 consent state per rendered lane, never refuses | t-6 |
| c-4 `lane list` prints selector_template + selector_join | t-3 |
| c-5 `--json` carries the same facts | t-7 |
| c-6 where each lane would run; unreachable ≠ failure | t-4, t-6 |

Every criterion has at least one task, and every task carries at least one
criterion — no orphan work, including the README edits, which ride the task
whose surface they describe.

## Judgment calls

- **Shared derivation, not a comparison test.** c-1 says preview and gate *cannot*
  diverge, so t-1 extracts one resolution path both call. Rejected: leaving
  runTestLanes alone and giving preview its own derivation guarded by an
  output-comparison test — a test catches drift after it ships, it does not
  prevent it, and the criterion asks for prevention.
- **Policy stays at the caller.** resolveLanes returns OutOfTree/Unmatched as data;
  the gate keeps exitBadFileSet and exitNothingMeasured, preview prints them and
  exits 0. Rejected: returning errors and having preview swallow them, which makes
  locked preview_exit_status depend on error-unwrapping rather than on structure.
- **No new laneSite values.** unprobed/unresolved live on previewHost, and lanes
  are decided by calling laneLocality with host="" — exactly what the run does on
  a transport fallback. Rejected: adding siteUnprobed/siteUnresolved to the enum,
  which every switch in the run path would silently accept.
- **Probe through preflightRemote/remoteProbeFn.** Rejected calling remote.Probe
  directly: doctor, the run and preview would then be three derivations of one
  question, and the drift shows up as doctor passing on a host preview says is
  fine and the run then falls back from.
- **A malformed lane (bad selector style, bad template) is a finding, not a fault.**
  The gate's up-front fence refuses; preview names the lane and its problem and
  exits 0, the same treatment c-3 mandates for an ungranted lane — preview's job
  is to show what the run would do, including refuse.
- **Deleted paths stay in the working-tree file set.** Rejected filtering them in
  t-2: c-2 requires the drop to be *named*, and a path removed at collection can
  never be named at derivation.
- **`--json` gets its own usage string, not jsonFlagUsage.** That constant promises
  "instead of toml, no `# <path>` header", and preview has no toml rendering to be
  instead of. The bare-document *shape* is still shared, asserted through
  assertBareJSONDocument.
- **Consent and locality split from the preview core (t-6, not folded into t-5).**
  They are the two facts that make a rendered line actionable and they have their
  own contracts; folding them in would give one task four criteria and one
  untestable "it all works" gate.
