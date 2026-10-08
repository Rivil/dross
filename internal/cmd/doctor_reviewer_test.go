package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDoctorReviewer(t *testing.T) (string, error) {
	t.Helper()
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	return doctorSection(t, out, "Reviewer:"), err
}

// TestDoctorReviewer (c-7): doctor exits non-zero whenever the solo reviewer
// could not run as shipped, and each line names the path and the fix.
func TestDoctorReviewer(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		doctorRepo(t)
		sec, err := runDoctorReviewer(t)
		if err != nil {
			t.Fatalf("doctor on a fresh install: %v\n%s", err, sec)
		}
		if !strings.Contains(sec, "✓") || !strings.Contains(sec, "dross-task-reviewer is installed and current") {
			t.Fatalf("Reviewer section = %q, want a ✓ for the installed reviewer", sec)
		}
	})

	cases := []struct {
		name   string
		break_ func(t *testing.T, repo, installed string) string // returns the path the line must name
		fix    string
	}{
		{"deleted", func(t *testing.T, repo, installed string) string {
			if err := os.Remove(installed); err != nil {
				t.Fatal(err)
			}
			return installed
		}, "dross install"},
		{"one byte changed", func(t *testing.T, repo, installed string) string {
			b, err := os.ReadFile(installed)
			if err != nil {
				t.Fatal(err)
			}
			b[len(b)-2] ^= 1
			if err := os.WriteFile(installed, b, 0o644); err != nil {
				t.Fatal(err)
			}
			return installed
		}, "dross install"},
		{"dangling symlink", func(t *testing.T, repo, installed string) string {
			_ = os.Remove(installed)
			if err := os.Symlink(filepath.Join(t.TempDir(), "gone.md"), installed); err != nil {
				t.Fatal(err)
			}
			return installed
		}, "dross install"},
		{"linked source edited past the binary", func(t *testing.T, repo, installed string) string {
			src := filepath.Join(t.TempDir(), reviewerFile)
			b, err := os.ReadFile(installed)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(src, append(b, []byte("\nan edit the binary has not seen\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(installed)
			if err := os.Symlink(src, installed); err != nil {
				t.Fatal(err)
			}
			return installed
		}, "make install"},
		{"repo copy shadows it", func(t *testing.T, repo, installed string) string {
			p := filepath.Join(repo, ".claude", "agents", reviewerFile)
			mustWrite(t, p, "---\nname: dross-task-reviewer\n---\n")
			return p
		}, "remove"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := doctorRepo(t)
			installed := filepath.Join(userAgentsDir(os.Getenv("HOME")), reviewerFile)
			named := c.break_(t, repo, installed)
			sec, err := runDoctorReviewer(t)
			if err == nil {
				t.Fatalf("doctor exited 0 with the reviewer %s:\n%s", c.name, sec)
			}
			if !strings.Contains(sec, "✗") && !strings.Contains(sec, "solo runs refuse") {
				t.Errorf("the Reviewer section reports no issue:\n%s", sec)
			}
			if !strings.Contains(sec, named) || !strings.Contains(sec, c.fix) {
				t.Errorf("the line does not name %s and the fix %q:\n%s", named, c.fix, sec)
			}
		})
	}
}

// TestDoctorReviewerDir: doctor reads the agents dir Claude Code reads.
func TestDoctorReviewerDir(t *testing.T) {
	doctorRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := syncAgents(filepath.Join(home, ".claude", "agents"), false, "", func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	sec, err := runDoctorReviewer(t)
	if err == nil || !strings.Contains(sec, filepath.Join(cfg, "agents", reviewerFile)) {
		t.Fatalf("doctor read $HOME/.claude/agents while CLAUDE_CONFIG_DIR points elsewhere:\n%s", sec)
	}
}
