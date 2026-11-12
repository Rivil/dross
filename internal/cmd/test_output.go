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
	// keep holds on to what the parsers read from every suite spawn, in
	// parsed, for a caller that judges the run by which tests failed: a
	// red-proof. It builds segments on a terminal too, where nothing is
	// summarised, because the verdict needs them wherever the run printed.
	keep   bool
	parsed []runsummary.Result
}

// spawnSeg is one spawn's share of the output.
type spawnSeg struct {
	seg *runsummary.Segment
	// aux marks a spawn that is not the suite — the tree sync, a lane's
	// prepare — whose output no parser should be asked to claim. Its tail is
	// still what a 3, 4 or 7 shows when it is the spawn that failed.
	aux bool
	// failed marks the spawn that returned the error (see testRun.failed).
	failed bool
}

func openTestRun(repoDir string) *testRun {
	tty := testStdoutIsTTY()
	l := runlog.Open(repoDir, runlog.Name{Kind: runlog.KindTest}, runlog.Options{Terminal: runlog.Stdout, Echo: tty})
	return &testRun{log: l, tty: tty, out: l, started: testNow()}
}

// spawn records the line about to spawn and gives it its own summary
// segment. The first one creates the log, names it before anything runs, and
// copies in the lines dross printed before there was a log. say already
// printed those, so they go in by Record, on a terminal and off one: Write's
// echo, or its fallback when the log is unavailable, would print them twice.
func (r *testRun) spawn(line string, aux bool) {
	if !r.named {
		for _, s := range r.pending {
			r.log.Record([]byte(s))
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
	if r.tty && !r.keep {
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

// failed marks the segment just spawned as the step whose spawn returned an
// error. Each spawn site calls it before classifying the error, so a 3, 4 or 7
// shows that spawn's tail rather than whichever spawned last: after host-1's
// rsync, a host-2 lock that could not be taken spawned nothing, and rsync's
// chatter is not why the run did not happen. On a terminal there are no
// segments and nothing to mark.
func (r *testRun) failed() {
	if n := len(r.segs); n > 0 {
		r.segs[n-1].failed = true
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
// happen (3, 4) or whose bootstrap failed (7) shows the failing step (see
// failedStep); otherwise the suite's own spawns are parsed. On a terminal, or
// when the log fell back to streaming, there is nothing to summarise: every
// byte was shown.
//
// A run that keeps its results takes them here, before the segments go: each
// lane's are dropped as the lane is summarised, long before finish.
func (r *testRun) summarise(err error) {
	segs := r.segs
	r.segs = nil
	if len(segs) == 0 {
		return
	}
	for _, s := range segs {
		s.seg.Close()
	}
	if r.keep {
		for _, s := range segs {
			if !s.aux {
				r.parsed = append(r.parsed, s.seg.Results()...)
			}
		}
	}
	if r.tty || r.log.Err() != nil {
		return
	}
	code := ExitCode(err)
	path := r.log.Path()
	var out string
	switch code {
	case exitTransport, exitPartial, exitPrepareFailed:
		out = r.failedStep(err, code, segs, path)
	default:
		var show []*runsummary.Segment
		for _, s := range segs {
			if !s.aux {
				show = append(show, s.seg)
			}
		}
		if len(show) == 0 {
			show = []*runsummary.Segment{segs[len(segs)-1].seg}
		}
		out = runsummary.Render(runsummary.Outcome{Exit: code, Ran: true}, show, &r.budget, path, testNow().Sub(r.started))
	}
	// The log line is printed once, at the very end, however many lanes ran.
	fmt.Fprint(os.Stdout, strings.TrimSuffix(out, "log: "+path+"\n"))
}

// failedStep renders a run that did not happen from the step that failed: the
// tail of the spawn that returned the error, never another spawn's. A failing
// step with no output of its own — a host lock that could not be taken, an
// argv refused before anything started, a spawn that printed nothing — shows
// no tail at all and names the reason instead, which is the first line of err;
// the full error still reaches stderr through the command's return.
func (r *testRun) failedStep(err error, code int, segs []spawnSeg, path string) string {
	var failing *runsummary.Segment
	for _, s := range segs {
		if s.failed {
			failing = s.seg
		}
	}
	outcome := runsummary.Outcome{Exit: code, Ran: false}
	if failing != nil && len(failing.Tail()) > 0 {
		return runsummary.Render(outcome, []*runsummary.Segment{failing}, &r.budget, path, testNow().Sub(r.started))
	}
	reason, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	return fmt.Sprintf("exit %d · the run did not happen — the failing step printed nothing:\n%s\n", code, runsummary.Cut(reason))
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
