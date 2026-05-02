package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/defaults"
)

func Defaults() *cobra.Command {
	c := &cobra.Command{
		Use:   "defaults",
		Short: "Manage cross-project defaults at ~/.claude/dross/defaults.toml",
	}
	c.AddCommand(defaultsShow(), defaultsSave())
	return c
}

func defaultsShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the global defaults file",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := defaultsPath()
			if err != nil {
				return err
			}
			d, err := defaults.LoadFile(path)
			if err != nil {
				return err
			}
			Printf("# %s\n", path)
			return toml.NewEncoder(os.Stdout).Encode(d)
		},
	}
}

func defaultsSave() *cobra.Command {
	return &cobra.Command{
		Use:   "save",
		Short: "Save the current project's [remote] as global defaults for future projects",
		Long: `Reads .dross/project.toml [remote] and writes the reusable subset
(provider, api_base, log_api, auth_env, reviewers) to
~/.claude/dross/defaults.toml. URL and public flag are project-specific
and never copied.

Use this once you've configured a project the way you'd like future
projects to start. Subsequent ` + "`dross init`" + ` / ` + "`dross onboard`" + ` runs will
pre-fill those fields from the saved defaults.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			path, err := defaultsPath()
			if err != nil {
				return err
			}
			d, err := defaults.LoadFile(path)
			if err != nil {
				return err
			}
			d.Remote = defaults.FromRemote(p.Remote)
			if err := d.SaveFile(path); err != nil {
				return err
			}
			Printf("Saved %s\n", path)
			Printf("  provider  = %q\n", d.Remote.Provider)
			Printf("  api_base  = %q\n", d.Remote.APIBase)
			Printf("  log_api   = %t\n", d.Remote.LogAPI)
			Printf("  auth_env  = %q\n", d.Remote.AuthEnv)
			if len(d.Remote.Reviewers) > 0 {
				Printf("  reviewers = %v\n", d.Remote.Reviewers)
			}
			return nil
		},
	}
}

func defaultsPath() (string, error) {
	dir, err := GlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, defaults.File), nil
}

// dummy unused import guard
var _ = fmt.Sprintf
