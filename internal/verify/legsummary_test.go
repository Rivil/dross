package verify

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

var rowRE = regexp.MustCompile(`^    (\S+): killed=(\d+) survived=(\d+) \(not_covered=(\d+)\) timeout=(\d+)$`)

// rows parses LegLines' package rows into dir → [killed, survived, nc, timeout].
func rows(t *testing.T, lines []string) map[string][4]int {
	t.Helper()
	out := map[string][4]int{}
	for _, l := range lines {
		m := rowRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		var v [4]int
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+2])
		}
		if _, dup := out[m[1]]; dup {
			t.Fatalf("directory %s has two rows", m[1])
		}
		out[m[1]] = v
	}
	return out
}

func TestPackageRowsSumToLeg(t *testing.T) {
	rep := &mutation.Report{
		Tool: "gremlins", Killed: 9, Survived: 4, NotCovered: 2, Timeout: 1,
		Files: map[string]mutation.FileStat{
			"internal/a/x.go": {Killed: 3, Survived: 1, NotCovered: 1},
			"internal/a/y.go": {Killed: 4, Survived: 2, NotCovered: 0, Timeout: 1},
			"internal/b/z.go": {Killed: 2, Survived: 1, NotCovered: 1},
		},
	}
	lines := LegLines(LanguageRun{Name: "go", Tool: "gremlins", Mutation: rep})
	got := rows(t, lines)
	want := map[string][4]int{
		"internal/a": {7, 3, 1, 1},
		"internal/b": {2, 1, 1, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want exactly %v (no per-file row)", got, want)
	}
	var sum [4]int
	for dir, w := range want {
		if got[dir] != w {
			t.Errorf("%s row = %v, want %v", dir, got[dir], w)
		}
		for i := range sum {
			sum[i] += got[dir][i]
		}
	}
	if sum != [4]int{rep.Killed, rep.Survived, rep.NotCovered, rep.Timeout} {
		t.Errorf("columns sum to %v, want the leg's %v", sum, [4]int{rep.Killed, rep.Survived, rep.NotCovered, rep.Timeout})
	}
	for _, l := range lines {
		if strings.Contains(l, "x.go:") && rowRE.MatchString(l) {
			t.Errorf("a per-file row was printed: %q", l)
		}
	}
}

func TestSurvivorLines(t *testing.T) {
	rep := &mutation.Report{
		Tool: "gremlins",
		Files: map[string]mutation.FileStat{
			"internal/a/x.go": {Survived: 3, NotCovered: 1},
			"internal/b/z.go": {Survived: 2},
		},
		Surviving: []mutation.Mutant{
			{File: "internal/b/z.go", Line: 7},
			{File: "internal/a/x.go", Line: 40, NotCovered: true},
			{File: "internal/a/x.go", Line: 12},
			{File: "internal/b/z.go", Line: 3},
			{File: "internal/a/x.go", Line: 12, Op: "ARITHMETIC_BASE", NotCovered: true},
		},
	}
	lines := LegLines(LanguageRun{Mutation: rep})
	joined := strings.Join(lines, "\n")
	want := []string{
		"      internal/a/x.go:12",
		"      internal/a/x.go:40 NOT COVERED",
		"      internal/b/z.go:3",
		"      internal/b/z.go:7",
	}
	var got []string
	for _, l := range lines {
		if strings.HasPrefix(l, "      ") {
			got = append(got, l)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("survivor lines:\n%s\nwant (grouped by file, each site once):\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// x.go:12 holds one covered and one NOT COVERED survivor: a test gap.
	if strings.Count(joined, "NOT COVERED") != 1 {
		t.Errorf("NOT COVERED marked %d times, want once:\n%s", strings.Count(joined, "NOT COVERED"), joined)
	}
	// An out-of-scope survivor never reaches Mutation.Surviving: FilterReport
	// drops it, so a scoped report lists only in-scope sites.
	scope := NewScope(ScopeInput{Git: []string{"internal/a/x.go"}})
	kept, _ := FilterReport(rep, scope, "go")
	for _, l := range LegLines(LanguageRun{Mutation: kept}) {
		if strings.Contains(l, "internal/b/") {
			t.Errorf("an out-of-scope survivor is listed: %q", l)
		}
	}
}

func TestStrykerRowsByDir(t *testing.T) {
	rep := &mutation.Report{
		Tool: "stryker",
		Files: map[string]mutation.FileStat{
			"src/a/x.ts": {Killed: 2, Survived: 1},
			"src/a/y.ts": {Killed: 1},
		},
	}
	got := rows(t, LegLines(LanguageRun{Name: "typescript", Tool: "stryker", Mutation: rep}))
	if len(got) != 1 || got["src/a"] != [4]int{3, 1, 0, 0} {
		t.Fatalf("rows %v, want one src/a row of killed=3 survived=1", got)
	}
}

func TestErrorLegUnchanged(t *testing.T) {
	rep := &mutation.Report{Files: map[string]mutation.FileStat{"a/x.go": {Killed: 1}}}
	if l := LegLines(LanguageRun{Error: "boom", Mutation: rep}); l != nil {
		t.Errorf("a failed leg grew lines: %q", l)
	}
	if l := LegLines(LanguageRun{}); l != nil {
		t.Errorf("a leg with no report grew lines: %q", l)
	}
}

func TestAdapterLanguageMatchesAdapterNames(t *testing.T) {
	for adapter, want := range map[mutation.Adapter]string{
		&mutation.Gremlins{}:   "go",
		&mutation.Stryker{}:    "typescript",
		&mutation.StrykerNet{}: "csharp",
	} {
		if got := adapterLanguage(adapter.Name()); got != want {
			t.Errorf("adapterLanguage(%q) = %q, want %q", adapter.Name(), got, want)
		}
	}
}
