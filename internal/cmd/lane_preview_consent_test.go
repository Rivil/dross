package cmd

// Preview's two annotations: whether a lane's command is trusted here, and
// where it would run.
//
// Both are things the RUN acts on and preview only reports. The gate refuses an
// ungranted lane and convicts a lane whose toolchain is on neither machine;
// preview prints the same facts beside the derived line and exits 0. Every test
// here asserts the annotation AND the absence of the action.

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// TestPreviewRendersAnUngrantedLaneWithoutRefusing is c-3: a line that would be
// refused at run time is visible as refused BEFORE it is run.
//
// Asserted beside the gate's own behaviour over the same lane, because "preview
// does not refuse" only means something as a difference — and the derived line
// must still print, since seeing the line you have not yet trusted is the whole
// point of looking.
func TestPreviewRendersAnUngrantedLaneWithoutRefusing(t *testing.T) {
	filesFixture(t, selectorLanes)
	touchFile(t, "internal/cmd/here.go")
	rec := installSpawnRecorder(t, nil)

	out := previewOut(t, "--files", "internal/cmd/here.go")
	if !strings.Contains(out, "consent: "+ConsentAbsent.String()) {
		t.Errorf("the ungranted lane is not annotated absent:\n%s", out)
	}
	if !strings.Contains(out, "lane go: go test -count=1 './internal/cmd/...'") {
		t.Errorf("the derived line did not print for an ungranted lane:\n%s", out)
	}
	if rec.count() != 0 {
		t.Errorf("preview spawned %d command(s)", rec.count())
	}

	// The same lane, at the gate: the refusal preview declined to make.
	if err := runCmd(t, Test(), "--files", "internal/cmd/here.go"); ExitCode(err) != exitLaneRefused {
		t.Errorf("the gate exited %d over the same lane, want %d — preview's exit 0 proves nothing otherwise",
			ExitCode(err), exitLaneRefused)
	}
}

// TestStaleAndGrantedAreDistinguished: preview reads the consent LADDER, not a
// boolean.
//
// "Stale — the command changed since you trusted it" and "never trusted here"
// call for very different reactions, and a preview that collapsed them would
// send a user to `dross trust` for a lane they had already read once. Asserted
// against ConsentState.String() so a second vocabulary cannot be hand-written
// here.
func TestStaleAndGrantedAreDistinguished(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")
	touchFile(t, "docs/a.md")
	// The go lane's command moves out from under its grant. The grant is kept
	// deliberately — that is what makes the state stale rather than absent.
	if err := runCmd(t, Test(), "lane", "edit", "go", "--command", "go test -count=1 -race"); err != nil {
		t.Fatalf("lane edit: %v", err)
	}

	out := previewOut(t, "--files", "internal/cmd/here.go", "--files", "docs/a.md")
	goLine := previewLaneBlock(out, "go")
	docsLine := previewLaneBlock(out, "docs")
	if !strings.Contains(goLine, "consent: "+ConsentStale.String()) {
		t.Errorf("the edited lane is not stale:\n%s", goLine)
	}
	if !strings.Contains(docsLine, "consent: "+ConsentGranted.String()) {
		t.Errorf("the untouched lane is not granted:\n%s", docsLine)
	}
}

// previewLaneBlock returns the transcript from one lane's header up to the
// next one, so an absence assertion is about ONE lane rather than the whole
// report.
func previewLaneBlock(out, name string) string {
	lines := strings.Split(out, "\n")
	var block []string
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, "lane ") {
			in = strings.HasPrefix(l, "lane "+name+":") || strings.HasPrefix(l, "lane "+name+" ")
		}
		if in {
			block = append(block, l)
		}
	}
	return strings.Join(block, "\n")
}

// TestUnreachableHostStillExitsZero: a dead network is a fact about the host,
// not about the file set the user asked about.
//
// Preview's answer degrades to "unresolved" and keeps every derived line it
// already had. Exiting non-zero would make a verb that spawns nothing report a
// transport failure — and would take the derived lines down with it.
func TestUnreachableHostStillExitsZero(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	transportProbe(t)

	out := previewOut(t, "--files", "internal/a.go", "--files", "web/app.ts")
	if !strings.Contains(out, "host: helicon ("+string(hostUnresolved)+")") {
		t.Errorf("the host is not reported unresolved:\n%s", out)
	}
	for _, lane := range []string{"go", "web"} {
		block := previewLaneBlock(out, lane)
		if !strings.Contains(block, "runs on: unresolved") {
			t.Errorf("lane %s does not render unresolved:\n%s", lane, block)
		}
		if !strings.Contains(block, "helicon") {
			t.Errorf("lane %s does not name the configured host:\n%s", lane, block)
		}
	}
}

// TestNoProbeAsksNothing: the flag's promise is the instant offline read.
//
// The call counter is the assertion, not the output — a preview that probed and
// then rendered "unprobed" would satisfy any string check while costing the ssh
// round trip the flag exists to skip.
func TestNoProbeAsksNothing(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	calls := fakeProbe(t, func(remote.Target, []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})

	out := previewOut(t, "--no-probe", "--files", "internal/a.go")
	if *calls != 0 {
		t.Errorf("--no-probe opened %d connection(s)", *calls)
	}
	if !strings.Contains(out, "host: helicon ("+string(hostUnprobed)+")") {
		t.Errorf("the configured host is not reported unprobed:\n%s", out)
	}
}
