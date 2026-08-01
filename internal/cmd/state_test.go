package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

func TestStateSetSupportedFields(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		field, value string
		assert       func(*state.State) bool
	}{
		{"version", "0.2.3.4", func(s *state.State) bool { return s.Version == "0.2.3.4" }},
		{"current_milestone", "v1.2", func(s *state.State) bool { return s.CurrentMilestone == "v1.2" }},
		{"current_phase", "03-tags", func(s *state.State) bool { return s.CurrentPhase == "03-tags" }},
		{"current_phase_status", "executing", func(s *state.State) bool { return s.CurrentPhaseStatus == "executing" }},
	}
	for _, c := range cases {
		if err := runCmd(t, State(), "set", c.field, c.value); err != nil {
			t.Fatalf("set %s: %v", c.field, err)
		}
	}
	s, err := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !c.assert(s) {
			t.Errorf("field %s did not persist (got state: %+v)", c.field, s)
		}
	}
}

func TestStateSetRejectsUnknownField(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, State(), "set", "nonsense", "x")
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention 'unknown': %v", err)
	}
}

func TestStateTouchAppendsHistoryAndPrintsConfirmation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// init already wrote a history entry ("dross init"). Subsequent touches should append.
	out := captureStdout(t, func() {
		runCmd(t, State(), "touch", "did the thing")
	})
	if !strings.Contains(out, "did the thing") {
		t.Errorf("touch output should echo the action: %q", out)
	}
	if !strings.Contains(out, "history now") {
		t.Errorf("touch output should mention history count: %q", out)
	}

	s, err := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	// init's entry + this touch = at least 2
	if len(s.History) < 2 {
		t.Errorf("expected ≥2 history entries, got %d", len(s.History))
	}
	if s.LastAction != "did the thing" {
		t.Errorf("LastAction: %q", s.LastAction)
	}
}

func TestStateBumpInternalIncrementsLastSegment(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "version", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, State(), "bump", "internal")
	})
	if !strings.Contains(out, "1.2.3.4 → 1.2.3.5") {
		t.Errorf("bump output should show transition: %q", out)
	}
	s, err := state.Load(filepath.Join(dir, ".dross", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "1.2.3.5" {
		t.Errorf("Version: got %q want %q", s.Version, "1.2.3.5")
	}
}

func TestStateBumpRejectsUnsupportedSegment(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, State(), "bump", "patch")
	if err == nil {
		t.Fatal("expected error for unsupported segment")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention 'unsupported': %v", err)
	}
}

func TestStateBumpRejectsMalformedVersion(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "version", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	err := runCmd(t, State(), "bump", "internal")
	if err == nil {
		t.Fatal("expected error for non-4-part version")
	}
	if !strings.Contains(err.Error(), "4-part") {
		t.Errorf("error should mention '4-part': %v", err)
	}
}

func TestStateShowRendersJSON(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		runCmd(t, State(), "show")
	})
	for _, want := range []string{
		`"version"`,
		`"last_activity"`,
		`"history"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("state show missing %q\n--- output ---\n%s", want, out)
		}
	}
}

// TestStateTouchSilentOnNonRoot (c-2) pins `state touch` as a hook target: on
// an incomplete root it exits 0, prints nothing, and — the real assertion —
// creates no state.json. A fix that silently scaffolds the file instead of
// bailing passes the first two checks and fails the third.
func TestStateTouchSilentOnNonRoot(t *testing.T) {
	dir := realTempDir(t)
	root := mkRoot(t, dir) // incomplete: no project.toml
	chdir(t, dir)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, State(), "touch", "x") })
	if err != nil {
		t.Errorf("state touch should exit 0 outside a dross repo, got %v", err)
	}
	if out != "" {
		t.Errorf("state touch should print nothing outside a dross repo, got:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, state.File)); !os.IsNotExist(statErr) {
		t.Errorf("state touch must not create state.json; stat err = %v", statErr)
	}
}

// TestStateShowLoudOnNonRoot is the scoping test for t-3: the silence belongs
// in the two hook handlers, never in loadState(). `state show` is a
// user-typed read against the same fixture and must stay loud — moving the
// swallow down into loadState() makes it silent and fails here.
func TestStateShowLoudOnNonRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir) // incomplete: no project.toml
	chdir(t, dir)

	err := runCmd(t, State(), "show")
	if err == nil {
		t.Fatal("state show should fail on an incomplete root")
	}
	if !strings.Contains(err.Error(), filepath.Join(RootDirName, "project.toml")) {
		t.Errorf("error should name .dross/project.toml, got %v", err)
	}
}

// TestStateTouchLoudOnCorruptState (locked completeness_check): corrupt is
// loud even in the hook targets. An implementation that swallows every
// loadState error in the handler — rather than only ErrNoRoot — passes every
// other row in this phase and dies here.
func TestStateTouchLoudOnCorruptState(t *testing.T) {
	dir := realTempDir(t)
	root := mkRoot(t, dir, "project.toml")
	if err := os.WriteFile(filepath.Join(root, state.File), []byte("{{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	err := runCmd(t, State(), "touch", "x")
	if err == nil {
		t.Fatal("state touch should fail loudly on a corrupt state.json")
	}
	if !strings.Contains(err.Error(), state.File) {
		t.Errorf("error should name state.json, got %v", err)
	}
}

// TestStateLoadNamesPathOnCorrupt pins the unmarshal error wrap in
// state.Load: without the path in the message, the two corrupt-file rows
// above have nothing to assert on.
func TestStateLoadNamesPathOnCorrupt(t *testing.T) {
	path := filepath.Join(realTempDir(t), state.File)
	if err := os.WriteFile(path, []byte("{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := state.Load(path)
	if err == nil {
		t.Fatal("state.Load should reject unparseable JSON")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should contain the file path %q, got %v", path, err)
	}
}

func TestStateGetSinglePathIsBare(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_phase", "cli-surface-sweep"); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runCmd(t, State(), "get", "current_phase"); err != nil {
			t.Errorf("state get: %v", err)
		}
	})
	if out != "cli-surface-sweep\n" {
		t.Errorf("state get current_phase = %q, want %q", out, "cli-surface-sweep\n")
	}
}

func TestStateGetVersionAndMultiPath(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "version", "1.1.3.0"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, State(), "set", "current_milestone", "v1.1"); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, State(), "get", "version"); err != nil {
			t.Errorf("state get version: %v", err)
		}
	})
	if strings.TrimSpace(out) != "1.1.3.0" {
		t.Errorf("state get version = %q, want the 4-part version", strings.TrimSpace(out))
	}

	out = captureStdout(t, func() {
		if err := runCmd(t, State(), "get", "version", "current_milestone"); err != nil {
			t.Errorf("state get multi: %v", err)
		}
	})
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("multi-path state get is not one JSON object: %v\ngot %q", err, out)
	}
	if got["version"] != "1.1.3.0" || got["current_milestone"] != "v1.1" {
		t.Errorf("multi-path state get = %v", got)
	}
}

func TestStateGetUnknownFieldNamesIt(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// project.name lives in project.toml, not state.json.
	err := runCmd(t, State(), "get", "project.name")
	if err == nil {
		t.Fatal("state get project.name succeeded; want unknown-field error")
	}
	if !strings.Contains(err.Error(), "project.name") {
		t.Errorf("error = %q, want it to name the unknown field", err)
	}
}

func TestStateGetIsRegistered(t *testing.T) {
	var found bool
	for _, c := range State().Commands() {
		if c.Name() == "get" {
			found = true
		}
	}
	if !found {
		t.Error("`dross state --help` would not list `get` — subcommand not registered")
	}
}

func TestStateShowAcceptsJSONFlag(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	bare := captureStdout(t, func() {
		if err := runCmd(t, State(), "show"); err != nil {
			t.Errorf("state show: %v", err)
		}
	})
	// Pre-change this failed with `unknown flag: --json`.
	withFlag := captureStdout(t, func() {
		if err := runCmd(t, State(), "show", "--json"); err != nil {
			t.Errorf("state show --json: %v", err)
		}
	})
	if bare != withFlag {
		t.Errorf("--json changed the output:\n--- bare ---\n%s\n--- --json ---\n%s", bare, withFlag)
	}
	if strings.TrimSpace(bare) == "" {
		t.Error("state show printed nothing — the comparison is vacuous")
	}
}
