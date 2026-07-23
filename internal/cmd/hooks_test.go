package cmd

import (
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
