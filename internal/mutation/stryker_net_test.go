package mutation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestStrykerNetParsesSchemaSharedWithJS confirms that the Stryker.NET
// adapter routes its JSON through ParseStrykerJSON and applies the
// .NET-specific tool name on the result. The schema (mutation-testing-
// report-schema) is identical across Stryker.JS and Stryker.NET; this
// test pins that assumption.
func TestStrykerNetParsesSchemaSharedWithJS(t *testing.T) {
	// Synthetic report following the public schema with C# source data.
	report := map[string]any{
		"schemaVersion": "1",
		"files": map[string]any{
			"src/Calculator.cs": map[string]any{
				"language": "csharp",
				"source":   "public class Calculator { public int Add(int a, int b) => a + b; }",
				"mutants": []map[string]any{
					{
						"id":          "1",
						"mutatorName": "BinaryOperator",
						"replacement": "a - b",
						"status":      "Killed",
						"location":    map[string]any{"start": map[string]int{"line": 1, "column": 1}, "end": map[string]int{"line": 1, "column": 80}},
					},
					{
						"id":          "2",
						"mutatorName": "BooleanLiteral",
						"replacement": "false",
						"status":      "Survived",
						"location":    map[string]any{"start": map[string]int{"line": 5, "column": 1}, "end": map[string]int{"line": 5, "column": 10}},
					},
				},
			},
		},
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	r, err := ParseStrykerJSON(b)
	if err != nil {
		t.Fatalf("parse shared schema: %v", err)
	}
	if r.Killed != 1 || r.Survived != 1 {
		t.Errorf("counts wrong: killed=%d survived=%d", r.Killed, r.Survived)
	}
	if len(r.Surviving) != 1 || r.Surviving[0].Op != "BooleanLiteral" {
		t.Errorf("surviving mutant lost: %+v", r.Surviving)
	}
}

// TestStrykerNetFindReportPicksMostRecent ensures the discovery logic
// handles Stryker.NET's varying output layouts (timestamped subdirs in
// older versions, flat layout in newer ones) and prefers the freshest
// report on a re-run.
func TestStrykerNetFindReportPicksMostRecent(t *testing.T) {
	out := t.TempDir()

	// Older-style timestamped layout — write a stale report.
	stalePath := filepath.Join(out, "2024-01-01T00-00-00", "reports", "mutation-report.json")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, []byte(`{"schemaVersion":"1","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	// Newer flat layout — fresh report.
	freshPath := filepath.Join(out, "reports", "mutation-report.json")
	if err := os.MkdirAll(filepath.Dir(freshPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte(`{"schemaVersion":"1","files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := findReport(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != freshPath {
		t.Errorf("findReport picked %q, want %q (most recent)", got, freshPath)
	}
}

func TestStrykerNetFindReportErrsWhenMissing(t *testing.T) {
	out := t.TempDir()
	_, err := findReport(out)
	if err == nil || !strings.Contains(err.Error(), "no mutation-report.json") {
		t.Errorf("expected missing-report error, got: %v", err)
	}
}

func TestStrykerNetSupports(t *testing.T) {
	s := &StrykerNet{}
	for _, ok := range []string{"Foo.cs", "src/Bar.cs", "deep/path/Baz.CS"} {
		if !s.Supports(ok) {
			t.Errorf("should support %q", ok)
		}
	}
	for _, no := range []string{"foo.ts", "foo.go", "foo.cshtml", "foo.fs"} {
		if s.Supports(no) {
			t.Errorf("should not support %q", no)
		}
	}
}

func TestStrykerNetRunReturnsEmptyForNoFiles(t *testing.T) {
	s := &StrykerNet{ProjectRoot: t.TempDir()}
	r, err := s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "stryker-net" || r.Killed != 0 || r.Survived != 0 {
		t.Errorf("expected empty report, got %+v", r)
	}
}

func TestStrykerNetOutputDirDefaults(t *testing.T) {
	s := &StrykerNet{ProjectRoot: "/tmp/proj"}
	if got := s.outputDir(); got != "/tmp/proj/StrykerOutput" {
		t.Errorf("default output dir wrong: %q", got)
	}
	s.OutputDir = "custom"
	if got := s.outputDir(); got != "/tmp/proj/custom" {
		t.Errorf("relative custom output dir wrong: %q", got)
	}
	s.OutputDir = "/abs/output"
	if got := s.outputDir(); got != "/abs/output" {
		t.Errorf("absolute output dir should pass through: %q", got)
	}
}

// --- repo-relative paths ----------------------------------------------------

// fakeStrykerNet swaps the process seam for one that drops payload at
// <outDir>/reports/mutation-report.json — where findReport looks — instead of
// running `dotnet stryker`. Returns the restore func.
func fakeStrykerNet(t *testing.T, outDir string, payload []byte) func() {
	t.Helper()
	orig := strykerNetBuildCmd
	strykerNetBuildCmd = func(_ *StrykerNet, _ []string) *exec.Cmd {
		dir := filepath.Join(outDir, "reports")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("prepare fake output dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mutation-report.json"), payload, 0o644); err != nil {
			t.Fatalf("write fake stryker.net report: %v", err)
		}
		return exec.Command("true")
	}
	return func() { strykerNetBuildCmd = orig }
}

// TestStrykerNetRunMakesPathsRepoRelative is the .NET half of the attribution
// contract, and the one shape in this package that had to be settled against
// the tool rather than assumed.
//
// Stryker.NET keys `files` by each component's ABSOLUTE FullPath and writes
// the mutated tree's root alongside as `projectRoot` — see JsonReport.cs in
// stryker-mutator/stryker-net, which builds the dictionary from
// `fileComponent.FullPath` and sets `ProjectRoot = mutationReport.FullPath`.
// testdata/strykernet_report.json carries that shape (schemaVersion 2,
// language "cs", the real MutantStatus spellings).
//
// Left alone, not one of those absolute keys matches a repo-relative change
// set: every C# mutant filters out as out-of-scope and the leg reports 0/0 —
// an entire language exempt from the gate while looking measured.
func TestStrykerNetRunMakesPathsRepoRelative(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "strykernet_report.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Guard the premise: a fixture quietly replaced with relative keys would
	// make this test pass without exercising anything.
	if !strings.Contains(string(payload), `"/home/dev/repo/src/Calculator/Calculator.cs"`) {
		t.Fatalf("fixture must carry absolute FullPath keys:\n%s", payload)
	}

	out := t.TempDir()
	defer fakeStrykerNet(t, out, payload)()

	s := &StrykerNet{ProjectRoot: "/home/dev/repo", OutputDir: out}
	rep, err := s.Run([]string{"src/Calculator/Calculator.cs"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantFiles := map[string]FileStat{
		"src/Calculator/Calculator.cs": {Killed: 1, Survived: 1},
		"src/Calculator/Formatter.cs":  {Survived: 1},
	}
	if !reflect.DeepEqual(rep.Files, wantFiles) {
		t.Errorf("per-file keys:\n got %+v\nwant %+v", rep.Files, wantFiles)
	}
	var survivingPaths []string
	for _, m := range rep.Surviving {
		survivingPaths = append(survivingPaths, m.File)
	}
	sort.Strings(survivingPaths)
	wantSurviving := []string{"src/Calculator/Calculator.cs", "src/Calculator/Formatter.cs"}
	if !reflect.DeepEqual(survivingPaths, wantSurviving) {
		t.Errorf("surviving paths:\n got %v\nwant %v", survivingPaths, wantSurviving)
	}
	// Both surfaces must agree — a row key the scope matches whose Surviving
	// entry it doesn't (or the reverse) is worse than either alone.
	for _, p := range survivingPaths {
		if _, ok := rep.Files[p]; !ok {
			t.Errorf("surviving path %q has no matching per-file row", p)
		}
	}
	assertPerFileMatchesAggregate(t, rep)
}

// TestStrykerNetRePrefixParityWithStryker: when the absolute paths belong to a
// tree dross cannot locate — an older Stryker.NET writing project-relative
// paths, or a container whose filesystem is not the host's — Workdir does the
// placing, and it must produce exactly what Stryker.rePrefixFiles produces for
// its own Workdir. t-4's adapter-agnostic filtering depends on the two legs
// speaking the same path shape.
func TestStrykerNetRePrefixParityWithStryker(t *testing.T) {
	const rel = "Calculator/Calculator.cs"

	net := &StrykerNet{Workdir: "src"}
	netRep := &Report{
		Surviving: []Mutant{{File: rel, Line: 9}},
		Files:     map[string]FileStat{rel: {Survived: 1}},
	}
	net.rePrefixFiles(netRep, "")

	js := &Stryker{Workdir: "src"}
	jsRep := &Report{
		Surviving: []Mutant{{File: rel, Line: 9}},
		Files:     map[string]FileStat{rel: {Survived: 1}},
	}
	js.rePrefixFiles(jsRep)

	if netRep.Surviving[0].File != jsRep.Surviving[0].File {
		t.Errorf("surviving path diverges: net=%q js=%q",
			netRep.Surviving[0].File, jsRep.Surviving[0].File)
	}
	if !reflect.DeepEqual(netRep.Files, jsRep.Files) {
		t.Errorf("per-file keys diverge:\n net %+v\n js  %+v", netRep.Files, jsRep.Files)
	}
	if _, ok := netRep.Files["src/"+rel]; !ok {
		t.Errorf("expected the workdir prefix to be applied: %+v", netRep.Files)
	}
}

// TestStrykerNetRePrefixNoRegression: a report already speaking repo-relative
// paths passes through untouched. Double-prefixing would be the same bug in
// the opposite direction — src/src/Foo.cs matches nothing either.
func TestStrykerNetRePrefixNoRegression(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    *StrykerNet
		root string
	}{
		{"no workdir configured", &StrykerNet{}, ""},
		{"workdir already present in the path", &StrykerNet{Workdir: "src"}, ""},
		{"projectRoot does not match", &StrykerNet{Workdir: "src"}, "/elsewhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Report{
				Surviving: []Mutant{{File: "src/Calculator/Calculator.cs"}},
				Files:     map[string]FileStat{"src/Calculator/Calculator.cs": {Survived: 1}},
			}
			tc.s.rePrefixFiles(r, tc.root)
			if got := r.Surviving[0].File; got != "src/Calculator/Calculator.cs" {
				t.Errorf("surviving path changed: %q", got)
			}
			if _, ok := r.Files["src/Calculator/Calculator.cs"]; !ok {
				t.Errorf("per-file key changed: %+v", r.Files)
			}
		})
	}
}

// TestStrykerNetLeavesUnlocatableAbsolutePath: a path under neither root stays
// absolute rather than having the workdir glued to the front of it. It will be
// reported as out-of-scope, which is honest; a fabricated relative path could
// silently match the wrong file.
func TestStrykerNetLeavesUnlocatableAbsolutePath(t *testing.T) {
	s := &StrykerNet{ProjectRoot: "/home/dev/repo", Workdir: "src"}
	r := &Report{Files: map[string]FileStat{"/opt/other/Foo.cs": {Survived: 1}}}
	s.rePrefixFiles(r, "/home/dev/repo/src")
	if _, ok := r.Files["/opt/other/Foo.cs"]; !ok {
		t.Errorf("unlocatable path must be left alone: %+v", r.Files)
	}
}

// TestStrykerNetWindowsPathsFold: a report produced on Windows carries
// backslashes; the scope speaks slashes, so they must fold on the way in.
func TestStrykerNetWindowsPathsFold(t *testing.T) {
	s := &StrykerNet{ProjectRoot: `C:\dev\repo`}
	r := &Report{
		Surviving: []Mutant{{File: `C:\dev\repo\src\Foo.cs`}},
		Files:     map[string]FileStat{`C:\dev\repo\src\Foo.cs`: {Survived: 1}},
	}
	s.rePrefixFiles(r, "")
	if got := r.Surviving[0].File; got != "src/Foo.cs" {
		t.Errorf("surviving path = %q want src/Foo.cs", got)
	}
	if _, ok := r.Files["src/Foo.cs"]; !ok {
		t.Errorf("per-file key = %+v want src/Foo.cs", r.Files)
	}
}

// TestStrykerNetBuildCmdPrefix pins both arms of the runtime-prefix seam. With
// no prefix the argv must be exactly what the caller assembled; with one, the
// prefix's words come first and the original args follow in order. Inverting
// the guard swaps the two behaviours: a native run would try to exec the first
// word of an empty prefix, and a docker run would exec the tool on the host —
// bypassing the container the project runs everything else in.
func TestStrykerNetBuildCmdPrefix(t *testing.T) {
	args := []string{"dotnet", "stryker", "--output", "out"}

	t.Run("no prefix runs the args verbatim", func(t *testing.T) {
		cmd := strykerNetBuildCmd(&StrykerNet{}, args)
		if !reflect.DeepEqual(cmd.Args, args) {
			t.Errorf("Args = %v, want exactly the args passed in %v", cmd.Args, args)
		}
	})

	t.Run("prefix is prepended word by word", func(t *testing.T) {
		cmd := strykerNetBuildCmd(&StrykerNet{Prefix: "docker run"}, args)
		want := append([]string{"docker", "run"}, args...)
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Errorf("Args = %v, want %v", cmd.Args, want)
		}
		if filepath.Base(cmd.Path) != "docker" {
			t.Errorf("Path = %q, want the prefix's first word to be the executable", cmd.Path)
		}
	})
}
