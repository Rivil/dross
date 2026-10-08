package diag

import (
	"slices"

	"github.com/Rivil/dross/internal/milestone"
)

// Duplicate is one slug and every milestone roadmap listing it.
type Duplicate struct {
	Slug     string
	Versions []string
}

// RoadmapDuplicates names every slug on more than one milestone's phases
// array, in the order those slugs are first listed, each with the versions
// carrying it — the first of which is the position `dross phase list` renders
// it at (phase.Ordered keeps the first occurrence).
//
// A milestone that fails to load is skipped, matching milestonePhaseOrder: this
// is a finding, never a hard dependency.
func RoadmapDuplicates(root string) []Duplicate {
	versions, err := milestone.List(root)
	if err != nil {
		return nil
	}
	on := map[string][]string{}
	var order []string
	for _, v := range versions {
		m, err := milestone.Load(milestone.FilePath(root, v))
		if err != nil {
			continue
		}
		for _, slug := range m.Phases {
			if len(on[slug]) == 0 {
				order = append(order, slug)
			}
			// A slug repeated inside ONE array is still one roadmap: what this
			// reports is the same phase claimed by two milestones.
			if !slices.Contains(on[slug], v) {
				on[slug] = append(on[slug], v)
			}
		}
	}
	var out []Duplicate
	for _, slug := range order {
		if len(on[slug]) > 1 {
			out = append(out, Duplicate{Slug: slug, Versions: on[slug]})
		}
	}
	return out
}
