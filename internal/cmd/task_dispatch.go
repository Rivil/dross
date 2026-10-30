package cmd

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/gate"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/review"
)

// writerSurfaceBytes is the size gate (the locked dispatch decision): a solo
// task whose files sum to at least this many bytes at HEAD hands its code to a
// dross-task-writer; a smaller one stays inline, where a subagent's start-up
// would cost more than the reading it saves. Calibrated on the v1.7 baseline in
// .dross/phases/solo-task-subagent/kill-switch.toml [calibration].
const writerSurfaceBytes = 40000

// writerEnabled is the kill switch (c-6). It ships true for the trial; once
// kill-switch.toml records the after leg it must equal compareKillSwitch's
// verdict, so a writer that fails the thresholds turns itself off and solo
// reverts to inline.
const writerEnabled = true

// The locked thresholds: the writer ships only if median main-loop peak
// context drops by at least killSwitchContextDrop and median per-task
// wall-clock rises by at most killSwitchWallClockRise. Turns are recorded,
// never gated.
const (
	killSwitchContextDrop   = 0.25
	killSwitchWallClockRise = 0.25
)

func taskDispatch() *cobra.Command {
	return &cobra.Command{
		Use:   "dispatch <phase-id> <task-id>",
		Short: "Decide whether a solo task's code goes to a dross-task-writer or stays inline",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			scope, err := gate.ArmedScope(repoDir)
			if err != nil {
				return err
			}
			if scope == nil || scope.Kind != review.KindTask || scope.Phase != args[0] || scope.Task != args[1] {
				return fmt.Errorf("dispatch decides only the in-progress task of a /dross-execute --solo run on its phase branch, and %s %s is not that — pair mode and quicks stay inline", args[0], args[1])
			}
			plan, _, err := loadPhasePlan(args[0])
			if err != nil {
				return err
			}
			task := plan.FindTask(args[1])
			if task == nil {
				return fmt.Errorf("task not found: %s", args[1])
			}
			surface, err := taskSurface(repoDir, task.Files)
			if err != nil {
				return err
			}
			writer, why := dispatchDecision(surface, writerEnabled)
			if !writer {
				Printf("dispatch: inline (%s)\n", why)
				return nil
			}
			Printf("dispatch: writer\nsubagent_type: %s\nprompt: %s\n", review.WriterAgent, review.WriterPromptLine(args[0], args[1]))
			return nil
		},
	}
}

// dispatchDecision applies the size gate and the kill switch to a task's
// surface; why names the reason a task stays inline.
func dispatchDecision(surface int64, enabled bool) (writer bool, why string) {
	switch {
	case !enabled:
		return false, "the writer kill switch is off"
	case surface < writerSurfaceBytes:
		return false, fmt.Sprintf("surface %d bytes is under the %d-byte gate", surface, writerSurfaceBytes)
	}
	return true, ""
}

// taskSurface sums the committed size of a task's files at HEAD. A file HEAD
// does not have — a new one — counts 0, and the working tree never counts: the
// decision must not move with uncommitted edits. A path escaping the repo is
// an error, never a 0.
func taskSurface(repoDir string, files []string) (int64, error) {
	var total int64
	for _, f := range files {
		c, err := pathfence.Contain(repoDir, "plan.toml task files", f)
		if err != nil {
			return 0, err
		}
		// `<mode> <type> <object> <size>\t<path>`, or nothing for a path HEAD
		// does not have.
		out, err := gitrun.Read(repoDir, gitPathArgs("ls-tree", []string{"-l", "HEAD"}, c.Rel())...)
		if err != nil {
			return 0, fmt.Errorf("size %s at HEAD: %w", c.Rel(), err)
		}
		meta, _, _ := strings.Cut(out, "\t")
		fields := strings.Fields(meta)
		if len(fields) < 4 || fields[1] != "blob" {
			continue
		}
		n, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("size %s at HEAD: git ls-tree printed no numeric size", c.Rel())
		}
		total += n
	}
	return total, nil
}

// killSwitchRow is one task's measured main-loop cost, as kill-switch.toml
// records it.
type killSwitchRow struct {
	PeakContext  float64
	WallClockS   float64
	SurfaceBytes int64
}

// killSwitchVerdict is the kill switch's comparison of the after leg against
// the before leg, over tasks at or above the size gate only.
type killSwitchVerdict struct {
	ContextDrop   float64
	WallClockRise float64
	Pass          bool
	Reason        string
}

// compareKillSwitch filters both legs to above-gate rows and compares their
// medians: context drop = 1 − median after peak / median before peak, wall-
// clock rise = median after / median before − 1. Below-gate tasks never had a
// writer, so they cannot move the verdict. A leg with no above-gate row is an
// error, never a pass.
func compareKillSwitch(before, after []killSwitchRow) (killSwitchVerdict, error) {
	bPeak, bWall := aboveGate(before)
	aPeak, aWall := aboveGate(after)
	if len(bPeak) == 0 || len(aPeak) == 0 {
		return killSwitchVerdict{}, errors.New("a kill-switch leg has no task at or above the size gate — nothing a writer ran to compare")
	}
	v := killSwitchVerdict{
		ContextDrop:   1 - median(aPeak)/median(bPeak),
		WallClockRise: median(aWall)/median(bWall) - 1,
	}
	const eps = 1e-9
	switch {
	case v.ContextDrop < killSwitchContextDrop-eps:
		v.Reason = fmt.Sprintf("main-loop peak context dropped %.1f%%, under the %.0f%% the writer must save", 100*v.ContextDrop, 100*killSwitchContextDrop)
	case v.WallClockRise > killSwitchWallClockRise+eps:
		v.Reason = fmt.Sprintf("per-task wall-clock rose %.1f%%, over the %.0f%% the writer may cost", 100*v.WallClockRise, 100*killSwitchWallClockRise)
	default:
		v.Pass = true
	}
	return v, nil
}

func aboveGate(rows []killSwitchRow) (peaks, walls []float64) {
	for _, r := range rows {
		if r.SurfaceBytes >= writerSurfaceBytes {
			peaks = append(peaks, r.PeakContext)
			walls = append(walls, r.WallClockS)
		}
	}
	return peaks, walls
}

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n == 0 {
		return math.NaN()
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
