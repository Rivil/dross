package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/facts"
	"github.com/Rivil/dross/internal/phase"
)

// What a facts run checked, as the clean line and --json's `checked` name it.
const (
	factsCheckedSpec     = "spec only"
	factsCheckedSpecPlan = "spec + plan"
)

// factsRow is one unresolved citation in `dross phase facts --json`. Line is
// the finding exactly as the plain output prints it (Finding.String()), so a
// prompt or the plan-lock gate can quote the command line for line; NewLine is
// the line a moved symbol now lives at, 0 for every other kind.
type factsRow struct {
	Field    string `json:"field"`
	Kind     string `json:"kind"`
	Citation string `json:"citation"`
	Line     string `json:"line"`
	NewLine  int    `json:"new_line"`
}

// factsReport is `dross phase facts --json`. Unresolved is never nil, so a
// clean phase encodes it as [] rather than null. Degraded says the symbol
// index could not cover the tree and unmatched symbols were skipped, not
// reported; DegradedWhy names the first file that caused it.
type factsReport struct {
	Phase       string     `json:"phase"`
	Checked     string     `json:"checked"`
	Unresolved  []factsRow `json:"unresolved"`
	Degraded    bool       `json:"degraded"`
	DegradedWhy string     `json:"degraded_why,omitempty"`
}

// phaseFacts reports every citation in a phase's spec and plan that the tree
// does not back: a missing path, a file:line whose symbol moved or is gone, an
// unresolved symbol. The rules are facts.Check's, which the plan-lock gate runs
// too, so the two cannot disagree.
//
// Absence is never clean: an unknown phase, an id that escapes phases/, a
// phase with no spec.toml and a spec or plan that does not decode each exit
// non-zero naming what is wrong. A phase with no plan.toml yet checks its spec
// alone. Any finding exits non-zero. The command only reads.
func phaseFacts() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "facts <phase-id>",
		Short: "Report file paths, file:line citations and symbols a phase's spec or plan cites that the tree does not back",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			id := args[0]
			spec, plan, err := loadFactsPhase(root, id)
			if err != nil {
				return err
			}
			rep, err := facts.Check(filepath.Dir(root), spec, plan)
			if err != nil {
				return fmt.Errorf("check phase %s: %w", id, err)
			}
			r := buildFactsReport(id, factsChecked(plan), rep)
			if asJSON {
				if err := emitJSON(r); err != nil {
					return err
				}
			} else {
				printFacts(r)
			}
			if n := len(r.Unresolved); n > 0 {
				return fmt.Errorf("%d unresolved citation(s) in phase %s (checked %s)", n, id, r.Checked)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false,
		"emit the report as JSON {phase, checked, unresolved, degraded} (for /dross-plan)")
	return c
}

// loadFactsPhase reads the spec and plan a facts check runs over. The spec is
// required: its absence is an error wrapping fs.ErrNotExist and naming the
// path. The plan is optional and nil when absent. A file that exists but does
// not decode is an error naming it — reporting the phase clean would hide it.
// An id that would escape phases/ is refused before anything is read.
func loadFactsPhase(root, id string) (*phase.Spec, *phase.Plan, error) {
	if _, err := phase.ContainID(root, id); err != nil {
		return nil, nil, fmt.Errorf("phase id %q refused: %w", id, err)
	}
	dir := phase.Dir(root, id)
	if !isDir(dir) {
		return nil, nil, fmt.Errorf("unknown phase %q — no directory at %s (run `dross phase list` to see ids)", id, dir)
	}
	specPath := filepath.Join(dir, "spec.toml")
	if _, err := os.Stat(specPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("phase %q has no spec at %s, so there is nothing to check: %w", id, specPath, fs.ErrNotExist)
		}
		return nil, nil, fmt.Errorf("stat %s: %w", specPath, err)
	}
	spec, err := phase.LoadSpec(specPath)
	if err != nil {
		return nil, nil, err
	}
	planPath := filepath.Join(dir, "plan.toml")
	if _, err := os.Stat(planPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return spec, nil, nil
		}
		return nil, nil, fmt.Errorf("stat %s: %w", planPath, err)
	}
	plan, err := phase.LoadPlan(planPath)
	if err != nil {
		return nil, nil, err
	}
	return spec, plan, nil
}

// factsChecked names what a run covered: the spec alone until a plan exists.
func factsChecked(plan *phase.Plan) string {
	if plan == nil {
		return factsCheckedSpec
	}
	return factsCheckedSpecPlan
}

// buildFactsReport assembles the --json document; the plain output prints
// from it too, so the two render every finding identically.
func buildFactsReport(id, checked string, rep facts.Report) factsReport {
	r := factsReport{
		Phase:       id,
		Checked:     checked,
		Unresolved:  make([]factsRow, 0, len(rep.Unresolved)),
		Degraded:    rep.Degraded,
		DegradedWhy: rep.DegradedWhy,
	}
	for _, f := range rep.Unresolved {
		r.Unresolved = append(r.Unresolved, factsRow{
			Field: f.Field, Kind: f.Kind, Citation: f.Citation,
			Line: f.String(), NewLine: f.NewLine,
		})
	}
	return r
}

// printFacts renders one line per finding, or the clean line naming what was
// checked. A degraded index is said either way: a clean degraded run has not
// checked every symbol, and a reader must not take it for a full pass.
func printFacts(r factsReport) {
	if len(r.Unresolved) == 0 {
		Printf("nothing unresolved — checked %s\n", r.Checked)
	}
	for _, row := range r.Unresolved {
		Print(row.Line)
	}
	if r.Degraded {
		Printf("symbol checks were skipped for unmatched symbols — %s\n", r.DegradedWhy)
	}
}
