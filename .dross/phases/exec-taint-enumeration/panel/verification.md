# exec-taint-enumeration: verification-lens draft

Lens: I wrote each criterion's test contract first and then derived the smallest task that can satisfy it. Where a criterion depends on shared machinery (the loader, the fixture type-checker, the taint engine), that machinery gets its own task. Its contract is the property the downstream contracts rely on, so a downstream failure points at one layer.

Constraints found in the tree that shape the contracts:
- `TestNoTestLost` (boundary_test.go) fails if any recorded `Test*` name disappears. Every existing exec-consent and pathfence test keeps its name. Bodies may be rewired, but names may not change.
- `TestMain` (hermetic_env_test.go) replaces HOME with an empty temp dir for the whole internal/cmd binary. Under that HOME, a `go list` resolves GOCACHE and GOMODCACHE to empty directories: every dependency gets cold-compiled and every module downloaded. That breaks c-8 before any scan runs, so the loader must pin the ambient caches.
- CI runs `go vet ./...` before `go test`. That leaves non-race export data in the ambient GOCACHE, which is what makes a `go list -export` load cheap, but only if the loader uses that cache.
- The internal/cmd `os/exec` import ratchet (`cmdForbiddenBaseline`) means no new non-test internal/cmd file may import os/exec.
- `gitCallFuncs` in subprocargs_audit_test.go is a name list. Renaming `gitCombined` without updating it silently drops about 40 git call sites from the argv audit.
- Existing fixtures are `.go.txt` files. New fixtures stay `.go.txt` so the exec-consent sweep, the subprocargs audit and `go list ./...` never count them as source.

```
Phase exec-taint-enumeration — 15 tasks across 7 waves

Wave 1
  t-1  Load module once into shared SSA program
       files:    go.mod, go.sum, internal/cmd/ssaload_test.go (new)
       covers:   c-5, c-8
       desc:     Add golang.org/x/tools (test-only import). Add a sync.Once loader: packages.Load("./...")
                 from the repo root, env pinned to the ambient GOCACHE/GOMODCACHE/GOPATH (`go env` run
                 with HOME=ambientHome), ssautil.Packages built, plus a lazily built, shared VTA callgraph
                 (CHA initial graph). It exposes the derived package set (dirs, CompiledGoFiles).
       contract: - TestSharedProgramLoadsOnce: two sharedProgram(t) calls return the identical *ssa.Program
                   and *callgraph.Graph pointers and the package-level load counter reads 1. Dropping the
                   sync.Once makes it 2.
                 - TestOnePackagesLoadCallSite: an AST walk of internal/cmd/*_test.go finds exactly one
                   packages.Load call. A second loader anywhere in the test binary fails, naming its file:line.
                 - TestSharedLoadUsesAmbientGoCaches: the loader's Config.Env GOCACHE and GOMODCACHE equal
                   `go env` resolved under ambientHome, and neither sits under TestMain's throwaway HOME.
                   A loader that just inherits os.Environ() fails here.
                 - TestSharedLoadIsClean: the load has zero packages.Errors or TypeErrors, and every loaded
                   package has Types, TypesInfo and Syntax. An ill-typed package (which would give partial SSA
                   bodies) fails, naming the package and position.
                 - TestDerivedWalkIsTheModule: the set of loaded package dirs equals an independent WalkDir
                   set (dirs holding non-test .go files, skipping testdata and _/.-prefixed dirs). The pattern
                   is the single literal "./...". A dir on disk but absent from the load fails, naming it.
                   The set includes internal/cmd, internal/ship, internal/mutation, cmd/dross, cmd/testsummary
                   and assets.
                 - TestXToolsStaysTestOnly: no non-test .go file in the derived set imports golang.org/x/tools/...,
                   and the transitive Imports closure of cmd/dross in the shared load has no x/tools package.
                   Moving the loader into a non-test file fails.
                 - `go mod tidy -diff` exits 0 (the CI tidy gate) with x/tools required.

  t-2  Share exec-exempt grammar with output-exempt marker
       files:    internal/cmd/execconsent_audit_test.go, internal/cmd/directive_marker_test.go (new)
       covers:   c-1
       desc:     Extract execExemptMarkers' parsing into directiveMarkers(fset, file, directive) and the
                 reason-floor check. Declare outputExemptMarker = "//dross:output-exempt" with the same
                 20-rune floor. Exec-consent calls the shared parser.
       contract: - TestDirectiveGrammarIsShared: every marker-grammar row of snippets.txt (reasonless, one-word
                   reason, glued, spaced comment, two lines above, below the call, tab before reason) is parsed
                   under both //dross:exec-exempt and //dross:output-exempt, and the two verdicts must be
                   identical (applies / reasonless / under floor). A second parser that diverges on any row
                   fails, naming the row and the directive.
                 - TestExecConsentMarkerGrammar and TestExecConsentFlagsItsOwnSnippets pass with their
                   assertions untouched.
                 - A well-formed //dross:output-exempt above an exec.Command does NOT exempt the spawn: the
                   exec-consent finding still fires. The two directives are not interchangeable.

Wave 2 (depends t-1)
  t-3  Type-check fixtures on the shared import graph
       files:    internal/cmd/ssafixture_test.go (new), internal/cmd/testdata/ssa_fixture/root.go.txt,
                 helper.go.txt, badimport.go.txt, typeerror.go.txt, want_mismatch.go.txt (all new)
       covers:   c-7
       desc:     typecheckFixture(t, files...) groups .go.txt files by package clause. Each package is
                 type-checked with an importer that resolves from the shared load's transitive graph, or from a
                 sibling fixture package (matched on the last element of the import path), then built into an
                 SSA program with VTA. Adds a want-comment harness: `// want "re"` marks each expected finding
                 line, and every fixture func must be named mustTrip* or mustNotTrip*.
       contract: - A two-package fixture that imports os/exec, github.com/spf13/cobra and its sibling via
                   example.com/fixture/helperpkg type-checks and yields SSA functions for both packages. An
                   importer that consulted only the stdlib fails on cobra.
                 - badimport (path absent from the shared graph) fails with the import path in the message and
                   is never skipped. typeerror fails naming file:line. SSA is never built from ill-typed source.
                 - Harness self-test on want_mismatch with a stub scanner: the report names BOTH the missing want
                   line and the unexpected finding line. A func named neither mustTrip* nor mustNotTrip* fails.
                   A mustTrip* func without a want fails. A mustNotTrip* func with a want fails.
                 - A fixture file named *_test.go.txt is left out of the program, mirroring the loader's
                   Tests:false.
                 - No fixture build calls packages.Load (TestOnePackagesLoadCallSite stays green).

  t-4  Fail on spawns or field reads outside walk
       files:    internal/cmd/sourcewalk_test.go (new)
       covers:   c-5, c-7
       desc:     An independent filesystem sweep of every non-test .go file in the module, ignoring go's package
                 rules, collects files with exec.Command/CommandContext calls or with a selector naming a
                 pathfence.Fields() field in a file that imports that field's package. Each one must be in the
                 shared load's CompiledGoFiles. outsideWalk(root, loaded) is a pure function.
       contract: - TestNoSpawnOrFieldReadOutsideTheWalk: zero on the live tree. The sweep's own floor is at least
                   20 spawn files (exec-consent measured 24), so a sweep that stops matching fails rather than
                   passing on nothing.
                 - On a temp tree, each of these is reported, naming the file and "spawn" or "field read":
                   (a) a //go:build ignore file with exec.Command; (b) a nested go.mod module with exec.Command;
                   (c) internal/newpkg missing from the supplied loaded set; (d) a build-tagged file reading
                   changes.TaskRecord.Files. The same tree with those files in the loaded set reports nothing.
                 - The live build-constrained pair internal/mutation/freespace_{unix,other}.go is swept.
                   Whichever file the current GOOS excludes fails the test the moment it gains an exec.Command.

Wave 3
  t-5  Close the serialized string-field world
       files:    internal/cmd/pathfence_fields_test.go, internal/cmd/pathfence_enum_test.go,
                 internal/pathfence/fields.go, internal/cmd/testdata/pathfence_fields/text_fields.txt (new),
                 internal/cmd/testdata/pathfence_fields/task_experiment.go.txt (new)
       depends:  t-1, t-3
       covers:   c-9, c-5, c-7
       desc:     Replace schemaDirs and pathShapedTags. Every toml- or json-tagged field of underlying type string
                 or []string, in any derived package (via go/types), must be declared in pathfence.Fields() (a
                 path) or in text_fields.txt (not a path), checked both ways. Triage today's undeclared path fields
                 (cmd.detachedRun.RunDir/Workdir, project RootRunDir/Workdir/RemoteWorkdir, survivor/quality/
                 security/findings File, …) into the registry with Why and Readers. The ".." literal ban walks the
                 derived set minus internal/pathfence.
       contract: - TestEveryPathShapedFieldIsDeclared: task_experiment is internal/phase's Task struct verbatim
                   plus the 2026-09-07 fields, strings tagged workdir, output_dir and spec. It yields exactly three
                   undeclared findings naming phase.Task.<Field> and its tag. The control (the same fixture
                   without the three fields) yields zero, which proves those three fields cause the findings.
                 - A tagged string field in a fixture package named outside the old four schema dirs (package
                   remote) is reported. Reverting to a hand-listed dir set fails here.
                 - A field of named type `type RelPath string` is enumerated (underlying-type rule). A bool tagged
                   `public` is not.
                 - A ledger row naming a nonexistent field fails as stale. A field in both the ledger and the
                   registry fails as double-declared. There is no -update flag: a failure prints the exact row to
                   paste, so every classification is a hand edit a reviewer sees.
                 - TestWalkerSelfTest keeps its name. Its want now includes the `replay`-tagged field, the one
                   existing assertion c-9 inverts on purpose.
                 - TestWalkerFindsANonEmptySet's floor rises to about 75% of the measured count.
                   TestNoSecondDotDotImplementation visits exactly derived−1 packages (all but internal/pathfence).
                 - TestRegistryIsWellFormed is green over every new entry.

  t-6  Build taint engine; prove c-2 channels
       files:    internal/cmd/taint_engine_test.go (new), internal/cmd/exec_taint_test.go (new),
                 internal/cmd/testdata/exec_taint/channel_return.go.txt, channel_field.go.txt,
                 channel_writer.go.txt, clearance.go.txt, marker.go.txt (all new)
       depends:  t-2, t-3
       covers:   c-1, c-2
       desc:     Forward SSA taint over module functions. Taint follows def-use, returns to call results,
                 field-keyed stores to loads, globals, closures, channel send/recv, and pointer/io.Writer
                 arguments back into the caller's concrete buffer. Calls with no body in the module (stdlib and
                 third-party) are ESCAPES unless their package is in a pure propagation set. Taint clears when a
                 value's type is non-text (bool or numeric), and at an output-exempt marker. Sources in this task
                 are the results of (*exec.Cmd).Output and CombinedOutput, keyed by *types.Func identity.
       contract: - channel_return: the spawn is wrapped in gitCmd() *exec.Cmd, the conversion in head() string,
                   and the consumer calls errors.New(head()). The finding is on the consumer line. The twin
                   fmt.Fprintln(os.Stderr, head()) gives none.
                 - channel_field: the output is stored to res.Log in one func, and another func calls
                   os.WriteFile(p, []byte(r.Log), 0o600). That gives a finding at the WriteFile call. Storing to
                   res.Other and reading res.Log gives none (field-keyed, not whole-struct).
                 - channel_writer: dump(w io.Writer, b []byte) calls fmt.Fprintf(w, …). A caller that passes
                   &strings.Builder and then calls errors.New(sb.String()) gets a finding at the errors.New call;
                   a caller that passes os.Stderr gets none. A writer param with no caller in the walk is an
                   escape (io.Writer is not terminal).
                 - Sources are keyed by type: a fixture-local `type Cmd struct{}` with an Output() method is not
                   a source; a struct embedding *exec.Cmd calling Output() through promotion is.
                 - clearance (type-based, no name list): strconv.Atoi's int, s == "x", strings.Contains,
                   strings.HasPrefix, len(s) and a fixture-local func isEmpty(s string) bool all clear.
                   strings.TrimSpace, Split, Fields and fmt.Sprintf stay tainted. The error from
                   strconv.Atoi(tainted) stays tainted (NumError embeds the input), so
                   `return fmt.Errorf("…: %w", err)` is a finding.
                 - marker: a //dross:output-exempt with at least 20 runes of prose above
                   `sha := strings.TrimSpace(string(out))` clears every downstream use. The same marker two lines
                   above gives a finding. A marker whose next line derives nothing tainted gives a finding
                   ("clears nothing — delete the marker"). //dross:exec-exempt in the same slot clears nothing.
                 - Default-deny: taint into os.Setenv, or into an unresolvable dynamic call, is a finding.
                 - Taint sent on a chan string and received in another goroutine reaches its consumer
                   (hold.go's shape).

  t-7  Attribute exec-consent reach through VTA callgraph
       files:    internal/cmd/execconsent_audit_test.go, internal/cmd/testdata/exec_consent/vta/never_passed.go.txt
                 (new), internal/cmd/testdata/exec_consent/vta/unresolved.go.txt (new)
       depends:  t-1, t-3
       covers:   c-10
       desc:     Delete execScope inference, methodsOn and implementers. Reach edges come from the VTA graph:
                 the shared program for the repo, the fixture program for fixtures. Anonymous funcs collapse into
                 their enclosing named node; a package-var literal is keyed by its var. The AST still owns sites,
                 markers, gating and the command tree. An unresolved dispatch whose CHA candidates reach a spawn is
                 a NoRemedy finding.
       contract: - Every existing exec-consent test passes with its assertions untouched and its name kept: the
                   snippets table, ungated.go.txt, every reach-fixture case (cross-package, var seam alias and
                   literal, unreachable, mixed, severed edge, extra hop), TestExecReachReportsAmbiguity (message
                   still says "across 2 packages", Pos still prefixed "interface "),
                   TestExecReachDoesNotDegenerate, TestMutationSpawnsAreGatedViaVerify,
                   TestDeletingVerifysGateFlagsEveryMutationSpawn (4 of 4), TestReachProofIsLoadBearing and
                   TestFuncLiteralSpawnIsAttributedThroughItsVar.
                 - never_passed: the command calls Drive(Quiet{}). Loud, a spawning implementer in another
                   package, is never passed in, so Loud's site is NOT in that command's reach. Twin: the command
                   calls Drive(Loud{}) and the site is attributed. Reverting to the CHA union fails the first half.
                 - unresolved: the command calls r.Go() on a Runner returned by a call with no body in the module,
                   and a spawning implementer exists. The result is a finding naming the interface method and the
                   call site, never silently attributed and never dropped.
                 - repoExecGraph takes edges from the shared program and never reloads.
                   TestOnePackagesLoadCallSite stays green. repoExecGraph refuses a rewrite that changes the file's
                   set of declared funcs, because edges come from the unrewritten program.

Wave 4
  t-8  Complete exec-output sources, terminals, live gate
       files:    internal/cmd/exec_taint_test.go, internal/cmd/testdata/exec_taint/writers.go.txt, pipes.go.txt,
                 exiterr.go.txt, terminal.go.txt (all new)
       depends:  t-6
       covers:   c-1, c-5, c-7
       desc:     Remaining sources: writers stored to (*exec.Cmd).Stdout/Stderr taint every concrete buffer that
                 flows in, including io.MultiWriter args and a custom type's Write(p) param. Also
                 StdoutPipe/StderrPipe readers and loads of (*exec.ExitError).Stderr. Terminal means os.Stdout,
                 os.Stderr, and cobra OutOrStdout/ErrOrStderr, resolved by flow. Building an error is always an
                 escape. Adds the live gate TestSpawnOutputEndsSafely over the derived set, with a source floor and
                 a transitional per-file pending set whose findings are t.Logf'd.
       contract: - writers: after `c.Stdout = io.MultiWriter(os.Stderr, head)`, head.buf read into fmt.Errorf is a
                   finding; the same with only os.Stderr gives none. A fixture `type sink` whose Write(p) appends p
                   to a field that reaches errors.New is a finding.
                 - pipes: sc.Text() from bufio.NewScanner(StdoutPipe) stored into a persisted field is a finding;
                   into fmt.Println it is not.
                 - exiterr: `_, err := c.Output(); return fmt.Errorf("git: %w", err)` gives none (ExitError renders
                   only "exit status N"). `errors.As(err, &ee); errors.New(string(ee.Stderr))` is a finding.
                 - terminal: fmt.Fprintln(cmd.OutOrStdout(), s) and cmd.ErrOrStderr() give none. A package
                   `var out io.Writer = os.Stderr` that non-test source never reassigns gives none; reassigning it
                   to a builder in non-test source is a finding. A cobra RunE returning errors.New(s) is a finding
                   (errors are never terminal).
                 - Binary-blind: two fixtures that differ only in "git" vs "cargo" produce identical findings, and
                   the finding struct has no binary field.
                 - Pure-set pinning: every package in the propagation set is the callee package of at least one
                   corpus line. An untested entry fails, naming the package.
                 - TestExecTaintFloorCatchesANarrowedWalk: the floor passes at its minimum and fails at min−1. The
                   live walk counts at least the floor, set to about 75% of the measured source count (measurement
                   recorded in the constant's comment).
                 - Corpus parity: the number of files in testdata/exec_taint equals the number exercised; there are
                   at least N mustTrip and N mustNotTrip funcs.
                 - Live gate: a finding in a file outside the pending set fails. The pending set is the exact
                   file list this task observes, recorded in the task notes. Any listed file not covered by
                   t-11..t-14 gets `dross task add` before wave 5.

  t-9  Taint declared path fields and Contained unwraps
       files:    internal/cmd/path_taint_test.go (new), internal/cmd/pathfence_enum_test.go,
                 internal/cmd/testdata/pathfence_scan/raw_field.go.txt, notconsumed_seam.go.txt,
                 unwrap_laundered.go.txt, not_contained.go.txt (all new; existing fixtures kept)
       depends:  t-5, t-6
       covers:   c-4, c-9, c-5, c-7
       desc:     A path policy on the engine. Sources: loads of pathfence.Fields() entries (resolved to types.Var)
                 and results of pathfence.Contained's String/Rel (keyed by the Contained type object). Sinks:
                 string args of os functions whose stdlib parameter is named name, path, dir, oldpath, newpath,
                 oldname or newname. A Consumed field clears at Contained construction; a NotConsumed field never
                 clears. TestNoUnwrappedPathReachesAnOSCall points at this policy; delete scannedPackages.
       contract: - raw_field: `os.Stat(filepath.Join(root, f))` over changes.TaskRecord.Files is a finding naming
                   the field. The same through pathfence.Contain and pathfence.Stat gives none.
                 - notconsumed_seam: phase.Task.Files → Contain → pathfence.ReadFile is a finding (a NotConsumed
                   field opened), reported at the consumer's pathfence.ReadFile line, not inside pathfence.
                 - The sink set is derived: os.Lstat, absent from the old osVerbs list, trips; os.Getenv(field) does
                   not.
                 - unwrap_laundered: `s := c.String(); os.ReadFile(s)` is a finding (the old AST ban passes it and
                   its message recommends it). not_contained: os.WriteFile(sb.String(), …) with a strings.Builder
                   gives none, and a fixture-local type named Contained with String() gives none (keyed by
                   identity, not name).
                 - The existing fixtures keep their verdicts: unwrap_os 5, rundir_revert 3, seam_ok 0.
                   TestUnwrapScanTripsOnItsFixtures and TestUnwrapScanSkipsTestFiles assertions are unchanged.
                 - Floors, each tested both ways: live declared-field reads ≥ floorF, Contained-accessor calls
                   ≥ floorC.
                 - The live path scan has zero findings over the derived set, including the fields t-5 newly
                   registered. There is no marker escape hatch.

Wave 5
  t-10 Pin the stryker shape and finding facts
       files:    internal/cmd/exec_taint_test.go, internal/cmd/path_taint_test.go,
                 internal/cmd/testdata/exec_taint/stryker_prephase.go.txt (new),
                 internal/cmd/testdata/exec_taint/stryker_prephase_stubs.go.txt (new),
                 internal/cmd/testdata/taint_findings/one_of_each.go.txt (new)
       depends:  t-8, t-9
       covers:   c-3, c-6
       desc:     The c-3 fixture holds headBuffer, quote, the no-report branch and checkInstrumented verbatim
                 from `git show 6f27eaa^:internal/mutation/stryker.go`, with stubs only for symbols defined
                 elsewhere in that package, plus today's printHead(&quoted, …) → fmt.Errorf(quoted.String())
                 laundering. The c-6 fixture puts one exec-taint, one path-field and one unwrap violation in a
                 single package.
       contract: - stryker_prephase trips at errors.New(msg) in the no-report branch and at checkInstrumented's
                   fmt.Errorf, and each finding traces to the fixture's exec.Command line. The post-phase twin
                   (quote printed to os.Stderr) gives none in that func.
                 - Laundering: printHead(&quoted, …) then fmt.Errorf(…, quoted.String()) is a finding at the Errorf
                   line. Replacing &quoted with os.Stderr gives none.
                 - The verbatim region's sha256 equals a constant recorded from the git show. When the blob is in
                   local history the test also diffs against it and fails on any difference.
                 - one_of_each: each finding has escape "<file>:<line>" (the line found by searching the fixture
                   text), its origin, and its remedy, asserted as three separate facts so a message that loses one
                   names which. Origins: "spawned at <file>:<line>" pointing at an exec.Command two helper calls
                   away; "changes.TaskRecord.Files"; the Contained accessor's line. Remedies: stderr / recorder /
                   //dross:output-exempt; pathfence.Contain; the pathfence seam.

  t-11 Route gh failure output to stderr
       files:    internal/ship/open.go, internal/ship/comment.go, internal/ship/basepr.go, internal/ship/headpr.go,
                 internal/ship/merged.go, internal/ship/comment_test.go, internal/ship/gh_failure_test.go (new)
       depends:  t-8
       covers:   c-1
       desc:     On failure, each gh CombinedOutput site prints the output to os.Stderr and returns
                 fmt.Errorf("gh <sub>: %w", err) without it (the cut_point precedent). JSON-decoded forge values
                 (PR URL/number/state) get an output-exempt marker at the decode line.
       contract: - With ghCommand stubbed to print CANARY-GH and exit 1, all five entry points return an error
                   containing "exit status 1" and the gh subcommand but not CANARY-GH, and captured stderr contains
                   CANARY-GH.
                 - comment_test.go:230's "pull request is closed" assertion moves from the error to stderr.
                 - The live gate logs zero suppressed findings for these five files.

  t-12 Replace gitCombined with failure-to-stderr gitRun
       files:    internal/cmd/ship_recover.go, cleantree.go, phase.go, repair.go, redproof_lifecycle.go,
                 phase_lifecycle.go, originpush.go, switchbranch.go, phase_backfill.go, basebranch.go,
                 repair_files.go, redproof_replay.go, milestone.go, ship.go, subprocargs_audit_test.go,
                 ship_test.go, switchbranch_test.go
       depends:  t-8
       covers:   c-1
       desc:     Add gitRun(repoDir, args...) error to ship_recover.go (already in cmdForbiddenBaseline). It keeps
                 gitCombined's exec-exempt marker and prints combined output to os.Stderr on failure. Migrate the
                 ~40 `fmt.Errorf("git …: %w\n%s", err, out)` sites, delete gitCombined, and swap it for gitRun in
                 gitCallFuncs.
       contract: - With a PATH git stub printing CANARY-GIT and exiting 1, `dross ship recover`'s fetch failure
                   returns an error without CANARY-GIT, and stderr carries it.
                 - The subprocargs audit visits the same number of git-helper call sites before and after (a floor
                   added to the audit). Leaving gitRun out of gitCallFuncs drops about 40 and fails.
                   TestAuditFlagsItsOwnSnippets' gitCombined snippet is retargeted to gitRun and still flags.
                 - TestEverySpawnSiteGatedOrExempt stays green (gitRun's spawn is marked), and the os/exec ratchet
                   stays green (no new cmd file imports os/exec).
                 - The live gate logs zero suppressed findings at any former gitCombined call site.

  t-13 Clear helper-package spawn output findings
       files:    internal/remote/hold.go, internal/remote/remote.go, internal/codex/git.go,
                 internal/codex/ast_grep.go, internal/mutation/construct.go, internal/techdebt/run.go,
                 internal/quality/run.go, internal/security/run.go, internal/compilefence/compilefence.go
       depends:  t-8
       covers:   c-1
       desc:     Fix or mark each pending finding in these packages. Failure output routed into an error is
                 FIXED (terminal plus a structured error): hold.go's stderr buffer, remote run failures. Text that
                 stays data (rev-parse --short SHAs, git log lines, ast-grep JSON, compilefence's output returned
                 to test callers) is MARKED at the narrowest conversion that knows what it is.
       contract: - hold.go: with holdCommandFn stubbed to write CANARY-SSH to stderr and exit 255, the returned
                   error omits CANARY-SSH and stderr carries it. The existing remote.ErrTransport classification
                   is unchanged (errors.Is still holds).
                 - Every marker added passes the t-2 grammar and clears at least one tainted value. The engine
                   enforces both, so a decorative marker fails.
                 - The live gate logs zero suppressed findings for these files.

Wave 6 (depends t-12)
  t-14 Mark or fix internal/cmd text readers
       files:    internal/cmd/ship_recover.go (gitTrim), internal/cmd/cleantree.go, internal/cmd/pause.go,
                 internal/cmd/worktree_files.go, internal/cmd/phase.go, internal/cmd/techdebt.go,
                 internal/cmd/init.go, internal/cmd/statusline.go, internal/cmd/milestone_stale.go,
                 internal/cmd/survivor_drain.go, internal/cmd/lane_install.go, internal/cmd/test.go
       covers:   c-1
       desc:     Resolve the remaining internal/cmd findings. strconv errors wrapped with %w become fixed prose.
                 Porcelain paths, SHAs, branch names and `git show` plan bytes get a marker at the conversion,
                 with prose that is true for every caller it clears. lane_install and test.go failure output goes
                 to the terminal.
       contract: - Every marker added passes the grammar and clears at least one value (engine-enforced).
                 - lane_install: an install step printing CANARY-LANE and exiting 1 returns an error without
                   CANARY-LANE, and stderr carries it.
                 - `TestSpawnOutputEndsSafely -v` logs zero suppressed findings in internal/cmd.

Wave 7 (depends t-10, t-11, t-13, t-14)
  t-15 Make exec gate strict; document; read budget
       files:    internal/cmd/exec_taint_test.go, ARCHITECTURE.md
       covers:   c-1, c-8
       desc:     Delete the pending set so TestSpawnOutputEndsSafely is strict over the whole derived set. Update
                 the exec-consent entry (VTA reach, unresolved-dispatch findings) and the path-containment entry
                 (drop "hand-maintained scope list … tracked debt"), and add the exec-output taint entry.
       contract: - TestSpawnOutputEndsSafely: zero findings with no pending set. An AST check asserts
                   exec_taint_test.go declares no pending/skip map, so the ratchet cannot quietly return.
                 - `dross architecture check` is clean. The pathfence paragraph no longer claims "8 of 36
                   packages, an 11-tag vocabulary, 4 schema dirs", and the new entry's landmark resolves.
                 - c-8 evidence at verify: on the phase PR's CI step summary, the internal/cmd package total minus
                   the base run's total is ≤ 15s, and the Seconds of the top-level tests that call sharedProgram
                   sum to ≤ 15s. TestSharedProgramLoadsOnce and TestOnePackagesLoadCallSite are green on that run.
```

## Coverage

| Criterion | Tasks | Where the proof lives |
|---|---|---|
| c-1 | t-2, t-6, t-8, t-11, t-12, t-13, t-14, t-15 | Engine and sources (t-6, t-8). Live tree driven to zero (t-11..t-14) and made strict (t-15). |
| c-2 | t-6 | One fixture per channel: channel_return, channel_field, channel_writer, each with a must-not-trip twin. |
| c-3 | t-10 | stryker_prephase (verbatim, hash-pinned) plus the printHead(&quoted) laundering fixture. |
| c-4 | t-9 | raw_field (Consumed raw → os), notconsumed_seam (NotConsumed through the seam). |
| c-5 | t-1, t-4, t-5, t-8, t-9 | Derived "./..." walk equals the dir set (t-1). Outside-walk sweep (t-4). All scans use the derived set (t-5, t-8, t-9). |
| c-6 | t-10 | one_of_each: escape file:line, origin and remedy asserted separately for every scan. |
| c-7 | t-3, t-4, t-5, t-8, t-9 | Floors tested both ways for each scan; written answers via want/mustTrip/mustNotTrip; corpus parity. |
| c-8 | t-1, t-15 | Structural once-check and counter (t-1); CI timing-table reading (t-15). |
| c-9 | t-5, t-9 | Derived dirs, closed-world ledger and the 2026-09-07 fixture (t-5). Contained-keyed unwrap ban and deletion of scannedPackages (t-9). |
| c-10 | t-7 | All existing calibration tests unchanged, plus never_passed and unresolved. |

10/10 criteria covered.

## Judgment calls

- **Fixture type-checking:** type-checked with go/types against the shared load's import graph. I rejected analysistest because it runs a packages.Load per test (breaks c-8's once per binary) and needs a GOPATH layout.
- **c-10 split of work:** SSA supplies only reach edges; the AST keeps sites, markers, gating and the command tree. I rejected a full SSA rewrite of the audit, because the calibration verdicts and the rewrite tests are AST-shaped, and rewrites would otherwise force reloads.
- **Error of Output():** only loads of (*exec.ExitError).Stderr are sources; the error value is clean because it renders "exit status N". I rejected tainting every spawn error, which would flag every `%w` of a git error for text the error does not contain.
- **Errors from pure calls stay tainted:** strconv's NumError embeds its input. clearance_model clears the int, not the sibling error. This adds a few live findings and each one is a real leak.
- **Calls with no body in the module:** these are escapes by default, with a pure propagation set per package, and every package in that set must be pinned by a corpus line. I rejected building stdlib bodies into SSA: fmt's buffer pool collapses precision, and the extra load cost counts against c-8. I rejected treating unknown calls as pass-through because it fails open.
- **Closed-world string-field ledger:** text_fields.txt, about 400 rows, checked both ways, no -update flag. I rejected a morpheme or tag heuristic: c-9 requires an *unlisted* tag word to fail, which no word list can satisfy.
- **Marker name:** `//dross:output-exempt`, a sibling of exec-exempt, with the same 20-rune floor. A marker that clears nothing is itself a finding, mirroring exec-consent's rule that a marker on a gated-only site is refused.
- **No automated ban on markers in generic runners like gitTrim:** I rejected the ban because exec-consent already accepts a helper-level exec-exempt on gitTrim, and a ban would multiply markers roughly 35×. Failure output is instead FIXED, never marked, and CANARY behaviour tests prove it.
- **Transitional pending set:** the per-file pending set in the live exec gate is deleted in t-15, and an AST check stops it returning. I rejected landing the live gate last, because the remediation tasks would then have no automated gate.
- **Ambient go caches pinned from ambientHome:** this is mandatory. Without it TestMain's throwaway HOME cold-compiles every dependency and downloads modules, and c-8 fails before any scan runs.
- **c-3 fidelity:** a hash pin plus a history diff when the blob is present. I rejected reading history only at test time, which fails on a remote runner tree without .git.
- **c-8 measurement:** evidence comes from the PR timing table plus the structural once-checks. I rejected an in-test wall-clock assertion because it flakes under -race on shared runners.
- **Remediation grouped by fix pattern:** t-12 and t-14 exceed the 5-file guideline. gitCombined → gitRun is a compile-atomic signature change across 14 files, and splitting it would leave two runners live. t-14 is mostly one-line markers with one shared contract.
- **Path scan has no marker escape hatch:** the only remedy is pathfence.Contain. A hatch would reopen exactly what c-4 closes.
- **Path-scan sink set:** derived from the stdlib os parameter names (name/path/dir/…). I rejected extending the osVerbs list, which is another hand list.
- **Unresolved dispatch:** reported only when a CHA candidate reaches a spawn. I rejected reporting every unresolved invoke, which is noise with no verdict at stake.
- **c-6 fixture:** a single fixture with one violation per scan (exec, path-field, unwrap), which is the literal reading of "a fixture that introduces one violation of each scan".
- **TestWalkerSelfTest:** the only existing assertion changed on purpose; its `replay` field is now enumerated, as c-9 requires. Every other existing test keeps its assertions and name (TestNoTestLost).
