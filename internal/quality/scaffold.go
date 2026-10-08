package quality

import (
	"fmt"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
)

// ScaffoldSpec turns the surviving findings in a ledger into a remediation phase
// spec: one acceptance criterion per surviving finding, ordered highest-risk-first
// (maintainability-risk drives wave order downstream), each criterion citing its
// findings.toml ledger id so no finding can hide behind a tier-level test. It
// refuses to build a spec from zero survivors — a vacuous remediation phase is
// never written.
func ScaffoldSpec(phaseID, title string, l Ledger) (*phase.Spec, error) {
	survivors := l.Survivors()
	if len(survivors) == 0 {
		return nil, fmt.Errorf("no surviving findings: refusing to scaffold a vacuous remediation phase")
	}
	spec := &phase.Spec{Phase: phase.SpecPhase{ID: phaseID, Title: title}}
	for i, f := range survivors {
		spec.Criteria = append(spec.Criteria, phase.Criterion{
			ID:   fmt.Sprintf("c-%d", i+1),
			Text: criterionText(f),
		})
	}
	return spec, nil
}

// criterionText renders one finding as a testable acceptance criterion, leading
// with the ledger id (the citation), the substantive dimension, and the
// contextual maintainability-risk.
func criterionText(f Finding) string {
	what := f.Title
	if what == "" {
		what = string(f.Dimension)
	}
	dim := string(f.Dimension)
	if dim == "" {
		dim = "maintainability"
	}
	return fmt.Sprintf("[%s] %s is resolved and a test proves it — %s debt (risk: %s)",
		f.ID, what, dim, f.Risk)
}

// WriteScaffoldSpec builds the remediation spec from the ledger and writes it as
// TOML to the contained path, using phase.Spec.Save so the output matches the
// repo's spec.toml formatting and round-trips through phase.LoadSpec.
//
// The Contained crosses into phase.Spec.Save as its JOINED form. That boundary
// is deliberate and is the one place in this package where unwrapping is
// correct: phase.saveTOML is a func(path string) whose atomic write (encode to
// <path>.tmp, then rename) is crash-safety pathfence.WriteFile does not
// replicate, and internal/phase is not retyped by this phase. Rel() here would
// resolve against the process working directory and scatter spec.toml wherever
// the command happened to run.
func WriteScaffoldSpec(c pathfence.Contained, phaseID, title string, l Ledger) error {
	spec, err := ScaffoldSpec(phaseID, title, l)
	if err != nil {
		return err
	}
	return spec.Save(c.String())
}
