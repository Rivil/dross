package cmd

// `dross test lane install` — the verb that owns both sides.
//
// The side it chose is asserted on CALL COUNTS rather than on prose: a header
// naming the host while the local seam fires is exactly the bug prose cannot
// catch, and it is the bug that provisions the wrong machine.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/remote"
)

// mustGrantLaneInstall consents to a lane's declared install line on this
// machine, through the real verb rather than the store — the gate is behaviour
// under test everywhere else, so a fixture that wrote around it could pass while
// the verb was broken.
func mustGrantLaneInstall(t *testing.T, name string) {
	t.Helper()
	if err := runCmd(t, Trust(), "--lane-install", name); err != nil {
		t.Fatalf("dross trust --lane-install %s: %v", name, err)
	}
}

// failingRemoteExec makes every remote install attempt fail, so a FAILED exec
// can be told apart from a REFUSED one.
func failingRemoteExec(t *testing.T) {
	t.Helper()
	orig := remoteExecFn
	t.Cleanup(func() { remoteExecFn = orig })
	remoteExecFn = func(remote.Target, []string) (string, error) {
		return "", fmt.Errorf("exit status 1")
	}
}

// laneInstallVerbFixture is a granted repo with one lane whose toolchain is
// `pnpm`, plus recording stubs for both install seams.
//
// hostMissing is what the granted host's probe reports absent; localMissing is
// what this machine lacks. Two independent knobs, because every side-selection
// case here is a different combination of the two.
func laneInstallVerbFixture(t *testing.T, lanes string, hostMissing, localMissing []string) (remoteCalls, localCalls *int) {
	t.Helper()
	grantedLaneFixture(t, lanes)
	installLaneLookPath(t, localMissing...)
	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: hostMissing}, nil
	})
	return installSeamRecorder(t)
}

const pnpmLane = `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"`

// TestInstallActsOnTheSideWithTheGap is c-6, asserted in both directions on
// call counts. A verb that always reached one seam would satisfy either half
// alone.
func TestInstallActsOnTheSideWithTheGap(t *testing.T) {
	t.Run("gap on the host", func(t *testing.T) {
		rn, ln := laneInstallVerbFixture(t, pnpmLane, []string{"pnpm"}, nil)
		var out string
		if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply"); err != nil {
			t.Fatal(err)
		}
		if *rn != 1 || *ln != 0 {
			t.Errorf("a host-side gap installed remote=%d local=%d, want 1 and 0", *rn, *ln)
		}
		if !strings.Contains(out, "helicon") {
			t.Errorf("the output does not name the machine it acted on:\n%s", out)
		}
		if strings.Contains(out, "this machine") {
			t.Errorf("a host install named this machine:\n%s", out)
		}
	})

	t.Run("gap on this machine", func(t *testing.T) {
		rn, ln := laneInstallVerbFixture(t, pnpmLane, nil, []string{"pnpm"})
		var out string
		if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply"); err != nil {
			t.Fatal(err)
		}
		if *rn != 0 || *ln != 1 {
			t.Errorf("a local gap installed remote=%d local=%d, want 0 and 1", *rn, *ln)
		}
		if !strings.Contains(out, "this machine") {
			t.Errorf("the output does not name the machine it acted on:\n%s", out)
		}
	})
}

// TestBothMachinesMissingInstallsOnTheHost: the side the lane would have RUN on
// wins, rather than whichever branch happens to be checked first. A refusal
// there would send the user to install by hand exactly what dross can install.
func TestBothMachinesMissingInstallsOnTheHost(t *testing.T) {
	rn, ln := laneInstallVerbFixture(t, pnpmLane, []string{"pnpm"}, []string{"pnpm"})

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply"); err != nil {
		t.Fatal(err)
	}
	if *rn != 1 || *ln != 0 {
		t.Errorf("both machines missing installed remote=%d local=%d, want the HOST", *rn, *ln)
	}
	if !strings.Contains(out, "helicon") {
		t.Errorf("the output does not name the host it provisioned:\n%s", out)
	}
}

// TestFailedProbeInstallsNowhere: a probe that failed told us nothing about
// either machine. A local install firing on the strength of an unanswered probe
// provisions the wrong machine on no evidence at all.
func TestFailedProbeInstallsNowhere(t *testing.T) {
	grantedLaneFixture(t, pnpmLane)
	installLaneLookPath(t, "pnpm")
	log := &runLog{}
	log.probeSeam(t, nil, remoteTransportErr())
	rn, ln := installSeamRecorder(t)

	err := runCmd(t, Test(), "lane", "install", "web", "--apply")
	if err == nil {
		t.Fatal("a failed probe was treated as an answer")
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("a failed probe still installed: remote=%d local=%d", *rn, *ln)
	}
	if !strings.Contains(err.Error(), "helicon") {
		t.Errorf("the failure does not name the host that did not answer: %v", err)
	}
}

// TestInstallIsDryRunByDefault is c-5's first half. The preview still has to
// show the argv, or a dry run is a verb that prints nothing useful and gets
// skipped straight to --apply.
func TestInstallIsDryRunByDefault(t *testing.T) {
	rn, ln := laneInstallVerbFixture(t, pnpmLane, []string{"pnpm"}, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web"); err != nil {
		t.Fatal(err)
	}
	if *rn != 0 || *ln != 0 {
		t.Fatalf("a dry run installed: remote=%d local=%d", *rn, *ln)
	}
	if !strings.Contains(out, "npm install -g pnpm") {
		t.Errorf("the dry run does not print what it would run:\n%s", out)
	}
	if !strings.Contains(out, "--apply") {
		t.Errorf("the dry run does not end with the --apply hint:\n%s", out)
	}
}

// TestATestFallbackInstallsNothing is c-5's other half — "never as a side
// effect". Asserted through a full `dross test --files` run that DOES fall
// back, with both install counters read afterwards.
func TestATestFallbackInstallsNothing(t *testing.T) {
	grantedLaneFixture(t, pnpmLane)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	log.spawnSeam(t)
	installSpawnRecorder(t, nil)
	rn, ln := installSeamRecorder(t)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if !strings.Contains(out, "fallback") {
		t.Fatalf("the fixture did not fall back, so it proves nothing:\n%s", out)
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("a fallback installed as a side effect: remote=%d local=%d", *rn, *ln)
	}
}

// TestLocalFlagForcesThisMachine is locked install_local_override: the escape
// hatch for a granted host that is up but is not the machine the user wants
// provisioned.
func TestLocalFlagForcesThisMachine(t *testing.T) {
	rn, ln := laneInstallVerbFixture(t, pnpmLane, []string{"pnpm"}, []string{"pnpm"})

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply", "--local"); err != nil {
		t.Fatal(err)
	}
	if *ln != 1 || *rn != 0 {
		t.Errorf("--local installed remote=%d local=%d, want this machine", *rn, *ln)
	}
	if !strings.Contains(out, "this machine") {
		t.Errorf("--local did not name this machine:\n%s", out)
	}
	if strings.Contains(out, "helicon") {
		t.Errorf("--local still named the host:\n%s", out)
	}
}

// TestVerbRefusesAnUngrantedDeclaredLine: refused before ANY I/O, so an
// ungranted line does not even earn a connection to another machine.
func TestVerbRefusesAnUngrantedDeclaredLine(t *testing.T) {
	grantedLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"
install = "corepack enable pnpm"`)
	installLaneLookPath(t, "pnpm")
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	rn, ln := installSeamRecorder(t)

	err := runCmd(t, Test(), "lane", "install", "web", "--apply")
	if err == nil {
		t.Fatal("an ungranted declared install line was accepted")
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("a refused install still reached a seam: remote=%d local=%d", *rn, *ln)
	}
	if log.probes != 0 {
		t.Errorf("an ungranted line was probed for anyway (%d probes) — refused before ANY I/O", log.probes)
	}
	if !strings.Contains(err.Error(), "corepack enable pnpm") {
		t.Errorf("the refusal does not print the line: %v", err)
	}
	if !strings.Contains(err.Error(), "dross trust --lane-install web") {
		t.Errorf("the refusal does not name the fix: %v", err)
	}
}

// TestRefusalIsNotAFailedAttempt is c-8. A refusal and a failed exec have
// different remedies and only one of them is dross's to perform, so they are
// counted apart even though both exit non-zero.
func TestRefusalIsNotAFailedAttempt(t *testing.T) {
	t.Run("a runtime is refused, never attempted", func(t *testing.T) {
		rn, ln := laneInstallVerbFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "node run.js"`, []string{"node"}, nil)

		var out string
		err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply")
		if err == nil {
			t.Fatal("a runtime refusal exited 0")
		}
		if *rn != 0 || *ln != 0 {
			t.Errorf("a refusal was attempted: remote=%d local=%d", *rn, *ln)
		}
		if !strings.Contains(out, "refused") || !strings.Contains(out, "1 refused, 0 failed") {
			t.Errorf("the refusal was not counted apart from a failure:\n%s", out)
		}
	})

	t.Run("a failed exec is counted as a failure", func(t *testing.T) {
		grantedLaneFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"
install = "corepack enable pnpm"`)
		installLaneLookPath(t, "pnpm")
		fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
			return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
		})
		mustGrantLaneInstall(t, "web")
		failingRemoteExec(t)

		var out string
		err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply")
		if err == nil {
			t.Fatal("a failed install exited 0")
		}
		if !strings.Contains(out, "0 refused, 1 failed") {
			t.Errorf("a failed exec was not counted as a failure:\n%s", out)
		}
	})
}

// TestUndeclaredToolIsReportedAndExitsZero is locked undeclared_exit. Counting
// it would make every repo with lanes and no install lines start exiting 1 on a
// command that passed the day before.
func TestUndeclaredToolIsReportedAndExitsZero(t *testing.T) {
	rn, ln := laneInstallVerbFixture(t, `[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "bespoke-runner test"`, []string{"bespoke-runner"}, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply"); err != nil {
		t.Fatalf("a tool with no recipe and no install line exited non-zero: %v", err)
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("an unknown tool was attempted: remote=%d local=%d", *rn, *ln)
	}
	if !strings.Contains(out, "no install line") {
		t.Errorf("the report does not name the gap:\n%s", out)
	}
	if !strings.Contains(out, "--install") {
		t.Errorf("the report does not say how to close it:\n%s", out)
	}
}

// TestNothingToInstallExitsZero: a provisioned machine is the success case, and
// a verb that failed on "there was nothing to do" could not go in a script.
func TestNothingToInstallExitsZero(t *testing.T) {
	rn, ln := laneInstallVerbFixture(t, pnpmLane, nil, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "install", "web", "--apply"); err != nil {
		t.Fatalf("a fully provisioned lane exited non-zero: %v", err)
	}
	if *rn != 0 || *ln != 0 {
		t.Errorf("nothing was missing but something installed: remote=%d local=%d", *rn, *ln)
	}
	if !strings.Contains(out, "nothing to install") {
		t.Errorf("the no-op case is not reported:\n%s", out)
	}
}

// TestInstallResolvesLanesThroughTheSharedFinder: an unknown name lists the
// declared lanes exactly as `dross trust --lane` does, and does it before
// anything is probed.
func TestInstallResolvesLanesThroughTheSharedFinder(t *testing.T) {
	grantedLaneFixture(t, pnpmLane)
	installLaneLookPath(t)
	log := &runLog{}
	log.probeSeam(t, []string{"pnpm"}, nil)
	installSeamRecorder(t)

	err := runCmd(t, Test(), "lane", "install", "nope")
	if err == nil {
		t.Fatal("`lane install nope` was accepted")
	}
	for _, want := range []string{"nope", "web"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if log.probes != 0 {
		t.Errorf("an unknown lane was probed for anyway: %d probes", log.probes)
	}
}

// TestTheOfferedInvocationResolves: the fallback line advertises `dross test
// lane install <name>`, and a transcript that offered a command the binary does
// not have would be worse than offering nothing.
func TestTheOfferedInvocationResolves(t *testing.T) {
	// Resolved through cobra's own tree, from the string the fallback prints,
	// rather than by calling the builder directly — the builder existing is not
	// the same as it being registered where the offer points.
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Test())

	offered := strings.Fields("test lane install web")
	found, _, err := root.Find(offered)
	if err != nil {
		t.Fatalf("the offered invocation does not resolve: %v", err)
	}
	if found.Name() != "install" {
		t.Errorf("`dross %s` resolved to %q, want the install command", strings.Join(offered, " "), found.Name())
	}
	if found.Flags().Lookup("apply") == nil || found.Flags().Lookup("local") == nil {
		t.Error("the resolved command is missing --apply or --local")
	}
}
