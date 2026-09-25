package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/localstore"
)

// Local manages .dross/local.toml — machine-local values that must NOT ride
// cumulative history. The store, its key table and every typed reader live in
// internal/localstore; this is only the `dross local get|set` command tree.
//
// Phase work does not use this store — a phase's forked-from base lives in its
// phase-scoped changes.json, which cannot be dragged forward either.
func Local() *cobra.Command {
	c := &cobra.Command{
		Use:   "local",
		Short: "Read and write .dross/local.toml (gitignored, machine-local values)",
	}
	c.AddCommand(localGet(), localSet())
	return c
}

func localGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a machine-local value (empty when unset)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localstore.Keys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localstore.KeyNames())
			}
			l, err := localstore.Load(localstore.Path(root))
			if err != nil {
				return err
			}
			// An unset key prints nothing and exits 0 — callers branch on
			// empty output, so a missing value must not look like a failure.
			if v := acc.Get(l); v != "" {
				Print(v)
			}
			return nil
		},
	}
}

func localSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a machine-local value to .dross/local.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localstore.Keys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localstore.KeyNames())
			}
			path := localstore.Path(root)
			l, err := localstore.Load(path)
			if err != nil {
				return err
			}
			acc.Set(l, args[1])
			if err := l.Save(path); err != nil {
				return err
			}
			Printf("%s = %s\n", args[0], args[1])
			return nil
		},
	}
}
