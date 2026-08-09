package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/profile"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/state"
)

// Onboard adopts an existing repo into dross.
//
// This is the *CLI* side. It performs the pre-flight scan and writes
// a `project.toml` populated with everything dross can detect from
// signal files. The /dross-onboard slash command + prompts/onboard.md
// drive the conversational confirmation flow.
func Onboard() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "onboard",
		Short: "Adopt the existing repo into dross by scanning signal files",
		RunE: func(c *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root := filepath.Join(cwd, RootDirName)
			exists := true
			if _, err := os.Stat(root); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				exists = false
			}

			// Branch order matters, and --force wins outright: it wipes and
			// re-scaffolds whatever is there, complete or not. Without it, a
			// COMPLETE root still refuses, but an INCOMPLETE one is adopted in
			// place — onboard is the repair command every not-a-dross-repo
			// message now names, so refusing on exactly the roots it should
			// repair would make that pointer a dead end.
			//
			// Completeness comes from MissingRootFiles against the cwd-local
			// `.dross`, never LocateRoot: an up-walk here would let onboard
			// adopt an ancestor project's root (locked walk_stop).
			adopt := false
			if exists {
				if force {
					if err := os.RemoveAll(root); err != nil {
						return err
					}
				} else {
					missing, err := MissingRootFiles(root)
					if err != nil {
						return err
					}
					if len(missing) == 0 {
						return errors.New(".dross already exists — use --force to overwrite")
					}
					adopt = true
				}
			}
			// Adopting preserves what's already there; only the absent files
			// are written.
			keepExisting := func(path string) bool {
				if !adopt {
					return false
				}
				_, err := os.Stat(path)
				return err == nil
			}
			for _, d := range []string{root, filepath.Join(root, "milestones"), filepath.Join(root, "phases")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
			}

			scan := scanRepo(cwd)
			p := scan.toProject()
			p.Remote, _ = seedRemote(cwd)
			seedRuntimeFromProfile(cwd, p)
			if projPath := filepath.Join(root, project.File); !keepExisting(projPath) {
				if err := p.Save(projPath); err != nil {
					return err
				}
			}

			if statePath := filepath.Join(root, state.File); !keepExisting(statePath) {
				s := state.New()
				s.Touch("dross onboard")
				if err := s.Save(statePath); err != nil {
					return err
				}
			}

			if rulesPath := filepath.Join(root, rules.File); !keepExisting(rulesPath) {
				rs := &rules.Set{}
				if err := rs.SaveFile(rulesPath); err != nil {
					return err
				}
			}

			if err := ensureDrossGitattributes(cwd); err != nil {
				return fmt.Errorf("write .gitattributes: %w", err)
			}

			// Adopting a repo has to untrack-going-forward too: without the
			// ignore, the very first ship folds state.json back into the tree
			// (locked state_tracking).
			if err := ensureDrossGitignore(cwd); err != nil {
				return fmt.Errorf("write .gitignore: %w", err)
			}

			_ = profile.SeedFromGSD(filepath.Join(root, profile.File))

			Printf("dross onboarded at %s\n", root)
			Print("Detected:")
			for _, line := range scan.summary() {
				Printf("  • %s\n", line)
			}
			if p.Remote.URL != "" {
				Printf("  • git remote: %s (provider: %s)\n", p.Remote.URL, providerOrUnknown(p.Remote.Provider))
			}
			if err := ensureUserHooks(); err != nil {
				Printf("  • Could not wire user-level Claude hooks (non-fatal): %v\n", err)
			} else {
				Print("  • Ensured user-level Claude hooks (PreCompact → dross pause --auto, SessionStart → dross reentry)")
			}
			Print("\nNext: /dross-onboard to confirm captured runtime + rules")
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "remove existing .dross/ before onboard")
	return c
}

type scanResult struct {
	hasDocker      bool
	hasCompose     bool
	hasPackageJSON bool
	hasPNPM        bool
	hasNPM         bool
	hasYarn        bool
	hasBun         bool
	hasTSConfig    bool
	hasGoMod       bool
	hasCsproj      bool
	hasGodot       bool
	hasMakefile    bool
	hasGitHubCI    bool
}

func scanRepo(cwd string) scanResult {
	s := scanResult{}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(cwd, rel))
		return err == nil
	}
	s.hasDocker = exists("Dockerfile")
	s.hasCompose = exists("docker-compose.yml") || exists("compose.yaml") || exists("docker-compose.yaml")
	s.hasPackageJSON = exists("package.json")
	s.hasPNPM = exists("pnpm-lock.yaml")
	s.hasNPM = exists("package-lock.json")
	s.hasYarn = exists("yarn.lock")
	s.hasBun = exists("bun.lockb") || exists("bun.lock")
	s.hasTSConfig = exists("tsconfig.json")
	s.hasGoMod = exists("go.mod")
	s.hasGodot = exists("project.godot")
	s.hasMakefile = exists("Makefile")
	s.hasGitHubCI = exists(".github/workflows")

	// Walk one level looking for *.csproj
	if entries, err := os.ReadDir(cwd); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".csproj") {
				s.hasCsproj = true
				break
			}
		}
	}
	return s
}

func (s scanResult) toProject() *project.Project {
	languages := []string{}
	if s.hasTSConfig || s.hasPackageJSON {
		languages = append(languages, "typescript")
	}
	if s.hasGoMod {
		languages = append(languages, "go")
	}
	if s.hasCsproj {
		languages = append(languages, "csharp")
	}
	if s.hasGodot {
		languages = append(languages, "gdscript")
	}

	pm := ""
	switch {
	case s.hasPNPM:
		pm = "pnpm"
	case s.hasYarn:
		pm = "yarn"
	case s.hasBun:
		pm = "bun"
	case s.hasNPM:
		pm = "npm"
	}

	mode := "native"
	dev := ""
	if s.hasCompose {
		mode = "docker"
		dev = "docker compose up"
	} else if s.hasDocker {
		mode = "docker"
	}
	if mode == "native" && pm != "" {
		dev = pm + " dev"
	}

	return &project.Project{
		Project: project.ProjectMeta{
			Version: "0.1.0.0",
			Created: time.Now().UTC().Format("2006-01-02"),
		},
		Stack: project.Stack{
			Languages:      languages,
			PackageManager: pm,
		},
		Runtime: project.Runtime{
			Mode:       mode,
			DevCommand: dev,
		},
		Repo: project.Repo{
			Layout:        "single",
			GitMainBranch: "main",
		},
	}
}

func (s scanResult) summary() []string {
	var out []string
	add := func(cond bool, label string) {
		if cond {
			out = append(out, label)
		}
	}
	add(s.hasDocker, "Dockerfile")
	add(s.hasCompose, "docker-compose")
	add(s.hasPackageJSON, "package.json")
	add(s.hasPNPM, "pnpm-lock.yaml (→ pnpm)")
	add(s.hasNPM && !s.hasPNPM && !s.hasYarn, "package-lock.json (→ npm)")
	add(s.hasYarn, "yarn.lock (→ yarn)")
	add(s.hasBun, "bun.lock (→ bun)")
	add(s.hasTSConfig, "tsconfig.json (→ typescript)")
	add(s.hasGoMod, "go.mod (→ go)")
	add(s.hasCsproj, "*.csproj (→ csharp)")
	add(s.hasGodot, "project.godot (→ gdscript)")
	add(s.hasMakefile, "Makefile")
	add(s.hasGitHubCI, ".github/workflows (CI present)")
	if len(out) == 0 {
		out = append(out, "no signal files found — check cwd, or use `dross init` for greenfield")
	}
	return out
}

// dummy unused import guard
var _ = fmt.Sprintf
