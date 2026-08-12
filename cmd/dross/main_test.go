package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

// promptCmdRef matches a `dross <verb>` (optionally `dross <verb> <sub>`)
// invocation inside a backtick code span. The optional second word only
// matches a bare lowercase word, so flags (`--auto`) and placeholders
// (`<phase-id>`) fall out and leave just the top-level verb to check.
var promptCmdRef = regexp.MustCompile("`dross ([a-z][a-z0-9-]*)(?: ([a-z][a-z0-9-]*))?")

// TestShipPromptCommandsExist is the parity guard for ship.md, the sibling of
// TestReadmeAdvertisesOnlyRealCommands above: every `dross …` invocation the
// prompt tells the model to run must resolve against the assembled cobra tree.
// It lives here rather than in internal/cmd because the tree is assembled by
// newRoot, which is in package main and cannot be imported from internal/cmd.
//
// This is what stops a prompt advertising a command before it exists — the
// failure mode where a prompt edit lands ahead of the CLI verb it depends on
// and every run of the slash command hits "unknown command".
func TestShipPromptCommandsExist(t *testing.T) {
	top := topLevelIndex(newRoot())

	b, err := os.ReadFile(filepath.Join("..", "..", "assets", "prompts", "ship.md"))
	if err != nil {
		t.Fatal(err)
	}

	matches := promptCmdRef.FindAllStringSubmatch(string(b), -1)
	if len(matches) == 0 {
		t.Fatal("parsed zero `dross <cmd>` references from ship.md — the regex or path is wrong")
	}
	for _, m := range matches {
		if why := resolveCmdRef(top, m[1], m[2]); why != "" {
			t.Errorf("ship.md runs `%s` but %s", strings.TrimSpace("dross "+m[1]+" "+m[2]), why)
		}
	}
}

// topLevelIndex maps every top-level command name and alias to its command.
func topLevelIndex(root *cobra.Command) map[string]*cobra.Command {
	top := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		top[c.Name()] = c
		for _, a := range c.Aliases {
			top[a] = c
		}
	}
	return top
}

// resolveCmdRef reports why `dross <verb> [<sub>]` fails to resolve against the
// assembled tree, or "" when it resolves. A sub against a leaf command is not a
// subcommand claim — it's an argument — so it resolves.
func resolveCmdRef(top map[string]*cobra.Command, verb, sub string) string {
	parent, ok := top[verb]
	if !ok {
		return "no such command exists in the cobra tree"
	}
	if sub == "" || !parent.HasSubCommands() {
		return ""
	}
	for _, c := range parent.Commands() {
		if c.Name() == sub {
			return ""
		}
		for _, a := range c.Aliases {
			if a == sub {
				return ""
			}
		}
	}
	return fmt.Sprintf("%q has no such subcommand", verb)
}

// --- narrated-command parity (c-1) ---

// narratedRef is one `dross <verb> [<sub>]` reference lifted out of a Go string
// literal, tagged with the file that narrates it.
type narratedRef struct {
	file string
	verb string
	sub  string
}

func (r narratedRef) String() string {
	return strings.TrimSpace("dross " + r.verb + " " + r.sub)
}

// narratedIgnore holds `dross …` forms that appear in narration strings but are
// not command claims — placeholder verbs the user substitutes, and mis-reaches
// the error text quotes back at the user precisely because they do not resolve.
//
// Every entry is asserted below to be (a) actually used by some narration and
// (b) a form the guard would otherwise reject, so a stale entry cannot sit here
// masking a real command that has since been removed.
var narratedIgnore = map[string]bool{}

// narratedCmdRefs extracts every `dross <verb> [<sub>]` reference from the
// string literals of one Go file. Literals only: a stale command name in a
// comment misleads a reader, but only a narration string reaches the user, and
// that is the claim this guard is about.
func narratedCmdRefs(t *testing.T, path string) []narratedRef {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var out []narratedRef
	base := filepath.Base(path)
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, m := range promptCmdRef.FindAllStringSubmatch(val, -1) {
			out = append(out, narratedRef{file: base, verb: m[1], sub: m[2]})
		}
		return true
	})
	return out
}

// unresolvedNarrations returns one message per reference that neither resolves
// against the tree nor sits on the ignore list, and the set of ignore entries
// the references actually used. Pure over its inputs so the guard's own failure
// path can be exercised with a synthetic reference.
func unresolvedNarrations(top map[string]*cobra.Command, refs []narratedRef) (msgs []string, usedIgnores map[string]bool) {
	usedIgnores = map[string]bool{}
	for _, r := range refs {
		if narratedIgnore[r.String()] {
			usedIgnores[r.String()] = true
			continue
		}
		if why := resolveCmdRef(top, r.verb, r.sub); why != "" {
			msgs = append(msgs, fmt.Sprintf("%s narrates `%s` but %s", r.file, r, why))
		}
	}
	return msgs, usedIgnores
}

// TestNarratedCommandsResolveAgainstTheTree is the fourth sibling of the
// README / ship.md / curated-hint parity guards, covering the one narration
// surface none of them reach: a `dross …` invocation the CLI prints at the user
// from a Go string literal.
//
// The failure mode is a command being unregistered (or renamed) while an error
// message still tells the user to run it — they hit "unknown command" and fall
// back to the raw git incantation the whole phase exists to retire. Deleting
// `cmd.Checkout()` from main.go leaves every other test in this repo green;
// this one goes red, naming both the dead command and the file narrating it.
func TestNarratedCommandsResolveAgainstTheTree(t *testing.T) {
	top := topLevelIndex(newRoot())

	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "cmd", "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	var refs []narratedRef
	for _, f := range files {
		// Test sources deliberately narrate commands that do not exist — the
		// mis-reach fixtures (`dross task wibble`) are the point of them.
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		refs = append(refs, narratedCmdRefs(t, f)...)
	}
	if len(refs) == 0 {
		t.Fatal("parsed zero `dross <cmd>` references from internal/cmd — the glob, the literal walk or the regex is wrong")
	}

	msgs, usedIgnores := unresolvedNarrations(top, refs)
	for _, m := range msgs {
		t.Error(m)
	}

	// Ignore-list hygiene: an entry no narration uses is dead weight, and an
	// entry that resolves is masking nothing — either way it would silently
	// swallow a real regression later.
	for key := range narratedIgnore {
		if !usedIgnores[key] {
			t.Errorf("ignore entry %q matches no narration — stale, drop it", key)
		}
		parts := strings.Fields(key)
		if len(parts) < 2 || parts[0] != "dross" {
			t.Errorf("ignore entry %q is not a `dross <verb> [<sub>]` form", key)
			continue
		}
		var sub string
		if len(parts) > 2 {
			sub = parts[2]
		}
		if resolveCmdRef(top, parts[1], sub) == "" {
			t.Errorf("ignore entry %q resolves against the tree — it is a real command, drop it from the ignore list", key)
		}
	}
}

// TestNarratedCommandsGuardCatchesBogusSubcommands exercises the guard's own
// failure path: top-level resolution alone must not satisfy it, and the message
// must name both the unresolvable invocation and the file that narrates it.
func TestNarratedCommandsGuardCatchesBogusSubcommands(t *testing.T) {
	top := topLevelIndex(newRoot())

	msgs, _ := unresolvedNarrations(top, []narratedRef{
		{file: "phase.go", verb: "phase", sub: "nope"},
		{file: "phase.go", verb: "phase", sub: "create"},
		{file: "wat.go", verb: "definitelynotacommand"},
	})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2 (`dross phase nope` and `dross definitelynotacommand`):\n%s",
			len(msgs), strings.Join(msgs, "\n"))
	}
	if !strings.Contains(msgs[0], "phase.go") || !strings.Contains(msgs[0], "dross phase nope") {
		t.Errorf("message must name both the file and the invocation, got: %q", msgs[0])
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

// TestMainEntryPoint drives the real main() in a subprocess, which is the only
// way to reach it: main() ends in os.Exit, so calling it in-process would take
// the test binary down with it.
//
// The two guards it covers are the telemetry fallbacks, and they only ever run
// in opposite situations, so both arms need a real invocation:
//
//   - `if resolvedCmd == nil` — Execute can fail BEFORE PersistentPreRun (an
//     unknown subcommand, a flag parse error), leaving no resolved command and
//     a telemetry event with no `cmd` field. The fallback re-Finds it, so the
//     event still records what was attempted.
//   - `if err != nil` — the exit-code path. Dropped, dross would exit 0 on
//     every failure and every caller gating on `dross …` would see success.
//
// The child branch replaces os.Args before calling main(), so the invocation
// under test is exact rather than whatever `go test` was passed.
func TestMainEntryPoint(t *testing.T) {
	if args, ok := os.LookupEnv("DROSS_MAIN_TEST_ARGS"); ok {
		os.Args = append([]string{"dross"}, strings.Fields(args)...)
		main()
		return
	}

	cases := []struct {
		name     string
		args     string
		wantExit int
		wantErr  string // substring of combined output; empty means "no dross: line"
	}{
		{
			name:     "a resolved command exits 0",
			args:     "version",
			wantExit: 0,
		},
		{
			name:     "an unknown command exits 1 and names the failure",
			args:     "definitelynotacommand",
			wantExit: 1,
			wantErr:  "dross:",
		},
		{
			name:     "a flag parse error also exits 1",
			args:     "--definitelynotaflag",
			wantExit: 1,
			wantErr:  "dross:",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := exec.Command(os.Args[0], "-test.run=^TestMainEntryPoint$")
			sub.Env = append(os.Environ(),
				"DROSS_MAIN_TEST_ARGS="+c.args,
				"DROSS_NO_TELEMETRY=1",
				"HOME="+t.TempDir(),
				"CLAUDE_CONFIG_DIR="+t.TempDir(),
			)
			out, err := sub.CombinedOutput()

			exit := 0
			if err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("running the child failed outright: %v\n%s", err, out)
				}
				exit = ee.ExitCode()
			}
			if exit != c.wantExit {
				t.Errorf("`dross %s` exited %d, want %d:\n%s", c.args, exit, c.wantExit, out)
			}
			if c.wantErr == "" {
				if strings.Contains(string(out), "dross: ") {
					t.Errorf("`dross %s` succeeded but printed an error line:\n%s", c.args, out)
				}
				return
			}
			if !strings.Contains(string(out), c.wantErr) {
				t.Errorf("`dross %s` did not print %q:\n%s", c.args, c.wantErr, out)
			}
		})
	}
}
