package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/quality"
)

// TestQualityFindingsRegistered: `dross quality findings` is on the tree.
func TestQualityFindingsRegistered(t *testing.T) {
	var found bool
	for _, c := range Quality().Commands() {
		if c.Name() == "findings" {
			found = true
		}
	}
	if !found {
		t.Fatal("dross quality is missing the `findings` subcommand")
	}
}

// TestQualityItemUsesDimension: the adapter keys the fingerprint on Dimension,
// not Risk. Two findings with the same file+title but different dimension must
// not collide — which they would if the adapter fed Risk (here identical).
func TestQualityItemUsesDimension(t *testing.T) {
	led := quality.Ledger{Findings: []quality.Finding{
		{ID: "f-1", Dimension: quality.Complexity, Risk: quality.RiskHigh, File: "x.go", Title: "tangled func"},
		{ID: "f-2", Dimension: quality.Duplication, Risk: quality.RiskHigh, File: "x.go", Title: "tangled func"},
	}}
	items := led.Items()
	if len(items) != 2 {
		t.Fatalf("Items() = %d, want 2", len(items))
	}
	if items[0].Class != string(quality.Complexity) {
		t.Errorf("Item.Class = %q, want the finding's Dimension (not Risk)", items[0].Class)
	}
	if items[0].Fingerprint() == items[1].Fingerprint() {
		t.Fatal("two findings of different dimension but same file+title collided — adapter is keying on Risk, not Dimension")
	}
}

// TestQualityStateGitignored: .dross/quality/state.toml is ignored by git.
func TestQualityStateGitignored(t *testing.T) {
	root := moduleRoot(t)
	cmd := exec.Command("git", "-C", root, "check-ignore", ".dross/quality/state.toml")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/quality/state.toml is NOT ignored (err=%v); "+
			"durable findings state must be gitignored alongside the run artifacts", err)
	}
}

// TestQualityFoldSurvivesRerun: c-3 end-to-end — dismiss a finding via
// `findings <id> --state dismissed`, then `findings reconcile <run-dir>` over a
// fresh run containing the same finding folds it (not relisted as new). Exercises
// the real quality descriptor: ResolveID off the latest run, then reconcile.
func TestQualityFoldSurvivesRerun(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	runDir := filepath.Join(dir, ".dross", "quality", "20260627T120000-abcdef0")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	led := quality.Ledger{Findings: []quality.Finding{
		{ID: "f-1", Dimension: quality.Complexity, Risk: quality.RiskHigh, File: "a.go", Title: "cyclomatic spike"},
	}}
	if err := quality.Save(filepath.Join(runDir, "findings.toml"), led); err != nil {
		t.Fatal(err)
	}

	// Dismiss f-1 (resolved off the latest run's ledger).
	if err := runCmd(t, Quality(), "findings", "f-1", "--state", "dismissed"); err != nil {
		t.Fatalf("dismiss f-1: %v", err)
	}

	// Re-run reconcile over the same run dir: the dismissed finding folds.
	out := captureStdout(t, func() {
		if err := runCmd(t, Quality(), "findings", "reconcile", runDir); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	})
	if !strings.Contains(out, "0 new") || !strings.Contains(out, "1 folded") {
		t.Errorf("dismissed finding was not folded on re-run:\n%s", out)
	}
}
