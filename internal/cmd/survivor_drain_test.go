package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
)

// gremlinsReportWith renders a per-package gremlins report naming file by bare
// basename — the shape the tool actually writes — with one mutant per entry.
func gremlinsReportWith(file string, mutants ...string) string {
	var b strings.Builder
	b.WriteString(`{"go_module":"example.com/m","files":[{"file_name":"` + file + `","mutations":[`)
	b.WriteString(strings.Join(mutants, ","))
	b.WriteString("]}]}")
	return b.String()
}

func mutantJSON(line int, op, status string) string {
	return `{"line":` + itoa(line) + `,"column":1,"type":"` + op + `","status":"` + status + `"}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// drainFixture builds a repo with one phase (alpha, current), a second phase
// (beta) to route to, a source file the identifier can resolve, and a recorded
// gremlins report for ./internal.
//
// The package set is faked so the drain's default scope is deterministic and
// does not depend on what the test binary's own module contains.
func drainFixture(t *testing.T, pkgs []string) string {
	t.Helper()
	dir := realTempDir(t)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	for _, id := range []string{"alpha", "beta"} {
		mustWrite(t, filepath.Join(dir, ".dross", "phases", id, "spec.toml"),
			"[phase]\nid = \""+id+"\"\ntitle = \""+id+"\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")
	}
	if err := runCmd(t, State(), "set", "current_phase", "alpha"); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(dir, "internal", "y.go"), `package internal

func f(limit int) error {
	if limit > 0 {
		return nil
	}
	return nil
}
`)

	orig := goListDirs
	goListDirs = func(repoRoot string) ([]string, error) {
		out := make([]string, 0, len(pkgs))
		for _, p := range pkgs {
			rel := strings.TrimPrefix(p, "./")
			if rel == "." {
				out = append(out, repoRoot)
				continue
			}
			out = append(out, filepath.Join(repoRoot, rel))
		}
		return out, nil
	}
	t.Cleanup(func() { goListDirs = orig })
	return dir
}

// writeRawReport places a per-package raw report where the adapter would.
func writeRawReport(t *testing.T, repoRoot, pkg, body string) {
	t.Helper()
	mustWrite(t, mutation.GremlinsReportPath(repoRoot, pkg), body)
}

// fakeDrainRunner substitutes the adapter run: it reports the given packages as
// unmeasured and otherwise leaves the reports already on disk alone.
func fakeDrainRunner(t *testing.T, unmeasured ...mutation.Unmeasured) {
	t.Helper()
	orig := drainRunner
	drainRunner = func(_ string, _ []string) ([]mutation.Unmeasured, error) {
		return unmeasured, nil
	}
	t.Cleanup(func() { drainRunner = orig })
}

// TestDrainReportsUndisposedSurvivor is c-1 at the command boundary: a survivor
// in neither the store nor any [[deferred]] entry makes the drain fail and
// names it; the same survivor with an acceptance exits 0.
func TestDrainReportsUndisposedSurvivor(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "CONDITIONALS_NEGATION", "LIVED")))
	fakeDrainRunner(t)

	var out string
	err := runCmdCapturing(t, &out, Survivor(), "drain")
	if err == nil {
		t.Fatal("drain with an undisposed survivor exited 0, want an error")
	}
	if !strings.Contains(out, "internal/y.go:4 (CONDITIONALS_NEGATION)") {
		t.Errorf("drain did not name the survivor:\n%s", out)
	}

	// Accept it; now the drain is clean.
	if err := runCmd(t, Survivor(), "accept", "internal/y.go:4",
		"--op", "CONDITIONALS_NEGATION", "--reason", "unreachable — pinned by TestNothing"); err != nil {
		t.Fatal(err)
	}
	out = ""
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err != nil {
		t.Fatalf("drain after acceptance: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 unclassified") {
		t.Errorf("drain should report zero outstanding:\n%s", out)
	}
}

// TestDrainCannotBeClosedByRoutingToItself: routing a survivor to the phase
// being drained is not a disposition — that phase IS the destination. Without
// this, the whole backlog could be marked done by pointing it at the room it is
// already standing in.
func TestDrainCannotBeClosedByRoutingToItself(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "OP", "LIVED")))
	fakeDrainRunner(t)

	// Route it to alpha, which is the current phase — so alpha is what gets
	// drained by default.
	if err := runCmd(t, Survivor(), "route", "internal/y.go:4", "--op", "OP", "--target", "alpha"); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("a survivor routed to the phase being drained was treated as disposed:\n%s", out)
	}
	if !strings.Contains(out, "internal/y.go:4") {
		t.Errorf("the self-routed survivor was not named:\n%s", out)
	}

	// Draining a DIFFERENT phase, the same routing is a real destination.
	out = ""
	if err := runCmdCapturing(t, &out, Survivor(), "drain", "--phase", "beta"); err != nil {
		t.Fatalf("a survivor routed elsewhere must count as routed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "1 routed") {
		t.Errorf("expected the survivor to be reported routed:\n%s", out)
	}
}

// TestDrainCountsDismissedDeferredAsOutstanding: dismissal is triage — "we
// looked at this" — not a disposition. A dismissed routing leaves the survivor
// with no reason and nowhere to go.
func TestDrainCountsDismissedDeferredAsOutstanding(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "OP", "LIVED")))
	fakeDrainRunner(t)

	if err := runCmd(t, Survivor(), "route", "internal/y.go:4", "--op", "OP", "--target", "beta"); err != nil {
		t.Fatal(err)
	}
	// Sanity: routed elsewhere, it is disposed.
	if err := runCmd(t, Survivor(), "drain"); err != nil {
		t.Fatalf("premise broken — a routed survivor should be disposed: %v", err)
	}

	// Mark it dismissed while KEEPING the target. `dross deferred dismiss`
	// refuses a routed entry, so this shape is only reachable by hand — which
	// is exactly why it needs pinning: the spec is a hand-editable file, and a
	// classifier that read target alone would treat this as parked.
	specPath := filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml")
	body, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(body), `target = "beta"`, "target = \"beta\"\n  dismissed = true", 1)
	if edited == string(body) {
		t.Fatalf("could not find the routed entry to dismiss:\n%s", body)
	}
	mustWrite(t, specPath, edited)

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Errorf("a dismissed deferred entry was treated as a disposition:\n%s", out)
	}

	// And the plain unroute-then-dismiss path — what t-20 uses to close the
	// backlog — leaves it outstanding too, for the simpler reason that it now
	// names no destination at all.
	mustWrite(t, specPath, string(body))
	if err := runCmd(t, Deferred(), "unroute", "alpha", "0"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Deferred(), "dismiss", "alpha", "0"); err != nil {
		t.Fatal(err)
	}
	out = ""
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Errorf("an unrouted-then-dismissed entry was treated as a disposition:\n%s", out)
	}
}

// TestDrainFailsOnUnmeasuredPackage: a package the adapter wrote no report for
// tells us nothing. "No survivors found" and "never looked" are byte-identical
// in a count, so absence must be loud — otherwise a never-mutated package is
// indistinguishable from a clean one, which is the state this milestone found
// the repo in.
func TestDrainFailsOnUnmeasuredPackage(t *testing.T) {
	drainFixture(t, []string{"./internal"})
	fakeDrainRunner(t, mutation.Unmeasured{
		Package: "./internal",
		Kind:    mutation.UnmeasuredMissing,
		Message: "./internal (no report — gremlins gathered no covered mutants)",
	})

	var out string
	err := runCmdCapturing(t, &out, Survivor(), "drain")
	if err == nil {
		t.Fatal("an unmeasured package exited 0, want an error")
	}
	if !strings.Contains(err.Error(), "./internal") {
		t.Errorf("the unmeasured package was not named: %v", err)
	}
	if !strings.Contains(err.Error(), "not measured") {
		t.Errorf("err = %v, want it to say the package was not measured", err)
	}
}

// TestZeroCoveredPackageIsMeasured is the other side of that line. A package
// whose report EXISTS but holds only NOT COVERED mutants was measured — the
// adapter looked and found survivors nobody has killed. Those must flow into
// the normal classify path, not be excused as unmeasurable. (This is cmd/dross
// in the real repo: two NOT-COVERED mutants, zero killed.)
func TestZeroCoveredPackageIsMeasured(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go",
		mutantJSON(4, "CONDITIONALS_NEGATION", "NOT COVERED"),
		mutantJSON(6, "OP", "NOT COVERED"),
	))
	fakeDrainRunner(t, mutation.Unmeasured{
		Package: "./internal",
		Kind:    mutation.UnmeasuredUncovered,
		Message: "./internal (zero covered mutants — coverage blind spot)",
	})

	var out string
	err := runCmdCapturing(t, &out, Survivor(), "drain")
	if err == nil {
		t.Fatalf("a zero-coverage package's survivors were excused:\n%s", out)
	}
	if strings.Contains(err.Error(), "not measured") {
		t.Errorf("a zero-coverage package was reported as unmeasured: %v", err)
	}
	if !strings.Contains(out, "internal/y.go:4") {
		t.Errorf("the zero-coverage package's survivors were not classified:\n%s", out)
	}
}

// TestDrainClassifiesFromRawReports pins the wiring. Run()'s merged report
// deliberately drops zero-coverage packages so a blind spot cannot inflate the
// score — that exclusion is scoring-only. A drain reading the merged report
// would see this fixture as empty and exit 0: a vacuous green produced by the
// drain's own plumbing rather than by anything being drained.
func TestDrainClassifiesFromRawReports(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	body := gremlinsReportWith("y.go", mutantJSON(4, "OP", "NOT COVERED"))
	writeRawReport(t, dir, "./internal", body)
	fakeDrainRunner(t)

	// Premise: this is exactly the input that makes Run's MERGED report empty.
	rep, err := mutation.ParseGremlinsJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Killed != 0 || rep.NotCovered != rep.Survived {
		t.Fatalf("fixture premise broken — want an all-NOT-COVERED report, got %+v", rep)
	}

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Errorf("the drain read a merged report and saw nothing to do:\n%s", out)
	}
}

// TestDrainCoversEveryGoPackage: with --packages omitted, the drain's scope is
// every package `go list ./...` reports. A default that quietly covered only
// the current phase's files would make "zero unclassified" a statement about a
// subset while reading as one about the repo.
func TestDrainCoversEveryGoPackage(t *testing.T) {
	want := []string{".", "./internal", "./internal/deep", "./cmd/app"}
	dir := drainFixture(t, want)
	for _, pkg := range want {
		writeRawReport(t, dir, pkg, `{"go_module":"example.com/m","files":[]}`)
	}

	var got []string
	orig := drainRunner
	drainRunner = func(_ string, pkgs []string) ([]mutation.Unmeasured, error) {
		got = append([]string{}, pkgs...)
		return nil, nil
	}
	t.Cleanup(func() { drainRunner = orig })

	if err := runCmd(t, Survivor(), "drain"); err != nil {
		t.Fatalf("drain over empty reports: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("drain dispatched %d packages, want all %d: %v", len(got), len(want), got)
	}
	for _, pkg := range want {
		found := false
		for _, g := range got {
			if g == pkg {
				found = true
			}
		}
		if !found {
			t.Errorf("package %s was not drained: %v", pkg, got)
		}
	}
}

// TestDrainEmptyReportSetExitsZero: nothing to do is not a failure. A drain
// that errored on an empty input could not be used as a standing gate in a repo
// that happens to have no Go survivors.
func TestDrainEmptyReportSetExitsZero(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", `{"go_module":"example.com/m","files":[]}`)
	fakeDrainRunner(t)

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err != nil {
		t.Fatalf("an empty report set errored: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 unclassified") {
		t.Errorf("expected the zero-outstanding sentinel:\n%s", out)
	}
}

// TestDrainReadsRecordedReport pins the --report path, which is how the wave-4
// tasks check one file's worth of survivors without a full repo run.
func TestDrainReadsRecordedReport(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	path := mutation.GremlinsReportPath(dir, "./internal")
	mustWrite(t, path, gremlinsReportWith("y.go", mutantJSON(4, "OP", "LIVED")))

	// No adapter run at all: --report must not shell out.
	orig := drainRunner
	drainRunner = func(_ string, _ []string) ([]mutation.Unmeasured, error) {
		t.Fatal("--report must classify the recorded report without running the adapter")
		return nil, nil
	}
	t.Cleanup(func() { drainRunner = orig })

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain", "--report", path); err == nil {
		t.Fatalf("the recorded report's survivor was not reported:\n%s", out)
	}
	if !strings.Contains(out, "internal/y.go:4") {
		t.Errorf("--report did not resolve the package prefix from the report name:\n%s", out)
	}
}

// TestDrainStoreLoadFailureIsLoud: a store that will not load must stop the
// drain rather than yield an empty acceptance map, which would re-report every
// accepted survivor as outstanding and train the user to ignore the output.
func TestDrainStoreLoadFailureIsLoud(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "OP", "LIVED")))
	fakeDrainRunner(t)

	mustWrite(t, filepath.Join(dir, ".dross", survivor.StoreFile),
		"[[accepted]]\n  key = \"k\"\n  file = \"a.go\"\n  op = \"OP\"\n  text = \"x\"\n  reason = \"\"\n")

	err := runCmd(t, Survivor(), "drain")
	if err == nil {
		t.Fatal("a corrupt store did not stop the drain")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("err = %v, want the store's own validation error", err)
	}
}

// TestDrainUnknownPackageArgIsAnError: a typo'd --packages value must not
// quietly drain nothing and exit 0 — a green produced by a misspelling is the
// worst possible output for a gate.
func TestDrainUnknownPackageArgIsAnError(t *testing.T) {
	drainFixture(t, []string{"./internal"})
	fakeDrainRunner(t)

	err := runCmd(t, Survivor(), "drain", "--packages", "./nope")
	if err == nil {
		t.Fatal("an unmatched package argument exited 0")
	}
	if !strings.Contains(err.Error(), "./nope") {
		t.Errorf("err = %v, want it to name the unmatched argument", err)
	}
}

// TestDrainPackageArgExpandsRecursively: `./internal/...` means the subtree, the
// way every other Go tool reads it.
func TestDrainPackageArgExpandsRecursively(t *testing.T) {
	dir := drainFixture(t, []string{".", "./internal", "./internal/deep", "./cmd/app"})
	for _, pkg := range []string{".", "./internal", "./internal/deep", "./cmd/app"} {
		writeRawReport(t, dir, pkg, `{"go_module":"example.com/m","files":[]}`)
	}

	var got []string
	orig := drainRunner
	drainRunner = func(_ string, pkgs []string) ([]mutation.Unmeasured, error) {
		got = append([]string{}, pkgs...)
		return nil, nil
	}
	t.Cleanup(func() { drainRunner = orig })

	if err := runCmd(t, Survivor(), "drain", "--packages", "./internal/..."); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"./internal": true, "./internal/deep": true}
	if len(got) != len(want) {
		t.Fatalf("dispatched %v, want exactly %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("recursive arg pulled in an unrelated package: %s", g)
		}
	}
}

// TestGremlinsReportPathMatchesWhatRunWrites is the drift guard on the seam the
// drain depends on: it reads raw reports by deriving their path, so if the
// adapter ever writes them somewhere else the drain would silently find
// nothing and report a clean repo.
func TestGremlinsReportPathMatchesWhatRunWrites(t *testing.T) {
	root := t.TempDir()
	for _, pkg := range []string{".", "./internal/cmd"} {
		path := mutation.GremlinsReportPath(root, pkg)
		if filepath.Dir(path) != filepath.Join(root, "reports", "gremlins") {
			t.Errorf("report for %s lands outside reports/gremlins: %s", pkg, path)
		}
		if !strings.HasSuffix(path, ".json") {
			t.Errorf("report path for %s is not a .json file: %s", pkg, path)
		}
	}
	if a, b := mutation.GremlinsReportPath(root, "."), mutation.GremlinsReportPath(root, "./internal/cmd"); a == b {
		t.Error("two packages resolve to the same report file — one would clobber the other")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

// TestDrainExcludesTestdataFixtures: gremlins walks the package DIRECTORY, not
// the Go package, so a fixture under testdata/ lands in its parent's report and
// is always NOT COVERED there — the parent's tests never run it. Go excludes
// testdata from `./...` by construction and the code never reaches the binary,
// so counting it would make every test fixture permanent debt requiring a
// written reason.
//
// This is a scope rule, and it is deliberately narrow: only a path with a
// literal `testdata` SEGMENT is excluded, so a real package that merely has
// "testdata" inside a longer directory name is still drained.
func TestDrainExcludesTestdataFixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"internal/mutation/testdata/ceiling/ceiling.go", true},
		{"testdata/x.go", true},
		{"internal/testdata/deep/nested/y.go", true},
		{"internal/cmd/doctor.go", false},
		{"internal/testdatabase/store.go", false},
		{"internal/mytestdata.go", false},
	} {
		if got := isTestdataPath(tc.file); got != tc.want {
			t.Errorf("isTestdataPath(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
}

// TestDrainDropsTestdataSurvivorsEndToEnd drives the exclusion through the
// command, so a report carrying both a fixture survivor and a real one reports
// exactly the real one.
func TestDrainDropsTestdataSurvivorsEndToEnd(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	mustWrite(t, mutation.GremlinsReportPath(dir, "./internal"),
		`{"go_module":"example.com/m","files":[`+
			`{"file_name":"y.go","mutations":[`+mutantJSON(4, "OP", "LIVED")+`]},`+
			`{"file_name":"testdata/fixture/f.go","mutations":[`+mutantJSON(9, "OP", "NOT COVERED")+`]}`+
			`]}`)
	fakeDrainRunner(t)

	var out string
	err := runCmdCapturing(t, &out, Survivor(), "drain")
	if err == nil {
		t.Fatalf("the real survivor was not reported:\n%s", out)
	}
	if strings.Contains(out, "testdata") {
		t.Errorf("a testdata fixture was counted as standing debt:\n%s", out)
	}
	if !strings.Contains(out, "1 outstanding") {
		t.Errorf("expected exactly one outstanding survivor:\n%s", out)
	}
}
