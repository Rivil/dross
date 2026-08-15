package mutation

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// --- fixtures ---

func helicon(workdir string) *remote.Target {
	return &remote.Target{Host: "helicon", Workdir: workdir}
}

// recordRemote swaps the launcher's single process seam for one that records
// every argv, in order, and runs nothing.
//
// One seam for the push, the rm, the tool run and the fetch alike is what makes
// the ORDER assertable — and the order is most of what this launcher exists to
// hold. onCmd runs after the argv is recorded, so a fake fetch can materialize
// the file a real rsync would have delivered.
func recordRemote(t *testing.T, onCmd func(argv []string)) *[][]string {
	t.Helper()
	var rec [][]string
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		// The piped script is recorded as a trailing element, so one [][]string
		// still holds everything that crossed to the remote — argv AND stdin.
		// Nothing reaches the host through a channel the recorder cannot see.
		cp := append(append([]string(nil), argv...), stdin)
		rec = append(rec, cp)
		if onCmd != nil {
			onCmd(cp)
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { launcherCommand = orig })
	return &rec
}

// failLocalSpawns makes every LOCAL adapter seam fail the test.
//
// This is c-1's "the local machine spawns no compile or test process" as an
// assertion rather than a claim: a remote run that quietly fell back to the
// local toolchain would produce a perfectly good-looking report measured on the
// wrong machine, and nothing else in the run would say so.
func failLocalSpawns(t *testing.T) {
	t.Helper()
	og, os_, on := gremlinsBuildCmd, strykerBuildCmd, strykerNetBuildCmd
	gremlinsBuildCmd = func(_ *Gremlins, a []string) *exec.Cmd {
		t.Fatalf("a remote run spawned the LOCAL gremlins: %v", a)
		return nil
	}
	strykerBuildCmd = func(_ *Stryker, a []string) *exec.Cmd {
		t.Fatalf("a remote run spawned the LOCAL stryker: %v", a)
		return nil
	}
	strykerNetBuildCmd = func(_ *StrykerNet, a []string) *exec.Cmd {
		t.Fatalf("a remote run spawned the LOCAL stryker.net: %v", a)
		return nil
	}
	t.Cleanup(func() { gremlinsBuildCmd, strykerBuildCmd, strykerNetBuildCmd = og, os_, on })
}

// isFetch distinguishes the per-report pull from the one-shot push: FetchArgs
// emits `rsync -a <host>:<path> <local>`, SyncArgs emits `rsync -az …`.
func isFetch(argv []string) bool {
	return argv[0] == "rsync" && argv[1] == "-a"
}

func isPush(argv []string) bool { return argv[0] == "rsync" && argv[1] == "-az" }

// remoteScript returns the script piped to an ssh command's stdin, or "".
// It is the recorder's trailing element — see recordRemote.
func remoteScript(argv []string) string {
	if argv[0] != "ssh" || len(argv) < 3 {
		return ""
	}
	return argv[len(argv)-1]
}

// kinds renders a recorded sequence as short tags, so an order mismatch reports
// what actually happened rather than four screens of argv.
func kinds(rec [][]string) []string {
	out := make([]string, 0, len(rec))
	for _, argv := range rec {
		switch {
		case isPush(argv):
			out = append(out, "push")
		case isFetch(argv):
			out = append(out, "fetch")
		case strings.Contains(remoteScript(argv), "'rm' '-rf'"):
			out = append(out, "rm")
		case argv[0] == "ssh":
			out = append(out, "run")
		default:
			out = append(out, "?"+argv[0])
		}
	}
	return out
}

const strykerPayload = `{"schemaVersion":"1.0","files":{"src/a.ts":{"language":"typescript","source":"x",
"mutants":[{"id":"1","mutatorName":"BooleanLiteral","replacement":"false","status":"Killed",
"location":{"start":{"line":1,"column":1},"end":{"line":1,"column":2}}}]}}}`

// --- c-1 / c-6: the gremlins leg, end to end ---

// TestGremlinsRemoteRunOrderAndArgv is the phase's central claim executed on one
// adapter: with a remote configured the local machine spawns nothing but ssh and
// rsync, the tree is pushed once before anything else, and each package is
// rm'd-run-fetched in that order.
//
// The merged report is compared against the LOCAL expectation from
// TestGremlinsRunRePrefixesPackagePaths — same per-file keys, same score. A
// remote run that produced a differently-shaped report would be a second code
// path for verify's diff scoping to disagree with.
func TestGremlinsRemoteRunOrderAndArgv(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "gremlins_pkg_report.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	root := t.TempDir()
	failLocalSpawns(t)
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			if werr := os.WriteFile(argv[3], payload, 0o644); werr != nil {
				t.Fatalf("fake fetch write: %v", werr)
			}
		}
	})

	g := &Gremlins{ProjectRoot: root, Remote: helicon("/srv/dross")}
	report, err := g.Run([]string{"internal/argfence/argfence.go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, argv := range *rec {
		if argv[0] != "ssh" && argv[0] != "rsync" {
			t.Fatalf("a remote run spawned %q locally — c-1 requires argv[0] in {ssh, rsync}: %v", argv[0], argv)
		}
	}
	if got, want := kinds(*rec), []string{"push", "rm", "run", "fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded order = %v, want %v\nfull:\n%v", got, want, *rec)
	}

	// The rsync source is the adapter's ProjectRoot as a VALUE, not merely
	// something with a trailing slash: a push from any other local directory
	// would measure a tree the user is not looking at.
	// Counted back from the end, past the (empty) stdin script the recorder
	// appends: rsync's source and destination are always its last two real
	// arguments, whereas their offset from the front moves whenever a flag is
	// added or removed — which is exactly what dropping --exclude=.git did.
	push := (*rec)[0]
	src, dst := push[len(push)-3], push[len(push)-2]
	if src != root+"/" {
		t.Errorf("rsync source = %q, want %q (the adapter's ProjectRoot)", src, root+"/")
	}
	if dst != "helicon:/srv/dross" {
		t.Errorf("rsync destination = %q, want helicon:/srv/dross", dst)
	}

	// The tool argv reached the remote as the ssh payload, not as a local argv[0].
	if script := remoteScript((*rec)[2]); !strings.Contains(script, "'gremlins' 'unleash'") ||
		!strings.HasPrefix(script, "cd '/srv/dross' && ") {
		t.Errorf("the gremlins invocation did not reach the remote as a cd'd script: %q", script)
	}

	// The report resolves at the path THIS adapter reads it from.
	reportAbs := GremlinsReportPath(root, "./internal/argfence")
	if _, serr := os.Stat(reportAbs); serr != nil {
		t.Fatalf("the fetched report is not at GremlinsReportPath: %v", serr)
	}
	want := map[string]FileStat{
		"internal/argfence/policy.go":   {Survived: 1, NotCovered: 1},
		"internal/argfence/argfence.go": {Killed: 2},
	}
	if !reflect.DeepEqual(report.Files, want) {
		t.Errorf("remote merge differs from the local expectation:\n got %+v\nwant %+v", report.Files, want)
	}
}

// TestGremlinsRemotePushesOnceAndFetchesPerPackage pins the two ordering
// properties a single-package run cannot see.
//
// The push is one-shot: a second rsync inside the loop would overwrite the
// reports already fetched for earlier packages. And each package's fetch is
// issued before the NEXT package runs, per the locked report_fetch decision —
// a batched end-of-run fetch would make "no report" stop meaning "nothing was
// learned about this package".
func TestGremlinsRemotePushesOnceAndFetchesPerPackage(t *testing.T) {
	root := t.TempDir()
	failLocalSpawns(t)
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			if werr := os.WriteFile(argv[3], []byte(fixtureGremlinsBareBasename), 0o644); werr != nil {
				t.Fatalf("fake fetch write: %v", werr)
			}
		}
	})

	g := &Gremlins{ProjectRoot: root, Remote: helicon("/srv/dross")}
	if _, err := g.Run([]string{"a/x.go", "b/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"push", "rm", "run", "fetch", "rm", "run", "fetch"}
	if got := kinds(*rec); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded order = %v, want %v", got, want)
	}
	pushes := 0
	for _, argv := range *rec {
		if isPush(argv) {
			pushes++
		}
	}
	if pushes != 1 {
		t.Errorf("the tree was pushed %d times, want exactly 1", pushes)
	}
}

// --- c-1 / c-6: the other two adapters ---

// TestStrykerRemoteRunCdsIntoTheJoinedWorkdir is the monorepo case, and it is
// the one a local-run mental model gets wrong: cmd.Dir on the LOCAL ssh process
// says nothing about the remote cwd. If Workdir does not reach the remote as
// part of the command, stryker runs at the remote repo root and measures the
// wrong package — with no error anywhere.
func TestStrykerRemoteRunCdsIntoTheJoinedWorkdir(t *testing.T) {
	root := t.TempDir()
	failLocalSpawns(t)
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			if werr := os.WriteFile(argv[3], []byte(strykerPayload), 0o644); werr != nil {
				t.Fatalf("fake fetch write: %v", werr)
			}
		}
	})

	s := &Stryker{ProjectRoot: root, Workdir: "web", PackageManager: "pnpm", Remote: helicon("/srv/dross")}
	report, err := s.Run([]string{"web/src/a.ts"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The middle "run" is the dependency restore, which a remote Node run needs
	// before stryker can resolve anything — see restore_test.go.
	if got, want := kinds(*rec), []string{"push", "rm", "run", "run", "fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded order = %v, want %v\nfull:\n%v", got, want, *rec)
	}
	for _, i := range []int{1, 2, 3} {
		if script := remoteScript((*rec)[i]); !strings.HasPrefix(script, "cd '/srv/dross/web' && ") {
			t.Errorf("command %d does not cd into the joined workdir: %q", i, script)
		}
	}
	if script := remoteScript((*rec)[1]); !strings.Contains(script, "'rm' '-rf' 'reports/mutation/mutation.json'") {
		t.Errorf("the rm does not clear stryker's remote report: %q", script)
	}
	// The fetch lands at the path Run reads from — reportPath(), which carries
	// the workdir. A fetch that only got the gremlins path shape right would
	// leave this adapter reading nothing.
	if _, serr := os.Stat(filepath.Join(root, "web", "reports", "mutation", "mutation.json")); serr != nil {
		t.Fatalf("the fetched report is not at reportPath(): %v", serr)
	}
	if report.Killed != 1 {
		t.Errorf("the fetched report did not parse: %+v", report)
	}
}

// TestStrykerNetRemoteRunClearsTheWholeOutputTree covers the adapter whose
// staleness mode is the nastiest.
//
// findReport WALKS the output dir and returns the most recently modified
// mutation-report.json. So a run that produces nothing does not fail — it scores
// the previous run's report, which for a leg that never ran reads as clean. The
// rm is what makes the run error instead.
func TestStrykerNetRemoteRunClearsTheWholeOutputTree(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "StrykerOutput", "reports", "mutation-report.json")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(strykerPayload), 0o644); err != nil {
		t.Fatal(err)
	}

	failLocalSpawns(t)
	rec := recordRemote(t, nil) // the remote produces no new report

	s := &StrykerNet{ProjectRoot: root, Remote: helicon("/srv/dross")}
	_, err := s.Run([]string{"src/A.cs"})
	if err == nil {
		t.Fatal("a remote run that produced no report returned no error — findReport scored a stale tree as clean")
	}
	if _, serr := os.Stat(stale); !os.IsNotExist(serr) {
		t.Errorf("the previous run's report survived, stat err = %v", serr)
	}
	// The middle "run" is `dotnet restore` — see restore_test.go.
	if got, want := kinds(*rec), []string{"push", "rm", "run", "run", "fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded order = %v, want %v\nfull:\n%v", got, want, *rec)
	}
	if script := remoteScript((*rec)[1]); !strings.Contains(script, "'rm' '-rf' 'StrykerOutput'") {
		t.Errorf("the rm does not clear the whole output tree: %q", script)
	}
}

// TestStrykerNetRemoteRefusesAbsoluteOutputDir: an absolute output dir names a
// path on THIS machine. Rooting it under the remote workdir anyway would be
// dross deciding the user meant something they did not write.
func TestStrykerNetRemoteRefusesAbsoluteOutputDir(t *testing.T) {
	failLocalSpawns(t)
	rec := recordRemote(t, nil)

	s := &StrykerNet{ProjectRoot: t.TempDir(), OutputDir: "/tmp/out", Remote: helicon("/srv/dross")}
	_, err := s.Run([]string{"src/A.cs"})
	if err == nil {
		t.Fatal("an absolute output dir was accepted for a remote run")
	}
	if !strings.Contains(err.Error(), "/tmp/out") || !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the refusal names neither the dir nor the host: %v", err)
	}
	if len(*rec) != 0 {
		t.Errorf("a refused run still spawned %d commands: %v", len(*rec), *rec)
	}
}

// --- the local path is untouched ---

// TestRemoteNilSpawnsNothingThroughTheLauncherSeam is the regression half. Every
// existing argv-pinning test (TestStrykerRunArgsPinned,
// TestGremlinsBuildUnleashArgsDefault, TestStrykerNetRejectsDashOutputDir) still
// passes unmodified; this adds the property those cannot see — that a local run
// does not reach the remote seam at all, so no remote code path can alter it.
func TestRemoteNilSpawnsNothingThroughTheLauncherSeam(t *testing.T) {
	root := t.TempDir()
	rec := recordRemote(t, nil)

	var gotArgs []string
	orig := gremlinsBuildCmd
	gremlinsBuildCmd = func(g *Gremlins, args []string) *exec.Cmd {
		gotArgs = append([]string(nil), args...)
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				_ = os.WriteFile(filepath.Join(g.ProjectRoot, args[i+1]), []byte(fixtureGremlinsBareBasename), 0o644)
			}
		}
		return exec.Command("true")
	}
	t.Cleanup(func() { gremlinsBuildCmd = orig })

	g := &Gremlins{ProjectRoot: root}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*rec) != 0 {
		t.Errorf("a local run spawned %d commands through the remote seam: %v", len(*rec), *rec)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "gremlins" {
		t.Errorf("the local seam did not receive the bare gremlins argv: %v", gotArgs)
	}
}

// TestExistingKeyedLiteralsStillCompile is a compile-time assertion with a
// runtime tail.
//
// The launcher is held by each adapter as a NAMED Remote field rather than as an
// embedded struct precisely so this stays true: Go's keyed literals do not
// promote through an embedded field, so `&Stryker{Prefix: …}` — the form used in
// verify.go, survivor_drain.go and the three adapters' own tests — would have
// stopped compiling, and this task would have had no green commit to land on.
func TestExistingKeyedLiteralsStillCompile(t *testing.T) {
	for _, a := range []Adapter{
		&Gremlins{Prefix: "docker compose exec app", ProjectRoot: "/x"},
		&Stryker{Prefix: "docker compose exec app", ProjectRoot: "/x", Workdir: "web"},
		&StrykerNet{Prefix: "docker compose exec app", ProjectRoot: "/x"},
	} {
		if a.Name() == "" {
			t.Errorf("%T has no name", a)
		}
	}
}

// --- the launcher's own refusals ---

// TestLauncherRefusesPrefixAndTargetTogether keeps the mutual exclusion a real
// refusal rather than a comment.
//
// Per the locked docker_prefix_under_remote decision the wiring DROPS the docker
// prefix when a remote is granted, so this is unreachable today. It stays
// because "unreachable" is a claim about today's callers: a prefix wrapping an
// ssh invocation would run the mutation tool inside THIS machine's container,
// against a tree that is not there.
func TestLauncherRefusesPrefixAndTargetTogether(t *testing.T) {
	rec := recordRemote(t, nil)

	_, err := newLauncher("gremlins", "docker compose exec app", helicon("/srv/dross"), "/x", "")
	if err == nil {
		t.Fatal("a launcher with both a prefix and a remote was accepted")
	}
	for _, want := range []string{"docker compose exec app", "helicon"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if len(*rec) != 0 {
		t.Errorf("a refused launcher still built %d commands", len(*rec))
	}
}

// TestLauncherRefusesAdapterMissingFromTheReportTable: the table is closed, and
// an absent adapter is an error rather than a skipped step.
//
// A silent no-op here is invisible and wrong in the worst direction — no rm
// leaves a stale remote report to fetch, and no fetch leaves the adapter reading
// whatever the last run left locally. Either way the score is computed from
// numbers nobody measured this time.
func TestLauncherRefusesAdapterMissingFromTheReportTable(t *testing.T) {
	_, err := newLauncher("mutant-4000", "", helicon("/srv/dross"), "/x", "")
	if err == nil {
		t.Fatal("an adapter absent from the remote report table was accepted for a remote run")
	}
	if !strings.Contains(err.Error(), "mutant-4000") {
		t.Errorf("the refusal does not name the adapter: %v", err)
	}

	// The same adapter runs LOCALLY without complaint: the table governs remote
	// report handling, not adapter registration.
	if _, err := newLauncher("mutant-4000", "", nil, "/x", ""); err != nil {
		t.Errorf("a local run of an unknown adapter was refused: %v", err)
	}

	// And every adapter that actually exists is in it, so the refusal above can
	// never fire for a real one.
	for _, a := range []Adapter{&Gremlins{}, &Stryker{}, &StrykerNet{}} {
		if _, ok := remoteReportPaths[a.Name()]; !ok {
			t.Errorf("adapter %q has no remote report path", a.Name())
		}
	}
}

// --- c-3: the worker default reads the right machine ---

// TestRemoteWorkersDeriveFromTheProbedHost is the locked remote_workers
// decision. The halving rule is unchanged — 6 workers measured 0 timeouts where
// 14 produced 539 false ones that masked real survivors — but under a remote it
// must halve the REMOTE's cores. Reading runtime.NumCPU() here would size a
// 32-core host's run by this laptop.
func TestRemoteWorkersDeriveFromTheProbedHost(t *testing.T) {
	remoteG := &Gremlins{Remote: &remote.Target{Host: "helicon", Workdir: "/srv/dross", Cores: 32}}
	args, err := remoteG.buildUnleashArgs("reports/gremlins/x.json", []string{"./pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if got := flagValue(t, args, "--workers"); got != "16" {
		t.Errorf("--workers = %s with a 32-core remote, want 16 (this machine has %d cores)", got, runtime.NumCPU())
	}

	localG := &Gremlins{}
	args, err = localG.buildUnleashArgs("reports/gremlins/x.json", []string{"./pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flagValue(t, args, "--workers"), strconv.Itoa(defaultWorkers()); got != want {
		t.Errorf("--workers = %s with no remote, want the local default %s", got, want)
	}

	// An explicit setting still wins on both.
	remoteG.Workers = 3
	args, err = remoteG.buildUnleashArgs("reports/gremlins/x.json", []string{"./pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if got := flagValue(t, args, "--workers"); got != "3" {
		t.Errorf("an explicit Workers was overridden by the remote default: %s", got)
	}
}

func flagValue(t *testing.T, argv []string, flag string) string {
	t.Helper()
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	t.Fatalf("%s not present in %v", flag, argv)
	return ""
}

// TestRemoteRunCreatesTheReportDirectory is the regression test for the bug
// that made every remote run unmeasurable.
//
// reports/ is gitignored, and the locked `:- .gitignore` sync filter keeps
// ignored paths off the wire — so the report DIRECTORY never reaches the
// remote. Gremlins does not create parent dirs for --output; it fails on the
// first mutant write with "impossible to write file" and leaves nothing behind.
// The fetch then exits 23, which is read (correctly) as UnmeasuredMissing, and
// the run reports "no report — gremlins gathered no covered mutants" for every
// package. Nothing anywhere says the word "error": a fully working remote run
// scores 0/0 and reads as nothing-to-measure.
//
// That is why this asserts the mkdir rather than trusting the tool: every layer
// below already behaved correctly, and the result was still silence.
func TestRemoteRunCreatesTheReportDirectory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		run     func(t *testing.T, root string) error
		wantDir string
	}{
		{
			name: "gremlins",
			run: func(t *testing.T, root string) error {
				g := &Gremlins{ProjectRoot: root, Remote: helicon("/srv/dross")}
				_, err := g.Run([]string{"internal/verify/verify.go"})
				return err
			},
			wantDir: "reports/gremlins",
		},
		{
			name: "stryker",
			run: func(t *testing.T, root string) error {
				s := &Stryker{ProjectRoot: root, PackageManager: "pnpm", Remote: helicon("/srv/dross")}
				_, err := s.Run([]string{"src/a.ts"})
				return err
			},
			wantDir: "reports/mutation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			failLocalSpawns(t)
			rec := recordRemote(t, func(argv []string) {
				if isFetch(argv) {
					_ = os.WriteFile(argv[3], []byte(fixtureGremlinsBareBasename), 0o644)
				}
			})
			_ = tc.run(t, root)

			want := "'mkdir' '-p' '" + tc.wantDir + "'"
			if idx := indexOfScript(*rec, want); idx < 0 {
				t.Fatalf("no remote command creates the report directory %q:\n%v",
					tc.wantDir, remoteScripts(*rec))
			}
			// It must precede the tool, or the tool still has nowhere to write.
			mk := indexOfScript(*rec, want)
			tool := -1
			for i, argv := range *rec {
				s := remoteScript(argv)
				if strings.Contains(s, "'gremlins'") || strings.Contains(s, "'npx'") {
					tool = i
					break
				}
			}
			if tool < 0 {
				t.Fatalf("the tool was never invoked:\n%v", remoteScripts(*rec))
			}
			if mk > tool {
				t.Errorf("the report directory is created AFTER the tool runs (%d vs %d)", mk, tool)
			}
		})
	}
}

// TestRemoteReportDirIsCreatedInTheSameCommandAsTheRm pins the shape rather
// than only the effect: folding the mkdir into the rm's script keeps the
// per-package remote leg count at push/rm/run/fetch. A separate ssh call would
// double the round trips inside the package loop and would silently change the
// ordering every other test in this file asserts.
func TestRemoteReportDirIsCreatedInTheSameCommandAsTheRm(t *testing.T) {
	root := t.TempDir()
	failLocalSpawns(t)
	rec := recordRemote(t, func(argv []string) {
		if isFetch(argv) {
			_ = os.WriteFile(argv[3], []byte(fixtureGremlinsBareBasename), 0o644)
		}
	})

	g := &Gremlins{ProjectRoot: root, Remote: helicon("/srv/dross")}
	if _, err := g.Run([]string{"a/x.go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := kinds(*rec), []string{"push", "rm", "run", "fetch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the mkdir added a remote leg: %v, want %v", got, want)
	}
	script := remoteScript((*rec)[1])
	if !strings.Contains(script, "'rm' '-rf'") || !strings.Contains(script, "'mkdir' '-p'") {
		t.Errorf("the rm and the mkdir are not one command: %q", script)
	}
	if strings.Index(script, "'rm'") > strings.Index(script, "'mkdir'") {
		t.Errorf("the mkdir precedes the rm, so the rm deletes what it just made: %q", script)
	}
}
