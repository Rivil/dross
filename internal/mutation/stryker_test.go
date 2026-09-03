package mutation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// realistic Stryker output: 1 file, 5 mutants — 3 killed, 1 survived,
// 1 timeout. NoCoverage rolls up into survived (the test never even ran it).
const fixtureSimple = `{
  "schemaVersion": "1",
  "thresholds": {"high": 80, "low": 60, "break": null},
  "files": {
    "src/api/tags.ts": {
      "language": "typescript",
      "source": "export function ...",
      "mutants": [
        {"id":"1","mutatorName":"ConditionalExpression","replacement":"true","status":"Killed",
         "location":{"start":{"line":12,"column":4},"end":{"line":12,"column":12}}},
        {"id":"2","mutatorName":"ConditionalExpression","replacement":"false","status":"Killed",
         "location":{"start":{"line":12,"column":4},"end":{"line":12,"column":12}}},
        {"id":"3","mutatorName":"ConditionalExpression","replacement":"true","status":"Survived",
         "location":{"start":{"line":42,"column":10},"end":{"line":42,"column":20}}},
        {"id":"4","mutatorName":"BlockStatement","replacement":"{}","status":"Timeout",
         "location":{"start":{"line":67,"column":4},"end":{"line":75,"column":1}}},
        {"id":"5","mutatorName":"ArithmeticOperator","replacement":"-","status":"Killed",
         "location":{"start":{"line":80,"column":12},"end":{"line":80,"column":13}}}
      ]
    }
  }
}`

func TestParseStrykerJSONCounts(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(fixtureSimple))
	if err != nil {
		t.Fatal(err)
	}
	if r.Tool != "stryker" {
		t.Errorf("tool: %q", r.Tool)
	}
	if r.Killed != 3 {
		t.Errorf("killed: %d want 3", r.Killed)
	}
	if r.Survived != 1 {
		t.Errorf("survived: %d want 1", r.Survived)
	}
	if r.Timeout != 1 {
		t.Errorf("timeout: %d want 1", r.Timeout)
	}
	if r.Errors != 0 {
		t.Errorf("errors: %d want 0", r.Errors)
	}
	// score = killed / (killed + survived + timeout) = 3 / 5 = 0.6
	if math.Abs(r.Score-0.6) > 1e-9 {
		t.Errorf("score: %v want 0.6", r.Score)
	}
}

func TestParseStrykerJSONSurvivingMutants(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(fixtureSimple))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Surviving) != 1 {
		t.Fatalf("expected 1 surviving, got %d", len(r.Surviving))
	}
	m := r.Surviving[0]
	if m.File != "src/api/tags.ts" {
		t.Errorf("file: %q", m.File)
	}
	if m.Line != 42 {
		t.Errorf("line: %d", m.Line)
	}
	if m.Op != "ConditionalExpression" {
		t.Errorf("op: %q", m.Op)
	}
	if m.Snippet != "true" {
		t.Errorf("snippet: %q", m.Snippet)
	}
}

const fixtureNoCoverage = `{
  "schemaVersion": "1",
  "files": {
    "src/auth.ts": {
      "language": "typescript",
      "source": "...",
      "mutants": [
        {"id":"1","mutatorName":"X","replacement":"...","status":"NoCoverage",
         "location":{"start":{"line":1,"column":1},"end":{"line":1,"column":2}}}
      ]
    }
  }
}`

func TestParseStrykerJSONNoCoverageRollsUpAsSurvived(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(fixtureNoCoverage))
	if err != nil {
		t.Fatal(err)
	}
	if r.Survived != 1 {
		t.Errorf("NoCoverage should count as survived; got survived=%d", r.Survived)
	}
	if len(r.Surviving) != 1 || r.Surviving[0].File != "src/auth.ts" {
		t.Errorf("NoCoverage mutant should be in Surviving list: %+v", r.Surviving)
	}
}

const fixtureErrorsAndIgnored = `{
  "schemaVersion": "1",
  "files": {
    "src/x.ts": {
      "language": "typescript",
      "source": "...",
      "mutants": [
        {"id":"1","mutatorName":"X","replacement":"...","status":"RuntimeError",
         "location":{"start":{"line":1,"column":1},"end":{"line":1,"column":2}}},
        {"id":"2","mutatorName":"X","replacement":"...","status":"CompileError",
         "location":{"start":{"line":2,"column":1},"end":{"line":2,"column":2}}},
        {"id":"3","mutatorName":"X","replacement":"...","status":"Ignored",
         "location":{"start":{"line":3,"column":1},"end":{"line":3,"column":2}}},
        {"id":"4","mutatorName":"X","replacement":"...","status":"Pending",
         "location":{"start":{"line":4,"column":1},"end":{"line":4,"column":2}}}
      ]
    }
  }
}`

func TestParseStrykerJSONErrorClassification(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(fixtureErrorsAndIgnored))
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors != 2 {
		t.Errorf("Runtime+Compile errors should count as errors=2; got %d", r.Errors)
	}
	if r.Killed+r.Survived+r.Timeout != 0 {
		t.Errorf("Ignored/Pending mutants leaked into counts: k=%d s=%d t=%d",
			r.Killed, r.Survived, r.Timeout)
	}
	// Score is 0/0 → guarded to remain 0
	if r.Score != 0 {
		t.Errorf("score with no scoring mutants should be 0; got %v", r.Score)
	}
}

// TestParseStrykerJSONPerFileAttribution pins the same per-file contract the
// gremlins parser owes, on the file-granular adapter: every status lands in a
// row keyed by the report's own path, and the aggregates are untouched.
func TestParseStrykerJSONPerFileAttribution(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(fixtureSimple))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FileStat{"src/api/tags.ts": {Killed: 3, Survived: 1, Timeout: 1}}
	if !reflect.DeepEqual(r.Files, want) {
		t.Errorf("per-file rows:\n got %+v\nwant %+v", r.Files, want)
	}
	if r.Killed != 3 || r.Survived != 1 || r.Timeout != 1 {
		t.Errorf("aggregates changed: killed=%d survived=%d timeout=%d", r.Killed, r.Survived, r.Timeout)
	}
	assertPerFileMatchesAggregate(t, r)
}

// TestParseStrykerJSONPerFileNoDrift runs the drift invariant over the other
// fixtures — NoCoverage (survived) and the error/ignored classification,
// where Ignored and Pending must contribute to neither a total nor a row.
func TestParseStrykerJSONPerFileNoDrift(t *testing.T) {
	for name, payload := range map[string]string{
		"no coverage":        fixtureNoCoverage,
		"errors and ignored": fixtureErrorsAndIgnored,
	} {
		t.Run(name, func(t *testing.T) {
			r, err := ParseStrykerJSON([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			assertPerFileMatchesAggregate(t, r)
		})
	}
}

// TestStrykerRePrefixMovesPerFileKeys: the workdir prefix must be applied to
// the per-file rows as well as to Surviving. If the two disagree, diff scoping
// scores a mutant out (row key unmatched) while still reporting it as a
// survivor (Surviving path matched) — worse than either error alone.
func TestStrykerRePrefixMovesPerFileKeys(t *testing.T) {
	s := &Stryker{ProjectRoot: "/repo", Workdir: "web"}
	r := &Report{
		Surviving: []Mutant{{File: "src/a.ts", Line: 3}},
		Files:     map[string]FileStat{"src/a.ts": {Survived: 1}},
	}
	s.rePrefixFiles(r)
	if r.Surviving[0].File != "web/src/a.ts" {
		t.Errorf("surviving path: %q want web/src/a.ts", r.Surviving[0].File)
	}
	want := map[string]FileStat{"web/src/a.ts": {Survived: 1}}
	if !reflect.DeepEqual(r.Files, want) {
		t.Errorf("per-file key must track Surviving:\n got %+v\nwant %+v", r.Files, want)
	}

	// Workdir unset: rows pass through untouched.
	bare := &Stryker{ProjectRoot: "/repo"}
	r2 := &Report{Files: map[string]FileStat{"src/a.ts": {Killed: 1}}}
	bare.rePrefixFiles(r2)
	if _, ok := r2.Files["src/a.ts"]; !ok || len(r2.Files) != 1 {
		t.Errorf("no-workdir rePrefix must be a no-op: %+v", r2.Files)
	}
}

func TestParseStrykerJSONEmptyFiles(t *testing.T) {
	r, err := ParseStrykerJSON([]byte(`{"schemaVersion":"1","files":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Killed != 0 || r.Survived != 0 || r.Score != 0 {
		t.Errorf("empty report should be all zero; got %+v", r)
	}
}

func TestParseStrykerJSONMalformed(t *testing.T) {
	if _, err := ParseStrykerJSON([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestStrykerSupports(t *testing.T) {
	s := &Stryker{}
	cases := map[string]bool{
		"src/api.ts":      true,
		"src/Button.tsx":  true,
		"src/util.js":     true,
		"src/page.svelte": true,
		"main.go":         false,
		"index.html":      false,
		"README.md":       false,
	}
	for file, want := range cases {
		if got := s.Supports(file); got != want {
			t.Errorf("Supports(%q): got %v want %v", file, got, want)
		}
	}
}

func TestStrykerName(t *testing.T) {
	if (&Stryker{}).Name() != "stryker" {
		t.Error("name should be 'stryker'")
	}
}

func TestDispatch(t *testing.T) {
	adapters := []Adapter{&Stryker{}}
	if got := Dispatch("src/x.ts", adapters); got == nil {
		t.Error("expected stryker for .ts")
	}
	if got := Dispatch("main.go", adapters); got != nil {
		t.Error("expected nil for .go (no go adapter yet)")
	}
}

// TestStrykerRunArgsScopedPackage pins the npx package name: bare "stryker"
// on the registry is the ancient pre-scoped package that crashes on modern
// Node — the invocation must use @stryker-mutator/core.
func TestStrykerRunArgsScopedPackage(t *testing.T) {
	s := &Stryker{ProjectRoot: "/repo"}
	args, _, err := s.runArgs([]string{"src/a.ts", "src/b.ts"})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}

	joined := strings.Join(args, " ")
	// The spec now carries an exact version (see strykerPin), so the assertion
	// is on the scoped name rather than on the bare-name-then-"run" adjacency
	// it used to check. TestStrykerRunArgsPinned covers the version half.
	if !strings.Contains(joined, "@stryker-mutator/core@") {
		t.Errorf("must invoke the scoped package: %v", args)
	}
	for _, a := range args {
		if a == "stryker" {
			t.Errorf("bare 'stryker' package must not be invoked: %v", args)
		}
	}
	if !strings.Contains(joined, "--mutate src/a.ts,src/b.ts") {
		t.Errorf("mutate list wrong: %v", args)
	}
}

// TestStrykerWorkdirMonorepo pins the workdir plumbing: --mutate paths are
// workdir-relative, stryker runs and reports under <root>/<workdir>, and
// parsed report paths are re-prefixed back to repo-relative.
func TestStrykerWorkdirMonorepo(t *testing.T) {
	s := &Stryker{ProjectRoot: "/repo", Workdir: "web"}

	args, _, err := s.runArgs([]string{"web/src/a.ts", "web/src/b.svelte"})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	if !strings.Contains(strings.Join(args, " "), "--mutate src/a.ts,src/b.svelte") {
		t.Errorf("workdir prefix must be stripped from --mutate: %v", args)
	}

	if got, want := s.workDir(), filepath.Join("/repo", "web"); got != want {
		t.Errorf("workDir = %q, want %q", got, want)
	}
	if got, want := s.reportPath(), filepath.Join("/repo", "web", "reports", "mutation", "mutation.json"); got != want {
		t.Errorf("reportPath = %q, want %q", got, want)
	}

	r := &Report{Surviving: []Mutant{{File: "src/a.ts", Line: 3}}}
	s.rePrefixFiles(r)
	if r.Surviving[0].File != "web/src/a.ts" {
		t.Errorf("report paths must be re-prefixed repo-relative, got %q", r.Surviving[0].File)
	}

	// Workdir unset: everything is a no-op passthrough.
	bare := &Stryker{ProjectRoot: "/repo"}
	r2 := &Report{Surviving: []Mutant{{File: "src/a.ts"}}}
	bare.rePrefixFiles(r2)
	if r2.Surviving[0].File != "src/a.ts" {
		t.Errorf("no-workdir rePrefix must be a no-op, got %q", r2.Surviving[0].File)
	}
	if bare.workDir() != "/repo" {
		t.Errorf("no-workdir workDir = %q, want /repo", bare.workDir())
	}
}

// strykerPinPattern is the shape an exact pin has: name@MAJOR.MINOR.PATCH and
// nothing else. It deliberately rejects everything npm would happily accept in
// its place — a bare name, a dist-tag, a caret or tilde range — because each of
// those hands the choice of what code runs back to whatever the registry serves
// at that moment.
var strykerPinPattern = regexp.MustCompile(`^@stryker-mutator/core@\d+\.\d+\.\d+$`)

// TestStrykerPinPatternRejectsLooseSpecs checks the checker. A regex that
// accepted a bare name would make TestStrykerRunArgsPinned vacuous, and a
// vacuous supply-chain test is worse than none — it reads as coverage.
func TestStrykerPinPatternRejectsLooseSpecs(t *testing.T) {
	for _, spec := range []string{
		"@stryker-mutator/core",
		"@stryker-mutator/core@latest",
		"@stryker-mutator/core@next",
		"@stryker-mutator/core@^9.0.0",
		"@stryker-mutator/core@~9.6.0",
		"@stryker-mutator/core@9",
		"@stryker-mutator/core@9.6",
		"@stryker-mutator/core@*",
	} {
		if strykerPinPattern.MatchString(spec) {
			t.Errorf("pattern accepted a loose spec: %q", spec)
		}
	}
	if !strykerPinPattern.MatchString("@stryker-mutator/core@9.6.1") {
		t.Error("pattern rejected a valid exact pin")
	}
}

// TestStrykerRunArgsPinned asserts the package spec npx is actually handed is
// an exact version. `npx --yes` suppresses the install prompt, so an unpinned
// spec here means dross downloads and executes registry-latest, unreviewed, on
// a developer machine — the shape of the 2025–2026 npm compromises.
func TestStrykerRunArgsPinned(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	args, _, err := s.runArgs([]string{"src/api/tags.ts"})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}

	yes := -1
	for i, a := range args {
		if a == "--yes" {
			yes = i
			break
		}
	}
	if yes < 0 {
		t.Fatalf("no --yes in argv: %q", args)
	}
	if yes+1 >= len(args) {
		t.Fatalf("nothing follows --yes: %q", args)
	}
	if spec := args[yes+1]; !strykerPinPattern.MatchString(spec) {
		t.Errorf("package spec %q is not an exact pin (argv: %q)", spec, args)
	}
}

// TestStrykerHintUsesSamePin catches the drift that makes a pin useless in
// practice: the invocation pinned, the advice telling the user to install
// something else. Forced through a Prefix naming a binary that does not exist,
// which fails as an *exec.Error rather than an *exec.ExitError and so takes the
// adapter-failure branch that carries the hint.
func TestStrykerHintUsesSamePin(t *testing.T) {
	s := &Stryker{
		Prefix:      "dross-no-such-binary-9f3c1a",
		ProjectRoot: t.TempDir(),
	}
	_, err := s.Run([]string{"src/api/tags.ts"})
	if err == nil {
		t.Fatal("expected the invocation to fail")
	}
	if !strings.Contains(err.Error(), "stryker invocation failed") {
		t.Fatalf("not the adapter-failure branch: %v", err)
	}
	if !strings.Contains(err.Error(), strykerPin) {
		t.Errorf("install hint does not carry %s: %v", strykerPin, err)
	}
}

// fakeStryker swaps the process-construction seam for one that runs `true` and
// lets the caller decide what — if anything — lands at the report path. Every
// error seam in Run() is downstream of "what stryker left on disk", so that is
// the only knob these tests need.
func fakeStryker(t *testing.T, s *Stryker, place func(reportPath string)) func() {
	t.Helper()
	orig := strykerBuildCmd
	strykerBuildCmd = func(_ *Stryker, _ []string) *exec.Cmd {
		path := s.reportPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("prepare fake report dir: %v", err)
		}
		if place != nil {
			place(path)
		}
		return exec.Command("true")
	}
	return func() { strykerBuildCmd = orig }
}

// TestStrykerRunDistinguishesMissingFromUnreadable pins both arms of the
// report-read guard. "Stryker wrote nothing" is a config problem the message
// must name the expected path for; anything else is an I/O failure that must
// surface as-is. Collapsing them — which is what dropping the ErrNotExist
// discrimination does — sends a user with a permissions problem to go audit
// their stryker config.
func TestStrykerRunDistinguishesMissingFromUnreadable(t *testing.T) {
	t.Run("no report written", func(t *testing.T) {
		s := &Stryker{ProjectRoot: t.TempDir()}
		defer fakeStryker(t, s, nil)()

		rep, err := s.Run([]string{"src/a.ts"})
		if err == nil {
			t.Fatal("Run with no report on disk returned nil error")
		}
		if rep != nil {
			t.Errorf("Run returned a report alongside its error: %+v", rep)
		}
		if !strings.Contains(err.Error(), "did not write a report") {
			t.Errorf("err = %q, want it to say the report was never written", err)
		}
		if !strings.Contains(err.Error(), s.reportPath()) {
			t.Errorf("err = %q, want it to name the expected path %q", err, s.reportPath())
		}
	})

	t.Run("report path is unreadable", func(t *testing.T) {
		s := &Stryker{ProjectRoot: t.TempDir()}
		// A directory where the report should be: ReadFile fails with
		// something that is NOT fs.ErrNotExist.
		defer fakeStryker(t, s, func(path string) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatalf("place directory at report path: %v", err)
			}
		})()

		_, err := s.Run([]string{"src/a.ts"})
		if err == nil {
			t.Fatal("Run with an unreadable report returned nil error")
		}
		if !strings.HasPrefix(err.Error(), "read stryker report:") {
			t.Errorf("err = %q, want it to begin \"read stryker report:\"", err)
		}
		if strings.Contains(err.Error(), "did not write a report") {
			t.Error("an unreadable report was reported as a missing one")
		}
	})
}

// TestStrykerRunSurfacesParseError: a report stryker wrote but nobody can parse
// must abort the run with the decode error and no report. Dropping the error
// check after parsing hands the caller a nil *Report that the next line
// dereferences.
func TestStrykerRunSurfacesParseError(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer fakeStryker(t, s, func(path string) {
		if err := os.WriteFile(path, []byte("this is not json"), 0o644); err != nil {
			t.Fatalf("write malformed report: %v", err)
		}
	})()

	rep, err := s.Run([]string{"src/a.ts"})
	if err == nil {
		t.Fatal("Run over a malformed report returned nil error")
	}
	if rep != nil {
		t.Errorf("Run returned a report alongside its parse error: %+v", rep)
	}
	if !strings.Contains(err.Error(), "decode stryker report") {
		t.Errorf("err = %q, want the ParseStrykerJSON error", err)
	}
}

// TestStrykerRunUnknownStatusCountsAsError drives Run's whole happy path and
// pins the default arm of the status switch. An unrecognised status is counted
// as an error so a stryker release that renames a status is loud rather than
// silently dropping mutants out of the denominator — and the count is asserted
// as an exact equality, so flipping `r.Errors++` to `r.Errors--` yields -1.
func TestStrykerRunUnknownStatusCountsAsError(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "stryker-unknown-status.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Guard the premise: a fixture whose status is later "fixed" to a known
	// one would make this pass while exercising the wrong arm.
	if !strings.Contains(string(payload), `"status": "Quantum"`) {
		t.Fatalf("fixture must carry an unrecognised status:\n%s", payload)
	}

	s := &Stryker{ProjectRoot: t.TempDir()}
	defer fakeStryker(t, s, func(path string) {
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("write fixture report: %v", err)
		}
	})()

	rep, err := s.Run([]string{"src/a.ts"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Errors != 1 {
		t.Errorf("Errors = %d, want exactly 1", rep.Errors)
	}
	if rep.Killed != 0 || rep.Survived != 0 || rep.Timeout != 0 {
		t.Errorf("an unknown status must count ONLY as an error, got %+v", rep)
	}
	want := map[string]FileStat{"src/a.ts": {Errors: 1}}
	if !reflect.DeepEqual(rep.Files, want) {
		t.Errorf("per-file rows:\n got %+v\nwant %+v", rep.Files, want)
	}
}

// TestStrykerMutateEscapesBracketPaths pins the escape form.
//
// Square brackets are the one metacharacter a real --mutate list carries all
// the time: SvelteKit names every dynamic route segment with them. Handed to
// Stryker raw, "[id]" is a minimatch character class, the path matches nothing,
// and the file is dropped from the run with a warning nobody reads while the
// score comes back looking complete.
func TestStrykerMutateEscapesBracketPaths(t *testing.T) {
	s := &Stryker{ProjectRoot: "/repo", Workdir: "web"}

	args, requested, err := s.runArgs([]string{"web/src/routes/recipes/[id]/+page.server.ts"})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	joined := strings.Join(args, " ")

	const want = "src/routes/recipes/[[]id[]]/@(+page.server.ts)"
	if !strings.Contains(joined, "--mutate "+want) {
		t.Errorf("--mutate is not the bracket-expression form:\n got %v\nwant it to contain %q", args, want)
	}

	// The obvious fix, asserted ABSENT. Stryker builds its FileMatcher with
	// normalizeFileName(path.resolve(pattern)), and normalizeFileName is
	// replace(/\\/g, "/") — so every backslash is a forward slash before
	// minimatch sees the pattern and `\[id\]` matches nothing at all. A future
	// reader "simplifying" the escape to backslashes would reintroduce the bug
	// in a form that looks more correct than the fix.
	if strings.Contains(joined, `\[`) || strings.Contains(joined, `\]`) {
		t.Errorf("backslash escaping is wrong here — stryker normalises every \\ to / before matching: %v", args)
	}

	// The unescaped request list is what a report-key diff has to compare
	// against: report keys come back in the REAL path form, so a caller handed
	// only the argv would find every bracket path missing.
	if len(requested) != 1 || requested[0] != "src/routes/recipes/[id]/+page.server.ts" {
		t.Errorf("requested list must be trimmed but UNESCAPED, got %q", requested)
	}
}

// TestStrykerEscapeIsSinglePass is the regression row for the implementation
// shape, not the interface. Two sequential ReplaceAll calls (a "[" pass then a
// "]" pass) look equivalent and are not: the second pass rewrites the "]" the
// first pass just emitted. Nested brackets are where that shows up.
func TestStrykerEscapeIsSinglePass(t *testing.T) {
	cases := []struct{ in, want string }{
		// The nesting case. A two-pass replace corrupts this one.
		{"[[opt]]", "@([[][[]opt[]][]])"},
		{"[id]", "@([[]id[]])"},
		{"src/routes/(app)/[...catchall]/+page.ts", "src/routes/(app)/[[]...catchall[]]/@(+page.ts)"},
		// Brackets in the FILENAME rather than a directory: the wrapper still
		// goes around the last segment, so it contains the escaped brackets.
		{"src/routes/[id].ts", "src/routes/@([[]id[]].ts)"},
		// Parentheses are literal to minimatch — a SvelteKit group like
		// (app) needs no escaping, and escaping it would be over-reach.
		{"src/routes/(app)/+page.ts", "src/routes/(app)/+page.ts"},
		// Untouched: the overwhelmingly common case must be byte-identical.
		{"src/lib/utils/format.ts", "src/lib/utils/format.ts"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escapeGlobMeta(tc.in); got != tc.want {
			t.Errorf("escapeGlobMeta(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStrykerPlainPathsAreUnchanged is the golden row that reds if the escape
// over-reaches. Nearly every --mutate entry in a real run is bracket-free, and
// a change to those is a change to every existing measurement.
func TestStrykerPlainPathsAreUnchanged(t *testing.T) {
	s := &Stryker{ProjectRoot: "/repo", Workdir: "web"}
	args, requested, err := s.runArgs([]string{"web/src/lib/utils/format.ts", "web/src/lib/server/tier.ts"})
	if err != nil {
		t.Fatalf("runArgs: %v", err)
	}
	const want = "--mutate src/lib/utils/format.ts,src/lib/server/tier.ts"
	if !strings.Contains(strings.Join(args, " "), want) {
		t.Errorf("plain paths must be emitted byte-identical to before escaping existed:\n got %v\nwant %q", args, want)
	}
	// And with no brackets anywhere, the two lists are the same strings.
	if strings.Join(requested, ",") != "src/lib/utils/format.ts,src/lib/server/tier.ts" {
		t.Errorf("requested list drifted from the argv on a bracket-free input: %q", requested)
	}
}

// TestStrykerEscapeRunsAfterTheFence: the escape must not become a way past
// argfence. It runs last, on the already-fenced list, so the fence still sees
// the string it is meant to judge.
//
// The existing refusal rows in argv_test.go cover the leading-dash case itself;
// this one pins the ORDER, which is what could silently change.
func TestStrykerEscapeRunsAfterTheFence(t *testing.T) {
	s := &Stryker{Workdir: "web"}

	// A dash entry that only becomes one after the workdir trim is still
	// refused — escaping happens after both, so it cannot rescue it.
	if _, _, err := s.runArgs([]string{"web/-rf.ts"}); err == nil {
		t.Error("escaping smuggled a leading-dash path past the fence")
	}
	// And a bracket path is not refused: escaping never introduces a dash, so
	// the fence must have nothing to say about it.
	if _, _, err := s.runArgs([]string{"web/src/routes/[id]/+page.ts"}); err != nil {
		t.Errorf("a bracket path was refused by the fence: %v", err)
	}
}

// TestStrykerEscapeSurvivesNpmArgQuoting pins the SECOND rewrite layer, and it
// is the one a reader is most likely to undo as redundant.
//
// dross invokes stryker through npx, and npm runs the command via `sh -c`
// after escaping each argument with @npmcli/promise-spawn's sh(), which quotes
// only an argument matching /[\t\n\r "#$&'()*;<>?\\`|~]/. Brackets are absent
// from that set, so a bracket-only path reaches the shell UNQUOTED and the
// shell glob-expands it straight back to the real on-disk filename — undoing
// the minimatch escape exactly when the file exists, which is the only case
// that matters. Measured against npm 11.14.1 on 2026-08-26.
//
// The extglob wrapper is what defeats that: its parentheses ARE in npm's
// quoting set. Assert the property (a character npm quotes on) rather than the
// literal "@(", so the row survives a different but equally valid wrapper.
func TestStrykerEscapeSurvivesNpmArgQuoting(t *testing.T) {
	// npm's own trigger set, transcribed from promise-spawn/lib/escape.js.
	const npmQuoteTriggers = "\t\n\r \"#$&'()*;<>?\\`|~"

	for _, in := range []string{
		"src/routes/recipes/[id]/+page.server.ts",
		"src/routes/[id].ts",
		"[id].ts",
	} {
		got := escapeGlobMeta(in)
		if !strings.ContainsAny(got, npmQuoteTriggers) {
			t.Errorf("escapeGlobMeta(%q) = %q — contains nothing npm quotes on, so `sh -c` will glob it back to the raw path and the escape is a no-op", in, got)
		}
	}

	// And the guard on the other side: a bracket-free path must NOT acquire a
	// wrapper just to satisfy the rule above. It needs no protection because
	// neither layer misbehaves on it.
	if got := escapeGlobMeta("src/lib/utils/format.ts"); got != "src/lib/utils/format.ts" {
		t.Errorf("a bracket-free path was rewritten: %q", got)
	}
}

// ── head-of-output surfacing and the dropped-file hard fail (c-5) ────────────

// noisyStryker swaps the process seam for one that prints `out` and exits with
// `code`, optionally leaving a report behind. The head-buffer and drop-check
// paths are both about what the TOOL said, which `true` cannot say.
func noisyStryker(t *testing.T, s *Stryker, out string, code int, place func(string)) func() {
	t.Helper()
	orig := strykerBuildCmd
	strykerBuildCmd = func(_ *Stryker, _ []string) *exec.Cmd {
		path := s.reportPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("prepare fake report dir: %v", err)
		}
		if place != nil {
			place(path)
		}
		return exec.Command("sh", "-c", fmt.Sprintf("cat <<'DROSSEOF'\n%s\nDROSSEOF\nexit %d", out, code))
	}
	return func() { strykerBuildCmd = orig }
}

// writeReport places a stryker report naming exactly `paths`, each with one
// killed mutant.
func writeReport(t *testing.T, path string, paths ...string) {
	t.Helper()
	files := map[string]any{}
	for _, p := range paths {
		files[p] = map[string]any{
			"language": "typescript",
			"source":   "export const x = 1;\n",
			"mutants": []any{map[string]any{
				"id": "1", "mutatorName": "BooleanLiteral", "status": "Killed",
				"location": map[string]any{"start": map[string]any{"line": 1, "column": 1}},
			}},
		}
	}
	b, err := json.Marshal(map[string]any{"schemaVersion": "1.0", "files": files})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStrykerReportlessErrorNamesHeadOfOutput: "check stryker config" was wrong
// in every case it was actually hit — the config was fine and the cause was at
// the head of the output the user had just watched scroll past. The commonest
// instance, verbatim.
func TestStrykerReportlessErrorNamesHeadOfOutput(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	const cause = "Missing required environment variable: DATABASE_URL"
	defer noisyStryker(t, s, "INFO Stryker Starting\n"+cause, 1, nil)()

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a reportless failure returned nil error")
	}
	if !strings.Contains(err.Error(), cause) {
		t.Errorf("the error does not quote the real cause:\n%v", err)
	}
	if strings.Contains(err.Error(), "check stryker config") {
		t.Errorf("the misleading advice is still there:\n%v", err)
	}
}

// TestStrykerHardFailsOnUninstrumentedFile is locked decision drop_behaviour.
func TestStrykerHardFailsOnUninstrumentedFile(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "done", 0, func(p string) {
		writeReport(t, p, "src/a.ts", "src/b.ts") // asked for three
	})()

	_, rep, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/a.ts", "src/b.ts", "src/c.ts"})
	})
	if err == nil {
		t.Fatal("a run that silently dropped a file returned nil error")
	}
	if !strings.Contains(err.Error(), "src/c.ts") {
		t.Errorf("the error does not name the dropped file:\n%v", err)
	}
	for _, present := range []string{"src/a.ts", "src/b.ts"} {
		if strings.Contains(err.Error(), present) {
			t.Errorf("the error names %s, which WAS instrumented:\n%v", present, err)
		}
	}
	// NIL, not empty. An empty *Report reads as a perfect score — the exact
	// trap argv_test.go already guards on the refusal path.
	if rep != nil {
		t.Errorf("a hard fail still produced a Report: %+v", rep)
	}
}

// TestStrykerDropWarningIsQuoted: stryker's own wording for the fault, carried
// through verbatim so a user can search for it.
func TestStrykerDropWarningIsQuoted(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	const warning = `Glob pattern "src/routes/x/[id]/y.ts" did not result in any files.`
	defer noisyStryker(t, s, "WARN ProjectReader "+warning, 0, func(p string) {
		writeReport(t, p, "src/a.ts")
	})()

	_, _, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/a.ts", "src/routes/x/[id]/y.ts"})
	})
	if err == nil {
		t.Fatal("a dropped glob returned nil error")
	}
	if !strings.Contains(err.Error(), warning) {
		t.Errorf("stryker's own warning is not in the error:\n%v", err)
	}
}

// TestStrykerBracketPathIsNotReadAsDropped is the specific way t-1's escaping
// and this drop check break each other, asserted directly rather than left to
// the end-to-end run.
//
// The argv carries the ESCAPED form while the report comes back keyed on the
// REAL path. A check that diffed report keys against the argv would call every
// bracket path dropped — and would do it on exactly the files the escaping fix
// exists to instrument.
func TestStrykerBracketPathIsNotReadAsDropped(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "done", 0, func(p string) {
		writeReport(t, p, "src/routes/a/[id]/b.ts")
	})()

	_, rep, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/routes/a/[id]/b.ts"})
	})
	if err != nil {
		t.Fatalf("an instrumented bracket path was read as dropped: %v", err)
	}
	if rep == nil || len(rep.Files) == 0 {
		t.Fatalf("no report came back for an instrumented bracket path: %+v", rep)
	}
}

// TestStrykerIgnoredMutantsAreNotADrop: a file whose glob MATCHED but whose
// every mutant is Ignored — the `// Stryker disable all` case — is instrumented
// and must not hard-fail.
//
// This is why the check reads the report's raw keys rather than
// ParseStrykerJSON's Files map: Ignored is not counted, so such a file gets no
// row there and a naive diff cannot tell it from a glob that matched nothing.
func TestStrykerIgnoredMutantsAreNotADrop(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "done", 0, func(p string) {
		b, err := json.Marshal(map[string]any{
			"schemaVersion": "1.0",
			"files": map[string]any{
				"src/disabled.ts": map[string]any{
					"language": "typescript", "source": "export const x = 1;\n",
					"mutants": []any{map[string]any{
						"id": "1", "mutatorName": "BooleanLiteral", "status": "Ignored",
						"location": map[string]any{"start": map[string]any{"line": 1, "column": 1}},
					}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	})()

	_, rep, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/disabled.ts"})
	})
	if err != nil {
		t.Fatalf("an all-Ignored file was reported as dropped: %v", err)
	}
	if rep == nil {
		t.Fatal("no report came back")
	}
	// And the premise holds: it really does carry no counted row, so a diff
	// against report.Files WOULD have failed here.
	if _, ok := rep.Files["src/disabled.ts"]; ok {
		t.Error("premise broken — an all-Ignored file now has a counted row, so this row no longer proves what it claims")
	}
}

// TestStrykerFullCoverageDoesNotHardFail: the strict check must not red a
// healthy run.
func TestStrykerFullCoverageDoesNotHardFail(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "done", 0, func(p string) {
		writeReport(t, p, "src/a.ts", "src/b.ts")
	})()

	_, rep, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"src/a.ts", "src/b.ts"})
	})
	if err != nil {
		t.Fatalf("a complete run was hard-failed: %v", err)
	}
	if rep == nil || rep.Killed != 2 {
		t.Errorf("want a report with 2 killed, got %+v", rep)
	}
}

// TestStrykerDropCheckSpeaksWorkdirRelativePaths pins the diff to the correct
// side of rePrefixFiles.
//
// `requested` is the TRIMMED, workdir-relative list (src/a.ts), while
// rePrefixFiles rewrites the report's keys to repo-relative (web/src/a.ts).
// Run the diff one line later and the two forms no longer agree — every path
// looks dropped and every healthy monorepo run hard-fails.
func TestStrykerDropCheckSpeaksWorkdirRelativePaths(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir(), Workdir: "web"}
	defer noisyStryker(t, s, "done", 0, func(p string) {
		writeReport(t, p, "src/a.ts") // workdir-relative, as stryker writes it
	})()

	_, rep, err := captureStderr(t, func() (*Report, error) {
		return s.Run([]string{"web/src/a.ts"})
	})
	if err != nil {
		t.Fatalf("a healthy monorepo run was hard-failed — the diff ran on the wrong side of rePrefixFiles: %v", err)
	}
	// And the report the caller gets is still repo-relative, so the check did
	// not sidestep the re-prefixing it has to precede.
	if _, ok := rep.Files["web/src/a.ts"]; !ok {
		t.Errorf("report keys were not re-prefixed repo-relative: %v", keysOf(rep.Files))
	}
}

// TestStrykerHeadBufferIsBoundedAndDoesNotSwallowTheStream: the retained head
// is capped, but capping it must not truncate what the user watches.
//
// io.MultiWriter aborts on a short write, so a headBuffer reporting the number
// of bytes it KEPT would silently cut os.Stderr off at the cap.
func TestStrykerHeadBufferIsBoundedAndDoesNotSwallowTheStream(t *testing.T) {
	const line = "0123456789012345678901234567890123456789012345678901234567890123456789012345678"
	const lines = 30000 // ~2.4MB, comfortably past strykerHeadBytes

	s := &Stryker{ProjectRoot: t.TempDir()}
	defer func() func() {
		orig := strykerBuildCmd
		strykerBuildCmd = func(_ *Stryker, _ []string) *exec.Cmd {
			_ = os.MkdirAll(filepath.Dir(s.reportPath()), 0o755)
			return exec.Command("sh", "-c",
				fmt.Sprintf("i=0; while [ $i -lt %d ]; do echo '%s'; i=$((i+1)); done; exit 1", lines, line))
		}
		return func() { strykerBuildCmd = orig }
	}()()

	out, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("expected the reportless failure")
	}
	if got, want := len(out), lines*(len(line)+1); got < want {
		t.Errorf("the stream was truncated: os.Stderr got %d bytes, want the full %d", got, want)
	}
	if n := strings.Count(err.Error(), "\n"); n > strykerHeadLines+10 {
		t.Errorf("the error quoted %d lines; it must quote at most ~%d", n, strykerHeadLines)
	}
}

// TestStrykerLocalRunClearsAStaleReport: a local run that writes nothing must
// be reportless, not answered with yesterday's numbers.
//
// clearReport used to return early for a local run, so a failing local run read
// whatever the previous one left behind. Observed 2026-08-26: a run that
// instrumented zero files and died in its dry run returned a nil error and a
// report two days old.
func TestStrykerLocalRunClearsAStaleReport(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	stale := s.reportPath()
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReport(t, stale, "src/yesterday.ts")

	// The tool runs, writes nothing this time, and fails.
	defer noisyStryker(t, s, "ERROR Stryker No tests were executed.", 1, nil)()

	_, rep, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a local run that wrote no report returned nil error — yesterday's report was read as today's")
	}
	if rep != nil {
		t.Errorf("a stale report was returned: %+v", rep)
	}
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("yesterday's report survived a local run, stat err = %v", statErr)
	}
}

// ── initial-test-run abort: the truncation note (c-4) ────────────────────────

// c-4 positive: an abort whose output carries Stryker's initial-test-run
// header gets the truncation note.
//
// The header is written out as a HARDCODED literal here, not read from
// strykerInitialTestFailureText — the constant is never fed its own text, so a
// reworded constant (a dropped trailing colon, say) stops matching real
// Stryker output and this test goes red, which is the whole point of pinning
// wording in a constant.
func TestStrykerInitialTestFailureAbortNamesTruncation(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	const header = "One or more tests failed in the initial test run:"
	out := "INFO Stryker Starting\n" + header + "\n\tsrc/lib/a.test.ts > adds two numbers"
	defer noisyStryker(t, s, out, 1, nil)()

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("an initial-test-run abort returned nil error")
	}
	if !strings.Contains(err.Error(), strykerInitialTestTruncationNote) {
		t.Errorf("the truncation note is missing from an initial-test-run abort:\n%v", err)
	}
}

// c-4 note content: the note has to say BOTH that only the first failing test
// is named AND that the list is truncated, so the reader knows an unknown
// number remain rather than assuming the one shown is the whole problem.
//
// Driven with the constant, so this test is about what the note SAYS; the test
// above is about whether it fires on Stryker's real wording.
func TestStrykerTruncationNoteSaysListIsTruncated(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, strykerInitialTestFailureText+"\n\tsrc/lib/a.test.ts > adds two numbers", 1, nil)()

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("an initial-test-run abort returned nil error")
	}
	low := strings.ToLower(err.Error())
	for _, want := range []string{"only the first failing test", "truncated", "unknown number"} {
		if !strings.Contains(low, want) {
			t.Errorf("the note does not say %q — the reader would take the one named test for the whole problem:\n%v", want, err)
		}
	}
}

// c-4 negative: the note is attached ONLY on an initial-test-run abort. A
// reportless failure with some other cause never printed a truncated list, so
// claiming one is misleading in its own right (locked decision note_trigger).
func TestStrykerTruncationNoteAbsentOnOtherAborts(t *testing.T) {
	s := &Stryker{ProjectRoot: t.TempDir()}
	defer noisyStryker(t, s, "INFO Stryker Starting\nMissing required environment variable: DATABASE_URL", 1, nil)()

	_, _, err := captureStderr(t, func() (*Report, error) { return s.Run([]string{"src/a.ts"}) })
	if err == nil {
		t.Fatal("a reportless failure returned nil error")
	}
	if strings.Contains(err.Error(), strykerInitialTestTruncationNote) {
		t.Errorf("the truncation note was attached to an abort that printed no failure list:\n%v", err)
	}
}
