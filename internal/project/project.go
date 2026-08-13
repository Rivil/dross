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
	Project     ProjectMeta       `toml:"project" json:"project"`
	Stack       Stack             `toml:"stack" json:"stack"`
	Runtime     Runtime           `toml:"runtime" json:"runtime"`
	Repo        Repo              `toml:"repo" json:"repo"`
	Remote      Remote            `toml:"remote,omitempty" json:"remote,omitempty"`
	Board       Board             `toml:"board,omitempty" json:"board,omitempty"`
	Paths       Paths             `toml:"paths" json:"paths"`
	Env         Env               `toml:"env" json:"env"`
	Goals       Goals             `toml:"goals" json:"goals"`
	Mutation    Mutation          `toml:"mutation,omitempty" json:"mutation,omitempty"`
	Constraints map[string]string `toml:"constraints,omitempty" json:"constraints,omitempty"`
	Competition []Competitor      `toml:"competition,omitempty" json:"competition,omitempty"`
}

type ProjectMeta struct {
	Name        string `toml:"name" json:"name"`
	Version     string `toml:"version" json:"version"` // 4-part: major.minor.patch.internal
	Description string `toml:"description,omitempty" json:"description,omitempty"`
	Created     string `toml:"created" json:"created"`
}

type Stack struct {
	Languages      []string       `toml:"languages" json:"languages"`
	Profile        string         `toml:"profile,omitempty" json:"profile,omitempty"` // matched stack-profile id (internal/stack)
	Frameworks     []string       `toml:"frameworks,omitempty" json:"frameworks,omitempty"`
	PackageManager string         `toml:"package_manager,omitempty" json:"package_manager,omitempty"`
	TypeChecker    string         `toml:"type_checker,omitempty" json:"type_checker,omitempty"`
	Linter         string         `toml:"linter,omitempty" json:"linter,omitempty"`
	Formatter      string         `toml:"formatter,omitempty" json:"formatter,omitempty"`
	TestRunner     string         `toml:"test_runner,omitempty" json:"test_runner,omitempty"`
	E2ERunner      string         `toml:"e2e_runner,omitempty" json:"e2e_runner,omitempty"`
	Locked         []LockedChoice `toml:"locked,omitempty" json:"locked,omitempty"`
}

type LockedChoice struct {
	Choice   string `toml:"choice" json:"choice"`
	Why      string `toml:"why" json:"why"`
	LockedAt string `toml:"locked_at" json:"locked_at"`
}

// Runtime is the pain-point-killer section. Capture exact commands
// so Claude never guesses pnpm/npm/docker again.
type Runtime struct {
	Mode             string             `toml:"mode" json:"mode"` // docker | native | hybrid
	DevCommand       string             `toml:"dev_command,omitempty" json:"dev_command,omitempty"`
	StopCommand      string             `toml:"stop_command,omitempty" json:"stop_command,omitempty"`
	TestCommand      string             `toml:"test_command,omitempty" json:"test_command,omitempty"`
	TestWatch        string             `toml:"test_watch,omitempty" json:"test_watch,omitempty"`
	E2ECommand       string             `toml:"e2e_command,omitempty" json:"e2e_command,omitempty"`
	TypecheckCommand string             `toml:"typecheck_command,omitempty" json:"typecheck_command,omitempty"`
	LintCommand      string             `toml:"lint_command,omitempty" json:"lint_command,omitempty"`
	FormatCommand    string             `toml:"format_command,omitempty" json:"format_command,omitempty"`
	BuildCommand     string             `toml:"build_command,omitempty" json:"build_command,omitempty"`
	MigrateCommand   string             `toml:"migrate_command,omitempty" json:"migrate_command,omitempty"`
	SeedCommand      string             `toml:"seed_command,omitempty" json:"seed_command,omitempty"`
	ShellCommand     string             `toml:"shell_command,omitempty" json:"shell_command,omitempty"`
	LogsCommand      string             `toml:"logs_command,omitempty" json:"logs_command,omitempty"`
	Services         map[string]Service `toml:"services,omitempty" json:"services,omitempty"`
}

type Service struct {
	URL    string `toml:"url,omitempty" json:"url,omitempty"`
	Health string `toml:"health,omitempty" json:"health,omitempty"`
	Admin  string `toml:"admin,omitempty" json:"admin,omitempty"`
}

type Repo struct {
	Layout           string   `toml:"layout" json:"layout"` // single | monorepo
	RootRunDir       string   `toml:"root_run_dir,omitempty" json:"root_run_dir,omitempty"`
	Workspaces       []string `toml:"workspaces,omitempty" json:"workspaces,omitempty"`
	GitMainBranch    string   `toml:"git_main_branch" json:"git_main_branch"`
	BranchPattern    string   `toml:"branch_pattern,omitempty" json:"branch_pattern,omitempty"`
	CommitConvention string   `toml:"commit_convention,omitempty" json:"commit_convention,omitempty"` // conventional | freeform
	SquashMerge      bool     `toml:"squash_merge" json:"squash_merge"`
}

// Remote describes the canonical hosting destination for the repo.
// Separated from Repo (which holds branch/layout policy) because hosting
// + auth + reviewer config travels with the code, not the local checkout.
type Remote struct {
	URL        string   `toml:"url,omitempty" json:"url,omitempty"`                 // canonical https URL of the repo
	Provider   string   `toml:"provider,omitempty" json:"provider,omitempty"`       // github | forgejo | gitea | gitlab | bitbucket, or none for no remote (configenum.ShipProviders)
	Public     bool     `toml:"public,omitempty" json:"public,omitempty"`           // true if cloud agents can clone
	APIBase    string   `toml:"api_base,omitempty" json:"api_base,omitempty"`       // override; default derived from provider+URL
	LogAPI     bool     `toml:"log_api,omitempty" json:"log_api,omitempty"`         // instance exposes CI logs via API
	AuthEnv    string   `toml:"auth_env,omitempty" json:"auth_env,omitempty"`       // env var name (NEVER the value)
	AuthUser   string   `toml:"auth_user,omitempty" json:"auth_user,omitempty"`     // bitbucket: account user for HTTP Basic auth (user:token)
	AuthScheme string   `toml:"auth_scheme,omitempty" json:"auth_scheme,omitempty"` // private-token (default) | bearer | basic (configenum.AuthSchemes); basic needs auth_user
	ProjectID  string   `toml:"project_id,omitempty" json:"project_id,omitempty"`   // gitlab: numeric project-id override (else derived from URL)
	Reviewers  []string `toml:"reviewers,omitempty" json:"reviewers,omitempty"`     // default human reviewers for /dross-ship
}

// Board describes the issue-tracker destination for board sync, kept separate
// from Remote so a repo can ship code to one host ([remote]) while tracking
// issues on another ([board]) — e.g. ship to GitLab, track in YouTrack. It is
// the single source for `dross issue` board operations; there is no [remote]
// fallback.
type Board struct {
	Provider      string            `toml:"provider,omitempty" json:"provider,omitempty"`             // forgejo | gitea | gitlab | youtrack | jira | github
	BaseURL       string            `toml:"base_url,omitempty" json:"base_url,omitempty"`             // instance base URL of the tracker
	AuthEnv       string            `toml:"auth_env,omitempty" json:"auth_env,omitempty"`             // env var name holding the token (NEVER the value)
	AuthUser      string            `toml:"auth_user,omitempty" json:"auth_user,omitempty"`           // jira: account email for HTTP Basic auth (email:token)
	Project       string            `toml:"project,omitempty" json:"project,omitempty"`               // project short-name / key on the tracker; github: "owner/repo"
	GitHubProject string            `toml:"github_project,omitempty" json:"github_project,omitempty"` // github: Projects v2 board node id to add created issues to
	Enabled       bool              `toml:"enabled,omitempty" json:"enabled,omitempty"`               // board sync is on
	MilestoneMode string            `toml:"milestone_mode,omitempty" json:"milestone_mode,omitempty"` // version (default) | agile | epic
	StateMap      map[string]string `toml:"state_map,omitempty" json:"state_map,omitempty"`           // dross lifecycle state → tracker State value override
	Fields        BoardFields       `toml:"fields,omitempty" json:"fields,omitempty"`                 // tracker-native field-name overrides
}

// BoardFields overrides the tracker-native field names board sync writes to.
// Every key defaults to the literal the provider ships with, so an untouched
// project syncs exactly as it did before — but a project that renamed a field
// (or runs a non-English tracker UI) no longer needs a code change.
//
// Scoped to YouTrack today, which is the only provider this repo runs against
// and therefore the only one where the fix is provable; the Jira and GitHub
// halves are deferred to the provider work itself.
type BoardFields struct {
	State       string `toml:"state,omitempty" json:"state,omitempty"`               // youtrack: the State custom field (default "State")
	Type        string `toml:"type,omitempty" json:"type,omitempty"`                 // youtrack: the issue-type custom field (default "Type")
	FixVersions string `toml:"fix_versions,omitempty" json:"fix_versions,omitempty"` // youtrack: the version bundle field (default "Fix versions")
}

type Paths struct {
	Source     string `toml:"source,omitempty" json:"source,omitempty"`
	Tests      string `toml:"tests,omitempty" json:"tests,omitempty"`
	E2E        string `toml:"e2e,omitempty" json:"e2e,omitempty"`
	Migrations string `toml:"migrations,omitempty" json:"migrations,omitempty"`
	Schemas    string `toml:"schemas,omitempty" json:"schemas,omitempty"`
	I18n       string `toml:"i18n,omitempty" json:"i18n,omitempty"`
	Public     string `toml:"public,omitempty" json:"public,omitempty"`
}

type Env struct {
	Files           []string `toml:"files,omitempty" json:"files,omitempty"`                       // load order
	SecretsLocation string   `toml:"secrets_location,omitempty" json:"secrets_location,omitempty"` // vault | doppler | 1password | local
	Gitignored      bool     `toml:"gitignored,omitempty" json:"gitignored,omitempty"`
}

type Goals struct {
	CoreValue       string   `toml:"core_value,omitempty" json:"core_value,omitempty"`
	Audience        string   `toml:"audience,omitempty" json:"audience,omitempty"`
	NonGoals        []string `toml:"non_goals,omitempty" json:"non_goals,omitempty"`
	Differentiators []string `toml:"differentiators,omitempty" json:"differentiators,omitempty"`
}

// Mutation holds per-adapter knobs for the mutation testing pipeline.
// Each sub-table is optional; unset values fall back to the adapter's
// built-in default.
type Mutation struct {
	// Adapters is an allowlist of adapter names ("gremlins", "stryker",
	// "stryker.net"). Non-empty means run ONLY these; files whose adapter
	// is filtered out fall into verify's Skipped path ("no mutation adapter
	// for .ts"). The escape hatch for a polyglot repo where one adapter
	// isn't set up yet.
	Adapters []string         `toml:"adapters,omitempty" json:"adapters,omitempty"`
	Gremlins MutationGremlins `toml:"gremlins,omitempty" json:"gremlins,omitempty"`
	Stryker  MutationStryker  `toml:"stryker,omitempty" json:"stryker,omitempty"`
}

// MutationStryker surfaces the stryker adapter's tunable settings.
//
// Workdir is the repo-relative package that hosts stryker and its config in
// a monorepo (e.g. "web" when vitest + stryker.config.json live in web/).
// The adapter runs there, strips the prefix from --mutate paths, and
// re-prefixes report paths so tests.json stays repo-relative.
type MutationStryker struct {
	Workdir string `toml:"workdir,omitempty" json:"workdir,omitempty"`
}

// MutationGremlins surfaces the gremlins adapter's tunable settings.
//
// TimeoutCoefficient overrides gremlins' --timeout-coefficient flag.
// Gremlins multiplies this by the baseline test duration to decide
// per-mutant timeout. The tool's built-in default (~3) is too tight
// for Go projects with fast test suites: a 75ms baseline yields a
// 0.22s budget per mutant, far below Go's 1–2s compile-and-test cycle,
// and most mutants get classified TIMED OUT before they can be killed
// or surviving. Dross overrides this default to 30 unless the project
// sets a different value.
type MutationGremlins struct {
	TimeoutCoefficient int `toml:"timeout_coefficient,omitempty" json:"timeout_coefficient,omitempty"`
}

type Competitor struct {
	Name           string `toml:"name" json:"name"`
	URL            string `toml:"url,omitempty" json:"url,omitempty"`
	WhatTheyDo     string `toml:"what_they_do,omitempty" json:"what_they_do,omitempty"`
	Differentiator string `toml:"differentiator,omitempty" json:"differentiator,omitempty"`
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
