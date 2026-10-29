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
// to be mined. Silent while no such run exists: a trial with nothing to
// measure yet is not a fault.
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
		var runs []string
		for _, other := range ids {
			if other != id && laterWriterRun(root, repoDir, other, since) {
				runs = append(runs, other)
			}
		}
		if len(runs) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s: [trial] is still pending, but %s has since run solo with a writer — mine its main-loop transcript with the [before] filter into [[after.task]] rows, then set [trial].status and writerEnabled from compareKillSwitch's verdict",
			RootDirName+"/phases/"+id, strings.Join(runs, ", ")))
	}
	return out
}

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
// at least one task a writer took, finished after since.
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
// Anything that cannot be read — a plan or record that does not load, a
// commit git no longer has — is not proof of a writer run, so it is false.
func laterWriterRun(root, repoDir, id string, since time.Time) bool {
	plan, err := phase.LoadPlan(filepath.Join(phase.Dir(root, id), "plan.toml"))
	if err != nil || len(plan.Task) == 0 {
		return false
	}
	for _, t := range plan.Task {
		if t.Status != "done" && t.Status != "failed" {
			return false
		}
	}
	c, err := changes.Load(changes.FilePath(root, id), id)
	if err != nil {
		return false
	}
	for _, t := range plan.Task {
		rec, done := c.Tasks[t.ID]
		if _, reviewed := c.Reviews[t.ID]; !done || !reviewed {
			continue
		}
		if rec.Commit == "" || !rec.CompletedAt.After(since) {
			continue
		}
		surface, err := surfaceAt(repoDir, rec.Commit+"^", t.Files)
		if err != nil {
			continue
		}
		if writer, _ := dispatchDecision(surface, writerEnabled); writer {
			return true
		}
	}
	return false
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
