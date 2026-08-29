package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/configenum"
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
					// Interpolated from the Set rather than typed out, so this
					// message cannot name a value the code no longer accepts —
					// which is exactly what it did while it offered hybrid.
					problems = append(problems, fmt.Sprintf("project.toml: runtime.mode is empty (%s)", configenum.RuntimeModes.List()))
				}
				problems = append(problems, enumProblems(p)...)
				problems = append(problems, laneProblems(p)...)
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

// laneProblems reports every structural fault in the [[runtime.test_lane]]
// blocks. A lane is three required parts and nothing about the toml decoder
// enforces any of them: a missing key decodes to a zero value, so an
// unvalidated lane is a lane that fails later, at the moment a gate wanted to
// run something.
//
// Each fault is reported on its own line, against the lane it belongs to, so a
// project.toml with several half-written lanes tells the user everything that
// is wrong in one pass rather than one fault per run.
//
// Nothing is reported for a repo declaring no lane: lanes are opt-in (the
// bare_test_run decision), and a validator that invented a problem for every
// existing repo would make an opt-in feature mandatory by nagging.
func laneProblems(p *project.Project) []string {
	var problems []string
	seen := map[string]bool{}
	for i, lane := range p.Runtime.TestLane {
		label := laneLabel(i, lane.Name)
		if strings.TrimSpace(lane.Name) == "" {
			// Reported by ordinal, never as the empty string: the consent
			// store is keyed by lane name, so a nameless lane can never be
			// granted, and `runtime.test_lane ""` would not tell the user
			// which of several blocks to go and fix.
			problems = append(problems, fmt.Sprintf("project.toml: %s has no name — a lane is granted consent by name, so every lane needs one", label))
		} else if seen[lane.Name] {
			// Two lanes under one name collapse to a single entry in the
			// name-keyed grant store, so one lane's grant would silently
			// authorize the other lane's command.
			problems = append(problems, fmt.Sprintf("project.toml: %s is declared more than once — lane names key the machine-local consent store, so they must be unique", label))
		} else {
			seen[lane.Name] = true
		}
		if len(lane.Match) == 0 {
			problems = append(problems, fmt.Sprintf("project.toml: %s has an empty match list — a lane matching no path can never be selected", label))
		}
		if strings.TrimSpace(lane.Command) == "" {
			problems = append(problems, fmt.Sprintf("project.toml: %s has no command — a lane with no command line has nothing to run and nothing to consent to", label))
		}
		if lane.Prepare != "" && strings.TrimSpace(lane.Prepare) == "" {
			// Not a reading of any locked decision but an addition: a
			// whitespace-only prepare is the one shape that disagrees with
			// itself. It survives project.Load non-empty, so the consent
			// fingerprint covers it and `dross trust --lane` prints a blank
			// line, while every reader that asks "does this lane declare a
			// prepare" trims it and says no. `dross test lane add --prepare`
			// normalizes it away, so only a hand-edited project.toml can
			// carry one — which is exactly what validate is for.
			problems = append(problems, fmt.Sprintf("project.toml: %s has a whitespace-only prepare — it reads as no prepare but fingerprints as one; drop the key or give it a command line", label))
		}
		problems = append(problems, laneSelectorProblems(label, lane)...)
		problems = append(problems, laneToolchainProblems(label, lane)...)
		for _, pattern := range lane.Match {
			if err := checkGlob(pattern); err != nil {
				// Named by pattern as well as by lane: a broken glob
				// otherwise presents as a file set that never matches, which
				// reads as a file-set problem rather than a lane that cannot
				// compile.
				problems = append(problems, fmt.Sprintf("project.toml: %s match pattern %q does not compile: %v", label, pattern, err))
			}
		}
	}
	return problems
}

// laneSelectorProblems reports the faults in one lane's opt-in selector fields.
//
// It is split out of laneProblems so the selector rules read as one paragraph:
// a style dross cannot translate, an exit code that would mean the wrong thing,
// and a code declared on a lane that can never produce it.
func laneSelectorProblems(label string, lane project.TestLane) []string {
	var problems []string
	// The empty case is guarded here rather than left to Has: SelectorStyles
	// has no code default, so Has("") is false, and an omitted selector is the
	// pre-selector behaviour every existing lane already relies on.
	if strings.TrimSpace(lane.Selector) != "" && !configenum.SelectorStyles.Has(lane.Selector) {
		problems = append(problems, fmt.Sprintf("project.toml: %s selector = %q is not a selector style — expected %s", label, lane.Selector, configenum.SelectorStyles.List()))
	}
	for _, code := range lane.EmptyExit {
		switch {
		case code == 0:
			// A lane claiming 0 means "collected nothing" would report every
			// green run as a miss, which is the one outcome that must never
			// be silently swallowed.
			problems = append(problems, fmt.Sprintf("project.toml: %s empty_exit lists 0 — 0 is the runner's success code, so a lane declaring it would report every passing run as collecting no tests", label))
		case code == 255:
			// internal/remote spends 255 on ssh transport failure, so a lane
			// claiming it would report an unreachable host as "no tests" —
			// a run that never happened dressed up as a run that found none.
			problems = append(problems, fmt.Sprintf("project.toml: %s empty_exit lists 255 — 255 is ssh's transport-failure code, so a lane declaring it would report an unreachable host as collecting no tests", label))
		case code < 0 || code > 255:
			problems = append(problems, fmt.Sprintf("project.toml: %s empty_exit lists %d — a process exit code is 0-255, so no runner can ever return it", label, code))
		}
	}
	if len(lane.EmptyExit) > 0 && strings.TrimSpace(lane.Selector) == "" {
		// Not a reading of the locked empty_detection decision but an addition
		// to it: without a selector the lane always runs its whole suite, so
		// the declared code can never fire and the user is left believing they
		// configured something they did not.
		problems = append(problems, fmt.Sprintf("project.toml: %s declares empty_exit with no selector — an unscoped lane runs its whole suite, so the code could never fire", label))
	}
	return problems
}

// laneToolchainProblems reports the faults in one lane's opt-in toolchain
// override.
//
// Every entry is asked of a host as `command -v <entry>`, so the rules here are
// all one rule: an entry that could never resolve on any host would send its
// lane local on every future run — or refuse to spawn it at all — with nothing
// in the transcript pointing at the override as the cause. Refusing the shape
// up front is the only place that failure is legible.
//
// Nothing is reported for a lane that omits the key. The list is derived then
// (the locked toolchain_source decision), and a validator inventing a problem
// for every lane written before this phase would make an opt-in field
// mandatory by nagging.
func laneToolchainProblems(label string, lane project.TestLane) []string {
	var problems []string
	for _, tool := range lane.Toolchain {
		switch {
		case strings.TrimSpace(tool) == "":
			problems = append(problems, fmt.Sprintf("project.toml: %s toolchain lists a blank entry — every entry is probed as `command -v <tool>`, and no host answers to the empty string", label))
		case len(strings.Fields(tool)) > 1:
			// A whole command line rather than a binary: the shape a user
			// reaches for when they copy the lane's command into the override.
			problems = append(problems, fmt.Sprintf("project.toml: %s toolchain lists %q — an entry is one binary name, not a command line; list the binary alone", label, tool))
		case strings.Contains(tool, "="):
			// `FOO=1` is what first-token derivation produces for an
			// env-prefixed command, and it is exactly what the override exists
			// to REPLACE — carrying it into the override reproduces the fault.
			problems = append(problems, fmt.Sprintf("project.toml: %s toolchain lists %q — that is an environment assignment, not a binary; name the binary the line actually runs", label, tool))
		case strings.ContainsAny(tool, `/\`):
			// A path resolves against a working directory, and the whole point
			// of the probe is that it is asked of two different hosts whose
			// trees do not have to agree. Only a name looked up on PATH means
			// the same question on both.
			problems = append(problems, fmt.Sprintf("project.toml: %s toolchain lists %q — an entry is looked up on PATH on whichever host runs the lane, so it must be a bare binary name, not a path", label, tool))
		}
	}
	return problems
}

// laneLabel names a lane for a problem line: by name when it has one, by
// ordinal when it does not, so every problem points at a specific block.
func laneLabel(i int, name string) string {
	if strings.TrimSpace(name) == "" {
		return fmt.Sprintf("runtime.test_lane[%d]", i)
	}
	return fmt.Sprintf("runtime.test_lane %q", name)
}

// checkGlob reports whether one lane pattern is syntactically well-formed.
//
// filepath.Match validates the WHOLE pattern even once the comparison has
// failed, so matching against a throwaway name is a syntax check: only
// ErrBadPattern comes back, a plain non-match returns nil.
func checkGlob(pattern string) error {
	_, err := filepath.Match(pattern, "x")
	return err
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
