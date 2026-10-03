package gatestate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// repo is a git repo with a committed .dross/project.toml.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "T")
	git(t, dir, "config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", "project.toml"), []byte("[project]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".dross")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

var at = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

// TestGateDirUntracked: the directory ignores itself. No root .gitignore line
// is needed, and nothing in it ever shows or stages.
func TestGateDirUntracked(t *testing.T) {
	dir := repo(t)
	if err := SaveGreen(dir, Green{Tree: "abc", At: at, Runner: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-1", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	if st := git(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.Contains(st, ".dross/gate") {
		t.Errorf("git status shows the gate dir:\n%s", st)
	}
	git(t, dir, "add", ".dross/")
	if staged := git(t, dir, "diff", "--cached", "--name-only"); strings.Contains(staged, ".dross/gate") {
		t.Errorf("git add .dross/ staged gate records:\n%s", staged)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("a root .gitignore appeared (err=%v); the gate dir must ignore itself", err)
	}
}

// TestConcurrentWritesAreAtomic: writers racing readers. A reader only ever
// sees no record or a whole one — never a torn or truncated file.
func TestConcurrentWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 1000)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				tree := strings.Repeat(string(rune('a'+w)), 200+i)
				if err := SaveGreen(dir, Green{Tree: tree, At: at, Runner: "local"}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				g, err := LoadGreen(dir)
				if err != nil {
					errs <- err
					continue
				}
				if g != nil && len(g.Tree) < 200 {
					errs <- os.ErrInvalid
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a reader saw a torn record: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".dross", "gate", ".*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %q", left)
	}
}

func TestRecords(t *testing.T) {
	dir := t.TempDir()
	if g, err := LoadGreen(dir); g != nil || err != nil {
		t.Errorf("missing green = %+v, %v; want nil, nil", g, err)
	}
	if e, err := LoadExecute(dir); e != nil || err != nil {
		t.Errorf("missing execute = %+v, %v; want nil, nil", e, err)
	}
	if a, err := LoadApproval(dir); a != nil || err != nil {
		t.Errorf("missing approval = %+v, %v; want nil, nil", a, err)
	}

	want := Green{Tree: "4b825dc", At: at, Runner: "helicon"}
	if err := SaveGreen(dir, want); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err != nil || *g != want {
		t.Errorf("green round trip = %+v, %v", g, err)
	}
	if err := SaveExecute(dir, Execute{Phase: "p", Mode: "solo", At: at}); err != nil {
		t.Fatal(err)
	}
	if e, err := LoadExecute(dir); err != nil || e.Mode != "solo" || e.Phase != "p" {
		t.Errorf("execute round trip = %+v, %v", e, err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-3", Head: "deadbeef", At: at}); err != nil {
		t.Fatal(err)
	}
	if a, err := LoadApproval(dir); err != nil || a.Task != "t-3" || a.Head != "deadbeef" {
		t.Errorf("approval round trip = %+v, %v", a, err)
	}

	if err := SaveGreen(dir, Green{Tree: " ", At: at}); err == nil {
		t.Error("a green with no tree was saved")
	}
	if err := os.WriteFile(Path(dir, GreenFile), []byte(`{"tree":"","at":"2026-10-03T09:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err == nil || g != nil {
		t.Errorf("a green with an empty tree on disk = %+v, %v; want an error", g, err)
	}
	if err := os.WriteFile(Path(dir, GreenFile), []byte(`{"tree":"abc","at":"2026-`), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, err := LoadGreen(dir); err == nil || g != nil || !strings.Contains(err.Error(), ".dross/gate/green.json") {
		t.Errorf("a truncated green = %+v, %v; want an error naming .dross/gate/green.json", g, err)
	}

	if err := ClearGreen(dir); err != nil {
		t.Fatal(err)
	}
	if err := ClearGreen(dir); err != nil {
		t.Errorf("clearing an absent green: %v", err)
	}
	if g, err := LoadGreen(dir); g != nil || err != nil {
		t.Errorf("after ClearGreen = %+v, %v", g, err)
	}
}

// TestTrackedRecordRefused: a record force-added to git is refused unread.
func TestTrackedRecordRefused(t *testing.T) {
	dir := repo(t)
	if err := SaveGreen(dir, Green{Tree: "abc", At: at}); err != nil {
		t.Fatal(err)
	}
	if err := SaveApproval(dir, Approval{Phase: "p", Task: "t-1", Head: "h", At: at}); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-f", ".dross/gate/green.json", ".dross/gate/approval.json")
	if g, err := LoadGreen(dir); err == nil || g != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked green = %+v, %v; want a refusal", g, err)
	}
	if a, err := LoadApproval(dir); err == nil || a != nil || !strings.Contains(err.Error(), "tracked") {
		t.Errorf("tracked approval = %+v, %v; want a refusal", a, err)
	}
	if e, err := LoadExecute(dir); err != nil || e != nil {
		t.Errorf("untracked, absent execute = %+v, %v", e, err)
	}
}
