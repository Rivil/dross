package cmd

import (
	"errors"
	"fmt"
	"os"
	"slices"
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

// milestoneCloseoutGate refuses to open the integration PR while the
// close-out check has anything blocking, and otherwise returns the joined
// report so the caller can carry its acknowledgements into the PR body.
//
// The refusal is the whole report, every kind at once (c-4): clearing a
// milestone one error per run is the friction that teaches people to reach for
// an override. --force-unverified drops only the unverified kind, items and
// collector errors alike (locked force_unverified); it is the one kind an ack
// cannot clear, and the override records nothing anywhere. Every other kind
// still needs clearing or an ack.
//
// No milestone record, or one with no phases, has nothing to close out and
// passes (c-8).
func milestoneCloseoutGate(root, version string, forceUnverified bool) (closeoutJoined, error) {
	j, err := loadCloseout(root, version)
	if err != nil {
		return closeoutJoined{}, err
	}
	if forceUnverified {
		j = withoutCloseoutKind(j, closeoutUnverified)
	}
	if j.Blocking() == 0 {
		return j, nil
	}
	msg := fmt.Sprintf("milestone %s is not ready to close — refusing to open the integration PR.\n\n%s\n"+
		"Clear each item with the command under it, or acknowledge it with `dross milestone ack <item> --reason \"...\"`.",
		version, strings.TrimRight(renderCloseout(j), "\n"))
	if slices.ContainsFunc(j.Open, func(it closeoutItem) bool { return it.Kind == closeoutUnverified }) {
		msg += " An unverified phase is never acknowledged: verify it, or pass --force-unverified to override."
	}
	return j, errors.New(msg)
}

// withoutCloseoutKind drops one kind's open items and collector errors. Acks
// are untouched: no ack ever applies to the unverified kind, so its removal
// cannot move one between Applied and Stale.
func withoutCloseoutKind(j closeoutJoined, kind string) closeoutJoined {
	open := make([]closeoutItem, 0, len(j.Open))
	for _, it := range j.Open {
		if it.Kind != kind {
			open = append(open, it)
		}
	}
	errs := make([]closeoutError, 0, len(j.Errors))
	for _, e := range j.Errors {
		if e.Kind != kind {
			errs = append(errs, e)
		}
	}
	j.Open, j.Errors = open, errs
	return j
}

// milestonePRBody is the integration PR's description. With no applied
// acknowledgement it is exactly the body every milestone PR has carried (c-8).
// Each applied ack adds one line — item, reason, and the item's current detail
// — so a reviewer sees what is riding in unresolved and why (c-5). Stale acks
// never appear: their item is resolved, and listing it would claim otherwise
// (c-11).
func milestonePRBody(version, target string, applied []closeoutApplied) string {
	body := fmt.Sprintf("Integration PR for milestone %s.\n\n"+
		"Merge as a **merge commit** (not squash) to preserve per-phase history on %s.",
		version, target)
	if len(applied) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n## Close-out acknowledgements\n\n" +
		"Unresolved when this PR opened, and acknowledged with `dross milestone ack`:\n\n")
	for _, a := range applied {
		// One bullet per ack: a reason typed with newlines must not spill
		// into lines that read as separate items.
		fmt.Fprintf(&b, "- `%s` — %s (%s)\n", a.Ack.Item, oneLine(a.Ack.Reason), oneLine(a.Item.Detail))
	}
	return strings.TrimRight(b.String(), "\n")
}

// oneLine collapses every run of whitespace, newlines included, to one space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
