package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/prtriage"
	"github.com/Rivil/dross/internal/ship"
)

// PR is the `dross pr` noun: the review comments on a phase's PR, read as
// untrusted data, triaged one verdict at a time, and answered with one reply.
// It backs /dross-respond.
func PR() *cobra.Command {
	c := &cobra.Command{
		Use:   "pr",
		Short: "Read and triage the review comments on a phase's PR (backs /dross-respond)",
	}
	c.AddCommand(prComments(), prResolve())
	return c
}

// prThread is what every `dross pr` verb works on: one PR, the phase its head
// branch ships, the comments on it as triage items, and the triage record they
// are judged against.
type prThread struct {
	root, repoDir string
	pr            int
	head          ship.PRHead
	phase         string
	opts          ship.OpenOpts
	self          ship.Account
	items         []prtriage.Item
	recPath       string
	rec           prtriage.Record
	recBytes      []byte
}

// parsePRNumber reads a PR number argument: digits only, above zero.
func parsePRNumber(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 || strconv.Itoa(n) != arg {
		return 0, fmt.Errorf("invalid PR number %q — pass the number alone, e.g. `dross pr comments 138`", arg)
	}
	return n, nil
}

// loadPRThread binds PR arg to its phase and reads its thread. It refuses a
// PR whose head is not a same-repo phase/<id> branch, refuses unless HEAD is
// on that branch — the record it reads and the plan a verdict writes to are
// that branch's — and then refuses a phase with no plan here, all before a
// single comment is fetched.
func loadPRThread(arg string) (*prThread, error) {
	n, err := parsePRNumber(arg)
	if err != nil {
		return nil, err
	}
	root, err := FindRoot()
	if err != nil {
		return nil, err
	}
	t := &prThread{root: root, repoDir: filepath.Dir(root), pr: n}
	p, _, err := loadProject()
	if err != nil {
		return nil, err
	}
	if p.Remote.URL == "" || p.Remote.Provider == "" {
		return nil, errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
	}
	hosts, err := remotePolicy(root, t.repoDir, p)
	if err != nil {
		return nil, err
	}
	t.opts = buildOpenOpts(p, hosts)

	if t.head, err = ship.PRHeadOfFunc(t.opts, n); err != nil {
		return nil, fmt.Errorf("read PR #%d: %w", n, err)
	}
	id, err := prtriage.ParsePhaseHead(t.head.Ref, t.head.CrossRepo)
	if err != nil {
		return nil, fmt.Errorf("invalid PR for `dross pr`: PR #%d does not ship a phase — it triages a PR whose head is a phase/<id> branch planned here: %w", n, err)
	}
	cur, err := gitrun.Trim(t.repoDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("read current branch: %w", err)
	}
	if cur != "phase/"+id {
		return nil, fmt.Errorf("invalid branch: `dross pr` works on the phase/<id> branch the PR ships — PR #%d ships phase/%s but HEAD is on %s; switch with `dross phase checkout %[2]s`, then re-run",
			n, prtriage.OneLine(id), prtriage.OneLine(cur))
	}
	if t.phase, err = prtriage.PhaseFromHead(t.repoDir, t.head.Ref, t.head.CrossRepo); err != nil {
		return nil, fmt.Errorf("invalid PR for `dross pr`: PR #%d does not ship a phase planned here: %w", n, err)
	}

	if t.self, err = ship.AuthenticatedUserFunc(t.opts); err != nil {
		return nil, fmt.Errorf("read the account dross acts as: %w", err)
	}
	comments, err := ship.ListPRCommentsFunc(t.opts, n)
	if err != nil {
		return nil, fmt.Errorf("read PR #%d comments: %w", n, err)
	}
	t.items = prtriage.Items(comments, t.self)

	t.recPath = filepath.Join(phase.Dir(root, t.phase), prtriage.File)
	if t.rec, t.recBytes, err = prtriage.Load(t.recPath); err != nil {
		return nil, err
	}
	return t, nil
}

func prComments() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "comments <pr>",
		Short: "List a phase PR's review comments that still need a verdict, bodies fenced as untrusted data",
		Long: "Lists the conversation comments, inline comments (with file:line) and review " +
			"summaries on PR <pr> that have no verdict yet, or have been edited since theirs, " +
			"each as one metadata line and its body fenced as untrusted data. --all lists every " +
			"comment with its status. Reads only; writes nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			t, err := loadPRThread(args[0])
			if err != nil {
				return err
			}
			show := prtriage.Pending(t.items, t.rec)
			pending := len(show)
			if all {
				show = t.items
			}
			if err := prtriage.Render(c.OutOrStdout(), show, t.rec); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "PR #%d (phase/%s): %d of %d %s need a verdict\n",
				t.pr, t.phase, pending, len(t.items), plural(len(t.items), "comment", "comments"))
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "list every comment with its status, not only those that need a verdict")
	return c
}
