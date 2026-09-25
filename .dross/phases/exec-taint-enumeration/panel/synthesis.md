# exec-taint-enumeration: panel synthesis

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | 4/5. All 10 criteria covered, each with a named owner. Its c-1 burn-down misses 11 of the 14 files that call `gitCombined`. Those 11 hold 25 `%w\n%s` output-into-error sites (10 in milestone.go) plus redproof_replay's `%v: %s`. Its gate lands last, so it would go red on files no task owns. | 3/5. All 10 covered on paper, but the t-7 drain has the same `gitCombined` gap. It never pins the loader's go caches: TestMain's throwaway HOME makes `go list` cold-compile and download, and c-8 fails before any scan runs. | 5/5. All 10 covered. It is the only draft whose burn-down owns every `gitCombined` caller (t-12). It also catches the stale "8 of 36 packages, 11-tag, 4 schema dirs" claim in ARCHITECTURE.md. |
| Test-contract specificity | 5/5. Every contract is the test for a named failure mode: context-sensitive params, recorder not a blanket sink, the os.WriteFile blind spot, anchor tripwires. It has two factual errors. "unwrap_os keeps 5" contradicts its own builderWrite bullet. Its gh pin list names test files that do not pin gh output and misses comment_test.go:230. | 3/5. Strong corpus-equality and one-per-channel contracts. It is the only draft right about builderWrite flipping. Its loader and drain contracts are thin: nothing on the strconv error, orphan markers, context sensitivity or go caches. | 5/5. Floors tested both ways, pure-set pinning, binary-blind findings, CANARY behaviour tests, and awareness of TestNoTestLost and gitCallFuncs. It repeats the "unwrap_os 5" error. Its burn-down contracts only "log zero suppressed", so they fail nothing. |
| Granularity | 4/5. Burn-downs are sized by area and the live-proof task is its own. t-1 bundles the loader, fixture builder and WANT harness: 5 files, 3 layers. | 2/5. t-2 is the engine, every source, cross-function flow, the marker, c-3, c-6 and the floor. t-7 is about 30 files plus the gate. | 4/5. Clean layer split: loader, parser, fixture builder, sweep. t-12 at 17 files is justified as compile-atomic. |
| Wave correctness | 4/5. A tight 5 waves, and it catches the anchor ordering. But the gh burn-down t-12 edits open.go, which is a t-9 surgery anchor, without depending on t-9. Path taint t-10 does not wait for t-8's new registry entries. | 3/5. The dependencies are right for its shape, but it reaches 3 waves only by collapsing layers. | 4/5. Correct but 7 waves. t-15 rewrites the exec-consent ARCHITECTURE entry without depending on t-7. |

**Skeleton: risk.** Every failure mode has exactly one owning task. It is the only draft that keeps fixture types out of the live VTA/CHA graph, orders burn-downs after the exec-consent anchor surgery, and lands the global gate only once it can be green. Verification is a close second and supplies most of the grafts: the layer split, gitRun, CANARY contracts, ARCHITECTURE.md, and the discovery step. mvp supplies the builderWrite correction and the c-9 ledger shape.

## Merged plan

```
Phase exec-taint-enumeration — 21 tasks across 7 waves

Wave 1
  t-1  Load the module once for source scans  [risk+verification+mvp]
       files:    go.mod, go.sum, internal/cmd/srcprog_test.go (new), internal/cmd/hermetic_env_test.go
       covers:   c-8
       desc:     One sync.Once packages.Load("./...") from the repo root. Tests:false; syntax and
                 types for module packages; export data for deps. Env is snapshotted with
                 HOME=ambientHome, and any package error is fatal. SSA bodies are built for module
                 packages only, plus one VTA graph with a CHA seed.
                 Gate before t-5: measure load+SSA+VTA under -race and record it in the commit
                 body. Over 10s, narrow the scope first.
       contract: - a second loader (a second Once, or a helper bypassing it) makes TestMain exit
                   "source program loaded 2 times". TestOnePackagesLoadCallSite's AST walk of
                   internal/cmd/*_test.go finds exactly one packages.Load and names any other one's
                   file:line
                 - TestSharedLoadUsesAmbientGoCaches: Config.Env GOCACHE/GOMODCACHE equal `go env`
                   under ambientHome, and neither sits under TestMain's HOME. A loader that inherits
                   os.Environ() fails this; with GOPROXY=off it fails on a module download
                 - loadErrors() fed a synthetic package with one TypeError returns an error naming
                   that import path. The live load has zero Errors/TypeErrors, and every package has
                   Types, TypesInfo and Syntax
                 - no *ssa.Function with non-nil Blocks lies outside github.com/Rivil/dross. Building
                   dependency bodies (the dominant -race cost) fails this
                 - golang.org/x/tools/... is absent from cmd/dross's import closure, no non-test file
                   imports it, and `go mod tidy -diff` exits 0 (CI's tidy gate)
                 - no loaded CompiledGoFiles entry ends in _test.go

  t-2  Share exec-exempt grammar with clearance marker  [verification+risk]
       files:    internal/cmd/execconsent_audit_test.go, internal/cmd/directive_test.go (new)
       covers:   c-1
       desc:     Extract execExemptMarkers into a directive-generic parser (fset, file, directive)
                 with the execExemptMinReason (20) floor; exec-consent calls it unchanged. Declare
                 the clearance directive //dross:taint-cleared (name: Disagreement 10).
       contract: - TestDirectiveGrammarIsShared: every marker-grammar row of snippets.txt gets the
                   same verdict under exec-exempt and taint-cleared. The rows are reasonless,
                   one-word reason, glued, spaced, two lines above, below the call, and tab before
                   the reason. A diverging second parser fails, naming the row and the directive
                 - TestExecConsentMarkerGrammar's three verdicts, TestExecConsentFlagsItsOwnSnippets,
                   and TestExecConsentSkipsTestFiles' 1-site/1-finding all pass with their assertions
                   untouched
                 - a well-formed //dross:taint-cleared above an exec.Command leaves the exec-consent
                   finding in place. The two directives are not interchangeable

Wave 2 (depends t-1)
  t-3  Type-check fixtures on the shared import graph  [verification+risk]
       files:    internal/cmd/ssafixture_test.go (new), internal/cmd/srccorpus_test.go (new),
                 internal/cmd/testdata/ssa_fixture/{root,helper,badimport,typeerror,want_mismatch}.go.txt (new)
       covers:   c-7
       desc:     typecheckFixture groups .go.txt files by package clause and type-checks them through
                 an importer backed by t-1's type graph (or by a sibling fixture package). The result
                 goes into a SEPARATE ssa.Program with its own VTA, never overlaid into the live load.
                 Also adds the `// WANT <scan>` corpus harness.
       contract: - a two-package fixture importing os/exec, cobra and example.com/fixture/helperpkg
                   type-checks in either file order. An importer that sees only the stdlib fails on
                   cobra
                 - badimport fails naming the missing path and is never skipped. typeerror fails
                   naming file:line. No SSA is ever built from ill-typed source
                 - with a stub scanner, want_mismatch reports BOTH the MISSED WANT line and the
                   UNEXPECTED finding line. The parsed WANT count must equal the asserted count, so a
                   parser that drops the last annotation fails. An empty corpus dir fails
                 - a *_test.go.txt fixture is excluded, and fixture builds never call packages.Load.
                   No fixture function appears in the live program's call graph

  t-4  Derive the scanned set; cross-check every file  [risk+verification+mvp]
       files:    internal/cmd/srcscope_test.go (new)
       covers:   c-5, c-7
       desc:     The scanned set is every package t-1 loads under the module path; there is no roots
                 list. An independent go/parser sweep ignores build constraints and checks every
                 non-test .go file outside a `testdata` segment. Any file holding exec.Command or
                 CommandContext, or a selector naming a pathfence.Fields() field, must appear in some
                 loaded CompiledGoFiles.
       contract: - the loaded dir set equals the WalkDir set of dirs with non-test .go files. A dir on
                   only one side fails, named. The set includes internal/cmd, internal/ship,
                   internal/mutation, cmd/dross, cmd/testsummary and assets
                 - the live set holds at least a floor about 25% under today's package count, and no
                   path contains /testdata/
                 - on a temp tree, each of these is reported with its file name and "spawn" or
                   "field read": extra/ missing from the walk; a //go:build ignore file with
                   exec.Command; a nested go.mod module with exec.Command; a build-tagged file reading
                   changes.TaskRecord.Files. With those files in the loaded set, nothing is reported
                 - the live tree has zero outside-walk files, and the sweep finds at least 20 spawn
                   files (28 today). Both internal/mutation/freespace_{unix,other}.go are swept
                 - internal/mutation/testdata/ceiling is out. A dir named internal/testdatabase
                   would stay in

Wave 3
  t-5  Taint sources, in-function flow, sink classes  [risk+verification+mvp]  (depends t-3)
       files:    internal/cmd/taint_engine_test.go (new),
                 internal/cmd/testdata/taint/{sources,sinks}.go.txt (new)
       covers:   c-1, c-7
       desc:     An engine parameterized by source, sink and clear predicates; t-12 reuses it.
                 Exec sources are matched by types.Func/Var identity: Output/CombinedOutput results,
                 StdoutPipe/StderrPipe reads, ExitError.Stderr reads, and referents stored into
                 Cmd.Stdout/Stderr.
                 External calls: a numeric or bool result clears; any other result, error included,
                 stays tainted. Reverse taint flows only into a fixed stdlib transform-package set.
                 Any other external callee that receives taint is an escape.
                 Terminal means os.Stdout/os.Stderr and cobra's OutOrStdout/ErrOrStderr; an error is
                 never terminal. A tainted return is a fail-closed escape until t-9. Exposes a
                 clearAt(pos) hook. Findings carry the escape position and the origin set.
       contract: - sources.go.txt: Output, CombinedOutput, StdoutPipe and StderrPipe reads, an
                   ExitError.Stderr read after errors.As, and a &bytes.Buffer on Cmd.Stdout each
                   reach errors.New with exactly one WANT finding. os/exec imported as `x`, and an
                   embedded *exec.Cmd's promoted Output(), trip identically
                 - a local type with its own Output() does NOT trip (identity, not name). Neither
                   does `_, err := c.Output(); fmt.Errorf("git: %w", err)`, because ExitError renders
                   only "exit status N"
                 - strconv.Atoi(out): the int going into Errorf is clean and the error compared to
                   nil is clean. The error wrapped with %w trips at the wrap line.
                   Contains/HasPrefix/==/len clear; TrimSpace/Split/Fields/Sprintf stay tainted
                 - Fprintln(os.Stderr, out) and Fprint(cmd.ErrOrStderr(), out) are clean.
                   Fprint(&b, out) into a strings.Builder, then errors.New(b.String()), trips
                 - os.WriteFile(p, out, 0o600) trips, and so does os.Setenv(k, tainted). Unknown
                   externals default to deny; a results-only rule misses both
                 - RecordToolFailure with a tainted `tool` argument trips, while Observed(len(out)) is
                   clean
                 - fixtures that differ only in "git" vs "cargo" give identical findings, and the
                   finding struct has no binary field. Every transform-set package is the callee
                   package of at least one corpus line; an entry with none fails, named

  t-6  Route exec-consent reach through the VTA graph  [risk+verification+mvp]  (depends t-2, t-3)
       files:    internal/cmd/execconsent_audit_test.go,
                 internal/cmd/testdata/exec_consent/{dispatch,unresolved}.go.txt (new)
       covers:   c-10
       desc:     AST facts (sites, markers, cobra commands, gating, the AddCommand tree) come from
                 the load's pkg.Syntax. Every reach edge comes from the shared VTA graph, joined by
                 token.Pos. The implementers union, methodsOn and execScope inference are deleted.
                 A dispatch is reported, never unioned, when VTA yields no callee and a CHA
                 candidate reaches a spawn.
       contract: - every snippets.txt row keeps its FLAG/PASS with headers == rows == exercised, and
                   the header lines stay byte-identical
                 - reach verdicts are unchanged: git log is ungated through two hops; go test is
                   gated through the runFn seam; rsync is mixed; markdownlint is ungated; cargo is
                   "no command at all". The var-seam, severed-edge and extra-hop tests and
                   TestFuncLiteralSpawnIsAttributedThroughItsVar keep their names and assertions
                   (TestNoTestLost green)
                 - dispatch: a command passing only Quiet{} to Drive(r Runner) does NOT reach Loud's
                   spawn in the other package. The Drive(Loud{}) twin attributes it. A CHA or union
                   resolution fails the first half
                 - unresolved: r.Go() on a Runner returned by a bodiless call, with a spawning
                   implementer present, is a finding that names the method and the call site's
                   file:line. TestExecReachReportsAmbiguity still says "across 2 packages".
                   Deleting the report branch fails both

  t-7  Type-key the unwrap ban; walk derived set  [risk+mvp]  (depends t-3, t-4)
       files:    internal/cmd/pathfence_enum_test.go, internal/cmd/security_test.go,
                 internal/cmd/testdata/pathfence_scan/unwrap_os.go.txt,
                 internal/cmd/testdata/pathfence_scan/unwrap_renamed.go.txt (new)
       covers:   c-9
       desc:     String()/Rel() is flagged only when TypesInfo says the receiver is
                 pathfence.Contained. security_test's containedVar name list gets the same type
                 test. scannedPackages is deleted: the unwrap and ".." walks cover t-4's set, and the
                 ".." ban excludes internal/pathfence.
       contract: - unwrap_os's builderWrite row (strings.Builder.String() in os.WriteFile) flips from
                   must-trip to must-not-trip. The fixture's count goes from 5 to 4, and the other four
                   rows still trip. Name matching fails the builderWrite row (Disagreement 13)
                 - unwrap_renamed: `x := c; os.ReadFile(x.String())` IS flagged. security_test's
                   run-dir check flags renamed.String() on a Contained whose name is not ledgerPath,
                   outPath or reportPath
                 - rundir_revert keeps 3, seam_ok 0, dotdot_path 3, dotdot_revrange 0.
                   TestDotDotScanSeesTheRealLiterals still finds pathfence's literals, and the ban
                   reports zero everywhere else
                 - both walks visit internal/argfence and internal/hostallow; a derived package
                   visited with zero files fails. The ban examines at least a floor of os.* sites, and
                   a walk that drops internal/security falls under it

  t-8  Classify every serialized string field  [risk+mvp+verification]  (depends t-3, t-4)
       files:    internal/cmd/pathfence_fields_test.go, internal/pathfence/fields.go,
                 internal/cmd/testdata/pathfence_scan/not_paths.txt (new),
                 internal/cmd/testdata/pathfence_scan/task_experiment.go.txt (new)
                 (+ internal/cmd/pathfence_carrier_test.go if a newly found field is Consumed)
       covers:   c-9
       desc:     schemaDirs and pathShapedTags are deleted. Every toml/json-tagged field in a t-4
                 package whose underlying type is string or []string must be declared, either in
                 pathfence.Fields() or in not_paths.txt. Both are checked in both directions. There
                 is no -update flag: a failure prints the exact row to paste. Undeclared path fields
                 found today (such as cmd.detachedRun.RunDir) get registry entries with Why and
                 Readers.
       contract: - task_experiment is phase.Task plus WorkDir `toml:"workdir"`, OutputDir
                   `toml:"output_dir"` and Spec `toml:"spec"`. It yields exactly three undeclared
                   findings, one naming each field, and the control without them yields zero. It
                   stays in the tree as a must-fail fixture
                 - a tagged string field in a fixture package outside the old four dirs (package
                   remote) is reported. The live detachedRun.RunDir (internal/cmd/local.go
                   `toml:"run_dir"`) is walked and declared
                 - a field of `type RelPath string` is enumerated; a bool tagged `public` is not
                 - a not_paths.txt row naming a vanished field fails as stale, and a field in both
                   registries fails as double-declared. TestRegistryIsWellFormed is green over every
                   new entry
                 - TestWalkerSelfTest keeps its name, and its want now includes the replay-tagged
                   field. The walker floor sits about 25% under the measured count (~450)

Wave 4
  t-9  Carry taint across returns, fields, writers  [risk+verification]  (depends t-5)
       files:    internal/cmd/taint_flow_test.go (new), internal/cmd/taint_engine_test.go,
                 internal/cmd/testdata/taint/channels_{return,field,writer}.go.txt (new)
       covers:   c-2
       desc:     Context-sensitive summaries (param→result, param→field, param→writer-param),
                 applied at each call site, replace t-5's fail-closed return. Field taint is keyed
                 by field. A reverse referent closure runs through MakeInterface, ChangeType, Phi,
                 field loads, transform constructors and params. A tainted Write receiver makes its
                 []byte parameter a source.
                 On landing: run the exec policy over the live set once and record the per-file
                 findings in the task notes. Any file no burn-down (t-14..t-19) owns gets
                 `dross task add` before wave 5.
       contract: - return: a.Rev() returns TrimSpace(string(out)), and b's errors.New(a.Rev()) trips
                   at b's line. A caller that only prints Rev() to os.Stderr is clean
                 - context: helper norm(s) is called with output at site A and with a literal at
                   site B. Only A's consumer trips; a context-insensitive param rule fails this
                 - field: output stored in Result.Log by one function and read into Errorf by another
                   trips. A store to res.Other followed by a read of res.Log is clean (keyed by
                   field, not whole struct)
                 - writer: Run(w){c.Stdout = w; c.Run()} with a &strings.Builder, then errors.New(
                   b.String()), trips. With os.Stderr it is clean, and so is a cmd.OutOrStdout()
                   writer set through an options-struct field. A writer param with no caller is an
                   escape. A package `var out io.Writer = os.Stderr` never reassigned is terminal;
                   reassigning it to a builder trips
                 - a toStr(b) conversion helper feeding Errorf trips. With the tee
                   io.MultiWriter(os.Stderr, head), head.buf.String() into Errorf trips, while a head
                   that only leaves through printHead(os.Stderr, …) is clean
                 - a closure capture, a generic id[T], and a chan string passed between goroutines
                   (hold.go's shape) each carry taint to a tripping Errorf. A two-function recursion
                   reaches its fixpoint with its one finding

  t-10 Add the //dross:taint-cleared conversion marker  [risk+verification]  (depends t-2, t-5)
       files:    internal/cmd/taint_marker_test.go (new), internal/cmd/testdata/taint/markers.go.txt (new)
       covers:   c-1
       desc:     Parsed by t-2's parser, the marker binds to the line immediately below. It clears
                 only the tainted values defined on that line, and their downstream uses. A marker
                 that binds no tainted definition is itself an orphan finding.
       contract: - a valid marker directly above `sha := strings.TrimSpace(string(out))` clears every
                   downstream use of sha; the same marker two lines up does not
                 - //dross:exec-exempt in the same slot clears nothing
                 - a marker whose next line defines no tainted value is an "orphan marker" finding,
                   so blanket-marking a file fails
                 - an Errorf of the raw `out` on the line after a marked conversion still trips

  t-11 Run exec-consent live proofs on shared graph  [risk]  (depends t-6)
       files:    internal/cmd/execconsent_audit_test.go
       covers:   c-10, c-8
       desc:     repoExecGraph reads the shared program and stops re-parsing. The four rewrite tests
                 (run.go consent, verify.go gate, open.go marker, gremlins.go marker) become surgery
                 on a COPY of the joined graph: drop an edge, or toggle a marker at an anchored
                 line. "rewrite never matched" becomes "anchor never matched".
       contract: - once every exec-consent test has run, the load counter still reads 1
                 - dropping RunE's consent.RunConsented edge makes run.go's spawn "ungated". Dropping
                   verify's requireExecConsent gate flags exactly `want` mutation spawns. An anchor
                   absent from source fails with "anchor never matched"
                 - the shared graph's edge count is identical before and after surgery, and under
                   -shuffle=on TestToolchainSpawnsResolveAsGated stays green
                 - the 30-site / 20-file floor holds, and restricting the program to cmd/ fails it
                 - execConsentGatedFiles and execConsentMarkedFiles are unchanged, and the live tree
                   has zero unresolved-dispatch findings. The migration adds no marker to get there

Wave 5  (burn-down rule for t-14..t-18: if the engine proves imprecise, fix it and add the
         must-not-trip corpus row first [mvp]; failure output is fixed, never marked)
  t-12 Trace declared path fields to os.* calls  [risk+mvp+verification]  (depends t-4, t-8, t-9)
       files:    internal/cmd/pathtaint_audit_test.go (new),
                 internal/cmd/testdata/pathtaint/{raw_os,contained_ok}.go.txt (new)
       covers:   c-4, c-7
       desc:     A second policy on the engine. Its sources are Field/FieldAddr reads of every
                 pathfence.Fields() entry, resolved to a types.Var. Its sinks are package-level os
                 functions. pathfence.Contain clears a Consumed field. A NotConsumed field never
                 clears: opening it through the seam is a finding too (Disagreement 8). There is no
                 marker. Includes the live gate and its floor.
       contract: - raw_os: each of these trips and names the field: a changes.TaskRecord.Files
                   element into os.ReadFile; project.Env.Files (NotConsumed) into os.Stat;
                   filepath.Join(root, f) into os.Open; os.Lstat, which was absent from the old
                   osVerbs; a field copied into a struct in another function, then into os.Remove
                 - contained_ok: a Consumed field → Contain → pathfence.ReadFile is clean, and a
                   field that is only printed is clean. phase.Task.Files → Contain →
                   pathfence.ReadFile trips at the consumer's line
                 - a registry entry that does not resolve to a types.Var fails with "unresolved
                   declaration" instead of contributing zero reads
                 - the live tree has zero findings, t-8's new fields included. Declared-field reads
                   meet a floor, and the floor passes at its minimum and fails at min−1. A live hit is
                   fixed with a Contained, never by redeclaring the field NotConsumed

  t-13 Pin the pre-phase stryker branch as must-trip  [risk+verification]  (depends t-9)
       files:    internal/cmd/taint_stryker_test.go (new),
                 internal/cmd/testdata/taint/stryker_prephase.go.txt (new),
                 internal/mutation/toolfence_residual_test.go
       covers:   c-3
       desc:     6f27eaa^'s branch copied verbatim with minimal stubs: the MultiWriter tee, headBuffer,
                 quote() into a strings.Builder, the reportless errors.New(msg), and
                 checkInstrumented's Errorf. It adds the spec's two-line `quoted.String()` →
                 fmt.Errorf shape. The residual guard's "WHAT THIS DOES NOT CATCH" note now names
                 this guard.
       contract: - there are findings at the reportless `return nil, errors.New(msg)`, at
                   checkInstrumented's `fmt.Errorf("%s\n%s", msg, head.quote(...))`, and at the
                   paraphrase's Errorf. Each names the `cmd.Stdout = sink` origin
                 - the tee, quote() and escape lines equal a const copied from `git show
                   6f27eaa^:internal/mutation/stryker.go` (115-118, 142, 150, 201-222, 315). Editing
                   the fixture toward a caught shape fails the pin, which needs no .git on the
                   remote runner
                 - two twins yield zero: errors.New(msg) swapped for printHead(os.Stderr, …) plus a
                   fixed-prose error, and &quoted swapped for os.Stderr

  t-14 Route gh failure output to stderr  [risk+verification]  (depends t-9, t-10, t-11)
       files:    internal/ship/{open,headpr,basepr,merged,comment}.go, internal/ship/comment_test.go,
                 internal/ship/gh_failure_test.go (new), internal/cmd/taint_ship_test.go (new)
       covers:   c-1
       desc:     Each of the five `fmt.Errorf("gh …: %w\n%s", err, string(out))` sites prints the
                 output to os.Stderr and returns the verb and exit status only. The PR URL and the
                 decoded JSON get a marker at the conversion. It waits on t-11 because open.go
                 carries a surgery anchor.
       contract: - with ghCommand stubbed to print CANARY-GH and exit 1, all five entry points return
                   an error holding the subcommand and "exit status 1" but not CANARY-GH, and stderr
                   holds CANARY-GH
                 - comment_test.go:230's "want gh's own output included" assertion MOVES to captured
                   stderr; it is not deleted
                 - the scoped gate over internal/ship reports zero, and removing any added marker
                   reports that exact line. t-11's open.go anchor still matches

  t-15 Replace gitCombined with failure-to-stderr gitRun  [verification]  (depends t-9, t-10)
       files:    internal/cmd/{ship_recover,cleantree,phase,repair,redproof_lifecycle,phase_lifecycle,
                 originpush,switchbranch,phase_backfill,basebranch,repair_files,redproof_replay,
                 milestone,ship}.go, internal/cmd/subprocargs_audit_test.go, internal/cmd/ship_test.go,
                 internal/cmd/switchbranch_test.go, internal/cmd/taint_gitplumbing_test.go (new)
       covers:   c-1
       desc:     Add gitRun(repoDir, args...) error to ship_recover.go, which is already in
                 cmdForbiddenBaseline. It keeps gitCombined's exec-exempt marker and prints the
                 output to stderr on failure. Migrate every output-into-error site: 36 `%w\n%s` across these
                 files (10 in milestone.go), plus redproof_replay's `%v: %s`. Delete gitCombined and swap it for gitRun in
                 gitCallFuncs. The change is compile-atomic, hence 18 files in one task.
       contract: - with a PATH git stub printing CANARY-GIT and exiting 1, `dross ship recover`'s
                   fetch failure returns an error without CANARY-GIT, and stderr carries it
                 - the subprocargs audit's new git-helper call-site floor holds; leaving gitRun out of
                   gitCallFuncs drops about 40 sites and fails it. TestAuditFlagsItsOwnSnippets'
                   snippet, retargeted to gitRun, still flags
                 - TestEverySpawnSiteGatedOrExempt and the os/exec import ratchet stay green
                 - the scoped gate over the 14 migrated files reports zero findings

  t-16 Burn down codex and scanner taint  [risk+verification]  (depends t-9, t-10)
       files:    internal/codex/git.go, internal/codex/ast_grep.go, internal/security/run.go,
                 internal/quality/run.go, internal/techdebt/run.go, internal/cmd/taint_scanners_test.go (new)
       covers:   c-1
       desc:     Each conversion of git log, ast-grep or scanner output into data gets a marker at
                 the narrowest conversion, with prose naming what the values are and where they go.
                 Any error that carries tool output sends that output to stderr instead.
       contract: - the scoped gate over codex, security, quality and techdebt reports zero findings
                   and no orphan marker
                 - removing the rev-parse marker in internal/security/run.go reports that line and
                   names the git spawn as its origin
                 - every added marker passes t-2's grammar and clears at least one tainted value

  t-17 Burn down tool streams in mutation and remote  [risk+verification]  (depends t-9, t-10, t-11)
       files:    internal/mutation/{gremlins,stryker_net,construct}.go, internal/remote/{hold,remote}.go,
                 internal/compilefence/compilefence.go, internal/cmd/taint_toolstreams_test.go (new)
       covers:   c-1
       desc:     Tool streams end at the terminal or the recorder, never at a marker. That covers
                 construct.go's stdout buffer, hold.go's stderr sink and remote's transport output.
                 compilefence's CombinedOutput, returned to test callers, gets a marker whose prose
                 says it is test-only.
       contract: - with holdCommandFn stubbed to write CANARY-SSH and exit 255, the error omits
                   CANARY-SSH, stderr carries it, and errors.Is(err, remote.ErrTransport) still
                   holds
                 - a //dross:taint-cleared in mutation's stryker.go, gremlins.go or stryker_net.go is
                   itself a finding (Disagreement 5)
                 - the scoped gate over internal/mutation, remote and compilefence reports zero
                   findings, and t-11's gremlins.go anchor still matches

  t-18 Burn down user-command streams in cmd  [risk+verification]  (depends t-9, t-10, t-11)
       files:    internal/cmd/{lane_install,run,test,update,verify,survivor_drain,techdebt}.go,
                 internal/cmd/taint_usercmds_test.go (new)
       covers:   c-1
       desc:     lane_install stops returning its install output; the output goes to stderr, and the
                 pin moves with it. The test/run/verify streams reach Cmd.Stdout through an
                 OutOrStdout-derived writer, with no marker. The `git ls-files` lists in
                 survivor_drain and techdebt get conversion markers.
       contract: - with a stub installer printing CANARY-LANE and exiting 2, CANARY-LANE is in
                   captured stderr and in no returned value
                 - the scoped gate over these seven files reports zero findings, and there is no
                   marker on the test.go, run.go or verify.go stream sites
                 - t-11's run.go and verify.go anchors still match

Wave 6
  t-19 Mark or fix git text readers in cmd  [risk+verification]  (depends t-10, t-15)
       files:    internal/cmd/{ship_recover,cleantree,init,milestone_stale,pause,phase,statusline,
                 worktree_files}.go, internal/cmd/taint_gitplumbing_test.go
       covers:   c-1
       desc:     SHAs, branch names, porcelain paths and `git show` bytes from gitTrim get a marker
                 whose prose is true for every caller. Values that are only compared or counted
                 need nothing. A strconv error wrapped with %w becomes fixed prose, and
                 milestone_stale's &diff buffer goes to stderr. Runs after t-15, which shares three
                 of these files and the gate file.
       contract: - the gate in taint_gitplumbing_test.go, extended to these eight files, reports zero
                   findings and no orphan marker
                 - a fixture copy of pre-fix milestone_stale, where the diff buffer reaches a
                   returned error, trips and is kept as a regression case
                 - every added marker passes t-2's grammar and clears at least one tainted value

  t-20 Findings name escape, origin and remedy  [risk+verification]  (depends t-9, t-12)
       files:    internal/cmd/srcfinding_test.go (new),
                 internal/cmd/testdata/{taint,pathtaint}/one_violation.go.txt (new)
       covers:   c-6
       desc:     One single-violation fixture per new scan; the taint spawn sits two packages away
                 from the escape. Lines are found by anchor text. Escape, origin and remedy are
                 asserted as three separate facts, so a message that loses one names which.
       contract: - the taint finding holds the escape's file:line, the spawn's file:line in the
                   other package, and the remedy ("print it to os.Stderr … or mark the conversion
                   with //dross:taint-cleared <reason>")
                 - the path finding holds the escape's file:line, "changes.TaskRecord.Files" and
                   "construct a pathfence.Contained via pathfence.Contain"
                 - a value merged from two spawns through a phi names both origins; a finding that
                   keeps only the first origin fails

Wave 7
  t-21 Land live taint gate, floors, budget, docs  [risk+verification]  (depends t-7, t-11, t-12, t-14..t-19)
       files:    internal/cmd/taint_audit_test.go (new), ARCHITECTURE.md
       covers:   c-1, c-7, c-8
       desc:     TestNoSpawnOutputEscapes runs the exec policy over t-4's full set. It lands after
                 every burn-down, so it is never red mid-phase. Floors sit about 25% under the
                 measured counts.
                 ARCHITECTURE.md: the exec-consent entry covers VTA reach and unresolved dispatch;
                 the path-containment entry drops "8 of 36 packages, an 11-tag vocabulary, 4 schema
                 dirs … tracked debt"; a new exec-output taint entry is added.
       contract: - zero live findings. Sources meet the source floor across at least the file floor;
                   the floor passes at its minimum and fails at min−1, and a walk restricted to cmd/
                   falls under it
                 - the repo-wide census finds no orphan //dross:taint-cleared marker
                 - `dross architecture check` is clean, the new entry's landmark resolves, and the
                   8-of-36 / 11-tag / 4-dir figures are gone
                 - c-8 evidence at verify: the internal/cmd total in the phase PR's timing table is
                   at most main's latest run + 15s. The local -race elapsed time of the scan tests is
                   recorded beside it, and the load-once tests are green on that run
```

## Coverage

| Criterion | Merged tasks |
|---|---|
| c-1 | t-2, t-5, t-10, t-14, t-15, t-16, t-17, t-18, t-19, t-21 |
| c-2 | t-9 |
| c-3 | t-13 |
| c-4 | t-12 |
| c-5 | t-4. t-1 supplies the single `./...` load; t-7, t-8, t-12 and t-21 walk t-4's set |
| c-6 | t-20 |
| c-7 | t-3 (harness), t-4, t-5, t-12, t-21. The migrated floors are kept in t-7, t-8 and t-11 |
| c-8 | t-1 (load once, module-only bodies, early -race measurement), t-11 (no reload), t-21 (CI table read) |
| c-9 | t-7, t-8 |
| c-10 | t-6, t-11 |

## Disagreements

**1. When the live exec-taint gate lands and how burn-downs are gated.**
- risk: the global gate goes in last (t-18), and each burn-down asserts zero findings in its own scoped test file.
- verification: the gate goes in with the engine, carrying a transitional per-file pending set whose findings are only t.Logf'd. The final task deletes the set, and an AST check stops it returning.
- mvp: one ~30-file task drains the tree and turns the gate on.
- **Default: risk.** It never puts a hand-kept suppression list in the tree, and every burn-down has a failing gate. Verification's burn-downs only "log zero suppressed", so they fail nothing.
- Cost: six scoped gate files that duplicate the global gate afterwards. Each re-runs the engine against c-8's 15s budget unless the analysis is memoized.

**2. How burn-downs are grouped, and who owns the `gitCombined` callers.**
- risk groups by area (5 tasks), verification by fix pattern (4 tasks, including a gitCombined → gitRun refactor), and mvp uses a single task.
- On the tree, `gitCombined` has 39 call sites in 14 cmd files. risk and mvp don't cover 11 of those files, which hold 25 `%w\n%s` output-into-error sites (10 in milestone.go). Only verification owns them.
- **Default:** risk's area split, plus verification's gitRun task (t-15) and its discovery step (t-9 records live findings; any file without an owner gets `dross task add`). This adds a wave, because t-19 shares three files with t-15.
- Without it, the last-landing gate goes red on files no task owns.

**3. How fixtures enter the program.**
- risk and verification type-check `.go.txt` into a separate ssa.Program through an importer backed by the one load.
- mvp overlays `.go.txt` as `.go` into the single packages.Load (Config.Overlay).
- **Default: separate program.** Overlaid fixture types enter the live CHA graph. Spawning fixture implementers would then become candidates at live VTA-empty dispatch sites and invent "unresolved" exec-consent findings.
- The overlay would also force moving the existing pathfence_scan fixtures, and it merges the 09-07 fixture into the live walk.

**4. The c-10 reach model and the four repoExecGraph rewrite tests.**
- risk: all reach edges come from VTA, joined by token.Pos, and the rewrites become surgery on a graph copy with anchor tripwires.
- verification: all reach edges come from VTA, keyed by function. The text rewrites stay for AST facts, with edges from the unrewritten program, and a rewrite that changes declared funcs is refused.
- mvp: the AST graph stays, and only interface dispatch goes through a (caller, method) VTA resolver.
- **Default: risk.** Under verification's model, a future rewrite that deletes a call that matters for reach would assert against stale SSA edges and pass: a false green. mvp leaves static reach on the AST, which is a weak reading of "attributes reach through the shared SSA call graph".
- risk's cost: graph-copy machinery plus the -shuffle and edge-count contracts.

**5. Whether a marker may clear tool failure output.**
- mvp lets any live finding be cleared by a marker, error escapes included, after a check against telemetry err_detail.
- risk bans markers outright in stryker.go, gremlins.go and stryker_net.go.
- verification says failure output is fixed, never marked, proven by CANARY tests. It explicitly rejects an automated marker ban, which would multiply markers about 35× on generic runners like gitTrim.
- **Default:** fix-not-mark with CANARY tests (risk and verification), keeping risk's three-file ban in t-17.
- Marking an error escape is laundering by annotation. risk's ban, though, is a file list, which is the shape v1.7 keeps retiring.

**6. The unwrap-ban mechanism.**
- risk and mvp keep the AST scan and key String()/Rel() on the receiver type pathfence.Contained.
- verification makes Contained accessor results sources in the path taint policy. That catches `s := c.String(); os.ReadFile(s)`, which today's ban passes. Its own message even recommends "assign the string to a variable before the call".
- **Default: type-keyed AST (t-7).** It meets c-9's letter and lands in wave 3 with no engine dependency.
- Under the default, the laundered two-line unwrap stays open. Verification's version closes it but moves c-9's unwrap half to wave 5.

**7. The c-9 closed world: schema derivation and where the ledger lives.**
- Schema set: risk takes structs that reach a TOML/JSON encode or decode call; mvp and verification take every tagged string field in any derived package.
- Ledger: risk uses a Go `pathfence.NotPaths()` with a Why per struct, which is shipped code. mvp uses `not_paths.txt`, generated once. verification uses `text_fields.txt`, hand-edited, with no -update flag.
- **Default:** tag-based set, testdata ledger, no -update flag.
- Encode reachability is a second flow analysis that can miss indirect encodes. NotPaths would put about 450 non-path rows into the binary. An -update flag lets a bulk regenerate classify a path field as text unseen.

**8. Path-scan semantics for NotConsumed fields and the os sink set.**
- risk and mvp: Contain clears any field, NotConsumed included. verification: a NotConsumed field never clears, so phase.Task.Files → Contain → pathfence.ReadFile is a finding.
- Sinks: risk takes any package-level os function. verification takes only os parameters named name, path, dir and the like, so os.Getenv(field) is clean.
- **Default:** verification on NotConsumed, risk on sinks. fields.go defines NotConsumed as "a declaration that nothing opens it — a declaration a test can falsify". Opening via the seam falsifies it silently under risk's rule.
- The stricter rule may turn live code red where the registry is currently lying.

**9. Layering the loader, fixture builder and parser.**
- risk bundles loader, fixture builder and WANT harness in t-1 (5 files, 3 layers), and extracts the parser inside the exec-consent migration.
- verification gives each its own task. mvp bundles the loader with scope.
- **Default: split.** The granularity rule requires it. A broken importer then fails in its own task instead of surfacing as scan misses, and the marker no longer waits on the VTA migration.
- Cost: one extra wave.

**10. The marker directive name and the orphan rule.**
- risk: `//dross:taint-cleared`. mvp: `//dross:taint-exempt`. verification: `//dross:output-exempt`. The spec leaves the name to plan time.
- risk and verification make a marker that clears nothing a finding; mvp doesn't.
- **Default:** `//dross:taint-cleared`, with the orphan finding. The name describes what happens at the conversion site.
- The name is permanent in source, so the user should pick it. Without the orphan rule, blanket-marking passes.

**11. c-3 fixture fidelity.**
- mvp carries only the spec's two-line paraphrase, with no pin, and explicitly leaves toolfence_residual_test.go untouched.
- risk carries the literal 6f27eaa^ lines as a const plus the paraphrase, and updates the residual "does not catch" note.
- verification pins with a sha256 plus a history diff when the blob is present.
- **Default: risk.** c-3 says "verbatim", and a paraphrase-only fixture can drift toward whatever the engine catches. The const also needs no .git on the remote runner. The note update is one comment in a file the task already concerns.

**12. c-6 fixture shape.**
- risk: one fixture per new scan (taint, path), plus a check that a phi merge names both origins.
- verification: one package holding one violation each of exec, path-field and unwrap.
- mvp: per-scan checks spread across the engine tasks.
- **Default: risk.** "The spawn site (or declared path field) it traces back to" fits only the two tracing scans. Treating the unwrap ban as "a scan" is verification's broader reading, and it adds an origin kind the criterion doesn't name.

**13. The unwrap_os count. Resolved on evidence, not provisional.**
- risk and verification both assert that unwrap_os keeps 5 findings after type-keying. mvp says its builderWrite row flips.
- The fixture's own comment marks builderWrite (`strings.Builder.String()` in os.WriteFile) as "must-trip on purpose" only because "ast alone cannot tell the two apart".
- Type-keying makes it must-not-trip, so the count is 4. t-7 takes mvp's contract. The other two drafts' contracts would fail on the first run, or be "fixed" by weakening the type key.
