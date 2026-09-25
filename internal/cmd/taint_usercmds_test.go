package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// The user-command burn-down's guards: the spawns in seven internal/cmd files —
// the suite, slot and verify streams, lane installs, the self-update, the
// drain's package discovery and techdebt's file list. A replay's captured
// tail, quoted into a repoint refusal two files away, is still the suite's
// output. The zero-findings gate is retired into TestNoSpawnOutputEscapes
// (taint_audit_test.go).
//
// The streams of `dross test`, `dross run` and `dross verify` reach the
// terminal through an OutOrStdout-derived writer and carry no marker: a marker
// on a stream site would clear the stream itself.

// streamSiteFiles carry the user's own suite and slot streams.
var streamSiteFiles = []string{"internal/cmd/test.go", "internal/cmd/run.go", "internal/cmd/verify.go"}

// TestStreamSitesCarryNoMarker: the suite, slot and verify streams end at the
// terminal; none of their files may carry a taint-cleared marker. A pin whose
// spawn moved away would pass this vacuously, so each must still hold one.
func TestStreamSitesCarryNoMarker(t *testing.T) {
	assertPinsHoldSites(t, repoExecGraph(t), "streamSiteFiles", streamSiteFiles)
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
