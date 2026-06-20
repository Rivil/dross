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
