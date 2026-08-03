package ship

import (
	"errors"
	"os/exec"
	"testing"
)

// TestPRStatusGitHubReturnsBaseRef drives the GitHub path with a stubbed
// ghCommand whose stdout is a canned `gh pr view --json` document, and
// captures the args so a regression that drops the --json fields is caught.
func TestPRStatusGitHubReturnsBaseRef(t *testing.T) {
	var gotArgs []string
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		gotArgs = append([]string{}, args...)
		return exec.Command("printf", "%s", `{"state":"MERGED","mergedAt":"2026-07-01T00:00:00Z","baseRefName":"main"}`)
	}
	defer func() { ghCommand = prev }()

	status, err := GetPRStatus(OpenOpts{Provider: "github", PRNumber: 7})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if !status.Merged || status.BaseRef != "main" {
		t.Errorf("got %+v, want {Merged:true BaseRef:main}", status)
	}
	want := []string{"pr", "view", "7", "--json", "state,mergedAt,baseRefName"}
	if len(gotArgs) != len(want) {
		t.Fatalf("gh args: got %v want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("gh arg %d: got %q want %q", i, gotArgs[i], want[i])
		}
	}
}

// TestPRStatusGitHubClosedIsNotMerged proves a closed-not-merged PR reports
// Merged false.
func TestPRStatusGitHubClosedIsNotMerged(t *testing.T) {
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		return exec.Command("printf", "%s", `{"state":"CLOSED","mergedAt":null,"baseRefName":"main"}`)
	}
	defer func() { ghCommand = prev }()

	status, err := GetPRStatus(OpenOpts{Provider: "github", PRNumber: 7})
	if err != nil {
		t.Fatalf("GetPRStatus: %v", err)
	}
	if status.Merged {
		t.Errorf("closed PR must not report Merged true")
	}
}

// TestPRStatusUnsupportedProvider proves the not-yet-implemented providers
// return the sentinel (not a silent merged=true), so callers fall back to
// git ancestry rather than false-completing.
func TestPRStatusUnsupportedProvider(t *testing.T) {
	for _, prov := range []string{"forgejo", "gitea", "gitlab", "Forgejo"} {
		status, err := GetPRStatus(OpenOpts{Provider: prov, PRNumber: 1})
		if !errors.Is(err, ErrMergeStatusUnsupported) {
			t.Errorf("provider %q: err=%v want ErrMergeStatusUnsupported", prov, err)
		}
		if status.Merged {
			t.Errorf("provider %q: merged must be false when unsupported", prov)
		}
	}
}

// TestPRStatusUnknownProvider returns a plain error (not the unsupported
// sentinel) so an outright misconfiguration is still distinguishable.
func TestPRStatusUnknownProvider(t *testing.T) {
	_, err := GetPRStatus(OpenOpts{Provider: "perforce", PRNumber: 1})
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if errors.Is(err, ErrMergeStatusUnsupported) {
		t.Error("an unknown provider should not report as merely unsupported")
	}
}

// TestPRStatusFuncDefaultsToPRStatus proves the exported seam is a non-nil
// var defaulting to the real implementation, so production wiring works and
// cmd-package tests have something to override. It keys off an unsupported,
// not-yet-wired provider rather than "gitlab" — t-4 makes gitlab a supported
// PRStatus provider, which would otherwise break this assertion silently two
// tasks later.
func TestPRStatusFuncDefaultsToPRStatus(t *testing.T) {
	if PRStatusFunc == nil {
		t.Fatal("PRStatusFunc must be a non-nil overridable var")
	}
	if _, err := PRStatusFunc(OpenOpts{Provider: "forgejo", PRNumber: 1}); !errors.Is(err, ErrMergeStatusUnsupported) {
		t.Errorf("PRStatusFunc should delegate to GetPRStatus, got: %v", err)
	}
}
