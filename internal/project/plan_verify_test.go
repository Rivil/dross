package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

// TestPlanSaveRefusesUnverifiedPatch drives a verify mismatch through the real
// plan.toml writer: Plan.Save, the call every `dross task` verb ends in, given
// a keyed patch that would not load as the plan it saves. The save is refused,
// the error names plan.toml, and the file keeps every byte.
func TestPlanSaveRefusesUnverifiedPatch(t *testing.T) {
	src := "# a hand-written header\ntask_seq = 2\n\n[phase]\n  id = \"p\"\n\n[[task]]\n  id = \"t-1\"\n  wave = 1\n  title = \"one\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-2\"\n  wave = 1\n  title = \"two\"\n  status = \"pending\"\n"
	path := filepath.Join(t.TempDir(), "plan.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := phase.LoadPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Task[1].Status = "done"

	restore := project.CorruptKeyedOpsForTest()
	defer restore()

	err = p.Save(path)
	if err == nil {
		t.Fatal("a patch that does not load as the plan was written")
	}
	if !strings.Contains(err.Error(), "plan.toml") || !strings.Contains(err.Error(), "left untouched") {
		t.Errorf("err = %v, want plan.toml named and the file left untouched", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != src {
		t.Errorf("plan.toml changed on a refused save:\n%s", got)
	}
}
