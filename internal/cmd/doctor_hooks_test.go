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
	subagentStopPair = [2]string{"SubagentStop", GateRecordHook}
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
	// The swapped config dir starts with a current solo reviewer, as chdir's
	// does: these tests are about the hooks, and doctor fails without one.
	if err := syncAgents(filepath.Join(cfg, "agents"), false, "", func(string, ...any) {}); err != nil {
		t.Fatalf("seed the reviewer definition: %v", err)
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
	for _, want := range []string{"✓ PreCompact", "✓ SessionStart", "✓ PreToolUse → dross gate check", "✓ PostToolUse → dross gate record", "✓ SubagentStop → dross gate record"} {
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
		{"no PreCompact", [][2]string{sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair}, "⚠ PreCompact"},
		{"no SessionStart", [][2]string{preCompactPair, gateCheckPair, gateRecordPair, subagentStopPair}, "⚠ SessionStart"},
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
		withConfig(t, settingsWith(t, "Bash", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair))
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
		all := settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair)
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
		settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair))
	cfg := withConfig(t, "")
	sec, err := runDoctorHooks(t)
	if err == nil || !strings.Contains(sec, "✗ PreToolUse") {
		t.Fatalf("doctor read ~/.claude instead of %s: %v\n%s", cfg, err, sec)
	}
	if _, statErr := os.Stat(filepath.Join(cfg, "settings.json")); !os.IsNotExist(statErr) {
		t.Errorf("doctor wrote settings.json (err=%v); it only reads", statErr)
	}
}

// TestDoctorProjectHooksDisabled: a repo's own .claude/settings.json or
// settings.local.json setting "disableAllHooks": true turns the gates off in
// that repo however well the user-level file wires them.
func TestDoctorProjectHooksDisabled(t *testing.T) {
	repo := doctorRepo(t)
	withConfig(t, settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair))
	baseline, err := runDoctorHooks(t)
	if err != nil {
		t.Fatalf("doctor with every hook wired and no .claude/: %v\n%s", err, baseline)
	}
	for _, name := range []string{"settings.json", "settings.local.json"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(repo, ".claude", name)
			t.Cleanup(func() { _ = os.Remove(p) })
			mustWrite(t, p, `{"disableAllHooks": true}`)
			sec, err := runDoctorHooks(t)
			if err == nil || !strings.Contains(sec, "✗ .claude/"+name+` sets "disableAllHooks": true`) {
				t.Errorf("a repo-level disableAllHooks in %s passed: %v\n%s", name, err, sec)
			}

			mustWrite(t, p, `{"disableAllHooks": false, "permissions": {}}`)
			sec, err = runDoctorHooks(t)
			if err != nil || sec != baseline {
				t.Errorf("repo-level %s that leaves hooks on changed the Hooks section (%v):\n--- got ---\n%s\n--- want ---\n%s", name, err, sec, baseline)
			}
		})
	}
}

func TestDoctorProjectSettingsUnparseable(t *testing.T) {
	repo := doctorRepo(t)
	withConfig(t, settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair, subagentStopPair))
	mustWrite(t, filepath.Join(repo, ".claude", "settings.local.json"), `{"hooks": `)
	sec, err := runDoctorHooks(t)
	if err == nil || !strings.Contains(sec, "✗ .claude/settings.local.json cannot be read") || !strings.Contains(sec, "unexpected end of JSON input") {
		t.Fatalf("a malformed repo-level settings.local.json: %v\n%s", err, sec)
	}
}

// TestDoctorSubagentStopHook: without SubagentStop → dross gate record a
// background solo reviewer's verdict never reaches the recorder, so solo
// reviews in an interactive session can never pass — an issue, not a warning.
func TestDoctorSubagentStopHook(t *testing.T) {
	doctorRepo(t)
	withConfig(t, settingsWith(t, "", preCompactPair, sessionStartPair, gateCheckPair, gateRecordPair))
	sec, err := runDoctorHooks(t)
	if err == nil || !strings.Contains(sec, "✗ SubagentStop → `dross gate record` is not wired") || !strings.Contains(sec, "dross hooks ensure") {
		t.Fatalf("a missing SubagentStop record hook: %v\n%s", err, sec)
	}
}
