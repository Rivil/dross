package quality

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// sampleLedger has a low finding (f-1) listed BEFORE a critical (f-2), so tests
// can prove the scaffold re-orders highest-risk-first rather than echoing input order.
func sampleLedger() Ledger {
	return Ledger{Findings: []Finding{
		{ID: "f-1", Title: "duplicated parser across three commands", Risk: RiskLow,
			Dimension: Duplication, Refutation: "panel: real clone, low blast radius"},
		{ID: "f-2", Title: "god function orchestrates the whole run", Risk: RiskCritical,
			Dimension: Complexity, Refutation: "panel: central, churny — confirmed"},
	}}
}

func TestScaffoldOnePerFinding(t *testing.T) {
	spec, err := ScaffoldSpec("07-remediate", "Remediate quality findings", sampleLedger())
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Criteria) != 2 {
		t.Fatalf("expected one criterion per surviving finding (2), got %d", len(spec.Criteria))
	}
}

func TestScaffoldRiskOrder(t *testing.T) {
	spec, err := ScaffoldSpec("07-remediate", "Remediate", sampleLedger())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec.Criteria[0].Text, "f-2") {
		t.Fatalf("first criterion isn't the critical finding f-2: %q", spec.Criteria[0].Text)
	}
	if !strings.Contains(spec.Criteria[1].Text, "f-1") {
		t.Fatalf("second criterion isn't the low finding f-1: %q", spec.Criteria[1].Text)
	}
}

func TestScaffoldCitesLedger(t *testing.T) {
	spec, err := ScaffoldSpec("07-remediate", "Remediate", sampleLedger())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"c-1": "f-2", "c-2": "f-1"} // highest-risk-first
	for _, c := range spec.Criteria {
		want := ids[c.ID]
		if !strings.Contains(c.Text, want) {
			t.Errorf("criterion %s does not cite its ledger id %q: %q", c.ID, want, c.Text)
		}
	}
}

func TestScaffoldEmptyRefuses(t *testing.T) {
	// A finding with no refutation is not a survivor → zero survivors → refuse.
	l := Ledger{Findings: []Finding{{ID: "f-1", Risk: RiskHigh, Refutation: ""}}}
	if _, err := ScaffoldSpec("07-x", "x", l); err == nil {
		t.Fatal("ScaffoldSpec built a spec from zero survivors; want a refusal")
	}
}

func TestScaffoldRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.toml")
	if err := WriteScaffoldSpec(path, "07-remediate", "Remediate quality findings", sampleLedger()); err != nil {
		t.Fatal(err)
	}
	spec, err := phase.LoadSpec(path)
	if err != nil {
		t.Fatalf("emitted spec.toml failed to round-trip through phase.LoadSpec: %v", err)
	}
	if spec.Phase.ID != "07-remediate" || len(spec.Criteria) != 2 {
		t.Fatalf("round-tripped spec wrong: id=%q criteria=%d", spec.Phase.ID, len(spec.Criteria))
	}
}
