package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/runlog"
	"github.com/Rivil/dross/internal/runsummary"
)

// testStdoutIsTTY is the summarise-or-stream switch (locked tty_behaviour): a
// terminal streams live as it always did; anything else — the agent's Bash
// tool, a `!` run — gets a summary. A test swaps it.
var testStdoutIsTTY = runlog.StdoutIsTerminal

// testNow is the clock a run's wall-clock is read from. A test swaps it.
var testNow = time.Now

// testRun is one `dross test` invocation's output: its run log under
// .dross/logs/ and, when stdout is not a terminal, one summary segment per
// spawn.
//
// Every spawn of the run — the suite, each lane and its prepare, the rsync of
// the tree, the remote ssh — gets the same writer for stdout and stderr, so
// the log holds the run in the order it happened. The file is created by the
// first `$ <line>` header, written just before the first spawn: a run refused
// before it spawns anything leaves no log behind (D8). The log's path prints
// before that first spawn, so a run killed mid-way still names it, and again
// at the end (D9).
type testRun struct {
	log     *runlog.Log
	tty     bool
	out     io.Writer // every spawn's stdout and stderr
	started time.Time
	named   bool     // the log exists and its path has been printed
	pending []string // dross's own lines printed before the log existed
	budget  runsummary.Budget
	segs    []spawnSeg // spawned since the last summary
}

// spawnSeg is one spawn's share of the output.
type spawnSeg struct {
	seg *runsummary.Segment
	// aux marks a spawn that is not the suite — the tree sync, a lane's
	// prepare — whose output no parser should be asked to claim. Its tail is
	// still what a 3, 4 or 7 shows.
	aux bool
}

func openTestRun(repoDir string) *testRun {
	tty := testStdoutIsTTY()
	l := runlog.Open(repoDir, runlog.Name{Kind: runlog.KindTest}, runlog.Options{Terminal: runlog.Stdout, Echo: tty})
	return &testRun{log: l, tty: tty, out: l, started: testNow()}
}

// spawn records the line about to spawn and gives it its own summary
// segment. The first one creates the log, names it before anything runs, and
// copies in the lines dross printed before there was a log — except on a
// terminal, where the log's echo would print them a second time.
func (r *testRun) spawn(line string, aux bool) {
	if !r.named && !r.tty {
		for _, s := range r.pending {
			r.log.Write([]byte(s))
		}
	}
	r.pending = nil
	fmt.Fprintf(r.log, "$ %s\n", line)
	if !r.named {
		r.named = true
		if p := r.log.Path(); p != "" {
			Print("log: " + p)
		}
	}
	if r.tty {
		return
	}
	s := runsummary.NewSegment()
	r.segs = append(r.segs, spawnSeg{seg: s, aux: aux})
	r.out = io.MultiWriter(r.log, s)
}

// say prints one of dross's own lines to stdout and copies it to the log, so
// the log reads as the whole transcript in order. Before the first spawn there
// is no log yet — a refusal must not create one — so the line waits for the
// first header. Once the log exists, on a terminal its echo is the print, and
// a log that failed has already printed the line through its fallback.
func (r *testRun) say(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if !r.named {
		fmt.Fprint(os.Stdout, s)
		r.pending = append(r.pending, s)
		return
	}
	r.log.Write([]byte(s))
	if !r.tty && r.log.Err() == nil {
		fmt.Fprint(os.Stdout, s)
	}
}

// laneDone prints the summary of what the lane just spawned, from the lane's
// own outcome. Each lane is its own summary: one parsed lane cannot vouch for
// another the parsers never recognised.
func (r *testRun) laneDone(err error) { r.summarise(err) }

// dropLane discards what a lane spawned unsummarised: a selector miss is told
// by its own miss line, and its runner's "no tests" output says nothing more.
func (r *testRun) dropLane() { r.segs = nil }

// summarise renders the segments spawned since the last summary, under err's
// exit code, and prints it to stdout — never into an error. A run that did not
// happen (3, 4) or whose bootstrap failed (7) shows the failing spawn's tail;
// otherwise the suite's own spawns are parsed. On a terminal, or when the log
// fell back to streaming, there is nothing to summarise: every byte was shown.
func (r *testRun) summarise(err error) {
	segs := r.segs
	r.segs = nil
	if r.tty || len(segs) == 0 || r.log.Err() != nil {
		return
	}
	for _, s := range segs {
		s.seg.Close()
	}
	code := ExitCode(err)
	outcome := runsummary.Outcome{Exit: code, Ran: true}
	var show []*runsummary.Segment
	switch code {
	case exitTransport, exitPartial, exitPrepareFailed:
		outcome.Ran = false
		show = []*runsummary.Segment{segs[len(segs)-1].seg}
	default:
		for _, s := range segs {
			if !s.aux {
				show = append(show, s.seg)
			}
		}
		if len(show) == 0 {
			show = []*runsummary.Segment{segs[len(segs)-1].seg}
		}
	}
	path := r.log.Path()
	out := runsummary.Render(outcome, show, &r.budget, path, testNow().Sub(r.started))
	// The log line is printed once, at the very end, however many lanes ran.
	fmt.Fprint(os.Stdout, strings.TrimSuffix(out, "log: "+path+"\n"))
}

// finish ends the run: whatever was spawned outside a lane is summarised, and
// the log is named again. It never changes err.
func (r *testRun) finish(err error) {
	r.log.Close()
	if !r.named {
		return // nothing spawned: no log, nothing to summarise
	}
	r.summarise(err)
	if p := r.log.Path(); p != "" {
		Print("log: " + p)
	}
}
