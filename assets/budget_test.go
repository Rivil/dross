package assets

import (
	"fmt"
	"testing"
	"testing/fstest"
)

// promptByteBudget caps what a base prompt costs to load, checked on the
// embedded file: the bytes every run pays before it fetches any partial
// (locked budget_cap: a fixed 15,000 bytes, not "KB").
var promptByteBudget = map[string]int{
	"prompts/execute.md": 15000,
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
}

// TestPromptByteBudgetBites: one byte over the cap fails and names the file
// and the overage; exactly at the cap passes.
func TestPromptByteBudgetBites(t *testing.T) {
	at := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, 15000)}}
	if p := budgetProblems(at, promptByteBudget); len(p) != 0 {
		t.Errorf("a prompt at its cap failed: %v", p)
	}
	over := fstest.MapFS{"prompts/execute.md": {Data: make([]byte, 15001)}}
	p := budgetProblems(over, promptByteBudget)
	if want := "prompts/execute.md is 15001 bytes, 1 over its 15000-byte budget"; len(p) != 1 || p[0] != want {
		t.Errorf("15,001 bytes reported %v, want [%q]", p, want)
	}
}
