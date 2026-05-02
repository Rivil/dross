package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/profile"
)

func Profile() *cobra.Command {
	c := &cobra.Command{
		Use:   "profile",
		Short: "Read user profile (global + project overrides)",
	}
	c.AddCommand(profileShow(), profileSeed())
	return c
}

func profileShow() *cobra.Command {
	var scope string
	c := &cobra.Command{
		Use:   "show",
		Short: "Print profile (global | project | merged)",
		RunE: func(_ *cobra.Command, _ []string) error {
			gp, err := loadProfile("global")
			if err != nil {
				return err
			}
			pp, _ := loadProfile("project") // project optional outside repo
			if pp == nil {
				pp = &profile.Profile{Dimensions: map[string]profile.Dimension{}}
			}
			var out *profile.Profile
			switch scope {
			case "global":
				out = gp
			case "project":
				out = pp
			case "merged", "":
				out = profile.Merge(gp, pp)
			default:
				return fmt.Errorf("scope must be global | project | merged")
			}
			return toml.NewEncoder(os.Stdout).Encode(out)
		},
	}
	c.Flags().StringVar(&scope, "scope", "merged", "global | project | merged")
	return c
}

// profileSeed (re-)imports the GSD profile into the global path.
func profileSeed() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Seed the global profile from GSD's USER-PROFILE.md if present",
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := GlobalDir()
			if err != nil {
				return err
			}
			path := filepath.Join(dir, profile.File)
			if err := profile.SeedFromGSD(path); err != nil {
				return err
			}
			Printf("seeded %s\n", path)
			return nil
		},
	}
}

func loadProfile(scope string) (*profile.Profile, error) {
	switch scope {
	case "global":
		dir, err := GlobalDir()
		if err != nil {
			return nil, err
		}
		return profile.LoadFile(filepath.Join(dir, profile.File))
	case "project":
		root, err := FindRoot()
		if err != nil {
			return nil, err
		}
		return profile.LoadFile(filepath.Join(root, profile.File))
	default:
		return nil, fmt.Errorf("unknown scope: %s", scope)
	}
}
