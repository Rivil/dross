// Package project handles .dross/project.toml — the long-lived
// per-repo identity, stack, runtime, and constraints.
package project

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// File is the canonical filename inside .dross/.
const File = "project.toml"

// Project is the top-level schema.
type Project struct {
	Project     ProjectMeta       `toml:"project"`
	Stack       Stack             `toml:"stack"`
	Runtime     Runtime           `toml:"runtime"`
	Repo        Repo              `toml:"repo"`
	Paths       Paths             `toml:"paths"`
	Env         Env               `toml:"env"`
	Goals       Goals             `toml:"goals"`
	Constraints map[string]string `toml:"constraints,omitempty"`
	Competition []Competitor      `toml:"competition,omitempty"`
}

type ProjectMeta struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"` // 4-part: major.minor.patch.internal
	Description string `toml:"description,omitempty"`
	Created     string `toml:"created"`
}

type Stack struct {
	Languages      []string       `toml:"languages"`
	Frameworks     []string       `toml:"frameworks,omitempty"`
	PackageManager string         `toml:"package_manager,omitempty"`
	TypeChecker    string         `toml:"type_checker,omitempty"`
	Linter         string         `toml:"linter,omitempty"`
	Formatter      string         `toml:"formatter,omitempty"`
	TestRunner     string         `toml:"test_runner,omitempty"`
	E2ERunner      string         `toml:"e2e_runner,omitempty"`
	Locked         []LockedChoice `toml:"locked,omitempty"`
}

type LockedChoice struct {
	Choice   string `toml:"choice"`
	Why      string `toml:"why"`
	LockedAt string `toml:"locked_at"`
}

// Runtime is the pain-point-killer section. Capture exact commands
// so Claude never guesses pnpm/npm/docker again.
type Runtime struct {
	Mode             string             `toml:"mode"` // docker | native | hybrid
	DevCommand       string             `toml:"dev_command,omitempty"`
	StopCommand      string             `toml:"stop_command,omitempty"`
	TestCommand      string             `toml:"test_command,omitempty"`
	TestWatch        string             `toml:"test_watch,omitempty"`
	E2ECommand       string             `toml:"e2e_command,omitempty"`
	TypecheckCommand string             `toml:"typecheck_command,omitempty"`
	LintCommand      string             `toml:"lint_command,omitempty"`
	FormatCommand    string             `toml:"format_command,omitempty"`
	BuildCommand     string             `toml:"build_command,omitempty"`
	MigrateCommand   string             `toml:"migrate_command,omitempty"`
	SeedCommand      string             `toml:"seed_command,omitempty"`
	ShellCommand     string             `toml:"shell_command,omitempty"`
	LogsCommand      string             `toml:"logs_command,omitempty"`
	Services         map[string]Service `toml:"services,omitempty"`
}

type Service struct {
	URL    string `toml:"url,omitempty"`
	Health string `toml:"health,omitempty"`
	Admin  string `toml:"admin,omitempty"`
}

type Repo struct {
	Layout           string   `toml:"layout"` // single | monorepo
	RootRunDir       string   `toml:"root_run_dir,omitempty"`
	Workspaces       []string `toml:"workspaces,omitempty"`
	GitMainBranch    string   `toml:"git_main_branch"`
	BranchPattern    string   `toml:"branch_pattern,omitempty"`
	CommitConvention string   `toml:"commit_convention,omitempty"` // conventional | freeform
	SquashMerge      bool     `toml:"squash_merge"`
}

type Paths struct {
	Source     string `toml:"source,omitempty"`
	Tests      string `toml:"tests,omitempty"`
	E2E        string `toml:"e2e,omitempty"`
	Migrations string `toml:"migrations,omitempty"`
	Schemas    string `toml:"schemas,omitempty"`
	I18n       string `toml:"i18n,omitempty"`
	Public     string `toml:"public,omitempty"`
}

type Env struct {
	Files            []string `toml:"files,omitempty"`             // load order
	SecretsLocation  string   `toml:"secrets_location,omitempty"`  // vault | doppler | 1password | local
	Gitignored       bool     `toml:"gitignored,omitempty"`
}

type Goals struct {
	CoreValue       string   `toml:"core_value,omitempty"`
	Audience        string   `toml:"audience,omitempty"`
	NonGoals        []string `toml:"non_goals,omitempty"`
	Differentiators []string `toml:"differentiators,omitempty"`
}

type Competitor struct {
	Name           string `toml:"name"`
	URL            string `toml:"url,omitempty"`
	WhatTheyDo     string `toml:"what_they_do,omitempty"`
	Differentiator string `toml:"differentiator,omitempty"`
}

// Load reads a project.toml file.
func Load(path string) (*Project, error) {
	var p Project
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &p, nil
}

// Save writes a project.toml file (overwrites).
func (p *Project) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(p); err != nil {
		return fmt.Errorf("encode project.toml: %w", err)
	}
	return nil
}
