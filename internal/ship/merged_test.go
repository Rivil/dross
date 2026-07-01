package ship

import (
	"errors"
	"os/exec"
	"testing"
)

// TestPRMergedGitHub drives the GitHub path with a stubbed ghCommand whose
// stdout is a canned `gh pr view --json` document: merged is true only when
// state == "MERGED". It also captures the args so a regression that drops the
// PR number or the --json fields is caught.
func TestPRMergedGitHub(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"open", `{"state":"OPEN","mergedAt":null}`, false},
		{"closed unmerged", `{"state":"CLOSED","mergedAt":null}`, false},
		{"merged", `{"state":"MERGED","mergedAt":"2026-07-01T00:00:00Z"}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotArgs []string
			prev := ghCommand
			ghCommand = func(args ...string) *exec.Cmd {
				gotArgs = append([]string{}, args...)
				return exec.Command("printf", "%s", c.json)
			}
			defer func() { ghCommand = prev }()

			merged, err := PRMerged(OpenOpts{Provider: "github", PRNumber: 7})
			if err != nil {
				t.Fatalf("PRMerged: %v", err)
			}
			if merged != c.want {
				t.Errorf("state in %s: merged=%v want %v", c.json, merged, c.want)
			}
			want := []string{"pr", "view", "7", "--json", "state,mergedAt"}
			if len(gotArgs) != len(want) {
				t.Fatalf("gh args: got %v want %v", gotArgs, want)
			}
			for i := range want {
				if gotArgs[i] != want[i] {
					t.Errorf("gh arg %d: got %q want %q", i, gotArgs[i], want[i])
				}
			}
		})
	}
}

// TestPRMergedUnsupportedProvider proves the not-yet-implemented providers
// return the sentinel (not a silent merged=true), so callers fall back to
// git ancestry rather than false-completing.
func TestPRMergedUnsupportedProvider(t *testing.T) {
	for _, prov := range []string{"forgejo", "gitea", "gitlab", "Forgejo"} {
		merged, err := PRMerged(OpenOpts{Provider: prov, PRNumber: 1})
		if !errors.Is(err, ErrMergeStatusUnsupported) {
			t.Errorf("provider %q: err=%v want ErrMergeStatusUnsupported", prov, err)
		}
		if merged {
			t.Errorf("provider %q: merged must be false when unsupported", prov)
		}
	}
}

// TestPRMergedUnknownProvider returns a plain error (not the unsupported
// sentinel) so an outright misconfiguration is still distinguishable.
func TestPRMergedUnknownProvider(t *testing.T) {
	_, err := PRMerged(OpenOpts{Provider: "perforce", PRNumber: 1})
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if errors.Is(err, ErrMergeStatusUnsupported) {
		t.Error("an unknown provider should not report as merely unsupported")
	}
}

// TestPRMergedFuncDefaultsToPRMerged proves the exported seam is a non-nil var
// defaulting to the real implementation, so production wiring works and
// cmd-package tests have something to override.
func TestPRMergedFuncDefaultsToPRMerged(t *testing.T) {
	if PRMergedFunc == nil {
		t.Fatal("PRMergedFunc must be a non-nil overridable var")
	}
	if _, err := PRMergedFunc(OpenOpts{Provider: "gitlab", PRNumber: 1}); !errors.Is(err, ErrMergeStatusUnsupported) {
		t.Errorf("PRMergedFunc should delegate to PRMerged, got: %v", err)
	}
}
