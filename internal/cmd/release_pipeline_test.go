package cmd

import (
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

// govulncheckPins returns every pinned govulncheck install version in a
// workflow, so an unpinned (`@latest`) or version-drifted install is visible.
func govulncheckPins(workflow string) []string {
	re := regexp.MustCompile(`golang\.org/x/vuln/cmd/govulncheck@(\S+)`)
	var pins []string
	for _, m := range re.FindAllStringSubmatch(workflow, -1) {
		pins = append(pins, m[1])
	}
	return pins
}

// TestGovulncheckPinAgrees pins that ci.yml and release.yml install the same
// govulncheck version, and that it is a version, not a floating tag: two pins
// would mean the release could be scanned by a different scanner than the one
// that gated the PR.
func TestGovulncheckPinAgrees(t *testing.T) {
	ci := govulncheckPins(readRepoFile(t, ".github/workflows/ci.yml"))
	rel := govulncheckPins(readRepoFile(t, ".github/workflows/release.yml"))
	if len(ci) == 0 || len(rel) == 0 {
		t.Fatalf("govulncheck install missing: ci.yml pins %v, release.yml pins %v", ci, rel)
	}
	all := append(append([]string{}, ci...), rel...)
	for _, pin := range all {
		if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(pin) {
			t.Errorf("govulncheck pin %q is not an exact version — @latest or a branch lets a compromised release walk in on the next run", pin)
		}
		if pin != ci[0] {
			t.Errorf("govulncheck pins disagree: ci.yml %v vs release.yml %v — the release must be scanned by the scanner that gated the PR", ci, rel)
		}
	}
}
