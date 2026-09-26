package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const testModule = "example.com/m"

// checkProfile writes go.mod and a profile holding body into a temp dir and
// runs the checker over them.
func checkProfile(t *testing.T, body string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+testModule+"\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prof := filepath.Join(dir, "cover.out")
	if err := os.WriteFile(prof, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code = run(dir, []string{prof}, &out, &errb)
	return code, out.String(), errb.String()
}

// block is one profile line for file (module-relative) with n statements,
// covered or not. Each call's line number is distinct so blocks never merge.
func block(file string, line, n int, covered bool) string {
	count := "0"
	if covered {
		count = "1"
	}
	l := strconv.Itoa(line)
	return testModule + "/" + file + ":" + l + ".1," + l + ".20 " + strconv.Itoa(n) + " " + count + "\n"
}

// fileOf builds a file with `covered` of `total` statements covered, one
// statement per block.
func fileOf(file string, covered, total int) string {
	var b strings.Builder
	for i := 0; i < total; i++ {
		b.WriteString(block(file, i+1, 1, i < covered))
	}
	return b.String()
}

// offendersIn reads the file names off the checker's below-floor listing.
func offendersIn(stdout string) []string {
	var out []string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(l, "  ") {
			out = append(out, strings.Fields(l)[0])
		}
	}
	return out
}

// TestFloorBoundary: exactly half passes; anything under half fails. A `<`
// widened to `<=`, a rounding comparison or a moved constant each flip a row.
func TestFloorBoundary(t *testing.T) {
	for _, tc := range []struct {
		covered, total int
		below          bool
	}{
		{1, 2, false},
		{2, 4, false},
		{99, 200, true},
		{1, 3, true},
		{0, 5, true},
	} {
		if got := belowFloor(tc.covered, tc.total); got != tc.below {
			t.Errorf("belowFloor(%d, %d) = %v, want %v", tc.covered, tc.total, got, tc.below)
		}
	}

	code, out, _ := checkProfile(t, "mode: set\n"+fileOf("internal/half/h.go", 1, 2))
	if code != 0 || !strings.Contains(out, "1 files measured, lowest internal/half/h.go 50.0%") {
		t.Errorf("an exactly-50%% file: exit %d, stdout %q", code, out)
	}
}

// TestPassingSummaryNamesTheLowestFile: the passing line names the true
// lowest of several files. The ratios are picked so integer division gives a
// different pick from multiplication: either `*` of the cross-multiply turned
// into `/` picks c or keeps a, and the negated comparison keeps a.
func TestPassingSummaryNamesTheLowestFile(t *testing.T) {
	body := "mode: set\n" +
		fileOf("internal/a/a.go", 3, 4) +
		fileOf("internal/b/b.go", 3, 5) +
		fileOf("internal/c/c.go", 2, 3)
	code, out, errb := checkProfile(t, body)
	if code != 0 || !strings.Contains(out, "3 files measured, lowest internal/b/b.go 60.0%") {
		t.Errorf("exit %d, stdout %q, stderr %q; want b at 60.0%% as the lowest", code, out, errb)
	}
}

// TestPassingSummaryTieKeepsFirstByPath: two files at the same ratio in
// different terms both read 50%; the first by path is the one named, so a `<`
// widened to `<=` — which takes the later one — is caught.
func TestPassingSummaryTieKeepsFirstByPath(t *testing.T) {
	body := "mode: set\n" +
		fileOf("internal/a/a.go", 1, 2) +
		fileOf("internal/b/b.go", 2, 4) +
		fileOf("internal/c/c.go", 3, 3)
	code, out, errb := checkProfile(t, body)
	if code != 0 || !strings.Contains(out, "3 files measured, lowest internal/a/a.go 50.0%") {
		t.Errorf("exit %d, stdout %q, stderr %q; want the tie to keep a", code, out, errb)
	}
}

// TestParseErrorsNameTheProfileLine: a parse error points at the profile's
// own line number — the mode line is 1, so the second block is line 3.
func TestParseErrorsNameTheProfileLine(t *testing.T) {
	span := testModule + "/internal/a/a.go:1.1,1.20 "
	for _, tc := range []struct {
		name, body string
		wants      []string
	}{
		{
			"statement count overflows an int",
			"mode: set\n" + block("internal/a/a.go", 1, 1, true) + testModule + "/internal/a/a.go:2.1,2.20 99999999999999999999 1\n",
			[]string{"cover.out:3:", "statement count"},
		},
		{
			"one span listed with two statement counts",
			"mode: set\n" + span + "3 1\n" + span + "2 1\n",
			[]string{"cover.out:3:", "lists 2 statements, earlier 3"},
		},
	} {
		code, out, errb := checkProfile(t, tc.body)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2 (stdout %q, stderr %q)", tc.name, code, out, errb)
			continue
		}
		for _, w := range tc.wants {
			if !strings.Contains(errb, w) {
				t.Errorf("%s: stderr %q does not contain %q", tc.name, errb, w)
			}
		}
	}
}

// TestScopeExcludesOnlyInternalCmd: internal/cmd/ is the one exclusion, by
// directory — not every path that merely starts with "internal/cmd" — and
// nothing outside internal/ is measured at all.
func TestScopeExcludesOnlyInternalCmd(t *testing.T) {
	body := "mode: set\n" +
		fileOf("internal/cmd/x.go", 0, 3) +
		fileOf("internal/cmdx/a.go", 0, 3) +
		fileOf("internal/cmdline/x.go", 0, 3) +
		fileOf("internal/ok/ok.go", 3, 3) +
		fileOf("cmd/dross/main.go", 0, 3) +
		fileOf("cmd/testsummary/main.go", 0, 3)
	code, out, _ := checkProfile(t, body)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, out)
	}
	if got, want := offendersIn(out), []string{"internal/cmdline/x.go", "internal/cmdx/a.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("offenders = %v, want %v", got, want)
	}
	if !strings.Contains(out, "2 of 3 files below") {
		t.Errorf("stdout %q does not count 3 in-scope files", out)
	}
}

// TestDuplicateBlocksCountOnce: one span listed twice — as two packages' runs
// both report it — counts its statements once, and as covered because one
// run reached it. Counted twice it would read 3/6, not 3/3.
func TestDuplicateBlocksCountOnce(t *testing.T) {
	span := testModule + "/internal/dup/d.go:1.1,1.20 3 "
	code, out, _ := checkProfile(t, "mode: set\n"+span+"0\n"+span+"3\n")
	if code != 0 || !strings.Contains(out, "1 files measured, lowest internal/dup/d.go 100.0%") {
		t.Errorf("exit %d, stdout %q; want the duplicated block counted once, as covered", code, out)
	}
}

// TestVacuousInputIsExitTwo: nothing measured must never read as a pass.
func TestVacuousInputIsExitTwo(t *testing.T) {
	good := "mode: set\n" + fileOf("internal/ok/ok.go", 1, 1)
	for _, tc := range []struct{ name, body string }{
		{"mode line only", "mode: set\n"},
		{"only internal/cmd", "mode: set\n" + fileOf("internal/cmd/x.go", 1, 1)},
		{"only unprefixed paths", "mode: set\n" + fileOf("cmd/dross/main.go", 1, 1) + "other.org/z/internal/a.go:1.1,1.2 1 1\n"},
		{"non-numeric count", "mode: set\n" + testModule + "/internal/a/a.go:1.1,1.20 1 x\n"},
		{"missing span", "mode: set\n" + testModule + "/internal/a/a.go 1 1\n"},
		{"no mode line", testModule + "/internal/a/a.go:1.1,1.20 1 1\n"},
	} {
		if code, out, errb := checkProfile(t, tc.body); code != 2 {
			t.Errorf("%s: exit %d, want 2 (stdout %q, stderr %q)", tc.name, code, out, errb)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+testModule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prof := filepath.Join(dir, "cover.out")
	if err := os.WriteFile(prof, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		dir  string
		args []string
	}{
		{"missing profile", dir, []string{filepath.Join(dir, "absent.out")}},
		{"no argument", dir, nil},
		{"stray flag", dir, []string{"-allow", prof}},
		{"a flag as the profile", dir, []string{"-v"}},
		{"extra argument", dir, []string{prof, prof}},
		{"no go.mod", t.TempDir(), []string{prof}},
	} {
		var out, errb bytes.Buffer
		if code := run(tc.dir, tc.args, &out, &errb); code != 2 {
			t.Errorf("%s: exit %d, want 2 (stdout %q, stderr %q)", tc.name, code, out.String(), errb.String())
		}
	}
	// The same good profile with valid usage passes, so each refusal above is
	// about its own input and not about the fixture.
	var out, errb bytes.Buffer
	if code := run(dir, []string{prof}, &out, &errb); code != 0 {
		t.Errorf("the control run exited %d: %s%s", code, out.String(), errb.String())
	}
}
