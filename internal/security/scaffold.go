package security

import (
	"fmt"

	"github.com/Rivil/dross/internal/phase"
)

// ScaffoldSpec turns the surviving findings in a ledger into a remediation phase
// spec: one acceptance criterion per surviving finding, ordered criticals-first
// (severity drives wave order downstream), each criterion citing its findings.toml
// ledger id so no finding can hide behind a tier-level test. It refuses to build a
// spec from zero survivors — a vacuous remediation phase is never written.
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
// with the ledger id (the citation) and the contextual severity.
func criterionText(f Finding) string {
	what := f.Title
	if what == "" {
		what = f.Class
	}
	class := f.Class
	if class == "" {
		class = "finding"
	}
	return fmt.Sprintf("[%s] %s is blocked and a test proves it — %s (severity: %s)",
		f.ID, what, class, f.Severity)
}

// WriteScaffoldSpec builds the remediation spec from the ledger and writes it as
// TOML to path, using phase.Spec.Save so the output matches the repo's spec.toml
// formatting and round-trips through phase.LoadSpec.
func WriteScaffoldSpec(path, phaseID, title string, l Ledger) error {
	spec, err := ScaffoldSpec(phaseID, title, l)
	if err != nil {
		return err
	}
	return spec.Save(path)
}
