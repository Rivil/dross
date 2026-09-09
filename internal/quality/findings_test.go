package quality

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/compilefence"
	"github.com/Rivil/dross/internal/pathfence"
)

// containedIn builds the Contained for name inside dir. Every test that reaches
// the filesystem goes through it, because Save and Load no longer accept a
// string: the retype is the point of this file's half of the phase.
func containedIn(t *testing.T, dir, name string) pathfence.Contained {
	t.Helper()
	c, err := pathfence.Contain(dir, "run directory", name)
	if err != nil {
		t.Fatalf("Contain(%q, %q): %v", dir, name, err)
	}
	return c
}

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
	dir := t.TempDir()
	// Truncated / garbled TOML (unterminated string).
	if err := os.WriteFile(filepath.Join(dir, "findings.toml"), []byte("[[finding]]\nid = \"f-1\"\nrisk = \"hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(containedIn(t, dir, "findings.toml")); err == nil {
		t.Fatal("Load accepted a garbled findings.toml; want an error, not a panic")
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	c := containedIn(t, t.TempDir(), "findings.toml")
	in := Ledger{Findings: []Finding{
		{ID: "f-1", Title: "god function orchestrates whole run", Risk: RiskHigh,
			Dimension: Complexity, File: "internal/cmd/run.go", Line: 42,
			Evidence: "cyclomatic 38, called on every path", Refutation: "panel: central, confirmed"},
	}}
	if err := Save(c, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 1 {
		t.Fatalf("round-trip lost findings: got %d", len(out.Findings))
	}
	if g := out.Findings[0]; g.Risk != RiskHigh || g.Dimension != Complexity || g.Refutation != "panel: central, confirmed" {
		t.Errorf("round-trip dropped risk/dimension/refutation: %+v", g)
	}
	// The path field specifically: moving the I/O boundary must not disturb what
	// is written, and a finding's File is the field a reader most easily breaks.
	if g := out.Findings[0].File; g != "internal/cmd/run.go" {
		t.Errorf("round-trip dropped the finding path field: got %q", g)
	}
}

// TestLoadAndSaveRequireContainedPath is the compile-time half of c-4 for this
// package: Load and Save take a pathfence.Contained, which has no constructor
// outside pathfence, so a caller that never ran the containment check has no
// value to pass and fails to BUILD. A runtime test cannot assert this — the
// fixture would have to be valid Go to be referenced at all.
func TestLoadAndSaveRequireContainedPath(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	// Positive control FIRST. Without it a broken compile fence (bad module
	// path, unresolvable go.mod) makes every refusal below a false positive.
	compilefence.AssertCompiles(t, `package tmpfence

import (
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/quality"
)

func f() {
	c, err := pathfence.Contain("/run", "run directory", "findings.toml")
	if err != nil {
		return
	}
	_, _ = quality.Load(c)
	_ = quality.Save(c, quality.Ledger{})
}
`)

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import "github.com/Rivil/dross/internal/quality"

func f() { _, _ = quality.Load("findings.toml") }
`, "as pathfence.Contained value in argument to quality.Load")

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import "github.com/Rivil/dross/internal/quality"

func f() { _ = quality.Save("findings.toml", quality.Ledger{}) }
`, "as pathfence.Contained value in argument to quality.Save")

	compilefence.AssertDoesNotCompile(t, `package tmpfence

import "github.com/Rivil/dross/internal/quality"

func f() { _ = quality.WriteScaffoldSpec("spec.toml", "07-x", "x", quality.Ledger{}) }
`, "as pathfence.Contained value in argument to quality.WriteScaffoldSpec")
}

// TestPackageInternalImports pins the repo-internal import set of this package.
// The retype added exactly one dependency — internal/pathfence, which is a leaf
// and imports nothing from internal/ — so no cycle is possible. A future edit
// that drags a heavier package in here has to update this list deliberately.
func TestPackageInternalImports(t *testing.T) {
	want := map[string]bool{
		"github.com/Rivil/dross/internal/findings":  true,
		"github.com/Rivil/dross/internal/pathfence": true,
		"github.com/Rivil/dross/internal/phase":     true,
		"github.com/Rivil/dross/internal/stack":     true,
	}
	assertInternalImports(t, ".", want)
}

// assertInternalImports walks every non-test .go file in dir with go/parser and
// compares the github.com/Rivil/dross imports against want.
func assertInternalImports(t *testing.T, dir string, want map[string]bool) {
	t.Helper()
	const prefix = `"github.com/Rivil/dross/`
	got := map[string]bool{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if strings.HasPrefix(imp.Path.Value, prefix) {
				got[strings.Trim(imp.Path.Value, `"`)] = true
			}
		}
	}
	for p := range want {
		if !got[p] {
			t.Errorf("expected internal import %s is gone", p)
		}
	}
	for p := range got {
		if !want[p] {
			t.Errorf("undeclared internal import %s — the retype must not smuggle in a new dependency", p)
		}
	}
}

// TestRunDirRootStaysAPlainString pins the rule the retype must NOT erode: a
// containment ROOT is a plain string. NewRun's own os.Stat / os.MkdirAll operate
// on the run directory itself, which is the root, not a path contained within
// one — so they are deliberately untouched, and Contain takes that root as a
// string.
func TestRunDirRootStaysAPlainString(t *testing.T) {
	root := t.TempDir()
	runDir, err := NewRun(root, time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC), "abc1234")
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}
	if _, err := os.Stat(runDir); err != nil {
		t.Fatalf("NewRun did not create the run dir: %v", err)
	}
	// The root is a string; only what goes INSIDE it is contained.
	if err := Save(containedIn(t, runDir, "findings.toml"), Ledger{}); err != nil {
		t.Fatalf("Save into a run dir created by NewRun: %v", err)
	}
}
