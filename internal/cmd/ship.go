package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
)

// Ship orchestrates /dross-ship: derives a clean review branch from
// the current phase, pushes it, and opens a provider-aware PR with
// auto-assigned human reviewers.
func Ship() *cobra.Command {
	var (
		title           string
		body            string
		bodyFile        string
		noPush          bool
		draft           bool
		forceUnverified bool
		forceBranch     bool
		printBody       bool
	)
	c := &cobra.Command{
		Use:   "ship [phase-id]",
		Short: "Open a clean PR for a phase (filter .dross/, push, open PR via provider)",
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
				return fmt.Errorf("load verify (run /dross-verify first?): %w", err)
			}
			ch, err := changes.Load(changes.FilePath(root, phaseID), phaseID)
			if err != nil {
				return fmt.Errorf("load changes: %w", err)
			}

			// 3) Pre-flight gates.
			if p.Remote.URL == "" || p.Remote.Provider == "" {
				return errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
			}
			if vrf.Verify.Verdict != "pass" && !forceUnverified {
				return fmt.Errorf("verify verdict is %q (need 'pass'); use --force-unverified to override",
					vrf.Verify.Verdict)
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

			// 5) Build the squash branch.
			branch, sha, err := ship.FilterSquash(ch, ship.FilterOpts{
				RepoDir: repoDir,
				PhaseID: phaseID,
				Message: title,
				Force:   forceBranch,
			})
			if err != nil {
				return fmt.Errorf("filter squash: %w", err)
			}
			Printf("Built %s @ %s\n", branch, sha[:min(7, len(sha))])

			if noPush {
				Print("--no-push set; not pushing or opening PR.")
				return nil
			}

			// 6) Push.
			pushOut, perr := exec.Command("git", "-C", repoDir, "push", "-u", "origin", branch).CombinedOutput()
			if perr != nil {
				return fmt.Errorf("git push: %w\n%s", perr, string(pushOut))
			}
			Printf("Pushed %s to origin\n", branch)

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
				HeadBranch: branch,
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

			// 8) State update.
			action := fmt.Sprintf("shipped %s", phaseID)
			if res != nil {
				action = fmt.Sprintf("shipped %s → %s", phaseID, res.URL)
			}
			s.Touch(action)
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "PR title (default: 'phase <id>: <spec title>')")
	c.Flags().StringVar(&body, "body", "", "PR body override (overrides generated body)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read PR body from file")
	c.Flags().BoolVar(&noPush, "no-push", false, "build the squash branch locally but don't push or open PR")
	c.Flags().BoolVar(&draft, "draft", false, "open the PR as draft")
	c.Flags().BoolVar(&forceUnverified, "force-unverified", false, "skip the 'verify must be pass' gate")
	c.Flags().BoolVar(&forceBranch, "force-branch", false, "overwrite an existing pr/<id> branch")
	c.Flags().BoolVar(&printBody, "print-body", false, "print the generated PR body and exit (no push, no branch)")
	return c
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
