// Package techdebt is dross's dependency-free, language-agnostic tech-debt
// scanner. It surfaces self-flagged debt markers (TODO/FIXME/HACK/XXX) and
// language-agnostic size heuristics (oversized files, over-long lines) without
// any external tool — distinct from the deep analyzer audit /dross-quality runs.
package techdebt

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Finding classes.
const (
	ClassMarker        = "marker"         // a TODO/FIXME/HACK/XXX comment marker
	ClassOversizedFile = "oversized-file" // a file longer than the line threshold
	ClassLongLine      = "long-line"      // a single line longer than the char threshold
)

// Finding is one tech-debt item. Line is 1-based; for an oversized-file finding
// (which is about the whole file, not a line) Line is 0. Detail is a short
// human-readable qualifier: the marker word for markers, or a "<n> lines" /
// "<n> chars" measure for the size heuristics.
type Finding struct {
	File   string
	Line   int
	Class  string
	Detail string
}

// Thresholds configure the language-agnostic size heuristics. A non-positive
// bound disables that heuristic.
type Thresholds struct {
	MaxFileLines int // a file with more lines than this yields one oversized-file finding
	MaxLineChars int // a line longer than this yields one long-line finding
}

// DefaultThresholds are the size bounds used when none are supplied. They are
// generous on purpose — the scan is a nudge, not a style gate.
var DefaultThresholds = Thresholds{MaxFileLines: 600, MaxLineChars: 400}

// markerRe matches a debt marker as a whole word. The \b boundaries make it
// catch markers mid-line ("x := 1 // FIXME later") while rejecting identifiers
// that merely contain one ("TODOList").
var markerRe = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX)\b`)

// Scan reads each path and returns its tech-debt findings. Files that can't be
// read are skipped silently (the path set may include freshly-deleted entries);
// binary files (those containing a NUL byte) and empty files yield nothing. I/O
// is limited to reading the supplied paths — no enumeration, no external tools.
func Scan(paths []string, th Thresholds) []Finding {
	var out []Finding
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, scanContent(p, content, th)...)
	}
	return out
}

// scanContent is the pure core: it scans one file's bytes with no disk access,
// so edge cases (binary, empty, no trailing newline) are unit-testable directly.
func scanContent(path string, content []byte, th Thresholds) []Finding {
	if len(content) == 0 || bytes.IndexByte(content, 0) >= 0 {
		return nil // empty or binary — nothing to flag
	}
	lines := splitLines(content)
	var out []Finding
	for i, line := range lines {
		if m := markerRe.FindString(line); m != "" {
			out = append(out, Finding{File: path, Line: i + 1, Class: ClassMarker, Detail: m})
		}
		if th.MaxLineChars > 0 && len(line) > th.MaxLineChars {
			out = append(out, Finding{File: path, Line: i + 1, Class: ClassLongLine, Detail: fmt.Sprintf("%d chars", len(line))})
		}
	}
	if th.MaxFileLines > 0 && len(lines) > th.MaxFileLines {
		out = append(out, Finding{File: path, Line: 0, Class: ClassOversizedFile, Detail: fmt.Sprintf("%d lines", len(lines))})
	}
	return out
}

// splitLines splits content into logical lines, counting a final line that lacks
// a trailing newline and NOT inventing a phantom empty line when one is present.
// CRLF is normalized to LF first so Windows files count the same.
func splitLines(content []byte) []string {
	s := strings.ReplaceAll(string(content), "\r\n", "\n")
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
