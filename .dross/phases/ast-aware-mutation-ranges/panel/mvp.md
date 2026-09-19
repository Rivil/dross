# MVP-lens draft — ast-aware-mutation-ranges

Phase ast-aware-mutation-ranges — 5 tasks across 3 waves

Bias applied: smallest task set that satisfies c-1..c-6. Every task traces to a
criterion; the two routed cmd-layer debts (c-5, c-6) are merged into one task
because each alone is a sub-10-minute single-file change. The AST work is split
by LAYER only (mutation spawn → verify planner → cmd readouts → docs), never by
speculative sub-feature.

Wave 1
  t-1  Babel construct resolver on the Stryker adapter
       files:    internal/mutation/adapter.go
                 internal/mutation/construct.go          (new)
                 internal/mutation/construct_spans.js    (new, //go:embed)
                 internal/mutation/construct_test.go     (new)
                 internal/argfence/policy.go
                 internal/argfence/argfence_test.go
       covers:   c-1, c-4 (the detail side)
       description:
         - adapter.go: add `ConstructRange{Start, End int; Construct string}` and
           widen `RangeRunner` with `Constructs(file string, hunks []Range)
           ([]ConstructRange, error)` — the ONE range-capable interface grows the
           method rather than a second optional interface, so "range-capable but
           cannot resolve constructs" is not a state that exists.
         - construct.go: `(*Stryker).Constructs` spawns `node -` with
           `cmd.Dir = ProjectRoot/Workdir`, script on stdin = a JSON prelude
           (`const __dross = {"file":…,"hunks":[[s,e],…]};`, json.Marshal'd so
           U+2028/2029 and quotes are escaped) + the embedded construct_spans.js.
           argv is the two literals `node`, `-`; nothing derived reaches argv.
           Spawn through `var constructBuildCmd = (*Stryker).constructCmd` (the
           strykerBuildCmd alias pattern) so tests swap it and the exec-consent
           reach walker follows it. Failures map to a sentinel
           `ErrASTUnavailable` wrapped with the detail: `exec.ErrNotFound` →
           "node not found on PATH"; script `{"error":…}` exit 2 → its text
           (parser unresolvable / parse error at line:col). Local only — never
           through newLauncher/remote (planning_locus lock).
         - construct_spans.js: `require.resolve('@babel/parser', {paths:
           [dirname(require.resolve('@stryker-mutator/instrumenter/package.json'))]})`;
           parse `sourceType:'unambiguous'`, plugins typescript(+jsx for .tsx/.jsx),
           errorRecovery false; walk ONLY `program.body` (expansion_unit lock:
           outermost statement below Program); a hunk overlapping statement S
           yields {S.loc.start.line, S.loc.end.line, "<S.type> <id>"} where id is
           the declaration name (Function/Class/TSInterface/TSTypeAlias/TSEnum
           `.id.name`, VariableDeclaration first declarator, Export* unwrapped to
           its `.declaration`) or absent; one entry per distinct statement;
           hunk lines enclosed by no statement stay as the bare hunk with
           construct "hunk". Emits `{"ranges":[…]}` sorted by start.
         - argfence: `"node"` policy entry (Reject; why: node parses options
           ahead of the script and dross hands it only the literal `-`, the
           request rides on stdin) + add "node" to the pinned exported-tool list
           at argfence_test.go:61.
       test_contract:
         - If the Go side mis-decodes the script's JSON or drops the construct
           label, `TestConstructsDecodesTheScriptOutput` (constructBuildCmd
           swapped for a canned-JSON fake) fails: hunk 30-31 → exactly one
           ConstructRange {1, 60, "FunctionDeclaration cascade"}.
         - If a missing node stops being classified, `TestConstructsWithoutNodeIsASTUnavailable`
           fails: the fake returns exec.ErrNotFound and the error must satisfy
           errors.Is(err, ErrASTUnavailable) with "node not found" in its text;
           a fake exiting 2 with `{"error":"parse error at 12:5: …"}` must carry
           that text verbatim.
         - If a derived value ever reaches node's argv, `TestConstructsArgvIsLiteral`
           fails: the fake records args == ["node","-"] and the file path appears
           only in the stdin prelude; the subprocargs audit stays green because
           "node" is in the policy table (TestArgfencePolicyTableIsPinned — the
           list at argfence_test.go:61 — fails if "node" is missing).
         - If construct_spans.js widens to the innermost node instead of the
           top-level statement, `TestConstructScriptWidensToTheTopLevelStatement`
           fails: real node + real @babel/parser (resolved from
           $DROSS_BABEL_WORKDIR, a project with Stryker installed; t.Skip with
           the env var named when absent) parse a fixture whose changed line is
           40 lines into an exported function nested inside a block, and the
           range must start at the `export function` line — the mutant the pad
           lost. Same test asserts a hunk on a blank line between statements
           comes back as construct "hunk".
         - If the new spawn were reachable ungated, `TestEverySpawnSiteGatedOrExempt`
           (derived enumeration of every exec.Command in internal/) reports
           construct.go's site as a finding; it must come out "gated via verify"
           with no //dross:exec-exempt marker.
       depends_on: []
       status: pending

  t-4  Close the two routed cmd debts (c-5, c-6)
       files:    internal/cmd/verify.go
                 internal/cmd/verify_detach_test.go
                 internal/cmd/verify_reuse_report_test.go
       covers:   c-5, c-6
       description:
         - c-5: in the detach-collect path (verify.go ~line 704–712) filter
           `files` to `g.Supports()` before PlanRanges and before the
           LanguageRun's `Files:`; the attached path already groups by Dispatch,
           so this is the one leg that listed README.md.
         - c-6: command-level tests in the shape of
           TestVerifyDetachDispatchesThroughTheCommand driving Verify().RunE.
           If the mutant is still listed after the tests land, do NOT accept —
           the criterion names the test as the fix.
       test_contract:
         - If the collected gremlins leg lists a non-Go file again,
           `TestCollectedLegListsOnlyMutableFiles` fails: a detached run whose
           scope holds README.md + a.go + b.go collects a leg with Files ==
           [a.go, b.go], whole_file with exactly those two keys (both
           adapter-lacks-range-runner) and no README.md anywhere in the leg.
         - If `if reuseReport` is negated (verify.go:115), 
           `TestPlainVerifyLeavesReuseReportOff` fails: `dross verify <phase>`
           with a configured `*mutation.Stryker` (run swapped via
           strykerBuildCmd → canned report so nothing real spawns) ends with
           `ReuseReport == false` on that adapter — negated, applyReuseReport
           runs and flips it true.
         - Same mutant, other arm: `TestReuseReportWithDetachRefusesThroughTheCommand`
           fails if `dross verify <phase> --reuse-report --detach` does not
           return the error containing "--reuse-report with --detach" — negated,
           the guard is skipped and the run reaches detachRequiresAHost's
           different refusal instead.
         - If the survivor a22f0ab51a69e9e8 is still routed after the tests
           land, the phase verify re-lists it — the contract is "absent from the
           next run", checked at /dross-verify.
       depends_on: []
       status: pending

Wave 2 (depends t-1)
  t-2  PlanRanges consumes constructs; delete the pad
       files:    internal/verify/range_provenance.go
                 internal/verify/verify.go
                 internal/verify/range_provenance_test.go
                 internal/verify/range_dispatch_test.go
                 internal/verify/range_record_test.go
                 internal/verify/range_summary_test.go
       covers:   c-1, c-2 (code side), c-3 (record side), c-4
       description:
         - EffectiveRange → {Start, End, Construct string `json:"construct"`};
           Pad removed. Add `WholeFileASTUnavailable = "ast-unavailable"` to the
           closed reason set.
         - New `Resolution{Constructs []mutation.ConstructRange; Unavailable string}`
           and `ResolveConstructs(a mutation.Adapter, files []string, scope *Scope)
           map[string]Resolution`: for a RangeRunner only, per file with hunks
           whose raw hunks are not malformed, calls `rr.Constructs(f, hunks)`;
           an ErrASTUnavailable error lands as Unavailable = its detail. This is
           the one impure step and it runs BEFORE PlanRanges (planning_locus lock).
         - `PlanRanges(a, files, scope, resolved map[string]Resolution)`: stays
           pure; order per file: absent-from-hunks → malformed-range →
           ast-unavailable (Unavailable set, OR no entry in resolved for a
           RangeRunner file — "constructs not resolved") with Degraded line
           `"<tool>: AST unavailable for <file> (<detail>); mutating the whole
           file"` → ranged: Dispatch = the construct spans as mutation.Range,
           Ranges = the same values with Construct, built in the same loop.
         - RunScoped: `resolved := ResolveConstructs(a, byAdapter[name], scope)`
           then `PlanRanges(a, byAdapter[name], scope, resolved)`. Delete
           hunkContextLines, padAndMerge and their comment block; delete
           LegSummary.Pad and legProvenance's pad return; LanguageRun.Ranges
           comment rewritten (no "post-pad").
         - Tests: delete TestPadAndMerge* (5), TestEffectiveRangesArePostPadPostMerge,
           TestRunScopedPassesPaddedRanges, TestLegRecordsEffectiveRangesWithPad;
           replace with the construct-era tests below. Stub RangeRunners in
           range_record_test.go / range_dispatch_test.go gain Constructs.
       test_contract:
         - If a hunk deep inside a construct no longer widens to the construct's
           first line, `TestRangesWidenToTheEnclosingConstruct` fails: stub
           Constructs returns {1,80,"FunctionDeclaration f"} for hunk 40-41 and
           RunRanges must receive {src/a.ts: [{1,80}]}, tests.json's leg must
           carry the literal `"ranges":{"src/a.ts":[{"start":1,"end":80,"construct":"FunctionDeclaration f"}]}`
           and the marshalled record must not contain the substring `"pad"`.
         - If recorded ranges and dispatched ranges are computed separately,
           `TestRecordedRangesAreTheDispatchedRanges` (kept, re-pointed) fails:
           plan.Ranges projected to {Start,End} DeepEquals plan.Dispatch.
         - If the AST fallback stops degrading, `TestASTUnavailableDegradesTheScope`
           fails: Resolution{Unavailable:"node not found on PATH"} for src/a.ts
           must yield WholeFile[src/a.ts]=="ast-unavailable", no Dispatch entry,
           and Scope.Degraded containing "AST unavailable for src/a.ts (node not
           found on PATH)"; the reason VALUE must not contain the detail
           (provenance_shape lock). Same test: a RangeRunner file with hunks and
           a nil resolved map is ast-unavailable, never ranged.
         - If the reason set silently grows or shrinks, `TestWholeFileReasonsAreAClosedSet`
           (extended) fails: exactly the five constants, "ast-unavailable" among them.
         - If the resolver spawns for a file it should not, `TestResolveConstructsSkipsMalformedAndAbsentFiles`
           fails: a stub counting Constructs calls sees zero calls for a
           malformed-hunk file, an absent-from-hunks file, and any file under a
           non-RangeRunner adapter.
         - If verify.toml's leg summary still states a pad or drops the
           construct, `TestVerifyTomlStatesEffectiveRanges` (rewritten) fails:
           the leg's `ranges` entry is `"src/a.ts:1-80 FunctionDeclaration f"`
           and the rendered TOML contains no `pad =` line.
         - If hunkContextLines or padAndMerge survives anywhere in internal/,
           `TestNoPadResidueInCode` fails: a grep over internal/**/*.go for
           `hunkContextLines|padAndMerge|\bPad\b` finds nothing.
       depends_on: [t-1]
       status: pending

Wave 3 (depends t-2)
  t-3  Print the construct in verify output and `verify scope`
       files:    internal/cmd/verify.go            (printRangeProvenance ~line 1517)
                 internal/cmd/verifyscope.go       (~line 282)
                 internal/cmd/verifyscope_test.go
                 internal/cmd/verify_scoping_test.go   (stubRangeAdapter gains Constructs)
                 internal/cmd/verify_range_e2e_test.go
       covers:   c-3 (readout side), c-4 (printed warning), c-1 (e2e)
       description:
         - printRangeProvenance: drop the pad hunt; print `ranged stryker N
           file(s)` then one line per construct `  src/a.ts:1-80  FunctionDeclaration f`.
           Degraded lines already print via the existing `scope degraded:` loop —
           no new code, but the test below pins the ast-unavailable line through it.
         - verifyscope.go: `1-80 (FunctionDeclaration f)` in place of `(pad 25)`.
         - e2e: stubRangeAdapter.Constructs returns {1,60,"FunctionDeclaration main"}
           for the two-line hunk at 40-41; assertion becomes RunRanges received
           {internal/pkg/c.go: [{1,60}]}.
       test_contract:
         - If the readout re-renders or drops the construct,
           `TestVerifyScopePrintsRawAndEffective` (rewritten) fails: stdout has
           the raw hunk `40-41` beside `1-80 (FunctionDeclaration f)` and never
           the string "pad"; `TestVerifyScopeJSONIsTheRecordVerbatim` (kept)
           fails if --json's ranges lack the `construct` field or carry `pad`.
         - If the verify summary stops naming the construct,
           `TestVerifyOutputNamesTheConstructPerRange` fails: a run through
           Verify() with the stub prints `ranged stryker 1 file(s)` and
           `internal/pkg/c.go:1-60  FunctionDeclaration main`, and prints no "pad".
         - If a lost AST reads as a ranged run, `TestASTUnavailablePrintsTheDegradedWarning`
           fails: stub Constructs returning ErrASTUnavailable("node not found on
           PATH") → stdout carries `scope degraded: stryker: AST unavailable for
           internal/pkg/c.go (node not found on PATH)` and `whole-file stryker`
           with reason ast-unavailable, and the word "ranged" never appears.
         - If the hunk-key vocabulary between scope and adapter drifts,
           `TestRangedDispatchReachesTheAdapterEndToEnd` (re-pointed) fails: the
           map RunRanges receives is keyed by the repo-relative slash path and
           holds {1,60}, not the bare hunk {40,41}.
       depends_on: [t-2]
       status: pending

  t-5  Retire the pad from ARCHITECTURE.md and the verify prompt
       files:    ARCHITECTURE.md                   (lines ~567–586)
                 assets/prompts/verify.md          (line ~74)
                 internal/cmd/verify_prompt_test.go
       covers:   c-2 (docs side)
       description:
         - ARCHITECTURE.md §scoping: replace the padded-hunks sentence with the
           construct design (Babel from Stryker's tree, outermost top-level
           statement, `construct` on every range, `ast-unavailable` as the fifth
           reason, degrades like malformed-range); re-point the `RunScoped` /
           `PlanRanges` / e2e symbol bullets (run `dross architecture check`).
         - verify.md: `file → [{start, end, construct}]`, five reasons listed,
           "no `ranges` = whole-file leg" guidance kept; no mention of pad.
       test_contract:
         - If either document still describes a line-pad heuristic,
           `TestVerifyPromptAndArchitectureCarryNoPadResidue` fails: neither
           file contains `pad`, `hunkContextLines` or `padAndMerge` (word-bounded),
           and both contain `ast-unavailable` and `construct`.
         - If a symbol bullet went stale, `dross architecture check` (run at
           /dross-verify) reports it — the executor runs it and commits the fix.
       depends_on: [t-2]
       status: pending

## Coverage

| criterion | tasks |
|---|---|
| c-1 (widen to enclosing top-level construct, >25-line fixture) | t-1 (script + resolver), t-2 (planner dispatches construct spans), t-3 (e2e) |
| c-2 (pad deleted everywhere) | t-2 (code + TestNoPadResidueInCode), t-5 (docs) |
| c-3 (`construct` in tests.json, verify.toml, `verify scope`) | t-2 (record + leg summary), t-3 (readouts) |
| c-4 (`ast-unavailable` degrades, detail on Degraded, warning printed) | t-1 (detail classification), t-2 (reason + Degraded), t-3 (printed warning) |
| c-5 (leg files filtered to Supports) | t-4 |
| c-6 (verify.go:115 survivor killed by command-level test) | t-4 |

All 6 criteria covered; no task lacks a criterion.

## Judgment calls

- Widened `RangeRunner` with `Constructs` instead of adding a second optional
  `ConstructResolver` interface. Rejected the second interface: it would create
  a "range-capable but construct-blind" state that c-1 says must not exist, and
  a second type assertion in the planner for the exec-consent walker to resolve.
- Request travels as a JSON prelude on stdin, argv = `node -` only. Rejected env
  var (a second channel to document) and positional args (would need argv
  fencing of a derived path). A `node` argfence entry is still required because
  the subprocargs audit fails closed on any unlisted literal binary.
- Kept the resolver LOCAL and outside newLauncher even when Stryker.Remote is
  set — that is the planning_locus lock; a remote-only setup with no local
  node_modules is simply ast-unavailable, which is the honest reading.
- .svelte files get no special handling: Babel cannot parse them, so they land
  as ast-unavailable (parse error, position on the Degraded line). Rejected
  script-block extraction as speculative structure — no criterion asks for it,
  and the fallback is exactly what c-4 specifies.
- Real-Babel proof lives in ONE conditional test (`$DROSS_BABEL_WORKDIR`, else
  skip with the reason) plus the phase's own verify against a Stryker project;
  every Go-side contract runs with a swapped constructBuildCmd. Rejected
  vendoring @babel/parser into testdata (network/licensing churn for a Go repo)
  and rejected a node-only script unit test harness (a second test runner).
- Merged c-5 and c-6 into t-4: each alone is one small edit in
  internal/cmd/verify.go's neighbourhood; both are routed debts from the prior
  phase, independent of the AST work, so they run in wave 1 while t-1 is built.
- PlanRanges takes a pre-resolved map rather than a resolver callback. A
  callback would keep the signature pure-looking while spawning from inside the
  planner, which is the exact thing the planning_locus lock forbids; the map
  makes the detached-collect call site (`nil`) obviously spawn-free.
- A RangeRunner file with hunks but no entry in the resolved map is
  `ast-unavailable` ("constructs not resolved"), not a bare-hunk range. Rejected
  falling back to the raw hunk: that would resurrect the exact under-generation
  c-1 retires, silently.
- t-5 (docs) is a separate wave-3 task rather than folded into t-3: t-3 already
  spans five files, and the docs need t-2's final names and line anchors.
