package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/cmd"
)

// readmeCmdRef matches a `dross <word>` reference inside a backtick code
// span in the README — the command the docs advertise. We check the first
// word after `dross ` against the real cobra tree.
var readmeCmdRef = regexp.MustCompile("`dross ([a-z][a-z-]*)")

// TestReadmeAdvertisesOnlyRealCommands is the README truth-pass guard
// (readme-truth-pass c-1): every top-level `dross <cmd>` the README
// advertises must exist in the assembled command tree. Catches the
// over-claim failure mode — a renamed or removed command still documented
// — which is the lie a first-time reader would hit. (Under-claiming, i.e.
// an internal command the table omits, is allowed and not checked.)
func TestReadmeAdvertisesOnlyRealCommands(t *testing.T) {
	real := map[string]bool{}
	for _, c := range newRoot().Commands() {
		real[c.Name()] = true
	}

	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	// `dross <word>` forms that aren't real top-level commands but appear
	// in prose as sub-verbs or placeholders — not command claims.
	ignore := map[string]bool{"phase-id": true}

	seen := map[string]bool{}
	for _, m := range readmeCmdRef.FindAllStringSubmatch(string(b), -1) {
		name := m[1]
		if ignore[name] || seen[name] {
			continue
		}
		seen[name] = true
		if !real[name] {
			t.Errorf("README advertises `dross %s` but no such command exists in the cobra tree", name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("parsed zero `dross <cmd>` references from README — the regex or path is wrong")
	}
}

// TestReadmeStatusNotStale pins the version framing: once the repo is at
// v1.0 the status line must not still advertise a v0.x milestone series.
func TestReadmeStatusNotStale(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "**Status:** v0.10") {
		t.Error("README status line still says v0.10.x — stale after the v1.0 milestone")
	}
}

// runRoot executes the assembled tree against args and returns the error,
// with cobra's own output captured so a failing test doesn't spray usage text
// into the test log.
func runRoot(t *testing.T, args ...string) error {
	t.Helper()
	t.Setenv("DROSS_NO_TELEMETRY", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := newRoot()
	root.SilenceErrors = true
	root.SetArgs(args)
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))
	return root.Execute()
}

// TestMisreachesAreSelfCorrecting is c-2's acceptance sentence, executed. This
// is the only package that can see the real assembled tree rather than a
// hand-built one, so it is the only place the four mis-reaches can be pinned
// end to end.
func TestMisreachesAreSelfCorrecting(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{"task done", []string{"task", "done", "t-1"}, "dross task status"},
		{"phase create --title", []string{"phase", "create", "--title", "x"}, `dross phase create "<title>"`},
		{"task edit --files", []string{"task", "edit", "01-p", "t-1", "--files", "a.go"}, "dross task add"},
		{"security run --new", []string{"security", "run", "--new"}, "dross security run"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runRoot(t, c.args...)
			if err == nil {
				t.Fatalf("`dross %s` exited 0", strings.Join(c.args, " "))
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error does not print the working invocation %q:\n%v", c.wantIn, err)
			}
		})
	}
}

// flagRef matches a long flag inside a curated hint's replacement invocation.
var flagRef = regexp.MustCompile(`--[a-z][a-z-]*`)

// TestCuratedHintsResolveAgainstTheTree stops the hint table rotting into
// pointing at nothing: every command path and every flag it names — on both
// the key side and the replacement side — must exist in the real tree.
func TestCuratedHintsResolveAgainstTheTree(t *testing.T) {
	root := newRoot()
	misreaches := cmd.CuratedMisreaches()
	if len(misreaches) == 0 {
		t.Fatal("the curated table is empty — this guard is vacuous")
	}

	for _, mr := range misreaches {
		keyPath, token := mr[0], mr[1]
		// The mis-reached command itself must exist, or the entry is dead
		// weight that can never fire.
		if _, err := resolvePath(root, keyPath); err != nil {
			t.Errorf("curated key %q: %v", keyPath, err)
		}
		hint, ok := cmd.CuratedHint(keyPath, token)
		if !ok {
			t.Fatalf("CuratedMisreaches returned (%q, %q) but CuratedHint has no entry", keyPath, token)
		}

		target, err := resolvePath(root, hint.Command)
		if err != nil {
			t.Errorf("hint for (%q, %q) names Command %q: %v", keyPath, token, hint.Command, err)
			continue
		}
		// Every flag the replacement invocation mentions — declared in
		// Flags or merely written into Fix — must exist on that command.
		for _, f := range append(append([]string{}, hint.Flags...), flagRef.FindAllString(hint.Fix, -1)...) {
			name := strings.TrimPrefix(f, "--")
			if target.Flags().Lookup(name) == nil && target.InheritedFlags().Lookup(name) == nil {
				t.Errorf("hint for (%q, %q) names flag %s, which %q does not declare", keyPath, token, f, hint.Command)
			}
		}
		// The Fix's own leading command path must agree with Command, so the
		// two can't drift apart.
		if got := leadingCommandPath(hint.Fix); got != hint.Command {
			t.Errorf("hint for (%q, %q): Fix starts with %q but Command is %q", keyPath, token, got, hint.Command)
		}
	}
}

// resolvePath finds the command named by a full "dross a b" path, requiring an
// exact match — cobra's Find falls back to the deepest ancestor, which would
// otherwise let a bogus leaf resolve to its parent.
func resolvePath(root *cobra.Command, path string) (*cobra.Command, error) {
	parts := strings.Fields(path)
	if len(parts) == 0 || parts[0] != root.Name() {
		return nil, fmt.Errorf("path %q does not start with %q", path, root.Name())
	}
	found, _, err := root.Find(parts[1:])
	if err != nil {
		return nil, fmt.Errorf("does not resolve: %w", err)
	}
	if found.CommandPath() != path {
		return nil, fmt.Errorf("resolves to %q, not %q", found.CommandPath(), path)
	}
	return found, nil
}

// leadingCommandPath returns the `dross a b` prefix of an invocation, stopping
// at the first argument, placeholder or flag.
func leadingCommandPath(invocation string) string {
	var out []string
	for _, w := range strings.Fields(invocation) {
		if w == "" || strings.HasPrefix(w, "-") || strings.HasPrefix(w, "<") ||
			strings.HasPrefix(w, "[") || strings.HasPrefix(w, `"`) {
			break
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}
