package verify

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/mutation"
)

// Scope is the file set a phase's mutation score is allowed to be computed
// from: the files the phase actually touched, plus the line ranges it changed
// inside them.
//
// It exists because mutation tools attribute at a coarser granularity than a
// phase does. Gremlins mutates a whole Go package; a survivor in a file the
// phase never opened is real, but it is not this phase's survivor, and letting
// it into the score means a phase either fails for a neighbour's debt or —
// worse — passes on a neighbour's kills.
//
// The type is pure: no git, no filesystem, no I/O. Deriving the inputs is
// internal/cmd's job; deciding what they mean is this file's.
type Scope struct {
	// Root is the absolute repo root, used to make absolute paths
	// repo-relative. Not persisted — it is machine-specific, and a scope
	// reloaded on another machine must not silently re-anchor.
	Root string `json:"-"`

	// Files is the normalised, deduped, sorted in-scope set.
	Files []string `json:"files"`

	// Hunks maps a normalised file to the NEW-side line ranges the phase
	// changed in it. Advisory only: it decides the in-hunk vs inherited tag,
	// never whether a mutant is in scope (see the scope_granularity lock).
	Hunks map[string][]Range `json:"hunks,omitempty"`

	// Base is the resolved merge-base SHA the diff was taken against — a
	// sha, not a ref, so a stale-base run is diagnosable after the fact
	// rather than merely plausible.
	Base string `json:"base,omitempty"`

	// Source names which inputs contributed: SourceGitOnly, SourceChangesOnly,
	// SourceUnion or SourceNone.
	Source string `json:"source"`

	// Degraded lists the reasons this scope is less than a complete view.
	// Non-empty means a mis-scoped run is possible and the verify output
	// should say so — a degraded scope must never look like a clean pass.
	Degraded []string `json:"degraded,omitempty"`

	// set indexes Files for lookup. Rebuilt lazily so a Scope decoded from
	// tests.json (where the index is not persisted) still answers Contains.
	set map[string]bool
}

// Range is an inclusive line range, both ends counted.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Scope sources, recorded so a run can be read back and diagnosed.
const (
	// SourceGitOnly — git named the files and changes.json recorded none.
	// Not degraded: git is the complete view on its own.
	SourceGitOnly = "git-only"
	// SourceChangesOnly — git contributed nothing, so the scope is only as
	// wide as what was recorded. Always degraded: an unrecorded task's files
	// are invisible here.
	SourceChangesOnly = "changes-only"
	// SourceUnion — both sides contributed.
	SourceUnion = "git+changes"
	// SourceNone — neither side named a file.
	SourceNone = "none"
)

// ScopeInput is the raw, un-normalised material NewScope folds into a Scope.
type ScopeInput struct {
	Root     string             // absolute repo root ("" if unknown)
	Base     string             // resolved merge-base sha
	Git      []string           // files named by the git diff
	Recorded []string           // files recorded in changes.json
	Hunks    map[string][]Range // per-file changed ranges, raw keys
	Degraded []string           // reasons carried in by the caller (e.g. git failed)
}

// NewScope normalises and unions the two independent views of what a phase
// touched.
//
// The two sides are UNIONED, never intersected. Each sees what the other
// cannot: git catches files a task forgot to record, changes.json catches
// files a later commit restored to their original contents (git shows no
// diff, but the phase certainly owns them). Union fails OPEN — wider scope,
// more mutants gated — so a forgotten `dross changes record` can never
// silently shrink a phase to nothing and manufacture a vacuous pass.
func NewScope(in ScopeInput) *Scope {
	root := normalizeRoot(in.Root)
	s := &Scope{
		Root:     root,
		Base:     in.Base,
		Degraded: append([]string(nil), in.Degraded...),
	}

	var rejected []string
	keep := map[string]bool{}
	add := func(paths []string) int {
		n := 0
		for _, p := range paths {
			key, ok := NormalizePath(root, p)
			if !ok {
				rejected = append(rejected, p)
				continue
			}
			keep[key] = true
			n++
		}
		return n
	}
	gitCount := add(in.Git)
	recordedCount := add(in.Recorded)

	s.Files = make([]string, 0, len(keep))
	for f := range keep {
		s.Files = append(s.Files, f)
	}
	sort.Strings(s.Files)
	s.index()

	for raw, ranges := range in.Hunks {
		key, ok := NormalizePath(root, raw)
		if !ok {
			rejected = append(rejected, raw)
			continue
		}
		if len(ranges) == 0 {
			continue
		}
		if s.Hunks == nil {
			s.Hunks = map[string][]Range{}
		}
		s.Hunks[key] = append(s.Hunks[key], ranges...)
	}
	for _, ranges := range s.Hunks {
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	}

	switch {
	case gitCount > 0 && recordedCount > 0:
		s.Source = SourceUnion
	case gitCount > 0:
		s.Source = SourceGitOnly
	case recordedCount > 0:
		s.Source = SourceChangesOnly
		s.Degraded = append(s.Degraded,
			"git contributed no files; scope is changes.json only and misses anything unrecorded")
	default:
		s.Source = SourceNone
		s.Degraded = append(s.Degraded, "no files in scope from either git or changes.json")
	}

	// Rejections are reported, never silently dropped: a path that escapes
	// the repo is either a bug upstream or an attempt to widen the scope
	// outside it, and both need to be visible.
	sort.Strings(rejected)
	for _, p := range dedupe(rejected) {
		s.Degraded = append(s.Degraded, fmt.Sprintf("ignored out-of-repo path %q", p))
	}
	return s
}

// index rebuilds the lookup set from Files.
func (s *Scope) index() {
	s.set = make(map[string]bool, len(s.Files))
	for _, f := range s.Files {
		s.set[f] = true
	}
}

// Contains reports whether file is in scope. It normalises first, so a caller
// may pass whatever shape its tool emitted — "./a.go", an absolute path, or a
// backslash-separated one.
func (s *Scope) Contains(file string) bool {
	if s == nil {
		return false
	}
	if s.set == nil {
		s.index()
	}
	key, ok := NormalizePath(s.Root, file)
	if !ok {
		return false
	}
	return s.set[key]
}

// InHunk reports whether line of file sits inside a range the phase changed.
// False for a file with no recorded hunks — absence of hunk data means "not
// known to be in a hunk", which is the conservative reading.
func (s *Scope) InHunk(file string, line int) bool {
	if s == nil || len(s.Hunks) == 0 {
		return false
	}
	key, ok := NormalizePath(s.Root, file)
	if !ok {
		return false
	}
	for _, r := range s.Hunks[key] {
		if line >= r.Start && line <= r.End {
			return true
		}
	}
	return false
}

// Origin classifies an in-scope mutant: mutation.OriginInHunk when it sits on
// a line the phase changed, mutation.OriginInherited otherwise.
//
// It defaults to inherited, not in-hunk. Inherited is the weaker claim, and
// hunk data is the part of the scope most likely to be missing (a degraded
// git leg drops it entirely) — defaulting the other way would silently
// promote every survivor in a hunk-less run to the strongest evidence tier.
func (s *Scope) Origin(file string, line int) string {
	if s.InHunk(file, line) {
		return mutation.OriginInHunk
	}
	return mutation.OriginInherited
}

// Empty reports whether the scope names no files at all.
func (s *Scope) Empty() bool { return s == nil || len(s.Files) == 0 }

// hunkHeader matches a unified-diff hunk header, capturing the NEW-side start
// line and optional length. The old-side numbers are irrelevant here: scope is
// about the lines that exist after the change, which is where a mutant lands.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ParseHunks reads unified-diff text (as `git diff -U0` emits it) and returns
// the changed new-side line ranges per file, plus any reasons the parse was
// incomplete.
//
// It never returns an error. A diff is a large machine-generated document and
// one unreadable header in it is not a reason to throw away every range that
// parsed cleanly — the ranges only refine a tag, so the failure mode of losing
// them all is strictly worse than the failure mode of losing one.
func ParseHunks(diff string) (map[string][]Range, []string) {
	var (
		hunks     map[string][]Range
		degraded  []string
		current   string
		sawHeader bool
	)
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			current = diffPath(strings.TrimPrefix(line, "+++ "))
			sawHeader = true
		case sawHeader && current == "" && strings.HasPrefix(line, "@@"):
			// A deleted file's `+++ /dev/null` section: no new side, so its
			// hunks belong to no file. Skipping is correct, not a degradation
			// — and they must not attach to whatever file preceded them.
		case strings.HasPrefix(line, "@@"):
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				where := current
				if where == "" {
					where = "(before any file header)"
				}
				degraded = append(degraded,
					fmt.Sprintf("unparsable hunk header in %s: %q", where, line))
				continue
			}
			if current == "" {
				degraded = append(degraded,
					fmt.Sprintf("hunk header with no file: %q", line))
				continue
			}
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			// A pure deletion (+n,0) adds no new-side lines, so it contributes
			// no range — there is nothing there for a mutant to land on.
			if count <= 0 {
				continue
			}
			if hunks == nil {
				hunks = map[string][]Range{}
			}
			hunks[current] = append(hunks[current], Range{Start: start, End: start + count - 1})
		}
	}
	return hunks, degraded
}

// diffPath strips the `b/` prefix and any C-style quoting git applies to a
// path with non-ASCII or special characters. `/dev/null` (a deleted file) maps
// to "" — there is no new side to record ranges against.
func diffPath(p string) string {
	p = strings.TrimSpace(p)
	// Drop a trailing tab-separated timestamp if the diff carries one.
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	if strings.HasPrefix(p, `"`) {
		if unquoted, err := strconv.Unquote(p); err == nil {
			p = unquoted
		}
	}
	if p == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(p, "b/")
}

// NormalizePath rewrites a path into the canonical scope key: slash-separated,
// repo-relative, cleaned, no leading "./". It reports ok=false for a path that
// names something outside the repo, which then never becomes a Contains match.
//
// Backslashes are folded to slashes unconditionally rather than by platform:
// the paths being matched come from mutation tools and from changes.json, and
// a report produced on Windows must still match a scope derived on a Unix box.
// A literal backslash in a Unix filename is not a case worth preserving here.
func NormalizePath(root, p string) (string, bool) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return "", false
	}
	// Canonicalise the root here too: this is exported, and a caller that
	// hands over a trailing-slash root should not silently get every path
	// classified as out-of-repo.
	root = normalizeRoot(root)
	if root != "" {
		if p == root {
			return "", false
		}
		if strings.HasPrefix(p, root+"/") {
			p = strings.TrimPrefix(p, root+"/")
		}
	}
	if path.IsAbs(p) {
		// Absolute and not under the root: a different tree entirely.
		return "", false
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

// normalizeRoot canonicalises the repo root for prefix stripping.
func normalizeRoot(root string) string {
	root = strings.ReplaceAll(strings.TrimSpace(root), `\`, "/")
	if root == "" {
		return ""
	}
	root = path.Clean(root)
	if root == "." || root == "/" {
		return ""
	}
	return root
}

// dedupe removes adjacent duplicates from a sorted slice.
func dedupe(in []string) []string {
	out := in[:0]
	var last string
	for i, s := range in {
		if i > 0 && s == last {
			continue
		}
		out = append(out, s)
		last = s
	}
	return out
}
