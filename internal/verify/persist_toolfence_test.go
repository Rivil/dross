package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
)

// c-2, read off the disk. Everything upstream of this file asserts what an
// adapter RETURNS; this asserts what actually lands in tests.json and
// verify.toml, because those two files are what the criterion is about.
//
// BOTH artifacts, in one test. A fix applied to one writer and not the other is
// the failure this shape exists to catch: LegSummary.Error is populated from
// LanguageRun.Error, so the two agree or a leg's cause is missing from whichever
// file the reader happened to open.
const canaryB = "CANARY-B-6c40"

// strykerReportlessError is the error the stryker adapter returns on the path
// this phase changed: the record, wrapped with dross's own context. Built
// through the exported recorder so a rewording of the record reaches this test.
func strykerReportlessError(reportPath string, exit, observed int) error {
	return fmt.Errorf("stryker did not write a report at %s: %w",
		reportPath, mutation.RecordToolFailure("stryker", exit, mutation.Observed(observed)))
}

// leakyAdapterError is what the SAME path produced before this phase: the head
// of stryker's output quoted straight into the error string. It is here to
// prove the absence assertions below can fail — an "is not on disk" check over
// an artifact that never carried the thing proves nothing.
func leakyAdapterError(reportPath string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "stryker did not write a report at %s.\n", reportPath)
	b.WriteString("the head of stryker's output, which is where the cause is:\n\n")
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, "    %s line %d\n", canaryB, i)
	}
	return fmt.Errorf("%s", b.String())
}

// TestFailedLegReachesDiskClean drives a reportless stryker leg through
// RunScoped, Tests.Save, Skeleton and Verify.Save, then reads the bytes back.
func TestFailedLegReachesDiskClean(t *testing.T) {
	const exit = 1
	const observed = 4096

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "reports", "mutation", "mutation.json")
	legErr := strykerReportlessError(reportPath, exit, observed)

	testsPath, verifyPath := writeRun(t, dir, legErr)
	testsRaw := readFile(t, testsPath)
	verifyRaw := readFile(t, verifyPath)

	// The facts ABOUT the output are in both files.
	for _, want := range []string{
		fmt.Sprintf("exit status %d", exit),
		fmt.Sprintf("%d bytes of tool output observed", observed),
		"went to this run's stderr",
	} {
		if !strings.Contains(testsRaw, want) {
			t.Errorf("%s does not record %q:\n%s", TestsFile, want, testsRaw)
		}
		if !strings.Contains(verifyRaw, want) {
			t.Errorf("%s does not record %q:\n%s", VerifyFile, want, verifyRaw)
		}
	}

	// The output itself is in neither.
	for _, banned := range []string{canaryB, "the head of stryker's output"} {
		if strings.Contains(testsRaw, banned) {
			t.Errorf("%s carries tool output (%q):\n%s", TestsFile, banned, testsRaw)
		}
		if strings.Contains(verifyRaw, banned) {
			t.Errorf("%s carries tool output (%q):\n%s", VerifyFile, banned, verifyRaw)
		}
	}

	// The leg's error survived into verify.toml at all. Without the
	// LanguageRun.Error → LegSummary.Error population, this file records a
	// dead leg with an empty error field — a run that reads as clean.
	v, err := LoadVerify(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Summary.Legs) != 1 {
		t.Fatalf("want one leg in %s, got %+v", VerifyFile, v.Summary.Legs)
	}
	if v.Summary.Legs[0].Error == "" {
		t.Errorf("%s records the leg with an empty error field — a dead leg reads as a clean run", VerifyFile)
	}

	// The pin the renderable_derivation decision rests on: the finding is
	// EXACTLY the prefix plus the recorded leg error, so a composer that
	// interpolated anything else fails here rather than in review.
	tests, err := LoadTests(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests.Languages) != 1 {
		t.Fatalf("want one leg in %s, got %+v", TestsFile, tests.Languages)
	}
	want := "mutation adapter stryker failed: " + tests.Languages[0].Error
	found := 0
	for _, f := range v.Findings {
		if !strings.HasPrefix(f.Text, "mutation adapter stryker failed:") {
			continue
		}
		found++
		if f.Text != want {
			t.Errorf("finding text is\n  %q\nwant exactly\n  %q\n— Finding.Text is Renderable only because it interpolates the recorded field and nothing else", f.Text, want)
		}
		// RemoteTransport is false, so this is a FLAG. Grading it BLOCKING
		// would mark the phase unverifiable on a leg that ran and failed.
		if f.Severity != "FLAG" {
			t.Errorf("the adapter-failure finding is %q, want FLAG — RemoteTransport was never set", f.Severity)
		}
	}
	if found != 1 {
		t.Fatalf("want exactly one adapter-failure finding, got %d: %+v", found, v.Findings)
	}
	if tests.Languages[0].RemoteTransport {
		t.Error("a local reportless failure was classified as a remote transport error")
	}
}

// TestPersistenceAssertionsCanFire is the calibration. The absence checks above
// are only worth anything if the persistence path would carry tool output when
// an adapter puts it in the error — which is exactly what it used to do.
func TestPersistenceAssertionsCanFire(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "reports", "mutation", "mutation.json")

	testsPath, verifyPath := writeRun(t, dir, leakyAdapterError(reportPath))
	testsRaw := readFile(t, testsPath)
	verifyRaw := readFile(t, verifyPath)

	if !strings.Contains(testsRaw, canaryB) {
		t.Errorf("a leaking adapter's output did NOT reach %s — the absence assertions in TestFailedLegReachesDiskClean are vacuous:\n%s", TestsFile, testsRaw)
	}
	if !strings.Contains(verifyRaw, canaryB) {
		t.Errorf("a leaking adapter's output did NOT reach %s — the absence assertions in TestFailedLegReachesDiskClean are vacuous:\n%s", VerifyFile, verifyRaw)
	}
}

// writeRun drives one failing leg through the whole persistence path and returns
// the two files it wrote.
func writeRun(t *testing.T, dir string, legErr error) (testsPath, verifyPath string) {
	t.Helper()

	stry := &fakeAdapter{name: "stryker", supportsExt: []string{".ts"}, err: legErr}
	scope := NewScope(ScopeInput{Root: dir, Git: []string{"src/a.ts"}})

	tests, err := RunScoped("p", []string{"src/a.ts"}, []mutation.Adapter{stry}, scope)
	if err != nil {
		t.Fatalf("a failing adapter must not fail the whole run: %v", err)
	}

	testsPath = filepath.Join(dir, TestsFile)
	if err := tests.Save(testsPath); err != nil {
		t.Fatal(err)
	}
	verifyPath = filepath.Join(dir, VerifyFile)
	if err := Skeleton(tests, []string{"c-1"}).Save(verifyPath); err != nil {
		t.Fatal(err)
	}
	return testsPath, verifyPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
