package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// setupTargetFixture extends the shared deferred fixture with a second, earlier
// milestone whose phases array parks a slug that has NO phase directory —
// modelled on this repo's own `watch-pr-ci-status` (v0.9's array, never
// scaffolded). Routing at such a slug is legitimate, and is the arm a
// current-milestone-only rule would wrongly reject.
func setupTargetFixture(t *testing.T) string {
	t.Helper()
	dir := setupDeferredFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "milestones", "v0.4.toml"),
		"phases = [\"watch-pr-ci-status\"]\n\n[milestone]\n  version = \"v0.4\"\n  title = \"Earlier\"\n")
	return dir
}

// TestDeferredRouteAcceptsMilestoneOnlySlug: a slug parked in ANY milestone's
// phases array is a valid destination even with no directory on disk. Deleting
// the milestone-array branch from deferredTargetSet fails here.
func TestDeferredRouteAcceptsMilestoneOnlySlug(t *testing.T) {
	dir := setupTargetFixture(t)
	if err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", "watch-pr-ci-status"); err != nil {
		t.Fatalf("route to a milestone-array-only slug should succeed: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"))
	if !strings.Contains(body, `target = "watch-pr-ci-status"`) {
		t.Errorf("target not stamped:\n%s", body)
	}
}

// TestDeferredRouteAcceptsScaffoldedOnlySlug is the complementary arm: a phase
// with a directory but absent from every milestone array (gamma in the fixture)
// is also a valid destination. A milestone-only check would reject it.
func TestDeferredRouteAcceptsScaffoldedOnlySlug(t *testing.T) {
	dir := setupTargetFixture(t)
	if err := runCmd(t, Deferred(), "route", "alpha", "1", "--target", "gamma"); err != nil {
		t.Fatalf("route to a scaffolded phase outside every milestone array should succeed: %v", err)
	}
	body := mustRead(t, filepath.Join(dir, ".dross", "phases", "alpha", "spec.toml"))
	if !strings.Contains(body, `target = "gamma"`) {
		t.Errorf("target not stamped:\n%s", body)
	}
}

// TestDeferredRouteAndValidateAgree is the point of the extraction: one rule,
// one implementation. For every slug in the table, `route` accepting it must
// imply validate stays green, and `route` rejecting it must imply validate goes
// red. A slug the two disagree on is a silently unreachable destination — and
// `watch-pr-ci-status` is exactly the slug the pre-amendment current-milestone
// rule would have split.
func TestDeferredRouteAndValidateAgree(t *testing.T) {
	for _, tc := range []struct {
		slug      string
		wantValid bool
	}{
		{"beta", true},               // phase dir + in the current milestone array
		{"gamma", true},              // phase dir only
		{"watch-pr-ci-status", true}, // earlier milestone's array only, no dir
		{"typo-slug", false},         // neither
		{projectStoreSlug, false},    // the store is a home, never a destination
	} {
		t.Run(tc.slug, func(t *testing.T) {
			dir := setupTargetFixture(t)
			routeErr := runCmd(t, Deferred(), "route", "gamma", "0", "--target", tc.slug)
			if (routeErr == nil) != tc.wantValid {
				t.Fatalf("route --target %q: err=%v, wantValid=%v", tc.slug, routeErr, tc.wantValid)
			}
			if !tc.wantValid {
				// Nothing was stamped, so validate can only be green here; the
				// meaningful half is the accepted case below. Hand-write the
				// same target to prove validate would have flagged it.
				mustWrite(t, filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml"),
					"[phase]\nid = \"gamma\"\ntitle = \"Gamma\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n\n[[deferred]]\ntext = \"hand-written\"\ntarget = \""+tc.slug+"\"\n")
				if err := runCmd(t, Validate()); err == nil {
					t.Errorf("route rejected %q but validate calls it valid — the two rules have drifted", tc.slug)
				}
				return
			}
			if err := runCmd(t, Validate()); err != nil {
				t.Errorf("route accepted %q but validate reports it dangling — the two rules have drifted: %v", tc.slug, err)
			}
		})
	}
}

// TestDeferredRouteRejectionNamesBothRemedies: a bare "unknown target" leaves
// the user guessing. The message must name both ways to make the slug real.
func TestDeferredRouteRejectionNamesBothRemedies(t *testing.T) {
	setupTargetFixture(t)
	err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", "typo-slug")
	if err == nil {
		t.Fatal("route to an unknown slug should be rejected")
	}
	msg := err.Error()
	for _, want := range []string{"typo-slug", "scaffold a phase", "milestone's phases array"} {
		if !strings.Contains(msg, want) {
			t.Errorf("rejection missing %q:\n%s", want, msg)
		}
	}
}

// TestDeferredRouteRejectionWritesNothing pins both halves of "validation
// precedes the save": a typo must not be auto-appended to the roadmap (that
// turns a typo into a permanent fake phase), and the source spec must be
// untouched (no orphan item pointing at a dead slug).
func TestDeferredRouteRejectionWritesNothing(t *testing.T) {
	dir := setupTargetFixture(t)
	milestonePath := filepath.Join(dir, ".dross", "milestones", "v0.5.toml")
	specPath := filepath.Join(dir, ".dross", "phases", "gamma", "spec.toml")
	beforeMilestone := mustRead(t, milestonePath)
	beforeSpec := mustRead(t, specPath)

	if err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", "typo-slug"); err == nil {
		t.Fatal("route to an unknown slug should be rejected")
	}

	if got := mustRead(t, milestonePath); got != beforeMilestone {
		t.Errorf("milestone toml rewritten by a rejected route — a typo must never be auto-appended:\n%s", got)
	}
	if got := mustRead(t, specPath); got != beforeSpec {
		t.Errorf("source spec rewritten by a rejected route; validation must precede the save:\n%s", got)
	}
}

// TestDeferredRouteRejectsProjectStore: the reserved store holds homeless items,
// it is not somewhere they can be routed to. Nothing would ever re-surface them.
func TestDeferredRouteRejectsProjectStore(t *testing.T) {
	dir := setupTargetFixture(t)
	// Even with a stray phases/_project directory present, the slug is refused —
	// the exclusion is explicit, not an accident of the directory not existing.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", projectStoreSlug, ".keep"), "")
	if err := runCmd(t, Deferred(), "route", "gamma", "0", "--target", projectStoreSlug); err == nil {
		t.Fatalf("route --target %s should be rejected", projectStoreSlug)
	}
}
