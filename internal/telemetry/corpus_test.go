package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusPath is the checked-in fixture of real err_detail shapes, curated from
// the live telemetry log with home directories already collapsed to "~" (that
// redaction happens in Detail, before anything is written).
const corpusPath = "testdata/error_corpus.jsonl"

// minCorpusLines keeps the ratio gate below from passing vacuously. A corpus
// that shrank to a handful of hand-picked lines could hit any ratio.
const minCorpusLines = 20

// maxOtherShare is c-7's target: under 15% of the distinct detail-carrying
// shapes seen in real use may remain unclassified.
const maxOtherShare = 0.15

type corpusRow struct {
	Detail string `json:"detail"`
	Want   string `json:"want"`
	line   int
}

// loadCorpus reads the fixture strictly. A malformed line is a failure, not a
// skip: silently dropping lines would shrink the denominator and let the ratio
// gate pass by attrition.
func loadCorpus(t *testing.T) []corpusRow {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(corpusPath))
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	var rows []corpusRow
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var r corpusRow
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("%s:%d: malformed corpus line: %v", corpusPath, n, err)
		}
		r.line = n
		if r.Detail == "" {
			t.Errorf("%s:%d: empty detail", corpusPath, n)
		}
		if r.Want == "" {
			t.Errorf("%s:%d: empty want", corpusPath, n)
		}
		if prev, dup := seen[r.Detail]; dup {
			t.Errorf("%s:%d: duplicates the shape on line %d — a shrinking set of distinct shapes weakens the ratio gate", corpusPath, n, prev)
		}
		seen[r.Detail] = n
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if len(rows) < minCorpusLines {
		t.Fatalf("corpus has %d lines, want at least %d — the ratio gate is meaningless on a tiny corpus", len(rows), minCorpusLines)
	}
	return rows
}

// TestCorpusShapesClassifyAsExpected pins every real shape to its bucket. This
// is the regression half: re-bucketing a shape fails here even when the overall
// unclassified ratio is unchanged.
func TestCorpusShapesClassifyAsExpected(t *testing.T) {
	for _, r := range loadCorpus(t) {
		if got := ClassifyMessage(r.Detail); got != r.Want {
			t.Errorf("%s:%d: classifies as %q, want %q\n  detail: %s",
				corpusPath, r.line, got, r.Want, oneLine(r.Detail))
		}
	}
}

// TestCorpusOtherShareUnderCeiling is c-7: the ceiling on how much of the real
// error surface may remain opaque. Reverting one of the heavily-represented
// cases (arg_count, unknown_flag, unknown_subcommand) pushes the share back
// over it; a revert in a bucket with only two real shapes stays under the
// ratio, which is what TestCorpusShapesClassifyAsExpected above is for — it
// names the exact lines and buckets regardless of the ratio.
func TestCorpusOtherShareUnderCeiling(t *testing.T) {
	rows := loadCorpus(t)

	var unclassified []string
	wantOther := 0
	for _, r := range rows {
		if r.Want == "other" {
			wantOther++
		}
		if ClassifyMessage(r.Detail) == "other" {
			unclassified = append(unclassified, oneLine(r.Detail))
		}
	}
	// Without a line that genuinely fails to classify, a passing ratio proves
	// nothing — the gate has to be falsifiable.
	if wantOther == 0 {
		t.Error("corpus has no want:\"other\" line — keep the sentinel, or the ratio gate can never fail")
	}

	share := float64(len(unclassified)) / float64(len(rows))
	if share >= maxOtherShare {
		t.Errorf("%.1f%% of corpus shapes classify as \"other\" (%d/%d), want under %.0f%%:\n  %s",
			share*100, len(unclassified), len(rows), maxOtherShare*100,
			strings.Join(unclassified, "\n  "))
	}
}

// oneLine flattens a shape for a readable failure message — several corpus
// details are cobra's multi-line "Available subcommands" help.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
