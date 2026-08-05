package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// maxReconstructedHistory caps reconstructed history the same way
// state.Touch caps live history — a "plausible version" (locked decision
// repair_invocation_mode) doesn't need to replay every phase ever shipped.
const maxReconstructedHistory = 50

// phaseCommitPattern matches the phase-completion commit marker ship.go
// writes as the PR title (`phase %s: %s`), which becomes the squash-merge
// commit subject on the main branch (locked decision
// state_json_reconstruction_source).
var phaseCommitPattern = regexp.MustCompile(`^phase (\S+): (.+)$`)

// trailingPRRef strips the " (#123)" GitHub appends to a squash-merged PR's
// title when it becomes the commit subject, so the recovered title matches
// what ship.go originally wrote.
var trailingPRRef = regexp.MustCompile(`\s+\(#\d+\)$`)

// reconstructState rebuilds a plausible state.State from tracked git
// history when the live one is missing or clearly stale: History from
// phase-completion commit markers on mainBranch, CurrentPhase (and, via
// that phase's own spec.toml, CurrentMilestone) from the checked-out
// phase/<id> branch.
//
// Never returns an error for "nothing to infer" cases (detached HEAD, a
// branch not named phase/<id>, no phase commits yet) — those all yield a
// State with correspondingly empty fields, since a partial-but-honest
// reconstruction is the point (locked decision repair_invocation_mode: this
// is shown to the user before anything is written, never assumed correct).
func reconstructState(repoDir, root, mainBranch string) (*state.State, error) {
	s := state.New()

	history, err := historyFromPhaseCommits(repoDir, mainBranch)
	if err != nil {
		return nil, err
	}
	if len(history) > maxReconstructedHistory {
		history = history[len(history)-maxReconstructedHistory:]
	}
	s.History = history
	if len(history) > 0 {
		last := history[len(history)-1]
		s.LastActivity = last.At
		s.LastAction = last.Action
	}

	branch, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return s, nil // detached HEAD or similar — nothing to infer, not fatal
	}
	id, ok := strings.CutPrefix(branch, "phase/")
	if !ok {
		return s, nil
	}
	s.CurrentPhase = id
	if sp, err := phase.LoadSpec(filepath.Join(phase.Dir(root, id), "spec.toml")); err == nil {
		s.CurrentMilestone = sp.Phase.Milestone
	}
	return s, nil
}

// historyFromPhaseCommits walks mainBranch's commit log oldest-first and
// turns every commit whose subject matches phaseCommitPattern into a
// state.Activity. Non-matching commits (ordinary feature/fix/chore work) are
// skipped rather than recorded as garbage history entries.
func historyFromPhaseCommits(repoDir, mainBranch string) ([]state.Activity, error) {
	// The live arbitrary-file-write vector: git's diff options accept
	// --output=<file> on `log`, so a committed [repo].git_main_branch of that
	// shape made `dross repair` write wherever it was pointed. Guarded, then
	// fenced behind the separator — either alone would have closed it, and
	// neither alone is enough for the next call site someone adds here.
	if err := validateGitRef("repo.git_main_branch", mainBranch); err != nil {
		return nil, err
	}
	out, err := gitTrim(repoDir, gitRefArgs("log", []string{"--reverse", "--pretty=format:%at\x1f%s"}, mainBranch)...)
	if err != nil {
		return nil, fmt.Errorf("git log %s: %w", mainBranch, err)
	}
	if out == "" {
		return nil, nil
	}
	var history []state.Activity
	for _, line := range strings.Split(out, "\n") {
		ts, subject, found := strings.Cut(line, "\x1f")
		if !found {
			continue
		}
		m := phaseCommitPattern.FindStringSubmatch(subject)
		if m == nil {
			continue
		}
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			continue
		}
		id, title := m[1], trailingPRRef.ReplaceAllString(m[2], "")
		history = append(history, state.Activity{
			At:     time.Unix(sec, 0).UTC(),
			Action: fmt.Sprintf("phase %s shipped: %s", id, title),
		})
	}
	return history, nil
}
