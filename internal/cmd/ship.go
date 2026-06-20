package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
)

// Ship orchestrates /dross-ship: pushes the current phase/<id> branch
// and opens a provider-aware PR with auto-assigned human reviewers.
// The provider's squash-merge collapses the per-task commits on the
// branch into a single commit on main — no client-side squash needed.
func Ship() *cobra.Command {
	var (
		title           string
		body            string
		bodyFile        string
		noPush          bool
		draft           bool
		forceUnverified bool
		forcePush       bool
		printBody       bool
	)
	c := &cobra.Command{
		Use:   "ship [phase-id]",
		Short: "Push phase/<id> and open a PR via the project's provider",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			// 1) Resolve phase id.
			phaseID := ""
			if len(args) == 1 {
				phaseID = args[0]
			} else {
				phaseID = s.CurrentPhase
			}
			if phaseID == "" {
				return errors.New("no phase id given and state has no current_phase")
			}
			phaseBranch := "phase/" + phaseID

			// 2) Load phase artefacts.
			phaseDir := phase.Dir(root, phaseID)
			spec, err := phase.LoadSpec(filepath.Join(phaseDir, "spec.toml"))
			if err != nil {
				return fmt.Errorf("load spec: %w", err)
			}
			vTests, vToml := verify.FilePaths(root, phaseID)
			_ = vTests
			vrf, err := verify.LoadVerify(vToml)
			if err != nil {
				return fmt.Errorf("load verify for %s: %w\n\nNext: run /dross-verify to write verify.toml, then `dross verify finalize %s`, then re-run ship",
					phaseID, err, phaseID)
			}

			// 3) Pre-flight gates.
			if p.Remote.URL == "" || p.Remote.Provider == "" {
				return errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
			}
			if vrf.Verify.Verdict != "pass" && !forceUnverified {
				switch vrf.Verify.Verdict {
				case "pending":
					return fmt.Errorf("verify verdict is \"pending\" for %s — finalize with `dross verify finalize %s`, or pass --force-unverified to override",
						phaseID, phaseID)
				case "fail", "partial":
					return fmt.Errorf("verify verdict is %q for %s — fix the failing criteria and re-run /dross-verify, or pass --force-unverified to override",
						vrf.Verify.Verdict, phaseID)
				default:
					return fmt.Errorf("verify verdict is %q (need \"pass\"); use --force-unverified to override",
						vrf.Verify.Verdict)
				}
			}

			// Must be on the phase branch. Pushing from anywhere else is
			// almost certainly a mistake — phase work belongs on phase/<id>.
			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("read current branch: %w", err)
			}
			if cur != phaseBranch {
				return fmt.Errorf("must be on %s to ship (currently on %s); switch with `git checkout %s`",
					phaseBranch, cur, phaseBranch)
			}

			// 4) Title + body.
			if title == "" {
				title = fmt.Sprintf("phase %s: %s", phaseID, spec.Phase.Title)
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read --body-file: %w", err)
				}
				body = string(b)
			}
			if body == "" {
				body = ship.BuildPRBody(spec, vrf)
			}

			if printBody {
				Print(body)
				return nil
			}

			if noPush {
				Print("--no-push set; not pushing or opening PR.")
				return nil
			}

			// 5) Fold the completion record into the squash. Write the
			//    completed-state transition (clear current_phase + status,
			//    append a `completed <id>` history entry) and commit it
			//    *before* the push, so the pushed ref carries it and the
			//    provider's squash-merge lands it on main. This is what
			//    eliminates the completion-chore divergence: phase complete
			//    no longer writes a standalone post-merge commit to local
			//    main (it becomes ff + branch-delete only — see t-2). The PR
			//    URL is known only post-push, so it drops out of the commit
			//    and is printed instead.
			//
			//    Idempotent: re-shipping after review edits re-writes the
			//    same state and only commits when something actually staged,
			//    so a no-op re-ship doesn't error on "nothing to commit".
			s.CurrentPhase = ""
			s.CurrentPhaseStatus = ""
			s.Touch(fmt.Sprintf("completed %s", phaseID))
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			if out, err := gitCombined(repoDir, "add", filepath.Join(".dross", state.File)); err != nil {
				return fmt.Errorf("git add state.json: %w\n%s", err, out)
			}
			if err := gitNoOut(repoDir, "diff", "--cached", "--quiet"); err != nil {
				// Non-nil err means there IS a staged change to commit.
				shipMsg := fmt.Sprintf("chore(dross): ship %s", phaseID)
				if out, err := gitCombined(repoDir, "commit", "-m", shipMsg); err != nil {
					return fmt.Errorf("git commit: %w\n%s", err, out)
				}
			}

			// 6) Push phase/<id> directly. The provider's squash-merge will
			//    collapse the per-task commits into one on main; no client-side
			//    synthetic branch needed.
			pushArgs := []string{"push", "-u", "origin", phaseBranch}
			if forcePush {
				// --force-with-lease guards against clobbering a concurrent
				// push from another machine without requiring the user to
				// know the remote SHA.
				pushArgs = []string{"push", "-u", "--force-with-lease", "origin", phaseBranch}
			}
			pushOut, perr := gitCombined(repoDir, pushArgs...)
			if perr != nil {
				return fmt.Errorf("git push: %w\n%s", perr, pushOut)
			}
			Printf("Pushed %s to origin\n", phaseBranch)

			// 7) Open the PR via the provider.
			baseBranch := p.Repo.GitMainBranch
			if baseBranch == "" {
				baseBranch = "main"
			}
			res, err := ship.OpenPR(ship.OpenOpts{
				Provider:   p.Remote.Provider,
				URL:        p.Remote.URL,
				APIBase:    p.Remote.APIBase,
				AuthEnv:    p.Remote.AuthEnv,
				HeadBranch: phaseBranch,
				BaseBranch: baseBranch,
				Title:      title,
				Body:       body,
				Reviewers:  p.Remote.Reviewers,
				Draft:      draft,
			})
			if err != nil && res == nil {
				return fmt.Errorf("open PR: %w", err)
			}
			if res != nil {
				Printf("PR opened: %s (#%d)\n", res.URL, res.Number)
				if len(p.Remote.Reviewers) > 0 {
					Printf("Reviewers requested: %s\n", strings.Join(p.Remote.Reviewers, ", "))
				}
			}
			if err != nil {
				// Non-fatal post-PR errors (e.g. reviewer add failed).
				Printf("Warning: %v\n", err)
			}
			Printf("Completion record folded into %s — squash-merge will land it on %s\n", phaseBranch, baseBranch)

			// 8) Telemetry — capture shape of this ship without leaking
			//    repo URL, body content, or reviewer names.
			tags := map[string]string{
				"provider": p.Remote.Provider,
				"result":   shipResultTag(res, err),
			}
			if draft {
				tags["draft"] = "true"
			}
			if forceUnverified || forcePush {
				tags["force"] = "true"
			}
			counts := map[string]int{
				"reviewers":   len(p.Remote.Reviewers),
				"body_chars":  len(body),
				"title_chars": len(title),
			}
			RecordOutcomeEvent("ship", counts, nil, tags)
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "PR title (default: 'phase <id>: <spec title>')")
	c.Flags().StringVar(&body, "body", "", "PR body override (overrides generated body)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read PR body from file")
	c.Flags().BoolVar(&noPush, "no-push", false, "don't push the phase branch or open a PR")
	c.Flags().BoolVar(&draft, "draft", false, "open the PR as draft")
	c.Flags().BoolVar(&forceUnverified, "force-unverified", false, "skip the 'verify must be pass' gate")
	c.Flags().BoolVar(&forcePush, "force", false,
		"force-with-lease the push (use when re-pushing after rewriting phase/<id>)")
	c.Flags().BoolVar(&printBody, "print-body", false, "print the generated PR body and exit (no push, no PR)")
	c.AddCommand(shipComment())
	c.AddCommand(shipRecover())
	return c
}

// shipComment posts a markdown comment to an existing PR via the
// project's provider. Used by /dross-review to publish the aggregated
// subagent panel findings as a single consolidated comment.
func shipComment() *cobra.Command {
	var (
		prNumber int
		body     string
		bodyFile string
	)
	c := &cobra.Command{
		Use:   "comment --pr <n> (--body \"...\" | --body-file <path>)",
		Short: "Post a comment to a PR via the project's provider",
		RunE: func(_ *cobra.Command, _ []string) error {
			if prNumber <= 0 {
				return errors.New("--pr is required")
			}
			if body == "" && bodyFile == "" {
				return errors.New("either --body or --body-file is required")
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read --body-file: %w", err)
				}
				body = string(b)
			}
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			if p.Remote.URL == "" || p.Remote.Provider == "" {
				return errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
			}
			if err := ship.PostComment(ship.CommentOpts{
				Provider: p.Remote.Provider,
				URL:      p.Remote.URL,
				APIBase:  p.Remote.APIBase,
				AuthEnv:  p.Remote.AuthEnv,
				PRNumber: prNumber,
				Body:     body,
			}); err != nil {
				return fmt.Errorf("post comment: %w", err)
			}
			Printf("Posted comment to PR #%d\n", prNumber)
			return nil
		},
	}
	c.Flags().IntVar(&prNumber, "pr", 0, "PR number to comment on (required)")
	c.Flags().StringVar(&body, "body", "", "comment body (markdown)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read comment body from file")
	return c
}

// shipResultTag classifies a ship's outcome into a single token. Used
// for the telemetry "result" tag so ship outcomes are easy to bucket.
func shipResultTag(res *ship.OpenResult, err error) string {
	switch {
	case err != nil && res == nil:
		return "failed"
	case err != nil && res != nil:
		return "partial" // PR opened, post-step (reviewers etc.) failed
	case res != nil:
		return "opened"
	default:
		return "noop"
	}
}
