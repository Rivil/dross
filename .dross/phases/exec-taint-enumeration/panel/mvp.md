# exec-taint-enumeration — mvp draft

```
Phase exec-taint-enumeration — 7 tasks across 3 waves

Wave 1
  t-1  Shared SSA load and derived package set
       files:    go.mod, go.sum,
                 internal/cmd/ssaload_test.go (new),
                 internal/cmd/testdata/typed/loadsmoke/smoke.go.txt (new)
       covers:   c-5, c-8
       desc:     Add golang.org/x/tools as a test-only dependency. One sync.Once loader,
                 sharedSSA(t), runs a single packages.Load over "./..." plus every fixture dir
                 under internal/cmd/testdata/typed/*/ (found with os.ReadDir, not listed; its
                 *.go.txt overlaid as *.go through Config.Overlay). It builds the SSA program and
                 one VTA call graph (CHA as the initial graph), and exposes the derived live
                 package set: module packages with no /testdata/ element. Measure the load time
                 under -race on the first run and choose the load mode (deps from export data,
                 syntax for module packages only) before anything else is built on it.
       contract: - TestSharedLoadIsOncePerBinary: calling sharedSSA(t) twice returns the identical
                   *ssa.Program and *callgraph.Graph, and the package-level load counter reads 1.
                   A scan that calls packages.Load itself raises the counter and fails this test.
                 - TestDerivedSetCoversEveryPackage: a filesystem walk of internal/ and cmd/ (with
                   testdata skipped) lists every dir that holds a non-test .go file. Each one must
                   be in the loaded live set. derivedSetGaps(fsDirs, loaded) with internal/mutation
                   removed from `loaded` returns an error naming internal/mutation, and the live
                   comparison returns nothing.
                 - The live set holds at least N packages (the floor sits roughly a quarter under
                   today's count), and no path in it contains /testdata/. A fixture that leaked
                   into the live set fails this.
                 - The loadsmoke fixture dir holds only smoke.go.txt. It still loads as a
                   type-checked package: its func is present in the SSA program with
                   non-nil TypesInfo. If the overlay wiring breaks, this fails before any scan
                   depends on it.
                 - No non-test package in the load imports golang.org/x/tools, directly or
                   transitively. This fails if the loader ever moves out of a _test.go file.

Wave 2 (each depends t-1)
  t-2  Spawn-output taint engine and fixture corpus
       files:    internal/cmd/taint_test.go (new),
                 internal/cmd/testdata/typed/taint/{return,field,writer,stryker_prephase,clean}.go.txt (new),
                 internal/cmd/execconsent_audit_test.go (execExemptMarkers delegates to a shared
                 drossDirectives(fset, f, name) parser, with no change in behaviour)
       covers:   c-1, c-2, c-3, c-6, c-7
       desc:     Sources are identified by *types.Func / *types.Var identity: the []byte results
                 of (*exec.Cmd).Output and CombinedOutput, values stored into Cmd.Stdout/Stderr
                 (the stored buffer's Alloc is tainted), StdoutPipe/StderrPipe readers, and
                 ExitError.Stderr. Taint moves forward over SSA def-use, across calls through the
                 shared VTA graph (params, returns, struct fields, io.Writer args). It drops when a
                 value's type stops being text (bool, int, float). The values that pass are:
                 terminal writes (concrete os.Stdout/os.Stderr and cobra
                 OutOrStdout/ErrOrStderr), mutation.RecordToolFailure, and a
                 `//dross:taint-exempt <prose>` directive on the line above the conversion
                 (same grammar as exec-exempt, and the same 20-char floor). Everything else is
                 a finding, and that includes any error value and any unmodelled external call
                 that receives text. Scan scope is the derived set only. The live zero-findings
                 gate is NOT in this task; it is added in t-7.
       contract: - TestTaintFixtureCorpus: the set of flagged escape lines in testdata/typed/taint
                   must equal the set of lines annotated `// want:taint`. Any unannotated flag is
                   a false positive, and any unflagged annotation is a miss.
                 - c-2, one per channel. return.go: a spawn wrapped in a helper that returns
                   out, then a conversion helper strings.TrimSpace(string(b)), then fmt.Errorf at
                   the caller, is flagged at the caller's Errorf. field.go: out is stored in
                   r.out, then errors.New(string(r.out)) runs in another method, and that is
                   flagged. writer.go: cmd.Stdout = w, where the caller passes &sb, then
                   fmt.Errorf(sb.String()), is flagged, while the same helper called with
                   os.Stderr is not. Stub out interprocedural propagation and all three
                   annotations go unflagged.
                 - c-3: stryker_prephase.go.txt carries the headBuffer (Write, h.buf field,
                   printHead(w)) teed through io.MultiWriter(os.Stderr, head), then
                   `var quoted strings.Builder; head.printHead(&quoted, …)` and on the next line
                   fmt.Errorf("%s\n%s", msg, quoted.String()). It is flagged at the Errorf line.
                 - clean.go (must-not-trip, unannotated). None of these may be flagged: writes to
                   os.Stderr and cmd.ErrOrStderr(); RecordToolFailure(tool, exit, Observed(len(out)));
                   strings.Contains(string(out), x); strconv.Atoi; len(out) == 0;
                   string(out) == "y"; and a conversion under a marker with ≥20 chars of prose.
                 - Only identity counts. A local type with its own Output() method is not a
                   source, and exec.Command(...).Output() reached through a method value is.
                 - An error value is never terminal: `return errors.New(string(out))` is
                   flagged even though cobra would print it.
                 - Marker grammar. A bare `//dross:taint-exempt` and a reason under 20 chars
                   are both findings, and `//dross:taint-exemptfoo` is not a marker.
                   TestExecConsentMarkerGrammar still passes unchanged.
                 - c-6: the single-violation fixture's message contains the escape
                   `<file>:<line>`, the spawn site `<file>:<line>` it traces to, and the remedy
                   (terminal / RecordToolFailure / //dross:taint-exempt).
                 - c-7: the live walk enumerates at least N spawn-output sources (about 50 today,
                   with the floor a quarter under). taintSourceFloor(N-1) returns an error.

  t-3  Type-key the unwrap bans over derived set
       files:    internal/cmd/pathfence_enum_test.go, internal/cmd/security_test.go,
                 internal/cmd/testdata/typed/unwrap/{unwrap_os,rundir_revert,seam_ok,renamed_local}.go.txt
                 (moved from testdata/pathfence_scan/ + one new)
       covers:   c-9
       desc:     Delete scannedPackages. The unwrap ban walks the derived set and flags an os.*
                 argument containing a call whose method object is (pathfence.Contained).String
                 or .Rel, resolved through TypesInfo. containedVar is deleted from
                 TestRunDirSitesPassContainedThrough in favour of the same type check. The ".."
                 literal ban stays AST, walks the derived set, and excludes internal/pathfence by
                 package identity. Any live hits that the widened scope surfaces are fixed in
                 this task.
       contract: - unwrap_os keeps its written answers for c.String() and c.Rel() inside
                   os.ReadFile/Stat/Remove/WriteFile. The type-blind builderWrite row
                   (strings.Builder.String() inside os.WriteFile) flips from must-trip to
                   must-not-trip in the table. Revert to name matching and that row fails.
                 - renamed_local.go.txt holds a Contained in a local named `x`, and
                   os.ReadFile(x.String()) is flagged. Under the old containedVar list it was
                   not, so a rename can no longer silence the arm.
                 - rundir_revert keeps 3 findings and seam_ok keeps 0.
                 - Vacuity: the ban examines at least N os.* call sites across the derived set.
                   A walk that drops internal/security (where the run-dir sites live) falls
                   under the floor and fails.
                 - TestDotDotScanSeesTheRealLiterals still finds literals in internal/pathfence,
                   and the ban over every other derived package reports zero.

  t-4  Field walker classifies every tagged field
       files:    internal/cmd/pathfence_fields_test.go, internal/pathfence/fields.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt (new),
                 internal/cmd/testdata/pathfence_scan/task_20260907.go.txt (new),
                 internal/cmd/pathfence_carrier_test.go (only if a newly found field is Consumed)
       covers:   c-9
       desc:     Delete schemaDirs and pathShapedTags. Walk every toml/json-tagged string or
                 []string field in the derived set. Each one must appear in pathfence.Fields()
                 (as a path) or in not_paths.txt (as not a path), and the two sets are checked
                 in both directions. Generate not_paths.txt once. Newly surfaced path fields,
                 such as cmd.detachedRun.RunDir, get real registry entries with Why and Readers.
       contract: - task_20260907.go.txt (package phase; Task plus Workdir `toml:"workdir"`,
                   OutputDir `toml:"output_dir"`, Spec `toml:"spec"`) is merged into the live walk.
                   It yields exactly three "unclassified field phase.Task.{Workdir,OutputDir,Spec}"
                   findings. Restore a tag vocabulary and this falls to zero, which fails the test.
                 - A tagged string field declared in a derived package outside the old four dirs
                   is found. The live detachedRun.RunDir (internal/cmd/local.go, `toml:"run_dir"`)
                   is now walked and declared.
                 - A not_paths.txt line naming a field that no longer exists fails as stale. A
                   field listed both in pathfence.Fields() and in not_paths.txt fails as a
                   contradiction.
                 - The walker finds at least N tagged string fields (about 450 today).
                   TestWalkerSelfTest keeps its tag-option-stripping and bool-drops rows.

  t-5  Exec-consent reach through the SSA call graph
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/typed/reach_iface/{cmd,impls}.go.txt (new)
       covers:   c-10
       desc:     Keep the AST graph for sites, commands, gating and rewrites. Replace only the
                 interface-dispatch branch of methodsOn/implementers with a resolver keyed by
                 (caller node key, method), which reads the invoke-mode edges of the shared VTA
                 graph and maps each ssa.Function to the AST key. A dispatch that has no
                 resolver, or that VTA leaves empty, becomes a reported "cannot resolve"
                 finding when any candidate reaches a spawn. The union is gone.
       contract: - In reach_iface, the Runner interface has Quiet and Loud implementers (Loud
                   spawns), and the command passes only Quiet. Loud's site is NOT attributed to
                   the command. Restore the union and it is, which fails the test.
                 - A dispatch on an interface param that no concrete type ever reaches, with a
                   spawning implementer in the program, gives a finding containing
                   "cannot resolve". It is never attributed silently.
                 - The calibration verdicts do not move: TestExecConsentFlagsItsOwnSnippets, the
                   reach-fixture tests (CrossesPackages, VarSeams, SeveringTheGatedEdge,
                   ExtraHop), TestExecReachReportsAmbiguity (still reported, still a finding),
                   and the repoExecGraph rewrite tests (DeletingVerifysGate,
                   MarkerOnAMutationSpawn) all keep their asserted classes.
                 - TestEverySpawnSiteGatedOrExempt and TestMutationSpawnsAreGatedViaVerify stay
                   green with the resolver wired in: verify's adapter.Run() resolves through
                   VTA to every constructed adapter.

Wave 3
  t-6  Declared-path-field taint scan (depends t-2, t-4)
       files:    internal/cmd/pathfield_taint_test.go (new),
                 internal/cmd/testdata/typed/pathfield/{raw,notconsumed,contained}.go.txt (new)
       covers:   c-4, c-6, c-7
       desc:     This reuses t-2's engine. The sources are loads (FieldAddr/Field) of every
                 struct field declared in pathfence.Fields(), resolved to a *types.Var. The sink
                 is the path argument of an os.* call. A value clears only by passing through a
                 pathfence constructor that yields a Contained. A Consumed field that reaches a
                 sink raw is a finding, and so is any NotConsumed field that reaches one.
                 Scope is the derived set.
       contract: - raw.go: a changes.TaskRecord.Files element passed to os.ReadFile, including
                   via a helper return, is flagged. contained.go: the same value run through
                   pathfence.Contain and read via the seam is not flagged. notconsumed.go:
                   phase.Task.Files reaching os.Stat is flagged. The flagged lines must equal
                   the lines annotated `// want:pathfield`.
                 - c-6: the raw.go finding names the os.ReadFile file:line, the field
                   "changes.TaskRecord.Files", and the remedy (construct a pathfence.Contained
                   via pathfence.Contain).
                 - c-7: the live walk enumerates at least N declared-field reads. A registry
                   entry whose Struct.Field does not resolve to a *types.Var in the load fails as
                   stale rather than contributing zero reads.
                 - The live derived set reports zero findings.

  t-7  Drain live spawn-output findings; enable gate (depends t-2)
       files:    internal/cmd/{ship_recover,statusline,survivor_drain,phase,cleantree,update,test,
                 milestone_stale,pause,lane_install,worktree_files,init,techdebt}.go,
                 internal/{techdebt,quality,security}/run.go,
                 internal/mutation/{construct,gremlins,stryker,stryker_net}.go,
                 internal/codex/{git,ast_grep}.go, internal/compilefence/compilefence.go,
                 internal/ship/{headpr,comment,open,merged,basepr}.go,
                 internal/remote/{hold,remote}.go, internal/cmd/taint_test.go
       covers:   c-1
       desc:     Add TestSpawnOutputEndsSafe, the zero-findings gate over the derived set.
                 Clear every live finding by re-routing (to stderr or the recorder) or by adding
                 a //dross:taint-exempt marker whose prose names where the text goes. Check each
                 error escape against telemetry err_detail before marking it. If the engine
                 proves imprecise, fix it in taint_test.go and add a must-not-trip row to the
                 corpus first.
       contract: - TestSpawnOutputEndsSafe reports zero findings on the live tree, with the t-2
                   source floor met.
                 - Removing any one added marker (for example, the one on ship/open.go's
                   CombinedOutput error path) makes the gate report that escape by file:line
                   and name its spawn site.
                 - Adding `out, _ := exec.Command("git","log").Output(); return
                   fmt.Errorf("%s", out)` to any derived package fails the gate. That is c-1's
                   live half.
                 - Every added marker passes the grammar check: prose of at least 20 chars, on
                   the line directly above the conversion.
```

## Coverage

| Criterion | Tasks | Note |
|---|---|---|
| c-1 | t-2, t-7 | t-2 builds the engine and pins it with fixtures. t-7 drains the live tree and switches on the zero-findings gate. |
| c-2 | t-2 | One fixture per channel: return, field, io.Writer. Helper-wrapped spawn and helper-wrapped conversion are in return.go. |
| c-3 | t-2 | stryker_prephase.go.txt carries the exact builder, then quoted.String(), then fmt.Errorf shape. |
| c-4 | t-6 | Raw Consumed field reaching os.* fails, and so does a NotConsumed field reaching os.*. |
| c-5 | t-1 | Derived set, plus a filesystem-walk completeness check that covers every package, a superset of "packages with a spawn site or field read". t-3, t-4 and t-6 walk it. |
| c-6 | t-2, t-6 | One single-violation message fixture per new scan, asserting escape file:line, source, and remedy. |
| c-7 | t-2, t-6 (t-3, t-4 keep floors) | A source floor plus a written-down `// want:` corpus for each new scan. The migrated guards carry their own floors. |
| c-8 | t-1 | t-1 covers the once-per-binary half (a load counter of 1). The ≤15s half is read at verify from the CI timing table (internal/cmd package total, branch run against base run). It cannot be a unit test without flaking. |
| c-9 | t-3, t-4 | t-3 covers the derived set, the Contained-typed unwrap ban and deleting containedVar. t-4 covers the unlisted tag word, the unlisted dir, and the 2026-09-07 fixture. |
| c-10 | t-5 | Reach resolves through VTA. An unresolvable dispatch is reported. Calibration verdicts stay fixed. |

Criteria covered: 10/10.

## Judgment calls

- **All scans live in internal/cmd _test.go files, not a new test-only package.** The loader must stay in a _test.go file (loader_mechanism), and the exec-consent audit and pathfence guards that c-9 and c-10 migrate already live in internal/cmd. A separate package would need a second load in the cmd binary, or a move of every audit into the new package.
- **Typed fixtures are `.go.txt` files overlaid into the single Load (Config.Overlay), not real `.go` packages under testdata.** Real `.go` files would be ingested as live source by at least four existing WalkDir audits that do not skip testdata (the execconsent sweep, hermetic_dross_read, project_writer_pin, execconsent_docs). A second load per fixture would break c-8's once-per-binary rule.
- **For c-9, every toml/json-tagged string field is classified, either in the registry or in not_paths.txt.** I rejected a path-morpheme heuristic because it is still a word list, and "spec" carries no morpheme. I rejected dataflow-only detection because the 2026-09-07 fields have no consumer and would stay green. The cost is about 450 generated lines, plus one line of friction for each new tagged field. It fails loudly in both directions, which is the property the tracked-path-containment FLAG asked for.
- **For c-10, the change is a hybrid: the AST graph stays and only interface dispatch is swapped for a VTA resolver keyed by (caller, method).** A full SSA port would need a fresh Load for each repoExecGraph rewrite (the verify-gate deletion and the gremlins marker), which breaks c-8. Keying by function rather than position also survives the line shifts that rewrites cause.
- **The shared call graph is VTA (with a CHA seed), not CHA or RTA.** VTA is the algorithm whose output is "the concrete types that actually flow". CHA is the union again. RTA would need main-rooted reachability and would drop functions that only a command constructor reaches through cobra.
- **The marker is `//dross:taint-exempt`, not `//dross:taint-cleared` or anything else.** It sits in the same family as exec-exempt, uses one generalized parser (drossDirectives), and uses one floor (execExemptMinReason = 20), which is what marker_grammar asks for.
- **An unmodelled external call that receives tainted text is an escape (fail-closed).** The pure-propagation models are stdlib-semantics only (strings, bytes, strconv, fmt.Sprint*, path/filepath, encoding/json.Unmarshal into a pointer). The alternative, treating unknown externals as pure, silently misses os.WriteFile(p, out).
- **The live drain is one task across about 30 files, not split by package.** It is a single layer (one-line markers or local re-routes), and the gate cannot land until every package is clean. Splitting either adds a fourth wave or commits a red gate. This knowingly breaks the 5-file granularity guideline.
- **The live zero-findings gate lands in t-7, not t-2.** In t-2 it would be red until the drain, so t-2 ships the engine, the corpus and the source floor only.
- **The c-8 budget is measured at verify, not in a test.** Laptop timings are unreliable. The risk is flagged instead: CI runs `-race`, and race-instrumented x/tools type-checking and VTA could exceed 15s. t-1 has to measure under -race first and pick the load mode (export data for dependencies, syntax only for module packages) before t-2 to t-5 build on it.
- **The x/tools-not-in-shipped-binary guard is folded into t-1 as one assertion, not given its own task.** It protects a locked decision and costs nothing inside t-1.
- **internal/mutation/toolfence_residual_test.go is left as is,** including its "does not catch the laundered shape" comment. t-2 closes that gap, but touching the file traces to no criterion. The judge may fold a comment update into t-2.
- **The c-5 completeness check requires every package dir with non-test Go code to be in the load**, not only the ones the scans themselves find sources in. It is simpler, and it does not use the scans to vouch for their own coverage.
