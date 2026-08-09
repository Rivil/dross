package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The release tag is computed on a bare runner, before setup-go and with no
// dross binary on it — so the script that computes it can depend on nothing but
// a POSIX shell and coreutils. These tests run it exactly that way: a temp dir
// holding only a fixture .dross/project.toml, and a PATH narrowed to the system
// directories so a jq or dross that happens to be installed on the developer's
// machine cannot quietly satisfy a dependency CI would not have.

// runReleaseVersion executes scripts/release-version.sh against a fixture
// project.toml and returns stdout, stderr and the error.
func runReleaseVersion(t *testing.T, projectTOML string) (string, string, error) {
	t.Helper()
	script := filepath.Join(repoRootFromTest(t), "scripts", "release-version.sh")
	dir := t.TempDir()
	if projectTOML != "" {
		root := filepath.Join(dir, RootDirName)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "project.toml"), []byte(projectTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // no jq, no dross
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

const releaseFixture = `[project]
  name = "dross"
  version = "1.2.3.4"
  created = "2026-06-19"
`

// TestReleaseVersionScriptResolvesTag: the 4-part project version projects to
// the 3-part semver tag, with no state.json anywhere and nothing on PATH beyond
// the system directories.
func TestReleaseVersionScriptResolvesTag(t *testing.T) {
	out, errOut, err := runReleaseVersion(t, releaseFixture)
	if err != nil {
		t.Fatalf("script failed (%v); stderr:\n%s", err, errOut)
	}
	if got := strings.TrimSpace(out); got != "v1.2.3" {
		t.Errorf("tag = %q, want %q (stderr: %s)", got, "v1.2.3", errOut)
	}
}

// TestReleaseVersionScriptRejectsMissingVersion: a [project] table with no
// version is a hard error naming the file, not a partial tag on stdout — a
// silently-empty tag would make the release step tag the wrong thing.
func TestReleaseVersionScriptRejectsMissingVersion(t *testing.T) {
	out, errOut, err := runReleaseVersion(t, "[project]\n  name = \"dross\"\n")
	if err == nil {
		t.Fatalf("script should fail with no [project].version; stdout: %q", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("nothing should reach stdout on the error path, got %q", out)
	}
	if !strings.Contains(errOut, ".dross/project.toml") {
		t.Errorf("stderr should name .dross/project.toml, got %q", errOut)
	}
}

// TestReleaseVersionScriptIgnoresOtherTables: the key lookup is anchored to the
// [project] table. An unanchored grep returns whichever version line comes
// first, which is how a [stack] version would end up as the release tag.
func TestReleaseVersionScriptIgnoresOtherTables(t *testing.T) {
	fixture := releaseFixture + "\n[stack]\n  version = \"9.9.9.9\"\n  languages = [\"go\"]\n"
	out, errOut, err := runReleaseVersion(t, fixture)
	if err != nil {
		t.Fatalf("script failed (%v); stderr:\n%s", err, errOut)
	}
	if got := strings.TrimSpace(out); got != "v1.2.3" {
		t.Errorf("tag = %q, want %q — [stack].version is not the project's", got, "v1.2.3")
	}
}

// TestReleaseVersionScriptRejectsMissingFile: no .dross/ at all is an error
// naming the file, never an empty tag.
func TestReleaseVersionScriptRejectsMissingFile(t *testing.T) {
	out, errOut, err := runReleaseVersion(t, "")
	if err == nil {
		t.Fatalf("script should fail with no project.toml; stdout: %q", out)
	}
	if !strings.Contains(errOut, ".dross/project.toml") {
		t.Errorf("stderr should name .dross/project.toml, got %q", errOut)
	}
}

// TestReleaseWorkflowHasNoStateJSONRead: release.yml must not reach for
// state.json — it is gitignored, so a CI checkout of main has no copy of it —
// nor for jq, which only ever existed to read it.
func TestReleaseWorkflowHasNoStateJSONRead(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(b)
	for _, banned := range []string{"state.json", "jq "} {
		if strings.Contains(yaml, banned) {
			t.Errorf("release.yml still references %q — the version comes from project.toml now", banned)
		}
	}
	if !strings.Contains(yaml, "scripts/release-version.sh") {
		t.Error("release.yml should compute its tag via scripts/release-version.sh")
	}
}

// TestCIShellcheckCoversScripts: a shell script nobody lints is a shell script
// that breaks the release on a runner nobody was watching.
func TestCIShellcheckCoversScripts(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(b)
	var line string
	for _, l := range strings.Split(yaml, "\n") {
		if strings.Contains(l, "shellcheck ") && strings.Contains(l, "run:") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("ci.yml has no shellcheck invocation")
	}
	for _, want := range []string{"install.sh", "scripts/"} {
		if !strings.Contains(line, want) {
			t.Errorf("shellcheck invocation %q does not cover %q", strings.TrimSpace(line), want)
		}
	}
}
