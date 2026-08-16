package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// adoptRepo is a dross repo with an active milestone whose phases array
// already lists `slug` — the state `dross deferred route --target` and
// `dross milestone add phases` leave behind.
func adoptRepo(t *testing.T, slug string, arrayBefore, arrayAfter []string) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	root := filepath.Join(dir, ".dross")
	phases := append(append(append([]string{}, arrayBefore...), slug), arrayAfter...)
	writeMilestoneWithPhases(t, root, "v1.0", phases)
	if err := runCmd(t, State(), "set", "current_milestone", "v1.0"); err != nil {
		t.Fatal(err)
	}
	// The placeholder directory itself.
	if err := os.MkdirAll(filepath.Join(root, "phases", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeMilestoneWithPhases(t *testing.T, root, version string, phases []string) {
	t.Helper()
	quoted := make([]string, 0, len(phases))
	for _, p := range phases {
		quoted = append(quoted, `"`+p+`"`)
	}
	body := "phases = [" + strings.Join(quoted, ", ") + "]\n\n" +
		"[milestone]\n  version = \"" + version + "\"\n  title = \"m\"\n  status = \"active\"\n"
	mustWrite(t, filepath.Join(root, "milestones", version+".toml"), body)
}

// TestCreateAdoptsRatherThanCoining is c-1 through the CLI: the exact incident
// that routed this phase here.
func TestCreateAdoptsRatherThanCoining(t *testing.T) {
	dir := adoptRepo(t, "survivor-drain", nil, nil)

	if err := runCmd(t, Phase(), "create", "Survivor drain"); err != nil {
		t.Fatalf("phase create: %v", err)
	}

	root := filepath.Join(dir, ".dross")
	if _, err := os.Stat(filepath.Join(root, "phases", "survivor-drain-2")); err == nil {
		t.Error("phase create coined survivor-drain-2 — the roadmap entry and the started phase would be two phases apart by one digit")
	}
	m, err := milestone.Load(milestone.FilePath(root, "v1.0"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range m.Phases {
		if strings.HasPrefix(p, "survivor-drain") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the milestone array holds %d survivor-drain entries, want 1: %v", n, m.Phases)
	}
	if !branchExists(t, dir, "phase/survivor-drain") {
		t.Error("no branch was cut for the adopted phase")
	}
	if branchExists(t, dir, "phase/survivor-drain-2") {
		t.Error("a branch was cut for the coined slug")
	}
}

// TestAdoptionKeepsItsMilestoneSlot is c-3. The array is the ordering truth
// every ordinal, version digit and next-phase narration reads — moving an
// adopted phase to the tail renumbers every phase between.
func TestAdoptionKeepsItsMilestoneSlot(t *testing.T) {
	dir := adoptRepo(t, "middle-phase", []string{"first", "second"}, []string{"fourth", "fifth"})

	if err := runCmd(t, Phase(), "create", "Middle phase"); err != nil {
		t.Fatalf("phase create: %v", err)
	}

	m, err := milestone.Load(milestone.FilePath(filepath.Join(dir, ".dross"), "v1.0"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "middle-phase", "fourth", "fifth"}
	if strings.Join(m.Phases, ",") != strings.Join(want, ",") {
		t.Errorf("phases = %v, want %v — an adopted phase must keep the slot the roadmap gave it", m.Phases, want)
	}
}

// TestAdoptionIsReported is c-4: a user who expected a new phase and got an
// existing one must see it, not infer it from a directory that already had
// contents.
func TestAdoptionIsReported(t *testing.T) {
	adoptRepo(t, "survivor-drain", nil, nil)

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "create", "Survivor drain"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	if !strings.Contains(out, "adopted existing") {
		t.Errorf("adoption was silent:\n%s", out)
	}
}

// TestCreateRefusesAStartedPhase is c-2, including that the refusal acts on
// NOTHING. A refusal that had already cut a branch or touched the array leaves
// the repo in a state neither outcome asked for.
func TestCreateRefusesAStartedPhase(t *testing.T) {
	dir := adoptRepo(t, "survivor-drain", nil, nil)
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", "survivor-drain", "spec.toml"),
		"[phase]\n  id = \"survivor-drain\"\n  title = \"Survivor drain\"\n")

	before, err := milestone.Load(milestone.FilePath(root, "v1.0"))
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(t, Phase(), "create", "Survivor drain")
	if err == nil {
		t.Fatal("phase create did not refuse over a started phase")
	}
	for _, want := range []string{"survivor-drain", "already exists", "dross phase checkout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
	if branchExists(t, dir, "phase/survivor-drain") {
		t.Error("the refusal still cut a branch")
	}
	after, err := milestone.Load(milestone.FilePath(root, "v1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(before.Phases, ",") != strings.Join(after.Phases, ",") {
		t.Errorf("the refusal changed the milestone array: %v -> %v", before.Phases, after.Phases)
	}
	if _, err := os.Stat(filepath.Join(root, "phases", "survivor-drain-2")); err == nil {
		t.Error("the refusal created the coined directory anyway")
	}
}

// TestFreshCreateIsUnchanged: adoption is additive. A phase whose slug is free
// must behave exactly as it always did — same report, same branch, same array
// append.
func TestFreshCreateIsUnchanged(t *testing.T) {
	dir := adoptRepo(t, "placeholder", nil, nil)

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "create", "Brand new thing"); err != nil {
		t.Fatalf("phase create: %v", err)
	}
	if !strings.Contains(out, "created ") {
		t.Errorf("a fresh create no longer reports `created`:\n%s", out)
	}
	if strings.Contains(out, "adopted") {
		t.Errorf("a fresh create reported an adoption:\n%s", out)
	}
	if !branchExists(t, dir, "phase/brand-new-thing") {
		t.Error("no branch was cut for the fresh phase")
	}
	m, err := milestone.Load(milestone.FilePath(filepath.Join(dir, ".dross"), "v1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Phases[len(m.Phases)-1] != "brand-new-thing" {
		t.Errorf("a fresh phase did not join the array at the tail: %v", m.Phases)
	}
}
