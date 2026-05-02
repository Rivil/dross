package mutation

import (
	"math"
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
