package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/findings"
)

// chdirDross creates a temp dir with a .dross subdir, chdirs into it so
// FindRoot succeeds, and returns the .dross root path.
func chdirDross(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	return root
}

func fakeTool(statePath string, items []findings.Item, runID string, resolve map[string]findings.Item) FindingsTool {
	return FindingsTool{
		Name:        "security",
		StatePath:   func(string) string { return statePath },
		ItemsForRun: func(string) ([]findings.Item, string, error) { return items, runID, nil },
		ResolveID: func(_, id string) (findings.Item, error) {
			it, ok := resolve[id]
			if !ok {
				return findings.Item{}, fmt.Errorf("no finding %q", id)
			}
			return it, nil
		},
	}
}

// TestFindingsStateFlagRejectsUnknown: an unknown --state value is refused
// before anything is persisted.
func TestFindingsStateFlagRejectsUnknown(t *testing.T) {
	tool := fakeTool("", nil, "", map[string]findings.Item{"f-1": {Class: "sec", File: "a.go", Title: "x"}})
	if err := runCmd(t, newFindingsCmd(tool), "f-1", "--state", "bogus"); err == nil {
		t.Fatal("`findings f-1 --state bogus` was accepted; want a validation error")
	}
}

// TestFindingsSetStatePersistsByFingerprint: setting a state via the per-run id
// persists it under that finding's fingerprint.
func TestFindingsSetStatePersistsByFingerprint(t *testing.T) {
	chdirDross(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.toml")
	item := findings.Item{Class: "sec", File: "a.go", Title: "hardcoded secret"}
	tool := fakeTool(statePath, nil, "", map[string]findings.Item{"f-1": item})

	if err := runCmd(t, newFindingsCmd(tool), "f-1", "--state", "resolved"); err != nil {
		t.Fatalf("set state: %v", err)
	}
	store, err := findings.LoadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := store.Get(item.Fingerprint())
	if !ok || rec.State != findings.StateResolved {
		t.Fatalf("state not persisted by fingerprint: got %+v ok=%v", rec, ok)
	}
}

// TestFindingsListRendersStateAndRegressed: `list` shows each finding's state
// and marks regressed ones.
func TestFindingsListRendersStateAndRegressed(t *testing.T) {
	chdirDross(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.toml")
	seed := &findings.Store{Records: []findings.Record{
		{Fingerprint: "aaa", State: findings.StateDismissed, Title: "accepted risk", File: "a.go"},
		{Fingerprint: "bbb", State: findings.StateResolved, Regressed: true, Title: "came back", File: "b.go"},
	}}
	if err := findings.SaveStore(statePath, seed); err != nil {
		t.Fatal(err)
	}
	tool := fakeTool(statePath, nil, "", nil)

	out := captureStdout(t, func() {
		if err := runCmd(t, newFindingsCmd(tool), "list"); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	if !strings.Contains(out, "dismissed") {
		t.Errorf("list omitted the dismissed state:\n%s", out)
	}
	if !strings.Contains(out, "resolved") || !strings.Contains(out, "REGRESSED") {
		t.Errorf("list omitted the resolved state or regressed marker:\n%s", out)
	}
}

// TestFindingsReconcileSubcommand: `reconcile <run-dir>` folds a prior dismissed
// finding end-to-end through the descriptor — it is not relisted as new.
func TestFindingsReconcileSubcommand(t *testing.T) {
	chdirDross(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.toml")
	item := findings.Item{Class: "sec", File: "a.go", Title: "hardcoded secret"}
	seed := &findings.Store{Records: []findings.Record{
		{Fingerprint: item.Fingerprint(), State: findings.StateDismissed, Title: item.Title, File: item.File, Class: item.Class},
	}}
	if err := findings.SaveStore(statePath, seed); err != nil {
		t.Fatal(err)
	}
	tool := fakeTool(statePath, []findings.Item{item}, "run9", nil)

	out := captureStdout(t, func() {
		if err := runCmd(t, newFindingsCmd(tool), "reconcile", "/any/run-dir"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	})
	if !strings.Contains(out, "0 new") || !strings.Contains(out, "1 folded") {
		t.Errorf("reconcile did not fold the dismissed finding:\n%s", out)
	}
	store, err := findings.LoadStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := store.Get(item.Fingerprint()); g.State != findings.StateDismissed {
		t.Fatalf("dismissed finding flipped to %q after reconcile", g.State)
	}
}
