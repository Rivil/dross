package cmd

// Docs coverage for both install surfaces.
//
// This file exists because the sweep that would otherwise catch the drift walks
// localKeys, and `trusted_lane_installs` is deliberately absent from it — so
// nothing else in this repo would notice options.md never mentioning the grant.
// A verb the docs do not name is one nobody finds.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// docsBody reads one repo doc from the module root, which is where the tests'
// working directory is not.
func docsBody(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{repoRootForDocs(t)}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestReadmeDocumentsTheLaneInstallVerb: the README table is the surface a
// reader scans first, and both flags change what the verb does to a machine.
func TestReadmeDocumentsTheLaneInstallVerb(t *testing.T) {
	body := docsBody(t, "README.md")
	// Located by the row's own heading, not by the verb's name: the remote row
	// now cross-references `dross test lane install` among the surfaces that
	// must agree about the host, and matching on that would assert against it.
	row := readmeRowContaining(t, body, "dross test lane {")
	for _, want := range []string{"--apply", "--local"} {
		if !strings.Contains(row, want) {
			t.Errorf("the `dross test lane install` row does not name %s", want)
		}
	}
}

// TestReadmeDocumentsTheInstallGrant: a grant nobody knows exists is a refusal
// nobody can act on.
func TestReadmeDocumentsTheInstallGrant(t *testing.T) {
	body := docsBody(t, "README.md")
	row := readmeRowContaining(t, body, "`dross trust [")
	if !strings.Contains(row, "--lane-install") {
		t.Errorf("the `dross trust` row does not name --lane-install:\n%s", row)
	}
}

// TestReadmeDocumentsBootstrapsLaneCoverage is c-2 at the docs layer: a reader
// told bootstrap covers adapters alone still believes lane readiness is a
// second question they have to ask somewhere else.
func TestReadmeDocumentsBootstrapsLaneCoverage(t *testing.T) {
	body := docsBody(t, "README.md")
	row := readmeRowContaining(t, body, "dross remote bootstrap [--apply]")
	if !strings.Contains(row, "test_lane") && !strings.Contains(row, "declared lane") {
		t.Errorf("the bootstrap row does not say it covers declared lanes:\n%s", row)
	}
}

// TestOptionsDocumentsTheInstallGrant: options.md is the one place the
// machine-local keys are enumerated for a reader, and this key is deliberately
// out of localKeys — so the sweep that keeps that enumeration honest cannot see
// it.
func TestOptionsDocumentsTheInstallGrant(t *testing.T) {
	body := docsBody(t, "assets", "prompts", "options.md")
	for _, want := range []string{"dross trust --lane-install", "trusted_lane_installs", "dross test lane install"} {
		if !strings.Contains(body, want) {
			t.Errorf("options.md does not mention %q", want)
		}
	}
}

// TestOptionsNeverTellsTheUserToSetTheGrantDirectly is the negative arm the
// toolchain precedent carries. A doc offering `dross local set
// trusted_lane_installs` would be teaching a gesture the CLI refuses — and one
// that, if it worked, would authorize changing a machine without showing the
// line.
func TestOptionsNeverTellsTheUserToSetTheGrantDirectly(t *testing.T) {
	body := docsBody(t, "assets", "prompts", "options.md")
	if strings.Contains(body, "local set trusted_lane_installs") {
		t.Error("options.md tells the user to set the install grant through `dross local set`")
	}
}

// TestDocumentedFlagsResolve: a README naming a flag the binary lacks is worse
// than no documentation — the reader types it and gets an error they cannot
// distinguish from their own mistake.
func TestDocumentedFlagsResolve(t *testing.T) {
	root := &cobra.Command{Use: "dross"}
	root.AddCommand(Test(), Trust(), Remote())

	for _, tc := range []struct {
		path  []string
		flags []string
	}{
		{[]string{"test", "lane", "install"}, []string{"apply", "local"}},
		{[]string{"trust"}, []string{"lane-install", "lane", "check"}},
		{[]string{"remote", "bootstrap"}, []string{"apply"}},
	} {
		found, _, err := root.Find(tc.path)
		if err != nil {
			t.Errorf("`dross %s` does not resolve: %v", strings.Join(tc.path, " "), err)
			continue
		}
		for _, flag := range tc.flags {
			if found.Flags().Lookup(flag) == nil {
				t.Errorf("`dross %s` has no --%s, but the README names it", strings.Join(tc.path, " "), flag)
			}
		}
	}
}

// readmeRowContaining returns the single README table row carrying want.
func readmeRowContaining(t *testing.T, body, want string) string {
	t.Helper()
	// The row whose FIRST CELL names it wins, before any row that merely
	// mentions it. The README is one long table and rows cross-reference each
	// other — the remote row names `dross test lane install` among the surfaces
	// that must agree about the host — so a plain body match hands back
	// whichever row happens to come first and silently asserts against the
	// wrong one.
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		lines = append(lines, line)
		if strings.Contains(firstCell(line), want) {
			return line
		}
	}
	// A caller may legitimately name something documented in a row's body
	// rather than its heading (`dross remote bootstrap [--apply]` lives inside
	// the `dross remote {…}` row), so the body match stays as the fallback.
	for _, line := range lines {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no README table row mentions %q", want)
	return ""
}
