package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnboardSeedsRuntimeFromProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	if err := runCmd(t, Onboard()); err != nil {
		t.Fatalf("onboard: %v", err)
	}
	p := loadInitedProject(t, dir)

	if p.Stack.Profile != "go" {
		t.Errorf("[stack].profile = %q, want \"go\"", p.Stack.Profile)
	}
	if p.Runtime.TestCommand != "go test -count=1 ./..." {
		t.Errorf("[runtime].test = %q, want the Go profile value", p.Runtime.TestCommand)
	}
	// onboard's own scan still owns mode; seeding must not clobber it.
	if p.Runtime.Mode == "" {
		t.Error("onboard runtime mode was lost when seeding from the profile")
	}
}

// TestOnboardCover_ForceRemovesAndRecreates exercises onboard.go:41, the
// `if err := os.RemoveAll(root); err != nil` guard in the --force branch.
// RemoveAll succeeds (err == nil), so the real code falls through and
// re-scaffolds .dross. A negated guard (err == nil) would return early after
// wiping, leaving no project.toml — which this asserts against.
func TestOnboardCover_ForceRemovesAndRecreates(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	// Pre-existing .dross with a sentinel that --force must wipe.
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, ".dross", "sentinel")
	if err := os.WriteFile(sentinel, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Onboard(), "--force"); err != nil {
		t.Fatalf("onboard --force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "project.toml")); err != nil {
		t.Errorf("project.toml should exist after --force onboard: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("sentinel should have been wiped by --force: err=%v", err)
	}
}

// TestOnboardCover_NativeDevCommandFromPM exercises onboard.go:177,
// `dev = pm + " dev"`, reached only for native mode with a package manager.
// It pins the exact concatenated value so an arithmetic-op mutation of the
// `+` (or any other change to the operands) is observable.
func TestOnboardCover_NativeDevCommandFromPM(t *testing.T) {
	got := scanResult{hasNPM: true}.toProject()
	if got.Runtime.Mode != "native" {
		t.Fatalf("mode = %q, want native", got.Runtime.Mode)
	}
	if got.Runtime.DevCommand != "npm dev" {
		t.Errorf("dev_command = %q, want %q (pm + \" dev\")", got.Runtime.DevCommand, "npm dev")
	}
}
