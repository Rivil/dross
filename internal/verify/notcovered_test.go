package verify

import (
	"slices"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// fakeCoverage places a line by its number alone: whatever the test maps it to.
type fakeCoverage map[int]NotCoveredKind

func (f fakeCoverage) ClassifyNotCovered(_ string, line int, _ string) NotCoveredKind {
	return f[line]
}

func notCoveredRun(survivors ...mutation.Mutant) *Tests {
	return &Tests{Languages: []LanguageRun{
		{Name: "go", Tool: "gremlins"}, // a leg that produced no report is skipped, not dereferenced
		{Name: "go", Tool: "gremlins", Mutation: &mutation.Report{Surviving: survivors}},
	}}
}

// TestSplitNotCoveredCountsOnlyNotCoveredSurvivors is c-5's split: a no-block
// line is uncoverable, a line in a block is a test gap, and a line no profile
// placed is neither — it stays reachable without being called either.
// A survivor the tests ran and missed is not NOT COVERED at all, so even a
// line the profile would call no-block must not count.
func TestSplitNotCoveredCountsOnlyNotCoveredSurvivors(t *testing.T) {
	run := notCoveredRun(
		mutation.Mutant{File: "a.go", Line: 1, NotCovered: true},
		mutation.Mutant{File: "a.go", Line: 2, NotCovered: true},
		mutation.Mutant{File: "a.go", Line: 2, NotCovered: true},
		mutation.Mutant{File: "a.go", Line: 3, NotCovered: true},
		mutation.Mutant{File: "a.go", Line: 1}, // LIVED on a no-block line
	)
	v := &Verify{}
	// Stale counts from an earlier split must not survive into this one.
	v.Summary.MutantsNoBlock, v.Summary.MutantsTestGap = 9, 9

	SplitNotCovered(v, run, fakeCoverage{1: NotCoveredNoBlock, 2: NotCoveredTestGap, 3: NotCoveredUnplaced})

	if v.Summary.MutantsNoBlock != 1 || v.Summary.MutantsTestGap != 2 {
		t.Errorf("no-block=%d test-gap=%d, want 1 and 2", v.Summary.MutantsNoBlock, v.Summary.MutantsTestGap)
	}
}

// TestNotCoveredPackagesAreLocalRootedAndDeduped pins what reaches `go test`'s
// argv: only Go files, only repo-local paths, each package once, every one
// "./"-rooted so none can read as a flag.
func TestNotCoveredPackagesAreLocalRootedAndDeduped(t *testing.T) {
	run := notCoveredRun(
		mutation.Mutant{File: "internal/b/x.go", NotCovered: true},
		mutation.Mutant{File: "internal/b/y.go", NotCovered: true},
		mutation.Mutant{File: "internal/a/z.go", NotCovered: true},
		mutation.Mutant{File: "root.go", NotCovered: true},
		mutation.Mutant{File: "internal/c/lived.go"},              // ran and missed: not NOT COVERED
		mutation.Mutant{File: "web/app.ts", NotCovered: true},     // not Go
		mutation.Mutant{File: "/etc/abs.go", NotCovered: true},    // absolute
		mutation.Mutant{File: "../escape/e.go", NotCovered: true}, // climbs out
		mutation.Mutant{File: "-exec=x/f.go", NotCovered: true},   // flag-shaped, rooted below
	)

	got := NotCoveredPackages(run)

	want := []string{"./-exec=x", "./.", "./internal/a", "./internal/b"}
	if !slices.Equal(got, want) {
		t.Errorf("packages = %q, want %q", got, want)
	}
	for _, p := range got {
		if p[0] == '-' {
			t.Errorf("package %q would be read as a flag", p)
		}
	}
}

// TestNotCoveredPackagesEmptyWithoutNotCovered: no NOT COVERED survivor means
// no package, so the caller builds no profile at all.
func TestNotCoveredPackagesEmptyWithoutNotCovered(t *testing.T) {
	if got := NotCoveredPackages(notCoveredRun(mutation.Mutant{File: "a/b.go"})); len(got) != 0 {
		t.Errorf("packages = %q, want none", got)
	}
}
