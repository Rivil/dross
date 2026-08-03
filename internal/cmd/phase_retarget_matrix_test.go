package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/ship"
)

// TestRetargetRefusesForEveryProvider proves mergeGate's retarget refusal is
// uniform across all 5 ship providers (github, gitlab, forgejo, gitea,
// bitbucket — the widened retarget_scope), not special-cased for any one of
// them: the same retargeted PRStatus answer, varying only opts.Provider,
// refuses completion identically every time. Each provider's own wire-format
// -> PRStatus.BaseRef mapping is proven separately at the ship layer
// (internal/ship/retarget_matrix_test.go); a provider wired for merged-status
// but not base-ref would fail that test, not this one.
func TestRetargetRefusesForEveryProvider(t *testing.T) {
	const recordedBase = "main"
	const retargetedBase = "release/y"

	for _, provider := range []string{"github", "gitlab", "forgejo", "gitea", "bitbucket"} {
		t.Run(provider, func(t *testing.T) {
			dir, _ := completeFixture(t)
			if err := runCmd(t, Project(), "set", "remote.provider", provider); err != nil {
				t.Fatalf("project set remote.provider: %v", err)
			}
			gitCommit(t, dir, "test: point remote.provider at "+provider)

			prev := ship.PRStatusFunc
			ship.PRStatusFunc = func(opts ship.OpenOpts) (ship.PRStatus, error) {
				if opts.Provider != provider {
					t.Errorf("mergeGate called PRStatusFunc with provider %q, want %q", opts.Provider, provider)
				}
				return ship.PRStatus{Merged: true, BaseRef: retargetedBase}, nil
			}
			t.Cleanup(func() { ship.PRStatusFunc = prev })

			mainBefore := mustGit(t, dir, "rev-parse", "main")

			err := runCmd(t, Phase(), "complete")
			if err == nil {
				t.Fatalf("provider %q: expected a retarget refusal", provider)
			}
			if !strings.Contains(err.Error(), recordedBase) || !strings.Contains(err.Error(), retargetedBase) {
				t.Errorf("provider %q: error must name both branches, got: %v", provider, err)
			}
			assertRefusalTouchedNothing(t, dir, "phase/auth", "main", mainBefore)
		})
	}
}
