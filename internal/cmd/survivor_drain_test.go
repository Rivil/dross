package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
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
//
// The rule is asserted through verify.IsTestdataPath because that is the one
// the drain now calls — `dross verify` classifies the same repo state with the
// same function, and a local copy here is what let the two disagree.
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
		if got := verify.IsTestdataPath(tc.file); got != tc.want {
			t.Errorf("IsTestdataPath(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
}

// TestDrainHasNoLocalTestdataRule fails if a second copy of the scope rule
// reappears in internal/cmd. The defect this task fixes was not a wrong
// predicate — both copies agreed — it was that only one of the two commands
// applied one at all, and a private copy here is how that divergence hides.
// Sharing verify.IsTestdataPath is the fix; this test is what keeps it shared.
func TestDrainHasNoLocalTestdataRule(t *testing.T) {
	b, err := os.ReadFile("survivor_drain.go")
	if err != nil {
		t.Fatalf("read survivor_drain.go: %v", err)
	}
	if strings.Contains(string(b), "func isTestdataPath(") {
		t.Error("survivor_drain.go redefines the testdata scope rule locally; " +
			"call verify.IsTestdataPath so the drain and `dross verify` cannot diverge")
	}
	if !strings.Contains(string(b), "verify.IsTestdataPath(") {
		t.Error("survivor_drain.go no longer calls verify.IsTestdataPath — " +
			"testdata fixtures will be drained as standing debt")
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

// fakeCoverage substitutes the coverage seam with a profile built from the
// given file:line → count table.
func fakeCoverage(t *testing.T, rel string, lines map[int]int) {
	t.Helper()
	body := "mode: set\n"
	for line, count := range lines {
		body += "example.com/m/" + rel + ":" + itoa(line) + ".1," + itoa(line) + ".2 1 " + itoa(count) + "\n"
	}
	path := filepath.Join(t.TempDir(), "cover.out")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := survivor.ParseProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	orig := coverageProfileFn
	coverageProfileFn = func(string, []string) *survivor.Profile { return prof }
	t.Cleanup(func() { coverageProfileFn = orig })
}

// TestDrainAttachesEvidenceToEachOutstanding: the drain answers kill-vs-accept
// per survivor from code, rather than leaving ~91 near-identical judgement
// calls to be made by hand. The covered-and-applicable case must say so
// explicitly — that is the one an acceptance may never claim.
func TestDrainAttachesEvidenceToEachOutstanding(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "CONDITIONALS_NEGATION", "LIVED")))
	fakeDrainRunner(t)
	fakeCoverage(t, "internal/y.go", map[int]int{4: 7})

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("expected the survivor to be outstanding:\n%s", out)
	}
	for _, want := range []string{
		"coverage=covered",
		"applicable=applicable",
		"do not accept it",
		"covered AND operator-applicable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("drain output missing %q:\n%s", want, out)
		}
	}
}

// TestDrainEvidenceUnknownWithoutProfile: with no coverage profile the drain
// must say unknown, not "not covered". An unknown that read as uncovered would
// be evidence FOR an acceptance produced by the absence of evidence.
func TestDrainEvidenceUnknownWithoutProfile(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go", mutantJSON(4, "OP", "LIVED")))
	fakeDrainRunner(t)

	orig := coverageProfileFn
	coverageProfileFn = func(string, []string) *survivor.Profile { return nil }
	t.Cleanup(func() { coverageProfileFn = orig })

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("expected the survivor to be outstanding:\n%s", out)
	}
	if !strings.Contains(out, "coverage=unknown") {
		t.Errorf("a missing profile must report unknown:\n%s", out)
	}
	if strings.Contains(out, "coverage=not covered") {
		t.Errorf("a missing profile was reported as uncovered:\n%s", out)
	}
	if strings.Contains(out, "covered AND operator-applicable") {
		t.Errorf("an unknown-coverage survivor was called killable:\n%s", out)
	}
}

// TestDrainCeilingIsPerMutantNotPerFile: ceiling eligibility turns on whether
// the tool called THIS mutant NOT COVERED. A file-granular approximation would
// let one uncovered mutant grant the ceiling to every other survivor in its
// file — which is exactly how a killable survivor gets accepted.
func TestDrainCeilingIsPerMutantNotPerFile(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	// Same file, same coverage: one mutant the tool called NOT COVERED (the
	// ceiling shape) and one it called LIVED (a genuine, killable escape).
	writeRawReport(t, dir, "./internal", gremlinsReportWith("y.go",
		mutantJSON(4, "CONDITIONALS_NEGATION", "NOT COVERED"),
		mutantJSON(6, "CONDITIONALS_NEGATION", "LIVED"),
	))
	fakeDrainRunner(t)
	fakeCoverage(t, "internal/y.go", map[int]int{4: 3, 6: 3})

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("expected outstanding survivors:\n%s", out)
	}

	lines := strings.Split(out, "\n")
	evidenceFor := func(loc string) string {
		for i, l := range lines {
			if strings.Contains(l, loc) && i+1 < len(lines) {
				return lines[i+1]
			}
		}
		t.Fatalf("no evidence line for %s:\n%s", loc, out)
		return ""
	}

	if got := evidenceFor("internal/y.go:4"); !strings.Contains(got, "ceiling-eligible=yes") {
		t.Errorf("the NOT COVERED mutant should be ceiling-eligible: %q", got)
	}
	if got := evidenceFor("internal/y.go:6"); strings.Contains(got, "ceiling-eligible=yes") {
		t.Errorf("the LIVED mutant inherited its neighbour's ceiling: %q", got)
	}
	if got := evidenceFor("internal/y.go:6"); !strings.Contains(got, "do not accept it") {
		t.Errorf("the LIVED mutant is covered and applicable, so it is killable: %q", got)
	}
	// And exactly one of the two is counted killable.
	if !strings.Contains(out, "1 of these are covered AND operator-applicable") {
		t.Errorf("expected exactly one killable survivor:\n%s", out)
	}
}

// TestRealGoListDirsSurfacesFailures covers the REAL package-discovery closure.
// Every other drain test substitutes goListDirs wholesale, so the function that
// actually shells out to the toolchain had no coverage: a broken `go list` would
// have surfaced as an empty package set — a drain that reports nothing
// outstanding because it looked at nothing.
func TestRealGoListDirsSurfacesFailures(t *testing.T) {
	t.Run("a directory with no Go module is an error", func(t *testing.T) {
		// Not a module: `go list ./...` exits non-zero.
		_, err := goListDirs(t.TempDir())
		if err == nil {
			t.Fatal("goListDirs over a non-module directory returned no error")
		}
		if !strings.Contains(err.Error(), "go list") {
			t.Errorf("err = %q, want the go list context", err)
		}
	})

	t.Run("this repo lists its packages", func(t *testing.T) {
		dirs, err := goListDirs(repoRootFromTest(t))
		if err != nil {
			t.Fatalf("goListDirs over the real repo: %v", err)
		}
		if len(dirs) < 20 {
			t.Fatalf("listed %d package dirs, want the whole repo — the blank-line filter or the split is wrong", len(dirs))
		}
		for _, d := range dirs {
			if strings.TrimSpace(d) == "" {
				t.Error("a blank line survived into the package list")
			}
		}
	})
}

// TestRunGremlinsOverPackagesRequiresAProject covers the real adapter-dispatch
// helper, which drainRunner replaces in every other test. Its first act is to
// load project.toml for the runtime prefix and timeout coefficient; without one
// there is no configuration to run under, and continuing would invoke gremlins
// with silent defaults that do not match the repo.
func TestRunGremlinsOverPackagesRequiresAProject(t *testing.T) {
	_, err := runGremlinsOverPackages(t.TempDir(), []string{"./internal"})
	if err == nil {
		t.Fatal("runGremlinsOverPackages with no project.toml returned no error")
	}
}

// TestRunGremlinsOverPackagesDispatchesEveryPackage covers the rest of the real
// helper: the module-root special case and the adapter invocation.
//
// Gremlins derives its package set from the DIRECTORIES of the files it is
// handed, so the helper synthesises one representative path per package — and
// the module root has to become "" rather than ".", or the derived path is
// "./drain.go" and the root package is silently dropped from the run.
func TestRunGremlinsOverPackagesDispatchesEveryPackage(t *testing.T) {
	dir := drainFixture(t, []string{".", "./internal"})

	// A repo with a project.toml but no Go module: gremlins is invoked and
	// fails fast, which is the arm that proves the error is surfaced rather
	// than swallowed into an empty "nothing unmeasured" result.
	_, err := runGremlinsOverPackages(dir, []string{".", "./internal"})
	if err == nil {
		t.Skip("gremlins is installed and succeeded over the fixture — the failure arm is unreachable here")
	}
	if !strings.Contains(err.Error(), "gremlins") {
		t.Errorf("err = %q, want the adapter's own failure", err)
	}
}

// TestDrainSortsOutstandingDeterministically covers the outstanding comparator.
// sort.Slice never calls a comparator with fewer than two elements, and every
// other drain test has one outstanding survivor — so the ordering the user
// reads had never been exercised. An unstable order makes the same repo print a
// different list every run, which is the fastest way to make a gate's output
// unreadable.
func TestDrainSortsOutstandingDeterministically(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	// Deliberately out of order, and spanning both comparator branches: two
	// files, and two lines within one of them, plus two ops on one line.
	mustWrite(t, mutation.GremlinsReportPath(dir, "./internal"),
		`{"go_module":"example.com/m","files":[`+
			`{"file_name":"z.go","mutations":[`+mutantJSON(9, "OP", "LIVED")+`]},`+
			`{"file_name":"y.go","mutations":[`+
			mutantJSON(20, "OP", "LIVED")+`,`+
			mutantJSON(4, "ZOP", "LIVED")+`,`+
			mutantJSON(4, "AOP", "LIVED")+
			`]}]}`)
	fakeDrainRunner(t)

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("expected outstanding survivors:\n%s", out)
	}

	var got []string
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "  internal/") {
			continue
		}
		// Drop any note suffix: this test is about ORDER, and a fixture line
		// that does not exist on disk legitimately carries one.
		head, _, _ := strings.Cut(strings.TrimSpace(l), " — ")
		got = append(got, head)
	}
	want := []string{
		"internal/y.go:4 (AOP)",
		"internal/y.go:4 (ZOP)",
		"internal/y.go:20 (OP)",
		"internal/z.go:9 (OP)",
	}
	if len(got) != len(want) {
		t.Fatalf("printed %d outstanding lines, want %d:\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("outstanding order:\n got %v\nwant %v", got, want)
		}
	}
}

// TestDrainPrintsTheSurvivorNote covers the conditional note suffix. A survivor
// whose identity will not resolve carries the reason why, and that reason is
// the only thing distinguishing "you have not decided about this" from "this
// could not even be looked up" — the two need different actions from the user.
func TestDrainPrintsTheSurvivorNote(t *testing.T) {
	dir := drainFixture(t, []string{"./internal"})
	// gone.go is not on disk, so identity resolution fails and the classifier
	// attaches its error as the note.
	mustWrite(t, mutation.GremlinsReportPath(dir, "./internal"),
		`{"go_module":"example.com/m","files":[`+
			`{"file_name":"gone.go","mutations":[`+mutantJSON(3, "OP", "LIVED")+`]},`+
			`{"file_name":"y.go","mutations":[`+mutantJSON(4, "OP", "LIVED")+`]}]}`)
	fakeDrainRunner(t)

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "drain"); err == nil {
		t.Fatalf("expected outstanding survivors:\n%s", out)
	}

	var noted, plain string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, "internal/gone.go:3"):
			noted = l
		case strings.Contains(l, "internal/y.go:4"):
			plain = l
		}
	}
	if !strings.Contains(noted, " — ") || !strings.Contains(noted, "identity") {
		t.Errorf("the unresolvable survivor printed no note: %q", noted)
	}
	// And the note is genuinely conditional: a resolvable survivor renders no
	// dangling separator.
	if strings.Contains(plain, " — ") {
		t.Errorf("a note-less survivor rendered a separator with nothing after it: %q", plain)
	}
}
