package cmd

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/facts"
	"github.com/Rivil/dross/internal/phase"
)

// phaseMarkNew records citations a phase introduces in its spec's top-level
// `new` list (the locked new_names decision), the "confirm it is new" arm of
// /dross-plan's c-4 walk. Each argument is read the way the fact check reads
// prose, so wrapping punctuation never reaches the list: `foo.go:12.` is
// stored as foo.go:12.
//
// Only a citation `dross phase facts` currently reports may be marked: one it
// does not report (a typo, an existing file, a citation the phase never
// makes) is refused, so the list can never silence anything but a real
// finding. One already in the list is a no-op. Every argument is checked
// before anything is written; any refusal leaves the spec untouched.
func phaseMarkNew() *cobra.Command {
	return &cobra.Command{
		Use:   "mark-new <phase-id> <citation>...",
		Short: "Record citations a phase introduces in its spec's new list, so `dross phase facts` stops reporting them",
		Args:  cobra.MinimumNArgs(2),
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

			type candidate struct {
				arg, entry string
				cite       facts.Citation
			}
			listed := toSet(spec.New)
			var adds []candidate
			var refused, already []string
			for _, arg := range args[1:] {
				c, err := markNewCitation(arg)
				if err != nil {
					refused = append(refused, err.Error())
					continue
				}
				entry, err := markNewEntry(c)
				if err != nil {
					return err
				}
				if listed[entry] {
					already = append(already, entry)
					continue
				}
				listed[entry] = true
				adds = append(adds, candidate{arg, entry, c})
			}

			if len(adds) > 0 {
				rep, err := facts.Check(filepath.Dir(root), spec, plan)
				if err != nil {
					return fmt.Errorf("check phase %s: %w", id, err)
				}
				for _, a := range adds {
					covers, err := markNewCoversAny(a.cite, rep.Unresolved)
					if err != nil {
						return fmt.Errorf("check phase %s: %w", id, err)
					}
					if !covers {
						refused = append(refused, fmt.Sprintf("%q is not an unresolved citation in phase %s", a.arg, id))
					}
				}
			}
			if len(refused) > 0 {
				return fmt.Errorf("mark-new refused, nothing written (run `dross phase facts %s` for what is unresolved):\n  %s",
					id, strings.Join(refused, "\n  "))
			}

			for _, e := range already {
				Printf("already new: %s\n", e)
			}
			if len(adds) == 0 {
				return nil
			}
			specPath := filepath.Join(phase.Dir(root, id), "spec.toml")
			for _, a := range adds {
				spec.New = append(spec.New, a.entry)
			}
			if err := spec.Save(specPath); err != nil {
				return fmt.Errorf("write %s: %w", specPath, err)
			}
			for _, a := range adds {
				Printf("marked new: %s\n", a.entry)
			}
			return nil
		},
	}
}

// markNewCitation reads one argument as exactly one citation, by the same
// facts.Extract the check scans prose with. A bare symbol has no backticks on
// a command line, so an argument holding no citation as written is read again
// as a backtick span. An argument that is no citation, or holds several, is
// an error naming it.
func markNewCitation(arg string) (facts.Citation, error) {
	cs := facts.Extract(arg)
	if len(cs) == 0 {
		cs = facts.Extract("`" + strings.Trim(strings.TrimSpace(arg), "`") + "`")
	}
	switch len(cs) {
	case 0:
		return facts.Citation{}, fmt.Errorf("%q is not a path, file:line or symbol citation", arg)
	case 1:
		return cs[0], nil
	}
	return facts.Citation{}, fmt.Errorf("%q holds %d citations; pass each as its own argument", arg, len(cs))
}

// markNewEntry is the `new` list form of a citation: a cleaned path, a
// cleaned path:line (any paired symbol dropped — the line is what is new), or
// the symbol as cited. A citation kind it does not know is an error, so a
// kind added to facts fails here rather than storing an entry nothing reads.
func markNewEntry(c facts.Citation) (string, error) {
	switch c.Kind {
	case facts.KindPath:
		return path.Clean(c.Path), nil
	case facts.KindLine:
		return path.Clean(c.Path) + ":" + strconv.Itoa(c.Line), nil
	case facts.KindSymbol:
		return c.Raw, nil
	}
	return "", fmt.Errorf("mark-new: citation %q has kind %q, which has no new-list form", c.Raw, c.Kind)
}

// markNewCoversAny reports whether recording c as new suppresses at least one
// of the findings — the facts check's own covering rule: a path covers that
// path and any file:line in it, a file:line covers that line, a symbol covers
// that symbol.
//
// Every finding's Citation is the canonical form facts.Extract produced, so it
// re-reads as exactly one citation. One that does not is a broken invariant,
// returned as an error: skipping it would tell the user their citation is not
// unresolved when the fault is ours.
func markNewCoversAny(c facts.Citation, findings []facts.Finding) (bool, error) {
	for _, f := range findings {
		fc, err := markNewCitation(f.Citation)
		if err != nil {
			return false, fmt.Errorf("finding %s does not re-read as one citation: %w", f.String(), err)
		}
		switch c.Kind {
		case facts.KindPath:
			if (fc.Kind == facts.KindPath || fc.Kind == facts.KindLine) && path.Clean(fc.Path) == path.Clean(c.Path) {
				return true, nil
			}
		case facts.KindLine:
			if fc.Kind == facts.KindLine && path.Clean(fc.Path) == path.Clean(c.Path) && fc.Line == c.Line {
				return true, nil
			}
		case facts.KindSymbol:
			if fc.Kind == facts.KindSymbol && fc.Symbol == c.Symbol {
				return true, nil
			}
		}
	}
	return false, nil
}
