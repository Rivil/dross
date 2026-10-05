package phase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanCorpusOneLineStatusFlip runs every plan.toml this repo carries
// through Plan.Save with one change — the first task's status flipped — and
// requires the file to differ by exactly that: one changed line, or one
// inserted line where the task had no status key. Whatever else the real
// plans hold (hand edits, comments, older encoder layouts) survives.
func TestPlanCorpusOneLineStatusFlip(t *testing.T) {
	plans, err := filepath.Glob(filepath.Join("..", "..", ".dross", "phases", "*", "plan.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) < 50 {
		t.Fatalf("found %d plans, want the repo's corpus — the glob is not reaching .dross/phases", len(plans))
	}
	for _, src := range plans {
		name := filepath.Base(filepath.Dir(src))
		t.Run(name, func(t *testing.T) {
			orig, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "plan.toml")
			if err := os.WriteFile(path, orig, 0o644); err != nil {
				t.Fatal(err)
			}
			p, err := LoadPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Task) == 0 {
				t.Skip("no tasks")
			}
			flipped := "done"
			if p.Task[0].Status == "done" {
				flipped = "pending"
			}
			p.Task[0].Status = flipped
			if err := p.Save(path); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ol, gl := strings.Split(string(orig), "\n"), strings.Split(string(got), "\n")
			switch len(gl) - len(ol) {
			case 0:
				var changed []string
				for i := range ol {
					if ol[i] != gl[i] {
						changed = append(changed, gl[i])
					}
				}
				if len(changed) != 1 || !strings.Contains(changed[0], `status = "`+flipped+`"`) {
					t.Errorf("changed lines = %q, want only the first task's status", changed)
				}
			case 1:
				// The one inserted line must be the status, and cutting it
				// back out must give the original bytes.
				at := -1
				for i := range ol {
					if ol[i] != gl[i] {
						at = i
						break
					}
				}
				if at < 0 {
					at = len(ol)
				}
				if !strings.Contains(gl[at], `status = "`+flipped+`"`) {
					t.Errorf("the inserted line %q is not the status", gl[at])
				}
				if back := strings.Join(append(append([]string{}, gl[:at]...), gl[at+1:]...), "\n"); back != string(orig) {
					t.Error("cutting the inserted status line out does not restore the original")
				}
			default:
				t.Errorf("line count changed by %d, want 0 or 1", len(gl)-len(ol))
			}
			back, err := LoadPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			if back.Task[0].Status != flipped || len(back.Task) != len(p.Task) {
				t.Errorf("reloaded plan: first status %q, %d tasks; want %q, %d", back.Task[0].Status, len(back.Task), flipped, len(p.Task))
			}
		})
	}
}
