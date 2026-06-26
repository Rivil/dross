package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// migrateRoot builds a minimal valid .dross root (project.toml passes validate)
// and chdirs into the repo. Returns the .dross path.
func migrateRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	root := filepath.Join(dir, ".dross")
	mustWrite(t, filepath.Join(root, "project.toml"),
		"[project]\n  name = \"p\"\n  version = \"0.1.0.0\"\n\n[runtime]\n  mode = \"native\"\n")
	return root
}

// writePhaseFixture writes a minimal phase dir (spec.toml + plan.toml) carrying
// the given id under root/phases/<dir>.
func writePhaseFixture(t *testing.T, root, dir, id string) {
	t.Helper()
	pd := filepath.Join(root, "phases", dir)
	mustWrite(t, filepath.Join(pd, "spec.toml"),
		"[phase]\n  id = \""+id+"\"\n  title = \"X\"\n\n[[criteria]]\n  id = \"c-1\"\n  text = \"x\"\n")
	mustWrite(t, filepath.Join(pd, "plan.toml"),
		"[phase]\n  id = \""+id+"\"\n")
}

func setCurrentPhase(t *testing.T, root, id string) {
	t.Helper()
	st := &state.State{Version: "0.1.0.0", CurrentPhase: id}
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree records every file's bytes under root, keyed by relative path.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPhaseMigrate(t *testing.T) {
	root := migrateRoot(t)
	writePhaseFixture(t, root, "01-foo", "01-foo")
	writePhaseFixture(t, root, "02-bar", "02-bar")
	mustWrite(t, filepath.Join(root, "milestones", "v0.1.toml"),
		"phases = [\"01-foo\", \"02-bar\"]\n\n[milestone]\n  version = \"v0.1\"\n")
	setCurrentPhase(t, root, "02-bar") // in-flight phase

	if err := runCmd(t, Phase(), "migrate"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 01-foo migrated to bare slug; the in-flight 02-bar is left untouched.
	if !isDir(filepath.Join(root, "phases", "foo")) {
		t.Error("expected phases/foo after migrate")
	}
	if isDir(filepath.Join(root, "phases", "01-foo")) {
		t.Error("phases/01-foo should be gone")
	}
	if !isDir(filepath.Join(root, "phases", "02-bar")) {
		t.Error("in-flight phases/02-bar should be untouched")
	}
	// [phase].id rewritten in both files of foo; the in-flight phase keeps its id.
	if sp, _ := phase.LoadSpec(filepath.Join(root, "phases", "foo", "spec.toml")); sp.Phase.ID != "foo" {
		t.Errorf("foo spec id: got %q want foo", sp.Phase.ID)
	}
	if pl, _ := phase.LoadPlan(filepath.Join(root, "phases", "foo", "plan.toml")); pl.Phase.ID != "foo" {
		t.Errorf("foo plan id: got %q want foo", pl.Phase.ID)
	}
	if pl, _ := phase.LoadPlan(filepath.Join(root, "phases", "02-bar", "plan.toml")); pl.Phase.ID != "02-bar" {
		t.Errorf("in-flight plan id should be unchanged: got %q", pl.Phase.ID)
	}
	// milestone array: 01-foo -> foo, 02-bar (in-flight) kept verbatim.
	m, _ := milestone.Load(milestone.FilePath(root, "v0.1"))
	if len(m.Phases) != 2 || m.Phases[0] != "foo" || m.Phases[1] != "02-bar" {
		t.Errorf("milestone array: got %v want [foo 02-bar]", m.Phases)
	}
	// state.current_phase unchanged.
	if s2, _ := state.Load(filepath.Join(root, state.File)); s2.CurrentPhase != "02-bar" {
		t.Errorf("current_phase: got %q want 02-bar", s2.CurrentPhase)
	}
	// The migrated tree validates — proves the plan.id rewrite kept dir/id in sync.
	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("validate should pass on migrated tree: %v", err)
	}
}

func TestPhaseMigrateIdempotent(t *testing.T) {
	root := migrateRoot(t)
	writePhaseFixture(t, root, "01-foo", "01-foo")
	writePhaseFixture(t, root, "02-bar", "02-bar")
	mustWrite(t, filepath.Join(root, "milestones", "v0.1.toml"),
		"phases = [\"01-foo\", \"02-bar\"]\n\n[milestone]\n  version = \"v0.1\"\n")
	setCurrentPhase(t, root, "")

	if err := runCmd(t, Phase(), "migrate"); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	before := snapshotTree(t, root)
	if err := runCmd(t, Phase(), "migrate"); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	after := snapshotTree(t, root)

	if len(before) != len(after) {
		t.Fatalf("file set changed on re-run: %d -> %d files", len(before), len(after))
	}
	for path, b := range before {
		if a, ok := after[path]; !ok || a != b {
			t.Errorf("re-run changed %s (idempotency broken)", path)
		}
	}
}

func TestPhaseMigrateDisambiguatesCollidingSlugs(t *testing.T) {
	root := migrateRoot(t)
	writePhaseFixture(t, root, "01-foo", "01-foo")
	writePhaseFixture(t, root, "03-foo", "03-foo")
	mustWrite(t, filepath.Join(root, "milestones", "v0.1.toml"),
		"phases = [\"01-foo\", \"03-foo\"]\n\n[milestone]\n  version = \"v0.1\"\n")
	setCurrentPhase(t, root, "")

	if err := runCmd(t, Phase(), "migrate"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !isDir(filepath.Join(root, "phases", "foo")) || !isDir(filepath.Join(root, "phases", "foo-2")) {
		t.Error("expected both foo and foo-2 after disambiguation")
	}
	m, _ := milestone.Load(milestone.FilePath(root, "v0.1"))
	if len(m.Phases) != 2 || m.Phases[0] != "foo" || m.Phases[1] != "foo-2" {
		t.Errorf("milestone array should remap distinctly: got %v want [foo foo-2]", m.Phases)
	}
}

func TestPhaseMigrateRefusesPreexistingCollision(t *testing.T) {
	root := migrateRoot(t)
	writePhaseFixture(t, root, "01-foo", "01-foo")
	writePhaseFixture(t, root, "foo", "foo") // pre-existing, unrelated bare dir
	setCurrentPhase(t, root, "")

	err := runCmd(t, Phase(), "migrate")
	if err == nil {
		t.Fatal("expected migrate to refuse when the target slug already exists")
	}
	// The legacy dir must be left intact — nothing clobbered.
	if !isDir(filepath.Join(root, "phases", "01-foo")) {
		t.Error("01-foo should be untouched after a refused migrate")
	}
}
