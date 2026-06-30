package statusline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// envMap returns an env(name) func backed by m (no os.Getenv).
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// noGit is a gitBranch stub so gather tests never shell out.
func noGit(string) string { return "" }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var fixedNow = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

// TestGatherConfigDirOverride: CLAUDE_CONFIG_DIR is read and the default ~/.claude is
// NOT consulted when it is set.
func TestGatherConfigDirOverride(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	// A todo under the DEFAULT ~/.claude that must be ignored.
	writeFile(t, filepath.Join(home, ".claude", "todos", "sess-agent-1.json"),
		`[{"status":"in_progress","activeForm":"WRONG default"}]`)
	// The real todo under the override.
	writeFile(t, filepath.Join(cfg, "todos", "sess-agent-1.json"),
		`[{"status":"in_progress","activeForm":"Active task"}]`)

	in, err := Gather([]byte(`{"session_id":"sess","workspace":{"current_dir":"/x"}}`),
		envMap(map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": cfg}), fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	if in.TodoActiveForm != "Active task" {
		t.Errorf("todo = %q, want the override's %q (default ~/.claude must be ignored)", in.TodoActiveForm, "Active task")
	}
}

// TestGatherNewestTodoWinsAndBeatsState: newest matching todo by mtime wins, and a
// present todo suppresses the dross state (todo-wins) even when state.json exists.
func TestGatherNewestTodoWinsAndBeatsState(t *testing.T) {
	cfg := t.TempDir()
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".dross", "state.json"),
		`{"current_milestone":"v0.8","current_phase":"p","current_phase_status":"planned"}`)

	old := filepath.Join(cfg, "todos", "sess-agent-1.json")
	newer := filepath.Join(cfg, "todos", "sess-agent-2.json")
	writeFile(t, old, `[{"status":"in_progress","activeForm":"Old"}]`)
	writeFile(t, newer, `[{"status":"in_progress","activeForm":"New"}]`)
	// Pin mtimes: old older, newer newer.
	os.Chtimes(old, fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour))
	os.Chtimes(newer, fixedNow, fixedNow)

	in, err := Gather([]byte(`{"session_id":"sess","workspace":{"current_dir":"`+proj+`"}}`),
		envMap(map[string]string{"CLAUDE_CONFIG_DIR": cfg}), fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	if in.TodoActiveForm != "New" {
		t.Errorf("todo = %q, want newest %q", in.TodoActiveForm, "New")
	}
	if in.DrossState != "" {
		t.Errorf("DrossState = %q, want empty (todo wins)", in.DrossState)
	}
}

// TestGatherTodoFilter: only files matching startsWith(session) && contains("-agent-")
// && endsWith(".json") are considered, even when the non-matching ones are newer.
func TestGatherTodoFilter(t *testing.T) {
	cfg := t.TempDir()
	todos := filepath.Join(cfg, "todos")
	writeFile(t, filepath.Join(todos, "sess-agent-1.json"), `[{"status":"in_progress","activeForm":"Good"}]`)
	// Decoys, all made newer so a broken filter would pick one of them.
	writeFile(t, filepath.Join(todos, "other-agent-9.json"), `[{"status":"in_progress","activeForm":"WrongPrefix"}]`)
	writeFile(t, filepath.Join(todos, "sess-noagent-9.json"), `[{"status":"in_progress","activeForm":"NoAgent"}]`)
	writeFile(t, filepath.Join(todos, "sess-agent-9.txt"), `[{"status":"in_progress","activeForm":"NotJson"}]`)
	for _, n := range []string{"other-agent-9.json", "sess-noagent-9.json", "sess-agent-9.txt"} {
		os.Chtimes(filepath.Join(todos, n), fixedNow.Add(time.Hour), fixedNow.Add(time.Hour))
	}
	os.Chtimes(filepath.Join(todos, "sess-agent-1.json"), fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour))

	in, err := Gather([]byte(`{"session_id":"sess","workspace":{"current_dir":"/x"}}`),
		envMap(map[string]string{"CLAUDE_CONFIG_DIR": cfg}), fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	if in.TodoActiveForm != "Good" {
		t.Errorf("todo = %q, want %q — non-matching files must be excluded", in.TodoActiveForm, "Good")
	}
}

// TestGatherInputDefaults: model -> "Claude", current_dir -> cwd, peer name ->
// daemonShort/id fallback (A1).
func TestGatherInputDefaults(t *testing.T) {
	cfg := t.TempDir()
	// A peer with no name but a daemonShort, and one with neither (=> id).
	writeFile(t, filepath.Join(cfg, "jobs", "j1", "state.json"), `{"daemonShort":"short1","state":"working"}`)
	writeFile(t, filepath.Join(cfg, "jobs", "j2", "state.json"), `{"state":"working"}`)

	in, err := Gather([]byte(`{"session_id":""}`),
		envMap(map[string]string{"CLAUDE_CONFIG_DIR": cfg}), fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	if in.Model != "Claude" {
		t.Errorf("model = %q, want default Claude", in.Model)
	}
	wd, _ := os.Getwd()
	if in.Dir != wd {
		t.Errorf("dir = %q, want cwd %q", in.Dir, wd)
	}
	names := map[string]bool{}
	for _, p := range in.Peers {
		names[p.Name] = true
	}
	if !names["short1"] || !names["j2"] {
		t.Errorf("peer name fallbacks wrong: got %v, want daemonShort 'short1' and id 'j2'", names)
	}
}

// TestGatherStateWalkUpAndHomeBoundary: state.json is found by walking up, and the
// walk stops at the home boundary (a .dross above HOME is never read).
func TestGatherStateWalkUpAndHomeBoundary(t *testing.T) {
	t.Run("walk up finds it", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "a", ".dross", "state.json"),
			`{"current_milestone":"v0.8","current_phase":"native-statusline","current_phase_status":"planned"}`)
		dir := filepath.Join(home, "a", "b", "c")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		in, err := Gather([]byte(`{"session_id":"","workspace":{"current_dir":"`+dir+`"}}`),
			envMap(map[string]string{"HOME": home}), fixedNow, noGit)
		if err != nil {
			t.Fatal(err)
		}
		if in.DrossState != "v0.8 · native-statusline · planned" {
			t.Errorf("DrossState = %q, want the walked-up state", in.DrossState)
		}
	})

	t.Run("stops at home boundary", func(t *testing.T) {
		root := t.TempDir()
		// state.json ABOVE the home boundary — must never be reached.
		writeFile(t, filepath.Join(root, ".dross", "state.json"),
			`{"current_milestone":"vX","current_phase":"above-home","current_phase_status":"x"}`)
		home := filepath.Join(root, "sub")
		dir := filepath.Join(home, "x", "y")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		in, err := Gather([]byte(`{"session_id":"","workspace":{"current_dir":"`+dir+`"}}`),
			envMap(map[string]string{"HOME": home}), fixedNow, noGit)
		if err != nil {
			t.Fatal(err)
		}
		if in.DrossState != "" {
			t.Errorf("DrossState = %q, want empty — walk must stop at home and not read the .dross above it", in.DrossState)
		}
	})
}

// TestGatherStatePartialField: a state missing current_phase_status degrades to
// "milestone · phase" (no trailing separator / no doubling).
func TestGatherStatePartialField(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "proj")
	writeFile(t, filepath.Join(dir, ".dross", "state.json"),
		`{"current_milestone":"v0.8","current_phase":"native-statusline"}`)
	in, err := Gather([]byte(`{"session_id":"","workspace":{"current_dir":"`+dir+`"}}`),
		envMap(map[string]string{"HOME": home}), fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	if in.DrossState != "v0.8 · native-statusline" {
		t.Errorf("DrossState = %q, want graceful partial", in.DrossState)
	}
}

// TestGatherPeerStalenessAndSkip: 6h staleness filter (clock-free via the now
// parameter), skip-own-CLAUDE_JOB_DIR, and garbage state.json skipped not fatal.
func TestGatherPeerStalenessAndSkip(t *testing.T) {
	cfg := t.TempDir()
	jobs := filepath.Join(cfg, "jobs")
	writeFile(t, filepath.Join(jobs, "self", "state.json"), `{"name":"me","state":"working"}`)
	writeFile(t, filepath.Join(jobs, "fresh", "state.json"),
		`{"name":"fresh","state":"working","updatedAt":"`+fixedNow.Add(-5*time.Hour).Format(time.RFC3339)+`"}`)
	writeFile(t, filepath.Join(jobs, "stale", "state.json"),
		`{"name":"stale","state":"working","updatedAt":"`+fixedNow.Add(-7*time.Hour).Format(time.RFC3339)+`"}`)
	writeFile(t, filepath.Join(jobs, "noupdate", "state.json"), `{"name":"noupdate","state":"review"}`)
	writeFile(t, filepath.Join(jobs, "garbage", "state.json"), `not json at all`)

	in, err := Gather([]byte(`{"session_id":""}`),
		envMap(map[string]string{"CLAUDE_CONFIG_DIR": cfg, "CLAUDE_JOB_DIR": "/run/jobs/self"}),
		fixedNow, noGit)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range in.Peers {
		got[p.Name] = true
	}
	if !got["fresh"] || !got["noupdate"] {
		t.Errorf("expected fresh + noupdate peers, got %v", got)
	}
	if got["stale"] {
		t.Error("stale peer (>6h) should be filtered out")
	}
	if got["me"] {
		t.Error("own job (CLAUDE_JOB_DIR basename) should be skipped")
	}
	if len(in.Peers) != 2 {
		t.Errorf("got %d peers, want exactly 2 (garbage must be skipped, not fatal): %v", len(in.Peers), got)
	}
}

// TestGatherMalformedStdin returns an error (the command turns this into silent
// no-output) rather than a partial Inputs.
func TestGatherMalformedStdin(t *testing.T) {
	if _, err := Gather([]byte(`{not json`), envMap(nil), fixedNow, noGit); err == nil {
		t.Error("malformed stdin: want error, got nil")
	}
}
