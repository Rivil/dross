package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/security"
)

// TestSecurityFindingsRegistered: `dross security findings` is on the tree.
func TestSecurityFindingsRegistered(t *testing.T) {
	var found bool
	for _, c := range Security().Commands() {
		if c.Name() == "findings" {
			found = true
		}
	}
	if !found {
		t.Fatal("dross security is missing the `findings` subcommand")
	}
}

// TestSecurityItemUsesClass: the adapter keys the fingerprint on Class, not
// Severity. Two findings with the same file+title but different class must not
// collide — which they would if the adapter fed Severity (here identical).
func TestSecurityItemUsesClass(t *testing.T) {
	led := security.Ledger{Findings: []security.Finding{
		{ID: "f-1", Class: "injection", Severity: security.SeverityHigh, File: "x.go", Title: "tainted exec"},
		{ID: "f-2", Class: "path-traversal", Severity: security.SeverityHigh, File: "x.go", Title: "tainted exec"},
	}}
	items := led.Items()
	if len(items) != 2 {
		t.Fatalf("Items() = %d, want 2", len(items))
	}
	if items[0].Class != "injection" {
		t.Errorf("Item.Class = %q, want the finding's Class (not Severity)", items[0].Class)
	}
	if items[0].Fingerprint() == items[1].Fingerprint() {
		t.Fatal("two findings of different class but same file+title collided — adapter is keying on Severity, not Class")
	}
}

// TestSecurityStateGitignored: .dross/security/state.toml is ignored by git, so
// durable findings state never pre-discloses on a public repo.
func TestSecurityStateGitignored(t *testing.T) {
	root := moduleRoot(t)
	cmd := exec.Command("git", "-C", root, "check-ignore", ".dross/security/state.toml")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git check-ignore reports .dross/security/state.toml is NOT ignored (err=%v); "+
			"durable findings state must be gitignored alongside the run artifacts", err)
	}
}

// moduleRoot walks up from the test's working dir to the module root (the dir
// holding go.mod), so git operations resolve against the real repo .gitignore.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}
