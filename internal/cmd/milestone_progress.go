package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
	"github.com/spf13/cobra"
)

// `dross milestone progress` answers, in one deterministic call, the question
// /dross-milestone has to answer before it can do anything: is there an active
// milestone, is it finished, and if not what is left.
//
// It is dispatch data, not a gate. Every arm exits 0, including "planning" and
// "nothing done yet" — a non-zero exit here would make the prompt read a normal
// state as a broken command.

// milestoneProgress is the emitted document.
type milestoneProgressReport struct {
	Version string `json:"version"`
	// Status is [milestone].status verbatim, never normalised or inferred. The
	// dispatch branches on it first, so a value invented here would route the
	// prompt somewhere the toml never asked for.
	Status string `json:"status"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	// AllDone is true only when every listed slug is done AND there is at least
	// one. An empty roadmap is not a finished milestone.
	AllDone bool `json:"all_done"`
	// Remaining is every slug not yet done, in roadmap order — including the
	// unscaffolded ones, which are outstanding work like any other.
	Remaining []string `json:"remaining"`
	// Unscaffolded is the subset of Remaining with no directory under
	// .dross/phases/ at all: listed on the roadmap, never started.
	Unscaffolded []string `json:"unscaffolded"`
}

func milestoneProgressCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "progress [version]",
		Short: "Report how far a milestone has got: status, done/total, and what is left",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			version := ""
			if len(args) == 1 {
				version = args[0]
			} else {
				s, err := state.Load(filepath.Join(root, state.File))
				if err != nil {
					return fmt.Errorf("no version given and load state: %w", err)
				}
				version = s.CurrentMilestone
				if version == "" {
					return errors.New("no version given and state has no current_milestone; run `dross milestone list` to see options")
				}
			}
			rep, err := buildMilestoneProgress(root, version)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(rep)
			}
			Printf("%s (%s): %d/%d phases done\n", rep.Version, rep.Status, rep.Done, rep.Total)
			if len(rep.Remaining) > 0 {
				Printf("remaining: %s\n", strings.Join(rep.Remaining, ", "))
			}
			if len(rep.Unscaffolded) > 0 {
				Printf("not scaffolded yet: %s\n", strings.Join(rep.Unscaffolded, ", "))
			}
			if rep.AllDone {
				Printf("every phase is done — `dross milestone complete %s` opens the integration PR\n", rep.Version)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, jsonFlagUsage)
	return c
}

func buildMilestoneProgress(root, version string) (*milestoneProgressReport, error) {
	m, err := milestone.Load(milestone.FilePath(root, version))
	if err != nil {
		return nil, err
	}
	// state.json is machine-local and gitignored, so it is simply absent in a
	// fresh clone (and on CI). It only ever feeds phaseIsDone's history
	// fallback, never the authoritative changes.json marker — so its absence
	// degrades the fallback to "nothing recorded" rather than failing the
	// command. Erroring here made `dross milestone progress <version>` unusable
	// on a checkout nobody had run a dross command in yet.
	s, err := state.Load(filepath.Join(root, state.File))
	if errors.Is(err, fs.ErrNotExist) {
		s = &state.State{}
	} else if err != nil {
		return nil, err
	}

	rep := &milestoneProgressReport{
		Version:      version,
		Status:       m.Milestone.Status,
		Total:        len(m.Phases),
		Remaining:    []string{},
		Unscaffolded: []string{},
	}
	for _, slug := range m.Phases {
		scaffolded := phaseDirExists(root, slug)
		if phaseIsDone(root, slug, scaffolded, s) {
			rep.Done++
			continue
		}
		rep.Remaining = append(rep.Remaining, slug)
		if !scaffolded {
			rep.Unscaffolded = append(rep.Unscaffolded, slug)
		}
	}
	rep.AllDone = rep.Total > 0 && rep.Done == rep.Total
	return rep, nil
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
	want := "completed " + slug
	for _, a := range s.History {
		act := strings.TrimSpace(a.Action)
		if act == want || strings.HasPrefix(act, want+" ") {
			return true
		}
	}
	return false
}
