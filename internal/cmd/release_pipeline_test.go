package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// The release job must build from exactly the module graph it scanned. Three
// properties make that true by construction, and each is pinned here because a
// workflow refactor could drop any one of them without a test noticing (YAML
// has no mutation adapter): the job's GOFLAGS forbid graph mutation, goreleaser
// has no before-hook that would rewrite go.mod/go.sum, and govulncheck runs in
// the release job itself — before goreleaser, ungated, pinned to the same
// version CI uses. Phase supply-chain-currency, criterion c-4.

// TestReleaseJobBuildsReadonly pins GOFLAGS=-mod=readonly on the release job.
// Without it a compromised step could rewrite the graph between the scan and
// the build, and the scanned graph would no longer be the shipped one.
func TestReleaseJobBuildsReadonly(t *testing.T) {
	w := readRepoFile(t, ".github/workflows/release.yml")
	if !regexp.MustCompile(`(?m)^\s+GOFLAGS:\s*-mod=readonly\s*$`).MatchString(w) {
		t.Fatal("release.yml job env lacks `GOFLAGS: -mod=readonly` — the graph govulncheck scans is not provably the graph goreleaser builds")
	}
}

// TestGoreleaserHasNoBeforeHooks pins the absence of any `before:` hook. The
// old `go mod tidy` hook rewrote the module graph at release time, which is
// both what -mod=readonly forbids and what would decouple the scanned graph
// from the built one.
func TestGoreleaserHasNoBeforeHooks(t *testing.T) {
	y := readRepoFile(t, ".goreleaser.yaml")
	for i, line := range strings.Split(y, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, "before:") {
			t.Fatalf(".goreleaser.yaml:%d carries a `before:` hook — release-time hooks may rewrite the module graph after it was scanned", i+1)
		}
	}
}

// TestReleaseJobRunsGovulncheck pins the release-job scan: a `govulncheck ./...`
// run step exists, it precedes the goreleaser step, and nothing softens its
// exit status. A finding at release time fails the release — no allowlist,
// no `|| true` (locked decision release_govulncheck_failure).
func TestReleaseJobRunsGovulncheck(t *testing.T) {
	w := readRepoFile(t, ".github/workflows/release.yml")
	scan, goreleaser := -1, -1
	for i, raw := range strings.Split(w, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "run: govulncheck ./...") {
			if scan >= 0 {
				t.Fatalf("release.yml:%d second `govulncheck ./...` step (first at line %d)", i+1, scan+1)
			}
			scan = i
			if strings.Contains(trimmed, "|| true") || strings.Contains(trimmed, "||true") {
				t.Errorf("release.yml:%d govulncheck step is softened with `|| true` — a finding must fail the release", i+1)
			}
		}
		if strings.HasPrefix(trimmed, "- uses: goreleaser/goreleaser-action@") && goreleaser < 0 {
			goreleaser = i
		}
	}
	if scan < 0 {
		t.Fatal("release.yml has no `run: govulncheck ./...` step — the shipped graph is never scanned in the release job")
	}
	if goreleaser < 0 {
		t.Fatal("release.yml has no goreleaser step")
	}
	if scan > goreleaser {
		t.Fatalf("release.yml runs govulncheck (line %d) after goreleaser (line %d) — the scan must precede the build it vouches for", scan+1, goreleaser+1)
	}
}

// govulncheckInstall is a govulncheck version declaration.
var govulncheckInstall = regexp.MustCompile(`golang\.org/x/vuln/cmd/govulncheck@(\S+)`)

// govulncheckDeclarationProblems sweeps every workflow and composite action
// under root and reports anything but exactly one govulncheck declaration at an
// exact vX.Y.Z. Comments are not declarations.
func govulncheckDeclarationProblems(t *testing.T, root string) []string {
	t.Helper()
	var where, versions []string
	for _, rel := range githubYAMLFiles(t, root) {
		for i, raw := range strings.Split(readTreeFile(t, root, rel), "\n") {
			line, _ := splitYAMLComment(raw)
			for _, m := range govulncheckInstall.FindAllStringSubmatch(line, -1) {
				where = append(where, fmt.Sprintf("%s:%d", rel, i+1))
				versions = append(versions, m[1])
			}
		}
	}
	if len(where) != 1 {
		return []string{fmt.Sprintf("govulncheck is declared %d times (%v), want exactly once — two declarations let the release be scanned by a different scanner than the one that gated the PR", len(where), where)}
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(versions[0]) {
		return []string{fmt.Sprintf("%s: govulncheck pin %q is not an exact version — @latest or a branch lets a compromised release walk in on the next run", where[0], versions[0])}
	}
	return nil
}

// jobLines returns the lines of one job under a workflow's top-level `jobs:`
// map, and the 0-based index of the job's key line. ok is false when the job
// is absent.
func jobLines(workflow, job string) (lines []string, at int, ok bool) {
	all := strings.Split(workflow, "\n")
	inJobs := false
	for i, raw := range all {
		if raw == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs || raw != "  "+job+":" {
			continue
		}
		end := i + 1
		for ; end < len(all); end++ {
			trimmed := strings.TrimSpace(all[end])
			indent := len(all[end]) - len(strings.TrimLeft(all[end], " "))
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") && indent <= 2 {
				break
			}
		}
		return all[i:end], i, true
	}
	return nil, 0, false
}

// TestGovulncheckDeclaredOnce pins govulncheck's version to one declaration —
// the local composite action — and both scanning jobs to it: ci.yml's test job
// and release.yml's release job each use the action before they run the scan.
// Replaces TestGovulncheckPinAgrees, which compared two copies that no longer
// exist. Phase run-block-pin-currency, criterion c-5.
func TestGovulncheckDeclaredOnce(t *testing.T) {
	for _, p := range govulncheckDeclarationProblems(t, repoRootFromTest(t)) {
		t.Error(p)
	}

	for _, tc := range []struct{ file, job string }{
		{".github/workflows/ci.yml", "test"},
		{".github/workflows/release.yml", "release"},
	} {
		lines, at, ok := jobLines(readRepoFile(t, tc.file), tc.job)
		if !ok {
			t.Errorf("%s has no %s job", tc.file, tc.job)
			continue
		}
		uses, scan := -1, -1
		for i, raw := range lines {
			v, _ := splitYAMLComment(raw)
			trimmed := strings.TrimPrefix(strings.TrimSpace(v), "- ")
			if trimmed == "uses: ./.github/actions/govulncheck" && uses < 0 {
				uses = i
			}
			if strings.HasPrefix(trimmed, "run: govulncheck ./...") && scan < 0 {
				scan = i
			}
		}
		switch {
		case uses < 0:
			t.Errorf("%s %s job never uses ./.github/actions/govulncheck — it would scan with an undeclared govulncheck", tc.file, tc.job)
		case scan < 0:
			t.Errorf("%s %s job never runs `govulncheck ./...`", tc.file, tc.job)
		case scan < uses:
			t.Errorf("%s:%d runs govulncheck before the install at line %d", tc.file, at+scan+1, at+uses+1)
		}
	}

	t.Run("a second declaration fails", func(t *testing.T) {
		root := writeTreeFiles(t, map[string]string{
			".github/actions/govulncheck/action.yml": "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0\n",
			".github/workflows/ci.yml":               "jobs:\n  test:\n    steps:\n      # go install golang.org/x/vuln/cmd/govulncheck@v0.0.1 (a comment, not a declaration)\n      - run: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0\n",
		})
		if p := govulncheckDeclarationProblems(t, root); len(p) != 1 || !strings.Contains(p[0], "declared 2 times") {
			t.Fatalf("two declarations: got %q, want one problem naming 2 declarations", p)
		}
	})
	t.Run("a floating version fails", func(t *testing.T) {
		root := writeTreeFiles(t, map[string]string{
			".github/actions/govulncheck/action.yml": "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: go install golang.org/x/vuln/cmd/govulncheck@latest\n",
		})
		if p := govulncheckDeclarationProblems(t, root); len(p) != 1 || !strings.Contains(p[0], "not an exact version") {
			t.Fatalf("@latest: got %q, want one not-exact problem", p)
		}
	})
}
