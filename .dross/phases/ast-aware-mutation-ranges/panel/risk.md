# Risk-lens plan — ast-aware-mutation-ranges

Lens: start from what breaks. Every failure mode below is owned and tested by exactly one task; the graph is shaped so no commit lands red (the exec-consent audit flags an unreachable spawn site, so the spawn cannot precede its wiring) and so the four test files that pin the pad die with the pad, not before it.

## Failure modes → owner

| # | What breaks | Owner |
|---|---|---|
| F1 | Changed line >25 lines into a function: enclosing-block mutant never generated (the bug) | t-1 (pure), t-8 (real Stryker proof) |
| F2 | Hunk straddles two top-level constructs → must yield two ranges, not one merged blob | t-1 |
| F3 | Two hunks inside one construct → one range, never two `--mutate` specs for one span | t-1 |
| F4 | Hunk lines outside any construct (blank/comment/between statements/past EOF) → bare `hunk` range, never dropped, never widened | t-1 |
| F5 | A bare-hunk line that also lies inside a construct emitted twice (overlap) | t-1 |
| F6 | Malformed raw hunk must still be caught BEFORE expansion (no resolver call, `malformed-range` unchanged) | t-5 |
| F7 | AST unobtainable (no `node`, parser unresolvable, parse error, timeout, garbage stdout) silently reads as ranged | t-6 (classification) + t-5 (reason + Degraded) |
| F8 | A RangeRunner that cannot resolve constructs falls back silently | t-5 |
| F9 | Resolver spawned for files it must not be (gremlins leg, hunk-less scope, malformed hunk, absent-from-hunks) | t-5 |
| F10 | The `node` spawn is not exec-consent gated / not argv-fenced / carries a derived positional | t-6 |
| F11 | The spawn goes through the docker Prefix or the remote Launcher (planning_locus lock) | t-6 |
| F12 | `.svelte` in hunks: Babel cannot parse it; must be `ast-unavailable` without a doomed spawn | t-6 |
| F13 | `.ts` parsed with the jsx plugin breaks on generics; `.tsx` without it breaks on JSX | t-8 (real parser) |
| F14 | Old tests.json (has `pad`, no `construct`) fails to load or prints as ranged-with-construct | t-4 |
| F15 | verify.toml/tests.json still carry a `pad` field | t-5 |
| F16 | Gremlins leg lists README.md / *.md, whole-file count over-reads | t-3 |
| F17 | Routed survivor verify.go:115 survives again | t-2 |
| F18 | Pad prose creeps back into docs/prompt/comments | t-7 (grep gate) |
| F19 | Detached (gremlins-only) collect path breaks when PlanRanges gains a parameter | t-5 |

## Plan

Phase ast-aware-mutation-ranges — 8 tasks across 5 waves

Wave 1
  t-1  Add construct seam and pure hunk expansion
       files:    internal/mutation/construct.go, internal/verify/range_expand.go, internal/verify/range_expand_test.go, internal/verify/range_dispatch_test.go
       covers:   c-1 (pure half), c-3 (shape)
       description:
         internal/mutation/construct.go: `Construct{Start,End int; Kind,Name string}`, `Construct.Label()`
         ("Kind Name" or "Kind"), `ConstructResolver interface { Adapter; Constructs(file string) ([]Construct, error) }`
         (optional half, RangeRunner's pattern), `ErrASTUnavailable` sentinel. No spawn, no implementer yet.
         internal/verify/range_expand.go: `ConstructHunk = "hunk"` and `expandToConstructs(hunks []Range,
         cs []mutation.Construct) []EffectiveRange` — per hunk, every line covered by a top-level construct maps
         to that construct's FULL span (deduped by span, labelled Label()); every maximal run of uncovered lines
         becomes one bare range labelled "hunk"; output sorted by Start, pairwise non-overlapping. EffectiveRange
         gains `Construct string` here ADDITIVELY (Pad stays until t-5 so nothing downstream breaks).
         range_dispatch_test.go: `rangingAdapter` gains a `Constructs` method returning a canned list (additive prep).
       contract:
         - F1: hunk {40,41} with constructs [{10,60,FunctionDeclaration,run}] → exactly [{10,60,"FunctionDeclaration run"}]; if the expansion returns the hunk or a padded window, TestExpandWidensToTheEnclosingConstruct fails
         - F2: hunk {18,23} over constructs [{1,20,FunctionDeclaration,a},{21,40,FunctionDeclaration,b}] → two ranges {1,20},{21,40} in that order; a merged {1,40} fails TestExpandSplitsAHunkAcrossConstructs
         - F3: hunks {12,12} and {30,31} inside one construct {10,60} → ONE range; a duplicate fails TestExpandDedupesHunksInOneConstruct
         - F4: hunk {5,7} with no construct touching lines 5-7 → [{5,7,"hunk"}]; hunk past the last construct (EOF) likewise; a dropped or widened result fails TestExpandKeepsUncoveredLinesAsTheBareHunk
         - F5: hunk {18,26} where 18-20 are inside construct {1,20} and 21-26 are uncovered → [{1,20,...},{21,26,"hunk"}], and a property check over 200 random hunk/construct layouts asserts no two output ranges overlap and every hunk line is inside exactly one output range — TestExpandOutputNeverOverlaps
         - empty constructs list → every hunk returned verbatim as "hunk" (TestExpandWithNoConstructsIsTheRawHunks)
       depends_on: []
       status: pending

  t-2  Kill verify.go:115 reuse-report survivor at command level
       files:    internal/cmd/verify_reuse_report_test.go
       covers:   c-6
       description:
         Two command-level tests through `runCmd(t, Verify(), …)` on a scopedVerifyRepo: (a) a plain `dross verify`
         with an adapter list [gremlins stub for .go, a zero `*mutation.Stryker{}` that Supports no in-scope file]
         succeeds and leaves `Stryker.ReuseReport == false` afterwards; (b) `dross verify <p> --reuse-report --detach`
         returns the applyReuseReport refusal text through the command. After the next verify run no longer lists
         a22f0ab51a69e9e8, close the routed deferred (82e3e60f30734120) via `dross deferred unroute` + dismiss (two
         hand steps — deferred ec9abaedcbc6fce4 records that gap).
       contract:
         - F17 arm 1: CONDITIONALS_NEGATION on `if reuseReport` makes the plain run call applyReuseReport → either the "no stryker adapter" error (stub-only list) or ReuseReport flipped true; TestPlainVerifyLeavesReuseReportOff fails on both
         - F17 arm 2: negation skips the refusal under `--reuse-report --detach`, so the command proceeds to detachRequiresAHost and errors with a DIFFERENT message; TestReuseReportWithDetachRefusesThroughTheCommand asserts the "--reuse-report with --detach" text and fails
       depends_on: []
       status: pending

  t-3  Filter leg files to adapter-supported ones
       files:    internal/mutation/adapter.go, internal/mutation/adapter_test.go, internal/cmd/verify.go, internal/cmd/verify_results_test.go
       covers:   c-5
       description:
         `mutation.Supported(a Adapter, files []string) []string` (order-preserving). collectDetachedFrom
         (internal/cmd/verify.go ~L704) passes `Supported(g, files)` to BOTH LanguageRun.Files and PlanRanges,
         so the detached gremlins leg records only .go files. RunScoped already dispatches by Supports(); that
         invariant is pinned rather than re-implemented.
       contract:
         - F16: a detached collect whose scope holds a.go, README.md, assets/prompts/verify.md records leg.Files == [a.go] and whole_file with exactly one entry; TestCollectLegListsOnlyMutableFiles fails if README.md appears in either
         - `Supported(gremlins, [a.go, README.md, b.go])` == [a.go, b.go] (order kept); TestSupportedKeepsOrderAndDropsUnsupported
         - attached path pin: RunScoped with the same three files against a .go-only adapter never lists README.md on the leg (it lands in Skipped) — TestRunScopedLegNeverListsUnsupportedFiles; guards against a future caller widening byAdapter
       depends_on: []
       status: pending

Wave 2 (depends t-1)
  t-4  Print construct, not pad, in verify readouts
       files:    internal/cmd/verify.go, internal/cmd/verifyscope.go, internal/cmd/verifyscope_test.go, internal/cmd/verify_scoping_test.go, internal/cmd/verify_range_e2e_test.go
       covers:   c-3 (readout half)
       description:
         printRangeProvenance: `ranged stryker N file(s), M construct(s)` (M = total ranges), no pad read.
         verifyscope: `10-48 FunctionDeclaration tally` per range; an EffectiveRange with empty Construct prints
         `10-48 (construct unrecorded)`. verifyscope_test's provenanceFixture stops setting Pad and asserts the
         construct label in text and `"construct":` in --json; e2e test drops its `pad 25` assertion (keeps {15,66}
         until t-5 changes behaviour); `stubRangeAdapter` gains a `Constructs` method returning a canned
         [{30,50,FunctionDeclaration,edited}] (additive prep for t-5).
       contract:
         - F14: a tests.json whose ranges carry `"pad": 25` and no `construct` loads and `dross verify scope` prints `(construct unrecorded)` for that range and never the word `pad`; TestVerifyScopeOnAPadEraRecordSaysUnrecorded fails if either happens
         - c-3: `dross verify scope` on a record with construct "FunctionDeclaration tally" prints that label on the range line; `--json` contains `"construct": "FunctionDeclaration tally"` and no `"pad"` key — TestVerifyScopePrintsTheConstruct
         - printRangeProvenance never prints "pad": TestVerifyOutputNamesConstructCount asserts `ranged stryker 1 file(s), 1 construct(s)` on a stubbed ranged run
       depends_on: [t-1]
       status: pending

Wave 3 (depends t-1, t-4)
  t-5  Retire the pad and wire construct planning
       files:    internal/verify/range_provenance.go, internal/verify/verify.go, internal/verify/range_provenance_test.go, internal/verify/range_dispatch_test.go, internal/verify/range_record_test.go, internal/verify/range_summary_test.go, internal/cmd/verify_range_e2e_test.go
       covers:   c-1 (wiring), c-2 (code), c-3 (record), c-4 (plan-level)
       description:
         Delete hunkContextLines, padAndMerge, EffectiveRange.Pad, LegSummary.Pad and every comment describing the
         pad. `WholeFileASTUnavailable = "ast-unavailable"`. `type ASTIndex map[string]ASTResult{Constructs
         []mutation.Construct; Err error}`. `PlanRanges(a, files, scope, asts ASTIndex)` stays pure: for a file with
         well-formed hunks, missing entry or Err → WholeFile=ast-unavailable + Degraded line
         `"<tool>: AST unavailable for <file>: <detail>; mutating the whole file"`, else Ranges =
         expandToConstructs. `resolveConstructs(a, files, scope) ASTIndex` in verify.go runs BEFORE PlanRanges in
         RunScoped, only when a is RangeRunner and scope has hunks, only for files whose hunks are present and
         well-formed; a RangeRunner that is not a ConstructResolver gets Err "adapter has no construct resolver"
         for every such file. legProvenance emits "file:start-end construct". collectDetachedFrom passes nil.
         Pad-pinning tests (TestPadAndMerge*, TestEffectiveRangesArePostPadPostMerge, pad assertions) are replaced
         by the construct equivalents; e2e assertion becomes the stub's {30,50} with label "FunctionDeclaration edited".
       contract:
         - F7/c-4: a rangingAdapter whose Constructs returns ErrASTUnavailable("parse error at 12:4") for a.ts → WholeFile[a.ts]=="ast-unavailable", Scope.Degraded contains "parse error at 12:4", Dispatch has no a.ts, and the reason value is exactly the constant (never the detail) — TestASTUnavailableDegradesAndFallsWhole; a second file whose resolver succeeds is still ranged in the same plan (only-its-own-file, mirrors TestMalformedHunkFallsBackOnlyItsOwnFile)
         - F8: a RangeRunner without Constructs, scope with hunks → every hunked file ast-unavailable and Degraded non-empty; TestRangeRunnerWithoutResolverDegrades
         - F9: a counting resolver sees ZERO calls for a plainAdapter leg, for a hunk-less scope, for a file absent from hunks, and for a file whose raw hunk is malformed; exactly one call per ranged file otherwise — TestResolverIsAskedOnlyForRangeableFiles
         - F6: TestMalformedRawHunkIsCaughtBeforePadding survives renamed …BeforeExpansion: {0,3} still records malformed-range with the resolver never called
         - F15/c-2: `grep -rn "hunkContextLines\|padAndMerge\|Pad\b" internal --include=*.go` is empty (asserted by t-7's gate); json round-trip of a LanguageRun has no "pad" key and every range has a non-empty "construct" — TestRecordCarriesConstructNotPad
         - c-3: verify.toml leg `ranges` string is `src/a.ts:10-48 FunctionDeclaration tally`; TestVerifyTomlStatesEffectiveRanges updated fails if the label is missing
         - F19: PlanRanges(&Gremlins{}, files, scope, nil) still returns whole-file/adapter-lacks-range-runner with no panic — TestNilIndexOnANonRangeAdapter
         - TestRecordedRangesAreTheDispatchedRanges continues to hold on the construct-shaped plan (Dispatch and Ranges from one loop)
       depends_on: [t-1, t-4]
       status: pending

Wave 4 (depends t-5)
  t-6  Resolve TS constructs via Babel under node
       files:    internal/mutation/construct.go, internal/mutation/astspan.js, internal/mutation/construct_test.go, internal/argfence/policy.go
       covers:   c-1 (resolver), c-4 (causes)
       description:
         `(*Stryker).Constructs(file)`: refuse `.svelte` up front (ErrASTUnavailable "no parser for .svelte");
         exec.LookPath("node") → ErrASTUnavailable "node not on PATH"; `exec.CommandContext(30s, "node", "-")`
         with Dir = s.workDir(), stdin = embedded astspan.js (//go:embed), request via env DROSS_AST_FILE
         (workdir-relative path) — argv is two literals, no derived positional, NEVER via Launcher/Prefix/Remote.
         Script: createRequire from cwd resolves `@babel/parser` (Stryker's own tree), plugins ["typescript"] for
         .ts/.js/.mjs/.cjs and ["typescript","jsx"] for .tsx/.jsx, sourceType "module", errorRecovery false;
         emits `{"constructs":[{start,end,kind,name}]}` for every Program.body node (Export{Named,Default}Declaration
         unwrapped to the inner declaration's kind/id; VariableDeclaration → first declarator id) or
         `{"error":{"message","line","column"}}` and exit 2. Go classifies: non-zero exit with error JSON → "parse
         error at L:C: msg" / "cannot resolve @babel/parser"; timeout → "timed out after 30s"; undecodable stdout →
         "malformed resolver output". argfence policy gains `"node": Reject`.
         Tests use a fake `node` on PATH (t.TempDir shell script) for every failure arm — no Babel needed.
       contract:
         - F10: TestEverySpawnSiteGatedOrExempt (internal/cmd) enumerates the new exec.CommandContext in construct.go and passes only because it is reached from Verify's requireExecConsent via RunScoped → resolveConstructs → ConstructResolver fan-out; TestAuditKnowsEveryPolicyBinary + TestEveryCatalogToolHasAnArgvPolicy pass with `node` in the table; if the site is marked exec-exempt instead, the audit reports it as a finding
         - F11: the built *exec.Cmd has Path resolving to `node`, Args == ["node","-"], Dir == workDir, and is built by a function that never touches s.Prefix or s.Remote — TestConstructsSpawnsNodeDirectly asserts a Stryker with Prefix "docker …" and a Remote target still produces the bare argv
         - F7 arms, each with a fake node: absent → error wraps ErrASTUnavailable and says "node not on PATH"; exits 2 with `{"error":{"line":12,"column":4}}` → "parse error at 12:4"; prints `nope` → "malformed resolver output"; sleeps 60s → "timed out" (test lowers the timeout via an unexported var); each is TestConstructsClassifies<Cause> and each asserts errors.Is(err, ErrASTUnavailable)
         - F12: Constructs("web/src/App.svelte") returns ErrASTUnavailable "no parser for .svelte" and the fake node records ZERO invocations — TestSvelteIsUnavailableWithoutASpawn
         - happy path with fake node echoing canned JSON: Constructs returns [{10,60,"FunctionDeclaration","run"}] and the fake node's captured env has DROSS_AST_FILE == "src/a.ts" (workdir prefix trimmed) and cwd == workdir — TestConstructsPassesTheRequestInEnvNotArgv
         - astspan.js is embedded: TestEmbeddedScriptIsNonEmpty fails if the go:embed pattern stops matching
       depends_on: [t-5]
       status: pending

  t-7  Purge pad residue from docs and gate it
       files:    ARCHITECTURE.md, assets/prompts/verify.md, README.md, internal/cmd/pad_residue_test.go
       covers:   c-2 (docs)
       description:
         Rewrite ARCHITECTURE.md §mutation scoping (L567-586: padded hunks → top-level AST construct spans via
         Babel; reason list gains ast-unavailable; ranges carry construct), assets/prompts/verify.md L74 (`{start,
         end, construct}`, ast-unavailable in the reason list, no "padded"), README.md L222 (`start-end <construct>`
         in place of `(pad N)`, reason list). A repo-wide test greps the three docs and every non-test .go under
         internal/ and cmd/ for `hunkContextLines|padAndMerge|\bpad\b` (case-insensitive on the docs, with a
         word-boundary so "padding" in unrelated prose is judged by the executor and allowlisted by exact line
         only if it is not about mutation ranges).
       contract:
         - F18: TestNoPadHeuristicResidue fails naming file:line if `hunkContextLines`, `padAndMerge`, `pad 25`, `(pad ` or `{start, end, pad}` appears in ARCHITECTURE.md, assets/prompts/verify.md, README.md, or any non-test .go file under internal/ or cmd/; it also fails if the four reason names plus `ast-unavailable` are not all present in verify.md and README.md (the docs must describe the NEW closed set, not merely stop describing the old one)
         - execconsent_docs_test-style pin: ARCHITECTURE.md names `expandToConstructs` and `ast-unavailable` — TestArchitectureDescribesConstructRanges
       depends_on: [t-5]
       status: pending

Wave 5 (depends t-6)
  t-8  Prove the lost mutant is recovered on the fixture
       files:    internal/mutation/testdata/ts-project/src/deep.ts, internal/mutation/testdata/ts-project/src/deep.test.ts, internal/mutation/construct_e2e_test.go, internal/mutation/ts_fixture_test.go
       covers:   c-1 (acceptance proof), c-4 (real parser)
       description:
         deep.ts: one exported function whose body runs ≥ 40 lines with a mutable statement at line ≥ 30 into it
         (a switch/arith chain the test covers), plus a `.tsx`-free generic (`function pick<T>(xs: T[]): T`) so the
         real parser is exercised on TS generics. deep.test.ts covers both. construct_e2e_test.go (skips via
         e2eSkipReason, same as stryker_e2e_test): (1) Constructs("src/deep.ts") in the fixture Workdir returns the
         function's span starting at its `export function` line; (2) RunRanges with range = the changed line alone
         yields N1 mutants for deep.ts, with the construct span yields N2, whole file yields N3; assert N1 < N2 and
         N2 == N3. ts_fixture_test pins that deep.ts stays covered and stays ≥ 40 lines (a shrunken fixture no
         longer exhibits the loss).
       contract:
         - F1 (the phase's acceptance): TestConstructRangeRecoversTheDeepMutant fails if N2 < N3 (a mutant is still lost with construct ranges) or if N1 == N2 (the fixture no longer demonstrates the loss, so the proof is vacuous)
         - F13: TestBabelParsesGenericsAndJSX — a `.ts` with `<T>` generics resolves (no jsx plugin), a `.tsx` snippet with JSX resolves (jsx plugin), and a `.ts` containing JSX returns ErrASTUnavailable with a parse position — run against the real @babel/parser in the fixture tree, skipped without it
         - c-4 real cause: pointing Workdir at a temp dir with no node_modules returns ErrASTUnavailable whose detail contains "@babel/parser" — TestUnresolvableParserIsUnavailable
         - fixture pin: TestDeepFixtureStaysDeep fails if deep.ts's function body is < 40 lines or deep.test.ts stops importing it
       depends_on: [t-6]
       status: pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 | t-1 (pure expansion), t-5 (wiring), t-6 (resolver), t-8 (fixture proof) |
| c-2 | t-5 (code deletion + comments), t-7 (docs + residue gate) |
| c-3 | t-1 (shape), t-4 (readouts), t-5 (record + verify.toml) |
| c-4 | t-5 (reason + Degraded + printed warning via existing printScopeSummary), t-6 (cause classification), t-8 (real parser causes) |
| c-5 | t-3 |
| c-6 | t-2 |

All 6 criteria covered.

## Judgment calls

- **Resolver returns ALL top-level constructs of a file; Go decides which enclose hunks.** Rejected: passing the hunks into the script and letting JS expand. Keeping expansion in Go makes F1-F5 unit-testable without node, and the script stays a dumb span dumper.
- **Request travels in an env var, argv is two literals (`node -`).** Rejected: `node - <file> <ranges>`. `node` has no argfence entry today and a derived positional would need one plus fence plumbing; env sidesteps the whole class (F10) at the cost of one `Reject` policy row the audit demands anyway.
- **Spawn is local `node`, never via Launcher/Prefix/Remote.** Follows the planning_locus lock literally; a docker-prefixed or remote-only Stryker with no local node_modules is `ast-unavailable`, degraded, honest. Rejected: running the script through the launcher so remote setups keep precision — that would author the record from the remote.
- **`.svelte` refused before spawning.** Babel cannot parse Svelte; spawning to get a guaranteed parse error wastes a process per run and buries the real cause. Same reason value, clearer detail.
- **A RangeRunner without ConstructResolver is `ast-unavailable`, degraded.** Rejected: treating it like `adapter-lacks-range-runner` (informational). The adapter CAN range, the hunk existed, precision was lost — the ast_fallback_severity lock's principle.
- **Missing ASTIndex entry for a rangeable file is `ast-unavailable` ("no construct result"), not a panic and not silent whole-file.** It only happens on an internal wiring bug; making it loud in the record is how that bug gets seen.
- **c-5 fixed at the two callers (`mutation.Supported` in collectDetachedFrom; RunScoped invariant pinned), not inside PlanRanges.** Keeps t-3 in wave 1 with no edit to the file t-5 rewrites. Rejected: a PlanRanges-level filter — a second owner for the same risk and a guaranteed edit collision.
- **Wave ordering is forced by two gates, not by taste.** The exec-consent audit flags an unreachable spawn, so t-6 must follow the wiring in t-5; removing `Pad` breaks cmd compilation, so cmd readouts (t-4) must stop reading it before t-5 deletes it. The result is a 5-wave chain with parallelism only in wave 1 and wave 4.
- **t-5 is 7 files.** Four are the tests that pin the pad; they cannot outlive it and cannot precede its removal without a red commit. Splitting would trade one atomic refactor for two commits, the first of which fails `go test`.
- **Fixture proof compares three Stryker runs (hunk-only / construct / whole-file) on deep.ts.** Rejected: asserting only N2 == N3. Without N1 < N2 the fixture could silently stop exhibiting the loss and the test would prove nothing about the heuristic it retires.
- **Docs residue is a test, not a checklist.** Rejected: trusting the executor's grep. The residue rule is exactly the kind that erodes; a gate that names file:line keeps c-2 true after this phase.
