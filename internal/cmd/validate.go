package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rivil/dross/internal/phase"
	"github.com/rivil/dross/internal/project"
	"github.com/rivil/dross/internal/rules"
	"github.com/rivil/dross/internal/state"
)

// Validate runs structural checks on every dross artefact in the repo.
//
// v0 checks:
//   - project.toml decodes; required fields present (project.name, project.version)
//   - state.json decodes
//   - rules.toml decodes
//   - each phases/NN-slug/{spec,plan}.toml decodes (if present)
//   - phase id in plan.toml matches dir name
//   - plan.task[].covers references criteria that exist in spec.toml
func Validate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Schema-check every dross artefact",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			var problems []string

			// project.toml
			p, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				problems = append(problems, fmt.Sprintf("project.toml: %v", err))
			} else {
				if p.Project.Name == "" {
					problems = append(problems, "project.toml: project.name is empty")
				}
				if p.Project.Version == "" {
					problems = append(problems, "project.toml: project.version is empty")
				}
				if p.Runtime.Mode == "" {
					problems = append(problems, "project.toml: runtime.mode is empty (docker | native | hybrid)")
				}
			}

			// state.json
			if _, err := state.Load(filepath.Join(root, state.File)); err != nil {
				problems = append(problems, fmt.Sprintf("state.json: %v", err))
			}

			// rules.toml (optional)
			if _, err := rules.LoadFile(filepath.Join(root, rules.File)); err != nil {
				problems = append(problems, fmt.Sprintf("rules.toml: %v", err))
			}

			// phases
			phaseIDs, err := phase.List(root)
			if err != nil {
				problems = append(problems, fmt.Sprintf("phases: %v", err))
			}
			for _, id := range phaseIDs {
				dir := phase.Dir(root, id)
				specPath := filepath.Join(dir, "spec.toml")
				planPath := filepath.Join(dir, "plan.toml")
				var spec *phase.Spec
				if _, err := loadIfExists(specPath, func() (any, error) { s, err := phase.LoadSpec(specPath); spec = s; return s, err }); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %v", specPath, err))
				}
				var plan *phase.Plan
				if _, err := loadIfExists(planPath, func() (any, error) { p, err := phase.LoadPlan(planPath); plan = p; return p, err }); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %v", planPath, err))
				}
				if plan != nil && !strings.HasPrefix(id, plan.Phase.ID) && id != plan.Phase.ID {
					problems = append(problems, fmt.Sprintf("%s: plan.phase.id (%s) does not match directory (%s)", planPath, plan.Phase.ID, id))
				}
				if spec != nil && plan != nil {
					ids := map[string]bool{}
					for _, c := range spec.Criteria {
						ids[c.ID] = true
					}
					for _, t := range plan.Task {
						for _, cov := range t.Covers {
							if !ids[cov] {
								problems = append(problems, fmt.Sprintf("%s task %s covers unknown criterion %s", planPath, t.ID, cov))
							}
						}
					}
				}
			}

			if len(problems) == 0 {
				Print("✓ all dross artefacts valid")
				return nil
			}
			for _, p := range problems {
				Printf("✗ %s\n", p)
			}
			return fmt.Errorf("%d problem(s) found", len(problems))
		},
	}
}

// loadIfExists skips missing files quietly.
func loadIfExists(path string, load func() (any, error)) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	return load()
}
