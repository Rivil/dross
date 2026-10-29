package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/killswitch"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
)

// killSwitchWarnings is doctor's "Kill switch:" block. A kill switch only bites
// once its [after] leg is recorded, and nothing else prompts that: without
// this, writerEnabled stays true indefinitely and "ships only if the
// thresholds hold" never happens. So a record whose [trial] is still pending,
// with no [after] rows, is named once a later solo run has completed with at
// least one task a writer took (laterWriterRun) — that run is the leg waiting
// to be mined. A completed solo run whose writer use cannot be confirmed
// because a reviewed task's commit is gone is named too, as possibly due.
// Silent while no such run exists: a trial with nothing to measure yet is not
// a fault.
//
// A kill-switch.toml that does not read or decode is named too; a record
// doctor cannot read is one the trial cannot be judged from.
func killSwitchWarnings(root string, repoDir string) []string {
	ids, err := phase.List(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, id := range ids {
		st, err := killswitch.Load(filepath.Join(phase.Dir(root, id), killswitch.File))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			out = append(out, fmt.Sprintf("%s/phases/%s/%s: the writer trial cannot be judged: %v", RootDirName, id, killswitch.File, err))
			continue
		}
		if !st.AwaitingAfter() {
			continue
		}
		since, ok := trialAnchor(root, id)
		if !ok {
			continue
		}
		var runs, unconfirmed []string
		for _, other := range ids {
			if other == id {
				continue
			}
			switch laterWriterRun(root, repoDir, other, since) {
			case writerRunYes:
				runs = append(runs, other)
			case writerRunUnknown:
				unconfirmed = append(unconfirmed, other)
			}
		}
		dir := RootDirName + "/phases/" + id
		if len(runs) > 0 {
			out = append(out, fmt.Sprintf(
				"%s: [trial] is still pending, but %s has since run solo with a writer — mine its main-loop transcript with the [before] filter into [[after.task]] rows, then set [trial].status and writerEnabled from compareKillSwitch's verdict",
				dir, strings.Join(runs, ", ")))
		}
		if len(unconfirmed) > 0 {
			out = append(out, fmt.Sprintf(
				"%s: [trial] is still pending, and %s has since run solo, but its writer use could not be confirmed (a reviewed task's files could not be sized at its commit — usually a commit gone from git after a squash-merge, or none recorded) — the [after] leg may be due: if its main-loop transcript shows a writer spawn, mine it with the [before] filter into [[after.task]] rows, then set [trial].status and writerEnabled from compareKillSwitch's verdict",
				dir, strings.Join(unconfirmed, ", ")))
		}
	}
	return out
}

// writerRun is what a later phase's run says about the writer trial.
type writerRun int

const (
	// writerRunNone: no completed solo run after the trial, or one whose
	// reviewed tasks all stayed under the dispatch gate.
	writerRunNone writerRun = iota
	// writerRunUnknown: a completed solo run after the trial with a reviewed
	// task whose commit cannot be read, and no task proven to be a writer's.
	writerRunUnknown
	// writerRunYes: a completed solo run after the trial in which a writer
	// took at least one task.
	writerRunYes
)

// trialAnchor is when the trialling phase's last recorded task completed: a
// run counts as "later" only after it, so the solo runs the [before] leg was
// mined from — and the trial phase's own tasks — never read as the [after]
// leg. A phase with no recorded task has shipped no writer, so there is no
// anchor and nothing to wait for.
func trialAnchor(root, id string) (time.Time, bool) {
	c, err := changes.Load(changes.FilePath(root, id), id)
	if err != nil {
		return time.Time{}, false
	}
	var last time.Time
	for _, rec := range c.Tasks {
		if rec.CompletedAt.After(last) {
			last = rec.CompletedAt
		}
	}
	return last, !last.IsZero()
}

// laterWriterRun reports whether phase id is a completed solo execute run with
// at least one task a writer took, finished after since — or, when a reviewed
// task's writer use cannot be measured, that it is unknown.
//
// No record names a run's mode or a task's dispatch durably — execute.json and
// writer.json are machine-local and cleared — so both are read back from what
// the run left in the phase directory:
//   - completed: every plan task is done or failed;
//   - solo: the task carries a changes.json review, which only a solo run
//     writes;
//   - writer: its plan files, sized at its commit's parent (the HEAD dispatch
//     measured), clear the same dispatchDecision `dross task dispatch` applies.
//
// A plan or record that does not load is not a completed solo run, so it is
// writerRunNone. A reviewed task whose commit git no longer has — squash-merge,
// a deleted branch, gc: the normal path once a phase ships — or that recorded
// none is writerRunUnknown unless another task proves a writer: staying silent
// there would hide exactly the run the [after] leg waits on.
func laterWriterRun(root, repoDir, id string, since time.Time) writerRun {
	plan, err := phase.LoadPlan(filepath.Join(phase.Dir(root, id), "plan.toml"))
	if err != nil || len(plan.Task) == 0 {
		return writerRunNone
	}
	for _, t := range plan.Task {
		if t.Status != "done" && t.Status != "failed" {
			return writerRunNone
		}
	}
	c, err := changes.Load(changes.FilePath(root, id), id)
	if err != nil {
		return writerRunNone
	}
	result := writerRunNone
	for _, t := range plan.Task {
		rec, done := c.Tasks[t.ID]
		if _, reviewed := c.Reviews[t.ID]; !done || !reviewed {
			continue
		}
		if !rec.CompletedAt.After(since) {
			continue
		}
		if rec.Commit == "" {
			result = writerRunUnknown
			continue
		}
		surface, err := surfaceAt(repoDir, rec.Commit+"^", t.Files)
		if err != nil {
			result = writerRunUnknown
			continue
		}
		if writer, _ := dispatchDecision(surface, writerEnabled); writer {
			return writerRunYes
		}
	}
	return result
}

// surfaceAt sums the committed size of files at rev, as taskSurface does at
// HEAD: a file rev does not have counts 0, and a path escaping the repo is an
// error.
func surfaceAt(repoDir, rev string, files []string) (int64, error) {
	var total int64
	for _, f := range files {
		c, err := pathfence.Contain(repoDir, "plan.toml task files", f)
		if err != nil {
			return 0, err
		}
		// `<mode> <type> <object> <size>\t<path>`, or nothing for a path rev
		// does not have.
		out, err := gitrun.Read(repoDir, gitRefPathArgs("ls-tree", []string{"-l"}, []string{rev}, c.Rel())...)
		if err != nil {
			return 0, fmt.Errorf("size %s at %s: %w", c.Rel(), rev, err)
		}
		meta, _, _ := strings.Cut(out, "\t")
		fields := strings.Fields(meta)
		if len(fields) < 4 || fields[1] != "blob" {
			continue
		}
		n, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("size %s at %s: git ls-tree printed no numeric size", c.Rel(), rev)
		}
		total += n
	}
	return total, nil
}
