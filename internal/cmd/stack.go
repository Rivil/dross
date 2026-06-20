package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

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

// stackApply is wired here so the subcommand set is complete; its body lands in
// t-11 (re-sync [runtime] from the current profile).
func stackApply() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Re-sync project.toml [runtime] from the current stack profile",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("stack apply: not yet implemented")
		},
	}
}

// stackLoadout is wired here so the subcommand set is complete; its body lands in
// t-9 (emit the agent loadout as a markdown block).
func stackLoadout() *cobra.Command {
	return &cobra.Command{
		Use:   "loadout [id]",
		Short: "Emit the agent loadout for the stack as a markdown block",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("stack loadout: not yet implemented")
		},
	}
}
