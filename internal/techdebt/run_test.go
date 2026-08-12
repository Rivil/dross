package techdebt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewRunNeverClobbers fails if a second run with the same timestamp+sha
// overwrites the first: it must get a "-2" suffix and both dirs must exist.
func TestNewRunNeverClobbers(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	sha := "deadbee"

	first, err := NewRun(root, now, sha)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRun(root, now, sha)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("second run clobbered the first: both %q", first)
	}
	if !strings.HasSuffix(second, "-2") {
		t.Fatalf("collision suffix missing: second run = %q, want a -2 suffix", second)
	}
	for _, d := range []string{first, second} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("run dir %q not created: err=%v", d, err)
		}
	}
}

// TestWriteReportRendersFindings fails if WriteReport drops findings: the run
// dir's report file must exist and name the scanned finding.
func TestWriteReportRendersFindings(t *testing.T) {
	root := t.TempDir()
	runDir, err := NewRun(root, time.Date(2026, 6, 27, 11, 0, 0, 0, time.UTC), "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	fs := []Finding{
		{File: "internal/x.go", Line: 42, Class: ClassMarker, Detail: "TODO"},
		{File: "internal/big.go", Line: 0, Class: ClassOversizedFile, Detail: "800 lines"},
	}
	if err := WriteReport(runDir, fs); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(runDir, ReportName))
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "internal/x.go:42") || !strings.Contains(s, "TODO") {
		t.Fatalf("report omitted the marker finding:\n%s", s)
	}
	if !strings.Contains(s, "internal/big.go") || !strings.Contains(s, "800 lines") {
		t.Fatalf("report omitted the oversized-file finding:\n%s", s)
	}
}

// TestRenderReportEmpty fails if a clean scan doesn't render the explicit
// no-findings line.
func TestRenderReportEmpty(t *testing.T) {
	if got := renderReport(nil); !strings.Contains(got, "No tech-debt findings") {
		t.Fatalf("empty report = %q, want a no-findings line", got)
	}
}

// TestShortSHADegradesToNogit covers ShortSHA's failure arm. It is documented
// as best-effort — a run must not fail because the tree is not a git repo — so
// the fallback is the whole contract. Inverted, a real repo would be labelled
// "nogit" and every run id would collide with every other.
func TestShortSHADegradesToNogit(t *testing.T) {
	// A bare temp dir is not a git repo, so rev-parse fails.
	if got := ShortSHA(t.TempDir()); got != "nogit" {
		t.Errorf("ShortSHA(non-repo) = %q, want %q", got, "nogit")
	}

	// And in a real repo it returns something that is NOT the fallback, so the
	// guard is genuinely conditional. This test file lives inside this repo.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got := ShortSHA(cwd)
	if got == "nogit" {
		t.Errorf("ShortSHA(%q) = nogit inside a real git repo", cwd)
	}
	if strings.ContainsAny(got, " \n\t") {
		t.Errorf("ShortSHA = %q, want git's output trimmed", got)
	}
}

// TestNormalizeSHAFallsBackOnEmpty is the other half: git can exit 0 and print
// nothing. An empty run-id component would make two runs share a directory name
// and silently clobber each other, which NewRun's suffixing then papers over.
func TestNormalizeSHAFallsBackOnEmpty(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "nogit"},
		{"   ", "nogit"},
		{"\n\t ", "nogit"},
		{"abc1234\n", "abc1234"},
		{"  abc1234  ", "abc1234"},
	}
	for _, tc := range cases {
		if got := normalizeSHA(tc.in); got != tc.want {
			t.Errorf("normalizeSHA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRenderReportCountsEveryClass drives all three arms of the tally switch.
// The counts are the first line a reader sees, and a class silently counted as
// another makes the scan report a different problem than the one it found.
func TestRenderReportCountsEveryClass(t *testing.T) {
	out := renderReport([]Finding{
		{File: "a.go", Line: 1, Class: ClassMarker, Detail: "TODO"},
		{File: "a.go", Line: 2, Class: ClassMarker, Detail: "FIXME"},
		{File: "b.go", Line: 0, Class: ClassOversizedFile, Detail: "1200 lines"},
		{File: "c.go", Line: 9, Class: ClassLongLine, Detail: "240 chars"},
		{File: "c.go", Line: 11, Class: ClassLongLine, Detail: "300 chars"},
		{File: "c.go", Line: 13, Class: ClassLongLine, Detail: "310 chars"},
	})

	// Distinct counts per class, so no two can be swapped undetected.
	if want := "6 findings: 2 markers, 1 oversized files, 3 long lines."; !strings.Contains(out, want) {
		t.Errorf("summary line wrong:\nwant %q\nin:\n%s", want, out)
	}

	// An unrecognised class is counted in the total but in no bucket — the
	// switch has no default, and that is deliberate.
	mixed := renderReport([]Finding{
		{File: "a.go", Class: ClassMarker},
		{File: "b.go", Class: "something-new"},
	})
	if want := "2 findings: 1 markers, 0 oversized files, 0 long lines."; !strings.Contains(mixed, want) {
		t.Errorf("an unknown class must count in the total only:\nwant %q\nin:\n%s", want, mixed)
	}
}
