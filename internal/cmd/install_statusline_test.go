package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testBin = "/Users/u/.local/bin/dross"

// wantCommand is the statusLine.command t-7 writes for testBin.
var wantCommand = statuslineCommand(testBin)

func slSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal settings: %v\n%s", err, b)
	}
	return m
}

func slCommand(t *testing.T, m map[string]any) string {
	t.Helper()
	sl, _ := m["statusLine"].(map[string]any)
	cmd, _ := sl["command"].(string)
	return cmd
}

func yes(string) bool { return true }
func no(string) bool  { return false }

// TestEnableFromEmptyAbsolutePath: enabling against a missing settings.json creates
// it with the ABSOLUTE binary path command (not a bare `dross`).
func TestEnableFromEmptyAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := enableStatuslineIn(path, testBin, no, io.Discard); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got := slCommand(t, slSettings(t, path))
	if got != wantCommand {
		t.Errorf("command = %q, want %q", got, wantCommand)
	}
	if !strings.Contains(got, testBin) || !filepath.IsAbs(testBin) {
		t.Errorf("command %q must embed the absolute binary path, not a bare name", got)
	}
}

// TestEnablePreservesAndIdempotent: unrelated keys survive and a second enable is
// byte-stable.
func TestEnablePreservesAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"effortLevel":"high","env":{"A":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := enableStatuslineIn(path, testBin, no, io.Discard); err != nil {
		t.Fatal(err)
	}
	m := slSettings(t, path)
	if m["effortLevel"] != "high" || !reflect.DeepEqual(m["env"], map[string]any{"A": "1"}) {
		t.Errorf("unrelated keys not preserved: %v", m)
	}
	first, _ := os.ReadFile(path)
	if err := enableStatuslineIn(path, testBin, no, io.Discard); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second enable not idempotent:\n first: %q\nsecond: %q", first, second)
	}
}

// TestEnableRefusesForeignWithoutConsent: a different existing statusLine is left
// intact and enable errors when consent is withheld.
func TestEnableRefusesForeignWithoutConsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"statusLine":{"type":"command","command":"my-bar --x"},"effortLevel":"high"}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	err := enableStatuslineIn(path, testBin, no, io.Discard)
	if err == nil {
		t.Fatal("expected refusal error when a foreign statusLine exists and consent is withheld")
	}
	if got := slCommand(t, slSettings(t, path)); got != "my-bar --x" {
		t.Errorf("foreign statusLine altered without consent: %q", got)
	}
}

// TestEnableForeignWithConsent overwrites only when consent is given.
func TestEnableForeignWithConsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"statusLine":{"type":"command","command":"my-bar --x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := enableStatuslineIn(path, testBin, yes, io.Discard); err != nil {
		t.Fatalf("enable with consent: %v", err)
	}
	if got := slCommand(t, slSettings(t, path)); got != wantCommand {
		t.Errorf("command = %q, want overwrite to %q", got, wantCommand)
	}
}

// TestDisableRemovesOnlyOurs: disable removes dross's entry and preserves siblings;
// it is a no-op when dross never wired it and never removes a foreign statusLine.
func TestDisableRemovesOnlyOurs(t *testing.T) {
	t.Run("removes ours, preserves siblings", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(`{"effortLevel":"high"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := enableStatuslineIn(path, testBin, no, io.Discard); err != nil {
			t.Fatal(err)
		}
		if err := disableStatuslineIn(path, testBin, io.Discard); err != nil {
			t.Fatal(err)
		}
		m := slSettings(t, path)
		if _, ok := m["statusLine"]; ok {
			t.Errorf("statusLine not removed: %v", m)
		}
		if m["effortLevel"] != "high" {
			t.Errorf("sibling lost on disable: %v", m)
		}
	})

	t.Run("missing file is a no-op", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := disableStatuslineIn(path, testBin, io.Discard); err != nil {
			t.Errorf("disable on missing settings should be a no-op, got %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("disable created a settings.json where there was none")
		}
	})

	t.Run("foreign statusLine untouched", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		orig := `{"statusLine":{"type":"command","command":"my-bar --x"}}`
		if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := disableStatuslineIn(path, testBin, io.Discard); err != nil {
			t.Fatal(err)
		}
		if got := slCommand(t, slSettings(t, path)); got != "my-bar --x" {
			t.Errorf("foreign statusLine removed/altered: %q", got)
		}
	})
}

// TestResolveStatuslineBinaryAbsolute: the resolved binary path is absolute.
func TestResolveStatuslineBinaryAbsolute(t *testing.T) {
	bin, err := resolveStatuslineBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(bin) {
		t.Errorf("resolved binary path %q is not absolute", bin)
	}
}
