package memadvise

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// turnsEnv points CLAUDE_CONFIG_DIR at a fresh dir and returns it with a
// fresh repo root, so no test reads the real transcripts.
func turnsEnv(t *testing.T) (claude, root string) {
	t.Helper()
	claude, root = t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	return claude, root
}

// projectDir creates <claude>/projects/<slug> and returns it.
func projectDir(t *testing.T, claude, slug string) string {
	t.Helper()
	dir := filepath.Join(claude, "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tline is one transcript line, encoded the way Claude Code writes it.
func tline(t *testing.T, typ, id, model string, sidechain bool, at time.Time) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":        typ,
		"isSidechain": sidechain,
		"timestamp":   at.UTC().Format(time.RFC3339Nano),
		"message":     map[string]any{"id": id, "model": model, "content": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// turn is a main-loop assistant line: the one shape that counts.
func turn(t *testing.T, id string, at time.Time) string {
	t.Helper()
	return tline(t, "assistant", id, "claude-opus-5-5", false, at)
}

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTranscript(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func countTurns(t *testing.T, root string, now time.Time) TurnCounts {
	t.Helper()
	c, err := Turns(root, now)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	return c
}

func TestTurnsDedupeMessageID(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	// One turn written as three content-block lines, a tool result between
	// two of them.
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"),
		turn(t, "msg_a", at),
		turn(t, "msg_a", at),
		tline(t, "user", "", "", false, at),
		turn(t, "msg_a", at.Add(time.Second)),
	)
	if c := countTurns(t, root, now); c.Repo != 1 || c.All != 1 {
		t.Errorf("three lines of one message.id counted as Repo=%d All=%d, want 1", c.Repo, c.All)
	}
}

func TestTurnsMainAgentOnly(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	proj := projectDir(t, claude, Slug(root))
	writeTranscript(t, filepath.Join(proj, "s.jsonl"),
		turn(t, "msg_main", at),
		tline(t, "assistant", "msg_side", "claude-opus-5-5", true, at),
		tline(t, "assistant", "msg_synth", syntheticModel, false, at),
		tline(t, "user", "msg_user", "claude-opus-5-5", false, at),
	)
	// Subagent transcripts, at both depths Claude Code has filed them.
	writeTranscript(t, filepath.Join(proj, "subagents", "agent-1.jsonl"), turn(t, "msg_sub1", at))
	writeTranscript(t, filepath.Join(proj, "sess", "subagents", "agent-2.jsonl"), turn(t, "msg_sub2", at))
	if c := countTurns(t, root, now); c.Repo != 1 || c.All != 1 {
		t.Errorf("Repo=%d All=%d, want only the one main-loop assistant turn", c.Repo, c.All)
	}
}

func TestTurnsWindow(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"),
		turn(t, "msg_old", now.Add(-31*24*time.Hour)),
		turn(t, "msg_in", now.Add(-29*24*time.Hour)),
		turn(t, "msg_new", now.Add(-time.Hour)),
	)
	if c := countTurns(t, root, now); c.Repo != 2 {
		t.Errorf("Repo=%d, want 2 — the turn 31 days old is outside the window", c.Repo)
	}
}

func TestTurnsSkipsStaleFilesByMtime(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 file, so an open cannot be detected")
	}
	unreadable := func(t *testing.T, mtime time.Time) int {
		t.Helper()
		claude, root := turnsEnv(t)
		now := time.Now()
		path := filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl")
		writeTranscript(t, path, turn(t, "msg_a", now.Add(-time.Hour)))
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return countTurns(t, root, now).Unreadable
	}
	// The probe: a recent mode-000 file is opened, and the failure shows.
	if n := unreadable(t, time.Now()); n != 1 {
		t.Fatalf("a recent unreadable transcript reported Unreadable=%d, want 1 — the probe below detects nothing", n)
	}
	if n := unreadable(t, time.Now().Add(-31*24*time.Hour)); n != 0 {
		t.Errorf("a transcript untouched for 31 days was opened (Unreadable=%d)", n)
	}
}

func TestTurnsSurviveMultiMegabyteLine(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	big, err := json.Marshal(map[string]any{
		"type":      "user",
		"timestamp": at.UTC().Format(time.RFC3339Nano),
		"message":   map[string]any{"content": strings.Repeat("x", 5<<20)},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"),
		turn(t, "msg_a", at), string(big)+"\n", turn(t, "msg_b", at))
	if c := countTurns(t, root, now); c.Repo != 2 || c.Unreadable != 0 {
		t.Errorf("Repo=%d Unreadable=%d around a 5 MB line, want 2 and 0", c.Repo, c.Unreadable)
	}
}

func TestTurnsSkipMalformedLine(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"),
		turn(t, "msg_a", at),
		`{"type":"assistant","message":{"id":"msg_torn"`+"\n",
		turn(t, "msg_b", at),
	)
	c, err := Turns(root, now)
	if err != nil {
		t.Fatalf("a malformed line was fatal: %v", err)
	}
	if c.Repo != 2 {
		t.Errorf("Repo=%d, want the 2 turns either side of the malformed line", c.Repo)
	}
}

func TestTurnsShareAcrossProjects(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), turn(t, "msg_a", at))
	writeTranscript(t, filepath.Join(projectDir(t, claude, "-srv-other"), "s.jsonl"),
		turn(t, "msg_b", at), turn(t, "msg_c", at), turn(t, "msg_d", at))
	c := countTurns(t, root, now)
	if c.Repo != 1 || c.All != 4 {
		t.Fatalf("Repo=%d All=%d, want 1 and 4", c.Repo, c.All)
	}
	if got := c.RepoShare(); got != 0.25 {
		t.Errorf("RepoShare = %v, want 0.25", got)
	}
	if got := (TurnCounts{}).RepoShare(); got != 0 {
		t.Errorf("RepoShare with no turns = %v, want 0", got)
	}
}

func TestTurnsProjectsNotADir(t *testing.T) {
	claude, root := turnsEnv(t)
	if err := os.WriteFile(filepath.Join(claude, "projects"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := countTurns(t, root, time.Now()); c.Unreadable != 1 {
		t.Errorf("a projects file reported Unreadable=%d, want 1", c.Unreadable)
	}
}

func TestTurnsUnreadableProjectDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-000 dir, so a failed read cannot be detected")
	}
	claude, root := turnsEnv(t)
	now := time.Now()
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), turn(t, "msg_a", now.Add(-time.Hour)))
	locked := projectDir(t, claude, "-srv-locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if c := countTurns(t, root, now); c.Unreadable != 1 || c.Repo != 1 {
		t.Errorf("a mode-000 sibling project reported Unreadable=%d Repo=%d, want 1 and 1", c.Unreadable, c.Repo)
	}
}

func TestTurnsPastTailCap(t *testing.T) {
	claude, root := turnsEnv(t)
	now := time.Now()
	at := now.Add(-time.Hour)
	var lines []string
	for i := range tailIDs + 1 {
		lines = append(lines, turn(t, fmt.Sprintf("msg_%d", i), at))
	}
	writeTranscript(t, filepath.Join(projectDir(t, claude, Slug(root)), "s.jsonl"), lines...)
	if c := countTurns(t, root, now); c.Repo != tailIDs+1 {
		t.Errorf("Repo=%d, want %d distinct turns past the tail cap", c.Repo, tailIDs+1)
	}
}

// TestUpdateRecountsInPlaceRewrite: a transcript rewritten in place to the
// same or a larger size has a stat that only says "changed", like an append.
// Resuming at the old offset would keep the old turns and count whatever
// lies past it; the Mark check recounts the new content from byte 0 instead.
func TestUpdateRecountsInPlaceRewrite(t *testing.T) {
	start := windowStart(time.Now())
	at := time.Now().Add(-time.Hour)
	// counted reads path from scratch, as a first status run would.
	counted := func(t *testing.T, path string) fileEntry {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		e, _, err := update(path, fileEntry{}, false, info, start)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	// rerun brings e up to date after the file changed, with an mtime the
	// first stat cannot have had.
	rerun := func(t *testing.T, path string, e fileEntry) (fileEntry, int64) {
		t.Helper()
		later := time.Now().Add(time.Minute)
		if err := os.Chtimes(path, later, later); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		e, n, err := update(path, e, true, info, start)
		if err != nil {
			t.Fatal(err)
		}
		return e, n
	}
	total := func(e fileEntry) int {
		n := 0
		for _, c := range e.Days {
			n += c
		}
		return n
	}
	oldA, oldB := turn(t, "msg_a", at), turn(t, "msg_b", at)
	// pad is a line of len(oldB) bytes that holds no turn.
	pad := strings.Repeat(" ", len(oldB)-1) + "\n"

	t.Run("control: an append resumes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		writeTranscript(t, path, oldA, oldB)
		e := counted(t, path)
		appended := turn(t, "msg_c", at)
		appendTranscript(t, path, appended)
		e, n := rerun(t, path, e)
		if total(e) != 3 || n != int64(len(appended)) {
			t.Fatalf("append: %d turns, %d bytes read; want 3 and only the %d appended", total(e), n, len(appended))
		}
	})
	for _, tc := range []struct {
		name  string
		lines []string
		want  int
	}{
		// Same size: a resume reads nothing and keeps msg_a and msg_b.
		{"same size", []string{turn(t, "msg_x", at), pad}, 1},
		// Larger: a resume lands at the end of pad and adds msg_y to the
		// two stale turns, three in all.
		{"larger", []string{turn(t, "msg_x", at), pad, turn(t, "msg_y", at)}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			writeTranscript(t, path, oldA, oldB)
			e := counted(t, path)
			if total(e) != 2 {
				t.Fatalf("first count = %d, want 2", total(e))
			}
			writeTranscript(t, path, tc.lines...)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			e, n := rerun(t, path, e)
			if total(e) != tc.want {
				t.Errorf("after an in-place rewrite: %d turns, want the new content's %d", total(e), tc.want)
			}
			if n != info.Size() {
				t.Errorf("the rewrite was read from byte %d, want a recount of all %d bytes", info.Size()-n, info.Size())
			}
		})
	}
}
