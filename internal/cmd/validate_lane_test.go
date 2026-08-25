package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// laneFixture is a repo that validates clean, ready for a lane block to be
// appended. Everything these tests assert is therefore attributable to the
// lane: a problem list of length zero before, and exactly the lane's own
// problems after.
func laneFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	return dir
}

// appendLanes adds raw [[runtime.test_lane]] blocks to the fixture's
// project.toml. Appended as text, not written through a struct: an
// array-of-tables header names its full path from the document root, so it is
// valid wherever it lands, and the tests then exercise the same decode path a
// hand-edited project.toml takes — which is the path validate exists for.
func appendLanes(t *testing.T, dir, blocks string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", "project.toml")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + blocks + "\n"); err != nil {
		t.Fatal(err)
	}
}

// validateOutput runs validate over the cwd fixture and returns its printed
// problem lines alongside the error, since the error only carries a count and
// every assertion here is about which lane was named.
func validateOutput(t *testing.T) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Validate())
	return out, err
}

// TestValidateNamesLaneMissingCommand: a lane with a name and globs but no
// command is unrunnable, and the message has to say WHICH lane so a
// project.toml carrying several does not send the user reading all of them.
func TestValidateNamesLaneMissingCommand(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a lane with no command:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "command") {
		t.Errorf("problem must name the lane and the missing field, got:\n%s", out)
	}
}

// TestValidateNamesLaneWithEmptyMatch: a lane matching nothing is not a lane
// that runs rarely, it is a lane that can never be selected — reported by name
// rather than accepted as a deliberate no-op.
func TestValidateNamesLaneWithEmptyMatch(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "docs"
match = []
command = "true"`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a lane with an empty match list:\n%s", out)
	}
	if !strings.Contains(out, `"docs"`) || !strings.Contains(out, "match") {
		t.Errorf("problem must name the lane and the empty match list, got:\n%s", out)
	}
}

// TestValidateIdentifiesNamelessLaneByIndex: a nameless lane cannot be named,
// so it is addressed by its ordinal. Reporting it as `runtime.test_lane ""`
// would point at nothing when the document holds more than one block.
func TestValidateIdentifiesNamelessLaneByIndex(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
match = ["internal/**"]
command = "go test ./..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted a nameless lane:\n%s", out)
	}
	if !strings.Contains(out, "runtime.test_lane[0]") {
		t.Errorf("nameless lane must be addressed by ordinal, got:\n%s", out)
	}
	if strings.Contains(out, `runtime.test_lane ""`) {
		t.Errorf("nameless lane reported as the empty name, which points at nothing:\n%s", out)
	}
}

// TestValidateRejectsDuplicateLaneName: the machine-local grant store is keyed
// by lane name, so two lanes sharing one name means one grant with authority
// over two different command lines. Dropping this check is how a consented
// `go test` lane silently authorizes whatever the second `go` lane runs.
func TestValidateRejectsDuplicateLaneName(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test ./internal/..."

[[runtime.test_lane]]
name = "go"
match = ["cmd/**"]
command = "go test ./cmd/..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted two lanes named go:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) || !strings.Contains(out, "more than once") {
		t.Errorf("problem must report the repeated lane name, got:\n%s", out)
	}
}

// TestValidateRejectsUncompilableGlob: a pattern that does not compile matches
// nothing, forever. Left unchecked it surfaces later as a file set that
// mysteriously selects no lane — a file-set-shaped symptom for a
// lane-configuration cause, which sends the user to the wrong file.
func TestValidateRejectsUncompilableGlob(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/[cmd/**"]
command = "go test ./..."`)

	out, err := validateOutput(t)
	if err == nil {
		t.Fatalf("validate accepted an uncompilable glob:\n%s", out)
	}
	if !strings.Contains(out, `"go"`) {
		t.Errorf("problem must name the lane, got:\n%s", out)
	}
	if !strings.Contains(out, "internal/[cmd/**") {
		t.Errorf("problem must quote the offending pattern, got:\n%s", out)
	}
}

// TestValidateAcceptsAWellFormedLane is the other side of every check above:
// a complete lane must pass. Without it, a validator that rejected all lanes
// would satisfy the whole rest of this file.
func TestValidateAcceptsAWellFormedLane(t *testing.T) {
	dir := laneFixture(t)
	appendLanes(t, dir, `[[runtime.test_lane]]
name = "go"
match = ["internal/**/*.go", "main.go"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "true"`)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("validate rejected well-formed lanes: %v\n%s", err, out)
	}
}

// TestValidateIgnoresARepoWithNoLanes: lanes are opt-in (the bare_test_run
// decision), so the validator must invent no work for the repos that never
// asked for them — which today is every existing repo.
func TestValidateIgnoresARepoWithNoLanes(t *testing.T) {
	laneFixture(t)

	out, err := validateOutput(t)
	if err != nil {
		t.Fatalf("a lane-less repo must still validate clean: %v\n%s", err, out)
	}
	if strings.Contains(out, "test_lane") {
		t.Errorf("a lane-less repo must produce no lane problems, got:\n%s", out)
	}
}
