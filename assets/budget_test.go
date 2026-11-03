package assets

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// promptByteCap caps what every base prompt costs to load, checked on the
// embedded file: the bytes each run pays before it fetches any partial. One
// number for every prompts/*.md — no per-file overrides (locked no_exceptions)
// — and partials (_*.md) are exempt.
const promptByteCap = 15000

// budgetProblems globs prompts/*.md in fsys, skips `_`-prefixed partials and
// playbooks, and lists each base prompt over promptByteCap with its overage.
// It also returns the base prompts it visited, so a glob that silently
// matches nothing is caught.
func budgetProblems(fsys fs.FS) (problems, visited []string) {
	files, err := fs.Glob(fsys, "prompts/*.md")
	if err != nil {
		return []string{fmt.Sprintf("glob prompts/*.md: %v", err)}, nil
	}
	sort.Strings(files)
	for _, file := range files {
		if strings.HasPrefix(path.Base(file), "_") {
			continue
		}
		visited = append(visited, file)
		b, err := fs.ReadFile(fsys, file)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		if n := len(b); n > promptByteCap {
			problems = append(problems, fmt.Sprintf("%s is %d bytes, %d over the %d-byte cap", file, n, n-promptByteCap, promptByteCap))
		}
	}
	return problems, visited
}

// TestPromptByteBudget runs the cap over every embedded base prompt, one
// subtest per file, and fails if the glob found too few to be the real tree.
func TestPromptByteBudget(t *testing.T) {
	problems, visited := budgetProblems(FS)
	if len(visited) < 24 || !contains(visited, "prompts/execute.md") {
		t.Fatalf("budget visited %d base prompts %v; want at least 24, execute.md among them", len(visited), visited)
	}
	for _, file := range visited {
		t.Run(path.Base(file), func(t *testing.T) {
			for _, p := range problems {
				if strings.HasPrefix(p, file+" ") || strings.HasPrefix(p, file+":") {
					t.Error(p)
				}
			}
		})
	}
}

// TestPromptByteBudgetBites: one byte over the cap fails and names the file
// and the overage; exactly at the cap passes.
func TestPromptByteBudgetBites(t *testing.T) {
	at := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, promptByteCap)}}
	if p, _ := budgetProblems(at); len(p) != 0 {
		t.Errorf("a prompt at the cap failed: %v", p)
	}
	over := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, promptByteCap+1)}}
	p, _ := budgetProblems(over)
	if want := "prompts/execute.md is 15001 bytes, 1 over the 15000-byte cap"; len(p) != 1 || p[0] != want {
		t.Errorf("15,001 bytes reported %v, want [%q]", p, want)
	}
}

// TestPromptByteBudgetEnumerates: the cap reaches every base prompt by glob —
// a brand-new prompt, each of the cut prompts (no override survives), and an
// uncut one like milestone.md — while partials and the playbook stay exempt.
func TestPromptByteBudgetEnumerates(t *testing.T) {
	over := func(names ...string) fstest.MapFS {
		m := fstest.MapFS{}
		for _, n := range names {
			m["prompts/"+n] = &fstest.MapFile{Data: make([]byte, promptByteCap+1)}
		}
		return m
	}
	for _, c := range []struct {
		name  string
		files []string
	}{
		{"new prompt", []string{"brand-new.md"}},
		{"no overrides", []string{"options.md", "ship.md", "plan.md", "spec.md", "quick.md", "execute.md", "verify.md"}},
		{"uncut prompt", []string{"milestone.md"}},
	} {
		p, _ := budgetProblems(over(c.files...))
		if len(p) != len(c.files) {
			t.Errorf("%s: %d files at 15,001 B reported %v, want %d problems", c.name, len(c.files), p, len(c.files))
		}
	}
	partials := fstest.MapFS{
		"prompts/_options.lanes.md": {Data: make([]byte, 40000)},
		"prompts/_interaction.md":   {Data: make([]byte, 40000)},
	}
	if p, visited := budgetProblems(partials); len(p) != 0 || len(visited) != 0 {
		t.Errorf("40,000-byte partials reported %v (visited %v), want nothing", p, visited)
	}
}

// TestVerifySoloReviewMarksUnresolved: moving the solo-review block out of
// base verify.md must keep the marker that tells the user a reviewer-failed
// task's findings went unresolved — and it must still land in §4's report.
func TestVerifySoloReviewMarksUnresolved(t *testing.T) {
	exp, err := Expand(FS, "verify")
	if err != nil {
		t.Fatal(err)
	}
	s := string(exp)
	i, j := strings.Index(s, "## 4. Surface to user"), strings.Index(s, "## 5. Wrap")
	if i < 0 || j < i {
		t.Fatal("expanded verify.md lost its §4/§5 headings")
	}
	if !strings.Contains(s[i:j], "exhausted after 2 rounds — UNRESOLVED") {
		t.Error("expanded verify.md §4 lost the solo review's UNRESOLVED marker")
	}
}
