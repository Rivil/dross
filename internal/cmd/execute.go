package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/gatestate"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/state"
)

// Execute is `dross execute`: the state /dross-execute keeps through the CLI.
func Execute() *cobra.Command {
	c := &cobra.Command{
		Use:   "execute",
		Short: "Bookkeeping /dross-execute keeps through the CLI",
	}
	c.AddCommand(executeBegin())
	return c
}

// executeBegin records the mode a phase is executed in, in the machine-local
// .dross/gate/execute.json the pair-approval gate reads: pair unless --solo.
// It is the only writer of that record. A same-mode re-run leaves the record
// byte-for-byte alone. A solo begin refuses unless the shipped reviewer is
// installed.
//
// After the mode record it absorbs the mechanical start-of-run steps the
// prompt used to spell out, each reported on one labelled line: the version
// patch (version:), the board's in-progress sync (board:) and a report-only
// inbound task pull (pull:). Of state.json it writes only version, and the
// plan never: applying board moves stays the user's call. Every refusal runs
// before any of it, so a refused begin writes nothing and calls no board.
func executeBegin() *cobra.Command {
	var solo bool
	c := &cobra.Command{
		Use:   "begin <phase-id>",
		Short: "Record the mode /dross-execute runs a phase in (pair unless --solo)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			if err := pathfence.Segment("phase id", id); err != nil {
				return err
			}
			root, err := FindRoot()
			if err != nil {
				return err
			}
			if !isFile(filepath.Join(phase.Dir(root, id), "plan.toml")) {
				return fmt.Errorf("phase %q has no plan.toml — plan it with /dross-plan before executing it", id)
			}
			repoDir := filepath.Dir(root)
			mode := "pair"
			if solo {
				mode = "solo"
				if err := requireReviewer(repoDir); err != nil {
					return err
				}
			}
			// A phase run supersedes any quick marker left behind: a quick
			// that ended without `dross quick end` must not go on arming its
			// review inside the phase run.
			if err := gatestate.RemoveQuick(repoDir); err != nil {
				return err
			}
			rec, err := gatestate.LoadExecute(repoDir)
			if err != nil {
				return err
			}
			if rec != nil && rec.Phase == id && rec.Mode == mode {
				Printf("execute %s: %s (already recorded)\n", id, mode)
			} else {
				if err := gatestate.SaveExecute(repoDir, gatestate.Execute{Phase: id, Mode: mode, At: time.Now().UTC()}); err != nil {
					return err
				}
				Printf("execute %s: %s (recorded)\n", id, mode)
			}
			// absorbs: dross state set version <major>.<minor>.$PATCH.0
			line, err := beginVersion(root, id)
			if err != nil {
				return err
			}
			Printf("version: %s\n", line)
			return beginBoard(id)
		},
	}
	c.Flags().BoolVar(&solo, "solo", false, "record solo mode: the run proceeds without per-task approvals")
	return c
}

// milestoneVersionRe is the vX.Y milestone shape the version's major.minor
// comes from.
var milestoneVersionRe = regexp.MustCompile(`^v(\d+)\.(\d+)$`)

// beginVersion sets the version patch to the phase's ordinal within the
// current milestone — phase.DisplayNumber, the source `dross phase number`
// reads — and zeroes internal. It writes only when major.minor or patch
// differ, so a resumed run keeps the quick-task counter. With no milestone to
// derive from it says why and writes nothing rather than guess.
func beginVersion(root, id string) (string, error) {
	path := filepath.Join(root, state.File)
	s, err := state.Load(path)
	if err != nil {
		return "", err
	}
	if s.CurrentMilestone == "" {
		return "skipped (no current milestone)", nil
	}
	m, err := milestone.Load(milestone.FilePath(root, s.CurrentMilestone))
	if err != nil {
		return fmt.Sprintf("skipped (milestone %s: %v)", s.CurrentMilestone, err), nil
	}
	n := phase.DisplayNumber(m.Phases, id)
	if n == 0 {
		return fmt.Sprintf("skipped (phase %s is not in milestone %s)", id, s.CurrentMilestone), nil
	}
	mv := milestoneVersionRe.FindStringSubmatch(s.CurrentMilestone)
	if mv == nil {
		return fmt.Sprintf("skipped (milestone %q is not vX.Y)", s.CurrentMilestone), nil
	}
	patch := strconv.Itoa(n)
	if cur := strings.Split(s.Version, "."); validateVersion(s.Version) == nil &&
		cur[0] == mv[1] && cur[1] == mv[2] && cur[2] == patch {
		return s.Version + " unchanged", nil
	}
	want := mv[1] + "." + mv[2] + "." + patch + ".0"
	old := s.Version
	if err := writeVersion(root, s, want); err != nil {
		return "", err
	}
	if err := s.Save(path); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s -> %s", old, want), nil
}

// beginBoard marks the phase in progress on the board and reports, without
// applying, any task a card move put out of step with the plan. A board
// failure is printed as `board: FAILED` and fails the begin — after the mode
// record, which stands. Board sync off is a no-op that says so.
func beginBoard(id string) error {
	ctx, enabled, err := openBoard()
	if err != nil {
		Printf("board: FAILED: %v\npull: skipped (board failed)\n", err)
		return err
	}
	if !enabled {
		Printf("board: off\npull: off\n")
		return nil
	}
	ctx.Out = &labelWriter{label: "board: "}
	// absorbs: dross issue phase sync <id> --status in-progress
	if err := boardsync.SyncPhase(ctx, id, "in-progress", false); err != nil {
		Printf("board: FAILED: %v\npull: skipped (board failed)\n", err)
		return fmt.Errorf("board phase sync: %w", err)
	}
	// absorbs: dross issue task pull <phase>
	if err := boardsync.RequireWorkflowState(ctx.Proj.Board.Provider); err != nil {
		Printf("pull: n/a (board provider %q has no workflow state; only the outbound mirror runs)\n", ctx.Proj.Board.Provider)
		return nil
	}
	plan, _, _, err := loadPhasePlanAndSpec(id)
	if err != nil {
		return err
	}
	moves, err := boardsync.CollectTaskMoves(ctx, id, plan)
	if err != nil {
		Printf("pull: FAILED: %v\n", err)
		return fmt.Errorf("board task pull: %w", err)
	}
	reported := 0
	for _, m := range moves {
		switch m.Kind {
		case boardsync.TaskBoardMoved:
			reported++
			Printf("pull: %s %s -> %s (board says %s)\n", m.TaskID, m.PlanStatus, m.NewStatus, m.BoardState)
		case boardsync.TaskConflict:
			reported++
			Printf("pull: %s CONFLICT: plan says %q, board says %q — both changed since they last agreed (%q/%q)\n",
				m.TaskID, m.PlanStatus, m.BoardState, m.WasPlan, m.WasBoard)
		}
	}
	if reported == 0 {
		Printf("pull: no task moves on the board\n")
		return nil
	}
	Printf("pull: %d reported, nothing applied — `dross issue task pull %s --apply` writes the clean moves\n", reported, id)
	return nil
}

// labelWriter prints each whole line written to it with label in front, so a
// board sync's own narration lands on begin's labelled line.
type labelWriter struct {
	label string
	buf   []byte
}

func (w *labelWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		Printf("%s%s\n", w.label, w.buf[:i])
		w.buf = w.buf[i+1:]
	}
}
