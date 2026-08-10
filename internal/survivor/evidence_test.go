package survivor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// evidenceFixture writes a Go file whose lines cover every shape the deriver
// has to distinguish, and returns the repo root plus the file's relative path.
func evidenceFixture(t *testing.T) (root, rel string) {
	t.Helper()
	root = t.TempDir()
	rel = "pkg/sample.go"
	body := `package pkg

import "time"

const Deadline = 3 * time.Second

const Greeting = "hello" + ", " + "world"

func Describe(name string) string {
	return "name: " + name
}

func Total(a, b int) int {
	return a + b + 1
}

func Opaque(p, q int) int {
	return p + q
}
`
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, rel
}

// lineOfText finds the 1-based line holding substr, so the assertions below do
// not hardcode positions that a fixture edit would silently invalidate.
func lineOfText(t *testing.T, root, rel, substr string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	found, line := 0, 0
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, substr) {
			found++
			line = i + 1
		}
	}
	if found != 1 {
		t.Fatalf("fixture holds %d lines containing %q, want 1", found, substr)
	}
	return line
}

// writeProfile renders a go-cover profile covering the given line ranges.
func writeProfile(t *testing.T, dir, rel string, blocks [][3]int) string {
	t.Helper()
	body := "mode: set\n"
	for _, b := range blocks {
		body += "example.com/m/" + rel + ":" +
			strconv.Itoa(b[0]) + ".1," + strconv.Itoa(b[1]) + ".2 1 " + strconv.Itoa(b[2]) + "\n"
	}
	path := filepath.Join(dir, "cover.out")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestArithmeticOnStringConcatIsInapplicable is the split that ended
// bogus_arithmetic_class's original premise. gremlins swaps `+` for `-`, and
// `-` is undefined on strings, so a string-concatenation line's ARITHMETIC_BASE
// mutant cannot compile and no test can distinguish it. On numeric operands the
// same operator is a real change and the survivor must be killed.
func TestArithmeticOnStringConcatIsInapplicable(t *testing.T) {
	root, rel := evidenceFixture(t)

	cases := []struct {
		name string
		text string
		want Applicability
	}{
		{"string concatenation", `return "name: " + name`, Inapplicable},
		{"const string concatenation", `const Greeting =`, Inapplicable},
		{"integer arithmetic", `return a + b + 1`, Applicable},
		{"time.Duration multiplication", `const Deadline =`, Applicable},
		{"all-identifier operands are undecidable", `return p + q`, ApplicabilityUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := lineOfText(t, root, rel, tc.text)
			got, err := ApplicabilityAt(root, rel, line, "ARITHMETIC_BASE")
			if err != nil {
				t.Fatalf("ApplicabilityAt: %v", err)
			}
			if got != tc.want {
				t.Errorf("ApplicabilityAt(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}

	// A conditional operator always has something to negate — the analysis is
	// specific to ARITHMETIC_BASE and must not silently answer for the rest.
	line := lineOfText(t, root, rel, `return "name: " + name`)
	if got, _ := ApplicabilityAt(root, rel, line, "CONDITIONALS_NEGATION"); got != Applicable {
		t.Errorf("non-arithmetic op = %q, want %q", got, Applicable)
	}
}

// TestMissingProfileIsUnknownNotUncovered: absence of a profile is absence of
// evidence. If a missing profile read as "not covered", the cheapest way to
// justify an acceptance would be to not run coverage at all.
func TestMissingProfileIsUnknownNotUncovered(t *testing.T) {
	prof, err := ParseProfile(filepath.Join(t.TempDir(), "nope.out"))
	if err != nil {
		t.Fatalf("a missing profile must not be an error: %v", err)
	}
	if prof != nil {
		t.Fatalf("a missing profile must yield a nil Profile, got %+v", prof)
	}

	cov, count := prof.CoverageAt("internal/cmd/doctor.go", 10)
	if cov != CoverageUnknown {
		t.Errorf("Coverage = %q, want %q — \"I did not look\" is not \"it never ran\"", cov, CoverageUnknown)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	root, rel := evidenceFixture(t)
	e := Derive(root, rel, lineOfText(t, root, rel, `return a + b + 1`), "ARITHMETIC_BASE", prof, false)
	if e.Killable {
		t.Error("an unknown-coverage line was reported killable")
	}
	if e.CeilingEligible {
		t.Error("an unknown-coverage line was reported ceiling-eligible")
	}
}

// TestCeilingEligibilityRequiresCoverage: the attribution ceiling is the
// DISAGREEMENT between go-cover (ran) and the mutation tool (NOT COVERED). A
// line nothing executes is a plain coverage gap, and letting it claim the
// ceiling would turn the category into a blanket excuse for untested code.
func TestCeilingEligibilityRequiresCoverage(t *testing.T) {
	root, rel := evidenceFixture(t)
	line := lineOfText(t, root, rel, `return a + b + 1`)

	uncovered := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{{line, line, 0}}))
	e := Derive(root, rel, line, "ARITHMETIC_BASE", uncovered, true)
	if e.Coverage != CoverageNotCovered {
		t.Fatalf("premise broken: Coverage = %q", e.Coverage)
	}
	if e.CeilingEligible {
		t.Error("a line with count 0 claimed the attribution ceiling — that is a coverage gap, not a ceiling")
	}

	covered := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{{line, line, 3}}))
	e = Derive(root, rel, line, "ARITHMETIC_BASE", covered, true)
	if !e.CeilingEligible {
		t.Error("a covered line the tool reported NOT COVERED is exactly the ceiling shape")
	}
	// The ceiling DOMINATES killability. This line is covered and its operator
	// applies, so it looks killable — but the tool believes it never ran and so
	// never builds the mutant. Calling it killable would send someone to write
	// tests that cannot possibly move it.
	if e.Killable {
		t.Error("a ceiling-eligible survivor was reported killable — no test can kill it")
	}
	if !strings.Contains(e.Why, "ceiling category") {
		t.Errorf("Why = %q, must route a ceiling survivor to the category rather than to more tests", e.Why)
	}
	// And with no disagreement there is no ceiling.
	e = Derive(root, rel, line, "ARITHMETIC_BASE", covered, false)
	if e.CeilingEligible {
		t.Error("ceiling claimed with no disagreement between the two tools")
	}
}

// TestCoveredAndApplicableIsKillable is the cell an acceptance must never
// claim: the line runs, the operator changes it, so a test can tell the mutant
// apart. Accepting one of these is the laundering c-3 forbids.
func TestCoveredAndApplicableIsKillable(t *testing.T) {
	root, rel := evidenceFixture(t)

	arith := lineOfText(t, root, rel, `return a + b + 1`)
	concat := lineOfText(t, root, rel, `return "name: " + name`)
	prof := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{
		{arith, arith, 5},
		{concat, concat, 5},
	}))

	killable := Derive(root, rel, arith, "ARITHMETIC_BASE", prof, false)
	if !killable.Killable {
		t.Errorf("covered + applicable must be killable: %+v", killable)
	}
	if killable.Count != 5 {
		t.Errorf("Count = %d, want 5", killable.Count)
	}
	if !strings.Contains(killable.Why, "do not accept it") {
		t.Errorf("Why = %q, must say plainly that this one is not acceptable", killable.Why)
	}

	// Covered but inapplicable is NOT killable — same coverage, opposite verdict.
	inapplicable := Derive(root, rel, concat, "ARITHMETIC_BASE", prof, false)
	if inapplicable.Killable {
		t.Errorf("a string-concatenation line was reported killable: %+v", inapplicable)
	}
	if inapplicable.Coverage != CoverageCovered {
		t.Errorf("premise broken — the concat line should be covered: %+v", inapplicable)
	}
}

// TestProfileMatchesRepoRelativePath: profile keys are import paths and
// survivors are repo-relative, so the lookup matches on suffix. A lookup that
// required equality would report every line unknown and quietly disable the
// whole analysis.
func TestProfileMatchesRepoRelativePath(t *testing.T) {
	root, rel := evidenceFixture(t)
	line := lineOfText(t, root, rel, `return a + b + 1`)
	prof := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{{line, line, 2}}))

	if cov, count := prof.CoverageAt(rel, line); cov != CoverageCovered || count != 2 {
		t.Errorf("CoverageAt(%q) = %q/%d, want covered/2", rel, cov, count)
	}
	// A file the profile does not mention at all is not covered — nothing ran
	// it — which is distinct from the nil-profile unknown above.
	if cov, _ := prof.CoverageAt("other/pkg/z.go", 1); cov != CoverageNotCovered {
		t.Errorf("unmentioned file = %q, want %q", cov, CoverageNotCovered)
	}
}

func mustProfile(t *testing.T, path string) *Profile {
	t.Helper()
	p, err := ParseProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("profile parsed to nil")
	}
	return p
}

// TestNoCoverageBlockIsNotACoverageGap is the const-initializer distinction,
// and it decides opposite actions. go-cover instruments STATEMENTS, so a const
// initializer belongs to no block at all — it is not merely untested, it is
// uninstrumentable. No test can make it covered, so the mutation tool never
// builds its mutant and no test can ever kill it.
//
// Collapsing this into "not covered" sends someone to write a test that cannot
// possibly work, which is exactly what happened to argfence/policy.go:33,
// stack/detect.go:260 and telemetry/telemetry.go:46 before this existed.
func TestNoCoverageBlockIsNotACoverageGap(t *testing.T) {
	root, rel := evidenceFixture(t)
	constLine := lineOfText(t, root, rel, `const Deadline =`)
	stmtLine := lineOfText(t, root, rel, `return a + b + 1`)

	// The file IS instrumented — a statement block exists — but the const line
	// falls in no block.
	prof := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{{stmtLine, stmtLine, 4}}))

	if cov, _ := prof.CoverageAt(rel, constLine); cov != CoverageNoBlock {
		t.Errorf("const initializer coverage = %q, want %q", cov, CoverageNoBlock)
	}
	e := Derive(root, rel, constLine, "ARITHMETIC_BASE", prof, true)
	if e.Killable {
		t.Error("a const initializer was reported killable")
	}
	if !strings.Contains(e.Why, "accept it") {
		t.Errorf("Why = %q, must route a const initializer to acceptance rather than to a test", e.Why)
	}

	// A statement block that simply never ran is the OPPOSITE verdict: a real
	// coverage gap, and the advice must be to cover it.
	unrun := mustProfile(t, writeProfile(t, t.TempDir(), rel, [][3]int{{stmtLine, stmtLine, 0}}))
	gap := Derive(root, rel, stmtLine, "ARITHMETIC_BASE", unrun, false)
	if gap.Coverage != CoverageNotCovered {
		t.Errorf("an unrun statement block = %q, want %q", gap.Coverage, CoverageNotCovered)
	}
	if !strings.Contains(gap.Why, "cover it") {
		t.Errorf("Why = %q, must send a real coverage gap to a test", gap.Why)
	}

	// A file the profile never mentions is not-covered, never no-block: its
	// package simply was not tested, and a test would fix that.
	if cov, _ := prof.CoverageAt("other/pkg/z.go", 1); cov != CoverageNotCovered {
		t.Errorf("unmentioned file = %q, want %q", cov, CoverageNotCovered)
	}
}
