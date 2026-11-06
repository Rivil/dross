package cmd

// The whole scoped journey on a real git repo: real Test(), a real relate script
// run through the real spawn, the real commit-green on a real `git commit`
// payload, then verify. No record here is seeded, so the recorder and the gate
// have to agree on their own, and that agreement is what this file proves.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
)

// e2eVouchingLane runs real scripts: relate.sh answers for the tests it knows,
// and run.sh passes.
const e2eVouchingLane = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "sh scripts/run.sh"
selector = "path"
vouch = true
relate = "sh scripts/relate.sh"`

// e2ePlainLane is the same lane with no vouch, so a green there never admits.
const e2ePlainLane = `[[runtime.test_lane]]
name = "web"
match = ["src/**"]
command = "sh scripts/run.sh"
selector = "path"`

// e2eRelateScript maps the component to a committed test no --files run asks
// about, so the batched line can only have it from relate, and a test to itself.
// Anything else it omits, which is what a path with no related test looks like.
const e2eRelateScript = `#!/bin/sh
for p in "$@"; do
  case "$p" in
    src/A.svelte|./src/A.svelte) printf 'src/A.svelte\tsrc/covers-a.test.ts\n' ;;
    src/a.test.ts|./src/a.test.ts) printf 'src/a.test.ts\tsrc/a.test.ts\n' ;;
  esac
done
`

// e2eBatchedLine is what a run over the component and its neighbour test spawns:
// the tests relate named, in first-seen order.
const e2eBatchedLine = "sh scripts/run.sh 'src/covers-a.test.ts' 'src/a.test.ts'"

// journeyRepo is a phase ready to verify with lane declared and granted, its
// scripts committed, and two untracked sources for a run to measure.
func journeyRepo(t *testing.T, lane string) (string, *int) {
	t.Helper()
	dir, calls := fullGreenRepo(t, lane)
	grantLaneLines(t)
	writeScopeFile(t, dir, "scripts/relate.sh", e2eRelateScript)
	writeScopeFile(t, dir, "scripts/run.sh", "#!/bin/sh\nexit 0\n")
	writeScopeFile(t, dir, "src/covers-a.test.ts", "export {}\n")
	mustGit(t, dir, "add", "scripts", "src")
	mustGit(t, dir, "commit", "-qm", "lane scripts and the component's test")
	writeScopeFile(t, dir, "src/A.svelte", "<p>a</p>\n")
	writeScopeFile(t, dir, "src/a.test.ts", "export {}\n")
	return dir, calls
}

// recordSpawns runs every spawn for real and lists its line, bar the repo's
// test_command, which is only listed: a full run here would prove nothing.
func recordSpawns(t *testing.T, dir string) *[]string {
	t.Helper()
	p, err := project.Load(filepath.Join(dir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	lines := new([]string)
	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	spawnLocal = func(at, line string, stdout, stderr io.Writer) error {
		*lines = append(*lines, line)
		if line == p.Runtime.TestCommand {
			return nil
		}
		return orig(at, line, stdout, stderr)
	}
	return lines
}

// judgeCommit asks commit-green about `git add -A && git commit`, as the hook
// would, against the repo as it stands.
func judgeCommit(t *testing.T, dir string) gate.Result {
	t.Helper()
	g, ok := gate.Lookup("commit-green")
	if !ok {
		t.Fatal("commit-green is not registered")
	}
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "s", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "git add -A && git commit -m x"}, "cwd": dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return gate.Check(payload, gate.Env{Home: t.TempDir(), Gates: []gate.Gate{g}})
}

// commitAll makes the commit the gate was asked about.
func commitAll(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "-qm", "the scoped commit")
}

// TestScopedJourneyAdmitsThenVerifyWantsAFullGreen: spelled two ways, the paths
// of a green scoped run reach a commit no full run measured. verify then holds
// the phase to a full green, and a bare run clears it.
func TestScopedJourneyAdmitsThenVerifyWantsAFullGreen(t *testing.T) {
	dir, calls := journeyRepo(t, e2eVouchingLane)
	lines := recordSpawns(t, dir)

	if err := runCmd(t, Test(), "--files", "./src/A.svelte", "--files", "src/a.test.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if !slices.Contains(*lines, e2eBatchedLine) {
		t.Errorf("spawned %q, want the batched line %q", *lines, e2eBatchedLine)
	}
	for _, line := range *lines {
		if strings.HasPrefix(line, "sh scripts/run.sh") && strings.Contains(line, "A.svelte") {
			t.Errorf("the changed path reached the batched line: %q", line)
		}
		if !strings.HasPrefix(line, "sh scripts/") {
			t.Errorf("a scoped run spawned %q, and only the lane's scripts may run", line)
		}
	}
	if g, err := gatestate.LoadGreen(dir); err != nil || g != nil {
		t.Fatalf("green.json = %+v, %v; a scoped run must record no full green", g, err)
	}

	if res := judgeCommit(t, dir); !res.Allowed() {
		t.Fatalf("a commit of exactly what the scoped run covered was refused: %s", res.Text())
	}
	commitAll(t, dir)

	err := runCmd(t, Verify(), fullGreenPhase)
	assertNamesBareTest(t, err)
	assertNotMeasured(t, dir, calls)

	recordFullGreen(t, dir)
	if err := runCmd(t, Verify(), fullGreenPhase); err != nil {
		t.Fatalf("verify on a full green: %v", err)
	}
	assertMeasured(t, dir, calls)
}

// TestScopedJourneyRefusesTheMissedPath: one path of two reaches a test, and the
// commit is refused by the name of the other.
func TestScopedJourneyRefusesTheMissedPath(t *testing.T) {
	dir, _ := journeyRepo(t, e2eVouchingLane)
	writeScopeFile(t, dir, "src/untested.svelte", "<p>b</p>\n")
	recordSpawns(t, dir)

	if err := runCmd(t, Test(), "--files", "src/A.svelte", "--files", "src/a.test.ts", "--files", "src/untested.svelte"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	res := judgeCommit(t, dir)
	if res.Allowed() || !strings.Contains(res.Text(), "src/untested.svelte") {
		t.Fatalf("a commit holding a path no test reached: %q, want a refusal naming it", res.Text())
	}
	if strings.Contains(res.Text(), "src/A.svelte") {
		t.Errorf("the refusal names a covered path: %q", res.Text())
	}
}

// TestNonVouchingJourneyStaysOnTheFullGreen: declaring lanes vouches for
// nothing, and verify is not held to a full green.
func TestNonVouchingJourneyStaysOnTheFullGreen(t *testing.T) {
	dir, calls := journeyRepo(t, e2ePlainLane)
	recordSpawns(t, dir)

	if err := runCmd(t, Test(), "--files", "src/A.svelte", "--files", "src/a.test.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	res := judgeCommit(t, dir)
	if res.Allowed() || !strings.Contains(res.Text(), "no full green") {
		t.Fatalf("a --files green in a non-vouching repo: %q, want the full-green refusal", res.Text())
	}
	if s, err := gatestate.LoadScoped(dir); err != nil || s != nil {
		t.Errorf("scoped.json = %+v, %v; a non-vouching lane records nothing", s, err)
	}

	recordFullGreen(t, dir)
	if res := judgeCommit(t, dir); !res.Allowed() {
		t.Fatalf("a bare green was refused: %s", res.Text())
	}
	commitAll(t, dir)
	if err := os.Remove(filepath.Join(dir, RootDirName, "gate", gatestate.GreenFile)); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Verify(), fullGreenPhase); err != nil {
		t.Fatalf("verify with no green in a non-vouching repo: %v", err)
	}
	assertMeasured(t, dir, calls)
}

// TestRemovingVouchEndsTheAdmission: the tree the scoped run covered, unmoved,
// is refused with the full-green text once the lane stops vouching.
func TestRemovingVouchEndsTheAdmission(t *testing.T) {
	dir, _ := journeyRepo(t, e2eVouchingLane)
	recordSpawns(t, dir)
	if err := runCmd(t, Test(), "--files", "src/A.svelte", "--files", "src/a.test.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if res := judgeCommit(t, dir); !res.Allowed() {
		t.Fatalf("the covered tree was refused before the edit: %s", res.Text())
	}

	f := filepath.Join(dir, RootDirName, project.File)
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, strings.NewReplacer("vouch = true\n", "", "relate = \"sh scripts/relate.sh\"", "").Replace(string(b)))

	res := judgeCommit(t, dir)
	if res.Allowed() || !strings.Contains(res.Text(), "no full green") {
		t.Fatalf("after removing vouch: %q, want the full-green refusal", res.Text())
	}
}
