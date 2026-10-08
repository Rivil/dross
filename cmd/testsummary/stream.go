package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// event is one line of `go test -json`: a cmd/test2json event, or one of the
// build events cmd/go interleaves with them (build-output, build-fail), which
// name their package by ImportPath rather than Package.
type event struct {
	Time       time.Time
	Action     string
	Package    string
	Test       string
	Elapsed    float64
	Output     string
	ImportPath string
}

// result is one finished test or package, as the timing summary reads it.
// Test is "" for the package itself. Action is pass, fail or skip — or
// "running" for a test its package ended underneath, which is how a timeout's
// victim looks: go test's panic names it, but it never gets an event of its own.
type result struct {
	Package string
	Test    string
	Action  string
	Elapsed float64
}

// testRun is a test that has started and not finished: its output so far.
type testRun struct {
	start time.Time
	lines []string
}

// pkgRun is one package while its events stream in.
type pkgRun struct {
	running map[string]*testRun
	order   []string // running's keys, first-seen first
	output  []string // package-level output (events with no Test)
	done    bool
}

func (p *pkgRun) test(name string) *testRun {
	t, ok := p.running[name]
	if !ok {
		t = &testRun{}
		p.running[name] = t
		p.order = append(p.order, name)
	}
	return t
}

// reducer turns the event stream into the plain log and the verdict.
type reducer struct {
	w       io.Writer
	pkgs    map[string]*pkgRun
	order   []string            // pkgs' keys, first-seen first
	build   map[string][]string // build output per ImportPath, held until its build-fail
	results []result
	events  int
	last    time.Time // the latest event time, standing in for a cut stream's end
	failed  bool
}

func newReducer(w io.Writer) *reducer {
	return &reducer{w: w, pkgs: map[string]*pkgRun{}, build: map[string][]string{}}
}

// read feeds every line of in to the reducer. bufio.Reader.ReadBytes, not a
// bufio.Scanner: one output line of a test can be arbitrarily long, and a
// scanner's 64 KiB ceiling would abort the whole summary on it.
func (r *reducer) read(in io.Reader) error {
	br := bufio.NewReader(in)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			r.line(bytes.TrimRight(line, "\r\n"))
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// line handles one line: an event, or anything else, which passes through.
func (r *reducer) line(b []byte) {
	var e event
	if len(b) == 0 || b[0] != '{' || json.Unmarshal(b, &e) != nil || e.Action == "" {
		r.print(string(b) + "\n")
		return
	}
	r.events++
	if e.Time.After(r.last) {
		r.last = e.Time
	}
	switch {
	case e.Action == "build-output":
		r.build[e.ImportPath] = append(r.build[e.ImportPath], e.Output)
	case e.Action == "build-fail":
		r.failed = true
		r.print(r.build[e.ImportPath]...)
		delete(r.build, e.ImportPath)
	case e.Package == "":
		// neither a test event nor a build event: nothing to attribute it to
	case e.Test == "":
		r.packageEvent(e, r.pkg(e.Package))
	default:
		r.testEvent(e, r.pkg(e.Package))
	}
}

func (r *reducer) pkg(name string) *pkgRun {
	p, ok := r.pkgs[name]
	if !ok {
		p = &pkgRun{running: map[string]*testRun{}}
		r.pkgs[name] = p
		r.order = append(r.order, name)
	}
	return p
}

func (r *reducer) testEvent(e event, p *pkgRun) {
	switch e.Action {
	case "run":
		p.test(e.Test).start = e.Time
	case "output":
		t := p.test(e.Test)
		t.lines = append(t.lines, e.Output)
	case "pass", "skip":
		delete(p.running, e.Test)
		r.results = append(r.results, result{e.Package, e.Test, e.Action, e.Elapsed})
	case "fail":
		r.failed = true
		lines := p.test(e.Test).lines
		delete(p.running, e.Test)
		// A failed subtest's output goes under its parent, which prints it
		// when it fails in turn — nested as plain go test nests it.
		if parent, ok := p.running[parentOf(e.Test)]; ok {
			parent.lines = append(parent.lines, lines...)
		} else {
			r.print(lines...)
		}
		r.results = append(r.results, result{e.Package, e.Test, e.Action, e.Elapsed})
	}
}

func (r *reducer) packageEvent(e event, p *pkgRun) {
	switch e.Action {
	case "output":
		p.output = append(p.output, e.Output)
	case "pass", "skip":
		// The package's own last line is its summary: `ok  <pkg> 0.1s`, or
		// `?   <pkg> [no test files]` for the skip a test-less package ends in.
		if n := len(p.output); n > 0 {
			r.print(p.output[n-1])
		} else {
			r.print(fmt.Sprintf("%s\t%s\n", e.Action, e.Package))
		}
		r.end(p, e.Package, e.Action, e.Elapsed)
	case "fail":
		r.failed = true
		r.flushRunning(p, e.Package, e.Time)
		r.print(p.output...)
		r.end(p, e.Package, e.Action, e.Elapsed)
	}
}

func (r *reducer) end(p *pkgRun, name, action string, elapsed float64) {
	p.done = true
	// Emptied, not nil: a stray late event must not panic on a nil map.
	p.running, p.order, p.output = map[string]*testRun{}, nil, nil
	r.results = append(r.results, result{Package: name, Action: action, Elapsed: elapsed})
}

// flushRunning prints the output of every test p ended underneath — a timeout
// panic is attributed to the test that was running — and records each one.
func (r *reducer) flushRunning(p *pkgRun, name string, end time.Time) {
	for _, test := range p.order {
		t, ok := p.running[test]
		if !ok {
			continue
		}
		r.print(t.lines...)
		delete(p.running, test)
		var elapsed float64
		if !t.start.IsZero() && end.After(t.start) {
			elapsed = end.Sub(t.start).Seconds()
		}
		r.results = append(r.results, result{name, test, "running", elapsed})
	}
}

// finish flushes whatever the stream left open and returns the exit code: 0
// only when there were events and nothing failed or was cut off.
func (r *reducer) finish() int {
	for _, name := range r.order {
		p := r.pkgs[name]
		if p.done {
			continue
		}
		r.failed = true
		r.flushRunning(p, name, r.last)
		r.print(p.output...)
		r.print(fmt.Sprintf("FAIL\t%s\t(the stream ended before the package finished)\n", name))
		r.end(p, name, "fail", 0)
	}
	var leftover []string
	for ip := range r.build {
		leftover = append(leftover, ip)
	}
	sort.Strings(leftover)
	for _, ip := range leftover {
		r.print(r.build[ip]...)
	}
	if r.events == 0 {
		r.print("testsummary: no test events in the stream — was go test run with -json?\n")
		return 1
	}
	if r.failed {
		return 1
	}
	return 0
}

func (r *reducer) print(lines ...string) {
	for _, l := range lines {
		fmt.Fprint(r.w, l)
	}
}

// parentOf is the test a subtest runs under, or "" for a top-level test.
func parentOf(test string) string {
	i := strings.LastIndexByte(test, '/')
	if i < 0 {
		return ""
	}
	return test[:i]
}
