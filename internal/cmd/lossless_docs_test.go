package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The lossless-write fix is only as retired as its documentation says it is:
// a repo that banned `dross project set` because it used to drop comments
// keeps the ban until something tells it the ban is obsolete. These tests
// pin that the README says so — in the one place, once — and that
// ARCHITECTURE.md names the writer and the patcher with anchors that resolve.

// readmeSection returns the normalised text of README.md's `### Updating`
// section: from its heading to the next `### ` heading.
func readmeSection(t *testing.T, heading string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	doc := string(body)
	start := strings.Index(doc, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("README.md has no %q section", heading)
	}
	section := doc[start+1:]
	if end := strings.Index(section[len(heading):], "\n### "); end >= 0 {
		section = section[:len(heading)+end]
	}
	s := strings.ToLower(section)
	s = strings.NewReplacer("`", "", "*", "", "_", "", "\n", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// TestReadmeRetiresProjectSetBans: the `### Updating` section carries a
// subsection that names both writers together with "safe to retire" and the
// r-01 example. Deleting or renaming the subsection, or moving the claim
// somewhere else in the README, fails here — docText over the whole file
// would not notice a move, and the `dross project {show,get,set}` table row
// is deliberately NOT a second home for the claim.
func TestReadmeRetiresProjectSetBans(t *testing.T) {
	section := readmeSection(t, "### Updating")
	for _, want := range []string{
		"dross project set",
		"dross state set version",
		"safe to retire",
		"r-01",
		"#### project set and state set version are now surgical",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("README.md's Updating section does not say %q", want)
		}
	}
	// The whole file says it exactly once: one location, one pin.
	if n := strings.Count(docText(t, "README.md"), "safe to retire"); n != 1 {
		t.Errorf("README.md says \"safe to retire\" %d times, want exactly 1 (the Updating subsection)", n)
	}
}

// TestArchitectureNamesLosslessWriter: the Configuration entry names Save as
// the sole writer and the raw-byte patcher, each with a file:line anchor that
// points at a real line of the named file. `dross doctor` reports an anchor
// whose line has moved; this asserts the anchors are PRESENT and RESOLVE.
func TestArchitectureNamesLosslessWriter(t *testing.T) {
	root := repoRootFromTest(t)
	body, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	doc := string(body)
	start := strings.Index(doc, "### Configuration\n")
	if start < 0 {
		t.Fatal("no Configuration entry")
	}
	entry := doc[start:]
	if end := strings.Index(entry[1:], "\n### "); end >= 0 {
		entry = entry[:end+1]
	}

	for _, want := range []string{
		"sole writer",
		"`project.Project.Save`",
		"`internal/project/project.go:",
		"`internal/project/patch.go:",
		"`projectWriterViolations`",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("the Configuration entry does not contain %s — the lossless writer is undocumented", want)
		}
	}

	// This phase's own anchors — the writer and the patcher — resolve to a
	// line that exists and mentions the symbol the bullet names. Older
	// anchors in the entry are left to `dross doctor`, the drift reporter.
	anchor := regexp.MustCompile("`(project\\.Project\\.Save|apply|diff|encodeFresh)`[^\n]*— `(internal/project/[a-z_]+\\.go):(\\d+)`")
	matches := anchor.FindAllStringSubmatch(entry, -1)
	seen := map[string]bool{}
	for _, m := range matches {
		symbol, file, lineStr := m[1], m[2], m[3]
		line, _ := strconv.Atoi(lineStr)
		src, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("anchor names %s, which cannot be read: %v", file, err)
		}
		lines := strings.Split(string(src), "\n")
		if line < 1 || line > len(lines) {
			t.Errorf("anchor %s:%d is past the end of the file (%d lines)", file, line, len(lines))
			continue
		}
		leaf := symbol[strings.LastIndex(symbol, ".")+1:]
		if !strings.Contains(lines[line-1], leaf) {
			t.Errorf("anchor %s:%d does not mention %s: %q", file, line, leaf, strings.TrimSpace(lines[line-1]))
		}
		seen[file] = true
	}
	for _, file := range []string{"internal/project/project.go", "internal/project/patch.go"} {
		if !seen[file] {
			t.Errorf("no resolving anchor into %s in the Configuration entry", file)
		}
	}
}
