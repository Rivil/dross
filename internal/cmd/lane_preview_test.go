package cmd

// `dross test lane preview` — the verb.
//
// Two properties carry every test here. It SPAWNS nothing, which is asserted
// against recorders rather than against the absence of output — a preview that
// ran a prepare and printed nothing about it would pass any output assertion.
// And it exits 0 on every finding, which is asserted beside the gate's own
// refusal over the same argv, because "preview exits 0" is only meaningful as a
// difference from what `dross test --files` does with the same input.

import (
	"strings"
	"testing"
)

// previewOut runs the verb and returns its transcript, failing on any error.
func previewOut(t *testing.T, args ...string) string {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Test(), append([]string{"lane", "preview"}, args...)...); err != nil {
		t.Fatalf("dross test lane preview %v: %v", args, err)
	}
	return out
}

// prepareLanes declares a lane with a prepare, so "spawns nothing" covers the
// bootstrap as well as the command.
const prepareLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1"
prepare = "go mod download"
selector = "go-package"`

// TestPreviewSpawnsNothing is the verb's whole promise, asserted against every
// seam that could execute something.
//
// Output assertions cannot carry this: a preview that ran the prepare and said
// nothing about it would satisfy any of them. Only the recorders can tell the
// difference between "described the run" and "performed it".
func TestPreviewSpawnsNothing(t *testing.T) {
	grantedLaneFixture(t, prepareLanes)
	installLaneLookPath(t)
	touchFile(t, "internal/cmd/here.go")
	log := &runLog{}
	log.probeSeam(t, nil, nil)
	log.spawnSeam(t)
	rec := installSpawnRecorder(t, nil)

	previewOut(t, "--files", "internal/cmd/here.go")

	if rec.count() != 0 {
		t.Errorf("preview spawned %d local command(s): %v", rec.count(), rec.lines)
	}
	for _, e := range log.events {
		if e == "rsync" {
			t.Error("preview synced the tree")
		}
		if strings.HasPrefix(e, "ssh") {
			t.Errorf("preview spawned remotely: %s", e)
		}
	}
}

// TestPreviewLineIsTheLineTheGateWouldSpawn is c-1 at the verb, asserted as an
// identity between the recorded spawn and the printed line.
//
// Each against a literal would pass while the two disagreed, which is exactly
// the drift a preview built on its own derivation would produce.
func TestPreviewLineIsTheLineTheGateWouldSpawn(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want 1 spawn, got %d", rec.count())
	}

	out := previewOut(t, "--files", "internal/cmd/test.go")
	if !strings.Contains(out, "lane go: "+rec.lines[0]) {
		t.Errorf("preview does not print the line the gate spawned (%q):\n%s", rec.lines[0], out)
	}
}

// TestPreviewExitsZeroWhereTheGateRefuses is locked preview_exit_status.
//
// Asserted as a PAIR over the same argv: preview describing and the gate
// refusing are the same fact seen twice, and a preview that inherited exit 2
// could be wired into CI as a gate that never ran a test.
func TestPreviewExitsZeroWhereTheGateRefuses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		file     string
		gateExit int
	}{
		{"out of tree", "/abs/x.go", exitBadFileSet},
		{"matches no lane", "NOTES.txt", exitNothingMeasured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filesFixture(t, selectorLanes)
			grantAllLanes(t)
			installSpawnRecorder(t, nil)

			gate := runCmd(t, Test(), "--files", tc.file)
			if ExitCode(gate) != tc.gateExit {
				t.Fatalf("the gate exited %d, want %d — the fixture proves nothing otherwise",
					ExitCode(gate), tc.gateExit)
			}
			if err := runCmd(t, Test(), "lane", "preview", "--files", tc.file); err != nil {
				t.Errorf("preview inherited the gate's refusal: %v", err)
			}
		})
	}
}

// TestPreviewNamesEveryNonRunningOutcome is c-2, in ONE invocation.
//
// One invocation rather than four, because the failure mode is a finding that
// gets swallowed when another one is also present — an outcome rendered only
// when it is the sole thing wrong is an outcome nobody sees on a real file set.
func TestPreviewNamesEveryNonRunningOutcome(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")

	out := previewOut(t,
		"--files", "internal/cmd/here.go", // the go lane's surviving path
		"--files", "internal/gone/away.go", // matched by the go lane, not on disk
		"--files", "docs/gone.md", // matched by the docs lane, every path gone
		"--files", "NOTES.txt", // in-tree, matches nothing
		"--files", "/abs/x.go", // outside the repo
	)

	for _, want := range []struct{ path, reason string }{
		{"internal/gone/away.go", "not on disk"},
		{"NOTES.txt", "no declared lane matches"},
		{"/abs/x.go", "outside this repository"},
	} {
		line := findLineWith(out, want.path)
		if line == "" {
			t.Errorf("no finding names %s:\n%s", want.path, out)
			continue
		}
		if !strings.Contains(line, want.reason) {
			t.Errorf("the finding for %s does not give its reason (%q): %q", want.path, want.reason, line)
		}
	}
	if !strings.Contains(out, "lane docs — every path matching it is gone") {
		t.Errorf("the docs lane's empty selector is not named:\n%s", out)
	}
}

// findLineWith returns the first line containing s, or "".
func findLineWith(out, s string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	return ""
}

// TestMalformedLaneIsAFindingNotAFault: the gate refuses the whole run over a
// malformed lane; preview names it and keeps going.
//
// Keeping going is the point. The lanes that ARE fine are the ones the user can
// still act on, and refusing the preview would hide them behind the one lane
// that needs an edit in project.toml.
func TestMalformedLaneIsAFindingNotAFault(t *testing.T) {
	filesFixture(t, malformedTemplateLane)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")
	rec := installSpawnRecorder(t, nil)

	out := previewOut(t, "--files", "internal/cmd/here.go")
	if !strings.Contains(out, "lane bad — would not run:") {
		t.Errorf("the malformed lane is not named as a finding:\n%s", out)
	}
	if !strings.Contains(out, "selector_template") {
		t.Errorf("the finding does not name the problem:\n%s", out)
	}
	if rec.count() != 0 {
		t.Errorf("preview spawned %d command(s)", rec.count())
	}
}

// TestUnknownLaneNameIsAFault: --lane goes through findLane, so a misspelling
// is an argv mistake and not an empty preview.
//
// Printing nothing for `--lane goo` would answer a question the user did not
// ask, and would read as "that lane matches none of your files".
func TestUnknownLaneNameIsAFault(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")

	err := runCmd(t, Test(), "lane", "preview", "--lane", "nope", "--files", "internal/cmd/here.go")
	if err == nil {
		t.Fatal("an unknown lane name was accepted")
	}
	if !strings.Contains(err.Error(), "go") || !strings.Contains(err.Error(), "docs") {
		t.Errorf("the refusal does not list the declared lanes: %v", err)
	}

	touchFile(t, "docs/a.md")
	out := previewOut(t, "--lane", "go", "--files", "internal/cmd/here.go", "--files", "docs/a.md")
	if !strings.Contains(out, "lane go:") {
		t.Errorf("--lane go rendered no go lane:\n%s", out)
	}
	if strings.Contains(out, "lane docs") {
		t.Errorf("--lane go rendered the docs lane too:\n%s", out)
	}
}

// TestBarePreviewTakesTheWorkingTree is locked bare_preview_default: the most
// common invocation is the one that needs no arguments.
func TestBarePreviewTakesTheWorkingTree(t *testing.T) {
	dir := laneFixture(t)
	gitInit(t, dir, "git@example.com:x/y.git")
	mustRunSet(t, "runtime.test_command", "go test ./...")
	appendLanes(t, dir, selectorLanes)
	installLaneLookPath(t)
	// Committed first, so the ONE untracked file below is the whole working
	// tree. Without a baseline the fixture's own scaffolding is in the set and
	// the count proves nothing about what preview picked up.
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-q", "-m", "baseline")
	touchFile(t, "internal/new/a.go")

	out := previewOut(t)
	if !strings.Contains(out, "1 file from the working tree") {
		t.Errorf("preview did not say what it took off the tree:\n%s", out)
	}
	if !strings.Contains(out, "lane go:") {
		t.Errorf("the untracked file did not reach the go lane:\n%s", out)
	}
}

// TestPreviewTakesTrailingPathsAsFiles is c-1's literal invocation: `dross test
// lane preview --files a.go b.go`.
//
// Trailing paths JOIN the file set and are never read as a lane name, which is
// what keeps locked preview_invocation intact — a positional read as a lane
// would make the same argv mean two different things depending on spelling.
func TestPreviewTakesTrailingPathsAsFiles(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/a.go")
	touchFile(t, "docs/b.md")

	out := previewOut(t, "--files", "internal/cmd/a.go", "docs/b.md")
	if !strings.Contains(out, "preview: 2 files") {
		t.Errorf("the trailing path did not join the file set:\n%s", out)
	}
	if !strings.Contains(out, "lane go:") || !strings.Contains(out, "lane docs:") {
		t.Errorf("the two-path set did not hit both lanes:\n%s", out)
	}
}
