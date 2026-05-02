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

// readDotted covers every settable leaf in project.toml. Lists return as
// CSV strings; bools as "true"/"false". Nested map fields like
// runtime.services and stack.locked still require direct toml edits;
// /dross-options surfaces them but doesn't iterate keys.
func readDotted(p *project.Project, path string) (string, bool) {
	switch path {
	// project
	case "project.name":
		return p.Project.Name, true
	case "project.description":
		return p.Project.Description, true
	case "project.version":
		return p.Project.Version, true
	// stack
	case "stack.languages":
		return strings.Join(p.Stack.Languages, ","), true
	case "stack.frameworks":
		return strings.Join(p.Stack.Frameworks, ","), true
	case "stack.package_manager":
		return p.Stack.PackageManager, true
	case "stack.type_checker":
		return p.Stack.TypeChecker, true
	case "stack.linter":
		return p.Stack.Linter, true
	case "stack.formatter":
		return p.Stack.Formatter, true
	case "stack.test_runner":
		return p.Stack.TestRunner, true
	case "stack.e2e_runner":
		return p.Stack.E2ERunner, true
	// runtime
	case "runtime.mode":
		return p.Runtime.Mode, true
	case "runtime.dev_command":
		return p.Runtime.DevCommand, true
	case "runtime.stop_command":
		return p.Runtime.StopCommand, true
	case "runtime.test_command":
		return p.Runtime.TestCommand, true
	case "runtime.test_watch":
		return p.Runtime.TestWatch, true
	case "runtime.e2e_command":
		return p.Runtime.E2ECommand, true
	case "runtime.typecheck_command":
		return p.Runtime.TypecheckCommand, true
	case "runtime.lint_command":
		return p.Runtime.LintCommand, true
	case "runtime.format_command":
		return p.Runtime.FormatCommand, true
	case "runtime.build_command":
		return p.Runtime.BuildCommand, true
	case "runtime.migrate_command":
		return p.Runtime.MigrateCommand, true
	case "runtime.seed_command":
		return p.Runtime.SeedCommand, true
	case "runtime.shell_command":
		return p.Runtime.ShellCommand, true
	case "runtime.logs_command":
		return p.Runtime.LogsCommand, true
	// repo
	case "repo.layout":
		return p.Repo.Layout, true
	case "repo.root_run_dir":
		return p.Repo.RootRunDir, true
	case "repo.workspaces":
		return strings.Join(p.Repo.Workspaces, ","), true
	case "repo.git_main_branch":
		return p.Repo.GitMainBranch, true
	case "repo.branch_pattern":
		return p.Repo.BranchPattern, true
	case "repo.commit_convention":
		return p.Repo.CommitConvention, true
	case "repo.squash_merge":
		return fmt.Sprintf("%t", p.Repo.SquashMerge), true
	// remote
	case "remote.url":
		return p.Remote.URL, true
	case "remote.provider":
		return p.Remote.Provider, true
	case "remote.public":
		return fmt.Sprintf("%t", p.Remote.Public), true
	case "remote.api_base":
		return p.Remote.APIBase, true
	case "remote.log_api":
		return fmt.Sprintf("%t", p.Remote.LogAPI), true
	case "remote.auth_env":
		return p.Remote.AuthEnv, true
	case "remote.reviewers":
		return strings.Join(p.Remote.Reviewers, ","), true
	// paths
	case "paths.source":
		return p.Paths.Source, true
	case "paths.tests":
		return p.Paths.Tests, true
	case "paths.e2e":
		return p.Paths.E2E, true
	case "paths.migrations":
		return p.Paths.Migrations, true
	case "paths.schemas":
		return p.Paths.Schemas, true
	case "paths.i18n":
		return p.Paths.I18n, true
	case "paths.public":
		return p.Paths.Public, true
	// env
	case "env.files":
		return strings.Join(p.Env.Files, ","), true
	case "env.secrets_location":
		return p.Env.SecretsLocation, true
	case "env.gitignored":
		return fmt.Sprintf("%t", p.Env.Gitignored), true
	// goals
	case "goals.core_value":
		return p.Goals.CoreValue, true
	case "goals.audience":
		return p.Goals.Audience, true
	case "goals.non_goals":
		return strings.Join(p.Goals.NonGoals, ","), true
	case "goals.differentiators":
		return strings.Join(p.Goals.Differentiators, ","), true
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
	setBool := func(target *bool) error {
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		*target = b
		return nil
	}
	switch path {
	// project
	case "project.name":
		p.Project.Name = value
	case "project.description":
		p.Project.Description = value
	case "project.version":
		p.Project.Version = value
	// stack
	case "stack.languages":
		p.Stack.Languages = splitCSV(value)
	case "stack.frameworks":
		p.Stack.Frameworks = splitCSV(value)
	case "stack.package_manager":
		p.Stack.PackageManager = value
	case "stack.type_checker":
		p.Stack.TypeChecker = value
	case "stack.linter":
		p.Stack.Linter = value
	case "stack.formatter":
		p.Stack.Formatter = value
	case "stack.test_runner":
		p.Stack.TestRunner = value
	case "stack.e2e_runner":
		p.Stack.E2ERunner = value
	// runtime
	case "runtime.mode":
		p.Runtime.Mode = value
	case "runtime.dev_command":
		p.Runtime.DevCommand = value
	case "runtime.stop_command":
		p.Runtime.StopCommand = value
	case "runtime.test_command":
		p.Runtime.TestCommand = value
	case "runtime.test_watch":
		p.Runtime.TestWatch = value
	case "runtime.e2e_command":
		p.Runtime.E2ECommand = value
	case "runtime.typecheck_command":
		p.Runtime.TypecheckCommand = value
	case "runtime.lint_command":
		p.Runtime.LintCommand = value
	case "runtime.format_command":
		p.Runtime.FormatCommand = value
	case "runtime.build_command":
		p.Runtime.BuildCommand = value
	case "runtime.migrate_command":
		p.Runtime.MigrateCommand = value
	case "runtime.seed_command":
		p.Runtime.SeedCommand = value
	case "runtime.shell_command":
		p.Runtime.ShellCommand = value
	case "runtime.logs_command":
		p.Runtime.LogsCommand = value
	// repo
	case "repo.layout":
		p.Repo.Layout = value
	case "repo.root_run_dir":
		p.Repo.RootRunDir = value
	case "repo.workspaces":
		p.Repo.Workspaces = splitCSV(value)
	case "repo.git_main_branch":
		p.Repo.GitMainBranch = value
	case "repo.branch_pattern":
		p.Repo.BranchPattern = value
	case "repo.commit_convention":
		p.Repo.CommitConvention = value
	case "repo.squash_merge":
		return setBool(&p.Repo.SquashMerge)
	// remote
	case "remote.url":
		p.Remote.URL = value
	case "remote.provider":
		p.Remote.Provider = value
	case "remote.public":
		return setBool(&p.Remote.Public)
	case "remote.api_base":
		p.Remote.APIBase = value
	case "remote.log_api":
		return setBool(&p.Remote.LogAPI)
	case "remote.auth_env":
		p.Remote.AuthEnv = value
	case "remote.reviewers":
		p.Remote.Reviewers = splitCSV(value)
	// paths
	case "paths.source":
		p.Paths.Source = value
	case "paths.tests":
		p.Paths.Tests = value
	case "paths.e2e":
		p.Paths.E2E = value
	case "paths.migrations":
		p.Paths.Migrations = value
	case "paths.schemas":
		p.Paths.Schemas = value
	case "paths.i18n":
		p.Paths.I18n = value
	case "paths.public":
		p.Paths.Public = value
	// env
	case "env.files":
		p.Env.Files = splitCSV(value)
	case "env.secrets_location":
		p.Env.SecretsLocation = value
	case "env.gitignored":
		return setBool(&p.Env.Gitignored)
	// goals
	case "goals.core_value":
		p.Goals.CoreValue = value
	case "goals.audience":
		p.Goals.Audience = value
	case "goals.non_goals":
		p.Goals.NonGoals = splitCSV(value)
	case "goals.differentiators":
		p.Goals.Differentiators = splitCSV(value)
	default:
		return fmt.Errorf("unknown or unsettable field: %s", path)
	}
	return nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "1":
		return true, nil
	case "false", "no", "n", "0", "":
		return false, nil
	}
	return false, fmt.Errorf("invalid bool: %q (use true/false)", v)
}
