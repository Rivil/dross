package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// readmeMilestoneClaim matches a roadmap heading and the status it claims:
//
//	### Milestone v1.3 — finish the tool: … (complete)
var readmeMilestoneClaim = regexp.MustCompile(`(?m)^### Milestone (v[0-9.]+) — .*\(([a-z]+)\)\s*$`)

// TestReadmeRoadmapMatchesRecordedMilestones stops the roadmap drifting away
// from the milestones it describes.
//
// It had drifted badly: v0.6, v0.7 and v0.9 still read "active" and v0.10 read
// "current" long after all four closed, and v1.0 through v1.4 were absent
// entirely — so the README announced v0.10 as the current milestone in a repo
// that had since shipped four more. A roadmap is the first thing a reader
// trusts and the last thing anyone remembers to update, which is exactly the
// shape that needs a guard rather than a habit.
//
// The comparison is against .dross/milestones/*.toml, the same records the CLI
// reads. A hand-maintained list in this test would just be a second thing to
// forget.
func TestReadmeRoadmapMatchesRecordedMilestones(t *testing.T) {
	repo := repoRootFromTest(t)

	// .dross/milestones is tracked, so a fresh checkout has it and this reads
	// the same records everywhere (hermetic_dross_read_test.go).
	dir := filepath.Join(repo, ".dross", "milestones")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read milestones: %v", err)
	}
	recorded := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		version := strings.TrimSuffix(e.Name(), ".toml")
		m, err := milestone.Load(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		recorded[version] = m.Milestone.Status
	}
	if len(recorded) == 0 {
		t.Fatal("no milestones found — the guard would be vacuous")
	}

	body, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	claimed := map[string]string{}
	for _, m := range readmeMilestoneClaim.FindAllStringSubmatch(string(body), -1) {
		claimed[m[1]] = m[2]
	}

	for version, status := range recorded {
		got, ok := claimed[version]
		if !ok {
			t.Errorf("milestone %s (%s) has no section in the README roadmap — a reader cannot see it exists", version, status)
			continue
		}
		if got != status {
			t.Errorf("README says milestone %s is %q; it is recorded %q", version, got, status)
		}
	}
	for version := range claimed {
		if _, ok := recorded[version]; !ok {
			t.Errorf("README describes milestone %s, which has no .dross/milestones/%s.toml", version, version)
		}
	}
}
