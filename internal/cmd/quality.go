package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/quality"
)

// Quality registers `dross quality {detect,run,scaffold}` — the deterministic
// surface of the dross-quality audit. The audit orchestration itself (recon,
// analyzers, fan-out, calibrate-only refute-panel) lives in the quality.md
// prompt; these subcommands handle run-dir creation, analyzer detection, and the
// findings→spec.toml scaffold. Every write stays inside .dross/quality/.
func Quality() *cobra.Command {
	c := &cobra.Command{
		Use:   "quality",
		Short: "Deterministic surface of the dross-quality audit (run dirs, analyzer detection, scaffold)",
	}
	c.AddCommand(qualityDetect(), qualityRun(), qualityScaffold(), qualityFindings())
	return c
}

// qualityFindings builds the `dross quality findings` group, supplying the
// quality-specific descriptor: state.toml under .dross/quality, the run-dir
// ledger → reconcile-items adapter (keyed on Dimension), and the latest-run id
// resolver.
func qualityFindings() *cobra.Command {
	return newFindingsCmd(FindingsTool{
		Name:      "quality",
		StatePath: quality.StatePath,
		ItemsForRun: func(runDir string) ([]findings.Item, string, error) {
			ledgerPath, err := containedPath(runDir, "findings.toml")
			if err != nil {
				return nil, "", err
			}
			ledger, err := quality.Load(ledgerPath)
			if err != nil {
				return nil, "", err
			}
			return ledger.Items(), filepath.Base(runDir), nil
		},
		ResolveID: quality.ResolveItem,
	})
}

func qualityDetect() *cobra.Command {
	return &cobra.Command{
		Use:   "detect [path]",
		Short: "Detect languages and report installed vs missing analyzers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			m, err := quality.BuildManifest(pathArg(args), quality.LookPath)
			if err != nil {
				return err
			}
			Printf("languages: %s\n", strings.Join(m.Languages, ", "))
			Print("analyzers:")
			for _, t := range m.Tools {
				if t.Installed {
					Printf("  [installed] %s\n", t.Name)
					continue
				}
				Printf("  [missing]   %s  — %s\n", t.Name, t.Install)
			}
			return nil
		},
	}
}

func qualityRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run [path]",
		Short: "Create a quality run dir (.dross/quality/<id>) and write the tool-coverage manifest",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			runDir, err := quality.NewRun(root, time.Now().UTC(), quality.ShortSHA(repoDir))
			if err != nil {
				return err
			}
			// A missing analyzer never hard-errors: the run proceeds with partial
			// coverage and records what was skipped in the manifest.
			m, err := quality.BuildManifest(pathArg(args), quality.LookPath)
			if err != nil {
				return err
			}
			if err := writeQualityRunReport(runDir, m); err != nil {
				return err
			}
			// Stamp the store-level last_run so `dross status` can rank the
			// quality area by staleness; merges into state.toml without
			// disturbing the findings ledger.
			if err := findings.StampLastRun(quality.StatePath(root), time.Now().UTC()); err != nil {
				return err
			}
			Printf("quality run: %s\n", runDir)
			Printf("  analyzers: %d ran, %d skipped\n", len(m.Ran()), len(m.Skipped()))
			return nil
		},
	}
}

func qualityScaffold() *cobra.Command {
	var phaseID, title string
	c := &cobra.Command{
		Use:   "scaffold <run-dir>",
		Short: "Build a proposed remediation spec.toml from a run's findings.toml ledger",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runDir := args[0]
			ledgerPath, err := containedPath(runDir, "findings.toml")
			if err != nil {
				return err
			}
			ledger, err := quality.Load(ledgerPath)
			if err != nil {
				return err
			}
			outPath, err := containedPath(runDir, "spec.toml")
			if err != nil {
				return err
			}
			if err := quality.WriteScaffoldSpec(outPath, phaseID, title, ledger); err != nil {
				return err
			}
			Printf("scaffolded remediation spec: %s\n", outPath)
			return nil
		},
	}
	c.Flags().StringVar(&phaseID, "phase-id", "07-remediate-quality", "phase id for the scaffolded spec")
	c.Flags().StringVar(&title, "title", "Remediate quality findings", "title for the scaffolded spec")
	return c
}

// writeQualityRunReport writes the human report.md (with the tool-coverage
// manifest) into the run dir, through containedPath so it can never escape the
// sandbox.
func writeQualityRunReport(runDir string, m quality.Manifest) error {
	reportPath, err := containedPath(runDir, "report.md")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Code-quality audit report\n\n")
	fmt.Fprintf(&b, "Languages detected: %s\n\n", strings.Join(m.Languages, ", "))
	b.WriteString("## Tool coverage\n\n")
	for _, t := range m.Tools {
		status := "skipped (not installed)"
		if t.Installed {
			status = "ran"
		}
		fmt.Fprintf(&b, "- %s (%s) — %s\n", t.Name, t.Dimension, status)
	}
	b.WriteString("\n## Findings\n\n_(populated by the dross-quality audit, highest maintainability-risk first)_\n")
	return os.WriteFile(reportPath, []byte(b.String()), 0o644)
}
