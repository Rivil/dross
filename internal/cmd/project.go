package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
)

func Project() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Read and edit .dross/project.toml",
	}
	c.AddCommand(projectShow(), projectSet(), projectGet())
	return c
}

func projectShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print project.toml",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			Printf("# %s\n", path)
			return toml.NewEncoder(os.Stdout).Encode(p)
		},
	}
}

// projectGet prints a single dotted-path field (e.g. project.name, runtime.mode).
func projectGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <dotted.path>",
		Short: "Print a single field by dotted path",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			v, ok := readDotted(p, args[0])
			if !ok {
				return fmt.Errorf("unknown field: %s", args[0])
			}
			Print(v)
			return nil
		},
	}
}

// projectSet writes a single dotted-path field.
// String slices accept comma-separated input; bools accept true/false; ints parsed.
func projectSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <dotted.path> <value>",
		Short: "Write a single field by dotted path",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			if err := writeDotted(p, args[0], args[1]); err != nil {
				return err
			}
			return p.Save(path)
		},
	}
}

func loadProject() (*project.Project, string, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(root, project.File)
	p, err := project.Load(path)
	if err != nil {
		return nil, "", err
	}
	return p, path, nil
}

// readDotted is a deliberately small implementation supporting the
// fields most often touched by the /dross-init prompt:
//   project.name, project.description
//   stack.languages, stack.frameworks, stack.package_manager
//   runtime.mode, runtime.dev_command, runtime.test_command, ...
//   repo.git_main_branch, repo.layout
//   goals.core_value
func readDotted(p *project.Project, path string) (string, bool) {
	switch path {
	case "project.name":
		return p.Project.Name, true
	case "project.description":
		return p.Project.Description, true
	case "project.version":
		return p.Project.Version, true
	case "stack.package_manager":
		return p.Stack.PackageManager, true
	case "stack.languages":
		return strings.Join(p.Stack.Languages, ","), true
	case "stack.frameworks":
		return strings.Join(p.Stack.Frameworks, ","), true
	case "runtime.mode":
		return p.Runtime.Mode, true
	case "runtime.dev_command":
		return p.Runtime.DevCommand, true
	case "runtime.test_command":
		return p.Runtime.TestCommand, true
	case "runtime.typecheck_command":
		return p.Runtime.TypecheckCommand, true
	case "runtime.lint_command":
		return p.Runtime.LintCommand, true
	case "runtime.build_command":
		return p.Runtime.BuildCommand, true
	case "runtime.migrate_command":
		return p.Runtime.MigrateCommand, true
	case "repo.git_main_branch":
		return p.Repo.GitMainBranch, true
	case "repo.layout":
		return p.Repo.Layout, true
	case "goals.core_value":
		return p.Goals.CoreValue, true
	}
	return "", false
}

func writeDotted(p *project.Project, path, value string) error {
	splitCSV := func(s string) []string {
		out := []string{}
		for _, x := range strings.Split(s, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				out = append(out, x)
			}
		}
		return out
	}
	switch path {
	case "project.name":
		p.Project.Name = value
	case "project.description":
		p.Project.Description = value
	case "project.version":
		p.Project.Version = value
	case "stack.package_manager":
		p.Stack.PackageManager = value
	case "stack.languages":
		p.Stack.Languages = splitCSV(value)
	case "stack.frameworks":
		p.Stack.Frameworks = splitCSV(value)
	case "runtime.mode":
		p.Runtime.Mode = value
	case "runtime.dev_command":
		p.Runtime.DevCommand = value
	case "runtime.test_command":
		p.Runtime.TestCommand = value
	case "runtime.typecheck_command":
		p.Runtime.TypecheckCommand = value
	case "runtime.lint_command":
		p.Runtime.LintCommand = value
	case "runtime.build_command":
		p.Runtime.BuildCommand = value
	case "runtime.migrate_command":
		p.Runtime.MigrateCommand = value
	case "repo.git_main_branch":
		p.Repo.GitMainBranch = value
	case "repo.layout":
		p.Repo.Layout = value
	case "goals.core_value":
		p.Goals.CoreValue = value
	default:
		return fmt.Errorf("unknown or unsettable field: %s", path)
	}
	return nil
}
