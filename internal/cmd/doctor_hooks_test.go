package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settingsWith is a settings.json wiring the given event→command pairs, each
// in its own group; matcher, when set, goes on every group.
func settingsWith(t *testing.T, matcher string, pairs ...[2]string) string {
	t.Helper()
	hooksObj := map[string][]map[string]any{}
	for _, p := range pairs {
		g := map[string]any{"hooks": []map[string]string{{"type": "command", "command": p[1]}}}
		if matcher != "" {
			g["matcher"] = matcher
		}
		hooksObj[p[0]] = append(hooksObj[p[0]], g)
	}
	b, err := json.Marshal(map[string]any{"hooks": hooksObj})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	preCompactPair   = [2]string{"PreCompact", preCompactHookCommand}
	sessionStartPair = [2]string{"SessionStart", sessionStartHookCommand}
	gateCheckPair    = [2]string{"PreToolUse", GateCheckHook}
	gateRecordPair   = [2]string{"PostToolUse", GateRecordHook}
)

// withConfig points CLAUDE_CONFIG_DIR at a fresh dir holding settings (none
// when settings is "").
func withConfig(t *testing.T, settings string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if settings != "" {
		mustWrite(t, filepath.Join(cfg, "settings.json"), settings)
	}
	return cfg
}

func runDoctorHooks(t *testing.T) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	return doctorSection(t, out, "Hooks:"), err
}

func TestDoctorGateHooks(t *testing.T) {
	doctorRepo(t)
	withConfig(t, "")
	sec, err := runDoctorHooks(t)
	if err == nil {
		t.Fatalf("doctor passed with no gate hooks wired:\n%s", sec)
	}
	for _, want := range []string{"✗ PreToolUse → `dross gate check`", "✗ PostToolUse → `dross gate record`", "dross hooks ensure"} {
		if !strings.Contains(sec, want) {
			t.Errorf("Hooks section lacks %q:\n%s", want, sec)
		}
	}

	if err := runCmd(t, Hooks(), "ensure"); err != nil {
		t.Fatal(err)
	}
	sec, err = runDoctorHooks(t)
	if err != nil {
		t.Fatalf("doctor failed after `dross hooks ensure`: %v\n%s", err, sec)
	}
	for _, want := range []string{"✓ PreCompact", "✓ SessionStart", "✓ PreToolUse → dross gate check", "✓ PostToolUse → dross gate record"} {
		if !strings.Contains(sec, want) {
			t.Errorf("Hooks section lacks %q after ensure:\n%s", want, sec)
		}
	}
}

func TestDoctorMissingConvenienceHooksWarnOnly(t *testing.T) {
	doctorRepo(t)
	for _, c := range []struct {
		name  string
		pairs [][2]string
		warn  string
	}{
		{"no PreCompact", [][2]string{sessionStartPair, gateCheckPair, gateRecordPair}, "⚠ PreCompact"},
		{"no SessionStart", [][2]string{preCompactPair, gateCheckPair, gateRecordPair}, "⚠ SessionStart"},
	} {
		t.Run(c.name, func(t *testing.T) {
			withConfig(t, settingsWith(t, "", c.pairs...))
			sec, err := runDoctorHooks(t)
			if err != nil {
				t.Errorf("a missing convenience hook failed doctor: %v\n%s", err, sec)
			}
			if !strings.Contains(sec, c.warn) {
				t.Errorf("Hooks section lacks %q:\n%s", c.warn, sec)
			}
		})
	}
}

func TestDoctorHiddenGateHooks(t *testing.T) {
	doctorRepo(t)
	t.Run("gate commands only under a matcher", func(t *testing.T) {
		withConfig(t, settingsWith(t, "Bash", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair))
		sec, err := runDoctorHooks(t)
		if err == nil || !strings.Contains(sec, "✗ PreToolUse → `dross gate check` is wired only under a matcher") {
			t.Fatalf("a matcher-restricted gate hook passed: %v\n%s", err, sec)
		}
		for _, line := range strings.Split(sec, "\n") {
			if strings.Contains(line, "only under a matcher") && (!strings.Contains(line, "remove the matcher") || strings.Contains(line, "Fix: `dross hooks ensure`")) {
				t.Errorf("the matcher issue must point at removing the matcher, not at hooks ensure: %q", line)
			}
		}
	})
	t.Run("disableAllHooks", func(t *testing.T) {
		all := settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair)
		withConfig(t, strings.Replace(all, "{", `{"disableAllHooks":true,`, 1))
		sec, err := runDoctorHooks(t)
		if err == nil || !strings.Contains(sec, "disableAllHooks") {
			t.Fatalf("disableAllHooks passed: %v\n%s", err, sec)
		}
	})
}

func TestDoctorMalformedSettings(t *testing.T) {
	doctorRepo(t)
	withConfig(t, `{"hooks": `)
	sec, err := runDoctorHooks(t)
	if err == nil || !strings.Contains(sec, "cannot be read") || !strings.Contains(sec, "unexpected end of JSON input") {
		t.Fatalf("a malformed settings.json: %v\n%s", err, sec)
	}
}

// TestDoctorReadsEffectiveSettings: with CLAUDE_CONFIG_DIR set, Claude Code
// reads that dir's settings.json — and so must doctor, however wired the
// ~/.claude one is.
func TestDoctorReadsEffectiveSettings(t *testing.T) {
	doctorRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"),
		settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair))
	cfg := withConfig(t, "")
	sec, err := runDoctorHooks(t)
	if err == nil || !strings.Contains(sec, "✗ PreToolUse") {
		t.Fatalf("doctor read ~/.claude instead of %s: %v\n%s", cfg, err, sec)
	}
	if _, statErr := os.Stat(filepath.Join(cfg, "settings.json")); !os.IsNotExist(statErr) {
		t.Errorf("doctor wrote settings.json (err=%v); it only reads", statErr)
	}
}
