package cmd

import (
	"fmt"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/spf13/cobra"
)

// This file is the other half of `dross milestone add`. Adding to a milestone's
// list fields was scriptable; correcting one was not — a mistyped phase slug, a
// success criterion that came out wrong, a non-goal that turned out to be a goal
// all meant hand-editing the toml, which is exactly the thing the dotted-path
// surface exists to avoid.
//
// Both verbs address entries BY VALUE rather than by index (locked
// remove_addressing). An index is a moving target: every removal shifts the ones
// after it, so a two-step "look, then edit" would routinely act on a different
// entry than the one the user read. The value is stable, and mirroring `add`'s
// signature means the inverse of an add is the same command line with one word
// changed.

// milestoneRemove deletes one entry from a list field, addressed by its exact
// value. A value that is not there is an error, never a silent success: `remove`
// is used to correct a mistake, and reporting a correction that did not happen
// is how the mistake survives into the next command.
func milestoneRemove() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [version] <list.path> <value>",
		Short: "Remove an exact value from a list field (version defaults to state.current_milestone)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			version, dotted, value := "", args[0], args[1]
			if len(args) == 3 {
				version, dotted, value = args[0], args[1], args[2]
			}
			m, path, err := loadMilestone(version)
			if err != nil {
				return err
			}
			if err := removeMilestoneListValue(m, dotted, value); err != nil {
				return err
			}
			return m.Save(path)
		},
	}
}

// milestoneReplace swaps one entry for another IN PLACE. It is not
// remove-then-add: `phases` is a delivery order, and rewording the third phase
// must not move it to the end of the roadmap.
func milestoneReplace() *cobra.Command {
	return &cobra.Command{
		Use:   "replace [version] <list.path> <old> <new>",
		Short: "Replace an exact value in a list field, keeping its position",
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(_ *cobra.Command, args []string) error {
			version, dotted, oldVal, newVal := "", args[0], args[1], args[2]
			if len(args) == 4 {
				version, dotted, oldVal, newVal = args[0], args[1], args[2], args[3]
			}
			m, path, err := loadMilestone(version)
			if err != nil {
				return err
			}
			if err := replaceMilestoneListValue(m, dotted, oldVal, newVal); err != nil {
				return err
			}
			return m.Save(path)
		},
	}
}

// milestoneListField hands back a pointer to the addressed slice, so remove and
// replace can edit it without each repeating the three-way switch. The bare and
// scope-prefixed spellings both resolve, exactly as they do for `add`.
func milestoneListField(m *milestone.Milestone, path string) (*[]string, error) {
	switch normalizeListField(path) {
	case "scope.success_criteria":
		return &m.Scope.SuccessCriteria, nil
	case "scope.non_goals":
		return &m.Scope.NonGoals, nil
	case "phases":
		return &m.Phases, nil
	}
	return nil, fmt.Errorf("not a list field %q — valid: scope.success_criteria, scope.non_goals, phases", path)
}

func removeMilestoneListValue(m *milestone.Milestone, path, value string) error {
	list, err := milestoneListField(m, path)
	if err != nil {
		return err
	}
	i := indexOfValue(*list, value)
	if i < 0 {
		return fmt.Errorf("no entry %q in %s — nothing removed", value, normalizeListField(path))
	}
	// Order-preserving delete. A swap-with-last would be cheaper and would
	// silently reorder `phases`, which is a delivery sequence.
	*list = append((*list)[:i:i], (*list)[i+1:]...)
	return nil
}

func replaceMilestoneListValue(m *milestone.Milestone, path, oldVal, newVal string) error {
	list, err := milestoneListField(m, path)
	if err != nil {
		return err
	}
	i := indexOfValue(*list, oldVal)
	if i < 0 {
		return fmt.Errorf("no entry %q in %s — nothing replaced", oldVal, normalizeListField(path))
	}
	(*list)[i] = newVal
	return nil
}

func indexOfValue(list []string, value string) int {
	for i, existing := range list {
		if existing == value {
			return i
		}
	}
	return -1
}
