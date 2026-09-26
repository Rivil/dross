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
