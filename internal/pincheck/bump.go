package pincheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BumpStatus is what the bump planner did with one stale site.
type BumpStatus string

const (
	// Bumped: the pin was rewritten in place to its target.
	Bumped BumpStatus = "bumped"
	// Skipped: the pin is stale but moves by another route.
	Skipped BumpStatus = "skipped"
	// Refused: the pin sits somewhere the bump job must never write.
	Refused BumpStatus = "refused"
)

// BumpOutcome is one stale site and what became of it.
type BumpOutcome struct {
	Site
	Target string
	Status BumpStatus
	Reason string // why a site was skipped or refused
}

func (o BumpOutcome) String() string {
	s := fmt.Sprintf("%-8s %s:%d %s %s → %s", o.Status, o.File, o.Line, o.Name, o.Version, o.Target)
	if o.Reason != "" {
		s += " — " + o.Reason
	}
	return s
}

// workflowDir is where no bump may write. The bump job pushes with a
// GITHUB_TOKEN, and GitHub rejects a GITHUB_TOKEN push that changes a workflow
// file (locked decision bump_reach) — so a pin there is reported, not moved.
const workflowDir = ".github/workflows/"

// Bump rewrites every stale result's pin in place, under root, to its target:
// the newest release on the pin's line past the cooldown, as Classify chose it.
// Only the version token on the site's own line changes; every other byte of
// the file is kept.
//
// Two kinds of stale pin are reported and left alone. An npm pin is skipped:
// it moves with Dependabot's lockfile bump, and regenerating a lockfile inside
// a job holding write permissions is the install vector the CI hardening rules
// exist to keep out (locked decision bot_bump_scope — strykerPin is the case).
// A pin under .github/workflows/ is refused (bump_reach).
//
// Results that are not stale produce no outcome, so a bump over a tree that
// is already current changes nothing and reports nothing.
func Bump(root string, results []Result) ([]BumpOutcome, error) {
	var out []BumpOutcome
	for _, r := range results {
		if r.Verdict != Stale {
			continue
		}
		o := BumpOutcome{Site: r.Site, Target: r.Target}
		switch {
		case r.Kind == KindNPM:
			o.Status, o.Reason = Skipped, "npm pins move with Dependabot's lockfile bump, never the pin-currency bot"
		case strings.HasPrefix(r.File, workflowDir):
			o.Status, o.Reason = Refused, "the bump job's GITHUB_TOKEN cannot push a workflow file — move the pin out of .github/workflows/"
		default:
			if err := rewriteVersion(root, r.Site, r.Target); err != nil {
				return out, err
			}
			o.Status = Bumped
		}
		out = append(out, o)
	}
	return out, nil
}

// rewriteVersion replaces the one occurrence of s.Version on line s.Line of
// s.File with target. It refuses a line where the version is absent or
// ambiguous — the tree moved since the scan, and guessing would corrupt it.
func rewriteVersion(root string, s Site, target string) error {
	path := filepath.Join(root, filepath.FromSlash(s.File))
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("pincheck: bump %s: %w", s.File, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("pincheck: bump %s: %w", s.File, err)
	}
	lines := strings.SplitAfter(string(b), "\n")
	if s.Line < 1 || s.Line > len(lines) {
		return fmt.Errorf("pincheck: bump %s:%d: no such line", s.File, s.Line)
	}
	line := lines[s.Line-1]
	at := versionOccurrences(line, s.Version)
	if len(at) != 1 {
		return fmt.Errorf("pincheck: bump %s:%d: want exactly one %q on the line, found %d — the tree changed since it was scanned", s.File, s.Line, s.Version, len(at))
	}
	lines[s.Line-1] = line[:at[0]] + target + line[at[0]+len(s.Version):]
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), info.Mode().Perm()); err != nil {
		return fmt.Errorf("pincheck: bump %s: %w", s.File, err)
	}
	return nil
}

// versionOccurrences returns the offset of every occurrence of v in line that
// is not part of a longer version — 1.8.0 inside 11.8.0 or 1.8.01 does not
// count.
func versionOccurrences(line, v string) []int {
	var at []int
	for i := 0; ; {
		j := strings.Index(line[i:], v)
		if j < 0 {
			return at
		}
		start, end := i+j, i+j+len(v)
		before := start == 0 || !isVersionChar(line[start-1])
		after := end == len(line) || !isVersionChar(line[end])
		if before && after {
			at = append(at, start)
		}
		i = start + 1
	}
}

func isVersionChar(c byte) bool {
	return c >= '0' && c <= '9' || c == '.'
}
