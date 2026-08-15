package quality

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/stack"
)

// An empty analyzer set for a language dross claims to support is a silent
// no-op audit: `dross quality` runs, reports nothing, and exits 0. That reads
// identically to a clean codebase. These tests are what stop a non-Go stack
// from getting that answer.

// TestTypeScriptGetsItsDedicatedAnalyzers asserts the tools BY NAME rather than
// checking for a non-empty set.
//
// Non-empty is satisfied by the agnostic pair alone (scc + jscpd), which is
// exactly the state a TypeScript project would be in if its profile stopped
// contributing anything — the audit would still look like it ran.
func TestTypeScriptGetsItsDedicatedAnalyzers(t *testing.T) {
	got := AnalyzersFor("typescript")
	if len(got) == 0 {
		t.Fatal("typescript resolves to no analyzers at all")
	}
	names := analyzerNames(got)

	// eslint is the profile's declared complexity analyzer. If the dedicated
	// set stopped resolving, this is the name that vanishes first.
	if !hasName(names, "eslint") {
		t.Errorf("typescript did not get its dedicated eslint analyzer — got %v", names)
	}
	// And it must be more than the agnostic pair, or the profile contributed
	// nothing while the set still looked populated.
	dedicated := 0
	for _, a := range got {
		if !a.Agnostic() {
			dedicated++
		}
	}
	if dedicated == 0 {
		t.Errorf("every analyzer for typescript is agnostic — the profile contributed none: %v", names)
	}
}

// TestEveryProfileWithAnalyzersContributesThem walks the SHIPPED profiles
// rather than a hand-typed list, so a profile added tomorrow is covered on
// arrival instead of when someone remembers this file.
func TestEveryProfileWithAnalyzersContributesThem(t *testing.T) {
	profiles, err := stack.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("no shipped profiles — this test would pass vacuously")
	}

	checked := 0
	for _, p := range profiles {
		var declared []string
		for _, tool := range p.Tools {
			if tool.Kind == "analyzer" {
				declared = append(declared, tool.Name)
			}
		}
		if len(declared) == 0 {
			continue // a profile may legitimately declare no dedicated analyzer
		}
		checked++
		names := analyzerNames(AnalyzersFor(p.ID))
		for _, want := range declared {
			if !hasName(names, want) {
				t.Errorf("profile %s declares analyzer %q but AnalyzersFor(%q) does not return it — got %v", p.ID, want, p.ID, names)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no shipped profile declares an analyzer — the walk asserted nothing")
	}
}

// TestDedicatedAnalyzersDoNotDisplaceAgnostic: dedicated tools are ADDITIVE.
// Losing scc/jscpd for a language with a profile would narrow the audit while
// looking like an upgrade — the language gained a specialist and quietly lost
// duplication and complexity coverage.
func TestDedicatedAnalyzersDoNotDisplaceAgnostic(t *testing.T) {
	agnostic := analyzerNames(AnalyzersFor(""))
	if len(agnostic) == 0 {
		t.Fatal("the agnostic set is empty — every assertion below would be vacuous")
	}
	for _, lang := range []string{"typescript", "javascript", "python", "go"} {
		names := analyzerNames(AnalyzersFor(lang))
		for _, want := range agnostic {
			if !hasName(names, want) {
				t.Errorf("%s lost the agnostic analyzer %q — dedicated tools add to the set, they do not replace it", lang, want)
			}
		}
	}
}

// TestUnknownLanguageStillGetsTheAgnosticSet is the floor: a stack dross has no
// profile for must still be audited by something, or adopting dross into an
// unfamiliar repo silently disables the audit.
func TestUnknownLanguageStillGetsTheAgnosticSet(t *testing.T) {
	names := analyzerNames(AnalyzersFor("cobol"))
	if len(names) == 0 {
		t.Fatal("an unknown language resolves to no analyzers — the audit would silently do nothing")
	}
	for _, a := range AnalyzersFor("cobol") {
		if !a.Agnostic() {
			t.Errorf("an unknown language picked up the dedicated analyzer %q", a.Name)
		}
	}
}

func analyzerNames(as []Analyzer) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}
