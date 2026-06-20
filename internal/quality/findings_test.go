package quality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnrefutedIsNotSurvivor(t *testing.T) {
	l := Ledger{Findings: []Finding{
		{ID: "f-1", Risk: RiskHigh, Dimension: Complexity, Refutation: "panel: hot path, not refuted"},
		{ID: "f-2", Risk: RiskCritical, Dimension: Duplication, Refutation: ""}, // no evidence → not a survivor
	}}
	got := l.Survivors()
	if len(got) != 1 || got[0].ID != "f-1" {
		t.Fatalf("Survivors = %+v, want only f-1 (f-2 lacks refutation)", got)
	}
}

func TestLedgerDuplicateID(t *testing.T) {
	if err := (Ledger{Findings: []Finding{{ID: "", Risk: RiskHigh}}}).Validate(); err == nil {
		t.Error("Validate accepted a finding with an empty id")
	}
	dup := Ledger{Findings: []Finding{
		{ID: "f-1", Risk: RiskHigh},
		{ID: "f-1", Risk: RiskLow},
	}}
	if err := dup.Validate(); err == nil {
		t.Error("Validate accepted two findings sharing id f-1")
	}
}

func TestLedgerInvalidRisk(t *testing.T) {
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Risk: ""}}}).Validate(); err == nil {
		t.Error("Validate accepted a finding with empty risk")
	}
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Risk: "spicy"}}}).Validate(); err == nil {
		t.Error("Validate accepted a finding with an unknown risk")
	}
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Risk: RiskHigh}}}).Validate(); err != nil {
		t.Errorf("Validate rejected a valid finding: %v", err)
	}
}

func TestSurvivorsRiskOrder(t *testing.T) {
	// The contextual ranking_model is blast-radius weighted: the panel assigns a
	// high-complexity finding on a COLD path a Low risk, and a moderate-complexity
	// finding on a CORE/HOT path a High risk — so the hot one must sort ABOVE the
	// cold one despite being intrinsically "less complex". An unknown risk sorts last.
	l := Ledger{Findings: []Finding{
		{ID: "f-cold", Title: "high complexity, cold path", Risk: RiskLow, Dimension: Complexity,
			Refutation: "panel: rarely-changed, isolated — downranked"},
		{ID: "f-unknown", Title: "weird", Risk: "ungraded", Dimension: Coupling,
			Refutation: "panel: kept"},
		{ID: "f-hot", Title: "moderate complexity, core path", Risk: RiskHigh, Dimension: Complexity,
			Refutation: "panel: central, churny — upranked"},
	}}
	got := l.Survivors()
	ids := []string{}
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	want := []string{"f-hot", "f-cold", "f-unknown"}
	if len(ids) != len(want) {
		t.Fatalf("Survivors ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("Survivors order = %v, want %v (hot core-path above cold, unknown last)", ids, want)
		}
	}
}

func TestLoadMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.toml")
	// Truncated / garbled TOML (unterminated string).
	if err := os.WriteFile(path, []byte("[[finding]]\nid = \"f-1\"\nrisk = \"hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a garbled findings.toml; want an error, not a panic")
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.toml")
	in := Ledger{Findings: []Finding{
		{ID: "f-1", Title: "god function orchestrates whole run", Risk: RiskHigh,
			Dimension: Complexity, File: "internal/cmd/run.go", Line: 42,
			Evidence: "cyclomatic 38, called on every path", Refutation: "panel: central, confirmed"},
	}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 1 {
		t.Fatalf("round-trip lost findings: got %d", len(out.Findings))
	}
	if g := out.Findings[0]; g.Risk != RiskHigh || g.Dimension != Complexity || g.Refutation != "panel: central, confirmed" {
		t.Errorf("round-trip dropped risk/dimension/refutation: %+v", g)
	}
}
