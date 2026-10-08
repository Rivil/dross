package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const autoPause = "dross pause --auto"

func TestMergeHookIdempotent(t *testing.T) {
	tests := []struct {
		name    string
		initial string
	}{
		{"empty settings", ""},
		{"unrelated keys", `{"model": "opus", "statusLine": {"type": "command", "command": "/bin/sl"}}`},
		{"existing other event", `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := MergeHook([]byte(tt.initial), EventPreCompact, autoPause)
			if err != nil {
				t.Fatalf("first merge: %v", err)
			}
			second, err := MergeHook(first, EventPreCompact, autoPause)
			if err != nil {
				t.Fatalf("second merge: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Errorf("second merge not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
			if got := countCommand(t, second, EventPreCompact, autoPause); got != 1 {
				t.Errorf("want exactly 1 %q entry, got %d", autoPause, got)
			}
		})
	}
}

func TestMergeHookPreservesForeign(t *testing.T) {
	initial := `{
  "model": "opus",
  "hooks": {
    "PreCompact": [
      {"matcher": "manual", "hooks": [{"type": "command", "command": "/usr/local/bin/my-backup.sh"}]}
    ],
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "echo session"}]}
    ]
  },
  "permissions": {"allow": ["Bash(ls:*)"]}
}`
	out, err := MergeHook([]byte(initial), EventPreCompact, autoPause)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Top-level siblings survive in order.
	root := decodeOrderedKeys(t, out)
	want := []string{"model", "hooks", "permissions"}
	for i, k := range want {
		if root[i] != k {
			t.Fatalf("top-level key order changed: got %v, want %v", root, want)
		}
	}

	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	pc := parsed.Hooks["PreCompact"]
	if len(pc) != 2 {
		t.Fatalf("want 2 PreCompact groups (foreign + dross), got %d", len(pc))
	}
	// Foreign group survives untouched, still first.
	if pc[0].Matcher != "manual" || pc[0].Hooks[0].Command != "/usr/local/bin/my-backup.sh" {
		t.Errorf("foreign PreCompact group disturbed: %+v", pc[0])
	}
	if pc[1].Hooks[0].Command != autoPause {
		t.Errorf("dross entry not appended: %+v", pc[1])
	}
	// Foreign SessionStart untouched by a PreCompact merge.
	ss := parsed.Hooks["SessionStart"]
	if len(ss) != 1 || ss[0].Hooks[0].Command != "echo session" {
		t.Errorf("foreign SessionStart disturbed: %+v", ss)
	}
}

func TestMergeHookRejectsGarbage(t *testing.T) {
	tests := []struct {
		name     string
		settings string
	}{
		{"malformed root", `{"hooks": `},
		{"non-object root", `[1, 2]`},
		{"non-object hooks", `{"hooks": "nope"}`},
		{"non-array event", `{"hooks": {"PreCompact": {"bad": true}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MergeHook([]byte(tt.settings), EventPreCompact, autoPause)
			if err == nil {
				t.Fatalf("want error, got output:\n%s", out)
			}
			if out != nil {
				t.Errorf("want nil output on error (no partial clobber), got:\n%s", out)
			}
		})
	}
}

// countCommand counts entries under hooks.<event> whose command equals command.
func countCommand(t *testing.T, settings []byte, event, command string) int {
	t.Helper()
	var parsed struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(settings, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	n := 0
	for _, g := range parsed.Hooks[event] {
		for _, h := range g.Hooks {
			if h.Command == command {
				n++
			}
		}
	}
	return n
}

// decodeOrderedKeys returns the top-level key order of a JSON object.
func decodeOrderedKeys(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if _, err := dec.Token(); err != nil {
		t.Fatalf("token: %v", err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		if k, ok := tok.(string); ok {
			keys = append(keys, k)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return keys
}

func TestMutateSettingsCreatesFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")

	err := MutateSettings(path, func(doc map[string]any) {
		envMap := map[string]any{"FOO": "bar"}
		doc["env"] = envMap
	})
	if err != nil {
		t.Fatalf("mutate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0o600 perms (token storage), got %v", info.Mode().Perm())
	}

	doc, err := ReadSettings(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	envMap := doc["env"].(map[string]any)
	if !reflect.DeepEqual(envMap, map[string]any{"FOO": "bar"}) {
		t.Errorf("env mismatch: %+v", envMap)
	}
}

func TestMutateSettingsPreservesUnrelatedKeys(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"env":{"A":"1"},"model":"m","theme":"dark"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MutateSettings(path, func(doc map[string]any) {
		envMap := doc["env"].(map[string]any)
		envMap["B"] = "2"
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	doc, _ := ReadSettings(path)
	if doc["model"] != "m" || doc["theme"] != "dark" {
		t.Errorf("non-env keys lost: %+v", doc)
	}
	envMap := doc["env"].(map[string]any)
	if envMap["A"] != "1" || envMap["B"] != "2" {
		t.Errorf("env keys lost: %+v", envMap)
	}
}

// TestMutateSettingsWriteShape pins the env write's shape: a foreign key
// survives, the file is 0600 (it holds tokens), it ends in a newline, and the
// temp file it was staged through is gone.
func TestMutateSettingsWriteShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MutateSettings(path, func(doc map[string]any) { doc["env"] = map[string]any{"K": "V"} }); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"theme": "dark"`) || !strings.Contains(string(b), `"K": "V"`) {
		t.Errorf("settings lost a key:\n%s", b)
	}
	if !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("settings.json does not end in a newline: %q", b)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v (%v), want 0600", info.Mode().Perm(), err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the staging file %s.tmp was left behind: %v", path, err)
	}
}

// TestReadSettingsParseErrorAndEmpty: a malformed file fails naming it with
// the "parse <path>:" prefix, and an empty or missing one reads as {}.
func TestReadSettingsParseErrorAndEmpty(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(bad); err == nil || !strings.HasPrefix(err.Error(), "parse "+bad+":") {
		t.Errorf("malformed settings gave %v, want an error prefixed \"parse %s:\"", err, bad)
	}
	if err := MutateSettings(bad, func(map[string]any) {}); err == nil {
		t.Error("a mutate over malformed settings wrote anyway")
	}
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{empty, filepath.Join(dir, "absent.json")} {
		doc, err := ReadSettings(path)
		if err != nil || doc == nil || len(doc) != 0 {
			t.Errorf("ReadSettings(%s) = %v, %v; want {}", filepath.Base(path), doc, err)
		}
	}
}
