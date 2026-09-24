package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// The user-command burn-down gate. Scoped BY ORIGIN to the spawns in seven
// internal/cmd files — the suite, slot and verify streams, lane installs, the
// self-update, the drain's package discovery and techdebt's file list —
// wherever their output escapes. A replay's captured tail, quoted into a
// repoint refusal two files away, is still the suite's output.
//
// The streams of `dross test`, `dross run` and `dross verify` reach the
// terminal through an OutOrStdout-derived writer and carry no marker: a marker
// on a stream site would clear the stream itself.

var userCmdOriginFiles = []string{
	"internal/cmd/lane_install.go",
	"internal/cmd/run.go",
	"internal/cmd/test.go",
	"internal/cmd/update.go",
	"internal/cmd/verify.go",
	"internal/cmd/survivor_drain.go",
	"internal/cmd/techdebt.go",
}

// streamSiteFiles carry the user's own suite and slot streams.
var streamSiteFiles = []string{"internal/cmd/test.go", "internal/cmd/run.go", "internal/cmd/verify.go"}

// TestNoUserCommandOutputEscapes is the gate.
func TestNoUserCommandOutputEscapes(t *testing.T) {
	taint, markers := execTaintScan(liveView(t))
	root := sourceProgram(t).Root
	for _, f := range taint {
		for _, o := range f.Origins {
			rel, _ := filepath.Rel(root, o.Filename)
			if containsString(userCmdOriginFiles, filepath.ToSlash(rel)) {
				t.Errorf("%s — %s", f, execTaintRemedy)
				break
			}
		}
	}
	for _, m := range markers {
		rel, _ := filepath.Rel(root, m.Escape.Filename)
		if containsString(userCmdOriginFiles, filepath.ToSlash(rel)) {
			t.Error(m.String())
		}
	}
}

// TestStreamSitesCarryNoMarker: the suite, slot and verify streams end at the
// terminal; none of their files may carry a taint-cleared marker.
func TestStreamSitesCarryNoMarker(t *testing.T) {
	root := sourceProgram(t).Root
	for _, m := range taintMarkersIn(t, "internal/cmd") {
		rel, _ := filepath.Rel(root, m.file)
		if containsString(streamSiteFiles, filepath.ToSlash(rel)) {
			t.Errorf("%s:%d carries a taint-cleared marker — the stream must end at the terminal, not at a marker", rel, m.line)
		}
	}
}

// TestLaneInstallOutputGoesToStderr: an install that fails prints its own
// output to stderr and returns an error naming the binary only.
func TestLaneInstallOutputGoesToStderr(t *testing.T) {
	var stderr strings.Builder
	prev := installStderr
	installStderr = &stderr
	defer func() { installStderr = prev }()

	err := runInstallLocally([]string{"sh", "-c", "echo CANARY-LANE; exit 2"})
	if err == nil {
		t.Fatal("a failing install returned no error")
	}
	if strings.Contains(err.Error(), "CANARY-LANE") {
		t.Errorf("the install's output reached the error: %q", err)
	}
	if !strings.Contains(stderr.String(), "CANARY-LANE") {
		t.Errorf("stderr = %q, want the install's output there", stderr.String())
	}
}

// TestUserCommandFixKeepsTheSurgeryAnchors: run.go and verify.go carry t-11's
// exec-consent surgery anchors, and this burn-down must not move them.
func TestUserCommandFixKeepsTheSurgeryAnchors(t *testing.T) {
	for _, op := range execLiveSurgeries {
		if op.file != "internal/cmd/run.go" && op.file != "internal/cmd/verify.go" {
			continue
		}
		if _, err := repoExecGraph(t).surgery(sourceProgram(t).Root, op); err != nil {
			t.Error(err)
		}
	}
}
