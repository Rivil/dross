package main

import (
	"bytes"
	"encoding/json"
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

// --- `show --json` coverage gate (c-5) ---

// jsonExemptShows are the `show` subcommands that deliberately have no --json.
//
// Both print prose for a human or for injection into a prompt, not a document:
// `rule show` emits the <rules> block the slash commands paste verbatim, and
// `interaction show` emits the interaction playbook markdown. Neither has a
// structured form to serialise, so a --json flag would have to invent one.
//
// The list is asserted to be exactly this, so adding a tenth structured show
// and quietly exempting it fails rather than passing.
var jsonExemptShows = map[string]bool{"rule": true, "interaction": true}

// showSubcommands walks the assembled tree for every `show` subcommand,
// returning parent-name -> command.
func showSubcommands(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Name() == "show" {
				out[c.Name()] = sub
			}
			walk(sub)
		}
	}
	walk(root)
	return out
}

// TestEveryStructuredShowAcceptsJSON is c-5's completeness gate. Every `show`
// in the real tree either registers a `json` flag or is on the exempt list —
// so a future `dross findings show` landing without one fails here rather than
// leaving a hole a prompt discovers at runtime.
func TestEveryStructuredShowAcceptsJSON(t *testing.T) {
	shows := showSubcommands(newRoot())
	if len(shows) < 10 {
		t.Fatalf("walked only %d `show` subcommands — the walk is not reaching the tree", len(shows))
	}

	for parent, c := range shows {
		hasFlag := c.Flags().Lookup("json") != nil
		exempt := jsonExemptShows[parent]
		switch {
		case hasFlag && exempt:
			t.Errorf("`dross %s show` has a --json flag but is on the exempt list — drop it from the list", parent)
		case !hasFlag && !exempt:
			t.Errorf("`dross %s show` has no --json flag and is not exempt (c-5: every structured show accepts --json)", parent)
		}
	}

	// The exempt list is exactly {rule, interaction}: a stale entry naming a
	// command that no longer exists, or a tenth structured show quietly added
	// to the list instead of given a flag, both fail here.
	for name := range jsonExemptShows {
		if _, ok := shows[name]; !ok {
			t.Errorf("exempt list names %q, which has no `show` subcommand", name)
		}
	}
	if len(jsonExemptShows) != 2 || !jsonExemptShows["rule"] || !jsonExemptShows["interaction"] {
		t.Errorf("exempt list = %v, want exactly {rule, interaction}", jsonExemptShows)
	}
}

// TestEveryJSONShowEmitsValidJSON invokes each flagged `show` with --json
// against a fixture repo. A flag can be registered and never read — this is
// what catches that, and the `# ` check pins the locked json_shape (bare
// document, no header line).
func TestEveryJSONShowEmitsValidJSON(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := runRoot(t, "init"); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFixture(t, filepath.Join(dir, ".dross", "milestones", "v1.1.toml"),
		"[milestone]\nversion = \"v1.1\"\ntitle = \"Friction\"\n\n[scope]\nsuccess_criteria = [\"ships\"]\n")
	writeFixture(t, filepath.Join(dir, ".dross", "phases", "01-auth", "spec.toml"),
		"[phase]\nid = \"01-auth\"\ntitle = \"Auth\"\n")
	writeFixture(t, filepath.Join(dir, ".dross", "phases", "01-auth", "plan.toml"),
		"[phase]\nid = \"01-auth\"\n\n[[task]]\nid = \"t-1\"\nwave = 1\ntitle = \"schema\"\nfiles = [\"a.go\"]\n")

	// One invocation per flagged show. Asserted below to cover every command
	// the walk finds, so a new `show` cannot land without a row here.
	invocations := map[string][]string{
		"project":   {"project", "show", "--json"},
		"milestone": {"milestone", "show", "v1.1", "--json"},
		"phase":     {"phase", "show", "01-auth", "--json"},
		"task":      {"task", "show", "01-auth", "t-1", "--json"},
		"changes":   {"changes", "show", "01-auth", "--json"},
		"stats":     {"stats", "show", "--json"},
		"stack":     {"stack", "show", "go", "--json"},
		"defaults":  {"defaults", "show", "--json"},
		"profile":   {"profile", "show", "--json"},
		"state":     {"state", "show", "--json"},
	}
	for parent, c := range showSubcommands(newRoot()) {
		if c.Flags().Lookup("json") == nil {
			continue
		}
		if _, ok := invocations[parent]; !ok {
			t.Errorf("`dross %s show` accepts --json but this test has no invocation for it — add one, or the flag is unexercised", parent)
		}
	}

	for parent, args := range invocations {
		out, err := captureRoot(t, args...)
		if err != nil {
			t.Errorf("dross %s: %v\n%s", strings.Join(args, " "), err, out)
			continue
		}
		if !json.Valid([]byte(out)) {
			t.Errorf("`dross %s show --json` output is not valid JSON — the flag is registered but not wired:\n%s", parent, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "# ") {
				t.Errorf("`dross %s show --json` emitted a `# ` header line (locked json_shape: bare document): %q", parent, line)
			}
		}
	}
}

// TestReadmeDocumentsJSONShows keeps the doc claim honest: each command-table
// row whose `show` accepts --json says so, so the claim rots loudly.
func TestReadmeDocumentsJSONShows(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")

	for _, name := range []string{"project", "milestone", "phase", "task", "changes", "stats", "stack", "defaults", "profile"} {
		prefix := "| `dross " + name
		found := false
		for _, l := range lines {
			if !strings.HasPrefix(l, prefix) {
				continue
			}
			found = true
			if !strings.Contains(l, "--json") {
				t.Errorf("README row for `dross %s` does not mention --json", name)
			}
			break
		}
		if !found {
			t.Errorf("README has no command-table row for `dross %s`", name)
		}
	}
}

// captureRoot executes the assembled tree with os.Stdout swapped for a pipe.
// The commands print with fmt.Print* straight to os.Stdout, so this is the only
// seam. Mirrors internal/cmd's captureStdout, which is unexported.
func captureRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	runErr := runRoot(t, args...)
	_ = w.Close()
	os.Stdout = orig
	return <-done, runErr
}

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
