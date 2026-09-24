# Panel draft — RISK LENS

Phase exec-taint-enumeration — 18 tasks across 5 waves

This draft starts from what can break. Each failure mode below is owned by exactly
one task, and that task's contract is the test that fails when it happens.

| # | Failure mode | Owner |
|---|---|---|
| R1 | The load fails partway or comes back partial (type errors, `go list` failure), and every scan passes over a smaller program | t-1 |
| R2 | TestMain's throwaway HOME points `go list` at an empty GOMODCACHE/GOCACHE: a network fetch plus a cold compile inside the test | t-1 |
| R3 | The load runs more than once per binary (once per caller, or once per rewrite test), breaking c-8's "once" | t-1 (counter), t-9 (surgery, not reload) |
| R4 | SSA bodies get built for stdlib and deps, which under `-race` blows the 15s budget | t-1 (structural), t-18 (CI read) |
| R5 | golang.org/x/tools links into the shipped binary through a non-test import | t-1 |
| R6 | Fixture types leak into the live program, so CHA/VTA resolve live interfaces to fixture implementers | t-1 |
| R7 | A spawn or field read sits in a package or file the load never type-checks (a new dir, a build-tagged file) | t-2 |
| R8 | A source kind is missed (pipes, ExitError.Stderr, a buffer on Cmd.Stdout), or sources are matched by name rather than type | t-3 |
| R9 | Clearance is too eager: `strconv.Atoi`'s error carries the input text, and a whole-call clearance drops it | t-3 |
| R10 | A file write or other effectful external call has no result to carry taint, so a results-only rule misses the persistence | t-3 |
| R11 | The recorder, an io.Writer or an error is treated as terminal. This is the laundering hole | t-3 |
| R12 | Taint is lost at a function boundary: a return, a field, a writer argument, a closure, a generic, a channel. Recursion never converges | t-5 |
| R13 | Context-insensitive params taint every caller of a shared helper. The false positives turn into marker spam | t-5 |
| R14 | A marker clears too much: it sits at a distance, covers no conversion, or a directive is confused with another | t-6 |
| R15 | The switch to SSA changes an existing exec-consent calibration verdict | t-4 |
| R16 | Interface dispatch silently unions all implementers or silently drops them | t-4 |
| R17 | The live-tree load-bearing proofs either force reloads or mutate the shared graph, and the result depends on test order | t-9 |
| R18 | The unwrap ban stays keyed on names, and the derived set misses argfence/hostallow | t-7 |
| R19 | Path-shape detection is still a word list, so the 2026-09-07 experiment still passes | t-8 |
| R20 | A registry entry that fails to resolve to a types.Var is silently dropped as a source | t-10 |
| R21 | The pre-phase stryker fixture drifts from verbatim into a shape the engine happens to catch | t-11 |
| R22 | The burn-down launders instead of fixing: markers go on the mutation tool stream, and error-text pins are deleted rather than moved | t-12..t-16 |
| R23 | The burn-down edits source text that t-9's surgery anchors depend on | ordering: t-15, t-16 after t-9 |
| R24 | A finding is unactionable because origin is lost across packages | t-17 |
| R25 | The live walk narrows and the gate passes vacuously | t-18 (taint), t-10 (path), t-9 (exec) |

---

```
Wave 1

  t-1  Load the module once for source scans
       files:    go.mod, go.sum,
                 internal/cmd/srcprog_test.go (NEW),
                 internal/cmd/srccorpus_test.go (NEW),
                 internal/cmd/hermetic_env_test.go
       covers:   c-8, c-7
       desc:     Wraps the one packages.Load of ./... in a sync.Once. The load uses Tests:false,
                 syntax and types for module packages, and export data for deps. Dir is the
                 absolute repo root. Env is snapshotted at init with HOME=ambientHome, so the go
                 caches resolve. Any package error is fatal. SSA bodies are built for module
                 packages only, and one VTA call graph is built over them. Adds a fixture-program
                 builder: .go.txt files are type-checked through an importer backed by the load's
                 own type graph, into a SEPARATE ssa.Program. Adds a corpus harness driven by
                 inline `// WANT <scan>` annotations. TestMain fails the binary when the load
                 counter is >1 after m.Run.
                 Gate before t-3 starts: measure load+SSA+VTA once under `go test -race` on the
                 laptop and record it in the commit body. Over 10s, narrow the SSA/VTA scope first.
       contract: - calling the loader from a second Once, or from a helper that bypasses it, makes
                   TestMain exit non-zero with "source program loaded 2 times". A per-caller load
                   cannot pass
                 - forcing GOPROXY=off in the loader env, the load still succeeds under TestMain's
                   empty HOME. Reverting cfg.Env to the live os.Environ() points GOMODCACHE at the
                   temp HOME, and this test fails with a module-download error
                 - loadErrors() fed a synthetic *packages.Package carrying one TypeError returns an
                   error naming that import path. A loader that continued past IllTyped packages
                   fails this
                 - no *ssa.Function with non-nil Blocks belongs to a package outside
                   github.com/Rivil/dross. Building dependency bodies, the dominant -race cost,
                   fails this
                 - golang.org/x/tools/... is reachable from no import of
                   github.com/Rivil/dross/cmd/dross in the loaded graph. A non-test file importing
                   go/ssa fails this
                 - no loaded CompiledGoFiles entry ends in _test.go (the spawn_surface boundary)
                 - a two-package fixture, rootpkg importing example.com/fixture/helperpkg,
                   type-checks whichever order its files are given. A fixture importing a path
                   absent from the load graph fails and names the path rather than skipping the file
                 - harness: a finding on an unannotated line fails as UNEXPECTED; a WANT line with no
                   finding fails as MISSED; the count of parsed WANT annotations must equal the count
                   asserted, so a parser that drops the last one fails; an empty corpus dir fails

Wave 2 (depends t-1)

  t-2  Derive the scanned set; cross-check every file
       files:    internal/cmd/srcscope_test.go (NEW)
       covers:   c-5
       desc:     The scanned set is every package the load returns under the module path. No
                 roots list is kept. An independent filesystem pass (go/parser, no types) finds
                 every non-test .go file outside a testdata segment that holds an
                 exec.Command/CommandContext call or a selector naming a registry field. Each
                 such FILE must be in some loaded package's CompiledGoFiles.
       contract: - the scanned set equals the set of dirs holding non-test .go files outside
                   testdata. A package present on disk but absent from the load, or the reverse,
                   fails and names the dir. A new internal/ or cmd/ package is therefore in both
                   sets with no edit
                 - crossCheck over a temp tree whose extra/ dir holds an exec.Command, given a
                   walked set lacking extra, fails and names extra/…go
                 - a spawn in a file carrying `//go:build windows` fails the cross-check and names
                   the file. The darwin/linux load never type-checks it, so the SSA scans are blind
                   to it
                 - internal/mutation/testdata/ceiling is outside the set (excluded because a path
                   segment is literally `testdata`), while a dir like internal/testdatabase would
                   stay in

  t-3  Taint sources, in-function flow, sink classes
       files:    internal/cmd/taint_engine_test.go (NEW),
                 internal/cmd/testdata/taint/sources.go.txt (NEW),
                 internal/cmd/testdata/taint/sinks.go.txt (NEW)
       covers:   c-1, c-7
       desc:     The engine is parameterized by a policy of source, sink and clear predicates, so
                 t-10 reuses it. The exec policy's sources are resolved by types.Func/Var identity:
                 (*exec.Cmd).Output and CombinedOutput results, reads from StdoutPipe and
                 StderrPipe, reads of the ExitError.Stderr field, and a local referent stored into
                 Cmd.Stdout or Cmd.Stderr.
                 External calls: a numeric or bool result clears. Any other result, error
                 included, stays tainted. Reference args (pointer, slice, map, interface) receive
                 reverse taint, but only for callees in a fixed stdlib transform-package set.
                 Every other external callee that receives taint is an escape.
                 Terminal means os.Stdout, os.Stderr, and cobra's OutOrStdout and ErrOrStderr.
                 A tainted error is followed, never terminal. A tainted return is an escape here,
                 fail-closed until t-5. The engine exposes a clearAt(pos) hook for t-6.
                 Findings carry the escape position plus the set of origin positions.
       contract: - sources.go.txt: Output, CombinedOutput, a StdoutPipe read, a StderrPipe read, an
                   ExitError.Stderr read, and a &bytes.Buffer on Cmd.Stdout each reach errors.New
                   and yield exactly their one WANT finding. Importing os/exec as `x` trips
                   identically
                 - a local type with its own Output() method reaching errors.New does NOT trip.
                   Sources are matched by type identity, not by name
                 - strconv.Atoi(out): the int result into fmt.Errorf is clean, and the error only
                   compared to nil is clean. The same error wrapped with %w trips at the wrap line
                 - strings.Contains / HasPrefix, == and len over output yield no finding
                 - fmt.Fprintln(os.Stderr, out) and fmt.Fprint(cmd.ErrOrStderr(), out) are clean.
                   fmt.Fprint(&b, out) into a strings.Builder followed by errors.New(b.String())
                   trips, because a writer is not terminal
                 - os.WriteFile(p, out, 0o600) trips. The write has no result to carry the taint,
                   so a results-only rule would miss it
                 - mutation.RecordToolFailure with a tainted `tool` argument trips, while
                   Observed(len(out)) is clean. The recorder is not a blanket sink

  t-4  Route exec-consent reach through the VTA graph
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/directive_test.go (NEW),
                 internal/cmd/testdata/exec_consent/dispatch.go.txt (NEW)
       covers:   c-10
       desc:     AST facts stay as they are: sites, markers, cobra commands, gating and the
                 AddCommand tree. They are read from the load's own pkg.Syntax. Reach edges now
                 come from the shared VTA graph, joined by token.Pos within the same FileSet. The
                 `implementers` union is deleted.
                 A dynamic call is reported, never unioned, when VTA yields no callee and CHA has
                 a candidate that reaches a spawn.
                 Calibration fixtures go through t-1's fixture builder. They may gain stub
                 declarations only. execExemptMarkers moves unchanged into a directive-generic
                 parser in directive_test.go.
       contract: - every snippets.txt row keeps its FLAG/PASS verdict with headers == rows ==
                   exercised. The FLAG/PASS header lines stay byte-identical. Bodies may gain
                   declarations, but no answer changes
                 - reach fixture verdicts are unchanged:
                   git log is ungated via two hops;
                   go test is gated through the runFn var seam (a dynamic call via a global);
                   rsync is mixed; markdownlint is ungated; cargo is "no command at all"
                 - dispatch.go.txt: a command passing only Quiet{} to Drive(r Runner) does not
                   reach Loud's spawn, so Loud's site reads "no command at all". The variant
                   passing Loud{} attributes it. A CHA or union resolution fails the first case
                 - TestExecReachReportsAmbiguity's fixture, where nothing calls Drive, is still a
                   reported finding saying "across 2 packages". A VTA-empty/CHA-spawning dispatch
                   is reported with its call-site file:line. Deleting the report branch fails both
                 - TestExecConsentMarkerGrammar's three verdicts and TestExecConsentSkipsTestFiles's
                   1-site/1-finding are unchanged after the parser extraction and builder switch

Wave 3

  t-5  Carry taint across returns, fields, writers  (depends t-3)
       files:    internal/cmd/taint_flow_test.go (NEW),
                 internal/cmd/taint_engine_test.go,
                 internal/cmd/testdata/taint/channels_return.go.txt (NEW),
                 internal/cmd/testdata/taint/channels_field.go.txt (NEW),
                 internal/cmd/testdata/taint/channels_writer.go.txt (NEW)
       covers:   c-2
       desc:     Per-function summaries over module functions (param→result, param→field,
                 param→writer-param) are applied at each call site with that site's argument
                 taint. This replaces t-3's fail-closed return escape.
                 Field taint is field-based: any tainted store to T.f taints every read of T.f.
                 A reverse referent closure runs through MakeInterface, ChangeType, Phi, field
                 loads (to every store to that field), transform-set constructors (io.MultiWriter,
                 bufio.NewWriter) and parameters (to callers' arguments).
                 When taint flows into an object whose type has a Write method, that method's
                 []byte parameter becomes a source.
                 Each channel's fixture keeps the helper and the consumer in separate packages.
       contract: - return: a.Rev() returns TrimSpace(string(out)), and b's errors.New(a.Rev()) trips
                   at b's line. A caller that only prints Rev() to os.Stderr is clean, which proves
                   the return is followed rather than flagged
                 - context: a shared helper norm(s) is called with output at site A and with a
                   literal at site B. Only A's consumer trips. A context-insensitive param rule
                   fails this
                 - field: a helper stores output into Result.Log, and a different function reads
                   r.Log into fmt.Errorf. That trips
                 - io.Writer arg: given Run(w io.Writer){ c.Stdout = w; c.Run() }, a caller
                   passing &strings.Builder and then errors.New(b.String()) trips. A caller passing
                   os.Stderr is clean. A writer that reaches Cmd.Stdout through an options-struct
                   field set from cmd.OutOrStdout() is clean
                 - conversion wrapped: toStr(b []byte) string applied to the output and then passed
                   into Errorf trips
                 - tee: io.MultiWriter(os.Stderr, head), where head.Write copies into head.buf, so
                   head.buf.String() into Errorf trips. If head only leaves through
                   printHead(os.Stderr, …), it is clean
                 - a closure capture, a generic id[T], and a chan string send/recv each carry the
                   taint through to a tripping Errorf
                 - a two-function mutual recursion around a spawn reaches its fixpoint and reports
                   its one finding

  t-6  Add the //dross:taint-cleared conversion marker  (depends t-3, t-4)
       files:    internal/cmd/taint_marker_test.go (NEW),
                 internal/cmd/directive_test.go,
                 internal/cmd/testdata/taint/markers.go.txt (NEW)
       covers:   c-1
       desc:     The directive name is fixed here as //dross:taint-cleared <reason>, parsed by
                 t-4's shared parser with the reason floor at execExemptMinReason (20). It binds to
                 the line immediately below and clears only tainted values DEFINED on that line.
                 A marker binding no tainted definition is an orphan finding.
       contract: - a valid marker directly above `sha := strings.TrimSpace(string(out))` clears it,
                   and the same marker two lines up does not. Position counts, not proximity
                 - bare, glued (`//dross:taint-clearedx`), spaced (`// dross:taint-cleared`) and
                   19-char markers each give the verdict class that exec-exempt's grammar table
                   gives, replayed through the one parser
                 - //dross:exec-exempt above a conversion leaves the taint in place, and
                   //dross:taint-cleared above an exec.Command leaves exec-consent's finding in
                   place
                 - a marker whose next line defines no tainted value is an "orphan marker" finding,
                   so blanket-marking a file fails
                 - an Errorf of the raw `out` on the line after a marked conversion still trips

  t-7  Type-key the unwrap ban; walk the derived set  (depends t-2)
       files:    internal/cmd/pathfence_enum_test.go,
                 internal/cmd/security_test.go,
                 internal/cmd/testdata/pathfence_scan/unwrap_renamed.go.txt (NEW)
       covers:   c-9
       desc:     The unwrap ban fires on String()/Rel() only when types.Info says the receiver is
                 pathfence.Contained. scannedPackages goes; both the unwrap and the literal walks
                 cover t-2's set, minus internal/pathfence for the literal ban. The containedVar
                 name list in security_test.go is replaced by the same type test.
       contract: - strings.Builder.String() inside os.WriteFile is not flagged. A Contained held
                   under an unlisted name (`x := c; os.ReadFile(x.String())`) IS flagged. The old
                   name-keyed rule fails the second case
                 - both walks visit internal/argfence and internal/hostallow. Any package in the
                   derived set visited with zero files fails per-package
                 - the existing fixtures keep their written counts: unwrap_os 5, rundir_revert 3,
                   seam_ok 0, dotdot_path 3, dotdot_revrange 0
                 - security_test's run-dir check flags `renamed.String()` on a Contained whose name
                   is not ledgerPath, outPath or reportPath

  t-8  Classify every serialized string field  (depends t-2)
       files:    internal/cmd/pathfence_fields_test.go,
                 internal/pathfence/fields.go,
                 internal/pathfence/fields_test.go,
                 internal/cmd/testdata/pathfence_scan/task_experiment.go.txt (NEW)
       covers:   c-9
       desc:     schemaDirs goes. The schema set is struct types, closed over their field types,
                 that reach a TOML or JSON encode/decode call in any package of t-2's set.
                 pathShapedTags goes too. Every toml- or json-tagged string or []string field in
                 the set must be declared, either in pathfence.Fields() or in a new
                 pathfence.NotPaths() that lists field names per struct with a Why. Both
                 registries are two-way stale-checked.
       contract: - task_experiment.go.txt is phase.Task plus WorkDir `toml:"workdir"`, OutputDir
                   `toml:"output_dir"` and Spec `toml:"spec"`. It yields exactly three
                   undeclared-field failures, one naming each, and stays in the tree as a
                   must-fail fixture
                 - a struct in a package outside the old four dirs, passed to
                   toml.NewEncoder(w).Encode, fails when it carries an undeclared string field.
                   This proves the unlisted-schema-dir half
                 - a NotPaths name that no longer exists fails as stale, and a field declared in
                   both registries fails. pathfence.Validate rejects a NotPaths entry with an empty
                   Why
                 - the walker self-test's type rule holds: bool, int and untagged fields are never
                   reported. The live walk finds at least a stated floor of serialized string
                   fields

  t-9  Run exec-consent live proofs on the shared graph  (depends t-4)
       files:    internal/cmd/execconsent_audit_test.go,
                 ARCHITECTURE.md
       covers:   c-10, c-8
       desc:     repoExecGraph reads the shared program and stops re-parsing. The four rewrite
                 tests (run.go consent, verify.go gate, open.go marker, gremlins.go marker) become
                 surgery on a COPY of the joined graph: drop a call edge, or toggle a marker at an
                 anchored source line. The "rewrite never matched" tripwire stays as "anchor never
                 matched". ARCHITECTURE.md's exec-consent section and anchors move to the SSA
                 call-graph description.
       contract: - after every exec-consent test has run, the load counter is still 1.
                   repoExecGraph reloads nothing
                 - dropping run.go RunE's consent.RunConsented edge makes run.go's spawn an
                   "ungated" finding. Dropping verify's requireExecConsent gate flags exactly
                   `want` mutation spawns. An anchor absent from the source fails with "anchor
                   never matched" instead of passing on an unmodified graph
                 - the shared graph's edge count is identical before and after the surgery tests,
                   and under -shuffle=on TestToolchainSpawnsResolveAsGated stays green whatever
                   order the tests run in
                 - the 30-site / 20-file floor holds, and restricting the shared program to cmd/
                   still fails it
                 - execConsentGatedFiles and execConsentMarkedFiles are unchanged, and the live
                   tree has zero unresolved-dispatch findings. The migration adds no marker to reach
                   green

Wave 4

  t-10 Trace declared path fields to os.* calls  (depends t-2, t-5)
       files:    internal/cmd/pathtaint_audit_test.go (NEW),
                 internal/cmd/testdata/pathtaint/raw_os.go.txt (NEW),
                 internal/cmd/testdata/pathtaint/contained_ok.go.txt (NEW)
       covers:   c-4, c-7
       desc:     A second policy runs on t-3's engine. Sources are Field/FieldAddr reads of every
                 pathfence.Fields() entry, Consumed and NotConsumed alike, each resolved to a
                 types.Var. Sinks are any package-level os function that receives the string. An
                 argument to pathfence.Contain clears. There is no marker for this scan. Includes
                 the live gate and floor for the path scan.
       contract: - raw_os.go.txt: a changes.TaskRecord.Files element passed to os.ReadFile trips
                   and names the field. project.Env.Files (NotConsumed) passed to os.Stat trips.
                   filepath.Join(root, f) passed to os.Open trips. The field copied into a local
                   struct in another function and then passed to os.Remove trips
                 - contained_ok.go.txt: field → pathfence.Contain → pathfence.ReadFile is clean,
                   and a field that is only printed is clean
                 - a registry entry naming a struct or field absent from the load fails with
                   "unresolved declaration" rather than dropping out of the source set
                 - live tree: zero findings, and at least a stated floor of declared-field reads.
                   If this surfaces a live violation, the fix is to construct a Contained. It is
                   never to redeclare the field NotConsumed

  t-11 Pin the pre-phase stryker branch as must-trip  (depends t-5)
       files:    internal/cmd/taint_stryker_test.go (NEW),
                 internal/cmd/testdata/taint/stryker_prephase.go.txt (NEW),
                 internal/mutation/toolfence_residual_test.go
       covers:   c-3
       desc:     The fixture carries 6f27eaa^'s branch verbatim: the MultiWriter tee on
                 Cmd.Stdout/Stderr, headBuffer, quote() rendering into a strings.Builder, the
                 reportless errors.New(msg), and checkInstrumented's Errorf. Minimal stubs keep it
                 type-checking. It also carries the spec's two-line paraphrase: a `quoted`
                 strings.Builder, then fmt.Errorf(…, quoted.String()) on the next line. The
                 residual guard's "does not catch" note now names this guard as the one that
                 closes the gap.
       contract: - findings land at three places: the reportless `return nil, errors.New(msg)`,
                   checkInstrumented's `return fmt.Errorf("%s\n%s", msg, head.quote(...))`, and the
                   paraphrase's Errorf line. Each names the `cmd.Stdout = sink` origin
                 - the fixture's tee, quote() and escape lines equal a verbatim const copied from
                   `git show 6f27eaa^:internal/mutation/stryker.go` (lines 115-118, 142, 150,
                   201-222, 315). Editing the fixture toward a shape the engine handles fails the
                   pin
                 - a copy with errors.New(msg) swapped for printHead(os.Stderr, …) plus a
                   fixed-prose error yields zero findings. This proves the trip follows the flow,
                   not the file

  t-12 Burn down gh output taint in ship  (depends t-5, t-6)
       files:    internal/ship/open.go, internal/ship/headpr.go, internal/ship/basepr.go,
                 internal/ship/merged.go, internal/ship/comment.go,
                 internal/cmd/taint_ship_test.go (NEW)
       covers:   c-1
       desc:     The four `fmt.Errorf("gh …: %w\n%s", err, string(out))` sites stop carrying gh's
                 output. The output is printed to os.Stderr at the failure point, and the error
                 keeps the verb and the exit status. The PR URL and the JSON parsed from gh's
                 stdout each get a taint-cleared marker at the conversion. Error-text pins in
                 open_test.go, headpr_test.go, basepr_test.go and merged_test.go MOVE to captured
                 stderr.
       contract: - with gh stubbed to exit 1 and print CANARY: for each of the four verbs the
                   error names the verb and has no CANARY, while captured stderr has CANARY.
                   Deleting a pin instead of moving it leaves the CANARY-in-stderr half unasserted
                   and fails review against this line
                 - the scoped gate over internal/ship reports zero findings, and removing any added
                   marker makes it report that exact line

  t-13 Burn down codex and scanner taint  (depends t-5, t-6)
       files:    internal/codex/git.go, internal/codex/ast_grep.go,
                 internal/security/run.go, internal/quality/run.go, internal/techdebt/run.go,
                 internal/cmd/taint_scanners_test.go (NEW)
       covers:   c-1
       desc:     Each conversion of git log, ast-grep or scanner output into data dross persists
                 or prints gets a taint-cleared marker. Its prose names what the decoded values
                 are and where they go. Any error carrying tool output moves that output to
                 stderr.
       contract: - the scoped gate over codex, security, quality and techdebt reports zero
                   findings, and the marker census in these files has no orphan
                 - removing the rev-parse marker in internal/security/run.go reports that line and
                   names the git spawn as its origin

  t-14 Burn down git plumbing taint in cmd  (depends t-5, t-6)
       files:    internal/cmd/cleantree.go, internal/cmd/init.go, internal/cmd/milestone_stale.go,
                 internal/cmd/pause.go, internal/cmd/phase.go, internal/cmd/ship_recover.go,
                 internal/cmd/statusline.go, internal/cmd/worktree_files.go,
                 internal/cmd/taint_gitplumbing_test.go (NEW)
       covers:   c-1
       desc:     Covers SHAs, branch names and path lists sliced from git output, plus
                 milestone_stale's &diff buffer. Where the value stays text it gets a marker at the
                 conversion. A value only compared, counted or Contains-checked needs nothing,
                 because the type rule clears it. Error text carrying output moves to stderr, with
                 its test pin moved beside it.
       contract: - the scoped gate over these eight files reports zero findings, and no marker is
                   an orphan
                 - milestone_stale's diff buffer reaching a returned error trips before the fix.
                   Written as a fixture copy of the pre-fix function, it is kept as a regression
                   case

  t-15 Burn down tool streams in mutation and remote  (depends t-5, t-6, t-9)
       files:    internal/mutation/gremlins.go, internal/mutation/stryker_net.go,
                 internal/mutation/construct.go, internal/remote/hold.go, internal/remote/remote.go,
                 internal/compilefence/compilefence.go,
                 internal/cmd/taint_toolstreams_test.go (NEW)
       covers:   c-1
       desc:     Tool streams must end at the terminal or the recorder, not at a marker. Covers
                 construct.go's stdout buffer, hold.go's stderr sink and remote's transport output.
                 compilefence's CombinedOutput reaching t.Fatalf gets a marker whose prose says it
                 is test-only.
       contract: - a //dross:taint-cleared marker in internal/mutation/stryker.go, gremlins.go or
                   stryker_net.go is itself a finding. The tool stream cannot be cleared by
                   annotation
                 - the scoped gate over internal/mutation, internal/remote and internal/compilefence
                   reports zero findings
                 - t-9's gremlins.go surgery anchor still matches after this task's edits

  t-16 Burn down user-command streams in cmd  (depends t-5, t-6, t-9)
       files:    internal/cmd/lane_install.go, internal/cmd/run.go, internal/cmd/test.go,
                 internal/cmd/update.go, internal/cmd/verify.go, internal/cmd/survivor_drain.go,
                 internal/cmd/techdebt.go,
                 internal/cmd/taint_usercmds_test.go (NEW)
       covers:   c-1
       desc:     lane_install stops returning the install output as a string. It now goes to
                 stderr, and its captured-output test pin moves there too. The test/run/verify
                 streams reach Cmd.Stdout through a cobra OutOrStdout-derived writer with no
                 marker. survivor_drain's and techdebt's `git ls-files` lists get conversion
                 markers.
       contract: - the scoped gate over these seven files reports zero findings, with no marker
                   on the test.go, run.go or verify.go stream sites
                 - t-9's run.go and verify.go surgery anchors still match after this task's edits
                 - lane_install with a stub installer printing CANARY and exiting 2: CANARY is in
                   captured stderr and in no returned value

Wave 5

  t-17 Findings name escape, origin and remedy  (depends t-5, t-10)
       files:    internal/cmd/srcfinding_test.go (NEW),
                 internal/cmd/testdata/taint/one_violation.go.txt (NEW),
                 internal/cmd/testdata/pathtaint/one_violation.go.txt (NEW)
       covers:   c-6
       desc:     One fixture per scan, each with one violation. The taint spawn sits two
                 packages away from the escape. Assertion lines are looked up by anchor text,
                 never hard-coded.
       contract: - the taint finding's text contains three things: the escape's file:line, the
                   spawn's file:line in the other package, and the remedy ("print it to os.Stderr
                   … or mark the conversion with //dross:taint-cleared <reason>")
                 - the path finding contains the escape's file:line, "changes.TaskRecord.Files",
                   and "construct a pathfence.Contained via pathfence.Contain"
                 - a value merged from two spawns through a phi names both origins. A finding type
                   that keeps only the first origin fails this

  t-18 Land the live taint gate, floors and budget  (depends t-10..t-16)
       files:    internal/cmd/taint_audit_test.go (NEW)
       covers:   c-1, c-7, c-8
       desc:     TestNoSpawnOutputEscapes runs the exec policy over t-2's full set. It lands LAST,
                 after every burn-down, so a guard that would be red mid-phase never gets loosened.
                 Floors are set about 25% under the measured source and file counts. The CI timing
                 table is read for c-8.
       contract: - zero findings on the live tree. Live sources number at least the source floor,
                   across at least the file floor. The walk restricted to cmd/ falls under the
                   floor
                 - the repo-wide census finds no orphan //dross:taint-cleared marker
                 - c-8 evidence for verify: the testsummary per-package total for
                   github.com/Rivil/dross/internal/cmd on the phase PR run is at most main's latest
                   run + 15s, both read from the timing table; the local -race elapsed of the scan
                   tests is recorded next to it
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (spawn output ends terminal / recorder / marker) | t-3, t-6, t-12, t-13, t-14, t-15, t-16, t-18 |
| c-2 (taint crosses returns, fields, io.Writer args) | t-5 |
| c-3 (pre-phase stryker branch fails the guard) | t-11 |
| c-4 (declared path field raw into os.*) | t-10 |
| c-5 (derived package set + outside-the-walk check) | t-2 |
| c-6 (finding names escape, origin, remedy) | t-17 |
| c-7 (floors + written must-trip/must-not-trip corpora) | t-1 (harness), t-3, t-10, t-18 (floors); t-9 keeps exec floors |
| c-8 (load once; ≤15s from the timing table) | t-1 (once + module-only bodies + early -race measure), t-9 (no reload), t-18 (CI read) |
| c-9 (pathfence guards cannot go stale silently) | t-7, t-8 |
| c-10 (exec-consent reach via SSA call graph) | t-4, t-9 |

## Judgment calls

- **Fixtures stay `.go.txt`.** Each is type-checked into its own ssa.Program by an importer backed by the single load. I rejected real `.go` under testdata: execconsent's WalkDir has no testdata skip, so fixture spawns would become live sites. I also rejected `go list -overlay` into the one load, because fixture implementers would then pollute CHA/VTA over live interfaces.
- **The loader env is snapshotted with HOME=ambientHome.** TestMain replaces HOME with an empty dir, and `go list` under it means a download plus a cold compile. I rejected loading eagerly inside TestMain, which would charge every `go test -run X` the full load.
- **Exec-consent keeps its AST facts and takes reach edges from SSA/VTA**, joined by token.Pos from the load's own pkg.Syntax. I rejected a full SSA port of command and gating detection: that is roughly 800 lines of proven logic re-derived, which is the verdict-drift risk itself. I also rejected SSA-only-for-interface-fan-out: its join keys break under rewrites, and c-10 says reach goes through the graph.
- **Rewrite tests become graph surgery on a copy, with anchor tripwires.** I rejected overlay reloads, which would add at least four loads per binary and break c-8's "once".
- **"Unresolved" means VTA yields no callee while CHA has a spawn-reaching candidate.** I rejected reporting every VTA-empty site, which is noise on dead code. I rejected a CHA-fallback union, which is exactly what c-10 retires.
- **Field taint is field-based (T.f is global), not object-sensitive.** x/tools no longer ships pointer analysis. The false positives are accepted as fail-closed. Param flows, by contrast, are context-sensitive summaries (R13), because a context-insensitive rule turns every shared helper into marker spam.
- **External calls follow a type rule plus a fixed stdlib transform-package set.** A numeric or bool result clears. Other results, errors included, carry taint. Taint reaching any callee outside the set is an escape. I rejected a results-only rule, which is blind to os.WriteFile. I rejected a per-function stdlib summary table, a hand list that goes stale silently. The package set never CLEARS anything, so adding to it cannot launder.
- **Output()'s error is not a source; reading ExitError.Stderr is.** ExitError.Error() renders only "exit status N". Tainting every Output() error would make every `%w` wrap of a git failure a finding that carries no output.
- **The recorder is not a blanket sink.** Its safety falls out of clearance, since Observed takes an int. A tainted `tool` string passed to RecordToolFailure is still a finding.
- **The directive name is `//dross:taint-cleared`.** It clears only values defined on the next line, and an orphan marker is a finding. The path scan gets no marker: a raw declared path reaching os.* has one fix, a Contained.
- **c-9 classifies every tagged string field, with the schema set derived from encode/decode reachability.** I rejected widening the tag vocabulary with dir/spec morphemes. That passes the 09-07 fixture by adding its words, then misses the next unlisted one, so it is a word list in disguise. The cost is a larger NotPaths table that fails closed rather than staying silent.
- **All scans stay in internal/cmd's test binary, with one load.** I rejected a dedicated scan package. It would give c-8 a clean per-package timing row. But the exec-consent audit is bound to internal/cmd's internals (runCmd, testdata, trust.go's doc pin), so moving it means two loads and two "their package"s.
- **The live taint gate lands last.** Burn-down tasks assert per-area zero findings in their own new test files. I rejected landing the global gate with the engine, because a guard that is red mid-phase gets loosened until it passes. That is the tool-output-not-persisted precedent.
- **t-15 and t-16 run after t-9.** t-9's surgery anchors live in run.go, verify.go and gremlins.go. The burn-downs edit those files, and a parallel edit could silently move an anchor.
- **The c-3 fixture carries both the literal 6f27eaa^ lines and the spec's two-line `quoted` paraphrase.** The squash merge lost the intermediate commits, so "verbatim" is pinned to the one pre-phase revision that exists.
- **The -race cost is measured in wave 1, before any engine is built.** It is the cheapest point to find out that SSA plus VTA does not fit in 15s. At t-18 the fix would mean re-architecting every scan.
- **c-6's fixture covers the two scans that trace to a source (taint, path).** Exec-consent already has TestExecConsentFindingNamesFileLineAndRemedy, and t-8 asserts that the undeclared-field message names the field.
