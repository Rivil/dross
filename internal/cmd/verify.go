package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/verify"
)

// Verify registers `dross verify <phase>`.
//
// What this command does (mechanical only — LLM does criterion mapping):
//   1. Read project.toml + phases/<id>/spec.toml + phases/<id>/changes.json
//   2. Group touched files by language
//   3. For each language with an adapter: run mutation testing
//   4. Aggregate into tests.json
//   5. Write a verify.toml skeleton (verdict = pending) for /dross-verify
//      to fill in criterion-to-test mappings + final verdict.
func Verify() *cobra.Command {
	var skipMutation bool
	c := &cobra.Command{
		Use:   "verify <phase-id>",
		Short: "Run mutation testing per language and write tests.json + verify.toml skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			proj, err := project.Load(filepath.Join(root, project.File))
			if err != nil {
				return err
			}
			specPath := filepath.Join(phase.Dir(root, phaseID), "spec.toml")
			spec, err := phase.LoadSpec(specPath)
			if err != nil {
				return fmt.Errorf("read spec: %w (run /dross-spec first)", err)
			}

			changesPath := changes.FilePath(root, phaseID)
			ch, err := changes.Load(changesPath, phaseID)
			if err != nil {
				return err
			}
			filesByTask := map[string][]string{}
			for taskID, rec := range ch.Tasks {
				filesByTask[taskID] = rec.Files
			}
			files := verify.FilesFromChanges(filesByTask)
			if len(files) == 0 {
				Print("verify: no changes recorded for this phase yet (execute hasn't touched any files).")
				Print("Run /dross-execute first, or record changes manually with `dross changes record`.")
				return nil
			}

			adapters := configuredAdapters(proj, root, skipMutation)
			t, err := verify.Run(phaseID, files, adapters)
			if err != nil {
				return err
			}

			testsPath, verifyPath := verify.FilePaths(root, phaseID)
			if err := t.Save(testsPath); err != nil {
				return err
			}

			ids := make([]string, 0, len(spec.Criteria))
			for _, c := range spec.Criteria {
				ids = append(ids, c.ID)
			}
			v := verify.Skeleton(t, ids)
			if err := v.Save(verifyPath); err != nil {
				return err
			}

			printVerifySummary(t, v)
			return nil
		},
	}
	c.Flags().BoolVar(&skipMutation, "skip-mutation", false,
		"do not run mutation tests (record what would have been mutated, skip execution)")
	return c
}

// configuredAdapters returns the list of mutation adapters appropriate
// for the project, with runtime prefixes applied (docker compose exec ...).
func configuredAdapters(p *project.Project, _ string, skip bool) []mutation.Adapter {
	if skip {
		return nil // verify still runs — files end up in Skipped
	}
	prefix := dockerPrefix(p)
	// Project root for stryker is the runtime's cwd — host cwd for native,
	// or the host cwd for docker (we read the report via bind-mounted fs).
	// If docker volume layout diverges, this is where we'd surface config.
	cwd, _ := os.Getwd()
	return []mutation.Adapter{
		&mutation.Stryker{Prefix: prefix, ProjectRoot: cwd},
		&mutation.Gremlins{Prefix: prefix, ProjectRoot: cwd},
		// Future: &mutation.StrykerNet{...}
	}
}

// dockerPrefix returns the runtime command prefix for docker mode.
// For native, returns "". For docker, derives from runtime.test_command
// (which already has the right shape: "docker compose exec app pnpm test").
func dockerPrefix(p *project.Project) string {
	if p.Runtime.Mode != "docker" {
		return ""
	}
	// Strip the trailing test-runner from runtime.test_command to get
	// the docker exec prefix. Pragmatic heuristic — if it doesn't start
	// with "docker", fall back to empty (warns later).
	tc := p.Runtime.TestCommand
	if !strings.HasPrefix(tc, "docker") {
		return "docker compose exec app"
	}
	// Take everything before the last "pnpm "/"npm "/"yarn "/"bun " token,
	// or the first 4 fields, whichever is shorter — we want the prefix
	// that gets us inside the container, then we'll append "npx stryker..."
	for _, runner := range []string{" pnpm ", " npm ", " yarn ", " bun ", " node "} {
		if i := strings.Index(tc, runner); i > 0 {
			return tc[:i]
		}
	}
	// Default: "docker compose exec app"
	return "docker compose exec app"
}

func printVerifySummary(t *verify.Tests, v *verify.Verify) {
	Printf("verify: phase %s\n", t.Phase)
	if len(t.Languages) == 0 && len(t.Skipped) == 0 {
		Print("  (nothing to mutation-test)")
	}
	for _, lr := range t.Languages {
		if lr.Mutation == nil {
			Printf("  %s (%s): %d files — no mutation report\n", lr.Name, lr.Tool, len(lr.Files))
			continue
		}
		m := lr.Mutation
		Printf("  %s (%s): %d files — killed=%d survived=%d timeout=%d errors=%d score=%.2f\n",
			lr.Name, lr.Tool, len(lr.Files), m.Killed, m.Survived, m.Timeout, m.Errors, m.Score)
	}
	for _, s := range t.Skipped {
		Printf("  skipped %s — %s\n", s.File, s.Reason)
	}
	Printf("\nWrote tests.json + verify.toml (verdict=%s — /dross-verify will fill criterion mappings).\n", v.Verify.Verdict)
}
