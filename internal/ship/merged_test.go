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
	// The number is fenced behind `--`, with --json ahead of it. See
	// gharg_test.go for why the ordering is load-bearing rather than cosmetic.
	want := []string{"pr", "view", "--json", "state,mergedAt,baseRefName", "--", "7"}
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
// cmd-package tests have something to override. It keys off an outright
// unknown provider rather than any real one — by the end of this phase every
// provider in configenum.ShipProviders answers PRStatus authoritatively, so
// any real-provider exemplar would eventually break this assertion silently.
func TestPRStatusFuncDefaultsToPRStatus(t *testing.T) {
	if PRStatusFunc == nil {
		t.Fatal("PRStatusFunc must be a non-nil overridable var")
	}
	if _, err := PRStatusFunc(OpenOpts{Provider: "perforce", PRNumber: 1}); err == nil {
		t.Error("PRStatusFunc should delegate to GetPRStatus and return its unknown-provider error")
	}
}
