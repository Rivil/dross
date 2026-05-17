package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Doctor checks project-level health for the current dross repo.
//
// Distinct from `make doctor` (which checks the dross dev install).
// This runs inside any dross-onboarded project to surface drift between
// what's recorded in project.toml and what's actually true on disk.
//
// Exit code is non-zero on any issue so it can gate CI / pre-push hooks.
func Doctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check project-level health (.dross/project.toml vs reality)",
		RunE: func(c *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root) // root is .dross; parent is repo cwd
			issues := 0

			// --- Foundational files ---
			//
			// project.toml + rules.toml + state.json must exist before
			// loadProject can succeed. Surface their absence with a
			// remediation hint — most common cause is a botched recovery
			// after a legacy .dross/-stripping ship.
			Print("Foundational files:")
			missing := checkFoundationalFiles(root)
			if len(missing) > 0 {
				for _, m := range missing {
					Printf("  ✗ %s — missing. If a recent squash-merge wiped .dross/, run `dross ship recover` to restore from the pre-merge HEAD.\n", m)
					issues++
				}
				Print("")
				return finalizeDoctor(issues)
			}
			Print("  ✓ project.toml, rules.toml, state.json present")
			Print("")

			p, _, err := loadProject()
			if err != nil {
				return err
			}

			// --- [remote] checks ---
			gitURL := gitRemoteOriginURL(repoDir)

			Print("Remote:")
			switch {
			case gitURL == "" && p.Remote.URL == "":
				Print("  ✓ no git origin and no [remote] configured — fine for greenfield")
			case gitURL == "" && p.Remote.URL != "":
				Printf("  ⚠ project.toml has [remote].url = %s but git has no origin — push to remote, or remove the config\n", p.Remote.URL)
				issues++
			case gitURL != "" && p.Remote.URL == "":
				Printf("  ✗ git origin = %s but project.toml has no [remote] — run /dross-onboard or set manually\n", gitURL)
				issues++
			default:
				if !sameRemoteURL(gitURL, p.Remote.URL) {
					Printf("  ⚠ git origin (%s) does not match [remote].url (%s)\n", gitURL, p.Remote.URL)
					issues++
				} else {
					Printf("  ✓ git origin matches [remote].url (%s)\n", p.Remote.URL)
				}
			}

			if p.Remote.AuthEnv != "" {
				if v := os.Getenv(p.Remote.AuthEnv); v == "" {
					Printf("  ✗ [remote].auth_env = %s but $%s is not set in this shell\n", p.Remote.AuthEnv, p.Remote.AuthEnv)
					issues++
				} else {
					Printf("  ✓ $%s is set (length %d)\n", p.Remote.AuthEnv, len(v))
				}
			}

			Print("")

			// --- .gitattributes ---
			//
			// Without `.dross/** linguist-generated=true`, planning artefacts
			// flood reviewer diffs on every PR. New projects get this for
			// free from `dross init`/`dross onboard`; legacy projects need
			// it added explicitly.
			Print(".gitattributes:")
			if ok, err := drossLinguistAttrPresent(repoDir); err != nil {
				Printf("  ⚠ couldn't read .gitattributes: %v\n", err)
				issues++
			} else if !ok {
				Printf("  ⚠ .dross/ is not marked linguist-generated — PR reviews will see planning noise.\n")
				Printf("    Fix: append `%s` to .gitattributes (or rerun `dross init` to auto-scaffold).\n", drossGitattributesLine)
				issues++
			} else {
				Print("  ✓ .dross/ is marked linguist-generated")
			}
			Print("")

			return finalizeDoctor(issues)
		},
	}
}

// finalizeDoctor records the doctor outcome event and returns the
// appropriate error (or nil) for the issue count. Shared between the
// foundational-files short-circuit path and the full-check path so the
// telemetry shape stays consistent.
func finalizeDoctor(issues int) error {
	result := "passed"
	if issues > 0 {
		result = "issues_found"
	}
	RecordOutcomeEvent("doctor",
		map[string]int{"issues": issues},
		nil,
		map[string]string{"result": result},
	)
	if issues == 0 {
		Print("All project-level checks passed.")
		return nil
	}
	return fmt.Errorf("%d project-level issue(s) found", issues)
}

// checkFoundationalFiles returns the list of missing foundational files
// (relative paths) that loadProject would otherwise crash on. Empty
// slice means the trio is intact.
func checkFoundationalFiles(root string) []string {
	var missing []string
	for _, rel := range []string{"project.toml", "rules.toml", "state.json"} {
		if _, err := os.Stat(filepath.Join(root, rel)); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, ".dross/"+rel)
		}
	}
	return missing
}

// drossLinguistAttrPresent returns whether .gitattributes covers .dross/
// with linguist-generated=true. Missing file → not present. Read error
// (permissions etc.) → propagated.
func drossLinguistAttrPresent(repoDir string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(repoDir, ".gitattributes"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasDrossLinguistLine(string(body)), nil
}

// sameRemoteURL compares two git remote forms loosely — strips trailing
// .git and compares hostname + path. Avoids false-positives from "the
// same remote in https vs ssh form".
func sameRemoteURL(a, b string) bool {
	hA, pA := parseGitForCompare(a)
	hB, pB := parseGitForCompare(b)
	return hA == hB && pA == pB
}

func parseGitForCompare(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".git")
	// Reuse parseGitRemote semantics by lifting from the project pkg
	// indirectly: short-and-dumb host:path extractor here keeps the
	// dependency direction right (cmd → project, never project → cmd).
	if !strings.Contains(raw, "://") && strings.Contains(raw, "@") && strings.Contains(raw, ":") {
		afterAt := raw[strings.Index(raw, "@")+1:]
		colon := strings.Index(afterAt, ":")
		if colon < 0 {
			return "", ""
		}
		return afterAt[:colon], afterAt[colon+1:]
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return rest, ""
		}
		return rest[:slash], rest[slash+1:]
	}
	return "", ""
}
