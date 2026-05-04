package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
)

func Milestone() *cobra.Command {
	c := &cobra.Command{
		Use:   "milestone",
		Short: "Manage milestones under .dross/milestones/",
	}
	c.AddCommand(
		milestoneList(),
		milestoneCreate(),
		milestoneShow(),
		milestoneGet(),
		milestoneSet(),
		milestoneAdd(),
	)
	return c
}

func milestoneList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			versions, err := milestone.List(root)
			if err != nil {
				return err
			}
			if len(versions) == 0 {
				Print("(no milestones)")
				return nil
			}
			for _, v := range versions {
				Print(v)
			}
			return nil
		},
	}
}

func milestoneCreate() *cobra.Command {
	return &cobra.Command{
		Use:   "create <version>",
		Short: "Create a new milestone (e.g. v0.1)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := milestone.FilePath(root, args[0])
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			m := &milestone.Milestone{
				Milestone: milestone.Meta{
					Version: args[0],
					Status:  "planning",
					Started: time.Now().UTC().Format("2006-01-02"),
				},
			}
			if err := m.Save(path); err != nil {
				return err
			}
			Printf("created %s\n", path)
			return nil
		},
	}
}

func milestoneShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <version>",
		Short: "Print a milestone toml",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := milestone.FilePath(root, args[0])
			m, err := milestone.Load(path)
			if err != nil {
				return err
			}
			Printf("# %s\n", path)
			return toml.NewEncoder(os.Stdout).Encode(m)
		},
	}
}

// milestoneGet prints a single dotted-path field
// (e.g. milestone.title, scope.success_criteria).
// Lists are printed one entry per line.
func milestoneGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <version> <dotted.path>",
		Short: "Read a single milestone field by dotted path",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			m, _, err := loadMilestone(args[0])
			if err != nil {
				return err
			}
			val, ok, list := readMilestoneDotted(m, args[1])
			if !ok {
				return fmt.Errorf("unknown milestone field: %s", args[1])
			}
			if list != nil {
				for _, v := range list {
					Print(v)
				}
				return nil
			}
			Print(val)
			return nil
		},
	}
}

// milestoneSet writes a scalar dotted-path field. Use `add` for list fields.
func milestoneSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <version> <dotted.path> <value>",
		Short: "Write a single scalar milestone field",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			m, path, err := loadMilestone(args[0])
			if err != nil {
				return err
			}
			if err := writeMilestoneDotted(m, args[1], args[2]); err != nil {
				return err
			}
			return m.Save(path)
		},
	}
}

// milestoneAdd appends a value to a list field (success_criteria, non_goals,
// phases). Idempotent — silently skips if the value is already present so the
// slash command can re-run safely.
func milestoneAdd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <version> <list.path> <value>",
		Short: "Append a value to a list field (success_criteria, non_goals, phases)",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			m, path, err := loadMilestone(args[0])
			if err != nil {
				return err
			}
			if err := appendMilestoneList(m, args[1], args[2]); err != nil {
				return err
			}
			return m.Save(path)
		},
	}
}

func loadMilestone(version string) (*milestone.Milestone, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, "", err
	}
	path := milestone.FilePath(root, version)
	m, err := milestone.Load(path)
	if err != nil {
		return nil, "", err
	}
	return m, path, nil
}

// readMilestoneDotted returns either a scalar string (val, true, nil) or a
// list ("", true, slice). Unknown path returns ("", false, nil).
func readMilestoneDotted(m *milestone.Milestone, path string) (string, bool, []string) {
	switch path {
	case "milestone.version":
		return m.Milestone.Version, true, nil
	case "milestone.title":
		return m.Milestone.Title, true, nil
	case "milestone.status":
		return m.Milestone.Status, true, nil
	case "milestone.started":
		return m.Milestone.Started, true, nil
	case "milestone.shipped":
		return m.Milestone.Shipped, true, nil
	case "scope.success_criteria":
		return "", true, m.Scope.SuccessCriteria
	case "scope.non_goals":
		return "", true, m.Scope.NonGoals
	case "phases":
		return "", true, m.Phases
	}
	return "", false, nil
}

func writeMilestoneDotted(m *milestone.Milestone, path, value string) error {
	switch path {
	case "milestone.version":
		m.Milestone.Version = value
	case "milestone.title":
		m.Milestone.Title = value
	case "milestone.status":
		m.Milestone.Status = value
	case "milestone.started":
		m.Milestone.Started = value
	case "milestone.shipped":
		m.Milestone.Shipped = value
	case "scope.success_criteria", "scope.non_goals", "phases":
		return fmt.Errorf("%s is a list — use `dross milestone add`", path)
	default:
		return fmt.Errorf("unknown or unsettable milestone field: %s", path)
	}
	return nil
}

func appendMilestoneList(m *milestone.Milestone, path, value string) error {
	switch path {
	case "scope.success_criteria":
		m.Scope.SuccessCriteria = appendUnique(m.Scope.SuccessCriteria, value)
	case "scope.non_goals":
		m.Scope.NonGoals = appendUnique(m.Scope.NonGoals, value)
	case "phases":
		m.Phases = appendUnique(m.Phases, value)
	default:
		return fmt.Errorf("not a list field (or unknown): %s", path)
	}
	return nil
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
