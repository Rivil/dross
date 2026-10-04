package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/review"
	"github.com/Rivil/dross/internal/treefp"
)

// Review registers `dross review {context,status}` — the solo task reviewer's
// read side. Neither verb records a review: a pass is written only by the
// PostToolUse recorder reading the reviewer agent's own verdict
// (review_pass_signal).
func Review() *cobra.Command {
	c := &cobra.Command{
		Use:   "review",
		Short: "Prepare and read the solo per-task review (/dross-execute --solo, /dross-quick --solo)",
	}
	c.AddCommand(reviewContext(), reviewStatus())
	return c
}

// reviewRoot is the repo root and the user's home.
func reviewRoot() (string, string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Dir(root), home, nil
}

// unarmedReason says why no solo review scope is armed.
func unarmedReason(repoDir string) string {
	if ex, err := gatestate.LoadExecute(repoDir); err == nil && ex != nil && ex.Mode == "pair" {
		return "/dross-execute is running " + ex.Phase + " in pair mode — the human is the gate, so no solo review runs"
	}
	if q, err := gatestate.LoadQuick(repoDir); err == nil && q != nil && q.Mode == "pair" {
		return "the quick is running in pair mode — the human is the gate, so no solo review runs"
	}
	return "no solo task or quick is armed: a solo review covers the one in_progress task of `dross execute begin <phase> --solo` (on its phase branch) or a /dross-quick --solo begun at the current HEAD"
}

func reviewContext() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Write the review context for the armed solo task or quick and print the reviewer's prompt line",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			repoDir, home, err := reviewRoot()
			if err != nil {
				return err
			}
			scope, err := gate.ArmedScope(repoDir)
			if err != nil {
				return err
			}
			if scope == nil {
				return errors.New(unarmedReason(repoDir))
			}
			cs, err := gate.ContextScope(repoDir, home, scope)
			if err != nil {
				return err
			}
			ctx, err := review.BuildContext(repoDir, cs, gate.ReviewSecrets(home))
			if errors.Is(err, review.ErrNoCode) {
				return fmt.Errorf("nothing to review for %s: %w — a .dross/-only commit needs no review", scope.Name(), err)
			}
			if err != nil {
				return err
			}
			if err := gatestate.SaveContext(repoDir, ctx.File()); err != nil {
				return err
			}
			// Only the path, the digest and the line to spawn with — never the
			// diff: the context is for the reviewer's eyes, and printing it
			// here would put it in the executing session's transcript.
			Printf("context:       %s\n", review.ContextPath)
			Printf("digest:        %s\n", ctx.Digest)
			Printf("scope:         %s\n", scope.Name())
			Printf("subagent_type: %s\n", review.ReviewerAgent)
			Printf("prompt:        %s\n", review.PromptLine(ctx.Digest))
			return nil
		},
	}
}

func reviewStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print where the armed solo review stands (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			repoDir, _, err := reviewRoot()
			if err != nil {
				return err
			}
			scope, err := gate.ArmedScope(repoDir)
			if err != nil {
				return err
			}
			if scope == nil {
				Printf("status: unarmed\n%s\n", unarmedReason(repoDir))
				return nil
			}
			rec, err := gatestate.LoadReview(repoDir)
			if err != nil {
				return err
			}
			var rounds []review.Round
			if scope.Matches(rec) {
				rounds = rec.Rounds
			}
			st := review.StateOf(rounds)
			status := string(st.Status)
			note := ""
			switch st.Status {
			case review.StatusPass:
				tree, err := treefp.WorkingTree(repoDir)
				if err != nil {
					return err
				}
				if tree != st.Tree {
					status = "pass-stale"
					note = "the tree changed after the review passed — re-run `dross test`, then `dross review context` and the reviewer"
				}
			case review.StatusNone:
				note = "no review recorded yet — run `dross review context`, then spawn " + review.ReviewerAgent + " with the printed prompt and wait for its verdict"
			case review.StatusBlocked:
				note = "blocked — one fix round: address every blocking finding, re-run `dross test`, then `dross review context` and the reviewer"
			case review.StatusExhausted, review.StatusUnavailable:
				note = st.Cause + " — set the code aside and mark the task failed"
			}
			Printf("status: %s\nscope:  %s\nrounds: %d\n", status, scope.Name(), len(rounds))
			if note != "" {
				Printf("%s\n", note)
			}
			if n := len(rounds); n > 0 {
				last := rounds[n-1]
				for _, f := range last.Spec {
					Printf("  spec %s %s: %s\n", f.Severity, f.Criterion, f.Text)
				}
				for _, f := range last.Quality {
					Printf("  quality %s: %s\n", f.Severity, f.Text)
				}
			}
			return nil
		},
	}
}
