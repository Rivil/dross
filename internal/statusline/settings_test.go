package statusline

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

const drossCmd = "/Users/u/.local/bin/dross statusline"

// statusLineCommand extracts statusLine.command from settings bytes (or "").
func statusLineCommand(t *testing.T, settings []byte) string {
	t.Helper()
	var v struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(settings, &v); err != nil {
		t.Fatalf("unmarshal settings: %v\n%s", err, settings)
	}
	return v.StatusLine.Command
}

func asMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	m := map[string]any{}
	if len(b) == 0 {
		return m
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b)
	}
	return m
}

// TestMergeCreatesFromEmpty: a missing/empty settings.json yields one wired only
// with our statusLine (type=command, command=absolute).
func TestMergeCreatesFromEmpty(t *testing.T) {
	for _, in := range [][]byte{nil, []byte(""), []byte("  \n")} {
		out, err := MergeStatusline(in, drossCmd, false)
		if err != nil {
			t.Fatalf("MergeStatusline(%q): %v", in, err)
		}
		if got := statusLineCommand(t, out); got != drossCmd {
			t.Errorf("command = %q, want %q", got, drossCmd)
		}
		m := asMap(t, out)
		sl, _ := m["statusLine"].(map[string]any)
		if sl["type"] != "command" {
			t.Errorf("statusLine.type = %v, want command", sl["type"])
		}
		if len(m) != 1 {
			t.Errorf("fresh settings should hold only statusLine, got keys %v", keysOf(m))
		}
	}
}

// TestMergePreservesSiblings: every unrelated key survives, order is preserved, and
// only statusLine is appended.
func TestMergePreservesSiblings(t *testing.T) {
	in := []byte(`{
  "env": {
    "FOO": "bar"
  },
  "permissions": {
    "allow": [
      "Read"
    ]
  },
  "effortLevel": "high"
}
`)
	out, err := MergeStatusline(in, drossCmd, false)
	if err != nil {
		t.Fatalf("MergeStatusline: %v", err)
	}
	// Siblings present and value-equal.
	gotMap, wantMap := asMap(t, out), asMap(t, in)
	for _, k := range []string{"env", "permissions", "effortLevel"} {
		if !reflect.DeepEqual(gotMap[k], wantMap[k]) {
			t.Errorf("sibling %s changed: got %v, want %v", k, gotMap[k], wantMap[k])
		}
	}
	// Order preserved: original keys, then statusLine appended.
	root, err := parseOrdered(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"env", "permissions", "effortLevel", "statusLine"}
	if !reflect.DeepEqual(root.keys, want) {
		t.Errorf("key order = %v, want %v", root.keys, want)
	}
}

// TestMergeIdempotent: merging twice is byte-stable.
func TestMergeIdempotent(t *testing.T) {
	in := []byte(`{"effortLevel":"high"}`)
	once, err := MergeStatusline(in, drossCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := MergeStatusline(once, drossCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("not idempotent:\n once: %q\ntwice: %q", once, twice)
	}
}

// TestMergeAlreadyOurs returns the input bytes unchanged when already wired to us.
func TestMergeAlreadyOurs(t *testing.T) {
	once, _ := MergeStatusline([]byte(`{"effortLevel":"high"}`), drossCmd, false)
	out, err := MergeStatusline(once, drossCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(once) {
		t.Errorf("already-ours merge mutated bytes:\n in: %q\nout: %q", once, out)
	}
}

// TestMergeRefusesClobber: a different existing statusLine.command is refused
// (sentinel) and the input bytes are returned unchanged.
func TestMergeRefusesClobber(t *testing.T) {
	in := []byte(`{"statusLine":{"type":"command","command":"my-bar --x","refreshInterval":5},"effortLevel":"high"}`)
	out, err := MergeStatusline(in, drossCmd, false)
	if !errors.Is(err, ErrStatusLineClobber) {
		t.Fatalf("want ErrStatusLineClobber, got %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("clobber must not mutate input: got %q", out)
	}
}

// TestMergeForceOverwrites: with force, a foreign statusLine.command is overwritten
// while its other sub-keys (refreshInterval) survive.
func TestMergeForceOverwrites(t *testing.T) {
	in := []byte(`{"statusLine":{"type":"command","command":"my-bar --x","refreshInterval":5}}`)
	out, err := MergeStatusline(in, drossCmd, true)
	if err != nil {
		t.Fatalf("force merge: %v", err)
	}
	if got := statusLineCommand(t, out); got != drossCmd {
		t.Errorf("command = %q, want %q (overwrite)", got, drossCmd)
	}
	sl := asMap(t, out)["statusLine"].(map[string]any)
	if sl["refreshInterval"] != float64(5) {
		t.Errorf("refreshInterval not preserved on force overwrite: %v", sl["refreshInterval"])
	}
}

// TestRemoveOurs removes dross's entry and preserves every sibling.
func TestRemoveOurs(t *testing.T) {
	merged, _ := MergeStatusline([]byte(`{"effortLevel":"high","env":{"A":"1"}}`), drossCmd, false)
	out, err := RemoveStatusline(merged, drossCmd)
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(t, out)
	if _, ok := m["statusLine"]; ok {
		t.Errorf("statusLine not removed: %s", out)
	}
	if m["effortLevel"] != "high" || !reflect.DeepEqual(m["env"], map[string]any{"A": "1"}) {
		t.Errorf("siblings lost on remove: %v", m)
	}
}

// TestRemoveNoStatusLine is a byte-identical no-op when there is nothing to remove.
func TestRemoveNoStatusLine(t *testing.T) {
	in := []byte(`{"effortLevel":"high"}`)
	out, err := RemoveStatusline(in, drossCmd)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Errorf("no-op remove changed bytes: %q", out)
	}
}

// TestRemoveForeignUntouched never removes a status line the user configured.
func TestRemoveForeignUntouched(t *testing.T) {
	in := []byte(`{"statusLine":{"type":"command","command":"my-bar --x"}}`)
	out, err := RemoveStatusline(in, drossCmd)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Errorf("foreign statusLine must be left untouched: %q", out)
	}
	if got := statusLineCommand(t, out); got != "my-bar --x" {
		t.Errorf("foreign command altered: %q", got)
	}
}

// TestRoundTrip: Merge then Remove returns settings semantically equal to the
// pre-merge original (which had no statusLine).
func TestRoundTrip(t *testing.T) {
	orig := []byte(`{"env":{"A":"1"},"effortLevel":"high"}`)
	merged, err := MergeStatusline(orig, drossCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	back, err := RemoveStatusline(merged, drossCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asMap(t, back), asMap(t, orig)) {
		t.Errorf("round-trip not equal:\norig: %s\nback: %s", orig, back)
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// TestMalformedSettingsErrors: non-object / malformed input is an error (caller
// leaves the file untouched), never a silent overwrite.
func TestMalformedSettingsErrors(t *testing.T) {
	for _, in := range []string{`[1,2,3]`, `"a string"`, `{not json`} {
		if _, err := MergeStatusline([]byte(in), drossCmd, false); err == nil {
			t.Errorf("MergeStatusline(%q): want error, got nil", in)
		}
	}
	// A leading-space-then-object is still valid.
	if _, err := MergeStatusline([]byte("  {}\n"), drossCmd, false); err != nil {
		t.Errorf("valid object with leading whitespace errored: %v", err)
	}
}
