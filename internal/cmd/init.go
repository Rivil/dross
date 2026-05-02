package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/profile"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/rules"
	"github.com/Rivil/dross/internal/state"
)

// Init creates the .dross/ directory tree with empty skeletons.
//
// This is the *CLI* side. The conversational flow lives in the
// /dross-init slash command + assets/prompts/init.md, which calls
// `dross init` here, then `dross project set ...` to fill it in.
func Init() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap .dross/ in the current repo (greenfield)",
		Long: `Creates the .dross/ scaffold so the /dross-init slash command can
fill it in conversationally. For adopting an existing repo, use ` + "`dross onboard`" + ` instead.`,
		RunE: func(c *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root := filepath.Join(cwd, RootDirName)
			if _, err := os.Stat(root); err == nil && !force {
				return errors.New(".dross already exists — use --force to overwrite")
			}
			if err == nil && force {
				if err := os.RemoveAll(root); err != nil {
					return fmt.Errorf("force remove existing .dross: %w", err)
				}
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}

			for _, d := range []string{root, filepath.Join(root, "milestones"), filepath.Join(root, "phases")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
			}

			// Skeleton project.toml — slash command fills in the rest.
			p := &project.Project{
				Project: project.ProjectMeta{
					Version: "0.1.0.0",
					Created: time.Now().UTC().Format("2006-01-02"),
				},
				Repo: project.Repo{
					Layout:        "single",
					GitMainBranch: "main",
				},
			}
			if err := p.Save(filepath.Join(root, project.File)); err != nil {
				return err
			}

			// Empty state.
			s := state.New()
			s.Touch("dross init")
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return err
			}

			// Empty project rules file (so `dross rule add` has somewhere to write).
			rs := &rules.Set{}
			if err := rs.SaveFile(filepath.Join(root, rules.File)); err != nil {
				return err
			}

			// Seed profile from GSD if available — silent no-op otherwise.
			seedErr := profile.SeedFromGSD(filepath.Join(root, profile.File))
			seeded := seedErr == nil
			if seedErr != nil {
				fmt.Fprintln(os.Stderr, "warning: GSD profile seed failed:", seedErr)
			}

			Printf("dross initialized at %s\n", root)
			if seeded {
				Print("• Seeded profile.toml from GSD USER-PROFILE.md")
			}
			Print("Next: run /dross-init to fill in project.toml conversationally")
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "remove existing .dross/ before init")
	return c
}
