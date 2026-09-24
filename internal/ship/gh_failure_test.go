package ship

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gh's own output can carry an API response body, so a failed invocation
// prints it to stderr and returns only the subcommand and exit status. Each
// of the five gh entry points is driven against a stub gh that prints a canary
// and exits 1: the canary must be on stderr and nowhere in the error.
func TestGhFailureOutputGoesToStderr(t *testing.T) {
	for _, tc := range []struct {
		name string
		verb string
		call func() error
	}{
		{"pr create", "gh pr create", func() error {
			// openGitHubPR looks gh up on PATH before it spawns anything;
			// a stub there keeps the case from depending on the machine.
			stubGHOnPath(t)
			_, err := openGitHubPR(OpenOpts{Provider: "github", HeadBranch: "pr/x", BaseBranch: "main", Title: "t", Body: "b"})
			return err
		}},
		{"pr list --head", "gh pr list --head pr/x", func() error {
			_, err := gitHubOpenPRByHead("pr/x")
			return err
		}},
		{"pr list --base", "gh pr list --base main", func() error {
			_, err := gitHubOpenPRsTargeting("main")
			return err
		}},
		{"pr view", "gh pr view #7", func() error {
			_, err := gitHubPRStatus(OpenOpts{Provider: "github", PRNumber: 7})
			return err
		}},
		{"pr comment", "gh pr comment", func() error {
			return postGitHubComment(CommentOpts{Provider: "github", PRNumber: 7, Body: "hi"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stderr := stubFailingGH(t, "CANARY-GH")
			err := tc.call()
			if err == nil {
				t.Fatal("a failing gh returned no error")
			}
			if strings.Contains(err.Error(), "CANARY-GH") {
				t.Errorf("gh's output reached the error: %q", err)
			}
			if !strings.Contains(err.Error(), tc.verb) || !strings.Contains(err.Error(), "exit status 1") {
				t.Errorf("err = %q, want the subcommand %q and exit status 1", err, tc.verb)
			}
			if !strings.Contains(stderr.String(), "CANARY-GH") {
				t.Errorf("stderr = %q, want gh's own output there", stderr.String())
			}
		})
	}
}

// TestGhUnparseableOutputGoesToStderr: output that is not the JSON gh
// promised is printed, and the error is fixed prose.
func TestGhUnparseableOutputGoesToStderr(t *testing.T) {
	prev, prevErr := ghCommand, ghStderr
	defer func() { ghCommand, ghStderr = prev, prevErr }()
	var stderr bytes.Buffer
	ghStderr = &stderr
	ghCommand = func(...string) *exec.Cmd { return exec.Command("printf", "%s", "CANARY-JSON{") }

	_, err := gitHubOpenPRsTargeting("main")
	if err == nil || strings.Contains(err.Error(), "CANARY-JSON") || !strings.Contains(err.Error(), "not the JSON") {
		t.Errorf("err = %v, want fixed prose without gh's output", err)
	}
	if !strings.Contains(stderr.String(), "CANARY-JSON") {
		t.Errorf("stderr = %q, want the unparseable output there", stderr.String())
	}
}

// stubGHOnPath puts an executable named gh first on PATH for the test.
func stubGHOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubFailingGH points ghCommand at a gh that prints canary and exits 1, and
// captures ghStderr; both are restored when the test ends.
func stubFailingGH(t *testing.T, canary string) *bytes.Buffer {
	t.Helper()
	prev, prevErr := ghCommand, ghStderr
	t.Cleanup(func() { ghCommand, ghStderr = prev, prevErr })
	var stderr bytes.Buffer
	ghStderr = &stderr
	ghCommand = func(...string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo "+canary+"; exit 1")
	}
	return &stderr
}
