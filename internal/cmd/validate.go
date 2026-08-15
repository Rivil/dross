package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/state"
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
				problems = append(problems, enumProblems(p)...)
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

			// Valid deferred-target slugs: any existing phase dir, or any slug
			// parked in a milestone's phases array. A target outside this set is
			// dangling — it would silently break the 1:1 re-surface it routes to.
			// The set is built by the same helper `deferred route --target` and
			// `deferred add --target` gate on, so a target the CLI accepts can
			// never be one validate calls dangling (locked target_validation).
			validTargets := deferredTargetSet(root)

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
				if spec != nil {
					problems = append(problems, danglingTargets(specPath, spec, validTargets)...)
				}
			}

			// The reserved project-store slug must never be a phase directory:
			// two sources would share the slug, making `_project 0` name two
			// different items. `deferred list` skips it; validate says why.
			for _, id := range phaseIDs {
				if id == projectStoreSlug {
					problems = append(problems, fmt.Sprintf("%s: %q is reserved for the project-level deferred store — rename the phase directory", filepath.Join("phases", id), projectStoreSlug))
				}
			}

			// The project store carries routed items too, and is hand-editable
			// like any spec, so it gets the same dangling-target walk.
			storePath := filepath.Join(root, "deferred.toml")
			if _, err := os.Stat(storePath); err == nil {
				if store, err := phase.LoadSpec(storePath); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %v", storePath, err))
				} else {
					problems = append(problems, danglingTargets(storePath, store, validTargets)...)
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

// enumProblems reports every enum-valued project.toml key holding a value its
// set does not accept.
//
// This is the second of the two gates the enum_enforcement_point decision
// requires, and it reads the SAME enumKeys table `project set` refuses on
// (project.go) — not a restated list. Set-time rejection only ever sees values
// a human typed at the CLI; project.toml is a tracked file that gets
// hand-edited, cloned and carried forward across versions, so a value that
// never passed through the setter would otherwise go unchecked forever. That
// was most of them.
//
// An EMPTY value is never reported here. Empty means unset, every optional
// key's absence is legitimate, and the one key where empty is itself a problem
// (runtime.mode) is reported by its own check above — folding it in here would
// report it twice and word it worse.
//
// Keys are walked in sorted order so a project.toml with several bad values
// produces the same problem list every run.
func enumProblems(p *project.Project) []string {
	keys := make([]string, 0, len(enumKeys))
	for k := range enumKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var problems []string
	for _, k := range keys {
		v, ok := readDotted(p, k)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if set := enumKeys[k]; !set.Has(v) {
			// Both the key and the offending value, then what IS allowed.
			// "invalid value" alone sends the reader to the source.
			problems = append(problems, fmt.Sprintf("project.toml: %s = %q is not a valid value (%s)", k, v, set.List()))
		}
	}
	return problems
}

// danglingTargets reports every [[deferred]] target in one source that names no
// valid destination. Shared by the phase-spec walk and the project store so both
// are judged by exactly the same rule and reported in the same shape.
func danglingTargets(path string, spec *phase.Spec, valid map[string]bool) []string {
	var problems []string
	for _, d := range spec.Deferred {
		if d.Target != "" && !valid[d.Target] {
			problems = append(problems, fmt.Sprintf("%s: deferred target %q names no phase dir or milestone.phases entry", path, d.Target))
		}
	}
	return problems
}

// loadIfExists skips missing files quietly.
func loadIfExists(path string, load func() (any, error)) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	return load()
}
