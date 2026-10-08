# Verification-lens draft — ast-aware-mutation-ranges

Lens: every criterion's ideal test contract was written first; each task is the
smallest change that makes its contracts satisfiable. Node-gated tests use the
existing `e2eSkipReason()` shape (skip naming the install line locally,
`DROSS_REQUIRE_E2E` fatal in CI); everything that CAN be proven without node is
proven without node, so the pure planner is never hostage to the toolchain.

Facts the plan rests on (read from source, not assumed):
- The attached path already groups files by `mutation.Dispatch` (Supports); the
  README.md/assets leak in c-5 comes from the DETACHED collector,
  `collectDetachedFrom` (internal/cmd/verify.go:704-716), which hands the raw
  `mutationCandidates` list to `PlanRanges(&mutation.Gremlins{}, files, scope)`
  and to `Files:`.
- The fixture `internal/mutation/testdata/ts-project` has `node_modules/@babel/parser`
  installed via `@stryker-mutator/instrumenter` (package.json line 44 pins `~7.29.0`).
- The exec-consent gate is reach-based: a spawn reached only through the gated
  `verify` command needs no marker (`TestEverySpawnSiteGatedOrExempt`,
  `TestToolchainSpawnsResolveAsGated`). `verify.RunScoped → a.(iface).Method`
  fans out to every implementer, which is how `RunRanges` is already resolved.
- argfence's table is closed (`TestPolicyCoversEveryKnownBinary` lists the keys;
  `TestAuditKnowsEveryPolicyBinary` requires a `valueTakingFlags` entry per key).
- toolfence's sink vocabulary is `error|message|note|notes|detail|output|stderr|reason|text|degraded`;
  a `construct` tag is not a sink, and `Scope.Degraded` is already declared NotToolStream.
- `internal/mutation` is outside pathfence's unwrap-scan set and already reads repo
  files by raw path (`inapplicable.go:127 constLines`), so the resolver may read
  source the same way.

---

Phase ast-aware-mutation-ranges — 9 tasks across 4 waves

Wave 1
  t-1  Add construct resolver: Babel script + Stryker.Constructs
       files:    internal/mutation/adapter.go, internal/mutation/constructs.go,
                 internal/mutation/ast_constructs.js, internal/mutation/constructs_test.go,
                 internal/mutation/testdata/ts-project/src/long.ts,
                 internal/mutation/testdata/ts-project/src/long.test.ts
       covers:   c-1, c-4
       description:
         adapter.go gains the optional interface and value types:
           type Construct struct { Range; Kind, Name string }
           type Resolved  struct { Constructs []Construct; Unavailable string }   // Unavailable = dross prose ("parse error at 12:5"), "" when resolved
           type ConstructResolver interface { Constructs(hunks map[string][]Range) (map[string]Resolved, error) }
         constructs.go implements it on *Stryker: reads each file's source
         (filepath.Join(ProjectRoot, file), same pattern as inapplicable.go),
         builds stdin = "const REQUEST = " + json + ";\n" + embedded script,
         spawns ONE `node -` per leg via s.buildCmd([]string{"node","-"}) with
         cmd.Dir = s.workDir() (Prefix honoured, launcher NEVER used — planning_locus).
         Leg-wide failure → error carrying a closed cause: node not on PATH, or
         @babel/parser unresolvable from Workdir; per-file parse failure → Resolved.Unavailable
         with line:col only (composed from parsed JSON fields, never the stream).
         ast_constructs.js (go:embed): createRequire from
         node_modules/@stryker-mutator/instrumenter/package.json → require("@babel/parser");
         parse with {sourceType:"module", plugins:["typescript","jsx"], errorRecovery:false};
         for each hunk, every Program.body statement whose loc spans overlap the hunk
         becomes one construct (span = OUTERMOST node incl. export wrapper; Kind = type
         after unwrapping Export{Named,Default}Declaration; Name = declaration id /
         first declarator id / class id, "" otherwise); hunk lines covered by no
         statement are returned as Kind "hunk" sub-ranges. Output JSON per file.
         Fixture long.ts: `export function big()` of ≥40 lines followed by a blank
         line and `export function small()`; long.test.ts covers both.
       contract:
         - if Constructs stops widening to the top-level span, TestConstructsWidenToTheEnclosingTopLevelStatement
           fails: hunks {long.ts:[{30,30}]} → exactly one construct, Start == line of `export function big(`,
           End == its closing brace line, Kind "FunctionDeclaration", Name "big". (node-gated)
         - if two hunks inside one function yield two constructs, TestOneConstructPerDistinctStatement
           fails: hunks {long.ts:[{12,12},{30,30}]} → len == 1. (node-gated)
         - if a hunk on the blank line between big and small is not returned as Kind "hunk"
           with its own line span, TestLinesOutsideAnyStatementStayABareHunk fails. (node-gated)
         - if a hunk that straddles big's last line and small's first line does not
           return BOTH constructs, TestAHunkCrossingTwoStatementsYieldsBoth fails. (node-gated)
         - if unparseable source ("export function {") is reported as a leg-wide error
           instead of Resolved{Unavailable:"parse error at 1:17"} for that file alone while
           a sibling file still resolves, TestParseErrorIsPerFileAndCarriesPosition fails. (node-gated)
         - if node is absent (t.Setenv PATH to an empty dir) and Constructs returns anything
           but an error whose text names "node" and PATH, TestNodeMissingIsALegWideCause fails. (node-free)
         - if @babel/parser cannot be resolved (Workdir = t.TempDir() with no node_modules)
           and the error does not name "@babel/parser" and the Workdir, TestParserUnresolvableNamesTheWorkdir fails. (node-gated)
         - if Constructs ever routes through the launcher, TestConstructsNeverTouchTheRemote fails:
           Stryker{Remote:&remote.Target{Host:"never"}} with launcherCommand swapped for a
           func that t.Fatal()s; PATH emptied so the local spawn fails fast; the stub is never called. (node-free)
         - if the argv is not exactly ["node","-"] with the request on stdin (e.g. the file path
           becomes a positional), TestConstructsArgvIsLiteralNodeDash fails — asserted by swapping
           s.Prefix to a recorder script that echoes "$@" and its stdin length. (node-free)
         - if the spawn becomes reachable from an ungated command, the existing repo-wide
           TestEverySpawnSiteGatedOrExempt reports "ungated via …" for stryker.go's buildCmd site.
       depends_on: []
       status: pending

  t-2  Register node in the argfence policy table
       files:    internal/argfence/policy.go, internal/argfence/argfence_test.go,
                 internal/cmd/subprocargs_audit_test.go
       covers:   c-1
       description:
         table gains "node": {Kind: Reject, Why: "node reads options ahead of the script operand and
         honours no end-of-options token; the only positional dross passes is the literal `-`"}.
         TestPolicyCoversEveryKnownBinary's want-list and subprocargs' valueTakingFlags gain "node".
       contract:
         - if "node" is missing from the table, TestPolicyCoversEveryKnownBinary fails on the key set
           and TestAuditKnowsEveryPolicyBinary fails on the stale valueTakingFlags entry.
         - if a Constructs caller ever passes a derived positional to node, argfence.Fence("node", …)
           (Reject kind) refuses a leading-dash value — pinned by TestFenceRejectTools' table-driven case gaining node.
       depends_on: []
       status: pending

  t-3  Kill verify.go:115 through the command
       files:    internal/cmd/verify_reuse_report_test.go
       covers:   c-6
       description:
         Two command-level tests driving Verify() over scopedVerifyRepo with a .go-only
         phase and configuredAdaptersFn stubbed to return []Adapter{&mutation.Stryker{}, stubGremlins}
         (the real Stryker is never dispatched because no file is .ts, so nothing spawns).
       contract:
         - if `if reuseReport` is negated, TestPlainVerifyLeavesReuseReportOff fails: after
           runCmd(t, Verify(), phaseID) with no flag, stryker.ReuseReport == false (the mutant
           would call applyReuseReport and flip it true).
         - if the branch is negated, TestReuseReportWithDetachRefusesThroughTheCommand fails:
           runCmd(t, Verify(), phaseID, "--reuse-report", "--detach") returns an error containing
           "a reused report is a run that is not launched" (the mutant skips the refusal and
           reaches detachRequiresAHost, whose error text differs).
         - verify-time step, not a test: after the run shows survivor a22f0ab51a69e9e8 absent,
           close its route (`dross deferred` unroute + dismiss on mutation-range-provenance, per
           deferred ec9abaedcbc6fce4's description) so it is not re-listed.
       depends_on: []
       status: pending

  t-4  Filter the collected gremlins leg to Supports()
       files:    internal/cmd/verify.go, internal/cmd/verify_results_test.go
       covers:   c-5
       description:
         collectDetachedFrom narrows `files` through a local helper
         supportedBy(g, files) before both `Files:` and PlanRanges; the helper is the
         single place the detached leg's file set is decided.
       contract:
         - if the collected leg still carries non-Go scope files, the extended
           TestCollectRecordsWholeFileProvenance fails: scope holds a.go, README.md and
           assets/prompts/verify.md; leg.Files == ["a.go"], leg.WholeFile has exactly one
           key ("a.go" → adapter-lacks-range-runner), and len(leg.WholeFile) == number of .go
           files in scope.
         - if the filter dropped a Go file too, the same test fails on the missing "a.go".
       depends_on: []
       status: pending

Wave 2 (depends t-1)
  t-5  Replace the pad planner with construct planning
       files:    internal/verify/range_provenance.go, internal/verify/verify.go,
                 internal/verify/range_provenance_test.go, internal/verify/range_dispatch_test.go,
                 internal/verify/range_record_test.go
       covers:   c-1, c-2, c-3, c-4, c-5
       description:
         Delete hunkContextLines, padAndMerge and their comment block. EffectiveRange
         becomes {Start, End int; Construct string `json:"construct"`}. New closed reason
         WholeFileASTUnavailable = "ast-unavailable" (degrading). New pure step
         `resolveConstructs(a Adapter, files, scope) (map[string]mutation.Resolved, legErr string)`
         called from RunScoped BEFORE PlanRanges (spawns via the adapter, nowhere else);
         PlanRanges(a, files, scope, resolved) stays pure and: filters files by a.Supports()
         first; a RangeRunner that is not a ConstructResolver, a leg-wide legErr, or a file
         with Resolved.Unavailable → WholeFile[f]=ast-unavailable + Degraded line
         "<adapter>: ast unavailable for <f> (<detail>); mutating the whole file"; otherwise
         one EffectiveRange per distinct construct span, sorted, touching/overlapping
         bare-hunk spans merged, Construct = Kind+" "+Name (Kind alone when Name empty,
         "hunk" for bare). Malformed-range stays judged on the raw hunk before resolution.
         Detached path passes nil resolved (gremlins never reaches the resolver).
       contract:
         - if EffectiveRange regains a Pad field or a "pad" JSON key, TestEffectiveRangeHasNoPad
           fails (reflect.TypeOf(EffectiveRange{}).FieldByName("Pad") absent; json of a leg
           lacks `"pad"` and contains `"construct":"FunctionDeclaration tally"`). [range_record_test]
         - if two hunks in one construct produce two ranges, TestOneRangePerDistinctConstruct
           fails: rangingAdapter canned {a.ts: [{10,35,"FunctionDeclaration","tally"}] for both hunks}
           → plan.Ranges["a.ts"] == [{10,35,"FunctionDeclaration tally"}] and Dispatch == [{10,35}]. [range_provenance_test]
         - if a bare hunk is padded or dropped, TestBareHunkKeepsItsOwnLinesAndSaysHunk fails:
           Resolved{Constructs:[{{40,41},"hunk",""}]} → one range {40,41,"hunk"}.
         - if a range-capable adapter without ConstructResolver is dispatched bare hunks
           instead of whole files, TestRangeRunnerWithoutResolverIsASTUnavailable fails:
           existing rangingAdapter (no Constructs) → WholeFile[f]=="ast-unavailable",
           plan.Dispatch nil, Degraded non-empty; and RunScoped calls Run, not RunRanges. [range_dispatch_test]
         - if the specific cause leaks into the reason value, TestASTUnavailableDetailStaysOnDegraded
           fails: legErr "node is not on PATH" → every WholeFile value == "ast-unavailable" exactly,
           Degraded[0] contains "node is not on PATH"; Resolved{Unavailable:"parse error at 3:9"}
           on b.ts alone → only b.ts whole-file, a.ts still ranged, Degraded names "3:9".
         - if ast-unavailable is informational, TestASTUnavailableDegradesTheScope fails:
           RunScoped with a failing resolver leaves scope.Degraded non-empty (same shape as
           TestHunklessScopeOnACapableAdapterDegrades).
         - if the closed set loses or gains a member, TestWholeFileReasonsAreAClosedSet fails
           (count 5, ast-unavailable present, every plan path lands inside it).
         - if PlanRanges records a file the adapter does not Support, TestPlanRangesIgnoresUnsupportedFiles
           fails: plainAdapter with Supports(".go" only), files [a.go, README.md] → WholeFile lacks README.md.
         - if Constructs is called AFTER RunRanges, or with padded rather than raw hunks,
           TestResolverSeesRawHunksBeforeDispatch fails: the stub records call order and
           the hunks it received == scope.Hunks[f].
         - if a resolver error aborts the leg instead of falling open, TestResolverFailureStillRunsTheLeg
           fails: leg has Mutation set, Error empty, WholeFile all ast-unavailable.
         - deleted: TestPadAndMerge* (5), TestRunScopedPassesPaddedRanges, TestEffectiveRangesArePostPadPostMerge,
           TestLegRecordsEffectiveRangesWithPad — replaced by the above, never left asserting a pad.
       depends_on: [t-1]
       status: pending

  t-6  Prove the pad-lost mutant is generated (real Stryker)
       files:    internal/mutation/stryker_e2e_test.go
       covers:   c-1
       description:
         TestConstructRangeGeneratesTheMutantThePadLost (node-gated, DROSS_REQUIRE_E2E-aware):
         hunks {src/long.ts:[{L,L}]} where L is a mutable line ≥ 26 lines into big();
         constructs := s.Constructs(hunks); ranged := s.RunRanges([long.ts], {long.ts: [construct span]});
         bare := s.RunRanges([long.ts], {long.ts: [{L,L}]}).
       contract:
         - if the construct span no longer starts at big's first line, the test fails on
           construct[0].Start != the `export function big(` line read from the fixture.
         - if the widened range does not recover the enclosing-block mutants, the test fails on
           total(ranged.Files["src/long.ts"]) <= total(bare.Files["src/long.ts"]) — the strictly
           greater count IS the mutant the pad heuristic lost.
         - if the range leaks into small(), the test fails: ranged has no Surviving mutant with
           Line > big's closing line (small is deliberately left uncovered so its mutants would survive).
         - if the fixture loses big/small or long.test.ts stops importing ./long,
           TestFixtureTestCoversTheSource (extended) fails.
       depends_on: [t-1]
       status: pending

Wave 3 (depends t-5)
  t-7  Put the construct in verify.toml's leg summary
       files:    internal/verify/verify.go, internal/verify/range_summary_test.go
       covers:   c-2, c-3
       description:
         LegSummary drops Pad; legProvenance returns (ranges, whole) with each range rendered
         "file:start-end (construct)"; both call sites in Skeleton updated. Comment blocks on
         LanguageRun.Ranges / LegSummary lose the pad wording.
       contract:
         - if verify.toml's leg carries a pad key or omits the construct, TestVerifyTomlStatesConstructs
           fails: rendered toml contains `ranges = ["src/a.ts:10-35 (FunctionDeclaration tally)"]`
           and does not contain "pad".
         - if LegSummary regains Pad, TestLegSummaryHasNoPad fails (reflect FieldByName).
         - if a whole-file leg prints anything under ranges, TestGremlinsLegHasNoRanges (renamed
           from TestGremlinsLegHasNoPad) fails.
         - if ordering regresses, TestLegProvenanceIsSorted fails on the construct-bearing strings.
       depends_on: [t-5]
       status: pending

  t-8  Print constructs and the ast-unavailable warning in cmd
       files:    internal/cmd/verify.go, internal/cmd/verifyscope.go,
                 internal/cmd/verify_scoping_test.go, internal/cmd/verifyscope_test.go,
                 internal/cmd/verify_range_e2e_test.go
       covers:   c-3, c-4
       description:
         printRangeProvenance prints "ranged stryker N file(s)" (no pad) and, per ranged file,
         its construct list; verifyscope.go prints "ranged <f>  10-35 (FunctionDeclaration tally)".
         stubRangeAdapter (verify_scoping_test.go) gains a canned Constructs so the cmd e2e
         still narrows; a second stub without Constructs exercises the fallback.
       contract:
         - if the ranged-leg line still says "(pad", TestVerifyOutputNamesWholeFileLegs fails on
           the needle "ranged stryker 1 file(s)" followed by "  a.go  3-3 (FunctionDeclaration A)".
         - if a RangeRunner without a resolver reads as ranged, TestVerifyOutputWarnsWhenTheASTIsUnavailable
           fails: stdout contains "scope degraded: stryker: ast unavailable for a.go" and
           "whole-file … ast-unavailable", and never the word "ranged".
         - if `dross verify scope` prints the pad or hides the construct, TestVerifyScopePrintsRawAndEffective
           fails on needles "10-12" (raw) and "1-37 (FunctionDeclaration tally)" (effective).
         - if --json drops the construct, TestVerifyScopeJSONIsTheRecordVerbatim fails on
           `"construct": "FunctionDeclaration tally"` present and `"pad"` absent.
         - if the real pipeline's hunk keys and the resolver's file keys diverge,
           TestRangedDispatchReachesTheAdapterEndToEnd fails: the stub's Constructs receives
           {internal/pkg/c.go: [{40,41}]}, RunRanges receives the canned {30,50}, and
           tests.json records {30,50,"FunctionDeclaration F"} — never the raw {40,41}.
       depends_on: [t-5]
       status: pending

Wave 4 (depends t-7, t-8)
  t-9  Retire the pad from docs and pin its absence
       files:    ARCHITECTURE.md, assets/prompts/verify.md, internal/cmd/pad_residue_test.go
       covers:   c-2
       description:
         ARCHITECTURE.md's scoping paragraph (line ~567) and function list (~575-586) and
         assets/prompts/verify.md line 74 rewritten around constructs: `ranges` are
         `file → [{start, end, construct}]`, the closed reason set lists ast-unavailable,
         `dross verify scope` shows the construct. pad_residue_test.go walks
         internal/verify, internal/mutation, internal/cmd/verify*.go (non-test),
         ARCHITECTURE.md and assets/prompts/verify.md.
       contract:
         - if any of the tokens `hunkContextLines`, `padAndMerge`, `json:"pad"`, `toml:"pad`,
           `(pad `, `post-pad` or `padded hunk` returns to those files, TestNoPadResidue fails
           naming file:line. Calibrated by TestPadResidueScanTripsOnItsFixture against a
           testdata snippet containing `const hunkContextLines = 25`, so a scan that stopped
           matching cannot pass silently.
         - if ARCHITECTURE.md stops naming ast-unavailable or the construct record,
           TestArchitectureDescribesConstructRanges fails on those two needles.
       depends_on: [t-7, t-8]
       status: pending

## Coverage
- c-1 → t-1, t-2, t-5, t-6   (t-6 is the measured proof: strictly more mutants than the bare hunk)
- c-2 → t-5, t-7, t-9        (deletion in t-5, summary in t-7, docs + residue gate in t-9)
- c-3 → t-5, t-7, t-8        (record → verify.toml → `dross verify scope` / verify output)
- c-4 → t-1, t-5, t-8        (causes in t-1, reason + Degraded in t-5, printed warning in t-8)
- c-5 → t-4, t-5             (detached collector is the live offender; PlanRanges filter is the guard)
- c-6 → t-3

## Judgment calls
- ConstructResolver is a SEPARATE optional interface, not a new method on RangeRunner: every existing
  RangeRunner stub keeps compiling, and "range-capable but no AST" maps directly onto c-4's
  ast-unavailable fallback — which gives the planner a node-free test for the whole degrade path.
  Rejected: widening RangeRunner (breaks two packages' stubs mid-wave and leaves no way to express
  "cannot resolve").
- One `node -` spawn per leg carrying every file's source on stdin, not one per file and not a path
  argument: argv stays the literal ["node","-"] (nothing for the argv audit to flag), the parse-error
  test can feed arbitrary source, and node-missing / parser-unresolvable are leg-wide facts.
  Rejected: node reading files itself (cwd is Workdir, hunk keys are repo-relative — a second path
  vocabulary to keep in step).
- Spawn through the existing `buildCmd` seam, never the launcher: honours a docker Prefix, and
  `TestConstructsNeverTouchTheRemote` pins that a Remote target does not redirect it (planning_locus).
  Flagged, not solved: `docker compose exec` without `-T` may not forward stdin; dross itself is native,
  so this rides until a docker project hits it.
- Construct rendering lives in verify (Kind+" "+Name → one string), the tool-side type stays structured:
  the record's string is dross-authored, and the `construct` tag sits outside toolfence's sink vocabulary
  on purpose — its values are a Babel node type plus a repo-authored identifier, never a stream.
- c-5 fixed at the detached collector (t-4, wave 1, independent) plus a defensive Supports() filter inside
  PlanRanges (t-5). Rejected: only the PlanRanges filter — it would leave `Files:` in the collected leg
  still listing README.md, which is half the criterion.
- c-1's proof is a strict inequality against the bare hunk (two Stryker runs), not equality against a
  whole-file count: Report exposes killed mutants only as counters, so "all of big's mutants" cannot be
  read per line without a third run and a parser change. Strictly-more IS the lost mutant.
- The pad deletion is asserted three ways (reflect on the struct, JSON/TOML output, a source-token scan
  calibrated on a fixture) because c-2 is a negative criterion: a single grep test that quietly stopped
  matching would pass forever.
- t-3's tests stub `configuredAdaptersFn` with a real zero `*mutation.Stryker` beside the gremlins stub:
  `applyReuseReport` type-asserts `*mutation.Stryker`, so only the real type can prove the flag stayed
  off, and a .go-only phase guarantees it is never dispatched (no spawn).
