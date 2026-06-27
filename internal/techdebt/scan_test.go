package techdebt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// count returns how many findings have the given class.
func count(fs []Finding, class string) int {
	n := 0
	for _, f := range fs {
		if f.Class == class {
			n++
		}
	}
	return n
}

// TestScanMarkersMidLineAllKinds fails if the marker regex misses a marker that
// appears mid-line, for any of the four marker words.
func TestScanMarkersMidLineAllKinds(t *testing.T) {
	for _, kind := range []string{"TODO", "FIXME", "HACK", "XXX"} {
		content := []byte("x := 1 // " + kind + " later")
		fs := scanContent("f.go", content, Thresholds{})
		if count(fs, ClassMarker) != 1 {
			t.Fatalf("%s mid-line: got %d marker findings, want 1 (%+v)", kind, count(fs, ClassMarker), fs)
		}
		if fs[0].Detail != kind {
			t.Errorf("%s: detail = %q, want the marker word", kind, fs[0].Detail)
		}
	}
}

// TestScanMarkerWordBoundary fails if substring matching creeps in: an
// identifier that merely contains a marker word must not be flagged.
func TestScanMarkerWordBoundary(t *testing.T) {
	fs := scanContent("f.go", []byte("TODOList := buildTODOList()"), Thresholds{})
	if count(fs, ClassMarker) != 0 {
		t.Fatalf("identifier TODOList flagged as a marker: %+v", fs)
	}
}

// TestScanSizeThresholds fails if either size heuristic miscounts: an oversized
// file yields exactly one file-level finding, and a single over-length line
// yields exactly one long-line finding.
func TestScanSizeThresholds(t *testing.T) {
	th := Thresholds{MaxFileLines: 3, MaxLineChars: 10}

	big := []byte("a\nb\nc\nd\ne") // 5 lines > 3
	fs := scanContent("big.txt", big, th)
	if count(fs, ClassOversizedFile) != 1 {
		t.Fatalf("oversized file: got %d findings, want 1 (%+v)", count(fs, ClassOversizedFile), fs)
	}

	long := []byte("short\n" + strings.Repeat("x", 25) + "\nshort") // one 25-char line > 10
	fl := scanContent("long.txt", long, th)
	if count(fl, ClassLongLine) != 1 {
		t.Fatalf("long line: got %d findings, want 1 (%+v)", count(fl, ClassLongLine), fl)
	}
}

// TestScanBinaryAndEmpty fails if a NUL-containing (binary) file or a zero-byte
// file produces any finding — both must scan to nothing without error.
func TestScanBinaryAndEmpty(t *testing.T) {
	// A binary file even with a marker-looking byte sequence is skipped.
	bin := []byte("TODO\x00FIXME more bytes")
	if fs := scanContent("bin", bin, DefaultThresholds); len(fs) != 0 {
		t.Fatalf("binary file yielded findings: %+v", fs)
	}
	if fs := scanContent("empty", []byte(""), DefaultThresholds); len(fs) != 0 {
		t.Fatalf("empty file yielded findings: %+v", fs)
	}
}

// TestScanNoTrailingNewlineCountsLastLine fails if the final line is dropped when
// the file lacks a trailing newline: the marker on the last line must be found,
// and the line count must not be off by one.
func TestScanNoTrailingNewlineCountsLastLine(t *testing.T) {
	// Last line has a marker and no trailing newline.
	fs := scanContent("f.go", []byte("ok\n// TODO last"), Thresholds{})
	if count(fs, ClassMarker) != 1 || fs[0].Line != 2 {
		t.Fatalf("last-line marker missed without trailing newline: %+v", fs)
	}
	// A trailing newline must not add a phantom empty line: 3 real lines stays 3.
	withNL := scanContent("g.txt", []byte("a\nb\nc\n"), Thresholds{MaxFileLines: 3})
	if count(withNL, ClassOversizedFile) != 0 {
		t.Fatalf("trailing newline invented a 4th line (oversized at threshold 3): %+v", withNL)
	}
	withoutNL := scanContent("h.txt", []byte("a\nb\nc\nd"), Thresholds{MaxFileLines: 3})
	if count(withoutNL, ClassOversizedFile) != 1 {
		t.Fatalf("4 lines without trailing newline not counted as oversized: %+v", withoutNL)
	}
}

// TestScanCleanTreeNoFindings fails if the scanner emits false positives on
// marker-free, reasonably-sized content.
func TestScanCleanTreeNoFindings(t *testing.T) {
	clean := []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	if fs := scanContent("main.go", clean, DefaultThresholds); len(fs) != 0 {
		t.Fatalf("clean file yielded findings: %+v", fs)
	}
}

// TestScanReadsPathSetSkipsUnreadable fails if Scan's read path doesn't skip a
// binary file or an unreadable path while still scanning the good ones.
func TestScanReadsPathSetSkipsUnreadable(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.go")
	if err := os.WriteFile(good, []byte("// FIXME real one"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(bin, []byte("TODO\x00stuff"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.go")

	fs := Scan([]string{good, bin, missing}, DefaultThresholds)
	if count(fs, ClassMarker) != 1 || fs[0].File != good {
		t.Fatalf("Scan over [good, binary, missing] = %+v; want exactly the good marker", fs)
	}
}
