package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// series builds a history with one entry per value, pkg "p" holding it, shas
// s0, s1, … in order.
func series(ys ...float64) history {
	var h history
	for i, y := range ys {
		h.Entries = append(h.Entries, historyEntry{SHA: fmt.Sprintf("s%d", i), Packages: map[string]float64{"p": y}})
	}
	return h
}

// TestLoadHistoryDegraded (c-5): a missing file, a zero-byte file, invalid
// JSON and a history with no entries each load as an empty history with
// exactly one note line, and a good file loads with no note.
func TestLoadHistoryDegraded(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"missing":    filepath.Join(dir, "absent.json"),
		"zero-byte":  write("empty.json", ""),
		"invalid":    write("bad.json", "{not json"),
		"wrong type": write("array.json", "[1,2]"),
		"no entries": write("none.json", `{"entries":[]}`),
	}
	for name, path := range cases {
		h, note := loadHistory(path)
		if len(h.Entries) != 0 {
			t.Errorf("%s: %d entries, want an empty history", name, len(h.Entries))
		}
		if note == "" || strings.Contains(note, "\n") {
			t.Errorf("%s: note = %q, want exactly one non-empty line", name, note)
		}
	}

	good := write("good.json", `{"entries":[{"sha":"a","packages":{"p":1.5}}]}`)
	h, note := loadHistory(good)
	if note != "" || len(h.Entries) != 1 || h.Entries[0].Packages["p"] != 1.5 {
		t.Errorf("good file: %+v, note %q — want one entry and no note", h, note)
	}
}

// TestUpsertReplacesSameSHA (c-6): a second upsert of a sha replaces its
// entry in place; a new sha appends.
func TestUpsertReplacesSameSHA(t *testing.T) {
	var h history
	h.upsert("a", map[string]float64{"p": 1})
	h.upsert("b", map[string]float64{"p": 2})
	h.upsert("a", map[string]float64{"p": 3})
	want := []historyEntry{
		{SHA: "a", Packages: map[string]float64{"p": 3}},
		{SHA: "b", Packages: map[string]float64{"p": 2}},
	}
	if !reflect.DeepEqual(h.Entries, want) {
		t.Errorf("entries = %+v, want %+v", h.Entries, want)
	}
}

// TestHistoryWindowEvicts: a 21st distinct sha drops the oldest entry.
func TestHistoryWindowEvicts(t *testing.T) {
	var h history
	for i := 0; i <= historyWindow; i++ {
		h.upsert(fmt.Sprintf("s%d", i), map[string]float64{"p": float64(i)})
	}
	if len(h.Entries) != historyWindow {
		t.Fatalf("len = %d, want %d", len(h.Entries), historyWindow)
	}
	if h.Entries[0].SHA != "s1" || h.Entries[historyWindow-1].SHA != fmt.Sprintf("s%d", historyWindow) {
		t.Errorf("window = %s..%s, want s1..s%d", h.Entries[0].SHA, h.Entries[historyWindow-1].SHA, historyWindow)
	}
	if historyWindow != 20 {
		t.Errorf("historyWindow = %d, want 20 (decision trend_rule)", historyWindow)
	}
}

// TestHistoryRoundTrip: save then load gives back the same entries in order,
// creating the directory a cache miss leaves absent.
func TestHistoryRoundTrip(t *testing.T) {
	h := history{Entries: []historyEntry{
		{SHA: "b", Packages: map[string]float64{"x": 2.25, "y": 9}},
		{SHA: "a", Packages: map[string]float64{"x": 1.5}},
	}}
	path := filepath.Join(t.TempDir(), "nested", "history.json")
	if err := saveHistory(path, h); err != nil {
		t.Fatal(err)
	}
	got, note := loadHistory(path)
	if note != "" || !reflect.DeepEqual(got, h) {
		t.Errorf("round trip = %+v (note %q), want %+v", got, note, h)
	}
}

// TestBaselineMedian (decision baseline): the median of the window, the
// excluded sha never contributing.
func TestBaselineMedian(t *testing.T) {
	cases := []struct {
		name    string
		h       history
		exclude string
		want    float64
	}{
		{"odd", series(10, 30, 20), "", 20},
		{"even", series(40, 10, 30, 20), "", 25},
		{"outlier", series(10, 11, 1000, 12, 13), "", 12},
		{"excluded", series(10, 20, 30), "s2", 15},
	}
	for _, c := range cases {
		got, ok := c.h.baseline("p", c.exclude)
		if !ok || got != c.want {
			t.Errorf("%s: baseline = %v, %v; want %v, true", c.name, got, ok, c.want)
		}
	}

	if _, ok := series(10, 20).baseline("absent", ""); ok {
		t.Error("a package in no entry: ok = true, want false")
	}
	if _, ok := series(10).baseline("p", "s0"); ok {
		t.Error("a package only in the excluded entry: ok = true, want false")
	}
}

// TestTrend (decision trend_rule, c-3): a least-squares line over the last
// 10 entries, >= 5 points, rising, reaching 540s within 10 more runs.
func TestTrend(t *testing.T) {
	// 80, 130, …, 530 then ten runs flat at 530: the last ten are flat, while
	// a fit over all twenty — or over the first ten — would warn.
	var riseThenFlat []float64
	for i := 0; i < 10; i++ {
		riseThenFlat = append(riseThenFlat, 80+50*float64(i))
	}
	for i := 0; i < 10; i++ {
		riseThenFlat = append(riseThenFlat, 530)
	}

	cases := []struct {
		name string
		h    history
		want bool
	}{
		{"4 rising points", series(500, 520, 540, 560), false},
		{"5 flat", series(100, 100, 100, 100, 100), false},
		{"5 falling", series(500, 400, 300, 200, 100), false},
		{"flat above the line", series(545, 545, 545, 545, 545), false},
		{"falling above the line", series(600, 586.25, 572.5, 558.75, 545), false},
		// fit at x=14 is exactly 540.0 (x=13 → 530, x=15 → 550)
		{"reaches 540.0", series(400, 410, 420, 430, 440), true},
		// fit at x=14 is 539.5 (x=15 → 549.5)
		{"reaches 539.5", series(399.5, 409.5, 419.5, 429.5, 439.5), false},
		{"rise confined to the first ten", series(riseThenFlat...), false},
	}
	for _, c := range cases {
		if got := c.h.trend("p", ""); got != c.want {
			t.Errorf("%s: trend = %v, want %v", c.name, got, c.want)
		}
	}
	if trendWindow != 10 || trendMinPoints != 5 || trendHorizon != 10 || capWarnSeconds != 540 {
		t.Errorf("trend constants = %d/%d/%d/%d, want 10/5/10/540 (decision trend_rule)",
			trendWindow, trendMinPoints, trendHorizon, capWarnSeconds)
	}
}

// TestTrendExcludesSHA: the excluded entry is dropped before the last-10
// window is taken, and never feeds the fit.
func TestTrendExcludesSHA(t *testing.T) {
	// p is in the first five entries only, rising to warn; five more entries
	// hold another package; the newest entry is the excluded sha. Dropping it
	// first leaves all five points in the window; windowing first would leave
	// four.
	h := series(400, 410, 420, 430, 440)
	for i := 5; i < 10; i++ {
		h.Entries = append(h.Entries, historyEntry{SHA: fmt.Sprintf("s%d", i), Packages: map[string]float64{"q": 1}})
	}
	h.Entries = append(h.Entries, historyEntry{SHA: "self", Packages: map[string]float64{"q": 1}})
	if !h.trend("p", "self") {
		t.Error("excluded newest entry: trend = false, want the five earlier points to warn")
	}

	// A flat history whose newest entry — the excluded sha — spikes to 1000s:
	// the spike would warn if it counted.
	h = series(300, 300, 300, 300, 300, 300, 300, 300, 300, 1000)
	if h.trend("p", "s9") {
		t.Error("excluded 1000s self-entry: trend = true, want it ignored")
	}
	if !h.trend("p", "") {
		t.Error("control: the same history with nothing excluded should warn")
	}
}

// TestRecordedTotals: only packages that ran — pass or fail with elapsed > 0 —
// are recorded.
func TestRecordedTotals(t *testing.T) {
	got := packageTotals([]result{
		{Package: "pass", Action: "pass", Elapsed: 1.5},
		{Package: "fail", Action: "fail", Elapsed: 2},
		{Package: "skip", Action: "skip", Elapsed: 0.01},
		{Package: "zero", Action: "pass", Elapsed: 0},
		{Package: "pass", Test: "TestX", Action: "pass", Elapsed: 9},
	})
	want := map[string]float64{"pass": 1.5, "fail": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("totals = %v, want %v", got, want)
	}
}
