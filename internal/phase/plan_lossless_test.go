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

// preconditionPlan carries precondition blocks in both forms, with comments
// inside and between them, so a status save has something to disturb.
const preconditionPlan = `[phase]
id = "01-test"

[[task]]
id = "t-1"
wave = 1
title = "first"
files = ["a.go"]
covers = ["c-1"]
status = "pending"

# the build must stay green before t-1 starts
[[task.preconditions]]
run = "go build ./..."   # exit 0 is enough

[[task.preconditions]]
run = "go test ./x | wc -l"
max = 3
timeout = "90s"
why = "no more than three failing lines"

[[task]]
id = "t-2"
wave = 1
title = "second"
files = ["b.go"]
covers = ["c-1"]
preconditions = [{ run = "true", min = 1 }]
`

// TestStatusSaveLeavesPreconditionBlocksAlone: SetTaskStatus + Save on tasks
// carrying preconditions — header form with comments, and inline form —
// changes exactly the one status line (or inserts it where there was none),
// and the preconditions reload unchanged.
func TestStatusSaveLeavesPreconditionBlocksAlone(t *testing.T) {
	for _, c := range []struct {
		task      string
		inserted  bool
		wantLines []string
	}{
		{"t-1", false, []string{`status = "in_progress"`}},
		{"t-2", true, []string{`status = "in_progress"`}},
	} {
		t.Run(c.task, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plan.toml")
			if err := os.WriteFile(path, []byte(preconditionPlan), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := LoadPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			p, err := LoadPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			p.SetTaskStatus(c.task, StatusInProgress)
			if err := p.Save(path); err != nil {
				t.Fatalf("save: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ol, gl := strings.Split(preconditionPlan, "\n"), strings.Split(string(got), "\n")
			var diff []string
			if c.inserted {
				if len(gl) != len(ol)+1 {
					t.Fatalf("got %d lines, want one inserted:\n%s", len(gl), got)
				}
				for i, j := 0, 0; i < len(gl); i++ {
					if j < len(ol) && gl[i] == ol[j] {
						j++
						continue
					}
					diff = append(diff, gl[i])
				}
			} else {
				if len(gl) != len(ol) {
					t.Fatalf("got %d lines, want %d:\n%s", len(gl), len(ol), got)
				}
				for i := range ol {
					if ol[i] != gl[i] {
						diff = append(diff, gl[i])
					}
				}
			}
			if strings.Join(diff, "\n") != strings.Join(c.wantLines, "\n") {
				t.Errorf("status save changed %q, want exactly %q:\n%s", diff, c.wantLines, got)
			}
			after, err := LoadPlan(path)
			if err != nil {
				t.Fatal(err)
			}
			if PreconditionSetLine(after) != PreconditionSetLine(before) {
				t.Errorf("preconditions moved on a status save:\n%s\n%s", PreconditionSetLine(before), PreconditionSetLine(after))
			}
		})
	}
}
