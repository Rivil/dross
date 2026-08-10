package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readHookCommands returns the flat command list under hooks.<event> in a
// settings.json file (empty when the file is missing).
func readHookCommands(t *testing.T, path, event string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	var cmds []string
	for _, g := range parsed.Hooks[event] {
		for _, h := range g.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	return cmds
}

func countIn(list []string, want string) int {
	n := 0
	for _, c := range list {
		if c == want {
			n++
		}
	}
	return n
}

// TestEnsureUserHooksIdempotent: running the ensure twice against a temp
// CLAUDE_CONFIG_DIR leaves exactly one dross PreCompact and one dross
// SessionStart entry, and the second run is a byte-stable no-op.
func TestEnsureUserHooksIdempotent(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	settings := filepath.Join(cfg, "settings.json")

	if err := ensureUserHooks(); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	first := mustRead(t, settings)
	if err := ensureUserHooks(); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second := mustRead(t, settings)
	if first != second {
		t.Errorf("second ensure not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	if n := countIn(readHookCommands(t, settings, "PreCompact"), preCompactHookCommand); n != 1 {
		t.Errorf("want exactly 1 PreCompact %q entry, got %d", preCompactHookCommand, n)
	}
	if n := countIn(readHookCommands(t, settings, "SessionStart"), sessionStartHookCommand); n != 1 {
		t.Errorf("want exactly 1 SessionStart %q entry, got %d", sessionStartHookCommand, n)
	}
}

// TestEnsureUserHooksPreservesForeign: a pre-existing foreign PreCompact hook
// (and unrelated top-level keys) survive the ensure untouched.
func TestEnsureUserHooksPreservesForeign(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	settings := filepath.Join(cfg, "settings.json")
	mustWrite(t, settings, `{
  "model": "opus",
  "hooks": {
    "PreCompact": [
      {"hooks": [{"type": "command", "command": "/usr/local/bin/my-backup.sh"}]}
    ]
  }
}`)

	if err := ensureUserHooks(); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	pc := readHookCommands(t, settings, "PreCompact")
	if countIn(pc, "/usr/local/bin/my-backup.sh") != 1 {
		t.Errorf("foreign PreCompact hook must survive, got %v", pc)
	}
	if countIn(pc, preCompactHookCommand) != 1 {
		t.Errorf("dross PreCompact hook must be added alongside, got %v", pc)
	}
	if !strings.Contains(mustRead(t, settings), `"model": "opus"`) {
		t.Error("unrelated top-level keys must survive the ensure")
	}
}

// TestEnsureUserHooksRefusesGarbage: a malformed settings.json errors and is
// left byte-identical — never half-rewritten.
func TestEnsureUserHooksRefusesGarbage(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	settings := filepath.Join(cfg, "settings.json")
	garbage := `{"hooks": `
	mustWrite(t, settings, garbage)

	if err := ensureUserHooks(); err == nil {
		t.Fatal("want error on malformed settings.json")
	}
	if got := mustRead(t, settings); got != garbage {
		t.Errorf("malformed settings.json must be left untouched, got:\n%s", got)
	}
}

// TestHooksEnsureCommand: the standalone `dross hooks ensure` verb wires both
// hooks without going through init/onboard, and a second run is a byte-stable
// no-op.
func TestHooksEnsureCommand(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	settings := filepath.Join(cfg, "settings.json")

	run := func() {
		t.Helper()
		c := Hooks()
		c.SetOut(new(bytes.Buffer))
		c.SetErr(new(bytes.Buffer))
		c.SetArgs([]string{"ensure"})
		if err := c.Execute(); err != nil {
			t.Fatalf("hooks ensure: %v", err)
		}
	}

	run()
	if n := countIn(readHookCommands(t, settings, "PreCompact"), preCompactHookCommand); n != 1 {
		t.Errorf("want exactly 1 PreCompact %q entry, got %d", preCompactHookCommand, n)
	}
	if n := countIn(readHookCommands(t, settings, "SessionStart"), sessionStartHookCommand); n != 1 {
		t.Errorf("want exactly 1 SessionStart %q entry, got %d", sessionStartHookCommand, n)
	}

	first := mustRead(t, settings)
	run()
	if second := mustRead(t, settings); first != second {
		t.Errorf("second `hooks ensure` not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestUserSettingsPathFallsBackToHome covers both arms of the settings-path
// resolution, including the home-resolution failure the CLAUDE_CONFIG_DIR
// override normally hides.
//
// The override exists so a test (and a user with a relocated config) never
// touches the real ~/.claude, so the fallback below it only runs when the
// override is absent — which is exactly why it went unmeasured.
func TestUserSettingsPathFallsBackToHome(t *testing.T) {
	t.Run("the override wins outright", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", dir)
		got, err := userSettingsPath()
		if err != nil {
			t.Fatalf("userSettingsPath: %v", err)
		}
		if want := filepath.Join(dir, "settings.json"); got != want {
			t.Errorf("userSettingsPath = %q, want %q", got, want)
		}
	})

	t.Run("without it, it falls back to ~/.claude", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", home)
		got, err := userSettingsPath()
		if err != nil {
			t.Fatalf("userSettingsPath: %v", err)
		}
		if want := filepath.Join(home, ".claude", "settings.json"); got != want {
			t.Errorf("userSettingsPath = %q, want %q", got, want)
		}
	})

	t.Run("an unresolvable home is an error, not a bare path", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		t.Setenv("HOME", "")
		got, err := userSettingsPath()
		if err == nil {
			t.Fatalf("userSettingsPath returned %q with no resolvable home, want an error", got)
		}
		if got != "" {
			t.Errorf("a failed resolve returned a path anyway: %q — writing to it would land somewhere arbitrary", got)
		}
	})
}
