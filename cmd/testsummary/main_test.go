package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const historyNote = "testsummary: no timing history"

// runEnv runs the command over stream with env as its whole environment.
func runEnv(stream string, env map[string]string) (string, int) {
	var out strings.Builder
	code := run(strings.NewReader(stream), &out, func(k string) string { return env[k] })
	return out.String(), code
}

// noteLines is the history note lines in out.
func noteLines(out string) []string {
	var ns []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, historyNote) {
			ns = append(ns, l)
		}
	}
	return ns
}

// writeHistory saves h to a fresh file and returns its path.
func writeHistory(t *testing.T, h history) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.json")
	if err := saveHistory(path, h); err != nil {
		t.Fatal(err)
	}
	return path
}

// readSaved loads the history at path, failing on a degraded load.
func readSaved(t *testing.T, path string) history {
	t.Helper()
	h, note := loadHistory(path)
	if note != "" {
		t.Fatalf("saved history did not load: %s", note)
	}
	return h
}

// TestRunHistoryMissing (c-1, c-5): a missing history is one note line, the
// verdict as exit code, and afterwards a file holding this run's totals
// under GITHUB_SHA.
func TestRunHistoryMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "history.json")
	env := map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "abc"}
	for _, c := range []struct {
		stream string
		want   int
	}{
		{pkgStream("example.com/p", 400), 0},
		{timed("start", "example.com/p", "", 0) + timed("fail", "example.com/p", "TestA", 1) + timed("fail", "example.com/p", "", 400), 1},
	} {
		if err := os.RemoveAll(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		out, code := runEnv(c.stream, env)
		if code != c.want {
			t.Errorf("exit = %d, want the verdict %d", code, c.want)
		}
		if ns := noteLines(out); len(ns) != 1 {
			t.Errorf("want exactly one note line, got %q", ns)
		}
		if ws := warningsTitled(out, "test trend"); len(ws) != 0 {
			t.Errorf("a missing history drew a trend warning: %q", ws)
		}
		h := readSaved(t, path)
		if len(h.Entries) != 1 || h.Entries[0].SHA != "abc" || h.Entries[0].Packages["example.com/p"] != 400 {
			t.Errorf("saved history = %+v, want one abc entry with example.com/p at 400", h)
		}
	}
}

// TestRunHistoryCorrupt (c-5): a corrupt history is one note line, no trend
// warning, and the same exit code as the stream with no history at all.
func TestRunHistoryCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	streams := []string{
		pkgStream("example.com/p", 400),
		timed("start", "example.com/p", "", 0) + timed("fail", "example.com/p", "", 400),
	}
	for _, s := range streams {
		// each run saves over it, so corrupt it afresh
		if err := os.WriteFile(path, []byte("{corrupt"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, bare := runEnv(s, nil)
		out, code := runEnv(s, map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "abc"})
		if code != bare {
			t.Errorf("exit = %d with a corrupt history, %d without", code, bare)
		}
		if ns := noteLines(out); len(ns) != 1 {
			t.Errorf("want exactly one note line, got %q", ns)
		}
		if ws := warningsTitled(out, "test trend"); len(ws) != 0 {
			t.Errorf("a corrupt history drew a trend warning: %q", ws)
		}
	}
}

// TestRunSameSHATwice (c-6 end to end): two runs of one commit leave one
// entry, holding the second run's totals.
func TestRunSameSHATwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	env := map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "abc"}
	runEnv(pkgStream("example.com/p", 10), env)
	out, _ := runEnv(pkgStream("example.com/p", 12), env)
	if ns := noteLines(out); len(ns) != 0 {
		t.Errorf("the second run found no history: %q", ns)
	}
	h := readSaved(t, path)
	if len(h.Entries) != 1 || h.Entries[0].SHA != "abc" || h.Entries[0].Packages["example.com/p"] != 12 {
		t.Errorf("saved history = %+v, want one abc entry at 12", h)
	}
}

// TestRunHistoryUnset: without TESTSUMMARY_HISTORY nothing is read or
// written, no note is printed, and every baseline is "—".
func TestRunHistoryUnset(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out, code := runEnv(pkgStream("example.com/p", 400), map[string]string{"GITHUB_SHA": "abc"})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if ns := noteLines(out); len(ns) != 0 {
		t.Errorf("history unset printed %q", ns)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("history unset wrote %v (err %v)", entries, err)
	}
	if rows := tableRows(t, out, "Package totals"); len(rows) != 1 || rows[0][2] != "—" || rows[0][3] != "" {
		t.Errorf("package totals = %q, want baseline — and no delta", rows)
	}
}

// TestRunWarningsKeepVerdict: cap and trend warnings never change the exit
// code, either way.
func TestRunWarningsKeepVerdict(t *testing.T) {
	rising := history{}
	for i, s := range []float64{400, 410, 420, 430, 440} {
		rising.Entries = append(rising.Entries, historyEntry{SHA: string(rune('a' + i)), Packages: map[string]float64{
			"example.com/trend": s, "example.com/cap": s,
		}})
	}
	pass := pkgStream("example.com/trend", 400) + pkgStream("example.com/cap", 545)
	fail := pass + timed("start", "example.com/bad", "", 0) + timed("fail", "example.com/bad", "", 1)
	for _, c := range []struct {
		stream string
		want   int
	}{{pass, 0}, {fail, 1}} {
		path := writeHistory(t, rising)
		out, code := runEnv(c.stream, map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "now"})
		if code != c.want {
			t.Errorf("exit = %d, want the verdict %d", code, c.want)
		}
		if len(warningsTitled(out, "test cap")) != 1 || len(warningsTitled(out, "test trend")) != 1 {
			t.Errorf("want one cap and one trend warning:\n%s", out)
		}
	}
}

// TestRunReRunExcludesSelf: a history whose newest entry is this commit (a
// re-run) neither moves the baseline nor draws a trend warning from that
// entry, and the saved file still holds one entry for the sha.
func TestRunReRunExcludesSelf(t *testing.T) {
	const pkg = "example.com/p"
	var h history
	for i, s := range []float64{300, 300, 300, 300, 310, 310, 310, 310} {
		h.Entries = append(h.Entries, historyEntry{SHA: string(rune('a' + i)), Packages: map[string]float64{pkg: s}})
	}
	h.Entries = append(h.Entries, historyEntry{SHA: "self", Packages: map[string]float64{pkg: 1000}})
	path := writeHistory(t, h)

	out, _ := runEnv(pkgStream(pkg, 400), map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "self"})
	// With the 1000s self-entry counted the median would be 310 and the fit
	// would warn; without it the median is 305 and the line stays flat.
	if rows := tableRows(t, out, "Package totals"); len(rows) != 1 || rows[0][2] != "305.00" || rows[0][3] != "+95.00" {
		t.Errorf("package totals = %q, want baseline 305.00 and delta +95.00", rows)
	}
	if ws := warningsTitled(out, "test trend"); len(ws) != 0 {
		t.Errorf("the self-entry drew a trend warning: %q", ws)
	}
	saved := readSaved(t, path)
	n := 0
	for _, e := range saved.Entries {
		if e.SHA == "self" {
			n++
			if e.Packages[pkg] != 400 {
				t.Errorf("self entry = %v, want this run's 400", e.Packages)
			}
		}
	}
	if n != 1 || len(saved.Entries) != 9 {
		t.Errorf("saved %d self entries of %d, want 1 of 9", n, len(saved.Entries))
	}
}

// TestRunEmptyStreamSavesNothing: a run with no package totals leaves the
// history as it was rather than pushing a real entry out with an empty one.
func TestRunEmptyStreamSavesNothing(t *testing.T) {
	path := writeHistory(t, series(10, 20))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, code := runEnv("", map[string]string{"TESTSUMMARY_HISTORY": path, "GITHUB_SHA": "abc"}); code != 1 {
		t.Errorf("exit = %d for an empty stream, want 1", code)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("an empty run rewrote the history:\n%s", after)
	}
}

// TestRunUnsavableHistoryWarns: a history that cannot be written is a
// warning, never a verdict change.
func TestRunUnsavableHistoryWarns(t *testing.T) {
	dir := t.TempDir() // a directory is not a writable history file
	out, code := runEnv(pkgStream("example.com/p", 1), map[string]string{"TESTSUMMARY_HISTORY": dir, "GITHUB_SHA": "abc"})
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if ws := warningsTitled(out, "test timing history"); len(ws) != 1 {
		t.Errorf("want one timing-history warning, got:\n%s", out)
	}
}
