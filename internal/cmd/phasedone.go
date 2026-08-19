package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/state"
)

// This file is the single doneness reader. `dross status`, `dross milestone
// progress` and `dross phase list` all answer "is this phase done?" through
// phaseDone, so the three surfaces cannot drift into three different answers
// over the same phase directory — the divergence v1.4 shipped with, where the
// status bar counted verify verdicts and milestone progress counted completion
// records.

// phaseDone is the entry point: it resolves scaffolded-ness itself, so a caller
// holding only a slug never has to know that an unscaffolded slug is a separate
// arm. Callers already holding that answer (buildMilestoneProgress computes it
// for its own reporting) go straight to phaseIsDone.
func phaseDone(root, slug string, s *state.State) bool {
	return phaseIsDone(root, slug, phaseDirExists(root, slug), s)
}

// phaseDoneState loads the state the history fallback reads, leniently: a
// missing state.json degrades the fallback to "nothing recorded" rather than
// failing the caller. state.json is machine-local and gitignored, so it is
// simply absent in a fresh clone (and on CI), and a doneness question must
// still be answerable there.
func phaseDoneState(root string) *state.State {
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		// Absent (fresh clone, CI) or unreadable alike: the authoritative
		// marker lives in changes.json either way, so an empty state degrades
		// the fallback to "nothing recorded" instead of failing the caller.
		return &state.State{}
	}
	return s
}

// phaseIsDone answers the locked phases_done_test: a slug counts done only when
// it has a phase directory AND that directory's own record says it finished.
//
// The authority is changes.json's status marker, written by `dross ship` and
// `dross phase complete`. State history is a fallback for records written
// before that field existed, and only a fallback: history is capped at 50
// entries and had already evicted mutation-diff-scope's "completed" breadcrumb
// while the phase was plainly finished (verdict pass, PR 79 merged). Counting
// from history alone reads a window, not a record.
//
// An unscaffolded slug is never done, whatever history says. A roadmap entry
// with no phase directory is work that was listed and never built, and letting
// a stale breadcrumb close it would let a milestone finish with unbuilt
// criteria still on its roadmap.
//
// A verify verdict of "pass" is deliberately not evidence either: verified is
// not shipped, and a phase can pass verification and never open a PR.
func phaseIsDone(root, slug string, scaffolded bool, s *state.State) bool {
	if !scaffolded {
		return false
	}
	c, err := changes.Load(changes.FilePath(root, slug), slug)
	if err == nil {
		switch c.Status {
		case changes.StatusComplete, changes.StatusShipped:
			return true
		}
	}
	return historyCompletedPhase(s, slug)
}

// phaseDirExists reports whether the slug has a directory under .dross/phases/.
func phaseDirExists(root, slug string) bool {
	fi, err := os.Stat(filepath.Join(root, "phases", slug))
	return err == nil && fi.IsDir()
}

// historyCompletedPhase is the fallback arm, and it matches the breadcrumb's
// action TOKEN rather than a substring of it.
//
// historyHasAction (phase.go) uses strings.Contains, which is right for its own
// job — re-run guarding on one known id — and wrong here: with both
// `mutation-diff` and `mutation-diff-scope` on a roadmap, a single "completed
// mutation-diff-scope" breadcrumb would close BOTH, and the shorter phase would
// silently count as delivered.
func historyCompletedPhase(s *state.State, slug string) bool {
	if s == nil {
		return false
	}
	want := "completed " + slug
	for _, a := range s.History {
		act := strings.TrimSpace(a.Action)
		if act == want || strings.HasPrefix(act, want+" ") {
			return true
		}
	}
	return false
}
