package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Rivil/dross/internal/runlog"
	"github.com/Rivil/dross/internal/runsummary"
)

// testStdoutIsTTY is the summarise-or-stream switch (locked tty_behaviour): a
// terminal streams live as it always did; anything else — the agent's Bash
// tool, a `!` run — gets a summary. A test swaps it.
var testStdoutIsTTY = runlog.StdoutIsTerminal

// testRun is one `dross test` invocation's output: its run log under
// .dross/logs/ and, when stdout is not a terminal, the summary feed.
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
	seg     *runsummary.Segment // nil on a terminal
	out     io.Writer           // every spawn's stdout and stderr
	started time.Time
	named   bool     // the log exists and its path has been printed
	pending []string // dross's own lines printed before the log existed
}

func openTestRun(repoDir string) *testRun {
	tty := testStdoutIsTTY()
	l := runlog.Open(repoDir, runlog.Name{Kind: runlog.KindTest}, runlog.Options{Terminal: runlog.Stdout, Echo: tty})
	r := &testRun{log: l, tty: tty, started: time.Now()}
	if tty {
		r.out = l
	} else {
		r.seg = runsummary.NewSegment()
		r.out = io.MultiWriter(l, r.seg)
	}
	return r
}

// header records the line about to spawn, creating the log on the first one
// and naming it before anything runs. The first header also copies in the
// lines dross printed before there was a log, so the log reads in order —
// except on a terminal, where the log's echo would print them a second time.
func (r *testRun) header(line string) {
	if !r.named && !r.tty {
		for _, s := range r.pending {
			r.log.Write([]byte(s))
		}
	}
	r.pending = nil
	fmt.Fprintf(r.log, "$ %s\n", line)
	if r.named {
		return
	}
	r.named = true
	if p := r.log.Path(); p != "" {
		Print("log: " + p)
	}
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

// finish ends the run: on a terminal it names the log again; otherwise it
// prints the summary of everything the spawns wrote. It never changes err.
func (r *testRun) finish(err error) {
	r.log.Close()
	if !r.named {
		return // nothing spawned: no log, nothing to summarise
	}
	if r.tty || r.log.Err() != nil {
		// Streamed live: on a terminal by choice, otherwise because the log
		// could not take it and its fallback printed every byte.
		if p := r.log.Path(); p != "" {
			Print("log: " + p)
		}
		return
	}
	r.seg.Close()
	code := ExitCode(err)
	ran := code != exitTransport && code != exitPartial
	fmt.Fprint(os.Stdout, runsummary.Render(runsummary.Outcome{Exit: code, Ran: ran},
		[]*runsummary.Segment{r.seg}, &runsummary.Budget{}, r.log.Path(), time.Since(r.started)))
}
