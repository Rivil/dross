package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var slNow = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func nilEnv(string) string  { return "" }
func nilGit(string) string  { return "" }
func mainGit(string) string { return "main" }

// TestStatuslineSilentFail: malformed / empty stdin produces no output, no panic, and
// runStatusline returns normally (the command exits 0).
func TestStatuslineSilentFail(t *testing.T) {
	for _, in := range []string{`{not json`, ``, `   `} {
		var buf bytes.Buffer
		runStatusline(strings.NewReader(in), &buf, nilEnv, slNow, nilGit, time.Second)
		if buf.Len() != 0 {
			t.Errorf("stdin %q: want empty output on parse failure, got %q", in, buf.String())
		}
	}
}

// TestStatuslineRenders: a minimal valid stdin renders at least line 1 through the
// command (Gather -> Render wiring).
func TestStatuslineRenders(t *testing.T) {
	var buf bytes.Buffer
	runStatusline(strings.NewReader(`{"model":{"display_name":"Claude"},"workspace":{"current_dir":"/work/myproject"}}`),
		&buf, nilEnv, slNow, mainGit, time.Second)
	out := buf.String()
	if !strings.Contains(out, "Claude") || !strings.Contains(out, "myproject") || !strings.Contains(out, "main") {
		t.Errorf("rendered line 1 missing expected parts: %q", out)
	}
}

// blockingReader never returns from Read, simulating a pipe that never closes.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }

// TestStatuslineNoHang: a stdin that never closes must not block past the bounded
// deadline — runStatusline returns within the timeout.
func TestStatuslineNoHang(t *testing.T) {
	done := make(chan struct{})
	go func() {
		var buf bytes.Buffer
		runStatusline(blockingReader{}, &buf, nilEnv, slNow, nilGit, 50*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runStatusline hung past the bounded stdin deadline")
	}
}

// TestStatuslineEndToEndGolden drives the command end to end against a temp config
// dir with an in-progress todo and a stubbed branch, and asserts the bytes match the
// node-minted internal/statusline golden — proving Gather->Render through the command
// is byte-faithful to the reference.
func TestStatuslineEndToEndGolden(t *testing.T) {
	root := repoRootFromTest(t)
	cfg := t.TempDir()
	todo := filepath.Join(cfg, "todos", "sess123-agent-1.json")
	if err := os.MkdirAll(filepath.Dir(todo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(todo, []byte(`[{"status":"in_progress","activeForm":"Doing the thing"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return cfg
		}
		return ""
	}
	var buf bytes.Buffer
	runStatusline(
		strings.NewReader(`{"model":{"display_name":"Claude"},"workspace":{"current_dir":"/work/myproject"},"session_id":"sess123"}`),
		&buf, env, slNow, nilGit, time.Second)

	want, err := os.ReadFile(filepath.Join(root, "internal", "statusline", "testdata", "todo_only.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(want) {
		t.Errorf("end-to-end output not byte-faithful to golden\n got: %q\nwant: %q", buf.String(), want)
	}
}

// TestGitBranch exercises the REAL git runner over a `git init` temp repo: a normal
// branch resolves via symbolic-ref --short, a detached HEAD falls back to the short
// SHA, and a non-repo dir yields "".
func TestGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.email=t@e", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)
		c := exec.Command("git", full...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")

	// Normal branch (symbolic-ref --short HEAD works before the first commit).
	if got := statuslineGitBranch(repo); got != "main" {
		t.Errorf("normal branch = %q, want main", got)
	}

	// Detached HEAD => short SHA via rev-parse fallback.
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "c1")
	git("checkout", "-q", "--detach")
	wantSHA, err := gitBranchTrim(repo, "rev-parse", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := statuslineGitBranch(repo); got != wantSHA {
		t.Errorf("detached HEAD = %q, want short SHA %q", got, wantSHA)
	}

	// Non-repo dir => "".
	if got := statuslineGitBranch(t.TempDir()); got != "" {
		t.Errorf("non-repo dir = %q, want empty", got)
	}
}

// TestStatuslineRegistered guards reachability: the command must be wired into the
// real root tree in cmd/dross/main.go.
func TestStatuslineRegistered(t *testing.T) {
	root := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(root, "cmd", "dross", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(b), "cmd.Statusline()") {
		t.Error("cmd.Statusline() is not registered in cmd/dross/main.go — `dross statusline` would be unreachable")
	}
}
