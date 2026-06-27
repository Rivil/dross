package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/findings"
	"github.com/Rivil/dross/internal/security"
)

// Security registers `dross security {detect,run,scaffold}` — the deterministic
// surface of the dross-secure audit. The audit orchestration itself (recon,
// scanners, fan-out, refute-panel) lives in the secure.md prompt; these
// subcommands handle run-dir creation, scanner detection, and the
// findings→spec.toml scaffold. Every write stays inside .dross/security/.
func Security() *cobra.Command {
	c := &cobra.Command{
		Use:   "security",
		Short: "Deterministic surface of the dross-secure audit (run dirs, scanner detection, scaffold)",
	}
	c.AddCommand(securityDetect(), securityRun(), securityScaffold(), securityFindings())
	return c
}

// securityFindings builds the `dross security findings` group, supplying the
// security-specific descriptor: state.toml under .dross/security, the run-dir
// ledger → reconcile-items adapter (keyed on Class), and the latest-run id
// resolver.
func securityFindings() *cobra.Command {
	return newFindingsCmd(FindingsTool{
		Name:      "security",
		StatePath: security.StatePath,
		ItemsForRun: func(runDir string) ([]findings.Item, string, error) {
			ledgerPath, err := containedPath(runDir, "findings.toml")
			if err != nil {
				return nil, "", err
			}
			ledger, err := security.Load(ledgerPath)
			if err != nil {
				return nil, "", err
			}
			return ledger.Items(), filepath.Base(runDir), nil
		},
		ResolveID: security.ResolveItem,
	})
}

func securityDetect() *cobra.Command {
	return &cobra.Command{
		Use:   "detect [path]",
		Short: "Detect languages and report installed vs missing scanners",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			m, err := security.BuildManifest(pathArg(args), security.LookPath)
			if err != nil {
				return err
			}
			Printf("languages: %s\n", strings.Join(m.Languages, ", "))
			Print("scanners:")
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

func securityRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run [path]",
		Short: "Create a security run dir (.dross/security/<id>) and write the tool-coverage manifest",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			runDir, err := security.NewRun(root, time.Now().UTC(), security.ShortSHA(repoDir))
			if err != nil {
				return err
			}
			// A missing scanner never hard-errors: the run proceeds with partial
			// coverage and records what was skipped in the manifest.
			m, err := security.BuildManifest(pathArg(args), security.LookPath)
			if err != nil {
				return err
			}
			if err := writeRunReport(runDir, m); err != nil {
				return err
			}
			Printf("security run: %s\n", runDir)
			Printf("  scanners: %d ran, %d skipped\n", len(m.Ran()), len(m.Skipped()))
			return nil
		},
	}
}

func securityScaffold() *cobra.Command {
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
			ledger, err := security.Load(ledgerPath)
			if err != nil {
				return err
			}
			outPath, err := containedPath(runDir, "spec.toml")
			if err != nil {
				return err
			}
			if err := security.WriteScaffoldSpec(outPath, phaseID, title, ledger); err != nil {
				return err
			}
			Printf("scaffolded remediation spec: %s\n", outPath)
			return nil
		},
	}
	c.Flags().StringVar(&phaseID, "phase-id", "06-remediate-security", "phase id for the scaffolded spec")
	c.Flags().StringVar(&title, "title", "Remediate security findings", "title for the scaffolded spec")
	return c
}

// pathArg returns the optional positional path, defaulting to the current dir.
func pathArg(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

// containedPath joins name onto runDir and guarantees the result stays inside
// runDir. A finding-derived name like "../main.go" is refused, so a run can never
// write outside its sandbox — the command is read-only with respect to the rest
// of the repo.
func containedPath(runDir, name string) (string, error) {
	p := filepath.Join(runDir, name)
	rel, err := filepath.Rel(runDir, p)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the run directory", name)
	}
	return p, nil
}

// writeRunReport writes the human report.md (with the tool-coverage manifest) into
// the run dir, through containedPath so it can never escape the sandbox.
func writeRunReport(runDir string, m security.Manifest) error {
	reportPath, err := containedPath(runDir, "report.md")
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Security audit report\n\n")
	fmt.Fprintf(&b, "Languages detected: %s\n\n", strings.Join(m.Languages, ", "))
	b.WriteString("## Tool coverage\n\n")
	for _, t := range m.Tools {
		status := "skipped (not installed)"
		if t.Installed {
			status = "ran"
		}
		fmt.Fprintf(&b, "- %s — %s\n", t.Name, status)
	}
	b.WriteString("\n## Findings\n\n_(populated by the dross-secure audit)_\n")
	return os.WriteFile(reportPath, []byte(b.String()), 0o644)
}
