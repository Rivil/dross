package mutation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// c-4, executed: a broken remote fails loudly with a named cause and never
// degrades to a local run or to an empty report that reads as clean.
//
// The failure mode this file exists for is subtle enough to be worth stating.
// Both adapters tolerate ANY *exec.ExitError, reasoning that a mutation tool
// exits non-zero when mutants survive. Over ssh that reasoning breaks: ssh
// relays the remote program's exit code through the SAME channel it reports its
// own failures on. Under the unnarrowed rule an unreachable host and an
// uninstalled remote toolchain both arrive as "gremlins wrote no report", which
// is UnmeasuredMissing — a non-fatal skip the score excludes. The leg then
// reports clean because nothing ran, which is the exact inversion c-4 forbids.

// remoteExit is a per-command exit-code script for the fake seam, keyed by a
// substring of the command it should fire on.
type remoteExit struct {
	match string // "" matches the tool run; "rsync -az" the push; "rsync -a" a fetch
	code  int
}

// scriptRemote swaps the launcher seam for one that records every argv and
// returns a command exiting with the code the script asks for.
//
// A real subprocess rather than a synthesized error: the classification under
// test reads *exec.ExitError.ExitCode(), and a hand-built error would let a
// wrong read pass.
func scriptRemote(t *testing.T, exits []remoteExit, onCmd func(argv []string)) *[][]string {
	t.Helper()
	var rec [][]string
	orig := launcherCommand
	launcherCommand = func(argv []string, stdin string) *exec.Cmd {
		cp := append(append([]string(nil), argv...), stdin)
		rec = append(rec, cp)
		if onCmd != nil {
			onCmd(cp)
		}
		for _, e := range exits {
			if matchesLeg(cp, e.match) {
				return exitingCmd(e.code)
			}
		}
		return exitingCmd(0)
	}
	t.Cleanup(func() { launcherCommand = orig })
	return &rec
}

// matchesLeg classifies a recorded argv against a script key.
func matchesLeg(argv []string, key string) bool {
	switch key {
	case "push":
		return argv[0] == "rsync" && argv[1] == "-az"
	case "fetch":
		return argv[0] == "rsync" && argv[1] == "-a"
	case "rm":
		return argv[0] == "ssh" && strings.Contains(argv[len(argv)-1], "'rm' '-rf'")
	case "tool":
		return argv[0] == "ssh" && !strings.Contains(argv[len(argv)-1], "'rm' '-rf'")
	}
	return false
}

func exitingCmd(code int) *exec.Cmd {
	if code == 0 {
		return exec.Command("true")
	}
	return exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
}

func countLeg(rec [][]string, key string) int {
	n := 0
	for _, argv := range rec {
		if matchesLeg(argv, key) {
			n++
		}
	}
	return n
}

func remoteGremlins(root string) *Gremlins {
	return &Gremlins{ProjectRoot: root, Remote: &remote.Target{Host: "helicon", Workdir: "/srv/dross"}}
}

// TestRemotePushFailureIsFatalAndSpawnsNothing: if the tree never arrived,
// nothing downstream can be meaningful. The run must stop at the push, not
// carry on and measure whatever is already on the host.
func TestRemotePushFailureIsFatalAndSpawnsNothing(t *testing.T) {
	failLocalSpawns(t)
	rec := scriptRemote(t, []remoteExit{{match: "push", code: 255}}, nil)

	g := remoteGremlins(t.TempDir())
	report, err := g.Run([]string{"internal/argfence/argfence.go"})
	if err == nil {
		t.Fatal("a failed rsync push produced no error")
	}
	if report != nil {
		t.Errorf("a failed push still produced a Report: %+v", report)
	}
	for _, want := range []string{"helicon", "rsync"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
	if !errors.Is(err, remote.ErrTransport) {
		t.Errorf("a 255 push is not classified as a transport failure: %v", err)
	}
	if n := countLeg(*rec, "tool") + countLeg(*rec, "rm"); n != 0 {
		t.Errorf("a failed push still issued %d ssh command(s): %v", n, *rec)
	}
}

// TestRemoteTransportFailureNeverDegradesToLocal is c-4's "never degrades to a
// local run", asserted at the only place it could happen: the process seam.
//
// failLocalSpawns fails the test on any gremlins/npx/dotnet spawn, so a
// fallback path added later — however reasonable it looked — goes red here
// rather than silently producing a report measured on the wrong machine.
func TestRemoteTransportFailureNeverDegradesToLocal(t *testing.T) {
	for _, tc := range []struct{ name, leg string }{
		{"push", "push"},
		{"remote rm", "rm"},
		{"tool run", "tool"},
		{"fetch", "fetch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failLocalSpawns(t)
			scriptRemote(t, []remoteExit{{match: tc.leg, code: 255}}, nil)

			g := remoteGremlins(t.TempDir())
			if _, err := g.Run([]string{"a/x.go"}); err == nil {
				t.Fatalf("a 255 on the %s leg produced no error", tc.leg)
			} else if !errors.Is(err, remote.ErrTransport) {
				t.Errorf("a 255 on the %s leg is not a transport failure: %v", tc.leg, err)
			}
		})
	}
}

// TestRemoteToolExitCodesAreClassifiedDistinctly is the heart of c-4, and the
// three cases have to be asserted together — each one alone is satisfiable by a
// rule that gets the other two wrong.
//
//   - 127 with no report is FATAL. The remote shell could not execute the
//     command, so the tool never ran. Today's fall-through would file this as
//     UnmeasuredMissing, which the drain treats as non-fatal and the score
//     excludes: an uninstalled remote toolchain would read as a clean leg.
//   - exit 1 WITH a report is tolerated and parsed. That is gremlins reporting
//     surviving mutants — a successful measurement with bad results, and
//     escalating it would make every real finding fatal.
//   - exit 0 with no report stays UnmeasuredMissing. Gremlins gathered no
//     covered mutants; nothing was learned, and that is not an error.
func TestRemoteToolExitCodesAreClassifiedDistinctly(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "gremlins_pkg_report.json"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("127 with no report is fatal and names the binary and host", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{{match: "tool", code: 127}}, nil)

		g := remoteGremlins(t.TempDir())
		report, rerr := g.Run([]string{"internal/argfence/argfence.go"})
		if rerr == nil {
			t.Fatal("an uninstalled remote toolchain was reported as a skip, not an error")
		}
		if report != nil {
			t.Errorf("a fatal exit still produced a Report: %+v", report)
		}
		if !strings.Contains(rerr.Error(), "gremlins not found on helicon") {
			t.Errorf("the error does not name the binary and the host: %v", rerr)
		}
		if len(g.Unmeasured) != 0 {
			t.Errorf("a fatal exit was also filed as Unmeasured: %+v", g.Unmeasured)
		}
	})

	t.Run("126 is fatal too", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{{match: "tool", code: 126}}, nil)

		g := remoteGremlins(t.TempDir())
		if _, rerr := g.Run([]string{"a/x.go"}); rerr == nil {
			t.Fatal("a remote command that could not be executed (126) was tolerated")
		}
	})

	t.Run("exit 1 with a report is tolerated and parsed", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{{match: "tool", code: 1}}, func(argv []string) {
			if matchesLeg(argv, "fetch") {
				if werr := os.WriteFile(argv[3], payload, 0o644); werr != nil {
					t.Fatalf("fake fetch: %v", werr)
				}
			}
		})

		g := remoteGremlins(t.TempDir())
		report, rerr := g.Run([]string{"internal/argfence/argfence.go"})
		if rerr != nil {
			t.Fatalf("surviving mutants (exit 1) were escalated to a failure: %v", rerr)
		}
		if report == nil || report.Survived == 0 {
			t.Fatalf("the report was not parsed after a tolerated non-zero exit: %+v", report)
		}
	})

	t.Run("exit 0 with no report stays UnmeasuredMissing", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{}, nil) // everything succeeds; nothing is delivered

		g := remoteGremlins(t.TempDir())
		report, rerr := g.Run([]string{"a/x.go"})
		if rerr != nil {
			t.Fatalf("a package with no covered mutants was made fatal: %v", rerr)
		}
		if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
			t.Fatalf("want one UnmeasuredMissing entry, got %+v", g.Unmeasured)
		}
		if report == nil {
			t.Fatal("a skip must still produce a (empty) Report")
		}
	})
}

// TestRemoteFetchFailureSplitsPartialFromTransport.
//
// rsync 23 is "the source file is not there", which means exactly what a missing
// local report means — the tool wrote nothing. Anything else tells us nothing
// about whether a report exists, and calling it "no report" would mark a package
// unmeasured that may have measured fine.
func TestRemoteFetchFailureSplitsPartialFromTransport(t *testing.T) {
	t.Run("rsync 23 keeps UnmeasuredMissing", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{{match: "fetch", code: 23}}, nil)

		g := remoteGremlins(t.TempDir())
		if _, err := g.Run([]string{"a/x.go"}); err != nil {
			t.Fatalf("an absent remote report was made fatal: %v", err)
		}
		if len(g.Unmeasured) != 1 || g.Unmeasured[0].Kind != UnmeasuredMissing {
			t.Errorf("want one UnmeasuredMissing entry, got %+v", g.Unmeasured)
		}
	})

	t.Run("rsync 255 is fatal", func(t *testing.T) {
		failLocalSpawns(t)
		scriptRemote(t, []remoteExit{{match: "fetch", code: 255}}, nil)

		g := remoteGremlins(t.TempDir())
		report, err := g.Run([]string{"a/x.go"})
		if err == nil {
			t.Fatal("a dead connection during the fetch was filed as a skip")
		}
		if report != nil {
			t.Errorf("a fatal fetch still produced a Report: %+v", report)
		}
		if !errors.Is(err, remote.ErrTransport) {
			t.Errorf("the fetch failure is not classified as transport: %v", err)
		}
	})
}

// TestRemoteRunClearsYesterdaysLocalReport: the local report path is cleared
// before the remote package runs, so a run that fails or delivers nothing cannot
// merge the previous run's numbers.
//
// Without this the worst case is silent and plausible: a transport failure
// leaves yesterday's report in place, the read succeeds, and the phase is scored
// against measurements taken from a different tree.
func TestRemoteRunClearsYesterdaysLocalReport(t *testing.T) {
	root := t.TempDir()
	stale := GremlinsReportPath(root, "./a")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(fixtureGremlinsBareBasename), 0o644); err != nil {
		t.Fatal(err)
	}

	failLocalSpawns(t)
	scriptRemote(t, []remoteExit{{match: "tool", code: 255}}, nil)

	g := remoteGremlins(root)
	if _, err := g.Run([]string{"a/x.go"}); err == nil {
		t.Fatal("a 255 tool run produced no error")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("yesterday's report survived a failed remote run, stat err = %v", err)
	}
}

// TestLocalExitTolerationIsUnchanged is the regression half: narrowing the
// remote tolerance must not narrow the local one. A local gremlins exiting 1
// with surviving mutants — or 127, which locally means the process could not
// start and is already handled by the not-an-ExitError path — must behave
// exactly as before.
func TestLocalExitTolerationIsUnchanged(t *testing.T) {
	var local Launcher
	for _, code := range []int{1, 2, 126, 127, 255} {
		if err := local.remoteExitFatal("gremlins", code); err != nil {
			t.Errorf("a LOCAL exit %d was escalated: %v", code, err)
		}
	}

	rl := &Launcher{Target: &remote.Target{Host: "helicon", Workdir: "/srv/dross"}}
	if err := rl.remoteExitFatal("gremlins", 0); err != nil {
		t.Errorf("a clean remote exit was escalated: %v", err)
	}
	for _, code := range []int{1, 2, 3} {
		if err := rl.remoteExitFatal("gremlins", code); err != nil {
			t.Errorf("a remote exit %d (the tool speaking) was escalated: %v", code, err)
		}
	}
}
