package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// readmeMilestoneClaim matches a roadmap heading and the status it claims:
//
//	### Milestone v1.3 — finish the tool: … (complete)
var readmeMilestoneClaim = regexp.MustCompile(`(?m)^### Milestone (v[0-9.]+) — .*\(([a-z]+)\)\s*$`)

var (
	// readmeNextH2 ends the roadmap section.
	readmeNextH2 = regexp.MustCompile(`(?m)^## `)
	// readmeNextHeading ends one milestone's section. Level 4 and deeper sit
	// inside a section rather than closing it.
	readmeNextHeading = regexp.MustCompile(`(?m)^#{1,3} `)
	readmeOpenBox     = regexp.MustCompile(`(?m)^[ \t]*- \[ \]`)
	readmeAnyBox      = regexp.MustCompile(`(?m)^[ \t]*- \[[ xX]\]`)
)

// recordedMilestone is what the roadmap guard holds a README section to.
type recordedMilestone struct {
	status   string
	criteria int // len(success_criteria)
}

// readmeRoadmapSection returns the body of "## Roadmap", or "" when the README
// has none. Everything the guard checks is read from here, so a missing section
// reports every milestone as absent rather than passing on nothing.
func readmeRoadmapSection(readme string) string {
	_, after, found := strings.Cut(readme, "\n## Roadmap\n")
	if !found {
		return ""
	}
	if next := readmeNextH2.FindStringIndex(after); next != nil {
		return after[:next[0]]
	}
	return after
}

// readmeRoadmapProblems lists every way the roadmap disagrees with the recorded
// milestones, sorted so a failure reads the same on every run.
func readmeRoadmapProblems(readme string, recorded map[string]recordedMilestone) []string {
	roadmap := readmeRoadmapSection(readme)
	var problems []string
	if strings.Contains(strings.ToLower(roadmap), "azure devops") {
		problems = append(problems, "README roadmap mentions Azure DevOps, which v1.9 dropped as a non-goal")
	}

	claimed := map[string]bool{}
	for _, m := range readmeMilestoneClaim.FindAllStringSubmatchIndex(roadmap, -1) {
		version, status := roadmap[m[2]:m[3]], roadmap[m[4]:m[5]]
		claimed[version] = true
		section := roadmap[m[1]:]
		if next := readmeNextHeading.FindStringIndex(section); next != nil {
			section = section[:next[0]]
		}

		rec, ok := recorded[version]
		if !ok {
			problems = append(problems, fmt.Sprintf("README describes milestone %s, which has no .dross/milestones/%s.toml", version, version))
			continue
		}
		if status != rec.status {
			problems = append(problems, fmt.Sprintf("README says milestone %s is %q; it is recorded %q", version, status, rec.status))
		}
		switch status {
		case "complete":
			if open := len(readmeOpenBox.FindAllString(section, -1)); open > 0 {
				problems = append(problems, fmt.Sprintf("README keeps %d open box(es) under complete milestone %s", open, version))
			}
		case "active":
			if boxes := len(readmeAnyBox.FindAllString(section, -1)); boxes != rec.criteria {
				problems = append(problems, fmt.Sprintf("README shows %d boxes under active milestone %s; it records %d success criteria", boxes, version, rec.criteria))
			}
		}
	}

	for version, rec := range recorded {
		if !claimed[version] {
			problems = append(problems, fmt.Sprintf("milestone %s (%s) has no section in the README roadmap — a reader cannot see it exists", version, rec.status))
		}
	}
	sort.Strings(problems)
	return problems
}

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
// A heading alone can be right while the list under it lies: v1.8 closed with
// four open boxes, and v1.9 went active on a "not yet written" placeholder. So
// the guard also holds a complete section to no open box, and an active one to
// one box per recorded success criterion.
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
	recorded := map[string]recordedMilestone{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		version := strings.TrimSuffix(e.Name(), ".toml")
		m, err := milestone.Load(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("load %s: %v", e.Name(), err)
		}
		recorded[version] = recordedMilestone{status: m.Milestone.Status, criteria: len(m.Scope.SuccessCriteria)}
	}
	if len(recorded) == 0 {
		t.Fatal("no milestones found — the guard would be vacuous")
	}

	body, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	for _, problem := range readmeRoadmapProblems(string(body), recorded) {
		t.Error(problem)
	}
}

// roadmapGuardFixture agrees with roadmapGuardRecords. The Azure line and the
// box under Telemetry sit outside the roadmap, so the clean fixture only reads
// clean if the guard stops at the section's edge.
const roadmapGuardFixture = `# Dross

Azure DevOps outside the roadmap is not this guard's business.

## Roadmap

- [ ] an unversioned todo above the milestones

### Milestone v1.8 — context economy (complete)

- [x] first
- [x] second

### Milestone v1.9 — workflow depth (active)

- [ ] first
- [ ] second
- [ ] third
- [ ] fourth
- [ ] fifth

## Telemetry

- [ ] a box past the roadmap belongs to no milestone
`

// TestReadmeRoadmapGuardCatchesEachDrift proves each check bites on a fixture
// rather than on the live README, whose milestones will move on: the real
// test above can only ever show that today's text passes.
func TestReadmeRoadmapGuardCatchesEachDrift(t *testing.T) {
	records := map[string]recordedMilestone{
		"v1.8": {status: "complete", criteria: 2},
		"v1.9": {status: "active", criteria: 5},
	}
	if problems := readmeRoadmapProblems(roadmapGuardFixture, records); len(problems) != 0 {
		t.Fatalf("clean fixture reported problems, so no case below proves anything: %q", problems)
	}

	cases := []struct {
		name     string
		old, new string
		want     string // substring of the reported problems; "" means none
	}{
		{"v1.8 reads active", "context economy (complete)", "context economy (active)", `milestone v1.8 is "active"; it is recorded "complete"`},
		{"v1.9 reads planning", "workflow depth (active)", "workflow depth (planning)", `milestone v1.9 is "planning"; it is recorded "active"`},
		{"complete section keeps an open box", "- [x] second", "- [ ] second", "1 open box(es) under complete milestone v1.8"},
		{"indented open box still counts", "- [x] second", "  - [ ] second", "1 open box(es) under complete milestone v1.8"},
		{"active section with 4 boxes", "- [ ] fifth\n", "", "4 boxes under active milestone v1.9; it records 5"},
		{"active section with 6 boxes", "- [ ] fifth\n", "- [ ] fifth\n- [ ] sixth\n", "6 boxes under active milestone v1.9; it records 5"},
		{"ticked boxes count toward an active section", "- [ ] third", "- [x] third", ""},
		{"Azure DevOps as roadmap prose", "## Roadmap\n", "## Roadmap\n\nAzure DevOps is next.\n", "mentions Azure DevOps"},
		{"Azure DevOps inside a milestone", "- [x] second", "- [x] azure devops provider", "mentions Azure DevOps"},
		{"recorded milestone has no section", "### Milestone v1.8 — context economy (complete)\n", "", "milestone v1.8 (complete) has no section"},
		{"section has no record", "### Milestone v1.8 —", "### Milestone v1.7 —", "README describes milestone v1.7"},
		{"no roadmap section at all", "## Roadmap\n", "## Plans\n", "milestone v1.9 (active) has no section"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drifted := strings.Replace(roadmapGuardFixture, tc.old, tc.new, 1)
			if drifted == roadmapGuardFixture {
				t.Fatalf("%q is not in the fixture, so this case tests nothing", tc.old)
			}
			problems := readmeRoadmapProblems(drifted, records)
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("want no problems, got %q", problems)
				}
				return
			}
			if joined := strings.Join(problems, "\n"); !strings.Contains(joined, tc.want) {
				t.Fatalf("want a problem containing %q, got %q", tc.want, problems)
			}
		})
	}
}
