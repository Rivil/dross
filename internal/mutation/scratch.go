package mutation

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// scratch is one run's private build cache: a directory the run compiles into
// and which is thrown away when the run ends.
//
// It exists because a mutation run's compiled output is worthless the moment
// the run finishes, and yet it was being written into the developer's shared
// cache. Measured 2026-08-16: the Go build cache held 64 GB, of which 61 GB was
// 2,093 single-use archives averaging 30 MB and only 348 MB was genuinely
// shared stdlib and deps. gremlins copies the module into a fresh
// `gremlins-PID/wd-RANDOM` every run and Go keys the build cache on source file
// paths, so every run re-stores every package under new keys and NOTHING is
// ever reused across runs. That is why scratch-and-wipe costs no rebuild time:
// there was no reuse to lose. An earlier symptom of the same churn was a 399 GB
// cache that filled the disk and failed a verify outright.
//
// A size ceiling on the shared cache was the wrong mechanism and is deliberately
// not what this does (the locked no_shared_ceiling decision) — it would evict
// the developer's genuinely warm entries while doing nothing about the churn.
type scratch struct {
	// Dir is the directory every declared variable points at. Empty when the
	// stack declared no cache vars, which is the no-op case.
	Dir string
	// vars is the declared variable list, in the order it was declared —
	// assignments are emitted in that order so two runs' scripts diff cleanly.
	vars []string
	// reporter receives a wipe failure. Never nil in practice; a nil reporter
	// degrades to silence rather than panicking, since a cleanup path must not
	// be able to take down a finished run.
	reporter io.Writer
}

// newScratch creates a run-private cache directory under base.
//
// An empty var list is a no-op that creates nothing: a stack whose profile
// declares no cache is not opted out of a feature, it simply has nothing to
// redirect, and creating a directory nobody would point at would be litter.
//
// base is the caller's choice rather than os.TempDir() on purpose (the locked
// scratch_location decision). helicon's /tmp is a 32 GB RAM-backed tmpfs shared
// with a running LLM; a 60 GB build cache there is an outage, not a slowdown.
func newScratch(base string, vars []string, reporter io.Writer) (*scratch, error) {
	if len(vars) == 0 {
		return &scratch{reporter: reporter}, nil
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("mutation: create scratch base %s: %w", base, err)
	}
	dir, err := os.MkdirTemp(base, "dross-cache-")
	if err != nil {
		return nil, fmt.Errorf("mutation: create scratch cache under %s: %w", base, err)
	}
	return &scratch{Dir: dir, vars: append([]string(nil), vars...), reporter: reporter}, nil
}

// Assignments returns the `NAME=<dir>` pairs this scratch stands for, in the
// order the profile declared them. Nil when there is no scratch, so a caller
// appending them changes nothing.
func (s *scratch) Assignments() []string {
	if s == nil || s.Dir == "" {
		return nil
	}
	out := make([]string, 0, len(s.vars))
	for _, v := range s.vars {
		out = append(out, v+"="+s.Dir)
	}
	return out
}

// Remove wipes the scratch. It is safe to call twice, because every exit path
// calls it and a deferred wipe must not fight an explicit one.
//
// It returns nil even when the wipe fails, reporting the failure instead (the
// locked wipe_policy decision). Both halves are load-bearing in opposite
// directions: a silent leak is what produced a 399 GB cache, and a cleanup
// error that failed the run would throw away a measurement that had already
// completed — strictly worse than the disk it was reclaiming.
func (s *scratch) Remove() error {
	if s == nil || s.Dir == "" {
		return nil
	}
	dir := s.Dir
	s.Dir = "" // idempotent: a second call has nothing left to name
	if err := os.RemoveAll(dir); err != nil {
		s.report("mutation: could not remove the scratch build cache at %s: %v\n", dir, err)
		return nil
	}
	return nil
}

// RemoteAssignments is the same list for a path that exists on another machine,
// where this process cannot create the directory itself — the remote shell does
// it as part of the script.
func remoteAssignments(dir string, vars []string) []string {
	if dir == "" || len(vars) == 0 {
		return nil
	}
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, v+"="+dir)
	}
	return out
}

// remoteScratchDir is where a remote run's scratch lives: under the granted
// workdir, never the host's default temp.
//
// The workdir is the path the operator deliberately chose for this repo's work,
// so its volume is the one they meant this to use. The host's temp is whatever
// the host happened to configure — on helicon, RAM.
func remoteScratchDir(workdir string) string {
	return filepath.Join(workdir, ".dross-cache")
}

func (s *scratch) report(format string, args ...any) {
	if s == nil || s.reporter == nil {
		return
	}
	fmt.Fprintf(s.reporter, format, args...)
}
