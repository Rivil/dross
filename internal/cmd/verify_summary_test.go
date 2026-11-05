package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// TestVerifySummaryPrintsPackagesAndSurvivors: the summary finishVerify prints
// — the one attached and collected runs share — carries each package's counts
// beneath the leg line and every in-scope survivor's file:line.
func TestVerifySummaryPrintsPackagesAndSurvivors(t *testing.T) {
	const id = "leg-summary"
	root := finishFixture(t, id)
	// The NOT COVERED survivor would otherwise send finishVerify off to build a
	// real coverage profile.
	stubNotCoveredProfile(t, notCoveredFixtureProfile(t))
	ts := finishTests(id)
	m := ts.Languages[0].Mutation
	m.Killed, m.Survived, m.NotCovered = 9, 3, 1
	m.Files = map[string]mutation.FileStat{
		"internal/cmd/verify.go":   {Killed: 6, Survived: 2, NotCovered: 1},
		"internal/verify/score.go": {Killed: 3, Survived: 1},
	}
	m.Surviving = []mutation.Mutant{
		{File: "internal/cmd/verify.go", Line: 42, Op: "CONDITIONALS_NEGATION"},
		{File: "internal/cmd/verify.go", Line: 77, Op: "ARITHMETIC_BASE", NotCovered: true},
		{File: "internal/verify/score.go", Line: 9, Op: "INCREMENT_DECREMENT"},
	}
	ts.Languages[0].Files = []string{"internal/cmd/verify.go", "internal/verify/score.go"}

	out := captureStdout(t, func() {
		if err := finishVerify(root, id, finishSpec(id, "c-5"), ts, "local", nil); err != nil {
			t.Fatalf("finishVerify: %v", err)
		}
	})
	for _, want := range []string{
		"    internal/cmd: killed=6 survived=2 (not_covered=1) timeout=0",
		"    internal/verify: killed=3 survived=1 (not_covered=0) timeout=0",
		"      internal/cmd/verify.go:42",
		"      internal/cmd/verify.go:77 NOT COVERED",
		"      internal/verify/score.go:9",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("summary is missing %q:\n%s", want, out)
		}
	}
	leg := strings.Index(out, "go (gremlins):")
	row := strings.Index(out, "    internal/cmd: killed=")
	if leg < 0 || row < leg {
		t.Errorf("the package rows do not follow the leg's counts line:\n%s", out)
	}
}
