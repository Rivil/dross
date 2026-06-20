package security

import (
	"errors"
	"testing"
)

func TestCatalogCompleteness(t *testing.T) {
	core := map[string]bool{
		"govulncheck": false, "gosec": false, "gitleaks": false,
		"semgrep": false, "trivy": false,
	}
	for _, s := range Catalog() {
		if _, ok := core[s.Name]; ok {
			core[s.Name] = true
			if !s.Core {
				t.Errorf("scanner %q is in the core set but not flagged Core", s.Name)
			}
		}
	}
	for name, found := range core {
		if !found {
			t.Errorf("core scanner %q missing from the catalog", name)
		}
	}
}

func TestAvailabilityMissingHasHint(t *testing.T) {
	allMissing := func(string) (string, error) { return "", errors.New("not found") }
	statuses := Detect(Catalog(), allMissing)
	if len(statuses) == 0 {
		t.Fatal("Detect returned no statuses")
	}
	for _, st := range statuses {
		if st.Installed {
			t.Errorf("scanner %q reported installed under an all-missing lookup", st.Name)
		}
		if st.Install == "" {
			t.Errorf("missing scanner %q has no install instruction", st.Name)
		}
	}
}

func TestScannersForAgnosticFallback(t *testing.T) {
	// python has no dedicated catalog (a stub language) — it must still get the
	// agnostic scanners, never an empty set.
	got := ScannersFor("python")
	if len(got) == 0 {
		t.Fatal("ScannersFor(\"python\") returned no scanners; agnostic fallback broken")
	}
	want := map[string]bool{"gitleaks": true, "semgrep": true, "trivy": true}
	for _, s := range got {
		if !s.Agnostic() {
			t.Errorf("ScannersFor(\"python\") returned non-agnostic scanner %q", s.Name)
		}
		if !want[s.Name] {
			t.Errorf("unexpected agnostic scanner %q for python", s.Name)
		}
	}
}

func TestScannersForGoIsComplete(t *testing.T) {
	names := map[string]bool{}
	for _, s := range ScannersFor("go") {
		names[s.Name] = true
	}
	for _, want := range []string{"govulncheck", "gosec", "staticcheck", "osv-scanner", "gitleaks", "semgrep", "trivy"} {
		if !names[want] {
			t.Errorf("ScannersFor(\"go\") missing %q", want)
		}
	}
}
