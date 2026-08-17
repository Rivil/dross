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

// startedRepo is adoptRepo plus the markers that make CreateSlug read the slug
// as SlugOccupied rather than an empty placeholder — i.e. real work in flight.
func startedRepo(t *testing.T, slug string) string {
	t.Helper()
	dir := adoptRepo(t, slug, nil, nil)
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "phases", slug, "spec.toml"),
		"[phase]\n  id = \""+slug+"\"\n  title = \""+slug+"\"\n")
	return dir
}

// TestCreateStillRefusesStartedSlugWithoutAdopt: the default stays a refusal.
// Silently retitling work in flight is exactly what phase-create-adoption
// removed, and --adopt must be an opt-in rather than a softening.
func TestCreateStillRefusesStartedSlugWithoutAdopt(t *testing.T) {
	startedRepo(t, "started-phase")
	err := runCmd(t, Phase(), "create", "started phase")
	if err == nil {
		t.Fatal("expected a refusal for a slug that already holds a spec")
	}
	if !strings.Contains(err.Error(), "already exists and has been started") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRefusalNamesAdoptFlag: an error that does not say how to proceed is the
// reason people go back to hand-editing.
func TestRefusalNamesAdoptFlag(t *testing.T) {
	startedRepo(t, "started-phase")
	err := runCmd(t, Phase(), "create", "started phase")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "--adopt") {
		t.Errorf("the refusal must name --adopt as the way through:\n%v", err)
	}
}

func TestAdoptTakesOverStartedPhase(t *testing.T) {
	dir := startedRepo(t, "started-phase")
	out := captureStdout(t, func() {
		if err := runCmd(t, Phase(), "create", "started phase", "--adopt"); err != nil {
			t.Fatalf("create --adopt: %v", err)
		}
	})
	if !strings.Contains(out, "adopted existing") {
		t.Errorf("adoption must be narrated as such, got:\n%s", out)
	}
	// The spec it found is still the spec it has: adoption takes work over, it
	// does not start it again.
	body := mustRead(t, filepath.Join(dir, ".dross", "phases", "started-phase", "spec.toml"))
	if !strings.Contains(body, "started-phase") {
		t.Errorf("the adopted spec was rewritten: %s", body)
	}
}

// TestAdoptChecksOutExistingBranch: the branch is where the adopted phase's
// commits live. forkPhaseBranch refuses when it exists, and re-forking would
// abandon them.
func TestAdoptChecksOutExistingBranch(t *testing.T) {
	dir := startedRepo(t, "started-phase")
	mustGit(t, dir, "branch", "phase/started-phase")

	if err := runCmd(t, Phase(), "create", "started phase", "--adopt"); err != nil {
		t.Fatalf("create --adopt with an existing branch: %v", err)
	}
	got := strings.TrimSpace(gitOutT(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	if got != "phase/started-phase" {
		t.Errorf("HEAD = %s, want phase/started-phase — adoption must enter the branch", got)
	}
}

// TestAdoptPreservesRecordedFork: the fork point scopes the phase's mutation
// diff. Rewriting it to today's tip drops every commit the phase already made
// out of its own scope, which is a silent loss of coverage.
func TestAdoptPreservesRecordedFork(t *testing.T) {
	dir := startedRepo(t, "started-phase")
	mustGit(t, dir, "branch", "phase/started-phase")
	changesPath := filepath.Join(dir, ".dross", "phases", "started-phase", "changes.json")
	mustWrite(t, changesPath, `{"phase":"started-phase","forked_from":{"base":"main","sha":"deadbeef"}}`)
	before := mustRead(t, changesPath)

	if err := runCmd(t, Phase(), "create", "started phase", "--adopt"); err != nil {
		t.Fatalf("create --adopt: %v", err)
	}
	if after := mustRead(t, changesPath); after != before {
		t.Errorf("the recorded fork point was rewritten:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// TestAdoptPreservesPhaseStatus: a phase holding a plan must not read
// "created" afterwards — that is a lie about how far the work got, and the
// status carried in state belongs to whichever phase was current before.
func TestAdoptPreservesPhaseStatus(t *testing.T) {
	dir := startedRepo(t, "started-phase")
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "started-phase", "plan.toml"),
		"task_seq = 1\n\n[phase]\n  id = \"started-phase\"\n")

	if err := runCmd(t, Phase(), "create", "started phase", "--adopt"); err != nil {
		t.Fatalf("create --adopt: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, State(), "get", "current_phase_status"); err != nil {
			t.Fatal(err)
		}
	})
	if got := strings.TrimSpace(out); got != "planned" {
		t.Errorf("current_phase_status = %q, want planned — the phase has a plan", got)
	}
}

// TestAdoptRefusesShippedPhase: taking over a phase whose branch is live on the
// remote is the identical hazard `phase rename` refuses. Without this, --adopt
// is the way around that guard.
func TestAdoptRefusesShippedPhase(t *testing.T) {
	dir := startedRepo(t, "started-phase")
	mustGit(t, dir, "branch", "phase/started-phase")
	mustGit(t, dir, "push", "-q", "origin", "phase/started-phase")

	err := runCmd(t, Phase(), "create", "started phase", "--adopt")
	if err == nil {
		t.Fatal("expected a refusal for a phase with a live origin branch")
	}
	if !strings.Contains(err.Error(), "live origin branch") {
		t.Errorf("unexpected error: %v", err)
	}
}
