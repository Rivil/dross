package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLedgerValidateSeverity(t *testing.T) {
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Severity: ""}}}).Validate(); err == nil {
		t.Error("Validate accepted a finding with empty severity")
	}
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Severity: "spicy"}}}).Validate(); err == nil {
		t.Error("Validate accepted a finding with an unknown severity")
	}
	if err := (Ledger{Findings: []Finding{{ID: "f-1", Severity: SeverityHigh}}}).Validate(); err != nil {
		t.Errorf("Validate rejected a valid finding: %v", err)
	}
}

func TestSurvivorsRequireRefutation(t *testing.T) {
	l := Ledger{Findings: []Finding{
		{ID: "f-1", Severity: SeverityHigh, Refutation: "panel: reachable from handler, not refuted"},
		{ID: "f-2", Severity: SeverityCritical, Refutation: ""}, // no evidence → not a survivor
	}}
	got := l.Survivors()
	if len(got) != 1 || got[0].ID != "f-1" {
		t.Fatalf("Survivors = %+v, want only f-1 (f-2 lacks refutation)", got)
	}
}

func TestLoadMalformedLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.toml")
	// Truncated / garbled TOML (unterminated string).
	if err := os.WriteFile(path, []byte("[[finding]]\nid = \"f-1\"\nseverity = \"crit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a garbled findings.toml; want an error, not a panic")
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.toml")
	in := Ledger{Findings: []Finding{
		{ID: "f-1", Title: "cmd injection in git shell-out", Severity: SeverityCritical,
			Class: "cmd-injection", File: "internal/forge/git.go", Line: 42,
			Evidence: "user input flows to exec", Refutation: "panel: confirmed reachable"},
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
	if g := out.Findings[0]; g.Severity != SeverityCritical || g.Refutation != "panel: confirmed reachable" {
		t.Errorf("round-trip dropped severity/refutation: %+v", g)
	}
}
