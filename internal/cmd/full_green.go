package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/treefp"
)

// fullGreenRerun is the way out of a refusal only a new full run can clear.
// The commit comes first: a run over uncommitted work records the work tree,
// and requireFullGreen would refuse that one too.
const fullGreenRerun = "Commit your work, then run a bare `dross test` (no selector; in a laned repo no --files), and try again once it is green."

// hasVouchingLane reports whether any lane may admit a commit on a scoped green.
func hasVouchingLane(p *project.Project) bool {
	return slices.ContainsFunc(p.Runtime.TestLane, func(l project.TestLane) bool { return l.Vouch })
}

// requireFullGreen refuses a repo with a vouching lane whose HEAD tree no full
// green ran on. Commits there can land on scoped greens, each of which measured
// only its own paths, so this is the one point that holds a phase to a whole
// suite passing on the tree it ends with. The scoped record is never read: it
// vouches for a commit, not a phase.
//
// A repo with no vouching lane returns at once and keeps today's behaviour.
// Outside a git work tree there is no HEAD and a green can never be recorded,
// so there is nothing to refuse (startGreen and captureMeasuredTree draw the
// same line).
//
// The green is compared with treefp.Base, not the work tree. Both leave .dross/
// out, so bookkeeping committed after the run leaves the green standing.
func requireFullGreen(p *project.Project, repoDir string) error {
	if !hasVouchingLane(p) || !underGitDir(repoDir) {
		return nil
	}
	green, err := gatestate.LoadGreen(repoDir)
	if err != nil {
		return err
	}
	if green == nil {
		return errors.New("refusing: no full green `dross test` is recorded for this repo, and a scoped green never stands in for one\n" + fullGreenRerun)
	}
	head, err := treefp.Base(repoDir)
	if err != nil {
		return fmt.Errorf("refusing: could not fingerprint HEAD's tree to check it for a full green: %w", err)
	}
	if green.Tree == head {
		return nil
	}
	// The green may still be the work tree, with HEAD yet to catch up: the
	// suite already passed on that tree, so a re-run cannot converge on it.
	work, err := treefp.WorkingTree(repoDir)
	if err != nil {
		return fmt.Errorf("refusing: could not fingerprint the work tree to compare it with the last full green: %w", err)
	}
	if work == green.Tree {
		paths, err := treefp.Diff(repoDir, head, green.Tree)
		if err != nil {
			return fmt.Errorf("refusing: could not list what differs from the last full green: %w", err)
		}
		return fmt.Errorf("refusing: the last full green ran on the work tree, not on HEAD's tree — these paths differ: %s\n"+
			"Commit or clean them, then try again: the suite already passed on that tree, so running it again cannot change this",
			strings.Join(paths, " "))
	}
	return errors.New("refusing: HEAD's tree is not the one the last full green ran on, so no full run has measured this code\n" + fullGreenRerun)
}
