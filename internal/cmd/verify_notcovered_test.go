package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

// notCoveredFixtureProfile is a go-cover profile for internal/fake/f.go with a
// count-0 block over lines 10-12 and a covered one over 20-22. Every other line
// of that file sits in no block; any other file is absent.
func notCoveredFixtureProfile(t *testing.T) *survivor.Profile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.out")
	body := "mode: set\n" +
		"github.com/x/y/internal/fake/f.go:10.1,12.2 1 0\n" +
		"github.com/x/y/internal/fake/f.go:20.1,22.2 1 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := survivor.ParseProfile(path)
	if err != nil || prof == nil {
		t.Fatalf("parse fixture profile: %v", err)
	}
	return prof
}

// TestNotCoveredSplitAgreesWithDrainDerive: verify's split is the drain's
// classifier, not a second one that could drift from it. For every fixture
// line, no-block and count-0 land exactly where survivor.Derive puts them.
func TestNotCoveredSplitAgreesWithDrainDerive(t *testing.T) {
	prof := notCoveredFixtureProfile(t)
	repoRoot := t.TempDir()
	c := profileClassifier{repoRoot: repoRoot, prof: prof}

	cases := []struct {
		file string
		line int
		want verify.NotCoveredKind
	}{
		{"internal/fake/f.go", 5, verify.NotCoveredNoBlock},   // no block
		{"internal/fake/f.go", 10, verify.NotCoveredTestGap},  // count-0 block, first line
		{"internal/fake/f.go", 12, verify.NotCoveredTestGap},  // count-0 block, last line
		{"internal/fake/f.go", 21, verify.NotCoveredUnplaced}, // runs: the ceiling, not a gap
		{"internal/other/g.go", 3, verify.NotCoveredTestGap},  // package never tested
	}
	for _, tc := range cases {
		got := c.ClassifyNotCovered(tc.file, tc.line, "CONDITIONALS_NEGATION")
		if got != tc.want {
			t.Errorf("%s:%d = %v, want %v", tc.file, tc.line, got, tc.want)
		}
		d := survivor.Derive(repoRoot, tc.file, tc.line, "CONDITIONALS_NEGATION", prof, true)
		if (got == verify.NotCoveredNoBlock) != (d.Coverage == survivor.CoverageNoBlock) ||
			(got == verify.NotCoveredTestGap) != (d.Coverage == survivor.CoverageNotCovered) {
			t.Errorf("%s:%d: verify says %v, the drain's Derive says %q", tc.file, tc.line, got, d.Coverage)
		}
	}

	if got := (profileClassifier{repoRoot: repoRoot}).ClassifyNotCovered("internal/fake/f.go", 5, "CONDITIONALS_NEGATION"); got != verify.NotCoveredUnplaced {
		t.Errorf("with no profile a no-block line = %v, want unplaced — no evidence is not uncoverable", got)
	}
}

// notCoveredTests is a finished run: 26 killed and 6 NOT COVERED survivors on
// the given lines of internal/fake/f.go.
func notCoveredTests(phaseID string, lines ...int) *verify.Tests {
	var surviving []mutation.Mutant
	for _, l := range lines {
		surviving = append(surviving, mutation.Mutant{
			File: "internal/fake/f.go", Line: l, Op: "CONDITIONALS_NEGATION", NotCovered: true,
		})
	}
	return &verify.Tests{
		Phase:       phaseID,
		GeneratedAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
		Languages: []verify.LanguageRun{{
			Name:  "go",
			Tool:  "gremlins",
			Files: []string{"internal/fake/f.go"},
			Mutation: &mutation.Report{
				Tool: "gremlins", Killed: 26, Survived: len(lines), NotCovered: len(lines),
				Surviving: surviving,
			},
		}},
	}
}

// stubNotCoveredProfile swaps the profile seam for one returning prof, and
// records the packages each call asked for.
func stubNotCoveredProfile(t *testing.T, prof *survivor.Profile) *[][]string {
	t.Helper()
	var calls [][]string
	orig := notCoveredProfileFn
	notCoveredProfileFn = func(_ string, pkgs []string) *survivor.Profile {
		calls = append(calls, pkgs)
		return prof
	}
	t.Cleanup(func() { notCoveredProfileFn = orig })
	return &calls
}

// TestFinishVerifySplitsNotCoveredAgainstTheProfile is c-5 end to end: the
// summary a verify run prints and persists comes from the profile, so 6
// count-0 mutants are a test gap and 2 no-block ones of 6 are the only
// uncoverable ones.
func TestFinishVerifySplitsNotCoveredAgainstTheProfile(t *testing.T) {
	for _, tc := range []struct {
		name             string
		lines            []int
		noBlock, testGap int
		want, never      string
	}{
		{"all count-0", []int{10, 10, 11, 11, 12, 12}, 0, 6,
			"of which 6 NOT COVERED in a coverage block no test ran — a test gap", "uncoverable"},
		{"2 no-block, 4 count-0", []int{5, 6, 10, 11, 12, 12}, 2, 4,
			"of which 2 uncoverable by construction (no go-cover block holds their line) — efficacy over the 30 reachable = 0.87", "unplaced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "not-covered-split"
			root := finishFixture(t, id)
			calls := stubNotCoveredProfile(t, notCoveredFixtureProfile(t))

			out := captureStdout(t, func() {
				if err := finishVerify(root, id, finishSpec(id, "c-5"), notCoveredTests(id, tc.lines...), "helicon", nil); err != nil {
					t.Fatalf("finishVerify: %v", err)
				}
			})

			if len(*calls) != 1 || !slices.Equal((*calls)[0], []string{"./internal/fake"}) {
				t.Errorf("profile built for %q, want once for ./internal/fake", *calls)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("summary is missing %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, tc.never) {
				t.Errorf("summary says %q, which this split must not:\n%s", tc.never, out)
			}
			_, verifyPath := verify.FilePaths(root, id)
			v, err := verify.LoadVerify(verifyPath)
			if err != nil {
				t.Fatal(err)
			}
			if v.Summary.MutantsNoBlock != tc.noBlock || v.Summary.MutantsTestGap != tc.testGap {
				t.Errorf("verify.toml records no-block=%d test-gap=%d, want %d and %d",
					v.Summary.MutantsNoBlock, v.Summary.MutantsTestGap, tc.noBlock, tc.testGap)
			}
		})
	}
}

// TestFinishVerifyBuildsNoProfileWithoutGoNotCovered: the profile compiles test
// binaries, so it is built only when a Go NOT COVERED survivor needs placing.
// A run with none, or with only non-Go ones, never pays for it — and the
// non-Go ones stay reachable, unplaced.
func TestFinishVerifyBuildsNoProfileWithoutGoNotCovered(t *testing.T) {
	const id = "not-covered-none"
	root := finishFixture(t, id)
	calls := stubNotCoveredProfile(t, notCoveredFixtureProfile(t))

	captureStdout(t, func() {
		if err := finishVerify(root, id, finishSpec(id, "c-5"), finishTests(id), "helicon", nil); err != nil {
			t.Fatalf("finishVerify: %v", err)
		}
	})
	ts := notCoveredTests(id, 5)
	ts.Languages[0].Mutation.Surviving[0].File = "web/app.ts"
	out := captureStdout(t, func() {
		if err := finishVerify(root, id, finishSpec(id, "c-5"), ts, "helicon", nil); err != nil {
			t.Fatalf("finishVerify: %v", err)
		}
	})

	if len(*calls) != 0 {
		t.Errorf("a profile was built for %q with no Go NOT COVERED survivor to place", *calls)
	}
	if !strings.Contains(out, "of which 1 NOT COVERED unplaced") {
		t.Errorf("the non-Go NOT COVERED survivor is not named as unplaced:\n%s", out)
	}
}
