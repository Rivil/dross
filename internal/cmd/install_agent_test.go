package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/assets"
)

func embeddedReviewer(t *testing.T) []byte {
	t.Helper()
	b, err := assets.FS.ReadFile("agents/" + reviewerFile)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeSource is a source checkout's assets/ holding the reviewer definition.
func fakeSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "agents", reviewerFile), embeddedReviewer(t), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestInstallReviewer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	dst := filepath.Join(dir, reviewerFile)

	if err := syncAgents(dir, false, "", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dst)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copy mode left %v (err %v), want a regular file", fi, err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, embeddedReviewer(t)) {
		t.Fatal("copy mode did not write the embedded bytes")
	}

	src := fakeSource(t)
	if err := syncAgents(dir, true, src, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(dst)
	if err != nil || target != filepath.Join(src, "agents", reviewerFile) {
		t.Fatalf("link mode: readlink = %q, %v; want a symlink to %s", target, err, filepath.Join(src, "agents", reviewerFile))
	}

	if err := syncAgents(dir, false, "", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("switching back to copy mode left the symlink")
	}
}

func TestInstallReviewerIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	for i := 0; i < 2; i++ {
		if err := syncAgents(dir, false, "", func(string, ...any) {}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, reviewerFile)); !bytes.Equal(got, embeddedReviewer(t)) {
		t.Fatal("a second install changed the definition's bytes")
	}
}

func TestInstallAgentPrune(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "my-agent.md")
	stale := filepath.Join(dir, "dross-old.md")
	for _, p := range []string{foreign, stale} {
		if err := os.WriteFile(p, []byte("---\nname: x\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(foreign, old, old); err != nil {
		t.Fatal(err)
	}
	if err := syncAgents(dir, false, "", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(foreign)
	if err != nil || !fi.ModTime().Equal(old) {
		t.Fatalf("a foreign agent was touched: %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "---\nname: x\n---\n" {
		t.Fatal("a foreign agent's bytes changed")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a dross-* agent this version does not ship survived: %v", err)
	}
}

func TestReviewerStatus(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, dir, repo string)
		want  reviewerState
	}{
		{"fresh install", func(t *testing.T, dir, repo string) {}, reviewerOK},
		{"absent", func(t *testing.T, dir, repo string) {
			if err := os.Remove(filepath.Join(dir, reviewerFile)); err != nil {
				t.Fatal(err)
			}
		}, reviewerMissing},
		{"dangling symlink", func(t *testing.T, dir, repo string) {
			p := filepath.Join(dir, reviewerFile)
			_ = os.Remove(p)
			if err := os.Symlink(filepath.Join(t.TempDir(), "gone.md"), p); err != nil {
				t.Fatal(err)
			}
		}, reviewerMissing},
		{"one byte changed", func(t *testing.T, dir, repo string) {
			b := append([]byte{}, embeddedReviewer(t)...)
			b[len(b)-2] ^= 1
			if err := os.WriteFile(filepath.Join(dir, reviewerFile), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}, reviewerStale},
		{"repo copy shadows it", func(t *testing.T, dir, repo string) {
			p := filepath.Join(repo, ".claude", "agents", reviewerFile)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("---\nname: dross-task-reviewer\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, reviewerShadowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agents")
			repo := t.TempDir()
			if err := syncAgents(dir, false, "", func(string, ...any) {}); err != nil {
				t.Fatal(err)
			}
			c.setup(t, dir, repo)
			got := reviewerStatusIn(dir, repo)
			if got.State != c.want {
				t.Fatalf("state = %s, want %s", got.State, c.want)
			}
			if c.want == reviewerShadowed && !strings.Contains(got.Problem()+got.Remedy(), filepath.Join(repo, ".claude", "agents", reviewerFile)) {
				t.Fatalf("a shadowed status does not name the repo file: %q / %q", got.Problem(), got.Remedy())
			}
			if c.want != reviewerOK && got.Remedy() == "" {
				t.Fatal("a not-ok status names no remedy")
			}
		})
	}
}

// TestReviewerDirAgreement: install writes where Claude Code reads, and the
// readiness check reads where install wrote.
func TestReviewerDirAgreement(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	chdirOnly(t, t.TempDir())
	if err := runCmd(t, Install(), "--copy"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "agents", reviewerFile)); err != nil {
		t.Fatalf("install did not write under CLAUDE_CONFIG_DIR: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", reviewerFile)); !os.IsNotExist(err) {
		t.Fatalf("install wrote under $HOME/.claude/agents while CLAUDE_CONFIG_DIR is set: %v", err)
	}
	if got := reviewerStatusIn(userAgentsDir(home), ""); got.State != reviewerOK || !strings.HasPrefix(got.Path, cfg) {
		t.Fatalf("readiness read %s (%s), not the dir install wrote", got.Path, got.State)
	}
}

// chdirOnly changes directory for the test without chdir's fixture seeding.
func chdirOnly(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// TestInstallNeverTouchesRealAgentsDir: outside chdir, the agents directory
// resolves under the hermetic HOME — never the developer's real one, whatever
// CLAUDE_CONFIG_DIR their shell exports.
func TestInstallNeverTouchesRealAgentsDir(t *testing.T) {
	if got := os.Getenv("CLAUDE_CONFIG_DIR"); got != "" {
		t.Fatalf("TestMain left CLAUDE_CONFIG_DIR=%q set", got)
	}
	dir := userAgentsDir(os.Getenv("HOME"))
	if ambientHome != "" && strings.HasPrefix(dir, filepath.Join(ambientHome, ".claude")) {
		t.Fatalf("the agents dir %s is the developer's real one", dir)
	}
	if ambientClaudeConfigDir != "" && strings.HasPrefix(dir, ambientClaudeConfigDir) {
		t.Fatalf("the agents dir %s is under the ambient CLAUDE_CONFIG_DIR", dir)
	}
}

func TestChdirSeedsReviewer(t *testing.T) {
	chdir(t, t.TempDir())
	if got := reviewerStatusIn(userAgentsDir(os.Getenv("HOME")), ""); got.State != reviewerOK {
		t.Fatalf("after chdir the reviewer is %s at %s, want ok", got.State, got.Path)
	}
}
