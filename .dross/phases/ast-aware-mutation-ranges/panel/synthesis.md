# Synthesis — ast-aware-mutation-ranges

Judge: cold, authored none of the three drafts. Claims below marked *(verified)*
were checked against the tree with grep before scoring; nothing else in the
drafts was taken on trust where it affects the plan.

Verified facts the merge rests on:
- c-5 originates in the DETACHED collector, `collectDetachedFrom` — `internal/cmd/verify.go:704` hands the raw
  `mutationCandidates` list to `PlanRanges(&mutation.Gremlins{}, files, scope)` and to `Files: files`. `RunScoped`
  (internal/verify/verify.go:662) already groups by `mutation.Dispatch` (Supports). *(verified — mvp/verification correct)*
- The subprocargs audit fails closed: a literal binary with no argfence policy is a finding ("binary … has no
  argv policy — add one to internal/argfence", subprocargs_audit_test.go ~L180), and `TestAuditKnowsEveryPolicyBinary`
  requires a `valueTakingFlags` entry per policy key, while `TestPolicyCoversEveryKnownBinary`
  (argfence_test.go:61) pins the key set. So `node` needs three edits: policy.go, argfence_test.go, subprocargs_audit_test.go. *(verified)*
- `Pad` is read in three non-test places: `internal/cmd/verifyscope.go:282`, `internal/cmd/verify.go:1523`
  (printRangeProvenance), `internal/verify/verify.go:452` (legProvenance). Deleting `EffectiveRange.Pad` without
  first changing those readers breaks `go build ./internal/cmd`. *(verified — decisive for wave scoring)*
- `verify.go:115` is `if reuseReport {`; the detach refusal text is
  "--reuse-report with --detach: a reused report is a run that is not launched; drop one of the two". *(verified)*
- Stryker `Supports` includes `.svelte`, so the Svelte question is live. *(verified)*
- The ts-project fixture has `node_modules/@babel/parser` installed locally (gitignored, installed from the
  pinned lockfile); `e2eSkipReason()` + `DROSS_REQUIRE_E2E` is the existing node-gating convention. *(verified)*
- `strykerBuildCmd = (*Stryker).buildCmd` honours `Prefix`; `launcherCommand`/`newLauncher` is the separate
  remote seam, and `newLauncher` refuses Prefix+Remote together. *(verified)*
- Ground truth from the lead: Stryker's `shouldMutate` keeps a mutant only when its ENTIRE span lies inside a
  `--mutate` range, so the expansion unit is the full top-level construct span. All three drafts widen to the full
  `Program.body` node span (export wrapper included), so all three satisfy this; none needs correcting on it.

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| risk (8 tasks / 5 waves) | 6/6; c-4 owned across reason (t-5), cause classification (t-6), real causes (t-8); c-5 fixed at the verified offender plus a pinned RunScoped invariant | Very high: every contract names the test, the input, the expected output and the failure it catches; property check for non-overlap; per-cause fake-`node` arms. One defect: t-8 asserts `N2 == N3` but its own fixture has a second function (`pick<T>`), so whole-file N3 > construct-span N2 — the assertion is wrong as written | Good: 4 small wave-1/2 tasks; t-5 is 7 files but the four pad-pinning tests genuinely cannot outlive the pad; argfence folded into t-6 and misses the two test files the audit demands | Correct: the only draft that stops the cmd/verify readers reading `Pad` (t-4, additive) BEFORE deleting it (t-5), and gates the spawn after its wiring so the exec-consent audit never sees an unreachable site. Cost: 5 waves, parallelism only in waves 1 and 4 |
| mvp (5 tasks / 3 waves) | 6/6; c-4's printed warning relies on the existing `scope degraded:` loop but is pinned by a cmd test; c-5 correctly located | High but thinner: ~14 contracts vs ~35; the only real-Babel proof depends on an out-of-repo `$DROSS_BABEL_WORKDIR` project, so it skips on every machine that has not set it — including the one with the fixture already installed | Coarse: t-1 bundles resolver + script + argfence + tests (6 files), t-2 rewrites 6 files; c-5+c-6 merged into one task though they share no files beyond verify.go | Broken at t-2: deletes `EffectiveRange.Pad` in wave 2 while `internal/cmd/verifyscope.go:282` and `verify.go:1523` still read it (fixed only in t-3, wave 3) — the t-2 commit does not compile in internal/cmd. Also omits `subprocargs_audit_test.go` from the argfence edit, so `TestAuditKnowsEveryPolicyBinary` goes red at t-1 |
| verification (9 tasks / 4 waves) | 6/6; c-5 covered twice (collector + PlanRanges filter); c-2 asserted three ways (reflect, output, calibrated source scan) | Very high: needles are exact, node-gated tests labelled, residue scan calibrated on a fixture so it cannot rot silently, "never touch the remote" pinned with a fatal stub. Weak spots: parses every file with `["typescript","jsx"]` (breaks `.ts` angle-bracket generics/assertions — risk's F13 catches this); asserts argv by swapping `Prefix` to a recorder script (indirect); fixture says `long.test.ts covers both` yet the proof assumes `small` is uncovered | Slightly over-split (t-2 argfence is 3 files, t-7 is 2) but every task is one clean commit; t-5 is 5 files | Broken at t-5: deletes `EffectiveRange.Pad` in wave 2 while the cmd readers (t-8) and `LegSummary.Pad`/`legProvenance` (t-7) are wave 3 — same red commit as mvp. Waves otherwise well reasoned (docs after both readouts) |

**Skeleton: risk.** It is the only draft whose commit sequence stays green through the pad deletion (the
three `Pad` readers are verified above), it keeps hunk→construct expansion in Go where it is unit-testable
without node, and it owns each failure mode exactly once. Its two real defects — the `N2 == N3` proof
assertion and the incomplete argfence file list — are fixed by grafts from verification. Verification is the
richer contract source and supplies most of the grafts; mvp contributes the stdin-prelude request channel,
the `dross architecture check` step and the cmd-level "AST unavailable prints the warning" test.

## Merged plan

Phase ast-aware-mutation-ranges — 9 tasks across 5 waves

Wave 1
  t-1  Add construct seam and pure hunk expansion  [risk]
       files:    internal/mutation/construct.go, internal/verify/range_expand.go,
                 internal/verify/range_expand_test.go, internal/verify/range_dispatch_test.go
       covers:   c-1, c-3
       description:
         construct.go: `Construct{Start, End int; Kind, Name string}`, `(Construct).Label()` ("Kind Name", or
         "Kind" when Name is empty), `ConstructResolver interface { Adapter; Constructs(file string) ([]Construct, error) }`
         as the OPTIONAL half beside RangeRunner, and the `ErrASTUnavailable` sentinel. No spawn, no implementer.
         range_expand.go: `ConstructHunk = "hunk"` and `expandToConstructs(hunks []Range, cs []mutation.Construct) []EffectiveRange`:
         every hunk line covered by a top-level construct maps to that construct's FULL span (deduped by span,
         labelled Label()); every maximal run of uncovered hunk lines becomes one bare range labelled "hunk";
         output sorted by Start, pairwise non-overlapping. `EffectiveRange` gains `Construct string \`json:"construct"\``
         ADDITIVELY here — `Pad` stays until t-6 so nothing downstream breaks. range_dispatch_test's
         `rangingAdapter` gains a canned `Constructs` method (additive prep for t-6).
       contract:
         - hunk {40,41} with constructs [{10,60,FunctionDeclaration,run}] → exactly [{10,60,"FunctionDeclaration run"}];
           returning the hunk or a padded window fails TestExpandWidensToTheEnclosingConstruct
         - hunk {18,23} over [{1,20,FunctionDeclaration,a},{21,40,FunctionDeclaration,b}] → two ranges {1,20},{21,40} in
           that order; a merged {1,40} fails TestExpandSplitsAHunkAcrossConstructs
         - hunks {12,12} and {30,31} inside one construct {10,60} → ONE range; a duplicate fails TestExpandDedupesHunksInOneConstruct
         - hunk {5,7} with no construct touching 5-7 → [{5,7,"hunk"}]; a hunk past the last construct likewise; dropped or
           widened fails TestExpandKeepsUncoveredLinesAsTheBareHunk
         - hunk {18,26} where 18-20 lie in construct {1,20} and 21-26 are uncovered → [{1,20,…},{21,26,"hunk"}]; and a
           property check over 200 random hunk/construct layouts asserts no two output ranges overlap and every hunk line
           lies in exactly one output range — TestExpandOutputNeverOverlaps
         - empty construct list → every hunk returned verbatim labelled "hunk" — TestExpandWithNoConstructsIsTheRawHunks
         - a Construct with empty Name labels as the bare Kind ("ExpressionStatement"), never "Kind " with a trailing
           space — TestLabelOmitsAMissingName
       depends_on: []

  t-2  Kill verify.go:115 reuse-report survivor at command level  [risk+mvp+verification]
       files:    internal/cmd/verify_reuse_report_test.go
       covers:   c-6
       description:
         Two command-level tests through `runCmd(t, Verify(), …)` on a scopedVerifyRepo with a .go-only phase and
         `configuredAdaptersFn` stubbed to return [gremlins stub, a zero `*mutation.Stryker{}`] — the real type is
         needed because applyReuseReport type-asserts `*mutation.Stryker`, and a .go-only phase guarantees it is never
         dispatched, so nothing spawns. After the next verify run no longer lists survivor a22f0ab51a69e9e8, close
         the routed deferred (82e3e60f30734120) via `dross deferred` unroute + dismiss on mutation-range-provenance
         (deferred ec9abaedcbc6fce4 records that two-step gap). Do not `survivor accept` — the criterion names the
         test as the fix.
       contract:
         - CONDITIONALS_NEGATION on `if reuseReport` makes the plain `dross verify <phase>` call applyReuseReport → either
           the "no stryker adapter" error or `Stryker.ReuseReport` flipped true; TestPlainVerifyLeavesReuseReportOff asserts
           nil error AND ReuseReport == false afterwards, so it fails on both arms
         - negation skips the refusal under `--reuse-report --detach`, so the command proceeds to detachRequiresAHost and
           errors with a DIFFERENT message; TestReuseReportWithDetachRefusesThroughTheCommand asserts the error contains
           "--reuse-report with --detach" and fails
         - verify-time step, not a test: the phase's verify run must not re-list a22f0ab51a69e9e8; if it does, the tests
           did not reach the mutant and the task is not done
       depends_on: []

  t-3  Filter the collected gremlins leg to adapter-supported files  [risk+verification+mvp]
       files:    internal/mutation/adapter.go, internal/mutation/adapter_test.go,
                 internal/cmd/verify.go, internal/cmd/verify_results_test.go
       covers:   c-5
       description:
         `mutation.Supported(a Adapter, files []string) []string` (order-preserving). `collectDetachedFrom`
         (internal/cmd/verify.go:704) passes `Supported(g, files)` to BOTH `LanguageRun.Files` and `PlanRanges`, so the
         detached gremlins leg records only .go files. The attached path already dispatches by Supports(); that invariant
         is pinned, not re-implemented (see Disagreement 5 for the rejected PlanRanges-level filter).
       contract:
         - a detached collect whose scope holds a.go, README.md, assets/prompts/verify.md records leg.Files == [a.go] and
           whole_file with exactly one key (a.go → adapter-lacks-range-runner), i.e. len(WholeFile) == number of .go files
           in scope; README.md anywhere in the leg fails TestCollectLegListsOnlyMutableFiles (extends
           TestCollectRecordsWholeFileProvenance's fixture); a dropped a.go fails the same test
         - `Supported(gremlins, [a.go, README.md, b.go])` == [a.go, b.go], order kept — TestSupportedKeepsOrderAndDropsUnsupported
         - attached-path pin: RunScoped over the same three files against a .go-only adapter never lists README.md on the
           leg (it lands in Skipped) — TestRunScopedLegNeverListsUnsupportedFiles
       depends_on: []

  t-4  Register node in the argfence policy table  [verification+mvp]
       files:    internal/argfence/policy.go, internal/argfence/argfence_test.go,
                 internal/cmd/subprocargs_audit_test.go
       covers:   c-1
       description:
         `table` gains `"node": {Kind: Reject, Why: "node reads options ahead of the script operand and honours no
         end-of-options token; the only positional dross passes is the literal `-`, the request rides on stdin"}`.
         TestPolicyCoversEveryKnownBinary's want-list (argfence_test.go:61) and subprocargs' `valueTakingFlags` gain
         "node" (an empty flag set). Landed ahead of the spawn so t-7's commit cannot go red on the audit.
       contract:
         - "node" missing from the table fails TestPolicyCoversEveryKnownBinary on the key set and
           TestAuditKnowsEveryPolicyBinary on the missing valueTakingFlags entry; present in valueTakingFlags without a
           policy fails the same test's stale-entry arm
         - argfence.Fence("node", …) refuses a leading-dash derived value — TestFenceRejectTools' table gains a node case
       depends_on: []

Wave 2 (depends t-1)
  t-5  Print the construct, not the pad, in cmd readouts  [risk; format from mvp+verification]
       files:    internal/cmd/verify.go, internal/cmd/verifyscope.go, internal/cmd/verifyscope_test.go,
                 internal/cmd/verify_scoping_test.go, internal/cmd/verify_range_e2e_test.go
       covers:   c-3
       description:
         printRangeProvenance (verify.go ~1517): drop the pad hunt; print `ranged stryker N file(s)` then one line per
         range `  <file>:<start>-<end>  <construct>`. verifyscope.go:282: `10-48 (FunctionDeclaration tally)` in place
         of `(pad 25)`; an EffectiveRange with empty Construct prints `10-48 (construct unrecorded)`. verifyscope_test's
         provenance fixture stops setting Pad and asserts the label in text and `"construct":` in --json; the e2e test
         drops its `pad 25` assertion but keeps {15,66} until t-6 changes behaviour; `stubRangeAdapter`
         (verify_scoping_test.go:597) gains a canned `Constructs` returning [{30,50,FunctionDeclaration,edited}]
         (additive prep for t-6). After this task NO file under internal/cmd reads `.Pad`.
       contract:
         - a tests.json whose ranges carry `"pad": 25` and no `construct` loads, and `dross verify scope` prints
           `(construct unrecorded)` for that range and never the word `pad` — TestVerifyScopeOnAPadEraRecordSaysUnrecorded
         - `dross verify scope` on a record with construct "FunctionDeclaration tally" prints `1-37 (FunctionDeclaration tally)`
           beside the raw `10-12` — TestVerifyScopePrintsRawAndEffective (rewritten); `--json` contains
           `"construct": "FunctionDeclaration tally"` and no `"pad"` key — TestVerifyScopeJSONIsTheRecordVerbatim (kept, re-pointed)
         - a run through Verify() with the stub prints `ranged stryker 1 file(s)` and a per-range line naming the
           construct, and never "pad" — TestVerifyOutputNamesTheConstructPerRange; TestVerifyOutputNamesWholeFileLegs'
           "(pad" needle is replaced by the construct line
         - `grep -rn "\.Pad\b" internal/cmd` is empty after the commit (checked by the executor; pinned repo-wide by t-8)
       depends_on: [t-1]

Wave 3 (depends t-1, t-5)
  t-6  Retire the pad and wire construct planning  [risk; contracts grafted from verification+mvp]
       files:    internal/verify/range_provenance.go, internal/verify/verify.go,
                 internal/verify/range_provenance_test.go, internal/verify/range_dispatch_test.go,
                 internal/verify/range_record_test.go, internal/verify/range_summary_test.go,
                 internal/cmd/verify_scoping_test.go, internal/cmd/verify_range_e2e_test.go
       covers:   c-1, c-2, c-3, c-4
       description:
         Delete hunkContextLines, padAndMerge, EffectiveRange.Pad, LegSummary.Pad, legProvenance's pad return (both
         Skeleton call sites) and every comment describing the pad. `WholeFileASTUnavailable = "ast-unavailable"` joins
         the closed reason set. `type ASTIndex map[string]ASTResult{Constructs []mutation.Construct; Err error}`.
         `PlanRanges(a, files, scope, asts ASTIndex)` stays pure; per file: absent-from-hunks → malformed-range (raw
         hunk, before any resolution) → ast-unavailable (missing entry OR Err, on a RangeRunner) with Degraded line
         `"<tool>: AST unavailable for <file> (<detail>); mutating the whole file"` → ranged: Ranges = expandToConstructs,
         Dispatch projected from the same values in the same loop. `resolveConstructs(a, files, scope) ASTIndex` in
         verify.go runs BEFORE PlanRanges inside RunScoped, only when `a` is a RangeRunner and the scope has hunks,
         only for files whose hunks are present and well-formed; a RangeRunner that is not a ConstructResolver gets
         Err "adapter has no construct resolver" per such file. legProvenance renders `file:start-end (construct)`.
         collectDetachedFrom passes nil. Pad-pinning tests (TestPadAndMerge* ×5, TestRunScopedPassesPaddedRanges,
         TestEffectiveRangesArePostPadPostMerge, TestLegRecordsEffectiveRangesWithPad, TestGremlinsLegHasNoPad) are
         replaced by the construct equivalents; the cmd e2e assertion becomes the stub's {30,50} labelled
         "FunctionDeclaration edited"; a second cmd stub WITHOUT Constructs exercises the printed warning.
       contract:
         - c-4: a rangingAdapter whose Constructs returns ErrASTUnavailable("parse error at 12:4") for a.ts →
           WholeFile[a.ts] == "ast-unavailable" exactly (never the detail), Scope.Degraded contains "parse error at 12:4",
           Dispatch has no a.ts, and a sibling file whose resolver succeeds is still ranged in the same plan —
           TestASTUnavailableDegradesAndFallsWholeOnlyItsOwnFile (mirrors TestMalformedHunkFallsBackOnlyItsOwnFile)
         - a RangeRunner without Constructs, scope with hunks → every hunked file ast-unavailable, Degraded non-empty, and
           RunScoped calls Run, not RunRanges — TestRangeRunnerWithoutResolverDegrades
         - a counting resolver sees ZERO calls for a plainAdapter leg, a hunk-less scope, a file absent from hunks, and a
           file whose raw hunk is malformed; exactly one call per rangeable file otherwise, and every call happens BEFORE
           RunRanges — TestResolverIsAskedOnlyForRangeableFilesAndBeforeDispatch
         - TestMalformedRawHunkIsCaughtBeforePadding survives renamed …BeforeExpansion: {0,3} still records malformed-range
           with the resolver never called
         - a resolver error falls open, never aborts: the leg has Mutation set, Error empty, WholeFile all
           ast-unavailable — TestResolverFailureStillRunsTheLeg
         - `reflect.TypeOf(EffectiveRange{}).FieldByName("Pad")` and `…(LegSummary{})…` are absent; a marshalled LanguageRun
           has no "pad" key and every range a non-empty "construct" — TestRecordCarriesConstructNotPad, TestLegSummaryHasNoPad
         - TestWholeFileReasonsAreAClosedSet extended: exactly five constants, "ast-unavailable" among them
         - verify.toml leg `ranges` entry is `src/a.ts:10-48 (FunctionDeclaration tally)` and the rendered TOML has no
           `pad =` line — TestVerifyTomlStatesEffectiveRanges (rewritten); TestGremlinsLegHasNoRanges (renamed from
           …HasNoPad); TestLegProvenanceIsSorted holds on the construct-bearing strings
         - PlanRanges(&Gremlins{}, files, scope, nil) still returns adapter-lacks-range-runner with no panic —
           TestNilIndexOnANonRangeAdapter
         - TestRecordedRangesAreTheDispatchedRanges continues to hold: plan.Ranges projected to {Start,End} DeepEquals plan.Dispatch
         - cmd, c-4 printed warning: the stub without Constructs → stdout carries `scope degraded: stryker: AST unavailable
           for internal/pkg/c.go (…)` and `whole-file stryker … ast-unavailable`, and the word "ranged" never appears —
           TestVerifyOutputWarnsWhenTheASTIsUnavailable
         - cmd e2e: TestRangedDispatchReachesTheAdapterEndToEnd re-pointed — the stub's Constructs is asked for
           internal/pkg/c.go, RunRanges receives {internal/pkg/c.go: [{30,50}]} keyed by the repo-relative slash path,
           and tests.json records {30,50,"FunctionDeclaration edited"}, never the raw hunk
       depends_on: [t-1, t-5]

Wave 4 (depends t-6)
  t-7  Resolve TS constructs via Babel under node  [risk; stdin prelude from mvp+verification; buildCmd seam from verification]
       files:    internal/mutation/construct.go, internal/mutation/astspan.js, internal/mutation/construct_test.go
       covers:   c-1, c-4
       description:
         `(*Stryker).Constructs(file)`: refuse `.svelte` up front (ErrASTUnavailable "no parser for .svelte", no spawn);
         read the source from filepath.Join(ProjectRoot, file) (the inapplicable.go pattern); build stdin =
         `const __dross = <json.Marshal({"file":…,"source":…})>;\n` + the embedded astspan.js (//go:embed) — the
         script never touches the filesystem beyond require.resolve; spawn via `strykerBuildCmd(s, []string{"node","-"})`
         with cmd.Dir = s.workDir(), a 30s deadline (unexported var so tests lower it), NEVER via launcherCommand/
         newLauncher (planning_locus). argv is two literals; nothing derived reaches it. Script: createRequire from
         `node_modules/@stryker-mutator/instrumenter/package.json` under cwd → `@babel/parser`; plugins ["typescript"]
         for .ts/.js/.mjs/.cjs and ["typescript","jsx"] for .tsx/.jsx; sourceType "module"; errorRecovery false; emits
         `{"constructs":[{start,end,kind,name}]}` for every Program.body node (span = the outermost node incl. the
         Export{Named,Default}Declaration wrapper; kind/name from the unwrapped declaration — Function/Class/
         TSInterface/TSTypeAlias/TSEnum `.id.name`, VariableDeclaration first declarator, "" otherwise) or
         `{"error":{"message","line","column"}}` with exit 2. Go classifies: exec.ErrNotFound → "node not on PATH";
         error JSON → "parse error at L:C: msg" or "cannot resolve @babel/parser from <workdir>"; deadline → "timed out
         after 30s"; undecodable stdout → "malformed resolver output"; every arm wraps ErrASTUnavailable. Failure-arm
         tests use a fake `node` on PATH (t.TempDir shell script) — no Babel needed.
       contract:
         - TestEverySpawnSiteGatedOrExempt stays green with no //dross:exec-exempt marker: the spawn is reached from
           Verify's requireExecConsent via RunScoped → resolveConstructs → ConstructResolver fan-out (the same reach
           that gates RunRanges); marked exempt instead, the audit reports it as a finding
         - a Stryker with Prefix "docker compose exec -T app" builds argv [docker compose exec -T app node -] and with
           Remote set still spawns locally — TestConstructsHonoursPrefixAndNeverTheRemote: launcherCommand swapped for a
           func that t.Fatal()s, PATH emptied so the local spawn fails fast, the stub is never called
         - happy path with a fake node echoing canned JSON: Constructs returns [{10,60,"FunctionDeclaration","run"}], the
           fake's recorded argv is exactly ["node","-"], cwd == workDir, and its captured stdin contains
           `"file":"src/a.ts"` and the file's source — TestConstructsPassesTheRequestOnStdinNotArgv
         - failure arms, each errors.Is(err, ErrASTUnavailable): node absent → "node not on PATH"; exit 2 with
           `{"error":{"line":12,"column":4}}` → "parse error at 12:4"; exit 2 with a resolve error → names "@babel/parser";
           prints `nope` → "malformed resolver output"; sleeps past the lowered deadline → "timed out" —
           TestConstructsClassifies<Cause> ×5
         - Constructs("web/src/App.svelte") returns ErrASTUnavailable "no parser for .svelte" and the fake node records ZERO
           invocations — TestSvelteIsUnavailableWithoutASpawn
         - astspan.js is embedded and non-empty — TestEmbeddedScriptIsNonEmpty
       depends_on: [t-6]

  t-8  Purge pad residue from docs and gate it  [risk; calibration from verification; architecture check from mvp]
       files:    ARCHITECTURE.md, assets/prompts/verify.md, README.md,
                 internal/cmd/pad_residue_test.go, internal/cmd/testdata/pad_residue/fixture.txt
       covers:   c-2
       description:
         Rewrite ARCHITECTURE.md §mutation scoping (L567-586: padded hunks → outermost top-level AST construct spans via
         Babel out of Stryker's tree; reason list gains ast-unavailable, degrading like malformed-range; ranges carry
         `construct`; re-point the RunScoped/PlanRanges/e2e symbol bullets and run `dross architecture check`),
         assets/prompts/verify.md L74 (`file → [{start, end, construct}]`, five reasons, no "padded"), README.md L222
         (`start-end (<construct>)` in place of `(pad N)`, five reasons). pad_residue_test.go walks the three docs and
         every non-test .go under internal/ and cmd/.
       contract:
         - TestNoPadHeuristicResidue fails naming file:line if `hunkContextLines`, `padAndMerge`, `json:"pad"`, `toml:"pad`,
           `(pad `, `post-pad` or `padded hunk` appears in ARCHITECTURE.md, assets/prompts/verify.md, README.md or any
           non-test .go under internal/ or cmd/; it also fails if the four old reason names plus `ast-unavailable` are not
           all present in verify.md and README.md (the docs must describe the NEW closed set)
         - TestPadResidueScanTripsOnItsFixture runs the same scanner over testdata containing `const hunkContextLines = 25`
           and fails if it reports nothing — a scan that stopped matching cannot pass silently
         - ARCHITECTURE.md names `expandToConstructs` and `ast-unavailable` — TestArchitectureDescribesConstructRanges;
           `dross architecture check` reports no stale bullet (executor runs it and commits the fix)
       depends_on: [t-6]

Wave 5 (depends t-7)
  t-9  Prove the pad-lost mutant is recovered on the fixture  [risk+verification]
       files:    internal/mutation/testdata/ts-project/src/long.ts, internal/mutation/testdata/ts-project/src/long.test.ts,
                 internal/mutation/construct_e2e_test.go, internal/mutation/ts_fixture_test.go
       covers:   c-1, c-4
       description:
         long.ts: `export function big()` whose body runs ≥ 40 lines with a mutable statement at line ≥ 30 into it,
         a blank line, then `export function small<T>(xs: T[]): T` (the generic makes the real parser prove the
         no-jsx plugin choice on a .ts file). long.test.ts covers BOTH so the existing whole-file fixture run keeps
         its score. construct_e2e_test.go gates on e2eSkipReason()/DROSS_REQUIRE_E2E like stryker_e2e_test: (1)
         Constructs("src/long.ts") returns big's span starting at its `export function` line and ending before
         small's first line; (2) RunRanges with the changed line alone yields N1 mutants for long.ts, with the
         construct span N2; assert N1 < N2. ts_fixture_test pins that long.ts stays ≥ 40 lines and stays covered.
       contract:
         - the phase's acceptance: TestConstructRangeRecoversTheDeepMutant fails if N2 <= N1 (no mutant recovered) — the
           strictly-greater count IS the mutant the pad heuristic lost; it also fails if constructs[0].Start != the
           `export function big(` line read from the fixture, or constructs[0].End >= small's first line (the range leaked)
         - TestBabelParsesGenericsAndJSX: a .ts with `<T>` generics resolves (no jsx plugin), a .tsx snippet with JSX resolves
           (jsx plugin), and a .ts containing JSX returns ErrASTUnavailable with a parse position — against the real
           @babel/parser in the fixture tree, skipped without it
         - two hunks inside big() → one construct; a hunk on the blank line between big and small → Kind "hunk" with its
           own span; a hunk straddling big's last line and small's first → both constructs — TestRealParserSpansMatchTheExpansion
         - c-4 real cause: Workdir pointed at a t.TempDir() with no node_modules → ErrASTUnavailable whose detail names
           "@babel/parser" — TestUnresolvableParserIsUnavailable
         - TestDeepFixtureStaysDeep fails if big's body is < 40 lines or long.test.ts stops importing ./long;
           TestFixtureTestCoversTheSource extended to long.ts
       depends_on: [t-7]

Coverage
| criterion | tasks |
|---|---|
| c-1 | t-1 (pure expansion), t-4 (argfence), t-6 (wiring), t-7 (resolver), t-9 (fixture proof) |
| c-2 | t-6 (code deletion), t-8 (docs + calibrated residue gate) |
| c-3 | t-1 (shape), t-5 (readouts), t-6 (record + verify.toml) |
| c-4 | t-6 (reason + Degraded + printed warning), t-7 (cause classification), t-9 (real parser cause) |
| c-5 | t-3 |
| c-6 | t-2 |

## Disagreements

1. **Wave structure around the pad deletion (5 vs 3 vs 4 waves).**
   risk: cmd readers stop reading `Pad` in wave 2 (additive), deletion in wave 3. mvp and verification: delete
   `EffectiveRange.Pad` in wave 2 with the cmd readers updated in wave 3.
   Default: risk's ordering. `internal/cmd/verifyscope.go:282` and `internal/cmd/verify.go:1523` read `.Pad`
   (verified), so the mvp/verification sequence produces a commit that does not compile in internal/cmd.
   Why it matters: a red commit is exactly what the execute rules forbid; the cost is one extra serial wave.

2. **Interface shape: separate optional `ConstructResolver` vs widening `RangeRunner`.**
   risk and verification: a second optional interface. mvp: add `Constructs` to `RangeRunner` so "range-capable but
   construct-blind" cannot exist.
   Default: separate optional interface. Every existing RangeRunner stub keeps compiling across waves, and the
   "RangeRunner without resolver" state is what gives the whole degrade path a node-free unit test (t-6).
   Why it matters: mvp's argument is real — the merged plan answers it by making that state `ast-unavailable`
   and degraded, never bare-hunk, so precision is never silently lost.

3. **Where hunk→construct expansion runs.**
   risk: the script dumps every Program.body span; Go's `expandToConstructs` does the overlap/dedupe/bare-hunk
   logic. mvp and verification: the script receives the hunks and returns already-expanded ranges.
   Default: Go (risk). Straddle, dedupe, bare-hunk and non-overlap are then unit-tested without node (t-1), and the
   JS stays a dumb span dumper. Verification's real-parser cases for the same behaviours are kept as t-9's
   TestRealParserSpansMatchTheExpansion so the two halves are proven to agree.
   Why it matters: with expansion in JS the core logic is only tested when node and the fixture are present.

4. **Spawn shape: request channel, granularity, who reads the source.**
   risk: per file, `DROSS_AST_FILE` env var, node reads the file, direct `exec.CommandContext`. mvp: per file, JSON
   prelude on stdin, own `constructBuildCmd` seam. verification: ONE spawn per leg carrying every file's source on
   stdin, via `buildCmd`.
   Default: per file (matches the `Constructs(file)` seam from t-1), JSON prelude on stdin carrying the source
   (2 of 3 chose stdin; Go reading the source avoids a second path vocabulary between repo-relative hunk keys
   and the workdir cwd). Per-leg batching is a later optimisation if spawn count ever shows up.
   Why it matters: the argfence and audit posture is identical either way; the difference is N node startups vs 1.

5. **Prefix (docker) handling.**
   risk: bare `exec.Command("node","-")`, never via Prefix or Launcher — a docker-prefixed project is
   `ast-unavailable`. verification: through `strykerBuildCmd` so a Prefix is honoured; flags that `docker compose
   exec` without `-T` may not forward stdin. mvp: silent on Prefix.
   Default: honour Prefix via `strykerBuildCmd`, never the launcher. A docker project's node_modules live inside
   the container, so risk's choice costs every docker project all of its narrowing on every run; the planning_locus
   lock forbids the REMOTE, not a local container. Reusing the seam also adds no new spawn site for the audit.
   Why it matters: a wrong call here is invisible in dross's own (native) dogfood and only bites a docker project.

6. **c-5 fix locus.**
   risk and mvp: the detached collector only (`mutation.Supported` + pinned RunScoped invariant). verification: the
   collector plus a defensive `Supports()` filter inside PlanRanges.
   Default: collector only. The collector is the verified sole offender; a PlanRanges-level filter is a second
   owner for the same risk and an edit collision with t-6's rewrite. The attached invariant is pinned by test instead.
   Why it matters: c-5 says the leg's recorded files are filtered; both approaches satisfy it, one with less surface.

7. **Real-Babel proof harness.**
   risk and verification: the in-repo ts-project fixture gated by `e2eSkipReason()`/`DROSS_REQUIRE_E2E`. mvp: an
   external `$DROSS_BABEL_WORKDIR` project, skipped when unset.
   Default: the fixture. `node_modules/@babel/parser` is already installed there (verified) and the CI leg already
   fails loudly when the toolchain is missing; an env-var-gated test would skip on the very machine that can run it.

8. **Proof assertion shape.**
   risk: three Stryker runs, `N1 < N2 && N2 == N3`. verification: two runs, `N2 > N1`, plus a leak check.
   Default: verification's inequality plus a STRUCTURAL leak check (construct End < small's first line). Risk's
   `N2 == N3` is wrong on its own fixture — a second function in the file makes whole-file N3 exceed the
   construct-span N2. Verification's survivor-based leak check assumed `small` is uncovered, which contradicts its
   own fixture description and would dent the existing whole-file fixture run; the span check needs no such assumption.
   Why it matters: this is the phase's acceptance test; a vacuous or mis-stated assertion proves nothing.

9. **`.svelte` handling.**
   risk: refuse before spawning ("no parser for .svelte"). mvp: no special case, let Babel fail with a parse
   position. verification: silent.
   Default: refuse pre-spawn. Same closed reason, no doomed process per run, and the Degraded line names the
   real cause instead of a Babel position inside a `<script>` block.

10. **Task packaging of c-5 and c-6.**
    mvp: one task. risk and verification: two.
    Default: two. They share only internal/cmd/verify.go's neighbourhood, c-6 is test-only, and an atomic commit
    per criterion keeps each survivor/route closure attributable.

11. **argfence registration: standalone task or folded into the resolver.**
    verification: standalone wave-1 task (3 files). risk and mvp: folded into the resolver task, and both omit
    `internal/cmd/subprocargs_audit_test.go`, which `TestAuditKnowsEveryPolicyBinary` requires (verified).
    Default: standalone in wave 1 (t-4). It is additive, parallel, and lands the three edits before the spawn exists.

12. **Readout format.**
    risk: `10-48 FunctionDeclaration tally` and a `ranged stryker N file(s), M construct(s)` count line. mvp and
    verification: `10-48 (FunctionDeclaration tally)` and one line per range in verify output.
    Default: parenthesised label (it fills the exact slot `(pad N)` vacated) and per-range lines (2 of 3).
    Why it matters: c-3 only mandates verify.toml and `verify scope`; the verify-output choice is UX, but the two
    surfaces should agree on the label form so tests can share one needle.
