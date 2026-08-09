package mutation

import (
	"math"
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
	args, err := s.runArgs([]string{"src/a.ts", "src/b.ts"})
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

	args, err := s.runArgs([]string{"web/src/a.ts", "web/src/b.svelte"})
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
	args, err := s.runArgs([]string{"src/api/tags.ts"})
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
