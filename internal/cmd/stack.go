package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/stack"
)

// Stack registers `dross stack {detect,show,list,apply,loadout}` — the surface for
// stack profiles. A profile tunes runtime commands, the scanner/analyzer loadout,
// and the agent loadout to a detected technology stack. detect/show/list are the
// read surface; apply re-syncs [runtime] (t-11) and loadout emits the agent block
// (t-9).
func Stack() *cobra.Command {
	c := &cobra.Command{
		Use:   "stack",
		Short: "Stack profiles: detect the stack and tune runtime/tools/loadout to it",
	}
	c.AddCommand(stackDetect(), stackShow(), stackList(), stackApply(), stackLoadout())
	return c
}

func stackDetect() *cobra.Command {
	return &cobra.Command{
		Use:   "detect [path]",
		Short: "Detect the stack at path and print the matched profile id (or 'unsupported')",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profiles, err := stack.LoadAll()
			if err != nil {
				// A malformed user profile must not block detection — the embedded
				// set is still returned. Warn on stderr and proceed.
				fmt.Fprintf(os.Stderr, "dross: warning: %v\n", err)
			}
			Print(stack.Detect(pathArg(args), profiles))
			return nil
		},
	}
}

func stackShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a stack profile by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profiles, err := stack.LoadAll()
			if err != nil {
				fmt.Fprintf(os.Stderr, "dross: warning: %v\n", err)
			}
			p := stack.ByID(profiles, args[0])
			if p == nil {
				return fmt.Errorf("stack profile %q not found", args[0])
			}
			enc := toml.NewEncoder(os.Stdout)
			enc.Indent = "  "
			return enc.Encode(p)
		},
	}
}

func stackList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available stack profiles (embedded + user dir)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			profiles, err := stack.LoadAll()
			if err != nil {
				fmt.Fprintf(os.Stderr, "dross: warning: %v\n", err)
			}
			for _, p := range profiles {
				if p.Title != "" {
					Printf("%s\t%s\n", p.ID, p.Title)
					continue
				}
				Print(p.ID)
			}
			return nil
		},
	}
}

// stackApply re-syncs project.toml [runtime] from the current stack profile so an
// existing repo can pick up profile improvements on demand. An unsupported stack
// errors rather than fabricating commands.
func stackApply() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Re-sync project.toml [runtime] from the current stack profile",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			projPath := filepath.Join(root, project.File)
			p, err := project.Load(projPath)
			if err != nil {
				return err
			}
			id := seedRuntimeFromProfile(repoDir, p)
			if id == stack.Unsupported {
				return errors.New("no stack profile matches this repo — nothing to apply")
			}
			if err := p.Save(projPath); err != nil {
				return err
			}
			Printf("re-synced [runtime] from the %q stack profile\n", id)
			return nil
		},
	}
}

// stackLoadout emits the agent loadout for the named stack (or the detected one)
// as a markdown block for prompts to inject inline.
func stackLoadout() *cobra.Command {
	return &cobra.Command{
		Use:   "loadout [id]",
		Short: "Emit the agent loadout for the stack as a markdown block",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profiles, err := stack.LoadAll()
			if err != nil {
				fmt.Fprintf(os.Stderr, "dross: warning: %v\n", err)
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			} else {
				id = stack.Detect(".", profiles)
			}
			if id == stack.Unsupported {
				return errors.New("no stack profile matches here — pass an id (see 'dross stack list')")
			}
			p := stack.ByID(profiles, id)
			if p == nil {
				return fmt.Errorf("stack profile %q not found", id)
			}
			Print(stack.RenderLoadout(p, runtime.GOOS, exec.LookPath))
			return nil
		},
	}
}
