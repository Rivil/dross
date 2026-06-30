package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/architecture"
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

			// auth_scheme is the GitLab credential selector: empty defaults to
			// private-token in code, so only a non-empty, non-recognised value
			// is a misconfiguration worth flagging.
			if scheme := strings.ToLower(p.Remote.AuthScheme); scheme != "" && scheme != "private-token" && scheme != "bearer" {
				Printf("  ✗ [remote].auth_scheme = %q is invalid (expected private-token | bearer)\n", p.Remote.AuthScheme)
				issues++
			}

			Print("")

			// --- [board] checks ---
			//
			// [board] is the single source for issue-board sync, independent
			// of [remote] (a repo can ship to one host and track issues on
			// another). Validate it only when something is configured — board
			// sync is opt-in, so an empty block is fine and stays silent.
			b := p.Board
			if b.Provider != "" || b.BaseURL != "" || b.AuthEnv != "" || b.Project != "" || b.Enabled || b.MilestoneMode != "" || len(b.StateMap) > 0 {
				Print("Board:")
				boardIssues := 0

				switch strings.ToLower(b.Provider) {
				case "forgejo", "gitea", "gitlab", "youtrack":
					// recognised
				default:
					Printf("  ✗ [board].provider = %q is invalid (expected forgejo | gitea | gitlab | youtrack)\n", b.Provider)
					boardIssues++
				}

				if b.AuthEnv == "" {
					Print("  ✗ [board].auth_env is not set (board ops read the token from this env var)")
					boardIssues++
				} else if v := os.Getenv(b.AuthEnv); v == "" {
					Printf("  ✗ [board].auth_env = %s but $%s is not set in this shell\n", b.AuthEnv, b.AuthEnv)
					boardIssues++
				}

				if !looksLikeBoardURL(b.BaseURL) {
					Printf("  ✗ [board].base_url = %q is not a valid URL (expected http(s)://host)\n", b.BaseURL)
					boardIssues++
				}

				// Empty milestone_mode defaults to "version" in code, so only a
				// non-empty, unrecognised value is a misconfiguration.
				switch b.MilestoneMode {
				case "", "version", "agile", "epic":
					// recognised (empty = version default)
				default:
					Printf("  ✗ [board].milestone_mode = %q is invalid (expected version | agile | epic)\n", b.MilestoneMode)
					boardIssues++
				}

				if boardIssues == 0 {
					Printf("  ✓ [board] is well-formed (%s @ %s)\n", b.Provider, b.BaseURL)
				}
				issues += boardIssues
				Print("")
			}

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

			// --- Phase work on main ---
			//
			// Phase commits should live on phase/<id> branches, not on
			// main. Legacy projects (or anyone using --no-branch) may
			// have accumulated phase commits on local main; flag them
			// so the user can migrate to the branch model before the
			// next ship.
			mainBranch := p.Repo.GitMainBranch
			if mainBranch == "" {
				mainBranch = "main"
			}
			Print("Phase branch hygiene:")
			leaked, err := phaseCommitsOnMain(root, repoDir, mainBranch)
			switch {
			case err != nil:
				Printf("  ⚠ couldn't check phase commits on %s: %v\n", mainBranch, err)
				// not a hard issue — most likely no origin configured yet
			case len(leaked) > 0:
				Printf("  ⚠ %d phase commit(s) found on local %s ahead of origin/%s:\n",
					len(leaked), mainBranch, mainBranch)
				for _, c := range leaked {
					Printf("      %s  (recorded in phase %s)\n", c.sha[:short7], c.phaseID)
				}
				Print("    Fix: move them to a phase branch, e.g.")
				Printf("      git branch phase/<id> %s && git reset --hard origin/%s\n", mainBranch, mainBranch)
				issues++
			default:
				Printf("  ✓ no recorded phase commits on local %s\n", mainBranch)
			}
			Print("")

			// --- Architecture links ---
			//
			// ARCHITECTURE.md's `Symbol — file:line` bullets go stale as code
			// moves. Surface stale links ADVISORILY: this never touches `issues`
			// and never blocks the loop (the doc is best-effort; `dross
			// architecture check --fix` repairs it). A repo with no
			// ARCHITECTURE.md gets no section at all.
			if warnings, present := architectureLinkWarnings(repoDir); present {
				Print("Architecture links:")
				if len(warnings) == 0 {
					Print("  ✓ all ARCHITECTURE.md symbol links resolve")
				} else {
					for _, w := range warnings {
						Printf("  ⚠ %s\n", w)
					}
					Print("    Advisory only. Fix: `dross architecture check --fix` (or /dross-architecture to refresh).")
				}
				Print("")
			}

			// --- Interaction coverage ---
			//
			// Fail-closed classification of every command-backed prompt:
			// interactive ones (AskUserQuestion shim) must have a `### dross-<name>`
			// audit section; non-interactive ones must be enrolled in the doc's
			// `## Exempt` list. Reuses the same classifier the Go-test gate
			// (interaction_coverage_test.go) runs — that test is the enforcing
			// gate; this surfaces the same verdict on demand. It fires only in the
			// dross source tree (docs/interaction-audit.md is not embedded) and
			// stays silent in other onboarded projects.
			if warnings, present := interactionCoverageWarnings(repoDir); present {
				Print("Interaction coverage:")
				if len(warnings) == 0 {
					Print("  ✓ every command-backed prompt is sectioned or exempt")
				} else {
					for _, w := range warnings {
						Printf("  ✗ %s\n", w)
						issues++
					}
					Print("    Fix: add a `### dross-<name>` audit section (interactive) or an `## Exempt` entry (non-interactive) in docs/interaction-audit.md.")
				}
				Print("")
			}

			return finalizeDoctor(issues)
		},
	}
}

// architectureLinkWarnings resolves every symbol link in ARCHITECTURE.md against
// the live code (paths pinned to repoDir) and returns one advisory warning per
// Moved/Unresolved bullet. present=false means there is no ARCHITECTURE.md — the
// caller emits no section and no error. Ambiguous and Skipped links never warn:
// a duplicate name or a language codex can't index is not "stale".
func architectureLinkWarnings(repoDir string) (warnings []string, present bool) {
	body, err := os.ReadFile(filepath.Join(repoDir, architecture.File))
	if err != nil {
		return nil, false // absent (or unreadable) → no section
	}
	for _, r := range architecture.ResolveAllIn(string(body), repoDir) {
		switch r.Status {
		case architecture.StatusMoved:
			warnings = append(warnings, fmt.Sprintf("%s — %s:%d moved to line %d", r.Link.Symbol, r.Link.File, r.Link.Line, r.NewLine))
		case architecture.StatusUnresolved:
			warnings = append(warnings, fmt.Sprintf("%s — %s:%d no longer resolves", r.Link.Symbol, r.Link.File, r.Link.Line))
		}
	}
	return warnings, true
}

// interactionCoverageWarnings runs the interaction-contract coverage classifier
// — the same one the Go-test gate uses — against the dross source tree and
// returns one warning per unclassified command-backed prompt. present=false means
// repoDir is not the dross source tree: docs/interaction-audit.md is absent (it is
// not embedded, so the classifier has nothing to read in other onboarded
// projects), and the caller emits no section. The Go test is the enforcing gate;
// this lint only surfaces the same classification on demand inside the dross repo.
func interactionCoverageWarnings(repoDir string) (warnings []string, present bool) {
	if _, err := os.Stat(filepath.Join(repoDir, "docs", "interaction-audit.md")); err != nil {
		return nil, false // not the dross source tree → no section
	}
	res, err := interactionCoverage(repoDir)
	if err != nil {
		return nil, false // partial/malformed tree → skip silently
	}
	for _, gap := range res.Uncovered {
		warnings = append(warnings, fmt.Sprintf("%s — %s", gap.Name, gap.Reason))
	}
	return warnings, true
}

// looksLikeBoardURL reports whether s is a plausible instance base URL: an
// absolute http(s) URL with a host. Shape-only — it doesn't dial the host.
func looksLikeBoardURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
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

// leakedPhaseCommit pairs a phase commit SHA with the phase id whose
// changes.json recorded it. Returned by phaseCommitsOnMain.
type leakedPhaseCommit struct {
	sha     string
	phaseID string
}

// short7 caps the SHA preview in doctor output. 7 chars is enough to
// disambiguate in any realistic repo.
const short7 = 7

// phaseCommitsOnMain returns the commits between origin/<mainBranch>
// and local <mainBranch> that match any recorded task commit in
// .dross/phases/*/changes.json. An empty result means main is clean
// — either it's at origin or its ahead-commits aren't phase work.
//
// Returns an error if origin/<mainBranch> isn't reachable (no origin
// configured, never pushed). Caller should treat that as a soft skip
// rather than an issue: nothing to leak if there's no upstream.
func phaseCommitsOnMain(root, repoDir, mainBranch string) ([]leakedPhaseCommit, error) {
	// Collect all recorded phase commit SHAs.
	phasesDir := filepath.Join(root, "phases")
	entries, err := os.ReadDir(phasesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	recorded := make(map[string]string) // sha → phaseID
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		phaseID := e.Name()
		body, err := os.ReadFile(filepath.Join(phasesDir, phaseID, "changes.json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read changes.json for %s: %w", phaseID, err)
		}
		for _, sha := range extractCommitSHAs(string(body)) {
			recorded[sha] = phaseID
		}
	}
	if len(recorded) == 0 {
		return nil, nil
	}

	// List commits on local main not in origin/main.
	out, err := exec.Command("git", "-C", repoDir,
		"rev-list", "origin/"+mainBranch+".."+mainBranch).Output()
	if err != nil {
		return nil, err
	}
	var leaked []leakedPhaseCommit
	for _, sha := range strings.Fields(string(out)) {
		if pid, ok := recorded[sha]; ok {
			leaked = append(leaked, leakedPhaseCommit{sha: sha, phaseID: pid})
		}
	}
	return leaked, nil
}

// extractCommitSHAs pulls all `"commit": "<sha>"` values out of a
// changes.json body. Cheap regex-style scan rather than a full JSON
// parse — keeps doctor from coupling to the changes/ package shape.
func extractCommitSHAs(body string) []string {
	var out []string
	const key = `"commit":`
	for i := 0; i < len(body); {
		j := strings.Index(body[i:], key)
		if j < 0 {
			break
		}
		j += i + len(key)
		// Skip whitespace and the opening quote.
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '"') {
			j++
		}
		k := j
		for k < len(body) && body[k] != '"' {
			k++
		}
		if k > j {
			out = append(out, body[j:k])
		}
		i = k
	}
	return out
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
