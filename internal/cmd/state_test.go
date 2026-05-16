package cmd

import (
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
