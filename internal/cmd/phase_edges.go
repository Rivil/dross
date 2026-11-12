package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/phase"
)

// edgeClassRow is one edge class with the hint it is proposed under.
type edgeClassRow struct {
	Class string `json:"class"`
	Hint  string `json:"hint"`
}

// edgesReport is `dross phase edges --json`. /dross-spec reads it to choose
// which classes to propose, so the hints live here rather than in spec.md:
// Classes is always all eight in phase.EdgeClasses order, built from that list
// and never from Open, so it survives a spec where every class is resolved.
// Every list is non-nil — an absent one encodes as [] rather than null.
type edgesReport struct {
	Classes    []edgeClassRow `json:"classes"`
	Resolved   []string       `json:"resolved"`
	Unresolved []string       `json:"unresolved"`
	Open       []edgeClassRow `json:"open"`
}

// phaseEdges reports where a phase's spec stands on each edge class: resolved,
// unresolved, or open (no record yet). The verdicts are phase.EdgeStatus and
// phase.EdgeProblems — the rule set validate and the plan-lock gate share —
// so this report cannot call a record resolved that either would refuse.
//
// Absence is not an error: a phase with no spec.toml, or a legacy spec with no
// [[edges]], reports all eight classes open. An unknown phase id is.
func phaseEdges() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "edges <phase-id>",
		Short: "Report which edge classes a phase's spec has resolved, left unresolved or not yet probed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			dir := phase.Dir(root, args[0])
			if !isDir(dir) {
				return fmt.Errorf("unknown phase %q — no directory at %s (run `dross phase list` to see ids)", args[0], dir)
			}
			specPath := filepath.Join(dir, "spec.toml")
			spec, err := loadEdgeSpec(specPath)
			if err != nil {
				return err
			}
			known, err := edgeDeferredIDs(root, specPath, spec)
			if err != nil {
				return err
			}
			resolved, unresolved, open := phase.EdgeStatus(spec, known)
			if asJSON {
				return emitJSON(buildEdgesReport(resolved, unresolved, open))
			}
			printEdges(specPath, spec, known, resolved, unresolved)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON {classes, resolved, unresolved, open} (for /dross-spec)")
	return c
}

// loadEdgeSpec reads specPath, standing in an empty spec when the file does
// not exist: a phase scaffolded but not yet specced has every class open. A
// spec that exists and fails to decode is an error — reporting it all-open
// would invite /dross-spec to re-probe classes it already recorded.
func loadEdgeSpec(specPath string) (*phase.Spec, error) {
	if _, err := os.Stat(specPath); errors.Is(err, fs.ErrNotExist) {
		return &phase.Spec{}, nil
	}
	return phase.LoadSpec(specPath)
}

// edgeDeferredIDs gathers the project-wide [[deferred]] ids a deferred edge is
// judged against — every phase spec plus the _project store. As in the
// plan-lock gate, only a spec with a deferred edge pays for it, so a broken
// file in an unrelated phase cannot break this report; when one does matter
// the report fails rather than calling the edge dangling.
func edgeDeferredIDs(root, specPath string, spec *phase.Spec) (map[string]bool, error) {
	known := map[string]bool{}
	for _, e := range spec.Edges {
		if e.Outcome != phase.EdgeDeferred {
			continue
		}
		entries, err := deferred.Collect(root)
		if err != nil {
			return nil, fmt.Errorf("cannot gather [[deferred]] ids to judge %s's deferred edges: %w", specPath, err)
		}
		for _, d := range entries {
			if d.ID != "" {
				known[d.ID] = true
			}
		}
		break
	}
	return known, nil
}

// buildEdgesReport assembles the --json document from EdgeStatus' partition.
func buildEdgesReport(resolved, unresolved, open []string) edgesReport {
	r := edgesReport{
		Classes:    make([]edgeClassRow, 0, len(phase.EdgeClasses)),
		Resolved:   resolved,
		Unresolved: unresolved,
		Open:       []edgeClassRow{},
	}
	hints := map[string]string{}
	for _, c := range phase.EdgeClasses {
		r.Classes = append(r.Classes, edgeClassRow{Class: c.Name, Hint: c.Hint})
		hints[c.Name] = c.Hint
	}
	for _, name := range open {
		r.Open = append(r.Open, edgeClassRow{Class: name, Hint: hints[name]})
	}
	return r
}

// printEdges renders every class in EdgeClasses order — its status line, then
// its hint — and then any record naming a class outside the set, which
// EdgeStatus places in no partition and would otherwise vanish.
func printEdges(specPath string, spec *phase.Spec, known map[string]bool, resolved, unresolved []string) {
	problems := edgeProblemsByRecord(spec, known)
	isResolved := toSet(resolved)
	isUnresolved := toSet(unresolved)
	knownClass := map[string]bool{}

	Printf("# edges — %s\n", specPath)
	for _, c := range phase.EdgeClasses {
		knownClass[c.Name] = true
		var status string
		switch {
		case isResolved[c.Name]:
			// A resolved class has exactly one record: a second would be a
			// duplicate, which leaves it unresolved.
			status = edgeOutcome(spec, c.Name) + " (resolved)"
		case isUnresolved[c.Name]:
			var details []string
			for i, e := range spec.Edges {
				if e.Class == c.Name && problems[i] != "" {
					details = append(details, fmt.Sprintf("edges[%d]: %s", i, problems[i]))
				}
			}
			status = "unresolved: " + strings.Join(details, "; ")
		default:
			status = "open"
		}
		Printf("%s: %s\n  %s\n", c.Name, status, c.Hint)
	}

	var unknown []string
	for i, e := range spec.Edges {
		if !knownClass[e.Class] {
			unknown = append(unknown, fmt.Sprintf("  edges[%d] (%s): %s", i, e.Class, problems[i]))
		}
	}
	if len(unknown) > 0 {
		Print("unknown class records:")
		for _, l := range unknown {
			Print(l)
		}
	}
}

// edgeProblemsByRecord maps each unresolved record's index to its defects, as
// phase.EdgeProblems words them, minus the `edges[i] (<class>): ` prefix it
// leads each line with. The prefix is rebuilt per record, so the match is
// exact even for a class name carrying odd characters.
func edgeProblemsByRecord(spec *phase.Spec, known map[string]bool) map[int]string {
	lines := phase.EdgeProblems(spec, known)
	out := map[int]string{}
	for i, e := range spec.Edges {
		prefix := fmt.Sprintf("edges[%d] (%s): ", i, e.Class)
		for _, l := range lines {
			if strings.HasPrefix(l, prefix) {
				out[i] = strings.TrimPrefix(l, prefix)
				break
			}
		}
	}
	return out
}

// edgeOutcome is the outcome of the first record naming class.
func edgeOutcome(spec *phase.Spec, class string) string {
	for _, e := range spec.Edges {
		if e.Class == class {
			return e.Outcome
		}
	}
	return ""
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}
