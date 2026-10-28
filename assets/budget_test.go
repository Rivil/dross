package assets

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// promptByteBudget caps what a base prompt costs to load, checked on the
// embedded file: the bytes every run pays before it fetches any partial
// (locked budget_cap: a fixed 15,000 bytes, not "KB").
var promptByteBudget = map[string]int{
	"prompts/execute.md": 15000,
	"prompts/verify.md":  15000,
}

// budgetProblems lists each budgeted file over its cap, naming the overage.
func budgetProblems(fsys interface{ ReadFile(string) ([]byte, error) }, budget map[string]int) []string {
	var out []string
	for file, limit := range budget {
		b, err := fsys.ReadFile(file)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		if n := len(b); n > limit {
			out = append(out, fmt.Sprintf("%s is %d bytes, %d over its %d-byte budget", file, n, n-limit, limit))
		}
	}
	return out
}

func TestPromptByteBudget(t *testing.T) {
	for _, p := range budgetProblems(FS, promptByteBudget) {
		t.Error(p)
	}
	// The cap is the milestone's, for exactly these two prompts: dropping one
	// silently unbudgets it, and adding one is a scope change of its own.
	var got []string
	for file := range promptByteBudget {
		got = append(got, file)
	}
	sort.Strings(got)
	if want := []string{"prompts/execute.md", "prompts/verify.md"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("budgeted prompts are %v, want exactly %v", got, want)
	}
}

// TestPromptByteBudgetBites: one byte over the cap fails and names the file
// and the overage; exactly at the cap passes.
func TestPromptByteBudgetBites(t *testing.T) {
	budget := map[string]int{"prompts/execute.md": promptByteBudget["prompts/execute.md"]}
	at := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, 15000)}}
	if p := budgetProblems(at, budget); len(p) != 0 {
		t.Errorf("a prompt at its cap failed: %v", p)
	}
	over := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, 15001)}}
	p := budgetProblems(over, budget)
	if want := "prompts/execute.md is 15001 bytes, 1 over its 15000-byte budget"; len(p) != 1 || p[0] != want {
		t.Errorf("15,001 bytes reported %v, want [%q]", p, want)
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
