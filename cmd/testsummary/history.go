package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// capSeconds is the per-package hard wall: go test's own 10m default timeout
// (locked decision cap_source). CI passes no -timeout, so this is the cap a
// package actually dies at.
const capSeconds = 600

// capWarnSeconds is the line a package draws a cap warning at, and the line a
// trend warns about reaching: 90% of the cap.
const capWarnSeconds = capSeconds * 9 / 10

// The history window and the trend rule (locked decisions trend_rule and
// baseline): the file keeps the newest historyWindow main runs; the trend fits
// a line over the last trendWindow of them, needs trendMinPoints where the
// package is present, and warns when the line reaches capWarnSeconds within
// trendHorizon more runs.
const (
	historyWindow  = 20
	trendWindow    = 10
	trendMinPoints = 5
	trendHorizon   = 10
)

// historyEntry is one main run's package totals, keyed by its commit.
type historyEntry struct {
	SHA      string             `json:"sha"`
	Packages map[string]float64 `json:"packages"`
}

// history is the persisted timing history, oldest entry first. Only main runs
// save it, so every entry is a main run.
type history struct {
	Entries []historyEntry `json:"entries"`
}

// loadHistory reads the history at path. A missing, empty or unparseable file
// is not an error — a first run or an evicted cache is normal — but it yields
// an empty history and one note line saying so, so the run can tell the
// reader why there is no baseline or trend. The note is "" when the history
// loaded with entries in it.
func loadHistory(path string) (history, string) {
	degraded := func(why string) (history, string) {
		return history{}, fmt.Sprintf("testsummary: no timing history (%s at %s) — no baseline or trend this run", why, path)
	}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return degraded("no file")
	case err != nil:
		return degraded("unreadable: " + err.Error())
	case len(b) == 0:
		return degraded("empty file")
	}
	var h history
	if err := json.Unmarshal(b, &h); err != nil {
		return degraded("unparseable: " + err.Error())
	}
	if len(h.Entries) == 0 {
		return degraded("no entries")
	}
	return h, ""
}

// saveHistory writes h to path, creating its directory: on a cache miss the
// directory the restore would have filled does not exist yet.
func saveHistory(path string, h history) error {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the timing history: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating the timing history directory: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing the timing history: %w", err)
	}
	return nil
}

// upsert records totals under sha. A sha already in the history is replaced
// in place — a re-run of the same commit never adds a second entry — and a
// new one is appended, dropping the oldest entries past historyWindow.
func (h *history) upsert(sha string, totals map[string]float64) {
	for i := range h.Entries {
		if h.Entries[i].SHA == sha {
			h.Entries[i].Packages = totals
			return
		}
	}
	h.Entries = append(h.Entries, historyEntry{SHA: sha, Packages: totals})
	if n := len(h.Entries); n > historyWindow {
		h.Entries = h.Entries[n-historyWindow:]
	}
}

// points is pkg's seconds across entries, oldest first, skipping the entries
// it is absent from.
func points(entries []historyEntry, pkg string) []float64 {
	var ys []float64
	for _, e := range entries {
		if s, ok := e.Packages[pkg]; ok {
			ys = append(ys, s)
		}
	}
	return ys
}

// without is the history's entries minus the one for sha, so a re-run of a
// commit never compares against its own earlier attempt.
func (h history) without(sha string) []historyEntry {
	var out []historyEntry
	for _, e := range h.Entries {
		if e.SHA != sha {
			out = append(out, e)
		}
	}
	return out
}

// baseline is the median of pkg's seconds across the window, excluding the
// entry for excludeSHA (locked decision baseline: one noisy runner must not
// swing the comparison). ok is false when no entry holds pkg.
func (h history) baseline(pkg, excludeSHA string) (float64, bool) {
	ys := points(h.without(excludeSHA), pkg)
	if len(ys) == 0 {
		return 0, false
	}
	sort.Float64s(ys)
	mid := len(ys) / 2
	if len(ys)%2 == 1 {
		return ys[mid], true
	}
	return (ys[mid-1] + ys[mid]) / 2, true
}

// trend reports whether pkg is heading for the cap (locked decision
// trend_rule): a least-squares line over the last trendWindow entries
// (excludeSHA's dropped first) where pkg is present, at least trendMinPoints
// of them, rising, and reaching capWarnSeconds within trendHorizon more runs.
func (h history) trend(pkg, excludeSHA string) bool {
	entries := h.without(excludeSHA)
	if n := len(entries); n > trendWindow {
		entries = entries[n-trendWindow:]
	}
	ys := points(entries, pkg)
	k := len(ys)
	if k < trendMinPoints {
		return false
	}
	// Centred least squares: x = 0..k-1, slope = Σdx·dy / Σdx².
	xMean := float64(k-1) / 2
	var yMean float64
	for _, y := range ys {
		yMean += y
	}
	yMean /= float64(k)
	var sxy, sxx float64
	for i, y := range ys {
		dx := float64(i) - xMean
		sxy += dx * (y - yMean)
		sxx += dx * dx
	}
	slope := sxy / sxx
	projected := yMean + slope*(float64(k-1+trendHorizon)-xMean)
	return slope > 0 && projected >= capWarnSeconds
}

// packageTotals is what a run records into the history: each package's own
// elapsed time, for packages that ran — ended pass or fail with time on the
// clock. A skip (no test files) and a zero elapsed measure nothing.
func packageTotals(results []result) map[string]float64 {
	totals := map[string]float64{}
	for _, r := range results {
		if r.Test == "" && (r.Action == "pass" || r.Action == "fail") && r.Elapsed > 0 {
			totals[r.Package] = r.Elapsed
		}
	}
	return totals
}
