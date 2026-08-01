package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// This file holds the cross-cutting guarantees of the root-robustness phase —
// the ones no single command owns:
//
//   - every non-hook command fails loudly and actionably on an incomplete root
//   - the four hook targets write nothing, whether the root is incomplete or
//     absent, and the two cases are indistinguishable from outside
//   - only an explicit allowlist of files may swallow ErrNoRoot or reach for
//     LocateRoot, so command 57 can't quietly copy the silent pattern
//
// It is deliberately the last thing written in the phase: the allowlist below
// is a regression gate for future commands, not a scaffold for this one.

// TestIncompleteRootIsLoudEverywhere (c-3): silent exit 0 is scoped to the
// hook targets. Every other command must fail with a non-zero exit, name the
// file that's missing, and point at the repair — each its own subtest, so a
// regression names the command that broke.
func TestIncompleteRootIsLoudEverywhere(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"state show", State, []string{"show"}},
		{"project show", Project, []string{"show"}},
		{"validate", Validate, nil},
		// `task next`, not `task list` — dross has no `task list` subcommand.
		{"task next", Task, []string{"next", "some-phase"}},
		{"verify", Verify, []string{"some-phase"}},
		{"rule show", Rule, []string{"show"}},
		{"phase list", Phase, []string{"list"}},
		{"milestone show", Milestone, []string{"show"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ruleCovFakeHome(t) // `rule show` must not read the developer's global scope
			dir := realTempDir(t)
			mkRoot(t, dir) // incomplete: no project.toml
			chdir(t, dir)

			err := runCmd(t, tc.cmd(), tc.args...)
			if err == nil {
				t.Fatalf("%s should fail on an incomplete root", tc.name)
			}
			for _, want := range []string{filepath.Join(RootDirName, "project.toml"), RepairHint} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s error should contain %q, got: %v", tc.name, want, err)
				}
			}
		})
	}
}

// TestHookTargetsWriteNothing (c-1, c-2, c-4): the four hook targets are the
// whole silent set. Running all of them must leave the tree byte-identical —
// no handoff.md, no state.json, no new `.dross/` — and print nothing.
//
// Both fixtures matter. The incomplete row is c-2; the absent row is c-1's
// indistinguishability claim, asserted as behaviour rather than as an error
// type: to a caller the two cases must look exactly the same.
func TestHookTargetsWriteNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{"incomplete .dross", func(t *testing.T, dir string) { mkRoot(t, dir) }},
		{"no .dross at all", func(*testing.T, string) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := realTempDir(t)
			tc.setup(t, dir)
			chdir(t, dir)
			before := treeSnapshot(t, dir)

			var out strings.Builder
			for _, run := range []struct {
				name string
				exec func() error
			}{
				{"reentry", func() error { return runCmd(t, Reentry()) }},
				{"status", func() error { return runCmd(t, Status()) }},
				{"state touch", func() error { return runCmd(t, State(), "touch", "x") }},
				{"pause --auto", func() error { return runCmd(t, Pause(), "--auto") }},
			} {
				var printed string
				err := runCmdCapturingInto(t, &printed, run.exec)
				if err != nil {
					t.Errorf("%s should exit 0 on a non-root, got %v", run.name, err)
				}
				out.WriteString(printed)
			}

			if s := out.String(); s != "" {
				t.Errorf("the hook targets should print nothing on a non-root, got:\n%s", s)
			}
			if after := treeSnapshot(t, dir); after != before {
				t.Errorf("the hook targets wrote to the tree:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// runCmdCapturingInto captures stdout around an arbitrary run func, so the
// snapshot test can drive four differently-shaped commands uniformly.
func runCmdCapturingInto(t *testing.T, dst *string, run func() error) error {
	t.Helper()
	var err error
	*dst = captureStdout(t, func() { err = run() })
	return err
}

// treeSnapshot renders every path under dir, with file sizes, as one string.
// Comparing snapshots catches a created file, a deleted one and a rewritten
// one alike.
func treeSnapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			lines = append(lines, rel+"/")
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lines = append(lines, rel+" "+string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestRootHelperCallersAreAllowlisted is the regression gate for command 57: a
// new command that copies the silent pattern, or reaches for the walking
// LocateRoot when it should be cwd-scoped, fails here by filename rather than
// by some subtle behaviour nobody thought to assert.
//
// The scan is over parsed identifiers, not raw text, so a comment that merely
// names a helper is not a call — which is exactly what onboard.go contains.
func TestRootHelperCallersAreAllowlisted(t *testing.T) {
	errNoRootUsers := identUsers(t, "ErrNoRoot")
	locateRootUsers := identUsers(t, "LocateRoot")
	missingRootFilesUsers := identUsers(t, "MissingRootFiles")

	// Subset, not equality: swallowing ErrNoRoot is a licence, and a file may
	// legitimately stop needing it. Adding a new one is what must not pass.
	allowedSwallowers := map[string]bool{
		"root.go": true, "reentry.go": true, "status.go": true,
		"state.go": true, "pause.go": true, "rule.go": true,
	}
	for _, f := range errNoRootUsers {
		if !allowedSwallowers[f] {
			t.Errorf("%s references ErrNoRoot but is not an allowed hook target — "+
				"silent exit 0 is scoped to reentry, status, state touch and pause --auto", f)
		}
	}

	// Equality here: LocateRoot deliberately tolerates an incomplete root, so
	// every caller is a considered exception and the set is closed.
	wantLocate := []string{"doctor.go", "root.go", "ship_recover.go", "telemetry.go", "verify.go"}
	if strings.Join(locateRootUsers, ",") != strings.Join(wantLocate, ",") {
		t.Errorf("LocateRoot callers = %v, want %v", locateRootUsers, wantLocate)
	}

	// onboard.go must answer about the cwd only. Giving it LocateRoot's
	// up-walk would let it adopt an ancestor project's root (locked walk_stop).
	if !contains(missingRootFilesUsers, "onboard.go") {
		t.Errorf("onboard.go should call MissingRootFiles, callers = %v", missingRootFilesUsers)
	}
	if contains(locateRootUsers, "onboard.go") {
		t.Error("onboard.go must not call LocateRoot — it would gain an up-walk (locked walk_stop)")
	}
}

// identUsers returns the sorted base names of the non-test files in
// internal/cmd that reference the given identifier in code.
func identUsers(t *testing.T, name string) []string {
	t.Helper()
	dir := filepath.Join(repoRootFromTest(t), "internal", "cmd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var users []string
	for _, e := range entries {
		base := e.Name()
		if e.IsDir() || !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, base), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", base, err)
		}
		found := false
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				found = true
			}
			return !found
		})
		if found {
			users = append(users, base)
		}
	}
	sort.Strings(users)
	return users
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
