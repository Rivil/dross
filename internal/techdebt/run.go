package techdebt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runIDTimeFormat is a fixed-width, lexically-sortable timestamp layout, so run
// ids sort chronologically when listed. Mirrors security/quality.
const runIDTimeFormat = "20060102T150405"

// ReportName is the fixed report filename written inside each run dir.
const ReportName = "report.md"

// RunID returns the run identifier "<timestamp>-<short-sha>". Pass UTC for ids
// stable across machines; sha is the short commit sha, or "nogit" when the repo
// state is unavailable.
func RunID(now time.Time, sha string) string {
	if sha == "" {
		sha = "nogit"
	}
	return fmt.Sprintf("%s-%s", now.Format(runIDTimeFormat), sha)
}

// ShortSHA returns the short HEAD sha for the repo at repoDir, or "nogit" if it
// can't be read (not a git repo, no commits). Best-effort — never errors, so a
// missing repo degrades to a stable "nogit" rather than failing a run.
func ShortSHA(repoDir string) string {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "nogit"
	}
	return normalizeSHA(string(out))
}

// normalizeSHA trims git's output and falls back to "nogit" when it is empty.
func normalizeSHA(out string) string {
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "nogit"
	}
	return sha
}

// NewRun creates a fresh run directory under .dross/techdebt/ and returns its
// absolute path. The directory name is RunID(now, sha); if a directory with that
// id already exists (e.g. a second run in the same second on the same commit), a
// numeric suffix ("-2", "-3", …) is appended so an existing run is never
// clobbered. Nothing is written outside the returned run directory.
func NewRun(root string, now time.Time, sha string) (string, error) {
	base := RunID(now, sha)
	dir := filepath.Join(Dir(root), base)
	for n := 2; ; n++ {
		_, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				break
			}
			return "", err
		}
		dir = filepath.Join(Dir(root), fmt.Sprintf("%s-%d", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// WriteReport renders fs into <runDir>/report.md. The report is a human-readable
// summary: a totals line by class, then one line per finding.
func WriteReport(runDir string, fs []Finding) error {
	path := filepath.Join(runDir, ReportName)
	return os.WriteFile(path, []byte(renderReport(fs)), 0o644)
}

// renderReport is the pure report body, separated from WriteReport so it is
// unit-testable without disk.
func renderReport(fs []Finding) string {
	var b strings.Builder
	b.WriteString("# Tech-debt scan\n\n")
	if len(fs) == 0 {
		b.WriteString("No tech-debt findings.\n")
		return b.String()
	}
	var markers, oversized, longLines int
	for _, f := range fs {
		switch f.Class {
		case ClassMarker:
			markers++
		case ClassOversizedFile:
			oversized++
		case ClassLongLine:
			longLines++
		}
	}
	fmt.Fprintf(&b, "%d findings: %d markers, %d oversized files, %d long lines.\n\n",
		len(fs), markers, oversized, longLines)
	for _, f := range fs {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(&b, "- [%s] %s — %s\n", f.Class, loc, f.Detail)
	}
	return b.String()
}
