package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/gitrun"
)

// Quick is `dross quick`: the state /dross-quick keeps through the CLI.
func Quick() *cobra.Command {
	c := &cobra.Command{
		Use:   "quick",
		Short: "Bookkeeping /dross-quick keeps through the CLI",
	}
	c.AddCommand(quickBegin(), quickEnd())
	return c
}

// requireReviewer refuses a solo begin whose reviewer could not run as
// shipped: missing, stale, or shadowed by a repo-level definition. A solo run
// reviews every task before its commit, so without the shipped reviewer it
// would implement every task only to fail each one at review.
func requireReviewer(repoDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	r := reviewerStatusIn(userAgentsDir(home), repoDir)
	if r.State == reviewerOK {
		return nil
	}
	return fmt.Errorf("refusing a solo run: %s (%s) — %s. A solo run has every task reviewed before its commit; "+
		"without the reviewer dross ships, each task would fail its review", r.Problem(), r.State, r.Remedy())
}

// quickBegin records the mode a /dross-quick runs in, its stated description
// (the solo reviewer's only spec source), the HEAD it began at and when — a
// fresh begin is a fresh review attempt. It writes nothing else.
func quickBegin() *cobra.Command {
	var solo bool
	c := &cobra.Command{
		Use:   "begin <description>",
		Short: "Record the mode /dross-quick runs in (pair unless --solo) and what it sets out to do",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			desc := args[0]
			if strings.TrimSpace(desc) == "" {
				return errors.New("a quick needs its stated description: the solo reviewer checks the change against it")
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			mode := "pair"
			if solo {
				mode = "solo"
				if err := requireReviewer(repoDir); err != nil {
					return err
				}
			}
			head, err := gitrun.Trim(repoDir, "rev-parse", "--verify", "--quiet", "HEAD")
			if err != nil && gitrun.ExitCode(err) != 1 {
				return fmt.Errorf("read HEAD: %w", err)
			}
			if err := gatestate.SaveQuick(repoDir, gatestate.Quick{Mode: mode, Description: desc, Head: head, At: time.Now().UTC()}); err != nil {
				return err
			}
			Printf("quick: %s (recorded)\n", mode)
			return nil
		},
	}
	c.Flags().BoolVar(&solo, "solo", false, "record solo mode: the quick is reviewed by the solo reviewer before its commit")
	return c
}

// quickEnd removes the quick-mode record; none is not an error, so every
// abort path can run it unconditionally.
func quickEnd() *cobra.Command {
	return &cobra.Command{
		Use:   "end",
		Short: "Clear the quick-mode record when a /dross-quick finishes or aborts",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			if err := gatestate.RemoveQuick(filepath.Dir(root)); err != nil {
				return err
			}
			Printf("quick: ended\n")
			return nil
		},
	}
}
