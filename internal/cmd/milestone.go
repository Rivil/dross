package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
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

			// Cut + push the milestone integration branch as an
			// unconditional side effect of scoping (the v0.7 branch
			// topology). Skips silently in a non-git dir so `dross init`
			// flows and non-git usage keep working.
			mainBranch := "main"
			if p, _, err := loadProject(); err == nil && p.Repo.GitMainBranch != "" {
				mainBranch = p.Repo.GitMainBranch
			}
			branch, created, pushed, err := ensureMilestoneBranch(filepath.Dir(root), mainBranch, args[0])
			if err != nil {
				return err
			}
			if created {
				Printf("cut %s from %s\n", branch, mainBranch)
			}
			if pushed {
				Printf("pushed %s to origin\n", branch)
			}
			return nil
		},
	}
}

// ensureMilestoneBranch cuts milestone/<version> from the main branch (without
// checking it out — HEAD stays put) and pushes it to origin, so the integration
// branch exists as an unconditional side effect of scoping. Idempotent: an
// existing local ref is left as-is (re-scope no-ops rather than erroring) and
// the push is a no-op when origin already carries the ref at the same commit.
// Skips silently when the repo has no git, no main ref to cut from yet, or no
// origin remote — scoping must still succeed in those cases.
func ensureMilestoneBranch(repoDir, mainBranch, version string) (branch string, created, pushed bool, err error) {
	branch = "milestone/" + version
	if !isDir(filepath.Join(repoDir, ".git")) {
		return branch, false, false, nil
	}
	// Need a main ref to cut from; a repo with no commits has none.
	if gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+mainBranch) != nil {
		return branch, false, false, nil
	}
	// Idempotent create: only when the local ref is absent.
	if gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+branch) != nil {
		if out, e := gitCombined(repoDir, "branch", branch, mainBranch); e != nil {
			return branch, false, false, fmt.Errorf("git branch %s %s: %w\n%s", branch, mainBranch, e, out)
		}
		created = true
	}
	// Push only when an origin remote exists.
	if gitNoOut(repoDir, "remote", "get-url", "origin") == nil {
		if out, e := gitCombined(repoDir, "push", "origin", branch); e != nil {
			return branch, created, false, fmt.Errorf("git push origin %s: %w\n%s", branch, e, out)
		}
		pushed = true
	}
	return branch, created, pushed, nil
}

func milestoneShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show [version]",
		Short: "Print a milestone toml (defaults to state.current_milestone)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			version := ""
			if len(args) == 1 {
				version = args[0]
			} else {
				s, err := state.Load(filepath.Join(root, state.File))
				if err != nil {
					return fmt.Errorf("no version given and load state: %w", err)
				}
				version = s.CurrentMilestone
				if version == "" {
					return errors.New("no version given and state has no current_milestone; run `dross milestone list` to see options")
				}
			}
			path := milestone.FilePath(root, version)
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
		Use:   "get [version] <dotted.path>",
		Short: "Read a single milestone field by dotted path (version defaults to state.current_milestone)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			version, dotted := "", args[0]
			if len(args) == 2 {
				version, dotted = args[0], args[1]
			}
			m, _, err := loadMilestone(version)
			if err != nil {
				return err
			}
			val, ok, list := readMilestoneDotted(m, dotted)
			if !ok {
				return fmt.Errorf("unknown milestone field: %s", dotted)
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
		Use:   "set [version] <dotted.path> <value>",
		Short: "Write a single scalar milestone field (version defaults to state.current_milestone)",
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
			if err := writeMilestoneDotted(m, dotted, value); err != nil {
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
		Use:   "add [version] <list.path> <value>",
		Short: "Append a value to a list field (version defaults to state.current_milestone)",
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
			if err := appendMilestoneList(m, dotted, value); err != nil {
				return err
			}
			return m.Save(path)
		},
	}
}

// loadMilestone resolves the milestone version and loads it. An empty version
// falls back to state.current_milestone, mirroring `milestone show [version]`
// so get/set/add don't force the caller to repeat the milestone they're on.
func loadMilestone(version string) (*milestone.Milestone, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, "", err
	}
	if version == "" {
		s, err := state.Load(filepath.Join(root, state.File))
		if err != nil {
			return nil, "", fmt.Errorf("no version given and load state: %w", err)
		}
		version = s.CurrentMilestone
		if version == "" {
			return nil, "", errors.New("no version given and state has no current_milestone; run `dross milestone list` to see options")
		}
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
	switch normalizeListField(path) {
	case "scope.success_criteria":
		m.Scope.SuccessCriteria = appendUnique(m.Scope.SuccessCriteria, value)
	case "scope.non_goals":
		m.Scope.NonGoals = appendUnique(m.Scope.NonGoals, value)
	case "phases":
		m.Phases = appendUnique(m.Phases, value)
	default:
		return fmt.Errorf("not a list field %q — valid: scope.success_criteria, scope.non_goals, phases", path)
	}
	return nil
}

// normalizeListField canonicalizes the list-field path so `dross milestone
// add` tolerates the naming inconsistency: two fields live under scope.* but
// phases is bare. Accept both the bare and scope-prefixed spellings of each
// so a wrong-but-reasonable guess doesn't fail.
func normalizeListField(path string) string {
	switch path {
	case "success_criteria", "scope.success_criteria":
		return "scope.success_criteria"
	case "non_goals", "scope.non_goals":
		return "scope.non_goals"
	case "phases", "scope.phases":
		return "phases"
	}
	return path
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
