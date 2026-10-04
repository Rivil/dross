package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gate"
)

// runGate runs `dross gate <args…>` with stdin under whatever HOME the test
// set, returning stdout, stderr and the error.
func runGate(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	c := Gate()
	var out, errb bytes.Buffer
	c.SetArgs(args)
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&errb)
	err := c.Execute()
	return out.String(), errb.String(), err
}

// gateClock pins gateNow to at for the rest of the test.
func gateClock(t *testing.T, at time.Time) {
	t.Helper()
	orig := gateNow
	t.Cleanup(func() { gateNow = orig })
	gateNow = func() time.Time { return at }
}

// readEnvPayload is a PreToolUse Read of a .env file — secret-read refuses it.
func readEnvPayload(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": "s", "tool_name": "Read",
		"tool_input": map[string]any{"file_path": filepath.Join(dir, ".env")}, "cwd": dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// drossRepoAt is a minimal dross root — a .dross/project.toml — at a resolved
// path, so the working directory reads back exactly as written.
func drossRepoAt(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGateOffLiftExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	gateClock(t, base)
	in := readEnvPayload(t)
	if _, _, err := runGate(t, in, "check"); ExitCode(err) != 2 {
		t.Fatalf("a Read of .env before any lift: exit %d (%v), want 2", ExitCode(err), err)
	}
	if _, _, err := runGate(t, "", "off", "secret-read", "--for", "30m"); err != nil {
		t.Fatalf("gate off secret-read --for 30m: %v", err)
	}
	if _, errOut, err := runGate(t, in, "check"); err != nil {
		t.Fatalf("a Read of .env inside the lift: exit %d (%v; stderr %q), want 0", ExitCode(err), err, errOut)
	}
	gateClock(t, base.Add(31*time.Minute))
	if _, _, err := runGate(t, in, "check"); ExitCode(err) != 2 {
		t.Errorf("a Read of .env 31m into a 30m lift: exit %d (%v), want 2 — the lift must expire", ExitCode(err), err)
	}
}

func TestGateOffLiftScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, b := drossRepoAt(t), drossRepoAt(t)
	chdir(t, a)
	out, _, err := runGate(t, "", "off", "commit-green")
	if err != nil || !strings.Contains(out, a) {
		t.Fatalf("gate off commit-green in repo A: %q (%v), want output naming %s", out, err, a)
	}
	g, _ := gate.Lookup("commit-green")
	lifted := gate.LiftedBy(home, time.Now())
	if !lifted(g, a) || lifted(g, b) {
		t.Errorf("after lifting commit-green in A: lifted in A=%v, in B=%v — want A only", lifted(g, a), lifted(g, b))
	}

	chdir(t, t.TempDir())
	if _, _, err := runGate(t, "", "off", "commit-green"); err == nil || !strings.Contains(err.Error(), "not inside a dross repo") {
		t.Errorf("gate off commit-green outside any repo: %v, want an error", err)
	}
	out, _, err = runGate(t, "", "off", "secret-read")
	if err != nil || !strings.Contains(out, "machine-wide") {
		t.Errorf("gate off secret-read: %q (%v), want output saying machine-wide", out, err)
	}
}

func TestGateOffBounds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	in := readEnvPayload(t)
	for _, dur := range []string{"0", "-5m", "25h"} {
		_, _, err := runGate(t, "", "off", "secret-read", "--for="+dur)
		if err == nil || !strings.Contains(err.Error(), "24h") {
			t.Errorf("--for %s: %v, want an error naming the 24h cap", dur, err)
		}
		if _, statErr := os.Stat(gate.OverridesPath(home)); !os.IsNotExist(statErr) {
			t.Fatalf("--for %s wrote the override store (stat: %v)", dur, statErr)
		}
	}
	if _, _, err := runGate(t, "", "off", "secret-read", "--for", "24h"); err != nil {
		t.Fatalf("--for 24h, the cap itself: %v", err)
	}
	if _, _, err := runGate(t, in, "check"); err != nil {
		t.Fatalf("the check inside the lift: %v, want exit 0", err)
	}
	if _, _, err := runGate(t, "", "on", "secret-read"); err != nil {
		t.Fatalf("gate on secret-read: %v", err)
	}
	if _, _, err := runGate(t, in, "check"); ExitCode(err) != 2 {
		t.Errorf("the check after gate on: exit %d (%v), want 2", ExitCode(err), err)
	}
}

func TestGateOffNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, _, err := runGate(t, "", "off", "secret-raed")
	if err == nil {
		t.Fatal("gate off of an unknown name succeeded")
	}
	for _, g := range gate.All() {
		if !strings.Contains(err.Error(), g.Name) {
			t.Errorf("the unknown-name error %q does not list %s", err, g.Name)
		}
	}
	for _, name := range []string{"gate-off-guard", "tamper-guard"} {
		if _, _, err := runGate(t, "", "off", name); err == nil {
			t.Errorf("gate off %s succeeded; it cannot be lifted", name)
		}
	}
}

func TestGateStatusListsEveryGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	chdir(t, drossRepoAt(t))
	gateClock(t, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	if _, _, err := runGate(t, "", "off", "commit-green", "--for", "2h"); err != nil {
		t.Fatal(err)
	}
	out, _, err := runGate(t, "", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gate.All() {
		if !strings.Contains(out, g.Name) {
			t.Errorf("gate status omits %s:\n%s", g.Name, out)
		}
	}
	if !strings.Contains(out, "off this repo until 2026-10-03T14:00:00Z") {
		t.Errorf("gate status does not show commit-green's lift and expiry:\n%s", out)
	}

	if err := os.WriteFile(gate.OverridesPath(home), []byte("[{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err = runGate(t, "", "status")
	if err != nil || !strings.Contains(out, gate.OverridesFile) || !strings.Contains(out, "invalid character") {
		t.Errorf("gate status with a corrupt override store: %q (%v), want its parse error shown", out, err)
	}
}

// TestGateRegistryNames pins the registered set: a gate added or renamed must
// change this list, and the README row and remedies along with it.
func TestGateRegistryNames(t *testing.T) {
	want := []string{"commit-green", "curated-shrink", "gate-off-guard", "pair-approval", "plan-edit", "secret-read", "secret-stream", "solo-review", "tamper-guard"}
	var got []string
	for _, g := range gate.All() {
		got = append(got, g.Name)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("registered gates = %v, want %v", got, want)
	}
}

// TestGateStatusScopesAndRepo pins what `gate status` says about where it
// runs — a dross repo, none, or a directory it cannot read — and the scope it
// prints beside each gate.
func TestGateStatusScopesAndRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chdir(t, t.TempDir())
	out, _, err := runGate(t, "", "status")
	if err != nil || !strings.Contains(out, "repo: none here") {
		t.Errorf("gate status outside any dross repo: %q (%v), want `repo: none here`", out, err)
	}
	for name, scope := range map[string]string{"commit-green": "workflow", "secret-read": "always-on"} {
		if !regexp.MustCompile(`(?m)^` + name + ` +` + scope + ` `).MatchString(out) {
			t.Errorf("gate status does not list %s as %s:\n%s", name, scope, out)
		}
	}

	// A .dross/ the locator can see but not search: whether project.toml is
	// inside cannot be told, which is neither "a repo" nor "none".
	if os.Geteuid() == 0 {
		t.Skip("root searches a mode-000 directory, so the locator cannot be made to fail")
	}
	dir := drossRepoAt(t)
	chdir(t, dir)
	sealed := filepath.Join(dir, ".dross")
	if err := os.Chmod(sealed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o755) })
	out, _, err = runGate(t, "", "status")
	if err != nil || !strings.Contains(out, "repo: cannot tell (") || !strings.Contains(out, "permission denied") {
		t.Errorf("gate status over an unsearchable .dross/: %q (%v), want `repo: cannot tell` naming the error", out, err)
	}
}

// TestReadmeNamesEveryGate: the README's `dross gate` row names the gates a
// human can lift by name — every registered gate must appear there, or a
// refusal points at a gate the docs never mention.
func TestReadmeNamesEveryGate(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "| `dross gate {") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("README has no `dross gate` row")
	}
	for _, g := range gate.All() {
		if !strings.Contains(row, "`"+g.Name+"`") {
			t.Errorf("README's `dross gate` row does not name the %s gate", g.Name)
		}
	}
}
