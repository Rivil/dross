package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// timed renders one synthetic event line carrying an Elapsed.
func timed(action, pkg, test string, elapsed float64) string {
	b, err := json.Marshal(map[string]any{"Action": action, "Package": pkg, "Test": test, "Elapsed": elapsed})
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

// tableRows returns the cells of each row in the markdown table under heading.
func tableRows(t *testing.T, out, heading string) [][]string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "### "+heading+"\n")
	if !ok {
		t.Fatalf("no %q table in:\n%s", heading, out)
	}
	var rows [][]string
	for i, line := range strings.Split(strings.TrimLeft(rest, "\n"), "\n") {
		if !strings.HasPrefix(line, "|") {
			break
		}
		if i < 2 {
			continue // header and alignment rows
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
			cells = append(cells, strings.TrimSpace(c))
		}
		rows = append(rows, cells)
	}
	return rows
}

func seconds(t *testing.T, cell string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(cell, 64)
	if err != nil {
		t.Fatalf("seconds cell %q: %v", cell, err)
	}
	return f
}

// TestTableTop20: 25 tests across two packages yield exactly the 20 slowest,
// slowest first; subtests never take a row.
func TestTableTop20(t *testing.T) {
	var stream strings.Builder
	for p, pkg := range []string{"example.com/a", "example.com/b"} {
		stream.WriteString(timed("start", pkg, "", 0))
		for i := p; i < 25; i += 2 { // a gets even-numbered tests, b odd
			name := fmt.Sprintf("Test%02d", i)
			stream.WriteString(timed("pass", pkg, name, float64(i+1)))
			// a subtest slower than any test must still take no row
			stream.WriteString(timed("pass", pkg, name+"/sub", 1000))
		}
		stream.WriteString(timed("pass", pkg, "", 5))
	}
	out, code := reduce(t, stream.String())
	if code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, out)
	}
	rows := tableRows(t, out, "Slowest tests")
	if len(rows) != 20 {
		t.Fatalf("got %d rows, want 20:\n%s", len(rows), out)
	}
	for i, row := range rows {
		// the slowest is Test24 at 25s, then down by one second a row
		want := fmt.Sprintf("Test%02d", 24-i)
		if row[1] != want || seconds(t, row[2]) != float64(25-i) {
			t.Errorf("row %d = %v, want %s at %ds", i, row, want, 25-i)
		}
		if strings.Contains(row[1], "/") {
			t.Errorf("row %d is a subtest: %v", i, row)
		}
	}
	if strings.Contains(out, "| Test04 |") {
		t.Errorf("the 21st-slowest test (Test04) is listed:\n%s", out)
	}
}

// TestPackageTotals: a package's total is its own terminal event's Elapsed,
// not the sum of its test rows.
func TestPackageTotals(t *testing.T) {
	const pkg = "example.com/p"
	stream := timed("start", pkg, "", 0) +
		timed("pass", pkg, "TestA", 1) +
		timed("pass", pkg, "TestB", 2) +
		timed("pass", pkg, "", 10)
	out, _ := reduce(t, stream)
	rows := tableRows(t, out, "Package totals")
	if len(rows) != 1 || rows[0][0] != pkg || seconds(t, rows[0][1]) != 10 || rows[0][2] != "pass" {
		t.Fatalf("package totals = %v, want one %s row at 10s (the package event), not 3s (the test sum)", rows, pkg)
	}
}

func TestBudgetWarning(t *testing.T) {
	warnings := func(out string) []string {
		var ws []string
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "::warning") {
				ws = append(ws, l)
			}
		}
		return ws
	}
	pkgAt := func(pkg string, s float64) string {
		return timed("start", pkg, "", 0) + timed("pass", pkg, "TestA", 1) + timed("pass", pkg, "", s)
	}

	out, code := reduce(t, pkgAt("example.com/edge", 300.0))
	if ws := warnings(out); len(ws) != 0 {
		t.Errorf("a package at exactly 300.0s warned: %q", ws)
	}
	if code != 0 {
		t.Errorf("exit = %d at 300.0s, want 0", code)
	}

	out, _ = reduce(t, pkgAt("example.com/over", 300.1))
	ws := warnings(out)
	if len(ws) != 1 || !strings.Contains(ws[0], "example.com/over") || !strings.Contains(ws[0], "300.1") {
		t.Errorf("a package at 300.1s: want one ::warning naming it and 300.1, got %q", ws)
	}

	out, code = reduce(t, pkgAt("example.com/slow", 301)+pkgAt("example.com/fast", 2))
	if code != 0 {
		t.Errorf("an all-pass stream with a 301s package exited %d — the budget is a warning, never a failure", code)
	}
	if ws := warnings(out); len(ws) != 1 || !strings.Contains(ws[0], "example.com/slow") {
		t.Errorf("want one warning, for the slow package only, got %q", ws)
	}
}

// TestTableRendersTimedOutRun: the real timed-out fixture still yields a
// table — the package FAIL with its elapsed, the test it ended under named,
// and the test that passed before the panic listed.
func TestTableRendersTimedOutRun(t *testing.T) {
	out, code := reduce(t, fixtureStream(t, "hang"))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	pkgs := tableRows(t, out, "Package totals")
	if len(pkgs) != 1 || pkgs[0][0] != fixtureModule+"/hang" || pkgs[0][2] != "FAIL" || seconds(t, pkgs[0][1]) < 1 {
		t.Errorf("package totals = %v, want hang FAIL at >= 1s (its -timeout)", pkgs)
	}
	byName := map[string][]string{}
	for _, row := range tableRows(t, out, "Slowest tests") {
		byName[row[1]] = row
	}
	if row, ok := byName["TestBlocks"]; !ok || !strings.HasPrefix(row[3], "running") || seconds(t, row[2]) < 0.5 {
		t.Errorf("TestBlocks row = %v, want it listed as running at about the 1s timeout", row)
	}
	if row, ok := byName["TestQuick"]; !ok || row[3] != "pass" {
		t.Errorf("TestQuick row = %v, want the test that passed before the panic listed as pass", row)
	}
}

func TestSummaryDestination(t *testing.T) {
	const pkg = "example.com/p"
	green := timed("start", pkg, "", 0) + timed("pass", pkg, "TestA", 1) + timed("pass", pkg, "", 2)
	red := timed("start", pkg, "", 0) + timed("fail", pkg, "TestA", 1) + timed("fail", pkg, "", 2)
	withEnv := func(path string) func(string) string {
		return func(k string) string {
			if k == "GITHUB_STEP_SUMMARY" {
				return path
			}
			return ""
		}
	}
	runWith := func(stream string, getenv func(string) string) (string, int) {
		var out strings.Builder
		code := run(strings.NewReader(stream), &out, getenv)
		return out.String(), code
	}

	t.Run("a file is appended to, not truncated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "summary.md")
		if err := os.WriteFile(path, []byte("earlier step\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := runWith(green, withEnv(path))
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(b), "earlier step\n") || !strings.Contains(string(b), "### Slowest tests") {
			t.Errorf("summary file = %q, want the earlier line first and the table after it", b)
		}
		if strings.Contains(out, "### Slowest tests") {
			t.Errorf("the table went to stdout as well as the summary file:\n%s", out)
		}
	})
	t.Run("an unwritable destination warns and keeps the verdict", func(t *testing.T) {
		dir := t.TempDir()
		for _, c := range []struct {
			stream string
			want   int
		}{{green, 0}, {red, 1}} {
			out, code := runWith(c.stream, withEnv(dir))
			if code != c.want {
				t.Errorf("exit = %d, want the test verdict %d", code, c.want)
			}
			if !strings.Contains(out, "::warning") || !strings.Contains(out, "GITHUB_STEP_SUMMARY") {
				t.Errorf("a directory as GITHUB_STEP_SUMMARY did not warn:\n%s", out)
			}
		}
	})
	t.Run("unset prints the table to stdout", func(t *testing.T) {
		out, _ := runWith(green, withEnv(""))
		if !strings.Contains(out, "### Slowest tests") || !strings.Contains(out, "### Package totals") {
			t.Errorf("no table on stdout:\n%s", out)
		}
	})
}
