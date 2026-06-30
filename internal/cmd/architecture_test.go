package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// archRepo builds a dross repo with two Go fixtures and returns the repo dir +
// the ARCHITECTURE.md path. Bar lives at foo.go:3; dup.go declares two Dup
// methods (an ambiguous bare name).
func archRepo(t *testing.T) (dir, archPath string) {
	t.Helper()
	dir = t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "foo.go"), "package foo\n\nfunc Bar() {}\n")
	mustWrite(t, filepath.Join(dir, "dup.go"),
		"package foo\n\ntype A struct{}\n\nfunc (A) Dup() {}\n\ntype B struct{}\n\nfunc (B) Dup() {}\n")
	return dir, filepath.Join(dir, "ARCHITECTURE.md")
}

// TestArchitectureCheckFix (c-4) proves the byte-faithful repair: `--fix`
// rewrites only the :line of a Moved bullet, while an Unresolved bullet and an
// Ambiguous bullet are left verbatim (never repointed to a guessed line). The
// expected document is the original with a single :99→:3 substitution — any
// other byte change fails the equality.
func TestArchitectureCheckFix(t *testing.T) {
	_, archPath := archRepo(t)
	orig := "### Feature\n\none line.\n\n" +
		"- `Bar` — `foo.go:99`\n" + // Moved → repointed to :3
		"- `Dup` — `dup.go:5`\n" + // Ambiguous → left verbatim
		"- `Stay` — `foo.go:7`\n\n_x_\n" // Unresolved → left verbatim
	mustWrite(t, archPath, orig)

	if err := runCmd(t, Architecture(), "check", "--fix"); err != nil {
		t.Fatalf("architecture check --fix: %v", err)
	}

	got, err := os.ReadFile(archPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(orig, "foo.go:99", "foo.go:3", 1)
	if string(got) != want {
		t.Errorf("--fix must change only the moved bullet's :line.\n got: %q\nwant: %q", string(got), want)
	}
}

// TestArchitectureCheckNoFixWritesNothing (c-4) proves the report-only path
// never writes the file.
func TestArchitectureCheckNoFixWritesNothing(t *testing.T) {
	_, archPath := archRepo(t)
	orig := "### Feature\n\none line.\n\n- `Bar` — `foo.go:99`\n\n_x_\n"
	mustWrite(t, archPath, orig)

	if err := runCmd(t, Architecture(), "check"); err != nil {
		t.Fatalf("architecture check: %v", err)
	}
	got, err := os.ReadFile(archPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("`check` without --fix must not write; file changed:\n%q", string(got))
	}
}

// TestArchitectureCheckRegistered (c-4) proves the subcommand is wired under a
// root carrying EnforceSubcommandKnown and that `architecture check -h` resolves
// (a known leaf with a RunE, so the guard's parent-RunE never swallows it).
func TestArchitectureCheckRegistered(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Architecture())
	EnforceSubcommandKnown(root)

	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{"architecture", "check", "-h"})
	if err := root.Execute(); err != nil {
		t.Errorf("`architecture check -h` should resolve, got: %v", err)
	}

	// And an unknown architecture subcommand is rejected by the guard.
	root.SetArgs([]string{"architecture", "bogus"})
	if err := root.Execute(); err == nil {
		t.Error("expected an error for an unknown architecture subcommand")
	}
}
