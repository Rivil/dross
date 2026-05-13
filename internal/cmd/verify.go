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
			recordVerifyOutcome(t, v)
			return nil
		},
	}
	c.Flags().BoolVar(&skipMutation, "skip-mutation", false,
		"do not run mutation tests (record what would have been mutated, skip execution)")
	c.AddCommand(verifyFinalize())
	return c
}

// verifyFinalize records a telemetry outcome event with the resolved
// verdict from a verify.toml that the LLM (via /dross-verify) has
// filled in. The mechanical `dross verify <phase>` always emits a
// pending-verdict event; this is the second event closing the loop.
//
// Verdict must be pass | partial | fail. Pending or unknown verdicts
// are rejected so the slash command can't accidentally finalize a
// half-filled skeleton.
func verifyFinalize() *cobra.Command {
	return &cobra.Command{
		Use:   "finalize <phase-id>",
		Short: "Record the resolved verdict from verify.toml as a telemetry outcome event",
		Long: "Reads .dross/phases/<phase>/verify.toml after /dross-verify (the LLM step) " +
			"has written the final verdict, and emits a telemetry outcome event so " +
			"`dross stats` and downstream gates can see the pass/partial/fail resolution.\n\n" +
			"Verdict must be pass | partial | fail. Pending or unknown is rejected — " +
			"finalize the verify.toml first.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			testsPath, verifyPath := verify.FilePaths(root, phaseID)
			v, err := verify.LoadVerify(verifyPath)
			if err != nil {
				return fmt.Errorf("read verify.toml: %w", err)
			}
			if v == nil {
				return fmt.Errorf("verify.toml not found at %s — run `dross verify %s` first", verifyPath, phaseID)
			}
			switch v.Verify.Verdict {
			case "pass", "partial", "fail":
				// ok — accepted final verdicts
			case "pending", "":
				return fmt.Errorf("verify.toml verdict is %q — fill in pass | partial | fail before finalizing", v.Verify.Verdict)
			default:
				return fmt.Errorf("verify.toml verdict %q is not one of pass | partial | fail", v.Verify.Verdict)
			}
			t, _ := verify.LoadTests(testsPath) // optional — may be absent under --skip-mutation manual cleanup
			recordVerifyOutcome(t, v)
			Printf("verify finalize: %s — verdict=%s recorded\n", phaseID, v.Verify.Verdict)
			return nil
		},
	}
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
		&mutation.Gremlins{
			Prefix:             prefix,
			ProjectRoot:        cwd,
			TimeoutCoefficient: p.Mutation.Gremlins.TimeoutCoefficient,
		},
		&mutation.StrykerNet{Prefix: prefix, ProjectRoot: cwd},
	}
}

// dockerPrefix returns the runtime command prefix for docker mode.
// For native, returns "". For docker, derives from runtime.test_command
// (which already has the right shape: "docker compose exec app pnpm test").
//
// We strip the trailing runner+args to get the prefix. Field-based
// (not substring) so a container name that happens to match a runner
// name (e.g. "docker compose exec node node test.js") doesn't fool us.
func dockerPrefix(p *project.Project) string {
	if p.Runtime.Mode != "docker" {
		return ""
	}
	tc := p.Runtime.TestCommand
	if !strings.HasPrefix(tc, "docker") {
		return "docker compose exec app"
	}
	runners := map[string]bool{
		"pnpm": true, "npm": true, "yarn": true, "bun": true,
		"node": true, "deno": true,
		"go": true, "make": true,
	}
	fields := strings.Fields(tc)
	// We need at minimum [docker, compose, exec, <service>] before any
	// runner, so start scanning from index 4.
	for i := 4; i < len(fields); i++ {
		if runners[fields[i]] {
			return strings.Join(fields[:i], " ")
		}
	}
	return "docker compose exec app"
}

// recordVerifyOutcome writes a telemetry outcome event capturing the
// shape of this verify run — verdict, mutation score, file/criterion
// counts. Never logs file paths or criterion text.
//
// t may be nil (e.g. when finalizing after tests.json was cleaned up
// or never written). When nil, falls back to the summary block in
// verify.toml, which the LLM populates during /dross-verify.
func recordVerifyOutcome(t *verify.Tests, v *verify.Verify) {
	counts := map[string]int{
		"criteria": len(v.Criteria),
		"findings": len(v.Findings),
	}
	nums := map[string]float64{}

	if t != nil {
		counts["languages"] = len(t.Languages)
		counts["skipped"] = len(t.Skipped)
		files := 0
		killed := 0
		survived := 0
		for _, lr := range t.Languages {
			files += len(lr.Files)
			if lr.Mutation != nil {
				killed += lr.Mutation.Killed
				survived += lr.Mutation.Survived
			}
		}
		counts["files"] = files
		counts["mutants_killed"] = killed
		counts["mutants_survived"] = survived
		if total := killed + survived; total > 0 {
			nums["mutation_score"] = float64(killed) / float64(total)
		}
	} else {
		counts["mutants_killed"] = v.Summary.MutantsKilled
		counts["mutants_survived"] = v.Summary.MutantsSurvived
		if v.Summary.MutationScore > 0 {
			nums["mutation_score"] = v.Summary.MutationScore
		}
	}

	tags := map[string]string{
		"verdict": v.Verify.Verdict,
	}
	RecordOutcomeEvent("verify", counts, nums, tags)
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
		Printf("  %s (%s): %d files — killed=%d survived=%d (not_covered=%d) timeout=%d errors=%d score=%.2f\n",
			lr.Name, lr.Tool, len(lr.Files), m.Killed, m.Survived, m.NotCovered, m.Timeout, m.Errors, m.Score)
		if m.NotCovered > 0 {
			// Show the gremlins-style efficacy (ignores NOT COVERED) when it
			// diverges meaningfully from dross's score. Often signals a
			// coverage blind spot — e.g. Go's package-init code in top-level
			// var arrays — rather than weak tests.
			efficacyDenom := m.Killed + (m.Survived - m.NotCovered)
			if efficacyDenom > 0 {
				efficacy := float64(m.Killed) / float64(efficacyDenom)
				Printf("    note: %d/%d mutants NOT COVERED — tests never ran them; efficacy excluding them = %.2f\n",
					m.NotCovered, m.Killed+m.Survived+m.Timeout, efficacy)
			}
		}
	}
	for _, s := range t.Skipped {
		Printf("  skipped %s — %s\n", s.File, s.Reason)
	}
	Printf("\nWrote tests.json + verify.toml (verdict=%s — /dross-verify will fill criterion mappings).\n", v.Verify.Verdict)
}
