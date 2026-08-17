package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/verify"
)

// unverifiedPhase is one phase that fails the milestone gate, with the reason
// stated in the phase's own terms — a missing verify.toml and a pending
// verdict need different next commands, and a gate that says only "unverified"
// makes the reader open the file to find out which.
type unverifiedPhase struct {
	id     string
	reason string
}

// milestoneUnverifiedPhases names every phase in the milestone whose verify
// verdict is not "pass".
//
// This is the milestone-level counterpart to ship's per-phase gate, and it
// exists because the per-phase gate is not sufficient on its own: ship refuses
// an unverified phase, but nothing refused an unverified MILESTONE, so a phase
// that reached the milestone branch by any route ship did not drive — a hand
// merge, a phase whose verify run was never written, a phase that landed after
// the milestone's own verification — rode the integration PR into main
// unmeasured. v1.4 closed in exactly that state: its last phase had no
// verify.toml at all, and the milestone's mutation campaign had been recorded
// before that phase landed, so no artefact in the tree disagreed with itself.
//
// Every offending phase is collected rather than the first, because clearing
// them one error at a time is the friction that teaches people to reach for
// --force-unverified.
func milestoneUnverifiedPhases(root, version string) ([]unverifiedPhase, error) {
	path := milestone.FilePath(root, version)
	// No milestone record is not a gate failure. `milestone complete` has
	// always opened a PR for an unrecorded version, and turning that into a
	// refusal here would be a second behaviour change riding in on this one —
	// the gate checks the phases a milestone claims, and an absent record
	// claims nothing. A record that exists but will not parse still errors.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	m, err := milestone.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load milestone %s: %w", version, err)
	}
	var bad []unverifiedPhase
	for _, id := range m.Phases {
		_, vToml := verify.FilePaths(root, id)
		// LoadVerify reports a missing file as (nil, nil), not an error, so
		// the nil check is the missing-file case and err is a malformed one.
		// Both are "not verified" here, but they are not the same advice.
		vrf, err := verify.LoadVerify(vToml)
		if err != nil {
			bad = append(bad, unverifiedPhase{id: id,
				reason: fmt.Sprintf("verify.toml will not parse (%v)", err)})
			continue
		}
		if vrf == nil {
			bad = append(bad, unverifiedPhase{id: id, reason: "no verify.toml — run /dross-verify"})
			continue
		}
		switch v := vrf.Verify.Verdict; v {
		case "pass":
		case "":
			bad = append(bad, unverifiedPhase{id: id, reason: "verdict unset — run /dross-verify"})
		case "pending":
			bad = append(bad, unverifiedPhase{id: id,
				reason: fmt.Sprintf("verdict pending — resolve it, then `dross verify finalize %s`", id)})
		default:
			bad = append(bad, unverifiedPhase{id: id,
				reason: fmt.Sprintf("verdict %q — fix the failing criteria and re-run /dross-verify", v)})
		}
	}
	return bad, nil
}

// milestoneVerifyGate refuses to open the integration PR while any phase in
// the milestone is unverified. It returns nil when the milestone has no
// phases: an empty milestone has nothing to measure, and refusing it would
// block the very first close on a technicality.
func milestoneVerifyGate(root, version, target string) error {
	bad, err := milestoneUnverifiedPhases(root, version)
	if err != nil {
		return err
	}
	if len(bad) == 0 {
		return nil
	}
	lines := make([]string, 0, len(bad))
	for _, b := range bad {
		lines = append(lines, fmt.Sprintf("  %s — %s", b.id, b.reason))
	}
	return fmt.Errorf("milestone %s has %d unverified phase(s):\n%s\n\n"+
		"An integration PR carries these into %s unmeasured. Clear each one, or pass --force-unverified to override.",
		version, len(bad), strings.Join(lines, "\n"), target)
}
