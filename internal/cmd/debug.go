package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/debugsession"
)

// Debug is `dross debug`: the machine-local session files /dross-debug keeps
// under .dross/debug/. A thin layer over internal/debugsession — every write
// goes through that package's store, never from here.
func Debug() *cobra.Command {
	c := &cobra.Command{
		Use:   "debug",
		Short: "Debug sessions — scaffold, list and close the .dross/debug/<slug>.md files /dross-debug keeps",
	}
	c.AddCommand(debugNew(), debugList(), debugClose())
	return c
}

// debugRepo is the project directory the session store works under.
func debugRepo() (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Dir(root), nil
}

func debugNew() *cobra.Command {
	return &cobra.Command{
		Use:   "new <slug>",
		Short: "Start a debug session: scaffold .dross/debug/<slug>.md from the fixed template, gitignored",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			repo, err := debugRepo()
			if err != nil {
				return err
			}
			slug := args[0]
			if _, err := debugsession.Create(repo, slug, time.Now()); err != nil {
				return err
			}
			Printf("created %s/%s.md (gitignored, machine-local)\n", debugsession.Dir, slug)
			Printf("work it with /dross-debug %s\n", slug)
			return nil
		},
	}
}

func debugList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List debug sessions: state, hypotheses, failed fixes since the last re-plan, last update",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			repo, err := debugRepo()
			if err != nil {
				return err
			}
			entries, err := debugsession.Load(repo)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				Printf("no debug sessions in %s\n", debugsession.Dir)
				return nil
			}
			width := 0
			for _, e := range entries {
				width = max(width, len(e.Slug))
			}
			for _, e := range entries {
				s := e.Session
				Printf("%-*s  %-12s  hypotheses: %d  failed since re-plan: %d  updated: %s\n",
					width, e.Slug, s.State(), s.Hypotheses, s.FailedSinceReplan, e.Updated.UTC().Format(time.RFC3339))
				for _, p := range s.Problems {
					Printf("    problem: %s\n", p)
				}
			}
			return nil
		},
	}
}

func debugClose() *cobra.Command {
	var (
		fixed, abandoned bool
		reason           string
	)
	c := &cobra.Command{
		Use:   "close <slug>",
		Short: "Close a debug session: --fixed (needs 2 Resolution signals and a Prevention) or --abandoned --reason <why>",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			slug := args[0]
			to, err := closeTarget(slug, fixed, abandoned, reason)
			if err != nil {
				return err
			}
			if !debugsession.ValidSlug(slug) {
				return fmt.Errorf("invalid session name %q; see `dross debug list`", slug)
			}
			repo, err := debugRepo()
			if err != nil {
				return err
			}
			entries, err := debugsession.Load(repo)
			if err != nil {
				return err
			}
			var e *debugsession.Entry
			for i := range entries {
				if entries[i].Slug == slug {
					e = &entries[i]
				}
			}
			if e == nil {
				return fmt.Errorf("debug session %q not found; see `dross debug list`", slug)
			}
			if e.Raw == nil {
				return fmt.Errorf("debug session %q: %s", slug, strings.Join(e.Session.Problems, "; "))
			}
			// CloseText first: an already-closed session is refused as such,
			// whatever gaps its body has.
			out, err := debugsession.CloseText(e.Raw, to, reason, time.Now())
			if err != nil {
				return err
			}
			if to == debugsession.StateResolved {
				if gaps := debugsession.FixedCloseGaps(e.Session); len(gaps) > 0 {
					return fmt.Errorf("cannot close %s as fixed; missing:\n  - %s\nRecord them in %s/%s.md, or give up on it with `dross debug close %s --abandoned --reason \"<why>\"`",
						slug, strings.Join(gaps, "\n  - "), debugsession.Dir, slug, slug)
				}
			}
			if err := debugsession.Rewrite(repo, slug, e.Raw, out); err != nil {
				return err
			}
			Printf("closed %s as %s; %s/%s.md stays on disk\n", slug, to, debugsession.Dir, slug)
			if to == debugsession.StateResolved {
				if line := debugsession.RuleAddCommand(e.Session); line != "" {
					Printf("its Prevention, as a rule — adding one is your call, so it is printed, not run:\n  %s\n", line)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&fixed, "fixed", false, "the cause is found and fixed: needs 2 signals under ## Resolution and a non-empty ## Prevention")
	c.Flags().BoolVar(&abandoned, "abandoned", false, "stop without a fix; needs --reason")
	c.Flags().StringVar(&reason, "reason", "", "why the session is abandoned (one line; with --abandoned only)")
	return c
}

// closeTarget validates the close flags, naming the working form on a refusal.
func closeTarget(slug string, fixed, abandoned bool, reason string) (debugsession.State, error) {
	fixedForm := fmt.Sprintf("`dross debug close %s --fixed`", slug)
	abandonForm := fmt.Sprintf("`dross debug close %s --abandoned --reason \"<why>\"`", slug)
	switch {
	case fixed == abandoned:
		return "", fmt.Errorf("invalid flags: pass exactly one of --fixed or --abandoned: %s or %s", fixedForm, abandonForm)
	case fixed && reason != "":
		return "", fmt.Errorf("invalid flags: --reason goes with --abandoned only; a fixed close records its evidence under ## Resolution: %s", fixedForm)
	case abandoned && strings.TrimSpace(reason) == "":
		return "", errors.New("--reason is required with --abandoned: " + abandonForm)
	case fixed:
		return debugsession.StateResolved, nil
	default:
		return debugsession.StateAbandoned, nil
	}
}
